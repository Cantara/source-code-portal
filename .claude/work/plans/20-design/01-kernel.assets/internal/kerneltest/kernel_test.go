// Package kerneltest holds the cross-package checks of the kernel unit: the import rows of architecture 5.2 for the six
// kernel packages (the executable slice of archtest), and the compile-time shape assertions the wiring relies on.
package kerneltest

import (
	"context"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/routes"
	"github.com/cantara/source-code-portal/internal/scm"
)

const mod = "github.com/cantara/source-code-portal/internal/"

// rows is architecture 5.2 for the kernel: package -> allowed module-internal imports (non-test files).
var rows = map[string][]string{
	"scm":       {},
	"freshness": {},
	"routes":    {},
	"safehtml":  {},
	"job":       {"freshness"},
	"reactor":   {"job", "scm"}, // plus platform/logx once it exists: the scratch module uses log/slog directly
}

// thirdParty is the only non-stdlib import any kernel package may have.
var thirdParty = map[string][]string{"safehtml": {"github.com/microcosm-cc/bluemonday"}}

func TestImportRows(t *testing.T) {
	for pkg, allowed := range rows {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, mod+pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, imp := range strings.Fields(string(out)) {
			switch {
			case strings.HasPrefix(imp, mod):
				if !slices.Contains(allowed, strings.TrimPrefix(imp, mod)) {
					t.Errorf("%s imports %s, not in its row", pkg, imp)
				}
			case !strings.Contains(strings.SplitN(imp, "/", 2)[0], "."): // stdlib: no dot in the first element
			case slices.Contains(thirdParty[pkg], imp):
			default:
				t.Errorf("%s imports third-party %s", pkg, imp)
			}
		}
	}
}

func TestBannedStdlibInKernel(t *testing.T) {
	// G2/G3/G4: no log, no net/http client or server construction in the kernel; routes is the only one allowed net/http.
	for pkg := range rows {
		out, _ := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, mod+pkg).Output()
		for _, imp := range strings.Fields(string(out)) {
			if imp == "log" || imp == "os/exec" || (imp == "net/http" && pkg != "routes") {
				t.Errorf("%s imports %s", pkg, imp)
			}
		}
	}
}

// ---- compile-time shape assertions (the cmd/scp/assert.go entries of this unit) ----

// sink is the one-method interface webhook.Receiver and catalog.EventSink declare.
type sink interface {
	Handle(ctx context.Context, ev scm.Event) error
}

// mux is httpapi.Mux: exactly oapi-codegen's generated ServeMux interface.
type mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	http.Handler
}

var (
	_ sink         = (*reactor.Fan)(nil)
	_ mux          = (*routes.Recorder)(nil)
	_ job.Enqueuer = job.EnqueueFunc(nil)
)
