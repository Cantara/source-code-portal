package catalog

import (
	"regexp/syntax"
	"strings"
	"testing"
)

func TestMatcherTable(t *testing.T) {
	type c struct {
		m      Matcher
		name   string
		topics []string
		want   bool
	}
	cases := []c{
		{Matcher{KindExact, "Widget"}, "Widget", nil, true},
		{Matcher{KindExact, "Widget"}, "widget", nil, true}, // case-insensitive
		{Matcher{KindExact, "Widget"}, "Widget-Admin-SDK", nil, false},
		{Matcher{KindExact, "ACS"}, "ACSSearch", nil, false}, // a substring search would match this; exact does not
		{Matcher{KindPrefix, "Widget-"}, "Widget-TypeLib", nil, true},
		{Matcher{KindPrefix, "Widget-"}, "Widget", nil, false},
		{Matcher{KindPrefix, "IoT"}, "_non_maintained_IoT-Service", nil, false}, // a substring search would match this; prefix does not
		{Matcher{KindGlob, "*aseline*"}, "microservice-baseline", nil, true},
		{Matcher{KindGlob, "*aseline*"}, "HTTPLoadTest-Baseline", nil, true},
		{Matcher{KindGlob, "Widget-*-SDK"}, "Widget-Admin-SDK", nil, true},
		{Matcher{KindGlob, "Widget-*-SDK"}, "Widget-Admin-SDK2", nil, false},
		{Matcher{KindGlob, "a?c"}, "abc", nil, true},
		{Matcher{KindGlob, "a?c"}, "abbc", nil, false},
		{Matcher{KindGlob, "a.c"}, "abc", nil, false}, // '.' is literal in a glob
		{Matcher{KindRegexp, `(Widget|Ledger)-.*`}, "Ledger-SDK", nil, true},
		{Matcher{KindRegexp, `Widget`}, "Widget-Admin-SDK", nil, false}, // anchored: whole name
		{Matcher{KindRegexp, `(?i)widget.*`}, "Widget-Admin-SDK", nil, true},
		{Matcher{KindRegexp, `a|b`}, "xb", nil, false}, // alternation must not escape the anchors
		{Matcher{KindTopic, "widget"}, "x", []string{"iam", "Widget"}, true},
		{Matcher{KindTopic, "widget"}, "x", nil, false},
		{Matcher{KindCatchAll, "true"}, "anything", nil, true},
	}
	for _, tc := range cases {
		cm, err := tc.m.Compile()
		if err != nil {
			t.Fatalf("%v: %v", tc.m, err)
		}
		if got := cm.Match(tc.name, tc.topics); got != tc.want {
			t.Errorf("%s %q on %q: got %v want %v", tc.m.Kind, tc.m.Value, tc.name, got, tc.want)
		}
	}
}

func TestMatcherRejects(t *testing.T) {
	for _, m := range []Matcher{
		{KindExact, ""}, {KindExact, "a b"}, {KindPrefix, "x/y"}, {KindGlob, "a[b]"}, {KindGlob, "a**b**c**d**e**f**g**h"},
		{KindRegexp, "("}, {KindRegexp, "a)|(b"}, {KindRegexp, `(a{1000}){1000}`}, {KindRegexp, strings.Repeat("a", 300)},
		{KindTopic, "Has Space"}, {KindTopic, "-lead"}, {"nope", "x"},
	} {
		if _, err := m.Compile(); err == nil {
			t.Errorf("%v compiled", m)
		}
	}
}

// The wrapper ^(?:p)$ must be a single concatenation for every p that parses on its own.
func TestAnchorWrapperNeverEscapes(t *testing.T) {
	for _, p := range []string{"a|b", "a|", "|b", "(a|b)c", "x*", "(?i)a|b", `\d+|[a-z]`, ""} {
		if _, err := syntax.Parse(p, syntax.Perl); err != nil {
			continue
		}
		re, _, err := compileAnchored(p)
		if err != nil {
			continue
		}
		parsed, err := syntax.Parse(re.String(), syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Op == syntax.OpAlternate {
			t.Errorf("%q: wrapped pattern %q is a top-level alternation", p, re.String())
		}
	}
}

func FuzzMatcherCompile(f *testing.F) {
	for _, s := range []string{"Widget*", "a|b", "(", "[^\\s]*aseline*", "a**", "(?i)x", "\xff", "a{1000,}"} {
		f.Add(string(KindGlob), s, "Widget-X")
		f.Add(string(KindRegexp), s, "Widget-X")
		f.Add(string(KindPrefix), s, "Widget-X")
	}
	f.Fuzz(func(t *testing.T, kind, value, name string) {
		cm, err := Matcher{MatcherKind(kind), value}.Compile()
		if err != nil {
			return
		}
		a := cm.Match(name, nil)
		if b := cm.Match(name, nil); a != b {
			t.Fatal("not deterministic")
		}
	})
}
