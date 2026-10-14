package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestKeyOfMatchesRepoRefKey(t *testing.T) {
	r := scm.RepoRef{Source: "acme-app", ID: 7, Owner: "acme", Name: "web"}
	if got := store.KeyOf(r).String(); got != r.Key() {
		t.Fatalf("%q != %q: the job-key tail and the store key must be one string", got, r.Key())
	}
}

func TestKeyOfFoldsLikeRepoRefKey(t *testing.T) { // V-01: GitHub reports display case; job keys carry the folded tail
	r := scm.RepoRef{Source: "acme-app", ID: 7, Owner: "Acme", Name: "Widget-Core"}
	if got := store.KeyOf(r).String(); got != r.Key() || got != "acme-app/acme/widget-core" {
		t.Fatalf("%q vs %q", got, r.Key())
	}
	if err := (store.RepoKey{Source: "s", Owner: "Acme", Name: "n"}).Validate(); err == nil {
		t.Fatal("a non-folded key must be rejected")
	}
}

func TestValidKind(t *testing.T) {
	ok := []string{"commits.refresh", "docs.refresh", "manifests.facts", "integrations.jenkins", "a.b", "x1.y_2-z"}
	bad := []string{"", "a", "ab", "docs", "Docs.refresh", "docs..refresh", ".docs", "docs.", "docs/refresh", "docs.re fresh", strings.Repeat("a", 61) + ".bcd"}
	for _, k := range ok {
		if err := store.ValidKind(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	for _, k := range bad {
		if err := store.ValidKind(k); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%q accepted", k)
		}
	}
}

func TestRepoKeyValidate(t *testing.T) {
	for _, k := range []store.RepoKey{{}, {"s", "o", ""}, {"s", "a/b", "n"}, {"s", "o", "n\x00"}, {"s", "o", strings.Repeat("x", 201)}} {
		if k.Validate() == nil {
			t.Errorf("%+v accepted", k)
		}
	}
	if (store.RepoKey{"s", "o", "n"}).Validate() != nil {
		t.Fatal("valid key rejected")
	}
}

func FuzzValidKind(f *testing.F) {
	f.Add("docs.refresh")
	f.Fuzz(func(t *testing.T, s string) {
		if store.ValidKind(s) == nil && (strings.ContainsAny(s, "/ \x00A") || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".")) {
			t.Fatalf("accepted %q", s)
		}
	})
}
