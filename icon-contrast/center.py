import numpy as np
from PIL import Image, ImageDraw, ImageFont, ImageChops
F="/usr/share/fonts/opentype/inter/Inter-"
f=lambda s,w="Regular": ImageFont.truetype(F+w+".otf",s)
A=np.load("alpha_clean.npy").clip(0,255).astype(np.uint8)
alpha=Image.fromarray(A,"L")
BG=(11,11,16); ACC=(124,140,255)
def icon(dx, size=512):
    im=Image.new("RGBA",(512,512),BG+(255,))
    a=ImageChops.offset(alpha,dx,0)
    im.alpha_composite(Image.merge("RGBA",[Image.new("L",(512,512),c) for c in ACC]+[a]))
    return im.resize((size,size),Image.LANCZOS), a
def stats(a):
    M=np.asarray(a,float); Y,X=np.indices(M.shape); s=M.sum()
    ys,xs=np.nonzero(M>128)
    return ((xs.min()+xs.max())/2,(ys.min()+ys.max())/2),((X*M).sum()/s,(Y*M).sum()/s)
def rr(size,r):
    m=Image.new("L",(size*4,size*4),0); ImageDraw.Draw(m).rounded_rectangle([0,0,size*4-1,size*4-1],radius=int(size*4*r),fill=255)
    return m.resize((size,size),Image.LANCZOS)
cols=[("Current (box-centred)",0),("Proposed: +10 px",10),("Ink-centred: +21 px",21)]
S=300; cw=360; W=40+cw*3; H=640
out=Image.new("RGB",(W,H),(250,250,252)); d=ImageDraw.Draw(out)
d.text((24,18),"Logo centring — 512 px canvas, horizontal shift only",fill=(20,20,26),font=f(24,"SemiBold"))
d.text((24,52),"cyan = centre of the bounding box   orange = centre of mass of the ink   white = canvas centre",fill=(70,70,80),font=f(14))
for i,(lbl,dx) in enumerate(cols):
    x=24+i*cw; y=90
    big,a=icon(dx,S); (bx,by),(cx,cy)=stats(a); k=S/512
    big=big.convert("RGB"); dd=ImageDraw.Draw(big)
    dd.line([S/2,0,S/2,S],fill=(255,255,255),width=1); dd.line([0,S/2,S,S/2],fill=(255,255,255),width=1)
    for (px,py),col in [((bx,by),(34,211,238)),((cx,cy),(245,158,11))]:
        px*=k; py*=k; dd.line([px,0,px,S],fill=col,width=1); dd.ellipse([px-5,py-5,px+5,py+5],fill=col)
    out.paste(big,(x,y))
    d.text((x,y+S+10),lbl,fill=(20,20,26),font=f(17,"SemiBold"))
    d.text((x,y+S+36),f"box centre x={bx:.0f} ({bx-256:+.0f})   ink centre x={cx:.0f} ({cx-256:+.0f})",fill=(70,70,80),font=f(13))
    # home-screen size, no guides
    for j,wall in enumerate([(236,236,244),(0,0,0)]):
        sx=x+j*165; sy=y+S+70
        d.rectangle([sx,sy,sx+150,sy+150],fill=wall)
        sm,_=icon(dx,110); sm.putalpha(rr(110,0.225)); out.paste(sm,(sx+20,sy+20),sm)
out.save("out/logo-centering.png",optimize=True)
