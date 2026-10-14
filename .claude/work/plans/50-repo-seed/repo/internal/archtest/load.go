package archtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

type listed struct {
	ImportPath   string
	Name         string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Error        *struct{ Err string }
	DepsErrors   []struct{ Err string }
}

// Load runs `go list -e -json ./...` in dir for every build-tag set (default: none, spa, devtools: the three builds that exist) and returns the union of the packages, with
// internal import paths made module-relative. Test imports of the package and of its x_test package are merged.
// The go child process is invisible to the test cache, so Load also reads every .go file and go.mod/go.sum under dir
// (ReadInputs): a changed import then invalidates a cached passing result (verified in 05-research/04 section 8).
func Load(dir string, tagSets ...string) (module string, pkgs []Pkg, err error) {
	if len(tagSets) == 0 {
		tagSets = []string{"", "spa", "devtools"}
	}
	if err := ReadInputs(dir); err != nil {
		return "", nil, err
	}
	out, err := run(dir, "go", "list", "-m")
	if err != nil {
		return "", nil, err
	}
	module = strings.TrimSpace(string(out))
	merged := map[string]*Pkg{}
	for _, tags := range tagSets {
		args := []string{"list", "-e", "-json", "./..."}
		if tags != "" {
			args = []string{"list", "-e", "-tags", tags, "-json", "./..."}
		}
		out, err := run(dir, "go", args...)
		if err != nil {
			return "", nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(out))
		for {
			var l listed
			if err := dec.Decode(&l); err == io.EOF {
				break
			} else if err != nil {
				return "", nil, fmt.Errorf("decode go list: %w", err)
			}
			if l.Error != nil && !embedMissing(l.Error.Err) {
				return "", nil, fmt.Errorf("go list: %s: %s", l.ImportPath, l.Error.Err)
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(l.ImportPath, module), "/")
			p := merged[rel]
			if p == nil {
				p = &Pkg{Path: rel, Name: strings.TrimSuffix(l.Name, "_test")}
				merged[rel] = p
			}
			p.Imports = union(p.Imports, relativise(module, l.Imports))
			p.TestImports = union(p.TestImports, relativise(module, append(append([]string(nil), l.TestImports...), l.XTestImports...)))
		}
	}
	for _, p := range merged {
		pkgs = append(pkgs, *p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Path < pkgs[j].Path })
	return module, pkgs, nil
}

// embedMissing reports whether err is only an unsatisfied //go:embed pattern: the `spa` build of package web embeds web/dist, which a
// checkout that has not run `make web` does not have (the `go` job of CI). The package's imports are still listed with -e, and the
// matrix is about imports; a build that really needs dist fails in its own job (`build`), so this is no loss of coverage.
func embedMissing(err string) bool {
	return strings.Contains(err, "pattern ") && strings.Contains(err, "no matching files found")
}

func relativise(module string, in []string) []string {
	out := make([]string, 0, len(in))
	for _, imp := range in {
		if imp == module || strings.HasPrefix(imp, module+"/") {
			imp = strings.TrimPrefix(strings.TrimPrefix(imp, module), "/")
		}
		out = append(out, imp)
	}
	return out
}

func union(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func run(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

// ReadInputs opens every input of the module so that `go test` records them in its cache key. It walks an fs.FS rooted at dir (a
// root-scoped read: no path leaves dir through a symlink swapped between the walk and the read, gosec G122).
func ReadInputs(dir string) error {
	fsys := os.DirFS(dir)
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(p) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") || d.Name() == "go.mod" || d.Name() == "go.sum" {
			_, err := fs.ReadFile(fsys, p)
			return err
		}
		return nil
	})
}

// skipDir says which directories no rule looks into: dependencies, VCS data and the design package (.claude/, whose reference assets
// are not product code; go list ./... skips dot directories too) anywhere, test data anywhere, and by exact path the nested tools
// module (its own go.mod; outside go list ./...) and the build outputs web/dist and dist. Matching
// the BASENAME "tools" or "dist" anywhere would hide a product package of that name from the source rules.
func skipDir(rel string) bool {
	switch path.Base(rel) {
	case "node_modules", ".git", ".claude", "testdata":
		return true
	}
	switch rel {
	case "tools", "dist", "web/dist", ".dev", "bin":
		return true
	}
	return false
}
