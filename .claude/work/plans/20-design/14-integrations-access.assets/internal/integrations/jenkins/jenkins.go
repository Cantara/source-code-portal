// Package jenkins is the Jenkins JSON API StatusProvider. It reads structured build data from
// {base}/job/.../api/json, never an SVG, and builds every request URL itself from validated parts.
package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/integrations"
	"github.com/cantara/source-code-portal/internal/scm"
)

// tree selects the minimum Jenkins returns: class and colour of the job, the last completed build and whether a
// newer build is running. (Jenkins "tree" syntax: https://github.com/jenkinsci/jenkins Api/index.jelly, read 2026-10-14.)
const tree = "_class,color,lastCompletedBuild[number,result,timestamp,duration],lastBuild[number,building]"

const userAgent = "scp-integrations/1"

type instance struct {
	cfg  integrations.JenkinsInstance
	base *url.URL
	hc   *http.Client // shallow copy of the injected client with a pinned redirect policy
	pass string
}

// Provider serves every configured Jenkins instance.
type Provider struct {
	inst map[string]*instance
	now  func() time.Time
}

// New builds the provider. clients maps an instance name to the SSRF-guarded client that cmd/scp built with
// platform/netguard from that instance's allow_private_cidrs and ports; the provider never builds a transport.
func New(cfgs []integrations.JenkinsInstance, clients map[string]*http.Client) (*Provider, error) {
	p := &Provider{inst: map[string]*instance{}, now: time.Now}
	for _, c := range cfgs {
		hc := clients[c.Name]
		if hc == nil {
			return nil, fmt.Errorf("jenkins: no HTTP client for instance %q", c.Name)
		}
		if err := integrations.ValidateBaseURL(c.BaseURL, c.AllowHTTP); err != nil {
			return nil, fmt.Errorf("jenkins: instance %q: %w", c.Name, err)
		}
		base, _ := url.Parse(strings.TrimRight(c.BaseURL, "/"))
		cp := *hc // copy: the redirect policy is ours, whatever the injected client had
		cp.Timeout = c.Timeout
		cp.CheckRedirect = pinRedirects(base)
		p.inst[c.Name] = &instance{cfg: c, base: base, hc: &cp, pass: c.Token.Reveal()}
	}
	return p, nil
}

func (p *Provider) Name() string { return integrations.ProviderJenkins }

// pinRedirects allows at most 3 redirects, each to the same scheme, host and port as the configured base URL
// (Jenkins redirects to add a trailing slash). Anything else is not followed: credentials never travel to another host.
func pinRedirects(base *url.URL) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return http.ErrUseLastResponse
		}
		if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}
}

type build struct {
	Number    int    `json:"number"`
	Result    string `json:"result"`
	Building  bool   `json:"building"`
	Timestamp int64  `json:"timestamp"`
}

type jobJSON struct {
	Class              string `json:"_class"`
	Color              string `json:"color"`
	LastCompletedBuild *build `json:"lastCompletedBuild"`
	LastBuild          *build `json:"lastBuild"`
}

// Fetch implements integrations.StatusProvider.
func (p *Provider) Fetch(ctx context.Context, t integrations.Target) (integrations.Status, error) {
	in, ok := p.inst[t.Instance]
	if !ok {
		return integrations.Status{}, &integrations.PermanentError{Provider: p.Name(), Code: "config", Err: fmt.Errorf("unknown instance %q", t.Instance)}
	}
	jp, err := integrations.ParseJobPath(t.Job)
	if err != nil {
		return integrations.Status{}, &integrations.PermanentError{Provider: p.Name(), Code: "config", Err: err}
	}
	path := jp.URLPath()
	j, err := in.get(ctx, path)
	if err != nil {
		return integrations.Status{}, err
	}
	mb := in.cfg.Multibranch
	if mb == "always" || (mb == "auto" && strings.Contains(j.Class, "MultiBranch")) {
		if t.Branch == "" || t.Branch == "." || t.Branch == ".." { // PathEscape leaves dot segments alone; git ref names cannot be them
			return integrations.Status{Provider: p.Name(), State: integrations.StateUnknown, Summary: "no branch to look up"}, nil
		}
		j, err = in.get(ctx, path+"/job/"+branchSegment(t.Branch))
		if err != nil {
			if errors.Is(err, scm.ErrNotFound) {
				return integrations.Status{Provider: p.Name(), State: integrations.StateUnknown, Summary: "branch not built"}, nil
			}
			return integrations.Status{}, err
		}
		path += "/job/" + branchSegment(t.Branch)
	}
	return in.status(j, path), nil
}

// branchSegment encodes a branch ref as a multibranch job-name path segment. Jenkins stores the item name with "/"
// as "%2F" and the URL carries that name path-escaped again, so a slash becomes "%252F". Corroborated by the Jenkins
// issue tracker (JENKINS-36154, JENKINS-42390) but not by the Jenkins documentation: M0 verifies it against a real
// instance and records the fixture.
func branchSegment(branch string) string {
	return url.PathEscape(url.PathEscape(branch))
}

func (in *instance) get(ctx context.Context, jobPath string) (jobJSON, error) {
	return in.getRaw(ctx, jobPath, tree)
}

