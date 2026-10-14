// Package authz is the only place that turns a principal and a repository's visibility into a decision.
package authz

import (
	"context"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Principal is who is asking. The zero value is anonymous.
type Principal struct {
	Subject   string
	Groups    []string
	Anonymous bool
}

// IsAnonymous is the one test for "no identity": Anonymous set, or no Subject.
func (p Principal) IsAnonymous() bool { return p.Anonymous || p.Subject == "" }

// NewPrincipal builds an authenticated principal; an empty subject yields the anonymous principal.
func NewPrincipal(subject string, groups []string) Principal {
	if subject == "" {
		return Principal{Anonymous: true}
	}
	return Principal{Subject: subject, Groups: append([]string(nil), groups...)}
}

type ctxKey struct{}

// WithPrincipal is called by the `access` middleware (and by tests).
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the principal in ctx, or the anonymous principal.
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(ctxKey{}).(Principal); ok && !p.IsAnonymous() {
		return p
	}
	return Principal{Anonymous: true}
}

// Authorizer decides whether a principal may see a repository of a given visibility.
// Implementations MUST NOT depend on ctx for the decision (ClassOf calls it with context.Background()).
type Authorizer interface {
	CanView(ctx context.Context, p Principal, v scm.Visibility) bool
}

// PublicOnly is the authorizer of `access.mode: public`: everybody sees public repositories only.
type PublicOnly struct{}

func (PublicOnly) CanView(_ context.Context, _ Principal, v scm.Visibility) bool {
	return v == scm.VisPublic
}
