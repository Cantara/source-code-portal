package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
)

func exec(t *testing.T, mod func(*Env), args ...string) (code int, out, errOut string) {
	t.Helper()
	var so, se bytes.Buffer
	env := Env{Args: args, Stdin: strings.NewReader(""), Stdout: &so, Stderr: &se,
		Listen: func(a string) (net.Listener, error) { return listenTCP(a) }}
	if mod != nil {
		mod(&env)
	}
	code = run(t.Context(), env)
	return code, so.String(), se.String()
}

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDispatch(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int
	}{
		{nil, exitUsage},
		{[]string{"nonsense"}, exitUsage},
		{[]string{"config"}, exitUsage}, // a group word alone is not a command
		{[]string{"help"}, exitOK},
		{[]string{"-h"}, exitOK},
		{[]string{"help", "config", "lint"}, exitOK},
		{[]string{"version"}, exitOK},
		{[]string{"--version"}, exitOK},
		{[]string{"version", "extra"}, exitUsage},
		{[]string{"version", "--bogus"}, exitUsage},
		{[]string{"config", "lint"}, exitUsage}, // no file and no --config
	} {
		if code, _, _ := exec(t, nil, tc.args...); code != tc.want {
			t.Errorf("scp %v = %d, want %d", tc.args, code, tc.want)
		}
	}
	code, out, _ := exec(t, nil, "version", "--json")
	var bi BuildInfo
	if code != 0 || json.Unmarshal([]byte(out), &bi) != nil || bi.Version == "" || bi.SPA != "placeholder" {
		t.Fatalf("version --json: %d %q", code, out)
	}
}

func TestEveryCommandIsReachableAndDocumented(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands() {
		if c.summary == "" || c.run == nil {
			t.Errorf("%s: missing summary or run", c.name())
		}
		if seen[c.name()] {
			t.Errorf("duplicate command %s", c.name())
		}
		seen[c.name()] = true
		if got, _, ok := lookup(append([]string{}, c.words...)); !ok || got.name() != c.name() {
			t.Errorf("%s: not found by lookup", c.name())
		}
	}
	if len(seen) != 13 {
		t.Errorf("the command table has %d entries; design 16 section 4.1 lists 13 (the architecture's nine, config reference|env|show of 02, and healthcheck)", len(seen))
	}
}

// loopbackAdmin is the one line that makes a test document warning-free (the default admin listener is open to the network).
const loopbackAdmin = "server:\n  admin: {listen: \"127.0.0.1:9091\"}\n"

func TestConfigLintExitCodes(t *testing.T) {
	ok := write(t, "ok.yaml", "version: 1\n"+loopbackAdmin)
	unknown := write(t, "unknown.yaml", "version: 1\nserver:\n  bogus: 1\n")
	bad := write(t, "bad.yaml", "version: 2\nlog: {level: loud}\n")
	warn := write(t, "warn.yaml", "version: 1\n") // defaults: the admin listener is open to the network
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"ok", []string{"config", "lint", "--strict", ok}, exitOK},
		{"unknown key", []string{"config", "lint", unknown}, exitInvalid},
		{"two errors", []string{"config", "lint", bad}, exitInvalid},
		{"warning only", []string{"config", "lint", warn}, exitOK},
		{"warning only strict", []string{"config", "lint", "--strict", warn}, exitWarn},
		{"missing file", []string{"config", "lint", filepath.Join(t.TempDir(), "nope.yaml")}, exitFail},
		{"several files, one bad", []string{"config", "lint", ok, bad}, exitInvalid},
		{"no file", []string{"config", "lint"}, exitUsage},
	} {
		if code, out, errOut := exec(t, nil, tc.args...); code != tc.want {
			t.Errorf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", tc.name, code, tc.want, out, errOut)
		}
	}
	// Every violation is listed, with the line of the key.
	_, out, _ := exec(t, nil, "config", "lint", unknown)
	if !strings.Contains(out, "unknown.yaml:3") || !strings.Contains(out, "bogus") {
		t.Errorf("unknown key must be reported with its line:\n%s", out)
	}
	// --json prints every finding of every file as an array, with the file name.
	_, out, _ = exec(t, nil, "config", "lint", "--json", bad)
	var rows []lintRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) < 2 || rows[0].File != bad || rows[0].Severity != "error" {
		t.Fatalf("lint --json: %v %q", err, out)
	}
	_, out, _ = exec(t, nil, "config", "lint", "--json", ok)
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("a clean --json run prints [], got %q", out)
	}
	// SCP_CONFIG is the default of --config; lint ignores every other SCP_ variable (it reads the file alone).
	if code, _, _ := exec(t, func(e *Env) { e.Environ = []string{"SCP_CONFIG=" + ok, "SCP_SERVER_ADMIN_LISTEN=:9090"} }, "config", "lint"); code != exitOK {
		t.Errorf("SCP_CONFIG default: %d", code)
	}
}

