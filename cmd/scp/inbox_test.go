package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/sync"
)

type memPark struct {
	rows map[string]string
	fail error
}

func (m *memPark) Park(_ context.Context, id, v string) error {
	if m.fail != nil {
		return m.fail
	}
	m.rows[id] = v
	return nil
}
func (m *memPark) Parked(context.Context) (map[string]string, error) { return maps.Clone(m.rows), nil }
func (m *memPark) Unpark(_ context.Context, ids []string) error {
	for _, id := range ids {
		delete(m.rows, id)
	}
	return nil
}

type got struct {
	ev     sync.Event
	repo   snapshot.RepoKey
	source string
}

func newInbox(p *memPark, now *time.Time, out *[]got) *inbox {
	return &inbox{park: p, handled: func(sync.Event, snapshot.RepoKey, string, time.Time) bool { return false }, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: func() time.Time { return *now },
		deliver: func(ev sync.Event, repo snapshot.RepoKey, source string) { *out = append(*out, got{ev, repo, source}) }}
}

// An event is parked and passed on; one that could not be parked is passed on all the same, and the error is the receiver's to answer.
func TestInboxTakesEvents(t *testing.T) {
	p := &memPark{rows: map[string]string{}}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var out []got
	b := newInbox(p, &now, &out)
	if err := b.take("issues", "acme/one", ""); err != nil || len(p.rows) != 1 || len(out) != 1 {
		t.Fatalf("take: err %v, parked %d, delivered %v", err, len(p.rows), out)
	}
	p.fail = errors.New("disk full")
	if err := b.take("pull_request", "acme/two", ""); err == nil || len(out) != 2 || out[1].ev != "pull_request" {
		t.Errorf("an event that could not be parked: err %v, delivered %v", err, out)
	}
}

// What a process left parked is passed on after its restart, once for identical events, unless the engine has acted on it already. The
// sweep drops what the engine has acted on, keeps the rest however long it waits, and drops it after inboxKeep.
func TestInboxReplaysAndSweeps(t *testing.T) {
	p := &memPark{rows: map[string]string{}}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var before []got
	old := newInbox(p, &now, &before)
	for _, e := range []struct {
		ev   sync.Event
		repo snapshot.RepoKey
	}{{"issues", "acme/one"}, {"issues", "acme/one"}, {"pull_request", "acme/two"}, {"issues", "acme/done"}} {
		now = now.Add(time.Second)
		if err := old.take(e.ev, e.repo, ""); err != nil {
			t.Fatal(err)
		}
	}
	var after []got // the restarted process, whose engine has already acted on acme/done
	b := newInbox(p, &now, &after)
	b.handled = func(_ sync.Event, repo snapshot.RepoKey, _ string, _ time.Time) bool { return repo == "acme/done" }
	b.replay(t.Context())
	want := []got{{"issues", "acme/one", ""}, {"pull_request", "acme/two", ""}}
	if !cmp.Equal(after, want, cmp.AllowUnexported(got{})) {
		t.Errorf("replayed %v, want %v", after, want)
	}
	b.sweep(t.Context())
	if len(p.rows) != 3 {
		t.Errorf("a sweep kept %d rows, want the 3 not acted on", len(p.rows))
	}
	now = now.Add(48 * time.Hour)
	b.sweep(t.Context())
	if len(p.rows) != 3 {
		t.Errorf("a sweep after two days kept %d rows: events not acted on must wait", len(p.rows))
	}
	now = now.Add(inboxKeep)
	b.sweep(t.Context())
	if len(p.rows) != 0 {
		t.Errorf("a sweep after %v left %d", inboxKeep, len(p.rows))
	}
}
