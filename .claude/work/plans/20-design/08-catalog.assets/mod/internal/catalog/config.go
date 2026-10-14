package catalog

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Ungrouped selects what happens to a repository that no group selects.
type Ungrouped string

const (
	UngroupedTrack  Ungrouped = "track"  // listed in /repos with groups == [] (default)
	UngroupedIgnore Ungrouped = "ignore" // not tracked at all; costs no upstream budget
)

// Config is the `catalog:` block of the root config. Keys are documented in section 6 of the design.
type Config struct {
	Ungrouped          Ungrouped         `yaml:"ungrouped"`
	SlugPrefixBySource map[string]string `yaml:"slug_prefix_by_source"` // key "source" or "source/owner"
	Heads              HeadsConfig       `yaml:"heads"`
	Groups             []GroupConfig     `yaml:"groups"`
}

// HeadsConfig tunes the head/stats sweep.
type HeadsConfig struct {
	BatchSize        int           `yaml:"batch_size"`        // repositories per ProbeHeads call, 1..100
	Interval         time.Duration `yaml:"interval"`          // TTL of catalog.heads
	ArchivedInterval time.Duration `yaml:"archived_interval"` // TTL of the per-repository catalog.stats job (archived repositories and event accelerator)
}

// GroupConfig is one group ("system"). Declaration order is display order.
type GroupConfig struct {
	ID           scm.GroupID       `yaml:"id"`
	DisplayName  string            `yaml:"display_name"`
	Description  string            `yaml:"description"`
	DefaultRepo  string            `yaml:"default_repo"` // a slug (case-insensitive) of a member
	Sources      []scm.SourceID    `yaml:"sources"`      // empty = every source
	Match        []Matcher         `yaml:"match"`
	Ref          string            `yaml:"ref"`     // tracked ref override; "" = the repository's default branch
	Aliases      []string          `yaml:"aliases"` // earlier ids of this group (their URLs redirect to the current id)
	Integrations GroupIntegrations `yaml:"integrations"`
}

// GroupIntegrations are per-group defaults handed to the integrations feature through scm.GroupMeta.Integrations.
type GroupIntegrations struct {
	Jenkins *JenkinsDefaults `yaml:"jenkins"`
	Snyk    *SnykDefaults    `yaml:"snyk"`
}
type JenkinsDefaults struct {
	JobPrefix string `yaml:"job_prefix"`
	Instance  string `yaml:"instance"` // name of a configured Jenkins instance (existence is validated by integrations.ValidateGroupDefaults)
}
type SnykDefaults struct {
	ProjectPrefix string `yaml:"project_prefix"`
}

// Meta returns the group's scm.GroupMeta.
func (g GroupConfig) Meta() scm.GroupMeta {
	m := scm.GroupMeta{ID: g.ID, DisplayName: g.DisplayName, Description: g.Description}
	if j := g.Integrations.Jenkins; j != nil && (j.JobPrefix != "" || j.Instance != "") {
		jm := map[string]string{}
		if j.JobPrefix != "" {
			jm["job_prefix"] = j.JobPrefix
		}
		if j.Instance != "" {
			jm["instance"] = j.Instance
		}
		m.Integrations = map[string]map[string]string{"jenkins": jm}
	}
	if s := g.Integrations.Snyk; s != nil && s.ProjectPrefix != "" {
		if m.Integrations == nil {
			m.Integrations = map[string]map[string]string{}
		}
		m.Integrations["snyk"] = map[string]string{"project_prefix": s.ProjectPrefix}
	}
	return m
}

// DefaultConfig is the embedded default (cmd/scp decodes the user's file on top of it).
func DefaultConfig() Config {
	return Config{
		Ungrouped: UngroupedTrack,
		Heads:     HeadsConfig{BatchSize: 50, Interval: 5 * time.Minute, ArchivedInterval: 24 * time.Hour},
	}
}

// Policy limits (constants, not config).
const (
	MaxGroups       = 500
	MaxAliases      = 16
	MaxTextRunes    = 500
	MaxNameRunes    = 120
	MaxRefBytes     = 200
	MaxPrefixBytes  = 100
	MaxSlugAliasLen = 8
)

// ValidateContext carries what only cmd/scp knows.
type ValidateContext struct {
	Sources []scm.SourceID // configured sources, in order; empty disables source-reference checks (offline lint of a fragment)
}

func (c ValidateContext) knows(s scm.SourceID) bool {
	if len(c.Sources) == 0 {
		return true
	}
	for _, k := range c.Sources {
		if k == s {
			return true
		}
	}
	return false
}

func errf(code, path, format string, a ...any) Finding {
	return Finding{Code: code, Severity: SevError, Path: path, Message: fmt.Sprintf(format, a...)}
}
func warnf(code, path, format string, a ...any) Finding {
	return Finding{Code: code, Severity: SevWarning, Path: path, Message: fmt.Sprintf(format, a...)}
}

