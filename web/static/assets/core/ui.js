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
  const titleId = uid();
  // A form's trailing submit button joins the footer, after Cancel, as in the dialogs that pass it there.
  const submit = body instanceof HTMLFormElement && body.lastElementChild?.type === 'submit' ? body.lastElementChild : null;
  if (submit) { body.id ||= uid(); submit.setAttribute('form', body.id); actions = [...actions, submit]; }
  const el = h('dialog', { class: 'modal', 'aria-labelledby': titleId, onclick: event => {
    // A click on ::backdrop targets the dialog itself, outside its box.
    const r = el.getBoundingClientRect();
    if (event.target === el && (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom)) close();
  } },
    h('header', { class: 'row gap-2' }, h('h2', { id: titleId, class: 'grow' }, title), button({ icon: 'x', variant: 'ghost', ariaLabel: t('close'), onClick: () => close() })),
    h('div', { class: 'modal-body stack gap-2' }, body), h('footer', { class: 'row gap-1 modal-actions' }, actions));
  const self = { close, el };
  let closed = false;
  // The dialog's close event is async, so the stack unwinds here, not in it.
  function close() {
    if (closed) return;
    while (dialogs.at(-1) !== self) dialogs.at(-1).close();
    closed = true;
    dialogs.pop();
    window.removeEventListener('hashchange', close);
    window.removeEventListener('langchange', close);
    if (el.open) el.close();
    el.remove();
    onClose?.();
  }
  // Escape closes the dialog natively; the browser restores focus to the opener.
  el.addEventListener('close', close);
  window.addEventListener('hashchange', close);
  window.addEventListener('langchange', close);
  document.body.append(el);
  dialogs.push(self);
  el.showModal();
  return self;
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
export function submitForm(form, fn, { lock = [], validate, onError } = {}) {
  let busy = false;
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || validate?.() === false) return;
    // Read at submit time: a button tied by form= joins form.elements only once both are in the document.
    const locked = [[...form.elements].find(el => el.type === 'submit'), ...lock].filter(Boolean);
    busy = true; locked.forEach(el => { el.disabled = true; }); form.setAttribute('aria-busy', 'true'); formError(form, null);
    try { await fn(); }
    catch (err) { if (onError) onError(err); else formError(form, err); }
    finally { busy = false; locked.forEach(el => { el.disabled = false; }); form.removeAttribute('aria-busy'); }
  });
}
