package commits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const defaultHistoryLimit = 100

// historyCursor is the commit after which the next page starts, bound to the filter by its digest.
type historyCursor struct {
	F string `json:"f"`
	S string `json:"s"`
}

func historyProblem(status int, code, detail string) oapi.Problem {
	return oapi.NewProblem(status, code, http.StatusText(status), detail)
}

// GetHistory serves a page of a repository's history in graph order.
func (a API) GetHistory(ctx context.Context, req oapi.GetHistoryRequestObject) (oapi.GetHistoryResponseObject, error) {
	s, p := a.Current(ctx), req.Params
	e, moved := s.Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.GetHistory404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
			historyProblem(http.StatusNotFound, "not-found", "no such repository"))}, nil
	case moved:
		return oapi.GetHistory308Response{Headers: oapi.MovedResponseHeaders{Location: "/api/v1/repos/" + string(e.Repo.Key()) + "/history"}}, nil
	}
	bad := func(code, detail string) (oapi.GetHistoryResponseObject, error) {
		return oapi.GetHistory400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: oapi.BadRequestApplicationProblemPlusJSONResponse(
			historyProblem(http.StatusBadRequest, code, detail))}, nil
	}
	limit := defaultHistoryLimit
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxLimit {
		return bad("validation-failed", "limit must be 1 to 200")
	}
	ref, author, q := deref(p.Ref), strings.ToLower(strings.TrimSpace(deref(p.Author))), strings.ToLower(strings.TrimSpace(deref(p.Q)))
	if len(q) > 200 {
		return bad("validation-failed", "q is at most 200 characters")
	}
	automation := p.Automation == nil || *p.Automation
	h, fresh := HistoryOf(e)
	if h == nil {
		h = newHistory(nil, nil, nil, "")
	}
	reach, ok := h.reachable(ref)
	if !ok {
		return bad("unknown-ref", "no branch or tag "+ref+" in the stored history")
	}
	filter := digest(ref, author, q, automation)
	match := func(c *Commit) bool {
		switch {
		case reach != nil && !reach[c.SHA]:
			return false
		case !automation && Automated(c):
			return false
		case author != "" && strings.ToLower(c.AuthorLogin) != author && strings.ToLower(c.AuthorName) != author:
			return false
		}
		return q == "" || strings.HasPrefix(c.SHA, q) || strings.Contains(strings.ToLower(c.Subject), q) ||
			strings.Contains(strings.ToLower(c.Body), q)
	}
	start := 0
	if p.Cursor != nil {
		var c historyCursor
		at, known := -1, false
		if decodeAny(*p.Cursor, &c) && c.F == filter {
			at, known = h.Position(c.S)
		}
		if !known {
			return bad("invalid-cursor", "the cursor is not valid for this filter")
		}
		start = at + 1
	}
	key := e.Repo.Key()
	out := oapi.HistoryPage{Commits: []oapi.HistoryCommit{}, Labels: make([]oapi.Label, 0, len(h.Labels)),
		Filtered: author != "" || q != "" || !automation, Page: oapi.Page{Limit: limit}, Freshness: oapi.FreshnessOf(fresh, time.Now())}
	for _, l := range h.Labels {
		out.Labels = append(out.Labels, labelOf(l))
	}
	var last *Commit
	for i, c := range h.Order {
		if !match(c) {
			continue
		}
		out.Total++
		if i < start {
			continue
		}
		if len(out.Commits) == limit {
			if out.Page.NextCursor == nil {
				out.Page.NextCursor = new(encodeAny(historyCursor{F: filter, S: last.SHA}))
			}
			continue
		}
		last = c
		out.Commits = append(out.Commits, h.commitOf(key, c))
	}
	return oapi.GetHistory200JSONResponse{Body: out}, nil
}

// reachable is the set of commits reachable from ref through the stored history; nil for no ref, false when ref is not a label.
func (h *History) reachable(ref string) (map[string]bool, bool) {
	if ref == "" {
		return nil, true
	}
	var head string
	for _, l := range h.Labels {
		if l.Name == ref {
			head = l.SHA
			break
		}
	}
	if head == "" {
		return nil, false
	}
	by := make(map[string]*Commit, len(h.Order))
	for _, c := range h.Order {
		by[c.SHA] = c
	}
	seen := map[string]bool{}
	for stack := []string{head}; len(stack) > 0; {
		sha := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c, ok := by[sha]
		if !ok || seen[sha] {
			continue
		}
		seen[sha] = true
		stack = append(stack, c.Parents...)
	}
	return seen, true
}

func (h *History) commitOf(key snapshot.RepoKey, c *Commit) oapi.HistoryCommit {
	out := oapi.HistoryCommit{Repo: string(key), Sha: c.SHA, Subject: c.Subject, Body: c.Body, AuthorName: c.AuthorName,
		AuthorLogin: c.AuthorLogin, CommitterName: c.CommitterName, Url: c.URL, CommittedAt: c.CommittedAt.UTC(), Parents: c.Parents,
		Automated: Automated(c), Labels: []oapi.Label{}}
	for _, l := range h.LabelsAt(c.SHA) {
		out.Labels = append(out.Labels, labelOf(l))
	}
	return out
}

func labelOf(l Label) oapi.Label {
	return oapi.Label{Name: l.Name, Kind: oapi.LabelKind(l.Kind), Sha: l.SHA, Default: l.Default}
}

// digest binds a cursor to its filter.
func digest(parts ...any) string {
	sum := sha256.New()
	for _, p := range parts {
		_, _ = sum.Write([]byte(strings.ReplaceAll(toString(p), "\x00", "")))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
