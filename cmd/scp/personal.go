package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cantara/source-code-portal/internal/auth"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/ghlogin"
	"github.com/cantara/source-code-portal/internal/setup"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/vault"
)

// The personal edition (design/personal.md): one directory holding the store, personal.yaml and the local secrets; a terminal
// wizard that signs the person in to GitHub; the credential sealed in the store; a portal on loopback that refuses requests without
// a session and signs the owner in through a one-use link.
const (
	personalSource     = "github" // the source's id and the credential's
	personalOwner      = "owner"  // the token and the group of the person who started the portal
	personalLaunchTTL  = 10 * time.Minute
	personalDefaultURL = "github.com"
)

type personalFlags struct {
	dir, host, clientID, scope, webDir string
	port                               int
	reset                              bool
	terminal                           bool // ask the questions in the terminal, not in the browser
	noBrowser                          bool // print the setup link and do not open it
}

func newPersonalCmd(opts *options) *cobra.Command {
	var f personalFlags
	cmd := &cobra.Command{
		Use:   "personal",
		Short: "Run a portal of your own on this computer: sign in to GitHub once and see every repository you can read",
		Long: "Signs you in to GitHub (github.com, a GitHub Enterprise Server or a GHE.com tenant), keeps the credential sealed in a store in\n" +
			"one directory, writes personal.yaml there for you to edit, and serves the portal on this computer only. The first run asks\n" +
			"a few questions; the next runs start at once and print a link that signs you in to the portal.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := &personal{flags: f, in: bufio.NewReader(cmd.InOrStdin()), out: cmd.OutOrStdout(), tty: cmd.InOrStdin()}
			return p.run(cmd, opts, deps{})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.dir, "dir", "", "the directory of this portal's files (default: scp in your user configuration directory)")
	fl.StringVar(&f.host, "host", "", "the GitHub host: github.com, https://ghe.example.org (Enterprise Server) or https://name.ghe.com")
	fl.StringVar(&f.clientID, "client-id", "", "the client id of an OAuth app or GitHub App you registered on the host, to sign in with a code (else paste a token)")
	fl.StringVar(&f.scope, "scope", "repo read:org", "the OAuth scopes asked with the code (ignored for a GitHub App)")
	fl.StringVar(&f.webDir, "web-dir", "", "the web UI bundle (default: web beside this program, or web/bundle here)")
	fl.IntVar(&f.port, "port", 9090, "the port of the portal on this computer (the admin port is the next)")
	fl.BoolVar(&f.terminal, "terminal", false, "set up with questions in the terminal instead of the page in the browser")
	fl.BoolVar(&f.noBrowser, "no-browser", false, "print the link of the setup page and do not open it")
	fl.BoolVar(&f.reset, "reset", false, "set up again and write personal.yaml anew (the old one is kept as personal.yaml.bak)")
	return cmd
}

// personal is one run of the command. in, out and the hooks are replaced in tests.
type personal struct {
	flags    personalFlags
	in       *bufio.Reader
	out      io.Writer
	tty      io.Reader // the terminal, when stdin is one: hidden input
	ring     vault.Ring
	http     *http.Client
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
	open     func(url string)                                           // opens the setup page in the browser; nil: the system's
	cliToken func(ctx context.Context, hostname string) (string, error) // the GitHub CLI's token; nil: ask `gh` if it is installed
}

func (p *personal) say(format string, a ...any) { _, _ = fmt.Fprintf(p.out, format+"\n", a...) }

func (p *personal) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *personal) ask(label, def string) (string, error) {
	if def != "" {
		label += " [" + def + "]"
	}
	_, _ = fmt.Fprint(p.out, label+": ")
	line, err := p.in.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", errors.New("no answer: scp personal asks its questions in a terminal")
	}
	if line = strings.TrimSpace(line); line != "" {
		return line, nil
	}
	return def, nil
}

// askSecret reads a line without echoing it when the input is a terminal.
func (p *personal) askSecret(label string) (string, error) {
	_, _ = fmt.Fprint(p.out, label+": ")
	if f, ok := p.tty.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(p.out)
		return strings.TrimSpace(string(b)), err
	}
	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no answer: scp personal asks its questions in a terminal")
	}
	return strings.TrimSpace(line), nil
}

func (p *personal) dir() (string, error) {
	d := p.flags.dir
	if d == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("no user configuration directory: name one with --dir: %w", err)
		}
		d = filepath.Join(base, "scp")
	}
	d, err := filepath.Abs(d)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, os.Chmod(d, 0o700) //nolint:gosec // G302: a private directory is 0700
}

