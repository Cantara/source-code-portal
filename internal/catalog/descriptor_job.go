package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

// DescriptorKind is the job kind that reads a repository's scp.yaml.
const DescriptorKind = "catalog.descriptor"

const descriptorTTL = 24 * time.Hour // pushes notify the job

// descriptorNames are the accepted file names in one location.
var descriptorNames = [...]string{"scp.yaml", "scp.yml"} //nolint:gochecknoglobals // constant table

// DescriptorSpec is the per-repository descriptor job: .github/scp.yaml (or .yml) wins over scp.yaml at the root. The blob is
// fetched only when its SHA changed.
func (c *Catalog) DescriptorSpec() sync.Spec {
	return sync.Spec{Kind: DescriptorKind, Prio: sync.PrioScheduled, TTL: descriptorTTL, On: []sync.Event{sync.EventPush},
		Run: c.runDescriptor, Publish: publishDescriptor}
}

func (c *Catalog) runDescriptor(ctx context.Context, j sync.Job) (sync.Result, error) {
	src, ok := c.sources[j.Source]
	if !ok {
		return sync.Result{}, fmt.Errorf("catalog: unknown source %s", j.Source)
	}
	owner, name, _ := strings.Cut(string(j.Repo), "/")
	file, entry, d, err := locate(ctx, src.gh, owner, name)
	var e *github.Error
	switch {
	case errors.As(err, &e) && e.Code == "empty":
		return descriptorResult(snapshot.StateNoContent, "", Descriptor{}, j.Prev)
	case github.IsNotFound(err):
		return descriptorResult(snapshot.StateNotFound, "", Descriptor{}, j.Prev)
	case err != nil:
		return sync.Result{}, err
	case entry == nil:
		return descriptorResult(snapshot.StateNotFound, "", d, j.Prev) // none, or ambiguous (d holds the problem)
	case entry.Size > maxDescriptorBytes:
		d = Descriptor{File: file, Problems: []snapshot.Problem{{Code: "descriptor.too_large",
			Message: fmt.Sprintf("the file is larger than %d bytes", maxDescriptorBytes)}}}
		return descriptorResult(snapshot.StateTooLarge, entry.SHA, d, j.Prev)
	case j.Prev.State == snapshot.StateOK && j.Prev.ContentSHA == entry.SHA:
		return sync.Result{Unchanged: true}, nil
	}
	b, err := src.gh.Blob(ctx, owner, name, entry.SHA)
	if err != nil {
		return sync.Result{}, err
	}
	d = ParseDescriptor(b)
	d.File = file
	return descriptorResult(snapshot.StateOK, entry.SHA, d, j.Prev)
}

// locate finds the descriptor file. Both extensions in the winning location is a problem and no descriptor.
func locate(ctx context.Context, gh GitHub, owner, name string) (string, *github.TreeEntry, Descriptor, error) {
	root, err := gh.Tree(ctx, owner, name, "HEAD", false)
	if err != nil {
		return "", nil, Descriptor{}, err
	}
	locations := []struct {
		dir     string
		entries []github.TreeEntry
	}{{"", root}}
	for _, e := range root {
		if e.Path == ".github" && e.Type == "tree" {
			sub, err := gh.Tree(ctx, owner, name, e.SHA, false)
			if err != nil {
				return "", nil, Descriptor{}, err
			}
			locations = append([]struct {
				dir     string
				entries []github.TreeEntry
			}{{".github/", sub}}, locations...)
		}
	}
	for _, loc := range locations {
		var found []github.TreeEntry
		for _, e := range loc.entries {
			for _, n := range descriptorNames {
				if e.Type == "blob" && e.Path == n {
					found = append(found, e)
				}
			}
		}
		switch len(found) {
		case 0:
			continue
		case 1:
			return loc.dir + found[0].Path, &found[0], Descriptor{}, nil
		default:
			return "", nil, Descriptor{Problems: []snapshot.Problem{{Code: "descriptor.ambiguous",
				Message: "both " + loc.dir + "scp.yaml and " + loc.dir + "scp.yml exist; rename one"}}}, nil
		}
	}
	return "", nil, Descriptor{}, nil
}

func descriptorResult(state snapshot.State, sha string, d Descriptor, prev store.Item) (sync.Result, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return sync.Result{}, err
	}
	return sync.Result{State: state, ContentSHA: sha, Payload: b,
		Unchanged: prev.State == state && prev.ContentSHA == sha && string(prev.Payload) == string(b)}, nil
}

func publishDescriptor(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) {
	var d Descriptor
	if len(it.Payload) > 0 && json.Unmarshal(it.Payload, &d) != nil {
		d = Descriptor{}
	}
	b.UpdateRepo(it.Repo, func(e *snapshot.RepoEntry) { descriptorFacet.Set(e, d, fresh) })
}
