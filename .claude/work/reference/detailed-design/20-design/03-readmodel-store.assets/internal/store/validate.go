package store

import (
	"encoding/json"
	"strconv"
)

// MaxPayload bounds Item.Payload, CommitRow.Payload and Repo.Meta (a sanity bound; feature caps are far lower).
const MaxPayload = 16 << 20

// ValidateItem is shared by every implementation so they reject the same inputs.
func ValidateItem(it Item) error {
	if err := it.Key.Validate(); err != nil {
		return err
	}
	if !it.State.HasResult() { // freshness.State.HasResult: ok, not_found, unsupported, too_large, no_content
		return invalid("state", "PutItem accepts only ok, not_found, unsupported, too_large, no_content")
	}
	if it.FetchedAt.IsZero() {
		return invalid("fetched_at", "required for a result state")
	}
	if it.CheckedAt.IsZero() {
		return invalid("checked_at", "required")
	}
	if len(it.Payload) > MaxPayload {
		return ErrTooLarge
	}
	return nil
}

func ValidateFailure(k ItemKey, f Failure) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if f.Code == "" || len(f.Code) > 64 {
		return invalid("failure.code", "required, at most 64 bytes")
	}
	if f.At.IsZero() {
		return invalid("failure.at", "required")
	}
	return nil
}

func ValidateRepo(r Repo) error {
	if err := r.Key.Validate(); err != nil {
		return err
	}
	if err := validSegment("repo.slug", r.Slug); err != nil {
		return err
	}
	if r.SeenAt.IsZero() {
		return invalid("repo.seen_at", "required")
	}
	if len(r.Meta) == 0 || len(r.Meta) > MaxPayload {
		return invalid("repo.meta", "required JSON object, at most 16 MiB")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r.Meta, &obj); err != nil || obj == nil {
		return invalid("repo.meta", "must be a JSON object")
	}
	return nil
}

// ValidatePlan checks one planner row (a zero RepoKey is a global kind and is skipped by SavePlans, not rejected).
func ValidatePlan(p PlanRow) error {
	if !p.Key.Repo.IsZero() {
		if err := p.Key.Validate(); err != nil {
			return err
		}
	}
	if p.UnchangedStreak < 0 {
		return invalid("plan.unchanged_streak", "must be >= 0")
	}
	return nil
}

// ValidateCommit: sha and time required; the payload (the commits feature's DTO) must be non-empty, because a commit
// without a body is useless and the schema column is NOT NULL (verification V-03).
func ValidateCommit(c CommitRow) error {
	if c.SHA == "" || len(c.SHA) > 128 {
		return invalid("commit.sha", "required, at most 128 bytes")
	}
	if c.CommittedAt.IsZero() {
		return invalid("commit.committed_at", "required")
	}
	if len(c.Payload) == 0 {
		return invalid("commit.payload", "required")
	}
	if len(c.Payload) > MaxPayload {
		return ErrTooLarge
	}
	return nil
}

// ValidateCatalogue checks every row and the uniqueness the schema enforces (key, slug, non-zero GitHub id).
func ValidateCatalogue(repos []Repo) error {
	keys := make(map[RepoKey]struct{}, len(repos))
	slugs := make(map[string]struct{}, len(repos))
	gids := make(map[[2]string]struct{}, len(repos))
	for _, r := range repos {
		if err := ValidateRepo(r); err != nil {
			return err
		}
		if _, dup := keys[r.Key]; dup {
			return invalid("catalogue", "duplicate repository "+r.Key.String())
		}
		keys[r.Key] = struct{}{}
		if _, dup := slugs[r.Slug]; dup {
			return invalid("catalogue", "duplicate slug "+r.Slug)
		}
		slugs[r.Slug] = struct{}{}
		if r.GitHubID != 0 {
			g := [2]string{r.Key.Source, strconv.FormatInt(r.GitHubID, 10)}
			if _, dup := gids[g]; dup {
				return invalid("catalogue", "duplicate github id for "+r.Key.String())
			}
			gids[g] = struct{}{}
		}
	}
	return nil
}

// ValidateCacheArgs: namespace and key bounds shared by implementations.
func ValidateCacheArgs(ns, key string) error {
	if ns == "" || len(ns) > 64 {
		return invalid("cache.ns", "required, at most 64 bytes")
	}
	if key == "" || len(key) > 512 {
		return invalid("cache.key", "required, at most 512 bytes")
	}
	return nil
}
