package httpapi

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	// DefaultMaxBody caps request bodies on routes that set no limit of their own.
	DefaultMaxBody = 64 << 10
	gzipMin        = 1 << 10
)

const (
	cspDefault      = "default-src 'none'; frame-ancestors 'none'; sandbox"
	permissionsPage = "camera=(), microphone=(), geolocation=(), payment=(), usb=()"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Options configures what both listeners share.
type Options struct {
	Log            *slog.Logger
	Build          func() string  // the web UI bundle's build, sent as X-SCP-Build on every response, so open pages notice a new one
	HSTS           bool           // the public URL is https
	PublicURL      string         // server.public_url: the origin of canonical URLs; empty: the request's
	WebToken       string         // the secret the web UI server shows on its API calls to name the page's viewer (Pages)
	FormTargets    []string       // origins a page's form may end at beyond the portal: the sign-in provider's end session
	TrustedProxies []netip.Prefix // peers whose X-Forwarded-For, and the Access headers, are believed
	Access         Access
	Auth           Authenticator // SCP's own sign-in and bearer tokens; nil without them
	RequireAuth    bool          // access.require_auth: the public listener refuses anonymous requests (but the open paths)
	// Observe, when set, receives every request: listener, matched pattern ("unmatched" when none), method, status, duration.
	Observe func(listener, pattern, method string, status int, d time.Duration)
}

// wrap is the middleware chain, outermost first: request id, recover, access log and metrics, security headers, request deadline,
// gzip. It is one function so the pattern the mux matched (set on the request it was given) is visible to the log and metrics.
func (o Options) wrap(listener string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		h := w.Header()
		h.Set("X-Request-Id", id)
		if o.Build != nil {
			if b := o.Build(); b != "" {
				h.Set("X-SCP-Build", b)
			}
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", cspDefault)
		if o.HSTS {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if o.Access.Vary {
			h.Add("Vary", o.Access.UserHeader+", "+o.Access.GroupsHeader)
		}
		if o.Auth != nil {
			h.Add("Vary", "Cookie, Authorization")
		}
		viewer, verr := o.viewer(w, r)
		private := viewer.Private || viewer.SignedIn() // what a signed-in viewer sees names them: no shared cache may store it
		base := context.WithValue(r.Context(), viewerKey{}, viewer)
		ctx, cancel := context.WithTimeout(context.WithValue(base, baseKey{}, base), requestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		sw := &statusWriter{ResponseWriter: w, private: private}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // the sentinel is panicked as is
					panic(v)
				}
				o.Log.Error("handler panic", "panic", v, "stack", string(debug.Stack()), "request_id", id)
				if sw.status == 0 {
					WriteProblem(sw, http.StatusInternalServerError, "internal", "The response could not be produced.")
				}
			}
			pattern := r.Pattern
			if pattern == "" {
				pattern = "unmatched"
			}
			status := sw.status
			if status == 0 { // nothing written: the server sends the headers after this
				sw.privatise()
				status = http.StatusOK
			}
			d := time.Since(start)
			if o.Observe != nil {
				o.Observe(listener, pattern, r.Method, status, d)
			}
			o.Log.Info("request", "listener", listener, "pattern", pattern, "method", r.Method, "status", status, "bytes", sw.bytes,
				"duration_ms", d.Milliseconds(), "request_id", id, "client_ip", o.clientIP(r))
		}()
		if verr != nil {
			sw.Header().Set("WWW-Authenticate", `Bearer realm="scp"`)
			WriteProblem(sw, http.StatusUnauthorized, "unauthenticated", "The credential names nobody.")
			return
		}
		if listener == "public" && o.RequireAuth && !viewer.SignedIn() && !viewer.web && !openPath(r.URL.Path) {
			refuse(sw, r)
			return
		}
		gz := &gzipWriter{ResponseWriter: sw, accept: r.Method != http.MethodHead && acceptsGzip(r.Header.Get("Accept-Encoding"))}
		defer gz.close()
		next.ServeHTTP(gz, r)
	})
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// fromTrusted reports whether the peer of r is a trusted proxy.
func (o Options) fromTrusted(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && o.trusted(ip)
}

