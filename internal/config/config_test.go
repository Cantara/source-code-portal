package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/pflag"
)

// minimal is the smallest valid configuration; %s is the token file path.
const minimal = "github:\n  sources:\n    - id: gh\n      orgs: [Cantara]\n      token_file: %s\n"

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// file writes a config file: the minimal configuration plus extra YAML.
func file(t *testing.T, extra string) string {
	t.Helper()
	token := write(t, "token", "ghp_test\n")
	return write(t, "scp.yaml", strings.Replace(minimal, "%s", token, 1)+extra)
}

func TestDefaults(t *testing.T) {
	cfg, _, err := Load(file(t, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Server: Server{PublicAddr: ":9090", AdminAddr: ":9091", ShutdownGrace: 20 * time.Second, RawTrustedProxies: []string{}},
		Log:    Log{Level: "info", Format: "json"},
		Store:  Store{Path: "/data/scp.db"},
		Sync:   Sync{Concurrency: 4},
		GitHub: GitHub{Sources: []Source{{
			ID: "gh", BaseURL: DefaultBaseURL, Orgs: []string{"Cantara"}, TokenFile: cfg.GitHub.Sources[0].TokenFile,
			IncludeArchived: true, Token: NewSecret([]byte("ghp_test")),
		}}},
		Catalog: Catalog{ReconcileInterval: 5 * time.Minute},
		Access:  Access{UserHeader: "X-Forwarded-User", GroupsHeader: "X-Forwarded-Groups", PrivateGroups: []string{}},
		Auth: Auth{SessionTTL: 12 * time.Hour, AdminEmails: []string{}, AdminGroups: []string{},
			OIDC: OIDC{Scopes: []string{"openid", "email", "profile"}, GroupsClaim: "groups"}},
		Cluster: Cluster{Addresses: []string{}, Name: "dev", Namespace: "scp"},
		Web:     Web{Node: "node"},
	}
	if diff := cmp.Diff(want, cfg, cmp.Comparer(func(a, b Secret) bool { return string(a.v) == string(b.v) })); diff != "" {
		t.Errorf("defaults (-want +got):\n%s", diff)
	}
}

func TestPrecedence(t *testing.T) {
	path := file(t, "server:\n  public_addr: \":8000\"\n  admin_addr: \":8001\"\nlog:\n  level: warn\n")
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
	group := func(match string) string {
		return "catalog:\n  groups:\n    - id: core\n      name: Core\n      default_repo: x\n      match: " + match + "\n"
	}
	tests := map[string]struct {
		file string // replaces the minimal configuration when it starts with "!"
		env  map[string]string
		want string // part of one problem
	}{
		"unknown file key":       {file: "server:\n  public_adr: \":1\"\n", want: "public_adr"},
		"unknown section":        {file: "sever:\n  x: 1\n", want: "sever"},
		"unknown source key":     {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN, colour: red}\n", want: "colour"},
		"wrong type":             {file: "server:\n  shutdown_grace: soon\n", want: "shutdown_grace"},
		"bad yaml":               {file: "server: [\n", want: "yaml"},
		"unknown env":            {env: map[string]string{"SCP_SERVER_PUBLIC_ADR": ":1"}, want: "SCP_SERVER_PUBLIC_ADR"},
		"bad level":              {env: map[string]string{"SCP_LOG_LEVEL": "loud"}, want: "log.level"},
		"bad address":            {env: map[string]string{"SCP_SERVER_PUBLIC_ADDR": "9090"}, want: "not host:port"},
		"same addresses":         {env: map[string]string{"SCP_SERVER_ADMIN_ADDR": ":9090"}, want: "must differ"},
		"non-positive grace":     {env: map[string]string{"SCP_SERVER_SHUTDOWN_GRACE": "0s"}, want: "must be positive"},
		"unknown log format":     {env: map[string]string{"SCP_LOG_FORMAT": "xml"}, want: "log.format"},
		"no sources":             {file: "!log:\n  level: info\n", want: "at least one source"},
		"app and token":          {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN, app: {id: 1, private_key_file: /y}}\n", want: "not both"},
		"neither app nor token":  {file: "!github:\n  sources:\n    - {id: gh, orgs: [a]}\n", want: "set app or token_file"},
		"missing token file":     {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: /does/not/exist}\n", want: "token_file"},
		"base url without slash": {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN, base_url: \"https://ghe.example/api/v3\"}\n", want: "ending in /"},
		"org in two sources":     {file: "!github:\n  sources:\n    - {id: a, orgs: [Cantara], token_file: TOKEN}\n    - {id: b, orgs: [cantara], token_file: TOKEN}\n", want: "belongs to one source"},
		"reconcile too often":    {env: map[string]string{"SCP_CATALOG_RECONCILE_INTERVAL": "10s"}, want: "reconcile_interval"},
		"two matcher kinds":      {file: group("[{prefix: a, glob: 'b*'}]"), want: "exactly one of"},
		"no matcher kind":        {file: group("[{catch_all: false}]"), want: "exactly one of"},
		"unknown matcher":        {file: group("[{suffix: a}]"), want: "suffix"},
		"bare matcher":           {file: group("[prefix]"), want: "match"},
		"bad group id":           {file: "catalog:\n  groups:\n    - {id: Core, name: Core, default_repo: x, match: [{catch_all: true}]}\n", want: "must match"},
		"relative public url":    {env: map[string]string{"SCP_SERVER_PUBLIC_URL": "/portal"}, want: "server.public_url"},
		"public url with query":  {env: map[string]string{"SCP_SERVER_PUBLIC_URL": "https://scp.example/?a=1"}, want: "server.public_url"},
		"bad proxy":              {file: "server:\n  trusted_proxies: [10.0.0.0/33]\n", want: "trusted_proxies"},
		"previous secret alone":  {file: "webhook:\n  previous_secret_file: /x\n", want: "needs webhook.secret_file"},
		"missing secret file":    {file: "webhook:\n  secret_file: /does/not/exist\n", want: "webhook.secret_file"},
		"private without groups": {file: "!server:\n  trusted_proxies: [10.0.0.0/8]\ngithub:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN, include_private: true}\n", want: "access.private_groups: required"},
		"private without proxy":  {file: "!access:\n  private_groups: [staff]\ngithub:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN, include_private: true}\n", want: "server.trusted_proxies: required"},
		"bad user header":        {env: map[string]string{"SCP_ACCESS_USER_HEADER": "X User"}, want: "access.user_header"},
		"same headers":           {env: map[string]string{"SCP_ACCESS_GROUPS_HEADER": "x-forwarded-user"}, want: "must differ"},
		"group with comma":       {file: "access:\n  private_groups: ['a,b']\n", want: "not a group name"},
		"cluster address":        {file: "cluster:\n  addresses: [hazelcast]\n", want: "not host:port"},
		"cluster namespace":      {env: map[string]string{"SCP_CLUSTER_NAMESPACE": "SCP_Prod"}, want: "cluster.namespace"},
		"web dir without server": {env: map[string]string{"SCP_WEB_DIR": "/nonexistent"}, want: "web.dir"},
		"web node empty":         {env: map[string]string{"SCP_WEB_NODE": " "}, want: "web.node"},
		"short session ttl":      {env: map[string]string{"SCP_AUTH_SESSION_TTL": "1m"}, want: "auth.session_ttl"},
		"oidc without key":       {file: "auth:\n  oidc: {issuer: 'https://id.example', client_id: scp}\nserver:\n  public_url: https://scp.example\n", want: "auth.session_key_file: required"},
		"oidc without client":    {env: map[string]string{"SCP_AUTH_OIDC_ISSUER": "https://id.example"}, want: "auth.oidc.client_id"},
		"oidc without url":       {env: map[string]string{"SCP_AUTH_OIDC_ISSUER": "https://id.example"}, want: "server.public_url: required"},
		"oidc over http":         {env: map[string]string{"SCP_AUTH_OIDC_ISSUER": "http://id.example"}, want: "auth.oidc.issuer"},
		"oidc without openid":    {file: "auth:\n  oidc: {issuer: 'https://id.example', scopes: [email]}\n", want: "must include openid"},
		"bad admin email":        {file: "auth:\n  admin_emails: [root]\n", want: "auth.admin_emails"},
		"bad token name":         {file: "auth:\n  tokens: [{name: 'CI bot', secret_file: /x}]\n", want: "auth.tokens[0].name"},
		"token without secret":   {file: "auth:\n  tokens: [{name: ci}]\n", want: "auth.tokens[0].secret_file: required"},
		"short token secret":     {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN}\nauth:\n  tokens: [{name: ci, secret_file: TOKEN}]\n", want: "at least 32 characters"},
		"short session key":      {file: "!github:\n  sources:\n    - {id: gh, orgs: [a], token_file: TOKEN}\nauth:\n  session_key_file: TOKEN\n", want: "at least 32 bytes"},
		"unknown group source":   {file: "catalog:\n  groups:\n    - {id: core, name: Core, default_repo: x, sources: [nope], match: [{catch_all: true}]}\n", want: "unknown source"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			token := write(t, "token", "ghp_test")
			path := file(t, tc.file)
			if body, ok := strings.CutPrefix(tc.file, "!"); ok {
				path = write(t, "scp.yaml", strings.ReplaceAll(body, "TOKEN", token))
			}
			_, _, err := Load(path, nil)
			var inv *Invalid
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *Invalid", err)
			}
			if !strings.Contains(inv.Error(), tc.want) {
				t.Errorf("problems %q do not mention %q", inv.Problems, tc.want)
			}
		})
	}
}

