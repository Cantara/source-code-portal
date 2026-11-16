package config

import (
	"fmt"
	"strings"
)

// Commits configures how commits are read (design/layout.md, P-127).
type Commits struct {
	// Assistants adds the deployment's own AI assistants to the built-in rules that tell AI work from people's own and from automation.
	Assistants []CommitAssistant `mapstructure:"assistants"`
}

// CommitAssistant is one assistant: the authors that are it as a coding agent and the co-authors that mark a person's commit as
// made with it. Entries are matched case-insensitively.
type CommitAssistant struct {
	Name      string   `mapstructure:"name"`       // the name the pages show
	Authors   []string `mapstructure:"authors"`    // an author's login or name
	CoAuthors []string `mapstructure:"co_authors"` // a Co-authored-by trailer's name, address or address's local part
}

func (c Commits) validate() []string {
	var p []string
	seen := map[string]string{}
	for i, a := range c.Assistants {
		at := fmt.Sprintf("commits.assistants[%d]", i)
		if strings.TrimSpace(a.Name) == "" {
			p = append(p, at+".name: required")
		}
		if len(a.Authors)+len(a.CoAuthors) == 0 {
			p = append(p, at+": needs authors or co_authors")
		}
		for key, list := range map[string][]string{"authors": a.Authors, "co_authors": a.CoAuthors} {
			for _, m := range list {
				k := key + ":" + strings.ToLower(strings.TrimSpace(m))
				switch {
				case strings.TrimSpace(m) == "" || strings.ContainsAny(m, "<>\n"):
					p = append(p, fmt.Sprintf("%s.%s: %q is not a login, name or address", at, key, m))
				case seen[k] != "":
					p = append(p, fmt.Sprintf("%s.%s: %q is also in %s", at, key, m, seen[k]))
				default:
					seen[k] = at
				}
			}
		}
	}
	return p
}
