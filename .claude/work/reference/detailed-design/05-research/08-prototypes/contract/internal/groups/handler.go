package groups

import (
	"context"
	"time"

	api "example.com/scp/internal/groups/api"
	openapi "example.com/scp/internal/httpapi/openapi"
)

type Handler struct{}

func (Handler) ListGroups(context.Context, api.ListGroupsRequestObject) (api.ListGroupsResponseObject, error) {
	now := time.Now().UTC()
	d := "Identity platform"
	return api.ListGroups200JSONResponse{Items: []api.Group{{Id: "identity", DisplayName: "Identity", Description: &d, RepoCount: 12}}, Freshness: openapi.Freshness{State: openapi.Fresh, FetchedAt: &now}}, nil
}

func (Handler) GetGroup(_ context.Context, r api.GetGroupRequestObject) (api.GetGroupResponseObject, error) {
	if r.GroupId != "identity" {
		return api.GetGroupdefaultApplicationProblemPlusJSONResponse{StatusCode: 404, Body: openapi.Problem{Type: "/problems/not-found", Title: "Group not found", Status: 404}}, nil
	}
	return api.GetGroup200JSONResponse{Id: "identity", DisplayName: "Identity", RepoCount: 12}, nil
}
