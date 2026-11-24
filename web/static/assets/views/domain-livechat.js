import { h, mount } from '../core/h.js';
import { t, getLang } from '../core/i18n.js';
import { api } from '../core/api.js';
import { button, field, textInput, select, checkbox, table, errorBox, modal, confirmDialog, toast, formError, submitForm } from '../core/ui.js';
import { helpdeskError } from './domain-helpdesk.js';

const DEFAULT_COLOR = '#A5431E';
const HEX = /^#[0-9a-f]{6}$/i;
let seq = 0;
const uid = prefix => `livechat-${prefix}-${++seq}`;
const chatPath = (view, id) => `${view.base}/livechats${id == null ? '' : `/${encodeURIComponent(id)}`}`;

function chatError(err) {
  if (err?.code === 'livechat_missing') return { message: t('livechat.error.missing') };
  return helpdeskError(err);
}

async function copy(text, message) {
  try { await navigator.clipboard.writeText(text); toast(message); }
  catch { toast(t('error.clipboard'), 'danger'); }
}

function snippet(widget) {
  const base = widget.base_url.replace(/\/+$/, '');
  return `<script>window.LibredeskSettings={baseURL:${JSON.stringify(base)},inboxID:${JSON.stringify(widget.inbox_uuid)}};</script>\n` +
    `<script async src=${JSON.stringify(`${base}/widget.js`)}></script>`;
}

// publicURL: browsers block public sites from loading http, localhost or LAN addresses.
function publicURL(raw) {
  try {
    const u = new URL(raw);
    return u.protocol === 'https:' && u.hostname.includes('.') && u.hostname !== '127.0.0.1';
  } catch { return false; }
}

