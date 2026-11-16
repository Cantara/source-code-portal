package githubtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"
)

// Commit is a commit on the fake default branch.
type Commit struct {
	SHA, Message, AuthorName, AuthorLogin string
	Date                                  time.Time
	Parents                               []string // nil: the next commit of the list (a linear history), none for the last
	// Additions, Deletions and Files are its diff stats, answered by POST /graphql; all zero: GitHub answers null for it.
	Additions, Deletions, Files int
}

// SetCommits replaces the default-branch commits of the repository with id (newest first). No commits means an empty repository.
func (s *Server) SetCommits(id int64, cs ...Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits[id] = cs
}

// SetBranch sets a branch of the repository with id to its own commits, newest first; the last one names its parents in the
// default branch (or another branch). No commits deletes the branch.
func (s *Server) SetBranch(id int64, name string, cs ...Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.branches[id] == nil {
		s.branches[id] = map[string][]Commit{}
	}
	if len(cs) == 0 {
		delete(s.branches[id], name)
		return
	}
	s.branches[id][name] = cs
}

// SetTag points a tag of the repository with id at a commit; an empty SHA deletes it.
func (s *Server) SetTag(id int64, name, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tags[id] == nil {
		s.tags[id] = map[string]string{}
	}
	if sha == "" {
		delete(s.tags[id], name)
		return
	}
	s.tags[id][name] = sha
}

// SetFile writes a file on the default branch of the repository with id; an empty content deletes it.
func (s *Server) SetFile(id int64, path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files[id] == nil {
		s.files[id] = map[string]string{}
	}
	if content == "" {
		delete(s.files[id], path)
		return
	}
	s.files[id][path] = content
}

// SetTreeLimit makes a recursive tree of more than n entries come truncated, its first n entries only, as GitHub truncates one past
// 100,000 entries or 7 MB; 0 lifts the limit.
func (s *Server) SetTreeLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.treeMax = n
}

// Release is a release as GitHub lists it (SetReleases).
type Release struct {
	Tag, Name, Body, Author string
	Published               time.Time
	Pre, Draft              bool
}

// SetRelease sets the one release of the repository with id; an empty tag removes it.
func (s *Server) SetRelease(id int64, tag, name string, published time.Time) {
	if tag == "" {
		s.SetReleases(id)
		return
	}
	s.SetReleases(id, Release{Tag: tag, Name: name, Published: published})
}

// SetReleases sets the releases of the repository with id, replacing its earlier ones; GitHub lists them newest first.
func (s *Server) SetReleases(id int64, rels ...Release) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]*gh.RepositoryRelease, 0, len(rels))
	for _, r := range rels {
		x := &gh.RepositoryRelease{TagName: r.Tag, Name: new(r.Name), Body: new(r.Body), Prerelease: r.Pre, Draft: r.Draft,
			PublishedAt: &gh.Timestamp{Time: r.Published}, HTMLURL: fmt.Sprintf("https://github.com/releases/%d/%s", id, r.Tag),
			Assets: []*gh.ReleaseAsset{{Name: new("asset")}}}
		if r.Author != "" {
			x.Author = &gh.User{Login: new(r.Author)}
		}
		list = append(list, x)
	}
	slices.SortStableFunc(list, func(x, y *gh.RepositoryRelease) int { return y.GetPublishedAt().Compare(x.GetPublishedAt().Time) })
	if len(list) == 0 {
		delete(s.releases, id)
		return
	}
	s.releases[id] = list
}

