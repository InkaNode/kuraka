import { h, mount } from '../core/h.js';
import { t, formatDate } from '../core/i18n.js';
import { api, errorMessage } from '../core/api.js';
import { icon } from '../core/icons.js';
import { button, field, textInput, select, table, badge, spinner, errorBox, emptyState, modal, toast, formError, submitForm } from '../core/ui.js';

const POLL_MS = 3000;
const ACTIVE = ['queued', 'running'];
const STATUS_KIND = { queued: 'neutral', running: 'warn', done: 'ok', failed: 'danger', cancelled: 'neutral' };
const TLS_MODES = ['tls', 'starttls', 'none'];
const DEFAULT_PORT = { tls: '993', starttls: '143', none: '143' };
const formatCount = n => new Intl.NumberFormat().format(n || 0);
const dash = () => h('span', { class: 'muted' }, '—');

export async function showMigrations(view, mine) {
  const { panel } = view;
  mount(panel, spinner());
  let jobs;
  let mailboxes;
  try {
    [jobs, mailboxes] = await Promise.all([api('GET', `${view.base}/migrations`), view.loadMailboxes().catch(() => [])]);
  } catch (err) {
    if (mine === view.current()) mount(panel, errorBox(err), button({ label: t('domain.retry'), icon: 'refresh', onClick: view.show }));
    return;
  }
  if (mine !== view.current()) return;
  const emails = new Map(mailboxes.map(m => [m.id, m.email]));
  const emailOf = j => emails.get(j.account_id) || j.account_id;
  const notice = h('div', {});
  const list = h('div', {});
  const create = () => migrationDialog(view, null, () => view.goTab('migrations'));
  mount(panel,
    h('div', { class: 'row gap-2' }, h('p', { class: 'muted grow' }, t('migration.intro')),
      button({ label: t('migration.new'), icon: 'plus', variant: 'primary', onClick: create })),
    notice, list);

  // Poll while a job is pending, the tab is shown and the browser tab is visible.
  let timer = 0;
  const alive = () => mine === view.current() && panel.isConnected;
  const onLeave = () => stop();
  const onVisible = () => { if (!document.hidden && !timer) schedule(); };
  function stop() {
    clearTimeout(timer); timer = 0;
    window.removeEventListener('hashchange', onLeave);
    document.removeEventListener('visibilitychange', onVisible);
  }
  function schedule() {
    clearTimeout(timer); timer = 0;
    if (!alive() || !jobs.some(j => ACTIVE.includes(j.status))) { stop(); return; }
    window.addEventListener('hashchange', onLeave);
    document.addEventListener('visibilitychange', onVisible);
    if (!document.hidden) timer = setTimeout(() => { timer = 0; refresh(); }, POLL_MS);
  }
  async function refresh() {
    if (!alive()) { stop(); return; }
    try { jobs = await api('GET', `${view.base}/migrations`); mount(notice); }
    catch (err) { mount(notice, errorBox(err)); }
    if (!alive()) { stop(); return; }
    render(); schedule();
  }

  async function cancel(j, el) {
    el.disabled = true;
    try {
      await api('POST', `${view.base}/migrations/${encodeURIComponent(j.id)}/cancel`);
      toast(t('migration.cancelled', { email: emailOf(j) }));
    } catch (err) { mount(notice, errorBox(err)); }
    await refresh();
  }

  function render() {
    // Keep keyboard focus on the same action across poll re-renders.
    const focusKey = document.activeElement?.closest?.('[data-focus-key]')?.getAttribute('data-focus-key');
    const rows = [...jobs].sort((a, b) => b.id - a.id);
    if (!rows.length) {
      mount(list, emptyState({ icon: 'download', title: t('migration.empty.title'), text: t('migration.empty.text'),
        action: button({ label: t('migration.new'), icon: 'plus', variant: 'primary', onClick: create }) }));
      return;
    }
    mount(list, table({ rows, columns: [
      { label: t('migration.col.mailbox'), render: j => h('span', { class: 'mono' }, emailOf(j)) },
      { label: t('migration.col.source'), render: j => h('div', { class: 'stack gap-0' },
        h('span', { class: 'mono' }, j.source_user),
        h('span', { class: 'muted' }, t('migration.source.detail', { host: j.source_host, port: j.source_port, tls: t(`migration.tls.${j.source_tls}`) }))) },
      { label: t('migration.col.status'), render: j => badge(Object.hasOwn(STATUS_KIND, j.status) ? t(`migration.status.${j.status}`) : String(j.status), STATUS_KIND[j.status] || 'neutral') },
      { label: t('migration.col.progress'), render: j => progressCell(j, emailOf(j)) },
      { label: t('migration.col.started'), render: j => j.started_at ? formatDate(j.started_at) : dash() },
      { label: t('migration.col.finished'), render: j => j.finished_at ? formatDate(j.finished_at) : dash() },
      { label: t('migration.col.error'), render: j => j.error ? h('span', { class: 'danger' }, j.error) : dash() },
      { label: t('migration.col.actions'), render: j => {
        if (!ACTIVE.includes(j.status)) return dash();
        const el = button({ label: t('migration.cancel'), icon: 'x', ariaLabel: t('migration.cancel.label', { email: emailOf(j) }), onClick: () => cancel(j, el) });
        el.setAttribute('data-focus-key', `cancel-${j.id}`);
        return el;
      } },
    ] }));
    if (focusKey) list.querySelector(`[data-focus-key="${CSS.escape(focusKey)}"]`)?.focus();
  }

  render();
  schedule();
}

