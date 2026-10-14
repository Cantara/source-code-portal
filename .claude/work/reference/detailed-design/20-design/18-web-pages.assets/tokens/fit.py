from tokens import *
def fit(v):
    L,C,h=v
    while clipped(L,C,h) and C>0: C=round(C-0.002,4)
    return (L,C,h)
for name,d in (('LIGHT',LIGHT),('DARK',DARK)):
    print(name)
    for k,v in d.items():
        f=fit(v)
        if f!=v: print('  ',repr(k),v,'->',f)
