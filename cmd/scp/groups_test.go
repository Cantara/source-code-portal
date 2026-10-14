package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigGroupsPreview(t *testing.T) {
	path := configFile(t, `catalog:
  groups:
    - {id: whydah, name: Whydah, default_repo: Whydah-SSO, match: [{regexp: 'Whydah-.*'}]}
    - {id: libs, name: Libraries, default_repo: commons, match: [{topic: lib}, {exact: commons}]}
`)
	dir := filepath.Dir(path)
	listing := filepath.Join(dir, "repos.json")
	// Two pages, as gh api --paginate prints them.
	body := `[{"id":1,"name":"Whydah-SSO","owner":{"login":"acme"},"visibility":"public"},
{"id":2,"name":"Whydah-Fork","owner":{"login":"acme"},"fork":true,"visibility":"public"}]
[{"id":3,"name":"tools","owner":{"login":"acme"},"topics":["lib"],"visibility":"public"},
{"id":4,"name":"stray","owner":{"login":"acme"},"visibility":"public"}]`
	if err := os.WriteFile(listing, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute(t, "config", "groups", "-c", path, "--repos", listing)
	for _, want := range []string{"whydah: Whydah (1 repositories; default acme/whydah-sso)\n  Whydah-SSO\n",
		"libs: Libraries (1 repositories; default MISSING: no member is named commons)\n  tools\n",
		"In no group (1):\n  stray\n", "Left out by the source filters (1):\n  Whydah-Fork (fork; include_forks)\n"} {
		if code != exitOK || !strings.Contains(stdout, want) {
			t.Errorf("exit %d, stdout misses %q:\n%s\nstderr %s", code, want, stdout, stderr)
		}
	}

	names := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(names, []byte("Whydah-SSO\n# a comment\ncommons\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := execute(t, "config", "groups", "-c", path, "--repos", names); code != exitOK ||
		!strings.Contains(stdout, "libs: Libraries (1 repositories; default acme/commons)") {
		t.Errorf("names: exit %d:\n%s", code, stdout)
	}
}
