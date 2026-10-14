// Package scm: STUB copied from architecture 8.1 (only what unit 14 needs).
package scm

import (
	"errors"
	"time"
)

type SourceID string
type RepoID int64
type Slug string
type GroupID string

type RepoRef struct {
	Source SourceID
	ID     RepoID
	Owner  string
	Name   string
}

func (r RepoRef) Key() string { return string(r.Source) + "/" + r.Owner + "/" + r.Name }

type Visibility uint8

const (
	VisPublic Visibility = iota
	VisInternal
	VisPrivate
)

type ViewClass uint8

const (
	ViewPublic ViewClass = iota
	ViewAll
)

func (v Visibility) Visible(c ViewClass) bool { return v == VisPublic || c == ViewAll }

type Repository struct {
	Ref           RepoRef
	Slug          Slug
	Description   string
	DefaultBranch string
	Topics        []string
	Visibility    Visibility
	Archived      bool
	Fork          bool
	HTMLURL       string
	CreatedAt     time.Time
	PushedAt      time.Time
}

type GroupMeta struct {
	ID           GroupID
	DisplayName  string
	Description  string
	Integrations map[string]map[string]string
}

// Membership is immutable. STUB constructor NewMembership exists only in the scratch module.
type Membership struct {
	groups  []GroupMeta
	byRepo  map[Slug][]GroupID
	byGroup map[GroupID][]Slug
}

func NewMembership(groups []GroupMeta, byGroup map[GroupID][]Slug) Membership {
	m := Membership{groups: groups, byGroup: byGroup, byRepo: map[Slug][]GroupID{}}
	for _, g := range groups {
		for _, s := range byGroup[g.ID] {
			m.byRepo[s] = append(m.byRepo[s], g.ID)
		}
	}
	return m
}
func (m Membership) GroupsOf(s Slug) []GroupID { return m.byRepo[s] }
func (m Membership) ReposOf(g GroupID) []Slug  { return m.byGroup[g] }
func (m Membership) Groups() []GroupMeta       { return m.groups }

var ErrNotFound = errors.New("scm: not found")
var ErrForbidden = errors.New("scm: forbidden")
var ErrUnavailable = errors.New("scm: unavailable")