func (s *Server) listReleases(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var rels []*gh.RepositoryRelease
	if r != nil {
		rels = s.releases[r.GetID()]
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	if n, err := strconv.Atoi(req.URL.Query().Get("per_page")); err == nil && n > 0 && n < len(rels) {
		rels = rels[:n]
	}
	s.respond(w, req, cred, append([]*gh.RepositoryRelease{}, rels...))
}

// lookup returns the repository at owner/name. Callers hold s.mu.
// ID is the id of the repository owner/name, 0 when there is none.
func (s *Server) ID(owner, name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(owner, name).GetID()
}

func (s *Server) lookup(owner, name string) *gh.Repository {
	for _, r := range s.orgs[strings.ToLower(owner)] {
		if strings.EqualFold(r.GetName(), name) {
			return r
		}
	}
	return nil
}

func (s *Server) listCommits(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	ref := req.URL.Query().Get("sha")
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var cs []Commit
	if r != nil {
		cs = s.history(r, ref)
	}
	s.mu.Unlock()
	switch {
	case r == nil:
		writeError(w, http.StatusNotFound, "Not Found")
		return
	case len(cs) == 0 && ref == "":
		writeError(w, http.StatusConflict, "Git Repository is empty.")
		return
	case len(cs) == 0:
		writeError(w, http.StatusNotFound, "No commit found for SHA: "+ref)
		return
	}
	lo, hi := s.page(w, req, len(cs))
	out := make([]*gh.RepositoryCommit, 0, hi-lo)
	for _, c := range cs[lo:hi] {
		date := &gh.Timestamp{Time: c.Date}
		rc := &gh.RepositoryCommit{SHA: new(c.SHA), HTMLURL: new(r.GetHTMLURL() + "/commit/" + c.SHA),
			Commit: &gh.Commit{Message: new(c.Message), Author: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date},
				Committer: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date}}}
		for _, p := range c.Parents {
			rc.Parents = append(rc.Parents, &gh.Commit{SHA: new(p)})
		}
		if c.AuthorLogin != "" {
			rc.Author = &gh.User{Login: new(c.AuthorLogin)}
		}
		out = append(out, rc)
	}
	s.respond(w, req, cred, out)
}

// page reads per_page (default 30, at most 100) and page, sets the Link header to the next page and returns the bounds of n items.
func (s *Server) page(w http.ResponseWriter, req *http.Request, n int) (lo, hi int) {
	per, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if per <= 0 || per > 100 {
		per = 30
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	page = max(page, 1)
	lo, hi = min((page-1)*per, n), min(page*per, n)
	if hi < n {
		next := *req.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, s.URL, next.RequestURI()))
	}
	return lo, hi
}

// history is the commits reachable from ref, newest first, each with its parents filled in. The default branch ("" or its name)
// lists in the order SetCommits gave; another branch, a tag or a SHA walks the parents and orders by date. Callers hold s.mu.
func (s *Server) history(r *gh.Repository, ref string) []Commit {
	id := r.GetID()
	all := map[string]Commit{}
	line := func(cs []Commit) []Commit {
		out := slices.Clone(cs)
		for i := range out {
			if out[i].Parents == nil && i+1 < len(out) {
				out[i].Parents = []string{out[i+1].SHA}
			}
			all[out[i].SHA] = out[i]
		}
		return out
	}
	main := line(s.commits[id])
	heads := map[string]string{}
	for name, cs := range s.branches[id] {
		heads[name] = line(cs)[0].SHA
	}
	if ref == "" || ref == r.GetDefaultBranch() {
		return main
	}
	head, ok := heads[ref]
	if !ok {
		head, ok = s.tags[id][ref]
	}
	if !ok {
		head = ref
	}
	var out []Commit
	seen := map[string]bool{}
	for stack := []string{head}; len(stack) > 0; {
		sha := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c, known := all[sha]
		if !known || seen[sha] {
			continue
		}
		seen[sha] = true
		out = append(out, c)
		stack = append(stack, c.Parents...)
	}
	slices.SortStableFunc(out, func(a, b Commit) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return strings.Compare(b.SHA, a.SHA)
	})
	return out
}

