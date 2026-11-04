import { api } from './api.js';
import { t, setBrand } from './i18n.js';

// Built-in look; the API returns only the keys the active scope sets.
const LOGO = '/assets/favicon.svg';
let logo = LOGO;
let banner = null;
export function brandLogo() { return logo; }
export function brandBanner() { return banner; }

const hex = value => typeof value === 'string' && /^#[0-9a-f]{6}$/i.test(value) ? value : null;
// Images load only from this origin.
function local(value) {
  if (typeof value !== 'string' || !value) return null;
  try { const url = new URL(value, location.origin); return url.origin === location.origin ? url.href : null; } catch { return null; }
}
// WCAG relative luminance; text on a colour turns dark above ~0.18.
function luminance(color) {
  return [1, 3, 5].map(i => parseInt(color.slice(i, i + 2), 16) / 255)
    .map(c => c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
    .reduce((sum, c, i) => sum + c * [0.2126, 0.7152, 0.0722][i], 0);
}
const onColor = value => value > 0.179 ? '#1c1917' : '#ffffff';
function setVar(name, value) {
  const style = document.documentElement.style;
  if (value) style.setProperty(name, value); else style.removeProperty(name);
}

export async function loadBranding() {
  let data = {};
  try { data = await api('GET', '/branding/kuraka') || {}; } catch { /* Built-in defaults stay. */ }
  const color = hex(data.color);
  const from = hex(data.gradient_from);
  const to = hex(data.gradient_to);
  setVar('--accent', color);
  setVar('--on-accent', color && onColor(luminance(color)));
  setVar('--brand-gradient', from && to && `linear-gradient(135deg, ${from}, ${to})`);
  setVar('--on-brand', from && to && onColor((luminance(from) + luminance(to)) / 2));
  setBrand(typeof data.name === 'string' ? data.name.trim() : null);
  logo = local(data.logo) || LOGO;
  banner = local(data.banner);
  const favicon = local(data.icon);
  let link = document.querySelector('link[rel="icon"]');
  if (!link) { link = document.createElement('link'); link.rel = 'icon'; document.head.append(link); }
  link.href = favicon || LOGO;
  if (favicon) link.removeAttribute('type'); else link.type = 'image/svg+xml';
  // Brand marks already on screen follow without a re-render.
  for (const el of document.querySelectorAll('.brand')) {
    el.querySelector('img')?.setAttribute('src', logo);
    const name = el.querySelector('span');
    if (name) name.textContent = t('brand');
  }
}
