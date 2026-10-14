package render

import (
	"strconv"
	"strings"
	"unicode"
)

// slugger reproduces github-slugger: lower-case, keep letters, marks, numbers, "_" and "-", spaces become "-", everything else is
// removed, duplicates get -1, -2 ... Its state is per document.
type slugger struct {
	seen  map[string]struct{}
	next_ map[string]int // per base: next suffix to try (github-slugger keeps the same counter; avoids quadratic probing)
}

func newSlugger() *slugger { return &slugger{seen: map[string]struct{}{}, next_: map[string]int{}} }

func (s *slugger) put(id string) { s.seen[id] = struct{}{} }

func (s *slugger) next(text string) string {
	base := slugBase(text)
	if base == "" {
		base = "heading"
	}
	id := base
	i := s.next_[base]
	for {
		if _, dup := s.seen[id]; !dup {
			break
		}
		i++
		id = base + "-" + strconv.Itoa(i)
	}
	s.next_[base] = i
	s.seen[id] = struct{}{}
	return id
}

func slugBase(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), unicode.Is(unicode.M, r), r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// NamespaceID is the only place the "doc-" prefix is applied.
func NamespaceID(slug string) string { return "doc-" + slug }
