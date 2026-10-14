package readmodel

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownRepo: Put of a non-existence slot for an entry that has no value in the existence slot.
	ErrUnknownRepo = errors.New("readmodel: unknown repository")
	// ErrInvalidSnapshot: a projector or validator rejected the candidate; the previous snapshot stays current.
	ErrInvalidSnapshot = errors.New("readmodel: candidate snapshot failed validation")
	// ErrNilValue: Put/PutGlobal of a nil interface value (an absent slot value is represented by absence, never by nil).
	ErrNilValue = errors.New("readmodel: nil slot value")
)

// UnknownRepoError satisfies job.Gone structurally (the repository no longer exists: drop the key).
type UnknownRepoError struct {
	ID   EntryID
	Slot string
}

func (e *UnknownRepoError) Error() string {
	return fmt.Sprintf("readmodel: unknown repository %q (slot %q): no value in the existence slot", e.ID, e.Slot)
}
func (e *UnknownRepoError) Is(t error) bool { return t == ErrUnknownRepo }
func (e *UnknownRepoError) Gone() bool      { return true }

// InvalidSnapshotError satisfies job.Permanent structurally (a retry would rewrite the same row).
type InvalidSnapshotError struct {
	Stage string // "projector" | "validator"
	Index int    // position in the registered list; -1 for the registry's built-in validator
	Err   error
}

func (e *InvalidSnapshotError) Error() string {
	return fmt.Sprintf("readmodel: candidate snapshot rejected by %s #%d: %v", e.Stage, e.Index, e.Err)
}
func (e *InvalidSnapshotError) Is(t error) bool { return t == ErrInvalidSnapshot }
func (e *InvalidSnapshotError) Unwrap() error   { return e.Err }
func (e *InvalidSnapshotError) Permanent() bool { return true }
