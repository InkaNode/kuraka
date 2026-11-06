# Generates exploration SVGs (strokes, black on white) for three Kuraka concepts.
SW = 28
def word(x0=0, y0=0, color="#111"):
    L = {
        "k": (68, "M0 54V186M66 110L6 162M28 144L68 186"),
        "u": (72, "M0 110V150A36 36 0 0 0 72 150V110M72 110V186"),
        "r": (52, "M0 110V186M0 156C0 126 22 110 52 110"),
        "a": (74, "M74 110V186M74 148A37 38 0 1 0 74 148.1"),
    }
    out, x = [], x0
    for ch in "kuraka":
        w, d = L[ch]
        out.append(f'<path transform="translate({x+SW/2:.1f} {y0})" d="{d}"/>')
        x += w + 30
    return "".join(out), x - 30 + SW
def g(body, color="#111", square=False):
    if square:
        return (f'<clipPath id="c"><rect x="-10" y="40" width="2000" height="160"/></clipPath>'
                f'<g clip-path="url(#c)" fill="none" stroke="{color}" stroke-width="{SW}" stroke-linecap="square" stroke-linejoin="miter">{body}</g>')
    return f'<g fill="none" stroke="{color}" stroke-width="{SW}" stroke-linecap="round" stroke-linejoin="round">{body}</g>'

sym = {}
# A Escalon: tile + k whose arm climbs in Andean steps (staff of authority).
u = 32
cells = "".join(f'<rect x="{104+u*i}" y="{120-u*i}" width="{u}" height="{2*u}"/>' for i in range(3))
legs = "".join(f'<rect x="{104+u*i}" y="{152+u*i}" width="{u}" height="{u}"/>' for i in range(2))
K = ["X....XX","X...XX.","X..XX..","XXXX...","X..XX..","X...XX.","X....XX"]
u, o = 24, 44
px = "".join(f'<rect x="{o+c*u}" y="{o+r*u}" width="{u}" height="{u}"/>' for r, row in enumerate(K) for c, ch in enumerate(row) if ch == "X")
sym["a"] = f'<rect x="16" y="16" width="224" height="224" rx="40" fill="#111"/><g fill="#fff" shape-rendering="crispEdges">{px}</g>' 
# B Chakana: stepped Andean cross, centre opened as the hub.
s = 20
pts = [(-2,-6),(2,-6),(2,-4),(4,-4),(4,-2),(6,-2),(6,2),(4,2),(4,4),(2,4),(2,6),(-2,6),(-2,4),(-4,4),(-4,2),(-6,2),(-6,-2),(-4,-2),(-4,-4),(-2,-4)]
outer = "M" + "L".join(f"{128+x*s} {128+y*s}" for x, y in pts) + "Z"
hole = "M108 108H148V148H108Z"
sym["b"] = f'<path fill="#111" fill-rule="evenodd" d="{outer}{hole}"/>'
# C Sobre: envelope whose flap folds into a K.
sym["c"] = ('<rect x="16" y="40" width="224" height="176" rx="28" fill="#111"/>'
            '<g fill="none" stroke="#fff" stroke-width="30" stroke-linecap="round" stroke-linejoin="round">'
            '<path d="M76 84V172"/><path d="M196 74L104 128L196 182"/></g>')

wbody, ww = word()
for k, body in sym.items():
    open(f"{k}-symbol.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">{body}</svg>')
    W = 256 + 40 + ww * 0.82
    open(f"{k}-lockup.svg", "w").write(
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W:.0f} 256">{body}'
        f'<g transform="translate(296 {256*0.09:.1f}) scale(0.82)">{g(wbody, square=k != "c")}</g></svg>')
open("word.svg","w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {ww:.0f} 256">{g(wbody)}</svg>')
