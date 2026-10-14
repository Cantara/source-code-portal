// Package liveapi stands in for the oapi-codegen output of internal/live/liveapi/live.yaml (schemas Snapshot, SseHello,
// SseInvalidate, SseResync of api/openapi.yaml). Hand-written here only so unit 15 compiles.
package liveapi

type Snapshot struct {
	Version   int64  `json:"version"`
	BuiltAt   string `json:"builtAt"`
	Readiness string `json:"readiness"`
	BuildID   string `json:"buildId"`
}

type SseHello struct {
	Generation int64  `json:"generation"`
	BuildID    string `json:"buildId"`
	Epoch      string `json:"epoch,omitempty"` // additive, deviation D-L3
}

type SseInvalidate struct {
	Topics     []string `json:"topics"`
	Generation int64    `json:"generation"`
}

type SseResync struct {
	Generation int64 `json:"generation"`
}
