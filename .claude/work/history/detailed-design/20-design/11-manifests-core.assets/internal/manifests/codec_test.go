package manifests

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

func TestFactsRoundTrip(t *testing.T) {
	d := Dependency{Target: ComponentID{Golang, "github.com/org", "lib"}, Requirement: "v1.2.3", Version: "v1.2.3", Resolution: ResPinned, Scope: ScopeRuntime, Line: 7, Group: "require"}
	d.SetTargetDisplay("github.com/Org/Lib")
	d.SetSource(SourceGit, "https://github.com/org/lib")
	d.SetCondition("windows")
	plain := Dependency{Target: ComponentID{NPM, "scope", "x"}, Scope: ScopeDev, Confidence: ConfHeuristic}
	f := RepoFacts{TreeSHA: "t", ParsedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Units: []Unit{{ID: ".#golang", Ecosystem: Golang, Dir: ".", Dependencies: []Dependency{d, plain},
		Locked: []LockedComponent{{ID: d.Target, Version: "v1.2.3"}}, Diagnostics: []Diagnostic{{Code: "x.y", Severity: SevWarning, Message: "m", Loc: Location{Path: "go.mod", Line: 3}}}}}}
	b, err := EncodeFacts(f, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFacts(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Units[0].LockedCount != 1 || got.Units[0].Locked != nil {
		t.Fatalf("locked must be stripped and counted: %+v", got.Units[0])
	}
	f.Units[0].Locked, f.Units[0].LockedCount = nil, 1
	if !reflect.DeepEqual(got, f) {
		t.Fatalf("round trip differs:\n%+v\n%+v", got, f)
	}
	if plain.Extra != nil || got.Units[0].Dependencies[1].Extra != nil {
		t.Fatal("Extra must stay nil when unused")
	}
	if _, err := EncodeFacts(f, 10); !errors.Is(err, ErrFactsTooLarge) {
		t.Fatal(err)
	}
	if _, err := DecodeFacts([]byte(`{"schema":99,"facts":{}}`)); !errors.Is(err, ErrSchema) {
		t.Fatal(err)
	}
}

func TestLockedKeyIsBounded(t *testing.T) {
	rk := store.RepoKey{Source: "github", Owner: "acme", Name: "app"}
	short := lockedKey(rk, ".#npm", "t1")
	if short != "github/acme/app|.#npm|t1" {
		t.Fatal(short)
	}
	long := lockedKey(rk, strings.Repeat("d/", 200)+"#npm", "t1")
	if len(long) > 512 || !strings.HasPrefix(long, "h:") || long != lockedKey(rk, strings.Repeat("d/", 200)+"#npm", "t1") || long == lockedKey(rk, strings.Repeat("d/", 200)+"#npm", "t2") {
		t.Fatalf("%q", long)
	}
}

func TestLockedEncodingRoundTripAndSize(t *testing.T) {
	var lc []LockedComponent
	for i := 0; i < 600; i++ {
		sum := sha512.Sum512([]byte{byte(i), byte(i >> 8)}) // distinct, incompressible integrity digests like a real lockfile
		lc = append(lc, LockedComponent{ID: ComponentID{NPM, "types", "pkg-" + strings.Repeat("x", i%7) + string(rune('a'+i%26))}, Version: "20.11." + string(rune('0'+i%10)), Relationship: RelTransitive,
			Scope: ScopeRuntime, Integrity: "sha512:" + hex.EncodeToString(sum[:]), Loc: Location{Line: 1000 + i*7}, DependsOn: []int{i % 600, (i + 1) % 600}})
	}
	b, err := EncodeLocked(lc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLocked(b)
	if err != nil || !reflect.DeepEqual(got, lc) {
		t.Fatalf("round trip: %v", err)
	}
	t.Logf("600 locked components: %d bytes (%d per component)", len(b), len(b)/600)
	if len(b)/600 > 140 {
		t.Fatalf("encoding grew to %d bytes per component", len(b)/600)
	}
	if _, err := DecodeLocked([]byte(`[{"ID":1}]`)); err == nil {
		t.Fatal("plain JSON must not decode")
	}
	if _, err := DecodeLocked(b[:len(b)/2]); err == nil {
		t.Fatal("truncated entry must not decode")
	}
}
