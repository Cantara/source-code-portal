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
)

// TestCatchUpWithoutAReceiver: with webhook.catch_up and no webhook secret the portal has no receiver (GitHub cannot reach a
// portal at home) and still learns what happened from the App's delivery log, without waiting for a poll.
func TestCatchUpWithoutAReceiver(t *testing.T) {
	gh := githubtest.New(t)
	gh.Install("acme")
	missed := gh.AddRepo("acme", gogh.Repository{Name: new("missed"), Visibility: new("public")})
	quiet := gh.AddRepo("acme", gogh.Repository{Name: new("quiet"), Visibility: new("public")})

	dir := t.TempDir()
	key := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(key, gh.PrivateKeyPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
webhook: {catch_up: true}
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
	srv, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if srv.inbox == nil || len(srv.inbox.catchUps) != 1 {
		t.Fatalf("the delivery log is not read: %+v", srv.inbox)
	}
	srv.inbox.catchUps[0].Every = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()
	public := "http://" + srv.publicLn.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	releases := func() map[string]string {
		resp, err := client.Get(public + "/api/v1/releases")
		if err != nil {
			return nil
		}
		defer func() { _ = resp.Body.Close() }()
		var r struct {
			Releases []struct {
				Repo string
				Tag  string
			}
		}
		_ = json.NewDecoder(resp.Body).Decode(&r)
		out := map[string]string{}
		for _, x := range r.Releases {
			out[x.Repo] = x.Tag
		}
		return out
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 10s", what)
			}
		}
	}
	wait("the first read", func() bool { return srv.pub.Current().Catalogued && releases() != nil })
	for w, r := srv.engine.Depth(); w != 0 || r != 0; w, r = srv.engine.Depth() {
		time.Sleep(20 * time.Millisecond) // every job has run: what comes next is not the first read
	}

	// A receiver would answer here; there is none.
	resp, err := client.Post(public+"/webhooks/github", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed { // a receiver answers 401 or 202
		t.Errorf("a webhook receiver is registered without a secret: %d", resp.StatusCode)
	}

	// Two releases are published; GitHub could not deliver the first one's event (no answer), nobody announced the second.
	at := time.Now().UTC().Add(-time.Minute)
	gh.SetRelease(missed, "v1.0.0", "One", at)
	gh.SetRelease(quiet, "v2.0.0", "Two", at)
	gh.SetDeliveries(githubtest.Delivery{GUID: "lost-1", Event: "release", Action: "published", RepositoryID: missed, At: time.Now().UTC(), Status: 0})
	wait("the missed delivery's release", func() bool { return releases()["acme/missed"] == "v1.0.0" })
	if _, ok := releases()["acme/quiet"]; ok {
		t.Errorf("a release nobody announced was read before its poll: %v", releases())
	}
}
