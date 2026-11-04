import '../i18n/monitoring.js';
import '../i18n/security.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { icon } from '../core/icons.js';
import { t, formatDate } from '../core/i18n.js';
import { button, pageHeader, table, badge, confirmDialog, errorBox, emptyState } from '../core/ui.js';

const REASONS = ['portScanning'];

export async function securityView() {
  let rows = await api('GET', '/blocked-ips');
  let busy = false;
  const content = h('div');
  const errors = h('div');
  async function act(row) {
    if (busy) return;
    busy = true;
    try {
      const key = row.internal ? 'fix' : 'unblock';
      if (!await confirmDialog({ title: t(`security.${key}`), message: t(`security.${key}Confirm`, { address: row.address, trust: row.trust }), confirmLabel: t(`security.${key}`) })) return;
      content.setAttribute('aria-busy', 'true'); mount(errors);
      await api('DELETE', `/blocked-ips/${encodeURIComponent(row.id)}`);
      rows = await api('GET', '/blocked-ips'); render();
    } catch (err) { mount(errors, errorBox(err)); errors.firstElementChild.focus(); }
    finally { busy = false; content.removeAttribute('aria-busy'); }
  }
  function render() {
    mount(content, h('div', { class: 'stack gap-2' },
      rows.some(row => row.internal) ? h('p', { class: 'secret-warning row gap-1' }, icon('alert'), h('span', {}, t('security.internalWarning'))) : null,
      table({ rows, empty: emptyState({ icon: 'shield', title: t('security.empty') }), columns: [
        { label: t('security.address'), className: 'mono', render: row => row.address },
        { label: t('security.reason'), render: row => REASONS.includes(row.reason) ? t(`security.reason.${row.reason}`) : row.reason },
        { label: t('security.since'), render: row => formatDate(row.createdAt) },
        { label: t('security.until'), render: row => row.expiresAt ? formatDate(row.expiresAt) : t('security.forever') },
        { label: t('security.type'), render: row => badge(t(row.internal ? 'security.internal' : 'security.external'), row.internal ? 'warn' : 'neutral') },
        { label: t('monitoring.actions'), render: row => button({ label: t(row.internal ? 'security.fix' : 'security.unblock'), icon: row.internal ? 'shield' : undefined, onClick: () => act(row) }) },
      ] })));
  }
  render();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.security')), h('p', { class: 'muted' }, t('security.intro')), errors, content);
}
