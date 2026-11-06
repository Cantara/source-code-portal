package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	gh "github.com/google/go-github/v93/github"
)

// GraphQL posts query with vars to the GraphQL endpoint beside c's REST base and decodes the answer's data into out. An answer
// with errors and no data is a Transient error coded "graphql" (or "graphql_rate_limited"): GraphQL has its own rate limit, so a
// caller treats it as a missing extra and never pauses the source. An answer with data and errors (an alias GitHub does not
// resolve) decodes the data and drops the errors.
func GraphQL(ctx context.Context, c *gh.Client, query string, vars map[string]any, out any) error {
	base, err := url.Parse(c.BaseURL())
	if err != nil {
		return err
	}
	req, err := c.NewRequest(ctx, http.MethodPost, graphQLEndpoint(base), map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	resp, err := c.Do(req, &answer)
	if err != nil {
		return Classify(resp, err)
	}
	if d := bytes.TrimSpace(answer.Data); len(d) == 0 || bytes.Equal(d, []byte("null")) {
		code, msg := "graphql", "no data"
		if len(answer.Errors) > 0 {
			msg = answer.Errors[0].Type + ": " + answer.Errors[0].Message
			if answer.Errors[0].Type == "RATE_LIMITED" {
				code = "graphql_rate_limited"
			}
		}
		return &Error{Kind: Transient, Code: code, Err: errors.New(msg)}
	}
	if err := json.Unmarshal(answer.Data, out); err != nil {
		return &Error{Kind: Permanent, Code: "malformed", Err: err}
	}
	return nil
}

// graphQLEndpoint is the GraphQL endpoint beside a REST base: api.github.com/graphql, and /api/graphql on GitHub Enterprise Server,
// whose REST base is /api/v3/.
func graphQLEndpoint(base *url.URL) string {
	u := *base
	if strings.HasSuffix(u.Path, "/api/v3/") {
		u.Path = strings.TrimSuffix(u.Path, "v3/") + "graphql"
	} else {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/graphql"
	}
	return u.String()
}
