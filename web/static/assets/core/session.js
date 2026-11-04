import { api } from './api.js';
let me = null;
let pending = null;
let revision = 0;
export function getMe() { return me; }
export function setMe(user) { revision++; me = user; }
export function clearMe() { setMe(null); }
export function isSuperadmin() { return me?.role === 'superadmin'; }
export async function loadMe() {
  if (pending) return pending;
  const requestedAt = revision;
  pending = (async () => {
    try {
      const user = await api('GET', '/auth/me');
      if (revision === requestedAt) setMe(user);
      return me;
    } catch (err) {
      if (err.status !== 401) throw err;
      if (revision === requestedAt) clearMe();
      return me;
    } finally { pending = null; }
  })();
  return pending;
}
