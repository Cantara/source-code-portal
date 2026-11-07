package setup

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/ghlogin"
)

func repoList(names ...string) []ghlogin.Repo {
	out := make([]ghlogin.Repo, len(names))
	for i, n := range names {
		out[i] = ghlogin.Repo{Owner: "acme", Name: n}
	}
	return out
}

func ruleValues(d Derived) []string {
	var out []string
	for _, r := range d.Rules {
		out = append(out, r.Kind+":"+r.Value)
	}
	return out
}

// matches reports whether a name is selected by the matchers a product is written with.
func matches(t *testing.T, d Derived, name string, topics ...string) bool {
	t.Helper()
	for _, e := range d.Entries {
		fn, err := compile(e.Kind, e.Value)
		if err != nil {
			t.Fatalf("an entry does not compile: %+v: %v", e, err)
		}
		if fn(name, topics) {
			return true
		}
	}
	return false
}

func TestDeriveOpenRuleFitsNewRepositories(t *testing.T) {
	all := repoList("Whydah-SSO", "whydah-admin", "Whydah_UI", "whydah-brand", "tools", "notes")
	d := Derive(all, []Product{{Name: "Whydah", Repos: []string{"Whydah-SSO", "whydah-admin", "Whydah_UI"}}}, nil)[0]
	if !slices.Equal(ruleValues(d), []string{"prefix:whydah-"}) {
		t.Fatalf("rules: %v pinned %v", ruleValues(d), d.Pinned)
	}
	r := d.Rules[0]
	// whydah- covers two (SSO, admin); Whydah_UI has no partner for a rule of its own and stays pinned.
	if r.Own != 2 || !r.Open || r.ExtraN != 1 || !slices.Equal(r.Extra, []string{"whydah-brand"}) || !slices.Equal(d.Pinned, []string{"Whydah_UI"}) {
		t.Errorf("rule %+v pinned %v", r, d.Pinned)
	}
	// A repository created later fits; an unrelated one does not; the pinned one is kept as it is.
	if !matches(t, d, "whydah-brand-new") || !matches(t, d, "Whydah-Other") || matches(t, d, "tools") || !matches(t, d, "Whydah_UI") {
		t.Errorf("entries: %+v", d.Entries)
	}
}

func TestDeriveNeverCatchesAnotherProductsMember(t *testing.T) {
	all := repoList("kcp-a", "kcp-b", "kcp-c-x", "kcp-c-y", "kcp-c-z")
	got := Derive(all, []Product{
		{Name: "Kcp", Repos: []string{"kcp-a", "kcp-b"}},
		{Name: "Kcp C", Repos: []string{"kcp-c-x", "kcp-c-y", "kcp-c-z"}},
	}, nil)
	if len(got[0].Rules) != 0 || !slices.Equal(got[0].Pinned, []string{"kcp-a", "kcp-b"}) {
		t.Errorf("kcp- would catch the other product: %v pinned %v", ruleValues(got[0]), got[0].Pinned)
	}
	if !slices.Equal(ruleValues(got[1]), []string{"prefix:kcp-c-"}) || len(got[1].Pinned) != 0 {
		t.Errorf("the narrower product: %v pinned %v", ruleValues(got[1]), got[1].Pinned)
	}
}

func TestDeriveTopicsCamelCaseAndShortPrefixes(t *testing.T) {
	all := []ghlogin.Repo{
		{Name: "sso", Topics: []string{"iam"}}, {Name: "directory", Topics: []string{"iam", "go"}}, {Name: "docs"},
		{Name: "MessiSDK"}, {Name: "MessiProject"}, {Name: "MessiSQSProvider"},
		{Name: "ab-1"}, {Name: "ab-2"}, {Name: "cd-1"}, {Name: "cd-2"}, {Name: "cd-3"},
	}
	got := Derive(all, []Product{
		{Name: "IAM", Repos: []string{"sso", "directory"}},
		{Name: "Messi", Repos: []string{"MessiSDK", "MessiProject", "MessiSQSProvider"}},
		{Name: "Two", Repos: []string{"ab-1", "ab-2"}},
		{Name: "Three", Repos: []string{"cd-1", "cd-2", "cd-3"}},
	}, nil)
	if !slices.Equal(ruleValues(got[0]), []string{"topic:iam"}) {
		t.Errorf("topic: %v", ruleValues(got[0]))
	}
	if !slices.Equal(ruleValues(got[1]), []string{"prefix:messi"}) || !matches(t, got[1], "MessiNew") {
		t.Errorf("camel case: %v", ruleValues(got[1]))
	}
	if len(got[2].Rules) != 0 || len(got[2].Pinned) != 2 {
		t.Errorf("a three-letter prefix of two is not a rule: %v %v", ruleValues(got[2]), got[2].Pinned)
	}
	if !slices.Equal(ruleValues(got[3]), []string{"prefix:cd-"}) {
		t.Errorf("a three-letter prefix of three is: %v", ruleValues(got[3]))
	}
}

