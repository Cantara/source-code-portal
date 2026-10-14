package cluster_test

import (
	"context"
	"log/slog"
	"slices"
	gosync "sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/cluster"
	"github.com/cantara/source-code-portal/internal/cluster/clustertest"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// engine records what a member does to it.
type engine struct {
	warm chan struct{}

	mu       gosync.Mutex
	leads    bool
	received []sync.Shared
	records  []sync.Shared
	notified []string
}

func newEngine(records ...sync.Shared) *engine {
	e := &engine{warm: make(chan struct{}), records: records}
	close(e.warm)
	return e
}

func (e *engine) Warm() <-chan struct{} { return e.warm }
func (e *engine) Lead(on bool)          { e.mu.Lock(); e.leads = on; e.mu.Unlock() }
func (e *engine) Receive(_ context.Context, r sync.Shared) {
	e.mu.Lock()
	e.received = append(e.received, r)
	e.mu.Unlock()
}

// Records returns the configured records and the received ones, as an engine that applied them would.
func (e *engine) Records() []sync.Shared {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append(slices.Clone(e.records), e.received...)
}
func (e *engine) Notify(_ sync.Event, repo snapshot.RepoKey) {
	e.mu.Lock()
	e.notified = append(e.notified, string(repo))
	e.mu.Unlock()
}
func (e *engine) NotifySource(_ sync.Event, source string) {
	e.mu.Lock()
	e.notified = append(e.notified, "source:"+source)
	e.mu.Unlock()
}

func (e *engine) state() (leads bool, received []sync.Shared, notified []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.leads, slices.Clone(e.received), slices.Clone(e.notified)
}

// node is a running member with its engine.
type node struct {
	m      *cluster.Member
	e      *engine
	cancel context.CancelFunc
	done   chan error
}

func start(t *testing.T, hub *clustertest.Hub, e *engine) *node {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n := &node{m: cluster.New(hub.Dial(), slog.New(slog.DiscardHandler)), e: e, cancel: cancel, done: make(chan error, 1)}
	go func() { n.done <- n.m.Run(ctx, e) }()
	synctest.Wait()
	return n
}

func (n *node) stop(t *testing.T) {
	t.Helper()
	n.cancel()
	if err := <-n.done; err != nil {
		t.Error(err)
	}
}

func shared(rev int64, key string, catalogue ...store.Repo) sync.Shared {
	repo := snapshot.RepoKey("")
	kind := "list"
	if catalogue == nil {
		kind, repo = "commits", "acme/a"
	}
	return sync.Shared{Rev: rev, Source: "gh", Catalogue: catalogue,
		Item: store.Item{Key: key, Kind: kind, Repo: repo, State: snapshot.StateOK, CheckedAt: time.Now(), Payload: []byte(`{"v":1}`)}}
}

var repoA = store.Repo{Key: "acme/a", ID: snapshot.RepoID{Source: "gh", GitHubID: 1}} //nolint:gochecknoglobals // test fixture

func TestOneLeaderAndFailover(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := clustertest.NewHub()
		a, b := start(t, hub, newEngine()), start(t, hub, newEngine())
		time.Sleep(3 * time.Second)
		la, _, _ := a.e.state()
		lb, _, _ := b.e.state()
		if la == lb || a.m.Leader() != la || b.m.Leader() != lb {
			t.Fatalf("leaders: a %v, b %v", la, lb)
		}
		leader, other := a, b
		if lb {
			leader, other = b, a
		}
		leader.stop(t)
		time.Sleep(3 * time.Second)
		if l, _, _ := other.e.state(); !l || !hub.Members()[slices.IndexFunc([]*node{a, b}, func(n *node) bool { return n == other })].Leads() {
			t.Errorf("the other instance does not lead after the leader stopped")
		}
		other.stop(t)
	})
}

