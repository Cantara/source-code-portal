package badges

import "unicode"

// advanceUnits returns the advance of r in font units (widthUnitsPerEm per em).
func advanceUnits(r rune) int {
	var idx int
	switch {
	case r >= 0x20 && r <= 0x7E:
		idx = int(r - 0x20)
	case r >= 0xA0 && r <= 0x17F:
		idx = 95 + int(r-0xA0)
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0 // combining marks and format characters add no advance
	case isWide(r):
		return widthWide
	default:
		return widthAverage
	}
	return int(widthTable[2*idx])<<8 | int(widthTable[2*idx+1])
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0x303E, r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3, r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60, r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}

// fontPx is the size the badge text is drawn at (font-size 110 scaled by .1).
const fontPx = 11

// textWidth is the pixel width the renderer reserves for s at fontPx: the summed advance truncated to whole pixels and
// rounded up to the next odd number, which is what Shields' flat-square does to keep the text centred on whole pixels.
func textWidth(s string) int {
	sum := 0
	for _, r := range s {
		sum += advanceUnits(r)
	}
	w := sum * fontPx / widthUnitsPerEm
	if w%2 == 0 {
		w++
	}
	return w
}
