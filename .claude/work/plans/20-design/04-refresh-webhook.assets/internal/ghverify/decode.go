package ghverify

import (
	"encoding/json"
	"net/http"

	"github.com/cantara/source-code-portal/internal/scm"
)

const maxCommitsInPayload = 2048 // GitHub caps push.commits at 2,048 (docs, verified 2026-10-14)
const maxChangedPaths = 20000

type pushPayload struct {
	Ref     string `json:"ref"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Created bool   `json:"created"`
	Deleted bool   `json:"deleted"`
	Forced  bool   `json:"forced"`
	Commits []struct {
		ID       string    `json:"id"`
		TreeID   string    `json:"tree_id"`
		Added    *[]string `json:"added"`
		Removed  *[]string `json:"removed"`
		Modified *[]string `json:"modified"`
	} `json:"commits"`
	HeadCommit *struct {
		TreeID string `json:"tree_id"`
	} `json:"head_commit"`
	Repository struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// otherPayload is the common shape of release, create, repository, installation and installation_repositories.
type otherPayload struct {
	Action     string `json:"action"`
	Ref        string `json:"ref"`      // create: the plain name, not a full ref
	RefType    string `json:"ref_type"` // create: "branch" or "tag" (others: "repository")
	Repository *struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Release *struct {
		TagName string `json:"tag_name"`
	} `json:"release"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (p *otherPayload) repo(src scm.SourceID) scm.RepoRef {
	if p.Repository == nil {
		return scm.RepoRef{}
	}
	return scm.RepoRef{Source: src, ID: scm.RepoID(p.Repository.ID), Owner: p.Repository.Owner.Login, Name: p.Repository.Name}
}

// Verify is the reference of github.Verifier.Verify: signature first, decode second, typed errors.
func Verify(src scm.SourceID, secrets [][]byte, h http.Header, body []byte) (scm.Event, error) {
	if err := VerifySignature(h.Get("X-Hub-Signature-256"), body, secrets...); err != nil {
		return scm.Event{}, err
	}
	delivery := h.Get("X-GitHub-Delivery")
	if h.Get("X-GitHub-Event") == "" {
		return scm.Event{}, &Error{"malformed", "missing X-GitHub-Event"}
	}
	switch kind := h.Get("X-GitHub-Event"); kind {
	case "ping":
		return scm.Event{Kind: scm.EventPing, Delivery: delivery, Source: src}, nil
	case "push":
		var p pushPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return scm.Event{}, &Error{"malformed", "push payload is not valid JSON"}
		}
		ev := scm.Event{Kind: scm.EventPush, Delivery: delivery, Source: src,
			Installation: scm.InstallationID(p.Installation.ID),
			Ref:          p.Ref, Before: p.Before, After: p.After, Created: p.Created, Deleted: p.Deleted, Forced: p.Forced,
			CommitCount: len(p.Commits), PathsComplete: len(p.Commits) < maxCommitsInPayload}
		owner := p.Repository.Owner.Login
		if owner == "" {
			owner = p.Repository.Owner.Name // push payloads carry owner.name, other events owner.login
		}
		ev.Repo = scm.RepoRef{Source: src, ID: scm.RepoID(p.Repository.ID), Owner: owner, Name: p.Repository.Name}
		if p.HeadCommit != nil {
			ev.HeadTreeOID = p.HeadCommit.TreeID
		}
		seen := map[string]struct{}{}
		for _, c := range p.Commits {
			if c.Added == nil || c.Removed == nil || c.Modified == nil {
				ev.PathsComplete = false // the lists are undocumented (V13): absent means "unknown", never "empty"
				continue
			}
			for _, list := range [][]string{*c.Added, *c.Removed, *c.Modified} {
				for _, path := range list {
					if _, dup := seen[path]; !dup && len(seen) < maxChangedPaths {
						seen[path] = struct{}{}
						ev.ChangedPaths = append(ev.ChangedPaths, path)
					} else if !dup {
						ev.PathsComplete = false
					}
				}
			}
		}
		// A push whose changed paths cannot be known: a forced push (history rewritten, the commit list names only
		// what is new), or a head that moved without any listed commit (GitHub omits commits above its caps).
		if !p.Deleted && (p.Forced || len(p.Commits) == 0) {
			ev.PathsComplete = false
		}
		return ev, nil
	case "release", "create", "repository", "installation", "installation_repositories":
		var p otherPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return scm.Event{}, &Error{"malformed", kind + " payload is not valid JSON"}
		}
		ev := scm.Event{Delivery: delivery, Source: src, Installation: scm.InstallationID(p.Installation.ID), Action: p.Action}
		switch kind {
		case "release":
			ev.Kind, ev.Repo = scm.EventRelease, p.repo(src)
			if p.Release != nil && p.Release.TagName != "" {
				ev.Ref = "refs/tags/" + p.Release.TagName
			}
		case "create":
			ev.Kind, ev.Repo, ev.Created = scm.EventCreate, p.repo(src), true
			switch p.RefType {
			case "branch":
				ev.Ref = "refs/heads/" + p.Ref
			case "tag":
				ev.Ref = "refs/tags/" + p.Ref
			}
		case "repository":
			ev.Kind, ev.Repo = scm.EventRepository, p.repo(src)
		case "installation":
			ev.Kind = scm.EventInstallation
		default:
			ev.Kind = scm.EventInstallationRepos
		}
		if ev.Kind != scm.EventInstallation && ev.Kind != scm.EventInstallationRepos && ev.Repo.ID == 0 {
			return scm.Event{}, &Error{"malformed", kind + " payload has no repository"}
		}
		return ev, nil
	default:
		return scm.Event{Delivery: delivery}, &Error{"ignored", "event " + kind + " is not handled"}
	}
}
