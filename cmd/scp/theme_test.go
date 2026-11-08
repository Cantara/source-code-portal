package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestThemeIsServedToEveryone: the files of web.theme_dir are the portal's own, so they are open even when sign-in is required (the
// sign-in page needs its theme), and only stylesheets and pictures are served, never a listing or another kind of file.
func TestThemeIsServedToEveryone(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"theme.css": ":root{--primary:oklch(0.5 0.2 20)}", "logo.svg": "<svg xmlns='http://www.w3.org/2000/svg'/>", "run.js": "alert(1)", ".env": "x",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	get, _ := privatePortalAt(t, "access: {private_groups: [staff], require_auth: true}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}\n"+
		"web: {theme_dir: "+dir+", product_name: Acme Code}\n")
	if code, b, h := get("/theme/theme.css?v=1"); code != http.StatusOK || !strings.Contains(b, "--primary") || h.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("anonymous stylesheet: %d %q %q", code, b, h.Get("Content-Type"))
	} else if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("the files are served under the API's policy, not a page's: %q", csp)
	}
	if code, _, h := get("/theme/logo.svg"); code != http.StatusOK || h.Get("Content-Type") != "image/svg+xml" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("anonymous logo: %d %q %q", code, h.Get("Content-Type"), h.Get("Content-Security-Policy"))
	}
	for _, p := range []string{"/theme/", "/theme/run.js", "/theme/.env", "/theme/missing.css", "/theme/%2e%2e/scp.yaml"} {
		if code, _, _ := get(p); code != http.StatusNotFound {
			t.Errorf("anonymous %s = %d, want 404", p, code)
		}
	}
	if code, _, _ := get("/llms.txt"); code != http.StatusUnauthorized {
		t.Errorf("the rest of the portal stays closed: %d", code)
	}
}

// TestNoThemeIsNoRoute: without web.theme_dir the address is not served (the test portal has no web UI, which answers 503 for a page).
func TestNoThemeIsNoRoute(t *testing.T) {
	get, _ := publicTwin(t, "")
	if code, _, _ := get("/theme/theme.css"); code != http.StatusNotFound && code != http.StatusServiceUnavailable {
		t.Errorf("/theme/theme.css without a theme = %d", code)
	}
}
