package version

import (
	"fmt"
	"slices"
	"testing"
)

func TestSortSamples(t *testing.T) {
	cases := []struct {
		sys  System
		in   []string
		want []string
	}{
		{Maven, []string{"1.1", "1.0-SNAPSHOT", "1.0-rc1", "1.0", "1.0-alpha-1"}, []string{"1.0-alpha-1", "1.0-rc1", "1.0-SNAPSHOT", "1.0", "1.1"}},
		{NPM, []string{"1.0.0", "1.0.0-rc.1", "0.9.9", "1.0.0-alpha"}, []string{"0.9.9", "1.0.0-alpha", "1.0.0-rc.1", "1.0.0"}},
		{PyPI, []string{"1.0", "1.0a1", "1.0.dev1", "1.0.post1", "1.0rc1"}, []string{"1.0.dev1", "1.0a1", "1.0rc1", "1.0", "1.0.post1"}},
		{Go, []string{"v1.2.4", "v1.2.3", "v1.2.3-pre", "v2.0.0+incompatible"}, []string{"v1.2.3-pre", "v1.2.3", "v1.2.4", "v2.0.0+incompatible"}},
		{NuGet, []string{"1.0.0.1", "1.0", "1.0.0-beta", "1.0.0-Beta.2"}, []string{"1.0.0-beta", "1.0.0-Beta.2", "1.0", "1.0.0.1"}},
		{Opaque, []string{"b", "a", "latest"}, []string{"a", "b", "latest"}},
	}
	for _, c := range cases {
		got := slices.Clone(c.in)
		Sort(c.sys, got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v want %v", c.sys, got, c.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	for _, c := range []struct {
		sys    System
		v, req string
		ok, kn bool
	}{
		{NPM, "1.2.5", "^1.2.3", true, true},
		{NPM, "2.0.0", "^1.2.3", false, true},
		{Maven, "1.5", "[1.0,2.0)", true, true},
		{PyPI, "2.1", "~=2.0", true, true},
		{NuGet, "1.5.0", "[1.0,2.0)", true, true},
		{NPM, "banana", "^1", false, false},
		{Opaque, "1", "1", false, false},
	} {
		ok, kn := Satisfies(c.sys, c.v, c.req)
		if ok != c.ok || kn != c.kn {
			t.Errorf("%s %s %s: %v,%v want %v,%v", c.sys, c.v, c.req, ok, kn, c.ok, c.kn)
		}
	}
}

func TestMaxPrefersStable(t *testing.T) {
	if v, ok := Max(NPM, []string{"1.0.0", "2.0.0-rc.1", "1.5.0"}); !ok || v != "1.5.0" {
		t.Fatalf("got %q", v)
	}
	if v, ok := Max(NPM, []string{"2.0.0-rc.1", "2.0.0-rc.2"}); !ok || v != "2.0.0-rc.2" {
		t.Fatalf("got %q", v)
	}
}

// FuzzOrderAntisymmetric: arbitrary bytes. Transitivity is NOT claimed for arbitrary strings: the upstream Maven comparator
// is not transitive for ("00", "0A0", "0") (found by this fuzz target); the guarantee for such input is determinism
// (FuzzSortDeterministic), and the transitivity guarantee covers realistic versions (FuzzOrderTransitiveRealistic).
func FuzzOrderAntisymmetric(f *testing.F) {
	f.Add("1.0", "1.0.0")
	f.Add("v1.2.3", "v1.2.3-pre")
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, s := range allSystems {
			ab, ba := Order(s, a, b), Order(s, b, a)
			if (ab > 0) != (ba < 0) || (ab == 0) != (ba == 0) {
				t.Fatalf("%s antisymmetry %q %q: %d %d", s, a, b, ab, ba)
			}
		}
	})
}

var allSystems = []System{Maven, NPM, PyPI, NuGet, Go, Cargo, RubyGems, Composer, Default, Opaque}

var qualifiers = map[System][]string{
	Maven:    {"", "-alpha-1", "-beta-2", "-rc1", "-SNAPSHOT", "-sp1", ".Final"},
	NPM:      {"", "-alpha", "-alpha.1", "-rc.1", "+build5"},
	PyPI:     {"", "a1", "b2", "rc1", ".post1", ".dev1", "+local"},
	NuGet:    {"", "-beta", "-Beta.2", "-rc.1", ".1"},
	Go:       {"", "-pre", "-0.20230101000000-abcdefabcdef", "+incompatible"},
	Cargo:    {"", "-alpha", "-rc.1", "+meta"},
	RubyGems: {"", ".pre", ".rc1", "-beta"},
	Composer: {"", "-alpha", "-RC1", "-patch1"},
	Default:  {"", "-alpha", "-rc.1"},
	Opaque:   {""},
}

func FuzzOrderTransitiveRealistic(f *testing.F) {
	f.Add(uint8(1), uint8(0), uint8(0), uint8(0), uint8(1), uint8(2), uint8(1), uint8(0), uint8(0), uint8(3), uint8(2), uint8(1))
	f.Fuzz(func(t *testing.T, a1, a2, a3, aq, b1, b2, b3, bq, c1, c2, c3, cq uint8) {
		for _, s := range allSystems {
			q := qualifiers[s]
			mk := func(x, y, z, k uint8) string {
				v := fmt.Sprintf("%d.%d.%d%s", x%4, y%4, z%4, q[int(k)%len(q)])
				if s == Go {
					v = "v" + v
				}
				return v
			}
			a, b, c := mk(a1, a2, a3, aq), mk(b1, b2, b3, bq), mk(c1, c2, c3, cq)
			if Order(s, a, b) <= 0 && Order(s, b, c) <= 0 && Order(s, a, c) > 0 {
				t.Fatalf("%s transitivity %q %q %q", s, a, b, c)
			}
		}
	})
}

func FuzzSortDeterministic(f *testing.F) {
	f.Add("1", "1.x0", "1..rC")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		for _, s := range []System{Maven, NPM, PyPI, NuGet, Go, Cargo, RubyGems, Composer, Default, Opaque} {
			x, y := []string{a, b, c}, []string{c, a, b}
			Sort(s, x)
			Sort(s, y)
			if !slices.Equal(x, y) {
				t.Fatalf("%s order depends on input order: %v vs %v", s, x, y)
			}
		}
	})
}
