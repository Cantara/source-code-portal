package untrusted

import "testing"

func TestText(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"plain", 10, "plain"},
		{"a\u202eb\x07c\u200bd", 10, "abcd"},
		{"  lead\n\tand  inner  ", 20, "lead and inner"},
		{"abcdef", 4, "abc…"},
		{"abcd", 4, "abcd"},
		{"ab cdef", 4, "ab…"},
		{"æøå日本語", 5, "æøå日…"},
	}
	for _, tc := range tests {
		if got := Text(tc.in, tc.max); got != tc.want {
			t.Errorf("Text(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("\n subject\nbody\n"); got != "subject" {
		t.Errorf("FirstLine = %q", got)
	}
}
