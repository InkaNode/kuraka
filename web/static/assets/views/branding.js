import '../i18n/branding.js';
import { h, mount } from '../core/h.js';
import { api, errorMessage } from '../core/api.js';
import { t } from '../core/i18n.js';
import { icon } from '../core/icons.js';
import { loadBranding } from '../core/branding.js';
import { button, card, checkbox, field, textInput, select, pageHeader, tabs, spinner, errorBox, toast, submitForm } from '../core/ui.js';

const APPS = ['default', 'kuraka', 'stalwart', 'desk'];
const FIELDS = ['name', 'logo', 'icon', 'banner', 'color', 'gradient'];
const DESK_FIELDS = ['name', 'logo', 'icon'];
const IMAGES = ['logo', 'icon', 'banner'];
const ACCEPT = 'image/png,image/jpeg,image/webp,image/gif,image/svg+xml';
const MAX_IMAGE = 512 * 1024;
// Starting values for colour pickers when nothing is inherited.
const COLORS = ['#a9502f', '#5c2a18'];
let seq = 0;
const uid = prefix => `${prefix}-${++seq}`;
const assetURL = id => `/branding/assets/${encodeURIComponent(id)}`;
const readFile = file => new Promise((resolve, reject) => {
  const reader = new FileReader();
  reader.onload = () => resolve(reader.result); reader.onerror = () => reject(reader.error);
  reader.readAsDataURL(file);
});

