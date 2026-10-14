package catalog

import (
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

func sortStrings(s []string) { sort.Strings(s) }

func joinSources(s []scm.SourceID) string {
	var p []string
	for _, x := range s {
		p = append(p, string(x))
	}
	sort.Strings(p)
	return strings.Join(p, ",")
}
