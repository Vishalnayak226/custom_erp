// Stage 170.38 / 170.40: Configuration > Data governance (super admin) -
// retention per record type, and a masked copy of a client into a new sandbox.

export async function renderDataGovernanceView(container) {
  const res = await apiFetch('/api/v1/admin/data-governance/retention');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Retention could not be loaded.'); return; }
  const rows = (await res.json()) || [];
  const { txnPagedSection, txnRestorePane } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  const esc = escapeHTMLText;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div class="s-39ec6d">
      <div class="form-group"><label class="form-label" for="dg-type">Record type</label><input id="dg-type" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="dg-field">Field (blank = whole record)</label><input id="dg-field" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="dg-for">Keep for</label><input id="dg-for" type="number" class="form-input" min="0" value="90"></div>
      <div class="form-group"><label class="form-label" for="dg-unit">Unit</label>
        <select id="dg-unit" class="form-select"><option>days</option><option>months</option><option>years</option></select></div>
      <div class="form-group"><label class="form-label" for="dg-action">Then</label>
        <select id="dg-action" class="form-select"><option>clear field</option><option>purge</option><option>anonymise</option></select></div>
      <div class="form-group"><label class="form-label" for="dg-basis">Legal basis</label><input id="dg-basis" class="form-input" autocomplete="off"></div>
    </div>
    <button type="button" class="btn btn-primary" id="dg-save">Save rule</button>
    <hr class="dg-sep">
    <div class="s-39ec6d">
      <div class="form-group"><label class="form-label" for="dg-src">Masked copy of client</label><input id="dg-src" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="dg-ttl">Days to keep</label><input id="dg-ttl" type="number" class="form-input" min="1" value="14"></div>
    </div>
    <button type="button" class="btn btn-outline" id="dg-copy">Make masked copy</button>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = rows.length === 0 ? '<p class="text-muted s-287f77">None yet.</p>' : `
    <div class="table-wrapper"><table><thead><tr><th>Record type</th><th>Field</th><th>Keep</th><th>Then</th><th>Basis</th><th></th></tr></thead>
      <tbody>${rows.map((r, i) => `<tr><td>${esc(r.record_type)}</td><td>${esc(r.field_name || '—')}</td>
        <td>${esc(String(r.keep_for))} ${esc(r.keep_unit)}</td><td>${esc(r.then_action)}</td><td>${esc(r.legal_basis || '—')}</td>
        <td><button class="action-btn" data-dg-edit="${i}">Edit</button></td></tr>`).join('')}</tbody></table></div>`;

  container.appendChild(txnPagedSection({ id: 'data-governance', title: 'Data governance', page: 'Rule or copy', count: rows.length, newLabel: 'New', form, list }));
  txnRestorePane('data-governance');
  const reload = () => renderView('configuration:data-governance');
  const $ = (s) => form.querySelector(s);

  list.addEventListener('click', (e) => {
    const b = e.target.closest('[data-dg-edit]'); if (!b) return;
    const r = rows[Number(b.dataset.dgEdit)];
    $('#dg-type').value = r.record_type; $('#dg-field').value = r.field_name || '';
    $('#dg-for').value = r.keep_for; $('#dg-unit').value = r.keep_unit;
    $('#dg-action').value = r.then_action; $('#dg-basis').value = r.legal_basis || '';
    if (window.txnShowPane) window.txnShowPane('data-governance:Rule or copy');
  });
  $('#dg-save').addEventListener('click', async () => {
    const body = { record_type: $('#dg-type').value.trim(), field_name: $('#dg-field').value.trim(), keep_for: Number($('#dg-for').value),
      keep_unit: $('#dg-unit').value, then_action: $('#dg-action').value, legal_basis: $('#dg-basis').value.trim() };
    const r = await apiFetch('/api/v1/admin/data-governance/retention', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Could not save the rule.'); return; }
    showToast('Saved.'); reload();
  });
  $('#dg-copy').addEventListener('click', async () => {
    const src = $('#dg-src').value.trim();
    if (!src) { showToast('Enter the client to copy.'); return; }
    const r = await apiFetch('/api/v1/admin/data-governance/masked-copy', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source: src, ttl_days: Number($('#dg-ttl').value) || 14 }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The copy failed.'); return; }
    const out = await r.json();
    await window.showOneTimeSecretDialog('Sandbox ' + out.tenant_id, 'Shown once. Store it now:', out.admin_password);
  });
}
