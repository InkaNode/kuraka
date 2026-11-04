import '../i18n/integrations.js';
import { h, mount } from '../core/h.js';
import { api, ApiError, errorMessage } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { button, card, field, textInput, select, pageHeader, table, emptyState, modal, confirmDialog, formError, toast } from '../core/ui.js';

export async function integrationsView(ctx) {
  const { me } = ctx;
  const admin = me.role === 'superadmin';
  const [dns, helpdesks, tenants] = await Promise.all([
    api('GET', '/dns-providers'), api('GET', '/helpdesks'), admin ? api('GET', '/tenants') : [],
  ]);
  const rows = { dns, helpdesks };
  const paths = { dns: '/dns-providers', helpdesks: '/helpdesks' };
  const tenantNames = new Map(tenants.map(tenant => [tenant.id, tenant.name]));
  const tenantName = id => tenantNames.get(id) || t(!admin && id === me.tenant_id ? 'integrations.ownTenant' : 'integrations.unknownTenant', { id });
  const dnsBody = h('div');
  const helpdeskBody = h('div');
  const root = h('div', { class: 'stack gap-3' });

  function tenantSelect(global = false) {
    return select({ name: 'tenant_id', required: !global, value: '', options: [
      { value: '', label: t(global ? 'integrations.global' : 'integrations.chooseTenant') },
      ...tenants.map(tenant => ({ value: String(tenant.id), label: tenant.name })),
    ] });
  }

  const activating = new Set();

  function helpdeskURL(row) {
    // Treat stored URLs as untrusted; only web links may be opened.
    try {
      const url = new URL(row.url);
      // Provisioned desks store their internal URL; the browser opens the public host.
      if (/^kuraka-[a-z0-9-]+-desk$/.test(url.hostname) && row.name.length <= 253 && /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(row.name)) {
        url.protocol = 'https:'; url.host = row.name; url.port = ''; url.pathname = '/'; url.search = ''; url.hash = '';
      }
      if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) {
        return h('a', { class: 'mono', href: url.href, target: '_blank', rel: 'noopener' }, url.href);
      }
    } catch { /* Invalid stored values remain plain text. */ }
    return h('span', { class: 'mono' }, row.url);
  }

  function render() {
    for (const [kind, body] of [['dns', dnsBody], ['helpdesks', helpdeskBody]]) {
      const isDNS = kind === 'dns';
      mount(body, table({ rows: rows[kind], empty: emptyState({
        icon: isDNS ? 'globe' : 'inbox',
        title: t(isDNS ? 'integrations.dnsEmpty' : 'integrations.helpdeskEmpty'),
        text: t(isDNS ? 'integrations.dnsEmptyHelp' : 'integrations.helpdeskEmptyHelp'),
      }), columns: [
        { label: t('integrations.name'), render: row => row.name },
        { label: t(isDNS ? 'integrations.scope' : 'integrations.tenant'), render: row => row.tenant_id == null ? t('integrations.global') : tenantName(row.tenant_id) },
        isDNS ? { label: t('integrations.created'), render: row => formatDate(row.created_at) }
          : { label: t('integrations.url'), render: helpdeskURL },
        ...(!isDNS ? [{ label: t('integrations.status'), render: row => t(`integrations.${row.status}`) }] : []),
        { label: t('integrations.actions'), render: row => h('div', { class: 'row gap-1' },
          !isDNS && row.status === 'pending' ? button({ label: t('integrations.activate'), disabled: activating.has(row.id), onClick: () => activate(row) }) : null,
          button({
            label: t('delete'), icon: 'trash', variant: 'danger',
            ariaLabel: t('integrations.deleteLabel', { name: row.name }), onClick: event => remove(kind, row, event),
          })) },
      ] }));
    }
  }

  async function activate(row, silent = false) {
    if (activating.has(row.id)) return;
    activating.add(row.id); render();
    try {
      const result = await api('POST', `/helpdesks/${encodeURIComponent(row.id)}/activate`);
      rows.helpdesks = rows.helpdesks.map(entry => entry.id === row.id ? result : entry);
      if (!silent && root.isConnected) toast(t('integrations.activated'));
    } catch (err) {
      if (!silent && root.isConnected) toast(err.code === 'helpdesk_not_ready' ? t('integrations.notReady') : errorMessage(err), 'danger');
    } finally { activating.delete(row.id); render(); }
  }

  async function remove(kind, row, event) {
    const trigger = event.currentTarget;
    if (trigger.disabled) return;
    trigger.disabled = true;
    try {
      if (!await confirmDialog({ title: t('integrations.deleteTitle'),
        message: t(kind === 'dns' ? 'integrations.deleteDNS' : 'integrations.deleteHelpdesk', { name: row.name }),
        danger: true, confirmLabel: t('delete') })) return;
      await api('DELETE', `${paths[kind]}/${encodeURIComponent(row.id)}`);
      rows[kind] = rows[kind].filter(entry => entry.id !== row.id);
      render();
      if (root.isConnected) toast(t('integrations.deleted'));
    } catch (err) { if (root.isConnected) toast(errorMessage(err), 'danger'); }
    finally { trigger.disabled = false; }
  }

  function connect(kind) {
    const isDNS = kind === 'dns';
    let busy = false;
    let closed = false;
    const name = textInput({ name: 'name', required: true, autocomplete: 'off' });
    const tenant = admin ? tenantSelect(isDNS) : null;
    const secret = textInput({ name: isDNS ? 'token' : 'api_secret', type: 'password', required: true, autocomplete: 'off', spellcheck: 'false' });
    const url = isDNS ? null : textInput({ name: 'url', type: 'url', required: true, autocomplete: 'off', spellcheck: 'false' });
    const key = isDNS ? null : textInput({ name: 'api_key', required: true, autocomplete: 'off', spellcheck: 'false' });
    const controls = [name, tenant, secret, url, key].filter(Boolean);
    const title = t(isDNS ? 'integrations.connectDNS' : 'integrations.connectHelpdesk');
    const submit = button({ label: title, variant: 'primary', type: 'submit', disabled: admin && !isDNS && !tenants.length });
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || !form.reportValidity()) return;
      formError(form, null);
      if (!name.value.trim() || !secret.value.trim()) {
        formError(form, new ApiError(422, 'invalid_input')); return;
      }
      if (!isDNS) {
        try {
          const parsed = new URL(url.value.trim());
          if (!['http:', 'https:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error();
        } catch { formError(form, new Error(t('integrations.urlHelp'))); url.focus(); return; }
      }
      const payload = { name: name.value.trim(), tenant_id: admin ? (tenant.value ? Number(tenant.value) : null) : me.tenant_id,
        kind: isDNS ? 'cloudflare' : 'libredesk',
        ...(isDNS ? { token: secret.value } : { url: url.value.trim(), api_key: key.value, api_secret: secret.value }),
      };
      busy = true; submit.disabled = true; controls.forEach(input => { input.disabled = true; });
      form.setAttribute('aria-busy', 'true');
      try {
        const result = await api('POST', paths[kind], payload);
        rows[kind] = [...rows[kind], result]; render();
        if (!closed) { dialog.close(); toast(t('integrations.connected')); }
      } catch (err) {
        if (!closed) formError(form, err.code === 'upstream_error'
          ? new Error(t(isDNS ? 'integrations.dnsVerification' : 'integrations.helpdeskVerification')) : err);
      } finally {
        busy = false; submit.disabled = false; controls.forEach(input => { input.disabled = false; });
        form.removeAttribute('aria-busy');
        delete payload.token; delete payload.api_key; delete payload.api_secret;
      }
    } }, field({ label: t('integrations.name'), input: name }),
    tenant ? field({ label: t(isDNS ? 'integrations.scope' : 'integrations.tenant'), input: tenant }) : null,
    url ? field({ label: t('integrations.url'), input: url, help: t('integrations.urlHelp') }) : null,
    key ? field({ label: t('integrations.apiKey'), input: key }) : null,
    field({ label: t(isDNS ? 'integrations.token' : 'integrations.apiSecret'), input: secret }),
    admin && !isDNS && !tenants.length ? h('p', { class: 'help' }, t('integrations.noTenants')) : null, submit);
    const dialog = modal({ title, body: form, onClose: () => { closed = true; secret.value = ''; if (key) key.value = ''; },
      actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
  }

  function bundleForm() {
    const tenant = tenantSelect();
    const host = textInput({ name: 'host', required: true, placeholder: 'desk.example.com', autocomplete: 'off', spellcheck: 'false' });
    let busy = false;
    const submit = button({ label: t('integrations.bundle'), icon: 'download', type: 'submit', disabled: !tenants.length });
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || !form.reportValidity()) return;
      busy = true; submit.disabled = true; tenant.disabled = true; host.disabled = true;
      form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        let response;
        try { response = await fetch('/api/v1/helpdesks/provision', {
          method: 'POST', credentials: 'same-origin', headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
          body: JSON.stringify({ tenant_id: Number(tenant.value), host: host.value.trim().toLowerCase() }),
        }); }
        catch { throw new ApiError(0, 'network'); }
        if (!response.ok) {
          if (response.status === 401) window.dispatchEvent(new Event('auth:lost'));
          const data = await response.json().catch(() => null);
          throw new ApiError(response.status, data?.error?.code || 'internal_error');
        }
        const blob = await response.blob();
        if (!form.isConnected) return;
        const disposition = response.headers.get('Content-Disposition') || '';
        const match = /filename\s*=\s*(?:"([^"]+)"|([^;]+))/i.exec(disposition);
        const filename = (match?.[1] || match?.[2] || t('integrations.bundleFilename', { tenant: tenant.value })).trim().replace(/[\\/\x00-\x1f\x7f]/g, '_');
        const objectURL = URL.createObjectURL(blob);
        const link = h('a', { href: objectURL, download: filename, hidden: true });
        try { document.body.append(link); link.click(); }
        finally { link.remove(); setTimeout(() => URL.revokeObjectURL(objectURL), 1000); }
        const dns = response.headers.get('X-Kuraka-DNS');
        if (['created', 'exists', 'manual', 'error'].includes(dns)) toast(t(`integrations.dns.${dns}`, { host: host.value.trim().toLowerCase() }), ['manual', 'error'].includes(dns) ? 'warn' : 'ok');
        rows.helpdesks = await api('GET', '/helpdesks'); render();
      } catch (err) {
        if (form.isConnected) formError(form, err.code === 'conflict' ? new Error(t('integrations.provisionConflict')) : err instanceof ApiError ? err : new Error(t('integrations.bundleError')));
      } finally { busy = false; submit.disabled = !tenants.length; tenant.disabled = false; host.disabled = false; form.removeAttribute('aria-busy'); }
    } }, h('h3', {}, t('integrations.bundle')), h('p', { class: 'muted' }, t('integrations.bundleHelp')),
    !tenants.length ? h('p', { class: 'help' }, t('integrations.noTenants')) : null,
    field({ label: t('integrations.tenant'), input: tenant }), field({ label: t('integrations.host'), input: host }), submit);
    return form;
  }

  render();
  mount(root, pageHeader(t('nav.integrations')),
    card(h('div', { class: 'stack gap-2' },
      h('div', { class: 'row gap-2' }, h('h2', { class: 'grow' }, t('integrations.dns')),
        button({ label: t('integrations.connectDNS'), icon: 'plus', variant: 'primary', onClick: () => connect('dns') })),
      h('p', { class: 'muted' }, t('integrations.dnsHelp')), dnsBody)),
    card(h('div', { class: 'stack gap-2' },
      h('div', { class: 'row gap-2' }, h('h2', { class: 'grow' }, t('integrations.helpdesk')),
        button({ label: t('integrations.connectHelpdesk'), icon: 'plus', variant: 'primary', onClick: () => connect('helpdesks') })),
      h('p', { class: 'muted' }, t('integrations.helpdeskHelp')), helpdeskBody, admin ? bundleForm() : null)));
  rows.helpdesks.filter(row => row.status === 'pending').forEach(row => activate(row, true));
  return root;
}
