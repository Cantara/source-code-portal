package apitest_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/apispec"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/apitest"
)

func TestBundledSpecLintsClean(t *testing.T) {
	for name, b := range map[string][]byte{"public": apispec.Public(), "admin": apispec.Admin()} {
		if v := apitest.Lint(apitest.Load(t, b)); len(v) > 0 {
			t.Errorf("%s: %s", name, strings.Join(v, "\n"))
		}
	}
}

func TestLintCatchesViolations(t *testing.T) {
	bad := `
openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /api/v1/things/:
    get:
      operationId: Get_things
      tags: [a, b]
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {snake_case: {type: string, nullable: true, enum: [Bad_Value]}}}
`
	v := apitest.Lint(apitest.Load(t, []byte(bad)))
	want := []string{"L1", "L2", "L3", "L4", "L5", "L6"}
	got := strings.Join(v, "\n")
	for _, id := range want {
		if !strings.Contains(got, id+" ") {
			t.Errorf("rule %s did not fire:\n%s", id, got)
		}
	}
}

func TestProblemBodyMatchesSchema(t *testing.T) {
	doc := apitest.Load(t, apispec.Public())
	for _, p := range []httpapi.Problem{
		httpapi.NotFound("x"), httpapi.Validation(httpapi.FieldError{Pointer: "#/query/limit", Detail: "d"}),
		httpapi.RateLimited(3e9), httpapi.Internal(), httpapi.MethodNotAllowed("GET"),
	} {
		req := httptest.NewRequest("GET", "/api/v1/commits", nil)
		rec := httptest.NewRecorder()
		httpapi.WriteProblem(rec, req, p)
		apitest.ValidateResponse(t, doc, req, rec)
	}
}

func TestETagCoversBodyProperty(t *testing.T) {
	version := 1
	status := "fresh"
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", httpapi.WithDigest(httpapi.WeakETag(version, "public"), httpapi.FreshnessDigest(status)))
		w.Write([]byte(`{"v":` + string(rune('0'+version)) + `,"status":"` + status + `"}`))
	})
	call := func() (string, []byte) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
		return rec.Header().Get("ETag"), rec.Body.Bytes()
	}
	apitest.ETagCoversBody(t, call, func() { status = "stale" }) // time passing flips derived status without a new snapshot
}

type recT struct {
	testing.TB
	failed bool
}

func (r *recT) Errorf(string, ...any) { r.failed = true }
func (r *recT) Helper()               {}

func TestValidateResponseRejectsCORSHeaders(t *testing.T) {
	doc := apitest.Load(t, apispec.Public())
	req := httptest.NewRequest("GET", "/api/v1/commits", nil)
	rec := httptest.NewRecorder()
	httpapi.WriteProblem(rec, req, httpapi.NotFound("x"))
	rec.Header().Set("Access-Control-Allow-Origin", "*")
	rt := &recT{TB: t}
	apitest.ValidateResponse(rt, doc, req, rec)
	if !rt.failed {
		t.Fatal("a response with Access-Control-Allow-Origin passed ValidateResponse (GR-18)")
	}
}
