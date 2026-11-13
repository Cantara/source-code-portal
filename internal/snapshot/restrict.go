package snapshot

import "log/slog"

// Restrict returns the view of the published snapshot s that holds only the repositories allowed names, with the hidden forks when
// forks is true: the view one person reads when what they may see is a list of their own (scp enterprise). It is built by the same
// machinery as the other views (aliases, sources and every feature's index over the kept repositories only), and holds only the groups
// that keep a member, live or retired. A nil or empty allowed gives the empty view. An index that cannot be built is not replaced by the full snapshot's: the build panics, and the request
// fails rather than shows more.
//
// A restricted view is its own public view: what one person reads is never another's, and the enterprise mode has no anonymous reader.
func (s *Snapshot) Restrict(allowed map[RepoKey]bool, forks bool, log *slog.Logger) *Snapshot {
	if s.fat != nil {
		s = s.fat
	}
	fat := s.cut(func(e *RepoEntry) bool { return allowed[e.Repo.Key()] }, nil, true, log)
	if fat == s { // every repository allowed: the full snapshot's own views are the restricted ones
		return s.View(true, forks)
	}
	lean := fat
	for k := range fat.Repos {
		if s.hidden[k] {
			lean = fat.cut(func(e *RepoEntry) bool { return !s.hidden[e.Repo.Key()] }, nil, true, log)
			break
		}
	}
	fat.public, fat.lean, fat.fat, fat.hidden = fat, lean, fat, s.hidden
	lean.public, lean.lean, lean.fat, lean.hidden = lean, lean, fat, s.hidden
	if forks {
		return fat
	}
	return lean
}
