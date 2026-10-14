package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/store"
)

type touchKey struct{ ns, key string }

// Store is the SQLite implementation of store.Store.
type Store struct {
	cfg    Config
	drv    driver
	path   string
	log    *slog.Logger
	m      *metrics
	tracer trace.Tracer

	rw, ro, bk *sql.DB
	bkMu       sync.Mutex // Backup and Check share the one bk connection

	closed  atomic.Bool
	health  atomic.Pointer[store.Health]
	rebuilt string
	schemaV int
	sqliteV string
	buildID string

	caps       map[string]int64
	putMu      sync.Mutex // serialises PutCache end to end (accounting window)
	cacheMu    sync.Mutex
	cacheBytes map[string]int64
	touchMu    sync.Mutex
	touched    map[touchKey]uint64
	seq        atomic.Uint64

	lastBackup    atomic.Int64 // unix seconds, 0 = never
	lastBackupErr atomic.Pointer[string]
}

func (s *Store) Health() store.Health {
	if h := s.health.Load(); h != nil {
		return *h
	}
	return store.Health{OK: true}
}

// SQLite primary result codes that flip the store to degraded.
const (
	sqliteIOErr    = 10
	sqliteCorrupt  = 11
	sqliteFull     = 13
	sqliteCantOpen = 14
	sqliteReadOnly = 8
	sqliteNotADB   = 26
)

func degrading(code int) bool {
	switch code {
	case sqliteIOErr, sqliteCorrupt, sqliteFull, sqliteCantOpen, sqliteReadOnly, sqliteNotADB:
		return true
	}
	return false
}

func (s *Store) markDegraded(op string, err error) {
	h := store.Health{OK: false, Reason: fmt.Sprintf("%s: %v", op, err)}
	if old := s.health.Swap(&h); old == nil || old.OK {
		s.log.Error("store degraded: writes suspended", "op", op, "err", err, "probe_interval", s.cfg.ProbeInterval.String())
	}
}

func (s *Store) markHealthy() {
	h := store.Health{OK: true}
	if old := s.health.Swap(&h); old != nil && !old.OK {
		s.log.Info("store recovered: writes resumed")
	}
}

// fail counts and classifies a SQLite error. Errors that are the caller's (context, typed store errors) pass through.
func (s *Store) fail(op string, err error) error {
	var ie *store.InvalidError
	var nr *store.NoRepoError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.As(err, &ie), errors.As(err, &nr),
		errors.Is(err, store.ErrNoItem), errors.Is(err, store.ErrTooLarge), errors.Is(err, store.ErrUnknownNamespace):
		return err
	}
	s.m.errors.Add(context.Background(), 1, metric.WithAttributes(attribute.String("op", op)))
	if code, ok := s.drv.code(err); ok && degrading(code) {
		s.markDegraded(op, err)
	}
	return fmt.Errorf("store: %s: %w", op, err)
}

func (s *Store) gate(op string, write bool) error {
	if s.closed.Load() {
		return store.ErrClosed
	}
	if write {
		if h := s.Health(); !h.OK {
			return fmt.Errorf("%w: %s", store.ErrDegraded, h.Reason)
		}
	}
	return nil
}

// write runs fn in one BEGIN IMMEDIATE transaction on the single writer connection. fn must use only tx (never s.rw).
// post, if returned, runs after a successful commit (in-memory counter updates).
func (s *Store) write(ctx context.Context, op string, fn func(tx *sql.Tx) (post func(), err error)) error {
	if err := s.gate(op, true); err != nil {
		return err
	}
	ctx, span := s.tracer.Start(ctx, "store.tx", trace.WithAttributes(attribute.String("op", op)))
	defer span.End()
	t0 := time.Now()
	defer func() {
		s.m.txSeconds.Record(ctx, time.Since(t0).Seconds(), metric.WithAttributes(attribute.String("op", op)))
	}()
	tx, err := s.rw.BeginTx(ctx, nil)
	if err != nil {
		span.SetStatus(codes.Error, "begin")
		return s.fail(op, err)
	}
	post, err := fn(tx)
	if err != nil {
		_ = tx.Rollback()
		span.SetStatus(codes.Error, "fn")
		return s.fail(op, err)
	}
	if err := tx.Commit(); err != nil {
		span.SetStatus(codes.Error, "commit")
		return s.fail(op, err)
	}
	if post != nil {
		post()
	}
	return nil
}

// read runs fn on the read pool inside a read transaction (one consistent snapshot).
func (s *Store) read(ctx context.Context, op string, fn func(tx *sql.Tx) error) error {
	if err := s.gate(op, false); err != nil {
		return err
	}
	ctx, span := s.tracer.Start(ctx, "store.tx", trace.WithAttributes(attribute.String("op", op)))
	defer span.End()
	t0 := time.Now()
	defer func() {
		s.m.txSeconds.Record(ctx, time.Since(t0).Seconds(), metric.WithAttributes(attribute.String("op", op)))
	}()
	tx, err := s.ro.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return s.fail(op, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return s.fail(op, err)
	}
	return nil
}

func unix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func fromUnix(n sql.NullInt64) time.Time {
	if !n.Valid || n.Int64 == 0 {
		return time.Time{}
	}
	return time.Unix(n.Int64, 0).UTC()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

var _ store.Store = (*Store)(nil)
