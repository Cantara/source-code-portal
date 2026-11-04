// Package jsonstream reads JSON as a stream in bounded memory: a value not asked for is skipped as it is read, never held, and a
// string keeps at most the bytes asked for, its start and its end. A document of any size is read through one 64 KiB buffer, so a
// repository whose data is larger than any limit is read in part, never refused. A value of another kind than the one asked for
// (a null, a number where a string was expected) is skipped and reads as empty.
package jsonstream

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	bufferSize = 64 << 10
	maxKey     = 256 // bytes of an object's key kept; a longer key matches nothing a caller asks for
)

// ErrSyntax is wrapped by every error of a document that is not JSON. Any other error is the reader's.
var ErrSyntax = errors.New("jsonstream: not JSON")

// Reader reads one JSON document.
type Reader struct {
	r   *bufio.Reader
	off int64 // bytes consumed, for errors
}

// New reads from r.
func New(r io.Reader) *Reader { return &Reader{r: bufio.NewReaderSize(r, bufferSize)} }

func (d *Reader) syntax(format string, args ...any) error {
	return fmt.Errorf("%w at byte %d: %s", ErrSyntax, d.off, fmt.Sprintf(format, args...))
}

func (d *Reader) byte() (byte, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}
	d.off++
	return b, nil
}

// peek is the next byte that is not white space, left unread.
func (d *Reader) peek() (byte, error) {
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		switch b {
		case ' ', '\t', '\n', '\r':
			d.off++
			continue
		}
		if err := d.r.UnreadByte(); err != nil {
			return 0, err
		}
		return b, nil
	}
}

// Array calls each for every element of an array; each reads the element with one of the Reader's methods. Anything but an array
// is skipped and calls nothing.
func (d *Reader) Array(each func() error) error {
	b, err := d.peek()
	if err != nil {
		return err
	}
	if b != '[' {
		return d.Skip()
	}
	_, _ = d.byte()
	if b, err = d.peek(); err != nil {
		return err
	}
	if b == ']' {
		_, _ = d.byte()
		return nil
	}
	for {
		if err := each(); err != nil {
			return err
		}
		if b, err = d.peek(); err != nil {
			return err
		}
		_, _ = d.byte()
		switch b {
		case ',':
		case ']':
			return nil
		default:
			return d.syntax("%q after an array element", b)
		}
	}
}

// Object calls each for every member of an object with its key (its first 256 bytes); each reads the value with one of the Reader's
// methods, Skip for one it does not need. Anything but an object is skipped and calls nothing.
func (d *Reader) Object(each func(key string) error) error {
	b, err := d.peek()
	if err != nil {
		return err
	}
	if b != '{' {
		return d.Skip()
	}
	_, _ = d.byte()
	if b, err = d.peek(); err != nil {
		return err
	}
	if b == '}' {
		_, _ = d.byte()
		return nil
	}
	for {
		if b, err = d.peek(); err != nil {
			return err
		}
		if b != '"' {
			return d.syntax("%q for an object's key", b)
		}
		key, _, _, err := d.str(maxKey, 0)
		if err != nil {
			return err
		}
		if b, err = d.peek(); err != nil {
			return err
		}
		if _, _ = d.byte(); b != ':' {
			return d.syntax("%q after an object's key", b)
		}
		if err := each(key); err != nil {
			return err
		}
		if b, err = d.peek(); err != nil {
			return err
		}
		_, _ = d.byte()
		switch b {
		case ',':
		case '}':
			return nil
		default:
			return d.syntax("%q after an object's member", b)
		}
	}
}

// String reads a string and keeps its first max bytes (never a broken character); anything but a string reads as "".
func (d *Reader) String(max int) (string, error) {
	s, _, _, err := d.Ends(max, 0)
	return s, err
}

// Ends reads a string and keeps its start and its end: up to head bytes of the start and tail bytes of the end. A string that fits
// in head+tail is all in start, end empty and cut false; a longer one is cut, the middle dropped as it is read. Anything but a string
// reads as empty.
func (d *Reader) Ends(head, tail int) (start, end string, cut bool, err error) {
	b, err := d.peek()
	if err != nil {
		return "", "", false, err
	}
	if b != '"' {
		return "", "", false, d.Skip()
	}
	return d.str(head, tail)
}

// str reads the string at the reader into a bounded start and end.
func (d *Reader) str(head, tail int) (string, string, bool, error) {
	_, _ = d.byte() // the opening quote
	s := sink{head: make([]byte, 0, min(head, 4096)), headCap: head, ring: make([]byte, tail)}
	for {
		b, err := d.byte()
		if err != nil {
			return "", "", false, err
		}
		switch {
		case b == '"':
			start, end, cut := s.result()
			return start, end, cut, nil
		case b == '\\':
			r, err := d.escape()
			if err != nil {
				return "", "", false, err
			}
			var enc [utf8.UTFMax]byte
			for _, c := range enc[:utf8.EncodeRune(enc[:], r)] {
				s.add(c)
			}
		case b < 0x20:
			return "", "", false, d.syntax("a control character in a string")
		default:
			s.add(b)
		}
	}
}

