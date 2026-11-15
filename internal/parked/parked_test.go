package parked

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// The stored list is one JSON object of lower-case addresses, kept sorted and without repeats; anything else is refused.
func TestParse(t *testing.T) {
	l, err := Parse([]byte(`{"repos":["acme/b","acme/a","acme/b"]}`))
	if err != nil || strings.Join(l.Repos, ",") != "acme/a,acme/b" {
		t.Errorf("parse: %v %v", l, err)
	}
	for _, bad := range []string{`{"repos":["Acme/A"]}`, `{"repos":["acme"]}`, `{"repos":[],"groups":[]}`, `[]`,
		`{"repos":["` + strings.Repeat("a", maxBytes) + `/b"]}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%.40s: accepted", bad)
		}
	}
}

// stored is a backend holding one document.
type stored []byte

func (b stored) Document(context.Context, string) ([]byte, string, bool, error) {
	return b, "r1", true, nil
}
func (stored) PutDocument(context.Context, string, []byte, string, string) error { return nil }
func (stored) DeleteDocument(context.Context, string, string) error              { return nil }

// Has answers from the stored, sorted list; with no store nothing is parked, and a stored list that does not parse parks nothing and
// says why.
func TestHas(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if s := Open(t.Context(), nil, nil, log); s.Has("acme/a") || s.Problems() != nil {
		t.Error("no store parks something")
	}
	s := Open(t.Context(), stored(`{"repos":["acme/c","acme/a"]}`), nil, log)
	if !s.Has("acme/c") || s.Has("acme/b") || !s.Repo(&snapshot.RepoEntry{Repo: snapshot.Repo{Owner: "Acme", Name: "A"}}) {
		t.Error("has")
	}
	if s := Open(t.Context(), stored(`{"repos":["Acme/A"]}`), nil, log); s.Has("acme/a") || len(s.Problems()) != 1 {
		t.Errorf("a bad list: problems %v", s.Problems())
	}
}
