// Package snyk is the Snyk REST StatusProvider (Q-25). One paged project listing per sweep answers every
// repository (counts come from meta.latest_issue_counts), so the sweep costs ceil(projects/100) requests.
package snyk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/integrations"
)

const (
	userAgent = "scp-integrations/1"
	pageLimit = "100" // maximum of the documented limit parameter (multipleOf 10, 10..100)
	maxPages  = 200   // safety valve: 20,000 projects
)

var projectIDRE = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// Provider implements integrations.BulkProvider.
type Provider struct {
	cfg  integrations.SnykConfig
	base *url.URL
	hc   *http.Client
	tok  string
	now  func() time.Time
}

// New takes the SSRF-guarded client built by cmd/scp (netguard, regional host allow-listed) and pins redirects to the base host.
func New(cfg integrations.SnykConfig, hc *http.Client) (*Provider, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, fmt.Errorf("snyk: base_url %q is not an https origin", cfg.BaseURL)
	}
	if hc == nil {
		return nil, fmt.Errorf("snyk: no HTTP client")
	}
	cp := *hc
	cp.Timeout = cfg.Timeout
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 2 || req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &Provider{cfg: cfg, base: base, hc: &cp, tok: cfg.Token.Reveal(), now: time.Now}, nil
}

func (p *Provider) Name() string { return integrations.ProviderSnyk }

type counts struct {
	Critical float64 `json:"critical"`
	High     float64 `json:"high"`
	Medium   float64 `json:"medium"`
	Low      float64 `json:"low"`
}

type project struct {
	ID         string `json:"id"`
	Attributes struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Origin string `json:"origin"`
	} `json:"attributes"`
	Meta struct {
		LatestIssueCounts *counts `json:"latest_issue_counts"`
	} `json:"meta"`
	Relationships struct {
		Target struct {
			Data struct {
				Attributes struct {
					DisplayName string `json:"display_name"`
				} `json:"attributes"`
			} `json:"data"`
		} `json:"target"`
	} `json:"relationships"`
}

type page struct {
	Data  []project `json:"data"`
	Links struct {
		Next json.RawMessage `json:"next"`
	} `json:"links"`
}

type agg struct {
	c        [4]int // critical, high, medium, low
	projects int
	firstID  string
}

// Fetch answers one target by listing everything (used by doctor and tests; sweeps use FetchBulk).
func (p *Provider) Fetch(ctx context.Context, t integrations.Target) (integrations.Status, error) {
	rs, err := p.FetchBulk(ctx, []integrations.Target{t})
	if err != nil {
		return integrations.Status{}, err
	}
	return rs[0].Status, rs[0].Err
}

// FetchBulk lists the organisation's projects once and maps them onto the targets by target display name.
func (p *Provider) FetchBulk(ctx context.Context, targets []integrations.Target) ([]integrations.Result, error) {
	byName := map[string]*agg{}
	after := ""
	for n := 0; ; n++ {
		if n >= maxPages {
			return nil, fmt.Errorf("snyk: more than %d pages: %w", maxPages, integrations.ErrProtocol)
		}
		pg, err := p.page(ctx, after)
		if err != nil {
			return nil, err
		}
		for _, pr := range pg.Data {
			if pr.Attributes.Status != "active" || pr.Meta.LatestIssueCounts == nil {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(pr.Relationships.Target.Data.Attributes.DisplayName))
			if key == "" {
				continue
			}
			a := byName[key]
			if a == nil {
				a = &agg{}
				byName[key] = a
			}
			ic := pr.Meta.LatestIssueCounts
			a.c[0] += clamp(ic.Critical)
			a.c[1] += clamp(ic.High)
			a.c[2] += clamp(ic.Medium)
			a.c[3] += clamp(ic.Low)
			a.projects++
			if a.firstID == "" && projectIDRE.MatchString(pr.ID) {
				a.firstID = pr.ID
			}
		}
		next := nextCursor(pg.Links.Next)
		if next == "" || next == after {
			break
		}
		after = next
	}
	out := make([]integrations.Result, len(targets))
	for i, t := range targets {
		a := byName[strings.ToLower(t.Project)]
		if a == nil {
			out[i].Err = integrations.ErrNoData
			continue
		}
		out[i].Status = p.status(a, t)
	}
	return out, nil
}

// clamp saturates at integrations.MaxCount. A value above the cap must never wrap to 0: "0 critical" would read as safe.
func clamp(f float64) int {
	switch {
	case f != f || f < 0: // NaN or negative
		return 0
	case f > integrations.MaxCount:
		return integrations.MaxCount
	}
	return int(f)
}

