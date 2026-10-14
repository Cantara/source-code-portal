package scp_test

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

// The shipped config machinery (platform/config) decodes strictly through go.yaml.in/yaml/v3 KnownFields(true); this test
// pins that the store block decodes that way: durations, ByteSize via encoding.TextUnmarshaler, unknown keys rejected.
func TestStoreConfigStrictDecode(t *testing.T) {
	decode := func(doc string) (sqlite.Config, error) {
		var w struct {
			Store sqlite.Config `yaml:"store"`
		}
		w.Store = sqlite.DefaultConfig()
		d := yaml.NewDecoder(strings.NewReader(doc))
		d.KnownFields(true)
		err := d.Decode(&w)
		return w.Store, err
	}
	c, err := decode("store:\n  path: /x/scp.db\n  busy_timeout: 2s\n  journal_size_limit: 32MiB\n  backup: {enabled: true, interval: 30m}\n  cache_caps: {blob: 8MiB, depsdev: 4MiB}\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.JournalSizeLimit != 32<<20 || c.CacheCaps.Blob != 8<<20 || c.CacheCaps.DepsDev != 4<<20 || !c.Backup.Enabled {
		t.Fatalf("%+v", c)
	}
	for _, bad := range []string{"store:\n  bogus: 1\n", "store:\n  journal_size_limit: 64MB\n", "store:\n  journal_size_limit: 9999999999GiB\n", "store:\n  busy_timeout: soon\n"} {
		if _, err := decode(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if errs := c.Validate("store"); len(errs) != 0 {
		t.Fatal(errs)
	}
}
