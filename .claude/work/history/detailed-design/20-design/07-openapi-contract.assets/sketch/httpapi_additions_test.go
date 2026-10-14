package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

func TestAggregateFreshness(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 9, 41, 7, 0, time.UTC)
	fresh := httpapi.FreshnessView{Status: "fresh", FetchedAt: t0, AsOf: t0, CheckedAt: t0}
	stale := httpapi.FreshnessView{Status: "stale", FetchedAt: t0.Add(-time.Hour), AsOf: t0.Add(-2 * time.Hour), CheckedAt: t0, StaleSince: t0.Add(-30 * time.Minute), LastErrorCode: "github.rate_limited"}
	unknown := httpapi.FreshnessView{Status: "unknown"}
	failed := httpapi.FreshnessView{Status: "error", CheckedAt: t0, LastErrorCode: "github.not_found"}
	enc := func(f any) string { b, _ := json.Marshal(f); return string(b) }

	cases := []struct {
		name  string
		parts []httpapi.FreshnessView
		want  string
	}{
		{"fresh and stale", []httpapi.FreshnessView{fresh, stale}, `{"asOf":"2026-10-14T07:41:07Z","checkedAt":"2026-10-14T09:41:07Z","fetchedAt":"2026-10-14T08:41:07Z","kind":"manifests.repo","lastErrorCode":"github.rate_limited","staleSince":"2026-10-14T09:11:07Z","status":"stale"}`},
		{"order does not matter", []httpapi.FreshnessView{stale, fresh}, `{"asOf":"2026-10-14T07:41:07Z","checkedAt":"2026-10-14T09:41:07Z","fetchedAt":"2026-10-14T08:41:07Z","kind":"manifests.repo","lastErrorCode":"github.rate_limited","staleSince":"2026-10-14T09:11:07Z","status":"stale"}`},
		{"unknown beats stale", []httpapi.FreshnessView{stale, unknown}, `{"asOf":"2026-10-14T07:41:07Z","checkedAt":"2026-10-14T09:41:07Z","fetchedAt":"2026-10-14T08:41:07Z","kind":"manifests.repo","lastErrorCode":null,"staleSince":"2026-10-14T09:11:07Z","status":"unknown"}`},
		{"error beats everything", []httpapi.FreshnessView{fresh, stale, unknown, failed}, `{"asOf":"2026-10-14T07:41:07Z","checkedAt":"2026-10-14T09:41:07Z","fetchedAt":"2026-10-14T08:41:07Z","kind":"manifests.repo","lastErrorCode":"github.not_found","staleSince":"2026-10-14T09:11:07Z","status":"error"}`},
		{"a status the server does not know is unknown", []httpapi.FreshnessView{{Status: "weird"}}, `{"asOf":null,"checkedAt":null,"fetchedAt":null,"kind":"manifests.repo","lastErrorCode":null,"staleSince":null,"status":"unknown"}`},
		{"no contributor", nil, `{"asOf":null,"checkedAt":null,"fetchedAt":null,"kind":"manifests.repo","lastErrorCode":null,"staleSince":null,"status":"unknown"}`},
	}
	for _, c := range cases {
		if got := enc(httpapi.AggregateFreshness("manifests.repo", c.parts...)); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestNonNil(t *testing.T) {
	var nilItems []string
	if string(must(json.Marshal(httpapi.NonNil(nilItems)))) != "[]" || string(must(json.Marshal(nilItems))) != "null" {
		t.Fatal("NonNil contract")
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
