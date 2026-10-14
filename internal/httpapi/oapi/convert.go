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

// NewProblem returns an RFC 9457 problem with the portal's type URN for code.
func NewProblem(status int, code, title, detail string) Problem {
	return Problem{Type: "urn:scp:problem:" + code, Title: title, Status: status, Code: code, Detail: &detail}
}
