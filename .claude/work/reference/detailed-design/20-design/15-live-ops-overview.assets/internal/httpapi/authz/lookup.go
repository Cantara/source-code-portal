package authz

import (
	"context"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// LookupRepo is the only sanctioned way for a handler to resolve a slug. A repository that does not exist,
// a malformed slug ("." and ".." included) and a repository the principal may not see produce the SAME
// problem (404 not-found, identical bytes except `instance`), so the API is not an existence oracle.
func LookupRepo(ctx context.Context, s *readmodel.Snapshot, repos readmodel.Slot[freshness.Value[scm.Repository]], az Authorizer, slug scm.Slug) (scm.Repository, *httpapi.Problem) {
	miss := func() (scm.Repository, *httpapi.Problem) {
		p := httpapi.NotFound("repository not found")
		return scm.Repository{}, &p
	}
	if _, err := scm.ParseSlug(string(slug)); err != nil { // the kernel grammar: canonical lower-case, no dot segments, no reserved word
		return miss()
	}
	v, ok := readmodel.Get(s, readmodel.EntryID(slug), repos)
	if !ok {
		return miss()
	}
	if !NewViewer(ctx, az, PrincipalFrom(ctx)).Can(v.V.Visibility) {
		return miss()
	}
	return v.V, nil
}
