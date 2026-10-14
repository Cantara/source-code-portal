package badges

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Names of the routes in routes.Table that this package serves.
const (
	RouteBadge      = "badge"       // GET /badges/{repo}/{file}
	RouteBadgeGroup = "badge.group" // GET /badges/groups/{groupId}/{file}
)

const (
	maxSegment    = 200 // longest path value considered at all
	unknownMaxAge = 60 * time.Second
	securityCSP   = "default-src 'none'; sandbox"
)

type Deps struct {
	Cfg     Config
	Facts   Facts
	Pub     *readmodel.Publisher
	Repos   readmodel.Slot[freshness.Value[scm.Repository]]
	Members readmodel.Global[scm.Membership]
	Aliases readmodel.Global[map[scm.Slug]scm.Slug] // catalog.aliases: renamed slug -> current slug
	Authz   authz.Authorizer
	Enabled []Kind // manifest-based kinds served when manifests.enabled; build and security exist when RepoFacts carries a Fact
	Meter   metric.Meter
}

type Service struct {
	d       Deps
	palette Palette
	enabled map[Kind]bool
	badges  metric.Int64Counter
	cond    metric.Int64Counter
}

const (
	MetricBadgesTotal      = "scp_badges_total"             // {kind,state}
	MetricBadgesConditions = "scp_badges_conditional_total" // {result}
)

func New(d Deps) (*Service, error) {
	errs := d.Cfg.Validate("badges")
	for _, c := range []struct {
		name string
		nil_ bool
	}{{"Facts", d.Facts == nil}, {"Pub", d.Pub == nil}, {"Authz", d.Authz == nil}, {"Meter", d.Meter == nil}} {
		if c.nil_ {
			errs = append(errs, fmt.Errorf("badges.Deps.%s is required", c.name))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	pal, _ := parsePalette(d.Cfg.Palette)
	s := &Service{d: d, palette: pal, enabled: map[Kind]bool{}}
	for _, k := range d.Enabled {
		s.enabled[k] = true
	}
	var err error
	if s.badges, err = d.Meter.Int64Counter(MetricBadgesTotal, metric.WithDescription("badge responses by kind and state")); err != nil {
		return nil, err
	}
	if s.cond, err = d.Meter.Int64Counter(MetricBadgesConditions, metric.WithDescription("badge responses: 200 versus 304")); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) served(k Kind) bool { return !k.manifestKind() || s.enabled[k] }

// Repo serves route "badge": GET /badges/{repo}/{file}.
func (s *Service) Repo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind, ok := ParseFile(r.PathValue("file"))
		s.serveRepo(w, r, kind, ok, r.PathValue("repo"))
	})
}

// Group serves route "badge.group": GET /badges/groups/{groupId}/{file}; only "repos.svg" exists.
func (s *Service) Group() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, gid := r.PathValue("file"), r.PathValue("groupId")
		if file != "repos.svg" || len(gid) == 0 || len(gid) > maxSegment {
			s.notFound(w, r, KindRepos, file == "repos.svg")
			return
		}
		snap := s.d.Pub.Current()
		if snap == nil || snap.Version() == 0 {
			s.starting(w, r)
			return
		}
		mem, _ := readmodel.GetGlobal(snap, s.d.Members)
		g := scm.GroupID(gid)
		if _, found := mem.Group(g); !found {
			s.notFound(w, r, KindRepos, true)
			return
		}
		class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
		n := len(mem.VisibleReposOf(g, class)) // the count follows the caller's view class (A-23)
		s.respond(w, r, http.StatusOK, KindRepos, EvaluateRepos(n), class == scm.ViewAll)
	})
}

