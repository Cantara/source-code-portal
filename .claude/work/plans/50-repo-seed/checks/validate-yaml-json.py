import sys, json, glob, os
import yaml
root=sys.argv[1]
bad=0; n=0
for pat in ['**/*.yml','**/*.yaml']:
    for f in glob.glob(os.path.join(root,pat), recursive=True):
        if '/node_modules/' in f or '/docs/design/' in f: continue
        n+=1
        try:
            list(yaml.safe_load_all(open(f)))
        except Exception as e:
            bad+=1; print('YAML FAIL',f,e)
for pat in ['**/*.json','.*.json']:
    for f in glob.glob(os.path.join(root,pat), recursive=True):
        if '/node_modules/' in f or '/docs/design/' in f: continue
        n+=1
        try: json.load(open(f))
        except Exception as e:
            bad+=1; print('JSON FAIL',f,e)
print('checked',n,'files, failures',bad)
sys.exit(1 if bad else 0)
