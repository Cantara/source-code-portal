package manifests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

var errBlobVanished = errors.New("manifests: a blob listed in the tree is no longer readable (history rewritten); retry with a fresh tree")

func (s *Service) runRefresh(ctx context.Context, k job.Key) (job.Outcome, error) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.refresh")
	defer span.End()
	out, outcome, err := s.refresh(ctx, k)
	s.m.inc(s.m.refreshTotal, "outcome", outcome)
	return out, err
}

func (s *Service) refresh(ctx context.Context, k job.Key) (job.Outcome, string, error) {
	ref, err := scm.ParseRepoKey(strings.Join(k.Parts(), "/"))
	if err != nil {
		return job.Outcome{}, "gone", job.MarkGone(errors.New("manifests: malformed job key"))
	}
	rk := store.KeyOf(ref)
	snap := s.d.Publisher.Current()
	slug, ok := s.idx.lookup(snap, s.d.Slots.Repos, rk)
	if !ok {
		return job.Outcome{}, "gone", job.MarkGone(errors.New("manifests: repository is not in the catalogue"))
	}
	id := readmodel.EntryID(slug)
	repoV, _ := readmodel.Get(snap, id, s.d.Slots.Repos)
	statsV, _ := readmodel.Get(snap, id, s.d.Slots.Stats)
	prev, havePrev := readmodel.Get(snap, id, s.d.Slots.Facts)
	repo := repoV.V
	ref = repo.Ref // full identity including the numeric id
	log := s.d.Log.With(slog.String("job_kind", KindRefresh), slog.String("repo", string(slug)))
	now := time.Now().UTC()
	reg := s.d.Registry

	// (0) change detection without any API call
	forced := s.takeLockMiss(rk)
	recheck := s.takeRecheck(rk) // a webhook says the tree moved; catalog.stats may not know yet (it is refreshed by the head sweep only)
	if havePrev && prev.Info.HasValue() && !forced && !recheck && prev.V.ParserSet == reg.SetHash() && prev.V.TreeSHA != "" && statsV.V.HeadTreeOID == prev.V.TreeSHA {
		return s.unchanged(ctx, id, rk, prev, now)
	}

	// (1) tree discovery
	disc, tree, treeCalls, err := s.discover(ctx, ref, repo.DefaultBranch)
	if err != nil {
		if errors.Is(err, scm.ErrNotFound) { // empty repository or default branch missing: a state, not a failure
			return s.publish(ctx, log, ref, slug, statsV.V, RepoFacts{TreeSHA: "", ParsedAt: now, ParserSet: reg.SetHash(),
				Diagnostics: []Diagnostic{{Code: "tree.empty", Severity: SevInfo, Message: "the repository has no readable tree on its default branch"}}}, freshness.StateNoContent, prev.Info, now)
		}
		return job.Outcome{}, "failed", s.fail(ctx, id, rk, prev, havePrev, err, now)
	}
	if havePrev && prev.Info.HasValue() && prev.V.TreeSHA == tree.sha && prev.V.ParserSet == reg.SetHash() && !forced {
		return s.unchanged(ctx, id, rk, prev, now)
	}

	// (2) load: parse cache, blob cache, fetch
	work, files, fdiags, err := s.load(ctx, ref, disc.Candidates)
	if err != nil {
		return job.Outcome{}, "failed", s.fail(ctx, id, rk, prev, havePrev, err, now)
	}

	// (3) assemble
	facts, locked, err := s.assembleAll(ctx, tree, disc, work, files, fdiags, statsV.V, now)
	if err != nil {
		return job.Outcome{}, "failed", s.fail(ctx, id, rk, prev, havePrev, err, now)
	}
	facts.Truncated = tree.truncated
	if tree.truncated {
		facts.Diagnostics = append(facts.Diagnostics, Diagnostic{Code: DiagTreeTruncated, Severity: SevWarning, Message: fmt.Sprintf("the tree is too large for one call; %d directory reads were used", treeCalls)})
	}
	// locked components go to the bounded cache before the facts reference them
	for _, u := range locked {
		if b, err := EncodeLocked(u.locked); err == nil {
			_ = s.d.Store.PutCache(ctx, NSLocked, lockedKey(rk, u.unitID, facts.TreeSHA), b)
		}
	}
	log.Info("manifests refreshed", slog.String("tree_sha", facts.TreeSHA), slog.Int("files", facts.FileCount), slog.Int("units", len(facts.Units)))
	return s.publish(ctx, log, ref, slug, statsV.V, facts, freshness.StateOK, prev.Info, now)
}

