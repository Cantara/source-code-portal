package manifests

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

// ADDED by unit 12 (requirement RR-A2, owner: unit 11). RunTree is the pure core of a refresh: everything that happens after the blobs
// are in memory. The Service keeps its own concurrent, cached, watchdog-supervised version of the same sequence; RunTree is what
// golden, property and fuzz tests use so that they run exactly the sequence below: reject binary, size check, Parse, error
// classification, sanitizeParsed, stamp File and Kind, fill Location.Path of parser diagnostics, rebasePaths, assemble per ecosystem
// (panic contained, default assembler as fallback), finalize. Unit 11 is expected to make Service.assembleAll call the second half.

// TreeResult is the output of RunTree.
type TreeResult struct {
	Parsed      []Parsed     // successful parses, runner-stamped, in (path, parser) order
	Units       []Unit       // finalised units
	Diagnostics []Diagnostic // runner-level diagnostics (parse failures, limits, assembly) plus finalize diagnostics
}

// BlobSHA returns the Git blob id (SHA-1) of data, the cache identity of a file.
func BlobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// RunTree parses and assembles the files of one repository (path -> bytes) with the registry's parsers and assemblers.
func RunTree(ctx context.Context, reg *Registry, cfg Config, files map[string][]byte) TreeResult {
	var res TreeResult
	paths := make(PathSet, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	lc := cfg.Limits
	byEco := map[EcosystemID][]Parsed{}
	for _, p := range paths {
		for _, ps := range reg.Match(p) {
			spec, _ := reg.Spec(ps.Ecosystem())
			parsed, perr := runOne(ctx, ps, spec, p, files[p], lc)
			if perr != nil {
				res.Diagnostics = append(res.Diagnostics, Diagnostic{Code: perr.Code, Severity: SevWarning, Message: perr.Msg, Loc: Location{Path: p}})
				continue
			}
			parsed.File = FileRef{Path: p, BlobSHA: BlobSHA(files[p]), ParserID: ps.ID(), ParserVersion: ps.Version()}
			if k, ok := ps.(Kinded); ok {
				parsed.Kind = k.Kind()
			}
			parsed.File.Kind = parsed.Kind
			stampPaths(&parsed)
			rebasePaths(&parsed)
			res.Parsed = append(res.Parsed, parsed)
			byEco[ps.Ecosystem()] = append(byEco[ps.Ecosystem()], parsed)
		}
	}
	ecos := make([]EcosystemID, 0, len(byEco))
	for e := range byEco {
		ecos = append(ecos, e)
	}
	slices.Sort(ecos)
	var units []Unit
	for _, e := range ecos {
		ps := byEco[e]
		slices.SortFunc(ps, func(a, b Parsed) int {
			return strings.Compare(a.File.Path+a.File.ParserID, b.File.Path+b.File.ParserID)
		})
		actx, cancel := context.WithTimeout(ctx, lc.AssembleTimeout)
		us, ad := assembleChecked(actx, reg, e, ps, paths)
		cancel()
		res.Diagnostics = append(res.Diagnostics, ad...)
		units = append(units, us...)
	}
	role := map[string]Role{}
	fixt := toSet(cfg.Acquisition.FixtureSegments)
	for _, p := range paths {
		role[p] = RolePrimary
		for _, seg := range strings.Split(p, "/") {
			if fixt[seg] {
				role[p] = RoleFixture
			}
		}
	}
	units, fd := finalize(units, role, lc.MaxUnitsPerRepo, lc)
	res.Units = units
	res.Diagnostics = append(res.Diagnostics, fd...)
	slices.SortStableFunc(res.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Code+"|"+a.Loc.Path, b.Code+"|"+b.Loc.Path) })
	return res
}

func runOne(ctx context.Context, p Parser, spec EcosystemSpec, path string, data []byte, lc LimitsConfig) (out Parsed, perr *parseErr) {
	maxBytes := int(lc.ManifestBytes)
	if k, ok := p.(Kinded); ok && k.Kind() == KindLockfile {
		maxBytes = int(lc.LockfileBytes)
	}
	lim := Limits{MaxBytes: maxBytes, MaxDepth: lc.MaxDepth, MaxNodes: lc.MaxNodes, MaxExpand: int(lc.MaxExpand), MaxExpandAll: int(lc.MaxExpandFile), Timeout: lc.ParseTimeout}
	text, err := limits.RejectBinary(limits.StripBOM(data))
	if err != nil {
		return Parsed{}, &parseErr{Code: DiagEncoding, Msg: err.Error()}
	}
	if len(text) > lim.MaxBytes {
		return Parsed{}, &parseErr{Code: "limit." + limits.LimitSize, Msg: fmt.Sprintf("file is %d bytes, limit %d", len(text), lim.MaxBytes)}
	}
	pctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	func() {
		defer func() {
			if r := recover(); r != nil {
				perr = &parseErr{Code: DiagParsePanic, Msg: "the parser panicked on this file"}
			}
		}()
		out, err = p.Parse(pctx, File{Path: path, Data: text, BlobSHA: BlobSHA(data)}, lim)
	}()
	if perr != nil {
		return Parsed{}, perr
	}
	var le *limits.LimitError
	var ee *limits.EncodingError
	switch {
	case err == nil:
		sanitizeParsed(&out, spec, lc, strings.Count(string(text), "\n")+1)
		return out, nil
	case errors.As(err, &le):
		return Parsed{}, &parseErr{Code: "limit." + le.Limit, Msg: le.Error()}
	case errors.As(err, &ee):
		return Parsed{}, &parseErr{Code: DiagEncoding, Msg: ee.Error()}
	case pctx.Err() != nil:
		return Parsed{}, &parseErr{Code: "limit." + limits.LimitTimeout, Msg: "the parser did not finish in time"}
	}
	return Parsed{}, &parseErr{Code: DiagParseFailed, Msg: CleanText(err.Error(), 200)}
}

func snapshot(ps []Parsed) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		b, _ := json.Marshal(p)
		out[i] = string(b)
	}
	return out
}

// assembleChecked is runAssembler with one addition: the registered assembler's input is snapshotted around its call, so a Parsed value that an
// assembler modified (they are cached and shared in the Service, RR-A5) is reported as assemble.mutated-input. The default assembler that
// follows a nil result is the root's own code and is not checked (its Loc.Path stamping writes through Dependency.Extra: RR-A5).
func assembleChecked(ctx context.Context, reg *Registry, eco EcosystemID, parsed []Parsed, files FileSet) (units []Unit, diags []Diagnostic) {
	a, ok := reg.Assembler(eco)
	if !ok {
		return defaultAssemble(eco, parsed, files), nil
	}
	snap := snapshot(parsed)
	func() {
		defer func() {
			if r := recover(); r != nil {
				diags = append(diags, Diagnostic{Code: DiagAssembleFailed, Severity: SevError, Message: fmt.Sprintf("assembler for %s panicked; default assembly used", eco)})
				units = nil
			}
		}()
		var err error
		units, err = a.Assemble(ctx, files, parsed)
		if err != nil {
			units = nil
			diags = append(diags, Diagnostic{Code: DiagAssembleFailed, Severity: SevError, Message: fmt.Sprintf("assembler for %s failed; default assembly used", eco)})
		}
	}()
	if after := snapshot(parsed); !slices.Equal(snap, after) {
		diags = append(diags, Diagnostic{Code: "assemble.mutated-input", Severity: SevError, Message: "the " + string(eco) + " assembler modified a Parsed value"})
	}
	if units == nil {
		units = defaultAssemble(eco, parsed, files)
	}
	return units, diags
}
