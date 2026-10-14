package badges

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/untrusted"
)

// color is an RGB value from the closed palette; request data never reaches an SVG attribute.
type color struct{ r, g, b uint8 }

func (c color) String() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

// The palette: white text on every colour at 4.5:1 or more (WCAG AA).
var (
	labelColor   = color{0x55, 0x55, 0x55} //nolint:gochecknoglobals // constant palette
	successColor = color{0x1a, 0x7f, 0x37} //nolint:gochecknoglobals // constant palette
	infoColor    = color{0x0b, 0x61, 0xa4} //nolint:gochecknoglobals // constant palette
	warningColor = color{0xb4, 0x53, 0x09} //nolint:gochecknoglobals // constant palette
	neutralColor = color{0x6e, 0x6e, 0x6e} //nolint:gochecknoglobals // constant palette
)

// badge is what render draws. Label and message are plain text.
type badge struct {
	label, message string
	color          color
}

const (
	badgeHeight     = 20
	hPad            = 5 // horizontal padding on each side of a text run
	maxMessageRunes = 40
)

type rect struct {
	XMLName xml.Name `xml:"rect"`
	X       int      `xml:"x,attr,omitempty"`
	Width   int      `xml:"width,attr"`
	Height  int      `xml:"height,attr"`
	Fill    string   `xml:"fill,attr"`
}

type textRun struct {
	XMLName    xml.Name `xml:"text"`
	X          int      `xml:"x,attr"`
	Y          int      `xml:"y,attr"`
	TextLength int      `xml:"textLength,attr"`
	Transform  string   `xml:"transform,attr"`
	Content    string   `xml:",chardata"`
}

type shapes struct {
	XMLName        xml.Name `xml:"g"`
	ShapeRendering string   `xml:"shape-rendering,attr"`
	Rects          []rect
}

type labels struct {
	XMLName       xml.Name `xml:"g"`
	Fill          string   `xml:"fill,attr"`
	TextAnchor    string   `xml:"text-anchor,attr"`
	FontFamily    string   `xml:"font-family,attr"`
	TextRendering string   `xml:"text-rendering,attr"`
	FontSize      int      `xml:"font-size,attr"`
	Texts         []textRun
}

type svg struct {
	XMLName   xml.Name `xml:"svg"`
	Xmlns     string   `xml:"xmlns,attr"`
	Width     int      `xml:"width,attr"`
	Height    int      `xml:"height,attr"`
	Role      string   `xml:"role,attr"`
	AriaLabel string   `xml:"aria-label,attr"`
	Title     string   `xml:"title"`
	Groups    []any    `xml:",any"`
}

// render draws a flat badge: two rectangles and two text runs whose drawn length is fixed with textLength, so an error in the width
// table shows as letter spacing and never as overflow. The document is marshalled from typed values: escaping is structural and
// characters XML 1.0 cannot carry become U+FFFD.
func render(b badge) []byte {
	label, msg := untrusted.Text(b.label, maxMessageRunes), untrusted.Text(b.message, maxMessageRunes)
	lt, mt := textWidth(label), textWidth(msg)
	lw, mw := lt+2*hPad, mt+2*hPad
	aria := label + ": " + msg
	doc := svg{
		Xmlns: "http://www.w3.org/2000/svg", Width: lw + mw, Height: badgeHeight, Role: "img", AriaLabel: aria, Title: aria,
		Groups: []any{&shapes{ShapeRendering: "crispEdges", Rects: []rect{
			{Width: lw, Height: badgeHeight, Fill: labelColor.String()},
			{X: lw, Width: mw, Height: badgeHeight, Fill: b.color.String()},
		}}, &labels{Fill: "#fff", TextAnchor: "middle", FontFamily: "DejaVu Sans,Verdana,Geneva,sans-serif",
			TextRendering: "geometricPrecision", FontSize: 110, Texts: []textRun{
				{X: lw*5 + 10, Y: 140, TextLength: lt * 10, Transform: "scale(.1)", Content: label},
				{X: lw*10 + mw*5 - 10, Y: 140, TextLength: mt * 10, Transform: "scale(.1)", Content: msg},
			}}},
	}
	out, err := xml.Marshal(doc)
	if err != nil { // unreachable: the document holds only strings and integers
		panic("badges: marshal: " + err.Error())
	}
	return out
}

// formatCount abbreviates counts: 999, 1k, 1.2k, 12k, 999k, 1M, 1.2M (truncated, never rounded up).
func formatCount(n int) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return abbreviate(n, 1000, "k")
	}
	return abbreviate(n, 1_000_000, "M")
}

func abbreviate(n, unit int, suffix string) string {
	tenths := min(n*10/unit, 9990)
	if tenths < 100 {
		return strings.TrimSuffix(strconv.Itoa(tenths/10)+"."+strconv.Itoa(tenths%10), ".0") + suffix
	}
	return strconv.Itoa(tenths/10) + suffix
}

// relativeDay is the age of t at now in UTC calendar days: today, yesterday, N days ago (2..13), N weeks ago, N months ago, N years
// ago. A future t is today.
func relativeDay(now, t time.Time) (string, int) {
	days := civilDay(now) - civilDay(t)
	switch {
	case days <= 0:
		return "today", 0
	case days == 1:
		return "yesterday", 1
	case days < 14:
		return strconv.Itoa(days) + " days ago", days
	case days < 60:
		return strconv.Itoa(days/7) + " weeks ago", days
	case days < 730:
		return strconv.Itoa(days/30) + " months ago", days
	}
	return strconv.Itoa(days/365) + " years ago", days
}

func civilDay(t time.Time) int {
	y, m, d := t.UTC().Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}
