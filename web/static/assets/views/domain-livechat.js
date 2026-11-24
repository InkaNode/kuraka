import { h, mount } from '../core/h.js';
import { t, getLang } from '../core/i18n.js';
import { api } from '../core/api.js';
import { button, field, textInput, select, table, errorBox, modal, confirmDialog, toast, formError, submitForm } from '../core/ui.js';
import { helpdeskError } from './domain-helpdesk.js';

const DEFAULT_COLOR = '#A5431E';
const HEX = /^#[0-9a-f]{6}$/i;
let seq = 0;
const uid = prefix => `livechat-${prefix}-${++seq}`;
const chatPath = (view, id) => `${view.base}/livechats${id == null ? '' : `/${encodeURIComponent(id)}`}`;

function chatError(err) {
  if (err?.code === 'livechat_missing') return { message: t('livechat.error.missing') };
  return helpdeskError(err);
}

async function copy(text, message) {
  try { await navigator.clipboard.writeText(text); toast(message); }
  catch { toast(t('error.clipboard'), 'danger'); }
}

function snippet(widget) {
  const base = widget.base_url.replace(/\/+$/, '');
  return `<script>window.LibredeskSettings={baseURL:${JSON.stringify(base)},inboxID:${JSON.stringify(widget.inbox_uuid)}};</script>\n` +
    `<script async src=${JSON.stringify(`${base}/widget.js`)}></script>`;
}

// publicURL: browsers block public sites from loading http, localhost or LAN addresses.
function publicURL(raw) {
  try {
    const u = new URL(raw);
    return u.protocol === 'https:' && u.hostname.includes('.') && u.hostname !== '127.0.0.1';
  } catch { return false; }
}

