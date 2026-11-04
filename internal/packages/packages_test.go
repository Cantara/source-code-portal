package packages

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// registry is a fake registry host: answers by path, a ledger of the paths asked.
type registry struct {
	*httptest.Server
	mu      gosync.Mutex
	answers map[string]string // path -> body; a missing path answers 404
	status  map[string]int    // path -> a status instead
	asked   []string
	etag    bool
}

func newRegistry(t *testing.T) *registry {
	t.Helper()
	r := &registry{answers: map[string]string{}, status: map[string]int{}}
	r.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.asked = append(r.asked, req.URL.EscapedPath()+" "+req.Header.Get("If-None-Match"))
		body, ok := r.answers[req.URL.EscapedPath()]
		status := r.status[req.URL.EscapedPath()]
		etag := r.etag
		r.mu.Unlock()
		switch {
		case status != 0:
			w.WriteHeader(status)
		case !ok:
			http.NotFound(w, req)
		case etag && req.Header.Get("If-None-Match") == `"v1"`:
			w.WriteHeader(http.StatusNotModified)
		default:
			if etag {
				w.Header().Set("ETag", `"v1"`)
			}
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *registry) set(p, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers[p] = body
}

func (r *registry) ledger() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// repo is a fake GitHub repository: files by path.
type repo map[string]string

func (r repo) Files(_ context.Context, _, _ string, keep func(string) bool) ([]github.TreeEntry, error) {
	var out []github.TreeEntry
	for p, c := range r {
		if dir := dirOf(p); dir == "." || keep(dir) {
			out = append(out, github.TreeEntry{Path: p, Type: "blob", SHA: "sha:" + p + ":" + c, Size: int64(len(c))})
		}
	}
	return out, nil
}

func (r repo) Tags(context.Context, string, string) ([]string, error) { return nil, nil }

// public is every repository catalogued and public.
func public(snapshot.RepoKey) (snapshot.Repo, bool) {
	return snapshot.Repo{Visibility: "public", HTMLURL: "https://github.com/acme/x"}, true
}

func (r repo) Blob(_ context.Context, _, _, sha string) ([]byte, error) {
	_, rest, _ := strings.Cut(sha, ":")
	_, c, _ := strings.Cut(rest, ":")
	return []byte(c), nil
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func run(t *testing.T, p *Packages, prev store.Item) (sync.Result, payload) {
	t.Helper()
	res, err := p.Spec().Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x", Prev: prev})
	if err != nil {
		t.Fatal(err)
	}
	return res, decode(res.Payload)
}

const childPOM = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent><groupId>no.example</groupId><artifactId>parent</artifactId><version>8</version></parent>
  <groupId>net.example.sso</groupId>
  <artifactId>TypeLib</artifactId>
  <version>3.0.22-SNAPSHOT</version>
  <repositories>
    <repository><id>releases</id><url>%REPO%/releases/</url></repository>
  </repositories>
</project>`

const parentPOM = `<project>
  <groupId>no.example</groupId><artifactId>parent</artifactId><version>8</version>
  <properties><repo.base>%REPO%</repo.base></properties>
  <distributionManagement>
    <repository><id>example</id><url>${repo.base}/releases/</url></repository>
    <snapshotRepository><id>example</id><url>${repo.base}/snapshots/</url></snapshotRepository>
  </distributionManagement>
</project>`

const typelibMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata><groupId>net.example.sso</groupId><artifactId>TypeLib</artifactId>
  <versioning><release>3.0.21</release><versions><version>3.0.20</version><version>3.0.21</version></versions>
  <lastUpdated>20250612093000</lastUpdated></versioning></metadata>`

func TestMavenInheritsItsDistributionFromTheParent(t *testing.T) {
	reg, central := newRegistry(t), newRegistry(t)
	reg.set("/releases/no/example/parent/8/parent-8.pom", strings.ReplaceAll(parentPOM, "%REPO%", reg.URL))
	reg.set("/releases/net/example/sso/TypeLib/maven-metadata.xml", typelibMetadata)
	files := repo{"pom.xml": strings.ReplaceAll(childPOM, "%REPO%", reg.URL)}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: central.URL + "/maven2/"}}, Client: NewClient(reg.Client().Transport), Public: true, Repo: public})

	res, got := run(t, p, store.Item{})
	if len(got.Packages) != 1 || got.Packages[0].Latest == nil {
		t.Fatalf("packages = %+v", got.Packages)
	}
	v := got.Packages[0].Latest
	if got.Packages[0].Name != "net.example.sso:TypeLib" || v.Version != "3.0.21" || v.Published != time.Date(2025, 6, 12, 9, 30, 0, 0, time.UTC) ||
		v.URL != reg.URL+"/releases/net/example/sso/TypeLib/3.0.21/" || res.Again != 0 {
		t.Errorf("latest = %+v, again %v", v, res.Again)
	}
	if len(central.ledger()) != 0 {
		t.Errorf("Maven Central was asked: %v", central.ledger())
	}
	// The released parent is kept: the next run asks only for the metadata.
	asked := len(reg.ledger())
	_, again := run(t, p, store.Item{State: snapshot.StateOK, Payload: res.Payload})
	if n := len(reg.ledger()) - asked; n != 1 || again.Packages[0].Latest.Version != "3.0.21" {
		t.Errorf("the second run asked %d times: %v", n, reg.ledger()[asked:])
	}
}

