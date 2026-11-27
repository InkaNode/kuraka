import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { button, field, textInput, select, badge, card, copyButton, errorBox, modal, confirmDialog, toast, formError, submitForm } from '../core/ui.js';
import { helpdeskError } from './domain-helpdesk.js';
import { publicURL } from './domain-livechat.js';

const metaPath = (view, id) => `${view.base}/meta${id == null ? '' : `/${encodeURIComponent(id)}`}`;

function metaError(err) {
  if (err?.code === 'meta_missing') return { message: t('meta.error.missing') };
  if (err?.code === 'whatsapp_rejected') return { message: t('meta.error.rejected') };
  return helpdeskError(err);
}

function readonly(value) {
  return h('div', { class: 'row gap-1' }, textInput({ class: 'mono grow', readonly: true, value, spellcheck: 'false' }), copyButton(value));
}

// webhookDialog shows what Meta's WhatsApp webhook setup asks for.
function webhookDialog(ch) {
  const body = publicURL(ch.webhook.url)
    ? [
      h('ol', { class: 'stack gap-1' }, ['meta.webhook.step1', 'meta.webhook.step2', 'meta.webhook.step3', 'meta.webhook.step4'].map(key => h('li', {}, t(key)))),
      field({ label: t('meta.webhook.url'), input: readonly(ch.webhook.url) }),
      field({ label: t('meta.webhook.token'), input: readonly(ch.webhook.verify_token) }),
    ]
    : errorBox({ message: t('meta.webhook.private', { url: ch.webhook.url }) });
  const dialog = modal({ title: t('meta.webhook.title', { name: ch.name }), body, actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
}

async function whatsAppDialog(view, existing, notice, onSaved) {
  mount(notice);
  let ch = null;
  let helpdesks = [];
  try {
    if (existing) ch = await api('GET', metaPath(view, existing.id));
    else helpdesks = (await api('GET', '/helpdesks')).filter(x => x.tenant_id === view.domain.tenant_id && x.status === 'ready').sort((a, b) => a.name.localeCompare(b.name));
  } catch (err) { mount(notice, errorBox(metaError(err))); notice.firstChild?.focus(); return; }
  if (!existing && !helpdesks.length) {
    const dialog = modal({ title: t('meta.whatsapp.connect'), body: h('p', {}, t('livechat.noHelpdesk')), actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }

  const s = ch?.settings || { name: `WhatsApp ${view.domain.name}`, phone_number_id: '', waba_id: '' };
  const helpdesk = helpdesks.length > 1 ? select({ required: true, options: helpdesks.map(x => ({ value: String(x.id), label: x.name })) }) : null;
  const idInput = value => textInput({ class: 'mono', required: true, inputmode: 'numeric', pattern: '[0-9]+', maxlength: 32, autocomplete: 'off', spellcheck: 'false', value });
  const secretInput = () => h('input', { type: 'password', class: 'mono', required: !existing, maxlength: 2048, autocomplete: 'off', spellcheck: 'false' });
  const inputs = {
    name: textInput({ required: true, maxlength: 128, autocomplete: 'off', value: s.name }),
    phone_number_id: idInput(s.phone_number_id),
    waba_id: idInput(s.waba_id),
    access_token: secretInput(),
    app_secret: secretInput(),
  };
  const keep = existing ? ` ${t('meta.secret.keep')}` : '';
  const form = h('form', { class: 'stack gap-2' },
    h('p', {}, t('meta.whatsapp.intro')),
    helpdesk ? field({ label: t('helpdesk.select'), input: helpdesk }) : null,
    field({ label: t('livechat.name'), input: inputs.name, help: t('meta.name.help') }),
    field({ label: t('meta.phone'), input: inputs.phone_number_id, help: t('meta.phone.help') }),
    field({ label: t('meta.waba'), input: inputs.waba_id, help: t('meta.waba.help') }),
    field({ label: t('meta.token'), input: inputs.access_token, help: t('meta.token.help') + keep }),
    field({ label: t('meta.secret'), input: inputs.app_secret, help: t('meta.secret.help') + keep }),
    button({ label: t(existing ? 'save' : 'meta.whatsapp.connect'), variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    for (const el of form.querySelectorAll('[aria-invalid]')) el.removeAttribute('aria-invalid');
    const values = Object.fromEntries(Object.entries(inputs).map(([k, el]) => [k, el.value.trim()]));
    const result = existing
      ? await api('PUT', metaPath(view, existing.id), values)
      : await api('POST', metaPath(view), { ...values, channel: 'whatsapp', helpdesk_id: Number(helpdesk?.value ?? helpdesks[0].id) });
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
  const dialog = modal({ title: existing ? t('meta.edit.title', { name: existing.name }) : t('meta.whatsapp.connect'), body: form,
    actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
}

function soon(title, text) {
  return card(h('div', { class: 'row gap-2' }, h('h3', { class: 'grow' }, title), badge(t('meta.soon'))), h('p', { class: 'muted' }, text));
}

// metaSection lists the domain's Meta channels: WhatsApp, and Messenger and
// Instagram marked as coming soon.
export function metaSection(view, mine, notice) {
  const list = h('div', {});
  const reload = () => view.show();
  const node = h('section', { class: 'stack gap-1' },
    h('h2', {}, t('meta.title')),
    h('p', { class: 'muted' }, t('meta.intro')),
    h('div', { class: 'metric-grid' },
      card(h('h3', {}, 'WhatsApp'), h('p', { class: 'muted' }, t('meta.whatsapp.text')), list,
        button({ label: t('meta.whatsapp.connect'), icon: 'plus', onClick: () => whatsAppDialog(view, null, notice, reload) })),
      soon('Messenger', t('meta.messenger.text')),
      soon('Instagram', t('meta.instagram.text'))));

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
    if (mine !== view.current() || !channels.length) return;
    channels.sort((a, b) => a.name.localeCompare(b.name));
    mount(list, h('ul', { class: 'stack gap-1' }, channels.map(ch => h('li', { class: 'row gap-1' },
      h('span', { class: 'grow' }, ch.name),
      button({ label: t('meta.webhook'), icon: 'link', variant: 'ghost', ariaLabel: t('meta.webhook.title', { name: ch.name }), onClick: () => showWebhook(ch) }),
      button({ icon: 'edit', variant: 'ghost', ariaLabel: t('meta.edit.title', { name: ch.name }), onClick: () => whatsAppDialog(view, ch, notice, reload) }),
      button({ icon: 'trash', variant: 'ghost', ariaLabel: t('meta.delete.title', { name: ch.name }), onClick: () => remove(ch) })))));
  }).catch(err => { if (mine === view.current()) mount(list, errorBox(err)); });

  return { node };
}
