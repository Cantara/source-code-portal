package github

import (
	"context"
	"errors"
	"fmt"
	"time"

	gh "github.com/google/go-github/v93/github"
)

// Delivery is one line of the App's webhook delivery log: what GitHub sent, and what the portal answered.
type Delivery struct {
	GUID         string // the X-GitHub-Delivery id
	Event        string
	Action       string
	RepositoryID int64 // 0 for an event about no repository (an installation)
	At           time.Time
	Status       int // the answer's HTTP status; 0 when none came (the portal was down or too slow)
}

// OK says the portal answered the delivery with a success.
func (d Delivery) OK() bool { return d.Status >= 200 && d.Status < 300 }

// ErrNoDeliveryLog is returned in token mode: only an App has a delivery log.
var ErrNoDeliveryLog = errors.New("github: no webhook delivery log without an App")

// ErrLogTruncated says the log reaches further back than Deliveries read: the deliveries returned with it are the newest ones, and
// older ones, down to since, were not read.
var ErrLogTruncated = errors.New("github: the webhook delivery log is longer than was read")

// maxDeliveryPages bounds a read at 10,000 deliveries, about the three days GitHub keeps for a busy organisation. A round of the
// catch-up reads one page; only a long outage reads more.
const maxDeliveryPages = 100

// Deliveries reads the App's webhook delivery log, newest first, back to since (GET /app/hook/deliveries with the App's JWT; a read).
// It is how the portal learns of a delivery it never received, or answered with a failure, while it was down or restarting. When the
// log is longer than maxDeliveryPages it returns the newest deliveries with ErrLogTruncated.
func (s *Source) Deliveries(ctx context.Context, since time.Time) ([]Delivery, error) {
	if s.app == nil {
		return nil, ErrNoDeliveryLog
	}
	var out []Delivery
	opts := &gh.ListCursorOptions{PerPage: perPage}
	for range maxDeliveryPages {
		got, resp, err := s.appClient.Apps.ListHookDeliveries(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list webhook deliveries: %w", err)
		}
		for _, d := range got {
			at := d.GetDeliveredAt().UTC()
			if at.Before(since) {
				return out, nil
			}
			out = append(out, Delivery{GUID: d.GetGUID(), Event: d.GetEvent(), Action: d.GetAction(), RepositoryID: d.GetRepositoryID(),
				At: at, Status: d.GetStatusCode()})
		}
		if resp.Cursor == "" {
			return out, nil
		}
		opts.Cursor = resp.Cursor
	}
	return out, ErrLogTruncated
}
