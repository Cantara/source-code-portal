package render

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
)

var tagRE = regexp.MustCompile(`^[a-z0-9:+#]+(-[a-z0-9:+#]+)*$`)

// isStrippable reports characters removed from LLM-facing text: C0/C1 controls except tab, LF and CR; zero-width space, word joiner,
// BOM inside text, bidi controls (LRM, RLM, ALM, embeddings/overrides/isolates), the Unicode tag block (ASCII smuggling), and the
// supplementary variation selectors. ZWJ (U+200D), ZWNJ (U+200C) and VS1-16 (U+FE00-FE0F) stay: emoji and Indic/Persian text need them.
func isStrippable(r rune) bool {
	switch {
	case r < 0x20:
		return r != '\t' && r != '\n' && r != '\r'
	case r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r == 0x200b, r == 0x2060, r == 0xfeff, r == 0x180e, r == 0x061c, r == 0x200e, r == 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0xe0000 && r <= 0xe007f, r >= 0xe0100 && r <= 0xe01ef:
		return true
	}
	return false
}

type span struct{ a, b int }

// Normalise returns the LLM-facing Markdown body: HTML comments outside code are removed (block and inline, found through the AST so a
// literal "<!--" inside a fenced block survives), then invisible/control characters are stripped everywhere.
func Normalise(doc ast.Node, src []byte) (string, []string) {
	var cut []span
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.HTMLBlock:
			lo, hi := -1, -1
			l := t.Lines()
			for i := 0; i < l.Len(); i++ {
				s := l.At(i)
				if lo < 0 || s.Start < lo {
					lo = s.Start
				}
				if s.Stop > hi {
					hi = s.Stop
				}
			}
			if t.HasClosure() {
				if t.ClosureLine.Stop > hi {
					hi = t.ClosureLine.Stop
				}
			}
			if lo >= 0 {
				cut = append(cut, commentSpans(src, lo, hi)...)
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			for i := 0; i < t.Segments.Len(); i++ {
				s := t.Segments.At(i)
				cut = append(cut, commentSpans(src, s.Start, s.Stop)...)
			}
		}
		return ast.WalkContinue, nil
	})
	var markers []string
	out := src
	if len(cut) > 0 {
		sort.Slice(cut, func(i, j int) bool { return cut[i].a < cut[j].a })
		var b bytes.Buffer
		pos := 0
		for _, c := range cut {
			if c.a < pos {
				if c.b > pos {
					pos = c.b
				}
				continue
			}
			b.Write(src[pos:c.a])
			pos = c.b
			// a comment that occupied whole lines takes its newline with it
			if pos < len(src) && src[pos] == '\n' && (b.Len() == 0 || b.Bytes()[b.Len()-1] == '\n') {
				pos++
			}
		}
		b.Write(src[pos:])
		out = b.Bytes()
		markers = append(markers, "html-comments-removed")
	}
	if stripped, n := stripLinkComments(out); n > 0 {
		out = stripped
		markers = append(markers, "link-comments-removed")
	}
	s := string(out)
	clean := strings.Map(func(r rune) rune {
		if isStrippable(r) {
			return -1
		}
		return r
	}, s)
	if clean != s {
		markers = append(markers, "invisible-characters-removed")
		if hasTag(s) {
			markers = append(markers, "tag-characters-removed")
		}
	}
	if !utf8.ValidString(clean) {
		clean = strings.ToValidUTF8(clean, "�")
	}
	return clean, markers
}

func hasTag(s string) bool {
	for _, r := range s {
		if r >= 0xe0000 && r <= 0xe007f {
			return true
		}
	}
	return false
}

// commentSpans finds "<!-- ... -->" inside src[a:b]; an unterminated comment runs to b.
func commentSpans(src []byte, a, b int) []span {
	var out []span
	i := a
	for i < b {
		j := bytes.Index(src[i:b], []byte("<!--"))
		if j < 0 {
			break
		}
		start := i + j
		k := bytes.Index(src[start+4:b], []byte("-->"))
		end := b
		if k >= 0 {
			end = start + 4 + k + 3
		}
		out = append(out, span{start, end})
		i = end
	}
	return out
}

// linkCommentRE matches the Markdown "comment" idioms that are link reference definitions nobody references: they render as
// nothing for a human but are plain text for a model (`[//]: # (text)`, `[//]: <> (text)`, `[comment]: <> (text)`,
// `[comment]: # "text"`). Verification V-15: HTML-comment stripping alone left this hidden-instruction channel open.
var linkCommentRE = regexp.MustCompile(`^ {0,3}\[(?://|#|comment|comments|todo)\]:[ \t]*(?:#|<>|<#>)[ \t]*(?:\(.*\)|".*"|'.*')?[ \t]*$`)

// stripLinkComments removes such lines outside fenced code and returns the number removed.
func stripLinkComments(src []byte) ([]byte, int) {
	if !bytes.Contains(src, []byte("]:")) {
		return src, 0
	}
	var out bytes.Buffer
	n, inFence := 0, false
	var fch byte
	var flen int
	for len(src) > 0 {
		var line []byte
		if i := bytes.IndexByte(src, '\n'); i >= 0 {
			line, src = src[:i+1], src[i+1:]
		} else {
			line, src = src, nil
		}
		body := bytes.TrimRight(line, "\r\n")
		trim := bytes.TrimLeft(body, " ")
		if len(body)-len(trim) < 4 && len(trim) >= 3 && (trim[0] == '`' || trim[0] == '~') {
			k := 0
			for k < len(trim) && trim[k] == trim[0] {
				k++
			}
			if !inFence && k >= 3 {
				inFence, fch, flen = true, trim[0], k
			} else if inFence && trim[0] == fch && k >= flen && len(bytes.TrimSpace(trim[k:])) == 0 {
				inFence = false
			}
		}
		if !inFence && linkCommentRE.Match(body) {
			n++
			continue
		}
		out.Write(line)
	}
	return out.Bytes(), n
}
