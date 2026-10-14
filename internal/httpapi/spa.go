package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// reserved prefixes never fall back to the shell.
var reserved = []string{"/api/", "/assets/", "/badges/", "/webhooks/", "/.well-known/"} //nolint:gochecknoglobals // lookup table

// spaHandler serves the files of the web UI build. Files under assets/ carry a content hash in their name and are cached for a
// year; everything else is revalidated on each use. Paths without a file get the shell: 200 for the shapes of the UI's routes,
// 404 for other shapes, and a plain 404 under a reserved prefix or for a name with an extension.
func spaHandler(spa fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(spa, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if path.Ext(name) == ".html" {
				shellHeaders(w)
			}
			http.ServeFileFS(w, r, spa, name) //nolint:gosec // G703: fs.FS rejects names that leave its root (fs.ValidPath)
			return
		}
		p := r.URL.Path
		switch {
		case shellShape(p):
			shell(w, r, spa, http.StatusOK)
		case hasReservedPrefix(p) || strings.Contains(path.Base(p), "."):
			http.NotFound(w, r)
		default:
			shell(w, r, spa, http.StatusNotFound)
		}
	})
}

func hasReservedPrefix(p string) bool {
	for _, pre := range reserved {
		if strings.HasPrefix(p+"/", pre) {
			return true
		}
	}
	return false
}

// shellShape reports whether p is the address of a page of the UI: /, /commits, /groups/{g}[/commits], /repos/{o}/{n}[/commits],
// /repos/{o}/{n}/docs/{path...}.
func shellShape(p string) bool {
	seg := strings.Split(strings.Trim(p, "/"), "/")
	for _, s := range seg {
		if s == "" && len(seg) > 1 {
			return false
		}
	}
	switch {
	case len(seg) == 1:
		return seg[0] == "" || seg[0] == "commits"
	case seg[0] == "groups":
		return len(seg) == 2 || len(seg) == 3 && seg[2] == "commits"
	case seg[0] == "repos":
		return len(seg) == 3 || len(seg) == 4 && seg[3] == "commits" || len(seg) >= 5 && seg[3] == "docs"
	}
	return false
}

// shell writes index.html with status.
func shell(w http.ResponseWriter, r *http.Request, spa fs.FS, status int) {
	b, err := fs.ReadFile(spa, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	shellHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

func shellHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", cspShell)
	w.Header().Set("Permissions-Policy", permissionsShell)
}
