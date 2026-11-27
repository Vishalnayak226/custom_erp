// Ask 169, items 73.4.2.6 and 73.4.2.7: OMS -> Carrier claims (oms:freight) and
// OMS -> Freight invoices (oms:freight-invoices), one page each since 288.2.
// Carrier claims for a lost, damaged or short shipment (Open -> Filed ->
// Settled, or Rejected), and the carrier's freight invoice checked AWB by AWB
// against what each booking was quoted. Invoice lines come in through the
// standard CSV import on the FreightInvoiceLine list; Reconcile matches them.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const money = (v) => formatMoney(num(v));
const again = () => renderView('oms:freight');

const OPEN_CLAIM = ['Open', 'Filed'];
const LINE_ORDER = ['Overcharged', 'Undercharged', 'Duplicate', 'No booking', 'Unmatched', 'Matched'];
const LINE_BADGE = { Matched: 'badge-success', Overcharged: 'badge-danger', Undercharged: 'badge-warning', Duplicate: 'badge-danger', 'No booking': 'badge-warning', Unmatched: 'badge-secondary' };
const CLAIM_BADGE = { Open: 'badge-warning', Filed: 'badge-info', Settled: 'badge-success', Rejected: 'badge-secondary' };
// Imported lines carry the import's document status until reconciled.
const lineStatus = (l) => (LINE_ORDER.includes(l.status) ? l.status : 'Unmatched');

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

async function post(url, body, failMsg) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failMsg); return null; }
  try { return await res.json(); } catch (_) { return {}; }
}

async function formDialog(title, html, collect) {
  const { posFormDialog } = await import('./view-pos-extras.js' + Q);
  return posFormDialog(title, '', html, collect);
}

// raiseCarrierClaimDialog asks for the booking (its id or AWB), the type, the
// amount and a note, and raises the claim. Also used from the order screen.
export async function raiseCarrierClaimDialog(booking = '', after) {
  const picked = await formDialog('Raise claim', `
    <div class="form-group"><label class="form-label" for="clm-booking">Booking or AWB</label>
      <input type="text" id="clm-booking" class="form-input" value="${esc(booking)}"></div>
    <div class="form-group"><label class="form-label" for="clm-type">Type</label>
      <select id="clm-type" class="form-input"><option>Lost</option><option>Damaged</option><option>Short</option></select></div>
    <div class="form-group"><label class="form-label" for="clm-amount">Amount claimed</label>
      <input type="number" id="clm-amount" class="form-input" min="0" step="0.01"></div>
    <div class="form-group"><label class="form-label" for="clm-notes">Note</label>
      <input type="text" id="clm-notes" class="form-input"></div>`,
  (el) => ({
    booking_id: el.querySelector('#clm-booking').value.trim(),
    claim_type: el.querySelector('#clm-type').value,
    amount: parseFloat(el.querySelector('#clm-amount').value) || 0,
    notes: el.querySelector('#clm-notes').value.trim(),
  }));
  if (!picked || !picked.booking_id) return;
  const out = await post('/api/v1/oms/carrier-claims', picked, 'The claim was not raised.');
  if (!out) return;
  showToast(`Claim ${out.id || ''} raised.`);
  if (after) await after();
}

async function moveClaim(id, action) {
  let body = { action };
  if (action === 'file') {
    const ref = await showCustomPrompt("Carrier's claim reference:", '', 'File claim');
    if (ref === null || !ref.trim()) return;
    body.claim_ref = ref.trim();
  } else if (action === 'settle') {
    const picked = await formDialog('Settle claim', `
      <div class="form-group"><label class="form-label" for="clm-settled">Amount paid by the carrier</label>
        <input type="number" id="clm-settled" class="form-input" min="0" step="0.01"></div>
      <div class="form-group"><label class="form-label" for="clm-settled-on">Paid on</label>
        <input type="date" id="clm-settled-on" class="form-input" value="${new Date().toISOString().slice(0, 10)}"></div>`,
    (el) => ({ amount: parseFloat(el.querySelector('#clm-settled').value) || 0, settled_on: el.querySelector('#clm-settled-on').value }));
    if (!picked) return;
    body = { ...body, ...picked };
  } else if (action === 'reject') {
    const note = await showCustomPrompt('Why did the carrier reject it?', '', 'Reject claim');
    if (note === null || !note.trim()) return;
    body.note = note.trim();
  }
  const out = await post(`/api/v1/oms/carrier-claims/${encodeURIComponent(id)}/move`, body, 'The claim was not updated.');
  if (out) { showToast('Claim updated.'); again(); }
}

