// Package sqlite is a STUB of the offline surface of 03-readmodel-store.md 3.5 used by `scp store check|restore`.
package sqlite

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/cantara/source-code-portal/internal/store"
)

// CheckFile never writes. The stub treats a file starting with "CORRUPT" as corrupt.
func CheckFile(_ context.Context, path string, _ bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("unable to open database file (14)") // the real CheckFile does not wrap fs.ErrNotExist either (verified against 03's sqlite)
	}
	if strings.HasPrefix(string(b), "CORRUPT") {
		return store.ErrCorrupt
	}
	return nil
}

// Restore stub: verifies the backup, moves the old file aside, copies the backup in.
func Restore(ctx context.Context, dbPath, backupPath string, keep int) error {
	if err := CheckFile(ctx, backupPath, true); err != nil {
		return err
	}
	b, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, dbPath+".quarantine"); err != nil {
			return err
		}
	}
	return os.WriteFile(dbPath, b, 0o600)
}
