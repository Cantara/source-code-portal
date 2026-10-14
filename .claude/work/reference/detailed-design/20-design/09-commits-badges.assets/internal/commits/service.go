package commits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// Deps is everything New needs; cmd/scp builds it. Every field is required: New returns an error naming each nil one
// (a nil logger or meter would otherwise panic on the first failed store write).
type Deps struct {
	Cfg    Config
	Lister CommitLister
	Store  CommitStore
	Pub    *readmodel.Publisher
	Slots
	Authz  authz.Authorizer
	Meter  metric.Meter
	Tracer trace.Tracer
	Log    *slog.Logger
}

// Service owns the commits feature. It starts no goroutines and keeps no mutable state: everything it knows lives in
// the snapshot and the store, so it is safe for concurrent use by the engine workers, webhook goroutines and handlers.
type Service struct {
	*Projection
	d Deps
	m instruments
	// pushed holds, per repository key (scm.RepoRef.Key), the generation of the newest push to the default branch that a
	// run has not yet consumed. A run that finds one must not use the head-sweep skip (the sweep value predates the
	// push); it deletes the entry with CompareAndDelete after a successful fetch, so a push that arrives during the fetch
	// leaves a newer generation behind and the follow-up run fetches again.
	pushed sync.Map // string -> uint64
	seq    atomic.Uint64
}

