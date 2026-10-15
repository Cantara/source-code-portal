// Package llm streams tool-using chat from a language-model provider: two wire formats (Anthropic Messages and the OpenAI-compatible
// Chat Completions) over net/http, one adapter each, for five providers. Every failure arrives as one error event.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Role is who wrote a message.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	ToolRole  Role = "tool" // a tool's result
)

// Message is one message of a conversation.
type Message struct {
	Role    Role
	Content string
	// An assistant's tool calls, and the provider state its turn must be replayed with (Replay, opaque).
	ToolCalls []ToolCall
	Replay    *Replay
	// A tool result's call, and whether the tool failed.
	ToolCallID string
	IsError    bool
}

// ToolCall is a call the model asks for.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
	// InputError says why the arguments were not a JSON object (Input is then empty).
	InputError string
	// Extra holds fields of the call the provider wants back unchanged (Gemini's thought signatures).
	Extra map[string]json.RawMessage
}

// Replay is provider state carried on an assistant turn within one request: Anthropic's thinking blocks, OpenRouter's reasoning.
type Replay struct {
	Provider string
	Data     json.RawMessage
}

// Tool is a function the model may call; Schema is its parameters' JSON Schema.
type Tool struct {
	Name, Description string
	Schema            map[string]any
}

// Request is one model turn.
type Request struct {
	System    string
	Messages  []Message
	Tools     []Tool
	NoTools   bool // the turn must answer: tools are declared (when the history holds calls) but not callable
	MaxTokens int  // output tokens; 0: 4096
}

// Kind is an event's kind.
type Kind string

const (
	Text      Kind = "text"
	Call      Kind = "tool_call"
	Usage     Kind = "usage"
	Stop      Kind = "stop"
	ErrorKind Kind = "error"
)

// Error codes.
const (
	CodeAuth             = "auth"
	CodeRateLimited      = "rate_limited"
	CodeOverloaded       = "overloaded"
	CodeBadRequest       = "bad_request"
	CodeToolsUnsupported = "tools_unsupported"
	CodeNetwork          = "network"
	CodeAborted          = "aborted"
	CodeProvider         = "provider"
)

// Stop reasons.
const (
	EndTurn   = "end_turn"
	ToolUse   = "tool_use"
	MaxTokens = "max_tokens"
	Refusal   = "refusal"
	Other     = "other"
)

// Event is one thing a turn produced: Text, a Call, the Usage, then Stop; or one Error, after which nothing follows.
type Event struct {
	Kind                Kind
	Text                string
	Call                ToolCall
	InputTokens, Output int
	Reason              string  // Stop
	Replay              *Replay // Stop: the state the turn must be replayed with
	Code, Message       string  // Error
	Status              int     // Error: the HTTP status, when there was one
}

// Chat is a provider's model.
type Chat interface {
	Stream(ctx context.Context, r Request) iter.Seq[Event]
	Provider() string
	Model() string
}

// Config names the provider; Model and BaseURL default to the provider's.
type Config struct {
	Provider, Model, BaseURL string
	APIKey                   []byte
	Client                   *http.Client // nil: one without a total timeout (streams run long), a 30 s wait for headers
}

type preset struct {
	base, model, path string
	anthropic         bool
	maxParam          string // max_tokens or max_completion_tokens
	omitTools         bool   // ollama: a turn without tools leaves them out instead of tool_choice none
	keyless           bool
}

var presets = map[string]preset{ //nolint:gochecknoglobals // lookup table
	"anthropic":  {base: "https://api.anthropic.com", model: "claude-haiku-5-5", path: "/v1/messages", anthropic: true},
	"openai":     {base: "https://api.openai.com/v1", model: "gpt-4o", path: "/chat/completions", maxParam: "max_completion_tokens"},
	"gemini":     {base: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-3.6-flash", path: "/chat/completions", maxParam: "max_tokens"},
	"openrouter": {base: "https://openrouter.ai/api/v1", model: "anthropic/claude-haiku-4.5", path: "/chat/completions", maxParam: "max_tokens"},
	"ollama":     {base: "http://localhost:11434/v1", model: "llama3.1", path: "/chat/completions", maxParam: "max_tokens", omitTools: true, keyless: true},
}

// Providers are the provider names, sorted.
func Providers() []string {
	out := make([]string, 0, len(presets))
	for p := range presets {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Keyless reports whether the provider works without a key.
func Keyless(provider string) bool { return presets[provider].keyless }

// New returns the chat of c.
func New(c Config) (Chat, error) {
	p, ok := presets[c.Provider]
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider %q", c.Provider)
	}
	if len(c.APIKey) == 0 && !p.keyless {
		return nil, fmt.Errorf("llm: provider %s needs a key", c.Provider)
	}
	if c.Model == "" {
		c.Model = p.model
	}
	if c.BaseURL == "" {
		c.BaseURL = p.base
	}
	if c.Client == nil {
		c.Client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true}}
	}
	t := transport{client: c.Client, url: strings.TrimSuffix(c.BaseURL, "/") + p.path, key: string(c.APIKey)}
	if p.anthropic {
		return &anthropic{t: t, provider: c.Provider, model: c.Model}, nil
	}
	return &openAI{t: t, provider: c.Provider, model: c.Model, maxParam: p.maxParam, omitTools: p.omitTools}, nil
}

func errorEvent(code, message string, status int) Event {
	return Event{Kind: ErrorKind, Code: code, Message: message, Status: status}
}

const defaultMaxTokens = 4096

func maxTokens(r Request) int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return defaultMaxTokens
}
