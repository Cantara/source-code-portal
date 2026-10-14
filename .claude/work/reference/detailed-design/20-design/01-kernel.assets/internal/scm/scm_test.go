package scm_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestSlug(t *testing.T) {
	ok := map[string]string{"auth-service": "auth-service", "Auth-Service": "auth-service", ".github": ".github", "three.js": "three.js", "a_b": "a_b", "X": "x"}
	for in, want := range ok {
		got, err := scm.SlugFromName(in)
		if err != nil || string(got) != want {
			t.Errorf("SlugFromName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", ".", "..", "a/b", "a b", "ü", "a?b", "ext", "Ext", "groups", "avatars", strings.Repeat("a", 101), "a%2Fb", "a\x00b", "a\\b"}
	for _, in := range bad {
		_, err := scm.SlugFromName(in)
		if !errors.Is(err, scm.ErrInvalid) {
			t.Errorf("SlugFromName(%q) = %v, want ErrInvalid", in, err)
		}
	}
	if _, err := scm.ParseSlug("Auth"); err == nil {
		t.Error("ParseSlug must not fold case")
	}
}

func TestGroupID(t *testing.T) {
	for _, in := range []string{"platform", "core-libs", "iot", "a1"} {
		got, err := scm.ParseGroupID(in)
		if err != nil || string(got) != in {
			t.Errorf("ParseGroupID(%q)=%q,%v", in, got, err)
		}
	}
	for _, in := range []string{"", "-a", "a b", "a_b", "A", strings.Repeat("a", 64)} {
		if _, err := scm.ParseGroupID(in); err == nil {
			t.Errorf("ParseGroupID(%q) accepted", in)
		}
	}
}

func TestRepoKeyRoundTripAndFolding(t *testing.T) {
	ref := scm.RepoRef{Source: "acme", ID: 7, Owner: "Acme-Labs", Name: "Auth-Service"}
	if ref.Key() != "acme/acme-labs/auth-service" {
		t.Fatal(ref.Key())
	}
	got, err := scm.ParseRepoKey(ref.Key())
	if err != nil || got.Key() != ref.Key() || got.ID != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if !ref.SameRepo(scm.RepoRef{Source: "acme", Owner: "acme-labs", Name: "AUTH-service"}) {
		t.Error("case-insensitive SameRepo")
	}
	if ref.SameRepo(scm.RepoRef{Source: "acme", ID: 8, Owner: "acme-labs", Name: "auth-service"}) {
		t.Error("different non-zero IDs are different repositories even with equal names (rename and re-create)")
	}
	if got := strings.Join(ref.KeyParts(), "/"); got != ref.Key() || len(ref.KeyParts()) != 3 {
		t.Errorf("KeyParts %v must join to Key %q", ref.KeyParts(), ref.Key())
	}
	for _, bad := range []string{"", "a/b", "a/b/c/d", "A/b/c", "acme//c", "acme/b/..", "acme/b/c d", "acme/b/c\x00"} {
		if _, err := scm.ParseRepoKey(bad); err == nil {
			t.Errorf("ParseRepoKey(%q) accepted", bad)
		}
	}
	if err := (scm.RepoRef{Source: "acme", Owner: ".github", Name: ".github"}).Validate(); err == nil {
		t.Error("owner may not start with a dot")
	}
	if err := (scm.RepoRef{Source: "acme", Owner: "o", Name: ".github"}).Validate(); err != nil {
		t.Errorf(".github is a legal repository name: %v", err)
	}
}

func TestVisibilityZeroValueIsNotPublic(t *testing.T) {
	var v scm.Visibility
	if v.Visible(scm.ViewPublic) || !v.Visible(scm.ViewAll) {
		t.Fatal("the zero Visibility must be hidden from the public class")
	}
	var c scm.ViewClass
	if c != scm.ViewPublic {
		t.Fatal("the zero ViewClass must be the least privileged")
	}
	if !scm.VisPublic.Visible(scm.ViewPublic) || scm.VisInternal.Visible(scm.ViewPublic) || scm.VisPrivate.Visible(scm.ViewPublic) {
		t.Fatal("visibility matrix")
	}
	b, _ := json.Marshal(struct{ V scm.Visibility }{scm.VisPrivate})
	if string(b) != `{"V":"private"}` {
		t.Fatal(string(b))
	}
	var back struct{ V scm.Visibility }
	if err := json.Unmarshal([]byte(`{"V":"secret"}`), &back); err == nil {
		t.Fatal("unknown visibility must not decode to public")
	}
	if err := json.Unmarshal([]byte(`{"V":""}`), &back); err == nil {
		t.Fatal("empty visibility must not decode")
	}
}

func TestClassed(t *testing.T) {
	c := scm.Classed[int]{Public: 1, All: 2}
	if c.For(scm.ViewPublic) != 1 || c.For(scm.ViewAll) != 2 || c.For(scm.ViewClass(9)) != 1 {
		t.Fatal("For")
	}
	if s := scm.Same(5); s.For(scm.ViewPublic) != 5 || s.For(scm.ViewAll) != 5 {
		t.Fatal("Same")
	}
}

func TestTree(t *testing.T) {
	es := []scm.TreeEntry{{Path: "docs/b.md"}, {Path: "README.md"}, {Path: "docs/a.md"}, {Path: "docs"}, {Path: "docs2/x"}, {Path: "README.md", SHA: "dup"}}
	tr := scm.NewTree("abc", es, true)
	if tr.Len() != 5 || !tr.Truncated() || tr.SHA() != "abc" {
		t.Fatalf("%d", tr.Len())
	}
	var all []string
	for e := range tr.All() {
		all = append(all, e.Path)
	}
	if !slices.Equal(all, []string{"README.md", "docs", "docs/a.md", "docs/b.md", "docs2/x"}) {
		t.Fatal(all)
	}
	if e, ok := tr.Lookup("README.md"); !ok || e.SHA != "" {
		t.Fatalf("first duplicate wins: %+v", e)
	}
	var under []string
	for e := range tr.Under("docs") {
		under = append(under, e.Path)
	}
	if !slices.Equal(under, []string{"docs/a.md", "docs/b.md"}) {
		t.Fatalf("Under(docs) = %v (must exclude docs and docs2)", under)
	}
	if n := 0; func() int {
		for range tr.Under("") {
			n++
		}
		return n
	}() != 5 {
		t.Fatal("Under(\"\") is everything")
	}
	if _, ok := tr.Lookup("nope"); ok {
		t.Fatal("lookup")
	}
}

func TestCleanLine(t *testing.T) {
	cases := map[string]string{
		"plain":                  "plain",
		"  a \t b\n\nc  ":        "a b c",
		"a‮b":                    "ab",                    // RLO dropped
		"a⁦b⁩":                   "ab",                    // isolates dropped
		"a​b":                    "ab",                    // ZWSP dropped
		"x\U000E0041\U000E0042y": "xy",                    // tag characters (ASCII smuggling)
		"x\U000E0100y":           "xy",                    // variation selector supplement
		"é":                     "é",                    // combining mark kept
		"\U0001F468‍\U0001F469":  "\U0001F468‍\U0001F469", // ZWJ kept
		"a\x00b\x1b[31mc":        "ab[31mc",               // controls dropped
		"bad\xffbyte":            "bad�byte",
		"a b":                    "a b", // NBSP is a space
		"\ufeffx":                "x",
		"":                       "",
	}
	for in, want := range cases {
		if got := scm.CleanLine(in, 0); got != want {
			t.Errorf("CleanLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := scm.CleanLine("abcdef", 3); got != "abc" {
		t.Errorf("cap: %q", got)
	}
	if got := scm.CleanLine("abédef", 3); got != "abé" {
		t.Errorf("cap inside multibyte: %q", got)
	}
	if got := scm.NewSubject("fix: thing\n\nlong body"); got != "fix: thing" {
		t.Errorf("subject: %q", got)
	}
	if got := scm.NewSubject(strings.Repeat("x", 500)); len([]rune(got)) != scm.MaxSubjectRunes {
		t.Errorf("subject cap: %d", len([]rune(got)))
	}
}

func FuzzCleanLine(f *testing.F) {
	for _, s := range []string{"a‮b", "x\U000E0041", "\xff\xfe", "a \t\n b", " "} {
		f.Add(s, 10)
	}
	f.Fuzz(func(t *testing.T, s string, n int) {
		max := n % 50
		out := scm.CleanLine(s, max)
		if !utf8.ValidString(out) || (max > 0 && utf8.RuneCountInString(out) > max) {
			t.Fatalf("invalid or too long: %q (max %d)", out, max)
		}
		if strings.ContainsAny(out, "\n\r\t\x00") || strings.HasPrefix(out, " ") || strings.HasSuffix(out, " ") || strings.Contains(out, "  ") {
			t.Fatalf("not one clean line: %q", out)
		}
		if again := scm.CleanLine(out, 0); again != out {
			t.Fatalf("not idempotent: %q -> %q", out, again)
		}
		if again := scm.CleanLine(out, max); again != out {
			t.Fatalf("not idempotent with the same cap: %q -> %q (max %d)", out, again, max)
		}
		for _, r := range out {
			if r == '‮' || r == '​' || (r >= 0xE0000 && r <= 0xE007F) {
				t.Fatalf("invisible rune %U survived", r)
			}
			if (unicode.Is(unicode.Variation_Selector, r) && r != 0xFE0E && r != 0xFE0F) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
				t.Fatalf("default-ignorable rune %U survived", r)
			}
		}
	})
}

func TestCompareCommits(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	cs := []scm.Commit{{SHA: "a", CommittedAt: t0}, {SHA: "c"}, {SHA: "b", CommittedAt: t0}, {SHA: "d", CommittedAt: t0.Add(time.Hour)}}
	slices.SortFunc(cs, scm.CompareCommits)
	var got []string
	for _, c := range cs {
		got = append(got, c.SHA)
	}
	if !slices.Equal(got, []string{"d", "b", "a", "c"}) {
		t.Fatalf("order = %v (newest first, SHA descending on ties, unknown dates last)", got)
	}
}

func TestEvent(t *testing.T) {
	e := scm.Event{Kind: scm.EventPush, Ref: "refs/heads/feature/x"}
	if b, ok := e.Branch(); !ok || b != "feature/x" {
		t.Fatal(b)
	}
	for _, ref := range []string{"refs/tags/v1", "refs/heads/", "main", "refs/heads"} {
		if _, ok := (scm.Event{Ref: ref}).Branch(); ok {
			t.Errorf("%q is not a branch ref", ref)
		}
	}
	if !(scm.Event{Kind: scm.EventPush, Ref: "refs/heads/main"}).IsPushTo("main") ||
		(scm.Event{Kind: scm.EventPush, Ref: "refs/heads/main", Deleted: true}).IsPushTo("main") ||
		(scm.Event{Kind: scm.EventRelease, Ref: "refs/heads/main"}).IsPushTo("main") {
		t.Fatal("IsPushTo")
	}
	if len(scm.EventKinds()) != 8 {
		t.Fatal("update the handler enumeration tests when adding a kind")
	}
	for _, k := range scm.EventKinds() {
		if !k.Known() {
			t.Fatal(k)
		}
	}
	if scm.EventKind("nope").Known() {
		t.Fatal("known")
	}
}

func TestResultResolve(t *testing.T) {
	if (scm.Result[int]{NotModified: true}).Resolve(7) != 7 || (scm.Result[int]{Value: 3}).Resolve(7) != 3 {
		t.Fatal("Resolve")
	}
}

func TestClone(t *testing.T) {
	r := scm.Repository{Topics: []string{"a"}}
	c := r.Clone()
	c.Topics[0] = "z"
	if r.Topics[0] != "a" {
		t.Fatal("Repository.Clone shares Topics")
	}
	s := scm.RepoStats{Release: &scm.Release{Tag: "v1"}}
	d := s.Clone()
	d.Release.Tag = "v2"
	if s.Release.Tag != "v1" {
		t.Fatal("RepoStats.Clone shares Release")
	}
}
