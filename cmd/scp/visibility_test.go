package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// visibilityAdmin is a configuration whose token bot is an administrator.
const visibilityAdmin = "access: {private_groups: [staff]}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff], admin: true}]}\n"

var adminToken = []string{"Authorization", "Bearer " + botSecret} //nolint:gochecknoglobals // test value

// visibilityCard is what the admins' list answers.
type visibilityCard struct {
	Revision string
	Counts   struct{ Everyone, SignedIn, NoOne, SearchOff, Private int }
	Tree     []struct {
		ID    string
		Repos []struct {
			Address  string
			Private  bool
			Audience string
			Why      string
		}
	}
}

func readVisibility(t *testing.T, get fetch) visibilityCard {
	t.Helper()
	var c visibilityCard
	code, b, _ := get("/api/v1/admin/visibility", adminToken...)
	if code != http.StatusOK || json.Unmarshal([]byte(b), &c) != nil {
		t.Fatalf("the list: %d %s", code, b)
	}
	return c
}

// saveVisibility saves doc (a JSON object without its revision) as the admin.
func saveVisibility(t *testing.T, get fetch, doc string) {
	t.Helper()
	body := strings.TrimSuffix(strings.TrimSpace(doc), "}")
	if body != "{" {
		body += ","
	}
	body += `"revision":"` + readVisibility(t, get).Revision + `"}`
	if code, b, _ := get("/api/v1/admin/visibility", append([]string{":put", body, "Origin", "https://scp.example"}, adminToken...)...); code != http.StatusOK {
		t.Fatalf("save %s: %d %s", body, code, b)
	}
}

// until waits for what a save's publish brings.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not within 10s: %s", what)
		}
	}
}

// TestNoOneSeesWhatIsSetToNoOne: a repository set to no one leaves every answer a private viewer and the public read (its page and
// Markdown, the lists, the groups, search, llms and the sitemap), the admins' list still holds it and the status counts it; set back,
// it is there again with what was read meanwhile. Only an admin reads or changes the list.
func TestNoOneSeesWhatIsSetToNoOne(t *testing.T) {
	get, _ := privatePortalAt(t, visibilityAdmin)
	shown := func(path string, headers ...string) bool {
		code, _, _ := get(path, headers...)
		return code == http.StatusOK
	}
	untilListed(t, get)
	if !shown("/api/v1/repos/acme/zq-hidden", staff...) || !shown("/api/v1/repos/acme/open") {
		t.Fatal("nothing chosen: both repositories are shown")
	}
	if code, _, _ := get("/api/v1/admin/visibility", staff...); code != http.StatusForbidden {
		t.Errorf("a private viewer who is no admin reads the list: %d", code)
	}
	if code, _, _ := get("/api/v1/admin/visibility"); code != http.StatusUnauthorized {
		t.Errorf("an anonymous read of the list: %d", code)
	}
	if code, _, _ := get("/api/v1/admin/visibility/preview", append([]string{":post", `{}`, "Origin", "https://scp.example"}, staff...)...); code != http.StatusForbidden {
		t.Errorf("a private viewer who is no admin previews: %d", code)
	}
	if c := readVisibility(t, get); c.Counts.Everyone != 1 || c.Counts.SignedIn != 1 || c.Counts.Private != 1 || len(c.Tree) != 1 || len(c.Tree[0].Repos) != 2 {
		t.Errorf("the list lists the private repository too: %+v", c)
	}

	saveVisibility(t, get, `{"repos":{"acme/zq-hidden":{"audience":"no-one","search":true},"acme/open":{"audience":"no-one","search":true}}}`)
	until(t, "the repositories leave the views", func() bool { return !shown("/api/v1/repos/acme/open") })
	for _, who := range [][]string{nil, staff, adminToken} {
		for _, p := range []string{"/api/v1/repos/acme/zq-hidden", "/api/v1/repos/acme/open", "/repos/acme/open/readme.md"} {
			if code, _, _ := get(p, who...); code != http.StatusNotFound {
				t.Errorf("%v %s: %d, want 404", who, p, code)
			}
		}
		for _, p := range []string{"/api/v1/dashboard", "/api/v1/groups", "/api/v1/commits", "/api/v1/pulls", "/api/v1/issues",
			"/api/v1/search?q=open", "/api/v1/search?q=zq", "/llms.txt", "/sitemap.xml"} {
			if _, b, _ := get(p, who...); strings.Contains(b, "acme/open") || strings.Contains(b, "zq-hidden") || strings.Contains(b, "zq hidden") {
				t.Errorf("%v %s names a repository no one sees:\n%.400s", who, p, b)
			}
		}
	}
	if c := readVisibility(t, get); c.Counts.NoOne != 2 || len(c.Tree) != 1 || len(c.Tree[0].Repos) != 2 || c.Tree[0].Repos[0].Audience != "no-one" {
		t.Errorf("the list after the save: %+v", c)
	}
	if _, b, _ := get("/api/v1/admin/status", adminToken...); !strings.Contains(b, `"parked": 2`) {
		t.Errorf("the status does not count them: %.300s", b)
	}

	saveVisibility(t, get, `{}`)
	until(t, "the repositories are back", func() bool { return shown("/api/v1/repos/acme/open") })
	if _, b, _ := get("/api/v1/commits", staff...); !shown("/api/v1/repos/acme/zq-hidden", staff...) || !strings.Contains(b, "zq hidden change") {
		t.Errorf("the private repository is not back with its commits:\n%.400s", b)
	}
}

