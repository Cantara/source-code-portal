package ops

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/ops/opsapi"
)

// BuildInfo is fixed at construction (ldflags and debug.ReadBuildInfo, read once in cmd/scp).
type BuildInfo struct{ Version, Commit, BuildID, GoVersion string }

// Collector assembles the status document. Everything it reads is in memory; it makes no outbound call.
type Collector struct {
	cfg     Config
	src     Sources
	kinds   []Kind
	ready   *Readiness
	build   BuildInfo
	started time.Time
	core    []string
}

func utcp(t time.Time) *time.Time { return tp(t) }
func sp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func secp(d time.Duration) *int64 { v := int64(d / time.Second); return &v }

// Status builds the document for the instant now. The per-kind pass is O(repositories x kinds) (about 10 ms at 1,000 x 8).
func (c *Collector) Status(now time.Time) opsapi.Status {
	rep := c.ready.Evaluate(now)
	snap := c.src.Snapshot.Current()
	pub := c.src.Snapshot.Status()
	eng := c.src.Engine.EngineView()
	st := c.src.Store.StoreView()

	out := opsapi.Status{GeneratedAt: now.UTC().Truncate(time.Second)}
	out.Build = opsapi.BuildInfo{Version: c.build.Version, Commit: c.build.Commit, BuildID: c.build.BuildID, GoVersion: c.build.GoVersion,
		StartedAt: c.started.UTC().Truncate(time.Second), UptimeSeconds: int64(now.Sub(c.started) / time.Second)}

	out.Readiness = opsapi.StatusReadiness{State: rep.Phase.String(), ReadySince: utcp(rep.ReadySince), Gates: []opsapi.ReadinessGate{},
		Coverage: opsapi.Coverage{Have: rep.Coverage.Have, Total: rep.Coverage.Total, Ratio: rep.Coverage.Ratio, CoreKinds: slices.Clone(c.core)}}
	for _, g := range rep.Gates {
		out.Readiness.Gates = append(out.Readiness.Gates, opsapi.ReadinessGate{Name: g.Name, OK: g.OK, Detail: g.Detail})
	}

	out.Snapshot = opsapi.StatusSnapshot{Version: int64(snap.Version()), BuiltAt: snap.BuiltAt().UTC().Truncate(time.Second),
		AgeSeconds: max(0, int64(now.Sub(snap.BuiltAt())/time.Second)), RepoCount: len(snap.Repos()), InvalidPublishes: int64(pub.Rejected)}
	if pub.LastRejected != nil {
		out.Snapshot.LastInvalidMessage = truncate(clean(pub.LastRejected.Error()), 300)
	}

	out.Store = opsapi.StatusStore{State: ifs(st.OK, "ok", "degraded"), Reason: sp(truncate(clean(st.Reason), 300)), LastBackupAt: utcp(st.LastBackupAt), RebuildsTotal: st.RebuildsTotal}
	if st.SizeBytes >= 0 {
		v := st.SizeBytes
		out.Store.SizeBytes = &v
	}

	out.Upstreams = []opsapi.StatusUpstream{}
	for _, u := range eng.Upstreams {
		pu := opsapi.StatusUpstream{Name: u.Name, Concurrency: u.Concurrency, Pending: u.Pending, Running: u.Running, Stuck: u.Stuck, Breaker: u.Breaker}
		if u.PausedUntil.After(now) {
			pu.PausedUntil, pu.PauseReason = utcp(u.PausedUntil), sp(u.PauseReason)
		}
		if pu.Breaker == "" {
			pu.Breaker = "none"
		}
		out.Upstreams = append(out.Upstreams, pu)
	}

	var inst []InstallationView
	var rates []RateView
	if c.src.Installations != nil {
		inst, rates = c.src.Installations.Installations(), c.src.Installations.Rate()
	}
	out.Installations = []opsapi.StatusInstallation{}
	for _, i := range inst {
		si := opsapi.StatusInstallation{Source: i.Source, CredentialKind: i.CredentialKind, AppSlug: sp(i.AppSlug), KeyFingerprint: sp(i.KeyFingerprint),
			Account: sp(i.Account), AccountType: i.AccountType, State: i.State, RepoCount: i.RepoCount, LastReconcileAt: utcp(i.LastReconcileAt),
			TokenExpiresAt: utcp(i.TokenExpiresAt), Rate: []opsapi.RateInfo{}}
		for _, r := range rates {
			if r.Source == i.Source && r.InstallationID == i.InstallationID {
				si.Rate = append(si.Rate, opsapi.RateInfo{Resource: r.Resource, Limit: r.Limit, Remaining: r.Remaining, ResetAt: utcp(r.ResetAt)})
			}
		}
		out.Installations = append(out.Installations, si)
	}

	var rows []kindRow
	out.Jobs = opsapi.StatusJobs{Kinds: []opsapi.StatusJobKind{}, Parked: []opsapi.ParkedJob{}, ParkedTotal: eng.ParkedTotal}
	for _, k := range c.kinds {
		ks := k.Stats(snap, now)
		rows = append(rows, kindRow{Job: k.Job, TTL: k.staleBasis(), Stats: ks})
		jk := opsapi.StatusJobKind{Kind: k.Job, Upstream: k.Upstream, Items: ks.Items, LastSuccessAt: utcp(ks.LastSuccess), Failing: ks.Failing, Parked: eng.ParkedByKind[k.Job]}
		if ks.HasAges {
			jk.OldestFetchAgeSeconds, jk.P95FetchAgeSeconds = secp(ks.Oldest), secp(ks.P95)
		}
		out.Jobs.Kinds = append(out.Jobs.Kinds, jk)
	}
	for _, g := range eng.Globals {
		rows = append(rows, kindRow{Job: g.Kind, TTL: g.TTL, Global: true, Last: g})
		jk := opsapi.StatusJobKind{Kind: g.Kind, Upstream: g.Upstream, Items: 1, LastSuccessAt: utcp(g.LastSuccessAt), Parked: eng.ParkedByKind[g.Kind]}
		if g.ConsecutiveFailures > 0 {
			jk.Failing = 1
		}
		if !g.LastSuccessAt.IsZero() {
			jk.OldestFetchAgeSeconds = secp(now.Sub(g.LastSuccessAt))
			jk.P95FetchAgeSeconds = jk.OldestFetchAgeSeconds
		}
		out.Jobs.Kinds = append(out.Jobs.Kinds, jk)
	}
	slices.SortStableFunc(out.Jobs.Kinds, func(a, b opsapi.StatusJobKind) int { return strings.Compare(a.Kind, b.Kind) })
	for i, p := range eng.Parked {
		if i >= c.cfg.Status.MaxParkedShown {
			break
		}
		out.Jobs.Parked = append(out.Jobs.Parked, opsapi.ParkedJob{Key: p.Key, Since: p.Since.UTC().Truncate(time.Second), RetryAt: utcp(p.Retry), Reason: truncate(clean(p.Reason), 300)})
	}

	out.Tasks = []opsapi.StatusTask{}
	if c.src.Tasks != nil {
		for _, t := range c.src.Tasks.Tasks() {
			out.Tasks = append(out.Tasks, opsapi.StatusTask{Name: t.Name, EverySeconds: int64(t.Every / time.Second), LastAttemptAt: utcp(t.LastAttempt),
				LastSuccessAt: utcp(t.LastSuccess), LastError: sp(truncate(clean(t.LastError), 300)), Running: t.Running, Skipped: t.Skipped})
		}
	}

	var dv DocsView
	var dsv DescriptorsView
	var mv ManifestsView
	if c.src.Features != nil {
		dv, dsv, mv = c.src.Features.Docs(), c.src.Features.Descriptors(), c.src.Features.Manifests()
	}
	out.Docs = opsapi.StatusDocs{ResidentBytes: dv.ResidentBytes, BudgetBytes: dv.BudgetBytes, NotResidentDocs: dv.NotResidentDocs, NotResidentBytes: dv.NotResidentBytes}
	out.Descriptors = opsapi.StatusDescriptors{RepositoriesWithProblems: dsv.RepositoriesWithProblems, Problems: dsv.Problems}
	out.Manifests = opsapi.StatusManifests{Enabled: mv.Enabled, RepositoriesWithDiagnostics: mv.RepositoriesWithDiagnostics,
		DisabledParsers: append([]string{}, mv.DisabledParsers...), DanglingUnits: mv.DanglingUnits, StaleRepos: mv.StaleRepos}

	var integ []IntegrationView
	if c.src.Integrations != nil {
		integ = c.src.Integrations.IntegrationHealth()
	}
	out.Integrations = []opsapi.StatusIntegration{}
	for _, i := range integ {
		s := i.State
		if !i.Enabled {
			s = "disabled" // a disabled provider is never "ok" (GR-41)
		}
		out.Integrations = append(out.Integrations, opsapi.StatusIntegration{Provider: i.Provider, Enabled: i.Enabled, State: s, LastSweepAt: utcp(i.LastSweepAt), Targets: i.Targets})
	}

	var av AccessView
	var hv WebhookView
	if c.src.Access != nil {
		av = c.src.Access.Access()
		out.Access = &opsapi.StatusAccess{Mode: av.Mode, TrustedRanges: av.TrustedRanges, SpoofDropped: av.SpoofDropped, ProxySecretConfigured: av.ProxySecretConfigured}
	}
	if c.src.Webhook != nil {
		hv = c.src.Webhook.Webhook()
		out.Webhook = &opsapi.StatusWebhook{LastAcceptedAt: utcp(hv.LastAcceptedAt), Results: hv.Results, DedupeEntries: hv.DedupeEntries}
	}

	out.Warnings = deriveWarnings(warnInputs{Now: now, Started: c.started, Cfg: c.cfg, Ready: rep, Store: st, Publish: pub, Eng: eng, Inst: inst, Rates: rates,
		Kinds: rows, Docs: dv, Desc: dsv, Man: mv, Integ: integ, Access: av, Hook: hv})
	return out
}

// clean replaces control characters (an upstream error text can carry newlines) so one message is one line.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const ell = "…"
	return strings.ToValidUTF8(s[:n-len(ell)], "") + ell // at most n bytes in all, valid UTF-8
}

// Handler serves api.status: the document, a weak ETag W/"b<hash>" over the exact bytes (design 07 section 4.4, body-hash) (ages are in seconds, so a 304 happens
// only for repeat polls within the same second; the validator is honest rather than coarse), HEAD supported.
func (c *Collector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(c.Status(time.Now()))
		if err != nil {
			httpapi.WriteProblem(w, r, httpapi.Problem{Status: 500, Code: httpapi.CodeInternal, Title: "Internal error"})
			return
		}
		sum := sha256.Sum256(body)
		etag := `W/"b` + base64.RawURLEncoding.EncodeToString(sum[:12]) + `"` // weak, "body-hash" of design 07 section 4.4
		h := w.Header()
		h.Set("ETag", etag)
		h.Set("Cache-Control", "no-cache")
		if httpapi.NotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

var _ = freshness.StatusFresh
