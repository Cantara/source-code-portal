package catalog

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestLegacy(t *testing.T) {
	l := &lister{repos: map[string][]snapshot.Repo{
		"acme": {pub(1, "Whydah-Core"), pub(2, "whydah-sso")},
		"beta": {func() snapshot.Repo { r := pub(3, "tool"); r.Owner = "beta"; return r }()},
	}}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	if _, _, err := runAndPublish(t, newCatalog(t, l), p, store.Item{State: snapshot.StateNever}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	for _, r := range Legacy(func(context.Context) *snapshot.Snapshot { return p.Current() }) {
		mux.Handle(r.Pattern, r.Handler)
	}
	for _, tc := range []struct {
		method, path string
		status       int
		location     string
	}{
		{"GET", "/dashboard", 308, "/"},
		{"GET", "/index", 308, "/"},
		{"GET", "/group/Whydah", 308, "/groups/whydah"},
		{"GET", "/group/Wh-ydah", 308, "/groups/whydah"},
		{"GET", "/group/nope", 404, ""},
		{"GET", "/contents/Acme/Whydah-Core/master", 308, "/repos/acme/whydah-core"},
		{"GET", "/commits/Acme/Whydah-Core", 308, "/repos/acme/whydah-core/commits"},
		{"GET", "/commits/Whydah", 308, "/groups/whydah/commits"},
		{"GET", "/commits/tool", 308, "/repos/beta/tool/commits"},
		{"GET", "/commits/nope", 404, ""},
		{"GET", "/badge/license/Whydah-Core/master", 308, "/badges/repos/acme/whydah-core/license.svg"},
		{"GET", "/badge/shields-issues/tool/master", 308, "/badges/repos/beta/tool/issues.svg"},
		{"GET", "/badge/shields-commits/whydah-sso/master", 308, "/badges/repos/acme/whydah-sso/last-commit.svg"},
		{"GET", "/badge/shields-releases/whydah-sso/master", 308, "/badges/repos/acme/whydah-sso/release.svg"},
		{"GET", "/badge/license/nope/master", 404, ""},
		{"GET", "/badge/jenkins/tool/master", 410, ""},
		{"GET", "/badge/snyk/tool/master", 410, ""},
		{"GET", "/wiki", 410, ""},
		{"GET", "/wiki/Page", 410, ""},
		{"POST", "/github/webhook", 410, ""},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Location") != tc.location {
			t.Errorf("%s %s = %d %q, want %d %q", tc.method, tc.path, w.Code, w.Header().Get("Location"), tc.status, tc.location)
		}
	}
}
