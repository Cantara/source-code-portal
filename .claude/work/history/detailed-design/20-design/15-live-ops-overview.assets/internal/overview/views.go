package overview

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/overview/overviewapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// The interfaces below are consumer-defined: overview names what it reads, and the feature roots (rank 4, which overview
// may import at rank 5) satisfy them directly with the accessor listed in design 15 section 2.3. The view structs are
// the minimum overview needs; in production code the owner's own type replaces the stand-in where the owner exports one
// (commits.View, integrations.Status). Nothing here is stateful.

// Sourced is a resolved field with its provenance (descriptor, catalog-info, codeowners, github-topics, github, derived).
type Sourced struct {
	V      string
	Source openapi.Provenance
}

type OwnerView struct {
	Handle, Kind string // kind: user | team | group | unresolved
	Source       openapi.Provenance
}

type LinkView struct {
	URL, Title, Type string
	Source           openapi.Provenance
}

type APIView struct {
	Type, Title, Path, URL, SourceURL string // Path, URL, SourceURL may be ""
	Source                            openapi.Provenance
}

type DependsOnView struct {
	Repo     string   // the reference as authored
	Slug     scm.Slug // "" when Resolved is false
	Resolved bool
	Source   openapi.Provenance
}

// MetaView is the resolved metadata of one repository, merged by docs at ingestion (descriptor > catalog-info >
// CODEOWNERS/topics > derived, architecture 14.1). A nil Sourced means "absent"; overview fills the derived fallbacks
// (title from the repository name, summary from the GitHub description, tags from topics) when docs holds nothing yet.
type MetaView struct {
	Title, Summary, Type, Lifecycle *Sourced
	Tags                            []Sourced
	Owners                          []OwnerView
	Links                           []LinkView
	System                          string
	APIs                            []APIView
	DependsOn                       []DependsOnView
	Hidden                          bool // descriptor hidden: true; omitted from every list, llms output and the sitemap (not access control)
	Descriptor                      openapi.DescriptorInfo
}

// DocKind is the role of a document.
type DocKind string

const (
	DocReadme    DocKind = "readme"
	DocDoc       DocKind = "doc"
	DocAgents    DocKind = "agents"
	DocChangelog DocKind = "changelog"
)

// DocEntry is one indexed, non-hidden document, in navigation order (README first). Hidden documents are never returned.
type DocEntry struct {
	Kind           DocKind
	ID             string // readme | path with "/" replaced by "~"
	Path           string // repository-relative, forward slashes
	Title          string
	Description    string // untrusted
	Format         string // markdown | asciidoc | text
	State          string // resident | not_resident | too_large | unsupported | no_content
	BlobSHA        string
	Order          *int
	Tags           []string
	Section        []string
	GithubRendered bool
	UpdatedAt      time.Time
	SourceURL      string
}

// DocsSource is satisfied by the docs root's service.
type DocsSource interface {
	// Meta returns the resolved metadata of a repository; ok is false before docs has produced anything for it.
	Meta(s *readmodel.Snapshot, slug scm.Slug) (MetaView, bool)
	// Index returns the non-hidden documents and the freshness of the docs item. ok is false before the first docs publish.
	Index(s *readmodel.Snapshot, slug scm.Slug) (entries []DocEntry, info freshness.Info, ok bool)
	// Body returns the LLM-normalised Markdown body of a document: the same bytes the raw .md endpoint serves after its
	// front matter (HTML comments and invisible characters removed). ok is false for a document that is not resident
	// or not Markdown; the call never reaches the network and reads at most one store row.
	Body(ctx context.Context, slug scm.Slug, id string) (md string, ok bool)
}

// CommitsSource is satisfied by the commits root's service.
type CommitsSource interface {
	// Recent returns up to n newest commits of a repository and the freshness of its commits item.
	Recent(s *readmodel.Snapshot, slug scm.Slug, n int) ([]scm.Commit, freshness.Info, bool)
}

// TechView is the manifests digest of one repository; nil Summary means no facts yet.
type TechView struct {
	Summary *overviewapi.TechnologySummary
	Info    freshness.Info
}

// ManifestsSource is satisfied by the manifests root's service. Enabled is false when manifests.enabled is false.
type ManifestsSource interface {
	Enabled() bool
	Technology(s *readmodel.Snapshot, slug scm.Slug) (TechView, bool)
}

// ProviderState is the integrations status vocabulary (stand-in for integrations.Status).
type ProviderStatusView struct {
	Provider   string
	State      string // ok | failing | unstable | unknown | disabled
	Summary    string
	DetailURL  string
	ObservedAt time.Time
	Counts     map[string]int
}

// IntegrationsSource is satisfied by *integrations.Service (StatusOf and Providers have exactly these shapes).
type IntegrationsSource interface {
	Providers() []string
	StatusOf(s *readmodel.Snapshot, slug scm.Slug, provider string) freshness.Value[ProviderStatusView]
}

// BadgesSource is satisfied by the badges root: the badge list of one repository, in the documented order.
type BadgesSource interface {
	For(slug scm.Slug, providers []string) []openapi.Badge
}

// PageEntry is one entry of the pages set (title is untrusted operator text; slug is validated).
type PageEntry struct {
	Slug      string
	UpdatedAt time.Time
}

// AnnouncementView is the pages-owned announcement (plain text plus a level).
type AnnouncementView struct {
	Text        string
	Level       string // info | warning | critical
	Dismissible bool
}

// PagesSource is satisfied by the pages root's service.
type PagesSource interface {
	Announcement() (AnnouncementView, bool)
	Pages(s *readmodel.Snapshot) []PageEntry
}

// BuildInfo is what api.site publishes of the build.
type BuildInfo = openapi.BuildInfo

// Slots are the handles overview reads; the owners register them, overview registers only Site.
type Slots struct {
	Repos      readmodel.Slot[freshness.Value[scm.Repository]]
	Stats      readmodel.Slot[freshness.Value[scm.RepoStats]]
	Membership readmodel.Global[scm.Membership]
	Aliases    readmodel.Global[map[scm.Slug]scm.Slug]
	Site       readmodel.Global[scm.Classed[SiteMeta]] // registered by overview (RegisterSlots)
}

// SiteMeta is the `site.meta` global (one per view class): the count never leaks the number of private repositories.
type SiteMeta struct {
	RepoCount    int
	GroupCount   int
	LastFullSync time.Time // zero: never
}
