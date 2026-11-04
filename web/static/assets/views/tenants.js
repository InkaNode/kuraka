import '../i18n/admin.js';
import { h, mount } from '../core/h.js';
import { api, errorMessage } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { button, field, textInput, pageHeader, table, modal, confirmDialog, formError, toast } from '../core/ui.js';

export async function tenantsView() {
  const [initial, domains, users] = await Promise.all([
    api('GET', '/tenants'), api('GET', '/domains'), api('GET', '/users'),
  ]);
  let tenants = initial;
  const counts = rows => {
    const result = new Map();
    for (const row of rows) result.set(row.tenant_id, (result.get(row.tenant_id) || 0) + 1);
    return result;
  };
  const domainCounts = counts(domains);
  const userCounts = counts(users);
  const content = h('div');
  function render() {
    mount(content, table({ rows: tenants, columns: [
      { label: t('admin.name'), render: row => row.name },
      { label: t('tenants.domains'), render: row => domainCounts.get(row.id) || 0 },
      { label: t('tenants.users'), render: row => userCounts.get(row.id) || 0 },
      { label: t('admin.created'), render: row => formatDate(row.created_at) },
      { label: t('admin.actions'), render: row => h('div', { class: 'row gap-1' },
        button({ icon: 'edit', label: t('tenants.rename'), ariaLabel: t('tenants.renameLabel', { name: row.name }), onClick: () => edit(row) }),
        button({ icon: 'trash', label: t('delete'), variant: 'danger', ariaLabel: t('tenants.deleteLabel', { name: row.name }), onClick: event => remove(row, event) })) },
    ] }));
  }
  function edit(tenant) {
    let busy = false;
    let closed = false;
    const name = textInput({ name: 'name', value: tenant?.name || '', required: true, autocomplete: 'organization' });
    const submit = button({ label: t(tenant ? 'save' : 'create'), variant: 'primary', type: 'submit' });
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || !form.reportValidity()) return;
      if (!name.value.trim()) { formError(form, new Error(t('error.invalid_input'))); name.focus(); return; }
      busy = true; submit.disabled = true; name.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        const result = await api(tenant ? 'PATCH' : 'POST', tenant ? `/tenants/${tenant.id}` : '/tenants', { name: name.value.trim() });
        tenants = tenant ? tenants.map(row => row.id === tenant.id ? result : row) : [...tenants, result];
        render();
        if (!closed) { dialog.close(); toast(t('admin.saved')); }
      } catch (err) { if (!closed) formError(form, err); }
      finally { busy = false; submit.disabled = false; name.disabled = false; form.removeAttribute('aria-busy'); }
    } }, field({ label: t('admin.name'), input: name }), submit);
    const dialog = modal({ title: t(tenant ? 'tenants.rename' : 'tenants.new'), body: form,
      onClose: () => { closed = true; }, actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
  }
  async function remove(tenant, event) {
    const trigger = event.currentTarget;
    if (trigger.disabled) return;
    trigger.disabled = true;
    try {
      if (!await confirmDialog({ title: t('tenants.deleteTitle'), message: t('tenants.deleteConfirm', { name: tenant.name }), danger: true, confirmLabel: t('delete') })) return;
      await api('DELETE', `/tenants/${tenant.id}`);
      tenants = tenants.filter(row => row.id !== tenant.id);
      render(); toast(t('tenants.deleted'));
    } catch (err) { toast(err.status === 409 ? t('tenants.occupied') : errorMessage(err), 'danger'); }
    finally { trigger.disabled = false; }
  }
  render();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.tenants'),
    button({ label: t('tenants.new'), icon: 'plus', variant: 'primary', onClick: () => edit() })), content);
}
