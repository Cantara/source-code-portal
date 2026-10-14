package e2e

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The import matrix of architecture 5.2 for the two packages of this unit (the executable form of
// internal/archtest rows). refresh: job (+ freshness, logx); webhook: scm, job, httpapi (+ logx).
func TestImportsStayInsideTheMatrix(t *testing.T) {
	allowed := map[string][]string{
		"refresh": {"internal/job", "internal/freshness", "internal/platform/logx"},
		"webhook": {"internal/scm", "internal/job", "internal/httpapi", "internal/platform/logx"},
	}
	for pkg, ok := range allowed {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "../"+pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", pkg, err, out)
		}
		for _, imp := range strings.Fields(string(out)) {
			_, rest, found := strings.Cut(imp, "/internal/")
			if !found {
				continue
			}
			if !slices.Contains(ok, "internal/"+rest) {
				t.Errorf("%s imports %s, not in the matrix row %v", pkg, imp, ok)
			}
		}
	}
}