func TestConfigSchemaReferenceEnvShow(t *testing.T) {
	code, out, _ := exec(t, nil, "config", "schema")
	var doc map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &doc) != nil || doc["$schema"] == nil {
		t.Fatalf("schema: %d %.200s", code, out)
	}
	if code, out, _ := exec(t, nil, "config", "env"); code != exitOK || !strings.Contains(out, "SCP_SHUTDOWN_SUPERVISOR_GRACE\n") || !strings.Contains(out, "SCP_SERVER_ADMIN_BEARER_TOKEN_FILE\n") {
		t.Errorf("env names: %d\n%.300s", code, out)
	}
	if code, out, _ := exec(t, nil, "config", "reference"); code != exitOK || !strings.Contains(out, "shutdown.supervisor_grace") {
		t.Errorf("reference: %d\n%.300s", code, out)
	}
	if code, _, _ := exec(t, nil, "config", "schema", "extra"); code != exitUsage {
		t.Errorf("schema takes no arguments: %d", code)
	}

	// show: the effective document with the secret redacted, and the layer of every key that is not the default.
	f := write(t, "c.yaml", "version: 1\nserver:\n  admin: {listen: \"127.0.0.1:9091\", bearer_token: {file: /run/secrets/admin}}\nlog: {level: debug}\n")
	environ := []string{"SCP_SHUTDOWN_DRAIN_DELAY=1s", "SCP_CONFIG=" + f}
	code, out, _ = exec(t, func(e *Env) { e.Environ = environ }, "config", "show", "--set", "log.format=text")
	if code != exitOK || !strings.Contains(out, "level: debug") || !strings.Contains(out, "drain_delay: 1s") || !strings.Contains(out, "format: text") || strings.Contains(out, "/run/secrets/admin") && !strings.Contains(out, "REDACTED") {
		t.Fatalf("show: %d\n%s", code, out)
	}
	_, out, _ = exec(t, func(e *Env) { e.Environ = environ }, "config", "show", "--layers", "--set", "log.format=text")
	for _, want := range []string{"log.level", "file", "shutdown.drain_delay", "env", "log.format", "flag"} {
		if !strings.Contains(out, want) {
			t.Errorf("--layers lacks %q:\n%s", want, out)
		}
	}
	if code, _, _ := exec(t, nil, "config", "show", "--set", "log.level=loud"); code != exitInvalid {
		t.Errorf("show of an invalid configuration: %d", code)
	}
}

func TestConfigLintStrictEnv(t *testing.T) {
	ok := write(t, "ok.yaml", "version: 1\n"+loopbackAdmin)
	typo := []string{"SCP_SERVER_PUBLIC_LISTN=:1"}
	if code, _, _ := exec(t, func(e *Env) { e.Environ = typo }, "config", "lint", ok); code != exitOK {
		t.Errorf("without --strict-env the environment is not read: %d", code)
	}
	code, out, _ := exec(t, func(e *Env) { e.Environ = typo }, "config", "lint", "--strict-env", ok)
	if code != exitInvalid || !strings.Contains(out, "SCP_SERVER_PUBLIC_LISTN") {
		t.Errorf("--strict-env: %d\n%s", code, out)
	}
}

