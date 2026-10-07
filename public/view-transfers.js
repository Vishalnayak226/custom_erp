// Transfers (Stage 21 QA fix): the sidebar's "Transfers" item routed to a
// view name ('transfers') the router had no case for, so it always fell
// through to the generic "Module Setup Pending" mock screen - a dead entry
// point despite engines/transfer_orders.go already having a full dispatch/
// receive lifecycle. TransferOrder is already a registered generic doctype
// (Draft/Approved/Dispatched/Received), but its dispatch/receive engine
// functions need a JSON-encoded `items` line array that has no field/UI
// anywhere yet, so - unlike Vendors/POSProfile - this needed a small
// bespoke view (mirroring renderAssetsView's form+list+action-button shape)
// rather than just pointing at the generic doctype-table.
//
// Draft -> Approved has no approval_rules row configured for TransferOrder
// (SubmitForApproval would just error "no approval rule configured"), so
// "Mark Approved" here is a direct status edit an authorized role can
// already make via the generic edit modal - this button just makes that
// one click instead of open-modal-find-status-save. Wiring TransferOrder
// into the maker-checker engine for real is a policy decision (which
// amount slab, which approver role) outside a QA-fix's scope.
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
//
let transferLineItems = [];

async function renderTransfersView(container) {
  const res = await apiFetch('/api/v1/doc/TransferOrder');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load transfer orders.', () => renderView('transfers')); return; }
  const transfers = await res.json();
  state.docData = transfers;
  transferLineItems = [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Stock Transfer</h1>
      <p class="page-subtitle">Move stock between stores/warehouses: create a draft, get it approved, then dispatch and receive it.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Transfer (Draft)</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Transfer Number', 'TO', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-from">From Warehouse</label>
        <input type="text" id="transfer-from" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-to">To Warehouse</label>
        <input type="text" id="transfer-to" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-line-sku">SKU</label>
        <input type="text" id="transfer-line-sku" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-line-qty">Qty</label>
        <input type="number" id="transfer-line-qty" class="form-input" style="width: 90px;" min="1">
      </div>
      <button class="btn btn-outline" id="transfer-add-line-btn" type="button">Add Line</button>
    </div>
    <div id="transfer-lines-list" style="margin: 12px 0;"></div>
    <div id="transfer-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="transfer-create-btn">Create Transfer</button>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Transfer #</th><th>From</th><th>To</th><th>Items</th><th>Status</th><th></th>
        </tr>
      </thead>
      <tbody>
  `;
  html += transfers.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No transfers yet. Use <b>Add Line</b> then <b>Create Transfer</b> above to move stock between locations.</td></tr>`
    : transfers.map(t => `
        <tr>
          <td style="font-family: monospace;">${t.transfer_number || t.id}</td>
          <td>${t.from_warehouse || ''}</td>
          <td>${t.to_warehouse || ''}</td>
          <td>${(() => { try { return JSON.parse(t.items || '[]').length; } catch (e) { return 0; } })()}</td>
          <td><span class="badge ${t.status === 'Received' ? 'badge-success' : t.status === 'Draft' ? 'badge-secondary' : 'badge-warning'}">${t.status}</span></td>
          <td>${renderTransferActions(t)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  renderTransferLinesList();
  document.getElementById('transfer-add-line-btn').addEventListener('click', addTransferLine);
  document.getElementById('transfer-create-btn').addEventListener('click', createTransferOrder);
  attachLinkTypeahead(document.getElementById('transfer-from'), 'Location');
  attachLinkTypeahead(document.getElementById('transfer-to'), 'Location');
  // Stage 57: the line's item was a bare text box - people had to type the
  // exact code. It picks by name now and still stores the code.
  attachLinkTypeahead(document.getElementById('transfer-line-sku'), 'Item');
}

function renderTransferLinesList() {
  const el = document.getElementById('transfer-lines-list');
  if (!el) return;
  if (transferLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = transferLineItems.map((line, idx) => `
    <div style="display: flex; align-items: center; gap: 12px; padding: 6px 0; font-size: 13.5px;">
      <span data-link-doctype="Item" data-link-ref="${escapeHTMLText(line.sku)}">${escapeHTMLText(line.sku)}</span>
      <span>qty ${line.qty}</span>
      <button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeTransferLine', [idx])}>Remove</button>
    </div>
  `).join('');
}

function addTransferLine() {
  const skuEl = document.getElementById('transfer-line-sku');
  const qtyEl = document.getElementById('transfer-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku || !qty || qty <= 0) return;
  transferLineItems.push({ sku, qty });
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
      items: JSON.stringify(transferLineItems), status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create transfer.';
    errorEl.classList.remove('hidden');
    return;
  }
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
  dispatchTransferOrder,
  receiveTransferOrder
};

function renderTransferActions(t) {
  if (t.status === 'Draft') {
    return `<button class="action-btn" ${actionAttrs('approveTransferOrder', [t.id])}>Mark Approved</button>`;
  }
  if (t.status === 'Approved') {
    // Pack (Stage 20.19) is an optional confirmation step, not a required
    // gate - Dispatch stays available directly from Approved too. Stage
    // 26.5.8 adds a second pack path that suggests a carton split instead
    // of prompting box-by-box.
    return `<button class="action-btn" ${actionAttrs('packTransferOrder', [t.id])}>Pack</button> <button class="action-btn" ${actionAttrs('packTransferOrderWithCartonization', [t.id])}>Pack (Suggested Cartons)</button> <button class="action-btn" ${actionAttrs('dispatchTransferOrder', [t.id])}>Dispatch</button>`;
  }
  if (t.status === 'Packed') {
    return `<button class="action-btn" ${actionAttrs('dispatchTransferOrder', [t.id])}>Dispatch</button>`;
  }
  if (t.status === 'Dispatched') {
    return `<button class="action-btn" ${actionAttrs('receiveTransferOrder', [t.id])}>Receive</button>`;
  }
  return '';
}

// Prompts for a Box ID per line item (same sequential-prompt pattern
// receiveTransferOrder below uses for received qty), grouping items that
// share a Box ID into one box before submitting - covers both "one box per
// SKU" and "everything in one box" without a bespoke multi-box form.
async function packTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
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
  const row = state.docData.find(t => t.id === id);
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

// The generic doc engine's POST-with-id update replaces the whole `data`
// blob (no JSONB merge - see internal/server/handlers_core_doc_engine.go's
// `data = EXCLUDED.data`), so this resends every field from the
// already-loaded row rather than just {status: ...}, or a status-only
// payload would silently wipe transfer_number/from_warehouse/to_warehouse/items.
async function approveTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  const res = await apiFetch(`/api/v1/doc/TransferOrder/${encodeURIComponent(id)}`, {
    method: 'POST',
    body: JSON.stringify({ ...row, status: 'Approved' })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to approve transfer.');
    return;
  }
  renderView('transfers');
}

async function dispatchTransferOrder(id) {
  if (!(await showCustomConfirm('Dispatch this transfer? Stock will move from the source location into transit.', 'Dispatch Transfer'))) return;
  const res = await apiFetch('/api/v1/transfer/dispatch', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to dispatch transfer.', 'Dispatch Failed');
    return;
  }
  renderView('transfers');
}

// Prompts for the received quantity of each dispatched line (sequentially,
// via the same showCustomPrompt dialog the rest of the app uses), defaulting
// to the full dispatched qty - covers both the common full-receipt case and
// a genuine partial/shortage receipt in one flow without a bespoke modal.
async function receiveTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  let dispatchedLines = [];
  try { dispatchedLines = JSON.parse(row.dispatched_items || row.items || '[]'); } catch (e) { dispatchedLines = []; }
  if (dispatchedLines.length === 0) {
    await showCustomAlert('No dispatched line items found on this transfer.', 'Error');
    return;
  }

  const receivedItems = [];
  for (const line of dispatchedLines) {
    const qtyStr = await showCustomPrompt(`Quantity received for ${line.sku} (dispatched ${line.qty}):`, String(line.qty));
    if (qtyStr === null) return;
    const qty = parseInt(qtyStr, 10);
    const item = { sku: line.sku, qty: isNaN(qty) ? 0 : qty };
    // TRN-0259: the server refuses to save a short receipt with no reason -
    // ask for one here, rather than the whole receive failing with nowhere
    // on this screen to ever enter it.
    if (item.qty < line.qty) {
      const reason = await showCustomPrompt(`Reason for the shortfall on ${line.sku} (received ${item.qty} of ${line.qty}):`, '');
      if (reason === null) return;
      item.reason = reason;
    }
    receivedItems.push(item);
  }

  const res = await apiFetch('/api/v1/transfer/receive', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, received_items: receivedItems })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to receive transfer.', 'Receive Failed');
    return;
  }
  renderView('transfers');
}
