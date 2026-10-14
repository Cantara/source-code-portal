// Package ops owns /livez, /readyz, /status, /metrics, pprof and the operator actions. It reads in-memory state only
// (GR-41) and imports no job, scm or client package: everything it shows arrives through the consumer-defined
// interfaces below, whose view types it owns and which cmd/scp/internal/bridge satisfies.
package ops

import (
	"iter"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// ---- consumer-defined interfaces ---------------------------------------------------------------------------------

// SnapshotSource is satisfied DIRECTLY by *readmodel.Publisher (ops may import readmodel).
type SnapshotSource interface {
	Current() *readmodel.Snapshot
	Status() readmodel.PublishStatus
}

// StoreSource is bridged over store.Store.Health and the sqlite store's Info (ops imports no store package).
type StoreSource interface{ StoreView() StoreView }

// EngineSource is bridged over refresh.Engine.Inspect/Parked, the planner's Progress, the global-job recorder and the
// github breaker state.
type EngineSource interface{ EngineView() EngineView }

// InstallationStatus is bridged over github.Client (architecture 8.7, signature fixed there).
type InstallationStatus interface {
	Installations() []InstallationView
	Rate() []RateView
}

// Retrier is bridged over refresh.Engine.Enqueue(force=true) (architecture 8.7, signature fixed there).
type Retrier interface{ Retry(key string) error }

// TaskSource is bridged over refresh.Scheduler.Status for every registered task.
type TaskSource interface{ Tasks() []TaskView }

// CatalogueSource feeds the readiness gates; bridged over the catalog service and the warm-load report.
type CatalogueSource interface{ Catalogue() CatalogueView }

// FeatureHealth is the optional per-feature health of docs and manifests (nil: the section shows neutral zeros).
type FeatureHealth interface {
	Docs() DocsView
	Descriptors() DescriptorsView
	Manifests() ManifestsView
}

// IntegrationHealth is bridged over integrations.Service.Health (unit 14, section 2.4).
type IntegrationHealth interface{ IntegrationHealth() []IntegrationView }

// AccessInfo is bridged over access.Access.Info (unit 14).
type AccessInfo interface{ Access() AccessView }

// WebhookInfo is bridged over webhook.Receiver.Stats (unit 04).
type WebhookInfo interface{ Webhook() WebhookView }

// Sources groups the inputs of the status assembler. Snapshot, Store, Engine and Catalogue are required.
type Sources struct {
	Snapshot      SnapshotSource
	Store         StoreSource
	Engine        EngineSource
	Catalogue     CatalogueSource
	Installations InstallationStatus
	Tasks         TaskSource
	Features      FeatureHealth
	Integrations  IntegrationHealth
	Access        AccessInfo
	Webhook       WebhookInfo
}

// ---- views (ops-owned value types; zero time means "unknown/never") ------------------------------------------------

type StoreView struct {
	OK             bool
	Reason         string
	LastBackupAt   time.Time
	BackupEnabled  bool
	BackupInterval time.Duration
	RebuildsTotal  int
	SizeBytes      int64 // -1 unknown
}

type UpstreamView struct {
	Name          string
	Concurrency   int
	Pending       int
	Running       int
	Stuck         int
	PausedUntil   time.Time
	PauseReason   string // "rate_limit" | "unauthorized" | "breaker" | ""
	Breaker       string // "closed" | "open" | "half-open" | "none"
	AllStuckSince time.Time
}

type ParkedView struct {
	Key          string
	Since, Retry time.Time
	Reason       string
}

// GlobalJobView is the last-run record of a global job kind (catalog.reconcile, integrations.jenkins, ...).
type GlobalJobView struct {
	Kind, Upstream      string
	TTL                 time.Duration
	LastAttemptAt       time.Time
	LastSuccessAt       time.Time
	LastError           string
	ConsecutiveFailures int
}

type EngineView struct {
	Heartbeat    time.Time // last dispatcher pass; zero before Run
	Upstreams    []UpstreamView
	Parked       []ParkedView // oldest first, already capped by the bridge at MaxParkedShown
	ParkedTotal  int
	ParkedByKind map[string]int // job kind -> parked keys (all of them, not only the shown ones)
	Globals      []GlobalJobView
}

type InstallationView struct {
	Source          string
	InstallationID  int64
	CredentialKind  string // installation | pat | oauth_user | external | anonymous
	AppSlug         string
	KeyFingerprint  string
	Account         string
	AccountType     string // organization | user | enterprise | unknown
	State           string // active | suspended | removed | unknown
	RepoCount       int
	LastReconcileAt time.Time
	TokenExpiresAt  time.Time
}

type RateView struct {
	Source         string
	InstallationID int64
	Resource       string // core | graphql | search | ...
	Limit          int
	Remaining      int
	ResetAt        time.Time
}

type TaskView struct {
	Name                     string
	Every                    time.Duration
	LastAttempt, LastSuccess time.Time
	LastError                string
	Running                  bool
	Skipped                  int64
}

type CatalogueView struct {
	StoreOpened bool      // the store opened (a degraded store after this does not regress anything)
	Live        bool      // a catalog.reconcile of THIS process succeeded
	Warm        bool      // a catalogue was loaded from the store
	WarmFetched time.Time // when the warm catalogue was last confirmed against GitHub (zero when !Warm)
	Repos       int
}

type DocsView struct {
	ResidentBytes, BudgetBytes, NotResidentBytes int64
	NotResidentDocs                              int
}
type DescriptorsView struct{ RepositoriesWithProblems, Problems int }
type ManifestsView struct {
	Enabled                     bool
	RepositoriesWithDiagnostics int
	DisabledParsers             []string
	DanglingUnits, StaleRepos   int
}

type IntegrationView struct {
	Provider    string
	Enabled     bool
	State       string // ok | failing | unstable | unknown | disabled
	LastSweepAt time.Time
	LastSuccess time.Time
	TTL         time.Duration
	Targets     int
}

type AccessView struct {
	Mode                  string
	TrustedRanges         int
	SpoofDropped          uint64
	ProxySecretConfigured bool
	Warnings              []string // stable codes: broad_trusted_range, no_proxy_secret
}

type WebhookView struct {
	Enabled        bool
	LastAcceptedAt time.Time
	Results        map[string]uint64
	DedupeEntries  int
}

// Kind registers one per-repository item family: how to enumerate the freshness.Info of every catalogue repository in a
// snapshot (one Info per repository, `never` when nothing was fetched). The closures are built in cmd/scp/internal/bridge
// from the typed slots (ops cannot name the generic slot value types).
type Kind struct {
	Family   string // "commits": the name used by readiness.core_kinds and the scp_* metric labels
	Job      string // "commits.refresh": the engine job kind shown in /status
	Upstream string
	TTL      time.Duration // the job's freshness.Policy.TTL
	// Interval is the longest legitimate time between two successful validations of one item: freshness.Policy.IdleBackoffMax
	// when it exceeds TTL (an unchanged repository is revalidated only that rarely), otherwise TTL. 0 means TTL. The
	// stale-kind warning is measured against it, not against TTL (verification finding V15-06: commits has TTL 2 h and idle
	// back-off cap 12 h, so a quiet organisation would otherwise carry jobs.stale permanently).
	Interval time.Duration
	Infos    func(s *readmodel.Snapshot) iter.Seq[freshness.Info]
}

// Errors the Retrier bridge returns (ops cannot see job.ErrQueueFull or refresh.ErrUnknownKind).
type retryError string

func (e retryError) Error() string { return string(e) }

const (
	ErrUnknownKey retryError = "ops: unknown job kind"
	ErrQueueFull  retryError = "ops: job queue full"
	ErrClosed     retryError = "ops: engine closed"
)

// staleBasis is the duration the stale-kind warning multiplies by stale_factor.
func (k Kind) staleBasis() time.Duration {
	if k.Interval > k.TTL {
		return k.Interval
	}
	return k.TTL
}
