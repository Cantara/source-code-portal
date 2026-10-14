package badges

import (
	"bytes"
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const goldenDir = "../../testdata/golden/badges"

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), got) {
		t.Errorf("%s differs:\n got: %s\nwant: %s", name, got, want)
	}
	assertWellFormed(t, got)
}

// assertWellFormed checks that b is a closed document: strict XML, one svg root, only the known elements and attributes
// (so no script, foreignObject, href, on* handler, style, processing instruction or entity declaration can appear), and
// that the root width is the sum of the two rectangles.
func assertWellFormed(t *testing.T, b []byte) {
	t.Helper()
	elems := map[string]bool{"svg": true, "title": true, "g": true, "rect": true, "text": true}
	attrs := map[string]bool{"xmlns": true, "width": true, "height": true, "role": true, "aria-label": true, "shape-rendering": true,
		"x": true, "fill": true, "text-anchor": true, "font-family": true, "text-rendering": true, "font-size": true, "y": true,
		"textLength": true, "transform": true}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	roots, depth, rootW, sumW := 0, 0, "", 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v\n%s", err, b)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			if !elems[v.Name.Local] {
				t.Fatalf("element %q is not in the closed set: %s", v.Name.Local, b)
			}
			if depth == 0 {
				roots++
			}
			depth++
			for _, a := range v.Attr {
				if !attrs[a.Name.Local] {
					t.Fatalf("attribute %q is not in the closed set: %s", a.Name.Local, b)
				}
				if v.Name.Local == "svg" && a.Name.Local == "width" {
					rootW = a.Value
				}
				if v.Name.Local == "rect" && a.Name.Local == "width" {
					n := 0
					fmt.Sscanf(a.Value, "%d", &n)
					sumW += n
				}
			}
		case xml.EndElement:
			depth--
		case xml.ProcInst, xml.Directive:
			t.Fatalf("processing instruction or directive in the document: %s", b)
		}
	}
	if roots != 1 || fmt.Sprint(sumW) != rootW {
		t.Fatalf("roots=%d root width %q rect sum %d: %s", roots, rootW, sumW, b)
	}
}

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func facts() RepoFacts {
	ok := freshness.Info{State: freshness.StateOK, FetchedAt: t0.Add(-time.Minute), TTL: 5 * time.Minute}
	return RepoFacts{
		Stats:      freshness.Value[scm.RepoStats]{V: scm.RepoStats{OpenIssues: 12, License: "Apache-2.0", Release: &scm.Release{Tag: "v2.4.1"}}, Info: ok},
		LastCommit: freshness.Value[time.Time]{V: t0.Add(-3 * 24 * time.Hour), Info: ok},
		Language:   freshness.Value[string]{V: "Java", Info: ok},
		Build:      &Fact{Message: "failing", Tone: ToneDanger},
		Security:   &Fact{Message: "2 high", Tone: ToneDanger},
	}
}

func TestGoldenSVG(t *testing.T) {
	for _, pal := range []struct {
		name string
		p    Palette
	}{{"aa", PaletteAA}, {"aaa", PaletteAAA}} {
		for _, k := range []Kind{KindIssues, KindLastCommit, KindRelease, KindLicence, KindLanguage, KindBuild, KindSecurity} {
			sp := Evaluate(k, facts(), t0)
			b := Badge{sp.Label, sp.Message, pal.p.Color(ToneLabel), pal.p.Color(sp.Tone)}
			golden(t, fmt.Sprintf("%s-%s.svg", pal.name, k), Render(b))
		}
	}
	// states and hostile text
	p := PaletteAA
	un := unknown(KindIssues)
	golden(t, "aa-unknown.svg", Render(Badge{un.Label, un.Message, p.Color(ToneLabel), p.Color(un.Tone)}))
	rp := EvaluateRepos(1234)
	golden(t, "aa-repos.svg", Render(Badge{rp.Label, rp.Message, p.Color(ToneLabel), p.Color(rp.Tone)}))
	golden(t, "aa-hostile.svg", Render(Badge{"issues", "<script>alert(1)</script>&\"'‮\x00end", p.Color(ToneLabel), p.Color(ToneDanger)}))
	golden(t, "aa-nonascii.svg", Render(Badge{"release", "æøå-ÆØÅ 日本語", p.Color(ToneLabel), p.Color(ToneInfo)}))
	golden(t, "aa-long.svg", Render(Badge{"release", strings.Repeat("v1.2.3-rc.", 10), p.Color(ToneLabel), p.Color(ToneWarning)}))
}

