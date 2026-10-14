package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Compiled is a validated Config with every matcher compiled. Immutable; safe for concurrent use.
type Compiled struct {
	cfg    Config
	groups []compiledGroup
}

type compiledGroup struct {
	cfg     GroupConfig
	ms      []CompiledMatcher
	sources map[scm.SourceID]bool // nil = every source
}

// Compile compiles every matcher. Callers run Config.Validate first; Compile returns the first matcher error if they did not.
func Compile(cfg Config) (*Compiled, error) {
	c := &Compiled{cfg: cfg}
	for i, g := range cfg.Groups {
		cg := compiledGroup{cfg: g}
		for j, m := range g.Match {
			cm, err := m.Compile()
			if err != nil {
				return nil, fmt.Errorf("groups[%d].match[%d]: %w", i, j, err)
			}
			cg.ms = append(cg.ms, cm)
		}
		if len(g.Sources) > 0 {
			cg.sources = map[scm.SourceID]bool{}
			for _, s := range g.Sources {
				cg.sources[s] = true
			}
		}
		c.groups = append(c.groups, cg)
	}
	return c, nil
}

// Matches reports whether group i selects the repository (a repository may be selected by any number of groups).
func (c *Compiled) Matches(i int, r scm.Repository) bool {
	g := c.groups[i]
	if g.sources != nil && !g.sources[r.Ref.Source] {
		return false
	}
	for _, m := range g.ms {
		if m.Match(r.Ref.Name, r.Topics) {
			return true
		}
	}
	return false
}

// Grouped reports whether at least one group selects the repository.
func (c *Compiled) Grouped(r scm.Repository) bool {
	for i := range c.groups {
		if c.Matches(i, r) {
			return true
		}
	}
	return false
}

// GroupHealth is the per-group outcome of BuildMembership, shown in /status and in the metrics.
type GroupHealth struct {
	ID            scm.GroupID
	Members       int
	DefaultRepo   scm.Slug // "" when unresolved
	DefaultReason string   // "", "missing" (no tracked repository has that slug) or "not_selected" (tracked, but this group's matchers do not select it)
}

// MembershipResult is what BuildMembership returns besides the scm.Membership itself.
type MembershipResult struct {
	Membership scm.Membership
	Groups     []GroupHealth
	Multi      map[scm.Slug][]scm.GroupID // repositories selected by more than one group
	Ungrouped  []scm.Slug
	// TrackedRef is the effective tracked ref of every repository with a group `ref` override (first group in config order wins).
	TrackedRef   map[scm.Slug]string
	RefConflicts map[scm.Slug][]string // groups disagree on the ref; the first wins
}

// BuildMembership computes the membership index from the tracked repositories (Slug set). Deterministic: members are sorted by
// folded slug, groups keep configuration order, and the result does not depend on the order of repos.
func (c *Compiled) BuildMembership(repos []scm.Repository) (MembershipResult, error) {
	rs := append([]scm.Repository(nil), repos...)
	sort.SliceStable(rs, func(i, j int) bool { return FoldSlug(rs[i].Slug) < FoldSlug(rs[j].Slug) })
	res := MembershipResult{Multi: map[scm.Slug][]scm.GroupID{}, TrackedRef: map[scm.Slug]string{}, RefConflicts: map[scm.Slug][]string{}}
	metas := make([]scm.GroupMeta, len(c.groups))
	members := map[scm.GroupID][]scm.Slug{}
	defaults := map[scm.GroupID]scm.Slug{}
	tracked := map[string]bool{} // folded GitHub repository names of every tracked repository (default_repo is a repository NAME, not a slug)
	slugOf := map[scm.Slug]scm.Repository{}
	for _, r := range rs {
		tracked[strings.ToLower(r.Ref.Name)] = true
		slugOf[r.Slug] = r
	}
	for gi, g := range c.groups {
		metas[gi] = g.cfg.Meta()
		members[g.cfg.ID] = []scm.Slug{}
	}
	for _, r := range rs {
		var in []scm.GroupID
		for gi, g := range c.groups {
			if !c.Matches(gi, r) {
				continue
			}
			in = append(in, g.cfg.ID)
			members[g.cfg.ID] = append(members[g.cfg.ID], r.Slug)
			if g.cfg.Ref != "" {
				if cur, ok := res.TrackedRef[r.Slug]; !ok {
					res.TrackedRef[r.Slug] = g.cfg.Ref
				} else if cur != g.cfg.Ref {
					res.RefConflicts[r.Slug] = append(res.RefConflicts[r.Slug], string(g.cfg.ID))
				}
			}
		}
		switch {
		case len(in) == 0:
			res.Ungrouped = append(res.Ungrouped, r.Slug)
		case len(in) > 1:
			res.Multi[r.Slug] = in
		}
	}
	for _, g := range c.groups {
		h := GroupHealth{ID: g.cfg.ID, Members: len(members[g.cfg.ID])}
		want := strings.ToLower(g.cfg.DefaultRepo)
		for _, s := range members[g.cfg.ID] { // members are in slug order: with two members of that name the smaller slug wins
			if strings.ToLower(slugOf[s].Ref.Name) == want {
				h.DefaultRepo = s
				defaults[g.cfg.ID] = s
				break
			}
		}
		if h.DefaultRepo == "" {
			if tracked[want] {
				h.DefaultReason = "not_selected"
			} else {
				h.DefaultReason = "missing"
			}
		}
		res.Groups = append(res.Groups, h)
	}
	spec := scm.MembershipSpec{Repos: make([]scm.MemberRepo, 0, len(rs)), Groups: make([]scm.GroupSpec, 0, len(c.groups))}
	for _, r := range rs {
		spec.Repos = append(spec.Repos, scm.MemberRepo{Slug: r.Slug, Ref: r.Ref, Visibility: r.Visibility})
	}
	for gi, g := range c.groups {
		spec.Groups = append(spec.Groups, scm.GroupSpec{Meta: metas[gi], Members: members[g.cfg.ID], Default: defaults[g.cfg.ID]})
	}
	m, err := scm.NewMembership(spec) // validates slug grammar, duplicate (source, id) and every group rule; joined errors
	if err != nil {
		return MembershipResult{}, fmt.Errorf("catalog: membership: %w", err)
	}
	res.Membership = m
	return res, nil
}
