import '../i18n/monitoring.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { button, pageHeader, table, badge, confirmDialog, errorBox } from '../core/ui.js';

export async function queueView() {
  let rows = await api('GET', '/queue');
  let busy = false;
  const content = h('div');
  const errors = h('div');
  async function act(message, cancel) {
    if (busy) return;
    busy = true;
    try {
      const key = cancel ? 'cancel' : 'retry';
      if (!await confirmDialog({ title: t(`queue.${key}`), message: t(`queue.${key}Confirm`, { sender: message.returnPath || t('queue.noSender') }), danger: cancel })) return;
      content.setAttribute('aria-busy', 'true'); mount(errors);
      await api(cancel ? 'DELETE' : 'POST', `/queue/${encodeURIComponent(message.id)}${cancel ? '' : '/retry'}`);
      rows = await api('GET', '/queue'); render();
    } catch (err) { mount(errors, errorBox(err)); errors.firstElementChild.focus(); }
    finally { busy = false; content.removeAttribute('aria-busy'); }
  }
  function render() {
    mount(content, table({ rows, columns: [
      { label: t('queue.created'), render: row => formatDate(row.createdAt) },
      { label: t('queue.sender'), className: 'mono', render: row => row.returnPath || t('queue.noSender') },
      { label: t('queue.recipients'), render: row => h('div', { class: 'stack gap-1' }, row.recipients.map(recipient => h('div', { class: 'stack' },
        h('span', { class: 'mono' }, recipient.address),
        badge(t(`queue.status.${recipient.status}`), ({ Completed: 'ok', Scheduled: 'neutral', TemporaryFailure: 'warn', PermanentFailure: 'danger' })[recipient.status] || 'neutral'),
        recipient.errorMessage ? h('span', { class: 'muted' }, recipient.errorMessage) : null))) },
      { label: t('queue.nextRetry'), render: row => row.nextRetry ? formatDate(row.nextRetry) : t('queue.none') },
      { label: t('queue.size'), render: row => t('queue.bytes', { value: row.size }) },
      { label: t('monitoring.actions'), render: row => h('div', { class: 'row gap-1' },
        button({ label: t('queue.retry'), icon: 'refresh', onClick: () => act(row, false) }),
        button({ label: t('queue.cancel'), variant: 'danger', onClick: () => act(row, true) })) },
    ] }));
  }
  render();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.queue')), errors, content);
}
