package access

import (
	"context"
	"net/netip"
	"strings"

	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/scm"
)

type netipPrefix = netip.Prefix

// authorizer implements authz.Authorizer. Invariant (property-tested): CanView is monotone in visibility, i.e.
// VisInternal and VisPrivate always get the same answer, because scm.ViewClass has two values and every global
// projection is computed per class; a finer split would reopen the count leak of ADR-023.
type authorizer struct {
	trusted  bool
	groups   map[string]bool
	subjects map[string]bool // lower-cased
	open     bool            // no restriction lists: every authenticated principal
}

func newAuthorizer(c Config) *authorizer {
	a := &authorizer{trusted: c.Mode == ModeTrustedProxy, groups: map[string]bool{}, subjects: map[string]bool{}}
	for _, g := range c.ViewAll.Groups {
		a.groups[strings.TrimSpace(g)] = true
	}
	for _, s := range c.ViewAll.Subjects {
		a.subjects[asciiLower(strings.TrimSpace(s))] = true
	}
	a.open = len(a.groups) == 0 && len(a.subjects) == 0
	return a
}

func (a *authorizer) CanView(_ context.Context, p authz.Principal, v scm.Visibility) bool {
	if v == scm.VisPublic {
		return true
	}
	// Every other value, including ones added later, is non-public: fail closed.
	if !a.trusted || p.Anonymous || p.Subject == "" {
		return false
	}
	if a.open {
		return true
	}
	if a.subjects[asciiLower(p.Subject)] {
		return true
	}
	for _, g := range p.Groups {
		if a.groups[g] {
			return true
		}
	}
	return false
}

type operator struct{}

func (operator) CanView(context.Context, authz.Principal, scm.Visibility) bool { return true }

// asciiLower folds A-Z only. strings.ToLower and strings.EqualFold apply Unicode simple case folding, which maps
// U+212A KELVIN SIGN to "k" and U+0130 to "i" plus a combining dot: a subject registered at the identity provider with
// such a character would match the allow-list entry of a different user. E-mail local parts are compared byte-exact
// apart from ASCII case.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
