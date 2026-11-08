// Package theme is what makes a deployment look like its owner's without rebuilding the portal (design/theme.md, P-108): a directory
// of files the portal serves under /theme/ (a stylesheet that sets the colour tokens, a logo, a favicon) and a product name, both
// read at the start. The pages get their addresses, with the content hash in each, from the environment of the web UI server.
package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The files a theme knows by name; each is optional.
const (
	CSSFile     = "theme.css"
	LogoFile    = "logo.svg"
	FaviconFile = "favicon.svg"
)

const (
	maxCSS   = 128 << 10 // a stylesheet
	maxAsset = 1 << 20   // any other file served
	maxName  = 60        // the product name, in characters
	// Prefix is where the files are served.
	Prefix = "/theme/"
)

// types are the extensions served, with their media types: stylesheets and pictures, no script, no font.
var types = map[string]string{ //nolint:gochecknoglobals // lookup table
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".ico":  "image/x-icon",
}

// Theme is a deployment's look: the addresses of its files (empty when the file is not there) and its product name.
type Theme struct {
	dir                string
	Name               string
	CSS, Logo, Favicon string
}

// Validate lists what is wrong with the theme directory dir (empty: no theme files) and the product name name.
func Validate(dir, name string) []string {
	var p []string
	if n := strings.TrimSpace(name); utf8.RuneCountInString(n) > maxName || strings.ContainsFunc(n, unicode.IsControl) {
		p = append(p, fmt.Sprintf("web.product_name: at most %d characters and no control characters", maxName))
	}
	if dir == "" {
		return p
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return append(p, fmt.Sprintf("web.theme_dir: %q is not a directory", dir))
	}
	for _, f := range []string{CSSFile, LogoFile, FaviconFile} {
		b, err := read(dir, f)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			p = append(p, fmt.Sprintf("web.theme_dir: %s: %v", f, err))
		case f == CSSFile && !utf8.Valid(b):
			p = append(p, fmt.Sprintf("web.theme_dir: %s is not UTF-8", f))
		}
	}
	return p
}

// read returns the file f of dir, which must be a regular file within its size limit.
func read(dir, f string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	limit := int64(maxAsset)
	if f == CSSFile {
		limit = maxCSS
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("not a regular file of at most %d KiB", limit>>10)
	}
	return io.ReadAll(io.LimitReader(file, limit))
}

// Load reads the theme at the start: the product name and, when dir is set, the three known files, each address carrying the
// content's hash so a changed file is a new address.
func Load(dir, name string) (*Theme, error) {
	if p := Validate(dir, name); len(p) > 0 {
		return nil, errors.New(strings.Join(p, "; "))
	}
	t := &Theme{dir: dir, Name: strings.TrimSpace(name)}
	if dir == "" {
		return t, nil
	}
	for f, to := range map[string]*string{CSSFile: &t.CSS, LogoFile: &t.Logo, FaviconFile: &t.Favicon} {
		b, err := read(dir, f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		*to = Prefix + f + "?v=" + hex.EncodeToString(sum[:6])
	}
	return t, nil
}

// Env is what the web UI server is told (SCP_THEME): the name and the addresses as JSON; empty when there is nothing to tell.
func (t *Theme) Env() string {
	if t.Name == "" && t.CSS == "" && t.Logo == "" && t.Favicon == "" {
		return ""
	}
	b, _ := json.Marshal(struct {
		Name    string `json:"name,omitempty"`
		CSS     string `json:"css,omitempty"`
		Logo    string `json:"logo,omitempty"`
		Favicon string `json:"favicon,omitempty"`
	}{t.Name, t.CSS, t.Logo, t.Favicon})
	return string(b)
}

// Handler serves GET /theme/{file...}: the regular files of the directory with an allowed extension, never a listing, a dotfile or
// anything outside the directory (the directory is opened as a root, which a path or a symbolic link cannot leave). The files are not
// cached without asking: the addresses the pages use change with the content.
func (t *Theme) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		mediaType, ok := types[strings.ToLower(path.Ext(name))]
		if !ok || !fs.ValidPath(name) || strings.HasPrefix(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		root, err := os.OpenRoot(t.dir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = root.Close() }()
		f, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxAsset {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, name, info.ModTime(), f)
	})
}
