// Package httpapi builds the HTTP handlers of the two listeners: the public port (web UI, API, badges, LLM surface, webhook)
// and the admin port (probes, metrics, profiling, status).
package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Public returns the handler of the public listener. spa holds the web UI build with index.html at its root.
func Public(spa fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", spaHandler(spa))
	return mux
}

// Admin returns the handler of the admin listener.
func Admin() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// spaHandler serves the files of the web UI build. Files under assets/ carry a content hash in their name and are cached for a
// year; everything else is revalidated on each use. The root serves index.html.
func spaHandler(spa fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(spa, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, spa, name) //nolint:gosec // G703: fs.FS rejects names that leave its root (fs.ValidPath)
	})
}
