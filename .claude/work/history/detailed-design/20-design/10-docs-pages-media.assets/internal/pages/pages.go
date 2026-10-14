// Package pages serves config-driven Markdown pages and the optional plain-text announcement (Q-04: the replacement of
// the Confluence mirror and the banner). Pages are files in one flat directory; nothing is fetched from GitHub.
package pages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/safehtml"
)

// Names registered by cmd/scp.
const (
	GlobalName = "pages.set"
	TaskName   = "pages.reload"
	KindName   = "pages.set" // Freshness.kind
)

// Metric names.
const (
	MetricReloadTotal = "scp_pages_reload_total"
	MetricPages       = "scp_pages"
	MetricProblems    = "scp_pages_problems"
)

// Config is the pages section of the root configuration.
type Config struct {
	Dir          string             `yaml:"dir"`            // flat directory of <slug>.md files; "" disables pages
	MaxPages     int                `yaml:"max_pages"`      // pages loaded, 1..500
	MaxPageBytes int                `yaml:"max_page_bytes"` // size of one page file, 1 KiB..1 MiB
	ReloadEvery  time.Duration      `yaml:"reload_every"`   // 0 = on demand only (admin trigger, SIGHUP), else 10s..24h
	Announcement AnnouncementConfig `yaml:"announcement"`
}

// AnnouncementConfig is the announcement block.
type AnnouncementConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Text        string `yaml:"text"`  // plain text, 1..500 runes after cleaning
	Level       string `yaml:"level"` // info | warning | critical
	Dismissible bool   `yaml:"dismissible"`
}

// DefaultConfig returns the shipped defaults (pages and the announcement are off).
func DefaultConfig() Config {
	return Config{MaxPages: 100, MaxPageBytes: 256 << 10, Announcement: AnnouncementConfig{Level: "info", Dismissible: true}}
}

// Validate returns every violation with its path.
func (c Config) Validate(prefix string) []error {
	var errs []error
	bad := func(k, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s.%s: %s", prefix, k, fmt.Sprintf(f, a...)))
	}
	if c.MaxPages < 1 || c.MaxPages > 500 {
		bad("max_pages", "must be between 1 and 500, got %d", c.MaxPages)
	}
	if c.MaxPageBytes < 1<<10 || c.MaxPageBytes > 1<<20 {
		bad("max_page_bytes", "must be between 1 KiB and 1 MiB, got %d", c.MaxPageBytes)
	}
	if c.ReloadEvery != 0 && (c.ReloadEvery < 10*time.Second || c.ReloadEvery > 24*time.Hour) {
		bad("reload_every", "must be 0 or between 10s and 24h, got %s", c.ReloadEvery)
	}
	a := c.Announcement
	if a.Enabled {
		switch a.Level {
		case "info", "warning", "critical":
		default:
			bad("announcement.level", "must be info, warning or critical, got %q", a.Level)
		}
		if n := utf8.RuneCountInString(cleanText(a.Text, 1000)); n < 1 || n > 500 {
			bad("announcement.text", "must be between 1 and 500 characters after cleaning, got %d", n)
		}
	}
	return errs
}

// TOCEntry is one heading of a rendered page.
type TOCEntry struct {
	Depth int
	Text  string
	ID    string
}

// Problem is a non-fatal problem of one file.
type Problem struct{ File, Code, Message string }

// RenderedPage is what the injected Renderer returns for one page source (architecture 8.10 declared a narrower
// RenderPage; the TOC, the description and the front matter keys are needed by the API: deviation D-4).
type RenderedPage struct {
	HTML        safehtml.SafeHTML
	Title       string
	Description string
	Order       *int
	Hidden      bool
	Tags        []string
	TOC         []TOCEntry
	Problems    []Problem
}

// Renderer is bridged to docs.Service.RenderPage by cmd/scp/internal/bridge.
type Renderer interface {
	RenderPage(ctx context.Context, src []byte) (RenderedPage, error)
}

// Page is one loaded page.
type Page struct {
	Slug      string
	Title     string
	Summary   string // "" = null
	Order     int
	Hidden    bool // omitted from the list, llms.txt and the sitemap; still served by slug (not access control)
	UpdatedAt time.Time
	HTML      safehtml.SafeHTML
	TOC       []TOCEntry
	SHA       string // sha256 of the file, 16 hex characters
}

