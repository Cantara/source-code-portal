import yaml,sys
ALLOWED={'type','format','pattern','minLength','maxLength','minimum','maximum','exclusiveMinimum','exclusiveMaximum','enum','const','default','description','examples','example','items','properties','required','additionalProperties','allOf','oneOf','anyOf','$ref','minItems','maxItems','uniqueItems','title','deprecated','externalDocs','discriminator','nullable','readOnly','writeOnly','multipleOf','not','prefixItems','contains','patternProperties','minProperties','maxProperties'}
bad=0
def walk_schema(n,path):
    global bad
    if isinstance(n,dict):
        for k,v in n.items():
            if k not in ALLOWED and not k.startswith('x-'):
                print('UNKNOWN schema key',k,'at',path); bad+=1
        for k in ('properties',):
            if isinstance(n.get(k),dict):
                for pk,pv in n[k].items(): walk_schema(pv,path+'/'+k+'/'+pk)
        if isinstance(n.get('items'),dict): walk_schema(n['items'],path+'/items')
        if isinstance(n.get('additionalProperties'),dict): walk_schema(n['additionalProperties'],path+'/ap')
        for k in ('allOf','oneOf','anyOf'):
            if isinstance(n.get(k),list):
                for i,x in enumerate(n[k]): walk_schema(x,path+'/'+k+str(i))
def find(n,path):
    # find schema occurrences: components.schemas.*, any 'schema' key, parameters schema
    if isinstance(n,dict):
        for k,v in n.items():
            if k=='schema' and isinstance(v,dict): walk_schema(v,path+'/schema')
            elif k=='schemas' and isinstance(v,dict) and path.endswith('components'):
                for sk,sv in v.items(): walk_schema(sv,path+'/schemas/'+sk)
            elif k!='x-scp-fixtures': find(v,path+'/'+str(k))
    elif isinstance(n,list):
        for i,x in enumerate(n): find(x,path+'/'+str(i))
for f in sys.argv[1:]:
    d=yaml.safe_load(open(f))
    find(d,f)
print('unknown keys:',bad)
