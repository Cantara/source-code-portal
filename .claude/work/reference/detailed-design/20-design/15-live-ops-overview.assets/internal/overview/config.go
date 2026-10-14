// Package overview is cross-feature read composition and nothing else (ADR-001 exit condition): site info, the list and
// composite repository resources, the llms.txt family, sitemap, robots, negotiated Markdown for human URLs and
// slug-alias redirects. It holds no domain state, runs no job, owns no store and calls no upstream.
package overview

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Config is the union of the two root keys this package owns: `site:` and `llms:`. The zero value is invalid: use DefaultConfig.
type Config struct {
	Site SiteConfig `yaml:"site"`
	LLMs LLMsConfig `yaml:"llms"`
}

type NavEntry struct {
	Label string `yaml:"label"` // 1..60 runes
	Page  string `yaml:"page"`  // a pages slug, or ""
	URL   string `yaml:"url"`   // an https URL, or ""; exactly one of Page and URL is set
}

type RobotsConfig struct {
	ExtraRules []string `yaml:"extra_rules"` // appended after a blank line; the first must be a User-agent line
}

type SiteConfig struct {
	Title           string       `yaml:"title"`             // 1..120 runes, no control characters
	Description     string       `yaml:"description"`       // 0..500 runes
	Nav             []NavEntry   `yaml:"nav"`               // at most 20
	DisplayTimeZone string       `yaml:"display_time_zone"` // IANA name; "UTC"
	DefaultTheme    string       `yaml:"default_theme"`     // system | light | dark
	Sitemap         bool         `yaml:"sitemap"`           // false: GET /sitemap.xml answers 404
	Robots          RobotsConfig `yaml:"robots"`
}

type LLMsConfig struct {
	Enabled           bool          `yaml:"enabled"`             // false: every llms*.txt route answers 404 feature-disabled
	IndexMaxChars     int           `yaml:"index_max_chars"`     // 100000: the ## Repositories section is omitted above this
	FullMaxBytes      int64         `yaml:"full_max_bytes"`      // 2 MiB: cap of the root and group llms-full.txt
	SummaryMaxRunes   int           `yaml:"summary_max_runes"`   // 160
	MaxConcurrentFull int           `yaml:"max_concurrent_full"` // 4: concurrent *-full.txt generations, the rest get 503 queue-full
	MaxAge            time.Duration `yaml:"max_age"`             // 5m: Cache-Control max-age of the text outputs
}

// DefaultConfig is the embedded neutral default (GR-14).
func DefaultConfig() Config {
	return Config{
		Site: SiteConfig{Title: "Source Code Portal", DisplayTimeZone: "UTC", DefaultTheme: "system", Sitemap: true},
		LLMs: LLMsConfig{Enabled: true, IndexMaxChars: 100_000, FullMaxBytes: 2 << 20, SummaryMaxRunes: 160, MaxConcurrentFull: 4, MaxAge: 5 * time.Minute},
	}
}

var (
	pageSlugRE  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	robotsDirRE = regexp.MustCompile(`^(?i:user-agent|allow|disallow|crawl-delay|sitemap): ?[\x21-\x7e][\x20-\x7e]*$`)
)

// Validate collects every violation with a path-accurate key.
func (c Config) Validate() []error {
	var errs []error
	add := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }
	s := c.Site
	if n := utf8.RuneCountInString(s.Title); n < 1 || n > 120 || scm.CleanLine(s.Title, 0) != s.Title {
		add("site.title: 1..120 characters without control characters or surrounding space")
	}
	if utf8.RuneCountInString(s.Description) > 500 || scm.CleanLine(s.Description, 0) != s.Description {
		add("site.description: at most 500 characters without control characters")
	}
	if len(s.Nav) > 20 {
		add("site.nav: %d entries, at most 20", len(s.Nav))
	}
	for i, n := range s.Nav {
		k := fmt.Sprintf("site.nav[%d]", i)
		if l := utf8.RuneCountInString(n.Label); l < 1 || l > 60 || scm.CleanLine(n.Label, 0) != n.Label {
			add("%s.label: 1..60 characters without control characters", k)
		}
		switch {
		case (n.Page == "") == (n.URL == ""):
			add("%s: exactly one of page and url", k)
		case n.Page != "" && (len(n.Page) > 63 || !pageSlugRE.MatchString(n.Page)):
			add("%s.page: %q is not a page slug", k, n.Page)
		case n.URL != "":
			if u, err := url.Parse(n.URL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(n.URL) > 2000 {
				add("%s.url: must be an https URL without credentials, at most 2000 bytes", k)
			}
		}
	}
	if s.DisplayTimeZone == "" || s.DisplayTimeZone == "Local" {
		add("site.display_time_zone: an IANA name is required")
	} else if _, err := time.LoadLocation(s.DisplayTimeZone); err != nil {
		add("site.display_time_zone: %q is not an IANA zone", s.DisplayTimeZone)
	}
	switch s.DefaultTheme {
	case "system", "light", "dark":
	default:
		add("site.default_theme: %q, want system, light or dark", s.DefaultTheme)
	}
	if r := s.Robots.ExtraRules; len(r) > 0 {
		if len(r) > 50 {
			add("site.robots.extra_rules: %d lines, at most 50", len(r))
		}
		for i, line := range r {
			if len(line) > 200 || !robotsDirRE.MatchString(line) {
				add("site.robots.extra_rules[%d]: one line of at most 200 printable ASCII bytes starting with User-agent, Allow, Disallow, Crawl-delay or Sitemap", i)
			}
		}
		if !strings.HasPrefix(strings.ToLower(r[0]), "user-agent:") {
			add("site.robots.extra_rules[0]: the first rule must be a User-agent line (the rules form a new group)")
		}
	}
	l := c.LLMs
	rng := func(k string, v, lo, hi int64) {
		if v < lo || v > hi {
			add("llms.%s: %d outside %d..%d", k, v, lo, hi)
		}
	}
	rng("index_max_chars", int64(l.IndexMaxChars), 1_000, 1_000_000)
	rng("full_max_bytes", l.FullMaxBytes, 64<<10, 32<<20)
	rng("summary_max_runes", int64(l.SummaryMaxRunes), 40, 300)
	rng("max_concurrent_full", int64(l.MaxConcurrentFull), 1, 64)
	if l.MaxAge < 0 || l.MaxAge > 24*time.Hour {
		add("llms.max_age: %s outside 0s..24h", l.MaxAge)
	}
	return errs
}
