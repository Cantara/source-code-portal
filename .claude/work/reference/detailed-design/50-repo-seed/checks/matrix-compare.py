import json, os, re, sys
arch = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', '10-architecture.md')).read().split('\n')
rules = {r['Pkg']: r for r in json.load(open(sys.argv[1]))['rules']}
K4 = ['scm','freshness','job','routes','readmodel','store','httpapi','httpapi/authz','platform/secret','platform/logx']
start = next(i for i,l in enumerate(arch) if l.startswith('| Rank | Package | May import'))
rows = []
for l in arch[start+2:]:
    if not l.startswith('|'): break
    c = [x.strip() for x in l.strip().strip('|').split('|')]
    rows.append(c)
def pk(s):
    return re.findall(r'`([^`]+)`', s)
def expand_pkg(p):
    p = p.strip()
    m = re.match(r'(.*)\{([^}]*)\}(.*)', p)
    if m:
        return [m.group(1)+x+m.group(3) for x in m.group(2).split(',')]
    return [p]
FEATS = {'catalog','commits','docs','manifests','badges','pages','integrations','media'}
problems = []; checked = 0
for rank, pkg, allow, third in rows:
    pkgs = pk(pkg)
    allowed_txt = allow
    # the row's allowed list
    toks = []
    if allowed_txt.strip().lower() == 'none': toks = []
    else:
        a = allowed_txt
        if 'K4' in a: toks += K4
        for t in pk(a):
            toks += [x for x in expand_pkg(t)]
        # prose handling: 'the **roots** of ...' handled below
    for p0 in pkgs:
        for p in expand_pkg(p0):
            p = p.replace('<feature>/<feature>api', 'FEATAPI')
            if p == 'FEATAPI': continue   # generated api packages checked separately
            key = None
            for cand in ('internal/'+p, p):
                if cand in rules: key = cand; break
            if key is None:
                # pattern rows
                for cand in rules:
                    if cand.endswith('/*') and re.fullmatch(cand.replace('*','[^/]+'), 'internal/'+p): key = cand
            if key is None:
                if p.startswith('cmd/') : 
                    key = p if p in rules else None
            if key is None:
                problems.append(f'NO RULE for architecture row {p}'); continue
            checked += 1
            r = rules[key]
            have = set(x for x in (r['Allow'] or []))
            want = set()
            for t in toks:
                if t.startswith('<') or t in ('none',): continue
                if t.startswith('cmd/'): want.add(t)
                else: want.add('internal/'+t if not t.startswith('internal/') else t)
            # special prose rows
            if p == 'overview': 
                want |= {'internal/'+f for f in ['catalog','commits','docs','manifests','badges','pages','integrations']}
            if r['AllowAll']: continue
            if p == 'cmd/scp/internal/bridge':
                continue
            extra = have - want; missing = want - have
            if extra or missing:
                problems.append(f'{key}: rules.go-only {sorted(extra)} architecture-only {sorted(missing)}')
print('rows checked:', checked)
for x in problems: print(x)
