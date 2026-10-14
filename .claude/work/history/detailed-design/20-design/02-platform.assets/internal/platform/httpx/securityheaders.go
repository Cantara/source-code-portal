package httpx

import "net/http"

// HeaderClass selects the default security headers for a response. The route
// table (cmd/scp) assigns a class to every route; httpx only knows the names.
type HeaderClass string

const (
	HeadersNone  HeaderClass = ""      // set nothing (a handler sets everything itself)
	HeadersHTML  HeaderClass = "html"  // SPA document; CSP is set by webapp, not here
	HeadersAsset HeaderClass = "asset" // static JS/CSS/fonts/images under the SPA
	HeadersAPI   HeaderClass = "api"   // /api/*, llms*.txt, *.md
	HeadersBadge HeaderClass = "badge" // /badges/*
	HeadersMedia HeaderClass = "media" // /media/*
)

const (
	cspLocked   = "default-src 'none'; frame-ancestors 'none'; sandbox"
	cspSandbox  = "default-src 'none'; sandbox"
	permsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
	hstsValue   = "max-age=31536000"
)

// Pair is one header name and value.
type Pair struct{ Name, Value string }

// Defaults returns the headers a class receives, in a stable order. These
// strings are pinned by golden tests. A header the handler already set is never
// overwritten.
func (c HeaderClass) Defaults(hsts bool) []Pair {
	if c == HeadersNone {
		return nil
	}
	if c == HeadersAsset { // a CSP or CORP on the SPA's own JS and CSS means nothing (06-httpapi 4.10.3)
		p := []Pair{{"X-Content-Type-Options", "nosniff"}}
		if hsts {
			p = append(p, Pair{"Strict-Transport-Security", hstsValue})
		}
		return p
	}
	p := []Pair{
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Permissions-Policy", permsPolicy},
	}
	switch c {
	case HeadersHTML:
		p = append(p,
			Pair{"Cross-Origin-Opener-Policy", "same-origin"},
			Pair{"X-Frame-Options", "DENY"},
		)
	case HeadersAPI:
		p = append(p,
			Pair{"Content-Security-Policy", cspLocked},
			Pair{"Cross-Origin-Resource-Policy", "same-origin"},
		)
	case HeadersBadge:
		p = append(p,
			Pair{"Content-Security-Policy", cspSandbox},
			Pair{"Cross-Origin-Resource-Policy", "cross-origin"},
		)
	case HeadersMedia:
		p = append(p,
			Pair{"Content-Security-Policy", cspSandbox},
			Pair{"Cross-Origin-Resource-Policy", "same-origin"},
		)
	}
	if hsts {
		p = append(p, Pair{"Strict-Transport-Security", hstsValue})
	}
	return p
}

// applyDefaults sets each default only if the header is absent.
func applyDefaults(h http.Header, c HeaderClass, hsts bool) {
	for _, p := range c.Defaults(hsts) {
		if _, ok := h[http.CanonicalHeaderKey(p.Name)]; !ok {
			h.Set(p.Name, p.Value)
		}
	}
}
