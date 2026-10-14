package ops

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/ops/opsapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// Severity values of the API enum.
const (
	SevInfo     = "info"
	SevWarning  = "warning"
	SevCritical = "critical"
)

type kindRow struct {
	Job    string
	TTL    time.Duration
	Stats  KindStats
	Global bool
	Last   GlobalJobView
}

type warnInputs struct {
	Now     time.Time
	Started time.Time
	Cfg     Config
	Ready   Report
	Store   StoreView
	Publish readmodel.PublishStatus
	Eng     EngineView
	Inst    []InstallationView
	Rates   []RateView
	Kinds   []kindRow
	Docs    DocsView
	Desc    DescriptorsView
	Man     ManifestsView
	Integ   []IntegrationView
	Access  AccessView
	Hook    WebhookView
}

func tp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC().Truncate(time.Second)
	return &u
}

// deriveWarnings is a pure function of its inputs: the rules table of the design (section 4.6) in code. Output is
// ranked most severe first, then oldest first, then by code, so it is deterministic.
func deriveWarnings(in warnInputs) []opsapi.StatusWarning {
	var w []opsapi.StatusWarning
	add := func(code, sev, msg string, since time.Time) {
		// every message is one line of at most 300 bytes: several carry upstream error text
		w = append(w, opsapi.StatusWarning{Code: code, Severity: sev, Message: truncate(clean(msg), 300), Since: tp(since)})
	}
	if in.Ready.Phase == PhaseDegraded {
		add("readiness.degraded", SevWarning, fmt.Sprintf("ready by the startup deadline with coverage %.2f of %q", in.Ready.Coverage.Ratio, in.Ready.Coverage.Family), in.Ready.ReadySince)
	}
	if !in.Store.OK {
		add("store.degraded", SevCritical, "store is degraded (writes suspended, serving continues): "+in.Store.Reason, time.Time{})
	}
	if in.Store.BackupEnabled && in.Store.BackupInterval > 0 {
		switch {
		case !in.Store.LastBackupAt.IsZero() && in.Now.Sub(in.Store.LastBackupAt) > 3*in.Store.BackupInterval:
			add("store.backup-stale", SevWarning, fmt.Sprintf("last backup %s ago (interval %s)", in.Now.Sub(in.Store.LastBackupAt).Truncate(time.Minute), in.Store.BackupInterval), in.Store.LastBackupAt)
		case in.Store.LastBackupAt.IsZero() && in.Now.Sub(in.Started) > 3*in.Store.BackupInterval: // never succeeded is the worst case, not a silent one
			add("store.backup-stale", SevWarning, fmt.Sprintf("no backup has succeeded since the process started %s ago (interval %s)", in.Now.Sub(in.Started).Truncate(time.Minute), in.Store.BackupInterval), in.Started)
		}
	}
	if in.Publish.Rejected > 0 {
		sev := SevWarning
		if in.Now.Sub(in.Publish.LastRejectedAt) < time.Hour {
			sev = SevCritical
		}
		add("snapshot.invalid-publish", sev, fmt.Sprintf("%d snapshot candidates were rejected by a validator", in.Publish.Rejected), in.Publish.LastRejectedAt)
	}
	for _, u := range in.Eng.Upstreams {
		switch {
		case u.Stuck > 0 && u.Stuck >= u.Concurrency:
			add("upstream.stuck", SevCritical, fmt.Sprintf("every slot of upstream %q is held by a job that ignores cancellation", u.Name), u.AllStuckSince)
		case u.Stuck > 0:
			add("upstream.stuck", SevWarning, fmt.Sprintf("%d job(s) of upstream %q ignore cancellation", u.Stuck, u.Name), time.Time{})
		}
		if u.PauseReason == "unauthorized" && u.PausedUntil.After(in.Now) {
			add("upstream.unauthorized", SevCritical, fmt.Sprintf("upstream %q rejects the credential; paused until %s", u.Name, u.PausedUntil.UTC().Format(time.RFC3339)), time.Time{})
		} else if !u.PausedUntil.IsZero() && u.PausedUntil.After(in.Now) {
			add("upstream.paused", SevInfo, fmt.Sprintf("upstream %q paused until %s (%s)", u.Name, u.PausedUntil.UTC().Format(time.RFC3339), cmp.Or(u.PauseReason, "unspecified")), time.Time{})
		}
	}
	if in.Eng.ParkedTotal > 0 {
		sev := SevWarning
		if in.Eng.ParkedTotal >= 50 {
			sev = SevCritical
		}
		var since time.Time
		for _, p := range in.Eng.Parked {
			if since.IsZero() || p.Since.Before(since) {
				since = p.Since
			}
		}
		add("jobs.parked", sev, fmt.Sprintf("%d job key(s) are parked after permanent failures", in.Eng.ParkedTotal), since)
	}
	for _, r := range in.Inst {
		switch r.State {
		case "suspended", "removed":
			add("github.installation-"+r.State, SevWarning, fmt.Sprintf("installation of source %q is %s", r.Source, r.State), time.Time{})
		}
		if !r.TokenExpiresAt.IsZero() && r.TokenExpiresAt.Before(in.Now) {
			add("github.token-expired", SevCritical, fmt.Sprintf("the token of source %q expired and was not refreshed", r.Source), r.TokenExpiresAt)
		}
	}
	for _, r := range in.Rates {
		if r.Limit <= 0 || (!r.ResetAt.IsZero() && r.ResetAt.Before(in.Now)) {
			continue
		}
		ratio := float64(r.Remaining) / float64(r.Limit)
		switch {
		case ratio < in.Cfg.Warnings.RateLowRatio/5:
			add("github.rate-low", SevCritical, fmt.Sprintf("%s/%s: %d of %d requests left until %s", r.Source, r.Resource, r.Remaining, r.Limit, r.ResetAt.UTC().Format(time.RFC3339)), time.Time{})
		case ratio < in.Cfg.Warnings.RateLowRatio:
			add("github.rate-low", SevWarning, fmt.Sprintf("%s/%s: %d of %d requests left until %s", r.Source, r.Resource, r.Remaining, r.Limit, r.ResetAt.UTC().Format(time.RFC3339)), time.Time{})
		}
	}
	for _, k := range in.Kinds {
		limit := time.Duration(float64(k.TTL) * in.Cfg.Warnings.StaleFactor)
		switch {
		case k.Global && k.Last.ConsecutiveFailures > 0 && !k.Last.LastSuccessAt.IsZero() && in.Now.Sub(k.Last.LastSuccessAt) > limit && limit > 0:
			add("jobs.stale", SevWarning, fmt.Sprintf("%s has not succeeded for %s (TTL %s): %s", k.Job, in.Now.Sub(k.Last.LastSuccessAt).Truncate(time.Second), k.TTL, k.Last.LastError), k.Last.LastSuccessAt)
		case k.Global && k.Last.LastSuccessAt.IsZero() && limit > 0 && in.Now.Sub(in.Started) > limit:
			add("jobs.stale", SevWarning, fmt.Sprintf("%s has never succeeded since start", k.Job), in.Started)
		case !k.Global && k.Stats.HasAges && limit > 0 && k.Stats.Oldest > limit:
			add("jobs.stale", SevWarning, fmt.Sprintf("%s: oldest item is %s old (TTL %s)", k.Job, k.Stats.Oldest.Truncate(time.Second), k.TTL), in.Now.Add(-k.Stats.Oldest))
		}
	}
	if in.Docs.NotResidentDocs > 0 {
		add("docs.budget-exceeded", SevWarning, fmt.Sprintf("%d document(s) are not resident (docs.resident_budget %d bytes); raise it together with GOMEMLIMIT", in.Docs.NotResidentDocs, in.Docs.BudgetBytes), time.Time{})
	}
	if in.Desc.Problems > 0 {
		add("descriptors.problems", SevInfo, fmt.Sprintf("%d descriptor problem(s) in %d repositories", in.Desc.Problems, in.Desc.RepositoriesWithProblems), time.Time{})
	}
	for _, p := range in.Man.DisabledParsers {
		add("manifests.parser-disabled", SevWarning, fmt.Sprintf("parser %q is disabled until restart after repeated abandoned parses", p), time.Time{})
	}
	if in.Man.Enabled && in.Man.StaleRepos > 0 {
		add("manifests.stale", SevInfo, fmt.Sprintf("%d repositories have stale manifest facts", in.Man.StaleRepos), time.Time{})
	}
	for _, i := range in.Integ {
		if !i.Enabled {
			continue
		}
		switch {
		case i.LastSuccess.IsZero() && i.TTL > 0 && in.Now.Sub(in.Started) > 3*i.TTL: // grace: no warning while the first sweep is still due
			add("integrations.degraded", SevWarning, fmt.Sprintf("provider %q has no successful sweep", i.Provider), time.Time{})
		case i.TTL > 0 && !i.LastSuccess.IsZero() && in.Now.Sub(i.LastSuccess) > 3*i.TTL:
			add("integrations.degraded", SevWarning, fmt.Sprintf("provider %q: last successful sweep %s ago", i.Provider, in.Now.Sub(i.LastSuccess).Truncate(time.Second)), i.LastSuccess)
		}
	}
	for _, c := range in.Access.Warnings {
		add("access."+c, SevWarning, "access: "+c, time.Time{})
	}
	if in.Hook.Enabled && in.Cfg.Warnings.WebhookSilence > 0 {
		since := in.Hook.LastAcceptedAt
		if since.IsZero() {
			since = in.Started
		}
		if in.Now.Sub(since) > in.Cfg.Warnings.WebhookSilence {
			add("webhook.silent", SevInfo, fmt.Sprintf("no webhook delivery accepted for %s", in.Now.Sub(since).Truncate(time.Minute)), since)
		}
	}
	rank := func(s string) int {
		switch s {
		case SevCritical:
			return 0
		case SevWarning:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(w, func(a, b opsapi.StatusWarning) int {
		if c := cmp.Compare(rank(a.Severity), rank(b.Severity)); c != 0 {
			return c
		}
		switch {
		case a.Since != nil && b.Since != nil:
			if c := a.Since.Compare(*b.Since); c != 0 {
				return c
			}
		case a.Since != nil:
			return -1
		case b.Since != nil:
			return 1
		}
		return cmp.Compare(a.Code, b.Code)
	})
	if w == nil {
		w = []opsapi.StatusWarning{}
	}
	return w
}
