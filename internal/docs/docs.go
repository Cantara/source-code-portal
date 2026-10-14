// Package docs keeps every catalogued repository's README and the Markdown files under docs/, rendered to SafeHTML with a table of
// contents, and serves them.
package docs

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// Kind is the job kind of the per-repository refresh.
const Kind = "docs"

const (
	ttl           = time.Hour // pushes notify the job
	root          = "docs"    // the documentation directory
	maxDocs       = 50        // files under docs/, in path order
	maxFileBytes  = 1 << 20   // larger files are not fetched
	payloadSchema = 1
)

var readmeName = regexp.MustCompile(`(?i)^readme(?:\.[a-z0-9]+)?$`)

// GitHub reads trees and blobs of one source; *github.Source implements it.
type GitHub interface {
	Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]github.TreeEntry, error)
	Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
	WebURL() string
}

// Docs is the docs feature.
type Docs struct {
	sources map[string]GitHub
	pipe    *pipeline
}

// New returns the feature for one GitHub per source id.
func New(sources map[string]GitHub, policy *safehtml.Policy) *Docs {
	return &Docs{sources: sources, pipe: &pipeline{policy: policy}}
}

// record is one stored document. The HTML is untrusted bytes until it passes the sanitiser again at publish.
type record struct {
	Path        string             `json:"path"`
	README      bool               `json:"readme,omitempty"`
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	Order       int                `json:"order"`
	Hidden      bool               `json:"hidden,omitempty"`
	Format      string             `json:"format"`
	HTML        string             `json:"html"`
	Source      []byte             `json:"source,omitempty"`
	TOC         []snapshot.Heading `json:"toc,omitempty"`
	BlobSHA     string             `json:"blobSha"`
	Truncated   bool               `json:"truncated,omitempty"`
	State       snapshot.State     `json:"state"`
}

type payload struct {
	Schema   int      `json:"schema"`
	Pipeline int      `json:"pipeline"` // pipelineVersion and safehtml.PolicyVersion the HTML was made with
	Policy   int      `json:"policy"`
	Index    string   `json:"index"` // digest of the indexed paths and web host the links were resolved against
	Docs     []record `json:"docs"`
}

func decode(b []byte) payload {
	var p payload
	if len(b) == 0 || json.Unmarshal(b, &p) != nil || p.Schema != payloadSchema {
		return payload{}
	}
	return p
}

// Spec is the per-repository refresh job.
func (d *Docs) Spec() sync.Spec {
	return sync.Spec{Kind: Kind, Prio: sync.PrioScheduled, TTL: ttl, On: []sync.Event{sync.EventPush}, Run: d.run, Publish: d.publish}
}

// file is one document to keep.
type file struct {
	path   string
	entry  github.TreeEntry
	readme bool
}

func (d *Docs) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	gh := d.sources[j.Source]
	if gh == nil {
		return sync.Result{}, errors.New("docs: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	files, err := list(ctx, gh, owner, name)
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		return result(snapshot.StateNoContent, payload{}, j.Prev)
	case github.IsNotFound(err):
		return result(snapshot.StateNotFound, payload{}, j.Prev)
	case err != nil:
		return sync.Result{}, err
	}

	l := links{web: gh.WebURL(), repo: j.Repo, indexed: map[string]bool{}}
	digest := sha256.New()
	digest.Write([]byte(l.web + "\n"))
	for _, f := range files {
		l.indexed[f.path] = true
		digest.Write([]byte(f.path + "\n"))
	}
	next := payload{Schema: payloadSchema, Pipeline: pipelineVersion, Policy: safehtml.PolicyVersion, Index: hex.EncodeToString(digest.Sum(nil))}
	prev := decode(j.Prev.Payload)
	current := prev.Pipeline == next.Pipeline && prev.Policy == next.Policy && prev.Index == next.Index
	prevByPath := map[string]record{}
	for _, r := range prev.Docs {
		prevByPath[r.Path] = r
	}
	for _, f := range files {
		old, seen := prevByPath[f.path]
		seen = seen && old.BlobSHA == f.entry.SHA
		var r record
		switch {
		case f.entry.Size > maxFileBytes:
			r = record{Path: f.path, Title: path.Base(f.path), Format: "text", Order: noOrder, State: snapshot.StateTooLarge}
		case seen && (current || old.State != snapshot.StateOK):
			r = old
		case seen: // rendered by another pipeline, policy or index: render the stored source again
			r = d.record(f, old.Source, l)
			r.Truncated = r.Truncated || old.Truncated
		default:
			b, err := gh.Blob(ctx, owner, name, f.entry.SHA)
			if err != nil {
				return sync.Result{}, err
			}
			r = d.record(f, b, l)
		}
		r.README, r.BlobSHA = f.readme, f.entry.SHA
		next.Docs = append(next.Docs, r)
	}
	slices.SortStableFunc(next.Docs, func(a, b record) int {
		switch {
		case a.README != b.README:
			if a.README {
				return -1
			}
			return 1
		case a.Order != b.Order:
			return cmp.Compare(a.Order, b.Order)
		}
		return strings.Compare(a.Path, b.Path)
	})
	state := snapshot.StateOK
	if len(next.Docs) == 0 {
		state = snapshot.StateNotFound
	}
	return result(state, next, j.Prev)
}

