package gradle

import (
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type tkind uint8

const (
	tEOF tkind = iota
	tIdent
	tStr // string literal; templ=true for double-quoted forms that may contain $ templates
	tNum
	tPunct
	tNL
)

type tok struct {
	k     tkind
	s     string // identifier text, decoded string content, number text, punctuation
	line  int
	templ bool
}

// lex tokenises Groovy and Kotlin DSL sources lexically. Comments are dropped; newlines are kept (Groovy command calls end
// at a newline). It never fails: unterminated strings and comments end at end of input. The guard counts tokens and polls
// the context.
func lex(src []byte, g *limits.Guard) ([]tok, error) {
	var out []tok
	line := 1
	i, n := 0, len(src)
	emit := func(t tok) error {
		if err := g.Node(); err != nil {
			return err
		}
		out = append(out, t)
		return nil
	}
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			if err := emit(tok{k: tNL, line: line}); err != nil {
				return nil, err
			}
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '#' && i == 0 && i+1 < n && src[i+1] == '!':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '"' || c == '\'':
			start := line
			triple := i+2 < n && src[i+1] == c && src[i+2] == c
			var b strings.Builder
			if triple {
				i += 3
				for i < n && !(src[i] == c && i+2 < n && src[i+1] == c && src[i+2] == c) {
					if src[i] == '\n' {
						line++
					}
					if b.Len() < 1<<16 {
						b.WriteByte(src[i])
					}
					i++
				}
				i += 3
			} else {
				i++
				depth := 0 // ${ ... } nesting inside double-quoted strings
				for i < n {
					ch := src[i]
					if depth == 0 && ch == c {
						break
					}
					if ch == '\n' && depth == 0 {
						break // unterminated single-line string
					}
					if ch == '\n' {
						line++
					}
					if c == '"' && ch == '$' && i+1 < n && src[i+1] == '{' {
						depth++
						b.WriteString("${")
						i += 2
						continue
					}
					if depth > 0 {
						if ch == '{' {
							depth++
						} else if ch == '}' {
							depth--
						}
						if b.Len() < 1<<16 {
							b.WriteByte(ch)
						}
						i++
						continue
					}
					if ch == '\\' && i+1 < n {
						i++
						switch src[i] {
						case 'n':
							b.WriteByte('\n')
						case 't':
							b.WriteByte('\t')
						default:
							b.WriteByte(src[i])
						}
						i++
						continue
					}
					if b.Len() < 1<<16 {
						b.WriteByte(ch)
					}
					i++
				}
				if i < n && src[i] == c {
					i++
				}
			}
			if err := emit(tok{k: tStr, s: b.String(), line: start, templ: c == '"'}); err != nil {
				return nil, err
			}
		case c == '`':
			j := i + 1
			for j < n && src[j] != '`' && src[j] != '\n' {
				j++
			}
			if err := emit(tok{k: tIdent, s: string(src[i+1 : j]), line: line}); err != nil {
				return nil, err
			}
			i = j + 1
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(src[j]) {
				j++
			}
			if err := emit(tok{k: tIdent, s: string(src[i:j]), line: line}); err != nil {
				return nil, err
			}
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && (isIdentPart(src[j]) || src[j] == '.') {
				j++
			}
			if err := emit(tok{k: tNum, s: string(src[i:j]), line: line}); err != nil {
				return nil, err
			}
			i = j
		default:
			s := string(c)
			if i+1 < n {
				switch two := string(src[i : i+2]); two {
				case "?.", "::", "->", "+=", "==", "!=", "&&", "||", "<<", "?:", "..":
					s = two
				}
			}
			if err := emit(tok{k: tPunct, s: s, line: line}); err != nil {
				return nil, err
			}
			i += len(s)
		}
	}
	return out, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }
