import '../i18n/admin.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { getMe, setMe } from '../core/session.js';
import { button, field, textInput, select, checkbox, pageHeader, table, badge, modal, secretOnce, formError, toast } from '../core/ui.js';

export async function usersView() {
  const [initial, tenants] = await Promise.all([api('GET', '/users'), api('GET', '/tenants')]);
  let users = initial;
  const tenantNames = new Map(tenants.map(tenant => [tenant.id, tenant.name]));
  const content = h('div');
  function render() {
    mount(content, table({ rows: users, columns: [
      { label: t('admin.name'), render: row => row.name },
      { label: t('admin.email'), className: 'mono', render: row => row.email },
      { label: t('admin.role'), render: row => badge(t(`admin.role.${row.role}`)) },
      { label: t('admin.tenant'), render: row => row.tenant_id == null ? t('admin.none') : tenantNames.get(row.tenant_id) || String(row.tenant_id) },
      { label: t('admin.status'), render: row => badge(t(row.disabled ? 'admin.disabled' : 'admin.active'), row.disabled ? 'warn' : 'ok') },
      { label: t('admin.2fa'), render: row => badge(t(row.totp_enabled ? 'admin.enabled' : 'admin.notEnabled'), row.totp_enabled ? 'ok' : 'neutral') },
      { label: t('users.lastLogin'), render: row => row.last_login_at ? formatDate(row.last_login_at) : t('admin.unavailable') },
      { label: t('admin.actions'), render: row => button({ label: t('edit'), icon: 'edit', ariaLabel: t('users.editLabel', { name: row.name }), onClick: () => edit(row) }) },
    ] }));
  }
  function edit(user) {
    let busy = false;
    let closed = false;
    const name = textInput({ name: 'name', value: user?.name || '', required: true, autocomplete: 'name' });
    const email = textInput({ type: 'email', name: 'email', value: user?.email || '', required: true, maxlength: 254, autocomplete: 'username', class: 'mono' });
    const role = select({ name: 'role', value: user?.role || 'tenant_admin', options: ['tenant_admin', 'superadmin'].map(value => ({ value, label: t(`admin.role.${value}`) })) });
    const tenant = select({ name: 'tenant_id', value: user?.tenant_id == null ? '' : String(user.tenant_id), options: [
      { value: '', label: t('users.tenantPlaceholder') }, ...tenants.map(row => ({ value: String(row.id), label: row.name })),
    ] });
    const tenantField = field({ label: t('admin.tenant'), input: tenant });
    function updateTenant() {
      const required = role.value === 'tenant_admin';
      tenantField.hidden = !required; tenant.required = required; tenant.disabled = !required;
    }
    role.addEventListener('change', updateTenant); updateTenant();
    const password = textInput({ type: 'password', name: 'password', autocomplete: 'new-password', minlength: 12 });
    let disabled = user?.disabled || false;
    let reset = false;
    const submit = button({ label: t(user ? 'save' : 'create'), variant: 'primary', type: 'submit' });
    const inputs = h('div', { class: 'stack gap-2' },
      field({ label: t('admin.name'), input: name }),
      user ? h('p', { class: 'mono' }, user.email) : field({ label: t('admin.email'), input: email }),
      field({ label: t('admin.role'), input: role }), tenantField,
      user ? [checkbox({ label: t('admin.disabled'), checked: disabled, onChange: event => { disabled = event.target.checked; } }),
        checkbox({ label: t('users.resetPassword'), onChange: event => { reset = event.target.checked; } })]
        : field({ label: t('admin.password'), input: password, help: t('users.passwordHelp') }));
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || !form.reportValidity()) return;
      if (!name.value.trim()) { formError(form, new Error(t('error.invalid_input'))); name.focus(); return; }
      const body = { name: name.value.trim(), role: role.value, tenant_id: role.value === 'tenant_admin' ? Number(tenant.value) : null };
      if (user) { body.disabled = disabled; body.reset_password = reset; }
      else { body.email = email.value.trim(); body.password = password.value; }
      busy = true; submit.disabled = true; inputs.inert = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        const result = await api(user ? 'PATCH' : 'POST', user ? `/users/${user.id}` : '/users', body);
        const { generated_password: generated, ...saved } = result;
        users = user ? users.map(row => row.id === user.id ? saved : row) : [...users, saved];
        if (getMe()?.id === saved.id) setMe(saved);
        render(); password.value = '';
        if (content.isConnected) {
          if (!closed) dialog.close();
          toast(t('admin.saved'));
          if (generated) secretOnce({ title: t('admin.generated'), label: t('admin.password'), secret: generated });
        }
      } catch (err) { if (!closed) formError(form, err.status === 409 && user ? new Error(t('users.lastSuperadmin')) : err); }
      finally { busy = false; submit.disabled = false; inputs.inert = false; form.removeAttribute('aria-busy'); }
    } }, inputs, submit);
    const dialog = modal({ title: t(user ? 'users.edit' : 'users.new'), body: form,
      onClose: () => { closed = true; password.value = ''; }, actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
  }
  render();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.users'),
    button({ label: t('users.new'), icon: 'plus', variant: 'primary', onClick: () => edit() })), content);
}