// escape reads an escape after its backslash, a surrogate pair as one rune.
func (d *Reader) escape() (rune, error) {
	b, err := d.byte()
	if err != nil {
		return 0, err
	}
	switch b {
	case '"', '\\', '/':
		return rune(b), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
	default:
		return 0, d.syntax("the escape \\%c", b)
	}
	r, err := d.hex4()
	if err != nil || !utf16.IsSurrogate(r) {
		return r, err
	}
	// A high surrogate wants its low one as the next escape; a lone one is the replacement character, as encoding/json reads it.
	if next, err := d.r.Peek(2); err != nil || next[0] != '\\' || next[1] != 'u' {
		return utf8.RuneError, nil //nolint:nilerr // a lone surrogate is not an error of the document
	}
	_, _ = d.byte()
	_, _ = d.byte()
	low, err := d.hex4()
	if err != nil {
		return 0, err
	}
	if pair := utf16.DecodeRune(r, low); pair != utf8.RuneError {
		return pair, nil
	}
	return utf8.RuneError, nil
}

func (d *Reader) hex4() (rune, error) {
	var r rune
	for range 4 {
		b, err := d.byte()
		if err != nil {
			return 0, err
		}
		switch {
		case b >= '0' && b <= '9':
			r = r<<4 | rune(b-'0')
		case b >= 'a' && b <= 'f':
			r = r<<4 | rune(b-'a'+10)
		case b >= 'A' && b <= 'F':
			r = r<<4 | rune(b-'A'+10)
		default:
			return 0, d.syntax("%q in a \\u escape", b)
		}
	}
	return r, nil
}

// Skip reads the next value, of any kind and size, and keeps nothing of it.
func (d *Reader) Skip() error {
	b, err := d.peek()
	if err != nil {
		return err
	}
	switch {
	case b == '"':
		_, _, _, err := d.str(0, 0)
		return err
	case b == '{' || b == '[':
		return d.skipNested()
	case b == '-' || b >= '0' && b <= '9':
		return d.literal("")
	case b == 't':
		return d.literal("true")
	case b == 'f':
		return d.literal("false")
	case b == 'n':
		return d.literal("null")
	}
	return d.syntax("%q for a value", b)
}

// skipNested reads an object or an array to its end, keeping only the closing bracket of each value still open (a byte per level).
func (d *Reader) skipNested() error {
	var stack []byte
	for {
		b, err := d.peek()
		if err != nil {
			return err
		}
		switch b {
		case '"':
			if _, _, _, err := d.str(0, 0); err != nil {
				return err
			}
			continue
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != b {
				return d.syntax("an unmatched %q", b)
			}
			stack = stack[:len(stack)-1]
		}
		_, _ = d.byte()
		if len(stack) == 0 {
			return nil
		}
	}
}

// literal reads a number (want "") or the literal want.
func (d *Reader) literal(want string) error {
	if want != "" {
		for i := range len(want) {
			b, err := d.byte()
			if err != nil {
				return err
			}
			if b != want[i] {
				return d.syntax("%q in %s", b, want)
			}
		}
		return nil
	}
	for {
		b, err := d.r.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if b == '-' || b == '+' || b == '.' || b == 'e' || b == 'E' || b >= '0' && b <= '9' {
			d.off++
			continue
		}
		return d.r.UnreadByte()
	}
}

// sink keeps a string's first headCap bytes and its last len(ring) bytes.
type sink struct {
	head    []byte
	headCap int
	ring    []byte
	next    int   // where the ring writes next
	past    int64 // bytes past the head
}

func (s *sink) add(b byte) {
	if len(s.head) < s.headCap {
		s.head = append(s.head, b)
		return
	}
	if len(s.ring) > 0 {
		s.ring[s.next] = b
		s.next = (s.next + 1) % len(s.ring)
	}
	s.past++
}

// result is the start and the end, each cut at whole characters, and whether the middle was dropped.
func (s *sink) result() (string, string, bool) {
	n := int(min(s.past, int64(len(s.ring))))
	end := make([]byte, 0, n)
	if s.past > int64(len(s.ring)) {
		end = append(append(end, s.ring[s.next:]...), s.ring[:s.next]...)
	} else {
		end = append(end, s.ring[:n]...)
	}
	cut := s.past > int64(len(s.ring))
	if !cut {
		return string(s.head) + string(end), "", false // all of it, in order: the head and what followed
	}
	return string(trimEnd(s.head)), string(trimStart(end)), true
}

// trimEnd drops a character cut at the end of b.
func trimEnd(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

// trimStart drops a character cut at the start of b.
func trimStart(b []byte) []byte {
	for i := range b {
		if utf8.RuneStart(b[i]) {
			return b[i:]
		}
	}
	return nil
}
