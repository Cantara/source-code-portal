package web_test

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/webapp"
	"github.com/cantara/source-code-portal/web"
)

func TestEmbeddedBundleLoads(t *testing.T) {
	fsys, err := web.FS()
	if err != nil {
		t.Fatal(err)
	}
	// In the placeholder build CheckBuild is off (its buildId is "placeholder"); with -tags spa CI sets it on.
	if _, err := webapp.New(fsys, webapp.Options{Reserved: []string{"/api/"}, RequirePrecompressed: web.EmbeddedMode == web.ModeSPA, CheckBuild: false}); err != nil {
		t.Fatal(err)
	}
}
