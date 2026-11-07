package setup

import (
	"cmp"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"unicode"

	"github.com/cantara/source-code-portal/internal/ghlogin"
)

// Rule is one matcher of a product, in the words of the portal's configuration: prefix, topic, glob, regexp or exact. What a
// product holds is its pinned repositories (exact) and the rules that new repositories fit.
type Rule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	// Filled by Derive, for the page:
	Own    int      `json:"own,omitempty"`    // members it covers
	Extra  []string `json:"extra,omitempty"`  // repositories of no product it also catches (the first 8)
	ExtraN int      `json:"extraN,omitempty"` // how many of those
	Spill  []string `json:"spill,omitempty"`  // members of other products it catches (the first 8)
	Manual bool     `json:"manual,omitempty"` // the person wrote it
	Open   bool     `json:"open"`             // a repository created later can join through it
}

// Derived is what a product comes to: the rules, the repositories no rule covers, and the matchers the configuration gets.
type Derived struct {
	Name    string   `json:"name"`
	Rules   []Rule   `json:"rules"`
	Pinned  []string `json:"pinned"`
	TooMany bool     `json:"tooMany,omitempty"` // more matchers than a group holds
	Entries []Rule   `json:"-"`                 // rules, then the pinned ones as exact (or alternations when there are too many)
}

// Limits the portal's matchers hold (internal/catalog/match.go): the page checks the same so the configuration loads.
const (
	maxRegexpBytes = 256
	maxRegexpInsts = 1000
	maxGlobStars   = 6
	maxSample      = 8
)

var topicPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)

type matchFn func(name string, topics []string) bool

