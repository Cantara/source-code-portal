package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"slices"
	"strings"
)

// openAI is the OpenAI-compatible Chat Completions API (OpenAI, Gemini, OpenRouter, Ollama). Tool calls are accumulated across
// chunks by index (by id without one); Gemini's extra_content and OpenRouter's reasoning_details are kept and replayed unchanged.
type openAI struct {
	t         transport
	provider  string
	model     string
	maxParam  string
	omitTools bool
}

func (o *openAI) Provider() string { return o.provider }
func (o *openAI) Model() string    { return o.model }

func (o *openAI) Stream(ctx context.Context, r Request) iter.Seq[Event] {
	return func(yield func(Event) bool) {
		h := http.Header{}
		if o.t.key != "" {
			h.Set("Authorization", "Bearer "+o.t.key)
		}
		o.t.stream(ctx, h, o.body(r), &openAITurn{provider: o.provider}, yield)
	}
}

func (o *openAI) body(r Request) map[string]any {
	body := map[string]any{"model": o.model, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"messages": openAIMessages(r, o.provider), o.maxParam: maxTokens(r)}
	if len(r.Tools) > 0 && (!r.NoTools || !o.omitTools) { // a provider that ignores tool_choice is kept from calls by not offering tools
		tools := make([]map[string]any, 0, len(r.Tools))
		for _, t := range r.Tools {
			tools = append(tools, map[string]any{"type": "function",
				"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}})
		}
		body["tools"], body["tool_choice"] = tools, "auto"
		if r.NoTools {
			body["tool_choice"] = "none"
		}
	}
	return body
}

func openAIMessages(r Request, provider string) []map[string]any {
	var out []map[string]any
	if r.System != "" {
		out = append(out, map[string]any{"role": "system", "content": r.System})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case ToolRole:
			out = append(out, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		case Assistant:
			w := map[string]any{"role": "assistant", "content": nil}
			if m.Content != "" {
				w["content"] = m.Content
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, c := range m.ToolCalls {
					args, _ := json.Marshal(c.Input)
					call := map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(args)}}
					for k, v := range c.Extra {
						call[k] = v
					}
					calls = append(calls, call)
				}
				w["tool_calls"] = calls
			}
			if m.Replay != nil && m.Replay.Provider == provider {
				var extra map[string]json.RawMessage
				if json.Unmarshal(m.Replay.Data, &extra) == nil {
					for k, v := range extra {
						w[k] = v
					}
				}
			}
			out = append(out, w)
		default:
			out = append(out, map[string]any{"role": "user", "content": m.Content})
		}
	}
	return out
}

var openAIStops = map[string]string{"stop": EndTurn, "tool_calls": ToolUse, "function_call": ToolUse, "length": MaxTokens, //nolint:gochecknoglobals // lookup table
	"content_filter": Refusal}

type slot struct {
	index    int
	hasIndex bool
	id, name string
	args     strings.Builder
	extra    map[string]json.RawMessage
}

type openAITurn struct {
	provider      string
	calls         []*slot
	reasoning     []json.RawMessage
	finish        string
	input, output int
	finished      bool
}

func (d *openAITurn) done() bool { return d.finished }

// end completes a stream closed without [DONE] once a finish reason arrived.
func (d *openAITurn) end() []Event {
	if d.finish == "" {
		return []Event{earlyEnd()}
	}
	return d.complete()
}

func (d *openAITurn) accept(e event) []Event {
	if strings.TrimSpace(e.data) == "[DONE]" {
		return d.complete()
	}
	m, ok := object(e.data)
	if !ok {
		d.finished = true
		return []Event{malformed()}
	}
	if er, ok := m["error"]; ok && er != nil {
		d.finished = true
		return []Event{streamError(er)}
	}
	if u := rec(m, "usage"); len(u) > 0 {
		if n, ok := num(u, "prompt_tokens"); ok {
			d.input = n
		}
		if n, ok := num(u, "completion_tokens"); ok {
			d.output = n
		}
	}
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}
	delta := rec(choice, "delta")
	d.collect(delta["tool_calls"])
	if rd, ok := delta["reasoning_details"].([]any); ok {
		for _, x := range rd {
			b, _ := json.Marshal(x)
			d.reasoning = append(d.reasoning, b)
		}
	}
	if f := str(choice, "finish_reason"); f != "" {
		d.finish = f
	}
	if t := str(delta, "content"); t != "" {
		return []Event{{Kind: Text, Text: t}}
	}
	return nil
}

func (d *openAITurn) collect(v any) {
	items, _ := v.([]any)
	for _, it := range items {
		item, _ := it.(map[string]any)
		if item == nil {
			continue
		}
		s := d.slotFor(item)
		if id := str(item, "id"); id != "" {
			s.id = id
		}
		fn := rec(item, "function")
		if n := str(fn, "name"); n != "" {
			s.name = n
		}
		switch a := fn["arguments"].(type) {
		case string:
			s.args.WriteString(a)
		case map[string]any:
			b, _ := json.Marshal(a)
			s.args.Write(b)
		}
		if ec, ok := item["extra_content"].(map[string]any); ok {
			b, _ := json.Marshal(ec)
			if s.extra == nil {
				s.extra = map[string]json.RawMessage{}
			}
			s.extra["extra_content"] = b
		}
	}
}

// slotFor is the call a chunk continues: by index, else by id, else a new one.
func (d *openAITurn) slotFor(item map[string]any) *slot {
	idx, hasIndex := num(item, "index")
	id := str(item, "id")
	for _, s := range d.calls {
		if hasIndex && s.hasIndex && s.index == idx || !hasIndex && !s.hasIndex && id != "" && s.id == id {
			return s
		}
	}
	s := &slot{index: idx, hasIndex: hasIndex}
	d.calls = append(d.calls, s)
	return s
}

func (d *openAITurn) complete() []Event {
	d.finished = true
	ordered := slices.Clone(d.calls)
	slices.SortStableFunc(ordered, func(a, b *slot) int {
		if a.hasIndex && b.hasIndex {
			return a.index - b.index
		}
		return 0
	})
	var out []Event
	for i, s := range ordered {
		in, bad := input(s.args.String())
		id := s.id
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out = append(out, Event{Kind: Call, Call: ToolCall{ID: id, Name: s.name, Input: in, InputError: bad, Extra: s.extra}})
	}
	out = append(out, Event{Kind: Usage, InputTokens: d.input, Output: d.output})
	stop := Event{Kind: Stop, Reason: Other}
	if r, ok := openAIStops[d.finish]; ok {
		stop.Reason = r
	}
	if len(ordered) > 0 {
		stop.Reason = ToolUse
	}
	if len(d.reasoning) > 0 {
		data, _ := json.Marshal(map[string]any{"reasoning_details": d.reasoning})
		stop.Replay = &Replay{Provider: d.provider, Data: data}
	}
	return append(out, stop)
}

// streamError is a mid-stream {"error"} chunk (OpenRouter).
func streamError(v any) Event {
	m, _ := v.(map[string]any)
	msg := str(m, "message")
	if s, ok := v.(string); ok {
		msg = s
	}
	if msg == "" {
		msg = "the provider reported an error mid-stream"
	}
	if status, ok := num(m, "code"); ok && status > 0 {
		return errorEvent(code(status, msg), msg, status)
	}
	if toolsUnsupported.MatchString(msg) {
		return errorEvent(CodeToolsUnsupported, msg, 0)
	}
	return errorEvent(CodeProvider, msg, 0)
}