function claimRows(claims) {
  if (!claims.length) return '<tr><td colspan="10" class="text-center text-muted">No claims.</td></tr>';
  const sorted = [...claims].sort((a, b) => (OPEN_CLAIM.includes(b.status) - OPEN_CLAIM.includes(a.status)) || String(b.created_at || '').localeCompare(String(a.created_at || '')));
  return sorted.map(c => {
    const id = c.code || c.id;
    const acts = [
      c.status === 'Open' ? `<button class="action-btn" data-claim="${esc(id)}" data-move="file">File</button>` : '',
      c.status === 'Filed' ? `<button class="action-btn" data-claim="${esc(id)}" data-move="settle">Settle</button>` : '',
      OPEN_CLAIM.includes(c.status) ? `<button class="action-btn" data-claim="${esc(id)}" data-move="reject">Reject</button>` : '',
    ].join('');
    return `<tr>
      <td class="s-355794">${esc(id)}</td>
      <td class="s-355794">${esc(c.booking_id)}</td>
      <td class="s-355794">${esc(c.order_id)}</td>
      <td>${esc(c.carrier)}</td>
      <td class="s-355794">${esc(c.awb)}</td>
      <td>${esc(c.claim_type)}</td>
      <td class="num">${money(c.amount_claimed)}</td>
      <td class="num">${c.status === 'Settled' ? money(c.amount_settled) : ''}</td>
      <td><span class="badge ${CLAIM_BADGE[c.status] || 'badge-secondary'}">${esc(c.status)}</span>${c.claim_ref ? `<div class="text-muted">${esc(c.claim_ref)}</div>` : ''}</td>
      <td>${acts}</td>
    </tr>`;
  }).join('');
}

function lineRows(lines) {
  if (!lines.length) return '<tr><td colspan="8" class="text-center text-muted">No invoice lines.</td></tr>';
  const sorted = [...lines].sort((a, b) => LINE_ORDER.indexOf(lineStatus(a)) - LINE_ORDER.indexOf(lineStatus(b)));
  return sorted.map(l => {
    const st = lineStatus(l);
    const decided = st !== 'Unmatched' && st !== 'No booking';
    return `<tr>
      <td>${esc(l.invoice_no)}</td>
      <td class="s-355794">${esc(l.awb)}</td>
      <td>${esc(l.carrier)}</td>
      <td class="num">${money(l.charged_amount)}</td>
      <td class="num">${decided ? money(l.expected_amount) : ''}</td>
      <td class="num">${decided ? money(l.difference) : ''}</td>
      <td class="s-355794">${esc(l.booking_id)}</td>
      <td><span class="badge ${LINE_BADGE[st]}">${esc(st)}</span></td>
    </tr>`;
  }).join('');
}

function lineSummary(lines) {
  const count = {};
  let over = 0;
  lines.forEach(l => {
    const st = lineStatus(l);
    count[st] = (count[st] || 0) + 1;
    if (st === 'Overcharged') over += num(l.difference);
  });
  const chips = LINE_ORDER.filter(s => count[s]).map(s => `<span class="badge ${LINE_BADGE[s]}">${esc(s)} ${count[s]}</span>`);
  if (over > 0) chips.push(`<span class="badge badge-danger">Over by ${money(over)}</span>`);
  return chips.join(' ');
}

