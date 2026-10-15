package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/cluster"
	"github.com/cantara/source-code-portal/internal/cluster/clustertest"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// TestCluster is the M9 acceptance test: two instances on one cluster make each GitHub call once, share webhook de-duplication and
// serve the same data; when the leader stops, the other takes over. It runs on the in-memory cluster, and on a real Hazelcast when
// TEST_HAZELCAST is set.
func TestCluster(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		hub := clustertest.NewHub()
		testCluster(t, func() func(context.Context) (cluster.Backend, error) { return hub.Dial() })
	})
	t.Run("hazelcast", func(t *testing.T) {
		addr := os.Getenv("TEST_HAZELCAST")
		if addr == "" {
			t.Skip("TEST_HAZELCAST is not set")
		}
		ns := fmt.Sprintf("scptest-%d", time.Now().UnixNano())
		testCluster(t, func() func(context.Context) (cluster.Backend, error) {
			return cluster.DialHazelcast([]string{addr}, "dev", ns, slog.New(slog.DiscardHandler))
		})
	})
}

func testCluster(t *testing.T, dial func() func(context.Context) (cluster.Backend, error)) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	commit := func(c byte, h int) githubtest.Commit {
		return githubtest.Commit{SHA: strings.Repeat(string(c), 40), Message: "change " + string(c), AuthorName: "Ann", Date: at.Add(time.Duration(h) * time.Hour)}
	}
	core := gh.AddRepo("acme", gogh.Repository{Name: new("core"), Visibility: new("public")})
	web := gh.AddRepo("acme", gogh.Repository{Name: new("web"), Visibility: new("public")})
	gh.SetCommits(core, commit('a', 1))
	gh.SetCommits(web, commit('b', 2))
	gh.SetFile(core, "README.md", "# Core\n")
	gh.SetFile(core, "go.mod", "module github.com/acme/core\n\ngo 1.27\n")
	gh.SetFile(web, "go.mod", "module github.com/acme/web\n\ngo 1.27\n\nrequire github.com/acme/core v1.0.0\n")
	gh.SetRelease(core, "v1.0.0", "One", at)

	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	token, secret := write("token", "ghp_test"), write("hook", "hooks")
	instance := func(name string) config.Config {
		path := write(name+".yaml", `store: {path: `+filepath.Join(dir, name, "scp.db")+`}
cluster: {addresses: ["127.0.0.1:5701"]}
webhook: {secret_file: `+secret+`}
github:
  sources:
    - {id: gh, base_url: "`+gh.BaseURL()+`", orgs: [acme], token_file: `+token+`}
catalog:
  groups:
    - {id: all, name: All, default_repo: core, match: [{catch_all: true}]}
`)
		cfg, _, err := config.Load(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
		return cfg
	}
	type node struct {
		public, admin string
		stop          func()
	}
	start := func(name string) *node {
		srv, err := newServer(t.Context(), instance(name), deps{cluster: dial()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- srv.run(ctx) }()
		stopped := false
		n := &node{public: "http://" + srv.publicLn.Addr().String(), admin: "http://" + srv.adminLn.Addr().String()}
		n.stop = func() {
			if stopped {
				return
			}
			stopped = true
			cancel()
			if err := <-done; err != nil {
				t.Errorf("run %s: %v", name, err)
			}
		}
		t.Cleanup(n.stop)
		return n
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	get := func(url string) (string, string) {
		t.Helper()
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Header.Get("ETag")
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 20s", what)
			}
		}
	}
	type clusterStatus struct {
		Cluster statusCluster `json:"cluster"`
	}
	role := func(n *node) statusCluster {
		var st clusterStatus
		b, _ := get(n.admin + "/api/v1/status")
		_ = json.Unmarshal([]byte(b), &st)
		return st.Cluster
	}
	commits := func(n *node, repo string) int {
		var d oapi.RepoDetail
		b, _ := get(n.public + "/api/v1/repos/acme/" + repo)
		if json.Unmarshal([]byte(b), &d) != nil {
			return 0
		}
		return len(d.Commits)
	}
	loaded := func(n *node) bool {
		var d oapi.Dashboard
		b, _ := get(n.public + "/api/v1/dashboard")
		return json.Unmarshal([]byte(b), &d) == nil && d.Freshness.Fresh == 2 && len(d.Latest) == 2
	}
	push := func(n *node, delivery, name string) string {
		t.Helper()
		body := `{"ref":"refs/heads/main","repository":{"name":"` + name + `","default_branch":"main","owner":{"login":"acme"}}}`
		m := hmac.New(sha256.New, []byte("hooks"))
		m.Write([]byte(body))
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, n.public+"/webhooks/github", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", delivery)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(m.Sum(nil)))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var r struct{ Status string }
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return r.Status
	}

	a, b := start("a"), start("b")
	waitFor("one leader", func() bool { return role(a).Leader != role(b).Leader })
	leader, follower := a, b
	if role(b).Leader {
		leader, follower = b, a
	}
	waitFor("both instances loaded", func() bool { return loaded(leader) && loaded(follower) })

	// Each GitHub call once: the cold start of two instances is the ledger of one.
	ledger := gh.Ledger()
	seen := map[string]int{}
	for _, l := range ledger {
		seen[l]++
	}
	for l, n := range seen {
		if n > 1 {
			t.Errorf("GitHub call made %d times: %s", n, l)
		}
	}

	// The same data: equal bodies and ETags on both instances, read between two equal versions of both, since the leader may still be
	// publishing what its last jobs read.
	paths := []string{"/api/v1/dashboard", "/api/v1/repos", "/api/v1/groups/all", "/api/v1/repos/acme/core", "/api/v1/repos/acme/core/docs",
		"/api/v1/repos/acme/web/manifests", "/api/v1/commits", "/llms.txt", "/repos/acme/core/readme.md", "/badges/repos/acme/core/release.svg"}
	version := func(n *node) int64 {
		var info oapi.SnapshotInfo
		b, _ := get(n.public + "/api/v1/snapshot")
		_ = json.Unmarshal([]byte(b), &info)
		return info.Version
	}
	var differs string
	same := func() bool {
		v := version(leader)
		if v == 0 || version(follower) != v {
			differs = "the snapshot version"
			return false
		}
		for _, p := range paths {
			lb, le := get(leader.public + p)
			fb, fe := get(follower.public + p)
			if lb != fb || le != fe || le == "" {
				differs = fmt.Sprintf("%s:\nleader   %s\nfollower %s", p, lb, fb)
				return false
			}
		}
		return version(leader) == v && version(follower) == v
	}
	for deadline := time.Now().Add(20 * time.Second); !same(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the instances differ after 20s in %s", differs)
		}
	}

	// A push delivered to the follower is run once by the leader and shows on both; its redelivery to the leader is a duplicate.
	fetches := gh.Count("GET /repos/acme/web/commits")
	gh.SetCommits(web, commit('c', 3), commit('b', 2))
	if st := push(follower, "push-1", "web"); st != "accepted" {
		t.Fatalf("push to the follower: %s", st)
	}
	if st := push(leader, "push-1", "web"); st != "duplicate" {
		t.Errorf("redelivery to the leader: %s, want duplicate", st)
	}
	waitFor("the pushed commit on both", func() bool { return commits(leader, "web") == 2 && commits(follower, "web") == 2 })
	if got := gh.Count("GET /repos/acme/web/commits") - fetches; got != 1 {
		t.Errorf("the push fetched commits %d times, want once", got)
	}

	// The leader stops: the other takes over and keeps the data fresh.
	leader.stop()
	waitFor("the other instance leads", func() bool { return role(follower).Leader })
	gh.SetCommits(core, commit('d', 4), commit('a', 1))
	if st := push(follower, "push-2", "core"); st != "accepted" {
		t.Fatalf("push after failover: %s", st)
	}
	waitFor("the pushed commit after failover", func() bool { return commits(follower, "core") == 2 })
	if st := role(follower); !st.Connected || !st.Enabled {
		t.Errorf("status after failover: %+v", st)
	}
	if b, _ := get(follower.admin + "/metrics"); !strings.Contains(b, "scp_cluster_leader 1") {
		t.Errorf("metrics lack scp_cluster_leader 1")
	}
}
