package commits

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

// testMembership builds the real scm.Membership from the repositories and (id -> members) groups, in the given order.
func testMembership(t testing.TB, repos []scm.Repository, order []scm.GroupID, members map[scm.GroupID][]scm.Slug) scm.Membership {
	t.Helper()
	spec := scm.MembershipSpec{}
	for _, r := range repos {
		spec.Repos = append(spec.Repos, scm.MemberRepo{Slug: r.Slug, Ref: r.Ref, Visibility: r.Visibility})
	}
	for _, id := range order {
		spec.Groups = append(spec.Groups, scm.GroupSpec{Meta: scm.GroupMeta{ID: id}, Members: members[id]})
	}
	m, err := scm.NewMembership(spec)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
