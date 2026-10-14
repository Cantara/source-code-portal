package docs

import (
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/safehtml"
)

// Names of the snapshot entries and the store item kind (registered by cmd/scp with exactly these names).
const (
	SlotName = "docs.repo"
	ItemKind = "docs.repo"
	JobKind  = "docs.refresh"
)

// Format is the document format as the API names it.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatAsciiDoc Format = "asciidoc"
	FormatText     Format = "text"
)

// State is the availability of a document body (the API enum DocState).
type State string

const (
	StateResident    State = "resident"
	StateNotResident State = "not_resident"
	StateTooLarge    State = "too_large"
	StateUnsupported State = "unsupported"
	StateNoContent   State = "no_content"
)

// DocID identifies an indexed document inside one repository: "readme" or the repository path with '/' replaced by '~'.
type DocID string

// ReadmeID is the id of the README.
const ReadmeID DocID = "readme"

// Entry is one row of the documentation index (everything the list endpoint shows, no body).
type Entry struct {
	ID          DocID
	Path        string // repository-relative, '/' separated
	Key         string // route key: Path without its extension; the portal URL is routes.RepoDoc(slug, Key)
	Title       string
	Description string // "" = null
	Format      Format
	Bridged     bool // GitHub-rendered AsciiDoc (renderedBy: github)
	State       State
	Order       *int
	Tags        []string
	Section     []string // navigation group titles, outermost first
	BlobSHA     string
	UpdatedAt   time.Time // commit time is unknown to docs: the time the blob last changed in our view (FetchedAt of the run that saw the new SHA)
	Size        int64
	Aux         bool   // AGENTS.md / CHANGELOG.md at the repository root (outside docs.root); owns the agents.md / changelog.md routes
	Pending     bool   // the file has not been fetched yet (continuation run): Title is derived from the path
	Reason      string // why State is StateNotResident: "budget" (per-repository or global bytes), "count_cap", "pending"
}

// TOCEntry is one heading of a rendered document.
type TOCEntry struct {
	Depth int
	Text  string
	ID    string // always prefixed "doc-"
}

// LinkKind classifies an outgoing link after rewriting.
type LinkKind string

const (
	LinkDoc      LinkKind = "doc"
	LinkRepoFile LinkKind = "repo-file"
	LinkExternal LinkKind = "external"
	LinkAnchor   LinkKind = "anchor"
	LinkMedia    LinkKind = "media"
)

// Link is an outgoing link of a document.
type Link struct {
	Text string
	Href string
	Kind LinkKind
}

// Problem is a non-fatal front-matter or rendering problem of one document.
type Problem struct{ Code, Message string }

// DocView is one document of a repository. HTML and Markdown are zero unless Entry.State is StateResident.
type DocView struct {
	Entry       Entry
	HTML        safehtml.SafeHTML // sanitised; built only by safehtml.Sanitize or safehtml.Text
	Markdown    string            // LLM variant: comments and invisible characters stripped (the raw .md body without the scp: header)
	TOC         []TOCEntry
	FrontMatter map[string]any
	Links       []Link
	Problems    []Problem
	RenderedBy  string // "scp" or "github"
	Normalised  []string
	WordCount   int
	Source      string // original source text, kept only to re-render after a pipeline change (resident Markdown documents)
	FetchedAt   time.Time
	Images      []ImageRef // repository images the rendered HTML points at, with the blob SHA seen at render time (sorted by path)
}

// ImageRef is a repository-relative image a document references. media URLs embed the COMMIT the document was read at, so
// a changed image blob must re-render every document that references it (verification V-12).
type ImageRef struct{ Path, BlobSHA string }

// Bytes is the resident size of the view used for admission and metrics.
func (v DocView) Bytes() int64 {
	return int64(len(v.HTML.String()) + len(v.Markdown) + len(v.Source) + 64*len(v.TOC))
}

// RepoDocs is the value of the docs.repo slot: everything docs knows about one repository.
type RepoDocs struct {
	Readme      DocView
	Docs        []DocView // index order (navigation order); hidden documents are not present
	Descriptor  DescriptorView
	HeadSHA     string // the commit the content was read at
	TreeOID     string // tree object id of HeadSHA when known
	Fingerprint string // digest of every input path and blob SHA; equal fingerprint = nothing to re-fetch
	Pipeline    string // PipelineID the HTML was produced with
	Info        freshness.Info
	Pending     int // documents still waiting for a continuation run
}

