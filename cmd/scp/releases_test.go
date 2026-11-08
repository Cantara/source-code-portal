package main

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// TestMemoNotes: the notes of a release are rendered once per repository and text, not at every request.
func TestMemoNotes(t *testing.T) {
	calls := 0
	render := memoNotes(func(*snapshot.RepoEntry, string) safehtml.SafeHTML { calls++; return safehtml.Text("x") })
	a := &snapshot.RepoEntry{Repo: snapshot.Repo{Owner: "acme", Name: "a"}}
	b := &snapshot.RepoEntry{Repo: snapshot.Repo{Owner: "acme", Name: "b"}}
	render(a, "one")
	render(a, "one")
	if calls != 1 {
		t.Errorf("the same notes were rendered %d times", calls)
	}
	render(b, "one")
	render(a, "two")
	if calls != 3 {
		t.Errorf("another repository or text is rendered again: %d renders", calls)
	}
}
