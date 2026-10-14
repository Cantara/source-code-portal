package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
)

// FreshnessView is the input of NewFreshness. Status is freshness.Status.String(): the kernel vocabulary
// equals the API vocabulary (fresh|stale|error|unknown). Zero times become null.
type FreshnessView struct {
	Status        string
	Kind          string // item family, for example "commits.view"; REQUIRED by common.yaml (07 made it a required string); "" is a bug
	AsOf          time.Time
	FetchedAt     time.Time
	CheckedAt     time.Time
	StaleSince    time.Time
	LastErrorCode string
}

// NewFreshness builds the shared Freshness object (common.yaml Freshness).
func NewFreshness(v FreshnessView) openapi.Freshness {
	f := openapi.Freshness{Status: openapi.FreshnessStatus(v.Status), Kind: v.Kind, AsOf: utc(v.AsOf), FetchedAt: utc(v.FetchedAt), CheckedAt: utc(v.CheckedAt), StaleSince: utc(v.StaleSince)}
	if v.LastErrorCode != "" {
		f.LastErrorCode = &v.LastErrorCode
	}
	return f
}

func utc(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// NewPageInfo builds PageInfo; next == "" means the last page.
func NewPageInfo(next string, limit int) openapi.PageInfo {
	p := openapi.PageInfo{Limit: limit}
	if next != "" {
		p.NextCursor = &next
	}
	return p
}

// NextLink returns the RFC 8288 value `<path?query>; rel="next"` for the request with cursor replaced.
// It is a relative reference (resolved by the client against the request URI), so no Host header is trusted.
func NextLink(r *http.Request, cursor string) string {
	u := url.URL{Path: r.URL.Path}
	q := r.URL.Query()
	q.Set("cursor", cursor)
	u.RawQuery = q.Encode()
	return "<" + u.RequestURI() + `>; rel="next"`
}
