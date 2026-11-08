//go:build !server

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/auth"
	"github.com/cantara/source-code-portal/internal/setup"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/vault"
)

// defaultClientID is the client id of the app people sign in with on github.com (the device flow needs no secret). Empty until the
// app is registered; --client-id names one, and without one the page asks for a token.
const defaultClientID = ""

// browserSetup is the first run in the browser (design/personal.md): a setup page on the port the portal will use, behind a one-use
// code, where the person signs in to GitHub, chooses accounts and sorts repositories into products. When they finish it has written
// personal.yaml, sealed the credential and made the sign-in link of the portal that starts next on the same port.
func (p *personal) browserSetup(ctx context.Context, dir, cfgPath string) (*auth.Launch, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, where, err := vault.OpenKey(dir, p.keyRing(dir))
	if err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(p.flags.port))
	if err != nil {
		return nil, fmt.Errorf("the setup page needs port %d on this computer: %w (name another with --port)", p.flags.port, err)
	}
	code := make([]byte, 24)
	if _, err := rand.Read(code); err != nil {
		_ = ln.Close()
		return nil, err
	}
	entry := base64.RawURLEncoding.EncodeToString(code)
	var launch *auth.Launch
	clientID := p.flags.clientID
	if clientID == "" {
		clientID = defaultClientID
	}
	s := setup.New(setup.Config{Addr: ln.Addr().String(), Code: entry, Host: p.flags.host, ClientID: clientID, Scope: p.flags.scope,
		HTTP: p.http, Now: p.now, CLIToken: p.cliTokenFunc(),
		Finish: func(ctx context.Context, r setup.Result) (string, error) {
			var err error
			if launch, err = p.finishSetup(ctx, dir, cfgPath, key, r); err != nil {
				return "", err
			}
			return "/auth/launch?code=" + launch.Code, nil
		}})
	defer s.Close()
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	link := "http://" + ln.Addr().String() + "/setup/?code=" + entry
	if where == vault.InFile {
		p.say("Note: this computer has no keychain, so the key that seals your credential is the file %s (readable by you only).", filepath.Join(dir, "vault.key"))
	}
	p.say("Open this page to set up your portal (it works once, from one browser):\n\n  %s\n", link)
	if !p.flags.noBrowser {
		p.openPage(ctx, link)
	}
	select {
	case <-s.Done():
	case err := <-served:
		return nil, fmt.Errorf("the setup page stopped: %w", err)
	case <-ctx.Done():
		_ = srv.Close()
		return nil, ctx.Err()
	}
	// The answer to the last call is on its way; give it a moment, then free the port for the portal.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	p.say("Settings saved in %s. Starting your portal...", cfgPath)
	return launch, nil
}

// cliTokenFunc offers the GitHub CLI's token when `gh` is installed (or a test supplies one): the setup page shows a button for it.
func (p *personal) cliTokenFunc() func(context.Context, string) (string, error) {
	if p.cliToken != nil {
		return p.cliToken
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil
	}
	return func(ctx context.Context, hostname string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, gh, "auth", "token", "--hostname", hostname).Output() //nolint:gosec // G204: gh found on PATH, the host name is a parsed URL host
		return string(out), err
	}
}

func (p *personal) openPage(ctx context.Context, link string) {
	if p.open != nil {
		p.open(link)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", link) //nolint:gosec // G204: the link is our own, on loopback
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", link) //nolint:gosec // G204: as above
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", link) //nolint:gosec // G204: as above
	}
	if err := cmd.Start(); err != nil {
		return // the link is printed; the person opens it
	}
	go func() { _ = cmd.Wait() }()
}

// finishSetup writes what the person chose: the sealed credential, personal.yaml (the old one kept as .bak) and the local secrets.
func (p *personal) finishSetup(ctx context.Context, dir, cfgPath string, key []byte, r setup.Result) (*auth.Launch, error) {
	st, err := store.OpenAs(ctx, filepath.Join(dir, "scp.db"), store.Personal, slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, err
	}
	defer st.Close()
	if err := os.Chmod(filepath.Join(dir, "scp.db"), 0o600); err != nil {
		return nil, err
	}
	v, err := vault.New(key, st)
	if err != nil {
		return nil, err
	}
	if err := v.Put(ctx, vault.Secret{ID: personalSource, Source: r.Host.Web, Kind: "token", Value: r.Token.Value, ExpiresAt: r.Token.Expires}); err != nil {
		return nil, err
	}
	for _, k := range r.Apps {
		if err := v.Put(ctx, vault.Secret{ID: appSourceID(k.Account), Source: r.Host.Web, Kind: "app-key", Value: k.Key}); err != nil {
			return nil, err
		}
	}
	if err := p.secrets(dir); err != nil {
		return nil, err
	}
	if old, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: the person's own file
		if err := os.WriteFile(cfgPath+".bak", old, 0o600); err != nil { //nolint:gosec // G703: the personal directory's own file
			return nil, err
		}
	}
	webDir := p.flags.webDir
	if webDir == "" {
		webDir = findWebBundle()
	}
	unassigned := r.Unassigned
	slices.Sort(unassigned)
	yaml := personalYAML(dir, r.Host, r.User, r.Accounts, r.Apps, r.Products, r.Hidden, unassigned, r.Default, webDir, p.flags.port)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		return nil, err
	}
	l, err := newLaunch(time.Now())
	if err != nil {
		return nil, err
	}
	return l, nil
}
