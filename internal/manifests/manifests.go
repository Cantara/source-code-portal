// Package manifests reads the package manifests of every catalogued repository (go.mod, package.json, pom.xml) as untrusted input
// and keeps the technology and the dependencies they declare. Internal dependencies between repositories are indexed by the snapshot.
package manifests

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// Kind is the job kind of the per-repository refresh.
const Kind = "manifests"

const (
	ttl           = 6 * time.Hour // pushes notify the job
	maxFiles      = 30
	maxDirDepth   = 4
	maxFileBytes  = 512 << 10
	payloadSchema = 1
	// parserVersion moves when any parser's output changes; stored manifests made by another version are parsed again.
	parserVersion = 1
)

// ecosystems maps a manifest's base name to its ecosystem.
var ecosystems = map[string]string{"go.mod": "go", "package.json": "npm", "pom.xml": "maven"} //nolint:gochecknoglobals // fixed table

// skipped are directory names whose manifests belong to vendored code, fixtures or examples.
var skipped = map[string]bool{"node_modules": true, "vendor": true, "testdata": true, "test": true, "tests": true, //nolint:gochecknoglobals // fixed table
	"example": true, "examples": true, "fixtures": true}

// GitHub reads trees and blobs of one source; *github.Source implements it.
type GitHub interface {
	Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]github.TreeEntry, error)
	Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
}

// Manifests is the manifests feature.
type Manifests struct {
	sources map[string]GitHub
}

// New returns the feature for one GitHub per source id.
func New(sources map[string]GitHub) *Manifests { return &Manifests{sources: sources} }

// record is one stored manifest with the blob it was parsed from.
type record struct {
	BlobSHA  string            `json:"blobSha"`
	Manifest snapshot.Manifest `json:"manifest"`
}

type payload struct {
	Schema  int      `json:"schema"`
	Parser  int      `json:"parser"`
	Records []record `json:"records"`
}

func decode(b []byte) payload {
	var p payload
	if len(b) == 0 || json.Unmarshal(b, &p) != nil || p.Schema != payloadSchema {
		return payload{}
	}
	return p
}

// Spec is the per-repository refresh job.
func (m *Manifests) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: []sync.Event{sync.EventPush}, Run: m.run, Publish: publish}
}

func (m *Manifests) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	gh := m.sources[j.Source]
	if gh == nil {
		return sync.Result{}, errors.New("manifests: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	tree, err := gh.Tree(ctx, owner, name, "HEAD", true)
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		return result(snapshot.StateNoContent, payload{Schema: payloadSchema, Parser: parserVersion}, j.Prev)
	case github.IsNotFound(err):
		return result(snapshot.StateNotFound, payload{Schema: payloadSchema, Parser: parserVersion}, j.Prev)
	case err != nil:
		return sync.Result{}, err
	}
	prev := decode(j.Prev.Payload)
	known := map[string]record{}
	if prev.Parser == parserVersion {
		for _, r := range prev.Records {
			known[r.Manifest.Path] = r
		}
	}
	paths := map[string]bool{}
	for _, e := range tree {
		if e.Type == "blob" {
			paths[e.Path] = true
		}
	}
	next := payload{Schema: payloadSchema, Parser: parserVersion}
	for _, f := range discover(tree) {
		if r, ok := known[f.Path]; ok && r.BlobSHA == f.SHA {
			next.Records = append(next.Records, r)
			continue
		}
		eco := ecosystems[path.Base(f.Path)]
		if f.Size > maxFileBytes {
			next.Records = append(next.Records, record{BlobSHA: f.SHA, Manifest: snapshot.Manifest{Path: f.Path, Ecosystem: eco,
				Problems: []snapshot.Problem{{Code: "too-large", Message: "the file is over 512 KiB and was not read"}}}})
			continue
		}
		b, err := gh.Blob(ctx, owner, name, f.SHA)
		if err != nil {
			return sync.Result{}, err
		}
		next.Records = append(next.Records, record{BlobSHA: f.SHA, Manifest: parse(f.Path, b, paths)})
	}
	return result(snapshot.StateOK, next, j.Prev)
}

// discover picks the manifests of a recursive tree: shallow paths first, then by path, at most maxFiles.
func discover(tree []github.TreeEntry) []github.TreeEntry {
	var out []github.TreeEntry
	for _, e := range tree {
		if e.Type != "blob" || ecosystems[path.Base(e.Path)] == "" {
			continue
		}
		dir := path.Dir(e.Path)
		if dir == "." {
			out = append(out, e)
			continue
		}
		segs := strings.Split(dir, "/")
		if len(segs) <= maxDirDepth && !slices.ContainsFunc(segs, func(s string) bool { return skipped[s] || strings.HasPrefix(s, ".") }) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b github.TreeEntry) int {
		return cmp.Or(cmp.Compare(strings.Count(a.Path, "/"), strings.Count(b.Path, "/")), strings.Compare(a.Path, b.Path))
	})
	return out[:min(len(out), maxFiles)]
}

// parse reads one manifest; paths are the repository's files, for what sits beside it.
func parse(p string, b []byte, paths map[string]bool) snapshot.Manifest {
	var m snapshot.Manifest
	switch ecosystems[path.Base(p)] {
	case "go":
		m = parseGoMod(b)
	case "npm":
		dir := path.Dir(p)
		m = parsePackageJSON(b, func(name string) bool { return paths[path.Join(dir, name)] })
	default:
		m = parsePOM(b)
	}
	m.Path = p
	sortDependencies(m.Dependencies)
	return m
}

// scopeOrder is the display order of dependency scopes.
var scopeOrder = []string{"parent", "import", "runtime", "peer", "optional", "provided", "dev", "test", "tool", "indirect"} //nolint:gochecknoglobals // fixed table

func sortDependencies(ds []snapshot.Dependency) {
	slices.SortStableFunc(ds, func(a, b snapshot.Dependency) int {
		return cmp.Or(cmp.Compare(slices.Index(scopeOrder, a.Scope), slices.Index(scopeOrder, b.Scope)), strings.Compare(a.Name, b.Name))
	})
}

func result(state snapshot.State, p payload, prev store.Item) (sync.Result, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return sync.Result{}, err
	}
	sum := sha256.New()
	for _, r := range p.Records {
		sum.Write([]byte(r.Manifest.Path + " " + r.BlobSHA + "\n"))
	}
	sha := hex.EncodeToString(sum.Sum(nil))
	return sync.Result{State: state, ContentSHA: sha, Payload: b,
		Unchanged: prev.State == state && prev.ContentSHA == sha && bytes.Equal(prev.Payload, b)}, nil
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	p := decode(it.Payload)
	ms := make([]snapshot.Manifest, 0, len(p.Records))
	for _, r := range p.Records {
		ms = append(ms, r.Manifest)
	}
	slices.SortFunc(ms, func(a, b snapshot.Manifest) int { return strings.Compare(a.Path, b.Path) })
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { e.Manifests, e.ManifestsFresh = ms, fresh })
}
