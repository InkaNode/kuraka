import { h, mount } from '../core/h.js';
import { t, formatDate } from '../core/i18n.js';
import { api } from '../core/api.js';
import { button, field, textInput, select, badge, spinner, errorBox, modal, confirmDialog, toast, formError, submitForm } from '../core/ui.js';

const CONCURRENCY = 4;
const linkPath = (view, id) => `${view.base}/mailboxes/${encodeURIComponent(id)}/helpdesk`;

function helpdeskError(err) {
  if (err?.code === 'helpdesk_not_ready') return { message: t('helpdesk.error.notReady') };
  if (err?.code === 'upstream_error') return { message: t('helpdesk.error.upstream') };
  if (err?.code === 'conflict') return { message: t('helpdesk.error.conflict') };
  return err;
}

function fill(el, links) {
  mount(el, links?.length ? badge(t('helpdesk.badge'), 'ok') : null);
  if (links?.length) el.firstChild.setAttribute('title', t('helpdesk.badge.title', { n: links.length }));
}

// Row badges load lazily with a small concurrency limit and stop once the
// mailboxes tab is no longer the one on screen.
export function helpdeskBadges(view, rows, mine) {
  const cache = view.cache.links;
  const cells = new Map();
  function slot(m) {
    const el = h('span', {});
    cells.set(m.id, el);
    if (cache.has(m.id)) fill(el, cache.get(m.id));
    return el;
  }
  function refresh(id) {
    const el = cells.get(id);
    if (el?.isConnected) fill(el, cache.get(id));
  }
  function start() {
    const queue = rows.filter(m => !cache.has(m.id));
    const worker = async () => {
      while (queue.length) {
        if (mine !== view.current()) return;
        const m = queue.shift();
        try { cache.set(m.id, await api('GET', linkPath(view, m.id))); refresh(m.id); }
        catch { /* The badge is optional; the dialog reports errors. */ }
      }
    };
    setTimeout(() => { for (let i = 0; i < CONCURRENCY; i++) worker(); }, 0);
  }
  return { slot, refresh, start };
}

export function helpdeskDialog(view, m, onChange) {
  const status = h('div', {});
  const body = h('div', { class: 'stack gap-2' }, spinner());
  const dialog = modal({ title: t('helpdesk.title', { email: m.email }), body,
    actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
  let helpdesks = [];
  let links = [];

  async function load() {
    mount(body, spinner());
    try {
      const [all, current] = await Promise.all([api('GET', '/helpdesks'), api('GET', linkPath(view, m.id))]);
      helpdesks = all.filter(x => x.tenant_id === view.domain.tenant_id).sort((a, b) => a.name.localeCompare(b.name));
      links = current;
      view.cache.links.set(m.id, links);
    } catch (err) {
      mount(body, errorBox(err), h('div', {}, button({ label: t('domain.retry'), icon: 'refresh', onClick: load })));
      return;
    }
    render();
  }

  function changed(next) {
    links = next;
    view.cache.links.set(m.id, links);
    onChange?.();
    render();
  }

  function render() {
    const byId = new Map(helpdesks.map(x => [x.id, x]));
    const linked = new Set(links.map(link => link.helpdesk_id));
    const available = helpdesks.filter(x => x.status === 'ready' && !linked.has(x.id));
    let connect;
    if (!helpdesks.length) connect = h('p', { class: 'muted' }, t('helpdesk.none'));
    else if (!available.length) connect = h('p', { class: 'muted' }, t(helpdesks.some(x => x.status === 'pending' && !linked.has(x.id)) ? 'helpdesk.error.notReady' : 'helpdesk.all'));
    else connect = connectForm(available);
    mount(body,
      h('p', {}, t('helpdesk.intro')),
      status,
      h('section', { class: 'stack gap-1' }, h('h3', {}, t('helpdesk.current')),
        links.length
          ? h('ul', { class: 'stack gap-1' }, links.map(link => linkItem(link, byId.get(link.helpdesk_id))))
          : h('p', { class: 'muted' }, t('helpdesk.current.none'))),
      h('section', { class: 'stack gap-1' }, h('h3', {}, t('helpdesk.connect')), connect));
  }

  function linkItem(link, helpdesk) {
    const name = helpdesk?.name || t('helpdesk.unknown', { id: link.helpdesk_id });
    return h('li', { class: 'row gap-2' },
      h('span', { class: 'stack gap-0 grow' }, h('span', {}, name),
        h('span', { class: 'muted' }, t('helpdesk.link.detail', { inbox: link.inbox_id, date: formatDate(link.created_at) }))),
      button({ label: t('helpdesk.disconnect'), icon: 'x', ariaLabel: t('helpdesk.disconnect.label', { name }), onClick: () => disconnect(link, name) }));
  }

  async function disconnect(link, name) {
    const ok = await confirmDialog({ title: t('helpdesk.disconnect.title', { name }), message: t('helpdesk.disconnect.message', { name, email: m.email }),
      confirmLabel: t('helpdesk.disconnect'), danger: true });
    if (!ok) return;
    mount(status);
    try {
      await api('DELETE', `${linkPath(view, m.id)}/${encodeURIComponent(link.id)}`);
      toast(t('helpdesk.disconnected', { name, email: m.email }));
      changed(links.filter(entry => entry.id !== link.id));
    } catch (err) { mount(status, errorBox(helpdeskError(err))); status.firstChild?.focus(); }
  }

  function connectForm(available) {
    const helpdesk = select({ required: true, options: available.map(x => ({ value: String(x.id), label: x.name })) });
    const inbox = textInput({ maxlength: 128, autocomplete: 'off', placeholder: m.display_name || m.name || '' });
    const submit = button({ label: t('helpdesk.connect.submit'), icon: 'link', variant: 'primary', type: 'submit' });
    const send = async () => {
      const name = available.find(x => String(x.id) === helpdesk.value)?.name || '';
      const link = await api('POST', linkPath(view, m.id), { helpdesk_id: Number(helpdesk.value), inbox_name: inbox.value.trim() });
      toast(t('helpdesk.connected', { name, email: m.email }));
      changed([...links, link]);
    };
    const form = h('form', { class: 'stack gap-2' },
      field({ label: t('helpdesk.select'), input: helpdesk }),
      field({ label: t('helpdesk.inboxName'), input: inbox, help: t('helpdesk.inboxName.help') }),
      h('div', {}, submit));
    submitForm(form, send, { onError: err => formError(form, helpdeskError(err)) });
    return form;
  }

  load();
  return dialog;
}
