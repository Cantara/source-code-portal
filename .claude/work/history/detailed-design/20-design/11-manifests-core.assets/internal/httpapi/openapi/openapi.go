// Package openapi is a STUB of the generated common types (unit 07, api/common.yaml): only what internal/manifests names.
package openapi

import "time"

type Freshness struct {
	Status        string     `json:"status"`
	Kind          string     `json:"kind"`
	AsOf          *time.Time `json:"asOf"`
	FetchedAt     *time.Time `json:"fetchedAt"`
	CheckedAt     *time.Time `json:"checkedAt"`
	StaleSince    *time.Time `json:"staleSince"`
	LastErrorCode *string    `json:"lastErrorCode"`
}
type PageInfo struct {
	NextCursor *string `json:"nextCursor"`
	Limit      int     `json:"limit"`
}
type LanguageShare struct {
	Name  string  `json:"name"`
	Bytes int64   `json:"bytes"`
	Share float32 `json:"share"` // the generated type is float32 (verified by generating from common.yaml)
}
