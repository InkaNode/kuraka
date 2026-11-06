import { t } from './i18n.js';
export class ApiError extends Error {
  constructor(status, code, message, params = {}) {
    super(message || t(`error.${code}`, params));
    this.name = 'ApiError'; this.status = status; this.code = code; this.params = params;
  }
}
// Requests in flight show the top loading bar (body.busy).
let pending = 0;
function busy(delta) { pending += delta; document.body?.classList.toggle('busy', pending > 0); }
export async function api(method, path, body) {
  // Only API-relative paths are accepted.
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\') || path.split(/[?#]/)[0].split('/').some(part => /^\.{1,2}$/.test(decodeURIComponent(part)))) {
    throw new ApiError(0, 'invalid_input');
  }
  const headers = { Accept: 'application/json' };
  const options = { method, credentials: 'same-origin', headers };
  if (body !== undefined) { headers['Content-Type'] = 'application/json'; options.body = JSON.stringify(body); }
  let response;
  let data = null;
  let parsed = false;
  busy(1);
  try {
    try { response = await fetch(`/api/v1${path}`, options); }
    catch { throw new ApiError(0, 'network'); }
    parsed = response.status === 204;
    if (response.status !== 204) {
      try { data = await response.json(); parsed = true; } catch { /* Report invalid JSON below. */ }
    }
  } finally { busy(-1); }
  if (!response.ok) {
    const pathname = path.split('?')[0];
    if (response.status === 401 && !['/auth/login', '/auth/totp', '/setup', '/auth/me'].includes(pathname)) window.dispatchEvent(new Event('auth:lost'));
    throw new ApiError(response.status, data?.error?.code || 'network', data?.error?.message, data?.error?.params || {});
  }
  if (!parsed) throw new ApiError(response.status, 'network');
  return data;
}
export function errorMessage(err) {
  if (err?.code) {
    const key = `error.${err.code}`;
    const message = t(key, err.params || {});
    if (message !== key) return message;
  }
  return err?.message || t('error.network');
}
