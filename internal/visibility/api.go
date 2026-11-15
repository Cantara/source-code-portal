package visibility

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

// AdminPath is the address of the admins' choices; PreviewPath answers for a draft without saving it.
const (
	AdminPath   = "/api/v1/admin/visibility"
	PreviewPath = AdminPath + "/preview"
)

// Item is a repository as the admins' list draws it: its own choice (nil: it takes its audience from elsewhere) and the result.
type Item struct {
	Address string   `json:"address"`
	Private bool     `json:"private"` // private or internal on GitHub: never shown to everyone
	Fork    bool     `json:"fork"`
	Setting *Setting `json:"setting"`
	Effective
}

// GroupNode is a product with its own choice, its result and its repositories, live and retired.
type GroupNode struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Setting *Setting `json:"setting"`
	Effective
	Repos []Item `json:"repos"`
}

// Counts are how many catalogued repositories each audience has, and how many of those everyone sees are kept from search engines.
type Counts struct {
	Everyone  int `json:"everyone"`
	SignedIn  int `json:"signedIn"`
	NoOne     int `json:"noOne"`
	SearchOff int `json:"searchOff"`
	Private   int `json:"private"`
}

// adminBody is what the admins' list needs: the document, every catalogued repository by product (the ones no one sees too: they
// are in the base snapshot), the addresses the document names that the catalogue does not hold (renamed, deleted, not listed yet),
// how many repositories each rule matches, the counts and the revision. Configured is the configuration's `visibility:`, so the list
// can tell what is changed here (design/config-baseline.md).
type adminBody struct {
	Doc
	Configured  Doc         `json:"configured"`
	Tree        []GroupNode `json:"tree"`
	Ungrouped   []Item      `json:"ungrouped"`
	Missing     []string    `json:"missing"`
	RuleMatches []int       `json:"ruleMatches"`
	Counts      Counts      `json:"counts"`
	Revision    string      `json:"revision"`
	Writable    bool        `json:"writable"`
	Problem     string      `json:"problem"`
}

type saveBody struct {
	Doc
	Revision string `json:"revision"`
}

// Routes are the admins' read, preview and save, behind the admin role (which sees private repositories); a preview and a save come
// from the portal's own pages. base is the published snapshot itself, which holds the repositories no one sees.
func (s *Set) Routes(publicURL string, base func() *snapshot.Snapshot, log *slog.Logger) []httpapi.Route {
	h := admin{s: s, publicURL: publicURL, base: base, log: log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPath, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
		{Pattern: "POST " + PreviewPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.preview))},
	}
}

type admin struct {
	s         *Set
	publicURL string
	base      func() *snapshot.Snapshot
	log       *slog.Logger
}

// compose is the list's body for the document d over the base snapshot s.
func compose(d Doc, s *snapshot.Snapshot) adminBody {
	body := adminBody{Doc: d, Tree: make([]GroupNode, 0, len(s.Groups)), Ungrouped: []Item{}, Missing: []string{},
		RuleMatches: make([]int, len(d.Rules))}
	chosen := func(m map[string]Setting, k string) *Setting {
		if st, ok := m[k]; ok {
			return &st
		}
		return nil
	}
	item := func(k snapshot.RepoKey) Item {
		e := s.Repos[k]
		return Item{Address: string(k), Private: !e.Repo.Public(), Fork: e.Repo.Fork, Setting: chosen(d.Repos, string(k)),
			Effective: d.Repo(k, s.GroupsOf(k), e.Repo.Public())}
	}
	for _, g := range s.Groups {
		keys := slices.Concat(g.Members, g.Retired)
		slices.Sort(keys)
		node := GroupNode{ID: g.ID, Name: g.Name, Setting: chosen(d.Groups, g.ID), Effective: d.Group(g.ID), Repos: make([]Item, 0, len(keys))}
		for _, k := range slices.Compact(keys) {
			if s.Repos[k] != nil {
				node.Repos = append(node.Repos, item(k))
			}
		}
		body.Tree = append(body.Tree, node)
	}
	for k, e := range s.Repos {
		if len(s.GroupsOf(k)) == 0 {
			body.Ungrouped = append(body.Ungrouped, item(k))
		}
		for i, r := range d.Rules {
			if r.re != nil && r.re.MatchString(string(k)) {
				body.RuleMatches[i]++
			}
		}
		if !e.Repo.Public() {
			body.Counts.Private++
		}
		switch eff := d.Repo(k, s.GroupsOf(k), e.Repo.Public()); eff.Audience {
		case Everyone:
			body.Counts.Everyone++
			if !eff.Search {
				body.Counts.SearchOff++
			}
		case SignedIn:
			body.Counts.SignedIn++
		default:
			body.Counts.NoOne++
		}
	}
	slices.SortFunc(body.Ungrouped, func(a, b Item) int { return cmp.Compare(a.Address, b.Address) })
	for _, addr := range sorted(d.Repos) {
		if s.Repos[snapshot.RepoKey(addr)] == nil {
			body.Missing = append(body.Missing, addr)
		}
	}
	return body
}

func (h admin) answer(w http.ResponseWriter, d Doc, revision, problem string) {
	body := compose(d, h.base())
	body.Configured = h.s.base
	body.Revision, body.Writable, body.Problem = revision, h.s.file.Writable(), problem
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(body) //nolint:errcheck,gosec // the client went away
}

func (h admin) get(w http.ResponseWriter, _ *http.Request) {
	st := h.s.file.Get()
	h.answer(w, h.s.Doc(), st.Revision, st.Problem)
}

// read checks a preview's or a save's request and returns its body, cleaned; false when it answered with a problem.
func (h admin) read(w http.ResponseWriter, r *http.Request) (saveBody, bool) {
	if !httpapi.RequireJSON(w, r, "the choices") {
		return saveBody{}, false
	}
	if !httpapi.SameOrigin(r, h.publicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change who sees what from the portal's own pages.")
		return saveBody{}, false
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a visibility document.") {
		return saveBody{}, false
	}
	d, err := clean(body.Doc)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return saveBody{}, false
	}
	body.Doc = d
	return body, true
}

// preview answers as if the draft were saved, and saves nothing: what the list shows while an admin chooses.
func (h admin) preview(w http.ResponseWriter, r *http.Request) {
	body, ok := h.read(w, r)
	if !ok {
		return
	}
	st := h.s.file.Get()
	h.answer(w, body.Doc, st.Revision, st.Problem)
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	body, ok := h.read(w, r)
	if !ok {
		return
	}
	if !h.s.file.Writable() {
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save the choices in.")
		return
	}
	// What is stored is the difference from the configuration: a choice equal to the file's is not kept.
	b, err := encodeOverrides(Diff(h.s.base, body.Doc))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The choices could not be written.")
		return
	}
	switch _, err := h.s.file.Save(context.WithoutCancel(r.Context()), b, body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved who sees what since you opened it; reload to see theirs.")
	case err != nil:
		h.log.Error("visibility not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The choices could not be written.")
	default:
		h.s.refresh()
		h.s.changed()
		h.get(w, r)
	}
}
