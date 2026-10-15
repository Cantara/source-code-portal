package llm

import (
	"context"
	"encoding/json"
	"iter"
	"maps"
	"net/http"
	"slices"
)

// anthropic is the Messages API. Thinking blocks are assembled from the stream and replayed with the turn that produced them.
type anthropic struct {
	t        transport
	provider string
	model    string
}

func (a *anthropic) Provider() string { return a.provider }
func (a *anthropic) Model() string    { return a.model }

func (a *anthropic) Stream(ctx context.Context, r Request) iter.Seq[Event] {
	return func(yield func(Event) bool) {
		h := http.Header{}
		h.Set("x-api-key", a.t.key)
		h.Set("anthropic-version", "2023-06-01")
		a.t.stream(ctx, h, a.body(r), &anthropicTurn{blocks: map[int]*block{}}, yield)
	}
}

func (a *anthropic) body(r Request) map[string]any {
	body := map[string]any{"model": a.model, "max_tokens": maxTokens(r), "stream": true, "messages": anthropicMessages(r.Messages)}
	if r.System != "" {
		body["system"] = r.System
	}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, 0, len(r.Tools))
		for _, t := range r.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
		}
		choice := "auto"
		if r.NoTools {
			choice = "none"
		}
		body["tools"], body["tool_choice"] = tools, map[string]string{"type": choice}
	}
	return body
}

// anthropicMessages are the turns of the Messages API; consecutive tool results share one user turn.
func anthropicMessages(ms []Message) []map[string]any {
	var out []map[string]any
	var results []map[string]any
	flush := func() {
		if results != nil {
			out = append(out, map[string]any{"role": "user", "content": results})
			results = nil
		}
	}
	for _, m := range ms {
		switch m.Role {
		case ToolRole:
			r := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content}
			if m.IsError {
				r["is_error"] = true
			}
			results = append(results, r)
		case Assistant:
			flush()
			if m.Replay != nil && m.Replay.Provider == "anthropic" {
				out = append(out, map[string]any{"role": "assistant", "content": m.Replay.Data})
				continue
			}
			content := []map[string]any{}
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, c := range m.ToolCalls {
				content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Input})
			}
			out = append(out, map[string]any{"role": "assistant", "content": content})
		default:
			flush()
			out = append(out, map[string]any{"role": "user", "content": m.Content})
		}
	}
	flush()
	return out
}

type block struct {
	b    map[string]any
	json string // a tool call's arguments as they arrive
}

type anthropicTurn struct {
	blocks        map[int]*block
	input, output int
	reason        string
	calls         bool
	finished      bool
}

var anthropicStops = map[string]string{"end_turn": EndTurn, "stop_sequence": EndTurn, "tool_use": ToolUse, "max_tokens": MaxTokens, //nolint:gochecknoglobals // lookup table
	"model_context_window_exceeded": MaxTokens, "refusal": Refusal}

var anthropicErrors = map[string]string{"overloaded_error": CodeOverloaded, "rate_limit_error": CodeRateLimited, //nolint:gochecknoglobals // lookup table
	"authentication_error": CodeAuth, "permission_error": CodeAuth, "invalid_request_error": CodeBadRequest}

func (d *anthropicTurn) done() bool   { return d.finished }
func (d *anthropicTurn) end() []Event { return []Event{earlyEnd()} }

func (d *anthropicTurn) fail(e Event) []Event { d.finished = true; return []Event{e} }

func (d *anthropicTurn) accept(e event) []Event {
	if e.name == "ping" {
		return nil
	}
	m, ok := object(e.data)
	if !ok {
		return d.fail(malformed())
	}
	idx, _ := num(m, "index")
	switch e.name {
	case "message_start":
		d.input, _ = num(rec(rec(m, "message"), "usage"), "input_tokens")
	case "content_block_start":
		b := &block{b: maps.Clone(rec(m, "content_block"))}
		d.blocks[idx] = b
	case "content_block_delta":
		b := d.blocks[idx]
		if b == nil {
			return nil
		}
		delta := rec(m, "delta")
		switch str(delta, "type") {
		case "text_delta":
			t := str(delta, "text")
			b.b["text"] = str(b.b, "text") + t
			if t != "" {
				return []Event{{Kind: Text, Text: t}}
			}
		case "input_json_delta":
			b.json += str(delta, "partial_json")
		case "thinking_delta":
			b.b["thinking"] = str(b.b, "thinking") + str(delta, "thinking")
		case "signature_delta":
			b.b["signature"] = str(b.b, "signature") + str(delta, "signature")
		}
	case "content_block_stop":
		b := d.blocks[idx]
		if b == nil || str(b.b, "type") != "tool_use" {
			return nil
		}
		in, bad := input(b.json)
		b.b["input"] = in
		d.calls = true
		return []Event{{Kind: Call, Call: ToolCall{ID: str(b.b, "id"), Name: str(b.b, "name"), Input: in, InputError: bad}}}
	case "message_delta":
		if r := str(rec(m, "delta"), "stop_reason"); r != "" {
			d.reason = r
		}
		if n, ok := num(rec(m, "usage"), "output_tokens"); ok {
			d.output = n
		}
	case "message_stop":
		d.finished = true
		stop := Event{Kind: Stop, Reason: Other}
		if r, ok := anthropicStops[d.reason]; ok {
			stop.Reason = r
		}
		if d.calls {
			stop.Reason = ToolUse
			keys := slices.Sorted(maps.Keys(d.blocks))
			content := make([]map[string]any, 0, len(keys))
			for _, k := range keys {
				content = append(content, d.blocks[k].b)
			}
			data, _ := json.Marshal(content)
			stop.Replay = &Replay{Provider: "anthropic", Data: data}
		}
		return []Event{{Kind: Usage, InputTokens: d.input, Output: d.output}, stop}
	case "error":
		er := rec(m, "error")
		c, ok := anthropicErrors[str(er, "type")]
		if !ok {
			c = CodeProvider
		}
		msg := str(er, "message")
		if msg == "" {
			msg = "the provider reported " + str(er, "type")
		}
		return d.fail(errorEvent(c, msg, 0))
	}
	return nil
}
