// Stage 148.37: AI assistant access. Lazy module, appended under API access.
// One client = one named user; every call uses that user's rights. List
// first, the make form on its own page, the secret shown once, revoke here.

function aiWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : formatDateTime(d);
}

async function renderAIAccessPanel(container) {
  // Ask 191: with the module off the route refuses (403), so do not ask it.
  if (typeof isMenuModuleVisible === 'function' && !isMenuModuleVisible('ai_access')) return;
  const panel = document.createElement('div');
  panel.id = 'ai-access-panel';
  panel.style.marginTop = '24px';
  container.appendChild(panel);

  const res = await apiFetch('/api/v1/admin/ai-access/clients');
  if (!res) return;
  // Module off or not an admin: the panel stays out of the way.
  if (!res.ok) { panel.remove(); return; }
  const { clients = [] } = await res.json();
  const usersRes = await apiFetch('/api/v1/admin/users');
  const users = usersRes && usersRes.ok ? (await usersRes.json()).filter(u => u.status === 'Active') : [];
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div class="s-2ff289">
      <div class="form-group s-ef0b7a"><label class="form-label" for="ai-name">Name</label>
        <input id="ai-name" class="form-input s-0fac91"></div>
      <div class="form-group s-ef0b7a"><label class="form-label" for="ai-user">Acts as</label>
        <select id="ai-user" class="form-select s-30196d">${users.map(u =>
          `<option value="${escapeHTMLText(u.id)}">${escapeHTMLText(u.username)} · ${escapeHTMLText(u.role || '')}</option>`).join('')}</select></div>
    </div>
    <div class="s-1fac3d">
      <label class="s-5ef167"><input type="checkbox" class="ai-scope" value="ai:read" checked> Read</label>
      <label class="s-5ef167"><input type="checkbox" class="ai-scope" value="ai:draft"> Draft orders</label>
    </div>
    <button class="btn btn-primary" id="ai-issue">Make</button>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `
    <div class="table-wrapper"><table>
      <thead><tr><th>Name</th><th>Acts as</th><th>Rights</th><th>Status</th><th>Last used</th><th>Actions</th></tr></thead>
      <tbody>${clients.length === 0
        ? '<tr><td colspan="6" class="text-muted">None yet</td></tr>'
        : clients.map(c => `<tr>
          <td>${escapeHTMLText(c.name)}</td>
          <td>${escapeHTMLText(c.user_name || '')}${c.user_active ? '' : ' <span class="text-muted">(off)</span>'}</td>
          <td>${escapeHTMLText((c.scopes || []).map(s => s === 'ai:read' ? 'Read' : s === 'ai:draft' ? 'Draft orders' : s).join(', '))}</td>
          <td>${escapeHTMLText(c.status)}</td>
          <td>${escapeHTMLText(aiWhen(c.last_used_at))}</td>
          <td>${c.status === 'Active' ? `<button class="action-btn action-btn-danger" data-ai-revoke="${escapeHTMLText(c.credential_id)}">Revoke</button>` : ''}</td>
        </tr>`).join('')}</tbody>
    </table></div>`;

  panel.appendChild(txnPagedSection({ id: 'ai-access', title: 'AI assistants', page: 'New AI assistant', count: clients.length,
    newLabel: 'New AI assistant', form, list, heading: 'h2' }));
  txnRestorePane('ai-access');

  panel.querySelector('#ai-issue').addEventListener('click', async (event) => {
    const name = panel.querySelector('#ai-name').value.trim();
    const scopes = Array.from(panel.querySelectorAll('.ai-scope:checked')).map(b => b.value);
    const actingUser = panel.querySelector('#ai-user').value;
    if (!name) { showToast('Give it a name.'); panel.querySelector('#ai-name').focus(); return; }
    if (!actingUser) { showToast('Pick a user.'); return; }
    if (scopes.length === 0) { showToast('Tick one right.'); return; }
    await guardAgainstDoubleSubmit(event.currentTarget, 'Making…', async () => {
      const r = await apiFetch('/api/v1/admin/ai-access/clients', { method: 'POST', body: JSON.stringify({ name, acting_user: actingUser, scopes }) });
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'Not made.'); return; }
      const body = await r.json();
      await window.showOneTimeSecretDialog('AI assistant made',
        `Client ID: ${body.client_id}. Token URL: ${body.token_endpoint}. Secret, shown once:`, body.client_secret);
      txnDone('ai-access');
      renderView('api-access');
    });
  });

  panel.querySelectorAll('[data-ai-revoke]').forEach(btn => btn.addEventListener('click', async () => {
    if (!await showCustomConfirm('Revoke this assistant? Its tokens stop now.')) return;
    const r = await apiFetch(`/api/v1/admin/api-credentials/${encodeURIComponent(btn.dataset.aiRevoke)}`, { method: 'DELETE' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Not revoked.'); return; }
    showToast('Revoked.');
    renderView('api-access');
  }));
}

export { renderAIAccessPanel };