func New(d Deps) (*Service, error) {
	errs := d.Cfg.Validate("commits")
	for _, c := range []struct {
		name string
		nil_ bool
	}{{"Lister", d.Lister == nil}, {"Store", d.Store == nil}, {"Pub", d.Pub == nil}, {"Authz", d.Authz == nil},
		{"Meter", d.Meter == nil}, {"Tracer", d.Tracer == nil}, {"Log", d.Log == nil}} {
		if c.nil_ {
			errs = append(errs, fmt.Errorf("commits.Deps.%s is required", c.name))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	m, err := newInstruments(d)
	if err != nil {
		return nil, err
	}
	return &Service{Projection: NewProjection(d.Cfg, d.Slots), d: d, m: m}, nil
}

// Jobs returns the engine registration (mechanism C of the architecture).
func (s *Service) Jobs() []job.Spec {
	return []job.Spec{{
		Kind: JobKind, Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
		Policy: freshness.Policy{TTL: s.d.Cfg.RefreshTTL, Jitter: 0.1, IdleBackoffMax: s.d.Cfg.IdleBackoffMax},
		Run:    s.run,
	}}
}

// errors that classify themselves for the engine through the behaviour interfaces of package job.
type goneError struct{ err error }

func (e goneError) Error() string { return e.err.Error() }
func (e goneError) Unwrap() error { return e.err }
func (e goneError) Gone() bool    { return true }

type permanentError struct{ err error }

func (e permanentError) Error() string   { return e.err.Error() }
func (e permanentError) Unwrap() error   { return e.err }
func (e permanentError) Permanent() bool { return true }

// resolve finds the snapshot entry of a repository reference. It is a scan over at most a few thousand entries (about
// 20 microseconds per 1,000 repositories, BenchmarkResolve); jobs and reactors are not request paths.
func (s *Service) resolve(snap *readmodel.Snapshot, ref scm.RepoRef) (readmodel.EntryID, freshness.Value[scm.Repository], bool) {
	for _, id := range snap.Repos() {
		v, ok := readmodel.Get(snap, id, s.d.Repos)
		if !ok {
			continue
		}
		r := v.V.Ref
		if r.Source != ref.Source {
			continue
		}
		if r.ID != 0 && ref.ID != 0 {
			if r.ID == ref.ID {
				return id, v, true
			}
			continue
		}
		if strings.EqualFold(r.Owner, ref.Owner) && strings.EqualFold(r.Name, ref.Name) {
			return id, v, true
		}
	}
	return "", freshness.Value[scm.Repository]{}, false
}

func (s *Service) run(ctx context.Context, k job.Key) (out job.Outcome, err error) {
	ctx, span := s.d.Tracer.Start(ctx, "commits.ingest")
	defer span.End()

	ref, perr := scm.ParseRepoKey(strings.Join(k.Parts(), "/"))
	if perr != nil {
		return job.Outcome{}, permanentError{fmt.Errorf("commits: malformed job key %q: %w", k, perr)}
	}
	now := time.Now().UTC()
	snap := s.d.Pub.Current()
	id, repo, ok := s.resolve(snap, ref)
	if !ok {
		s.m.refresh(ctx, "gone")
		s.pushed.Delete(ref.Key())
		return job.Outcome{}, goneError{fmt.Errorf("commits: %s: %w", ref.Key(), readmodel.ErrUnknownRepo)}
	}
	span.SetAttributes(attribute.String("repo", string(id)))
	prior, _ := readmodel.Get(snap, id, s.d.View)
	gen, pushed := s.pushedGeneration(ref)
	fetched := false // the lister answered: a push seen before this point is now reflected
	defer func() {
		if fetched && err == nil && pushed {
			s.pushed.CompareAndDelete(ref.Key(), gen)
		}
	}()
	rk := store.KeyOf(repo.V.Ref)
	ik := store.ItemKey{Repo: rk, Kind: ItemKind}
	branch := repo.V.DefaultBranch

	// 1. The catalog head sweep already knows the head SHA: when it equals the head of our ring and the sweep itself is
	// fresh, the ring is current and no GitHub request is needed.
	if st, ok := readmodel.Get(snap, id, s.d.Stats); ok && !pushed && prior.Info.State == freshness.StateOK &&
		prior.Info.ContentSHA != "" && st.V.HeadSHA == prior.Info.ContentSHA && st.Info.Derive(now) == freshness.StatusFresh {
		return s.revalidate(ctx, id, ik, branch, prior, now, "skipped_head")
	}
	if branch == "" { // a repository without a default branch has no commits
		return s.publishEmpty(ctx, id, ik, "", now)
	}

	// 2. Fetch the newest Keep commits of the default branch.
	res, err := s.d.Lister.LatestCommits(ctx, repo.V.Ref, branch, s.d.Cfg.Keep, scm.Validators{SHA: prior.Info.ContentSHA})
	if err != nil {
		return job.Outcome{}, s.fail(ctx, id, ik, prior, now, err)
	}
	fetched = true
	if res.NotModified {
		if !prior.Info.HasValue() {
			// A 304 for a ring we do not hold: refetch without validators on the next attempt.
			return job.Outcome{}, s.fail(ctx, id, ik, prior, now, errors.New("commits: not modified without a stored ring"))
		}
		return s.revalidate(ctx, id, ik, branch, prior, now, "not_modified")
	}

	kept, head, st := normalise(res.Value, now, s.d.Cfg.Keep, repo.V.HTMLURL, prior.Latest)
	s.m.dropped(ctx, st)
	if len(kept) == 0 {
		return s.publishEmpty(ctx, id, ik, branch, now)
	}
	// The adapter reports the branch tip it observed (GitHub does not document the order of the list endpoint, so the
	// first element is only a fallback); without either, the newest commit by date is a usable validator.
	if v, ok := cleanSHA(res.Validators.SHA); ok {
		head = v
	} else if head == "" {
		head = kept[0].SHA
	}

	// 3. Unchanged ring: same head, same membership.
	if head == prior.Info.ContentSHA && sameSHAs(prior.Latest, kept) && prior.Info.HasValue() && prior.Info.State != freshness.StateNoContent {
		return s.revalidate(ctx, id, ik, branch, prior, now, "unchanged")
	}
	s.m.evicted(ctx, prior.Latest, kept)

	// 4. Persist, then publish. Rows first, membership record second: a crash in between leaves a ring record that
	// lists SHAs the rows may lack, which Restore detects (incomplete) and answers with an immediate refresh.
	rows, err := encodeRows(kept)
	if err != nil {
		return job.Outcome{}, permanentError{err}
	}
	ring, err := encodeRing(branch, head, kept)
	if err != nil {
		return job.Outcome{}, permanentError{err}
	}
	if perr := s.persist(ctx, rk, rows, store.Item{
		Key: ik, State: freshness.StateOK, FetchedAt: now, CheckedAt: now, ContentSHA: head, Payload: ring,
	}); perr != nil {
		if errors.Is(perr, store.ErrNoRepo) {
			return job.Outcome{}, goneError{perr}
		}
		s.m.persistError(ctx)
		s.d.Log.WarnContext(ctx, "commits: store write failed; publishing anyway", "repo", string(id), "err", perr)
	}
	v := View{Latest: kept, Info: s.info(freshness.Info{}, freshness.StateOK, now, head)}
	if err := s.publish(id, v); err != nil {
		return job.Outcome{}, err
	}
	s.m.refresh(ctx, "changed")
	s.d.Log.DebugContext(ctx, "commits refreshed", "repo", string(id), "fetched", len(res.Value), "kept", len(kept), "head", head)
	return job.Outcome{}, nil
}

func (s *Service) persist(ctx context.Context, rk store.RepoKey, rows []store.CommitRow, it store.Item) error {
	if err := s.d.Store.AppendCommits(ctx, rk, rows, s.d.Cfg.Keep); err != nil {
		return err
	}
	return s.d.Store.PutItem(ctx, it)
}

func (s *Service) publishEmpty(ctx context.Context, id readmodel.EntryID, ik store.ItemKey, branch string, now time.Time) (job.Outcome, error) {
	ring, _ := encodeRing(branch, "", nil)
	if err := s.d.Store.PutItem(ctx, store.Item{Key: ik, State: freshness.StateNoContent, FetchedAt: now, CheckedAt: now, Payload: ring}); err != nil {
		if errors.Is(err, store.ErrNoRepo) {
			return job.Outcome{}, goneError{err}
		}
		s.m.persistError(ctx)
	}
	v := View{Info: s.info(freshness.Info{}, freshness.StateNoContent, now, "")}
	if err := s.publish(id, v); err != nil {
		return job.Outcome{}, err
	}
	s.m.refresh(ctx, "empty")
	return job.Outcome{Unchanged: true}, nil
}

// revalidate records that the ring was confirmed current without writing commit rows. When the store has no good item
// to revalidate (store.ErrNoItem: an earlier write failed and the ring was only published, or the row was pruned) it
// writes the ring it is serving instead, so a missed write heals on the next confirmation and not only on a change.
func (s *Service) revalidate(ctx context.Context, id readmodel.EntryID, ik store.ItemKey, branch string, prior View, now time.Time, result string) (job.Outcome, error) {
	if err := s.d.Store.Revalidated(ctx, ik, now); err != nil {
		switch {
		case errors.Is(err, store.ErrNoRepo):
			return job.Outcome{}, goneError{err}
		case errors.Is(err, store.ErrNoItem):
			if rerr := s.repersist(ctx, ik, branch, prior, now); rerr != nil {
				if errors.Is(rerr, store.ErrNoRepo) {
					return job.Outcome{}, goneError{rerr}
				}
				s.m.persistError(ctx)
			}
		default:
			s.m.persistError(ctx)
		}
	}
	v := View{Latest: prior.Latest, Info: prior.Info.AfterRevalidated(now)} // the slice is shared: both values are immutable
	if prior.Info.State == freshness.StateNoContent {
		v.Info.State = freshness.StateNoContent // AfterRevalidated reports "ok"; an empty ring stays empty
	}
	if err := s.publish(id, v); err != nil {
		return job.Outcome{}, err
	}
	s.m.refresh(ctx, result)
	return job.Outcome{Unchanged: true}, nil
}

// repersist writes the ring that is being served (rows, then the membership item), exactly as a change does.
func (s *Service) repersist(ctx context.Context, ik store.ItemKey, branch string, prior View, now time.Time) error {
	rows, err := encodeRows(prior.Latest)
	if err != nil {
		return err
	}
	ring, err := encodeRing(branch, prior.Info.ContentSHA, prior.Latest)
	if err != nil {
		return err
	}
	state := freshness.StateOK
	if len(prior.Latest) == 0 {
		state = freshness.StateNoContent
	}
	return s.persist(ctx, ik.Repo, rows, store.Item{Key: ik, State: state, FetchedAt: now, CheckedAt: now, ContentSHA: prior.Info.ContentSHA, Payload: ring})
}

// fail records a failed attempt (stale-while-error: the ring and FetchedAt stay) and returns the error for the engine.
// Rate limits, cancellation and Gone errors are not failures of the data and are returned untouched.
func (s *Service) fail(ctx context.Context, id readmodel.EntryID, ik store.ItemKey, prior View, now time.Time, err error) error {
	if _, rl := job.AsRateLimited(err); rl || ctx.Err() != nil || job.IsGone(err) {
		s.m.refresh(ctx, "deferred")
		return err
	}
	code := errCode(err)
	if rerr := s.d.Store.RecordFailure(ctx, ik, store.Failure{Code: code, At: now}); rerr != nil && !errors.Is(rerr, store.ErrNoRepo) {
		s.m.persistError(ctx)
	}
	v := View{Latest: prior.Latest, Info: prior.Info.AfterFailure(now, code)} // keeps FetchedAt, ContentSHA and the ring
	v.Info.TTL = s.d.Cfg.RefreshTTL
	if perr := s.publish(id, v); perr != nil {
		return errors.Join(err, perr)
	}
	s.m.refresh(ctx, "error")
	return err
}

// info is the Info after a definitive result (kernel helper AfterSuccess) with this policy's TTL.
func (s *Service) info(prev freshness.Info, state freshness.State, now time.Time, head string) freshness.Info {
	i := prev.AfterSuccess(state, now, head)
	i.TTL = s.d.Cfg.RefreshTTL
	return i
}

func errCode(err error) string {
	// One closed vocabulary for freshness.Info.ErrCode across all features (20-design/92-error-catalogue.md section 3, CD-ER-01),
	// in the precedence of job.Classify: Gone > Permanent > RateLimited > Canceled, then the specific causes, then transient.
	switch {
	case job.IsGone(err):
		return "gone"
	case job.IsPermanent(err):
		return "permanent"
	}
	if _, rl := job.AsRateLimited(err); rl {
		return "rate_limited"
	}
	switch {
	case errors.Is(err, scm.ErrNotFound):
		return "not_found"
	case errors.Is(err, scm.ErrForbidden):
		return "forbidden"
	case errors.Is(err, scm.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "transient"
}

func (s *Service) publish(id readmodel.EntryID, v View) error {
	_, err := s.d.Pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, id, s.d.View, v) })
	switch {
	case err == nil:
		return nil
	case errors.Is(err, readmodel.ErrUnknownRepo):
		return goneError{err}
	case errors.Is(err, readmodel.ErrInvalidSnapshot):
		return permanentError{err}
	}
	return err
}

func (s *Service) pushedGeneration(ref scm.RepoRef) (uint64, bool) {
	v, ok := s.pushed.Load(ref.Key())
	if !ok {
		return 0, false
	}
	return v.(uint64), true
}

func sameSHAs(a, b []scm.Commit) bool {
	return slices.EqualFunc(a, b, func(x, y scm.Commit) bool { return x.SHA == y.SHA })
}

// React is the webhook and head-sweep accelerator (mechanism D). It is a cheap pure decision plus a non-blocking
// Enqueue, runs on the webhook goroutine, retains nothing of ev and is safe for concurrent use.
func (s *Service) React(_ context.Context, ev scm.Event, enq job.Enqueuer) error {
	var prio job.Priority
	switch ev.Kind {
	case scm.EventPush:
		prio = job.PrioWebhook
	case scm.EventHeadChanged:
		prio = job.PrioScheduled
	default:
		return nil
	}
	if ev.Repo.Name == "" || (ev.Kind == scm.EventPush && ev.Deleted) {
		return nil
	}
	_, repo, ok := s.resolve(s.d.Pub.Current(), ev.Repo)
	if !ok {
		s.m.reaction("unknown_repo")
		return nil
	}
	if ev.Kind == scm.EventPush && ev.Ref != "refs/heads/"+repo.V.DefaultBranch {
		s.m.reaction("not_default_branch")
		return nil
	}
	s.m.reaction("enqueued")
	if ev.Kind == scm.EventPush {
		s.pushed.Store(repo.V.Ref.Key(), s.seq.Add(1)) // before Enqueue: the run that starts must see it
	}
	// The key tail MUST be built from the catalogue's ref with KeyParts (folded owner and name): the planner's key for the
	// same repository is, and a key in the display case would not coalesce with it (scm SC-04, 01-kernel.md).
	return enq.Enqueue(job.NewKey(JobKind, repo.V.Ref.KeyParts()...), prio, time.Time{}, false)
}

// Restore rebuilds one repository's view from the store during warm start. cmd/scp calls it after LoadItems has
// returned (a LoadItems callback must not call the store), once per loaded item of kind ItemKind, with the builder of
// the version-1 Apply in which the catalogue entry already exists. A ring that cannot be reconstructed completely is
// skipped (the repository then has no view and its job is due at once); Restore only returns an error for a failing
// store read.
func (s *Service) Restore(ctx context.Context, b *readmodel.Builder, id readmodel.EntryID, repo store.RepoKey, it store.Item) error {
	if !it.State.HasResult() || it.FetchedAt.IsZero() {
		return nil // the error row RecordFailure creates for a repository that never had a ring: nothing to restore, not an anomaly
	}
	ring, err := decodeRing(it.Payload)
	if err != nil {
		s.m.restoreResult(ctx, "invalid")
		return nil
	}
	if len(ring.SHAs) == 0 {
		v := View{Info: s.restoredInfo(it, ring)}
		s.m.restoreResult(ctx, "ok")
		return putView(b, id, s.d.View, v)
	}
	rows, err := s.d.Store.LoadCommits(ctx, repo, s.d.Cfg.Keep)
	if err != nil {
		return err
	}
	by := make(map[string]store.CommitRow, len(rows))
	for _, r := range rows {
		by[r.SHA] = r
	}
	cs := make([]scm.Commit, 0, len(ring.SHAs))
	for _, sha := range ring.SHAs {
		r, ok := by[sha]
		if !ok {
			s.m.restoreResult(ctx, "incomplete")
			return nil
		}
		c, derr := decodeRow(r)
		if derr != nil {
			s.m.restoreResult(ctx, "invalid")
			return nil
		}
		cs = append(cs, c)
	}
	kept, _, st := normalise(cs, time.Now(), s.d.Cfg.Keep, "", nil)
	if st != (dropStats{}) || len(kept) != len(ring.SHAs) {
		s.m.restoreResult(ctx, "invalid")
		return nil
	}
	s.m.restoreResult(ctx, "ok")
	return putView(b, id, s.d.View, View{Latest: kept, Info: s.restoredInfo(it, ring)})
}

func putView(b *readmodel.Builder, id readmodel.EntryID, slot readmodel.Slot[View], v View) error {
	return readmodel.Put(b, id, slot, v)
}

func (s *Service) restoredInfo(it store.Item, ring ringRecord) freshness.Info {
	// it.Error holds the closed code written by fail. The store keeps the status of a row that has a value (stale-while-error),
	// so a failing repository is recognised by Attempts and Error and restored as an Info in state error with its ring held.
	info := freshness.Info{State: it.State, FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, Attempts: it.Attempts,
		ErrCode: it.Error, ContentSHA: ring.Head, TTL: s.d.Cfg.RefreshTTL}
	if info.HasValue() && it.Attempts > 0 && it.Error != "" {
		info.State = freshness.StateError
	}
	return info
}

// Recent is the read side overview needs (unit 15, R-09-1; its consumer interface CommitsSource is satisfied directly):
// up to n of the newest commits of the repository in snapshot snap, with the freshness Info of its ring; ok is false when
// the repository has no view yet. It does no authorisation: the caller has resolved the slug through authz.LookupRepo.
// The result is a copy, so a caller can never alias the immutable ring.
func (s *Service) Recent(snap *readmodel.Snapshot, slug scm.Slug, n int) ([]scm.Commit, freshness.Info, bool) {
	v, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.View)
	if !ok {
		return nil, freshness.Info{}, false
	}
	return slices.Clone(v.Latest[:max(0, min(n, len(v.Latest)))]), v.Info, true
}

