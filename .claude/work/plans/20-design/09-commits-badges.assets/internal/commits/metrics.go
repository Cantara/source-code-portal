package commits

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Instrument names (constants next to the instruments; the cmd/scp registry test compares them with the metrics
// reference).
const (
	MetricRefreshTotal   = "scp_commits_refresh_total" // {result}
	MetricDroppedTotal   = "scp_commits_dropped_total" // {reason}
	MetricClampedTotal   = "scp_commits_clamped_total" // future-dated commits clamped to now
	MetricEvictedTotal   = "scp_commits_evicted_total" // {reason}
	MetricPersistErrors  = "scp_commits_persist_errors_total"
	MetricRestoreTotal   = "scp_commits_restore_total"   // {result}
	MetricReactionsTotal = "scp_commits_reactions_total" // {result}
	MetricMergeSeconds   = "scp_commits_merge_seconds"   // {path}
	MetricRetained       = "scp_commits_retained"
)

type instruments struct {
	refreshC, droppedC, clampedC, evictedC, persistC, restoreC, reactC metric.Int64Counter
	mergeH                                                             metric.Float64Histogram
}

func newInstruments(d Deps) (instruments, error) {
	var in instruments
	var err error
	mk := func(name, desc string) metric.Int64Counter {
		if err != nil {
			return nil
		}
		var c metric.Int64Counter
		c, err = d.Meter.Int64Counter(name, metric.WithDescription(desc))
		return c
	}
	in.refreshC = mk(MetricRefreshTotal, "commits.refresh outcomes")
	in.droppedC = mk(MetricDroppedTotal, "commits rejected by validation")
	in.clampedC = mk(MetricClampedTotal, "future-dated commits clamped to the ingestion time")
	in.evictedC = mk(MetricEvictedTotal, "commits that left a repository ring")
	in.persistC = mk(MetricPersistErrors, "store writes of commits.refresh that failed")
	in.restoreC = mk(MetricRestoreTotal, "warm-start restore outcomes")
	in.reactC = mk(MetricReactionsTotal, "webhook and head-sweep reactions")
	if err != nil {
		return in, err
	}
	in.mergeH, err = d.Meter.Float64Histogram(MetricMergeSeconds, metric.WithUnit("s"), metric.WithDescription("duration of one commit feed page"))
	if err != nil {
		return in, err
	}
	_, err = d.Meter.Int64ObservableGauge(MetricRetained, metric.WithDescription("commits held in the current snapshot"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			snap := d.Pub.Current()
			n := 0
			for _, id := range snap.Repos() {
				if v, ok := readmodel.Get(snap, id, d.View); ok {
					n += len(v.Latest)
				}
			}
			o.Observe(int64(n))
			return nil
		}))
	return in, err
}

func (m instruments) refresh(ctx context.Context, result string) {
	m.refreshC.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
func (m instruments) dropped(ctx context.Context, st dropStats) {
	add := func(reason string, n int) {
		if n > 0 {
			m.droppedC.Add(ctx, int64(n), metric.WithAttributes(attribute.String("reason", reason)))
		}
	}
	add("no_date", st.NoDate)
	add("invalid_sha", st.InvalidSHA)
	add("no_url", st.NoURL)
	add("duplicate", st.Duplicate)
	if st.Clamped > 0 {
		m.clampedC.Add(ctx, int64(st.Clamped))
	}
}
func (m instruments) persistError(ctx context.Context) { m.persistC.Add(ctx, 1) }
func (m instruments) restoreResult(ctx context.Context, r string) {
	m.restoreC.Add(ctx, 1, metric.WithAttributes(attribute.String("result", r)))
}
func (m instruments) reaction(r string) {
	m.reactC.Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", r)))
}
func (m instruments) merge(ctx context.Context, path string, d time.Duration) {
	m.mergeH.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("path", path)))
}

// evicted classifies commits of the previous ring that are not in the new one: aged_out (older than the oldest new
// commit: the normal case) or rewritten (inside the new time range: force push, rebase or a changed default branch).
func (m instruments) evicted(ctx context.Context, prior, kept []scm.Commit) {
	if len(prior) == 0 || len(kept) == 0 {
		return
	}
	in := make(map[string]struct{}, len(kept))
	for _, c := range kept {
		in[c.SHA] = struct{}{}
	}
	oldest := kept[len(kept)-1].CommittedAt
	var aged, rewritten int64
	for _, c := range prior {
		if _, ok := in[c.SHA]; ok {
			continue
		}
		if c.CommittedAt.Before(oldest) {
			aged++
		} else {
			rewritten++
		}
	}
	if aged > 0 {
		m.evictedC.Add(ctx, aged, metric.WithAttributes(attribute.String("reason", "aged_out")))
	}
	if rewritten > 0 {
		m.evictedC.Add(ctx, rewritten, metric.WithAttributes(attribute.String("reason", "rewritten")))
	}
}

var _ = freshness.StateOK
