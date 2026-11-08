package store

import (
	"context"
	"database/sql"
	"strings"
)

// inboxPrefix marks the meta rows that hold webhook events not yet known to have been acted on.
const inboxPrefix = "inbox/"

// Park keeps a webhook event under id until Unpark removes it, so an event the process never acted on (a crash, a restart) is not lost.
// GitHub has been told the delivery arrived and will not send it again, so the write is synced to disk before it returns.
func (s *DB) Park(ctx context.Context, id, value string) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			inboxPrefix+id, value)
		return err
	})
}

// durably runs fn on a connection whose commits are synced to disk before they return. The store's own writes are not (a cache may
// lose its last seconds to a power cut), but what has been acknowledged to a person or to GitHub must survive the process dying and
// the machine losing power: parked webhook events, sealed credentials, the documents administrators save.
func (s *DB) durably(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PRAGMA synchronous = FULL"); err != nil {
		return err
	}
	defer func() { // the connection goes back to the pool
		if _, e := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA synchronous = NORMAL"); e != nil && err == nil {
			err = e
		}
	}()
	return fn(conn)
}

// Parked returns the parked events, by id.
func (s *DB) Parked(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM meta WHERE substr(key, 1, ?) = ?", len(inboxPrefix), inboxPrefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(k, inboxPrefix)] = v
	}
	return out, rows.Err()
}

// Unpark removes the parked events with these ids.
func (s *DB) Unpark(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "DELETE FROM meta WHERE key = ?", inboxPrefix+id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
