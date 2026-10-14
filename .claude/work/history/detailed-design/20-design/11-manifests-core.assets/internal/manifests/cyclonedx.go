package manifests

import (
	"crypto/sha256"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	cdx "github.com/CycloneDX/cyclonedx-go"

	"github.com/cantara/source-code-portal/internal/scm"
)

// CycloneDXSpec is the only version written; older versions are not offered (they would lose fields) and SPDX is deferred (ADR-021).
const CycloneDXSpec = cdx.SpecVersion1_7

// BuildBOM converts one repository's facts into a CycloneDX 1.7 document. It is pure: the output depends on the arguments only
// (no clock, no random serial number: the serial is derived from the tree SHA, so equal trees give byte-equal documents).
// locked maps Unit.ID to the unit's locked components (from the bounded cache; absent when evicted, which the document records as the property
// scp:lockedUnavailable). omitted names the units whose locked components were left out on purpose (output.sbom_max_locked); the document records
// their component count as scp:lockedOmitted. Both maps may be nil.
func BuildBOM(reg *Registry, repo scm.Repository, f RepoFacts, locked map[string][]LockedComponent, omitted map[string]bool, version string) *cdx.BOM {
	bom := cdx.NewBOM()
	bom.SpecVersion = CycloneDXSpec
	bom.SerialNumber = "urn:uuid:" + uuidFrom("scp-sbom|"+repo.Ref.Key()+"|"+f.TreeSHA)
	bom.Version = 1
	root := cdx.Component{BOMRef: "repo:" + string(repo.Slug), Type: cdx.ComponentTypeApplication, Name: CleanText(repo.Ref.Name, 200), Group: CleanText(repo.Ref.Owner, 200)}
	tool := cdx.Component{Type: cdx.ComponentTypeApplication, Name: "source-code-portal", Version: version}
	bom.Metadata = &cdx.Metadata{Timestamp: f.ParsedAt.UTC().Format(time.RFC3339), Component: &root, Tools: &cdx.ToolsChoice{Components: &[]cdx.Component{tool}},
		Properties: &[]cdx.Property{{Name: "scp:treeSha", Value: f.TreeSHA}, {Name: "scp:parserSet", Value: f.ParserSet}, {Name: "scp:licences", Value: "declared-only"}}}

	comps := map[string]*cdx.Component{}
	depOf := map[string]map[string]struct{}{}
	add := func(id ComponentID, ver, kind string, scope Scope, licences []string, integrity string) string {
		if id.Namespace == LocalNamespace {
			return "" // a path inside the repository is not a package
		}
		ref, ok := reg.FormatPURL(id, "", ver, "")
		if !ok || ref == "" {
			return "" // a component without a purl (ecosystem formatter said so) is not exported
		}
		if c, ok := comps[ref]; ok {
			if c.Scope == cdx.ScopeOptional && scope == ScopeRuntime {
				c.Scope = cdx.ScopeRequired
			}
			return ref
		}
		c := &cdx.Component{BOMRef: ref, Type: cdx.ComponentTypeLibrary, Name: CleanText(id.Name, 200), Group: CleanText(id.Namespace, 200), Version: CleanText(ver, 100), PackageURL: ref}
		if kind == "application" {
			c.Type = cdx.ComponentTypeApplication
		}
		switch scope {
		case ScopeRuntime, ScopePeer, ScopeProvided:
			c.Scope = cdx.ScopeRequired
		case ScopeDev, ScopeTest, ScopeBuild, ScopeOptional:
			c.Scope = cdx.ScopeOptional
		}
		if len(licences) > 0 {
			lic := make(cdx.Licenses, 0, len(licences))
			for _, l := range licences {
				lic = append(lic, cdx.LicenseChoice{Expression: CleanText(l, 200)})
			}
			c.Licenses = &lic
		}
		if alg, hex, ok := strings.Cut(integrity, ":"); ok && hex != "" { // "<alg>:<lowercase hex>"; "h1:<base64>" (Go) and unknown algorithms are not exported
			if a, ok := map[string]cdx.HashAlgorithm{"sha1": cdx.HashAlgoSHA1, "sha256": cdx.HashAlgoSHA256, "sha512": cdx.HashAlgoSHA512}[alg]; ok {
				c.Hashes = &[]cdx.Hash{{Algorithm: a, Value: hex}}
			}
		}
		comps[ref] = c
		return ref
	}
	link := func(from, to string) {
		if from == "" || to == "" || from == to {
			return
		}
		if depOf[from] == nil {
			depOf[from] = map[string]struct{}{}
		}
		depOf[from][to] = struct{}{}
	}
	var unitRefs []string
	missing, omittedN := 0, 0
	for _, u := range f.Units {
		if u.Role == RoleFixture {
			continue
		}
		uref := "unit:" + string(repo.Slug) + ":" + u.ID
		unitRefs = append(unitRefs, uref)
		comps[uref] = &cdx.Component{BOMRef: uref, Type: cdx.ComponentTypeApplication, Name: CleanText(u.ID, 200), Properties: &[]cdx.Property{{Name: "scp:ecosystem", Value: string(u.Ecosystem)}, {Name: "scp:dir", Value: u.Dir}}}
		for _, d := range u.Dependencies {
			link(uref, add(d.Target, d.Version, "library", d.Scope, nil, ""))
		}
		if lc, ok := locked[u.ID]; ok {
			refs := make([]string, len(lc))
			for i, l := range lc {
				refs[i] = add(l.ID, l.Version, "library", l.Scope, l.Licenses, l.Integrity)
			}
			for i, l := range lc {
				for _, j := range l.DependsOn {
					if j >= 0 && j < len(refs) {
						link(refs[i], refs[j])
					}
				}
			}
		} else if omitted[u.ID] {
			omittedN += u.LockedCount
		} else if u.LockedCount > 0 {
			missing++
		}
	}
	if missing > 0 {
		props := append(*bom.Metadata.Properties, cdx.Property{Name: "scp:lockedUnavailable", Value: fmt.Sprint(missing)})
		bom.Metadata.Properties = &props
	}
	if omittedN > 0 {
		props := append(*bom.Metadata.Properties, cdx.Property{Name: "scp:lockedOmitted", Value: fmt.Sprint(omittedN)})
		bom.Metadata.Properties = &props
	}
	refs := make([]string, 0, len(comps))
	for r := range comps {
		refs = append(refs, r)
	}
	slices.Sort(refs)
	out := make([]cdx.Component, 0, len(refs))
	for _, r := range refs {
		out = append(out, *comps[r])
	}
	bom.Components = &out
	deps := []cdx.Dependency{{Ref: root.BOMRef, Dependencies: &unitRefs}}
	froms := make([]string, 0, len(depOf))
	for k := range depOf {
		froms = append(froms, k)
	}
	slices.Sort(froms)
	for _, from := range froms {
		to := make([]string, 0, len(depOf[from]))
		for t := range depOf[from] {
			to = append(to, t)
		}
		slices.Sort(to)
		deps = append(deps, cdx.Dependency{Ref: from, Dependencies: &to})
	}
	bom.Dependencies = &deps
	return bom
}

// WriteBOM encodes the document as JSON.
func WriteBOM(w io.Writer, bom *cdx.BOM) error {
	enc := cdx.NewBOMEncoder(w, cdx.BOMFileFormatJSON)
	enc.SetPretty(true)
	return enc.EncodeVersion(bom, CycloneDXSpec)
}

func uuidFrom(s string) string {
	h := sha256.Sum256([]byte(s))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
