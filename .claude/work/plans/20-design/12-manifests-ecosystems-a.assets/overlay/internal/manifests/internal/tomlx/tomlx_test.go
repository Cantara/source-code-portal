package tomlx

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDecodeIndex(t *testing.T) {
	src := "[package]\nname = \"a\"\n\n[dependencies]\nserde = \"1\"\nfoo.workspace = true\n\n[[bin]]\nname = \"x\"\n\n[[bin]]\nname = \"y\"\n"
	tree, ix, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"package": 1, "package.name": 2, "dependencies.serde": 5, "dependencies.foo": 6, "dependencies.foo.workspace": 6, "bin[0]": 8, "bin[1]": 11, "bin[1].name": 12} {
		if got := ix[key]; got != want {
			t.Errorf("line of %q = %d, want %d", key, got, want)
		}
	}
	if s, _ := Str(At(tree, "package", "name")); s != "a" || At(tree, "package", "nope", "x") != nil {
		t.Error("accessors")
	}
	if ks := ix.KeysBySource("dependencies", Map(tree["dependencies"])); len(ks) != 2 || ks[0] != "serde" {
		t.Errorf("source order %v", ks)
	}
}

func TestKeyDepthCap(t *testing.T) {
	_, _, err := Decode([]byte("[" + strings.Repeat("a.", 100000) + "a]\nx=1\n"))
	if !errors.Is(err, ErrDepth) || Code(err, "cargo.syntax") != "limit.depth" {
		t.Fatalf("err=%v", err)
	}
	_, _, err = Decode([]byte(strings.Repeat("a.", 100000) + "a = 1\n"))
	if !errors.Is(err, ErrDepth) {
		t.Fatalf("dotted key: %v", err)
	}
	if _, _, err = Decode([]byte("[a\nb")); err == nil || Code(err, "x.syntax") != "x.syntax" {
		t.Fatalf("syntax error: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("[a]\nb = 1\n[[c]]\nd.e = 2\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<14 {
			return
		}
		_, _, _ = Decode(data)
	})
}

// TestDecodeIsLinearInTheNumberOfTables (VF-12-01): the line index must not rescan the document per key. 40,000 array-of-tables elements
// (about 1.4 MiB, like a large Cargo.lock) took 25 s with unstable.Parser.Shape; the bound below is generous for any machine.
func TestDecodeIsLinearInTheNumberOfTables(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40000; i++ {
		fmt.Fprintf(&b, "[[package]]\nname = \"crate%d\"\nversion = \"1.0.%d\"\n\n", i, i)
	}
	st := time.Now()
	tree, ix, err := Decode([]byte(b.String()))
	if err != nil || len(List(tree["package"])) != 40000 {
		t.Fatalf("err=%v", err)
	}
	if got := ix["package[39999].name"]; got != 4*39999+2 {
		t.Errorf("line of the last name = %d", got)
	}
	if d := time.Since(st); d > 3*time.Second {
		t.Errorf("Decode took %v for 1.4 MiB of tables", d)
	}
}

// TestKeysPerTableCap (VF-12-01b): one table with a huge number of direct keys is refused before go-toml's quadratic duplicate check runs.
func TestKeysPerTableCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("[dependencies]\n")
	for i := 0; i <= MaxKeysPerTable; i++ {
		fmt.Fprintf(&b, "d%d = \"1\"\n", i)
	}
	st := time.Now()
	_, _, err := Decode([]byte(b.String()))
	if !errors.Is(err, ErrKeys) || Code(err, "cargo.syntax") != "limit.nodes" {
		t.Fatalf("err=%v", err)
	}
	if d := time.Since(st); d > time.Second {
		t.Errorf("refusal took %v", d)
	}
	// the cap is per table, not per file: 30 tables of 1,000 keys (30,000 keys in all) are fine
	b.Reset()
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "[t%d]\n", i)
		for j := 0; j < 1000; j++ {
			fmt.Fprintf(&b, "k%d = 1\n", j)
		}
	}
	if _, _, err := Decode([]byte(b.String())); err != nil {
		t.Fatalf("30 tables of 1,000 keys: %v", err)
	}
}
