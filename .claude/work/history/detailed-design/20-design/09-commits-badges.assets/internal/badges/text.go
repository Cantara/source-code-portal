package badges

import (
	"strings"
	"unicode"
)

const maxMessageRunes = 40

// cleanText makes untrusted text fit for a badge: control and bidirectional/zero-width format characters are removed or
// turned into one space, runs of spaces collapse, and the result is cut to max runes with an ellipsis. Invalid UTF-8
// becomes U+FFFD (range over a string). encoding/xml still escapes whatever remains.
func cleanText(s string, max int) string {
	var b strings.Builder
	n, space := 0, true
	for _, r := range s {
		switch {
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0x061C,
			r == 0x200B, r == 0x200C, r == 0x200D, r == 0x2060, r == 0xFEFF:
			continue
		case unicode.IsControl(r) || unicode.IsSpace(r):
			if !space {
				b.WriteByte(' ')
				n++
				space = true
			}
			continue
		}
		if n >= max {
			out := strings.TrimRight(b.String(), " ")
			rs := []rune(out)
			for len(rs) > max-1 || (len(rs) > 0 && unicode.IsMark(rs[len(rs)-1])) {
				rs = rs[:len(rs)-1]
			}
			return string(rs) + "…"
		}
		b.WriteRune(r)
		n++
		space = false
	}
	return strings.TrimRight(b.String(), " ")
}
