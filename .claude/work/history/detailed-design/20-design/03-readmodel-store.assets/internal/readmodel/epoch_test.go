package readmodel_test

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/readmodel"
)

// TestEpochSurvivesApply: the epoch given in Deps is on the empty snapshot and on every published one (CD-IF-03).
func TestEpochSurvivesApply(t *testing.T) {
	reg := &readmodel.Registry{}
	readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	p := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{Epoch: "4f7a1c2e9b3d5e60"})
	if got := p.Current().Epoch(); got != "4f7a1c2e9b3d5e60" {
		t.Fatalf("initial epoch %q", got)
	}
	snap, err := p.Apply(func(*readmodel.Builder) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if snap.Epoch() != "4f7a1c2e9b3d5e60" || p.Current().Epoch() != snap.Epoch() {
		t.Fatalf("epoch lost on publish: %q", snap.Epoch())
	}
}