func (p *personal) client(h ghlogin.Host) *ghlogin.Client {
	return &ghlogin.Client{Host: h, HTTP: p.http, Sleep: p.sleep, Now: p.now}
}

// run sets the directory up (the wizard on the first run), loads personal.yaml, opens the credential and serves.
func (p *personal) run(cmd *cobra.Command, opts *options, d deps) error {
	ctx := cmd.Context()
	dir, err := p.dir()
	if err != nil {
		return failure{err}
	}
	var launch *auth.Launch
	cfgPath := filepath.Join(dir, "personal.yaml")
	if _, statErr := os.Stat(cfgPath); !p.flags.terminal && (p.flags.reset || errors.Is(statErr, os.ErrNotExist)) {
		if launch, err = p.browserSetup(ctx, dir, cfgPath); err != nil {
			return failure{err}
		}
	} else if cfgPath, err = p.setup(ctx, dir); err != nil {
		return failure{err}
	}
	opts.configPath = cfgPath
	cfg, _, err := loadConfig(cmd, opts)
	if err != nil {
		return err
	}
	if err := requireLoopback(cfg); err != nil {
		return err
	}
	if err := p.resolve(ctx, dir, &cfg); err != nil {
		return failure{err}
	}
	if d.launch = launch; d.launch == nil {
		if d.launch, err = newLaunch(p.clock()); err != nil {
			return failure{err}
		}
	}
	srv, err := newServer(ctx, cfg, d, newLogger(cfg.Log, cmd.ErrOrStderr()))
	if err != nil {
		return failure{fmt.Errorf("%w (to use other ports, change server.public_addr and server.public_url in %s)", err, cfgPath)}
	}
	p.say("\nYour portal is running. Open this link once to sign in (it works for %d minutes and one time):\n\n  %s\n\nPress Ctrl-C to stop.",
		int(personalLaunchTTL.Minutes()), launchLink(srv.publicLn.Addr(), d.launch.Code))
	if err := srv.run(ctx); err != nil {
		return failure{err}
	}
	return nil
}

func newLaunch(now time.Time) (*auth.Launch, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &auth.Launch{Code: base64.RawURLEncoding.EncodeToString(b), Token: personalOwner, Expires: now.Add(personalLaunchTTL)}, nil
}

func launchLink(addr net.Addr, code string) string {
	return "http://" + addr.String() + "/auth/launch?code=" + code
}

// requireLoopback refuses a configuration that would serve beyond this computer: the personal edition has one person and no proxy.
func requireLoopback(cfg config.Config) error {
	for key, addr := range map[string]string{"server.public_addr": cfg.Server.PublicAddr, "server.admin_addr": cfg.Server.AdminAddr} {
		host, _, err := net.SplitHostPort(addr)
		if a, perr := netip.ParseAddr(host); err != nil || host != "localhost" && (perr != nil || !a.IsLoopback()) {
			return fmt.Errorf("%s: %q is not a loopback address; scp personal serves this computer only (use scp serve for a shared portal)", key, addr)
		}
	}
	return nil
}

// setup makes the directory ready: the wizard where there is no personal.yaml (or --reset), a new sign-in where the credential has
// ended, and the local secrets. It returns the path of personal.yaml.
func (p *personal) setup(ctx context.Context, dir string) (string, error) {
	cfgPath := filepath.Join(dir, "personal.yaml")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: a private directory is 0700
		return "", err
	}
	st, err := store.Open(ctx, filepath.Join(dir, "scp.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		return "", err
	}
	defer st.Close()
	if err := os.Chmod(filepath.Join(dir, "scp.db"), 0o600); err != nil { // the sealed credential is in it; the directory is private too
		return "", err
	}
	key, where, err := vault.OpenKey(dir, p.keyRing(dir))
	if err != nil {
		return "", err
	}
	v, err := vault.New(key, st)
	if err != nil {
		return "", err
	}
	_, statErr := os.Stat(cfgPath)
	switch {
	case p.flags.reset || errors.Is(statErr, os.ErrNotExist):
		if err := p.wizard(ctx, dir, cfgPath, v, where); err != nil {
			return "", err
		}
	case statErr != nil:
		return "", statErr
	default:
		sec, err := v.Get(ctx, personalSource)
		switch {
		case err == nil && !sec.Expired(p.clock()):
		case err == nil:
			p.say("Your sign-in to GitHub ended on %s. Signing in again.", sec.ExpiresAt.Local().Format("2006-01-02 15:04"))
			fallthrough
		default:
			if err != nil && !errors.Is(err, store.ErrNoCredential) {
				p.say("The stored credential cannot be opened (%v). Signing in again.", err)
			} else if err != nil {
				p.say("There is no stored credential. Signing in.")
			}
			if _, err := p.signIn(ctx, v, sec.Source); err != nil {
				return "", err
			}
		}
	}
	return cfgPath, p.secrets(dir)
}

