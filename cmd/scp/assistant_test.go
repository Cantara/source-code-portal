package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// fakeAnthropic is a loopback Messages API: a question gets a call of get_repository for the repository it names, and a tool result
// is echoed back as the answer.
func fakeAnthropic(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if r.Header.Get("x-api-key") != "sk-test" || json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(name string, data any) {
			b, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
		}
		event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 50}}})
		last := req.Messages[len(req.Messages)-1].Content
		var question string
		var results []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		stop := "end_turn"
		if json.Unmarshal(last, &question) == nil {
			name := question[strings.LastIndex(question, " ")+1:]
			event("content_block_start", map[string]any{"type": "content_block_start", "index": 0,
				"content_block": map[string]any{"type": "tool_use", "id": "toolu_1", "name": "get_repository", "input": map[string]any{}}})
			in, _ := json.Marshal(map[string]string{"owner": "acme", "name": name})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(in)}})
			stop = "tool_use"
		} else {
			_ = json.Unmarshal(last, &results)
			event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": results[0].Content}})
		}
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": 5}})
		event("message_stop", map[string]any{"type": "message_stop"})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAssistant asks the assistant through the public listener: its tools read what the viewer may see, and no more.
func TestAssistant(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	open := gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	secret := gh.AddRepo("acme", gogh.Repository{Name: new("secret"), Visibility: new("private")})
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	gh.SetCommits(open, githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "open change", AuthorName: "Ann", Date: at})
	gh.SetCommits(secret, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "secret change", AuthorName: "Ann", Date: at})
	gh.SetFile(open, "README.md", "# Open\n\nThe open one.\n")
	gh.SetFile(secret, "README.md", "# Secret\n\nThe hidden one.\n")
	model := fakeAnthropic(t)

	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	token, key := write("token", "ghp_test"), write("llm-key", "sk-test")
	path := write("scp.yaml", `store: {path: `+filepath.Join(dir, "scp.db")+`}
server: {trusted_proxies: [127.0.0.1/32]}
access: {user_header: X-Forwarded-User, groups_header: X-Forwarded-Groups, private_groups: [staff]}
llm: {provider: anthropic, model: claude-test, base_url: "`+model.URL+`", api_key_file: `+key+`}
assistant: {access: signed-in}
github:
  sources:
    - {id: gh, base_url: "`+gh.BaseURL()+`", orgs: [acme], token_file: `+token+`, include_private: true}
catalog:
  groups:
    - {id: all, name: All, default_repo: open, match: [{catch_all: true}]}
`)
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, deps{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()
	public, admin := "http://"+srv.publicLn.Addr().String(), "http://"+srv.adminLn.Addr().String()
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	do := func(method, url, user, groups, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			req.Header.Set("X-Forwarded-User", user)
			req.Header.Set("X-Forwarded-Groups", groups)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		_, b := do(http.MethodGet, public+"/api/v1/repos/acme/secret/docs", "ann", "staff", "")
		if strings.Contains(b, "README") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not loaded within 10s: %s", b)
		}
	}

	var st oapi.AssistantStatus
	_, b := do(http.MethodGet, public+"/api/v1/assistant", "", "", "")
	if json.Unmarshal([]byte(b), &st) != nil || !st.Enabled || st.Allowed || st.Provider != "anthropic" || st.Access != oapi.SignedIn {
		t.Errorf("anonymous status: %s", b)
	}
	ask := func(user, groups, name string) (int, string) {
		return do(http.MethodPost, public+"/api/v1/assistant/chat", user, groups, `{"messages":[{"role":"user","content":"What is acme/`+name+`? `+name+`"}]}`)
	}
	if code, b := ask("", "", "open"); code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d %s", code, b)
	}
	if code, b := ask("bob", "dev", "open"); code != 200 || !strings.Contains(b, "The open one.") || !strings.Contains(b, `event: tool`) ||
		!strings.Contains(b, "event: done") {
		t.Errorf("open: %d %s", code, b)
	}
	if _, b := ask("bob", "dev", "secret"); strings.Contains(b, "hidden") || !strings.Contains(b, "not found") {
		t.Errorf("a viewer outside staff read the private repository: %s", b)
	}
	if _, b := ask("ann", "staff", "secret"); !strings.Contains(b, "The hidden one.") {
		t.Errorf("staff: %s", b)
	}
	if _, b := do(http.MethodGet, admin+"/metrics", "", "", ""); !strings.Contains(b, `scp_assistant_requests_total{outcome="answered"} 3`) ||
		!strings.Contains(b, `scp_assistant_tokens_total{kind="output"} 30`) {
		t.Errorf("metrics lack the assistant")
	}
}
