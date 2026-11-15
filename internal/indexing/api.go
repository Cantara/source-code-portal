package indexing

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// AdminPath is the address of the admin's list.
const AdminPath = "/api/v1/admin/indexing"

// RepoItem is a repository the admin's card draws, with why it is kept out ("" when it is not: named, product or pattern).
type RepoItem struct {
	Address string `json:"address"`
	Why     string `json:"why"`
}

// GroupNode is a product with its repositories.
type GroupNode struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Excluded bool       `json:"excluded"`
	Repos    []RepoItem `json:"repos"`
}

// adminBody is what the admin's card needs: the stored list, the public products with their repositories (a private repository is
// never offered to search engines and is not listed), how many repositories each pattern matches, and the revision.
type adminBody struct {
	Excluded
	Tree           []GroupNode `json:"tree"`
	Ungrouped      []RepoItem  `json:"ungrouped"`
	ExcludeMatches []int       `json:"excludeMatches"`
	IncludeMatches []int       `json:"includeMatches"`
	Private        int         `json:"private"`
	Revision       string      `json:"revision"`
	Writable       bool        `json:"writable"`
	Problem        string      `json:"problem"`
}

type saveBody struct {
	Excluded
	Revision string `json:"revision"`
	DryRun   bool   `json:"dryRun"` // answer as if the list were saved, and save nothing: the card's preview of a rule
}

// Routes are the admin's read and save, behind the admin role; a save comes from the portal's own pages. view is the full snapshot the
// choices are drawn from.
func (s *Set) Routes(publicURL string, view func(context.Context) *snapshot.Snapshot, log *slog.Logger) []httpapi.Route {
	h := admin{s: s, publicURL: publicURL, view: view, log: log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPath, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
	}
}

type admin struct {
	s         *Set
	publicURL string
	view      func(context.Context) *snapshot.Snapshot
	log       *slog.Logger
}

// compose is the card's body for the list st over the view v (the public part of it is drawn).
func compose(st Excluded, v *snapshot.Snapshot) adminBody {
	for _, l := range []*[]string{&st.Groups, &st.Repos, &st.Exclude, &st.Include} { // nothing saved yet: empty lists, not null
		if *l == nil {
			*l = []string{}
		}
	}
	pub := v.Public()
	item := func(k snapshot.RepoKey) RepoItem { return RepoItem{Address: string(k), Why: why(st, pub, k)} }
	tree := make([]GroupNode, 0, len(pub.Groups))
	for _, g := range pub.Groups {
		keys := slices.Concat(g.Members, g.Retired)
		slices.Sort(keys)
		node := GroupNode{ID: g.ID, Name: g.Name, Excluded: slices.Contains(st.Groups, g.ID), Repos: make([]RepoItem, 0, len(keys))}
		for _, k := range keys {
			node.Repos = append(node.Repos, item(k))
		}
		tree = append(tree, node)
	}
	ungrouped := []RepoItem{}
	addresses := make([]snapshot.RepoKey, 0, len(pub.Repos))
	for k := range pub.Repos {
		addresses = append(addresses, k)
		if len(pub.GroupsOf(k)) == 0 {
			ungrouped = append(ungrouped, item(k))
		}
	}
	slices.SortFunc(ungrouped, func(a, b RepoItem) int { return cmp.Compare(a.Address, b.Address) })
	count := func(res []*regexp.Regexp) []int {
		out := make([]int, len(res))
		for i, re := range res {
			for _, k := range addresses {
				if re.MatchString(string(k)) {
					out[i]++
				}
			}
		}
		return out
	}
	return adminBody{Excluded: st, Tree: tree, Ungrouped: ungrouped, ExcludeMatches: count(st.exclude), IncludeMatches: count(st.include),
		Private: len(v.Repos) - len(pub.Repos)}
}

func (h admin) answer(w http.ResponseWriter, r *http.Request, st Excluded, revision, problem string) {
	body := compose(st, h.view(r.Context()))
	body.Revision, body.Writable, body.Problem = revision, h.s.file.Writable(), problem
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(body) //nolint:errcheck,gosec // the client went away
}

func (h admin) get(w http.ResponseWriter, r *http.Request) {
	st := h.s.file.Get()
	h.answer(w, r, st.Value, st.Revision, st.Problem)
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	if !httpapi.RequireJSON(w, r, "the list") {
		return
	}
	if !httpapi.SameOrigin(r, h.publicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the list from the portal's own pages.")
		return
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a list of groups, repositories and patterns.") {
		return
	}
	clean, err := clean(body.Excluded)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return
	}
	if body.DryRun {
		st := h.s.file.Get()
		h.answer(w, r, clean, st.Revision, st.Problem)
		return
	}
	if !h.s.file.Writable() {
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save the list in.")
		return
	}
	b, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
		return
	}
	switch _, err := h.s.file.Save(r.Context(), append(b, '\n'), body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved the list since you opened it; reload to see theirs.")
	case err != nil:
		h.log.Error("indexing list not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
	default:
		h.get(w, r)
	}
}
