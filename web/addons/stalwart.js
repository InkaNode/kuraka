(() => {
  'use strict';
  const probes = {
    sidebar: '#app-sidebar',
    'sidebar-toggle': 'header [aria-controls="app-sidebar"][aria-expanded]',
    'nav-links': '#app-sidebar nav a[href], #app-sidebar nav button[aria-describedby]',
    'user-menu': 'header [data-slot="dropdown-menu-trigger"]',
    'brand-logo': 'header a:is([href="/"], [href="/admin"], [href="/admin/"], [href="/account"], [href="/account/"]) > :is(img, svg), .tile svg[aria-label="Stalwart Logo"], #logo-wrap > :is(img:not([data-kuraka-logo]), svg[aria-label="Stalwart"])',
    'search-items': '[data-slot="command-item"][data-value]',
  };
  const managed = new Set(['x:Account/User', 'x:Tenant', 'x:Domain', 'x:DkimSignature']);
  const failed = new Set();
  const loaded = document.readyState === 'complete' ? Promise.resolve()
    : new Promise(resolve => window.addEventListener('load', resolve, { once: true }));
  const admin = () => /^\/admin(?:\/|$)/.test(location.pathname);
  let schema = null, schemaToken = '', queued = false, showAll = false, brandedTitle = '';
  // New key: the fork's kuraka.showAll could keep everything shown; hidden is the default.
  const showAllKey = 'kuraka.addon.stalwart.showAll';
  try { showAll = localStorage.getItem(showAllKey) === 'true'; } catch { /* Use the default. */ }

  fetch('/_kuraka/addon/stalwart.json').then(response => {
    if (!response.ok) throw new Error('Stalwart customization unavailable');
    return response.json();
  }).then(config => {
    if (config.disabled) return;
    document.documentElement.classList.add('kuraka-stalwart');
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.addEventListener('click', () => {
      showAll = !showAll;
      try { localStorage.setItem(showAllKey, String(showAll)); } catch { /* Retain the in-memory choice. */ }
      schedule();
    });
    function hiddenView(view) {
      if (!view) return false;
      const object = schema?.objects[view];
      const resolved = object?.type === 'view' ? schema.objects[object.objectName] : object;
      const backing = view.startsWith('Dashboard/') || view === 'CustomComponent/Dashboard' ? schema?.objects['x:Metric'] : resolved;
      return managed.has(view) || view === 'CustomComponent/LiveTracing' || !!backing?.enterprise;
    }
    // Same shape as WebUI navTree.ts: top-level { link } / { container }, nested { type }.
    function navNode(item) {
      const link = item.link || (item.type === 'link' ? item : null);
      if (link) return { view: link.viewName };
      const box = item.container || (item.type === 'container' ? item : null);
      return box ? { name: box.name, children: (box.items || []).map(navNode).filter(Boolean) } : null;
    }
    // frugal: links upstream hides for permissions count as shown, so a group mixing
    // those with Enterprise links stays visible; the fork hid it.
    const allHidden = nodes => nodes.every(node => node.view ? hiddenView(node.view) : allHidden(node.children));
    function loadSchema() {
      if (!admin() || schema) return;
      // Same endpoint and persisted bearer token as services/jmap/client.ts and authStore.ts.
      let token;
      try { token = JSON.parse(sessionStorage.getItem('stalwart-auth'))?.state?.accessToken; } catch { return; }
      // Retry only with a new token, e.g. after the WebUI refreshes an expired one.
      if (!token || token === schemaToken) return;
      schemaToken = token;
      fetch('/api/schema', { headers: { Authorization: `Bearer ${token}` }, redirect: 'error' })
        .then(response => { if (!response.ok) throw new Error('Schema unavailable'); return response.json(); })
        .then(value => { if (value?.objects && Array.isArray(value.layouts)) schema = value; schedule(); })
        .catch(() => { /* The compatibility check restores navigation when schema is unavailable. */ });
    }
    function setHidden(node, hide) {
      if (hide && !node.hasAttribute('data-kuraka-hidden')) node.setAttribute('data-kuraka-hidden', '');
      if (!hide && node.hasAttribute('data-kuraka-hidden')) node.removeAttribute('data-kuraka-hidden');
    }
    function apply() {
      queued = false;
      loadSchema();
      if (config.name && document.title !== brandedTitle && document.title.includes('Stalwart WebUI')) {
        brandedTitle = document.title.replace('Stalwart WebUI', () => config.name);
        document.title = brandedTitle;
      }
      // OAuth /login names Stalwart's own WebUI client; CSS hides its #app-tile logo.
      const loginTitle = document.getElementById('title');
      if (loginTitle?.textContent.includes('Stalwart Web Interface')) {
        if (!loginTitle.hasAttribute('data-kuraka-client')) loginTitle.setAttribute('data-kuraka-client', '');
        if (config.name) loginTitle.textContent = loginTitle.textContent.replace('Stalwart Web Interface', () => config.name);
      }
      if (config.icon) {
        const icons = document.querySelectorAll('link[rel~="icon"]');
        const targets = icons.length ? icons : [document.head.appendChild(document.createElement('link'))];
        targets.forEach(link => {
          if (link.getAttribute('href') !== config.icon) link.setAttribute('href', config.icon);
          if (!link.hasAttribute('rel')) link.setAttribute('rel', 'icon');
          link.removeAttribute('type');
        });
      }
      if (config.logo && !failed.has('brand-logo')) document.querySelectorAll(probes['brand-logo']).forEach(original => {
        if (original.tagName.toLowerCase() === 'img') {
          if (!original.hasAttribute('data-kuraka-src')) original.setAttribute('data-kuraka-src', original.getAttribute('src') || '');
          if (original.getAttribute('src') !== config.logo) original.setAttribute('src', config.logo);
        } else if (!original.hasAttribute('data-kuraka-logo-original')) {
          const image = document.createElement('img');
          image.src = config.logo; image.alt = config.name || original.getAttribute('aria-label') || '';
          image.className = 'h-7 w-auto max-w-[220px] object-contain';
          image.setAttribute('data-kuraka-logo', '');
          // Keep React's original node in place, so reconciliation can still remove it.
          original.setAttribute('data-kuraka-logo-original', ''); original.after(image);
        }
      });
      document.querySelectorAll('[data-kuraka-logo]').forEach(image => {
        if (!image.previousElementSibling?.hasAttribute('data-kuraka-logo-original')) image.remove();
      });
      const hide = admin() && !showAll && !['sidebar', 'sidebar-toggle', 'nav-links'].some(id => failed.has(id));
      document.querySelectorAll('[data-kuraka-hidden]').forEach(node => setHidden(node, false));
      if (hide) {
        const links = '#app-sidebar nav a[href]' + (failed.has('user-menu') ? '' : ', [data-slot="dropdown-menu-content"] a[href]');
        document.querySelectorAll(links).forEach(link => {
          let path;
          try { path = decodeURIComponent(new URL(link.href, location.href).pathname); } catch { return; }
          const base = new URL(document.baseURI).pathname.replace(/\/$/, '');
          const view = (base && path.startsWith(`${base}/`) ? path.slice(base.length) : path).replace(/^\/[^/]+\//, '');
          setHidden(link, hiddenView(view));
        });
        document.querySelectorAll('#app-sidebar nav button[aria-describedby]').forEach(node => setHidden(node, !!node.querySelector('svg.lucide-lock')));
        if (!failed.has('search-items')) document.querySelectorAll(probes['search-items']).forEach(node => {
          setHidden(node, hiddenView(node.getAttribute('data-value').split('|')[2]));
        });
        // The fork hid groups and sections left without links, and the Try Enterprise menu item.
        const layouts = (schema?.layouts || []).map(layout => ({ name: layout.name, children: (layout.items || []).map(navNode).filter(Boolean) }));
        const nav = document.querySelector('#app-sidebar nav[aria-label]');
        const section = layouts.find(layout => layout.name === nav?.getAttribute('aria-label'));
        if (section) {
          const shown = new Set(), gone = new Set();
          const walk = nodes => nodes.forEach(node => {
            if (node.view) return;
            (allHidden(node.children) ? gone : shown).add(node.name);
            walk(node.children);
          });
          walk(section.children);
          // Radix CollapsibleTrigger sets aria-controls; its parent is the Collapsible root.
          nav.querySelectorAll('button[aria-controls]').forEach(button => {
            const name = button.querySelector('span.truncate')?.textContent;
            if (gone.has(name) && !shown.has(name)) setHidden(button.parentElement, true);
          });
        }
        document.querySelectorAll('#app-sidebar [role="group"] button[aria-label]:not([aria-pressed="true"])').forEach(button => {
          const layout = layouts.find(item => item.name === button.getAttribute('aria-label'));
          if (layout && allHidden(layout.children)) setHidden(button, true);
        });
        if (!failed.has('user-menu')) document.querySelectorAll('[data-slot="dropdown-menu-content"] [data-slot="dropdown-menu-item"]:has(svg.lucide-sparkles)').forEach(item => {
          setHidden(item, true);
          if (item.nextElementSibling?.matches('[data-slot="dropdown-menu-separator"]')) setHidden(item.nextElementSibling, true);
        });
      }
      const trigger = document.querySelector(probes['user-menu']);
      const menu = trigger && document.getElementById(trigger.getAttribute('aria-controls'));
      const target = admin() ? (!failed.has('user-menu') && menu) || (!failed.has('sidebar') && document.querySelector(probes.sidebar)) : null;
      if (target) {
        const es = (document.documentElement.lang || navigator.language).toLowerCase().startsWith('es');
        const label = showAll
          ? (es ? 'Ocultar opciones de Kuraka y Enterprise' : 'Hide Kuraka and Enterprise items')
          : (es ? 'Mostrar opciones de Kuraka y Enterprise' : 'Show Kuraka and Enterprise items');
        if (toggle.textContent !== label) toggle.textContent = label;
        const classes = 'flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm text-muted-foreground hover:bg-muted focus-visible:ring-2';
        if (toggle.className !== classes) toggle.className = classes;
        const pressed = String(showAll);
        if (toggle.getAttribute('aria-pressed') !== pressed) toggle.setAttribute('aria-pressed', pressed);
        if (toggle.parentNode !== target) target.append(toggle);
      } else toggle.remove();
    }
    function schedule() { if (!queued) { queued = true; requestAnimationFrame(apply); } }
    // cmdk keeps its React item registry: never activate a visually hidden result.
    document.addEventListener('keydown', event => {
      if (event.key === 'Enter' && event.target.closest('[data-slot="command"]')?.querySelector('[data-selected="true"][data-kuraka-hidden]')) {
        event.preventDefault(); event.stopImmediatePropagation();
      }
    }, true);
    new MutationObserver(schedule).observe(document.documentElement, {
      subtree: true, childList: true, characterData: true, attributes: true,
      attributeFilter: ['href', 'src', 'lang', 'aria-controls', 'data-value'],
    });
    window.addEventListener('popstate', schedule);
    window.addEventListener('storage', event => {
      if (event.key === showAllKey) { showAll = event.newValue === 'true'; schedule(); }
    });
    apply();
    loaded.then(() => setTimeout(async () => {
      const shell = !/\/(?:login|oauth\/callback|bootstrap)(?:\/|$)/.test(location.pathname);
      const expected = shell ? ['brand-logo'] : [];
      if (admin() && shell) {
        expected.push('user-menu', 'sidebar-toggle');
        // Sidebar can legitimately be closed on mobile or by user choice.
        if (document.querySelector('header [aria-controls="app-sidebar"][aria-expanded="true"]')) expected.push('sidebar', 'nav-links');
        if (document.querySelector('[data-slot="command"] [data-slot="command-item"]')) expected.push('search-items');
        if (!schema) failed.add('view-schema');
      }
      expected.forEach(id => { if (!document.querySelector(probes[id])) failed.add(id); });
      if (failed.has('brand-logo')) {
        document.querySelectorAll('[data-kuraka-logo]').forEach(node => node.remove());
        document.querySelectorAll('[data-kuraka-logo-original]').forEach(node => node.removeAttribute('data-kuraka-logo-original'));
        document.querySelectorAll('[data-kuraka-src]').forEach(node => { node.setAttribute('src', node.getAttribute('data-kuraka-src')); node.removeAttribute('data-kuraka-src'); });
      }
      schedule();
      const missing = [...failed], key = 'kuraka.addon.stalwart.ok';
      try { if (!missing.length && sessionStorage.getItem(key)) return; } catch { /* Storage can be blocked. */ }
      const meta = document.querySelector('meta[name="app-version"], meta[name="application-version"]');
      const version = (meta?.content || '').slice(0, 40);
      try {
        const response = await fetch('/_kuraka/addon/stalwart/report', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ version, missing }),
        });
        if (response.ok && !missing.length) {
          try { sessionStorage.setItem(key, 'true'); } catch { /* Keep working without storage. */ }
        }
      } catch { /* Reporting must not interrupt Stalwart. */ }
    }, 5000));
  }).catch(() => { /* Preserve the original UI when configuration is unavailable. */ });
})();
