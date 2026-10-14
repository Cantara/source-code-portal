package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

// doctorExit maps findings to the exit code (design 16 section 4.1): any FAIL -> 1; WARN only -> 3 under --strict, else 0.
// (Research 09 8.4 said "2 for WARN with --strict"; 2 is the usage code of every other command, so it is 3 here: deviation D-4.)
func doctorExit(found []github.Finding, strict bool) int {
	var worst github.Severity
	for _, f := range found {
		worst = max(worst, f.Severity)
	}
	switch {
	case worst == github.SevFail:
		return exitFail
	case worst == github.SevWarn && strict:
		return exitWarn
	}
	return exitOK
}

// secretMarkers is the last line of defense against a check that prints a credential. The adapter never puts a secret in a
// Finding; this makes a regression visible as "[withheld]" instead of a leak, and a test pins it.
func secretMarkers() []string {
	return []string{"-----BEGIN", "ghs_", "ghp_", "github_pat_", "ghu_", "ghr_"}
}

func scrub(s string) string {
	for _, m := range secretMarkers() {
		if strings.Contains(s, m) {
			return "[withheld: looked like a credential]"
		}
	}
	return s
}

// localFindings are the checks cmd/scp owns (05 section 4.13: "cmd/scp runs cfg.*, store, webhook.self and git.* and merges").
func localFindings(ctx context.Context, l loaded) []github.Finding {
	var out []github.Finding
	for _, v := range l.violations {
		t := v.Message
		if v.Path != "" {
			t = v.Path + ": " + v.Message
		}
		if v.File != "" && v.Line > 0 {
			t = fmt.Sprintf("%s:%d: %s", v.File, v.Line, t)
		}
		out = append(out, github.Finding{ID: "cfg.invalid", Severity: github.SevFail, Title: t,
			Remediation: "fix the key; `scp config lint` lists every problem offline"})
	}
	for _, w := range l.warnings {
		out = append(out, github.Finding{ID: "cfg.warning", Severity: github.SevWarn, Title: w})
	}
	if len(l.violations) > 0 {
		return out
	}
	out = append(out, github.Finding{ID: "cfg.load", Severity: github.SevInfo, Title: "configuration decodes strictly, validates and its secrets resolve"})
	// store: never writes. sqlite.CheckFile reports a missing file as an ordinary open error ("unable to open database file (14)"),
	// not as fs.ErrNotExist, so existence is decided here first: a missing file is normal before the first start.
	path := l.cfg.Store.Path
	if _, serr := os.Stat(path); errors.Is(serr, fs.ErrNotExist) {
		if st, derr := os.Stat(filepath.Dir(path)); derr != nil || !st.IsDir() {
			out = append(out, github.Finding{ID: "store.dir", Severity: github.SevFail, Title: "store directory does not exist", Detail: filepath.Dir(path),
				Remediation: "mount a writable volume at the directory of store.path"})
		} else {
			out = append(out, github.Finding{ID: "store.new", Severity: github.SevInfo, Title: "no store file yet; it is created at first start", Detail: path})
		}
		return out
	}
	if err := sqlite.CheckFile(ctx, path, false); err != nil {
		out = append(out, github.Finding{ID: "store.integrity", Severity: github.SevFail, Title: "store failed the integrity check", Detail: err.Error(),
			Remediation: "restore a backup: `scp store restore --db ... --from ... --yes`"})
	} else {
		out = append(out, github.Finding{ID: "store.integrity", Severity: github.SevInfo, Title: "store passes quick_check", Detail: path})
	}
	return out
}

// mergeFindings orders by source (process-level, source "", first), then severity (worst first), keeping each source's own order otherwise.
func mergeFindings(parts ...[]github.Finding) []github.Finding {
	var all []github.Finding
	for _, p := range parts {
		all = append(all, p...)
	}
	slices.SortStableFunc(all, func(a, b github.Finding) int {
		if c := cmp.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		return cmp.Compare(b.Severity, a.Severity)
	})
	return all
}

type doctorRow struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // info | warn | fail
	Source      string `json:"source"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation"`
	sev         github.Severity
}

func cmdDoctor(ctx context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "doctor")
	path := configPathFlag(fl, env)
	source := fl.String("source", "", "check only these source ids (comma-separated)")
	asJSON := fl.Bool("json", false, "print findings as a JSON array")
	offline := fl.Bool("offline", false, "run only the checks that need no network")
	strict := fl.Bool("strict", false, "exit 3 when there are only warnings")
	timeout := fl.Duration("timeout", 60*time.Second, "total deadline")
	hints := fl.Bool("fix-hints", true, "print the remediation line under each finding")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	if *timeout <= 0 || fl.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp doctor: --timeout must be positive and there are no positional arguments")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	l, err := readConfig(loadOptions{file: *path, environ: env.Environ})
	if err != nil { // the file cannot be read or parsed at all: one finding, nothing else is meaningful
		return renderAndExit(env, []github.Finding{{ID: "cfg.read", Severity: github.SevFail, Title: err.Error(), Remediation: "run `scp config lint`"}}, *asJSON, *hints, *strict)
	}
	local := localFindings(ctx, l)
	var remote []github.Finding
	switch {
	case len(l.violations) > 0:
		local = append(local, github.Finding{ID: "net.skipped", Severity: github.SevWarn, Title: "network checks skipped: the configuration has errors"})
	case env.Doctor == nil:
		local = append(local, github.Finding{ID: "net.skipped", Severity: github.SevWarn, Title: "GitHub checks are not available in this build"})
	default:
		o := github.DoctorOptions{Offline: *offline, Timeout: *timeout, Features: []github.Feature{github.FeatureCore}, PublicURL: l.cfg.Server.Public.PublicURL}
		for s := range strings.SplitSeq(*source, ",") {
			if s = strings.TrimSpace(s); s != "" {
				o.Sources = append(o.Sources, scm.SourceID(s))
			}
		}
		if remote, err = env.Doctor(ctx, l.cfg, o); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "scp doctor: %v\n", err)
			return exitFail
		}
	}
	return renderAndExit(env, mergeFindings(local, remote), *asJSON, *hints, *strict)
}

func renderAndExit(env Env, found []github.Finding, asJSON, hints, strict bool) int {
	rows := make([]doctorRow, 0, len(found))
	for _, f := range found {
		rows = append(rows, doctorRow{ID: f.ID, Severity: strings.ToLower(f.Severity.String()), Source: f.Source, Title: scrub(f.Title), Detail: scrub(f.Detail), Remediation: scrub(f.Remediation), sev: f.Severity})
	}
	if asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	} else {
		cur := "\x00"
		var n [3]int
		for _, r := range rows {
			if r.Source != cur {
				cur = r.Source
				name := cmp.Or(cur, "(process)")
				_, _ = fmt.Fprintf(env.Stdout, "\n== %s\n", name)
			}
			switch r.sev {
			case github.SevFail:
				n[0]++
			case github.SevWarn:
				n[1]++
			default:
				n[2]++
			}
			_, _ = fmt.Fprintf(env.Stdout, "%-4s %-22s %s\n", strings.ToUpper(r.Severity), r.ID, r.Title)
			if r.Detail != "" {
				_, _ = fmt.Fprintf(env.Stdout, "     %s\n", r.Detail)
			}
			if hints && r.Remediation != "" && r.sev != github.SevInfo {
				_, _ = fmt.Fprintf(env.Stdout, "     fix: %s\n", r.Remediation)
			}
		}
		_, _ = fmt.Fprintf(env.Stdout, "\n%d FAIL, %d WARN, %d INFO\n", n[0], n[1], n[2])
	}
	return doctorExit(found, strict)
}
