package main

import (
	"net/url"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/pulls"
	"github.com/cantara/source-code-portal/internal/search"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const commitBody = 4 << 10 // bytes of a commit's body the index reads

// searchOptions is what the search index reads of the other features (design/search.md): the repository and its descriptor, its
// documents cut at their headings, its manifests, its packages and its default branch's commits.
func searchOptions() search.Options {
	return search.Options{Read: searchRead, Work: searchWork}
}

func searchRead(e *snapshot.RepoEntry) search.Repository {
	r, k := e.Repo, e.Repo.Key()
	page := snapshot.RepoPage(k)
	d, _ := catalog.DescriptorOf(e)
	ms, _ := manifests.Of(e)
	var out search.Repository
	for _, m := range ms {
		if m.Language != "" && !slices.Contains(out.Languages, m.Language) {
			out.Languages = append(out.Languages, m.Language)
		}
	}
	out.Topics, out.Lifecycle = slices.Concat(r.Topics, d.Tags), d.Lifecycle

	repo := search.Document{Kind: search.KindRepo, Title: r.Owner + "/" + r.Name, URL: page}
	repo.Fields[search.FieldName] = r.Owner + "/" + r.Name
	repo.Fields[search.FieldTitle] = d.Title
	repo.Fields[search.FieldTags] = strings.Join(out.Topics, " ")
	repo.Fields[search.FieldDescription] = strings.TrimSpace(d.Description + " " + r.Description)
	meta := slices.Concat(out.Languages, []string{r.License, d.Lifecycle}, d.Owners)
	if r.Archived {
		meta = append(meta, "archived")
	}
	repo.Fields[search.FieldMeta] = strings.Join(meta, " ")
	if rel, _ := badges.ReleaseOf(e); rel != nil {
		repo.Fields[search.FieldText] = rel.Tag + " " + rel.Name
	}
	out.Documents = append(out.Documents, repo)

	ds, _ := docs.Of(e)
	for _, doc := range ds {
		if doc.Hidden || doc.State != snapshot.StateOK {
			continue
		}
		at := snapshot.DocPage(k, doc.Path)
		if doc.README {
			at = page
		}
		anchors := make([]search.Anchor, 0, len(doc.TOC))
		for _, h := range doc.TOC {
			anchors = append(anchors, search.Anchor{Text: h.Text, ID: h.ID})
		}
		sections := []search.Section{{Text: string(doc.Source)}}
		if doc.Format == "markdown" {
			sections = search.Sections(string(doc.Source), anchors)
		}
		for _, sec := range sections {
			sd := search.Document{Kind: search.KindDoc, Title: doc.Title, URL: at}
			if sec.Heading != "" && !strings.EqualFold(sec.Heading, doc.Title) {
				sd.Title = doc.Title + " › " + sec.Heading
			}
			if sec.Anchor != "" {
				sd.URL = at + "#" + sec.Anchor
			}
			sd.Fields[search.FieldTitle], sd.Fields[search.FieldHeading], sd.Fields[search.FieldText] = doc.Title, sec.Heading, sec.Text
			out.Documents = append(out.Documents, sd)
		}
	}

	for _, m := range ms {
		names := make([]string, 0, len(m.Dependencies))
		for _, dep := range m.Dependencies {
			names = append(names, dep.Name)
		}
		out.Dependencies = append(out.Dependencies, names...)
		md := search.Document{Kind: search.KindDep, Title: m.Name, URL: page + "#dependencies"}
		if md.Title == "" {
			md.Title = m.Path
		}
		md.Fields[search.FieldName], md.Fields[search.FieldText] = m.Name, strings.Join(names, "\n")
		out.Documents = append(out.Documents, md)
	}

	ps, _ := packages.Of(e)
	for _, p := range ps {
		pd := search.Document{Kind: search.KindPackage, Title: p.Name, URL: page + "#packages"}
		pd.Fields[search.FieldName] = p.Name
		if p.Latest != nil {
			pd.Fields[search.FieldMeta] = p.Latest.Version + " " + p.Latest.Registry
		}
		out.Documents = append(out.Documents, pd)
	}

	if set, _ := pulls.Of(e); set != nil { // open pull requests by title, its author and its branch; a hit opens them on /pulls
		at := "/pulls?repo=" + url.QueryEscape(string(r.Key())) + "&drafts=true"
		for _, p := range set.Pulls {
			pd := search.Document{Kind: search.KindPull, Title: p.Title, URL: at, At: p.Updated}
			pd.Fields[search.FieldTitle], pd.Fields[search.FieldMeta] = p.Title, p.Head+" "+string(pulls.StateOf(&p))
			pd.Fields[search.FieldAuthor] = p.Author
			out.Documents = append(out.Documents, pd)
		}
	}

	cs, _ := commits.Of(e)
	for i := range cs {
		c := &cs[i]
		cd := search.Document{Kind: search.KindCommit, Title: c.Subject, URL: page + "/commits?sha=" + c.SHA, At: c.CommittedAt, Item: i}
		body := c.Body
		if len(body) > commitBody {
			body = body[:commitBody]
		}
		cd.Fields[search.FieldTitle], cd.Fields[search.FieldText] = c.Subject, body
		cd.Fields[search.FieldAuthor] = strings.TrimSpace(c.AuthorName + " " + c.AuthorLogin)
		out.Documents = append(out.Documents, cd)
	}
	return out
}

// searchWork is the kind of work of the commit item of e in the view s, as the lens counts it: automation (a bot or a sweep), with
// an AI, or by hand.
func searchWork(s *snapshot.Snapshot, e *snapshot.RepoEntry, item int) string {
	cs, _ := commits.Of(e)
	if item < 0 || item >= len(cs) {
		return ""
	}
	c := &cs[item]
	switch o, _ := commits.Classify(c); {
	case o == commits.OriginAutomation || commits.Sweep(s, c):
		return "automation"
	case o.AI():
		return "ai"
	default:
		return "hand"
	}
}