// lockedKey is the cache key of one unit's locked components: "<source>/<owner>/<name>|<unit id>|<tree sha>", replaced by its SHA-256 when it would
// exceed 400 bytes (the store accepts at most 512; deep directory names make long unit ids).
func lockedKey(rk store.RepoKey, unitID, tree string) string {
	k := rk.Source + "/" + rk.Owner + "/" + rk.Name + "|" + unitID + "|" + tree
	if len(k) > maxCacheKey {
		h := sha256.Sum256([]byte(k))
		return "h:" + hex.EncodeToString(h[:])
	}
	return k
}

const maxCacheKey = 400

type lockedUnit struct {
	unitID string
	locked []LockedComponent
}

// markRecheck remembers that an event proved (or could not exclude) a manifest change, so the next run of the job skips the no-call shortcut.
func (s *Service) markRecheck(rk store.RepoKey) {
	s.mu.Lock()
	s.recheck[rk] = struct{}{}
	s.mu.Unlock()
}

func (s *Service) takeRecheck(rk store.RepoKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.recheck[rk]
	delete(s.recheck, rk)
	return ok
}

func (s *Service) takeLockMiss(rk store.RepoKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.lockMiss[rk]
	delete(s.lockMiss, rk)
	return ok
}

// unchanged revalidates the stored row and, at most every RepublishAfter (or when the held value was marked failed), republishes the slot
// so Info.FetchedAt advances: the derived status must not drift to stale while nothing changed (design 06 D15).
func (s *Service) unchanged(ctx context.Context, id readmodel.EntryID, rk store.RepoKey, prev freshness.Value[RepoFacts], now time.Time) (job.Outcome, string, error) {
	if err := s.d.Store.Revalidated(ctx, store.ItemKey{Repo: rk, Kind: ItemKind}, now); err != nil && !errors.Is(err, store.ErrNoRepo) {
		s.d.Log.Warn("manifests revalidation not persisted", slog.Any("err", err))
	}
	if now.Sub(prev.Info.FetchedAt) >= s.cfg.Refresh.RepublishAfter || prev.Info.Attempts > 0 || prev.Info.State != freshness.StateOK {
		v := prev
		v.Info = s.withTTL(prev.Info.AfterRevalidated(now))
		if _, err := s.d.Publisher.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, id, s.d.Slots.Facts, v) }); err != nil && !errors.Is(err, readmodel.ErrUnknownRepo) {
			return job.Outcome{}, "failed", err
		}
	}
	return job.Outcome{Unchanged: true}, "unchanged", nil
}

// withTTL sets Info.TTL to idle_backoff_max: the planner may leave an unchanged repository alone for that long, so staleness (1.5 x TTL) is
// measured against it and not against the refresh TTL. Without a TTL the kernel derives Stale (Derive treats TTL <= 0 as stale).
func (s *Service) withTTL(i freshness.Info) freshness.Info {
	i.TTL = s.cfg.Refresh.IdleBackoffMax
	return i
}

type treeInfo struct {
	sha       string
	truncated bool
}

