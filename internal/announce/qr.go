package announce

import (
	"strconv"
	"strings"

	"rsc.io/qr"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// qrOf is url as a QR code (level M: readable with 15% of it covered, as a screen's glare or a smudge does) drawn as one SVG path of
// horizontal runs, one unit per module, without the quiet zone the page adds around it. Nil when url does not fit a code.
func qrOf(url string) *oapi.QRCode {
	c, err := qr.Encode(url, qr.M)
	if err != nil {
		return nil
	}
	var b strings.Builder
	for y := range c.Size {
		for x := 0; x < c.Size; {
			if !c.Black(x, y) {
				x++
				continue
			}
			run := 1
			for x+run < c.Size && c.Black(x+run, y) {
				run++
			}
			b.WriteString("M" + strconv.Itoa(x) + " " + strconv.Itoa(y) + "h" + strconv.Itoa(run) + "v1h-" + strconv.Itoa(run) + "z")
			x += run
		}
	}
	return &oapi.QRCode{Size: c.Size, Path: b.String()}
}
