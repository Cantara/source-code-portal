package manifests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func dirOf(p string) string { return path.Dir(p) }

// rebasePaths converts every SourcePath reference of a Parsed from "relative to the file's directory" (what a parser can know) to
// repo-relative. A reference that escapes the repository is cleared with a diagnostic (the dependency stays, marked as a path dependency
// without a resolvable location); a "_local" dependency that escapes is not a dependency of this repository at all and is dropped.
// Runs before any assembler. Names of "_local" targets are rewritten by the join assemblers of unit 13 (RR-10), not here.
func rebasePaths(p *Parsed) {
	base := dirOf(p.File.Path)
	escapes := func(joined string) bool {
		return joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined)
	}
	fix := func(d *Dependency) (drop bool) {
		if d.Source != SourcePath || d.SourceRef() == "" {
			return false
		}
		joined := path.Clean(path.Join(base, d.SourceRef()))
		if escapes(joined) {
			d.Extra.SourceRef = ""
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: DiagPathEscape, Severity: SevWarning, Message: "path dependency escapes the repository", Loc: Location{Line: int(d.Line)}})
			return d.Target.Namespace == LocalNamespace
		}
		d.Extra.SourceRef = joined
		return false
	}
	deps := p.Dependencies[:0]
	for i := range p.Dependencies {
		if !fix(&p.Dependencies[i]) {
			deps = append(deps, p.Dependencies[i])
		}
	}
	p.Dependencies = deps
	for i := range p.Imports {
		fix(&p.Imports[i])
	}
}

