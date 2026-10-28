// Stage 49.5.6a (user, 2026-10-28: "There will be a validation of offline
// sales screen. Once its validated it will deduct. Validation of erp barcode
// vs scanned. Else won't be posted."): POS > Offline Sales. A sale the till
// rang up offline arrives here held, not posted. The list comes first; a sale
// opens as its own page with each line's ERP barcode against the code
// scanned. Validate (every line matching, by someone other than its cashier)
// posts it as a normal sale; Reject (with a reason) never posts it. The
// server enforces all of it - the buttons only follow.

const V = new URL(import.meta.url).search;
const esc = (s) => window.escapeHTMLText(s == null ? '' : String(s));
const when = (ts) => {
  if (!ts) return '';
  const d = new Date(ts);
  if (isNaN(d)) return '';
  const p = (n) => String(n).padStart(2, '0');
  return `${p(d.getDate())}-${p(d.getMonth() + 1)}-${d.getFullYear()} ${p(d.getHours())}:${p(d.getMinutes())}`;
};
const money = (v) => (typeof window.formatMoney === 'function' ? window.formatMoney(Number(v) || 0) : (Number(v) || 0).toFixed(2));
const SCREEN = 'ofs-screen';
let ofsStatus = 'Pending';
let ofsOpenId = '';
let ofsRows = [];

function statusBadge(s) {
  const cls = s === 'Validated' ? 'badge-success' : s === 'Rejected' ? 'badge-danger' : 'badge-warning';
  return `<span class="badge ${cls}">${esc(s)}</span>`;
}

export async function renderOfflineSalesView(container) {
  const { txnPageTitle, txnRestorePane, txnPageOf } = await import('./txn-screen.js' + V);
  container.innerHTML = `
    <div id="${SCREEN}" class="txn-paged txn-on-landing" data-landing="list">
      <div class="page-section-bar">
        ${txnPageTitle({ screen: SCREEN, title: 'Offline Sales', page: '<span id="ofs-page-title"></span>', count: '', landing: 'list' })}
        <span class="page-section-actions txn-landing-only">
          <select id="ofs-status" class="form-input" aria-label="Status" style="width: 150px;">
            ${['Pending', 'Validated', 'Rejected', ''].map(s => `<option value="${s}"${s === ofsStatus ? ' selected' : ''}>${s || 'All'}</option>`).join('')}
          </select>
        </span>
      </div>
      <div data-pane="list">
        <div class="table-panel" style="overflow-x: auto;">
          <table>
            <thead><tr><th>Received</th><th>Sold</th><th>Cart</th><th>Store</th><th>Cashier</th><th style="text-align: right;">Total</th><th>Lines matched</th><th>Status</th></tr></thead>
            <tbody id="ofs-rows"><tr><td colspan="8" class="text-muted">Loading…</td></tr></tbody>
          </table>
        </div>
      </div>
      <div data-pane="sale" class="hidden"><div id="ofs-sale"></div></div>
    </div>`;
  container.querySelector('#ofs-status').addEventListener('change', (e) => { ofsStatus = e.target.value; loadOfflineSales(); });
  container.querySelector('#ofs-rows').addEventListener('click', (e) => {
    const tr = e.target.closest('tr[data-ofs]');
    if (tr) openOfflineSale(tr.dataset.ofs);
  });
  container.querySelector('#ofs-sale').addEventListener('click', onSaleClick);
  txnRestorePane(SCREEN);
  const onSale = txnPageOf(SCREEN, 'list') === 'sale';
  await loadOfflineSales();
  if (onSale) {
    if (ofsOpenId) await openOfflineSale(ofsOpenId, false);
    else window.txnShowPane(`${SCREEN}:list`);
  }
}

async function loadOfflineSales() {
  const body = document.getElementById('ofs-rows');
  if (!body) return;
  const res = await window.apiFetch(`/api/v1/pos/offline-sales?status=${encodeURIComponent(ofsStatus)}`);
  if (!res || !res.ok) {
    body.innerHTML = '<tr><td colspan="8" class="text-muted">The offline sales could not be loaded.</td></tr>';
    if (res) await window.showApiError(res, 'Could not load the offline sales.');
    return;
  }
  ofsRows = ((await res.json()) || {}).sales || [];
  const count = document.querySelector(`#${SCREEN} .txn-count`);
  if (count) count.textContent = ofsRows.length;
  body.innerHTML = ofsRows.length ? ofsRows.map(s => `<tr data-ofs="${esc(s.id)}" style="cursor: pointer;">
      <td style="white-space: nowrap;">${esc(when(s.received_at))}</td><td style="white-space: nowrap;">${esc(when(s.sold_at))}</td>
      <td>${esc(s.cart_number)}</td><td>${esc(s.location)}</td><td>${esc(s.cashier)}</td>
      <td style="text-align: right;">${esc(money(s.total))}</td>
      <td>${s.lines_matched}/${s.lines_total}</td><td>${statusBadge(s.status)}</td></tr>`).join('')
    : `<tr><td colspan="8" class="text-muted">No ${ofsStatus ? ofsStatus.toLowerCase() + ' ' : ''}offline sales.</td></tr>`;
}

