// Command apicheck is the contract linter of the SCP OpenAPI documents (design unit 07).
//
//	apicheck -public openapi-public.yaml [-admin openapi-admin.yaml] [-catalogue out.md] [-owners out.md]
//
// It exits non-zero when any rule fails. Rules are numbered A1..A27 and documented in the design document (12.2).
//
//	apicheck aliases -common common.yaml -gen common.gen.go -module M -pkg commitsapi   (writes the alias file of a feature api package)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

var (
	out          io.Writer = os.Stdout
	bundled      bool
	skipExamples bool
	trusted      = map[string]bool{} // "Schema.member" of free-text members that the server authors itself (A24)
	etagKind     = map[string]bool{"snapshot-variant": true, "repo-rev": true, "snapshot-version": true, "tree-sha": true, "body-hash": true, "none": true}
	owners       = map[string]bool{"overview": true, "live": true, "catalog": true, "commits": true, "docs": true, "manifests": true, "pages": true, "ops": true, "apispec": true}
)

var (
	failures int
	camel    = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	opIDRe   = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	routeRe  = regexp.MustCompile(`^api(\.[a-z]+)+$`)
)

func fail(rule, format string, a ...any) {
	failures++
	fmt.Fprintf(out, "FAIL %s: %s\n", rule, fmt.Sprintf(format, a...))
}

func load(path string) *openapi3.T {
	l := openapi3.NewLoader()
	l.IsExternalRefsAllowed = true
	doc, err := l.LoadFromFile(path)
	if err != nil {
		fmt.Fprintf(out, "FAIL A1: load %s: %v\n", path, err)
		failures++
		return nil
	}
	vopts := []openapi3.ValidationOption{openapi3.EnableExamplesValidation()}
	if skipExamples {
		vopts = []openapi3.ValidationOption{openapi3.DisableExamplesValidation()}
	}
	if err := doc.Validate(context.Background(), vopts...); err != nil {
		fmt.Fprintf(out, "FAIL A1: validate %s: %v\n", path, err)
		failures++
		return nil
	}
	return doc
}

type op struct {
	Method, Path string
	Op           *openapi3.Operation
	Item         *openapi3.PathItem
}

