// Package badges serves local SVG badges for repositories and groups, and keeps the latest release of every repository.
package badges

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/health"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// ReleaseKind is the job kind that reads a repository's latest release.
const ReleaseKind = "badges.release"

const (
	releaseTTL    = 24 * time.Hour // tag pushes notify the job
	dataMaxAge    = 5 * time.Minute
	unknownMaxAge = time.Minute
	csp           = "default-src 'none'; sandbox"
)

// Release is a repository's latest published release.
type Release struct {
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
}

// facet is the latest release: nil when there is none or it is not known yet (the freshness tells which).
const facet snapshot.Facet[*Release] = "release"

// ReleaseOf is a repository's latest release (nil when there is none or it is not known yet) and its freshness.
func ReleaseOf(e *snapshot.RepoEntry) (*Release, snapshot.Freshness) { return facet.Get(e) }

// Repo adds the latest release to a repository summary.
func Repo(e *snapshot.RepoEntry, r *oapi.Repo) {
	if rel, _ := facet.Get(e); rel != nil {
		r.Release = &oapi.Release{Tag: rel.Tag, Url: rel.URL, PublishedAt: rel.Published.UTC()}
	}
}

// Badges is the badges feature.
type Badges struct {
	sources map[string]Releases
}

// New returns the feature for one Releases per source id.
func New(sources map[string]Releases) *Badges { return &Badges{sources: sources} }

// ReleaseSpec is the per-repository latest-release job.
func (b *Badges) ReleaseSpec() sync.Spec {
	return sync.Spec{Kind: ReleaseKind, Prio: sync.PrioScheduled, TTL: releaseTTL, On: []sync.Event{sync.EventTag}, Run: b.run,
		Publish: publish}
}

