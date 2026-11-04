import '../i18n/admin.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { getMe } from '../core/session.js';
import { button, pageHeader, table, badge, errorBox } from '../core/ui.js';

export async function auditView({ me = getMe() } = {}) {
  const [initial, users] = await Promise.all([
    api('GET', '/audit?limit=50'), me?.role === 'superadmin' ? api('GET', '/users') : Promise.resolve([]),
  ]);
  let rows = initial;
  let hasMore = initial.length === 50;
  let busy = false;
  let filter = 'all';
  const emails = new Map(users.map(user => [user.id, user.email]));
  const content = h('div');
  const errors = h('div');
  const status = h('p', { class: 'muted', role: 'status', 'aria-live': 'polite' });
  const filters = h('div', { class: 'row gap-1', role: 'group', 'aria-label': t('audit.filters') });
  const choices = ['all', 'ok', 'denied', 'error'].map(value => {
    const el = button({ label: t(`audit.${value}`), onClick: () => { filter = value; render(); } });
    el.classList.add('chip'); filters.append(el);
    return [value, el];
  });
  const more = button({ label: t('audit.more'), onClick: async () => {
    if (busy || !hasMore) return;
    busy = true; more.disabled = true; content.setAttribute('aria-busy', 'true'); mount(errors);
    try {
      // The cursor follows the full page, regardless of the active local filter.
      const page = await api('GET', `/audit?limit=50&before=${encodeURIComponent(rows.at(-1).id)}`);
      const seen = new Set(rows.map(row => row.id));
      rows = [...rows, ...page.filter(row => !seen.has(row.id))];
      hasMore = page.length === 50;
      render();
    } catch (err) { mount(errors, errorBox(err)); errors.firstElementChild.focus(); }
    finally { busy = false; more.disabled = false; content.removeAttribute('aria-busy'); }
  } });
  function render() {
    for (const [value, el] of choices) el.setAttribute('aria-pressed', String(value === filter));
    status.textContent = t('audit.loaded', { count: rows.length });
    more.hidden = !hasMore;
    end.hidden = hasMore;
    mount(content, table({ rows: filter === 'all' ? rows : rows.filter(row => row.result === filter), columns: [
      { label: t('audit.time'), render: row => formatDate(row.at) },
      { label: t('audit.user'), className: 'mono', render: row => emails.get(row.user_id)
        || (row.user_id == null ? t('audit.anonymous') : row.user_id === me?.id ? t('audit.me') : String(row.user_id)) },
      { label: t('audit.action'), className: 'mono', render: row => row.action },
      { label: t('audit.target'), className: 'mono', render: row => {
        const value = String(row.target || '');
        return h('span', { title: value }, value.length > 64 ? t('admin.truncated', { value: value.slice(0, 64) }) : value);
      } },
      { label: t('audit.result'), render: row => badge(t(`audit.${row.result}`), ({ ok: 'ok', denied: 'warn', error: 'danger' })[row.result] || 'neutral') },
      { label: t('audit.ip'), className: 'mono', render: row => row.ip || t('admin.unavailable') },
    ] }));
  }
  const end = h('p', { class: 'muted' }, t('audit.end'));
  render();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.audit')), filters, content, errors, status, more, end);
}
