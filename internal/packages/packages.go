// Package packages finds what a repository publishes to package registries and the newest release there (design/packages.md):
// one Provider per package manager reads the coordinates from the build files, and asks the registries the build file names, the
// configured ones and the public one.
package packages

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// Kind is the job kind that looks up a repository's packages.
const Kind = "packages"

const (
	ttl           = 6 * time.Hour    // pushes and tags notify the job
	failedAgain   = 30 * time.Minute // a lookup that failed is asked again after this
	maxDirDepth   = 2                // directories below the root searched for build files
	maxFiles      = 20               // build files read
	maxFileBytes  = 256 << 10        // a larger build file is not read
	maxPackages   = 20               // coordinates looked up
	lookups       = 8                // lookups of one job at once
	blobReads     = 4                // build files read at once
	payloadSchema = 1
)

// skipped are directories whose build files are vendored code, tests or examples, not what the repository publishes.
var skipped = map[string]bool{"node_modules": true, "vendor": true, "testdata": true, "test": true, "tests": true, //nolint:gochecknoglobals // fixed table
	"example": true, "examples": true, "fixtures": true, "samples": true, "docs": true}

// GitHub reads a source's files; *github.Source implements it.
type GitHub interface {
	Files(ctx context.Context, owner, name string, keep func(dir string) bool) ([]github.TreeEntry, error)
	Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
}

// Package is one package a repository publishes, with its newest release when a registry holds one.
type Package struct {
	Coordinate
	Latest  *Version `json:"latest,omitempty"`
	Problem string   `json:"problem,omitempty"` // why the last lookup failed; the release shown is the last one known
}

type payload struct {
	Schema   int               `json:"schema"`
	Files    string            `json:"files"` // the digest of the build files' paths and SHAs: unchanged, the coordinates are kept
	Packages []Package         `json:"packages"`
	Kept     map[string]string `json:"kept,omitempty"` // answers that never change (released parent POMs)
}

func decode(b []byte) payload {
	var p payload
	if len(b) == 0 || json.Unmarshal(b, &p) != nil || p.Schema != payloadSchema {
		return payload{}
	}
	return p
}

// Packages is the packages feature.
type Packages struct {
	sources    map[string]GitHub
	providers  []Provider
	client     *Client
	registries []Registry
	public     bool
}

// New returns the feature for one GitHub per source id, the providers in their order, the shared client, the configured registries
// and whether the public registries are asked.
func New(sources map[string]GitHub, providers []Provider, client *Client, registries []Registry, public bool) *Packages {
	return &Packages{sources: sources, providers: providers, client: client, registries: registries, public: public}
}

// Spec is the per-repository lookup job.
func (p *Packages) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: []sync.Event{sync.EventPush, sync.EventTag}, Run: p.run,
		Publish: publish}
}

