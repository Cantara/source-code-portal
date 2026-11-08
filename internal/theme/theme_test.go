package theme

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func files(t *testing.T, content map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range content {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidate(t *testing.T) {
	good := files(t, map[string]string{CSSFile: ":root{--primary:red}", LogoFile: "<svg/>"})
	for name, tc := range map[string]struct {
		dir, name string
		bad       string // a part of the problem, empty: none
	}{
		"nothing set":        {},
		"a directory":        {dir: good, name: "Acme Portal"},
		"no such directory":  {dir: filepath.Join(good, "gone"), bad: "not a directory"},
		"a file for a dir":   {dir: filepath.Join(good, CSSFile), bad: "not a directory"},
		"a long name":        {name: strings.Repeat("x", 61), bad: "product_name"},
		"a control in name":  {name: "Acme\nPortal", bad: "product_name"},
		"a css over 128 KiB": {dir: files(t, map[string]string{CSSFile: strings.Repeat("a", 129<<10)}), bad: "theme.css"},
		"a css not UTF-8":    {dir: files(t, map[string]string{CSSFile: "\xff\xfe"}), bad: "UTF-8"},
	} {
		t.Run(name, func(t *testing.T) {
			p := strings.Join(Validate(tc.dir, tc.name), "; ")
			if (tc.bad == "") != (p == "") || !strings.Contains(p, tc.bad) {
				t.Errorf("problems = %q, want one with %q", p, tc.bad)
			}
		})
	}
	// A stylesheet that is a link out of the directory is refused, not followed.
	out := files(t, map[string]string{"secret.css": "x"})
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(out, "secret.css"), filepath.Join(dir, CSSFile)); err != nil {
		t.Skip("no symbolic links:", err)
	}
	if p := Validate(dir, ""); len(p) == 0 {
		t.Error("a symbolic link out of the directory was accepted")
	}
}

func TestLoadAddressesCarryTheContent(t *testing.T) {
	dir := files(t, map[string]string{CSSFile: "a{}", FaviconFile: "<svg/>"})
	one, err := Load(dir, "  Acme Portal ")
	if err != nil {
		t.Fatal(err)
	}
	if one.Name != "Acme Portal" || one.Logo != "" || !strings.HasPrefix(one.CSS, "/theme/theme.css?v=") || !strings.HasPrefix(one.Favicon, "/theme/favicon.svg?v=") {
		t.Fatalf("theme = %+v", one)
	}
	if err := os.WriteFile(filepath.Join(dir, CSSFile), []byte("b{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	two, _ := Load(dir, "Acme Portal")
	if two.CSS == one.CSS || two.Favicon != one.Favicon {
		t.Errorf("a changed stylesheet is a new address, an unchanged favicon is the same: %s %s", one.CSS, two.CSS)
	}
	if env := one.Env(); !strings.Contains(env, `"name":"Acme Portal"`) || !strings.Contains(env, `"css":"/theme/theme.css?v=`) || strings.Contains(env, "logo") {
		t.Errorf("env = %s", env)
	}
	if empty, _ := Load("", ""); empty.Env() != "" {
		t.Errorf("a portal without a theme tells the web UI nothing: %q", empty.Env())
	}
	if _, err := Load(filepath.Join(dir, "gone"), ""); err == nil {
		t.Error("a missing directory loaded")
	}
}

func serve(t *testing.T, dir string) *http.ServeMux {
	t.Helper()
	th, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+Prefix+"{file...}", th.Handler())
	return mux
}

func get(mux http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandlerServesPicturesAndStylesOnly(t *testing.T) {
	out := files(t, map[string]string{"outside.css": "secret"})
	dir := files(t, map[string]string{
		CSSFile: ":root{--primary:red}", LogoFile: "<svg/>", "img/mark.png": "png", ".hidden.css": "h", "run.js": "alert(1)", "notes.txt": "t",
	})
	if err := os.Symlink(filepath.Join(out, "outside.css"), filepath.Join(dir, "link.css")); err != nil {
		t.Skip("no symbolic links:", err)
	}
	mux := serve(t, dir)
	for target, want := range map[string]int{
		"/theme/theme.css":                 200,
		"/theme/theme.css?v=abc":           200,
		"/theme/logo.svg":                  200,
		"/theme/img/mark.png":              200,
		"/theme/":                          404, // no listing
		"/theme/img/":                      404,
		"/theme/img":                       404,
		"/theme/.hidden.css":               404,
		"/theme/run.js":                    404, // no script
		"/theme/notes.txt":                 404,
		"/theme/missing.css":               404,
		"/theme/link.css":                  404, // a link out of the directory
		"/theme/%2e%2e/outside.css":        404,
		"/theme/..%2foutside.css":          404,
		"/theme/img/..%2f..%2foutside.css": 404,
	} {
		if got := get(mux, target).Code; got != want {
			t.Errorf("GET %s = %d, want %d", target, got, want)
		}
	}
	rec := get(mux, "/theme/theme.css")
	if ct := rec.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" || rec.Header().Get("Cache-Control") != "no-cache" || rec.Body.String() != ":root{--primary:red}" {
		t.Errorf("css answer: %q %q %q", ct, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	if ct := get(mux, "/theme/logo.svg").Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("svg content type %q", ct)
	}
	// Traversal given straight to the handler, as the router would never hand it.
	req := httptest.NewRequest(http.MethodGet, "/theme/x", nil)
	req.SetPathValue("file", "../outside.css")
	rec = httptest.NewRecorder()
	th, _ := Load(dir, "")
	th.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("a parent path = %d", rec.Code)
	}
}
