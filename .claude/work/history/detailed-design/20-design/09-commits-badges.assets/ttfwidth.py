import struct, sys
b=open('tools/badgewidths/font/DejaVuSans.ttf','rb').read()
n=struct.unpack('>H',b[4:6])[0]
tabs={}
for i in range(n):
    tag,cs,off,ln=struct.unpack('>4sIII',b[12+16*i:28+16*i]); tabs[tag.decode()]=(off,ln)
upm=struct.unpack('>H',b[tabs['head'][0]+18:tabs['head'][0]+20])[0]
nh=struct.unpack('>H',b[tabs['hhea'][0]+34:tabs['hhea'][0]+36])[0]
hm=tabs['hmtx'][0]
def adv(g):
    g=min(g,nh-1); return struct.unpack('>H',b[hm+4*g:hm+4*g+2])[0]
co=tabs['cmap'][0]
nt=struct.unpack('>H',b[co+2:co+4])[0]
cm={}
for i in range(nt):
    pid,eid,off=struct.unpack('>HHI',b[co+4+8*i:co+12+8*i])
    if (pid,eid) in ((3,1),(0,3),(3,10)):
        o=co+off; fmt=struct.unpack('>H',b[o:o+2])[0]
        if fmt==4:
            sc=struct.unpack('>H',b[o+6:o+8])[0]//2
            ends=struct.unpack('>%dH'%sc,b[o+14:o+14+2*sc]); st=o+16+2*sc
            starts=struct.unpack('>%dH'%sc,b[st:st+2*sc]); dl=st+2*sc
            deltas=struct.unpack('>%dh'%sc,b[dl:dl+2*sc]); ro=dl+2*sc
            ros=struct.unpack('>%dH'%sc,b[ro:ro+2*sc])
            for s in range(sc):
                for c in range(starts[s],ends[s]+1):
                    if c==0xFFFF: continue
                    if ros[s]==0: g=(c+deltas[s])&0xFFFF
                    else:
                        p=ro+2*s+ros[s]+2*(c-starts[s]); g=struct.unpack('>H',b[p:p+2])[0]
                        if g: g=(g+deltas[s])&0xFFFF
                    cm[c]=g
        break
def w(s):
    t=sum(adv(cm[ord(c)]) for c in s)*11/upm
    v=int(t); return (v+1 if v%2==0 else v), round(t,2)
for s in sys.argv[1:]: print(repr(s), *w(s))
