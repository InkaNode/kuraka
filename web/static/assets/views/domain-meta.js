import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { button, field, textInput, select, card, checkbox, copyButton, errorBox, spinner, modal, confirmDialog, toast, formError, submitForm } from '../core/ui.js';
import { navigate } from '../core/router.js';
import { helpdeskError } from './domain-helpdesk.js';
import { publicURL } from './domain-livechat.js';

const metaPath = (view, id) => `${view.base}/meta${id == null ? '' : `/${encodeURIComponent(id)}`}`;

const LABELS = { whatsapp: 'WhatsApp', messenger: 'Messenger', instagram: 'Instagram' };
const WEBHOOK_STEPS = {
  whatsapp: ['meta.webhook.step1', 'meta.webhook.step2', 'meta.webhook.step3', 'meta.webhook.step4'],
  messenger: ['meta.webhook.messenger.step1', 'meta.webhook.messenger.step2', 'meta.webhook.messenger.step3'],
  instagram: ['meta.webhook.instagram.step1', 'meta.webhook.instagram.step2', 'meta.webhook.instagram.step3'],
};

function metaError(err) {
  if (err?.code === 'meta_missing') return { message: t('meta.error.missing') };
  if (err?.code === 'meta_app_missing') return { message: t('meta.error.app') };
  if (err?.code === 'meta_expired') return { message: t('meta.error.expired') };
  if (err?.code === 'meta_rejected') return { message: t('meta.error.login') };
  if (err?.code === 'whatsapp_rejected') return { message: t('meta.error.rejected') };
  return helpdeskError(err);
}

function readonly(value) {
  return h('div', { class: 'row gap-1' }, textInput({ class: 'mono grow', readonly: true, value, spellcheck: 'false' }), copyButton(value));
}

