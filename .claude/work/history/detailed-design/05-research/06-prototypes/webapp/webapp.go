// Package webapp serves the embedded SPA. It knows nothing about features.
package webapp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const noncePlaceholder = "__CSP_NONCE__"

type asset struct {
	body     []byte
	br, gz   []byte
	etag     string
	ctype    string
	hashed   bool // filename carries a content hash -> immutable
}

type Handler struct {
	files map[string]*asset // key: "assets/index-XXXX.js"
	index []byte            // index.html template with placeholder
	csp   func(nonce string) string
	mod   time.Time
}

func New(dist fs.FS, csp func(nonce string) string) (*Handler, error) {
	h := &Handler{files: map[string]*asset{}, csp: csp, mod: time.Now()}
	err := fs.WalkDir(dist, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".br") || strings.HasSuffix(p, ".gz") {
			return err
		}
		b, _ := fs.ReadFile(dist, p)
		if p == "index.html" {
			h.index = b
			return nil
		}
		a := &asset{body: b, ctype: ctypeOf(p), hashed: strings.HasPrefix(p, "assets/")}
		sum := sha256.Sum256(b)
		a.etag = `"` + hex.EncodeToString(sum[:12]) + `"`
		a.br, _ = fs.ReadFile(dist, p+".br")
		a.gz, _ = fs.ReadFile(dist, p+".gz")
		h.files[p] = a
		return nil
	})
	return h, err
}

func ctypeOf(p string) string {
	switch path.Ext(p) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".json", ".webmanifest":
		return "application/json"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	}
	return "application/octet-stream"
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if a, ok := h.files[p]; ok { // exact file
		h.serveAsset(w, r, a)
		return
	}
	// History fallback ONLY for navigations: GET/HEAD, no file extension, client accepts HTML.
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		path.Ext(p) == "" && strings.Contains(r.Header.Get("Accept"), "text/html") &&
		!strings.HasPrefix(p, "assets/") {
		h.serveIndex(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, a *asset) {
	hd := w.Header()
	hd.Set("Content-Type", a.ctype)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Add("Vary", "Accept-Encoding")
	if a.hashed {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hd.Set("Cache-Control", "no-cache")
	}
	body := a.body
	ae := r.Header.Get("Accept-Encoding")
	switch {
	case a.br != nil && acceptsEnc(ae, "br"):
		hd.Set("Content-Encoding", "br")
		body = a.br
		hd.Set("ETag", strings.TrimSuffix(a.etag, `"`)+`-br"`)
	case a.gz != nil && acceptsEnc(ae, "gzip"):
		hd.Set("Content-Encoding", "gzip")
		body = a.gz
		hd.Set("ETag", strings.TrimSuffix(a.etag, `"`)+`-gz"`)
	default:
		hd.Set("ETag", a.etag)
	}
	// ServeContent handles If-None-Match/If-Modified-Since/Range/HEAD. Content-Type already set.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	var nb [18]byte
	_, _ = rand.Read(nb[:])
	nonce := base64.RawStdEncoding.EncodeToString(nb[:])
	body := bytes.ReplaceAll(h.index, []byte(noncePlaceholder), []byte(nonce))
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Cache-Control", "no-cache") // per-response nonce => no ETag on purpose
	hd.Set("Content-Security-Policy", h.csp(nonce))
	hd.Set("Content-Length", itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(appendInt(nil, n))) }
func appendInt(b []byte, n int) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var t [20]byte
	i := len(t)
	for n > 0 {
		i--
		t[i] = byte('0' + n%10)
		n /= 10
	}
	return append(b, t[i:]...)
}

// acceptsEnc: minimal Accept-Encoding parse honouring q=0.
func acceptsEnc(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), enc) {
			return !strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0") || strings.Contains(strings.ReplaceAll(params, " ", ""), "q=0.")
		}
	}
	return false
}
