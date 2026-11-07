package catalog

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Retired says why a repository is an archive entry (design/archive.md): archived on GitHub, else deprecated by its scp.yaml; "" when it is
// neither. A quiet repository is dormant, not retired.
func Retired(e *snapshot.RepoEntry) oapi.ArchiveReason {
	if e.Repo.Archived {
		return oapi.ArchiveReasonArchived
	}
	if d, _ := DescriptorOf(e); d.Lifecycle == "deprecated" {
		return oapi.ArchiveReasonDeprecated
	}
	return ""
}

func archiveBadRequest(detail string) oapi.ListArchive400ApplicationProblemPlusJSONResponse {
	return oapi.ListArchive400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: oapi.BadRequestApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusBadRequest, "validation-failed", "Bad Request", detail))}
}

// ListArchive lists the retired repositories of the viewer's view with the rows the ArchiveEntry hooks complete.
func (a API) ListArchive(ctx context.Context, req oapi.ListArchiveRequestObject) (oapi.ListArchiveResponseObject, error) {
	s, now, p := a.Current(ctx), time.Now(), req.Params
	number, size, bad := scope.Params(p.Page, p.Limit, scope.DefaultLimit)
	switch {
	case bad != "":
		return archiveBadRequest(bad), nil
	case p.Reason != nil && !slices.Contains([]oapi.ArchiveReason{oapi.ArchiveReasonArchived, oapi.ArchiveReasonDeprecated}, *p.Reason):
		return archiveBadRequest("unknown reason"), nil
	}
	keys := make([]snapshot.RepoKey, 0, len(s.Repos))
	if p.Group != nil {
		g, ok := s.Group(*p.Group)
		if !ok {
			return oapi.ListArchive404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such group")}, nil
		}
		keys = append(keys, g.Members...)
	} else {
		for k := range s.Repos {
			keys = append(keys, k)
		}
	}
	q := ""
	if p.Q != nil {
		q = strings.ToLower(strings.TrimSpace(*p.Q))
	}
	rows := make([]oapi.ArchiveEntry, 0, len(keys))
	var in []snapshot.RepoKey
	var counts oapi.ArchiveCounts
	for _, k := range keys {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		why := Retired(e)
		if why == "" || !strings.Contains(strings.ToLower(e.Repo.Name), q) {
			continue
		}
		in = append(in, k)
		row := oapi.ArchiveEntry{Repo: a.repo(s, e, now), Reason: why, LastActive: e.Repo.PushedAt.UTC()}
		for _, fn := range a.ArchiveEntry {
			fn(s, e, &row)
		}
		counts.Total++
		if why == oapi.ArchiveReasonArchived {
			counts.Archived++
		} else {
			counts.Deprecated++
		}
		if row.UsedBy > 0 {
			counts.Used++
		}
		if p.Reason != nil && why != *p.Reason || p.Used != nil && *p.Used && row.UsedBy == 0 {
			continue
		}
		rows = append(rows, row)
	}
	slices.SortStableFunc(rows, archiveOrder(p.Sort))
	start, end, pg := scope.Paging(len(rows), number, size)
	return oapi.ListArchive200JSONResponse{Body: oapi.ArchivePage{Archive: rows[start:end], Counts: counts, Page: pg,
		Freshness: scope.Freshness(s, in, now, func(e *snapshot.RepoEntry) snapshot.Freshness { return e.Fresh })}}, nil
}

// archiveOrder is the order the sort names: last active first by default; ties go by name so a page is stable.
func archiveOrder(sort *oapi.ArchiveSort) func(x, y oapi.ArchiveEntry) int {
	byName := func(x, y oapi.ArchiveEntry) int { return cmp.Compare(x.Repo.Address, y.Repo.Address) }
	byActive := func(x, y oapi.ArchiveEntry) int { return cmp.Or(y.LastActive.Compare(x.LastActive), byName(x, y)) }
	if sort == nil {
		return byActive
	}
	switch *sort {
	case oapi.ArchiveSortName:
		return byName
	case oapi.ArchiveSortGroup:
		return func(x, y oapi.ArchiveEntry) int {
			return cmp.Or(cmp.Compare(firstGroup(x), firstGroup(y)), byName(x, y))
		}
	case oapi.ArchiveSortUsed:
		return func(x, y oapi.ArchiveEntry) int { return cmp.Or(cmp.Compare(y.UsedBy, x.UsedBy), byActive(x, y)) }
	}
	return byActive
}

// firstGroup is the id of the first group a repository belongs to; repositories in none sort last.
func firstGroup(r oapi.ArchiveEntry) string {
	if len(r.Repo.Groups) == 0 {
		return "￿"
	}
	return r.Repo.Groups[0]
}
