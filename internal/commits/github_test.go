package commits

import (
	"log/slog"
	"strings"
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
	srv.AddRepo("acme", gh.Repository{Name: new("empty")})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	srv.SetCommits(id, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "second\n\nbody", AuthorName: "Ann", AuthorLogin: "ann", Date: at},
		githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "first", AuthorName: "Bo", Date: at.Add(-time.Hour)})
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f := FromGitHub(c.Source("gh"))
	cs, err := f.Commits(t.Context(), "acme", "one", 30)
	if err != nil || len(cs) != 2 || cs[0].Message != "second\n\nbody" || cs[0].AuthorLogin != "ann" || !cs[0].Date.Equal(at) ||
		!strings.HasSuffix(cs[0].HTMLURL, "/commit/"+strings.Repeat("b", 40)) {
		t.Fatalf("commits = %+v, %v", cs, err)
	}
	if _, err := f.Commits(t.Context(), "acme", "empty", 30); !github.IsNotFound(err) {
		t.Errorf("empty repository: err = %v, want not found", err)
	}
}
