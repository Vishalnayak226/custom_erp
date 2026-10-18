// Supply Proposals (Stage 73.2.18) - Manufacturing → Supply Proposals.
//
// Reorder and MRP suggestions used to be read-only lists. Here the planner
// turns them into proposals for a location, then approves a selection as one
// action - a Draft purchase order to a vendor, a Draft stock transfer from
// another location, or a Draft production order per item on its BOM - or
// rejects them with a reason. Each proposal converts once and shows the
// document it became.
// BLD-041: native ES module loaded with import() by the Manufacturing view.

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const num = v => { const x = Number(v) || 0; return x === Math.round(x) ? String(x) : x.toFixed(2); };
let planLocation = '';
let planStatus = 'Proposed';

async function renderSupplyProposalsTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.style.marginBottom = '16px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-location">Location</label>
        <input type="text" id="sp-location" class="form-input" style="width: 180px;" autocomplete="off" value="${esc(planLocation)}"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-source">Propose from</label>
        <select id="sp-source" class="form-select"><option value="reorder">Reorder points (all stock items)</option><option value="mrp">MRP (manufactured items and their materials)</option></select></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-lead">Lead time (days)</label>
        <input type="number" id="sp-lead" class="form-input" style="width: 90px;" min="1" value="7"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-safety">Safety stock</label>
        <input type="number" id="sp-safety" class="form-input" style="width: 90px;" min="0" value="0"></div>
      <button class="btn btn-primary" id="sp-generate" type="button">Generate proposals</button>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-status">Show</label>
        <select id="sp-status" class="form-select">${['Proposed', 'Converted', 'Rejected', ''].map(s => `<option value="${s}"${s === planStatus ? ' selected' : ''}>${s || 'All'}</option>`).join('')}</select></div>
    </div>
    <p style="margin: 8px 0 0; color: var(--text-muted);">A proposal is kept until you decide it; generating again refreshes the open ones instead of adding duplicates. An item's own reorder point (Setup → Reorder Point Config) wins over the lead time and safety stock here.</p>`;
  container.appendChild(panel);
  attachLinkTypeahead(document.getElementById('sp-location'), 'Location');
  document.getElementById('sp-generate').addEventListener('click', generateProposals);
  document.getElementById('sp-status').addEventListener('change', e => { planStatus = e.target.value; loadProposals(); });
  document.getElementById('sp-location').addEventListener('change', e => { planLocation = e.target.value.trim(); loadProposals(); });

  const list = document.createElement('div');
  list.id = 'sp-list';
  container.appendChild(list);
  await loadProposals();
}