func (p *Packages) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	gh := p.sources[j.Source]
	if gh == nil {
		return sync.Result{}, errors.New("packages: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	prev := decode(j.Prev.Payload)
	tree, err := gh.Files(ctx, owner, name, searched)
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		return result(snapshot.StateNoContent, payload{Schema: payloadSchema, Packages: []Package{}}, j.Prev, false)
	case github.IsNotFound(err):
		return result(snapshot.StateNotFound, payload{Schema: payloadSchema, Packages: []Package{}}, j.Prev, false)
	case err != nil:
		return sync.Result{}, err
	}
	build := p.buildFiles(tree)
	next := payload{Schema: payloadSchema, Files: digest(build)}
	var coords []Coordinate
	if next.Files == prev.Files && prev.Files != "" {
		for _, pk := range prev.Packages {
			coords = append(coords, pk.Coordinate)
		}
	} else {
		files, err := read(ctx, gh, owner, name, build)
		if err != nil {
			return sync.Result{}, err
		}
		coords = p.coordinates(files)
	}

	if prev.Kept == nil {
		prev.Kept = map[string]string{}
	}
	l := &Lookup{client: p.client, registries: p.registries, public: p.public, kept: maps.Clone(prev.Kept), used: map[string]bool{}}
	known := map[string]*Version{}
	for _, pk := range prev.Packages {
		known[pk.Ecosystem+" "+pk.Name] = pk.Latest
	}
	next.Packages = make([]Package, len(coords))
	// Structured concurrency: every lookup of the job runs in its group and ends with it (the job's deadline cancels them all); a
	// lookup that fails costs only its package, which keeps the release last known.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(lookups)
	for i, c := range coords {
		g.Go(func() error {
			pk := Package{Coordinate: c}
			v, err := p.provider(c.Ecosystem).Latest(gctx, l, c)
			switch {
			case err != nil && ctx.Err() != nil:
				return ctx.Err()
			case err != nil:
				pk.Latest, pk.Problem = known[c.Ecosystem+" "+c.Name], problem(err)
			default:
				pk.Latest = clean(v)
			}
			next.Packages[i] = pk
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return sync.Result{}, err
	}
	failed := slices.ContainsFunc(next.Packages, func(pk Package) bool { return pk.Problem != "" })
	next.Kept = l.keptUsed()
	return result(snapshot.StateOK, next, j.Prev, failed)
}

func (p *Packages) provider(ecosystem string) Provider {
	for _, pr := range p.providers {
		if pr.Ecosystem() == ecosystem {
			return pr
		}
	}
	return none{}
}

// none answers for an ecosystem whose provider is gone since the coordinates were kept.
type none struct{}

func (none) Ecosystem() string                                             { return "" }
func (none) Wants(string) bool                                             { return false }
func (none) Coordinates([]File) []Coordinate                               { return nil }
func (none) Latest(context.Context, *Lookup, Coordinate) (*Version, error) { return nil, nil } //nolint:nilnil // no provider, no release

// searched reports whether build files are looked for in dir: at most maxDirDepth deep, not in vendored code, tests, examples,
// documents or a hidden directory.
func searched(dir string) bool {
	segs := strings.Split(dir, "/")
	return len(segs) <= maxDirDepth && !slices.ContainsFunc(segs, func(s string) bool { return skipped[s] || strings.HasPrefix(s, ".") })
}

// buildFiles are the files some provider wants, shallow first, then by path, at most maxFiles.
func (p *Packages) buildFiles(tree []github.TreeEntry) []github.TreeEntry {
	var out []github.TreeEntry
	for _, e := range tree {
		if e.Type == "blob" && e.Size <= maxFileBytes && slices.ContainsFunc(p.providers, func(pr Provider) bool { return pr.Wants(e.Path) }) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b github.TreeEntry) int {
		return cmp.Or(cmp.Compare(strings.Count(a.Path, "/"), strings.Count(b.Path, "/")), strings.Compare(a.Path, b.Path))
	})
	return out[:min(len(out), maxFiles)]
}

func digest(files []github.TreeEntry) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s %s\n", f.Path, f.SHA)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// read reads the build files, blobReads at a time.
func read(ctx context.Context, gh GitHub, owner, name string, build []github.TreeEntry) ([]File, error) {
	files := make([]File, len(build))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(blobReads)
	for i, e := range build {
		g.Go(func() error {
			b, err := gh.Blob(gctx, owner, name, e.SHA)
			if err != nil {
				return err
			}
			files[i] = File{Path: e.Path, Content: b}
			return nil
		})
	}
	return files, g.Wait()
}

// coordinates are every provider's coordinates of files, one per ecosystem and name, at most maxPackages.
func (p *Packages) coordinates(files []File) []Coordinate {
	var out []Coordinate
	seen := map[string]bool{}
	for _, pr := range p.providers {
		var mine []File
		for _, f := range files {
			if pr.Wants(f.Path) {
				mine = append(mine, f)
			}
		}
		for _, c := range pr.Coordinates(mine) {
			if key := c.Ecosystem + " " + c.Name; !seen[key] && len(out) < maxPackages {
				seen[key] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// problem says why a lookup failed in words for the pages: the registry and its status, never a credential or an address's query.
func problem(err error) string {
	var re *RegistryError
	if errors.As(err, &re) {
		return untrusted.Text(re.Error(), 200)
	}
	return "the registry's answer could not be read"
}

// clean bounds what a registry said: its texts cleaned, its address https or none, its date not in the future.
func clean(v *Version) *Version {
	if v == nil {
		return nil
	}
	out := *v
	out.Version = untrusted.Text(v.Version, 100)
	out.Registry = untrusted.Text(v.Registry, 100)
	if !strings.HasPrefix(v.URL, "https://") {
		out.URL = ""
	}
	if out.Published.After(time.Now().Add(24 * time.Hour)) {
		out.Published = time.Time{}
	}
	if out.Version == "" {
		return nil
	}
	return &out
}

func result(state snapshot.State, p payload, prev store.Item, failed bool) (sync.Result, error) {
	if p.Packages == nil {
		p.Packages = []Package{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return sync.Result{}, err
	}
	res := sync.Result{State: state, Payload: b, Unchanged: prev.State == state && bytes.Equal(prev.Payload, b)}
	if failed {
		res.Again = failedAgain
	}
	return res, nil
}

// facet is a repository's packages, those with a release first.
const facet snapshot.Facet[[]Package] = "packages"

// Of is a repository's packages and their freshness.
func Of(e *snapshot.RepoEntry) ([]Package, snapshot.Freshness) { return facet.Get(e) }

// Newest is the newest release of a repository's packages; nil when none is published.
func Newest(e *snapshot.RepoEntry) *Package {
	ps, _ := facet.Get(e)
	var out *Package
	for i, pk := range ps {
		if pk.Latest != nil && (out == nil || pk.Latest.Published.After(out.Latest.Published)) {
			out = &ps[i]
		}
	}
	return out
}

// Repo adds the published packages to a repository summary, the newest release first.
func Repo(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	ps, _ := facet.Get(e)
	out := []oapi.Package{}
	for _, pk := range ps {
		if pk.Latest == nil {
			continue
		}
		op := oapi.Package{Ecosystem: pk.Ecosystem, Name: pk.Name, Version: pk.Latest.Version, Url: pk.Latest.URL, Registry: pk.Latest.Registry}
		if !pk.Latest.Published.IsZero() {
			at := pk.Latest.Published.UTC()
			op.PublishedAt = &at
		}
		out = append(out, op)
	}
	slices.SortStableFunc(out, func(a, b oapi.Package) int {
		switch {
		case a.PublishedAt == nil && b.PublishedAt == nil:
			return 0
		case a.PublishedAt == nil:
			return 1
		case b.PublishedAt == nil:
			return -1
		}
		return b.PublishedAt.Compare(*a.PublishedAt)
	})
	r.Packages = out
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	ps := decode(it.Payload).Packages
	slices.SortStableFunc(ps, func(a, b Package) int {
		return cmp.Or(cmp.Compare(boolInt(b.Latest != nil), boolInt(a.Latest != nil)), strings.Compare(a.Path, b.Path))
	})
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, ps, fresh) })
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
