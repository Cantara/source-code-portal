package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/secret"
)

type Rate struct{ N int }

func (r *Rate) UnmarshalText(b []byte) error {
	var n int
	if _, err := fmtSscan(string(b), &n); err != nil {
		return err
	}
	r.N = n
	return nil
}

func fmtSscan(s string, n *int) (int, error) {
	v := 0
	for _, c := range strings.TrimSuffix(s, "/min") {
		if c < '0' || c > '9' {
			return 0, errors.New("bad rate " + s)
		}
		v = v*10 + int(c-'0')
	}
	*n = v
	return 1, nil
}

type Timeouts struct {
	Read   time.Duration `yaml:"read" jsonschema_description:"Read timeout."`
	MaxHdr int64         `yaml:"max_header_bytes" scp:"unit=bytes,min=1KiB,max=1MiB" jsonschema_description:"Header cap."`
}
type Listener struct {
	Listen string `yaml:"listen" jsonschema_description:"Address."`
	Hsts   bool   `yaml:"hsts" jsonschema_description:"HSTS."`
}
type Server struct {
	Public   Listener `yaml:"public" jsonschema_description:"Public listener."`
	Timeouts Timeouts `yaml:"timeouts" jsonschema_description:"Timeouts."`
	Proxies  []string `yaml:"trusted_proxies" jsonschema_description:"CIDRs."`
	Rate     Rate     `yaml:"rate" jsonschema_description:"A rate."`
}

func (s *Server) Validate(report func(path, msg string)) {
	if s.Public.Listen == ":0" {
		report("public.listen", "port 0 is not allowed")
	}
}

type Auth struct {
	Kind  string        `yaml:"kind" jsonschema:"required,enum=app,enum=pat" jsonschema_description:"Strategy."`
	Key   secret.Secret `yaml:"private_key" jsonschema_description:"App key."`
	Level int           `yaml:"level" jsonschema:"minimum=1,maximum=5" jsonschema_description:"Level."`
}
type Source struct {
	ID   string `yaml:"id" jsonschema:"required,pattern=^[a-z0-9-]+$" jsonschema_description:"Id."`
	Auth Auth   `yaml:"auth" jsonschema_description:"Auth."`
}
type GitHub struct {
	Sources []Source          `yaml:"sources" jsonschema_description:"Sources."`
	Labels  map[string]string `yaml:"labels" jsonschema_description:"Labels."`
}
type Upstream struct {
	Concurrency int           `yaml:"concurrency" jsonschema:"minimum=1,maximum=64" jsonschema_description:"Parallel calls."`
	Pace        time.Duration `yaml:"pace" jsonschema_description:"Delay between calls."`
}
type Refresh struct {
	Upstreams map[string]Upstream `yaml:"upstreams" jsonschema_description:"Per-upstream pacing; entries come from the defaults or the file."`
}
type Root struct {
	Version int     `yaml:"version" jsonschema:"required,enum=1" jsonschema_description:"Version."`
	Server  Server  `yaml:"server" jsonschema_description:"Server."`
	GitHub  GitHub  `yaml:"github" jsonschema_description:"GitHub."`
	Refresh Refresh `yaml:"refresh" jsonschema_description:"Refresh."`
}

const defaults = `
version: 1
server:
  public: {listen: ":9090", hsts: false}
  timeouts: {read: 30s, max_header_bytes: 16KiB}
  trusted_proxies: []
  rate: 120/min
github:
  sources: []
  labels: {}
refresh:
  upstreams:
    github: {concurrency: 4, pace: 100ms}
    jenkins_ci: {concurrency: 1, pace: 0s}
`

func load(t *testing.T, file string, env []string, set ...string) (Root, config.Result, error) {
	t.Helper()
	var r Root
	o := config.Options{Defaults: []byte(defaults), Environ: env, Set: set, SkipSecrets: true}
	if file != "" {
		o.File = "scp.yaml"
		o.ReadFile = func(string) ([]byte, error) { return []byte(file), nil }
	}
	res, err := config.Load(&r, o)
	return r, res, err
}

