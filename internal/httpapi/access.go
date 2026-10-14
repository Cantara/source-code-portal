package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"
)

// Access says who sees private repositories: the user and groups headers a trusted authenticating proxy sets, and the groups whose
// members are admitted ("*" admits every authenticated user).
type Access struct {
	UserHeader, GroupsHeader string
	PrivateGroups            []string
	Vary                     bool // private repositories are admitted: every response varies on the two headers
}

type privateKey struct{}

// Private reports whether the request's viewer sees private repositories. The middleware decides it once per request.
func Private(ctx context.Context) bool {
	p, _ := ctx.Value(privateKey{}).(bool)
	return p
}

// private decides the viewer of r. An API call of the web UI server names the viewer of its page with X-Scp-Viewer, believed only
// with the right X-Scp-Token. The identity headers count only from a trusted proxy; from any other peer they are removed, so nothing
// behind the middleware reads a forged identity.
func (o Options) private(r *http.Request) bool {
	token, viewer := r.Header.Get(headerToken), r.Header.Get(headerViewer)
	r.Header.Del(headerToken)
	r.Header.Del(headerViewer)
	if token != "" && o.WebToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(o.WebToken)) == 1 {
		return viewer == "private"
	}
	if o.Access.UserHeader == "" {
		return false
	}
	if !o.fromTrusted(r) {
		r.Header.Del(o.Access.UserHeader)
		r.Header.Del(o.Access.GroupsHeader)
		return false
	}
	if strings.TrimSpace(r.Header.Get(o.Access.UserHeader)) == "" || len(o.Access.PrivateGroups) == 0 {
		return false
	}
	if slices.Contains(o.Access.PrivateGroups, "*") {
		return true
	}
	for _, v := range r.Header.Values(o.Access.GroupsHeader) {
		for g := range strings.SplitSeq(v, ",") {
			if g = strings.TrimSpace(g); g != "" && slices.Contains(o.Access.PrivateGroups, g) { // exact: group names are case-sensitive
				return true
			}
		}
	}
	return false
}
