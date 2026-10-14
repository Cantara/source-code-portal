package catalog

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// maxListPages bounds one listing (100 repositories per page: 100,000 repositories). A listing that does not end by then is an error, not a result.
const maxListPages = 1000

// ListError is the typed error of a failed listing; it unwraps to the adapter's error so job.AsRateLimited and friends work.
type ListError struct {
	Source scm.SourceID
	Page   int
	Err    error
}

func (e *ListError) Error() string {
	return fmt.Sprintf("list source %s page %d: %v", e.Source, e.Page, e.Err)
}
func (e *ListError) Unwrap() error { return e.Err }

var errListLoop = errors.New("listing did not terminate")

// runReconcile is the engine job catalog.reconcile (global scope, key "catalog.reconcile/_").
func (s *Service) runReconcile(ctx context.Context, _ job.Key) (job.Outcome, error) {
	ctx, span := s.d.Tracer.Start(ctx, "catalog.reconcile")
	defer span.End()
	var errs []error
	changed := false
	var next time.Time // earliest explicit follow-up (unconfirmed absences)
	for _, src := range s.d.Sources {
		if !s.due(src.ID) {
			s.m.reconcile.Add(ctx, 1, metric.WithAttributes(attribute.String("source", string(src.ID)), attribute.String("result", "skipped")))
			continue
		}
		ch, err := s.reconcileSource(ctx, src)
		if err != nil {
			// A rate limit, a cancelled context or a closed engine ends the whole run at once (the engine pauses the upstream);
			// any other failure of one source must not starve the others.
			if _, rl := job.AsRateLimited(err); rl || ctx.Err() != nil || errors.Is(err, job.ErrClosed) {
				return job.Outcome{}, err
			}
			errs = append(errs, err)
			continue
		}
		changed = changed || ch
		if s.hasUnconfirmed(src.ID) {
			s.want(src.ID)
			if at := time.Now().Add(confirmGap); next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	return job.Outcome{Unchanged: !changed, NextDue: next}, errors.Join(errs...)
}

// due reports whether the source's own reconcile_every has elapsed (10% slack, so the planner's jitter never skips a source).
func (s *Service) due(id scm.SourceID) bool {
	src, ok := s.source(id)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sources[id]
	return st.wanted || st.lastSuccess.IsZero() || time.Since(st.lastSuccess) >= src.Policy.ReconcileEvery-src.Policy.ReconcileEvery/10
}

func (s *Service) reconcileSource(ctx context.Context, src SourceDeps) (changed bool, err error) {
	start := time.Now()
	attr := metric.WithAttributes(attribute.String("source", string(src.ID)))
	ctx, span := s.d.Tracer.Start(ctx, "catalog.reconcile.source")
	defer span.End()
	defer func() { s.m.reconcileSecs.Record(ctx, time.Since(start).Seconds(), attr) }()

	s.mu.Lock()
	s.sources[src.ID].wanted = false
	s.mu.Unlock()
	listStart := time.Now().UTC() // events that arrive after this instant are newer than the listing
	listed, err := s.listSource(ctx, src)
	if err != nil {
		s.markFailed(ctx, src.ID, err)
		s.m.reconcile.Add(ctx, 1, attr, metric.WithAttributes(attribute.String("result", "failed")))
		return false, err
	}
	changed, held, err := s.commit(ctx, src, listed, listStart)
	switch {
	case err != nil:
		s.markFailed(ctx, src.ID, err)
		s.m.reconcile.Add(ctx, 1, attr, metric.WithAttributes(attribute.String("result", "failed")))
	case held:
		s.m.reconcile.Add(ctx, 1, attr, metric.WithAttributes(attribute.String("result", "held")))
	default:
		s.m.reconcile.Add(ctx, 1, attr, metric.WithAttributes(attribute.String("result", "ok")))
	}
	return changed, err
}

// confirmGap is the delay of the follow-up listing that confirms an absence (removal needs discovery.removal_confirmations complete
// listings; waiting a whole reconcile_every would keep a deleted repository visible for an hour). Long enough that GitHub's page
// shifting during a listing cannot repeat, short enough to be noticed by nobody.
const confirmGap = 2 * time.Minute

// hasUnconfirmed reports whether a repository of the source is missing from the last complete listing and has not yet reached
// discovery.removal_confirmations: only then does a follow-up listing help. Once the count is reached the removal is either applied or
// held back by the mass-removal guard, and a held plan is re-evaluated at the normal cadence (polling every confirmGap for the whole
// hold would be hundreds of useless listings).
func (s *Service) hasUnconfirmed(id scm.SourceID) bool {
	src, ok := s.source(id)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tracked {
		if t.Repo.Ref.Source == id && t.Misses > 0 && t.Misses < src.Policy.RemovalConfirmations {
			return true
		}
	}
	return false
}

// markFailed records a failed attempt and republishes catalog.sync, so /status and the readiness gate see the failure (the
// source state lives in memory; only a publish makes it visible). Called without any lock held.
func (s *Service) markFailed(ctx context.Context, id scm.SourceID, err error) {
	s.mu.Lock()
	st := s.sources[id]
	st.lastAttempt, st.state, st.errCode = time.Now().UTC(), "failed", errCode(err)
	s.mu.Unlock()
	s.publishStatus(ctx)
}

// publishStatus puts a fresh catalog.sync into the snapshot without touching anything else. It runs under pubMu so that it can
// never overwrite a newer status published by commit or hide, and so that tracked and the snapshot agree while it builds.
func (s *Service) publishStatus(ctx context.Context) {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	s.mu.Lock()
	next := slices.Clone(s.tracked)
	groups := s.groups
	s.mu.Unlock()
	status := s.buildStatus(next, groups, nil)
	if _, err := s.d.Publisher.Apply(func(b *readmodel.Builder) error { return readmodel.PutGlobal(b, s.d.Slots.Sync, status) }); err != nil {
		s.d.Log.ErrorContext(ctx, "status not published", "err", err)
	}
}

// listSource fetches every page of the source (R0). Any page failure abandons the listing: a partial list is never diffed.
// Pages are requested with the validators of the previous pass; a 304 reuses the stored page.
func (s *Service) listSource(ctx context.Context, src SourceDeps) ([]scm.Repository, error) {
	var all []scm.Repository
	cursor := ""
	touched := map[string]bool{}
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, &ListError{src.ID, page, errListLoop}
		}
		s.mu.Lock()
		old, have := s.sources[src.ID].pages[cursor]
		s.mu.Unlock()
		v := scm.Validators{}
		if have {
			v.ETag = old.etag
		}
		res, err := s.d.Lister.ListRepositories(ctx, src.ID, cursor, v)
		if err != nil {
			return nil, &ListError{src.ID, page, err}
		}
		var pg scm.Page[scm.Repository]
		switch {
		case res.NotModified && have:
			pg = old.page
			s.m.listPages.Add(ctx, 1, metric.WithAttributes(attribute.String("source", string(src.ID)), attribute.String("result", "304")))
		case res.NotModified: // a 304 for a page we do not hold: the adapter answered a validator we never sent; ask again without one
			res, err = s.d.Lister.ListRepositories(ctx, src.ID, cursor, scm.Validators{})
			if err != nil || res.NotModified {
				return nil, &ListError{src.ID, page, errors.Join(err, errors.New("unconditional request answered 304"))}
			}
			fallthrough
		default:
			pg = res.Value
			pg.Items = slices.Clone(pg.Items) // the adapter's slice is not ours
			s.mu.Lock()
			s.sources[src.ID].pages[cursor] = pageEntry{etag: res.Validators.ETag, page: pg}
			s.mu.Unlock()
			s.m.listPages.Add(ctx, 1, metric.WithAttributes(attribute.String("source", string(src.ID)), attribute.String("result", "200")))
		}
		touched[cursor] = true
		all = append(all, pg.Items...)
		if pg.Next == "" {
			break
		}
		cursor = pg.Next
	}
	s.mu.Lock() // forget cached pages whose cursor this pass did not use (the cursor space changed)
	maps.DeleteFunc(s.sources[src.ID].pages, func(c string, _ pageEntry) bool { return !touched[c] })
	s.mu.Unlock()
	return all, nil
}

