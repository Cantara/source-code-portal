package jenkins_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/integrations"
	"github.com/cantara/source-code-portal/internal/integrations/jenkins"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../testdata/jenkins/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type rec struct {
	mu    sync.Mutex
	paths []string
	auth  []string
}

func (r *rec) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, req.URL.EscapedPath())
	r.auth = append(r.auth, req.Header.Get("Authorization"))
}

func setup(t *testing.T, h http.HandlerFunc, mut func(*integrations.JenkinsInstance)) (*jenkins.Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	cfg := integrations.JenkinsInstance{Name: "main", BaseURL: srv.URL + "/ci", User: "bot", Token: secret.New("tok"), Discover: "declared", Multibranch: "auto",
		Timeout: 5 * time.Second, MaxResponseBytes: 4096, ExposeDetailURL: true}
	if mut != nil {
		mut(&cfg)
	}
	p, err := jenkins.New([]integrations.JenkinsInstance{cfg}, map[string]*http.Client{"main": srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func serveJSON(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.Write(body)
	}
}

func target(job, branch string) integrations.Target {
	return integrations.Target{Repo: scm.RepoRef{Owner: "acme", Name: "alpha"}, Instance: "main", Job: job, Branch: branch}
}

func TestFetchMapping(t *testing.T) {
	tests := []struct {
		fixture   string
		wantState integrations.State
		wantSum   string
		wantURL   string
	}{
		{"freestyle-success.json", integrations.StateOK, "build #42 passed", "/ci/job/team/job/alpha/42/"},
		{"freestyle-failure-running.json", integrations.StateFailing, "build #7 failed (build #8 running)", "/ci/job/team/job/alpha/7/"},
		{"freestyle-unstable.json", integrations.StateUnstable, "build #3 unstable", "/ci/job/team/job/alpha/3/"},
		{"aborted.json", integrations.StateUnknown, "build #5 aborted", "/ci/job/team/job/alpha/5/"},
		{"disabled.json", integrations.StateUnknown, "job disabled", ""},
		{"never-built.json", integrations.StateUnknown, "no completed builds", ""},
		{"folder.json", integrations.StateUnknown, "job is a folder", ""},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			p, srv := setup(t, serveJSON(fixture(t, tc.fixture)), nil)
			st, err := p.Fetch(context.Background(), target("team/alpha", "main"))
			if err != nil {
				t.Fatal(err)
			}
			if st.State != tc.wantState || st.Summary != tc.wantSum {
				t.Fatalf("got %v %q, want %v %q", st.State, st.Summary, tc.wantState, tc.wantSum)
			}
			wantURL := ""
			if tc.wantURL != "" {
				wantURL = srv.URL + tc.wantURL
			}
			if st.DetailURL != wantURL {
				t.Fatalf("DetailURL = %q, want %q", st.DetailURL, wantURL)
			}
		})
	}
}

func TestDetailURLHiddenByDefault(t *testing.T) {
	p, _ := setup(t, serveJSON(fixture(t, "freestyle-success.json")), func(c *integrations.JenkinsInstance) { c.ExposeDetailURL = false })
	st, _ := p.Fetch(context.Background(), target("alpha", ""))
	if st.DetailURL != "" {
		t.Fatalf("DetailURL = %q", st.DetailURL)
	}
}

func TestRequestShapeAndAuth(t *testing.T) {
	var r rec
	var query string
	p, _ := setup(t, func(w http.ResponseWriter, req *http.Request) {
		r.add(req)
		query = req.URL.Query().Get("tree")
		serveJSON(fixture(t, "freestyle-success.json"))(w, req)
	}, nil)
	if _, err := p.Fetch(context.Background(), target("my team/alpha.v2", "")); err != nil {
		t.Fatal(err)
	}
	if got := r.paths[0]; got != "/ci/job/my%20team/job/alpha.v2/api/json" {
		t.Errorf("path = %q", got)
	}
	if !strings.HasPrefix(r.auth[0], "Basic ") {
		t.Errorf("Authorization = %q", r.auth[0])
	}
	if !strings.Contains(query, "lastCompletedBuild[number,result") {
		t.Errorf("tree = %q", query)
	}
}

func TestMultibranch(t *testing.T) {
	var r rec
	p, _ := setup(t, func(w http.ResponseWriter, req *http.Request) {
		r.add(req)
		if strings.HasSuffix(req.URL.EscapedPath(), "/job/alpha/api/json") {
			serveJSON(fixture(t, "multibranch-parent.json"))(w, req)
			return
		}
		serveJSON(fixture(t, "multibranch-branch.json"))(w, req)
	}, nil)
	st, err := p.Fetch(context.Background(), target("alpha", "feature/x y"))
	if err != nil {
		t.Fatal(err)
	}
	if st.State != integrations.StateOK || st.Summary != "build #19 passed" {
		t.Fatalf("status = %+v", st)
	}
	if len(r.paths) != 2 || r.paths[1] != "/ci/job/alpha/job/feature%252Fx%2520y/api/json" {
		t.Fatalf("paths = %v", r.paths)
	}
	if !strings.Contains(st.DetailURL, "/job/feature%252Fx%2520y/19/") {
		t.Errorf("DetailURL = %q", st.DetailURL)
	}
}

