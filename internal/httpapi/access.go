package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Access says who sees what: the user and groups headers a trusted authenticating proxy sets, the groups whose members see private
// repositories ("*" admits every identified viewer), and who is an admin.
type Access struct {
	UserHeader, GroupsHeader string
	PrivateGroups            []string
	AdminEmails, AdminGroups []string
	Vary                     bool // private repositories are admitted: every response varies on the identity headers
}

// Identity is who a request comes from, as a proxy, the session or a token names it.
type Identity struct {
	User   string   // the stable id: the provider's subject, the proxy's user, token:<name>
	Name   string   // what the web UI shows
	Email  string   // matched against Access.AdminEmails
	Groups []string // matched against the group lists
	Admin  bool     // granted by the credential itself (an admin token)
	Via    string   // proxy, oidc, token
}

// Authenticator identifies a request by its bearer token or session cookie (design/auth.md). It reports false when the request
// carries neither, and ErrUnauthenticated when it carries a credential that names nobody. It may refresh the session on w.
type Authenticator interface {
	Identify(w http.ResponseWriter, r *http.Request) (Identity, bool, error)
}

// ErrUnauthenticated is a credential that names nobody: the request is refused, never served anonymously.
var ErrUnauthenticated = errors.New("unauthenticated")

// Viewer is the request's viewer with its roles. The zero value is an anonymous viewer of the public view.
type Viewer struct {
	Identity
	Private bool // sees private repositories
	Admin   bool // sees the operator surfaces; implies Private
	web     bool // the web UI server's call for a page the middleware already let through
}

// SignedIn reports whether the viewer is identified.
func (v Viewer) SignedIn() bool { return v.Via != "" }

type viewerKey struct{}

// ViewerOf returns the request's viewer, decided once per request by the middleware.
func ViewerOf(ctx context.Context) Viewer {
	v, _ := ctx.Value(viewerKey{}).(Viewer)
	return v
}

// Private reports whether the request's viewer sees private repositories.
func Private(ctx context.Context) bool { return ViewerOf(ctx).Private }

// WithViewer returns ctx carrying v, for tests of handlers that read the viewer.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// The viewer values of X-Scp-Viewer.
const (
	viewerPublic  = "public"
	viewerPrivate = "private"
	viewerAdmin   = "admin"
)

// viewer decides the viewer of r, first match wins: the web UI server's API call (X-Scp-Viewer, believed only with the right
// X-Scp-Token), a bearer token or the session cookie, the trusted proxy's identity headers. The forwarded headers are removed from
// r, so nothing behind the middleware reads a forged identity.
func (o Options) viewer(w http.ResponseWriter, r *http.Request) (Viewer, error) {
	token, kind, name, via := r.Header.Get(headerToken), r.Header.Get(headerViewer), r.Header.Get(headerName), r.Header.Get(headerVia)
	for _, h := range []string{headerToken, headerViewer, headerName, headerVia} {
		r.Header.Del(h)
	}
	proxied := o.Access.UserHeader != "" && o.fromTrusted(r)
	user, groups := r.Header.Get(o.Access.UserHeader), r.Header.Values(o.Access.GroupsHeader)
	if o.Access.UserHeader != "" {
		r.Header.Del(o.Access.UserHeader)
		r.Header.Del(o.Access.GroupsHeader)
	}
	if token != "" && o.WebToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(o.WebToken)) == 1 {
		v := Viewer{Private: kind == viewerPrivate || kind == viewerAdmin, Admin: kind == viewerAdmin}
		v.Name, _ = url.PathUnescape(name)
		v.Via, v.web = via, true
		return v, nil
	}
	if o.Auth != nil {
		id, ok, err := o.Auth.Identify(w, r)
		if err != nil {
			return Viewer{}, err
		}
		if ok {
			return o.Access.roles(id), nil
		}
	}
	if !proxied || strings.TrimSpace(user) == "" {
		return Viewer{}, nil
	}
	id := Identity{User: strings.TrimSpace(user), Name: strings.TrimSpace(user), Via: "proxy"}
	if strings.Contains(id.User, "@") {
		id.Email = id.User
	}
	for _, v := range groups {
		for g := range strings.SplitSeq(v, ",") {
			if g = strings.TrimSpace(g); g != "" {
				id.Groups = append(id.Groups, g)
			}
		}
	}
	return o.Access.roles(id), nil
}

// roles derives the roles of id from the lists; names are compared exactly (group names are case-sensitive), emails without case.
func (a Access) roles(id Identity) Viewer {
	in := func(list []string) bool {
		return slices.ContainsFunc(id.Groups, func(g string) bool { return slices.Contains(list, g) })
	}
	v := Viewer{Identity: id}
	v.Admin = id.Admin || in(a.AdminGroups) ||
		id.Email != "" && slices.ContainsFunc(a.AdminEmails, func(e string) bool { return strings.EqualFold(e, id.Email) })
	v.Private = v.Admin || slices.Contains(a.PrivateGroups, "*") || in(a.PrivateGroups)
	return v
}

// headerValue is the X-Scp-Viewer value of v.
func (v Viewer) headerValue() string {
	switch {
	case v.Admin:
		return viewerAdmin
	case v.Private:
		return viewerPrivate
	}
	return viewerPublic
}

// SameOrigin reports whether a state-changing request comes from the portal's own pages (design/auth.md): Sec-Fetch-Site is
// same-origin, or absent with an Origin equal to the public origin (the request's own when publicURL is empty). A request without a
// cookie (a bearer client) needs neither.
func SameOrigin(r *http.Request, publicURL string) bool {
	if r.Header.Get("Cookie") == "" {
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	want := publicURL
	if want == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		want = scheme + "://" + r.Host
	}
	u, err := url.Parse(want)
	return err == nil && origin == u.Scheme+"://"+u.Host
}

// AdminOnly answers 401 to an anonymous viewer and 403 to a viewer who is not an admin; admins reach h.
func AdminOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := ViewerOf(r.Context())
		switch {
		case !v.SignedIn():
			WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Sign in to see this.")
		case !v.Admin:
			WriteProblem(w, http.StatusForbidden, "forbidden", "Only an admin sees this.")
		default:
			h.ServeHTTP(w, r)
		}
	})
}
