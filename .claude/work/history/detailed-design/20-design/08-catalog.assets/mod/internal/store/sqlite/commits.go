package sqlite

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/store"
)

func (s *Store) LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []store.CommitRow
	err := s.read(ctx, "LoadCommits", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT sha, committed_at, payload FROM commit_log WHERE repo_id=`+repoID+
			` ORDER BY committed_at DESC, id DESC LIMIT ?4`, repo.Source, repo.Owner, repo.Name, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c store.CommitRow
			var at sql.NullInt64
			if err := rows.Scan(&c.SHA, &at, &c.Payload); err != nil {
				return err
			}
			c.CommittedAt = fromUnix(at)
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// AppendCommits inserts new commits (existing SHAs are ignored: commits are immutable) and trims the repository to the
// newest `keep` rows in the same transaction.
func (s *Store) AppendCommits(ctx context.Context, repo store.RepoKey, cs []store.CommitRow, keep int) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if keep <= 0 {
		return &store.InvalidError{Field: "keep", Reason: "must be positive"}
	}
	for _, c := range cs {
		if err := store.ValidateCommit(c); err != nil {
			return err
		}
	}
	return s.write(ctx, "AppendCommits", func(tx *sql.Tx) (func(), error) {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM repo WHERE source=?1 AND owner=?2 AND name=?3`, repo.Source, repo.Owner, repo.Name).Scan(&id); err != nil {
			if err == sql.ErrNoRows {
				return nil, &store.NoRepoError{Key: repo}
			}
			return nil, err
		}
		ins, err := tx.PrepareContext(ctx, `INSERT INTO commit_log(repo_id, sha, committed_at, payload) VALUES(?1,?2,?3,?4) ON CONFLICT(repo_id, sha) DO NOTHING`)
		if err != nil {
			return nil, err
		}
		defer ins.Close()
		for _, c := range cs {
			if _, err := ins.ExecContext(ctx, id, c.SHA, c.CommittedAt.Unix(), c.Payload); err != nil {
				return nil, err
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM commit_log WHERE repo_id=?1 AND (committed_at, id) <
  (SELECT committed_at, id FROM commit_log WHERE repo_id=?1 ORDER BY committed_at DESC, id DESC LIMIT 1 OFFSET ?2-1)`, id, keep)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		return func() {
			if n > 0 {
				s.m.trimmed.Add(ctx, n, metric.WithAttributes())
			}
		}, nil
	})
}
