package githubtest

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Pull is an open pull request of the fake: what the REST listing gives and what POST /graphql answers for it.
type Pull struct {
	Number                      int
	Title, Author               string
	Bot, Draft, Fork            bool // Bot: the author's account type is Bot
	Created, Updated            time.Time
	Base, Head, HeadSHA         string
	Labels                      []string
	Requested                   int      // people asked to review, named reviewer0, reviewer1, ... after Reviewers
	Reviewers                   []string // people asked to review, by login
	Review, Mergeable, Checks   string   // GraphQL's reviewDecision, mergeable and the head's statusCheckRollup state
	Approved, Changes           bool     // a reviewer's latest opinion
	Additions, Deletions, Files int
}

// SetPulls replaces the open pull requests of the repository id.
func (s *Server) SetPulls(id int64, ps ...Pull) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pulls[id] = slices.Clone(ps)
}

func (s *Server) listPulls(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var ps []Pull
	if r != nil {
		ps = slices.Clone(s.pulls[r.GetID()])
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	slices.SortStableFunc(ps, func(a, b Pull) int { return b.Updated.Compare(a.Updated) })
	n, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if n <= 0 || n > 100 {
		n = 30
	}
	ps = ps[:min(n, len(ps))]
	full := r.GetOwner().GetLogin() + "/" + r.GetName()
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		typ := "User"
		if p.Bot {
			typ = "Bot"
		}
		head := full
		if p.Fork {
			head = "someone/" + r.GetName()
		}
		labels := []map[string]string{}
		for _, l := range p.Labels {
			labels = append(labels, map[string]string{"name": l, "color": "0e8a16"})
		}
		reviewers := make([]map[string]string, 0, max(p.Requested, len(p.Reviewers)))
		for _, l := range p.Reviewers {
			reviewers = append(reviewers, map[string]string{"login": l})
		}
		for i := 0; len(reviewers) < p.Requested; i++ {
			reviewers = append(reviewers, map[string]string{"login": fmt.Sprintf("reviewer%d", i)})
		}
		out = append(out, map[string]any{"number": p.Number, "title": p.Title, "draft": p.Draft,
			"html_url":   fmt.Sprintf("https://github.com/%s/pull/%d", full, p.Number),
			"user":       map[string]string{"login": p.Author, "type": typ},
			"created_at": p.Created.UTC().Format(time.RFC3339), "updated_at": p.Updated.UTC().Format(time.RFC3339),
			"base":   map[string]any{"ref": p.Base, "repo": map[string]string{"full_name": full}},
			"head":   map[string]any{"ref": p.Head, "sha": p.HeadSHA, "repo": map[string]string{"full_name": head}},
			"labels": labels, "requested_reviewers": reviewers, "requested_teams": []any{}, "body": "not read"})
	}
	s.respond(w, req, cred, out)
}
