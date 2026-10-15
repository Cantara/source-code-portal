package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
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
	Orgs         []string `mapstructure:"orgs"`     // organisations and user accounts
	App          *App     `mapstructure:"app"`
	TokenFile    string   `mapstructure:"token_file"`
	IncludeForks bool     `mapstructure:"include_forks"`
	// IncludePrivate admits private and internal repositories; access.private_groups says who sees them.
	IncludePrivate bool `mapstructure:"include_private"`
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

// Webhook configures the GitHub webhook receiver; it is served when SecretFile is set. PreviousSecretFile is accepted too while the
// secret is rotated.
type Webhook struct {
	SecretFile         string `mapstructure:"secret_file"`
	PreviousSecretFile string `mapstructure:"previous_secret_file"`
	Secret             Secret `mapstructure:"-"`
	PreviousSecret     Secret `mapstructure:"-"`
}

// Access names the headers a trusted authenticating proxy sets and the groups whose members see private repositories.
type Access struct {
	UserHeader    string   `mapstructure:"user_header"`
	GroupsHeader  string   `mapstructure:"groups_header"`  // comma-separated group names
	PrivateGroups []string `mapstructure:"private_groups"` // "*" admits every authenticated user
	RequireAuth   bool     `mapstructure:"require_auth"`   // no public view: an anonymous request is refused
}

var headerPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Cluster joins the instance to other instances of the same portal through Hazelcast; empty Addresses: the instance is alone.
type Cluster struct {
	Addresses []string `mapstructure:"addresses"` // Hazelcast members, host:port
	Name      string   `mapstructure:"name"`      // the Hazelcast cluster name
	Namespace string   `mapstructure:"namespace"` // prefix of the Hazelcast objects
}

// Web runs the web UI: the Next.js server in Dir (web/bundle of the build), started with Node; empty Dir: no web UI, the pages
// answer 503.
type Web struct {
	Dir  string `mapstructure:"dir"`  // holds server.js
	Node string `mapstructure:"node"` // the Node.js executable, a path or a name on PATH
}

func (w Web) validate() []string {
	var p []string
	if w.Dir != "" {
		if info, err := os.Stat(filepath.Join(w.Dir, "server.js")); err != nil || !info.Mode().IsRegular() {
			p = append(p, fmt.Sprintf("web.dir: %q holds no server.js", w.Dir))
		}
	}
	if strings.TrimSpace(w.Node) == "" {
		p = append(p, "web.node: required")
	}
	return p
}

var namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func (c Cluster) validate() []string {
	var p []string
	for _, a := range c.Addresses {
		if host, port, err := net.SplitHostPort(a); err != nil || host == "" || port == "" {
			p = append(p, fmt.Sprintf("cluster.addresses: %q is not host:port", a))
		}
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 256 {
		p = append(p, "cluster.name: required")
	}
	if !namespacePattern.MatchString(c.Namespace) {
		p = append(p, fmt.Sprintf("cluster.namespace: %q must be 1..32 of a-z, 0-9 and -", c.Namespace))
	}
	return p
}

// validate checks the access lists; identified reports a way to know a viewer (a trusted proxy, sign-in or tokens).
func (a Access) validate(g GitHub, identified bool) []string {
	var p []string
	for key, h := range map[string]string{"access.user_header": a.UserHeader, "access.groups_header": a.GroupsHeader} {
		if !headerPattern.MatchString(h) {
			p = append(p, fmt.Sprintf("%s: %q is not a header name", key, h))
		}
	}
	if strings.EqualFold(a.UserHeader, a.GroupsHeader) {
		p = append(p, "access.groups_header: must differ from access.user_header")
	}
	for _, gr := range a.PrivateGroups {
		if strings.TrimSpace(gr) == "" || strings.Contains(gr, ",") || len(gr) > 256 {
			p = append(p, fmt.Sprintf("access.private_groups: %q is not a group name", gr))
		}
	}
	private := slices.ContainsFunc(g.Sources, func(s Source) bool { return s.IncludePrivate })
	switch {
	case private && len(a.PrivateGroups) == 0:
		p = append(p, "access.private_groups: required when a source sets include_private")
	case private && !identified:
		p = append(p, "server.trusted_proxies: required when a source sets include_private (the authenticating proxy), unless auth signs viewers in")
	}
	if a.RequireAuth && !identified {
		p = append(p, "access.require_auth: needs a way to know the viewer (server.trusted_proxies, auth sign-in or auth.tokens)")
	}
	return p
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
	c.Server.PublicURL = strings.TrimRight(c.Server.PublicURL, "/")
	for _, cidr := range c.Server.RawTrustedProxies {
		if pfx, err := netip.ParsePrefix(cidr); err == nil {
			c.Server.TrustedProxies = append(c.Server.TrustedProxies, pfx.Masked())
		}
	}
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
			p = append(p, at+".orgs: at least one organisation or user account is required")
		}
		for _, o := range s.Orgs {
			if !ownerPattern.MatchString(o) {
				p = append(p, fmt.Sprintf("%s.orgs: %q is not a GitHub account name", at, o))
			}
			if prev, dup := owners[strings.ToLower(o)]; dup {
				p = append(p, fmt.Sprintf("%s.orgs: %q is also in source %q; an account belongs to one source", at, o, prev))
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
	if c.Webhook.SecretFile != "" {
		read("webhook.secret_file", c.Webhook.SecretFile, &c.Webhook.Secret)
	}
	if c.Webhook.PreviousSecretFile != "" {
		read("webhook.previous_secret_file", c.Webhook.PreviousSecretFile, &c.Webhook.PreviousSecret)
	}
	p = append(p, c.Auth.readSecrets(read)...)
	c.LLM.readSecrets(read)
	slices.Sort(p)
	return p
}
