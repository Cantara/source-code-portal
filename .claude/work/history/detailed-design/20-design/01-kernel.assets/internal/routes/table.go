// Package routes is the single table of every URL the server answers: names, methods, ServeMux patterns, classes and
// owners, the recording mux through which generated API handlers register, and the URL builders. It imports the standard library only.
package routes

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Class says how a route behaves at the edge.
type Class uint8

const (
	// ClassAPI: JSON under /api/, registered only through Table.Mux (the recording mux), validated against OpenAPI.
	ClassAPI Class = iota + 1
	// ClassSPA: a page shape of the single-page app (the same index.html, or a negotiated Markdown variant).
	ClassSPA
	// ClassServer: server-generated content that never falls back to the SPA (badges, media, llms, webhooks, assets, probes).
	ClassServer
	// ClassAdmin: served on the admin listener only (status page, metrics, pprof).
	ClassAdmin
)

func (c Class) String() string {
	switch c {
	case ClassAPI:
		return "api"
	case ClassSPA:
		return "spa"
	case ClassServer:
		return "server"
	case ClassAdmin:
		return "admin"
	}
	return "invalid"
}

// Route is one row of the table.
type Route struct {
	Name    string // stable; handlers are registered by name
	Method  string // "GET" or "POST"; GET also matches HEAD
	Pattern string // ServeMux pattern without the method: "/repos/{repo}/readme.md"
	Class   Class
	Owner   string // the package that implements the handler: "commits", "docs", "webapp", ...
}

// FullPattern is the ServeMux pattern including the method.
func (r Route) FullPattern() string { return r.Method + " " + r.Pattern }

// Table holds a list of routes and tracks what has been registered. Build one with Public() or Admin(); a Table is
// used once, at startup, from one goroutine; its methods are mutex-guarded so a mistake does not corrupt it.
type Table struct {
	mu         sync.Mutex
	listener   string
	routes     []Route
	byName     map[string]int
	byFull     map[string]int
	mux        *http.ServeMux
	registered map[string]bool
}

func newTable(listener string, rs []Route) *Table {
	t := &Table{listener: listener, routes: rs, byName: map[string]int{}, byFull: map[string]int{}, registered: map[string]bool{}}
	for i, r := range rs {
		if err := validateRoute(r); err != nil {
			panic("routes: " + err.Error())
		}
		if _, dup := t.byName[r.Name]; dup {
			panic("routes: duplicate route name " + r.Name)
		}
		if _, dup := t.byFull[r.FullPattern()]; dup {
			panic("routes: duplicate pattern " + r.FullPattern())
		}
		t.byName[r.Name], t.byFull[r.FullPattern()] = i, i
	}
	return t
}

// Without removes routes (a feature switched off in configuration, a v1.1 route) before anything is registered.
// It panics on an unknown name. It returns t for chaining.
func (t *Table) Without(names ...string) *Table {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.registered) > 0 {
		panic("routes: Without after registration")
	}
	for _, n := range names {
		if _, ok := t.byName[n]; !ok {
			panic("routes: Without unknown route " + n)
		}
	}
	kept := t.routes[:0:0]
	for _, r := range t.routes {
		if !slices.Contains(names, r.Name) {
			kept = append(kept, r)
		}
	}
	t.routes = kept
	t.byName, t.byFull = map[string]int{}, map[string]int{}
	for i, r := range kept {
		t.byName[r.Name], t.byFull[r.FullPattern()] = i, i
	}
	return t
}

// Routes lists the routes of a class in table order (a copy).
func (t *Table) Routes(c Class) []Route {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Route
	for _, r := range t.routes {
		if r.Class == c {
			out = append(out, r)
		}
	}
	return out
}

// All lists every route in table order (a copy).
func (t *Table) All() []Route {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.routes)
}

// Lookup finds a route by name.
func (t *Table) Lookup(name string) (Route, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.byName[name]
	if !ok {
		return Route{}, false
	}
	return t.routes[i], true
}

func (t *Table) bind(m *http.ServeMux) {
	if m == nil {
		panic("routes: nil mux")
	}
	if t.mux != nil && t.mux != m {
		panic("routes: a Table registers on exactly one mux")
	}
	t.mux = m
}