// defaultAssemble is the assembler used for ecosystems without their own: one unit per manifest file; lockfile, catalog and
// workspace files of the same directory attach to the first manifest unit there (or form a lock-only unit); declared dependencies
// are joined with the lock. It is a pure function of parsed and the path list.
func defaultAssemble(eco EcosystemID, parsed []Parsed, files FileSet) []Unit {
	byDir := map[string][]Parsed{}
	for _, p := range parsed {
		d := dirOf(p.File.Path)
		byDir[d] = append(byDir[d], p)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	allDirs := map[string]bool{}
	for _, f := range files.Paths() {
		allDirs[dirOf(f)] = true
	}
	var knownDirs []string
	for d := range allDirs {
		knownDirs = append(knownDirs, d)
	}
	slices.Sort(knownDirs)
	var units []Unit
	for _, d := range dirs {
		ps := byDir[d]
		slices.SortFunc(ps, func(a, b Parsed) int {
			if a.Kind != b.Kind {
				return int(a.Kind) - int(b.Kind)
			}
			return strings.Compare(a.File.Path, b.File.Path)
		})
		var manifests, others []Parsed
		for _, p := range ps {
			if p.Kind == KindManifest {
				manifests = append(manifests, p)
			} else {
				others = append(others, p)
			}
		}
		if len(manifests) == 0 {
			manifests = []Parsed{{}} // lock-only unit
		}
		for i, m := range manifests {
			parts := []Parsed{m}
			if i == 0 {
				parts = append(parts, others...)
			}
			units = append(units, mergeUnit(eco, d, parts, knownDirs))
		}
	}
	return units
}

func mergeUnit(eco EcosystemID, dir string, parts []Parsed, knownDirs []string) Unit {
	u := Unit{Ecosystem: eco, Dir: dir}
	var deps []Dependency
	toolchain := map[string]string{}
	primary := ""
	for i, p := range parts {
		if i == 0 {
			primary = p.File.Path
		}
		if i > 0 && p.File.Path != "" && p.File.Path != primary { // dependencies read from a secondary file say so (managed versions, catalogs)
			for j := range p.Dependencies {
				if p.Dependencies[j].Loc().Path == "" {
					p.Dependencies[j].SetLocPath(p.File.Path)
				}
			}
		}
		if p.File.Path != "" {
			u.Files = append(u.Files, p.File)
		}
		u.Provides = append(u.Provides, p.Provides...)
		deps = append(deps, p.Dependencies...)
		u.Locked = append(u.Locked, p.Locked...)
		u.Licenses = append(u.Licenses, p.Licenses...)
		u.Diagnostics = append(u.Diagnostics, p.Diagnostics...)
		u.Members = append(u.Members, ExpandMembers(dir, p.Members, knownDirs)...)
		for k, v := range p.Toolchain {
			if _, ok := toolchain[k]; !ok {
				toolchain[k] = v
			}
		}
	}
	var diags []Diagnostic
	u.Dependencies, diags = JoinLock(eco, dedupeDeps(deps), u.Locked)
	u.Diagnostics = append(u.Diagnostics, diags...)
	if len(toolchain) > 0 {
		u.Toolchain = toolchain
	}
	slices.Sort(u.Licenses)
	u.Licenses = slices.Compact(u.Licenses)
	slices.Sort(u.Members)
	u.Members = slices.Compact(u.Members)
	return u
}

func dedupeDeps(ds []Dependency) []Dependency {
	type key struct {
		t     ComponentID
		scope Scope
		group string
		cond  string
		q     string
	}
	seen := make(map[key]bool, len(ds))
	out := ds[:0:0]
	for _, d := range ds {
		k := key{d.Target, d.Scope, d.Group, d.Condition(), d.Qualifiers()}
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}

// finalize applies the root-owned rules to the output of ANY assembler: dir normalisation, ids, roles, caps, ordering.
func finalize(units []Unit, role map[string]Role, maxUnits int, lim LimitsConfig) ([]Unit, []Diagnostic) {
	var diags []Diagnostic
	for i := range units {
		u := &units[i]
		if c, ok := cleanRepoPath(u.Dir); ok {
			u.Dir = c
		} else if u.Dir == "." || u.Dir == "" {
			u.Dir = "."
		} else {
			u.Dir = "."
			diags = append(diags, Diagnostic{Code: DiagPathEscape, Severity: SevWarning, Message: "unit directory rejected"})
		}
		if len(u.Files) > 0 {
			u.Role = role[u.Files[0].Path]
		}
		if len(u.Dependencies) > lim.MaxDepsPerFile*4 {
			u.Dependencies = u.Dependencies[:lim.MaxDepsPerFile*4]
			diags = append(diags, Diagnostic{Code: DiagDepsCap, Severity: SevWarning, Message: "unit dependencies capped"})
		}
		u.LockedCount = len(u.Locked)
	}
	dirKey := func(d string) string { // "." (the root unit) sorts first
		if d == "." {
			return ""
		}
		return d
	}
	slices.SortStableFunc(units, func(a, b Unit) int {
		if c := strings.Compare(dirKey(a.Dir), dirKey(b.Dir)); c != 0 {
			return c
		}
		if c := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); c != 0 {
			return c
		}
		return strings.Compare(primaryPath(a), primaryPath(b))
	})
	if len(units) > maxUnits {
		diags = append(diags, Diagnostic{Code: DiagUnitCap, Severity: SevWarning, Message: fmt.Sprintf("%d units beyond the cap of %d were dropped", len(units)-maxUnits, maxUnits)})
		units = units[:maxUnits]
	}
	assignIDs(units)
	return units, diags
}

func primaryPath(u Unit) string {
	if len(u.Files) > 0 {
		return u.Files[0].Path
	}
	return ""
}

// assignIDs gives every unit "<dir>#<ecosystem>" and appends "#<primary base name>" where two units share dir and ecosystem. The directory is written
// through idSegment so that the id satisfies the contract pattern ^[^#\s]{1,100}#[^\s]{1,300}$ (UnitId of the public API) for every legal path.
func assignIDs(units []Unit) {
	base := func(u Unit) string { return idSegment(u.Dir, 210) + "#" + string(u.Ecosystem) }
	count := map[string]int{}
	for _, u := range units {
		count[base(u)]++
	}
	used := map[string]bool{}
	for i := range units {
		u := &units[i]
		id := base(*u)
		if count[id] > 1 {
			id += "#" + idSegment(path.Base(primaryPath(*u)), 40)
		}
		for n := 2; used[id]; n++ { // never fail on an unforeseen collision
			id = fmt.Sprintf("%s#%d", base(*u), n)
		}
		used[id] = true
		u.ID = id
	}
}

// idSegment makes a path usable inside a unit id: whitespace (any Unicode space) and '%' become %XX per UTF-8 byte, and a segment longer than max bytes
// is cut at a rune boundary and suffixed with "~" and 8 hex digits of the SHA-256 of the whole path, so two long directories never share an id.
func idSegment(p string, max int) string {
	var b strings.Builder
	for _, r := range p {
		if unicode.IsSpace(r) || r == '%' {
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) <= max {
		return out
	}
	sum := sha256.Sum256([]byte(p))
	cut := max - 9
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + "~" + hex.EncodeToString(sum[:4])
}

// runAssembler calls the registered assembler of eco (bounded by ctx, panics contained) and falls back to the default one.
func runAssembler(ctx context.Context, reg *Registry, eco EcosystemID, parsed []Parsed, files FileSet) (units []Unit, diags []Diagnostic) {
	a, ok := reg.Assembler(eco)
	if !ok {
		return defaultAssemble(eco, parsed, files), nil
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				diags = append(diags, Diagnostic{Code: DiagAssembleFailed, Severity: SevError, Message: fmt.Sprintf("assembler for %s panicked; default assembly used", eco)})
				units = nil
			}
		}()
		var err error
		units, err = a.Assemble(ctx, files, parsed)
		if err != nil {
			units = nil
			diags = append(diags, Diagnostic{Code: DiagAssembleFailed, Severity: SevError, Message: fmt.Sprintf("assembler for %s failed; default assembly used", eco)})
		}
	}()
	if units == nil {
		units = defaultAssemble(eco, parsed, files)
	}
	return units, diags
}