func TestMultibranchMissingBranch(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.EscapedPath(), "/job/alpha/api/json") {
			serveJSON(fixture(t, "multibranch-parent.json"))(w, req)
			return
		}
		http.NotFound(w, req)
	}, nil)
	st, err := p.Fetch(context.Background(), target("alpha", "main"))
	if err != nil || st.Summary != "branch not built" {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name    string
		h       http.HandlerFunc
		check   func(error) bool
		wantMsg string
	}{
		{"404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, func(e error) bool { return errors.Is(e, scm.ErrNotFound) }, "not found"},
		{"401 is permanent auth", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }, func(e error) bool {
			var pe *integrations.PermanentError
			return errors.As(e, &pe) && pe.Code == "auth"
		}, "auth"},
		// 403 is per job on Jenkins (folder/matrix permissions): a per-target result, not an instance-wide permanent error
		{"403 is per target forbidden", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, func(e error) bool {
			var pe *integrations.PermanentError
			return errors.Is(e, scm.ErrForbidden) && !errors.As(e, &pe) && integrations.ErrCode(e) == "forbidden"
		}, "forbidden"},
		{"redirect to /login is permanent auth", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/login") {
				w.Header().Set("Content-Type", "text/html")
				w.Write(fixture(t, "login-page.html"))
				return
			}
			http.Redirect(w, r, "/ci/login?from="+r.URL.Path, http.StatusFound)
		}, func(e error) bool {
			var pe *integrations.PermanentError
			return errors.As(e, &pe) && pe.Code == "auth"
		}, "auth"},
		{"429 is rate limited", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) }, func(e error) bool {
			var rl *integrations.RateLimitError
			return errors.As(e, &rl) && time.Until(rl.RetryAt()) > 100*time.Second
		}, "rate"},
		{"500 is transient", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, func(e error) bool {
			var rl *integrations.RateLimitError
			var pe *integrations.PermanentError
			return e != nil && !errors.As(e, &rl) && !errors.As(e, &pe) && integrations.ErrCode(e) == "transient"
		}, "transient"},
		{"html with 200 is protocol", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write(fixture(t, "login-page.html"))
		}, func(e error) bool { return errors.Is(e, integrations.ErrProtocol) }, "protocol"},
		{"oversized", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_class":"` + strings.Repeat("a", 10000) + `"}`))
		}, func(e error) bool { return errors.Is(e, integrations.ErrTooLarge) }, "too_large"},
		{"bad json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{"))
		},
			func(e error) bool { return errors.Is(e, integrations.ErrProtocol) }, "protocol"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := setup(t, tc.h, nil)
			_, err := p.Fetch(context.Background(), target("alpha", ""))
			if err == nil || !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
			if got := integrations.ErrCode(err); !strings.Contains(got, tc.wantMsg) && tc.wantMsg != "transient" && tc.wantMsg != "rate" {
				t.Logf("code=%s", got)
			}
		})
	}
}

// A redirect to another host is not followed and the credentials never reach it.
func TestRedirectToOtherHostIsPinned(t *testing.T) {
	var hits int
	var gotAuth string
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer evil.Close()
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, evil.URL+"/steal", http.StatusFound) }, nil)
	_, err := p.Fetch(context.Background(), target("alpha", ""))
	if err == nil {
		t.Fatal("expected an error: the 302 response has no JSON body")
	}
	if hits != 0 || gotAuth != "" {
		t.Fatalf("redirect was followed: hits=%d auth=%q", hits, gotAuth)
	}
}

func TestRedirectSameHostFollowed(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/") && strings.HasSuffix(r.URL.Path, "/final") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
			return
		}
		serveJSON(fixture(t, "freestyle-success.json"))(w, r)
	}, nil)
	if _, err := p.Fetch(context.Background(), target("final", "")); err != nil {
		t.Fatal(err)
	}
}

func TestDotBranchIsNotRequested(t *testing.T) {
	var paths []string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		serveJSON(fixture(t, "multibranch-parent.json"))(w, r)
	}, nil)
	for _, b := range []string{".", ".."} {
		st, err := p.Fetch(context.Background(), target("alpha", b))
		if err != nil || st.Summary != "no branch to look up" {
			t.Fatalf("%q: %+v %v", b, st, err)
		}
	}
	if len(paths) != 2 { // only the parent lookups, never a /job/.. request
		t.Fatalf("paths = %v", paths)
	}
}

func TestBadTargetsRejected(t *testing.T) {
	p, _ := setup(t, serveJSON(fixture(t, "freestyle-success.json")), nil)
	for _, job := range []string{"../etc", "a//b", "a/./b", "a%2Fb", "", "a b/ c"} {
		_, err := p.Fetch(context.Background(), target(job, ""))
		var pe *integrations.PermanentError
		if !errors.As(err, &pe) {
			t.Errorf("job %q: err = %v", job, err)
		}
	}
	_, err := p.Fetch(context.Background(), integrations.Target{Instance: "nope", Job: "a"})
	var pe *integrations.PermanentError
	if !errors.As(err, &pe) {
		t.Errorf("unknown instance: %v", err)
	}
}

func TestCheck(t *testing.T) {
	var path, tree string
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		path, tree = r.URL.Path, r.URL.Query().Get("tree")
		serveJSON([]byte(`{"_class":"hudson.model.Hudson"}`))(w, r)
	}, nil)
	rs := p.Check(context.Background())
	if len(rs) != 1 || rs[0].Err != nil || rs[0].Instance != "main" || path != "/ci/api/json" || tree != "_class" {
		t.Fatalf("rs=%+v path=%q tree=%q", rs, path, tree)
	}
	p2, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, nil)
	if rs := p2.Check(context.Background()); integrations.ErrCode(rs[0].Err) != "auth" {
		t.Fatalf("rs=%+v", rs)
	}
}
