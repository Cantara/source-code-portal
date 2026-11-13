package people

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// TokensPattern is where a signed-in person reads, links and removes their GitHub tokens.
const TokensPattern = "/api/v1/me/github/tokens" //nolint:gosec // G101: an address, not a credential

// maxBody bounds a link request: a token and two names.
const maxBody = 4 << 10

// Routes are the people's own routes: GET their sources, owners and tokens; POST a token; DELETE one. Each needs a person signed in
// through OIDC, and a change comes from the portal's own pages (same origin).
func (p *People) Routes(publicURL string) []httpapi.Route {
	a := api{p: p, publicURL: publicURL}
	return []httpapi.Route{
		{Pattern: "GET " + TokensPattern, Handler: http.HandlerFunc(a.list)},
		{Pattern: "POST " + TokensPattern, MaxBody: maxBody, Handler: http.HandlerFunc(a.link)},
		{Pattern: "DELETE " + TokensPattern + "/{source}/{owner}", Handler: http.HandlerFunc(a.unlink)},
	}
}

type api struct {
	p         *People
	publicURL string
}

// who is the request's person, or false with the problem written.
func (a api) who(w http.ResponseWriter, r *http.Request) (Who, bool) {
	v := httpapi.ViewerOf(r.Context())
	if v.Via != "oidc" || v.User == "" {
		httpapi.WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Sign in to link your GitHub tokens.")
		return Who{}, false
	}
	return Who{Subject: v.User, Identity: v.Email, Name: v.Name}, true
}

// Owner is one organisation of a source: the link that makes its token, and the token linked for it.
type Owner struct {
	Owner     string `json:"owner"`
	CreateURL string `json:"createUrl"`
	Token     *Token `json:"token"`
}

// SourceTokens is one source read with people's tokens.
type SourceTokens struct {
	Source string  `json:"source"`
	WebURL string  `json:"webUrl"`
	Owners []Owner `json:"owners"`
}

type listBody struct {
	Sources     []SourceTokens `json:"sources"`
	Permissions []string       `json:"permissions"`
}

func (a api) list(w http.ResponseWriter, r *http.Request) {
	who, ok := a.who(w, r)
	if !ok {
		return
	}
	if err := a.p.Seen(r.Context(), who); err != nil {
		httpapi.WriteProblem(w, http.StatusServiceUnavailable, "unavailable", "The portal could not record your sign-in. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, listBody{Sources: a.p.Overview(who), Permissions: Permissions})
}

// Overview is every source and owner the portal reads with w's token for each, or none.
func (p *People) Overview(w Who) []SourceTokens {
	have := map[slot]Token{}
	for _, t := range p.Tokens(w) {
		have[slot{t.Source, t.Owner}] = t
	}
	out := []SourceTokens{}
	for _, src := range p.o.Sources {
		st := SourceTokens{Source: src.ID, WebURL: src.WebURL, Owners: []Owner{}}
		for _, o := range src.Owners {
			ow := Owner{Owner: o, CreateURL: CreationLink(src.WebURL, o, "Read-only access for the Source Code Portal at "+p.o.Portal)}
			if t, ok := have[slot{src.ID, lower(o)}]; ok {
				ow.Token = &t
			}
			st.Owners = append(st.Owners, ow)
		}
		out = append(out, st)
	}
	return out
}

type linkBody struct {
	Source string `json:"source"`
	Owner  string `json:"owner"`
	Token  string `json:"token"`
}

func (a api) link(w http.ResponseWriter, r *http.Request) {
	who, ok := a.who(w, r)
	if !ok {
		return
	}
	if !httpapi.SameOrigin(r, a.publicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Link your token from the portal's own pages.")
		return
	}
	if !httpapi.RequireJSON(w, r, "the token") {
		return
	}
	var body linkBody
	if !httpapi.DecodeStrict(w, r, &body, "The body is not a token to link.") {
		return
	}
	t, err := a.p.Link(r.Context(), who, body.Source, body.Owner, body.Token)
	var refused *Refusal
	switch {
	case errors.As(err, &refused):
		httpapi.WriteProblem(w, http.StatusUnprocessableEntity, refused.Code, refused.Detail)
	case err != nil:
		httpapi.WriteProblem(w, http.StatusServiceUnavailable, "unavailable", "The token could not be kept. Try again.")
	default:
		writeJSON(w, http.StatusCreated, t)
	}
}

func (a api) unlink(w http.ResponseWriter, r *http.Request) {
	who, ok := a.who(w, r)
	if !ok {
		return
	}
	if !httpapi.SameOrigin(r, a.publicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Remove your token from the portal's own pages.")
		return
	}
	if err := a.p.Unlink(r.Context(), who, r.PathValue("source"), r.PathValue("owner")); err != nil {
		httpapi.WriteProblem(w, http.StatusServiceUnavailable, "unavailable", "The token could not be removed. Try again.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
