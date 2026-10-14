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

// Releases reads the latest release; *github.Source implements it. nil, nil is no release.
type Releases interface {
	LatestRelease(ctx context.Context, owner, name string) (*snapshot.Release, error)
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
	var rel *snapshot.Release
	if it.State == snapshot.StateOK {
		rel = &snapshot.Release{}
		if json.Unmarshal(it.Payload, rel) != nil {
			rel = nil
		}
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { e.Release, e.ReleaseFresh = rel, fresh })
}

// Handler serves GET /badges/repos/{owner}/{name}/{kind}.svg (last-commit, release, license, issues) and
// GET /badges/groups/{group}/repos.svg from the current snapshot.
func Handler(current func() *snapshot.Snapshot) http.Handler {
	h := handler{current: current, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /badges/repos/{owner}/{name}/{file}", h.repo)
	mux.HandleFunc("GET /badges/groups/{group}/{file}", h.group)
	mux.HandleFunc("/badges/", func(w http.ResponseWriter, r *http.Request) { h.write(w, r, unknown(""), http.StatusNotFound, false) })
	return mux
}

type handler struct {
	current func() *snapshot.Snapshot
	now     func() time.Time
}

func unknown(label string) badge {
	if label == "" {
		label = "badge"
	}
	return badge{label: label, message: "unknown", color: neutralColor}
}

func (h handler) repo(w http.ResponseWriter, r *http.Request) {
	s := h.current()
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
		switch {
		case len(e.Commits) > 0:
			msg, days := relativeDay(h.now(), e.Commits[0].CommittedAt)
			b.message, b.color = msg, ageColor(days)
		case e.CommitsFresh.State == snapshot.StateOK || e.CommitsFresh.State == snapshot.StateNoContent ||
			e.CommitsFresh.State == snapshot.StateNotFound:
			b.message, b.color, data = "none", neutralColor, false
		default:
			b, data = unknown(b.label), false
		}
	case "release":
		b.label = "release"
		switch {
		case e.Release != nil:
			b.message, b.color = e.Release.Tag, infoColor
		case e.ReleaseFresh.State == snapshot.StateNotFound:
			b.message, b.color, data = "none", neutralColor, false
		default:
			b, data = unknown(b.label), false
		}
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
	s := h.current()
	if !h.ready(w, r, s) {
		return
	}
	g, ok := s.Group(r.PathValue("group"))
	if !ok || r.PathValue("file") != "repos.svg" {
		h.write(w, r, unknown(""), http.StatusNotFound, false)
		return
	}
	h.write(w, r, badge{label: "repos", message: formatCount(len(g.Members)), color: infoColor}, http.StatusOK, true)
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
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
