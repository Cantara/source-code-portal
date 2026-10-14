package gradle

import (
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

// handler receives the statements of a Groovy or Kotlin DSL file. Everything is lexical: nothing is evaluated.
type handler struct {
	// enter is called for "name { ... }" and "name(args) { ... }"; the returned label is pushed on the block stack.
	enter  func(stack []string, name string, args [][]tok, line int) string
	call   func(stack []string, name string, args [][]tok, tail []tok, line int)
	assign func(stack []string, lhs string, rhs []tok, line int)
	decl   func(stack []string, name string, rhs []tok, line int)
}

type scanner struct {
	t     []tok
	i     int
	g     *limits.Guard
	h     handler
	stack []string
	err   error
}

// isModifier reports declaration modifiers and type names that precede a variable name ("val x", "String v").
func isModifier(name string) bool {
	switch name {
	case "val", "var", "def", "final", "const", "private", "public", "protected", "static", "lateinit", "override", "internal",
		"String", "Integer", "int", "boolean", "Boolean", "Object", "List", "Map", "Set":
		return true
	}
	return false
}

func (s *scanner) peek() tok {
	if s.i < len(s.t) {
		return s.t[s.i]
	}
	return tok{k: tEOF}
}

func (s *scanner) isP(p string) bool { t := s.peek(); return t.k == tPunct && t.s == p }

func scan(t []tok, g *limits.Guard, h handler) error {
	s := &scanner{t: t, g: g, h: h}
	for s.i < len(s.t) && s.err == nil {
		before := s.i
		s.block()
		if s.i == before || s.isP("}") { // stray closing brace: skip it
			s.i++
		}
	}
	return s.err
}

func (s *scanner) skipSep() {
	for s.i < len(s.t) {
		t := s.t[s.i]
		if t.k == tNL || (t.k == tPunct && t.s == ";") {
			s.i++
			continue
		}
		return
	}
}

// block parses statements until a closing brace (not consumed) or end of input.
func (s *scanner) block() {
	for s.err == nil {
		s.skipSep()
		if s.i >= len(s.t) || s.isP("}") {
			return
		}
		if err := s.g.Node(); err != nil {
			s.err = err
			return
		}
		before := s.i
		s.stmt()
		if s.i == before {
			s.i++
		}
	}
}

func (s *scanner) openBlock(label string) {
	if err := s.g.Enter(); err != nil {
		s.err = err
		return
	}
	s.i++ // '{'
	s.stack = append(s.stack, label)
	s.block()
	if s.isP("}") {
		s.i++
	}
	s.stack = s.stack[:len(s.stack)-1]
	s.g.Leave()
}

// chain reads ident ('.'|'?.' ident)* with optional [..] suffixes folded into the name.
func (s *scanner) chain() string {
	var b strings.Builder
	for s.peek().k == tIdent {
		b.WriteString(s.peek().s)
		s.i++
		for s.isP("[") { // extra["x"] / ext["x"]
			s.i++
			for s.i < len(s.t) && !s.isP("]") && s.peek().k != tNL {
				if t := s.peek(); t.k == tStr || t.k == tIdent {
					b.WriteString("[" + t.s + "]")
				}
				s.i++
			}
			if s.isP("]") {
				s.i++
			}
		}
		if s.isP(".") || s.isP("?.") {
			if s.i+1 < len(s.t) && s.t[s.i+1].k == tIdent {
				b.WriteByte('.')
				s.i++
				continue
			}
		}
		break
	}
	return b.String()
}

func (s *scanner) stmt() {
	start := s.peek()
	var name string
	if start.k == tStr && s.i+1 < len(s.t) && s.t[s.i+1].k == tPunct && s.t[s.i+1].s == "(" {
		name = start.s // Kotlin: "implementation"("g:a:v")
		s.i++
	} else if start.k != tIdent {
		s.collectExpr()
		return
	} else {
		name = s.chain()
	}
	line := start.line
	if name == "else" {
		return
	}
	// declarations: val x = ..., def x = ..., String x = ...
	if isModifier(name) && s.peek().k == tIdent {
		for isModifier(s.peek().s) && s.i+1 < len(s.t) && s.t[s.i+1].k == tIdent {
			s.i++
		}
		id := s.peek().s
		s.i++
		if s.isP(":") { // Kotlin type annotation
			for s.i < len(s.t) && !s.isP("=") && s.peek().k != tNL {
				s.i++
			}
		}
		if s.isP("=") {
			s.i++
			rhs := s.collectExpr()
			if s.h.decl != nil {
				s.h.decl(s.stack, id, rhs, line)
			}
			return
		}
		s.collectExpr()
		return
	}
	switch t := s.peek(); {
	case t.k == tPunct && t.s == "(":
		args := s.parseArgs()
		if s.isP("{") {
			label := name
			if s.h.enter != nil {
				label = s.h.enter(s.stack, name, args, line)
			}
			if s.h.call != nil {
				s.h.call(s.stack, name, args, nil, line)
			}
			s.openBlock(label)
			return
		}
		tail := s.collectExpr()
		if s.h.call != nil {
			s.h.call(s.stack, name, args, tail, line)
		}
	case t.k == tPunct && t.s == "{":
		label := name
		if s.h.enter != nil {
			label = s.h.enter(s.stack, name, nil, line)
		}
		s.openBlock(label)
	case t.k == tPunct && (t.s == "=" || t.s == "+="):
		s.i++
		rhs := s.collectExpr()
		if s.h.assign != nil {
			s.h.assign(s.stack, name, rhs, line)
		}
	case t.k == tNL || t.k == tEOF || (t.k == tPunct && (t.s == ";" || t.s == "}")):
		if s.h.call != nil {
			s.h.call(s.stack, name, nil, nil, line)
		}
	case t.k == tPunct && (t.s == "." || t.s == "[" || t.s == "-" || t.s == "+" || t.s == "<" || t.s == "?" || t.s == "!"):
		s.collectExpr()
	default:
		raw := s.collectExpr()
		if s.h.call != nil {
			s.h.call(s.stack, name, splitTop(raw), nil, line)
		}
	}
}

// parseArgs reads "( ... )" and splits the content at top-level commas.
func (s *scanner) parseArgs() [][]tok {
	s.i++ // '('
	var cur []tok
	var args [][]tok
	depth := 0
	for s.i < len(s.t) {
		t := s.t[s.i]
		if err := s.g.Node(); err != nil {
			s.err = err
			return args
		}
		if t.k == tPunct {
			switch t.s {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth == 0 {
					if t.s == ")" {
						s.i++
					}
					if len(cur) > 0 {
						args = append(args, cur)
					}
					return args
				}
				depth--
			case ",":
				if depth == 0 {
					args = append(args, cur)
					cur = nil
					s.i++
					continue
				}
			}
		}
		if t.k != tNL {
			cur = append(cur, t)
		}
		s.i++
	}
	if len(cur) > 0 {
		args = append(args, cur)
	}
	return args
}

// collectExpr gathers the rest of a statement: up to a newline, ';' or '}' at nesting depth 0, continuing across lines
// after a trailing operator or comma and before a leading '.'.
func (s *scanner) collectExpr() []tok {
	var out []tok
	depth := 0
	for s.i < len(s.t) {
		t := s.t[s.i]
		if err := s.g.Node(); err != nil {
			s.err = err
			return out
		}
		if t.k == tPunct {
			switch t.s {
			case "(", "[", "{":
				depth++
			case ")", "]":
				depth--
			case "}":
				if depth == 0 {
					return out
				}
				depth--
			case ";":
				if depth <= 0 {
					return out
				}
			}
		}
		if t.k == tNL && depth <= 0 {
			if n := len(out); n > 0 && out[n-1].k == tPunct {
				switch out[n-1].s {
				case ",", "+", ".", "?.", "&&", "||", "?:", "->", "=", "-", "*", "/", "<<", "::":
					s.i++
					continue
				}
			}
			j := s.i
			for j < len(s.t) && s.t[j].k == tNL {
				j++
			}
			if j < len(s.t) && s.t[j].k == tPunct && (s.t[j].s == "." || s.t[j].s == "?." || s.t[j].s == "+" || s.t[j].s == "&&" || s.t[j].s == "||" || s.t[j].s == "?:") {
				s.i = j
				continue
			}
			return out
		}
		if depth < 0 {
			depth = 0
		}
		if t.k != tNL {
			out = append(out, t)
		}
		s.i++
	}
	return out
}

// splitTop splits tokens at top-level commas.
func splitTop(ts []tok) [][]tok {
	var args [][]tok
	var cur []tok
	depth := 0
	for _, t := range ts {
		if t.k == tPunct {
			switch t.s {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			case ",":
				if depth == 0 {
					args = append(args, cur)
					cur = nil
					continue
				}
			}
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		args = append(args, cur)
	}
	return args
}
