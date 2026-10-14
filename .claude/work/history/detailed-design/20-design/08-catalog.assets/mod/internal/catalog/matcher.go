package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
)

// MatcherKind is the closed set of matcher kinds (04 Q-07). The YAML form is a one-key mapping: `- prefix: Widget`.
type MatcherKind string

const (
	KindExact    MatcherKind = "exact"
	KindPrefix   MatcherKind = "prefix"
	KindGlob     MatcherKind = "glob"
	KindRegexp   MatcherKind = "regexp"
	KindTopic    MatcherKind = "topic"
	KindCatchAll MatcherKind = "catch_all"
)

// Matcher is one entry of a group's `match` list. Value is the string operand (for KindCatchAll it is "true").
// The zero Matcher is invalid (Validate reports it).
type Matcher struct {
	Kind  MatcherKind
	Value string
}

// Limits on matcher operands (bounds the work a hostile or careless config can cause).
const (
	MaxMatchersPerGroup = 64
	MaxPatternBytes     = 256 // exact, prefix, glob, regexp, topic
	MaxRegexpProgram    = 1000
	MaxGlobWildcards    = 6
)

// UnmarshalYAML decodes exactly one of exact|prefix|glob|regexp|topic|catch_all. It uses the yaml.v3 "obsolete"
// unmarshaler signature on purpose: it needs no yaml import (catalog may not import a YAML library, 10-architecture 5.2)
// and the decoder's KnownFields(true) still applies to the nested struct.
func (m *Matcher) UnmarshalYAML(unmarshal func(any) error) error {
	var raw struct {
		Exact    *string `yaml:"exact"`
		Prefix   *string `yaml:"prefix"`
		Glob     *string `yaml:"glob"`
		Regexp   *string `yaml:"regexp"`
		Topic    *string `yaml:"topic"`
		CatchAll *bool   `yaml:"catch_all"`
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	var got []Matcher
	add := func(k MatcherKind, v *string) {
		if v != nil {
			got = append(got, Matcher{k, *v})
		}
	}
	add(KindExact, raw.Exact)
	add(KindPrefix, raw.Prefix)
	add(KindGlob, raw.Glob)
	add(KindRegexp, raw.Regexp)
	add(KindTopic, raw.Topic)
	if raw.CatchAll != nil {
		if !*raw.CatchAll {
			return errors.New("catch_all must be true (remove the entry instead of writing false)")
		}
		got = append(got, Matcher{KindCatchAll, "true"})
	}
	if len(got) != 1 {
		return fmt.Errorf("a matcher entry has exactly one of exact, prefix, glob, regexp, topic, catch_all (found %d)", len(got))
	}
	*m = got[0]
	return nil
}

// CompiledMatcher is immutable and safe for concurrent use.
type CompiledMatcher struct {
	kind  MatcherKind
	fold  string         // lower-cased operand (exact, prefix, topic)
	re    *regexp.Regexp // glob (translated) and regexp
	empty bool           // regexp/glob matches the empty string (lint only)
	src   string
}

// Kind returns the matcher kind.
func (c CompiledMatcher) Kind() MatcherKind { return c.kind }

// Source returns the operand as written.
func (c CompiledMatcher) Source() string { return c.src }

// MatchesEmpty reports whether the matcher accepts the empty name (a sign of a catch-all written as a regexp).
func (c CompiledMatcher) MatchesEmpty() bool { return c.empty }

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	topicRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)
)

