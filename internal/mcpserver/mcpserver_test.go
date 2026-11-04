package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// TestGuard: a session cookie's viewer reaches the tools as the public (or is refused without a public view); a token's keeps its
// view; a browser of another site is refused, the portal's own pages and agents without an Origin pass.
func TestGuard(t *testing.T) {
	var seen httpapi.Viewer
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = httpapi.ViewerOf(r.Context()) })
	serve := func(o Options, v httpapi.Viewer, origin string) int {
		seen = httpapi.Viewer{Private: true} // a stale value must not pass for the next call's
		r := httptest.NewRequestWithContext(httpapi.WithViewer(t.Context(), v), http.MethodPost, "http://scp.example/mcp", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		guard(o, next).ServeHTTP(w, r)
		return w.Code
	}
	o := Options{PublicURL: "https://scp.example"}
	session := httpapi.Viewer{Identity: httpapi.Identity{User: "ann", Via: "oidc"}, Private: true}
	token := httpapi.Viewer{Identity: httpapi.Identity{User: "token:bot", Via: "token"}, Private: true}
	if code := serve(o, session, ""); code != 200 || seen.Private || seen.SignedIn() {
		t.Errorf("a session = %d, seen %+v", code, seen)
	}
	if code := serve(Options{PublicURL: o.PublicURL, RequireAuth: true}, session, ""); code != http.StatusUnauthorized {
		t.Errorf("a session without a public view = %d", code)
	}
	if code := serve(o, token, ""); code != 200 || !seen.Private {
		t.Errorf("a token = %d, seen %+v", code, seen)
	}
	if code := serve(o, token, "https://evil.example"); code != http.StatusForbidden {
		t.Errorf("another origin = %d", code)
	}
	if code := serve(o, token, "https://SCP.example"); code != 200 {
		t.Errorf("the portal's origin = %d", code)
	}
	if code := serve(Options{}, token, "http://scp.example"); code != 200 {
		t.Errorf("without a public URL, the request's own host = %d", code)
	}
}
