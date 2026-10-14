package scm

import (
	"iter"
	"slices"
	"strings"
)

// EntryType is the kind of a tree entry. The zero value is EntryUnknown, never a blob.
type EntryType uint8

const (
	EntryUnknown EntryType = iota
	EntryBlob
	EntryTree
	EntryCommit // submodule
	EntryLink   // symlink
)

func (t EntryType) String() string {
	switch t {
	case EntryBlob:
		return "blob"
	case EntryTree:
		return "tree"
	case EntryCommit:
		return "commit"
	case EntryLink:
		return "link"
	}
	return "unknown"
}

// TreeEntry is one path of a Git tree. Mode is the octal string of the git object mode ("100644", "120000", ...).
type TreeEntry struct {
	Path string // slash separated, relative to the listed tree root
	SHA  string
	Mode string
	Size int64 // blobs only
	Type EntryType
}

// Tree is an immutable, shareable listing of a Git tree. Entries are stored sorted by Path (byte order) with
// duplicate paths removed (first wins), so Lookup and Under are binary searches and iteration is deterministic.
type Tree struct {
	sha       string
	entries   []TreeEntry
	truncated bool
}

// NewTree takes ownership of entries (it sorts the slice in place): the caller must not touch it afterwards.
func NewTree(sha string, entries []TreeEntry, truncated bool) Tree {
	slices.SortStableFunc(entries, func(a, b TreeEntry) int { return strings.Compare(a.Path, b.Path) })
	entries = slices.CompactFunc(entries, func(a, b TreeEntry) bool { return a.Path == b.Path })
	return Tree{sha: sha, entries: entries, truncated: truncated}
}

func (t Tree) SHA() string     { return t.sha }
func (t Tree) Truncated() bool { return t.truncated }
func (t Tree) Len() int        { return len(t.entries) }

// All iterates every entry in path order.
func (t Tree) All() iter.Seq[TreeEntry] { return slices.Values(t.entries) }

// Lookup finds an exact path.
func (t Tree) Lookup(path string) (TreeEntry, bool) {
	i, ok := slices.BinarySearchFunc(t.entries, path, func(e TreeEntry, p string) int { return strings.Compare(e.Path, p) })
	if !ok {
		return TreeEntry{}, false
	}
	return t.entries[i], true
}

// Under iterates the entries below a directory (dir "" is the whole tree), in path order, without allocating.
func (t Tree) Under(dir string) iter.Seq[TreeEntry] {
	prefix := ""
	if dir != "" {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	return func(yield func(TreeEntry) bool) {
		i, _ := slices.BinarySearchFunc(t.entries, prefix, func(e TreeEntry, p string) int { return strings.Compare(e.Path, p) })
		for ; i < len(t.entries) && strings.HasPrefix(t.entries[i].Path, prefix); i++ {
			if !yield(t.entries[i]) {
				return
			}
		}
	}
}

// TreeOptions is shared by every ListTree consumer.
type TreeOptions struct {
	Recursive  bool
	Path       string // sub-tree to list; "" is the root
	MaxEntries int    // 0: adapter default
}
