package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/store"
)

const (
	touchMax    = 8192 // bound of the in-memory recency map; beyond it a read is not recorded (approximate LRU)
	evictBatch  = 256
	lowWaterDiv = 10 // evict down to cap - cap/10
)

func (s *Store) nextSeq() uint64 { return s.seq.Add(1) }

// GetCache reads from the read pool and records recency in memory only: a read never takes the writer.
func (s *Store) GetCache(ctx context.Context, ns, key string) ([]byte, bool, error) {
	if err := store.ValidateCacheArgs(ns, key); err != nil {
		return nil, false, err
	}
	if _, ok := s.caps[ns]; !ok {
		return nil, false, store.ErrUnknownNamespace
	}
	var data []byte
	found := false
	err := s.read(ctx, "GetCache", func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT data FROM cache WHERE ns=?1 AND key=?2`, ns, key).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return nil, false, err
	}
	res := "miss"
	if found {
		res = "hit"
		s.touchMu.Lock()
		if len(s.touched) < touchMax {
			s.touched[touchKey{ns, key}] = s.nextSeq()
		}
		s.touchMu.Unlock()
	}
	s.m.cacheOps.Add(ctx, 1, metric.WithAttributes(attribute.String("ns", ns), attribute.String("result", res)))
	return data, found, nil
}

// flushTouchTx writes pending recency ticks inside an existing write transaction.
func (s *Store) flushTouchTx(ctx context.Context, tx *sql.Tx) error {
	s.touchMu.Lock()
	pending := s.touched
	s.touched = make(map[touchKey]uint64, len(pending))
	s.touchMu.Unlock()
	for k, seq := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE cache SET use_seq=?3 WHERE ns=?1 AND key=?2 AND use_seq<?3`, k.ns, k.key, seq); err != nil {
			return err
		}
	}
	return nil
}

// FlushTouches is called by Run every few minutes and by Close.
func (s *Store) FlushTouches(ctx context.Context) error {
	s.touchMu.Lock()
	n := len(s.touched)
	s.touchMu.Unlock()
	if n == 0 {
		return nil
	}
	return s.write(ctx, "FlushTouches", func(tx *sql.Tx) (func(), error) { return nil, s.flushTouchTx(ctx, tx) })
}

// PutCache upserts one value and evicts least recently used entries of the namespace down to 90% of its cap when the cap
// is exceeded, in the same transaction. A value larger than the cap is refused (it would evict itself).
func (s *Store) PutCache(ctx context.Context, ns, key string, data []byte) error {
	if err := store.ValidateCacheArgs(ns, key); err != nil {
		return err
	}
	capB, ok := s.caps[ns]
	if !ok {
		return store.ErrUnknownNamespace
	}
	if int64(len(data)) > capB {
		return store.ErrTooLarge
	}
	if data == nil {
		data = []byte{} // the column is NOT NULL; a nil value is stored (and read back) as an empty one, like store/memory
	}
	s.putMu.Lock() // one PutCache at a time end to end: accounting is updated after commit, before the next put reads it (V-06)
	defer s.putMu.Unlock()
	return s.write(ctx, "PutCache", func(tx *sql.Tx) (func(), error) {
		if err := s.flushTouchTx(ctx, tx); err != nil {
			return nil, err
		}
		var old int64
		if err := tx.QueryRowContext(ctx, `SELECT size FROM cache WHERE ns=?1 AND key=?2`, ns, key).Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cache(ns,key,data,size,use_seq) VALUES(?1,?2,?3,?4,?5)
ON CONFLICT(ns,key) DO UPDATE SET data=excluded.data, size=excluded.size, use_seq=excluded.use_seq`, ns, key, data, len(data), s.nextSeq()); err != nil {
			return nil, err
		}
		s.cacheMu.Lock()
		total := s.cacheBytes[ns] + int64(len(data)) - old
		s.cacheMu.Unlock()
		freed, evicted := int64(0), int64(0)
		if total > capB {
			target := capB - capB/lowWaterDiv
			for total-freed > target {
				rows, err := tx.QueryContext(ctx, `SELECT key, size FROM cache WHERE ns=?1 AND key<>?2 ORDER BY use_seq, rowid LIMIT ?3`, ns, key, evictBatch)
				if err != nil {
					return nil, err
				}
				type victim struct {
					key  string
					size int64
				}
				var vs []victim
				for rows.Next() {
					var v victim
					if err := rows.Scan(&v.key, &v.size); err != nil {
						rows.Close()
						return nil, err
					}
					vs = append(vs, v)
				}
				if err := rows.Close(); err != nil {
					return nil, err
				}
				if len(vs) == 0 {
					break
				}
				for _, v := range vs {
					if total-freed <= target {
						break
					}
					if _, err := tx.ExecContext(ctx, `DELETE FROM cache WHERE ns=?1 AND key=?2`, ns, v.key); err != nil {
						return nil, err
					}
					freed += v.size
					evicted++
				}
			}
		}
		return func() {
			s.cacheMu.Lock()
			s.cacheBytes[ns] = total - freed
			s.cacheMu.Unlock()
			if evicted > 0 {
				s.m.cacheEvict.Add(ctx, evicted, metric.WithAttributes(attribute.String("ns", ns)))
			}
		}, nil
	})
}
