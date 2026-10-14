package httpapi

import "time"

// Metrics records the HTTP metrics.
type Metrics struct{}

func (m *Metrics) observeRequest(listener, pattern, method string, status int, d time.Duration) {}
