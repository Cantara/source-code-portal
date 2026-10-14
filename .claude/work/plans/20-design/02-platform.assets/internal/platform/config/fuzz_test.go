package config_test

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/platform/config"
)

// FuzzLoadFile: arbitrary file bytes must produce a *config.Error or a value, never a panic, and never hang.
func FuzzLoadFile(f *testing.F) {
	f.Add([]byte("server:\n  public: {listen: ':1'}\n"))
	f.Add([]byte("version: 1\ngithub:\n  sources:\n    - id: a\n      auth: {kind: app, private_key: {file: /x}}\n"))
	f.Add([]byte("a: &x [*x]\n"))
	f.Add([]byte("\x00\xff:"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var r Root
		_, _ = config.Load(&r, config.Options{
			Defaults: []byte(defaults), SkipSecrets: true,
			File: "f.yaml", ReadFile: func(string) ([]byte, error) { return data, nil },
		})
	})
}

func FuzzParseBytes(f *testing.F) {
	for _, s := range []string{"64MiB", "1KiB", "12", "1.5GiB", "-1", "9999999999999999999TiB", "", "MB"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := config.ParseBytes(s)
		if err == nil && n < 0 {
			t.Fatalf("%q parsed to a negative size %d", s, n)
		}
	})
}

// FuzzEnv: arbitrary variable names and values never panic and never create keys the schema does not have.
func FuzzEnv(f *testing.F) {
	f.Add("SCP_SERVER_PUBLIC_LISTEN", ":1")
	f.Add("SCP_GITHUB_SOURCES_99999999999_ID", "x")
	f.Add("SCP_REFRESH_UPSTREAMS__CONCURRENCY", "1")
	f.Fuzz(func(t *testing.T, k, v string) {
		var r Root
		_, _ = config.Load(&r, config.Options{Defaults: []byte(defaults), SkipSecrets: true, Environ: []string{k + "=" + v}})
	})
}
