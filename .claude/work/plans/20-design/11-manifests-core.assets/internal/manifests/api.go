package manifests

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Route patterns and names of the eight JSON operations (contract: unit 07 api/openapi-public.yaml, tag manifests; names: unit 01 routes table).
const (
	RouteRepoManifests = "GET /api/v1/repos/{repo}/manifests"
	RouteRepoDeps      = "GET /api/v1/repos/{repo}/dependencies"
	RouteRepoSBOM      = "GET /api/v1/repos/{repo}/sbom.cdx.json"
	RouteGraph         = "GET /api/v1/dependencies/graph"
	RouteVersions      = "GET /api/v1/dependencies/versions"
	RouteLicences      = "GET /api/v1/dependencies/licenses"
	RouteComponent     = "GET /api/v1/components"
	RouteDependents    = "GET /api/v1/components/dependents"
)

// Routes maps every pattern this feature registers to its route name; the equality test compares it with the route table and the OpenAPI document.
var Routes = map[string]string{
	RouteRepoManifests: "api.manifests.repo",
	RouteRepoDeps:      "api.manifests.deps",
	RouteRepoSBOM:      "api.manifests.sbom",
	RouteGraph:         "api.manifests.graph",
	RouteVersions:      "api.manifests.versions",
	RouteLicences:      "api.manifests.licenses",
	RouteComponent:     "api.manifests.component",
	RouteDependents:    "api.manifests.dependents",
}

// Mount satisfies httpapi.Mounter. The patterns are exactly what oapi-codegen's HandlerWithOptions registers; in the repository the body is
// replaced by that call over a StrictServerInterface whose methods are the build* functions below (parameters arrive typed, the strict adapter
// writes the response). The hand-written adapters here keep the scratch module self-contained and are what the tests drive.
func (s *Service) Mount(m httpapi.Mux) {
	m.HandleFunc(RouteRepoManifests, s.guard(s.handleRepoManifests))
	m.HandleFunc(RouteRepoDeps, s.guard(s.handleRepoDeps))
	m.HandleFunc(RouteRepoSBOM, s.guard(s.handleSBOM))
	m.HandleFunc(RouteGraph, s.guard(s.handleGraph))
	m.HandleFunc(RouteVersions, s.guard(s.handleVersions))
	m.HandleFunc(RouteLicences, s.guard(s.handleLicences))
	m.HandleFunc(RouteComponent, s.guard(s.handleComponent))
	m.HandleFunc(RouteDependents, s.guard(s.handleDependents))
}

// guard answers 404 feature-disabled for every operation while manifests.enabled is false (contract section 4).
func (s *Service) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Enabled {
			httpapi.WriteProblem(w, r, httpapi.New(404, "feature-disabled", "Package manifest intelligence is switched off (manifests.enabled)."))
			return
		}
		h(w, r)
	}
}

// ---- request context -------------------------------------------------------------------------------------------------------------

// view is what one request reads: one snapshot (taken once, architecture rule), the caller's view class and that class's graph variant.
type view struct {
	snap  *readmodel.Snapshot
	class scm.ViewClass
	graph *Graph
	now   time.Time
}

func (s *Service) view(r *http.Request) view {
	snap := s.d.Publisher.Current()
	class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
	v := view{snap: snap, class: class, now: time.Now()}
	if g, ok := readmodel.GetGlobal(snap, s.d.Slots.Graph); ok {
		v.graph = g.For(class)
	}
	return v
}

func classOrd(c scm.ViewClass) int {
	if c == scm.ViewAll {
		return 1
	}
	return 0
}

func (s *Service) reply(w http.ResponseWriter, r *http.Request, p httpapi.Problem) {
	httpapi.WriteProblem(w, r, p)
}

func notReady() httpapi.Problem { return httpapi.NotReady(5 * time.Second) }

