package badges

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func TestRenderGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/last-commit.golden.svg")
	if err != nil {
		t.Fatal(err)
	}
	if got := render(badge{label: "last commit", message: "3 days ago", color: successColor}); string(got) != strings.TrimSpace(string(want)) {
		t.Errorf("render:\n got %s\nwant %s", got, want)
	}
}

func TestRenderHostile(t *testing.T) {
	got := render(badge{label: "issues", message: "<script>alert(1)</script>&\"'\u202e\x00end" + strings.Repeat("x", 60), color: warningColor})
	d := xml.NewDecoder(strings.NewReader(string(got)))
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("not well-formed: %v\n%s", err, got)
		}
		if se, ok := tok.(xml.StartElement); ok {
			switch se.Name.Local {
			case "svg", "title", "g", "rect", "text":
			default:
				t.Errorf("element %s in %s", se.Name.Local, got)
			}
		}
	}
	if strings.Contains(string(got), "<script") || strings.Contains(string(got), "\u202e") || !strings.Contains(string(got), "…") {
		t.Errorf("hostile message: %s", got)
	}
}

func TestWidths(t *testing.T) {
	for s, want := range map[string]int{
		"issues": 33, "12 open": 45, "last commit": 65, "Apache-2.0": 63, "release": 41, "v1.2.3": 35, "unknown": 49, "none": 27,
		"today": 31, "yesterday": 55, "3 days ago": 61, "æøå ÆØÅ": 55, "日本語": 33, "": 1, " ": 3, "W": 11, "i": 3, "0": 7,
		"1.2k open": 55, "MIT": 19, "GPL-3.0-only": 69, "repos": 31,
	} {
		if got := textWidth(s); got != want {
			t.Errorf("textWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestFormat(t *testing.T) {
	for n, want := range map[int]string{-5: "0", 999: "999", 1000: "1k", 1100: "1.1k", 12345: "12k", 999_999: "999k", 1_234_567: "1.2M"} {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %s, want %s", n, got, want)
		}
	}
	now := time.Date(2026, 10, 14, 0, 30, 0, 0, time.UTC)
	for ago, want := range map[time.Duration]string{0: "today", time.Hour: "yesterday", 3 * 24 * time.Hour: "3 days ago",
		20 * 24 * time.Hour: "2 weeks ago", 100 * 24 * time.Hour: "3 months ago", 800 * 24 * time.Hour: "2 years ago", -time.Hour: "today"} {
		if got, _ := relativeDay(now, now.Add(-ago)); got != want {
			t.Errorf("relativeDay(-%v) = %s, want %s", ago, got, want)
		}
	}
}

func testSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	fresh := snapshot.Freshness{State: snapshot.StateOK}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x", License: "Apache-2.0", OpenIssues: 1234},
				Commits: []snapshot.Commit{{SHA: "a", CommittedAt: time.Now().Add(-3 * 24 * time.Hour)}}, CommitsFresh: fresh,
				Release: &snapshot.Release{Tag: "v1.2.3"}, ReleaseFresh: fresh},
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "new"},
				ReleaseFresh: snapshot.Freshness{State: snapshot.StateNotFound}},
		}, map[snapshot.RepoKey]snapshot.RepoKey{"acme/old": "acme/x"}, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "g", Members: []snapshot.RepoKey{"acme/new", "acme/x"}}})
		b.SetCatalogued()
	})
	p.Flush()
	return p.Current()
}

