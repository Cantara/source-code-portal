package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/prereq"
	"github.com/cantara/source-code-portal/internal/search"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// lowRate is the rate-limit fraction below which the status warns.
const lowRate = 0.1

// status is the operator view on the admin listener: hand-written JSON that evolves with operations.
type status struct {
	ready, alive func() bool
	current      func() *snapshot.Snapshot
	depth        func() (waiting, running int)
	rate         func() map[string]float64
	webhook      bool                                         // events reach the portal (a receiver or the delivery log)
	receiver     bool                                         // GitHub can post to the portal
	catchUp      bool                                         // the Apps' delivery logs are read
	polled       map[string]bool                              // sources nothing tells about changes
	cadence      func(source string) map[string]time.Duration // each subject's refresh for a source
	layouts      func() []string                              // why stored layouts are not served
	prereqs      func() []prereq.Unmet
	cluster      interface {
		Leader() bool
		Connected() bool
	} // nil when the instance is alone

	mu         gosync.Mutex
	deliveries map[string]int // by result
	lastAt     time.Time
}

type statusBody struct {
	Ready    bool           `json:"ready"`
	Alive    bool           `json:"alive"`
	Snapshot statusSnapshot `json:"snapshot"`
	Queue    statusQueue    `json:"queue"`
	Sources  []statusSource `json:"sources"`
	Webhook  statusWebhook  `json:"webhook"`
	Cluster  statusCluster  `json:"cluster"`
	Search   search.Stats   `json:"search"`
	Warnings []string       `json:"warnings"`
}

type statusSnapshot struct {
	Version    uint64    `json:"version"`
	BuiltAt    time.Time `json:"builtAt"`
	AgeSeconds float64   `json:"ageSeconds"`
	Catalogued bool      `json:"catalogued"`
	Repos      int       `json:"repos"`
}

type statusQueue struct {
	Waiting int `json:"waiting"`
	Running int `json:"running"`
}

type statusSource struct {
	ID            string     `json:"id"`
	Repos         int        `json:"repos"`
	Freshness     string     `json:"freshness"`
	CheckedAt     *time.Time `json:"checkedAt"`
	RateRemaining float64    `json:"rateRemaining"`
	// Mode is how the source is kept fresh: events (GitHub tells the portal: the receiver, or the App's delivery log) or poll. Cadence
	// is each subject's refresh in seconds: under events it is the safety net, under poll how often it is asked.
	Mode    string             `json:"mode"`
	Cadence map[string]float64 `json:"cadenceSeconds"`
}

type statusCluster struct {
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
	Leader    bool `json:"leader"` // runs the sync jobs; true when the instance is alone
}

type statusWebhook struct {
	Enabled      bool           `json:"enabled"`
	Receiver     bool           `json:"receiver"`
	CatchUp      bool           `json:"catchUp"`
	Deliveries   map[string]int `json:"deliveries"`
	LastDelivery *time.Time     `json:"lastDelivery"`
}

// delivery counts a webhook delivery by result.
func (st *status) delivery(_, result string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.deliveries[result]++
	st.lastAt = time.Now()
}

func (st *status) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s, now := st.current(), time.Now()
	b := statusBody{Ready: st.ready(), Alive: st.alive(), Warnings: []string{}, Sources: []statusSource{},
		Snapshot: statusSnapshot{Version: s.Version, BuiltAt: s.BuiltAt, Catalogued: s.Catalogued, Repos: len(s.Repos)}}
	if !s.BuiltAt.IsZero() {
		b.Snapshot.AgeSeconds = now.Sub(s.BuiltAt).Seconds()
	}
	if ix := search.Of(s); ix != nil {
		b.Search = ix.Stats()
	}
	b.Queue.Waiting, b.Queue.Running = st.depth()
	rates := st.rate()
	for _, src := range s.Sources {
		ss := statusSource{ID: src.ID, Repos: src.Repos, Freshness: src.Fresh.Derive(now).String(), RateRemaining: rates[src.ID], Mode: "events",
			Cadence: map[string]float64{}}
		if st.polled[src.ID] {
			ss.Mode = "poll"
		}
		for kind, d := range st.cadence(src.ID) {
			ss.Cadence[kind] = d.Seconds()
		}
		if !src.Fresh.CheckedAt.IsZero() {
			at := src.Fresh.CheckedAt
			ss.CheckedAt = &at
		}
		if ss.Freshness != "fresh" {
			b.Warnings = append(b.Warnings, fmt.Sprintf("source %s: listing is %s", src.ID, ss.Freshness))
		}
		b.Sources = append(b.Sources, ss)
	}
	for id, f := range rates {
		if f < lowRate {
			b.Warnings = append(b.Warnings, fmt.Sprintf("source %s: %.0f%% of the GitHub rate limit left", id, f*100))
		}
	}
	b.Cluster.Leader = true
	if st.cluster != nil {
		b.Cluster = statusCluster{Enabled: true, Connected: st.cluster.Connected(), Leader: st.cluster.Leader()}
		if !b.Cluster.Connected {
			b.Warnings = append(b.Warnings, "cluster: not connected; serving the last good data, nothing is refreshed")
		}
	}
	if !st.webhook {
		b.Warnings = append(b.Warnings, "webhook: not configured; pushes show after the next scheduled listing (webhook.secret_file, or webhook.catch_up with a GitHub App)")
	}
	if st.layouts != nil {
		b.Warnings = append(b.Warnings, st.layouts()...)
	}
	if st.prereqs != nil {
		b.Warnings = append(b.Warnings, warnings(st.prereqs())...)
	}
	slices.Sort(b.Warnings)
	st.mu.Lock()
	b.Webhook = statusWebhook{Enabled: st.webhook, Receiver: st.receiver, CatchUp: st.catchUp, Deliveries: map[string]int{}}
	for k, v := range st.deliveries {
		b.Webhook.Deliveries[k] = v
	}
	if !st.lastAt.IsZero() {
		at := st.lastAt
		b.Webhook.LastDelivery = &at
	}
	st.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(b)
}
