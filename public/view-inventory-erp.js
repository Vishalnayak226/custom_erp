// Stage 88.5 (D9): Inventory as an ERP module - Dashboard, Inventory Ledger
// and MRP Change, each opened from the Inventory menu as inventory:<page>
// (renderInventoryView hands over here). Stock on Hand stays the plain
// inventory view; Stock Movement is the transfer screen.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const fmtQty = (v) => num(v).toLocaleString();
const when = (v) => { const d = new Date(v); return Number.isNaN(d.getTime()) ? esc(v || '') : esc(d.toLocaleString()); };

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

function bar(container, title, extra = '') {
  const el = document.createElement('div');
  el.className = 'page-header';
  el.innerHTML = `<div class="page-title-section"><h1 class="page-title">${esc(title)}</h1></div><div style="display:flex;gap:8px;flex-wrap:wrap;">${extra}</div>`;
  container.appendChild(el);
  return el;
}

function panel(container, html) {
  const el = document.createElement('div');
  el.className = 'table-panel';
  el.style.cssText = 'padding:16px 20px;margin-bottom:20px;';
  el.innerHTML = html;
  container.appendChild(el);
  return el;
}

function goPage(page) {
  setActiveMenu(`menu-inventory-${page}`);
  renderView(`inventory:${page}`);
}

// ---------------------------------------------------------------------------
// 88.5.1 Dashboard
// ---------------------------------------------------------------------------

let dashboardStore = '';

async function renderDashboard(container) {
  const d = await getJSON(`/api/v1/inventory/dashboard?location=${encodeURIComponent(dashboardStore)}`, null);
  bar(container, 'Inventory Dashboard', '<button type="button" class="btn btn-outline" data-inv-refresh>Refresh</button>');
  container.querySelector('[data-inv-refresh]').addEventListener('click', () => renderView('inventory:dashboard'));
  if (!d) { renderErrorPanel(container, 'Failed to load the inventory dashboard.', () => renderView('inventory:dashboard')); return; }
  const stores = (d.by_location || []).map(r => r.location);
  const tiles = document.createElement('div');
  tiles.className = 'dashboard-stats-row';
  tiles.innerHTML = [
    ['Units on hand', fmtQty(d.total_units)],
    d.value_hidden ? null : ['Stock value', formatMoney(d.total_value)],
    ['Low or out of stock', String((d.low_stock || []).length)],
    ['Adjustments waiting approval', String((d.pending_adjustments || []).length)]
  ].filter(Boolean).map(([label, val]) => `<div class="stat-card"><span class="stat-label">${esc(label)}</span><span class="stat-val">${esc(val)}</span></div>`).join('');
  container.appendChild(tiles);

  panel(container, `<div style="display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:10px;">
      <h2 style="font-size:16px;margin:0;">By store</h2>
      <label style="display:flex;gap:6px;align-items:center;font-size:13px;">Store <select class="form-input" data-inv-store style="width:auto;">
        <option value="">All stores</option>${(dashboardStore && !stores.includes(dashboardStore) ? [dashboardStore, ...stores] : stores).map(s => `<option${s === dashboardStore ? ' selected' : ''}>${esc(s)}</option>`).join('')}</select></label>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Store</th><th class="num">Items</th><th class="num">Units</th>${d.value_hidden ? '' : '<th class="num">Value</th>'}</tr></thead>
    <tbody>${(d.by_location || []).length ? d.by_location.map(r => `<tr><td>${esc(r.location)}</td><td class="num">${fmtQty(r.items)}</td><td class="num">${fmtQty(r.units)}</td>${d.value_hidden ? '' : `<td class="num">${formatMoney(r.value)}</td>`}</tr>`).join('')
      : '<tr><td colspan="4" style="text-align:center;color:var(--text-muted);padding:20px;">No stock yet. Goods Receipt, transfers in and adjustments put stock here.</td></tr>'}</tbody></table></div>`)
    .querySelector('[data-inv-store]').addEventListener('change', e => { dashboardStore = e.target.value; renderView('inventory:dashboard'); });

  panel(container, `<h2 style="font-size:16px;margin:0 0 10px;">Low or out of stock</h2>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th>Store</th><th class="num">Free to sell</th><th class="num">Safety stock</th></tr></thead>
    <tbody>${(d.low_stock || []).length ? d.low_stock.map(r => `<tr><td>${esc(r.sku)}</td><td>${esc(r.location_code)}</td><td class="num">${fmtQty(r.free)}</td><td class="num">${num(r.safety) ? fmtQty(r.safety) : '—'}</td></tr>`).join('')
      : '<tr><td colspan="4" style="text-align:center;color:var(--text-muted);padding:20px;">Nothing is at or under its safety stock.</td></tr>'}</tbody></table></div>`);

  const recent = panel(container, `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;">
      <h2 style="font-size:16px;margin:0;">Latest movements</h2><button type="button" class="btn btn-outline btn-sm" data-inv-ledger>Inventory Ledger</button></div>
    <div class="table-wrapper"><table><thead><tr><th>When</th><th>Item</th><th>Store</th><th class="num">Qty</th><th>Document</th></tr></thead>
    <tbody>${(d.recent_movements || []).length ? d.recent_movements.map(r => `<tr><td>${when(r.created_at)}</td><td>${esc(r.sku)}</td><td>${esc(r.location)}</td><td class="num">${num(r.qty) > 0 ? '+' : ''}${fmtQty(r.qty)}</td><td>${esc(r.voucher_type)} <span class="text-muted">${esc(r.voucher_id || '')}</span></td></tr>`).join('')
      : '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:20px;">No stock has moved yet.</td></tr>'}</tbody></table></div>`);
  recent.querySelector('[data-inv-ledger]').addEventListener('click', () => goPage('ledger'));

  if ((d.pending_adjustments || []).length) {
    const pend = panel(container, `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;">
        <h2 style="font-size:16px;margin:0;">Adjustments waiting approval</h2><button type="button" class="btn btn-outline btn-sm" data-inv-approvals>Approvals</button></div>
      <div class="table-wrapper"><table><thead><tr><th>Adjustment</th><th>Store</th><th class="num">Value</th><th>Since</th></tr></thead>
      <tbody>${d.pending_adjustments.map(r => `<tr><td>${esc(r.code)}</td><td>${esc(r.location)}</td><td class="num">${formatMoney(r.amount)}</td><td>${when(r.created_at)}</td></tr>`).join('')}</tbody></table></div>`);
    pend.querySelector('[data-inv-approvals]').addEventListener('click', () => { setActiveMenu('menu-approvals'); renderView('approvals'); });
  }
}

// ---------------------------------------------------------------------------
// 88.5.2 Inventory Ledger - every movement, with each item's running balance
// at its store (the stock-ledger report).
// ---------------------------------------------------------------------------

const LEDGER_LIMIT = 500;
const ledgerFilter = { sku: '', location_code: '', voucher_type: '', start: '', end: '' };

