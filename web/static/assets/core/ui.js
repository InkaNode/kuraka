import { h } from './h.js';
import { t } from './i18n.js';
import { icon as svgIcon } from './icons.js';
import { errorMessage } from './api.js';

let nextId = 0;
const uid = () => `kuraka-${++nextId}`;
export function button({ label, icon, variant = 'secondary', onClick, type = 'button', disabled = false, ariaLabel }) {
  return h('button', { class: ['btn', `btn-${variant}`], type, disabled, onclick: onClick, 'aria-label': ariaLabel },
    icon ? (typeof icon === 'string' ? svgIcon(icon) : icon) : null, label ? h('span', {}, label) : null);
}
export function field({ label, input, help, error, id }) {
  input.id = id || input.id || uid();
  const describedBy = (input.getAttribute('aria-describedby') || '').split(' ').filter(Boolean);
  const helpId = `${input.id}-help`;
  const errorId = `${input.id}-error`;
  if (help) describedBy.push(helpId);
  if (error) { describedBy.push(errorId); input.setAttribute('aria-invalid', 'true'); }
  if (describedBy.length) input.setAttribute('aria-describedby', [...new Set(describedBy)].join(' '));
  return h('div', { class: 'field' }, h('label', { class: 'label', for: input.id }, label), input,
    help ? h('p', { class: 'help', id: helpId }, help) : null,
    error ? h('p', { class: 'error', id: errorId }, error) : null);
}
export function textInput(attrs = {}) { return h('input', { type: 'text', ...attrs }); }
export function select({ options, value, ...attrs }) {
  const el = h('select', attrs, options.map(option => h('option', { value: option.value }, option.label)));
  if (value != null) el.value = value;
  return el;
}
export function checkbox({ label, checked = false, onChange }) {
  const id = uid();
  return h('label', { class: 'checkbox row gap-1', for: id },
    h('input', { id, type: 'checkbox', checked, onchange: onChange }), h('span', {}, label));
}
export function card(...children) { return h('section', { class: 'card' }, children); }
export function pageHeader(title, ...actions) {
  return h('header', { class: 'page-header' }, h('h1', {}, title), h('div', { class: 'row gap-1' }, actions));
}
export function table({ columns, rows, empty }) {
  return h('div', { class: 'table-wrap', tabindex: 0, role: 'region', 'aria-label': columns.map(column => column.label).join(', ') },
    h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, columns.map(column => h('th', { scope: 'col', class: column.className }, column.label)))),
      h('tbody', {}, rows.length
        ? rows.map(row => h('tr', {}, columns.map(column => h('td', { class: column.className }, column.render(row)))))
        : h('tr', {}, h('td', { colspan: Math.max(columns.length, 1) }, empty || emptyState({ title: t('empty.title'), text: t('empty.text') }))))));
}
export function badge(text, kind = 'neutral') { return h('span', { class: ['badge', kind] }, text); }
export function chip(text, { onRemove } = {}) {
  return h('span', { class: 'chip' }, h('span', {}, text), onRemove ? button({ icon: 'x', variant: 'ghost', ariaLabel: t('remove', { name: text }), onClick: onRemove }) : null);
}
export function copyButton(text) {
  const label = h('span', { 'aria-live': 'polite', 'aria-atomic': 'true' }, t('copy'));
  const el = button({ icon: 'copy', ariaLabel: t('copy'), onClick: async () => {
    if (el.disabled) return;
    el.disabled = true;
    try {
      await navigator.clipboard.writeText(String(text));
      label.textContent = t('copied');
      el.setAttribute('aria-label', t('copied'));
      setTimeout(() => { label.textContent = t('copy'); el.setAttribute('aria-label', t('copy')); el.disabled = false; }, 1500);
    } catch { el.disabled = false; toast(t('error.clipboard'), 'danger'); }
  } });
  el.append(label);
  return el;
}
export function spinner() {
  return h('div', { class: 'loading', role: 'status' }, h('span', { class: 'spinner', 'aria-hidden': 'true' }), h('span', { class: 'sr-only' }, t('loading')));
}
export function errorBox(err) {
  return h('div', { class: 'error error-box row gap-1', role: 'alert', tabindex: -1 }, svgIcon('alert'), h('span', {}, errorMessage(err)));
}
export function emptyState({ icon = 'inbox', title, text, action } = {}) {
  return h('section', { class: 'empty' }, svgIcon(icon, { size: 32 }), h('h2', {}, title || t('empty.title')),
    text ? h('p', { class: 'muted' }, text) : null, action);
}
export function tabs({ items, active, onChange }) {
  const buttons = items.map(item => h('button', {
    class: 'tab', type: 'button', role: 'tab', 'aria-selected': item.id === active,
    tabindex: item.id === active ? 0 : -1,
    onclick: () => activate(item.id),
    onkeydown: event => {
      const index = items.findIndex(entry => entry.id === item.id);
      let next;
      if (event.key === 'ArrowRight') next = (index + 1) % items.length;
      if (event.key === 'ArrowLeft') next = (index - 1 + items.length) % items.length;
      if (event.key === 'Home') next = 0;
      if (event.key === 'End') next = items.length - 1;
      if (next !== undefined) { event.preventDefault(); buttons[next].focus(); activate(items[next].id); }
    },
  }, item.label));
  function activate(id) {
    buttons.forEach((el, index) => { el.setAttribute('aria-selected', String(items[index].id === id)); el.tabIndex = items[index].id === id ? 0 : -1; });
    onChange?.(id);
  }
  if (buttons.length && !items.some(item => item.id === active)) buttons[0].tabIndex = 0;
  return h('div', { class: 'tabs', role: 'tablist', 'aria-label': t('tabs.label') }, buttons);
}

