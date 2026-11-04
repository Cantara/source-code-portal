package tools

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// CallPattern is the tool call route, outside the generated handlers: it needs the request for the same-origin rule.
const CallPattern = "POST /api/v1/tools/{name}"

const maxCallBody = 16 << 10

// API serves the catalogue: the listing in the contract and the call as a Route.
type API struct {
	Catalogue *Catalogue
	PublicURL string // the portal's origin, for the same-origin rule
}

// Annotations are every tool's: it only reads, and what it returns holds text repositories wrote.
var Annotations = oapi.ToolAnnotations{ReadOnly: true, UntrustedContent: true} //nolint:gochecknoglobals // a constant

// List is the catalogue as the contract gives it.
func (c *Catalogue) List() oapi.ToolList {
	out := oapi.ToolList{Tools: make([]oapi.Tool, 0, len(c.list))}
	for _, t := range c.list {
		out.Tools = append(out.Tools, oapi.Tool{Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: t.Schema, Annotations: Annotations})
	}
	return out
}

// ListTools answers GET /api/v1/tools.
func (a API) ListTools(context.Context, oapi.ListToolsRequestObject) (oapi.ListToolsResponseObject, error) {
	return oapi.ListTools200JSONResponse(a.Catalogue.List()), nil
}

// Routes is the call route.
func (a API) Routes() []httpapi.Route {
	return []httpapi.Route{{Pattern: CallPattern, Handler: http.HandlerFunc(a.call), MaxBody: maxCallBody}}
}

// Answer is a call's answer (the contract's ToolResult, served outside the generated handlers).
type Answer struct {
	Source  string `json:"source"`
	Text    string `json:"text"`
	IsError bool   `json:"isError"`
}

func (a API) call(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		httpapi.WriteProblem(w, http.StatusUnsupportedMediaType, "unsupported-media-type", "Send the input as application/json.")
		return
	}
	if !httpapi.SameOrigin(r, a.PublicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "A tool is called from the portal's own pages, or with a token.")
		return
	}
	var body struct {
		Input map[string]any `json:"input"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "validation-failed", `The body is not {"input": {...}}.`)
		return
	}
	res, err := a.Catalogue.Call(r.Context(), r.PathValue("name"), body.Input, 0)
	var ie *InputError
	switch {
	case errors.Is(err, ErrUnknown):
		httpapi.WriteProblem(w, http.StatusNotFound, "not-found", "There is no tool of that name; GET /api/v1/tools lists them.")
		return
	case errors.As(err, &ie):
		httpapi.WriteProblem(w, http.StatusBadRequest, "validation-failed", ie.Msg)
		return
	case r.Context().Err() != nil:
		httpapi.WriteProblem(w, http.StatusServiceUnavailable, "unavailable", "The call took too long.")
		return
	}
	out := Answer{Source: res.Source, Text: Frame(Nonce(), res.Source, res.Text)}
	if err != nil {
		out = Answer{Text: err.Error(), IsError: true}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
