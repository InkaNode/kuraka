(() => {
  'use strict';
  const loaded = document.readyState === 'complete' ? Promise.resolve()
    : new Promise(resolve => window.addEventListener('load', resolve, { once: true }));
  fetch('/_kuraka/addon/desk.json').then(response => {
    if (!response.ok) throw new Error('Desk customization unavailable');
    return response.json();
  }).then(config => {
    if (config.disabled) return;
    if (config.color) document.documentElement.classList.add('kuraka-desk');
    if (config.icon) {
      const icons = document.querySelectorAll('link[rel~="icon"]');
      const targets = icons.length ? icons : [document.head.appendChild(document.createElement('link'))];
      targets.forEach(link => {
        link.setAttribute('rel', 'icon'); link.setAttribute('href', config.icon); link.removeAttribute('type');
      });
    }
    loaded.then(() => setTimeout(async () => {
      const missing = document.querySelector('#app') ? [] : ['app-root'];
      const key = 'kuraka.addon.desk.ok';
      try { if (sessionStorage.getItem(key)) return; } catch { /* Storage can be blocked. */ }
      try {
        const response = await fetch('/_kuraka/addon/desk/report', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ version: '', missing }),
        });
        if (response.ok) {
          try { sessionStorage.setItem(key, 'true'); } catch { /* Keep working without storage. */ }
        }
      } catch { /* Reporting must not interrupt Libredesk. */ }
    }, 5000));
  }).catch(() => { /* Preserve the original app when configuration is unavailable. */ });
})();
