package catalog

import (
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// VisibilityPolicy decides which repositories of a source may enter the catalogue (ADR-023).
type VisibilityPolicy string

const (
	VisibilityPublicOnly        VisibilityPolicy = "public_only"        // default
	VisibilityInstallationScope VisibilityPolicy = "installation_scope" // everything the credential can see; needs access control
)

// HostClass is what catalog needs to know about the source's host (filled by cmd/scp from github.SourceConfig).
type HostClass uint8

const (
	HostGitHubCom HostClass = iota
	HostGHES
	HostGHECDataResidency
)

// DiscoveryPolicy is the catalog-owned part of `github.sources[].discovery` (cmd/scp inlines it next to github's own
// `mode`/`org` keys, section 6.2). Defaults come from DefaultDiscoveryPolicy().
type DiscoveryPolicy struct {
	Visibility            VisibilityPolicy `yaml:"visibility"`
	IncludeArchived       bool             `yaml:"include_archived"`
	IncludeForks          bool             `yaml:"include_forks"`
	ReconcileEvery        time.Duration    `yaml:"reconcile_every"`
	MaxRemovalRatio       float64          `yaml:"max_removal_ratio"`
	RemovalConfirmations  int              `yaml:"removal_confirmations"`
	MassRemovalHold       time.Duration    `yaml:"mass_removal_hold"`
	AccessAcknowledgement string           `yaml:"access_acknowledgement"`
}

func DefaultDiscoveryPolicy() DiscoveryPolicy {
	return DiscoveryPolicy{
		Visibility:           VisibilityPublicOnly,
		IncludeArchived:      true,
		IncludeForks:         false,
		ReconcileEvery:       30 * time.Minute,
		MaxRemovalRatio:      0.3,
		RemovalConfirmations: 2,
		MassRemovalHold:      6 * time.Hour,
	}
}

// PolicyContext is what only cmd/scp knows about the surroundings of a source.
type PolicyContext struct {
	Host          HostClass
	AccessEnabled bool // access.mode != public (trusted-proxy identity is configured)
}

// Validate implements the startup refusals of ADR-023 and 10-architecture section 16 for one source (rules C080-C089).
func (p DiscoveryPolicy) Validate(path string, pc PolicyContext) []Finding {
	var out []Finding
	switch p.Visibility {
	case VisibilityPublicOnly:
		if pc.Host == HostGHECDataResidency {
			out = append(out, errf("C081", path+"/visibility", "GitHub Enterprise Cloud with data residency has no public repositories; public_only would always be empty"))
		}
	case VisibilityInstallationScope:
		if !pc.AccessEnabled {
			out = append(out, errf("C080", path+"/visibility", "installation_scope publishes private repositories and requires access.mode: trusted-proxy"))
		}
		if pc.Host != HostGitHubCom {
			switch p.AccessAcknowledgement {
			case "network_restricted", "oidc_proxy", "trusted_proxy_headers":
			default:
				out = append(out, errf("C082", path+"/access_acknowledgement", "GHES and GHEC data residency need access_acknowledgement: network_restricted|oidc_proxy|trusted_proxy_headers with installation_scope"))
			}
		}
	default:
		out = append(out, errf("C080", path+"/visibility", "must be public_only or installation_scope, got %q", p.Visibility))
	}
	if p.ReconcileEvery < time.Minute || p.ReconcileEvery > 24*time.Hour {
		out = append(out, errf("C083", path+"/reconcile_every", "must be between 1m and 24h, got %s", p.ReconcileEvery))
	}
	if !(p.MaxRemovalRatio > 0 && p.MaxRemovalRatio <= 1) {
		out = append(out, errf("C084", path+"/max_removal_ratio", "must be in (0, 1], got %v", p.MaxRemovalRatio))
	}
	if p.RemovalConfirmations < 1 || p.RemovalConfirmations > 10 {
		out = append(out, errf("C085", path+"/removal_confirmations", "must be 1..10, got %d", p.RemovalConfirmations))
	}
	if p.MassRemovalHold < 0 || p.MassRemovalHold > 7*24*time.Hour {
		out = append(out, errf("C086", path+"/mass_removal_hold", "must be between 0 and 168h, got %s", p.MassRemovalHold))
	}
	return out
}

// Exclusion is why a listed repository is not tracked. ExcludedNone means it is eligible.
type Exclusion uint8

const (
	ExcludedNone Exclusion = iota
	ExcludedInvalid
	ExcludedVisibility
	ExcludedArchived
	ExcludedFork
	ExcludedUngrouped
)

func (e Exclusion) String() string {
	return [...]string{"", "invalid", "visibility", "archived", "fork", "ungrouped"}[e]
}

// Admit applies the policy to one listed repository. Order matters: visibility is checked first and is the only exclusion
// that is applied without confirmation or mass-removal hold (a repository that became private must disappear at once).
// Fail closed: only an explicit VisPublic passes public_only; any other value, including a value this code does not know, is excluded.
func (p DiscoveryPolicy) Admit(r scm.Repository) Exclusion {
	if r.Ref.ID == 0 || r.Ref.Name == "" || r.Ref.Owner == "" {
		return ExcludedInvalid
	}
	switch p.Visibility {
	case VisibilityPublicOnly:
		if r.Visibility != scm.VisPublic {
			return ExcludedVisibility
		}
	case VisibilityInstallationScope:
		if r.Visibility != scm.VisPublic && r.Visibility != scm.VisInternal && r.Visibility != scm.VisPrivate {
			return ExcludedVisibility
		}
	default:
		return ExcludedVisibility
	}
	if r.Archived && !p.IncludeArchived {
		return ExcludedArchived
	}
	if r.Fork && !p.IncludeForks {
		return ExcludedFork
	}
	return ExcludedNone
}