// listRefs answers GET /repos/{owner}/{name}/branches (the default branch first, then by name) and .../tags (by name, descending).
func (s *Server) listRefs(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	type ref struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	var refs []ref
	add := func(name, sha string) {
		var x ref
		x.Name, x.Commit.SHA = name, sha
		refs = append(refs, x)
	}
	if r != nil {
		id := r.GetID()
		if strings.HasSuffix(req.URL.Path, "/branches") {
			if cs := s.commits[id]; len(cs) > 0 {
				add(r.GetDefaultBranch(), cs[0].SHA)
			}
			for _, name := range slices.Sorted(maps.Keys(s.branches[id])) {
				add(name, s.branches[id][name][0].SHA)
			}
		} else {
			for _, name := range slices.Backward(slices.Sorted(maps.Keys(s.tags[id]))) {
				add(name, s.tags[id][name])
			}
		}
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	lo, hi := s.page(w, req, len(refs))
	s.respond(w, req, cred, refs[lo:hi])
}

// tree builds the git tree at dir ("" is the root) from the repository's files. Callers hold s.mu.
func (s *Server) tree(id int64, dir string) *gh.Tree {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	seen := map[string]bool{}
	var entries []*gh.TreeEntry
	for _, p := range slices.Sorted(maps.Keys(s.files[id])) {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		if sub, _, isDir := strings.Cut(rest, "/"); isDir {
			if !seen[sub] {
				seen[sub] = true
				t := s.tree(id, prefix+sub)
				entries = append(entries, &gh.TreeEntry{Path: new(sub), Type: new("tree"), Mode: new("040000"), SHA: t.SHA})
			}
			continue
		}
		content := s.files[id][p]
		entries = append(entries, &gh.TreeEntry{Path: new(rest), Type: new("blob"), Mode: new("100644"), SHA: new(blobSHA(content)),
			Size: new(len(content))})
	}
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s %s\n", e.GetPath(), e.GetSHA())
	}
	return &gh.Tree{SHA: new(hex.EncodeToString(h.Sum(nil))[:40]), Entries: entries}
}

// flatten replaces the tree entries of t with everything below them, paths relative to t. Callers hold s.mu.
func (s *Server) flatten(id int64, dir string, t *gh.Tree) *gh.Tree {
	var entries []*gh.TreeEntry
	for _, e := range t.Entries {
		entries = append(entries, e)
		if e.GetType() == "tree" {
			sub := e.GetPath()
			if dir != "" {
				sub = dir + "/" + sub
			}
			for _, c := range s.flatten(id, sub, s.tree(id, sub)).Entries {
				entries = append(entries, &gh.TreeEntry{Path: new(e.GetPath() + "/" + c.GetPath()), Type: c.Type, Mode: c.Mode, SHA: c.SHA, Size: c.Size})
			}
		}
	}
	return &gh.Tree{SHA: t.SHA, Entries: entries}
}

func blobSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:40]
}

func (s *Server) getTree(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var found *gh.Tree
	foundDir := ""
	empty := true
	if r != nil {
		id := r.GetID()
		empty = len(s.commits[id]) == 0
		ref := req.PathValue("ref")
		if root := s.tree(id, ""); ref == "HEAD" || ref == r.GetDefaultBranch() || ref == root.GetSHA() {
			found = root
		} else {
			for p := range s.files[id] {
				for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
					if t := s.tree(id, dir); t.GetSHA() == ref {
						found, foundDir = t, dir
					}
				}
			}
		}
		if found != nil && req.URL.Query().Get("recursive") != "" {
			found = s.flatten(id, foundDir, found)
			if s.treeMax > 0 && len(found.Entries) > s.treeMax {
				found.Entries, found.Truncated = found.Entries[:s.treeMax], new(true)
			}
		}
	}
	s.mu.Unlock()
	switch {
	case r == nil:
		writeError(w, http.StatusNotFound, "Not Found")
	case empty:
		writeError(w, http.StatusConflict, "Git Repository is empty.")
	case found == nil:
		writeError(w, http.StatusNotFound, "Not Found")
	default:
		s.respond(w, req, cred, found)
	}
}

func (s *Server) getBlob(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	content, ok := "", false
	if r != nil {
		for _, c := range s.files[r.GetID()] {
			if blobSHA(c) == req.PathValue("sha") {
				content, ok = c, true
			}
		}
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	if strings.Contains(req.Header.Get("Accept"), "raw") {
		w.Header().Set("Content-Type", "application/vnd.github.raw")
		_, _ = w.Write([]byte(content))
		return
	}
	s.respond(w, req, cred, map[string]any{"sha": req.PathValue("sha"), "size": len(content), "encoding": "base64",
		"content": base64.StdEncoding.EncodeToString([]byte(content))})
}
