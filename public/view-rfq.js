// RFQ / Vendor Quotes (Stage 73.2.1-73.2.3, decision D06) - its own lazy
// module (it used to live inside view-reports.js).
//
// An RFQ asks for item lines (an Item or free text, a unit and a quantity)
// from invited vendors, each with a response status (Invited / Responded /
// Declined) and a due date. Each vendor's quote prices every line in the
// vendor's own unit (converted to the RFQ's), with tax and lead time, plus
// freight, payment terms and validity; quotes are compared on their landed
// total, line by line. Awarding a quote creates its Draft purchase order -
// once - and the RFQ, quote and PO point at each other.
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
import { documentSearchLink } from './view-document-links.js';

const loadTxnScreen = () => import('./txn-screen.js' + new URL(import.meta.url).search);
let selectedRFQId = '';
let rfqDraftLines = [];
let rfqEditingId = '';
let rfqCompare = null;

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const parseList = s => { try { const v = typeof s === 'string' ? JSON.parse(s || '[]') : s; return Array.isArray(v) ? v : []; } catch (e) { return []; } };
const money = v => formatMoney(Number(v) || 0);
const num = v => { const x = Number(v) || 0; return x === Math.round(x) ? String(x) : x.toFixed(2); };

async function postJSON(path, payload) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return { ok: false, message: 'No response from the server.' };
  if (res.ok) return { ok: true, body: await res.json() };
  try {
    const b = await res.json();
    return { ok: false, message: b.detail || b.message || b.error || `Failed (${res.status}).` };
  } catch (e) { return { ok: false, message: `Failed (${res.status}).` }; }
}

