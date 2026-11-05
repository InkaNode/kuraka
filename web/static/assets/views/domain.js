import '../i18n/domains.js';
import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api, errorMessage } from '../core/api.js';
import { icon } from '../core/icons.js';
import { button, field, textInput, select, checkbox, card, pageHeader, table, badge, chip, copyButton, spinner, errorBox, emptyState, tabs, modal, confirmDialog, secretOnce, toast, formError } from '../core/ui.js';
import { splitNames } from './domains.js';
import { dnsAutomation } from './domain-dns.js';
import { helpdeskBadges, helpdeskDialog } from './domain-helpdesk.js';
import { showMigrations, migrationDialog } from './domain-migrations.js';

const TABS = ['mailboxes', 'aliases', 'dns', 'migrations'];
const LONG_VALUE = 60;
let seq = 0;
const uid = prefix => `${prefix}-${++seq}`;

export async function domainView(ctx) {
  const id = ctx.params.id;
  const base = `/domains/${encodeURIComponent(id)}`;
  const back = h('a', { href: '#/domains' }, t('domain.back'));
  let domain;
  try { domain = await api('GET', base); }
  catch (err) { return h('div', { class: 'stack gap-2' }, back, pageHeader(t('nav.domain')), errorBox(err)); }

  let active = TABS.includes(ctx.query.get('tab')) ? ctx.query.get('tab') : 'mailboxes';
  let token = 0;
  const panelId = uid('panel');
  const tabId = tab => `${panelId}-${tab}`;
  const panel = h('div', { id: panelId, role: 'tabpanel', tabindex: 0, class: 'stack gap-2' });
  const tabBar = tabs({ items: TABS.map(tab => ({ id: tab, label: t(`domain.tab.${tab}`) })), active, onChange: tab => {
    active = tab;
    // replaceState keeps the tab in the URL without a hashchange re-render.
    history.replaceState(history.state, '', `#/domains/${encodeURIComponent(id)}?tab=${tab}`);
    show();
  } });
  [...tabBar.children].forEach((el, index) => { el.id = tabId(TABS[index]); el.setAttribute('aria-controls', panelId); });

  // Shared state across tabs.
  let mailboxes = null;
  let tenantDomains = null;
  let dnsStale = false;
  const current = () => token;

  // Context shared with the DNS, helpdesk and migration modules.
  const view = {
    get domain() { return domain; },
    base, panel, current, show,
    cache: { links: new Map() },
    async loadMailboxes() { if (!mailboxes) await loadMailboxes(); return mailboxes; },
    dnsChecked(s) { guide.update({ dns: !s.missing && !s.different }); },
    goTab(tab) {
      if (active === tab) show();
      else tabBar.children[TABS.indexOf(tab)]?.click();
    },
  };

  function show() {
    const mine = ++token;
    panel.setAttribute('aria-labelledby', tabId(active));
    ({ mailboxes: showMailboxes, aliases: showAliases, dns: showDNS, migrations: m => showMigrations(view, m) })[active](mine);
  }

  // ---- Mailboxes -------------------------------------------------------
  async function loadMailboxes() { mailboxes = await api('GET', `${base}/mailboxes`); guide.update({ boxes: mailboxes.length > 0 }); }
  async function showMailboxes(mine) {
    if (!mailboxes) {
      mount(panel, spinner());
      try { await loadMailboxes(); }
      catch (err) { if (mine === current()) mount(panel, errorBox(err), button({ label: t('domain.retry'), icon: 'refresh', onClick: show })); return; }
      if (mine !== current()) return;
    }
    const notice = h('div', {});
    const create = button({ label: t('mailbox.new'), icon: 'plus', variant: 'primary', onClick: () => mailboxDialog(null, notice) });
    const rows = [...mailboxes].sort((a, b) => a.email.localeCompare(b.email));
    const toolbar = h('div', { class: 'row gap-2' }, h('p', { class: 'muted grow' }, t('mailbox.count', { n: rows.length })), create);
    if (!rows.length) {
      mount(panel, notice, emptyState({ icon: 'inbox', title: t('mailbox.empty.title'), text: t('mailbox.empty.text'),
        action: button({ label: t('mailbox.new'), icon: 'plus', variant: 'primary', onClick: () => mailboxDialog(null, notice) }) }));
      return;
    }
    const links = helpdeskBadges(view, rows, mine);
    mount(panel, toolbar, notice, table({ rows, columns: [
      { label: t('mailbox.col.address'), render: m => h('div', { class: 'row gap-1' }, h('span', { class: 'mono' }, m.email), links.slot(m)) },
      { label: t('mailbox.col.name'), render: m => m.display_name || h('span', { class: 'muted' }, '—') },
      { label: t('mailbox.col.type'), render: m => badge(t(`mailbox.kind.${m.kind}`), m.kind === 'shared' ? 'warn' : 'neutral') },
      { label: t('mailbox.col.aliases'), render: m => m.aliases?.length ? h('div', { class: 'row gap-1' }, m.aliases.map(a => chip(a.email))) : h('span', { class: 'muted' }, '—') },
      { label: t('mailbox.col.quota'), render: m => quotaText(m) },
      { label: t('mailbox.col.actions'), render: m => h('div', { class: 'row gap-1' },
        button({ label: t('helpdesk.action'), icon: 'link', variant: 'ghost', ariaLabel: t('helpdesk.action.label', { email: m.email }), onClick: () => helpdeskDialog(view, m, () => links.refresh(m.id)) }),
        m.kind === 'user' ? button({ label: t('migration.action'), icon: 'download', variant: 'ghost', ariaLabel: t('migration.action.label', { email: m.email }), onClick: () => migrationDialog(view, m.id, () => view.goTab('migrations')) }) : null,
        button({ icon: 'edit', variant: 'ghost', ariaLabel: t('mailbox.edit.label', { email: m.email }), onClick: () => mailboxDialog(m, notice) }),
        button({ icon: 'trash', variant: 'ghost', ariaLabel: t('mailbox.delete.label', { email: m.email }), onClick: () => deleteMailbox(m, notice) })) },
    ] }));
    links.start();
  }
  async function refreshMailboxes(notice) {
    try { await loadMailboxes(); }
    catch (err) { mount(notice, errorBox(err)); return; }
    if (active === 'mailboxes') show();
  }
  async function loadTenantDomains() {
    if (!tenantDomains) {
      const all = await api('GET', '/domains');
      tenantDomains = all.filter(d => d.tenant_id === domain.tenant_id).sort((a, b) => a.name.localeCompare(b.name));
    }
    return tenantDomains;
  }

  async function mailboxDialog(existing, notice) {
    let domains;
    try { domains = await loadTenantDomains(); }
    catch (err) { mount(notice, errorBox(err)); notice.firstChild?.focus(); return; }
    mount(notice);
    const editing = Boolean(existing);
    const shared = editing && existing.kind === 'shared';
    let helpdesk;
    if (!editing) {
      try { helpdesk = (await api('GET', '/helpdesks')).find(row => row.tenant_id === domain.tenant_id && row.status === 'ready'); }
      catch { /* Mailbox creation remains available if integrations cannot be loaded. */ }
    }
    const connect = helpdesk ? checkbox({ label: t('mailbox.helpdesk'), checked: true }) : null;
    let busy = false;
    const formId = uid('mailbox');

    const local = textInput({ class: 'mono grow', required: true, maxlength: 64, autocomplete: 'off', spellcheck: 'false', pattern: '[a-zA-Z0-9][a-zA-Z0-9._+\\-]{0,63}' });
    const localField = field({ label: t('mailbox.local'), input: local, help: t('mailbox.local.help', { domain: domain.name }) });
    const localRow = h('div', { class: 'row gap-1' });
    local.replaceWith(localRow);
    localRow.append(local, h('span', { class: 'mono muted', 'aria-hidden': 'true' }, `@${domain.name}`));

    const displayName = textInput({ maxlength: 256, autocomplete: 'off', value: existing?.display_name || '' });
    const sharedBox = checkbox({ label: t('mailbox.shared'), onChange: () => syncPassword() });
    const sharedInput = sharedBox.querySelector('input');
    const sharedHelp = h('p', { class: 'help' }, t('mailbox.shared.help'));

    // Password: create = auto|manual, edit = keep|auto|manual.
    const password = textInput({ type: 'password', autocomplete: 'new-password', minlength: 12, maxlength: 256 });
    const passwordField = field({ label: t(editing ? 'mailbox.password.new' : 'mailbox.password'), input: password, help: t('mailbox.password.help') });
    const modes = editing ? ['keep', 'auto', 'manual'] : ['auto', 'manual'];
    let passwordMode = modes[0];
    const radioName = uid('pw');
    const pwLabel = uid('pw-label');
    const passwordGroup = h('div', { class: 'stack gap-1', role: 'radiogroup', 'aria-labelledby': pwLabel },
      h('span', { class: 'label', id: pwLabel }, t('mailbox.password')),
      h('div', { class: 'row gap-2' }, modes.map(mode => {
        const rid = uid('radio');
        return h('label', { class: 'checkbox row gap-1', for: rid }, h('input', { id: rid, type: 'radio', name: radioName, value: mode, checked: mode === passwordMode,
          onchange: () => { passwordMode = mode; syncPassword(); } }), h('span', {}, t(`mailbox.password.${mode}`)));
      })));
    function syncPassword() {
      const hide = shared || sharedInput.checked;
      passwordGroup.hidden = hide;
      // Groups cannot sign in, so they cannot get the helpdesk app password.
      if (connect) connect.hidden = hide;
      passwordField.hidden = hide || passwordMode !== 'manual';
      password.required = !passwordField.hidden;
    }

    const quota = textInput({ type: 'number', min: 0, step: 1, inputmode: 'numeric', value: existing?.quota_mb ? String(existing.quota_mb) : '' });

    // Aliases editor: rows of local part + tenant domain.
    const aliasRows = h('div', { class: 'stack gap-1' });
    const aliasError = h('p', { class: 'error', id: uid('alias-error'), hidden: true });
    const aliasLabel = uid('aliases-label');
    function addAliasRow(alias = {}) {
      const name = textInput({ class: 'mono grow', required: true, maxlength: 64, autocomplete: 'off', spellcheck: 'false', value: alias.name || '', 'aria-label': t('mailbox.alias.local'), pattern: '[a-zA-Z0-9][a-zA-Z0-9._+\\-]{0,63}' });
      const target = select({ class: 'grow', 'aria-label': t('mailbox.alias.domain'), value: String(alias.domain_id ?? domain.id),
        options: domains.map(d => ({ value: String(d.id), label: `@${d.name}` })) });
      const row = h('div', { class: 'row gap-1', 'data-alias-row': '' }, name, target,
        button({ icon: 'trash', variant: 'ghost', ariaLabel: t('mailbox.alias.remove'), onClick: () => { const next = row.nextElementSibling || row.previousElementSibling; row.remove(); (next?.querySelector('input') || addAlias).focus(); } }));
      row.aliasValue = () => ({ name: name.value.trim().toLowerCase(), domain_id: Number(target.value) });
      aliasRows.append(row);
      return name;
    }
    (existing?.aliases || []).forEach(alias => addAliasRow(alias));
    const addAlias = button({ label: t('mailbox.alias.add'), icon: 'plus', onClick: () => addAliasRow().focus() });
    const aliasSection = h('div', { class: 'stack gap-1', role: 'group', 'aria-labelledby': aliasLabel },
      h('span', { class: 'label', id: aliasLabel }, t('mailbox.col.aliases')),
      h('p', { class: 'help' }, t('mailbox.aliases.help')), aliasError, aliasRows, h('div', {}, addAlias));

    function markFields(err) {
      for (const input of form.querySelectorAll('[aria-invalid]')) input.removeAttribute('aria-invalid');
      aliasError.hidden = true;
      if (!err) return;
      const msg = err.message || '';
      if (err.code === 'foreign_alias' || /aliases/i.test(msg)) {
        aliasError.textContent = err.code === 'foreign_alias' ? errorMessage(err) : t('mailbox.alias.invalid');
        aliasError.hidden = false;
        for (const el of aliasRows.querySelectorAll('input, select')) {
          el.setAttribute('aria-invalid', 'true'); el.setAttribute('aria-describedby', aliasError.id);
        }
      } else if (/password/i.test(msg)) password.setAttribute('aria-invalid', 'true');
      else if (/quota/i.test(msg)) quota.setAttribute('aria-invalid', 'true');
      else if (/Invalid name/i.test(msg) && !editing) local.setAttribute('aria-invalid', 'true');
    }

    const submit = button({ label: t(editing ? 'save' : 'create'), variant: 'primary', type: 'submit' });
    submit.setAttribute('form', formId);
    const form = h('form', { id: formId, class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy) return;
      busy = true; submit.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null); markFields(null);
      const quotaValue = quota.value.trim() === '' ? 0 : Number(quota.value);
      const aliases = [...aliasRows.querySelectorAll('[data-alias-row]')].map(row => row.aliasValue());
      const body = { display_name: displayName.value.trim(), quota_mb: quotaValue, aliases };
      const isShared = shared || sharedInput.checked;
      if (!isShared && passwordMode !== 'keep') body.password = passwordMode === 'manual' ? password.value : '';
      try {
        let result;
        if (editing) result = await api('PATCH', `${base}/mailboxes/${encodeURIComponent(existing.id)}`, body);
        else result = await api('POST', `${base}/mailboxes`, { ...body, name: local.value.trim().toLowerCase(), shared: isShared });
        password.value = '';
        dialog.close();
        toast(t(editing ? 'mailbox.updated' : 'mailbox.created', { email: result.email }));
        if (result.generated_password) secretOnce({ title: t('mailbox.secret.title', { email: result.email }), label: t('mailbox.password'), secret: result.generated_password });
        if (!isShared && connect?.querySelector('input').checked) {
          try { await api('POST', `${base}/mailboxes/${encodeURIComponent(result.id)}/helpdesk`, { helpdesk_id: helpdesk.id }); }
          catch { toast(t('mailbox.helpdeskFailed', { email: result.email }), 'warn'); }
        }
        await refreshMailboxes(notice);
      } catch (err) {
        formError(form, err); markFields(err);
        if (err.code === 'foreign_alias') aliasRows.querySelector('select')?.focus();
      } finally { busy = false; submit.disabled = false; form.removeAttribute('aria-busy'); }
    } },
      editing ? h('p', {}, h('span', { class: 'mono' }, existing.email), ' ', badge(t(`mailbox.kind.${existing.kind}`), shared ? 'warn' : 'neutral')) : localField,
      field({ label: t('mailbox.col.name'), input: displayName }),
      editing ? null : h('div', { class: 'stack gap-1' }, sharedBox, sharedHelp),
      passwordGroup, passwordField,
      field({ label: t('mailbox.quota'), input: quota, help: t('mailbox.quota.help') }),
      aliasSection, connect);
    syncPassword();
    const dialog = modal({ title: editing ? t('mailbox.edit.title', { email: existing.email }) : t('mailbox.new'), body: form,
      actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), submit] });
  }

  function deleteMailbox(m, notice) {
    let busy = false;
    const formId = uid('delete');
    const confirmInput = textInput({ class: 'mono', autocomplete: 'off', spellcheck: 'false' });
    const submit = button({ label: t('delete'), variant: 'danger', type: 'submit', disabled: true });
    submit.setAttribute('form', formId);
    confirmInput.addEventListener('input', () => { submit.disabled = busy || confirmInput.value.trim().toLowerCase() !== m.email.toLowerCase(); });
    const form = h('form', { id: formId, class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || submit.disabled) return;
      busy = true; submit.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        await api('DELETE', `${base}/mailboxes/${encodeURIComponent(m.id)}`);
        dialog.close();
        toast(t('mailbox.deleted', { email: m.email }));
        await refreshMailboxes(notice);
      } catch (err) { formError(form, err); submit.disabled = false; }
      finally { busy = false; form.removeAttribute('aria-busy'); }
    } },
      h('p', { class: 'danger' }, t('mailbox.delete.warning')),
      field({ label: t('mailbox.delete.type', { email: m.email }), input: confirmInput }));
    const dialog = modal({ title: t('mailbox.delete.title'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), submit] });
  }

  // ---- Alias domains ---------------------------------------------------
  function showAliases() {
    const input = textInput({ class: 'mono grow', autocomplete: 'off', spellcheck: 'false', placeholder: t('aliasdomains.placeholder') });
    let busy = false;
    const add = button({ label: t('aliasdomains.add'), icon: 'plus', variant: 'primary', type: 'submit' });
    async function save(next, message) {
      if (busy) return false;
      busy = true; add.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        const result = await api('PUT', `${base}/aliases`, { aliases: next });
        domain.aliases = result.aliases; dnsStale = true;
        toast(message);
        return true;
      } catch (err) { formError(form, err); return false; }
      finally { busy = false; add.disabled = false; form.removeAttribute('aria-busy'); }
    }
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      const names = splitNames(input.value).filter(name => !domain.aliases.includes(name));
      if (!names.length) { input.focus(); return; }
      if (await save([...domain.aliases, ...names], t('aliasdomains.added', { names: names.join(', ') }))) { input.value = ''; render(); input.focus(); }
    } }, field({ label: t('aliasdomains.input'), input, help: t('aliasdomains.input.help') }), h('div', {}, add));
    const chips = h('div', { class: 'row gap-1', role: 'list', 'aria-label': t('aliasdomains.current') });
    function render() {
      mount(chips, domain.aliases.length
        ? domain.aliases.map(name => h('span', { role: 'listitem' }, chip(name, { onRemove: async () => {
          const ok = await confirmDialog({ title: t('aliasdomains.remove.title', { name }), message: t('aliasdomains.remove.message', { name, primary: domain.name }), confirmLabel: t('aliasdomains.remove'), danger: true });
          if (ok && await save(domain.aliases.filter(alias => alias !== name), t('aliasdomains.removed', { name }))) { render(); input.focus(); }
        } })))
        : h('p', { class: 'muted' }, t('aliasdomains.none')));
    }
    render();
    mount(panel, card(h('div', { class: 'stack gap-2' },
      h('h2', {}, t('domain.tab.aliases')),
      h('p', { class: 'muted' }, t('aliasdomains.intro', { primary: domain.name })),
      chips, form)));
  }

  // ---- DNS -------------------------------------------------------------
  async function showDNS(mine) {
    if (dnsStale) {
      mount(panel, spinner());
      try { domain = await api('GET', base); dnsStale = false; }
      catch (err) { if (mine === current()) mount(panel, errorBox(err), button({ label: t('domain.retry'), icon: 'refresh', onClick: show })); return; }
      if (mine !== current()) return;
    }
    const zones = [{ name: domain.name, records: domain.dns_records || [], primary: true },
      ...Object.entries(domain.alias_dns_records || {}).sort(([a], [b]) => a.localeCompare(b)).map(([name, records]) => ({ name, records }))];
    const zoneText = zones.map(zone => [`; ${zone.name}`, ...zone.records.map(bindLine)].join('\n')).join('\n\n') + '\n';
    const automation = dnsAutomation(view);
    const copyAll = button({ label: t('dns.copyZone'), icon: 'copy', onClick: async () => {
      try { await navigator.clipboard.writeText(zoneText); toast(t('dns.copiedZone')); }
      catch { toast(t('error.clipboard'), 'danger'); }
    } });
    mount(panel,
      h('div', { class: 'row gap-2' }, h('p', { class: 'grow' }, t('dns.intro')), copyAll),
      automation.node,
      zones.map(zone => card(h('div', { class: 'stack gap-2' },
        h('h2', { class: 'row gap-1' }, h('span', { class: 'mono' }, zone.name), badge(t(zone.primary ? 'dns.primary' : 'dns.alias'), zone.primary ? 'ok' : 'neutral')),
        recordsTable(zone.records, automation.slot)))),
      card(h('div', { class: 'stack gap-2' }, h('h2', {}, t('dns.dkim')), h('p', { class: 'muted' }, t('dns.dkim.intro')),
        table({ rows: domain.dkim || [], empty: emptyState({ icon: 'key', title: t('dns.dkim.empty') }), columns: [
          { label: t('dns.dkim.selector'), render: k => h('span', { class: 'mono' }, k.selector) },
          { label: t('dns.dkim.algorithm'), render: k => h('span', { class: 'mono' }, k.algorithm) },
          { label: t('dns.dkim.stage'), render: k => stageBadge(k.stage) },
        ] }))));
  }

  // ---- Setup guide -----------------------------------------------------
  // Shown until DNS checks out and the domain has a mailbox. Loaded once per
  // visit (a fresh DNS check on every tab switch would hit the provider again),
  // then kept current by the DNS check and the mailbox list.
  function setupGuide() {
    const steps = h('ol', { class: 'setup-steps' });
    const node = card(h('div', { class: 'stack gap-2' },
      h('div', { class: 'row gap-2' }, h('h2', { class: 'grow' }, t('setup.title')), h('a', { href: '#/docs?s=domains' }, t('setup.docs'))),
      h('p', { class: 'muted' }, t('setup.intro')), steps));
    node.hidden = true;
    const step = (done, title, text, action) => h('li', { class: 'row gap-2' },
      h('span', { class: done ? 'ok' : 'warn' }, icon(done ? 'check' : 'alert')),
      h('div', { class: 'stack grow' }, h('strong', {}, title), h('span', { class: 'muted' }, text)),
      done ? badge(t('setup.done'), 'ok') : action);
    const state = { dns: null, boxes: null };
    function update(patch) {
      Object.assign(state, patch);
      node.hidden = state.dns == null || state.boxes == null || (state.dns && state.boxes);
      if (node.hidden) return;
      const hasProvider = domain.dns_provider_id != null;
      // DNS first: an MX already pointing elsewhere is a conflict that is only
      // replaced on request (dns.plan.replace.warning), so publishing early moves no mail.
      mount(steps,
        step(state.dns, t('setup.dns'), t(hasProvider ? 'setup.dns.provider' : 'setup.dns.manual'),
          h('div', { class: 'row gap-1' },
            hasProvider ? null : h('a', { class: 'btn btn-ghost', href: '#/integrations' }, t('setup.dns.connect')),
            button({ label: t('setup.dns.go'), onClick: () => view.goTab('dns') }))),
        step(state.boxes, t('setup.mailbox'), t('setup.mailbox.text'), button({ label: t('setup.mailbox.go'), onClick: () => view.goTab('mailboxes') })));
    }
    Promise.all([
      api('GET', `${base}/dns/check`).then(r => r.summary).catch(() => null),
      view.loadMailboxes().catch(() => null),
    ]).then(([dns, boxes]) => update({ dns: dns != null && !dns.missing && !dns.different, boxes: boxes?.length > 0 }));
    return { node, update };
  }

  const guide = setupGuide();
  const root = h('div', { class: 'stack gap-2' }, back, pageHeader(h('span', { class: 'mono' }, domain.name)), guide.node, tabBar, panel);
  show();
  return root;
}

