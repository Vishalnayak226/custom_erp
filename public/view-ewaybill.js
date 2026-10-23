// Stage 130.16 (74.4, the e-way bill side): the E-Way Bills workbench -
// Financial Accounting ▸ E-Way Bills. Every delivery challan, sales invoice
// and stock transfer is decided (needs a bill or not, and why); a bill is
// generated through the provider, or - until one is connected - made on the
// e-way bill portal and recorded here by its number; the vehicle can be
// updated, the bill extended near its expiry and cancelled within 24 hours.
const esc = (s) => escapeHTMLText(String(s ?? ''));
const TABS = [['required', 'Required'], ['generated', 'Generated'], ['expiring', 'Expiring'], ['failed', 'Failed'], ['not_required', 'Not required'], ['all', 'All']];
const LABEL = { DeliveryChallan: 'Delivery challan', SalesInvoice: 'Sales invoice', TransferOrder: 'Stock transfer' };
let ewbTab = 'required';

export async function renderEWBView(container) {
  const res = await apiFetch(`/api/v1/gst/ewaybills?tab=${encodeURIComponent(ewbTab)}`);
  if (!res || !res.ok) { renderErrorPanel(container, 'Failed to load e-way bills.', () => renderView('einvoice:ewb')); return; }
  const { rows = [], counts = {} } = await res.json();
  const when = (v) => { if (!v) return ''; const d = new Date(v); return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(); };
  const badge = { Ready: 'badge-warning', Submitted: 'badge-warning', Generated: 'badge-success', Reconciled: 'badge-success', Failed: 'badge-danger', Expired: 'badge-danger', Cancelled: 'badge-secondary', NotRequired: 'badge-secondary' };
  const el = document.createElement('div');
  el.className = 'txn-screen';
  el.innerHTML = `<div class="txn-bar"><h1 class="page-title txn-title">E-Way Bills</h1><div class="txn-actions"><button type="button" class="btn btn-outline" data-ewb-irn>E-Invoice (IRN)</button></div></div>
    <div style="display:flex;gap:6px;flex-wrap:wrap;margin:6px 0 12px;">${TABS.map(([k, l]) => `<button type="button" class="btn btn-sm ${ewbTab === k ? 'btn-primary' : 'btn-outline'}" data-ewb-tab="${k}">${l} <b>${counts[k] || 0}</b></button>`).join('')}</div>
    <div class="txn-grid"><table><thead><tr><th>Document</th><th>From → to</th><th class="num">Value</th><th>Decision</th><th>E-way bill</th><th>Valid until</th><th>Vehicle</th><th></th></tr></thead><tbody>
    ${rows.length ? rows.map((r, i) => `<tr><td>${esc(LABEL[r.doctype] || r.doctype)}<div style="font-family:monospace;font-size:12px;">${esc(r.doc_number || r.doc_id)}</div></td>
      <td>${esc(r.from_state || '?')} → ${esc(r.to_state || '?')}</td><td class="num">${formatMoney(r.value)}</td>
      <td style="max-width:240px;"><span class="text-muted" style="font-size:12px;">${esc((r.decision || {}).reason || '')}</span></td>
      <td><span class="badge ${badge[r.status] || 'badge-secondary'}">${esc(r.status)}</span>${r.ewb_no ? `<div style="font-family:monospace;">${esc(r.ewb_no)}</div>` : ''}${r.source === 'portal' ? '<div class="text-muted" style="font-size:12px;">from the portal</div>' : ''}${r.last_error ? `<div style="color:var(--danger-strong);font-size:12px;max-width:240px;">${esc(r.last_error)}</div>` : ''}</td>
      <td>${esc(when(r.valid_upto))}${r.expiring ? ' <span class="badge badge-warning">expiring</span>' : ''}${r.extensions ? `<div class="text-muted" style="font-size:12px;">extended ${r.extensions}×</div>` : ''}</td>
      <td>${esc(r.vehicle_no || r.transporter_id || '')}</td>
      <td style="white-space:nowrap;">${['Ready', 'Submitted', 'Failed'].includes(r.status) ? `<button type="button" class="action-btn" data-ewb="generate" data-i="${i}">Generate</button> <button type="button" class="action-btn" data-ewb="record" data-i="${i}">Record from portal</button>` : ''}
        ${['Generated', 'Reconciled'].includes(r.status) ? `<button type="button" class="action-btn" data-ewb="vehicle" data-i="${i}">Vehicle</button> <button type="button" class="action-btn" data-ewb="extend" data-i="${i}">Extend</button> <button type="button" class="action-btn action-btn-danger" data-ewb="cancel" data-i="${i}">Cancel</button>` : ''}
        ${r.status === 'Expired' ? `<button type="button" class="action-btn" data-ewb="extend" data-i="${i}">Extend</button>` : ''}</td></tr>`).join('')
      : '<tr><td colspan="8" style="text-align:center;color:var(--text-muted);padding:20px;">Nothing here. Delivery challans, sales invoices and stock transfers of the last 30 days are decided here as they are approved.</td></tr>'}</tbody></table></div>
    <div data-ewb-pane style="margin-top:14px;"></div>`;
  container.appendChild(el);
  el.querySelector('[data-ewb-irn]').addEventListener('click', () => renderView('einvoice'));
  el.querySelectorAll('[data-ewb-tab]').forEach(b => b.addEventListener('click', () => { ewbTab = b.dataset.ewbTab; renderView('einvoice:ewb'); }));
  const pane = el.querySelector('[data-ewb-pane]');
  const field = (id, label, width, extra = '') => `<div class="form-group" style="margin:0;"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" style="width:${width}px;" ${extra}></div>`;
  const send = async (r, action, body, ok) => {
    const res = await apiFetch(`/api/v1/gst/ewaybills/${encodeURIComponent(r.doctype)}/${encodeURIComponent(r.doc_id)}/${action}`, { method: 'POST', body: JSON.stringify(body) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'That did not go through.'); renderView('einvoice:ewb'); return; }
    showToast(ok, { variant: 'success' });
    renderView('einvoice:ewb');
  };
  el.querySelectorAll('[data-ewb]').forEach(b => b.addEventListener('click', async () => {
    const r = rows[Number(b.dataset.i)];
    const act = b.dataset.ewb;
    const title = `<h3 style="margin:0 0 8px;">${esc(LABEL[r.doctype] || r.doctype)} ${esc(r.doc_number)}</h3>`;
    const partB = `<div class="form-group" style="margin:0;"><label class="form-label" for="ewb-mode">Mode</label><select class="form-select" id="ewb-mode" style="width:110px;"><option>Road</option><option>Rail</option><option>Air</option><option>Ship</option></select></div>
      ${field('ewb-vehicle', 'Vehicle no', 130, `value="${esc(r.vehicle_no || '')}" placeholder="KA01AB1234"`)}${field('ewb-tid', 'Transporter ID', 170, 'placeholder="or 15-character ID"')}`;
    const vals = () => ({ transport_mode: pane.querySelector('#ewb-mode')?.value, vehicle_no: pane.querySelector('#ewb-vehicle')?.value.trim(), transporter_id: pane.querySelector('#ewb-tid')?.value.trim(),
      distance_km: Number(pane.querySelector('#ewb-km')?.value) || 0 });
    if (act === 'cancel') {
      const why = String((await showCustomPrompt('Why is this e-way bill cancelled? (within 24 hours of generating it)', '', 'Cancel e-way bill')) || '').trim();
      if (why) send(r, 'cancel', { reason: why }, 'E-way bill cancelled.');
      return;
    }
    const forms = {
      generate: [`${partB}${field('ewb-km', 'Distance (km)', 100, 'type="number" min="1"')}`, 'Generate'],
      record: [`${field('ewb-no', 'E-way bill no (12 digits)', 170, 'maxlength="12" inputmode="numeric"')}${field('ewb-until', 'Valid until', 190, 'type="datetime-local"')}${field('ewb-vehicle', 'Vehicle no', 130)}${field('ewb-km', 'Distance (km)', 100, 'type="number" min="1"')}`, 'Record'],
      vehicle: [partB, 'Update vehicle'],
      extend: [`${field('ewb-rem', 'Distance still to go (km)', 160, 'type="number" min="1"')}<div class="form-group" style="margin:0;flex:1;min-width:200px;"><label class="form-label" for="ewb-why">Why</label><input class="form-input" id="ewb-why" placeholder="e.g. vehicle breakdown"></div>`, 'Extend']
    };
    const [inner, label] = forms[act];
    pane.innerHTML = `${title}<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">${inner}<button type="button" class="btn btn-primary" data-ewb-go>${label}</button></div>
      ${act === 'record' ? '<div class="txn-hint" style="margin-top:6px;">Made on the e-way bill portal? Record its number and validity so the register and the expiry watch know it.</div>' : ''}
      ${act === 'extend' ? '<div class="txn-hint" style="margin-top:6px;">Allowed within 8 hours before or after it expires.</div>' : ''}`;
    pane.querySelector('[data-ewb-go]').addEventListener('click', () => {
      if (act === 'generate') send(r, 'generate', vals(), 'E-way bill generated.');
      if (act === 'record') send(r, 'record', { ...vals(), ewb_no: pane.querySelector('#ewb-no').value.trim(), valid_upto: pane.querySelector('#ewb-until').value }, 'E-way bill recorded.');
      if (act === 'vehicle') send(r, 'vehicle', vals(), 'Vehicle updated.');
      if (act === 'extend') send(r, 'extend', { remaining_km: Number(pane.querySelector('#ewb-rem').value) || 0, reason: pane.querySelector('#ewb-why').value.trim() }, 'E-way bill extended.');
    });
  }));
}
