package authz

import (
	"context"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
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

func PrincipalFrom(ctx context.Context) Principal { return Principal{Anonymous: true} }
func ClassOf(az Authorizer, p Principal) scm.ViewClass {
	if az != nil && az.CanView(context.Background(), p, scm.VisPrivate) {
		return scm.ViewAll
	}
	return scm.ViewPublic
}
func LookupRepo(ctx context.Context, s *readmodel.Snapshot, repos readmodel.Slot[freshness.Value[scm.Repository]], az Authorizer, slug scm.Slug) (scm.Repository, *httpapi.Problem) {
	v, ok := readmodel.Get(s, readmodel.EntryID(slug), repos)
	if !ok || (az != nil && !az.CanView(ctx, PrincipalFrom(ctx), v.V.Visibility)) {
		return scm.Repository{}, &httpapi.Problem{Status: 404, Code: "not-found", Title: "Repository not found"}
	}
	return v.V, nil
}

// PublicOnly is the authorizer of `access.mode: public` (the real package ships it; unit 14).
type PublicOnly struct{}

func (PublicOnly) CanView(_ context.Context, _ Principal, v scm.Visibility) bool {
	return v == scm.VisPublic
}
