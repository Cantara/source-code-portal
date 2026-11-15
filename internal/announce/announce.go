// Package announce keeps the portal's announcements (design/announcements.md): the configuration's `announcements[]` is the base, and
// the admins' changes are the store's overrides (the document `announcements`, design/config-baseline.md): an announcement by id
// replaces the file's or is added, and `removed` names the file's ones taken away. For gives the ones a viewer may see on a page now;
// the admin's routes read and save the list in force.
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

// List is the stored document: the announcements changed or added here, and the ids of the configuration's ones removed here.
type List struct {
	Announcements []config.Announcement `json:"announcements"`
	Removed       []string              `json:"removed,omitempty"`
}

// Count is how many changes l holds.
func (l List) Count() int { return len(l.Announcements) + len(l.Removed) }

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
	for i, id := range l.Removed {
		if id == "" || len(id) > 63 {
			return List{}, fmt.Errorf("removed[%d]: %q is not an announcement id", i, id)
		}
	}
	return l, nil
}

// Notices are the configured announcements and the stored changes to them.
type Notices struct {
	configured []config.Announcement // checked by the configuration, drafts and published made explicit
	file       *docstore.Doc[List]
	now        func() time.Time
	qr         *qrCache
}

// Name is the stored announcements' document.
const Name = "announcements"

// Open reads the stored changes from db (nil: the configured ones only, and no saving). Changes equal to the configuration are
// dropped, so a configuration downloaded and deployed leaves the store without changes of its own. now is the clock (time.Now).
func Open(ctx context.Context, configured []config.Announcement, db docstore.Backend, now func() time.Time, log *slog.Logger) *Notices {
	log = log.With("store", "announcements")
	n := &Notices{
		configured: explicit(configured),
		file:       docstore.Open(ctx, db, Name, List{Announcements: []config.Announcement{}}, Parse, log),
		now:        now,
		qr:         &qrCache{codes: map[string]*oapi.QRCode{}},
	}
	if st := n.file.Get(); st.Stored && st.Problem == "" {
		if pruned := n.diff(n.all()); pruned.Count() != st.Value.Count() {
			if b, err := encode(pruned); err == nil {
				if _, err := n.file.Save(ctx, b, st.Revision); err != nil {
					log.Error("the changes equal to the configuration were not dropped", "err", err)
				}
			}
		}
	}
	return n
}

// explicit is list with every announcement's published made explicit, so two that mean the same compare equal.
func explicit(list []config.Announcement) []config.Announcement {
	out := slices.Clone(list)
	for i, a := range out {
		if a.Published == nil {
			out[i].Published = new(true)
		}
	}
	return out
}

// same reports whether a and b are the same announcement, field by field as stored.
func same(a, b config.Announcement) bool {
	ja, _ := json.Marshal(a) //nolint:errchkjson // plain strings and slices
	jb, _ := json.Marshal(b) //nolint:errchkjson // plain strings and slices
	return bytes.Equal(ja, jb)
}

// diff is what the store keeps for the list in force: the announcements not as the configuration has them, and the configuration's
// ones the list lacks.
func (n *Notices) diff(list []config.Announcement) List {
	out := List{Announcements: []config.Announcement{}}
	for _, a := range list {
		if i := slices.IndexFunc(n.configured, func(c config.Announcement) bool { return c.ID == a.ID }); i < 0 || !same(n.configured[i], a) {
			out.Announcements = append(out.Announcements, a)
		}
	}
	for _, c := range n.configured {
		if !slices.ContainsFunc(list, func(a config.Announcement) bool { return a.ID == c.ID }) {
			out.Removed = append(out.Removed, c.ID)
		}
	}
	return out
}

func encode(l List) ([]byte, error) {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Problems is why the stored document is not served, and that changes are kept only in the store, for the admin status.
func (n *Notices) Problems() []string {
	st := n.file.Get()
	if st.Problem != "" {
		return []string{"announcements: " + st.Problem + "; serving the last good announcements"}
	}
	if c := st.Value.Count(); c > 0 {
		word, them := "changes", "them"
		if c == 1 {
			word, them = "change", "it"
		}
		return []string{fmt.Sprintf("announcements: %d %s kept only in the store, not in the configuration; download scp.yaml from Admin to keep %s",
			c, word, them)}
	}
	return nil
}

// all is the list in force: the configured ones in their order, each replaced by its stored change or left out when removed, then
// the ones added here.
func (n *Notices) all() []config.Announcement {
	l := n.file.Get().Value
	out := make([]config.Announcement, 0, len(n.configured)+len(l.Announcements))
	for _, c := range n.configured {
		if slices.Contains(l.Removed, c.ID) {
			continue
		}
		if i := slices.IndexFunc(l.Announcements, func(a config.Announcement) bool { return a.ID == c.ID }); i >= 0 {
			c = l.Announcements[i]
		}
		out = append(out, c)
	}
	for _, a := range l.Announcements {
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

// List is the announcements in force: the configuration's with the admins' changes.
func (n *Notices) List() []config.Announcement { return n.all() }
