// Package scm: scratch subset of architecture section 8.1.
package scm

import "time"

type SourceID string
type RepoID int64
type InstallationID int64

type RepoRef struct {
	Source SourceID
	ID     RepoID
	Owner  string
	Name   string
}

type EventKind string

const (
	EventPush              EventKind = "push"
	EventRelease           EventKind = "release"
	EventCreate            EventKind = "create"
	EventRepository        EventKind = "repository"
	EventInstallation      EventKind = "installation"
	EventInstallationRepos EventKind = "installation_repositories"
	EventPing              EventKind = "ping"
	EventHeadChanged       EventKind = "head_changed"
)

type Event struct {
	Kind                     EventKind
	Delivery                 string
	Source                   SourceID
	Installation             InstallationID
	Repo                     RepoRef
	Action                   string
	Ref                      string
	Before, After            string
	Created, Deleted, Forced bool
	CommitCount              int
	ChangedPaths             []string
	PathsComplete            bool
	HeadTreeOID              string
}

var _ = time.Second