function codeDialog(chat) {
  if (!publicURL(chat.widget.base_url)) {
    const dialog = modal({ title: t('livechat.code.title', { name: chat.name }), body: errorBox({ message: t('livechat.code.private', { url: chat.widget.base_url }) }),
      actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return dialog;
  }
  const code = snippet(chat.widget);
  const side = t(chat.settings.position === 'left' ? 'livechat.side.left' : 'livechat.side.right');
  const prompt = t('livechat.ai.prompt', { code, side, color: chat.settings.color, origin: chat.widget.base_url.replace(/\/+$/, '') });
  const dialog = modal({ title: t('livechat.code.title', { name: chat.name }), body: [
    h('p', {}, t('livechat.code.intro', { side })),
    field({ label: t('livechat.code.label'), input: h('textarea', { class: 'mono', readonly: true, rows: 4, spellcheck: 'false' }, code) }),
    h('p', { class: 'help' }, t('livechat.ai.help')),
  ], actions: [
    button({ label: t('livechat.ai'), onClick: () => copy(prompt, t('livechat.ai.copied')) }),
    button({ label: t('livechat.code.copy'), icon: 'copy', variant: 'primary', onClick: () => copy(code, t('livechat.code.copied')) }),
  ] });
  return dialog;
}

const parseDomains = text => text.split(/[\s,]+/).filter(Boolean);

// Flow settings of a new chat; missing keys on older chats read as off.
const FLOW_DEFAULTS = {
  prechat_form: { enabled: true, last_name: { shown: false, required: false }, email: { shown: true, required: false }, phone: { shown: true, required: false } },
  menu: { enabled: true, items: [], agent_button_text: '' }, team_id: 0, offline: { mode: 'wait', email: '' }, hide_agent_names: true, inactivity_close: 30,
};
const MAX_ITEMS = 8;
const TEMPLATES = { store: ['orders', 'shipping', 'returns', 'payments'], services: ['prices', 'booking', 'hours'], support: ['technical', 'billing', 'other'] };

function box(label, checked) {
  const el = checkbox({ label, checked: !!checked });
  return { el, input: el.querySelector('input') };
}

function section(title, ...children) {
  const id = uid('section');
  return h('section', { class: 'stack gap-1', 'aria-labelledby': id }, h('h3', { id }, title), children);
}

// formSettings: the name is always asked and required; the other fields can be shown and required.
function formSettings(p) {
  const enabled = box(t('livechat.form.enabled'), p.enabled);
  const rows = {};
  const groups = ['last_name', 'email', 'phone'].map(key => {
    const labelId = uid('field');
    const shown = box(t('livechat.form.shown'), p[key]?.shown);
    const required = box(t('livechat.form.required'), p[key]?.required);
    const sync = () => { required.input.disabled = !shown.input.checked; if (!shown.input.checked) required.input.checked = false; };
    shown.input.addEventListener('change', sync);
    sync();
    rows[key] = { shown: shown.input, required: required.input };
    return h('div', { class: 'row gap-2', role: 'group', 'aria-labelledby': labelId },
      h('span', { id: labelId, class: 'grow' }, t(`livechat.form.${key}`)), shown.el, required.el);
  });
  const fields = h('div', { class: 'stack gap-1' }, h('p', { class: 'help' }, t('livechat.form.name')), groups);
  const sync = () => { fields.hidden = !enabled.input.checked; };
  enabled.input.addEventListener('change', sync);
  sync();
  return {
    node: section(t('livechat.form'), h('p', { class: 'help' }, t('livechat.form.help')), enabled.el, fields),
    value: () => ({ enabled: enabled.input.checked, ...Object.fromEntries(Object.entries(rows).map(([key, row]) =>
      [key, { shown: row.shown.checked, required: row.shown.checked && row.required.checked }])) }),
  };
}

// menuSettings: up to MAX_ITEMS buttons with a short answer, and ready templates.
function menuSettings(m) {
  const enabled = box(t('livechat.menu.enabled'), m.enabled);
  const agentText = textInput({ maxlength: 60, autocomplete: 'off', value: m.agent_button_text || '', placeholder: t('livechat.menu.agent.default') });
  const list = h('div', { class: 'stack gap-2' });
  const status = h('p', { class: 'help', 'aria-live': 'polite' });
  let items = [];
  const add = button({ label: t('livechat.menu.add'), icon: 'plus', onClick: () => { addItem({ label: '', answer: '' }); items.at(-1).label.focus(); } });
  const sync = () => { add.disabled = items.length >= MAX_ITEMS; };
  function addItem(item) {
    const label = textInput({ required: true, maxlength: 60, autocomplete: 'off', value: item.label });
    const answer = h('textarea', { rows: 2, required: true, maxlength: 1000 }, item.answer);
    const entry = { label, answer };
    const node = h('div', { class: 'stack gap-1', role: 'group', 'aria-label': t('livechat.menu.item') },
      field({ label: t('livechat.menu.label'), input: label }),
      field({ label: t('livechat.menu.answer'), input: answer }),
      h('div', { class: 'row' }, button({ label: t('livechat.menu.remove'), icon: 'trash', variant: 'ghost',
        onClick: () => { items = items.filter(x => x !== entry); node.remove(); sync(); add.focus(); } })));
    entry.node = node;
    items.push(entry);
    list.append(node);
    sync();
  }
  function apply(name) {
    list.replaceChildren();
    items = [];
    for (const key of TEMPLATES[name]) addItem({ label: t(`livechat.tpl.${key}`), answer: t(`livechat.tpl.${key}.answer`) });
    status.textContent = t('livechat.menu.applied', { name: t(`livechat.tpl.${name}`) });
  }
  (m.items || []).slice(0, MAX_ITEMS).forEach(addItem);
  sync();
  const templateLabel = uid('templates');
  const body = h('div', { class: 'stack gap-2' },
    h('div', { class: 'stack gap-1', role: 'group', 'aria-labelledby': templateLabel },
      h('span', { class: 'label', id: templateLabel }, t('livechat.menu.templates')),
      h('div', { class: 'row gap-1' }, Object.keys(TEMPLATES).map(name => button({ label: t(`livechat.tpl.${name}`), onClick: () => apply(name) }))),
      h('p', { class: 'help' }, t('livechat.menu.templates.help')), status),
    list, h('div', { class: 'row' }, add),
    field({ label: t('livechat.menu.agent'), input: agentText, help: t('livechat.menu.agent.help') }));
  const syncBody = () => { body.hidden = !enabled.input.checked; };
  enabled.input.addEventListener('change', syncBody);
  syncBody();
  return {
    node: section(t('livechat.menu'), h('p', { class: 'help' }, t('livechat.menu.help')), enabled.el, body),
    agentText,
    value: () => ({ enabled: enabled.input.checked, agent_button_text: agentText.value.trim(),
      items: items.map(x => ({ label: x.label.value.trim(), answer: x.answer.value.trim() })) }),
  };
}

// teamSettings: the select is filled once the helpdesk's teams load; warnings explain what happens to new chats.
// Kuraka never changes an existing team: its assignment also applies to the team's emails.
function teamSettings(view, teamID, chatName) {
  let teams = [];
  let helpdesk = null;
  const team = select({ class: 'grow', options: [{ value: '0', label: t('livechat.team.none') }] });
  const hint = h('p', { class: 'warn', 'aria-live': 'polite' });
  const sync = () => {
    const chosen = teams.find(x => String(x.id) === team.value);
    hint.textContent = !chosen ? t('livechat.team.warn.none')
      : chosen.members === 0 ? t('livechat.team.warn.empty')
      : chosen.conversation_assignment_type === 'Manual' ? t('livechat.team.warn.manual') : '';
  };
  team.addEventListener('change', sync);
  const teamsPath = () => `${view.base}/helpdesks/${encodeURIComponent(helpdesk)}/teams`;
  // Until the teams load, saving keeps the current team.
  let loaded = false;
  async function load(helpdeskID) {
    helpdesk = helpdeskID;
    teams = [];
    loaded = false;
    team.replaceChildren(h('option', { value: '0' }, t('livechat.team.none')));
    try { teams = await api('GET', teamsPath()); }
    catch { hint.textContent = t('livechat.team.error'); return; }
    teams.sort((a, b) => a.name.localeCompare(b.name));
    team.append(...teams.map(x => h('option', { value: String(x.id) }, x.name)));
    team.value = teams.some(x => x.id === teamID) ? String(teamID) : '0';
    loaded = true;
    sync();
  }
  function createTeam() {
    const name = textInput({ required: true, maxlength: 50, autocomplete: 'off', value: t('livechat.team.create.default', { name: chatName() }).slice(0, 50) });
    const form = h('form', { class: 'stack gap-2' }, field({ label: t('livechat.team.create.name'), input: name, help: t('livechat.team.create.help') }),
      button({ label: t('create'), variant: 'primary', type: 'submit' }));
    const dialog = modal({ title: t('livechat.team.create'), body: form, actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
    submitForm(form, async () => {
      const created = await api('POST', teamsPath(), { name: name.value.trim() });
      dialog.close();
      toast(t('livechat.team.created', { name: created.name }));
      teamID = created.id;
      await load(helpdesk);
      team.focus();
    });
  }
  const node = field({ label: t('livechat.team'), input: team, help: t('livechat.team.help') });
  const row = h('div', { class: 'row gap-1' });
  team.replaceWith(row);
  row.append(team, button({ label: t('livechat.team.create'), icon: 'plus', onClick: createTeam }));
  return { input: team, load, node, hint, value: () => (loaded ? Number(team.value) : teamID) };
}

function availabilitySettings(s) {
  const mode = select({ value: s.offline?.mode || '', options: ['', 'wait', 'both', 'email'].map(value => ({ value, label: t(`livechat.offline.mode.${value || 'off'}`) })) });
  const email = textInput({ type: 'email', class: 'mono', maxlength: 254, autocomplete: 'off', spellcheck: 'false', value: s.offline?.email || '' });
  const emailField = field({ label: t('livechat.offline.email'), input: email, help: t('livechat.offline.email.help') });
  const sync = () => { const needed = mode.value === 'both' || mode.value === 'email'; emailField.hidden = !needed; email.required = needed; };
  mode.addEventListener('change', sync);
  sync();
  const hide = box(t('livechat.hideNames'), s.hide_agent_names);
  const inactivity = h('input', { type: 'number', min: 0, max: 10080, step: 1, required: true, value: String(s.inactivity_close ?? 0) });
  return {
    mode, email, inactivity,
    nodes: [field({ label: t('livechat.offline'), input: mode, help: t('livechat.offline.help') }), emailField,
      h('div', { class: 'stack gap-1' }, hide.el, h('p', { class: 'help' }, t('livechat.hideNames.help'))),
      field({ label: t('livechat.inactivity'), input: inactivity, help: t('livechat.inactivity.help') })],
    value: () => ({ offline: { mode: mode.value, email: email.required ? email.value.trim() : '' }, hide_agent_names: hide.input.checked,
      inactivity_close: Number(inactivity.value) || 0 }),
  };
}

async function chatDialog(view, existing, notice, onSaved) {
  mount(notice);
  let chat = null;
  let helpdesks = [];
  try {
    if (existing) chat = await api('GET', chatPath(view, existing.id));
    else helpdesks = (await api('GET', '/helpdesks')).filter(x => x.tenant_id === view.domain.tenant_id && x.status === 'ready').sort((a, b) => a.name.localeCompare(b.name));
  } catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); return; }
  if (!existing && !helpdesks.length) {
    const dialog = modal({ title: t('livechat.new'), body: h('p', {}, t('livechat.noHelpdesk')), actions: [button({ label: t('close'), onClick: () => dialog.close() })] });
    return;
  }

  // CSP frame-ancestors: *.example.com does not cover example.com itself.
  const apex = view.domain.name.toLowerCase().replace(/^www\./, '');
  const s = chat?.settings || { name: view.domain.name, website_url: view.domain.name, color: DEFAULT_COLOR, position: 'right', logo_url: '',
    greeting: t('livechat.greeting.default'), allowed_domains: [apex, `*.${apex}`] };
  const helpdesk = helpdesks.length > 1 ? select({ required: true, options: helpdesks.map(x => ({ value: String(x.id), label: x.name })) }) : null;
  const language = existing ? null : select({ value: getLang() === 'es' ? 'es' : 'en', options: [{ value: 'es', label: 'Español' }, { value: 'en', label: 'English' }] });
  const name = textInput({ required: true, maxlength: 128, autocomplete: 'off', value: s.name });
  const website = textInput({ class: 'mono', required: true, inputmode: 'url', autocomplete: 'off', spellcheck: 'false', value: s.website_url });
  const color = h('input', { type: 'color', value: HEX.test(s.color) ? s.color : DEFAULT_COLOR });
  const theme = select({ value: s.theme || 'light', options: ['light', 'dark', 'system'].map(value => ({ value, label: t(`livechat.theme.${value}`) })) });
  const logo = textInput({ class: 'mono', inputmode: 'url', autocomplete: 'off', spellcheck: 'false', value: s.logo_url || '' });
  const launcherLogo = textInput({ class: 'mono', inputmode: 'url', autocomplete: 'off', spellcheck: 'false', value: s.launcher_logo_url || '' });
  const greeting = h('textarea', { rows: 3, maxlength: 500 }, s.greeting || '');
  const domains = h('textarea', { class: 'mono', rows: 3, spellcheck: 'false', autocomplete: 'off' }, (s.allowed_domains || []).join('\n'));
  const inputs = { name, website_url: website, color, theme, logo_url: logo, launcher_logo_url: launcherLogo, greeting, allowed_domains: domains };

  let position = s.position === 'left' ? 'left' : 'right';
  const positionName = uid('position');
  const positionLabel = uid('position-label');
  const positionGroup = h('div', { class: 'stack gap-1', role: 'radiogroup', 'aria-labelledby': positionLabel },
    h('span', { class: 'label', id: positionLabel }, t('livechat.position')),
    h('div', { class: 'row gap-2' }, ['left', 'right'].map(side => {
      const id = uid('radio');
      return h('label', { class: 'checkbox row gap-1', for: id }, h('input', { id, type: 'radio', name: positionName, value: side, checked: side === position,
        onchange: () => { position = side; } }), h('span', {}, t(`livechat.position.${side}`)));
    })));

  const anySite = h('p', { class: 'warn', 'aria-live': 'polite' });
  const syncAnySite = () => { anySite.textContent = parseDomains(domains.value).length ? '' : t('livechat.domains.any'); };
  domains.addEventListener('input', syncAnySite);
  syncAnySite();
  const domainsField = field({ label: t('livechat.domains'), input: domains, help: t('livechat.domains.help') });
  domainsField.append(anySite);

  const flow = { ...FLOW_DEFAULTS, ...(chat ? chat.settings : {}) };
  const prechat = formSettings(flow.prechat_form || FLOW_DEFAULTS.prechat_form);
  const menu = menuSettings(flow.menu || FLOW_DEFAULTS.menu);
  const team = teamSettings(view, flow.team_id || 0, () => name.value.trim());
  const availability = availabilitySettings(flow);
  team.load(existing ? chat.helpdesk_id : helpdesk?.value ?? helpdesks[0].id);
  helpdesk?.addEventListener('change', () => team.load(helpdesk.value));
  Object.assign(inputs, { menu: menu.agentText, team_id: team.input, offline: availability.mode, offline_email: availability.email, inactivity_close: availability.inactivity });

  const form = h('form', { class: 'stack gap-2' },
    helpdesk ? field({ label: t('helpdesk.select'), input: helpdesk }) : null,
    field({ label: t('livechat.name'), input: name, help: t('livechat.name.help') }),
    field({ label: t('livechat.website'), input: website, help: t('livechat.website.help') }),
    language ? field({ label: t('livechat.language'), input: language, help: t('livechat.language.help') }) : null,
    field({ label: t('livechat.color'), input: color }),
    field({ label: t('livechat.theme'), input: theme }),
    positionGroup,
    field({ label: t('livechat.logo'), input: logo, help: t(existing ? 'livechat.logo.help.edit' : 'livechat.logo.help') }),
    field({ label: t('livechat.launcherLogo'), input: launcherLogo, help: t('livechat.launcherLogo.help') }),
    field({ label: t('livechat.greeting'), input: greeting, help: t('livechat.greeting.help') }),
    domainsField,
    prechat.node,
    menu.node,
    section(t('livechat.flow'), team.node, team.hint, availability.nodes),
    button({ label: t(existing ? 'save' : 'create'), variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    for (const el of form.querySelectorAll('[aria-invalid]')) el.removeAttribute('aria-invalid');
    const values = { name: name.value.trim(), website_url: website.value.trim(), color: color.value, theme: theme.value, position,
      logo_url: logo.value.trim(), launcher_logo_url: launcherLogo.value.trim(), greeting: greeting.value.trim(), allowed_domains: parseDomains(domains.value),
      prechat_form: prechat.value(), menu: menu.value(), team_id: team.value(), ...availability.value() };
    const result = existing
      ? await api('PUT', chatPath(view, existing.id), values)
      : await api('POST', chatPath(view), { ...values, helpdesk_id: Number(helpdesk?.value ?? helpdesks[0].id), language: language.value });
    dialog.close();
    toast(t(existing ? 'livechat.updated' : 'livechat.created', { name: result.name }));
    onSaved();
    if (!existing) codeDialog(result);
  }, { onError: err => {
    const key = err?.code === 'invalid_input' ? /^Invalid (\w+)$/.exec(err.message || '')?.[1] : null;
    if (!inputs[key]) { formError(form, chatError(err)); return; }
    formError(form, { message: t(`livechat.invalid.${key}`) });
    inputs[key].setAttribute('aria-invalid', 'true');
    inputs[key].focus();
  } });
  const dialog = modal({ title: existing ? t('livechat.edit.title', { name: existing.name }) : t('livechat.new'), body: form,
    actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
}

// liveChats returns the "add" button for the mailboxes toolbar and the list,
// which stays empty until the domain has a live chat.
export function liveChats(view, mine, notice) {
  const node = h('div', {});
  const reload = () => view.show();
  const add = () => button({ label: t('livechat.new'), icon: 'message', onClick: () => chatDialog(view, null, notice, reload) });

  async function showCode(chat) {
    mount(notice);
    try { codeDialog(await api('GET', chatPath(view, chat.id))); }
    catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); }
  }
  async function remove(chat) {
    const ok = await confirmDialog({ title: t('livechat.delete.title', { name: chat.name }), message: t('livechat.delete.message', { website: chat.website_url }),
      confirmLabel: t('delete'), danger: true });
    if (!ok) return;
    mount(notice);
    try {
      await api('DELETE', chatPath(view, chat.id));
      toast(t('livechat.deleted', { name: chat.name }));
      reload();
    } catch (err) { mount(notice, errorBox(chatError(err))); notice.firstChild?.focus(); }
  }

  api('GET', chatPath(view)).then(chats => {
    if (mine !== view.current() || !chats.length) return;
    chats.sort((a, b) => a.name.localeCompare(b.name));
    mount(node, h('section', { class: 'stack gap-1' }, h('h2', {}, t('livechat.list')), table({ rows: chats, columns: [
      { label: t('livechat.name'), render: c => c.name },
      { label: t('livechat.website'), render: c => h('span', { class: 'mono' }, c.website_url) },
      { label: t('mailbox.col.actions'), render: c => h('div', { class: 'row gap-1' },
        button({ label: t('livechat.code'), icon: 'code', variant: 'ghost', ariaLabel: t('livechat.code.title', { name: c.name }), onClick: () => showCode(c) }),
        button({ icon: 'edit', variant: 'ghost', ariaLabel: t('livechat.edit.title', { name: c.name }), onClick: () => chatDialog(view, c, notice, reload) }),
        button({ icon: 'trash', variant: 'ghost', ariaLabel: t('livechat.delete.title', { name: c.name }), onClick: () => remove(c) })) },
    ] })));
  }).catch(err => { if (mine === view.current()) mount(node, errorBox(err)); });

  return { add, node };
}
