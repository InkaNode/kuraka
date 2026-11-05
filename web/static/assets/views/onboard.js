import '../i18n/onboard.js';
import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api, errorMessage } from '../core/api.js';
import { icon } from '../core/icons.js';
import { button, field, textInput, select, checkbox, card, pageHeader, table, badge, copyButton, errorBox, formError, submitForm } from '../core/ui.js';
import { splitNames, createOrImportDomain } from './domains.js';

const NEW = 'new';
const enc = encodeURIComponent;
const clock = ms => { const s = Math.floor(ms / 1000); return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; };
const BADGE = { pending: 'neutral', ok: 'ok', warn: 'warn', danger: 'danger' };

// One mailbox per line: the local part, then an optional display name.
const parseMailboxes = text => [...new Map(text.split('\n').map(line => line.trim().split(/\s+/)).filter(([local]) => local)
  .map(([local, ...rest]) => [local.toLowerCase(), { name: local.toLowerCase(), display_name: rest.join(' ') }])).values()];

// Superadmin wizard: client, domain, DNS, mailboxes and Libredesk in one run.
export async function onboardView() {
  const viewHash = location.hash;
  let tenants, providers;
  try { [tenants, providers] = await Promise.all([api('GET', '/tenants'), api('GET', '/dns-providers')]); }
  catch (err) { return h('div', { class: 'stack gap-2' }, pageHeader(t('onboard.title')), errorBox(err)); }

  const tenant = select({ required: true, options: [...[...tenants].sort((a, b) => a.name.localeCompare(b.name)).map(entry => ({ value: String(entry.id), label: entry.name })),
    { value: NEW, label: t('onboard.tenant.new') }] });
  const tenantName = textInput({ maxlength: 200, autocomplete: 'off' });
  const tenantNameField = field({ label: t('onboard.tenant.name'), input: tenantName });
  const name = textInput({ class: 'mono', required: true, maxlength: 253, autocomplete: 'off', spellcheck: 'false', placeholder: t('domains.add.name.placeholder') });
  const aliases = h('textarea', { class: 'mono', rows: 2, autocomplete: 'off', spellcheck: 'false' });
  const zoneBox = checkbox({ label: t('onboard.dns.zone') });
  const provider = select({ options: [] });
  const zoneGroup = h('div', { class: 'stack gap-1' }, zoneBox, field({ label: t('onboard.dns.provider'), input: provider }));
  const noProvider = h('p', { class: 'muted' }, t('onboard.dns.none'));
  const adminName = textInput({ maxlength: 200, autocomplete: 'off' });
  const adminEmail = textInput({ type: 'email', maxlength: 254, autocomplete: 'off', spellcheck: 'false' });
  const mailboxes = h('textarea', { class: 'mono', rows: 4, autocomplete: 'off', spellcheck: 'false' });
  const helpdeskBox = checkbox({ label: t('onboard.helpdesk.create'), checked: true });
  const host = textInput({ class: 'mono', maxlength: 253, autocomplete: 'off', spellcheck: 'false' });
  const hostField = field({ label: t('onboard.helpdesk.host'), input: host });
  let hostEdited = false;

  const syncTenant = () => {
    const isNew = tenant.value === NEW;
    tenantNameField.hidden = !isNew; tenantName.required = isNew;
    const keep = provider.value;
    const usable = providers.filter(p => p.tenant_id == null || String(p.tenant_id) === tenant.value);
    provider.replaceChildren(...usable.map(p => h('option', { value: String(p.id) }, p.name)));
    if (usable.some(p => String(p.id) === keep)) provider.value = keep;
    zoneGroup.hidden = !usable.length; noProvider.hidden = usable.length > 0;
  };
  const syncHelpdesk = () => { const on = helpdeskBox.querySelector('input').checked; hostField.hidden = !on; host.required = on; };
  tenant.addEventListener('change', syncTenant);
  adminEmail.addEventListener('input', () => { adminName.required = Boolean(adminEmail.value.trim()); });
  helpdeskBox.addEventListener('change', syncHelpdesk);
  name.addEventListener('input', () => { if (!hostEdited) host.value = name.value.trim() ? `soporte.${name.value.trim().toLowerCase()}` : ''; });
  host.addEventListener('input', () => { hostEdited = true; });
  syncTenant(); syncHelpdesk();

  const section = (title, ...children) => card(h('div', { class: 'stack gap-2' }, h('h2', {}, title), children));
  const inputs = h('div', { class: 'stack gap-2' },
    section(t('onboard.tenant'), field({ label: t('onboard.tenant'), input: tenant }), tenantNameField),
    section(t('onboard.domain'), field({ label: t('onboard.domain.name'), input: name, help: t('domains.add.name.help') }),
      field({ label: t('onboard.domain.aliases'), input: aliases, help: t('onboard.domain.aliases.help') })),
    section(t('onboard.dns'), h('p', {}, t('onboard.dns.info')), zoneGroup, noProvider),
    section(t('onboard.admin'), h('p', { class: 'muted' }, t('onboard.admin.help')),
      field({ label: t('onboard.admin.name'), input: adminName }), field({ label: t('onboard.admin.email'), input: adminEmail })),
    section(t('onboard.mailboxes'), field({ label: t('onboard.mailboxes.list'), input: mailboxes, help: t('onboard.mailboxes.help') })),
    section(t('onboard.helpdesk'), helpdeskBox, hostField));
  const form = h('form', { class: 'stack gap-2 form-page' }, inputs, h('div', { class: 'row gap-1' }, button({ label: t('onboard.submit'), variant: 'primary', type: 'submit' })));
  const progress = h('div', { class: 'stack gap-2 form-page' });
  const credentials = h('div', {});
  const root = h('div', { class: 'stack gap-2' }, pageHeader(t('onboard.title')), h('p', { class: 'muted' }, t('onboard.intro')), form, progress, credentials);

  // Results of the steps that worked, so a retry never repeats them.
  const s = { boxes: new Map(), connected: new Set() };

  function checklist() {
    const list = h('div', { class: 'stack gap-2', 'aria-live': 'polite' });
    mount(progress, section(t('onboard.progress'), list));
    return label => {
      const state = h('span', {}, badge(t('onboard.status.pending'), 'neutral'));
      const detail = h('div', { class: 'stack gap-1 muted' });
      list.append(h('div', { class: 'stack gap-1' }, h('div', { class: 'row gap-1' }, h('span', { class: 'grow' }, label), state), detail));
      return (kind, ...nodes) => { state.replaceChildren(badge(t(`onboard.status.${kind}`), BADGE[kind])); mount(detail, ...nodes); };
    };
  }
  const fail = (row, err) => { row('danger', errorMessage(err)); err.shown = true; throw err; };
  const domainLink = text => h('a', { href: `#/domains/${s.domain.id}` }, text);

  async function run() {
    const step = checklist();
    const tenantRow = step(t('onboard.step.tenant'));
    if (!s.tenant && tenant.value === NEW) {
      try {
        const created = await api('POST', '/tenants', { name: tenantName.value.trim() });
        // Keep the new client selected so a retry does not create it twice.
        tenants.push(created);
        tenant.append(h('option', { value: String(created.id) }, created.name));
        tenant.value = String(created.id);
        s.tenant = created;
        syncTenant();
      } catch (err) { fail(tenantRow, err); }
    }
    s.tenant ??= { id: Number(tenant.value), name: tenant.selectedOptions[0].textContent };
    tenant.disabled = tenantName.disabled = true;
    tenantRow('ok', s.tenant.name);

    const domainRow = step(t('onboard.step.domain'));
    if (!s.domain) {
      try { ({ domain: s.domain, imported: s.imported } = await createOrImportDomain(s.tenant.id, name.value.trim().toLowerCase(), splitNames(aliases.value))); }
      catch (err) { fail(domainRow, err); }
      for (const el of [tenant, tenantName, name, aliases]) el.disabled = true;
    }
    domainRow('ok', h('span', { class: 'mono' }, s.domain.name), s.imported ? ` ${t('onboard.imported')}` : null);
    const id = enc(s.domain.id);

    const zoneRow = step(t('onboard.step.zone'));
    const nameServers = zone => [h('p', {}, t('onboard.zone.ns')),
      (zone.name_servers || []).map(ns => h('div', { class: 'row gap-1' }, h('span', { class: 'mono' }, ns), copyButton(ns)))];
    if (s.zone) zoneRow('ok', nameServers(s.zone));
    else if (s.domain.dns_provider_id != null) zoneRow('ok', t('onboard.zone.skip'));
    else if (!zoneGroup.hidden && zoneBox.querySelector('input').checked && provider.value) {
      try {
        const result = await api('POST', `/domains/${id}/dns/zone`, { provider_id: Number(provider.value) });
        s.domain = result.domain; s.zone = result.zone;
        zoneRow('ok', nameServers(s.zone));
      } catch (err) {
        if (err.code === 'zone_permission') zoneRow('warn', t('onboard.zone.permission'));
        else zoneRow(err.status === 409 ? 'warn' : 'danger', errorMessage(err));
      }
    } else zoneRow('ok', t('onboard.status.skip'));

    const dnsRow = step(t('onboard.step.dns'));
    if (s.domain.dns_provider_id == null) dnsRow('warn', domainLink(t('onboard.dns.manual')));
    else {
      try {
        s.applied ??= await api('POST', `/domains/${id}/dns/apply`);
        const conflict = (s.applied.changes || []).some(change => change.op === 'conflict');
        dnsRow(conflict ? 'warn' : 'ok', t('onboard.dns.applied', { n: s.applied.applied }), conflict ? h('p', {}, domainLink(t('onboard.dns.conflict'))) : null);
      } catch (err) { dnsRow('danger', errorMessage(err)); }
    }

    const email = adminEmail.value.trim();
    if (email) {
      const row = step(t('onboard.step.admin'));
      try {
        s.admin ??= await api('POST', '/users', { email, name: adminName.value.trim(), role: 'tenant_admin', tenant_id: s.tenant.id });
        adminName.disabled = adminEmail.disabled = true;
        row('ok', s.admin.email || email);
      } catch (err) { row('danger', errorMessage(err)); }
      finish();
    }

    for (const box of parseMailboxes(mailboxes.value)) {
      const row = step(t('onboard.step.mailbox', { email: `${box.name}@${s.domain.name}` }));
      try {
        if (!s.boxes.has(box.name)) s.boxes.set(box.name, await api('POST', `/domains/${id}/mailboxes`, box));
        row('ok');
      } catch (err) { row('danger', errorMessage(err)); }
      finish();
    }

    if (helpdeskBox.querySelector('input').checked && await helpdesk(step) && root.isConnected && location.hash === viewHash) await connect(step, id, email);
    finish();
  }

  async function helpdesk(step) {
    const row = step(t('onboard.step.helpdesk'));
    if (!s.helpdesk) {
      const hostName = host.value.trim().toLowerCase();
      try { s.helpdesk = { ...await api('POST', '/helpdesks/provision', { tenant_id: s.tenant.id, host: hostName, install: true }), host: hostName }; }
      catch (err) {
        if (err.code === 'updates_disabled') row('warn', t('onboard.helpdesk.disabled'));
        else if (err.status === 409) row('warn', t('onboard.helpdesk.conflict', { host: hostName }));
        else row('danger', errorMessage(err));
        return false;
      }
      host.disabled = true; helpdeskBox.querySelector('input').disabled = true;
    }
    const manual = s.helpdesk.dns === 'manual' || s.helpdesk.dns === 'error';
    row(manual ? 'warn' : 'ok', h('span', { class: 'mono' }, s.helpdesk.host), manual ? h('p', {}, t('onboard.helpdesk.cname', { host: s.helpdesk.host })) : null);

    const live = step(t('onboard.step.install'));
    if (!s.ready) s.ready = await waitReady(live);
    if (s.ready) live('ok', t('onboard.helpdesk.ready'));
    return s.ready;
  }

  // frugal: polling only runs while this page is open; a server-side job that
  // activates Libredesk and connects the mailboxes would finish it after a close.
  async function waitReady(row) {
    const hid = enc(s.helpdesk.id);
    const started = s.installStarted ??= Date.now();
    const deadline = started + 10 * 60_000;
    let left = location.hash !== viewHash, wake;
    const stop = () => { left = true; clearInterval(timer); wake?.(); };
    const elapsed = h('span', {});
    const tick = () => { elapsed.textContent = t('onboard.helpdesk.elapsed', { time: clock(Date.now() - started) }); };
    tick();
    row('pending', elapsed, h('p', {}, t('onboard.helpdesk.note')));
    const timer = setInterval(() => { if (!root.isConnected) stop(); else tick(); }, 1000);
    window.addEventListener('hashchange', stop);
    try {
      while (!left && root.isConnected && Date.now() < deadline) {
        if (!s.installed) {
          const install = await api('GET', `/helpdesks/${hid}/install`);
          if (left || !root.isConnected) return false;
          if (install.status === 'error') { row('danger', install.message || t('onboard.status.danger')); return false; }
          s.installed = install.status === 'ok';
        }
        if (s.installed && Date.now() < deadline) {
          try {
            const result = await api('POST', `/helpdesks/${hid}/activate`);
            if (left || !root.isConnected) return false;
            if (result.status === 'ready') return true;
          } catch (err) { if (err.status !== 409 || err.code !== 'helpdesk_not_ready') throw err; }
        }
        if (left || !root.isConnected) return false;
        const delay = Math.min(10_000, deadline - Date.now());
        if (delay > 0) await new Promise(resolve => {
          const timer = setTimeout(() => { wake = null; resolve(); }, delay);
          wake = () => { clearTimeout(timer); wake = null; resolve(); };
        });
      }
      if (!left && root.isConnected) row('warn', t('onboard.helpdesk.timeout'));
      return false;
    } catch (err) { row('danger', errorMessage(err)); return false; }
    finally { clearInterval(timer); window.removeEventListener('hashchange', stop); }
  }

  async function connect(step, id, email) {
    const hid = enc(s.helpdesk.id);
    if (email) {
      const row = step(t('onboard.step.agent'));
      try {
        s.agent ??= await api('POST', `/helpdesks/${hid}/agents`, { mode: 'other', name: adminName.value.trim() || email, email, role: 'Admin' });
        adminName.disabled = adminEmail.disabled = true;
        row('ok', h('a', { href: s.agent.url, target: '_blank', rel: 'noopener' }, s.agent.url));
      } catch (err) { row(err.status === 409 ? 'warn' : 'danger', errorMessage(err)); }
      finish();
    }
    if (!s.boxes.size) return;
    for (const box of s.boxes.values()) {
      if (!root.isConnected || location.hash !== viewHash) return;
      const row = step(t('onboard.step.connect', { email: box.email }));
      if (s.connected.has(box.id)) { row('ok'); continue; }
      try { await api('POST', `/domains/${id}/mailboxes/${enc(box.id)}/helpdesk`, { helpdesk_id: s.helpdesk.id }); s.connected.add(box.id); }
      catch (err) { row('warn', t('onboard.connect.failed', { email: box.email }), errorMessage(err)); continue; }
      row('ok');
    }
  }

  function finish() {
    const creds = [
      s.admin?.generated_password ? { account: t('onboard.creds.panel'), user: s.admin.email, password: s.admin.generated_password } : null,
      ...[...s.boxes.values()].filter(box => box.generated_password).map(box => ({ account: t('onboard.creds.mailbox'), user: box.email, password: box.generated_password })),
      s.agent ? { account: t('onboard.creds.helpdesk'), user: s.agent.email, password: s.agent.password, url: s.agent.url } : null,
    ].filter(Boolean);
    const columns = [
      { label: t('onboard.creds.account'), render: r => r.account },
      { label: t('onboard.creds.user'), render: r => h('span', { class: 'mono' }, r.user) },
      { label: t('onboard.creds.password'), render: r => r.password ? h('div', { class: 'row gap-1' }, h('span', { class: 'mono' }, r.password), copyButton(r.password)) : '' },
      { label: t('onboard.creds.url'), render: r => r.url ? h('div', { class: 'row gap-1' }, h('span', { class: 'mono' }, r.url), copyButton(r.url)) : '' },
    ];
    mount(credentials, card(h('div', { class: 'stack gap-2' },
      h('h2', {}, t('onboard.creds')), h('p', { class: 'secret-warning row gap-1' }, icon('alert'), t('onboard.creds.warning')), table({ columns, rows: creds }),
      h('a', { class: 'btn btn-secondary', href: `#/domains/${s.domain.id}` }, h('span', {}, t('onboard.goDomain'))))));
  }

  submitForm(form, async () => {
    inputs.inert = true;
    try { await run(); } finally { inputs.inert = false; }
  }, { validate: () => form.reportValidity(), onError: err => { if (!err.shown) formError(form, err); } });
  return root;
}
