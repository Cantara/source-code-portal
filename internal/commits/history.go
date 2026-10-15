package commits

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const defaultHistoryLimit = 100

var atPattern = regexp.MustCompile(`^[0-9a-f]{4,40}$`)

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
	number, limit, problem := pageParams(p.Page, p.Limit, defaultHistoryLimit)
	if problem != "" {
		return bad("validation-failed", problem)
	}
	ref, author, q := deref(p.Ref), strings.ToLower(strings.TrimSpace(deref(p.Author))), strings.ToLower(strings.TrimSpace(deref(p.Q)))
	at := strings.ToLower(deref(p.At))
	switch {
	case len(q) > 200:
		return bad("validation-failed", "q is at most 200 characters")
	case p.At != nil && !atPattern.MatchString(at):
		return bad("validation-failed", "at is a SHA or a prefix of 4 to 40 hex digits")
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
	filtered := author != "" || q != "" || !automation
	keep := func(c *Commit) bool {
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
	list, rows := h.Order, h.Rows
	if reach != nil || filtered {
		list = slices.DeleteFunc(slices.Clone(h.Order), func(c *Commit) bool { return !keep(c) })
		rows = nil
		if !filtered { // what a branch or tag reaches is a history of its own: lay it out
			rows = layout(list, h.held)
		}
	}
	out := oapi.HistoryPage{Commits: []oapi.HistoryCommit{}, Labels: make([]oapi.Label, 0, len(h.Labels)), Filtered: filtered,
		Freshness: oapi.FreshnessOf(fresh, time.Now())}
	if at != "" {
		if i := slices.IndexFunc(list, func(c *Commit) bool { return strings.HasPrefix(c.SHA, at) }); i >= 0 {
			number, out.At = i/limit+1, &list[i].SHA
		}
	}
	start, end, pg := paging(len(list), number, limit)
	out.Page = pg
	for _, l := range h.Labels {
		out.Labels = append(out.Labels, labelOf(l))
	}
	key := e.Repo.Key()
	for i, c := range list[start:end] {
		hc := h.commitOf(key, c)
		if rows != nil {
			hc.Graph = rowOf(rows[start+i])
		}
		out.Commits = append(out.Commits, hc)
	}
	if rows != nil {
		out.Lanes = width(rows[start:end])
	}
	return oapi.GetHistory200JSONResponse{Body: out}, nil
}

func rowOf(r Row) *oapi.GraphRow {
	edges := func(es []Edge) []oapi.GraphEdge {
		out := make([]oapi.GraphEdge, len(es))
		for i, e := range es {
			out[i] = oapi.GraphEdge{From: e.From, To: e.To, Color: e.Color, Open: e.Open}
		}
		return out
	}
	return &oapi.GraphRow{Lane: r.Lane, Color: r.Color, Merge: r.Merge, Above: edges(r.Above), Below: edges(r.Below)}
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
		Automated: Automated(c), Labels: []oapi.Label{}, Beyond: []string{}}
	for _, p := range c.Parents {
		if !h.held(p) {
			out.Beyond = append(out.Beyond, p)
		}
	}
	for _, l := range h.LabelsAt(c.SHA) {
		out.Labels = append(out.Labels, labelOf(l))
	}
	return out
}

func labelOf(l Label) oapi.Label {
	return oapi.Label{Name: l.Name, Kind: oapi.LabelKind(l.Kind), Sha: l.SHA, Default: l.Default}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
