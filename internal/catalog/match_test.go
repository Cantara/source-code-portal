package catalog

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func TestMatchers(t *testing.T) {
	repo := func(name string, topics ...string) *snapshot.Repo {
		return &snapshot.Repo{Name: name, Topics: topics, ID: snapshot.RepoID{Source: "gh"}}
	}
	tests := []struct {
		kind, value string
		yes, no     []*snapshot.Repo
	}{
		{"exact", "Whydah-SSO", []*snapshot.Repo{repo("whydah-sso")}, []*snapshot.Repo{repo("Whydah-SSO-x")}},
		{"prefix", "Whydah-", []*snapshot.Repo{repo("whydah-x"), repo("Whydah-")}, []*snapshot.Repo{repo("xWhydah-")}},
		{"glob", "IoT*", []*snapshot.Repo{repo("iot-hub"), repo("IoT")}, []*snapshot.Repo{repo("my-IoT")}},
		{"glob", "a?c", []*snapshot.Repo{repo("abc")}, []*snapshot.Repo{repo("ac"), repo("abbc")}},
		{"glob", "a.b", []*snapshot.Repo{repo("a.b")}, []*snapshot.Repo{repo("axb")}},
		{"regexp", "IoT.*", []*snapshot.Repo{repo("IoT-hub")}, []*snapshot.Repo{repo("iot-hub"), repo("my-IoT-hub")}},
		{"regexp", "(?i)iot.*", []*snapshot.Repo{repo("IoT-hub")}, nil},
		{"regexp", "a|b", []*snapshot.Repo{repo("a"), repo("b")}, []*snapshot.Repo{repo("ab"), repo("xa")}},
		{"topic", "iam", []*snapshot.Repo{repo("x", "iam")}, []*snapshot.Repo{repo("iam")}},
		{"catch_all", "", []*snapshot.Repo{repo("anything")}, nil},
	}
	for _, tc := range tests {
		m, err := compileMatcher(tc.kind, tc.value)
		if err != nil {
			t.Fatalf("%s %q: %v", tc.kind, tc.value, err)
		}
		for _, r := range tc.yes {
			if !m(r) {
				t.Errorf("%s %q does not match %q", tc.kind, tc.value, r.Name)
			}
		}
		for _, r := range tc.no {
			if m(r) {
				t.Errorf("%s %q matches %q", tc.kind, tc.value, r.Name)
			}
		}
	}
}

func TestMatcherRefused(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"glob", "*a*b*c*d*e*f*g"},
		{"regexp", "a)|(b"}, // an unbalanced ) must not escape the anchors
		{"regexp", "("},
		{"regexp", strings.Repeat("a", 257)},
		{"regexp", "(a{100}){100}"},
		{"topic", "Not A Topic"},
	} {
		if _, err := compileMatcher(tc.kind, tc.value); err == nil {
			t.Errorf("%s %q compiled", tc.kind, tc.value)
		}
	}
}

func TestCompileFindings(t *testing.T) {
	g := func(id, def string, m ...config.Match) config.Group {
		return config.Group{ID: id, Name: id, DefaultRepo: def, Match: m}
	}
	tests := map[string]struct {
		groups []config.Group
		err    bool
		want   string
	}{
		"ok":                   {groups: []config.Group{g("a", "Whydah-x", config.Match{Prefix: "whydah-"})}},
		"default not selected": {groups: []config.Group{g("a", "other", config.Match{Prefix: "whydah-"})}, err: true, want: "not selected"},
		"default via topic":    {groups: []config.Group{g("a", "other", config.Match{Topic: "iam"})}, want: "not selected"},
		"bad default name":     {groups: []config.Group{g("a", "a/b", config.Match{CatchAll: true})}, err: true, want: "not a repository name"},
		"bad regexp":           {groups: []config.Group{g("a", "x", config.Match{Regexp: "("})}, err: true, want: "regexp"},
		"empty-name glob":      {groups: []config.Group{g("a", "x", config.Match{Glob: "*"})}, want: "empty name"},
		"two catch-alls": {groups: []config.Group{g("a", "x", config.Match{CatchAll: true}), g("b", "x", config.Match{CatchAll: true})},
			want: "catch_all"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gs, fs := Compile(tc.groups)
			if (gs == nil) != tc.err {
				t.Errorf("refused = %v, want %v (%v)", gs == nil, tc.err, fs)
			}
			found := tc.want == ""
			for _, f := range fs {
				found = found || strings.Contains(f.Message, tc.want)
			}
			if !found {
				t.Errorf("findings %v do not mention %q", fs, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	gs, _ := Compile([]config.Group{
		{ID: "w", Name: "W", DefaultRepo: "whydah-core", Match: []config.Match{{Prefix: "whydah-"}}},
		{ID: "x", Name: "X", DefaultRepo: "missing", Sources: []string{"other"}, Match: []config.Match{{CatchAll: true}}},
	})
	repos := map[snapshot.RepoKey]*snapshot.RepoEntry{}
	for _, n := range []string{"Whydah-Core", "whydah-b", "other"} {
		r := snapshot.Repo{ID: snapshot.RepoID{Source: "gh"}, Owner: "acme", Name: n}
		repos[r.Key()] = &snapshot.RepoEntry{Repo: r}
	}
	got := gs.Resolve(repos)
	if len(got) != 2 || strings.Join(keys(got[0].Members), ",") != "acme/whydah-b,acme/whydah-core" ||
		got[0].DefaultRepo != "acme/whydah-core" || got[0].DefaultMissing {
		t.Errorf("group w = %+v", got[0])
	}
	if len(got[1].Members) != 0 || !got[1].DefaultMissing {
		t.Errorf("group x (another source only) = %+v", got[1])
	}
}

func keys(ks []snapshot.RepoKey) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}
