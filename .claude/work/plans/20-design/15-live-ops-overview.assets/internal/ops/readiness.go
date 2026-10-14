package ops

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// Phase is the readiness state machine. Order matters: a latched phase never decreases except to ShuttingDown.
type Phase uint8

const (
	PhaseWarming Phase = iota
	PhaseDegraded
	PhaseReady
	PhaseShuttingDown
)

func (p Phase) String() string {
	switch p {
	case PhaseDegraded:
		return "degraded"
	case PhaseReady:
		return "ready"
	case PhaseShuttingDown:
		return "shutting-down"
	}
	return "warming"
}

// Gate names are the enum of the API (ReadinessGate.name).
const (
	GateStore           = "store"
	GateCatalogue       = "catalogue"
	GateCoverage        = "coverage"
	GateStartupDeadline = "startup_deadline"
)

type GateResult struct {
	Name   string
	OK     bool
	Detail string // admin form: with numbers; public form: generic
}

type CoverageResult struct {
	Have, Total int
	Ratio       float64
	Family      string // the family that is furthest from the threshold
}

// Report is one evaluation.
type Report struct {
	Phase      Phase
	Gates      []GateResult
	Coverage   CoverageResult
	ReadySince time.Time
}

// Readiness evaluates the gates of architecture 10.2 and latches the result.
type Readiness struct {
	cfg     ReadinessConfig
	cat     CatalogueSource
	snap    SnapshotSource
	kinds   map[string]Kind // by family
	started time.Time

	mu         sync.Mutex
	latched    Phase // PhaseWarming, PhaseDegraded or PhaseReady
	readySince time.Time
	memoSnap   *readmodel.Snapshot
	memoCov    CoverageResult
	shutting   atomic.Bool
}

// NewReadiness fails when a core kind is not registered.
func NewReadiness(cfg ReadinessConfig, cat CatalogueSource, snap SnapshotSource, kinds []Kind, started time.Time) (*Readiness, error) {
	if errs := cfg.Validate("readiness"); len(errs) > 0 {
		return nil, joinErrs(errs)
	}
	m := make(map[string]Kind, len(kinds))
	for _, k := range kinds {
		m[k.Family] = k
	}
	for _, f := range cfg.CoreKinds {
		if _, ok := m[f]; !ok {
			return nil, fmt.Errorf("readiness.core_kinds: %q is not a registered item family", f)
		}
	}
	return &Readiness{cfg: cfg, cat: cat, snap: snap, kinds: m, started: started}, nil
}

// SetShuttingDown is the first lifecycle Drain action: /readyz answers 503 from now on, /livez stays 200.
func (r *Readiness) SetShuttingDown() { r.shutting.Store(true) }

// DataPhase is what the snapshot document shows: never "shutting-down" (the document describes data).
func (r *Readiness) DataPhase() string {
	p := r.Evaluate(time.Now()).Phase
	if p == PhaseShuttingDown {
		r.mu.Lock()
		p = r.latched
		r.mu.Unlock()
	}
	return p.String()
}

// Phase is the current phase.
func (r *Readiness) Phase() Phase { return r.Evaluate(time.Now()).Phase }

// coverage returns the worst core family. Memoised per *Snapshot: Info.State is immutable data, so the result cannot
// change until the next publish (derived age plays no part in "has a result").
func (r *Readiness) coverage(s *readmodel.Snapshot, now time.Time) CoverageResult {
	r.mu.Lock()
	if r.memoSnap == s && s != nil {
		c := r.memoCov
		r.mu.Unlock()
		return c
	}
	r.mu.Unlock()
	worst := CoverageResult{Ratio: 2}
	for _, f := range r.cfg.CoreKinds {
		st := r.kinds[f].Stats(s, now)
		if rt := st.Ratio(); rt < worst.Ratio {
			worst = CoverageResult{Have: st.Have, Total: st.Items, Ratio: rt, Family: f}
		}
	}
	r.mu.Lock()
	r.memoSnap, r.memoCov = s, worst
	r.mu.Unlock()
	return worst
}

