package search

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The query's limits (design/search.md).
const (
	MaxQuery   = 256 // characters
	maxWords   = 12
	maxFilters = 8
)

// ErrQuery is a query that cannot be run: empty, too long, too many words or filters, or a filter's value it cannot read.
var ErrQuery = errors.New("search: bad query")

// Query is a parsed query.
type Query struct {
	Text    string     // the query as given, trimmed
	clauses []clause   // each must match
	not     [][]string // a document holding every term of one is left out
	phrases [][]string // a document must hold each as consecutive terms
	in      []Kind     // empty: every kind
	group   string
	repo    string
	lang    string
	topic   string
	life    string
	author  []string // terms in the author field
	work    []string // hand, ai, automation
	dep     string
	before  time.Time
	after   time.Time
}

// clause is one required word: any of its alternatives (a word OR another), each a term, the last word's also as a prefix. A
// clause of the author filter matches its field alone.
type clause struct {
	alts   []string
	scale  []float64 // per alternative; nil: each counts in full (a corrected word counts less)
	prefix bool
	field  int // -1: any field
}

// Filters returns the filter keys a query takes, in the order the documents name them.
func Filters() []string {
	return []string{"in", "group", "repo", "lang", "topic", "lifecycle", "author", "kind", "dep", "before", "after"}
}

// Parse reads a query: words (all required), "phrases", -exclusions, OR between two words, and key:value filters (Filters). The last
// plain word also matches as a prefix.
func Parse(text string) (Query, error) {
	q := Query{Text: strings.TrimSpace(text)}
	if q.Text == "" {
		return q, fmt.Errorf("%w: empty", ErrQuery)
	}
	if utf8.RuneCountInString(q.Text) > MaxQuery {
		return q, fmt.Errorf("%w: longer than %d characters", ErrQuery, MaxQuery)
	}
	parts := split(q.Text)
	nwords, nfilters := 0, 0
	or := false
	lastPlain := -1
	for _, p := range parts {
		if !p.quoted && p.text == "OR" {
			or = len(q.clauses) > 0
			continue
		}
		if key, value, ok := strings.Cut(p.text, ":"); ok && !p.quoted && !p.not && known(key) && value != "" {
			nfilters++
			if err := q.filter(key, unquote(value)); err != nil {
				return q, err
			}
			or = false
			continue
		}
		ws := words(p.text)
		if len(ws) == 0 {
			continue
		}
		nwords += len(ws)
		switch {
		case p.not:
			q.not = append(q.not, ws)
		case p.quoted:
			for _, w := range ws {
				q.clauses = append(q.clauses, clause{alts: []string{w}, field: -1})
			}
			if len(ws) > 1 {
				q.phrases = append(q.phrases, ws)
			}
			lastPlain = -1
		case or && len(ws) == 1:
			last := &q.clauses[len(q.clauses)-1]
			last.alts = append(last.alts, ws[0])
			lastPlain = len(q.clauses) - 1
		default:
			for _, w := range ws {
				q.clauses = append(q.clauses, clause{alts: []string{w}, field: -1})
			}
			lastPlain = len(q.clauses) - 1
		}
		or = false
	}
	if nwords > maxWords {
		return q, fmt.Errorf("%w: more than %d words", ErrQuery, maxWords)
	}
	if nfilters > maxFilters {
		return q, fmt.Errorf("%w: more than %d filters", ErrQuery, maxFilters)
	}
	if lastPlain >= 0 && len(parts) > 0 && !parts[len(parts)-1].quoted && !strings.HasSuffix(text, " ") {
		if c := &q.clauses[lastPlain]; len(c.alts[len(c.alts)-1]) >= 2 {
			c.prefix = true
		}
	}
	for _, a := range q.author {
		q.clauses = append(q.clauses, clause{alts: []string{a}, field: int(FieldAuthor)})
	}
	if len(q.clauses) == 0 && !q.filtered() {
		return q, fmt.Errorf("%w: no words", ErrQuery)
	}
	return q, nil
}

// filtered reports whether the query has a filter that selects documents by itself.
func (q Query) filtered() bool {
	return q.group != "" || q.repo != "" || q.lang != "" || q.topic != "" || q.life != "" || q.dep != "" || len(q.in) > 0 ||
		len(q.work) > 0 || !q.before.IsZero() || !q.after.IsZero()
}

func known(key string) bool {
	return slices.Contains(Filters(), key)
}

func (q *Query) filter(key, value string) error {
	v := strings.ToLower(value)
	switch key {
	case "in":
		for _, s := range strings.Split(v, ",") {
			k, ok := KindOf(strings.TrimSuffix(strings.TrimSpace(s), "s"))
			if !ok {
				return fmt.Errorf("%w: in:%s is not a kind (repo, group, doc, dep, package, commit)", ErrQuery, s)
			}
			q.in = append(q.in, k)
		}
	case "group":
		q.group = v
	case "repo":
		q.repo = v
	case "lang":
		q.lang = v
	case "topic":
		q.topic = v
	case "lifecycle":
		q.life = v
	case "author":
		q.author = append(q.author, words(v)...)
	case "kind":
		for _, s := range strings.Split(v, ",") {
			switch s = strings.TrimSpace(s); s {
			case "hand", "ai", "automation":
				q.work = append(q.work, s)
			default:
				return fmt.Errorf("%w: kind:%s is not hand, ai or automation", ErrQuery, s)
			}
		}
	case "dep":
		q.dep = v
	case "before", "after":
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return fmt.Errorf("%w: %s:%s is not a date (2026-11-05)", ErrQuery, key, value)
		}
		if key == "before" {
			q.before = t
		} else {
			q.after = t.Add(24 * time.Hour)
		}
	}
	return nil
}

// part is one whitespace-separated part of a query, or a quoted one.
type part struct {
	text   string
	quoted bool
	not    bool
}

// split cuts text at spaces outside quotes; a quote may follow a minus or a filter's colon.
func split(text string) []part {
	var out []part
	for i := 0; i < len(text); {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i >= len(text) {
			break
		}
		p := part{}
		if text[i] == '-' && i+1 < len(text) && text[i+1] != ' ' {
			p.not = true
			i++
		}
		if text[i] == '"' {
			end := strings.IndexByte(text[i+1:], '"')
			if end < 0 {
				end = len(text) - i - 1
			}
			p.text, p.quoted = text[i+1:i+1+end], true
			i += end + 2
			out = append(out, p)
			continue
		}
		start := i
		for i < len(text) && text[i] != ' ' {
			if text[i] == '"' { // key:"a value"
				if end := strings.IndexByte(text[i+1:], '"'); end >= 0 {
					i += end + 2
					continue
				}
			}
			i++
		}
		p.text = text[start:i]
		out = append(out, p)
	}
	return out
}

func unquote(v string) string { return strings.Trim(v, `"`) }
