// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
async function renderFulfillmentView(container) {
  const res = await apiFetch('/api/v1/doc/FulfillmentTask');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Fulfillment</h1>
      <p class="page-subtitle">Pick, pack, and dispatch tasks routed to your location.</p>
    </div>
  `;
  container.appendChild(header);

  if (!res.ok) {
    // BLD-036: was a dead-end static message under the header with no retry -
    // matches the same gap already fixed on Approvals.
    renderErrorPanel(container, 'Failed to load fulfillment tasks.', () => renderView('fulfillment'));
    return;
  }

  const tasks = await res.json();
  // 2026-10-09: a picker identifies an order by who it is for, not by its
  // generated id - one read of the orders, mapped by id, labels every row
  // (the id stays, small, for anyone who needs to quote it).
  const orderInfo = {};
  if ((tasks || []).some(t => t.order_id)) {
    const ordersRes = await apiFetch('/api/v1/doc/SalesOrder?limit=500');
    if (ordersRes && ordersRes.ok) {
      for (const o of await ordersRes.json()) orderInfo[o.code || o.id] = o;
    }
  }
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Task ID</th>
          <th>Order</th>
          <th>Location</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (!tasks || tasks.length === 0) {
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No fulfillment tasks routed to your location. Tasks appear automatically when a Sales Order is released under <b>Order Management</b>.</td></tr>`;
  }
  (tasks || []).forEach(t => {
    const badgeClass = FULFILLMENT_STATUS_BADGE[t.status] || 'badge-secondary';
    html += `
      <tr>
        <td style="font-family: monospace;">${t.code || t.id}</td>
        <td>${fulfillmentOrderLabel(t.order_id, orderInfo[t.order_id])}</td>
        <td>${t.location_code || ''}</td>
        <td><span class="badge ${badgeClass}">${t.status}</span></td>
        <td>${renderFulfillmentActions(t)}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

function fulfillmentOrderLabel(orderID, order) {
  if (!orderID) return '';
  if (!order) return escapeHTMLText(orderID);
  const who = order.customer_name || '';
  const ref = order.channel_order_id ? ` · ${order.channel_order_id}` : '';
  return `${escapeHTMLText(who || orderID)}<div style="font-size: 11.5px; color: var(--text-muted);">${escapeHTMLText((order.channel || 'Manual') + ref)}</div><div style="font-size: 11px; color: var(--text-muted); font-family: monospace;">${escapeHTMLText(orderID)}</div>`;
}

function renderFulfillmentActions(task) {
  const id = task.code || task.id;
  switch (task.status) {
    case 'Pending':
      return `
        <button class="action-btn" ${actionAttrs('transitionFulfillmentTask', [id, 'Picking'])}>Start Picking</button>
        <button class="action-btn action-btn-danger" ${actionAttrs('transitionFulfillmentTask', [id, 'Rejected'])}>Reject</button>
        <button class="action-btn" ${actionAttrs('viewPickList', [id])}>View Pick List</button>
      `;
    case 'Picking':
      return `
        <button class="action-btn" ${actionAttrs('transitionFulfillmentTask', [id, 'Packed'])}>Mark Packed</button>
        <button class="action-btn action-btn-danger" ${actionAttrs('transitionFulfillmentTask', [id, 'Rejected'])}>Reject</button>
        <button class="action-btn" ${actionAttrs('viewPickList', [id])}>View Pick List</button>
      `;
    case 'Packed':
      return `<button class="action-btn" ${actionAttrs('transitionFulfillmentTask', [id, 'Dispatched'])}>Dispatch</button>`;
    default:
      return '';
  }
}

async function transitionFulfillmentTask(taskId, newStatus) {
  const res = await apiFetch('/api/v1/fulfillment/task/transition', {
    method: 'POST',
    body: JSON.stringify({ task_id: taskId, status: newStatus })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update task status.');
    return;
  }
  renderView('fulfillment');
}

// viewPickList (Stage 26.3.4) shows GenerateBinPickList's bin-grouped,
// walk-route-sorted result for one FulfillmentTask in a lightweight
// Stage 42.1.5: one shared renderer for the "which lot, and how urgent" cell,
// used by every pick surface (task pick list, wave pick list, mobile picking)
// so a picker reads the same thing in the same colours wherever they are.
//
// The urgency badge is the whole point of showing the expiry at all: FEFO has
// already put the earliest-expiry lot first, and the colour is what tells a
// picker whether the line in front of them is routine or needs a supervisor.
// Days are computed client-side from the date the server sent, so this stays a
// pure render helper with no extra round trip.
// read-only modal, reusing the same .modal-overlay/.modal-container
// primitives as viewTaxonomyHistory instead of introducing a new one.
window.viewPickList = async function(taskId) {
  const res = await apiFetch(`/api/v1/wms/pick-list?task_id=${encodeURIComponent(taskId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the pick list for this task.');
    return;
  }
  const lines = await res.json();

  document.getElementById('pick-list-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pick-list-modal';
  // Stage 42.1.5: the Batch/Expiry column only appears when the list actually
  // carries lots, so a warehouse with no batch-tracked item sees exactly the
  // six-column table it saw before.
  const hasBatches = (lines || []).some(l => l.batch_no);
  const rows = (!lines || lines.length === 0)
    ? `<tr><td colspan="${hasBatches ? 7 : 6}" class="text-center text-muted">No bin-level pick lines for this task &mdash; it will be picked from general stock instead. Continue as normal.</td></tr>`
    : lines.map(l => {
        const short = l.shortfall > 0
          ? `<span class="badge badge-danger">Short ${l.shortfall}</span>`
          : '';
        return `<tr><td>${l.sku || ''}</td>${hasBatches ? `<td>${batchCellHTML(l)}</td>` : ''}<td>${l.bin_code || ''}</td><td>${l.zone || ''}</td><td>${l.aisle || ''}</td><td>${l.rack || ''}</td><td>${l.pick_qty || 0} ${short}</td></tr>`;
      }).join('');
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Pick List: ${taskId}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><div class="table-wrapper"><table><thead><tr><th>SKU</th>${hasBatches ? '<th>Batch / Expiry</th>' : ''}<th>Bin</th><th>Zone</th><th>Aisle</th><th>Rack</th><th>Pick Qty</th></tr></thead><tbody>${rows}</tbody></table></div></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
};

// WMS operations screens (Stage 26.3.4) - engines/wms.go's putaway, bin
// condition transitions, and cycle-count reconciliation (Stage 20 Track B.2)
// have been real, routed, working backend endpoints since Stage 20 with zero
// frontend anywhere. These three screens are pure UI on top of that existing
// backend - no new engine code, doctype, or migration needed.

async function renderPutawayView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Putaway</h1>
      <p class="page-subtitle">Place accepted stock into a bin. Refuses more than the location's unassigned on-hand quantity.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-bin">Bin Code</label>
        <input type="text" id="putaway-bin" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-sku">SKU</label>
        <input type="text" id="putaway-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-qty">Qty</label>
        <input type="number" id="putaway-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-location">Location (for Suggest Bin)</label>
        <input type="text" id="putaway-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="putaway-suggest-btn" type="button">Suggest Bin</button>
      <button class="btn btn-primary" id="putaway-submit-btn" type="button">Put Away</button>
    </div>
    <div id="putaway-suggest-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="putaway-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);

  document.getElementById('putaway-submit-btn').addEventListener('click', submitPutaway);
  document.getElementById('putaway-suggest-btn').addEventListener('click', suggestPutawayBin);
  attachLinkTypeahead(document.getElementById('putaway-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('putaway-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('putaway-location'), 'Location');

  // Stage 26.5.3: cross-dock/flow-through putaway - an alternative to
  // shelving when a transfer/sale is already waiting on this exact SKU at
  // this location, skipping bin placement in favor of a staging bin.
  const xdockPanel = document.createElement('div');
  xdockPanel.className = 'table-panel';
  xdockPanel.style.padding = '24px';
  xdockPanel.style.marginTop = '24px';
  xdockPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Cross-Dock Staging</h2>
    <p style="color: var(--text-muted); margin-bottom: 12px;">Check whether an open transfer/sale is already waiting on a SKU before shelving it - if so, stage it for immediate outbound instead.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-sku">SKU</label>
        <input type="text" id="xdock-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-location">Location</label>
        <input type="text" id="xdock-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-qty">Qty on hand to place</label>
        <input type="number" id="xdock-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-outline" id="xdock-check-btn" type="button">Check Opportunity</button>
      <button class="btn btn-primary" id="xdock-stage-btn" type="button">Stage for Cross-Dock</button>
    </div>
    <div id="xdock-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="xdock-form-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(xdockPanel);
  document.getElementById('xdock-check-btn').addEventListener('click', checkCrossDockOpportunity);
  document.getElementById('xdock-stage-btn').addEventListener('click', submitCrossDockPutaway);
  attachLinkTypeahead(document.getElementById('xdock-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('xdock-location'), 'Location');

  // Stage 42.3.8: planned cross-dock/flow-through/transship - stages against
  // a CrossDockPlan raised ahead of receipt (Cross-Dock Plans in the sidebar)
  // instead of scanning for live opportunistic demand like the panel above.
  const planPanel = document.createElement('div');
  planPanel.className = 'table-panel';
  planPanel.style.padding = '24px';
  planPanel.style.marginTop = '24px';
  planPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Planned Cross-Dock / Flow-Through / Transship</h2>
    <p style="color: var(--text-muted); margin-bottom: 12px;">Stages against a Cross-Dock Plan raised ahead of receipt (see Cross-Dock Plans), rather than scanning for live demand.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xplan-sku">SKU</label>
        <input type="text" id="xplan-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xplan-location">Location</label>
        <input type="text" id="xplan-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xplan-qty">Qty on hand to stage</label>
        <input type="number" id="xplan-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-primary" id="xplan-stage-btn" type="button">Stage Against Plan</button>
    </div>
    <div id="xplan-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="xplan-form-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(planPanel);
  document.getElementById('xplan-stage-btn').addEventListener('click', submitPlannedCrossDockPutaway);
  attachLinkTypeahead(document.getElementById('xplan-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('xplan-location'), 'Location');
}

async function submitPlannedCrossDockPutaway() {
  const errorEl = document.getElementById('xplan-form-error');
  const resultEl = document.getElementById('xplan-result');
  errorEl.classList.add('hidden');
  const sku = document.getElementById('xplan-sku').value.trim();
  const location = document.getElementById('xplan-location').value.trim();
  const qty = parseInt(document.getElementById('xplan-qty').value, 10);
  if (!sku || !location || !qty || qty <= 0) {
    errorEl.textContent = 'SKU, Location, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/cross-dock/planned-putaway', {
    method: 'POST',
    body: JSON.stringify({ sku, location_code: location, qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to stage against a cross-dock plan.', 'Planned Cross-Dock Failed');
    return;
  }
  const data = await res.json();
  resultEl.textContent = `Staged ${data.staged} x ${sku} against plan ${data.plan_id}.`;
}

async function checkCrossDockOpportunity() {
  const resultEl = document.getElementById('xdock-result');
  const sku = document.getElementById('xdock-sku').value.trim();
  const location = document.getElementById('xdock-location').value.trim();
  if (!sku || !location) { resultEl.textContent = 'Enter a SKU and Location first.'; return; }
  const res = await apiFetch('/api/v1/wms/cross-dock/check', {
    method: 'POST',
    body: JSON.stringify({ sku, location_code: location })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to check cross-dock opportunity.', 'Check Failed'); return; }
  const data = await res.json();
  if (data.matched_qty > 0) {
    resultEl.innerHTML = `<span class="badge badge-success">Matched ${data.matched_qty} unit(s)</span> across ${data.opportunities.length} open order(s) - eligible for cross-dock.`;
  } else {
    resultEl.textContent = 'No open transfer/sale is waiting on this SKU here - use ordinary Putaway above instead.';
  }
}

async function submitCrossDockPutaway() {
  const errorEl = document.getElementById('xdock-form-error');
  errorEl.classList.add('hidden');
  const resultEl = document.getElementById('xdock-result');
  const sku = document.getElementById('xdock-sku').value.trim();
  const location = document.getElementById('xdock-location').value.trim();
  const qty = parseInt(document.getElementById('xdock-qty').value, 10);
  if (!sku || !location || !qty || qty <= 0) {
    errorEl.textContent = 'SKU, Location, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/cross-dock/putaway', {
    method: 'POST',
    body: JSON.stringify({ sku, location_code: location, qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to stage for cross-dock.', 'Cross-Dock Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Staged ${data.staged} unit(s)</span> for cross-dock.`;
}

// suggestPutawayBin (42.2.7) calls the directed-putaway suggestion endpoint
// and fills the Bin Code field when one comes back - a pure convenience, it
// never blocks manual entry, and a "no suggestion" response is shown as
// plain text rather than an error (SuggestPutawayBin's own contract: no
// configured strategy or no eligible bin are both real, expected outcomes).
async function suggestPutawayBin() {
  const resultEl = document.getElementById('putaway-suggest-result');
  const sku = document.getElementById('putaway-sku').value.trim();
  const location = document.getElementById('putaway-location').value.trim();
  const qty = parseInt(document.getElementById('putaway-qty').value, 10) || 1;
  if (!sku || !location) {
    resultEl.textContent = 'Enter a SKU and Location first.';
    return;
  }
  const params = new URLSearchParams({ sku, location_code: location, qty: String(qty) });
  const res = await apiFetch(`/api/v1/wms/putaway/suggest-bin?${params}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to suggest a bin.', 'Suggestion Failed'); return; }
  const data = await res.json();
  if (data.bin_code) {
    document.getElementById('putaway-bin').value = data.bin_code;
    resultEl.innerHTML = `<span class="badge badge-success">Suggested bin ${data.bin_code}</span> - ${data.reason}`;
  } else {
    resultEl.textContent = data.reason || 'No suggestion available - enter a bin manually.';
  }
}

async function submitPutaway() {
  const errorEl = document.getElementById('putaway-form-error');
  errorEl.classList.add('hidden');

  const binCode = document.getElementById('putaway-bin').value.trim();
  const sku = document.getElementById('putaway-sku').value.trim();
  const qty = parseInt(document.getElementById('putaway-qty').value, 10);

  if (!binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/putaway', {
    method: 'POST',
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to put away stock.', 'Putaway Failed');
    return;
  }
  await showCustomAlert(`Put away ${qty} x ${sku} into bin ${binCode}.`, 'Putaway Complete');
  document.getElementById('putaway-qty').value = 1;
}

// Stage 42.3.5 - Place Hold: the only creation path for a Hold document
// (generic create is blocked by role_permissions, see
// db/migrations_stage42_3_5_holdcode.sql) - same single-panel-form shape as
// Putaway above, posting to its own action endpoint instead of the generic
// doc API.
async function renderPlaceHoldView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Place Hold</h1>
      <p class="page-subtitle">Immediately blocks qty of a SKU at a location from allocation. Release requires a Hold Release Request and Store Manager approval.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-code">Hold Code</label>
        <input type="text" id="hold-code" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-sku">SKU</label>
        <input type="text" id="hold-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-location">Location</label>
        <input type="text" id="hold-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-batch">Batch / Lot No (optional)</label>
        <input type="text" id="hold-batch" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-qty">Qty</label>
        <input type="number" id="hold-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hold-reason">Reason (optional)</label>
        <input type="text" id="hold-reason" class="form-input" style="width: 220px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="hold-submit-btn" type="button">Place Hold</button>
    </div>
    <div id="hold-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);

  document.getElementById('hold-submit-btn').addEventListener('click', submitPlaceHold);
  attachLinkTypeahead(document.getElementById('hold-code'), 'HoldCode');
  attachLinkTypeahead(document.getElementById('hold-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('hold-location'), 'Location');
}

async function submitPlaceHold() {
  const errorEl = document.getElementById('hold-form-error');
  errorEl.classList.add('hidden');

  const holdCode = document.getElementById('hold-code').value.trim();
  const sku = document.getElementById('hold-sku').value.trim();
  const locationCode = document.getElementById('hold-location').value.trim();
  const batchNo = document.getElementById('hold-batch').value.trim();
  const qty = parseInt(document.getElementById('hold-qty').value, 10);
  const reason = document.getElementById('hold-reason').value.trim();

  if (!holdCode || !sku || !locationCode || !qty || qty <= 0) {
    errorEl.textContent = 'Hold Code, SKU, Location, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/hold/place', {
    method: 'POST',
    body: JSON.stringify({ hold_code: holdCode, sku: sku, location_code: locationCode, batch_no: batchNo, qty: qty, reason: reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to place hold.', 'Place Hold Failed');
    return;
  }
  await showCustomAlert(`Placed a hold of ${qty} x ${sku} at ${locationCode}.`, 'Hold Placed');
  document.getElementById('hold-qty').value = 1;
  document.getElementById('hold-reason').value = '';
}

// Stage 42.3.4 - Yard Board: check-in/check-out for trailers, InYard ->
// AtDoor -> Departed (validateYardCheckInMasterRules enforces the order
// server-side). Infor's 3D yard view is explicitly out of scope - this is a
// flat status board, not a spatial map.
async function renderYardBoardView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Yard Board</h1>
      <p class="page-subtitle">Check a trailer into the yard, spot it at a door, and check it out when it departs.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="yard-trailer">Trailer No</label>
        <input type="text" id="yard-trailer" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="yard-carrier">Carrier (optional)</label>
        <input type="text" id="yard-carrier" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="yard-driver">Driver (optional)</label>
        <input type="text" id="yard-driver" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="yard-slot">Yard Slot (optional)</label>
        <input type="text" id="yard-slot" class="form-input" style="width: 110px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="yard-checkin-btn" type="button">Check In</button>
    </div>
    <div id="yard-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);
  document.getElementById('yard-checkin-btn').addEventListener('click', submitYardCheckIn);
  attachLinkTypeahead(document.getElementById('yard-trailer'), 'Trailer', { valueFields: ['code'] });

  const body = document.createElement('div');
  body.id = 'yard-board-body';
  container.appendChild(body);
  await loadYardBoard();
}

async function loadYardBoard(liveBody = null) {
  const body = liveBody?.isConnected ? liveBody : document.getElementById('yard-board-body');
  if (!body) return;
  body.innerHTML = '<div class="table-panel" style="padding:24px;">Loading yard status…</div>';
  const res = await apiFetch('/api/v1/doc/YardCheckIn');
  if (!res) return;
  if (!res.ok) {
    // BLD-036: the modal used to close onto a body stuck on "Loading yard
    // status..." forever, with no retry short of navigating away and back.
    renderErrorPanel(body, 'Failed to load yard status.', retryBody => loadYardBoard(retryBody));
    return;
  }
  const records = await res.json();
  const active = (records || []).filter(r => r.status !== 'Departed').sort((a, b) => (a.trailer_no || '').localeCompare(b.trailer_no || ''));

  if (active.length === 0) {
    body.innerHTML = '<div class="table-panel" style="padding:24px; color:var(--text-muted);">No trailers currently in the yard.</div>';
    return;
  }

  const rows = active.map(r => `
    <tr>
      <td>${escapeHTMLText(r.trailer_no || '')}</td>
      <td>${escapeHTMLText(r.carrier || '-')}</td>
      <td>${escapeHTMLText(r.driver_name || '-')}</td>
      <td><span class="badge">${escapeHTMLText(r.status || '')}</span></td>
      <td>${escapeHTMLText(r.dock_door || r.yard_location || '-')}</td>
      <td>
        ${r.status === 'InYard' ? `<button class="btn btn-outline btn-sm" ${actionAttrs('assignYardDoor', [r.id])}>Assign to Door</button>` : ''}
        <button class="btn btn-outline btn-sm" ${actionAttrs('checkOutYardTrailer', [r.id])}>Check Out</button>
      </td>
    </tr>`).join('');

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-wrapper"><table>
      <thead><tr><th>Trailer</th><th>Carrier</th><th>Driver</th><th>Status</th><th>Door / Slot</th><th>Actions</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  `;
  body.innerHTML = '';
  body.appendChild(panel);
}

async function submitYardCheckIn() {
  // BLD-036: guard against a double-click creating two YardCheckIn records.
  await guardAgainstDoubleSubmit(document.getElementById('yard-checkin-btn'), 'Checking in...', submitYardCheckInInner);
}

async function submitYardCheckInInner() {
  const errorEl = document.getElementById('yard-form-error');
  errorEl.classList.add('hidden');
  const trailerNo = document.getElementById('yard-trailer').value.trim();
  if (!trailerNo) {
    errorEl.textContent = 'Trailer No is required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const payload = {
    trailer_no: trailerNo,
    carrier: document.getElementById('yard-carrier').value.trim(),
    driver_name: document.getElementById('yard-driver').value.trim(),
    yard_location: document.getElementById('yard-slot').value.trim(),
    status: 'InYard'
  };
  const res = await apiFetch('/api/v1/doc/YardCheckIn', { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to check in trailer.');
    return;
  }
  document.getElementById('yard-trailer').value = '';
  document.getElementById('yard-carrier').value = '';
  document.getElementById('yard-driver').value = '';
  document.getElementById('yard-slot').value = '';
  await loadYardBoard();
}

// The generic doc endpoint replaces the whole `data` blob on save (the same
// full-record resend editDocRecord/handleDynamicFormSubmit already do for
// every other doctype's edit form) - so both actions below fetch the
// current record first and patch it, rather than posting just the two
// fields that changed, which would silently blank every other field.
async function patchYardCheckIn(id, patch) {
  const getRes = await apiFetch(`/api/v1/doc/YardCheckIn/${encodeURIComponent(id)}`);
  if (!getRes) return null;
  if (!getRes.ok) {
    await showApiError(getRes, 'Failed to load yard check-in record.');
    return null;
  }
  const record = await getRes.json();
  Object.assign(record, patch);
  return apiFetch(`/api/v1/doc/YardCheckIn/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify(record) });
}

window.assignYardDoor = async function(id) {
  const dockDoor = await showCustomPrompt('Assign to which dock door?');
  if (!dockDoor) return;
  const res = await patchYardCheckIn(id, { status: 'AtDoor', dock_door: dockDoor.trim() });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to assign door.');
    return;
  }
  await loadYardBoard();
};

window.checkOutYardTrailer = async function(id) {
  const res = await patchYardCheckIn(id, { status: 'Departed', checked_out_at: new Date().toISOString() });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to check out trailer.');
    return;
  }
  await loadYardBoard();
};

// Stage 42.3.2/42.3.3 - Appointment scheduling + calendar. Day view lays
// appointments out on a horizontal timeline per door (06:00-22:00, plain
// CSS flex positioning - no calendar library, per the plan). Week view
// trades the timeline for a door x day grid of chips, since positioning by
// time-of-day stops being legible at 7-day zoom.
let calendarDate = localISODate(new Date());
let calendarWeekView = false;

async function renderAppointmentCalendarView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Appointment Calendar</h1>
      <p class="page-subtitle">Inbound/outbound dock appointments, scheduled against each door's capacity and service window.</p>
    </div>
  `;
  container.appendChild(header);

  const controls = document.createElement('div');
  controls.className = 'table-panel';
  controls.style.padding = '16px 24px';
  controls.style.marginBottom = '24px';
  controls.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap;">
      <button class="btn btn-outline" id="cal-prev-btn" type="button">&larr;</button>
      <input type="date" id="cal-date" class="form-input" style="width: 160px;" value="${calendarDate}">
      <button class="btn btn-outline" id="cal-next-btn" type="button">&rarr;</button>
      <button class="btn btn-outline" id="cal-view-toggle" type="button">${calendarWeekView ? 'Switch to Day' : 'Switch to Week'}</button>
      <div style="flex:1;"></div>
      <button class="btn btn-primary" id="cal-new-btn" type="button">+ New Appointment</button>
    </div>
    <div id="cal-new-form" class="hidden" style="margin-top:16px; padding-top:16px; border-top:1px solid var(--border-color); display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-door">Dock Door</label><input type="text" id="cal-door" class="form-input" style="width:120px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-type">Type</label>
        <select id="cal-type" class="form-select" style="width:120px;"><option value="Inbound">Inbound</option><option value="Outbound">Outbound</option></select>
      </div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-carrier">Carrier</label><input type="text" id="cal-carrier" class="form-input" style="width:120px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-trailer">Trailer No</label><input type="text" id="cal-trailer" class="form-input" style="width:120px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-appt-date">Date</label><input type="date" id="cal-appt-date" class="form-input" style="width:150px;" value="${calendarDate}"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-start">Start</label><input type="time" id="cal-start" class="form-input" style="width:120px;" value="09:00"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-end">End</label><input type="time" id="cal-end" class="form-input" style="width:120px;" value="10:00"></div>
      <button class="btn btn-primary" id="cal-save-btn" type="button">Save</button>
    </div>
    <div id="cal-form-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(controls);

  const body = document.createElement('div');
  body.id = 'cal-body';
  container.appendChild(body);

  document.getElementById('cal-date').addEventListener('change', (e) => { calendarDate = e.target.value; loadAppointmentCalendar(); });
  document.getElementById('cal-prev-btn').addEventListener('click', () => shiftCalendarDate(calendarWeekView ? -7 : -1));
  document.getElementById('cal-next-btn').addEventListener('click', () => shiftCalendarDate(calendarWeekView ? 7 : 1));
  document.getElementById('cal-view-toggle').addEventListener('click', () => { calendarWeekView = !calendarWeekView; renderView('appointment-calendar'); });
  document.getElementById('cal-new-btn').addEventListener('click', () => { document.getElementById('cal-new-form').classList.toggle('hidden'); });
  document.getElementById('cal-save-btn').addEventListener('click', submitNewAppointment);
  attachLinkTypeahead(document.getElementById('cal-door'), 'DockDoor', { valueFields: ['code'] });

  await loadAppointmentCalendar();
}

function shiftCalendarDate(days) {
  const d = new Date(calendarDate + 'T00:00:00');
  d.setDate(d.getDate() + days);
  calendarDate = localISODate(d);
  document.getElementById('cal-date').value = calendarDate;
  loadAppointmentCalendar();
}

function calendarWeekDates() {
  const d = new Date(calendarDate + 'T00:00:00');
  d.setDate(d.getDate() - d.getDay());
  const out = [];
  for (let i = 0; i < 7; i++) {
    out.push(localISODate(d));
    d.setDate(d.getDate() + 1);
  }
  return out;
}

async function loadAppointmentCalendar() {
  const body = document.getElementById('cal-body');
  if (!body) return;
  body.innerHTML = '<div class="table-panel" style="padding:24px;">Loading appointments…</div>';

  const [doorsRes, apptRes] = await Promise.all([
    apiFetch('/api/v1/doc/DockDoor'),
    apiFetch('/api/v1/doc/Appointment')
  ]);
  if (!doorsRes || !apptRes) return;
  if (!doorsRes.ok || !apptRes.ok) {
    await showApiError(!doorsRes.ok ? doorsRes : apptRes, 'Failed to load calendar data.');
    return;
  }
  const doors = (await doorsRes.json()).filter(d => d.status === 'Active').sort((a, b) => (a.code || '').localeCompare(b.code || ''));
  const allAppts = await apptRes.json();

  if (doors.length === 0) {
    body.innerHTML = '<div class="table-panel" style="padding:24px; color:var(--text-muted);">No Active dock doors - create one under Dock Doors first.</div>';
    return;
  }

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '16px';
  panel.style.overflowX = 'auto';

  if (calendarWeekView) {
    const dates = calendarWeekDates();
    let html = '<table style="min-width:900px;"><thead><tr><th>Door</th>' + dates.map(d => `<th>${d}</th>`).join('') + '</tr></thead><tbody>';
    for (const door of doors) {
      html += `<tr><td><strong>${escapeHTMLText(door.code)}</strong></td>`;
      for (const date of dates) {
        const dayAppts = allAppts.filter(a => a.dock_door === door.code && a.appointment_date === date && a.status !== 'Cancelled');
        html += `<td style="vertical-align:top; min-width:110px;">${dayAppts.map(a => apptChip(a)).join('')}</td>`;
      }
      html += '</tr>';
    }
    html += '</tbody></table>';
    panel.innerHTML = html;
  } else {
    const hours = [];
    for (let h = 6; h <= 22; h++) hours.push(h);
    let html = '<div style="display:flex; flex-direction:column; gap:6px;">';
    html += '<div style="display:flex;"><div style="width:100px;"></div>' + hours.map(h => `<div style="flex:1; font-size:11px; color:var(--text-muted); text-align:center;">${String(h).padStart(2, '0')}:00</div>`).join('') + '</div>';
    for (const door of doors) {
      const dayAppts = allAppts.filter(a => a.dock_door === door.code && a.appointment_date === calendarDate && a.status !== 'Cancelled');
      html += `<div style="display:flex; align-items:center; border-top:1px solid var(--border-color); padding-top:6px;">
        <div style="width:100px; font-weight:600; font-size:13px;">${escapeHTMLText(door.code)}</div>
        <div style="position:relative; flex:1; height:32px; background:var(--bg-subtle, #f5f5f7); border-radius:4px;">`;
      for (const a of dayAppts) {
        const startMin = timeToMinutes(a.start_time), endMin = timeToMinutes(a.end_time);
        const rangeStart = 6 * 60, rangeEnd = 22 * 60;
        const leftPct = Math.max(0, (startMin - rangeStart) / (rangeEnd - rangeStart) * 100);
        const widthPct = Math.max(2, (endMin - startMin) / (rangeEnd - rangeStart) * 100);
        html += `<div title="${escapeHTMLText(a.carrier || '')} ${escapeHTMLText(a.trailer_no || '')} ${escapeHTMLText(a.start_time)}-${escapeHTMLText(a.end_time)}"
          style="position:absolute; left:${leftPct}%; width:${widthPct}%; top:2px; bottom:2px; background:var(--accent, #4f46e5); color:#fff; font-size:10px; border-radius:3px; overflow:hidden; padding:2px 4px; white-space:nowrap;">
          ${escapeHTMLText(a.appointment_type === 'Outbound' ? 'OUT' : 'IN')} ${escapeHTMLText(a.trailer_no || a.carrier || '')}</div>`;
      }
      html += '</div></div>';
    }
    html += '</div>';
    panel.innerHTML = html;
  }
  body.innerHTML = '';
  body.appendChild(panel);
}

function timeToMinutes(hhmm) {
  const [h, m] = (hhmm || '0:0').split(':').map(n => parseInt(n, 10) || 0);
  return h * 60 + m;
}

function apptChip(a) {
  const color = a.appointment_type === 'Outbound' ? '#9333ea' : '#4f46e5';
  return `<div style="background:${color}; color:#fff; font-size:10px; border-radius:3px; padding:2px 4px; margin-bottom:3px;">${escapeHTMLText(a.start_time || '')} ${escapeHTMLText(a.trailer_no || a.carrier || '')}</div>`;
}

async function submitNewAppointment() {
  // BLD-036: guard against a double-click creating two Appointment records.
  await guardAgainstDoubleSubmit(document.getElementById('cal-save-btn'), 'Saving...', submitNewAppointmentInner);
}

async function submitNewAppointmentInner() {
  const errorEl = document.getElementById('cal-form-error');
  errorEl.classList.add('hidden');
  const payload = {
    dock_door: document.getElementById('cal-door').value.trim(),
    appointment_type: document.getElementById('cal-type').value,
    carrier: document.getElementById('cal-carrier').value.trim(),
    trailer_no: document.getElementById('cal-trailer').value.trim(),
    appointment_date: document.getElementById('cal-appt-date').value,
    start_time: document.getElementById('cal-start').value.trim(),
    end_time: document.getElementById('cal-end').value.trim(),
    status: 'Scheduled'
  };
  if (!payload.dock_door || !payload.appointment_date || !payload.start_time || !payload.end_time) {
    errorEl.textContent = 'Dock Door, Date, Start and End time are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/doc/Appointment', { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to schedule appointment.');
    return;
  }
  document.getElementById('cal-new-form').classList.add('hidden');
  document.getElementById('cal-carrier').value = '';
  document.getElementById('cal-trailer').value = '';
  document.getElementById('cal-start').value = '';
  document.getElementById('cal-end').value = '';
  await loadAppointmentCalendar();
}

// Stage 42.3.10 - RF Receiving: scan-driven receiving against an ASN,
// mirroring Mobile Picking's one-card, big-target shape and POS's
// scan-then-Enter input convention (this codebase's barcodes encode the SKU
// code directly - see addSKUToPOSCart - so a scan resolves against
// expected_items[].sku with no separate barcode->SKU lookup step). Deliberately
// qty-confirm only: batch/serial/catch-weight/dimension capture (42.1.4,
// 42.1.8, 42.3.7) stay on the desktop GRN Workbench, the same scope boundary
// Infor's own RF receiving screens draw - an RF gun confirms quantities fast,
// a supervisor desk handles exceptions.
let rfReceivingExpected = [];
let rfReceivingLines = [];
let rfReceivingASNId = '';
let rfReceivingPOId = '';

async function renderRFReceivingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">RF Receiving</h1>
      <p class="page-subtitle">Scan an ASN, then scan each carton's SKU to confirm it against what's expected. Batch/serial/catch-weight capture stays on the GRN Workbench.</p>
    </div>
  `;
  container.appendChild(header);

  const setupPanel = document.createElement('div');
  setupPanel.className = 'table-panel';
  setupPanel.style.padding = '20px';
  setupPanel.style.marginBottom = '16px';
  setupPanel.style.maxWidth = '480px';
  setupPanel.innerHTML = `
    <div class="form-group" style="margin-bottom: 12px;">
      <label class="form-label" for="rf-asn-input">Scan or Enter ASN</label>
      <input type="text" id="rf-asn-input" class="form-input" placeholder="ASN number, then Enter" autocomplete="off" style="font-size:18px; padding:14px;">
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="rf-location-input">Receiving Location</label>
      <input type="text" id="rf-location-input" class="form-input" autocomplete="off">
    </div>
    <div id="rf-setup-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(setupPanel);
  document.getElementById('rf-asn-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); loadRFReceivingASN(); } });
  attachLinkTypeahead(document.getElementById('rf-location-input'), 'Location');

  const body = document.createElement('div');
  body.id = 'rf-receiving-body';
  body.style.maxWidth = '480px';
  container.appendChild(body);

  rfReceivingExpected = [];
  rfReceivingLines = [];
  rfReceivingASNId = '';
  rfReceivingPOId = '';
}

async function loadRFReceivingASN() {
  const errorEl = document.getElementById('rf-setup-error');
  errorEl.classList.add('hidden');
  const asnId = document.getElementById('rf-asn-input').value.trim();
  if (!asnId) return;

  const res = await apiFetch(`/api/v1/doc/ASN/${encodeURIComponent(asnId)}`);
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = `ASN ${asnId} not found.`;
    errorEl.classList.remove('hidden');
    return;
  }
  const asn = await res.json();
  let items = [];
  try { items = JSON.parse(asn.expected_items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    errorEl.textContent = `ASN ${asnId} has no recorded expected items.`;
    errorEl.classList.remove('hidden');
    return;
  }

  rfReceivingASNId = asnId;
  rfReceivingPOId = asn.po_id || '';
  rfReceivingExpected = items.map(it => ({ sku: it.sku || '', expectedQty: Number(it.qty) || 0, receivedQty: 0 }));
  rfReceivingLines = [];
  renderRFReceivingBody();
}

function renderRFReceivingBody() {
  const body = document.getElementById('rf-receiving-body');
  if (!body) return;
  if (rfReceivingExpected.length === 0) {
    body.innerHTML = '';
    return;
  }

  const remaining = rfReceivingExpected.filter(l => l.receivedQty < l.expectedQty);
  const totalLines = rfReceivingExpected.length;
  const doneLines = totalLines - remaining.length;

  const rows = rfReceivingExpected.map(l => `
    <tr style="${l.receivedQty >= l.expectedQty ? 'opacity:0.5;' : ''}">
      <td>${escapeHTMLText(l.sku)}</td>
      <td>${l.receivedQty} / ${l.expectedQty}</td>
    </tr>`).join('');

  body.innerHTML = `
    <div class="table-panel" style="padding:20px; margin-bottom:16px;">
      <div class="text-muted" style="font-size:13px; margin-bottom:10px;">ASN ${escapeHTMLText(rfReceivingASNId)} &mdash; ${doneLines} of ${totalLines} SKU(s) fully received</div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="rf-scan-input">Scan Carton SKU</label>
        <input type="text" id="rf-scan-input" class="form-input" placeholder="Scan or type SKU, then Enter" autocomplete="off" style="font-size:22px; padding:16px;">
      </div>
      <div id="rf-scan-error" class="login-error hidden" style="margin-top:10px;"></div>
      <div id="rf-scan-confirm" style="margin-top:10px; font-size:14px;"></div>
    </div>
    <div class="table-panel" style="padding:0; margin-bottom:16px;">
      <div class="table-wrapper"><table>
        <thead><tr><th>SKU</th><th>Received / Expected</th></tr></thead>
        <tbody>${rows}</tbody>
      </table></div>
    </div>
    <button class="btn btn-primary" id="rf-post-btn" type="button" style="width:100%; padding:14px; font-size:16px;" ${rfReceivingLines.length === 0 ? 'disabled' : ''}>Post Receipt (${rfReceivingLines.length} line(s))</button>
  `;
  const scanInput = document.getElementById('rf-scan-input');
  scanInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); confirmRFScan(); } });
  scanInput.focus();
  document.getElementById('rf-post-btn').addEventListener('click', postRFReceipt);
}

function confirmRFScan() {
  const scanInput = document.getElementById('rf-scan-input');
  const errorEl = document.getElementById('rf-scan-error');
  const confirmEl = document.getElementById('rf-scan-confirm');
  errorEl.classList.add('hidden');
  const scanned = scanInput.value.trim();
  scanInput.value = '';
  if (!scanned) return;

  const expectedLine = rfReceivingExpected.find(l => l.sku === scanned && l.receivedQty < l.expectedQty);
  if (!expectedLine) {
    errorEl.textContent = `${scanned} is not an outstanding line on this ASN.`;
    errorEl.classList.remove('hidden');
    scanInput.focus();
    return;
  }

  const qty = expectedLine.expectedQty - expectedLine.receivedQty;
  expectedLine.receivedQty += qty;
  const existingLine = rfReceivingLines.find(l => l.sku === scanned);
  if (existingLine) {
    existingLine.qty += qty;
  } else {
    rfReceivingLines.push({ sku: scanned, qty, accepted_qty: qty, rejected_qty: 0, damaged_qty: 0 });
  }
  confirmEl.innerHTML = `<span class="badge badge-success">Confirmed ${qty} x ${escapeHTMLText(scanned)}</span>`;
  renderRFReceivingBody();
}

async function postRFReceipt() {
  const errorEl = document.getElementById('rf-setup-error');
  errorEl.classList.add('hidden');
  const location = document.getElementById('rf-location-input').value.trim();
  if (!location) {
    errorEl.textContent = 'A receiving location is required before posting.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (!rfReceivingPOId) {
    errorEl.textContent = `ASN ${rfReceivingASNId} has no PO reference - a GRN requires one. Use the GRN Workbench to receive against this ASN instead.`;
    errorEl.classList.remove('hidden');
    return;
  }
  if (rfReceivingLines.length === 0) return;

  const totalQty = rfReceivingLines.reduce((s, l) => s + l.qty, 0);
  if (!(await showCustomConfirm(`Post this goods receipt against ${rfReceivingPOId}? ${totalQty} unit(s) will be added to stock at ${location}.`, 'Post Goods Receipt'))) return;

  const payload = {
    po_id: rfReceivingPOId,
    asn_id: rfReceivingASNId,
    location,
    received_items: JSON.stringify(rfReceivingLines),
    status: 'Approved'
  };

  const res = await apiFetch('/api/v1/doc/GRN', { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post the receipt.', 'RF Receiving Failed');
    return;
  }
  await showCustomAlert(`Posted a GRN for ${rfReceivingLines.length} line(s) against ASN ${rfReceivingASNId}.`, 'Receipt Posted');
  rfReceivingExpected = [];
  rfReceivingLines = [];
  rfReceivingASNId = '';
  rfReceivingPOId = '';
  document.getElementById('rf-asn-input').value = '';
  renderRFReceivingBody();
}

// Stage 42.4.3 - Sortation / put-wall. Loads a SortStation's slot board and
// lets the operator assign an order to the next Empty slot, confirm a scan
// into it, and clear it once handed off - the un-consolidation step
// GenerateWavePickList's batch pick needs before packing.
async function renderSortationView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sortation / Put-Wall</h1>
      <p class="page-subtitle">Assign an order to a slot, confirm scans into it, then clear it once handed off to packing.</p>
    </div>
  `;
  container.appendChild(header);

  const controls = document.createElement('div');
  controls.className = 'table-panel';
  controls.style.padding = '20px';
  controls.style.marginBottom = '16px';
  controls.innerHTML = `
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="sort-station">Sort Station</label>
        <input type="text" id="sort-station" class="form-input" style="width:160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="sort-load-btn" type="button">Load Slots</button>
    </div>
    <div id="sort-setup-error" class="login-error hidden" style="margin-top:12px;"></div>
  `;
  container.appendChild(controls);

  const assignPanel = document.createElement('div');
  assignPanel.className = 'table-panel';
  assignPanel.style.padding = '20px';
  assignPanel.style.marginBottom = '16px';
  assignPanel.innerHTML = `
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="sort-task-id">Fulfillment Task / Order</label>
        <input type="text" id="sort-task-id" class="form-input" style="width:180px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="sort-sku">SKU (optional)</label>
        <input type="text" id="sort-sku" class="form-input" style="width:140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="sort-qty-expected">Qty Expected (optional)</label>
        <input type="number" id="sort-qty-expected" class="form-input" style="width:100px;" min="0">
      </div>
      <button class="btn btn-secondary" id="sort-assign-btn" type="button">Assign to Slot</button>
    </div>
  `;
  container.appendChild(assignPanel);

  const body = document.createElement('div');
  body.id = 'sortation-body';
  container.appendChild(body);

  document.getElementById('sort-load-btn').addEventListener('click', loadSortationSlots);
  document.getElementById('sort-assign-btn').addEventListener('click', sortationAssignSlot);
}

async function loadSortationSlots() {
  const errorEl = document.getElementById('sort-setup-error');
  errorEl.classList.add('hidden');
  const station = document.getElementById('sort-station').value.trim();
  const body = document.getElementById('sortation-body');
  if (!station) { body.innerHTML = ''; return; }
  const res = await apiFetch(`/api/v1/wms/sortation/slots?station=${encodeURIComponent(station)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load slots.', 'Load Failed'); return; }
  const data = await res.json();
  const slots = data.slots || [];
  if (slots.length === 0) {
    body.innerHTML = `<div class="table-panel" style="padding:24px; color:var(--text-muted);">No slots provisioned for station ${escapeHTMLText(station)} yet - provision them from the SortStation master record.</div>`;
    return;
  }
  body.innerHTML = `
    <div class="table-panel" style="padding:0;">
      <div class="table-wrapper"><table>
        <thead><tr><th>Slot</th><th>Status</th><th>Order</th><th>SKU</th><th>Confirmed / Expected</th><th></th></tr></thead>
        <tbody>${slots.map(s => `<tr>
            <td>${s.slot_no}</td><td>${s.status}</td><td>${s.fulfillment_task_id || '&mdash;'}</td><td>${s.sku || '&mdash;'}</td>
            <td>${s.qty_confirmed} / ${s.qty_expected || '?'}</td>
            <td>
              ${(s.status === 'Assigned' || s.status === 'Filled') ? `
                <input type="number" min="1" value="1" class="form-input sort-confirm-qty" data-slot="${s.doc_id}" style="width:70px; display:inline-block;">
                <button class="btn btn-sm btn-secondary sort-confirm-btn" data-slot="${s.doc_id}" type="button">Confirm</button>` : ''}
              ${s.status === 'Filled' ? `<button class="btn btn-sm btn-secondary sort-clear-btn" data-slot="${s.doc_id}" type="button">Clear</button>` : ''}
            </td>
          </tr>`).join('')}</tbody>
      </table></div>
    </div>`;
  document.querySelectorAll('.sort-confirm-btn').forEach(btn => btn.addEventListener('click', async () => {
    const qtyInput = document.querySelector(`.sort-confirm-qty[data-slot="${btn.dataset.slot}"]`);
    const qty = parseInt(qtyInput.value, 10) || 0;
    if (qty <= 0) return;
    const res = await apiFetch('/api/v1/wms/sortation/confirm', { method: 'POST', body: JSON.stringify({ slot_id: btn.dataset.slot, qty }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to confirm the slot.', 'Confirm Failed'); return; }
    loadSortationSlots();
  }));
  document.querySelectorAll('.sort-clear-btn').forEach(btn => btn.addEventListener('click', async () => {
    const res = await apiFetch('/api/v1/wms/sortation/clear', { method: 'POST', body: JSON.stringify({ slot_id: btn.dataset.slot }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to clear the slot.', 'Clear Failed'); return; }
    loadSortationSlots();
  }));
}

async function sortationAssignSlot() {
  const errorEl = document.getElementById('sort-setup-error');
  errorEl.classList.add('hidden');
  const station = document.getElementById('sort-station').value.trim();
  const taskId = document.getElementById('sort-task-id').value.trim();
  const sku = document.getElementById('sort-sku').value.trim();
  const qtyExpected = parseInt(document.getElementById('sort-qty-expected').value, 10) || 0;
  if (!station || !taskId) {
    errorEl.textContent = 'A station and a fulfillment task / order are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/sortation/assign', {
    method: 'POST',
    body: JSON.stringify({ station, fulfillment_task_id: taskId, sku, qty_expected: qtyExpected })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to assign a slot.', 'Assign Failed'); return; }
  document.getElementById('sort-task-id').value = '';
  document.getElementById('sort-sku').value = '';
  document.getElementById('sort-qty-expected').value = '';
  loadSortationSlots();
}

// Stage 42.4.8/42.4.9 - Loading: open a load against a dock door + trailer,
// scan cartons onto it, complete (running the pre-ship gate), record pallet
// exchange and depart, and print a Bill of Lading through the existing
// browser print-sheet path.
let loadingCurrentTaskId = '';

async function renderLoadingDockView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Loading</h1>
      <p class="page-subtitle">Open a load against a dock door and trailer, scan each carton, then complete and depart.</p>
    </div>
  `;
  container.appendChild(header);

  const setupPanel = document.createElement('div');
  setupPanel.className = 'table-panel';
  setupPanel.style.padding = '20px';
  setupPanel.style.marginBottom = '16px';
  setupPanel.innerHTML = `
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="load-existing-id">Existing Loading Task ID</label>
        <input type="text" id="load-existing-id" class="form-input" style="width:200px;" autocomplete="off">
      </div>
      <button class="btn btn-secondary" id="load-open-existing-btn" type="button">Open</button>
    </div>
    <hr style="margin:16px 0;">
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="load-dock-door">Dock Door</label>
        <input type="text" id="load-dock-door" class="form-input" style="width:140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="load-trailer">Trailer</label>
        <input type="text" id="load-trailer" class="form-input" style="width:140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="load-manifest">Manifest ID (optional)</label>
        <input type="text" id="load-manifest" class="form-input" style="width:160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="load-expected-count">Expected Cartons (optional)</label>
        <input type="number" id="load-expected-count" class="form-input" style="width:120px;" min="0">
      </div>
      <button class="btn btn-primary" id="load-create-btn" type="button">Open New Load</button>
    </div>
    <div id="load-setup-error" class="login-error hidden" style="margin-top:12px;"></div>
  `;
  container.appendChild(setupPanel);

  const body = document.createElement('div');
  body.id = 'loading-body';
  container.appendChild(body);

  document.getElementById('load-open-existing-btn').addEventListener('click', () => {
    const id = document.getElementById('load-existing-id').value.trim();
    if (id) { loadingCurrentTaskId = id; loadLoadingDock(); }
  });
  document.getElementById('load-create-btn').addEventListener('click', createLoadingDockTask);
  loadingCurrentTaskId = '';
}

async function createLoadingDockTask() {
  // BLD-036: guard against a double-click opening two loading tasks for the
  // same dock/trailer.
  await guardAgainstDoubleSubmit(document.getElementById('load-create-btn'), 'Opening...', createLoadingDockTaskInner);
}

async function createLoadingDockTaskInner() {
  const errorEl = document.getElementById('load-setup-error');
  errorEl.classList.add('hidden');
  const dockDoor = document.getElementById('load-dock-door').value.trim();
  const trailer = document.getElementById('load-trailer').value.trim();
  const manifestId = document.getElementById('load-manifest').value.trim();
  const expected = parseInt(document.getElementById('load-expected-count').value, 10) || 0;
  if (!dockDoor || !trailer) {
    errorEl.textContent = 'Dock door and trailer are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/loading/create', {
    method: 'POST',
    body: JSON.stringify({ dock_door: dockDoor, trailer_no: trailer, manifest_id: manifestId, expected_carton_count: expected })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to open the load.', 'Open Failed'); return; }
  const data = await res.json();
  loadingCurrentTaskId = data.loading_task_id;
  loadLoadingDock();
}

async function loadLoadingDock() {
  const body = document.getElementById('loading-body');
  if (!loadingCurrentTaskId) { body.innerHTML = ''; return; }
  const res = await apiFetch(`/api/v1/wms/loading/bol?loading_task_id=${encodeURIComponent(loadingCurrentTaskId)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load the loading task.', 'Load Failed'); return; }
  const data = await res.json();
  const task = data.loading_task;
  const packages = data.packages || [];

  body.innerHTML = `
    <div class="table-panel" style="padding:20px; margin-bottom:16px;">
      <div class="text-muted" style="font-size:13px; margin-bottom:10px;">
        Load ${escapeHTMLText(task.doc_id)} &mdash; door ${escapeHTMLText(task.dock_door)}, trailer ${escapeHTMLText(task.trailer_no)} &mdash;
        <span class="badge">${task.status}</span> &mdash; ${task.scanned_carton_count} scanned${task.expected_carton_count ? ' / ' + task.expected_carton_count + ' expected' : ''}
      </div>
      ${(task.status === 'Planned' || task.status === 'Loading') ? `
        <div class="form-group" style="margin-bottom:0;">
          <label class="form-label" for="load-scan-input">Scan Package / Carton</label>
          <input type="text" id="load-scan-input" class="form-input" placeholder="Scan or type package code, then Enter" autocomplete="off" style="font-size:20px; padding:14px;">
        </div>
        <div id="load-scan-error" class="login-error hidden" style="margin-top:10px;"></div>
        <button class="btn btn-primary" id="load-complete-btn" type="button" style="margin-top:12px;" ${task.scanned_carton_count === 0 ? 'disabled' : ''}>Complete Load</button>
      ` : ''}
      ${task.status === 'Loaded' ? `
        <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap; margin-top:8px;">
          <div class="form-group" style="margin-bottom:0;">
            <label class="form-label" for="load-pallets-out">Pallets Out</label>
            <input type="number" id="load-pallets-out" class="form-input" style="width:100px;" min="0" value="${task.pallet_exchange_out || 0}">
          </div>
          <div class="form-group" style="margin-bottom:0;">
            <label class="form-label" for="load-pallets-in">Pallets In</label>
            <input type="number" id="load-pallets-in" class="form-input" style="width:100px;" min="0" value="${task.pallet_exchange_in || 0}">
          </div>
          <button class="btn btn-secondary" id="load-depart-btn" type="button">Record Exchange &amp; Depart</button>
          <button class="btn btn-secondary" id="load-print-bol-btn" type="button">Print Bill of Lading</button>
        </div>
      ` : ''}
      ${task.status === 'Departed' ? `<button class="btn btn-secondary" id="load-print-bol-btn" type="button" style="margin-top:8px;">Print Bill of Lading</button>` : ''}
    </div>
    <div class="table-panel" style="padding:0;">
      <div class="table-wrapper"><table>
        <thead><tr><th>Package</th><th>Order</th><th>Weight (kg)</th></tr></thead>
        <tbody>${packages.length === 0 ? '<tr><td colspan="3">No cartons scanned yet.</td></tr>' : packages.map(p => `<tr><td>${p.package_code}</td><td>${p.order_id || '&mdash;'}</td><td>${p.weight_kg || '&mdash;'}</td></tr>`).join('')}</tbody>
      </table></div>
    </div>`;

  const scanInput = document.getElementById('load-scan-input');
  if (scanInput) {
    scanInput.addEventListener('keydown', async (e) => {
      if (e.key !== 'Enter') return;
      e.preventDefault();
      const errorEl = document.getElementById('load-scan-error');
      errorEl.classList.add('hidden');
      const code = scanInput.value.trim();
      scanInput.value = '';
      if (!code) return;
      const res = await apiFetch('/api/v1/wms/loading/scan', { method: 'POST', body: JSON.stringify({ loading_task_id: loadingCurrentTaskId, package_code: code }) });
      if (!res) return;
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        errorEl.textContent = data.error || 'Scan failed.';
        errorEl.classList.remove('hidden');
        return;
      }
      loadLoadingDock();
    });
    scanInput.focus();
  }
  const completeBtn = document.getElementById('load-complete-btn');
  if (completeBtn) completeBtn.addEventListener('click', async () => {
    const res = await apiFetch('/api/v1/wms/loading/complete', { method: 'POST', body: JSON.stringify({ loading_task_id: loadingCurrentTaskId }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to complete the load.', 'Complete Failed'); return; }
    loadLoadingDock();
  });
  const departBtn = document.getElementById('load-depart-btn');
  if (departBtn) departBtn.addEventListener('click', async () => {
    const palletsOut = parseInt(document.getElementById('load-pallets-out').value, 10) || 0;
    const palletsIn = parseInt(document.getElementById('load-pallets-in').value, 10) || 0;
    const res = await apiFetch('/api/v1/wms/loading/depart', {
      method: 'POST',
      body: JSON.stringify({ loading_task_id: loadingCurrentTaskId, pallet_exchange_out: palletsOut, pallet_exchange_in: palletsIn })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to record departure.', 'Depart Failed'); return; }
    loadLoadingDock();
  });
  const printBtn = document.getElementById('load-print-bol-btn');
  if (printBtn) printBtn.addEventListener('click', () => renderBOLPrintSheet(task, packages));
}

function renderBOLPrintSheet(task, packages) {
  const area = document.getElementById('bol-print-area');
  if (!area) return;
  const rows = packages.map(p => `<tr><td>${p.package_code}</td><td>${p.order_id || '&mdash;'}</td><td>${p.weight_kg || '&mdash;'}</td></tr>`).join('');
  area.innerHTML = `
    <div class="invoice-sheet">
      <div class="invoice-title">Bill of Lading</div>
      <hr>
      <table>
        <tr><td class="invoice-key">Load</td><td>${task.doc_id}</td></tr>
        <tr><td class="invoice-key">Dock Door</td><td>${task.dock_door}</td></tr>
        <tr><td class="invoice-key">Trailer</td><td>${task.trailer_no}</td></tr>
        <tr><td class="invoice-key">Status</td><td>${task.status}</td></tr>
        <tr><td class="invoice-key">Pallets Out / In</td><td>${task.pallet_exchange_out || 0} / ${task.pallet_exchange_in || 0}</td></tr>
      </table>
      <hr>
      <table>
        <thead><tr><th>Package</th><th>Order</th><th>Weight (kg)</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
      <div class="invoice-total">Total Cartons: ${packages.length}</div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// Stage 42.2.10 - the warehouse cockpit: one screen combining open tasks by
// type/age, the exception queue, wave status, inbound due today, pick/pack
// throughput and bin utilisation - every section reads GetWarehouseCockpit's
// own best-effort aggregation, so one empty section never blocks the rest
// from rendering. No dock/appointment strip - 42.3 (dock scheduling)
// doesn't exist yet, and the plan's own note says that section is populated
// there, not here.
async function renderWarehouseCockpitView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Warehouse Cockpit</h1>
      <p class="page-subtitle">Open tasks, exceptions, wave status, inbound due today, throughput and bin utilisation for one location, in one place.</p>
    </div>
  `;
  container.appendChild(header);

  const controls = document.createElement('div');
  controls.className = 'table-panel';
  controls.style.padding = '24px';
  controls.style.marginBottom = '24px';
  controls.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="cockpit-location">Location</label>
        <input type="text" id="cockpit-location" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="cockpit-refresh-btn" type="button">Refresh</button>
    </div>
  `;
  container.appendChild(controls);

  const body = document.createElement('div');
  body.id = 'cockpit-body';
  container.appendChild(body);

  document.getElementById('cockpit-refresh-btn').addEventListener('click', loadWarehouseCockpit);
  attachLinkTypeahead(document.getElementById('cockpit-location'), 'Location');
  document.getElementById('cockpit-location').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') loadWarehouseCockpit();
  });
}

async function loadWarehouseCockpit(liveBody = null) {
  const body = liveBody?.isConnected ? liveBody : document.getElementById('cockpit-body');
  if (!body) return;
  const location = document.getElementById('cockpit-location').value.trim();
  if (!location) {
    body.innerHTML = `<div class="table-panel" style="padding:24px; color:var(--text-muted);">Enter a Location and click Refresh.</div>`;
    return;
  }
  body.innerHTML = `<div class="table-panel" style="padding:24px; color:var(--text-muted);">Loading...</div>`;
  const res = await apiFetch(`/api/v1/wms/cockpit?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) {
    // BLD-036: the modal used to close onto a body stuck on "Loading..."
    // forever, with no retry short of navigating away and back.
    renderErrorPanel(body, 'Failed to load the cockpit.', retryBody => loadWarehouseCockpit(retryBody));
    return;
  }
  const c = await res.json();

  const section = (title, inner) => `
    <div class="table-panel" style="padding:24px; margin-bottom:24px;">
      <h2 style="font-size:16px; font-weight:700; margin-bottom:12px;">${title}</h2>
      ${inner}
    </div>`;
  const empty = 'No records.';

  const openTasksHtml = (c.open_tasks || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>Task Type</th><th>Status</th><th>Count</th><th>Oldest (mins)</th></tr></thead>
      <tbody>
        ${c.open_tasks.map(t => `<tr><td>${t.task_type}</td><td>${t.status}</td><td>${t.count}</td><td>${t.oldest_age_mins}</td></tr>`).join('')}
      </tbody>
    </table>`;

  const exceptionsHtml = (c.exception_queue || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>Task</th><th>Type</th><th>Item</th><th>Bin</th><th>Process Step</th><th>Follow-On</th><th>Age (mins)</th></tr></thead>
      <tbody>
        ${c.exception_queue.map(e => `<tr>
          <td>${e.task_id}</td><td>${e.task_type}</td><td>${e.item || '&mdash;'}</td><td>${e.bin_code || '&mdash;'}</td>
          <td>${e.process_step || '&mdash;'}</td><td><span class="badge badge-warning">${e.follow_on_action || '&mdash;'}</span></td>
          <td>${e.age_mins}</td>
        </tr>`).join('')}
      </tbody>
    </table>`;

  const waveHtml = (c.wave_status || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>Wave</th><th>Status</th><th>Count</th></tr></thead>
      <tbody>${c.wave_status.map(w => `<tr><td>${w.wave_id}</td><td>${w.status}</td><td>${w.count}</td></tr>`).join('')}</tbody>
    </table>`;

  // Stage 42.4.2: the registered-Wave lifecycle view, separate from the
  // per-FulfillmentTask-status breakdown above. "Advance" moves a wave one
  // step forward through TransitionWaveStatus; a wave with no open tasks
  // left can be advanced straight through to Closed by repeated clicks.
  const waveNextStatus = { 'Planned': 'Released', 'Released': 'In Progress', 'In Progress': 'Complete', 'Complete': 'Closed' };
  const waveMonitorHtml = (c.wave_monitor || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>Wave</th><th>Status</th><th>Via</th><th>Tasks</th><th>Open</th><th>Age (mins)</th><th></th></tr></thead>
      <tbody>${c.wave_monitor.map(w => `<tr>
          <td>${w.wave_id}</td><td>${w.status}</td><td>${w.created_via}</td><td>${w.task_count}</td><td>${w.open_tasks}</td><td>${w.age_mins}</td>
          <td>${waveNextStatus[w.status] ? `<button class="btn btn-sm btn-secondary wave-advance-btn" data-wave="${w.wave_id}" data-next="${waveNextStatus[w.status]}" type="button">${waveNextStatus[w.status]}</button>` : ''}</td>
        </tr>`).join('')}</tbody>
    </table>
    <div style="display:flex; gap:8px; align-items:flex-end; margin-top:16px; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="cockpit-wave-template">Wave Template ID</label>
        <input type="text" id="cockpit-wave-template" class="form-input" style="width:220px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="cockpit-run-template-btn" type="button">Run Template Now</button>
    </div>`;

  const inboundHtml = (c.inbound_due_today || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>ASN</th><th>Vendor</th><th>Expected Date</th><th>Status</th></tr></thead>
      <tbody>${c.inbound_due_today.map(a => `<tr><td>${a.asn_number}</td><td>${a.vendor || '&mdash;'}</td><td>${a.expected_date}</td><td>${a.status}</td></tr>`).join('')}</tbody>
    </table>`;

  const throughputHtml = (c.throughput_today || []).length === 0 ? empty : `
    <table>
      <thead><tr><th>User</th><th>Task Type</th><th>Count</th><th>Tasks/Hour</th></tr></thead>
      <tbody>${c.throughput_today.map(p => `<tr><td>${p.user_id}</td><td>${p.task_type}</td><td>${p.task_count}</td><td>${p.tasks_per_hour}</td></tr>`).join('')}</tbody>
    </table>`;

  const binUtilHtml = (c.bin_utilization || []).length === 0 ? `${empty} (no bin at this location has a capacity configured yet.)` : `
    <table>
      <thead><tr><th>Bin</th><th>Used</th><th>Capacity</th><th>% Used</th></tr></thead>
      <tbody>${c.bin_utilization.map(u => `<tr><td>${u.bin_code}</td><td>${u.used_qty}</td><td>${u.max_qty}</td>
        <td><span class="badge ${u.pct_used >= 90 ? 'badge-danger' : (u.pct_used >= 70 ? 'badge-warning' : 'badge-success')}">${u.pct_used}%</span></td></tr>`).join('')}</tbody>
    </table>`;

  body.innerHTML =
    section('Open Tasks by Type / Age', openTasksHtml) +
    section('Exception Queue', exceptionsHtml) +
    section('Wave Status', waveHtml) +
    section('Wave Monitor', waveMonitorHtml) +
    section('Inbound Due Today', inboundHtml) +
    section("Today's Throughput", throughputHtml) +
    section('Bin Utilisation', binUtilHtml);

  document.querySelectorAll('.wave-advance-btn').forEach(btn => {
    btn.addEventListener('click', async () => {
      const res = await apiFetch('/api/v1/wms/wave/transition', { method: 'POST', body: JSON.stringify({ wave_id: btn.dataset.wave, status: btn.dataset.next }) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Failed to advance the wave.', 'Advance Failed'); return; }
      loadWarehouseCockpit();
    });
  });
  const runTemplateBtn = document.getElementById('cockpit-run-template-btn');
  if (runTemplateBtn) {
    runTemplateBtn.addEventListener('click', async () => {
      const templateId = document.getElementById('cockpit-wave-template').value.trim();
      if (!templateId) return;
      const res = await apiFetch('/api/v1/wms/wave/template/run', { method: 'POST', body: JSON.stringify({ wave_template_id: templateId }) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Failed to run the wave template.', 'Run Failed'); return; }
      loadWarehouseCockpit();
    });
  }
}

const BIN_STOCK_CONDITIONS = ['Good', 'Damaged', 'QC-Hold', 'RTV'];

async function renderBinConditionsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bin Conditions</h1>
      <p class="page-subtitle">Move bin stock between Good, Damaged, QC-Hold, and RTV. Moving out of Good makes it unsellable; moving into Good makes it sellable again.</p>
    </div>
  `;
  container.appendChild(header);

  const options = BIN_STOCK_CONDITIONS.map(c => `<option value="${c}">${c}</option>`).join('');
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-bin">Bin Code</label>
        <input type="text" id="bincond-bin" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-sku">SKU</label>
        <input type="text" id="bincond-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-qty">Qty</label>
        <input type="number" id="bincond-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-from">From Condition</label>
        <select id="bincond-from" class="form-input" style="width: 130px;">${options}</select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-to">To Condition</label>
        <select id="bincond-to" class="form-input" style="width: 130px;">${options}</select>
      </div>
      <button class="btn btn-primary" id="bincond-submit-btn" type="button">Move</button>
    </div>
    <div id="bincond-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);
  document.getElementById('bincond-to').value = 'Damaged';

  document.getElementById('bincond-submit-btn').addEventListener('click', submitBinConditionTransition);
  attachLinkTypeahead(document.getElementById('bincond-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('bincond-sku'), 'Item');
}

async function submitBinConditionTransition() {
  const errorEl = document.getElementById('bincond-form-error');
  errorEl.classList.add('hidden');

  const binCode = document.getElementById('bincond-bin').value.trim();
  const sku = document.getElementById('bincond-sku').value.trim();
  const qty = parseInt(document.getElementById('bincond-qty').value, 10);
  const fromCondition = document.getElementById('bincond-from').value;
  const toCondition = document.getElementById('bincond-to').value;

  if (!binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (fromCondition === toCondition) {
    errorEl.textContent = 'From Condition and To Condition must differ.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/condition-transition', {
    method: 'POST',
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty, from_condition: fromCondition, to_condition: toCondition })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to move bin stock condition.', 'Condition Move Failed');
    return;
  }
  await showCustomAlert(`Moved ${qty} x ${sku} in bin ${binCode} from ${fromCondition} to ${toCondition}.`, 'Condition Move Complete');
  document.getElementById('bincond-qty').value = 1;
}

// Stage 26.5.4: LPN/carton/pallet grouping on top of bin_stock - assign a
// bin's sku/condition qty into a container, and look up what's inside one.
async function renderLPNView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">LPN / Carton / Pallet Grouping</h1>
      <p class="page-subtitle">Group bin stock into a carton or pallet container for tracking - a further breakdown of bin stock, never a second source of truth for a bin's total.</p>
    </div>
  `;
  container.appendChild(header);

  const assignPanel = document.createElement('div');
  assignPanel.className = 'table-panel';
  assignPanel.style.padding = '24px';
  assignPanel.style.marginBottom = '24px';
  assignPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">Assign Bin Stock to an LPN</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-code">LPN Code</label>
        <input type="text" id="lpn-code" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-bin">Bin Code</label>
        <input type="text" id="lpn-bin" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-sku">SKU</label>
        <input type="text" id="lpn-sku" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-condition">Condition</label>
        <select id="lpn-condition" class="form-input" style="width: 120px;">${BIN_STOCK_CONDITIONS.map(c => `<option value="${c}">${c}</option>`).join('')}</select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-qty">Qty</label>
        <input type="number" id="lpn-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-primary" id="lpn-assign-btn" type="button">Assign</button>
    </div>
    <div id="lpn-assign-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(assignPanel);

  const lookupPanel = document.createElement('div');
  lookupPanel.className = 'table-panel';
  lookupPanel.style.padding = '24px';
  lookupPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">Look Up an LPN's Contents</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-lookup-code">LPN Code</label>
        <input type="text" id="lpn-lookup-code" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="lpn-lookup-btn" type="button">Look Up</button>
    </div>
    <div id="lpn-contents-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(lookupPanel);

  document.getElementById('lpn-assign-btn').addEventListener('click', submitLPNAssign);
  document.getElementById('lpn-lookup-btn').addEventListener('click', lookupLPNContents);
  attachLinkTypeahead(document.getElementById('lpn-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('lpn-sku'), 'Item');
}

async function submitLPNAssign() {
  const errorEl = document.getElementById('lpn-assign-error');
  errorEl.classList.add('hidden');
  const lpnCode = document.getElementById('lpn-code').value.trim();
  const binCode = document.getElementById('lpn-bin').value.trim();
  const sku = document.getElementById('lpn-sku').value.trim();
  const condition = document.getElementById('lpn-condition').value;
  const qty = parseInt(document.getElementById('lpn-qty').value, 10);
  if (!lpnCode || !binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'LPN Code, Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/lpn/assign', {
    method: 'POST',
    body: JSON.stringify({ lpn_code: lpnCode, bin_code: binCode, sku, condition, qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to assign to LPN.', 'LPN Assign Failed'); return; }
  await showCustomAlert(`Assigned ${qty} x ${sku} (${condition}) from bin ${binCode} to LPN ${lpnCode}.`, 'LPN Assign Complete');
}

async function lookupLPNContents() {
  const resultEl = document.getElementById('lpn-contents-result');
  const lpnCode = document.getElementById('lpn-lookup-code').value.trim();
  if (!lpnCode) return;
  const res = await apiFetch(`/api/v1/wms/lpn/contents?lpn_code=${encodeURIComponent(lpnCode)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to look up LPN contents.', 'Lookup Failed'); return; }
  const lines = await res.json();
  if (lines.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No contents found for LPN ${lpnCode}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>Bin</th><th>SKU</th><th>Condition</th><th>Qty</th></tr></thead>
      <tbody>
        ${lines.map(l => `<tr><td>${l.bin_code}</td><td>${l.sku}</td><td>${l.condition}</td><td>${l.qty}</td></tr>`).join('')}
      </tbody>
    </table>
  `;
}

// Stage 26.5.5: bin-to-bin replenishment min/max triggers - suggestions
// (shortage = max_qty - current_qty, filled from reserve bins highest-qty-
// first) plus the action that actually executes one.
async function renderBinReplenishmentView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bin Replenishment</h1>
      <p class="page-subtitle">Pick-face bins below their min qty, with a suggested reserve bin to draw from - configure rules via the BinReplenishmentRule master.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="replen-location">Location</label>
        <input type="text" id="replen-location" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="replen-fetch-btn" type="button">Get Suggestions</button>
    </div>
    <div id="replen-result"></div>
  `;
  container.appendChild(panel);
  document.getElementById('replen-fetch-btn').addEventListener('click', fetchBinReplenishmentSuggestions);
  attachLinkTypeahead(document.getElementById('replen-location'), 'Location');
}

async function fetchBinReplenishmentSuggestions() {
  const resultEl = document.getElementById('replen-result');
  const location = document.getElementById('replen-location').value.trim();
  if (!location) return;
  const res = await apiFetch(`/api/v1/wms/bin-replenishment/suggestions?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to fetch replenishment suggestions.', 'Fetch Failed'); return; }
  const suggestions = await res.json();
  if (suggestions.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No bins are below their min qty at ${location}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>Bin</th><th>SKU</th><th>Current</th><th>Min</th><th>Max</th><th>Shortage</th><th>From Bin</th><th>Move Qty</th><th></th></tr></thead>
      <tbody>
        ${suggestions.map((s, idx) => `
          <tr>
            <td>${s.bin_code}</td><td>${s.sku}</td><td>${s.current_qty}</td><td>${s.min_qty}</td><td>${s.max_qty}</td>
            <td><span class="badge badge-warning">${s.shortage}</span></td>
            <td>${s.from_bin_code || '&mdash;'}</td>
            <td>${s.move_qty || 0}</td>
            <td>${s.from_bin_code ? `<button class="action-btn" ${actionAttrs('executeBinReplenishmentRow', [idx])}>Replenish</button>` : ''}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
  window._replenSuggestions = suggestions;
}

window.executeBinReplenishmentRow = async function(idx) {
  const s = (window._replenSuggestions || [])[idx];
  if (!s || !s.from_bin_code) return;
  const res = await apiFetch('/api/v1/wms/bin-replenishment/execute', {
    method: 'POST',
    body: JSON.stringify({ from_bin_code: s.from_bin_code, to_bin_code: s.bin_code, sku: s.sku, qty: s.move_qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to execute replenishment.', 'Replenishment Failed'); return; }
  await showCustomAlert(`Moved ${s.move_qty} x ${s.sku} from ${s.from_bin_code} to ${s.bin_code}.`, 'Replenishment Complete');
  fetchBinReplenishmentSuggestions();
};

// --- Stage 57.9: Location Movement ------------------------------------------
//
// Bin-to-bin moves inside one location (user decision 2026-10-07). Lists what
// each bin holds, then moves part of it to another bin at the same location
// through POST /api/v1/wms/bin-move - the same MoveBinStock the replenishment
// screen uses, so both refuse the same destinations (missing, inactive,
// Blocked/Full/Counting, another location, hazmat zone, over capacity).
let locMoveRows = [];

async function renderLocationMovementView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Location Movement</h1>
      <p class="page-subtitle">Move stock from one bin to another inside the same warehouse or store. To move stock to a different location, use Stock Transfer.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-location">Location</label>
        <input type="text" id="locmove-location" class="form-input" style="width: 220px;" autocomplete="off" placeholder="Search by name">
      </div>
      <button class="btn btn-primary" id="locmove-load-btn" type="button">Show Bin Stock</button>
    </div>
    <div id="locmove-form" class="hidden" style="border: 1px solid var(--border-color); border-radius: 8px; padding: 16px; margin-bottom: 16px;"></div>
    <div id="locmove-result"><p style="color: var(--text-muted);">Choose a location to see what each bin holds.</p></div>
  `;
  container.appendChild(panel);
  attachLinkTypeahead(document.getElementById('locmove-location'), 'Location');
  document.getElementById('locmove-load-btn').addEventListener('click', loadLocationMovementStock);
  document.getElementById('locmove-location').addEventListener('change', loadLocationMovementStock);
}

async function loadLocationMovementStock() {
  const resultEl = document.getElementById('locmove-result');
  const formEl = document.getElementById('locmove-form');
  const location = document.getElementById('locmove-location').value.trim();
  if (!location) return;
  formEl.classList.add('hidden');
  const res = await apiFetch(`/api/v1/wms/bin-contents?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load bin stock.', 'Load Failed'); return; }
  locMoveRows = await res.json();
  if (locMoveRows.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No bin at this location holds any stock yet. Stock gets into bins through Putaway.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>Bin</th><th>Item</th><th class="num">Qty</th><th>Batches</th><th></th></tr></thead>
      <tbody>
        ${locMoveRows.map((r, idx) => `
          <tr>
            <td>${escapeHTMLText(r.bin_code)}</td>
            <td>${escapeHTMLText(r.item_name || r.sku)}<div style="font-size: 11.5px; color: var(--text-muted);">${escapeHTMLText(r.sku)}</div></td>
            <td class="num">${r.qty}</td>
            <td>${(r.batches || []).map(escapeHTMLText).join(', ') || '&mdash;'}</td>
            <td><button class="action-btn" data-locmove-idx="${idx}">Move</button></td>
          </tr>`).join('')}
      </tbody>
    </table>
  `;
  resultEl.querySelectorAll('[data-locmove-idx]').forEach(btn =>
    btn.addEventListener('click', () => openLocationMoveForm(Number(btn.dataset.locmoveIdx))));
}

function openLocationMoveForm(idx) {
  const r = locMoveRows[idx];
  if (!r) return;
  const location = document.getElementById('locmove-location').value.trim();
  const formEl = document.getElementById('locmove-form');
  const batches = r.batches || [];
  formEl.innerHTML = `
    <h3 style="margin: 0 0 12px; font-size: 16px;">Move ${escapeHTMLText(r.item_name || r.sku)} out of bin ${escapeHTMLText(r.bin_code)} (${r.qty} here)</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${batches.length ? `
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-batch">Batch</label>
        <select id="locmove-batch" class="form-input" style="width: 150px;">
          ${batches.length > 1 ? '<option value="">Choose a batch</option>' : ''}
          ${batches.map(b => `<option value="${escapeHTMLText(b)}">${escapeHTMLText(b)}</option>`).join('')}
        </select>
      </div>` : ''}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-qty">Quantity</label>
        <input type="number" id="locmove-qty" class="form-input" style="width: 110px;" min="1" max="${r.qty}" value="${r.qty}">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-to-bin">To Bin</label>
        <input type="text" id="locmove-to-bin" class="form-input" style="width: 200px;" autocomplete="off" placeholder="Bin at this location">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-reason">Reason (optional)</label>
        <input type="text" id="locmove-reason" class="form-input" style="width: 220px;" placeholder="e.g. re-slotting, damaged shelf">
      </div>
      <button class="btn btn-primary" id="locmove-submit-btn" type="button">Move Stock</button>
      <button class="btn btn-outline" id="locmove-cancel-btn" type="button">Cancel</button>
    </div>
  `;
  formEl.classList.remove('hidden');
  attachLinkTypeahead(document.getElementById('locmove-to-bin'), 'Bin', {
    valueFields: ['bin_code'],
    labelFn: d => d.bin_code || d.id,
    filters: { location },
    showAllOnFocus: true,
  });
  document.getElementById('locmove-cancel-btn').addEventListener('click', () => formEl.classList.add('hidden'));
  document.getElementById('locmove-submit-btn').addEventListener('click', () => submitLocationMove(r));
  formEl.scrollIntoView({ block: 'nearest' });
}

async function submitLocationMove(r) {
  const qty = parseInt(document.getElementById('locmove-qty').value, 10);
  const toBin = document.getElementById('locmove-to-bin').value.trim();
  const batchEl = document.getElementById('locmove-batch');
  const batchNo = batchEl ? batchEl.value : '';
  const reason = document.getElementById('locmove-reason').value.trim();
  if (!toBin) { await showCustomAlert('Choose the bin to move the stock into.', 'To Bin Required'); return; }
  if (!(qty > 0)) { await showCustomAlert('Enter a quantity of at least 1.', 'Quantity Required'); return; }
  const btn = document.getElementById('locmove-submit-btn');
  btn.disabled = true;
  try {
    const res = await apiFetch('/api/v1/wms/bin-move', {
      method: 'POST',
      body: JSON.stringify({ from_bin_code: r.bin_code, to_bin_code: toBin, sku: r.sku, qty, batch_no: batchNo, reason })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The stock could not be moved.', 'Move Failed'); return; }
    showToast(`Moved ${qty} x ${r.item_name || r.sku} from ${r.bin_code} to ${toBin}.`);
    await loadLocationMovementStock();
  } finally {
    btn.disabled = false;
  }
}

// Stage 26.5.6: wave/batch pick-list grouping - tag a batch of open
// FulfillmentTasks into a wave, then generate one consolidated,
// zone-then-bin-sorted pick list covering every order in it.
async function renderWavePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Wave / Batch Picking</h1>
      <p class="page-subtitle">Tag several open fulfillment tasks into a wave, then generate one consolidated pick list instead of walking the warehouse once per order.</p>
    </div>
  `;
  container.appendChild(header);

  const assignPanel = document.createElement('div');
  assignPanel.className = 'table-panel';
  assignPanel.style.padding = '24px';
  assignPanel.style.marginBottom = '24px';
  assignPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">1. Tag tasks into a wave</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="wave-id">Wave ID</label>
        <input type="text" id="wave-id" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 260px;">
        <label class="form-label" for="wave-task-ids">Task IDs (comma-separated)</label>
        <input type="text" id="wave-task-ids" class="form-input" style="width: 100%;" placeholder="FT-1001, FT-1002, FT-1003">
      </div>
      <button class="btn btn-outline" id="wave-assign-btn" type="button">Tag Tasks</button>
    </div>
    <div id="wave-assign-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(assignPanel);

  const genPanel = document.createElement('div');
  genPanel.className = 'table-panel';
  genPanel.style.padding = '24px';
  genPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">2. Generate the wave pick list</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="wave-gen-id">Wave ID</label>
        <input type="text" id="wave-gen-id" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="wave-gen-btn" type="button">Generate Pick List</button>
    </div>
    <div id="wave-pick-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(genPanel);

  document.getElementById('wave-assign-btn').addEventListener('click', submitWaveAssign);
  document.getElementById('wave-gen-btn').addEventListener('click', submitWavePickList);
}

// Stage 26.5.14 (P2, go-ahead 2026-07-27): mobile/voice picking. Reuses
// the exact same wave pick-list endpoint Wave/Batch Picking already calls
// (GET /api/v1/wms/wave/pick-list) - no new picking logic, just a
// narrow single-item-at-a-time layout suited to a phone screen instead of
// a wide table, plus optional voice readout/confirm via the browser-native
// Web Speech API (SpeechSynthesis/SpeechRecognition) - no new dependency,
// and both are feature-detected so this degrades to silent button-tap
// navigation wherever unsupported (notably: no SpeechRecognition in
// Firefox as of this writing).
let mobilePickLines = [];
let mobilePickIndex = 0;
let mobilePickRecognition = null;

async function renderMobilePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Mobile Picking</h1>
      <p class="page-subtitle">A phone-friendly, one-item-at-a-time view of a wave's pick list, with optional voice readout and hands-free "next"/"confirm" control.</p>
    </div>
  `;
  container.appendChild(header);

  const loadPanel = document.createElement('div');
  loadPanel.className = 'table-panel';
  loadPanel.style.padding = '20px';
  loadPanel.style.marginBottom = '16px';
  loadPanel.style.maxWidth = '420px';
  loadPanel.innerHTML = `
    <div style="display:flex; gap:10px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0; flex:1; min-width:140px;">
        <label class="form-label" for="mobile-pick-wave-id">Wave ID</label>
        <input type="text" id="mobile-pick-wave-id" class="form-input" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="mobile-pick-load-btn" type="button">Load</button>
    </div>
    <div id="mobile-pick-error" class="login-error hidden" style="margin-top:10px;"></div>
  `;
  container.appendChild(loadPanel);

  const cardWrap = document.createElement('div');
  cardWrap.id = 'mobile-pick-card-wrap';
  cardWrap.style.maxWidth = '420px';
  container.appendChild(cardWrap);

  document.getElementById('mobile-pick-load-btn').addEventListener('click', loadMobilePickList);
}

async function loadMobilePickList() {
  const errorEl = document.getElementById('mobile-pick-error');
  errorEl.classList.add('hidden');
  const waveId = document.getElementById('mobile-pick-wave-id').value.trim();
  if (!waveId) { errorEl.textContent = 'Wave ID is required.'; errorEl.classList.remove('hidden'); return; }

  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'Failed to load the wave pick list.'); errorEl.classList.remove('hidden'); return; }
  const data = await res.json();
  mobilePickLines = data.pick_lines || [];
  mobilePickIndex = 0;
  renderMobilePickCard();
}

function renderMobilePickCard() {
  const wrap = document.getElementById('mobile-pick-card-wrap');
  if (!wrap) return;
  if (mobilePickLines.length === 0) {
    wrap.innerHTML = `<div class="table-panel" style="padding:24px; text-align:center; color:var(--text-muted);">No pick lines for this wave. Generate a wave under <b>Wave Picking</b> first.</div>`;
    return;
  }
  const line = mobilePickLines[mobilePickIndex];
  const speechSupported = 'speechSynthesis' in window;
  const listenSupported = !!(window.SpeechRecognition || window.webkitSpeechRecognition);
  wrap.innerHTML = `
    <div class="table-panel" style="padding:24px; text-align:center;">
      <div class="text-muted" style="font-size:13px; margin-bottom:12px;">Item ${mobilePickIndex + 1} of ${mobilePickLines.length}</div>
      <div style="font-size:32px; font-weight:700; letter-spacing:-0.5px; margin-bottom:6px;">${line.sku}</div>
      <div style="font-size:15px; color:var(--text-muted); margin-bottom:16px;">Zone ${line.zone || '-'} / Aisle ${line.aisle || '-'} / Rack ${line.rack || '-'} / Bin ${line.bin_code}</div>
      ${line.batch_no ? `<div style="font-size:15px; margin-bottom:16px; line-height:1.7;">Batch ${batchCellHTML(line)}</div>` : ''}
      <div style="font-size:48px; font-weight:800; color:var(--primary-color); margin-bottom:20px;">${line.pick_qty}${line.shortfall ? ` <span class="badge badge-warning" style="font-size:14px; vertical-align:middle;">short ${line.shortfall}</span>` : ''}</div>
      <div style="display:flex; gap:10px; justify-content:center; margin-bottom:14px;">
        <button class="btn btn-outline" id="mobile-pick-prev" type="button" ${mobilePickIndex === 0 ? 'disabled' : ''}>Previous</button>
        <button class="btn btn-primary" id="mobile-pick-next" type="button" ${mobilePickIndex === mobilePickLines.length - 1 ? 'disabled' : ''}>Confirm &amp; Next</button>
      </div>
      ${speechSupported || listenSupported ? `
      <div style="display:flex; gap:10px; justify-content:center; padding-top:14px; border-top:1px solid var(--border-color);">
        ${speechSupported ? `<button class="btn btn-outline btn-sm" id="mobile-pick-speak" type="button">Speak Item</button>` : ''}
        ${listenSupported ? `<button class="btn btn-outline btn-sm" id="mobile-pick-listen" type="button">${mobilePickRecognition ? 'Stop Listening' : 'Listen ("next"/"confirm")'}</button>` : ''}
      </div>` : ''}
    </div>
  `;
  const prevBtn = document.getElementById('mobile-pick-prev');
  const nextBtn = document.getElementById('mobile-pick-next');
  if (prevBtn) prevBtn.addEventListener('click', () => { mobilePickIndex = Math.max(0, mobilePickIndex - 1); renderMobilePickCard(); });
  if (nextBtn) nextBtn.addEventListener('click', () => { mobilePickIndex = Math.min(mobilePickLines.length - 1, mobilePickIndex + 1); renderMobilePickCard(); });
  const speakBtn = document.getElementById('mobile-pick-speak');
  if (speakBtn) speakBtn.addEventListener('click', () => speakMobilePickLine(line));
  const listenBtn = document.getElementById('mobile-pick-listen');
  if (listenBtn) listenBtn.addEventListener('click', toggleMobilePickListening);
}

function speakMobilePickLine(line) {
  if (!('speechSynthesis' in window)) return;
  window.speechSynthesis.cancel();
  // Stage 42.1.5: the lot is spoken too when there is one - a voice-picking
  // flow that names the bin but not the batch would send a picker to the right
  // shelf to take the wrong lot, which is precisely the failure FEFO exists to
  // prevent.
  const batchPhrase = line.batch_no ? ` Batch ${line.batch_no}.` : '';
  const utterance = new SpeechSynthesisUtterance(`Pick ${line.pick_qty} of ${line.sku}, bin ${line.bin_code}, zone ${line.zone || 'unspecified'}.${batchPhrase}`);
  window.speechSynthesis.speak(utterance);
}

function toggleMobilePickListening() {
  const SpeechRecognitionCtor = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SpeechRecognitionCtor) return;
  const btn = document.getElementById('mobile-pick-listen');
  if (mobilePickRecognition) {
    mobilePickRecognition.stop();
    mobilePickRecognition = null;
    if (btn) btn.textContent = 'Listen ("next"/"confirm")';
    return;
  }
  mobilePickRecognition = new SpeechRecognitionCtor();
  mobilePickRecognition.continuous = true;
  mobilePickRecognition.interimResults = false;
  mobilePickRecognition.onresult = (event) => {
    // continuous:true keeps this instance running independent of DOM
    // rebuilds - renderMobilePickCard() replaces the card markup (including
    // the Listen button) but re-reads mobilePickRecognition's current state
    // to relabel the new button, no stop/restart needed here.
    const said = event.results[event.results.length - 1][0].transcript.trim().toLowerCase();
    if (said.includes('next') || said.includes('confirm')) {
      mobilePickIndex = Math.min(mobilePickLines.length - 1, mobilePickIndex + 1);
      renderMobilePickCard();
    } else if (said.includes('previous') || said.includes('back')) {
      mobilePickIndex = Math.max(0, mobilePickIndex - 1);
      renderMobilePickCard();
    }
  };
  mobilePickRecognition.onerror = () => { mobilePickRecognition = null; };
  mobilePickRecognition.onend = () => { mobilePickRecognition = null; };
  mobilePickRecognition.start();
  if (btn) btn.textContent = 'Stop Listening';
}

async function submitWaveAssign() {
  const resultEl = document.getElementById('wave-assign-result');
  const waveId = document.getElementById('wave-id').value.trim();
  const taskIds = document.getElementById('wave-task-ids').value.split(',').map(s => s.trim()).filter(Boolean);
  if (!waveId || taskIds.length === 0) { resultEl.textContent = 'Wave ID and at least one Task ID are required.'; return; }
  const res = await apiFetch('/api/v1/wms/wave/assign', {
    method: 'POST',
    body: JSON.stringify({ wave_id: waveId, task_ids: taskIds })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to tag tasks into wave.', 'Wave Assign Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Tagged ${data.tagged} of ${taskIds.length} task(s)</span> into wave ${waveId}.`;
  // Same class as the renderExecDashboard null-deref: this runs after an
  // await, so the user may have navigated away and taken the form with them.
  // Writing to a detached `resultEl` above is harmless; a null getElementById
  // here is not. The request itself already succeeded either way - only the
  // convenience refill of the form is skipped.
  const waveIdInput = document.getElementById('wave-gen-id');
  if (waveIdInput) waveIdInput.value = waveId;
}

async function submitWavePickList() {
  const resultEl = document.getElementById('wave-pick-result');
  const waveId = document.getElementById('wave-gen-id').value.trim();
  if (!waveId) return;
  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to generate wave pick list.', 'Wave Pick List Failed'); return; }
  const data = await res.json();
  // Stage 42.1.5: the Batch/Expiry column appears only when this wave actually
  // allocated lots (FEFO), so a non-traceability warehouse sees the same six
  // columns it always has.
  const waveHasBatches = (data.pick_lines || []).some(l => l.batch_no);
  let html = `<h3 style="font-size: 14px; font-weight: 700; margin: 16px 0 8px;">Consolidated Pick List (zone/aisle/rack walking order)</h3>`;
  if (waveHasBatches) {
    html += `<p class="text-muted" style="font-size:12px; margin:0 0 8px;">Lines are allocated <b>FEFO</b> (first-expiry-first-out) &mdash; pick the exact batch shown. Expired lots and lots inside the item's minimum remaining shelf life are excluded automatically.</p>`;
  }
  html += `<table><thead><tr><th>Zone</th><th>Aisle</th><th>Rack</th><th>Bin</th><th>SKU</th>${waveHasBatches ? '<th>Batch / Expiry</th>' : ''}<th>Pick Qty</th></tr></thead><tbody>`;
  html += data.pick_lines.length === 0
    ? `<tr><td colspan="${waveHasBatches ? 7 : 6}" style="text-align:center; color:var(--text-muted);">No pick lines &mdash; nothing in this wave is currently stored in a bin.</td></tr>`
    : data.pick_lines.map(l => `<tr><td>${l.zone || ''}</td><td>${l.aisle || ''}</td><td>${l.rack || ''}</td><td>${l.bin_code}</td><td>${l.sku}</td>${waveHasBatches ? `<td>${batchCellHTML(l)}</td>` : ''}<td>${l.pick_qty}</td></tr>`).join('');
  html += `</tbody></table>`;
  html += `<h3 style="font-size: 14px; font-weight: 700; margin: 20px 0 8px;">Per-Order Allocation</h3>`;
  html += `<table><thead><tr><th>Task ID</th><th>SKU</th><th>Allocated Qty</th><th>Shortfall</th></tr></thead><tbody>`;
  html += data.allocations.length === 0
    ? `<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No allocations &mdash; no stock could be reserved for these orders. Check on-hand quantities under <b>Inventory</b>.</td></tr>`
    : data.allocations.map(a => `<tr><td>${a.task_id}</td><td>${a.sku}</td><td>${a.allocated_qty}</td><td>${a.shortfall ? `<span class="badge badge-warning">${a.shortfall}</span>` : '0'}</td></tr>`).join('');
  html += `</tbody></table>`;
  resultEl.innerHTML = html;
}

// Cycle Count session lines are created the same way every other line-item
// bulk load happens in this repo (Stage 20.21: reuses BulkImportCSV rather
// than a bespoke entry form) - but CycleCountLine is a Transaction doctype,
// and the Setup submenu's generic doctype browser only lists Master
// doctypes (renderSidebarSubmenu's `document_type === 'Master'` filter), so
// there was previously no way to reach CycleCountLine's generic table (and
// therefore its Bulk Import button) from the UI at all. Fixed here by
// linking directly into the existing generic doctype-table view instead of
// building a second import mechanism.
async function renderCycleCountView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Cycle Count</h1>
      <p class="page-subtitle">Reconcile a count session: zero-variance lines post immediately, non-zero variance routes to approval.</p>
    </div>
  `;
  container.appendChild(header);

  const importPanel = document.createElement('div');
  importPanel.className = 'table-panel';
  importPanel.style.padding = '24px';
  importPanel.style.marginBottom = '24px';
  importPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">1. Enter counted quantities</h2>
    <p style="color: var(--text-muted); margin-bottom: 12px;">Count lines are entered the same way as any other bulk line-item load, via Bulk Import.</p>
    <button class="btn btn-outline" id="cyclecount-open-lines-btn" type="button">Manage Count Lines</button>
  `;
  container.appendChild(importPanel);

  const reconcilePanel = document.createElement('div');
  reconcilePanel.className = 'table-panel';
  reconcilePanel.style.padding = '24px';
  reconcilePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">2. Reconcile a session</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="cyclecount-session">Count Session</label>
        <input type="text" id="cyclecount-session" class="form-input" style="width: 220px;">
      </div>
      <button class="btn btn-primary" id="cyclecount-reconcile-btn" type="button">Reconcile Session</button>
    </div>
    <div id="cyclecount-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
    <div id="cyclecount-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(reconcilePanel);

  // Stage 26.5.10: a non-zero-variance line cannot post until it has a
  // variance root-cause reason code - set one here, then (if the line was
  // already Approved before the reason existed) retry the post directly.
  const variancePanel = document.createElement('div');
  variancePanel.className = 'table-panel';
  variancePanel.style.padding = '24px';
  variancePanel.style.marginTop = '24px';
  variancePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">3. Variance root-cause + posting</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="ccvariance-line-id">Line ID</label>
        <input type="text" id="ccvariance-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="ccvariance-reason">Reason Code (ReasonCode ID)</label>
        <input type="text" id="ccvariance-reason" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="ccvariance-set-btn" type="button">Set Variance Reason</button>
      <button class="btn btn-primary" id="ccvariance-post-btn" type="button">Retry Post</button>
    </div>
    <div id="ccvariance-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(variancePanel);

  // Stage 26.5.10: blind recount - a second, blind count on a line whose
  // first result looks wrong, before it's trusted enough to post.
  const recountPanel = document.createElement('div');
  recountPanel.className = 'table-panel';
  recountPanel.style.padding = '24px';
  recountPanel.style.marginTop = '24px';
  recountPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">4. Blind recount</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-orig-line-id">Original Line ID</label>
        <input type="text" id="recount-orig-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="recount-request-btn" type="button">Request Recount</button>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-new-line-id">Recount Line ID</label>
        <input type="text" id="recount-new-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-value">Counted Qty (blind)</label>
        <input type="number" id="recount-value" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-outline" id="recount-submit-btn" type="button">Submit Recount Value</button>
    </div>
    <div id="recount-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(recountPanel);

  // Stage 26.5.9: ABC cycle-count planner - which SKUs are due for their
  // next count, ranked by velocity tier.
  const abcPanel = document.createElement('div');
  abcPanel.className = 'table-panel';
  abcPanel.style.padding = '24px';
  abcPanel.style.marginTop = '24px';
  abcPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">5. ABC cycle-count planner</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="abc-location">Location</label>
        <input type="text" id="abc-location" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="abc-plan-btn" type="button">Get Plan</button>
    </div>
    <div id="abc-plan-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(abcPanel);

  document.getElementById('cyclecount-open-lines-btn').addEventListener('click', () => {
    currentDoctype = 'CycleCountLine';
    currentSearchQuery = '';
    currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('cyclecount-reconcile-btn').addEventListener('click', submitCycleCountReconcile);
  document.getElementById('ccvariance-set-btn').addEventListener('click', submitCycleCountVarianceReason);
  document.getElementById('ccvariance-post-btn').addEventListener('click', submitRetryCycleCountPost);
  document.getElementById('recount-request-btn').addEventListener('click', submitRequestRecount);
  document.getElementById('recount-submit-btn').addEventListener('click', submitRecountValue);
  document.getElementById('abc-plan-btn').addEventListener('click', fetchABCCycleCountPlan);
  attachLinkTypeahead(document.getElementById('abc-location'), 'Location');
}

async function submitCycleCountReconcile() {
  const errorEl = document.getElementById('cyclecount-form-error');
  errorEl.classList.add('hidden');
  const resultEl = document.getElementById('cyclecount-result');
  resultEl.innerHTML = '';

  const countSession = document.getElementById('cyclecount-session').value.trim();
  if (!countSession) {
    errorEl.textContent = 'Count Session is required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/cycle-count/reconcile', {
    method: 'POST',
    body: JSON.stringify({ count_session: countSession })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to reconcile the count session.', 'Reconcile Failed');
    return;
  }
  const data = await res.json();
  resultEl.innerHTML = `
    <span class="badge badge-success">${data.posted_no_variance || 0} posted (no variance)</span>
    &nbsp;
    <span class="badge badge-warning">${data.pending_approval || 0} pending approval</span>
  `;
}

async function submitCycleCountVarianceReason() {
  const resultEl = document.getElementById('ccvariance-result');
  const lineId = document.getElementById('ccvariance-line-id').value.trim();
  const reasonCode = document.getElementById('ccvariance-reason').value.trim();
  if (!lineId || !reasonCode) { resultEl.textContent = 'Line ID and Reason Code are both required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/variance-reason', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId, reason_code: reasonCode })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to set variance reason.', 'Set Reason Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Variance reason set</span> on line ${lineId}.`;
}

async function submitRetryCycleCountPost() {
  const resultEl = document.getElementById('ccvariance-result');
  const lineId = document.getElementById('ccvariance-line-id').value.trim();
  if (!lineId) { resultEl.textContent = 'Line ID is required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/post-adjustment', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to post the adjustment.', 'Post Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Posted</span> line ${lineId}.`;
}

async function submitRequestRecount() {
  const resultEl = document.getElementById('recount-result');
  const origLineId = document.getElementById('recount-orig-line-id').value.trim();
  if (!origLineId) { resultEl.textContent = 'Original Line ID is required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/recount/request', {
    method: 'POST',
    body: JSON.stringify({ line_id: origLineId })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to request recount.', 'Recount Request Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Recount line ${data.new_line_id} created</span> (blind - no counted/system qty carried over). Enter its value below.`;
  const recountInput = document.getElementById('recount-new-line-id');
  if (recountInput) recountInput.value = data.new_line_id;
}

async function submitRecountValue() {
  const resultEl = document.getElementById('recount-result');
  const lineId = document.getElementById('recount-new-line-id').value.trim();
  const countedQty = parseFloat(document.getElementById('recount-value').value);
  if (!lineId || isNaN(countedQty)) { resultEl.textContent = 'Recount Line ID and a Counted Qty are both required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/recount/submit', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId, counted_qty: countedQty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to submit recount value.', 'Recount Submit Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Recount value ${countedQty} recorded</span> on ${lineId}. Reconcile its count_session above to post it.`;
}

async function fetchABCCycleCountPlan() {
  const resultEl = document.getElementById('abc-plan-result');
  const location = document.getElementById('abc-location').value.trim();
  if (!location) return;
  const res = await apiFetch(`/api/v1/wms/cycle-count/abc-plan?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to fetch the ABC cycle-count plan.', 'Fetch Failed'); return; }
  const plan = await res.json();
  if (plan.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No SKUs on hand at ${location}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>SKU</th><th>Tier</th><th>Daily Velocity</th><th>Days Since Last Count</th><th>Interval</th><th>Due</th></tr></thead>
      <tbody>
        ${plan.map(s => `
          <tr>
            <td style="font-family: monospace;">${s.sku}</td>
            <td><span class="badge badge-secondary">${s.tier}</span></td>
            <td>${s.daily_velocity.toFixed(2)}</td>
            <td>${s.days_since_last_count < 0 ? 'never' : s.days_since_last_count}</td>
            <td>${s.interval_days}d</td>
            <td>${s.due ? '<span class="badge badge-warning">Due</span>' : ''}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

// Unified OMS workbench: the order-to-cash operational view over the
// existing SalesOrder, FulfillmentTask, LogisticsBooking, and SalesInvoice
// doctypes. It deliberately reads through the generic document API rather
// than creating a second read model/API for data already available there.
// ---------------------------------------------------------------------------
// The OMS Console (Stage 35.2)
//
// What this replaces, and why: the previous version of this screen fetched
// GET /api/v1/doc/SalesOrder, /FulfillmentTask, /LogisticsBooking and
// /SalesInvoice in full and joined them in the browser. That has no filter, no
// pagination and no ordering - it transfers every order the tenant has ever
// taken on every page view, and faceting is impossible on a page that only
// holds one page of rows. All of that moved to SQL behind /api/v1/oms/*
// (engines/oms_console.go); this file now renders one page plus its facets.
//
// Everything here is built from the existing vocabulary: .table-panel,
// .stat-card, .btn/.action-btn, .badge, .bulk-edit-bar and the
// .modal-overlay/.modal-container primitives. No new table implementation, no
// new dialog mechanism, no framework.
// ---------------------------------------------------------------------------

const FULFILLMENT_STATUS_BADGE = {
  Pending: 'badge-warning',
  Picking: 'badge-secondary',
  Packed: 'badge-secondary',
  Dispatched: 'badge-success',
  Rejected: 'badge-danger'
};

export { renderFulfillmentView, renderFulfillmentActions, transitionFulfillmentTask, renderPutawayView, submitPlannedCrossDockPutaway, checkCrossDockOpportunity, submitCrossDockPutaway, suggestPutawayBin, submitPutaway, renderPlaceHoldView, submitPlaceHold, renderYardBoardView, loadYardBoard, submitYardCheckIn, submitYardCheckInInner, patchYardCheckIn, renderAppointmentCalendarView, shiftCalendarDate, calendarWeekDates, loadAppointmentCalendar, timeToMinutes, apptChip, submitNewAppointment, submitNewAppointmentInner, renderRFReceivingView, loadRFReceivingASN, renderRFReceivingBody, confirmRFScan, postRFReceipt, renderSortationView, loadSortationSlots, sortationAssignSlot, renderLoadingDockView, createLoadingDockTask, createLoadingDockTaskInner, loadLoadingDock, renderBOLPrintSheet, renderWarehouseCockpitView, loadWarehouseCockpit, renderBinConditionsView, submitBinConditionTransition, renderLPNView, submitLPNAssign, lookupLPNContents, renderBinReplenishmentView, fetchBinReplenishmentSuggestions, renderLocationMovementView, loadLocationMovementStock, renderWavePickingView, renderMobilePickingView, loadMobilePickList, renderMobilePickCard, speakMobilePickLine, toggleMobilePickListening, submitWaveAssign, submitWavePickList, renderCycleCountView, submitCycleCountReconcile, submitCycleCountVarianceReason, submitRetryCycleCountPost, submitRequestRecount, submitRecountValue, fetchABCCycleCountPlan };