function quotaText(m) {
  const used = formatMB(m.used_mb || 0);
  return m.quota_mb ? t('mailbox.quota.value', { used, total: formatMB(m.quota_mb) }) : t('mailbox.quota.unlimited', { used });
}
const formatMB = n => new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(n);

function recordsTable(records, statusSlot) {
  return table({ rows: records, empty: emptyState({ icon: 'globe', title: t('dns.empty') }), columns: [
    statusSlot ? { label: t('dns.status'), render: r => statusSlot(r) } : null,
    { label: t('dns.type'), render: r => badge(r.type) },
    { label: t('dns.name'), render: r => h('span', { class: 'mono' }, r.name) },
    { label: t('dns.value'), render: r => h('div', { class: 'row gap-1' }, longValue(r.value), copyButton(r.value)) },
    { label: t('dns.priority'), render: r => (r.type === 'MX' || r.type === 'SRV') ? String(r.priority) : h('span', { class: 'muted' }, '—') },
  ].filter(Boolean) });
}

// Long values (DKIM keys) collapse to a prefix; the full text stays reachable
// in a disclosure and through the copy button.
function longValue(value) {
  if (value.length <= LONG_VALUE) return h('span', { class: 'mono' }, value);
  return h('details', { class: 'grow' },
    h('summary', { class: 'mono', title: value }, `${value.slice(0, LONG_VALUE)}…`, h('span', { class: 'sr-only' }, t('dns.value.expand'))),
    h('textarea', { class: 'mono', readonly: true, rows: 4, 'aria-label': t('dns.value.full'), spellcheck: 'false' }, value));
}

