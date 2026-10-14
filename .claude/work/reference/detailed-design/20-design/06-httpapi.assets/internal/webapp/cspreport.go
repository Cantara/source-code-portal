package webapp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxReportBody = 64 << 10

func knownDirective(d string) bool {
	switch d {
	case "default-src", "script-src", "script-src-elem", "script-src-attr", "style-src", "style-src-elem", "style-src-attr", "img-src",
		"font-src", "connect-src", "manifest-src", "frame-src", "frame-ancestors", "form-action", "base-uri", "object-src",
		"require-trusted-types-for", "trusted-types":
		return true
	}
	return false
}

// CSPReports is POST /csp-reports. It accepts both report formats (application/csp-report from report-uri,
// application/reports+json from the Reporting API), reads at most 64 KiB, hands at most 20 sanitised
// reports per request to Options.OnCSPReport and answers 204. It stores nothing and returns nothing.
func (h *Handler) CSPReports() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.common(w)
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			h.plain(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
		if ct != "application/csp-report" && ct != "application/reports+json" && ct != "application/json" {
			h.plain(w, r, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReportBody))
		if err != nil {
			h.plain(w, r, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		reps, ok := parseReports(body)
		if !ok {
			h.plain(w, r, http.StatusBadRequest, "malformed report")
			return
		}
		if len(reps) > 20 {
			reps = reps[:20]
		}
		for _, rep := range reps {
			if h.o.OnCSPReport != nil {
				h.o.OnCSPReport(r.Context(), rep)
			}
			h.observe(KindCSPReport, "")
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type rawBody struct {
	DocumentURL        string `json:"documentURL"`
	DocumentURI        string `json:"document-uri"`
	BlockedURL         string `json:"blockedURL"`
	BlockedURI         string `json:"blocked-uri"`
	EffectiveDirective string `json:"effectiveDirective"`
	EffectiveDash      string `json:"effective-directive"`
	ViolatedDash       string `json:"violated-directive"`
	Disposition        string `json:"disposition"`
	SourceFile         string `json:"sourceFile"`
	SourceFileDash     string `json:"source-file"`
	LineNumber         int    `json:"lineNumber"`
	LineDash           int    `json:"line-number"`
}

func parseReports(b []byte) ([]CSPReport, bool) {
	b = []byte(strings.TrimSpace(string(b)))
	var raws []rawBody
	switch {
	case len(b) > 0 && b[0] == '[': // Reporting API
		var arr []struct {
			Type string  `json:"type"`
			Body rawBody `json:"body"`
		}
		if json.Unmarshal(b, &arr) != nil {
			return nil, false
		}
		for _, a := range arr {
			if a.Type == "csp-violation" {
				raws = append(raws, a.Body)
			}
		}
	case len(b) > 0 && b[0] == '{': // report-uri
		var one struct {
			Report rawBody `json:"csp-report"`
		}
		if json.Unmarshal(b, &one) != nil {
			return nil, false
		}
		raws = append(raws, one.Report)
	default:
		return nil, false
	}
	out := make([]CSPReport, 0, len(raws))
	for _, r := range raws {
		out = append(out, sanitise(r))
	}
	return out, true
}

func first(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func sanitise(r rawBody) CSPReport {
	dir := first(r.EffectiveDirective, r.EffectiveDash, firstWord(r.ViolatedDash))
	if !knownDirective(dir) {
		dir = "other"
	}
	disp := "report"
	if r.Disposition == "enforce" {
		disp = "enforce"
	}
	return CSPReport{
		Directive: dir, Disposition: disp,
		BlockedKind:  blockedKind(first(r.BlockedURL, r.BlockedURI)),
		DocumentPath: pathOnly(first(r.DocumentURL, r.DocumentURI)),
		SourceFile:   pathOnly(first(r.SourceFile, r.SourceFileDash)),
		Line:         max(0, first2(r.LineNumber, r.LineDash)),
	}
}

func first2(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func blockedKind(s string) string {
	switch s {
	case "inline", "eval", "data", "blob", "self", "":
		if s == "" {
			return "unknown"
		}
		return s
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return "host:" + clip(u.Host, 100)
	}
	if strings.HasPrefix(s, "data:") {
		return "data"
	}
	if strings.HasPrefix(s, "blob:") {
		return "blob"
	}
	return "unknown"
}

func pathOnly(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return clip(u.EscapedPath(), 200)
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}
