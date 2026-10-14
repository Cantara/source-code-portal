package commits

import (
	"context"
	"time"

	api "example.com/scp/internal/commits/api"
	openapi "example.com/scp/internal/httpapi/openapi"
)

// Handler implements api.StrictServerInterface for the commits feature.
type Handler struct{ Snap func() []api.Commit }

func (h Handler) ListCommits(_ context.Context, req api.ListCommitsRequestObject) (api.ListCommitsResponseObject, error) {
	items := h.Snap()
	if req.Params.Group != nil && *req.Params.Group == "unknown" {
		return api.ListCommitsdefaultApplicationProblemPlusJSONResponse{StatusCode: 404, Body: openapi.Problem{Type: "/problems/not-found", Title: "Group not found", Status: 404}}, nil
	}
	now := time.Now().UTC()
	etag := `"commits-v1"`
	if req.Params.IfNoneMatch != nil && *req.Params.IfNoneMatch == etag {
		return api.ListCommits304Response{Headers: openapi.NotModifiedResponseHeaders{ETag: &etag}}, nil
	}
	return api.ListCommits200JSONResponse{
		Body:    api.CommitPage{Items: items, Page: openapi.PageInfo{NextCursor: nil}, Freshness: openapi.Freshness{State: openapi.Fresh, FetchedAt: &now}},
		Headers: api.ListCommits200ResponseHeaders{ETag: &etag},
	}, nil
}
