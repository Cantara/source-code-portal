package apitest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// ValidateResponse checks one recorded exchange against doc: the status must be declared (or covered by
// `default`), headers must match and the JSON body must validate (problem+json against the Problem schema).
func ValidateResponse(t testing.TB, doc *openapi3.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	for k := range rec.Header() { // GR-18: no CORS machinery exists, so no response may carry it
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("%s %s carries %s: there is no CORS in this product", req.Method, req.URL.Path, k)
		}
	}
	d := *doc
	d.Servers = nil
	router, err := gorillamux.NewRouter(&d)
	if err != nil {
		t.Fatal(err)
	}
	lookup := req
	if req.Method == http.MethodHead {
		cp := *req
		cp.Method = http.MethodGet
		lookup = &cp
	}
	route, params, err := router.FindRoute(lookup)
	if err != nil { // unknown path or method: the only valid answer is a Problem
		ValidateProblem(t, doc, rec)
		return
	}
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Encoding") == "gzip" { // validate the representation, not the transfer coding
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("gzip response is not gzip: %v", err)
		}
		body, _ = io.ReadAll(zr)
		res.Header = res.Header.Clone()
		res.Header.Del("Content-Encoding")
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: lookup, PathParams: params, Route: route},
		Status:                 res.StatusCode, Header: res.Header,
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	in.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(req.Context(), in); err != nil {
		t.Errorf("%s %s -> %d violates the contract: %v", req.Method, req.URL.RequestURI(), res.StatusCode, err)
	}
}

// ETagCoversBody is the property "if the body changes, the ETag changes". call performs one GET against the
// handler under test; mutate changes whatever the response depends on (publishes a snapshot, advances the
// fake clock past a TTL, ...). Both the "unchanged" case (same ETag, same body) and the "changed" case run.
func ETagCoversBody(t testing.TB, call func() (etag string, body []byte), mutate func()) {
	t.Helper()
	e1, b1 := call()
	e1b, b1b := call()
	if e1 != e1b || !bytes.Equal(b1, b1b) {
		t.Fatalf("response is not stable without a change: %q vs %q", e1, e1b)
	}
	mutate()
	e2, b2 := call()
	if !bytes.Equal(b1, b2) && e1 == e2 {
		t.Fatalf("body changed but ETag %q did not: a client would get a stale 304", e1)
	}
}

var instanceRE = regexp.MustCompile(`"instance":"[^"]*"`)

// Normalised removes the only per-request member of a problem body so two problems can be compared byte for byte.
func Normalised(body []byte) string {
	return instanceRE.ReplaceAllString(string(body), `"instance":"-"`)
}

// NoLeak fails when any recorded response (status line, headers, body) contains one of needles.
func NoLeak(t testing.TB, route string, rec *httptest.ResponseRecorder, needles ...string) {
	t.Helper()
	var sb strings.Builder
	for k, v := range rec.Header() {
		sb.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	sb.Write(rec.Body.Bytes())
	for _, n := range needles {
		if n != "" && strings.Contains(strings.ToLower(sb.String()), strings.ToLower(n)) {
			t.Errorf("%s leaks %q to an anonymous principal", route, n)
		}
	}
}

// ValidateProblem checks that rec is a problem+json response whose body validates against components.schemas.Problem
// and whose `status` member equals the HTTP status.
func ValidateProblem(t testing.TB, doc *openapi3.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("error response has Content-Type %q, want application/problem+json (body %q)", ct, rec.Body.String())
		return
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Errorf("problem body is not JSON: %v", err)
		return
	}
	if err := doc.Components.Schemas["Problem"].Value.VisitJSON(v); err != nil {
		t.Errorf("problem violates components.schemas.Problem: %v", err)
	}
	if int(v["status"].(float64)) != rec.Code {
		t.Errorf("problem status %v != HTTP status %d", v["status"], rec.Code)
	}
}
