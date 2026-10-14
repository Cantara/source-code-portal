package docs

import (
	"net/http"

	"github.com/cantara/source-code-portal/internal/docs/schema"
	"github.com/cantara/source-code-portal/internal/httpapi"
)

func absoluteURL(base, p string) (string, error) { return routesAbsolute(base, p) }

func schemasHandler() http.Handler {
	files := schema.Files()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.PathValue("file")]
		if !ok {
			httpapi.WriteProblem(w, r, httpapi.NotFound("schema not found"))
			return
		}
		httpapi.Static("application/schema+json", b).ServeHTTP(w, r)
	})
}
