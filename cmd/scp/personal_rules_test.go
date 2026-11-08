//go:build !server

package main

import (
	"slices"
	"testing"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/setup"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// TestSetupRulesSelectWhatThePortalSelects holds the setup page's idea of a rule to the catalogue's: what the page counts and shows is
// what the written configuration then selects.
func TestSetupRulesSelectWhatThePortalSelects(t *testing.T) {
	type repo struct {
		name   string
		topics []string
	}
	repos := []repo{{"Whydah-SSO", []string{"iam"}}, {"whydah-admin", nil}, {"whydah_ui", nil}, {"MessiSDK", nil}, {"messi", []string{"kcp"}},
		{"kcp-a", []string{"kcp", "go"}}, {"tools", nil}, {".github", nil}, {"a.b-c", nil}, {"zzz", nil}}
	for _, c := range []struct{ kind, value string }{
		{"prefix", "whydah-"}, {"prefix", "WHYDAH"}, {"prefix", "messi"}, {"exact", "TOOLS"}, {"exact", ".github"},
		{"glob", "*-a"}, {"glob", "whydah?*"}, {"glob", "A.B*"}, {"regexp", "whydah[-_].*"}, {"regexp", "(?i)messi.*"}, {"regexp", "Messi.*"},
		{"regexp", "kcp|tools"}, {"topic", "kcp"}, {"topic", "go"}, {"topic", "iam"},
	} {
		fn, err := setup.Matcher(c.kind, c.value)
		if err != nil {
			t.Fatalf("%s %s: %v", c.kind, c.value, err)
		}
		m := config.Match{}
		switch c.kind {
		case "prefix":
			m.Prefix = c.value
		case "exact":
			m.Exact = c.value
		case "glob":
			m.Glob = c.value
		case "regexp":
			m.Regexp = c.value
		case "topic":
			m.Topic = c.value
		}
		// The default repository has to be selected, so the group also holds zzz by name.
		gs, fs := catalog.Compile([]config.Group{{ID: "g", Name: "G", DefaultRepo: "zzz", Match: []config.Match{m, {Exact: "zzz"}}}})
		if gs == nil {
			t.Fatalf("%s %s refused: %v", c.kind, c.value, fs)
		}
		entries := map[snapshot.RepoKey]*snapshot.RepoEntry{}
		for _, r := range repos {
			sr := snapshot.Repo{ID: snapshot.RepoID{Source: "gh"}, Owner: "acme", Name: r.name, Topics: r.topics}
			entries[sr.Key()] = &snapshot.RepoEntry{Repo: sr}
		}
		var want, got []string
		for _, r := range repos {
			if r.name != "zzz" && fn(r.name, r.topics) {
				want = append(want, r.name)
			}
		}
		for _, k := range gs.Resolve(entries)[0].Members {
			if n := entries[k].Repo.Name; n != "zzz" {
				got = append(got, n)
			}
		}
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			t.Errorf("%s %q: the page says %v, the portal selects %v", c.kind, c.value, want, got)
		}
	}
}
