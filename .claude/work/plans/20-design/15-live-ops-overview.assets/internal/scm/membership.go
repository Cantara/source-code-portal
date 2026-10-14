package scm

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// GroupMeta is the display and defaults record of a group. The Integrations maps are read-only for consumers.
// Merge order for an integration key: descriptor, then the first group (config order) that defines it, then the
// instance default.
type GroupMeta struct {
	ID           GroupID
	DisplayName  string
	Description  string
	Integrations map[string]map[string]string // provider -> key -> value
}

// Integration returns one group default.
func (g GroupMeta) Integration(provider, key string) (string, bool) {
	v, ok := g.Integrations[provider][key]
	return v, ok
}

// Clone deep-copies the maps.
func (g GroupMeta) Clone() GroupMeta {
	if g.Integrations != nil {
		m := make(map[string]map[string]string, len(g.Integrations))
		for p, kv := range g.Integrations {
			m[p] = maps.Clone(kv)
		}
		g.Integrations = m
	}
	return g
}

// MemberRepo is one catalogue entry as Membership needs it.
type MemberRepo struct {
	Slug       Slug
	Ref        RepoRef
	Visibility Visibility
}

// GroupSpec is one group and its members, in the order the caller wants them served.
type GroupSpec struct {
	Meta    GroupMeta
	Members []Slug
	Default Slug // "" when the group has no default; otherwise must be one of Members
}

// MembershipSpec is the input of NewMembership. Groups are in config order.
type MembershipSpec struct {
	Repos  []MemberRepo
	Groups []GroupSpec
}

type groupData struct {
	meta     GroupMeta
	all      []Slug
	public   []Slug // aliases all when every member is public
	dflt     Slug
	dfltVis  Visibility
	groupIdx int
}

// Membership is the immutable many-to-many index between repositories and groups plus the slug directory of the
// catalogue. The zero value is an empty, usable membership. Returned slices are shared and read-only.
type Membership struct {
	order    []GroupMeta
	groups   map[GroupID]*groupData
	groupsOf map[Slug][]GroupID
	bySlug   map[Slug]MemberRepo
	byKey    map[string]Slug
	allCount int
	pubCount int
}

// NewMembership validates and builds the index. It copies everything it keeps. Errors (all violations, joined):
// invalid or duplicate slug, duplicate repository key, duplicate or invalid group id, member unknown, member listed
// twice in one group, default not a member.
func NewMembership(spec MembershipSpec) (Membership, error) {
	var errs []error
	m := Membership{
		groups:   make(map[GroupID]*groupData, len(spec.Groups)),
		groupsOf: make(map[Slug][]GroupID, len(spec.Repos)),
		bySlug:   make(map[Slug]MemberRepo, len(spec.Repos)),
		byKey:    make(map[string]Slug, len(spec.Repos)),
	}
	for _, r := range spec.Repos {
		if _, err := ParseSlug(string(r.Slug)); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, dup := m.bySlug[r.Slug]; dup {
			errs = append(errs, invalid("slug", string(r.Slug), "duplicate slug"))
			continue
		}
		k := r.Ref.Key()
		if other, dup := m.byKey[k]; dup {
			errs = append(errs, fmt.Errorf("scm: repository %q has two slugs: %q and %q", k, other, r.Slug))
			continue
		}
		m.bySlug[r.Slug] = r
		m.byKey[k] = r.Slug
		m.allCount++
		if r.Visibility == VisPublic {
			m.pubCount++
		}
	}
	for i, g := range spec.Groups {
		if _, err := ParseGroupID(string(g.Meta.ID)); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, dup := m.groups[g.Meta.ID]; dup {
			errs = append(errs, invalid("group id", string(g.Meta.ID), "duplicate group id"))
			continue
		}
		gd := &groupData{meta: g.Meta.Clone(), groupIdx: i}
		seen := make(map[Slug]bool, len(g.Members))
		allPublic := true
		for _, s := range g.Members {
			r, ok := m.bySlug[s]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("scm: group %q lists unknown repository %q", g.Meta.ID, s))
				continue
			case seen[s]:
				errs = append(errs, fmt.Errorf("scm: group %q lists %q twice", g.Meta.ID, s))
				continue
			}
			seen[s] = true
			gd.all = append(gd.all, s)
			if r.Visibility != VisPublic {
				allPublic = false
			}
			m.groupsOf[s] = append(m.groupsOf[s], g.Meta.ID)
		}
		if allPublic {
			gd.public = gd.all
		} else {
			for _, s := range gd.all {
				if m.bySlug[s].Visibility == VisPublic {
					gd.public = append(gd.public, s)
				}
			}
		}
		if g.Default != "" {
			if !seen[g.Default] {
				errs = append(errs, fmt.Errorf("scm: group %q default repository %q is not a member", g.Meta.ID, g.Default))
			} else {
				gd.dflt, gd.dfltVis = g.Default, m.bySlug[g.Default].Visibility
			}
		}
		m.groups[g.Meta.ID] = gd
		m.order = append(m.order, gd.meta)
	}
	if len(errs) > 0 {
		return Membership{}, joinErrors(errs)
	}
	return m, nil
}

