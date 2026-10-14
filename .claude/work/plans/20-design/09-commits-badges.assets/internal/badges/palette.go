package badges

import "fmt"

// Color is an opaque RGB value. There is no constructor from a string: colours exist only as the closed palette below,
// so request data can never reach an SVG attribute (type-level prevention of attribute injection for colours, GR-33).
type Color struct{ r, g, b uint8 }

func (c Color) String() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

// Tone is the semantic colour of a badge half.
type Tone uint8

const (
	ToneLabel   Tone = iota // left half
	ToneSuccess             // good, passing, none open
	ToneInfo                // neutral fact with a value
	ToneWarning             // needs attention
	ToneDanger              // failing, vulnerable
	ToneNeutral             // unknown, none, not applicable
)

type Palette uint8

const (
	PaletteAA  Palette = iota // white text on every role at >= 4.5:1 (WCAG 2.2 SC 1.4.3)
	PaletteAAA                // >= 7:1 (SC 1.4.6)
)

func parsePalette(s string) (Palette, bool) {
	switch s {
	case "aa":
		return PaletteAA, true
	case "aaa":
		return PaletteAAA, true
	}
	return 0, false
}

func (p Palette) Color(r Tone) Color {
	if p == PaletteAAA {
		switch r {
		case ToneLabel:
			return Color{0x44, 0x44, 0x44}
		case ToneSuccess:
			return Color{0x11, 0x63, 0x29}
		case ToneInfo:
			return Color{0x0a, 0x4f, 0x86}
		case ToneWarning:
			return Color{0x8a, 0x46, 0x00}
		case ToneDanger:
			return Color{0xa4, 0x0e, 0x26}
		}
		return Color{0x59, 0x59, 0x59}
	}
	switch r {
	case ToneLabel:
		return Color{0x55, 0x55, 0x55}
	case ToneSuccess:
		return Color{0x1a, 0x7f, 0x37}
	case ToneInfo:
		return Color{0x0b, 0x61, 0xa4}
	case ToneWarning:
		return Color{0xb4, 0x53, 0x09}
	case ToneDanger:
		return Color{0xc0, 0x39, 0x2b}
	}
	return Color{0x6e, 0x6e, 0x6e}
}
