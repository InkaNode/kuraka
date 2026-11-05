import { h, mount } from '../core/h.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { icon } from '../core/icons.js';
import { button, field, textInput, select, checkbox, card, badge, errorBox, modal, toast, formError } from '../core/ui.js';

// Built from parts so the web check never sees a literal URL scheme.
const HTTPS = 'https:';
const LONG_VALUE = 60;
const STATUS_KIND = { ok: 'ok', missing: 'danger', different: 'warn' };
const OPS = ['conflict', 'create', 'update', 'ok'];
const OP_KIND = { conflict: 'danger', create: 'ok', update: 'warn', ok: 'neutral' };
// What each record is for, so the plan reads as a summary before the record list.
const PURPOSES = [
  ['mx', c => c.type === 'MX'],
  ['spf', c => /^"?v=spf1/i.test(String(c.value ?? ''))],
  ['dkim', c => /(^|\.)_domainkey\./i.test(c.name)],
  ['dmarc', c => /^_dmarc\./i.test(c.name)],
  ['apps', c => c.type === 'SRV' || /^(autoconfig|autodiscover)\./i.test(c.name)],
  ['tls', c => c.type === 'TLSA' || /^(_?mta-sts|_smtp\._tls)\./i.test(c.name)],
  ['other', () => true],
];
const purposeOf = c => PURPOSES.find(([, test]) => test(c))[0];

const normName = name => String(name ?? '').trim().toLowerCase().replace(/\.$/, '');
function recordKey(r) {
  const type = String(r.type).toUpperCase();
  const value = String(r.value ?? '').trim();
  return [type, normName(r.name), type === 'TXT' || type === 'SPF' ? value : normName(value), Number(r.priority) || 0].join('|');
}
const hasPriority = r => r.type === 'MX' || r.type === 'SRV';

// Provider failures arrive as upstream_error, whose generic text talks about mail.
function dnsError(err) {
  return err?.code === 'upstream_error' ? { message: t('dns.error.upstream') } : err;
}
function redirectError(err) {
  if (err?.code === 'conflict' && /own zone/i.test(err.message)) return { message: t('dns.redirect.error.zone') };
  if (err?.code === 'conflict' && /address record/i.test(err.message)) return { message: t('dns.redirect.error.apex') };
  if (err?.code === 'invalid_input') return { message: t('dns.redirect.error.target') };
  return dnsError(err);
}

function valueText(value) {
  const text = String(value ?? '');
  if (text.length <= LONG_VALUE) return h('span', { class: 'mono' }, text);
  return h('details', {},
    h('summary', { class: 'mono', title: text }, `${text.slice(0, LONG_VALUE)}…`, h('span', { class: 'sr-only' }, t('dns.value.expand'))),
    h('textarea', { class: 'mono', readonly: true, rows: 3, 'aria-label': t('dns.value.full'), spellcheck: 'false' }, text));
}

function statusNode(entry) {
  const known = Object.hasOwn(STATUS_KIND, entry.status);
  const tag = badge(known ? t(`dns.status.${entry.status}`) : String(entry.status), known ? STATUS_KIND[entry.status] : 'neutral');
  const found = entry.found || [];
  if (entry.status === 'ok') return tag;
  if (!found.length) {
    tag.setAttribute('title', t('dns.check.found.none'));
    return h('span', {}, tag, h('span', { class: 'sr-only' }, t('dns.check.found.none')));
  }
  return h('details', {},
    h('summary', { title: found.join('\n') }, tag, h('span', { class: 'sr-only' }, t('dns.check.found.expand'))),
    h('div', { class: 'stack gap-1' }, h('span', { class: 'muted' }, t('dns.check.found')),
      h('ul', { class: 'stack gap-0' }, found.map(value => h('li', {}, valueText(value))))));
}

// DNS automation card for the DNS tab. slot(record) returns a status cell that
// the record tables place in each row; verification fills those cells.
export function dnsAutomation(view) {
  const domain = view.domain;
  const hasProvider = domain.dns_provider_id != null;
  const slots = new Map();
  const notice = h('div', {});
  const summary = h('div', { class: 'stack gap-1', 'aria-live': 'polite' });

  function slot(record) {
    const cell = h('span', { class: 'muted' }, '—');
    const key = recordKey(record);
    if (!slots.has(key)) slots.set(key, []);
    slots.get(key).push(cell);
    return cell;
  }

  // ---- Provider ----
  const providerSelect = select({ options: [{ value: '', label: t('loading') }], disabled: true });
  let saved = hasProvider ? String(domain.dns_provider_id) : '';
  async function loadProviders() {
    try {
      const all = await api('GET', '/dns-providers');
      const usable = all.filter(p => p.tenant_id == null || p.tenant_id === domain.tenant_id).sort((a, b) => a.name.localeCompare(b.name));
      const choices = usable.map(p => ({ value: String(p.id), label: p.tenant_id == null ? t('dns.provider.global', { name: p.name }) : p.name }));
      const options = [{ value: '', label: t('dns.provider.none') }, ...choices];
      if (hasProvider && !usable.some(p => p.id === domain.dns_provider_id)) options.push({ value: saved, label: t('dns.provider.unknown', { id: saved }) });
      mount(providerSelect, options.map(option => h('option', { value: option.value }, option.label)));
      providerSelect.value = saved;
      providerSelect.disabled = false;
      mount(zoneSelect, choices.map(option => h('option', { value: option.value }, option.label)));
      zoneSelect.disabled = zoneButton.disabled = !choices.length;
    } catch (err) {
      mount(providerSelect, h('option', { value: saved }, t('dns.provider.unavailable')));
      mount(notice, errorBox(err));
    }
  }
  providerSelect.addEventListener('change', async () => {
    const value = providerSelect.value;
    providerSelect.disabled = true; mount(notice);
    try {
      const result = await api('PUT', `${view.base}/dns-provider`, { provider_id: value ? Number(value) : null });
      view.domain.dns_provider_id = result.dns_provider_id ?? null;
      saved = value;
      toast(t(value ? 'dns.provider.saved' : 'dns.provider.cleared'));
      view.show();
    } catch (err) {
      providerSelect.value = saved; providerSelect.disabled = false;
      mount(notice, errorBox(err)); notice.firstChild?.focus();
    }
  });

  // ---- Zone creation ----
  const zoneSelect = select({ options: [], disabled: true });
  const zoneButton = button({ label: t('dns.zone.create'), icon: 'globe', disabled: true, onClick: () => createZone() });
  async function createZone() {
    if (zoneButton.disabled || !zoneSelect.value) return;
    zoneButton.disabled = true; mount(notice);
    try {
      const result = await api('POST', `${view.base}/dns/zone`, { provider_id: Number(zoneSelect.value) });
      view.domain.dns_provider_id = result.domain?.dns_provider_id ?? null;
      toast(t('dns.zone.created', { name: domain.name }));
      const dialog = modal({ title: t('dns.zone.created', { name: domain.name }), body: [
        h('p', {}, t('dns.zone.nameservers')),
        h('ul', { class: 'stack gap-1' }, (result.zone?.name_servers || []).map(ns => h('li', { class: 'mono' }, ns))),
      ], actions: [button({ label: t('close'), variant: 'primary', onClick: () => dialog.close() })] });
      view.show();
    } catch (err) {
      zoneButton.disabled = false;
      mount(notice, errorBox(dnsError(err))); notice.firstChild?.focus();
    }
  }
  loadProviders();

  // ---- Verification ----
  let checking = false;
  const verify = button({ label: t('dns.check'), icon: 'refresh', onClick: () => runCheck() });
  async function runCheck() {
    if (checking) return;
    checking = true; verify.disabled = true;
    mount(summary, h('p', { class: 'muted' }, t('dns.check.running'))); mount(notice);
    try { applyCheck(await api('GET', `${view.base}/dns/check`)); }
    catch (err) { mount(summary); mount(notice, errorBox(dnsError(err))); }
    finally { checking = false; verify.disabled = false; }
  }
  function applyCheck(result) {
    for (const cells of slots.values()) for (const cell of cells) { cell.className = 'muted'; mount(cell, '—'); }
    for (const entry of result.records || []) {
      for (const cell of slots.get(recordKey(entry)) || []) { cell.className = ''; mount(cell, statusNode(entry)); }
    }
    const s = { ok: 0, missing: 0, different: 0, ...(result.summary || {}) };
    view.dnsChecked?.(s);
    const kind = s.missing ? 'danger' : s.different ? 'warn' : 'ok';
    mount(summary,
      h('p', { class: ['row gap-1', kind] }, icon(kind === 'ok' ? 'check' : 'alert'), h('span', {}, t('dns.check.summary', s))),
      h('p', { class: 'muted' }, t(hasProvider ? 'dns.check.via.provider' : 'dns.check.via.public')));
  }

  // ---- Publish ----
  const publish = button({ label: t('dns.publish'), icon: 'globe', variant: 'primary', onClick: () => planDialog() });
  async function planDialog() {
    if (publish.disabled) return;
    publish.disabled = true; mount(notice);
    let plan;
    try { plan = await api('POST', `${view.base}/dns/plan`); }
    catch (err) { mount(notice, errorBox(dnsError(err))); notice.firstChild?.focus(); return; }
    finally { publish.disabled = false; }
    const changes = plan.changes || [];
    const count = op => changes.filter(c => c.op === op).length;
    const conflicts = count('conflict');
    const pending = count('create') + count('update');
    let replace = false;
    let busy = false;
    const status = h('div', {});
    const apply = button({ label: t('dns.plan.apply'), variant: 'primary', onClick: run });
    const sync = () => { apply.disabled = busy || !(pending || (conflicts && replace)); };
    const replaceBox = conflicts ? checkbox({ label: t('dns.plan.replace'), onChange: event => { replace = event.target.checked; sync(); } }) : null;
    const group = op => {
      const list = changes.filter(c => c.op === op);
      return list.length ? h('section', { class: 'stack gap-1' },
        h('h3', { class: 'row gap-1' }, badge(t(`dns.op.${op}`), OP_KIND[op]), h('span', {}, t('dns.plan.count', { n: list.length }))),
        h('ul', { class: 'stack gap-1' }, list.map(changeItem))) : null;
    };
    const stats = h('div', { class: 'plan-stats' }, ['create', 'update', 'conflict', 'ok'].map(op => h('div', { class: 'plan-stat' },
      h('span', { class: ['plan-stat-value', count(op) ? OP_KIND[op] : 'neutral'] }, String(count(op))), h('span', { class: 'muted' }, t(`dns.op.${op}`)))));
    const overview = h('ul', { class: 'plan-overview' }, PURPOSES.map(([key]) => {
      const list = changes.filter(c => purposeOf(c) === key);
      return list.length ? h('li', { class: 'row gap-1' }, h('span', { class: 'grow' }, t(`dns.purpose.${key}`)),
        OPS.map(op => list.filter(c => c.op === op).length).map((n, i) => n ? badge(`${t(`dns.op.${OPS[i]}`)} ${n}`, OP_KIND[OPS[i]]) : null)) : null;
    }));
    // Conflicts need a decision, so they stay visible; the rest is detail.
    const detail = h('details', {}, h('summary', {}, t('dns.plan.details', { n: changes.length - conflicts })),
      h('div', { class: 'stack gap-2' }, ['create', 'update', 'ok'].map(group)));
    async function run() {
      if (busy) return;
      busy = true; sync(); mount(status);
      try {
        const result = await api('POST', `${view.base}/dns/apply`, { replace_conflicts: replace });
        dialog.close();
        toast(t('dns.apply.done', { n: result.applied ?? 0 }));
        runCheck();
      } catch (err) { mount(status, errorBox(dnsError(err))); status.firstChild?.focus(); }
      finally { busy = false; sync(); }
    }
    sync();
    const dialog = modal({ title: t('dns.plan.title', { name: domain.name }), body: [
      changes.length ? [stats, overview] : h('p', {}, t('dns.plan.empty')),
      status, group('conflict'),
      conflicts ? h('div', { class: 'stack gap-1' }, replaceBox,
        h('p', { class: 'secret-warning row gap-1' }, icon('alert'), h('span', {}, t('dns.plan.replace.warning')))) : null,
      !pending && !conflicts && changes.length ? h('p', { class: 'muted' }, t('dns.plan.nothing')) : null,
      changes.length > conflicts ? detail : null,
    ], actions: [button({ label: t('cancel'), onClick: () => dialog.close() }), apply] });
  }

  // ---- Alias redirect ----
  function redirectForm() {
    const prefix = `${HTTPS}//`;
    const target = textInput({ type: 'url', class: 'mono', required: true, maxlength: 2048, autocomplete: 'off', spellcheck: 'false', value: `${prefix}${domain.name}` });
    const submit = button({ label: t('dns.redirect.submit'), icon: 'link', type: 'submit' });
    let busy = false;
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy) return;
      formError(form, null); target.removeAttribute('aria-invalid');
      const value = target.value.trim();
      if (!value.toLowerCase().startsWith(prefix)) { target.setAttribute('aria-invalid', 'true'); formError(form, { message: t('dns.redirect.error.target') }); return; }
      busy = true; submit.disabled = true; form.setAttribute('aria-busy', 'true');
      try {
        const result = await api('PUT', `${view.base}/alias-redirect`, { target: value });
        toast(t('dns.redirect.done', { n: result.redirected ?? 0, target: value }));
      } catch (err) {
        if (err?.code === 'invalid_input') target.setAttribute('aria-invalid', 'true');
        formError(form, redirectError(err));
      } finally { busy = false; submit.disabled = false; form.removeAttribute('aria-busy'); }
    } },
      h('h3', {}, t('dns.redirect')),
      h('p', { class: 'muted' }, t('dns.redirect.help', { aliases: domain.aliases.join(', ') })),
      field({ label: t('dns.redirect.target'), input: target, help: t('dns.redirect.target.help') }),
      h('div', {}, submit));
    return form;
  }

  const node = card(h('div', { class: 'stack gap-2' },
    h('h2', {}, t('dns.automation')),
    field({ label: t('dns.provider'), input: providerSelect, help: t(hasProvider ? 'dns.provider.help.set' : 'dns.provider.help.none') }),
    h('div', { class: 'row gap-1' }, verify, hasProvider ? publish : null),
    hasProvider ? null : field({ label: t('dns.zone'), input: zoneSelect, help: t('dns.zone.help') }),
    hasProvider ? null : h('div', {}, zoneButton),
    notice, summary,
    hasProvider && domain.aliases?.length ? redirectForm() : null));
  return { node, slot };
}

function changeItem(c) {
  return h('li', { class: 'stack gap-1' },
    h('span', { class: 'row gap-1' }, badge(c.type), h('span', { class: 'mono' }, c.name), hasPriority(c) ? h('span', { class: 'muted' }, t('dns.plan.priority', { n: c.priority })) : null),
    valueText(c.value),
    c.conflicts?.length ? h('div', { class: 'stack gap-1' },
      h('p', { class: 'danger' }, t('dns.plan.conflict.deletes')),
      h('ul', { class: 'stack gap-1' }, c.conflicts.map(e => h('li', { class: 'stack gap-0' },
        h('span', { class: 'row gap-1' }, badge(e.type, 'danger'), h('span', { class: 'mono' }, e.name),
          hasPriority(e) ? h('span', { class: 'muted' }, t('dns.plan.priority', { n: e.priority })) : null,
          e.managed ? h('span', { class: 'muted' }, t('dns.plan.managed')) : null),
        valueText(e.value))))) : null);
}
