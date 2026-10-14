package scm_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

func repo(slug, owner, name string, v scm.Visibility) scm.MemberRepo {
	return scm.MemberRepo{Slug: scm.Slug(slug), Ref: scm.RepoRef{Source: "acme", Owner: owner, Name: name}, Visibility: v}
}

func spec() scm.MembershipSpec {
	return scm.MembershipSpec{
		Repos: []scm.MemberRepo{
			repo("a", "acme", "A", scm.VisPublic), repo("b", "acme", "b", scm.VisPublic),
			repo("secret", "acme", "secret", scm.VisPrivate), repo("loose", "acme", "loose", scm.VisPublic),
		},
		Groups: []scm.GroupSpec{
			{Meta: scm.GroupMeta{ID: "g1", DisplayName: "G1", Integrations: map[string]map[string]string{"jenkins": {"job_prefix": "x-"}}},
				Members: []scm.Slug{"a", "b", "secret"}, Default: "a"},
			{Meta: scm.GroupMeta{ID: "g2"}, Members: []scm.Slug{"b", "secret"}, Default: "secret"},
			{Meta: scm.GroupMeta{ID: "empty"}},
		},
	}
}

func TestMembership(t *testing.T) {
	m, err := scm.NewMembership(spec())
	if err != nil {
		t.Fatal(err)
	}
	if got := m.GroupsOf("b"); !slices.Equal(got, []scm.GroupID{"g1", "g2"}) {
		t.Fatalf("GroupsOf(b) = %v (config order, many to many)", got)
	}
	if m.GroupsOf("loose") != nil {
		t.Fatal("a repository in no group")
	}
	if got := m.ReposOf("g1"); !slices.Equal(got, []scm.Slug{"a", "b", "secret"}) {
		t.Fatal(got)
	}
	if got := m.VisibleReposOf("g1", scm.ViewPublic); !slices.Equal(got, []scm.Slug{"a", "b"}) {
		t.Fatalf("public class must not see the private member: %v", got)
	}
	if m.Len(scm.ViewPublic) != 3 || m.Len(scm.ViewAll) != 4 {
		t.Fatal("Len per class")
	}
	if s, ok := m.DefaultRepoFor("g2", scm.ViewPublic); ok {
		t.Fatalf("a private default repository must not be offered to the public class: %q", s)
	}
	if s, ok := m.DefaultRepo("g2"); !ok || s != "secret" {
		t.Fatal("default for ViewAll")
	}
	if _, ok := m.DefaultRepo("empty"); ok {
		t.Fatal("no default")
	}
	if s, ok := m.SlugFor(scm.RepoRef{Source: "acme", Owner: "ACME", Name: "a"}); !ok || s != "a" {
		t.Fatal("SlugFor must fold case")
	}
	var ids []scm.GroupID
	for _, g := range m.Groups() {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []scm.GroupID{"g1", "g2", "empty"}) {
		t.Fatal("Groups() is config order and lists every group to every class")
	}
	if v, ok := m.Groups()[0].Integration("jenkins", "job_prefix"); !ok || v != "x-" {
		t.Fatal("integration default")
	}
	if got := m.Slugs(scm.ViewPublic); !slices.Equal(got, []scm.Slug{"a", "b", "loose"}) {
		t.Fatal(got)
	}
}

func TestMembershipCopiesItsInput(t *testing.T) {
	sp := spec()
	m, _ := scm.NewMembership(sp)
	sp.Groups[0].Meta.Integrations["jenkins"]["job_prefix"] = "MUTATED"
	if v, _ := m.Groups()[0].Integration("jenkins", "job_prefix"); v != "x-" {
		t.Fatal("Membership kept the caller's map")
	}
}

func TestMembershipZeroValueIsUsable(t *testing.T) {
	var m scm.Membership
	if m.GroupsOf("x") != nil || m.ReposOf("g") != nil || m.Len(scm.ViewAll) != 0 || len(m.Groups()) != 0 || len(m.Slugs(scm.ViewAll)) != 0 {
		t.Fatal("zero membership")
	}
	if _, ok := m.SlugFor(scm.RepoRef{}); ok {
		t.Fatal("zero")
	}
}

func TestMembershipRejects(t *testing.T) {
	mut := func(f func(*scm.MembershipSpec)) error {
		sp := spec()
		f(&sp)
		_, err := scm.NewMembership(sp)
		return err
	}
	cases := map[string]func(*scm.MembershipSpec){
		"duplicate slug":     func(s *scm.MembershipSpec) { s.Repos = append(s.Repos, repo("a", "acme", "other", scm.VisPublic)) },
		"duplicate repo":     func(s *scm.MembershipSpec) { s.Repos = append(s.Repos, repo("a2", "ACME", "a", scm.VisPublic)) },
		"reserved slug":      func(s *scm.MembershipSpec) { s.Repos[0].Slug = "ext" },
		"unknown member":     func(s *scm.MembershipSpec) { s.Groups[0].Members = append(s.Groups[0].Members, "ghost") },
		"member twice":       func(s *scm.MembershipSpec) { s.Groups[0].Members = append(s.Groups[0].Members, "a") },
		"default not member": func(s *scm.MembershipSpec) { s.Groups[0].Default = "loose" },
		"duplicate group":    func(s *scm.MembershipSpec) { s.Groups = append(s.Groups, s.Groups[0]) },
		"bad group id":       func(s *scm.MembershipSpec) { s.Groups[0].Meta.ID = "G 1" },
	}
	for name, f := range cases {
		if err := mut(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// all violations are reported together
	err := mut(func(s *scm.MembershipSpec) {
		s.Groups[0].Members = append(s.Groups[0].Members, "ghost1", "ghost2")
	})
	if err == nil || !strings.Contains(err.Error(), "ghost1") || !strings.Contains(err.Error(), "ghost2") {
		t.Fatalf("want both violations: %v", err)
	}
}

func TestSlugForMatchesByIDAfterARename(t *testing.T) {
	r := repo("a", "acme", "old-name", scm.VisPublic)
	r.Ref.ID = 42
	m, err := scm.NewMembership(scm.MembershipSpec{Repos: []scm.MemberRepo{r}})
	if err != nil {
		t.Fatal(err)
	}
	renamed := scm.RepoRef{Source: "acme", ID: 42, Owner: "acme", Name: "new-name"}
	if s, ok := m.SlugFor(renamed); !ok || s != "a" {
		t.Fatalf("a webhook after a rename must still resolve: %v %v", s, ok)
	}
	if got, _ := m.Repo("a"); got.Ref.Name != "old-name" {
		t.Fatal("the catalogue's own ref is what job keys are built from")
	}
	otherSource := scm.RepoRef{Source: "ghes", ID: 42, Owner: "acme", Name: "new-name"}
	if _, ok := m.SlugFor(otherSource); ok {
		t.Fatal("the same numeric id in another source is another repository")
	}
	dup := r
	dup.Slug, dup.Ref.Name = "b", "other"
	if _, err := scm.NewMembership(scm.MembershipSpec{Repos: []scm.MemberRepo{r, dup}}); err == nil {
		t.Fatal("two slugs for one (source, id) must be rejected")
	}
}
