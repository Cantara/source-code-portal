package setup

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/ghlogin"
)

type repoOut struct {
	Owner    string   `json:"owner"`
	Name     string   `json:"name"`
	Private  bool     `json:"private,omitempty"`
	Archived bool     `json:"archived,omitempty"`
	Topics   []string `json:"topics,omitempty"`
}

type reposOut struct {
	Repos     []repoOut `json:"repos"`
	More      bool      `json:"more,omitempty"`
	Suggested []Product `json:"suggested"`
	Limits    struct {
		Products int `json:"products"`
		Matchers int `json:"matchers"`
		Hidden   int `json:"hidden"`
	} `json:"limits"`
}

// chosen validates the accounts the person ticked against the ones the token has.
func (s *Server) chosen(accounts []string) ([]string, error) {
	var out []string
	for _, a := range accounts {
		if !loginPattern.MatchString(a) || a != s.user && !slices.Contains(s.orgs, a) {
			return nil, bad("%q is not one of your accounts.", a)
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, bad("Choose at least one account.")
	}
	return out, nil
}

// listRepos answers with the repositories the read (startListing) found, for the accounts asked for.
func (s *Server) listRepos(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string `json:"accounts"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.token.Value == "" {
		s.mu.Unlock()
		return nil, bad("Sign in first.")
	}
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	slices.Sort(accounts)
	if !s.listed || !slices.Equal(s.listedFor, accounts) {
		s.mu.Unlock()
		return nil, bad("The repositories are not read yet.")
	}
	all, more := s.repos, s.more
	s.mu.Unlock()
	var mine []ghlogin.Repo
	for _, r := range all {
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && namePattern.MatchString(r.Name) {
			mine = append(mine, r)
		}
	}
	mine = OnePerName(mine)
	out := reposOut{More: more, Suggested: Suggest(mine)}
	out.Limits.Products, out.Limits.Matchers, out.Limits.Hidden = MaxProducts, MaxMatchers, MaxHidden
	for _, r := range mine {
		out.Repos = append(out.Repos, repoOut{Owner: r.Owner, Name: r.Name, Private: r.Private, Archived: r.Archived, Topics: r.Topics[:min(len(r.Topics), 5)]})
	}
	return out, nil
}

func (s *Server) finish(ctx context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string  `json:"accounts"`
		Products []Product `json:"products"`
		Hidden   []string  `json:"hidden"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.snapshot(), nil
	}
	if s.token.Value == "" || !s.listed {
		return nil, bad("Sign in and look at your repositories first.")
	}
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	if sorted := slices.Sorted(slices.Values(accounts)); !slices.Equal(sorted, s.listedFor) {
		return nil, bad("The repositories of these accounts are not read yet.")
	}
	known := map[string]bool{}
	for _, r := range s.repos {
		known[r.Name] = true
	}
	if err := checkProducts(in.Products, in.Hidden, known); err != nil {
		return nil, err
	}
	def := ""
	for _, r := range s.repos { // the most recently pushed first
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && !slices.Contains(in.Hidden, r.Name) {
			def = r.Name
			break
		}
	}
	for i := range in.Products {
		in.Products[i].Name = strings.TrimSpace(in.Products[i].Name)
	}
	mine := s.mine(accounts)
	derived := Derive(mine, in.Products, in.Hidden)
	for i, d := range derived {
		if d.TooMany {
			return nil, bad("The product %q needs more than %d matchers. Add a rule that covers some of its repositories, or split it.", in.Products[i].Name, MaxMatchers)
		}
		in.Products[i].Matchers = d.Entries
	}
	var rest []string // what no product selects: the portal's "Other"
	for _, r := range mine {
		if slices.Contains(in.Hidden, r.Name) || !namePattern.MatchString(r.Name) {
			continue
		}
		if !slices.ContainsFunc(in.Products, func(p Product) bool { return selects(p.Matchers, r) }) {
			rest = append(rest, r.Name)
		}
	}
	launch, err := s.cfg.Finish(ctx, Result{Host: s.host, Token: s.token, User: s.user, Accounts: accounts, Products: in.Products,
		Hidden: in.Hidden, Unassigned: rest, Default: def, Apps: s.installedApps(accounts)})
	if err != nil {
		return nil, fmt.Errorf("writing your settings: %w", err)
	}
	s.finished, s.launch = true, launch
	close(s.done)
	return s.snapshot(), nil
}

// selects reports whether any of the matchers selects the repository.
func selects(matchers []Rule, r ghlogin.Repo) bool {
	for _, m := range matchers {
		if fn, err := compile(m.Kind, m.Value); err == nil && fn(r.Name, r.Topics) {
			return true
		}
	}
	return false
}

// mine are the listed repositories the portal would take from the chosen accounts: no forks, valid names, one per name. Callers
// hold s.mu.
func (s *Server) mine(accounts []string) []ghlogin.Repo {
	var out []ghlogin.Repo
	for _, r := range s.repos {
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && namePattern.MatchString(r.Name) {
			out = append(out, r)
		}
	}
	return OnePerName(out)
}

// previewRules answers, for the products as the page has them now, what each comes to: its rules and what they catch, the pinned
// repositories, the matchers.
func (s *Server) previewRules(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string  `json:"accounts"`
		Products []Product `json:"products"`
		Hidden   []string  `json:"hidden"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	if len(in.Products) > MaxProducts {
		return nil, bad("At most %d products.", MaxProducts)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	if !s.listed || !slices.Equal(slices.Sorted(slices.Values(accounts)), s.listedFor) {
		return nil, bad("The repositories are not read yet.")
	}
	return map[string]any{"products": Derive(s.mine(accounts), in.Products, in.Hidden)}, nil
}

// testRule tells what one rule would select among the chosen accounts' repositories, for the page's "add a rule".
func (s *Server) testRule(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string `json:"accounts"`
		Kind     string   `json:"kind"`
		Value    string   `json:"value"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	fn, err := compile(in.Kind, strings.TrimSpace(in.Value))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range s.mine(accounts) {
		if fn(r.Name, r.Topics) {
			names = append(names, r.Name)
		}
	}
	return map[string]any{"matches": len(names), "names": names}, nil
}

// checkProducts holds the products and the hidden repositories to the configuration's limits and to the repositories listed.
func checkProducts(products []Product, hidden []string, known map[string]bool) error {
	if len(products) > MaxProducts {
		return bad("At most %d products.", MaxProducts)
	}
	if len(hidden) > MaxHidden {
		return bad("At most %d hidden repositories; leave out an account instead.", MaxHidden)
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	use := func(n string) error {
		if !known[n] {
			return bad("%q is not one of your repositories.", n)
		}
		if seen[n] {
			return bad("%q is in two places.", n)
		}
		seen[n] = true
		return nil
	}
	for _, p := range products {
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n"):
			return bad("A product needs a name of up to 120 characters.")
		case names[strings.ToLower(name)]:
			return bad("There are two products called %q.", name)
		case len(p.Repos) == 0:
			return bad("The product %q has no repositories.", name)
		}
		if len(p.Rules) > MaxRules {
			return bad("The product %q has more than %d rules of your own.", name, MaxRules)
		}
		for _, r := range p.Rules {
			if _, err := compile(r.Kind, r.Value); err != nil {
				return bad("A rule of %q (%s %s): %v", name, r.Kind, r.Value, err)
			}
		}
		names[strings.ToLower(name)] = true
		for _, n := range p.Repos {
			if err := use(n); err != nil {
				return err
			}
		}
	}
	for _, n := range hidden {
		if err := use(n); err != nil {
			return err
		}
	}
	return nil
}
