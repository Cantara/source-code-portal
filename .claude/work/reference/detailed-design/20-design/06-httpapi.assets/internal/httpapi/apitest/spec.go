// Package apitest is imported from _test.go files only. It holds the contract helpers: the spec linter that
// enforces the API conventions, response validation against the bundled document, the ETag-covers-body
// property and the visibility leak check.
package apitest

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Load parses and validates a bundled document.
func Load(t testing.TB, spec []byte) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(spec)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("invalid spec: %v", err)
	}
	return doc
}

var (
	camel     = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	enumLower = regexp.MustCompile(`^[a-z][a-z0-9]*([_-][a-z0-9]+)*$`)
)

func refName(ref string) string { return ref[strings.LastIndex(ref, "/")+1:] }

// Lint returns every convention violation in doc (empty means clean). Rules, by id:
//
//	L1 operationId unique camelCase          L2 exactly one tag             L3 path under /api/v1/, no trailing slash
//	L4 default response is components/responses/Problem (application/problem+json)
//	L5 GET: If-None-Match param ref, 200 with ETag header, 304 NotModified ref (opt out: x-scp-conditional: false)
//	L6 2xx JSON bodies are $ref to a named schema    L7 property names camelCase
//	L8 GET with Limit: 200 declares Link header and body has `page`
//	L9 no OpenAPI 3.0 `nullable`      L10 string enums lower-case (kebab or snake)      L11 servers, if any, is only the relative "/"
func Lint(doc *openapi3.T) []string {
	var out []string
	add := func(id, where, msg string) { out = append(out, fmt.Sprintf("%s %s: %s", id, where, msg)) }
	for _, sv := range doc.Servers {
		if sv.URL != "/" {
			add("L11", "document", "servers entry "+sv.URL+" must be the relative \"/\" (any host serves the API)")
		}
	}
	seen := map[string]string{}
	paths := doc.Paths.Map()
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		if !strings.HasPrefix(p, "/api/v1/") || strings.HasSuffix(p, "/") {
			add("L3", p, "path must start with /api/v1/ and not end with /")
		}
		for method, op := range paths[p].Operations() {
			where := method + " " + p
			if !camel.MatchString(op.OperationID) {
				add("L1", where, "operationId "+op.OperationID+" is not camelCase")
			}
			if prev, dup := seen[op.OperationID]; dup {
				add("L1", where, "operationId duplicates "+prev)
			}
			seen[op.OperationID] = where
			if len(op.Tags) != 1 {
				add("L2", where, "exactly one tag required")
			}
			def := op.Responses.Default()
			if def == nil || def.Ref == "" || refName(def.Ref) != "Problem" {
				add("L4", where, "default response must be $ref components/responses/Problem")
			}
			conditional := true
			if v, ok := op.Extensions["x-scp-conditional"]; ok && v == false {
				conditional = false
			}
			hasLimit := false
			hasINM := false
			for _, pr := range op.Parameters {
				if pr.Ref != "" && refName(pr.Ref) == "Limit" {
					hasLimit = true
				}
				if pr.Ref != "" && refName(pr.Ref) == "IfNoneMatch" {
					hasINM = true
				}
			}
			if method == "GET" && conditional {
				if !hasINM {
					add("L5", where, "missing If-None-Match parameter ($ref IfNoneMatch)")
				}
				r304 := op.Responses.Value("304")
				if r304 == nil || r304.Ref == "" || refName(r304.Ref) != "NotModified" {
					add("L5", where, "missing 304 response ($ref NotModified)")
				}
				if r200 := op.Responses.Value("200"); r200 == nil || r200.Value == nil || r200.Value.Headers["ETag"] == nil {
					add("L5", where, "200 must declare the ETag header")
				}
			}
			for code, r := range op.Responses.Map() {
				if len(code) == 0 || code[0] != '2' || r.Value == nil {
					continue
				}
				if mt := r.Value.Content.Get("application/json"); mt != nil && (mt.Schema == nil || mt.Schema.Ref == "") {
					add("L6", where+" "+code, "JSON body must be a $ref to a named component schema")
				}
			}
			if method == "GET" && hasLimit {
				r200 := op.Responses.Value("200")
				if r200 == nil || r200.Value == nil || r200.Value.Headers["Link"] == nil {
					add("L8", where, "paged GET must declare the Link header on 200")
				}
				if r200 != nil && r200.Value != nil {
					if mt := r200.Value.Content.Get("application/json"); mt != nil && mt.Schema != nil && mt.Schema.Value != nil {
						if _, ok := mt.Schema.Value.Properties["page"]; !ok {
							add("L8", where, "paged GET body must have a `page` property")
						}
					}
				}
			}
		}
	}
	if doc.Components == nil {
		return out
	}
	names := make([]string, 0, len(doc.Components.Schemas))
	for n := range doc.Components.Schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		walk(doc.Components.Schemas[n].Value, "schema "+n, add, map[*openapi3.Schema]bool{})
	}
	return out
}

func walk(s *openapi3.Schema, where string, add func(id, where, msg string), seen map[*openapi3.Schema]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	if s.Nullable {
		add("L9", where, "uses `nullable`; write type: [T, \"null\"]")
	}
	if s.Type.Includes("string") {
		for _, e := range s.Enum {
			if str, ok := e.(string); ok && !enumLower.MatchString(str) {
				add("L10", where, "enum value "+str+" is not lower-case ([a-z][a-z0-9_-]*)")
			}
		}
	}
	for name, p := range s.Properties {
		if !camel.MatchString(name) {
			add("L7", where, "property "+name+" is not camelCase")
		}
		walk(p.Value, where+"."+name, add, seen)
	}
	if s.Items != nil {
		walk(s.Items.Value, where+"[]", add, seen)
	}
	for _, x := range append(append([]*openapi3.SchemaRef{}, s.AllOf...), append(s.OneOf, s.AnyOf...)...) {
		walk(x.Value, where, add, seen)
	}
}