// Handle registers a handler for a route by name: every non-API route, and the API routes whose handler is hand-written
// (api.openapi is httpapi.Static, the SSE stream) rather than generated; generated API code registers through Mux. It
// panics on an unknown name, on a nil handler and on a second registration of the same name.
func (t *Table) Handle(mux *http.ServeMux, name string, h http.Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.byName[name]
	if !ok {
		panic("routes: Handle unknown route " + name)
	}
	r := t.routes[i]
	switch {
	case h == nil:
		panic("routes: nil handler for " + name)
	case t.registered[name]:
		panic("routes: route registered twice: " + name)
	}
	t.bind(mux)
	t.registered[name] = true
	mux.Handle(r.FullPattern(), h)
}

// Mux returns the recording mux that generated API code registers on (httpapi.Mux).
func (t *Table) Mux(m *http.ServeMux) *Recorder {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bind(m)
	return &Recorder{t: t, mux: m}
}

// Recorder satisfies httpapi.Mux (HandleFunc plus http.Handler). Generated code registers "GET /api/v1/..." patterns
// on it; each must equal the full pattern of a ClassAPI table entry or the call panics at startup.
type Recorder struct {
	t   *Table
	mux *http.ServeMux
}

func (r *Recorder) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	t := r.t
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.byFull[pattern]
	if !ok || t.routes[i].Class != ClassAPI {
		panic(fmt.Sprintf("routes: API pattern %q is not an API route of the %s table", pattern, t.listener))
	}
	name := t.routes[i].Name
	if t.registered[name] {
		panic("routes: route registered twice: " + name)
	}
	t.registered[name] = true
	r.mux.HandleFunc(pattern, h)
}

func (r *Recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.mux.ServeHTTP(w, req) }

// VerifyError lists everything Verify found.
type VerifyError struct {
	Missing  []string // routes with no handler
	Shadowed []string // "name: request resolved to pattern X"
}

func (e *VerifyError) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, "no handler registered for: "+strings.Join(e.Missing, ", "))
	}
	if len(e.Shadowed) > 0 {
		parts = append(parts, "resolved to another pattern: "+strings.Join(e.Shadowed, "; "))
	}
	return "routes: " + strings.Join(parts, "; ")
}

// Verify proves the wiring at startup: every route of the table has a handler, and a sample request for each route
// resolves, in the real mux, to that route's own pattern (nothing shadows it).
func (t *Table) Verify() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ve VerifyError
	for _, r := range t.routes {
		if !t.registered[r.Name] {
			ve.Missing = append(ve.Missing, r.Name)
		}
	}
	if t.mux != nil {
		for _, r := range t.routes {
			if !t.registered[r.Name] {
				continue
			}
			req, err := http.NewRequest(r.Method, "http://scp.invalid"+r.Sample(), nil)
			if err != nil {
				ve.Shadowed = append(ve.Shadowed, r.Name+": bad sample "+err.Error())
				continue
			}
			if _, got := t.mux.Handler(req); got != r.FullPattern() {
				ve.Shadowed = append(ve.Shadowed, fmt.Sprintf("%s: %s -> %q", r.Name, r.Sample(), got))
			}
		}
	}
	if len(ve.Missing) == 0 && len(ve.Shadowed) == 0 {
		return nil
	}
	return &ve
}

// ---- pattern syntax ----

type segment struct {
	kind byte // 'l' literal, 'w' wildcard, 'r' rest wildcard {x...}, 'e' end anchor {$}
	text string
}

var wildName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parsePattern(p string) ([]segment, error) {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, " \t\n?#") {
		return nil, fmt.Errorf("pattern %q must start with / and contain no space, ? or #", p)
	}
	parts := strings.Split(p[1:], "/")
	var out []segment
	seen := map[string]bool{}
	for i, s := range parts {
		last := i == len(parts)-1
		switch {
		case s == "" && last:
			out = append(out, segment{kind: 'e'}) // trailing slash: subtree pattern
		case s == "{$}":
			if !last {
				return nil, fmt.Errorf("pattern %q: {$} must be last", p)
			}
			out = append(out, segment{kind: 'e', text: "$"})
		case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
			name := s[1 : len(s)-1]
			kind := byte('w')
			if n, ok := strings.CutSuffix(name, "..."); ok {
				name, kind = n, 'r'
				if !last {
					return nil, fmt.Errorf("pattern %q: {%s...} must be last", p, name)
				}
			}
			if !wildName.MatchString(name) || seen[name] {
				return nil, fmt.Errorf("pattern %q: bad or repeated wildcard name %q", p, name)
			}
			seen[name] = true
			out = append(out, segment{kind: kind, text: name})
		case strings.ContainsAny(s, "{}"):
			return nil, fmt.Errorf("pattern %q: a wildcard must be a whole segment", p)
		case s == "":
			return nil, fmt.Errorf("pattern %q: empty segment", p)
		default:
			out = append(out, segment{kind: 'l', text: s})
		}
	}
	return out, nil
}

var routeNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9-]+)*$`)

func validateRoute(r Route) error {
	if !routeNameRE.MatchString(r.Name) {
		return fmt.Errorf("route name %q is invalid", r.Name)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return fmt.Errorf("%s: method %q must be GET or POST", r.Name, r.Method)
	}
	if r.Class < ClassAPI || r.Class > ClassAdmin {
		return fmt.Errorf("%s: class is unset", r.Name)
	}
	if r.Owner == "" {
		return fmt.Errorf("%s: owner is empty", r.Name)
	}
	segs, err := parsePattern(r.Pattern)
	if err != nil {
		return fmt.Errorf("%s: %w", r.Name, err)
	}
	if r.Class == ClassAPI {
		for _, s := range segs {
			if s.kind == 'r' || s.kind == 'e' {
				return fmt.Errorf("%s: API patterns are OpenAPI paths: no {x...}, no trailing slash", r.Name)
			}
		}
		if !strings.HasPrefix(r.Pattern, "/api/") {
			return fmt.Errorf("%s: API patterns start with /api/", r.Name)
		}
	}
	return nil
}

// Sample is a concrete request path that must resolve to this route: {x} becomes "x1", {x...} becomes "x1/y2", {$}
// disappears.
func (r Route) Sample() string { return r.Expand(nil) }

// Expand substitutes wildcard values (path-escaped per segment; a {x...} value may contain '/') and returns the
// escaped request path. Wildcards without a value get the sample defaults.
func (r Route) Expand(vals map[string]string) string {
	segs, err := parsePattern(r.Pattern)
	if err != nil {
		panic("routes: " + err.Error())
	}
	var b strings.Builder
	for _, s := range segs {
		switch s.kind {
		case 'l':
			b.WriteString("/" + s.text)
		case 'w':
			v, ok := vals[s.text]
			if !ok {
				v = "x1"
			}
			b.WriteString("/" + url.PathEscape(v))
		case 'r':
			v, ok := vals[s.text]
			if !ok {
				v = "x1/y2"
			}
			b.WriteString("/" + escapePath(v))
		case 'e':
			if s.text == "" { // trailing slash
				b.WriteString("/")
			} else if b.Len() == 0 {
				b.WriteString("/")
			}
		}
	}
	return b.String()
}

// Wildcards lists the wildcard names of the pattern in order.
func (r Route) Wildcards() []string {
	segs, _ := parsePattern(r.Pattern)
	var out []string
	for _, s := range segs {
		if s.kind == 'w' || s.kind == 'r' {
			out = append(out, s.text)
		}
	}
	return out
}

// HasSlug reports that the pattern carries a repository slug (the wildcard named "repo", by convention).
func (r Route) HasSlug() bool { return slices.Contains(r.Wildcards(), "repo") }

// ---- derived views used by wiring and tests ----

// SPAShapes lists the patterns of the ClassSPA routes, sorted: the set that must equal the shapes in dist/routes.json.
func (t *Table) SPAShapes() []string {
	var out []string
	for _, r := range t.Routes(ClassSPA) {
		if !strings.HasPrefix(r.Pattern, "/{path...}") {
			out = append(out, r.Pattern)
		}
	}
	sort.Strings(out)
	return out
}

// ReservedSuffixes are the path suffixes that never fall back to the SPA shell.
func ReservedSuffixes() []string {
	return []string{".md", ".adoc", ".txt", ".xml", ".svg", ".json", ".yaml"}
}

// ReservedPrefixes computes the path prefixes ("/api/") under which no route is an SPA page and no history fallback
// may apply: the first literal segment of every route whose class is not SPA and which has more than one segment,
// minus any first segment that also holds an SPA route. "/.well-known/" is always reserved. Sorted.
func (t *Table) ReservedPrefixes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	spa, other := map[string]bool{}, map[string]bool{}
	for _, r := range t.routes {
		segs, _ := parsePattern(r.Pattern)
		if len(segs) == 0 || segs[0].kind != 'l' {
			continue
		}
		first := segs[0].text
		switch {
		case r.Class == ClassSPA:
			spa[first] = true
		case len(segs) > 1:
			other[first] = true
		}
	}
	out := []string{"/.well-known/"}
	for f := range other {
		if !spa[f] {
			out = append(out, "/"+f+"/")
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}
