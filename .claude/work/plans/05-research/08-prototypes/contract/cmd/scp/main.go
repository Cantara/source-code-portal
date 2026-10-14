package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	nethttpmw "github.com/oapi-codegen/nethttp-middleware"
	"time"

	"example.com/scp/internal/commits"
	commitsapi "example.com/scp/internal/commits/api"
	"example.com/scp/internal/groups"
	groupsapi "example.com/scp/internal/groups/api"
)

func main() {
	mux := http.NewServeMux()
	snap := func() []commitsapi.Commit {
		a := "octocat"
		return []commitsapi.Commit{{Sha: "0123456789abcdef0123456789abcdef01234567", Repo: "identity-core", Message: "init", AuthorLogin: &a, AuthoredAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}}
	}
	commitsapi.HandlerFromMux(commitsapi.NewStrictHandler(commits.Handler{Snap: snap}, nil), mux)
	groupsapi.HandlerFromMux(groupsapi.NewStrictHandler(groups.Handler{}, nil), mux)
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile("api/openapi.yaml")
	if err != nil {
		log.Fatal("load spec: ", err)
	}
	doc.Servers = nil // match any host
	if err := doc.Validate(loader.Context); err != nil {
		log.Fatal("validate spec: ", err)
	}
	validator := nethttpmw.OapiRequestValidatorWithOptions(doc, &nethttpmw.Options{ErrorHandler: func(w http.ResponseWriter, message string, statusCode int) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "/problems/invalid-request", "title": "Invalid request", "status": statusCode, "detail": message})
	}})
	srv := &http.Server{Addr: "127.0.0.1:9191", Handler: validator(mux), ReadHeaderTimeout: 5 * time.Second}
	_ = srv.ListenAndServe()
}
