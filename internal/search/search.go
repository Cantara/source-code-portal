package search

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// The search's bounds (design/search.md).
const (
	MaxHits     = 1000 // the best hits a query keeps
	maxPrefix   = 64   // terms a prefix expands to, per segment
	snippetLen  = 240  // characters
	minFuzzy    = 5    // letters a word needs to be corrected
	k1, b       = 1.2, 0.75
	prefixScale = 0.7 // a prefix's expansion counts less than the word itself
	fuzzyScale  = 0.6 // so does a corrected word
)

// Part is a piece of a title or a snippet; Mark is a piece that matched.
type Part struct {
	Text string `json:"text"`
	Mark bool   `json:"mark,omitempty"`
}

// Hit is one document found.
type Hit struct {
	Kind    Kind
	Repo    snapshot.RepoKey // "" for a group
	Title   []Part
	URL     string
	Snippet []Part
	At      time.Time // zero when the document has no time
}

// Correction is a word the query did not find and the one it searched for instead.
type Correction struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Result is a page of a query's hits with the counts of every kind.
type Result struct {
	Counts      [kinds]int // of every kind, whichever the page shows
	Total       int        // of the page's kind (or every kind), at most MaxHits
	Hits        []Hit
	Corrections []Correction
}

// Search runs q in the view and returns the hits of kind only (every kind when only is nil) from offset, at most limit of them.
func (ix *Index) Search(ctx context.Context, q Query, only *Kind, offset, limit int) Result {
	r := ix.run(ctx, q, runOpts{})
	var res Result
	res.Counts, res.Corrections = r.counts, r.corrections
	hits := r.cands
	if only != nil {
		hits = slices.DeleteFunc(slices.Clone(hits), func(c cand) bool { return ix.segs[c.seg].kind[c.doc] != *only })
	}
	res.Total = len(hits)
	if offset < len(hits) {
		hits = hits[offset:min(len(hits), offset+limit)]
		docs := map[int][]Document{}
		for _, c := range hits {
			res.Hits = append(res.Hits, ix.hit(c, r.marks, docs))
		}
	}
	return res
}

// Suggest returns up to n repositories, groups and documents whose names or titles start with the words of text, for a search box.
func (ix *Index) Suggest(ctx context.Context, text string, n int) []Hit {
	q, err := Parse(text)
	if err != nil || len(q.clauses) == 0 {
		return nil
	}
	for i := range q.clauses {
		q.clauses[i].prefix = true
	}
	r := ix.run(ctx, q, runOpts{names: true, kinds: []Kind{KindRepo, KindGroup, KindDoc}})
	var out []Hit
	seen := map[string]bool{}
	docs := map[int][]Document{}
	for _, c := range r.cands {
		if len(out) == n {
			break
		}
		h := ix.hit(c, r.marks, docs)
		page, _, _ := strings.Cut(h.URL, "#")
		if seen[page] {
			continue
		}
		seen[page] = true
		h.Snippet = nil
		out = append(out, h)
	}
	return out
}

// runOpts narrow a run: to names and titles, and to kinds (for suggestions).
type runOpts struct {
	names bool
	kinds []Kind
}

// cand is a document that matched, with its score and the field that weighed most.
type cand struct {
	seg, doc int
	score    float64
	at       uint32
	field    Field
}

// run is a query's matches, best first.
type run struct {
	cands       []cand
	counts      [kinds]int
	corrections []Correction
	marks       matcher
}

// resolved is a clause alternative in one segment: the terms it found and how much each counts.
type resolved struct {
	term  int
	scale float64
	text  string
}

