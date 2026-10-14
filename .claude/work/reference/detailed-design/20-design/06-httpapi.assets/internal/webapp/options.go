// Package webapp serves the embedded SPA: static files with precompressed variants, the history fallback,
// the document CSP and security headers and the CSP report endpoint. It knows no feature and imports nothing
// from this module: everything it needs arrives through Options.
package webapp

import (
	"context"
	"fmt"
	"strings"
)

// Listener selects the route manifest: the public listener serves the SPA routes, the admin one only /status.
type Listener string

const (
	Public Listener = "public"
	Admin  Listener = "admin"
)

// CSPMode is config key web.csp.mode.
type CSPMode string

const (
	// CSPReportOnly sends the full policy as Content-Security-Policy-Report-Only only (rollout step 1).
	CSPReportOnly CSPMode = "report_only"
	// CSPEnforceCore enforces everything except the Trusted Types directives, which stay report-only (step 2).
	CSPEnforceCore CSPMode = "enforce_core"
	// CSPEnforce enforces the full policy (step 3).
	CSPEnforce CSPMode = "enforce"
)

// Config is the part of the process configuration owned by this package (YAML key `web`).
type Config struct {
	CSP struct {
		Mode CSPMode `yaml:"mode" json:"mode"` // default report_only
	} `yaml:"csp" json:"csp"`
}

// Validate returns path-accurate errors (platform/config collects them).
func (c Config) Validate() []error {
	switch c.CSP.Mode {
	case "", CSPReportOnly, CSPEnforceCore, CSPEnforce:
		return nil
	}
	return []error{fmt.Errorf("web.csp.mode: %q is not one of report_only, enforce_core, enforce", c.CSP.Mode)}
}

// ServeKind classifies a response for the Observe hook.
type ServeKind string

const (
	KindFile      ServeKind = "file"
	KindShell     ServeKind = "shell"
	KindShell404  ServeKind = "shell404"
	KindNotFound  ServeKind = "notfound"
	KindRedirect  ServeKind = "redirect"
	KindCSPReport ServeKind = "csp_report"
)

// CSPReport is a sanitised violation report. No query strings, no fragments, bounded lengths.
type CSPReport struct {
	Directive    string // normalised to a closed set, "other" otherwise
	Disposition  string // "enforce" | "report"
	BlockedKind  string // "inline" | "eval" | "data" | "blob" | "self" | "host:<host>" | "unknown"
	DocumentPath string
	SourceFile   string // path only
	Line         int
}

// Options configures New. Zero values are safe except Reserved, which is required.
type Options struct {
	BuildID              string // sent as X-SCP-Build; with CheckBuild it must equal routes.json buildId
	CheckBuild           bool
	Reserved             []string // path prefixes that never fall back to the shell, e.g. "/api/", "/badges/" (required, must include "/api/")
	RootRedirect         string   // GET / on the public listener (default "/dashboard")
	AdminRedirect        string   // GET / on the admin listener (default "/status")
	CSP                  CSPMode  // default report_only
	ReportPath           string   // default "/csp-reports"
	ReferrerPolicy       string   // default strict-origin-when-cross-origin
	RequirePrecompressed bool     // New fails when a compressible file >= 1 KiB lacks a .br sibling (release builds)
	OnCSPReport          func(ctx context.Context, r CSPReport)
	Observe              func(kind ServeKind, encoding string)
}

func (o *Options) defaults() error {
	if len(o.Reserved) == 0 {
		return fmt.Errorf("webapp: Options.Reserved is empty (it must at least contain /api/)")
	}
	hasAPI := false
	for _, p := range o.Reserved {
		if !strings.HasPrefix(p, "/") || !strings.HasSuffix(p, "/") {
			return fmt.Errorf("webapp: reserved prefix %q must start and end with /", p)
		}
		if p == "/api/" {
			hasAPI = true
		}
	}
	if !hasAPI {
		return fmt.Errorf("webapp: Options.Reserved must contain /api/")
	}
	if o.RootRedirect == "" {
		o.RootRedirect = "/dashboard"
	}
	if o.AdminRedirect == "" {
		o.AdminRedirect = "/status"
	}
	if o.CSP == "" {
		o.CSP = CSPReportOnly
	}
	if o.ReportPath == "" {
		o.ReportPath = "/csp-reports"
	}
	if o.ReferrerPolicy == "" {
		o.ReferrerPolicy = "strict-origin-when-cross-origin"
	}
	return nil
}
