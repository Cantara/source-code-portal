# Multiple instances
Status: v1 design note (M9), 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope
- `internal/cluster`: two or more instances on one Hazelcast cluster (`cluster.addresses`). One instance leads and runs the sync jobs;
  every instance serves the same data from its own snapshot and SQLite store.
- Shared through Hazelcast: the leader lock, the last outcome of every job key, webhook delivery de-duplication and webhook events.
- Without `cluster.addresses` nothing changes: the instance is alone and leads.

## Decisions
- One leader instead of shared job leases: only the leader calls GitHub, so each call is made once and the rate budget, ETags and App
  tokens stay in one process, as in a single instance. The work of one portal fits one process; the other instances serve and stand by.
- The leader lock is a Hazelcast map lock taken with `TryLock` every 2 s by the instances that do not hold it. Hazelcast releases it when the
  holder shuts down (about 10 s) or stops heartbeating (60 s, the server's `hazelcast.client.max.no.heartbeat.seconds`). An instance that
  loses its connection stops leading at once and takes part again after it reconnects. The CP subsystem (FencedLock) is not used: it is a
  Hazelcast Enterprise feature from 5.5 and needs three members.
- The state map holds, per job key, the last recorded outcome (the store item with its schedule; listing jobs add the source's catalogue)
  with a revision. The leader writes after each outcome through a coalescing queue (latest per key) in its own goroutine, so a slow
  Hazelcast never holds the dispatcher. Items of repositories that left are deleted.
- Every instance listens to the state map and applies what it receives exactly like a job outcome: store row, schedule and `Publish`.
  The same items through the same `Publish` give the same snapshot, so response bodies and their ETags are equal on every instance.
- Revisions: the leader stamps `max(now, last seen + 1)` in nanoseconds; an instance applies a record only when it is newer than what it
  has for that key, so echoes, replays and reordering are harmless. A new leader has seen every revision of the old one first.
- An item that arrives before the listing that admits its repository waits (per key, newest kept) and is applied when the catalogue
  admits it; a catalogue of the same source that does not list it drops it.
- Start: the engine warm-starts from the local store and serves; then the instance connects, subscribes, loads the whole state map,
  applies it and only then contends for the lock. On every reconnect it loads the map again (events may have been missed).
- On taking the lock the leader writes the outcomes the map lacks (a restarted or empty Hazelcast) and deletes keys it has no outcome
  for, so the map always mirrors the leader.
- A result that finishes after leadership was lost is dropped; the new leader runs the job again. Brief overlap during a network
  partition can make a call twice; the store is a cache and revisions settle it.
- Webhooks: delivery ids are de-duplicated with `PutIfAbsent` and a 15-minute TTL in a shared map, so a redelivery to another instance
  is a duplicate; an event is published on a Hazelcast topic and every instance notifies its engine, so the leader acts on deliveries
  received by any instance. When Hazelcast is unreachable the instance de-duplicates locally and notifies its own engine.
- Hazelcast down: instances keep serving their last good data and stay ready; no one leads, so nothing is refreshed until it is back.
  The client reconnects forever; startup never waits for it.
- Values are JSON. Hazelcast object names start with `cluster.namespace` (default `scp`), so several portals or test runs can share a
  cluster. Hazelcast OSS has no authentication or TLS: it belongs on a private network, like the admin listener.
- `internal/cluster` holds the member logic over a small `Backend` interface; `Hazelcast` implements it with
  `hazelcast-go-client` v1.6; `clustertest` implements it in memory for tests. The sync engine knows nothing of Hazelcast: it shares
  outcomes through a hook and receives them through a method.
- Followers report an empty queue; `/api/v1/status` shows the role; the gauge `scp_cluster_leader` is 1 on the leader.

## Shapes
```go
// internal/sync
type Shared struct { // one recorded outcome as instances share it
    Rev       int64
    Source    string
    Item      store.Item
    Catalogue []store.Repo // listing jobs: the source's repositories
    Removed   []string     // item keys that left with their repositories
}
func (e *Engine) Share(fn func(Shared))                   // before Run: outcomes are shared and the engine starts as a follower
func (e *Engine) Lead(on bool)                            // start or stop running jobs
func (e *Engine) Receive(ctx context.Context, r Shared)   // apply an outcome recorded by the leader
func (e *Engine) Records() []Shared                       // every recorded outcome, listings first (for seeding the map)
func (e *Engine) Warm() <-chan struct{}                   // closed after the warm start

// internal/cluster
type Backend interface {
    TryLead(ctx context.Context) (bool, error)
    Put(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Entries(ctx context.Context) (map[string][]byte, error)
    Watch(ctx context.Context, fn func(key string, value []byte)) error
    Once(ctx context.Context, id string, ttl time.Duration) (bool, error)
    Publish(ctx context.Context, msg []byte) error
    Subscribe(ctx context.Context, fn func(msg []byte)) error
    Connected() <-chan bool
    Close(ctx context.Context) error
}
func New(dial func(context.Context) (Backend, error), log *slog.Logger) *Member
func (m *Member) Run(ctx context.Context, e Engine) error  // until ctx ends
func (m *Member) Share(r sync.Shared); Leader() bool; Seen(id string) bool; Event(ev sync.Event, repo snapshot.RepoKey, source string)
```

## Config
| Key | Default | Meaning |
|---|---|---|
| `cluster.addresses` | none | Hazelcast members `host:port`; set to run as one of several instances |
| `cluster.name` | `dev` | the Hazelcast cluster name (the server's `cluster-name`) |
| `cluster.namespace` | `scp` | prefix of the Hazelcast objects |

## Tests
- Engine (`synctest`): a follower runs no jobs; `Lead(true)` starts them; a result after losing the lead is dropped; shared records carry
  increasing revisions, removed keys and the catalogue; `Receive` applies newer, ignores older and equal, holds early items until the
  catalogue admits them and drops them when it does not; `Records` lists listings first.
- Member over `clustertest`: one leader at a time, failover on close and on a lost connection, joining loads the map, reconnect reloads,
  seeding writes missing keys and deletes stale ones, coalescing queue, de-duplication, events reach every engine.
- `Hazelcast` against a real server when `TEST_HAZELCAST` is set (CI runs `hazelcast/hazelcast:5.7` as a service on loopback).
- End to end (`cmd/scp/cluster_test.go`, over `clustertest`, and over the real server when it is set): two instances on one fake GitHub
  make each GitHub call once, a webhook to the follower updates both, its redelivery to the leader is a duplicate, response bodies and
  ETags are equal on both, and when the leader stops the other takes over and keeps the data fresh.
