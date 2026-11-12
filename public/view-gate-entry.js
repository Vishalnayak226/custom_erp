// Stage 146.3.8: inward gate-entry register. List first; + New gate entry is
// its own page. "Start GRN" opens Goods Receipt with the entry carried across.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const GE_FIELDS = [['ge-vendor', 'Vendor', 'vendor'], ['ge-po', 'PO', 'po_id'], ['ge-location', 'Location', 'location_code'],
  ['ge-parcels', 'Parcels', 'parcels'], ['ge-transporter', 'Transporter', 'transporter'], ['ge-lr', 'LR no', 'lr_no'],
  ['ge-vehicle', 'Vehicle no', 'vehicle_number'], ['ge-remarks', 'Remarks', 'remarks']];

async function renderGateEntryView(container) {
  const res = await apiFetch('/api/v1/doc/GateEntry');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `<div class="s-97380c">
    ${GE_FIELDS.map(([id, label]) => `<div class="form-group s-ef0b7a"><label class="form-label" for="${id}">${label}</label>
      <input id="${id}" class="form-input s-7c07cd" ${id === 'ge-parcels' ? 'type="number" min="0"' : ''} autocomplete="off"></div>`).join('')}
    <div class="form-group s-ef0b7a"><label class="form-label" for="ge-photo">Photo</label>
      <input id="ge-photo" type="file" class="form-input s-2acaf3" accept="image/jpeg,image/png,image/webp,application/pdf"></div>
    <button class="btn btn-primary" id="ge-save">Save</button></div>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table><thead><tr><th>Entry</th><th>Vendor</th><th>PO</th><th>Location</th><th>Parcels</th><th>Transporter</th><th>LR no</th><th>Vehicle</th><th>Status</th><th>GRN</th><th></th></tr></thead><tbody>${rows.length === 0
    ? '<tr><td colspan="11" class="text-muted s-91a870">No gate entries yet.</td></tr>'
    : rows.map(r => `<tr><td class="s-72126d">${escapeHTMLText(r.code || r.id)}</td><td>${escapeHTMLText(r.vendor || '')}</td><td>${escapeHTMLText(r.po_id || '')}</td>
      <td>${escapeHTMLText(r.location_code || '')}</td><td>${escapeHTMLText(r.parcels || '')}</td><td>${escapeHTMLText(r.transporter || '')}</td>
      <td>${escapeHTMLText(r.lr_no || '')}</td><td>${escapeHTMLText(r.vehicle_number || '')}</td><td>${escapeHTMLText(r.status || '')}</td>
      <td>${escapeHTMLText(r.grn_id || '')}</td>
      <td>${r.status === 'Open' ? `<button class="action-btn" data-ge-grn="${escapeHTMLText(r.id)}">Start GRN</button>` : ''}</td></tr>`).join('')}</tbody></table>`;

  container.appendChild(txnPagedSection({ id: 'gate-entry-screen', title: 'Gate entries', page: 'New gate entry', count: rows.length,
    newLabel: 'New gate entry', form, list }));
  txnRestorePane('gate-entry-screen');
  attachLinkTypeahead(document.getElementById('ge-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('ge-location'), 'Location');

  list.querySelectorAll('[data-ge-grn]').forEach(b => b.addEventListener('click', () => {
    try { sessionStorage.setItem('erp-grn-gate-entry', b.dataset.geGrn); } catch (_) { /* storage can be disabled */ }
    renderView('grn');
  }));

  document.getElementById('ge-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const body = {};
    GE_FIELDS.forEach(([id, , key]) => { const v = document.getElementById(id).value.trim(); if (v) body[key] = key === 'parcels' ? Number(v) : v; });
    if (!body.vendor && !body.po_id) { showToast('Enter the vendor or the PO.'); return; }
    const r = await apiFetch('/api/v1/doc/GateEntry', { method: 'POST', body: JSON.stringify(body) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The gate entry was not saved.'); return; }
    const saved = await r.json().catch(() => ({}));
    const file = document.getElementById('ge-photo').files[0];
    if (file && saved.id) {
      const fd = new FormData();
      fd.append('file', file, file.name);
      const up = await apiUpload(`/api/v1/attachments/GateEntry/${encodeURIComponent(saved.id)}`, fd);
      if (up && !up.ok) showToast(`Saved, but the photo was not attached: ${await getErrorMessage(up, 'it could not be attached')}`);
    }
    txnDone('gate-entry-screen');
    renderView('gate-entry');
  }));
}

export { renderGateEntryView };
