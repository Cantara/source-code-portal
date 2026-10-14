package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
	"go.yaml.in/yaml/v3"
)

// Provenance names where a resolved metadata field came from (the API enum Provenance, precedence in architecture 14.2).
type Provenance string

const (
	FromDescriptor  Provenance = "descriptor"
	FromCatalogInfo Provenance = "catalog-info"
	FromCodeowners  Provenance = "codeowners"
	FromTopics      Provenance = "github-topics"
	FromGitHub      Provenance = "github"
	FromDerived     Provenance = "derived"
)

// Descriptor is the typed projection of a pruned scp.yaml (every field is optional; zero values mean absent).
type Descriptor struct {
	Schema       int          `json:"schema"`
	Title        string       `json:"title"`
	Summary      string       `json:"summary"`
	Type         string       `json:"type"`
	Lifecycle    string       `json:"lifecycle"`
	Owners       []string     `json:"owners"`
	System       string       `json:"system"`
	Tags         []string     `json:"tags"`
	Links        []DescLink   `json:"links"`
	Docs         DescDocs     `json:"docs"`
	APIs         []DescAPI    `json:"apis"`
	DependsOn    []string     `json:"dependsOn"`
	Integrations Integrations `json:"integrations"`
	Hidden       bool         `json:"hidden"`
}

// DescLink is a link of the descriptor.
type DescLink struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// DescDocs is the docs block.
type DescDocs struct {
	Root    string    `json:"root"`
	Include []string  `json:"include"`
	Exclude []string  `json:"exclude"`
	Nav     []NavItem `json:"nav"`
}

// NavItem is one navigation entry: a path, a path with a title, or a titled group.
type NavItem struct {
	Path  string    `json:"path"`
	Title string    `json:"title"`
	Items []NavItem `json:"items"`
}

// UnmarshalJSON accepts the three schema shapes (string, {path,title}, {title,items}).
func (n *NavItem) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*n = NavItem{Path: s}
		return nil
	}
	type plain NavItem
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*n = NavItem(p)
	return nil
}

// DescAPI is an API reference.
type DescAPI struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Path  string `json:"path"`
	URL   string `json:"url"`
}

// Integrations holds the per-repository integration targets; instance names refer to portal configuration.
type Integrations struct {
	Jenkins *struct {
		Instance string `json:"instance"`
		Job      string `json:"job"`
	} `json:"jenkins"`
	Snyk *struct {
		Project string `json:"project"`
	} `json:"snyk"`
}

// CatalogInfo is the subset of a Backstage catalog-info.yaml docs reads (kind Component only).
type CatalogInfo struct {
	Name, Title, Description string
	Tags                     []string
	Links                    []DescLink
	Owner, Lifecycle, Type   string // raw spec values (an owner is a reference such as group:default/team-a)
	System                   string
}

// DescriptorView is the descriptor state of one repository: the pruned typed value, the fallbacks and the problems.
type DescriptorView struct {
	Present     bool
	File        string // "scp.yaml" or ".github/scp.yaml"
	BlobSHA     string
	Value       Descriptor
	Problems    []DescriptorProblem
	CatalogInfo *CatalogInfo
	CatalogBlob string
	CodeOwners  []string // owners of the catch-all pattern of CODEOWNERS, in file order
	OwnersBlob  string
}

// DecodeDescriptor turns the pruned JSON-shaped value of DescriptorLoader.Load into the typed Descriptor.
func DecodeDescriptor(pruned map[string]any) (Descriptor, error) {
	b, err := json.Marshal(pruned)
	if err != nil {
		return Descriptor{}, err
	}
	var d Descriptor
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&d); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}

