package people

import (
	"errors"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestBound(t *testing.T) {
	for _, tc := range []struct {
		rule, identity, login string
		want                  bool
	}{
		{Managed, "ann.lee@example.org", "ann-lee_octo", true},
		{Managed, "Ann.Lee@example.org", "ann-lee_OCTO", true},
		{Managed, "ann.lee@example.org", "ann-lee", false},     // no short code: not a managed user's login
		{Managed, "ann.lee@example.org", "ann-lee_", false},    // an empty short code
		{Managed, "ann.lee@example.org", "bo-lee_octo", false}, // someone else
		{Managed, "ann.lee@example.org", "ann-lee-x_octo", false},
		{Managed, "ann_lee@example.org", "ann-lee_octo", true}, // _ is not kept in a managed login
		{Managed, "", "ann_octo", false},
		{Exact, "ann", "ANN", true},
		{Exact, "ann@example.org", "ann", false},
	} {
		if got := Bound(tc.rule, tc.identity, tc.login); got != tc.want {
			t.Errorf("Bound(%s, %q, %q) = %v", tc.rule, tc.identity, tc.login, got)
		}
	}
}

func TestCreationLink(t *testing.T) {
	u, err := url.Parse(CreationLink("https://octocorp.ghe.com/", "octocorp", "for the portal"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "octocorp.ghe.com" || u.Path != "/settings/personal-access-tokens/new" || q.Get("target_name") != "octocorp" ||
		q.Get("expires_in") != "90" || q.Get("contents") != "read" || q.Get("statuses") != "read" || q.Get("pull_requests") != "read" ||
		q.Get("description") != "for the portal" || len(q) != 4+len(Permissions) {
		t.Errorf("link %s", u)
	}
}

func TestSealing(t *testing.T) {
	s, err := newSealer([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.seal("p1", "ghe", "acme", []byte("github_pat_x"))
	if strings.Contains(string(sealed), "github_pat_x") {
		t.Fatal("the token is in the sealed bytes")
	}
	if got, err := s.open("p1", "ghe", "acme", sealed); err != nil || string(got) != "github_pat_x" {
		t.Errorf("open: %q %v", got, err)
	}
	if _, err := s.open("p2", "ghe", "acme", sealed); err == nil {
		t.Error("another person's row opened the token")
	}
	other, _ := newSealer([]byte(strings.Repeat("j", 32)))
	if _, err := other.open("p1", "ghe", "acme", sealed); err == nil {
		t.Error("another key opened the token")
	}
	if _, err := newSealer([]byte("short")); err == nil {
		t.Error("a short key was taken")
	}
}

type fixture struct {
	gh   *githubtest.Server
	db   *store.DB
	path string
	key  []byte
	src  Source
	now  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	srv := githubtest.New(t)
	inner := srv.AddRepo("acme", gh.Repository{Name: new("inner"), Visibility: new("internal")})
	srv.AddRepo("other", gh.Repository{Name: new("x"), Visibility: new("private")})
	for tok, login := range map[string]string{"github_pat_ann": "ann-lee_octo", "github_pat_bo": "bo_octo", "ghp_classic": "ann-lee_octo",
		"github_pat_old": "ann-lee_octo"} {
		srv.AddUserToken(tok, login)
		srv.Reach(tok, inner)
	}
	now := time.Now()
	srv.Expires("github_pat_ann", now.Add(90*24*time.Hour).Truncate(time.Second))
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "ghe", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, People: true}}},
		nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scp.db")
	db, err := store.OpenAs(t.Context(), path, store.Enterprise, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &fixture{gh: srv, db: db, path: path, key: []byte(strings.Repeat("k", 32)), now: now,
		src: Source{ID: "ghe", WebURL: "https://ghe.example.test/", Owners: []string{"Acme"}, Login: Managed, Check: c.Source("ghe").CheckToken}}
}

func (f *fixture) open(t *testing.T) *People {
	t.Helper()
	p, err := Open(t.Context(), Options{Store: f.db, Key: f.key, Issuer: "https://idp.example.test", Sources: []Source{f.src},
		Portal: "https://scp.example", Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var ann = Who{Subject: "u-1", Identity: "ann.lee@example.org", Name: "Ann"} //nolint:gochecknoglobals // test value

// A person links their own fine-grained token, which is kept sealed and survives a restart; every other kind of token is refused
// with its reason; removing it leaves nothing.
func TestLink(t *testing.T) {
	f := newFixture(t)
	p := f.open(t)
	ctx := t.Context()
	for _, tc := range []struct {
		owner, token, code string
	}{
		{"elsewhere", "github_pat_ann", "unknown-owner"},
		{"acme", "ghp_classic", "not-fine-grained"},
		{"acme", "github_pat_ann with spaces", "not-fine-grained"},
		{"acme", "github_pat_bo", "not-yours"},
		{"acme", "github_pat_unknown", "unauthorized"},
	} {
		var r *Refusal
		if _, err := p.Link(ctx, ann, "ghe", tc.owner, tc.token); !errors.As(err, &r) || r.Code != tc.code || r.Detail == "" {
			t.Errorf("%s %s: %v, want %s", tc.owner, tc.token, err, tc.code)
		}
	}
	f.gh.Expires("github_pat_old", f.now.Add(-time.Minute))
	var r *Refusal
	if _, err := p.Link(ctx, ann, "ghe", "acme", "github_pat_old"); !errors.As(err, &r) || r.Code != "unauthorized" {
		t.Errorf("an expired token: %v", err)
	}
	if got := p.Tokens(ann); len(got) != 0 {
		t.Fatalf("a refused token was kept: %+v", got)
	}

	tok, err := p.Link(ctx, ann, "ghe", "ACME", " github_pat_ann\n")
	if err != nil || tok.Login != "ann-lee_octo" || tok.Owner != "acme" || tok.State != "ok" || tok.ExpiresAt == nil {
		t.Fatalf("link: %+v %v", tok, err)
	}
	_, tokens, _, _ := f.db.People(ctx)
	if len(tokens) != 1 || strings.Contains(string(tokens[0].Sealed), "github_pat_ann") {
		t.Fatalf("stored %+v", tokens)
	}

	again := f.open(t) // a restart
	if got := again.Tokens(ann); len(got) != 1 || got[0].Login != "ann-lee_octo" || !got[0].ExpiresAt.Equal(*tok.ExpiresAt) {
		t.Errorf("after a restart: %+v", got)
	}
	if got, err := again.sealer.open(again.id(ann.Subject), "ghe", "acme", tokens[0].Sealed); err != nil || string(got) != "github_pat_ann" {
		t.Errorf("the sealed token: %q %v", got, err)
	}
	ov := again.Overview(ann)
	if len(ov) != 1 || len(ov[0].Owners) != 1 || ov[0].Owners[0].Token == nil || !strings.Contains(ov[0].Owners[0].CreateURL, "target_name=Acme") {
		t.Errorf("overview %+v", ov)
	}
	if ov := again.Overview(Who{Subject: "u-2"}); ov[0].Owners[0].Token != nil {
		t.Error("another person sees Ann's token")
	}
	if err := again.Unlink(ctx, ann, "ghe", "Acme"); err != nil {
		t.Fatal(err)
	}
	if got := f.open(t).Tokens(ann); len(got) != 0 {
		t.Errorf("after removal: %+v", got)
	}
	for _, l := range f.gh.Ledger() {
		if !strings.HasPrefix(l, "GET ") {
			t.Errorf("a request that is not a read: %s", l)
		}
	}
}
