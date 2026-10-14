package integrations

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// JobPath is a validated Jenkins job path: "/"-separated segments, each a plain job name.
type JobPath []string

const (
	maxJobPathLen = 300
	maxSegmentLen = 100
)

// ParseJobPath validates a descriptor or prefix-derived job path. Rejected: empty input, leading or trailing "/",
// empty segments, "." and "..", leading or trailing space in a segment, and any character outside [A-Za-z0-9._ -].
// "%" is rejected so a path can never smuggle an encoded separator (multibranch branch names are encoded by the
// provider from the repository's ref, not taken from the path).
func ParseJobPath(s string) (JobPath, error) {
	if s == "" || len(s) > maxJobPathLen {
		return nil, fmt.Errorf("job path must be 1..%d bytes", maxJobPathLen)
	}
	segs := strings.Split(s, "/")
	for _, g := range segs {
		switch {
		case g == "" || g == "." || g == "..":
			return nil, fmt.Errorf("job path %q has an empty, . or .. segment", s)
		case len(g) > maxSegmentLen || g != strings.TrimSpace(g):
			return nil, fmt.Errorf("job path %q has an over-long or space-padded segment", s)
		}
		for _, r := range g {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == ' ') {
				return nil, fmt.Errorf("job path %q contains %q", s, r)
			}
		}
	}
	return JobPath(segs), nil
}

func (p JobPath) String() string { return strings.Join(p, "/") }

// URLPath renders "/job/a/job/b" with every segment path-escaped.
func (p JobPath) URLPath() string {
	var sb strings.Builder
	for _, s := range p {
		sb.WriteString("/job/")
		sb.WriteString(url.PathEscape(s))
	}
	return sb.String()
}

// Overrides supplies the per-repository descriptor integration block: provider -> key -> value
// (jenkins: instance, job; snyk: project). It is bridged in cmd/scp over docs' descriptor view (docs is a feature and
// cannot be imported). Values are repository-authored and therefore untrusted: ResolveTargets re-validates them.
type Overrides interface {
	IntegrationOverrides(s *readmodel.Snapshot, slug scm.Slug) map[string]map[string]string
}

// InvalidTarget records why a repository got no target although it asked for one (shown in /status, logged, never
// echoed into API output).
type InvalidTarget struct {
	Slug   scm.Slug
	Reason string
}

// groupDefault returns the value of key for provider from the first group, in configuration order, that defines it.
func groupDefault(m scm.Membership, slug scm.Slug, provider, key string) (string, bool) {
	in := m.GroupsOf(slug)
	if len(in) == 0 {
		return "", false
	}
	for _, g := range m.Groups() { // configuration order, never map or membership order (GR-08)
		if !slices.Contains(in, g.ID) {
			continue
		}
		if v, ok := g.Integrations[provider][key]; ok {
			return v, true
		}
	}
	return "", false
}

// ResolveJenkins computes the Jenkins targets of a snapshot. Deterministic: sorted by slug; independent of map order.
func ResolveJenkins(cfg JenkinsConfig, snap *readmodel.Snapshot, repos readmodel.Slot[freshness.Value[scm.Repository]],
	mem scm.Membership, ov Overrides) ([]Target, []InvalidTarget) {
	instances := map[string]JenkinsInstance{}
	def := ""
	for _, in := range cfg.Instances {
		instances[in.Name] = in
		if in.Default || len(cfg.Instances) == 1 {
			def = in.Name
		}
	}
	ids := snap.Repos()
	slices.Sort(ids)
	var out []Target
	var bad []InvalidTarget
	for _, id := range ids {
		rv, ok := readmodel.Get(snap, id, repos)
		if !ok || rv.V.Archived {
			continue
		}
		repo := rv.V
		var o map[string]string
		if ov != nil {
			o = ov.IntegrationOverrides(snap, repo.Slug)["jenkins"]
		}
		instName := firstNonEmpty(o["instance"], mustGD(mem, repo.Slug, "instance"), def)
		inst, ok := instances[instName]
		if !ok {
			if instName != "" {
				bad = append(bad, InvalidTarget{repo.Slug, "unknown jenkins instance"})
			}
			continue
		}
		var path string
		switch {
		case o["job"] != "":
			path = o["job"]
		default:
			prefix, declared := groupDefault(mem, repo.Slug, "jenkins", "job_prefix")
			switch {
			case declared:
				path = prefix + repo.Ref.Name
			case inst.Discover == "all":
				path = inst.JobPrefix + repo.Ref.Name
			default:
				continue // declared mode and nothing declared: no blind probing
			}
		}
		jp, err := ParseJobPath(path)
		if err != nil {
			bad = append(bad, InvalidTarget{repo.Slug, "invalid jenkins job path"})
			continue
		}
		out = append(out, Target{Repo: repo.Ref, Instance: inst.Name, Job: jp.String(), Branch: repo.DefaultBranch})
	}
	return out, bad
}

// ResolveSnyk: every non-archived repository is a candidate (the listing is one bulk call, so "blind" matching costs nothing).
func ResolveSnyk(snap *readmodel.Snapshot, repos readmodel.Slot[freshness.Value[scm.Repository]], mem scm.Membership, ov Overrides) ([]Target, []InvalidTarget) {
	ids := snap.Repos()
	slices.Sort(ids)
	var out []Target
	var bad []InvalidTarget
	for _, id := range ids {
		rv, ok := readmodel.Get(snap, id, repos)
		if !ok || rv.V.Archived {
			continue
		}
		repo := rv.V
		var o map[string]string
		if ov != nil {
			o = ov.IntegrationOverrides(snap, repo.Slug)["snyk"]
		}
		name := repo.Ref.Name
		if p := o["project"]; p != "" {
			if !validSnykName(p) {
				bad = append(bad, InvalidTarget{repo.Slug, "invalid snyk project"})
				continue
			}
			name = p
		} else if prefix, ok := groupDefault(mem, repo.Slug, "snyk", "project_prefix"); ok {
			if prefix != "" && !validSnykName(prefix) {
				bad = append(bad, InvalidTarget{repo.Slug, "invalid snyk project_prefix"})
				continue
			}
			name = prefix + repo.Ref.Name
		}
		out = append(out, Target{Repo: repo.Ref, Project: repo.Ref.Owner + "/" + name})
	}
	return out, bad
}

func validSnykName(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func mustGD(m scm.Membership, slug scm.Slug, key string) string {
	v, _ := groupDefault(m, slug, "jenkins", key)
	return v
}
