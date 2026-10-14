package docs

import (
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Doc is one rendered document of a repository: the README or a Markdown file under docs/.
type Doc struct {
	Path        string // repository path
	README      bool
	Title       string
	Description string
	Order       int    // front matter order
	Hidden      bool   // front matter hidden: left out of the document list
	Format      string // markdown | text
	HTML        safehtml.SafeHTML
	Source      []byte    // the text after truncation, for the Markdown surface
	TOC         []Heading // h2 and h3
	BlobSHA     string
	Truncated   bool           // the source was cut at the size cap
	State       snapshot.State // ok | too_large | unsupported
	Images      []Image
}

// Image is one image a document shows, served at /images/{owner}/{name}/{ID}: a repository file (Path, with its BlobSHA when the
// tree has it within the size cap) or an external https image (URL).
type Image struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	URL     string `json:"url,omitempty"`
	BlobSHA string `json:"blobSha,omitempty"`
}

// Heading is a TOC entry.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// facet is a repository's documents: README first, then by order, then path.
const facet snapshot.Facet[[]Doc] = "docs"

// Of returns the documents of e, README first, with their freshness.
func Of(e *snapshot.RepoEntry) ([]Doc, snapshot.Freshness) { return facet.Get(e) }
