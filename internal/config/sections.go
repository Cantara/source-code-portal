package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Store configures the SQLite last-good cache.
type Store struct {
	Path string `mapstructure:"path"`
}

// Sync configures the job engine.
type Sync struct {
	Concurrency int `mapstructure:"concurrency"` // jobs running at once
}

// GitHub lists the sources SCP reads.
type GitHub struct {
	Sources []Source `mapstructure:"sources"`
}

// Source is one GitHub host with one credential: a GitHub App or a token.
type Source struct {
	ID           string   `mapstructure:"id"`
	BaseURL      string   `mapstructure:"base_url"` // REST base with a trailing slash; GHES: https://host/api/v3/
	Orgs         []string `mapstructure:"orgs"`
	App          *App     `mapstructure:"app"`
	TokenFile    string   `mapstructure:"token_file"`
	IncludeForks bool     `mapstructure:"include_forks"`
	// RawIncludeArchived is the configured value; IncludeArchived holds it with its default (true).
	RawIncludeArchived *bool  `mapstructure:"include_archived"`
	IncludeArchived    bool   `mapstructure:"-"`
	Token              Secret `mapstructure:"-"` // read from TokenFile
}

// App is a GitHub App credential.
type App struct {
	ID             int64    `mapstructure:"id"`
	PrivateKeyFile string   `mapstructure:"private_key_file"`
	Installations  []string `mapstructure:"installations"` // account logins to use; empty means all
	PrivateKey     Secret   `mapstructure:"-"`             // read from PrivateKeyFile
}

// DefaultBaseURL is the REST base of github.com.
const DefaultBaseURL = "https://api.github.com/"

// Catalog configures discovery and groups.
type Catalog struct {
	ReconcileInterval time.Duration `mapstructure:"reconcile_interval"`
	Groups            []Group       `mapstructure:"groups"`
}

// Group is a configured group of repositories. Matchers are compiled and checked by the catalog.
type Group struct {
	ID          string   `mapstructure:"id"`
	Name        string   `mapstructure:"name"`
	Description string   `mapstructure:"description"`
	DefaultRepo string   `mapstructure:"default_repo"`
	Sources     []string `mapstructure:"sources"` // source ids; empty means all
	Match       []Match  `mapstructure:"match"`
}

// Match is one matcher; exactly one field is set.
type Match struct {
	Exact    string `mapstructure:"exact"`
	Prefix   string `mapstructure:"prefix"`
	Glob     string `mapstructure:"glob"`
	Regexp   string `mapstructure:"regexp"`
	Topic    string `mapstructure:"topic"`
	CatchAll bool   `mapstructure:"catch_all"`
}

// Kind returns the matcher kind and its value; ok is false unless exactly one field is set.
func (m Match) Kind() (kind, value string, ok bool) {
	n := 0
	for _, f := range []struct{ kind, value string }{
		{"exact", m.Exact}, {"prefix", m.Prefix}, {"glob", m.Glob}, {"regexp", m.Regexp}, {"topic", m.Topic},
	} {
		if f.value != "" {
			kind, value = f.kind, f.value
			n++
		}
	}
	if m.CatchAll {
		kind, value = "catch_all", ""
		n++
	}
	return kind, value, n == 1
}

var (
	idPattern    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
)

func (c *Config) normalise() {
	for i := range c.GitHub.Sources {
		s := &c.GitHub.Sources[i]
		if s.BaseURL == "" {
			s.BaseURL = DefaultBaseURL
		}
		s.IncludeArchived = s.RawIncludeArchived == nil || *s.RawIncludeArchived
	}
}

