// Package opsapi stands in for the oapi-codegen output of the ops fragment of api/openapi.admin.yaml (schemas Status and
// its parts, RetryJobRequest, RetryJobResponse). Hand-written here only so unit 15 compiles; Webhook and Access are the
// two additive sections proposed by unit 15 (deviation D-O1).
package opsapi

import "time"

type BuildInfo struct {
	Version       string    `json:"version"`
	Commit        string    `json:"commit"`
	BuildID       string    `json:"buildId"`
	GoVersion     string    `json:"goVersion,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}
type ReadinessGate struct {
	Name   string `json:"name"` // store | catalogue | coverage | startup_deadline
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}
type Coverage struct {
	Have      int      `json:"have"`
	Total     int      `json:"total"`
	Ratio     float64  `json:"ratio"`
	CoreKinds []string `json:"coreKinds"`
}
type StatusReadiness struct {
	State      string          `json:"state"` // warming | ready | degraded | shutting-down
	ReadySince *time.Time      `json:"readySince"`
	Gates      []ReadinessGate `json:"gates"`
	Coverage   Coverage        `json:"coverage"`
}
type StatusSnapshot struct {
	Version            int64     `json:"version"`
	BuiltAt            time.Time `json:"builtAt"`
	AgeSeconds         int64     `json:"ageSeconds"`
	RepoCount          int       `json:"repoCount"`
	InvalidPublishes   int64     `json:"invalidPublishes"`
	LastInvalidMessage string    `json:"lastInvalidMessage,omitempty"`
}
type StatusStore struct {
	State         string     `json:"state"` // ok | degraded
	Reason        *string    `json:"reason"`
	LastBackupAt  *time.Time `json:"lastBackupAt"`
	RebuildsTotal int        `json:"rebuildsTotal"`
	SizeBytes     *int64     `json:"sizeBytes"`
}
type StatusUpstream struct {
	Name        string     `json:"name"`
	Concurrency int        `json:"concurrency"`
	Pending     int        `json:"pending"`
	Running     int        `json:"running"`
	Stuck       int        `json:"stuck"`
	PausedUntil *time.Time `json:"pausedUntil"`
	PauseReason *string    `json:"pauseReason"` // rate_limit | unauthorized | breaker | null
	Breaker     string     `json:"breaker"`     // closed | open | half-open | none
}
type RateInfo struct {
	Resource  string     `json:"resource"`
	Limit     int        `json:"limit"`
	Remaining int        `json:"remaining"`
	ResetAt   *time.Time `json:"resetAt"`
}
type StatusInstallation struct {
	Source          string     `json:"source"`
	CredentialKind  string     `json:"credentialKind"`
	AppSlug         *string    `json:"appSlug"`
	KeyFingerprint  *string    `json:"keyFingerprint"`
	Account         *string    `json:"account"`
	AccountType     string     `json:"accountType"`
	State           string     `json:"state"`
	RepoCount       int        `json:"repoCount"`
	LastReconcileAt *time.Time `json:"lastReconcileAt"`
	TokenExpiresAt  *time.Time `json:"tokenExpiresAt"`
	Rate            []RateInfo `json:"rate"`
}
type StatusJobKind struct {
	Kind                  string     `json:"kind"`
	Upstream              string     `json:"upstream"`
	Items                 int        `json:"items"`
	OldestFetchAgeSeconds *int64     `json:"oldestFetchAgeSeconds"`
	P95FetchAgeSeconds    *int64     `json:"p95FetchAgeSeconds"`
	LastSuccessAt         *time.Time `json:"lastSuccessAt"`
	Failing               int        `json:"failing"`
	Parked                int        `json:"parked"`
}
type ParkedJob struct {
	Key     string     `json:"key"`
	Since   time.Time  `json:"since"`
	RetryAt *time.Time `json:"retryAt"`
	Reason  string     `json:"reason"`
}
type StatusJobs struct {
	Kinds       []StatusJobKind `json:"kinds"`
	Parked      []ParkedJob     `json:"parked"`
	ParkedTotal int             `json:"parkedTotal"`
}
type StatusTask struct {
	Name          string     `json:"name"`
	EverySeconds  int64      `json:"everySeconds"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	LastError     *string    `json:"lastError"`
	Running       bool       `json:"running"`
	Skipped       int64      `json:"skipped"`
}
type StatusDocs struct {
	ResidentBytes    int64 `json:"residentBytes"`
	BudgetBytes      int64 `json:"budgetBytes"`
	NotResidentDocs  int   `json:"notResidentDocs"`
	NotResidentBytes int64 `json:"notResidentBytes"`
}
type StatusDescriptors struct {
	RepositoriesWithProblems int `json:"repositoriesWithProblems"`
	Problems                 int `json:"problems"`
}
type StatusManifests struct {
	Enabled                     bool     `json:"enabled"`
	RepositoriesWithDiagnostics int      `json:"repositoriesWithDiagnostics"`
	DisabledParsers             []string `json:"disabledParsers"`
	DanglingUnits               int      `json:"danglingUnits"`
	StaleRepos                  int      `json:"staleRepos"`
}
type StatusIntegration struct {
	Provider    string     `json:"provider"`
	Enabled     bool       `json:"enabled"`
	State       string     `json:"state"` // ok | failing | unstable | unknown | disabled
	LastSweepAt *time.Time `json:"lastSweepAt"`
	Targets     int        `json:"targets"`
}
type StatusWarning struct {
	Code     string     `json:"code"`
	Severity string     `json:"severity"` // info | warning | critical
	Message  string     `json:"message"`
	Since    *time.Time `json:"since"`
}

// StatusWebhook and StatusAccess are additive sections (D-O1).
type StatusWebhook struct {
	LastAcceptedAt *time.Time        `json:"lastAcceptedAt"`
	Results        map[string]uint64 `json:"results"`
	DedupeEntries  int               `json:"dedupeEntries"`
}
type StatusAccess struct {
	Mode                  string `json:"mode"`
	TrustedRanges         int    `json:"trustedRanges"`
	SpoofDropped          uint64 `json:"spoofDropped"`
	ProxySecretConfigured bool   `json:"proxySecretConfigured"`
}
type Status struct {
	GeneratedAt   time.Time            `json:"generatedAt"`
	Build         BuildInfo            `json:"build"`
	Readiness     StatusReadiness      `json:"readiness"`
	Snapshot      StatusSnapshot       `json:"snapshot"`
	Store         StatusStore          `json:"store"`
	Upstreams     []StatusUpstream     `json:"upstreams"`
	Installations []StatusInstallation `json:"installations"`
	Jobs          StatusJobs           `json:"jobs"`
	Tasks         []StatusTask         `json:"tasks"`
	Docs          StatusDocs           `json:"docs"`
	Descriptors   StatusDescriptors    `json:"descriptors"`
	Manifests     StatusManifests      `json:"manifests"`
	Integrations  []StatusIntegration  `json:"integrations"`
	Warnings      []StatusWarning      `json:"warnings"`
	Webhook       *StatusWebhook       `json:"webhook,omitempty"`
	Access        *StatusAccess        `json:"access,omitempty"`
}

type RetryJobRequest struct {
	Key string `json:"key"`
}
type RetryJobResponse struct {
	Key      string `json:"key"`
	Accepted bool   `json:"accepted"`
}
