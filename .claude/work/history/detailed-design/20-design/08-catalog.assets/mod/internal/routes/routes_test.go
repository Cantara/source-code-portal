package routes_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/routes"
)

// register wires every route of a table into a fresh mux with a handler that answers "<name>|<pathvalues>".
func register(t testing.TB, tb *routes.Table) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	rec := tb.Mux(mux)
	for _, r := range tb.All() {
		r := r
		h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var vals []string
			for _, n := range r.Wildcards() {
				vals = append(vals, n+"="+req.PathValue(n))
			}
			fmt.Fprintf(w, "%s|%s", r.Name, strings.Join(vals, ","))
		})
		switch {
		case r.Class == routes.ClassAPI:
			rec.HandleFunc(r.FullPattern(), h)
		default:
			tb.Handle(mux, r.Name, h)
		}
	}
	return mux
}

func TestPublicTableRegistersAndVerifies(t *testing.T) {
	tb := routes.Public()
	register(t, tb)
	if err := tb.Verify(); err != nil {
		t.Fatal(err)
	}
	t.Logf("public routes: %d", len(tb.All()))
}

func TestAdminTableRegistersAndVerifies(t *testing.T) {
	tb := routes.Admin()
	register(t, tb)
	if err := tb.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyReportsMissingAndShadowed(t *testing.T) {
	tb := routes.Public()
	mux := http.NewServeMux()
	tb.Handle(mux, "probe.live", http.NotFoundHandler())
	err := tb.Verify()
	ve, ok := err.(*routes.VerifyError)
	if !ok || len(ve.Missing) != len(tb.All())-1 {
		t.Fatalf("want %d missing, got %v", len(tb.All())-1, err)
	}
}

func TestHandleMisuse(t *testing.T) {
	for name, f := range map[string]func(*routes.Table, *http.ServeMux){
		"unknown name": func(tb *routes.Table, m *http.ServeMux) { tb.Handle(m, "nope", http.NotFoundHandler()) },
		"api route":    func(tb *routes.Table, m *http.ServeMux) { tb.Handle(m, "api.site", http.NotFoundHandler()) },
		"nil handler":  func(tb *routes.Table, m *http.ServeMux) { tb.Handle(m, "probe.live", nil) },
		"twice": func(tb *routes.Table, m *http.ServeMux) {
			tb.Handle(m, "probe.live", http.NotFoundHandler())
			tb.Handle(m, "probe.live", http.NotFoundHandler())
		},
		"two muxes": func(tb *routes.Table, m *http.ServeMux) {
			tb.Handle(m, "probe.live", http.NotFoundHandler())
			tb.Handle(http.NewServeMux(), "probe.ready", http.NotFoundHandler())
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			f(routes.Public(), http.NewServeMux())
		}()
	}
}

func TestRecorderRejectsUnknownPattern(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a pattern outside the table")
		}
	}()
	tb := routes.Public()
	tb.Mux(http.NewServeMux()).HandleFunc("GET /api/v1/not-in-table", func(http.ResponseWriter, *http.Request) {})
}

func resolve(mux *http.ServeMux, method, target string) string {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	if rec.Code >= 300 && rec.Code < 400 {
		return fmt.Sprintf("%d->%s", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Code >= 400 {
		return fmt.Sprintf("%d", rec.Code)
	}
	return rec.Body.String()
}

func TestResolution(t *testing.T) {
	mux := register(t, routes.Public())
	cases := []struct{ method, target, want string }{
		{"GET", "/", "spa.root|"},
		{"GET", "/dashboard", "spa.dashboard|"},
		{"GET", "/foo", "spa.fallback|path=foo"},
		{"GET", "/api/v1/nope", "spa.fallback|path=api/v1/nope"}, // webapp must 404 reserved prefixes (ProblemMux rewrites for /api/)
		{"GET", "/repos/foo", "negotiated.repo|repo=foo"},
		{"GET", "/repos/three.js", "negotiated.repo|repo=three.js"},
		{"GET", "/repos/foo/commits", "spa.repo.commits|repo=foo"},
		{"GET", "/repos/foo/whatever", "negotiated.repo.rest|repo=foo,rest=whatever"},
		{"GET", "/repos/foo/docs/a/b.md", "doc.md|repo=foo,path=a/b.md"},
		{"GET", "/repos/foo/readme.md", "readme.md|repo=foo"},
		{"GET", "/repos/foo/llms.txt", "llms.repo|repo=foo"},
		{"GET", "/groups/g", "negotiated.group|groupId=g"},
		{"GET", "/groups/g/commits", "spa.group.commits|groupId=g"},
		{"GET", "/groups/g/llms-full.txt", "llms.group.full|groupId=g"},
		{"GET", "/badges/foo/issues.svg", "badge|repo=foo,file=issues.svg"},
		{"GET", "/badges/groups/g/repos.svg", "badge.group|groupId=g,file=repos.svg"},
		{"GET", "/badges/groups/issues.svg", "badge|repo=groups,file=issues.svg"},
		{"GET", "/media/ext/sig/id", "media.ext|sig=sig,id=id"},
		{"GET", "/media/foo/main/a/b.png", "media.repo|repo=foo,ref=main,path=a/b.png"},
		{"GET", "/media/ext/a/b/c", "media.repo|repo=ext,ref=a,path=b/c"}, // why "ext" is a reserved slug
		{"POST", "/webhooks/github", "webhook|"},
		{"POST", "/webhooks/github/acme", "webhook.source|source=acme"},
		{"POST", "/dashboard", "405"},
		{"GET", "/webhooks/github", "spa.fallback|path=webhooks/github"},
		{"GET", "/api/v1/repos/foo/docs/a%2Fb.md", "api.docs.get|repo=foo,docId=a/b.md"},
	}
	for _, c := range cases {
		if got := resolve(mux, c.method, c.target); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.method, c.target, got, c.want)
		}
	}
}

