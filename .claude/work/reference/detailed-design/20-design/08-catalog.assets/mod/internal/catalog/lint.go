package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Lint runs Validate and then the rules that need a repository list (R201-R210). repos are the repositories the policy would
// admit (cmd/scp builds them from a JSON file for `config lint --repos`, or from a live listing for `scp doctor`).
// A nil list skips the repository rules. Every matcher problem was already reported by Validate; Lint then evaluates only
// the matchers that compile.
func Lint(cfg Config, vc ValidateContext, prefix func(src scm.SourceID, owner string) string, repos []scm.Repository) []Finding {
	out := cfg.Validate(vc)
	if repos == nil {
		SortFindings(out)
		return out
	}
	c, err := Compile(cfg)
	if err != nil {
		return out
	}
	// slugs, collisions and reserved words (R206)
	var tracked []scm.Repository
	seen := map[string]scm.RepoRef{}
	rs := append([]scm.Repository(nil), repos...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Ref.ID < rs[j].Ref.ID })
	for _, r := range rs {
		pre := ""
		if prefix != nil {
			pre = prefix(r.Ref.Source, r.Ref.Owner)
		}
		slug, err := MakeSlug(pre, r.Ref.Name)
		if err != nil {
			out = append(out, errf("R206", "/repos/"+r.Ref.Name, "%v", err))
			continue
		}
		if prev, dup := seen[FoldSlug(slug)]; dup {
			out = append(out, errf("R206", "/repos/"+r.Ref.Name, "slug %q collides with repository %s/%s; set catalog.slug_prefix_by_source", slug, prev.Owner, prev.Name))
			continue
		}
		seen[FoldSlug(slug)] = r.Ref
		r.Slug = slug
		tracked = append(tracked, r)
	}
	mr, err := c.BuildMembership(tracked)
	if err != nil {
		return append(out, errf("R206", "/repos", "the kernel membership index rejects this repository list: %v", err))
	}
	// per-matcher hit counts (R201) and empty groups (R202)
	for gi, g := range c.groups {
		gp := fmt.Sprintf("/catalog/groups/%d", gi)
		for mi, m := range g.ms {
			hits := 0
			for _, r := range tracked {
				if (g.sources == nil || g.sources[r.Ref.Source]) && m.Match(r.Ref.Name, r.Topics) {
					hits++
				}
			}
			if hits == 0 {
				out = append(out, warnf("R201", fmt.Sprintf("%s/match/%d/%s", gp, mi, m.Kind()), "matches no repository (%s: %q)", m.Kind(), m.Source()))
			}
		}
		h := mr.Groups[gi]
		if h.Members == 0 {
			out = append(out, warnf("R202", gp+"/match", "group %q has no members", g.cfg.ID))
		}
		switch h.DefaultReason {
		case "missing":
			out = append(out, errf("R205", gp+"/default_repo", "no repository named %q exists", g.cfg.DefaultRepo))
		case "not_selected":
			if !hasFinding(out, "C031", gp+"/default_repo") { // already reported by the offline rule
				out = append(out, errf("R205", gp+"/default_repo", "repository %q exists but group %q does not select it", g.cfg.DefaultRepo, g.cfg.ID))
			}
		}
		if h.DefaultRepo != "" {
			for _, r := range tracked {
				if r.Slug == h.DefaultRepo && r.Archived {
					out = append(out, warnf("R209", gp+"/default_repo", "the default repository %q is archived", r.Slug))
				}
			}
		}
	}
	// multi-group (R203): info per repository, excluding repositories whose only extra group is a catch_all group
	catchAll := map[scm.GroupID]bool{}
	for _, g := range c.groups {
		for _, m := range g.ms {
			if m.Kind() == KindCatchAll {
				catchAll[g.cfg.ID] = true
			}
		}
	}
	var multi []string
	for _, s := range sortedSlugs(mr.Multi) {
		var real []scm.GroupID
		for _, gid := range mr.Multi[s] {
			if !catchAll[gid] {
				real = append(real, gid)
			}
		}
		if len(real) > 1 {
			multi = append(multi, fmt.Sprintf("%s: %s", s, joinGroups(real)))
		}
	}
	if len(multi) > 0 {
		out = append(out, Finding{Code: "R203", Severity: SevInfo, Path: "/catalog/groups", Message: fmt.Sprintf("%d repositories are selected by more than one non-catch-all group: %s", len(multi), strings.Join(multi, "; "))})
	}
	if len(mr.Ungrouped) > 0 {
		sev := SevInfo
		note := "they are listed without a group"
		if cfg.Ungrouped == UngroupedIgnore {
			sev, note = SevWarning, "they are not tracked at all (catalog.ungrouped: ignore)"
		}
		out = append(out, Finding{Code: "R204", Severity: sev, Path: "/catalog/groups", Message: fmt.Sprintf("%d repositories are selected by no group; %s: %s", len(mr.Ungrouped), note, summarise(mr.Ungrouped, 10))})
	}
	for _, s := range sortedSlugsStr(mr.RefConflicts) {
		out = append(out, warnf("R207", "/repos/"+string(s), "groups %s set different ref overrides; %q (first in config order) wins", strings.Join(mr.RefConflicts[s], ", "), mr.TrackedRef[s]))
	}
	SortFindings(out)
	return out
}

func sortedSlugs(m map[scm.Slug][]scm.GroupID) []scm.Slug {
	ks := make([]scm.Slug, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return FoldSlug(ks[i]) < FoldSlug(ks[j]) })
	return ks
}
func sortedSlugsStr(m map[scm.Slug][]string) []scm.Slug {
	ks := make([]scm.Slug, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return FoldSlug(ks[i]) < FoldSlug(ks[j]) })
	return ks
}
func joinGroups(gs []scm.GroupID) string {
	var p []string
	for _, g := range gs {
		p = append(p, string(g))
	}
	return strings.Join(p, ", ")
}
func summarise(ss []scm.Slug, n int) string {
	var p []string
	for i, s := range ss {
		if i == n {
			p = append(p, fmt.Sprintf("... and %d more", len(ss)-n))
			break
		}
		p = append(p, string(s))
	}
	return strings.Join(p, ", ")
}

func hasFinding(fs []Finding, code, path string) bool {
	for _, f := range fs {
		if f.Code == code && f.Path == path {
			return true
		}
	}
	return false
}
