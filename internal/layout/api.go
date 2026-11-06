package layout

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves a page's layout resolved against the viewer's view. It implements the layout's method of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
	Store   *Store
	// Notices are the announcements the viewer may see on a page now (design/announcements.md); nil: none.
	Notices func(ctx context.Context, page string) []oapi.Announcement
	// Off is the element kinds of the features switched off (design/features.md): left out where a layout holds them.
	Off []oapi.LayoutElementKind
}

func (a API) GetPage(ctx context.Context, req oapi.GetPageRequestObject) (oapi.GetPageResponseObject, error) {
	page := string(req.Page)
	if !Known(page) {
		return oapi.GetPage404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
			oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", "no such page"))}, nil
	}
	p := a.Store.Get(page)
	notices := []oapi.Announcement{}
	if a.Notices != nil {
		notices = a.Notices(ctx, page)
	}
	return oapi.GetPage200JSONResponse{Body: oapi.PageLayout{Page: page, Elements: Resolve(p.Layout, a.Current(ctx), a.Off...), Stored: p.Stored,
		Announcements: notices}}, nil
}

// Resolve is l for a viewer of s: the groups the view has no repository of are left out, an element whose groups all are, and the
// elements of the kinds off.
func Resolve(l Layout, s *snapshot.Snapshot, off ...oapi.LayoutElementKind) []oapi.LayoutElement {
	out := make([]oapi.LayoutElement, 0, len(l.Elements))
	for _, e := range l.Elements {
		if slices.Contains(off, e.Kind) {
			continue
		}
		if e.Groups != nil {
			var keep []string
			for _, id := range *e.Groups {
				if g, ok := s.Group(id); ok && len(g.Members) > 0 {
					keep = append(keep, id)
				}
			}
			if len(keep) == 0 {
				continue
			}
			e.Groups = &keep
		}
		out = append(out, e)
	}
	return out
}

// AdminPattern is the address of the admin's layout of a page.
const AdminPattern = "/api/v1/admin/layouts/{page}"

// adminBody is the admin's layout of a page with what the design mode needs: its revision, whether it can be saved, and why the
// stored file is not served.
type adminBody struct {
	Page     string               `json:"page"`
	Elements []oapi.LayoutElement `json:"elements"`
	Revision string               `json:"revision"`
	Stored   bool                 `json:"stored"`
	Writable bool                 `json:"writable"`
	Problem  string               `json:"problem"`
}

type saveBody struct {
	Elements []oapi.LayoutElement `json:"elements"`
	Revision string               `json:"revision"`
}

// Routes are the admin's reads and writes of the stored layouts, behind the admin role; a write comes from the portal's own pages.
func (s *Store) Routes(publicURL string) []httpapi.Route {
	h := admin{store: s, publicURL: publicURL, log: s.log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPattern, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPattern, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
		{Pattern: "DELETE " + AdminPattern, Handler: httpapi.AdminOnly(http.HandlerFunc(h.reset))},
	}
}

type admin struct {
	store     *Store
	publicURL string
	log       *slog.Logger
}

func (h admin) page(w http.ResponseWriter, r *http.Request) (string, bool) {
	page := r.PathValue("page")
	if !Known(page) {
		httpapi.WriteProblem(w, http.StatusNotFound, "not-found", "no such page")
		return "", false
	}
	return page, true
}

func (h admin) get(w http.ResponseWriter, r *http.Request) {
	page, ok := h.page(w, r)
	if !ok {
		return
	}
	h.write(w, page)
}

func (h admin) write(w http.ResponseWriter, page string) {
	p := h.store.Get(page)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(adminBody{Page: page, Elements: p.Layout.Elements, Revision: p.Revision, Stored: p.Stored, //nolint:errcheck,gosec // the client went away
		Writable: h.store.Writable(), Problem: p.Problem})
}

// writable refuses a write that is cross-origin or that the store cannot take.
func (h admin) writable(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case !httpapi.SameOrigin(r, h.publicURL):
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the layout from the portal's own pages.")
	case !h.store.Writable():
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "Set layout.dir to save layouts.")
	default:
		return true
	}
	return false
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	page, ok := h.page(w, r)
	if !ok {
		return
	}
	var body saveBody
	if !h.writable(w, r) || !httpapi.JSONBody(w, r, &body, "the layout", "The body is not a layout.") {
		return
	}
	h.finish(w, page, func() error {
		_, err := h.store.Save(page, Layout{Elements: body.Elements}, body.Revision)
		return err
	})
}

func (h admin) reset(w http.ResponseWriter, r *http.Request) {
	page, ok := h.page(w, r)
	if !ok || !h.writable(w, r) {
		return
	}
	h.finish(w, page, func() error { return h.store.Reset(page, r.URL.Query().Get("revision")) })
}

// finish runs a write and answers the stored layout, or why the write was refused.
func (h admin) finish(w http.ResponseWriter, page string, write func() error) {
	switch err := write(); {
	case errors.Is(err, ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved this layout since you opened it; reload to see theirs.")
	case errors.Is(err, ErrReadOnly):
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "Set layout.dir to save layouts.")
	case errors.As(err, new(Invalid)):
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
	case err != nil:
		h.log.Error("layout not written", "page", page, "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The layout could not be written.")
	default:
		h.write(w, page)
	}
}
