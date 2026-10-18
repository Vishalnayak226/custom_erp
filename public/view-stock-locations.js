// Stage 85.B (user, 2026-10-18): "location storewise ... Saleable, Non
// saleable. I should have that control. It is not wms depenednt ... It is
// erp's part. WMS has to adopt and know that from erp."
//
// Two screens over engines/stock_locations.go:
//   Stock Locations (setup)  - the locations, which sell, which each store
//                              uses, and each transaction's from / to.
//   Move Stock               - move an item between two locations of a store
//                              (Main -> Damage, Inward -> Main, ...), list
//                              first and New as its own page (Stage 69).

const { txnTabs, txnRestorePane, txnPagedSection, txnDone, sessionStore } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const esc = (s) => window.escapeHTMLText(String(s ?? ''));

let setup = null;

async function loadSetup() {
  const res = await apiFetch('/api/v1/stock-locations');
  if (!res || !res.ok) return null;
  return res.json();
}

function bucketNote(l) {
  if (l.bucket === 'available') return 'Always sells';
  if (l.bucket === 'custom') return 'Your location - held away from sale';
  if (l.sellable_editable) return 'You decide';
  return 'Never sells';
}

// --- Stock Locations (setup) ----------------------------------------------

export async function renderStockLocationsView(container) {
  setup = await loadSetup();
  if (!setup) { renderErrorPanel(container, 'Failed to load stock locations.', () => renderView('stock-locations')); return; }
  const panel = document.createElement('div');
  panel.className = 'txn-screen';
  panel.id = 'stockloc-screen';
  panel.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">Stock Locations</h1>
      ${txnTabs('txnShowPane', 'stockloc-screen:locations', [['stockloc-screen:locations', 'Locations'], ['stockloc-screen:stores', 'Stores'], ['stockloc-screen:txns', 'Transactions']])}
      <div class="txn-actions txn-main-only"><button type="button" class="btn btn-primary" id="stockloc-new-btn">+ New Location</button></div>
    </div>
    <div class="txn-pane" data-pane="locations"><div class="txn-grid" id="stockloc-locations"></div></div>
    <div class="txn-pane hidden" data-pane="stores"><div class="txn-grid" id="stockloc-stores"></div></div>
    <div class="txn-pane hidden" data-pane="txns"><div class="txn-grid" id="stockloc-txns"></div></div>`;
  container.appendChild(panel);
  drawLocations();
  drawStores();
  drawTxns();
  panel.querySelector('#stockloc-new-btn').addEventListener('click', () => editLocation(null));
  txnRestorePane('stockloc-screen');
}

function drawLocations() {
  const host = document.getElementById('stockloc-locations');
  if (!host) return;
  host.innerHTML = `<table>
    <thead><tr><th>Location</th><th>Code</th><th>Sells?</th><th>Applies to</th><th>New stores</th><th>WMS bin condition</th><th>Status</th><th></th></tr></thead>
    <tbody>${setup.locations.map(l => `<tr>
      <td><b>${esc(l.name)}</b>${l.description ? `<div class="txn-hint">${esc(l.description)}</div>` : ''}</td>
      <td>${esc(l.code)}</td>
      <td><span class="badge ${l.sellable ? 'badge-success' : 'badge-secondary'}">${l.sellable ? 'Sellable' : 'Not sellable'}</span><div class="txn-hint">${esc(bucketNote(l))}</div></td>
      <td>${esc(l.applies_to)}</td>
      <td>${l.default_for_new ? 'Yes' : 'No'}</td>
      <td>${esc(l.wms_condition || '-')}</td>
      <td>${l.active ? 'Active' : 'Off'}</td>
      <td><button type="button" class="action-btn" data-edit-loc="${esc(l.code)}">Edit</button></td>
    </tr>`).join('')}</tbody></table>`;
  host.querySelectorAll('[data-edit-loc]').forEach(b => b.addEventListener('click', () => editLocation(setup.locations.find(l => l.code === b.dataset.editLoc))));
}

function locationDialog(title, body, onSave) {
  document.getElementById('stockloc-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'stockloc-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 560px;" role="dialog" aria-modal="true" aria-labelledby="stockloc-dialog-title">
      <div class="modal-header"><h3 class="modal-title" id="stockloc-dialog-title">${esc(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-sl-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">${body}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-sl-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-sl-save>Save</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-sl-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const save = overlay.querySelector('[data-sl-save]');
  save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => { if (await onSave(overlay)) close(); }));
  overlay.querySelector('input:not([readonly]), select')?.focus();
}

function editLocation(l) {
  const isNew = !l;
  l = l || { code: '', name: '', bucket: 'custom', sellable: false, applies_to: 'Both', default_for_new: true, active: true, display_order: 100, description: '' };
  const sellableField = l.sellable_editable
    ? `<label class="form-label" for="sl-sellable">Sells?</label>
       <select id="sl-sellable" class="form-input"><option value="1" ${l.sellable ? 'selected' : ''}>Sellable - the till and online channels may sell it</option><option value="0" ${l.sellable ? '' : 'selected'}>Not sellable</option></select>`
    : `<div class="form-label">Sells?</div><div>${l.sellable ? 'Sellable' : 'Not sellable'} <span class="txn-hint">(${esc(bucketNote(l))})</span></div>`;
  locationDialog(isNew ? 'New stock location' : `Stock location - ${l.name}`, `
    <div class="form-group"><label class="form-label" for="sl-code">Code</label>
      <input id="sl-code" class="form-input" value="${esc(l.code)}" ${isNew ? 'placeholder="e.g. ALTERATION" maxlength="30"' : 'readonly'} autocomplete="off"></div>
    <div class="form-group"><label class="form-label" for="sl-name">Name</label>
      <input id="sl-name" class="form-input" value="${esc(l.name)}" maxlength="80" autocomplete="off"></div>
    <div class="form-group">${sellableField}</div>
    <div class="form-group"><label class="form-label" for="sl-applies">Applies to</label>
      <select id="sl-applies" class="form-input">${['Both', 'Store', 'Warehouse'].map(v => `<option ${v === l.applies_to ? 'selected' : ''}>${v}</option>`).join('')}</select></div>
    <label style="display:flex; gap:8px; align-items:center;"><input type="checkbox" id="sl-default" ${l.default_for_new ? 'checked' : ''}> Give this location to every new store</label>
    ${l.bucket === 'available' ? '' : `<label style="display:flex; gap:8px; align-items:center;"><input type="checkbox" id="sl-active" ${l.active ? 'checked' : ''}> In use</label>`}
    <div class="form-group"><label class="form-label" for="sl-desc">What it is for</label>
      <input id="sl-desc" class="form-input" value="${esc(l.description)}" autocomplete="off"></div>
    ${isNew ? '<p class="txn-hint">Your own locations hold stock away from sale (Alteration, Display only, Reserved...). Main is what sells; QC Hold and Damage can be made sellable.</p>' : ''}`,
  async (o) => {
    const body = {
      code: o.querySelector('#sl-code').value.trim(), name: o.querySelector('#sl-name').value.trim(),
      sellable: o.querySelector('#sl-sellable') ? o.querySelector('#sl-sellable').value === '1' : l.sellable,
      applies_to: o.querySelector('#sl-applies').value, default_for_new: o.querySelector('#sl-default').checked,
      active: o.querySelector('#sl-active') ? o.querySelector('#sl-active').checked : true,
      display_order: l.display_order, description: o.querySelector('#sl-desc').value.trim(),
    };
    const res = await apiFetch('/api/v1/stock-locations', { method: 'POST', body: JSON.stringify(body) });
    if (!res) return false;
    if (!res.ok) { await showApiError(res, 'The location was not saved.'); return false; }
    showToast('Stock location saved.', { variant: 'success' });
    renderView('stock-locations');
    return true;
  });
}

function drawStores() {
  const host = document.getElementById('stockloc-stores');
  if (!host) return;
  const locs = setup.locations.filter(l => l.active);
  if (!setup.stores.length) { host.innerHTML = '<div class="empty-state-hint" style="padding: 24px;">No stores yet. Add them in Setup &rarr; Location.</div>'; return; }
  host.innerHTML = `<table>
    <thead><tr><th>Store</th>${locs.map(l => `<th title="${esc(l.sellable ? 'Sellable' : 'Not sellable')}">${esc(l.name)}</th>`).join('')}<th></th></tr></thead>
    <tbody>${setup.stores.map(s => {
      const uses = new Set(setup.uses[s.code] || []);
      const own = !!setup.own_list[s.code];
      return `<tr data-store="${esc(s.code)}">
        <td><b>${esc(s.name)}</b><div class="txn-hint">${esc(s.type || '')}${own ? '' : ' · using the defaults'}</div></td>
        ${locs.map(l => `<td><input type="checkbox" aria-label="${esc(s.name)}: ${esc(l.name)}" data-loc="${esc(l.code)}" ${uses.has(l.code) ? 'checked' : ''} ${l.bucket === 'available' ? 'disabled' : ''}></td>`).join('')}
        <td><button type="button" class="action-btn" data-save-store>Save</button></td>
      </tr>`;
    }).join('')}</tbody></table>
    <p class="txn-hint" style="padding: 8px 12px;">Main is always on. A store that has never been saved here uses every location marked for new stores. A location still holding stock at a store cannot be taken away from it.</p>`;
  host.querySelectorAll('[data-save-store]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
    const tr = btn.closest('tr');
    const locations = [...tr.querySelectorAll('input[data-loc]:checked')].map(c => c.dataset.loc);
    const res = await apiFetch('/api/v1/stock-locations/stores', { method: 'POST', body: JSON.stringify({ store: tr.dataset.store, locations }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The store was not saved.'); return; }
    showToast('Store locations saved.', { variant: 'success' });
  })));
}

function locationOptions(selected, filter) {
  return setup.locations.filter(l => l.active && filter(l))
    .map(l => `<option value="${esc(l.code)}" ${l.code === selected ? 'selected' : ''}>${esc(l.name)}</option>`).join('');
}

function drawTxns() {
  const host = document.getElementById('stockloc-txns');
  if (!host) return;
  const name = (code) => (setup.locations.find(l => l.code === code) || {}).name || code || '-';
  host.innerHTML = `<table>
    <thead><tr><th>Transaction</th><th>From</th><th>To</th><th>How it works</th><th></th></tr></thead>
    <tbody>${setup.defaults.map(d => `<tr data-txn="${esc(d.txn_type)}">
      <td><b>${esc(d.txn_type)}</b></td>
      <td>${d.from_editable ? `<select class="form-input" data-from>${locationOptions(d.from_location, l => l.movable)}</select>` : esc(name(d.from_location))}</td>
      <td>${d.to_editable ? `<select class="form-input" data-to>${locationOptions(d.to_location, l => l.movable)}</select>` : esc(name(d.to_location))}</td>
      <td class="txn-hint">${esc(d.note)}</td>
      <td>${d.from_editable || d.to_editable ? '<button type="button" class="action-btn" data-save-txn>Save</button>' : ''}</td>
    </tr>`).join('')}</tbody></table>`;
  host.querySelectorAll('[data-save-txn]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
    const tr = btn.closest('tr');
    const body = { txn_type: tr.dataset.txn, from_location: tr.querySelector('[data-from]')?.value || '', to_location: tr.querySelector('[data-to]')?.value || '' };
    const res = await apiFetch('/api/v1/stock-locations/defaults', { method: 'POST', body: JSON.stringify(body) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The default was not saved.'); return; }
    showToast(`${body.txn_type} default saved.`, { variant: 'success' });
  })));
}

// --- Move Stock -------------------------------------------------------------

export async function renderStockMovesView(container) {
  const [movesRes, data, working] = await Promise.all([
    apiFetch('/api/v1/doc/StockMove?limit=100'),
    loadSetup(),
    loadViewModule('/view-store.js?v=1').then(m => m.ensureWorkingStore()).catch(() => ({ code: sessionStore() })),
  ]);
  const store = working.code || sessionStore();
  if (!movesRes || !data) { renderErrorPanel(container, 'Failed to load stock moves.', () => renderView('stock-moves')); return; }
  setup = data;
  const moves = movesRes.ok ? await movesRes.json().catch(() => []) : [];
  const name = (code) => (setup.locations.find(l => l.code === code) || {}).name || code;
  const storeName = (code) => (setup.stores.find(s => s.code === code) || {}).name || code;
  const uses = setup.uses[store] || ['MAIN'];
  const movable = setup.locations.filter(l => l.active && l.movable && uses.includes(l.code));
  const moveDefault = setup.defaults.find(d => d.txn_type === 'Stock Move') || {};
  // From and To never start on the same location.
  const fromCode = movable.some(l => l.code === moveDefault.from_location) ? moveDefault.from_location : (movable[0] || {}).code;
  const toCode = movable.some(l => l.code === moveDefault.to_location && l.code !== fromCode) ? moveDefault.to_location : (movable.find(l => l.code !== fromCode) || {}).code;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table>
    <thead><tr><th>Move</th><th>Store</th><th>Item</th><th>Qty</th><th>From</th><th>To</th><th>Reason</th><th>By</th></tr></thead>
    <tbody>${(Array.isArray(moves) ? moves : []).map(m => `<tr>
      <td>${esc(m.id)}</td><td>${esc(storeName(m.location))}</td><td>${esc(m.item_name || m.sku)}<div class="txn-hint">${esc(m.sku)}</div></td>
      <td>${esc(m.qty)}</td><td>${esc(name(m.from_location))}</td><td>${esc(name(m.to_location))}</td><td>${esc(m.reason || '')}</td><td>${esc(m.moved_by || '')}</td>
    </tr>`).join('') || '<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No stock moves yet. Use <b>+ New Move</b> to move stock between locations.</td></tr>'}</tbody></table>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <p class="txn-hint" style="margin: 0 0 12px;">At <b>${esc(storeName(store))}</b> - switch store at the top of the screen.</p>
    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; align-items: end;">
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="sm-item-display">Item</label>
        <input type="text" id="sm-item-display" class="form-input" placeholder="Scan or search the item" autocomplete="off">
        <input type="hidden" id="sm-item"></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="sm-qty">Quantity</label>
        <input type="number" id="sm-qty" class="form-input" min="1" step="1" value="1"></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="sm-from">From</label>
        <select id="sm-from" class="form-input">${movable.map(l => `<option value="${esc(l.code)}" ${l.code === fromCode ? 'selected' : ''}>${esc(l.name)}</option>`).join('')}</select></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="sm-to">To</label>
        <select id="sm-to" class="form-input">${movable.map(l => `<option value="${esc(l.code)}" ${l.code === toCode ? 'selected' : ''}>${esc(l.name)}</option>`).join('')}</select></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="sm-reason">Reason</label>
        <input type="text" id="sm-reason" class="form-input" placeholder="e.g. found torn on the shelf" autocomplete="off"></div>
    </div>
    <div id="sm-balance" class="txn-hint" style="margin-top: 12px;"></div>
    <div style="margin-top: 16px;"><button type="button" class="btn btn-primary" id="sm-save">Move Stock</button></div>`;

  container.appendChild(txnPagedSection({ id: 'stock-moves-screen', title: 'Move Stock', page: 'New move', count: Array.isArray(moves) ? moves.length : 0, newLabel: 'New Move', form, list }));
  txnRestorePane('stock-moves-screen');

  attachCodeNamePicker(document.getElementById('sm-item-display'), document.getElementById('sm-item'), 'Item');
  const showBalance = async () => {
    const sku = document.getElementById('sm-item').value.trim();
    const el = document.getElementById('sm-balance');
    if (!sku) { el.textContent = ''; return; }
    const res = await apiFetch(`/api/v1/stock-locations/balance?sku=${encodeURIComponent(sku)}`);
    if (!res || !res.ok) { el.textContent = ''; return; }
    const b = await res.json();
    el.textContent = (b.locations || []).map(l => `${l.name} ${l.qty}${l.sellable ? ' (sells)' : ''}`).join(' · ') || 'No stock of this item here.';
  };
  document.getElementById('sm-item').addEventListener('change', showBalance);
  document.getElementById('sm-save').addEventListener('click', () => guardAgainstDoubleSubmit(document.getElementById('sm-save'), 'Moving...', async () => {
    const body = {
      store, sku: document.getElementById('sm-item').value.trim(), qty: parseInt(document.getElementById('sm-qty').value, 10) || 0,
      from_location: document.getElementById('sm-from').value, to_location: document.getElementById('sm-to').value,
      reason: document.getElementById('sm-reason').value.trim(),
    };
    const res = await apiFetch('/api/v1/stock-locations/move', { method: 'POST', body: JSON.stringify(body) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The stock was not moved.'); return; }
    const out = await res.json();
    showToast(`${out.id}: ${body.qty} moved from ${name(body.from_location)} to ${name(body.to_location)}.`, { variant: 'success' });
    txnDone('stock-moves-screen');
    renderView('stock-moves');
  }));
}
