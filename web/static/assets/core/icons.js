// Lucide icons, ISC license. Copyright (c) Lucide Contributors 2022.
// Permission to use, copy, modify, and/or distribute this software for any purpose
// with or without fee is hereby granted, provided that the above copyright notice
// and this permission notice appear in all copies.
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
// REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
// AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
// INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
// LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
// OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
// PERFORMANCE OF THIS SOFTWARE.
const paths = {
  book: ['M12 7v14', 'M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z'],
  mail: ['M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z', 'm22 6-10 7L2 6'],
  globe: ['M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z', 'M3 12h18', 'M12 3a16 16 0 0 1 0 18 16 16 0 0 1 0-18Z'],
  users: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z', 'M22 21v-2a4 4 0 0 0-3-3.87', 'M16 3.13a4 4 0 0 1 0 7.75'],
  building: ['M6 22V3a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v19', 'M6 12H4a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-9a2 2 0 0 0-2-2h-2', 'M10 6h4m-4 4h4m-4 4h4m-4 8v-4h4v4'],
  shield: ['M12 22s8-4 8-11V5l-8-3-8 3v6c0 7 8 11 8 11Z'],
  list: ['M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01'],
  user: ['M20 21v-2a7 7 0 0 0-14 0v2', 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z'],
  'log-out': ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'm16 17 5-5-5-5M21 12H9'],
  plus: ['M12 5v14M5 12h14'], trash: ['M3 6h18M19 6v14H5V6M9 6V3h6v3M10 10v7M14 10v7'],
  edit: ['m16 3 5 5-12 12-6 1 1-6Z', 'm14 5 5 5'],
  copy: ['M9 9h13v13H9Z', 'M5 15H2V2h13v3'], check: ['m20 6-11 11-5-5'], x: ['M18 6 6 18M6 6l12 12'],
  key: ['M16 8a5 5 0 1 1-3 9L3 21v-4l5-5a5 5 0 0 1 8-4Z', 'M16 8h.01M5 17l2 2'],
  refresh: ['M3 11a9 9 0 0 1 15.4-6.4L21 7M21 3v4h-4', 'M21 13a9 9 0 0 1-15.4 6.4L3 17M7 17H3v4'],
  menu: ['M4 6h16M4 12h16M4 18h16'],
  languages: ['m5 8 6 6m-7 0 6-6 2-5M2 5h12M7 2v3', 'm14 22 5-11 5 11m-8-4h6'],
  'chevron-right': ['m9 18 6-6-6-6'],
  alert: ['m10.3 3.9-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.7-3.1l-8-14a2 2 0 0 0-3.4 0Z', 'M12 9v4m0 4h.01'],
  inbox: ['M22 12h-6l-2 3h-4l-2-3H2', 'M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11Z'],
  link: ['M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71', 'M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71'],
  download: ['M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4', 'm7 10 5 5 5-5M12 15V3'],
};
const ns = 'http://www.w3.org/2000/svg';
export function icon(name, { label, size = 18 } = {}) {
  const svg = document.createElementNS(ns, 'svg');
  const attrs = { viewBox: '0 0 24 24', width: size, height: size, fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'stroke-linecap': 'round', 'stroke-linejoin': 'round', focusable: 'false' };
  if (label) { attrs.role = 'img'; attrs['aria-label'] = label; } else attrs['aria-hidden'] = 'true';
  for (const [key, value] of Object.entries(attrs)) svg.setAttribute(key, value);
  for (const d of paths[name] || paths.alert) {
    const path = document.createElementNS(ns, 'path');
    path.setAttribute('d', d); svg.append(path);
  }
  return svg;
}