async function openOfflineSale(id, push = true) {
  const res = await window.apiFetch(`/api/v1/pos/offline-sales/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) { await window.showApiError(res, 'Could not open the offline sale.'); return; }
  ofsOpenId = id;
  renderSale(await res.json());
  if (push) window.txnShowPane(`${SCREEN}:sale`);
}

function renderSale(s) {
  const el = document.getElementById('ofs-sale');
  const title = document.getElementById('ofs-page-title');
  if (!el) return;
  if (title) title.textContent = s.cart_number;
  const lines = s.lines || [];
  const allMatch = lines.length > 0 && lines.every(l => l.match);
  const fact = (label, value) => value ? `<div><div class="text-muted" style="font-size: 12px;">${label}</div><div>${value}</div></div>` : '';
  el.innerHTML = `
    <div class="table-panel" style="padding: 16px; margin-bottom: 16px; min-height: 0; display: grid; align-content: start; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 12px;">
      ${fact('Status', statusBadge(s.status))}
      ${fact('Store', esc(s.location))}
      ${fact('Cashier', esc(s.cashier))}
      ${fact('Sold', esc(when(s.sold_at)))}
      ${fact('Received', esc(when(s.received_at)))}
      ${fact('Total', esc(money(s.total)))}
      ${fact('Lines matched', `${s.lines_matched}/${lines.length}`)}
      ${fact('Bill', esc(s.bill_number))}
      ${fact(s.status === 'Rejected' ? 'Rejected by' : 'Validated by', esc(s.decided_by))}
      ${fact('Decided', esc(when(s.decided_at)))}
      ${fact('Reason', esc(s.reject_reason))}
    </div>
    <div class="table-panel" style="overflow-x: auto; margin-bottom: 16px;">
      <table>
        <thead><tr><th>Item</th><th style="text-align: right;">Qty</th><th>ERP barcode</th><th>Scanned</th><th>Match</th></tr></thead>
        <tbody>${lines.map(l => `<tr>
          <td>${esc(l.sku)}${l.name ? `<div class="text-muted" style="font-size: 12px;">${esc(l.name)}</div>` : ''}</td>
          <td style="text-align: right;">${esc(l.qty)}</td>
          <td>${(l.erp_barcodes || []).length ? esc(l.erp_barcodes.join(' · ')) : '<span class="text-muted">None</span>'}</td>
          <td>${l.scanned ? esc(l.scanned) : '<span class="text-muted">Not scanned</span>'}</td>
          <td>${l.match ? '<span class="badge badge-success">Yes</span>' : '<span class="badge badge-danger">No</span>'}</td></tr>`).join('')}</tbody>
      </table>
    </div>
    <div id="ofs-error" class="hidden" role="alert" style="color: var(--danger-color, #b3261e); margin-bottom: 12px;"></div>
    ${s.status === 'Pending' ? `<div style="display: flex; gap: 8px;">
      <button type="button" class="btn btn-primary" data-ofs-do="validate"${allMatch ? '' : ' disabled style="opacity: .5; cursor: not-allowed;" title="Every line must be scanned with its own barcode"'}>Validate</button>
      <button type="button" class="btn btn-outline" data-ofs-do="reject">Reject</button>
    </div>` : ''}`;
}

function showSaleError(text) {
  const el = document.getElementById('ofs-error');
  if (!el) return;
  el.textContent = text || '';
  el.classList.toggle('hidden', !text);
}

async function onSaleClick(e) {
  const btn = e.target.closest('[data-ofs-do]');
  if (!btn || btn.disabled || !ofsOpenId) return;
  const action = btn.dataset.ofsDo;
  showSaleError('');
  let reason = '';
  if (action === 'reject') {
    const typed = await window.showCustomPrompt('Why is this sale rejected? It will not be posted.', '', 'Reject sale');
    if (typed === null) return;
    reason = typed.trim();
    if (!reason) { showSaleError('Say why the sale is rejected.'); return; }
  }
  document.querySelectorAll('#ofs-sale [data-ofs-do]').forEach(b => { b.disabled = true; });
  const res = await window.apiFetch(`/api/v1/pos/offline-sales/${encodeURIComponent(ofsOpenId)}/${action}`, {
    method: 'POST',
    body: JSON.stringify({ reason })
  });
  if (!res) { document.querySelectorAll('#ofs-sale [data-ofs-do]').forEach(b => { b.disabled = false; }); return; }
  if (!res.ok) {
    await window.showApiError(res, action === 'validate' ? 'The sale was not validated.' : 'The sale was not rejected.');
    await openOfflineSale(ofsOpenId, false);
    return;
  }
  const sale = await res.json();
  renderSale(sale);
  window.showToast(action === 'validate' ? `Validated - bill ${sale.bill_number || sale.cart_number}` : 'Rejected - not posted', { variant: action === 'validate' ? 'success' : 'warning' });
  loadOfflineSales();
}
