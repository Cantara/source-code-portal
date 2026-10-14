package sqlite

import (
	"context"
	"database/sql"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/store"
)

func blobOrNil(b []byte) any {
	if b == nil {
		return nil
	}
	return b
}

func (s *Store) LoadItems(ctx context.Context, fn func(store.Item) error) error {
	return s.read(ctx, "LoadItems", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT r.source, r.owner, r.name, i.kind, i.status, i.fetched_at, i.checked_at, i.attempts,
       coalesce(i.etag,''), coalesce(i.content_sha,''), coalesce(i.error,''), i.payload
FROM item i JOIN repo r ON r.id = i.repo_id ORDER BY r.id, i.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it store.Item
			var st string
			var fa, ca sql.NullInt64
			if err := rows.Scan(&it.Key.Repo.Source, &it.Key.Repo.Owner, &it.Key.Repo.Name, &it.Key.Kind, &st, &fa, &ca, &it.Attempts,
				&it.ETag, &it.ContentSHA, &it.Error, &it.Payload); err != nil {
				return err
			}
			it.State, it.FetchedAt, it.CheckedAt = freshness.State(st), fromUnix(fa), fromUnix(ca)
			if err := fn(it); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

func (s *Store) LoadPlans(ctx context.Context, fn func(store.PlanRow) error) error {
	return s.read(ctx, "LoadPlans", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT r.source, r.owner, r.name, i.kind, i.next_due_at, i.unchanged_streak
FROM item i JOIN repo r ON r.id = i.repo_id WHERE i.next_due_at IS NOT NULL ORDER BY r.id, i.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p store.PlanRow
			var nd sql.NullInt64
			if err := rows.Scan(&p.Key.Repo.Source, &p.Key.Repo.Owner, &p.Key.Repo.Name, &p.Key.Kind, &nd, &p.UnchangedStreak); err != nil {
				return err
			}
			p.NextDue = fromUnix(nd)
			if err := fn(p); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

const repoID = `(SELECT id FROM repo WHERE source=?1 AND owner=?2 AND name=?3)`

func (s *Store) PutItem(ctx context.Context, it store.Item) error {
	if err := store.ValidateItem(it); err != nil {
		return err
	}
	k := it.Key.Repo
	return s.write(ctx, "PutItem", func(tx *sql.Tx) (func(), error) {
		res, err := tx.ExecContext(ctx, `
INSERT INTO item(repo_id, kind, status, fetched_at, checked_at, attempts, etag, content_sha, error, payload)
SELECT id, ?4, ?5, ?6, ?7, 0, ?8, ?9, NULL, ?10 FROM repo WHERE source=?1 AND owner=?2 AND name=?3
ON CONFLICT(repo_id, kind) DO UPDATE SET status=excluded.status, fetched_at=excluded.fetched_at, checked_at=excluded.checked_at,
  attempts=0, etag=excluded.etag, content_sha=excluded.content_sha, error=NULL, payload=excluded.payload`,
			k.Source, k.Owner, k.Name, it.Key.Kind, string(it.State), it.FetchedAt.Unix(), it.CheckedAt.Unix(),
			nullStr(it.ETag), nullStr(it.ContentSHA), blobOrNil(it.Payload))
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, &store.NoRepoError{Key: k}
		}
		return nil, nil
	})
}

func (s *Store) Revalidated(ctx context.Context, key store.ItemKey, now time.Time) error {
	if err := key.Validate(); err != nil {
		return err
	}
	k := key.Repo
	return s.write(ctx, "Revalidated", func(tx *sql.Tx) (func(), error) {
		res, err := tx.ExecContext(ctx, `UPDATE item SET fetched_at=?5, checked_at=?5, attempts=0, error=NULL
WHERE repo_id=`+repoID+` AND kind=?4 AND status NOT IN ('error','never')`, k.Source, k.Owner, k.Name, key.Kind, now.Unix())
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var one int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM repo WHERE source=?1 AND owner=?2 AND name=?3`, k.Source, k.Owner, k.Name).Scan(&one); err == sql.ErrNoRows {
				return nil, &store.NoRepoError{Key: k}
			}
			return nil, store.ErrNoItem
		}
		return nil, nil
	})
}

// RecordFailure keeps payload, fetched_at and (while a good value exists) the result state: stale-while-error. A row that
// never had a good value is created or marked status 'error'.
func (s *Store) RecordFailure(ctx context.Context, key store.ItemKey, f store.Failure) error {
	if err := store.ValidateFailure(key, f); err != nil {
		return err
	}
	k := key.Repo
	return s.write(ctx, "RecordFailure", func(tx *sql.Tx) (func(), error) {
		res, err := tx.ExecContext(ctx, `
INSERT INTO item(repo_id, kind, status, fetched_at, checked_at, attempts, error)
SELECT id, ?4, 'error', NULL, ?5, 1, ?6 FROM repo WHERE source=?1 AND owner=?2 AND name=?3
ON CONFLICT(repo_id, kind) DO UPDATE SET
  status = CASE WHEN item.fetched_at IS NULL THEN 'error' ELSE item.status END,
  checked_at = excluded.checked_at, attempts = item.attempts + 1, error = excluded.error`,
			k.Source, k.Owner, k.Name, key.Kind, f.At.Unix(), f.Code)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, &store.NoRepoError{Key: k}
		}
		return nil, nil
	})
}

// SavePlans persists planner state for existing items in one transaction. Rows for global kinds (zero RepoKey) and for items
// that have no row yet are skipped by design: global kinds are due at every start, and an item without a row is due anyway.
func (s *Store) SavePlans(ctx context.Context, rows []store.PlanRow) error {
	for _, p := range rows {
		if err := store.ValidatePlan(p); err != nil {
			return err
		}
	}
	var global, noItem int64
	return s.write(ctx, "SavePlans", func(tx *sql.Tx) (func(), error) {
		global, noItem = 0, 0
		st, err := tx.PrepareContext(ctx, `UPDATE item SET next_due_at=?5, unchanged_streak=?6 WHERE kind=?4 AND repo_id=`+repoID)
		if err != nil {
			return nil, err
		}
		defer st.Close()
		for _, p := range rows {
			if p.Key.Repo.IsZero() {
				global++
				continue
			}
			k := p.Key.Repo
			res, err := st.ExecContext(ctx, k.Source, k.Owner, k.Name, p.Key.Kind, unix(p.NextDue), p.UnchangedStreak)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				noItem++
			}
		}
		return func() { // a plan row that matches no item row is dropped: count it so a kind mismatch is visible (V-02)
			if global > 0 {
				s.m.planSkipped.Add(ctx, global, metric.WithAttributes(attribute.String("reason", "global")))
			}
			if noItem > 0 {
				s.m.planSkipped.Add(ctx, noItem, metric.WithAttributes(attribute.String("reason", "no_item")))
			}
		}, nil
	})
}
