package pulls

import (
	"slices"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Repo adds the open pull requests to a repository summary.
func Repo(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.Repo) {
	if n := Open(e); n >= 0 {
		r.OpenPulls = &n
	}
}

// ByWork is whose pull requests the work that counts holds: automation's only when it names automation.
func ByWork(w oapi.Work) By {
	if slices.Contains(w, oapi.WorkKindAutomation) {
		return ByAll
	}
	return ByPeople
}

// Dashboard adds the open pull requests of the fleet and of each group, of the work that counts.
func Dashboard(s *snapshot.Snapshot, d *oapi.Dashboard) {
	by := ByWork(d.Work)
	d.Pulls = Counts(All(s), by)
	for i := range d.Groups {
		d.Groups[i].Pulls = Counts(OfGroup(s, d.Groups[i].Id), by)
	}
}
