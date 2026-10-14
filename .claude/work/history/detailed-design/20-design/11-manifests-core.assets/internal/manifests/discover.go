package manifests

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

// TreeReader and BlobReader are the consumer-side interfaces implemented DIRECTLY by *github.Client (kernel types only).
type TreeReader interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}
type BlobReader interface {
	ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error)
}

// BlobBatchReader is the OPTIONAL batched read (GraphQL aliases, 25 per query). Missing or unreadable (binary, truncated) blobs are
// absent from the result map; the caller falls back to ReadBlob for them. Proposed addition to architecture 8.7 (section 14 of the design).
type BlobBatchReader interface {
	ReadBlobs(ctx context.Context, r scm.RepoRef, shas []string, maxBytes int64) (map[string]scm.Blob, error)
}

// Candidate is a tree entry the registry is interested in.
type Candidate struct {
	Path    string
	SHA     string
	Size    int64
	Parsers []Parser
	Role    Role
}

// Discovery is the pure result of path selection over a tree.
type Discovery struct {
	Candidates  []Candidate
	Diagnostics []Diagnostic
	Skipped     int // entries dropped by caps
}

// Diagnostic codes owned by this package (ecosystems use "<ecosystem>.<name>").
const (
	DiagLimitFiles     = "limit.files"
	DiagLimitSize      = "limit.size"
	DiagLimitTreeCalls = "limit.tree-calls"
	DiagLimitTreeEntry = "limit.tree-entries"
	DiagTreeTruncated  = "tree.truncated"
	DiagPathInvalid    = "tree.path-invalid"
	DiagSymlink        = "tree.symlink-skipped"
	DiagSubmodule      = "tree.submodule-skipped"
	DiagEncoding       = "encoding.unsupported"
	DiagFetchFailed    = "fetch.failed"
	DiagSHAMismatch    = "fetch.sha-mismatch"
	DiagBinary         = "fetch.binary"
	DiagParseFailed    = "parse.failed"
	DiagParsePanic     = "parse.panic"
	DiagParseAbandoned = "parse.abandoned"
	DiagCircuitOpen    = "parse.circuit-open"
	DiagAssembleFailed = "assemble.failed"
	DiagPathEscape     = "assemble.path-escape"
	DiagUnitCap        = "limit.units"
	DiagDepsCap        = "limit.deps"
	DiagLockedCap      = "limit.locked"
)

// LimitDiagPrefix prefixes every diagnostic code that reports a hit limit; the runner counts those into scp_manifests_limit_hits_total{limit=<suffix>}.
const LimitDiagPrefix = "limit."

// Discover selects candidate files from a tree: blobs only, valid repo-relative paths, depth and segment rules, at least one matching
// parser, size limits, deterministic order (shallow first, then path), capped at cfg.MaxFiles.
func Discover(entries func(yield func(scm.TreeEntry) bool), reg *Registry, cfg AcquisitionConfig, lim LimitsConfig) Discovery {
	excl := toSet(cfg.ExcludeSegments)
	fixt := toSet(cfg.FixtureSegments)
	var d Discovery
	for e := range entries {
		switch e.Type {
		case scm.EntryTree:
			continue
		case scm.EntryLink:
			if len(reg.Match(e.Path)) > 0 {
				d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: DiagSymlink, Severity: SevInfo, Message: "symlink not followed", Loc: Location{Path: e.Path}})
			}
			continue
		case scm.EntryCommit:
			continue // submodule: its manifests are another repository's
		}
		p, ok := cleanRepoPath(e.Path)
		if !ok {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: DiagPathInvalid, Severity: SevWarning, Message: "tree path rejected", Loc: Location{}})
			continue
		}
		dir := path.Dir(p)
		segs := []string{}
		if dir != "." {
			segs = strings.Split(dir, "/")
		}
		if len(segs) > cfg.MaxDepth || slices.ContainsFunc(segs, func(s string) bool { return excl[s] }) {
			continue
		}
		ps := reg.Match(p)
		if len(ps) == 0 {
			continue
		}
		max := int64(lim.ManifestBytes)
		for _, q := range ps {
			if k, ok := q.(Kinded); ok && k.Kind() == KindLockfile {
				max = max64(max, int64(lim.LockfileBytes))
			}
		}
		if e.Size > max {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: DiagLimitSize, Severity: SevWarning, Message: fmt.Sprintf("file is %d bytes, limit %d", e.Size, max), Loc: Location{Path: p}})
			continue
		}
		role := RolePrimary
		if slices.ContainsFunc(segs, func(s string) bool { return fixt[s] }) {
			role = RoleFixture
		}
		d.Candidates = append(d.Candidates, Candidate{Path: p, SHA: e.SHA, Size: e.Size, Parsers: ps, Role: role})
	}
	slices.SortFunc(d.Candidates, func(a, b Candidate) int {
		if da, db := strings.Count(a.Path, "/"), strings.Count(b.Path, "/"); da != db {
			return da - db
		}
		return strings.Compare(a.Path, b.Path)
	})
	if len(d.Candidates) > cfg.MaxFiles {
		d.Skipped = len(d.Candidates) - cfg.MaxFiles
		d.Candidates = d.Candidates[:cfg.MaxFiles]
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: DiagLimitFiles, Severity: SevWarning, Message: fmt.Sprintf("%d files beyond the cap of %d were not read", d.Skipped, cfg.MaxFiles)})
	}
	slices.SortFunc(d.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Code+a.Loc.Path, b.Code+b.Loc.Path) })
	return d
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func toSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, x := range s {
		m[x] = true
	}
	return m
}

// cleanRepoPath accepts only slash-separated, relative, escape-free, valid UTF-8 paths without control characters.
func cleanRepoPath(p string) (string, bool) {
	if p == "" || len(p) > 1024 || !utf8.ValidString(p) || strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return "", false
	}
	c := path.Clean(p)
	if c != p || c == "." || c == ".." || strings.HasPrefix(c, "../") || path.IsAbs(c) {
		return "", false
	}
	return c, true
}

// GitBlobSHA verifies fetched bytes against the tree entry id: SHA-1 ("blob <len>\0" + data) for 40-hex ids, SHA-256 for 64-hex ids.
func GitBlobSHA(want string, data []byte) bool {
	hdr := "blob " + strconv.Itoa(len(data)) + "\x00"
	switch len(want) {
	case 40:
		h := sha1.New()
		h.Write([]byte(hdr))
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want)
	case 64:
		h := sha256.New()
		h.Write([]byte(hdr))
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want)
	}
	return false
}
