// Transfers (Stage 21 QA fix): the sidebar's "Transfers" item routed to a
// view name ('transfers') the router had no case for, so it always fell
// through to the generic "Module Setup Pending" mock screen - a dead entry
// point despite engines/transfer_orders.go already having a full dispatch/
// receive lifecycle. TransferOrder is already a registered generic doctype,
// but its dispatch/receive engine functions need a JSON-encoded `items` line
// array that has no field/UI anywhere yet, so - unlike Vendors/POSProfile -
// this needed a small bespoke view rather than just pointing at the generic
// doctype-table.
//
// Stage 73.1.7 (D04): approval is maker-checker through the approval engine
// (Send for approval -> someone else approves), with a direct path for a
// Store Manager below the client's transfer limit (Approve now). Dispatch
// asks for serial numbers / lots where the item is tracked; receipt is one
// dialog per delivery (good, damaged, lost, excess, with reason codes, and
// "the rest stays in transit"); goods still in transit can go back to the
// source; a transfer can be cancelled before dispatch; Details shows the
// running balance and every receipt.
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
//
let transferLineItems = [];

// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnRestorePane, txnPageTitle, txnNewButton, txnDone, txnStockFromTo, mountStockFromTo, readStockFromTo } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const TRANSFER_STATUS_BADGE = {
  Draft: 'badge-secondary', 'Pending Approval': 'badge-warning', Approved: 'badge-success', Packed: 'badge-success',
  Dispatched: 'badge-warning', 'Partially Received': 'badge-warning', Received: 'badge-success',
  Returned: 'badge-secondary', Cancelled: 'badge-danger'
};

async function renderTransfersView(container) {
  const res = await apiFetch('/api/v1/doc/TransferOrder');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load transfer orders.', () => renderView('transfers')); return; }
  const transfers = await res.json();
  state.docData = transfers;
  transferLineItems = [];

  // Stage 68: the frozen transaction layout (txn-screen.css). Stage 69: opens
  // on the list; New is its own page, switched in place so the lines being
  // keyed in survive a look at the list.
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.id = 'transfer-screen';
  screen.dataset.landing = 'history';
  screen.innerHTML = `
    <div class="txn-bar">
      ${txnPageTitle({ screen: 'transfer-screen', title: 'Stock Movement', page: 'New transfer', count: transfers.length })}
      ${txnContext()}${txnStockFromTo('Transfer Out', 'txn-main-only')}
      ${txnNewButton({ screen: 'transfer-screen', label: 'New Transfer' })}
      <div id="transfer-sum" class="txn-sum txn-main-only"></div>
      <div class="txn-actions txn-main-only">
        <div id="transfer-form-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="transfer-create-btn">Create Transfer</button>
      </div>
    </div>

    <div class="txn-pane" data-pane="new">
      <div class="txn-head">
        ${autoNumberField('Transfer Number', 'TO', '100%')}
        <div class="form-group">
          <label class="form-label" for="transfer-from">From Warehouse</label>
          <input type="text" id="transfer-from" class="form-input" autocomplete="off">
        </div>
        <div class="form-group">
          <label class="form-label" for="transfer-to">To Warehouse</label>
          <input type="text" id="transfer-to" class="form-input" autocomplete="off">
        </div>
      </div>
      <div class="txn-hint"><span>Create a draft, get it approved, then dispatch and receive it.</span></div>
      <div class="txn-entry">
        <div class="form-group txn-entry-main">
          <label class="form-label" for="transfer-line-sku">SKU</label>
          <input type="text" id="transfer-line-sku" class="form-input">
        </div>
        <div class="form-group">
          <label class="form-label" for="transfer-line-qty">Qty</label>
          <input type="number" id="transfer-line-qty" class="form-input" min="1">
        </div>
        <button class="btn btn-outline" id="transfer-add-line-btn" type="button">Add Line</button>
        <!-- Stage 85.E8: lines from a CSV (Barcode, Qty). -->
        <button class="btn btn-outline" id="transfer-upload-btn" type="button">Upload lines</button>
      </div>
      <div class="txn-grid"><div id="transfer-lines-list"></div></div>
    </div>

    <div class="txn-pane hidden" data-pane="history">
      <div class="txn-grid">
        <table>
          <thead>
            <tr>
              <th>Transfer #</th><th>From</th><th>To</th><th>Items</th><th>Status</th><th></th>
            </tr>
          </thead>
          <tbody>
            ${transfers.length === 0
              ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No transfers yet. Click <b>+ New Transfer</b>, add the lines, then <b>Create Transfer</b> to move stock between locations.</td></tr>`
              : transfers.map(t => `
                <tr>
                  <td style="font-family: monospace;">${escapeHTMLText(t.transfer_number || t.id)}</td>
                  <td>${escapeHTMLText(t.from_warehouse || '')}</td>
                  <td>${escapeHTMLText(t.to_warehouse || '')}</td>
                  <td>${(() => { try { return JSON.parse(t.items || '[]').length; } catch (e) { return 0; } })()}</td>
                  <td><span class="badge ${TRANSFER_STATUS_BADGE[t.status] || 'badge-secondary'}">${escapeHTMLText(t.status || '')}</span></td>
                  <td>${renderTransferActions(t)}</td>
                </tr>
              `).join('')}
          </tbody>
        </table>
      </div>
    </div>
  `;
  container.appendChild(screen);
  txnRestorePane('transfer-screen');

  renderTransferLinesList();
  document.getElementById('transfer-add-line-btn').addEventListener('click', addTransferLine);
  document.getElementById('transfer-upload-btn').addEventListener('click', async () => {
    const pickers = await loadViewModule('/view-pickers.js?v=1');
    pickers.openLineUpload({ title: 'Upload transfer lines', template: 'stock-transfer', onRows: (rows) => {
      rows.forEach(r => {
        const qty = Math.round(r.qty);
        const existing = transferLineItems.find(l => l.sku === r.sku);
        if (existing) existing.qty += qty; else transferLineItems.push({ sku: r.sku, qty });
      });
      renderTransferLinesList();
    } });
  });
  document.getElementById('transfer-create-btn').addEventListener('click', createTransferOrder);
  attachLinkTypeahead(document.getElementById('transfer-from'), 'Location');
  attachLinkTypeahead(document.getElementById('transfer-to'), 'Location');
  // Stage 106.5: the From follows the sending store.
  const syncFrom = () => mountStockFromTo(screen, document.getElementById('transfer-from').value.trim());
  document.getElementById('transfer-from').addEventListener('change', syncFrom);
  // Stage 57: the line's item was a bare text box - people had to type the
  // exact code. It picks by name now and still stores the code.
  attachLinkTypeahead(document.getElementById('transfer-line-sku'), 'Item');
  const savedContext = sessionStorage.getItem('erp_stock_transfer_context');
  sessionStorage.removeItem('erp_stock_transfer_context');
  if (savedContext) {
    try {
      const context = JSON.parse(savedContext);
      document.getElementById('transfer-from').value = String(context.location || '');
      document.getElementById('transfer-line-sku').value = String(context.sku || '');
    } catch (error) { /* Ignore a stale navigation hint; the form remains usable. */ }
  }
  syncFrom();
}

function renderTransferLinesList() {
  const el = document.getElementById('transfer-lines-list');
  if (!el) return;
  // Stage 68: the bar's summary - lines and units moving.
  const sumEl = document.getElementById('transfer-sum');
  const units = transferLineItems.reduce((s, l) => s + (Number(l.qty) || 0), 0);
  if (sumEl) {
    sumEl.innerHTML = transferLineItems.length
      ? `<span><b>${transferLineItems.length}</b> line${transferLineItems.length === 1 ? '' : 's'}</span><span class="txn-total" title="Units moving">${units} units</span>`
      : '<span>Add a line to start the transfer.</span>';
  }
  if (transferLineItems.length === 0) {
    el.innerHTML = `<p class="txn-empty">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = `<table>
    <thead><tr><th>Item</th><th class="num">Qty</th><th></th></tr></thead>
    <tbody>${transferLineItems.map((line, idx) => `
      <tr>
        <td data-link-doctype="Item" data-link-ref="${escapeHTMLText(line.sku)}">${escapeHTMLText(line.sku)}</td>
        <td class="num">${line.qty}</td>
        <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeTransferLine', [idx])}>Remove</button></td>
      </tr>`).join('')}</tbody>
  </table>`;
}

