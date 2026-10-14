package manifests

import "testing"

func TestGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pat, p string
		want   bool
	}{
		{"packages/*", "packages/a", true},
		{"packages/*", "packages/a/b", false},
		{"packages/**", "packages/a/b", true},
		{"**/src", "a/b/src", true},
		{"**/src", "src", true},
		{"a/**/z", "a/z", true},
		{"a/**/z", "a/b/c/z", true},
		{"a/?", "a/b", true},
		{"a/?", "a/bc", false},
		{"[", "a", false},
	} {
		if got := GlobMatch(c.pat, c.p); got != c.want {
			t.Errorf("GlobMatch(%q,%q)=%v want %v", c.pat, c.p, got, c.want)
		}
	}
	got := ExpandMembers("apps", []string{"../libs/*", "../../etc", "*"}, []string{"libs/a", "libs/b", "apps/web", "x"})
	if len(got) != 3 || got[0] != "apps/web" || got[1] != "libs/a" || got[2] != "libs/b" {
		t.Fatalf("got %v", got)
	}
}

func TestJoinLock(t *testing.T) {
	id := ComponentID{NPM, "", "left-pad"}
	deps := []Dependency{{Target: id, Requirement: "^1.0.0"}, {Target: ComponentID{NPM, "", "gone"}, Requirement: "1"}}
	locked := []LockedComponent{{ID: id, Version: "2.0.0", Relationship: RelDirect}}
	out, diags := JoinLock(NPM, deps, locked)
	if out[0].Version != "2.0.0" || out[0].Resolution != ResLocked {
		t.Fatalf("not joined: %+v", out[0])
	}
	codes := map[string]bool{}
	for _, d := range diags {
		codes[d.Code] = true
	}
	if !codes["lock.stale"] || !codes["lock.missing"] {
		t.Fatalf("diags %v", diags)
	}
	if deps[0].Version != "" {
		t.Fatal("input must not be mutated")
	}
}
