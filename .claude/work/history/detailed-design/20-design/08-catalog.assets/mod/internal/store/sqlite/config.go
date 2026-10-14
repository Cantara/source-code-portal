// Package sqlite implements store.Store on SQLite through modernc.org/sqlite (CGO-free).
package sqlite

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ByteSize is a byte count that decodes "64MiB", "1GiB", "512KiB", "1048576".
type ByteSize int64

func (b *ByteSize) UnmarshalText(t []byte) error {
	s := strings.TrimSpace(string(t))
	mult := int64(1)
	for _, u := range []struct {
		suf string
		m   int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			s, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suf)), u.m
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult { // overflow would wrap into a "valid" size
		return fmt.Errorf("invalid byte size %q (want e.g. 64MiB)", string(t))
	}
	*b = ByteSize(n * mult)
	return nil
}
func (b ByteSize) MarshalText() ([]byte, error) { return []byte(strconv.FormatInt(int64(b), 10)), nil }

type BackupConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Interval   time.Duration `yaml:"interval"`
	Path       string        `yaml:"path"` // default: <store.path>.bak
	OnShutdown bool          `yaml:"on_shutdown"`
}

// CacheCaps keeps the YAML shape of the architecture (store.cache_caps.{blob,parse,locked}); cmd/scp maps each field to the
// namespace constant exported by the owning feature and passes the result as Deps.CacheNamespaces.
type CacheCaps struct {
	Blob    ByteSize `yaml:"blob"`
	Parse   ByteSize `yaml:"parse"`
	Locked  ByteSize `yaml:"locked"`
	DepsDev ByteSize `yaml:"depsdev"`
}

// Config is the `store:` block. Env names are SCP_STORE_<KEY>, for example SCP_STORE_BACKUP_INTERVAL.
type Config struct {
	Path             string        `yaml:"path"`
	Sync             string        `yaml:"sync"` // normal | full
	BusyTimeout      time.Duration `yaml:"busy_timeout"`
	ReadConns        int           `yaml:"read_conns"`
	JournalSizeLimit ByteSize      `yaml:"journal_size_limit"`
	QuarantineKeep   int           `yaml:"quarantine_keep"`
	ProbeInterval    time.Duration `yaml:"probe_interval"`
	Backup           BackupConfig  `yaml:"backup"`
	CacheCaps        CacheCaps     `yaml:"cache_caps"`
}

func DefaultConfig() Config {
	return Config{Path: "/data/scp.db", Sync: "normal", BusyTimeout: 5 * time.Second, ReadConns: 2,
		JournalSizeLimit: 64 << 20, QuarantineKeep: 2, ProbeInterval: 30 * time.Second,
		Backup:    BackupConfig{Interval: time.Hour, OnShutdown: true},
		CacheCaps: CacheCaps{Blob: 64 << 20, Parse: 128 << 20, Locked: 64 << 20, DepsDev: 16 << 20}}
}

// BackupPath is the effective backup file.
func (c Config) BackupPath() string {
	if c.Backup.Path != "" {
		return c.Backup.Path
	}
	return c.Path + ".bak"
}

// Validate collects every violation with a YAML-like path (prefix is "store").
func (c Config) Validate(prefix string) []error {
	var errs []error
	bad := func(key, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s.%s: %s", prefix, key, fmt.Sprintf(f, a...)))
	}
	switch {
	case c.Path == "":
		bad("path", "required")
	case strings.ContainsRune(c.Path, 0):
		bad("path", "contains NUL")
	case c.Path == ":memory:" || strings.HasPrefix(c.Path, "file:"):
		bad("path", "must be a plain file path (not :memory: or a file: URI)")
	}
	if c.Sync != "normal" && c.Sync != "full" {
		bad("sync", "must be normal or full, got %q", c.Sync)
	}
	if c.BusyTimeout < 100*time.Millisecond || c.BusyTimeout > time.Minute {
		bad("busy_timeout", "must be between 100ms and 1m, got %s", c.BusyTimeout)
	}
	if c.ReadConns < 1 || c.ReadConns > 8 {
		bad("read_conns", "must be 1..8, got %d", c.ReadConns)
	}
	if c.JournalSizeLimit < 1<<20 || c.JournalSizeLimit > 1<<30 {
		bad("journal_size_limit", "must be between 1MiB and 1GiB")
	}
	if c.QuarantineKeep < 1 || c.QuarantineKeep > 10 {
		bad("quarantine_keep", "must be 1..10, got %d", c.QuarantineKeep)
	}
	if c.ProbeInterval < 5*time.Second || c.ProbeInterval > 10*time.Minute {
		bad("probe_interval", "must be between 5s and 10m, got %s", c.ProbeInterval)
	}
	if c.Backup.Enabled {
		if c.Backup.Interval < time.Minute {
			bad("backup.interval", "must be at least 1m, got %s", c.Backup.Interval)
		}
		if a, b := filepath.Clean(c.BackupPath()), filepath.Clean(c.Path); a == b {
			bad("backup.path", "must differ from store.path")
		}
	}
	for _, x := range []struct {
		key    string
		v, max ByteSize
	}{{"cache_caps.blob", c.CacheCaps.Blob, 4 << 30}, {"cache_caps.parse", c.CacheCaps.Parse, 4 << 30},
		{"cache_caps.locked", c.CacheCaps.Locked, 4 << 30}, {"cache_caps.depsdev", c.CacheCaps.DepsDev, 256 << 20}} {
		if x.v < 1<<20 || x.v > x.max {
			bad(x.key, "must be between 1MiB and %s", map[ByteSize]string{4 << 30: "4GiB", 256 << 20: "256MiB"}[x.max])
		}
	}
	return errs
}
