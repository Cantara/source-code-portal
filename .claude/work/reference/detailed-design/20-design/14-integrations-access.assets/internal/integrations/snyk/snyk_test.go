package snyk_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/integrations"
	"github.com/cantara/source-code-portal/internal/integrations/snyk"
	"github.com/cantara/source-code-portal/internal/platform/secret"
)

const org = "4a18d42f-0706-4ad0-b127-24078731fbed"

func fx(t *testing.T, n string) []byte {
	t.Helper()
	b, err := os.ReadFile("../testdata/snyk/" + n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newProvider(t *testing.T, h http.HandlerFunc, mut func(*integrations.SnykConfig)) (*snyk.Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	cfg := integrations.DefaultConfig().Snyk
	cfg.Enabled, cfg.OrgID, cfg.Token, cfg.BaseURL = true, org, secret.New("snyk-token"), srv.URL
	cfg.Timeout = 5 * time.Second
	if mut != nil {
		mut(&cfg)
	}
	p, err := snyk.New(cfg, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func vnd(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.Write(b)
}

func tg(name string) integrations.Target {
	return integrations.Target{Project: "acme/" + name}
}

func TestBulkPagingAndMapping(t *testing.T) {
	var queries []string
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Token snyk-token" || r.URL.Path != "/rest/orgs/"+org+"/projects" {
			t.Errorf("bad request: %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("starting_after") == "" {
			vnd(w, fx(t, "projects-page1.json"))
			return
		}
		vnd(w, fx(t, "projects-page2.json"))
	}, nil)
	rs, err := p.FetchBulk(context.Background(), []integrations.Target{tg("alpha"), tg("beta"), tg("gamma"), tg("old"), tg("missing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "meta.latest_issue_counts=true") || !strings.Contains(queries[0], "expand=target") || !strings.Contains(queries[0], "version=2024-10-15") {
		t.Fatalf("queries = %v", queries)
	}
	// alpha: two active projects summed: critical 1, high 2, medium 1, low 4 -> failing at fail_on=high
	a := rs[0].Status
	if rs[0].Err != nil || a.State != integrations.StateFailing || a.Counts["critical"] != 1 || a.Counts["high"] != 2 || a.Counts["low"] != 4 || a.Summary != "1 critical, 2 high, 1 medium, 4 low" {
		t.Fatalf("alpha = %+v err=%v", a, rs[0].Err)
	}
	if b := rs[1].Status; rs[1].Err != nil || b.State != integrations.StateOK || b.Summary != "no known issues" { // case-insensitive match acme/Beta
		t.Fatalf("beta = %+v err=%v", b, rs[1].Err)
	}
	if g := rs[2].Status; g.State != integrations.StateUnstable || g.Summary != "3 medium" {
		t.Fatalf("gamma = %+v", g)
	}
	for _, i := range []int{3, 4} { // inactive-only project and unknown repo: no data
		if !errors.Is(rs[i].Err, integrations.ErrNoData) {
			t.Errorf("target %d err = %v", i, rs[i].Err)
		}
	}
}

func TestFailOnThreshold(t *testing.T) {
	for _, tc := range []struct {
		failOn string
		want   integrations.State
	}{{"critical", integrations.StateFailing}, {"high", integrations.StateFailing}, {"medium", integrations.StateFailing}, {"low", integrations.StateFailing}} {
		// alpha has a critical issue so every threshold fails; gamma has only medium
		p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("starting_after") == "" {
				vnd(w, fx(t, "projects-page1.json"))
				return
			}
			vnd(w, fx(t, "projects-page2.json"))
		}, func(c *integrations.SnykConfig) { c.FailOn = tc.failOn })
		rs, err := p.FetchBulk(context.Background(), []integrations.Target{tg("alpha"), tg("gamma")})
		if err != nil || rs[0].Status.State != tc.want {
			t.Fatalf("%s: %v %+v", tc.failOn, err, rs[0])
		}
		wantGamma := integrations.StateUnstable
		if tc.failOn == "medium" || tc.failOn == "low" {
			wantGamma = integrations.StateFailing
		}
		if rs[1].Status.State != wantGamma {
			t.Errorf("fail_on=%s: gamma = %v, want %v", tc.failOn, rs[1].Status.State, wantGamma)
		}
	}
}

// links.next pointing at another host must never be followed; only the opaque cursor is used.
func TestNextLinkNeverFollowed(t *testing.T) {
	var evilHits int
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { evilHits++ }))
	defer evil.Close()
	var calls int
	var cursors []string
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		cursors = append(cursors, r.URL.Query().Get("starting_after"))
		if calls == 1 {
			vnd(w, []byte(`{"jsonapi":{"version":"1.0"},"links":{"next":{"href":"`+evil.URL+`/steal?starting_after=CUR1"}},"data":[]}`))
			return
		}
		vnd(w, []byte(`{"jsonapi":{"version":"1.0"},"links":{},"data":[]}`))
	}, nil)
	if _, err := p.FetchBulk(context.Background(), []integrations.Target{tg("alpha")}); err != nil {
		t.Fatal(err)
	}
	if evilHits != 0 || len(cursors) != 2 || cursors[1] != "CUR1" {
		t.Fatalf("evilHits=%d cursors=%v", evilHits, cursors)
	}
}

