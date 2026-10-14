from tokens import *
# categorical viz palette (graph nodes, spread bars): hues spaced, colour-vision-friendly order, >=3:1 on card (graphical objects, 1.4.11)
VL=[(0.50,0.14,262),(0.55,0.13,150),(0.55,0.15,45),(0.50,0.17,330),(0.55,0.10,200),(0.52,0.16,20),(0.55,0.12,95),(0.45,0.03,260)]
VD=[(0.72,0.13,262),(0.76,0.14,150),(0.78,0.13,60),(0.74,0.15,330),(0.78,0.10,200),(0.72,0.15,25),(0.82,0.12,100),(0.72,0.02,260)]
def fitv(v):
    L,C,h=v
    while clipped(L,C,h) and C>0: C=round(C-0.002,4)
    return (L,C,h)
VL=[fitv(v) for v in VL]; VD=[fitv(v) for v in VD]
print('viz light on card'); 
for i,v in enumerate(VL,1): print(i,v,hexof(oklch(*v)),round(cr(oklch(*v),oklch(*LIGHT['card'])),2))
print('viz dark on card');
for i,v in enumerate(VD,1): print(i,v,hexof(oklch(*v)),round(cr(oklch(*v),oklch(*DARK['card'])),2))
# avatar backgrounds with white text (>=4.5) fixed for both themes
AV=[(0.50,0.14,262),(0.50,0.12,150),(0.52,0.14,45),(0.48,0.16,330),(0.50,0.09,200),(0.50,0.17,20),(0.50,0.11,95),(0.46,0.03,260)]
AV=[fitv(v) for v in AV]
print('avatar bg vs white text')
for i,v in enumerate(AV,1): print(i,v,hexof(oklch(*v)),round(cr(oklch(*v),oklch(1,0,0)),2))
