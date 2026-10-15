package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// The headers a proxied page request carries to the web UI server, which sends the first three back on its API calls.
const (
	headerToken  = "X-Scp-Token"  //nolint:gosec // a header name; proves that a request comes from the web UI server
	headerViewer = "X-Scp-Viewer" // public, private or admin: the viewer the page is rendered for
	headerName   = "X-Scp-Name"   // the signed-in viewer's name, path-escaped
	headerVia    = "X-Scp-Via"    // how the viewer is identified: proxy, oidc, token; empty when anonymous
	headerOrigin = "X-Scp-Origin" // the portal's public origin, for canonical URLs
)

// notFoundPath is an address no page has: the web UI server answers it with its not-found page.
const notFoundPath = "/_scp/not-found"

// reserved prefixes answer a plain 404 instead of a page.
var reserved = []string{"/auth/", "/badges/", "/images/", "/webhooks/", "/.well-known/"} //nolint:gochecknoglobals // lookup table

type pageKey struct{}

type pageRequest struct{ nonce, id string }

// Pages returns the handler of the web UI's pages: a reverse proxy to the web UI server at upstream (nil: no web UI, every page
// answers 503). Each request gets a CSP nonce, set on the response and, for the server to put on its scripts, on the request; the
// request carries the viewer with o.WebToken, the request id and the origin.
func (o Options) Pages(upstream *url.URL) http.Handler {
	if upstream == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("The web UI is not configured (web.dir). The API is at /api/v1/, Markdown for every page at /llms.txt.\n"))
		})
	}
	transport := &http.Transport{MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: requestTimeout}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.SetXForwarded()
			pr.Out.Host = pr.In.Host
			p, _ := pr.In.Context().Value(pageKey{}).(pageRequest)
			h := pr.Out.Header
			v := ViewerOf(pr.In.Context())
			h.Set("Content-Security-Policy", o.cspPage(p.nonce))
			h.Set("X-Request-Id", p.id)
			h.Set(headerToken, o.WebToken)
			h.Set(headerViewer, v.headerValue())
			h.Set(headerName, url.PathEscape(v.Name))
			h.Set(headerVia, v.Via)
			h.Del("Authorization") // the web UI server needs only the viewer
			h.Set(headerOrigin, o.origin(pr.In))
		},
		Transport: transport,
		ModifyResponse: func(res *http.Response) error {
			notFoundPage(transport, res)
			res.Header.Del("Content-Security-Policy") // the policy is the one set below, with this request's nonce
			res.Header.Del("X-Powered-By")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() == nil {
				o.Log.Warn("web UI server not reachable", "err", err)
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("The web UI is starting. Try again in a few seconds.\n"))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasReservedPrefix(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		p := pageRequest{nonce: newNonce(), id: w.Header().Get("X-Request-Id")}
		w.Header().Set("Content-Security-Policy", o.cspPage(p.nonce))
		w.Header().Set("Permissions-Policy", permissionsPage)
		rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), pageKey{}, p)))
	})
}

// notFoundPage replaces the body of a page that answered 404 with the not-found page of an address no page has. A page that finds
// no repository or group calls Next.js's notFound(), whose server HTML is an empty shell the browser renders only with JavaScript
// (vercel/next.js#62228); the not-found page of an unmatched address is complete HTML. Client navigations (RSC requests) keep their
// answer.
func notFoundPage(rt http.RoundTripper, res *http.Response) {
	in := res.Request
	if res.StatusCode != http.StatusNotFound || in.Header.Get("RSC") != "" || in.URL.Path == notFoundPath ||
		in.Method != http.MethodGet && in.Method != http.MethodHead {
		return
	}
	req := in.Clone(in.Context())
	req.URL.Path, req.URL.RawPath, req.URL.RawQuery = notFoundPath, "", ""
	nf, err := rt.RoundTrip(req)
	if err != nil {
		return
	}
	if nf.StatusCode != http.StatusNotFound {
		_ = nf.Body.Close()
		return
	}
	_ = res.Body.Close()
	res.Body, res.Header, res.ContentLength = nf.Body, nf.Header, nf.ContentLength
}

// cspPage is the policy of a page: scripts and styles from the portal or carrying the page's nonce; forms post to the portal, whose
// answer may lead on to a FormTargets origin (browsers hold a form's redirects to form-action).
func (o Options) cspPage(nonce string) string {
	forms := strings.Join(append([]string{"'self'"}, o.FormTargets...), " ")
	return "default-src 'none'; script-src 'self' 'nonce-" + nonce + "'; style-src 'self' 'nonce-" + nonce + "'; img-src 'self'; " +
		"font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action " + forms + "; frame-ancestors 'none'; " +
		"object-src 'none'"
}

func newNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.StdEncoding.EncodeToString(b[:])
}

// origin is the portal's public origin: server.public_url, else the scheme and host the request arrived with (the scheme from a
// trusted proxy's X-Forwarded-Proto).
func (o Options) origin(r *http.Request) string {
	if o.PublicURL != "" {
		if u, err := url.Parse(o.PublicURL); err == nil {
			return u.Scheme + "://" + u.Host
		}
	}
	scheme := "http"
	if r.TLS != nil || o.fromTrusted(r) && r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func hasReservedPrefix(p string) bool {
	for _, pre := range reserved {
		if strings.HasPrefix(p+"/", pre) {
			return true
		}
	}
	return false
}
