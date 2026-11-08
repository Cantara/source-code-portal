package announce

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/httpapi"
)

// AdminPath is the address of the admin's announcements.
const AdminPath = "/api/v1/admin/announcements"

// adminBody is what the admin's card needs: the stored announcements with their revision, the configured ones (locked), whether
// the store can save, and why the stored file is not served.
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
	st := h.n.file.Get()
	configured := h.n.configured
	if configured == nil {
		configured = []config.Announcement{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(adminBody{Announcements: st.Value.Announcements, Configured: configured, Revision: st.Revision, //nolint:errcheck,gosec // the client went away
		Writable: h.n.file.Writable(), Problem: st.Problem})
}

func (h admin) put(w http.ResponseWriter, r *http.Request) {
	if !httpapi.RequireJSON(w, r, "the announcements") {
		return
	}
	switch {
	case !httpapi.SameOrigin(r, h.publicURL):
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Change the announcements from the portal's own pages.")
		return
	case !h.n.file.Writable():
		httpapi.WriteProblem(w, http.StatusConflict, "read-only", "This portal has no store to save announcements in.")
		return
	}
	var body saveBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a list of announcements.") {
		return
	}
	b, err := h.n.check(body.Announcements)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, "validation-failed", err.Error())
		return
	}
	switch _, err := h.n.file.Save(r.Context(), b, body.Revision); {
	case errors.Is(err, docstore.ErrConflict):
		httpapi.WriteProblem(w, http.StatusPreconditionFailed, "conflict", "Someone saved the announcements since you opened them; reload to see theirs.")
	case err != nil:
		h.log.Error("announcements not written", "err", err)
		httpapi.WriteProblem(w, http.StatusInternalServerError, "internal", "The announcements could not be written.")
	default:
		h.get(w, r)
	}
}

// check cleans and checks list as the store will read it, refuses an id a configured announcement has, and returns the file.
func (n *Notices) check(list []config.Announcement) ([]byte, error) {
	if list == nil {
		list = []config.Announcement{}
	}
	if p := config.CheckAnnouncements("announcements", list); len(p) > 0 {
		return nil, errors.New(p[0])
	}
	for i, a := range list {
		if slices.ContainsFunc(n.configured, func(c config.Announcement) bool { return c.ID == a.ID }) {
			return nil, fmt.Errorf("announcements[%d].id: %q is a configured announcement's", i, a.ID)
		}
		if a.Published == nil {
			list[i].Published = new(true) // stored explicitly: an admin's draft is a choice, never a default
		}
	}
	b, err := json.MarshalIndent(List{Announcements: list}, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if _, err := Parse(b); err != nil {
		return nil, err
	}
	return b, nil
}
