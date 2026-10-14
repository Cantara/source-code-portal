package docs

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

func attrResult(r string) attribute.KeyValue { return attribute.String("result", r) }

// React is the reactor of docs (registered as a reactor.Func in cmd/scp). It is a cheap pure decision plus one
// non-blocking Enqueue: no I/O, no retained slices.
//
//	EventPush         default branch only, not deleted; refreshes when the changed paths touch a docs input
//	                  (or the path list is incomplete); the announced After commit is remembered as a hint
//	EventHeadChanged  emitted by the catalog head sweep when HEAD or tree changed: refresh (the job's fingerprint
//	                  shortcut makes a no-docs change cost one memoised tree listing)
//	EventCreate       a new branch or tag: ignored (docs follows the default branch)
//	EventRepository   created/renamed/transferred: refresh (the descriptor root and slug may have changed)
//	every other kind  ignored by design (counted)
func (s *Service) React(ctx context.Context, ev scm.Event, enq job.Enqueuer) error {
	count := func(r string) { s.m.reactions.Add(ctx, 1, metric.WithAttributes(attrResult(r))) }
	switch ev.Kind {
	case scm.EventPush, scm.EventHeadChanged, scm.EventRepository:
	default:
		count("ignored_kind")
		return nil
	}
	snap := s.d.Pub.Current()
	id, repo, ok := s.lookupRef(snap, ev.Repo)
	if !ok {
		count("unknown_repo")
		return nil
	}
	if ev.Kind == scm.EventPush {
		if ev.Deleted || ev.Ref != "refs/heads/"+repo.DefaultBranch {
			count("not_default_branch")
			return nil
		}
		if ev.PathsComplete && !TouchesDocs(ev.ChangedPaths, s.rootOf(snap, id)) && !s.touchesImages(snap, id, ev.ChangedPaths) {
			count("no_docs_path")
			return nil
		}
	}
	k := job.NewKey(JobKind, strings.Split(ev.Repo.Key(), "/")...)
	if ev.Kind == scm.EventPush && ev.After != "" {
		s.hints.Put(k, ev.After, time.Now())
	}
	if err := enq.Enqueue(k, job.PrioWebhook, time.Time{}, false); err != nil {
		count("enqueue_failed")
		return err
	}
	count("enqueued")
	return nil
}

func (s *Service) lookupRef(snap *readmodel.Snapshot, ref scm.RepoRef) (readmodel.EntryID, scm.Repository, bool) {
	for _, id := range snap.Repos() {
		if v, ok := readmodel.Get(snap, id, s.d.Repos); ok && v.V.Ref.Key() == ref.Key() { // folded comparison: job keys and events differ in case from the display ref
			return id, v.V, true
		}
	}
	return "", scm.Repository{}, false
}

// touchesImages is true when a changed path is a repository image some document of the repository references: the media
// URLs in the stored HTML embed the commit, so the document must be re-rendered at the new commit (V-12).
func (s *Service) touchesImages(snap *readmodel.Snapshot, id readmodel.EntryID, paths []string) bool {
	rd, ok := readmodel.Get(snap, id, s.d.DocsSlot)
	if !ok {
		return false
	}
	set := rd.ImageSet()
	for _, p := range paths {
		if _, hit := set[p]; hit {
			return true
		}
	}
	return false
}

func (s *Service) rootOf(snap *readmodel.Snapshot, id readmodel.EntryID) string {
	if rd, ok := readmodel.Get(snap, id, s.d.DocsSlot); ok && rd.Descriptor.Value.Docs.Root != "" {
		return rd.Descriptor.Value.Docs.Root
	}
	return s.d.Cfg.DefaultRoot
}

// TouchesDocs reports whether any changed path is an input of docs.refresh: a root README*, AGENTS.md or CHANGELOG.md,
// a descriptor, catalog-info.yaml, CODEOWNERS, or a document file under root.
func TouchesDocs(paths []string, root string) bool {
	prefix := strings.Trim(root, "/") + "/"
	for _, p := range paths {
		l := strings.ToLower(p)
		switch l {
		case "scp.yaml", "scp.yml", ".github/scp.yaml", ".github/scp.yml", "catalog-info.yaml", "catalog-info.yml",
			"codeowners", ".github/codeowners", "docs/codeowners":
			return true
		}
		if !strings.Contains(l, "/") && (strings.HasPrefix(l, "readme") || isAux(p)) {
			return true
		}
		// GitHub resolves the README in .github/, the root and docs/ (docs/ is covered by the root prefix rule below)
		if strings.HasPrefix(l, ".github/readme") && !strings.Contains(l[len(".github/"):], "/") {
			return true
		}
		if strings.HasPrefix(p, prefix) && FormatOf(p) != FormatText {
			return true
		}
	}
	return false
}
