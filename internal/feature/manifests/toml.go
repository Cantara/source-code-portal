package manifests

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// parseCargo reads a Cargo.toml: the package's name, version and rust-version, and its dependencies by table ([dependencies],
// [dev-dependencies], [build-dependencies], their target-specific forms, and a workspace's [workspace.dependencies]).
func parseCargo(b []byte) Manifest {
	m := Manifest{Ecosystem: "cargo", Language: "Rust", BuildTool: "Cargo"}
	var ct struct {
		Package struct {
			Name        string `toml:"name"`
			Version     any    `toml:"version"`
			RustVersion any    `toml:"rust-version"`
		} `toml:"package"`
		Dependencies      map[string]any `toml:"dependencies"`
		DevDependencies   map[string]any `toml:"dev-dependencies"`
		BuildDependencies map[string]any `toml:"build-dependencies"`
		Target            map[string]struct {
			Dependencies    map[string]any `toml:"dependencies"`
			DevDependencies map[string]any `toml:"dev-dependencies"`
		} `toml:"target"`
		Workspace struct {
			Dependencies map[string]any `toml:"dependencies"`
		} `toml:"workspace"`
	}
	if err := toml.Unmarshal(b, &ct); err != nil {
		m.Problems = append(m.Problems, problem("", "unparseable", "not valid TOML: %v", err))
		return m
	}
	m.Name = name(ct.Package.Name)
	if v, ok := ct.Package.Version.(string); ok {
		m.Version = version(v)
	}
	if v, ok := ct.Package.RustVersion.(string); ok && v != "" {
		m.Toolchain = append(m.Toolchain, Toolchain{Name: "rust", Version: version(v)})
	}
	var d deps
	add := func(table map[string]any, scope string) {
		for _, n := range slices.Sorted(maps.Keys(table)) {
			req, optional := cargoRequirement(table[n])
			if optional {
				d.add(n, req, "optional")
			} else {
				d.add(n, req, scope)
			}
		}
	}
	add(ct.Dependencies, "runtime")
	add(ct.DevDependencies, "dev")
	add(ct.BuildDependencies, "tool")
	for _, t := range slices.Sorted(maps.Keys(ct.Target)) {
		add(ct.Target[t].Dependencies, "runtime")
		add(ct.Target[t].DevDependencies, "dev")
	}
	add(ct.Workspace.Dependencies, "runtime")
	d.finish(&m)
	return m
}

// cargoRequirement is a dependency's version requirement ("1.2" or { version = "1.2" }) and whether it is optional.
func cargoRequirement(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, false
	case map[string]any:
		req, _ := t["version"].(string)
		opt, _ := t["optional"].(bool)
		return req, opt
	}
	return "", false
}

// pep508 is a requirement's name and the rest: its extras, version and markers.
var pep508 = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)\s*(\[[^\]]*\])?\s*(.*)$`)

// buildBackends name the build tool of a pyproject.toml by its build-system's backend.
var buildBackends = map[string]string{"poetry.core.masonry.api": "Poetry", "hatchling.build": "Hatch", //nolint:gochecknoglobals // fixed table
	"setuptools.build_meta": "setuptools", "flit_core.buildapi": "Flit", "pdm.backend": "PDM", "maturin": "maturin",
	"scikit_build_core.build": "scikit-build", "mesonpy": "meson-python", "uv_build": "uv"}

// parsePyproject reads a pyproject.toml: the project's name, version and requires-python, its dependencies, optional dependencies
// and dependency groups (PEP 621, PEP 735), or Poetry's; the build tool from the build backend, uv when uv.lock is beside it.
func parsePyproject(b []byte, beside func(string) bool) Manifest {
	m := Manifest{Ecosystem: "pypi", Language: "Python", BuildTool: "pip"}
	var pj struct {
		BuildSystem struct {
			Backend string `toml:"build-backend"`
		} `toml:"build-system"`
		Project struct {
			Name                 string              `toml:"name"`
			Version              string              `toml:"version"`
			RequiresPython       string              `toml:"requires-python"`
			Dependencies         []string            `toml:"dependencies"`
			OptionalDependencies map[string][]string `toml:"optional-dependencies"`
		} `toml:"project"`
		DependencyGroups map[string][]any `toml:"dependency-groups"`
		Tool             struct {
			Poetry struct {
				Name         string         `toml:"name"`
				Version      string         `toml:"version"`
				Dependencies map[string]any `toml:"dependencies"`
				Group        map[string]struct {
					Dependencies map[string]any `toml:"dependencies"`
				} `toml:"group"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(b, &pj); err != nil {
		m.Problems = append(m.Problems, problem("", "unparseable", "not valid TOML: %v", err))
		return m
	}
	if tool, ok := buildBackends[pj.BuildSystem.Backend]; ok {
		m.BuildTool = tool
	}
	if beside("uv.lock") {
		m.BuildTool = "uv"
	} else if beside("poetry.lock") {
		m.BuildTool = "Poetry"
	}
	po := pj.Tool.Poetry
	m.Name, m.Version = name(cmpOr(pj.Project.Name, po.Name)), version(cmpOr(pj.Project.Version, po.Version))
	python := pj.Project.RequiresPython
	var d deps
	for _, r := range pj.Project.Dependencies {
		n, v := requirement(r)
		d.add(n, v, "runtime")
	}
	for _, g := range slices.Sorted(maps.Keys(pj.Project.OptionalDependencies)) {
		for _, r := range pj.Project.OptionalDependencies[g] {
			n, v := requirement(r)
			d.add(n, v, "optional")
		}
	}
	for _, g := range slices.Sorted(maps.Keys(pj.DependencyGroups)) {
		for _, r := range pj.DependencyGroups[g] {
			if s, ok := r.(string); ok { // a table includes another group
				n, v := requirement(s)
				d.add(n, v, "dev")
			}
		}
	}
	for _, n := range slices.Sorted(maps.Keys(po.Dependencies)) {
		req, _ := cargoRequirement(po.Dependencies[n])
		if strings.EqualFold(n, "python") {
			python = cmpOr(python, req)
			continue
		}
		d.add(n, req, "runtime")
	}
	for _, g := range slices.Sorted(maps.Keys(po.Group)) {
		for _, n := range slices.Sorted(maps.Keys(po.Group[g].Dependencies)) {
			req, _ := cargoRequirement(po.Group[g].Dependencies[n])
			d.add(n, req, "dev")
		}
	}
	if python != "" {
		m.Toolchain = append(m.Toolchain, Toolchain{Name: "python", Version: version(python)})
	}
	d.finish(&m)
	return m
}

// requirement is a PEP 508 requirement's name and version specifier, its environment markers dropped.
func requirement(r string) (string, string) {
	mt := pep508.FindStringSubmatch(r)
	if mt == nil {
		return "", ""
	}
	spec, _, _ := strings.Cut(mt[3], ";")
	return mt[1], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(spec), "@"))
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
