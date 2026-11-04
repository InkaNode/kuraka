import '../i18n/domains.js';
import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { isSuperadmin } from '../core/session.js';
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
  const addButton = () => button({ label: t('domains.add'), icon: 'plus', variant: 'primary', onClick: () => addDomainDialog(tenants, load) });
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

function addDomainDialog(tenants, onDone) {
  if (!tenants.length) {
    const dialog = modal({ title: t('domains.add'), body: [h('p', {}, t('domains.add.noTenants')), h('a', { href: '#/tenants' }, t('domains.add.goTenants'))],
      actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }
  let mode = 'create';
  const formId = uid('add-domain');
  const tenant = select({ options: [...tenants].sort((a, b) => a.name.localeCompare(b.name)).map(entry => ({ value: String(entry.id), label: entry.name })), required: true });
  const name = textInput({ class: 'mono', required: true, maxlength: 253, autocomplete: 'off', spellcheck: 'false', placeholder: t('domains.add.name.placeholder') });
  const aliases = h('textarea', { class: 'mono', rows: 3, autocomplete: 'off', spellcheck: 'false' });
  const aliasField = field({ label: t('domains.add.aliases'), input: aliases, help: t('domains.add.aliases.help') });
  const modeHelp = h('p', { class: 'help', id: uid('mode-help') }, t('domains.add.mode.create.help'));
  const radioName = uid('mode');
  const legendId = uid('legend');
  const radio = value => {
    const id = uid('radio');
    return h('label', { class: 'checkbox row gap-1', for: id }, h('input', { id, type: 'radio', name: radioName, value, checked: value === mode, onchange: () => {
      mode = value; aliasField.hidden = mode !== 'create';
      modeHelp.textContent = t(`domains.add.mode.${mode}.help`);
      mount(submit.querySelector('span'), t(mode === 'create' ? 'create' : 'domains.add.import'));
    } }), h('span', {}, t(`domains.add.mode.${value}`)));
  };
  const submit = button({ label: t('create'), variant: 'primary', type: 'submit' });
  submit.setAttribute('form', formId);
  const send = async () => {
    const domainName = name.value.trim().toLowerCase();
    const path = `/tenants/${encodeURIComponent(tenant.value)}/domains${mode === 'import' ? '/import' : ''}`;
    await api('POST', path, mode === 'import' ? { name: domainName } : { name: domainName, aliases: splitNames(aliases.value) });
    dialog.close();
    toast(t(mode === 'import' ? 'domains.imported' : 'domains.created', { name: domainName }));
    await onDone();
  };
  const form = h('form', { id: formId, class: 'stack gap-2' },
    field({ label: t('domains.col.tenant'), input: tenant }),
    h('div', { class: 'stack gap-1', role: 'radiogroup', 'aria-labelledby': legendId, 'aria-describedby': modeHelp.id },
      h('span', { class: 'label', id: legendId }, t('domains.add.mode')), h('div', { class: 'row gap-2' }, radio('create'), radio('import')), modeHelp),
    field({ label: t('domains.add.name'), input: name }),
    aliasField);
  submitForm(form, send);
  const dialog = modal({ title: t('domains.add'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), submit] });
}
