package releases

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/untrusted"
)

const (
	maxNotes     = 6 << 10   // a GitHub release's notes, kept
	maxSection   = 3 << 10   // a changelog section, kept
	keepSections = 10        // the newest sections of a changelog kept
	maxChangelog = 512 << 10 // a larger changelog is not read
)

// Section is the part of a changelog that tells one version.
type Section struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Cut     bool   `json:"cut,omitempty"`
}

// changelogName is the name of a root file that holds a changelog.
var changelogName = regexp.MustCompile(`(?i)^(changelog|changes|history|news|releases|release[-_ ]?notes)(\.md|\.markdown)?$`)

// versionHeading reads a heading's text for the version it names: `1.2.3`, `[1.2.3] - date`, `v1.2.3 (date)`, `[1.2.3](link) (date)`,
// `Version 1.2.3`.
var versionHeading = regexp.MustCompile(`^(?i:(?:version|release|v)\s*)?\[?v?(\d+(?:\.\d+){1,3}(?:[-+][0-9A-Za-z.+-]*[0-9A-Za-z])?)\]?(?:[\s(,:\-–—]|$)`)

// cut is s bounded to limit bytes at a line end, and whether it was cut.
func cut(s string, limit int) (string, bool) {
	s = strings.TrimSpace(untrusted.Strip(s))
	if len(s) <= limit {
		return s, false
	}
	end := strings.LastIndexByte(s[:limit], '\n')
	if end <= 0 {
		end = limit
	}
	for end > 0 && !utf8.ValidString(s[:end]) {
		end--
	}
	return strings.TrimSpace(s[:end]), true
}

// headingOf is the level and text of an ATX heading line, level 0 for any other line.
func headingOf(line string) (int, string) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return 0, ""
	}
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n < len(t) && t[n] != ' ' && t[n] != '\t' {
		return 0, ""
	}
	return n, strings.TrimSpace(strings.TrimRight(strings.TrimSpace(t[n:]), "# "))
}

// sections reads the sections of a changelog's versions, the first keepSections of them: a section starts at a heading that names a
// version and ends at the next heading of its level or above (a deeper heading, "Added", belongs to it). Fenced code is not read for
// headings.
func sections(md string) []Section {
	var out []Section
	var cur *Section
	var body strings.Builder
	level, fence := 0, ""
	flush := func() {
		if cur != nil {
			cur.Notes, cur.Cut = cut(body.String(), maxSection)
			if cur.Notes != "" {
				out = append(out, *cur)
			}
		}
		cur = nil
		body.Reset()
	}
	for line := range strings.Lines(md) {
		if f := fenceOf(line); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(f, fence[:1]) && len(f) >= len(fence):
				fence = ""
			}
		}
		if lv, text := headingOf(line); fence == "" && lv > 0 && (cur == nil || lv <= level) {
			flush()
			if len(out) >= keepSections {
				return out
			}
			if m := versionHeading.FindStringSubmatch(text); m != nil {
				cur, level = &Section{Version: untrusted.Text(m[1], maxName)}, lv
			}
			continue
		}
		if cur != nil {
			body.WriteString(line)
		}
	}
	flush()
	return out
}

// fenceOf is the run of backticks or tildes that opens or closes a fenced code block on the line, else "".
func fenceOf(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || t[0] != '`' && t[0] != '~' {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

// sectionOf is the changelog's section of the version v (a leading v ignored), nil when it has none.
func sectionOf(set *Set, v string) *Section {
	if set == nil {
		return nil
	}
	for i := range set.Changelog {
		if version(set.Changelog[i].Version) == version(v) {
			return &set.Changelog[i]
		}
	}
	return nil
}