func TestDefaultsOnly(t *testing.T) {
	r, _, err := load(t, "", nil)
	if err != nil || r.Server.Public.Listen != ":9090" || r.Server.Timeouts.Read != 30*time.Second || r.Server.Timeouts.MaxHdr != 16<<10 || r.Server.Rate.N != 120 {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestPrecedence(t *testing.T) {
	file := "server:\n  public: {listen: \":8080\"}\n  timeouts: {read: 10s}\n"
	r, res, err := load(t, file, []string{"SCP_SERVER_PUBLIC_LISTEN=:7070", "SCP_SERVER_TIMEOUTS_MAX_HEADER_BYTES=32KiB", "SCP_SERVER_TRUSTED_PROXIES=10.0.0.0/8, 192.168.0.0/16", "SCP_CONFIG=/x", "SCP_SERVICE_HOST=1.2.3.4", "SCP_PORT_9090_TCP=tcp://x", "SCP_SERVER_PUBLIC_LISTN=:1"}, "server.timeouts.read=2m")
	if err != nil {
		t.Fatal(err)
	}
	if r.Server.Public.Listen != ":7070" || r.Server.Timeouts.Read != 2*time.Minute || r.Server.Timeouts.MaxHdr != 32<<10 || len(r.Server.Proxies) != 2 {
		t.Fatalf("%+v", r.Server)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "SCP_SERVER_PUBLIC_LISTN") || !strings.Contains(res.Warnings[0], "SCP_SERVER_PUBLIC_LISTEN") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	layers := map[string]config.Layer{}
	for _, a := range res.Applied {
		layers[a.Path] = a.Layer
	}
	if layers["server.public.listen"] != config.LayerEnv || layers["server.timeouts.read"] != config.LayerFlag {
		t.Fatalf("%v", layers)
	}
}

func TestAllViolationsWithLinesAndHints(t *testing.T) {
	file := "version: 1\nserver:\n  public: {listn: \":1\", hsts: maybe}\n  timeouts: {read: 5, max_header_bytes: 10}\ngithub:\n  sources:\n    - id: Bad_ID\n      auth: {kind: pat, level: 9, private_key: inline}\n"
	_, _, err := load(t, file, nil)
	var ce *config.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%v", err)
	}
	text := err.Error()
	for _, want := range []string{"scp.yaml:3: server.public.listn: unknown key \"listn\" (did you mean \"listen\"?)", "server.public.hsts", "server.timeouts.read", "server.timeouts.max_header_bytes", "github.sources[0].id", "github.sources[0].auth.level", "github.sources[0].auth.private_key"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if len(ce.Violations) < 7 {
		t.Fatalf("expected all problems at once, got %d:\n%s", len(ce.Violations), text)
	}
}

func TestListFromEnvAndGap(t *testing.T) {
	r, _, err := load(t, "", []string{"SCP_GITHUB_SOURCES_0_ID=acme", "SCP_GITHUB_SOURCES_0_AUTH_KIND=pat", "SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY=ghp_literalvalue", "SCP_GITHUB_SOURCES_1_ID=beta", "SCP_GITHUB_SOURCES_1_AUTH_KIND=app", "SCP_GITHUB_SOURCES_1_AUTH_PRIVATE_KEY_FILE=/run/secrets/k"})
	if err != nil || len(r.GitHub.Sources) != 2 || r.GitHub.Sources[0].Auth.Key.Reveal() != "ghp_literalvalue" || !r.GitHub.Sources[1].Auth.Key.Pending() {
		t.Fatalf("%v %+v", err, r.GitHub)
	}
	_, _, err = load(t, "", []string{"SCP_GITHUB_SOURCES_2_ID=gap", "SCP_GITHUB_SOURCES_2_AUTH_KIND=pat"})
	if err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("want gap error, got %v", err)
	}
	_, _, err = load(t, "", []string{"SCP_GITHUB_SOURCES_0_ID=a", "SCP_GITHUB_SOURCES_0_AUTH_KIND=pat", "SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY=x", "SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY_FILE=/y"})
	if err == nil || !strings.Contains(err.Error(), "use one") {
		t.Fatalf("both forms: %v", err)
	}
}

func TestSecretFileResolveAndFlagRefusal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	os.WriteFile(p, []byte("pem-bytes\n"), 0o600)
	var r Root
	_, err := config.Load(&r, config.Options{Defaults: []byte(defaults), File: "f", ReadFile: func(string) ([]byte, error) {
		return []byte("github:\n  sources:\n    - id: a\n      auth: {kind: app, private_key: {file: " + p + "}}\n"), nil
	}})
	if err != nil || r.GitHub.Sources[0].Auth.Key.Reveal() != "pem-bytes" {
		t.Fatalf("%v", err)
	}
	_, err = config.Load(&r, config.Options{Defaults: []byte(defaults), Set: []string{"github.sources[0].auth.private_key=x"}})
	if err == nil || !strings.Contains(err.Error(), "secrets cannot be set from a flag") {
		t.Fatalf("%v", err)
	}
	_, err = config.Load(&r, config.Options{Defaults: []byte(defaults), File: "f", ReadFile: func(string) ([]byte, error) {
		return []byte("github:\n  sources:\n    - id: a\n      auth: {kind: app, private_key: {file: /nonexistent/k}}\n"), nil
	}})
	if err == nil || !strings.Contains(err.Error(), "github.sources[0].auth.private_key") {
		t.Fatalf("missing file must fail at load: %v", err)
	}
}

