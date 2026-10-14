package overview

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/overview/overviewapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// SyncSource returns the time of the last successful full sync of the catalogue. cmd/scp passes
// `func(s) time.Time { st, _ := readmodel.GetGlobal(s, catalogSlots.Sync); return st.LastReconcile }` (design 08: the oldest
// LastSuccess over all sources, zero until every source has been listed live once and zero after a warm start). It is a
// timestamp only, so the same value serves both view classes; the catalog.sync global itself is admin class and never
// leaves overview except through this one field.
type SyncSource func(*readmodel.Snapshot) time.Time

// RegisterSlots registers the one global overview owns. cmd/scp calls it before the registry is frozen.
func RegisterSlots(reg *readmodel.Registry) readmodel.Global[scm.Classed[SiteMeta]] {
	return readmodel.RegisterGlobal[scm.Classed[SiteMeta]](reg, "site.meta")
}

// SiteProjector is the `overview.site` projector: pure and deterministic, it runs inside every Apply, in registration
// order, after the owners' own projectors. It computes the repository and group counts per view class: a group is
// counted only when it has at least one member visible to the class, so the public variant equals what a deployment
// containing only the public repositories would compute (architecture 17.3, the byte-identity property).
func SiteProjector(sl Slots, sync SyncSource) readmodel.Projector {
	if sync == nil {
		sync = func(*readmodel.Snapshot) time.Time { return time.Time{} }
	}
	return func(b *readmodel.Builder) error {
		v := b.View()
		m, _ := readmodel.GetGlobal(v, sl.Membership)
		calc := func(c scm.ViewClass) SiteMeta {
			groups := 0
			for _, g := range m.Groups() {
				if len(m.VisibleReposOf(g.ID, c)) > 0 {
					groups++
				}
			}
			return SiteMeta{RepoCount: m.Len(c), GroupCount: groups, LastFullSync: sync(v)}
		}
		pub, all := calc(scm.ViewPublic), calc(scm.ViewAll)
		if pub == all {
			return readmodel.PutGlobal(b, sl.Site, scm.Same(pub))
		}
		return readmodel.PutGlobal(b, sl.Site, scm.Classed[SiteMeta]{Public: pub, All: all})
	}
}

// announcementID is the content hash that lets a dismissal persist only until the text changes.
func announcementID(a AnnouncementView) string {
	h := sha256.Sum256([]byte(a.Level + "\x00" + a.Text))
	return hex.EncodeToString(h[:4])
}

// siteHandler is api.site. The body depends on the snapshot (counts, last sync), the class, the pages set (nav entries
// naming a missing page are dropped) and the announcement, so the weak ETag carries the version, the class and a digest
// of the page slugs and the announcement id; the build and the configuration are constant for the process lifetime.
func (s *Service) siteHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.d.Snapshots.Current()
	class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
	meta, ok := readmodel.GetGlobal(snap, s.d.Slots.Site)
	if !ok {
		httpapi.WriteProblem(w, r, httpapi.NotReady(15*time.Second))
		return
	}
	m := meta.For(class)

	pages := map[string]bool{}
	var pageKey []string
	for _, p := range s.d.Pages.Pages(snap) {
		pages[p.Slug] = true
		pageKey = append(pageKey, p.Slug)
	}
	slices.Sort(pageKey)
	ann, hasAnn := s.d.Pages.Announcement()
	annID := ""
	if hasAnn {
		annID = announcementID(ann)
	}
	etag := httpapi.WeakETag(snap.Version(), classTag(class), httpapi.Digest(append(pageKey, annID)...))
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache") // the access wrapper rewrites it to private for a ViewAll principal (unit 14)
	if httpapi.NotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	site := overviewapi.Site{
		Title: s.cfg.Site.Title, Description: s.cfg.Site.Description,
		DisplayTimeZone: s.cfg.Site.DisplayTimeZone, DefaultTheme: s.cfg.Site.DefaultTheme,
		RepoCount: m.RepoCount, GroupCount: m.GroupCount, Build: s.d.Build,
		Nav: make([]overviewapi.NavItem, 0, len(s.cfg.Site.Nav)),
		Features: overviewapi.SiteFeatures{Manifests: s.d.Manifests != nil && s.d.Manifests.Enabled(), Sse: s.d.SSEEnabled,
			Integrations: append([]string{}, s.d.Integrations.Providers()...)},
	}
	if !m.LastFullSync.IsZero() {
		t := m.LastFullSync.UTC()
		site.LastFullSync = &t
	}
	for _, n := range s.cfg.Site.Nav {
		switch {
		case n.Page != "" && pages[n.Page]:
			p := n.Page
			site.Nav = append(site.Nav, overviewapi.NavItem{Label: n.Label, Kind: "page", Page: &p})
		case n.URL != "":
			u := n.URL
			site.Nav = append(site.Nav, overviewapi.NavItem{Label: n.Label, Kind: "external", Url: &u})
		}
	}
	if hasAnn {
		site.Announcement = &overviewapi.Announcement{Id: annID, Text: ann.Text, Level: ann.Level, Dismissible: ann.Dismissible}
	}
	httpapi.WriteJSON(w, r, http.StatusOK, site)
}

func classTag(c scm.ViewClass) string {
	if c == scm.ViewAll {
		return "all"
	}
	return "public"
}
