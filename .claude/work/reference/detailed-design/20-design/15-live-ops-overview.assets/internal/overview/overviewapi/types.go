// Package overviewapi stands in for the oapi-codegen output of internal/overview/overviewapi/overview.yaml (schemas Site,
// NavItem, Announcement, SiteFeatures, RepoSummary, Repo, RepoList and their parts of api/openapi-public.yaml). It is
// hand-written, in the field order and with the JSON names of that document, only so that unit 15 compiles. Shared
// schemas come from the real httpapi/openapi package.
package overviewapi

import (
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
)

type NavItem struct {
	Label string  `json:"label"`
	Kind  string  `json:"kind"` // page | external
	Page  *string `json:"page,omitempty"`
	Url   *string `json:"url,omitempty"`
}

type Announcement struct {
	Id          string `json:"id"`
	Text        string `json:"text"`
	Level       string `json:"level"` // info | warning | critical
	Dismissible bool   `json:"dismissible"`
}

type SiteFeatures struct {
	Manifests    bool     `json:"manifests"`
	Sse          bool     `json:"sse"`
	Integrations []string `json:"integrations"`
}

type Site struct {
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Nav             []NavItem         `json:"nav"`
	Announcement    *Announcement     `json:"announcement,omitempty"`
	DisplayTimeZone string            `json:"displayTimeZone"`
	DefaultTheme    string            `json:"defaultTheme"`
	LastFullSync    *time.Time        `json:"lastFullSync"`
	RepoCount       int               `json:"repoCount"`
	GroupCount      int               `json:"groupCount"`
	Features        SiteFeatures      `json:"features"`
	Build           openapi.BuildInfo `json:"build"`
}

type SourcedString struct {
	Value  string             `json:"value"`
	Source openapi.Provenance `json:"source"`
}

type Tag = SourcedString

type GroupRef struct {
	Id          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Release struct {
	Tag         string     `json:"tag"`
	Name        *string    `json:"name"`
	Url         string     `json:"url"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt *time.Time `json:"publishedAt"`
}

type License struct {
	SpdxId string  `json:"spdxId"`
	Name   *string `json:"name"`
}

type RepoStats struct {
	OpenIssues    *int       `json:"openIssues"`
	Stars         int        `json:"stars"`
	License       *License   `json:"license,omitempty"`
	LatestRelease *Release   `json:"latestRelease,omitempty"`
	PushedAt      *time.Time `json:"pushedAt"`
	HeadSha       *string    `json:"headSha"`
	LastCommitAt  *time.Time `json:"lastCommitAt"`
}

type RepoSummary struct {
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Owner         string            `json:"owner"`
	Title         SourcedString     `json:"title"`
	Summary       *SourcedString    `json:"summary,omitempty"`
	Type          *SourcedString    `json:"type,omitempty"`
	Lifecycle     *SourcedString    `json:"lifecycle,omitempty"`
	Archived      bool              `json:"archived"`
	Fork          bool              `json:"fork"`
	Visibility    string            `json:"visibility"`
	DefaultBranch string            `json:"defaultBranch"`
	HtmlUrl       string            `json:"htmlUrl"`
	Tags          []Tag             `json:"tags"`
	Groups        []GroupRef        `json:"groups"`
	Stats         RepoStats         `json:"stats"`
	Badges        []openapi.Badge   `json:"badges"`
	UiUrl         string            `json:"uiUrl"`
	ApiUrl        string            `json:"apiUrl"`
	MarkdownUrl   *string           `json:"markdownUrl"`
	Freshness     openapi.Freshness `json:"freshness"`
}

type RepoList struct {
	Items     []RepoSummary     `json:"items"`
	Page      openapi.PageInfo  `json:"page"`
	Freshness openapi.Freshness `json:"freshness"`
}

type Owner struct {
	Handle string             `json:"handle"`
	Kind   string             `json:"kind"`
	Source openapi.Provenance `json:"source"`
}

type Link struct {
	Url    string             `json:"url"`
	Title  string             `json:"title,omitempty"`
	Type   string             `json:"type,omitempty"`
	Source openapi.Provenance `json:"source"`
}

type ApiRef struct {
	Type      string             `json:"type"`
	Title     string             `json:"title"`
	Path      *string            `json:"path"`
	Url       *string            `json:"url"`
	SourceUrl *string            `json:"sourceUrl"`
	Source    openapi.Provenance `json:"source"`
}

type DependsOn struct {
	Repo     string             `json:"repo"`
	Slug     *string            `json:"slug,omitempty"`
	Resolved bool               `json:"resolved"`
	Source   openapi.Provenance `json:"source"`
}

type ProviderStatus struct {
	Provider   string            `json:"provider"`
	State      string            `json:"state"`
	Summary    string            `json:"summary"`
	DetailUrl  *string           `json:"detailUrl"`
	ObservedAt *time.Time        `json:"observedAt"`
	Counts     map[string]int    `json:"counts"`
	Freshness  openapi.Freshness `json:"freshness"`
}

type TechnologySummary struct {
	Languages               []openapi.LanguageShare `json:"languages"`
	Ecosystems              []string                `json:"ecosystems"`
	BuildTools              []string                `json:"buildTools"`
	Toolchain               map[string]string       `json:"toolchain"`
	UnitCount               int                     `json:"unitCount"`
	DirectDependencyCount   int                     `json:"directDependencyCount"`
	InternalDependencyCount int                     `json:"internalDependencyCount"`
	DiagnosticCount         int                     `json:"diagnosticCount"`
	ParsedAt                *time.Time              `json:"parsedAt"`
	ManifestsUrl            string                  `json:"manifestsUrl"`
}

type RepoSections struct {
	Commits      *openapi.Freshness `json:"commits,omitempty"`
	Docs         *openapi.Freshness `json:"docs,omitempty"`
	Manifests    *openapi.Freshness `json:"manifests,omitempty"`
	Integrations *openapi.Freshness `json:"integrations,omitempty"`
}

// Repo is the composite: every RepoSummary member plus the sections (allOf in the contract; embedded here, which
// encoding/json flattens).
type Repo struct {
	RepoSummary
	Description   *string                 `json:"description"`
	Owners        []Owner                 `json:"owners"`
	Links         []Link                  `json:"links"`
	System        *string                 `json:"system"`
	Apis          []ApiRef                `json:"apis"`
	DependsOn     []DependsOn             `json:"dependsOn"`
	Docs          []openapi.DocIndexEntry `json:"docs"`
	Descriptor    openapi.DescriptorInfo  `json:"descriptor"`
	Statuses      []ProviderStatus        `json:"statuses"`
	Technology    *TechnologySummary      `json:"technology,omitempty"`
	LatestCommits []openapi.Commit        `json:"latestCommits"`
	Sections      RepoSections            `json:"sections"`
}
