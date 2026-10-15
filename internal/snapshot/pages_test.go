package snapshot

import (
	"encoding/json"
	"os"
	"testing"
)

// TestPages holds the page addresses to the cases web/src/lib/paths.test.ts holds paths.ts to, so the two builders never drift.
func TestPages(t *testing.T) {
	b, err := os.ReadFile("../../web/src/lib/paths.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Repos  []struct{ Owner, Name, Path string }
		Groups []struct{ ID, Path string }
		Docs   []struct{ Owner, Name, Doc, Path string }
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases.Repos {
		if got := RepoPage(KeyOf(c.Owner, c.Name)); got != c.Path {
			t.Errorf("RepoPage(%s/%s) = %s, want %s", c.Owner, c.Name, got, c.Path)
		}
	}
	for _, c := range cases.Groups {
		if got := GroupPage(c.ID); got != c.Path {
			t.Errorf("GroupPage(%s) = %s, want %s", c.ID, got, c.Path)
		}
	}
	for _, c := range cases.Docs {
		if got := DocPage(KeyOf(c.Owner, c.Name), c.Doc); got != c.Path {
			t.Errorf("DocPage(%s) = %s, want %s", c.Doc, got, c.Path)
		}
	}
}
