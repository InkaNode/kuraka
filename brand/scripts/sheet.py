import re
def inner(n): s = open(f"kit/{n}.svg", encoding="utf-8").read(); return re.search(r'viewBox="([^"]+)"', s).group(1), re.sub(r"^<svg[^>]*>|</svg>$", "", s)
def put(n, x, y, w, h):
    vb, b = inner(n); return f'<svg x="{x}" y="{y}" width="{w}" height="{h}" viewBox="{vb}">{b}</svg>'
F = 'font-family="Segoe UI,Arial" fill="#555" font-size="18"'
def lab(x, y, t): return f'<text x="{x}" y="{y}" {F}>{t}</text>'
c = ['<rect width="1400" height="1180" fill="#f7f7f4"/>', '<text x="50" y="70" font-family="Segoe UI,Arial" font-size="36" font-weight="700" fill="#161616">Kuraka · kit de logo</text>',
     '<rect x="50" y="100" width="640" height="300" rx="16" fill="#fff"/>', put("kuraka-horizontal", 90, 170, 560, 188), lab(70, 130, "Horizontal"),
     '<rect x="710" y="100" width="640" height="300" rx="16" fill="#3B1F14"/>', put("kuraka-horizontal-reversed", 750, 170, 560, 188), f'<text x="730" y="130" font-family="Segoe UI,Arial" font-size="18" fill="#e9d8cc">Negativo</text>',
     '<rect x="50" y="420" width="420" height="380" rx="16" fill="#fff"/>', put("kuraka-stacked", 120, 470, 280, 300), lab(70, 450, "Apilado"),
     '<rect x="490" y="420" width="420" height="380" rx="16" fill="#fff"/>', put("kuraka-symbol-black", 520, 480, 170, 170), put("kuraka-symbol-mono-a5431e", 710, 480, 170, 170), lab(510, 450, "Un color"),
     '<rect x="930" y="420" width="420" height="380" rx="16" fill="#fff"/>', put("kuraka-app-icon", 960, 480, 160, 160), put("kuraka-app-icon", 1140, 480, 64, 64), put("kuraka-app-icon", 1220, 480, 32, 32), put("kuraka-app-icon", 1268, 480, 16, 16),
     put("kuraka-symbol-small", 1140, 580, 64, 64), put("kuraka-symbol-small", 1220, 596, 32, 32), put("kuraka-symbol-small", 1268, 604, 16, 16), lab(950, 450, "Ícono y favicon (versión chica)")]
for i, (hx, nm) in enumerate([("#A5431E", "Terracota"), ("#D9922E", "Ocre"), ("#3B1F14", "Café"), ("#FFF7F2", "Crema")]):
    x = 50 + i*330
    c += [f'<rect x="{x}" y="830" width="310" height="140" rx="16" fill="{hx}" stroke="#e4e4e0"/>', lab(x, 1000, f"{nm} {hx}")]
open("kitsheet.svg", "w", encoding="utf-8").write(f'<svg xmlns="http://www.w3.org/2000/svg" width="1400" height="1040" viewBox="0 0 1400 1040">{"".join(c)}</svg>')