func TestPagingLoopAndCap(t *testing.T) {
	// the same cursor twice terminates; an endless stream of fresh cursors stops at the page cap
	n := 0
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		vnd(w, []byte(`{"jsonapi":{"version":"1.0"},"links":{"next":"/x?starting_after=`+strings.Repeat("a", 1+n)+`"},"data":[]}`))
	}, nil)
	_, err := p.FetchBulk(context.Background(), []integrations.Target{tg("alpha")})
	if !errors.Is(err, integrations.ErrProtocol) || n != 200 {
		t.Fatalf("err=%v pages=%d", err, n)
	}
	m := 0
	p2, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		m++
		vnd(w, []byte(`{"jsonapi":{"version":"1.0"},"links":{"next":"/x?starting_after=same"},"data":[]}`))
	}, nil)
	if _, err := p2.FetchBulk(context.Background(), []integrations.Target{tg("alpha")}); err != nil || m != 2 {
		t.Fatalf("err=%v pages=%d", err, m)
	}
}

func TestErrors(t *testing.T) {
	status := func(code int, hdr map[string]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for k, v := range hdr {
				w.Header().Set(k, v)
			}
			w.WriteHeader(code)
		}
	}
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		code string
	}{
		{"401", status(401, nil), "auth"},
		{"403", status(403, nil), "auth"},
		{"404 org", status(404, nil), "config"},
		{"400 version", status(400, nil), "config"},
		{"429", status(429, map[string]string{"Retry-After": "30"}), "rate_limited"},
		{"429 without Retry-After", status(429, nil), "rate_limited"},
		{"503", status(503, nil), "transient"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProvider(t, tc.h, nil)
			_, err := p.FetchBulk(context.Background(), []integrations.Target{tg("alpha")})
			if got := integrations.ErrCode(err); got != tc.code {
				t.Fatalf("code = %q (%v), want %q", got, err, tc.code)
			}
			if tc.code == "auth" && strings.Contains(err.Error(), "snyk-token") {
				t.Fatal("token leaked into error text")
			}
		})
	}
}

func TestDetailURLTemplate(t *testing.T) {
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("starting_after") == "" {
			vnd(w, fx(t, "projects-page1.json"))
			return
		}
		vnd(w, fx(t, "projects-page2.json"))
	}, func(c *integrations.SnykConfig) {
		c.DetailURLTemplate = "https://app.snyk.io/org/acme/project/{project_id}?repo={owner}/{repo}"
	})
	tt := tg("alpha")
	tt.Repo.Owner, tt.Repo.Name = "acme", "alpha"
	rs, _ := p.FetchBulk(context.Background(), []integrations.Target{tt})
	if want := "https://app.snyk.io/org/acme/project/11111111-1111-1111-1111-111111111111?repo=acme/alpha"; rs[0].Status.DetailURL != want {
		t.Fatalf("DetailURL = %q", rs[0].Status.DetailURL)
	}
}

func TestOversizedResponse(t *testing.T) {
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		vnd(w, []byte(`{"data":[],"x":"`+strings.Repeat("a", 5000)+`"}`))
	}, func(c *integrations.SnykConfig) { c.MaxResponseBytes = 1024 })
	if _, err := p.FetchBulk(context.Background(), []integrations.Target{tg("a")}); !errors.Is(err, integrations.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheck(t *testing.T) {
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		vnd(w, []byte(`{"jsonapi":{"version":"1.0"},"links":{},"data":[]}`))
	}, nil)
	if rs := p.Check(context.Background()); len(rs) != 1 || rs[0].Err != nil {
		t.Fatalf("%+v", rs)
	}
	p2, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }, nil)
	if rs := p2.Check(context.Background()); integrations.ErrCode(rs[0].Err) != "auth" {
		t.Fatalf("%+v", rs)
	}
}

// A count above the cap saturates; it must never wrap to 0, which would read as "no known issues" (fail open).
func TestHugeCountSaturates(t *testing.T) {
	body := `{"data":[{"id":"11111111-1111-1111-1111-111111111111","attributes":{"name":"x","status":"active"},
	"meta":{"latest_issue_counts":{"critical":1e12,"high":0,"medium":0,"low":-5}},
	"relationships":{"target":{"data":{"attributes":{"display_name":"acme/big"}}}}}],"links":{}}`
	p, _ := newProvider(t, func(w http.ResponseWriter, r *http.Request) { vnd(w, []byte(body)) }, nil)
	st, err := p.Fetch(context.Background(), tg("big"))
	if err != nil || st.State != integrations.StateFailing || st.Counts["critical"] != integrations.MaxCount || st.Counts["low"] != 0 {
		t.Fatalf("%+v %v", st, err)
	}
}
