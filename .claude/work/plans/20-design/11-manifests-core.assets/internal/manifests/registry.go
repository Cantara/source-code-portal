package manifests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
)

// File is the only input a parser sees. Parse must be a pure function of Data: Path is context for diagnostics only.
type File struct {
	Path    string
	Data    []byte
	BlobSHA string
}

// Parser is a pure function of File: no I/O, no clock, no network, no globals, safe for concurrent use, never panics (fuzzed).
type Parser interface {
	ID() string   // stable, "<ecosystem>/<format>", e.g. "maven/pom"; unique in the registry
	Version() int // bump on ANY change of output; part of every cache key and of Registry.SetHash
	Ecosystem() EcosystemID
	Match(path string) bool // cheap, path only, case-sensitive on the base name
	Parse(ctx context.Context, f File, lim Limits) (Parsed, error)
}

// Optional parser capabilities (checked with type assertions by the runner).
type (
	// Kinded declares the file kind before parsing (selects the size limit: lockfiles get MaxLockBytes). Default KindManifest.
	Kinded interface{ Kind() FileKind }
	// Weigher declares the memory amplification of the format (default 10): weight = size x amplification.
	Weigher interface{ Amplification() int }
)

// Assembler turns the Parsed files of one repository (one ecosystem) into units.
type Assembler interface {
	Ecosystem() EcosystemID
	Assemble(ctx context.Context, files FileSet, parsed []Parsed) ([]Unit, error)
}

// VersionedAssembler lets an assembler take part in Registry.SetHash (default 1).
type VersionedAssembler interface{ AssemblerVersion() int }

// FileSet lists the repository's candidate files (the paths the allow-list selected). It deliberately has no Read: assembly is a
// pure function of the Parsed values and the path list, so a run served from the parse cache and a run that fetched everything
// produce identical units. Anything an assembler needs from a file must therefore be in its Parsed.
type FileSet interface {
	Paths() []string // sorted, slash-separated, repo-relative
	Has(path string) bool
}

// PathSet is the in-memory FileSet used by the runner, the golden harness and tests.
type PathSet []string

func (m PathSet) Paths() []string   { out := slices.Clone(m); slices.Sort(out); return out }
func (m PathSet) Has(p string) bool { return slices.Contains(m, p) }

// Registry is the open/closed extension point. Built in cmd/scp, then frozen; reads after Freeze need no lock.
type Registry struct {
	frozen     bool
	parsers    []Parser
	byID       map[string]Parser
	assemblers map[EcosystemID]Assembler
	disabled   map[EcosystemID]bool // ecosystems switched off by config: their parsers are dropped now and refused later, whatever the call order
	specs      map[EcosystemID]EcosystemSpec
	hash       string
}

// NewRegistry returns a registry preloaded with the specs of the 16 built-in ecosystems.
func NewRegistry() *Registry {
	r := &Registry{disabled: map[EcosystemID]bool{}, byID: map[string]Parser{}, assemblers: map[EcosystemID]Assembler{}, specs: map[EcosystemID]EcosystemSpec{}}
	for _, s := range builtinSpecs() {
		r.specs[s.ID] = s
	}
	return r
}

func (r *Registry) mustBeOpen() {
	if r.frozen {
		panic("manifests: registry is frozen")
	}
}

// RegisterEcosystem adds or replaces an ecosystem spec (new ecosystems; the built-ins are preloaded).
func (r *Registry) RegisterEcosystem(s EcosystemSpec) error {
	r.mustBeOpen()
	if s.ID == "" || s.Normalize == nil || s.VersionSystem == "" {
		return fmt.Errorf("manifests: ecosystem spec %q incomplete", s.ID)
	}
	r.specs[s.ID] = s
	return nil
}

func (r *Registry) Register(p Parser) error {
	r.mustBeOpen()
	id := p.ID()
	if !validParserID(id) || !strings.HasPrefix(id, string(p.Ecosystem())+"/") {
		return fmt.Errorf("manifests: parser id %q must be \"<ecosystem>/<format>\"", id)
	}
	if _, dup := r.byID[id]; dup {
		return fmt.Errorf("manifests: duplicate parser %q", id)
	}
	if p.Version() < 1 {
		return fmt.Errorf("manifests: parser %q version must be >= 1", id)
	}
	if _, ok := r.specs[p.Ecosystem()]; !ok {
		return fmt.Errorf("manifests: parser %q: ecosystem %q has no spec (RegisterEcosystem first)", id, p.Ecosystem())
	}
	if r.disabled[p.Ecosystem()] { // valid, but switched off: registers nothing (the same result as Disable after Register)
		return nil
	}
	r.byID[id] = p
	r.parsers = append(r.parsers, p)
	slices.SortFunc(r.parsers, func(a, b Parser) int { return strings.Compare(a.ID(), b.ID()) })
	return nil
}