// writeJSON writes a 200 with the validator, or a 304 without body when the caller already holds it. Cache-Control comes from httpapi.APIDefaults.
func (s *Service) writeJSON(w http.ResponseWriter, r *http.Request, etag string, v any) {
	if etag != "" {
		w.Header().Set("ETag", etag)
		if httpapi.NotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	httpapi.WriteJSON(w, r, http.StatusOK, v)
}

// ---- parameters -------------------------------------------------------------------------------------------------------------------

type params struct {
	q    url.Values
	errs []httpapi.FieldError
}

func (p *params) bad(name, detail string) {
	p.errs = append(p.errs, httpapi.FieldError{Pointer: "#/query/" + name, Detail: detail})
}

func (p *params) list(name string, max int, valid func(string) bool) []string {
	vs := p.q[name]
	if len(vs) > max {
		p.bad(name, fmt.Sprintf("at most %d values", max))
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v == "" || !valid(v) {
			p.bad(name, fmt.Sprintf("invalid value %q", CleanText(v, 40)))
			return nil
		}
		out = append(out, v)
	}
	return out
}

func (p *params) boolean(name string) *bool {
	v := p.q.Get(name)
	if v == "" {
		return nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil || (v != "true" && v != "false") {
		p.bad(name, "must be true or false")
		return nil
	}
	return &b
}

func (p *params) integer(name string, def, lo, hi int) int {
	v := p.q.Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		p.bad(name, fmt.Sprintf("must be an integer between %d and %d", lo, hi))
		return def
	}
	return n
}

func (p *params) text(name string, max int) string {
	v := p.q.Get(name)
	if len(v) > max {
		p.bad(name, fmt.Sprintf("at most %d characters", max))
		return ""
	}
	return v
}

func (p *params) problem() *httpapi.Problem {
	if len(p.errs) == 0 {
		return nil
	}
	pr := httpapi.Validation(p.errs...)
	return &pr
}

var scopeSet = map[string]Scope{"runtime": ScopeRuntime, "dev": ScopeDev, "test": ScopeTest, "build": ScopeBuild, "optional": ScopeOptional, "peer": ScopePeer, "provided": ScopeProvided, "unknown": ScopeUnknown}

func validScope(v string) bool { _, ok := scopeSet[v]; return ok }

func (p *params) scopes() map[Scope]bool {
	vs := p.list("scope", 8, validScope)
	if len(vs) == 0 {
		return nil
	}
	m := map[Scope]bool{}
	for _, v := range vs {
		m[scopeSet[v]] = true
	}
	return m
}

func (p *params) ecosystems() map[string]bool {
	vs := p.list("ecosystem", 20, func(v string) bool { return len(v) <= 32 && v == strings.ToLower(v) })
	if len(vs) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// splitUnitID parses "<repo slug>#<unit id>" (unit ids themselves contain '#').
func splitUnitID(id string) (scm.Slug, string, bool) {
	slug, unit, ok := strings.Cut(id, "#")
	return scm.Slug(slug), unit, ok && slug != "" && unit != ""
}

func unitID(slug scm.Slug, unit string) string { return string(slug) + "#" + unit }

// ---- freshness and validators -------------------------------------------------------------------------------------------------------

type fresh struct {
	view     openapi.Freshness
	statuses []string
}

var statusRank = map[freshness.Status]int{freshness.StatusFresh: 0, freshness.StatusUnknown: 1, freshness.StatusStale: 2, freshness.StatusError: 3}

// aggregate builds the contract's Freshness of a response from the items it contributes: status is the worst, the times the oldest.
func (s *Service) aggregate(now time.Time, kind string, items []freshness.Info, asOf func(i int) time.Time) fresh {
	out := fresh{statuses: make([]string, len(items))}
	v := httpapi.FreshnessView{Kind: kind, Status: freshness.StatusUnknown.String()}
	worst := freshness.StatusFresh
	first := true
	for i, in := range items {
		st := in.Derive(now)
		out.statuses[i] = st.String()
		if first || statusRank[st] > statusRank[worst] {
			worst = st
		}
		first = false
		older := func(dst *time.Time, t time.Time) {
			if !t.IsZero() && (dst.IsZero() || t.Before(*dst)) {
				*dst = t
			}
		}
		older(&v.FetchedAt, in.FetchedAt)
		older(&v.CheckedAt, in.CheckedAt)
		if asOf != nil {
			older(&v.AsOf, asOf(i))
		}
		if st == freshness.StatusStale || st == freshness.StatusError {
			if t, ok := in.StaleSince(); ok {
				older(&v.StaleSince, t)
			}
			if v.LastErrorCode == "" {
				v.LastErrorCode = in.ErrCode
			}
		}
	}
	if !first {
		v.Status = worst.String()
	}
	out.view = httpapi.NewFreshness(v)
	return out
}

func (f fresh) digest() string { return httpapi.FreshnessDigest(f.statuses...) }

// etagSnapshot is the validator of an operation whose body depends on more than one repository: snapshot version, view class, freshness digest.
func (s *Service) etagSnapshot(v view, f fresh) string {
	return httpapi.WithDigest(httpapi.WeakETag(v.snap.Version(), classOrd(v.class)), f.digest())
}

// etagRepo is the validator of a body that is a pure function of one repository entry plus the viewer's graph variant: getRepoManifests prints
// graph-derived counts (internal dependencies), and the graph changes without the repository's entry changing, so the graph build id is part of the digest.
func (s *Service) etagRepo(v view, slug scm.Slug, f fresh) string {
	return httpapi.WithDigest(httpapi.WeakETagOf(httpapi.ETagRepo, v.snap.Rev(readmodel.EntryID(slug)), classOrd(v.class)), httpapi.Digest(f.digest(), graphBuild(v.graph)))
}

// repoOf resolves {repo} with visibility applied (the only sanctioned resolver) and returns its facts, or writes the problem.
func (s *Service) repoOf(w http.ResponseWriter, r *http.Request, v view) (scm.Repository, freshness.Value[RepoFacts], bool) {
	repo, prob := authz.LookupRepo(r.Context(), v.snap, s.d.Slots.Repos, s.d.Authz, scm.Slug(r.PathValue("repo")))
	if prob != nil {
		s.reply(w, r, *prob)
		return repo, freshness.Value[RepoFacts]{}, false
	}
	f, ok := readmodel.Get(v.snap, readmodel.EntryID(repo.Slug), s.d.Slots.Facts)
	if !ok || !f.Info.HasValue() {
		s.reply(w, r, httpapi.NotFound("No manifest data has been collected for this repository yet."))
		return repo, f, false
	}
	return repo, f, true
}

// contributing lists the repositories visible to the class (optionally restricted to a group) that hold manifest facts, in snapshot order.
func (s *Service) contributing(v view, group map[scm.Slug]bool) (ids []readmodel.EntryID, infos []freshness.Info, parsed []time.Time) {
	for _, id := range v.snap.Repos() {
		repo, ok := readmodel.Get(v.snap, id, s.d.Slots.Repos)
		if !ok || !repo.V.VisibleTo(v.class) || (group != nil && !group[repo.V.Slug]) {
			continue
		}
		if f, ok := readmodel.Get(v.snap, id, s.d.Slots.Facts); ok && f.Info.HasValue() {
			ids, infos, parsed = append(ids, id), append(infos, f.Info), append(parsed, f.V.ParsedAt)
		}
	}
	return
}

// groupSet resolves ?group= to the member slugs visible to the class; ok=false when the group is unknown or has no visible member (404).
func (s *Service) groupSet(v view, group string) (set map[scm.Slug]bool, ok bool) {
	if group == "" {
		return nil, true
	}
	m, found := readmodel.GetGlobal(v.snap, s.d.Slots.Membership)
	if !found {
		return nil, false
	}
	slugs := m.VisibleReposOf(scm.GroupID(group), v.class)
	if len(slugs) == 0 {
		return nil, false
	}
	set = make(map[scm.Slug]bool, len(slugs))
	for _, sl := range slugs {
		set[sl] = true
	}
	return set, true
}

// purl renders the purl of a component for the API. The contract requires one on every dependency, artifact and component, so an ecosystem whose
// formatter says "no purl" (terraform, helm: unit 13 RR-1) is rendered with its ecosystem id as a custom purl type (the form getComponent accepts back).
// The SBOM does not use this function: it exports official purls only.
func (s *Service) purl(id ComponentID, display, version, qualifiers string) string {
	if p, ok := s.d.Registry.FormatPURL(id, display, version, qualifiers); ok && p != "" {
		return p
	}
	p, err := purl.Build(string(id.Ecosystem), id.Namespace, id.Name, version, qualifiers)
	if err != nil {
		return "pkg:" + purl.Type(string(id.Ecosystem)) + "/invalid" // unreachable for a sanitised model (names are non-empty); keeps the contract pattern
	}
	return p
}

func ptr[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func provenanceOf(src string) string {
	if src == "" {
		return "own"
	}
	return src
}

// versionStatus compares a consumer's version with the producer's current one in the component's ecosystem order.
func versionStatus(eco EcosystemID, consumer, producer string) string {
	if consumer == "" || producer == "" {
		return "unknown"
	}
	sys := version.Of(string(eco))
	if !version.Parsable(sys, consumer) || !version.Parsable(sys, producer) {
		return "unknown"
	}
	c, ok := version.Compare(sys, consumer, producer)
	switch {
	case !ok:
		return "unknown"
	case c < 0:
		return "behind"
	case c > 0:
		return "ahead"
	}
	return "current"
}

// latestOf is the highest version of a list in the ecosystem's order ("" when unordered or empty).
func latestOf(eco EcosystemID, vs []string) string {
	v, ok := version.Max(version.Of(string(eco)), vs)
	if !ok {
		return ""
	}
	return v
}

func sortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	ks := make([]K, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}
