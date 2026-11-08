package github

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

func deliverySource(t *testing.T, srv *githubtest.Server) *Source {
	t.Helper()
	srv.Install("acme")
	c, err := New(config.GitHub{Sources: []config.Source{{
		ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"},
		App: &config.App{ID: srv.AppID, PrivateKey: config.NewSecret(srv.PrivateKeyPEM())},
	}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return c.Source("gh")
}

func logOf(n int, gap time.Duration, now time.Time) []githubtest.Delivery {
	var log []githubtest.Delivery // newest first
	for i := range n {
		log = append(log, githubtest.Delivery{GUID: fmt.Sprintf("g%d", i), Event: "push", RepositoryID: 1, At: now.Add(-time.Duration(i) * gap)})
	}
	return log
}

// A log of several pages is read to the end, and no further back than since.
func TestDeliveriesAreReadAcrossPages(t *testing.T) {
	srv := githubtest.New(t)
	now := time.Now().UTC().Truncate(time.Second)
	srv.SetDeliveries(logOf(450, time.Minute, now)...) // four and a half pages of 100
	s := deliverySource(t, srv)

	got, err := s.Deliveries(t.Context(), now.Add(-time.Hour))
	if err != nil || len(got) != 61 { // the minutes 0 to 60
		t.Fatalf("since an hour: %d deliveries, err %v", len(got), err)
	}
	got, err = s.Deliveries(t.Context(), now.Add(-72*time.Hour))
	if err != nil || len(got) != 450 || got[0].GUID != "g0" || got[449].GUID != "g449" {
		t.Fatalf("the whole log: %d deliveries, err %v", len(got), err)
	}
}

// A log longer than a read can be returns the newest deliveries and says so; it is not read as complete.
func TestADeliveryLogLongerThanARead(t *testing.T) {
	srv := githubtest.New(t)
	now := time.Now().UTC().Truncate(time.Second)
	srv.SetDeliveries(logOf(maxDeliveryPages*100+50, time.Second, now)...)
	s := deliverySource(t, srv)

	got, err := s.Deliveries(t.Context(), now.Add(-72*time.Hour))
	if !errors.Is(err, ErrLogTruncated) || len(got) != maxDeliveryPages*100 || got[0].GUID != "g0" {
		t.Fatalf("%d deliveries, err %v; want the newest %d and ErrLogTruncated", len(got), err, maxDeliveryPages*100)
	}
}