export async function brandingView() {
  const tenants = await api('GET', '/tenants');
  let tenant = '';
  let data = null;
  let active = 'default';
  let token = 0;
  const panelId = uid('panel');
  const notice = h('div');
  const content = h('div');
  const scope = select({ value: '', onchange: () => { tenant = scope.value; load(); }, options: [
    { value: '', label: t('branding.global') },
    ...tenants.map(row => ({ value: String(row.id), label: t('branding.tenant', { name: row.name }) })),
  ] });
  const query = () => tenant ? `?tenant=${encodeURIComponent(tenant)}` : '';

  async function load() {
    const mine = ++token;
    mount(notice); mount(content, spinner());
    try {
      const result = await api('GET', `/branding${query()}`);
      if (mine !== token) return;
      data = { scope: result?.scope || {}, global: result?.global || {} };
      render();
    } catch (err) { if (mine === token) mount(content, errorBox(err)); }
  }

  function render() {
    const apps = tenant ? APPS.filter(app => app !== 'stalwart') : APPS;
    if (!apps.includes(active)) active = 'default';
    // Forms persist across tab switches, so unsaved edits survive.
    const forms = Object.fromEntries(apps.map(app => [app, appForm(app)]));
    const tabId = app => `${panelId}-${app}`;
    const panel = h('div', { id: panelId, role: 'tabpanel', tabindex: 0, class: 'stack gap-2' });
    const show = () => { panel.setAttribute('aria-labelledby', tabId(active)); mount(panel, forms[active]); };
    const tabBar = tabs({ items: apps.map(app => ({ id: app, label: t(`branding.tab.${app}`) })), active, onChange: app => { active = app; show(); } });
    [...tabBar.children].forEach((el, index) => { el.id = tabId(apps[index]); el.setAttribute('aria-controls', panelId); });
    show();
    mount(content, card(tabBar, panel));
  }

  const source = app => t(`branding.use.${tenant ? (app === 'default' ? 'global' : 'tenantDefault') : (app === 'default' ? 'original' : 'default')}`);
  function inherited(app, key) {
    const { scope: own, global } = data;
    if (!tenant) return app === 'default' ? undefined : own.default?.[key];
    if (app === 'default') return global.default?.[key];
    return own.default?.[key] ?? global[app]?.[key] ?? global.default?.[key];
  }

  function appForm(app) {
    const own = data.scope[app] || {};
    const controls = (app === 'desk' ? DESK_FIELDS : FIELDS).map(key => control(app, key, own));
    if (app === 'kuraka' && !tenant) {
      const input = h('textarea', { name: 'maintenance_message', maxlength: 500, value: own.maintenance_message || '' });
      controls.push({
        el: field({ label: t('branding.field.maintenance_message'), input, help: t('branding.maintenanceHelp') }),
        value: () => ({ maintenance_message: input.value }), valid: () => input.reportValidity(),
      });
    }
    const note = app === 'default' && tenant ? 'defaultTenant' : app;
    const submit = button({ label: t('save'), variant: 'primary', type: 'submit' });
    const form = h('form', { class: 'stack gap-3' }, app === 'kuraka' ? null : h('p', { class: 'muted' }, t(`branding.note.${note}`)), controls.map(entry => entry.el), submit);
    submitForm(form, async () => {
      const body = Object.assign({}, ...controls.map(entry => entry.value()));
      const result = await api('PUT', `/branding/${app}${query()}`, body);
      data.scope[app] = body;
      const desks = result?.desks || [];
      const failed = desks.filter(desk => desk.error);
      toast(desks.length > failed.length ? t('branding.savedDesks', { count: desks.length - failed.length }) : t('branding.saved'));
      if (!tenant) await loadBranding();
      // Other tabs show Default as their inherited value.
      if (app === 'default') render();
      mount(notice, failed.length ? h('div', { class: 'error error-box row gap-1', role: 'alert', tabindex: -1 }, icon('alert'),
        h('div', { class: 'stack gap-1' }, h('strong', {}, t('branding.deskErrors')),
          h('ul', {}, failed.map(desk => h('li', {}, h('span', { class: 'mono' }, desk.url), ` ${desk.error}`))))) : null);
      notice.firstElementChild?.focus();
    }, { validate: () => form.reportValidity() && controls.map(entry => entry.valid()).every(Boolean) });
    return form;
  }

  function control(app, key, own) {
    const label = t(`branding.field.${key}`);
    const keys = key === 'gradient' ? ['gradient_from', 'gradient_to'] : [key];
    const mine = keys.map(name => own[name] || null);
    const base = keys.map(name => inherited(app, name) || null);
    const known = base.every(Boolean);
    const image = IMAGES.includes(key);
    const legendId = uid('legend');
    const errorId = uid('error');
    const error = h('p', { class: 'error', id: errorId, hidden: true });
    const hint = h('p', { class: 'help' }, !known ? t('branding.original') : image ? null : t('branding.inherited', { value: base.join(' → ') }));
    const box = checkbox({ label: source(app), checked: !mine.every(Boolean), onChange: () => { setError(null); sync(); } });
    const inherit = box.querySelector('input');
    const setError = message => { error.textContent = message || ''; error.hidden = !message; };
    let parts, sync, value, valid = () => true;
    if (image) {
      let id = mine[0];
      let uploading = false;
      const file = h('input', { type: 'file', accept: ACCEPT, 'aria-labelledby': legendId, 'aria-describedby': errorId, onchange: async () => {
        const chosen = file.files[0];
        setError(null);
        if (!chosen) return;
        const reject = message => { file.value = ''; setError(message); };
        if (!ACCEPT.split(',').includes(chosen.type)) return reject(t('branding.badType'));
        if (chosen.size > MAX_IMAGE) return reject(t('branding.tooLarge'));
        uploading = true; file.disabled = true;
        try { id = (await api('POST', '/branding/assets', { data: await readFile(chosen) })).id; sync(); }
        catch (err) { reject(errorMessage(err)); }
        finally { uploading = false; file.disabled = inherit.checked; }
      } });
      const preview = h('img', { class: 'brand-preview', alt: label, hidden: true });
      const remove = button({ label: t('branding.remove'), icon: 'x', onClick: () => { id = null; file.value = ''; sync(); file.focus(); } });
      parts = [file, h('div', { class: 'row gap-2' }, preview, remove)];
      sync = () => {
        const src = inherit.checked ? base[0] : id;
        file.disabled = inherit.checked || uploading;
        preview.hidden = !src;
        if (src) preview.src = assetURL(src); else preview.removeAttribute('src');
        remove.hidden = inherit.checked || !id;
        hint.hidden = !inherit.checked;
      };
      value = () => inherit.checked ? {} : { [key]: id };
      valid = () => {
        if (inherit.checked) return true;
        if (uploading || !id) { setError(t(uploading ? 'branding.uploading' : 'branding.chooseImage')); return false; }
        return true;
      };
    } else {
      const fallback = key === 'name' ? [''] : COLORS;
      const inputs = keys.map((name, index) => key === 'name'
        ? textInput({ maxlength: 64, required: true, pattern: '.*\\S.*', autocomplete: 'off', value: mine[0] || base[0] || '' })
        : h('input', { type: 'color', value: mine[index] || base[index] || fallback[index] }));
      if (key === 'gradient') parts = [h('div', { class: 'row gap-2' }, inputs.map((input, index) => field({ label: t(`branding.field.${keys[index]}`), input })))];
      else { inputs[0].setAttribute('aria-labelledby', legendId); parts = inputs; }
      sync = () => {
        inputs.forEach((input, index) => { input.disabled = inherit.checked; if (inherit.checked) input.value = base[index] || fallback[index]; });
        hint.hidden = !inherit.checked;
      };
      value = () => inherit.checked ? {} : Object.fromEntries(keys.map((name, index) => [name, key === 'name' ? inputs[index].value.trim() : inputs[index].value]));
    }
    sync();
    const el = h('fieldset', { class: 'brand-field stack gap-1' }, h('legend', { class: 'label', id: legendId }, label), box, parts, hint, error);
    return { el, value, valid };
  }

  await load();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.branding')),
    h('div', { class: 'branding-scope' }, field({ label: t('branding.scope'), input: scope })), notice, content);
}
