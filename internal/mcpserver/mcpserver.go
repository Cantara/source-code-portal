// Package mcpserver serves the tool catalogue over the Model Context Protocol (design/agents.md, W2): /mcp in Streamable HTTP,
// stateless, with JSON answers. A call answers from the request's viewer: a bearer token's, the trusted proxy's, or the public view;
// never a browser's session cookie.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/tools"
)

// Path is the endpoint: POST, and GET and DELETE answered 405 by the stateless transport.
const Path = "/mcp"

const maxBody = 64 << 10

// Options configures the endpoint.
type Options struct {
	Catalogue   *tools.Catalogue
	Version     string // the release, in initialize's serverInfo
	PublicURL   string // the portal's origin; a browser Origin other than it is refused
	RequireAuth bool   // no public view: a call without a token or the proxy's identity is refused
	Log         *slog.Logger
}

// Routes is /mcp.
func Routes(o Options) []httpapi.Route {
	s := mcp.NewServer(&mcp.Implementation{Name: "scp", Title: "Source Code Portal", Version: o.Version},
		&mcp.ServerOptions{Instructions: tools.Instructions, Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}})
	no := false
	for _, t := range o.Catalogue.Tools() {
		s.AddTool(&mcp.Tool{Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: t.Schema,
			Annotations: &mcp.ToolAnnotations{Title: t.Title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &no}},
			handler(o.Catalogue, t.Name))
	}
	// The portal runs behind a proxy that reaches it on a loopback address with the public host name: the SDK's localhost guard
	// would refuse every call there. The Origin check below is the DNS rebinding guard instead.
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true, Logger: o.Log})
	g := guard(o, h)
	return []httpapi.Route{{Pattern: "POST " + Path, Handler: g, MaxBody: maxBody}, {Pattern: "GET " + Path, Handler: g},
		{Pattern: "DELETE " + Path, Handler: g}}
}

// guard refuses a browser call from another origin and takes away a session cookie's identity: a page of another site can neither
// reach the endpoint nor ride a reader's session into it.
func guard(o Options, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, o.PublicURL, r) {
			httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "The MCP endpoint is not for pages of other sites.")
			return
		}
		if v := httpapi.ViewerOf(r.Context()); v.Via == "oidc" {
			if o.RequireAuth {
				w.Header().Set("WWW-Authenticate", `Bearer realm="scp"`)
				httpapi.WriteProblem(w, http.StatusUnauthorized, "unauthorized", "Call the MCP endpoint with an access token.")
				return
			}
			r = r.WithContext(httpapi.WithViewer(r.Context(), httpapi.Viewer{}))
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether origin is the portal's: its public URL's, else the request's own host.
func sameOrigin(origin, publicURL string, r *http.Request) bool {
	want := ""
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		want = u.Scheme + "://" + u.Host
	} else {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		want = scheme + "://" + r.Host
	}
	return strings.EqualFold(strings.TrimSuffix(origin, "/"), want)
}

func handler(c *tools.Catalogue, name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input := map[string]any{}
		if raw := req.Params.Arguments; len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &input); err != nil {
				return failed("The arguments are not a JSON object."), nil
			}
		}
		res, err := c.Call(ctx, name, input, 0)
		var ie *tools.InputError
		switch {
		case errors.As(err, &ie):
			return failed(ie.Msg), nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			return failed(err.Error()), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: tools.Frame(tools.Nonce(), res.Source, res.Text)}}}, nil
	}
}

func failed(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}
