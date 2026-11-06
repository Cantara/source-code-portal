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

// Tag characters spell ASCII a reader cannot see: ASCII smuggling.
func TestInvisibleTextIsStripped(t *testing.T) {
	smuggled := "ok" + string([]rune{0xe0049, 0xe0067, 0xe006e, 0x00ad, 0xe0100, 0x2061}) + "!"
	if got := Text(smuggled, 100); got != "ok!" {
		t.Errorf("Text = %q, want ok!", got)
	}
	if got := Strip("a\nb\t" + string(rune(0xe0041))); got != "a\nb\t" {
		t.Errorf("Strip = %q", got)
	}
	if got := Text("❤️", 100); got != "❤️" {
		t.Errorf("an emoji variation selector was stripped: %q", got)
	}
}
