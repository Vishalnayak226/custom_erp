// Stage 72.6: Consumption Entry (the user's item & material master blueprint,
// section 9.6). Lazy module, opened from the Inventory screen - nothing here
// loads at start-up.
//
// Two kinds of entry, one form:
//   * Periodic count - butter paper, tape, ribbon. Type the counted closing
//     quantity; the ERP posts book - counted as consumed.
//   * Manual issue - type the quantity the store used.
// The server checks the book again and posts in one transaction; a valuated
// item moves its cost to its profile's expense account, a non-valuated one
// moves quantity only.

let ceLines = [];

function ceEsc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function ceMode() {
  return (document.getElementById('ce-mode') || {}).value || 'Periodic count';
}

function ceConsumed(line) {
  const q = Number(line.qty);
  if (line.qty === '' || isNaN(q)) return '';
  return ceMode() === 'Periodic count' ? line.book - q : q;
}

function ceRender() {
  const body = document.getElementById('ce-lines');
  const head = document.getElementById('ce-qty-head');
  if (!body) return;
  if (head) head.textContent = ceMode() === 'Periodic count' ? 'Counted closing qty' : 'Qty used';
  if (!ceLines.length) {
    body.innerHTML = '<tr><td colspan="5" style="color:var(--text-muted);">Add the items counted or used.</td></tr>';
    return;
  }
  body.innerHTML = ceLines.map((l, i) => {
    const c = ceConsumed(l);
    const bad = c !== '' && (c < 0 || c > l.book);
    return `<tr>
      <td><div style="font-weight:600;">${ceEsc(l.name || l.sku)}</div><div style="font-size:11.5px; color:var(--text-muted);">${ceEsc(l.sku)}${l.valuated ? '' : ' &middot; non-valuated'}</div></td>
      <td>${l.book}</td>
      <td><input type="number" min="0" step="1" class="form-input" style="width:110px;" data-ce-qty="${i}" value="${ceEsc(l.qty)}"></td>
      <td style="${bad ? 'color:var(--danger-color, #b3261e); font-weight:600;' : ''}">${c === '' ? '' : c}</td>
      <td><button class="action-btn action-btn-danger" type="button" data-ce-remove="${i}">Remove</button></td>
    </tr>`;
  }).join('');
}

