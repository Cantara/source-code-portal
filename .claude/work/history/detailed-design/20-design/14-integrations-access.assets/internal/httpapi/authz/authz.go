// Package authz: STUB of architecture 8.14 plus the WithPrincipal counterpart proposed in unit 14 (deviation D-2).
package authz

import (
	"context"

	"github.com/cantara/source-code-portal/internal/scm"
)

type Principal struct {
	Subject   string
	Groups    []string
	Anonymous bool
}
type Authorizer interface {
	CanView(ctx context.Context, p Principal, v scm.Visibility) bool
}
type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(ctxKey{}).(Principal); ok {
		return p
	}
	return Principal{Anonymous: true}
}
func ClassOf(az Authorizer, p Principal) scm.ViewClass {
	if az.CanView(context.Background(), p, scm.VisPrivate) {
		return scm.ViewAll
	}
	return scm.ViewPublic
}