function progressCell(j, email) {
  const done = (j.messages_copied || 0) + (j.messages_skipped || 0);
  const total = j.messages_total || 0;
  if (!total && !j.folders_total && j.status !== 'running') return dash();
  const label = t('migration.progress.label', { email });
  const bar = total > 0
    ? h('progress', { max: total, value: Math.min(done, total), 'aria-label': label })
    : j.status === 'running' ? h('progress', { 'aria-label': label }) : null;
  return h('div', { class: 'stack gap-0' }, bar,
    h('span', { class: 'muted' }, t('migration.progress.messages', { done: formatCount(done), total: formatCount(total), skipped: formatCount(j.messages_skipped) })),
    h('span', { class: 'muted' }, t('migration.progress.folders', { done: formatCount(j.folders_done), total: formatCount(j.folders_total) })));
}

export async function migrationDialog(view, accountId, onCreated) {
  let mailboxes;
  try { mailboxes = await view.loadMailboxes(); }
  catch (err) { toast(errorMessage(err), 'danger'); return; }
  const users = mailboxes.filter(m => m.kind === 'user').sort((a, b) => a.email.localeCompare(b.email));
  if (!users.length) {
    const dialog = modal({ title: t('migration.new'), body: h('p', {}, t('migration.noTargets')),
      actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }
  const formId = `migration-${Date.now()}`;
  const target = select({ class: 'mono', required: true, options: users.map(m => ({ value: m.id, label: m.email })),
    value: users.some(m => m.id === accountId) ? accountId : users[0].id });
  const host = textInput({ class: 'mono', required: true, maxlength: 253, autocomplete: 'off', spellcheck: 'false', placeholder: t('migration.host.placeholder') });
  const port = textInput({ type: 'number', required: true, min: 1, max: 65535, step: 1, inputmode: 'numeric', value: DEFAULT_PORT.tls });
  const tls = select({ options: TLS_MODES.map(mode => ({ value: mode, label: t(`migration.tls.${mode}`) })), value: 'tls' });
  const tlsWarning = h('p', { class: 'secret-warning row gap-1', hidden: true }, icon('alert'), h('span', {}, t('migration.tls.none.warning')));
  tls.addEventListener('change', () => {
    // Follow the usual port unless the user typed a custom one.
    if (Object.values(DEFAULT_PORT).includes(port.value)) port.value = DEFAULT_PORT[tls.value];
    tlsWarning.hidden = tls.value !== 'none';
  });
  const user = textInput({ class: 'mono', required: true, maxlength: 320, autocomplete: 'off', spellcheck: 'false' });
  const password = textInput({ type: 'password', required: true, maxlength: 1024, autocomplete: 'new-password' });

  const submit = button({ label: t('migration.submit'), variant: 'primary', type: 'submit' });
  submit.setAttribute('form', formId);
  const send = async () => {
    for (const el of [host, tls]) el.removeAttribute('aria-invalid');
    const email = users.find(m => m.id === target.value)?.email || target.value;
    const job = await api('POST', `${view.base}/migrations`, { account_id: target.value, source_host: host.value.trim(), source_port: Number(port.value),
      source_tls: tls.value, source_user: user.value.trim(), source_password: password.value });
    password.value = '';
    dialog.close();
    toast(t('migration.created', { email }));
    onCreated?.(job);
  };
  const form = h('form', { id: formId, class: 'stack gap-2' },
    h('p', {}, t('migration.dialog.intro')),
    field({ label: t('migration.target'), input: target }),
    field({ label: t('migration.host'), input: host }),
    h('div', { class: 'row gap-2' }, field({ label: t('migration.port'), input: port }), field({ label: t('migration.tls'), input: tls })),
    tlsWarning,
    field({ label: t('migration.user'), input: user }),
    field({ label: t('migration.password'), input: password }),
    h('p', { class: 'help row gap-1' }, icon('key'), h('span', {}, t('migration.secret.note'))));
  submitForm(form, send, { onError: err => {
    formError(form, err);
    if (/source_host/.test(err.message || '')) host.setAttribute('aria-invalid', 'true');
    if (/source_tls/.test(err.message || '')) tls.setAttribute('aria-invalid', 'true');
  } });
  const dialog = modal({ title: t('migration.new'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), submit] });
}