func TestDoctorExitAndRendering(t *testing.T) {
	findings := []github.Finding{
		{ID: "app.key", Severity: github.SevWarn, Source: "gh", Title: "key is 2048 bits", Remediation: "rotate to 4096"},
		{ID: "net.tls", Severity: github.SevInfo, Source: "gh", Title: "tls ok"},
		{ID: "leak", Severity: github.SevInfo, Source: "gh", Title: "oops", Detail: "token ghs_abcdef"},
	}
	fake := func(fs []github.Finding, err error) func(*Env) {
		return func(e *Env) {
			e.Doctor = func(context.Context, Config, github.DoctorOptions) ([]github.Finding, error) {
				return append([]github.Finding(nil), fs...), err
			}
		}
	}
	dir := t.TempDir()
	cfg := write(t, "c.yaml", "version: 1\n"+loopbackAdmin+"store:\n  path: "+dir+"/scp.db\n")
	for _, tc := range []struct {
		name string
		mod  func(*Env)
		args []string
		want int
	}{
		{"warn is ok", fake(findings, nil), nil, exitOK},
		{"warn strict", fake(findings, nil), []string{"--strict"}, exitWarn},
		{"fail", fake(append(findings, github.Finding{ID: "x", Severity: github.SevFail, Title: "bad"}), nil), []string{"--strict"}, exitFail},
		{"adapter error", fake(nil, errors.New("boom")), nil, exitFail},
		{"no adapter is a warning", nil, nil, exitOK},
		{"no adapter strict", nil, []string{"--strict"}, exitWarn},
		{"bad timeout", fake(findings, nil), []string{"--timeout=0s"}, exitUsage},
	} {
		code, out, _ := exec(t, tc.mod, append([]string{"doctor", "--config", cfg}, tc.args...)...)
		if code != tc.want {
			t.Errorf("%s: exit %d, want %d\n%s", tc.name, code, tc.want, out)
		}
	}
	_, out, _ := exec(t, fake(findings, nil), "doctor", "--config", cfg)
	for _, want := range []string{"== (process)", "INFO cfg.load", "INFO store.new", "== gh", "WARN app.key", "fix: rotate to 4096", "0 FAIL, 1 WARN, 4 INFO"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "== (process)") > strings.Index(out, "== gh") {
		t.Error("process-level findings come first")
	}
	if strings.Contains(out, "ghs_abcdef") || !strings.Contains(out, "withheld") {
		t.Errorf("a credential-shaped string must be withheld:\n%s", out)
	}
	_, out, _ = exec(t, fake(findings, nil), "doctor", "--config", cfg, "--json")
	var back []doctorRow
	if err := json.Unmarshal([]byte(out), &back); err != nil || len(back) != 5 || strings.Contains(out, "ghs_") {
		t.Fatalf("doctor --json: %v %s", err, out)
	}
	for _, r := range back {
		if r.Severity != "info" && r.Severity != "warn" && r.Severity != "fail" {
			t.Errorf("severity %q", r.Severity)
		}
	}
}

func TestDoctorLocalChecks(t *testing.T) {
	called := false
	spy := func(e *Env) {
		e.Doctor = func(context.Context, Config, github.DoctorOptions) ([]github.Finding, error) {
			called = true
			return nil, nil
		}
	}
	// An invalid configuration fails, and the GitHub checks are not attempted.
	bad := write(t, "bad.yaml", "version: 2\n")
	if code, out, _ := exec(t, spy, "doctor", "--config", bad); code != exitFail || called || !strings.Contains(out, "FAIL cfg.invalid") {
		t.Errorf("bad config: %d called=%v\n%s", code, called, out)
	}
	// An undecodable file is one FAIL finding.
	if code, out, _ := exec(t, spy, "doctor", "--config", write(t, "u.yaml", "bogus: 1\n")); code != exitFail || !strings.Contains(out, "cfg.invalid") || !strings.Contains(out, "bogus") {
		t.Errorf("unknown key: %d\n%s", code, out)
	}
	// A corrupt store is a FAIL with the restore hint; a missing directory too.
	db := write(t, "scp.db", "CORRUPT")
	cfg := write(t, "c.yaml", "version: 1\n"+loopbackAdmin+"store:\n  path: "+db+"\n")
	if code, out, _ := exec(t, spy, "doctor", "--config", cfg); code != exitFail || !strings.Contains(out, "FAIL store.integrity") || !strings.Contains(out, "scp store restore") {
		t.Errorf("corrupt store: %d\n%s", code, out)
	}
	cfg = write(t, "d.yaml", "version: 1\n"+loopbackAdmin+"store:\n  path: /definitely/not/here/scp.db\n")
	if code, out, _ := exec(t, spy, "doctor", "--config", cfg); code != exitFail || !strings.Contains(out, "store.dir") {
		t.Errorf("missing dir: %d\n%s", code, out)
	}
	// The sources flag reaches the adapter.
	var got github.DoctorOptions
	dir := t.TempDir()
	cfg = write(t, "e.yaml", "version: 1\nserver:\n  public: {public_url: \"https://scp.example.org\"}\n  admin: {listen: \"127.0.0.1:9091\"}\nstore:\n  path: "+dir+"/x.db\n")
	exec(t, func(e *Env) {
		e.Doctor = func(_ context.Context, _ Config, o github.DoctorOptions) ([]github.Finding, error) {
			got = o
			return nil, nil
		}
	}, "doctor", "--config", cfg, "--source", "a, b", "--offline")
	if len(got.Sources) != 2 || got.Sources[1] != "b" || !got.Offline || got.PublicURL != "https://scp.example.org" {
		t.Errorf("DoctorOptions = %+v", got)
	}
}

