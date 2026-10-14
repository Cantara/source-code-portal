package webapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// manifest is dist/routes.json, generated from web/routes.ts at build time.
type manifest struct {
	Version int             `json:"version"`
	BuildID string          `json:"buildId"`
	Routes  []manifestRoute `json:"routes"`
}

type manifestRoute struct {
	Pattern   string   `json:"pattern"`   // ServeMux subset: literal segments, {name}, a final {name...}
	Listeners []string `json:"listeners"` // "public", "admin"
}

var litSeg = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
var wildSeg = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)(\.\.\.)?\}$`)

type segment struct {
	lit  string
	rest bool // {name...}
	any  bool // {name}
}

type shape struct {
	pattern string
	segs    []segment
}

// parseManifest validates routes.json strictly: unknown fields, wrong version, duplicate or malformed
// patterns and unknown listeners are start-time errors (a stale or hand-edited manifest must not boot).
func parseManifest(b []byte) (*manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("routes.json: %w", err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("routes.json: version %d, want 1", m.Version)
	}
	seen := map[string]bool{}
	for _, r := range m.Routes {
		if _, err := parsePattern(r.Pattern); err != nil {
			return nil, fmt.Errorf("routes.json: pattern %q: %w", r.Pattern, err)
		}
		if seen[r.Pattern] {
			return nil, fmt.Errorf("routes.json: duplicate pattern %q", r.Pattern)
		}
		seen[r.Pattern] = true
		if len(r.Listeners) == 0 {
			return nil, fmt.Errorf("routes.json: pattern %q has no listeners", r.Pattern)
		}
		for _, l := range r.Listeners {
			if l != string(Public) && l != string(Admin) {
				return nil, fmt.Errorf("routes.json: pattern %q: unknown listener %q", r.Pattern, l)
			}
		}
	}
	return &m, nil
}

func parsePattern(p string) (shape, error) {
	if !strings.HasPrefix(p, "/") || p == "/" || strings.HasSuffix(p, "/") {
		return shape{}, fmt.Errorf("must start with / and not end with /")
	}
	parts := strings.Split(p[1:], "/")
	s := shape{pattern: p}
	for i, part := range parts {
		switch {
		case litSeg.MatchString(part):
			s.segs = append(s.segs, segment{lit: part})
		case wildSeg.MatchString(part):
			m := wildSeg.FindStringSubmatch(part)
			if m[2] == "..." {
				if i != len(parts)-1 {
					return shape{}, fmt.Errorf("{name...} must be the last segment")
				}
				s.segs = append(s.segs, segment{rest: true})
			} else {
				s.segs = append(s.segs, segment{any: true})
			}
		default:
			return shape{}, fmt.Errorf("segment %q is neither a literal nor {name} / {name...}", part)
		}
	}
	return s, nil
}

// match reports whether the escaped request path has this shape. {name} needs one non-empty segment,
// {name...} one or more. Matching is on the escaped path, so %2F inside a segment never adds a segment.
func (s shape) match(segs []string) bool {
	for i, want := range s.segs {
		if want.rest {
			return i < len(segs) // at least one segment left
		}
		if i >= len(segs) || segs[i] == "" {
			return false
		}
		if !want.any && want.lit != segs[i] {
			return false
		}
	}
	return len(segs) == len(s.segs)
}

// shapeSet is the compiled manifest for one listener.
type shapeSet []shape

func (m *manifest) compile(l Listener) shapeSet {
	var out shapeSet
	for _, r := range m.Routes {
		for _, x := range r.Listeners {
			if x == string(l) {
				s, _ := parsePattern(r.Pattern) // validated in parseManifest
				out = append(out, s)
			}
		}
	}
	return out
}

func (set shapeSet) matches(escapedPath string) bool {
	segs := strings.Split(strings.TrimPrefix(escapedPath, "/"), "/")
	for _, s := range set {
		if s.match(segs) {
			return true
		}
	}
	return false
}
