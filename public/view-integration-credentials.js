// Stage 49.8.5: Settings > Integrations credentials - courier, channel and
// CleverTap (and Pine Labs / Unicommerce) on one list: kind, name, status,
// owner, last used, expires. Add is its own page; Rotate, Switch off and
// Disconnect are the existing admin routes. The list carries no secret, and a
// secret typed here is sent once and never shown again (a generated webhook
// secret is shown once in the dialog).

const icWhen = (v) => { if (!v) return '—'; const d = new Date(v); return String(v).length <= 10 || isNaN(d) ? String(v) : d.toLocaleString(); };
const icKinds = { courier: 'Courier', channel: 'Channel', clevertap: 'CleverTap', pinelabs: 'Pine Labs', unicommerce: 'Unicommerce' };

export async function renderIntegrationCredentialsView(container) {
  const res = await apiFetch('/api/v1/integrations/credentials');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Credentials could not be loaded.'); return; }
  const { data: rows = [], couriers = [] } = await res.json();
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  const esc = escapeHTMLText;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:0 16px;max-width:640px;">
      <div class="form-group"><label class="form-label" for="ic-kind">Kind</label>
        <select id="ic-kind" class="form-select"><option value="courier">Courier</option><option value="channel">Channel</option><option value="clevertap">CleverTap</option></select></div>
      <div class="form-group" id="ic-code-box"><label class="form-label" for="ic-code" id="ic-code-label">Courier</label>
        <select id="ic-courier" class="form-select">${couriers.map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join('')}</select>
        <input id="ic-code" class="form-input hidden" autocomplete="off"></div>
      <div class="form-group" id="ic-fields-box" style="grid-column:1/-1;"><label class="form-label" for="ic-fields">Fields</label>
        <textarea id="ic-fields" class="form-input" rows="4" autocomplete="off" placeholder="name=value, one per line"></textarea></div>
      <div class="form-group hidden" id="ic-region-box"><label class="form-label" for="ic-region">Region</label>
        <input id="ic-region" class="form-input" value="in1" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="ic-expires">Expires (optional)</label>
        <input id="ic-expires" type="date" class="form-input"></div>
    </div>
    <button type="button" class="btn btn-primary" id="ic-save">Save credential</button>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = rows.length === 0 ? '<p class="text-muted" style="padding:16px;">None yet.</p>' : `
    <div class="table-wrapper"><table><thead><tr><th>Kind</th><th>Name</th><th>Status</th><th>Owner</th><th>Last used</th><th>Expires</th><th>Changed</th><th></th></tr></thead>
      <tbody>${rows.map((r, i) => `<tr>
        <td>${esc(icKinds[r.kind] || r.kind)}</td><td>${esc(r.name)}</td>
        <td>${r.status === 'Active' ? 'Active' : `<span class="badge badge-danger">${esc(r.status)}</span>`}</td>
        <td>${esc(r.owner || '—')}</td><td>${esc(icWhen(r.last_used_at))}</td><td>${esc(icWhen(r.expires_at))}</td><td>${esc(icWhen(r.changed_at))}</td>
        <td><button class="action-btn" data-ic-rotate="${i}">Rotate</button>
          <button class="action-btn" data-ic-active="${i}">${r.active ? 'Switch off' : 'Switch on'}</button>
          <button class="action-btn action-btn-danger" data-ic-disconnect="${i}">Disconnect</button></td></tr>`).join('')}</tbody></table></div>`;

  container.appendChild(txnPagedSection({ id: 'integration-credentials', title: 'Credentials', page: 'New credential', count: rows.length,
    newLabel: 'New credential', form, list }));
  txnRestorePane('integration-credentials');
  const reload = () => renderView('configuration:credentials');

  // The Add form changes with the kind.
  const kindEl = form.querySelector('#ic-kind');
  const syncKind = () => {
    const k = kindEl.value;
    form.querySelector('#ic-courier').classList.toggle('hidden', k !== 'courier');
    form.querySelector('#ic-code').classList.toggle('hidden', k === 'courier');
    form.querySelector('#ic-code-label').textContent = k === 'courier' ? 'Courier' : k === 'channel' ? 'Channel code' : 'Account ID';
    form.querySelector('#ic-region-box').classList.toggle('hidden', k !== 'clevertap');
    form.querySelector('#ic-fields').placeholder = k === 'clevertap' ? 'Passcode' : 'name=value, one per line';
    form.querySelector('#ic-fields-box label').textContent = k === 'clevertap' ? 'Passcode' : 'Fields';
  };
  kindEl.addEventListener('change', syncKind);
  syncKind();

  form.querySelector('#ic-save').addEventListener('click', async (event) => {
    const kind = kindEl.value;
    const text = form.querySelector('#ic-fields').value.trim();
    const code = kind === 'courier' ? form.querySelector('#ic-courier').value : form.querySelector('#ic-code').value.trim();
    const expires = form.querySelector('#ic-expires').value;
    if (!code || !text) { showToast(kind === 'clevertap' ? 'Account ID and passcode are required.' : 'Name and fields are required.', { variant: 'error' }); return; }
    let url, body;
    if (kind === 'clevertap') {
      url = '/api/v1/clevertap/credentials';
      body = { account_id: code, passcode: text, region: form.querySelector('#ic-region').value.trim() || 'in1' };
    } else {
      body = {};
      for (const line of text.split('\n')) {
        const at = line.indexOf('=');
        if (at > 0 && line.slice(at + 1).trim()) body[line.slice(0, at).trim()] = line.slice(at + 1).trim();
      }
      if (!Object.keys(body).length) { showToast('Use name=value, one per line.', { variant: 'error' }); return; }
      url = kind === 'courier' ? `/api/v1/marketplace/couriers/${encodeURIComponent(code)}/credentials` : `/api/v1/marketplace/channels/${encodeURIComponent(code)}/credentials`;
    }
    if (expires) body.expires_at = expires;
    await guardAgainstDoubleSubmit(event.currentTarget, 'Saving…', async () => {
      const r = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'The credential was not saved.'); return; }
      showToast('Saved.', { variant: 'success' });
      txnDone('integration-credentials');
      reload();
    });
  });

  container.querySelectorAll('[data-ic-rotate]').forEach(btn => btn.addEventListener('click', async () => {
    const row = rows[Number(btn.dataset.icRotate)];
    const names = row.secret_fields || [];
    if (!names.length) { showToast('Nothing to rotate here. Save the credential again.'); return; }
    let field = names[0];
    if (names.length > 1) {
      const pick = await showCustomPrompt(`Which: ${names.join(', ')}`, names[0], 'Rotate');
      if (pick === null) return;
      field = pick.trim();
      if (!names.includes(field)) { showToast('Pick one of the names listed.', { variant: 'error' }); return; }
    }
    const blankMakes = field === 'webhook_secret';
    const value = await showCustomPrompt(blankMakes ? 'New value (blank makes one)' : 'New value', '', `Rotate ${row.name}`, 'password');
    if (value === null || (!blankMakes && !value.trim())) return;
    const r = await apiFetch('/api/v1/integrations/credentials/rotate', { method: 'POST', body: JSON.stringify({ kind: row.kind, code: row.code, fields: { [field]: value.trim() } }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Not rotated.'); return; }
    const out = await r.json().catch(() => ({}));
    if (out.generated && out.generated[field]) await window.showOneTimeSecretDialog('New secret', 'Shown once. Store it now:', out.generated[field]);
    else showToast('Rotated.', { variant: 'success' });
    reload();
  }));
  container.querySelectorAll('[data-ic-active]').forEach(btn => btn.addEventListener('click', async () => {
    const row = rows[Number(btn.dataset.icActive)];
    const r = await apiFetch('/api/v1/integrations/credentials/active', { method: 'POST', body: JSON.stringify({ kind: row.kind, code: row.code, active: !row.active }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Not changed.'); return; }
    reload();
  }));
  container.querySelectorAll('[data-ic-disconnect]').forEach(btn => btn.addEventListener('click', async () => {
    const row = rows[Number(btn.dataset.icDisconnect)];
    if (!await showCustomConfirm(`Disconnect ${row.name}? The credential goes and its queued jobs are cancelled; mappings stay.`, 'Disconnect')) return;
    const r = await apiFetch('/api/v1/integrations/credentials/disconnect', { method: 'POST', body: JSON.stringify({ kind: row.kind, code: row.code }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Not disconnected.'); return; }
    const out = await r.json().catch(() => ({}));
    showToast(out.warning ? `Disconnected. ${out.warning}` : 'Disconnected.', { variant: out.warning ? 'warning' : 'success' });
    reload();
  }));
}