func TestDeriveManualRulesAreKeptAndReported(t *testing.T) {
	all := repoList("api-a", "api-b", "api-c", "other")
	got := Derive(all, []Product{
		{Name: "A", Repos: []string{"api-a"}, Rules: []Rule{{Kind: "glob", Value: "api-*"}, {Kind: "regexp", Value: "("}}},
		{Name: "B", Repos: []string{"api-b"}},
	}, nil)
	r := got[0].Rules[0]
	if len(got[0].Rules) != 1 || !r.Manual || r.Own != 1 || !slices.Equal(r.Spill, []string{"api-b"}) || !slices.Equal(r.Extra, []string{"api-c"}) {
		t.Errorf("manual rule: %+v (a bad one is skipped)", got[0].Rules)
	}
	if len(got[0].Pinned) != 0 {
		t.Errorf("a member the rule covers is not pinned: %v", got[0].Pinned)
	}
}

func TestDeriveHiddenRepositoriesAreLeftOut(t *testing.T) {
	all := repoList("xyz-1", "xyz-2", "xyz-3", "xyz-secret")
	got := Derive(all, []Product{{Name: "X", Repos: []string{"xyz-1", "xyz-2", "xyz-3", "xyz-secret"}}}, []string{"xyz-secret"})[0]
	if got.Rules[0].Own != 3 || len(got.Pinned) != 0 {
		t.Errorf("%+v %v", got.Rules, got.Pinned)
	}
}

func TestDeriveManyPinnedBecomeAlternations(t *testing.T) {
	var names []string
	for i := range 200 {
		names = append(names, fmt.Sprintf("r%dx%d", i*7919%1000, i))
	}
	d := Derive(repoList(names...), []Product{{Name: "Many", Repos: names}}, nil)[0]
	if d.TooMany || len(d.Entries) > MaxMatchers || len(d.Entries) < 2 {
		t.Fatalf("entries: %d tooMany %v", len(d.Entries), d.TooMany)
	}
	for _, e := range d.Entries {
		if e.Kind != "regexp" && e.Kind != "prefix" || len(e.Value) > maxRegexpBytes {
			t.Errorf("entry %s %d bytes", e.Kind, len(e.Value))
		}
	}
	for _, n := range names {
		if !matches(t, d, n) {
			t.Fatalf("%s is not selected", n)
		}
	}
	if matches(t, d, "r1x1-not-it") {
		t.Error("an alternation selects more than the names")
	}
}

func TestCompileRefusesWhatThePortalRefuses(t *testing.T) {
	for _, c := range []struct{ kind, value string }{
		{"prefix", ""}, {"prefix", "a b"}, {"exact", "x/y"}, {"glob", "a*b*c*d*e*f*g*h"}, {"regexp", "("}, {"regexp", strings.Repeat("a", 300)},
		{"regexp", "(((a{1000}){1000}){1000})"}, {"topic", "Not A Topic"}, {"nothing", "x"},
	} {
		if _, err := compile(c.kind, c.value); err == nil {
			t.Errorf("%s %q was accepted", c.kind, c.value)
		}
	}
}

func TestDeriveBigBucketsNeedThreeForARule(t *testing.T) {
	names := []string{"alpha-1", "alpha-2", "beta-1", "beta-2", "gamma-x", "delta-y", "eps-z", "zeta-q", "omega-1", "omega-2", "omega-3"}
	d := Derive(repoList(names...), []Product{{Name: "Bucket", Repos: names}}, nil)[0]
	if !slices.Equal(ruleValues(d), []string{"prefix:omega-"}) || len(d.Pinned) != 8 {
		t.Errorf("rules %v pinned %v: two that share a start are a coincidence in a big bucket, three are a pattern", ruleValues(d), d.Pinned)
	}
	small := Derive(repoList(names[:4]...), []Product{{Name: "Small", Repos: names[:4]}}, nil)[0]
	if !slices.Equal(ruleValues(small), []string{"prefix:alpha-", "prefix:beta-"}) {
		t.Errorf("a small product keeps pairs: %v", ruleValues(small))
	}
}
