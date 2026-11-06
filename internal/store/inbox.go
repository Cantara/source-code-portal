package store

import (
	"context"
	"strings"
)

// inboxPrefix marks the meta rows that hold webhook events not yet known to have been acted on.
const inboxPrefix = "inbox/"

// Park keeps a webhook event under id until Unpark removes it, so an event the process never acted on (a crash, a restart) is not lost.
// The write is synced to disk before it returns (the store's own writes are not: a cache may lose its last seconds to a power cut, but
// GitHub has been told the delivery arrived and will not send it again).
func (s *DB) Park(ctx context.Context, id, value string) (err error) {
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
	_, err = conn.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		inboxPrefix+id, value)
	return err
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
