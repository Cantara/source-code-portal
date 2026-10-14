package linex

import "testing"

func TestLine(t *testing.T) {
	ix := New([]byte("a\nbb\n\nccc"))
	for off, want := range map[int]int{0: 1, 1: 1, 2: 2, 4: 2, 5: 3, 6: 4, 8: 4, 100: 4} {
		if got := ix.Line(off); got != want {
			t.Errorf("Line(%d) = %d, want %d", off, got, want)
		}
	}
	if ix.Count() != 4 {
		t.Errorf("Count = %d", ix.Count())
	}
	if New(nil).Count() != 1 || New(nil).Line(0) != 1 {
		t.Error("empty input is one line")
	}
}

func FuzzLine(f *testing.F) {
	f.Add([]byte("a\nb\n"), 3)
	f.Fuzz(func(t *testing.T, data []byte, off int) {
		ix := New(data)
		if l := ix.Line(off); l < 0 || l > ix.Count() {
			t.Fatalf("line %d of %d", l, ix.Count())
		}
	})
}