// commitInput is the in-memory state one plan is computed from, read in one critical section.
type commitInput struct {
	cur   []Tracked
	held  *Held
	list  []scm.Repository
	hides uint64 // value of hideGen when the input was read
}

// readInput snapshots the tracked set and drops from the listing every repository that an event hid AFTER the listing began: the
// older listing must not undo the event (the next listing, already enqueued by React, decides). Hints older than the listing are satisfied
// by it and forgotten.
func (s *Service) readInput(src SourceDeps, listed []scm.Repository, listStart time.Time) commitInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := commitInput{cur: slices.Clone(s.tracked), held: s.sources[src.ID].held, list: listed, hides: s.hideGen}
	if len(s.hiddenAt) > 0 {
		in.list = slices.Clone(listed)
		in.list = slices.DeleteFunc(in.list, func(r scm.Repository) bool {
			r.Ref.Source = src.ID
			h, ok := s.hiddenAt[KeyOf(r.Ref)]
			return ok && !h.at.Before(listStart) && !h.demote // at or after the start: the listing may predate the event
		})
		for i := range in.list { // an older listing that still says "public" must not undo a demotion either
			r := &in.list[i]
			r.Ref.Source = src.ID
			if h, ok := s.hiddenAt[KeyOf(r.Ref)]; ok && h.demote && !h.at.Before(listStart) && r.Visibility == scm.VisPublic {
				r.Visibility = scm.VisPrivate
			}
		}
		for k, h := range s.hiddenAt {
			if h.at.Before(listStart) {
				delete(s.hiddenAt, k)
			}
		}
	}
	return in
}

