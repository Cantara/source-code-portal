import math, sys, json
def lin2srgb(x): return 12.92*x if x<=0.0031308 else 1.055*x**(1/2.4)-0.055
def srgb2lin(x): return x/12.92 if x<=0.04045 else ((x+0.055)/1.055)**2.4
def oklch(L,C,h):
    a=C*math.cos(math.radians(h)); b=C*math.sin(math.radians(h))
    l_=L+0.3963377774*a+0.2158037573*b; m_=L-0.1055613458*a-0.0638541728*b; s_=L-0.0894841775*a-1.2914855480*b
    l,m,s=l_**3,m_**3,s_**3
    r= 4.0767416621*l-3.3077115913*m+0.2309699292*s
    g=-1.2684380046*l+2.6097574011*m-0.3413193965*s
    bb=-0.0041960863*l-0.7034186147*m+1.7076147010*s
    return tuple(max(0,min(1,x)) for x in (r,g,bb))   # linear, gamut-clipped
def lum(c): return 0.2126*c[0]+0.7152*c[1]+0.0722*c[2]
def cr(a,b):
    la,lb=lum(a),lum(b)
    if la<lb: la,lb=lb,la
    return (la+0.05)/(lb+0.05)
def mix(fg,bg,alpha):  # sRGB-space mix as browsers do
    return tuple(srgb2lin(lin2srgb(fg[i])*alpha+lin2srgb(bg[i])*(1-alpha)) for i in range(3))
def hexof(c): return '#%02x%02x%02x'%tuple(round(lin2srgb(x)*255) for x in c)
def clipped(L,C,h):
    a=C*math.cos(math.radians(h)); b=C*math.sin(math.radians(h))
    l_=L+0.3963377774*a+0.2158037573*b; m_=L-0.1055613458*a-0.0638541728*b; s_=L-0.0894841775*a-1.2914855480*b
    l,m,s=l_**3,m_**3,s_**3
    r= 4.0767416621*l-3.3077115913*m+0.2309699292*s
    g=-1.2684380046*l+2.6097574011*m-0.3413193965*s
    bb=-0.0041960863*l-0.7034186147*m+1.7076147010*s
    return any(x<-0.0005 or x>1.0005 for x in (r,g,bb))
