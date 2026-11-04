const messages = { es: {}, en: {} };
const warned = new Set();
let saved;
try { saved = localStorage.getItem('kuraka.lang'); } catch { /* Storage may be disabled. */ }
let lang = ['es', 'en'].includes(saved) ? saved : (navigator.language?.toLowerCase().startsWith('en') ? 'en' : 'es');
document.documentElement.lang = lang;
export function registerMessages(bundle) {
  for (const locale of ['es', 'en']) Object.assign(messages[locale], bundle[locale] || {});
}
// A configured brand name replaces 'brand' in every language.
let brand = null;
export function setBrand(name) { brand = name || null; }
export function getLang() { return lang; }
export function setLang(next) {
  if (!['es', 'en'].includes(next)) return;
  lang = next;
  try { localStorage.setItem('kuraka.lang', lang); } catch { /* Keep the in-memory preference. */ }
  document.documentElement.lang = lang;
  window.dispatchEvent(new Event('langchange'));
}
export function t(key, vars = {}) {
  if (key === 'brand' && brand) return brand;
  const value = messages[lang][key];
  if (value === undefined) {
    if (!warned.has(key)) { console.warn(`Missing translation: ${key}`); warned.add(key); }
    return key;
  }
  return value.replace(/\{([^{}]+)\}/g, (match, name) => Object.hasOwn(vars, name) ? String(vars[name]) : match);
}
export function formatDate(value) {
  const date = new Date(typeof value === 'number' ? value * 1000 : value);
  if (Number.isNaN(date.getTime())) return t('date.invalid');
  return new Intl.DateTimeFormat(lang, { dateStyle: 'short', timeStyle: 'short' }).format(date);
}
