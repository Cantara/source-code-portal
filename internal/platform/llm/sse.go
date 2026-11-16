package llm

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// event is one server-sent event.
type event struct{ name, data string }

// maxLine bounds a line of a provider's stream.
const maxLine = 1 << 20

var errLineTooLong = errors.New("llm: a stream line exceeds 1 MiB")

// events reads r as text/event-stream (WHATWG): \r\n, \n or \r line endings, comments skipped, multi-line data joined with \n, a
// last event without its blank line still dispatched. It stops at the first error, which it returns.
func events(r io.Reader, yield func(event) bool) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var name string
	var data [][]byte
	dispatch := func() bool {
		if len(data) == 0 {
			name = ""
			return true
		}
		e := event{name: name, data: string(bytes.Join(data, []byte("\n")))}
		if e.name == "" {
			e.name = "message"
		}
		name, data = "", nil
		return yield(e)
	}
	for {
		line, err := readLine(br)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if line == nil && errors.Is(err, io.EOF) {
			dispatch()
			return nil
		}
		switch {
		case len(line) == 0:
			if !dispatch() {
				return nil
			}
		case line[0] == ':':
		default:
			field, value, _ := bytes.Cut(line, []byte(":"))
			value = bytes.TrimPrefix(value, []byte(" "))
			switch string(field) {
			case "event":
				name = string(value)
			case "data":
				data = append(data, bytes.Clone(value))
			}
		}
		if errors.Is(err, io.EOF) {
			dispatch()
			return nil
		}
	}
}

// readLine reads one line without its ending; nil at the end of the input.
func readLine(br *bufio.Reader) ([]byte, error) {
	line := []byte{}
	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, io.EOF
			}
			return nil, err
		}
		switch b {
		case '\n':
			return line, nil
		case '\r':
			if next, err := br.Peek(1); err == nil && next[0] == '\n' {
				_, _ = br.ReadByte()
			}
			return line, nil
		}
		if len(line) >= maxLine {
			return nil, errLineTooLong
		}
		line = append(line, b)
	}
}
