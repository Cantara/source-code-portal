package githubtest

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Issue is an open issue of the fake.
type Issue struct {
	Number           int
	Title, Author    string
	Bot              bool // the author's account type is Bot
	Created, Updated time.Time
	Labels           []string
	Assignees        []string
	Comments         int
	Milestone, Type  string
}

// SetIssues replaces the open issues of the repository id.
func (s *Server) SetIssues(id int64, is ...Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues[id] = slices.Clone(is)
}

// listIssues is GitHub's listing of open issues: the issues and the open pull requests together, the most recently updated first,
// in pages; a pull request carries `pull_request`.
func (s *Server) listIssues(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var items []map[string]any
	var updated []time.Time
	if r != nil {
		full := r.GetOwner().GetLogin() + "/" + r.GetName()
		user := func(login string, bot bool) map[string]string {
			if bot {
				return map[string]string{"login": login, "type": "Bot"}
			}
			return map[string]string{"login": login, "type": "User"}
		}
		for _, p := range s.pulls[r.GetID()] {
			items = append(items, map[string]any{"number": p.Number, "title": p.Title, "user": user(p.Author, p.Bot),
				"html_url": fmt.Sprintf("https://github.com/%s/pull/%d", full, p.Number), "labels": []any{}, "assignees": []any{},
				"created_at": p.Created.UTC().Format(time.RFC3339), "updated_at": p.Updated.UTC().Format(time.RFC3339),
				"pull_request": map[string]string{"url": "https://api.github.com/repos/" + full + "/pulls/" + strconv.Itoa(p.Number)}})
			updated = append(updated, p.Updated)
		}
		for _, is := range s.issues[r.GetID()] {
			labels := []map[string]string{}
			for _, l := range is.Labels {
				labels = append(labels, map[string]string{"name": l, "color": "d73a4a"})
			}
			assignees := []map[string]string{}
			for _, a := range is.Assignees {
				assignees = append(assignees, map[string]string{"login": a})
			}
			it := map[string]any{"number": is.Number, "title": is.Title, "user": user(is.Author, is.Bot),
				"html_url": fmt.Sprintf("https://github.com/%s/issues/%d", full, is.Number), "labels": labels, "assignees": assignees,
				"comments": is.Comments, "created_at": is.Created.UTC().Format(time.RFC3339), "updated_at": is.Updated.UTC().Format(time.RFC3339),
				"body": "not read", "milestone": nil, "type": nil}
			if is.Milestone != "" {
				it["milestone"] = map[string]any{"title": is.Milestone, "number": 1}
			}
			if is.Type != "" {
				it["type"] = map[string]any{"name": is.Type, "id": 1}
			}
			items = append(items, it)
			updated = append(updated, is.Updated)
		}
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return updated[b].Compare(updated[a]) })
	lo, hi := s.page(w, req, len(items))
	out := make([]map[string]any, 0, hi-lo)
	for _, i := range order[lo:hi] {
		out = append(out, items[i])
	}
	s.respond(w, req, cred, out)
}
