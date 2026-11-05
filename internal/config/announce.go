package config

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/untrusted"
)

// The bounds of an announcement (design/announcements.md).
const (
	MaxAnnouncements = 20
	maxTitle         = 80
	maxLine          = 280
	maxPlace         = 80
	maxLinkLabel     = 40
	maxLinkURL       = 2048
)

// The announcements' tones, pages and audiences.
var (
	AnnouncementTones     = []string{"notice", "event", "info"} //nolint:gochecknoglobals // a constant table, in display order
	AnnouncementPages     = []string{"dashboard", "present"}    //nolint:gochecknoglobals // a constant table
	AnnouncementAudiences = []string{"everyone", "signed-in"}   //nolint:gochecknoglobals // a constant table
)

var (
	announcementID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	languageTag    = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,3}$`)
)

// Announcement is one announcement (design/announcements.md): from `announcements[]` in the configuration, or an admin's in the
// layout store's directory (the same fields in JSON). CheckAnnouncements cleans and checks it and fills the parsed times.
type Announcement struct {
	ID        string           `mapstructure:"id"        json:"id"`
	Published *bool            `mapstructure:"published" json:"published,omitempty"` // nil: published
	Title     string           `mapstructure:"title"     json:"title"`
	Text      string           `mapstructure:"text"      json:"text,omitempty"`
	Tone      string           `mapstructure:"tone"      json:"tone,omitempty"`   // notice, event, info (default)
	Starts    string           `mapstructure:"starts"    json:"starts,omitempty"` // RFC 3339, with its offset
	Ends      string           `mapstructure:"ends"      json:"ends,omitempty"`
	Place     string           `mapstructure:"place"     json:"place,omitempty"`
	Link      AnnouncementLink `mapstructure:"link"      json:"link"`
	Lang      string           `mapstructure:"lang"      json:"lang,omitempty"`
	Pages     []string         `mapstructure:"pages"     json:"pages,omitempty"`    // default: every page
	Audience  string           `mapstructure:"audience"  json:"audience,omitempty"` // everyone (default) or signed-in
	From      string           `mapstructure:"from"      json:"from,omitempty"`     // shown from (default: at once)
	Until     string           `mapstructure:"until"     json:"until,omitempty"`    // shown until; required unless ends is set

	StartsAt, EndsAt, FromAt, UntilAt time.Time `mapstructure:"-" json:"-"` // parsed; zero when not set (UntilAt never)
}

// AnnouncementLink is an announcement's one link: an https address and the words on its button.
type AnnouncementLink struct {
	Label string `mapstructure:"label" json:"label,omitempty"`
	URL   string `mapstructure:"url"   json:"url,omitempty"`
}

// IsPublished reports whether the announcement is published (not a draft).
func (a Announcement) IsPublished() bool { return a.Published == nil || *a.Published }

// Shows reports whether the announcement is shown at now on page to a viewer (private: a signed-in viewer's full view).
func (a Announcement) Shows(now time.Time, page string, private bool) bool {
	return a.IsPublished() && !now.Before(a.FromAt) && now.Before(a.UntilAt) && slices.Contains(a.Pages, page) &&
		(a.Audience == "everyone" || private)
}

// CheckAnnouncements cleans every announcement's text, fills its defaults and parsed times, and returns what is wrong, each problem
// prefixed with key and the announcement's index.
func CheckAnnouncements(key string, list []Announcement) []string {
	var p []string
	if len(list) > MaxAnnouncements {
		p = append(p, fmt.Sprintf("%s: at most %d announcements", key, MaxAnnouncements))
	}
	seen := map[string]bool{}
	for i := range list {
		at := fmt.Sprintf("%s[%d]", key, i)
		p = append(p, list[i].check(at)...)
		if seen[list[i].ID] {
			p = append(p, fmt.Sprintf("%s.id: %q is used twice", at, list[i].ID))
		}
		seen[list[i].ID] = true
	}
	return p
}

func (a *Announcement) check(at string) []string {
	var p []string
	bad := func(field, format string, args ...any) {
		p = append(p, fmt.Sprintf("%s.%s: ", at, field)+fmt.Sprintf(format, args...))
	}
	if !announcementID.MatchString(a.ID) {
		bad("id", "%q is not lower-case letters, digits and dashes (at most 63)", a.ID)
	}
	line := func(field string, s *string, maxRunes int, required bool) {
		*s = untrusted.Text(*s, 1<<16)
		switch n := utf8.RuneCountInString(*s); {
		case n == 0 && required:
			bad(field, "required")
		case n > maxRunes:
			bad(field, "at most %d characters, not %d", maxRunes, n)
		}
	}
	line("title", &a.Title, maxTitle, true)
	line("text", &a.Text, maxLine, false)
	line("place", &a.Place, maxPlace, false)
	line("link.label", &a.Link.Label, maxLinkLabel, false)
	if a.Tone == "" {
		a.Tone = "info"
	}
	if !slices.Contains(AnnouncementTones, a.Tone) {
		bad("tone", "%q is not one of %s", a.Tone, strings.Join(AnnouncementTones, ", "))
	}
	if a.Audience == "" {
		a.Audience = "everyone"
	}
	if !slices.Contains(AnnouncementAudiences, a.Audience) {
		bad("audience", "%q is not one of %s", a.Audience, strings.Join(AnnouncementAudiences, ", "))
	}
	if a.Pages == nil {
		a.Pages = slices.Clone(AnnouncementPages)
	}
	if len(a.Pages) == 0 {
		bad("pages", "name at least one of %s", strings.Join(AnnouncementPages, ", "))
	}
	for j, pg := range a.Pages {
		if !slices.Contains(AnnouncementPages, pg) || slices.Contains(a.Pages[:j], pg) {
			bad("pages", "%q is not one of %s or is listed twice", pg, strings.Join(AnnouncementPages, ", "))
		}
	}
	if a.Lang != "" && !languageTag.MatchString(a.Lang) {
		bad("lang", "%q is not a language tag such as nb or en-GB", a.Lang)
	}
	switch {
	case a.Link.URL == "" && a.Link.Label != "":
		bad("link.url", "required with a label")
	case a.Link.URL != "":
		if a.Link.Label == "" {
			bad("link.label", "required with a link")
		}
		if u, err := url.Parse(a.Link.URL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(a.Link.URL) > maxLinkURL {
			bad("link.url", "not an https address with a host and no user info, at most %d bytes", maxLinkURL)
		}
	}
	when := func(field, s string, t *time.Time) {
		*t = time.Time{}
		if s == "" {
			return
		}
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			bad(field, "%q is not an RFC 3339 time with its offset, such as 2026-11-26T18:00:00+01:00", s)
			return
		}
		*t = v
	}
	when("starts", a.Starts, &a.StartsAt)
	when("ends", a.Ends, &a.EndsAt)
	when("from", a.From, &a.FromAt)
	when("until", a.Until, &a.UntilAt)
	if !a.EndsAt.IsZero() && !a.EndsAt.After(a.StartsAt) {
		bad("ends", "after starts")
	}
	if a.Until == "" && !a.EndsAt.IsZero() {
		a.UntilAt = a.EndsAt
	}
	switch {
	case a.UntilAt.IsZero() && a.Until == "":
		bad("until", "required unless ends is set: an announcement ends by itself")
	case !a.UntilAt.IsZero() && !a.UntilAt.After(a.FromAt):
		bad("until", "after from")
	}
	return p
}
