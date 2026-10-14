// Package version compares and sorts version strings with the rules of each ecosystem (deps.dev/util/semver behind
// a wrapper). Parsed versions are never persisted; raw strings are. Everything here is total and deterministic.
package version

import (
	"slices"
	"strings"

	"deps.dev/util/semver"
)

// System names a version scheme. The zero value is Opaque.
type System string

const (
	Opaque   System = "opaque" // docker tags, action refs: no ordering, byte comparison only
	Default  System = "semver"
	Maven    System = "maven"
	NPM      System = "npm"
	PyPI     System = "pypi"
	NuGet    System = "nuget"
	Go       System = "go"
	Cargo    System = "cargo"
	RubyGems System = "rubygems"
	Composer System = "composer"
)

// Of maps a manifests ecosystem id to its version system.
func Of(eco string) System {
	switch eco {
	case "maven":
		return Maven
	case "npm":
		return NPM
	case "pypi":
		return PyPI
	case "nuget":
		return NuGet
	case "golang":
		return Go
	case "cargo":
		return Cargo
	case "gem", "cocoapods":
		return RubyGems
	case "composer":
		return Composer
	case "swift", "pub", "hex", "terraform", "helm":
		return Default
	}
	return Opaque
}

func (s System) lib() (semver.System, bool) {
	switch s {
	case Default:
		return semver.DefaultSystem, true
	case Maven:
		return semver.Maven, true
	case NPM:
		return semver.NPM, true
	case PyPI:
		return semver.PyPI, true
	case NuGet:
		return semver.NuGet, true
	case Go:
		return semver.Go, true
	case Cargo:
		return semver.Cargo, true
	case RubyGems:
		return semver.RubyGems, true
	case Composer:
		return semver.Composer, true
	}
	return 0, false
}

// WellFormed is the gate in front of every library parse: ASCII [0-9A-Za-z.+_!-], starts with an alphanumeric,
// no two adjacent separators, no trailing separator, at most 128 bytes. The upstream Maven comparator is not transitive on
// pathological inputs found by FuzzOrderTotal ("1", "1.x0", "1..rC"); the gate keeps those out of the ordered group,
// where they fall back to byte order.
func WellFormed(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	prevSep := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		sep := c == '.' || c == '+' || c == '_' || c == '!' || c == '-'
		alnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !sep && !alnum || i == 0 && sep || sep && prevSep {
			return false
		}
		prevSep = sep
	}
	return !prevSep
}

// Parsable reports whether v is well formed and parses under s.
func Parsable(s System, v string) bool {
	l, ok := s.lib()
	if !ok || !WellFormed(v) {
		return false
	}
	_, err := l.Parse(v)
	return err == nil
}

// Compare orders two versions by the rules of s. ok is false (and cmp 0) when either does not parse or s is Opaque.
func Compare(s System, a, b string) (cmp int, ok bool) {
	l, ok := s.lib()
	if !ok || !WellFormed(a) || !WellFormed(b) {
		return 0, false
	}
	va, err := l.Parse(a)
	if err != nil {
		return 0, false
	}
	vb, err := l.Parse(b)
	if err != nil {
		return 0, false
	}
	return va.Compare(vb), true
}

// Order is a TOTAL order for sorting: parsable versions first (by Compare, ties by raw bytes), then unparsable ones by raw bytes.
func Order(s System, a, b string) int {
	pa, pb := Parsable(s, a), Parsable(s, b)
	switch {
	case pa && pb:
		if c, _ := Compare(s, a, b); c != 0 {
			return c
		}
	case pa:
		return -1
	case pb:
		return 1
	}
	return strings.Compare(a, b)
}

// Sort sorts ascending by Order. The input is first sorted by bytes, so the result is a function of the SET of versions
// even if the upstream comparator is not transitive for some pair (deterministic output, GR-08 ordering independence).
func Sort(s System, vs []string) {
	slices.Sort(vs)
	slices.SortStableFunc(vs, func(a, b string) int { return Order(s, a, b) })
}

// Max returns the greatest PARSABLE version, ignoring pre-releases unless only pre-releases exist.
func Max(s System, vs []string) (string, bool) {
	var best string
	have, bestPre := false, true
	for _, v := range vs {
		if !Parsable(s, v) {
			continue
		}
		pre := IsPrerelease(s, v)
		switch {
		case !have, bestPre && !pre, bestPre == pre && Order(s, v, best) > 0:
			best, bestPre, have = v, pre, true
		}
	}
	return best, have
}

func IsPrerelease(s System, v string) bool {
	l, ok := s.lib()
	if !ok {
		return false
	}
	p, err := l.Parse(v)
	return err == nil && p.IsPrerelease()
}

// Satisfies reports whether version v satisfies requirement req under s. known=false when s is Opaque or either side does not parse
// (never guess: the caller shows "unknown").
func Satisfies(s System, v, req string) (ok, known bool) {
	l, has := s.lib()
	if !has || !WellFormed(v) || len(req) > 512 || strings.TrimSpace(req) == "" {
		return false, false
	}
	pv, err := l.Parse(v)
	if err != nil {
		return false, false
	}
	c, err := l.ParseConstraint(req)
	if err != nil {
		return false, false
	}
	return c.MatchVersion(pv), true
}