export async function renderOMSFreightView(container, part = 'claims') {
  await import('./txn-screen.js' + Q); // its stylesheet carries .page-section-bar
  const canClaims = part === 'claims' && canReadDoctype('CarrierClaim');
  const canLines = part === 'invoices' && canReadDoctype('FreightInvoiceLine');
  const againLines = () => renderView('oms:freight-invoices');
  const [claims, lines] = await Promise.all([
    canClaims ? getJSON('/api/v1/doc/CarrierClaim?limit=500', []) : [],
    canLines ? getJSON('/api/v1/doc/FreightInvoiceLine?limit=500', []) : [],
  ]);

  const bar = document.createElement('div');
  bar.className = 'page-section-bar';
  bar.innerHTML = `<h1 class="page-title">${part === 'invoices' ? 'Freight invoices' : 'Carrier claims'}</h1><span class="page-section-actions"><button type="button" class="btn btn-outline" data-freight-refresh>Refresh</button></span>`;
  bar.querySelector('[data-freight-refresh]').addEventListener('click', part === 'invoices' ? againLines : again);
  container.appendChild(bar);

  if (canClaims) {
    const panel = document.createElement('div');
    panel.className = 'table-panel oms-step-panel';
    const open = (claims || []).filter(c => OPEN_CLAIM.includes(c.status)).length;
    panel.innerHTML = `
      <div class="oms-console-head"><h2>Carrier claims · ${open} open</h2>
        ${canCreateDoctype('CarrierClaim') ? '<button type="button" class="btn btn-primary btn-sm" data-claim-new>Raise claim</button>' : ''}</div>
      <div class="table-wrapper"><table>
        <thead><tr><th>Claim</th><th>Booking</th><th>Order</th><th>Carrier</th><th>AWB</th><th>Type</th><th class="num">Claimed</th><th class="num">Settled</th><th>Status</th><th>Actions</th></tr></thead>
        <tbody>${claimRows(claims || [])}</tbody>
      </table></div>`;
    panel.querySelector('[data-claim-new]')?.addEventListener('click', () => raiseCarrierClaimDialog('', again));
    panel.querySelectorAll('[data-claim]').forEach(b => b.addEventListener('click', () => moveClaim(b.dataset.claim, b.dataset.move)));
    container.appendChild(panel);
  }

  if (canLines) {
    const panel = document.createElement('div');
    panel.className = 'table-panel oms-step-panel';
    panel.innerHTML = `
      <div class="oms-console-head"><h2>Freight invoices · ${(lines || []).length}</h2>
        <span><button type="button" class="btn btn-outline btn-sm" data-freight-import>Import lines</button>
        <button type="button" class="btn btn-primary btn-sm" data-freight-reconcile>Reconcile</button></span></div>
      <div class="s-87c136">${lineSummary(lines || [])}</div>
      <div class="table-wrapper"><table>
        <thead><tr><th>Invoice</th><th>AWB</th><th>Carrier</th><th class="num">Charged</th><th class="num">Expected</th><th class="num">Difference</th><th>Booking</th><th>Status</th></tr></thead>
        <tbody>${lineRows(lines || [])}</tbody>
      </table></div>`;
    panel.querySelector('[data-freight-import]').addEventListener('click', () => {
      currentDoctype = 'FreightInvoiceLine'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table');
    });
    panel.querySelector('[data-freight-reconcile]').addEventListener('click', async () => {
      const r = await post('/api/v1/oms/freight/reconcile', {}, 'Reconcile did not run.');
      if (!r) return;
      showToast(r.checked ? `${r.checked} checked: ${r.matched} matched, ${r.overcharged + r.undercharged} off, ${r.duplicate} duplicate, ${r.no_booking} no booking.` : 'Nothing new to check.');
      againLines();
    });
    container.appendChild(panel);
  }

  if (!canClaims && !canLines) {
    container.insertAdjacentHTML('beforeend', `<p class="text-muted">Your role cannot see ${part === 'invoices' ? 'freight invoices' : 'carrier claims'}.</p>`);
  }
}