func TestCrossFieldValidatorPaths(t *testing.T) {
	_, _, err := load(t, "server:\n  public: {listen: \":0\"}\n", nil)
	if err == nil || !strings.Contains(err.Error(), "server.public.listen: port 0 is not allowed") {
		t.Fatalf("%v", err)
	}
}

func TestSchemaAndReferenceAreDeterministicAndDocumented(t *testing.T) {
	a, err := config.Schema(&Root{}, []byte(defaults))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := config.Schema(&Root{}, []byte(defaults))
	if string(a) != string(b) {
		t.Fatal("schema not deterministic")
	}
	for _, want := range []string{`"x-scp-env": "SCP_SERVER_PUBLIC_LISTEN"`, `"x-scp-env": "SCP_GITHUB_SOURCES_{i}_AUTH_PRIVATE_KEY"`, `"default": ":9090"`, `"pattern": "^[0-9]+(B|KiB|MiB|GiB)?$"`} {
		if !strings.Contains(string(a), want) {
			t.Errorf("schema lacks %s", want)
		}
	}
	ref, err := config.Reference(&Root{}, []byte(defaults))
	if err != nil || !strings.Contains(ref, "`SCP_GITHUB_SOURCES_{i}_AUTH_PRIVATE_KEY` or `SCP_GITHUB_SOURCES_{i}_AUTH_PRIVATE_KEY_FILE`") {
		t.Fatalf("%v\n%s", err, ref)
	}
}

type Bad struct {
	A string `yaml:"a"`
	B string `yaml:"BadName" jsonschema_description:"x"`
	C struct {
		D string `yaml:"d" jsonschema_description:"d"`
	} `yaml:"c" jsonschema_description:"c"`
	E string `yaml:"c_d" jsonschema_description:"collides with c.d"`
}
type BadRoot struct {
	Bad  Bad `yaml:"bad" jsonschema_description:"b"`
	Bad2 struct {
		X string `yaml:"x" jsonschema_description:"x"`
	} `yaml:"bad_c" jsonschema_description:"c"`
}

func TestDescribeRejectsUndocumentedAndColliding(t *testing.T) {
	_, err := config.Describe(&BadRoot{})
	if err == nil || !strings.Contains(err.Error(), "no jsonschema_description") || !strings.Contains(err.Error(), "snake_case") {
		t.Fatalf("%v", err)
	}
	type R2 struct {
		A struct {
			B string `yaml:"b_c" jsonschema_description:"x"`
		} `yaml:"a" jsonschema_description:"a"`
		AB struct {
			C string `yaml:"c" jsonschema_description:"x"`
		} `yaml:"a_b" jsonschema_description:"a"`
	}
	_, err = config.Describe(&R2{})
	if err == nil || !strings.Contains(err.Error(), "environment name SCP_A_B_C is used by") {
		t.Fatalf("collision not detected: %v", err)
	}
}

