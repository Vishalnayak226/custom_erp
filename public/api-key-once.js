// 205.12 / 205.19: show a new key once. Copy, Download .env, ready snippets and
// Test connection. Shared by the Created page (view-api-access.js) and the
// developer claim page (api-claim.html), so it holds no app globals.
import { esc, copyText, downloadText, snippets } from './api-docs-viewer.js';

function codeBlock(text) {
  return `<div class="api-code"><pre>${esc(text)}</pre><button type="button" class="btn btn-outline btn-sm" data-copy>Copy</button></div>`;
}

// body: api_key (or client_secret), client_id, base_url, tenant_code.
// Returns the extra-button row so a caller can add its own buttons.
export function renderKeyOnce(host, body) {
  const key = body.api_key || body.client_secret || '';
  const base = String(body.base_url || `${location.origin}/api/public/v1`).replace(/\/$/, '');
  const tenant = body.tenant_code || '';
  const isOAuth = !body.api_key && !!body.client_secret;
  const sn = snippets({ base, tenant, key: isOAuth ? 'ACCESS_TOKEN' : key, path: '/me' });
  host.innerHTML = `
    ${isOAuth ? `<p class="api-muted">Client ID</p><div class="api-keybox"><span class="api-mono">${esc(body.client_id || '')}</span></div>` : ''}
    <p class="api-muted">${isOAuth ? 'Client secret' : 'API key'} (shown once)</p>
    <div class="api-keybox"><span class="api-mono">${esc(key)}</span>
      <button type="button" class="btn btn-primary btn-sm" data-key-copy>Copy</button>
      <button type="button" class="btn btn-outline btn-sm" data-key-env>Download .env</button></div>
    <div class="api-keybox"><button type="button" class="btn btn-outline btn-sm" data-key-test${isOAuth ? ' disabled' : ''}>Test connection</button>
      <span data-key-result class="api-muted"></span><span data-key-extra class="api-actions"></span></div>
    <div class="api-tabs">${['curl', 'javascript', 'python'].map((k, i) => `<button type="button" class="api-tab${i ? '' : ' active'}" data-snip="${k}">${k === 'javascript' ? 'JavaScript' : k === 'python' ? 'Python' : 'curl'}</button>`).join('')}</div>
    ${['curl', 'javascript', 'python'].map((k, i) => `<div data-snip-pane="${k}"${i ? ' hidden' : ''}>${codeBlock(sn[k])}</div>`).join('')}`;
  const flash = (btn, label, ok) => { const old = btn.textContent; btn.textContent = ok ? label : 'Select and copy'; setTimeout(() => { btn.textContent = old; }, 1500); };
  host.querySelector('[data-key-copy]').addEventListener('click', async (e) => flash(e.currentTarget, 'Copied', await copyText(key)));
  host.querySelector('[data-key-env]').addEventListener('click', () => {
    const lines = [`ERP_API_BASE=${base}`, `ERP_TENANT=${tenant}`];
    if (isOAuth) lines.push(`ERP_CLIENT_ID=${body.client_id || ''}`, `ERP_CLIENT_SECRET=${key}`);
    else lines.push(`ERP_API_KEY=${key}`);
    downloadText('erp-api.env', lines.join('\n') + '\n');
  });
  host.querySelector('[data-key-test]').addEventListener('click', async (e) => {
    const out = host.querySelector('[data-key-result]');
    const btn = e.currentTarget;
    btn.disabled = true;
    out.className = 'api-muted';
    out.textContent = 'Testing…';
    try {
      const headers = { Authorization: `Bearer ${key}` };
      if (tenant) headers['X-Tenant-ID'] = tenant;
      const res = await fetch(`${base}/me`, { headers });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        out.className = 'api-ok';
        out.textContent = `Connected${data.name ? ' as ' + data.name : ''}.`;
      } else {
        out.className = 'api-bad';
        out.textContent = `${data.message || data.error || 'Refused'}${data.fix ? ' - ' + data.fix : ''}`;
      }
    } catch (_) {
      out.className = 'api-bad';
      out.textContent = 'Could not reach it. Try the curl call.';
    }
    btn.disabled = false;
  });
  host.addEventListener('click', async (e) => {
    const cp = e.target.closest('[data-copy]');
    if (cp) { flash(cp, 'Copied', await copyText(cp.parentElement.querySelector('pre').textContent)); return; }
    const tab = e.target.closest('[data-snip]');
    if (tab) {
      host.querySelectorAll('[data-snip]').forEach(b => b.classList.toggle('active', b === tab));
      host.querySelectorAll('[data-snip-pane]').forEach(p => { p.hidden = p.dataset.snipPane !== tab.dataset.snip; });
    }
  });
  return host.querySelector('[data-key-extra]');
}