func (ix *Index) run(ctx context.Context, q Query, o runOpts) run {
	var r run
	r.corrections = ix.correct(&q)
	// The terms of each clause per segment, and their document frequencies in the view.
	df := map[string]int{}
	found := make([][][]resolved, len(ix.segs))
	for si, sg := range ix.segs {
		found[si] = make([][]resolved, len(q.clauses))
		for ci, c := range q.clauses {
			for ai, alt := range c.alts {
				scale := 1.0
				if c.scale != nil {
					scale = c.scale[ai]
				}
				if i := sg.find(alt); i >= 0 {
					found[si][ci] = append(found[si][ci], resolved{term: i, scale: scale, text: alt})
					df[alt] += sg.df(i)
				}
				if c.prefix && ai == len(c.alts)-1 {
					for _, rf := range sg.prefixed(alt, maxPrefix) {
						if rf.term == alt {
							continue
						}
						found[si][ci] = append(found[si][ci], resolved{term: rf.posts, scale: scale * prefixScale, text: rf.term})
						df[rf.term] += sg.df(rf.posts)
					}
				}
			}
		}
	}
	r.marks = matcher{exact: map[string]bool{}}
	for _, c := range q.clauses {
		for _, a := range c.alts {
			r.marks.exact[a] = true
		}
		if c.prefix {
			r.marks.prefixes = append(r.marks.prefixes, c.alts[len(c.alts)-1])
		}
	}
	n := 0.0
	for _, c := range ix.count {
		n += float64(c)
	}
	var avg [kinds][fields]float64
	for k := range kinds {
		for f := range fields {
			if ix.count[k] > 0 {
				avg[k][f] = max(float64(ix.sums[k][f])/float64(ix.count[k]), 1)
			}
		}
	}
	idf := func(t string) float64 { d := float64(df[t]); return math.Log(1 + (n-d+0.5)/(d+0.5)) }
	all := uint32(1)<<len(q.clauses) - 1
	var sc scratch
	for si, sg := range ix.segs {
		if ctx.Err() != nil {
			break
		}
		if !ix.segmentPasses(sg, q) {
			continue
		}
		sc.reset(len(sg.kind))
		for ci, c := range q.clauses {
			for _, rs := range found[si][ci] {
				w := idf(rs.text) * rs.scale
				sg.postings(rs.term, func(p posting) bool {
					k := sg.kind[p.doc]
					tf, bestF, bestW := 0.0, Field(0), 0.0
					for f := range fields {
						if p.freq[f] == 0 || c.field >= 0 && int(f) != c.field || o.names && f != FieldName && f != FieldTitle {
							continue
						}
						fw := ix.w.field[k][f]
						if fw == 0 && c.field < 0 {
							continue
						}
						fw = max(fw, 1)
						x := fw * float64(p.freq[f]) / (1 - b + b*sg.length(p.doc, f)/max(avg[k][f], 1))
						tf += x
						if x > bestW {
							bestF, bestW = f, x
						}
					}
					if tf == 0 {
						return true
					}
					sc.touch(p.doc)
					sc.mask[p.doc] |= 1 << ci
					sc.score[p.doc] += w * tf * (k1 + 1) / (k1 + tf)
					if bestW > sc.bestW[p.doc] {
						sc.best[p.doc], sc.bestW[p.doc] = bestF, bestW
					}
					return true
				})
			}
		}
		excluded := ix.excluded(sg, q)
		consider := func(d int) {
			if len(q.clauses) > 0 && sc.mask[d] != all || excluded[d] || !ix.docPasses(sg, d, q, o) {
				return
			}
			k := sg.kind[d]
			r.cands = append(r.cands, cand{seg: si, doc: d, score: sc.score[d] * ix.w.kind[k], at: sg.at[d], field: sc.best[d]})
		}
		if len(q.clauses) == 0 {
			for d := range sg.kind {
				consider(d)
			}
		} else {
			for _, d := range sc.touched {
				consider(d)
			}
		}
	}
	slices.SortFunc(r.cands, func(x, y cand) int {
		return cmp.Or(cmp.Compare(y.score, x.score), cmp.Compare(y.at, x.at), strings.Compare(string(ix.segs[x.seg].repo),
			string(ix.segs[y.seg].repo)), cmp.Compare(x.doc, y.doc))
	})
	if len(q.phrases) > 0 {
		r.cands = ix.phrased(ctx, r.cands, q.phrases)
	}
	r.cands = r.cands[:min(len(r.cands), MaxHits)]
	for _, c := range r.cands {
		r.counts[ix.segs[c.seg].kind[c.doc]]++
	}
	return r
}

// scratch holds a segment's scores while a query runs, reset for each segment by the documents it touched.
type scratch struct {
	score   []float64
	bestW   []float64
	best    []Field
	mask    []uint32
	seen    []bool
	touched []int
}

