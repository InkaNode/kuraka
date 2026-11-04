import { h, mount } from '../core/h.js';
import { t, getLang, setLang } from '../core/i18n.js';
import { api, ApiError } from '../core/api.js';
import { setMe, clearMe } from '../core/session.js';
import { navigate } from '../core/router.js';
import { brandLogo, brandBanner, loadBranding } from '../core/branding.js';
import { button, field, textInput, formError } from '../core/ui.js';

// Preserve the current MFA step when language changes rebuild this view.
let pendingMFA = false;
export async function loginView() {
  let mfa = pendingMFA;
  let busy = false;
  const email = textInput({ type: 'email', name: 'email', autocomplete: 'username', required: true, maxlength: 254 });
  const password = textInput({ type: 'password', name: 'password', autocomplete: 'current-password', required: true });
  const code = textInput({ name: 'code', inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]{6}', minlength: 6, maxlength: 6, required: true, class: 'mono' });
  const title = h('h1', {}, t(mfa ? 'login.mfa.title' : 'login.title'));
  const intro = h('p', { class: 'muted intro' }, t(mfa ? 'login.mfa.help' : 'login.intro'));
  const submit = button({ label: t(mfa ? 'login.mfa.submit' : 'login.submit'), variant: 'primary', type: 'submit' });
  const cancel = button({ label: t('cancel'), variant: 'ghost', onClick: async () => {
    if (busy) return;
    busy = true; cancel.disabled = true;
    try { await api('POST', '/auth/logout'); }
    catch (err) { if (err.status !== 401) { formError(form, err); return; } }
    finally { busy = false; cancel.disabled = false; }
    pendingMFA = false; clearMe(); navigate('/login');
  } });
  const form = h('form', { class: 'stack gap-2', onsubmit: async event => {
    event.preventDefault();
    if (busy) return;
    busy = true; submit.disabled = true; form.setAttribute('aria-busy', 'true'); formError(form, null);
    try {
      const result = await api('POST', mfa ? '/auth/totp' : '/auth/login', mfa ? { code: code.value } : { email: email.value.trim(), password: password.value });
      if (result.mfa_required) {
        mfa = true; pendingMFA = true; password.value = '';
        title.textContent = t('login.mfa.title'); intro.textContent = t('login.mfa.help');
        document.title = `${t('login.mfa.title')} · ${t('brand')}`;
        mount(submit, t('login.mfa.submit'));
        mount(form, field({ label: t('login.mfa.code'), input: code }), submit, cancel);
        code.focus();
      } else {
        if (!result.user) throw new ApiError(0, 'network');
        password.value = ''; code.value = ''; pendingMFA = false; setMe(result.user); await loadBranding(); navigate('/domains');
      }
    } catch (err) { formError(form, err); }
    finally { busy = false; submit.disabled = false; form.removeAttribute('aria-busy'); }
  } }, mfa ? field({ label: t('login.mfa.code'), input: code }) : [field({ label: t('login.email'), input: email }), field({ label: t('login.password'), input: password })], submit, mfa ? cancel : null);
  const banner = brandBanner();
  return h('main', { class: 'auth-page' }, banner ? h('img', { class: 'auth-banner', src: banner, alt: '' }) : null, h('section', { class: 'card auth-card' },
    h('div', { class: 'brand' }, h('img', { src: brandLogo(), alt: '', width: 28, height: 28 }), h('span', {}, t('brand'))),
    title, intro, form, h('a', { href: '#/setup' }, t('login.setup'))),
    h('div', { class: 'language-switch' }, ['es', 'en'].map(lang => {
      const el = button({ label: t(`lang.short.${lang}`), ariaLabel: t(`lang.${lang}`), onClick: () => setLang(lang) });
      el.setAttribute('aria-pressed', String(getLang() === lang)); el.setAttribute('lang', lang); return el;
    })), h('footer', { class: 'footer' }, t('footer')));
}