func (p *personal) keyRing(dir string) vault.Ring {
	if p.ring != nil {
		return p.ring
	}
	return vault.Keychain(dir)
}

// secrets makes the session key and the owner's token if they are not there: random, 0600, and only read by this program.
func (p *personal) secrets(dir string) error {
	for _, name := range []string{"session.key", "owner.token"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		b := make([]byte, 48)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(b)+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// resolve gives every source that takes its credential from the vault its token.
func (p *personal) resolve(ctx context.Context, dir string, cfg *config.Config) error {
	st, err := store.Open(ctx, cfg.Store.Path, slog.New(slog.DiscardHandler))
	if err != nil {
		return err
	}
	defer st.Close()
	key, _, err := vault.OpenKey(dir, p.keyRing(dir))
	if err != nil {
		return err
	}
	v, err := vault.New(key, st)
	if err != nil {
		return err
	}
	for i := range cfg.GitHub.Sources {
		s := &cfg.GitHub.Sources[i]
		if s.App != nil && s.App.Credential { // an App's private key
			sec, err := v.Get(ctx, s.ID)
			if err != nil {
				return fmt.Errorf("github.sources[%d]: the App key %q: %w; run scp personal --reset", i, s.ID, err)
			}
			s.App.PrivateKey = config.NewSecret([]byte(sec.Value))
			continue
		}
		if !s.Credential {
			continue
		}
		sec, err := v.Get(ctx, s.ID)
		if err != nil {
			return fmt.Errorf("github.sources[%d]: the credential %q: %w; run scp personal --reset", i, s.ID, err)
		}
		if sec.Expired(p.clock()) {
			return fmt.Errorf("github.sources[%d]: the credential %q ended; run scp personal to sign in again", i, s.ID)
		}
		s.Token = config.NewSecret([]byte(sec.Value))
	}
	return nil
}

// wizard asks what personal.yaml needs, signs in and writes the file.
func (p *personal) wizard(ctx context.Context, dir, cfgPath string, v *vault.Vault, where string) error {
	p.say("Welcome. This sets up a portal of your own for the repositories you can read on GitHub.\nIt only reads: it sends GET requests and never writes to GitHub.\n")
	if where == vault.InFile {
		p.say("Note: this computer has no keychain, so the key that seals your credential is the file %s (readable by you only).\nAnyone who can read your files can read the credential: keep this directory private.\n", filepath.Join(dir, "vault.key"))
	}
	signed, err := p.signIn(ctx, v, "")
	if err != nil {
		return err
	}
	c := p.client(signed.host)
	accounts, err := p.chooseAccounts(ctx, c, signed)
	if err != nil {
		return err
	}
	def, _ := c.Newest(ctx, signed.token)
	if def == "" {
		def = ".github"
	}
	webDir := p.flags.webDir
	if webDir == "" {
		webDir = findWebBundle()
	}
	if webDir == "" {
		p.say("\nNote: no web UI bundle was found (web beside this program, or web/bundle here). The API works; name the bundle with --web-dir.")
	}
	if old, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: the person's own file
		if err := os.WriteFile(cfgPath+".bak", old, 0o600); err != nil { //nolint:gosec // G703: cfgPath is the personal directory's own file
			return err
		}
	}
	if err := os.WriteFile(cfgPath, []byte(personalYAML(dir, signed.host, signed.user, accounts, nil, nil, nil, nil, def, webDir, p.flags.port)), 0o600); err != nil {
		return err
	}
	p.say("\nSigned in as %s. Settings are in %s (edit them freely; there is no secret in the file).", signed.user, cfgPath)
	return nil
}

type signedIn struct {
	host  ghlogin.Host
	user  string
	token string
}

// signIn asks for the host and the token (a code on GitHub, or a pasted one), checks it and seals it. host, when not empty, is the web
// address of the host to sign in to (a credential that ended).
func (p *personal) signIn(ctx context.Context, v *vault.Vault, host string) (signedIn, error) {
	if host == "" {
		host = p.flags.host
	}
	if host == "" {
		var err error
		if host, err = p.ask("GitHub host (github.com, https://ghe.example.org or https://name.ghe.com)", personalDefaultURL); err != nil {
			return signedIn{}, err
		}
	}
	h, err := ghlogin.ParseHost(host)
	if err != nil {
		return signedIn{}, err
	}
	c := p.client(h)
	clientID := p.flags.clientID
	if clientID == "" {
		p.say("\nTo sign in with a code you need an OAuth app or GitHub App registered on %s (docs/personal-edition.md says how).\nWithout one, paste a token instead: a fine-grained token with read access, or a classic one with the scopes repo and read:org.", h.Web)
		if clientID, err = p.ask("Client id (Enter to paste a token)", ""); err != nil {
			return signedIn{}, err
		}
	}
	var tok ghlogin.Token
	if clientID != "" {
		p.say("\nSCP only reads. The OAuth scope repo that private repositories need also carries write access that SCP never uses; a GitHub App with read-only permissions is the safer grant.")
		code, err := c.StartDevice(ctx, clientID, p.flags.scope)
		if err != nil {
			return signedIn{}, fmt.Errorf("starting the sign-in: %w", err)
		}
		p.say("\nOpen %s and enter the code  %s\nWaiting for you to confirm on GitHub...", code.VerificationURI, code.UserCode)
		if tok, err = c.WaitDevice(ctx, clientID, code); err != nil {
			return signedIn{}, err
		}
	} else {
		val, err := p.askSecret("Token (not shown)")
		if err != nil {
			return signedIn{}, err
		}
		if tok.Value = val; val == "" {
			return signedIn{}, errors.New("no token given")
		}
	}
	u, err := c.Whoami(ctx, tok.Value)
	if err != nil {
		return signedIn{}, fmt.Errorf("checking the token: %w", err)
	}
	if tok.Expires.IsZero() {
		tok.Expires = u.Expires
	}
	if err := v.Put(ctx, vault.Secret{ID: personalSource, Source: h.Web, Kind: "token", Value: tok.Value, ExpiresAt: tok.Expires}); err != nil {
		return signedIn{}, err
	}
	if !tok.Expires.IsZero() {
		p.say("This sign-in ends on %s; run scp personal again then.", tok.Expires.Local().Format("2006-01-02 15:04"))
	}
	return signedIn{host: h, user: u.Login, token: tok.Value}, nil
}

// chooseAccounts lists the person's account and organisations and takes the numbers chosen (all by default).
func (p *personal) chooseAccounts(ctx context.Context, c *ghlogin.Client, s signedIn) ([]string, error) {
	orgs, err := c.Orgs(ctx, s.token)
	if err != nil {
		p.say("The organisations could not be listed (%v); only your own account is used. Add more under github.sources[].orgs.", err)
		orgs = nil
	}
	all := append([]string{s.user}, orgs...)
	if len(all) == 1 {
		return all, nil
	}
	p.say("\nWhich accounts should the portal show?")
	for i, a := range all {
		p.say("  %d. %s", i+1, a)
	}
	answer, err := p.ask("Numbers separated by commas (Enter for all)", "")
	if err != nil {
		return nil, err
	}
	if answer == "" {
		return all, nil
	}
	var out []string
	for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(all) {
			return nil, fmt.Errorf("%q is not one of 1 to %d", f, len(all))
		}
		if !slices.Contains(out, all[n-1]) {
			out = append(out, all[n-1])
		}
	}
	return out, nil
}

