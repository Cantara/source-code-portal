import math
def oklch_to_srgb(L,C,h):
    a=C*math.cos(math.radians(h)); b=C*math.sin(math.radians(h))
    l_=L+0.3963377774*a+0.2158037573*b; m_=L-0.1055613458*a-0.0638541728*b; s_=L-0.0894841775*a-1.2914855480*b
    l,m,s=l_**3,m_**3,s_**3
    r= 4.0767416621*l-3.3077115913*m+0.2309699292*s
    g=-1.2684380046*l+2.6097574011*m-0.3413193965*s
    bb=-0.0041960863*l-0.7034186147*m+1.7076147010*s
    return [max(0,min(1,x)) for x in (r,g,bb)]  # linear
def lum(rgb): r,g,b=rgb; return 0.2126*r+0.7152*g+0.0722*b
def cr(a,b):
    la,lb=lum(a),lum(b); 
    if la<lb: la,lb=lb,la
    return (la+0.05)/(lb+0.05)
def mix(fg,bg,alpha): return [fg[i]*alpha+bg[i]*(1-alpha) for i in range(3)]  # approx in linear space (browser mixes in sRGB; close enough, noted)
def mix_srgb(fg,bg,alpha):
    enc=lambda x: 12.92*x if x<=0.0031308 else 1.055*x**(1/2.4)-0.055
    dec=lambda x: x/12.92 if x<=0.04045 else ((x+0.055)/1.055)**2.4
    return [dec(enc(fg[i])*alpha+enc(bg[i])*(1-alpha)) for i in range(3)]
O=lambda L,C=0,h=0: oklch_to_srgb(L,C,h)
white=O(1); fg=O(0.145)
print('LIGHT')
print(' foreground/background', round(cr(fg,white),2))
print(' muted-foreground(0.556)/background', round(cr(O(0.556),white),2), ' on muted(0.97):', round(cr(O(0.556),O(0.97)),2))
print(' primary-foreground/primary', round(cr(O(0.985),O(0.205)),2))
print(' destructive(0.577,.245,27.3)/background', round(cr(O(0.577,0.245,27.325),white),2))
print(' border(0.922)/background (need 3:1 for UI boundaries)', round(cr(O(0.922),white),2))
print(' input(0.922)/background', round(cr(O(0.922),white),2))
print(' ring(0.708)/background (focus indicator needs 3:1)', round(cr(O(0.708),white),2))
print(' chart-1(0.87)/background', round(cr(O(0.87),white),2))
print('DARK')
bg=O(0.145)
print(' foreground/background', round(cr(O(0.985),bg),2))
print(' muted-foreground(0.708)/background', round(cr(O(0.708),bg),2), ' on muted(0.269):', round(cr(O(0.708),O(0.269)),2))
print(' destructive(0.704,.191,22.2)/background', round(cr(O(0.704,0.191,22.216),bg),2))
bd=mix_srgb(O(1),bg,0.10); inp=mix_srgb(O(1),bg,0.15)
print(' border(white 10%)/background', round(cr(bd,bg),2),' input(white 15%)/background', round(cr(inp,bg),2))
print(' ring(0.556)/background', round(cr(O(0.556),bg),2))
