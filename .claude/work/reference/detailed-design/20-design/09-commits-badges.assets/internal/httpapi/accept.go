package httpapi

import (
	"sort"
	"strconv"
	"strings"
)

type mediaRange struct {
	typ, sub string
	q        float64
	order    int
}

// parseAccept parses an Accept header into ranges with q-values (default 1). Malformed entries are skipped.
func parseAccept(h string) []mediaRange {
	var out []mediaRange
	for i, part := range strings.Split(h, ",") {
		fields := strings.Split(part, ";")
		mt := strings.ToLower(strings.TrimSpace(fields[0]))
		typ, sub, ok := strings.Cut(mt, "/")
		if !ok || typ == "" || sub == "" || strings.ContainsAny(mt, " \t") {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			k, v, _ := strings.Cut(strings.TrimSpace(f), "=")
			if strings.EqualFold(strings.TrimSpace(k), "q") {
				if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && x >= 0 && x <= 1 {
					q = x
				} else {
					q = 0
				}
			}
		}
		out = append(out, mediaRange{typ, sub, q, i})
	}
	return out
}

// qFor is the q-value the header gives to the concrete media type mt: the most specific matching range wins.
// -1 means no range matches.
func qFor(ranges []mediaRange, mt string) float64 {
	typ, sub, _ := strings.Cut(mt, "/")
	best, spec := -1.0, -1
	for _, r := range ranges {
		s := -1
		switch {
		case r.typ == typ && r.sub == sub:
			s = 2
		case r.typ == typ && r.sub == "*":
			s = 1
		case r.typ == "*" && r.sub == "*":
			s = 0
		}
		if s > spec {
			spec, best = s, r.q
		}
	}
	return best
}

// AcceptsAny reports whether the request's Accept header allows at least one of the media types.
// An absent or empty header accepts everything; an unparseable header is treated as absent.
func AcceptsAny(accept string, offered ...string) bool {
	if strings.TrimSpace(accept) == "" {
		return true
	}
	ranges := parseAccept(accept)
	if len(ranges) == 0 {
		return true
	}
	for _, mt := range offered {
		if qFor(ranges, mt) > 0 {
			return true
		}
	}
	return false
}

// preferMarkdown is true when text/markdown is acceptable and ranked strictly above text/html.
// Ties (including */* and text/*) go to HTML.
func preferMarkdown(accept string) bool {
	ranges := parseAccept(accept)
	md, html := qFor(ranges, "text/markdown"), qFor(ranges, "text/html")
	if md <= 0 {
		return false
	}
	if html <= 0 {
		return true
	}
	// an explicit text/markdown range is required: wildcards never select markdown over html
	explicit := false
	for _, r := range ranges {
		if r.typ == "text" && r.sub == "markdown" {
			explicit = true
		}
	}
	return explicit && md > html
}

// acceptEncodings parses Accept-Encoding into coding -> q (lower-cased names, "*" kept).
func acceptEncodings(h string) map[string]float64 {
	m := map[string]float64{}
	for _, part := range strings.Split(h, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		q := 1.0
		if k, v, ok := strings.Cut(strings.TrimSpace(params), "="); ok && strings.EqualFold(strings.TrimSpace(k), "q") {
			if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && x >= 0 && x <= 1 {
				q = x
			} else {
				q = 0
			}
		}
		m[name] = q
	}
	return m
}

// AcceptsEncoding reports whether coding is acceptable (q > 0, directly or via "*").
func AcceptsEncoding(header, coding string) bool {
	m := acceptEncodings(header)
	if q, ok := m[coding]; ok {
		return q > 0
	}
	return m["*"] > 0
}

var _ = sort.Strings
