package ghlogin

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The GitHub App of an account (design/sync-modes.md, EV3): the person creates it in their browser from a manifest (GitHub's one-click
// flow), GitHub redirects back with a one-hour code, and exchanging the code is the only call here that is not a GET: it hands over
// the App's id and private key, creates nothing else and writes nothing to any repository.

// AppManifest is the manifest the person's browser submits to GitHub: a private App with the read permissions and the events the
// portal needs, whose webhook points at hookURL (the portal reads GitHub's delivery log, so the URL need not be reachable) and which
// returns to redirectURL with the code.
func AppManifest(name, homepage, hookURL, redirectURL string, permissions map[string]string, events []string) string {
	m := map[string]any{
		"name":                     name,
		"url":                      homepage,
		"hook_attributes":          map[string]any{"url": hookURL, "active": true},
		"redirect_url":             redirectURL,
		"public":                   false,
		"default_permissions":      permissions,
		"default_events":           events,
		"request_oauth_on_install": false,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// AppCreateURL is the address the manifest is posted to: the person's own account, or an organisation they own. state comes back
// to the redirect URL unchanged.
func (h Host) AppCreateURL(account string, self bool, state string) string {
	if self {
		return h.Web + "/settings/apps/new?state=" + url.QueryEscape(state)
	}
	return h.Web + "/organizations/" + url.PathEscape(account) + "/settings/apps/new?state=" + url.QueryEscape(state)
}

// AppInstallURL is where the person installs the App on its account.
func (h Host) AppInstallURL(slug string) string {
	return h.Web + "/apps/" + url.PathEscape(slug) + "/installations/new"
}

// AppURL is the App's own page.
func (h Host) AppURL(slug string) string { return h.Web + "/apps/" + url.PathEscape(slug) }

// App is what GitHub hands over when the manifest's code is exchanged.
type App struct {
	ID    int64
	Slug  string
	Name  string
	Owner string // the login of the account that owns it
	Key   string // the private key, PEM
}

// ConvertManifest exchanges the code GitHub redirected with for the App (POST /app-manifests/{code}/conversions, unauthenticated: the
// code is the proof). The webhook secret in the answer is not kept: the portal reads the delivery log, it has no receiver.
func (c *Client) ConvertManifest(ctx context.Context, code string) (App, error) {
	if code == "" || strings.ContainsAny(code, "/?#% ") {
		return App{}, errors.New("ghlogin: GitHub sent no usable code")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host.API+"app-manifests/"+code+"/conversions", nil)
	if err != nil {
		return App{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := c.http().Do(req)
	if err != nil {
		return App{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return App{}, err
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return App{}, fmt.Errorf("GitHub answered %d to the App's code (it works once, within an hour)", res.StatusCode)
	}
	var out struct {
		ID    int64  `json:"id"`
		Slug  string `json:"slug"`
		Name  string `json:"name"`
		PEM   string `json:"pem"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return App{}, err
	}
	if out.ID <= 0 || out.Slug == "" || out.PEM == "" {
		return App{}, errors.New("ghlogin: GitHub's answer holds no App")
	}
	if _, err := signer(out.PEM); err != nil {
		return App{}, err
	}
	return App{ID: out.ID, Slug: out.Slug, Name: out.Name, Owner: out.Owner.Login, Key: out.PEM}, nil
}

func signer(keyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("ghlogin: the App's key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("ghlogin: the App's key is not an RSA private key")
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("ghlogin: the App's key is not an RSA private key")
	}
	return rk, nil
}

// appJWT is the short-lived token an App speaks with: RS256, issued a minute ago (clock skew), valid for ten minutes.
func appJWT(appID int64, keyPEM string, now time.Time) (string, error) {
	key, err := signer(keyPEM)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": strconv.FormatInt(appID, 10)})
	in := head + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return in + "." + enc.EncodeToString(sig), nil
}

// Installation is an account the App is installed on.
type Installation struct {
	Account   string
	Suspended bool
}

// Installations lists where the App is installed (GET /app/installations, with the App's own token).
func (c *Client) Installations(ctx context.Context, appID int64, keyPEM string) ([]Installation, error) {
	jwt, err := appJWT(appID, keyPEM, c.now())
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Account     struct{ Login string } `json:"account"`
		SuspendedAt *time.Time             `json:"suspended_at"`
	}
	if _, err := c.get(ctx, jwt, "app/installations?per_page=100", &raw); err != nil {
		return nil, err
	}
	out := make([]Installation, len(raw))
	for i, r := range raw {
		out[i] = Installation{Account: r.Account.Login, Suspended: r.SuspendedAt != nil}
	}
	return out, nil
}
