package ops

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

func joinErrs(errs []error) error { return errors.Join(errs...) }

// LiveHandler serves /livez: process-local and outcome-based (GR-41). It fails when the dispatcher heartbeat is
// older than liveness.heartbeat_max or when some upstream has had every concurrency slot held by a stuck job for longer
// than liveness.stuck_max. A paused or saturated upstream, an unhealthy store and shutdown are NOT liveness failures.
// It reads EngineView only: no outbound call, no lock held across anything slow.
func LiveHandler(cfg LivenessConfig, eng EngineSource, started time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason := livenessFailure(cfg, eng.EngineView(), started, time.Now()); reason != "" {
			httpapi.WriteProblem(w, r, httpapi.Problem{Status: http.StatusServiceUnavailable, Code: httpapi.CodeUnhealthy, Title: "Unhealthy", Detail: reason})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
}

func livenessFailure(cfg LivenessConfig, ev EngineView, started, now time.Time) string {
	switch {
	case ev.Heartbeat.IsZero():
		if now.Sub(started) > cfg.StartupGrace {
			return "dispatcher has not started"
		}
	case now.Sub(ev.Heartbeat) > cfg.HeartbeatMax:
		return fmt.Sprintf("dispatcher heartbeat is %s old", now.Sub(ev.Heartbeat).Truncate(time.Second))
	}
	for _, u := range ev.Upstreams {
		if !u.AllStuckSince.IsZero() && now.Sub(u.AllStuckSince) > cfg.StuckMax {
			return fmt.Sprintf("every slot of upstream %q has been stuck for %s", u.Name, now.Sub(u.AllStuckSince).Truncate(time.Second))
		}
	}
	return ""
}
