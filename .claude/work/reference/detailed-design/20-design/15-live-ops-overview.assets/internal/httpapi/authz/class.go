package authz

import (
	"context"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Viewer is a principal's three visibility answers, evaluated once per request so list endpoints
// call Can per item in O(1) and never call the Authorizer in a loop.
type Viewer struct{ public, internal, private bool }

// NewViewer evaluates the authorizer for each visibility. VisPublic is always visible: public data has
// no access control, whatever a custom Authorizer says.
func NewViewer(ctx context.Context, az Authorizer, p Principal) Viewer {
	return Viewer{
		public:   true,
		internal: az.CanView(ctx, p, scm.VisInternal),
		private:  az.CanView(ctx, p, scm.VisPrivate),
	}
}

// Can reports whether the principal may see a repository of visibility v. Unknown values are not visible.
func (v Viewer) Can(vis scm.Visibility) bool {
	switch vis {
	case scm.VisPublic:
		return v.public
	case scm.VisInternal:
		return v.internal
	case scm.VisPrivate:
		return v.private
	}
	return false
}

// Class is ViewAll only when every non-public visibility is visible; otherwise ViewPublic (fail closed).
func (v Viewer) Class() scm.ViewClass {
	if v.internal && v.private {
		return scm.ViewAll
	}
	return scm.ViewPublic
}

// ClassOf picks the variant of every global projection a principal may read.
func ClassOf(az Authorizer, p Principal) scm.ViewClass {
	return NewViewer(context.Background(), az, p).Class()
}
