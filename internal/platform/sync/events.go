package sync

import (
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
)

// Notify runs every spec listening for ev soon: per-repository specs for repo, per-source specs for repo's source. It never blocks.
// A job that is running runs once more after it finishes; a job in retry backoff keeps its retry time.
func (e *Engine) Notify(ev Event, repo snapshot.RepoKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, known := e.repos[repo]
	if !known {
		return
	}
	now := time.Now()
	for _, s := range e.specs {
		if !slices.Contains(s.On, ev) {
			continue
		}
		key := s.Kind + "/" + string(repo)
		if s.PerSource {
			key = s.Kind + "/" + r.ID.Source
		}
		if en := e.entries[key]; en != nil {
			en.event(now)
		}
	}
	e.poke()
}

// event makes the entry due now at event priority; a running job runs once more, a retry keeps its time. Callers hold e.mu.
func (en *entry) event(now time.Time) {
	en.prio = PrioEvent
	at := en.settled(now)
	switch {
	case en.running:
		en.dirty = true
	case !en.backoff && en.due.After(at):
		en.due = at
	}
}

// settled is the earliest an event may run the entry again: now, or Spec.Settle after the last run started.
func (en *entry) settled(now time.Time) time.Time {
	if at := en.started.Add(en.spec.Settle); en.spec.Settle > 0 && at.After(now) {
		return at
	}
	return now
}

// NotifySource runs every per-source spec listening for ev for source soon, for events about repositories the engine may not know yet
// (created, transferred in, installation changes). It never blocks.
func (e *Engine) NotifySource(ev Event, source string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, s := range e.specs {
		if !s.PerSource || !slices.Contains(s.On, ev) {
			continue
		}
		if en := e.entries[s.Kind+"/"+source]; en != nil {
			en.event(now)
		}
	}
	e.poke()
}

// Handled reports whether the event ev about repo (or, when repo is empty, about source) has been acted on: every job that listens for it
// has finished a run since at, none is running or waiting to run again for it, and none is failing. A job that does not exist (the
// repository was removed, the feature is off) has nothing to do. Whoever parked the event may then forget it.
func (e *Engine) Handled(ev Event, repo snapshot.RepoKey, source string, at time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.specs {
		if !slices.Contains(s.On, ev) {
			continue
		}
		var key string
		switch {
		case repo == "" && s.PerSource:
			key = s.Kind + "/" + source
		case repo != "" && s.PerSource:
			r, known := e.repos[repo]
			if !known {
				continue
			}
			key = s.Kind + "/" + r.ID.Source
		case repo != "":
			key = s.Kind + "/" + string(repo)
		default:
			continue
		}
		if en := e.entries[key]; en != nil && (en.running || en.dirty || en.item.Attempts > 0 || !en.item.CheckedAt.After(at)) {
			return false
		}
	}
	return true
}

// Listeners are the kinds of the jobs that run on ev, sorted. A webhook event nothing listens to is a delivery acted on by nobody.
func (e *Engine) Listeners(ev Event) []string {
	var kinds []string
	for _, s := range e.specs {
		if slices.Contains(s.On, ev) {
			kinds = append(kinds, s.Kind)
		}
	}
	slices.Sort(kinds)
	return kinds
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
