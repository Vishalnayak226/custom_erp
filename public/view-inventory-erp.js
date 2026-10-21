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
  const result = await getJSON(`/api/v1/reports/run/stock-ledger?${params}`, null);
  const host = panel(container, '');
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
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${problem ? `<div style="color:var(--danger-strong);font-size:12px;max-width:260px;">${esc(problem)}</div>` : ''}</td>
        <td>${d.status === 'Approved' ? `<button type="button" class="action-btn" data-adj-post="${esc(d.id)}">Post</button>` : ''}</td></tr>`;
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
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Change (+ / -)</th><th>Reason</th><th>Note</th><th></th></tr></thead><tbody data-adj-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-adj-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
      <button type="button" class="btn btn-outline" data-adj-add>Add item</button>
    </div>
    <div class="login-error hidden" data-adj-error style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-adj-save>Save adjustment</button></div>`;

  container.appendChild(txnPagedSection({ id: 'adj-screen', title: 'Stock Adjustment', page: 'New stock adjustment', count: docs.length, newLabel: 'New Stock Adjustment', form, list }));
  txnRestorePane('adj-screen');
  attachLinkTypeahead(form.querySelector('#adj-store'), 'Location', { keepCode: true });
  list.querySelectorAll('[data-adj-post]').forEach(b => b.addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/inventory/adjustments/${encodeURIComponent(b.dataset.adjPost)}/post`, { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The adjustment was not posted.'); return; }
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
        <td><input class="form-input" data-adj-note="${i}" value="${esc(l.note)}" style="min-width:140px;"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-adj-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:16px;">Add the items whose stock is wrong. A negative change takes stock off; a positive one adds it.</td></tr>';
    // Kept in step as typed, never redrawn from an input's own events.
    form.querySelectorAll('[data-adj-qty]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjQty)].qty = el.value; }));
    form.querySelectorAll('[data-adj-reason]').forEach(el => el.addEventListener('change', () => { adjLines[Number(el.dataset.adjReason)].reason_code = el.value; }));
    form.querySelectorAll('[data-adj-note]').forEach(el => el.addEventListener('input', () => { adjLines[Number(el.dataset.adjNote)].note = el.value; }));
    form.querySelectorAll('[data-adj-remove]').forEach(el => el.addEventListener('click', () => { adjLines.splice(Number(el.dataset.adjRemove), 1); draw(); }));
  };
  const addItem = (item) => {
    errorEl.classList.add('hidden');
    const sku = item.code || item.id;
    if (adjLines.some(l => l.sku === sku)) { fail(`${sku} is already on this adjustment.`); return; }
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
      lines: adjLines.map(l => ({ sku: l.sku, qty: Number(l.qty), reason_code: l.reason_code, note: l.note })) }) });
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
  draw();
}

// Sticker Printing opens with these items added and the reprint reason filled.
function reprintStickers(number, ls) {
  try {
    sessionStorage.setItem('erp_sticker_prefill', JSON.stringify({ skus: ls.filter(l => num(l.on_hand) > 0).map(l => l.sku), reason: `MRP change ${number}` }));
  } catch (_) { /* storage off */ }
  setActiveMenu('menu-stickers');
  renderView('stickers');
}

const PAGES = { dashboard: renderDashboard, ledger: renderLedger, adjustments: renderAdjustments, mrp: renderMRPChanges };

export async function renderInventoryPage(tab, container) {
  if (!PAGES[tab]) return false;
  await PAGES[tab](container);
  return true;
}
