// Package untrusted cleans repository-authored text for display: one line, no control, bidirectional-formatting or zero-width
// characters, bounded length.
package untrusted

import (
	"strings"
	"unicode"
)

// Text removes control, bidirectional-formatting and zero-width characters, collapses whitespace runs to one space, trims, and cuts
// the result to at most maxRunes runes, ending a cut with "…".
func Text(s string, maxRunes int) string {
	out := make([]rune, 0, len(s))
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = len(out) > 0
		case invisible(r):
		default:
			if space {
				out = append(out, ' ')
				space = false
			}
			out = append(out, r)
		}
	}
	if len(out) <= maxRunes {
		return string(out)
	}
	return strings.TrimRight(string(out[:maxRunes-1]), " ") + "…"
}

// FirstLine returns the text before the first line break, ignoring leading blank lines.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(s, "\r\n\t "), "\n")
	return line
}

// Strip removes control (except newline and tab), bidirectional-formatting and zero-width characters from multi-line text.
func Strip(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && invisible(r) {
			return -1
		}
		return r
	}, s)
}

// invisible is what a reader cannot see but a model reads: control characters and every format character (Cf: bidirectional
// controls, zero-width characters, the soft hyphen, the tag block U+E0000 that spells ASCII invisibly), and the variation selectors
// supplement. The emoji variation selectors U+FE00 to U+FE0F stay.
func invisible(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r >= 0xe0100 && r <= 0xe01ef
}
