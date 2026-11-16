package githubtest

import (
	"cmp"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// graphAlias is one `alias:object(oid:"sha")` of a GraphQL query, as the commits feature asks diff stats.
var graphAlias = regexp.MustCompile(`(\w+):object\(oid:"([0-9a-f]{40,64})"\)`)

// graphQL answers the one GraphQL query the portal sends: a repository's commits by object id, each with its diff stats.
func (s *Server) graphQL(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	var body struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Problems parsing JSON")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if q := strings.TrimSpace(body.Query); strings.HasPrefix(q, "mutation") {
		s.t.Errorf("githubtest: the portal sent a GraphQL mutation (P-71): %.80s", q)
		writeError(w, http.StatusForbidden, "the portal must not write to GitHub")
		return
	}
	r := s.lookup(body.Variables["owner"], body.Variables["name"])
	if r == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": nil},
			"errors": []map[string]string{{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}}})
		return
	}
	all := map[string]Commit{}
	for _, c := range s.commits[r.GetID()] {
		all[c.SHA] = c
	}
	for _, cs := range s.branches[r.GetID()] {
		for _, c := range cs {
			all[c.SHA] = c
		}
	}
	repo := map[string]any{}
	s.answerPulls(r.GetID(), body.Query, repo)
	for _, m := range graphAlias.FindAllStringSubmatch(body.Query, -1) {
		c, ok := all[m[2]]
		if !ok || c.Additions+c.Deletions+c.Files == 0 {
			repo[m[1]] = nil
			continue
		}
		repo[m[1]] = map[string]int{"additions": c.Additions, "deletions": c.Deletions, "changedFilesIfAvailable": c.Files}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": repo}})
}

var (
	pullAlias  = regexp.MustCompile(`(\w+):pullRequest\(number:(\d+)\)`)
	totalAlias = regexp.MustCompile(`(\w+):pullRequests\(states:OPEN[^)]*\)\{totalCount\}`)
)

// answerPulls answers a query's pull requests by number and its open count. Callers hold s.mu.
func (s *Server) answerPulls(id int64, query string, repo map[string]any) {
	for _, m := range totalAlias.FindAllStringSubmatch(query, -1) {
		repo[m[1]] = map[string]int{"totalCount": len(s.pulls[id])}
	}
	for _, m := range pullAlias.FindAllStringSubmatch(query, -1) {
		n, _ := strconv.Atoi(m[2])
		repo[m[1]] = nil
		for _, p := range s.pulls[id] {
			if p.Number != n {
				continue
			}
			var opinions []map[string]string
			if p.Approved {
				opinions = append(opinions, map[string]string{"state": "APPROVED"})
			}
			if p.Changes {
				opinions = append(opinions, map[string]string{"state": "CHANGES_REQUESTED"})
			}
			var rollup any
			if p.Checks != "" {
				rollup = map[string]string{"state": p.Checks}
			}
			var review any
			if p.Review != "" {
				review = p.Review
			}
			repo[m[1]] = map[string]any{"reviewDecision": review, "mergeable": cmp.Or(p.Mergeable, "MERGEABLE"),
				"additions": p.Additions, "deletions": p.Deletions, "changedFiles": p.Files,
				"latestOpinionatedReviews": map[string]any{"nodes": opinions},
				"commits":                  map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}}}}}
		}
	}
}
