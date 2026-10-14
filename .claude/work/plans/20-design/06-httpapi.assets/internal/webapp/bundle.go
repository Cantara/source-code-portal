package webapp

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// file is one embedded file with its precompressed siblings. Everything is read once, at New.
type file struct {
	name         string // "assets/index-AbCd1234.js"
	ctype        string
	body, br, gz []byte
	etag         string // strong, content hash
	immutable    bool
}

// contentTypes is the allow-list of extensions in dist. mime.TypeByExtension is NOT used: it reads the OS
// mime database, which a distroless image does not have and which differs between machines.
func contentType(ext string) (string, bool) {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8", true
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8", true
	case ".css":
		return "text/css; charset=utf-8", true
	case ".json":
		return "application/json", true
	case ".webmanifest":
		return "application/manifest+json", true
	case ".svg":
		return "image/svg+xml", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".avif":
		return "image/avif", true
	case ".ico":
		return "image/x-icon", true
	case ".woff2":
		return "font/woff2", true
	case ".woff":
		return "font/woff", true
	case ".txt":
		return "text/plain; charset=utf-8", true
	}
	return "", false
}

func isCompressible(ext string) bool {
	switch ext {
	case ".html", ".js", ".mjs", ".css", ".json", ".webmanifest", ".svg", ".txt", ".ico":
		return true
	}
	return false
}

// A hashed asset's name is <name>-<hash>.<ext> where <hash> is exactly hashLen characters of the base64url alphabet
// (Vite/Rolldown default `[hash:8]`; the web build pins assetFileNames so the length is a contract). The hash may itself
// contain '-' and '_', so it is the LAST hashLen characters before the extension, preceded by a '-' (found by running
// the first draft against `index-Ab-d1234.js`, which it rejected: a build with ~20 assets would fail to start 9 times in 10).
const hashLen = 8

func hashedName(name string) bool {
	base := path.Base(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	i := len(stem) - hashLen - 1
	if i < 1 || stem[i] != '-' { // at least one character of name before the dash
		return false
	}
	for _, c := range stem[i+1:] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

const (
	maxFileBytes  = 8 << 20
	maxTotalBytes = 16 << 20
)

type bundle struct {
	files    map[string]*file
	shell    *file
	manifest *manifest
}

func loadBundle(fsys fs.FS, o Options) (*bundle, error) {
	b := &bundle{files: map[string]*file{}}
	sib := map[string][]byte{}
	var total int
	var problems []error
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			problems = append(problems, fmt.Errorf("%s: symlinks are not allowed in dist", p))
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > maxFileBytes {
			problems = append(problems, fmt.Errorf("%s: %d bytes exceeds the %d byte file limit", p, len(data), maxFileBytes))
		}
		if strings.HasPrefix(p, "api/") {
			problems = append(problems, fmt.Errorf("%s: a file under /api/ would shadow the API", p))
		}
		switch {
		case strings.HasSuffix(p, ".map"):
			problems = append(problems, fmt.Errorf("%s: source maps must not be embedded", p))
		case strings.HasSuffix(p, ".br") || strings.HasSuffix(p, ".gz"):
			sib[p] = data
		default:
			ext := strings.ToLower(path.Ext(p))
			ct, ok := contentType(ext)
			if !ok {
				problems = append(problems, fmt.Errorf("%s: extension %q is not in the allow-list", p, ext))
				return nil
			}
			if ext == ".html" && p != "index.html" {
				problems = append(problems, fmt.Errorf("%s: only index.html may be an HTML document (it carries the CSP)", p))
			}
			if strings.HasPrefix(p, "assets/") && !hashedName(p) {
				problems = append(problems, fmt.Errorf("%s: files under assets/ must carry a content hash", p))
			}
			sum := sha256.Sum256(data)
			b.files[p] = &file{name: p, ctype: ct, body: data, etag: `"` + base64.RawURLEncoding.EncodeToString(sum[:15]) + `"`, immutable: strings.HasPrefix(p, "assets/")}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("webapp: walk dist: %w", err)
	}
	if total > maxTotalBytes {
		problems = append(problems, fmt.Errorf("dist is %d bytes, over the %d byte limit", total, maxTotalBytes))
	}
	names := make([]string, 0, len(b.files))
	for n := range b.files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := b.files[n]
		ext := strings.ToLower(path.Ext(n))
		if brData, ok := sib[n+".br"]; ok && len(brData) < len(f.body) {
			f.br = brData
		}
		if gzData, ok := sib[n+".gz"]; ok && len(gzData) < len(f.body) {
			if err := verifyGzip(gzData, f.body); err != nil {
				problems = append(problems, fmt.Errorf("%s.gz: %w", n, err))
			} else {
				f.gz = gzData
			}
		}
		if o.RequirePrecompressed && isCompressible(ext) && len(f.body) >= 1024 && f.br == nil {
			problems = append(problems, fmt.Errorf("%s: no usable .br sibling (release builds must precompress)", n))
		}
	}
	for n := range sib {
		base := strings.TrimSuffix(strings.TrimSuffix(n, ".br"), ".gz")
		if _, ok := b.files[base]; !ok {
			problems = append(problems, fmt.Errorf("%s: orphan compressed file", n))
		}
	}
	b.shell = b.files["index.html"]
	if b.shell == nil {
		problems = append(problems, errors.New("index.html is missing"))
	}
	if rj, ok := b.files["routes.json"]; !ok {
		problems = append(problems, errors.New("routes.json is missing"))
	} else if m, err := parseManifest(rj.body); err != nil {
		problems = append(problems, err)
	} else {
		b.manifest = m
		if o.CheckBuild && m.BuildID != o.BuildID {
			problems = append(problems, fmt.Errorf("routes.json buildId %q does not match the binary's build id %q (stale dist?)", m.BuildID, o.BuildID))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("webapp: invalid dist: %w", errors.Join(problems...))
	}
	delete(b.files, "routes.json") // build metadata, never served
	return b, nil
}

func verifyGzip(gz, want []byte) error {
	zr, err := gzip.NewReader(strings.NewReader(string(gz)))
	if err != nil {
		return err
	}
	got, err := io.ReadAll(io.LimitReader(zr, int64(len(want))+1))
	if err != nil {
		return err
	}
	if string(got) != string(want) {
		return errors.New("does not decompress to the original")
	}
	return nil
}