// webhookDialog shows what Meta's webhook setup asks for on the channel.
function webhookDialog(ch) {
  const body = publicURL(ch.webhook.url)
    ? [
      h('ol', { class: 'stack gap-1' }, (WEBHOOK_STEPS[ch.channel] || WEBHOOK_STEPS.whatsapp).map(key => h('li', {}, t(key)))),
      field({ label: t('meta.webhook.url'), input: readonly(ch.webhook.url) }),
      field({ label: t('meta.webhook.token'), input: readonly(ch.webhook.verify_token) }),
    ]
    : errorBox({ message: t('meta.webhook.private', { url: ch.webhook.url }) });
  const dialog = modal({ title: t('meta.webhook.title', { name: ch.name }), body, actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
}

// channelDialog connects or edits a WhatsApp, Messenger or Instagram channel.
async function channelDialog(view, kind, existing, notice, onSaved) {
  mount(notice);
  let ch = null;
  let helpdesks = [];
  try {
    if (existing) ch = await api('GET', metaPath(view, existing.id));
    else helpdesks = (await api('GET', '/helpdesks')).filter(x => x.tenant_id === view.domain.tenant_id && x.status === 'ready').sort((a, b) => a.name.localeCompare(b.name));
  } catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); return; }
  if (!existing && !helpdesks.length) {
    const dialog = modal({ title: t(`meta.${kind}.connect`), body: h('p', {}, t('livechat.noHelpdesk')), actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }

  const whatsApp = kind === 'whatsapp';
  const s = ch?.settings || { name: `${LABELS[kind]} ${view.domain.name}`, phone_number_id: '', waba_id: '', page_id: '' };
  const helpdesk = helpdesks.length > 1 ? select({ required: true, options: helpdesks.map(x => ({ value: String(x.id), label: x.name })) }) : null;
  const idInput = value => textInput({ class: 'mono', required: true, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 32, autocomplete: 'off', spellcheck: 'false', value });
  const secretInput = () => h('input', { type: 'password', class: 'mono', required: !existing, maxlength: 2048, autocomplete: 'off', spellcheck: 'false' });
  const inputs = { name: textInput({ required: true, maxlength: 128, autocomplete: 'off', value: s.name }) };
  if (whatsApp) Object.assign(inputs, { phone_number_id: idInput(s.phone_number_id), waba_id: idInput(s.waba_id) });
  else inputs.page_id = idInput(s.page_id);
  Object.assign(inputs, { access_token: secretInput(), app_secret: secretInput() });
  const keep = existing ? ` ${t('meta.secret.keep')}` : '';
  const form = h('form', { class: 'stack gap-2' },
    h('p', {}, t(`meta.${kind}.intro`)),
    helpdesk ? field({ label: t('helpdesk.select'), input: helpdesk }) : null,
    field({ label: t('livechat.name'), input: inputs.name, help: t('meta.name.help') }),
    whatsApp ? field({ label: t('meta.phone'), input: inputs.phone_number_id, help: t('meta.phone.help') }) : null,
    whatsApp ? field({ label: t('meta.waba'), input: inputs.waba_id, help: t('meta.waba.help') }) : null,
    kind === 'messenger' ? field({ label: t('meta.page'), input: inputs.page_id, help: t('meta.page.help') }) : null,
    kind === 'instagram' ? field({ label: t('meta.igaccount'), input: inputs.page_id, help: t('meta.igaccount.help') }) : null,
    field({ label: t('meta.token'), input: inputs.access_token, help: t(whatsApp ? 'meta.token.help' : 'meta.page.token.help') + keep }),
    field({ label: t('meta.secret'), input: inputs.app_secret, help: t('meta.secret.help') + keep }),
    button({ label: t(existing ? 'save' : `meta.${kind}.connect`), variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    for (const el of form.querySelectorAll('[aria-invalid]')) el.removeAttribute('aria-invalid');
    const values = Object.fromEntries(Object.entries(inputs).map(([k, el]) => [k, el.value.trim()]));
    const result = existing
      ? await api('PUT', metaPath(view, existing.id), values)
      : await api('POST', metaPath(view), { ...values, channel: kind, helpdesk_id: Number(helpdesk?.value ?? helpdesks[0].id) });
    dialog.close();
    toast(t(existing ? 'meta.updated' : 'meta.created', { name: result.name }));
    onSaved();
    if (!existing) webhookDialog(result);
  }, { onError: err => {
    const key = err?.code === 'invalid_input' ? /^Invalid (\w+)$/.exec(err.message || '')?.[1] : null;
    if (!inputs[key]) { formError(form, metaError(err)); return; }
    formError(form, { message: t(`meta.invalid.${key}`) });
    inputs[key].setAttribute('aria-invalid', 'true');
    inputs[key].focus();
  } });
  const dialog = modal({ title: existing ? t('meta.edit.title', { name: existing.name }) : t(`meta.${kind}.connect`), body: form,
    actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
}

// pending is a finished Meta login waiting for the domain page to show what
// it granted.
let pending = null;

async function startLogin(view, kind, notice) {
  mount(notice);
  try { location.assign((await api('POST', `${view.base}/meta/login`, { kind })).url); }
  catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); }
}

// metaCallbackView is where Meta sends the login back. It trades the code
// for what was granted and returns to the domain to pick from it.
export async function metaCallbackView(ctx) {
  const state = ctx.query.get('state') || '';
  const id = /^\d+\./.test(state) ? state.split('.')[0] : null;
  const fail = message => h('div', { class: 'stack gap-2' }, errorBox({ message }),
    button({ label: t('meta.connect.back'), onClick: () => navigate(id ? `/domains/${id}` : '/domains') }));
  if (!id || !ctx.query.get('code')) return fail(t('meta.connect.cancelled'));
  try {
    const result = await api('POST', `/domains/${encodeURIComponent(id)}/meta/oauth`, { code: ctx.query.get('code'), state });
    pending = { domain: Number(id), ...result };
  } catch (err) { return fail(metaError(err).message); }
  // Replace keeps the spent code out of the history.
  location.replace(`#/domains/${id}`);
  return spinner();
}

const FAILED = new Set(['meta_exists', 'meta_rejected', 'meta_missing']);

// pickDialog lists what a Meta login granted; the checked ones become
// helpdesk inboxes.
async function pickDialog(view, grant, channels, notice, onSaved) {
  let helpdesks;
  try { helpdesks = (await api('GET', '/helpdesks')).filter(x => x.tenant_id === view.domain.tenant_id && x.status === 'ready').sort((a, b) => a.name.localeCompare(b.name)); }
  catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); return; }
  const items = [];
  for (const page of grant.pages) {
    items.push({ channel: 'messenger', id: page.id, label: page.name });
    if (page.instagram) items.push({ channel: 'instagram', id: page.instagram.id, label: `@${page.instagram.username}` });
  }
  for (const phone of grant.phones) items.push({ channel: 'whatsapp', id: phone.id, label: phone.name && phone.number ? `${phone.name} (${phone.number})` : phone.name || phone.number });
  const message = !helpdesks.length ? t('livechat.noHelpdesk') : !items.length ? t('meta.connect.none') : null;
  if (message) {
    const dialog = modal({ title: t('meta.connect.title'), body: h('p', {}, message), actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }

  const taken = new Set(channels.map(ch => `${ch.channel}:${ch.external_id}`));
  const boxes = items.map(item => {
    const done = taken.has(`${item.channel}:${item.id}`);
    const el = checkbox({ label: done ? `${item.label} (${t('meta.connect.done')})` : item.label, checked: !done });
    const input = el.querySelector('input');
    input.disabled = done;
    return { item, el, input };
  });
  const helpdesk = helpdesks.length > 1 ? select({ required: true, options: helpdesks.map(x => ({ value: String(x.id), label: x.name })) }) : null;
  const form = h('form', { class: 'stack gap-2' },
    helpdesk ? field({ label: t('helpdesk.select'), input: helpdesk }) : null,
    Object.keys(LABELS).map(kind => {
      const own = boxes.filter(b => b.item.channel === kind);
      return own.length ? h('div', { class: 'stack gap-1', role: 'group', 'aria-label': LABELS[kind] }, h('h3', {}, LABELS[kind]), own.map(b => b.el)) : null;
    }),
    button({ label: t('meta.connect.submit'), variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    const picked = boxes.filter(b => b.input.checked && !b.input.disabled).map(b => b.item);
    if (!picked.length) { formError(form, { message: t('meta.connect.pick') }); return; }
    const ids = kind => picked.filter(item => item.channel === kind).map(item => item.id);
    const result = await api('POST', `${view.base}/meta/import`, { grant: grant.grant, helpdesk_id: Number(helpdesk?.value ?? helpdesks[0].id),
      messenger: ids('messenger'), instagram: ids('instagram'), whatsapp: ids('whatsapp') });
    dialog.close();
    if (result.created.length) toast(t('meta.connect.created', { count: result.created.length }));
    onSaved();
    if (!result.failed.length) return;
    const lines = result.failed.map(f => h('li', {}, t(`meta.connect.failed.${FAILED.has(f.code) ? f.code : 'other'}`, { name: f.name || '' })));
    const report = modal({ title: t('meta.connect.failedTitle'), body: h('ul', { class: 'stack gap-1' }, lines),
      actions: [button({ label: t('close'), onClick: () => report.close() })] });
  }, { onError: err => formError(form, metaError(err)) });
  const dialog = modal({ title: t('meta.connect.title'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
}

// metaSection lists the domain's Meta channels, one card each for WhatsApp,
// Messenger and Instagram.
export function metaSection(view, mine, notice) {
  const lists = { whatsapp: h('div', {}), messenger: h('div', {}), instagram: h('div', {}) };
  const manual = { whatsapp: h('div', {}), messenger: h('div', {}), instagram: h('div', {}) };
  const connect = h('div', { class: 'stack gap-1' });
  const reload = () => view.show();
  const node = h('section', { class: 'stack gap-1' },
    h('h2', {}, t('meta.title')),
    h('p', { class: 'muted' }, t('meta.intro')),
    connect,
    h('div', { class: 'metric-grid' }, Object.keys(lists).map(kind =>
      card(h('h3', {}, LABELS[kind]), h('p', { class: 'muted' }, t(`meta.${kind}.text`)), lists[kind], manual[kind]))));

  // With the Meta app set up, the buttons connect through Meta and typing
  // the details by hand becomes the advanced way.
  function showButtons(status) {
    const auto = status.pages || status.whatsapp;
    for (const kind of Object.keys(manual)) {
      mount(manual[kind], button({ label: t(auto ? 'meta.manual' : `meta.${kind}.connect`), icon: auto ? 'edit' : 'plus', variant: auto ? 'ghost' : undefined,
        ariaLabel: auto ? `${t('meta.manual')}: ${LABELS[kind]}` : undefined, onClick: () => channelDialog(view, kind, null, notice, reload) }));
    }
    if (!auto) { mount(connect); return; }
    mount(connect, h('div', { class: 'row gap-1' },
      status.pages ? button({ label: t('meta.connect.pages'), icon: 'link', variant: 'primary', onClick: () => startLogin(view, 'pages', notice) }) : null,
      status.whatsapp ? button({ label: t('meta.connect.whatsapp'), icon: 'link', variant: 'primary', onClick: () => startLogin(view, 'whatsapp', notice) }) : null),
    h('p', { class: 'help' }, t('meta.connect.help')));
  }
  showButtons({});
  api('GET', '/meta-app/status').then(status => { if (mine === view.current()) showButtons(status); }).catch(() => {});

  async function showWebhook(ch) {
    mount(notice);
    try { webhookDialog(await api('GET', metaPath(view, ch.id))); }
    catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); }
  }
  async function remove(ch) {
    const ok = await confirmDialog({ title: t('meta.delete.title', { name: ch.name }), message: t('meta.delete.message'), confirmLabel: t('delete'), danger: true });
    if (!ok) return;
    mount(notice);
    try {
      await api('DELETE', metaPath(view, ch.id));
      toast(t('meta.deleted', { name: ch.name }));
      reload();
    } catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); }
  }

  api('GET', metaPath(view)).then(channels => {
    if (mine !== view.current()) return;
    if (pending?.domain === view.domain.id) { const p = pending; pending = null; pickDialog(view, p, channels, notice, reload); }
    channels.sort((a, b) => a.name.localeCompare(b.name));
    for (const [kind, list] of Object.entries(lists)) {
      const own = channels.filter(ch => ch.channel === kind);
      if (!own.length) continue;
      mount(list, h('ul', { class: 'stack gap-1' }, own.map(ch => h('li', { class: 'row gap-1' },
        h('span', { class: 'grow' }, ch.name),
        button({ label: t('meta.webhook'), icon: 'link', variant: 'ghost', ariaLabel: t('meta.webhook.title', { name: ch.name }), onClick: () => showWebhook(ch) }),
        button({ icon: 'edit', variant: 'ghost', ariaLabel: t('meta.edit.title', { name: ch.name }), onClick: () => channelDialog(view, kind, ch, notice, reload) }),
        button({ icon: 'trash', variant: 'ghost', ariaLabel: t('meta.delete.title', { name: ch.name }), onClick: () => remove(ch) })))));
    }
  }).catch(err => { if (mine === view.current()) mount(lists.whatsapp, errorBox(err)); });

  return { node };
}
