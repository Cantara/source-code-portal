// Package github is a STUB of the pieces of the adapter that the CLI calls (design 05 sections 3.6, 4.13, 4.14): the doctor
// finding model, DoctorOptions, ManifestOptions/ManifestResult and RunAppManifest. Signatures are copied from 05-github-adapter.md.
package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

type Severity uint8

const (
	SevInfo Severity = iota
	SevWarn
	SevFail
)

func (s Severity) String() string { return [...]string{"INFO", "WARN", "FAIL"}[s] }

type Finding struct {
	ID          string   `json:"id"`
	Severity    Severity `json:"severity"`
	Source      string   `json:"source"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Remediation string   `json:"remediation"`
}

type Feature string

const (
	FeatureCore     Feature = "core"
	FeaturePullReqs Feature = "pull_requests"
	FeatureIssues   Feature = "issues"
	FeatureStatuses Feature = "commit_statuses"
	FeatureChecks   Feature = "checks"
	FeatureActions  Feature = "actions"
)

type DoctorOptions struct {
	Sources   []scm.SourceID
	Offline   bool
	Features  []Feature
	PublicURL string
	Timeout   time.Duration
}

type ManifestOptions struct {
	WebURL     string
	APIURL     string
	Org        string
	Name       string
	WebhookURL string
	NoWebhook  bool
	Features   []Feature
	OutDir     string
	Code       string
	Listen     string
	Timeout    time.Duration
	HTTP       *http.Client
	Out        io.Writer
}

type ManifestResult struct {
	AppID             int64
	Slug              string
	ClientID          string
	HTMLURL           string
	PEMPath           string
	WebhookSecretPath string
	InstallURL        string
	ConfigSnippet     string
}

var ErrNotImplemented = errors.New("github: not implemented")

func RunAppManifest(context.Context, ManifestOptions) (ManifestResult, error) {
	return ManifestResult{}, ErrNotImplemented
}

// Network and HTTPConfig: design 05 sections 2.4 and 6.1 (the factory contract between the adapter and cmd/scp).
type HTTPConfig struct {
	Timeout               time.Duration
	GraphQLTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	MaxIdleConnsPerHost   int
}

type Network struct {
	Source                scm.SourceID
	Hosts                 []string // pinned hosts: API, GraphQL and web hosts of the source ("host" or "host:port")
	ProxyURL              string
	CAFile                string
	AllowPrivateAddresses bool
	AllowInsecureHTTP     bool
	HTTP                  HTTPConfig
}

// ReserveClass is the adapter-side image of job.Priority (05 section 4.4, ratelimit.go).
type ReserveClass uint8

const (
	ClassBackfill ReserveClass = iota
	ClassScheduled
	ClassWebhook
	ClassInteractive
)

// Client stands for *github.Client where the bridges need only its budget gate (05 section 2.3).
type Client struct{}

// Admit is the budget gate: zero admits, otherwise the time to wait (05 section 4.4).
func (c *Client) Admit(class ReserveClass) time.Duration { return 0 }
