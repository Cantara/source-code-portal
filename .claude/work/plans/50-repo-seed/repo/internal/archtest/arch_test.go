package archtest_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/cantara/source-code-portal/internal/archtest"
)

// TestRealTree is the mandatory CI gate (ADR-018): the module's real packages against the table. CI runs it with -count=1.
func TestRealTree(t *testing.T) {
	_, pkgs, err := archtest.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range archtest.Check(archtest.Rules(), archtest.OwnedImports(), pkgs) {
		t.Error(v)
	}
}

// TestTableIsAcyclic is the executable proof of architecture 5.4: the table alone, no tree needed.
func TestTableIsAcyclic(t *testing.T) {
	for _, v := range archtest.CheckTable(archtest.Rules()) {
		t.Error(v)
	}
}

var update = flag.Bool("update", false, "rewrite the generated depguard block of .golangci.yml")

// TestDepguardInSync fails when the generated block of .golangci.yml differs from the table (drift = CI failure, ADR-018).
func TestDepguardInSync(t *testing.T) {
	const module = "github.com/cantara/source-code-portal"
	path := filepath.Join("..", "..", ".golangci.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, ok := archtest.Splice(string(raw), archtest.GenerateDepguard(module, archtest.Rules(), archtest.OwnedImports()))
	if !ok {
		t.Fatal(".golangci.yml has no BEGIN/END GENERATED depguard markers")
	}
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want != string(raw) {
		t.Fatal("generated depguard rules are out of date: run `make arch-fix` (go test ./internal/archtest -run DepguardInSync -update)")
	}
}

// TestRealSource applies the AST rules (route literals, skips without issue, unowned TODOs) to the hand-written source.
func TestRealSource(t *testing.T) {
	vs, err := archtest.ScanSource("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Error(v)
	}
}