// clientIP is the peer address, or when the peer is a trusted proxy the right-most X-Forwarded-For entry that is not one.
func (o Options) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !o.trusted(ip) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if ip = hop.Unmap(); !o.trusted(ip) {
			return ip.String()
		}
	}
	return ip.String()
}

func (o Options) trusted(ip netip.Addr) bool {
	for _, p := range o.TrustedProxies {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

// limit caps the request body at n bytes.
func limit(n int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}

// statusWriter records the status and the bytes written.
type statusWriter struct {
	http.ResponseWriter
	status  int
	bytes   int64
	private bool // the response holds what only a private viewer sees: no shared cache may store it
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.privatise()
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
		w.privatise()
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// privatise marks a private viewer's response private: "public" becomes "private", and "private" is added when neither is set.
func (w *statusWriter) privatise() {
	if !w.private {
		return
	}
	h := w.Header()
	cc := h.Get("Cache-Control")
	switch {
	case strings.Contains(cc, "public"):
		h.Set("Cache-Control", strings.Replace(cc, "public", "private", 1))
	case cc == "":
		h.Set("Cache-Control", "private, no-cache")
	case !strings.Contains(cc, "private") && !strings.Contains(cc, "no-store"):
		h.Set("Cache-Control", "private, "+cc)
	}
}

func acceptsGzip(ae string) bool {
	for part := range strings.SplitSeq(ae, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.ReplaceAll(strings.TrimSpace(params), " ", "") != "q=0" {
			return true
		}
	}
	return false
}

func compressible(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch {
	case strings.HasPrefix(mt, "text/"), mt == "application/json", mt == "application/problem+json", mt == "application/yaml",
		mt == "image/svg+xml":
		return true
	}
	return false
}

// gzipWriter compresses compressible responses of at least gzipMin bytes. It holds the status and the first bytes until it knows
// the size; Vary: Accept-Encoding is set on every compressible response.
type gzipWriter struct {
	http.ResponseWriter
	accept  bool
	status  int
	buf     []byte
	decided bool
	gz      *gzip.Writer
}

func (w *gzipWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *gzipWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.decided {
		if w.gz != nil {
			return w.gz.Write(p)
		}
		return w.ResponseWriter.Write(p)
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) >= gzipMin {
		if err := w.decide(true); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// decide sends the header and the held bytes, compressed when big enough and allowed.
func (w *gzipWriter) decide(big bool) error {
	w.decided = true
	h := w.Header()
	ok := compressible(h.Get("Content-Type"))
	if ok {
		h.Add("Vary", "Accept-Encoding")
	}
	if big && ok && w.accept && w.status == http.StatusOK && h.Get("Content-Encoding") == "" && h.Get("Content-Range") == "" {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		w.ResponseWriter.WriteHeader(w.status)
		w.gz = gzip.NewWriter(w.ResponseWriter)
		_, err := w.gz.Write(w.buf)
		w.buf = nil
		return err
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, err := w.ResponseWriter.Write(w.buf)
	w.buf = nil
	return err
}

func (w *gzipWriter) close() {
	switch {
	case w.gz != nil:
		_ = w.gz.Close()
	case !w.decided && w.status != 0:
		_ = w.decide(false)
	}
}

func (w *gzipWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush sends what is held; streaming handlers are not compressed.
func (w *gzipWriter) Flush() {
	if !w.decided {
		_ = w.decide(false)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// openPaths answer an anonymous request under access.require_auth: signing in, the sign-in page with its assets and the sign-in
// methods it reads (/api/v1/me), GitHub's deliveries (signed) and robots.txt.
var openPaths = []string{"/auth/", "/signin", "/_next/static/", "/favicon.svg", "/theme-init.js", "/api/v1/me", "/webhooks/", "/robots.txt"} //nolint:gochecknoglobals // lookup table

func openPath(p string) bool {
	return slices.ContainsFunc(openPaths, func(o string) bool {
		return p == o || strings.HasSuffix(o, "/") && strings.HasPrefix(p, o)
	})
}

// refuse answers an anonymous request under access.require_auth: a browser asking for a page goes to sign-in and comes back;
// anything else gets 401.
func refuse(w http.ResponseWriter, r *http.Request) {
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !strings.HasPrefix(r.URL.Path, "/api/") &&
		strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/signin?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="scp"`)
	WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "This portal answers signed-in viewers only.")
}
