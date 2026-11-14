package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestSearchEnginesAreOnlyOfferedWhatIsNotKeptOut: what an admin keeps out is left out of the sitemap at once and its pages answer
// with noindex, while the portal's own pages still show it; a repository in no other group follows its group.
func TestSearchEnginesAreOnlyOfferedWhatIsNotKeptOut(t *testing.T) {
	get, _ := privatePortalAt(t, "access: {private_groups: [staff]}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff], admin: true}]}\n")
	admin := []string{"Authorization", "Bearer " + botSecret}
	save := func(body string) {
		t.Helper()
		var cur struct{ Revision string }
		_, b, _ := get("/api/v1/admin/indexing", admin...)
		if err := json.Unmarshal([]byte(b), &cur); err != nil {
			t.Fatal(err)
		}
		body = strings.Replace(body, "}", `,"revision":"`+cur.Revision+`"}`, 1)
		if code, b, _ := get("/api/v1/admin/indexing", append([]string{":put", body}, admin...)...); code != http.StatusOK {
			t.Fatalf("save %s: %d %s", body, code, b)
		}
	}
	sitemap := func() string { _, b, _ := get("/sitemap.xml"); return b }
	robots := func(path string) string { _, _, h := get(path); return h.Get("X-Robots-Tag") }

	if b := sitemap(); !strings.Contains(b, "/repos/acme/open") || !strings.Contains(b, "/groups/all") || robots("/repos/acme/open/readme.md") != "" {
		t.Fatalf("nothing kept out:\n%s", b)
	}
	if code, _, _ := get("/api/v1/admin/indexing"); code != http.StatusUnauthorized {
		t.Errorf("an anonymous read of the list: %d", code)
	}

	save(`{"groups":[],"repos":["acme/open"]}`)
	if b := sitemap(); strings.Contains(b, "/repos/acme/open") || !strings.Contains(b, "/groups/all") || !strings.Contains(b, "/commits") {
		t.Errorf("a repository kept out is in the sitemap, or the rest is not:\n%s", b)
	}
	for _, p := range []string{"/repos/acme/open", "/repos/acme/open/readme.md", "/repos/acme/open/docs/guide"} {
		if got := robots(p); got != "noindex, nofollow" {
			t.Errorf("%s: X-Robots-Tag = %q", p, got)
		}
	}
	if got := robots("/groups/all"); got != "" {
		t.Errorf("a group that is not kept out: %q", got)
	}
	if code, b, _ := get("/api/v1/repos/acme/open"); code != http.StatusOK || !strings.Contains(b, `"address":"acme/open"`) {
		t.Errorf("the portal still shows it: %d %s", code, b)
	}

	save(`{"groups":["all"],"repos":[]}`)
	if b := sitemap(); strings.Contains(b, "/groups/all") || strings.Contains(b, "/repos/acme/open") {
		t.Errorf("a group kept out, and the repository only it holds, are in the sitemap:\n%s", b)
	}
	if got := robots("/groups/all"); got != "noindex, nofollow" {
		t.Errorf("a group kept out: %q", got)
	}
	if got := robots("/repos/acme/open"); got != "noindex, nofollow" {
		t.Errorf("a repository whose every group is kept out: %q", got)
	}
}