func FuzzRenderWellFormed(f *testing.F) {
	for _, s := range []string{"", "plain", "<&>\"'", "\x00\x01", "\xff\xfe", "\U0001F600", "a‮b", strings.Repeat("x", 500)} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, label, msg string) {
		out := Render(Badge{label, msg, PaletteAA.Color(ToneLabel), PaletteAA.Color(ToneInfo)})
		assertWellFormed(t, out)
		if bytes.Contains(out, []byte("<script")) {
			t.Fatal("markup leaked into the document")
		}
	})
}

func TestPaletteContrast(t *testing.T) {
	for _, pal := range []struct {
		p   Palette
		min float64
	}{{PaletteAA, 4.5}, {PaletteAAA, 7.0}} {
		for r := ToneLabel; r <= ToneNeutral; r++ {
			if c := contrast(pal.p.Color(r)); c < pal.min {
				t.Errorf("palette %d role %d %s: contrast %.2f < %.1f", pal.p, r, pal.p.Color(r), c, pal.min)
			}
		}
	}
}

// contrast is the WCAG 2.x contrast ratio of white text on c.
func contrast(c Color) float64 {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
	return 1.05 / (l + 0.05)
}

func TestWidthTableGolden(t *testing.T) {
	// pins the generated table: regenerating from another font or size cannot silently change badge widths
	for s, want := range map[string]int{
		"issues": 33, "12 open": 45, "last commit": 65, "licence": 39, "Apache-2.0": 63, "release": 41, "v1.2.3": 35,
		"passing": 43, "failing": 33, "unknown": 49, "none": 27, "today": 31, "yesterday": 55, "3 days ago": 61, "æøå ÆØÅ": 55,
		"日本語": 33, "": 1, " ": 3, "W": 11, "i": 3, "0": 7, "internal deps": 73, "1.2k open": 55, "MIT": 19, "GPL-3.0-only": 69,
		"build tool": 51, "security": 43, "language": 51, "version": 41, "repos": 31,
	} {
		if got := textWidth(s); got != want {
			t.Errorf("textWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestFormatCountAndRelativeDay(t *testing.T) {
	for n, want := range map[int]string{-5: "0", 0: "0", 999: "999", 1000: "1k", 1049: "1k", 1050: "1k", 1100: "1.1k", 12345: "12k", 999_999: "999k", 1_000_000: "1M", 1_234_567: "1.2M"} {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q want %q", n, got, want)
		}
	}
	now := time.Date(2026, 10, 14, 0, 5, 0, 0, time.UTC) // five minutes past midnight
	for ago, want := range map[time.Duration]string{
		-time.Hour: "today", 0: "today", 10 * time.Minute: "yesterday", // 23:55 the day before is "yesterday", not "today"
		25 * time.Hour: "2 days ago", 49 * time.Hour: "3 days ago", 13 * 24 * time.Hour: "13 days ago", 14 * 24 * time.Hour: "2 weeks ago",
		59 * 24 * time.Hour: "8 weeks ago", 60 * 24 * time.Hour: "2 months ago", 729 * 24 * time.Hour: "24 months ago", 730 * 24 * time.Hour: "2 years ago",
	} {
		if got, _ := relativeDay(now, now.Add(-ago)); got != want {
			t.Errorf("relativeDay(-%s) = %q want %q", ago, got, want)
		}
	}
}

func TestEvaluateStates(t *testing.T) {
	now := t0
	f := facts()
	stale := f
	stale.Build = &Fact{Message: "passing", Tone: ToneNeutral, Stale: true}
	empty := RepoFacts{}
	absent := f
	absent.Stats.V.Release, absent.Stats.V.License = nil, ""
	for _, tc := range []struct {
		name string
		k    Kind
		f    RepoFacts
		want string
		st   State
	}{
		{"issues", KindIssues, f, "issues: 12 open", StateOK},
		{"no data", KindIssues, empty, "issues: unknown", StateUnknown},
		{"last commit", KindLastCommit, f, "last commit: 3 days ago", StateOK},
		{"release", KindRelease, f, "release: v2.4.1", StateOK},
		{"no release", KindRelease, absent, "release: none", StateNone},
		{"no licence", KindLicence, absent, "licence: none", StateNone},
		{"build failing", KindBuild, f, "build: failing", StateOK},
		{"build stale", KindBuild, stale, "build: passing (stale)", StateOK},
		{"security", KindSecurity, f, "security: 2 high", StateOK},
		{"language", KindLanguage, f, "language: Java", StateOK},
		{"build tool unknown", KindBuildTool, f, "build tool: unknown", StateUnknown},
	} {
		sp := Evaluate(tc.k, tc.f, now)
		if got := sp.Label + ": " + sp.Message; got != tc.want || sp.State != tc.st {
			t.Errorf("%s: got %q state %v, want %q state %v", tc.name, got, sp.State, tc.want, tc.st)
		}
	}
	// stale-while-error: an error state with a fetch time keeps showing the last good value
	e := f
	e.Stats.Info.State = freshness.StateError
	if sp := Evaluate(KindIssues, e, now); sp.Message != "12 open" {
		t.Errorf("stale-while-error lost the value: %q", sp.Message)
	}
}

// ---- handler ---------------------------------------------------------------------------------------------------

type fakeFacts struct{ m map[scm.Slug]RepoFacts }

func (f fakeFacts) Facts(_ *readmodel.Snapshot, s scm.Slug) (RepoFacts, bool) {
	v, ok := f.m[s]
	return v, ok
}

type az struct{ all bool }

func (a az) CanView(_ context.Context, _ authz.Principal, v scm.Visibility) bool {
	return v == scm.VisPublic || a.all
}

func newSvc(t *testing.T, all bool) (*Service, *http.ServeMux) {
	t.Helper()
	reg := &readmodel.Registry{}
	repos := readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	members := readmodel.RegisterGlobal[scm.Membership](reg, "catalog.membership")
	aliases := readmodel.RegisterGlobal[map[scm.Slug]scm.Slug](reg, "catalog.aliases")
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})
	if _, err := pub.Apply(func(b *readmodel.Builder) error {
		put := func(name string, vis scm.Visibility) {
			_ = readmodel.Put(b, readmodel.EntryID(name), repos, freshness.Value[scm.Repository]{V: scm.Repository{Slug: scm.Slug(name), Visibility: vis}})
		}
		put("api", scm.VisPublic)
		put("secret", scm.VisPrivate)
		put("pub2", scm.VisPublic)
		mr := func(name string, id int64, vis scm.Visibility) scm.MemberRepo {
			return scm.MemberRepo{Slug: scm.Slug(name), Ref: scm.RepoRef{Source: "s", ID: scm.RepoID(id), Owner: "o", Name: name}, Visibility: vis}
		}
		m, err := scm.NewMembership(scm.MembershipSpec{
			Repos:  []scm.MemberRepo{mr("api", 1, scm.VisPublic), mr("secret", 2, scm.VisPrivate), mr("pub2", 3, scm.VisPublic)},
			Groups: []scm.GroupSpec{{Meta: scm.GroupMeta{ID: "core"}, Members: []scm.Slug{"api", "secret", "pub2"}}},
		})
		if err != nil {
			return err
		}
		if err := readmodel.PutGlobal(b, members, m); err != nil {
			return err
		}
		return readmodel.PutGlobal(b, aliases, map[scm.Slug]scm.Slug{"old-api": "api"})
	}); err != nil {
		t.Fatal(err)
	}
	noSec := facts()
	noSec.Security = nil // no Snyk provider configured: the "security" kind does not exist in this deployment
	ff := fakeFacts{m: map[scm.Slug]RepoFacts{"api": noSec, "secret": facts(), "pub2": {}}}
	svc, err := New(Deps{Cfg: DefaultConfig(), Facts: ff, Pub: pub, Repos: repos, Members: members, Aliases: aliases, Authz: az{all},
		Enabled: []Kind{KindLanguage}, Meter: noop.NewMeterProvider().Meter("t")})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /badges/{repo}/{file}", svc.Repo())
	mux.Handle("GET /badges/groups/{groupId}/{file}", svc.Group())
	return svc, mux
}