// discover lists the tree (recursive; directory walk when GitHub truncates it) and selects candidates.
func (s *Service) discover(ctx context.Context, ref scm.RepoRef, branch string) (Discovery, treeInfo, int, error) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.discover")
	defer span.End()
	a := s.cfg.Acquisition
	res, err := s.d.Trees.ListTree(ctx, ref, branch, scm.TreeOptions{Recursive: true, MaxEntries: a.MaxTreeEntries})
	s.m.inc(s.m.treeCalls, "mode", "recursive")
	if err != nil {
		return Discovery{}, treeInfo{}, 1, err
	}
	t := res.Value
	if !t.Truncated() {
		return Discover(t.All(), s.d.Registry, a, s.cfg.Limits), treeInfo{sha: t.SHA()}, 1, nil
	}
	entries, calls, capped, err := s.walk(ctx, ref, t.SHA())
	if err != nil {
		return Discovery{}, treeInfo{}, calls + 1, err
	}
	d := Discover(func(yield func(scm.TreeEntry) bool) {
		for _, e := range entries {
			if !yield(e) {
				return
			}
		}
	}, s.d.Registry, a, s.cfg.Limits)
	if capped {
		s.m.inc(s.m.limitHits, "limit", "tree-calls")
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: DiagLimitTreeCalls, Severity: SevWarning, Message: fmt.Sprintf("directory walk stopped after %d reads", a.MaxTreeCalls)})
	}
	return d, treeInfo{sha: t.SHA(), truncated: true}, calls + 1, nil
}

// walk lists directories breadth first (shallow first), pruning excluded segments and depth, at most MaxTreeCalls reads. Every directory is read
// BY ITS TREE ID (the root id comes from the truncated recursive listing, a child's id from its parent's entry): one adapter call per directory
// (unit 05 resolves a Path option segment by segment), immutable and cacheable by id, and one consistent snapshot even when a push lands
// mid-walk (a branch name would mix trees of two commits). Entry paths returned by the reader are relative to the listed directory; they are joined here.
func (s *Service) walk(ctx context.Context, ref scm.RepoRef, rootTree string) (entries []scm.TreeEntry, calls int, capped bool, err error) {
	a := s.cfg.Acquisition
	excl := toSet(a.ExcludeSegments)
	type dir struct{ path, id string }
	queue := []dir{{"", rootTree}}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if calls >= a.MaxTreeCalls {
			return entries, calls, true, nil
		}
		res, err := s.d.Trees.ListTree(ctx, ref, d.id, scm.TreeOptions{Recursive: false, MaxEntries: a.MaxTreeEntries})
		calls++
		s.m.inc(s.m.treeCalls, "mode", "walk")
		if err != nil {
			return nil, calls, false, err
		}
		for e := range res.Value.All() {
			full := path.Join(d.path, e.Path)
			sha := e.SHA
			e.Path = full
			if e.Type == scm.EntryTree {
				if excl[path.Base(full)] || strings.Count(full, "/")+1 > a.MaxDepth || sha == "" {
					continue
				}
				queue = append(queue, dir{full, sha})
				continue
			}
			entries = append(entries, e)
		}
	}
	return entries, calls, false, nil
}

type fileWork struct {
	cand   Candidate
	parser Parser
	parsed Parsed
	perr   *parseErr
}

