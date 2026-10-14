// Package tomlx (rank 1; new package of unit 12, D-3): bounded TOML decode plus key-to-line index.
package tomlx

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// ErrDepth is returned when a table header or dotted key has more than MaxKeySegments segments. The cap exists because
// building the key-to-line index is quadratic in the number of segments (a 100,000-segment header took 37 s unbounded).
var ErrDepth = errors.New("limit.depth: key path too deep")

// MaxKeySegments bounds the segments of one table header or dotted key.
const MaxKeySegments = 32

// ErrKeys is returned when one table has more than MaxKeysPerTable direct keys. go-toml/v2 checks duplicate keys of a table with a linear scan per key
// (internal/tracker.SeenTracker.find), so one table of k keys costs O(k^2): 40,000 keys took 1.2 s and 90,000 (under the 1 MiB manifest limit) about 6 s (about 40 times that under -race),
// in code that cannot be interrupted (verification VF-12-01b). Array-of-tables elements are separate tables and are not affected.
var ErrKeys = errors.New("limit.nodes: too many keys in one table")

// MaxKeysPerTable bounds the direct keys of one table (real files: a few hundred; the largest corpus table has under 1,000).
const MaxKeysPerTable = 10000

// Code maps a Decode error to a diagnostic code: limit.depth for ErrDepth, limit.nodes for ErrKeys, else the ecosystem's syntax code.
func Code(err error, syntax string) string {
	switch {
	case errors.Is(err, ErrDepth):
		return "limit.depth"
	case errors.Is(err, ErrKeys):
		return "limit.nodes"
	}
	return syntax
}

// Index maps a key path to the 1-based line that defines it. Array-of-tables elements are indexed ("package[3].name").
// Keys inside inline tables and arrays are not indexed (the line of the outer key is used).
type Index map[string]int

// Line returns the line for the first path present, else 0.
func (ix Index) Line(paths ...string) int {
	for _, p := range paths {
		if l, ok := ix[p]; ok {
			return l
		}
	}
	return 0
}

// Decode decodes data (already size-checked by the caller) into a generic tree and builds the line index.
func Decode(data []byte) (map[string]any, Index, error) {
	idx := Index{}
	perTable := map[string]int{}
	// Line numbers come from a one-pass newline table and a binary search. unstable.Parser.Shape scans the document from its start on every call,
	// which made Decode quadratic in the number of keys (a 450 KiB file of 32,000 keys took 9.8 s, a 1 MiB Cargo.lock 6.7 s; verification VF-12-01).
	nl := newlines(data)
	lineAt := func(r unstable.Range) int { return sort.SearchInts(nl, int(r.Offset)) + 1 }
	var p unstable.Parser
	p.Reset(data)
	cur := map[string]int{}
	var table []string
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			var raw []string
			line := 0
			it := n.Key()
			for it.Next() {
				kn := it.Node()
				if line == 0 {
					line = lineAt(kn.Raw)
				}
				raw = append(raw, string(kn.Data))
				if len(raw) > MaxKeySegments {
					return nil, nil, ErrDepth
				}
			}
			table = table[:0]
			for i := range raw {
				rp := strings.Join(raw[:i+1], ".")
				seg := raw[i]
				if i == len(raw)-1 && n.Kind == unstable.ArrayTable {
					k, ok := cur[rp]
					if ok {
						k++
					}
					cur[rp] = k
				}
				if k, ok := cur[rp]; ok {
					seg += "[" + strconv.Itoa(k) + "]"
				}
				table = append(table, seg)
			}
			idx[strings.Join(table, ".")] = line
		case unstable.KeyValue:
			parts := append([]string(nil), table...)
			line := 0
			it := n.Key()
			for it.Next() {
				if line == 0 {
					line = lineAt(it.Node().Raw)
				}
				parts = append(parts, string(it.Node().Data))
				if len(parts) > len(table)+MaxKeySegments {
					return nil, nil, ErrDepth
				}
			}
			for k := len(table) + 1; k < len(parts); k++ { // dotted-key prefixes ("dependencies.foo" for foo.workspace = true)
				if _, ok := idx[strings.Join(parts[:k], ".")]; !ok {
					idx[strings.Join(parts[:k], ".")] = line
				}
			}
			idx[strings.Join(parts, ".")] = line
			owner := strings.Join(parts[:len(parts)-1], ".")
			if perTable[owner]++; perTable[owner] > MaxKeysPerTable {
				return nil, nil, ErrKeys
			}
		}
	}
	if err := p.Error(); err != nil {
		// The walk stopped at a syntax error with the depth and key limits already enforced up to there, so the full decoder is bounded too; it
		// produces the canonical message with line and column.
		var t map[string]any
		if e2 := toml.Unmarshal(data, &t); e2 != nil {
			return nil, nil, fmt.Errorf("toml: %w", e2)
		}
		return nil, nil, fmt.Errorf("toml: %w", err)
	}
	// Only now, with the depth and key counts bounded, hand the document to the full decoder.
	var tree map[string]any
	if err := toml.Unmarshal(data, &tree); err != nil {
		return nil, nil, fmt.Errorf("toml: %w", err)
	}
	return tree, idx, nil
}

// newlines returns the byte offsets of every '\n' in data, ascending.
func newlines(data []byte) []int {
	var nl []int
	for i, c := range data {
		if c == '\n' {
			nl = append(nl, i)
		}
	}
	return nl
}

// Accessors over the generic tree (all return zero values for a wrong type: callers diagnose, never panic).

func Map(v any) map[string]any { m, _ := v.(map[string]any); return m }
func List(v any) []any         { l, _ := v.([]any); return l }
func Str(v any) (string, bool) { s, ok := v.(string); return s, ok }
func Bool(v any) (bool, bool)  { b, ok := v.(bool); return b, ok }

// At walks nested tables: At(tree, "tool", "poetry", "name").
func At(tree map[string]any, keys ...string) any {
	var cur any = tree
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// SortedKeys returns the keys of m in lexical order (deterministic iteration).
func SortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// KeysBySource returns the keys of the table at prefix ordered by source line then name; prefix is the dotted path
// used by the index ("dependencies", "tool.poetry.dependencies").
func (ix Index) KeysBySource(prefix string, m map[string]any) []string {
	ks := SortedKeys(m)
	sort.SliceStable(ks, func(i, j int) bool { return ix[prefix+"."+ks[i]] < ix[prefix+"."+ks[j]] })
	return ks
}