func TestStoreCommands(t *testing.T) {
	good := write(t, "good.db", "SQLITE good")
	corrupt := write(t, "bad.db", "CORRUPT")
	if code, _, _ := exec(t, nil, "store", "check", "--db", good); code != exitOK {
		t.Errorf("check good: %d", code)
	}
	if code, out, _ := exec(t, nil, "store", "check", "--db", corrupt); code != exitFail || !strings.Contains(out, "CORRUPT") {
		t.Errorf("check corrupt: %d %q", code, out)
	}
	if code, _, _ := exec(t, nil, "store", "check"); code != exitUsage {
		t.Errorf("check without --db: %d", code)
	}
	// V16-07: the spelling of the operator runbook in 03 section 12 (`--file`, `--full`) works too
	if code, _, _ := exec(t, nil, "store", "check", "--file", good, "--full"); code != exitOK {
		t.Errorf("check --file --full: %d", code)
	}
	if code, _, _ := exec(t, nil, "store", "check", "--db", good, "--quick", "--full"); code != exitUsage {
		t.Errorf("--quick with --full must be a usage error: %d", code)
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "scp.db")
	if err := os.WriteFile(db, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := exec(t, nil, "store", "restore", "--db", db, "--from", good); code != exitUsage {
		t.Errorf("restore without --yes must refuse with 2, got %d", code)
	}
	if code, _, _ := exec(t, nil, "store", "restore", "--db", db, "--from", corrupt, "--yes"); code != exitFail {
		t.Errorf("restore of a corrupt backup must fail, got %d", code)
	}
	if b, _ := os.ReadFile(db); string(b) != "old" {
		t.Errorf("a failed restore must leave the live file alone, got %q", b)
	}
	if code, _, _ := exec(t, nil, "store", "restore", "--db", db, "--from", good, "--yes"); code != exitOK {
		t.Errorf("restore good: %d", code)
	}
	if b, _ := os.ReadFile(db); string(b) != "SQLITE good" {
		t.Errorf("restored content = %q", b)
	}
}

func TestDescriptorLint(t *testing.T) {
	ok := write(t, "scp.yaml", "schema: scp/v1\n")
	bad := write(t, "bad.yaml", "title: x\n")
	if code, _, _ := exec(t, nil, "descriptor", "lint", ok); code != exitOK {
		t.Errorf("ok: %d", code)
	}
	if code, out, _ := exec(t, nil, "descriptor", "lint", ok, bad); code != exitInvalid || !strings.Contains(out, bad) {
		t.Errorf("bad: %d %s", code, out)
	}
	if code, _, _ := exec(t, nil, "descriptor", "lint"); code != exitUsage {
		t.Errorf("no file: %d", code)
	}
	if code, _, _ := exec(t, nil, "descriptor", "lint", filepath.Join(t.TempDir(), "nope.yaml")); code != exitFail {
		t.Errorf("unreadable file is an I/O failure, not a violation: %d", code)
	}
}

func TestSchemaDescriptor(t *testing.T) {
	code, out, _ := exec(t, nil, "config", "schema", "--descriptor")
	if code != exitOK || !json.Valid([]byte(out)) {
		t.Fatalf("descriptor schema: %d %q", code, out)
	}
}

// ---- github app manifest: flags, defaults and exit codes (the flow itself is github.RunAppManifest, design 05) ----

func TestAppManifestWrapper(t *testing.T) {
	var got github.ManifestOptions
	ok := func(e *Env) {
		e.AppManifest = func(_ context.Context, o github.ManifestOptions) (github.ManifestResult, error) {
			got = o
			return github.ManifestResult{AppID: 7, Slug: "scp-acme", PEMPath: "/o/scp-acme.pem", WebhookSecretPath: "/o/scp-acme.whsec", InstallURL: "https://github.com/apps/scp-acme/installations/new", ConfigSnippet: "github:\n  sources: []\n"}, nil
		}
	}
	cfg := write(t, "c.yaml", "version: 1\nserver:\n  public: {public_url: \"https://scp.example.org\"}\n  admin: {listen: \"127.0.0.1:9091\"}\n")
	code, out, errOut := exec(t, ok, "github", "app", "manifest", "--config", cfg, "--org", "acme", "--source", "acme", "--features", "core,checks", "--out-dir", t.TempDir())
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got.WebhookURL != "https://scp.example.org/webhooks/github/acme" || got.APIURL != "" || got.Org != "acme" || got.Listen != "127.0.0.1:0" || got.Timeout != 15*time.Minute {
		t.Errorf("options = %+v", got)
	}
	if len(got.Features) != 2 || got.Features[0] != github.FeatureCore || got.Features[1] != github.FeatureChecks {
		t.Errorf("features = %v", got.Features)
	}
	for _, want := range []string{"installations/new", "/o/scp-acme.pem", "github:", "scp doctor"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// The API URL is the adapter's to derive (05 section 4.1 step 2): cmd/scp passes the flag through and nothing else.
	exec(t, ok, "github", "app", "manifest", "--config", cfg, "--web-url", "https://ghe.corp.example", "--api-url", "https://ghe.corp.example/x", "--out-dir", t.TempDir())
	if got.WebURL != "https://ghe.corp.example" || got.APIURL != "https://ghe.corp.example/x" {
		t.Errorf("urls = %q %q", got.WebURL, got.APIURL)
	}
	// No public_url and no flag: usage error; --no-webhook needs neither.
	empty := write(t, "e.yaml", "version: 1\n"+loopbackAdmin)
	if c, _, _ := exec(t, ok, "github", "app", "manifest", "--config", empty); c != exitUsage {
		t.Errorf("no webhook URL: %d", c)
	}
	if c, _, _ := exec(t, ok, "github", "app", "manifest", "--config", empty, "--no-webhook"); c != exitOK || got.WebhookURL != "" || !got.NoWebhook {
		t.Errorf("--no-webhook: %d %+v", c, got)
	}
	// The adapter's error is exit 1; a missing adapter is exit 1.
	boom := func(e *Env) {
		e.AppManifest = func(context.Context, github.ManifestOptions) (github.ManifestResult, error) {
			return github.ManifestResult{}, errors.New("state mismatch")
		}
	}
	if c, _, errOut := exec(t, boom, "github", "app", "manifest", "--config", empty, "--no-webhook"); c != exitFail || !strings.Contains(errOut, "state mismatch") {
		t.Errorf("adapter error: %d %s", c, errOut)
	}
	if c, _, _ := exec(t, nil, "github", "app", "manifest", "--config", empty, "--no-webhook"); c != exitFail {
		t.Errorf("no adapter: %d", c)
	}
	if c, _, _ := exec(t, ok, "github", "app", "manifest", "extra"); c != exitUsage {
		t.Errorf("positional: %d", c)
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	if code, _, e := exec(t, nil, "healthcheck", "--url", srv.URL+"/livez"); code != exitOK {
		t.Errorf("200: %d %s", code, e)
	}
	status = http.StatusServiceUnavailable
	if code, _, _ := exec(t, nil, "healthcheck", "--url", srv.URL+"/readyz"); code != exitFail {
		t.Errorf("503: %d", code)
	}
	if code, _, _ := exec(t, nil, "healthcheck", "--url", srv.URL+"/redir"); code != exitFail {
		t.Errorf("a redirect is not healthy (and must not be followed): %d", code)
	}
	if code, _, _ := exec(t, nil, "healthcheck", "--url", "http://169.254.169.254/latest/meta-data"); code != exitUsage {
		t.Errorf("a non-loopback target must be refused: %d", code)
	}
	if code, _, _ := exec(t, nil, "healthcheck", "--url", "http://127.0.0.1:1/", "--timeout", "200ms"); code != exitFail {
		t.Errorf("connection refused: %d", code)
	}
}
