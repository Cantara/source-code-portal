package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAnnouncementsLoad(t *testing.T) {
	cfg, _, err := Load(file(t, `announcements:
  - id: juleol-2026
    title: "  Juleølsmaking på\tRebel Makerspace "
    text: Det avholdes juleølsmaking i år.
    tone: event
    starts: "2026-11-26T18:00:00+01:00"
    ends: "2026-11-26T23:00:00+01:00"
    place: Rebel Makerspace
    link: {label: Meld deg på her, url: "https://example.org/pamelding"}
    lang: nb
  - id: maintenance
    title: GitHub App rotation
    published: false
    pages: [dashboard]
    audience: signed-in
    until: "2026-12-01T00:00:00Z"
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := cfg.Announcements[0], cfg.Announcements[1]
	if a.Title != "Juleølsmaking på Rebel Makerspace" || a.Tone != "event" || a.Audience != "everyone" || !slices.Equal(a.Pages, AnnouncementPages) {
		t.Errorf("cleaned and defaulted: %+v", a)
	}
	if !a.UntilAt.Equal(a.EndsAt) || a.StartsAt.Hour() != 18 {
		t.Errorf("until from ends, the offset kept: %v %v", a.UntilAt, a.StartsAt)
	}
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if !a.Shows(at, "present", false) || a.Shows(a.EndsAt, "present", false) {
		t.Error("shown until its end, not at it")
	}
	if b.IsPublished() || b.Shows(at, "dashboard", true) {
		t.Error("a draft is never shown")
	}
	on := true
	b.Published = &on
	if !b.Shows(at, "dashboard", true) || b.Shows(at, "dashboard", false) || b.Shows(at, "present", true) {
		t.Error("a signed-in one on the dashboard alone, to a private viewer alone")
	}
}

func TestAnnouncementsRefused(t *testing.T) {
	base := "  - {id: a, title: T, until: \"2026-12-01T00:00:00Z\""
	for name, tc := range map[string]struct{ entry, want string }{
		"no until":      {"  - {id: a, title: T", "announcements[0].until: required unless ends"},
		"no title":      {"  - {id: a, until: \"2026-12-01T00:00:00Z\"", "announcements[0].title: required"},
		"bad id":        {"  - {id: Jule Øl, title: T, until: \"2026-12-01T00:00:00Z\"", "announcements[0].id"},
		"long title":    {"  - {id: a, until: \"2026-12-01T00:00:00Z\", title: " + strings.Repeat("x", 81), "at most 80 characters"},
		"http link":     {base + ", link: {label: Go, url: \"http://example.org\"}", "announcements[0].link.url"},
		"user info":     {base + ", link: {label: Go, url: \"https://u:p@example.org\"}", "announcements[0].link.url"},
		"link no label": {base + ", link: {url: \"https://example.org\"}", "announcements[0].link.label: required"},
		"label no link": {base + ", link: {label: Go}", "announcements[0].link.url: required"},
		"tone":          {base + ", tone: alarm", "announcements[0].tone"},
		"page":          {base + ", pages: [groups]", "announcements[0].pages"},
		"no page":       {base + ", pages: []", "name at least one"},
		"audience":      {base + ", audience: admins", "announcements[0].audience"},
		"time zone":     {base + ", starts: \"2026-11-26 18:00\"", "announcements[0].starts"},
		"ends before":   {base + ", starts: \"2026-11-26T18:00:00Z\", ends: \"2026-11-26T17:00:00Z\"", "ends: after starts"},
		"until before":  {base + ", from: \"2026-12-02T00:00:00Z\"", "until: after from"},
		"language":      {base + ", lang: norsk_bokmål", "announcements[0].lang"},
		"unknown field": {base + ", colour: red", "colour"},
		"same id twice": {base + "}\n" + base, "is used twice"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Load(file(t, "announcements:\n"+tc.entry+"}\n"), nil)
			var inv *Invalid
			if !errors.As(err, &inv) || !strings.Contains(inv.Error(), tc.want) {
				t.Errorf("err = %v, want a problem with %q", err, tc.want)
			}
		})
	}
}
