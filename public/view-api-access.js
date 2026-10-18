// Stage 38.2d / 55.21: API access - the screen for the credentials an outside
// system uses on /api/public/v1. Native ES module, appended to the Extension
// Hooks page (the developer-integration area) so daily users never meet it.
// Issue an API key or an OAuth client with only the scopes it needs, see the
// secret once, rotate, revoke and read the recent calls. Every action is the
// existing Super Admin route; nothing here holds a secret after the dialog.

function apiAccessWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString();
}

async function renderAPIAccessPanel(container) {
  const panel = document.createElement('div');
  panel.id = 'api-access-panel';
  panel.style.marginTop = '24px';
  container.appendChild(panel);

  const res = await apiFetch('/api/v1/admin/api-credentials');
  if (!res) return;
  if (!res.ok) {
    panel.innerHTML = '<h2 style="font-size:16px;font-weight:700;margin:0 0 12px;">API access</h2>';
    const msg = document.createElement('p');
    msg.className = 'text-muted';
    msg.textContent = res.status === 403 ? 'Only a Super Admin can issue or change API access.' : 'API access could not be loaded.';
    panel.appendChild(msg);
    return;
  }
  const { credentials = [], scope_catalog: scopes = [] } = await res.json();
  // List first, + New API access as its own page (txn-screen.js), the
  // standard record shape.
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;margin-bottom:12px;">
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="api-cred-name">Name</label>
        <input id="api-cred-name" class="form-input" placeholder="e.g. Shopify stock sync" style="width:220px;"></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="api-cred-mode">Sign-in type</label>
        <select id="api-cred-mode" class="form-select" style="width:240px;">
          <option value="api_key">API key - sent on every call</option>
          <option value="oauth_client">OAuth client - exchanges its secret for a 15-minute token</option>
        </select></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="api-cred-expiry">Expires on (optional)</label>
        <input id="api-cred-expiry" type="date" class="form-input" style="width:170px;"></div>
    </div>
    <fieldset style="border:1px solid var(--border-color);border-radius:6px;padding:8px 12px;margin:0 0 12px;">
      <legend class="form-label" style="padding:0 4px;">Scopes - tick only what the integration needs</legend>
      <div style="display:flex;gap:16px;flex-wrap:wrap;">${scopes.map(s => `
        <label style="display:flex;gap:6px;align-items:center;"><input type="checkbox" class="api-cred-scope" value="${escapeHTMLText(s)}"> ${escapeHTMLText(s)}</label>`).join('')}</div>
    </fieldset>
    <button class="btn btn-primary" id="api-cred-issue">Issue</button>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Name</th><th>Type</th><th>Client ID / key prefix</th><th>Scopes</th><th>Status</th><th>Expires</th><th>Last used</th><th>Actions</th></tr></thead>
        <tbody>${credentials.length === 0
          ? '<tr><td colspan="8" class="text-muted">No API access issued yet. Nothing outside this system can call the public API.</td></tr>'
          : credentials.map(c => `<tr>
            <td>${escapeHTMLText(c.name)}</td>
            <td>${c.auth_mode === 'oauth_client' ? 'OAuth client' : 'API key'}</td>
            <td style="font-family:monospace;">${escapeHTMLText(c.key_prefix)}</td>
            <td>${escapeHTMLText((c.scopes || []).join(', '))}</td>
            <td>${escapeHTMLText(c.status)}${c.rotated_from ? ' <span class="text-muted">(rotated)</span>' : ''}</td>
            <td>${escapeHTMLText(c.expires_at ? apiAccessWhen(c.expires_at) : 'Never')}</td>
            <td>${escapeHTMLText(apiAccessWhen(c.last_used_at))}</td>
            <td>${c.status === 'Active' ? `<button class="action-btn" data-api-rotate="${escapeHTMLText(c.id)}">Rotate</button>
                 <button class="action-btn action-btn-danger" data-api-revoke="${escapeHTMLText(c.id)}">Revoke</button>` : ''}
                 <button class="action-btn" data-api-traffic="${escapeHTMLText(c.id)}">Recent calls</button></td>
          </tr>`).join('')}</tbody>
      </table>
    </div>
    <div id="api-cred-traffic" style="margin:12px 16px;"></div>`;

  panel.appendChild(txnPagedSection({ id: 'api-access', title: 'API access', page: 'New API access', count: credentials.length,
    newLabel: 'New API access', form, list, heading: 'h2' }));
  txnRestorePane('api-access');

  const showSecret = async (body) => {
    const show = window.showOneTimeSecretDialog;
    if (body.client_id) {
      await show('OAuth client issued',
        `Client ID: ${body.client_id}. Token URL: ${body.token_endpoint} (POST grant_type=client_credentials with this client ID and secret, plus the X-Tenant-ID header). Client secret - shown once, store it now:`,
        body.client_secret);
    } else {
      await show('API key issued', 'Send it as "Authorization: Bearer <key>" with the X-Tenant-ID header. Shown once - store it now:', body.api_key);
    }
  };

  panel.querySelector('#api-cred-issue').addEventListener('click', async (event) => {
    const name = panel.querySelector('#api-cred-name').value.trim();
    const chosen = Array.from(panel.querySelectorAll('.api-cred-scope:checked')).map(b => b.value);
    if (!name) { showToast('Give the integration a name.'); panel.querySelector('#api-cred-name').focus(); return; }
    if (chosen.length === 0) { showToast('Tick at least one scope.'); return; }
    const expiry = panel.querySelector('#api-cred-expiry').value;
    const payload = { name, scopes: chosen, auth_mode: panel.querySelector('#api-cred-mode').value };
    if (expiry) payload.expires_at = `${expiry}T23:59:59Z`;
    await guardAgainstDoubleSubmit(event.currentTarget, 'Issuing…', async () => {
      const r = await apiFetch('/api/v1/admin/api-credentials', { method: 'POST', body: JSON.stringify(payload) });
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'The credential was not issued.'); return; }
      await showSecret(await r.json());
      txnDone('api-access');
      renderView('extension-hooks');
    });
  });

  panel.querySelectorAll('[data-api-rotate]').forEach(btn => btn.addEventListener('click', async () => {
    if (!await showCustomConfirm('Rotate this credential? The current secret and every token it obtained stop working now; a new secret is shown once.')) return;
    const r = await apiFetch(`/api/v1/admin/api-credentials/${encodeURIComponent(btn.dataset.apiRotate)}/rotate`, { method: 'POST', body: '{}' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The credential was not rotated.'); return; }
    await showSecret(await r.json());
    renderView('extension-hooks');
  }));
  panel.querySelectorAll('[data-api-revoke]').forEach(btn => btn.addEventListener('click', async () => {
    if (!await showCustomConfirm('Revoke this credential? Every call and token using it is refused from now on. This cannot be undone.')) return;
    const r = await apiFetch(`/api/v1/admin/api-credentials/${encodeURIComponent(btn.dataset.apiRevoke)}`, { method: 'DELETE' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The credential was not revoked.'); return; }
    showToast('Revoked.');
    renderView('extension-hooks');
  }));
  panel.querySelectorAll('[data-api-traffic]').forEach(btn => btn.addEventListener('click', async () => {
    const host = panel.querySelector('#api-cred-traffic');
    host.innerHTML = '<p class="text-muted">Loading…</p>';
    const r = await apiFetch(`/api/v1/admin/api-credentials/${encodeURIComponent(btn.dataset.apiTraffic)}/traffic?limit=25`);
    if (!r) return;
    if (!r.ok) { host.innerHTML = ''; await showApiError(r, 'Recent calls could not be loaded.'); return; }
    const { entries = [] } = await r.json();
    host.innerHTML = entries.length === 0
      ? '<p class="text-muted">No calls yet with this credential.</p>'
      : `<div class="table-wrapper"><table><thead><tr><th>When</th><th>Call</th><th>Status</th><th>Outcome</th><th>Correlation ID</th></tr></thead><tbody>${entries.map(e => `<tr>
          <td>${escapeHTMLText(apiAccessWhen(e.created_at))}</td><td>${escapeHTMLText(`${e.method} ${e.path}`)}</td>
          <td>${escapeHTMLText(String(e.status_code))}</td><td>${escapeHTMLText(e.outcome || '')}</td>
          <td style="font-family:monospace;">${escapeHTMLText(e.correlation_id || '')}</td></tr>`).join('')}</tbody></table></div>`;
  }));
}

export { renderAPIAccessPanel };