func (b *Badges) run(ctx context.Context, j sync.Job) (sync.Result, error) {
	src := b.sources[j.Source]
	if src == nil {
		return sync.Result{}, errors.New("badges: unknown source " + j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	rel, err := src.LatestRelease(ctx, owner, name)
	if err != nil {
		return sync.Result{}, err
	}
	state, payload := snapshot.StateNotFound, []byte(nil)
	if rel != nil {
		if payload, err = json.Marshal(rel); err != nil {
			return sync.Result{}, err
		}
		state = snapshot.StateOK
	}
	return sync.Result{State: state, Payload: payload, Unchanged: j.Prev.State == state && string(j.Prev.Payload) == string(payload)}, nil
}

func publish(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	var rel *Release
	if it.State == snapshot.StateOK {
		rel = &Release{}
		if json.Unmarshal(it.Payload, rel) != nil {
			rel = nil
		}
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { facet.Set(e, rel, fresh) })
}

// Data is what the badges read from other features, wired in cmd/scp.
type Data struct {
	// LastCommit is a repository's newest commit time (zero when there is none) and the freshness of its commits.
	LastCommit func(*snapshot.RepoEntry) (time.Time, snapshot.Freshness)
	// Health is a repository's health at now.
	Health func(*snapshot.RepoEntry, time.Time) health.Repo
	// Product is a group's weighed health at now.
	Product func(*snapshot.Snapshot, snapshot.Group, time.Time) health.Product
}

// Handler serves GET /badges/repos/{owner}/{name}/{kind}.svg (last-commit, release, license, issues, health) and
// GET /badges/groups/{group}/{kind}.svg (repos, health, last-commit, release) from the viewer's view of the current snapshot.
func Handler(current func(context.Context) *snapshot.Snapshot, d Data) http.Handler {
	h := handler{current: current, Data: d, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /badges/repos/{owner}/{name}/{file}", h.repo)
	mux.HandleFunc("GET /badges/groups/{group}/{file}", h.group)
	mux.HandleFunc("/badges/", func(w http.ResponseWriter, r *http.Request) { h.write(w, r, unknown(""), http.StatusNotFound, false) })
	return mux
}

type handler struct {
	Data
	current func(context.Context) *snapshot.Snapshot // the viewer's view
	now     func() time.Time
}

func unknown(label string) badge {
	if label == "" {
		label = "badge"
	}
	return badge{label: label, message: "unknown", color: neutralColor}
}

func (h handler) repo(w http.ResponseWriter, r *http.Request) {
	s := h.current(r.Context())
	if !h.ready(w, r, s) {
		return
	}
	kind, ok := strings.CutSuffix(r.PathValue("file"), ".svg")
	e, _ := s.Repo(snapshot.KeyOf(r.PathValue("owner"), r.PathValue("name")))
	if !ok || e == nil {
		h.write(w, r, unknown(""), http.StatusNotFound, false)
		return
	}
	var b badge
	data := true
	switch kind {
	case "last-commit":
		b.label = "last commit"
		at, fresh := h.LastCommit(e)
		b, data = h.age(at, read(fresh))
	case "release":
		rel, fresh := facet.Get(e)
		b, data = release(rel, fresh.State == snapshot.StateNotFound)
	case "health":
		r := h.Health(e, h.now())
		b, data = score(r.Counted, r.Score, r.Level)
	case "license":
		b.label = "license"
		switch l := e.Repo.License; l {
		case "":
			b.message, b.color, data = "none", neutralColor, false
		case "NOASSERTION", "OTHER":
			b.message, b.color = "other", infoColor
		default:
			b.message, b.color = l, infoColor
		}
	case "issues":
		b.label, b.message, b.color = "issues", formatCount(e.Repo.OpenIssues)+" open", infoColor
		if e.Repo.OpenIssues == 0 {
			b.color = successColor
		}
	default:
		h.write(w, r, unknown(""), http.StatusNotFound, false)
		return
	}
	h.write(w, r, b, http.StatusOK, data)
}

func (h handler) group(w http.ResponseWriter, r *http.Request) {
	s := h.current(r.Context())
	if !h.ready(w, r, s) {
		return
	}
	g, ok := s.Group(r.PathValue("group"))
	kind, svg := strings.CutSuffix(r.PathValue("file"), ".svg")
	if !ok || !svg {
		h.write(w, r, unknown(""), http.StatusNotFound, false)
		return
	}
	b, data := badge{}, true
	switch kind {
	case "repos":
		b = badge{label: "repos", message: formatCount(len(g.Members)), color: infoColor}
	case "health":
		p := h.Product(s, g, h.now())
		b, data = score(p.Counted > 0, p.Score, p.Level)
	case "last-commit":
		var newest time.Time
		known := true
		for _, k := range g.Members {
			if e := s.Repos[k]; e != nil {
				at, fresh := h.LastCommit(e)
				newest, known = maxTime(newest, at), known && read(fresh)
			}
		}
		b, data = h.age(newest, known)
		b.label = "last commit"
	case "release":
		b, data = release(groupRelease(s, g))
	default:
		h.write(w, r, unknown(""), http.StatusNotFound, false)
		return
	}
	h.write(w, r, b, http.StatusOK, data)
}

// read reports whether a fetch completed with an answer, content or none.
func read(f snapshot.Freshness) bool {
	return f.State == snapshot.StateOK || f.State == snapshot.StateNoContent || f.State == snapshot.StateNotFound
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// age is the last-commit badge for at; with no commit it says none when the commits were read, else unknown.
func (h handler) age(at time.Time, known bool) (badge, bool) {
	switch {
	case !at.IsZero():
		msg, days := relativeDay(h.now(), at)
		return badge{label: "last commit", message: msg, color: ageColor(days)}, true
	case known:
		return badge{label: "last commit", message: "none", color: neutralColor}, false
	}
	return unknown("last commit"), false
}

// release is the release badge; none says there is no release, else a missing one is unknown.
func release(rel *Release, none bool) (badge, bool) {
	switch {
	case rel != nil:
		return badge{label: "release", message: rel.Tag, color: infoColor}, true
	case none:
		return badge{label: "release", message: "none", color: neutralColor}, false
	}
	return unknown("release"), false
}

// groupRelease is the default repository's latest release, else the newest release among the members; none when every member is known
// to have none.
func groupRelease(s *snapshot.Snapshot, g snapshot.Group) (*Release, bool) {
	if e := s.Repos[g.DefaultRepo]; e != nil {
		if rel, _ := facet.Get(e); rel != nil {
			return rel, false
		}
	}
	var newest *Release
	none := true
	for _, k := range g.Members {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		rel, fresh := facet.Get(e)
		if rel != nil && (newest == nil || rel.Published.After(newest.Published)) {
			newest = rel
		}
		none = none && fresh.State == snapshot.StateNotFound
	}
	return newest, newest == nil && none
}

// score is the health badge: the score coloured by its level; without a score it says none.
func score(counted bool, n int, level health.Level) (badge, bool) {
	if !counted {
		return badge{label: "health", message: "none", color: neutralColor}, false
	}
	c := successColor
	switch level {
	case health.Watch:
		c = warningColor
	case health.Attention:
		c = dangerColor
	}
	return badge{label: "health", message: strconv.Itoa(n), color: c}, true
}

// ready answers 503 "starting" until a catalogue is loaded.
func (h handler) ready(w http.ResponseWriter, r *http.Request, s *snapshot.Snapshot) bool {
	if s.Catalogued {
		return true
	}
	w.Header().Set("Retry-After", "15")
	h.write(w, r, badge{label: "badge", message: "starting", color: neutralColor}, http.StatusServiceUnavailable, false)
	return false
}

// ageColor is green up to 90 days, blue up to a year, amber beyond.
func ageColor(days int) color {
	switch {
	case days <= 90:
		return successColor
	case days <= 365:
		return infoColor
	}
	return warningColor
}

// write sends the badge. Data badges carry a strong ETag of the body (an age message changes at midnight UTC without a data change)
// and answer 304 on a match; unknown and none badges are cached for a minute and carry no ETag; "starting" is not stored.
func (h handler) write(w http.ResponseWriter, r *http.Request, b badge, status int, data bool) {
	body := render(b)
	hd := w.Header()
	hd.Set("Content-Type", "image/svg+xml")
	hd.Set("Content-Security-Policy", csp)
	hd.Set("Cross-Origin-Resource-Policy", "cross-origin")
	switch {
	case status == http.StatusServiceUnavailable:
		hd.Set("Cache-Control", "no-store")
	case data:
		hd.Set("Cache-Control", "public, max-age="+strconv.Itoa(int(dataMaxAge.Seconds())))
	default:
		hd.Set("Cache-Control", "public, max-age="+strconv.Itoa(int(unknownMaxAge.Seconds())))
	}
	if data {
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:12]) + `"`
		hd.Set("ETag", etag)
		if httpapi.Matches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:gosec // G705: an SVG marshalled by encoding/xml, served with a sandbox CSP
	}
}