func (s *Service) plan(src SourceDeps, in commitInput, now time.Time) Plan {
	var grouped func(scm.Repository) bool
	if s.d.Config.Ungrouped == UngroupedIgnore {
		grouped = s.compiled.Grouped
	}
	return PlanReconcile(PlanInput{Source: src.ID, Policy: src.Policy, Listed: in.list, Current: in.cur, Held: in.held,
		Now: now, Prefix: s.prefix, Grouped: grouped})
}

// commit plans, persists and publishes the result of one complete listing (steps R1-R8 of section 4.4).
//
// Order: plan, persist (store write, no catalog lock but commitMu), then the in-memory phase under pubMu: re-read the input when an event
// hid a repository meanwhile, build the membership, publish once, install the new tracked set. pubMu is also taken by hide, so an event can
// neither be undone by a commit that was already planning nor publish a membership computed from an older tracked set.
func (s *Service) commit(ctx context.Context, src SourceDeps, listed []scm.Repository, listStart time.Time) (changed, held bool, err error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	now := time.Now().UTC()
	in := s.readInput(src, listed, listStart)
	plan := s.plan(src, in, now)

	// R8a persist first (restart state), then publish (serving state). A store failure never blocks serving (section 5.3).
	rows := make([]store.Repo, 0, len(plan.Next))
	for _, t := range plan.Next {
		row, err := EncodeRow(t)
		if err != nil {
			return false, false, err
		}
		rows = append(rows, row)
	}
	if _, err := s.d.Store.ReplaceCatalogue(ctx, rows); err != nil {
		if ctx.Err() != nil {
			return false, false, ctx.Err()
		}
		s.m.persistErr.Add(ctx, 1, metric.WithAttributes(attribute.String("op", "replace_catalogue")))
		s.d.Log.ErrorContext(ctx, "catalogue not persisted", "err", err, "source", src.ID)
	}

	// R8b publish (in-memory phase)
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	s.mu.Lock()
	stale := s.hideGen != in.hides
	s.mu.Unlock()
	if stale { // an event hid a repository while the rows were being written: plan again from the current state (pure, no I/O)
		in = s.readInput(src, listed, listStart)
		plan = s.plan(src, in, now)
	}
	mr, err := s.buildMembership(ctx, plan.Next)
	if err != nil {
		return false, false, permanentError{err} // a membership the kernel rejects would be rejected again: park the key
	}
	out := s.outcomeOf(src.ID, now, len(in.list), plan)
	status := s.buildStatus(plan.Next, mr.Groups, &out)
	if _, err := s.publish(ctx, in.cur, plan.Next, mr, status, now); err != nil {
		if errors.Is(err, readmodel.ErrInvalidSnapshot) {
			err = permanentError{err}
		}
		return false, false, err
	}
	changed = len(plan.Added)+len(plan.Removed)+len(plan.Renamed)+len(plan.Updated) > 0

	s.mu.Lock()
	s.tracked, s.groups = plan.Next, mr.Groups
	s.sources[src.ID].apply(out)
	s.mu.Unlock()

	a := metric.WithAttributes(attribute.String("source", string(src.ID)))
	for kind, n := range map[string]int{"added": len(plan.Added), "removed": len(plan.Removed), "renamed": len(plan.Renamed), "updated": len(plan.Updated)} {
		if n > 0 {
			s.m.changes.Add(ctx, int64(n), a, metric.WithAttributes(attribute.String("change", kind)))
		}
	}
	for _, r := range plan.Removed {
		s.d.Log.InfoContext(ctx, "repository left the catalogue", "source", src.ID, "repo_id", r.Ref.ID, "repo", r.Ref.Name, "reason", r.Reason)
	}
	for _, r := range plan.Renamed {
		s.d.Log.InfoContext(ctx, "repository renamed", "source", src.ID, "repo_id", r.Ref.ID, "old_slug", r.OldSlug, "slug", r.NewSlug)
	}
	s.recordGauges(ctx, plan.Next, mr)
	if s.d.Tracker != nil {
		s.d.Tracker.SetRepos(s.refs()) // while pubMu is held: SetRepos must not call back into catalog (the bridge only forwards to the planner)
	}
	return changed, plan.Held != nil, nil
}