func TestMavenFallsBackToCentral(t *testing.T) {
	central := newRegistry(t)
	central.set("/maven2/org/example/lib/maven-metadata.xml",
		`<metadata><versioning><latest>2.0.0-SNAPSHOT</latest><versions><version>1.9</version><version>2.0.0-SNAPSHOT</version></versions></versioning></metadata>`)
	files := repo{"pom.xml": `<project><groupId>org.example</groupId><artifactId>lib</artifactId></project>`,
		"web/pom.xml": `<project><groupId>${g}</groupId><artifactId>skipped</artifactId></project>`}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: central.URL + "/maven2/"}}, Client: NewClient(central.Client().Transport), Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	if len(got.Packages) != 1 || got.Packages[0].Latest == nil || got.Packages[0].Latest.Version != "1.9" {
		t.Errorf("packages = %+v", got.Packages)
	}
	// Without the public registries nothing is asked.
	p = New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: central.URL + "/maven2/"}}, Client: NewClient(central.Client().Transport), Public: false, Repo: public})
	asked := len(central.ledger())
	if _, got = run(t, p, store.Item{}); got.Packages[0].Latest != nil || len(central.ledger()) != asked {
		t.Errorf("public off: %+v, asked %v", got.Packages, central.ledger()[asked:])
	}
}

