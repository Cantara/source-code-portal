package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// provider is a fake provider on loopback: it answers every request with status and body, and keeps what it was sent.
type provider struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	body   string
	header http.Header
	sent   map[string]any
}

func newProvider(t *testing.T, status int, fixture string) *provider {
	t.Helper()
	p := &provider{status: status, body: fixture}
	if strings.HasSuffix(fixture, ".sse") {
		b, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		p.body = string(b)
	}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.header, p.sent = r.Header.Clone(), nil
		_ = json.Unmarshal(b, &p.sent)
		p.mu.Unlock()
		if p.status == http.StatusOK {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(p.status)
		_, _ = io.WriteString(w, p.body)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *provider) chat(t *testing.T, name string) Chat {
	t.Helper()
	c, err := New(Config{Provider: name, BaseURL: p.URL, APIKey: []byte("sk-test")})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func collect(c Chat, r Request) []Event {
	var out []Event
	for e := range c.Stream(context.Background(), r) {
		out = append(out, e)
	}
	return out
}

func texts(es []Event) string {
	var b strings.Builder
	for _, e := range es {
		if e.Kind == Text {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func last(es []Event) Event { return es[len(es)-1] }

var tools = []Tool{{Name: "get_group", Description: "A group", Schema: map[string]any{"type": "object"}}} //nolint:gochecknoglobals // fixture

func TestAnthropicText(t *testing.T) {
	p := newProvider(t, 200, "anthropic_text.sse")
	es := collect(p.chat(t, "anthropic"), Request{System: "You read.", Messages: []Message{{Role: User, Content: "How big is Whydah?"}}})
	if texts(es) != "Whydah has two repositories." || last(es).Kind != Stop || last(es).Reason != EndTurn {
		t.Fatalf("events: %+v", es)
	}
	if u := es[len(es)-2]; u.Kind != Usage || u.InputTokens != 412 || u.Output != 9 {
		t.Errorf("usage: %+v", u)
	}
	if p.header.Get("x-api-key") != "sk-test" || p.header.Get("anthropic-version") == "" || p.header.Get("Authorization") != "" {
		t.Errorf("headers: %v", p.header)
	}
	if p.sent["model"] != "claude-haiku-5-5" || p.sent["system"] != "You read." || p.sent["stream"] != true || p.sent["max_tokens"] != 4096.0 {
		t.Errorf("body: %v", p.sent)
	}
}

func TestAnthropicToolsAndReplay(t *testing.T) {
	p := newProvider(t, 200, "anthropic_tools.sse")
	c := p.chat(t, "anthropic")
	es := collect(c, Request{Messages: []Message{{Role: User, Content: "Whydah?"}}, Tools: tools})
	var calls []ToolCall
	for _, e := range es {
		if e.Kind == Call {
			calls = append(calls, e.Call)
		}
	}
	stop := last(es)
	if len(calls) != 2 || calls[0].Name != "get_group" || calls[0].Input["id"] != "whydah" || calls[1].Name != "list_groups" ||
		len(calls[1].Input) != 0 || stop.Reason != ToolUse || stop.Replay == nil || texts(es) != "Let me look." {
		t.Fatalf("events: %+v", es)
	}
	if p.sent["tool_choice"].(map[string]any)["type"] != "auto" {
		t.Errorf("tool choice: %v", p.sent["tool_choice"])
	}

	// The next turn replays the thinking, text and calls unchanged, and its two results share one user turn.
	collect(c, Request{Tools: tools, NoTools: true, Messages: []Message{{Role: User, Content: "Whydah?"},
		{Role: Assistant, Content: "Let me look.", ToolCalls: calls, Replay: stop.Replay},
		{Role: ToolRole, ToolCallID: "toolu_01", Content: "two"}, {Role: ToolRole, ToolCallID: "toolu_02", Content: "boom", IsError: true}}})
	msgs := p.sent["messages"].([]any)
	if len(msgs) != 3 || p.sent["tool_choice"].(map[string]any)["type"] != "none" {
		t.Fatalf("messages: %v", msgs)
	}
	replayed := msgs[1].(map[string]any)["content"].([]any)
	thinking := replayed[0].(map[string]any)
	if len(replayed) != 4 || thinking["type"] != "thinking" || thinking["signature"] != "EqQBCgIYAhIM" ||
		replayed[2].(map[string]any)["input"].(map[string]any)["id"] != "whydah" {
		t.Errorf("replay: %v", replayed)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 || results[1].(map[string]any)["is_error"] != true || results[0].(map[string]any)["tool_use_id"] != "toolu_01" {
		t.Errorf("results: %v", results)
	}
}

func TestOpenAITools(t *testing.T) {
	p := newProvider(t, 200, "openai_tools.sse")
	c := p.chat(t, "openai")
	es := collect(c, Request{Messages: []Message{{Role: User, Content: "Whydah?"}}, Tools: tools, MaxTokens: 100})
	if len(es) != 4 || es[0].Call.ID != "call_a" || es[0].Call.Input["id"] != "whydah" || es[1].Call.Name != "recent_commits" ||
		es[1].Call.Input["limit"] != 5.0 || es[2].InputTokens != 500 || last(es).Reason != ToolUse {
		t.Fatalf("events: %+v", es)
	}
	if p.header.Get("Authorization") != "Bearer sk-test" || p.sent["max_completion_tokens"] != 100.0 || p.sent["max_tokens"] != nil ||
		p.sent["tool_choice"] != "auto" || p.sent["model"] != "gpt-4o" {
		t.Errorf("request: %v %v", p.header, p.sent)
	}
	collect(c, Request{System: "S", Messages: []Message{{Role: User, Content: "q"}, {Role: Assistant, ToolCalls: []ToolCall{es[0].Call}},
		{Role: ToolRole, ToolCallID: "call_a", Content: "two"}}})
	msgs := p.sent["messages"].([]any)
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if msgs[0].(map[string]any)["role"] != "system" || call["function"].(map[string]any)["arguments"] != `{"id":"whydah"}` ||
		msgs[3].(map[string]any)["tool_call_id"] != "call_a" || msgs[2].(map[string]any)["content"] != nil || p.sent["tools"] != nil {
		t.Errorf("messages: %v", msgs)
	}
}

func TestOpenAITextAndEnds(t *testing.T) {
	for _, fixture := range []string{"openai_text.sse", "openai_no_done.sse"} {
		p := newProvider(t, 200, fixture)
		es := collect(p.chat(t, "openai"), Request{Messages: []Message{{Role: User, Content: "q"}}})
		if texts(es) != "Whydah has two repositories." || last(es).Reason != EndTurn {
			t.Errorf("%s: %+v", fixture, es)
		}
	}
}

func TestGeminiThoughtSignature(t *testing.T) {
	p := newProvider(t, 200, "gemini_tools.sse")
	c := p.chat(t, "gemini")
	es := collect(c, Request{Messages: []Message{{Role: User, Content: "q"}}, Tools: tools})
	call := es[0].Call
	if call.ID != "function-call-1" || call.Input["owner_name"] != "acme/core" || call.Extra["extra_content"] == nil ||
		p.sent["max_tokens"] != 4096.0 || p.sent["model"] != "gemini-3.6-flash" {
		t.Fatalf("events: %+v, sent %v", es, p.sent)
	}
	collect(c, Request{Messages: []Message{{Role: User, Content: "q"}, {Role: Assistant, ToolCalls: []ToolCall{call}}}})
	sent := p.sent["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if sent["extra_content"].(map[string]any)["google"].(map[string]any)["thought_signature"] != "CiQBVKhc7g==" {
		t.Errorf("signature not replayed: %v", sent)
	}
}

func TestOpenRouterReasoning(t *testing.T) {
	p := newProvider(t, 200, "openrouter_reasoning.sse")
	c := p.chat(t, "openrouter")
	es := collect(c, Request{Messages: []Message{{Role: User, Content: "q"}}, Tools: tools})
	stop := last(es)
	if es[0].Call.Name != "list_groups" || stop.Replay == nil || stop.Replay.Provider != "openrouter" {
		t.Fatalf("events: %+v", es)
	}
	collect(c, Request{Messages: []Message{{Role: User, Content: "q"}, {Role: Assistant, ToolCalls: []ToolCall{es[0].Call}, Replay: stop.Replay}}})
	details := p.sent["messages"].([]any)[1].(map[string]any)["reasoning_details"].([]any)
	if len(details) != 1 || details[0].(map[string]any)["signature"] != "sig-1" {
		t.Errorf("reasoning not replayed: %v", p.sent["messages"])
	}
}

func TestOllamaLeavesToolsOut(t *testing.T) {
	p := newProvider(t, 200, "openai_text.sse")
	c, err := New(Config{Provider: "ollama", BaseURL: p.URL})
	if err != nil {
		t.Fatal(err)
	}
	collect(c, Request{Messages: []Message{{Role: User, Content: "q"}}, Tools: tools, NoTools: true})
	if p.sent["tools"] != nil || p.sent["tool_choice"] != nil || p.header.Get("Authorization") != "" || p.sent["model"] != "llama3.1" {
		t.Errorf("sent: %v %v", p.sent, p.header)
	}
	collect(c, Request{Messages: []Message{{Role: User, Content: "q"}}, Tools: tools})
	if p.sent["tools"] == nil {
		t.Error("tools left out of a turn that may call them")
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		name, provider, body string
		status               int
		code                 string
	}{
		{"401", "anthropic", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, 401, CodeAuth},
		{"429", "openai", `{"error":{"message":"Rate limit reached","type":"requests"}}`, 429, CodeRateLimited},
		{"529", "anthropic", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, 529, CodeOverloaded},
		{"400", "openai", `{"error":{"message":"Invalid 'messages[1]'"}}`, 400, CodeBadRequest},
		{"tools unsupported", "ollama", `{"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools"}}`, 400, CodeToolsUnsupported},
		{"gemini array", "gemini", `[{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}]`, 403, CodeAuth},
		{"500 no body", "openrouter", ``, 500, CodeProvider},
		{"mid-stream error", "openrouter", "openrouter_error.sse", 200, CodeProvider},
		{"mid-stream overloaded", "anthropic", "anthropic_overloaded.sse", 200, CodeOverloaded},
		{"cut stream", "anthropic", "anthropic_cut.sse", 200, CodeProvider},
		{"not JSON", "openai", "data: {nope\n\n", 200, CodeProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, tc.status, tc.body)
			es := collect(p.chat(t, tc.provider), Request{Messages: []Message{{Role: User, Content: "q"}}, Tools: tools})
			e := last(es)
			if e.Kind != ErrorKind || e.Code != tc.code || e.Message == "" || slices.ContainsFunc(es[:len(es)-1], func(e Event) bool { return e.Kind == ErrorKind }) {
				t.Errorf("events: %+v", es)
			}
			if strings.Contains(e.Message, "sk-test") {
				t.Errorf("the key in a message: %q", e.Message)
			}
		})
	}
	t.Run("network", func(t *testing.T) {
		p := newProvider(t, 200, "")
		c := p.chat(t, "openai")
		p.Close()
		if e := last(collect(c, Request{})); e.Code != CodeNetwork {
			t.Errorf("closed provider: %+v", e)
		}
	})
}

func TestCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	c, _ := New(Config{Provider: "anthropic", BaseURL: srv.URL, APIKey: []byte("k")})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	var es []Event
	for e := range c.Stream(ctx, Request{}) {
		es = append(es, e)
	}
	if len(es) != 1 || es[0].Code != CodeAborted {
		t.Errorf("cancelled: %+v", es)
	}
	pre, stop := context.WithCancel(context.Background())
	stop()
	for e := range c.Stream(pre, Request{}) {
		if e.Code != CodeAborted {
			t.Errorf("cancelled before: %+v", e)
		}
	}
}

func TestEarlyStop(t *testing.T) {
	p := newProvider(t, 200, "anthropic_text.sse")
	n := 0
	for range p.chat(t, "anthropic").Stream(context.Background(), Request{}) {
		n++
		break
	}
	if n != 1 {
		t.Errorf("events after break: %d", n)
	}
}

func TestEvents(t *testing.T) {
	in := ": comment\r\nevent: a\r\ndata: one\r\ndata:two\r\n\r\ndata: three\rid: 4\r\rdata: {}\n\n\n\ndata: last"
	var got []string
	if err := events(strings.NewReader(in), func(e event) bool { got = append(got, e.name+"="+e.data); return true }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a=one\ntwo", "message=three", "message={}", "message=last"}; !slices.Equal(got, want) {
		t.Errorf("events: %q, want %q", got, want)
	}
	if err := events(strings.NewReader("data: "+strings.Repeat("x", maxLine+1)), func(event) bool { return true }); !errors.Is(err, errLineTooLong) {
		t.Errorf("a line beyond the bound: %v", err)
	}
}

func TestNew(t *testing.T) {
	if _, err := New(Config{Provider: "acme"}); err == nil {
		t.Error("an unknown provider")
	}
	if _, err := New(Config{Provider: "openai"}); err == nil {
		t.Error("a provider without its key")
	}
	if c, err := New(Config{Provider: "ollama"}); err != nil || c.Model() != "llama3.1" || c.Provider() != "ollama" {
		t.Errorf("ollama: %v %v", c, err)
	}
	if !slices.Equal(Providers(), []string{"anthropic", "gemini", "ollama", "openai", "openrouter"}) || !Keyless("ollama") || Keyless("openai") {
		t.Errorf("providers: %v", Providers())
	}
}
