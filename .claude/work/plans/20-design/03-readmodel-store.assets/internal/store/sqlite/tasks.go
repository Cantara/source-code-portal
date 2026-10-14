package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

func (s *Store) SaveTask(ctx context.Context, name string, t store.TaskState) error {
	if name == "" || len(name) > 128 {
		return &store.InvalidError{Field: "task", Reason: "name required, at most 128 bytes"}
	}
	return s.write(ctx, "SaveTask", func(tx *sql.Tx) (func(), error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO task_state(name,last_attempt,last_success,last_error) VALUES(?,?,?,?)
ON CONFLICT(name) DO UPDATE SET last_attempt=excluded.last_attempt, last_success=excluded.last_success, last_error=excluded.last_error`,
			name, unix(t.LastAttempt), unix(t.LastSuccess), nullStr(trunc(t.LastError, 512)))
		return nil, err
	})
}

func (s *Store) LoadTasks(ctx context.Context) (map[string]store.TaskState, error) {
	out := map[string]store.TaskState{}
	err := s.read(ctx, "LoadTasks", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT name, last_attempt, last_success, coalesce(last_error,'') FROM task_state`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			var t store.TaskState
			var a, su sql.NullInt64
			if err := rows.Scan(&n, &a, &su, &t.LastError); err != nil {
				return err
			}
			t.LastAttempt, t.LastSuccess = fromUnix(a), fromUnix(su)
			out[n] = t
		}
		return rows.Err()
	})
	return out, err
}

// Park records a permanently failed key. `since` is the first time the key was parked and survives re-parking.
func (s *Store) Park(ctx context.Context, key, reason string, retryAt time.Time) error {
	if key == "" || len(key) > 600 || retryAt.IsZero() {
		return &store.InvalidError{Field: "park", Reason: "key and retry_at required"}
	}
	return s.write(ctx, "Park", func(tx *sql.Tx) (func(), error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO parked(key,since,reason,retry_at) VALUES(?,?,?,?)
ON CONFLICT(key) DO UPDATE SET reason=excluded.reason, retry_at=excluded.retry_at`, key, time.Now().Unix(), trunc(reason, 512), retryAt.Unix())
		return nil, err
	})
}

func (s *Store) Unpark(ctx context.Context, key string) error {
	return s.write(ctx, "Unpark", func(tx *sql.Tx) (func(), error) {
		_, err := tx.ExecContext(ctx, `DELETE FROM parked WHERE key=?`, key)
		return nil, err
	})
}

func (s *Store) LoadParked(ctx context.Context) ([]store.Parked, error) {
	var out []store.Parked
	err := s.read(ctx, "LoadParked", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT key, since, reason, retry_at FROM parked ORDER BY key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p store.Parked
			var si, re sql.NullInt64
			if err := rows.Scan(&p.Key, &si, &p.Reason, &re); err != nil {
				return err
			}
			p.Since, p.RetryAt = fromUnix(si), fromUnix(re)
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}
