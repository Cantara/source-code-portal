package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/config"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestExitCodes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	tests := []struct {
		args []string
		code int
	}{
		{[]string{"version"}, exitOK},
		{[]string{"--help"}, exitOK},
		{[]string{"config", "check"}, exitOK},
		{[]string{"bogus"}, exitUsage},
		{[]string{"version", "extra"}, exitUsage},
		{[]string{"serve", "--no-such-flag"}, exitUsage},
		{[]string{"config", "check", "--log-level", "loud"}, exitUsage},
		{[]string{"config", "check", "--config", missing}, exitFail},
	}
	for _, tc := range tests {
		if code, _, stderr := execute(t, tc.args...); code != tc.code {
			t.Errorf("scp %s: exit %d, want %d (stderr %q)", strings.Join(tc.args, " "), code, tc.code, stderr)
		}
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, _ := execute(t, "version", "--json")
	var bi BuildInfo
	if code != exitOK || json.Unmarshal([]byte(stdout), &bi) != nil || bi.Version != "dev" || bi.SPA != "placeholder" {
		t.Fatalf("version --json: exit %d, %q", code, stdout)
	}
}

func TestConfigCheckPrintsEffectiveSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.yaml")
	if err := os.WriteFile(path, []byte("server:\n  public_addr: \":8080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := execute(t, "config", "check", "-c", path)
	if code != exitOK || !strings.Contains(stdout, `public_addr: :8080`) || !strings.Contains(stdout, "admin_addr: :9091") {
		t.Fatalf("config check: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestServeAndShutdown(t *testing.T) {
	cfg, _, err := config.Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()

	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range []string{
		"http://" + srv.adminLn.Addr().String() + "/livez",
		"http://" + srv.publicLn.Addr().String() + "/",
	} {
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %d", url, resp.StatusCode)
		}
	}
	client.CloseIdleConnections()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}
