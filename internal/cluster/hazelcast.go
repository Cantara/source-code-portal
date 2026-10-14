package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	hz "github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/logger"
	"github.com/hazelcast/hazelcast-go-client/types"
)

const (
	invocationTimeout = 30 * time.Second
	leaderKey         = "leader"
)

// Hazelcast is a Backend on a Hazelcast cluster: the maps <namespace>.state, <namespace>.lock and <namespace>.deliveries and the
// topic <namespace>.events.
type Hazelcast struct {
	client                  *hz.Client
	state, lock, deliveries *hz.Map
	events                  *hz.Topic
	lockCtx                 context.Context //nolint:containedctx // Hazelcast identifies the lock owner by this context
	conn                    chan bool
	leads                   bool
}

var _ Backend = (*Hazelcast)(nil)

// DialHazelcast returns a dial function for the Hazelcast members at addresses in the cluster name. The client reconnects on its own
// after a lost connection, forever.
func DialHazelcast(addresses []string, name, namespace string, log *slog.Logger) func(context.Context) (Backend, error) {
	return func(ctx context.Context) (Backend, error) {
		h := &Hazelcast{conn: make(chan bool, 64), lockCtx: hz.NewLockContext(context.Background())}
		cfg := hz.NewConfig()
		cfg.ClientName = "scp"
		cfg.Cluster.Name = name
		cfg.Cluster.Network.SetAddresses(addresses...)
		cfg.Cluster.InvocationTimeout = types.Duration(invocationTimeout)
		cfg.Logger.CustomLogger = slogger{log}
		cfg.AddLifecycleListener(func(ev hz.LifecycleStateChanged) {
			var up bool
			switch ev.State {
			case hz.LifecycleStateConnected:
				up = true
			case hz.LifecycleStateDisconnected:
			default:
				return
			}
			select {
			case h.conn <- up:
			default:
			}
		})
		c, err := hz.StartNewClientWithConfig(ctx, cfg)
		if err != nil {
			return nil, err
		}
		h.client = c
		for _, m := range []struct {
			to   **hz.Map
			name string
		}{{&h.state, "state"}, {&h.lock, "lock"}, {&h.deliveries, "deliveries"}} {
			if *m.to, err = c.GetMap(ctx, namespace+"."+m.name); err != nil {
				_ = c.Shutdown(ctx)
				return nil, err
			}
		}
		if h.events, err = c.GetTopic(ctx, namespace+".events"); err != nil {
			_ = c.Shutdown(ctx)
			return nil, err
		}
		return h, nil
	}
}

func (h *Hazelcast) TryLead(ctx context.Context) (bool, error) {
	lctx, done := h.locking(ctx)
	defer done()
	ok, err := h.lock.TryLock(lctx, leaderKey)
	h.leads = h.leads || ok
	return ok, err
}

// locking returns the lock owner's context, cancelled with ctx.
func (h *Hazelcast) locking(ctx context.Context) (context.Context, func()) {
	lctx, cancel := context.WithCancel(h.lockCtx)
	stop := context.AfterFunc(ctx, cancel)
	return lctx, func() { stop(); cancel() }
}

func (h *Hazelcast) Put(ctx context.Context, key string, value []byte) error {
	return h.state.Set(ctx, key, value)
}

func (h *Hazelcast) Delete(ctx context.Context, key string) error {
	return h.state.Delete(ctx, key)
}

func (h *Hazelcast) Keys(ctx context.Context) ([]string, error) {
	keys, err := h.state.GetKeySet(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := k.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (h *Hazelcast) Entries(ctx context.Context) (map[string][]byte, error) {
	entries, err := h.state.GetEntrySet(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		k, ok1 := e.Key.(string)
		v, ok2 := e.Value.([]byte)
		if ok1 && ok2 {
			out[k] = v
		}
	}
	return out, nil
}

func (h *Hazelcast) Watch(ctx context.Context, fn func(string, []byte)) error {
	handle := func(ev *hz.EntryNotified) {
		k, ok1 := ev.Key.(string)
		v, ok2 := ev.Value.([]byte)
		if ok1 && ok2 {
			fn(k, v)
		}
	}
	_, err := h.state.AddListener(ctx, hz.MapListener{EntryAdded: handle, EntryUpdated: handle}, true)
	return err
}

func (h *Hazelcast) Once(ctx context.Context, id string, ttl time.Duration) (bool, error) {
	prev, err := h.deliveries.PutIfAbsentWithTTL(ctx, id, true, ttl)
	return prev == nil, err
}

func (h *Hazelcast) Publish(ctx context.Context, msg []byte) error {
	return h.events.Publish(ctx, msg)
}

func (h *Hazelcast) Subscribe(ctx context.Context, fn func([]byte)) error {
	_, err := h.events.AddMessageListener(ctx, func(ev *hz.MessagePublished) {
		if v, ok := ev.Value.([]byte); ok {
			fn(v)
		}
	})
	return err
}

func (h *Hazelcast) Connected() <-chan bool { return h.conn }

// Close releases the leader lock at once (the cluster would release it some seconds after the client leaves) and shuts the client down.
func (h *Hazelcast) Close(ctx context.Context) error {
	if h.leads {
		lctx, done := h.locking(ctx)
		// The lock is reentrant (taken again after a reconnect); Unlock fails once this client no longer holds it.
		for range 100 {
			if h.lock.Unlock(lctx, leaderKey) != nil {
				break
			}
		}
		done()
	}
	return h.client.Shutdown(ctx)
}

// slogger passes the client's log to slog.
type slogger struct{ log *slog.Logger }

func (l slogger) Log(w logger.Weight, f func() string) {
	var level slog.Level
	switch {
	case w <= logger.WeightOff:
		return
	case w <= logger.WeightError:
		level = slog.LevelError
	case w <= logger.WeightWarn:
		level = slog.LevelWarn
	case w <= logger.WeightInfo:
		level = slog.LevelInfo
	default:
		level = slog.LevelDebug
	}
	if l.log.Enabled(context.Background(), level) {
		l.log.Log(context.Background(), level, fmt.Sprintf("hazelcast: %s", f()), "component", "hazelcast")
	}
}
