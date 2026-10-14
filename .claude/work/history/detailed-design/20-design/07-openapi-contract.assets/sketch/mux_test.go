package scp_test

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"example.com/scp/internal/catalog/catalogapi"
	"example.com/scp/internal/commits/commitsapi"
	"example.com/scp/internal/docs/docsapi"
	"example.com/scp/internal/live/liveapi"
	"example.com/scp/internal/manifests/manifestsapi"
	"example.com/scp/internal/ops/opsapi"
	"example.com/scp/internal/overview/overviewapi"
	"example.com/scp/internal/pages/pagesapi"
)

// rec is the smallest possible stand-in for routes.Recorder: it satisfies the generated ServeMux interface
// (HandleFunc plus http.Handler, verified on oapi-codegen v2.8.0) and remembers every pattern it was given.
type rec struct {
	*http.ServeMux
	patterns []string
}

func (r *rec) HandleFunc(p string, h func(http.ResponseWriter, *http.Request)) {
	r.patterns = append(r.patterns, p)
	r.ServeMux.HandleFunc(p, h)
}

type (
	cat struct {
		catalogapi.StrictServerInterface
	}
	com struct {
		commitsapi.StrictServerInterface
	}
	doc struct{ docsapi.StrictServerInterface }
	liv struct{ liveapi.StrictServerInterface }
	man struct {
		manifestsapi.StrictServerInterface
	}
	ops struct{ opsapi.StrictServerInterface }
	ovw struct {
		overviewapi.StrictServerInterface
	}
	pag struct{ pagesapi.StrictServerInterface }
)

// TestRegistrationNoConflict registers every generated pattern of the admin document on one ServeMux (ServeMux
// panics on a conflict) and writes the sorted pattern list to patterns.txt for the bundled-paths comparison.
func TestRegistrationNoConflict(t *testing.T) {
	m := &rec{ServeMux: http.NewServeMux()}
	catalogapi.HandlerFromMux(catalogapi.NewStrictHandler(cat{}, nil), m)
	commitsapi.HandlerFromMux(commitsapi.NewStrictHandler(com{}, nil), m)
	docsapi.HandlerFromMux(docsapi.NewStrictHandler(doc{}, nil), m)
	liveapi.HandlerFromMux(liveapi.NewStrictHandler(liv{}, nil), m)
	manifestsapi.HandlerFromMux(manifestsapi.NewStrictHandler(man{}, nil), m)
	opsapi.HandlerFromMux(opsapi.NewStrictHandler(ops{}, nil), m)
	overviewapi.HandlerFromMux(overviewapi.NewStrictHandler(ovw{}, nil), m)
	pagesapi.HandlerFromMux(pagesapi.NewStrictHandler(pag{}, nil), m)
	m.ServeMux.HandleFunc("GET /api/v1/openapi.yaml", func(http.ResponseWriter, *http.Request) {})
	sort.Strings(m.patterns)
	if len(m.patterns) != 23 {
		t.Fatalf("want 23 generated patterns (24 operations minus the root-owned openapi.yaml), got %d", len(m.patterns))
	}
	if err := os.WriteFile("patterns.txt", []byte(strings.Join(m.patterns, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
