package main

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/feature/webhook"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/github"
	"github.com/cantara/source-code-portal/internal/platform/httpapi"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/sync"
)

// watchPrereqs checks the prerequisites at every publish (the App's installations are listed again with each source's listing),
// so a change is logged when it happens, not when a page asks.
func (s *server) watchPrereqs(ctx context.Context) {
	for {
		s.prereqs.check()
		select {
		case <-ctx.Done():
			return
		case <-s.pub.Published():
		}
	}
}

// deliver hands a webhook event to the engine of every instance (through the cluster when there is one, else or when it fails to
// this one's): per repository when repo is set, else per source.
func (s *server) deliver(ev sync.Event, repo snapshot.RepoKey, source string) {
	if s.member != nil {
		err := s.member.Event(context.Background(), ev, repo, source)
		if err == nil {
			return
		}
		s.log.Warn("webhook event not sent to the cluster; handled here", "err", err)
	}
	if repo == "" {
		s.engine.NotifySource(ev, source)
	} else {
		s.engine.Notify(ev, repo)
	}
}

// webhookSink is webhookSink over the server's inbox, knowing a repository by the current snapshot (an old address included).
func (s *server) webhookSink(sources []config.Source) webhook.Sink {
	return webhookSink(s.inbox.take, func(k snapshot.RepoKey) bool { e, moved := s.pub.Current().Repo(k); return e != nil || moved }, sources)
}

// discoverEvery is the least time between two listings of a source that events of repositories the portal does not know cause: a
// burst of them costs one listing at once and one when the window closes.
const discoverEvery = 15 * time.Second

// declinedFor is how long an address that asked for a listing does not ask again: a repository the source leaves out (excluded,
// private without include_private) stays unknown after the listing, and its pushes would otherwise list the source every window.
const declinedFor = time.Hour

// webhookSink hands webhook events on: repository events to the repository's jobs, owner-wide events to the listing of the source
// that has the owner among its organisations. An event of a repository the portal does not know (known is false: added to the App's
// selection, created, or not published yet after a start) is passed on as well, so the inbox keeps it until the repository is known,
// and lists its source (discovery). Owners no source lists are ignored.
func webhookSink(take func(sync.Event, snapshot.RepoKey, string) error, known func(snapshot.RepoKey) bool, sources []config.Source) webhook.Sink {
	d := &discovery{take: take, last: map[string]time.Time{}, armed: map[string]bool{}, asked: map[snapshot.RepoKey]time.Time{}}
	sourceOf := map[string]string{}
	for _, s := range sources {
		for _, org := range s.Orgs {
			sourceOf[strings.ToLower(org)] = s.ID
		}
	}
	return func(ev sync.Event, owner, name string) error {
		id, ok := sourceOf[strings.ToLower(owner)]
		switch {
		case !ok:
			return nil
		case name == "" || ev == sync.EventSourceChanged:
			return take(ev, "", id)
		case !known(snapshot.KeyOf(owner, name)):
			key := snapshot.KeyOf(owner, name)
			err := take(ev, key, "")
			d.ask(id, key)
			return err
		default:
			return take(ev, snapshot.KeyOf(owner, name), "")
		}
	}
}

// discovery lists a source for the events of repositories the portal does not know: at once, then at most once every discoverEvery
// (a trailing listing when the window closes catches what came in it), each address at most once every declinedFor.
type discovery struct {
	take  func(sync.Event, snapshot.RepoKey, string) error
	mu    gosync.Mutex
	last  map[string]time.Time           // by source: the last listing discovery asked for
	armed map[string]bool                // by source: a trailing listing is waiting for the window to close
	asked map[snapshot.RepoKey]time.Time // by address: when it last asked
}

