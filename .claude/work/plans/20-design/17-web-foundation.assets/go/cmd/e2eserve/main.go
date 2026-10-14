// Throwaway e2e server for the web-foundation prototype: the real webapp handler over a built dist plus fixture API.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cantara/source-code-portal/internal/webapp"
)

func main() {
	dist := flag.String("dist", "../web/dist", "built SPA")
	fixtures := flag.String("fixtures", "../web/e2e/fixtures/api", "api fixtures dir")
	addr := flag.String("addr", "127.0.0.1:9090", "listen")
	csp := flag.String("csp", "enforce", "report_only|enforce_core|enforce")
	build := flag.String("build", "abc123def456", "build id")
	admin := flag.Bool("admin", false, "serve the admin listener shape")
	flag.Parse()

	h, err := webapp.New(os.DirFS(*dist), webapp.Options{
		BuildID: *build, CheckBuild: true, RequirePrecompressed: true, CSP: webapp.CSPMode(*csp),
		Reserved: []string{"/.well-known/", "/_frame/", "/api/", "/assets/", "/badges/", "/media/", "/schemas/", "/webhooks/"},
	})
	if err != nil {
		log.Fatal(err)
	}
	spa := http.Handler(h)
	if *admin {
		spa = h.For(webapp.Admin)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /csp-reports", h.CSPReports())
	mux.HandleFunc("GET /api/v1/{name...}", func(w http.ResponseWriter, r *http.Request) {
		name := strings.ReplaceAll(r.PathValue("name"), "/", "_")
		b, err := os.ReadFile(filepath.Join(*fixtures, name+".json"))
		if err != nil {
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("X-SCP-Build", *build)
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"type":"urn:scp:problem:not-found","title":"Not found","status":404,"code":"not-found","instance":"urn:uuid:0e6c1d5a-7a3b-4c52-9f31-5d2f0f6a8b11"}`))
			return
		}
		sum := sha256.Sum256(b)
		etag := `W/"` + hex.EncodeToString(sum[:6]) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-SCP-Build", *build)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /badges/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="90" height="20"><rect width="90" height="20" fill="#1a7f37"/><text x="6" y="14" fill="#fff" font-size="11">badge</text></svg>`))
	})
	mux.Handle("/", spa)
	log.Printf("listening on %s (csp=%s, build=%s)", *addr, *csp, *build)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
