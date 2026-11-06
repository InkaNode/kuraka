# Round 7: fresh start, six concepts.
import math
from gen import word, g
from gen5 import build, C
def poly(pts):
    return "M" + "L".join(f"{x:.1f} {y:.1f}" for x, y in pts) + "Z"
def bar(x1, y1, x2, y2, w):
    dx, dy = x2-x1, y2-y1; L = math.hypot(dx, dy); nx, ny = -dy/L*w/2, dx/L*w/2; r = w/2
    return (f"M{x1+nx:.1f} {y1+ny:.1f}L{x2+nx:.1f} {y2+ny:.1f}A{r} {r} 0 0 0 {x2-nx:.1f} {y2-ny:.1f}"
            f"L{x1-nx:.1f} {y1-ny:.1f}A{r} {r} 0 0 0 {x1+nx:.1f} {y1+ny:.1f}Z")
def ell(cx, cy, rx, ry):
    return f"M{cx-rx} {cy}A{rx} {ry} 0 1 0 {cx+rx} {cy}A{rx} {ry} 0 1 0 {cx-rx} {cy}Z"
def arch(y0, y1, h0, k, t, tt, c=128):
    h = lambda y: h0 + k*(y-y0)
    return poly([(c-h(y1), y1), (c-h0, y0), (c+h0, y0), (c+h(y1), y1), (c+h(y1)-t, y1),
                 (c+h(y0+tt)-t, y0+tt), (c-h(y0+tt)+t, y0+tt), (c-h(y1)+t, y1)])
def spiral():
    out, inn = [], []
    for i in range(0, 241):
        th = 15.2*i/240; r = 8 + 6.2*th; w = 4 + 1.7*th
        out.append((r+w/2, th)); inn.append((max(r-w/2, 0), th))
    pts = [(math.cos(th)*r, math.sin(th)*r) for r, th in out] + [(math.cos(th)*r, math.sin(th)*r) for r, th in reversed(inn)]
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    ox, oy = 128-(max(xs)+min(xs))/2, 128-(max(ys)+min(ys))/2
    return poly([(x+ox, y+oy) for x, y in pts])
def pill(cx, cy, w, h):
    return bar(cx-(w-h)/2, cy, cx+(w-h)/2, cy, h)
xs, ends = [64, 104, 152, 192], [222, 160, 198, 128]
S = {
 "pa": [([arch(28, 228, 72, .12, 28, 34), arch(72, 228, 72+.12*44-38, .12, 20, 22)], [])],
 "pb": [([bar(36, 44, 220, 44, 24)] + [bar(x, 44, x, e, 16) for x, e in zip(xs, ends)], []),
        ([pill(64, 104, 36, 18), pill(64, 160, 36, 18), pill(104, 100, 36, 18),
          pill(152, 118, 36, 18), pill(152, 164, 36, 18), pill(192, 96, 36, 18)], [])],
 "pc": [([poly([(48, 36), (98, 30), (102, 118), (50, 126)]), poly([(50, 144), (102, 136), (104, 224), (46, 226)]),
          poly([(120, 118), (120, 96), (178, 32), (216, 34), (216, 52), (150, 118)]),
          poly([(120, 138), (150, 138), (216, 206), (216, 224), (178, 226), (120, 160)])], [])],
 "pd": [([ell(128, 118, 110, 76) + ell(128, 124, 92, 62), ell(128, 136, 80, 52) + ell(128, 141, 63, 40),
          ell(128, 150, 52, 32) + ell(128, 154, 36, 22), ell(128, 160, 22, 12)], [])],
 "pe": [([spiral()], [])],
 "pf": [([bar(78, 38, 78, 218, 36)], []), ([bar(128, 128, 206, 218, 34)], []), ([bar(78, 172, 204, 40, 34)], [])],
}
wbody, ww = word(color=C)
for k, layers in S.items():
    open(f"{k}-symbol.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">{build(k, layers)}</svg>')
    W = 256 + 24 + ww * 0.82
    open(f"{k}-lockup.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W:.0f} 256">{build("L"+k, layers)}'
        f'<g transform="translate(280 {256*0.09:.1f}) scale(0.82)">{g(wbody, color=C, square=True)}</g></svg>')
