// Manufacturing (Stage 13.13e, scoped MVP) - single-level BOM + a linear
// Production Order (Draft -> Material Issued -> Completed). BOM's
// "components" field is JSON under the hood; this screen offers a simple
// "sku:qty, sku:qty" shorthand input instead of asking a user to hand-type
// JSON (BOM can still be edited directly via Master Definition if needed -
// it's a Master-type doctype, so it already has a generic CRUD screen there).
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell; named
// exports preserve the legacy globals needed by existing screen callbacks.
let currentMfgTab = 'orders';
const MFG_TABS = [
  { id: 'orders', label: 'Orders' },
  { id: 'quality', label: 'Quality Inspections' },
  { id: 'mrp', label: 'MRP Suggestions' },
  { id: 'schedule', label: 'Production Schedule' },
  { id: 'subcontracting', label: 'Subcontracting' }
];

// Stage 26.9: Manufacturing/MRP Maturity Sprint - Work Centers and Routing
// are Master doctypes (managed under Setup, free generic form/table, same
// as every other master this Stage adds); this view adds a tab bar around
// the existing BOM/Production Order panels for Quality Inspections
// (26.9.7's QC gate, submitted here then approved on the existing
// Approvals inbox) and MRP Suggestions (26.9.5).
async function renderManufacturingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Manufacturing</h1>
      <p class="page-subtitle">Multi-level BOM, WIP-tracked production orders, QC, and MRP. Work Centers/Routing are managed under Setup.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  // Stage 45: at a narrow enough width/zoom this row is wider than the page
  // - .page-container clips overflow-x rather than scrolling it, so without
  // this the trailing tabs were just gone with no way back to them. Scoping
  // the scroll to the bar itself (not the whole page) keeps the rest of the
  // screen from picking up a horizontal scrollbar too.
  //
  // flex-shrink: 0 is load-bearing, not decorative. Verified live: setting
  // only overflow-x on a flex item inside .page-container's column flexbox
  // let the whole bar collapse to 0px height whenever the page's other
  // content was tall enough to overflow vertically - overflow-x non-visible
  // suppresses this item's automatic min-height (content-based sizing only
  // applies when overflow is 'visible'), and per the CSS Overflow spec the
  // *used* value of overflow-y becomes 'auto' here regardless of what's
  // declared (confirmed: getComputedStyle still reports 'auto' even with
  // overflow-y explicitly set to 'visible' below - that line documents
  // intent but does not itself prevent the collapse). flex-shrink: 0 is what
  // actually fixes it: it exempts the item from shrinking at all, which
  // sidesteps the automatic-minimum-size calculation entirely rather than
  // trying to win it.
  tabBar.style.overflowX = 'auto';
  tabBar.style.overflowY = 'visible';
  tabBar.style.flexShrink = '0';
  tabBar.innerHTML = MFG_TABS.map(t =>
    `<button class="btn ${t.id === currentMfgTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-mfg-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-mfg-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentMfgTab = btn.getAttribute('data-mfg-tab');
      renderView('manufacturing');
    });
  });

  if (currentMfgTab === 'orders') {
    await renderManufacturingOrdersTab(container);
  } else if (currentMfgTab === 'quality') {
    currentDoctype = 'QualityInspection';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('manufacturing', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentMfgTab === 'mrp') {
    await renderMRPSuggestionsTab(container);
  } else if (currentMfgTab === 'schedule') {
    await renderProductionScheduleTab(container);
  } else if (currentMfgTab === 'subcontracting') {
    currentDoctype = 'SubcontractOrder';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('manufacturing', container, LAZY_VIEW_MODULES['doctype-table']);
  }
}

async function renderManufacturingOrdersTab(container) {
  const [bomsRes, ordersRes] = await Promise.all([
    apiFetch('/api/v1/doc/BOM'),
    apiFetch('/api/v1/doc/ProductionOrder')
  ]);
  if (!bomsRes || !ordersRes) return;

  const boms = bomsRes.ok ? await bomsRes.json() : [];
  const orders = ordersRes.ok ? await ordersRes.json() : [];

  const bomFormPanel = document.createElement('div');
  bomFormPanel.className = 'table-panel';
  bomFormPanel.style.padding = '24px';
  bomFormPanel.style.marginBottom = '24px';
  bomFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">New BOM</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">For multi-level sub-assemblies, per-line scrap %, by-products, default/effective-dated alternates, QC requirement, or standard cost, edit the BOM under Setup &rarr; BOM after creating it here.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bom-code">BOM Code</label>
        <input type="text" id="bom-code" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bom-parent-item">Parent Item (Finished Good SKU)</label>
        <input type="text" id="bom-parent-item" class="form-input" style="width: 180px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 220px;">
        <label class="form-label" for="bom-components">Components (sku:qty, sku:qty, ...)</label>
        <input type="text" id="bom-components" class="form-input" placeholder="e.g. RAW-A:2, RAW-B:1">
      </div>
      <button class="btn btn-primary" id="bom-create-btn">Create BOM</button>
    </div>
    <div id="bom-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(bomFormPanel);

  const orderFormPanel = document.createElement('div');
  orderFormPanel.className = 'table-panel';
  orderFormPanel.style.padding = '24px';
  orderFormPanel.style.marginBottom = '24px';
  orderFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Production Order</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Order Number', 'PRO', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-bom">BOM</label>
        <select id="po-mfg-bom" class="form-input" style="width: 200px;">
          <option value="">Select a BOM</option>
          ${boms.map(b => `<option value="${b.code || b.id}">${b.code || b.id} (${b.parent_item || ''})</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-qty">Quantity</label>
        <input type="number" id="po-mfg-qty" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-location">Location</label>
        <input type="text" id="po-mfg-location" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-primary" id="po-mfg-create-btn">Create Order</button>
    </div>
    <div id="po-mfg-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(orderFormPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Order #</th><th>BOM</th><th>Quantity</th><th>Location</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += orders.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No production orders yet. Use <b>Create BOM</b> first if you have none, then <b>Create Order</b>.</td></tr>`
    : orders.map(o => `
        <tr>
          <td style="font-family: monospace;">${o.code || o.id}</td>
          <td>${o.bom_id || ''}</td>
          <td>${o.quantity ?? ''}</td>
          <td>${o.location || ''}</td>
          <td><span class="badge ${o.status === 'Completed' ? 'badge-success' : 'badge-secondary'}">${o.status}</span></td>
          <td>${renderProductionOrderActions(o)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('bom-create-btn').addEventListener('click', createBOM);
  document.getElementById('po-mfg-create-btn').addEventListener('click', createProductionOrder);
  attachLinkTypeahead(document.getElementById('bom-parent-item'), 'Item');
  attachLinkTypeahead(document.getElementById('po-mfg-location'), 'Location');
}

function renderProductionOrderActions(order) {
  if (order.status === 'Draft') {
    return `<button class="action-btn" ${actionAttrs('issueProductionMaterial', [order.id])}>Issue Material</button>`;
  }
  if (order.status === 'Material Issued' || order.status === 'In Process') {
    // Stage 26.9.3/26.9.4/26.9.6: WIP actions alongside the original
    // one-shot Complete - Confirm Operation/Report Partial/Scrap/Rework are
    // all additive, an order that never uses any of them behaves exactly
    // as it did before this Stage.
    return `
      <button class="action-btn" ${actionAttrs('completeProductionOrder', [order.id])}>Complete (Receive FG)</button>
      <button class="action-btn" ${actionAttrs('reportPartialProductionCompletion', [order.id])}>Report Partial</button>
      <button class="action-btn" ${actionAttrs('confirmProductionOperation', [order.id])}>Confirm Operation</button>
      <button class="action-btn" ${actionAttrs('postProductionScrap', [order.id])}>Scrap</button>
      <button class="action-btn" ${actionAttrs('sendProductionToRework', [order.id])}>Rework</button>
    `;
  }
  if (order.status === 'Completed' && (order.actual_cost === undefined || order.actual_cost === null)) {
    return `<button class="action-btn" ${actionAttrs('recordProductionActualCost', [order.id])}>Record Actual Cost</button>`;
  }
  return '';
}

async function reportPartialProductionCompletion(orderId) {
  const qtyStr = await showCustomPrompt('Quantity to report as completed in this batch:', '', 'Report Partial Completion');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/partial-complete', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to report partial completion.', 'Partial Completion Failed');
    return;
  }
  renderView('manufacturing');
}

export {
  renderManufacturingView,
  renderManufacturingOrdersTab,
  renderProductionOrderActions,
  reportPartialProductionCompletion,
  confirmProductionOperation,
  postProductionScrap,
  sendProductionToRework,
  recordProductionActualCost,
  renderMRPSuggestionsTab,
  runMRPSuggestions,
  renderProductionScheduleTab,
  loadProductionSchedule,
  createBOM,
  createBOMInner,
  createProductionOrder,
  createProductionOrderInner,
  issueProductionMaterial,
  completeProductionOrder
};

async function confirmProductionOperation(orderId) {
  const seqStr = await showCustomPrompt('Operation sequence number to confirm (from the order\'s Routing):', '1', 'Confirm Operation');
  if (seqStr === null || seqStr === '') return;
  const seq = parseInt(seqStr, 10);
  const res = await apiFetch('/api/v1/manufacturing/confirm-operation', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, seq })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    await showApiError(res, 'Failed to confirm operation.', 'Confirm Operation Failed');
    return;
  }
  if (data.capacity_warning) {
    await showCustomAlert(data.capacity_warning, 'Capacity Warning');
  }
  renderView('manufacturing');
}

async function postProductionScrap(orderId) {
  const sku = await showCustomPrompt('SKU being scrapped:', '', 'Post Scrap');
  if (sku === null || sku === '') return;
  const qtyStr = await showCustomPrompt('Scrap quantity:', '', 'Post Scrap');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const reason = await showCustomPrompt('Reason for scrap (required):', '', 'Post Scrap');
  if (!reason) return;

  const res = await apiFetch('/api/v1/manufacturing/scrap', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, sku, qty, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post scrap.', 'Scrap Failed');
    return;
  }
  renderView('manufacturing');
}

async function sendProductionToRework(orderId) {
  const qtyStr = await showCustomPrompt('Quantity to send to rework:', '', 'Send to Rework');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const reason = await showCustomPrompt('Reason (required):', '', 'Send to Rework');
  if (!reason) return;

  const res = await apiFetch('/api/v1/manufacturing/rework', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, qty, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to log rework.', 'Rework Failed');
    return;
  }
  renderView('manufacturing');
}

async function recordProductionActualCost(orderId) {
  const costStr = await showCustomPrompt('Actual total cost incurred for this production order:', '', 'Record Actual Cost');
  if (costStr === null || costStr === '') return;
  const cost = parseFloat(costStr);
  if (cost < 0 || isNaN(cost)) {
    await showCustomAlert('Enter a valid, non-negative cost.', 'Invalid Cost');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/record-actual-cost', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, actual_cost: cost })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record actual cost.', 'Record Cost Failed');
    return;
  }
  renderView('manufacturing');
}

// Stage 26.9.5: MRP reorder suggestions for manufactured items - reuses the
// existing replenishment-suggestion formula/query-param shape rather than a
// new planning engine (same as the backend).
async function renderMRPSuggestionsTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-location">Location</label>
        <input type="text" id="mrp-location" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-lead-time">Lead Time (Days)</label>
        <input type="number" id="mrp-lead-time" class="form-input" style="width: 100px;" value="7">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-safety-stock">Safety Stock</label>
        <input type="number" id="mrp-safety-stock" class="form-input" style="width: 100px;" value="0">
      </div>
      <button class="btn btn-primary" id="mrp-run-btn">Get Suggestions</button>
    </div>
  `;
  container.appendChild(formPanel);
  attachLinkTypeahead(document.getElementById('mrp-location'), 'Location');

  const resultsPanel = document.createElement('div');
  resultsPanel.id = 'mrp-results';
  container.appendChild(resultsPanel);

  document.getElementById('mrp-run-btn').addEventListener('click', runMRPSuggestions);
}

async function runMRPSuggestions() {
  const location = document.getElementById('mrp-location').value.trim();
  const resultsEl = document.getElementById('mrp-results');
  if (!location) {
    resultsEl.innerHTML = `<div class="login-error" style="margin-top: 8px;">Location is required.</div>`;
    return;
  }
  const leadTime = document.getElementById('mrp-lead-time').value || '7';
  const safetyStock = document.getElementById('mrp-safety-stock').value || '0';

  const res = await apiFetch(`/api/v1/manufacturing/mrp-suggestions?location=${encodeURIComponent(location)}&lead_time_days=${leadTime}&safety_stock=${safetyStock}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to fetch MRP suggestions.');
    return;
  }
  const suggestions = await res.json();

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  if (suggestions.length === 0) {
    panel.innerHTML = `<div style="padding: 24px; text-align:center; color:var(--text-muted);">No manufactured items are below their reorder point at this location. Set a reorder point on a manufactured Item, or pick a different location above.</div>`;
  } else {
    let html = `
      <table>
        <thead><tr><th>Item</th><th>Available</th><th>Reorder Point</th><th>Suggested Production Qty</th><th>BOM</th><th>Raw Material Shortfalls</th></tr></thead>
        <tbody>
    `;
    html += suggestions.map(s => `
      <tr>
        <td>${s.parent_item}</td>
        <td>${s.available}</td>
        <td>${s.reorder_point}</td>
        <td>${s.suggested_production_qty}</td>
        <td style="font-family: monospace;">${s.bom_id || '-'}</td>
        <td>${(s.raw_material_shortfalls || []).length === 0 ? 'None' :
          s.raw_material_shortfalls.map(r => `${r.sku}: need ${r.shortfall_qty.toFixed ? r.shortfall_qty.toFixed(2) : r.shortfall_qty} more`).join('<br>')}</td>
      </tr>
    `).join('');
    html += `</tbody></table>`;
    panel.innerHTML = html;
  }
  resultsEl.innerHTML = '';
  resultsEl.appendChild(panel);
}

// 26.9.10 (P2, go-ahead 2026-07-27): finite/infinite capacity scheduling -
// a read-only suggestion, same "GET on render + Refresh button" shape as
// the MRP Suggestions tab above, one table per page.
async function renderProductionScheduleTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '16px';
  panel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
      <p class="text-muted" style="margin:0; font-size:13px;">Every open, routed production order's operations, sequenced earliest-due-first against each work center's daily capacity. Finite respects that capacity (pushing overflow to a later day); Infinite ignores it - the gap between the two columns is how much capacity is actually stretching the schedule out.</p>
      <button class="btn btn-outline" id="mfg-schedule-refresh">Refresh</button>
    </div>
    <div id="mfg-schedule-results"></div>
  `;
  container.appendChild(panel);
  document.getElementById('mfg-schedule-refresh').addEventListener('click', loadProductionSchedule);
  await loadProductionSchedule();
}

async function loadProductionSchedule() {
  const resultsEl = document.getElementById('mfg-schedule-results');
  if (!resultsEl) return;
  resultsEl.innerHTML = `<div class="text-muted" style="padding:16px;">Loading...</div>`;
  const res = await apiFetch('/api/v1/manufacturing/production-schedule');
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to fetch the production schedule.');
    return;
  }
  const entries = await res.json();
  if (!entries || entries.length === 0) {
    resultsEl.innerHTML = `<div style="padding:24px; text-align:center; color:var(--text-muted);">No open, routed production orders to schedule. Create one under the <b>Orders</b> tab; its BOM needs a Routing before it can be scheduled.</div>`;
    return;
  }
  let html = `
    <table>
      <thead><tr><th>Order</th><th>Op #</th><th>Work Center</th><th>Needed (min)</th><th>Finite Date</th><th>Infinite Date</th><th>Past Due?</th></tr></thead>
      <tbody>
  `;
  html += entries.map(e => `
    <tr>
      <td>${copyableCell(e.order_id, e.order_id)}</td>
      <td>${e.seq}</td>
      <td>${e.work_center_id || '-'}</td>
      <td>${Math.round(e.needed_minutes)}</td>
      <td>${e.finite_date}</td>
      <td>${e.infinite_date}</td>
      <td>${e.overflow ? '<span class="badge badge-warning">Yes</span>' : ''}</td>
    </tr>
  `).join('');
  html += `</tbody></table>`;
  resultsEl.innerHTML = html;
}

async function createBOM() {
  // BLD-036: guard against a double-click creating two BOMs.
  await guardAgainstDoubleSubmit(document.getElementById('bom-create-btn'), 'Creating...', createBOMInner);
}

async function createBOMInner() {
  const errorEl = document.getElementById('bom-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('bom-code').value.trim();
  const parentItem = document.getElementById('bom-parent-item').value.trim();
  const componentsRaw = document.getElementById('bom-components').value.trim();

  if (!code || !parentItem || !componentsRaw) {
    errorEl.textContent = 'BOM Code, Parent Item, and Components are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  let components;
  try {
    components = componentsRaw.split(',').map(part => {
      const [sku, qty] = part.split(':').map(s => s.trim());
      if (!sku || !qty || isNaN(parseFloat(qty))) throw new Error('bad format');
      return { sku, qty: parseFloat(qty) };
    });
  } catch (e) {
    errorEl.textContent = 'Components must look like "SKU:QTY, SKU:QTY" (e.g. RAW-A:2, RAW-B:1).';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/BOM', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, parent_item: parentItem,
      components: JSON.stringify(components), status: 'Active'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create BOM.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('manufacturing');
}

async function createProductionOrder() {
  // BLD-036: guard against a double-click creating two ProductionOrders.
  await guardAgainstDoubleSubmit(document.getElementById('po-mfg-create-btn'), 'Creating...', createProductionOrderInner);
}

async function createProductionOrderInner() {
  const errorEl = document.getElementById('po-mfg-form-error');
  errorEl.classList.add('hidden');

  const bomId = document.getElementById('po-mfg-bom').value;
  const quantity = parseFloat(document.getElementById('po-mfg-qty').value);
  const location = document.getElementById('po-mfg-location').value.trim();

  if (!bomId || !quantity || !location) {
    errorEl.textContent = 'BOM, Quantity, and Location are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ProductionOrder', {
    method: 'POST',
    body: JSON.stringify({ bom_id: bomId, quantity, location, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create production order.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('manufacturing');
}

async function issueProductionMaterial(orderId) {
  const res = await apiFetch('/api/v1/manufacturing/issue-material', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to issue material.', 'Material Issue Failed');
    return;
  }
  renderView('manufacturing');
}

async function completeProductionOrder(orderId) {
  const confirmed = await showCustomConfirm('This will receive the finished goods into inventory and close the order. Continue?', 'Complete Production Order');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/manufacturing/complete', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId })
  });
  if (!res) return;
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    // Stage 26.9.2 (MFG-0276): the BOM changed after material was issued -
    // offer to acknowledge the variance and retry, rather than a dead end.
    if (data.code === 'MFG-0276') {
      const ack = await showCustomConfirm(data.error + ' Acknowledge the variance and complete anyway?', 'BOM Changed Since Issue');
      if (ack) {
        const ackRes = await apiFetch('/api/v1/manufacturing/acknowledge-bom-variance', {
          method: 'POST', body: JSON.stringify({ order_id: orderId })
        });
        if (ackRes && ackRes.ok) {
          return completeProductionOrder(orderId);
        }
      }
      return;
    }
    await showApiError(res, 'Failed to complete production order.', 'Completion Failed');
    return;
  }
  renderView('manufacturing');
}
