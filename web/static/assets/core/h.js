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
export function mount(el, ...nodes) { clear(el); append(el, nodes); }
