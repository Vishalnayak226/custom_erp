// 205.19: the shared docs page. Opens a link a key's owner shared: no login, no secret.
// 297.1: imported with this module's own ?v= (a static import carries none, so a release would not reach it).
const { renderApiDocs, esc } = await import('./api-docs-viewer.js' + new URL(import.meta.url).search);

const host = document.getElementById('docs-host');
const title = document.getElementById('docs-title');
const token = new URLSearchParams(location.search).get('t') || '';

function fail(text) {
  title.textContent = 'API docs';
  host.innerHTML = `<p class="api-muted">${esc(text)}</p>`;
}

(async () => {
  if (!token) { fail('This link is not complete.'); return; }
  try {
    const res = await fetch(`/api/public/v1/docs/${encodeURIComponent(token)}`);
    if (!res.ok) { fail(res.status === 404 || res.status === 410 ? 'This link has ended.' : 'The docs could not be loaded.'); return; }
    const spec = await res.json();
    title.textContent = (spec.info && spec.info.title) || 'API docs';
    renderApiDocs(host, spec, { tenant: spec['x-tenant-code'] || (spec.info && spec.info['x-tenant-code']) || '' });
  } catch (_) {
    fail('The docs could not be loaded.');
  }
})();