func validParserID(id string) bool {
	if len(id) > 64 || strings.Count(id, "/") != 1 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '/' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (r *Registry) RegisterAssembler(a Assembler) error {
	r.mustBeOpen()
	if _, dup := r.assemblers[a.Ecosystem()]; dup {
		return fmt.Errorf("manifests: duplicate assembler for %q", a.Ecosystem())
	}
	if _, ok := r.specs[a.Ecosystem()]; !ok {
		return fmt.Errorf("manifests: assembler: ecosystem %q has no spec", a.Ecosystem())
	}
	r.assemblers[a.Ecosystem()] = a
	return nil
}

// Disable removes the parsers of an ecosystem (config manifests.acquisition.disabled_ecosystems) and makes later Register calls of that ecosystem no-ops;
// the order of Register and Disable therefore does not matter. Open registry only.
func (r *Registry) Disable(eco EcosystemID) {
	r.mustBeOpen()
	r.disabled[eco] = true
	r.parsers = slices.DeleteFunc(r.parsers, func(p Parser) bool {
		if p.Ecosystem() == eco {
			delete(r.byID, p.ID())
			return true
		}
		return false
	})
}

// Freeze ends registration and computes SetHash.
func (r *Registry) Freeze() {
	if r.frozen {
		return
	}
	r.frozen = true
	h := sha256.New()
	fmt.Fprintf(h, "model=%d\n", ModelVersion)
	for _, p := range r.parsers {
		fmt.Fprintf(h, "p:%s@%d\n", p.ID(), p.Version())
	}
	ecos := make([]string, 0, len(r.assemblers))
	for e := range r.assemblers {
		ecos = append(ecos, string(e))
	}
	slices.Sort(ecos)
	for _, e := range ecos {
		v := 1
		if va, ok := r.assemblers[EcosystemID(e)].(VersionedAssembler); ok {
			v = va.AssemblerVersion()
		}
		fmt.Fprintf(h, "a:%s@%d\n", e, v)
	}
	r.hash = hex.EncodeToString(h.Sum(nil))[:16]
}

// ModelVersion is bumped when the normalisation, join or finalisation rules of this package change output.
const ModelVersion = 1

func (r *Registry) SetHash() string { return r.hash }

func (r *Registry) Parser(id string) (Parser, bool)           { p, ok := r.byID[id]; return p, ok }
func (r *Registry) Spec(e EcosystemID) (EcosystemSpec, bool)  { s, ok := r.specs[e]; return s, ok }
func (r *Registry) Assembler(e EcosystemID) (Assembler, bool) { a, ok := r.assemblers[e]; return a, ok }

// Match returns the parsers interested in path, in ID order.
func (r *Registry) Match(p string) []Parser {
	var out []Parser
	for _, q := range r.parsers {
		if q.Match(p) {
			out = append(out, q)
		}
	}
	return out
}

// Interesting is the cheap path pre-filter used by the push reactor: some parser matches and no excluded segment.
func (r *Registry) Interesting(p string, excluded map[string]bool) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if excluded[seg] {
			return false
		}
	}
	return len(r.Match(p)) > 0
}

func builtinSpecs() []EcosystemSpec {
	mk := func(id EcosystemID, display, lang string) EcosystemSpec {
		e := string(id)
		return EcosystemSpec{ID: id, DisplayName: display, PURLType: purl.Type(e), Normalize: purl.NormalizeFunc(e), VersionSystem: string(version.Of(e)), Language: lang}
	}
	return []EcosystemSpec{
		mk(Maven, "Maven", "Java"), mk(NPM, "npm", "JavaScript"), mk(PyPI, "Python", "Python"), mk(Golang, "Go modules", "Go"),
		mk(Cargo, "Cargo", "Rust"), mk(NuGet, "NuGet", "C#"), mk(Gem, "RubyGems", "Ruby"), mk(Composer, "Composer", "PHP"),
		mk(Swift, "Swift Package Manager", "Swift"), mk(Pub, "pub", "Dart"), mk(Hex, "Hex", "Elixir"), mk(CocoaPods, "CocoaPods", "Swift"),
		mk(Docker, "Docker", ""), mk(GitHubActions, "GitHub Actions", ""), mk(Terraform, "Terraform", ""), mk(Helm, "Helm", ""),
	}
}

// ParserIDs lists the registered parser ids in registration order (metrics, diagnostics).
func (r *Registry) ParserIDs() []string {
	ids := make([]string, len(r.parsers))
	for i, p := range r.parsers {
		ids[i] = p.ID()
	}
	return ids
}

// funcParser is the Parser built by NewParser (unit 13's "ParserFunc").
type funcParser struct {
	id    string
	ver   int
	eco   EcosystemID
	match func(string) bool
	parse func(context.Context, File, Limits) (Parsed, error)
	kind  FileKind
	amp   int
}

func (p *funcParser) ID() string             { return p.id }
func (p *funcParser) Version() int           { return p.ver }
func (p *funcParser) Ecosystem() EcosystemID { return p.eco }
func (p *funcParser) Match(path string) bool { return p.match(path) }
func (p *funcParser) Parse(ctx context.Context, f File, l Limits) (Parsed, error) {
	return p.parse(ctx, f, l)
}
func (p *funcParser) Kind() FileKind     { return p.kind }
func (p *funcParser) Amplification() int { return p.amp }

// NewParser wraps functions into a Parser (kind KindManifest, amplification 10; see WithKind and WithAmplification).
func NewParser(id string, version int, eco EcosystemID, match func(string) bool, parse func(context.Context, File, Limits) (Parsed, error)) Parser {
	return &funcParser{id: id, ver: version, eco: eco, match: match, parse: parse, amp: 10}
}

// WithKind sets the file kind of a parser made by NewParser (a lockfile parser gets the lockfile size limit); other parsers are returned unchanged.
func WithKind(p Parser, k FileKind) Parser {
	if f, ok := p.(*funcParser); ok {
		c := *f
		c.kind = k
		return &c
	}
	return p
}

// WithAmplification sets the memory amplification factor of a parser made by NewParser (weight = size x factor in the parse budget).
func WithAmplification(p Parser, n int) Parser {
	if f, ok := p.(*funcParser); ok && n > 0 {
		c := *f
		c.amp = n
		return &c
	}
	return p
}