// Evaluate computes the gates and advances the latch. Pure apart from the latch; cost O(repositories) once per snapshot.
func (r *Readiness) Evaluate(now time.Time) Report {
	cat := r.cat.Catalogue()
	cov := r.coverage(r.snap.Current(), now)
	var gates []GateResult
	storeOK := cat.StoreOpened
	gates = append(gates, GateResult{GateStore, storeOK, ifs(storeOK, "store opened", "store not open")})
	warmOK := cat.Warm && !cat.WarmFetched.IsZero() && now.Sub(cat.WarmFetched) <= r.cfg.WarmMaxAge
	catOK := cat.Live || warmOK
	var catDetail string
	switch {
	case cat.Live:
		catDetail = "live catalog.reconcile succeeded"
	case warmOK:
		catDetail = fmt.Sprintf("warm catalogue confirmed %s ago (limit %s)", now.Sub(cat.WarmFetched).Truncate(time.Second), r.cfg.WarmMaxAge)
	case cat.Warm:
		catDetail = fmt.Sprintf("warm catalogue too old: %s (limit %s)", now.Sub(cat.WarmFetched).Truncate(time.Second), r.cfg.WarmMaxAge)
	default:
		catDetail = "no catalogue yet (cold start: waiting for the first catalog.reconcile)"
	}
	gates = append(gates, GateResult{GateCatalogue, catOK, catDetail})
	covOK := cov.Ratio >= r.cfg.MinCoverage
	gates = append(gates, GateResult{GateCoverage, covOK, fmt.Sprintf("coverage %.2f of %q (%d/%d), threshold %.2f", cov.Ratio, cov.Family, cov.Have, cov.Total, r.cfg.MinCoverage)})
	deadline := now.Sub(r.started) >= r.cfg.StartupDeadline

	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case storeOK && catOK && covOK:
		if r.latched != PhaseReady {
			r.latched = PhaseReady
			if r.readySince.IsZero() { // first time the portal served: a degraded start upgraded later keeps its date
				r.readySince = now
			}
		}
	case storeOK && catOK && deadline && r.latched == PhaseWarming:
		r.latched, r.readySince = PhaseDegraded, now
	}
	if r.latched == PhaseDegraded && !covOK {
		gates = append(gates, GateResult{GateStartupDeadline, true, fmt.Sprintf("ready by startup deadline %s with incomplete coverage", r.cfg.StartupDeadline)})
	}
	p := r.latched
	if r.shutting.Load() {
		p = PhaseShuttingDown
	}
	return Report{Phase: p, Gates: gates, Coverage: cov, ReadySince: r.readySince}
}

func ifs(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

// ReadyHandler serves /readyz. detail is true on the admin listener: the 503 problem then lists gate details with
// numbers; the public form lists gate names only (no counts: a public body must not reveal the catalogue size).
func (r *Readiness) ReadyHandler(detail bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rep := r.Evaluate(time.Now())
		w.Header().Set("Cache-Control", "no-store")
		if rep.Phase == PhaseReady || rep.Phase == PhaseDegraded {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "{\"status\":%q}\n", rep.Phase.String())
			return
		}
		p := httpapi.WithRetry(httpapi.New(http.StatusServiceUnavailable, httpapi.CodeNotReady, "the portal is warming up"), 10*time.Second)
		if rep.Phase == PhaseShuttingDown {
			p.Detail = "shutting down"
		}
		for _, g := range rep.Gates {
			if g.OK {
				continue
			}
			d := "gate not satisfied"
			if detail {
				d = g.Detail
			}
			p.Errors = append(p.Errors, httpapi.FieldError{Pointer: "#/gates/" + g.Name, Detail: d})
		}
		httpapi.WriteProblem(w, req, p)
	})
}
