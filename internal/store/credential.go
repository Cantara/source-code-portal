package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNoCredential is returned for a credential id the store does not hold.
var ErrNoCredential = errors.New("store: no such credential")

// Credential is a sealed credential row. Sealed is opaque here: the vault seals and opens it.
type Credential struct {
	ID        string
	Source    string
	Kind      string
	Sealed    []byte
	ExpiresAt time.Time // zero: does not expire
	CreatedAt time.Time
}

// PutCredential stores c, replacing the credential with the same id; the write is synced to disk before it returns.
func (s *DB) PutCredential(ctx context.Context, c Credential) error {
	return s.durably(ctx, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO credential (id, source, kind, sealed, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET source = excluded.source, kind = excluded.kind, sealed = excluded.sealed,
			expires_at = excluded.expires_at, created_at = excluded.created_at`,
			c.ID, c.Source, c.Kind, c.Sealed, toUnix(c.ExpiresAt), c.CreatedAt.Unix())
		return err
	})
}

// Credential returns the credential with id, or ErrNoCredential.
func (s *DB) Credential(ctx context.Context, id string) (Credential, error) {
	var (
		c       Credential
		expires sql.NullInt64
		created int64
	)
	err := s.db.QueryRowContext(ctx, "SELECT id, source, kind, sealed, expires_at, created_at FROM credential WHERE id = ?", id).
		Scan(&c.ID, &c.Source, &c.Kind, &c.Sealed, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNoCredential
	}
	if err != nil {
		return Credential{}, err
	}
	if expires.Valid {
		c.ExpiresAt = fromUnix(expires.Int64)
	}
	c.CreatedAt = fromUnix(created)
	return c, nil
}

// DeleteCredential removes the credential with id; one that is not there is not an error.
func (s *DB) DeleteCredential(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM credential WHERE id = ?", id)
	return err
}