function addTransferLine() {
  const skuEl = document.getElementById('transfer-line-sku');
  const qtyEl = document.getElementById('transfer-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku || !qty || qty <= 0) return;
  // One line per item: the receipt and the balance are kept per item, so a
  // second line for the same item adds to the first.
  const existing = transferLineItems.find(l => l.sku === sku);
  if (existing) existing.qty += qty; else transferLineItems.push({ sku, qty });
  skuEl.value = '';
  qtyEl.value = '';
  renderTransferLinesList();
}

window.removeTransferLine = function(idx) {
  transferLineItems.splice(idx, 1);
  renderTransferLinesList();
};

async function createTransferOrder() {
  // BLD-036: guard against a double-click creating two TransferOrders.
  await guardAgainstDoubleSubmit(document.getElementById('transfer-create-btn'), 'Creating...', createTransferOrderInner);
}

async function createTransferOrderInner() {
  const errorEl = document.getElementById('transfer-form-error');
  errorEl.classList.add('hidden');

  const fromWarehouse = document.getElementById('transfer-from').value.trim();
  const toWarehouse = document.getElementById('transfer-to').value.trim();

  if (!fromWarehouse || !toWarehouse || transferLineItems.length === 0) {
    errorEl.textContent = 'From/To Warehouse and at least one line item are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/TransferOrder', {
    method: 'POST',
    body: JSON.stringify({
      from_warehouse: fromWarehouse, to_warehouse: toWarehouse,
      items: JSON.stringify(transferLineItems), status: 'Draft',
      ...readStockFromTo(document.getElementById('transfer-screen'))
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create transfer.';
    errorEl.classList.remove('hidden');
    return;
  }
  txnDone('transfer-screen');
  renderView('transfers');
}

export {
  renderTransfersView,
  renderTransferLinesList,
  addTransferLine,
  createTransferOrder,
  createTransferOrderInner,
  renderTransferActions,
  packTransferOrder,
  packTransferOrderWithCartonization,
  approveTransferOrder,
  submitTransferOrder,
  decideTransferOrder,
  dispatchTransferOrder,
  receiveTransferOrder,
  returnTransferOrder,
  cancelTransferOrder,
  showTransferDetails
};

// Stage 63: Transfer Out (dispatch challan) prints once approved, Transfer In
// once received - through the designed-template print dialog (trade-docs.js,
// which sets window.printTransactionDocument when it loads).
import('./trade-docs.js' + new URL(import.meta.url).search).catch(() => null);

function renderTransferActions(t) {
  const prints = [];
  if (['Approved', 'Packed', 'Dispatched', 'Partially Received', 'Received'].includes(t.status)) {
    prints.push(`<button class="action-btn" ${actionAttrs('printTransactionDocument', ['TransferOrder', t.id, 'TransferOut'])}>Print Transfer Out</button>`);
  }
  if (t.status === 'Received' || t.status === 'Partially Received') {
    prints.push(`<button class="action-btn" ${actionAttrs('printTransactionDocument', ['TransferOrder', t.id, 'TransferIn'])}>Print Transfer In</button>`);
  }
  const stage = renderTransferStageActions(t);
  const details = `<button class="action-btn" ${actionAttrs('showTransferDetails', [t.id])}>Details</button>`;
  return [stage, details, ...prints].filter(Boolean).join(' ');
}

function transferBtn(fn, id, label, danger) {
  return `<button class="action-btn${danger ? ' action-btn-danger' : ''}" ${actionAttrs(fn, [id])}>${label}</button>`;
}

function renderTransferStageActions(t) {
  const cancel = transferBtn('cancelTransferOrder', t.id, 'Cancel', true);
  switch (t.status) {
    case 'Draft':
      return `${transferBtn('approveTransferOrder', t.id, 'Approve now')} ${transferBtn('submitTransferOrder', t.id, 'Send for approval')} ${cancel}`;
    case 'Pending Approval':
      return `<button class="action-btn" ${actionAttrs('decideTransferOrder', [t.id, 'Approved'])}>Approve</button> <button class="action-btn action-btn-danger" ${actionAttrs('decideTransferOrder', [t.id, 'Rejected'])}>Reject</button> ${cancel}`;
    case 'Approved':
      // Pack (Stage 20.19) is an optional confirmation step, not a required
      // gate - Dispatch stays available directly from Approved too. Stage
      // 26.5.8 adds a second pack path that suggests a carton split instead
      // of prompting box-by-box.
      return `${transferBtn('packTransferOrder', t.id, 'Pack')} ${transferBtn('packTransferOrderWithCartonization', t.id, 'Pack (Suggested Cartons)')} ${transferBtn('dispatchTransferOrder', t.id, 'Dispatch')} ${cancel}`;
    case 'Packed':
      return `${transferBtn('dispatchTransferOrder', t.id, 'Dispatch')} ${cancel}`;
    case 'Dispatched':
    case 'Partially Received':
      return `${transferBtn('receiveTransferOrder', t.id, 'Receive')} ${transferBtn('returnTransferOrder', t.id, 'Return to source')}`;
  }
  return '';
}

// ---------------------------------------------------------------------------
// One small dialog for every step below - built on demand (nothing in the
// shell), removed on close, so a stale form never lingers in the DOM.
// ---------------------------------------------------------------------------
function openTransferDialog({ title, body, submitLabel, wide, onSubmit, onOpen, stockStore }) {
  document.getElementById('transfer-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'transfer-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: ${wide ? '860px' : '560px'};" role="dialog" aria-modal="true" aria-labelledby="transfer-dialog-title">
      <div class="modal-header">
        <h3 class="modal-title" id="transfer-dialog-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-transfer-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button>
      </div>
      <form id="transfer-dialog-form" novalidate>
        <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">
          ${body}
          <div id="transfer-dialog-error" class="login-error hidden"></div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" data-transfer-close>${onSubmit ? 'Cancel' : 'Close'}</button>
          ${onSubmit ? `<button type="submit" class="btn btn-primary" id="transfer-dialog-submit">${escapeHTMLText(submitLabel || 'Save')}</button>` : ''}
        </div>
      </form>
    </div>`;
  document.body.appendChild(overlay);
  if (stockStore) mountStockFromTo(overlay, stockStore); // Stage 106.5
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-transfer-close]').forEach(b => b.addEventListener('click', close));
  // "Add a reason code" leaves for the master list - the dialog goes with it.
  overlay.addEventListener('click', e => { if (e.target.closest('[data-act="openSetupDoctype"]')) close(); });
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const form = overlay.querySelector('#transfer-dialog-form');
  form.addEventListener('submit', async e => {
    e.preventDefault();
    if (!onSubmit) return;
    const errEl = overlay.querySelector('#transfer-dialog-error');
    errEl.classList.add('hidden');
    const btn = overlay.querySelector('#transfer-dialog-submit');
    await guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
      const message = await onSubmit(form);
      if (message) {
        errEl.textContent = message;
        errEl.classList.remove('hidden');
        return;
      }
      close();
    });
  });
  requestAnimationFrame(() => overlay.classList.add('open'));
  if (onOpen) onOpen(overlay);
  overlay.querySelector('input, select, textarea, button[type=submit]')?.focus();
  return overlay;
}

// postTransfer returns '' on success, or the server's message for the dialog.
async function postTransfer(path, payload) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return 'No response from the server.';
  if (res.ok) return '';
  try {
    const body = await res.json();
    return body.detail || body.message || body.error || `Failed (${res.status}).`;
  } catch (e) {
    return `Failed (${res.status}).`;
  }
}

async function loadTransferDetail(id) {
  const res = await apiFetch(`/api/v1/transfer/detail?transfer_order_id=${encodeURIComponent(id)}`);
  if (!res || !res.ok) return null;
  return res.json();
}

// Active reason codes of one category, for a <select>; with none, the select
// is replaced by a way to add one (Masters > Reason Codes), not a dead end.
async function reasonCodeField(category, id, label) {
  const res = await apiFetch('/api/v1/doc/ReasonCode?limit=500');
  let codes = [];
  if (res && res.ok) {
    const payload = await res.json();
    const docs = Array.isArray(payload) ? payload : (payload.data || payload.documents || []);
    codes = docs.map(d => ({ id: d.id, ...(d.data || d) }))
      .filter(d => d.category === category && (d.status || 'Active') === 'Active');
  }
  if (codes.length === 0) {
    return `<div class="form-group"><span class="form-label">${escapeHTMLText(label)}</span>
      <p style="margin: 0; color: var(--text-muted);">No active reason codes of type <b>${escapeHTMLText(category)}</b> yet.
      <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['ReasonCode'])}>Add a reason code</button></p>
      <input type="hidden" id="${id}" value=""></div>`;
  }
  return `<div class="form-group"><label class="form-label" for="${id}">${escapeHTMLText(label)}</label>
    <select id="${id}" class="form-select"><option value="">Choose a reason...</option>
    ${codes.map(c => `<option value="${escapeHTMLText(c.id)}">${escapeHTMLText(c.code || c.id)}${c.description && c.description !== c.code ? ` - ${escapeHTMLText(c.description)}` : ''}</option>`).join('')}
    </select></div>`;
}

// sku -> item name, so the dialogs show names, not codes (Stage 60.5).
async function transferItemNames(id) {
  const detail = await loadTransferDetail(id);
  return Object.fromEntries((detail?.lines || []).map(l => [l.sku, l.name || '']));
}

function itemLabel(names, sku) {
  const name = names[sku];
  return name && name !== sku ? `${escapeHTMLText(name)} <small style="color: var(--text-muted);">${escapeHTMLText(sku)}</small>` : escapeHTMLText(sku);
}

function transferRow(id) {
  return (state.docData || []).find(t => t.id === id);
}

function transferBalance(row) {
  try {
    if (row.transfer_balance) return JSON.parse(row.transfer_balance);
  } catch (e) { /* fall through to the dispatched lines */ }
  try {
    return JSON.parse(row.dispatched_items || '[]').map(l => ({ sku: l.sku, dispatched: l.qty, received: 0, damaged: 0, lost: 0, returned: 0, excess: 0, lots: (l.lots || []).map(x => ({ batch_no: x.batch_no, dispatched: x.qty, received: 0, damaged: 0, lost: 0, returned: 0 })) }));
  } catch (e) { return []; }
}

const openQty = b => (b.dispatched || 0) - (b.received || 0) - (b.damaged || 0) - (b.lost || 0) - (b.returned || 0);
const inTransitSerials = b => Object.entries(b.serials || {}).filter(([, st]) => st === 'In transit').map(([sn]) => sn);
const splitSerials = text => String(text || '').split(/[\s,;]+/).map(s => s.trim()).filter(Boolean);
const newKey = () => (window.crypto && crypto.randomUUID) ? crypto.randomUUID() : `k-${Date.now()}-${Math.random().toString(16).slice(2)}`;

// ---------------------------------------------------------------------------
// Approval (D04)
// ---------------------------------------------------------------------------
async function approveTransferOrder(id) {
  const detail = await loadTransferDetail(id);
  if (!detail) { await showCustomAlert('This transfer could not be valued.', 'Approve'); return; }
  const value = `₹${formatMoney(detail.value)}`;
  const limit = Number(detail.direct_approval_limit) || 0;
  let blocker = '';
  if (limit <= 0) blocker = 'Direct approval of transfers is switched off for this business.';
  else if (!detail.complete) blocker = `No cost is on record for ${(detail.uncosted || []).join(', ')}, so its value is not known.`;
  else if (detail.value >= limit) blocker = `This transfer is worth ${value}, at or above the ₹${formatMoney(limit)} direct-approval limit.`;
  if (blocker) {
    if (await showCustomConfirm(`${blocker}\n\nSend it for approval? Someone other than you approves it.`, 'Needs a second approver')) {
      await submitTransferOrder(id, true);
    }
    return;
  }
  if (!(await showCustomConfirm(`Approve this transfer now?\n\nValue ${value} is below the ₹${formatMoney(limit)} limit a Store Manager can approve alone.`, 'Approve Transfer'))) return;
  const msg = await postTransfer('/api/v1/transfer/approve-direct', { transfer_order_id: id });
  if (msg) { await showCustomAlert(msg, 'Approve Failed'); return; }
  renderView('transfers');
}

async function submitTransferOrder(id, confirmed) {
  if (!confirmed && !(await showCustomConfirm('Send this transfer for approval? A Store Manager other than you approves it from the Approvals inbox or this list.', 'Send for Approval'))) return;
  const msg = await postTransfer('/api/v1/transfer/submit', { transfer_order_id: id });
  if (msg) { await showCustomAlert(msg, 'Send Failed'); return; }
  await showCustomAlert('Sent for approval.', 'Transfer');
  renderView('transfers');
}

async function decideTransferOrder(id, decision) {
  let comment = '';
  if (decision === 'Rejected') {
    comment = await showCustomPrompt('Why is this transfer rejected?', '', 'Reject Transfer');
    if (comment === null) return;
  }
  const msg = await postTransfer('/api/v1/approval/decide', { doctype: 'TransferOrder', document_id: id, decision, comment });
  if (msg) { await showCustomAlert(msg, 'Decision Failed'); return; }
  renderView('transfers');
}

// ---------------------------------------------------------------------------
// Pack (unchanged)
// ---------------------------------------------------------------------------
// Prompts for a Box ID per line item, grouping items that share a Box ID into
// one box before submitting - covers both "one box per SKU" and "everything
// in one box" without a bespoke multi-box form.
async function packTransferOrder(id) {
  const row = transferRow(id);
  if (!row) return;
  let lines = [];
  try { lines = JSON.parse(row.items || '[]'); } catch (e) { lines = []; }
  if (lines.length === 0) {
    await showCustomAlert('No line items found on this transfer.', 'Error');
    return;
  }

  const boxByItem = {};
  for (const line of lines) {
    const boxId = await showCustomPrompt(`Box ID for ${line.sku} (qty ${line.qty}):`, 'BOX1');
    if (boxId === null) return;
    if (!boxByItem[boxId]) boxByItem[boxId] = [];
    boxByItem[boxId].push({ sku: line.sku, qty: line.qty });
  }
  const boxes = Object.keys(boxByItem).map(boxId => ({ box_id: boxId, items: boxByItem[boxId] }));

  const res = await apiFetch('/api/v1/wms/transfer/pack', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, boxes })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to pack transfer.', 'Pack Failed');
    return;
  }
  renderView('transfers');
}

// Stage 26.5.8: cartonization - suggests a box split via SuggestCartonization
// (first-fit-decreasing by qty capacity) instead of prompting a Box ID per
// line, then confirms the suggestion before packing exactly like the manual
// path above does with its own boxes array.
async function packTransferOrderWithCartonization(id) {
  const row = transferRow(id);
  if (!row) return;
  let lines = [];
  try { lines = JSON.parse(row.items || '[]'); } catch (e) { lines = []; }
  if (lines.length === 0) {
    await showCustomAlert('No line items found on this transfer.', 'Error');
    return;
  }
  const cartonType = await showCustomPrompt('Carton Type code to pack into:', 'BOX-S');
  if (!cartonType) return;

  const suggestRes = await apiFetch('/api/v1/wms/cartonization/suggest', {
    method: 'POST',
    body: JSON.stringify({ carton_type: cartonType, items: lines.map(l => ({ sku: l.sku, qty: l.qty })) })
  });
  if (!suggestRes) return;
  if (!suggestRes.ok) {
    await showApiError(suggestRes, 'Failed to suggest cartonization.', 'Cartonization Failed');
    return;
  }
  const boxes = await suggestRes.json();
  const summary = boxes.map(b => `${b.box_id}: ${b.items.map(it => `${it.sku} x${it.qty}`).join(', ')} (${b.used_capacity}/${b.max_capacity})`).join('\n');
  if (!(await showCustomConfirm(`Pack into ${boxes.length} suggested box(es)?\n\n${summary}`, 'Confirm Suggested Cartonization'))) return;

  const packRes = await apiFetch('/api/v1/wms/transfer/pack', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, boxes: boxes.map(b => ({ box_id: b.box_id, items: b.items })) })
  });
  if (!packRes) return;
  if (!packRes.ok) {
    await showApiError(packRes, 'Failed to pack transfer.', 'Pack Failed');
    return;
  }
  renderView('transfers');
}

// ---------------------------------------------------------------------------
// Dispatch: serial numbers for serial-tracked items; lots for batch-tracked
// ones are picked earliest-expiry-first from the bins unless named here.
// ---------------------------------------------------------------------------
async function dispatchTransferOrder(id) {
  const detail = await loadTransferDetail(id);
  const tracked = (detail?.lines || []).filter(l => /Serial|Batch/.test(l.tracking_mode || ''));
  if (tracked.length === 0) {
    if (!(await showCustomConfirm('Dispatch this transfer? Stock will move from the source location into transit.', 'Dispatch Transfer'))) return;
    const msg = await postTransfer('/api/v1/transfer/dispatch', { transfer_order_id: id });
    if (msg) { await showCustomAlert(msg, 'Dispatch Failed'); return; }
    renderView('transfers');
    return;
  }
  const body = `<p style="margin: 0; color: var(--text-muted);">These items are tracked. Stock moves from the source into transit when you dispatch.</p>
    ${tracked.map((l, i) => /Serial/.test(l.tracking_mode) ? `
      <div class="form-group"><label class="form-label" for="tdisp-serial-${i}">${escapeHTMLText(l.name || l.sku)} - scan ${l.qty} serial number${l.qty === 1 ? '' : 's'}</label>
        <textarea id="tdisp-serial-${i}" class="form-input" rows="3" data-sku="${escapeHTMLText(l.sku)}" data-kind="serial" data-qty="${l.qty}" placeholder="One per line"></textarea></div>` : `
      <div class="form-group"><label class="form-label" for="tdisp-lot-${i}">${escapeHTMLText(l.name || l.sku)} (${l.qty}) - lots</label>
        <input id="tdisp-lot-${i}" class="form-input" data-sku="${escapeHTMLText(l.sku)}" data-kind="lot" data-qty="${l.qty}" placeholder="Leave blank to pick earliest expiry first, or LOT-A:3, LOT-B:2">
        <small style="color: var(--text-muted);">A store without bins has to name the lot.</small></div>`).join('')}`;
  openTransferDialog({
    title: 'Dispatch Transfer', body, submitLabel: 'Dispatch',
    onSubmit: async form => {
      const lines = [];
      for (const el of form.querySelectorAll('[data-kind]')) {
        const sku = el.dataset.sku;
        const qty = Number(el.dataset.qty);
        if (el.dataset.kind === 'serial') {
          const serials = splitSerials(el.value);
          if (serials.length !== qty) return `${sku}: scan exactly ${qty} serial number(s) - ${serials.length} entered.`;
          lines.push({ sku, qty, serials });
        } else if (el.value.trim()) {
          const parts = el.value.split(',').map(p => p.trim()).filter(Boolean);
          const lots = parts.map(p => { const [batch, n] = p.split(':'); return { batch_no: batch.trim(), qty: n === undefined ? (parts.length === 1 ? qty : 0) : Number(n) }; });
          if (lots.some(l => !l.batch_no || !(l.qty > 0))) return `${sku}: write the lots as LOT:qty, separated by commas.`;
          lines.push({ sku, qty, lots });
        }
      }
      const msg = await postTransfer('/api/v1/transfer/dispatch', { transfer_order_id: id, lines });
      if (!msg) renderView('transfers');
      return msg;
    }
  });
}

// ---------------------------------------------------------------------------
// Receive: one dialog per delivery.
// ---------------------------------------------------------------------------
async function receiveTransferOrder(id) {
  const row = transferRow(id);
  if (!row) return;
  const balance = transferBalance(row).filter(b => openQty(b) > 0);
  if (balance.length === 0) { await showCustomAlert('Nothing is still in transit on this transfer.', 'Receive'); return; }
  const names = await transferItemNames(id);
  const reasonField = await reasonCodeField('Transfer Discrepancy', 'trcv-reason', 'Reason for damaged, lost or extra units');
  const numberCell = (cls, i, value) => `<td><input type="number" min="0" class="form-input ${cls}" data-row="${i}" value="${value}" style="width: 72px;" aria-label="${cls.replace('trcv-', '')} quantity"></td>`;
  const lineRows = balance.map((b, i) => {
    const serials = inTransitSerials(b);
    if (serials.length) {
      return `<tr data-row="${i}" data-sku="${escapeHTMLText(b.sku)}" data-serial="1">
        <td>${itemLabel(names, b.sku)}<br><small style="color: var(--text-muted);">${openQty(b)} in transit</small></td>
        <td colspan="4">
          <label class="form-label" for="trcv-sn-ok-${i}">Received (serial numbers)</label>
          <textarea id="trcv-sn-ok-${i}" class="form-input trcv-sn-ok" rows="2">${escapeHTMLText(serials.join('\n'))}</textarea>
          <label class="form-label" for="trcv-sn-dmg-${i}">Damaged</label>
          <textarea id="trcv-sn-dmg-${i}" class="form-input trcv-sn-dmg" rows="1"></textarea>
          <label class="form-label" for="trcv-sn-lost-${i}">Lost</label>
          <textarea id="trcv-sn-lost-${i}" class="form-input trcv-sn-lost" rows="1"></textarea>
        </td><td></td></tr>`;
    }
    const lots = (b.lots || []).filter(l => openQty(l) > 0);
    return `<tr data-row="${i}" data-sku="${escapeHTMLText(b.sku)}">
      <td>${itemLabel(names, b.sku)}<br><small style="color: var(--text-muted);">${openQty(b)} in transit</small></td>
      ${numberCell('trcv-good', i, openQty(b))}${numberCell('trcv-dmg', i, 0)}${numberCell('trcv-lost', i, 0)}${numberCell('trcv-excess', i, 0)}
      <td>${lots.length ? `<select class="form-select trcv-lot" data-row="${i}" aria-label="Lot"><option value="">Oldest lot first</option>${lots.map(l => `<option value="${escapeHTMLText(l.batch_no)}">${escapeHTMLText(l.batch_no)} (${openQty(l)})</option>`).join('')}</select>` : '<small style="color: var(--text-muted);">-</small>'}</td>
    </tr>`;
  }).join('');
  openTransferDialog({
    title: `Receive ${row.transfer_number || row.id}`, wide: true, submitLabel: 'Record Receipt', stockStore: row.to_warehouse,
    body: `<div class="table-panel" style="overflow-x: auto;"><table>
        <thead><tr><th>Item</th><th>Good</th><th>Damaged</th><th>Lost</th><th>Extra</th><th>Lot</th></tr></thead>
        <tbody>${lineRows}</tbody></table></div>
      ${txnStockFromTo('Transfer In')}
      ${reasonField}
      <label style="display: flex; gap: 8px; align-items: center;"><input type="checkbox" id="trcv-keep-open" checked>
        Units not entered here stay in transit for a later delivery</label>
      <small style="color: var(--text-muted);">Untick to close the transfer now: anything not entered is recorded as lost and needs the reason above. Damaged units are kept out of sellable stock.</small>`,
    onSubmit: async form => {
      const key = form.dataset.key || (form.dataset.key = newKey());
      const reason = form.querySelector('#trcv-reason')?.value || '';
      const lines = [];
      for (const tr of form.querySelectorAll('tbody tr[data-sku]')) {
        const sku = tr.dataset.sku;
        if (tr.dataset.serial) {
          const line = { sku, serials: splitSerials(tr.querySelector('.trcv-sn-ok').value),
            damaged_serials: splitSerials(tr.querySelector('.trcv-sn-dmg').value), lost_serials: splitSerials(tr.querySelector('.trcv-sn-lost').value) };
          if (line.serials.length + line.damaged_serials.length + line.lost_serials.length > 0) lines.push({ ...line, reason_code: reason });
          continue;
        }
        const n = cls => Math.max(0, parseInt(tr.querySelector(cls)?.value, 10) || 0);
        const line = { sku, qty: n('.trcv-good'), damaged_qty: n('.trcv-dmg'), lost_qty: n('.trcv-lost'), excess_qty: n('.trcv-excess'),
          batch_no: tr.querySelector('.trcv-lot')?.value || '', reason_code: reason };
        if (line.qty + line.damaged_qty + line.lost_qty + line.excess_qty > 0) lines.push(line);
      }
      const keepOpen = form.querySelector('#trcv-keep-open').checked;
      if (lines.length === 0 && keepOpen) return 'Enter what arrived.';
      if (lines.some(l => (l.damaged_qty || l.lost_qty || l.excess_qty || (l.damaged_serials || []).length || (l.lost_serials || []).length)) && !reason) return 'Choose a reason for the damaged, lost or extra units.';
      // Closing short needs the reason too; the server says which item.
      if (!keepOpen) for (const b of balance) if (!lines.some(l => l.sku === b.sku)) lines.push({ sku: b.sku, qty: 0, reason_code: reason });
      const msg = await postTransfer('/api/v1/transfer/receive', { transfer_order_id: id, received_items: lines, keep_open: keepOpen, receipt_key: key,
        ...readStockFromTo(form) });
      if (!msg) renderView('transfers');
      return msg;
    }
  });
}

// ---------------------------------------------------------------------------
// Return to source: goods still in transit go back to the sending store.
// ---------------------------------------------------------------------------
async function returnTransferOrder(id) {
  const row = transferRow(id);
  if (!row) return;
  const balance = transferBalance(row).filter(b => openQty(b) > 0);
  if (balance.length === 0) { await showCustomAlert('Nothing is still in transit on this transfer.', 'Return to Source'); return; }
  const names = await transferItemNames(id);
  const reasonField = await reasonCodeField('Transfer Discrepancy', 'tret-reason', 'Reason');
  const from = (await linkNameMap('Location')).get(String(row.from_warehouse || '')) || row.from_warehouse;
  openTransferDialog({
    title: `Return to ${from || 'source'}`, submitLabel: 'Return to Source',
    body: `<p style="margin: 0; color: var(--text-muted);">The units go back into the sending store's stock; put tracked lots away again there.</p>
      ${balance.map((b, i) => {
        const serials = inTransitSerials(b);
        return serials.length
          ? `<div class="form-group"><label class="form-label" for="tret-sn-${i}">${itemLabel(names, b.sku)} - serial numbers going back</label>
              <textarea id="tret-sn-${i}" class="form-input" rows="2" data-sku="${escapeHTMLText(b.sku)}" data-kind="serial">${escapeHTMLText(serials.join('\n'))}</textarea></div>`
          : `<div class="form-group"><label class="form-label" for="tret-qty-${i}">${itemLabel(names, b.sku)} - quantity going back (${openQty(b)} in transit)</label>
              <input id="tret-qty-${i}" type="number" min="0" max="${openQty(b)}" class="form-input" data-sku="${escapeHTMLText(b.sku)}" data-kind="qty" value="${openQty(b)}"></div>`;
      }).join('')}
      ${reasonField}
      <div class="form-group"><label class="form-label" for="tret-note">Note</label><input id="tret-note" class="form-input" maxlength="200"></div>`,
    onSubmit: async form => {
      const key = form.dataset.key || (form.dataset.key = newKey());
      const lines = [];
      for (const el of form.querySelectorAll('[data-kind]')) {
        if (el.dataset.kind === 'serial') {
          const serials = splitSerials(el.value);
          if (serials.length) lines.push({ sku: el.dataset.sku, serials });
        } else {
          const qty = parseInt(el.value, 10) || 0;
          if (qty > 0) lines.push({ sku: el.dataset.sku, qty });
        }
      }
      if (lines.length === 0) return 'Enter what is going back.';
      const reason = form.querySelector('#tret-reason')?.value || '';
      if (!reason) return 'Choose a reason.';
      const msg = await postTransfer('/api/v1/transfer/return', { transfer_order_id: id, reason_code: reason,
        note: form.querySelector('#tret-note').value.trim(), return_key: key, lines });
      if (!msg) renderView('transfers');
      return msg;
    }
  });
}

// ---------------------------------------------------------------------------
// Cancel (before dispatch)
// ---------------------------------------------------------------------------
async function cancelTransferOrder(id) {
  const row = transferRow(id);
  if (!row) return;
  const reasonField = await reasonCodeField('Cancellation', 'tcxl-reason', 'Reason');
  openTransferDialog({
    title: `Cancel ${row.transfer_number || row.id}`, submitLabel: 'Cancel Transfer',
    body: `<p style="margin: 0;">Nothing has left the store yet, so no stock moves.</p>${reasonField}
      <div class="form-group"><label class="form-label" for="tcxl-note">Note</label><input id="tcxl-note" class="form-input" maxlength="200"></div>`,
    onSubmit: async form => {
      const reason = form.querySelector('#tcxl-reason')?.value || '';
      if (!reason) return 'Choose a reason.';
      const msg = await postTransfer('/api/v1/transfer/cancel', { transfer_order_id: id, reason_code: reason, note: form.querySelector('#tcxl-note').value.trim() });
      if (!msg) renderView('transfers');
      return msg;
    }
  });
}

// ---------------------------------------------------------------------------
// Details: the running balance per item and lot, serial units, and every
// receipt / return with who and when.
// ---------------------------------------------------------------------------
async function showTransferDetails(id) {
  const row = transferRow(id);
  if (!row) return;
  const balance = transferBalance(row);
  const names = await transferItemNames(id);
  const stores = await linkNameMap('Location'); // 118.4: names, not codes
  const store = code => stores.get(String(code || '')) || code || '';
  let events = [];
  try { events = JSON.parse(row.transfer_events || '[]'); } catch (e) { events = []; }
  const cell = n => `<td class="num">${n || 0}</td>`;
  const balanceTable = balance.length ? `<div class="table-panel" style="overflow-x: auto;"><table>
      <thead><tr><th>Item / lot</th><th class="num">Sent</th><th class="num">Received</th><th class="num">Damaged</th><th class="num">Lost</th><th class="num">Returned</th><th class="num">Extra</th><th class="num">In transit</th></tr></thead>
      <tbody>${balance.map(b => `<tr><td><b>${itemLabel(names, b.sku)}</b></td>${cell(b.dispatched)}${cell(b.received)}${cell(b.damaged)}${cell(b.lost)}${cell(b.returned)}${cell(b.excess)}${cell(openQty(b))}</tr>
        ${(b.lots || []).map(l => `<tr><td style="padding-left: 20px;">Lot ${escapeHTMLText(l.batch_no)}${l.expiry_date ? ` <small>(exp ${escapeHTMLText(l.expiry_date)})</small>` : ''}</td>${cell(l.dispatched)}${cell(l.received)}${cell(l.damaged)}${cell(l.lost)}${cell(l.returned)}${cell(l.excess)}${cell(openQty(l))}</tr>`).join('')}
        ${Object.keys(b.serials || {}).length ? `<tr><td colspan="8" style="padding-left: 20px;"><small>${Object.entries(b.serials).map(([sn, st]) => `${escapeHTMLText(sn)}: ${escapeHTMLText(st)}`).join(' · ')}</small></td></tr>` : ''}`).join('')}
      </tbody></table></div>`
    : '<p style="color: var(--text-muted);">Not dispatched yet.</p>';
  const eventList = events.length ? `<ul style="margin: 0; padding-left: 18px;">${events.map(ev => `<li>${escapeHTMLText(ev.kind)} ${ev.no} - ${escapeHTMLText(new Date(ev.at).toLocaleString())} by ${escapeHTMLText(ev.by || '')}:
      ${ev.lines.map(l => escapeHTMLText([names[l.sku] || l.sku, l.batch_no && `lot ${l.batch_no}`, l.received && `${l.received} good`, l.damaged && `${l.damaged} damaged`, l.lost && `${l.lost} lost`, l.excess && `${l.excess} extra`, l.returned && `${l.returned} returned`, l.reason_code && `(${l.reason_code})`, l.reason].filter(Boolean).join(' '))).join('; ')}</li>`).join('')}</ul>`
    : '';
  const facts = [
    row.total_amount !== undefined && row.total_amount !== null && row.total_amount !== '' ? `Value at cost: ₹${formatMoney(row.total_amount)}` : '',
    row.approval_mode ? `Approval: ${escapeHTMLText(row.approval_mode)}` : '',
    row.cancel_reason_code ? `Cancelled (${escapeHTMLText(row.cancel_reason_code)})${row.cancel_note ? ` - ${escapeHTMLText(row.cancel_note)}` : ''}` : ''
  ].filter(Boolean).join(' · ');
  openTransferDialog({
    title: `${row.transfer_number || row.id} - ${row.status}`, wide: true,
    body: `<p style="margin: 0;">${escapeHTMLText(store(row.from_warehouse))} → ${escapeHTMLText(store(row.to_warehouse))}${facts ? ` · ${facts}` : ''}</p>
      ${balanceTable}${eventList ? `<h4 style="margin: 4px 0 0;">Receipts and returns</h4>${eventList}` : ''}`
  });
}
