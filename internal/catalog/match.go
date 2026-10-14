package catalog

import (
	"cmp"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	maxGlobStars    = 6
	maxRegexpBytes  = 256
	maxRegexpInsts  = 1000
	topicPatternStr = `^[a-z0-9][a-z0-9-]{0,49}$`
)

var (
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	topicPattern = regexp.MustCompile(topicPatternStr)
)

// matcher selects repositories by name or topic.
type matcher func(r *snapshot.Repo) bool

type group struct {
	cfg     config.Group
	sources map[string]bool // empty: all
	match   []matcher
}

// Groups are the compiled group definitions, in config order.
type Groups struct{ groups []group }

// Finding is a problem in the group configuration. Errors refuse start; warnings are logged.
type Finding struct {
	Error   bool
	Message string
}

func (f Finding) String() string {
	if f.Error {
		return "error: " + f.Message
	}
	return "warning: " + f.Message
}

// Compile checks and compiles the configured groups. It returns nil Groups when any finding is an error.
func Compile(cfg []config.Group) (*Groups, []Finding) {
	var findings []Finding
	add := func(isErr bool, format string, args ...any) {
		findings = append(findings, Finding{Error: isErr, Message: fmt.Sprintf(format, args...)})
	}
	gs := &Groups{}
	catchAlls := 0
	for i, gc := range cfg {
		at := fmt.Sprintf("catalog.groups[%d] (%s)", i, gc.ID)
		g := group{cfg: gc, sources: map[string]bool{}}
		for _, s := range gc.Sources {
			g.sources[s] = true
		}
		hasTopic := false
		for j, m := range gc.Match {
			kind, value, _ := m.Kind() // one key: checked by config
			fn, err := compileMatcher(kind, value)
			if err != nil {
				add(true, "%s.match[%d]: %v", at, j, err)
				continue
			}
			if fn(&snapshot.Repo{}) && kind != "catch_all" {
				add(false, "%s.match[%d]: %s %q matches the empty name", at, j, kind, value)
			}
			hasTopic = hasTopic || kind == "topic"
			if kind == "catch_all" {
				catchAlls++
			}
			g.match = append(g.match, fn)
		}
		switch {
		case !namePattern.MatchString(gc.DefaultRepo):
			add(true, "%s.default_repo: %q is not a repository name", at, gc.DefaultRepo)
		case len(g.match) == len(gc.Match) && !g.matches(&snapshot.Repo{Name: gc.DefaultRepo}):
			add(!hasTopic, "%s.default_repo: %q is not selected by the group's matchers", at, gc.DefaultRepo)
		}
		gs.groups = append(gs.groups, g)
	}
	if catchAlls > 1 {
		add(false, "catalog.groups: %d groups use catch_all", catchAlls)
	}
	if slices.ContainsFunc(findings, func(f Finding) bool { return f.Error }) {
		return nil, findings
	}
	return gs, findings
}

func compileMatcher(kind, value string) (matcher, error) {
	switch kind {
	case "exact":
		return func(r *snapshot.Repo) bool { return strings.EqualFold(r.Name, value) }, nil
	case "prefix":
		p := strings.ToLower(value)
		return func(r *snapshot.Repo) bool { return strings.HasPrefix(strings.ToLower(r.Name), p) }, nil
	case "glob":
		if strings.Count(value, "*") > maxGlobStars {
			return nil, fmt.Errorf("glob %q has more than %d *", value, maxGlobStars)
		}
		var b strings.Builder
		b.WriteString("(?i)^")
		for _, c := range value {
			switch c {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteString(".")
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		b.WriteString("$")
		re := regexp.MustCompile(b.String()) // quoted pieces always compile
		return func(r *snapshot.Repo) bool { return re.MatchString(r.Name) }, nil
	case "regexp":
		if len(value) > maxRegexpBytes {
			return nil, fmt.Errorf("regexp longer than %d bytes", maxRegexpBytes)
		}
		// Parse the pattern alone first, so an unbalanced ) cannot close the anchoring group.
		if _, err := syntax.Parse(value, syntax.Perl); err != nil {
			return nil, fmt.Errorf("regexp %q: %w", value, err)
		}
		anchored := `^(?:` + value + `)$`
		parsed, err := syntax.Parse(anchored, syntax.Perl)
		if err != nil {
			return nil, fmt.Errorf("regexp %q: %w", value, err)
		}
		prog, err := syntax.Compile(parsed.Simplify())
		if err != nil || len(prog.Inst) > maxRegexpInsts {
			return nil, fmt.Errorf("regexp %q is too complex", value)
		}
		re := regexp.MustCompile(anchored)
		return func(r *snapshot.Repo) bool { return re.MatchString(r.Name) }, nil
	case "topic":
		if !topicPattern.MatchString(value) {
			return nil, fmt.Errorf("topic %q does not match %s", value, topicPatternStr)
		}
		return func(r *snapshot.Repo) bool { return slices.Contains(r.Topics, value) }, nil
	case "catch_all":
		return func(*snapshot.Repo) bool { return true }, nil
	}
	return nil, fmt.Errorf("unknown matcher %q", kind)
}

func (g *group) selects(r *snapshot.Repo) bool {
	return (len(g.sources) == 0 || g.sources[r.ID.Source]) && g.matches(r)
}

func (g *group) matches(r *snapshot.Repo) bool {
	return slices.ContainsFunc(g.match, func(m matcher) bool { return m(r) })
}

// Resolve returns the groups with their members among repos.
func (gs *Groups) Resolve(repos map[snapshot.RepoKey]*snapshot.RepoEntry) []snapshot.Group {
	out := make([]snapshot.Group, 0, len(gs.groups))
	for _, g := range gs.groups {
		sg := snapshot.Group{ID: g.cfg.ID, Name: g.cfg.Name, Description: g.cfg.Description, Members: []snapshot.RepoKey{}}
		for k, e := range repos {
			if g.selects(&e.Repo) {
				sg.Members = append(sg.Members, k)
				if strings.EqualFold(e.Repo.Name, g.cfg.DefaultRepo) && (sg.DefaultRepo == "" || k < sg.DefaultRepo) {
					sg.DefaultRepo = k
				}
			}
		}
		slices.SortFunc(sg.Members, cmp.Compare)
		sg.DefaultMissing = sg.DefaultRepo == ""
		out = append(out, sg)
	}
	return out
}
