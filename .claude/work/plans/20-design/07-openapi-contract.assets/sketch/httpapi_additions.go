package httpapi

// Additions that design unit 07 (the OpenAPI contract) asks of unit 06's httpapi package (section 2.3.3 of the design).
// Everything else a feature needs from the contract (WithRequest/RequestFrom, NotModified, StrictErrors, BindingError,
// NewFreshness, FreshnessDigest, WithDigest, NextLink, NewPageInfo, Err, Deref, Ptr) already exists in unit 06.
//
// This file is dropped next to unit 06's files by tools/prove.sh and compiled and tested there (stage 2).

import (
	"net/http"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
)

// statusRank orders the wire statuses of the aggregate rule (design 4.4): error > unknown > stale > fresh. A value the
// server does not know ranks as unknown.
func statusRank(s string) int {
	switch s {
	case "fresh":
		return 0
	case "stale":
		return 1
	case "error":
		return 3
	}
	return 2
}

// AggregateFreshness is the freshness of a response built from several items: never better than its worst contributor.
// status is the worst contributor; asOf, fetchedAt, checkedAt and staleSince are the oldest non-null value among the
// contributors (null when none has one); lastErrorCode is the code of the worst contributor (the first on a tie); no
// contributor at all (an empty catalogue after a successful reconcile) is unknown with every timestamp null.
// Each part is the Derive(now) result of one item mapped to the wire vocabulary by the caller (FreshnessView.Status).
func AggregateFreshness(kind string, parts ...FreshnessView) openapi.Freshness {
	if len(parts) == 0 {
		return NewFreshness(FreshnessView{Status: "unknown", Kind: kind})
	}
	worst := parts[0]
	var asOf, fetched, checked, since time.Time
	oldest := func(cur, v time.Time) time.Time {
		if v.IsZero() || (!cur.IsZero() && !v.Before(cur)) {
			return cur
		}
		return v
	}
	for _, p := range parts {
		if statusRank(p.Status) > statusRank(worst.Status) {
			worst = p
		}
		asOf, fetched, checked, since = oldest(asOf, p.AsOf), oldest(fetched, p.FetchedAt), oldest(checked, p.CheckedAt), oldest(since, p.StaleSince)
	}
	status := worst.Status
	if statusRank(status) == 2 {
		status = "unknown"
	}
	return NewFreshness(FreshnessView{Status: status, Kind: kind, AsOf: asOf, FetchedAt: fetched, CheckedAt: checked, StaleSince: since, LastErrorCode: worst.LastErrorCode})
}

// NonNil turns a nil slice into an empty one: a nil slice marshals as null, the contract says arrays are never null
// (design 3.4). Generated response members are slices; every handler passes them through NonNil.
func NonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// FoldIfNoneMatch joins repeated If-None-Match header lines into one comma-separated line (RFC 9110 5.3 allows a sender to
// split a list over several lines). The generated binder of a header parameter accepts exactly one line and answers a
// binding error (a 400) for two, so the fold runs before the request validator and the mux. In the real package this is
// the first step of WithRequest; it is a separate function here only so that the sketch does not edit unit 06's files.
func FoldIfNoneMatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Values("If-None-Match"); len(v) > 1 {
			r.Header.Set("If-None-Match", strings.Join(v, ", "))
		}
		next.ServeHTTP(w, r)
	})
}
