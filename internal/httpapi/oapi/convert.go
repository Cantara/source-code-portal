package oapi

import (
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// FreshnessOf converts a stored freshness to its API form, deriving the status at now.
func FreshnessOf(f snapshot.Freshness, now time.Time) Freshness {
	return Freshness{Status: FreshnessStatus(f.Derive(now).String()), FetchedAt: TimeOrNil(f.FetchedAt), CheckedAt: TimeOrNil(f.CheckedAt)}
}

// TimeOrNil returns nil for the zero time and the UTC time otherwise.
func TimeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}