function codeDialog(chat) {
  if (!publicURL(chat.widget.base_url)) {
    const dialog = modal({ title: t('livechat.code.title', { name: chat.name }), body: errorBox({ message: t('livechat.code.private', { url: chat.widget.base_url }) }),
      actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return dialog;
  }
  const code = snippet(chat.widget);
  const side = t(chat.settings.position === 'left' ? 'livechat.side.left' : 'livechat.side.right');
  const prompt = t('livechat.ai.prompt', { code, side, color: chat.settings.color, origin: chat.widget.base_url.replace(/\/+$/, '') });
  const dialog = modal({ title: t('livechat.code.title', { name: chat.name }), body: [
    h('p', {}, t('livechat.code.intro', { side })),
    field({ label: t('livechat.code.label'), input: h('textarea', { class: 'mono', readonly: true, rows: 4, spellcheck: 'false' }, code) }),
    h('p', { class: 'help' }, t('livechat.ai.help')),
  ], actions: [
    button({ label: t('livechat.ai'), onClick: () => copy(prompt, t('livechat.ai.copied')) }),
    button({ label: t('livechat.code.copy'), icon: 'copy', variant: 'primary', onClick: () => copy(code, t('livechat.code.copied')) }),
  ] });
  return dialog;
}

const parseDomains = text => text.split(/[\s,]+/).filter(Boolean);

async function chatDialog(view, existing, notice, onSaved) {
  mount(notice);
  let chat = null;
  let helpdesks = [];
  try {
    if (existing) chat = await api('GET', chatPath(view, existing.id));
    else helpdesks = (await api('GET', '/helpdesks')).filter(x => x.tenant_id === view.domain.tenant_id && x.status === 'ready').sort((a, b) => a.name.localeCompare(b.name));
  } catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); return; }
  if (!existing && !helpdesks.length) {
    const dialog = modal({ title: t('livechat.new'), body: h('p', {}, t('livechat.noHelpdesk')), actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }

  const s = chat?.settings || { name: view.domain.name, website_url: view.domain.name, color: DEFAULT_COLOR, position: 'right', logo_url: '',
    greeting: t('livechat.greeting.default'), allowed_domains: [`*.${view.domain.name.toLowerCase().replace(/^www\./, '')}`] };
  const helpdesk = helpdesks.length > 1 ? select({ required: true, options: helpdesks.map(x => ({ value: String(x.id), label: x.name })) }) : null;
  const language = existing ? null : select({ value: getLang() === 'es' ? 'es' : 'en', options: [{ value: 'es', label: 'Español' }, { value: 'en', label: 'English' }] });
  const name = textInput({ required: true, maxlength: 128, autocomplete: 'off', value: s.name });
  const website = textInput({ class: 'mono', required: true, inputmode: 'url', autocomplete: 'off', spellcheck: 'false', value: s.website_url });
  const color = h('input', { type: 'color', value: HEX.test(s.color) ? s.color : DEFAULT_COLOR });
  const logo = textInput({ class: 'mono', inputmode: 'url', autocomplete: 'off', spellcheck: 'false', value: s.logo_url || '' });
  const greeting = h('textarea', { rows: 3, maxlength: 500 }, s.greeting || '');
  const domains = h('textarea', { class: 'mono', rows: 3, spellcheck: 'false', autocomplete: 'off' }, (s.allowed_domains || []).join('\n'));
  const inputs = { name, website_url: website, color, logo_url: logo, greeting, allowed_domains: domains };

  let position = s.position === 'left' ? 'left' : 'right';
  const positionName = uid('position');
  const positionLabel = uid('position-label');
  const positionGroup = h('div', { class: 'stack gap-1', role: 'radiogroup', 'aria-labelledby': positionLabel },
    h('span', { class: 'label', id: positionLabel }, t('livechat.position')),
    h('div', { class: 'row gap-2' }, ['left', 'right'].map(side => {
      const id = uid('radio');
      return h('label', { class: 'checkbox row gap-1', for: id }, h('input', { id, type: 'radio', name: positionName, value: side, checked: side === position,
        onchange: () => { position = side; } }), h('span', {}, t(`livechat.position.${side}`)));
    })));

  const anySite = h('p', { class: 'warn', 'aria-live': 'polite' });
  const syncAnySite = () => { anySite.textContent = parseDomains(domains.value).length ? '' : t('livechat.domains.any'); };
  domains.addEventListener('input', syncAnySite);
  syncAnySite();
  const domainsField = field({ label: t('livechat.domains'), input: domains, help: t('livechat.domains.help') });
  domainsField.append(anySite);

  const form = h('form', { class: 'stack gap-2' },
    helpdesk ? field({ label: t('helpdesk.select'), input: helpdesk }) : null,
    field({ label: t('livechat.name'), input: name, help: t('livechat.name.help') }),
    field({ label: t('livechat.website'), input: website, help: t('livechat.website.help') }),
    language ? field({ label: t('livechat.language'), input: language, help: t('livechat.language.help') }) : null,
    field({ label: t('livechat.color'), input: color }),
    positionGroup,
    field({ label: t('livechat.logo'), input: logo, help: t(existing ? 'livechat.logo.help.edit' : 'livechat.logo.help') }),
    field({ label: t('livechat.greeting'), input: greeting, help: t('livechat.greeting.help') }),
    domainsField,
    button({ label: t(existing ? 'save' : 'create'), variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    for (const el of form.querySelectorAll('[aria-invalid]')) el.removeAttribute('aria-invalid');
    const values = { name: name.value.trim(), website_url: website.value.trim(), color: color.value, position,
      logo_url: logo.value.trim(), greeting: greeting.value.trim(), allowed_domains: parseDomains(domains.value) };
    const result = existing
      ? await api('PUT', chatPath(view, existing.id), values)
      : await api('POST', chatPath(view), { ...values, helpdesk_id: Number(helpdesk?.value ?? helpdesks[0].id), language: language.value });
    dialog.close();
    toast(t(existing ? 'livechat.updated' : 'livechat.created', { name: result.name }));
    onSaved();
    if (!existing) codeDialog(result);
  }, { onError: err => {
    const key = err?.code === 'invalid_input' ? /^Invalid (\w+)$/.exec(err.message || '')?.[1] : null;
    if (!inputs[key]) { formError(form, chatError(err)); return; }
    formError(form, { message: t(`livechat.invalid.${key}`) });
    inputs[key].setAttribute('aria-invalid', 'true');
    inputs[key].focus();
  } });
  const dialog = modal({ title: existing ? t('livechat.edit.title', { name: existing.name }) : t('livechat.new'), body: form,
    actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
}

// liveChats returns the "add" button for the mailboxes toolbar and the list,
// which stays empty until the domain has a live chat.
export function liveChats(view, mine, notice) {
  const node = h('div', {});
  const reload = () => view.show();
  const add = () => button({ label: t('livechat.new'), icon: 'message', onClick: () => chatDialog(view, null, notice, reload) });

  async function showCode(chat) {
    mount(notice);
    try { codeDialog(await api('GET', chatPath(view, chat.id))); }
    catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); }
  }
  async function remove(chat) {
    const ok = await confirmDialog({ title: t('livechat.delete.title', { name: chat.name }), message: t('livechat.delete.message', { website: chat.website_url }),
      confirmLabel: t('delete'), danger: true });
    if (!ok) return;
    mount(notice);
    try {
      await api('DELETE', chatPath(view, chat.id));
      toast(t('livechat.deleted', { name: chat.name }));
      reload();
    } catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); }
  }

  api('GET', chatPath(view)).then(chats => {
    if (mine !== view.current() || !chats.length) return;
    chats.sort((a, b) => a.name.localeCompare(b.name));
    mount(node, h('section', { class: 'stack gap-1' }, h('h2', {}, t('livechat.list')), table({ rows: chats, columns: [
      { label: t('livechat.name'), render: c => c.name },
      { label: t('livechat.website'), render: c => h('span', { class: 'mono' }, c.website_url) },
      { label: t('mailbox.col.actions'), render: c => h('div', { class: 'row gap-1' },
        button({ label: t('livechat.code'), icon: 'code', variant: 'ghost', ariaLabel: t('livechat.code.title', { name: c.name }), onClick: () => showCode(c) }),
        button({ icon: 'edit', variant: 'ghost', ariaLabel: t('livechat.edit.title', { name: c.name }), onClick: () => chatDialog(view, c, notice, reload) }),
        button({ icon: 'trash', variant: 'ghost', ariaLabel: t('livechat.delete.title', { name: c.name }), onClick: () => remove(c) })) },
    ] })));
  }).catch(err => { if (mine === view.current()) mount(node, errorBox(err)); });

  return { add, node };
}
