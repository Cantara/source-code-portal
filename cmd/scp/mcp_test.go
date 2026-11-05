package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// headers adds headers (a token, an Origin) to every request of the MCP client.
type headers struct {
	set  map[string]string
	next http.RoundTripper
}

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.set {
		r.Header.Set(k, v)
	}
	return h.next.RoundTrip(r)
}

func mcpConnect(t *testing.T, base string, set map[string]string) (*mcp.ClientSession, error) {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	s, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: base + "/mcp",
		HTTPClient: &http.Client{Transport: headers{set, transport}}, DisableStandaloneSSE: true}, nil)
	if err == nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, err
}

func mcpSession(t *testing.T, base, token string) *mcp.ClientSession {
	t.Helper()
	set := map[string]string{}
	if token != "" {
		set["Authorization"] = "Bearer " + token
	}
	s, err := mcpConnect(t, base, set)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestMCP: an MCP client lists the catalogue's tools, read-only, and calls them; the public does not see the private repository,
// a token of the private group does; a page of another site is refused, and a session cookie counts for nothing.
func TestMCP(t *testing.T) {
	get, base := privatePortalAt(t, "access: {private_groups: [staff]}\nauth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}\n")
	public := mcpSession(t, base, "")
	if init := public.InitializeResult(); init.ServerInfo.Name != "scp" || !strings.Contains(init.Instructions, "untrusted-content") {
		t.Errorf("initialize = %+v", init)
	}
	list, err := public.ListTools(t.Context(), nil)
	if err != nil || len(list.Tools) < 8 {
		t.Fatalf("tools = %+v, %v", list, err)
	}
	for _, tool := range list.Tools {
		if a := tool.Annotations; a == nil || !a.ReadOnlyHint || a.OpenWorldHint == nil || *a.OpenWorldHint || tool.Title == "" {
			t.Errorf("%s annotations = %+v", tool.Name, a)
		}
	}
	call := func(s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return r
	}
	hidden := map[string]any{"owner": "acme", "name": "zq-hidden"}
	if r := call(public, "search", map[string]any{"query": "open"}); r.IsError || !strings.Contains(text(r), "acme/open") || strings.Contains(text(r), "zq-hidden") {
		t.Errorf("public search = %s", text(r))
	}
	if r := call(public, "repository_health", hidden); !r.IsError || !strings.Contains(text(r), "not found") {
		t.Errorf("public health of the private repository = %s", text(r))
	}
	if r := call(public, "search", map[string]any{"query": "open", "limit": "many"}); !r.IsError || !strings.Contains(text(r), "integer") {
		t.Errorf("a bad argument = %s", text(r))
	}
	staff := mcpSession(t, base, botSecret)
	if r := call(staff, "repository_health", hidden); r.IsError || !strings.Contains(text(r), "<untrusted-content") || !strings.Contains(text(r), "zq-hidden") {
		t.Errorf("token health = %s", text(r))
	}
	// The negotiated version is the newest the SDK speaks; the portal's own pages pass, a page of another site is refused.
	if v := public.InitializeResult().ProtocolVersion; v < "2026-07-28" {
		t.Errorf("protocol version %s", v)
	}
	if own, err := mcpConnect(t, base, map[string]string{"Origin": "https://scp.example"}); err != nil {
		t.Errorf("the portal's origin: %v", err)
	} else if l, err := own.ListTools(t.Context(), nil); err != nil || len(l.Tools) != len(list.Tools) {
		t.Errorf("the portal's origin lists %v, %v", l, err)
	}
	if _, err := mcpConnect(t, base, map[string]string{"Origin": "https://evil.example"}); err == nil || !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("another origin: %v", err)
	}
	if code, _, _ := get("/mcp", "Accept", "application/json, text/event-stream"); code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", code)
	}
}
