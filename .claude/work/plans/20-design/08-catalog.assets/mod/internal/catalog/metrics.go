package catalog

import (
	"go.opentelemetry.io/otel/metric"
)

// Metric names are constants next to their instruments (rule G11); a cmd/scp test compares them with docs/reference/metrics.md.
const (
	mRepositories   = "scp_catalog_repositories"       // gauge{source,state=tracked|quarantined|held}
	mExcluded       = "scp_catalog_excluded"           // gauge{source,reason}
	mGroups         = "scp_catalog_groups"             // gauge{state=ok|degraded}
	mGroupMembers   = "scp_catalog_group_members"      // gauge{group}
	mReconcile      = "scp_catalog_reconcile_total"    // counter{source,result=ok|failed|held|skipped}
	mReconcileSecs  = "scp_catalog_reconcile_seconds"  // histogram{source}
	mListPages      = "scp_catalog_list_pages_total"   // counter{source,result=200|304}
	mChanges        = "scp_catalog_changes_total"      // counter{source,change=added|removed|renamed|updated}
	mHeadsProbe     = "scp_catalog_heads_probe_total"  // counter{result=ok|error|missing}
	mHeadsSecs      = "scp_catalog_heads_seconds"      // histogram{}
	mHeadChanges    = "scp_catalog_head_changes_total" // counter{}
	mHeadEvents     = "scp_catalog_head_events_total"  // counter{result=emitted|error}
	mReact          = "scp_catalog_react_total"        // counter{kind,result=enqueued|hidden|ignored|unknown_repo|error}
	mMembershipSecs = "scp_catalog_membership_build_seconds"
	mPersistErrors  = "scp_catalog_store_errors_total" // counter{op}
)

type metrics struct {
	reconcile, listPages, changes, headsProbe, headChanges, headEvents, react, persistErr metric.Int64Counter
	reconcileSecs, headsSecs, membershipSecs                                              metric.Float64Histogram
	repositories, excluded, groups, groupMembers                                          metric.Int64Gauge
}

func newMetrics(m metric.Meter) (x metrics, err error) {
	ctr := func(name, desc string) metric.Int64Counter {
		if err != nil {
			return nil
		}
		var c metric.Int64Counter
		c, err = m.Int64Counter(name, metric.WithDescription(desc))
		return c
	}
	hist := func(name, desc string) metric.Float64Histogram {
		if err != nil {
			return nil
		}
		var h metric.Float64Histogram
		h, err = m.Float64Histogram(name, metric.WithDescription(desc), metric.WithUnit("s"))
		return h
	}
	gauge := func(name, desc string) metric.Int64Gauge {
		if err != nil {
			return nil
		}
		var g metric.Int64Gauge
		g, err = m.Int64Gauge(name, metric.WithDescription(desc))
		return g
	}
	x.reconcile = ctr(mReconcile, "Reconcile runs by source and result")
	x.listPages = ctr(mListPages, "Repository list pages fetched, by HTTP result")
	x.changes = ctr(mChanges, "Repositories added, removed, renamed or updated by a reconcile")
	x.headsProbe = ctr(mHeadsProbe, "Repositories probed by the head sweep, by result")
	x.headChanges = ctr(mHeadChanges, "Repositories whose HEAD changed since the last probe")
	x.headEvents = ctr(mHeadEvents, "Synthetic head_changed events handed to the reactor fan")
	x.react = ctr(mReact, "Webhook events handled by catalog.React")
	x.persistErr = ctr(mPersistErrors, "Store write failures of the catalogue")
	x.reconcileSecs = hist(mReconcileSecs, "Duration of one source reconcile")
	x.headsSecs = hist(mHeadsSecs, "Duration of one head sweep")
	x.membershipSecs = hist(mMembershipSecs, "Duration of one membership build")
	x.repositories = gauge(mRepositories, "Repositories by source and state")
	x.excluded = gauge(mExcluded, "Listed repositories excluded by policy, by reason")
	x.groups = gauge(mGroups, "Groups by health")
	x.groupMembers = gauge(mGroupMembers, "Members per group")
	return x, err
}
