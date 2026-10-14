// Package live is a compile-check sketch of one feature mounting its generated strict server (design 07, sections 2.3.4
// and 7.6). It imports exactly what the architecture matrix gives rank 4 `live`: httpapi and its own liveapi. It does NOT
// import httpapi/openapi: every contract type it needs is an alias in liveapi/common_aliases.go (apicheck aliases).
package live

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/live/liveapi"
)

// SnapshotSource is the consumer-defined interface (architecture 8.7): live asks only for what it uses. The numeric types are
// those of the kernel (readmodel.Snapshot.Version is a uint64). Epoch is a random token that changes at every process start:
// snapshot versions restart at 1 on a warm start (unit 03), so a version alone is not a stable validator across restarts.
type SnapshotSource interface {
	Version() uint64
	BuiltAt() time.Time
	Ready() bool
	Epoch() string
}

type Service struct {
	Snap    SnapshotSource
	BuildID string
	Log     *slog.Logger
}

var _ httpapi.Mounter = (*Service)(nil)
var _ liveapi.StrictServerInterface = (*Service)(nil)

func (s *Service) Mount(m httpapi.Mux) {
	req, resp := httpapi.StrictErrors(s.Log)
	h := liveapi.NewStrictHandlerWithOptions(s, nil, liveapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: req, ResponseErrorHandlerFunc: resp})
	liveapi.HandlerWithOptions(h, liveapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}

func (s *Service) GetSnapshot(ctx context.Context, _ liveapi.GetSnapshotRequestObject) (liveapi.GetSnapshotResponseObject, error) {
	v := s.Snap.Version()
	// The tag is computed before the body and names the process (epoch) as well as the version: W/"v<epoch>-<version>".
	etag := httpapi.WeakETagOf(httpapi.ETagVersion, s.Snap.Epoch(), v)
	if httpapi.NotModified(httpapi.RequestFrom(ctx), etag) {
		return liveapi.GetSnapshot304Response{Headers: liveapi.NotModifiedResponseHeaders{ETag: &etag}}, nil
	}
	ready := liveapi.SnapshotReadinessWarming
	if s.Snap.Ready() {
		ready = liveapi.SnapshotReadinessReady
	}
	return liveapi.GetSnapshot200JSONResponse{
		Body:    liveapi.Snapshot{Version: int64(v), BuiltAt: s.Snap.BuiltAt().UTC(), Readiness: ready, BuildId: s.BuildID},
		Headers: liveapi.GetSnapshot200ResponseHeaders{ETag: &etag},
	}, nil
}

// sse is the hand-written response type: a strict handler accepts any value that has the Visit method of the operation, so a
// stream does not need the generated io.Reader copy loop (which cannot select on shutdown).
type sse struct {
	run func(w http.ResponseWriter) error
}

func (e sse) VisitStreamEventsResponse(w http.ResponseWriter) error { return e.run(w) }

func (s *Service) StreamEvents(_ context.Context, req liveapi.StreamEventsRequestObject) (liveapi.StreamEventsResponseObject, error) {
	last := httpapi.Deref(req.Params.LastEventID)
	return sse{run: func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		v := s.Snap.Version()
		_, err := io.WriteString(w, fmt.Sprintf("retry: 3000\n\nid: %d\nevent: hello\ndata: {\"generation\":%d,\"buildId\":%q}\n\n", v, v, s.BuildID))
		// A client whose Last-Event-ID is ahead of this process (a restart: versions restart at 1) or older than the change ring
		// must invalidate everything: the server answers resync right after hello (design 7.6).
		// A value too large for uint64 (the pattern allows 20 digits) is ahead of any real version: ParseUint reports ErrRange.
		if n, perr := strconv.ParseUint(last, 10, 64); (perr == nil && n > v || errors.Is(perr, strconv.ErrRange)) && err == nil {
			_, err = io.WriteString(w, fmt.Sprintf("id: %d\nevent: resync\ndata: {\"generation\":%d}\n\n", v, v))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return err
	}}, nil
}