func TestAllProblemsReported(t *testing.T) {
	t.Setenv("SCP_LOG_LEVEL", "loud")
	t.Setenv("SCP_LOG_FORMAT", "xml")
	_, _, err := Load(file(t, ""), nil)
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

func TestWebhookSecrets(t *testing.T) {
	cur, prev := write(t, "cur", "s3cret\n"), write(t, "prev", "old")
	cfg, _, err := Load(file(t, "webhook:\n  secret_file: "+cur+"\n  previous_secret_file: "+prev+"\n"), nil)
	if err != nil || string(cfg.Webhook.Secret.Reveal()) != "s3cret" || string(cfg.Webhook.PreviousSecret.Reveal()) != "old" {
		t.Errorf("webhook = %+v, %v", cfg.Webhook, err)
	}
}

func TestSecretRedacted(t *testing.T) {
	s := NewSecret([]byte("ghp_supersecret"))
	for _, got := range []string{s.String(), s.GoString(), s.LogValue().String()} {
		if strings.Contains(got, "supersecret") {
			t.Errorf("secret leaked: %q", got)
		}
	}
	cfg, settings, err := Load(file(t, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.GitHub.Sources[0].Token.Reveal()) != "ghp_test" {
		t.Errorf("token not read")
	}
	for k, v := range settings {
		if strings.Contains(strings.ToLower(k), "token") && !strings.Contains(k, "file") {
			t.Errorf("settings expose %s = %v", k, v)
		}
	}
}

func TestAuth(t *testing.T) {
	key, secret := write(t, "key", strings.Repeat("k", 32)+"\n"), write(t, "ci", strings.Repeat("s", 40))
	cfg, _, err := Load(write(t, "scp.yaml", "server:\n  public_url: https://scp.example\ngithub:\n  sources:\n    - {id: gh, orgs: [a], token_file: "+
		write(t, "t", "ghp")+", include_private: true}\naccess:\n  private_groups: [staff]\nauth:\n  session_key_file: "+key+
		"\n  admin_emails: [ada@example.org]\n  tokens: [{name: ci, secret_file: "+secret+", groups: [staff]}]\n"+
		"  oidc: {issuer: 'http://127.0.0.1:5556/dex', client_id: scp}\n"), nil) // include_private without a proxy: sign-in identifies
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Auth
	if !a.SignIn() || len(a.SessionKey.Reveal()) != 32 || len(a.Tokens[0].Secret.Reveal()) != 40 || a.OIDC.GroupsClaim != "groups" {
		t.Errorf("auth = %+v", a)
	}
}
