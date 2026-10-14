package manifests

import (
	"fmt"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

// Field length caps applied to every parsed string (bytes, rune-boundary safe).
const (
	maxName    = 256
	maxVersion = 128
	maxReq     = 512
	maxMessage = 512
	maxPath    = 512
	maxGroup   = 128
)

// CleanText makes untrusted text safe for logs, JSON consumers and LLM outputs: invalid UTF-8 becomes U+FFFD, C0/C1 controls (tab and
// newline become one space), DEL, bidi controls (U+061C, U+200E/F, U+202A-E, U+2066-9), zero-width characters (U+200B-D, U+2060, U+FEFF)
// and Unicode tag characters (U+E0000-U+E007F, used for hidden prompt injection) are removed, and the result is cut at max bytes.
func CleanText(s string, max int) string {
	return limits.Text(s, max)
}

func isInvisible(r rune) bool { return limits.IsInvisible(r) }

// sanitizeParsed enforces the invariants parsers are also fuzzed against, so a parser bug cannot corrupt the model:
// strings cleaned and capped, identities re-normalised (idempotent), invalid dependencies dropped with a diagnostic,
// lists capped, lines in range. nlines is the line count of the source file (0 = unknown, lines are kept when >= 0).
func sanitizeParsed(p *Parsed, spec EcosystemSpec, lim LimitsConfig, nlines int) {
	norm := func(c ComponentID) ComponentID {
		ns, n := spec.Normalize(CleanText(c.Namespace, maxName), CleanText(c.Name, maxName))
		return ComponentID{Ecosystem: spec.ID, Namespace: intern(ns), Name: intern(n)}
	}
	line := func(l int32) int32 {
		if l < 0 || (nlines > 0 && int(l) > nlines) {
			return 0
		}
		return l
	}
	deps := p.Dependencies[:0:0]
	dropped := 0
	for _, d := range p.Dependencies {
		d.Target = norm(d.Target)
		if !d.Target.Valid() {
			dropped++
			continue
		}
		d.Requirement, d.Version, d.Group = CleanText(d.Requirement, maxReq), CleanText(d.Version, maxVersion), intern(CleanText(d.Group, maxGroup))
		d.Requirement, d.Version = intern(d.Requirement), intern(d.Version)
		d.Line = line(d.Line)
		if d.Extra != nil {
			x := *d.Extra
			x.TargetDisplay, x.Condition, x.Qualifiers = CleanText(x.TargetDisplay, maxName), CleanText(x.Condition, maxReq), CleanText(x.Qualifiers, maxReq)
			x.SourceRef, x.LocPath = CleanText(x.SourceRef, 1024), CleanText(x.LocPath, maxPath)
			if x == (DependencyExtra{}) {
				d.Extra = nil
			} else {
				d.Extra = &x
			}
		}
		deps = append(deps, d)
		if len(deps) >= lim.MaxDepsPerFile {
			if rest := len(p.Dependencies) - len(deps) - dropped; rest > 0 {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: DiagDepsCap, Severity: SevWarning, Message: fmt.Sprintf("dependencies beyond the cap of %d were dropped", lim.MaxDepsPerFile)})
			}
			break
		}
	}
	if dropped > 0 {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "parse.invalid-dependency", Severity: SevWarning, Message: fmt.Sprintf("%d dependencies without a valid name were dropped", dropped)})
	}
	p.Dependencies = deps
	for i := range p.Provides {
		a := &p.Provides[i]
		a.ID = norm(a.ID)
		a.Display, a.Version, a.Kind = CleanText(a.Display, maxName), CleanText(a.Version, maxVersion), CleanText(a.Kind, 32)
	}
	p.Provides = keepValid(p.Provides, func(a Artifact) bool { return a.ID.Valid() })
	if len(p.Locked) > lim.MaxLockedPerFile {
		p.Locked = p.Locked[:lim.MaxLockedPerFile]
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: DiagLockedCap, Severity: SevWarning, Message: fmt.Sprintf("locked components beyond the cap of %d were dropped", lim.MaxLockedPerFile)})
	}
	for i := range p.Locked {
		l := &p.Locked[i]
		l.ID = norm(l.ID)
		l.Version, l.Integrity = intern(CleanText(l.Version, maxVersion)), CleanText(l.Integrity, 256)
		for j := range l.Licenses {
			l.Licenses[j] = intern(CleanText(l.Licenses[j], 128))
		}
		l.DependsOn = keepValid(l.DependsOn, func(x int) bool { return x >= 0 && x < len(p.Locked) })
		l.Loc.Line = int(line(int32(l.Loc.Line)))
		l.Loc.Path = CleanText(l.Loc.Path, maxPath)
	}
	for k, v := range p.Toolchain {
		delete(p.Toolchain, k)
		if k = CleanText(k, 32); k != "" {
			p.Toolchain[k] = CleanText(v, maxVersion)
		}
	}
	for i := range p.Licenses {
		p.Licenses[i] = CleanText(p.Licenses[i], 128)
	}
	for i := range p.Members {
		p.Members[i] = CleanText(p.Members[i], maxPath)
	}
	for i := range p.Includes {
		p.Includes[i] = CleanText(p.Includes[i], maxPath)
	}
	for i := range p.Diagnostics {
		d := &p.Diagnostics[i]
		d.Code = CleanText(d.Code, 64)
		d.Message = CleanText(d.Message, maxMessage)
		d.Loc.Path = CleanText(d.Loc.Path, maxPath)
		d.Loc.Line = int(line(int32(d.Loc.Line)))
	}
}

func keepValid[T any](s []T, ok func(T) bool) []T {
	out := s[:0:0]
	for _, x := range s {
		if ok(x) {
			out = append(out, x)
		}
	}
	return out
}
