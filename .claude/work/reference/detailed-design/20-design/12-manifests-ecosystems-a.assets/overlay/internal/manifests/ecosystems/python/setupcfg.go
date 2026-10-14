package python

import (
	"context"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
)

type setupCfg struct{ base }

func NewSetupCfg() manifests.Parser {
	return setupCfg{base{id: SetupCfgID, match: func(p string) bool { return baseName(p) == "setup.cfg" && !vendoredPath(p) }, kind: manifests.KindManifest}}
}

type iniKey struct {
	val  string
	line int
}

// parseINI reads the subset of INI used by setuptools: [section], key = value (or key: value), indented continuation
// lines, '#' and ';' full-line comments. Keys are lower-cased; later duplicates override earlier ones.
func parseINI(data []byte) map[string]map[string]iniKey {
	out := map[string]map[string]iniKey{}
	sec, key := "", ""
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			sec, key = strings.ToLower(strings.TrimSpace(t[1:len(t)-1])), ""
			if out[sec] == nil {
				out[sec] = map[string]iniKey{}
			}
			continue
		}
		if sec == "" {
			continue
		}
		if (raw[0] == ' ' || raw[0] == '\t') && key != "" {
			k := out[sec][key]
			k.val += "\n" + t
			out[sec][key] = k
			continue
		}
		j := strings.IndexAny(t, "=:")
		if j <= 0 {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(t[:j]))
		out[sec][key] = iniKey{val: strings.TrimSpace(t[j+1:]), line: i + 1}
	}
	return out
}

func iniList(v string) []string {
	var parts []string
	if strings.Contains(v, "\n") {
		parts = strings.Split(v, "\n")
	} else {
		parts = strings.Split(v, ";")
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && !strings.HasPrefix(p, "#") {
			out = append(out, p)
		}
	}
	return out
}

func (p setupCfg) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, rej := p.admit(f, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest, Toolchain: map[string]string{"language": "python"}}
	ini := parseINI(data)
	cs := &ctxState{out: &out}
	if md := ini["metadata"]; md != nil {
		if n := md["name"]; idOf(n.val).Name != "" {
			out.Provides = []manifests.Artifact{{ID: idOf(n.val), Display: cleanName(n.val), Version: manifests.CleanText(md["version"].val, 128), Kind: "library"}}
			if strings.HasPrefix(md["version"].val, "attr:") || strings.HasPrefix(md["version"].val, "file:") {
				out.Provides[0].Version = ""
			}
		}
		if l := md["license"].val; l != "" && len(l) <= 100 && !strings.Contains(l, "\n") {
			out.Licenses = append(out.Licenses, manifests.CleanText(l, 100))
		}
	}
	if o := ini["options"]; o != nil {
		if pr := o["python_requires"].val; pr != "" {
			out.Toolchain["python"] = manifests.CleanText(pr, 100)
		}
		for _, k := range []struct {
			key   string
			scope manifests.Scope
			group string
		}{{"install_requires", manifests.ScopeRuntime, "install_requires"}, {"setup_requires", manifests.ScopeBuild, "setup_requires"}, {"tests_require", manifests.ScopeTest, "tests_require"}} {
			e := o[k.key]
			for _, s := range iniList(e.val) {
				if d, ok := cs.req(s, k.scope, k.group, e.line); ok {
					out.Dependencies = append(out.Dependencies, d)
				}
			}
		}
	}
	if ex := ini["options.extras_require"]; ex != nil {
		for _, extra := range sortedKeys(ex) {
			for _, s := range iniList(ex[extra].val) {
				if d, ok := cs.req(s, manifests.ScopeOptional, extra, ex[extra].line); ok {
					d.Optional = true
					out.Dependencies = append(out.Dependencies, d)
				}
			}
		}
	}
	return out, nil
}

func sortedKeys(m map[string]iniKey) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sortStrings(ks)
	return ks
}
