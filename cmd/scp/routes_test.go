package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// notWalked are the routes the leak walk does not fetch as a GET, each with the reason and the test that holds it.
var notWalked = map[string]string{ //nolint:gochecknoglobals // test table
	"POST /github/webhook":                "the Java portal's webhook address: gone, answers 410 without reading the body (legacy.go)",
	"POST /webhooks/github":               "the signed receiver, held by the webhook tests and run_test.go",
	"POST /auth/token":                    "a token for a session: signin_test.go",
	"POST /api/v1/assistant/chat":         "assistant_test.go, as the public and a private viewer",
	"POST /api/v1/admin/assistant/test":   "admin only: TestRequireAuth and the admin tests",
	"POST /api/v1/tools/{name}":           "TestPublicTwin calls every tool as the public and with a token",
	"POST /mcp":                           "TestPublicTwin and mcp_test.go call every tool through the SDK's client",
	"GET /mcp":                            "the same endpoint, served by the SDK's handler",
	"DELETE /mcp":                         "the same endpoint, served by the SDK's handler",
	"PUT /api/v1/admin/layouts/{page}":    "admin and same origin: the layout tests",
	"DELETE /api/v1/admin/layouts/{page}": "admin and same origin: the layout tests",
	"PUT /api/v1/admin/announcements":     "admin and same origin: the announce tests",
	"GET /api/v1/admin/status":            "admin only, asked on the public listener by edgePaths as /api/v1/status",
	"GET /auth/login":                     "the sign-in redirect: signin_test.go",
	"GET /auth/callback":                  "the sign-in return: signin_test.go",
	"GET /theme/{file...}":                "the operator's own files, nothing of the catalogue: theme_test.go",
	"POST /auth/logout":                   "signin_test.go",
}

// Every route the shared site registers is either walked by the leak and twin tests as the public or named in notWalked with the
// test that holds it: a new route cannot be added without being put in front of one of them.
func TestEveryRouteIsWalkedOrNamed(t *testing.T) {
	// Every optional feature on, so that its routes are registered too: sign-in, the webhook receiver and the assistant.
	cfg := modeConfigWith(t, filepath.Join(t.TempDir(), "routes.db"), func(file func(name, body string) string) string {
		return "webhook: {secret_file: " + file("hook", strings.Repeat("w", 40)) + "}\n" +
			"llm: {provider: anthropic, model: claude-test, base_url: \"http://127.0.0.1:1\", api_key_file: " + file("llm", "sk-test") + "}\n" +
			"assistant: {access: signed-in}\n" +
			"web: {theme_dir: " + filepath.Dir(file("theme.css", ":root{}")) + "}\n"
	})
	s, err := standUp(t, cfg, deps{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	for _, r := range s.routes {
		mux.HandleFunc(r, func(http.ResponseWriter, *http.Request) {})
	}
	walked := map[string]bool{}
	for _, p := range append(publicPaths(t), edgePaths...) {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		if _, pattern := mux.Handler(req); pattern != "" {
			walked[pattern] = true
		}
	}
	for _, r := range s.routes {
		reason, named := notWalked[r]
		switch {
		case walked[r] && named:
			t.Errorf("%s is walked and also listed in notWalked: drop the entry", r)
		case !walked[r] && !named:
			t.Errorf("%s is not walked by the leak test: add a path to publicPaths, or name it in notWalked with the test that holds it", r)
		case named && strings.TrimSpace(reason) == "":
			t.Errorf("%s: an exemption needs its reason", r)
		}
	}
	for r := range notWalked {
		if !slices.Contains(s.routes, r) {
			t.Errorf("notWalked lists %s, which is not registered", r)
		}
	}
}
