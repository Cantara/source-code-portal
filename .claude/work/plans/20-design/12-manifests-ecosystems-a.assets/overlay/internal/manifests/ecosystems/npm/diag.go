package npm

import (
	"fmt"

	"github.com/cantara/source-code-portal/internal/manifests"
)

// diag builds a Diagnostic of a catalogued code. The message is formatted, cleaned and clipped to 240 bytes; there is no path because a
// parser never knows its own path (the runner fills Location.Path, parsertest invariant 8).
func diag(code string, line int, format string, args ...any) manifests.Diagnostic {
	return manifests.Diagnostic{Code: code, Severity: severityOf(code), Message: manifests.CleanText(fmt.Sprintf(format, args...), 240), Loc: manifests.Location{Line: line}}
}

// diagAt is diag with an explicit file path: assembler diagnostics are not stamped by the runner.
func diagAt(code, file string, line int, format string, args ...any) manifests.Diagnostic {
	d := diag(code, line, format, args...)
	d.Loc.Path = file
	return d
}

// severityOf looks the code up in the catalogue; shared limit.* and encoding.* codes are warnings.
func severityOf(code string) manifests.Severity {
	for _, d := range catalogue() {
		if d.Code == code {
			return d.Severity
		}
	}
	return manifests.SevWarning
}
