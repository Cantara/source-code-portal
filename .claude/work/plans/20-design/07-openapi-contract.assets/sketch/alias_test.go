package overview_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/overview"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

func repo(slug string, v scm.Visibility) freshness.Value[scm.Repository] {
	return freshness.Value[scm.Repository]{V: scm.Repository{Slug: scm.Slug(slug), Visibility: v}}
}

func chain(t *testing.T) http.Handler {
	repos := readmodel.NewSlotForTest[freshness.Value[scm.Repository]]("catalog.repo")
	snap := readmodel.NewSnapshotForTest(map[string]map[readmodel.EntryID]any{"catalog.repo": {
		"billing-service": repo("billing-service", scm.VisPublic),
		"secret-ledger":   repo("secret-ledger", scm.VisPrivate),
		"reused":          repo("reused", scm.VisPublic), // an old name that a new repository now owns
	}})
	aliases := map[scm.Slug]scm.Slug{"billing-svc": "billing-service", "old-ledger": "secret-ledger", "reused": "billing-service"}
	src := overview.AliasSource{
		Snapshot: func() *readmodel.Snapshot { return snap },
		Aliases:  func(*readmodel.Snapshot) (map[scm.Slug]scm.Slug, bool) { return aliases, true },
		Repos:    repos,
		Authz:    authz.PublicOnly{},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	return overview.AliasRedirect(src)(next)
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

func TestRenamedRepositoryRedirects(t *testing.T) {
	h := chain(t)
	for _, tc := range []struct{ method, in, want string }{
		{"GET", "/api/v1/repos/billing-svc", "/api/v1/repos/billing-service"},
		{"GET", "/api/v1/repos/billing-svc/docs", "/api/v1/repos/billing-service/docs"},
		{"HEAD", "/api/v1/repos/billing-svc/docs/docs~install.md", "/api/v1/repos/billing-service/docs/docs~install.md"},
		{"GET", "/api/v1/repos/billing-svc/docs/docs~Release%2520Notes.md?x=1&y=%20z", "/api/v1/repos/billing-service/docs/docs~Release%2520Notes.md?x=1&y=%20z"},
		{"GET", "/api/v1/repos/billing-svc/dependencies?scope=runtime&scope=dev", "/api/v1/repos/billing-service/dependencies?scope=runtime&scope=dev"},
	} {
		rr := get(h, tc.method, tc.in)
		if rr.Code != 308 || rr.Header().Get("Location") != tc.want || rr.Header().Get("Content-Type") != "application/problem+json" || rr.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: %d Location=%q CT=%q", tc.method, tc.in, rr.Code, rr.Header().Get("Location"), rr.Header().Get("Content-Type"))
		}
	}
	var p struct {
		Code   string `json:"code"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(get(h, "GET", "/api/v1/repos/billing-svc").Body.Bytes(), &p); err != nil || p.Code != "repo-renamed" || p.Status != 308 {
		t.Fatalf("body: %+v %v", p, err)
	}
}

// An alias of a repository the caller may not see, a current slug, an unknown slug and a malformed slug all fall through to the
// next handler: the redirect never reveals existence or the new name (GR-35).
func TestVisibilityFirst(t *testing.T) {
	h := chain(t)
	for _, in := range []string{
		"/api/v1/repos/old-ledger", "/api/v1/repos/old-ledger/docs", // alias of a private repository
		"/api/v1/repos/billing-service", "/api/v1/repos/reused", // a current slug always wins over an alias of the same name
		"/api/v1/repos/nope", "/api/v1/repos/..", "/api/v1/repos/%2e%2e/docs", "/api/v1/repos/A%2FB", "/api/v1/repos/Billing-Svc", // unknown, dot segments, separators, upper case
		"/api/v1/groups/billing-svc", "/api/v1/repos", // other routes
	} {
		if rr := get(h, "GET", in); rr.Code != 404 || rr.Header().Get("Location") != "" {
			t.Errorf("%s: want the next handler's 404 without Location, got %d %q", in, rr.Code, rr.Header().Get("Location"))
		}
	}
	if rr := get(h, "POST", "/api/v1/repos/billing-svc"); rr.Code != 404 {
		t.Errorf("POST must fall through, got %d", rr.Code)
	}
}
