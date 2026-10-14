// Package badge renders flat-square SVG badges. No third-party code.
package badge

import (
	"fmt"
	"strings"
	"text/template"
)

type Measurer func(s string) float64 // pixel width at 11px, regular weight

type Badge struct {
	Label, Message string
	LabelColor     string // validated #rrggbb
	MessageColor   string
}

var tpl = template.Must(template.New("b").Funcs(template.FuncMap{"esc": xmlEscape}).Parse(
	`<svg xmlns="http://www.w3.org/2000/svg" width="{{.W}}" height="20" role="img" aria-label="{{esc .Aria}}"><title>{{esc .Aria}}</title>` +
		`<g shape-rendering="crispEdges"><rect width="{{.LW}}" height="20" fill="{{.LC}}"/><rect x="{{.LW}}" width="{{.MW}}" height="20" fill="{{.MC}}"/></g>` +
		`<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110">` +
		`<text x="{{.LX}}" y="140" textLength="{{.LT}}" transform="scale(.1)">{{esc .Label}}</text>` +
		`<text x="{{.MX}}" y="140" textLength="{{.MT}}" transform="scale(.1)">{{esc .Message}}</text></g></svg>`))

// xmlEscape: escape the five XML specials AND drop characters illegal in XML 1.0.
func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\'':
			b.WriteString("&apos;")
		case r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD && r != 0xFFFE) || r >= 0x10000:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func width(m Measurer, s string) int { // Shields: truncate to int, then round up to odd for pixel alignment
	w := int(m(s))
	if w%2 == 0 {
		w++
	}
	return w
}

func Render(b Badge, m Measurer) []byte {
	const pad = 5
	lt, mt := width(m, b.Label), width(m, b.Message)
	lw, mw := lt+2*pad, mt+2*pad
	aria := b.Message
	if b.Label != "" {
		aria = b.Label + ": " + b.Message
	}
	var sb strings.Builder
	_ = tpl.Execute(&sb, map[string]any{
		"W": lw + mw, "LW": lw, "MW": mw, "LC": b.LabelColor, "MC": b.MessageColor,
		"LX": lw*5 + 10, "MX": lw*10 + mw*5 - 10, "LT": lt * 10, "MT": mt * 10,
		"Label": b.Label, "Message": b.Message, "Aria": aria,
	})
	_ = fmt.Sprint
	return []byte(sb.String())
}
