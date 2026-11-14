package indexing

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// AdminPath is the address of the admin's list.
const AdminPath = "/api/v1/admin/indexing"

// GroupRow and RepoRow are the choices the admin's card offers: Excluded is what is stored, Via (repositories) that the repository
// is kept out only because all its groups are.
type GroupRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Repos    int    `json:"repos"`
	Excluded bool   `json:"excluded"`
}

type RepoRow struct {
	Address  string   `json:"address"`
	Groups   []string `json:"groups"`
	Excluded bool     `json:"excluded"`
	Via      bool     `json:"via"`
}

type adminBody struct {
	Excluded
	GroupRows []GroupRow `json:"groupRows"`
	RepoRows  []RepoRow  `json:"repoRows"`
	Revision  string     `json:"revision"`
	Writable  bool       `json:"writable"`
	Problem   string     `json:"problem"`
}

type saveBody struct {
	Excluded
	Revision string `json:"revision"`
}

// Routes are the admin's read and save, behind the admin role; a save comes from the portal's own pages. view is the full snapshot the
// choices are drawn from.
func (s *Set) Routes(publicURL string, view func(context.Context) *snapshot.Snapshot, log *slog.Logger) []httpapi.Route {
	h := admin{s: s, publicURL: publicURL, view: view, log: log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPath, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
	}
}

type admin struct {
	s         *Set
	publicURL string
	view      func(context.Context) *snapshot.Snapshot
	log       *slog.Logger
}

func (h admin) get(w http.ResponseWriter, r *http.Request) {
	st := h.s.file.Get()
	v := h.view(r.Context())
	groups := make([]GroupRow, 0, len(v.Groups))
	for _, g := range v.Groups {
		groups = append(groups, GroupRow{ID: g.ID, Name: g.Name, Repos: len(g.Members), Excluded: slices.Contains(st.Value.Groups, g.ID)})
	}
	repos := make([]RepoRow, 0, len(v.Repos))
	for k := range v.Repos {
		own := slices.Contains(st.Value.Repos, string(k))
		ids := slices.Clone(v.GroupsOf(k))
		if ids == nil {
			ids = []string{}
		}
		repos = append(repos, RepoRow{Address: string(k), Groups: ids, Excluded: own, Via: !own && h.s.Repo(v, k)})
	}
	slices.SortFunc(repos, func(a, b RepoRow) int { return cmp.Compare(a.Address, b.Address) })
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(adminBody{Excluded: st.Value, GroupRows: groups, RepoRows: repos, Revision: st.Revision, //nolint:errcheck,gosec // the client went away
		Writable: h.s.file.Writable(), Problem: st.Problem})
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	if !httpapi.RequireJSON(w, r, "the list") {
		return
	}
	switch {
	case !httpapi.SameOrigin(r, h.publicURL):
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the list from the portal's own pages.")
		return
	case !h.s.file.Writable():
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save the list in.")
		return
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a list of groups and repositories.") {
		return
	}
	clean, err := clean(body.Excluded)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return
	}
	b, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
		return
	}
	switch _, err := h.s.file.Save(r.Context(), append(b, '\n'), body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved the list since you opened it; reload to see theirs.")
	case err != nil:
		h.log.Error("indexing list not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
	default:
		h.get(w, r)
	}
}