// ImageSet is the union of every document's referenced images: path to blob SHA at render time.
func (r *RepoDocs) ImageSet() map[string]string {
	m := map[string]string{}
	add := func(v *DocView) {
		for _, im := range v.Images {
			m[im.Path] = im.BlobSHA
		}
	}
	add(&r.Readme)
	for i := range r.Docs {
		add(&r.Docs[i])
	}
	return m
}

// Doc returns the document with the given id.
func (r *RepoDocs) Doc(id DocID) (*DocView, bool) {
	if id == ReadmeID {
		return &r.Readme, r.Readme.Entry.ID == ReadmeID
	}
	for i := range r.Docs {
		if r.Docs[i].Entry.ID == id {
			return &r.Docs[i], true
		}
	}
	return nil, false
}

// ByKey returns the document whose route key equals key (README excluded).
func (r *RepoDocs) ByKey(key string) (*DocView, bool) {
	for i := range r.Docs {
		if r.Docs[i].Entry.Key == key {
			return &r.Docs[i], true
		}
	}
	return nil, false
}

// ResidentBytes sums Bytes over resident views (README included).
func (r *RepoDocs) ResidentBytes() int64 {
	var n int64
	if r.Readme.Entry.State == StateResident {
		n += r.Readme.Bytes()
	}
	for i := range r.Docs {
		if r.Docs[i].Entry.State == StateResident {
			n += r.Docs[i].Bytes()
		}
	}
	return n
}

// DocIDOf maps a repository path to its DocID exactly as the contract's DocId schema defines it (07, common.yaml): every
// '/' becomes '~' and every other byte outside [A-Za-z0-9._-] (including a literal '~' and '%', and each UTF-8 byte of a
// non-ASCII name) is written %XX with upper-case hex. The encoding is injective, so two paths never share an id and no
// path is refused for its characters (verification V-01: the first version mapped only '/' and refused '~'). ok is false
// for an empty path or an id over 1,024 bytes.
func DocIDOf(path string) (DocID, bool) {
	if path == "" {
		return "", false
	}
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		switch c := path[i]; {
		case c == '/':
			b.WriteByte('~')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
		if b.Len() > 1024 {
			return "", false
		}
	}
	return DocID(b.String()), true
}

// PathOf is the inverse of DocIDOf for ids other than ReadmeID ("" for a malformed id).
func (id DocID) PathOf() string {
	s := string(id)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '~':
			b.WriteByte('/')
		case c == '%':
			if i+2 >= len(s) {
				return ""
			}
			hi, lo := unhex(s, i+1), unhex(s, i+2)
			if hi < 0 || lo < 0 {
				return ""
			}
			b.WriteByte(byte(hi<<4 | lo))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func unhex(s string, i int) int {
	if i >= len(s) {
		return -1
	}
	switch c := s[i]; {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

var extensions = [...]string{".markdown", ".asciidoc", ".adoc", ".md"}

// hasExt reports whether path ends with ext (lower-case ASCII), comparing the last bytes with an ASCII-only fold.
// strings.ToLower is wrong here: it folds U+212A KELVIN SIGN to 'k', so "x.mar\u212adown" would be a Markdown file whose
// byte length differs from the extension (verification V-09, the same class as V-05 of unit 14).
func hasExt(path, ext string) bool {
	if len(path) < len(ext) {
		return false
	}
	t := path[len(path)-len(ext):]
	for i := 0; i < len(ext); i++ {
		c := t[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != ext[i] {
			return false
		}
	}
	return true
}

// FormatOf detects the format from the file name, case-insensitively (ASCII). Unknown extensions are FormatText.
func FormatOf(path string) Format {
	switch {
	case hasExt(path, ".md"), hasExt(path, ".markdown"):
		return FormatMarkdown
	case hasExt(path, ".adoc"), hasExt(path, ".asciidoc"):
		return FormatAsciiDoc
	}
	return FormatText
}

// KeyOf returns the route key of a path: the path without its Markdown or AsciiDoc extension.
func KeyOf(path string) string {
	for _, e := range extensions {
		if hasExt(path, e) {
			return path[:len(path)-len(e)]
		}
	}
	return path
}
