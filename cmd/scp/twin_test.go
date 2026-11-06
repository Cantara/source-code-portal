package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// volatile are the parts of an answer that differ between any two portals: request ids, times, nonces, and the snapshot's version
// (the sum of its items' digests, which hold when each was checked; TestPrivateChangeUnseen holds the public one still).
var volatile = []*regexp.Regexp{ //nolint:gochecknoglobals // test table
	regexp.MustCompile(`urn:scp:request:\w+`),
	regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`),
	regexp.MustCompile(`id=\\?"[0-9a-f]+\\?"`),
	regexp.MustCompile(`"version":\d+`),
}

func normal(s string) string {
	for _, re := range volatile {
		s = re.ReplaceAllString(s, "~")
	}
	return s
}

// TestPublicTwin: the public reads the portal with a private repository word for word as the same portal that never knew it, by
// every path and every tool: no name, and no count, score, total, order or existence the private repository changed.
func TestPublicTwin(t *testing.T) {
	extra := "access: {private_groups: [staff]}\nauth: {tokens: [{name: outsider, secret_file: $DIR/outsider}]}\n"
	get, base := privatePortalAt(t, extra)
	twin, twinBase := publicTwin(t, extra)
	outsider := []string{"Authorization", "Bearer " + outsiderSecret}
	for _, p := range append(publicPaths(t), edgePaths...) {
		for _, h := range [][]string{{"Accept", "*/*"}, {"Accept", "text/markdown"}, outsider} {
			h = streamed(p, h...)
			code, b, _ := get(p, h...)
			tc, tb, _ := twin(p, h...)
			if code != tc || normal(b) != normal(tb) {
				t.Errorf("public %s (%s): %d, the twin %d:\n%s\n---\n%s", p, h[1], code, tc, b, tb)
			}
		}
	}
	// A validator staff was given proves nothing to the public: the answer is the public one, never 304.
	_, _, h := get("/api/v1/dashboard", staff...)
	if code, b, _ := get("/api/v1/dashboard", "If-None-Match", h.Get("ETag")); code != http.StatusOK || strings.Contains(b, "zq-hidden") {
		t.Errorf("staff's ETag %s replayed by the public: %d %s", h.Get("ETag"), code, b)
	}
	_, list, _ := get("/api/v1/tools")
	var tl oapi.ToolList
	if err := json.Unmarshal([]byte(list), &tl); err != nil || len(tl.Tools) < 8 {
		t.Fatalf("tools = %s", list)
	}
	for _, tool := range tl.Tools {
		for _, body := range toolInputs(tool.Name) {
			for _, who := range [][]string{nil, outsider} {
				code, b, _ := get("/api/v1/tools/"+tool.Name, append([]string{":post", body}, who...)...)
				tc, tb, _ := twin("/api/v1/tools/"+tool.Name, append([]string{":post", body}, who...)...)
				if code != tc || normal(b) != normal(tb) {
					t.Errorf("public %s %s: %d, the twin %d:\n%s\n---\n%s", tool.Name, body, code, tc, b, tb)
				}
			}
		}
	}

	// MCP: every tool and input, anonymous and with a token of no private group, answers as the twin's; a wrong token is refused,
	// never read as the public.
	twinMCP := mcpSession(t, twinBase, "")
	for _, s := range []*mcp.ClientSession{mcpSession(t, base, ""), mcpSession(t, base, outsiderSecret)} {
		for _, tool := range tl.Tools {
			for _, body := range toolInputs(tool.Name) {
				var in struct{ Input map[string]any }
				if err := json.Unmarshal([]byte(body), &in); err != nil {
					t.Fatal(err)
				}
				r, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: in.Input})
				tr, terr := twinMCP.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: in.Input})
				if err != nil || terr != nil || r.IsError != tr.IsError || normal(text(r)) != normal(text(tr)) {
					t.Errorf("MCP %s %s: %v %v\n%s\n---\n%s", tool.Name, body, err, terr, text(r), text(tr))
				}
			}
		}
	}
	if _, err := mcpConnect(t, base, map[string]string{"Authorization": "Bearer wrong-0123456789abcdef0123456789"}); err == nil ||
		!strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("a wrong token's MCP: %v", err)
	}
}

// edgePaths are the private repository's addresses as a guesser varies them (case, an escaped character, a trailing slash, a
// suffix), the filters that name it, a misspelling and a prefix the search could correct or complete to it, and the admin
// listener's routes asked on the public one.
var edgePaths = []string{ //nolint:gochecknoglobals // test table
	"/api/v1/repos/acme/ZQ-HIDDEN", "/api/v1/repos/ACME/zq-hidden", "/api/v1/repos/acme/Zq-Hidden/docs", "/api/v1/repos/acme/zq%2Dhidden",
	"/api/v1/repos/acme/zq-hidden/", "/api/v1/repos/acme/zq-hidden.git", "/repos/acme/ZQ-hidden/readme.md", "/badges/repos/acme/ZQ-HIDDEN/release.svg",
	"/api/v1/repos/acme/zq-hidden/history?ref=main", "/api/v1/repos/acme/zq-hidden/docs/README.md",
	"/api/v1/commits?repo=acme/zq-hidden", "/api/v1/commits?repo=ACME/ZQ-HIDDEN", "/api/v1/commits?group=all",
	"/api/v1/pulls?repo=acme/zq-hidden", "/api/v1/pulls?group=all&drafts=true", "/api/v1/issues?repo=acme/zq-hidden", "/api/v1/issues?group=all", "/api/v1/alumni", "/api/v1/alumni?group=all&q=zq", "/api/v1/insights", "/api/v1/insights?repo=acme/zq-hidden", "/api/v1/insights?group=all", "/api/v1/releases?variant=all", "/api/v1/releases?repo=acme/zq-hidden&variant=all",
	"/api/v1/search?q=ZQ-HIDDEN", "/api/v1/search?q=zq-hiddem", "/api/v1/search?q=vaul", "/api/v1/search?q=q7", "/api/v1/search?q=%22zq+hidden%22",
	"/api/v1/search?q=change+-open", "/api/v1/search?q=ann", "/api/v1/search/suggest?q=zq", "/api/v1/search/suggest?q=va", "/api/v1/search/suggest?q=q",
	"/api/v1/status", "/metrics", "/debug/pprof/", "/api/v1/admin/assistant/test",
}

// toolInputs are the calls of a tool the twin test makes: with the public repository, with the private one, and with none named.
// The searches include a misspelling and a prefix of the private name: a correction or completion must not reveal it.
func toolInputs(name string) []string {
	switch name {
	case "search":
		return []string{`{"input":{"query":"open"}}`, `{"input":{"query":"zq-hidden"}}`, `{"input":{"query":"vault"}}`,
			`{"input":{"query":"zq-hiddem"}}`, `{"input":{"query":"zq"}}`, `{"input":{"query":"change"}}`}
	case "list_groups":
		return []string{`{"input":{}}`}
	case "get_group":
		return []string{`{"input":{"group":"all"}}`}
	case "get_document":
		return []string{`{"input":{"owner":"acme","name":"open","path":"README.md"}}`,
			`{"input":{"owner":"acme","name":"zq-hidden","path":"docs/guide.md"}}`}
	case "recent_commits", "open_pull_requests", "open_issues", "recent_releases", "fleet_insights":
		return []string{`{"input":{}}`, `{"input":{"repo":"acme/open"}}`, `{"input":{"repo":"acme/zq-hidden"}}`}
	case "retired_repositories":
		return []string{`{"input":{}}`, `{"input":{"q":"open"}}`, `{"input":{"q":"zq-hidden"}}`}
	}
	return []string{`{"input":{"owner":"acme","name":"open"}}`, `{"input":{"owner":"acme","name":"zq-hidden"}}`}
}

// TestPrivateChangeUnseen: a push to the private repository changes what staff sees and nothing the public sees: not the
// snapshot's version, not its event stream.
func TestPrivateChangeUnseen(t *testing.T) {
	var gh *githubtest.Server
	get, base := privatePortalAt(t, "access: {private_groups: [staff]}\nwebhook: {secret_file: $DIR/bot}\n",
		func(s *githubtest.Server) { gh = s })
	version := func(who ...string) int64 {
		t.Helper()
		var s oapi.SnapshotInfo
		if _, b, _ := get("/api/v1/snapshot", who...); json.Unmarshal([]byte(b), &s) != nil {
			t.Fatalf("snapshot: %s", b)
		}
		return s.Version
	}
	public, private := version(), version(staff...)
	stream := make(chan string, 1)
	go func() { _, b, _ := get("/api/v1/events"); stream <- b }() // the public's stream, open for the fetch's 2 s

	gh.SetCommits(gh.ID("acme", "zq-hidden"), githubtest.Commit{SHA: strings.Repeat("d", 40), Message: "zq hidden second change",
		AuthorName: "Ann", Date: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)})
	body := `{"ref":"refs/heads/main","repository":{"name":"zq-hidden","default_branch":"main","owner":{"login":"acme"}}}`
	m := hmac.New(sha256.New, []byte(botSecret))
	m.Write([]byte(body))
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/webhooks/github", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "private-1")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(m.Sum(nil)))
	resp, err := http.DefaultClient.Do(req) //nolint:forbidigo // loopback test server
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook: %d", resp.StatusCode)
	}
	for deadline := time.Now().Add(5 * time.Second); version(staff...) == private; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("staff does not see the push")
		}
	}
	if _, b, _ := get("/api/v1/repos/acme/zq-hidden", staff...); !strings.Contains(b, "zq hidden second change") {
		t.Errorf("staff detail: %s", b)
	}
	if v := version(); v != public {
		t.Errorf("the public version moved with a private push: %d, was %d", v, public)
	}
	if b := <-stream; strings.Count(b, "event: snapshot") != 1 {
		t.Errorf("the public stream told of a private push:\n%s", b)
	}
}
