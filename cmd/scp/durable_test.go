package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/store"
)

// TestWebhookEventsSurviveARestart: GitHub sends a delivery once. One that was acknowledged but not yet acted on when the process
// died is parked in the store and passed on at the next start; one that arrived while the portal was down is in the App's delivery log
// with no success answer and is passed on by reading that log. Both reach the repository's issues without waiting for a poll.
func TestWebhookEventsSurviveARestart(t *testing.T) {
	gh := githubtest.New(t)
	gh.Install("acme")
	parked := gh.AddRepo("acme", gogh.Repository{Name: new("parked"), Visibility: new("public")})
	missed := gh.AddRepo("acme", gogh.Repository{Name: new("missed"), Visibility: new("public")})
	quiet := gh.AddRepo("acme", gogh.Repository{Name: new("quiet"), Visibility: new("public")})

	dir := t.TempDir()
	key, secret, db := filepath.Join(dir, "app.pem"), filepath.Join(dir, "secret"), filepath.Join(dir, "scp.db")
	for f, b := range map[string][]byte{key: gh.PrivateKeyPEM(), secret: []byte("hooks")} {
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + db + `}
webhook: {secret_file: ` + secret + `}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], app: {id: ` + strconv.FormatInt(gh.AppID, 10) + `, private_key_file: ` + key + `}}
catalog:
  groups:
    - {id: all, name: All, default_repo: quiet, match: [{catch_all: true}]}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"

	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	issues := func(public string) map[string]int {
		resp, err := client.Get(public + "/api/v1/issues")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var r struct {
			Issues []struct {
				Repo   string
				Number int
			}
		}
		_ = json.NewDecoder(resp.Body).Decode(&r)
		out := map[string]int{}
		for _, i := range r.Issues {
			out[i.Repo] = i.Number
		}
		return out
	}
	start := func(prepare func(*server)) (stop func(), public string, srv *server) {
		t.Helper()
		srv, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if prepare != nil {
			prepare(srv)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- srv.run(ctx) }()
		return func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("run: %v", err)
			}
		}, "http://" + srv.publicLn.Addr().String(), srv
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 10s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	stop, public, first := start(nil)
	waitFor("the first start's issues", func() bool {
		resp, err := client.Get(public + "/api/v1/issues")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	if got := issues(public); len(got) != 0 {
		t.Fatalf("issues before there are any: %v", got)
	}
	idle := 0 // every job of the first start has run, so the next start finds nothing due
	waitFor("the first start's jobs", func() bool {
		if w, r := first.engine.Depth(); w == 0 && r == 0 {
			idle++
		} else {
			idle = 0
		}
		return idle >= 25
	})
	stop()

	// While the portal is down: an issue in each repository. One delivery was acknowledged but never acted on (parked); the other
	// never arrived (the log shows no answer). The third repository gets an issue and no event at all: only a poll would find it.
	at := time.Now().UTC().Add(-time.Minute)
	for id, n := range map[int64]int{parked: 1, missed: 2, quiet: 3} {
		gh.SetIssues(id, githubtest.Issue{Number: n, Title: "New", Author: "ann", Created: at, Updated: at})
	}
	gh.SetDeliveries(githubtest.Delivery{GUID: "lost-1", Event: "issues", Action: "opened", RepositoryID: missed, At: time.Now().UTC(), Status: 0})
	st, err := store.Open(t.Context(), db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(parkedEvent{Event: "issues", Repo: "acme/parked", At: time.Now().UTC()})
	if err := st.Park(t.Context(), "1-a", string(row)); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	stop, public, _ = start(func(s *server) {
		for _, c := range s.inbox.catchUps {
			c.Every = 50 * time.Millisecond
		}
	})
	defer stop()
	waitFor("the parked and the missed events", func() bool {
		got := issues(public)
		return got["acme/parked"] == 1 && got["acme/missed"] == 2
	})
	if got := issues(public); strings.Contains(strings.Join(mapKeys(got), ","), "acme/quiet") {
		t.Errorf("an issue nobody announced was read before its poll: %v", got)
	}
}

func mapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
