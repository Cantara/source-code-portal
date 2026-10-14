package docs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// repo is a fake repository: files by path, blob SHA = the content itself, prefixed.
type repo struct {
	files map[string]string
	err   error
	blobs int
}

func (r *repo) Tree(_ context.Context, _, _, ref string, recursive bool) ([]github.TreeEntry, error) {
	if r.err != nil {
		return nil, r.err
	}
	dir := ""
	if ref != "HEAD" {
		dir = strings.TrimPrefix(ref, "tree:")
	}
	seen := map[string]bool{}
	var out []github.TreeEntry
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for p := range r.files {
			if !yield(p) {
				return
			}
		}
	}) {
		rest, ok := strings.CutPrefix(p, dir)
		if !ok {
			continue
		}
		if sub, _, isDir := strings.Cut(rest, "/"); isDir && !recursive {
			if !seen[sub] {
				seen[sub] = true
				out = append(out, github.TreeEntry{Path: sub, Type: "tree", SHA: "tree:" + dir + sub + "/"})
			}
			continue
		}
		out = append(out, github.TreeEntry{Path: rest, Type: "blob", SHA: "blob:" + r.files[p], Size: int64(len(r.files[p]))})
	}
	return out, nil
}

func (r *repo) Blob(_ context.Context, _, _, sha string) ([]byte, error) {
	r.blobs++
	return []byte(strings.TrimPrefix(sha, "blob:")), nil
}

func (r *repo) WebURL() string { return "https://github.com/" }

func run(t *testing.T, d *Docs, prev store.Item) (sync.Result, payload) {
	t.Helper()
	res, err := d.Spec().Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x", Prev: prev})
	if err != nil {
		t.Fatal(err)
	}
	return res, decode(res.Payload)
}

func paths(p payload) string {
	var out []string
	for _, r := range p.Docs {
		out = append(out, r.Path)
	}
	return strings.Join(out, ",")
}

func TestJob(t *testing.T) {
	r := &repo{files: map[string]string{
		"README.md":            "# X\n\nSee [setup](docs/setup.md).\n",
		"readme.txt":           "older",
		".github/CONTRIBUTING": "c",
		"docs/setup.md":        "---\norder: 1\n---\n# Setup\n",
		"docs/a/deep.markdown": "# Deep\n",
		"docs/b.md":            "---\nhidden: true\n---\n# B\n",
		"docs/image.png":       "png",
		"src/main.go":          "package main",
	}}
	d := New(map[string]GitHub{"gh": r}, safehtml.New())
	res, p := run(t, d, store.Item{})
	if res.State != snapshot.StateOK || paths(p) != "README.md,docs/setup.md,docs/a/deep.markdown,docs/b.md" || r.blobs != 4 {
		t.Fatalf("first run: %s, %s, %d blobs", res.State, paths(p), r.blobs)
	}
	if !strings.Contains(p.Docs[0].HTML, `href="/repos/acme/x/docs/docs/setup"`) || !p.Docs[0].README || !p.Docs[3].Hidden {
		t.Errorf("docs = %+v", p.Docs)
	}

	prev := store.Item{State: res.State, ContentSHA: res.ContentSHA, Payload: res.Payload}
	if again, _ := run(t, d, prev); !again.Unchanged || r.blobs != 4 {
		t.Errorf("unchanged repository: Unchanged %v, %d blobs", again.Unchanged, r.blobs)
	}

	r.files["docs/new.md"] = "# New\n" // the index changes: everything renders again from the stored source, one fetch
	res, p = run(t, d, prev)
	if res.Unchanged || r.blobs != 5 || len(p.Docs) != 5 {
		t.Errorf("new document: %d blobs, %s", r.blobs, paths(p))
	}

	r.files[".github/README.md"] = "# From .github\n"
	if _, p = run(t, d, prev); p.Docs[0].Path != ".github/README.md" {
		t.Errorf(".github/ README does not win: %s", paths(p))
	}

	r.files["docs/huge.md"] = strings.Repeat("x", maxFileBytes+1)
	blobs := r.blobs
	_, p = run(t, d, prev)
	i := slices.IndexFunc(p.Docs, func(r record) bool { return path.Base(r.Path) == "huge.md" })
	if i < 0 || p.Docs[i].State != snapshot.StateTooLarge || r.blobs-blobs > 6 {
		t.Errorf("huge: %+v", p.Docs)
	}

	for name, tc := range map[string]struct {
		err   error
		state snapshot.State
	}{
		"empty":     {&github.Error{Kind: github.NotFound, Code: "empty", Err: errors.New("409")}, snapshot.StateNoContent},
		"not found": {&github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("404")}, snapshot.StateNotFound},
	} {
		r := &repo{err: tc.err}
		if res, _ := run(t, New(map[string]GitHub{"gh": r}, safehtml.New()), store.Item{}); res.State != tc.state {
			t.Errorf("%s: %s", name, res.State)
		}
	}
	if res, _ := run(t, New(map[string]GitHub{"gh": &repo{files: map[string]string{"x.go": "x"}}}, safehtml.New()), store.Item{}); res.State != snapshot.StateNotFound {
		t.Errorf("no documents: %s", res.State)
	}
}

func TestPublishSanitisesStoredHTML(t *testing.T) {
	d := New(nil, safehtml.New())
	b, _ := json.Marshal(payload{Schema: payloadSchema, Docs: []record{{Path: "README.md", HTML: `<p>x<script>alert(1)</script></p>`, State: snapshot.StateOK}}})
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	p.Update(func(bd *snapshot.Builder) {
		bd.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}}},
			nil, snapshot.SourceStatus{ID: "gh"})
		d.publish(bd, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: b}, snapshot.Freshness{})
	})
	p.Flush()
	if got := p.Current().Repos["acme/x"].Docs[0].HTML.String(); got != "<p>x</p>" {
		t.Errorf("published HTML = %s", got)
	}
}
