package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestParkedRepositoriesAreShownNowhere: a repository an admin parks leaves every answer a private viewer and the public read (its
// page and Markdown, the lists, the groups, search, llms and the sitemap), the admin's card still lists it, and the status counts it;
// taken off the list it is back with what was read while it was parked.
func TestParkedRepositoriesAreShownNowhere(t *testing.T) {
	get, _ := privatePortalAt(t, "access: {private_groups: [staff]}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff], admin: true}]}\n")
	admin := []string{"Authorization", "Bearer " + botSecret}
	type card struct {
		Repos    []string
		Revision string
		Tree     []struct {
			ID    string
			Repos []struct {
				Address string
				Private bool
				Parked  bool
			}
		}
	}
	read := func() card {
		t.Helper()
		var c card
		code, b, _ := get("/api/v1/admin/parked", admin...)
		if code != http.StatusOK || json.Unmarshal([]byte(b), &c) != nil {
			t.Fatalf("the card: %d %s", code, b)
		}
		return c
	}
	save := func(repos string) {
		t.Helper()
		body := `{"repos":` + repos + `,"revision":"` + read().Revision + `"}`
		if code, b, _ := get("/api/v1/admin/parked", append([]string{":put", body}, admin...)...); code != http.StatusOK {
			t.Fatalf("save %s: %d %s", body, code, b)
		}
	}
	// until waits for the publish a save asks for.
	until := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("not within 10s: %s", what)
			}
		}
	}
	shown := func(path string, headers ...string) bool {
		code, _, _ := get(path, headers...)
		return code == http.StatusOK
	}

	untilListed(t, get)
	if !shown("/api/v1/repos/acme/zq-hidden", staff...) || !shown("/api/v1/repos/acme/open") {
		t.Fatal("nothing parked: both repositories are shown")
	}
	if code, _, _ := get("/api/v1/admin/parked", staff...); code != http.StatusForbidden {
		t.Errorf("a private viewer who is no admin reads the list: %d", code)
	}
	if code, _, _ := get("/api/v1/admin/parked"); code != http.StatusUnauthorized {
		t.Errorf("an anonymous read of the list: %d", code)
	}

	save(`["acme/zq-hidden","acme/open"]`)
	until("the parked repositories leave the views", func() bool { return !shown("/api/v1/repos/acme/open") })
	for _, who := range [][]string{nil, staff, admin} {
		for _, p := range []string{"/api/v1/repos/acme/zq-hidden", "/api/v1/repos/acme/open", "/repos/acme/open/readme.md"} {
			if code, _, _ := get(p, who...); code != http.StatusNotFound {
				t.Errorf("%v %s: %d, want 404", who, p, code)
			}
		}
		for _, p := range []string{"/api/v1/dashboard", "/api/v1/groups", "/api/v1/commits", "/api/v1/pulls", "/api/v1/issues",
			"/api/v1/search?q=open", "/api/v1/search?q=zq", "/llms.txt", "/sitemap.xml"} {
			if _, b, _ := get(p, who...); strings.Contains(b, "acme/open") || strings.Contains(b, "zq-hidden") || strings.Contains(b, "zq hidden") {
				t.Errorf("%v %s names a parked repository:\n%.400s", who, p, b)
			}
		}
	}
	c := read()
	parked := 0
	for _, g := range c.Tree {
		for _, r := range g.Repos {
			if r.Parked {
				parked++
			}
		}
	}
	if len(c.Repos) != 2 || parked != 2 {
		t.Errorf("the card: list %v, %d ticked in the tree", c.Repos, parked)
	}

	save(`[]`)
	until("a repository taken off the list is back", func() bool { return shown("/api/v1/repos/acme/open") })
	if _, b, _ := get("/api/v1/commits", staff...); !shown("/api/v1/repos/acme/zq-hidden", staff...) || !strings.Contains(b, "zq hidden change") {
		t.Errorf("the private repository is not back with its commits:\n%.400s", b)
	}
}
