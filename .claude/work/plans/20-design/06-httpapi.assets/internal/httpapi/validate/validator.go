// Package validate holds the two kin-openapi based pieces of the API chain: request validation against the
// bundled OpenAPI document and the rewrite of the mux's plain-text 404/405 into problem+json. Only cmd/scp
// imports it.
package validate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// APIPrefix is the only path prefix the validator and ProblemMux act on (the same constant APIDefaults uses).
const APIPrefix = httpapi.APIPrefix

type config struct {
	allowUnknownQuery bool
	observe           func(status int, code string)
}

// Option configures RequestValidator.
type Option func(*config)

// AllowUnknownQuery turns off the rejection of undeclared query parameters (default: rejected, so a typo
// such as ?limt=5 is a 400 instead of a silently ignored filter).
func AllowUnknownQuery() Option { return func(c *config) { c.allowUnknownQuery = true } }

// WithObserver is called for every rejected request (status, problem code); cmd/scp wires a counter to it.
func WithObserver(f func(status int, code string)) Option { return func(c *config) { c.observe = f } }

// RequestValidator builds the middleware from the bundled OpenAPI document (a single self-contained file).
// Under /api/ it: (1) resolves the operation from the document (HEAD resolves as GET), answering 404
// not-found for an unknown path and 405 method-not-allowed with Allow for a known path; (2) validates path,
// query and header parameters and the JSON body; (3) rejects undeclared query parameters. Everything else
// passes through. Response bodies are NOT validated in production.
func RequestValidator(spec []byte, opts ...Option) (func(http.Handler) http.Handler, error) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("validate: load OpenAPI document: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate: invalid OpenAPI document: %w", err)
	}
	doc.Servers = nil // any Host matches (the document is served from whatever origin the portal has)
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("validate: build router: %w", err)
	}
	fopts := &openapi3filter.Options{MultiError: true, ExcludeResponseBody: true, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}

	reject := func(w http.ResponseWriter, r *http.Request, p httpapi.Problem) {
		if c.observe != nil {
			c.observe(p.Status, p.Code)
		}
		httpapi.WriteProblem(w, r, p)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, APIPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			lookup := r
			if r.Method == http.MethodHead {
				cp := *r
				cp.Method = http.MethodGet
				lookup = &cp
			}
			route, params, err := router.FindRoute(lookup)
			if err != nil {
				if errors.Is(err, routers.ErrMethodNotAllowed) {
					reject(w, r, httpapi.MethodNotAllowed(allowed(router, lookup)...))
					return
				}
				reject(w, r, httpapi.NotFound("no such API route"))
				return
			}
			var errs []httpapi.FieldError
			errs = append(errs, queryShape(route, r, c.allowUnknownQuery)...)
			verr := openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{
				Request: lookup, PathParams: params, Route: route, Options: fopts,
			})
			if verr != nil {
				p, fe := mapError(verr)
				if p != nil {
					reject(w, r, *p)
					return
				}
				errs = append(errs, fe...)
			}
			if len(errs) > 0 {
				sort.SliceStable(errs, func(i, j int) bool { return errs[i].Pointer < errs[j].Pointer })
				reject(w, r, httpapi.Validation(errs...))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// allowed lists the methods the document defines for the request's path (HEAD is added with GET).
func allowed(router routers.Router, r *http.Request) []string {
	var out []string
	for _, m := range [...]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace} {
		cp := *r
		cp.Method = m
		if _, _, err := router.FindRoute(&cp); err == nil {
			out = append(out, m)
			if m == http.MethodGet {
				out = append(out, http.MethodHead)
			}
		}
	}
	return out
}

