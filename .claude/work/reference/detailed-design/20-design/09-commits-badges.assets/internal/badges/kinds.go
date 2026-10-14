package badges

import (
	"github.com/cantara/source-code-portal/internal/freshness"
	"strconv"
	"strings"
	"time"
)

// Kind is a badge kind. The string is the URL form without ".svg".
type Kind string

const (
	KindIssues        Kind = "issues"
	KindLastCommit    Kind = "last-commit"
	KindRelease       Kind = "release"
	KindLicence       Kind = "licence"
	KindRepos         Kind = "repos" // group form only
	KindLanguage      Kind = "language"
	KindBuildTool     Kind = "build-tool"
	KindLatestVersion Kind = "latest-version"
	KindInternalDeps  Kind = "internal-deps"
	KindBuild         Kind = "build"    // optional: integrations (Jenkins)
	KindSecurity      Kind = "security" // optional: integrations (Snyk)
)

// label is the left half of the badge; it comes from this closed set, never from a request.
func (k Kind) label() string {
	switch k {
	case KindIssues:
		return "issues"
	case KindLastCommit:
		return "last commit"
	case KindRelease:
		return "release"
	case KindLicence:
		return "licence"
	case KindRepos:
		return "repos"
	case KindLanguage:
		return "language"
	case KindBuildTool:
		return "build tool"
	case KindLatestVersion:
		return "version"
	case KindInternalDeps:
		return "internal deps"
	case KindBuild:
		return "build"
	case KindSecurity:
		return "security"
	}
	return "badge"
}

// manifestKind reports the kinds that exist only when manifests.enabled (Deps.Enabled).
func (k Kind) manifestKind() bool {
	switch k {
	case KindLanguage, KindBuildTool, KindLatestVersion, KindInternalDeps:
		return true
	}
	return false
}

// Alt is the accessible text for an <img> that shows this kind, without the value (the SPA does not know it):
// the SVG itself carries role="img" and the full "label: message" text for contexts where it is the document.
func (k Kind) Alt() string {
	switch k {
	case KindIssues:
		return "Open issues"
	case KindLastCommit:
		return "Last commit"
	case KindRelease:
		return "Latest release"
	case KindLicence:
		return "Licence"
	case KindRepos:
		return "Number of repositories"
	case KindLanguage:
		return "Primary language"
	case KindBuildTool:
		return "Build tool"
	case KindLatestVersion:
		return "Latest version"
	case KindInternalDeps:
		return "Internal dependencies"
	case KindBuild:
		return "Build status"
	case KindSecurity:
		return "Security scan"
	}
	return "Badge"
}

// ParseFile maps the {file} path value of GET /badges/{repo}/{file} ("issues.svg") to a kind. The suffix is required
// and case-sensitive; "license" is accepted as the American spelling of "licence". KindRepos is not a repository kind.
func ParseFile(file string) (Kind, bool) {
	name, ok := strings.CutSuffix(file, ".svg")
	if !ok {
		return "", false
	}
	if name == "license" {
		name = "licence"
	}
	switch k := Kind(name); k {
	case KindIssues, KindLastCommit, KindRelease, KindLicence, KindLanguage, KindBuildTool, KindLatestVersion,
		KindInternalDeps, KindBuild, KindSecurity:
		return k, true
	}
	return "", false
}

// State is how a badge came about; it labels the metric and decides caching.
type State uint8

const (
	StateOK      State = iota // a value is shown
	StateNone                 // the fact is known to be absent ("none")
	StateUnknown              // no usable data (yet): grey "unknown"
)

func (s State) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateNone:
		return "none"
	}
	return "unknown"
}

// Spec is the result of evaluating a kind against facts: text, semantic colour and state.
type Spec struct {
	Label, Message string
	Tone           Tone
	State          State
}

func unknown(k Kind) Spec {
	return Spec{Label: k.label(), Message: "unknown", Tone: ToneNeutral, State: StateUnknown}
}
func none(k Kind) Spec {
	return Spec{Label: k.label(), Message: "none", Tone: ToneNeutral, State: StateNone}
}
func ok(k Kind, msg string, role Tone) Spec {
	return Spec{Label: k.label(), Message: msg, Tone: role, State: StateOK}
}

// Evaluate computes the Spec of kind k for one repository at time now. It is a pure function.
func Evaluate(k Kind, f RepoFacts, now time.Time) Spec {
	switch k {
	case KindIssues:
		v, st := fact(f.Stats)
		if st != hasValue {
			return missing(k, st)
		}
		role := ToneInfo
		if v.OpenIssues == 0 {
			role = ToneSuccess
		}
		return ok(k, formatCount(v.OpenIssues)+" open", role)
	case KindLastCommit:
		t, st := fact(f.LastCommit)
		if st != hasValue || t.IsZero() {
			return missing(k, st)
		}
		msg, days := relativeDay(now, t)
		role := ToneSuccess
		switch {
		case days > 365:
			role = ToneWarning
		case days > 90:
			role = ToneInfo
		}
		return ok(k, msg, role)
	case KindRelease:
		v, st := fact(f.Stats)
		if st != hasValue {
			return missing(k, st)
		}
		if v.Release == nil || v.Release.Tag == "" {
			return none(k)
		}
		role := ToneInfo
		if v.Release.Prerelease {
			role = ToneWarning
		}
		return ok(k, v.Release.Tag, role)
	case KindLicence:
		v, st := fact(f.Stats)
		if st != hasValue {
			return missing(k, st)
		}
		switch strings.ToUpper(v.License) {
		case "":
			return none(k)
		case "NOASSERTION", "OTHER":
			return Spec{Label: k.label(), Message: "other", Tone: ToneNeutral, State: StateOK}
		}
		return ok(k, v.License, ToneInfo)
	case KindLanguage:
		return stringFact(k, f.Language)
	case KindBuildTool:
		return stringFact(k, f.BuildTool)
	case KindLatestVersion:
		return stringFact(k, f.LatestVersion)
	case KindInternalDeps:
		n, st := fact(f.InternalDeps)
		if st != hasValue {
			return missing(k, st)
		}
		switch n {
		case 0:
			return none(k)
		case 1:
			return ok(k, "1 repo", ToneInfo)
		}
		return ok(k, strconv.Itoa(n)+" repos", ToneInfo)
	case KindBuild:
		return factSpec(k, f.Build)
	case KindSecurity:
		return factSpec(k, f.Security)
	}
	return unknown(k)
}

// EvaluateRepos is the group form: the number of repositories of a group visible to the caller.
func EvaluateRepos(n int) Spec {
	if n <= 0 {
		return Spec{Label: KindRepos.label(), Message: "0", Tone: ToneNeutral, State: StateOK}
	}
	return ok(KindRepos, formatCount(n), ToneInfo)
}

// factSpec renders an integration fact. A stale fact says so in words (colour is never the only carrier of a state).
func factSpec(k Kind, f *Fact) Spec {
	if f == nil || f.Message == "" {
		return unknown(k)
	}
	msg := f.Message
	if f.Stale {
		msg += " (stale)"
	}
	if msg == "unknown" {
		return unknown(k)
	}
	return ok(k, msg, f.Tone)
}

func stringFact(k Kind, v freshness.Value[string]) Spec {
	s, st := fact(v)
	if st != hasValue || strings.TrimSpace(s) == "" {
		if st == hasValue {
			return none(k)
		}
		return missing(k, st)
	}
	return ok(k, s, ToneInfo)
}

func missing(k Kind, st factState) Spec {
	if st == absent {
		return none(k)
	}
	return unknown(k)
}
