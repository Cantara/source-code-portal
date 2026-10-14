package httpapi

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// GzipOptions configures Gzip. Zero values mean: MinSize 1024, Level gzip.DefaultCompression.
type GzipOptions struct {
	MinSize int
	Level   int
}

// Gzip compresses dynamic responses (JSON, problem+json, YAML, Markdown, text, HTML, XML, +json/+xml types)
// of at least MinSize bytes for clients that accept gzip. It never touches responses that already carry
// Content-Encoding (precompressed static files), ranges, HEAD, 1xx/204/206/304, images and event streams.
// A strong ETag of a compressed response is weakened (RFC 9110 8.8.1); If-None-Match uses weak comparison,
// so conditional requests keep working.
func Gzip(o GzipOptions) func(http.Handler) http.Handler {
	if o.MinSize <= 0 {
		o.MinSize = 1024
	}
	if o.Level == 0 {
		o.Level = gzip.DefaultCompression
	}
	pool := &sync.Pool{New: func() any { zw, _ := gzip.NewWriterLevel(nil, o.Level); return zw }}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" || !AcceptsEncoding(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Add("Vary", "Accept-Encoding")
				next.ServeHTTP(w, r)
				return
			}
			gw := &gzWriter{ResponseWriter: w, o: o, pool: pool}
			defer gw.finish()
			next.ServeHTTP(gw, r)
		})
	}
}

type gzState uint8

const (
	gzUndecided gzState = iota
	gzPass
	gzCompress
)

type gzWriter struct {
	http.ResponseWriter
	o      GzipOptions
	pool   *sync.Pool
	state  gzState
	status int
	buf    bytes.Buffer
	zw     *gzip.Writer
}

func (g *gzWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func compressibleType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	switch {
	case strings.HasPrefix(mt, "image/"), mt == "text/event-stream":
		return false
	case strings.HasPrefix(mt, "text/"), mt == "application/json", mt == "application/yaml", mt == "application/xml",
		strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"):
		return true
	}
	return false
}

func (g *gzWriter) WriteHeader(code int) {
	if g.status != 0 {
		return
	}
	g.status = code
	h := g.Header()
	h.Add("Vary", "Accept-Encoding")
	switch {
	case code < 200 || code == 204 || code == 206 || code == 304,
		h.Get("Content-Encoding") != "", !compressibleType(h.Get("Content-Type")):
		g.state = gzPass
		g.ResponseWriter.WriteHeader(code)
		return
	}
	if cl := h.Get("Content-Length"); cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil || n < g.o.MinSize {
			g.state = gzPass
			g.ResponseWriter.WriteHeader(code)
			return
		}
		g.start()
	}
}

// start switches to compression and sends the header.
func (g *gzWriter) start() {
	h := g.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	if e := h.Get("ETag"); e != "" && !strings.HasPrefix(e, "W/") {
		h.Set("ETag", "W/"+e)
	}
	g.ResponseWriter.WriteHeader(g.status)
	g.zw = g.pool.Get().(*gzip.Writer)
	g.zw.Reset(g.ResponseWriter)
	g.state = gzCompress
	if g.buf.Len() > 0 {
		_, _ = g.zw.Write(g.buf.Bytes())
		g.buf.Reset()
	}
}

func (g *gzWriter) Write(p []byte) (int, error) {
	if g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	switch g.state {
	case gzPass:
		return g.ResponseWriter.Write(p)
	case gzCompress:
		return g.zw.Write(p)
	}
	g.buf.Write(p)
	if g.buf.Len() >= g.o.MinSize {
		g.start()
	}
	return len(p), nil
}

func (g *gzWriter) Flush() {
	if g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	if g.state == gzUndecided {
		g.start() // streaming response of unknown length: compress
	}
	if g.state == gzCompress {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzWriter) finish() {
	switch g.state {
	case gzCompress:
		_ = g.zw.Close()
		g.pool.Put(g.zw)
	case gzUndecided:
		if g.status == 0 {
			return // handler wrote nothing
		}
		g.Header().Set("Content-Length", strconv.Itoa(g.buf.Len()))
		g.ResponseWriter.WriteHeader(g.status)
		_, _ = g.ResponseWriter.Write(g.buf.Bytes())
	}
}
