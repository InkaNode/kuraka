export function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [name, value] of Object.entries(attrs)) {
    if (value == null) continue;
    if (name === 'class') el.className = Array.isArray(value) ? value.filter(Boolean).join(' ') : value;
    else if (/^on/i.test(name) && typeof value === 'function') el.addEventListener(name.slice(2).toLowerCase(), value);
    else if (name === 'value' || name === 'checked') el[name] = value;
    else if (typeof value === 'boolean' && !name.startsWith('aria-') && !name.startsWith('data-')) {
      if (value) el.setAttribute(name, '');
    } else el.setAttribute(name, String(value));
  }
  append(el, children);
  return el;
}
function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}
export function clear(el) { el.replaceChildren(); }
const motion = typeof matchMedia === 'function' ? matchMedia('(prefers-reduced-motion: no-preference)') : null;
// Opacity animation; skipped when the user prefers reduced motion.
export function fade(el, from, to, options) {
  return motion?.matches && el.animate ? el.animate([{ opacity: from }, { opacity: to }], { duration: 200, easing: 'ease-out', ...options }) : null;
}
// Every re-render fades in, so changed sections and values are noticeable.
export function mount(el, ...nodes) { clear(el); append(el, nodes); fade(el, 0.3, 1); }
