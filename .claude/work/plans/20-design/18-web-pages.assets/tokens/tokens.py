from contrast import *
H=260  # cool neutral hue
LIGHT={
 'background':(1,0,0),'foreground':(0.17,0.012,H),
 'card':(1,0,0),'card-foreground':(0.17,0.012,H),
 'muted':(0.962,0.005,H),'muted-foreground':(0.45,0.014,H),
 'subtle':(0.985,0.003,H),
 'border':(0.915,0.006,H),'input':(0.60,0.012,H),'ring':(0.50,0.19,262),
 'primary':(0.46,0.19,262),'primary-foreground':(0.985,0.003,H),
 'accent':(0.945,0.026,262),'accent-foreground':(0.28,0.10,262),
 'link':(0.46,0.19,262),
 'success':(0.47,0.13,150),'success-bg':(0.955,0.04,150),
 'warning':(0.48,0.108,65),'warning-bg':(0.96,0.042,85),
 'danger':(0.50,0.19,27),'danger-bg':(0.96,0.018,22),
 'info':(0.47,0.118,245),'info-bg':(0.955,0.022,245),
 'sidebar':(0.975,0.004,H),
}
DARK={
 'background':(0.165,0.012,H),'foreground':(0.965,0.004,H),
 'card':(0.205,0.013,H),'card-foreground':(0.965,0.004,H),
 'muted':(0.255,0.013,H),'muted-foreground':(0.74,0.012,H),
 'subtle':(0.185,0.012,H),
 'border':(0.30,0.014,H),'input':(0.58,0.014,H),'ring':(0.72,0.14,262),
 'primary':(0.74,0.132,262),'primary-foreground':(0.16,0.02,H),
 'accent':(0.30,0.06,262),'accent-foreground':(0.93,0.03,262),
 'link':(0.78,0.11,262),
 'success':(0.76,0.15,150),'success-bg':(0.27,0.05,150),
 'warning':(0.80,0.14,80),'warning-bg':(0.29,0.05,80),
 'danger':(0.75,0.15,25),'danger-bg':(0.29,0.06,25),
 'info':(0.76,0.12,245),'info-bg':(0.27,0.05,245),
 'sidebar':(0.19,0.013,H),
}
# (fg, bg, min, why)
PAIRS=[
 ('foreground','background',4.5,'body text'),
 ('foreground','card',4.5,'card text'),
 ('muted-foreground','background',4.5,'secondary text'),
 ('muted-foreground','muted',4.5,'secondary text on muted'),
 ('muted-foreground','card',4.5,'secondary text on card'),
 ('muted-foreground','sidebar',4.5,'secondary text on sidebar'),
 ('foreground','sidebar',4.5,'sidebar text'),
 ('primary-foreground','primary',4.5,'primary button'),
 ('link','background',4.5,'link text'),
 ('link','card',4.5,'link on card'),
 ('accent-foreground','accent',4.5,'active nav item'),
 ('input','background',3.0,'input boundary (1.4.11)'),
 ('input','card',3.0,'input boundary on card'),
 ('ring','background',3.0,'focus ring on background'),
 ('ring','card',3.0,'focus ring on card'),
 ('ring','muted',3.0,'focus ring on muted'),
 ('success','background',4.5,'success text'),('success','success-bg',4.5,'success badge'),
 ('warning','background',4.5,'warning text'),('warning','warning-bg',4.5,'warning badge'),
 ('danger','background',4.5,'danger text'),('danger','danger-bg',4.5,'danger badge'),
 ('info','background',4.5,'info text'),('info','info-bg',4.5,'info badge'),
 ('success','muted',4.5,'success on muted'),('warning','muted',4.5,'warning on muted'),('danger','muted',4.5,'danger on muted'),
]
def col(d,k):
    L,C,h=d[k]; return oklch(L,C,h)
def run(name,d,out):
    bad=0
    out.append(f'## {name}')
    for fg,bg,mn,why in PAIRS:
        r=cr(col(d,fg),col(d,bg)); ok=r>=mn
        if not ok: bad+=1
        out.append(f'{"PASS" if ok else "FAIL"} {r:5.2f} (>= {mn}) {fg} on {bg}: {why}')
    for k,v in d.items():
        if clipped(*v): out.append(f'WARN out of sRGB gamut (clipped): {k} {v}')
    return bad
if __name__=='__main__':
    out=[]; b=run('light',LIGHT,out)+run('dark',DARK,out); print('\n'.join(out)); print('FAILS',b)