// Compile validates and compiles one matcher. The error is a *MatcherError.
func (m Matcher) Compile() (CompiledMatcher, error) {
	bad := func(code, msg string) (CompiledMatcher, error) {
		return CompiledMatcher{}, &MatcherError{Kind: m.Kind, Code: code, Msg: msg}
	}
	v := m.Value
	if m.Kind != KindCatchAll {
		if v == "" {
			return bad("empty", "operand is empty")
		}
		if len(v) > MaxPatternBytes {
			return bad("too_long", fmt.Sprintf("operand is longer than %d bytes", MaxPatternBytes))
		}
	}
	switch m.Kind {
	case KindExact:
		if !nameRe.MatchString(v) {
			return bad("charset", "a repository name contains only letters, digits, '.', '_' and '-'")
		}
		return CompiledMatcher{kind: m.Kind, fold: strings.ToLower(v), src: v}, nil
	case KindPrefix:
		if !nameRe.MatchString(v) {
			return bad("charset", "a repository name prefix contains only letters, digits, '.', '_' and '-'")
		}
		return CompiledMatcher{kind: m.Kind, fold: strings.ToLower(v), src: v}, nil
	case KindGlob:
		re, empty, err := compileGlob(v)
		if err != nil {
			return bad("glob", err.Error())
		}
		return CompiledMatcher{kind: m.Kind, re: re, empty: empty, src: v}, nil
	case KindRegexp:
		re, empty, err := compileAnchored(v)
		if err != nil {
			return bad("regexp", err.Error())
		}
		return CompiledMatcher{kind: m.Kind, re: re, empty: empty, src: v}, nil
	case KindTopic:
		t := strings.ToLower(v)
		if !topicRe.MatchString(t) {
			return bad("charset", "a GitHub topic is 1-50 characters of a-z, 0-9 and '-', starting with a letter or digit")
		}
		return CompiledMatcher{kind: m.Kind, fold: t, src: v}, nil
	case KindCatchAll:
		return CompiledMatcher{kind: m.Kind, src: "true"}, nil
	default:
		return bad("kind", fmt.Sprintf("unknown matcher kind %q", m.Kind))
	}
}

// compileGlob translates a glob (only '*' and '?' are special; everything else must be a name character) into an anchored,
// case-insensitive RE2 program so that matching is linear in the name length (no backtracking).
func compileGlob(g string) (*regexp.Regexp, bool, error) {
	var b strings.Builder
	b.WriteString("(?is)^")
	stars := 0
	prevStar := false
	for _, r := range g {
		switch {
		case r == '*':
			stars++
			if !prevStar {
				b.WriteString(".*")
			}
			prevStar = true
			continue
		case r == '?':
			b.WriteString(".")
		case r < 0x80 && strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-", r):
			b.WriteString(regexp.QuoteMeta(string(r)))
		default:
			return nil, false, fmt.Errorf("a glob has only '*' and '?' as wildcards and name characters otherwise; found %q (use a regexp matcher for classes or alternation)", r)
		}
		prevStar = false
	}
	if stars > MaxGlobWildcards {
		return nil, false, fmt.Errorf("a glob has at most %d '*' wildcards", MaxGlobWildcards)
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, false, err
	}
	return re, re.MatchString(""), nil
}

// compileAnchored validates p on its own (so an unbalanced ')' cannot break out of the wrapper below), checks the program size,
// and compiles ^(?:p)$ (RE2, case-sensitive unless p says (?i)). Whole-name semantics, never find().
func compileAnchored(p string) (*regexp.Regexp, bool, error) {
	re, err := syntax.Parse(p, syntax.Perl)
	if err != nil {
		return nil, false, err
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return nil, false, err
	}
	if len(prog.Inst) > MaxRegexpProgram {
		return nil, false, fmt.Errorf("regexp compiles to %d instructions (limit %d)", len(prog.Inst), MaxRegexpProgram)
	}
	c, err := regexp.Compile("^(?:" + p + ")$")
	if err != nil {
		return nil, false, err
	}
	return c, c.MatchString(""), nil
}

// MatcherError is the typed error of Compile.
type MatcherError struct {
	Kind MatcherKind
	Code string // empty, too_long, charset, glob, regexp, kind
	Msg  string
}

func (e *MatcherError) Error() string { return fmt.Sprintf("%s matcher: %s", e.Kind, e.Msg) }

// Match reports whether the repository name (and, for topic matchers, its topics) is selected.
func (c CompiledMatcher) Match(name string, topics []string) bool {
	switch c.kind {
	case KindCatchAll:
		return true
	case KindExact:
		return strings.ToLower(name) == c.fold
	case KindPrefix:
		return strings.HasPrefix(strings.ToLower(name), c.fold)
	case KindGlob, KindRegexp:
		return c.re.MatchString(name)
	case KindTopic:
		for _, t := range topics {
			if strings.ToLower(t) == c.fold {
				return true
			}
		}
	}
	return false
}
