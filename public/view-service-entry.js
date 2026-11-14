// Service entry sheets: the work done against a service PO. List first; New
// is its own page. Accepting a sheet is what the vendor bill is matched to.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

async function renderServiceEntryView(container) {
  const res = await apiFetch('/api/v1/doc/ServiceEntrySheet');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let sourcePO = '';
  try { sourcePO = sessionStorage.getItem('erp-service-entry-po') || ''; sessionStorage.removeItem('erp-service-entry-po'); } catch (_) { /* storage can be disabled */ }

  const form = document.createElement('div');
  form.className = 'table-panel se-form';
  form.innerHTML = `<div class="se-head">
      <div class="form-group"><label class="form-label" for="se-po">Service PO</label><input id="se-po" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="se-date">Date</label><input id="se-date" type="date" class="form-input"></div>
      <div class="form-group"><label class="form-label" for="se-notes">Notes</label><input id="se-notes" class="form-input" autocomplete="off"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Ordered</th><th class="num">Accepted</th><th class="num">Left</th><th class="num">This entry</th></tr></thead>
      <tbody id="se-lines"><tr><td colspan="5" class="text-muted">Pick a service PO.</td></tr></tbody></table></div>
    <div class="pc-actions"><button class="btn btn-primary" id="se-save" type="button">Save</button></div>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table><thead><tr><th>Sheet</th><th>Service PO</th><th>Date</th><th>Store</th><th>Status</th><th></th></tr></thead><tbody>${rows.length === 0
    ? '<tr><td colspan="6" class="text-muted">No service entries yet.</td></tr>'
    : rows.map(r => `<tr><td>${escapeHTMLText(r.code || r.id)}</td><td>${escapeHTMLText(r.po_id || '')}</td><td>${escapeHTMLText(r.entry_date || '')}</td>
      <td>${escapeHTMLText(r.location || '')}</td><td><span class="badge ${r.status === 'Accepted' ? 'badge-success' : 'badge-warning'}">${escapeHTMLText(r.status || '')}</span></td>
      <td>${r.status === 'Draft' ? `<button class="action-btn" data-se-accept="${escapeHTMLText(r.id)}">Accept</button>` : ''}</td></tr>`).join('')}</tbody></table>`;

  container.appendChild(txnPagedSection({ id: 'service-entry-screen', title: 'Service entries', page: 'New service entry', count: rows.length,
    newLabel: 'New service entry', form, list }));
  txnRestorePane('service-entry-screen');

  const po = document.getElementById('se-po');
  const body = document.getElementById('se-lines');
  document.getElementById('se-date').value = new Date().toLocaleDateString('en-CA');
  attachLinkTypeahead(po, 'PurchaseOrder');

  async function loadLines() {
    const id = po.value.trim();
    if (!id) { body.innerHTML = '<tr><td colspan="5" class="text-muted">Pick a service PO.</td></tr>'; return; }
    const r = await apiFetch(`/api/v1/procurement/service-entry/po/${encodeURIComponent(id)}`);
    if (!r) return;
    if (!r.ok) { body.innerHTML = '<tr><td colspan="5" class="text-muted">No lines.</td></tr>'; await showApiError(r, 'The service PO could not be read.'); return; }
    const lines = (await r.json()).lines || [];
    body.innerHTML = lines.length === 0 ? '<tr><td colspan="5" class="text-muted">No lines.</td></tr>'
      : lines.map(l => `<tr><td>${escapeHTMLText(l.sku)}</td><td class="num">${l.ordered}</td><td class="num">${l.accepted}</td><td class="num">${l.remaining}</td>
        <td class="num"><input type="number" min="0" step="any" class="form-input num se-qty" data-se-sku="${escapeHTMLText(l.sku)}"></td></tr>`).join('');
  }
  po.addEventListener('change', loadLines);
  if (sourcePO) { po.value = sourcePO; await loadLines(); window.txnShowPane('service-entry-screen:new'); }

  list.querySelectorAll('[data-se-accept]').forEach(b => b.addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Accepting...', async () => {
    const r = await apiFetch(`/api/v1/procurement/service-entry/${encodeURIComponent(b.dataset.seAccept)}/accept`, { method: 'POST' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The service entry was not accepted.'); return; }
    showToast('Service entry accepted.', { variant: 'success' });
    renderView('service-entry');
  })));

  document.getElementById('se-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const lines = [...document.querySelectorAll('.se-qty')].map(i => ({ sku: i.dataset.seSku, qty: Number(i.value) || 0 })).filter(l => l.qty > 0);
    if (!po.value.trim() || lines.length === 0) { showToast('Pick a service PO and enter a quantity.'); return; }
    const r = await apiFetch('/api/v1/doc/ServiceEntrySheet', { method: 'POST', body: JSON.stringify({
      po_id: po.value.trim(), entry_date: document.getElementById('se-date').value, notes: document.getElementById('se-notes').value.trim(), status: 'Draft', lines: JSON.stringify(lines) }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The service entry was not saved.'); return; }
    txnDone('service-entry-screen');
    renderView('service-entry');
  }));
}

export { renderServiceEntryView };
