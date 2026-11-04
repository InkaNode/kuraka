import { h, mount } from './h.js';
import { t } from './i18n.js';
import { getMe, loadMe, clearMe, isSuperadmin } from './session.js';
import { spinner, errorBox, emptyState } from './ui.js';
import { renderLayout } from '../layout.js';

const routes = [];
let started = false;
let initialSession;
let generation = 0;
export function route(pattern, view, { auth = true, superadmin = false, layout = true } = {}) {
  routes.push({ parts: pattern.split('/'), view, auth, superadmin, layout });
}
export function currentPath() { return (location.hash.slice(1).split('?')[0] || '/'); }
export function navigate(path) {
  if (currentPath() === path && location.hash === `#${path}`) { if (started) render(); }
  else location.hash = `#${path}`;
}
function matchRoute(path) {
  const parts = path.split('/');
  for (const entry of routes) {
    if (entry.parts.length !== parts.length) continue;
    const params = {};
    if (entry.parts.every((part, index) => {
      if (!part.startsWith(':')) return part === parts[index];
      if (!parts[index]) return false;
      params[part.slice(1)] = decodeURIComponent(parts[index]); return true;
    })) return { entry, params };
  }
  return null;
}
async function render() {
  const version = ++generation;
  const app = document.getElementById('app');
  const path = currentPath();
  let entry;
  mount(app, spinner());
  try {
    const matched = matchRoute(path);
    entry = matched?.entry;
    // Public forms remain available without a session lookup. Protected and
    // unknown routes share one lookup; explicit sign-in supplies the user itself.
    if (!getMe() && (!entry || entry.auth || entry.superadmin)) {
      initialSession ||= loadMe().catch(err => { initialSession = null; throw err; });
      await initialSession;
    }
    if (version !== generation) return;
    if (!matched) { navigate(getMe() ? '/domains' : '/login'); return; }
    if ((entry.auth || entry.superadmin) && !getMe()) { navigate('/login'); return; }
    const ctx = { params: matched.params, query: new URLSearchParams(location.hash.split('?').slice(1).join('?')), me: getMe() };
    const content = entry.superadmin && !isSuperadmin()
      ? emptyState({ icon: 'shield', title: t('forbidden.title'), text: t('error.forbidden') })
      : await entry.view(ctx);
    if (version !== generation || path !== currentPath()) return;
    mount(app, entry.layout ? renderLayout(getMe(), content) : content);
    const heading = app.querySelector('h1');
    document.title = heading ? `${heading.textContent} · ${t('brand')}` : t('brand');
    if (heading) { heading.tabIndex = -1; heading.focus(); }
  } catch (err) {
    if (version !== generation) return;
    const content = h('div', { class: 'stack gap-2' }, errorBox(err));
    mount(app, entry?.layout && getMe() ? renderLayout(getMe(), content) : h('main', { class: 'auth-page' }, content));
  }
}
export function start() {
  if (started) return;
  started = true;
  window.addEventListener('hashchange', render);
  window.addEventListener('langchange', render);
  window.addEventListener('auth:lost', () => { clearMe(); navigate('/login'); });
  render();
}
