// Stage 88.5 (D9): Inventory as an ERP module - Dashboard, Inventory Ledger
// and MRP Change, each opened from the Inventory menu as inventory:<page>
// (renderInventoryView hands over here). Stock on Hand stays the plain
// inventory view; Stock Movement is the transfer screen.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const fmtQty = (v) => formatNumber(num(v));
const when = (v) => { const d = new Date(v); return Number.isNaN(d.getTime()) ? esc(v || '') : esc(formatDateTime(d)); };

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

function bar(container, title, extra = '') {
  const el = document.createElement('div');
  el.className = 'page-header';
  el.innerHTML = `<div class="page-title-section"><h1 class="page-title">${esc(title)}</h1></div><div class="s-b32917">${extra}</div>`;
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
// 88.5.1 Dashboard - Stage 145.1 (user, 2026-10-31: "Need management level
// dashboard. It should not scrol."): how much stock there is at the top-bar
// store, on one page - tiles, then By item (the short name) beside By store
// (All stores) or By category (one store).
// ---------------------------------------------------------------------------

async function renderDashboard(container) {
  const d = await getJSON('/api/v1/inventory/dashboard', null);
  bar(container, 'Inventory home', '<button type="button" class="btn btn-outline" data-inv-refresh>Refresh</button>');
  container.querySelector('[data-inv-refresh]').addEventListener('click', () => renderView('inventory:dashboard'));
  if (!d) { renderErrorPanel(container, 'Failed to load Inventory home.', () => renderView('inventory:dashboard')); return; }
  const showValue = !d.value_hidden;
  const pending = (d.pending_adjustments || []).length;
  const tiles = document.createElement('div');
  tiles.className = 'dashboard-stats-row inv-dash-tiles';
  tiles.innerHTML = [
    ['Designs', fmtQty(d.designs)],
    ['Qty', fmtQty(d.total_units)],
    ['Combinations', fmtQty(d.combinations)],
    showValue ? ['Stock value', formatMoney(d.total_value)] : null,
    pending ? ['Awaiting approval', fmtQty(pending), 'approvals'] : null
  ].filter(Boolean).map(([label, val, go]) => `<div class="stat-card"${go ? ` data-inv-go="${go}"` : ''}><div class="stat-info"><span class="stat-label">${esc(label)}</span><span class="stat-val">${esc(val)}</span></div></div>`).join('');
  container.appendChild(tiles);
  const approvals = tiles.querySelector('[data-inv-go="approvals"]');
  if (approvals) makeClickable(approvals, () => { setActiveMenu('menu-approvals'); renderView('approvals'); });

  // As many rows as the window holds, so the page never scrolls; the rest
  // fold into one Others row.
  const fit = Math.max(4, Math.min(12, Math.floor((window.innerHeight - 400) / 33)));
  const ranked = (title, head, list) => {
    list = list.filter(r => num(r.units) !== 0 || num(r.items) > 0);
    const shown = list.length > fit ? list.slice(0, fit - 1) : list;
    const rest = list.slice(shown.length);
    const max = Math.max(1, ...shown.map(r => num(r.units)));
    const line = r => `<tr title="${esc(`${r.name}: ${fmtQty(r.units)} qty in ${fmtQty(r.designs)} designs`)}"><td class="inv-dash-name">${esc(r.name)}</td>
      <td class="num">${fmtQty(r.designs)}</td><td class="num">${fmtQty(r.units)}</td>
      <td class="inv-dash-share"><div class="inv-dash-bar"><span data-css="width:${Math.max(0, Math.min(100, num(r.units) / max * 100)).toFixed(1)}%"></span></div></td>
      ${showValue ? `<td class="num">${formatMoney(r.value)}</td>` : ''}</tr>`;
    const others = rest.length ? `<tr class="inv-dash-other"><td class="inv-dash-name">Others (${rest.length})</td><td></td>
      <td class="num">${fmtQty(rest.reduce((t, r) => t + num(r.units), 0))}</td><td></td>
      ${showValue ? `<td class="num">${formatMoney(rest.reduce((t, r) => t + num(r.value), 0))}</td>` : ''}</tr>` : '';
    const cols = showValue ? 5 : 4;
    return `<div class="table-panel"><h2>${esc(title)}</h2><div class="table-wrapper"><table class="inv-dash-table">
      <thead><tr><th>${esc(head)}</th><th class="num">Designs</th><th class="num">Qty</th><th aria-label="Share of qty"></th>${showValue ? '<th class="num">Value</th>' : ''}</tr></thead>
      <tbody>${shown.length ? shown.map(line).join('') + others
        : `<tr><td colspan="${cols}" class="s-b4fdf8">No stock here yet.</td></tr>`}</tbody></table></div></div>`;
  };
  const grid = document.createElement('div');
  grid.className = 'inv-dash-grid';
  grid.innerHTML = ranked('By item', 'Item', d.by_item || [])
    + (d.store ? ranked('By category', 'Category', d.by_category || [])
      : ranked('By store', 'Store', (d.by_location || []).map(r => ({ ...r, name: r.name || r.location }))));
  // Names here are already names - the link sweep has nothing to decode.
  grid.querySelectorAll('td').forEach(td => { td.dataset.linkSwept = '1'; });
  container.appendChild(grid);
}

// ---------------------------------------------------------------------------
// 88.5.2 Inventory Ledger - every movement, with each item's running balance
// at its store (the stock-ledger report).
// ---------------------------------------------------------------------------

const LEDGER_LIMIT = 500;
let ledgerOffset = 0;
const ledgerFilter = { sku: '', location_code: '', voucher_type: '', start: '', end: '' };

async function renderLedger(container) {
  if (!ledgerFilter.start && !ledgerFilter.end) {
    const d = new Date(Date.now() - 30 * 86400000);
    ledgerFilter.start = d.toISOString().slice(0, 10);
  }
  // Stage 145.2: the Store is the top-bar store, unless the lists show every store.
  const working = await loadViewModule('/view-store.js?v=1').then(m => m.ensureWorkingStore()).catch(() => null);
  const fixedStore = working && working.code && !window.erpStoreViewAll ? working : null;
  bar(container, 'Inventory Ledger');
  const filters = panel(container, `<div class="s-97380c">
      <div class="form-group s-116966"><label class="form-label" for="inv-l-sku">Item</label><input class="form-input s-54f91a" id="inv-l-sku" value="${esc(ledgerFilter.sku)}" autocomplete="off"></div>
      <div class="form-group s-116966"><label class="form-label" for="inv-l-loc">Store</label>${fixedStore
        ? `<input class="form-input s-7c07cd" id="inv-l-loc" value="${esc(fixedStore.name || fixedStore.code)}" readonly title="The store picked in the top bar">`
        : `<input class="form-input s-7c07cd" id="inv-l-loc" value="${esc(ledgerFilter.location_code)}" autocomplete="off">`}</div>
      <div class="form-group s-116966"><label class="form-label" for="inv-l-type">Document type</label><input class="form-input s-7c07cd" id="inv-l-type" value="${esc(ledgerFilter.voucher_type)}" placeholder="All types"></div>
      <div class="form-group s-116966"><label class="form-label" for="inv-l-start">From</label><input type="date" class="form-input" id="inv-l-start" value="${esc(ledgerFilter.start)}"></div>
      <div class="form-group s-116966"><label class="form-label" for="inv-l-end">To</label><input type="date" class="form-input" id="inv-l-end" value="${esc(ledgerFilter.end)}"></div>
      <button type="button" class="btn btn-primary" data-inv-l-show>Show</button>
      <button type="button" class="btn btn-outline" data-inv-l-clear>Clear</button>
    </div>`);
  attachLinkTypeahead(document.getElementById('inv-l-sku'), 'Item', { noSetupHint: true });
  if (!fixedStore) attachLinkTypeahead(document.getElementById('inv-l-loc'), 'Location', { noSetupHint: true });
  const read = () => {
    ledgerFilter.sku = document.getElementById('inv-l-sku').value.trim();
    if (!fixedStore) ledgerFilter.location_code = document.getElementById('inv-l-loc').value.trim();
    ledgerFilter.voucher_type = document.getElementById('inv-l-type').value.trim();
    ledgerFilter.start = document.getElementById('inv-l-start').value;
    ledgerFilter.end = document.getElementById('inv-l-end').value;
  };
  filters.querySelector('[data-inv-l-show]').addEventListener('click', () => { read(); ledgerOffset = 0; renderView('inventory:ledger'); });
  filters.querySelector('[data-inv-l-clear]').addEventListener('click', () => { Object.keys(ledgerFilter).forEach(k => { ledgerFilter[k] = ''; }); ledgerOffset = 0; renderView('inventory:ledger'); });

  const params = new URLSearchParams(Object.entries({ ...ledgerFilter, ...(fixedStore ? { location_code: fixedStore.code } : {}) }).filter(([, v]) => v));
  const res = await apiFetch(`/api/v1/reports/run/stock-ledger?${params}&page_size=${LEDGER_LIMIT}&page_offset=${ledgerOffset}&page_order=newest`);
  const host = panel(container, '');
  // 134.6: past the report's row cap the server refuses (REPORT-0161) rather
  // than load every movement - say how to narrow it, the filters stay above.
  if (res && res.status === 422) {
    const body = await res.json().catch(() => ({}));
    if (body.code === 'REPORT-0161') {
      host.innerHTML = '<p class="text-muted s-c74152">More movements match than one screen can load. Choose an item or a store, or a shorter date range, above and click Show.</p>';
      return;
    }
  }
  const result = res && res.ok ? await res.json().catch(() => null) : null;
  if (!result) { renderErrorPanel(host, 'Failed to load the inventory ledger.', () => renderView('inventory:ledger')); return; }
  const rows = Array.isArray(result) ? result : (result.rows || []);
  const total = (result.paging && result.paging.total) || rows.length;
  host.innerHTML = `<div class="table-wrapper"><table>
    <thead><tr><th>When</th><th>Item</th><th>Store</th><th class="num">In</th><th class="num">Out</th><th class="num">Balance</th><th>Document</th><th>From → to</th><th>By</th></tr></thead>
    <tbody>${rows.length ? rows.map(r => {
      const q = num(r.qty);
      const moved = [r.from_location_id || r.from_status, r.to_location_id || r.to_status].filter(Boolean).join(' → ');
      // 276.8: the short values stay on one line.
      return `<tr><td class="nowrap">${when(r.created_at)}</td><td class="nowrap">${esc(r.item_id)}</td><td class="nowrap">${esc(r.warehouse_id)}</td>
        <td class="num">${q > 0 ? fmtQty(q) : ''}</td><td class="num">${q < 0 ? fmtQty(-q) : ''}</td><td class="num">${fmtQty(r.running_balance)}</td>
        <td>${esc(r.voucher_type)} <span class="text-muted">${esc(r.voucher_id || '')}</span></td><td class="nowrap">${esc(moved)}</td><td class="nowrap">${esc(r.user_id || '')}</td></tr>`;
    }).join('') : '<tr><td colspan="9" class="s-b4fdf8">No movement matches. Widen the dates or clear the filters.</td></tr>'}</tbody></table></div>
    ${total > rows.length ? `<div class="text-muted s-c7bd98"><button type="button" class="btn btn-outline" data-inv-l-newer ${ledgerOffset > 0 ? '' : 'disabled'}>Newer</button><span>${ledgerOffset + 1}-${ledgerOffset + rows.length} of ${total}</span><button type="button" class="btn btn-outline" data-inv-l-older ${ledgerOffset + rows.length < total ? '' : 'disabled'}>Older</button></div>` : ''}`;
  host.querySelector('[data-inv-l-newer]')?.addEventListener('click', () => { ledgerOffset = Math.max(0, ledgerOffset - LEDGER_LIMIT); renderView('inventory:ledger'); });
  host.querySelector('[data-inv-l-older]')?.addEventListener('click', () => { ledgerOffset += LEDGER_LIMIT; renderView('inventory:ledger'); });
}

// ---------------------------------------------------------------------------
// 88.5.5 MRP Change - the list is every item's MRP history; + New changes MRP
// from an effective date and offers the sticker reprint.
// ---------------------------------------------------------------------------

let mrpLines = [];
let mrpList = null; // 267.3: the history's search and page

async function renderMRPChanges(container) {
  const txn = await import('./txn-screen.js' + Q);
  const { txnPagedSection, txnRestorePane, txnDone } = txn;
  mrpList = mrpList || txn.txnListState();
  const page = await txn.txnLoadList('MRPChange', mrpList);
  if (page.error) { renderErrorPanel(container, page.error, () => renderView('inventory:mrp')); return; }
  const docs = page.docs;
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Applied: 'badge-success', Scheduled: 'badge-warning', Cancelled: 'badge-secondary', Draft: 'badge-secondary' };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `${txn.txnListBar(mrpList, page, null, "Find an item's MRP history (code or name)")}
    <div class="table-wrapper"><table><thead><tr><th>Change</th><th>Effective from</th><th>Items (old → new MRP)</th><th>Reason</th><th>Status</th><th></th></tr></thead>
    <tbody>${docs.length ? docs.map(d => {
      const ls = lines(d);
      return `<tr><td class="s-72126d">${esc(d.code || d.id)}</td><td>${esc(d.effective_date)}</td>
        <td>${ls.slice(0, 3).map(l => `${esc(l.item_name || l.sku)} <span class="text-muted">${formatMoney(l.old_mrp)} → </span><b>${formatMoney(l.new_mrp)}</b>`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td>${esc(d.reason || '')}</td><td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span></td>
        <td>${d.status === 'Scheduled' ? `<button type="button" class="action-btn action-btn-danger" data-mrp-cancel="${esc(d.id)}">Cancel</button>` : ''}
            ${d.status === 'Applied' && ls.some(l => num(l.on_hand) > 0) ? `<button type="button" class="action-btn" data-mrp-stickers="${esc(d.id)}">Reprint stickers</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="6" class="s-b4fdf8">${mrpList.q ? 'No MRP change names that item.' : 'No MRP changes yet.'}</td></tr>`}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  form.innerHTML = `<div class="s-d9da29">
      <div class="form-group s-116966"><label class="form-label" for="mrp-effective">Effective from</label><input type="date" class="form-input" id="mrp-effective" value="${new Date().toISOString().slice(0, 10)}"></div>
      <div class="form-group s-42a037"><label class="form-label" for="mrp-reason">Reason</label><input class="form-input" id="mrp-reason" placeholder="e.g. Vendor price revision"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Current MRP</th><th class="num">Sale price</th><th class="num">New MRP</th><th></th></tr></thead><tbody data-mrp-lines></tbody></table></div>
    <div class="s-ac2f58">
      <input class="form-input s-3521b3" data-mrp-add-input placeholder="Add by Design, Combination ID or barcode" autocomplete="off">
      <button type="button" class="btn btn-outline" data-mrp-add>Add item</button>
    </div>
    <div class="login-error hidden s-56f435" data-mrp-error></div>
    <div class="s-1b0f49"><button type="button" class="btn btn-primary" data-mrp-save>Save MRP change</button></div>`;

  const section = txnPagedSection({ id: 'mrp-screen', title: 'MRP Change', page: 'New MRP change', count: txn.txnListCount(page), newLabel: 'New MRP Change', form, list });
  container.appendChild(section);
  txnRestorePane('mrp-screen');

  txn.txnWireListBar(list, mrpList, () => renderView('inventory:mrp'));
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
        <td>${esc(l.name || l.sku)}<div class="text-muted s-6cb285">${esc(l.sku)}</div></td>
        <td class="num">${formatMoney(l.mrp)}</td><td class="num">${formatMoney(l.sale_price)}</td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input s-0d0b57" data-mrp-new="${i}" value="${esc(l.new_mrp ?? '')}">
          <div data-mrp-warn="${i}" class="s-3a7bb6"></div></td>
        <td><button type="button" class="action-btn action-btn-danger" data-mrp-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="5" class="s-f692de">Add the items whose MRP changes.</td></tr>';
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
let adjList = null; // 267.3: the list's view, search and page

async function renderAdjustments(container) {
  const [txn, rp] = await Promise.all([import('./txn-screen.js' + Q), import('./reason-picker.js' + Q)]);
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = txn;
  adjList = adjList || txn.txnListState();
  // 267.3: the work queue is paged on the server; each view is a status filter.
  const adjViews = { all: {}, post: { status: 'Approved' }, approval: { status: 'Pending Approval' } };
  const [page, reasons] = await Promise.all([
    txn.txnLoadList('InventoryAdjustment', adjList, { filters: adjViews[adjList.view] || {} }), rp.loadReasons('Inventory Adjustment')]);
  if (page.error) { renderErrorPanel(container, page.error, () => renderView('inventory:adjustments')); return; }
  const docs = page.docs;
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Posted: 'badge-success', 'Pending Approval': 'badge-warning', Approved: 'badge-warning', Rejected: 'badge-danger' };
  const codeOf = Object.fromEntries(docs.map(d => [d.id, d.code || d.id]));
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `${txn.txnListBar(adjList, page, [['all', 'All adjustments'], ['post', 'To post'], ['approval', 'Waiting approval']])}
    <div class="table-wrapper"><table><thead><tr><th>Adjustment</th><th>Store</th><th>Items</th><th class="num">Added</th><th class="num">Taken off</th><th>Status</th><th></th></tr></thead>
    <tbody>${docs.length ? docs.map(d => {
      const ls = lines(d);
      const problem = d.post_error || d.gl_error;
      return `<tr><td class="s-72126d">${esc(d.code || d.id)}</td><td>${esc(d.location)}</td>
        <td>${ls.slice(0, 3).map(l => `${num(l.qty) > 0 ? '+' : ''}${fmtQty(l.qty)} ${esc(l.item_name || l.sku)} <span class="text-muted">(${esc(l.reason || l.reason_code)})</span>`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td class="num">${formatMoney(d.increase_value)}</td><td class="num">${formatMoney(d.decrease_value)}</td>
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${problem ? `<div class="s-262624">${esc(problem)}</div>` : ''}
          ${d.reversal_of ? `<div class="text-muted s-6cb285">Reverses ${esc(codeOf[d.reversal_of] || d.reversal_of)}</div>` : ''}${d.reversed_by ? `<div class="text-muted s-6cb285">Reversed by ${esc(codeOf[d.reversed_by] || d.reversed_by)}</div>` : ''}</td>
        <td>${d.status === 'Approved' ? `<button type="button" class="action-btn" data-adj-post="${esc(d.id)}">Post</button>${/changed since|was \d+ when/.test(problem || '') ? ` <button type="button" class="action-btn" data-adj-post-now="${esc(d.id)}" title="Take the before quantities again from today's stock (store manager)">Post with current stock</button>` : ''}` : ''}
          ${d.status === 'Posted' && !d.reversed_by && !d.reversal_of ? `<button type="button" class="action-btn" data-adj-reverse="${esc(d.id)}">Reverse</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="7" class="s-b4fdf8">${adjList.view !== 'all' || adjList.q ? 'Nothing here in this view.' : 'No stock adjustments yet.'}</td></tr>`}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  form.innerHTML = `<div class="s-d9da29">
      <div class="form-group s-116966"><label class="form-label" for="adj-store">Store</label><input class="form-input s-2acaf3" id="adj-store" value="${esc(sessionStore())}" autocomplete="off"></div>
      <div class="form-group s-116966"><label class="form-label" for="adj-loss">Loss class</label><select class="form-select s-28c0f6" id="adj-loss">
        <option value="">None</option><option>Shrinkage</option><option>Damage</option><option>Expiry</option><option>Theft</option></select></div>
      <div class="form-group s-42a037"><label class="form-label" for="adj-note">Note</label><input class="form-input" id="adj-note" placeholder="e.g. Monthly damage check"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Change (+ / -)</th><th>Reason</th><th>Bin</th><th>Lot</th><th>To Damaged</th><th>Note</th><th></th></tr></thead><tbody data-adj-lines></tbody></table></div>
    <div class="s-ac2f58">
      <input class="form-input s-3521b3" data-adj-add-input placeholder="Add by Design, Combination ID or barcode" autocomplete="off">
      <button type="button" class="btn btn-outline" data-adj-add>Add item</button>
    </div>
    <div class="login-error hidden s-56f435" data-adj-error></div>
    <div data-adj-preview class="s-56f435"></div>
    <div class="s-20fcea"><button type="button" class="btn btn-outline" data-adj-preview-btn>Preview</button><button type="button" class="btn btn-primary" data-adj-save>Save adjustment</button></div>`;

  container.appendChild(txnPagedSection({ id: 'adj-screen', title: 'Stock Adjustment', page: 'New stock adjustment', count: txn.txnListCount(page), newLabel: 'New Stock Adjustment', form, list }));
  txnRestorePane('adj-screen');
  attachLinkTypeahead(form.querySelector('#adj-store'), 'Location', { keepCode: true });
  txn.txnWireListBar(list, adjList, () => renderView('inventory:adjustments'));
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
        <td>${esc(l.name || l.sku)}<div class="text-muted s-6cb285">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" class="form-input s-b54ee3" data-adj-qty="${i}" value="${esc(l.qty)}" placeholder="-2 or 3"></td>
        <td>${rp.reasonSelectHTML(reasons, { value: l.reason_code, attrs: `data-adj-reason="${i}"` })}</td>
        <td><input class="form-input s-db4cf1" data-adj-bin="${i}" value="${esc(l.bin || '')}" placeholder="optional"></td>
        <td><input class="form-input s-db4cf1" data-adj-lot="${i}" value="${esc(l.batch_no || '')}" placeholder="optional"></td>
        <td class="s-91a870"><input type="checkbox" data-adj-damaged="${i}" ${l.damaged ? 'checked' : ''} title="Still yours: moved out of sellable stock (or back with a + change), not written off"></td>
        <td><input class="form-input s-4592c7" data-adj-note="${i}" value="${esc(l.note)}"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-adj-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="8" class="s-f692de">Add the items whose stock is wrong. A negative change takes stock off; a positive one adds it. Tick To Damaged for a damaged unit you still own - it leaves sellable stock without being written off.</td></tr>';
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
    const res = await apiFetch('/api/v1/inventory/adjustments', { method: 'POST', commandKey: 'adjustment.create', body: JSON.stringify({
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
      <div class="text-muted s-fe7b49">GL: ${(pv.gl || []).map(g => g.debit ? `Dr ${esc(g.account)} ${formatMoney(g.debit)}` : `Cr ${esc(g.account)} ${formatMoney(g.credit)}`).join(' · ') || 'nothing is booked (condition moves only)'}</div>`;
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
let miList = null; // 267.3: the list's view, search and page

async function renderMaterials(container) {
  const txn = await import('./txn-screen.js' + Q);
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = txn;
  miList = miList || txn.txnListState();
  const me = localStorage.getItem('erp_username') || '';
  // My requests / Ready to issue / Return remaining (74.2.4's three screens) as
  // one list's views - 267.3: each a filter the server applies before paging.
  const views = { all: {}, mine: { requested_by: me }, issue: { status: 'Approved' }, back: { status: ['Issued', 'Partly Returned'] } };
  const page = await txn.txnLoadList('MaterialIssue', miList, { filters: views[miList.view] || {} });
  if (page.error) { renderErrorPanel(container, page.error, () => renderView('inventory:materials')); return; }
  const docs = page.docs;
  const lines = (d) => { try { return typeof d.items === 'string' ? JSON.parse(d.items) : (d.items || []); } catch (_) { return []; } };
  const badge = { Issued: 'badge-success', Returned: 'badge-success', 'Partly Returned': 'badge-success', Approved: 'badge-warning', 'Pending Approval': 'badge-warning', Rejected: 'badge-danger' };
  // 162.2: a role that may only ask (Cashier) sees no Issue or Return button.
  // The store may issue: edit on MaterialIssue, or create on MaterialReturn (the server's rule).
  const mayReturn = canDo('create', 'MaterialReturn'), mayIssue = mayReturn || canUpdateDoctype('MaterialIssue');
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `${txn.txnListBar(miList, page, [['all', 'All requests'], ['mine', 'My requests'], ['issue', 'Ready to issue'], ['back', 'Return remaining']])}
    <div class="table-wrapper"><table><thead><tr><th>Request</th><th>Store</th><th>Department</th><th>Items</th><th class="num">Value</th><th>Status</th><th></th></tr></thead>
    <tbody>${docs.length ? docs.map(d => {
      const ls = lines(d);
      return `<tr><td class="s-72126d">${esc(d.code || d.id)}</td><td>${esc(d.location)}</td>
        <td>${esc(d.department)}${d.cost_center ? `<div class="text-muted s-6cb285">${esc(d.cost_center)}</div>` : ''}<div class="text-muted s-6cb285">${esc(d.purpose)}</div></td>
        <td>${ls.slice(0, 3).map(l => `${fmtQty(l.qty)} ${esc(l.item_name || l.sku)}${num(l.returned_qty) ? ` <span class="text-muted">(${fmtQty(l.returned_qty)} back)</span>` : ''}`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}</td>
        <td class="num">${formatMoney(d.amount)}</td><td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span></td>
        <td>${mayIssue && d.status === 'Approved' ? `<button type="button" class="action-btn" data-mi-issue="${esc(d.id)}">Issue</button>` : ''}
          ${mayReturn && (d.status === 'Issued' || d.status === 'Partly Returned') ? `<button type="button" class="action-btn" data-mi-return="${esc(d.id)}">Return unused</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="7" class="s-b4fdf8">${miList.view !== 'all' || miList.q ? 'Nothing here in this view.' : 'No material requests yet.'}</td></tr>`}</tbody></table></div>
    <div data-mi-return-pane class="s-d6f2af"></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group s-116966"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" data-css="width:${width}px;" ${extra}></div>`;
  form.innerHTML = `<div class="s-d9da29">
      ${field('mi-store', 'Store', 180, `value="${esc(sessionStore())}"`)}${field('mi-dept', 'Department', 180)}${field('mi-cc', 'Cost centre', 160, 'placeholder="optional"')}
      ${field('mi-emp', 'Employee', 160, 'placeholder="optional"')}${field('mi-project', 'Project', 140, 'placeholder="optional"')}
      <div class="form-group s-42a037"><label class="form-label" for="mi-purpose">Purpose</label><input class="form-input" id="mi-purpose" placeholder="e.g. Packing for the weekend sale"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Qty</th><th></th></tr></thead><tbody data-mi-lines></tbody></table></div>
    <div class="s-ac2f58">
      <input class="form-input s-3521b3" data-mi-add-input placeholder="Add by Design, Combination ID or barcode" autocomplete="off">
      <button type="button" class="btn btn-outline" data-mi-add>Add item</button>
    </div>
    <div class="login-error hidden s-56f435" data-mi-error></div>
    <div class="s-1b0f49"><button type="button" class="btn btn-primary" data-mi-save>Save request</button></div>`;

  container.appendChild(txnPagedSection({ id: 'mi-screen', title: 'Material Issue', page: 'New material request', count: txn.txnListCount(page), newLabel: 'New Material Request', form, list }));
  txnRestorePane('mi-screen');
  attachLinkTypeahead(form.querySelector('#mi-store'), 'Location', { keepCode: true });
  attachLinkTypeahead(form.querySelector('#mi-dept'), 'Department', { keepCode: true });
  attachLinkTypeahead(form.querySelector('#mi-cc'), 'CostCenter', { keepCode: true });

  txn.txnWireListBar(list, miList, () => renderView('inventory:materials'));
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
    pane.innerHTML = `<h3 class="s-df6718">Return unused from ${esc(d.code || d.id)}</h3>
      <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Issued</th><th class="num">Back so far</th><th class="num">Returning now</th><th>Condition</th></tr></thead>
      <tbody>${open.map((l, i) => `<tr><td>${esc(l.item_name || l.sku)}</td><td class="num">${fmtQty(l.issued_qty)}</td><td class="num">${fmtQty(l.returned_qty)}</td>
        <td class="num"><input type="number" step="1" min="0" class="form-input s-96d2a5" data-mr-qty="${i}" placeholder="0"></td>
        <td><select class="form-select s-c6fffd" data-mr-cond="${i}"><option>Good</option><option>Damaged</option></select></td></tr>`).join('')}</tbody></table></div>
      <div class="login-error hidden s-d2c171" data-mr-error></div>
      <div class="s-d2c171"><button type="button" class="btn btn-primary" data-mr-save>Post return</button></div>`;
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
        <td>${esc(l.name || l.sku)}<div class="text-muted s-6cb285">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" min="1" class="form-input s-b54ee3" data-mi-qty="${i}" value="${esc(l.qty)}"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-mi-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="3" class="s-f692de">Add the store items the department needs - stock items only; what is consumed by auto backflush is not asked for here.</td></tr>';
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
let dcList = null; // 267.3: the list's view, search and page
const DC_PURPOSES = ['Job work', 'Repair', 'Exhibition', 'Sample', 'Returnable - other', 'Non-returnable', 'Other'];

async function renderChallans(container) {
  const txn = await import('./txn-screen.js' + Q);
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = txn;
  dcList = dcList || txn.txnListState();
  const parse = (v) => { try { return typeof v === 'string' ? JSON.parse(v || '[]') : (v || []); } catch (_) { return []; } };
  const open = (d) => parse(d.challan_balance).reduce((n, b) => n + (b.issued - b.returned - b.damaged - b.rejected - b.lost - b.kept), 0);
  const today = new Date().toISOString().slice(0, 10);
  const soon = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10);
  const due = (d) => parse(d.items).map(l => l.expected_return).filter(Boolean).sort()[0] || '';
  const out = (d) => d.status === 'Dispatched' || d.status === 'Partly Returned';
  // 267.3: All and E-way bill are server filters, paged. Open, due-soon and
  // exceptions depend on the lines' balance and dates, so they read every
  // challan still out and narrow it here.
  const served = { all: {}, ewb: { ewb_required: 'Yes' } };
  const narrowed = { open: d => out(d) && open(d) > 0, soon: d => out(d) && open(d) > 0 && due(d) && due(d) <= soon,
    exceptions: d => out(d) && ((due(d) && due(d) < today) || parse(d.challan_balance).some(b => b.damaged || b.rejected || b.lost)) };
  const whole = !!narrowed[dcList.view];
  const page = await txn.txnLoadList('DeliveryChallan', dcList,
    { filters: whole ? { status: ['Dispatched', 'Partly Returned'] } : (served[dcList.view] || {}), whole });
  if (page.error) { renderErrorPanel(container, page.error, () => renderView('inventory:challans')); return; }
  const docs = page.docs;
  const shown = whole ? docs.filter(narrowed[dcList.view]) : docs;
  const badge = { Dispatched: 'badge-warning', 'Partly Returned': 'badge-warning', Returned: 'badge-success', Closed: 'badge-secondary', Approved: 'badge-warning', 'Pending Approval': 'badge-warning', Cancelled: 'badge-danger', Rejected: 'badge-danger' };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `${txn.txnListBar(dcList, page, [['all', 'All challans'], ['open', 'Open returns'], ['soon', 'Due back in 7 days'], ['exceptions', 'Exceptions'], ['ewb', 'E-way bill needed']])}
    <div class="table-wrapper"><table><thead><tr><th>Challan</th><th>Purpose / party</th><th>Store</th><th>Items</th><th class="num">Value</th><th>Expected back</th><th>Status</th><th></th></tr></thead>
    <tbody>${shown.length ? shown.map(d => {
      const ls = parse(d.items);
      const o = open(d);
      const late = out(d) && o > 0 && due(d) && due(d) < today;
      return `<tr><td class="s-72126d">${esc(d.code || d.id)}</td>
        <td>${esc(d.purpose)}${d.returnable === false ? ' <span class="text-muted">(non-returnable)</span>' : ''}<div class="text-muted s-6cb285">${esc(d.party_name)}${d.party_gstin ? ' · ' + esc(d.party_gstin) : ''}</div></td>
        <td>${esc(d.location)}</td>
        <td>${ls.slice(0, 3).map(l => `${fmtQty(l.qty)} ${esc(l.item_name || l.sku)}`).join('<br>')}${ls.length > 3 ? `<div class="text-muted">+${ls.length - 3} more</div>` : ''}${out(d) && d.returnable !== false ? `<div class="text-muted s-6cb285">${fmtQty(o)} still out</div>` : ''}</td>
        <td class="num">${formatMoney(d.amount)}</td>
        <td>${esc(due(d))}${late ? ' <span class="badge badge-danger">overdue</span>' : ''}</td>
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${d.ewb_required === 'Yes' ? ' <span class="badge badge-warning" title="E-way bill needed">EWB</span>' : ''}</td>
        <td class="s-86abda">${d.status === 'Approved' ? `<button type="button" class="action-btn" data-dc-act="dispatch" data-id="${esc(d.id)}">Dispatch</button> ` : ''}
          ${out(d) && d.returnable !== false && o > 0 ? `<button type="button" class="action-btn" data-dc-act="return" data-id="${esc(d.id)}">Receive return</button> <button type="button" class="action-btn" data-dc-act="writeoff" data-id="${esc(d.id)}">Write off</button> ` : ''}
          ${['Draft', 'Pending Approval', 'Approved'].includes(d.status) ? `<button type="button" class="action-btn action-btn-danger" data-dc-act="cancel" data-id="${esc(d.id)}">Cancel</button> ` : ''}
          ${d.status !== 'Draft' && d.status !== 'Cancelled' ? `<button type="button" class="action-btn" data-dc-act="print" data-id="${esc(d.id)}">Print</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="8" class="s-b4fdf8">${dcList.view !== 'all' || dcList.q ? 'Nothing here in this view.' : 'No delivery challans yet.'}</td></tr>`}</tbody></table></div>
    <div data-dc-pane class="s-d6f2af"></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group s-116966"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" data-css="width:${width}px;" ${extra}></div>`;
  form.innerHTML = `<div class="s-d9da29">
      ${field('dc-store', 'From store', 160, `value="${esc(sessionStore())}"`)}
      <div class="form-group s-116966"><label class="form-label" for="dc-purpose">Purpose</label><select class="form-select s-d52ddd" id="dc-purpose">${DC_PURPOSES.map(p => `<option>${p}</option>`).join('')}</select></div>
      <div class="form-group s-116966"><label class="form-label" for="dc-returnable">Comes back</label><select class="form-select s-607468" id="dc-returnable"><option value="yes">Yes</option><option value="no">No</option></select></div>
      ${field('dc-expected', 'Expected back', 150, 'type="date"')}
      ${field('dc-ref', 'Reference', 140, 'placeholder="optional"')}
    </div>
    <div class="s-d9da29">
      <div class="form-group s-116966"><label class="form-label" for="dc-ptype">Party type</label><select class="form-select s-830eb9" id="dc-ptype"><option>Job worker</option><option>Vendor</option><option>Customer</option><option>Other</option></select></div>
      ${field('dc-party', 'Consignee', 200)}${field('dc-gstin', 'GSTIN', 170, 'placeholder="if registered"')}${field('dc-state', 'State', 130)}
      <div class="form-group s-42a037"><label class="form-label" for="dc-address">Address</label><input class="form-input" id="dc-address"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Qty</th><th>Lot</th><th>Serial numbers</th><th class="num">Value</th><th></th></tr></thead><tbody data-dc-lines></tbody></table></div>
    <div class="s-ac2f58">
      <input class="form-input s-3521b3" data-dc-add-input placeholder="Add by Design, Combination ID or barcode" autocomplete="off">
      <button type="button" class="btn btn-outline" data-dc-add>Add item</button>
    </div>
    <div class="login-error hidden s-56f435" data-dc-error></div>
    <div class="s-1b0f49"><button type="button" class="btn btn-primary" data-dc-save>Save challan</button></div>`;

  container.appendChild(txnPagedSection({ id: 'dc-screen', title: 'Delivery Challan', page: 'New delivery challan', count: txn.txnListCount(page, shown.length), newLabel: 'New Delivery Challan', form, list }));
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
  txn.txnWireListBar(list, dcList, () => renderView('inventory:challans'));
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
      pane.innerHTML = `<h3 class="s-df6718">Dispatch ${esc(d.code || d.id)}</h3>
        <div class="s-aad644">
          <div class="form-group s-116966"><label class="form-label" for="dcd-mode">Transport</label><select class="form-select s-465bfe" id="dcd-mode"><option>Road</option><option>Rail</option><option>Air</option><option>Ship</option><option>By hand</option></select></div>
          ${field('dcd-vehicle', 'Vehicle no', 130)}${field('dcd-lr', 'LR / AWB no', 130)}${field('dcd-transporter', 'Transporter', 160)}${field('dcd-tid', 'Transporter ID', 150)}${field('dcd-pk', 'Packages', 90, 'type="number" min="0" step="1"')}
          ${field('dcd-date', 'Dispatch date', 150, `type="date" value="${today}"`)}
          <button type="button" class="btn btn-primary" data-dcd-go>Dispatch</button></div>
        ${d.ewb_required === 'Yes' ? '<div class="txn-hint s-8a77e5">This challan needs an e-way bill before the goods move.</div>' : ''}`;
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
      pane.innerHTML = `<h3 class="s-df6718">What came back on ${esc(d.code || d.id)}</h3>
        <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Still out</th><th class="num">Good</th><th class="num">Damaged</th><th class="num">Rejected</th><th>Serial numbers back</th><th>Reason (damaged / rejected)</th></tr></thead>
        <tbody>${bal.map((x, i) => `<tr><td>${esc(x.sku)}${x.batch_no ? ` <span class="text-muted">lot ${esc(x.batch_no)}</span>` : ''}</td><td class="num">${fmtQty(left(x))}</td>
          ${['good', 'damaged', 'rejected'].map(c => `<td class="num"><input type="number" min="0" step="1" class="form-input s-21b28d" data-dcr="${c}" data-i="${i}" placeholder="0"></td>`).join('')}
          <td>${outSerials(x).length ? `<input class="form-input s-94253f" data-dcr="serials" data-i="${i}" placeholder="${esc(outSerials(x).slice(0, 3).join(', '))}">` : '<span class="text-muted">-</span>'}</td>
          <td><input class="form-input s-4592c7" data-dcr="reason" data-i="${i}"></td></tr>`).join('')}</tbody></table></div>
        <div class="txn-hint s-fe7b49">Serials: one condition per save.</div>
        <div class="s-d2c171"><button type="button" class="btn btn-primary" data-dcr-go>Receive</button></div>`;
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
      pane.innerHTML = `<h3 class="s-df6718">Write off from ${esc(d.code || d.id)}</h3>
        <div class="s-aad644">
          <div class="form-group s-116966"><label class="form-label" for="dcw-line">Item</label><select class="form-select s-0fac91" id="dcw-line">${bal.map((x, i) => `<option value="${i}">${esc(x.sku)}${x.batch_no ? ' / ' + esc(x.batch_no) : ''} (${left(x)} out)</option>`).join('')}</select></div>
          ${field('dcw-qty', 'Qty', 90, 'type="number" min="1" step="1"')}
          <div class="form-group s-116966"><label class="form-label" for="dcw-kind">What happened</label><select class="form-select s-7c07cd" id="dcw-kind"><option>Lost</option><option>Kept by party</option></select></div>
          ${field('dcw-serials', 'Serial numbers', 170, 'placeholder="if it has them"')}
          <div class="form-group s-780092"><label class="form-label" for="dcw-reason">Reason</label><input class="form-input" id="dcw-reason"></div>
          <button type="button" class="btn btn-primary" data-dcw-go>Send for approval</button></div>`;
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
        <td>${esc(l.name || l.sku)}<div class="text-muted s-6cb285">${esc(l.sku)}</div></td>
        <td class="num"><input type="number" step="1" min="1" class="form-input s-d29533" data-dcl="qty" data-i="${i}" value="${esc(l.qty)}"></td>
        <td><input class="form-input s-db4cf1" data-dcl="batch_no" data-i="${i}" value="${esc(l.batch_no || '')}" placeholder="optional"></td>
        <td><input class="form-input s-94253f" data-dcl="serials" data-i="${i}" value="${esc(l.serials || '')}" placeholder="optional, comma separated"></td>
        <td class="num"><input type="number" step="0.01" min="0" class="form-input s-b54ee3" data-dcl="declared_value" data-i="${i}" value="${esc(l.declared_value || '')}" placeholder="at cost"></td>
        <td><button type="button" class="action-btn action-btn-danger" data-dc-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="6" class="s-f692de">Add the items going out. Value is the declared value for the challan; blank uses their cost.</td></tr>';
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
