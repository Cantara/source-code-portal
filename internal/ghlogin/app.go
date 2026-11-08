package ghlogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v93/github"
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
	if code == "" || strings.ContainsAny(code, "/?#% ") { // go-github puts the code into the path as it is
		return App{}, errors.New("ghlogin: GitHub sent no usable code")
	}
	cl, err := c.api(c.base(), "")
	if err != nil {
		return App{}, err
	}
	cfg, _, err := cl.Apps.CompleteAppManifest(ctx, code)
	if err != nil {
		if er := new(gh.ErrorResponse); errors.As(err, &er) && er.Response != nil {
			return App{}, fmt.Errorf("GitHub answered %d to the App's code (it works once, within an hour)", er.Response.StatusCode)
		}
		return App{}, err
	}
	if cfg.GetID() <= 0 || cfg.GetSlug() == "" || cfg.GetPEM() == "" {
		return App{}, errors.New("ghlogin: GitHub's answer holds no App")
	}
	if _, err := ghinstallation.NewAppsTransport(http.DefaultTransport, cfg.GetID(), []byte(cfg.GetPEM())); err != nil { // the key must be one the portal can sign with
		return App{}, errors.New("ghlogin: the App's key is not an RSA private key")
	}
	return App{ID: cfg.GetID(), Slug: cfg.GetSlug(), Name: cfg.GetName(), Owner: cfg.GetOwner().GetLogin(), Key: cfg.GetPEM()}, nil
}

// Installation is an account the App is installed on.
type Installation struct {
	Account   string
	Suspended bool
}

const maxInstallPages = 10

// Installations lists where the App is installed (GET /app/installations, with the App's own token: ghinstallation signs it).
func (c *Client) Installations(ctx context.Context, appID int64, keyPEM string) ([]Installation, error) {
	atr, err := ghinstallation.NewAppsTransport(c.base(), appID, []byte(keyPEM))
	if err != nil {
		return nil, errors.New("ghlogin: the App's key is not an RSA private key")
	}
	cl, err := c.api(atr, "")
	if err != nil {
		return nil, err
	}
	var out []Installation
	for page := 1; page <= maxInstallPages; page++ {
		r, _, err := cl.Apps.ListInstallations(ctx, &gh.ListOptions{PerPage: pageSize, Page: page})
		if err != nil {
			return nil, err
		}
		for _, i := range r {
			out = append(out, Installation{Account: i.GetAccount().GetLogin(), Suspended: i.SuspendedAt != nil})
		}
		if len(r) < pageSize {
			break
		}
	}
	return out, nil
}
