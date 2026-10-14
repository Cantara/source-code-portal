package httpapi

import (
	"context"
	"net/http"
)

type requestKey struct{}

// WithRequest stores the *http.Request in its own context so that strict handlers, which receive only
// (ctx, requestObject) from oapi-codegen, can reach it for NotModified, NextLink and the like. It is mounted
// innermost (directly around the API mux) by cmd/scp. The stored request is read-only for the handler: headers,
// URL and method are the only things to look at.
func WithRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestKey{}, r)))
	})
}

// RequestFrom returns the request stored by WithRequest, or nil when the middleware is not mounted (tests of
// a handler that do not need it may pass a context without one; a handler that does need it fails closed).
func RequestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}

// Deref returns *p, or the zero value for nil: generated parameters are pointers for optional values.
func Deref[T any](p *T) (z T) {
	if p != nil {
		return *p
	}
	return z
}

// Ptr returns &v (for optional response header members such as ListCommits200ResponseHeaders.Link).
func Ptr[T any](v T) *T { return &v }
