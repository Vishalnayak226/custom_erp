// 205.19: the developer link page. Pressing the button makes the key once and
// shows it once; reloading the page cannot show it again.
// 297.1: imported with this module's own ?v= (a static import carries none, so a release would not reach it).
const { esc } = await import('./api-docs-viewer.js' + new URL(import.meta.url).search);
const { renderKeyOnce } = await import('./api-key-once.js' + new URL(import.meta.url).search);

const host = document.getElementById('claim-host');
const token = new URLSearchParams(location.search).get('t') || '';

function fail(text) {
  host.hidden = false;
  host.innerHTML = `<p class="api-muted">${esc(text)}</p>`;
}

document.getElementById('claim-go').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  if (!token) { fail('This link is not complete.'); return; }
  btn.disabled = true;
  try {
    const res = await fetch(`/api/public/v1/developer-links/${encodeURIComponent(token)}/claim`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) { fail(res.status === 404 || res.status === 410 ? 'This link was used or has ended.' : (body.message || 'The key could not be made.')); return; }
    document.getElementById('claim-intro').hidden = true;
    host.hidden = false;
    renderKeyOnce(host, body);
    const note = document.createElement('p');
    note.className = 'api-warn';
    note.textContent = 'Copy it now. It will not be shown again.';
    host.prepend(note);
  } catch (_) {
    fail('The key could not be made.');
    btn.disabled = false;
  }
});
