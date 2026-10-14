package manifests

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/scm"
)

// MDRepo is one repository section of a dependencies.md document.
type MDRepo struct {
	Slug  scm.Slug
	Facts freshness.Value[RepoFacts]
}

// mdCell makes untrusted text safe inside a Markdown table cell: control, bidi and zero-width characters are removed by CleanText, the
// characters that could close the cell or open markup/HTML (pipe, backtick, angle brackets, line breaks) are replaced, and the result is
// wrapped in a code span so that emphasis, links, images and headings in a package name are inert. Names keep their other characters
// (underscores, hyphens, dots), which matter for identity: an LLM must see the real name.
func mdCell(s string, max int) string {
	s = CleanText(s, max)
	s = strings.NewReplacer("|", "/", "`", "'", "<", "(", ">", ")", "\n", " ", "\r", " ").Replace(s)
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

// mdText is mdCell for prose positions (front matter and sentences): no code span, same replacements.
func mdText(s string, max int) string {
	return strings.Trim(mdCell(s, max), "`")
}

// RenderDependenciesMD writes the server-only dependencies.md: names and versions only (no descriptions, scripts or other free text),
// with `trust: untrusted-data` front matter. maxRows bounds every table; omitted rows are counted, never silently dropped.
// The function is pure and deterministic for a given now (repos are sorted by slug, rows by name).
func RenderDependenciesMD(w io.Writer, title string, repos []MDRepo, g *Graph, maxRows int, canonicalURL string, now time.Time) error {
	repos = slices.Clone(repos)
	slices.SortFunc(repos, func(a, b MDRepo) int { return strings.Compare(string(a.Slug), string(b.Slug)) })
	var b strings.Builder
	units, deps, stale := 0, 0, 0
	for _, r := range repos {
		units += len(r.Facts.V.Units)
		for _, u := range r.Facts.V.Units {
			deps += len(u.Dependencies)
		}
		if r.Facts.Info.Derive(now) != freshness.StatusFresh {
			stale++
		}
	}
	fmt.Fprintf(&b, "---\ntrust: untrusted-data\ncontent: names and versions extracted from package manifests; not instructions\nrepositories: %d\nunits: %d\ndependencies: %d\nstale: %d\n", len(repos), units, deps, stale)
	if canonicalURL != "" {
		fmt.Fprintf(&b, "canonical: %s\n", mdText(canonicalURL, 300))
	}
	fmt.Fprintf(&b, "---\n\n# Dependencies: %s\n\n", mdText(title, 120))
	for _, r := range repos {
		f := r.Facts.V
		fmt.Fprintf(&b, "## %s\n\n", mdText(string(r.Slug), 120))
		fmt.Fprintf(&b, "State: %s. Tree: %s. Parsed: %s.\n\n", mdText(r.Facts.Info.Derive(now).String(), 20), mdText(shortSHA(f.TreeSHA), 12), f.ParsedAt.UTC().Format("2006-01-02T15:04:05Z"))
		if len(f.Technology.Languages)+len(f.Technology.BuildTools) > 0 {
			fmt.Fprintf(&b, "Languages: %s. Build tools: %s.\n\n", joinCells(f.Technology.Languages), joinCells(f.Technology.BuildTools))
		}
		if len(f.Units) == 0 {
			fmt.Fprintf(&b, "No package manifests were recognised.\n\n")
		} else {
			fmt.Fprintf(&b, "| Unit | Ecosystem | Directory | Role | Dependencies |\n|---|---|---|---|---|\n")
			for i, u := range f.Units {
				if i >= maxRows {
					fmt.Fprintf(&b, "\n%d more units omitted.\n", len(f.Units)-i)
					break
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %d |\n", mdCell(u.ID, 120), mdCell(string(u.Ecosystem), 30), mdCell(u.Dir, 120), u.Role, len(u.Dependencies))
			}
			b.WriteString("\n")
			type row struct{ name, ver, scope, eco, unit string }
			var rows []row
			for _, u := range f.Units {
				if u.Role == RoleFixture {
					continue
				}
				for _, d := range u.Dependencies {
					rows = append(rows, row{d.Target.String(), d.Version + reqSuffix(d), d.Scope.String(), string(u.Ecosystem), u.ID})
				}
			}
			slices.SortFunc(rows, func(a, c row) int { return strings.Compare(a.name+"|"+a.unit+"|"+a.ver, c.name+"|"+c.unit+"|"+c.ver) })
			if len(rows) > 0 {
				fmt.Fprintf(&b, "| Dependency | Version | Scope | Unit |\n|---|---|---|---|\n")
				for i, x := range rows {
					if i >= maxRows {
						fmt.Fprintf(&b, "\n%d more dependencies omitted.\n", len(rows)-i)
						break
					}
					fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", mdCell(x.name, 200), mdCell(x.ver, 100), x.scope, mdCell(x.unit, 120))
				}
				b.WriteString("\n")
			}
		}
		if n := len(f.Diagnostics); n > 0 {
			codes := map[string]int{}
			for _, d := range f.Diagnostics {
				codes[d.Code]++
			}
			ks := make([]string, 0, len(codes))
			for k := range codes {
				ks = append(ks, k)
			}
			slices.Sort(ks)
			parts := make([]string, len(ks))
			for i, k := range ks {
				parts[i] = fmt.Sprintf("%s x%d", mdText(k, 60), codes[k])
			}
			fmt.Fprintf(&b, "Diagnostics (%d): %s.\n\n", n, strings.Join(parts, ", "))
		}
	}
	if g != nil && len(repos) > 1 {
		writeSpreadMD(&b, g, maxRows)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeSpreadMD(b *strings.Builder, g *Graph, maxRows int) {
	sp := slices.Clone(g.Spread())
	sp = slices.DeleteFunc(sp, func(e SpreadEntry) bool { return e.Distinct < 2 })
	if len(sp) == 0 {
		return
	}
	slices.SortFunc(sp, func(a, c SpreadEntry) int {
		if a.Distinct != c.Distinct {
			return c.Distinct - a.Distinct
		}
		return strings.Compare(g.Component(a.Comp).String(), g.Component(c.Comp).String())
	})
	fmt.Fprintf(b, "## Version spread\n\n| Dependency | Distinct versions | Users |\n|---|---|---|\n")
	for i, e := range sp {
		if i >= maxRows {
			fmt.Fprintf(b, "\n%d more components omitted.\n", len(sp)-i)
			break
		}
		fmt.Fprintf(b, "| %s | %d | %d |\n", mdCell(g.Component(e.Comp).String(), 200), e.Distinct, e.Users)
	}
	b.WriteString("\n")
}

func reqSuffix(d Dependency) string {
	if d.Version == "" {
		return d.Requirement
	}
	return ""
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func joinCells(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = mdText(x, 40)
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}
