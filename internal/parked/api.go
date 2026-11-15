package parked

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

// AdminPath is the address of the admins' list.
const AdminPath = "/api/v1/admin/parked"

// RepoItem is a repository the admins' card draws.
type RepoItem struct {
	Address string `json:"address"`
	Private bool   `json:"private"`
	Fork    bool   `json:"fork"`
	Parked  bool   `json:"parked"`
}

// GroupNode is a product with its repositories, live and retired.
type GroupNode struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Repos []RepoItem `json:"repos"`
}

// adminBody is what the card needs: the stored list, every catalogued repository by product (the parked ones too: they are in the
// base snapshot, not in any view), the parked addresses the catalogue does not hold (renamed, deleted, not listed yet) and the revision.
type adminBody struct {
	List
	Tree      []GroupNode `json:"tree"`
	Ungrouped []RepoItem  `json:"ungrouped"`
	Missing   []string    `json:"missing"`
	Revision  string      `json:"revision"`
	Writable  bool        `json:"writable"`
	Problem   string      `json:"problem"`
}

type saveBody struct {
	List
	Revision string `json:"revision"`
}

// Routes are the admins' read and save, behind the admin role (which sees private repositories); a save comes from the portal's own
// pages. base is the published snapshot itself, which holds the parked repositories.
func (s *Set) Routes(publicURL string, base func() *snapshot.Snapshot, log *slog.Logger) []httpapi.Route {
	h := admin{s: s, publicURL: publicURL, base: base, log: log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPath, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
	}
}

type admin struct {
	s         *Set
	publicURL string
	base      func() *snapshot.Snapshot
	log       *slog.Logger
}

// compose is the card's body for the list l over the base snapshot s.
func compose(l List, s *snapshot.Snapshot) adminBody {
	if l.Repos == nil {
		l.Repos = []string{}
	}
	item := func(k snapshot.RepoKey) RepoItem {
		e := s.Repos[k]
		_, parked := slices.BinarySearch(l.Repos, string(k))
		return RepoItem{Address: string(k), Private: !e.Repo.Public(), Fork: e.Repo.Fork, Parked: parked}
	}
	tree := make([]GroupNode, 0, len(s.Groups))
	for _, g := range s.Groups {
		keys := slices.Concat(g.Members, g.Retired)
		slices.Sort(keys)
		node := GroupNode{ID: g.ID, Name: g.Name, Repos: make([]RepoItem, 0, len(keys))}
		for _, k := range slices.Compact(keys) {
			if s.Repos[k] != nil {
				node.Repos = append(node.Repos, item(k))
			}
		}
		tree = append(tree, node)
	}
	ungrouped := []RepoItem{}
	for k := range s.Repos {
		if len(s.GroupsOf(k)) == 0 {
			ungrouped = append(ungrouped, item(k))
		}
	}
	slices.SortFunc(ungrouped, func(a, b RepoItem) int { return cmp.Compare(a.Address, b.Address) })
	missing := []string{}
	for _, r := range l.Repos {
		if s.Repos[snapshot.RepoKey(r)] == nil {
			missing = append(missing, r)
		}
	}
	return adminBody{List: l, Tree: tree, Ungrouped: ungrouped, Missing: missing}
}

func (h admin) answer(w http.ResponseWriter, l List, revision, problem string) {
	body := compose(l, h.base())
	body.Revision, body.Writable, body.Problem = revision, h.s.file.Writable(), problem
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(body) //nolint:errcheck,gosec // the client went away
}

func (h admin) get(w http.ResponseWriter, _ *http.Request) {
	st := h.s.file.Get()
	h.answer(w, st.Value, st.Revision, st.Problem)
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	if !httpapi.RequireJSON(w, r, "the list") {
		return
	}
	if !httpapi.SameOrigin(r, h.publicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the list from the portal's own pages.")
		return
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a list of repositories.") {
		return
	}
	l, err := clean(body.List)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return
	}
	if !h.s.file.Writable() {
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save the list in.")
		return
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
		return
	}
	switch _, err := h.s.file.Save(context.WithoutCancel(r.Context()), append(b, '\n'), body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved the list since you opened it; reload to see theirs.")
	case err != nil:
		h.log.Error("parked list not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The list could not be written.")
	default:
		h.s.changed()
		h.get(w, r)
	}
}
