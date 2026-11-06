package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/webhook"
)

// inboxKeep is how long a webhook event stays parked after it was handed to the engine: long enough for the jobs it asked for to have
// run. Past it the row is dropped; a restart before then reads the repository again, which costs a conditional request.
const inboxKeep = 10 * time.Minute

// parking is the part of the store the inbox keeps its events in.
type parking interface {
	Park(ctx context.Context, id, value string) error
	Parked(ctx context.Context) (map[string]string, error)
	Unpark(ctx context.Context, ids []string) error
}

// inbox makes a webhook event survive the process. The engine's schedule lives in memory until a job has run, so an event that was
// only passed on is lost with a crash or a restart (every update restarts the portal) and the repository waits for its poll. The inbox
// writes each event to the store before the delivery is acknowledged, passes it on, and after a start passes on what is still there.
type inbox struct {
	park    parking
	deliver func(ev sync.Event, repo snapshot.RepoKey, source string)
	log     *slog.Logger
	now     func() time.Time
	// catchUps read the delivery logs of the Apps (internal/webhook): what GitHub sent while the portal could not take it.
	catchUps []*webhook.CatchUp
}

// parkedEvent is an event as the store keeps it.
type parkedEvent struct {
	Event  sync.Event       `json:"event"`
	Repo   snapshot.RepoKey `json:"repo,omitempty"`
	Source string           `json:"source,omitempty"`
	At     time.Time        `json:"at"`
}

// take parks the event, then passes it on whatever came of parking: the error is the caller's to answer, the event is acted on anyway.
func (b *inbox) take(ev sync.Event, repo snapshot.RepoKey, source string) error {
	at := b.now()
	value, err := json.Marshal(parkedEvent{Event: ev, Repo: repo, Source: source, At: at.UTC()})
	if err == nil {
		var salt [6]byte
		_, _ = rand.Read(salt[:])
		err = b.park.Park(context.WithoutCancel(context.Background()), strconv.FormatInt(at.UnixNano(), 36)+"-"+hex.EncodeToString(salt[:]),
			string(value))
	}
	b.deliver(ev, repo, source)
	return err
}

// replay passes on every parked event once, the newest of identical ones.
func (b *inbox) replay(ctx context.Context) []string {
	rows, err := b.park.Parked(ctx)
	if err != nil {
		b.log.Warn("parked webhook events not read", "err", err)
		return nil
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	slices.Sort(ids) // ids start with the time they were parked at
	done := map[parkedEvent]bool{}
	for _, id := range ids {
		var p parkedEvent
		if json.Unmarshal([]byte(rows[id]), &p) != nil {
			continue
		}
		p.At = time.Time{}
		if done[p] {
			continue
		}
		done[p] = true
		b.deliver(p.Event, p.Repo, p.Source)
	}
	if len(ids) > 0 {
		b.log.Info("webhook events parked before the start passed on", "events", len(done))
	}
	return ids
}

// sweep drops the parked events older than inboxKeep.
func (b *inbox) sweep(ctx context.Context) {
	rows, err := b.park.Parked(ctx)
	if err != nil {
		return
	}
	var old []string
	for id, v := range rows {
		var p parkedEvent
		if json.Unmarshal([]byte(v), &p) != nil || b.now().Sub(p.At) > inboxKeep {
			old = append(old, id)
		}
	}
	if err := b.park.Unpark(ctx, old); err != nil {
		b.log.Warn("parked webhook events not dropped", "err", err)
	}
}

// run passes on what the last run left parked once the engine has its repositories (warm), then keeps the inbox short until ctx ends.
func (b *inbox) run(ctx context.Context, warm <-chan struct{}) {
	var wg gosync.WaitGroup
	defer wg.Wait()
	for _, c := range b.catchUps {
		wg.Go(func() { c.Run(ctx, warm) })
	}
	select {
	case <-warm:
	case <-ctx.Done():
		return
	}
	b.replay(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweep(ctx)
		}
	}
}

// catchUp reads one App source's delivery log. Resolve finds a repository of the source by its GitHub id in the snapshot; the time of
// the last complete read is kept in the store.
func catchUp(src *github.Source, id string, st interface {
	Meta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error
}, current func() *snapshot.Snapshot, take webhook.Sink, observe func(event, result string), log *slog.Logger) *webhook.CatchUp {
	key := "webhook/caught-up/" + id
	return &webhook.CatchUp{
		Read: func(ctx context.Context, since time.Time) ([]webhook.Delivery, error) {
			got, err := src.Deliveries(ctx, since)
			out := make([]webhook.Delivery, 0, len(got))
			for _, d := range got {
				out = append(out, webhook.Delivery{GUID: d.GUID, Event: d.Event, Action: d.Action, RepositoryID: d.RepositoryID, At: d.At, OK: d.OK()})
			}
			return out, err
		},
		Resolve: func(rid int64) (string, string, bool) {
			for _, e := range current().Repos {
				if e.Repo.ID.Source == id && e.Repo.ID.GitHubID == rid {
					return e.Repo.Owner, e.Repo.Name, true
				}
			}
			return "", "", false
		},
		Sink: take,
		Last: func(ctx context.Context) time.Time {
			v, _ := st.Meta(ctx, key)
			at, _ := time.Parse(time.RFC3339, v)
			return at
		},
		SetLast: func(ctx context.Context, at time.Time) { _ = st.SetMeta(ctx, key, at.UTC().Format(time.RFC3339)) },
		Observe: observe, Logger: log, Now: time.Now,
	}
}
