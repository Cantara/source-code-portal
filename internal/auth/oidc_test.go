package auth

import "testing"

func TestEmailVerified(t *testing.T) {
	for in, want := range map[any]bool{true: true, false: false, "true": true, "false": false, "FALSE": false, nil: true} {
		if got := emailVerified(in); got != want {
			t.Errorf("emailVerified(%v) = %v, want %v", in, got, want)
		}
	}
}