// Announcement is the banner: plain text plus a level; ID is a content hash.
type Announcement struct {
	ID          string
	Text        string
	Level       string
	Dismissible bool
}

// Set is the value of the pages.set global: immutable once published.
type Set struct {
	Pages        []Page // order, then slug
	Announcement *Announcement
	LoadedAt     time.Time
	Problems     []Problem
	Fingerprint  string
	Info         freshness.Info
}

// Page returns the page with the slug.
func (s Set) Page(slug string) (Page, bool) {
	for _, p := range s.Pages {
		if p.Slug == slug {
			return p, true
		}
	}
	return Page{}, false
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load reads dir through an os.Root (symlinks may point only inside the directory: a Kubernetes ConfigMap volume is a
// directory of symlinks into "..data/", all inside it), renders every <slug>.md and builds the Set. Files that fail
// are skipped with a Problem; the others are served. Load never fails because of one bad file; it fails (and the caller
// keeps the previous Set) only when dir itself cannot be read.
func Load(ctx context.Context, dir string, c Config, r Renderer, now time.Time) (Set, error) {
	set := Set{LoadedAt: now, Info: freshness.Info{State: freshness.StateOK, FetchedAt: now, CheckedAt: now}}
	set.Announcement = announcementOf(c.Announcement)
	if dir == "" {
		return set, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Set{}, fmt.Errorf("pages: open %s: %w", dir, err)
	}
	defer root.Close()
	ents, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return Set{}, fmt.Errorf("pages: read %s: %w", dir, err)
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	h := sha256.New()
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
			continue // README.txt, .git, ..data and friends are not pages
		}
		slug := strings.TrimSuffix(name, ".md")
		prob := func(code, msg string) { set.Problems = append(set.Problems, Problem{name, code, msg}) }
		if !slugRE.MatchString(slug) || len(slug) > 63 {
			prob("page.bad-slug", "file name must be a lower-case slug followed by .md")
			continue
		}
		if len(set.Pages) >= c.MaxPages {
			prob("page.over-cap", "more than max_pages files; ignored")
			continue
		}
		f, err := root.Open(name) // follows an in-directory symlink, refuses to leave the root
		if err != nil {
			prob("page.unreadable", "cannot open (outside the directory or unreadable)")
			continue
		}
		fi, serr := f.Stat()
		b, rerr := io.ReadAll(io.LimitReader(f, int64(c.MaxPageBytes)+1))
		f.Close()
		if serr != nil || rerr != nil || !fi.Mode().IsRegular() {
			prob("page.unreadable", "not a regular file")
			continue
		}
		if len(b) > c.MaxPageBytes {
			prob("page.too-large", fmt.Sprintf("larger than max_page_bytes (%d)", c.MaxPageBytes))
			continue
		}
		if err := ctx.Err(); err != nil {
			return Set{}, err
		}
		rp, err := r.RenderPage(ctx, b)
		if err != nil {
			prob("page.render-failed", "the renderer rejected the file")
			continue
		}
		sum := sha256.Sum256(b)
		p := Page{Slug: slug, Title: cleanText(rp.Title, 200), Summary: cleanText(rp.Description, 300), Hidden: rp.Hidden, UpdatedAt: fi.ModTime().UTC(),
			HTML: rp.HTML, TOC: rp.TOC, SHA: hex.EncodeToString(sum[:8])}
		if p.Title == "" {
			p.Title = slug
		}
		if rp.Order != nil {
			p.Order = *rp.Order
		}
		for _, pr := range rp.Problems {
			set.Problems = append(set.Problems, Problem{name, pr.Code, pr.Message})
		}
		set.Pages = append(set.Pages, p)
		h.Write([]byte(slug + "\x00" + p.SHA + "\x00"))
	}
	sort.SliceStable(set.Pages, func(i, j int) bool {
		if set.Pages[i].Order != set.Pages[j].Order {
			return set.Pages[i].Order < set.Pages[j].Order
		}
		return set.Pages[i].Slug < set.Pages[j].Slug
	})
	for _, pr := range set.Problems { // a new bad file name must reach /status even though no page changed (V-10)
		h.Write([]byte(pr.File + "\x00" + pr.Code + "\x00"))
	}
	if a := set.Announcement; a != nil {
		h.Write([]byte(a.ID))
	}
	set.Fingerprint = hex.EncodeToString(h.Sum(nil))[:16]
	return set, nil
}

