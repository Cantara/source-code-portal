package main

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	contract "github.com/cantara/source-code-portal/api"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// The private repository's traces: its name, its commit subject, its README and its description. None may reach a public viewer.
var hiddenMarks = []string{"zq-hidden", "zq hidden", "Zq Hidden", "q7 vault"} //nolint:gochecknoglobals // test table

// fetch requests a path of the portal with headers; a stream ends at the deadline with what it sent. The pseudo-header ":post"
// makes it a POST of its value as application/json; ":events" with a number n ends a stream after its n-th event instead of at the deadline.
type fetch func(path string, headers ...string) (int, string, http.Header)

// staff is the trusted proxy's identity of a member of the private group (the test is the proxy: it is on 127.0.0.1).
// botSecret is the secret of the token "bot" a configuration may name ($DIR/bot).
const botSecret = "bot-secret-0123456789abcdef0123456789"

// outsiderSecret is the secret of a token a configuration may name ($DIR/outsider) without a private group: it reads as the public.
const outsiderSecret = "outsider-secret-0123456789abcdef01234"

var staff = []string{"X-Forwarded-User", "ann", "X-Forwarded-Groups", "staff"} //nolint:gochecknoglobals // test value

// privatePortal runs a portal over acme/open and the private acme/zq-hidden (README, a document, a go.mod that open depends on),
// with extra appended to its configuration, and waits until a staff member sees everything.
func privatePortal(t *testing.T, extra string) fetch {
	t.Helper()
	get, _ := privatePortalAt(t, extra)
	return get
}

// privatePortalAt is privatePortal with the portal's base URL; setup changes the fake GitHub before the portal starts.
func privatePortalAt(t *testing.T, extra string, setup ...func(*githubtest.Server)) (fetch, string) {
	t.Helper()
	return portalAt(t, extra, true, setup...)
}

// publicTwin is privatePortal's twin without the private repository: the same GitHub and configuration, the private repository
// left out of the listing. Whatever the public reads of privatePortal it must read of the twin word for word.
func publicTwin(t *testing.T, extra string) (fetch, string) {
	t.Helper()
	return portalAt(t, extra, false)
}

