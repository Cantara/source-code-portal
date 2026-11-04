package packages

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// release is a version as its numbers: the release segments, and a post-release number (PEP 440 `.postN`), for ordering. Every
// package manager here agrees on what is stable: release numbers, optionally a post-release or build metadata; a pre-release (an
// alpha, beta, release candidate, a `-` suffix in SemVer, `.dev` in PEP 440) is not.
type release struct {
	nums   []int
	post   int
	stable bool
}

// parseVersion reads v (a leading "v" or "V" allowed, as Go and Swift tags have it); ok is false when it does not start with a number.
func parseVersion(v string) (release, bool) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if e, rest, ok := strings.Cut(v, "!"); ok { // a PEP 440 epoch: rare, kept out of the order
		if _, err := strconv.Atoi(e); err == nil {
			v = rest
		}
	}
	var r release
	i := 0
	for {
		j := i
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		if j == i || j-i > 9 {
			break
		}
		n, _ := strconv.Atoi(v[i:j])
		r.nums = append(r.nums, n)
		i = j
		if i < len(v) && v[i] == '.' && i+1 < len(v) && v[i+1] >= '0' && v[i+1] <= '9' {
			i++
			continue
		}
		break
	}
	if len(r.nums) == 0 {
		return release{}, false
	}
	rest := strings.ToLower(v[i:])
	if build, _, ok := strings.Cut(rest, "+"); ok {
		rest = build // build metadata (SemVer, PEP 440 local versions) does not order
	}
	switch {
	case rest == "":
		r.stable = true
	case strings.HasPrefix(rest, ".post") || strings.HasPrefix(rest, "-post") || strings.HasPrefix(rest, "post"):
		p := strings.TrimLeft(rest, ".-post")
		if n, err := strconv.Atoi(p); err == nil || p == "" {
			r.post, r.stable = n, true
		}
	case strings.HasPrefix(rest, ".final") || strings.HasPrefix(rest, ".release") || rest == "-release" || rest == ".ga":
		r.stable = true // Maven's old spellings of a release
	}
	return r, true
}

func (a release) compare(b release) int {
	for i := range max(len(a.nums), len(b.nums)) {
		var x, y int
		if i < len(a.nums) {
			x = a.nums[i]
		}
		if i < len(b.nums) {
			y = b.nums[i]
		}
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return cmp.Compare(a.post, b.post)
}

// newestStable is the highest stable version of vs; "" when none is stable.
func newestStable(vs []string) string {
	best, at := release{}, -1
	for i, v := range vs {
		r, ok := parseVersion(v)
		if !ok || !r.stable {
			continue
		}
		if at < 0 || r.compare(best) > 0 {
			best, at = r, i
		}
	}
	if at < 0 {
		return ""
	}
	return vs[at]
}

// stableTags are the tags under prefix (a Go module's directory, "" at the root) that are stable versions, without the prefix.
func stableTags(tags []string, prefix string) []string {
	var out []string
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, prefix); ok && !strings.Contains(v, "/") {
			if r, ok := parseVersion(v); ok && r.stable {
				out = append(out, t)
			}
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		x, _ := parseVersion(strings.TrimPrefix(a, prefix))
		y, _ := parseVersion(strings.TrimPrefix(b, prefix))
		return y.compare(x)
	})
	return out
}
