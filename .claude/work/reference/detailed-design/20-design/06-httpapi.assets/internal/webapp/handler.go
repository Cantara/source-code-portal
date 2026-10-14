package webapp

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// Handler serves the SPA for one listener. Build it once with New; For derives the admin variant.
type Handler struct {
	b                   *bundle
	o                   Options
	shapes              shapeSet
	listener            Listener
	enforce, reportOnly string
}

// New reads and validates the whole dist once (GR-19: nothing is read per request, a bad build fails here).
func New(fsys fs.FS, o Options) (*Handler, error) {
	if err := o.defaults(); err != nil {
		return nil, err
	}
	b, err := loadBundle(fsys, o)
	if err != nil {
		return nil, err
	}
	h := &Handler{b: b, o: o, listener: Public}
	h.shapes = b.manifest.compile(Public)
	h.enforce, h.reportOnly = DocumentCSP(o.CSP)
	return h, nil
}

// For returns a handler for another listener sharing the same files.
func (h *Handler) For(l Listener) *Handler {
	c := *h
	c.listener = l
	c.shapes = h.b.manifest.compile(l)
	return &c
}

func (h *Handler) observe(k ServeKind, enc string) {
	if h.o.Observe != nil {
		h.o.Observe(k, enc)
	}
}

func (h *Handler) common(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("X-Content-Type-Options", "nosniff")
	if h.o.BuildID != "" {
		hd.Set("X-SCP-Build", h.o.BuildID)
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.common(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.plain(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p := r.URL.Path
	if p == "" || p == "/" {
		target := h.o.RootRedirect
		if h.listener == Admin {
			target = h.o.AdminRedirect
		}
		h.redirect(w, http.StatusFound, target, "no-store")
		return
	}
	// An exact file of the bundle is served even under a reserved prefix: the table lists /assets/ as reserved (it is not an
	// SPA page), and New refuses any bundle file under /api/. The reserved prefixes only stop the history fallback.
	if f, ok := h.b.files[strings.TrimPrefix(p, "/")]; ok && f != h.b.shell { // exact name: ServeMux already cleaned the path, so "/theme-init.js/" is not the file
		h.serveFile(w, r, f)
		return
	}
	for _, pre := range h.o.Reserved {
		if strings.HasPrefix(p, pre) {
			h.plain(w, r, http.StatusNotFound, "not found")
			return
		}
	}
	esc := r.URL.EscapedPath()
	if h.shapes.matches(esc) {
		h.serveShell(w, r, http.StatusOK)
		return
	}
	if strings.HasSuffix(esc, "/") && esc != "/" && h.shapes.matches(strings.TrimRight(esc, "/")) {
		target := strings.TrimRight(esc, "/")
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		h.redirect(w, http.StatusPermanentRedirect, target, "public, max-age=3600")
		return
	}
	if strings.HasPrefix(p, "/assets/") || path.Ext(p) != "" {
		h.plain(w, r, http.StatusNotFound, "not found") // a stale deploy must never answer HTML for a file
		return
	}
	h.serveShell(w, r, http.StatusNotFound)
}

func (h *Handler) plain(w http.ResponseWriter, r *http.Request, code int, msg string) {
	hd := w.Header()
	hd.Set("Content-Type", "text/plain; charset=utf-8")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
	hd.Set("Content-Length", strconv.Itoa(len(msg)+1))
	w.WriteHeader(code)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(msg + "\n"))
	}
	h.observe(KindNotFound, "")
}

func (h *Handler) redirect(w http.ResponseWriter, code int, target, cache string) {
	hd := w.Header()
	hd.Set("Location", target)
	hd.Set("Cache-Control", cache)
	hd.Set("Content-Length", "0")
	w.WriteHeader(code)
	h.observe(KindRedirect, "")
}

// pick chooses the representation for Accept-Encoding: br, then gzip, then identity.
func pick(f *file, ae string) (body []byte, enc, etag string) {
	switch {
	case f.br != nil && acceptsEnc(ae, "br"):
		return f.br, "br", strings.TrimSuffix(f.etag, `"`) + `-br"`
	case f.gz != nil && acceptsEnc(ae, "gzip"):
		return f.gz, "gzip", strings.TrimSuffix(f.etag, `"`) + `-gz"`
	}
	return f.body, "", f.etag
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, f *file) {
	hd := w.Header()
	body, enc, etag := pick(f, r.Header.Get("Accept-Encoding"))
	hd.Set("Content-Type", f.ctype)
	hd.Set("ETag", etag)
	hd.Add("Vary", "Accept-Encoding")
	if f.immutable {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hd.Set("Cache-Control", "no-cache")
	}
	if enc != "" {
		hd.Set("Content-Encoding", enc)
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	h.observe(KindFile, enc)
}

func (h *Handler) shellHeaders(hd http.Header) {
	if h.enforce != "" {
		hd.Set("Content-Security-Policy", h.enforce)
	}
	if h.reportOnly != "" {
		hd.Set("Content-Security-Policy-Report-Only", h.reportOnly)
	}
	hd.Set("Reporting-Endpoints", `csp="`+h.o.ReportPath+`"`)
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Cross-Origin-Opener-Policy", "same-origin")
	hd.Set("Referrer-Policy", h.o.ReferrerPolicy)
	hd.Set("Permissions-Policy", permissionsPolicy)
}

func (h *Handler) serveShell(w http.ResponseWriter, r *http.Request, status int) {
	f := h.b.shell
	hd := w.Header()
	body, enc, etag := pick(f, r.Header.Get("Accept-Encoding"))
	hd.Set("Content-Type", f.ctype)
	hd.Set("Cache-Control", "no-cache")
	hd.Add("Vary", "Accept-Encoding")
	h.shellHeaders(hd)
	if enc != "" {
		hd.Set("Content-Encoding", enc)
	}
	if status == http.StatusOK {
		hd.Set("ETag", etag)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
		h.observe(KindShell, enc)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status) // same body, real 404 status: the SPA renders its NotFound page
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
	h.observe(KindShell404, enc)
}

func acceptsEnc(header, enc string) bool {
	star := -1.0
	got := -1.0
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		q := 1.0
		if k, v, ok := strings.Cut(strings.TrimSpace(params), "="); ok && strings.EqualFold(strings.TrimSpace(k), "q") {
			x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || x < 0 || x > 1 {
				x = 0
			}
			q = x
		}
		switch name {
		case enc:
			got = q
		case "*":
			star = q
		}
	}
	if got >= 0 {
		return got > 0
	}
	return star > 0
}
