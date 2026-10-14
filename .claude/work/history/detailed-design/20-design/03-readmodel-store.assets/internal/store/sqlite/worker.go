package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

const touchFlushEvery = 5 * time.Minute

// worker holds the timing logic of Run with its effects injected, so it is unit-testable inside a synctest bubble
// without SQLite.
type worker struct {
	probeEvery, backupEvery time.Duration
	backupEnabled           bool
	firstBackup             time.Duration // delay before the first backup (see firstBackupDelay)
	healthy                 func() bool
	probe                   func(ctx context.Context) error
	backup                  func(ctx context.Context) error
	flush                   func(ctx context.Context) error
	onErr                   func(op string, err error)
}

// firstBackupDelay: resume the cadence from the backup file's age, never earlier than 5 minutes after start (the cold-start
// storm), never later than one interval.
func firstBackupDelay(interval time.Duration, age time.Duration, exists bool) time.Duration {
	if !exists {
		return min(interval, 5*time.Minute)
	}
	return min(max(interval-age, 5*time.Minute), interval)
}

func (w *worker) run(ctx context.Context) {
	probe := time.NewTicker(w.probeEvery)
	flush := time.NewTicker(touchFlushEvery)
	defer probe.Stop()
	defer flush.Stop()
	var backupC <-chan time.Time
	var bt *time.Timer
	if w.backupEnabled {
		bt = time.NewTimer(w.firstBackup)
		defer bt.Stop()
		backupC = bt.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-probe.C:
			if !w.healthy() {
				if err := w.probe(ctx); err != nil {
					w.onErr("probe", err)
				}
			}
		case <-flush.C:
			if w.healthy() {
				if err := w.flush(ctx); err != nil {
					w.onErr("flush", err)
				}
			}
		case <-backupC:
			if w.healthy() {
				if err := w.backup(ctx); err != nil {
					w.onErr("backup", err)
				}
			}
			bt.Reset(w.backupEvery) // fixed delay: the next interval starts after completion
		}
	}
}

// Run blocks until ctx is done: degraded-state write probe, hourly backup, recency flush. It returns nil on cancellation.
func (s *Store) Run(ctx context.Context) error {
	w := &worker{probeEvery: s.cfg.ProbeInterval, backupEvery: s.cfg.Backup.Interval, backupEnabled: s.cfg.Backup.Enabled,
		healthy: func() bool { return s.Health().OK },
		probe:   s.probe,
		backup:  func(ctx context.Context) error { return s.Backup(ctx, s.cfg.BackupPath()) },
		flush:   s.FlushTouches,
		onErr:   func(op string, err error) { s.log.Warn("store worker task failed", "task", op, "err", err) }}
	if s.cfg.Backup.Enabled {
		fi, err := os.Stat(s.cfg.BackupPath())
		w.firstBackup = firstBackupDelay(s.cfg.Backup.Interval, time.Since(fiModTime(fi)), err == nil)
	}
	w.run(ctx)
	return nil
}

func fiModTime(fi os.FileInfo) time.Time {
	if fi == nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// probe attempts a tiny write on the writer connection, bypassing the degraded gate. Success restores write service.
func (s *Store) probe(ctx context.Context) error {
	if s.closed.Load() {
		return store.ErrClosed
	}
	tx, err := s.rw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('probe',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.markHealthy()
	return nil
}

// Close must be called by lifecycle only after Run has returned: flush recency, PRAGMA optimize, optional final backup,
// wal_checkpoint(TRUNCATE), then close read handles before the writer. Idempotent.
func (s *Store) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	var errs []error
	healthy := s.Health().OK
	// writes are gated by `closed` now: use the raw connection for the final maintenance
	if healthy {
		if err := s.finalFlush(ctx); err != nil {
			errs = append(errs, err)
		}
		if _, err := s.rw.ExecContext(ctx, `PRAGMA optimize`); err != nil {
			errs = append(errs, err)
		}
	}
	if healthy && s.cfg.Backup.Enabled && s.cfg.Backup.OnShutdown {
		if err := s.backup(ctx, s.cfg.BackupPath()); err != nil {
			errs = append(errs, err)
		}
	}
	if healthy {
		var busy, logf, ckpt int
		if err := s.rw.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logf, &ckpt); err != nil {
			errs = append(errs, err)
		} else if busy != 0 {
			s.log.Warn("wal checkpoint could not truncate: a reader was active", "log_frames", logf, "checkpointed", ckpt)
		}
	}
	for _, db := range []*sql.DB{s.ro, s.bk, s.rw} {
		if err := db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) finalFlush(ctx context.Context) error {
	s.touchMu.Lock()
	pending := s.touched
	s.touched = map[touchKey]uint64{}
	s.touchMu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	tx, err := s.rw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, seq := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE cache SET use_seq=?3 WHERE ns=?1 AND key=?2 AND use_seq<?3`, k.ns, k.key, seq); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
