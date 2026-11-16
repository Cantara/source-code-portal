package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
)

// decoder turns one wire format's events into chat events for one turn.
type decoder interface {
	accept(e event) []Event
	end() []Event // the stream ended before the decoder was done
	done() bool
}

type transport struct {
	client *http.Client
	url    string
	key    string
}

// maxErrorBody bounds the error body read from a provider.
const maxErrorBody = 64 << 10

// stream posts body and yields the decoded events; any failure is one error event, and the stream stops with it.
func (t transport) stream(ctx context.Context, header http.Header, body any, dec decoder, yield func(Event) bool) {
	if ctx.Err() != nil {
		yield(aborted())
		return
	}
	b, err := json.Marshal(body)
	if err != nil {
		yield(errorEvent(CodeBadRequest, "the request could not be encoded: "+err.Error(), 0))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(b))
	if err != nil {
		yield(errorEvent(CodeBadRequest, err.Error(), 0))
		return
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := t.client.Do(req)
	if err != nil {
		yield(failure(ctx, err))
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		yield(fromResponse(res))
		return
	}
	stopped := false
	err = events(res.Body, func(e event) bool {
		for _, ev := range dec.accept(e) {
			if !yield(ev) {
				stopped = true
				return false
			}
		}
		return !dec.done()
	})
	switch {
	case stopped || dec.done():
	case ctx.Err() != nil:
		yield(aborted())
	case err != nil:
		yield(failure(ctx, err))
	default:
		for _, ev := range dec.end() {
			if !yield(ev) {
				return
			}
		}
	}
}

func aborted() Event { return errorEvent(CodeAborted, "the request was cancelled", 0) }

func failure(ctx context.Context, err error) Event {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return aborted()
	}
	return errorEvent(CodeNetwork, err.Error(), 0)
}

func earlyEnd() Event { return errorEvent(CodeProvider, "the provider ended the stream early", 0) }

func malformed() Event {
	return errorEvent(CodeProvider, "the provider sent an event that is not JSON", 0)
}

var toolsUnsupported = regexp.MustCompile(`(?i)does(?: not|n't) support (?:tools|tool use|tool calling|function calling)|no endpoints found that support tool use|tools? (?:is|are) not supported|function calling is not (?:enabled|supported)`)

// code is the error code of an HTTP status and the provider's message.
func code(status int, message string) string {
	switch {
	case (status == 400 || status == 404 || status == 422) && toolsUnsupported.MatchString(message):
		return CodeToolsUnsupported
	case status == 401 || status == 403:
		return CodeAuth
	case status == 429:
		return CodeRateLimited
	case status == 503 || status == 529:
		return CodeOverloaded
	case status >= 400 && status < 500:
		return CodeBadRequest
	}
	return CodeProvider
}

func fromResponse(res *http.Response) Event {
	b, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	msg := errorMessage(b)
	if msg == "" {
		msg = "the provider answered " + res.Status
	}
	return errorEvent(code(res.StatusCode, msg), msg, res.StatusCode)
}

// errorMessage is error.message of an error body: {"error": {"message"}}, {"error": "…"}, or either in an array (Gemini).
func errorMessage(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	if a, ok := v.([]any); ok && len(a) > 0 {
		v = a[0]
	}
	m, _ := v.(map[string]any)
	switch e := m["error"].(type) {
	case string:
		return e
	case map[string]any:
		s, _ := e["message"].(string)
		return s
	}
	return ""
}

// object parses an event's data as a JSON object.
func object(data string) (map[string]any, bool) {
	var m map[string]any
	if json.Unmarshal([]byte(data), &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

// input parses streamed tool arguments: empty is {}, anything not a JSON object is {} with the reason.
func input(args string) (map[string]any, string) {
	if len(bytes.TrimSpace([]byte(args))) == 0 {
		return map[string]any{}, ""
	}
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return map[string]any{}, "the tool arguments were not valid JSON: " + err.Error()
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}, "the tool arguments were not a JSON object"
	}
	return m, ""
}

func rec(m map[string]any, k string) map[string]any {
	v, _ := m[k].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

func num(m map[string]any, k string) (int, bool) {
	v, ok := m[k].(float64)
	return int(v), ok
}
