package badges

import (
	"log/slog"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

func TestFromGitHub(t *testing.T) {
	srv := githubtest.New(t)
	id := srv.AddRepo("acme", gh.Repository{Name: new("one")})
	srv.AddRepo("acme", gh.Repository{Name: new("none")})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	srv.SetRelease(id, "v1.2.0", "One two", at)
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	src := FromGitHub(c.Source("gh"))
	if r, err := src.LatestRelease(t.Context(), "acme", "one"); err != nil || r == nil || r.Tag != "v1.2.0" || !r.Published.Equal(at) {
		t.Errorf("release = %+v, %v", r, err)
	}
	if r, err := src.LatestRelease(t.Context(), "acme", "none"); err != nil || r != nil {
		t.Errorf("no release = %+v, %v", r, err)
	}
}