func emptyFingerprint(a *Announcement) string {
	h := sha256.New()
	if a != nil {
		h.Write([]byte(a.ID))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func announcementOf(c AnnouncementConfig) *Announcement {
	if !c.Enabled {
		return nil
	}
	t := cleanText(c.Text, 500)
	if t == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(c.Level + "\x00" + t))
	return &Announcement{ID: hex.EncodeToString(sum[:4]), Text: t, Level: c.Level, Dismissible: c.Dismissible}
}

// cleanText collapses control characters and whitespace runs to one space and truncates to max runes.
func cleanText(s string, max int) string {
	var b strings.Builder
	sp, n := false, 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 || (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfeff || (r >= 0xe0000 && r <= 0xe007f) {
			r = ' '
		}
		if r == ' ' {
			if sp || b.Len() == 0 {
				continue
			}
			sp = true
		} else {
			sp = false
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// Deps are the constructor dependencies.
type Deps struct {
	Cfg      Config
	Renderer Renderer
	Pub      *readmodel.Publisher
	Global   readmodel.Global[Set] // registered by cmd/scp as GlobalName
	Meter    metric.Meter
	Tracer   trace.Tracer
	Log      *slog.Logger
}

// Service loads pages and serves the API. It starts no goroutine: Reload is called by a refresh.Task built in cmd/scp
// (TaskName, Every()), by the admin trigger and by SIGHUP.
type Service struct {
	d      Deps
	reload metric.Int64Counter
}

// New validates the dependencies and builds the Service.
func New(d Deps) (*Service, error) {
	errs := d.Cfg.Validate("pages")
	for n, isNil := range map[string]bool{"Renderer": d.Renderer == nil, "Pub": d.Pub == nil, "Log": d.Log == nil} {
		if isNil {
			errs = append(errs, fmt.Errorf("pages: Deps.%s is required", n))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if d.Meter == nil {
		d.Meter = noop.NewMeterProvider().Meter("pages")
	}
	if d.Tracer == nil {
		d.Tracer = nooptrace.NewTracerProvider().Tracer("pages")
	}
	c, err := d.Meter.Int64Counter(MetricReloadTotal)
	if err != nil {
		return nil, err
	}
	return &Service{d: d, reload: c}, nil
}

// Every is the cadence of the reload Task (0 = on demand only).
func (s *Service) Every() time.Duration { return s.d.Cfg.ReloadEvery }

// Reload loads the directory and publishes the Set. On a directory failure the previous Set stays and its Info becomes
// an error (stale-while-error); the error is returned for the Task's status.
func (s *Service) Reload(ctx context.Context) error {
	now := time.Now()
	set, err := Load(ctx, s.d.Cfg.Dir, s.d.Cfg, s.d.Renderer, now)
	if err != nil {
		s.reload.Add(ctx, 1, metric.WithAttributes(attrResult("error")))
		prev, ok := readmodel.GetGlobal(s.d.Pub.Current(), s.d.Global)
		if !ok { // first load failed: publish an empty set (the announcement is config and must still show) carrying the error
			prev = Set{Announcement: announcementOf(s.d.Cfg.Announcement), LoadedAt: now, Info: freshness.Info{FetchedAt: now}}
			prev.Fingerprint = emptyFingerprint(prev.Announcement)
		}
		prev.Info.CheckedAt, prev.Info.State, prev.Info.ErrCode, prev.Info.Attempts = now, freshness.StateError, "pages.reload_failed", prev.Info.Attempts+1
		_, _ = s.d.Pub.Apply(func(b *readmodel.Builder) error { return readmodel.PutGlobal(b, s.d.Global, prev) })
		return err
	}
	if prev, ok := readmodel.GetGlobal(s.d.Pub.Current(), s.d.Global); ok && prev.Fingerprint == set.Fingerprint && prev.Info.State == freshness.StateOK {
		s.reload.Add(ctx, 1, metric.WithAttributes(attrResult("unchanged")))
		return nil
	}
	if _, err := s.d.Pub.Apply(func(b *readmodel.Builder) error { return readmodel.PutGlobal(b, s.d.Global, set) }); err != nil {
		s.reload.Add(ctx, 1, metric.WithAttributes(attrResult("publish_failed")))
		return err
	}
	s.reload.Add(ctx, 1, metric.WithAttributes(attrResult("ok")))
	return nil
}

// Mount registers the generated strict server of api.pages and api.page.
func (s *Service) Mount(m httpapi.Mux) {}