func TestNPM(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/@acme%2Fui", `{"_id":"@acme/ui","name":"@acme/ui","dist-tags":{"next":"3.0.0-rc.1","latest":"2.4.1"},
		"versions":{"2.4.1":{"name":"@acme/ui","dependencies":{"react":"^19"}}},
		"time":{"created":"2020-01-01T00:00:00.000Z","2.4.0":"2025-01-01T00:00:00.000Z","2.4.1":"2025-05-05T10:00:00.000Z"}}`)
	files := repo{"package.json": `{"name":"@acme/ui","version":"2.4.1"}`, "site/package.json": `{"name":"site","private":true}`,
		"tests/package.json": `{"name":"fixture"}`}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{NPM{Registry: reg.URL + "/"}}, Client: NewClient(reg.Client().Transport), Public: true, Repo: public})
	_, got := run(t, p, store.Item{})
	if len(got.Packages) != 1 {
		t.Fatalf("packages = %+v", got.Packages)
	}
	v := got.Packages[0].Latest
	if v == nil || v.Version != "2.4.1" || v.Published != time.Date(2025, 5, 5, 10, 0, 0, 0, time.UTC) || v.URL != reg.URL+"/@acme%2Fui" {
		t.Errorf("latest = %+v", v)
	}
}

func TestAFailedLookupKeepsTheLastRelease(t *testing.T) {
	reg := newRegistry(t)
	reg.set("/maven2/org/example/lib/maven-metadata.xml", `<metadata><versioning><release>1.0</release></versioning></metadata>`)
	files := repo{"pom.xml": `<project><groupId>org.example</groupId><artifactId>lib</artifactId></project>`}
	p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: reg.URL + "/maven2/"}}, Client: NewClient(reg.Client().Transport), Public: true, Repo: public})
	first, _ := run(t, p, store.Item{})
	reg.mu.Lock()
	reg.status["/maven2/org/example/lib/maven-metadata.xml"] = http.StatusBadGateway
	reg.mu.Unlock()
	res, got := run(t, p, store.Item{State: snapshot.StateOK, Payload: first.Payload})
	if pk := got.Packages[0]; pk.Latest == nil || pk.Latest.Version != "1.0" || !strings.Contains(pk.Problem, "answered 502") || res.Again != failedAgain {
		t.Errorf("package = %+v, again %v", pk, res.Again)
	}
}

func TestClientHoldsNoHostForAnother(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	fast := newRegistry(t)
	fast.set("/x", "ok")
	transport := slow.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true // two loopback test servers with their own certificates
	c := NewClient(transport)
	ctx, cancel := context.WithCancel(t.Context())
	var wg gosync.WaitGroup
	for range perHost + 2 { // every slot of the slow host taken, two more waiting
		wg.Go(func() { _ = c.Get(ctx, slow.URL+"/hang", "*/*", func(r io.Reader) error { return nil }) })
	}
	time.Sleep(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- c.Get(t.Context(), fast.URL+"/x", "*/*", func(r io.Reader) error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the fast host: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("a slow host held another")
	}
	cancel()
	wg.Wait()
}

func TestClientPausesAHostThatAsksToSlowDown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewClient(srv.Client().Transport)
	read := func(io.Reader) error { return nil }
	var re *RegistryError
	if err := c.Get(t.Context(), srv.URL+"/a", "*/*", read); !errors.As(err, &re) || re.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v", err)
	}
	if err := c.Get(t.Context(), srv.URL+"/b", "*/*", read); err == nil || !strings.Contains(err.Error(), "paused until") || hits.Load() != 1 {
		t.Errorf("err = %v, hits %d", err, hits.Load())
	}
	c.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	_ = c.Get(t.Context(), srv.URL+"/c", "*/*", read)
	if hits.Load() != 2 {
		t.Error("the pause did not end")
	}
}

func TestClientAsksConditionally(t *testing.T) {
	reg := newRegistry(t)
	reg.etag = true
	reg.set("/doc", `{"a":1}`)
	c := NewClient(reg.Client().Transport)
	var bodies []string
	for range 2 {
		err := c.Get(t.Context(), reg.URL+"/doc", "application/json", func(r io.Reader) error {
			var v map[string]int
			err := json.NewDecoder(r).Decode(&v)
			bodies = append(bodies, strings.Repeat("x", v["a"]))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if l := reg.ledger(); len(l) != 2 || l[1] != `/doc "v1"` || bodies[1] != "x" {
		t.Errorf("ledger %v, bodies %v", l, bodies)
	}
	if err := c.Get(t.Context(), "http://"+strings.TrimPrefix(reg.URL, "https://")+"/doc", "*/*", nil); err == nil {
		t.Error("an http address was read")
	}
}

func TestAPrivateRepositoryNeverNamesItsPackagesToAPublicRegistry(t *testing.T) {
	reg := newRegistry(t) // the public registries are its /maven2/ and /npm/
	reg.set("/releases/org/example/secret/maven-metadata.xml", `<metadata><versioning><release>2.0</release></versioning></metadata>`)
	files := repo{"pom.xml": `<project><groupId>org.example</groupId><artifactId>secret</artifactId>
		<distributionManagement><repository><url>` + reg.URL + `/releases/</url></repository></distributionManagement></project>`,
		"web/package.json": `{"name":"secret-ui"}`}
	for name, lookup := range map[string]func(snapshot.RepoKey) (snapshot.Repo, bool){
		"private":   func(snapshot.RepoKey) (snapshot.Repo, bool) { return snapshot.Repo{Visibility: "private"}, true },
		"internal":  func(snapshot.RepoKey) (snapshot.Repo, bool) { return snapshot.Repo{Visibility: "internal"}, true },
		"not known": func(snapshot.RepoKey) (snapshot.Repo, bool) { return snapshot.Repo{}, false },
	} {
		t.Run(name, func(t *testing.T) {
			p := New(map[string]GitHub{"gh": files}, Options{Providers: []Provider{Maven{Central: reg.URL + "/maven2/"}, NPM{Registry: reg.URL + "/npm/"}},
				Client: NewClient(reg.Client().Transport), Public: true, Repo: lookup})
			_, got := run(t, p, store.Item{})
			if len(got.Packages) != 2 || got.Packages[0].Latest == nil || got.Packages[0].Latest.Version != "2.0" || got.Packages[1].Latest != nil {
				t.Errorf("packages = %+v", got.Packages)
			}
			for _, asked := range reg.ledger() {
				if strings.HasPrefix(asked, "/maven2/") || strings.HasPrefix(asked, "/npm/") {
					t.Errorf("a public registry was asked: %s", asked)
				}
			}
		})
	}
}
