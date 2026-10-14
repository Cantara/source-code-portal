package main

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/archtest"
	"github.com/cantara/source-code-portal/internal/routes"
)

// TestRouteLiteralRuleCoversEveryRoute pins the archtest "no route-looking literal outside internal/routes" rule to the real
// route tables: the first path segment of every public and admin pattern must be recognized by archtest.RouteLiteral, so a new
// top-level route cannot slip past the rule unnoticed (V16-05).
func TestRouteLiteralRuleCoversEveryRoute(t *testing.T) {
	seen := 0
	for _, tbl := range []*routes.Table{routes.Public(), routes.Admin()} {
		for _, r := range tbl.All() {
			seen++
			first := "/" + strings.SplitN(strings.TrimPrefix(r.Pattern, "/"), "/", 2)[0]
			if first == "/" {
				continue // "/" itself is the SPA shell
			}
			if strings.HasPrefix(first, "/{") {
				continue // a pure wildcard first segment has no literal to match
			}
			if !archtest.RouteLiteral(first) && !archtest.RouteLiteral(first+"/") {
				t.Errorf("route %s %q: first segment %q is not recognized by archtest.RouteLiteral", r.Name, r.Pattern, first)
			}
		}
	}
	if seen < 30 {
		t.Fatalf("only %d routes seen; the tables are not registered", seen)
	}
}
