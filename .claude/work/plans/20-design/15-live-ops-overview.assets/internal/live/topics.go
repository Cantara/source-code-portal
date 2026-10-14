package live

// Topic names are the InvalidationTopic enum of the API (open enum). Canonical output order:
const (
	TopicSite      = "site"
	TopicCatalog   = "catalog"
	TopicCommits   = "commits"
	TopicDocs      = "docs"
	TopicManifests = "manifests"
	TopicPages     = "pages"
	TopicStatus    = "status"
)

func contentTopics() []string {
	return []string{TopicSite, TopicCatalog, TopicCommits, TopicDocs, TopicManifests, TopicPages}
}

// topicsOfName maps a registry slot or global name to the query families it feeds. An unknown name maps to every
// content topic (conservative: an extra refetch is a 304, a missed one is a stale screen); cmd/scp's test
// TestLiveTopicsCoverRegistry fails the build when the registry contains a name that this switch does not know.
func topicsOfName(name string) (topics []string, known bool) {
	switch name {
	case "catalog.repo", "catalog.stats", "catalog.membership", "catalog.aliases", "catalog.group_aliases", "integrations.status":
		return []string{TopicCatalog}, true
	case "catalog.sync":
		// admin-class status of the reconcile; its only public effect is site.meta.lastFullSync, which has its own name.
		// No content topic: a public stream advances silently, an admin stream gets "status".
		return nil, true
	case "commits.view", "commits.latest":
		return []string{TopicCommits}, true
	case "docs.repo":
		return []string{TopicDocs}, true
	case "manifests.repo", "manifests.graph":
		return []string{TopicManifests}, true
	case "pages.set":
		return []string{TopicPages, TopicSite}, true // the announcement and the nav live in pages and feed api.site
	case "site.meta":
		return []string{TopicSite}, true
	}
	return contentTopics(), false
}

// KnownName reports whether the topic mapping knows a registry name (used by the registry-coverage test).
func KnownName(name string) bool { _, ok := topicsOfName(name); return ok }

// Topics folds change sets into the sorted topic list of one invalidate event. resync is true when any set asks for it.
// A set that names repositories but no slots (a readmodel that predates ChangeSet.Slots) maps to every content topic.
// withStatus adds the "status" topic (admin listener streams only).
func Topics(cs []ChangeSet, withStatus bool) (topics []string, resync bool) {
	in := map[string]bool{}
	for _, c := range cs {
		if c.Resync {
			return nil, true
		}
		if len(c.Repos) > 0 && len(c.Slots) == 0 {
			for _, t := range contentTopics() {
				in[t] = true
			}
		}
		for _, n := range c.Slots {
			ts, _ := topicsOfName(n)
			for _, t := range ts {
				in[t] = true
			}
		}
		for _, n := range c.Globals {
			ts, _ := topicsOfName(n)
			for _, t := range ts {
				in[t] = true
			}
		}
	}
	if withStatus {
		in[TopicStatus] = true
	}
	for _, t := range append(contentTopics(), TopicStatus) {
		if in[t] {
			topics = append(topics, t)
		}
	}
	return topics, false
}
