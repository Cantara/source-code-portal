package e2e

import (
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/refresh"
	"github.com/cantara/source-code-portal/internal/webhook"
)

// The wiring assertions cmd/scp/assert.go carries for this unit (design document section 2.4).
var (
	_ job.Enqueuer     = (*refresh.Engine)(nil)
	_ job.Enqueuer     = (*refresh.Planner)(nil)
	_ webhook.Sink     = fanFunc{}
	_ webhook.Verifier = ver{}
	_ refresh.Gate     = gateAssert{}
	_ refresh.PlanSink = (*planSinkAssert)(nil)
	_ webhook.Limiter  = (*limAssert)(nil)
)

type gateAssert struct{}

func (gateAssert) Admit(string, job.Priority) time.Duration { return 0 }

type planSinkAssert struct{}

func (*planSinkAssert) SaveState(job.Key, refresh.PlanState) {}

type limAssert struct{}

func (*limAssert) Allow(string) bool { return true }