// load resolves every (candidate, parser) pair to a Parsed or a deterministic failure: parse cache first, then the raw-blob cache,
// then the network (batched when possible), verifying every blob against its tree id.
func (s *Service) load(ctx context.Context, ref scm.RepoRef, cands []Candidate) ([]*fileWork, FileSet, []Diagnostic, error) {
	a := s.cfg.Acquisition
	var work []*fileWork
	var diags []Diagnostic
	need := map[string][]*fileWork{} // blob sha -> work needing bytes
	paths := make(PathSet, 0, len(cands))
	for _, c := range cands {
		paths = append(paths, c.Path)
		for _, p := range c.Parsers {
			w := &fileWork{cand: c, parser: p}
			work = append(work, w)
			if b, ok, err := s.d.Store.GetCache(ctx, NSParse, ParseKey(c.SHA, p.ID(), p.Version())); err == nil && ok {
				if parsed, perr, err := DecodeParse(b); err == nil {
					w.parsed, w.perr = parsed, perr
					s.m.inc(s.m.filesTotal, "result", "parse_cache")
					continue
				}
			}
			need[c.SHA] = append(need[c.SHA], w)
		}
	}
	// bytes for the misses
	data := map[string][]byte{}
	var fetch []Candidate
	seen := map[string]bool{}
	for _, c := range cands {
		if _, ok := need[c.SHA]; !ok || seen[c.SHA] {
			continue
		}
		seen[c.SHA] = true
		if b, ok, err := s.d.Store.GetCache(ctx, NSBlob, c.SHA); err == nil && ok {
			data[c.SHA] = b
			s.m.inc(s.m.filesTotal, "result", "blob_cache")
			continue
		}
		fetch = append(fetch, c)
	}
	fetched, fd, err := s.fetchBlobs(ctx, ref, fetch)
	if err != nil {
		return nil, nil, nil, err
	}
	diags = append(diags, fd...)
	for sha, b := range fetched {
		data[sha] = b
		if int64(len(b)) <= int64(a.RawBlobCacheMax) {
			_ = s.d.Store.PutCache(ctx, NSBlob, sha, b)
		}
	}
	// parse the misses with bounded concurrency
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.parseConc)
	for _, ws := range need {
		for _, w := range ws {
			b, ok := data[w.cand.SHA]
			if !ok {
				w.perr = &parseErr{} // empty code: already diagnosed by fetchBlobs, not cached, not reported again
				continue
			}
			g.Go(func() error {
				w.parsed, w.perr = s.parseOne(gctx, w.parser, w.cand, b)
				return nil
			})
		}
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	return work, paths, diags, nil
}

