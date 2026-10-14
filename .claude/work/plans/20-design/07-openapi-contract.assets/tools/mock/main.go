// Command mock serves the examples of an OpenAPI document as a contract test double: it validates every request
// against the document (kin-openapi), answers the declared 200/202 example, 304 on a matching If-None-Match, and RFC 9457
// problems for unknown routes (404), wrong methods (405 + Allow) and invalid requests (400). It stands in for the real
// server so Schemathesis can exercise the contract before any handler exists (design unit 07, section 12).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

func problem(w http.ResponseWriter, status int, code, title, detail string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	p := map[string]any{"type": "urn:scp:problem:" + code, "title": title, "status": status, "code": code, "detail": detail, "instance": "urn:uuid:0e6c1d5a-7a3b-4c52-9f31-5d2f0f6a8b11"}
	for k, v := range extra {
		p[k] = v
	}
	json.NewEncoder(w).Encode(p)
}

func main() {
	l := openapi3.NewLoader()
	l.IsExternalRefsAllowed = true
	doc, err := l.LoadFromFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	doc.Servers = nil
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		log.Fatal(err)
	}
	known := map[string]string{"repo": "billing-service", "groupId": "platform", "docId": "docs~install.md", "slug": "about"}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			problem(w, 404, "not-found", "Not found", "no such route", nil)
			return
		}
		method := r.Method
		if method == http.MethodHead {
			method = http.MethodGet
		}
		req := r.Clone(r.Context())
		req.Method = method
		route, pathParams, err := router.FindRoute(req)
		if err != nil {
			// distinguish unknown path from wrong method
			var allow []string
			for _, m := range []string{"GET", "POST"} {
				probe := r.Clone(r.Context())
				probe.Method = m
				if _, _, e := router.FindRoute(probe); e == nil {
					allow = append(allow, m)
				}
			}
			if len(allow) > 0 {
				w.Header().Set("Allow", strings.Join(allow, ", "))
				problem(w, 405, "method-not-allowed", "Method not allowed", r.Method+" is not allowed here", nil)
				return
			}
			problem(w, 404, "not-found", "Not found", "no such route", nil)
			return
		}
		// the rules that kin-openapi does not enforce and that httpapi/validate adds (design 4.12): an unknown query key, an empty
		// value and a repeated scalar parameter are `400 validation-failed`
		declared := map[string]*openapi3.Parameter{}
		for _, p := range route.Operation.Parameters {
			if p.Value != nil && p.Value.In == "query" {
				declared[p.Value.Name] = p.Value
			}
		}
		var violations []map[string]string
		for k, vs := range r.URL.Query() {
			p, ok := declared[k]
			switch {
			case !ok:
				violations = append(violations, map[string]string{"pointer": "#/query/" + k, "detail": "unknown parameter"})
			case len(vs) > 1 && (p.Schema == nil || p.Schema.Value == nil || p.Schema.Value.Type == nil || !p.Schema.Value.Type.Is("array")):
				violations = append(violations, map[string]string{"pointer": "#/query/" + k, "detail": "must not be repeated"})
			default:
				for _, v := range vs {
					if v == "" {
						violations = append(violations, map[string]string{"pointer": "#/query/" + k, "detail": "must not be empty"})
						break
					}
				}
			}
		}
		if len(violations) > 0 {
			problem(w, 400, "validation-failed", "Validation failed", "The request has invalid parameters.", map[string]any{"errors": violations})
			return
		}
		in := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route, Options: &openapi3filter.Options{MultiError: true, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
		if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
			problem(w, 400, "validation-failed", "Validation failed", "The request violates the contract.", map[string]any{"errors": []map[string]string{{"pointer": "#/request", "detail": strings.ReplaceAll(err.Error(), "\n", " ")}}})
			return
		}
		op := route.Operation
		for k, want := range known {
			if got, ok := pathParams[k]; ok && got != want {
				problem(w, 404, "not-found", "Not found", "no such "+k, nil)
				return
			}
		}
		if q := r.URL.Query().Get("purl"); q != "" && !strings.HasPrefix(q, "pkg:maven/org.acme.platform/core-lib") {
			problem(w, 404, "not-found", "Not found", "unknown component", nil)
			return
		}
		if v, ok := op.Extensions["x-scp-flag"]; ok {
			problem(w, 404, "feature-disabled", "Feature disabled", fmt.Sprint(v)+" is false", nil)
			return
		}
		ok := "200"
		if method == http.MethodPost {
			ok = "202"
		}
		resp := op.Responses.Value(ok)
		if resp == nil || resp.Value == nil {
			problem(w, 500, "internal", "Internal error", "no success response", nil)
			return
		}
		if etag := resp.Value.Headers["ETag"]; etag != nil {
			tag := `W/"g4f7a1c2e-1841-0"`
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
			if inm := r.Header.Get("If-None-Match"); inm == tag || inm == "*" {
				w.WriteHeader(304)
				return
			}
		}
		var mts []string
		for mt := range resp.Value.Content {
			mts = append(mts, mt)
		}
		sort.Strings(mts)
		mt := mts[0]
		if r.URL.Query().Get("format") == "mermaid" {
			mt = "text/plain"
		} else if _, has := resp.Value.Content["application/json"]; has {
			mt = "application/json"
		}
		media := resp.Value.Content[mt]
		var body any = media.Example
		if body == nil {
			for _, e := range media.Examples {
				body = e.Value.Value
				break
			}
		}
		w.Header().Set("Content-Type", mt)
		w.WriteHeader(200 + map[string]int{"202": 2}[ok])
		if method == http.MethodHead {
			return
		}
		switch b := body.(type) {
		case string:
			fmt.Fprint(w, b)
		default:
			json.NewEncoder(w).Encode(b)
		}
	})
	log.Println("listening on", os.Args[2])
	log.Fatal(http.ListenAndServe(os.Args[2], h))
}
