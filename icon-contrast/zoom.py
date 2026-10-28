import runpy, sys
from PIL import Image, ImageDraw, ImageFont
sys.argv = ["gen.py", "out"]
g = runpy.run_path("gen.py")
F = "/usr/share/fonts/opentype/inter/Inter-"
f = lambda s, w="Regular": ImageFont.truetype(F + w + ".otf", s)
cur = g["android_render"](g["mask_cur"], 512, "circle")
new = g["android_render"](g["prop_mask"], 512, "circle")
X0, Y0, X1, Y1, Z = 0, 226, 48, 286, 6
W, H = 1360, 1010
im = Image.new("RGB", (W, H), (250, 250, 252))
d = ImageDraw.Draw(im)
d.text((24, 18), "Android maskable icon under a circle mask — where the grey rim comes from",
       fill=(20, 20, 26), font=f(24, "SemiBold"))
red = (239, 68, 68)
for row, (lbl, src) in enumerate([("Current", cur), ("Proposed", new)]):
    y = 70 + row * 480
    d.text((24, y), lbl, fill=(20, 20, 26), font=f(20, "SemiBold"))
    # full icon on a black wallpaper, 400px
    s = 400 / 512
    d.rectangle([24, y + 36, 24 + 420, y + 36 + 420], fill=(0, 0, 0))
    ic = src.resize((400, 400), Image.LANCZOS)
    im.paste(ic, (34, y + 46), ic)
    bx = (34 + X0 * s - 3, y + 46 + Y0 * s, 34 + X1 * s, y + 46 + Y1 * s)
    d.rectangle(bx, outline=red, width=3)
    # zoom
    zx, zy = 520, y + 36
    crop = Image.new("RGBA", (X1 - X0, Y1 - Y0), (0, 0, 0, 255))
    c = src.crop((X0, Y0, X1, Y1))
    crop.alpha_composite(c)
    zoom = crop.resize(((X1 - X0) * Z, (Y1 - Y0) * Z), Image.NEAREST)
    im.paste(zoom, (zx, zy))
    d.rectangle([zx - 2, zy - 2, zx + zoom.width + 1, zy + zoom.height + 1], outline=red, width=3)
    d.line([bx[2], bx[1], zx - 2, zy - 2], fill=red, width=2)
    d.line([bx[2], bx[3], zx - 2, zy + zoom.height + 1], fill=red, width=2)
    d.text((zx, zy + zoom.height + 8), f"zoom ×{Z}", fill=(70, 70, 80), font=f(14))
    tx = zx + zoom.width + 30
    if row == 0:
        notes = [
            (2, "A  black: outside the circle (wallpaper)"),
            (6, "B  #0b0b10: background the favicon was flattened onto"),
            (11.5, "C  grey line: the favicon's 1px rim (#a1a1a2)"),
            (30, "D  #17161c: the favicon's tile, lighter than B"),
        ]
    else:
        notes = [(2, "A  black: outside the circle (wallpaper)"),
                 (30, "B  #0b0b10: one solid colour up to the mask edge")]
    for i, (px, t) in enumerate(notes):
        ax = zx + int(px * Z)
        ty = zy + 20 + i * 70
        d.line([ax, ty + 10, tx - 6, ty + 10], fill=(245, 158, 11), width=2)
        d.ellipse([ax - 5, ty + 5, ax + 5, ty + 15], fill=(245, 158, 11))
        d.text((tx, ty), t, fill=(20, 20, 26), font=f(15, "Medium"))
    if row == 0:
        d.text((tx, zy + 300), "The favicon is a rounded tile with a rim.\nThe maskable icon pastes it onto a darker\nsquare, so the rim and the tile edge land\ninside the visible circle (245px from the\ncentre, the mask radius is 256px).",
               fill=(70, 70, 80), font=f(14))
im.save("out/android-maskable-rim-zoom.png", optimize=True)