const dialogs = [];
export function modal({ title, body, actions = [], onClose }) {
  const opener = document.activeElement;
  const titleId = uid();
  let closed = false;
  const el = h('div', { class: 'modal-backdrop', onclick: event => { if (event.target === el) close(); } });
  const dialog = h('section', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': titleId, tabindex: -1 },
    h('header', { class: 'row gap-2' }, h('h2', { id: titleId, class: 'grow' }, title), button({ icon: 'x', variant: 'ghost', ariaLabel: t('close'), onClick: () => close() })),
    h('div', { class: 'modal-body stack gap-2' }, body), h('footer', { class: 'row gap-1 modal-actions' }, actions));
  el.append(dialog);
  const background = Array.from(document.body.children).map(node => [node, node.inert]);
  background.forEach(([node]) => { node.inert = true; });
  document.body.append(el);
  dialogs.push(el);
  document.body.classList.add('modal-open');
  const focusables = () => [...dialog.querySelectorAll('button, a[href], input, select, textarea, [tabindex]')]
    .filter(node => !node.disabled && node.tabIndex >= 0 && !node.closest('[hidden], .hidden, [inert]') && node.getClientRects().length);
  function onKey(event) {
    if (dialogs.at(-1) !== el) return;
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
    if (event.key === 'Tab') {
      const nodes = focusables();
      const first = nodes[0] || dialog;
      const last = nodes.at(-1) || dialog;
      if (!nodes.length || (event.shiftKey && (document.activeElement === first || document.activeElement === dialog))) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog)) { event.preventDefault(); first.focus(); }
    }
  }
  function onFocus(event) { if (dialogs.at(-1) === el && !dialog.contains(event.target)) dialog.focus(); }
  function close() {
    if (closed) return;
    // Close descendants first so nested dialogs restore inert state in order.
    while (dialogs.at(-1) !== el) dialogs.at(-1)?.dispatchEvent(new Event('modal:close'));
    closed = true;
    dialogs.pop(); el.remove();
    background.forEach(([node, inert]) => { node.inert = inert; });
    document.removeEventListener('keydown', onKey, true);
    document.removeEventListener('focusin', onFocus);
    window.removeEventListener('hashchange', close);
    window.removeEventListener('langchange', close);
    if (!dialogs.length) document.body.classList.remove('modal-open');
    if (opener?.isConnected) opener.focus();
    onClose?.();
  }
  el.addEventListener('modal:close', close);
  document.addEventListener('keydown', onKey, true);
  document.addEventListener('focusin', onFocus);
  window.addEventListener('hashchange', close);
  window.addEventListener('langchange', close);
  queueMicrotask(() => { if (!closed) dialog.focus(); });
  return { close, el };
}
export function confirmDialog({ title, message, confirmLabel, danger = false }) {
  return new Promise(resolve => {
    let confirmed = false;
    const dialog = modal({ title, body: h('p', {}, message), onClose: () => resolve(confirmed), actions: [
      button({ label: t('cancel'), onClick: () => dialog.close() }),
      button({ label: confirmLabel || t('confirm'), variant: danger ? 'danger' : 'primary', onClick: () => { confirmed = true; dialog.close(); } }),
    ] });
  });
}
export function secretOnce({ title, label, secret }) {
  const dialog = modal({ title, body: [
    h('p', { class: 'secret-warning row gap-1' }, svgIcon('alert'), t('secret.once')),
    field({ label, input: textInput({ value: secret, readonly: true, class: 'mono', autocomplete: 'off', spellcheck: 'false' }) }), copyButton(secret),
  ], actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
  return dialog;
}
let toastStack;
export function toast(message, kind = 'ok') {
  if (!toastStack?.isConnected) { toastStack = h('div', { class: 'toast-stack' }); document.body.append(toastStack); }
  const el = h('div', { class: ['toast', kind], role: 'status', 'aria-live': 'polite' }, message);
  toastStack.append(el);
  setTimeout(() => { el.remove(); if (!toastStack.childElementCount) toastStack.remove(); }, 4000);
  return el;
}
export function formError(form, err) {
  form.querySelector('[data-form-error]')?.remove();
  if (!err) return;
  const box = errorBox(err);
  box.setAttribute('data-form-error', '');
  form.prepend(box); box.focus();
  return box;
}