// ParseCatalogInfo reads the first document of kind Component of a catalog-info.yaml. Anchors and aliases are not
// accepted, at most 64 KiB are read, and anything else is ignored: the file is an input of a fallback, never an authority.
func ParseCatalogInfo(src []byte) (CatalogInfo, error) {
	if len(src) > MaxDescriptorBytes {
		return CatalogInfo{}, errors.New("catalog-info.yaml exceeds 64 KiB")
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	for i := 0; i < 8; i++ {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return CatalogInfo{}, err
		}
		if walkNode(&n, 0) != "" {
			return CatalogInfo{}, errors.New("anchors, aliases and merge keys are not allowed")
		}
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name        string   `yaml:"name"`
				Title       string   `yaml:"title"`
				Description string   `yaml:"description"`
				Tags        []string `yaml:"tags"`
				Links       []struct {
					URL   string `yaml:"url"`
					Title string `yaml:"title"`
					Type  string `yaml:"type"`
				} `yaml:"links"`
			} `yaml:"metadata"`
			Spec struct {
				Owner     string `yaml:"owner"`
				Lifecycle string `yaml:"lifecycle"`
				Type      string `yaml:"type"`
				System    string `yaml:"system"`
			} `yaml:"spec"`
		}
		if err := n.Decode(&doc); err != nil || doc.Kind != "Component" {
			continue
		}
		// Every field is repository-authored and reaches the API (`Tag.value` has a pattern, link `type` has a pattern) and
		// the LLM outputs: clean or validate each one, never copy it (verification V-06).
		ci := CatalogInfo{Name: CleanLine(doc.Metadata.Name, 120), Title: CleanLine(doc.Metadata.Title, 120), Description: CleanLine(doc.Metadata.Description, 300),
			Owner: CleanLine(doc.Spec.Owner, 140), Lifecycle: CleanLine(doc.Spec.Lifecycle, 32), Type: CleanLine(doc.Spec.Type, 32), System: CleanLine(doc.Spec.System, 63)}
		for _, t := range doc.Metadata.Tags {
			if len(ci.Tags) < 20 && len(t) <= 63 && catalogTagRE.MatchString(t) {
				ci.Tags = append(ci.Tags, t)
			}
		}
		for _, l := range doc.Metadata.Links {
			if len(ci.Links) < 30 && strings.HasPrefix(l.URL, "https://") && len(l.URL) <= 2000 && !strings.ContainsAny(l.URL, " \t\r\n\x00") {
				lt := l.Type
				if !linkTypeRE.MatchString(lt) {
					lt = ""
				}
				ci.Links = append(ci.Links, DescLink{URL: l.URL, Title: CleanLine(l.Title, 120), Type: lt})
			}
		}
		return ci, nil
	}
	return CatalogInfo{}, errors.New("no Component document")
}

