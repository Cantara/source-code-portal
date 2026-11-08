//go:build !server

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/ghlogin"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/setup"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/vault"
)

// TestPersonalWithAnApp: the account whose GitHub App the person installed is read with the App (its key sealed in the vault, the
// delivery log read for events), and the portal that scp personal starts runs on it; scp serve alone refuses a key in a vault.
func TestPersonalWithAnApp(t *testing.T) {
	ring := &memRing{}
	gh := githubtest.New(t)
	gh.Install("acme")
	gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	p, _ := newPersonal(t, ring, dir, "", "", personalFlags{port: port})
	key, _, err := vault.OpenKey(dir, ring)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "layout"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "personal.yaml")
	pemKey := string(gh.PrivateKeyPEM())
	_, err = p.finishSetup(t.Context(), dir, cfgPath, key, setup.Result{Host: ghHost(gh.BaseURL()), Token: ghlogin.Token{Value: "ghp_test"}, User: "ann",
		Accounts: []string{"acme", "ann"}, Default: "open",
		Apps: []setup.AppKey{{Account: "acme", ID: gh.AppID, Slug: "scp-acme-ab12", Key: pemKey}}})
	if err != nil {
		t.Fatal(err)
	}
	yaml, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(yaml), "PRIVATE KEY") {
		t.Error("personal.yaml holds the App's key")
	}
	cfg, _, err := config.Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, yaml)
	}
	// One source per mode: the App's account by events, the other by polling with the token.
	if len(cfg.GitHub.Sources) != 2 || cfg.GitHub.Sources[0].ID != "app-acme" || cfg.GitHub.Sources[0].App == nil || !cfg.GitHub.Sources[0].App.Credential ||
		cfg.GitHub.Sources[0].App.ID != gh.AppID || cfg.GitHub.Sources[1].ID != personalSource || !cfg.GitHub.Sources[1].Credential ||
		strings.Join(cfg.GitHub.Sources[1].Orgs, ",") != "ann" || !cfg.Webhook.CatchUp {
		t.Fatalf("sources: %+v webhook: %+v\n%s", cfg.GitHub.Sources, cfg.Webhook, yaml)
	}
	cfg.Server.AdminAddr = "127.0.0.1:0"
	if _, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("serve must refuse a key that is in the vault: %v", err)
	}
	if err := p.resolve(t.Context(), dir, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.GitHub.Sources[0].App.PrivateKey.IsSet() || !cfg.GitHub.Sources[1].Token.IsSet() {
		t.Fatal("resolve left a credential out")
	}
	// Only the App's account is read in this test: the token source's account has no fake listing, so leave it out.
	cfg.GitHub.Sources = cfg.GitHub.Sources[:1]
	d := testDeps(t)
	d.mode = store.Personal // as scp personal runs it
	if d.launch, err = newLaunch(time.Now()); err != nil {
		t.Fatal(err)
	}
	srv, err := newServer(t.Context(), cfg, d, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if srv.inbox == nil || len(srv.inbox.catchUps) != 1 {
		t.Fatalf("the App's delivery log is not read: %+v", srv.inbox)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()
	for deadline := time.Now().Add(10 * time.Second); !srv.pub.Current().Catalogued; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the App's account was not read")
		}
	}
}