// sourceOutcome is what one successful commit changes in the display state of its source. It is applied to the in-memory state only
// after the snapshot was published, and is also what the published Status is computed from.
type sourceOutcome struct {
	id          scm.SourceID
	state       string
	at          time.Time
	listed      int
	excluded    map[Exclusion]int
	held        *Held
	heldCount   int
	quarantined []Quarantine
	findings    []Finding
}

func (s *Service) outcomeOf(id scm.SourceID, now time.Time, listed int, plan Plan) sourceOutcome {
	o := sourceOutcome{id: id, state: "ok", at: now, listed: listed, excluded: plan.Excluded, held: plan.Held, heldCount: plan.HeldCount,
		quarantined: plan.Quarantined, findings: plan.Findings}
	switch {
	case plan.Held != nil:
		o.state = "held"
	case countSource(plan.Next, id) == 0:
		o.state = "empty"
	}
	return o
}

func (x *sourceState) apply(o sourceOutcome) {
	x.lastAttempt, x.lastSuccess, x.errCode, x.state = o.at, o.at, "", o.state
	x.listed, x.excluded, x.held, x.heldCount, x.quarantined, x.findings = o.listed, o.excluded, o.held, o.heldCount, o.quarantined, o.findings
}

// recordGauges refreshes the catalogue gauges after a publish. Labels are bounded: sources and groups are configuration, never repositories.
func (s *Service) recordGauges(ctx context.Context, next []Tracked, mr MembershipResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ok, degraded := 0, 0
	for _, h := range mr.Groups {
		s.m.groupMembers.Record(ctx, int64(h.Members), metric.WithAttributes(attribute.String("group", string(h.ID))))
		if h.DefaultReason != "" || h.Members == 0 {
			degraded++
		} else {
			ok++
		}
	}
	s.m.groups.Record(ctx, int64(ok), metric.WithAttributes(attribute.String("state", "ok")))
	s.m.groups.Record(ctx, int64(degraded), metric.WithAttributes(attribute.String("state", "degraded")))
	for _, sd := range s.d.Sources {
		a := attribute.String("source", string(sd.ID))
		st := s.sources[sd.ID]
		s.m.repositories.Record(ctx, int64(countSource(next, sd.ID)), metric.WithAttributes(a, attribute.String("state", "tracked")))
		s.m.repositories.Record(ctx, int64(len(st.quarantined)), metric.WithAttributes(a, attribute.String("state", "quarantined")))
		s.m.repositories.Record(ctx, int64(st.heldCount), metric.WithAttributes(a, attribute.String("state", "held")))
		for e := ExcludedInvalid; e <= ExcludedUngrouped; e++ {
			s.m.excluded.Record(ctx, int64(st.excluded[e]), metric.WithAttributes(a, attribute.String("reason", e.String())))
		}
	}
}