func (sc *scratch) reset(n int) {
	for _, d := range sc.touched {
		sc.score[d], sc.bestW[d], sc.best[d], sc.mask[d], sc.seen[d] = 0, 0, 0, 0, false
	}
	sc.touched = sc.touched[:0]
	if len(sc.score) < n {
		sc.score, sc.bestW, sc.best, sc.mask, sc.seen = make([]float64, n), make([]float64, n), make([]Field, n), make([]uint32, n),
			make([]bool, n)
	}
}

func (sc *scratch) touch(d int) {
	if !sc.seen[d] {
		sc.seen[d] = true
		sc.touched = append(sc.touched, d)
	}
}

// segmentPasses reports whether the repository's filters let the segment through.
func (ix *Index) segmentPasses(sg *segment, q Query) bool {
	if sg.repo == "" {
		return q.repo == "" && q.lang == "" && q.topic == "" && q.life == "" && q.dep == "" && len(q.work) == 0 && len(q.author) == 0 &&
			q.before.IsZero() && q.after.IsZero()
	}
	if q.group != "" && !slices.ContainsFunc(ix.s.GroupsOf(sg.repo), func(id string) bool { return strings.EqualFold(id, q.group) }) {
		return false
	}
	return (q.repo == "" || strings.Contains(string(sg.repo), q.repo)) && (q.lang == "" || slices.Contains(sg.attrs.languages, q.lang)) &&
		(q.topic == "" || slices.Contains(sg.attrs.topics, q.topic)) && (q.life == "" || sg.attrs.lifecycle == q.life) &&
		(q.dep == "" || slices.ContainsFunc(sg.attrs.deps, func(d string) bool { return strings.Contains(d, q.dep) }))
}

// docPasses reports whether the document's filters let it through.
func (ix *Index) docPasses(sg *segment, d int, q Query, o runOpts) bool {
	k := sg.kind[d]
	if len(q.in) > 0 && !slices.Contains(q.in, k) || len(o.kinds) > 0 && !slices.Contains(o.kinds, k) {
		return false
	}
	if sg.repo == "" && q.group != "" && !strings.EqualFold(ix.s.Groups[sg.item[d]].ID, q.group) {
		return false
	}
	if !q.before.IsZero() || !q.after.IsZero() {
		at := time.Unix(int64(sg.at[d]), 0)
		if sg.at[d] == 0 || !q.before.IsZero() && !at.Before(q.before) || !q.after.IsZero() && at.Before(q.after) {
			return false
		}
	}
	if len(q.work) > 0 {
		if k != KindCommit || ix.o.Work == nil || !slices.Contains(q.work, ix.o.Work(ix.s, ix.s.Repos[sg.repo], int(sg.item[d]))) {
			return false
		}
	}
	return true
}

// excluded returns the documents of sg holding every term of one of the query's exclusions.
func (ix *Index) excluded(sg *segment, q Query) map[int]bool {
	if len(q.not) == 0 {
		return nil
	}
	out := map[int]bool{}
	for _, terms := range q.not {
		var docs map[int]bool
		for _, t := range terms {
			i := sg.find(t)
			if i < 0 {
				docs = nil
				break
			}
			next := map[int]bool{}
			sg.postings(i, func(p posting) bool {
				if docs == nil || docs[p.doc] {
					next[p.doc] = true
				}
				return true
			})
			docs = next
		}
		for d := range docs {
			out[d] = true
		}
	}
	return out
}

// correct replaces each word of five letters or more that the view does not hold with the one-edit variant it holds most often.
func (ix *Index) correct(q *Query) []Correction {
	var out []Correction
	for ci := range q.clauses {
		c := &q.clauses[ci]
		if c.field >= 0 {
			continue
		}
		for ai, alt := range c.alts {
			if utf8.RuneCountInString(alt) < minFuzzy || ix.df(alt) > 0 || c.prefix && ai == len(c.alts)-1 && ix.hasPrefix(alt) {
				continue
			}
			best, bestDF := "", 0
			for _, v := range edits(alt) {
				if d := ix.df(v); d > bestDF || d == bestDF && d > 0 && v < best {
					best, bestDF = v, d
				}
			}
			if best == "" {
				continue
			}
			if c.scale == nil {
				c.scale = make([]float64, len(c.alts))
				for i := range c.scale {
					c.scale[i] = 1
				}
			}
			c.alts[ai], c.scale[ai] = best, fuzzyScale
			out = append(out, Correction{From: alt, To: best})
		}
	}
	return out
}

