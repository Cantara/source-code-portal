package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Candidate is a file of the tree that may become an indexed document.
type Candidate struct {
	Path    string
	BlobSHA string
	Size    int64
	Format  Format
	Aux     bool // AGENTS.md / CHANGELOG.md at the repository root (outside docs.root)
}

// Problem codes of discovery (stable; shown in /status and in the index counts).
const (
	ProblemBadPath        = "doc.bad-path"        // the path is empty or its DocID would exceed 1,024 bytes (300-byte paths never do)
	ProblemRouteCollision = "doc.route-collision" // two files share one route key (install.md and install.markdown)
	ProblemTreeTruncated  = "tree.truncated"      // GitHub truncated the tree; discovery ran on the targeted listings only
	ProblemOverIndexCap   = "doc.over-index-cap"  // more than MaxIndexed candidates
	ProblemNavMissing     = "nav.missing-file"    // docs.nav names a file that is not in the tree
)

// Discover selects the candidates of a tree. root is the descriptor's docs.root (or the configured default),
// include/exclude are glob lists (defaults: include "**/*.md"). It returns candidates in path order and the problems.
// The README is not a candidate: it is read through the readme endpoint.
func Discover(entries func(yield func(scm.TreeEntry) bool), root string, include, exclude []string) (cands []Candidate, problems []Problem) {
	if len(include) == 0 {
		include = []string{"**/*.md"}
	}
	prefix := strings.Trim(root, "/") + "/"
	keys := map[string]string{}
	entries(func(e scm.TreeEntry) bool {
		if e.Type != scm.EntryBlob {
			return true
		}
		fmtOf := FormatOf(e.Path)
		if fmtOf == FormatText {
			return true
		}
		aux := false
		inRoot := strings.HasPrefix(e.Path, prefix)
		if !inRoot {
			if isAux(e.Path) {
				aux = true
			} else {
				return true
			}
		}
		if inRoot {
			rel := strings.TrimPrefix(e.Path, prefix)
			// a glob is tried against the path relative to root AND the repository-relative path: the research sample writes
			// `exclude: ["docs/internal/**"]` with root `docs` (V-18), and a root-relative-only rule would silently ignore it
			if !(matchAny(include, rel) || matchAny(include, e.Path)) || matchAny(exclude, rel) || matchAny(exclude, e.Path) {
				return true
			}
		}
		if _, ok := DocIDOf(e.Path); !ok {
			problems = append(problems, Problem{ProblemBadPath, e.Path})
			return true
		}
		k := strings.ToLower(KeyOf(e.Path))
		if prev, dup := keys[k]; dup {
			problems = append(problems, Problem{ProblemRouteCollision, prev + " and " + e.Path})
			return true
		}
		keys[k] = e.Path
		cands = append(cands, Candidate{Path: e.Path, BlobSHA: e.SHA, Size: e.Size, Format: fmtOf, Aux: aux})
		return true
	})
	sort.Slice(cands, func(i, j int) bool { return cands[i].Path < cands[j].Path })
	if len(cands) > MaxIndexed {
		cands = cands[:MaxIndexed]
		problems = append(problems, Problem{ProblemOverIndexCap, "more than 200 candidate documents"})
	}
	return cands, problems
}

func isAux(p string) bool {
	if strings.Contains(p, "/") {
		return false
	}
	switch strings.ToLower(KeyOf(p)) {
	case "agents", "changelog":
		return FormatOf(p) == FormatMarkdown
	}
	return false
}

func matchAny(globs []string, rel string) bool {
	for _, g := range globs {
		if Glob(g, rel) {
			return true
		}
	}
	return false
}

// Glob matches a '/'-separated relative path against a pattern with `*` (any run inside one segment), `?` (one
// character inside one segment) and `**` (any number of whole segments, including none). Matching is case-sensitive.
// It is a dynamic program over (pattern segment, path segment) pairs: O(P x S) segment matches whatever the number of
// `**` in the pattern (verification V-02: the earlier recursive form took 57 s for 38 `**` against 32 segments).
func Glob(pattern, p string) bool {
	ps, ss := strings.Split(pattern, "/"), strings.Split(p, "/")
	// reach[j] = the first i pattern segments can match the first j path segments
	reach := make([]bool, len(ss)+1)
	reach[0] = true
	for _, seg := range ps {
		next := make([]bool, len(ss)+1)
		if seg == "**" {
			seen := false
			for j := 0; j <= len(ss); j++ {
				seen = seen || reach[j]
				next[j] = seen
			}
		} else {
			for j := 1; j <= len(ss); j++ {
				next[j] = reach[j-1] && segMatch(seg, ss[j-1])
			}
		}
		reach = next
	}
	return reach[len(ss)]
}

