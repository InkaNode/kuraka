# Round 5: A/B/D with integrated detail. Each layer knocks a GAP-wide outline out of the layers below it.
from gen import word, g
C, GAP = "#A5431E", 6
def hexband(y, w, h):
    return f"M{128-w/2} {y+h/2}L{128-w/2+h/2} {y}H{128+w/2-h/2}L{128+w/2} {y+h/2}L{128+w/2-h/2} {y+h}H{128-w/2+h/2}Z"
def diamond(cx, cy, r):
    return f"M{cx} {cy-r}L{cx+r} {cy}L{cx} {cy+r}L{cx-r} {cy}Z"
def crescent(y, w, t):
    l, r = 128-w/2, 128+w/2
    return (f"M{l} {y}C{l+8} {y+w*.28} {128-w*.2} {y+w*.36} 128 {y+w*.36}C{128+w*.2} {y+w*.36} {r-8} {y+w*.28} {r} {y}"
            f"C{r-12} {y+w*.36-t-6} {128+w*.18} {y+w*.36-t} 128 {y+w*.36-t}C{128-w*.18} {y+w*.36-t} {l+12} {y+w*.36-t-6} {l} {y}Z")
def ring(cx, cy, R, r):
    return f"M{cx-R} {cy}A{R} {R} 0 1 0 {cx+R} {cy}A{R} {R} 0 1 0 {cx-R} {cy}ZM{cx-r} {cy}A{r} {r} 0 1 1 {cx+r} {cy}A{r} {r} 0 1 1 {cx-r} {cy}Z"
blade = "M50 30C60 78 90 100 128 100C166 100 196 78 206 30C184 58 158 68 128 68C98 68 72 58 50 30Z"
unified = ("M54 36C64 80 90 100 114 104C118 106 116 112 116 120L120 228C120 240 136 240 136 228"
           "L140 120C140 112 138 106 142 104C166 100 192 80 202 36C182 62 156 72 128 72C100 72 74 62 54 36Z")
ferrule = "M118 226H138L130 243Q128 247 126 243Z"
# each design: list of layers; a layer is (paths, extra cuts)
S = {
 "a2": [([blade], [diamond(80, 64, 6), diamond(176, 64, 6)]),
        (["M128 82L140 96L138 222H118L116 96Z"], []),
        ([hexband(128, 64, 18), hexband(172, 50, 14), ferrule], [])],
 "b2": [(["M112 34V26H120V18H136V26H144V34Z",
          "M88 42H168C180 42 182 54 174 64L156 86C152 90 148 92 142 92H114C108 92 104 90 100 86L82 64C74 54 76 42 88 42Z"],
         [diamond(128, 60, 10), diamond(100, 54, 5), diamond(156, 54, 5)]),
        (["M116 78H140L138 222H118Z"], []),
        ([hexband(116, 64, 18), hexband(160, 50, 14), ferrule], [])],
 "d2": [([unified], []),
        ([ring(128, 42, 24, 18), crescent(132, 64, 10), crescent(174, 48, 8)], []),
        ([f"M115 42A13 13 0 1 0 141 42A13 13 0 1 0 115 42Z"], [])],
}
def build(k, layers):
    defs, body = [], []
    for i, (paths, cuts) in enumerate(layers):
        above = [p for ps, _ in layers[i+1:] for p in ps]
        m = "".join(f'<path d="{p}" fill="#000" stroke="#000" stroke-width="{2*GAP}" stroke-linejoin="round"/>' for p in above)
        m += "".join(f'<path d="{p}" fill="#000"/>' for p in cuts)
        defs.append(f'<mask id="{k}{i}" maskUnits="userSpaceOnUse" x="0" y="0" width="256" height="256"><rect width="256" height="256" fill="#fff"/>{m}</mask>')
        body.append(f'<g mask="url(#{k}{i})" fill="{C}">' + "".join(f'<path fill-rule="evenodd" d="{p}"/>' for p in paths) + "</g>")
    return f'<defs>{"".join(defs)}</defs>{"".join(body)}'
wbody, ww = word(color=C)
for k, layers in S.items():
    body = build(k, layers)
    open(f"{k}-symbol.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">{body}</svg>')
    W = 256 + 24 + ww * 0.82
    open(f"{k}-lockup.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W:.0f} 256">{body.replace(f"#{k}", f"#L{k}").replace(f"id=\"{k}", f"id=\"L{k}")}'
        f'<g transform="translate(280 {256*0.09:.1f}) scale(0.82)">{g(wbody, color=C, square=True)}</g></svg>')
