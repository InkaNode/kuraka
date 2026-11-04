import { h } from './core/h.js';
import { t, getLang, setLang } from './core/i18n.js';
import { icon } from './core/icons.js';
import { api } from './core/api.js';
import { clearMe } from './core/session.js';
import { button, toast } from './core/ui.js';
import { errorMessage } from './core/api.js';
import { navigate, currentPath } from './core/router.js';

export function renderLayout(me, content) {
  const path = currentPath();
  const links = [
    ['/domains', 'nav.domains', 'globe'],
    ...(me?.role === 'superadmin' ? [['/tenants', 'nav.tenants', 'building'], ['/users', 'nav.users', 'users']] : []),
    ['/audit', 'nav.audit', 'list'], ['/account', 'nav.account', 'user'],
  ];
  const toggle = button({ icon: 'menu', variant: 'ghost', ariaLabel: t('nav.menu'), onClick: () => {
    const open = root.classList.toggle('menu-open');
    toggle.setAttribute('aria-expanded', String(open));
  } });
  toggle.classList.add('menu-toggle');
  toggle.setAttribute('aria-expanded', 'false');
  toggle.setAttribute('aria-controls', 'sidebar-menu');
  const logout = button({ label: t('nav.logout'), icon: 'log-out', variant: 'ghost', onClick: async () => {
    logout.disabled = true;
    try { await api('POST', '/auth/logout'); clearMe(); navigate('/login'); }
    catch (err) { if (err.status !== 401) toast(errorMessage(err), 'danger'); }
    finally { logout.disabled = false; }
  } });
  const root = h('div', { class: 'app-layout', onkeydown: event => {
    if (event.key === 'Escape' && root.classList.contains('menu-open')) {
      root.classList.remove('menu-open'); toggle.setAttribute('aria-expanded', 'false'); toggle.focus();
    }
  } },
    h('a', { class: 'skip-link', href: '#main-content', onclick: event => { event.preventDefault(); root.querySelector('main').focus(); } }, t('nav.skip')),
    h('aside', { class: 'sidebar' },
      h('header', { class: 'sidebar-header' }, h('a', { class: 'brand', href: '#/domains' }, h('img', { src: '/assets/favicon.svg', alt: '', width: 28, height: 28 }), t('brand')), toggle),
      h('div', { class: 'sidebar-body', id: 'sidebar-menu' },
        h('nav', { class: 'nav', 'aria-label': t('nav.main') }, links.map(([href, label, name]) => h('a', {
          href: `#${href}`, 'aria-current': path === href || path.startsWith(`${href}/`) ? 'page' : null,
        }, icon(name), t(label)))),
        h('div', { class: 'language-switch' }, ['es', 'en'].map(lang => {
          const el = button({ label: t(`lang.short.${lang}`), ariaLabel: t(`lang.${lang}`), onClick: () => setLang(lang) });
          el.setAttribute('aria-pressed', String(getLang() === lang));
          el.setAttribute('lang', lang);
          return el;
        })),
        h('div', { class: 'user-menu stack gap-1' }, h('strong', {}, me?.name), h('span', { class: 'mono muted' }, me?.email), logout))),
    h('div', { class: 'main-shell' }, h('main', { id: 'main-content', class: 'main-content', tabindex: -1 }, content), h('footer', { class: 'footer' }, t('footer'))));
  return root;
}
