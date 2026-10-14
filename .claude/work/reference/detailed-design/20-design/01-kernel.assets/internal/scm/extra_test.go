package scm_test

import (
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestValidationErrorIsBoundedAndQuoted(t *testing.T) {
	_, err := scm.ParseSlug(strings.Repeat("X", 5000) + "\n[ERROR] forged")
	var ve *scm.ValidationError
	if !errors.As(err, &ve) || !errors.Is(err, scm.ErrInvalid) {
		t.Fatal(err)
	}
	if len(err.Error()) > 200 || strings.Contains(err.Error(), "\n") {
		t.Fatalf("error text must be short and one line: %d %q", len(err.Error()), err.Error())
	}
}

func TestSmallHelpers(t *testing.T) {
	if !(scm.Repository{Visibility: scm.VisPublic}).VisibleTo(scm.ViewPublic) || (scm.Repository{}).VisibleTo(scm.ViewPublic) {
		t.Error("VisibleTo")
	}
	for v, w := range map[scm.Visibility]string{scm.VisUnknown: "unknown", scm.VisPublic: "public", scm.VisInternal: "internal", scm.VisPrivate: "private"} {
		if v.String() != w {
			t.Errorf("%d", v)
		}
		if p, err := scm.ParseVisibility(w); err != nil || p != v {
			t.Errorf("ParseVisibility(%q)", w)
		}
	}
	if (scm.Page[int]{}).HasNext() || !(scm.Page[int]{Next: "x"}).HasNext() {
		t.Error("HasNext")
	}
	if !(scm.Validators{}).IsZero() || (scm.Validators{SHA: "a"}).IsZero() {
		t.Error("Validators.IsZero")
	}
	for et, w := range map[scm.EntryType]string{scm.EntryUnknown: "unknown", scm.EntryBlob: "blob", scm.EntryTree: "tree", scm.EntryCommit: "commit", scm.EntryLink: "link"} {
		if et.String() != w {
			t.Errorf("%d", et)
		}
	}
	if !(scm.RepoRef{}).IsZero() || (scm.RepoRef{Name: "x"}).IsZero() {
		t.Error("RepoRef.IsZero")
	}
	if got := (scm.RepoRef{Source: "a", Owner: "o", Name: "n"}).String(); got != "a/o/n" {
		t.Error(got)
	}
}

func TestEventLogValueOmitsPaths(t *testing.T) {
	ev := scm.Event{Kind: scm.EventPush, Delivery: "d", Repo: scm.RepoRef{Source: "a", Owner: "O", Name: "N"}, Ref: "refs/heads/main", CommitCount: 2, ChangedPaths: []string{"secret/path", "b"}}
	v := ev.LogValue()
	if v.Kind() != slog.KindGroup {
		t.Fatal(v.Kind())
	}
	got := map[string]string{}
	for _, a := range v.Group() {
		got[a.Key] = a.Value.String()
	}
	if got["repo"] != "a/o/n" || got["paths"] != "2" || got["kind"] != "push" || strings.Contains(v.String(), "secret/path") {
		t.Fatalf("%v", got)
	}
}

// TestMembershipAgainstNaiveModel compares every accessor with a brute-force computation on random catalogues.
func TestMembershipAgainstNaiveModel(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	vis := []scm.Visibility{scm.VisPublic, scm.VisPublic, scm.VisInternal, scm.VisPrivate, scm.VisUnknown}
	for round := 0; round < 200; round++ {
		n, ng := 1+rng.IntN(40), 1+rng.IntN(6)
		var sp scm.MembershipSpec
		for i := 0; i < n; i++ {
			s := scm.Slug("r" + string(rune('a'+i/26)) + string(rune('a'+i%26)))
			sp.Repos = append(sp.Repos, scm.MemberRepo{Slug: s, Ref: scm.RepoRef{Source: "acme", Owner: "o", Name: string(s)}, Visibility: vis[rng.IntN(len(vis))]})
		}
		naive := map[scm.GroupID][]scm.Slug{}
		for g := 0; g < ng; g++ {
			id := scm.GroupID("g" + string(rune('a'+g)))
			gs := scm.GroupSpec{Meta: scm.GroupMeta{ID: id}}
			for _, i := range rng.Perm(n)[:rng.IntN(n+1)] {
				gs.Members = append(gs.Members, sp.Repos[i].Slug)
			}
			if len(gs.Members) > 0 && rng.IntN(2) == 0 {
				gs.Default = gs.Members[rng.IntN(len(gs.Members))]
			}
			naive[id] = gs.Members
			sp.Groups = append(sp.Groups, gs)
		}
		m, err := scm.NewMembership(sp)
		if err != nil {
			t.Fatal(err)
		}
		visOf := map[scm.Slug]scm.Visibility{}
		for _, r := range sp.Repos {
			visOf[r.Slug] = r.Visibility
		}
		for _, gs := range sp.Groups {
			for _, c := range []scm.ViewClass{scm.ViewPublic, scm.ViewAll} {
				var want []scm.Slug
				for _, s := range gs.Members {
					if visOf[s].Visible(c) {
						want = append(want, s)
					}
				}
				if got := m.VisibleReposOf(gs.Meta.ID, c); !slices.Equal(got, want) {
					t.Fatalf("round %d group %s class %d: %v want %v", round, gs.Meta.ID, c, got, want)
				}
				d, ok := m.DefaultRepoFor(gs.Meta.ID, c)
				wantOK := gs.Default != "" && visOf[gs.Default].Visible(c)
				if ok != wantOK || (ok && d != gs.Default) {
					t.Fatalf("default of %s for class %d: %q %v", gs.Meta.ID, c, d, ok)
				}
			}
		}
		for _, r := range sp.Repos {
			var want []scm.GroupID
			for _, gs := range sp.Groups {
				if slices.Contains(gs.Members, r.Slug) {
					want = append(want, gs.Meta.ID)
				}
			}
			if got := m.GroupsOf(r.Slug); !slices.Equal(got, want) {
				t.Fatalf("GroupsOf(%s) = %v want %v", r.Slug, got, want)
			}
		}
		pub := 0
		for _, r := range sp.Repos {
			if r.Visibility == scm.VisPublic {
				pub++
			}
		}
		if m.Len(scm.ViewPublic) != pub || m.Len(scm.ViewAll) != n || len(m.Slugs(scm.ViewPublic)) != pub {
			t.Fatalf("counts")
		}
		if g, ok := m.Group(sp.Groups[0].Meta.ID); !ok || g.ID != sp.Groups[0].Meta.ID {
			t.Fatal("Group")
		}
		if _, ok := m.Group("nope"); ok {
			t.Fatal("unknown group")
		}
	}
}

func TestValidationErrorValueIsCutAtConstruction(t *testing.T) {
	_, err := scm.ParseSlug(strings.Repeat("é", 500))
	var ve *scm.ValidationError
	if !errors.As(err, &ve) || len(ve.Value) > 70 || !strings.HasSuffix(ve.Value, "...") || !utf8.ValidString(ve.Value) {
		t.Fatalf("Value must be cut to 64 bytes plus an ellipsis and stay valid UTF-8: %d %q", len(ve.Value), ve.Value)
	}
}

func TestSlugForRefusesAReusedName(t *testing.T) {
	// V-04: repository 1 was renamed or deleted on GitHub and a different repository (id 2) now has its old name. A push
	// to the new one must not resolve to the catalogue entry of the old one before the reconcile has caught up.
	r := repo("old", "acme", "old", scm.VisPublic)
	r.Ref.ID = 1
	unknownID := repo("archive", "acme", "archive", scm.VisPublic) // catalogue entry whose id is not known
	m, err := scm.NewMembership(scm.MembershipSpec{Repos: []scm.MemberRepo{r, unknownID}})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := m.SlugFor(scm.RepoRef{Source: "acme", ID: 2, Owner: "acme", Name: "Old"}); ok {
		t.Fatalf("a different repository with the same name resolved to %q", s)
	}
	if s, ok := m.SlugFor(scm.RepoRef{Source: "acme", ID: 1, Owner: "acme", Name: "renamed"}); !ok || s != "old" {
		t.Fatalf("same id after a rename must resolve: %q %v", s, ok)
	}
	if s, ok := m.SlugFor(scm.RepoRef{Source: "acme", Owner: "ACME", Name: "OLD"}); !ok || s != "old" {
		t.Fatalf("a ref without an id resolves by the folded key: %q %v", s, ok)
	}
	if s, ok := m.SlugFor(scm.RepoRef{Source: "acme", ID: 9, Owner: "acme", Name: "archive"}); !ok || s != "archive" {
		t.Fatalf("a catalogue entry with an unknown id still resolves by key: %q %v", s, ok)
	}
}

func TestMembershipRejectsInvalidRef(t *testing.T) {
	for name, ref := range map[string]scm.RepoRef{
		"owner with slash": {Source: "acme", Owner: "a/b", Name: "n"},
		"empty owner":      {Source: "acme", Name: "n"},
		"empty name":       {Source: "acme", Owner: "o"},
		"bad source":       {Source: "ACME", Owner: "o", Name: "n"},
		"zero":             {},
	} {
		_, err := scm.NewMembership(scm.MembershipSpec{Repos: []scm.MemberRepo{{Slug: "x", Ref: ref}}})
		if !errors.Is(err, scm.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

func TestCleanLineVariationSelectorsAndIgnorables(t *testing.T) {
	cases := map[string]string{
		"a\uFE00\uFE01\uFE02b":   "ab",               // variation-selector smuggling channel
		"a\uFE0Fb":               "a\uFE0Fb",         // emoji presentation after a visible base is kept
		"\U0001F44D\uFE0F":       "\U0001F44D\uFE0F", // thumbs up, emoji presentation
		"\uFE0Fa":                "a",                // no base
		"a \uFE0Fb":              "a b",              // after a space
		"a\uFE0F\uFE0F\uFE0Fb":   "a\uFE0Fb",         // only one selector per base
		"a\u200b\uFE0Fb":         "a\uFE0Fb",         // dropped format characters do not become a base
		"x\u3164y\uFFA0z\u034Fw": "xyzw",             // Hangul fillers and the grapheme joiner are default-ignorable
		"x\u115F\u1160\u17B4y":   "xy",
		"\u180Bx\u180Dy":         "xy", // Mongolian free variation selectors
		"a\u200cb\u200dc":        "a\u200cb\u200dc",
	}
	for in, want := range cases {
		if got := scm.CleanLine(in, 0); got != want {
			t.Errorf("CleanLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventLogValueZeroRepo(t *testing.T) {
	v := scm.Event{Kind: scm.EventInstallation}.LogValue()
	for _, a := range v.Group() {
		if a.Key == "repo" && a.Value.String() != "" {
			t.Errorf("a zero repo logs as %q, want empty", a.Value.String())
		}
	}
}
