package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/webhook"
)

// Which sources are polled, which are told by a receiver that nothing can catch up for, and which are told with a delivery log behind.
func TestSourcesByHowTheyAreTold(t *testing.T) {
	app := &config.App{}
	sources := []config.Source{{ID: "token"}, {ID: "app", App: app}}
	for _, tc := range []struct {
		name             string
		secret, catchUp  bool
		polled, unlogged []string
	}{
		{"nothing tells them", false, false, []string{"token", "app"}, nil},
		{"a delivery log for the App", false, true, []string{"token"}, nil},
		{"a receiver", true, false, nil, []string{"token"}},
		{"a receiver and the log", true, true, nil, []string{"token"}},
	} {
		var cfg config.Config
		cfg.GitHub.Sources = sources
		cfg.Webhook.CatchUp = tc.catchUp
		if tc.secret {
			cfg.Webhook.Secret = config.NewSecret([]byte("s"))
		}
		if got := polledSources(cfg); !slices.Equal(got, tc.polled) {
			t.Errorf("%s: polled %v, want %v", tc.name, got, tc.polled)
		}
		if got := unloggedSources(cfg); !slices.Equal(got, tc.unlogged) {
			t.Errorf("%s: unlogged %v, want %v", tc.name, got, tc.unlogged)
		}
	}
}

// Every event the receiver and the delivery log can produce for a ref has a job that listens to it: a push to the default branch, to
// another branch, a tag, and the deletion of a branch or a tag.
func TestEveryRefEventHasAListener(t *testing.T) {
	s, err := standUp(t, modeConfig(t, filepath.Join(t.TempDir(), "events.db")), deps{})
	if err != nil {
		t.Fatal(err)
	}
	repo := `"repository":{"name":"core","default_branch":"main","owner":{"login":"acme"}}`
	for _, tc := range []struct{ name, body string }{
		{"default branch", `{"ref":"refs/heads/main",` + repo + `}`},
		{"another branch", `{"ref":"refs/heads/feature/x",` + repo + `}`},
		{"a deleted branch", `{"ref":"refs/heads/feature/x","deleted":true,` + repo + `}`},
		{"a tag", `{"ref":"refs/tags/v1",` + repo + `}`},
		{"a deleted tag", `{"ref":"refs/tags/v1","deleted":true,` + repo + `}`},
	} {
		n, ok := webhook.Normalise("push", []byte(tc.body))
		if !ok || len(s.engine.Listeners(n.Event)) == 0 {
			t.Errorf("%s: event %q (%v) has listeners %v", tc.name, n.Event, ok, s.engine.Listeners(n.Event))
		}
	}
	if got := s.engine.Listeners(sync.EventBranch); !slices.Equal(got, []string{"commits"}) {
		t.Errorf("a branch push runs %v, want only the history", got)
	}
}