var (
	catalogTagRE = regexp.MustCompile(`^[a-z0-9:+#]+(-[a-z0-9:+#]+)*$`)
	linkTypeRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// CleanLine collapses whitespace and control characters of repository-authored text to single spaces and truncates to
// max runes. It is applied to every free-text field before it reaches an API, a log line or an LLM output.
func CleanLine(s string, max int) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 || (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfeff || (r >= 0xe0000 && r <= 0xe007f) {
			r = ' '
		}
		if r == ' ' {
			if space || b.Len() == 0 {
				continue
			}
			space = true
		} else {
			space = false
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// ParseCodeOwners returns the owners of the last catch-all pattern ("*") of a CODEOWNERS file (GitHub's last-match rule).
func ParseCodeOwners(src []byte) []string {
	var owners []string
	for _, line := range strings.Split(string(src), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "*" {
			continue
		}
		var o []string
		for _, h := range f[1:] {
			if validOwner(h) {
				o = append(o, h)
			}
		}
		if len(o) > 0 {
			owners = o
		}
	}
	if len(owners) > 20 {
		owners = owners[:20]
	}
	return owners
}

func validOwner(h string) bool {
	if !strings.HasPrefix(h, "@") || len(h) < 2 || len(h) > 140 {
		return false
	}
	for _, r := range h[1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '/') {
			return false
		}
	}
	return true
}

// Sourced is a resolved value with its provenance.
type Sourced[T any] struct {
	Value  T
	Source Provenance
}

// OwnerClaim is an ownership claim, never an authorisation.
type OwnerClaim struct {
	Handle string
	Kind   string // user, team, group, unresolved
	Source Provenance
}

// Resolved is the read-time merge of descriptor, catalog-info, CODEOWNERS, topics and repository facts.
// Precedence per field: descriptor, catalog-info, CODEOWNERS (owners only) or topics (tags only), derived.
type Resolved struct {
	Title     Sourced[string]
	Summary   Sourced[string]
	Type      Sourced[string]
	Lifecycle Sourced[string]
	System    Sourced[string]
	Owners    []OwnerClaim
	Tags      []Sourced[string]
	Links     []Sourced[DescLink]
	Hidden    bool
}

// Resolve merges the inputs. It is pure: same input, same output, no clock, no I/O. readmeTitle is the first heading
// of the README ("" when none).
func Resolve(v DescriptorView, repo scm.Repository, readmeTitle string) Resolved {
	d, ci := v.Value, v.CatalogInfo
	var r Resolved
	r.Hidden = d.Hidden
	r.Title = pick(d.Title, FromDescriptor)
	if r.Title.Value == "" && ci != nil {
		t := ci.Title
		if t == "" {
			t = ci.Name
		}
		r.Title = pick(t, FromCatalogInfo)
	}
	if r.Title.Value == "" && readmeTitle != "" {
		r.Title = pick(readmeTitle, FromDerived)
	}
	if r.Title.Value == "" {
		r.Title = pick(repo.Ref.Name, FromDerived)
	}
	r.Summary = pick(d.Summary, FromDescriptor)
	if r.Summary.Value == "" && ci != nil {
		r.Summary = pick(ci.Description, FromCatalogInfo)
	}
	if r.Summary.Value == "" {
		r.Summary = pick(CleanLine(repo.Description, 300), FromGitHub)
	}
	r.Type = pick(d.Type, FromDescriptor)
	if r.Type.Value == "" && ci != nil {
		r.Type = pick(mapType(ci.Type), FromCatalogInfo)
	}
	r.Lifecycle = pick(d.Lifecycle, FromDescriptor)
	if r.Lifecycle.Value == "" && ci != nil {
		r.Lifecycle = pick(mapLifecycle(ci.Lifecycle), FromCatalogInfo)
	}
	r.System = pick(d.System, FromDescriptor)
	if r.System.Value == "" && ci != nil {
		r.System = pick(ci.System, FromCatalogInfo)
	}
	switch {
	case len(d.Owners) > 0:
		r.Owners = claims(d.Owners, FromDescriptor)
	case ci != nil && ci.Owner != "":
		r.Owners = []OwnerClaim{{Handle: ci.Owner, Kind: ownerKind(ci.Owner), Source: FromCatalogInfo}}
	case len(v.CodeOwners) > 0:
		r.Owners = claims(v.CodeOwners, FromCodeowners)
	}
	seen := map[string]bool{}
	add := func(t string, p Provenance) {
		if t != "" && !seen[t] {
			seen[t] = true
			r.Tags = append(r.Tags, Sourced[string]{t, p})
		}
	}
	for _, t := range d.Tags {
		add(t, FromDescriptor)
	}
	if ci != nil {
		for _, t := range ci.Tags {
			add(t, FromCatalogInfo)
		}
	}
	for _, t := range repo.Topics {
		add(t, FromTopics)
	}
	sort.SliceStable(r.Tags, func(i, j int) bool { return r.Tags[i].Value < r.Tags[j].Value })
	seenURL := map[string]bool{}
	for _, l := range d.Links {
		if !seenURL[l.URL] {
			seenURL[l.URL] = true
			r.Links = append(r.Links, Sourced[DescLink]{l, FromDescriptor})
		}
	}
	if ci != nil {
		for _, l := range ci.Links {
			if !seenURL[l.URL] {
				seenURL[l.URL] = true
				r.Links = append(r.Links, Sourced[DescLink]{l, FromCatalogInfo})
			}
		}
	}
	return r
}

func pick(v string, p Provenance) Sourced[string] {
	if v == "" {
		return Sourced[string]{}
	}
	return Sourced[string]{v, p}
}

func claims(hs []string, p Provenance) []OwnerClaim {
	out := make([]OwnerClaim, 0, len(hs))
	for _, h := range hs {
		out = append(out, OwnerClaim{Handle: h, Kind: ownerKind(h), Source: p})
	}
	return out
}

func ownerKind(h string) string {
	switch {
	case strings.HasPrefix(h, "@") && strings.Contains(h, "/"):
		return "team"
	case strings.HasPrefix(h, "@"):
		return "user"
	case strings.HasPrefix(h, "group:") || strings.HasPrefix(h, "user:"):
		if strings.HasPrefix(h, "group:") {
			return "group"
		}
		return "user"
	}
	return "unresolved"
}

func mapLifecycle(s string) string {
	switch s {
	case "experimental", "production", "deprecated":
		return s
	}
	return ""
}

func mapType(s string) string {
	switch s {
	case "service", "library", "website":
		return s
	case "documentation":
		return "documentation"
	case "":
		return ""
	}
	return "other"
}