func TestMapEntryEnv(t *testing.T) {
	r, res, err := load(t, "", []string{"SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY=8", "SCP_REFRESH_UPSTREAMS_JENKINS_CI_PACE=2s", "SCP_REFRESH_UPSTREAMS_NEW_CONCURRENCY=3"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Refresh.Upstreams["github"].Concurrency != 8 || r.Refresh.Upstreams["jenkins_ci"].Pace != 2*time.Second {
		t.Fatalf("%+v", r.Refresh.Upstreams)
	}
	if _, ok := r.Refresh.Upstreams["new"]; ok {
		t.Fatal("env created a map entry")
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "SCP_REFRESH_UPSTREAMS_NEW_CONCURRENCY") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
}

// Two owners, one YAML mapping: github.sources[].discovery is declared half by the github adapter and half by catalog.
type DiscoveryGH struct {
	Mode string `yaml:"mode" jsonschema:"enum=installation,enum=org" jsonschema_description:"How repositories are discovered."`
}
type DiscoveryCat struct {
	IncludeArchived bool `yaml:"include_archived" jsonschema_description:"List archived repositories."`
}

func (d DiscoveryCat) Validate(report func(path, msg string)) {
	if d.IncludeArchived {
		report("include_archived", "archived repositories are not supported in this test")
	}
}

type Discovery struct {
	DiscoveryGH  `yaml:",inline"`
	DiscoveryCat `yaml:",inline"`
}
type InlineRoot struct {
	Sources []struct {
		ID        string    `yaml:"id" jsonschema:"required" jsonschema_description:"Id."`
		Discovery Discovery `yaml:"discovery" jsonschema_description:"Discovery."`
	} `yaml:"sources" jsonschema_description:"Sources."`
}

func TestInlineTwoOwners(t *testing.T) {
	var r InlineRoot
	res, err := config.Load(&r, config.Options{
		Defaults: []byte("sources: []\n"),
		File:     "f.yaml",
		ReadFile: func(string) ([]byte, error) {
			return []byte("sources:\n  - id: a\n    discovery: {mode: org, include_archived: true}\n"), nil
		},
		Environ: []string{"SCP_SOURCES_0_DISCOVERY_MODE=installation"},
	})
	var ce *config.Error
	if !errors.As(err, &ce) || len(ce.Violations) != 1 || ce.Violations[0].Path != "sources[0].discovery.include_archived" {
		t.Fatalf("the embedded validator must run at the inline path: %v", err)
	}
	_ = res
	root, err := config.Describe(&InlineRoot{})
	if err != nil {
		t.Fatal(err)
	}
	m := root.Child("sources").Elem.Child("discovery").Child("mode")
	c := root.Child("sources").Elem.Child("discovery").Child("include_archived")
	if m == nil || c == nil || m.Index[0] == c.Index[0] || !strings.HasSuffix(m.Env, "SOURCES_{i}_DISCOVERY_MODE") {
		t.Fatalf("%+v %+v", m, c)
	}
	sch, err := config.Schema(&InlineRoot{}, []byte("sources: []\n"))
	if err != nil || !strings.Contains(string(sch), `"include_archived"`) || !strings.Contains(string(sch), `"mode"`) || strings.Contains(string(sch), "DiscoveryGH") {
		t.Fatalf("%v\n%s", err, sch)
	}
}

func TestOverlayAddsConditionalRule(t *testing.T) {
	overlay := config.Overlay{Path: "github.sources[].auth", Schema: []byte(`{
	  "type": "object", "additionalProperties": false,
	  "properties": {"kind": {"enum": ["app","pat"]}, "private_key": {"type":"object"}, "level": {"type":"integer"}},
	  "required": ["kind"],
	  "if": {"properties": {"kind": {"const": "app"}}}, "then": {"required": ["private_key"]}
	}`)}
	var r Root
	_, err := config.Load(&r, config.Options{
		Defaults: []byte(defaults), SkipSecrets: true, Overlays: []config.Overlay{overlay},
		File: "f.yaml", ReadFile: func(string) ([]byte, error) {
			return []byte("github:\n  sources:\n    - id: a\n      auth: {kind: app}\n"), nil
		},
	})
	var ce *config.Error
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "private_key") || !strings.Contains(err.Error(), "github.sources[0].auth") {
		t.Fatalf("overlay rule not enforced: %v", err)
	}
	sch, err := config.Schema(&Root{}, []byte(defaults), overlay)
	if err != nil || !strings.Contains(string(sch), `"then"`) || !strings.Contains(string(sch), "x-scp-owner") {
		t.Fatalf("%v", err)
	}
	if _, err := config.Schema(&Root{}, []byte(defaults), config.Overlay{Path: "github.nope", Schema: []byte(`{}`)}); err == nil {
		t.Fatal("overlay on a missing key accepted")
	}
}

func TestFileSizeLimit(t *testing.T) {
	var r Root
	_, err := config.Load(&r, config.Options{Defaults: []byte(defaults), File: "big.yaml", ReadFile: func(string) ([]byte, error) {
		return []byte("# " + strings.Repeat("x", config.MaxFileBytes)), nil
	}})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("%v", err)
	}
}

