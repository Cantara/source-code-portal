package scm

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxSubjectRunes caps Commit.Subject.
const MaxSubjectRunes = 200

// Commit is what the portal keeps of a commit. There is no e-mail field by design.
type Commit struct {
	SHA         string
	Subject     string    // first line of the message, cleaned and capped (NewSubject)
	AuthorLogin string    // GitHub login; "" when the author has no account
	AuthorName  string    // git author name, cleaned (CleanLine)
	CommittedAt time.Time // UTC; the zero time means unknown and sorts last
	URL         string
}

// HasDate reports whether CommittedAt is known.
func (c Commit) HasDate() bool { return !c.CommittedAt.IsZero() }

// CompareCommits orders commits newest first, ties by SHA descending, unknown dates last. It returns a negative
// number when a sorts before b. Every list and cursor in the portal uses this one order.
func CompareCommits(a, b Commit) int {
	switch {
	case a.HasDate() && !b.HasDate():
		return -1
	case !a.HasDate() && b.HasDate():
		return 1
	}
	if c := b.CommittedAt.Compare(a.CommittedAt); c != 0 {
		return c
	}
	return strings.Compare(b.SHA, a.SHA)
}

// NewSubject derives Commit.Subject: the first line of message, cleaned with CleanLine, capped at MaxSubjectRunes.
func NewSubject(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return CleanLine(line, MaxSubjectRunes)
}

// CleanLine makes untrusted text safe to show and to hand to an LLM as one line:
//   - invalid UTF-8 becomes U+FFFD;
//   - every Unicode space (TAB, CR, LF, VT, FF, NEL, NBSP, U+2028, U+2029, U+3000, ...) becomes one space; other C0 and C1
//     controls are dropped;
//   - invisible and direction-changing characters are dropped: soft hyphen, U+061C, U+200B, U+200E-U+200F,
//     U+202A-U+202E, U+2060-U+2064, U+2066-U+206F, U+FEFF, the tag block U+E0000-U+E007F (ASCII smuggling), every
//     other Default_Ignorable_Code_Point (U+034F, Hangul fillers U+115F, U+1160, U+3164, U+FFA0, Khmer inherent vowels,
//     U+FFF0-U+FFF8, ...) and every variation selector except U+FE0E and U+FE0F (variation-selector smuggling);
//   - U+200C and U+200D (needed by scripts and emoji) are kept, and so are U+FE0E and U+FE0F, but only directly after a
//     visible character (not at the start, after a space or after another variation selector);
//   - runs of spaces collapse to one and the result is trimmed;
//   - the result is cut after maxRunes runes (maxRunes <= 0: no cut), never inside a rune.
func CleanLine(s string, maxRunes int) string {
	var b strings.Builder
	b.Grow(len(s))
	runes, lastSpace, lastVS := 0, true, false // lastSpace=true trims leading space
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		s = s[n:]
		if r == utf8.RuneError && n == 1 {
			r = '�'
		}
		switch {
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
				runes++
			}
			continue
		case drop(r):
			continue
		case (r == 0xFE0E || r == 0xFE0F) && (lastSpace || lastVS):
			continue // a presentation selector needs a visible base before it
		}
		if maxRunes > 0 && runes >= maxRunes {
			break
		}
		b.WriteRune(r)
		lastSpace = false
		lastVS = r == 0xFE0E || r == 0xFE0F
		runes++
	}
	return strings.TrimRight(b.String(), " ")
}

func drop(r rune) bool {
	switch {
	case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		return true
	case r == 0xAD || r == 0x61C || r == 0x200B || r == 0x200E || r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064, r >= 0x2066 && r <= 0x206F:
		return true
	case r == 0xFEFF:
		return true
	case r >= 0xE0000 && r <= 0xE007F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	case unicode.Is(unicode.Variation_Selector, r) && r != 0xFE0E && r != 0xFE0F:
		return true
	case unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r):
		return true
	}
	return !unicode.IsPrint(r) && !unicode.IsSpace(r) && r != 0x200C && r != 0x200D && !unicode.Is(unicode.Mn, r) && !unicode.Is(unicode.Mc, r) && !unicode.Is(unicode.Me, r) && r != 0xFE0F && r != 0xFE0E
}
