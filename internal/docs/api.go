package docs

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves the documents from the current snapshot. It implements the docs methods of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

func notFound(detail string) oapi.NotFoundApplicationProblemPlusJSONResponse {
	return oapi.NotFoundApplicationProblemPlusJSONResponse(oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))
}

func (a API) ListDocs(ctx context.Context, req oapi.ListDocsRequestObject) (oapi.ListDocsResponseObject, error) {
	e, moved := a.Current(ctx).Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.ListDocs404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such repository")}, nil
	case moved:
		return oapi.ListDocs308Response{Headers: oapi.MovedResponseHeaders{Location: "/api/v1/repos/" + string(e.Repo.Key()) + "/docs"}}, nil
	}
	docs, fresh := Of(e)
	out := oapi.DocList{Docs: make([]oapi.DocSummary, 0, len(docs)), Freshness: oapi.FreshnessOf(fresh, time.Now())}
	for _, d := range docs {
		if !d.Hidden {
			out.Docs = append(out.Docs, oapi.DocSummary{Id: d.Path, Path: d.Path, Readme: d.README, Title: d.Title, Description: d.Description,
				Format: oapi.DocSummaryFormat(d.Format), State: oapi.DocSummaryState(d.State), Truncated: d.Truncated})
		}
	}
	return oapi.ListDocs200JSONResponse{Body: out}, nil
}

func (a API) GetDoc(ctx context.Context, req oapi.GetDocRequestObject) (oapi.GetDocResponseObject, error) {
	e, moved := a.Current(ctx).Repo(snapshot.KeyOf(req.Owner, req.Name))
	switch {
	case e == nil:
		return oapi.GetDoc404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such repository")}, nil
	case moved:
		return oapi.GetDoc308Response{Headers: oapi.MovedResponseHeaders{
			Location: "/api/v1/repos/" + string(e.Repo.Key()) + "/docs/" + url.PathEscape(req.DocId)}}, nil
	}
	docs, fresh := Of(e)
	if d, ok := find(docs, req.DocId); ok {
		toc := make([]oapi.Heading, 0, len(d.TOC))
		for _, h := range d.TOC {
			toc = append(toc, oapi.Heading{Level: h.Level, Id: h.ID, Text: h.Text})
		}
		return oapi.GetDoc200JSONResponse{Body: oapi.Doc{Id: d.Path, Path: d.Path, Readme: d.README, Title: d.Title, Description: d.Description,
			Format: oapi.DocFormat(d.Format), State: oapi.DocState(d.State), Truncated: d.Truncated, Html: d.HTML.String(), Toc: toc,
			BlobSha: d.BlobSHA, SourceUrl: e.Repo.HTMLURL + "/blob/HEAD/" + escapePath(d.Path),
			Freshness: oapi.FreshnessOf(fresh, time.Now())}}, nil
	}
	return oapi.GetDoc404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("no such document")}, nil
}

// find is the document at id, or else the one whose path without its extension is id (the address rendered links use).
func find(docs []Doc, id string) (Doc, bool) {
	for _, d := range docs {
		if d.Path == id {
			return d, true
		}
	}
	for _, d := range docs {
		if strings.TrimSuffix(d.Path, path.Ext(d.Path)) == id {
			return d, true
		}
	}
	return Doc{}, false
}