function stageBadge(stage = '') {
  const s = stage.toLowerCase();
  const kind = s === 'active' ? 'ok' : s === 'pending' || s === 'staged' ? 'warn' : s === 'revoked' || s === 'error' ? 'danger' : 'neutral';
  const known = ['active', 'pending', 'staged', 'retiring', 'retired', 'revoked'].includes(s);
  return badge(known ? t(`dns.stage.${s}`) : stage || '—', kind);
}

// BIND presentation: absolute names, TXT split into <=255-byte quoted strings.
function bindLine(r) {
  const fqdn = name => `${name.replace(/\.$/, '')}.`;
  let data;
  switch (r.type) {
    case 'TXT': case 'SPF': data = txtStrings(r.value); break;
    case 'MX': data = `${r.priority} ${fqdn(r.value)}`; break;
    case 'SRV': {
      const parts = r.value.split(/\s+/);
      if (parts.length === 3) parts[2] = fqdn(parts[2]);
      data = `${r.priority} ${parts.join(' ')}`; break;
    }
    case 'CNAME': case 'NS': case 'PTR': data = fqdn(r.value); break;
    default: data = r.value;
  }
  return `${fqdn(r.name)}\tIN\t${r.type}\t${data}`;
}
function txtStrings(value) {
  const encoder = new TextEncoder();
  const chunks = [];
  let chunk = '';
  let size = 0;
  for (const ch of value) {
    const n = encoder.encode(ch).length;
    if (size + n > 255) { chunks.push(chunk); chunk = ''; size = 0; }
    chunk += ch; size += n;
  }
  chunks.push(chunk);
  return chunks.map(part => `"${part.replace(/[\\"]/g, '\\$&')}"`).join(' ');
}
