// Package layout keeps the composition of the portal's pages (design/layout.md): which elements a page shows, in which order and
// with which options, as data an admin edits in the design mode. Layouts live in a store, the file system (`layout.dir`), one JSON
// file per page; without one the built-in layout renders. A layout selects groups, never repositories, and is resolved against the
// viewer's view before it is served.
package layout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

const (
	maxBytes    = 64 << 10 // a layout file or request body
	maxElements = 50
	maxGroups   = 100
	maxCount    = 50
	maxText     = 120
	maxNote     = 500
)

// pages are the pages that have a layout.
var pages = []string{"dashboard", "present"} //nolint:gochecknoglobals // a constant list

var groupID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Known reports whether page has a layout.
func Known(page string) bool { return slices.Contains(pages, page) }

// Layout is a page's elements in display order.
type Layout struct {
	Elements []oapi.LayoutElement `json:"elements"`
}

// Default is the built-in layout of page: the announcements first, below the header, then the fleet's numbers and map, then the products, then the pull requests whose turn it is and people's commits. The presentation page
// (design/present.md) shows the same compact: the fleet by health and the products as tiles.
func Default(page string) Layout {
	switch page {
	case "dashboard":
	case "present":
		return Layout{Elements: []oapi.LayoutElement{
			{Kind: oapi.LayoutElementKindTelemetry},
			{Kind: oapi.LayoutElementKindAnnouncements},
			{Kind: oapi.LayoutElementKindFleetMap, Color: new(oapi.LayoutElementColorHealth)},
			{Kind: oapi.LayoutElementKindProducts, View: new(oapi.Tiles)},
			{Kind: oapi.LayoutElementKindPulls, Count: new(8)},
			{Kind: oapi.LayoutElementKindLatestCommits, Count: new(30)},
		}}
	default:
		return Layout{Elements: []oapi.LayoutElement{}}
	}
	return Layout{Elements: []oapi.LayoutElement{
		{Kind: oapi.LayoutElementKindAnnouncements},
		{Kind: oapi.LayoutElementKindTelemetry},
		{Kind: oapi.LayoutElementKindFleetMap},
		{Kind: oapi.LayoutElementKindProducts, View: new(oapi.Cards)},
		{Kind: oapi.LayoutElementKindPulls, Count: new(5)},
		{Kind: oapi.LayoutElementKindLatestCommits, Count: new(10)},
	}}
}

// Parse decodes and checks a layout: at most 64 KiB of one JSON object, unknown fields refused.
func Parse(b []byte) (Layout, error) {
	if len(b) > maxBytes {
		return Layout{}, fmt.Errorf("larger than %d KiB", maxBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var l Layout
	if err := dec.Decode(&l); err != nil {
		return Layout{}, fmt.Errorf("not a layout: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Layout{}, errors.New("not a layout: more than one JSON value")
	}
	return l, l.Validate()
}

// Validate checks every element against the catalogue: a known kind, only its own fields, each within its bounds. Text is cleaned to
// one plain line first.
func (l Layout) Validate() error {
	if len(l.Elements) == 0 || len(l.Elements) > maxElements {
		return fmt.Errorf("elements: 1..%d", maxElements)
	}
	for i := range l.Elements {
		if err := check(&l.Elements[i]); err != nil {
			return fmt.Errorf("elements[%d] (%s): %w", i, l.Elements[i].Kind, err)
		}
	}
	return nil
}

func check(e *oapi.LayoutElement) error {
	if !e.Kind.Valid() {
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	takes := map[string]bool{}
	switch e.Kind {
	case oapi.LayoutElementKindFleetMap:
		takes["groups"], takes["color"] = true, true
	case oapi.LayoutElementKindProducts:
		takes["groups"], takes["view"] = true, true
	case oapi.LayoutElementKindLatestCommits, oapi.LayoutElementKindPulls:
		takes["count"] = true
	case oapi.LayoutElementKindHeading:
		takes["text"], takes["note"] = true, true
	case oapi.LayoutElementKindTelemetry, oapi.LayoutElementKindAnnouncements:
	}
	for field, set := range map[string]bool{"groups": e.Groups != nil, "view": e.View != nil, "count": e.Count != nil,
		"text": e.Text != nil, "note": e.Note != nil, "color": e.Color != nil} {
		if set && !takes[field] {
			return fmt.Errorf("%s is not an option of this kind", field)
		}
	}
	if e.Groups != nil {
		gs := *e.Groups
		if len(gs) == 0 || len(gs) > maxGroups {
			return fmt.Errorf("groups: 1..%d ids, or none for every group", maxGroups)
		}
		for j, id := range gs {
			if !groupID.MatchString(id) || len(id) > 63 || slices.Contains(gs[:j], id) {
				return fmt.Errorf("groups[%d]: %q is not a group id or is listed twice", j, id)
			}
		}
	}
	if e.View != nil && !e.View.Valid() {
		return fmt.Errorf("view: %q is not cards, table or tiles", *e.View)
	}
	if e.Color != nil && !e.Color.Valid() {
		return fmt.Errorf("color: %q is neither activity nor health", *e.Color)
	}
	if e.Count != nil && (*e.Count < 1 || *e.Count > maxCount) {
		return fmt.Errorf("count: 1..%d", maxCount)
	}
	if e.Kind == oapi.LayoutElementKindHeading {
		if e.Text == nil || untrusted.Text(*e.Text, maxText) == "" {
			return errors.New("text: required")
		}
		*e.Text = untrusted.Text(*e.Text, maxText)
		if e.Note != nil {
			*e.Note = untrusted.Text(*e.Note, maxNote)
		}
	}
	return nil
}
