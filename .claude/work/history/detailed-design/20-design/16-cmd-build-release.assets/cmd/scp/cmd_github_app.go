package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/routes"
)

// cmdGitHubAppManifest is a thin wrapper: the flow itself (loopback server, state check, conversion, 0600 files) is
// github.RunAppManifest (design 05 section 4.14). This command owns the flags, the defaults that come from the portal
// configuration, and the exit codes.
func cmdGitHubAppManifest(ctx context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "github app manifest")
	path := configPathFlag(fl, env)
	web := fl.String("web-url", "https://github.com", "GitHub web URL (GHES: https://ghe.example.com; GHEC-DR: https://SUB.ghe.com)")
	api := fl.String("api-url", "", "GitHub API URL (default: derived from --web-url by the adapter: github.com, *.ghe.com or a GHES host)")
	org := fl.String("org", "", "register under this organization (default: your personal account)")
	name := fl.String("name", "", "App name (default scp-<org>)")
	source := fl.String("source", "", "id of the github.sources[] entry you will configure (used for the default webhook route)")
	webhook := fl.String("webhook-url", "", "public webhook URL (default: server.public_url + the webhook route of --source)")
	noHook := fl.Bool("no-webhook", false, "register without a webhook (polling only)")
	features := fl.String("features", "core", "comma-separated optional features whose read permissions are requested: pull_requests,issues,commit_statuses,checks,actions")
	outDir := fl.String("out-dir", ".", "directory for the private key and webhook secret files (0600, never overwritten)")
	code := fl.String("code", "", "headless: exchange this already obtained manifest code instead of opening a browser")
	listen := fl.String("listen", "127.0.0.1:0", "loopback callback address")
	timeout := fl.Duration("timeout", 15*time.Minute, "whole flow")
	if c, done := parseFlags(fl, args); done {
		return c
	}
	if fl.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp github app manifest: no positional arguments")
		return exitUsage
	}
	l, err := readConfig(loadOptions{file: *path, environ: env.Environ})
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp github app manifest: %v\n", err)
		return exitFail
	}
	if len(l.violations) > 0 {
		_, _ = fmt.Fprint(env.Stderr, l.violationText())
		return exitInvalid
	}
	cfg := l.cfg
	hook := *webhook
	if hook == "" && !*noHook {
		if cfg.Server.Public.PublicURL == "" {
			_, _ = fmt.Fprintln(env.Stderr, "scp github app manifest: set server.public_url in the configuration, pass --webhook-url, or pass --no-webhook")
			return exitUsage
		}
		var err error
		if hook, err = routes.Absolute(cfg.Server.Public.PublicURL, routes.Webhook(*source)); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "scp github app manifest: %v\n", err)
			return exitUsage
		}
	}
	apiURL := *api // empty: RunAppManifest derives it from the web URL (05 section 4.1 step 2); the route-literal rule keeps GHES path knowledge out of cmd/scp
	var fs []github.Feature
	for f := range strings.SplitSeq(*features, ",") {
		if f = strings.TrimSpace(f); f != "" && f != "core" {
			fs = append(fs, github.Feature(f))
		}
	}
	if env.AppManifest == nil {
		_, _ = fmt.Fprintln(env.Stderr, "scp github app manifest: not available in this build")
		return exitFail
	}
	res, err := env.AppManifest(ctx, github.ManifestOptions{WebURL: *web, APIURL: apiURL, Org: *org, Name: *name, WebhookURL: hook, NoWebhook: *noHook,
		Features: append([]github.Feature{github.FeatureCore}, fs...), OutDir: *outDir, Code: *code, Listen: *listen, Timeout: *timeout, Out: env.Stdout})
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp github app manifest: %v\n", err)
		return exitFail
	}
	_, _ = fmt.Fprintf(env.Stdout, "\nApp %q (id %d) created. Install it on the organization and select the repositories to show:\n  %s\n\nWrote (mode 0600):\n  %s\n",
		res.Slug, res.AppID, res.InstallURL, res.PEMPath)
	if res.WebhookSecretPath != "" {
		_, _ = fmt.Fprintf(env.Stdout, "  %s\n", res.WebhookSecretPath)
	}
	_, _ = fmt.Fprintf(env.Stdout, "\nConfiguration snippet:\n\n%s\nRun `scp doctor` after installing.\n", res.ConfigSnippet)
	return exitOK
}