func (d *Docs) record(f file, b []byte, l links) record {
	l.dir = path.Dir(f.path)
	if l.dir == "." {
		l.dir = ""
	}
	out := d.pipe.render(f.path, b, l)
	return record{Path: f.path, Title: out.Title, Description: out.Description, Order: out.Order, Hidden: out.Hidden, Format: out.Format,
		HTML: out.HTML.String(), Source: out.Source, TOC: out.TOC, Truncated: out.Truncated, State: out.State}
}

func result(state snapshot.State, p payload, prev store.Item) (sync.Result, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return sync.Result{}, err
	}
	sum := sha256.New()
	for _, r := range p.Docs {
		sum.Write([]byte(r.Path + " " + r.BlobSHA + "\n"))
	}
	sha := hex.EncodeToString(sum.Sum(nil))
	return sync.Result{State: state, ContentSHA: sha, Payload: b,
		Unchanged: prev.State == state && prev.ContentSHA == sha && bytes.Equal(prev.Payload, b)}, nil
}

// list finds the README (.github/, then the root, then docs/, as GitHub resolves it) and the Markdown files under docs/.
func list(ctx context.Context, gh GitHub, owner, name string) ([]file, error) {
	top, err := gh.Tree(ctx, owner, name, "HEAD", false)
	if err != nil {
		return nil, err
	}
	var dotGitHub, docsDir []github.TreeEntry
	for _, e := range top {
		if e.Type != "tree" || e.Path != ".github" && e.Path != root {
			continue
		}
		sub, err := gh.Tree(ctx, owner, name, e.SHA, e.Path == root)
		if err != nil {
			return nil, err
		}
		if e.Path == root {
			docsDir = sub
		} else {
			dotGitHub = sub
		}
	}
	var out []file
	for _, loc := range []struct {
		dir     string
		entries []github.TreeEntry
	}{{".github/", dotGitHub}, {"", top}, {root + "/", docsDir}} {
		if r, ok := readme(loc.entries); ok {
			out = append(out, file{path: loc.dir + r.Path, entry: r, readme: true})
			break
		}
	}
	var docs []file
	for _, e := range docsDir {
		ext := strings.ToLower(path.Ext(e.Path))
		if p := root + "/" + e.Path; e.Type == "blob" && (ext == ".md" || ext == ".markdown") && (len(out) == 0 || out[0].path != p) {
			docs = append(docs, file{path: p, entry: e})
		}
	}
	slices.SortFunc(docs, func(a, b file) int { return strings.Compare(a.path, b.path) })
	return append(out, docs[:min(len(docs), maxDocs)]...), nil
}

// readme picks the README among the direct entries of one tree: README.md first, then by name.
func readme(entries []github.TreeEntry) (github.TreeEntry, bool) {
	var found []github.TreeEntry
	for _, e := range entries {
		if e.Type == "blob" && !strings.Contains(e.Path, "/") && readmeName.MatchString(e.Path) {
			found = append(found, e)
		}
	}
	if len(found) == 0 {
		return github.TreeEntry{}, false
	}
	slices.SortFunc(found, func(a, b github.TreeEntry) int {
		if am, bm := strings.EqualFold(a.Path, "README.md"), strings.EqualFold(b.Path, "README.md"); am != bm {
			if am {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return found[0], true
}

// publish sanitises the stored HTML again (the store is untrusted) and sets the repository's documents.
func (d *Docs) publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	p := decode(it.Payload)
	docs := make([]snapshot.Doc, 0, len(p.Docs))
	for _, r := range p.Docs {
		h, err := d.pipe.policy.Sanitize([]byte(r.HTML))
		if err != nil {
			h = safehtml.SafeHTML{}
		}
		docs = append(docs, snapshot.Doc{Path: r.Path, README: r.README, Title: r.Title, Description: r.Description, Order: r.Order,
			Hidden: r.Hidden, Format: r.Format, HTML: h, Source: r.Source, TOC: r.TOC, BlobSHA: r.BlobSHA, Truncated: r.Truncated, State: r.State})
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { e.Docs, e.DocsFresh = docs, fresh })
}