func (d *discovery) ask(source string, key snapshot.RepoKey) {
	d.mu.Lock()
	now := time.Now()
	if at, ok := d.asked[key]; ok && now.Sub(at) < declinedFor {
		d.mu.Unlock()
		return
	}
	d.asked[key] = now
	if len(d.asked) > 4096 {
		maps.DeleteFunc(d.asked, func(_ snapshot.RepoKey, at time.Time) bool { return now.Sub(at) >= declinedFor })
	}
	since := now.Sub(d.last[source])
	due := since >= discoverEvery
	switch {
	case due:
		d.last[source] = now
	case !d.armed[source]:
		d.armed[source] = true
		time.AfterFunc(discoverEvery-since, func() {
			d.mu.Lock()
			d.armed[source], d.last[source] = false, time.Now()
			d.mu.Unlock()
			_ = d.take(sync.EventSourceChanged, "", source) // passed on whatever came of parking
		})
	}
	d.mu.Unlock()
	if due {
		_ = d.take(sync.EventSourceChanged, "", source)
	}
}

// eventRoutes wires what tells the portal about changes: the inbox that makes events survive a restart, the catch-up of each App's
// delivery log, and with a webhook secret the receiver (the returned route).
func (s *server) eventRoutes(cfg config.Config, gh *github.Client, m *metrics, st *status, feed bool, log *slog.Logger) []httpapi.Route {
	if !feed {
		return nil
	}
	s.inbox = &inbox{park: s.store, deliver: s.deliver, handled: s.engine.Handled, log: log, now: time.Now}
	observe := func(event, result string) { m.delivery(event, result); st.delivery(event, result) }
	for _, src := range cfg.GitHub.Sources {
		if g := gh.Source(src.ID); g.App() {
			s.inbox.catchUps = append(s.inbox.catchUps, catchUp(g, src.ID, s.store, s.pub.Current, s.webhookSink(cfg.GitHub.Sources), observe, log))
		}
	}
	st.catchUps = func() []webhook.CatchUpState {
		out := make([]webhook.CatchUpState, 0, len(s.inbox.catchUps))
		for _, c := range s.inbox.catchUps {
			out = append(out, c.State())
		}
		return out
	}
	if !cfg.Receiver() { // GitHub cannot reach the portal: the delivery logs are the only way events arrive
		return nil
	}
	rc := webhook.New(cfg.WebhookSecrets(), s.webhookSink(cfg.GitHub.Sources), log)
	if s.member != nil {
		rc.Dedupe(s.member.First)
	}
	rc.Observe(observe)
	return []httpapi.Route{{Pattern: webhook.Pattern, MaxBody: webhook.MaxBody, Handler: timed(rc, st.answered)}}
}

// unloggedSources are the sources a receiver tells about changes but whose missed deliveries nothing can recover: no App, so no
// delivery log to read (EV4, EV5). They are in "events" mode, but a delivery sent while the portal was down is lost, so the jobs
// with a faster poll under events (sync.Spec.PollTTL: the releases) run at it as for a polled source.
func unloggedSources(cfg config.Config) []string {
	var out []string
	for _, src := range cfg.GitHub.Sources {
		if cfg.Receives(src) && src.App == nil {
			out = append(out, src.ID)
		}
	}
	return out
}

// polledSources are the sources GitHub does not tell about changes: with a webhook receiver every source is told (the operator set the
// webhooks), without one only an App whose delivery log is read (webhook.catch_up) is; the others are polled, and the jobs whose TTL is
// only a net under events (sync.Spec.PollTTL) run at their poll cadence.
func polledSources(cfg config.Config) []string {
	var out []string
	for _, src := range cfg.GitHub.Sources {
		if !cfg.Receives(src) && (!cfg.Webhook.CatchUp || src.App == nil) {
			out = append(out, src.ID)
		}
	}
	return out
}

// toldSources are the sources GitHub tells of their changes: every one not polled.
func toldSources(cfg config.Config) map[string]bool {
	told := map[string]bool{}
	for _, src := range cfg.GitHub.Sources {
		told[src.ID] = true
	}
	for _, id := range polledSources(cfg) {
		delete(told, id)
	}
	return told
}