func (g GitHub) validate() []string {
	var p []string
	if len(g.Sources) == 0 {
		return []string{"github.sources: at least one source is required"}
	}
	ids := map[string]bool{}
	owners := map[string]string{}
	for i, s := range g.Sources {
		at := fmt.Sprintf("github.sources[%d]", i)
		if !idPattern.MatchString(s.ID) || len(s.ID) > 63 {
			p = append(p, fmt.Sprintf("%s.id: %q must match [a-z0-9-]+", at, s.ID))
		}
		if ids[s.ID] {
			p = append(p, fmt.Sprintf("%s.id: duplicate %q", at, s.ID))
		}
		ids[s.ID] = true
		if u, err := url.Parse(s.BaseURL); err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || !strings.HasSuffix(s.BaseURL, "/") {
			p = append(p, fmt.Sprintf("%s.base_url: %q must be an absolute http(s) URL ending in /", at, s.BaseURL))
		}
		if len(s.Orgs) == 0 {
			p = append(p, at+".orgs: at least one organisation is required")
		}
		for _, o := range s.Orgs {
			if !ownerPattern.MatchString(o) {
				p = append(p, fmt.Sprintf("%s.orgs: %q is not a GitHub account name", at, o))
			}
			if prev, dup := owners[strings.ToLower(o)]; dup {
				p = append(p, fmt.Sprintf("%s.orgs: %q is also in source %q; an organisation belongs to one source", at, o, prev))
			}
			owners[strings.ToLower(o)] = s.ID
		}
		switch {
		case s.App != nil && s.TokenFile != "":
			p = append(p, at+": set either app or token_file, not both")
		case s.App == nil && s.TokenFile == "":
			p = append(p, at+": set app or token_file")
		case s.App != nil:
			if s.App.ID <= 0 {
				p = append(p, at+".app.id: required")
			}
			if s.App.PrivateKeyFile == "" {
				p = append(p, at+".app.private_key_file: required")
			}
		}
	}
	return p
}

func (c Catalog) validate(g GitHub) []string {
	var p []string
	if c.ReconcileInterval < time.Minute || c.ReconcileInterval > 24*time.Hour {
		p = append(p, "catalog.reconcile_interval: must be 1m..24h")
	}
	if len(c.Groups) > 500 {
		p = append(p, "catalog.groups: at most 500 groups")
	}
	sources := map[string]bool{}
	for _, s := range g.Sources {
		sources[s.ID] = true
	}
	ids := map[string]bool{}
	for i, gr := range c.Groups {
		at := fmt.Sprintf("catalog.groups[%d]", i)
		if !idPattern.MatchString(gr.ID) || len(gr.ID) > 63 {
			p = append(p, fmt.Sprintf("%s.id: %q must match [a-z0-9-]+, at most 63 characters", at, gr.ID))
		}
		if ids[gr.ID] {
			p = append(p, fmt.Sprintf("%s.id: duplicate %q", at, gr.ID))
		}
		ids[gr.ID] = true
		if gr.Name == "" || len(gr.Name) > 120 {
			p = append(p, at+".name: required, at most 120 characters")
		}
		if len(gr.Description) > 500 {
			p = append(p, at+".description: at most 500 characters")
		}
		if gr.DefaultRepo == "" {
			p = append(p, at+".default_repo: required")
		}
		for _, s := range gr.Sources {
			if !sources[s] {
				p = append(p, fmt.Sprintf("%s.sources: unknown source %q", at, s))
			}
		}
		if len(gr.Match) == 0 || len(gr.Match) > 64 {
			p = append(p, at+".match: 1..64 matchers")
		}
		for j, m := range gr.Match {
			if _, _, ok := m.Kind(); !ok {
				p = append(p, fmt.Sprintf("%s.match[%d]: exactly one of exact, prefix, glob, regexp, topic, catch_all: true", at, j))
			}
		}
	}
	return p
}

// readSecrets reads every secret file named in the configuration.
func (c *Config) readSecrets() []string {
	var p []string
	read := func(key, path string, dst *Secret) {
		b, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the file
		switch {
		case err != nil:
			p = append(p, fmt.Sprintf("%s: %v", key, err))
		case len(strings.TrimSpace(string(b))) == 0:
			p = append(p, fmt.Sprintf("%s: %s is empty", key, path))
		default:
			*dst = NewSecret([]byte(strings.TrimSpace(string(b))))
		}
	}
	for i := range c.GitHub.Sources {
		s := &c.GitHub.Sources[i]
		at := fmt.Sprintf("github.sources[%d]", i)
		if s.TokenFile != "" {
			read(at+".token_file", s.TokenFile, &s.Token)
		}
		if s.App != nil && s.App.PrivateKeyFile != "" {
			read(at+".app.private_key_file", s.App.PrivateKeyFile, &s.App.PrivateKey)
		}
	}
	slices.Sort(p)
	return p
}