async function generateProposals() {
  const location = document.getElementById('sp-location').value.trim();
  if (!location) { await showCustomAlert('Choose the location to plan for.', 'Supply Proposals'); return; }
  planLocation = location;
  const btn = document.getElementById('sp-generate');
  await guardAgainstDoubleSubmit(btn, 'Generating...', async () => {
    const res = await apiFetch('/api/v1/planning/proposals/generate', { method: 'POST', body: JSON.stringify({
      location, source: document.getElementById('sp-source').value,
      lead_time_days: parseInt(document.getElementById('sp-lead').value, 10) || 7,
      safety_stock: parseInt(document.getElementById('sp-safety').value, 10) || 0 }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Proposals could not be generated.', 'Supply Proposals'); return; }
    const out = await res.json();
    showToast(`${out.created} new proposal${out.created === 1 ? '' : 's'}, ${out.refreshed} refreshed.`, { variant: 'success' });
    planStatus = 'Proposed';
    document.getElementById('sp-status').value = 'Proposed';
    await loadProposals();
  });
}

async function loadProposals() {
  const el = document.getElementById('sp-list');
  if (!el) return;
  const q = new URLSearchParams();
  const loc = (document.getElementById('sp-location')?.value || planLocation).trim();
  if (loc) q.set('location', loc);
  if (planStatus) q.set('status', planStatus);
  const res = await apiFetch(`/api/v1/planning/proposals?${q}`);
  if (!res) return;
  if (!res.ok) { el.innerHTML = '<div class="login-error">Proposals could not be loaded.</div>'; return; }
  const props = await res.json();
  const open = props.filter(p => p.status === 'Proposed');
  el.innerHTML = `
    <div class="table-panel" style="overflow-x: auto;"><table>
      <thead><tr>${open.length ? '<th><input type="checkbox" id="sp-all" aria-label="Select all open proposals"></th>' : ''}<th>Item</th><th>Location</th><th>From</th><th class="num">Available</th><th class="num">Needed</th><th class="num">Suggested</th><th class="num">Quantity</th><th>Supply by</th><th>Status</th><th>Became / why</th></tr></thead>
      <tbody>${props.length === 0
        ? `<tr><td colspan="11" style="text-align:center; color:var(--text-muted);">No ${planStatus ? planStatus.toLowerCase() + ' ' : ''}proposals${loc ? ` at ${esc(loc)}` : ''}. Choose a location and <b>Generate proposals</b>.</td></tr>`
        : props.map(p => `<tr data-id="${esc(p.id)}">
          ${open.length ? `<td>${p.status === 'Proposed' ? `<input type="checkbox" class="sp-pick" aria-label="Select ${esc(p.item_name || p.sku)}">` : ''}</td>` : ''}
          <td>${esc(p.item_name || p.sku)}<div style="font-size: 11px; color: var(--text-muted);">${esc(p.sku)}</div></td>
          <td>${esc(p.location)}</td>
          <td>${esc(p.source)}${p.for_item ? `<div style="font-size: 11px;">for ${esc(p.for_item)}</div>` : ''}</td>
          <td class="num">${num(p.available)}</td><td class="num">${num(p.reorder_point)}</td><td class="num">${num(p.suggested_qty)}</td>
          <td class="num">${p.status === 'Proposed' ? `<input type="number" class="form-input sp-qty" style="width: 90px;" min="0" value="${num(p.suggested_qty)}" aria-label="Quantity for ${esc(p.sku)}">` : num(p.approved_qty)}</td>
          <td>${esc(p.action || '')}</td>
          <td><span class="badge ${p.status === 'Converted' ? 'badge-success' : p.status === 'Rejected' ? 'badge-danger' : 'badge-warning'}">${esc(p.status)}</span></td>
          <td>${p.target_id ? `${esc(p.target_doctype)} <b>${esc(p.target_id)}</b>` : ''}${p.reason ? `<div style="font-size: 11.5px;">${esc(p.reason)}</div>` : ''}${p.decided_by ? `<div style="font-size: 11px; color: var(--text-muted);">${esc(p.decided_by)} · ${esc((p.decided_at || '').slice(0, 10))}</div>` : ''}</td>
        </tr>`).join('')}</tbody></table></div>
    ${open.length ? `<div class="table-panel" style="padding: 16px; margin-top: 12px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-action">Supply the selected by</label>
        <select id="sp-action" class="form-select"><option value="Purchase">Purchase order</option><option value="Transfer">Transfer from another location</option><option value="Production">Production order</option></select></div>
      <div class="form-group" style="margin-bottom: 0;" id="sp-vendor-wrap"><label class="form-label" for="sp-vendor">Vendor</label>
        <input type="text" id="sp-vendor" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group hidden" style="margin-bottom: 0;" id="sp-from-wrap"><label class="form-label" for="sp-from">From location</label>
        <input type="text" id="sp-from" class="form-input" style="width: 180px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="sp-note">Note / reason</label>
        <input type="text" id="sp-note" class="form-input" style="width: 220px;" maxlength="200"></div>
      <button class="btn btn-primary" type="button" id="sp-approve">Approve selected</button>
      <button class="btn btn-outline" type="button" id="sp-reject">Reject selected</button>
    </div>` : ''}`;
  if (!open.length) return;
  attachLinkTypeahead(document.getElementById('sp-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('sp-from'), 'Location');
  document.getElementById('sp-all').addEventListener('change', e => el.querySelectorAll('.sp-pick').forEach(c => { c.checked = e.target.checked; }));
  const actionEl = document.getElementById('sp-action');
  actionEl.addEventListener('change', () => {
    document.getElementById('sp-vendor-wrap').classList.toggle('hidden', actionEl.value !== 'Purchase');
    document.getElementById('sp-from-wrap').classList.toggle('hidden', actionEl.value !== 'Transfer');
  });
  const picked = () => [...el.querySelectorAll('tr[data-id]')].filter(tr => tr.querySelector('.sp-pick')?.checked);
  document.getElementById('sp-approve').addEventListener('click', () => guardAgainstDoubleSubmit(document.getElementById('sp-approve'), 'Approving...', async () => {
    const rows = picked();
    if (!rows.length) { await showCustomAlert('Tick the proposals to approve.', 'Supply Proposals'); return; }
    const quantities = {};
    rows.forEach(tr => { quantities[tr.dataset.id] = parseFloat(tr.querySelector('.sp-qty').value) || 0; });
    const res = await apiFetch('/api/v1/planning/proposals/approve', { method: 'POST', body: JSON.stringify({
      ids: rows.map(tr => tr.dataset.id), action: actionEl.value, vendor: document.getElementById('sp-vendor').value.trim(),
      from_location: document.getElementById('sp-from').value.trim(), quantities, note: document.getElementById('sp-note').value.trim() }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The proposals could not be approved.', 'Supply Proposals'); return; }
    const out = await res.json();
    const where = { PurchaseOrder: 'Procurement → Purchase Orders', TransferOrder: 'Stock → Stock Transfer', ProductionOrder: 'Manufacturing → Orders' }[out.doctype] || '';
    await showCustomAlert(`Created ${out.documents.length === 1 ? 'Draft' : out.documents.length + ' Draft'} ${out.doctype.replace(/([a-z])([A-Z])/g, '$1 $2').toLowerCase()}${out.documents.length === 1 ? '' : 's'}: ${out.documents.join(', ')}. Review and submit ${out.documents.length === 1 ? 'it' : 'them'} on ${where}.`, 'Proposals Approved');
    await loadProposals();
  }));
  document.getElementById('sp-reject').addEventListener('click', () => guardAgainstDoubleSubmit(document.getElementById('sp-reject'), 'Rejecting...', async () => {
    const rows = picked();
    if (!rows.length) { await showCustomAlert('Tick the proposals to reject.', 'Supply Proposals'); return; }
    let reason = document.getElementById('sp-note').value.trim();
    if (!reason) {
      reason = await showCustomPrompt('Why are these proposals rejected?', '', 'Reject Proposals');
      if (!reason || !reason.trim()) return;
    }
    const res = await apiFetch('/api/v1/planning/proposals/reject', { method: 'POST', body: JSON.stringify({ ids: rows.map(tr => tr.dataset.id), reason: reason.trim() }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The proposals could not be rejected.', 'Supply Proposals'); return; }
    showToast(`${rows.length} proposal${rows.length === 1 ? '' : 's'} rejected.`);
    await loadProposals();
  }));
}

export { renderSupplyProposalsTab };
