package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"

	"github.com/cantara/source-code-portal/internal/jsonstream"
)

const (
	maxWalk = 100  // tree calls a walk of a truncated tree makes at most
	maxPath = 4096 // bytes of an entry's path kept; Git allows no longer one on most hosts
)

// Files returns the blobs of the default branch in the directories keep admits: the root always, another directory when keep
// admits its path (keep sees every directory's whole path, so it rules on its parents too). It reads the recursive tree as a
// stream and keeps only those entries, so a repository of any size costs the memory of what is kept. A tree GitHub truncated (past
// 100,000 entries or 7 MB) is read again directory by directory, shallow ones first, at most maxWalk calls: what the walk did not
// reach is left out, never a failure.
func (s *Source) Files(ctx context.Context, owner, name string, keep func(dir string) bool) ([]TreeEntry, error) {
	wanted := func(e TreeEntry) bool { return e.Type == "blob" && (path.Dir(e.Path) == "." || keep(path.Dir(e.Path))) }
	entries, truncated, err := s.readTree(ctx, owner, name, "HEAD", true, wanted)
	if err != nil || !truncated {
		return entries, err
	}
	s.log.Info("tree truncated: walking it", "repo", owner+"/"+name)
	return s.walk(ctx, owner, name, keep)
}

// walk reads the tree level by level from the root, entering the directories keep admits.
func (s *Source) walk(ctx context.Context, owner, name string, keep func(dir string) bool) ([]TreeEntry, error) {
	type dir struct{ ref, path string }
	var out []TreeEntry
	queue := []dir{{"HEAD", ""}}
	for calls := 0; len(queue) > 0 && calls < maxWalk; calls++ {
		d := queue[0]
		queue = queue[1:]
		entries, _, err := s.readTree(ctx, owner, name, d.ref, false, func(TreeEntry) bool { return true })
		if err != nil {
			if d.ref == "HEAD" {
				return nil, err
			}
			var e *Error
			if errors.As(err, &e) && (e.Kind == RateLimited || e.Kind == Auth) {
				return nil, err
			}
			continue // a directory that fails is left out
		}
		for _, e := range entries {
			e.Path = path.Join(d.path, e.Path)
			switch {
			case e.Type == "blob":
				out = append(out, e)
			case e.Type == "tree" && keep(e.Path):
				queue = append(queue, dir{e.SHA, e.Path})
			}
		}
	}
	return out, nil
}

// readTree reads one tree, keeping the entries want admits, and reports whether GitHub truncated it.
func (s *Source) readTree(ctx context.Context, owner, name, ref string, recursive bool, want func(TreeEntry) bool) ([]TreeEntry, bool, error) {
	c, err := s.client(ctx, owner)
	if err != nil {
		return nil, false, err
	}
	u := "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/git/trees/" + url.PathEscape(ref)
	if recursive {
		u += "?recursive=1"
	}
	req, err := c.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.BareDo(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, false, classify(resp, err)
	}
	entries, truncated, err := decodeTree(resp.Body, want)
	switch {
	case errors.Is(err, jsonstream.ErrSyntax):
		return nil, false, classify(resp, err) // a 200 that is not JSON: malformed
	case err != nil:
		return nil, false, &Error{Kind: Transient, Code: "transient", Err: err} // the connection broke mid-answer
	}
	return entries, truncated, nil
}

// decodeTree reads GitHub's tree answer: per entry its path, type, SHA and size, and the truncated flag; the rest streams by.
func decodeTree(body io.Reader, want func(TreeEntry) bool) ([]TreeEntry, bool, error) {
	d := jsonstream.New(body)
	var out []TreeEntry
	truncated := false
	err := d.Object(func(key string) (err error) {
		switch key {
		case "truncated":
			truncated, err = d.Bool()
			return err
		case "tree":
			return d.Array(func() error {
				var e TreeEntry
				err := d.Object(func(key string) (err error) {
					switch key {
					case "path":
						e.Path, err = d.String(maxPath)
					case "type":
						e.Type, err = d.String(16)
					case "sha":
						e.SHA, err = d.String(64)
					case "size":
						e.Size, err = d.Int()
					default:
						err = d.Skip()
					}
					return err
				})
				if err == nil && e.Path != "" && want(e) {
					out = append(out, e)
				}
				return err
			})
		}
		return d.Skip()
	})
	return out, truncated, err
}
