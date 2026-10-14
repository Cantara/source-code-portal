package ops

import (
	"net/http"
	"net/http/pprof" // init registers on http.DefaultServeMux, which this module never serves (G4): third-party init is outside G5
	"runtime"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// Apply sets the process-wide block and mutex profile rates. Called once by cmd/scp at startup (a global runtime
// side effect, which is why it is not done in a constructor).
func (c PprofConfig) Apply() {
	runtime.SetBlockProfileRate(c.BlockRate)
	runtime.SetMutexProfileFraction(c.MutexFraction)
}

// Route names of the pprof class (routes.Admin()); the map values are the handlers cmd/scp registers under them.
const (
	RoutePprofIndex      = "admin.pprof"
	RoutePprofCmdline    = "admin.pprof.cmdline"
	RoutePprofProfile    = "admin.pprof.profile"
	RoutePprofSymbol     = "admin.pprof.symbol"
	RoutePprofSymbolPost = "admin.pprof.symbol.post"
	RoutePprofTrace      = "admin.pprof.trace"
)

// PprofHandlers returns the pprof handlers by route name. Disabled: every handler answers 404 feature-disabled (the
// routes stay registered so routes.Table.Verify holds). index (named delta profiles), profile and trace reject ?seconds= above max_seconds with a 400
// problem (net/http/pprof itself extends the write deadline by that many seconds, verified in Go 1.27.1 source).
func PprofHandlers(cfg PprofConfig) map[string]http.Handler {
	wrap := func(h http.HandlerFunc, limited bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !cfg.Enabled {
				httpapi.WriteProblem(w, r, httpapi.Problem{Status: 404, Code: httpapi.CodeFeatureDisabled, Title: "Profiling disabled"})
				return
			}
			if limited {
				if v := r.URL.Query().Get("seconds"); v != "" {
					if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < 0 || time.Duration(n)*time.Second > cfg.MaxSeconds {
						httpapi.WriteProblem(w, r, httpapi.Problem{Status: 400, Code: httpapi.CodeValidationFailed, Title: "Invalid request",
							Errors: []httpapi.FieldError{{Pointer: "#/query/seconds", Detail: "must be an integer from 0 to " + strconv.Itoa(int(cfg.MaxSeconds/time.Second))}}})
						return
					}
				}
			}
			h(w, r)
		})
	}
	return map[string]http.Handler{
		RoutePprofIndex:      wrap(pprof.Index, true), // the index serves named profiles, and heap?seconds=N, goroutine?seconds=N ... block for N seconds too
		RoutePprofCmdline:    wrap(pprof.Cmdline, false),
		RoutePprofProfile:    wrap(pprof.Profile, true),
		RoutePprofSymbol:     wrap(pprof.Symbol, false),
		RoutePprofSymbolPost: wrap(pprof.Symbol, false),
		RoutePprofTrace:      wrap(pprof.Trace, true),
	}
}
