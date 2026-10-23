"""Icon contrast analysis: simulations of the PWA icon on iOS/Android home
screens (light, dark, tinted/themed) and the in-app token contrast table."""
import sys
from PIL import Image, ImageDraw, ImageFont, ImageChops, ImageOps

IMG = "/home/user/streamline/web/static/images/"
OUT = sys.argv[1]
F = "/usr/share/fonts/opentype/inter/"
font = lambda s, w="Regular": ImageFont.truetype(F + f"Inter-{w}.otf", s)

BG_DEEP = (11, 11, 16)
ACCENT = (124, 140, 255)  # --accent #7c8cff


def lum(c):
    def f(v):
        v /= 255
        return v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4
    return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2])


def cr(a, b):
    x, y = sorted([lum(a), lum(b)], reverse=True)
    return (x + 0.05) / (y + 0.05)


def hexc(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) for i in (0, 2, 4))


# ---- source icons -------------------------------------------------------
fav = Image.open(IMG + "favicon-512.png").convert("RGBA")
apple = Image.open(IMG + "apple-touch-icon.png").convert("RGBA")
mask_cur = Image.open(IMG + "icon-maskable-512.png").convert("RGBA")

# Logo alpha recovered from the favicon: blue channel between tile (28) and
# logo (248), anti-aliasing kept.
b = fav.getchannel("B")
logo_alpha = b.point(lambda v: max(0, min(255, int((v - 40) * 255 / (240 - 40)))))
# drop the grey rim (it is blue too, but neutral): require B - R > 40
r = fav.getchannel("R")
chroma = ImageChops.subtract(b, r).point(lambda v: 255 if v > 40 else 0)
logo_alpha = ImageChops.multiply(logo_alpha, chroma)
logo_alpha = ImageChops.multiply(logo_alpha, fav.getchannel("A"))


# Optical centring: the box is centred but the ink sits 21 px left of centre
# (the play glyph's heavy left bar vs its thin point). Shift halfway.
SHIFT_X = 10
logo_alpha_cur = logo_alpha
logo_alpha = ImageChops.offset(logo_alpha, SHIFT_X, 0)


def solid(size, color, alpha=None):
    im = Image.new("RGBA", size, color + (255,))
    if alpha is not None:
        im.putalpha(alpha)
    return im


def proposed_fullbleed(size, bg=BG_DEEP, fg=ACCENT):
    im = Image.new("RGBA", (512, 512), bg + (255,))
    im.alpha_composite(solid((512, 512), fg, logo_alpha))
    return im.resize((size, size), Image.LANCZOS)


def monochrome(size=512):
    im = Image.new("RGBA", (512, 512), (0, 0, 0, 0))
    im.alpha_composite(solid((512, 512), (255, 255, 255), logo_alpha))
    return im.resize((size, size), Image.LANCZOS)


prop_apple = proposed_fullbleed(180)
prop_any = proposed_fullbleed(512)
prop_mask = proposed_fullbleed(512)
prop_mono = monochrome()
for name, im in [("proposed-apple-touch-icon.png", prop_apple),
                 ("proposed-icon-512.png", prop_any),
                 ("proposed-icon-maskable-512.png", prop_mask),
                 ("proposed-icon-monochrome-512.png", prop_mono)]:
    im.save(OUT + "/" + name, optimize=True)


# ---- platform rendering -------------------------------------------------
def rr_mask(size, radius_frac, ss=4):
    m = Image.new("L", (size * ss, size * ss), 0)
    ImageDraw.Draw(m).rounded_rectangle([0, 0, size * ss - 1, size * ss - 1],
                                        radius=int(size * ss * radius_frac), fill=255)
    return m.resize((size, size), Image.LANCZOS)


def circle_mask(size, ss=4):
    m = Image.new("L", (size * ss, size * ss), 0)
    ImageDraw.Draw(m).ellipse([0, 0, size * ss - 1, size * ss - 1], fill=255)
    return m.resize((size, size), Image.LANCZOS)


def ios_render(icon, size):
    """iOS draws transparent pixels of an apple-touch-icon as black, then
    clips with its own continuous-corner mask (~22.5%)."""
    base = Image.new("RGBA", icon.size, (0, 0, 0, 255))
    base.alpha_composite(icon)
    out = base.resize((size, size), Image.LANCZOS)
    out.putalpha(rr_mask(size, 0.225))
    return out


def tinted(im, tint=(255, 196, 92)):
    """iOS 18 tinted mode approximation: luminance only, recoloured with the
    user's tint over a near-black plate."""
    a = im.getchannel("A")
    g = ImageOps.grayscale(im.convert("RGB"))
    g = ImageOps.autocontrast(g, cutoff=0)
    col = Image.merge("RGB", [g.point(lambda v, t=t: int(v * t / 255)) for t in tint])
    col = col.convert("RGBA")
    col.putalpha(a)
    return col