func plainText(s string, maxRunes int) string {
	if n := len([]rune(s)); n > maxRunes {
		return fmt.Sprintf("longer than %d characters", maxRunes)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			return "contains a control character"
		}
	}
	return ""
}

// Validate runs every offline rule (section 4.9: C002-C090, W101, W102, W103, W107, W108). It never touches the network and never panics on
// hostile input. The result is sorted; the process refuses to start when HasErrors.
func (c Config) Validate(vc ValidateContext) []Finding {
	var out []Finding
	if c.Ungrouped != UngroupedTrack && c.Ungrouped != UngroupedIgnore {
		out = append(out, errf("C002", "/catalog/ungrouped", "must be %q or %q, got %q", UngroupedTrack, UngroupedIgnore, c.Ungrouped))
	}
	h := c.Heads
	if h.BatchSize < 1 || h.BatchSize > 100 {
		out = append(out, errf("C003", "/catalog/heads/batch_size", "must be 1..100, got %d", h.BatchSize))
	}
	if h.Interval < time.Minute || h.Interval > time.Hour {
		out = append(out, errf("C003", "/catalog/heads/interval", "must be between 1m and 1h, got %s", h.Interval))
	}
	if h.ArchivedInterval < time.Hour || h.ArchivedInterval > 7*24*time.Hour {
		out = append(out, errf("C003", "/catalog/heads/archived_interval", "must be between 1h and 168h, got %s", h.ArchivedInterval))
	}
	for k, p := range c.SlugPrefixBySource {
		path := "/catalog/slug_prefix_by_source/" + k
		src, _, _ := strings.Cut(k, "/")
		if !vc.knows(scm.SourceID(src)) {
			out = append(out, errf("C060", path, "unknown source %q", src))
		}
		if !prefixRe.MatchString(p) {
			out = append(out, errf("C061", path, "a slug prefix is at most 32 characters of letters, digits, '.', '_' and '-'"))
		}
	}
	if len(c.Groups) > MaxGroups {
		out = append(out, errf("C090", "/catalog/groups", "at most %d groups, got %d", MaxGroups, len(c.Groups)))
	}
	ids := map[string]string{} // folded id or alias -> path that defined it
	claim := func(v, path string) {
		k := strings.ToLower(v)
		if prev, dup := ids[k]; dup {
			out = append(out, errf("C011", path, "%q is already used as a group id or alias at %s", v, prev))
			return
		}
		ids[k] = path
	}
	for i, g := range c.Groups {
		gp := fmt.Sprintf("/catalog/groups/%d", i)
		out = append(out, g.validate(gp, vc, claim)...)
	}
	out = append(out, c.lintStatic()...)
	SortFindings(out)
	return out
}

func (g GroupConfig) validate(gp string, vc ValidateContext, claim func(v, path string)) []Finding {
	var out []Finding
	if !ValidGroupID(g.ID) {
		out = append(out, errf("C010", gp+"/id", "must match ^[a-z0-9]+(-[a-z0-9]+)*$ and be at most 63 bytes, got %q", g.ID))
	} else {
		claim(string(g.ID), gp+"/id")
	}
	if g.DisplayName == "" {
		out = append(out, errf("C012", gp+"/display_name", "required"))
	} else if m := plainText(g.DisplayName, MaxNameRunes); m != "" {
		out = append(out, errf("C012", gp+"/display_name", "%s", m))
	}
	if m := plainText(g.Description, MaxTextRunes); m != "" {
		out = append(out, errf("C013", gp+"/description", "%s", m))
	}
	for j, a := range g.Aliases {
		ap := fmt.Sprintf("%s/aliases/%d", gp, j)
		switch {
		case a == "" || len(a) > 80 || plainText(a, 80) != "":
			out = append(out, errf("C014", ap, "an alias is 1-80 characters without control characters"))
		case j >= MaxAliases:
			out = append(out, errf("C014", ap, "at most %d aliases", MaxAliases))
		default:
			claim(a, ap)
		}
	}
	for j, s := range g.Sources {
		if !vc.knows(s) {
			out = append(out, errf("C070", fmt.Sprintf("%s/sources/%d", gp, j), "unknown source %q", s))
		}
	}
	if len(g.Match) == 0 {
		out = append(out, errf("C020", gp+"/match", "a group needs at least one matcher"))
	}
	if len(g.Match) > MaxMatchersPerGroup {
		out = append(out, errf("C020", gp+"/match", "at most %d matchers, got %d", MaxMatchersPerGroup, len(g.Match)))
	}
	for j, m := range g.Match {
		mp := fmt.Sprintf("%s/match/%d", gp, j)
		cm, err := m.Compile()
		if err != nil {
			me := err.(*MatcherError)
			out = append(out, errf("C022", mp+"/"+string(m.Kind), "%s", me.Msg))
			continue
		}
		if cm.MatchesEmpty() {
			out = append(out, warnf("W108", mp+"/"+string(m.Kind), "matches the empty name, i.e. every repository; write catch_all: true instead"))
		}
	}
	if g.DefaultRepo == "" {
		out = append(out, errf("C030", gp+"/default_repo", "required: the repository that represents the group on the dashboard"))
	} else if len(g.DefaultRepo) > 100 || g.DefaultRepo == "." || g.DefaultRepo == ".." || !nameRe.MatchString(g.DefaultRepo) {
		out = append(out, errf("C032", gp+"/default_repo", "must be a GitHub repository name (letters, digits, '.', '_', '-'; at most 100 bytes), got %q", g.DefaultRepo))
	} else if f, ok := g.defaultCheck(gp); ok {
		out = append(out, f)
	}
	if g.Ref != "" {
		if !validRef(g.Ref) {
			out = append(out, errf("C040", gp+"/ref", "not a valid branch name: %q", g.Ref))
		} else {
			out = append(out, warnf("W103", gp+"/ref", "a ref override is not covered by the head sweep (it probes each repository's default branch); freshness relies on webhooks and TTLs"))
		}
	}
	if j := g.Integrations.Jenkins; j != nil && (len(j.JobPrefix) > MaxPrefixBytes || plainText(j.JobPrefix, MaxPrefixBytes) != "") {
		out = append(out, errf("C050", gp+"/integrations/jenkins/job_prefix", "at most %d characters, no control characters", MaxPrefixBytes))
	}
	if j := g.Integrations.Jenkins; j != nil && j.Instance != "" && !instanceRe.MatchString(j.Instance) {
		out = append(out, errf("C051", gp+"/integrations/jenkins/instance", "an instance name is 1-63 characters of letters, digits, '.', '_' and '-'"))
	}
	if s := g.Integrations.Snyk; s != nil && (len(s.ProjectPrefix) > MaxPrefixBytes || plainText(s.ProjectPrefix, MaxPrefixBytes) != "") {
		out = append(out, errf("C050", gp+"/integrations/snyk/project_prefix", "at most %d characters, no control characters", MaxPrefixBytes))
	}
	return out
}

