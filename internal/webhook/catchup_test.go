package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/sync"
)

func TestCatchUpPassesOnWhatTheReceiverMissed(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	log := []Delivery{ // newest first, as GitHub lists them
		{GUID: "g5", Event: "issues", RepositoryID: 1, At: now.Add(-1 * time.Minute)},                 // never answered
		{GUID: "g4", Event: "pull_request", RepositoryID: 1, At: now.Add(-2 * time.Minute), OK: true}, // received
		{GUID: "g3", Event: "push", RepositoryID: 2, At: now.Add(-3 * time.Minute)},                   // failed
		{GUID: "g2", Event: "issues", RepositoryID: 99, At: now.Add(-4 * time.Minute)},                // a repository the portal does not know
		{GUID: "g1", Event: "installation", At: now.Add(-5 * time.Minute)},
	}
	type got struct {
		ev          sync.Event
		owner, name string
	}
	var passed []got
	var observed []string
	var last time.Time
	c := &CatchUp{
		Read: func(_ context.Context, since time.Time) ([]Delivery, error) {
			if want := now.Add(-catchUpFirst); !since.Equal(want) {
				t.Errorf("the first read went back to %v, want %v", since, want)
			}
			return log, nil
		},
		Resolve: func(id int64) (string, string, bool) {
			return "acme", map[int64]string{1: "one", 2: "two"}[id], id == 1 || id == 2
		},
		Sink:    func(ev sync.Event, o, n string) error { passed = append(passed, got{ev, o, n}); return nil },
		Last:    func(context.Context) time.Time { return last },
		SetLast: func(_ context.Context, at time.Time) { last = at },
		Observe: func(event, result string) { observed = append(observed, event+":"+result) },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:     func() time.Time { return now },
	}
	n, err := c.Once(t.Context())
	want := []got{{sync.EventPush, "acme", "two"}, {sync.EventTag, "acme", "two"}, {"issues", "acme", "one"}}
	if err != nil || n != 2 || !slices.Equal(passed, want) {
		t.Fatalf("passed %d %v (err %v), want the failed push and the unanswered issue, oldest first: %v", n, passed, err, want)
	}
	if !last.Equal(now) || len(observed) != 2 {
		t.Errorf("last %v, observed %v", last, observed)
	}

	// The next read starts before the last one ended; what was passed on is not passed on again.
	passed = nil
	c.Read = func(_ context.Context, since time.Time) ([]Delivery, error) {
		if want := now.Add(-catchUpOverlap); !since.Equal(want) {
			t.Errorf("the next read went back to %v, want %v", since, want)
		}
		return log, nil
	}
	if n, err := c.Once(t.Context()); err != nil || n != 0 || len(passed) != 0 {
		t.Errorf("a second read passed on %d %v (err %v)", n, passed, err)
	}
}

// A read that fails keeps the time of the last complete one: nothing is skipped, and a sink that refuses a delivery gets it again.
func TestCatchUpKeepsWhatItCouldNotDo(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var last time.Time
	refuse := true
	var passed int
	log := []Delivery{{GUID: "g1", Event: "issues", RepositoryID: 1, At: now.Add(-time.Minute)}}
	c := &CatchUp{
		Read:    func(context.Context, time.Time) ([]Delivery, error) { return log, nil },
		Resolve: func(int64) (string, string, bool) { return "acme", "one", true },
		Sink: func(sync.Event, string, string) error {
			if refuse {
				return errors.New("disk full")
			}
			passed++
			return nil
		},
		Last: func(context.Context) time.Time { return last }, SetLast: func(_ context.Context, at time.Time) { last = at },
		Observe: func(string, string) {}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now },
	}
	if n, _ := c.Once(t.Context()); n != 0 || passed != 0 {
		t.Fatalf("a refused delivery counted: %d", n)
	}
	refuse = false
	if n, _ := c.Once(t.Context()); n != 1 || passed != 1 {
		t.Errorf("the refused delivery was not tried again: %d", n)
	}
	c.Read = func(context.Context, time.Time) ([]Delivery, error) { return nil, errors.New("502") }
	before := last
	if _, err := c.Once(t.Context()); err == nil || !last.Equal(before) {
		t.Errorf("a failed read moved the cursor: %v then %v", before, last)
	}
}
