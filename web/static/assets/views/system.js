import '../i18n/system.js';
import { h, mount } from '../core/h.js';
import { api, errorMessage } from '../core/api.js';
import { t, formatDate, getLang } from '../core/i18n.js';
import { button, card, pageHeader, table, badge, checkbox, errorBox, confirmDialog, toast, infoTip } from '../core/ui.js';

const UPDATE_POLL_MS = 10000;
const IDLE_POLL_MS = 30000;
const UPDATE_BADGE = { current: 'ok', available: 'warn', waiting: 'warn', updating: 'neutral', error: 'danger', unknown: 'neutral' };

export async function systemView() {
  let updatesError = null;
  let addonsError = null;
  const [initialUpdates, addons] = await Promise.all([
    api('GET', '/updates').catch(err => { updatesError = err; return null; }),
    api('GET', '/addons').catch(err => { addonsError = err; return null; }),
  ]);
  let updates = initialUpdates;
  const root = h('div', { class: 'stack gap-3' });
  const updatesBody = h('div', { class: 'stack gap-3' });
  const applying = new Set();
  let pollTimer = 0;
  let kurakaBefore = updates?.kuraka?.state === 'updating' ? { ...updates.kuraka } : null;

  const dash = () => h('span', { class: 'muted' }, '—');
  const version = value => value ? h('span', { class: 'mono' }, value) : dash();
  function updatesTable(rows) {
    return table({ rows, columns: [
      { label: t('system.updates.name'), render: row => row.name },
      { label: t('system.updates.current'), render: row => version(row.key === 'kuraka' && row.commit ? `${row.current} (${row.commit.slice(0, 7)})` : row.current) },
      { label: t('system.updates.latest'), render: row => h('div', { class: 'stack gap-1' },
        version(row.key === 'kuraka' && row.latest_commit ? `${row.latest} (${row.latest_commit.slice(0, 7)})` : row.latest),
        row.key === 'kuraka' && row.url?.startsWith('https:' + '//github.com/')
          ? h('a', { href: row.url, target: '_blank', rel: 'noopener' }, t('system.updates.changes')) : null) },
      { label: t('system.updates.status'), render: row => h('div', { class: 'stack gap-1' },
        h('div', { class: 'row gap-1' },
          badge(t(row.state === 'available' && ['version', 'commits', 'patches'].includes(row.kind)
            ? `system.updates.available.${row.kind}` : `system.updates.state.${row.state}`, { latest: row.latest }), UPDATE_BADGE[row.state] || 'neutral'),
          row.state === 'waiting' ? infoTip(t('system.updates.waiting', { latest: row.latest, supported: row.supported })) : null),
        row.state !== 'waiting' && row.message ? h('span', { class: 'muted' }, row.message) : null) },
      // Failed server updates can be retried.
      { label: t('system.updates.actions'), render: row => (row.state === 'available' || (row.key !== 'webui' && row.state === 'error'))
        && (row.key === 'kuraka' || row.key === 'webui' || updates.updates_dir)
        ? button({ label: t(row.key === 'kuraka' ? 'system.updates.applyKuraka' : 'system.updates.apply'), disabled: applying.has(row.key), onClick: () => applyUpdate(row) }) : null },
    ] });
  }

  // Every patch Kuraka adds; the list opens on its own when some are skipped
  // because they no longer apply to this upstream version.
  function patchList(series) {
    if (!series?.length) return null;
    const skipped = series.filter(patch => patch.skipped).length;
    return h('details', { open: skipped > 0 },
      h('summary', {}, t(skipped ? 'system.patches.summarySkipped' : 'system.patches.summary', { n: series.length - skipped, skipped })),
      h('div', { class: 'stack gap-1' },
        h('p', { class: 'muted' }, t(skipped ? 'system.patches.intro' : 'system.patches.help')),
        h('ul', { class: 'patch-list stack gap-1' }, series.map(patch => h('li', { class: 'row gap-1' },
          patch.skipped ? badge(t(`system.patches.${patch.severity}`), patch.severity === 'urgent' ? 'danger' : 'warn') : badge(t('system.patches.active'), 'ok'),
          h('span', { class: 'stack grow' }, patch.title,
            patch.skipped ? h('span', { class: 'muted' }, getLang() === 'en' ? patch.missing_en : patch.missing_es) : null))))));
  }

  const section = (title, ...body) => card(h('div', { class: 'stack gap-2' }, h('h2', {}, title), ...body));
  function renderUpdates() {
    if (!updates) {
      mount(updatesBody, updatesError ? errorBox(updatesError) : null,
        section(t('system.stalwart'), addonNode('stalwart')), section(t('system.desk'), addonNode('desk')));
      return;
    }
    const desks = updates.libredesk.helpdesks.map(desk => ({ ...desk, key: desk.id, latest: updates.libredesk.latest }));
    mount(updatesBody,
      h('div', { class: 'stack gap-1' },
        h('p', { class: 'muted' }, updates.checked_at ? t('system.updates.checked', { date: formatDate(updates.checked_at) }) : t('system.updates.never')),
        h('p', { class: 'muted' }, t('system.updates.help')),
        updates.error ? h('p', { class: 'help' }, t('system.updates.checkError')) : null,
        !updates.updates_dir ? h('p', { class: 'muted' }, t('system.updates.noDir')) : null),
      section('Kuraka', updatesTable([{ key: 'kuraka', name: 'Kuraka', state: 'unknown', ...updates.kuraka }])),
      section(t('system.stalwart'),
        updatesTable([{ key: 'stalwart', name: t('system.updates.stalwart'), ...updates.stalwart }, { key: 'webui', name: t('system.updates.webui'), ...updates.webui }]),
        h('p', { class: 'muted' }, t('system.updates.together')),
        patchList(updates.webui.patches), addonNode('stalwart')),
      section(t('system.desk'),
        desks.length ? updatesTable(desks) : h('p', { class: 'muted' }, t('system.desk.none')),
        patchList(updates.libredesk.patches), addonNode('desk')));
  }

  // Keeps the card current (daily checks, other admins, running updates), retries through
  // maintenance responses and stops once the card leaves the document.
  function schedulePoll() {
    clearTimeout(pollTimer); pollTimer = 0;
    if (!kurakaBefore && updates?.kuraka?.state === 'updating') kurakaBefore = { ...updates.kuraka };
    const busy = updates?.stalwart.state === 'updating' || updates?.libredesk.helpdesks.some(desk => desk.state === 'updating');
    pollTimer = setTimeout(async () => {
      pollTimer = 0;
      if (!updatesBody.isConnected) return;
      try {
        const result = await api('GET', '/updates');
        if (!updatesBody.isConnected) return;
        if (kurakaBefore && result?.kuraka && (result.kuraka.commit !== kurakaBefore.commit
          || result.kuraka.current !== kurakaBefore.current || result.kuraka.state !== 'updating')) {
          location.reload(); return;
        }
        // Re-render only on changes so focus and open tips survive idle polls.
        if (updatesError || JSON.stringify(result) !== JSON.stringify(updates)) { updates = result; updatesError = null; renderUpdates(); }
      } catch { /* Network errors and non-JSON maintenance responses are retried. */ }
      schedulePoll();
    }, kurakaBefore ? 5000 : busy ? UPDATE_POLL_MS : IDLE_POLL_MS);
  }

  async function checkUpdates(event) {
    const trigger = event.currentTarget;
    if (trigger.disabled) return;
    trigger.disabled = true;
    try {
      updates = await api('POST', '/updates/check'); updatesError = null; renderUpdates(); schedulePoll();
      const found = [updates.kuraka, updates.stalwart, updates.webui, ...updates.libredesk.helpdesks].some(row => row?.state === 'available');
      if (root.isConnected) toast(t(updates.error ? 'system.updates.checkError' : found ? 'system.updates.found' : 'system.updates.none'), updates.error ? 'danger' : 'ok');
    }
    catch (err) { if (root.isConnected) toast(errorMessage(err), 'danger'); }
    finally { trigger.disabled = false; }
  }

  async function applyUpdate(row) {
    if (applying.has(row.key)) return;
    const kuraka = row.key === 'kuraka', webui = row.key === 'webui', stalwart = row.key === 'stalwart';
    applying.add(row.key); renderUpdates();
    try {
      if (!webui && !await confirmDialog({ title: t('system.updates.confirmTitle', { name: row.name }),
        message: t(kuraka ? 'system.updates.confirmKuraka' : stalwart ? 'system.updates.confirmStalwart' : 'system.updates.confirm'),
        confirmLabel: t(kuraka ? 'system.updates.confirmApply' : 'system.updates.apply') })) return;
      if (!updatesBody.isConnected) return;
      const before = kuraka ? { ...updates.kuraka } : null;
      updates = await api('POST', kuraka ? '/updates/kuraka' : webui ? '/updates/webui' : stalwart ? '/updates/stalwart' : `/helpdesks/${encodeURIComponent(row.key)}/update`);
      if (kuraka) kurakaBefore = before;
      if (root.isConnected) toast(t(webui ? 'system.updates.webuiDone' : 'system.updates.started'));
    } catch (err) { if (root.isConnected) toast(errorMessage(err), 'danger'); }
    finally { applying.delete(row.key); renderUpdates(); schedulePoll(); }
  }

  // Addon nodes are built once and moved into each re-render, so the switch keeps its state.
  const addonNodes = {};
  function addonNode(app) {
    if (addonsError) return app === 'stalwart' ? errorBox(addonsError) : null;
    const addon = addons?.[app];
    if (!addon) return null;
    if (addonNodes[app]) return addonNodes[app];
    const name = t(`system.addons.${app}`);
    const report = addon.report;
    const broken = report?.missing?.length > 0;
    const control = checkbox({ label: t('system.addons.disable'), checked: addon.disabled,
      onChange: async event => {
        const input = event.currentTarget;
        const disabled = input.checked;
        input.disabled = true;
        try { await api('PUT', `/addons/${app}`, { disabled }); addon.disabled = disabled; }
        catch (err) { input.checked = addon.disabled; if (root.isConnected) toast(errorMessage(err), 'danger'); }
        finally { input.disabled = false; }
      },
    });
    addonNodes[app] = h('div', { class: 'stack gap-1' }, h('h3', {}, t('system.addons')),
      broken ? h('div', { class: 'row gap-1', role: 'status' },
        badge(t('system.addons.original'), 'warn'), infoTip(t('system.addons.fix', { app: name })),
        report.at ? h('span', { class: 'muted' }, formatDate(report.at)) : null)
        : badge(t('system.addons.working'), 'ok'), control);
    return addonNodes[app];
  }

  renderUpdates(); schedulePoll();
  mount(root, pageHeader(t('nav.system'), button({ label: t('system.updates.check'), icon: 'refresh', onClick: checkUpdates })), updatesBody);
  return root;
}