func (in *instance) getRaw(ctx context.Context, jobPath, treeExpr string) (jobJSON, error) {
	raw := in.base.String() + jobPath + "/api/json?tree=" + url.QueryEscape(treeExpr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return jobJSON{}, fmt.Errorf("jenkins: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if in.cfg.User != "" {
		req.SetBasicAuth(in.cfg.User, in.pass) // only ever sent to this instance's host: redirects are pinned
	}
	resp, err := in.hc.Do(req)
	if err != nil {
		return jobJSON{}, fmt.Errorf("jenkins: %s: %w", redact(err), errTransport)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)) // let the connection be reused after a small error body
		resp.Body.Close()
	}()
	// A redirect to the login page means anonymous read is disabled and no (valid) credentials were sent: instance-wide.
	if resp.Request != nil && strings.HasSuffix(strings.TrimRight(resp.Request.URL.Path, "/"), "/login") {
		return jobJSON{}, &integrations.PermanentError{Provider: integrations.ProviderJenkins, Code: "auth", Err: errors.New("redirected to the Jenkins login page: configure user and token")}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return jobJSON{}, &integrations.PermanentError{Provider: integrations.ProviderJenkins, Code: "auth", Err: fmt.Errorf("HTTP 401: credentials rejected")}
	case resp.StatusCode == http.StatusForbidden:
		// Per target: Jenkins answers 403 for a job or folder the user may not read while other jobs are readable.
		// The sweep aborts the instance only after abort_after of them in a row.
		return jobJSON{}, fmt.Errorf("jenkins: HTTP 403 for this job: %w", scm.ErrForbidden)
	case resp.StatusCode == http.StatusNotFound:
		return jobJSON{}, fmt.Errorf("jenkins: job not found: %w", scm.ErrNotFound)
	case resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != ""):
		return jobJSON{}, &integrations.RateLimitError{Provider: integrations.ProviderJenkins, At: retryAt(resp, time.Now()), Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	case resp.StatusCode >= 500:
		return jobJSON{}, fmt.Errorf("jenkins: HTTP %d: %w", resp.StatusCode, errTransport)
	default:
		return jobJSON{}, fmt.Errorf("jenkins: HTTP %d: %w", resp.StatusCode, integrations.ErrProtocol)
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return jobJSON{}, fmt.Errorf("jenkins: content type %q: %w", resp.Header.Get("Content-Type"), integrations.ErrProtocol)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, in.cfg.MaxResponseBytes+1))
	if err != nil {
		return jobJSON{}, fmt.Errorf("jenkins: read body: %w", errTransport)
	}
	if int64(len(body)) > in.cfg.MaxResponseBytes {
		return jobJSON{}, integrations.ErrTooLarge
	}
	var j jobJSON
	if err := json.Unmarshal(body, &j); err != nil {
		return jobJSON{}, fmt.Errorf("jenkins: decode: %w", integrations.ErrProtocol)
	}
	return j, nil
}

var errTransport = errors.New("transport error")

// redact keeps the error class without the URL (the request URL contains the job path; never log tokens, query strings or bodies).
func redact(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + " failed: " + ue.Err.Error()
	}
	return err.Error()
}

func retryAt(resp *http.Response, now time.Time) time.Time {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 3600 {
			return now.Add(time.Duration(n) * time.Second)
		}
		if t, err := http.ParseTime(s); err == nil && t.After(now) && t.Before(now.Add(time.Hour)) {
			return t
		}
	}
	return now.Add(time.Minute)
}

// status maps Jenkins' answer onto the provider-neutral Status. Summary is generated; nothing from the response
// except the build number and the result enum reaches it.
func (in *instance) status(j jobJSON, jobPath string) integrations.Status {
	st := integrations.Status{Provider: integrations.ProviderJenkins, State: integrations.StateUnknown}
	switch {
	case j.Color == "disabled":
		st.Summary = "job disabled"
		return st
	case j.LastCompletedBuild == nil && strings.Contains(j.Class, "Folder"):
		st.Summary = "job is a folder"
		return st
	case j.LastCompletedBuild == nil:
		st.Summary = "no completed builds"
		return st
	}
	b := j.LastCompletedBuild
	var word string
	switch b.Result {
	case "SUCCESS":
		st.State, word = integrations.StateOK, "passed"
	case "UNSTABLE":
		st.State, word = integrations.StateUnstable, "unstable"
	case "FAILURE":
		st.State, word = integrations.StateFailing, "failed"
	case "ABORTED":
		word = "aborted"
	case "NOT_BUILT":
		word = "not built"
	default:
		word = "result unknown"
	}
	st.Summary = fmt.Sprintf("build #%d %s", b.Number, word)
	if j.LastBuild != nil && j.LastBuild.Building && j.LastBuild.Number != b.Number {
		st.Summary += fmt.Sprintf(" (build #%d running)", j.LastBuild.Number)
	}
	if in.cfg.ExposeDetailURL {
		st.DetailURL = in.base.String() + jobPath + "/" + strconv.Itoa(b.Number) + "/"
	}
	return st
}

// Check implements integrations.Checker: GET {base}/api/json?tree=_class with the instance's credentials.
func (p *Provider) Check(ctx context.Context) []integrations.CheckResult {
	var out []integrations.CheckResult
	for _, name := range sortedNames(p.inst) {
		_, err := p.inst[name].getRaw(ctx, "", "_class")
		if errors.Is(err, scm.ErrForbidden) { // at the root, 403 means the credentials lack Overall/Read: instance-wide
			err = &integrations.PermanentError{Provider: p.Name(), Code: "auth", Err: errors.New("HTTP 403 at the instance root: user or token lacks Overall/Read")}
		}
		out = append(out, integrations.CheckResult{Instance: name, Err: err})
	}
	return out
}

func sortedNames(m map[string]*instance) []string {
	var n []string
	for k := range m {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

var _ integrations.StatusProvider = (*Provider)(nil)
var _ integrations.Checker = (*Provider)(nil)
