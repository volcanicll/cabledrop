/* Shared helpers for both the desktop panel and the phone page.
 *
 * Talks to the same JSON API from either side, over fetch. Keeping the
 * frontend free of generated bindings means there is no codegen step and the
 * identical endpoints serve both clients.
 */

const $ = (id) => document.getElementById(id);

async function api(path, opts) {
  const res = await fetch(path, Object.assign({ cache: 'no-store' }, opts));
  const raw = await res.text();
  let data = null;
  try { data = raw ? JSON.parse(raw) : null; } catch (_) { /* non-JSON error body */ }
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

const post = (path, body) => api(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body || {}),
});

const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g,
  (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function sizeText(n) {
  if (!n) return '';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n : n.toFixed(n < 10 ? 1 : 0)) + ' ' + u[i];
}
