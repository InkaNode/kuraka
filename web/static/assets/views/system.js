import '../i18n/system.js';
import { h, mount } from '../core/h.js';
import { api, errorMessage } from '../core/api.js';
import { t, formatDate, getLang } from '../core/i18n.js';
import { button, card, pageHeader, table, badge, checkbox, errorBox, confirmDialog, toast } from '../core/ui.js';

const UPDATE_POLL_MS = 10000;
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
  const updatesBody = h('div', { class: 'stack gap-2' });
  const applying = new Set();
  let pollTimer = 0;
  let kurakaBefore = updates?.kuraka?.state === 'updating' ? { ...updates.kuraka } : null;

  function renderUpdates() {
    if (!updates) { mount(updatesBody, updatesError ? errorBox(updatesError) : null); return; }
    const dash = () => h('span', { class: 'muted' }, '—');
    const version = value => value ? h('span', { class: 'mono' }, value) : dash();
    const list = [
      { key: 'kuraka', name: 'Kuraka', state: 'unknown', ...updates.kuraka },
      { key: 'stalwart', name: t('system.updates.stalwart'), ...updates.stalwart },
      { key: 'webui', name: t('system.updates.webui'), ...updates.webui },
      ...updates.libredesk.helpdesks.map(desk => ({ ...desk, key: desk.id, latest: updates.libredesk.latest, patches: updates.libredesk.patches })),
    ];
    mount(updatesBody,
      h('p', { class: 'muted' }, updates.checked_at ? t('system.updates.checked', { date: formatDate(updates.checked_at) }) : t('system.updates.never')),
      updates.error ? h('p', { class: 'help' }, t('system.updates.checkError')) : null,
      table({ rows: list, columns: [
        { label: t('system.updates.name'), render: row => row.name },
        { label: t('system.updates.current'), render: row => version(row.key === 'kuraka' && row.commit ? `${row.current} (${row.commit.slice(0, 7)})` : row.current) },
        { label: t('system.updates.latest'), render: row => h('div', { class: 'stack gap-1' },
          version(row.key === 'kuraka' && row.latest_commit ? `${row.latest} (${row.latest_commit.slice(0, 7)})` : row.latest),
          row.key === 'kuraka' && row.url?.startsWith('https:' + '//github.com/')
            ? h('a', { href: row.url, target: '_blank', rel: 'noopener' }, t('system.updates.changes')) : null) },
        { label: t('system.updates.status'), render: row => h('div', { class: 'stack gap-1' },
          badge(t(row.key === 'kuraka' && row.state === 'available' && ['version', 'commits'].includes(row.kind)
            ? `system.updates.available.${row.kind}` : `system.updates.state.${row.state}`, { latest: row.latest }), UPDATE_BADGE[row.state] || 'neutral'),
          row.state === 'waiting' ? h('span', { class: 'muted' }, t('system.updates.waiting', { latest: row.latest, supported: row.supported }))
            : row.message ? h('span', { class: 'muted' }, row.message) : null,
          // Patches Kuraka skipped because they no longer apply to this upstream version.
          row.patches?.length ? h('span', { class: 'muted' }, t('system.patches.intro')) : null,
          ...(row.patches || []).map(patch => h('div', { class: 'row gap-1' },
            badge(t(`system.patches.${patch.severity}`), patch.severity === 'urgent' ? 'danger' : 'warn'),
            h('span', {}, getLang() === 'en' ? patch.missing_en : patch.missing_es)))) },
        // Failed server updates can be retried.
        { label: t('system.updates.actions'), render: row => (row.state === 'available' || (row.key !== 'webui' && row.state === 'error'))
          && (row.key === 'kuraka' || row.key === 'webui' || updates.updates_dir)
          ? button({ label: t(row.key === 'kuraka' ? 'system.updates.applyKuraka' : 'system.updates.apply'), disabled: applying.has(row.key), onClick: () => applyUpdate(row) }) : null },
      ] }),
      h('p', { class: 'muted' }, t('system.updates.together')),
      !updates.updates_dir ? h('p', { class: 'muted' }, t('system.updates.noDir')) : null);
  }

  // Retry through maintenance responses and stop once the card leaves the document.
  function schedulePoll() {
    clearTimeout(pollTimer); pollTimer = 0;
    if (!kurakaBefore && updates?.kuraka?.state === 'updating') kurakaBefore = { ...updates.kuraka };
    if (!kurakaBefore && updates?.stalwart.state !== 'updating' && !updates?.libredesk.helpdesks.some(desk => desk.state === 'updating')) return;
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
        updates = result; updatesError = null; renderUpdates();
      } catch { /* Network errors and non-JSON maintenance responses are retried. */ }
      schedulePoll();
    }, kurakaBefore ? 5000 : UPDATE_POLL_MS);
  }

  async function checkUpdates(event) {
    const trigger = event.currentTarget;
    if (trigger.disabled) return;
    trigger.disabled = true;
    try { updates = await api('POST', '/updates/check'); updatesError = null; renderUpdates(); schedulePoll(); }
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

  const addonsBody = h('div', { class: 'stack gap-2' });
  if (addonsError) mount(addonsBody, errorBox(addonsError));
  else if (addons) mount(addonsBody, ['stalwart', 'desk'].map(app => {
    const addon = addons[app];
    if (!addon) return null;
    const name = t(`system.addons.${app}`);
    const report = addon.report;
    const missing = report?.missing || [];
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
    return h('div', { class: 'stack gap-1' }, h('h3', {}, name),
      missing.length ? h('div', { class: 'stack gap-1', role: 'status' },
        badge(t('system.addons.warning', { app: name }), 'warn'),
        h('p', { class: 'muted' }, missing.join(', '), report.at ? ` · ${formatDate(report.at)}` : ''))
        : badge(t('system.addons.working'), 'ok'), control);
  }));

  renderUpdates(); schedulePoll();
  mount(root, pageHeader(t('nav.system')),
    card(h('div', { class: 'stack gap-2' },
      h('div', { class: 'row gap-2' }, h('h2', { class: 'grow' }, t('system.updates')),
        button({ label: t('system.updates.check'), icon: 'refresh', onClick: checkUpdates })),
      h('p', { class: 'muted' }, t('system.updates.help')), updatesBody)),
    card(h('div', { class: 'stack gap-2' }, h('h2', {}, t('system.addons')), addonsBody)));
  return root;
}