func portalAt(t *testing.T, extra string, private bool, setup ...func(*githubtest.Server)) (fetch, string) {
	t.Helper()
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	open := gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public"), Description: new("open tools")})
	hidden := gh.AddRepo("acme", gogh.Repository{Name: new("zq-hidden"), Visibility: new("private"), Description: new("the q7 vault"),
		Topics: []string{"zq-hidden"}})
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	gh.SetCommits(open, githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "open change", AuthorName: "Ann", Date: at})
	gh.SetCommits(hidden, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "zq hidden change", AuthorName: "Ann", Date: at.Add(time.Hour)})
	gh.SetFile(open, "README.md", "# Open\n")
	gh.SetFile(open, "go.mod", "module example.com/open\n\ngo 1.27\n\nrequire example.com/lib/q7 v1.0.0\n")
	gh.SetFile(hidden, "README.md", "# Zq Hidden\n\n![x](docs/x.png)\n")
	gh.SetFile(hidden, "docs/guide.md", "# Zq Hidden guide\n")
	gh.SetFile(hidden, "go.mod", "module example.com/lib/q7\n\ngo 1.27\n")
	gh.SetPulls(hidden, githubtest.Pull{Number: 1, Title: "zq hidden pull to the q7 vault", Author: "ann", Created: at, Updated: at,
		Base: "main", Head: "zq-hidden-work", HeadSHA: strings.Repeat("c", 40)})
	gh.SetIssues(hidden, githubtest.Issue{Number: 2, Title: "zq hidden issue in the q7 vault", Author: "ann", Created: at, Updated: at,
		Labels: []string{"zq-hidden"}})
	for _, f := range setup {
		f(gh)
	}

	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("ghp_test"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]string{"bot": botSecret, "outsider": outsiderSecret} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(secret), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {trusted_proxies: [127.0.0.1/32], public_url: "https://scp.example"}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `, include_private: ` + strconv.FormatBool(private) + `}
catalog:
  groups:
    - {id: all, name: All, default_repo: zq-hidden, match: [{catch_all: true}]}
announcements:
  - {id: move, title: "zq-hidden moves to the q7 vault", audience: signed-in, until: "2999-01-01T00:00:00Z"}
` + strings.ReplaceAll(extra, "$DIR", dir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	base := "http://" + srv.publicLn.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() {
		client.CloseIdleConnections()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	})
	get := func(path string, headers ...string) (int, string, http.Header) {
		t.Helper()
		rctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
		defer stop()
		req, _ := http.NewRequestWithContext(rctx, http.MethodGet, base+path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			if headers[i] == ":post" {
				req, _ = http.NewRequestWithContext(rctx, http.MethodPost, base+path, strings.NewReader(headers[i+1]))
				req.Header.Set("Content-Type", "application/json")
			}
		}
		events := 0
		for i := 0; i+1 < len(headers); i += 2 {
			switch headers[i] {
			case ":post":
			case ":events":
				events, _ = strconv.Atoi(headers[i+1])
			default:
				req.Header.Set(headers[i], headers[i+1])
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if events > 0 {
			return resp.StatusCode, firstEvents(resp.Body, events), resp.Header
		}
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var d oapi.Dashboard
		_, b, _ := get("/api/v1/dashboard", staff...)
		_, docs, _ := get("/api/v1/repos/acme/zq-hidden/docs", staff...)
		_, mf, _ := get("/api/v1/repos/acme/open/manifests", staff...)
		if !private && json.Unmarshal([]byte(b), &d) == nil && d.Freshness.Fresh == 1 && len(d.Latest) == 1 && strings.Contains(mf, "example.com/lib/q7") {
			return get, base
		}
		if json.Unmarshal([]byte(b), &d) == nil && d.Freshness.Fresh == 2 && len(d.Latest) == 2 && strings.Contains(docs, "guide") &&
			strings.Contains(mf, "zq-hidden") {
			return get, base
		}
		if time.Now().After(deadline) {
			t.Fatalf("not loaded within 10s: %s\n%s\n%s", b, docs, mf)
		}
	}
}

// streamed ends the event stream after its first two events (the retry hint and the snapshot's), where a path is the stream; the rest
// of its life is the heartbeat, which a comparison of two portals' answers needs not wait for.
func streamed(path string, headers ...string) []string {
	if path == "/api/v1/events" {
		return append(slices.Clone(headers), ":events", "2")
	}
	return headers
}

// firstEvents reads a server-sent stream up to the end of its n-th event (a blank line ends an event) or its end: what an open
// stream has said so far, without waiting for the deadline.
func firstEvents(r io.Reader, n int) string {
	var b strings.Builder
	buf := make([]byte, 512)
	for strings.Count(b.String(), "\n\n") < n {
		k, err := r.Read(buf)
		b.Write(buf[:k])
		if err != nil {
			break
		}
	}
	return b.String()
}

// untilListed waits until the lists of pull requests, issues and releases hold what the jobs of their own read (a cold start runs
// them after the catalogue's, which privatePortal waits for), so a test that reads them sees their answer and not an unread one.
func untilListed(t *testing.T, get fetch) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		ready := true
		for _, p := range []string{"/api/v1/pulls", "/api/v1/issues", "/api/v1/releases"} {
			var l struct{ Freshness oapi.Freshness }
			_, b, _ := get(p, staff...)
			ready = ready && json.Unmarshal([]byte(b), &l) == nil && l.Freshness.Status == "fresh"
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the lists were not read within 10s")
		}
	}
}

// publicPaths are every GET of the API contract, read from api/openapi.yaml, and every route outside it, with the private
// repository where a path names one. A new route goes on this list.
func publicPaths(t *testing.T) []string {
	t.Helper()
	param := regexp.MustCompile(`\{(\w+)\}`)
	var paths []string
	for _, m := range regexp.MustCompile(`(?m)^  (/\S+):\n    get:`).FindAllStringSubmatch(string(contract.OpenAPI), -1) {
		paths = append(paths, "/api/v1"+param.ReplaceAllStringFunc(m[1], func(p string) string {
			return map[string]string{"{owner}": "acme", "{name}": "zq-hidden", "{group}": "all", "{docId}": "docs%2Fguide.md", "{page}": "dashboard"}[p]
		}))
	}
	if len(paths) < 12 {
		t.Fatalf("contract paths: %v", paths)
	}
	return append(paths, "/llms.txt", "/llms-full.txt", "/knowledge.yaml", "/.well-known/kcp.json", "/sitemap.xml", "/sitemaps/1.xml",
		"/groups/all.md", "/repos/acme/open/readme.md", "/repos/acme/zq-hidden/readme.md", "/repos/acme/zq-hidden/docs/docs/guide.md",
		"/badges/repos/acme/zq-hidden/last-commit.svg", "/badges/repos/acme/zq-hidden/release.svg", "/badges/repos/acme/zq-hidden/license.svg",
		"/badges/repos/acme/zq-hidden/issues.svg", "/badges/repos/acme/zq-hidden/pulls.svg", "/badges/repos/acme/zq-hidden/health.svg", "/badges/groups/all/repos.svg",
		"/badges/groups/all/health.svg", "/badges/groups/all/last-commit.svg", "/badges/groups/all/release.svg",
		"/images/acme/zq-hidden/00000000000000000000000000000000", "/badge/license/zq-hidden/main", "/commits/zq-hidden",
		"/commits/acme/zq-hidden", "/contents/acme/zq-hidden/main", "/api/v1/events", "/api/v1/admin/layouts/dashboard",
		"/api/v1/pages/present", "/api/v1/pages/dashboard", "/api/v1/admin/layouts/present", "/api/v1/admin/announcements",
		// The search, by the private repository's name, its README's and commit's words, a filter and a prefix.
		"/api/v1/search?q=zq-hidden", "/api/v1/search?q=zq-hidden+in:commit", "/api/v1/search?q=vault+in:pull", "/api/v1/search?q=vault+in:issue", "/api/v1/search?q=repo:zq-hidden",
		"/api/v1/search?q=in:doc+zq-hidden+guide", "/api/v1/search?q=zq-hidden&in=repo", "/api/v1/search/suggest?q=zq-hidden",
		"/api/v1/search/suggest?q=zq-hid", "/opensearch.xml")
}

