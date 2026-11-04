import { h } from '../core/h.js';
import { t, getLang, setLang } from '../core/i18n.js';
import { api } from '../core/api.js';
import { navigate } from '../core/router.js';
import { button, field, textInput, toast, submitForm } from '../core/ui.js';

export async function setupView() {
  const token = textInput({ name: 'token', required: true, autocomplete: 'off', spellcheck: 'false', class: 'mono' });
  const name = textInput({ name: 'name', required: true, autocomplete: 'name' });
  const email = textInput({ type: 'email', name: 'email', required: true, autocomplete: 'username', maxlength: 254 });
  const password = textInput({ type: 'password', name: 'password', required: true, autocomplete: 'new-password', minlength: 12 });
  const confirm = textInput({ type: 'password', name: 'confirm', required: true, autocomplete: 'new-password', oninput: () => confirm.setCustomValidity('') });
  password.addEventListener('input', () => confirm.setCustomValidity(''));
  const submit = button({ label: t('setup.submit'), variant: 'primary', type: 'submit' });
  const form = h('form', { class: 'stack gap-2' }, field({ label: t('setup.token'), input: token, help: t('setup.token.help') }), field({ label: t('setup.name'), input: name }),
  field({ label: t('setup.email'), input: email }), field({ label: t('setup.password'), input: password, help: t('setup.password.help') }),
  field({ label: t('setup.confirm'), input: confirm }), submit);
  submitForm(form, async () => {
    await api('POST', '/setup', { token: token.value.trim(), name: name.value.trim(), email: email.value.trim(), password: password.value });
    token.value = ''; password.value = ''; confirm.value = '';
    toast(t('setup.success')); navigate('/login');
  }, { validate: () => {
    if (password.value === confirm.value) return true;
    confirm.setCustomValidity(t('setup.mismatch')); confirm.reportValidity(); return false;
  } });
  return h('main', { class: 'auth-page' }, h('section', { class: 'card auth-card' },
    h('div', { class: 'brand' }, h('img', { src: '/assets/favicon.svg', alt: '', width: 28, height: 28 }), t('brand')),
    h('h1', {}, t('setup.title')), h('p', { class: 'muted intro' }, t('setup.intro')), form,
    h('a', { href: '#/login' }, t('setup.login'))),
    h('div', { class: 'language-switch' }, ['es', 'en'].map(lang => {
      const el = button({ label: t(`lang.short.${lang}`), ariaLabel: t(`lang.${lang}`), onClick: () => setLang(lang) });
      el.setAttribute('aria-pressed', String(getLang() === lang)); el.setAttribute('lang', lang); return el;
    })), h('footer', { class: 'footer' }, t('footer')));
}
