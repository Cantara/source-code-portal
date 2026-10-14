package badges

import "encoding/xml"

// Badge is what the renderer draws. Label and Message are plain text (never markup); colours come from the palette.
type Badge struct {
	Label, Message string
	LabelColor     Color
	MessageColor   Color
}

const (
	badgeHeight = 20
	hPad        = 5 // horizontal padding on each side of a text run
)

type rect struct {
	XMLName xml.Name `xml:"rect"`
	X       int      `xml:"x,attr,omitempty"`
	Width   int      `xml:"width,attr"`
	Height  int      `xml:"height,attr"`
	Fill    string   `xml:"fill,attr"`
}

type text struct {
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
	Texts         []text
}

type svg struct {
	XMLName   xml.Name `xml:"svg"`
	Xmlns     string   `xml:"xmlns,attr"`
	Width     int      `xml:"width,attr"`
	Height    int      `xml:"height,attr"`
	Tone      string   `xml:"role,attr"`
	AriaLabel string   `xml:"aria-label,attr"`
	Title     string   `xml:"title"`
	Groups    []any    `xml:",any"` // *shapes then *labels; each carries its own XMLName
}

// Render draws a flat-square badge (the Shields layout without gradient, blur or ids): two rectangles and two text runs
// whose drawn length is forced with textLength, so a measurement error of the font table shows as slightly tighter or
// looser letter spacing and never as overflow. The document is built by marshalling typed values with encoding/xml:
// escaping is structural, characters that XML 1.0 cannot carry become U+FFFD, and no string is ever interpolated.
func Render(b Badge) []byte {
	label, msg := cleanText(b.Label, maxMessageRunes), cleanText(b.Message, maxMessageRunes)
	lt, mt := textWidth(label), textWidth(msg)
	lw, mw := lt+2*hPad, mt+2*hPad
	aria := msg
	if label != "" {
		aria = label + ": " + msg
	}
	doc := svg{
		Xmlns: "http://www.w3.org/2000/svg", Width: lw + mw, Height: badgeHeight, Tone: "img", AriaLabel: aria, Title: aria,
		Groups: []any{&shapes{ShapeRendering: "crispEdges", Rects: []rect{
			{Width: lw, Height: badgeHeight, Fill: b.LabelColor.String()},
			{X: lw, Width: mw, Height: badgeHeight, Fill: b.MessageColor.String()},
		}}, &labels{Fill: "#fff", TextAnchor: "middle", FontFamily: "DejaVu Sans,Verdana,Geneva,sans-serif",
			TextRendering: "geometricPrecision", FontSize: 110, Texts: []text{
				{X: lw*5 + 10, Y: 140, TextLength: lt * 10, Transform: "scale(.1)", Content: label},
				{X: lw*10 + mw*5 - 10, Y: 140, TextLength: mt * 10, Transform: "scale(.1)", Content: msg},
			}}},
	}
	out, err := xml.Marshal(doc)
	if err != nil { // unreachable: the document contains only strings and integers
		panic("badges: marshal: " + err.Error())
	}
	return out
}