async function ceAddItem() {
  const sku = (document.getElementById('ce-item') || {}).value.trim();
  const location = (document.getElementById('ce-location') || {}).value.trim();
  const errorEl = document.getElementById('ce-error');
  errorEl.classList.add('hidden');
  if (!location) { errorEl.textContent = 'Choose the store first.'; errorEl.classList.remove('hidden'); return; }
  if (!sku) return;
  const res = await apiFetch(`/api/v1/inventory/consumption/book?sku=${encodeURIComponent(sku)}&location=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'That item cannot be consumed here.'); errorEl.classList.remove('hidden'); return; }
  const info = await res.json();
  if (ceLines.some(l => l.sku === info.sku)) { errorEl.textContent = `${info.name || info.sku} is already on the entry.`; errorEl.classList.remove('hidden'); return; }
  ceLines.push({ sku: info.sku, name: info.name, book: Number(info.book_qty) || 0, valuated: !!info.valuated, qty: '' });
  document.getElementById('ce-item').value = '';
  const display = document.getElementById('ce-item-display');
  if (display) { display.value = ''; display.focus(); }
  ceRender();
}

async function cePost() {
  const errorEl = document.getElementById('ce-error');
  errorEl.classList.add('hidden');
  const location = document.getElementById('ce-location').value.trim();
  const mode = ceMode();
  if (!location || !ceLines.length) { errorEl.textContent = 'Choose the store and add at least one item.'; errorEl.classList.remove('hidden'); return; }
  const missing = ceLines.find(l => l.qty === '' || isNaN(Number(l.qty)));
  if (missing) { errorEl.textContent = `Enter the quantity for ${missing.name || missing.sku}.`; errorEl.classList.remove('hidden'); return; }
  const summary = ceLines.map(l => `${l.name || l.sku}: ${ceConsumed(l)}`).join('\n');
  if (!(await showCustomConfirm(`Post this consumption at ${location}?\n\n${summary}`, 'Consumption Entry'))) return;
  const body = {
    location, mode,
    count_reference: document.getElementById('ce-ref').value.trim(),
    reason: document.getElementById('ce-reason').value.trim(),
    idempotency_key: document.getElementById('consumption-entry-panel').dataset.key,
    lines: ceLines.map(l => mode === 'Periodic count'
      ? { sku: l.sku, counted_qty: parseInt(l.qty, 10) }
      : { sku: l.sku, issued_qty: parseInt(l.qty, 10) })
  };
  const res = await apiFetch('/api/v1/inventory/consumption', { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'The entry was not posted.'); errorEl.classList.remove('hidden'); return; }
  const r = await res.json();
  showToast(`${r.number || r.id} posted${r.cost_total ? ` - cost ${Number(r.cost_total).toFixed(2)} expensed` : ''}.`, { variant: 'success', title: 'Consumption Entry' });
  ceLines = [];
  document.getElementById('consumption-entry-panel').dataset.key = `ce-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  renderView('inventory');
}

export function openConsumptionEntry(host) {
  let panel = document.getElementById('consumption-entry-panel');
  if (panel) { panel.scrollIntoView({ block: 'start' }); return; }
  ceLines = [];
  panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.id = 'consumption-entry-panel';
  panel.dataset.key = `ce-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  panel.innerHTML = `
    <div style="padding:16px; display:grid; gap:12px;">
      <div style="display:flex; align-items:center; gap:8px;">
        <h2 style="margin:0; font-size:16px;">Consumption Entry</h2>
        <button class="btn btn-outline btn-sm" type="button" id="ce-close" style="margin-left:auto;">Close</button>
      </div>
      <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap:10px;">
        <div class="form-group"><label class="form-label" for="ce-location-display">Store</label>
          <input type="text" id="ce-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
          <input type="hidden" id="ce-location"></div>
        <div class="form-group"><label class="form-label" for="ce-mode">Type</label>
          <select id="ce-mode" class="form-input"><option>Periodic count</option><option>Manual issue</option></select></div>
        <div class="form-group"><label class="form-label" for="ce-ref">Count reference</label>
          <input type="text" id="ce-ref" class="form-input" placeholder="e.g. Week 42 count"></div>
        <div class="form-group"><label class="form-label" for="ce-reason">Reason</label>
          <input type="text" id="ce-reason" class="form-input" placeholder="Optional"></div>
      </div>
      <div style="display:flex; gap:8px; align-items:flex-end;">
        <div class="form-group" style="flex:1;"><label class="form-label" for="ce-item-display">Item</label>
          <input type="text" id="ce-item-display" class="form-input" placeholder="Search butter paper, tape, boxes..." autocomplete="off">
          <input type="hidden" id="ce-item"></div>
        <button class="btn btn-outline" type="button" id="ce-add">Add</button>
      </div>
      <div id="ce-error" class="login-error hidden"></div>
      <div class="table-wrapper"><table>
        <thead><tr><th>Item</th><th>Book stock</th><th id="ce-qty-head">Counted closing qty</th><th>Consumed</th><th></th></tr></thead>
        <tbody id="ce-lines"></tbody>
      </table></div>
      <div style="display:flex; justify-content:flex-end;"><button class="btn btn-primary" type="button" id="ce-post">Post Consumption</button></div>
    </div>`;
  // Next to the page header that was clicked - the live page, not a wrapper
  // the view may have replaced since it rendered.
  const header = host.querySelector('.page-header');
  host.insertBefore(panel, header ? header.nextSibling : host.firstChild);
  const q = id => panel.querySelector('#' + id);
  attachCodeNamePicker(q('ce-location-display'), q('ce-location'), 'Location');
  attachCodeNamePicker(q('ce-item-display'), q('ce-item'), 'Item');
  q('ce-item').addEventListener('change', ceAddItem);
  q('ce-add').addEventListener('click', ceAddItem);
  q('ce-mode').addEventListener('change', ceRender);
  q('ce-location').addEventListener('change', () => { ceLines = []; ceRender(); });
  q('ce-post').addEventListener('click', cePost);
  q('ce-close').addEventListener('click', () => panel.remove());
  panel.addEventListener('input', e => {
    const i = e.target.dataset.ceQty;
    if (i === undefined) return;
    ceLines[Number(i)].qty = e.target.value;
    const c = ceConsumed(ceLines[Number(i)]);
    const cell = e.target.closest('tr').children[3];
    cell.textContent = c === '' ? '' : c;
  });
  panel.addEventListener('click', e => {
    const i = e.target.dataset.ceRemove;
    if (i === undefined) return;
    ceLines.splice(Number(i), 1);
    ceRender();
  });
  ceRender();
  panel.scrollIntoView({ block: 'start' });
}