func countSource(ts []Tracked, id scm.SourceID) int {
	n := 0
	for _, t := range ts {
		if t.Repo.Ref.Source == id {
			n++
		}
	}
	return n
}

func (s *Service) buildMembership(ctx context.Context, next []Tracked) (MembershipResult, error) {
	start := time.Now()
	repos := make([]scm.Repository, len(next))
	for i, t := range next {
		repos[i] = t.Repo
	}
	mr, err := s.compiled.BuildMembership(repos)
	s.m.membershipSecs.Record(ctx, time.Since(start).Seconds())
	return mr, err
}

// entryRepo is the value of the existence slot: the tracked repository with its effective ref in DefaultBranch (design deviation D-1).
func entryRepo(t Tracked, mr MembershipResult) scm.Repository {
	r := t.Repo
	if ref, ok := mr.TrackedRef[r.Slug]; ok {
		r.DefaultBranch = ref
	}
	return r
}

// publish applies the plan to the snapshot in one Apply: removals first, then new and changed existence values, then the globals.
// An unchanged repository is rewritten only when its freshness record is older than half the TTL: every complete listing confirms that the
// repository exists (that is what catalog.repo's Info says), but a re-put per listing would bump the revision of every entry for nothing.
func (s *Service) publish(ctx context.Context, before, next []Tracked, mr MembershipResult, status Status, now time.Time) (*readmodel.Snapshot, error) {
	oldSlug := map[RepoKey]scm.Slug{}
	for _, t := range before {
		oldSlug[KeyOf(t.Repo.Ref)] = t.Repo.Slug
	}
	nextByKey := map[RepoKey]Tracked{}
	for _, t := range next {
		nextByKey[KeyOf(t.Repo.Ref)] = t
	}
	aliases := buildAliases(next)
	groupAliases := buildGroupAliases(s.d.Config)
	ttl := s.reconcileTTL()
	return s.d.Publisher.Apply(func(b *readmodel.Builder) error {
		cur := b.View()                                       // the candidate under construction, not a snapshot loaded before the publish mutex was taken
		carry := map[RepoKey]freshness.Value[scm.RepoStats]{} // stats of repositories whose entry id changes: read BEFORE the old entry is removed
		for k, slug := range oldSlug {
			if nt, still := nextByKey[k]; !still || nt.Repo.Slug != slug {
				if sv, ok := readmodel.Get(cur, readmodel.EntryID(slug), s.d.Slots.Stats); ok && still {
					carry[k] = sv
				}
				b.Remove(readmodel.EntryID(slug))
			}
		}
		for _, t := range next {
			id := readmodel.EntryID(t.Repo.Slug)
			v := entryRepo(t, mr)
			if old, ok := readmodel.Get(cur, id, s.d.Slots.Repo); ok && sameRepo(old.V, v) && t.SeenAt.Sub(old.Info.FetchedAt) < ttl/2 {
				continue
			}
			info := freshness.Info{State: freshness.StateOK, FetchedAt: t.SeenAt, CheckedAt: t.SeenAt, TTL: ttl}
			if err := readmodel.Put(b, id, s.d.Slots.Repo, freshness.Value[scm.Repository]{V: v, Info: info}); err != nil {
				return err
			}
			if sv, ok := carry[KeyOf(t.Repo.Ref)]; ok { // renamed: the old entry is gone, its stats keep their own freshness
				if err := readmodel.Put(b, id, s.d.Slots.Stats, sv); err != nil {
					return err
				}
			}
		}
		for _, err := range []error{
			readmodel.PutGlobal(b, s.d.Slots.Membership, mr.Membership), readmodel.PutGlobal(b, s.d.Slots.Aliases, aliases),
			readmodel.PutGlobal(b, s.d.Slots.GroupAliases, groupAliases), readmodel.PutGlobal(b, s.d.Slots.Sync, status),
		} {
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// buildAliases flattens every tracked repository's old slugs into old -> current, skipping an alias that is a real slug (slugs are
// canonical lower-case, so the comparison is exact). A typed upper-case URL is not an alias: overview lower-cases
// the candidate before they look anything up.
func buildAliases(next []Tracked) map[scm.Slug]scm.Slug {
	real := map[scm.Slug]bool{}
	for _, t := range next {
		real[t.Repo.Slug] = true
	}
	out := map[scm.Slug]scm.Slug{}
	for _, t := range next {
		for _, a := range t.Aliases {
			if !real[a] {
				out[a] = t.Repo.Slug
			}
		}
	}
	return out
}

// buildGroupAliases maps each group's earlier ids (lower-cased) to its current id.
func buildGroupAliases(cfg Config) map[scm.GroupID]scm.GroupID {
	out := map[scm.GroupID]scm.GroupID{}
	for _, g := range cfg.Groups {
		for _, a := range g.Aliases {
			out[scm.GroupID(FoldSlug(scm.Slug(a)))] = g.ID
		}
	}
	return out
}

// buildStatus assembles the published Status from the in-memory source states, with the outcome of the commit in progress (pend, may be
// nil) laid over its source. Findings are per source (a held source keeps its R301 while another source is committed). The caller holds
// pubMu, not mu.
func (s *Service) buildStatus(next []Tracked, groups []GroupHealth, pend *sourceOutcome) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Groups: groups}
	var oldest time.Time
	for i, sd := range s.d.Sources {
		x := *s.sources[sd.ID] // a copy: the outcome is not installed before the publish succeeded
		if pend != nil && pend.id == sd.ID {
			x.apply(*pend)
		}
		ss := SourceStatus{ID: sd.ID, State: x.state, LastAttempt: x.lastAttempt, LastSuccess: x.lastSuccess, ErrCode: x.errCode, Listed: x.listed,
			Tracked: countSource(next, sd.ID), HeldCount: x.heldCount, Quarantined: len(x.quarantined), Excluded: excludedMap(x.excluded)}
		if i == 0 || ss.LastSuccess.Before(oldest) {
			oldest = ss.LastSuccess
		}
		out.Sources = append(out.Sources, ss)
		out.Findings = append(out.Findings, x.findings...)
	}
	out.LastReconcile = oldest
	for _, h := range groups {
		if h.DefaultReason != "" {
			out.Findings = append(out.Findings, Finding{Code: "R205", Severity: SevWarning, Path: "/catalog/groups/" + string(h.ID) + "/default_repo",
				Message: fmt.Sprintf("group %q has no usable default repository (%s)", h.ID, h.DefaultReason)})
		}
	}
	SortFindings(out.Findings)
	return out
}

func excludedMap(m map[Exclusion]int) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		out[k.String()] = v
	}
	return out
}