// A feature's own size type (05-github-adapter declares ByteSize with UnmarshalText) works unchanged, from a string or a bare integer.
type ByteSize int64

func (b *ByteSize) UnmarshalText(t []byte) error {
	n, err := config.ParseBytes(string(t))
	*b = ByteSize(n)
	return err
}

func TestTextLeafTakesIntegerAndString(t *testing.T) {
	type S struct {
		A ByteSize `yaml:"a" jsonschema_description:"a"`
		B ByteSize `yaml:"b" jsonschema_description:"b"`
		C ByteSize `yaml:"c" jsonschema_description:"c"`
	}
	var s S
	_, err := config.Load(&s, config.Options{Defaults: []byte("a: 512KiB\nb: 4096\nc: 1MiB\n"), Environ: []string{"SCP_C=2048"}})
	if err != nil || s.A != 512<<10 || s.B != 4096 || s.C != 2048 {
		t.Fatalf("%v %+v", err, s)
	}
	_, err = config.Load(&s, config.Options{Defaults: []byte("a: [1]\nb: 1\nc: 1\n")})
	if err == nil {
		t.Fatal("a list for a text leaf accepted")
	}
}

// The three accepted Validate shapes (units 03, 05 and 14 were written against `error`-returning forms).
type vErr struct{ Path, Msg string }

func (e *vErr) Error() string      { return e.Path + ": " + e.Msg }
func (e *vErr) ConfigPath() string { return e.Path }

type ShapeB struct {
	N int `yaml:"n" jsonschema_description:"n"`
}

func (s ShapeB) Validate() error {
	return errors.Join(&vErr{"n", "must be even"}, &vErr{"extra", "second problem"}, errors.New("plain problem"))
}

type ShapeC struct {
	M int `yaml:"m" jsonschema_description:"m"`
}

func (s *ShapeC) Validate(prefix string) error {
	return errors.Join(errors.New(prefix+".m: must be positive"), errors.New("not about a key"))
}

type ShapeBad struct {
	K int `yaml:"k" jsonschema_description:"k"`
}

func (s ShapeBad) Validate(ctx context.Context) error { return nil }

func TestValidateShapes(t *testing.T) {
	type R struct {
		B ShapeB `yaml:"b" jsonschema_description:"b"`
		C ShapeC `yaml:"c" jsonschema_description:"c"`
	}
	var r R
	_, err := config.Load(&r, config.Options{Defaults: []byte("b: {n: 1}\nc: {m: 0}\n")})
	var ce *config.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%v", err)
	}
	got := map[string]string{}
	for _, v := range ce.Violations {
		got[v.Path+"|"+v.Message] = ""
	}
	for _, want := range []string{"b.n|must be even", "b.extra|second problem", "b|plain problem", "c.m|must be positive", "c|not about a key"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	type Bad struct {
		X ShapeBad `yaml:"x" jsonschema_description:"x"`
	}
	if _, err := config.Describe(&Bad{}); err == nil || !strings.Contains(err.Error(), "Validate has the signature") {
		t.Fatalf("an unsupported Validate shape must be refused, got %v", err)
	}
}

func TestDescribeRejectsFileTwinCollision(t *testing.T) {
	type bad struct {
		Token     secret.Secret `yaml:"token" jsonschema_description:"d"`
		TokenFile string        `yaml:"token_file" jsonschema_description:"d"`
	}
	type root struct {
		S bad `yaml:"s" jsonschema_description:"d"`
	}
	if _, err := config.Describe(&root{}); err == nil || !strings.Contains(err.Error(), "SCP_S_TOKEN_FILE") {
		t.Fatalf("the _FILE twin of a secret must be reserved: %v", err)
	}
}
