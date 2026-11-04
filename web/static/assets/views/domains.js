import '../i18n/domains.js';
import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api, errorMessage } from '../core/api.js';
import { isSuperadmin } from '../core/session.js';
import { navigate } from '../core/router.js';
import { icon } from '../core/icons.js';
import { button, field, textInput, select, pageHeader, table, chip, spinner, errorBox, emptyState, modal, confirmDialog, toast, submitForm } from '../core/ui.js';

let seq = 0;
const uid = prefix => `${prefix}-${++seq}`;
export const splitNames = text => [...new Set(text.split(/[\s,;]+/).map(name => name.trim().toLowerCase()).filter(Boolean))];

export async function domainsView() {
  const admin = isSuperadmin();
  const notice = h('div', {});
  const body = h('div', { class: 'stack gap-2' }, spinner());
  let tenants = [];
  const addButton = () => button({ label: t('domains.add'), icon: 'plus', variant: 'primary', onClick: () => addDomainDialog(tenants) });
  const root = h('div', { class: 'stack gap-2' }, pageHeader(t('nav.domains'), admin ? addButton() : null), notice, body);

  async function load() {
    mount(notice);
    let domains;
    try {
      [domains, tenants] = await Promise.all([api('GET', '/domains'), admin ? api('GET', '/tenants') : []]);
    } catch (err) { mount(body, errorBox(err)); return; }
    if (!domains.length) {
      mount(body, emptyState({ icon: 'globe', title: t('domains.empty.title'), text: t(admin ? 'domains.empty.admin' : 'domains.empty.tenant'), action: admin ? addButton() : null }));
      return;
    }
    domains.sort((a, b) => a.name.localeCompare(b.name));
    const tenantNames = new Map(tenants.map(tenant => [tenant.id, tenant.name]));
    const aliasCells = new Map(domains.map(d => [d.id, h('div', { class: 'row gap-1' }, h('span', { class: 'muted' }, t('loading')))]));
    const columns = [
      { label: t('domains.col.domain'), render: d => h('a', { href: `#/domains/${d.id}`, class: 'mono' }, d.name) },
      { label: t('domains.col.aliases'), render: d => aliasCells.get(d.id) },
      admin ? { label: t('domains.col.tenant'), render: d => tenantNames.get(d.tenant_id) ?? t('domains.tenant.unknown', { id: d.tenant_id }) } : null,
      { label: t('domains.col.actions'), render: d => h('div', { class: 'row gap-1' },
        h('a', { class: 'btn btn-ghost', href: `#/domains/${d.id}`, 'aria-label': t('domains.open.label', { name: d.name }) }, icon('chevron-right'), h('span', {}, t('domains.open'))),
        admin ? button({ label: t('domains.unlink'), icon: 'link', variant: 'ghost', ariaLabel: t('domains.unlink.label', { name: d.name }), onClick: () => unlink(d) }) : null) },
    ].filter(Boolean);
    mount(body, table({ columns, rows: domains }));
    fillAliases(domains, aliasCells, root);
  }

  async function unlink(d) {
    const ok = await confirmDialog({ title: t('domains.unlink.title', { name: d.name }), message: t('domains.unlink.message', { name: d.name }), confirmLabel: t('domains.unlink'), danger: true });
    if (!ok) return;
    try {
      await api('DELETE', `/domains/${d.id}`);
      toast(t('domains.unlinked', { name: d.name }));
      await load();
    } catch (err) { mount(notice, errorBox(err)); notice.firstChild?.focus(); }
  }

  await load();
  return root;
}

// GET /domains omits alias domains, so fetch each domain's detail with a small
// concurrency limit once the table is on screen, and stop if the user leaves.
function fillAliases(domains, cells, root) {
  const queue = [...domains];
  let mounted = false;
  const worker = async () => {
    while (queue.length) {
      if (root.isConnected) mounted = true;
      else if (mounted) return;
      const d = queue.shift();
      const cell = cells.get(d.id);
      try {
        const detail = await api('GET', `/domains/${d.id}`);
        mount(cell, detail.aliases?.length ? detail.aliases.map(name => chip(name)) : h('span', { class: 'muted' }, t('domains.aliases.none')));
      } catch (err) {
        mount(cell, h('span', { class: 'muted', title: err.message }, t('domains.aliases.error')));
      }
    }
  };
  setTimeout(() => { for (let i = 0; i < 4; i++) worker(); }, 0);
}

// One form for every case: a new client can be created inline, and a domain
// that already exists in Stalwart is imported instead of created.
function addDomainDialog(tenants) {
  const NEW = 'new';
  const formId = uid('add-domain');
  const tenant = select({ required: true, options: [...[...tenants].sort((a, b) => a.name.localeCompare(b.name)).map(entry => ({ value: String(entry.id), label: entry.name })),
    { value: NEW, label: t('domains.add.tenant.new') }] });
  const tenantName = textInput({ maxlength: 200, autocomplete: 'off' });
  const tenantNameField = field({ label: t('domains.add.tenant.name'), input: tenantName, help: t('domains.add.tenant.name.help') });
  const syncTenant = () => { tenantNameField.hidden = tenant.value !== NEW; tenantName.required = tenant.value === NEW; };
  tenant.addEventListener('change', syncTenant);
  syncTenant();
  const name = textInput({ class: 'mono', required: true, maxlength: 253, autocomplete: 'off', spellcheck: 'false', placeholder: t('domains.add.name.placeholder') });
  const aliases = h('textarea', { class: 'mono', rows: 3, autocomplete: 'off', spellcheck: 'false' });
  const submit = button({ label: t('domains.add'), variant: 'primary', type: 'submit' });
  submit.setAttribute('form', formId);
  const send = async () => {
    const domainName = name.value.trim().toLowerCase();
    const aliasNames = splitNames(aliases.value);
    if (tenant.value === NEW) {
      const created = await api('POST', '/tenants', { name: tenantName.value.trim() });
      // Keep the new client selected so a retry after a domain error does not create it twice.
      tenant.append(h('option', { value: String(created.id) }, created.name));
      tenant.value = String(created.id);
      tenants.push(created);
      syncTenant();
    }
    const path = `/tenants/${encodeURIComponent(tenant.value)}/domains`;
    let domain;
    let imported = false;
    try {
      domain = await api('POST', path, { name: domainName, aliases: aliasNames });
    } catch (err) {
      if (err.code !== 'domain_in_stalwart') throw err;
      domain = await api('POST', `${path}/import`, { name: domainName });
      imported = true;
      const merged = [...new Set([...(domain.aliases || []), ...aliasNames])];
      // The domain is linked by now: an alias failure is reported, not retried.
      if (merged.length > (domain.aliases || []).length) {
        try { await api('PUT', `/domains/${domain.id}/aliases`, { aliases: merged }); }
        catch (aliasErr) { toast(errorMessage(aliasErr), 'danger'); }
      }
    }
    dialog.close();
    toast(t(imported ? 'domains.imported' : 'domains.created', { name: domainName }));
    navigate(`/domains/${domain.id}`);
  };
  const form = h('form', { id: formId, class: 'stack gap-2' },
    h('p', { class: 'muted' }, t('domains.add.intro')),
    field({ label: t('domains.col.tenant'), input: tenant, help: t('domains.add.tenant.help') }),
    tenantNameField,
    field({ label: t('domains.add.name'), input: name, help: t('domains.add.name.help') }),
    field({ label: t('domains.add.aliases'), input: aliases, help: t('domains.add.aliases.help') }));
  submitForm(form, send);
  const dialog = modal({ title: t('domains.add'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), submit] });
}
