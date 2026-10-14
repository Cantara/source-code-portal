package wiring_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/routes"
	"github.com/cantara/source-code-portal/internal/webapp"
	"github.com/cantara/source-code-portal/web"
)

// Verification (V4/V5): the wiring of design 06 section 2.5 against the real table and the real ServeMux.
func TestWiringAgainstTable(t *testing.T) {
	tab := routes.Public()
	mux := http.NewServeMux()
	// V4: api.openapi is an API-class route: Table.Handle refuses it, the recording mux accepts it.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Table.Handle(api.openapi) must panic (API class)")
			}
		}()
		tab.Handle(mux, "api.openapi", httpapi.Static(httpapi.ContentTypeYAML, []byte("x")))
	}()
	rec := tab.Mux(mux)
	rec.HandleFunc("GET /api/v1/openapi.yaml", httpapi.Static(httpapi.ContentTypeYAML, []byte("x")).ServeHTTP)

	fsys, _ := web.FS()
	spa, err := webapp.New(fsys, webapp.Options{Reserved: tab.ReservedPrefixes()})
	if err != nil {
		t.Fatal(err)
	}
	// V5: Verify() needs a handler for EVERY table entry. Section 7.1 listed only spa.root, assets and the catch-all.
	for _, n := range []string{"spa.root", "spa.fallback", "assets", "theme.init", "favicon", "manifest", "frame.mermaid"} {
		tab.Handle(mux, n, spa)
	}
	err = tab.Verify()
	if err == nil || !strings.Contains(err.Error(), "spa.dashboard") {
		t.Fatalf("Verify must name the unregistered SPA entries, got %v", err)
	}
	// Register every ClassSPA route that webapp owns, plus the csp report route; what remains is other units' routes.
	for _, r := range tab.Routes(routes.ClassSPA) {
		if r.Owner == "webapp" {
			if r.Name == "spa.root" || r.Name == "spa.fallback" {
				continue
			}
			tab.Handle(mux, r.Name, spa)
		}
	}
	tab.Handle(mux, "csp.report", spa.CSPReports())
	if err := tab.Verify(); err == nil || strings.Contains(err.Error(), "spa.dashboard") || strings.Contains(err.Error(), "csp.report") {
		t.Fatalf("webapp-owned entries must be satisfied now: %v", err)
	}

	// V7: what ServeMux itself does before webapp is reached (Go 1.27.1).
	type tc struct {
		method, target string
		code           int
		loc, allow     string
	}
	for _, c := range []tc{
		{"GET", "//", 307, "/", ""},        // not a 404: ServeMux cleans the path, so no handler sees an uncleaned one
		{"GET", "/repos/..", 307, "/", ""}, // cleaned
		{"GET", "/dashboard//", 307, "/dashboard/", ""},
		{"GET", "/dashboard/", 308, "", ""},          // webapp: trailing slash on a shape
		{"POST", "/dashboard", 405, "", "GET, HEAD"}, // ServeMux answers (text/plain), not webapp
		{"OPTIONS", "/dashboard", 405, "", "GET, HEAD"},
		{"GET", "/dashboard/anything", 404, "", ""},
		{"GET", "/healthz", 404, "", ""}, // Q-11: /livez and /readyz are the only probes (GR-41)
		{"GET", "/no-such-route", 404, "", ""},
		{"GET", "/csp-reports", 404, "", ""}, // GET falls to the catch-all (shell 404), POST is the report endpoint
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(c.method, c.target, nil))
		if w.Code != c.code || (c.loc != "" && w.Header().Get("Location") != c.loc) || (c.allow != "" && w.Header().Get("Allow") != c.allow) {
			t.Errorf("%s %s: %d loc=%q allow=%q, want %d %q %q", c.method, c.target, w.Code, w.Header().Get("Location"), w.Header().Get("Allow"), c.code, c.loc, c.allow)
		}
	}
}