// languageTools maps unit evidence to technology facts.
var toolByBase = map[string]string{
	"pom.xml": "maven", "build.gradle": "gradle", "build.gradle.kts": "gradle", "settings.gradle": "gradle", "settings.gradle.kts": "gradle", "libs.versions.toml": "gradle", "gradle.lockfile": "gradle",
	"package-lock.json": "npm", "npm-shrinkwrap.json": "npm", "yarn.lock": "yarn", "pnpm-lock.yaml": "pnpm", "pnpm-workspace.yaml": "pnpm", "bun.lock": "bun",
	"poetry.lock": "poetry", "uv.lock": "uv", "Pipfile": "pipenv", "Pipfile.lock": "pipenv", "pdm.lock": "pdm", "pylock.toml": "pip", "requirements.txt": "pip",
	"go.mod": "go", "go.work": "go", "Cargo.toml": "cargo", "Cargo.lock": "cargo", "Directory.Packages.props": "nuget", "packages.config": "nuget", "packages.lock.json": "nuget",
	"Gemfile": "bundler", "Gemfile.lock": "bundler", "composer.json": "composer", "composer.lock": "composer", "Package.swift": "swiftpm", "Package.resolved": "swiftpm",
	"Podfile": "cocoapods", "Podfile.lock": "cocoapods", "pubspec.yaml": "pub", "pubspec.lock": "pub", "mix.exs": "mix", "mix.lock": "mix",
	"Dockerfile": "docker", ".terraform.lock.hcl": "terraform", "Chart.yaml": "helm", "Chart.lock": "helm",
}

func toolOf(p string) string {
	b := path.Base(p)
	if t, ok := toolByBase[b]; ok {
		return t
	}
	switch {
	case strings.HasSuffix(b, ".csproj"), strings.HasSuffix(b, ".fsproj"), strings.HasSuffix(b, ".vbproj"):
		return "dotnet"
	case strings.HasPrefix(b, "requirements") && strings.HasSuffix(b, ".txt"):
		return "pip"
	case strings.HasPrefix(b, "Dockerfile"), strings.HasSuffix(b, ".Dockerfile"):
		return "docker"
	case strings.Contains(p, ".github/workflows/"):
		return "github-actions"
	case strings.HasSuffix(b, ".gemspec"):
		return "bundler"
	}
	return ""
}

