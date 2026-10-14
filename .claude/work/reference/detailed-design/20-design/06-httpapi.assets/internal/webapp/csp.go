package webapp

import "strings"

// The document policy (architecture 11.3). Static: no nonce, no per-request value.
const (
	cspCore = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; " +
		"connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; " +
		"object-src 'none'"
	cspTrustedTypes = "require-trusted-types-for 'script'; trusted-types scp-html"
	cspReportTo     = "report-to csp"
)

// DocumentCSP returns the exact policy strings per mode: the value of Content-Security-Policy (enforce,
// possibly "") and of Content-Security-Policy-Report-Only (possibly "").
func DocumentCSP(mode CSPMode) (enforce, reportOnly string) {
	full := strings.Join([]string{cspCore, cspTrustedTypes, cspReportTo}, "; ")
	core := cspCore + "; " + cspReportTo
	switch mode {
	case CSPEnforce:
		return full, ""
	case CSPEnforceCore:
		return core, full
	default:
		return "", full
	}
}

const permissionsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