func get(mux http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerHeadersAndConditional(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, mux := newSvc(t, false)
		rec := get(mux, "/badges/api/issues.svg")
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		want := map[string]string{
			"Content-Type": "image/svg+xml", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'; sandbox",
			"Cross-Origin-Resource-Policy": "cross-origin", "Cache-Control": "public, max-age=300",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s = %q want %q", k, got, v)
			}
		}
		etag := rec.Header().Get("ETag")
		if !strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, "W/") {
			t.Fatalf("want a strong ETag, got %q", etag)
		}
		if h := rec.Header().Get("Access-Control-Allow-Origin"); h != "" {
			t.Errorf("badges must not set CORS headers, got %q", h)
		}
		rec2 := get(mux, "/badges/api/issues.svg", "If-None-Match", etag)
		if rec2.Code != 304 || rec2.Body.Len() != 0 || rec2.Header().Get("ETag") != etag || rec2.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Fatalf("conditional: %d %q %v", rec2.Code, rec2.Body.String(), rec2.Header())
		}
	})
}

// The text of a relative date changes at midnight UTC without any data change: the ETag follows the body.
func TestETagFollowsBodyAcrossMidnight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) { // the bubble clock starts at 2000-01-01 00:00:00 UTC
		svc, mux := newSvc(t, false)
		ff := svc.d.Facts.(fakeFacts)
		f := ff.m["api"]
		f.LastCommit = freshness.Value[time.Time]{V: time.Now(), Info: freshness.Info{State: freshness.StateOK, FetchedAt: time.Now()}}
		ff.m["api"] = f
		first := get(mux, "/badges/api/last-commit.svg")
		if !strings.Contains(first.Body.String(), ">today<") {
			t.Fatal(first.Body.String())
		}
		time.Sleep(24 * time.Hour)
		again := get(mux, "/badges/api/last-commit.svg", "If-None-Match", first.Header().Get("ETag"))
		if again.Code != 200 || !strings.Contains(again.Body.String(), ">yesterday<") || again.Header().Get("ETag") == first.Header().Get("ETag") {
			t.Fatalf("a day later: %d %s", again.Code, again.Body.String())
		}
	})
}

