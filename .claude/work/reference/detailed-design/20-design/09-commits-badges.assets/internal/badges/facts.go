package badges

import (
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Facts is the consumer-defined view of everything a badge can show; the satisfier is bridge.BadgeFacts in
// cmd/scp/internal/bridge, which reads the catalog, commits, manifests and integrations slots (architecture 6.2).
// The bool is false when the repository has no entry in s.
type Facts interface {
	Facts(s *readmodel.Snapshot, slug scm.Slug) (RepoFacts, bool)
}

type RepoFacts struct {
	Repo          scm.Repository
	Stats         freshness.Value[scm.RepoStats]
	LastCommit    freshness.Value[time.Time] // newest commit time; State no_content for an empty repository
	Language      freshness.Value[string]    // primary language of the manifests; State unsupported when none was recognised
	BuildTool     freshness.Value[string]
	LatestVersion freshness.Value[string] // declared version of the repository's primary artifact
	InternalDeps  freshness.Value[int]    // number of other catalogue repositories this one depends on
	Build         *Fact                   // nil: no Jenkins provider is configured, the "build" kind does not exist (404)
	Security      *Fact                   // nil: no Snyk provider is configured, the "security" kind does not exist (404)
}

// Fact is a ready-made message from an integration (architecture 8.9 and the integrations design, 4.6): the closed
// message strings and the tone are decided by the provider package, so no upstream text reaches a badge.
// Stale means the message is the last known one and the data is older than the provider's TTL allows.
type Fact struct {
	Message string
	Tone    Tone
	Stale   bool
}

type factState uint8

const (
	hasValue factState = iota
	absent             // the source answered and there is nothing: no content, not found, nothing recognised
	noData             // never fetched, or failed without ever succeeding, or too large to use
)

// fact classifies a freshness.Value. A value exists exactly when FetchedAt is set (stale-while-error keeps the last
// good value and FetchedAt); its State then tells whether it is a value or a recorded absence.
func fact[T any](v freshness.Value[T]) (T, factState) {
	var zero T
	if v.Info.FetchedAt.IsZero() {
		return zero, noData
	}
	switch v.Info.State {
	case freshness.StateOK, freshness.StateError:
		return v.V, hasValue
	case freshness.StateNoContent, freshness.StateNotFound, freshness.StateUnsupported:
		return zero, absent
	}
	return zero, noData
}
