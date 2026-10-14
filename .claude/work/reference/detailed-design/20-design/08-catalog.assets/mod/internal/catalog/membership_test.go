package catalog

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

func slugged(names ...string) []scm.Repository {
	var out []scm.Repository
	for i, n := range names {
		r := repo(i+1, n)
		r.Slug = scm.Slug(strings.ToLower(n))
		out = append(out, r)
	}
	return out
}

func TestMembershipManyToManyDefaultsAndRefs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups = []GroupConfig{
		{ID: "widget", DisplayName: "W", DefaultRepo: "widget-sts", Match: []Matcher{{KindPrefix, "Widget"}}, Ref: "release"},
		{ID: "sdks", DisplayName: "S", DefaultRepo: "Widget-Client-SDK", Match: []Matcher{{KindGlob, "*-SDK"}}, Ref: "stable",
			Integrations: GroupIntegrations{Jenkins: &JenkinsDefaults{JobPrefix: "J-", Instance: "main"}}},
		{ID: "all", DisplayName: "A", DefaultRepo: "docs", Match: []Matcher{{KindCatchAll, "true"}}},
		{ID: "ghost", DisplayName: "G", DefaultRepo: "nothing", Match: []Matcher{{KindPrefix, "noth"}}},
	}
	if fs := cfg.Validate(ValidateContext{}); HasErrors(fs) {
		t.Fatal(fs)
	}
	c, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rs := slugged("Widget-STS", "Widget-Client-SDK", "docs", "other-SDK")
	mr := mustBuild(t, c, rs)
	m := mr.Membership
	if got := m.ReposOf("widget"); !slices.Equal(got, []scm.Slug{"widget-client-sdk", "widget-sts"}) {
		t.Fatalf("widget: %v", got)
	}
	if got := m.GroupsOf("widget-client-sdk"); !slices.Equal(got, []scm.GroupID{"widget", "sdks", "all"}) {
		t.Fatalf("groups of Widget-Client-SDK: %v", got) // config order, not alphabetical
	}
	if d, ok := m.DefaultRepo("widget"); !ok || d != "widget-sts" { // the configured default is folded and resolved to the canonical (lower-case) slug
		t.Fatalf("default: %v %v", d, ok)
	}
	if _, ok := m.DefaultRepo("ghost"); ok {
		t.Fatal("a missing default repository must be reported as missing, never guessed")
	}
	if h := mr.Groups[3]; h.DefaultReason != "missing" || mr.Groups[0].DefaultReason != "" {
		t.Fatalf("health: %+v", mr.Groups)
	}
	if mr.TrackedRef["widget-client-sdk"] != "release" || len(mr.RefConflicts["widget-client-sdk"]) != 1 || mr.TrackedRef["widget-sts"] != "release" {
		t.Fatalf("refs: %v %v", mr.TrackedRef, mr.RefConflicts) // first group in config order wins, the conflict is reported
	}
	if got := m.Groups()[1].Integrations["jenkins"]["instance"]; got != "main" {
		t.Fatalf("instance: %v", m.Groups()[1].Integrations)
	}
	if got := m.Groups()[1].Integrations["jenkins"]["job_prefix"]; got != "J-" {
		t.Fatalf("integrations: %v", m.Groups()[1].Integrations)
	}
}

func TestMembershipIsOrderIndependent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups = []GroupConfig{{ID: "a", DisplayName: "A", DefaultRepo: "r1", Match: []Matcher{{KindGlob, "r*"}}}, {ID: "b", DisplayName: "B", DefaultRepo: "r2", Match: []Matcher{{KindPrefix, "r1"}}}}
	c, _ := Compile(cfg)
	var names []string
	for i := 0; i < 50; i++ {
		names = append(names, fmt.Sprintf("r%d", i))
	}
	want := mustBuild(t, c, slugged(names...)).Membership.ReposOf("a")
	rnd := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 20; i++ {
		rnd.Shuffle(len(names), func(a, b int) { names[a], names[b] = names[b], names[a] })
		if got := mustBuild(t, c, slugged(names...)).Membership.ReposOf("a"); !slices.Equal(got, want) {
			t.Fatal("membership depends on input order")
		}
	}
}