func (s *Service) serveRepo(w http.ResponseWriter, r *http.Request, kind Kind, kindOK bool, slugStr string) {
	if !kindOK || !s.served(kind) {
		s.notFound(w, r, kind, kindOK)
		return
	}
	if len(slugStr) == 0 || len(slugStr) > maxSegment {
		s.notFound(w, r, kind, true)
		return
	}
	snap := s.d.Pub.Current()
	if snap == nil || snap.Version() == 0 {
		s.starting(w, r)
		return
	}
	// GitHub names are case-insensitive and README embeds often carry the display case (Billing-Service), while slugs
	// are canonical lower-case ASCII (scm.ParseSlug): fold before the alias and the existence lookup.
	slug := scm.Slug(asciiLower(slugStr))
	if aliases, _ := readmodel.GetGlobal(snap, s.d.Aliases); aliases != nil {
		if cur, ok := aliases[slug]; ok {
			slug = cur // a renamed repository keeps serving its embedded badges (no redirect: Camo is unverified)
		}
	}
	ctx := r.Context()
	repo, prob := authz.LookupRepo(ctx, snap, s.d.Repos, s.d.Authz, slug)
	if prob != nil {
		s.notFound(w, r, kind, true) // identical for "does not exist" and "not visible to you"
		return
	}
	spec := unknown(kind)
	if f, ok := s.d.Facts.Facts(snap, slug); ok {
		if (kind == KindBuild && f.Build == nil) || (kind == KindSecurity && f.Security == nil) {
			s.notFound(w, r, kind, true) // a provider that is not configured: the kind does not exist in this deployment
			return
		}
		spec = Evaluate(kind, f, time.Now())
	}
	// a public repository's badge is the same for everybody; one that only some principals may see must not enter a shared cache
	s.respond(w, r, http.StatusOK, kind, spec, repo.Visibility != scm.VisPublic)
}

// asciiLower folds only ASCII letters, like scm.RepoRef.Key.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func (s *Service) render(sp Spec) Badge {
	return Badge{Label: sp.Label, Message: sp.Message, LabelColor: s.palette.Color(ToneLabel), MessageColor: s.palette.Color(sp.Tone)}
}

// notFound answers 404 with a small grey body so that an <img> still shows something. The body is the same for an
// unknown repository, an invisible one, a disabled kind and an unknown group, so it discloses nothing.
func (s *Service) notFound(w http.ResponseWriter, r *http.Request, kind Kind, kindOK bool) {
	sp := Spec{Label: "badge", Message: "unknown kind", Tone: ToneNeutral, State: StateUnknown}
	if kindOK {
		sp = unknown(kind)
	}
	label := "invalid"
	if kindOK {
		label = string(kind)
	}
	s.write(w, r, http.StatusNotFound, label, "not_found", s.render(sp), unknownMaxAge, false)
}

func (s *Service) starting(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "15")
	sp := Spec{Label: "badge", Message: "starting", Tone: ToneNeutral, State: StateUnknown}
	s.write(w, r, http.StatusServiceUnavailable, "invalid", "unavailable", s.render(sp), 0, false)
}

func (s *Service) respond(w http.ResponseWriter, r *http.Request, status int, kind Kind, sp Spec, private bool) {
	maxAge := s.d.Cfg.CacheMaxAge
	if sp.State == StateUnknown {
		maxAge = min(maxAge, unknownMaxAge)
	}
	s.write(w, r, status, string(kind), sp.State.String(), s.render(sp), maxAge, private)
}

// write renders, sets the fixed header set and answers the conditional request. The ETag is a hash of the body, not
// of the data generation: relative dates ("today") change without any data change, so a generation-based ETag would let
// a cache confirm a stale text with 304 indefinitely.
func (s *Service) write(w http.ResponseWriter, r *http.Request, status int, kind, state string, b Badge, maxAge time.Duration, private bool) {
	body := Render(b)
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	h := w.Header()
	h.Set("Content-Type", "image/svg+xml")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", securityCSP)
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if status == http.StatusOK {
		h.Set("ETag", etag)
	}
	h.Set("Cache-Control", cacheControl(status, maxAge, private))
	ctx := context.Background()
	s.badges.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind), attribute.String("state", state)))
	if status == http.StatusOK && httpapi.NotModified(r, etag) {
		s.cond.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "304")))
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.cond.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "200")))
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func cacheControl(status int, maxAge time.Duration, private bool) string {
	if status == http.StatusServiceUnavailable {
		return "no-store"
	}
	scope := "public"
	if private {
		scope = "private"
	}
	if maxAge <= 0 {
		return scope + ", no-cache"
	}
	return scope + ", max-age=" + strconv.Itoa(int(maxAge/time.Second))
}
