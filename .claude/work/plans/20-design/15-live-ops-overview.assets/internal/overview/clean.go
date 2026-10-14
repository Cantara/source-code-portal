package overview

import (
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Untrusted repository text reaches LLM-facing output only through these functions (architecture 14.5, 17.2). One line of
// text goes through line(); a whole document body is cleaned by the docs unit and only fenced here.

// line cleans one untrusted line (scm.CleanLine: control, bidi, zero-width and tag characters removed, spaces collapsed),
// cuts it after max runes with an ellipsis, and escapes Markdown so the result cannot start a link, image, HTML comment,
// heading or code span inside the list item or paragraph it is placed in.
func line(s string, max int) string {
	s = scm.CleanLine(s, 0)
	if max > 0 && utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max-1]) + "…"
	}
	return escapeInline(s)
}

// escapeInline escapes the characters that can open a link, image, HTML comment, heading, emphasis or table cell. It is a
// plain function, not a package-level strings.Replacer: the module has no mutable package-level variables (architecture G5).
func escapeInline(s string) string {
	if !strings.ContainsAny(s, "\\`[]<>*_!|#") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\\', '`', '[', ']', '*', '_', '!', '|', '#':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// urlForMarkdown makes an absolute URL safe as the destination of an inline link: parentheses, spaces, angle brackets
// and backslashes are percent-encoded (a `)` in a destination would end the link early).
func urlForMarkdown(u string) string {
	return strings.NewReplacer("(", "%28", ")", "%29", " ", "%20", "<", "%3C", ">", "%3E", `\`, "%5C", "\n", "%0A", "\r", "%0D").Replace(u)
}

// fenceBody neutralises everything in an already normalised document body that could be mistaken for a delimiter of the
// llms-full convention: `<!--` becomes `&lt;!--` and `-->` becomes `--&gt;`. The docs unit already removes HTML comments
// (so this is a second, independent barrier), and it is what keeps `<!-- scp:doc ... -->` and `<!-- scp:truncated ... -->`
// unforgeable and the comment markers of a full-text file balanced.
func fenceBody(md string) string {
	if !strings.Contains(md, "<!--") && !strings.Contains(md, "-->") {
		return md
	}
	return strings.NewReplacer("<!--", "&lt;!--", "-->", "--&gt;").Replace(md)
}

// attr renders one value of a `<!-- scp:... key=value -->` delimiter. Values are restricted to a safe alphabet and
// percent-encoded otherwise, so a path with a space, `-->` or a newline cannot end the comment or add an attribute.
func attr(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.', c == '_', c == '-', c == '/', c == '~', c == ':':
			b.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