def android_render(icon, size, shape):
    out = icon.resize((size, size), Image.LANCZOS)
    # adaptive icon: 108dp canvas, ~72dp visible -> crop the central 2/3?  The
    # W3C maskable spec only guarantees the 80% circle; launchers show more.
    # We show the full 512 canvas clipped by the shape, as Chrome's WebAPK does.
    m = circle_mask(size) if shape == "circle" else rr_mask(size, 0.30 if shape == "squircle" else 0.12)
    out.putalpha(ImageChops.multiply(out.getchannel("A"), m))
    return out


def themed(mono, size, dark):
    """Material You themed icon: monochrome layer drawn in a tonal colour on a
    tonal plate."""
    plate, fg = ((43, 47, 69), (196, 204, 255)) if dark else ((218, 224, 255), (32, 40, 96))
    im = Image.new("RGBA", (size, size), plate + (255,))
    m = mono.resize((size, size), Image.LANCZOS).getchannel("A")
    im.alpha_composite(solid((size, size), fg, m))
    im.putalpha(circle_mask(size))
    return im


WALL = {"light": (236, 236, 244), "dark": (0, 0, 0)}
TXT = {"light": (30, 30, 36), "dark": (235, 235, 240)}


def panel(title, rows, cell=170, icon=120, path=None):
    """rows: list of (row_label, [(col_label, mode, image)])"""
    cols = max(len(r[1]) for r in rows)
    lw = 230
    W = lw + cols * cell + 20
    H = 70 + len(rows) * (cell + 40)
    im = Image.new("RGB", (W, H), (250, 250, 252))
    d = ImageDraw.Draw(im)
    d.text((20, 20), title, fill=(20, 20, 26), font=font(26, "SemiBold"))
    y = 70
    for label, cells in rows:
        d.text((20, y + cell // 2 - 12), label, fill=(20, 20, 26), font=font(17, "SemiBold"))
        for i, (cl, mode, ic) in enumerate(cells):
            x = lw + i * cell
            d.rectangle([x, y, x + cell - 10, y + cell - 10], fill=WALL[mode])
            ox = x + (cell - 10 - icon) // 2
            oy = y + 10
            im.paste(ic, (ox, oy), ic)
            dd = ImageDraw.Draw(im)
            dd.text((x + (cell - 10) // 2, y + cell - 22), "Streamline", fill=TXT[mode],
                    font=font(13, "Medium"), anchor="mm")
            dd.text((x + (cell - 10) // 2, y + cell + 6), cl, fill=(70, 70, 80),
                    font=font(13), anchor="mm")
        y += cell + 40
    im.save(path, optimize=True)


I = 120
ios_cur = ios_render(apple, I)
ios_new = ios_render(prop_apple, I)
panel("iOS — home screen (apple-touch-icon)", [
    ("Current", [("Light", "light", ios_cur), ("Dark", "dark", ios_cur),
                ("Tinted (approx.)", "dark", tinted(ios_cur))]),
    ("Proposed", [("Light", "light", ios_new), ("Dark", "dark", ios_new),
                 ("Tinted (approx.)", "dark", tinted(ios_new))]),
], path=OUT + "/ios-home-screen.png")

rows = []
for lbl, src in [("Current", mask_cur), ("Proposed", prop_mask)]:
    rows.append((lbl, [
        ("Circle · light", "light", android_render(src, I, "circle")),
        ("Circle · dark", "dark", android_render(src, I, "circle")),
        ("Squircle · light", "light", android_render(src, I, "squircle")),
        ("Squircle · dark", "dark", android_render(src, I, "squircle")),
    ]))
cur_unthemed = android_render(mask_cur, I, "circle")
rows.append(("Themed icons\n(Material You)", [
    ("Current · light", "light", cur_unthemed),
    ("Current · dark", "dark", cur_unthemed),
    ("Proposed · light", "light", themed(prop_mono, I, False)),
    ("Proposed · dark", "dark", themed(prop_mono, I, True)),
]))
panel("Android — adaptive (maskable) and themed icons", rows,
      path=OUT + "/android-home-screen.png")

# ---- zoom on the maskable rim -------------------------------------------
z = android_render(mask_cur, 512, "circle").crop((0, 150, 140, 362)).resize((280, 424), Image.NEAREST)
zn = android_render(prop_mask, 512, "circle").crop((0, 150, 140, 362)).resize((280, 424), Image.NEAREST)
W, H = 660, 520
zim = Image.new("RGB", (W, H), (250, 250, 252))
d = ImageDraw.Draw(zim)
d.text((20, 18), "Zoom ×2 — left edge under a circle mask", fill=(20, 20, 26), font=font(22, "SemiBold"))
for i, (lbl, src) in enumerate([("Current: grey rim + two background tones", z), ("Proposed: solid background", zn)]):
    x = 20 + i * 320
    zim.paste((0, 0, 0), (x, 60, x + 300, 500))
    zim.paste(src, (x + 10, 68), src)
    d.text((x, 502), lbl, fill=(70, 70, 80), font=font(13))
zim.save(OUT + "/android-maskable-rim-zoom.png", optimize=True)

# ---- safe zone ----------------------------------------------------------
sz = mask_cur.copy()
dd = ImageDraw.Draw(sz)
dd.ellipse([256 - 205, 256 - 205, 256 + 205, 256 + 205], outline=(46, 204, 113, 255), width=4)
dd.ellipse([0, 0, 511, 511], outline=(239, 68, 68, 255), width=3)
sz.convert("RGB").resize((360, 360), Image.LANCZOS).save(OUT + "/maskable-safe-zone.png")

# ---- in-app contrast table ---------------------------------------------
surfaces = [("bg-deep", "#0b0b10"), ("bg-base", "#111118"), ("bg-elevated", "#15151f"),
            ("bg-card", "#1c1c28"), ("bg-hover", "#232333")]
tokens = [("fg", "#f5f5f7"), ("fg-muted", "#c8c8d2"), ("fg-subtle", "#8a8a99"),
          ("fg-faint", "#5a5a68"), ("fg-faint → #74748a (proposed)", "#74748a"),
          ("accent", "#7c8cff"), ("accent-text", "#b8c2ff"),
          ("status-failed", "#ef4444"), ("status-downloading", "#3b82f6"),
          ("status-missing", "#64748b"), ("status-paused", "#71717a")]
from PIL import Image as _I
cw, rh, lw = 150, 52, 300
W = lw + cw * len(surfaces) + 20
H = 120 + rh * len(tokens) + 60
im = _I.new("RGB", (W, H), (250, 250, 252))
d = ImageDraw.Draw(im)
d.text((20, 18), "In-app contrast — Lucide icon + 12px text on each surface",
       fill=(20, 20, 26), font=font(22, "SemiBold"))
d.text((20, 50), "✓ ≥4.5 (text)   ◐ ≥3 (icon / large text only)   ✗ <3 (fails WCAG 1.4.11 and 1.4.3)",
       fill=(70, 70, 80), font=ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 14))
for j, (sn, sh) in enumerate(surfaces):
    d.text((lw + j * cw + cw // 2, 95), f"{sn}\n{sh}", fill=(20, 20, 26), font=font(13, "Medium"),
           anchor="mm", align="center")
for i, (tn, th) in enumerate(tokens):
    y = 120 + i * rh
    d.text((20, y + rh // 2), tn, fill=(20, 20, 26), font=font(14, "Medium"), anchor="lm")
    for j, (sn, sh) in enumerate(surfaces):
        x = lw + j * cw
        sc, tc = hexc(sh), hexc(th)
        d.rectangle([x, y, x + cw - 4, y + rh - 4], fill=sc)
        # a "play" glyph standing in for an icon, 2px stroke like Lucide
        cx, cy = x + 22, y + rh // 2 - 2
        d.polygon([(cx - 7, cy - 9), (cx - 7, cy + 9), (cx + 9, cy)], outline=tc, width=2)
        v = cr(sc, tc)
        mark = "✓" if v >= 4.5 else ("◐" if v >= 3 else "✗")
        col = (34, 197, 94) if v >= 4.5 else ((245, 158, 11) if v >= 3 else (239, 68, 68))
        d.text((x + 40, y + rh // 2 - 2), f"{v:.2f}", fill=tc, font=font(15, "SemiBold"), anchor="lm")
        d.text((x + cw - 14, y + rh // 2 - 2), mark, fill=col, font=ImageFont.truetype(
            "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 16), anchor="rm")
im.save(OUT + "/in-app-contrast.png", optimize=True)

print("logo/tile", round(cr((113, 120, 248), (23, 22, 28)), 2))
print("accent/bg-deep", round(cr(ACCENT, BG_DEEP), 2))
print("tile/black", round(cr((23, 22, 28), (0, 0, 0)), 2), "bg-deep/black", round(cr(BG_DEEP, (0, 0, 0)), 2))
for s, h in surfaces:
    print("#74748a on", s, round(cr(hexc("#74748a"), hexc(h)), 2))