// compile builds a matcher the way the portal does: names are compared without regard to case for exact, prefix and glob, a regexp
// is anchored and case-sensitive, a topic is exact.
func compile(kind, value string) (matchFn, error) {
	switch kind {
	case "exact":
		if !namePattern.MatchString(value) {
			return nil, fmt.Errorf("%q is not a repository name", value)
		}
		return func(n string, _ []string) bool { return strings.EqualFold(n, value) }, nil
	case "prefix":
		if !namePattern.MatchString(value) {
			return nil, fmt.Errorf("%q is not the start of a repository name", value)
		}
		p := strings.ToLower(value)
		return func(n string, _ []string) bool { return strings.HasPrefix(strings.ToLower(n), p) }, nil
	case "glob":
		if value == "" || len(value) > 100 || strings.Count(value, "*") > maxGlobStars {
			return nil, fmt.Errorf("a pattern has 1 to 100 characters and at most %d *", maxGlobStars)
		}
		var b strings.Builder
		b.WriteString("(?i)^")
		for _, c := range value {
			switch c {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteString(".")
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		b.WriteString("$")
		re := regexp.MustCompile(b.String())
		return func(n string, _ []string) bool { return re.MatchString(n) }, nil
	case "regexp":
		if value == "" || len(value) > maxRegexpBytes {
			return nil, fmt.Errorf("a regular expression has 1 to %d bytes", maxRegexpBytes)
		}
		if _, err := syntax.Parse(value, syntax.Perl); err != nil {
			return nil, fmt.Errorf("not a regular expression: %v", err)
		}
		anchored := `^(?:` + value + `)$`
		parsed, err := syntax.Parse(anchored, syntax.Perl)
		if err != nil {
			return nil, fmt.Errorf("not a regular expression: %v", err)
		}
		if prog, err := syntax.Compile(parsed.Simplify()); err != nil || len(prog.Inst) > maxRegexpInsts {
			return nil, fmt.Errorf("the regular expression is too complex")
		}
		re := regexp.MustCompile(anchored)
		return func(n string, _ []string) bool { return re.MatchString(n) }, nil
	case "topic":
		if !topicPattern.MatchString(value) {
			return nil, fmt.Errorf("%q is not a topic (a-z, 0-9 and -)", value)
		}
		return func(_ string, topics []string) bool { return slices.Contains(topics, value) }, nil
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

// Describe is a rule in words, for the page.
func (r Rule) Describe() string {
	switch r.Kind {
	case "prefix":
		return "name starts with " + r.Value
	case "topic":
		return "topic " + r.Value
	case "glob":
		return "name like " + r.Value
	case "regexp":
		return "name matches " + r.Value
	}
	return r.Value
}

// Derive finds, for each product, the rules its members share: prefixes at the boundaries of their names (after a -, _ or ., or where
// a lower case letter meets an upper case one) and topics. A rule is taken when it covers at least two members not covered yet
// (three in a product of more than eight repositories)
// (three in all for a prefix shorter than five characters, which is too loose to be a pattern) and catches no member of another
// product; it may catch repositories no product holds, which is reported ("also catches"). What no rule covers stays pinned. The
// person's own rules (Product.Rules) come first and are always kept. Hidden repositories are left out of everything.
func Derive(all []ghlogin.Repo, products []Product, hidden []string) []Derived {
	hid := make(map[string]bool, len(hidden))
	for _, h := range hidden {
		hid[h] = true
	}
	var repos []ghlogin.Repo
	index := map[string]int{}
	for _, r := range all {
		if _, dup := index[r.Name]; !dup && !hid[r.Name] {
			index[r.Name] = len(repos)
			repos = append(repos, r)
		}
	}
	holder := map[string]int{} // repository name -> the product that holds it
	for i, p := range products {
		for _, n := range p.Repos {
			if _, ok := index[n]; ok {
				holder[n] = i
			}
		}
	}
	out := make([]Derived, len(products))
	for i, p := range products {
		out[i] = deriveOne(repos, index, holder, i, p)
	}
	return out
}

// hits are the positions of the repositories a matcher selects.
func hits(repos []ghlogin.Repo, fn matchFn) []int {
	var out []int
	for i, r := range repos {
		if fn(r.Name, r.Topics) {
			out = append(out, i)
		}
	}
	return out
}

func deriveOne(repos []ghlogin.Repo, index map[string]int, holder map[string]int, self int, p Product) Derived {
	d := Derived{Name: p.Name, Rules: []Rule{}, Pinned: []string{}}
	var members []string
	isMember := map[string]bool{}
	for _, n := range p.Repos {
		if _, ok := index[n]; ok && !isMember[n] {
			isMember[n] = true
			members = append(members, n)
		}
	}
	covered := map[string]bool{}
	// describe tells what a rule selects: members it covers, other products' members it spills into, repositories of no product.
	describe := func(r Rule, hit []int) Rule {
		for _, i := range hit {
			name := repos[i].Name
			h, held := holder[name]
			switch {
			case held && h == self:
				r.Own++
				covered[name] = true
			case held:
				r.Spill = append(r.Spill, name)
			default:
				r.ExtraN++
				if len(r.Extra) < maxSample {
					r.Extra = append(r.Extra, name)
				}
			}
		}
		r.Spill = r.Spill[:min(len(r.Spill), maxSample)]
		r.Open = r.Kind != "exact"
		return r
	}
	for _, mr := range p.Rules {
		if fn, err := compile(mr.Kind, mr.Value); err == nil { // finish refuses a bad one; the preview skips it
			d.Rules = append(d.Rules, describe(Rule{Kind: mr.Kind, Value: mr.Value, Manual: true}, hits(repos, fn)))
		}
	}

	type option struct {
		rule Rule
		hit  []int
		own  []string
	}
	var options []option
	var found []Rule
	if !p.Fixed {
		found = candidates(repos, index, members)
	}
	for _, r := range found {
		fn, _ := compile(r.Kind, r.Value)
		hit := hits(repos, fn)
		spill := false
		var own []string
		for _, i := range hit {
			h, held := holder[repos[i].Name]
			if held && h != self {
				spill = true
				break
			}
			if held {
				own = append(own, repos[i].Name)
			}
		}
		if !spill && len(own) >= minCover(r) {
			options = append(options, option{r, hit, own})
		}
	}
	// A small product is one a person made on purpose: two repositories that share a start are a pattern. In a big catch-all bucket
	// two are a coincidence, three are not.
	minFresh := 2
	if len(members) > 8 {
		minFresh = 3
	}
	free := func(o option) (fresh, extra int) {
		for _, n := range o.own {
			if !covered[n] {
				fresh++
			}
		}
		for _, i := range o.hit {
			if _, held := holder[repos[i].Name]; !held {
				extra++
			}
		}
		return fresh, extra
	}
	for {
		best, bestFresh, bestExtra := -1, 0, 0
		for k, o := range options {
			fresh, extra := free(o)
			if fresh < minFresh {
				continue
			}
			// The better option covers more new members, then catches fewer strangers, then is the longer (more specific) value.
			if best < 0 || cmp.Or(cmp.Compare(fresh, bestFresh), cmp.Compare(bestExtra, extra), cmp.Compare(len(o.rule.Value), len(options[best].rule.Value)),
				strings.Compare(options[best].rule.Value, o.rule.Value)) > 0 {
				best, bestFresh, bestExtra = k, fresh, extra
			}
		}
		if best < 0 {
			break
		}
		d.Rules = append(d.Rules, describe(options[best].rule, options[best].hit))
		options = slices.Delete(options, best, best+1)
	}
	for _, n := range members {
		if !covered[n] {
			d.Pinned = append(d.Pinned, n)
		}
	}
	slices.Sort(d.Pinned)
	d.Entries, d.TooMany = entries(d.Rules, d.Pinned)
	return d
}

// minCover is how many members a rule has to cover in all.
func minCover(r Rule) int {
	if r.Kind == "prefix" && len(r.Value) < 5 {
		return 3
	}
	return 2
}

// candidates are the rules at least two members share, in a fixed order.
func candidates(repos []ghlogin.Repo, index map[string]int, members []string) []Rule {
	type key struct{ kind, value string }
	count := map[key]int{}
	for _, n := range members {
		seen := map[key]bool{}
		for _, v := range prefixes(n) {
			seen[key{"prefix", v}] = true
		}
		for _, t := range repos[index[n]].Topics {
			if topicPattern.MatchString(t) {
				seen[key{"topic", t}] = true
			}
		}
		for k := range seen {
			count[k]++
		}
	}
	var out []Rule
	for k, c := range count {
		if c >= 2 {
			out = append(out, Rule{Kind: k.kind, Value: k.value})
		}
	}
	slices.SortFunc(out, func(a, b Rule) int { return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Value, b.Value)) })
	return out
}

// prefixes are the starts of a name worth a rule, lower case: up to and with each -, _ or ., and up to where a lower case letter meets
// an upper case one (MessiSDK: messi); at least three characters.
func prefixes(name string) []string {
	lower := strings.ToLower(name)
	var out []string
	for i := 0; i < len(name)-1; i++ {
		switch c := name[i]; {
		case c == '-' || c == '_' || c == '.':
			if i+1 >= 3 {
				out = append(out, lower[:i+1])
			}
		case i >= 3 && unicode.IsLower(rune(name[i-1])) && unicode.IsUpper(rune(c)):
			out = append(out, lower[:i])
		}
	}
	return out
}

// entries are the matchers a product is written with: its rules, then its pinned repositories as exact matchers, or, when that
// would be more than a group holds, as regular expressions of alternated names. tooMany is true when even that is too much.
func entries(rules []Rule, pinned []string) (out []Rule, tooMany bool) {
	for _, r := range rules {
		out = append(out, Rule{Kind: r.Kind, Value: r.Value})
	}
	if len(out)+len(pinned) <= MaxMatchers {
		for _, n := range pinned {
			out = append(out, Rule{Kind: "exact", Value: n})
		}
		return out, false
	}
	var chunk []string
	size := 0
	flush := func() {
		if len(chunk) > 0 {
			out = append(out, Rule{Kind: "regexp", Value: strings.Join(chunk, "|")})
			chunk, size = nil, 0
		}
	}
	for _, n := range pinned {
		q := regexp.QuoteMeta(n)
		if size+len(q)+1 > maxRegexpBytes-16 {
			flush()
		}
		chunk, size = append(chunk, q), size+len(q)+1
	}
	flush()
	return out, len(out) > MaxMatchers
}

// Matcher is the matcher the portal builds for a rule (the page's and the configuration's must select the same repositories; a test
// in cmd/scp holds them to it).
func Matcher(kind, value string) (func(name string, topics []string) bool, error) {
	return compile(kind, value)
}