// TestNoLeak fetches every public path as the public, in HTML and Markdown: no answer carries a trace of the private repository,
// and each address naming it answers word for word as an address that does not exist (no existence leak). A staff member sees it.
func TestNoLeak(t *testing.T) {
	get := privatePortal(t, "access: {private_groups: [staff]}\n")
	untilListed(t, get)
	clean := regexp.MustCompile(`urn:scp:request:\w+`)
	for _, p := range publicPaths(t) {
		for _, accept := range []string{"*/*", "text/markdown"} {
			code, b, _ := get(p, streamed(p, "Accept", accept)...)
			if !strings.Contains(p, "zq-hidden") {
				for _, m := range hiddenMarks {
					if strings.Contains(b, m) {
						t.Errorf("public %s (%s): %d carries %q:\n%s", p, accept, code, m, b)
					}
				}
				continue
			}
			// An address naming the repository may echo it; it must answer word for word as one that does not exist.
			absent := strings.ReplaceAll(p, "zq-hidden", "nothere")
			ac, ab, _ := get(absent, "Accept", accept)
			if ac != code || clean.ReplaceAllString(ab, "") != clean.ReplaceAllString(strings.ReplaceAll(b, "zq-hidden", "nothere"), "") {
				t.Errorf("public %s (%s) answers %d, unlike an address that does not exist (%d):\n%s\n%s", p, accept, code, ac, b, ab)
			}
		}
	}
	for _, p := range []string{"/api/v1/pages/dashboard", "/api/v1/pages/present", "/api/v1/repos/acme/zq-hidden", "/api/v1/pulls", "/api/v1/issues", "/llms.txt", "/knowledge.yaml", "/api/v1/repos/acme/open/manifests",
		"/api/v1/search?q=zq-hidden", "/api/v1/search?q=zq-hidden+in:commit", "/api/v1/search?q=vault+in:pull", "/api/v1/search?q=vault+in:issue", "/api/v1/search?q=in:doc+zq-hidden+guide", "/api/v1/search/suggest?q=zq-hid"} {
		if _, b, _ := get(p, staff...); !strings.Contains(b, "zq-hidden") {
			t.Errorf("staff %s does not show the private repository:\n%s", p, b)
		}
	}
}

