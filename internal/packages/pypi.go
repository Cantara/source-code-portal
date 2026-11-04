package packages

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/cantara/source-code-portal/internal/jsonstream"
)

const (
	pypiSimple     = "https://pypi.org/simple/"
	pypiBrowse     = "https://pypi.org/project/"
	simpleJSON     = "application/vnd.pypi.simple.v1+json"
	maxPyVersions  = 20_000 // versions of a project read
	maxPyFilenames = 400    // bytes of a file name kept
)

// pyName is a valid Python project name (PEP 508).
var pyName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,198}[A-Za-z0-9])?$`)

// normalisePy is a project name as the indexes address it (PEP 503): lower case, runs of '-', '_' and '.' as one '-'.
func normalisePy(n string) string {
	return strings.Trim(pySeparators.ReplaceAllString(strings.ToLower(n), "-"), "-")
}

var pySeparators = regexp.MustCompile(`[-_.]+`)

// PyPI is the provider of Python package indexes: pyproject.toml (PEP 621, Poetry) or setup.cfg names the project; the index's
// Simple API in JSON (PEP 691, with PEP 700's versions and upload times) says its newest release.
type PyPI struct {
	Simple string // the public index's Simple API root; pypi.org's when empty
}

func (p PyPI) public() string { return cmp.Or(p.Simple, pypiSimple) }

// Ecosystem is "pypi".
func (PyPI) Ecosystem() string { return "pypi" }

// Wants reports whether path is a pyproject.toml or a setup.cfg.
func (PyPI) Wants(p string) bool { b := path.Base(p); return b == "pyproject.toml" || b == "setup.cfg" }

// Coordinates reads the project's name: [project] name, else [tool.poetry] name (unless Poetry's package-mode is off), else
// setup.cfg's [metadata] name. A project whose classifiers say "Private :: Do Not Upload" is left out.
func (PyPI) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		name := ""
		if path.Base(f.Path) == "pyproject.toml" {
			var pj struct {
				Project struct {
					Name        string   `toml:"name"`
					Classifiers []string `toml:"classifiers"`
				} `toml:"project"`
				Tool struct {
					Poetry struct {
						Name        string   `toml:"name"`
						PackageMode *bool    `toml:"package-mode"`
						Classifiers []string `toml:"classifiers"`
					} `toml:"poetry"`
				} `toml:"tool"`
			}
			if toml.Unmarshal(f.Content, &pj) != nil {
				continue
			}
			private := func(cs []string) bool {
				for _, c := range cs {
					if strings.HasPrefix(strings.TrimSpace(c), "Private ::") {
						return true
					}
				}
				return false
			}
			switch po := pj.Tool.Poetry; {
			case pj.Project.Name != "" && !private(pj.Project.Classifiers):
				name = pj.Project.Name
			case pj.Project.Name == "" && po.Name != "" && (po.PackageMode == nil || *po.PackageMode) && !private(po.Classifiers):
				name = po.Name
			}
		} else {
			name = setupCfgName(f.Content)
		}
		if pyName.MatchString(name) {
			out = append(out, Coordinate{Ecosystem: "pypi", Name: name, Path: f.Path})
		}
	}
	return out
}

// setupCfgName is the name in setup.cfg's [metadata] section; "" when its classifiers say it is private.
func setupCfgName(b []byte) string {
	section, name := "", ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "["):
			section = strings.Trim(line, "[] ")
		case section != "metadata":
		case strings.Contains(line, "Private ::"):
			return ""
		default:
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "name" {
				name = strings.TrimSpace(v)
			}
		}
	}
	return name
}

// Latest asks the configured indexes and then the public one for the project's Simple API page.
func (p PyPI) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	var roots []string
	for _, r := range l.Configured("pypi") {
		roots = append(roots, r.URL)
	}
	if l.Public() {
		roots = append(roots, p.public())
	}
	var failed error
	for _, root := range distinct(roots) {
		v, err := p.project(ctx, l, root, c.Name)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		return v, nil
	}
	return nil, failed
}

// pyRelease is what the files of one version say: when the first was uploaded and whether every one is yanked.
type pyRelease struct {
	uploaded  time.Time
	files     int
	allYanked bool
}

// project reads root's page of name as a stream: the versions (PEP 700) and, per file, its version, upload time and yank.
func (p PyPI) project(ctx context.Context, l *Lookup, root, name string) (*Version, error) {
	n := normalisePy(name)
	var versions []string
	releases := map[string]*pyRelease{}
	err := l.Get(ctx, root+url.PathEscape(n)+"/", simpleJSON, func(r io.Reader) error {
		d := jsonstream.New(r)
		return d.Object(func(key string) error {
			switch key {
			case "versions":
				return d.Array(func() error {
					v, err := d.String(100)
					if len(versions) < maxPyVersions {
						versions = append(versions, v)
					}
					return err
				})
			case "files":
				return d.Array(func() error {
					var file, uploaded string
					yanked := false
					err := d.Object(func(k string) (err error) {
						switch k {
						case "filename":
							file, err = d.String(maxPyFilenames)
						case "upload-time":
							uploaded, err = d.String(64)
						case "yanked": // false, true, or the reason: a string is yanked too
							if b, err := d.Peek(); err != nil || b != '"' {
								yanked, err = d.Bool()
								return err
							}
							yanked = true
							return d.Skip()
						default:
							err = d.Skip()
						}
						return err
					})
					if v := fileVersion(file, n); v != "" && (releases[v] != nil || len(releases) < maxPyVersions) {
						rel := releases[v]
						if rel == nil {
							rel = &pyRelease{allYanked: true}
							releases[v] = rel
						}
						rel.files++
						rel.allYanked = rel.allYanked && yanked
						if t, err := time.Parse(time.RFC3339, uploaded); err == nil && (rel.uploaded.IsZero() || t.Before(rel.uploaded)) {
							rel.uploaded = t.UTC()
						}
					}
					return err
				})
			}
			return d.Skip()
		})
	})
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 { // an index without PEP 700: the versions of the files
		for v := range releases {
			versions = append(versions, v)
		}
	}
	var live []string
	for _, v := range versions {
		if rel := releases[v]; rel == nil || !rel.allYanked {
			live = append(live, v)
		}
	}
	v := newestStable(live)
	if v == "" {
		return nil, ErrNotFound
	}
	out := &Version{Version: v, Registry: l.Name(root), URL: root + url.PathEscape(n) + "/"}
	if root == pypiSimple {
		out.URL = pypiBrowse + url.PathEscape(n) + "/" + url.PathEscape(v) + "/"
	}
	if rel := releases[v]; rel != nil {
		out.Published = rel.uploaded
	}
	return out, nil
}

// fileVersion is the version a distribution file's name carries (a wheel's second part, an sdist's after the project's name); ""
// for another kind of file.
func fileVersion(file, project string) string {
	switch {
	case strings.HasSuffix(file, ".whl"):
		parts := strings.Split(strings.TrimSuffix(file, ".whl"), "-")
		if len(parts) >= 5 {
			return parts[1]
		}
	case strings.HasSuffix(file, ".tar.gz") || strings.HasSuffix(file, ".zip"):
		base := strings.TrimSuffix(strings.TrimSuffix(file, ".tar.gz"), ".zip")
		if i := strings.LastIndex(base, "-"); i > 0 && normalisePy(base[:i]) == project {
			return base[i+1:]
		}
	}
	return ""
}