// defaultCheck is rule C031: the default repository must be selected by one of the group's own matchers. It is decidable
// offline when no topic matcher is present (names are all it needs); with a topic matcher it can only be informational.
func (g GroupConfig) defaultCheck(gp string) (Finding, bool) {
	hasTopic := false
	for _, m := range g.Match {
		cm, err := m.Compile()
		if err != nil {
			return Finding{}, false // C022 already reported
		}
		if cm.Kind() == KindTopic {
			hasTopic = true
			continue
		}
		if cm.Match(g.DefaultRepo, nil) {
			return Finding{}, false
		}
	}
	if hasTopic {
		return Finding{Code: "C031", Severity: SevInfo, Path: gp + "/default_repo", Message: fmt.Sprintf("%q is not selected by a name matcher; it may be selected by a topic matcher (not decidable offline)", g.DefaultRepo)}, true
	}
	return errf("C031", gp+"/default_repo", "%q is not selected by any matcher of group %q, so it can never be a member", g.DefaultRepo, g.ID), true
}

// validRef is the practical subset of git-check-ref-format for a branch name.
func validRef(s string) bool {
	if s == "" || len(s) > MaxRefBytes || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") ||
		strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "@{") || s == "@" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r == 0x7f || strings.ContainsRune(`~^:?*[\`, r) {
			return false
		}
	}
	return true
}

// lintStatic holds the cross-group warnings that need no repository list (W101, W102, W107).
func (c Config) lintStatic() []Finding {
	var out []Finding
	catchAll := 0
	names := map[string]int{}
	sigs := map[string]int{}
	for i, g := range c.Groups {
		for _, m := range g.Match {
			if m.Kind == KindCatchAll {
				catchAll++
				break
			}
		}
		if g.DisplayName != "" {
			k := strings.ToLower(g.DisplayName)
			if j, ok := names[k]; ok {
				out = append(out, warnf("W107", fmt.Sprintf("/catalog/groups/%d/display_name", i), "same display name as group %d", j))
			} else {
				names[k] = i
			}
		}
		var parts []string
		for _, m := range g.Match {
			parts = append(parts, string(m.Kind)+"="+strings.ToLower(m.Value))
		}
		sortStrings(parts)
		sig := strings.Join(parts, "|") + "@" + joinSources(g.Sources)
		if len(parts) > 0 {
			if j, ok := sigs[sig]; ok {
				out = append(out, warnf("W101", fmt.Sprintf("/catalog/groups/%d/match", i), "selects exactly the same repositories as group %d", j))
			} else {
				sigs[sig] = i
			}
		}
	}
	if catchAll > 1 {
		out = append(out, warnf("W102", "/catalog/groups", "%d groups use catch_all; every repository is then in every one of them", catchAll))
	}
	return out
}
