package scm

import (
	"log/slog"
	"strings"
)

// EventKind is the normalised kind of an Event.
type EventKind string

const (
	EventPush              EventKind = "push"
	EventRelease           EventKind = "release"
	EventCreate            EventKind = "create"
	EventRepository        EventKind = "repository"
	EventInstallation      EventKind = "installation"
	EventInstallationRepos EventKind = "installation_repositories"
	EventPing              EventKind = "ping"
	EventHeadChanged       EventKind = "head_changed" // synthetic, emitted by the catalog head sweep
)

// EventKinds lists every kind, in declaration order, as a fresh slice. Tests enumerate it so that no kind is silently
// unhandled (GR-21).
func EventKinds() []EventKind {
	return []EventKind{EventPush, EventRelease, EventCreate, EventRepository, EventInstallation, EventInstallationRepos, EventPing, EventHeadChanged}
}

// Known reports whether k is one of EventKinds.
func (k EventKind) Known() bool {
	for _, c := range EventKinds() {
		if k == c {
			return true
		}
	}
	return false
}

// Event is a normalised signal that something may have changed. Events are hints (accelerators); the level-triggered
// planner repairs a lost one. Slices are shared with the producer: consumers must not mutate or retain them.
type Event struct {
	Kind          EventKind
	Delivery      string // X-GitHub-Delivery or a synthetic id
	Source        SourceID
	Installation  InstallationID
	Repo          RepoRef // zero for installation-level events
	Action        string  // created, deleted, renamed, ...
	Ref           string  // refs/heads/main
	Before, After string
	Created       bool
	Deleted       bool
	Forced        bool
	CommitCount   int
	ChangedPaths  []string // union of added, removed and modified paths across all commits
	PathsComplete bool     // false when commits hit the 2,048 cap or the payload omitted lists
	HeadTreeOID   string
}

const branchPrefix = "refs/heads/"

// Branch returns the branch name when Ref is a branch ref. Tags and anything else report false (GR-10).
func (e Event) Branch() (string, bool) {
	name, ok := strings.CutPrefix(e.Ref, branchPrefix)
	return name, ok && name != ""
}

// IsPushTo reports whether the event is a push (not a deletion) to the named branch.
func (e Event) IsPushTo(branch string) bool {
	b, ok := e.Branch()
	return e.Kind == EventPush && ok && !e.Deleted && b == branch
}

// LogValue renders the event for structured logs without the path list (which can be large).
func (e Event) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("kind", string(e.Kind)),
		slog.String("delivery", e.Delivery),
		slog.String("repo", e.Repo.Key()),
		slog.String("ref", e.Ref),
		slog.Int("commits", e.CommitCount),
		slog.Int("paths", len(e.ChangedPaths)),
		slog.Bool("paths_complete", e.PathsComplete),
	)
}
