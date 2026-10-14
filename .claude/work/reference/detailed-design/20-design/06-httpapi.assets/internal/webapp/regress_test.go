package webapp_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cantara/source-code-portal/internal/webapp"
)

func tryNew(t *testing.T, d fstest.MapFS) (*webapp.Handler, error) {
	t.Helper()
	return webapp.New(d, webapp.Options{BuildID: "b1", CheckBuild: true, Reserved: []string{"/api/", "/badges/", "/media/", "/webhooks/"}})
}

// Verification (V1): Vite/Rolldown hashes are base64url, so they can contain '-' and '_'. The first draft took the text
// after the LAST '-' as the hash and rejected ~12 % of real asset names, so a 20-asset build almost never started.
func TestHashedAssetNamesFromViteAreAccepted(t *testing.T) {
	for _, name := range []string{"assets/index-AbCd1234.js", "assets/index-Ab-d1234.js", "assets/index-_bCd123-.js", "assets/vendor-react-Zx9_K-qa.js", "assets/style-ab-cd123.css", "assets/a-b-12345678.woff2"} {
		d := dist(t)
		d[name] = &fstest.MapFile{Data: []byte("x")}
		if name == "assets/style-ab-cd123.css" {
			d[name] = &fstest.MapFile{Data: []byte("a{}")}
		}
		if _, err := tryNew(t, d); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	for _, name := range []string{"assets/app.js", "assets/my-long-name.js", "assets/index-1234567.js", "assets/-12345678.js"} {
		d := dist(t)
		d[name] = &fstest.MapFile{Data: []byte("x")}
		if _, err := tryNew(t, d); err == nil || !strings.Contains(err.Error(), "content hash") {
			t.Errorf("%s accepted, want 'must carry a content hash': %v", name, err)
		}
	}
}

// Verification (V30): a bundle file is served under its exact name only (no "/theme-init.js/").
func TestFileServedUnderExactNameOnly(t *testing.T) {
	h := newH(t, nil, dist(t))
	if w := get(h, "GET", "/theme-init.js/"); w.Code != 404 {
		t.Errorf("trailing slash: %d", w.Code)
	}
	if w := get(h, "GET", "/theme-init.js"); w.Code != 200 {
		t.Errorf("exact: %d", w.Code)
	}
}
