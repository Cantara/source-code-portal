package search

import (
	"bytes"
	"encoding/binary"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Kind is what a document is.
type Kind uint8

// The kinds, in the order the counts are given.
const (
	KindRepo Kind = iota
	KindGroup
	KindDoc
	KindDep
	KindPackage
	KindCommit
	KindPull
	kinds
)

func (k Kind) String() string {
	switch k {
	case KindRepo:
		return "repo"
	case KindGroup:
		return "group"
	case KindDoc:
		return "doc"
	case KindDep:
		return "dep"
	case KindPackage:
		return "package"
	case KindPull:
		return "pull"
	default:
		return "commit"
	}
}

// KindOf returns the kind named s.
func KindOf(s string) (Kind, bool) {
	for k := range kinds {
		if k.String() == s {
			return k, true
		}
	}
	return 0, false
}

// Field is a part of a document; its weight depends on the document's kind.
type Field uint8

// The fields.
const (
	FieldName        Field = iota // a repository's or group's name, a manifest's or package's coordinates
	FieldTitle                    // a descriptor's title, a document's title, a commit's subject
	FieldTags                     // topics and tags
	FieldDescription              // a description
	FieldMeta                     // language, licence, lifecycle, owners, a release or a version
	FieldHeading                  // a document section's heading
	FieldText                     // a section's text, a manifest's dependencies, a commit's body
	FieldAuthor                   // a commit's author
	fields
)

// weighting is the fields' weights per kind (0 leaves a field out of the score) and the kinds' own, which favour the documents a
// reader looks for first (design/search.md).
type weighting struct {
	field [kinds][fields]float64
	kind  [kinds]float64
}

func weights() weighting {
	return weighting{
		field: [kinds][fields]float64{
			KindRepo:    {FieldName: 8, FieldTitle: 6, FieldTags: 4, FieldDescription: 3, FieldMeta: 2, FieldText: 1},
			KindGroup:   {FieldName: 8, FieldDescription: 3},
			KindDoc:     {FieldTitle: 5, FieldHeading: 4, FieldText: 1},
			KindDep:     {FieldName: 6, FieldText: 3},
			KindPackage: {FieldName: 6, FieldMeta: 1},
			KindCommit:  {FieldTitle: 2, FieldText: 0.5, FieldAuthor: 2},
			KindPull:    {FieldTitle: 3, FieldMeta: 1, FieldAuthor: 2},
		},
		kind: [kinds]float64{KindRepo: 1.5, KindGroup: 1.5, KindDoc: 1.2, KindDep: 1, KindPackage: 1, KindCommit: 0.8, KindPull: 1},
	}
}

const maxText = 64 << 10 // bytes of a document indexed, all fields together

// Document is one searchable thing of a repository, as Options.Read gives it.
type Document struct {
	Kind   Kind
	Title  string    // what a hit shows
	URL    string    // the page a hit opens
	At     time.Time // when it happened (a commit), else zero
	Item   int       // the mapper's own index (a commit's in its history), handed back to Options.Work
	Fields [fields]string
}

// Repository is what the index reads of one repository: its attributes, for the filters, and its documents.
type Repository struct {
	Languages    []string
	Topics       []string // topics and descriptor tags
	Lifecycle    string
	Dependencies []string // the dependencies its manifests declare, by name
	Documents    []Document
}

// segment is the index of one repository (or of a view's groups): its documents' kinds, times, items and field lengths, and its
// terms with their postings. The terms are sorted and front-coded in blocks of 16, each followed by its postings, in one byte slice;
// a block's start is the one offset kept, so a term costs its new bytes and its postings (design/search.md).
type segment struct {
	repo   snapshot.RepoKey // "" for the groups
	key    uint64           // what it was built from (cache.build)
	used   uint64           // the last build that used it
	attrs  attrs
	kind   []Kind
	at     []uint32 // Unix seconds; 0 when the document has no time
	item   []int32
	lens   []uint8 // fields per document, quantised (quantise)
	sums   [kinds][fields]uint64
	count  [kinds]uint32
	blob   []byte
	heads  []uint32 // the offset of each block of terms in blob
	nterms int
	bloom  []uint64 // a Bloom filter of the terms: a term it lacks is not looked up (a correction tries hundreds)
}

const block = 16 // terms per front-coded block

// attrs are the repository's attributes, lower case.
type attrs struct {
	languages, topics, deps []string
	lifecycle               string
}

// posting is one document's frequencies of a term, per field.
type posting struct {
	doc  int
	freq [fields]uint8
}

// ref is a term found in a segment: its text and where its postings start.
type ref struct {
	term  string
	posts int
}

// build indexes the repository's documents.
func build(repo snapshot.RepoKey, r Repository) *segment {
	sg := &segment{repo: repo, attrs: attrs{languages: lower(r.Languages), topics: lower(r.Topics), deps: lower(r.Dependencies),
		lifecycle: strings.ToLower(r.Lifecycle)}}
	n := len(r.Documents)
	sg.kind, sg.at, sg.item, sg.lens = make([]Kind, n), make([]uint32, n), make([]int32, n), make([]uint8, n*int(fields))
	postings := map[string][]posting{}
	for d, doc := range r.Documents {
		sg.kind[d], sg.item[d] = doc.Kind, int32(min(doc.Item, 1<<31-1)) //nolint:gosec // G115: bounded
		if u := doc.At.Unix(); !doc.At.IsZero() && u > 0 && u < 1<<32 {
			sg.at[d] = uint32(u)
		}
		sg.count[doc.Kind]++
		budget := maxText
		for f := range fields {
			text := doc.Fields[f]
			if len(text) > budget {
				text = text[:budget]
			}
			budget -= len(text)
			length := 0
			tokens(text, func(t token) bool {
				if !t.whole {
					length++
				}
				ps := postings[t.term]
				if len(ps) == 0 || ps[len(ps)-1].doc != d {
					ps = append(ps, posting{doc: d})
				}
				if p := &ps[len(ps)-1]; p.freq[f] < 255 {
					p.freq[f]++
				}
				postings[t.term] = ps
				return true
			})
			sg.lens[d*int(fields)+int(f)] = quantise(length)
			sg.sums[doc.Kind][f] += uint64(length)
		}
	}
	terms := make([]string, 0, len(postings))
	for t := range postings {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	sg.nterms = len(terms)
	var enc []byte
	prev := ""
	for i, t := range terms {
		shared := 0
		if i%block == 0 {
			sg.heads = append(sg.heads, uint32(len(sg.blob))) //nolint:gosec // G115: a segment is far below 4 GiB
		} else {
			for shared < len(prev) && shared < len(t) && prev[shared] == t[shared] {
				shared++
			}
			sg.blob = binary.AppendUvarint(sg.blob, uint64(shared))
		}
		sg.blob = binary.AppendUvarint(sg.blob, uint64(len(t)-shared)) //nolint:gosec // G115: non-negative
		sg.blob = append(sg.blob, t[shared:]...)
		enc = encode(enc[:0], postings[t])
		sg.blob = binary.AppendUvarint(sg.blob, uint64(len(enc)))
		sg.blob = append(sg.blob, enc...)
		prev = t
	}
	sg.blob = slices.Clip(sg.blob)
	sg.bloom = make([]uint64, max(1, (len(terms)*bloomBits+63)/64))
	for _, t := range terms {
		h := hashTerm(t)
		for _, bit := range sg.bits(h) {
			sg.bloom[bit/64] |= 1 << (bit % 64)
		}
	}
	return sg
}

const bloomBits = 10 // bits per term: about 1% false positives with three probes

// hashTerm is a term's hash for the Bloom filters (FNV-1a).
func hashTerm(t string) uint64 {
	h := uint64(14695981039346656037)
	for i := range len(t) {
		h ^= uint64(t[i])
		h *= 1099511628211
	}
	return h
}

// bits are the filter's three bits of the hash h.
func (sg *segment) bits(h uint64) [3]uint64 {
	n := uint64(len(sg.bloom)) * 64
	h1, h2 := h, h>>33|1
	return [3]uint64{h1 % n, (h1 + h2) % n, (h1 + 2*h2) % n}
}

// mayHave reports whether the segment may hold the term of hash h; false is certain.
func (sg *segment) mayHave(h uint64) bool {
	for _, bit := range sg.bits(h) {
		if sg.bloom[bit/64]&(1<<(bit%64)) == 0 {
			return false
		}
	}
	return true
}

// encode appends the postings: their count, then per posting the document's distance from the last and its frequencies. A posting
// in one field fewer than 17 times (most) is one byte: the field and the frequency less one; else 0x80, the fields' mask and each
// frequency.
func encode(b []byte, ps []posting) []byte {
	b = binary.AppendUvarint(b, uint64(len(ps)))
	prev := 0
	for _, p := range ps {
		b = binary.AppendUvarint(b, uint64(p.doc-prev)) //nolint:gosec // G115: ascending
		prev = p.doc
		var mask byte
		one, field := 0, 0
		for f := range fields {
			if p.freq[f] > 0 {
				mask |= 1 << f
				one++
				field = int(f)
			}
		}
		if one == 1 && p.freq[field] <= 16 {
			b = append(b, byte(field)|(p.freq[field]-1)<<3)
			continue
		}
		b = append(b, 0x80, mask)
		for f := range fields {
			if p.freq[f] > 0 {
				b = append(b, p.freq[f])
			}
		}
	}
	return b
}

// quantise keeps a field's length in a byte: exact below 128, then in steps of 32 up to 4,192.
func quantise(n int) uint8 {
	if n < 128 {
		return uint8(n) //nolint:gosec // G115: < 128
	}
	return uint8(min(255, 128+(n-128)/32))
}

func lower(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// size is the bytes the segment holds, about.
func (sg *segment) size() int {
	return len(sg.blob) + 4*len(sg.heads) + 8*len(sg.bloom) + len(sg.kind) + 4*len(sg.at) + 4*len(sg.item) + len(sg.lens)
}

func (sg *segment) numTerms() int { return sg.nterms }

// scan yields the terms of the block from offset at, each with its postings' offset, until yield returns false; it returns the
// offset after the last term read.
func (sg *segment) scan(blk int, yield func(term []byte, posts int) bool) {
	at := int(sg.heads[blk])
	var term []byte
	for i := 0; i < block && at < len(sg.blob); i++ {
		shared := uint64(0)
		if i > 0 {
			var k int
			shared, k = binary.Uvarint(sg.blob[at:])
			at += k
		}
		n, k := binary.Uvarint(sg.blob[at:])
		at += k
		term = append(term[:shared], sg.blob[at:at+int(n)]...) //nolint:gosec // G115: a term's length
		at += int(n)                                           //nolint:gosec // G115: as above
		plen, k := binary.Uvarint(sg.blob[at:])
		at += k
		if !yield(term, at) {
			return
		}
		at += int(plen) //nolint:gosec // G115: a length within the blob
	}
}

// headTerm is the first term of block blk.
func (sg *segment) headTerm(blk int) []byte {
	at := int(sg.heads[blk])
	n, k := binary.Uvarint(sg.blob[at:])
	return sg.blob[at+k : at+k+int(n)] //nolint:gosec // G115: a term's length
}

// firstBlock is the block that holds the first term not less than t: the last block whose head is not greater than t.
func (sg *segment) firstBlock(t []byte) int {
	i := sort.Search(len(sg.heads), func(i int) bool { return bytes.Compare(sg.headTerm(i), t) > 0 })
	return max(i-1, 0)
}

// find returns where the postings of term t start, or -1.
func (sg *segment) find(t string) int { return sg.findHashed(t, hashTerm(t)) }

// findHashed is find with the term's hash given.
func (sg *segment) findHashed(t string, h uint64) int {
	if sg.nterms == 0 || !sg.mayHave(h) {
		return -1
	}
	want := []byte(t)
	found := -1
	sg.scan(sg.firstBlock(want), func(term []byte, posts int) bool {
		switch c := bytes.Compare(term, want); {
		case c == 0:
			found = posts
			return false
		case c > 0:
			return false
		}
		return true
	})
	return found
}

// prefixed returns the terms that start with p, at most limit of them.
func (sg *segment) prefixed(p string, limit int) []ref {
	if sg.nterms == 0 {
		return nil
	}
	want := []byte(p)
	var out []ref
	done := false
	for blk := sg.firstBlock(want); blk < len(sg.heads) && !done; blk++ {
		sg.scan(blk, func(term []byte, posts int) bool {
			if bytes.Compare(term, want) < 0 {
				return true
			}
			if !bytes.HasPrefix(term, want) || len(out) == limit {
				done = true
				return false
			}
			out = append(out, ref{term: string(term), posts: posts})
			return true
		})
	}
	return out
}

// df returns the number of documents with the term whose postings start at posts.
func (sg *segment) df(posts int) int {
	n, _ := binary.Uvarint(sg.blob[posts:])
	return int(n) //nolint:gosec // G115: a count
}

// postings yields the postings that start at posts.
func (sg *segment) postings(posts int, yield func(posting) bool) {
	b := sg.blob[posts:]
	n, k := binary.Uvarint(b)
	b = b[k:]
	doc := 0
	for range n {
		delta, k := binary.Uvarint(b)
		b = b[k:]
		doc += int(delta) //nolint:gosec // G115: a document number
		p := posting{doc: doc}
		if c := b[0]; c < 0x80 {
			p.freq[c&7] = c>>3 + 1
			b = b[1:]
		} else {
			mask := b[1]
			b = b[2:]
			for f := range fields {
				if mask&(1<<f) != 0 {
					p.freq[f] = b[0]
					b = b[1:]
				}
			}
		}
		if !yield(p) {
			return
		}
	}
}

// length is the length of field f of document doc, about.
func (sg *segment) length(doc int, f Field) float64 {
	q := sg.lens[doc*int(fields)+int(f)]
	if q < 128 {
		return float64(q)
	}
	return float64(128 + int(q-128)*32 + 16)
}
