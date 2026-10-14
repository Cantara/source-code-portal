package badges

import (
	"strings"
	"testing"
)

// V-09: README embeds often carry the display case of the repository name; slugs are canonical lower-case, so the
// handler folds the path value before the alias and existence lookup. Hidden stays hidden.
func TestHandlerFoldsTheRepositoryCase(t *testing.T) {
	_, mux := newSvc(t, false)
	a, b := get(mux, "/badges/API/issues.svg"), get(mux, "/badges/api/issues.svg")
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatalf("mixed-case slug: %d", a.Code)
	}
	if rec := get(mux, "/badges/Old-API/issues.svg"); rec.Code != 200 || rec.Body.String() != b.Body.String() {
		t.Fatalf("mixed-case alias of a renamed repository: %d", rec.Code)
	}
	if rec := get(mux, "/badges/SECRET/issues.svg"); rec.Code != 404 {
		t.Fatalf("a hidden repository stays hidden under any case: %d", rec.Code)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	_, err := New(Deps{Cfg: DefaultConfig()})
	if err == nil || !strings.Contains(err.Error(), "Deps.Facts") || !strings.Contains(err.Error(), "Deps.Pub") || !strings.Contains(err.Error(), "Deps.Meter") {
		t.Fatalf("got %v", err)
	}
}
