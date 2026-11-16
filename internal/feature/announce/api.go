package announce

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/docstore"
	"github.com/cantara/source-code-portal/internal/platform/httpapi"
)

// AdminPath is the address of the admin's announcements.
const AdminPath = "/api/v1/admin/announcements"

// adminBody is what the admin's card needs: the announcements in force with the stored document's revision, the configuration's
// (so the card can tell what is changed here), whether the store can save, and why the stored document is not served.
type adminBody struct {
	Announcements []config.Announcement `json:"announcements"`
	Configured    []config.Announcement `json:"configured"`
	Revision      string                `json:"revision"`
	Writable      bool                  `json:"writable"`
	Problem       string                `json:"problem"`
}

type saveBody struct {
	Announcements []config.Announcement `json:"announcements"`
	Revision      string                `json:"revision"`
}

// Routes are the admin's read and save of the stored announcements, behind the admin role; a save comes from the portal's own pages.
func (n *Notices) Routes(publicURL string, log *slog.Logger) []httpapi.Route {
	h := admin{n: n, publicURL: publicURL, log: log}
	return []httpapi.Route{
		{Pattern: "GET " + AdminPath, Handler: httpapi.AdminOnly(http.HandlerFunc(h.get))},
		{Pattern: "PUT " + AdminPath, MaxBody: maxBytes, Handler: httpapi.AdminOnly(http.HandlerFunc(h.put))},
	}
}

type admin struct {
	n         *Notices
	publicURL string
	log       *slog.Logger
}

func (h admin) get(w http.ResponseWriter, _ *http.Request) {
	st := h.n.overlay.State()
	list := h.n.List()
	configured := h.n.overlay.Base()
	if configured == nil {
		configured = []config.Announcement{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(adminBody{Announcements: list, Configured: configured, Revision: st.Revision, //nolint:errcheck,gosec // the client went away
		Writable: h.n.overlay.Writable(), Problem: st.Problem})
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	if !httpapi.RequireJSON(w, r, "the announcements") {
		return
	}
	switch {
	case !httpapi.SameOrigin(r, h.publicURL):
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the announcements from the portal's own pages.")
		return
	case !h.n.overlay.Writable():
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save announcements in.")
		return
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a list of announcements.") {
		return
	}
	list, err := h.n.check(body.Announcements)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return
	}
	switch _, err := h.n.overlay.Save(r.Context(), list, body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved the announcements since you opened them; reload to see theirs.")
	case err != nil:
		h.log.Error("announcements not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The announcements could not be written.")
	default:
		h.get(w, r)
	}
}

// check cleans and checks the list in force an admin saves, as the store will read what is kept of it.
func (n *Notices) check(list []config.Announcement) ([]config.Announcement, error) {
	if list == nil {
		list = []config.Announcement{}
	}
	if p := config.CheckAnnouncements("announcements", list); len(p) > 0 {
		return nil, errors.New(p[0])
	}
	b, err := docstore.Encode(diff(n.overlay.Base(), list))
	if err != nil {
		return nil, err
	}
	if _, err := Parse(b); err != nil {
		return nil, err
	}
	return list, nil
}
