# Round 8: ownable detail for the double-jamb door, two-tone.
from gen import word, g
from gen7 import arch, poly
T, OC = "#A5431E", "#D9922E"
O = arch(28, 228, 72, .12, 28, 34)
I = arch(72, 228, 72+.12*44-38, .12, 20, 22)
step = poly([(80, 35), (80, 24), (104, 24), (104, 14), (152, 14), (152, 24), (176, 24), (176, 35)])
D = {
 "q1": [(T, arch(34, 228, 72.7, .12, 28, 28)), (T, step), (OC, I)],
 "q2": [(T, O + "M128 35L138 45L128 55L118 45Z"), (OC, I)],
 "q3": [(T, O), (OC, I), (OC, "M104 228A24 24 0 0 1 152 228Z")],
}
wbody, ww = word(color=T)
for k, parts in D.items():
    body = "".join(f'<path fill="{c}" fill-rule="evenodd" d="{d}"/>' for c, d in parts)
    open(f"{k}-symbol.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">{body}</svg>')
    W = 256 + 24 + ww * 0.82
    open(f"{k}-lockup.svg", "w").write(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W:.0f} 256">{body}'
        f'<g transform="translate(280 {256*0.09:.1f}) scale(0.82)">{g(wbody, color=T, square=True)}</g></svg>')
