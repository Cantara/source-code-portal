package docs

import (
	"fmt"
	"strings"
	"time"
)

// Config is the docs section of the root configuration (keys under `docs:`).
type Config struct {
	MaxDocBytes        int           `yaml:"max_doc_bytes"`         // render truncation point, 1 KiB..1 MiB
	MaxDocsPerRepo     int           `yaml:"max_docs_per_repo"`     // resident documents per repository, 1..200
	MaxResidentPerRepo int64         `yaml:"max_resident_per_repo"` // resident bytes per repository
	ResidentBudget     int64         `yaml:"resident_budget"`       // resident bytes in the whole process
	BridgeAsciiDoc     bool          `yaml:"bridge_asciidoc"`       // AsciiDoc through GitHub's rendering
	DefaultRoot        string        `yaml:"default_root"`          // docs.root when the descriptor names none
	TTL                time.Duration `yaml:"ttl"`                   // drift bound of docs.refresh
	IdleBackoffMax     time.Duration `yaml:"idle_backoff_max"`      // cap of the idle back-off
	AsciiDocRevalidate time.Duration `yaml:"asciidoc_revalidate"`   // re-fetch of GitHub-rendered HTML
	JobTimeout         time.Duration `yaml:"job_timeout"`           // per-run deadline of docs.refresh
	MaxFetchPerRun     int           `yaml:"max_fetch_per_run"`     // file fetches per run before a continuation
	RenderTimeout      time.Duration `yaml:"render_timeout"`        // per-document render deadline
	RenderConcurrency  int           `yaml:"render_concurrency"`    // simultaneous renders
	CatalogInfo        bool          `yaml:"catalog_info"`          // read catalog-info.yaml as a metadata fallback
	CodeOwners         bool          `yaml:"codeowners"`            // read CODEOWNERS as an owners fallback
}

const (
	// MaxIndexed is the hard cap of documents listed per repository.
	MaxIndexed = 200
	// TooLargeBytes is the size above which a file is the typed state too_large (GR-29).
	TooLargeBytes = 1 << 20
)

// DefaultConfig returns the shipped defaults.
func DefaultConfig() Config {
	return Config{
		MaxDocBytes: 512000, MaxDocsPerRepo: 50, MaxResidentPerRepo: 2 << 20, ResidentBudget: 192 << 20,
		BridgeAsciiDoc: true, DefaultRoot: "docs", TTL: time.Hour, IdleBackoffMax: 6 * time.Hour,
		AsciiDocRevalidate: 24 * time.Hour, JobTimeout: 3 * time.Minute, MaxFetchPerRun: 25,
		RenderTimeout: 5 * time.Second, RenderConcurrency: 2, CatalogInfo: true, CodeOwners: true,
	}
}

// Validate returns every violation with its path (prefix is "docs").
func (c Config) Validate(prefix string) []error {
	var errs []error
	bad := func(key, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s.%s: %s", prefix, key, fmt.Sprintf(format, a...)))
	}
	if c.MaxDocBytes < 1<<10 || c.MaxDocBytes > TooLargeBytes {
		bad("max_doc_bytes", "must be between 1 KiB and 1 MiB, got %d", c.MaxDocBytes)
	}
	if c.MaxDocsPerRepo < 1 || c.MaxDocsPerRepo > MaxIndexed {
		bad("max_docs_per_repo", "must be between 1 and %d, got %d", MaxIndexed, c.MaxDocsPerRepo)
	}
	if c.MaxResidentPerRepo < 64<<10 || c.MaxResidentPerRepo > 8<<20 {
		bad("max_resident_per_repo", "must be between 64 KiB and 8 MiB (the persisted payload must stay under store.MaxPayload), got %d", c.MaxResidentPerRepo)
	}
	if c.ResidentBudget < c.MaxResidentPerRepo {
		bad("resident_budget", "must be at least max_resident_per_repo (%d), got %d", c.MaxResidentPerRepo, c.ResidentBudget)
	}
	if !validRoot(c.DefaultRoot) {
		bad("default_root", "must be a repository-relative directory without '..' segments, got %q", c.DefaultRoot)
	}
	if c.TTL < 5*time.Minute || c.TTL > 24*time.Hour {
		bad("ttl", "must be between 5m and 24h, got %s", c.TTL)
	}
	if c.IdleBackoffMax < c.TTL || c.IdleBackoffMax > 168*time.Hour {
		bad("idle_backoff_max", "must be between ttl and 168h, got %s", c.IdleBackoffMax)
	}
	if c.AsciiDocRevalidate < time.Hour || c.AsciiDocRevalidate > 168*time.Hour {
		bad("asciidoc_revalidate", "must be between 1h and 168h, got %s", c.AsciiDocRevalidate)
	}
	if c.JobTimeout < 10*time.Second || c.JobTimeout > 30*time.Minute {
		bad("job_timeout", "must be between 10s and 30m, got %s", c.JobTimeout)
	}
	if c.MaxFetchPerRun < 1 || c.MaxFetchPerRun > 200 {
		bad("max_fetch_per_run", "must be between 1 and 200, got %d", c.MaxFetchPerRun)
	}
	if c.RenderTimeout < time.Second || c.RenderTimeout > time.Minute {
		bad("render_timeout", "must be between 1s and 1m, got %s", c.RenderTimeout)
	}
	if c.RenderConcurrency < 1 || c.RenderConcurrency > 16 {
		bad("render_concurrency", "must be between 1 and 16, got %d", c.RenderConcurrency)
	}
	return errs
}

func validRoot(r string) bool {
	if r == "" || len(r) > 200 || strings.HasPrefix(r, "/") || strings.ContainsAny(r, "\x00\\") {
		return false
	}
	for _, seg := range strings.Split(r, "/") {
		if seg == ".." || seg == "" || seg == "." {
			return false
		}
	}
	return true
}
