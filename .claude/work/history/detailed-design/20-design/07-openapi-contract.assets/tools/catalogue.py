"""Generates the per-operation catalogue sections from the design documents (python3 -I tools/catalogue.py <admin.yaml>).
The admin document is a superset, so it is the one that is read; public paths are resolved through the $ref items."""
import sys, yaml, os
adm_path = sys.argv[1]
base = os.path.dirname(adm_path)
pub = yaml.safe_load(open(os.path.join(base, 'openapi-public.yaml')))
com = yaml.safe_load(open(os.path.join(base, 'common.yaml')))
adm = yaml.safe_load(open(adm_path))

def unesc(s): return s.replace('~1', '/').replace('~0', '~')
def resolve(ref, doc):
    f, _, frag = ref.partition('#')
    d = doc
    if f.startswith('common'): d = com
    elif f.startswith('openapi-public'): d = pub
    for part in frag.strip('/').split('/'):
        d = d[unesc(part)]
    return d

def pitem(path):
    it = adm['paths'][path]
    if '$ref' in it:
        return resolve(it['$ref'], adm), 'public'
    return it, 'admin'

def deref(node, doc):
    while isinstance(node, dict) and '$ref' in node:
        node = resolve(node['$ref'], doc)
    return node

def sch(s, doc):
    if not isinstance(s, dict): return '?'
    if '$ref' in s: return s['$ref'].split('/')[-1]
    t = s.get('type')
    if isinstance(t, list): t = '|'.join(t)
    if 'enum' in s: return 'enum(' + ', '.join(str(x) for x in s['enum']) + ')'
    r = t or 'object'
    if t == 'array': r = 'array<' + sch(s.get('items', {}), doc) + '>'
    for k in ('minimum', 'maximum', 'default', 'minLength', 'maxLength', 'pattern'):
        if k in s: r += f' {k}={s[k]}'
    return r

out = []
for path in sorted(adm['paths']):
    it, where = pitem(path)
    doc = pub if where == 'public' else adm
    for m in ('get', 'post'):
        if m not in it: continue
        o = it[m]
        out.append(f"### `{m.upper()} {path}` : `{o['operationId']}`\n")
        out.append(f"{o['summary']}. Tag `{o['tags'][0]}`, owner `{o.get('x-scp-owner')}`, route `{o.get('x-scp-route')}`, ETag `{o.get('x-scp-etag', 'none')}`" + (f", flag `{o['x-scp-flag']}`" if 'x-scp-flag' in o else '') + (f", since {o['x-scp-since']}" if 'x-scp-since' in o else '') + (', admin listener only' if where == 'admin' else '') + '.\n')
        ps = []
        for p in o.get('parameters', []) + it.get('parameters', []):
            p = deref(p, doc)
            if p['name'] == 'If-None-Match': continue
            ps.append(f"`{p['name']}` ({p['in']}{', required' if p.get('required') else ''}): {sch(p.get('schema', {}), doc)}")
        if 'requestBody' in o:
            rb = deref(o['requestBody'], doc)
            for mt, c in rb['content'].items(): ps.append(f"body `{mt}`: {sch(c['schema'], doc)}")
        out.append('Parameters: ' + ('; '.join(ps) if ps else 'none (besides `If-None-Match`)') + '.\n')
        rs = []
        for code, r in o['responses'].items():
            rr = deref(r, doc)
            cts = ', '.join(rr.get('content', {}).keys()) or 'no body'
            ex = []
            for mt, c in rr.get('content', {}).items():
                if 'examples' in c: ex += list(c['examples'].keys())
                elif 'example' in c: ex.append('example')
            rs.append(f"`{code}` {cts}" + (f" [{', '.join(ex)}]" if ex else ''))
        out.append('Responses: ' + '; '.join(rs) + '.\n')
print('\n'.join(out))
