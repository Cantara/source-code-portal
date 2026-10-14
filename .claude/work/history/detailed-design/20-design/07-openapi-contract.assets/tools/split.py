"""Mechanical split of the design documents into the ADR-020 repository layout (proof that the single-file
design documents are exactly the fragments of the repository, concatenated).

usage: split.py <public.yaml> <admin.yaml> <common.yaml> <outdir>
Layout produced:
  outdir/api/openapi.yaml, outdir/api/openapi.admin.yaml           roots ($ref path items of fragments)
  outdir/internal/httpapi/openapi/common.yaml
  outdir/internal/<feature>/<feature>api/<feature>.yaml            fragments
"""
import sys, os, re, copy, yaml, json

pub_f, adm_f, com_f, out = sys.argv[1:5]
pub = yaml.safe_load(open(pub_f)); adm = yaml.safe_load(open(adm_f))
for d in (pub, adm): d.pop('x-scp-fixtures', None)

def refs(node, acc):
    if isinstance(node, dict):
        for k, v in node.items():
            if k == '$ref' and isinstance(v, str) and v.startswith('#/components/'):
                acc.add(v.split('/')[-2] + '/' + v.split('/')[-1])
            else:
                refs(v, acc)
    elif isinstance(node, list):
        for x in node: refs(x, acc)

def closure(doc, seed):
    seen, todo = set(), list(seed)
    while todo:
        k = todo.pop()
        if k in seen: continue
        seen.add(k)
        kind, name = k.split('/')
        item = doc['components'][kind][name]
        acc = set(); refs(item, acc); todo.extend(acc)
    return seen

FEATURE = {'meta': None}  # tag -> feature directory ; meta stays in the root
def esc(p): return p.replace('~', '~0').replace('/', '~1')

def split(doc, tags_only, rootname, include_public_paths):
    frags = {}
    for path, item in doc['paths'].items():
        if '$ref' in item: continue
        op = next(v for k, v in item.items() if k in ('get', 'post', 'put', 'delete', 'patch'))
        tag = op['tags'][0]
        frags.setdefault(tag, {})[path] = item
    return frags

def write_fragment(tag, paths, doc, out):
    seed = set()
    for it in paths.values(): refs(it, seed)
    own = closure(doc, seed)
    comps = {}
    for k in sorted(own):
        kind, name = k.split('/')
        comps.setdefault(kind, {})[name] = doc['components'][kind][name]
    frag = {'openapi': '3.1.0', 'info': {'title': f'{tag} API fragment', 'version': '1.0.0'}, 'paths': paths}
    if comps: frag['components'] = comps
    d = os.path.join(out, 'internal', tag, tag + 'api'); os.makedirs(d, exist_ok=True)
    txt = yaml.safe_dump(frag, sort_keys=False, width=140, allow_unicode=True)
    txt = txt.replace("common.yaml#", "../../httpapi/openapi/common.yaml#")
    open(os.path.join(d, tag + '.yaml'), 'w').write(txt)
    return os.path.join('..', 'internal', tag, tag + 'api', tag + '.yaml')

os.makedirs(os.path.join(out, 'api'), exist_ok=True)
os.makedirs(os.path.join(out, 'internal/httpapi/openapi'), exist_ok=True)
open(os.path.join(out, 'internal/httpapi/openapi/common.yaml'), 'w').write(open(com_f).read())

pf = split(pub, None, None, True)
rootpaths = {}
for tag, paths in pf.items():
    if tag == 'meta':
        for p, it in paths.items(): rootpaths[p] = it
        continue
    rel = write_fragment(tag, paths, pub, out)
    for p in paths: rootpaths[p] = {'$ref': rel + '#/paths/' + esc(p)}
root = {k: v for k, v in pub.items() if k not in ('paths', 'components')}
root['paths'] = dict(sorted(rootpaths.items()))
root['components'] = {}
txt = yaml.safe_dump(root, sort_keys=False, width=140, allow_unicode=True).replace("common.yaml#", "../internal/httpapi/openapi/common.yaml#")
open(os.path.join(out, 'api/openapi.yaml'), 'w').write(txt)

# admin: ops fragment from the admin document, public path items re-used
af = split(adm, None, None, False)
ops_paths = af.get('ops', {})
rel = write_fragment('ops', ops_paths, adm, out)
admroot = copy.deepcopy(root)
admroot['info'] = adm['info']; admroot['tags'] = list(root['tags']) + adm['tags']
admroot['x-scp-contract'] = adm['x-scp-contract']
for p in ops_paths: admroot['paths'][p] = {'$ref': rel + '#/paths/' + esc(p)}
admroot['paths'] = dict(sorted(admroot['paths'].items()))
txt = yaml.safe_dump(admroot, sort_keys=False, width=140, allow_unicode=True).replace("common.yaml#", "../internal/httpapi/openapi/common.yaml#")
open(os.path.join(out, 'api/openapi.admin.yaml'), 'w').write(txt)
print('fragments:', sorted(pf), 'ops:', list(ops_paths))