// TestSearchEnginesAreOnlyOfferedWhatEveryoneSeesWithSearchOn: an item everyone sees with its search switch off leaves the sitemap at
// once and its pages answer noindex while the portal still shows it; a product's switch carries its repositories along; a rule reaches
// the repositories whose address it matches.
func TestSearchEnginesAreOnlyOfferedWhatEveryoneSeesWithSearchOn(t *testing.T) {
	get, _ := privatePortalAt(t, visibilityAdmin)
	sitemap := func() string { _, b, _ := get("/sitemap.xml"); return b }
	robots := func(path string) string { _, _, h := get(path); return h.Get("X-Robots-Tag") }
	if b := sitemap(); !strings.Contains(b, "/repos/acme/open") || !strings.Contains(b, "/groups/all") || robots("/repos/acme/open/readme.md") != "" {
		t.Fatalf("nothing kept out:\n%s", b)
	}

	saveVisibility(t, get, `{"repos":{"acme/open":{"audience":"everyone","search":false}}}`)
	if b := sitemap(); strings.Contains(b, "/repos/acme/open") || !strings.Contains(b, "/groups/all") || !strings.Contains(b, "<loc>https://scp.example/</loc>") {
		t.Errorf("a repository kept out is in the sitemap, or the rest is not:\n%s", b)
	}
	for _, p := range []string{"/repos/acme/open", "/repos/acme/open/readme.md", "/repos/acme/open/docs/guide"} {
		if got := robots(p); got != "noindex, nofollow" {
			t.Errorf("%s: X-Robots-Tag = %q", p, got)
		}
	}
	if got := robots("/groups/all"); got != "" {
		t.Errorf("a product that is offered: %q", got)
	}
	if code, b, _ := get("/api/v1/repos/acme/open"); code != http.StatusOK || !strings.Contains(b, `"address":"acme/open"`) {
		t.Errorf("the portal still shows it: %d %s", code, b)
	}

	saveVisibility(t, get, `{"groups":{"all":{"audience":"everyone","search":false}}}`)
	if b := sitemap(); strings.Contains(b, "/groups/all") || strings.Contains(b, "/repos/acme/open") {
		t.Errorf("a product kept out, and its repository, are in the sitemap:\n%s", b)
	}
	if robots("/groups/all") != "noindex, nofollow" || robots("/repos/acme/open") != "noindex, nofollow" {
		t.Error("a product kept out, or its repository, is offered")
	}

	saveVisibility(t, get, `{"groups":{"all":{"audience":"everyone","search":false}},"rules":[{"pattern":"^acme/open$","audience":"everyone","search":true}]}`)
	if b := sitemap(); !strings.Contains(b, "/repos/acme/open") {
		t.Errorf("a rule did not offer the repository it matches:\n%s", b)
	}
	saveVisibility(t, get, `{"rules":[{"pattern":"^acme/o","audience":"signed-in","search":true}]}`)
	until(t, "a rule takes the repository out of the public view", func() bool { code, _, _ := get("/api/v1/repos/acme/open"); return code == http.StatusNotFound })
	if b := sitemap(); strings.Contains(b, "/repos/acme/open") {
		t.Errorf("a repository only signed-in viewers see is in the sitemap:\n%s", b)
	}
}
