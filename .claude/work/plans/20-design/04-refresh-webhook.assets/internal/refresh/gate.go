package refresh

import (
	"time"

	"github.com/cantara/source-code-portal/internal/job"
)

// Gate is the consumer-defined budget gate (architecture 8.6). Admit returns 0 to start a job of
// priority p against upstream now, or the time to wait. It is called by the single dispatcher goroutine,
// never under an engine lock, at most once per (upstream, priority class) per dispatch pass, and must be
// cheap, non-blocking and must not call the engine.
//
// The reserve arithmetic (40/15/2 percent of the remaining primary budget kept for more urgent classes, ADR-015) is NOT here:
// cross-design review CD-CF-02 (X6, settled by unit 16 D-7) leaves it in the adapter (github.Client.Admit(ReserveClass),
// github.defaults.budget.reserve.*), and cmd/scp/internal/bridge.RateGate maps a job.Priority onto that class. This package
// therefore has no Budget, Reserves or NewReserveGate and no refresh.gate.* configuration.
type Gate interface {
	Admit(upstream string, p job.Priority) (wait time.Duration)
}
