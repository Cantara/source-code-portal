package catalog

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Severity of a Finding. Validate and Lint return findings; startup refuses on any SevError.
type Severity uint8

const (
	SevInfo Severity = iota
	SevWarning
	SevError
)

func (s Severity) String() string { return [...]string{"info", "warning", "error"}[s] }

// Finding is one validation or lint result. Code is stable (documented in section 4.9 of the design) and is what tests and
// suppression compare; Message is for humans. Path is a JSON-pointer-like location in the config ("/catalog/groups/3/match/1").
type Finding struct {
	Code     string
	Severity Severity
	Path     string
	Message  string
	Hint     string
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s %s %s: %s", f.Severity, f.Code, f.Path, f.Message)
	if f.Hint != "" {
		s += " (" + f.Hint + ")"
	}
	return s
}

// SortFindings orders findings deterministically: severity (errors first), then path, then code, then message.
func SortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if c := comparePath(a.Path, b.Path); c != 0 {
			return c < 0
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// HasErrors reports whether any finding is an error.
func HasErrors(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == SevError {
			return true
		}
	}
	return false
}

// comparePath orders JSON-pointer-like paths segment by segment, numeric segments numerically ("/groups/2" before "/groups/10").
func comparePath(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		x, ex := strconv.Atoi(as[i])
		y, ey := strconv.Atoi(bs[i])
		if ex == nil && ey == nil {
			return x - y
		}
		return strings.Compare(as[i], bs[i])
	}
	return len(as) - len(bs)
}
