package ops

import (
	"math"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/readmodel"
)

// KindStats is what one pass over a Kind's Infos yields.
type KindStats struct {
	Items       int // repositories enumerated
	Have        int // repositories with a definitive result (freshness.State.HasResult)
	Failing     int // Attempts > 0: the last attempt failed
	Oldest, P95 time.Duration
	HasAges     bool // false when no item has ever been fetched
	LastSuccess time.Time
	ByStatus    [4]int // indexed by freshness.Status (unknown, fresh, stale, error)
}

// Ratio is have/total, and 1 when there is nothing to cover (an empty catalogue is covered).
func (k KindStats) Ratio() float64 {
	if k.Items == 0 {
		return 1
	}
	return float64(k.Have) / float64(k.Items)
}

// Stats makes one pass. It allocates one slice of ages (one entry per fetched item).
func (k Kind) Stats(s *readmodel.Snapshot, now time.Time) KindStats {
	var st KindStats
	var ages []time.Duration
	for in := range k.Infos(s) {
		st.Items++
		if in.State.HasResult() {
			st.Have++
		}
		if in.Attempts > 0 {
			st.Failing++
		}
		st.ByStatus[in.Derive(now)]++
		if in.HasValue() {
			ages = append(ages, max(0, now.Sub(in.FetchedAt)))
			if in.FetchedAt.After(st.LastSuccess) {
				st.LastSuccess = in.FetchedAt
			}
		}
	}
	if len(ages) > 0 {
		slices.Sort(ages)
		st.HasAges = true
		st.Oldest = ages[len(ages)-1]
		st.P95 = ages[int(math.Ceil(0.95*float64(len(ages))))-1] // nearest rank
	}
	return st
}
