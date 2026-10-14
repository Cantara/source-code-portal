package catalog

import (
	"slices"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// GroupCard is the catalogue's own projection of one dashboard card for ONE view class. Everything in it is derived from the members
// the class may see (scm.Membership.VisibleReposOf / DefaultRepoFor), so a response built from it is byte-identical to the response of a
// deployment that tracks only the repositories visible to that class (the count-level enumeration rule, 10-architecture 17.3).
// The generated catalogapi.GroupSummary/GroupDetail are built from it by the handlers; this type has no wire tags on purpose.
type GroupCard struct {
	Meta         scm.GroupMeta
	RepoCount    int
	DefaultRepo  scm.Repository // zero when DefaultState != "ok"
	DefaultState string         // "ok" | "missing" (configured but not tracked, not selected, or not visible to the class)
	Integrations []string       // provider names configured on the group, sorted
	Members      []scm.Slug     // visible members: the default repository first when it resolves, then by slug (getGroup only)
	Freshness    CardFreshness
}

// CardFreshness is what the card's freshness object is derived from (the handler calls Info.Derive(now) and httpapi.NewFreshness).
type CardFreshness struct {
	Kind string // "catalog.stats" of the default repository, else "catalog.repo" of the first visible member
	Info freshness.Info
}

// Groups returns the cards visible to the class in configuration order: a group with no member visible to the class is omitted.
// ok is false before the first publish that carries a membership.
func (sl Slots) Groups(s *readmodel.Snapshot, c scm.ViewClass) (cards []GroupCard, ok bool) {
	m, ok := readmodel.GetGlobal(s, sl.Membership)
	if !ok {
		return nil, false
	}
	for _, g := range m.Groups() {
		if card, ok := sl.card(s, m, g, c, false); ok {
			cards = append(cards, card)
		}
	}
	return cards, true
}

// Group returns one card with its member list; an unknown id and a group with no visible member are both (zero, false): the caller answers
// the same 404 for both, so the response never says whether a group exists only for private members.
func (sl Slots) Group(s *readmodel.Snapshot, c scm.ViewClass, id scm.GroupID) (GroupCard, bool) {
	m, ok := readmodel.GetGlobal(s, sl.Membership)
	if !ok {
		return GroupCard{}, false
	}
	g, ok := m.Group(id)
	if !ok {
		return GroupCard{}, false
	}
	return sl.card(s, m, g, c, true)
}

func (sl Slots) card(s *readmodel.Snapshot, m scm.Membership, g scm.GroupMeta, c scm.ViewClass, members bool) (GroupCard, bool) {
	vis := m.VisibleReposOf(g.ID, c)
	if len(vis) == 0 {
		return GroupCard{}, false
	}
	card := GroupCard{Meta: g, RepoCount: len(vis), DefaultState: "missing"}
	for p := range g.Integrations {
		card.Integrations = append(card.Integrations, p)
	}
	sort.Strings(card.Integrations)
	if d, ok := m.DefaultRepoFor(g.ID, c); ok {
		if v, ok := readmodel.Get(s, readmodel.EntryID(d), sl.Repo); ok {
			card.DefaultState, card.DefaultRepo = "ok", v.V
		}
	}
	switch {
	case card.DefaultState == "ok":
		card.Freshness = CardFreshness{Kind: SlotStats}
		if sv, ok := readmodel.Get(s, readmodel.EntryID(card.DefaultRepo.Slug), sl.Stats); ok {
			card.Freshness.Info = sv.Info
		}
	default:
		card.Freshness = CardFreshness{Kind: SlotRepo}
		if v, ok := readmodel.Get(s, readmodel.EntryID(vis[0]), sl.Repo); ok {
			card.Freshness.Info = v.Info
		}
	}
	if members {
		card.Members = slices.Clone(vis) // VisibleReposOf is shared and read-only
		if card.DefaultState == "ok" {
			card.Members = slices.DeleteFunc(card.Members, func(x scm.Slug) bool { return x == card.DefaultRepo.Slug })
			card.Members = append([]scm.Slug{card.DefaultRepo.Slug}, card.Members...)
		}
	}
	return card, true
}

// MatchesQuery implements `q`: case-insensitive substring over id, display name and description.
func (c GroupCard) MatchesQuery(q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(string(c.Meta.ID)+" "+c.Meta.DisplayName+" "+c.Meta.Description), strings.ToLower(q))
}