// deriveTechnology rolls units up into languages, build tools and toolchain facts. Fixture units are ignored.
func deriveTechnology(units []Unit, reg *Registry) Technology {
	langs, tools, ecos := map[string]int{}, map[string]bool{}, map[EcosystemID]bool{}
	tc := map[string]string{}
	for _, u := range units {
		if u.Role == RoleFixture {
			continue
		}
		ecos[u.Ecosystem] = true
		spec, _ := reg.Spec(u.Ecosystem)
		lang := spec.Language
		for _, f := range u.Files {
			if t := toolOf(f.Path); t != "" {
				tools[t] = true
			}
		}
		for _, d := range u.Dependencies {
			switch {
			case u.Ecosystem == NPM && d.Target.Name == "typescript" && d.Target.Namespace == "":
				lang = "TypeScript"
			case (u.Ecosystem == Maven) && d.Target.Namespace == "org.jetbrains.kotlin":
				langs["Kotlin"]++
			}
		}
		if _, ok := u.Toolchain["kotlin"]; ok {
			langs["Kotlin"]++
		}
		if u.Ecosystem == NuGet && len(u.Files) > 0 {
			switch path.Ext(u.Files[0].Path) {
			case ".fsproj":
				lang = "F#"
			case ".vbproj":
				lang = "Visual Basic"
			}
		}
		if lang != "" {
			langs[lang] += 1 + len(u.Dependencies)/50
		}
		for k, v := range u.Toolchain {
			if _, ok := tc[k]; !ok {
				tc[k] = v
			}
		}
	}
	t := Technology{}
	for l := range langs {
		t.Languages = append(t.Languages, l)
	}
	slices.SortFunc(t.Languages, func(a, b string) int {
		if langs[a] != langs[b] {
			return langs[b] - langs[a]
		}
		return strings.Compare(a, b)
	})
	for x := range tools {
		t.BuildTools = append(t.BuildTools, x)
	}
	slices.Sort(t.BuildTools)
	for e := range ecos {
		t.Ecosystems = append(t.Ecosystems, e)
	}
	slices.Sort(t.Ecosystems)
	if len(tc) > 0 {
		t.Toolchain = tc
	}
	return t
}

// deriveLicences summarises declared licences of locked components (or, without a lockfile, nothing: dependencies carry none).
func deriveLicences(units []Unit) LicenceSummary {
	s := LicenceSummary{ByLicence: map[string]int{}, ComponentsByLicence: map[string]int{}}
	type ck struct {
		id  ComponentID
		ver string
	}
	seen := map[ck]string{} // distinct component -> licence expression ("" = undeclared)
	for _, u := range units {
		if u.Role == RoleFixture {
			continue
		}
		for _, l := range u.Locked {
			s.Total++
			lic := strings.Join(l.Licenses, " AND ")
			if lic != "" {
				s.Declared++
				s.ByLicence[lic]++
			}
			if old, ok := seen[ck{l.ID, l.Version}]; !ok || old == "" {
				seen[ck{l.ID, l.Version}] = lic
			}
		}
		if len(u.Locked) == 0 {
			s.Total += len(u.Dependencies)
			for _, d := range u.Dependencies {
				if _, ok := seen[ck{d.Target, d.Version}]; !ok {
					seen[ck{d.Target, d.Version}] = ""
				}
			}
		}
	}
	for _, lic := range seen {
		s.Components++
		if lic != "" {
			s.ComponentsDeclared++
			s.ComponentsByLicence[lic]++
		}
	}
	s.ByLicence = capLicences(s.ByLicence)
	s.ComponentsByLicence = capLicences(s.ComponentsByLicence)
	return s
}

// capLicences keeps the 49 largest buckets and folds the rest into "other" (bounded payload).
func capLicences(m map[string]int) map[string]int {
	if len(m) <= 50 {
		return m
	}
	type kv struct {
		k string
		n int
	}
	all := make([]kv, 0, len(m))
	for k, n := range m {
		all = append(all, kv{k, n})
	}
	slices.SortFunc(all, func(a, b kv) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.k, b.k)
	})
	out, other := map[string]int{}, 0
	for i, e := range all {
		if i < 49 {
			out[e.k] = e.n
		} else {
			other += e.n
		}
	}
	out["other"] = other
	return out
}
