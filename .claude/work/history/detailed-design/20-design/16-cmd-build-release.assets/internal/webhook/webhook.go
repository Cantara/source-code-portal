// Package webhook is a STUB with the exact consumer interfaces of 04-refresh-webhook.md (webhook.go, redelivery.go); the union
// module compiles cmd/scp against the real package.
package webhook

import (
	"context"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

type Verifier interface {
	Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error)
}

// Sink is the wired *reactor.Fan.
type Sink interface {
	Handle(ctx context.Context, ev scm.Event) error
}

// Limiter is the keyed invalid-signature limiter; *httpx.Limiter satisfies it directly (no bridge).
type Limiter interface{ Allow(key string) bool }

type Redeliverer interface {
	FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error
}

type Watermarks interface {
	Load(ctx context.Context, name string) (time.Time, error)
	Save(ctx context.Context, name string, t time.Time) error
}
