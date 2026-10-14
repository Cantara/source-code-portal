package integrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// StatusProvider is the contract of an optional status source (architecture 8.9). Structured data only: a provider
// never returns, stores or proxies SVG and never scrapes HTML (GR-33).
type StatusProvider interface {
	Name() string
	Fetch(ctx context.Context, t Target) (Status, error)
}

// Result is one target's outcome of a bulk fetch. len(results) always equals len(targets), in the same order.
type Result struct {
	Status Status
	Err    error
}

// BulkProvider is implemented by a provider whose upstream answers many targets in one pass (Snyk: one paged project
// listing for the whole organisation). The returned error is a whole-call failure (auth, rate limit, transport).
type BulkProvider interface {
	StatusProvider
	FetchBulk(ctx context.Context, targets []Target) ([]Result, error)
}

// CheckResult is one reachability and credential probe, for `scp doctor`.
type CheckResult struct {
	Instance string // Jenkins instance name; "" for Snyk
	Err      error  // nil: reachable and the credentials work
}

// Checker is implemented by providers for `scp doctor`: one cheap authenticated read per instance, no side effects.
type Checker interface {
	Check(ctx context.Context) []CheckResult
}

// Sentinels. Providers wrap them; the service classifies with errors.Is.
var (
	// ErrNoData: the upstream answered but knows nothing about the target (Snyk: not monitored). Wraps scm.ErrNotFound.
	ErrNoData = fmt.Errorf("integrations: no data for target: %w", scm.ErrNotFound)
	// ErrProtocol: the answer is not what the provider API promises (wrong content type, bad JSON).
	ErrProtocol = errors.New("integrations: unexpected response")
	// ErrTooLarge: the response exceeded the configured cap.
	ErrTooLarge = errors.New("integrations: response too large")
	// ErrAborted: the target was not attempted because its instance's sweep was aborted.
	ErrAborted = errors.New("integrations: sweep aborted for instance")
)

// RateLimitError satisfies job.RateLimited structurally (the engine pauses the "jenkins"/"snyk" upstream until At).
type RateLimitError struct {
	Provider string
	At       time.Time
	Err      error
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s: rate limited until %s: %v", e.Provider, e.At.UTC().Format(time.RFC3339), e.Err)
}
func (e *RateLimitError) Unwrap() error      { return e.Err }
func (e *RateLimitError) RetryAt() time.Time { return e.At }

// PermanentError satisfies job.Permanent structurally: credentials rejected, organisation unknown, configuration wrong.
// Retrying cannot help, so the engine parks the sweep key (24 h probe) and /status shows the reason.
type PermanentError struct {
	Provider string
	Code     string // "auth", "config"
	Err      error
}

func (e *PermanentError) Error() string   { return fmt.Sprintf("%s: %s: %v", e.Provider, e.Code, e.Err) }
func (e *PermanentError) Unwrap() error   { return e.Err }
func (e *PermanentError) Permanent() bool { return true }

// ErrCode is the stable machine code stored in freshness.Info.ErrCode and shown in /status.
func ErrCode(err error) string {
	var rl *RateLimitError
	var pe *PermanentError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &rl):
		return "rate_limited"
	case errors.As(err, &pe):
		return pe.Code
	case errors.Is(err, ErrNoData):
		return "no_data"
	case errors.Is(err, scm.ErrForbidden):
		return "forbidden" // per-target HTTP 403 (Jenkins folder or job permissions); instance-wide only after abort_after in a row
	case errors.Is(err, scm.ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrProtocol):
		return "protocol"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrAborted):
		return "aborted"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "transient"
	}
}