func TestSlugAliasGap(t *testing.T) {
	// An explicit /repos/{repo}/commits wins over the /repos/{repo}/{rest...} catch-all, so alias redirects placed on
	// the catch-all never see /repos/<old-slug>/commits: wiring must wrap every route with a {repo} wildcard.
	mux := register(t, routes.Public())
	if got := resolve(mux, "GET", "/repos/old-slug/commits"); !strings.HasPrefix(got, "spa.repo.commits") {
		t.Fatalf("got %q", got)
	}
	var withSlug []string
	for _, r := range routes.Public().All() {
		if r.HasSlug() && r.Class != routes.ClassAPI {
			withSlug = append(withSlug, r.Name)
		}
	}
	t.Logf("routes with a {repo} wildcard (non-API): %v", withSlug)
}

func TestBuildersRoundTrip(t *testing.T) {
	mux := register(t, routes.Public())
	for _, v := range []string{"foo", "a b", "a/b", "a?b", "a#b", "ü", "a%2Fb", "x;y", "[::1]", "a+b", "%", "..a", "a..", ".github"} {
		got := resolve(mux, "GET", routes.Repo(v))
		want := "negotiated.repo|repo=" + v
		if got != want {
			t.Errorf("Repo(%q)=%q resolved to %q, want %q", v, routes.Repo(v), got, want)
		}
		got = resolve(mux, "GET", routes.RepoCommits(v))
		if got != "spa.repo.commits|repo="+v {
			t.Errorf("RepoCommits(%q) resolved to %q", v, got)
		}
		got = resolve(mux, "GET", routes.Badge(v, "issues"))
		if got != "badge|repo="+v+",file=issues.svg" {
			t.Errorf("Badge(%q) resolved to %q", v, got)
		}
	}
	// dot segments are not representable as a slug: ServeMux cleans them before matching.
	for _, v := range []string{".", ".."} {
		if got := resolve(mux, "GET", routes.Repo(v)); strings.HasPrefix(got, "negotiated.repo") {
			t.Errorf("Repo(%q) reached the handler: %q", v, got)
		}
	}
	// multi-segment document paths are cleaned lexically inside the root
	for in, want := range map[string]string{"a/b.md": "a/b.md", "../../etc/passwd": "etc/passwd", "a/../b": "b", "/a": "a", "a b/c#d": "a%20b/c%23d", "": "", ".": ""} {
		if got := routes.RepoDoc("r", in); got != "/repos/r/docs/"+want {
			t.Errorf("RepoDoc(%q)=%q want suffix %q", in, got, want)
		}
	}
	if got := routes.Packages("pkg:maven/org.acme/x@1.0?type=jar"); got != "/packages?purl=pkg%3Amaven%2Forg.acme%2Fx%401.0%3Ftype%3Djar" {
		t.Errorf("Packages = %q", got)
	}
	if got, _ := routes.Absolute("https://scp.example.org/", routes.Repo("a b")); got != "https://scp.example.org/repos/a%20b" {
		t.Errorf("Absolute = %q", got)
	}
	if _, err := routes.Absolute("https://u:p@host", "/x"); err == nil {
		t.Error("userinfo must be refused")
	}
}