func TestValidateRules(t *testing.T) {
	g := func(mod func(*GroupConfig)) Config {
		c := DefaultConfig()
		x := GroupConfig{ID: "a", DisplayName: "A", DefaultRepo: "r", Match: []Matcher{{KindExact, "r"}}}
		mod(&x)
		c.Groups = []GroupConfig{x}
		return c
	}
	codes := func(c Config) string {
		var cs []string
		for _, f := range c.Validate(ValidateContext{Sources: []scm.SourceID{"acme"}}) {
			cs = append(cs, f.Code+":"+f.Severity.String())
		}
		slices.Sort(cs)
		return strings.Join(cs, " ")
	}
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"ok", g(func(*GroupConfig) {}), ""},
		{"bad id", g(func(x *GroupConfig) { x.ID = "Acme-All" }), "C010:error"},
		{"no name", g(func(x *GroupConfig) { x.DisplayName = "" }), "C012:error"},
		{"no matchers", g(func(x *GroupConfig) { x.Match = nil }), "C020:error C031:error"},
		{"bad regexp", g(func(x *GroupConfig) { x.Match = []Matcher{{KindRegexp, "("}} }), "C022:error"},
		{"default unreachable", g(func(x *GroupConfig) { x.DefaultRepo = "other" }), "C031:error"},
		{"default needs topic", g(func(x *GroupConfig) { x.DefaultRepo = "other"; x.Match = []Matcher{{KindTopic, "iam"}} }), "C031:info"},
		{"no default", g(func(x *GroupConfig) { x.DefaultRepo = "" }), "C030:error"},
		{"unknown source", g(func(x *GroupConfig) { x.Sources = []scm.SourceID{"nope"} }), "C070:error"},
		{"bad ref", g(func(x *GroupConfig) { x.Ref = "a..b" }), "C040:error"},
		{"ref warns", g(func(x *GroupConfig) { x.Ref = "release" }), "W103:warning"},
		{"star in prefix is a charset error", g(func(x *GroupConfig) { x.Match = []Matcher{{KindPrefix, "Widget*"}}; x.DefaultRepo = "Widget*" }), "C022:error C032:error"},
		{"alias equals own id", g(func(x *GroupConfig) { x.Aliases = []string{"A"} }), "C011:error"},
		{"bad jenkins instance", g(func(x *GroupConfig) { x.Integrations.Jenkins = &JenkinsDefaults{Instance: "a b"} }), "C051:error"},
		{"empty-matching regexp", g(func(x *GroupConfig) { x.Match = []Matcher{{KindRegexp, ".*"}} }), "W108:warning"},
	}
	for _, tc := range cases {
		if got := codes(tc.cfg); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
	// duplicate group ids and a duplicate alias across groups
	c := DefaultConfig()
	c.Groups = []GroupConfig{
		{ID: "a", DisplayName: "A", DefaultRepo: "r", Match: []Matcher{{KindExact, "r"}}, Aliases: []string{"Old"}},
		{ID: "a", DisplayName: "B", DefaultRepo: "r", Match: []Matcher{{KindExact, "r"}}, Aliases: []string{"old"}},
	}
	if got := codes(c); !strings.Contains(got, "C011:error") || !strings.Contains(got, "W101:warning") {
		t.Errorf("duplicates: %q", got)
	}
}

func TestDiscoveryPolicyValidate(t *testing.T) {
	p := DefaultDiscoveryPolicy()
	if fs := p.Validate("/p", PolicyContext{}); len(fs) != 0 {
		t.Fatal(fs)
	}
	p.Visibility = VisibilityInstallationScope
	if fs := p.Validate("/p", PolicyContext{}); len(fs) != 1 || fs[0].Code != "C080" {
		t.Fatalf("installation_scope without access: %v", fs)
	}
	if fs := p.Validate("/p", PolicyContext{AccessEnabled: true}); len(fs) != 0 {
		t.Fatalf("installation_scope with access on github.com: %v", fs)
	}
	if fs := p.Validate("/p", PolicyContext{AccessEnabled: true, Host: HostGHES}); len(fs) != 1 || fs[0].Code != "C082" {
		t.Fatalf("GHES needs the acknowledgement: %v", fs)
	}
	p.AccessAcknowledgement = "oidc_proxy"
	if fs := p.Validate("/p", PolicyContext{AccessEnabled: true, Host: HostGHES}); len(fs) != 0 {
		t.Fatal(fs)
	}
	q := DefaultDiscoveryPolicy()
	if fs := q.Validate("/p", PolicyContext{Host: HostGHECDataResidency}); len(fs) != 1 || fs[0].Code != "C081" {
		t.Fatalf("public_only on GHEC-DR: %v", fs)
	}
}

