package search

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxTerm = 64 // bytes; a longer token is dropped

// token is one term of a text at its byte offsets [start, end) in that text. whole marks a compound's joined form, which a phrase
// and a snippet skip (its parts are at the same place).
type token struct {
	term       string
	start, end int
	whole      bool
}

// connector reports whether r joins the parts of a compound (Whydah-TypeLib, net.whydah.sso, session_timeout, ann@example.org).
func connector(r rune) bool {
	switch r {
	case '-', '_', '.', ':', '/', '@', '\'':
		return true
	}
	return false
}

func alnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// tokens yields the terms of text: each run of letters and digits joined by connectors is a compound; its parts (split at the
// connectors) are terms, a part written in camel case (TypeLib, XMLParser) also yields its words, and a compound of several parts
// yields its parts joined (whydahtypelib), so any of the forms finds it. Terms are lower case in NFKC.
func tokens(text string, yield func(token) bool) {
	i := 0
	for i < len(text) {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !alnum(r) {
			i += n
			continue
		}
		start, end := i, i
		var parts [][2]int
		ps := i
		for i < len(text) {
			r, n = utf8.DecodeRuneInString(text[i:])
			if alnum(r) {
				i += n
				end = i
				continue
			}
			if connector(r) && i+n < len(text) {
				if next, _ := utf8.DecodeRuneInString(text[i+n:]); alnum(next) {
					parts = append(parts, [2]int{ps, i})
					i += n
					ps = i
					continue
				}
			}
			break
		}
		parts = append(parts, [2]int{ps, end})
		if !compound(text, start, end, parts, yield) {
			return
		}
	}
}

// compound yields the terms of one compound text[start:end] with its parts.
func compound(text string, start, end int, parts [][2]int, yield func(token) bool) bool {
	var whole strings.Builder
	for _, p := range parts {
		part := text[p[0]:p[1]]
		t := normal(part)
		if len(parts) > 1 {
			whole.WriteString(t)
		}
		if !emit(t, p[0], p[1], false, yield) {
			return false
		}
		if words := camel(part); len(words) > 1 {
			for _, w := range words {
				if !emit(normal(part[w[0]:w[1]]), p[0]+w[0], p[0]+w[1], true, yield) {
					return false
				}
			}
		}
	}
	if len(parts) > 1 {
		return emit(whole.String(), start, end, true, yield)
	}
	return true
}

func emit(term string, start, end int, whole bool, yield func(token) bool) bool {
	if term == "" || len(term) > maxTerm {
		return true
	}
	return yield(token{term: term, start: start, end: end, whole: whole})
}

// normal is a term's form: NFKC, lower case.
func normal(s string) string {
	if isLowerASCII(s) {
		return s
	}
	return strings.ToLower(norm.NFKC.String(s))
}

func isLowerASCII(s string) bool {
	for i := range len(s) {
		if c := s[i]; c >= utf8.RuneSelf || c >= 'A' && c <= 'Z' {
			return false
		}
	}
	return true
}

// camel splits a part at its case changes: TypeLib is Type and Lib, XMLParser is XML and Parser; digits stay with what they follow
// (slf4j is one word).
func camel(s string) [][2]int {
	var out [][2]int
	start := 0
	var prev rune
	for i, r := range s {
		if i > 0 {
			next, _ := utf8.DecodeRuneInString(s[i+utf8.RuneLen(r):])
			upper := unicode.IsUpper(r)
			if upper && unicode.IsLower(prev) || upper && unicode.IsUpper(prev) && unicode.IsLower(next) {
				out = append(out, [2]int{start, i})
				start = i
			}
		}
		prev = r
	}
	return append(out, [2]int{start, len(s)})
}

// words returns the terms a query word asks for: the parts of its compound (net.whydah.sso is net, whydah and sso), not their
// camel-case words or the joined form, since the index holds each part whole.
func words(word string) []string {
	var out []string
	tokens(word, func(t token) bool {
		if !t.whole {
			out = append(out, t.term)
		}
		return true
	})
	return out
}
