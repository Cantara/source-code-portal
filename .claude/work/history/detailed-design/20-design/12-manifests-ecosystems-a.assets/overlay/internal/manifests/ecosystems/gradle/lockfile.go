package gradle

import (
	"bytes"
	"context"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type lockParser struct{ base }

func NewLockfile() manifests.Parser {
	return lockParser{base{id: LockfileID, match: matchLockfile, kind: manifests.KindLockfile}}
}

func confRank(conf string) int {
	l := strings.ToLower(conf)
	switch {
	case strings.Contains(l, "annotationprocessor") || strings.HasPrefix(l, "kapt") || strings.HasPrefix(l, "ksp") || l == "classpath" || strings.Contains(l, "buildscript"):
		return 3
	case strings.Contains(l, "test"):
		return 2
	case strings.Contains(l, "runtime"):
		return 0
	case strings.Contains(l, "compile"):
		return 1
	}
	return 0
}

// unknownRank marks a lockfile line without "=configurations".
const unknownRank = 4

// rankScope maps a configuration rank (0 runtime, 1 compile-only, 2 test, 3 build) to a scope.
func rankScope(r int) manifests.Scope {
	switch r {
	case 0:
		return manifests.ScopeRuntime
	case 1:
		return manifests.ScopeProvided
	case 2:
		return manifests.ScopeTest
	}
	return manifests.ScopeBuild
}

func (p lockParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	g := limits.NewGuard(ctx, lim)
	type acc struct {
		lc   manifests.LockedComponent
		rank int
	}
	seen := map[string]*acc{}
	line := 0
	for len(data) > 0 {
		var ln []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			ln, data = data[:i], data[i+1:]
		} else {
			ln, data = data, nil
		}
		line++
		if err := g.Node(); err != nil {
			fail(&out, err, "gradle.syntax")
			return out, nil
		}
		s := strings.TrimSpace(string(ln))
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "empty=") {
			continue
		}
		coords, confs, _ := strings.Cut(s, "=")
		parts := strings.Split(coords, ":")
		for i := range parts {
			parts[i] = clean(strings.TrimSpace(parts[i]), 200)
		}
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			out.Diagnostics = append(out.Diagnostics, diag("gradle.lockfile-syntax", line, "expected group:name:version=configurations"))
			continue
		}
		rank := 3
		for _, c := range strings.Split(confs, ",") {
			if r := confRank(strings.TrimSpace(c)); r < rank {
				rank = r
			}
		}
		if confs == "" {
			rank = unknownRank // per-configuration lockfile: the configuration is the file name, which the assembler knows
		}
		k := coords
		if a := seen[k]; a != nil {
			if rank < a.rank {
				a.rank = rank
			}
			continue
		}
		seen[k] = &acc{lc: manifests.LockedComponent{ID: mvn(clean(parts[0], 200), clean(parts[1], 200)), Version: clean(parts[2], 128), Relationship: manifests.RelUnknown, Loc: manifests.Location{Line: line}}, rank: rank}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := seen[k]
		lc := a.lc
		if a.rank == unknownRank {
			lc.Scope = manifests.ScopeUnknown
		} else {
			lc.Scope = rankScope(a.rank)
		}
		out.Locked = append(out.Locked, lc)
	}
	return out, nil
}

// ---- gradle.properties ----

type propsParser struct{ base }

func NewProperties() manifests.Parser {
	return propsParser{base{id: PropertiesID, match: isBase("gradle.properties"), kind: manifests.KindWorkspace}}
}

// secretKey reports property names whose values must never be copied: gradle.properties routinely holds credentials.
func secretKey(k string) bool {
	l := strings.ToLower(k)
	for _, w := range []string{"pass", "secret", "token", "credential", "key", "auth", "cert", "signing", "user", "login", "pwd"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func (p propsParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindWorkspace}
	g := limits.NewGuard(ctx, lim)
	var logical strings.Builder
	for _, raw := range strings.Split(string(data), "\n") {
		if err := g.Node(); err != nil {
			fail(&out, err, "gradle.syntax")
			return out, nil
		}
		s := strings.TrimRight(raw, "\r")
		if logical.Len() == 0 {
			t := strings.TrimLeft(s, " \t\f")
			if t == "" || t[0] == '#' || t[0] == '!' {
				continue
			}
			s = t
		} else {
			s = strings.TrimLeft(s, " \t\f")
		}
		if strings.HasSuffix(s, "\\") && !strings.HasSuffix(s, "\\\\") {
			logical.WriteString(s[:len(s)-1])
			if logical.Len() < 1<<16 {
				continue
			}
		}
		logical.WriteString(s)
		kv := logical.String()
		logical.Reset()
		i := strings.IndexAny(kv, "=: \t")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(kv[:i])
		v := strings.TrimLeft(kv[i:], "=: \t")
		if secretKey(k) {
			v = "<redacted>"
		}
		if len(out.Properties) < 5000 {
			setProp(&out, "prop:"+clean(k, 200), clean(v, 500))
		}
	}
	return out, nil
}