func TestHandlerUnknownAndHidden(t *testing.T) {
	_, anon := newSvc(t, false)
	_, priv := newSvc(t, true)
	missing := get(anon, "/badges/does-not-exist/issues.svg")
	hidden := get(anon, "/badges/secret/issues.svg")
	if missing.Code != 404 || hidden.Code != 404 || missing.Body.String() != hidden.Body.String() || missing.Header().Get("Cache-Control") != hidden.Header().Get("Cache-Control") {
		t.Fatalf("hidden repository distinguishable from a missing one: %d/%d", missing.Code, hidden.Code)
	}
	if c := missing.Header().Get("Cache-Control"); c != "public, max-age=60" {
		t.Errorf("404 cache: %q", c)
	}
	if rec := get(priv, "/badges/secret/issues.svg"); rec.Code != 200 || rec.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Errorf("authorised view of a private repo: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for path, code := range map[string]int{
		"/badges/api/nope.svg": 404, "/badges/api/issues": 404, "/badges/api/issues.SVG": 404, "/badges/api/security.svg": 404, // no security fact: provider not configured
		"/badges/api/build.svg": 200, "/badges/api/license.svg": 200, "/badges/api/licence.svg": 200, "/badges/api/repos.svg": 404,
		"/badges/old-api/issues.svg": 200, "/badges/pub2/issues.svg": 200,
	} {
		if rec := get(anon, path); rec.Code != code {
			t.Errorf("%s: %d want %d", path, rec.Code, code)
		}
	}
	unknown := get(anon, "/badges/pub2/issues.svg") // exists, no facts
	if !strings.Contains(unknown.Body.String(), ">unknown<") || unknown.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("known repo without data: %q %q", unknown.Body.String(), unknown.Header().Get("Cache-Control"))
	}
}

func TestHandlerGroup(t *testing.T) {
	_, mux := newSvc(t, false)
	rec := get(mux, "/badges/groups/core/repos.svg")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), ">2<") { // api and pub2; the private repo is not counted for anonymous
		t.Errorf("group badge: %d %s", rec.Code, rec.Body.String())
	}
	_, priv := newSvc(t, true)
	if rec := get(priv, "/badges/groups/core/repos.svg"); !strings.Contains(rec.Body.String(), ">3<") || rec.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Errorf("group badge for an authorised principal: %s %q", rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	if rec := get(mux, "/badges/groups/nope/repos.svg"); rec.Code != 404 {
		t.Errorf("unknown group: %d", rec.Code)
	}
	if rec := get(mux, "/badges/groups/core/issues.svg"); rec.Code != 404 {
		t.Errorf("group form supports repos only: %d", rec.Code)
	}
}

func BenchmarkRender(b *testing.B) {
	bd := Badge{"issues", "12 open", PaletteAA.Color(ToneLabel), PaletteAA.Color(ToneInfo)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Render(bd)
	}
}
