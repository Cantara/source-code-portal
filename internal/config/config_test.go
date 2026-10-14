package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/pflag"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scp.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	cfg, _, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Server: Server{PublicAddr: ":9090", AdminAddr: ":9091", ShutdownGrace: 20 * time.Second},
		Log:    Log{Level: "info", Format: "json"},
	}
	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Errorf("defaults (-want +got):\n%s", diff)
	}
}

func TestPrecedence(t *testing.T) {
	path := writeFile(t, "server:\n  public_addr: \":8000\"\n  admin_addr: \":8001\"\nlog:\n  level: warn\n")
	t.Setenv("SCP_SERVER_ADMIN_ADDR", ":8002")
	t.Setenv("SCP_LOG_LEVEL", "error")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("log-level", "", "")
	if err := flags.Parse([]string{"--log-level=debug"}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path, flags)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.PublicAddr != ":8000" {
		t.Errorf("file: public_addr = %q, want :8000", cfg.Server.PublicAddr)
	}
	if cfg.Server.AdminAddr != ":8002" {
		t.Errorf("env over file: admin_addr = %q, want :8002", cfg.Server.AdminAddr)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("flag over env: level = %q, want debug", cfg.Log.Level)
	}
}

func TestRefused(t *testing.T) {
	tests := map[string]struct {
		file string
		env  map[string]string
	}{
		"unknown file key":   {file: "server:\n  public_adr: \":1\"\n"},
		"unknown section":    {file: "sever:\n  x: 1\n"},
		"wrong type":         {file: "server:\n  shutdown_grace: soon\n"},
		"bad yaml":           {file: "server: [\n"},
		"unknown env":        {env: map[string]string{"SCP_SERVER_PUBLIC_ADR": ":1"}},
		"bad level":          {env: map[string]string{"SCP_LOG_LEVEL": "loud"}},
		"bad address":        {env: map[string]string{"SCP_SERVER_PUBLIC_ADDR": "9090"}},
		"same addresses":     {env: map[string]string{"SCP_SERVER_ADMIN_ADDR": ":9090"}},
		"non-positive grace": {env: map[string]string{"SCP_SERVER_SHUTDOWN_GRACE": "0s"}},
		"unknown log format": {env: map[string]string{"SCP_LOG_FORMAT": "xml"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := ""
			if tc.file != "" {
				path = writeFile(t, tc.file)
			}
			_, _, err := Load(path, nil)
			var inv *Invalid
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *Invalid", err)
			}
		})
	}
}

func TestAllProblemsReported(t *testing.T) {
	t.Setenv("SCP_LOG_LEVEL", "loud")
	t.Setenv("SCP_LOG_FORMAT", "xml")
	_, _, err := Load("", nil)
	var inv *Invalid
	if !errors.As(err, &inv) || len(inv.Problems) != 2 {
		t.Fatalf("err = %v, want two problems", err)
	}
}

func TestMissingFile(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "absent.yaml"), nil)
	var inv *Invalid
	if err == nil || errors.As(err, &inv) {
		t.Fatalf("err = %v, want a read error", err)
	}
}
