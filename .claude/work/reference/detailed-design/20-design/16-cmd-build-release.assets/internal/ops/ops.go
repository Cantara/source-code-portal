// Package ops is a STUB with the exact exported surface the bridges use (15 section 3.2, views.go): the Retrier interface and the
// three errors the Retrier bridge returns. The union module compiles cmd/scp against the real package.
package ops

type Retrier interface{ Retry(key string) error }

type retryError string

func (e retryError) Error() string { return string(e) }

const (
	ErrUnknownKey retryError = "ops: unknown job kind"
	ErrQueueFull  retryError = "ops: job queue full"
	ErrClosed     retryError = "ops: engine closed"
)
