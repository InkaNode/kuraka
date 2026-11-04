import '../i18n/admin.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { t, getLang, setLang } from '../core/i18n.js';
import { getMe, loadMe, setMe } from '../core/session.js';
import { button, field, textInput, card, pageHeader, badge, modal, copyButton, formError, toast } from '../core/ui.js';

// SVG namespace identifiers do not cause a network request.
const svgNamespace = ['http:', '', 'www.w3.org', '2000', 'svg'].join('/');
function qrCode(url) {
  const qr = globalThis.qrcode(0, 'M');
  qr.addData(url); qr.make();
  const count = qr.getModuleCount();
  const svg = document.createElementNS(svgNamespace, 'svg');
  const attrs = { viewBox: `0 0 ${count + 8} ${count + 8}`, width: 240, height: 240,
    role: 'img', 'aria-label': t('account.qr'), focusable: 'false', 'shape-rendering': 'crispEdges' };
  for (const [key, value] of Object.entries(attrs)) svg.setAttribute(key, String(value));
  const background = document.createElementNS(svgNamespace, 'rect');
  for (const [key, value] of Object.entries({ width: count + 8, height: count + 8, fill: '#fff' })) background.setAttribute(key, String(value));
  svg.append(background);
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (!qr.isDark(row, col)) continue;
      const rect = document.createElementNS(svgNamespace, 'rect');
      for (const [key, value] of Object.entries({ x: col + 4, y: row + 4, width: 1, height: 1, fill: '#000' })) rect.setAttribute(key, String(value));
      svg.append(rect);
    }
  }
  return svg;
}
function codeInput() {
  return textInput({ name: 'code', inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]{6}', minlength: 6, maxlength: 6, required: true, class: 'mono' });
}

export async function accountView({ me = getMe() } = {}) {
  me ||= await loadMe();
  const current = textInput({ type: 'password', name: 'current', autocomplete: 'current-password', required: true });
  const next = textInput({ type: 'password', name: 'new', autocomplete: 'new-password', minlength: 12, required: true });
  const confirm = textInput({ type: 'password', name: 'confirm', autocomplete: 'new-password', minlength: 12, required: true });
  let passwordBusy = false;
  const save = button({ label: t('account.passwordTitle'), variant: 'primary', type: 'submit' });
  const passwordForm = h('form', { class: 'stack gap-2', onsubmit: async event => {
    event.preventDefault();
    if (passwordBusy || !passwordForm.reportValidity()) return;
    formError(passwordForm, null);
    if (next.value !== confirm.value) { formError(passwordForm, new Error(t('account.mismatch'))); confirm.focus(); return; }
    passwordBusy = true; save.disabled = true; passwordForm.setAttribute('aria-busy', 'true');
    try {
      await api('POST', '/auth/password', { current: current.value, new: next.value });
      current.value = ''; next.value = ''; confirm.value = '';
      if (passwordForm.isConnected) toast(t('account.passwordChanged'));
    } catch (err) { if (passwordForm.isConnected) formError(passwordForm, err); }
    finally { passwordBusy = false; save.disabled = false; passwordForm.removeAttribute('aria-busy'); }
  } }, field({ label: t('account.current'), input: current }),
  field({ label: t('account.new'), input: next, help: t('account.passwordHelp') }),
  field({ label: t('account.confirm'), input: confirm }), save);

  const totpBody = h('div', { class: 'stack gap-2' });
  let totpBusy = false;
  // Refresh the session after a successful mutation. Keep the known new state
  // even if the follow-up lookup fails, so confirmation is never submitted twice.
  async function updated(enabled) {
    me = { ...me, totp_enabled: enabled };
    setMe(me);
    renderTOTP();
    await loadMe();
    me = getMe() || me;
    renderTOTP();
  }
  function renderTOTP() {
    const enabled = !!me?.totp_enabled;
    mount(totpBody, badge(t(enabled ? 'admin.enabled' : 'admin.notEnabled'), enabled ? 'ok' : 'neutral'),
      h('p', { class: 'muted' }, t(enabled ? 'account.totpOn' : 'account.totpOff')),
      button({ label: t(enabled ? 'account.disable' : 'account.enable'), variant: enabled ? 'danger' : 'primary', onClick: enabled ? disable : enroll }));
  }
  async function enroll(event) {
    if (totpBusy) return;
    const trigger = event.currentTarget;
    totpBusy = true; trigger.disabled = true;
    try {
      const result = await api('POST', '/auth/totp/enroll');
      if (!totpBody.isConnected) return;
      let qr;
      try { qr = qrCode(result.otpauth_url); }
      catch { qr = h('p', { class: 'muted' }, t('account.qrUnavailable')); }
      const secret = textInput({ value: result.secret, readonly: true, class: 'mono', autocomplete: 'off', spellcheck: 'false' });
      const code = codeInput();
      const verify = button({ label: t('account.verify'), variant: 'primary', type: 'submit' });
      const form = h('form', { class: 'stack gap-2', onsubmit: async submitEvent => {
        submitEvent.preventDefault();
        if (totpBusy || !form.reportValidity()) return;
        totpBusy = true; verify.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
        try {
          await api('POST', '/auth/totp/confirm', { code: code.value });
          code.value = ''; secret.value = '';
          await updated(true);
          if (totpBody.isConnected) toast(t('account.totpEnabled'));
        } catch (err) {
          // A refresh error belongs on the current card after confirmation.
          if (totpBody.isConnected) formError(form.isConnected ? form : totpBody, err);
        } finally { totpBusy = false; verify.disabled = false; form.removeAttribute('aria-busy'); }
      } }, h('p', { class: 'muted' }, t('account.scan')), qr,
      field({ label: t('account.secret'), input: secret }), copyButton(result.secret),
      field({ label: t('account.code'), input: code }), verify,
      button({ label: t('cancel'), onClick: () => { if (!totpBusy) { secret.value = ''; code.value = ''; renderTOTP(); } } }));
      mount(totpBody, form); code.focus();
    } catch (err) { if (totpBody.isConnected) formError(totpBody, err); }
    finally { totpBusy = false; trigger.disabled = false; }
  }
  function disable() {
    if (totpBusy) return;
    let closed = false;
    let busy = false;
    const password = textInput({ type: 'password', name: 'password', autocomplete: 'current-password', required: true });
    const code = codeInput();
    const submit = button({ label: t('account.disable'), variant: 'danger', type: 'submit' });
    const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
      event.preventDefault();
      if (busy || totpBusy || !form.reportValidity()) return;
      busy = true; totpBusy = true; submit.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
      try {
        await api('POST', '/auth/totp/disable', { password: password.value, code: code.value });
        password.value = ''; code.value = '';
        dialog.close();
        await updated(false);
        if (totpBody.isConnected) toast(t('account.totpDisabled'));
      } catch (err) { if (!closed) formError(form, err); else if (totpBody.isConnected) formError(totpBody, err); }
      finally { busy = false; totpBusy = false; submit.disabled = false; form.removeAttribute('aria-busy'); }
    } }, h('p', { class: 'muted' }, t('account.disableHelp')),
    field({ label: t('admin.password'), input: password }), field({ label: t('account.code'), input: code }), submit);
    const dialog = modal({ title: t('account.disableTitle'), body: form,
      onClose: () => { closed = true; password.value = ''; code.value = ''; },
      actions: [button({ label: t('cancel'), onClick: () => dialog.close() })] });
  }
  renderTOTP();
  const languages = h('fieldset', { class: 'stack gap-1' }, h('legend', {}, t('account.language')),
    ['es', 'en'].map(lang => h('label', { class: 'checkbox row gap-1' },
      h('input', { type: 'radio', name: 'account-language', value: lang, checked: getLang() === lang, onchange: () => setLang(lang) }),
      h('span', { lang }, t(`lang.${lang}`)))));
  return h('div', { class: 'stack gap-3' }, pageHeader(t('nav.account')),
    card(h('dl', { class: 'stack gap-1' }, h('dt', {}, t('admin.name')), h('dd', {}, me?.name),
      h('dt', {}, t('admin.email')), h('dd', { class: 'mono' }, me?.email),
      h('dt', {}, t('admin.role')), h('dd', {}, t(`admin.role.${me?.role}`)))),
    card(h('div', { class: 'stack gap-2' }, h('h2', {}, t('account.passwordTitle')), passwordForm)),
    card(h('div', { class: 'stack gap-2' }, h('h2', {}, t('account.totp')), totpBody)), card(languages));
}