func (p *Provider) page(ctx context.Context, after string) (page, error) {
	q := url.Values{}
	q.Set("version", p.cfg.APIVersion)
	q.Set("limit", pageLimit)
	q.Set("expand", "target")
	q.Set("meta.latest_issue_counts", "true")
	if len(p.cfg.Origins) > 0 {
		q.Set("origins", strings.Join(p.cfg.Origins, ","))
	}
	if after != "" {
		q.Set("starting_after", after)
	}
	u := *p.base
	u.Path = "/rest/orgs/" + url.PathEscape(p.cfg.OrgID) + "/projects"
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return page{}, err
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Authorization", "Token "+p.tok)
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.hc.Do(req)
	if err != nil {
		return page{}, fmt.Errorf("snyk: request failed: %w", errTransport(err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return page{}, &integrations.PermanentError{Provider: p.Name(), Code: "auth", Err: fmt.Errorf("HTTP %d: token rejected (Snyk tokens are region-specific: check base_url)", resp.StatusCode)}
	case resp.StatusCode == http.StatusNotFound:
		return page{}, &integrations.PermanentError{Provider: p.Name(), Code: "config", Err: fmt.Errorf("HTTP 404: organisation not found")}
	case resp.StatusCode == http.StatusBadRequest:
		return page{}, &integrations.PermanentError{Provider: p.Name(), Code: "config", Err: fmt.Errorf("HTTP 400: request rejected (check api_version and origins)")}
	case resp.StatusCode == http.StatusTooManyRequests:
		return page{}, &integrations.RateLimitError{Provider: p.Name(), At: retryAt(resp, p.now()), Err: fmt.Errorf("HTTP 429")}
	case resp.StatusCode >= 500:
		return page{}, fmt.Errorf("snyk: HTTP %d: %w", resp.StatusCode, errTransportSentinel)
	default:
		return page{}, fmt.Errorf("snyk: HTTP %d: %w", resp.StatusCode, integrations.ErrProtocol)
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || !(mt == "application/vnd.api+json" || mt == "application/json") {
		return page{}, fmt.Errorf("snyk: content type %q: %w", resp.Header.Get("Content-Type"), integrations.ErrProtocol)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxResponseBytes+1))
	if err != nil {
		return page{}, fmt.Errorf("snyk: read body: %w", errTransportSentinel)
	}
	if int64(len(body)) > p.cfg.MaxResponseBytes {
		return page{}, integrations.ErrTooLarge
	}
	var pg page
	if err := json.Unmarshal(body, &pg); err != nil {
		return page{}, fmt.Errorf("snyk: decode: %w", integrations.ErrProtocol)
	}
	return pg, nil
}

var errTransportSentinel = fmt.Errorf("transport error")

func errTransport(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return fmt.Errorf("%s: %w", ue.Err, errTransportSentinel)
	}
	return fmt.Errorf("%v: %w", err, errTransportSentinel)
}

func retryAt(resp *http.Response, now time.Time) time.Time {
	if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n >= 0 && n <= 3600 {
		return now.Add(time.Duration(n) * time.Second)
	}
	return now.Add(time.Minute) // Retry-After is not documented by Snyk (2026-10-14); the documented limit is 1,620 requests/min/key
}

// nextCursor extracts ONLY the starting_after cursor from links.next (a string or {"href": ...}). The URL itself is
// never followed: a hostile or buggy value cannot redirect the token to another host or another path.
func nextCursor(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var o struct {
			Href string `json:"href"`
		}
		if json.Unmarshal(raw, &o) != nil {
			return ""
		}
		s = o.Href
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	c := u.Query().Get("starting_after")
	if len(c) > 512 {
		return ""
	}
	return c
}

var sevs = [4]string{"critical", "high", "medium", "low"}

func (p *Provider) status(a *agg, t integrations.Target) integrations.Status {
	st := integrations.Status{Provider: p.Name(), Counts: map[string]int{}}
	failAt := 1
	for i, s := range sevs {
		if s == p.cfg.FailOn {
			failAt = i
		}
	}
	var parts []string
	state := integrations.StateOK
	for i, s := range sevs {
		st.Counts[s] = a.c[i]
		if a.c[i] == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", a.c[i], s))
		if i <= failAt {
			state = integrations.StateFailing
		} else if state == integrations.StateOK {
			state = integrations.StateUnstable
		}
	}
	st.State = state
	if len(parts) == 0 {
		st.Summary = "no known issues"
	} else {
		st.Summary = strings.Join(parts, ", ")
	}
	st.DetailURL = p.detailURL(a, t)
	return st
}

// detailURL expands the operator-configured template with validated values only (project id: UUID; owner and repo come
// from the catalogue and are path-escaped). Empty template, no link.
func (p *Provider) detailURL(a *agg, t integrations.Target) string {
	tpl := p.cfg.DetailURLTemplate
	if tpl == "" {
		return ""
	}
	if strings.Contains(tpl, "{project_id}") && a.firstID == "" {
		return ""
	}
	r := strings.NewReplacer("{project_id}", a.firstID, "{owner}", url.PathEscape(t.Repo.Owner), "{repo}", url.PathEscape(t.Repo.Name))
	u, err := url.Parse(r.Replace(tpl))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return ""
	}
	return u.String()
}

// Check implements integrations.Checker: one project page (limit 100) proves region, token and organisation.
func (p *Provider) Check(ctx context.Context) []integrations.CheckResult {
	_, err := p.page(ctx, "")
	return []integrations.CheckResult{{Err: err}}
}

var _ integrations.BulkProvider = (*Provider)(nil)
var _ integrations.Checker = (*Provider)(nil)
