// Package webapp serves the embedded single-page application.
package webapp

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// Options configures the handler.
type Options struct {
	// APIPrefixes never receive the history fallback; unknown paths under them are real 404s.
	APIPrefixes []string
	// Build identifies the running build; sent as X-SCP-Build so a stale tab can detect a deploy.
	Build string
}

// Handler serves files from root with SPA history fallback.
func Handler(root fs.FS, o Options) http.Handler {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		panic("webapp: index.html missing in embedded FS: " + err.Error())
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("X-SCP-Build", o.Build)
		p := path.Clean(r.URL.Path)
		for _, pre := range o.APIPrefixes {
			if p == pre || strings.HasPrefix(p, pre+"/") {
				http.NotFound(w, r)
				return
			}
		}
		name := strings.TrimPrefix(p, "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(root, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				if serveCompressed(w, r, root, name) {
					return
				}
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" { // missing asset: real 404, not HTML
				http.NotFound(w, r)
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}

func serveCompressed(w http.ResponseWriter, r *http.Request, root fs.FS, name string) bool {
	ae := r.Header.Get("Accept-Encoding")
	w.Header().Add("Vary", "Accept-Encoding")
	for _, enc := range []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if !strings.Contains(ae, enc.token) {
			continue
		}
		f, err := root.Open(name + enc.ext)
		if err != nil {
			continue
		}
		defer f.Close()
		w.Header().Set("Content-Encoding", enc.token)
		if ct := contentType(name); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if rs, ok := f.(io.ReadSeeker); ok {
			http.ServeContent(w, r, name, time.Time{}, rs)
		} else {
			_, _ = io.Copy(w, f)
		}
		return true
	}
	return false
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json":
		return "application/json"
	}
	return ""
}
