// Package assistant is the feature that answers questions about the catalogue (design/assistant.md): a streaming chat with a
// language model that calls read-only tools over the viewer's view of the snapshot. It writes nothing and stores nothing.
package assistant

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/llm"
)

// Routes of the assistant outside the generated handlers.
const (
	ChatPattern = "POST /api/v1/assistant/chat"
	TestPattern = "POST /api/v1/admin/assistant/test"
)

// Tool is a read-only tool, wired in cmd/scp over the viewer's view (Run gets the request's context).
type Tool struct {
	llm.Tool
	// Label says what a call reads, for the viewer: "Reading acme/core".
	Label func(input map[string]any) string
	// Run returns what the tool read and where from (a path of the portal); an error is told to the model.
	Run func(ctx context.Context, input map[string]any) (source, text string, err error)
}

// Options configures the assistant; a nil Chat turns it off.
type Options struct {
	Chat   llm.Chat
	Tools  []Tool
	Access string // signed-in, admin or everyone
	Budget Budget
	// PublicURL is the portal's origin for the CSRF rule and the system prompt.
	PublicURL string
	// Groups names the viewer's groups for the system prompt.
	Groups func(ctx context.Context) []string
	// Observe reports a finished request: its outcome (answered, error code, refused) and tokens.
	Observe func(outcome string, input, output int)
	Now     func() time.Time
	Log     *slog.Logger
}

// Budget bounds one request.
type Budget struct {
	Concurrency, MaxTurns, MaxToolCalls, MaxTokens, MaxHistory, MaxInputChars, ToolResultChars int
}

// Assistant is the feature.
type Assistant struct {
	o     Options
	slots chan struct{}
	ping  time.Duration
}

// New returns the assistant of o.
func New(o Options) *Assistant {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Observe == nil {
		o.Observe = func(string, int, int) {}
	}
	if o.Groups == nil {
		o.Groups = func(context.Context) []string { return nil }
	}
	if o.Budget.Concurrency < 1 {
		o.Budget.Concurrency = 1
	}
	return &Assistant{o: o, slots: make(chan struct{}, o.Budget.Concurrency), ping: 15 * time.Second}
}

// Enabled reports whether a provider is configured.
func (a *Assistant) Enabled() bool { return a.o.Chat != nil }

// allowed reports whether the viewer may ask, and if not, the problem.
func (a *Assistant) allowed(v httpapi.Viewer) (bool, int, string) {
	switch a.o.Access {
	case "everyone":
		return true, 0, ""
	case "admin":
		if v.Admin {
			return true, 0, ""
		}
	default:
		if v.SignedIn() {
			return true, 0, ""
		}
	}
	if !v.SignedIn() {
		return false, http.StatusUnauthorized, "unauthenticated"
	}
	return false, http.StatusForbidden, "forbidden"
}

// GetAssistant implements the contract's /assistant.
func (a *Assistant) GetAssistant(ctx context.Context, _ oapi.GetAssistantRequestObject) (oapi.GetAssistantResponseObject, error) {
	st := oapi.AssistantStatus{Access: oapi.AssistantStatusAccess(a.o.Access), Enabled: a.Enabled()}
	if st.Access == "" {
		st.Access = oapi.SignedIn
	}
	if a.Enabled() {
		st.Allowed, _, _ = a.allowed(httpapi.ViewerOf(ctx))
		st.Provider, st.Model = a.o.Chat.Provider(), a.o.Chat.Model()
	}
	return oapi.GetAssistant200JSONResponse(st), nil
}

// Routes are the chat and the provider test; none when the assistant is off.
func (a *Assistant) Routes() []httpapi.Route {
	if !a.Enabled() {
		return nil
	}
	maxBody := int64(a.o.Budget.MaxInputChars)*4 + 64<<10 // four bytes a character at most, and the JSON around
	return []httpapi.Route{
		{Pattern: ChatPattern, Handler: http.HandlerFunc(a.chat), MaxBody: maxBody, Stream: true},
		{Pattern: TestPattern, Handler: httpapi.AdminOnly(http.HandlerFunc(a.test)), MaxBody: 1 << 10},
	}
}

type chatBody struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func (a *Assistant) chat(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		a.refuse(w, http.StatusUnsupportedMediaType, "unsupported-media-type", "Send the conversation as application/json.")
		return
	}
	if !httpapi.SameOrigin(r, a.o.PublicURL) {
		a.refuse(w, http.StatusForbidden, "cross-origin", "Ask from the portal's own pages.")
		return
	}
	if ok, status, code := a.allowed(httpapi.ViewerOf(r.Context())); !ok {
		detail := "Sign in to ask the assistant."
		if status == http.StatusForbidden {
			detail = "The assistant is for administrators."
		}
		a.refuse(w, status, code, detail)
		return
	}
	var body chatBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		a.refuse(w, http.StatusBadRequest, "validation-failed", "The body is not a conversation.")
		return
	}
	history, detail := a.history(body)
	if detail != "" {
		a.refuse(w, http.StatusBadRequest, "validation-failed", detail)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		w.Header().Set("Retry-After", "5")
		a.refuse(w, http.StatusTooManyRequests, "too-many-requests", "The assistant is busy; try again in a few seconds.")
		return
	}
	a.answer(w, r, history)
}

func (a *Assistant) refuse(w http.ResponseWriter, status int, code, detail string) {
	a.o.Observe("refused", 0, 0)
	httpapi.WriteProblem(w, status, code, detail)
}

// history checks the conversation: user and assistant messages taking turns, a question last, within the budgets.
func (a *Assistant) history(b chatBody) ([]llm.Message, string) {
	n := len(b.Messages)
	if n == 0 || n > a.o.Budget.MaxHistory {
		return nil, "The conversation holds 1 to " + strconv.Itoa(a.o.Budget.MaxHistory) + " messages."
	}
	chars := 0
	out := make([]llm.Message, 0, n)
	for i, m := range b.Messages {
		want := "user"
		if (n-1-i)%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			return nil, "The messages take turns, the viewer's question last."
		}
		if m.Role == "user" && m.Content == "" {
			return nil, "A question is empty."
		}
		chars += len([]rune(m.Content))
		out = append(out, llm.Message{Role: llm.Role(m.Role), Content: m.Content})
	}
	if chars > a.o.Budget.MaxInputChars {
		return nil, "The conversation is longer than " + strconv.Itoa(a.o.Budget.MaxInputChars) + " characters; start a new one."
	}
	return out, ""
}

type testResult struct {
	OK        bool   `json:"ok"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latencyMs"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// test asks the provider for one short completion, for an administrator.
func (a *Assistant) test(w http.ResponseWriter, r *http.Request) {
	if !httpapi.SameOrigin(r, a.o.PublicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Test from the portal's own pages.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res := testResult{Provider: a.o.Chat.Provider(), Model: a.o.Chat.Model(), OK: true}
	start := a.o.Now()
	for e := range a.o.Chat.Stream(ctx, llm.Request{MaxTokens: 16, Messages: []llm.Message{{Role: llm.User, Content: "Reply with OK."}}}) {
		if e.Kind == llm.ErrorKind {
			res.OK, res.Code, res.Message = false, e.Code, e.Message
		}
	}
	res.LatencyMs = a.o.Now().Sub(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}