func TestLostConnectionStepsDownAndReloads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := clustertest.NewHub()
		a := start(t, hub, newEngine())
		time.Sleep(3 * time.Second)
		b := start(t, hub, newEngine())
		hub.Set("commits/acme/a", mustEncode(t, shared(7, "commits/acme/a")))
		ba := hub.Members()[0]
		ba.Disconnect()
		synctest.Wait()
		if l, _, _ := a.e.state(); l || a.m.Leader() || a.m.Connected() {
			t.Fatalf("a still leads or reports a connection after losing it")
		}
		time.Sleep(3 * time.Second)
		if l, _, _ := b.e.state(); !l {
			t.Errorf("b does not lead after a lost its connection")
		}
		if _, err := a.m.First(context.Background(), "x"); err == nil {
			t.Errorf("First without a connection succeeded")
		}
		ba.Reconnect()
		time.Sleep(3 * time.Second)
		_, got, _ := a.e.state()
		if len(got) < 2 || got[len(got)-1].Rev != 7 {
			t.Errorf("a did not reload the state after reconnecting: %+v", got)
		}
		if l, _, _ := a.e.state(); l {
			t.Errorf("a leads again while b holds the lock")
		}
		a.stop(t)
		b.stop(t)
	})
}

func TestShareJoinAndSeed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := clustertest.NewHub()
		hub.Set("commits/acme/old", []byte("left by an old leader; no engine has it"))
		// The first instance leads with outcomes from its store: the map gains them and loses the key it has no outcome for.
		a := start(t, hub, newEngine(shared(2, "list/gh", repoA), shared(3, "commits/acme/a")))
		time.Sleep(3 * time.Second)
		if st := hub.State(); len(st) != 2 || st["list/gh"] == nil || st["commits/acme/a"] == nil {
			t.Fatalf("state after seeding: %v", keys(st))
		}

		// A joining instance applies the map, the listing first.
		b := start(t, hub, newEngine())
		_, got, _ := b.e.state()
		if len(got) < 2 || got[0].Item.Key != "list/gh" || got[0].Catalogue[0] != repoA || got[1].Item.Key != "commits/acme/a" {
			t.Fatalf("joiner received %+v", got)
		}

		// Shared outcomes reach the other instance; removed keys leave the map; a failed write is retried.
		hub.FailPuts(1)
		next := shared(4, "commits/acme/a")
		next.Removed = []string{"commits/acme/b"}
		hub.Set("commits/acme/b", mustEncode(t, shared(1, "commits/acme/b")))
		a.m.Share(next)
		time.Sleep(2 * time.Second)
		if st := hub.State(); st["commits/acme/b"] != nil || string(st["commits/acme/a"]) != string(mustEncode(t, next)) {
			t.Errorf("state after share: %v", keys(st))
		}
		if _, got, _ := b.e.state(); got[len(got)-1].Rev != 4 {
			t.Errorf("the other instance did not receive the outcome: %+v", got[len(got)-1])
		}
		a.stop(t)
		b.stop(t)
	})
}

func TestDeliveriesAndEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := clustertest.NewHub()
		a, b := start(t, hub, newEngine()), start(t, hub, newEngine())
		ctx := context.Background()
		if first, err := a.m.First(ctx, "d1"); !first || err != nil {
			t.Errorf("first delivery: %v %v", first, err)
		}
		if first, err := b.m.First(ctx, "d1"); first || err != nil {
			t.Errorf("redelivery to the other instance: %v %v", first, err)
		}
		if err := b.m.Event(ctx, sync.EventPush, "acme/a", ""); err != nil {
			t.Fatal(err)
		}
		if err := a.m.Event(ctx, sync.EventSourceChanged, "", "gh"); err != nil {
			t.Fatal(err)
		}
		for _, n := range []*node{a, b} {
			if _, _, got := n.e.state(); !slices.Equal(got, []string{"acme/a", "source:gh"}) {
				t.Errorf("notified %v", got)
			}
		}
		a.stop(t)
		b.stop(t)
	})
}

func TestRecordRoundTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := clustertest.NewHub()
		empty := shared(9, "list/gh")
		empty.Catalogue = []store.Repo{}
		empty.Item.Kind, empty.Item.Repo = "list", ""
		hub.Set("list/gh", mustEncode(t, empty))
		hub.Set("bad", []byte("{"))
		n := start(t, hub, newEngine())
		_, got, _ := n.e.state()
		if len(got) != 1 || got[0].Catalogue == nil || len(got[0].Catalogue) != 0 || got[0].Rev != 9 {
			t.Errorf("an empty catalogue did not survive: %+v", got)
		}
		n.stop(t)
	})
}

// mustEncode encodes r the way a member writes it.
func mustEncode(t *testing.T, r sync.Shared) []byte {
	t.Helper()
	return cluster.Encode(r)
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
