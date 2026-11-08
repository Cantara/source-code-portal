package auth

import "testing"

func TestEmailVerified(t *testing.T) {
	for in, want := range map[any]bool{true: true, false: false, "true": true, "false": false, "FALSE": false, nil: true} {
		if got := emailVerified(in, false); got != want {
			t.Errorf("emailVerified(%v) = %v, want %v", in, got, want)
		}
	}
	// Strict: only a claim that says so counts; silence does not, and an explicit answer is read the same.
	for in, want := range map[any]bool{true: true, false: false, "true": true, "false": false, nil: false, 1: false} {
		if got := emailVerified(in, true); got != want {
			t.Errorf("strict emailVerified(%v) = %v, want %v", in, got, want)
		}
	}
}
