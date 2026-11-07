package setup

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/cantara/source-code-portal/internal/ghlogin"
)

// minPrefix is how many repositories share a first word before it is suggested as a product.
const minPrefix = 3

// Suggest sorts repositories into products a person would recognise: the repositories sharing a first word of their name
// (whydah-sso, whydah-admin, whydah-ui: Whydah), then, when the repositories come from several accounts, the rest by account. What
// it leaves out stays outside any product. The person changes all of it.
func Suggest(repos []ghlogin.Repo) []Product {
	repos = OnePerName(repos)
	words := map[string][]string{}
	for _, r := range repos {
		if w := firstWord(r.Name); w != "" {
			words[w] = append(words[w], r.Name)
		}
	}
	taken := map[string]bool{}
	var out []Product
	for w, names := range words {
		if len(names) >= minPrefix {
			names = sorted(names)
			out = append(out, Product{Name: title(w), Repos: names})
			for _, n := range names {
				taken[n] = true
			}
		}
	}
	owners := map[string][]string{}
	for _, r := range repos {
		if !taken[r.Name] {
			owners[r.Owner] = append(owners[r.Owner], r.Name)
		}
	}
	all := map[string]bool{}
	for _, r := range repos {
		all[strings.ToLower(r.Owner)] = true
	}
	if len(all) > 1 {
		used := map[string]bool{}
		for _, p := range out {
			used[strings.ToLower(p.Name)] = true
		}
		for o, names := range owners {
			if len(names) >= 2 {
				name := o
				if used[strings.ToLower(name)] { // the account's name is also a first word: two products never share a name
					name = o + " (other)"
				}
				out = append(out, Product{Name: name, Repos: sorted(names)})
			}
		}
	}
	for i := range out {
		slices.Sort(out[i].Repos)
	}
	slices.SortFunc(out, func(a, b Product) int {
		return cmp.Or(cmp.Compare(len(b.Repos), len(a.Repos)), strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	return out[:min(len(out), MaxProducts)]
}

// OnePerName keeps the first of the repositories that share a name (the most recently pushed, as listed): a group matches by name
// alone, so the same name in two accounts is one repository to the portal, and the page shows it once.
func OnePerName(repos []ghlogin.Repo) []ghlogin.Repo {
	seen := make(map[string]bool, len(repos))
	out := make([]ghlogin.Repo, 0, len(repos))
	for _, r := range repos {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r)
		}
	}
	return out
}

func sorted(names []string) []string {
	slices.Sort(names)
	return names
}

// firstWord is the lower-case word a name starts with, up to its first -, _ or ., when it has two letters or more.
func firstWord(name string) string {
	w, _, found := strings.Cut(strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(name)), "-")
	if !found || len(w) < 2 || strings.IndexFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) >= 0 {
		return ""
	}
	return w
}

func title(w string) string { return strings.ToUpper(w[:1]) + w[1:] }
