package render

import (
	"bytes"
	"errors"
)

// ErrTooNested and ErrTooComplex are returned before parsing, from one linear pass over the raw bytes. goldmark has no depth or time
// limit: nested lists and block quotes are quadratic in depth (10 s for 160 KB of "- ") and inline parsing is quadratic in the length of
// ONE inline stream (a paragraph: 2.2 s for 64 KB of "*a_", about 134 s extrapolated for 512 KB). Both are measured in the design.
var (
	ErrTooNested  = errors.New("render: container nesting too deep")
	ErrTooComplex = errors.New("render: paragraph too long or document too complex")
)

const (
	maxNesting    = 64
	maxStream     = 40 << 10 // bytes of one inline stream (paragraph, list item, table row, heading); 40 KiB squared is 1.6e9, inside maxInlineCost
	maxInlineCost = 2e9      // sum of stream length squared; about 1 s at the measured worst constant (5e-10 s/B^2)
)

// checkShape applies both guards. Fenced code is skipped (it is not inline-parsed); a table row, ATX heading or list item starts a
// new stream; a blank line ends one.
func checkShape(src []byte) error {
	var (
		inFence  bool
		fenceCh  byte
		fenceLen int
		inTable  bool
		prevLine []byte
		acc      int
		cost     float64
		commit   = func() { cost += float64(acc) * float64(acc); acc = 0 }
	)
	for len(src) > 0 {
		var line []byte
		if i := bytes.IndexByte(src, '\n'); i >= 0 {
			line, src = src[:i], src[i+1:]
		} else {
			line, src = src, nil
		}
		line = bytes.TrimRight(line, "\r")
		trim := bytes.TrimLeft(line, " ")
		indent := len(line) - len(trim)
		if inFence {
			if indent < 4 && len(trim) >= fenceLen && trim[0] == fenceCh && len(bytes.TrimLeft(trim, string(fenceCh))) == 0 || indent < 4 && len(trim) >= fenceLen && trim[0] == fenceCh && len(bytes.TrimSpace(bytes.TrimLeft(trim, string(fenceCh)))) == 0 {
				inFence = false
			}
			continue
		}
		if indent < 4 && len(trim) >= 3 && (trim[0] == '`' || trim[0] == '~') {
			n := 0
			for n < len(trim) && trim[n] == trim[0] {
				n++
			}
			if n >= 3 && !(trim[0] == '`' && bytes.IndexByte(trim[n:], '`') >= 0) {
				commit()
				inFence, fenceCh, fenceLen = true, trim[0], n
				continue
			}
		}
		if len(bytes.TrimSpace(line)) == 0 {
			commit()
			inTable = false
			prevLine = nil
			continue
		}
		if len(line) > maxStream {
			return ErrTooComplex
		}
		if d := nestingOf(line); d > maxNesting {
			return ErrTooNested
		}
		switch {
		case inTable && bytes.IndexByte(line, '|') >= 0:
			acc = len(line)
			commit()
		case isDelimiterRow(trim) && prevLine != nil && bytes.IndexByte(prevLine, '|') >= 0:
			inTable = true
			acc = 0
		case len(trim) > 0 && trim[0] == '#' && isATX(trim):
			commit()
			acc = len(line)
			commit()
		case startsListItem(trim):
			commit()
			acc = len(line)
		default:
			acc += len(line) + 1
		}
		if acc > maxStream || cost > maxInlineCost {
			return ErrTooComplex
		}
		prevLine = line
	}
	commit()
	if cost > maxInlineCost {
		return ErrTooComplex
	}
	return nil
}

func isATX(t []byte) bool {
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && (n == len(t) || t[n] == ' ' || t[n] == '\t')
}

func startsListItem(t []byte) bool {
	if len(t) >= 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && (t[1] == ' ' || t[1] == '\t') {
		return true
	}
	k := 0
	for k < len(t) && k < 9 && t[k] >= '0' && t[k] <= '9' {
		k++
	}
	return k > 0 && k+1 < len(t) && (t[k] == '.' || t[k] == ')') && (t[k+1] == ' ' || t[k+1] == '\t')
}

func isDelimiterRow(t []byte) bool {
	if len(t) < 3 {
		return false
	}
	dash, pipe := false, false
	for _, c := range t {
		switch c {
		case '-':
			dash = true
		case '|':
			pipe = true
		case ':', ' ', '\t':
		default:
			return false
		}
	}
	return dash && pipe
}

// nestingOf estimates container depth of one line: block-quote and list markers in the prefix plus indentation/2 (tab = 4 columns).
func nestingOf(line []byte) int {
	depth, col, j := 0, 0, 0
	for j < len(line) {
		c := line[j]
		switch {
		case c == ' ':
			col++
			j++
		case c == '\t':
			col += 4 - col%4
			j++
		case c == '>':
			depth++
			j++
		case (c == '-' || c == '*' || c == '+') && j+1 < len(line) && (line[j+1] == ' ' || line[j+1] == '\t'):
			depth++
			j++
		case c >= '0' && c <= '9':
			k := j
			for k < len(line) && line[k] >= '0' && line[k] <= '9' && k-j < 9 {
				k++
			}
			if k+1 < len(line) && (line[k] == '.' || line[k] == ')') && (line[k+1] == ' ' || line[k+1] == '\t') {
				depth++
				j = k + 1
				continue
			}
			return depth + col/2
		default:
			return depth + col/2
		}
	}
	return depth + col/2
}
