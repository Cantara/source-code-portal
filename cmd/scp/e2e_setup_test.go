package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/setup"
)

// TestServeSetupForPlaywright serves the personal edition's setup page on E2E_SETUP_ADDR against a fake GitHub until it is
// interrupted: the second server of the web e2e suite (web/playwright.config.ts). The entry code is e2e-entry-code; the GitHub CLI
// "has" the token ghp_e2e; finishing answers with a launch path and writes nothing. Without E2E_SETUP_ADDR it is skipped.
func TestServeSetupForPlaywright(t *testing.T) {
	addr := os.Getenv("E2E_SETUP_ADDR")
	if addr == "" {
		t.Skip("E2E_SETUP_ADDR not set")
	}
	repos := map[string][][3]string{ // name, topics (JSON items), private
		"octo": {{"notes", "", "true"}, {"dossier", "", "true"}},
		"acme": {{"whydah-sso", `"iam"`, ""}, {"whydah-admin", `"iam"`, ""}, {"whydah-ui", "", ""}, {"whydah-spa", "", ""},
			{"kcp-triage", `"kcp"`, ""}, {"kcp-agent", "", ""}, {"kcp-hooks", "", ""}, {"kcp-skill", "", ""}, {"tools", "", ""}, {"docs", "", ""}},
	}
	list := func(w http.ResponseWriter, owner string) {
		var parts []string
		for _, r := range repos[owner] {
			parts = append(parts, fmt.Sprintf(`{"name":%q,"private":%t,"fork":false,"archived":false,"pushed_at":"2026-11-01T10:00:00Z","topics":[%s],"owner":{"login":%q}}`,
				r[0], r[2] == "true", r[1], owner))
		}
		fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
	}
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer ghp_e2e" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `{"login":"octo","public_repos":0,"owned_private_repos":2}`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `[{"login":"acme"}]`)
		}
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `{"login":"acme","public_repos":10,"total_private_repos":0}`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			list(w, "octo")
		}
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}/repos", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			time.Sleep(300 * time.Millisecond) // long enough for the progress bar to be seen
			list(w, r.PathValue("org"))
		}
	})
	gh := httptest.NewServer(mux)
	defer gh.Close()

	s := setup.New(setup.Config{Addr: addr, Code: "e2e-entry-code", Host: gh.URL,
		CLIToken: func(context.Context, string) (string, error) { return "ghp_e2e", nil },
		Finish:   func(context.Context, setup.Result) (string, error) { return "/auth/launch?code=e2e", nil }})
	defer s.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}
