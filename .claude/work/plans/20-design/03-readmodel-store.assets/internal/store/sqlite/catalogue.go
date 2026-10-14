package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/store"
)

func (s *Store) LoadCatalogue(ctx context.Context) ([]store.Repo, error) {
	var out []store.Repo
	err := s.read(ctx, "LoadCatalogue", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT source, owner, name, github_id, slug, seen_at, meta FROM repo ORDER BY slug`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r store.Repo
			var gid sql.NullInt64
			var seen sql.NullInt64
			var meta string
			if err := rows.Scan(&r.Key.Source, &r.Key.Owner, &r.Key.Name, &gid, &r.Slug, &seen, &meta); err != nil {
				return err
			}
			r.GitHubID, r.SeenAt, r.Meta = gid.Int64, fromUnix(seen), []byte(meta)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

type repoRowDB struct {
	id        int64
	key       store.RepoKey
	gid       int64
	slug      string
	claimedBy int // index into incoming + 1; 0 = unclaimed
}

// ReplaceCatalogue makes the stored catalogue equal to repos in one transaction (algorithm in design section 4.5).
// Rows are matched by GitHub id first (rename/transfer keeps items and commits), then by key; unmatched stored rows are
// deleted (cascading items and commits, and dropping parked job keys of that repository); the rest are inserted.
func (s *Store) ReplaceCatalogue(ctx context.Context, repos []store.Repo) ([]store.RepoKey, error) {
	if err := store.ValidateCatalogue(repos); err != nil {
		return nil, err
	}
	var removed []store.RepoKey
	err := s.write(ctx, "ReplaceCatalogue", func(tx *sql.Tx) (func(), error) {
		removed = removed[:0]
		rows, err := tx.QueryContext(ctx, `SELECT id, source, owner, name, coalesce(github_id,0), slug FROM repo`)
		if err != nil {
			return nil, err
		}
		var old []*repoRowDB
		for rows.Next() {
			r := &repoRowDB{}
			if err := rows.Scan(&r.id, &r.key.Source, &r.key.Owner, &r.key.Name, &r.gid, &r.slug); err != nil {
				rows.Close()
				return nil, err
			}
			old = append(old, r)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		byKey := map[store.RepoKey]*repoRowDB{}
		byGID := map[[2]string]*repoRowDB{}
		for _, r := range old {
			byKey[r.key] = r
			if r.gid != 0 {
				byGID[[2]string{r.key.Source, strconv.FormatInt(r.gid, 10)}] = r
			}
		}
		match := make([]*repoRowDB, len(repos))
		for i, in := range repos { // pass 1: by GitHub id
			if in.GitHubID != 0 {
				if r := byGID[[2]string{in.Key.Source, strconv.FormatInt(in.GitHubID, 10)}]; r != nil && r.claimedBy == 0 {
					r.claimedBy, match[i] = i+1, r
				}
			}
		}
		for i, in := range repos { // pass 2: by key, only an unclaimed row that is not another repository (different non-zero id)
			if match[i] != nil {
				continue
			}
			if r := byKey[in.Key]; r != nil && r.claimedBy == 0 && (r.gid == 0 || in.GitHubID == 0 || r.gid == in.GitHubID) {
				r.claimedBy, match[i] = i+1, r
			}
		}
		for _, r := range old { // delete the unclaimed
			if r.claimedBy != 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM repo WHERE id=?`, r.id); err != nil {
				return nil, err
			}
			tail := r.key.String()
			if _, err := tx.ExecContext(ctx, `DELETE FROM parked WHERE substr(key, -length(?1)) = ?1 AND substr(key, -length(?1)-1, 1) = '/'`, tail); err != nil {
				return nil, err
			}
			removed = append(removed, r.key)
		}
		for i, in := range repos { // phase A: free unique values of renamed rows
			if r := match[i]; r != nil && (r.key != in.Key || r.slug != in.Slug) {
				tmp := "~tmp~" + strconv.FormatInt(r.id, 10)
				if _, err := tx.ExecContext(ctx, `UPDATE repo SET owner=?2, name=?2, slug=?2 WHERE id=?1`, r.id, tmp); err != nil {
					return nil, err
				}
			}
		}
		for i, in := range repos { // phase B: final values; inserts
			var gid any
			if in.GitHubID != 0 {
				gid = in.GitHubID
			}
			if r := match[i]; r != nil {
				if _, err := tx.ExecContext(ctx, `UPDATE repo SET source=?2, owner=?3, name=?4, github_id=coalesce(?5, github_id), slug=?6, seen_at=?7, meta=?8 WHERE id=?1`,
					r.id, in.Key.Source, in.Key.Owner, in.Key.Name, gid, in.Slug, in.SeenAt.Unix(), string(in.Meta)); err != nil {
					return nil, err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO repo(source,owner,name,github_id,slug,seen_at,meta) VALUES(?,?,?,?,?,?,?)`,
				in.Key.Source, in.Key.Owner, in.Key.Name, gid, in.Slug, in.SeenAt.Unix(), string(in.Meta)); err != nil {
				return nil, fmt.Errorf("insert %s: %w", in.Key, err)
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(removed, func(i, j int) bool { return strings.Compare(removed[i].String(), removed[j].String()) < 0 })
	return append([]store.RepoKey(nil), removed...), nil
}
