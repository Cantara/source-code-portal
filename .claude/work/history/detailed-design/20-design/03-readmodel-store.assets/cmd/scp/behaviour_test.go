package scp_test

import (
	"fmt"
	"testing"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestBehaviourInterfacesMatchRealJob(t *testing.T) {
	if !job.IsGone(fmt.Errorf("x: %w", &store.NoRepoError{})) || !job.IsGone(&readmodel.UnknownRepoError{}) {
		t.Fatal("gone")
	}
	if !job.IsPermanent(&readmodel.InvalidSnapshotError{}) || job.IsGone(&readmodel.InvalidSnapshotError{}) {
		t.Fatal("permanent")
	}
}