func segMatch(p, s string) bool {
	pr, sr := []rune(p), []rune(s)
	pi, si, star, mark := 0, 0, -1, 0
	for si < len(sr) {
		switch {
		case pi < len(pr) && (pr[pi] == '?' || pr[pi] == sr[si]) && pr[pi] != '*':
			pi++
			si++
		case pi < len(pr) && pr[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pr) && pr[pi] == '*' {
		pi++
	}
	return pi == len(pr)
}

// Ordered is a candidate with its navigation position.
type Ordered struct {
	Candidate
	Section []string
	NavRank int // position in the explicit nav, or -1
	Title   string
}

// Order arranges candidates: explicit nav first (in nav order, with section titles and nav titles), then the rest by
// front-matter order (when known: orders maps path to order) then path. A nav entry that names a file that is not a
// candidate yields a ProblemNavMissing and is skipped.
func Order(cands []Candidate, nav []NavItem, orders map[string]int, root string) ([]Ordered, []Problem) {
	byPath := map[string]Candidate{}
	for _, c := range cands {
		byPath[c.Path] = c
	}
	var out []Ordered
	var problems []Problem
	used := map[string]bool{}
	rank := 0
	var walk func(items []NavItem, section []string)
	walk = func(items []NavItem, section []string) {
		for _, it := range items {
			if len(it.Items) > 0 {
				walk(it.Items, append(append([]string(nil), section...), it.Title))
				continue
			}
			p := it.Path
			c, ok := byPath[p]
			if !ok {
				if c2, ok2 := byPath[path.Join(strings.Trim(root, "/"), p)]; ok2 {
					c, p, ok = c2, c2.Path, true
				}
			}
			if !ok {
				problems = append(problems, Problem{ProblemNavMissing, it.Path})
				continue
			}
			if used[p] {
				continue
			}
			used[p] = true
			out = append(out, Ordered{Candidate: c, Section: section, NavRank: rank, Title: it.Title})
			rank++
		}
	}
	walk(nav, nil)
	var rest []Ordered
	for _, c := range cands {
		if !used[c.Path] {
			rest = append(rest, Ordered{Candidate: c, NavRank: -1})
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		oi, iok := orders[rest[i].Path]
		oj, jok := orders[rest[j].Path]
		if iok != jok {
			return iok
		}
		if iok && oi != oj {
			return oi < oj
		}
		return rest[i].Path < rest[j].Path
	})
	return append(out, rest...), problems
}

// Admit decides, in index order, which documents of one repository become resident.
//
//	order        paths in index order (README first, then navigation order)
//	need         bytes each document would occupy when resident
//	resident     paths that are resident now and whose blob is unchanged (they keep their place first)
//	perRepo      Config.MaxResidentPerRepo
//	globalAvail  bytes this repository may use in total: ResidentBudget minus the bytes of every OTHER repository
//	maxDocs      Config.MaxDocsPerRepo
//
// Pass 1 re-admits the already resident documents in index order, pass 2 admits new ones in index order; both stop at
// the first limit a document would break and continue with the next (a large document does not block small ones
// behind it). Deterministic: the same inputs give the same admission whatever the goroutine schedule.
func Admit(order []string, need map[string]int64, resident map[string]bool, perRepo, globalAvail int64, maxDocs int) map[string]bool {
	admitted := map[string]bool{}
	var used int64
	count := 0
	try := func(p string) {
		n := need[p]
		if admitted[p] || n <= 0 || count >= maxDocs || used+n > perRepo || used+n > globalAvail {
			return
		}
		admitted[p] = true
		used += n
		count++
	}
	for _, p := range order {
		if resident[p] {
			try(p)
		}
	}
	for _, p := range order {
		try(p)
	}
	return admitted
}

// Fingerprint digests every input that determines the index: sorted (path, blob sha) pairs of candidates and special
// files, plus the effective root and the include/exclude/nav of the descriptor. Equal fingerprints mean "no file needs
// to be fetched again".
func Fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
