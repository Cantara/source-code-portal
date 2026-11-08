package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/auth"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/store"
)

// modeConfig is a configuration with sign-in by a token named owner, the store at storePath and loopback listeners.
func modeConfig(t *testing.T, storePath string) config.Config {
	t.Helper()
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	dir := t.TempDir()
	file := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, _, err := config.Load(file("scp.yaml", `store: {path: `+storePath+`}
server: {public_url: "http://127.0.0.1:1"}
access: {private_groups: [staff]}
auth:
  session_key_file: `+file("key", strings.Repeat("k", 32))+`
  tokens: [{name: owner, secret_file: `+file("owner", strings.Repeat("o", 40))+`, groups: [staff], admin: true}]
github:
  sources:
    - {id: gh, base_url: "`+gh.BaseURL()+`", orgs: [acme], token_file: `+file("token", "ghp_test")+`, include_private: true}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	return cfg
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func launchFor(token string) *auth.Launch {
	return &auth.Launch{Code: strings.Repeat("c", 43), Token: token, Expires: time.Now().Add(time.Hour)}
}

// standUp starts a server far enough to read what it registered, and stops it.
func standUp(t *testing.T, cfg config.Config, d deps) (*server, error) {
	t.Helper()
	d.registries = newFakeRegistry(t)
	s, err := newServer(t.Context(), cfg, d, quietLog())
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = s.publicLn.Close(); _ = s.adminLn.Close(); _ = s.store.Close() })
	return s, nil
}

// Each mode registers its own routes: the same configuration, run as the shared site and as one person's portal, differs by the
// launch link and by nothing else; a route that appears in the wrong mode, or goes missing from one, fails here.
func TestEachModeRegistersItsOwnRoutes(t *testing.T) {
	dir := t.TempDir()
	shared, err := standUp(t, modeConfig(t, filepath.Join(dir, "shared.db")), deps{})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := standUp(t, modeConfig(t, filepath.Join(dir, "personal.db")), deps{mode: store.Personal, launch: launchFor("owner")})
	if err != nil {
		t.Fatal(err)
	}
	if shared.mode != store.Shared || personal.mode != store.Personal {
		t.Errorf("modes: %q and %q", shared.mode, personal.mode)
	}
	if slices.Contains(shared.routes, "GET /auth/launch") {
		t.Error("the shared site registers the launch link")
	}
	var extra []string
	for _, r := range personal.routes {
		if !slices.Contains(shared.routes, r) {
			extra = append(extra, r)
		}
	}
	for _, r := range shared.routes {
		if !slices.Contains(personal.routes, r) {
			t.Errorf("the personal portal lacks %s of the shared site", r)
		}
	}
	if !slices.Equal(extra, []string{"GET /auth/launch"}) {
		t.Errorf("the personal portal's own routes: %q, want only the launch link", extra)
	}
	if len(shared.routes) < 20 {
		t.Errorf("only %d routes registered: the comparison is empty", len(shared.routes))
	}
	for _, r := range shared.routes {
		if strings.Contains(r, "/setup") {
			t.Errorf("the shared site registers %s: the setup page is scp personal's own server", r)
		}
	}
}

// A store belongs to its mode for good: the shared site refuses a personal store and scp personal refuses a site's, whatever the
// configuration says, and the refusal leaves the file as it was.
func TestAStoreOfAnotherModeIsRefused(t *testing.T) {
	dir := t.TempDir()
	mark := func(path string, mode store.Mode) []byte {
		db, err := store.OpenAs(t.Context(), path, mode, quietLog())
		if err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		b, _ := os.ReadFile(path)
		return b
	}
	personalDB, sharedDB := filepath.Join(dir, "personal.db"), filepath.Join(dir, "shared.db")
	pBefore, sBefore := mark(personalDB, store.Personal), mark(sharedDB, store.Shared)

	var me *store.ModeError
	if _, err := standUp(t, modeConfig(t, personalDB), deps{}); !errors.As(err, &me) || me.Have != store.Personal {
		t.Errorf("scp serve opened a personal store: %v", err)
	}
	if _, err := standUp(t, modeConfig(t, sharedDB), deps{mode: store.Personal, launch: launchFor("owner")}); !errors.As(err, &me) || me.Have != store.Shared {
		t.Errorf("scp personal opened a shared site's store: %v", err)
	}
	if b, _ := os.ReadFile(personalDB); string(b) != string(pBefore) {
		t.Error("the personal store changed")
	}
	if b, _ := os.ReadFile(sharedDB); string(b) != string(sBefore) {
		t.Error("the shared store changed")
	}
	if _, err := standUp(t, modeConfig(t, filepath.Join(dir, "plain.db")), deps{launch: launchFor("owner")}); err == nil || !strings.Contains(err.Error(), "scp personal") {
		t.Errorf("a launch link on the shared site: %v", err)
	}
}
