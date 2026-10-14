// Package catalog is a STUB of the consumer interface the Tracker bridge satisfies (architecture 8.7).
package catalog

import (
	"context"

	"github.com/cantara/source-code-portal/internal/scm"
)

type Tracker interface{ SetRepos(repos []scm.RepoRef) }

// EventSink is the wired *reactor.Fan (08 interfaces.go).
type EventSink interface {
	Handle(ctx context.Context, ev scm.Event) error
}
