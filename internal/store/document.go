package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrRevision is a save or removal based on a revision that is no longer the stored one.
var ErrRevision = errors.New("store: the document changed since it was read")

// Document returns the document called name, its revision, and whether there is one.
func (s *DB) Document(ctx context.Context, name string) (body []byte, revision string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT body, revision FROM document WHERE name = ?", name).Scan(&body, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	return body, revision, err == nil, err
}

// PutDocument stores body as the document called name, with revision, when was is the stored revision (empty: there is none yet);
// otherwise it returns ErrRevision. The write is synced to disk before it returns.
func (s *DB) PutDocument(ctx context.Context, name string, body []byte, revision, was string) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		s.docMu.Lock()
		defer s.docMu.Unlock()
		if err := s.revisionIs(ctx, conn, name, was); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `INSERT INTO document (name, body, revision, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (name) DO UPDATE SET body = excluded.body, revision = excluded.revision, updated_at = excluded.updated_at`,
			name, body, revision, time.Now().Unix())
		return err
	})
}

// DeleteDocument removes the document called name when was is its stored revision.
func (s *DB) DeleteDocument(ctx context.Context, name, was string) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		s.docMu.Lock()
		defer s.docMu.Unlock()
		if err := s.revisionIs(ctx, conn, name, was); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, "DELETE FROM document WHERE name = ?", name)
		return err
	})
}

// revisionIs is nil when was is the revision stored under name (empty: nothing is). Callers hold docMu: one save at a time.
func (s *DB) revisionIs(ctx context.Context, conn *sql.Conn, name, was string) error {
	var cur string
	err := conn.QueryRowContext(ctx, "SELECT revision FROM document WHERE name = ?", name).Scan(&cur)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if cur != was {
		return ErrRevision
	}
	return nil
}
