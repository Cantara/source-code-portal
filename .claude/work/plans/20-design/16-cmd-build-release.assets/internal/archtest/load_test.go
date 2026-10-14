package archtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cantara/source-code-portal/internal/archtest"
)

// A checkout without web/dist (the `go` job of CI runs before the SPA is built) must still be analysable: the spa build of package
// web embeds dist, `go list -e` reports the missing pattern as the package's error, and the import matrix needs only its imports.
// Any OTHER package error stays fatal.
func TestLoadToleratesAMissingEmbedDirectory(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/m\n\ngo 1.21\n")
	write("web/web.go", "package web\n")
	write("web/spa.go", "//go:build spa\n\npackage web\n\nimport (\n\t\"embed\"\n\t_ \"example.test/m/lib\"\n)\n\n//go:embed all:dist\nvar embedded embed.FS\n")
	write("lib/lib.go", "package lib\n")

	module, pkgs, err := archtest.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if module != "example.test/m" {
		t.Errorf("module = %q", module)
	}
	var web *archtest.Pkg
	for i := range pkgs {
		if pkgs[i].Path == "web" {
			web = &pkgs[i]
		}
	}
	if web == nil || len(web.Imports) == 0 {
		t.Fatalf("web must be listed with the imports of its spa files: %+v", pkgs)
	}

	write("lib/broken.go", "package lib\n\nimport \"embed\"\n\n//go:embed ../x\nvar bad embed.FS\n")
	if _, _, err := archtest.Load(dir); err == nil {
		t.Error("an invalid embed pattern is a package error other than a missing directory and must stay fatal")
	}
}
