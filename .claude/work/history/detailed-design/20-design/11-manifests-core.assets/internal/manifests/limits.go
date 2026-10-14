package manifests

import "github.com/cantara/source-code-portal/internal/manifests/internal/limits"

// Limits bound one Parse call; the type lives in internal/limits so ecosystems and the harness can share it.
type Limits = limits.Limits

// LimitError is the typed error parsers return (or wrap) when a Guard check fails.
type LimitError = limits.LimitError
