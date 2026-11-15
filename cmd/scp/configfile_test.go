package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/config"
)

// TestConfigurationIsTheBaseline (design/config-baseline.md): scp.yaml's `visibility:` and `layouts:` decide with nothing stored; an
// admin's change is kept in the store and warned about; the download writes it into the file with the operator's comments and other
// keys kept; that file, loaded as the configuration, holds what was in force.
func TestConfigurationIsTheBaseline(t *testing.T) {
	get, _ := privatePortalAt(t, visibilityAdmin+`# Who sees what, kept in git.
visibility:
  repos:
    acme/open: {audience: signed-in}
layouts:
  present: {elements: [{kind: telemetry}]}
`)
	shown := func(path string, headers ...string) bool {
		code, _, _ := get(path, headers...)
		return code == http.StatusOK
	}
	untilListed(t, get)
	if shown("/api/v1/repos/acme/open") || !shown("/api/v1/repos/acme/open", staff...) {
		t.Fatal("the file's choice does not apply")
	}
	if _, b, _ := get("/api/v1/pages/present"); !strings.Contains(b, `"telemetry"`) || strings.Contains(b, `"fleetMap"`) {
		t.Errorf("the file's layout does not apply: %.300s", b)
	}
	if _, b, _ := get("/api/v1/admin/status", adminToken...); strings.Contains(b, "kept only in the store") {
		t.Errorf("nothing changed, yet the status warns: %.400s", b)
	}

	// An admin shows acme/open to everyone but keeps it from search engines, and adds a rule.
	saveVisibility(t, get, `{"repos":{"acme/open":{"audience":"everyone","search":false}},"rules":[{"pattern":"^acme/zq-","audience":"no-one","search":true}]}`)
	until(t, "the change applies", func() bool { return shown("/api/v1/repos/acme/open") })
	if _, b, _ := get("/api/v1/admin/status", adminToken...); !strings.Contains(b, "visibility: 2 changes kept only in the store") {
		t.Errorf("the status does not warn of the changes: %.600s", b)
	}

	if code, _, _ := get(configPath, staff...); code != http.StatusForbidden {
		t.Errorf("a viewer who is no admin downloads the configuration: %d", code)
	}
	code, file, h := get(configPath, adminToken...)
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "application/yaml") ||
		!strings.Contains(h.Get("Content-Disposition"), `filename="scp.yaml"`) {
		t.Fatalf("the download: %d %v", code, h)
	}
	for _, want := range []string{"# Who sees what, kept in git.", "acme/open:", "search: false", "pattern: ^acme/zq-", "audience: no-one",
		"present:", "kind: telemetry", "id: move", "include_private: true"} {
		if !strings.Contains(file, want) {
			t.Errorf("the download lacks %q:\n%s", want, file)
		}
	}

	// The downloaded file is a configuration: loaded, its base is what was in force.
	path := filepath.Join(t.TempDir(), "scp.yaml")
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatalf("the download does not load: %v\n%s", err, file)
	}
	vis, layouts, problems := baseline(cfg.Baseline)
	if len(problems) > 0 || len(layouts) != 1 || len(cfg.Announcements) != 1 {
		t.Fatalf("the download's base: %v %v %+v", problems, layouts, cfg.Announcements)
	}
	if got, want := string(visibilityValue(vis)), `{"repos":{"acme/open":{"audience":"everyone","search":false}},"rules":[{"pattern":"^acme/zq-","audience":"no-one"}]}`; got != want {
		t.Errorf("the download's visibility\n%s\nwant\n%s", got, want)
	}
}

// configYAML replaces, adds and removes top-level sections and keeps everything else as written.
func TestConfigYAML(t *testing.T) {
	file := "# The portal.\nstore: {path: /data/scp.db} # the store\nvisibility:\n  repos: {a/b: {audience: no-one}}\nlayouts: {}\n"
	out, err := configYAML([]byte(file), []section{
		{"visibility", []byte(`{"repos":{"a/c":{"audience":"signed-in"}}}`)},
		{"layouts", nil},
		{"announcements", []byte(`[{"id":"x","title":"Hi","until":"2999-01-01T00:00:00Z"}]`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `# The portal.
store: {path: /data/scp.db} # the store
visibility:
  repos:
    a/c:
      audience: signed-in
announcements:
  - id: x
    title: Hi
    until: "2999-01-01T00:00:00Z"
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if out, err := configYAML(nil, []section{{"layouts", []byte(`{"dashboard":{"elements":[]}}`)}}); err != nil || string(out) != "layouts:\n  dashboard:\n    elements: []\n" {
		t.Errorf("without a file: %q %v", out, err)
	}
}