async function renderRFQView(container) {
  const res = await apiFetch('/api/v1/doc/RFQ');
  if (!res) return;
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  const rfqs = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${rfqEditingId ? `<div class="form-group" style="margin-bottom: 0;"><span class="form-label">RFQ</span><div style="font-family: monospace; padding: 8px 0;">${esc(rfqEditingId)}</div></div>` : autoNumberField('RFQ Number', 'RFQ', '160px')}
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-location">Deliver to</label>
        <input type="text" id="rfq-location" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-due">Quotes due by</label>
        <input type="date" id="rfq-due" class="form-input"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-target-date">Goods needed by</label>
        <input type="date" id="rfq-target-date" class="form-input"></div>
    </div>
    <h3 style="font-size: 14px; font-weight: 700; margin: 18px 0 8px;">Items asked for</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-line-item">Item (optional)</label>
        <input type="text" id="rfq-line-item" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 180px;"><label class="form-label" for="rfq-line-desc">Description</label>
        <input type="text" id="rfq-line-desc" class="form-input" placeholder="Filled from the item, or describe it"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-line-uom">Unit</label>
        <input type="text" id="rfq-line-uom" class="form-input" style="width: 90px;" value="Nos"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-line-qty">Qty</label>
        <input type="number" id="rfq-line-qty" class="form-input" style="width: 100px;" min="0"></div>
      <button class="btn btn-outline" id="rfq-line-add" type="button">Add line</button>
    </div>
    <div id="rfq-lines" style="margin-top: 12px;"></div>
    <div style="display: flex; gap: 12px; margin-top: 16px;">
      <button class="btn btn-primary" id="rfq-create-btn">${rfqEditingId ? 'Save lines' : 'Create RFQ'}</button>
      ${rfqEditingId ? `<button class="btn btn-outline" type="button" ${actionAttrs('cancelRFQEdit', [])}>Start a new RFQ instead</button>` : ''}
    </div>
    <div id="rfq-form-error" class="login-error hidden" style="margin-top: 16px;"></div>`;

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.style.marginBottom = '24px';
  listPanel.innerHTML = `<table>
    <thead><tr><th>RFQ Number</th><th>Source requisition</th><th>Items</th><th>Deliver to</th><th>Quotes due</th><th>Vendors</th><th>Status</th><th></th></tr></thead>
    <tbody>${rfqs.length === 0
      ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No RFQs yet. Convert an approved requisition to an RFQ, or click <b>+ New RFQ</b>.</td></tr>`
      : rfqs.map(r => {
        const lines = parseList(r.lines);
        const inv = parseList(r.vendor_invitations).length ? parseList(r.vendor_invitations) : parseList(r.invited_vendors).map(v => ({ vendor: v, status: 'Invited' }));
        const responded = inv.filter(i => i.status === 'Responded').length;
        return `<tr${r.id === selectedRFQId ? ' style="background: var(--hover-bg, rgba(0,0,0,0.03));"' : ''}>
          <td style="font-family: monospace;">${esc(r.code || r.id)}</td>
          <td>${documentSearchLink('PurchaseRequisition', r.source_requisition_id)}</td>
          <td>${esc(r.description || '')}${lines.length ? `<div style="font-size: 11.5px; color: var(--text-muted);">${lines.length} line${lines.length === 1 ? '' : 's'}</div>` : ''}</td>
          <td>${esc(r.location || '')}</td>
          <td>${esc(r.response_due_date || r.target_date || '')}</td>
          <td>${inv.length ? `${responded}/${inv.length} quoted` : '<span style="color: var(--text-muted);">none</span>'}</td>
          <td><span class="badge ${r.status === 'Closed' ? 'badge-success' : r.status === 'Sent' ? 'badge-warning' : 'badge-secondary'}">${esc(r.status || '')}</span>${r.awarded_po_id ? `<div style="font-size: 11.5px;">PO ${esc(r.awarded_po_id)}</div>` : ''}</td>
          <td style="white-space: nowrap;"><button class="action-btn" ${actionAttrs('viewRFQQuotes', [r.id])}>Open</button></td>
        </tr>`;
      }).join('')}</tbody></table>`;

  container.appendChild(txnPagedSection({ id: 'rfq-screen', title: 'RFQ / Quotes', page: rfqEditingId ? 'Edit RFQ lines' : 'New RFQ', count: rfqs.length,
    newLabel: 'New RFQ', form: formPanel, list: listPanel }));
  txnRestorePane('rfq-screen');

  attachLinkTypeahead(document.getElementById('rfq-location'), 'Location');
  const itemInput = document.getElementById('rfq-line-item');
  attachLinkTypeahead(itemInput, 'Item');
  document.getElementById('rfq-line-add').addEventListener('click', addRFQLine);
  document.getElementById('rfq-create-btn').addEventListener('click', () =>
    guardAgainstDoubleSubmit(document.getElementById('rfq-create-btn'), 'Saving...', saveRFQForm));
  if (rfqEditingId) {
    const r = rfqs.find(x => x.id === rfqEditingId);
    if (r) {
      document.getElementById('rfq-location').value = r.location || '';
      document.getElementById('rfq-due').value = r.response_due_date || '';
      document.getElementById('rfq-target-date').value = r.target_date || '';
    }
  }
  renderRFQDraftLines();

  if (selectedRFQId) {
    const holder = document.createElement('div');
    holder.id = 'rfq-detail';
    document.querySelector('#rfq-screen > [data-pane="list"]').appendChild(holder);
    await renderRFQDetail(holder, selectedRFQId, rfqs.find(r => r.id === selectedRFQId));
  }
}

function renderRFQDraftLines() {
  const el = document.getElementById('rfq-lines');
  if (!el) return;
  el.innerHTML = rfqDraftLines.length === 0
    ? '<p style="color: var(--text-muted); margin: 0;">No lines yet. Pick an item (or describe what you need), its unit and quantity, then <b>Add line</b>.</p>'
    : `<table><thead><tr><th>#</th><th>Item</th><th>Description</th><th>Unit</th><th class="num">Qty</th><th></th></tr></thead><tbody>
      ${rfqDraftLines.map((l, i) => `<tr><td>${i + 1}</td><td>${esc(l.sku || '—')}</td><td>${esc(l.description || '')}</td><td>${esc(l.uom)}</td><td class="num">${num(l.qty)}</td>
        <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeRFQDraftLine', [i])}>Remove</button></td></tr>`).join('')}
      </tbody></table>`;
}

function addRFQLine() {
  const sku = document.getElementById('rfq-line-item').value.trim();
  const description = document.getElementById('rfq-line-desc').value.trim();
  const uom = document.getElementById('rfq-line-uom').value.trim() || 'Nos';
  const qty = parseFloat(document.getElementById('rfq-line-qty').value);
  const errEl = document.getElementById('rfq-form-error');
  errEl.classList.add('hidden');
  if (!sku && !description) { errEl.textContent = 'Pick an item or describe what you need.'; errEl.classList.remove('hidden'); return; }
  if (!(qty > 0)) { errEl.textContent = 'Enter a quantity above zero.'; errEl.classList.remove('hidden'); return; }
  rfqDraftLines.push({ sku, description, uom, qty });
  ['rfq-line-item', 'rfq-line-desc', 'rfq-line-qty'].forEach(id => { document.getElementById(id).value = ''; });
  renderRFQDraftLines();
}

function removeRFQDraftLine(i) {
  rfqDraftLines.splice(i, 1);
  renderRFQDraftLines();
}

async function saveRFQForm() {
  const errEl = document.getElementById('rfq-form-error');
  errEl.classList.add('hidden');
  if (rfqDraftLines.length === 0) { errEl.textContent = 'Add at least one line.'; errEl.classList.remove('hidden'); return; }
  const out = await postJSON('/api/v1/rfq/save', {
    id: rfqEditingId, location: document.getElementById('rfq-location').value.trim(),
    response_due_date: document.getElementById('rfq-due').value, target_date: document.getElementById('rfq-target-date').value,
    lines: rfqDraftLines.map(l => ({ sku: l.sku, description: l.description, uom: l.uom, qty: l.qty }))
  });
  if (!out.ok) { errEl.textContent = out.message; errEl.classList.remove('hidden'); return; }
  selectedRFQId = out.body.id;
  rfqDraftLines = [];
  rfqEditingId = '';
  (await loadTxnScreen()).txnDone('rfq-screen');
  renderView('rfq');
}

async function viewRFQQuotes(rfqId) {
  selectedRFQId = rfqId;
  (await loadTxnScreen()).txnDone('rfq-quotes');
  renderView('rfq');
}

async function editRFQLines(rfqId) {
  const res = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (!res || !res.ok) return;
  const r = await res.json();
  rfqEditingId = rfqId;
  rfqDraftLines = parseList(r.lines).map(l => ({ sku: l.sku || '', description: l.description || '', uom: l.uom || 'Nos', qty: l.qty }));
  await renderView('rfq');
  if (typeof window.txnShowPane === 'function') window.txnShowPane('rfq-screen:new');
}

function cancelRFQEdit() {
  rfqEditingId = '';
  rfqDraftLines = [];
  renderView('rfq');
}

// setRFQStatus: Draft -> Sent, or close an open RFQ without awarding. The
// generic update replaces the whole record, so it resends it with its version.
async function setRFQStatus(rfqId, status) {
  const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (!loaded || !loaded.ok) return;
  const { id, ...rfq } = await loaded.json();
  if (status === 'Sent' && parseList(rfq.vendor_invitations).length === 0 && parseList(rfq.invited_vendors).length === 0 &&
      !await showCustomConfirm('No vendors are invited to this RFQ yet. Mark it as sent anyway?', 'Mark RFQ as Sent')) return;
  if (status === 'Closed' && !await showCustomConfirm('Close this RFQ without awarding it? No further quotes can be recorded against it.', 'Close RFQ')) return;
  const payload = { ...rfq, status };
  if (typeof rfq.version === 'number') payload.expected_version = rfq.version;
  const res = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not update the RFQ.'); return; }
  showToast(`RFQ marked ${status}.`, { variant: 'success' });
  renderView('rfq');
}

async function renderRFQDetail(container, rfqId, rfqRow) {
  const res = await apiFetch(`/api/v1/rfq/${encodeURIComponent(rfqId)}/comparison`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the RFQ.'); return; }
  const cmp = rfqCompare = await res.json();
  const open = cmp.status === 'Draft' || cmp.status === 'Sent';
  const lines = cmp.lines || [];
  const quotes = cmp.quotes || [];
  const head = document.createElement('div');
  head.className = 'table-panel';
  head.style.padding = '20px';
  head.style.marginBottom = '16px';
  head.innerHTML = `
    <div style="display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; align-items: baseline;">
      <h2 style="font-size: 16px; font-weight: 700; margin: 0;">RFQ ${esc(cmp.number)} <span class="badge ${cmp.status === 'Closed' ? 'badge-success' : cmp.status === 'Sent' ? 'badge-warning' : 'badge-secondary'}">${esc(cmp.status)}</span></h2>
      <div style="display: flex; gap: 8px; flex-wrap: wrap;">
        ${cmp.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('editRFQLines', [rfqId])}>Edit lines</button><button class="action-btn" ${actionAttrs('setRFQStatus', [rfqId, 'Sent'])}>Mark as Sent</button>` : ''}
        ${open && !cmp.awarded_po_id ? `<button class="action-btn" ${actionAttrs('setRFQStatus', [rfqId, 'Closed'])}>Close without award</button>` : ''}
      </div>
    </div>
    <p style="margin: 6px 0 12px; color: var(--text-muted);">${cmp.location ? `Deliver to ${esc(cmp.location)}` : 'No deliver-to location yet'}${rfqRow && rfqRow.response_due_date ? ` · quotes due ${esc(rfqRow.response_due_date)}` : ''}</p>
    ${cmp.awarded_po_id ? `<div class="notice" style="margin-bottom: 12px;">Awarded: purchase order <b>${esc(cmp.awarded_po_id)}</b> was created from quote ${esc(cmp.awarded_quote_id)}. <button class="action-btn" ${actionAttrs('openAwardedPO', [cmp.awarded_po_id])}>Open purchase orders</button></div>` : ''}
    ${lines.length ? `<table><thead><tr><th>#</th><th>Item</th><th>Description</th><th>Unit</th><th class="num">Qty</th></tr></thead><tbody>
      ${lines.map(l => `<tr><td>${l.line_no}</td><td>${esc(l.sku || '—')}</td><td>${esc(l.description)}</td><td>${esc(l.uom)}</td><td class="num">${num(l.qty)}</td></tr>`).join('')}</tbody></table>`
      : `<p style="margin: 0;">This RFQ has no item lines yet (it was made before RFQs had lines, or from a requisition). ${cmp.status === 'Draft' ? 'Use <b>Edit lines</b> to add them before inviting vendors.' : ''}</p>`}`;
  container.appendChild(head);

  // Invited vendors and their responses.
  const inv = cmp.invitations || [];
  const vend = document.createElement('div');
  vend.className = 'table-panel';
  vend.style.padding = '20px';
  vend.style.marginBottom = '16px';
  vend.innerHTML = `
    <h3 style="font-size: 15px; font-weight: 700; margin: 0 0 10px;">Vendors</h3>
    ${inv.length ? `<table><thead><tr><th>Vendor</th><th>Response</th><th>Due</th><th>Quote</th><th></th></tr></thead><tbody>
      ${inv.map(i => `<tr><td>${esc(i.vendor_name || i.vendor)}${i.vendor_name && i.vendor_name !== i.vendor ? `<div style="font-size: 11px; color: var(--text-muted);">${esc(i.vendor)}</div>` : ''}</td>
        <td><span class="badge ${i.status === 'Responded' ? 'badge-success' : i.status === 'Declined' ? 'badge-danger' : 'badge-warning'}">${esc(i.status)}</span>${i.decline_reason ? `<div style="font-size: 11.5px;">${esc(i.decline_reason)}</div>` : ''}</td>
        <td>${esc(i.due_date || '')}</td><td>${esc(i.quote_id || '')}</td>
        <td>${open && i.status === 'Invited' ? `<button class="action-btn" ${actionAttrs('declineRFQVendor', [rfqId, i.vendor])}>Declined</button>` : ''}</td></tr>`).join('')}
      </tbody></table>` : '<p style="margin: 0; color: var(--text-muted);">No vendors invited yet.</p>'}
    ${open ? `<div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-invite-vendor">Vendor</label>
        <input type="text" id="rfq-invite-vendor" class="form-input" style="width: 220px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="rfq-invite-due">Reply by</label>
        <input type="date" id="rfq-invite-due" class="form-input" value="${esc((rfqRow && rfqRow.response_due_date) || '')}"></div>
      <button class="btn btn-outline" id="rfq-invite-btn" type="button">Invite</button></div>` : ''}`;
  container.appendChild(vend);
  const inviteInput = document.getElementById('rfq-invite-vendor');
  if (inviteInput) {
    attachLinkTypeahead(inviteInput, 'Vendor');
    document.getElementById('rfq-invite-btn').addEventListener('click', async () => {
      const vendor = inviteInput.value.trim();
      if (!vendor) { inviteInput.focus(); return; }
      const out = await postJSON(`/api/v1/rfq/${encodeURIComponent(rfqId)}/invite`, { vendors: [vendor], due_date: document.getElementById('rfq-invite-due').value });
      if (!out.ok) { await showCustomAlert(out.message, 'Invite'); return; }
      renderView('rfq');
    });
  }

  // The comparison: one column per live quote, landed cost per RFQ unit per line.
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.style.overflowX = 'auto';
  const best = cmp.best_by_line || {};
  panel.innerHTML = quotes.length === 0
    ? `<p style="margin: 0; color: var(--text-muted);">No quotes recorded yet.${open && lines.length ? ' Click <b>+ Record Quote</b> when a vendor replies.' : ''}</p>`
    : `<p style="margin: 0 0 10px; color: var(--text-muted);">Each cell is the vendor's price, and below it the landed cost of one unit as the RFQ asks for it (price + tax + its share of freight). <span class="badge badge-success">best</span> marks the lowest per line; <b>Recommended</b> is the lowest landed total among complete, valid quotes.</p>
      <table><thead><tr><th>Line</th>${quotes.map(q => `<th class="num">${esc(q.vendor_name || q.vendor)}<div style="font-size: 11px; font-weight: 400;">${esc(q.number)}${q.recommended ? ' · <b>Recommended</b>' : ''}${q.status === 'Selected' ? ' · <b>Awarded</b>' : ''}</div></th>`).join('')}</tr></thead>
      <tbody>${lines.map(l => `<tr><td>${l.line_no}. ${esc(l.description)} <span style="color: var(--text-muted);">(${num(l.qty)} ${esc(l.uom)})</span></td>
        ${quotes.map(q => { const ql = (q.lines || []).find(x => x.line_no === l.line_no); if (!ql || ql.not_quoted) return '<td class="num" style="color: var(--text-muted);">not quoted</td>';
          return `<td class="num">₹${money(ql.unit_price)}${ql.uom && ql.uom !== l.uom ? `/${esc(ql.uom)}` : ''}${ql.tax_rate ? ` +${num(ql.tax_rate)}%` : ''}<div style="font-size: 11.5px;">₹${money((q.landed_unit || {})[l.line_no])} landed${best[l.line_no] === q.quote_id ? ' <span class="badge badge-success">best</span>' : ''}</div></td>`; }).join('')}</tr>`).join('')}
        <tr><td>Lines (ex tax)</td>${quotes.map(q => `<td class="num">₹${money(q.subtotal)}</td>`).join('')}</tr>
        <tr><td>Tax</td>${quotes.map(q => `<td class="num">₹${money(q.tax_total)}</td>`).join('')}</tr>
        <tr><td>Freight</td>${quotes.map(q => `<td class="num">₹${money(q.freight_amount)}</td>`).join('')}</tr>
        <tr><td><b>Landed total</b></td>${quotes.map(q => `<td class="num"><b>₹${money(q.landed_total)}</b></td>`).join('')}</tr>
        <tr><td>Lead time</td>${quotes.map(q => `<td class="num">${q.lead_time_days ? `${q.lead_time_days} days` : '—'}</td>`).join('')}</tr>
        <tr><td>Payment terms</td>${quotes.map(q => `<td class="num">${esc(q.payment_terms || '—')}</td>`).join('')}</tr>
        <tr><td>Valid until</td>${quotes.map(q => `<td class="num">${esc(q.valid_until || '—')}${q.expired ? ' <span class="badge badge-danger">expired</span>' : ''}</td>`).join('')}</tr>
        <tr><td>Lines quoted</td>${quotes.map(q => `<td class="num">${q.lines_quoted}/${lines.length}${q.complete ? '' : ' <span class="badge badge-warning">partial</span>'}</td>`).join('')}</tr>
        ${!cmp.awarded_po_id ? `<tr><td></td>${quotes.map(q => `<td class="num">${q.expired ? '' : `<button class="action-btn" ${actionAttrs('awardRFQQuote', [rfqId, q.quote_id])}>Award</button>`}</td>`).join('')}</tr>` : ''}
      </tbody></table>`;

  if (!open || lines.length === 0) {
    container.appendChild(panel);
    return;
  }
  // + Record Quote - a page of its own (Stage 69).
  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  form.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Quote Number', 'QTN', '160px')}
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="quote-vendor">Vendor</label>
        <input type="text" id="quote-vendor" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="quote-valid">Valid until</label>
        <input type="date" id="quote-valid" class="form-input"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="quote-freight">Freight (₹)</label>
        <input type="number" id="quote-freight" class="form-input" style="width: 110px;" min="0" value="0"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="quote-terms">Payment terms</label>
        <input type="text" id="quote-terms" class="form-input" style="width: 160px;" placeholder="e.g. 30 days"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="quote-lead">Lead time (days)</label>
        <input type="number" id="quote-lead" class="form-input" style="width: 100px;" min="0"></div>
    </div>
    <table style="margin-top: 14px;"><thead><tr><th>Line</th><th>Vendor's unit</th><th>RFQ units in one</th><th class="num">Unit price (₹)</th><th class="num">Tax %</th><th class="num">Lead days</th><th>Not quoted</th></tr></thead>
      <tbody>${lines.map(l => `<tr data-line="${l.line_no}">
        <td>${l.line_no}. ${esc(l.description)} <span style="color: var(--text-muted);">(${num(l.qty)} ${esc(l.uom)})</span></td>
        <td><input class="form-input ql-uom" style="width: 80px;" value="${esc(l.uom)}" aria-label="Vendor unit for line ${l.line_no}"></td>
        <td><input type="number" class="form-input ql-per" style="width: 90px;" min="0" placeholder="same" aria-label="RFQ units in one vendor unit, line ${l.line_no}"></td>
        <td><input type="number" class="form-input ql-price" style="width: 110px;" min="0" aria-label="Unit price, line ${l.line_no}"></td>
        <td><input type="number" class="form-input ql-tax" style="width: 80px;" min="0" value="18" aria-label="Tax rate, line ${l.line_no}"></td>
        <td><input type="number" class="form-input ql-lead" style="width: 80px;" min="0" aria-label="Lead days, line ${l.line_no}"></td>
        <td><input type="checkbox" class="ql-skip" aria-label="Line ${l.line_no} not quoted"></td></tr>`).join('')}</tbody></table>
    <p style="margin: 8px 0 0; color: var(--text-muted);">A vendor quoting in a different unit (a box of 10, a dozen) needs <b>RFQ units in one</b>, unless the item has a unit conversion set up. A newer quote from the same vendor replaces their earlier one.</p>
    <div style="display: flex; gap: 12px; margin-top: 14px;"><button class="btn btn-primary" id="quote-submit-btn">Record Quote</button></div>
    <div id="quote-form-error" class="login-error hidden" style="margin-top: 12px;"></div>`;
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  container.appendChild(txnPagedSection({ id: 'rfq-quotes', title: `Quotes for ${esc(cmp.number)}`, page: 'Record quote', count: quotes.length,
    newLabel: 'Record Quote', form, list: panel, heading: 'h2' }));
  txnRestorePane('rfq-quotes');
  attachLinkTypeahead(document.getElementById('quote-vendor'), 'Vendor');
  document.getElementById('quote-submit-btn').addEventListener('click', () =>
    guardAgainstDoubleSubmit(document.getElementById('quote-submit-btn'), 'Recording...', () => submitVendorQuote(rfqId)));
}

async function submitVendorQuote(rfqId) {
  const errEl = document.getElementById('quote-form-error');
  errEl.classList.add('hidden');
  const fail = m => { errEl.textContent = m; errEl.classList.remove('hidden'); };
  const vendor = document.getElementById('quote-vendor').value.trim();
  if (!vendor) return fail('Choose the vendor.');
  const lines = [...document.querySelectorAll('#rfq-quotes tr[data-line]')].map(tr => {
    const v = cls => tr.querySelector(cls);
    return {
      line_no: Number(tr.dataset.line), uom: v('.ql-uom').value.trim(), rfq_units_per_uom: parseFloat(v('.ql-per').value) || 0,
      unit_price: parseFloat(v('.ql-price').value) || 0, tax_rate: parseFloat(v('.ql-tax').value) || 0,
      lead_time_days: parseInt(v('.ql-lead').value, 10) || 0, not_quoted: v('.ql-skip').checked
    };
  });
  if (!lines.some(l => !l.not_quoted && l.unit_price > 0)) return fail('Enter a price for at least one line.');
  const out = await postJSON(`/api/v1/rfq/${encodeURIComponent(rfqId)}/quote`, {
    vendor, lines, valid_until: document.getElementById('quote-valid').value,
    freight_amount: parseFloat(document.getElementById('quote-freight').value) || 0,
    payment_terms: document.getElementById('quote-terms').value.trim(),
    lead_time_days: parseInt(document.getElementById('quote-lead').value, 10) || 0
  });
  if (!out.ok) return fail(out.message);
  showToast(`Quote ${out.body.quote_id} recorded.`, { variant: 'success' });
  (await loadTxnScreen()).txnDone('rfq-quotes');
  renderView('rfq');
}

async function declineRFQVendor(rfqId, vendor) {
  const reason = await showCustomPrompt(`Why is ${vendor} not quoting? (optional)`, '', 'Vendor declined');
  if (reason === null) return;
  const out = await postJSON(`/api/v1/rfq/${encodeURIComponent(rfqId)}/decline`, { vendor, reason });
  if (!out.ok) { await showCustomAlert(out.message, 'Declined'); return; }
  renderView('rfq');
}

// Award: the quote becomes a Draft PO, once. A free-text line needs its item
// (or is left out); the deliver-to location defaults to the RFQ's.
async function awardRFQQuote(rfqId, quoteId) {
  const cmp = rfqCompare;
  const q = cmp && (cmp.quotes || []).find(x => x.quote_id === quoteId);
  if (!q) return;
  const freeText = (q.lines || []).filter(l => !l.not_quoted && !l.sku);
  document.getElementById('rfq-award-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'rfq-award-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 620px;" role="dialog" aria-modal="true" aria-labelledby="rfq-award-title">
      <div class="modal-header"><h3 class="modal-title" id="rfq-award-title">Award to ${esc(q.vendor_name || q.vendor)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-award-close><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></button></div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">
        <p style="margin: 0;">A Draft purchase order is created from quote ${esc(q.number)} - ₹${money(q.subtotal)} + tax, freight ₹${money(q.freight_amount)}, ${esc(q.payment_terms || 'no stated terms')}. The other quotes are rejected and the RFQ closes. This can be done once.</p>
        <div class="form-group"><label class="form-label" for="award-location">Deliver to</label><input id="award-location" class="form-input" autocomplete="off" value="${esc(cmp.location || '')}"></div>
        ${freeText.map(l => `<div class="form-group" data-freeline="${l.line_no}"><label class="form-label" for="award-item-${l.line_no}">Line ${l.line_no}: ${esc(l.description)} - item for the PO</label>
          <div style="display: flex; gap: 10px; align-items: center;"><input id="award-item-${l.line_no}" class="form-input award-item" autocomplete="off" style="flex: 1;">
          <label style="display: flex; gap: 6px; align-items: center; white-space: nowrap;"><input type="checkbox" class="award-skip"> Leave out</label></div></div>`).join('')}
        <div id="award-error" class="login-error hidden"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-award-close>Cancel</button><button type="button" class="btn btn-primary" id="award-submit">Create Purchase Order</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-award-close]').forEach(b => b.addEventListener('click', close));
  attachLinkTypeahead(overlay.querySelector('#award-location'), 'Location');
  overlay.querySelectorAll('.award-item').forEach(el => attachLinkTypeahead(el, 'Item'));
  requestAnimationFrame(() => overlay.classList.add('open'));
  const btn = overlay.querySelector('#award-submit');
  btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Creating...', async () => {
    const errEl = overlay.querySelector('#award-error');
    errEl.classList.add('hidden');
    const lineItems = {};
    const leaveOut = [];
    for (const div of overlay.querySelectorAll('[data-freeline]')) {
      const n = Number(div.dataset.freeline);
      if (div.querySelector('.award-skip').checked) { leaveOut.push(n); continue; }
      const sku = div.querySelector('.award-item').value.trim();
      if (!sku) { errEl.textContent = `Choose the item for line ${n}, or leave it out.`; errEl.classList.remove('hidden'); return; }
      lineItems[n] = sku;
    }
    const out = await postJSON(`/api/v1/rfq/${encodeURIComponent(rfqId)}/award`, {
      quote_id: quoteId, location: overlay.querySelector('#award-location').value.trim(), line_items: lineItems, leave_out: leaveOut });
    if (!out.ok) { errEl.textContent = out.message; errEl.classList.remove('hidden'); return; }
    close();
    const r = out.body;
    await showCustomAlert(`Purchase order ${r.po_id} created as a Draft (₹${money(r.po_total)} + tax, freight ₹${money(r.freight_amount)}).${(r.left_out || []).length ? `\nLeft off the PO: ${r.left_out.join('; ')}.` : ''}\nReview it and submit it for approval on Procurement → Purchase Orders.`, 'RFQ Awarded');
    renderView('rfq');
  }));
}

function openAwardedPO() {
  renderView('purchase-orders');
}

export {
  renderRFQView, viewRFQQuotes, removeRFQDraftLine, editRFQLines, cancelRFQEdit, setRFQStatus,
  declineRFQVendor, awardRFQQuote, openAwardedPO, submitVendorQuote
};