// fetchBlobs reads blobs by sha: batched for small ones when a BlobBatchReader is wired, singly otherwise; each result is verified.
func (s *Service) fetchBlobs(ctx context.Context, ref scm.RepoRef, cs []Candidate) (map[string][]byte, []Diagnostic, error) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.fetch")
	defer span.End()
	a := s.cfg.Acquisition
	out := map[string][]byte{}
	var diags []Diagnostic
	var mu = make(chan struct{}, 1)
	put := func(c Candidate, b []byte, d *Diagnostic) {
		mu <- struct{}{}
		defer func() { <-mu }()
		if d != nil {
			diags = append(diags, *d)
			s.m.inc(s.m.filesTotal, "result", "failed")
			return
		}
		out[c.SHA] = b
		s.m.inc(s.m.filesTotal, "result", "fetched")
	}
	var small, large []Candidate
	for _, c := range cs {
		if s.d.BlobBatch != nil && c.Size <= int64(a.BatchMaxBlobBytes) {
			small = append(small, c)
		} else {
			large = append(large, c)
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(a.FetchConcurrency)
	maxBytes := int64(s.cfg.Limits.LockfileBytes)
	verify := func(c Candidate, b []byte) (*Diagnostic, bool) {
		if !GitBlobSHA(c.SHA, b) {
			s.m.inc(s.m.fetchTotal, "mode", "sha_mismatch")
			return &Diagnostic{Code: DiagSHAMismatch, Severity: SevError, Message: "fetched content does not match the tree entry", Loc: Location{Path: c.Path}}, false
		}
		if _, err := limits.RejectBinary(limits.StripBOM(b)); err != nil {
			return &Diagnostic{Code: DiagEncoding, Severity: SevWarning, Message: err.Error(), Loc: Location{Path: c.Path}}, false
		}
		return nil, true
	}
	for i := 0; i < len(small); i += a.BatchSize {
		batch := small[i:min(i+a.BatchSize, len(small))]
		g.Go(func() error {
			shas := make([]string, len(batch))
			for j, c := range batch {
				shas[j] = c.SHA
			}
			got, err := s.d.BlobBatch.ReadBlobs(gctx, ref, shas, maxBytes)
			s.m.inc(s.m.fetchTotal, "mode", "batch")
			if err != nil {
				return err
			}
			for _, c := range batch {
				bl, ok := got[c.SHA]
				if !ok { // absent from the batch (binary, truncated, missing): read it singly
					appendLocked(mu, &large, c)
					continue
				}
				if d, ok := verify(c, bl.Data); !ok {
					put(c, nil, d)
					continue
				}
				put(c, bl.Data, nil)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	g, gctx = errgroup.WithContext(ctx)
	g.SetLimit(a.FetchConcurrency)
	for _, c := range large {
		g.Go(func() error {
			bl, err := s.d.Blobs.ReadBlob(gctx, ref, c.SHA, maxBytes)
			s.m.inc(s.m.fetchTotal, "mode", "rest")
			switch {
			case err == nil:
			case errors.Is(err, scm.ErrNotFound):
				return errBlobVanished
			case job.IsPermanent(err):
				put(c, nil, &Diagnostic{Code: DiagFetchFailed, Severity: SevWarning, Message: "the file could not be read", Loc: Location{Path: c.Path}})
				return nil
			default:
				return err
			}
			if d, ok := verify(c, bl.Data); !ok {
				put(c, nil, d)
				return nil
			}
			put(c, bl.Data, nil)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	slices.SortFunc(diags, func(a, b Diagnostic) int { return strings.Compare(a.Loc.Path+a.Code, b.Loc.Path+b.Code) })
	return out, diags, nil
}

func appendLocked(mu chan struct{}, dst *[]Candidate, c Candidate) {
	mu <- struct{}{}
	defer func() { <-mu }()
	*dst = append(*dst, c)
}

// parseOne runs one parser on one file under the limits, the memory budget and the watchdog, sanitises the result and caches it.
func (s *Service) parseOne(ctx context.Context, p Parser, c Candidate, data []byte) (Parsed, *parseErr) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.parse")
	defer span.End()
	t0 := time.Now()
	eco := string(p.Ecosystem())
	lc := s.cfg.Limits
	maxBytes := int(lc.ManifestBytes)
	if k, ok := p.(Kinded); ok && k.Kind() == KindLockfile {
		maxBytes = int(lc.LockfileBytes)
	}
	lim := Limits{MaxBytes: maxBytes, MaxDepth: lc.MaxDepth, MaxNodes: lc.MaxNodes, MaxExpand: int(lc.MaxExpand), MaxExpandAll: int(lc.MaxExpandFile), Timeout: lc.ParseTimeout}
	done := func(outcome string) {
		s.m.inc(s.m.parseTotal, "ecosystem", eco, "parser", p.ID(), "outcome", outcome)
		s.m.parseSeconds.Record(ctx, time.Since(t0).Seconds(), attrs("ecosystem", eco))
	}
	cacheKey := ParseKey(c.SHA, p.ID(), p.Version())
	remember := func(parsed Parsed, perr *parseErr) {
		if b, err := EncodeParse(parsed, perr); err == nil {
			_ = s.d.Store.PutCache(ctx, NSParse, cacheKey, b)
		}
	}
	text, err := limits.RejectBinary(limits.StripBOM(data))
	if err != nil {
		pe := &parseErr{Code: DiagEncoding, Msg: err.Error()}
		remember(Parsed{}, pe)
		done("failed")
		return Parsed{}, pe
	}
	if len(text) > lim.MaxBytes {
		s.m.inc(s.m.limitHits, "limit", limits.LimitSize)
		pe := &parseErr{Code: "limit." + limits.LimitSize, Msg: fmt.Sprintf("file is %d bytes, limit %d", len(text), lim.MaxBytes)}
		remember(Parsed{}, pe)
		done("limit")
		return Parsed{}, pe
	}
	amp := 10
	if w, ok := p.(Weigher); ok {
		amp = max(1, w.Amplification())
	}
	parsed, err := limits.Run(ctx, s.wd, p.ID(), lim.Timeout, int64(len(text))*int64(amp), func(pctx context.Context) (Parsed, error) {
		return p.Parse(pctx, File{Path: c.Path, Data: text, BlobSHA: c.SHA}, lim)
	})
	var le *limits.LimitError
	var ee *limits.EncodingError
	var pan *limits.PanicError
	switch {
	case err == nil:
		spec, _ := s.d.Registry.Spec(p.Ecosystem())
		sanitizeParsed(&parsed, spec, lc, strings.Count(string(text), "\n")+1)
		for _, d := range parsed.Diagnostics { // a parser that stopped at its own limit says so with a limit.* diagnostic (RR-4)
			if rest, ok := strings.CutPrefix(d.Code, LimitDiagPrefix); ok {
				s.m.inc(s.m.limitHits, "limit", rest)
			}
		}
		remember(parsed, nil)
		done("ok")
		return parsed, nil
	case errors.As(err, &le):
		s.m.inc(s.m.limitHits, "limit", le.Limit)
		pe := &parseErr{Code: "limit." + le.Limit, Msg: le.Error()}
		if le.Limit != limits.LimitTimeout { // a timeout depends on machine load: never cached
			remember(Parsed{}, pe)
		}
		done("limit")
		return Parsed{}, pe
	case errors.As(err, &ee):
		pe := &parseErr{Code: DiagEncoding, Msg: ee.Error()}
		remember(Parsed{}, pe)
		done("failed")
		return Parsed{}, pe
	case errors.As(err, &pan):
		pe := &parseErr{Code: DiagParsePanic, Msg: "the parser panicked on this file"}
		remember(Parsed{}, pe)
		done("panic")
		return Parsed{}, pe
	case errors.Is(err, limits.ErrAbandoned):
		done("abandoned")
		return Parsed{}, &parseErr{Code: DiagParseAbandoned, Msg: "the parser did not stop in time"}
	case errors.Is(err, limits.ErrCircuitOpen):
		done("abandoned")
		return Parsed{}, &parseErr{Code: DiagCircuitOpen, Msg: "the parser is disabled after repeated hangs"}
	case ctx.Err() != nil:
		return Parsed{}, &parseErr{Code: "canceled"}
	}
	pe := &parseErr{Code: DiagParseFailed, Msg: CleanText(err.Error(), 200)}
	remember(Parsed{}, pe)
	done("failed")
	return Parsed{}, pe
}

// assembleAll stamps file refs, rebases paths, assembles per ecosystem and finalises the repository facts.
func (s *Service) assembleAll(ctx context.Context, tree treeInfo, disc Discovery, work []*fileWork, files FileSet, fdiags []Diagnostic, stats scm.RepoStats, now time.Time) (RepoFacts, []lockedUnit, error) {
	ctx, span := s.d.Tracer.Start(ctx, "manifests.assemble")
	defer span.End()
	reg := s.d.Registry
	role := map[string]Role{}
	for _, c := range disc.Candidates {
		role[c.Path] = c.Role
	}
	diags := slices.Clone(disc.Diagnostics)
	diags = append(diags, fdiags...)
	byEco := map[EcosystemID][]Parsed{}
	for _, w := range work {
		if w.perr != nil && w.perr.Code == "" {
			continue
		}
		if w.perr != nil {
			diags = append(diags, Diagnostic{Code: w.perr.Code, Severity: SevWarning, Message: w.perr.Msg, Loc: Location{Path: w.cand.Path}})
			continue
		}
		p := w.parsed
		p.File = FileRef{Path: w.cand.Path, BlobSHA: w.cand.SHA, ParserID: w.parser.ID(), ParserVersion: w.parser.Version()}
		if k, ok := w.parser.(Kinded); ok {
			p.Kind = k.Kind()
		}
		p.File.Kind = p.Kind
		rebasePaths(&p)
		byEco[w.parser.Ecosystem()] = append(byEco[w.parser.Ecosystem()], p)
	}
	ecos := make([]EcosystemID, 0, len(byEco))
	for e := range byEco {
		ecos = append(ecos, e)
	}
	slices.Sort(ecos)
	var units []Unit
	for _, e := range ecos {
		ps := byEco[e]
		slices.SortFunc(ps, func(a, b Parsed) int {
			return strings.Compare(a.File.Path+a.File.ParserID, b.File.Path+b.File.ParserID)
		})
		actx, cancel := context.WithTimeout(ctx, s.cfg.Limits.AssembleTimeout)
		us, ad := runAssembler(actx, reg, e, ps, files)
		cancel()
		diags = append(diags, ad...)
		units = append(units, us...)
	}
	units, fd := finalize(units, role, s.cfg.Limits.MaxUnitsPerRepo, s.cfg.Limits)
	diags = append(diags, fd...)
	var locked []lockedUnit
	for _, u := range units {
		if len(u.Locked) > 0 {
			locked = append(locked, lockedUnit{u.ID, u.Locked})
		}
	}
	for _, d := range diags {
		s.m.inc(s.m.diagnostics, "severity", fmt.Sprint(d.Severity), "namespace", namespace(d.Code))
	}
	slices.SortStableFunc(diags, func(a, b Diagnostic) int { return strings.Compare(a.Code+"|"+a.Loc.Path, b.Code+"|"+b.Loc.Path) })
	facts := RepoFacts{TreeSHA: tree.sha, CommitSHA: stats.HeadSHA, ParsedAt: now, ParserSet: reg.SetHash(), Units: units,
		Technology: deriveTechnology(units, reg), Licences: deriveLicences(units), Diagnostics: diags, FileCount: len(disc.Candidates) + disc.Skipped}
	return facts, locked, nil
}

func namespace(code string) string {
	if i := strings.IndexByte(code, '.'); i > 0 {
		return code[:i]
	}
	return "other"
}

// publish persists the facts and swaps them into the snapshot, then asks for a debounced graph rebuild.
func (s *Service) publish(ctx context.Context, log *slog.Logger, ref scm.RepoRef, slug scm.Slug, stats scm.RepoStats, f RepoFacts, state freshness.State, prevInfo freshness.Info, now time.Time) (job.Outcome, string, error) {
	rk := store.KeyOf(ref)
	payload, err := EncodeFacts(f, int(s.cfg.Limits.MaxFactsBytes))
	for i := 0; errors.Is(err, ErrFactsTooLarge) && i < maxShrinkPasses; i++ { // cap every large unit at half the largest until the payload fits
		f = shrinkLargest(f)
		payload, err = EncodeFacts(f, int(s.cfg.Limits.MaxFactsBytes))
	}
	if err != nil {
		return job.Outcome{}, "failed", job.MarkPermanent(err)
	}
	s.m.factsBytes.Record(ctx, float64(len(payload)))
	info := s.withTTL(prevInfo.AfterSuccess(state, now, f.TreeSHA))
	err = s.d.Store.PutItem(ctx, store.Item{Key: store.ItemKey{Repo: rk, Kind: ItemKind}, State: info.State, FetchedAt: info.FetchedAt, CheckedAt: info.CheckedAt, ContentSHA: info.ContentSHA, Payload: payload})
	if errors.Is(err, store.ErrNoRepo) {
		return job.Outcome{}, "gone", job.MarkGone(errors.New("manifests: repository removed while refreshing"))
	}
	if err != nil { // a failing store does not stop serving (section 9.3 of the architecture): log, count, still publish
		log.Warn("manifests facts not persisted", slog.Any("err", err))
	}
	f.Units = cloneUnitsExact(stripLocked(f.Units))
	v := freshness.Value[RepoFacts]{V: f, Info: info}
	_, err = s.d.Publisher.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, readmodel.EntryID(slug), s.d.Slots.Facts, v) })
	switch {
	case errors.Is(err, readmodel.ErrUnknownRepo):
		return job.Outcome{}, "gone", job.MarkGone(errors.New("manifests: repository removed while refreshing"))
	case errors.Is(err, readmodel.ErrInvalidSnapshot):
		return job.Outcome{}, "failed", job.MarkPermanent(err)
	case err != nil:
		return job.Outcome{}, "failed", err
	}
	s.mu.Lock()
	delete(s.cleanOID, rk)
	s.mu.Unlock()
	s.enqueueGraph(now)
	return job.Outcome{}, "changed", nil
}

func (s *Service) enqueueGraph(now time.Time) {
	p := s.enq.Load()
	if p == nil {
		return
	}
	if err := (*p).Enqueue(graphKey(), job.PrioScheduled, now.Add(s.cfg.Graph.Debounce), false); err != nil {
		s.d.Log.Warn("manifests graph job not enqueued", slog.Any("err", err))
	}
}

func cloneUnitsExact(us []Unit) []Unit {
	out := cloneExact(us)
	for i := range out {
		u := &out[i]
		u.Files, u.Provides, u.Dependencies, u.Members, u.Licenses, u.Diagnostics = cloneExact(u.Files), cloneExact(u.Provides), cloneExact(u.Dependencies), cloneExact(u.Members), cloneExact(u.Licenses), cloneExact(u.Diagnostics)
	}
	return out
}

// maxShrinkPasses bounds the trimming: each pass halves the largest unit's cap, so 24 passes reach one dependency per unit from any admissible size
// (max_deps_per_file x 4 is under 2^23).
const maxShrinkPasses = 24

// shrinkLargest trims the facts so that they can be stored: every unit that has more than half of the largest unit's dependencies is cut to that half
// (several mid-sized units overflow a payload together, cutting only the single largest one does not help), and the repository says so once.
func shrinkLargest(f RepoFacts) RepoFacts {
	f.Units = slices.Clone(f.Units)
	most := 0
	for _, u := range f.Units {
		most = max(most, len(u.Dependencies))
	}
	if most <= 1 {
		return f // nothing left to trim; the caller gives up (permanent) after the pass bound
	}
	limit := most / 2
	for i := range f.Units {
		if len(f.Units[i].Dependencies) > limit {
			f.Units[i].Dependencies = f.Units[i].Dependencies[:limit:limit]
		}
	}
	if !hasDiagCode(f.Diagnostics, DiagDepsCap) {
		f.Diagnostics = append(slices.Clone(f.Diagnostics), Diagnostic{Code: DiagDepsCap, Severity: SevWarning, Message: "dependencies were dropped to fit the stored payload limit"})
	}
	return f
}

func hasDiagCode(ds []Diagnostic, code string) bool {
	return slices.ContainsFunc(ds, func(d Diagnostic) bool { return d.Code == code })
}

// fail records a failed attempt (stale-while-error: the held value keeps serving) and returns the error unchanged so the engine classifies it
// (rate limit, permanent, gone, transient). A rate limit or a cancellation is not an attempt.
func (s *Service) fail(ctx context.Context, id readmodel.EntryID, rk store.RepoKey, prev freshness.Value[RepoFacts], havePrev bool, err error, now time.Time) error {
	if _, limited := job.AsRateLimited(err); limited || ctx.Err() != nil {
		return err
	}
	code := errCode(err)
	_ = s.d.Store.RecordFailure(context.WithoutCancel(ctx), store.ItemKey{Repo: rk, Kind: ItemKind}, store.Failure{Code: code, Message: CleanText(err.Error(), 200), At: now})
	v := prev
	v.Info = s.withTTL(prev.Info.AfterFailure(now, code))
	_, _ = s.d.Publisher.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, id, s.d.Slots.Facts, v) })
	return err
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
	case errors.Is(err, scm.ErrForbidden):
		return "forbidden"
	case errors.Is(err, scm.ErrNotFound):
		return "not_found"
	case errors.Is(err, scm.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, errBlobVanished):
		return "history_rewritten"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "transient"
}
