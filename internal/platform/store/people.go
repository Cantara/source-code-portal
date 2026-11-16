package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Person is one signed-in identity of the enterprise mode (design/enterprise.md).
type Person struct {
	ID       string // the OIDC issuer and subject
	Identity string // the identity claim's value at the last sign-in
	Name     string
	FirstAt  time.Time
	LastAt   time.Time
}

// PersonToken is a GitHub token a person linked for one owner of one source. Sealed is opaque here: the people package seals and
// opens it.
type PersonToken struct {
	Person    string
	Source    string
	Owner     string // lower case
	Login     string
	Sealed    []byte
	ExpiresAt time.Time // zero: GitHub named no end
	CreatedAt time.Time
	State     string    // ok, or why GitHub refused it last
	ListedAt  time.Time // zero: its repositories were never listed
}

// Access is what one person's token for one owner lists: the repositories' GitHub ids.
type Access struct {
	Person, Source, Owner string
	Repos                 []int64
}

// ErrNoPersonToken is returned for a token the store does not hold.
var ErrNoPersonToken = errors.New("store: no such token")

// PutPerson stores p, replacing the person with the same id but keeping when they were first seen; synced to disk before it returns.
func (s *DB) PutPerson(ctx context.Context, p Person) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO person (id, identity, name, first_at, last_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET identity = excluded.identity, name = excluded.name, last_at = excluded.last_at`,
			p.ID, p.Identity, p.Name, p.FirstAt.Unix(), p.LastAt.Unix())
		return err
	})
}

// PutPersonToken stores t, replacing the person's token for the same source and owner, and drops what the token it replaces listed:
// a new token's access is its own. Synced to disk before it returns.
func (s *DB) PutPersonToken(ctx context.Context, t PersonToken) error {
	return s.durablyTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM person_access WHERE person = ? AND source = ? AND owner = ?", t.Person, t.Source, t.Owner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO person_token (person, source, owner, login, sealed, expires_at, created_at, state, listed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (person, source, owner) DO UPDATE SET login = excluded.login, sealed = excluded.sealed, expires_at = excluded.expires_at,
			created_at = excluded.created_at, state = excluded.state, listed_at = excluded.listed_at`,
			t.Person, t.Source, t.Owner, t.Login, t.Sealed, toUnix(t.ExpiresAt), t.CreatedAt.Unix(), t.State, toUnix(t.ListedAt)); err != nil {
			return err
		}
		return nil
	})
}

// SetTokenState records why GitHub refused a person's token (state), or ok; a refused token's access is dropped with it, so the
// person reads nothing of it until it is listed again. Synced to disk before it returns.
func (s *DB) SetTokenState(ctx context.Context, person, source, owner, state string) error {
	return s.durablyTx(ctx, func(tx *sql.Tx) error {
		listed := "listed_at"
		if state != "ok" {
			listed = "NULL"
			if _, err := tx.ExecContext(ctx, "DELETE FROM person_access WHERE person = ? AND source = ? AND owner = ?", person, source, owner); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE person_token SET state = ?, listed_at = "+listed+" WHERE person = ? AND source = ? AND owner = ?",
			state, person, source, owner); err != nil {
			return err
		}
		return nil
	})
}

// SetAccess replaces what a person's token lists with a.Repos, read at listedAt, in one transaction; synced to disk before it returns.
// A token the store no longer holds keeps nothing.
func (s *DB) SetAccess(ctx context.Context, a Access, listedAt time.Time) error {
	return s.durablyTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE person_token SET state = 'ok', listed_at = ? WHERE person = ? AND source = ? AND owner = ?",
			listedAt.Unix(), a.Person, a.Source, a.Owner)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return errors.Join(err, ErrNoPersonToken)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM person_access WHERE person = ? AND source = ? AND owner = ?", a.Person, a.Source, a.Owner); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO person_access (person, source, owner, repo_id) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING")
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, id := range a.Repos {
			if _, err := stmt.ExecContext(ctx, a.Person, a.Source, a.Owner, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeletePersonToken removes a person's token for source and owner and what it listed; one that is not there is not an error. Synced
// to disk before it returns: a removed token does not come back after a power loss.
func (s *DB) DeletePersonToken(ctx context.Context, person, source, owner string) error {
	return s.durablyTx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{"DELETE FROM person_access WHERE person = ? AND source = ? AND owner = ?",
			"DELETE FROM person_token WHERE person = ? AND source = ? AND owner = ?"} {
			if _, err := tx.ExecContext(ctx, q, person, source, owner); err != nil {
				return err
			}
		}
		return nil
	})
}

// People loads every person, token and access list, for the memory that serves them.
func (s *DB) People(ctx context.Context) ([]Person, []PersonToken, []Access, error) {
	var people []Person
	rows, err := s.db.QueryContext(ctx, "SELECT id, identity, name, first_at, last_at FROM person ORDER BY id")
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var (
			p           Person
			first, last int64
		)
		if err := rows.Scan(&p.ID, &p.Identity, &p.Name, &first, &last); err != nil {
			_ = rows.Close()
			return nil, nil, nil, err
		}
		p.FirstAt, p.LastAt = fromUnix(first), fromUnix(last)
		people = append(people, p)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, nil, err
	}

	var tokens []PersonToken
	rows, err = s.db.QueryContext(ctx, `SELECT person, source, owner, login, sealed, expires_at, created_at, state, listed_at FROM person_token
		ORDER BY person, source, owner`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var (
			t               PersonToken
			expires, listed sql.NullInt64
			created         int64
		)
		if err := rows.Scan(&t.Person, &t.Source, &t.Owner, &t.Login, &t.Sealed, &expires, &created, &t.State, &listed); err != nil {
			_ = rows.Close()
			return nil, nil, nil, err
		}
		if expires.Valid {
			t.ExpiresAt = fromUnix(expires.Int64)
		}
		if listed.Valid {
			t.ListedAt = fromUnix(listed.Int64)
		}
		t.CreatedAt = fromUnix(created)
		tokens = append(tokens, t)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, nil, err
	}

	var access []Access
	rows, err = s.db.QueryContext(ctx, "SELECT person, source, owner, repo_id FROM person_access ORDER BY person, source, owner, repo_id")
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			person, source, owner string
			id                    int64
		)
		if err := rows.Scan(&person, &source, &owner, &id); err != nil {
			return nil, nil, nil, err
		}
		if n := len(access); n == 0 || access[n-1].Person != person || access[n-1].Source != source || access[n-1].Owner != owner {
			access = append(access, Access{Person: person, Source: source, Owner: owner})
		}
		access[len(access)-1].Repos = append(access[len(access)-1].Repos, id)
	}
	return people, tokens, access, rows.Err()
}

// durablyTx runs fn in one transaction on a connection whose commit is synced to disk before it returns (durably).
func (s *DB) durablyTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err := fn(tx); err != nil {
			return err
		}
		return tx.Commit()
	})
}