// TestNoLeakThroughTools calls every tool of the catalogue as the public, with the private repository wherever a tool takes one:
// no answer carries a trace of it, and each answers as for a repository that does not exist. A staff member's call reads it.
func TestNoLeakThroughTools(t *testing.T) {
	get := privatePortal(t, "access: {private_groups: [staff]}\n")
	_, list, _ := get("/api/v1/tools")
	var tl oapi.ToolList
	if err := json.Unmarshal([]byte(list), &tl); err != nil || len(tl.Tools) < 8 {
		t.Fatalf("tools = %s", list)
	}
	input := func(name, repo string) string {
		return map[string]string{
			"search": `{"input":{"query":"` + repo + `"}}`, "list_groups": `{"input":{}}`, "get_group": `{"input":{"group":"all"}}`,
			"get_document":         `{"input":{"owner":"acme","name":"` + repo + `","path":"docs/guide.md"}}`,
			"recent_commits":       `{"input":{"repo":"acme/` + repo + `","work":["hand","ai","automation"]}}`,
			"open_pull_requests":   `{"input":{"repo":"acme/` + repo + `"}}`,
			"open_issues":          `{"input":{"repo":"acme/` + repo + `"}}`,
			"recent_releases":      `{"input":{"repo":"acme/` + repo + `","variant":"all"}}`,
			"fleet_insights":       `{"input":{"repo":"acme/` + repo + `"}}`,
			"retired_repositories": `{"input":{"q":"` + repo + `"}}`,
		}[name]
	}
	nonce := regexp.MustCompile(`id=\\"[0-9a-f]+\\"`)
	for _, tool := range tl.Tools {
		body := cmp.Or(input(tool.Name, "zq-hidden"), `{"input":{"owner":"acme","name":"zq-hidden"}}`)
		code, b, _ := get("/api/v1/tools/"+tool.Name, ":post", body)
		for _, m := range hiddenMarks {
			if strings.Contains(strings.ReplaceAll(b, "zq-hidden", ""), m) || (code == 200 && !strings.Contains(body, "zq-hidden") && strings.Contains(b, m)) {
				t.Errorf("public %s: %d carries %q:\n%s", tool.Name, code, m, b)
			}
		}
		absent := strings.ReplaceAll(body, "zq-hidden", "nothere")
		ac, ab, _ := get("/api/v1/tools/"+tool.Name, ":post", absent)
		if ac != code || nonce.ReplaceAllString(ab, "") != nonce.ReplaceAllString(strings.ReplaceAll(b, "zq-hidden", "nothere"), "") {
			t.Errorf("public %s answers %d, unlike a repository that does not exist (%d):\n%s\n%s", tool.Name, code, ac, b, ab)
		}
		if code, b, _ := get("/api/v1/tools/"+tool.Name, append([]string{":post", body}, staff...)...); code != 200 || strings.Contains(b, `"isError":true`) &&
			tool.Name != "get_document" {
			t.Errorf("staff %s: %d %s", tool.Name, code, b)
		}
	}
	if _, b, _ := get("/api/v1/tools/repository_health", append([]string{":post", `{"input":{"owner":"acme","name":"zq-hidden"}}`}, staff...)...); !strings.Contains(b, "Health") {
		t.Errorf("staff does not read the private repository's health: %s", b)
	}
	// A cookie-carrying call from another site is refused; a bad input is a problem, not a run.
	if code, _, _ := get("/api/v1/tools/list_groups", ":post", `{"input":{}}`, "Cookie", "scp-session=x", "Sec-Fetch-Site", "cross-site"); code != http.StatusForbidden {
		t.Errorf("a cross-site call = %d", code)
	}
	if code, b, _ := get("/api/v1/tools/search", ":post", `{"input":{"query":"open","limit":"many"}}`); code != http.StatusBadRequest {
		t.Errorf("a bad input = %d %s", code, b)
	}
	if code, _, _ := get("/api/v1/tools/drop_tables", ":post", `{"input":{}}`); code != http.StatusNotFound {
		t.Errorf("an unknown tool = %d", code)
	}
}

// TestRequireAuth: with access.require_auth there is no public view. Every public path refuses an anonymous request (401, or a
// browser's page request goes to sign-in); robots.txt keeps crawlers out; a token, or the proxy's identity, gets its answer.
func TestRequireAuth(t *testing.T) {
	get, base := privatePortalAt(t, `access: {private_groups: [staff], require_auth: true}
auth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}
`)
	if _, err := mcpConnect(t, base, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("anonymous MCP: %v", err)
	}
	if r, err := mcpSession(t, base, botSecret).CallTool(t.Context(), &mcp.CallToolParams{Name: "list_groups", Arguments: map[string]any{}}); err != nil || r.IsError {
		t.Errorf("a token's MCP: %v %+v", err, r)
	}
	for _, p := range append(publicPaths(t), "/", "/repos/acme/open", "/groups/all") {
		if p == "/api/v1/me" { // the sign-in page reads the methods
			continue
		}
		if code, b, _ := get(p); code != http.StatusUnauthorized || strings.Contains(b, "open") {
			t.Errorf("anonymous %s: %d %s", p, code, b)
		}
	}
	if code, _, h := get("/repos/acme/open?tab=docs", "Accept", "text/html"); code != http.StatusSeeOther ||
		h.Get("Location") != "/signin?return_to=%2Frepos%2Facme%2Fopen%3Ftab%3Ddocs" {
		t.Errorf("a browser's page: %d %q", code, h.Get("Location"))
	}
	if code, b, _ := get("/api/v1/me"); code != http.StatusOK || strings.Contains(b, "zq-hidden") {
		t.Errorf("/api/v1/me: %d %s", code, b)
	}
	if code, b, _ := get("/robots.txt"); code != http.StatusOK || b != "User-agent: *\nDisallow: /\n" {
		t.Errorf("robots.txt: %d %q", code, b)
	}
	for _, who := range [][]string{{"Authorization", "Bearer " + botSecret}, staff} {
		if code, b, _ := get("/llms.txt", who...); code != http.StatusOK || !strings.Contains(b, "zq-hidden") {
			t.Errorf("%s: %d %s", who[0], code, b)
		}
	}
}
