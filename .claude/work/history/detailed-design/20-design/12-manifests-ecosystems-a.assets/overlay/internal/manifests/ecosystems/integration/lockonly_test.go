package integration_test

import (
	"context"
	"testing"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

// TestLockOnlyDirectoriesStillShow: an assembler returns nil exactly when it placed no unit, so the root's default assembly shows a directory that has a
// lockfile (or a go.work) but no manifest. A non-nil empty result would hide the files (design section 4.1.2).
func TestLockOnlyDirectoriesStillShow(t *testing.T) {
	cases := map[string]map[string][]byte{
		"npm":    {"package-lock.json": []byte(`{"lockfileVersion":3,"packages":{"":{},"node_modules/a":{"version":"1.0.0"}}}`)},
		"cargo":  {"Cargo.lock": []byte("version = 4\n[[package]]\nname = \"a\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n")},
		"python": {"uv.lock": []byte("version = 1\n[[package]]\nname = \"a\"\nversion = \"1.0.0\"\n")},
		"gradle": {"gradle.lockfile": []byte("org.acme:a:1.0.0=runtimeClasspath\n")},
		"golang": {"go.work": []byte("go 1.22\nuse ./a\n")},
	}
	reg := register(t)
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			res := manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), files)
			if len(res.Units) == 0 {
				t.Fatalf("no unit for a lock-only tree; diagnostics %v", res.Diagnostics)
			}
			parsertest.CheckTree(t, reg, res)
		})
	}
}