func TestAdmitFailsClosed(t *testing.T) {
	p := DefaultDiscoveryPolicy()
	r := repo(1, "x")
	for _, v := range []scm.Visibility{scm.VisInternal, scm.VisPrivate, scm.Visibility(9)} {
		r.Visibility = v
		if p.Admit(r) != ExcludedVisibility {
			t.Errorf("public_only admitted visibility %d", v)
		}
	}
	p.Visibility = VisibilityInstallationScope
	r.Visibility = scm.Visibility(9)
	if p.Admit(r) != ExcludedVisibility {
		t.Error("installation_scope admitted an unknown visibility")
	}
}

func TestCodecRoundTrip(t *testing.T) {
	r := repo(7, "api")
	r.Slug, r.Topics, r.Description, r.CreatedAt, r.PushedAt = "api", []string{"x"}, "d", t0, t0
	in := Tracked{Repo: r, Aliases: []scm.Slug{"old"}, FirstSeen: t0, SeenAt: t0.Add(time.Hour), Misses: 1}
	row, err := EncodeRow(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeRow(row)
	if err != nil || !sameRepo(in.Repo, out.Repo) || !slices.Equal(in.Aliases, out.Aliases) || out.Misses != 1 || !out.SeenAt.Equal(in.SeenAt) {
		t.Fatalf("%+v %v", out, err)
	}
	row.Meta = []byte(`{"schema":2}`)
	if _, err := DecodeRow(row); err == nil {
		t.Fatal("unknown schema accepted")
	}
}

func BenchmarkBuildMembership(b *testing.B) {
	cfg := DefaultConfig()
	for i := 0; i < 50; i++ {
		cfg.Groups = append(cfg.Groups, GroupConfig{ID: scm.GroupID(fmt.Sprintf("g%d", i)), DisplayName: "G", DefaultRepo: "x",
			Match: []Matcher{{KindPrefix, fmt.Sprintf("p%d-", i)}, {KindGlob, fmt.Sprintf("*-g%d-*", i)}, {KindRegexp, fmt.Sprintf("(a|b)%d.*", i)}, {KindTopic, "iam"}}})
	}
	c, _ := Compile(cfg)
	var rs []scm.Repository
	for i := 0; i < 1000; i++ {
		r := repo(i+1, fmt.Sprintf("p%d-service-g%d-x%d", i%60, i%50, i)) // unique: the real kernel rejects a duplicate slug
		r.Slug, r.Topics = scm.Slug(strings.ToLower(r.Ref.Name)), []string{"iam", "go"}
		rs = append(rs, r)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.BuildMembership(rs); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFindingsSortNumerically(t *testing.T) {
	fs := []Finding{{Code: "R201", Severity: SevWarning, Path: "/catalog/groups/10/match"}, {Code: "R201", Severity: SevWarning, Path: "/catalog/groups/2/match"}, {Code: "C031", Severity: SevError, Path: "/catalog/groups/9/default_repo"}}
	SortFindings(fs)
	if fs[0].Code != "C031" || fs[1].Path != "/catalog/groups/2/match" || fs[2].Path != "/catalog/groups/10/match" {
		t.Fatalf("%v", fs)
	}
}

func mustBuild(t testing.TB, c *Compiled, rs []scm.Repository) MembershipResult {
	t.Helper()
	mr, err := c.BuildMembership(rs)
	if err != nil {
		t.Fatal(err)
	}
	return mr
}