func TestReservedPrefixes(t *testing.T) {
	want := []string{"/.well-known/", "/_frame/", "/api/", "/assets/", "/badges/", "/media/", "/schemas/", "/webhooks/"}
	if got := routes.Public().ReservedPrefixes(); !slices.Equal(got, want) {
		t.Errorf("public reserved prefixes = %v\nwant %v", got, want)
	}
	wantAdmin := []string{"/.well-known/", "/api/", "/assets/", "/debug/"}
	if got := routes.Admin().ReservedPrefixes(); !slices.Equal(got, wantAdmin) {
		t.Errorf("admin reserved prefixes = %v want %v", got, wantAdmin)
	}
}

func TestSPAShapes(t *testing.T) {
	want := []string{"/", "/commits", "/dashboard", "/dependencies", "/groups/{groupId}", "/groups/{groupId}/commits", "/packages", "/pages/{slug}", "/repos/{repo}", "/repos/{repo}/commits", "/repos/{repo}/dependencies", "/repos/{repo}/docs/{path...}"}
	got := routes.Public().SPAShapes()
	got = slices.DeleteFunc(got, func(s string) bool { return s == "/{$}" })
	got = append(got, "/")
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("shapes = %v\nwant   %v", got, want)
	}
	if got := routes.Admin().SPAShapes(); !slices.Equal(got, []string{"/status"}) {
		t.Errorf("admin shapes = %v", got)
	}
}

func TestEveryRouteHasSampleAndConventions(t *testing.T) {
	for _, tb := range []*routes.Table{routes.Public(), routes.Admin()} {
		for _, r := range tb.All() {
			u, err := url.Parse(r.Sample())
			if err != nil || !strings.HasPrefix(u.Path, "/") {
				t.Errorf("%s: bad sample %q", r.Name, r.Sample())
			}
			for _, w := range r.Wildcards() {
				switch w {
				case "repo", "groupId", "slug", "file", "ref", "path", "rest", "sig", "id", "docId", "source", "kind", "branch", "org", "page", "first", "second":
				default:
					t.Errorf("%s: wildcard %q is outside the documented vocabulary", r.Name, w)
				}
			}
		}
	}
}

func TestWithout(t *testing.T) {
	tb := routes.Public().Without("api.events", "frame.mermaid")
	if _, ok := tb.Lookup("api.events"); ok {
		t.Fatal("not removed")
	}
	mux := http.NewServeMux()
	rec := tb.Mux(mux)
	for _, r := range tb.All() {
		if r.Class == routes.ClassAPI {
			rec.HandleFunc(r.FullPattern(), func(http.ResponseWriter, *http.Request) {})
		}
	}
	err := tb.Verify()
	if err == nil || !strings.Contains(err.Error(), "no handler registered") {
		t.Fatalf("expected missing-handler error, got %v", err)
	}
	if strings.Contains(err.Error(), "api.events") {
		t.Fatal("removed route reported as missing")
	}
}

func BenchmarkVerifyPublic(b *testing.B) {
	tb := routes.Public()
	register(b, tb)
	b.ReportAllocs()
	for b.Loop() {
		if err := tb.Verify(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPublicTable(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = routes.Public()
	}
}

// TestPublicHasNoManagementRoutes: pprof, metrics and the admin API exist only on the admin listener (GR-17).
func TestPublicHasNoManagementRoutes(t *testing.T) {
	for _, r := range routes.Public().All() {
		if r.Class == routes.ClassAdmin || strings.HasPrefix(r.Pattern, "/debug") || r.Pattern == "/metrics" || strings.Contains(r.Pattern, "/admin/") || r.Pattern == "/api/v1/status" || strings.Contains(r.Pattern, "echo") {
			t.Errorf("public table has a management route: %s %s", r.Name, r.Pattern)
		}
	}
	for _, r := range routes.Admin().All() {
		if strings.HasPrefix(r.Pattern, "/badges") || strings.HasPrefix(r.Pattern, "/webhooks") || r.Pattern == "/api/v1/site" {
			t.Errorf("admin table has a public-site route: %s %s", r.Name, r.Pattern)
		}
	}
}

// TestCounts pins the sizes quoted by the design document.
func TestCounts(t *testing.T) {
	pub, adm := routes.Public(), routes.Admin()
	got := map[routes.Class]int{}
	for _, r := range pub.All() {
		got[r.Class]++
	}
	want := map[routes.Class]int{routes.ClassAPI: 22, routes.ClassSPA: 13, routes.ClassServer: 29}
	for c, n := range want {
		if got[c] != n {
			t.Errorf("public %s routes = %d, want %d", c, got[c], n)
		}
	}
	if len(pub.All()) != 64 || len(adm.All()) != 18 {
		t.Errorf("public %d, admin %d", len(pub.All()), len(adm.All()))
	}
}
