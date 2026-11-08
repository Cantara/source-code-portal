// Package announce keeps the portal's announcements (design/announcements.md): the deployment's from `announcements[]` in the
// configuration, locked, and the admins' in the store (the document `announcements`). For gives the ones a viewer may see on a page now;
// the admin's routes read and save the stored ones.
package announce

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/docstore"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

const (
	maxBytes = 64 << 10 // the file or a request body
	maxShown = 3        // on a page at once
)

// List is the stored file's content.
type List struct {
	Announcements []config.Announcement `json:"announcements"`
}

// Parse decodes and checks the stored announcements: at most 64 KiB of one JSON object, unknown fields refused.
func Parse(b []byte) (List, error) {
	if len(b) > maxBytes {
		return List{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var l List
	if err := dec.Decode(&l); err != nil {
		return List{}, fmt.Errorf("not a list of announcements: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return List{}, errors.New("not a list of announcements: more than one JSON value")
	}
	if l.Announcements == nil {
		l.Announcements = []config.Announcement{}
	}
	if p := config.CheckAnnouncements("announcements", l.Announcements); len(p) > 0 {
		return List{}, errors.New(p[0])
	}
	return l, nil
}

// Notices are the configured and the stored announcements.
type Notices struct {
	configured []config.Announcement // checked by the configuration
	file       *docstore.Doc[List]
	now        func() time.Time
	qr         *qrCache
}

// Name is the stored announcements' document.
const Name = "announcements"

// Open reads the stored announcements from db (nil: the configured ones only, and no saving). now is the clock (time.Now).
func Open(ctx context.Context, configured []config.Announcement, db docstore.Backend, now func() time.Time, log *slog.Logger) *Notices {
	return &Notices{
		configured: configured,
		file:       docstore.Open(ctx, db, Name, List{Announcements: []config.Announcement{}}, Parse, log.With("store", "announcements")),
		now:        now,
		qr:         &qrCache{codes: map[string]*oapi.QRCode{}},
	}
}

// Problems is why the stored document is not served, for the admin status.
func (n *Notices) Problems() []string {
	if p := n.file.Get().Problem; p != "" {
		return []string{"announcements: " + p + "; serving the last good announcements"}
	}
	return nil
}

// all is every announcement: the configured ones, then the stored ones whose id no configured one has.
func (n *Notices) all() []config.Announcement {
	out := slices.Clone(n.configured)
	for _, a := range n.file.Get().Value.Announcements {
		if !slices.ContainsFunc(n.configured, func(c config.Announcement) bool { return c.ID == a.ID }) {
			out = append(out, a)
		}
	}
	return out
}

// For is what the viewer of ctx may see on page now: published, within its dates, for the page and the viewer's audience; the
// most pressing tone first, then the soonest, at most three.
func (n *Notices) For(ctx context.Context, page string) []oapi.Announcement {
	now, private := n.now(), httpapi.Private(ctx)
	var shown []config.Announcement
	for _, a := range n.all() {
		if a.Shows(now, page, private) {
			shown = append(shown, a)
		}
	}
	slices.SortFunc(shown, func(a, b config.Announcement) int {
		return cmp.Or(
			cmp.Compare(slices.Index(config.AnnouncementTones, a.Tone), slices.Index(config.AnnouncementTones, b.Tone)),
			soonest(a, b),
			cmp.Compare(a.ID, b.ID))
	})
	out := make([]oapi.Announcement, 0, min(len(shown), maxShown))
	for _, a := range shown[:min(len(shown), maxShown)] {
		out = append(out, n.served(a))
	}
	return out
}

// soonest orders by start, the ones without a start after the ones with.
func soonest(a, b config.Announcement) int {
	switch {
	case a.StartsAt.IsZero() && b.StartsAt.IsZero():
		return 0
	case a.StartsAt.IsZero():
		return 1
	case b.StartsAt.IsZero():
		return -1
	}
	return a.StartsAt.Compare(b.StartsAt)
}

// served is a's answer: its fields as the pages draw them, its revision, and its link's QR code.
func (n *Notices) served(a config.Announcement) oapi.Announcement {
	b, _ := json.Marshal(a) //nolint:errchkjson // plain strings and slices
	out := oapi.Announcement{Id: a.ID, Revision: docstore.Digest(b)[:12], Title: a.Title, Tone: oapi.AnnouncementTone(a.Tone), Until: a.UntilAt}
	text := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	out.Text, out.Place, out.Lang = text(a.Text), text(a.Place), text(a.Lang)
	if !a.StartsAt.IsZero() {
		out.StartsAt = &a.StartsAt
	}
	if !a.EndsAt.IsZero() {
		out.EndsAt = &a.EndsAt
	}
	if a.Link.URL != "" {
		out.Link = &oapi.AnnouncementLink{Label: a.Link.Label, Url: a.Link.URL}
		out.Qr = n.qr.of(a.Link.URL)
	}
	return out
}

// qrCache keeps the QR code of each link while it is in use.
type qrCache struct {
	mu    sync.Mutex
	codes map[string]*oapi.QRCode
}

func (c *qrCache) of(url string) *oapi.QRCode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if q, ok := c.codes[url]; ok {
		return q
	}
	if len(c.codes) >= 2*(config.MaxAnnouncements*2) {
		clear(c.codes) // links that changed; at most twice the announcements are kept
	}
	q := qrOf(url)
	c.codes[url] = q
	return q
}