// EnqueueUnrestored enqueues commits.refresh at PrioBackfill for every repository of snap that has no view, and returns
// how many it enqueued. cmd/scp calls it once after the engine runs, following the warm start: a repository whose ring
// Restore skipped (incomplete, invalid, or the error row of a repository that never had one) has no view, but the planner
// restored its persisted NextDue and would leave it empty until then (up to the idle back-off cap, 12 h). Keys of
// repositories the planner already enqueues coalesce, so a cold start is unaffected. A refusal (queue full, engine
// closed) stops the walk and is returned with the count so far.
func (s *Service) EnqueueUnrestored(snap *readmodel.Snapshot, enq job.Enqueuer) (int, error) {
	n := 0
	for _, id := range snap.Repos() {
		rv, ok := readmodel.Get(snap, id, s.d.Repos)
		if !ok {
			continue
		}
		if _, has := readmodel.Get(snap, id, s.d.View); has {
			continue
		}
		if err := enq.Enqueue(job.NewKey(JobKind, rv.V.Ref.KeyParts()...), job.PrioBackfill, time.Time{}, false); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Coverage is the readiness probe of kind "commits": repositories with a result state over all repositories.
func (s *Service) Coverage(snap *readmodel.Snapshot) (have, total int) {
	for _, id := range snap.Repos() {
		total++
		if v, ok := readmodel.Get(snap, id, s.d.View); ok && v.Info.HasValue() { // a failing repository that still serves its ring counts
			have++
		}
	}
	return have, total
}