func joinErrors(errs []error) error { return errors.Join(errs...) }

// Groups lists every group in config order (all groups are listed to every class: group existence is configuration).
func (m Membership) Groups() []GroupMeta { return m.order }

// Group looks a group up.
func (m Membership) Group(g GroupID) (GroupMeta, bool) {
	gd, ok := m.groups[g]
	if !ok {
		return GroupMeta{}, false
	}
	return gd.meta, true
}

// GroupsOf lists the groups of a repository in config order (nil when it is in none).
func (m Membership) GroupsOf(s Slug) []GroupID { return m.groupsOf[s] }

// ReposOf lists a group's members as the builder ordered them (catalog: by slug). Equivalent to VisibleReposOf(g, ViewAll).
func (m Membership) ReposOf(g GroupID) []Slug { return m.VisibleReposOf(g, ViewAll) }

// VisibleReposOf lists the members of a group visible to a class. A class other than ViewAll sees public members only.
func (m Membership) VisibleReposOf(g GroupID, c ViewClass) []Slug {
	gd, ok := m.groups[g]
	if !ok {
		return nil
	}
	if c == ViewAll {
		return gd.all
	}
	return gd.public
}

// DefaultRepo returns the group's default repository (ViewAll semantics).
func (m Membership) DefaultRepo(g GroupID) (Slug, bool) { return m.DefaultRepoFor(g, ViewAll) }

// DefaultRepoFor returns the default repository when it exists and is visible to the class.
func (m Membership) DefaultRepoFor(g GroupID, c ViewClass) (Slug, bool) {
	gd, ok := m.groups[g]
	if !ok || gd.dflt == "" || !gd.dfltVis.Visible(c) {
		return "", false
	}
	return gd.dflt, true
}

// SlugFor resolves a repository ref (case-insensitive on owner and name) to its portal slug.
func (m Membership) SlugFor(ref RepoRef) (Slug, bool) { s, ok := m.byKey[ref.Key()]; return s, ok }

// Repo returns the catalogue entry of a slug.
func (m Membership) Repo(s Slug) (MemberRepo, bool) { r, ok := m.bySlug[s]; return r, ok }

// Len counts repositories visible to a class.
func (m Membership) Len(c ViewClass) int {
	if c == ViewAll {
		return m.allCount
	}
	return m.pubCount
}

// Slugs lists every slug visible to the class, sorted. It allocates; use it for generators, not per request.
func (m Membership) Slugs(c ViewClass) []Slug {
	out := make([]Slug, 0, m.Len(c))
	for s, r := range m.bySlug {
		if r.Visibility.Visible(c) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}
