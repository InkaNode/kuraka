# Final kit: double-jamb door with stepped lintel. Wordmark strokes converted to filled outlines.
import math, os
from gen7 import arch, poly
T, OC, DK, CR = "#A5431E", "#D9922E", "#3B1F14", "#FFF7F2"
SW = 28; H = SW/2
def clip(pts, lo=40, hi=200):
    for keep, y0 in ((lambda p: p[1] >= lo, lo), (lambda p: p[1] <= hi, hi)):
        out = []
        for i, p in enumerate(pts):
            q = pts[i-1]
            if keep(p) != keep(q):
                t = (y0-q[1])/(p[1]-q[1]); out.append((q[0]+t*(p[0]-q[0]), y0))
            if keep(p): out.append(p)
        pts = out
    return pts
def seg(x1, y1, x2, y2):  # square-capped stroke as a polygon
    dx, dy = x2-x1, y2-y1; L = math.hypot(dx, dy); ux, uy = dx/L*H, dy/L*H
    a, b = (x1-ux, y1-uy), (x2+ux, y2+uy)
    return clip([(a[0]-uy, a[1]+ux), (b[0]-uy, b[1]+ux), (b[0]+uy, b[1]-ux), (a[0]+uy, a[1]-ux)])
def curve(p0, p1, p2, p3, n=40):
    pts, nor = [], []
    for i in range(n+1):
        t = i/n; m = 1-t
        x = m**3*p0[0]+3*m*m*t*p1[0]+3*m*t*t*p2[0]+t**3*p3[0]; y = m**3*p0[1]+3*m*m*t*p1[1]+3*m*t*t*p2[1]+t**3*p3[1]
        dx = 3*m*m*(p1[0]-p0[0])+6*m*t*(p2[0]-p1[0])+3*t*t*(p3[0]-p2[0]); dy = 3*m*m*(p1[1]-p0[1])+6*m*t*(p2[1]-p1[1])+3*t*t*(p3[1]-p2[1])
        L = math.hypot(dx, dy); pts.append((x, y)); nor.append((-dy/L*H, dx/L*H))
    end = (pts[-1][0]+H, pts[-1][1])  # square cap continues the horizontal end
    return [(x+a, y+b) for (x, y), (a, b) in zip(pts, nor)] + [(end[0], end[1]+H), (end[0], end[1]-H)] + [(x-a, y-b) for (x, y), (a, b) in reversed(list(zip(pts, nor)))]
def P(pts, dx): return "M" + "L".join(f"{x+dx:.1f} {y:.1f}" for x, y in pts) + "Z"
def ellring(cx, cy):
    e = lambda rx, ry, s: f"M{cx-rx:.1f} {cy}A{rx} {ry} 0 1 {s} {cx+rx:.1f} {cy}A{rx} {ry} 0 1 {s} {cx-rx:.1f} {cy}Z"
    return e(37+H, 38+H, 1) + e(37-H, 38-H, 0)
def word():
    ds, x = [], H
    for ch in "kuraka":
        if ch == "k": ds += [P(seg(0, 54, 0, 186), x), P(seg(66, 110, 6, 162), x), P(seg(28, 144, 68, 186), x)]; w = 68
        if ch == "u": ds += [f"M{x-H} 96V150A50 50 0 0 0 {x+72+H} 150V96H{x+72-H}V150A22 22 0 0 1 {x+H} 150V96Z", P(seg(72, 110, 72, 186), x)]; w = 72
        if ch == "r": ds += [P(seg(0, 110, 0, 186), x), P(curve((0, 156), (0, 126), (22, 110), (52, 110)), x)]; w = 52
        if ch == "a": ds += [P(seg(74, 110, 74, 186), x), ellring(x+37, 148)]; w = 74
        x += w + 30
    return ds, x - 30 + H
# Master symbol (large) and small-size cut (one step, thicker parts, wider gap).
big = [(T, arch(34, 228, 72.7, .12, 28, 28)), (T, poly([(80, 35), (80, 24), (104, 24), (104, 14), (152, 14), (152, 24), (176, 24), (176, 35)])),
       (OC, arch(72, 228, 72+.12*44-38, .12, 20, 22))]
small = [(T, arch(44, 228, 73.9, .12, 34, 30)), (T, poly([(88, 45), (88, 20), (168, 20), (168, 45)])),
         (OC, arch(88, 228, 31.2, .12, 24, 24))]
def paths(parts, recolor=None): return "".join(f'<path fill="{(recolor or {}).get(c, c)}" fill-rule="evenodd" d="{d}"/>' for c, d in parts)
def svg(vb, body): return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}">{body}</svg>'
wd, ww = word()
def wordg(color, tx, ty, s): return f'<g fill="{color}" transform="translate({tx} {ty:.1f}) scale({s})">' + "".join(f'<path fill-rule="evenodd" d="{d}"/>' for d in wd) + "</g>"
os.makedirs("kit", exist_ok=True)
REV = {T: CR}  # reversed: terracotta parts become cream, ocre stays
files = {
 "kuraka-symbol": svg("0 0 256 256", paths(big)),
 "kuraka-symbol-small": svg("0 0 256 256", paths(small)),
 "kuraka-symbol-reversed": svg("0 0 256 256", paths(big, REV)),
 "kuraka-wordmark": svg(f"0 40 {ww:.0f} 160", wordg(T, 0, 0, 1)),
 "kuraka-horizontal": svg(f"0 0 {280+ww*.82:.0f} 256", paths(big) + wordg(T, 280, 256*.09, .82)),
 "kuraka-horizontal-reversed": svg(f"0 0 {280+ww*.82:.0f} 256", paths(big, REV) + wordg(CR, 280, 256*.09, .82)),
 "kuraka-stacked": svg(f"0 0 {max(256, ww*.6):.0f} 380", f'<g transform="translate({(max(256, ww*.6)-256)/2:.1f} 0)">{paths(big)}</g>' + wordg(T, (max(256, ww*.6)-ww*.6)/2, 252, .6)),
 "kuraka-app-icon": svg("0 0 256 256", f'<rect width="256" height="256" rx="56" fill="{DK}"/><g transform="translate(128 132) scale(.7) translate(-128 -128)">{paths(small, REV)}</g>'),
}
for n, s in files.items(): open(f"kit/{n}.svg", "w", encoding="utf-8").write(s)
print(f"word width {ww:.1f}")