async function renderLedger(container) {
  if (!ledgerFilter.start && !ledgerFilter.end) {
    const d = new Date(Date.now() - 30 * 86400000);
    ledgerFilter.start = d.toISOString().slice(0, 10);
  }
  bar(container, 'Inventory Ledger');
  const filters = panel(container, `<div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="inv-l-sku">Item</label><input class="form-input" id="inv-l-sku" style="width:180px;" value="${esc(ledgerFilter.sku)}" autocomplete="off"></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="inv-l-loc">Store</label><input class="form-input" id="inv-l-loc" style="width:150px;" value="${esc(ledgerFilter.location_code)}" autocomplete="off"></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="inv-l-type">Document type</label><input class="form-input" id="inv-l-type" style="width:150px;" value="${esc(ledgerFilter.voucher_type)}" placeholder="GRN, POSCart, StockMove..."></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="inv-l-start">From</label><input type="date" class="form-input" id="inv-l-start" value="${esc(ledgerFilter.start)}"></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="inv-l-end">To</label><input type="date" class="form-input" id="inv-l-end" value="${esc(ledgerFilter.end)}"></div>
      <button type="button" class="btn btn-primary" data-inv-l-show>Show</button>
      <button type="button" class="btn btn-outline" data-inv-l-clear>Clear</button>
    </div>`);
  attachLinkTypeahead(document.getElementById('inv-l-sku'), 'Item', { noSetupHint: true });
  attachLinkTypeahead(document.getElementById('inv-l-loc'), 'Location', { noSetupHint: true });
  const read = () => {
    ledgerFilter.sku = document.getElementById('inv-l-sku').value.trim();
    ledgerFilter.location_code = document.getElementById('inv-l-loc').value.trim();
    ledgerFilter.voucher_type = document.getElementById('inv-l-type').value.trim();
    ledgerFilter.start = document.getElementById('inv-l-start').value;
    ledgerFilter.end = document.getElementById('inv-l-end').value;
  };
  filters.querySelector('[data-inv-l-show]').addEventListener('click', () => { read(); renderView('inventory:ledger'); });
  filters.querySelector('[data-inv-l-clear]').addEventListener('click', () => { Object.keys(ledgerFilter).forEach(k => { ledgerFilter[k] = ''; }); renderView('inventory:ledger'); });

  const params = new URLSearchParams(Object.entries(ledgerFilter).filter(([, v]) => v));
  const res = await apiFetch(`/api/v1/reports/run/stock-ledger?${params}`);
  const host = panel(container, '');
  // 134.6: past the report's row cap the server refuses (REPORT-0161) rather
  // than load every movement - say how to narrow it, the filters stay above.
  if (res && res.status === 422) {
    const body = await res.json().catch(() => ({}));
    if (body.code === 'REPORT-0161') {
      host.innerHTML = '<p class="text-muted" style="padding: 16px;">More movements match than one screen can load. Choose an item or a store, or a shorter date range, above and click Show.</p>';
      return;
    }
  }
  const result = res && res.ok ? await res.json().catch(() => null) : null;
  if (!result) { renderErrorPanel(host, 'Failed to load the inventory ledger.', () => renderView('inventory:ledger')); return; }
  const all = (Array.isArray(result) ? result : (result.rows || [])).slice().reverse();
  const rows = all.slice(0, LEDGER_LIMIT);
  host.innerHTML = `<div class="table-wrapper"><table>
    <thead><tr><th>When</th><th>Item</th><th>Store</th><th class="num">In</th><th class="num">Out</th><th class="num">Balance</th><th>Document</th><th>From → to</th><th>By</th></tr></thead>
    <tbody>${rows.length ? rows.map(r => {
      const q = num(r.qty);
      const moved = [r.from_location_id || r.from_status, r.to_location_id || r.to_status].filter(Boolean).join(' → ');
      return `<tr><td>${when(r.created_at)}</td><td>${esc(r.item_id)}</td><td>${esc(r.warehouse_id)}</td>
        <td class="num">${q > 0 ? fmtQty(q) : ''}</td><td class="num">${q < 0 ? fmtQty(-q) : ''}</td><td class="num">${fmtQty(r.running_balance)}</td>
        <td>${esc(r.voucher_type)} <span class="text-muted">${esc(r.voucher_id || '')}</span></td><td>${esc(moved)}</td><td>${esc(r.user_id || '')}</td></tr>`;
    }).join('') : '<tr><td colspan="9" style="text-align:center;color:var(--text-muted);padding:20px;">No movement matches. Widen the dates or clear the filters.</td></tr>'}</tbody></table></div>
    ${all.length > rows.length ? `<p class="text-muted" style="margin:10px 0 0;font-size:13px;">Showing the latest ${rows.length} of ${all.length}; narrow the dates or pick an item to see the rest.</p>` : ''}`;
}

// ---------------------------------------------------------------------------
// 88.5.5 MRP Change - the list is every item's MRP history; + New changes MRP
// from an effective date and offers the sticker reprint.
// ---------------------------------------------------------------------------

let mrpLines = [];
let mrpSearch = '';

