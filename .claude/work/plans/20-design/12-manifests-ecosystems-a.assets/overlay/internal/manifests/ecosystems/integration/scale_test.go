package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

// TestInputsAtScaleStayLinear (verification VF-12-01..03, acceptance criterion 8): every format at a large size must parse and assemble in bounded time. Two
// quadratic defects of the first revision (tomlx line index: 1 MiB Cargo.lock 6.7 s, 8 MiB 443 s; pyproject locator: 1 MiB single-line array 11.4 s)
// passed every golden, fuzz and bomb test because those inputs are small or adversarial in shape, not large. The bound is 3 s per case, an order of
// magnitude above the measured 30 to 600 ms for the full 8 MiB and above what -race costs; a quadratic algorithm misses it by one to two orders.
// (Sizes here are a quarter of the limits so that the test stays fast under -race; the nightly oracle job runs the 8 MiB originals, appendix B E-A8.)

func fill(limit int, f func(i int) string) string {
	var b strings.Builder
	for i := 0; b.Len() < limit; i++ {
		b.WriteString(f(i))
	}
	return b.String()
}

func TestInputsAtScaleStayLinear(t *testing.T) {
	const M = 1 << 20
	lim, small := 2*M-2000, M/2-2000
	cases := []struct {
		name, path string
		data       string
	}{
		{"package-lock.json 2MiB", "package-lock.json", `{"lockfileVersion":3,"packages":{"":{"name":"r","dependencies":{"p0":"1"}}` + fill(lim, func(i int) string {
			return fmt.Sprintf(`,"node_modules/p%d":{"version":"1.0.%d","resolved":"https://r/p%d.tgz","integrity":"sha512-%080d","dependencies":{"p%d":"^1"}}`, i, i, i, i, i+1)
		}) + `}}`},
		{"pnpm-lock 2MiB", "pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .:\n    dependencies:\n      p0:\n        specifier: ^1\n        version: 1.0.0\npackages:\n" + fill(lim/2, func(i int) string {
			return fmt.Sprintf("  p%d@1.0.%d:\n    resolution: {integrity: sha512-%080d}\n", i, i, i)
		}) + "snapshots:\n" + fill(lim/2, func(i int) string {
			return fmt.Sprintf("  p%d@1.0.%d:\n    dependencies:\n      p%d: 1.0.%d\n", i, i, i+1, i+1)
		})},
		{"yarn1 2MiB", "yarn.lock", "# yarn lockfile v1\n\n" + fill(lim, func(i int) string {
			return fmt.Sprintf("p%d@^1.0.0:\n  version \"1.0.%d\"\n  resolved \"https://r/p%d.tgz\"\n  integrity sha512-%080d\n  dependencies:\n    p%d \"^1.0.0\"\n\n", i, i, i, i, i+1)
		})},
		{"yarn berry 2MiB", "yarn.lock", "__metadata:\n  version: 8\n\n" + fill(lim, func(i int) string {
			return fmt.Sprintf("\"p%d@npm:^1.0.0\":\n  version: 1.0.%d\n  resolution: \"p%d@npm:1.0.%d\"\n  dependencies:\n    p%d: ^1.0.0\n  checksum: %080d\n  languageName: node\n  linkType: hard\n\n", i, i, i, i, i+1, i)
		})},
		{"bun.lock 2MiB", "bun.lock", `{"lockfileVersion":1,"workspaces":{"":{"name":"r"}},"packages":{"p0":["p0@1.0.0","",{},"sha512-x"]` + fill(lim, func(i int) string {
			return fmt.Sprintf(`,"p%d":["p%d@1.0.%d","",{"dependencies":{"p%d":"^1"}},"sha512-%080d"]`, i, i, i, i+1, i)
		}) + `}}`},
		{"uv.lock 2MiB", "uv.lock", "version = 1\n" + fill(lim, func(i int) string {
			return fmt.Sprintf("[[package]]\nname = \"p%d\"\nversion = \"1.0.%d\"\nsource = { registry = \"https://pypi.org/simple\" }\ndependencies = [{ name = \"p%d\" }]\nwheels = [{ url = \"https://f/p%d.whl\", hash = \"sha256:%064d\", size = 1 }]\n\n", i, i, i+1, i, i)
		})},
		{"poetry.lock 2MiB", "poetry.lock", fill(lim, func(i int) string {
			return fmt.Sprintf("[[package]]\nname = \"p%d\"\nversion = \"1.0.%d\"\ndescription = \"x\"\noptional = false\npython-versions = \">=3.8\"\n\n[package.dependencies]\np%d = \">=1\"\n\n", i, i, i+1)
		}) + "[metadata]\nlock-version = \"2.0\"\n"},
		{"Pipfile.lock 2MiB", "Pipfile.lock", `{"_meta":{},"default":{"p0":{"version":"==1.0.0"}` + fill(lim, func(i int) string {
			return fmt.Sprintf(`,"p%d":{"version":"==1.0.%d","hashes":["sha256:%064d"]}`, i, i, i)
		}) + `},"develop":{}}`},
		{"gradle.lockfile 2MiB", "gradle.lockfile", fill(lim, func(i int) string { return fmt.Sprintf("g%d:a%d:1.0.%d=runtimeClasspath,compileClasspath\n", i, i, i) }) + "empty=\n"},
		{"requirements 512KiB", "requirements.txt", fill(small, func(i int) string {
			return fmt.Sprintf("pkg%d[x]==1.0.%d ; python_version>'3' --hash=sha256:%064d\n", i, i, i)
		})},
		{"go.mod 512KiB", "go.mod", "module m\ngo 1.22\nrequire (\n" + fill(small, func(i int) string { return fmt.Sprintf("\texample.com/p%d v1.0.%d\n", i, i) }) + ")\n"},
		{"package.json 512KiB", "package.json", `{"name":"x","dependencies":{"a":"1"` + fill(small, func(i int) string { return fmt.Sprintf(`,"p%d":"^1.0.%d"`, i, i) }) + `}}`},
		{"pom.xml 512KiB", "pom.xml", `<project><groupId>g</groupId><artifactId>a</artifactId><version>1</version><dependencies>` + fill(small, func(i int) string {
			return fmt.Sprintf("<dependency><groupId>g%d</groupId><artifactId>a%d</artifactId><version>1.%d</version></dependency>", i, i, i)
		}) + `</dependencies></project>`},
		{"build.gradle 512KiB", "build.gradle", "dependencies {\n" + fill(small, func(i int) string { return fmt.Sprintf("  implementation 'g%d:a%d:1.%d'\n", i, i, i) }) + "}\n"},
		{"Cargo.toml 512KiB arr", "Cargo.toml", "[package]\nname=\"x\"\nversion=\"1\"\n" + fill(small, func(i int) string { return fmt.Sprintf("[[bin]]\nname=\"b%d\"\n", i) })},
		{"Cargo.lock", "Cargo.lock", "version = 3\n" + fill(lim, func(i int) string {
			return fmt.Sprintf("[[package]]\nname = \"c%d\"\nversion = \"1.0.%d\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"%064d\"\ndependencies = [\"c%d\"]\n\n", i, i, i, i+1)
		})},
		{"Cargo.toml one table", "Cargo.toml", "[package]\nname=\"x\"\nversion=\"1\"\n[dependencies]\n" + fill(70000, func(i int) string { return fmt.Sprintf("d%d=\"1\"\n", i) })},
		{"pyproject single-line array", "pyproject.toml", "[project]\nname=\"x\"\ndependencies=[" + fill(small, func(i int) string { return fmt.Sprintf("\"p%d>=1\",", i) }) + "]\n"},
		{"settings.gradle 512KiB", "settings.gradle", fill(small, func(i int) string { return fmt.Sprintf("include ':m%d'\n", i) })},
	}
	r := register(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := parsertest.TreeConfig()
			cfg.Limits.LockfileBytes = 8 * M
			st := time.Now()
			res := manifests.RunTree(context.Background(), r, cfg, map[string][]byte{c.path: []byte(c.data)})
			if d := time.Since(st); d > 3*time.Second {
				t.Errorf("%s: %d KiB took %v", c.path, len(c.data)/1024, d)
			}
			if len(res.Units) != 1 && c.path != "settings.gradle" {
				t.Errorf("%s: %d units, diagnostics %v", c.path, len(res.Units), res.Diagnostics)
			}
		})
	}
}
