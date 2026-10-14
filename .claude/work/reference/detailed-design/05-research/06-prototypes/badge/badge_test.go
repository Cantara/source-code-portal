package badge

import (
	"os"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func meas(t *testing.T) Measurer {
	b, err := os.ReadFile("../../dj/package/ttf/DejaVuSans.ttf")
	if err != nil {
		t.Skip("font missing")
	}
	f, _ := opentype.Parse(b)
	fc, _ := opentype.NewFace(f, &opentype.FaceOptions{Size: 11, DPI: 72, Hinting: font.HintingNone})
	return func(s string) float64 {
		var d fixed.Int26_6
		for _, r := range s {
			a, _ := fc.GlyphAdvance(r)
			d += a
		}
		return float64(d) / 64
	}
}

func TestRender(t *testing.T) {
	m := meas(t)
	out := Render(Badge{"issues", "12 open", "#555", "#ea7233"}, m)
	t.Logf("%s", out)
	out2 := Render(Badge{"a<b&\"c\x00", "x'y>", "#555", "#007ec6"}, m)
	t.Logf("%s", out2)
}