// df is the number of the view's documents with term t.
func (ix *Index) df(t string) int {
	n, h := 0, hashTerm(t)
	for _, sg := range ix.segs {
		if i := sg.findHashed(t, h); i >= 0 {
			n += sg.df(i)
		}
	}
	return n
}

func (ix *Index) hasPrefix(p string) bool {
	for _, sg := range ix.segs {
		if len(sg.prefixed(p, 1)) > 0 {
			return true
		}
	}
	return false
}

// edits returns the strings one edit from w (a deletion, transposition, substitution or insertion) over its own letters and the
// latin letters and digits.
func edits(w string) []string {
	rs := []rune(w)
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz0123456789æøå")
	for _, r := range rs {
		if !slices.Contains(alphabet, r) {
			alphabet = append(alphabet, r)
		}
	}
	seen := map[string]bool{w: true}
	var out []string
	add := func(v []rune) {
		if s := string(v); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for i := range rs {
		add(slices.Concat(rs[:i], rs[i+1:]))
		if i+1 < len(rs) {
			v := slices.Clone(rs)
			v[i], v[i+1] = v[i+1], v[i]
			add(v)
		}
		for _, a := range alphabet {
			if a != rs[i] {
				v := slices.Clone(rs)
				v[i] = a
				add(v)
			}
		}
	}
	for i := range len(rs) + 1 {
		for _, a := range alphabet {
			add(slices.Concat(rs[:i], []rune{a}, rs[i:]))
		}
	}
	return out
}

// phrased keeps the candidates whose text holds every phrase as consecutive terms, best first, until MaxHits are kept.
func (ix *Index) phrased(ctx context.Context, cands []cand, phrases [][]string) []cand {
	docs := map[int][]Document{}
	out := cands[:0]
	for _, c := range cands {
		if len(out) == MaxHits || ctx.Err() != nil {
			break
		}
		d := ix.document(c, docs)
		ok := true
		for _, ph := range phrases {
			if !slices.ContainsFunc(d.Fields[:], func(text string) bool { return hasPhrase(text, ph) }) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// hasPhrase reports whether text holds the terms of ph one after the other.
func hasPhrase(text string, ph []string) bool {
	at := 0
	found := false
	tokens(text, func(t token) bool {
		if t.whole {
			return true
		}
		if t.term == ph[at] {
			at++
			if at == len(ph) {
				found = true
				return false
			}
		} else {
			at = 0
			if t.term == ph[0] {
				at = 1
			}
		}
		return true
	})
	return found
}

// document returns the document of a candidate, reading its repository once per request (docs).
func (ix *Index) document(c cand, docs map[int][]Document) Document {
	ds, ok := docs[c.seg]
	if !ok {
		sg := ix.segs[c.seg]
		if sg.repo == "" {
			ds = groupDocuments(ix.s)
		} else {
			ds = ix.o.Read(ix.s.Repos[sg.repo]).Documents
		}
		docs[c.seg] = ds
	}
	if c.doc < len(ds) {
		return ds[c.doc]
	}
	return Document{}
}

// hit is what a candidate shows: its title and a snippet with the matches marked.
func (ix *Index) hit(c cand, m matcher, docs map[int][]Document) Hit {
	d := ix.document(c, docs)
	h := Hit{Kind: d.Kind, Repo: ix.segs[c.seg].repo, URL: d.URL, Title: m.parts(untrusted.Text(d.Title, 200)), At: d.At}
	field := c.field
	if field == FieldName || field == FieldTitle {
		field = fields
		for _, f := range []Field{FieldText, FieldDescription, FieldHeading, FieldMeta, FieldTags, FieldAuthor} {
			if m.holds(d.Fields[f]) {
				field = f
				break
			}
		}
	}
	text := ""
	if field < fields {
		text = d.Fields[field]
	} else {
		text = cmpOr(d.Fields[FieldDescription], d.Fields[FieldText])
	}
	h.Snippet = m.snippet(text)
	return h
}

// matcher tells the query's terms in a text: the words themselves and the prefixes the last word stands for.
type matcher struct {
	exact    map[string]bool
	prefixes []string
}

func (m matcher) match(term string) bool {
	return m.exact[term] || slices.ContainsFunc(m.prefixes, func(p string) bool { return strings.HasPrefix(term, p) })
}

func (m matcher) holds(text string) bool {
	found := false
	tokens(text, func(t token) bool { found = m.match(t.term); return !found })
	return found
}

// spans returns the byte ranges of the matched terms of text, merged.
func (m matcher) spans(text string) [][2]int {
	var out [][2]int
	tokens(text, func(t token) bool {
		if m.match(t.term) {
			if n := len(out); n > 0 && t.start <= out[n-1][1] {
				out[n-1][1] = max(out[n-1][1], t.end)
			} else {
				out = append(out, [2]int{t.start, t.end})
			}
		}
		return true
	})
	return out
}

// parts cuts a short text into its matched and unmatched pieces.
func (m matcher) parts(text string) []Part {
	return cut(text, m.spans(text))
}

// snippet is up to 240 characters of text around its first match, one line, the matches marked; the text's start without a match.
func (m matcher) snippet(text string) []Part {
	text = untrusted.Strip(text)
	spans := m.spans(text)
	start := 0
	if len(spans) > 0 {
		start = wordStart(text, max(spans[0][0]-snippetLen/3, 0))
	}
	end := start
	for n := 0; end < len(text) && n < snippetLen; n++ {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
	}
	var in [][2]int
	for _, s := range spans {
		if s[0] >= start && s[1] <= end {
			in = append(in, [2]int{s[0] - start, s[1] - start})
		}
	}
	ps := cut(text[start:end], in)
	if start > 0 {
		ps = append([]Part{{Text: "… "}}, ps...)
	}
	if end < len(text) {
		ps = append(ps, Part{Text: " …"})
	}
	return collapse(ps)
}

// wordStart moves i back to the start of the word it is in.
func wordStart(text string, i int) int {
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:i])
		if unicode.IsSpace(r) {
			break
		}
		i -= size
	}
	return i
}

func cut(text string, spans [][2]int) []Part {
	var out []Part
	at := 0
	for _, s := range spans {
		if s[0] > at {
			out = append(out, Part{Text: text[at:s[0]]})
		}
		out = append(out, Part{Text: text[s[0]:s[1]], Mark: true})
		at = s[1]
	}
	if at < len(text) {
		out = append(out, Part{Text: text[at:]})
	}
	return out
}

// collapse makes the parts one line: every run of white space one space, none at the ends.
func collapse(ps []Part) []Part {
	out := make([]Part, 0, len(ps))
	space := true // at the start, a space is dropped
	for _, p := range ps {
		var sb strings.Builder
		for _, r := range p.Text {
			if unicode.IsSpace(r) {
				if !space {
					sb.WriteByte(' ')
				}
				space = true
				continue
			}
			space = false
			sb.WriteRune(r)
		}
		if sb.Len() > 0 {
			out = append(out, Part{Text: sb.String(), Mark: p.Mark})
		}
	}
	if n := len(out); n > 0 && !out[n-1].Mark {
		out[n-1].Text = strings.TrimRight(out[n-1].Text, " ")
	}
	return out
}

// groupDocuments are the view's groups as documents, in the groups' segment's order.
func groupDocuments(s *snapshot.Snapshot) []Document {
	docs := make([]Document, 0, len(s.Groups))
	for i, g := range s.Groups {
		d := Document{Kind: KindGroup, Title: cmpOr(g.Name, g.ID), URL: snapshot.GroupPage(g.ID), Item: i}
		d.Fields[FieldName] = strings.TrimSpace(g.Name + " " + g.ID)
		d.Fields[FieldDescription] = g.Description
		docs = append(docs, d)
	}
	return docs
}
