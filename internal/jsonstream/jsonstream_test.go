package jsonstream

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

// read reads the array of objects in doc, keeping each object's string "s" within head and tail bytes.
func read(t *testing.T, doc string, head, tail int) (starts, ends []string, cuts []bool) {
	t.Helper()
	d := New(strings.NewReader(doc))
	err := d.Array(func() error {
		return d.Object(func(key string) error {
			if key != "s" {
				return d.Skip()
			}
			start, end, cut, err := d.Ends(head, tail)
			starts, ends, cuts = append(starts, start), append(ends, end), append(cuts, cut)
			return err
		})
	})
	if err != nil {
		t.Fatalf("read %q: %v", doc, err)
	}
	return starts, ends, cuts
}

func TestReadsWhatIsAskedAndSkipsTheRest(t *testing.T) {
	doc := ` [ {"x": {"deep": [1, -2.5e3, true, false, null, {"s": "not this"}, "a \"quoted\" ]} string"]}, "s": "first", "y": null},
		{"s": null}, {"s": 42}, {"s": {"an": "object"}}, {}, {"s": "esc \n\t\\ \/ é 😀 \ud800"} ] `
	starts, _, cuts := read(t, doc, 100, 0)
	want := []string{"first", "", "", "", "esc \n\t\\ / é 😀 �"}
	if strings.Join(starts, "|") != strings.Join(want, "|") {
		t.Errorf("strings = %q, want %q", starts, want)
	}
	for i, c := range cuts {
		if c {
			t.Errorf("string %d is cut", i)
		}
	}
}

func TestLongStringKeepsItsStartAndEnd(t *testing.T) {
	body := "Subject\n\n" + strings.Repeat("é and more text. ", 100_000) + "\n\nCo-authored-by: Claude <noreply@anthropic.com>"
	b, _ := json.Marshal(map[string]string{"s": body})
	starts, ends, cuts := read(t, "["+string(b)+"]", 1000, 100)
	if !cuts[0] || !strings.HasPrefix(starts[0], "Subject\n\n") || !strings.HasSuffix(ends[0], "Co-authored-by: Claude <noreply@anthropic.com>") {
		t.Fatalf("cut = %v, start %q…, end …%q", cuts[0], starts[0][:20], ends[0])
	}
	if len(starts[0]) > 1000 || len(ends[0]) > 100 || !utf8.ValidString(starts[0]) || !utf8.ValidString(ends[0]) {
		t.Errorf("start %d bytes, end %d bytes, valid %v %v", len(starts[0]), len(ends[0]), utf8.ValidString(starts[0]), utf8.ValidString(ends[0]))
	}
}

func TestStringThatFitsIsWhole(t *testing.T) {
	for _, n := range []int{0, 5, 10, 15} {
		s := strings.Repeat("ab", n) // up to 30 bytes against head 10 and tail 20
		starts, ends, cuts := read(t, `[{"s":"`+s+`"}]`, 10, 20)
		if starts[0] != s || ends[0] != "" || cuts[0] {
			t.Errorf("%d bytes: start %q end %q cut %v", len(s), starts[0], ends[0], cuts[0])
		}
	}
	starts, ends, cuts := read(t, `[{"s":"`+strings.Repeat("ab", 16)+`"}]`, 10, 20) // 32 bytes: two dropped
	if starts[0] != "ababababab" || ends[0] != "abababababababababab" || !cuts[0] {
		t.Errorf("32 bytes: start %q end %q cut %v", starts[0], ends[0], cuts[0])
	}
}

// The reader holds its buffer and what it keeps, whatever the document's size: a huge document streams through.
func TestHugeDocumentStreams(t *testing.T) {
	const n = 20
	r, w := io.Pipe()
	go func() {
		_, _ = io.WriteString(w, "[")
		for i := range n {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, `{"skipped":"`+strings.Repeat("x", 1<<20)+`","s":"`+strings.Repeat("y", 1<<20)+`"}`)
		}
		_, err := io.WriteString(w, "]")
		_ = w.CloseWithError(err)
	}()
	d, count := New(r), 0
	err := d.Array(func() error {
		return d.Object(func(key string) error {
			if key != "s" {
				return d.Skip()
			}
			s, err := d.String(16)
			if s != strings.Repeat("y", 16) {
				t.Errorf("s = %q", s)
			}
			count++
			return err
		})
	})
	if err != nil || count != n {
		t.Fatalf("read %d, %v", count, err)
	}
}

func TestBrokenDocuments(t *testing.T) {
	for _, doc := range []string{`[{"s": "open`, `[{"s" "x"}]`, `[{"s": "x"]`, `[1 2]`, `[{"s": "\x"}]`, `[{"s": "\u12G4"}]`, `[}`, `[{"a": [}]`, "[\"\x01\"]"} {
		d := New(strings.NewReader(doc))
		err := d.Array(func() error { return d.Object(func(string) error { return d.Skip() }) })
		if err == nil || !errors.Is(err, ErrSyntax) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%q: err = %v, want a syntax error or an unexpected end", doc, err)
		}
	}
}

// FuzzStrings: every string of a valid document reads as encoding/json reads it, whole when it fits, its start and end when not.
func FuzzStrings(f *testing.F) {
	for _, s := range []string{"", "plain", "é😀", "\n\"\\", "\xed\xa0\x80", strings.Repeat("ab€", 40)} {
		f.Add(s, 8, 8)
	}
	f.Fuzz(func(t *testing.T, s string, head, tail int) {
		head, tail = abs(head)%64, abs(tail)%64
		b, _ := json.Marshal(map[string]string{"s": s})
		var want map[string]string
		if err := json.Unmarshal(b, &want); err != nil {
			t.Skip()
		}
		starts, ends, cuts := read(t, "["+string(b)+"]", head, tail)
		full := want["s"]
		if !cuts[0] {
			if starts[0] != full {
				t.Fatalf("whole: %q, want %q", starts[0], full)
			}
			return
		}
		if len(full) <= head+tail || !strings.HasPrefix(full, starts[0]) || !strings.HasSuffix(full, ends[0]) ||
			len(starts[0]) > head || len(ends[0]) > tail || !utf8.ValidString(starts[0]) || !utf8.ValidString(ends[0]) {
			t.Fatalf("cut %q into %q … %q (head %d, tail %d)", full, starts[0], ends[0], head, tail)
		}
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