async function renderMRPChanges(container) {
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + Q);
  const docs = await getJSON(`/api/v1/doc/MRPChange?sort=recent&limit=200${mrpSearch ? `&q=${encodeURIComponent(mrpSearch)}` : ''}`, null);
  if (!docs) { renderErrorPanel(container, 'Failed to load MRP changes.', () => renderView('inventory:mrp')); return; }
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Applied: 'badge-success', Scheduled: 'badge-warning', Cancelled: 'badge-secondary', Draft: 'badge-secondary' };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `<div style="display:flex;gap:8px;margin-bottom:12px;max-width:520px;">
      <input type="search" class="form-input" data-mrp-search placeholder="Find an item's MRP history (code or name)" value="${esc(mrpSearch)}">
      <button type="button" class="btn btn-outline" data-mrp-find>Find</button></div>
    <div class="table-wrapper"><table><thead><tr><th>Change</th><th>Effective from</th><th>Items (old → new MRP)</th><th>Reason</th><th>Status</th><th></th></tr></thead>
    <tbody>${docs.length ? docs.map(d => {
      const ls = lines(d);
      return `<tr><td style="font-family:monospace;">${esc(d.code || d.id)}</td><td>${esc(d.effective_date)}</td>
        <td>${ls.slice(0, 3).map(l => `${esc(l.item_name || l.sku)} <span class="text-muted">${formatMoney(l.old_mrp)} → </span><b>${formatMoney(l.new_mrp)}</b>`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td>${esc(d.reason || '')}</td><td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span></td>
        <td>${d.status === 'Scheduled' ? `<button type="button" class="action-btn action-btn-danger" data-mrp-cancel="${esc(d.id)}">Cancel</button>` : ''}
            ${d.status === 'Applied' && ls.some(l => num(l.on_hand) > 0) ? `<button type="button" class="action-btn" data-mrp-stickers="${esc(d.id)}">Reprint stickers</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:20px;">${mrpSearch ? 'No MRP change names that item.' : 'No MRP has been changed here yet. + New MRP Change starts one.'}</td></tr>`}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  form.innerHTML = `<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="mrp-effective">Effective from</label><input type="date" class="form-input" id="mrp-effective" value="${new Date().toISOString().slice(0, 10)}"></div>
      <div class="form-group" style="margin:0;flex:1;min-width:220px;"><label class="form-label" for="mrp-reason">Reason</label><input class="form-input" id="mrp-reason" placeholder="e.g. Vendor price revision"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Current MRP</th><th class="num">Sale price</th><th class="num">New MRP</th><th></th></tr></thead><tbody data-mrp-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-mrp-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
      <button type="button" class="btn btn-outline" data-mrp-add>Add item</button>
    </div>
    <div class="login-error hidden" data-mrp-error style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-mrp-save>Save MRP change</button></div>`;

  const section = txnPagedSection({ id: 'mrp-screen', title: 'MRP Change', page: 'New MRP change', count: docs.length, newLabel: 'New MRP Change', form, list });
  container.appendChild(section);
  txnRestorePane('mrp-screen');

  list.querySelector('[data-mrp-find]').addEventListener('click', () => { mrpSearch = list.querySelector('[data-mrp-search]').value.trim(); renderView('inventory:mrp'); });
  list.querySelector('[data-mrp-search]').addEventListener('keydown', e => { if (e.key === 'Enter') { mrpSearch = e.target.value.trim(); renderView('inventory:mrp'); } });
  list.querySelectorAll('[data-mrp-cancel]').forEach(b => b.addEventListener('click', async () => {
    if (!await showCustomConfirm('Cancel this MRP change? The items keep their current MRP.', 'Cancel MRP change')) return;
    const res = await apiFetch(`/api/v1/inventory/mrp-changes/${encodeURIComponent(b.dataset.mrpCancel)}/cancel`, { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not cancel the MRP change.'); return; }
    renderView('inventory:mrp');
  }));
  list.querySelectorAll('[data-mrp-stickers]').forEach(b => b.addEventListener('click', () => {
    const d = docs.find(x => x.id === b.dataset.mrpStickers);
    reprintStickers(d.code || d.id, lines(d));
  }));

  const input = form.querySelector('[data-mrp-add-input]');
  const errorEl = form.querySelector('[data-mrp-error]');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const draw = () => {
    form.querySelector('[data-mrp-lines]').innerHTML = mrpLines.length ? mrpLines.map((l, i) => `<tr>
        <td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td>
        <td class="num">${formatMoney(l.mrp)}</td><td class="num">${formatMoney(l.sale_price)}</td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input" style="width:120px;text-align:right;" data-mrp-new="${i}" value="${esc(l.new_mrp ?? '')}">
          <div data-mrp-warn="${i}" style="color:var(--danger-strong);font-size:12px;"></div></td>
        <td><button type="button" class="action-btn action-btn-danger" data-mrp-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:16px;">Add the items whose MRP changes.</td></tr>';
    // Updated in place: a redraw here would run inside the input's own blur.
    const warn = (i) => { const l = mrpLines[i]; form.querySelector(`[data-mrp-warn="${i}"]`).textContent = num(l.new_mrp) > 0 && num(l.sale_price) > num(l.new_mrp) ? 'Sale price is above this MRP' : ''; };
    form.querySelectorAll('[data-mrp-new]').forEach(el => { warn(Number(el.dataset.mrpNew)); el.addEventListener('input', () => { mrpLines[Number(el.dataset.mrpNew)].new_mrp = el.value; warn(Number(el.dataset.mrpNew)); }); });
    form.querySelectorAll('[data-mrp-remove]').forEach(el => el.addEventListener('click', () => { mrpLines.splice(Number(el.dataset.mrpRemove), 1); draw(); }));
  };
  const addItem = (item) => {
    errorEl.classList.add('hidden');
    const sku = item.code || item.id;
    if (mrpLines.some(l => l.sku === sku)) { fail(`${sku} is already on this change.`); return; }
    mrpLines.push({ sku, name: item.name || item.item_name || '', mrp: num(item.mrp), sale_price: num(item.sale_price), new_mrp: '' });
    input.value = '';
    input.dispatchEvent(new Event('input')); // an empty query closes a search still on its way
    draw();
    form.querySelector(`[data-mrp-new="${mrpLines.length - 1}"]`)?.focus();
  };
  // A pick from the list adds the item; a typed code or barcode is looked up.
  attachLinkTypeahead(input, 'Item', { noSetupHint: true, onPick: (doc) => addItem(doc) });
  const add = async () => {
    const code = input.value.trim();
    if (!code) return;
    const found = await getJSON(`/api/v1/doc/Item?code=${encodeURIComponent(code)}&limit=1`, []);
    const item = found[0] || (await getJSON(`/api/v1/doc/Item?barcode=${encodeURIComponent(code)}&limit=1`, []))[0];
    if (!item) { fail(`No item has the code or barcode ${code}.`); return; }
    addItem(item);
  };
  form.querySelector('[data-mrp-add]').addEventListener('click', add);
  form.querySelector('[data-mrp-save]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    errorEl.classList.add('hidden');
    if (!mrpLines.length) { fail('Add at least one item.'); return; }
    const bad = mrpLines.find(l => !(num(l.new_mrp) > 0));
    if (bad) { fail(`Enter the new MRP of ${bad.sku}.`); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/inventory/mrp-changes', { method: 'POST', body: JSON.stringify({
      effective_date: form.querySelector('#mrp-effective').value, reason: form.querySelector('#mrp-reason').value.trim(),
      lines: mrpLines.map(l => ({ sku: l.sku, new_mrp: num(l.new_mrp) })) }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The MRP change was not saved.')); return; }
    const out = await res.json();
    mrpLines = [];
    txnDone('mrp-screen');
    if (out.warning) await showCustomAlert(out.warning, 'MRP change');
    showToast(out.status === 'Applied' ? `${out.number}: the new MRP applies now.` : `${out.number}: scheduled from ${form.querySelector('#mrp-effective').value}.`, { variant: 'success' });
    if (out.status === 'Applied') {
      const doc = await getJSON(`/api/v1/doc/MRPChange/${encodeURIComponent(out.id)}`, null);
      const held = doc ? lines(doc).filter(l => num(l.on_hand) > 0) : [];
      if (held.length && await showCustomConfirm(`${held.map(l => `${l.sku}: ${fmtQty(l.on_hand)} on hand`).join(', ')}. Their stickers still show the old MRP - print new ones now?`, 'Reprint stickers')) {
        return reprintStickers(out.number, lines(doc));
      }
    }
    renderView('inventory:mrp');
  });
  draw();
}

// ---------------------------------------------------------------------------
// 88.5.4 Stock Adjustment (D13) - a reason on every line; posts at once, or
// waits for approval when an approval rule covers adjustments.
// ---------------------------------------------------------------------------

let adjLines = [];

async function renderAdjustments(container) {
  const [{ txnPagedSection, txnRestorePane, txnDone, sessionStore }, rp] = await Promise.all([
    import('./txn-screen.js' + Q), import('./reason-picker.js' + Q)]);
  const [docs, reasons] = await Promise.all([
    getJSON('/api/v1/doc/InventoryAdjustment?sort=recent&limit=200', null), rp.loadReasons('Inventory Adjustment')]);
  if (!docs) { renderErrorPanel(container, 'Failed to load stock adjustments.', () => renderView('inventory:adjustments')); return; }
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Posted: 'badge-success', 'Pending Approval': 'badge-warning', Approved: 'badge-warning', Rejected: 'badge-danger' };
  const codeOf = Object.fromEntries(docs.map(d => [d.id, d.code || d.id]));
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `<div class="table-wrapper"><table><thead><tr><th>Adjustment</th><th>Store</th><th>Items</th><th class="num">Added</th><th class="num">Taken off</th><th>Status</th><th></th></tr></thead>
    <tbody>${docs.length ? docs.map(d => {
      const ls = lines(d);
      const problem = d.post_error || d.gl_error;
      return `<tr><td style="font-family:monospace;">${esc(d.code || d.id)}</td><td>${esc(d.location)}</td>
        <td>${ls.slice(0, 3).map(l => `${num(l.qty) > 0 ? '+' : ''}${fmtQty(l.qty)} ${esc(l.item_name || l.sku)} <span class="text-muted">(${esc(l.reason || l.reason_code)})</span>`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td class="num">${formatMoney(d.increase_value)}</td><td class="num">${formatMoney(d.decrease_value)}</td>
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${problem ? `<div style="color:var(--danger-strong);font-size:12px;max-width:260px;">${esc(problem)}</div>` : ''}
          ${d.reversal_of ? `<div class="text-muted" style="font-size:12px;">Reverses ${esc(codeOf[d.reversal_of] || d.reversal_of)}</div>` : ''}${d.reversed_by ? `<div class="text-muted" style="font-size:12px;">Reversed by ${esc(codeOf[d.reversed_by] || d.reversed_by)}</div>` : ''}</td>
        <td>${d.status === 'Approved' ? `<button type="button" class="action-btn" data-adj-post="${esc(d.id)}">Post</button>${/changed since|was \d+ when/.test(problem || '') ? ` <button type="button" class="action-btn" data-adj-post-now="${esc(d.id)}" title="Take the before quantities again from today's stock (store manager)">Post with current stock</button>` : ''}` : ''}
          ${d.status === 'Posted' && !d.reversed_by && !d.reversal_of ? `<button type="button" class="action-btn" data-adj-reverse="${esc(d.id)}">Reverse</button>` : ''}</td></tr>`;
    }).join('') : '<tr><td colspan="7" style="text-align:center;color:var(--text-muted);padding:20px;">No stock has been adjusted here yet. + New Stock Adjustment starts one.</td></tr>'}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  form.innerHTML = `<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="adj-store">Store</label><input class="form-input" id="adj-store" value="${esc(sessionStore())}" autocomplete="off" style="width:200px;"></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="adj-loss">Loss class</label><select class="form-select" id="adj-loss" style="width:160px;">
        <option value="">None</option><option>Shrinkage</option><option>Damage</option><option>Expiry</option><option>Theft</option></select></div>
      <div class="form-group" style="margin:0;flex:1;min-width:220px;"><label class="form-label" for="adj-note">Note</label><input class="form-input" id="adj-note" placeholder="e.g. Monthly damage check"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Change (+ / -)</th><th>Reason</th><th>Bin</th><th>Lot</th><th>To Damaged</th><th>Note</th><th></th></tr></thead><tbody data-adj-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-adj-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
      <button type="button" class="btn btn-outline" data-adj-add>Add item</button>
    </div>
    <div class="login-error hidden" data-adj-error style="margin-top:12px;"></div>
    <div data-adj-preview style="margin-top:12px;"></div>
    <div style="margin-top:16px;display:flex;gap:8px;"><button type="button" class="btn btn-outline" data-adj-preview-btn>Preview</button><button type="button" class="btn btn-primary" data-adj-save>Save adjustment</button></div>`;

  container.appendChild(txnPagedSection({ id: 'adj-screen', title: 'Stock Adjustment', page: 'New stock adjustment', count: docs.length, newLabel: 'New Stock Adjustment', form, list }));
  txnRestorePane('adj-screen');
  attachLinkTypeahead(form.querySelector('#adj-store'), 'Location', { keepCode: true });
  list.querySelectorAll('[data-adj-post]').forEach(b => b.addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/inventory/adjustments/${encodeURIComponent(b.dataset.adjPost)}/post`, { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The adjustment was not posted.'); return; }
    renderView('inventory:adjustments');
  }));
  // Stage 130.14 (74.2.3b): post with the stock as it is now; the linked reversal.
  list.querySelectorAll('[data-adj-post-now]').forEach(b => b.addEventListener('click', async () => {
    if (!(await showCustomConfirm('The stock changed since this adjustment was made. Take the before quantities again from today\'s stock and post it? Check the count first.', 'Post with current stock'))) return;
    const res = await apiFetch(`/api/v1/inventory/adjustments/${encodeURIComponent(b.dataset.adjPostNow)}/post`, { method: 'POST', body: JSON.stringify({ accept_current_stock: true }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The adjustment was not posted.'); return; }
    renderView('inventory:adjustments');
  }));
  list.querySelectorAll('[data-adj-reverse]').forEach(b => b.addEventListener('click', async () => {
    const reason = await showCustomPrompt('Why is this adjustment reversed? Every line goes back the other way, through the same approval as any adjustment.', '', 'Reverse adjustment');
    if (!reason || !reason.trim()) return;
    const res = await apiFetch(`/api/v1/inventory/adjustments/${encodeURIComponent(b.dataset.adjReverse)}/reverse`, { method: 'POST', body: JSON.stringify({ reason: reason.trim() }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The adjustment was not reversed.'); return; }
    const out = await res.json();
    showToast(out.status === 'Posted' ? `${out.number} reverses it and is posted.` : `${out.number} reverses it and waits for approval.`, { variant: out.warning ? 'warning' : 'success' });
    renderView('inventory:adjustments');
  }));

  const input = form.querySelector('[data-adj-add-input]');
  const errorEl = form.querySelector('[data-adj-error]');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const draw = () => {
    form.querySelector('[data-adj-lines]').innerHTML = adjLines.length ? adjLines.map((l, i) => `<tr>
        <td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" class="form-input" style="width:110px;text-align:right;" data-adj-qty="${i}" value="${esc(l.qty)}" placeholder="-2 or 3"></td>
        <td>${rp.reasonSelectHTML(reasons, { value: l.reason_code, attrs: `data-adj-reason="${i}"` })}</td>
        <td><input class="form-input" data-adj-bin="${i}" value="${esc(l.bin || '')}" style="width:100px;" placeholder="optional"></td>
        <td><input class="form-input" data-adj-lot="${i}" value="${esc(l.batch_no || '')}" style="width:100px;" placeholder="optional"></td>
        <td style="text-align:center;"><input type="checkbox" data-adj-damaged="${i}" ${l.damaged ? 'checked' : ''} title="Still yours: moved out of sellable stock (or back with a + change), not written off"></td>
        <td><input class="form-input" data-adj-note="${i}" value="${esc(l.note)}" style="min-width:140px;"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-adj-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="8" style="text-align:center;color:var(--text-muted);padding:16px;">Add the items whose stock is wrong. A negative change takes stock off; a positive one adds it. Tick To Damaged for a damaged unit you still own - it leaves sellable stock without being written off.</td></tr>';
    // Kept in step as typed, never redrawn from an input's own events.
    form.querySelectorAll('[data-adj-qty]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjQty)].qty = el.value; }));
    form.querySelectorAll('[data-adj-reason]').forEach(el => el.addEventListener('change', () => { adjLines[Number(el.dataset.adjReason)].reason_code = el.value; }));
    form.querySelectorAll('[data-adj-note]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjNote)].note = el.value; }));
    form.querySelectorAll('[data-adj-bin]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjBin)].bin = el.value.trim(); }));
    form.querySelectorAll('[data-adj-lot]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjLot)].batch_no = el.value.trim(); }));
    form.querySelectorAll('[data-adj-damaged]').forEach(el => el.addEventListener('change', () => { adjLines[Number(el.dataset.adjDamaged)].damaged = el.checked; }));
    form.querySelectorAll('[data-adj-remove]').forEach(el => el.addEventListener('click', () => { adjLines.splice(Number(el.dataset.adjRemove), 1); draw(); }));
  };
  const addItem = (item) => {
    errorEl.classList.add('hidden');
    const sku = item.code || item.id;
    // Stage 130.14: the same item may come again for another bin or lot.
    adjLines.push({ sku, name: item.name || item.item_name || '', qty: '', reason_code: '', note: '' });
    input.value = '';
    input.dispatchEvent(new Event('input'));
    draw();
    form.querySelector(`[data-adj-qty="${adjLines.length - 1}"]`)?.focus();
  };
  attachLinkTypeahead(input, 'Item', { noSetupHint: true, onPick: (doc) => addItem(doc) });
  form.querySelector('[data-adj-add]').addEventListener('click', async () => {
    const code = input.value.trim();
    if (!code) return;
    const found = await getJSON(`/api/v1/doc/Item?code=${encodeURIComponent(code)}&limit=1`, []);
    const item = found[0] || (await getJSON(`/api/v1/doc/Item?barcode=${encodeURIComponent(code)}&limit=1`, []))[0];
    if (!item) { fail(`No item has the code or barcode ${code}.`); return; }
    addItem(item);
  });
  form.querySelector('[data-adj-save]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    errorEl.classList.add('hidden');
    const store = form.querySelector('#adj-store').value.trim();
    if (!store) { fail('Choose the store whose stock is adjusted.'); return; }
    if (!adjLines.length) { fail('Add at least one item.'); return; }
    const bad = adjLines.find(l => !Number.isInteger(Number(l.qty)) || Number(l.qty) === 0 || !l.reason_code);
    if (bad) { fail(`${bad.sku} needs a whole-number change (+ or -) and a reason.`); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/inventory/adjustments', { method: 'POST', body: JSON.stringify({
      // Stage 74.2.3: a Damage or Expiry loss books through its own posting profile.
      location: store, note: form.querySelector('#adj-note').value.trim(), loss_class: form.querySelector('#adj-loss').value,
      lines: adjLines.map(adjLinePayload) }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The adjustment was not saved.')); return; }
    const out = await res.json();
    adjLines = [];
    txnDone('adj-screen');
    if (out.warning) await showCustomAlert(out.warning, 'Stock adjustment');
    showToast(out.status === 'Posted' ? `${out.number} posted: stock and its value are adjusted.` : `${out.number} is waiting for approval; it posts when approved.`, { variant: out.warning ? 'warning' : 'success' });
    renderView('inventory:adjustments');
  });
  // Stage 130.14 (74.2.3b): what it would do - before / after and the GL - before saving.
  form.querySelector('[data-adj-preview-btn]').addEventListener('click', async () => {
    errorEl.classList.add('hidden');
    const store = form.querySelector('#adj-store').value.trim();
    const res = await apiFetch('/api/v1/inventory/adjustments/preview', { method: 'POST', body: JSON.stringify({
      location: store, loss_class: form.querySelector('#adj-loss').value, lines: adjLines.map(adjLinePayload) }) });
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The preview could not be worked out.')); return; }
    const pv = await res.json();
    form.querySelector('[data-adj-preview]').innerHTML = `<div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Before</th><th class="num">Change</th><th class="num">After</th><th class="num">Value</th></tr></thead>
      <tbody>${(pv.lines || []).map(l => `<tr><td>${esc(l.item_name || l.sku)}${l.bin ? ` <span class="text-muted">bin ${esc(l.bin)}</span>` : ''}${l.condition ? ' <span class="text-muted">to Damaged</span>' : ''}</td>
        <td class="num">${fmtQty(l.before_qty)}</td><td class="num">${num(l.qty) > 0 ? '+' : ''}${fmtQty(l.qty)}</td><td class="num">${fmtQty(l.after_qty)}</td><td class="num">${formatMoney(l.value)}</td></tr>`).join('')}</tbody></table></div>
      <div class="text-muted" style="margin-top:6px;">GL: ${(pv.gl || []).map(g => g.debit ? `Dr ${esc(g.account)} ${formatMoney(g.debit)}` : `Cr ${esc(g.account)} ${formatMoney(g.credit)}`).join(' · ') || 'nothing is booked (condition moves only)'}</div>`;
  });
  draw();
}

// adjLinePayload is one screen line as the server takes it (Stage 130.14).
function adjLinePayload(l) {
  return { sku: l.sku, qty: Number(l.qty), reason_code: l.reason_code, note: l.note, bin: l.bin || '', batch_no: l.batch_no || '', condition: l.damaged ? 'Damaged' : '' };
}

// Sticker Printing opens with these items added and the reprint reason filled.
function reprintStickers(number, ls) {
  try {
    sessionStorage.setItem('erp_sticker_prefill', JSON.stringify({ skus: ls.filter(l => num(l.on_hand) > 0).map(l => l.sku), reason: `MRP change ${number}`, reason_code: 'RPT-MRP' }));
  } catch (_) { /* storage off */ }
  setActiveMenu('menu-stickers');
  renderView('stickers');
}

// ---------------------------------------------------------------------------
// Stage 130.15 (74.2.4) Material Issue: a department asks for store items
// for its own use (approved when a rule covers MaterialIssue), the store
// issues them - stock out, cost to the department's expense and cost centre
// - and what was not used comes back as a Material Return (Good to stock,
// Damaged to QC hold).
// ---------------------------------------------------------------------------

let miLines = [];
let miView = 'all';

async function renderMaterials(container) {
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = await import('./txn-screen.js' + Q);
  const docs = await getJSON('/api/v1/doc/MaterialIssue?sort=recent&limit=200', null);
  if (!docs) { renderErrorPanel(container, 'Failed to load material requests.', () => renderView('inventory:materials')); return; }
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Issued: 'badge-success', Returned: 'badge-success', 'Partly Returned': 'badge-success', Approved: 'badge-warning', 'Pending Approval': 'badge-warning', Rejected: 'badge-danger' };
  // My requests / Ready to issue / Return remaining (74.2.4's three screens) as one list's views.
  const me = localStorage.getItem('erp_username') || '';
  const views = { all: () => true, mine: d => d.requested_by === me, issue: d => d.status === 'Approved', back: d => d.status === 'Issued' || d.status === 'Partly Returned' };
  const shown = docs.filter(views[miView] || views.all);
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `<div style="margin-bottom:10px;"><select class="form-select" data-mi-view style="width:200px;" aria-label="Show">
      ${[['all', 'All requests'], ['mine', 'My requests'], ['issue', 'Ready to issue'], ['back', 'Return remaining']].map(([k, l]) => `<option value="${k}" ${k === miView ? 'selected' : ''}>${l}</option>`).join('')}</select></div>
    <div class="table-wrapper"><table><thead><tr><th>Request</th><th>Store</th><th>Department</th><th>Items</th><th class="num">Value</th><th>Status</th><th></th></tr></thead>
    <tbody>${shown.length ? shown.map(d => {
      const ls = lines(d);
      return `<tr><td style="font-family:monospace;">${esc(d.code || d.id)}</td><td>${esc(d.location)}</td>
        <td>${esc(d.department)}${d.cost_center ? `<div class="text-muted" style="font-size:12px;">${esc(d.cost_center)}</div>` : ''}<div class="text-muted" style="font-size:12px;">${esc(d.purpose)}</div></td>
        <td>${ls.slice(0, 3).map(l => `${fmtQty(l.qty)} ${esc(l.item_name || l.sku)}${num(l.returned_qty) ? ` <span class="text-muted">(${fmtQty(l.returned_qty)} back)</span>` : ''}`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td class="num">${formatMoney(d.amount)}</td><td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span></td>
        <td>${d.status === 'Approved' ? `<button type="button" class="action-btn" data-mi-issue="${esc(d.id)}">Issue</button>` : ''}
          ${d.status === 'Issued' || d.status === 'Partly Returned' ? `<button type="button" class="action-btn" data-mi-return="${esc(d.id)}">Return unused</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="7" style="text-align:center;color:var(--text-muted);padding:20px;">${docs.length ? 'Nothing here in this view.' : 'No material has been asked for yet. + New Material Request starts one.'}</td></tr>`}</tbody></table></div>
    <div data-mi-return-pane style="margin-top:14px;"></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group" style="margin:0;"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  form.innerHTML = `<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      ${field('mi-store', 'Store', 180, `value="${esc(sessionStore())}"`)}${field('mi-dept', 'Department', 180)}${field('mi-cc', 'Cost centre', 160, 'placeholder="optional"')}
      ${field('mi-emp', 'Employee', 160, 'placeholder="optional"')}${field('mi-project', 'Project', 140, 'placeholder="optional"')}
      <div class="form-group" style="margin:0;flex:1;min-width:220px;"><label class="form-label" for="mi-purpose">Purpose</label><input class="form-input" id="mi-purpose" placeholder="e.g. Packing for the weekend sale"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Qty</th><th></th></tr></thead><tbody data-mi-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-mi-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
      <button type="button" class="btn btn-outline" data-mi-add>Add item</button>
    </div>
    <div class="login-error hidden" data-mi-error style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-mi-save>Save request</button></div>`;

  container.appendChild(txnPagedSection({ id: 'mi-screen', title: 'Material Issue', page: 'New material request', count: docs.length, newLabel: 'New Material Request', form, list }));
  txnRestorePane('mi-screen');
  attachLinkTypeahead(form.querySelector('#mi-store'), 'Location', { keepCode: true });
  attachLinkTypeahead(form.querySelector('#mi-dept'), 'Department', { keepCode: true });
  attachLinkTypeahead(form.querySelector('#mi-cc'), 'CostCenter', { keepCode: true });

  list.querySelector('[data-mi-view]').addEventListener('change', e => { miView = e.target.value; renderView('inventory:materials'); });
  list.querySelectorAll('[data-mi-issue]').forEach(b => b.addEventListener('click', async () => {
    b.disabled = true;
    const res = await apiFetch(`/api/v1/inventory/material-requests/${encodeURIComponent(b.dataset.miIssue)}/issue`, { method: 'POST' });
    b.disabled = false;
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The material was not issued.'); return; }
    showToast('Issued: the stock is out and its cost is booked to the department.', { variant: 'success' });
    renderView('inventory:materials');
  }));
  list.querySelectorAll('[data-mi-return]').forEach(b => b.addEventListener('click', () => {
    const d = docs.find(x => x.id === b.dataset.miReturn);
    const pane = list.querySelector('[data-mi-return-pane]');
    const open = lines(d).filter(l => num(l.issued_qty) - num(l.returned_qty) > 0);
    pane.innerHTML = `<h3 style="margin:0 0 8px;">Return unused from ${esc(d.code || d.id)}</h3>
      <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Issued</th><th class="num">Back so far</th><th class="num">Returning now</th><th>Condition</th></tr></thead>
      <tbody>${open.map((l, i) => `<tr><td>${esc(l.item_name || l.sku)}</td><td class="num">${fmtQty(l.issued_qty)}</td><td class="num">${fmtQty(l.returned_qty)}</td>
        <td class="num"><input type="number" step="1" min="0" class="form-input" style="width:100px;text-align:right;" data-mr-qty="${i}" placeholder="0"></td>
        <td><select class="form-select" data-mr-cond="${i}" style="width:130px;"><option>Good</option><option>Damaged</option></select></td></tr>`).join('')}</tbody></table></div>
      <div class="login-error hidden" data-mr-error style="margin-top:10px;"></div>
      <div style="margin-top:10px;"><button type="button" class="btn btn-primary" data-mr-save>Post return</button></div>`;
    const err = pane.querySelector('[data-mr-error]');
    pane.querySelector('[data-mr-save]').addEventListener('click', async e => {
      const btn = e.currentTarget;
      const back = open.map((l, i) => ({ sku: l.sku, qty: Number(pane.querySelector(`[data-mr-qty="${i}"]`).value) || 0, condition: pane.querySelector(`[data-mr-cond="${i}"]`).value })).filter(l => l.qty);
      if (!back.length) { err.textContent = 'Enter how many of each item come back.'; err.classList.remove('hidden'); return; }
      btn.disabled = true;
      const res = await apiFetch(`/api/v1/inventory/material-requests/${encodeURIComponent(d.id)}/return`, { method: 'POST', body: JSON.stringify({ lines: back }) });
      btn.disabled = false;
      if (!res) return;
      if (!res.ok) { err.textContent = await getErrorMessage(res, 'The return was not posted.'); err.classList.remove('hidden'); return; }
      const out = await res.json();
      showToast(`${out.number} posted: good units are back in stock, damaged ones wait in QC hold.`, { variant: 'success' });
      renderView('inventory:materials');
    });
    pane.querySelector('[data-mr-qty="0"]')?.focus();
  }));

  const errorEl = form.querySelector('[data-mi-error]');
  const input = form.querySelector('[data-mi-add-input]');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const draw = () => {
    form.querySelector('[data-mi-lines]').innerHTML = miLines.length ? miLines.map((l, i) => `<tr>
        <td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" min="1" class="form-input" style="width:110px;text-align:right;" data-mi-qty="${i}" value="${esc(l.qty)}"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-mi-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="3" style="text-align:center;color:var(--text-muted);padding:16px;">Add the store items the department needs - stock items only; what is consumed by auto backflush is not asked for here.</td></tr>';
    form.querySelectorAll('[data-mi-qty]').forEach(el => el.addEventListener('input', () => { miLines[Number(el.dataset.miQty)].qty = el.value; }));
    form.querySelectorAll('[data-mi-remove]').forEach(el => el.addEventListener('click', () => { miLines.splice(Number(el.dataset.miRemove), 1); draw(); }));
  };
  const addItem = (item) => {
    errorEl.classList.add('hidden');
    const sku = item.code || item.id;
    if (!miLines.some(l => l.sku === sku)) miLines.push({ sku, name: item.name || item.item_name || '', qty: '' });
    input.value = '';
    input.dispatchEvent(new Event('input'));
    draw();
    form.querySelector(`[data-mi-qty="${miLines.findIndex(l => l.sku === sku)}"]`)?.focus();
  };
  attachLinkTypeahead(input, 'Item', { noSetupHint: true, onPick: addItem });
  form.querySelector('[data-mi-add]').addEventListener('click', async () => {
    const code = input.value.trim();
    if (!code) return;
    const found = await getJSON(`/api/v1/doc/Item?code=${encodeURIComponent(code)}&limit=1`, []);
    const item = found[0] || (await getJSON(`/api/v1/doc/Item?barcode=${encodeURIComponent(code)}&limit=1`, []))[0];
    if (!item) { fail(`No item has the code or barcode ${code}.`); return; }
    addItem(item);
  });
  form.querySelector('[data-mi-save]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    errorEl.classList.add('hidden');
    const v = (id) => form.querySelector(id).value.trim();
    if (!v('#mi-store')) { fail('Choose the store the material comes from.'); return; }
    if (!v('#mi-dept') || !v('#mi-purpose')) { fail('Say which department it is for, and what for.'); return; }
    if (!miLines.length) { fail('Add at least one item.'); return; }
    const bad = miLines.find(l => !Number.isInteger(Number(l.qty)) || Number(l.qty) <= 0);
    if (bad) { fail(`Enter how many ${bad.sku} are needed (a whole number).`); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/inventory/material-requests', { method: 'POST', body: JSON.stringify({
      location: v('#mi-store'), department: v('#mi-dept'), cost_center: v('#mi-cc'), employee: v('#mi-emp'), project: v('#mi-project'), purpose: v('#mi-purpose'),
      lines: miLines.map(l => ({ sku: l.sku, qty: Number(l.qty) })) }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The request was not saved.')); return; }
    const out = await res.json();
    miLines = [];
    txnDone('mi-screen');
    if (out.warning) await showCustomAlert(out.warning, 'Material request');
    showToast(out.status === 'Approved' ? `${out.number} is ready to issue.` : `${out.number} is waiting for approval; issue it once approved.`, { variant: out.warning ? 'warning' : 'success' });
    renderView('inventory:materials');
  });
  draw();
}

// ---------------------------------------------------------------------------
// Stage 130.16 (74.3.1) Delivery Challans: goods that leave a store without a
// sale - job work, repair, exhibition, samples, or given away. Returnable
// goods stay yours ("with party") until they come back or a write-off is
// approved; non-returnable goods leave stock at dispatch.
// ---------------------------------------------------------------------------

let dcLines = [];
let dcView = 'all';
const DC_PURPOSES = ['Job work', 'Repair', 'Exhibition', 'Sample', 'Returnable - other', 'Non-returnable', 'Other'];

async function renderChallans(container) {
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = await import('./txn-screen.js' + Q);
  const docs = await getJSON('/api/v1/doc/DeliveryChallan?sort=recent&limit=200', null);
  if (!docs) { renderErrorPanel(container, 'Failed to load delivery challans.', () => renderView('inventory:challans')); return; }
  const parse = (v) => { try { return typeof v === 'string' ? JSON.parse(v || '[]') : (v || []); } catch (_) { return []; } };
  const open = (d) => parse(d.challan_balance).reduce((n, b) => n + (b.issued - b.returned - b.damaged - b.rejected - b.lost - b.kept), 0);
  const today = new Date().toISOString().slice(0, 10);
  const soon = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10);
  const due = (d) => parse(d.items).map(l => l.expected_return).filter(Boolean).sort()[0] || '';
  const out = (d) => d.status === 'Dispatched' || d.status === 'Partly Returned';
  const views = { all: () => true, open: d => out(d) && open(d) > 0, soon: d => out(d) && open(d) > 0 && due(d) && due(d) <= soon,
    exceptions: d => out(d) && ((due(d) && due(d) < today) || parse(d.challan_balance).some(b => b.damaged || b.rejected || b.lost)),
    ewb: d => d.ewb_required === 'Yes' };
  const shown = docs.filter(views[dcView] || views.all);
  const badge = { Dispatched: 'badge-warning', 'Partly Returned': 'badge-warning', Returned: 'badge-success', Closed: 'badge-secondary', Approved: 'badge-warning', 'Pending Approval': 'badge-warning', Cancelled: 'badge-danger', Rejected: 'badge-danger' };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `<div style="margin-bottom:10px;"><select class="form-select" data-dc-view style="width:200px;" aria-label="Show">
      ${[['all', 'All challans'], ['open', 'Open returns'], ['soon', 'Due back in 7 days'], ['exceptions', 'Exceptions'], ['ewb', 'E-way bill needed']].map(([k, l]) => `<option value="${k}" ${k === dcView ? 'selected' : ''}>${l}</option>`).join('')}</select></div>
    <div class="table-wrapper"><table><thead><tr><th>Challan</th><th>Purpose / party</th><th>Store</th><th>Items</th><th class="num">Value</th><th>Expected back</th><th>Status</th><th></th></tr></thead>
    <tbody>${shown.length ? shown.map(d => {
      const ls = parse(d.items);
      const o = open(d);
      const late = out(d) && o > 0 && due(d) && due(d) < today;
      return `<tr><td style="font-family:monospace;">${esc(d.code || d.id)}</td>
        <td>${esc(d.purpose)}${d.returnable === false ? ' <span class="text-muted">(non-returnable)</span>' : ''}<div class="text-muted" style="font-size:12px;">${esc(d.party_name)}${d.party_gstin ? ' · ' + esc(d.party_gstin) : ''}</div></td>
        <td>${esc(d.location)}</td>
        <td>${ls.slice(0, 3).map(l => `${fmtQty(l.qty)} ${esc(l.item_name || l.sku)}`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}${out(d) && d.returnable !== false ? `<div class="text-muted" style="font-size:12px;">${fmtQty(o)} still out</div>` : ''}</td>
        <td class="num">${formatMoney(d.amount)}</td>
        <td>${esc(due(d))}${late ? ' <span class="badge badge-danger">overdue</span>' : ''}</td>
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${d.ewb_required === 'Yes' ? ' <span class="badge badge-warning" title="E-way bill needed">EWB</span>' : ''}</td>
        <td style="white-space:nowrap;">${d.status === 'Approved' ? `<button type="button" class="action-btn" data-dc-act="dispatch" data-id="${esc(d.id)}">Dispatch</button> ` : ''}
          ${out(d) && d.returnable !== false && o > 0 ? `<button type="button" class="action-btn" data-dc-act="return" data-id="${esc(d.id)}">Receive return</button> <button type="button" class="action-btn" data-dc-act="writeoff" data-id="${esc(d.id)}">Write off</button> ` : ''}
          ${['Draft', 'Pending Approval', 'Approved'].includes(d.status) ? `<button type="button" class="action-btn action-btn-danger" data-dc-act="cancel" data-id="${esc(d.id)}">Cancel</button> ` : ''}
          ${d.status !== 'Draft' && d.status !== 'Cancelled' ? `<button type="button" class="action-btn" data-dc-act="print" data-id="${esc(d.id)}">Print</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="8" style="text-align:center;color:var(--text-muted);padding:20px;">${docs.length ? 'Nothing here in this view.' : 'No goods have gone out on a challan yet. + New Delivery Challan starts one.'}</td></tr>`}</tbody></table></div>
    <div data-dc-pane style="margin-top:14px;"></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group" style="margin:0;"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  form.innerHTML = `<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      ${field('dc-store', 'From store', 160, `value="${esc(sessionStore())}"`)}
      <div class="form-group" style="margin:0;"><label class="form-label" for="dc-purpose">Purpose</label><select class="form-select" id="dc-purpose" style="width:170px;">${DC_PURPOSES.map(p => `<option>${p}</option>`).join('')}</select></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="dc-returnable">Comes back</label><select class="form-select" id="dc-returnable" style="width:110px;"><option value="yes">Yes</option><option value="no">No</option></select></div>
      ${field('dc-expected', 'Expected back', 150, 'type="date"')}
      ${field('dc-ref', 'Reference', 140, 'placeholder="optional"')}
    </div>
    <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="dc-ptype">Party type</label><select class="form-select" id="dc-ptype" style="width:140px;"><option>Job worker</option><option>Vendor</option><option>Customer</option><option>Other</option></select></div>
      ${field('dc-party', 'Consignee', 200)}${field('dc-gstin', 'GSTIN', 170, 'placeholder="if registered"')}${field('dc-state', 'State', 130)}
      <div class="form-group" style="margin:0;flex:1;min-width:220px;"><label class="form-label" for="dc-address">Address</label><input class="form-input" id="dc-address"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Qty</th><th>Lot</th><th>Serial numbers</th><th class="num">Value</th><th></th></tr></thead><tbody data-dc-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-dc-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
      <button type="button" class="btn btn-outline" data-dc-add>Add item</button>
    </div>
    <div class="login-error hidden" data-dc-error style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-dc-save>Save challan</button></div>`;

  container.appendChild(txnPagedSection({ id: 'dc-screen', title: 'Delivery Challan', page: 'New delivery challan', count: docs.length, newLabel: 'New Delivery Challan', form, list }));
  txnRestorePane('dc-screen');
  attachLinkTypeahead(form.querySelector('#dc-store'), 'Location', { keepCode: true });
  const purposeEl = form.querySelector('#dc-purpose');
  purposeEl.addEventListener('change', () => { form.querySelector('#dc-returnable').value = ['Non-returnable', 'Other'].includes(purposeEl.value) ? 'no' : 'yes'; });

  const pane = list.querySelector('[data-dc-pane]');
  const post = async (url, body, okMsg) => {
    const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
    if (!res) return false;
    if (!res.ok) { await showApiError(res, 'That did not go through.'); return false; }
    showToast(okMsg, { variant: 'success' });
    renderView('inventory:challans');
    return true;
  };
  list.querySelector('[data-dc-view]').addEventListener('change', e => { dcView = e.target.value; renderView('inventory:challans'); });
  list.querySelectorAll('[data-dc-act]').forEach(b => b.addEventListener('click', async () => {
    const d = docs.find(x => x.id === b.dataset.id);
    const url = `/api/v1/inventory/challans/${encodeURIComponent(d.id)}`;
    const act = b.dataset.dcAct;
    if (act === 'print') { (await import('./trade-docs.js' + Q)).printTransactionDocument('DeliveryChallan', d.id); return; }
    if (act === 'cancel') {
      const why = String((await showCustomPrompt('Why is this challan cancelled?', '', 'Cancel challan')) || '').trim();
      if (why) await post(`${url}/cancel`, { reason: why }, `${d.code || d.id} cancelled.`);
      return;
    }
    if (act === 'dispatch') {
      pane.innerHTML = `<h3 style="margin:0 0 8px;">Dispatch ${esc(d.code || d.id)}</h3>
        <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">
          <div class="form-group" style="margin:0;"><label class="form-label" for="dcd-mode">Transport</label><select class="form-select" id="dcd-mode" style="width:120px;"><option>Road</option><option>Rail</option><option>Air</option><option>Ship</option><option>By hand</option></select></div>
          ${field('dcd-vehicle', 'Vehicle no', 130)}${field('dcd-lr', 'LR / AWB no', 130)}${field('dcd-transporter', 'Transporter', 160)}${field('dcd-tid', 'Transporter ID', 150)}${field('dcd-pk', 'Packages', 90, 'type="number" min="0" step="1"')}
          ${field('dcd-date', 'Dispatch date', 150, `type="date" value="${today}"`)}
          <button type="button" class="btn btn-primary" data-dcd-go>Dispatch</button></div>
        ${d.ewb_required === 'Yes' ? '<div class="txn-hint" style="margin-top:8px;">This challan needs an e-way bill before the goods move.</div>' : ''}`;
      pane.querySelector('[data-dcd-go]').addEventListener('click', () => post(`${url}/dispatch`, {
        transport_mode: pane.querySelector('#dcd-mode').value, vehicle_no: pane.querySelector('#dcd-vehicle').value, lr_awb_no: pane.querySelector('#dcd-lr').value,
        transporter: pane.querySelector('#dcd-transporter').value, transporter_id: pane.querySelector('#dcd-tid').value,
        package_count: Number(pane.querySelector('#dcd-pk').value) || 0, dispatch_date: pane.querySelector('#dcd-date').value }, `${d.code || d.id} dispatched.`));
      return;
    }
    const bal = parse(d.challan_balance).filter(x => x.issued - x.returned - x.damaged - x.rejected - x.lost - x.kept > 0);
    const left = (x) => x.issued - x.returned - x.damaged - x.rejected - x.lost - x.kept;
    const outSerials = (x) => Object.entries(x.serials || {}).filter(([, s]) => s === 'With party').map(([k]) => k);
    if (act === 'return') {
      pane.innerHTML = `<h3 style="margin:0 0 8px;">What came back on ${esc(d.code || d.id)}</h3>
        <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Still out</th><th class="num">Good</th><th class="num">Damaged</th><th class="num">Rejected</th><th>Serial numbers back</th><th>Reason (damaged / rejected)</th></tr></thead>
        <tbody>${bal.map((x, i) => `<tr><td>${esc(x.sku)}${x.batch_no ? ` <span class="text-muted">lot ${esc(x.batch_no)}</span>` : ''}</td><td class="num">${fmtQty(left(x))}</td>
          ${['good', 'damaged', 'rejected'].map(c => `<td class="num"><input type="number" min="0" step="1" class="form-input" style="width:80px;text-align:right;" data-dcr="${c}" data-i="${i}" placeholder="0"></td>`).join('')}
          <td>${outSerials(x).length ? `<input class="form-input" data-dcr="serials" data-i="${i}" placeholder="${esc(outSerials(x).slice(0, 3).join(', '))}" style="min-width:160px;">` : '<span class="text-muted">-</span>'}</td>
          <td><input class="form-input" data-dcr="reason" data-i="${i}" style="min-width:140px;"></td></tr>`).join('')}</tbody></table></div>
        <div class="txn-hint" style="margin-top:6px;">For an item that went out with serial numbers, list the serials that came back (comma separated) in the column of their condition's row - one condition per save.</div>
        <div style="margin-top:10px;"><button type="button" class="btn btn-primary" data-dcr-go>Receive</button></div>`;
      pane.querySelector('[data-dcr-go]').addEventListener('click', () => {
        const v = (c, i) => pane.querySelector(`[data-dcr="${c}"][data-i="${i}"]`);
        const lines = [];
        bal.forEach((x, i) => {
          const serials = (v('serials', i)?.value || '').split(',').map(s => s.trim()).filter(Boolean);
          [['good', 'Good'], ['damaged', 'Damaged'], ['rejected', 'Rejected']].forEach(([c, cond]) => {
            const qty = Number(v(c, i).value) || 0;
            if (qty > 0) lines.push({ sku: x.sku, batch_no: x.batch_no || '', qty, condition: cond, serials: serials.length ? serials : undefined, reason: v('reason', i).value.trim() });
          });
        });
        if (!lines.length) { showToast('Enter how many came back.', { variant: 'warning' }); return; }
        post(`${url}/return`, { return_key: `${d.id}:${Date.now()}`, lines }, 'Return received.');
      });
      return;
    }
    if (act === 'writeoff') {
      pane.innerHTML = `<h3 style="margin:0 0 8px;">Write off from ${esc(d.code || d.id)}</h3>
        <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">
          <div class="form-group" style="margin:0;"><label class="form-label" for="dcw-line">Item</label><select class="form-select" id="dcw-line" style="width:220px;">${bal.map((x, i) => `<option value="${i}">${esc(x.sku)}${x.batch_no ? ' / ' + esc(x.batch_no) : ''} (${left(x)} out)</option>`).join('')}</select></div>
          ${field('dcw-qty', 'Qty', 90, 'type="number" min="1" step="1"')}
          <div class="form-group" style="margin:0;"><label class="form-label" for="dcw-kind">What happened</label><select class="form-select" id="dcw-kind" style="width:150px;"><option>Lost</option><option>Kept by party</option></select></div>
          ${field('dcw-serials', 'Serial numbers', 170, 'placeholder="if it has them"')}
          <div class="form-group" style="margin:0;flex:1;min-width:200px;"><label class="form-label" for="dcw-reason">Reason</label><input class="form-input" id="dcw-reason"></div>
          <button type="button" class="btn btn-primary" data-dcw-go>Send for approval</button></div>
        <div class="txn-hint" style="margin-top:6px;">It stays open on the challan until someone else approves the write-off.</div>`;
      pane.querySelector('[data-dcw-go]').addEventListener('click', () => {
        const x = bal[Number(pane.querySelector('#dcw-line').value)];
        const serials = pane.querySelector('#dcw-serials').value.split(',').map(s => s.trim()).filter(Boolean);
        post(`${url}/write-off`, { sku: x.sku, batch_no: x.batch_no || '', qty: Number(pane.querySelector('#dcw-qty').value) || 0, kind: pane.querySelector('#dcw-kind').value,
          serials, reason: pane.querySelector('#dcw-reason').value.trim() }, 'Write-off sent for approval.');
      });
    }
  }));

  const errorEl = form.querySelector('[data-dc-error]');
  const input = form.querySelector('[data-dc-add-input]');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const draw = () => {
    form.querySelector('[data-dc-lines]').innerHTML = dcLines.length ? dcLines.map((l, i) => `<tr>
        <td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" min="1" class="form-input" style="width:90px;text-align:right;" data-dcl="qty" data-i="${i}" value="${esc(l.qty)}"></td>
        <td><input class="form-input" data-dcl="batch_no" data-i="${i}" value="${esc(l.batch_no || '')}" style="width:100px;" placeholder="optional"></td>
        <td><input class="form-input" data-dcl="serials" data-i="${i}" value="${esc(l.serials || '')}" style="min-width:160px;" placeholder="optional, comma separated"></td>
        <td class="num"><input type="number" step="0.01" min="0" class="form-input" style="width:110px;text-align:right;" data-dcl="declared_value" data-i="${i}" value="${esc(l.declared_value || '')}" placeholder="at cost"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-dc-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:16px;">Add the items going out. Value is the declared value for the challan; blank uses their cost.</td></tr>';
    form.querySelectorAll('[data-dcl]').forEach(el => el.addEventListener('input', () => { dcLines[Number(el.dataset.i)][el.dataset.dcl] = el.value; }));
    form.querySelectorAll('[data-dc-remove]').forEach(el => el.addEventListener('click', () => { dcLines.splice(Number(el.dataset.dcRemove), 1); draw(); }));
  };
  const addItem = (item) => {
    errorEl.classList.add('hidden');
    dcLines.push({ sku: item.code || item.id, name: item.name || item.item_name || '', qty: '' });
    input.value = '';
    input.dispatchEvent(new Event('input'));
    draw();
    form.querySelector(`[data-dcl="qty"][data-i="${dcLines.length - 1}"]`)?.focus();
  };
  attachLinkTypeahead(input, 'Item', { noSetupHint: true, onPick: addItem });
  form.querySelector('[data-dc-add]').addEventListener('click', async () => {
    const code = input.value.trim();
    if (!code) return;
    const found = await getJSON(`/api/v1/doc/Item?code=${encodeURIComponent(code)}&limit=1`, []);
    const item = found[0] || (await getJSON(`/api/v1/doc/Item?barcode=${encodeURIComponent(code)}&limit=1`, []))[0];
    if (!item) { fail(`No item has the code or barcode ${code}.`); return; }
    addItem(item);
  });
  form.querySelector('[data-dc-save]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    errorEl.classList.add('hidden');
    const v = (id) => form.querySelector(id).value.trim();
    if (!v('#dc-store')) { fail('Choose the store the goods leave from.'); return; }
    if (!v('#dc-party')) { fail('Say who the goods go to.'); return; }
    if (!dcLines.length) { fail('Add at least one item.'); return; }
    const bad = dcLines.find(l => !Number.isInteger(Number(l.qty)) || Number(l.qty) <= 0);
    if (bad) { fail(`Enter how many ${bad.sku} go out (a whole number).`); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/inventory/challans', { method: 'POST', body: JSON.stringify({
      location: v('#dc-store'), purpose: v('#dc-purpose'), returnable: v('#dc-returnable') === 'yes', expected_return: v('#dc-expected'), reference: v('#dc-ref'),
      party_type: v('#dc-ptype'), party_name: v('#dc-party'), party_gstin: v('#dc-gstin'), party_state: v('#dc-state'), party_address: v('#dc-address'),
      lines: dcLines.map(l => ({ sku: l.sku, qty: Number(l.qty), batch_no: (l.batch_no || '').trim(), declared_value: Number(l.declared_value) || 0,
        serials: String(l.serials || '').split(',').map(s => s.trim()).filter(Boolean) })) }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The challan was not saved.')); return; }
    const saved = await res.json();
    dcLines = [];
    txnDone('dc-screen');
    if (saved.warning) await showCustomAlert(saved.warning, 'Delivery challan');
    showToast(saved.status === 'Approved' ? `${saved.number} is ready to dispatch.` : `${saved.number} is waiting for approval; dispatch it once approved.`, { variant: saved.warning ? 'warning' : 'success' });
    renderView('inventory:challans');
  });
  draw();
}

const PAGES = { dashboard: renderDashboard, ledger: renderLedger, adjustments: renderAdjustments, mrp: renderMRPChanges, materials: renderMaterials, challans: renderChallans };

export async function renderInventoryPage(tab, container) {
  if (!PAGES[tab]) return false;
  await PAGES[tab](container);
  return true;
}