// findWebBundle looks for the web UI beside the program, then at web/bundle under the working directory.
func findWebBundle() string {
	var cands []string
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "web"))
	}
	cands = append(cands, filepath.Join("web", "bundle"))
	for _, c := range cands {
		if _, err := os.Stat(filepath.Join(c, "server.js")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}

// appSourceID is the id of the source an account's App makes, and of its key in the vault.
func appSourceID(account string) string { return "app-" + strings.ToLower(account) }

func q(s string) string { b, _ := json.Marshal(s); return string(b) } // a YAML double-quoted string

// personalYAML is personal.yaml. products are the groups the person made (none: one group of everything), hidden the repositories left
// out and unassigned the ones no product holds (they go in a group "Other" at the end).
func personalYAML(dir string, h ghlogin.Host, login string, accounts []string, apps []setup.AppKey, products []setup.Product, hidden, unassigned []string, defaultRepo, webDir string, port int) string {
	var b strings.Builder
	w := func(format string, a ...any) { _, _ = fmt.Fprintf(&b, format+"\n", a...) }
	w("# Written by scp personal. Edit it freely and restart; nothing secret is in it (the credential is sealed in the store).")
	w("# Run `scp config check -c %s` to see the effective settings.", filepath.Join(dir, "personal.yaml"))
	w("server:")
	w("  public_addr: %s", q("127.0.0.1:"+strconv.Itoa(port)))
	w("  admin_addr: %s", q("127.0.0.1:"+strconv.Itoa(port+1)))
	w("  public_url: %s", q("http://127.0.0.1:"+strconv.Itoa(port)))
	w("log: {level: info, format: text}")
	w("store: {path: %s}", q(filepath.Join(dir, "scp.db")))
	if webDir != "" {
		w("web: {dir: %s}", q(webDir))
	}
	w("github:")
	w("  sources:")
	var polled []string // the accounts no App of the person's is installed on: read with the token, by polling
	for _, a := range accounts {
		if !slices.ContainsFunc(apps, func(k setup.AppKey) bool { return k.Account == a }) {
			polled = append(polled, a)
		}
	}
	excluded := func() {
		if len(hidden) > 0 {
			w("      exclude: [%s] # kept out of the portal", strings.Join(exactMatchers(hidden), ", "))
		}
	}
	for _, k := range apps {
		w("    - id: %s", appSourceID(k.Account))
		w("      base_url: %s", q(h.API))
		w("      app: {id: %d, credential: true} # the App's key in the store's vault; GitHub's events reach the portal through its delivery log", k.ID)
		w("      include_private: true")
		w("      orgs: [%s]", q(k.Account))
		excluded()
	}
	if len(polled) > 0 {
		w("    - id: %s", personalSource)
		w("      base_url: %s", q(h.API))
		w("      credential: true # the token in the store's vault")
		w("      include_private: true")
		w("      orgs: [%s] # accounts and organisations to show, kept fresh by polling", strings.Join(mapq(polled), ", "))
		excluded()
	}
	if len(apps) > 0 {
		w("webhook: {catch_up: true} # read the Apps' delivery logs: events without anything reaching this computer")
	}
	w("catalog:")
	w("  groups:")
	if len(products) == 0 {
		w("    - id: mine")
		w("      name: My repositories")
		w("      default_repo: %s", q(defaultRepo))
		w("      match: [{catch_all: true}] # see docs/operator-guide.md for groups of your own")
	}
	ids := map[string]bool{"other": true}
	for i, p := range products {
		id := groupID(p.Name, i, ids)
		w("    - id: %s", id)
		w("      name: %s", q(p.Name))
		w("      default_repo: %s", q(p.Repos[0]))
		w("      match: [%s] # rules that new repositories fit, then the repositories pinned one by one", strings.Join(matchers(p), ", "))
	}
	if len(products) > 0 && len(unassigned) > 0 {
		w("    - id: other")
		w("      name: Other")
		w("      default_repo: %s", q(unassigned[0]))
		w("      match: [{unclaimed: true}] # the repositories no product holds")
	}
	w("# The portal is for you alone: no anonymous view, one owner who sees the private repositories.")
	w("access: {require_auth: true, private_groups: [%s]}", personalOwner)
	w("auth:")
	w("  session_key_file: %s", q(filepath.Join(dir, "session.key")))
	w("  # login is your GitHub login: the lists offer what is yours (opened by you, asking your review, assigned to you).")
	w("  tokens: [{name: %s, secret_file: %s, groups: [%s], admin: true, login: %s}]", personalOwner, q(filepath.Join(dir, "owner.token")), personalOwner, q(login))
	return b.String()
}

// matchers are a product's matchers in the configuration's words: the rules derived from its repositories, else each one by name.
func matchers(p setup.Product) []string {
	if len(p.Matchers) == 0 {
		return exactMatchers(p.Repos)
	}
	out := make([]string, len(p.Matchers))
	for i, m := range p.Matchers {
		out[i] = "{" + m.Kind + ": " + q(m.Value) + "}"
	}
	return out
}

func exactMatchers(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "{exact: " + q(n) + "}"
	}
	return out
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// groupID is a group id from a product's name: a-z, 0-9 and -, unique among ids (which it extends).
func groupID(name string, i int, ids map[string]bool) string {
	id := strings.Trim(idUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if id == "" || len(id) > 63 {
		id = "product-" + strconv.Itoa(i+1)
	}
	for base, n := id, 2; ids[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	ids[id] = true
	return id
}

func mapq(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = q(s)
	}
	return out
}
