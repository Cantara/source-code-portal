package python

import (
	"strings"
)

// NormalizeName applies PEP 503: runs of '-', '_' and '.' collapse to '-', then lower case.
func NormalizeName(name string) string {
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			if !prevSep {
				b.WriteByte('-')
			}
			prevSep = true
			continue
		}
		prevSep = false
		b.WriteRune(r)
	}
	return b.String()
}

// Requirement is a parsed PEP 508 dependency specifier. Extras and markers are kept as written.
type Requirement struct {
	Name      string   // as written
	Extras    []string // as written
	Specifier string   // comma-joined specifier text without parentheses and spaces ("" when absent)
	URL       string   // direct reference ("name @ url"), "" when absent
	Marker    string   // text after ';', trimmed
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ParseRequirement parses one PEP 508 specifier with a hand-written scanner (no regexp backtracking, linear time).
// It accepts the older parenthesised form "name (>=1.0)" used by Poetry 2 and old metadata.
func ParseRequirement(s string) (Requirement, bool) {
	var r Requirement
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > 4096 {
		return r, false
	}
	i := 0
	if !isAlnum(s[0]) {
		return r, false
	}
	for i < len(s) && (isAlnum(s[i]) || s[i] == '.' || s[i] == '_' || s[i] == '-') {
		i++
	}
	// the name must end with an alphanumeric character
	for i > 0 && !isAlnum(s[i-1]) {
		i--
	}
	r.Name = s[:i]
	skip := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
	}
	skip()
	if i < len(s) && s[i] == '[' {
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			return r, false
		}
		for _, e := range strings.Split(s[i+1:i+j], ",") {
			if e = strings.TrimSpace(e); e != "" {
				r.Extras = append(r.Extras, e)
			}
		}
		i += j + 1
		skip()
	}
	if i < len(s) && s[i] == '@' {
		i++
		skip()
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != ';' {
			j++
		}
		r.URL = s[i:j]
		i = j
		skip()
	} else {
		j := i
		depth := 0
		for j < len(s) {
			if s[j] == '(' {
				depth++
			} else if s[j] == ')' {
				depth--
			} else if s[j] == ';' && depth <= 0 {
				break
			}
			j++
		}
		spec := strings.TrimSpace(s[i:j])
		spec = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(spec, "("), ")"))
		r.Specifier = strings.ReplaceAll(strings.ReplaceAll(spec, " ", ""), "\t", "")
		if r.Specifier != "" && !validSpecifier(r.Specifier) {
			return r, false
		}
		i = j
	}
	if i < len(s) && s[i] == ';' {
		r.Marker = strings.TrimSpace(s[i+1:])
	} else if i < len(s) {
		return r, false
	}
	return r, true
}

// specOps lists the comparison operators of PEP 440 specifiers, longest first (a function: G5 forbids mutable package-level tables).
func specOps() []string { return []string{"===", "~=", "==", "!=", "<=", ">=", "<", ">"} }

func validSpecifier(sp string) bool {
	for _, part := range strings.Split(sp, ",") {
		ok := false
		for _, op := range specOps() {
			if strings.HasPrefix(part, op) && len(part) > len(op) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// ExactPin returns X when the specifier is exactly "==X" (no wildcard, no further clauses).
// Whitespace after the operator ("== 1.0", valid PEP 440) is dropped; a version that still holds whitespace or a marker separator is not a pin
// (verification VF-12-22: "== 1.0" used to return " 1.0" and "==1.0 ; python_version<'3'" returned the marker as part of the version).
func ExactPin(spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if !strings.HasPrefix(spec, "==") || strings.HasPrefix(spec, "===") || strings.ContainsAny(spec, ",*$;") {
		return "", false
	}
	v := strings.TrimSpace(spec[2:])
	if v == "" || strings.ContainsAny(v, " \t\r\n") {
		return "", false
	}
	return v, true
}
