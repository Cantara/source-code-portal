package manifests

import (
	"strings"
	"testing"
)

func depsOf(m Manifest) string {
	var out []string
	for _, d := range m.Dependencies {
		out = append(out, d.Scope+":"+d.Name+"@"+d.Version)
	}
	return strings.Join(out, " ")
}

func TestParseCargo(t *testing.T) {
	m := parseCargo([]byte(`[package]
name = "acme-core"
version = "0.4.2"
rust-version = "1.85"

[dependencies]
serde = { version = "1", features = ["derive"] }
tokio = "1.40"
tracing = { version = "0.1", optional = true }

[dev-dependencies]
proptest = "1"

[build-dependencies]
cc = "1"

[target.'cfg(windows)'.dependencies]
winapi = "0.3"
`))
	if m.Name != "acme-core" || m.Version != "0.4.2" || m.Language != "Rust" || m.BuildTool != "Cargo" ||
		len(m.Toolchain) != 1 || m.Toolchain[0] != (Toolchain{Name: "rust", Version: "1.85"}) {
		t.Errorf("manifest = %+v", m)
	}
	if got, want := depsOf(m), "runtime:serde@1 runtime:tokio@1.40 optional:tracing@0.1 dev:proptest@1 tool:cc@1 runtime:winapi@0.3"; got != want {
		t.Errorf("dependencies = %s\nwant %s", got, want)
	}
	if m := parseCargo([]byte("[package\nname=")); len(m.Problems) != 1 || m.Problems[0].Code != "unparseable" {
		t.Errorf("broken = %+v", m.Problems)
	}
	if m := parseCargo([]byte("[package]\nname = \"w\"\nversion.workspace = true\n")); m.Version != "" {
		t.Errorf("an inherited version = %q", m.Version)
	}
}

func TestParsePyproject(t *testing.T) {
	m := parsePyproject([]byte(`[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[project]
name = "acme-tools"
version = "1.2"
requires-python = ">=3.12"
dependencies = ["httpx[http2]>=0.27", "rich", "tomli; python_version < '3.11'", "pkg @ https://example.org/pkg.whl"]

[project.optional-dependencies]
cli = ["typer>=0.12"]

[dependency-groups]
test = ["pytest>=8", {include-group = "lint"}]
`), func(string) bool { return false })
	if m.Name != "acme-tools" || m.Version != "1.2" || m.Language != "Python" || m.BuildTool != "Hatch" ||
		len(m.Toolchain) != 1 || m.Toolchain[0] != (Toolchain{Name: "python", Version: ">=3.12"}) {
		t.Errorf("manifest = %+v", m)
	}
	if got, want := depsOf(m), "runtime:httpx@>=0.27 runtime:rich@ runtime:tomli@ runtime:pkg@https://example.org/pkg.whl optional:typer@>=0.12 dev:pytest@>=8"; got != want {
		t.Errorf("dependencies = %s\nwant %s", got, want)
	}
	poetry := parsePyproject([]byte(`[tool.poetry]
name = "site"
version = "0.1.0"

[tool.poetry.dependencies]
python = "^3.11"
django = "^5.1"

[tool.poetry.group.dev.dependencies]
ruff = { version = "^0.6" }
`), func(n string) bool { return n == "uv.lock" })
	if poetry.Name != "site" || poetry.BuildTool != "uv" || poetry.Toolchain[0].Version != "^3.11" ||
		depsOf(poetry) != "runtime:django@^5.1 dev:ruff@^0.6" {
		t.Errorf("poetry = %+v, %s", poetry, depsOf(poetry))
	}
}