// queryShape rejects undeclared query parameters (unless allowUnknown) and repeated values of scalar parameters.
func queryShape(route *routers.Route, r *http.Request, allowUnknown bool) []httpapi.FieldError {
	known := map[string]*openapi3.Parameter{}
	add := func(ps openapi3.Parameters) {
		for _, p := range ps {
			if p.Value != nil && p.Value.In == openapi3.ParameterInQuery {
				known[p.Value.Name] = p.Value
			}
		}
	}
	add(route.PathItem.Parameters)
	add(route.Operation.Parameters)
	var out []httpapi.FieldError
	for k, vs := range r.URL.Query() {
		p, ok := known[k]
		switch {
		case !ok:
			if !allowUnknown {
				out = append(out, httpapi.FieldError{Pointer: "#/query/" + k, Detail: "unknown parameter"})
			}
		case len(vs) > 1 && p.Schema != nil && p.Schema.Value != nil && !p.Schema.Value.Type.Includes("array"):
			out = append(out, httpapi.FieldError{Pointer: "#/query/" + k, Detail: "parameter must not be repeated"})
		}
	}
	return out
}

// mapError turns kin-openapi's typed errors into field errors (never from message strings), or into a
// whole problem for the body-level cases (413, 415).
func mapError(err error) (*httpapi.Problem, []httpapi.FieldError) {
	var out []httpapi.FieldError
	var visit func(error) *httpapi.Problem
	visit = func(e error) *httpapi.Problem {
		var re *openapi3filter.RequestError
		if errors.As(e, &re) {
			if re.RequestBody != nil && strings.HasPrefix(re.Reason, "header Content-Type has unexpected value") {
				p := httpapi.UnsupportedMediaType("application/json")
				return &p
			}
			var mbe *http.MaxBytesError
			if errors.As(re, &mbe) {
				p := httpapi.PayloadTooLarge(mbe.Limit)
				return &p
			}
			out = append(out, fieldError(re))
			return nil
		}
		var me openapi3.MultiError
		if errors.As(e, &me) {
			for _, x := range me {
				if p := visit(x); p != nil {
					return p
				}
			}
			return nil
		}
		out = append(out, httpapi.FieldError{Pointer: "#/", Detail: "invalid request"})
		return nil
	}
	if p := visit(err); p != nil {
		return p, nil
	}
	return nil, out
}

func fieldError(re *openapi3filter.RequestError) httpapi.FieldError {
	ptr, detail := "#/", re.Reason
	switch {
	case re.Parameter != nil:
		ptr = "#/" + re.Parameter.In + "/" + re.Parameter.Name
	case re.RequestBody != nil:
		ptr = "#/body"
	}
	var se *openapi3.SchemaError
	if re.RequestBody != nil && !errors.Is(re.Err, openapi3filter.ErrInvalidRequired) {
		// Body schema failures arrive as a multi-line jsonschema dump; the single body op is tiny, so a fixed detail is enough.
		detail = "body does not match the schema"
	} else if errors.As(re.Err, &se) {
		if jp := se.JSONPointer(); len(jp) > 0 {
			ptr += "/" + strings.Join(jp, "/")
		}
		detail = se.Reason
	} else if errors.Is(re.Err, openapi3filter.ErrInvalidRequired) {
		detail = "required"
	}
	detail = cleanReason(detail)
	if detail == "" {
		detail = "invalid value"
	}
	if len(detail) > 200 {
		detail = detail[:200]
	}
	return httpapi.FieldError{Pointer: ptr, Detail: detail}
}

// cleanReason reduces kin-openapi's jsonschema dump ("jsonschema validation failed with 'https://...'\n- at ”: maximum:
// got 500, want 200") to its last line without the location prefix ("maximum: got 500, want 200"), and strips
// control characters, so clients never see the fake schema URL or a multi-line message.
func cleanReason(s string) string {
	if i := strings.LastIndex(strings.TrimSpace(s), "\n"); i >= 0 {
		s = strings.TrimSpace(s)[i+1:]
	}
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "- "))
	if strings.HasPrefix(s, "at '") {
		if _, rest, ok := strings.Cut(s[4:], "': "); ok {
			s = rest
		}
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