func TestHandler(t *testing.T) {
	s := testSnapshot(t)
	h := Handler(func(context.Context) *snapshot.Snapshot { return s })
	tests := []struct {
		path, msg, cache string
		status           int
		etag             bool
	}{
		{"/badges/repos/acme/x/last-commit.svg", "3 days ago", "public, max-age=300", 200, true},
		{"/badges/repos/ACME/X/release.svg", "v1.2.3", "public, max-age=300", 200, true},
		{"/badges/repos/acme/x/license.svg", "Apache-2.0", "public, max-age=300", 200, true},
		{"/badges/repos/acme/x/issues.svg", "1.2k open", "public, max-age=300", 200, true},
		{"/badges/repos/acme/old/issues.svg", "1.2k open", "public, max-age=300", 200, true},
		{"/badges/repos/acme/new/release.svg", "none", "public, max-age=60", 200, false},
		{"/badges/repos/acme/new/license.svg", "none", "public, max-age=60", 200, false},
		{"/badges/repos/acme/new/last-commit.svg", "unknown", "public, max-age=60", 200, false},
		{"/badges/groups/g/repos.svg", "2", "public, max-age=300", 200, true},
		{"/badges/repos/acme/nope/issues.svg", "unknown", "public, max-age=60", 404, false},
		{"/badges/repos/acme/x/stars.svg", "unknown", "public, max-age=60", 404, false},
		{"/badges/repos/acme/x/issues.png", "unknown", "public, max-age=60", 404, false},
		{"/badges/groups/nope/repos.svg", "unknown", "public, max-age=60", 404, false},
		{"/badges/whatever", "unknown", "public, max-age=60", 404, false},
	}
	var unknownBody string
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		hd := rec.Header()
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), ">"+tc.msg+"</text>") || hd.Get("Cache-Control") != tc.cache ||
			(hd.Get("ETag") != "") != tc.etag || hd.Get("Content-Type") != "image/svg+xml" || hd.Get("Content-Security-Policy") != csp ||
			hd.Get("Cross-Origin-Resource-Policy") != "cross-origin" {
			t.Errorf("%s: %d %v %s", tc.path, rec.Code, hd, rec.Body)
		}
		if tc.status == 404 {
			if unknownBody != "" && rec.Body.String() != unknownBody {
				t.Errorf("%s: the 404 body differs", tc.path)
			}
			unknownBody = rec.Body.String()
		}
		if tc.etag {
			rec2 := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("If-None-Match", hd.Get("ETag"))
			h.ServeHTTP(rec2, req)
			if rec2.Code != http.StatusNotModified || rec2.Body.Len() != 0 {
				t.Errorf("%s: If-None-Match gave %d", tc.path, rec2.Code)
			}
		}
	}

	starting := &snapshot.Snapshot{}
	rec := httptest.NewRecorder()
	Handler(func(context.Context) *snapshot.Snapshot { return starting }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/badges/repos/a/b/issues.svg", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "15" || rec.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(rec.Body.String(), "starting") {
		t.Errorf("starting: %d %v", rec.Code, rec.Header())
	}
}

type releases struct {
	rel *snapshot.Release
	err error
}

func (r releases) LatestRelease(context.Context, string, string) (*snapshot.Release, error) {
	return r.rel, r.err
}

func TestReleaseJob(t *testing.T) {
	for name, tc := range map[string]struct {
		src   releases
		state snapshot.State
		err   bool
	}{
		"release": {releases{rel: &snapshot.Release{Tag: "v1"}}, snapshot.StateOK, false},
		"none":    {releases{}, snapshot.StateNotFound, false},
		"failing": {releases{err: errors.New("boom")}, "", true},
	} {
		spec := New(map[string]Releases{"gh": tc.src}).ReleaseSpec()
		res, err := spec.Run(t.Context(), sync.Job{Key: ReleaseKind + "/acme/x", Source: "gh", Repo: "acme/x"})
		if (err != nil) != tc.err || res.State != tc.state {
			t.Errorf("%s: %+v, %v", name, res, err)
		}
		if err == nil {
			again, _ := spec.Run(t.Context(), sync.Job{Key: ReleaseKind + "/acme/x", Source: "gh", Repo: "acme/x",
				Prev: store.Item{State: res.State, Payload: res.Payload}})
			if !again.Unchanged {
				t.Errorf("%s: the same release again is not Unchanged", name)
			}
		}
	}
}