func ops(doc *openapi3.T) []op {
	var out []op
	for p, item := range doc.Paths.Map() {
		for m, o := range item.Operations() {
			out = append(out, op{m, p, o, item})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func ext(o *openapi3.Operation, k string) (any, bool) { v, ok := o.Extensions[k]; return v, ok }

func hasRefParam(o *openapi3.Operation, item *openapi3.PathItem, name string) bool {
	for _, p := range o.Parameters {
		if p.Value != nil && p.Value.Name == name && p.Value.In == "header" {
			return true
		}
	}
	return false
}

func checkOps(doc *openapi3.T, label string, tags map[string]bool) {
	seenID, seenRoute := map[string]bool{}, map[string]bool{}
	for _, x := range ops(doc) {
		o := x.Op
		id := fmt.Sprintf("%s %s", x.Method, x.Path)
		if !strings.HasPrefix(x.Path, "/api/v1/") {
			fail("A2", "%s: path outside /api/v1", id)
		}
		if !opIDRe.MatchString(o.OperationID) || seenID[o.OperationID] {
			fail("A3", "%s: operationId %q not camelCase or not unique", id, o.OperationID)
		}
		seenID[o.OperationID] = true
		if len(o.Tags) != 1 || !tags[o.Tags[0]] {
			fail("A4", "%s: needs exactly one declared tag, has %v", id, o.Tags)
		}
		for _, pr := range append(o.Parameters, x.Item.Parameters...) {
			if pr == nil || pr.Value == nil || pr.Value.In != "query" || pr.Value.Schema == nil || pr.Value.Schema.Value == nil {
				continue
			}
			sv := pr.Value.Schema.Value
			if sv.Type != nil && sv.Type.Is("array") && sv.Items != nil && sv.Items.Value != nil {
				sv = sv.Items.Value
			}
			if sv.Type != nil && sv.Type.Includes("string") && len(sv.Enum) == 0 && sv.MinLength == 0 && sv.Pattern == "" {
				fail("A23", "%s: query parameter %q is an unconstrained string: the request validator rejects an empty value (400) although the schema allows it; add minLength: 1, a pattern or an enum", id, pr.Value.Name)
			}
		}
		for _, k := range []string{"x-scp-owner", "x-scp-route"} {
			if _, ok := ext(o, k); !ok {
				fail("A5", "%s: missing %s", id, k)
			}
		}
		if v, ok := ext(o, "x-scp-owner"); ok {
			if vs, _ := v.(string); !owners[vs] {
				fail("A5", "%s: x-scp-owner %q is not a package that owns operations", id, v)
			}
		}
		if v, ok := ext(o, "x-scp-etag"); ok {
			if vs, _ := v.(string); !etagKind[vs] {
				fail("A5", "%s: x-scp-etag %q is not one of the derivations of design 4.5", id, v)
			}
		}
		if r, ok := ext(o, "x-scp-route"); ok {
			rs, _ := r.(string)
			if !routeRe.MatchString(rs) || seenRoute[rs] {
				fail("A5", "%s: route name %q invalid or duplicated", id, rs)
			}
			seenRoute[rs] = true
		}
		if o.Summary == "" || o.Description == "" {
			fail("A6", "%s: summary and description are required", id)
		}
		// responses: examples, default problem
		for code, r := range o.Responses.Map() {
			if r.Value == nil {
				fail("A7", "%s %s: unresolved", id, code)
				continue
			}
			for mt, c := range r.Value.Content {
				if c.Example == nil && len(c.Examples) == 0 {
					fail("A7", "%s %s %s: no example", id, code, mt)
				}
			}
		}
		if o.RequestBody != nil && o.RequestBody.Value != nil {
			for mt, c := range o.RequestBody.Value.Content {
				if c.Example == nil && len(c.Examples) == 0 {
					fail("A7", "%s request %s: no example", id, mt)
				}
			}
		}
		d := o.Responses.Map()["default"]
		if d == nil || !(strings.HasSuffix(d.Ref, "common.yaml#/components/responses/Problem") || (bundled && d.Ref == "#/components/responses/Problem")) {
			fail("A8", "%s: default response must be common Problem", id)
		}
		if x.Method == "GET" {
			if v, ok := ext(o, "x-scp-conditional"); ok && v == false {
				// exempt (stream)
			} else {
				if !hasRefParam(o, nil, "If-None-Match") {
					fail("A9", "%s: GET without If-None-Match", id)
				}
				r200 := o.Responses.Map()["200"]
				if r200 == nil || r200.Value == nil || r200.Value.Headers["ETag"] == nil {
					fail("A9", "%s: 200 without ETag header", id)
				}
				if o.Responses.Map()["304"] == nil {
					fail("A9", "%s: no 304", id)
				}
				if _, ok := ext(o, "x-scp-etag"); !ok {
					fail("A5", "%s: missing x-scp-etag", id)
				}
			}
		}
		if strings.Contains(x.Path, "{repo}") && x.Method == "GET" {
			if o.Responses.Map()["308"] == nil {
				fail("A10", "%s: {repo} operation without 308", id)
			}
		}
		// path params declared
		for _, m := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(x.Path, -1) {
			found := false
			for _, p := range o.Parameters {
				if p.Value != nil && p.Value.In == "path" && p.Value.Name == m[1] {
					found = true
				}
			}
			if !found {
				fail("A11", "%s: path parameter %s not declared", id, m[1])
			}
		}
		// list operations use the shared pagination parameters
		if r := o.Responses.Map()["200"]; r != nil && r.Value != nil {
			if c := r.Value.Content.Get("application/json"); c != nil && c.Schema != nil && c.Schema.Value != nil {
				s := c.Schema
				if s.Ref != "" {
					if _, hasItems := s.Value.Properties["items"]; hasItems {
						names := map[string]bool{}
						for _, p := range o.Parameters {
							if p.Value != nil {
								names[p.Value.Name] = true
							}
						}
						if !names["cursor"] || !names["limit"] {
							fail("A12", "%s: collection without cursor and limit", id)
						}
						if _, ok := s.Value.Properties["page"]; !ok {
							fail("A12", "%s: collection without page", id)
						}
						if _, ok := s.Value.Properties["freshness"]; !ok {
							fail("A12", "%s: collection without freshness", id)
						}
						if r.Value.Headers["Link"] == nil {
							fail("A12", "%s: collection 200 without the Link header (rel=next)", id)
						}
					}
				}
			}
		}
	}
	_ = label
}

// walkSchemas visits every schema (local and common) of a document and applies naming and subset rules.
func checkSchemas(doc *openapi3.T, label string) {
	var visit func(path string, s *openapi3.SchemaRef, depth int)
	seen := map[*openapi3.Schema]bool{}
	visit = func(path string, s *openapi3.SchemaRef, depth int) {
		if s == nil || s.Value == nil || depth > 40 {
			return
		}
		if strings.HasSuffix(s.Ref, "#/components/schemas/Timestamp") || strings.HasSuffix(s.Ref, "#/components/schemas/NullableTimestamp") {
			return
		}
		if seen[s.Value] && s.Ref != "" {
			return
		}
		seen[s.Value] = true
		v := s.Value
		if v.Type != nil && len(*v.Type) > 1 {
			t := *v.Type
			if !(len(t) == 2 && t.Includes("null")) {
				fail("A13", "%s: %s: type array %v is not [T, \"null\"]", label, path, t)
			}
			if t.Includes("null") && (t.Includes("object") || t.Includes("array")) {
				fail("A13", "%s: %s: nullable object/array is outside the subset", label, path)
			}
		}
		if v.Nullable {
			fail("A13", "%s: %s: OpenAPI 3.0 nullable is not used", label, path)
		}
		for _, sub := range append(append([]*openapi3.SchemaRef{}, v.OneOf...), v.AnyOf...) {
			if sub.Value != nil && sub.Value.Type != nil && sub.Value.Type.Includes("null") {
				fail("A13", "%s: %s: oneOf/anyOf with null is outside the subset", label, path)
			}
		}
		if len(v.AllOf) > 0 {
			refs, inline := 0, 0
			for _, a := range v.AllOf {
				if a.Ref != "" {
					refs++
				} else {
					inline++
				}
			}
			if refs != 1 || inline != 1 || len(v.AllOf) != 2 {
				fail("A14", "%s: %s: allOf must be one $ref plus one inline object", label, path)
			}
		}
		if v.Format == "date-time" && !strings.HasSuffix(path, "/Timestamp") && !strings.HasSuffix(path, "/NullableTimestamp") && !strings.Contains(path, "/Timestamp/") {
			fail("A15", "%s: %s: raw date-time, use Timestamp or NullableTimestamp", label, path)
		}
		for name, p := range v.Properties {
			if !camel.MatchString(name) {
				fail("A16", "%s: %s.%s: property is not camelCase", label, path, name)
			}
			if p.Ref == "" && p.Value != nil && (strings.HasSuffix(name, "At") || strings.HasSuffix(name, "Since")) && p.Value.Format == "date-time" {
				fail("A15", "%s: %s.%s: inline date-time", label, path, name)
			}
			if strings.HasSuffix(name, "At") && p.Ref == "" && p.Value != nil && p.Value.Type != nil && p.Value.Type.Includes("string") {
				fail("A15", "%s: %s.%s: *At member must reference Timestamp", label, path, name)
			}
			visit(path+"."+name, p, depth+1)
		}
		req := map[string]bool{}
		for _, r := range v.Required {
			req[r] = true
		}
		for name, p := range v.Properties {
			nullable := strings.HasSuffix(p.Ref, "#/components/schemas/NullableTimestamp") || (p.Value != nil && p.Value.Type != nil && p.Value.Type.Includes("null"))
			if nullable && !req[name] {
				fail("A22", "%s: %s.%s: a nullable member must be required (generated Go would omit it instead of emitting null)", label, path, name)
			}
		}
		for _, r := range v.Required {
			if _, ok := v.Properties[r]; !ok && len(v.AllOf) == 0 {
				fail("A17", "%s: %s: required %q is not a property", label, path, r)
			}
		}
		if v.Items != nil {
			visit(path+"[]", v.Items, depth+1)
		}
		if v.AdditionalProperties.Schema != nil {
			visit(path+"{}", v.AdditionalProperties.Schema, depth+1)
		}
		for i, a := range v.AllOf {
			visit(fmt.Sprintf("%s.allOf%d", path, i), a, depth+1)
		}
		for i, a := range v.OneOf {
			visit(fmt.Sprintf("%s.oneOf%d", path, i), a, depth+1)
		}
		for i, a := range v.AnyOf {
			visit(fmt.Sprintf("%s.anyOf%d", path, i), a, depth+1)
		}
	}
	for name, s := range doc.Components.Schemas {
		if name == "Timestamp" || name == "NullableTimestamp" {
			continue
		}
		visit("/"+name, s, 0)
	}
}

func localName(ref, prefix string) (string, bool) {
	if strings.HasPrefix(ref, "#/components/"+prefix+"/") {
		return strings.TrimPrefix(ref, "#/components/"+prefix+"/"), true
	}
	return "", false
}

// ownership computes, per local schema, the set of tags (features) whose operations reach it.
func ownership(doc *openapi3.T) (map[string]map[string]bool, map[string]map[string]bool) {
	local := map[string]map[string]bool{}  // local schema -> tags
	common := map[string]map[string]bool{} // common schema -> tags
	for _, x := range ops(doc) {
		tag := x.Op.Tags[0]
		seen := map[*openapi3.Schema]bool{}
		var walk func(s *openapi3.SchemaRef, inCommon bool)
		walk = func(s *openapi3.SchemaRef, inCommon bool) {
			if s == nil {
				return
			}
			if n, ok := localName(s.Ref, "schemas"); ok && inCommon {
				if common[n] == nil {
					common[n] = map[string]bool{}
				}
				common[n][tag] = true
				inCommon = true
			} else if ok {
				if local[n] == nil {
					local[n] = map[string]bool{}
				}
				local[n][tag] = true
			} else if i := strings.Index(s.Ref, "common.yaml#/components/schemas/"); i >= 0 {
				n := s.Ref[i+len("common.yaml#/components/schemas/"):]
				if common[n] == nil {
					common[n] = map[string]bool{}
				}
				common[n][tag] = true
				inCommon = true
			}
			if s.Value == nil || seen[s.Value] {
				return
			}
			seen[s.Value] = true
			v := s.Value
			for _, p := range v.Properties {
				walk(p, inCommon)
			}
			walk(v.Items, inCommon)
			walk(v.AdditionalProperties.Schema, inCommon)
			for _, a := range v.AllOf {
				walk(a, inCommon)
			}
			for _, a := range v.OneOf {
				walk(a, inCommon)
			}
			for _, a := range v.AnyOf {
				walk(a, inCommon)
			}
		}
		walkParams := func(ps openapi3.Parameters) {
			for _, p := range ps {
				if p.Value != nil {
					walk(p.Value.Schema, strings.Contains(p.Ref, "common.yaml"))
				}
			}
		}
		walkParams(x.Op.Parameters)
		for _, r := range x.Op.Responses.Map() {
			if r.Value == nil {
				continue
			}
			for _, c := range r.Value.Content {
				walk(c.Schema, strings.Contains(r.Ref, "common.yaml"))
			}
		}
		if x.Op.RequestBody != nil && x.Op.RequestBody.Value != nil {
			for _, c := range x.Op.RequestBody.Value.Content {
				walk(c.Schema, false)
			}
		}
	}
	return local, common
}

// strictExample reports object members of an example that the schema does not declare (kin-openapi accepts
// additional properties, Redocly and Schemathesis flag them; a typo in a hand-written example is a defect).
func strictExample(s *openapi3.SchemaRef, v any, path string, report func(string)) {
	if s == nil || s.Value == nil {
		return
	}
	sv := s.Value
	if len(sv.AnyOf) > 0 || len(sv.OneOf) > 0 {
		alts := append(append([]*openapi3.SchemaRef{}, sv.AnyOf...), sv.OneOf...)
		for _, a := range alts {
			n := 0
			strictExample(a, v, path, func(string) { n++ })
			if n == 0 {
				return
			}
		}
		report(path + ": matches none of the union members strictly")
		return
	}
	props := map[string]*openapi3.SchemaRef{}
	var collect func(x *openapi3.Schema)
	collect = func(x *openapi3.Schema) {
		for k, p := range x.Properties {
			props[k] = p
		}
		for _, a := range x.AllOf {
			if a.Value != nil {
				collect(a.Value)
			}
		}
	}
	collect(sv)
	switch t := v.(type) {
	case map[string]any:
		open := sv.AdditionalProperties.Has != nil && *sv.AdditionalProperties.Has || sv.AdditionalProperties.Schema != nil
		if len(props) == 0 && !open && len(sv.AllOf) == 0 {
			open = true // free-form object
		}
		for k, x := range t {
			if p, ok := props[k]; ok {
				strictExample(p, x, path+"."+k, report)
			} else if sv.AdditionalProperties.Schema != nil {
				strictExample(sv.AdditionalProperties.Schema, x, path+"."+k, report)
			} else if !open {
				report(fmt.Sprintf("%s: member %q is not declared by the schema", path, k))
			}
		}
	case []any:
		for i, x := range t {
			strictExample(sv.Items, x, fmt.Sprintf("%s[%d]", path, i), report)
		}
	}
}

func checkExamples(doc *openapi3.T, label string) {
	for _, x := range ops(doc) {
		id := x.Method + " " + x.Path
		visit := func(where string, content openapi3.Content) {
			for mt, c := range content {
				if c.Schema == nil {
					continue
				}
				if c.Example != nil {
					strictExample(c.Schema, c.Example, id+" "+where+" "+mt, func(m string) { fail("A21", "%s", m) })
				}
				for n, e := range c.Examples {
					if e.Value != nil && e.Value.Value != nil {
						strictExample(c.Schema, e.Value.Value, id+" "+where+" "+mt+" example "+n, func(m string) { fail("A21", "%s", m) })
					}
				}
			}
		}
		for code, r := range x.Op.Responses.Map() {
			if r.Value != nil {
				visit(code, r.Value.Content)
			}
		}
		if x.Op.RequestBody != nil && x.Op.RequestBody.Value != nil {
			visit("request", x.Op.RequestBody.Value.Content)
		}
	}
	_ = label
}

func keys(m map[string]bool) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "aliases" {
		os.Exit(runAliases(os.Args[2:]))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	failures = 0
	fs := flag.NewFlagSet("apicheck", flag.ContinueOnError)
	pubPath := fs.String("public", "", "public document")
	fs.BoolVar(&bundled, "bundled", false, "input is a redocly-bundled document (skips ownership rules)")
	admPath := fs.String("admin", "", "admin document")
	catOut := fs.String("catalogue", "", "write operation table (markdown)")
	ownOut := fs.String("owners", "", "write schema ownership table (markdown)")
	fs.BoolVar(&skipExamples, "no-examples", false, "do not validate examples against their schemas (used by the negative tests of rules whose mutation would also break an example)")
	trustedPath := fs.String("trusted", "", "file listing the free-text members the server authors itself, one `Schema.member` per line (A24)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tags := map[string]bool{"meta": true, "overview": true, "live": true, "catalog": true, "commits": true, "docs": true, "manifests": true, "pages": true, "ops": true}
	trusted = map[string]bool{}
	if *trustedPath != "" {
		f, err := os.Open(*trustedPath)
		if err != nil {
			fmt.Fprintf(out, "FAIL A24: %v\n", err)
			return 1
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(strings.SplitN(sc.Text(), "#", 2)[0])
			if l != "" {
				trusted[l] = true
			}
		}
		f.Close()
	}

	pub := load(*pubPath)
	if pub == nil {
		return 1
	}
	fmt.Fprintf(out, "A1 ok: %s loads and validates (kin-openapi, examples validated), %d paths\n", *pubPath, pub.Paths.Len())
	checkOps(pub, "public", tags)
	checkSchemas(pub, "public")
	checkExamples(pub, "public")
	checkServers(pub, "public")
	local, common := ownership(pub)
	if bundled {
		local, common = map[string]map[string]bool{}, map[string]map[string]bool{}
	}
	// A18: a local schema may belong to exactly one tag
	var rows []string
	names := make([]string, 0, len(local))
	for n := range local {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := keys(local[n])
		if len(t) > 1 {
			fail("A18", "schema %s is reached from several features %v: move it to common.yaml", n, t)
		}
		rows = append(rows, fmt.Sprintf("| `%s` | %s |", n, t[0]))
	}
	// A18: a local schema may not reuse the name of a common schema (the generated alias file of the feature would clash)
	for n := range pub.Components.Schemas {
		if _, ok := common[n]; ok && !bundled {
			fail("A18", "schema %s is defined locally and in common.yaml", n)
		}
	}
	// local schemas that no operation reaches
	for n := range pub.Components.Schemas {
		if _, ok := local[n]; !ok && !bundled {
			fail("A19", "schema %s is not reachable from any operation", n)
		}
	}
	cn := make([]string, 0, len(common))
	for n := range common {
		cn = append(cn, n)
	}
	sort.Strings(cn)
	var crow []string
	for _, n := range cn {
		t := keys(common[n])
		crow = append(crow, fmt.Sprintf("| `%s` | %s |", n, strings.Join(t, ", ")))
		if len(t) == 1 {
			fmt.Fprintf(out, "INFO A18: common schema %s is used by one feature only (%s)\n", n, t[0])
		}
	}
	if *ownOut != "" {
		s := "| Local schema (owner feature) | Feature |\n|---|---|\n" + strings.Join(rows, "\n") + "\n\n| Common schema (reached directly by operations) | Features |\n|---|---|\n" + strings.Join(crow, "\n") + "\n"
		os.WriteFile(*ownOut, []byte(s), 0o644)
	}
	if *catOut != "" {
		var b strings.Builder
		b.WriteString("| Method and path | operationId | Tag | Owner | Route name | ETag | Statuses |\n|---|---|---|---|---|---|---|\n")
		for _, x := range ops(pub) {
			var st []string
			for c := range x.Op.Responses.Map() {
				st = append(st, c)
			}
			sort.Strings(st)
			get := func(k string) string { v, _ := ext(x.Op, k); s, _ := v.(string); return s }
			fmt.Fprintf(&b, "| `%s %s` | `%s` | %s | %s | `%s` | %s | %s |\n", x.Method, x.Path, x.Op.OperationID, x.Op.Tags[0], get("x-scp-owner"), get("x-scp-route"), get("x-scp-etag"), strings.Join(st, " "))
		}
		os.WriteFile(*catOut, []byte(b.String()), 0o644)
	}

	if *admPath != "" {
		adm := load(*admPath)
		if adm == nil {
			return 1
		}
		fmt.Fprintf(out, "A1 ok: %s loads and validates, %d paths\n", *admPath, adm.Paths.Len())
		checkOps(adm, "admin", tags)
		checkSchemas(adm, "admin")
		checkExamples(adm, "admin")
		checkServers(adm, "admin")
		// A20: admin is a strict superset of public
		pubSet := map[string]bool{}
		for _, x := range ops(pub) {
			pubSet[x.Method+" "+x.Path] = true
		}
		admSet := map[string]bool{}
		for _, x := range ops(adm) {
			admSet[x.Method+" "+x.Path] = true
		}
		for k := range pubSet {
			if !admSet[k] {
				fail("A20", "admin document lacks public operation %s", k)
			}
		}
		var extra []string
		for k := range admSet {
			if !pubSet[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		fmt.Fprintf(out, "A20 ok: admin = public (%d) + %v\n", len(pubSet), extra)
		// A24-A26 on both documents: the admin one adds the `ops` schemas
		checkUntrusted(pub, adm)
	} else {
		checkUntrusted(pub)
	}
	fmt.Fprintf(out, "apicheck: %d failures\n", failures)
	if failures > 0 {
		return 1
	}
	return 0
}

var emailRe = regexp.MustCompile(`(?i)e-?mail`)
var enumValueRe = regexp.MustCompile(`^[a-z0-9]+([_-][a-z0-9]+)*$`)

// isFreeText reports whether a schema member is a string (or an array of strings) whose content the contract does not constrain.
func isFreeText(p *openapi3.SchemaRef) (bool, *openapi3.SchemaRef) {
	if p == nil || p.Value == nil || p.Ref != "" {
		return false, nil
	}
	v := p.Value
	if v.Type != nil && v.Type.Is("array") && v.Items != nil && v.Items.Value != nil && v.Items.Ref == "" {
		ok, _ := isFreeText(v.Items)
		return ok, v.Items
	}
	if v.Type == nil || !v.Type.Includes("string") || len(v.Enum) > 0 || v.Pattern != "" || v.Format != "" {
		return false, nil
	}
	return true, nil
}

// checkUntrusted implements A24 (every free-text member is either marked x-scp-untrusted or listed as server-authored)
// and A25 (no e-mail member or format anywhere), A26 (enum values of schemas are lower-case words).
func checkUntrusted(docs ...*openapi3.T) {
	named := map[string]*openapi3.Schema{}
	seen := map[*openapi3.Schema]bool{}
	var reach func(sr *openapi3.SchemaRef)
	reach = func(sr *openapi3.SchemaRef) {
		if sr == nil || sr.Value == nil {
			return
		}
		if i := strings.LastIndex(sr.Ref, "/components/schemas/"); i >= 0 {
			named[sr.Ref[i+len("/components/schemas/"):]] = sr.Value
		}
		if seen[sr.Value] {
			return
		}
		seen[sr.Value] = true
		v := sr.Value
		for _, p := range v.Properties {
			reach(p)
		}
		reach(v.Items)
		reach(v.AdditionalProperties.Schema)
		for _, l := range [][]*openapi3.SchemaRef{v.AllOf, v.OneOf, v.AnyOf} {
			for _, a := range l {
				reach(a)
			}
		}
	}
	for _, doc := range docs {
		for n, s := range doc.Components.Schemas {
			if s.Ref == "" {
				s = &openapi3.SchemaRef{Ref: "#/components/schemas/" + n, Value: s.Value}
			}
			reach(s)
		}
		for _, x := range ops(doc) {
			for _, pr := range x.Op.Parameters {
				if pr.Value != nil {
					if emailRe.MatchString(pr.Value.Name) {
						fail("A25", "%s %s: parameter %s mentions e-mail", x.Method, x.Path, pr.Value.Name)
					}
					reach(pr.Value.Schema)
				}
			}
			for _, r := range x.Op.Responses.Map() {
				if r.Value != nil {
					for _, c := range r.Value.Content {
						reach(c.Schema)
					}
				}
			}
			if x.Op.RequestBody != nil && x.Op.RequestBody.Value != nil {
				for _, c := range x.Op.RequestBody.Value.Content {
					reach(c.Schema)
				}
			}
		}
	}
	listed := map[string]bool{}
	for _, n := range sortedKeys(named) {
		sv := named[n]
		if sv.Format == "email" || sv.Format == "idn-email" {
			fail("A25", "/%s: format %s (no e-mail address is ever returned, GR-35)", n, sv.Format)
		}
		for _, e := range sv.Enum {
			if str, ok := e.(string); ok && !enumValueRe.MatchString(str) {
				fail("A26", "/%s: enum value %q is not a lower-case word (`[a-z0-9]+([_-][a-z0-9]+)*`)", n, str)
			}
		}
		for _, pn := range sortedKeys(sv.Properties) {
			p := sv.Properties[pn]
			if emailRe.MatchString(pn) {
				fail("A25", "/%s.%s: member name mentions e-mail (GR-35)", n, pn)
			}
			if p.Value == nil {
				continue
			}
			if p.Value.Format == "email" || p.Value.Format == "idn-email" {
				fail("A25", "/%s.%s: format %s (GR-35)", n, pn, p.Value.Format)
			}
			if free, items := isFreeText(p); free {
				key := n + "." + pn
				_, mark := p.Value.Extensions["x-scp-untrusted"]
				if items != nil {
					_, m2 := items.Value.Extensions["x-scp-untrusted"]
					mark = mark || m2
				}
				switch {
				case mark && trusted[key]:
					fail("A24", "%s is marked x-scp-untrusted and also listed as server-authored", key)
				case !mark && !trusted[key]:
					fail("A24", "%s is free text that is neither marked x-scp-untrusted nor listed in the server-authored file: decide who authors it", key)
				}
				if trusted[key] {
					listed[key] = true
				}
			}
		}
	}
	for k := range trusted {
		if !listed[k] && len(docs) > 1 { // the admin-only entries are stale in a public-only run
			fail("A24", "server-authored list names %s, which is not a free-text member of the documents (stale entry)", k)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// checkServers implements A27: servers, when present, is exactly the same origin (`/`).
func checkServers(doc *openapi3.T, label string) {
	for _, sv := range doc.Servers {
		if sv.URL != "/" {
			fail("A27", "%s: server url %q: only the same origin `/` is allowed", label, sv.URL)
		}
	}
}
