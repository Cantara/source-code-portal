// Package wiring_test checks the seams between this unit and the route table of design unit 01 the way cmd/scp will.
package wiring_test

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"testing"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/routes"
	"github.com/cantara/source-code-portal/internal/webapp"
	"github.com/cantara/source-code-portal/web"
)

// D14: the placeholder routes.json equals the SPA-class shapes of routes.Public() (the root redirect "/{$}" is not a shape).
func TestPlaceholderManifestEqualsRouteTable(t *testing.T) {
	fsys, err := web.FS()
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(fsys, "routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Routes []struct {
			Pattern   string   `json:"pattern"`
			Listeners []string `json:"listeners"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range m.Routes {
		if slices.Contains(r.Listeners, "public") {
			got = append(got, r.Pattern)
		}
	}
	sort.Strings(got)
	want := slices.DeleteFunc(routes.Public().SPAShapes(), func(p string) bool { return p == "/{$}" })
	if !slices.Equal(got, want) {
		t.Fatalf("placeholder routes.json differs from routes.Public().SPAShapes():\n got  %v\n want %v", got, want)
	}
}

// The wiring of design 06 section 2.5 against the real table: Reserved comes from the table, the shell answers SPA shapes
// and never answers under a reserved prefix.
func TestReservedPrefixesFromTable(t *testing.T) {
	tab := routes.Public()
	fsys, _ := web.FS()
	h, err := webapp.New(fsys, webapp.Options{Reserved: tab.ReservedPrefixes()})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/dashboard": 200, "/repos/next.js": 200, "/repos/a/docs/b/c.md": 200, "/nope": 404,
		"/api/v1/nope": 404, "/badges/x/y.svg": 404, "/media/x": 404, "/webhooks/x": 404, "/x.map": 404,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
		if want == 404 && w.Header().Get("Content-Type") != "text/plain; charset=utf-8" && path != "/nope" {
			t.Errorf("%s: a reserved or file-like 404 must be plain text, got %q", path, w.Header().Get("Content-Type"))
		}
	}
	t.Logf("reserved prefixes from routes: %v", tab.ReservedPrefixes())
	_ = http.StatusOK
}

// D6: the kernel status vocabulary equals the generated API enum, so NewFreshness can take Status.String().
func TestKernelStatusIsTheAPIEnum(t *testing.T) {
	for _, st := range []freshness.Status{freshness.StatusUnknown, freshness.StatusFresh, freshness.StatusStale, freshness.StatusError} {
		f := httpapi.NewFreshness(httpapi.FreshnessView{Status: st.String()})
		if !f.Status.Valid() {
			t.Errorf("%v renders %q, which is not a member of the generated FreshnessStatus enum", st, f.Status)
		}
	}
	_ = openapi.FreshnessStatusFresh
}
