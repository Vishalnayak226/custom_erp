import { documentSearchLink } from './view-document-links.js';

// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
// Stage 88.2: the Fulfillment screen that stood here is retired - its pick,
// pack and dispatch steps are OMS ▸ Pick / Pack / Ship (view-oms-steps.js).

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
    </div>
  `;
  container.appendChild(header);

  // Stage 55.7: what is waiting to go into a bin at a location - received
  // stock not yet binned - so the operator picks a line instead of recalling it.
  const waitPanel = document.createElement('div');
  waitPanel.className = 'table-panel';
  waitPanel.style.padding = '24px';
  waitPanel.style.marginBottom = '24px';
  waitPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <h2 style="font-size: 16px; font-weight: 700; margin: 0; flex: 1;">Waiting for putaway</h2>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-wait-location">Location</label>
        <input type="text" id="putaway-wait-location" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
    </div>
    <div id="putaway-waiting" style="margin-top: 12px;"></div>`;
  container.appendChild(waitPanel);

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
        <label class="form-label" for="putaway-owner">Owner (3PL client, optional)</label>
        <input type="text" id="putaway-owner" class="form-input" style="width: 160px;" autocomplete="off" placeholder="Whose stock">
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
  // Stage 55.7: scanner order - bin, Enter, item, Enter, quantity, Enter puts it away.
  [['putaway-bin', 'putaway-sku'], ['putaway-sku', 'putaway-qty']].forEach(([from, to]) =>
    document.getElementById(from).addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); document.getElementById(to).focus(); document.getElementById(to).select?.(); } }));
  document.getElementById('putaway-qty').addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); submitPutaway(); } });
  const waitLoc = document.getElementById('putaway-wait-location');
  attachLinkTypeahead(waitLoc, 'Location');
  if (inboundLocation) waitLoc.value = inboundLocation;
  waitLoc.addEventListener('change', () => { inboundLocation = waitLoc.value.trim(); loadPutawayWaiting(); });
  loadPutawayWaiting();

  // Cross-dock is a different decision from shelving: kept, but folded away
  // as an explicit variant rather than two more blank forms beside this one.
  const xdockWrap = document.createElement('details');
  xdockWrap.className = 'table-panel';
  xdockWrap.style.padding = '16px 24px';
  xdockWrap.style.marginTop = '24px';
  xdockWrap.innerHTML = '<summary style="cursor: pointer; font-weight: 600;">Cross-dock instead of shelving (advanced)</summary>';
  container.appendChild(xdockWrap);

  // Stage 26.5.3: cross-dock/flow-through putaway - an alternative to
  // shelving when a transfer/sale is already waiting on this exact SKU at
  // this location, skipping bin placement in favor of a staging bin.
  const xdockPanel = document.createElement('div');
  xdockPanel.style.padding = '16px 0 0';
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
  xdockWrap.appendChild(xdockPanel);
  document.getElementById('xdock-check-btn').addEventListener('click', checkCrossDockOpportunity);
  document.getElementById('xdock-stage-btn').addEventListener('click', submitCrossDockPutaway);
  attachLinkTypeahead(document.getElementById('xdock-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('xdock-location'), 'Location');

  // Stage 42.3.8: planned cross-dock/flow-through/transship - stages against
  // a CrossDockPlan raised ahead of receipt (Cross-Dock Plans in the sidebar)
  // instead of scanning for live opportunistic demand like the panel above.
  const planPanel = document.createElement('div');
  planPanel.style.padding = '24px 0 0';
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
  xdockWrap.appendChild(planPanel);
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
    // Stage 59.4: in a warehouse shared by several 3PL owners, say whose.
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty, owner_id: (document.getElementById('putaway-owner')?.value || '').trim() })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to put away stock.', 'Putaway Failed');
    return;
  }
  showToast(`Put away ${qty} x ${sku} into bin ${binCode}.`, { variant: 'success' });
  document.getElementById('putaway-qty').value = 1;
  document.getElementById('putaway-bin').value = '';
  document.getElementById('putaway-sku').value = '';
  document.getElementById('putaway-suggest-result').textContent = '';
  await loadPutawayWaiting();
  document.getElementById('putaway-bin').focus();
}

// loadPutawayWaiting lists received stock not yet in a bin (Stage 55.7); a
// row's Put away fills the form below and asks for a bin suggestion.
async function loadPutawayWaiting() {
  const el = document.getElementById('putaway-waiting');
  if (!el) return;
  const loc = (document.getElementById('putaway-wait-location')?.value || '').trim();
  const res = await apiFetch(`/api/v1/wms/inbound-work?location=${encodeURIComponent(loc)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load what is waiting for putaway.', 'Putaway'); return; }
  const work = await res.json();
  const rows = work.putaway || [];
  if (!work.has_bins) {
    el.innerHTML = `<p style="margin: 0; color: var(--text-muted);">No bins are set up${loc ? ' at ' + escapeHTMLText(loc) : ''} yet - putaway places stock into bins. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['Bin'])}>Set up bins</button></p>`;
    return;
  }
  el.innerHTML = rows.length === 0 ? `<p style="margin: 0; color: var(--text-muted);">Nothing waiting${loc ? ' at ' + escapeHTMLText(loc) : ''} - every received unit is in a bin.</p>` : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Item</th><th>Location</th><th class="num">On hand</th><th class="num">In bins</th><th class="num">Waiting</th><th></th></tr></thead>
    <tbody>${rows.map(p => `<tr><td>${escapeHTMLText(p.name || p.sku)}<div style="font-size: 11px; color: var(--text-muted);">${escapeHTMLText(p.sku)}</div></td>
      <td>${escapeHTMLText(p.location)}</td><td class="num">${p.on_hand}</td><td class="num">${p.binned}</td><td class="num"><b>${p.waiting}</b></td>
      <td><button type="button" class="btn btn-outline btn-sm" data-putaway-sku="${escapeHTMLText(p.sku)}" data-putaway-loc="${escapeHTMLText(p.location)}" data-putaway-qty="${p.waiting}">Put away</button></td></tr>`).join('')}</tbody></table></div>`;
  el.querySelectorAll('[data-putaway-sku]').forEach(btn => btn.addEventListener('click', async () => {
    document.getElementById('putaway-sku').value = btn.dataset.putawaySku;
    document.getElementById('putaway-location').value = btn.dataset.putawayLoc;
    document.getElementById('putaway-qty').value = btn.dataset.putawayQty;
    document.getElementById('putaway-bin').value = '';
    await suggestPutawayBin();
    const bin = document.getElementById('putaway-bin');
    bin.focus();
    bin.select?.();
  }));
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
      <a href="#/view/inventory">← Back to Inventory</a>
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
    <div id="hold-impact" aria-live="polite" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);">Choose the item and location to see what a hold takes out of sellable stock.</div>
    <div id="hold-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
    <div id="hold-result" aria-live="polite" style="margin-top:16px;"></div>
  `;
  container.appendChild(panel);

  document.getElementById('hold-submit-btn').addEventListener('click', submitPlaceHold);
  // Stage 55.6: what the hold will do to sellable stock, before it is placed.
  ['hold-sku', 'hold-location', 'hold-qty'].forEach(id => document.getElementById(id).addEventListener('change', showHoldImpact));
  attachLinkTypeahead(document.getElementById('hold-code'), 'HoldCode');
  attachLinkTypeahead(document.getElementById('hold-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('hold-location'), 'Location');
  const savedContext = sessionStorage.getItem('erp_stock_hold_context');
  sessionStorage.removeItem('erp_stock_hold_context');
  if (savedContext) {
    try {
      const context = JSON.parse(savedContext);
      document.getElementById('hold-sku').value = String(context.sku || '');
      document.getElementById('hold-location').value = String(context.location || '');
    } catch (error) { /* A stale local hint must not block a fresh hold. */ }
  }
}

async function showHoldImpact() {
  const el = document.getElementById('hold-impact');
  const sku = document.getElementById('hold-sku')?.value.trim();
  const loc = document.getElementById('hold-location')?.value.trim();
  const qty = parseInt(document.getElementById('hold-qty')?.value, 10) || 0;
  if (!el || !sku || !loc) return;
  const res = await apiFetch(`/api/v1/availability?sku=${encodeURIComponent(sku)}&location=${encodeURIComponent(loc)}`);
  if (!res || !res.ok) { el.textContent = 'Stock for this item and location could not be read.'; return; }
  const a = await res.json();
  const ats = Number(a.ats) || 0;
  el.innerHTML = `At ${escapeHTMLText(loc)}: on hand <b>${Number(a.on_hand) || 0}</b>, already held <b>${Number(a.hold_qty) || 0}</b>, available to sell <b>${ats}</b>.` +
    (qty > 0 ? ` A hold of ${qty} leaves <b>${ats - qty}</b> to sell until a release is approved${qty > ats ? ' - <span style="color: var(--danger-color);">more than is available to sell</span>' : ''}. On-hand stock does not change.` : '');
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
  const saved = await res.json();
  const result = document.getElementById('hold-result');
  if (result) result.innerHTML = `<span class="badge badge-success">Hold placed</span> ${documentSearchLink('Hold', saved.id)}. <button class="action-btn" ${actionAttrs('startHoldReleaseRequest', [saved.id])}>Request release of this hold</button> <small>Release needs a separate approval; this button does not free the stock.</small>`;
  await showCustomAlert(`Placed hold ${saved.id} for ${qty} x ${sku} at ${locationCode}.`, 'Hold Placed');
  document.getElementById('hold-qty').value = 1;
  document.getElementById('hold-reason').value = '';
}

window.startHoldReleaseRequest = async function(holdId) {
  if (!canCreateDoctype('HoldReleaseRequest')) {
    await showCustomAlert('Your role cannot request a hold release. Ask an inventory supervisor.', 'Release Needs Authorization');
    return;
  }
  currentDoctype = 'HoldReleaseRequest';
  currentSearchQuery = '';
  currentTablePage = 1;
  await renderView('doctype-table');
  if (currentView === 'doctype-table' && currentDoctype === 'HoldReleaseRequest') {
    await openDynamicModal(undefined, { hold_id: holdId, status: 'Draft' });
  }
};

// Stage 42.3.4 - Yard Board: check-in/check-out for trailers, InYard ->
// AtDoor -> Departed (validateYardCheckInMasterRules enforces the order
// server-side). Infor's 3D yard view is explicitly out of scope - this is a
// flat status board, not a spatial map.
// Stage 69: list / New pages (txn-screen.js, the standard ERP shape), loaded
// only by the screens here that record something (Yard Board, Location
// Movement).
const loadTxnScreen = () => import('./txn-screen.js' + new URL(import.meta.url).search);

async function renderYardBoardView(container) {
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="yard-appt">Booked appointment (optional)</label>
        <select id="yard-appt" class="form-select" style="width: 260px;"><option value="">Not booked</option></select>
      </div>
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
  const body = document.createElement('div');
  body.id = 'yard-board-body';
  // The board is the page; + Check In Trailer opens the check-in as its own.
  container.appendChild(txnPagedSection({ id: 'yard-screen', title: 'Yard Board', page: 'Check in a trailer',
    newLabel: 'Check In Trailer', form: panel, list: body }));
  txnRestorePane('yard-screen');
  document.getElementById('yard-checkin-btn').addEventListener('click', submitYardCheckIn);
  attachLinkTypeahead(document.getElementById('yard-trailer'), 'Trailer', { valueFields: ['code'] });
  // Stage 55.7: check a booked trailer in against its appointment.
  const apptRes = await apiFetch('/api/v1/doc/Appointment');
  const appts = apptRes && apptRes.ok ? (await apptRes.json()).filter(a => a.status === 'Scheduled' && a.appointment_date >= localISODate(new Date(Date.now() - 864e5))) : [];
  appts.sort((a, b) => `${a.appointment_date} ${a.start_time}`.localeCompare(`${b.appointment_date} ${b.start_time}`));
  const apptSel = document.getElementById('yard-appt');
  apptSel.innerHTML += appts.map(a => `<option value="${escapeHTMLText(a.id)}">${escapeHTMLText(a.appointment_date)} ${escapeHTMLText(a.start_time)} · door ${escapeHTMLText(a.dock_door)} · ${escapeHTMLText(a.appointment_type)}${a.carrier ? ' · ' + escapeHTMLText(a.carrier) : ''}${a.trailer_no ? ' · ' + escapeHTMLText(a.trailer_no) : ''}</option>`).join('');
  apptSel.addEventListener('change', () => {
    const a = appts.find(x => x.id === apptSel.value);
    if (!a) return;
    if (a.trailer_no) document.getElementById('yard-trailer').value = a.trailer_no;
    if (a.carrier) document.getElementById('yard-carrier').value = a.carrier;
  });
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
  // Stage 55.7: what each trailer brought, from its appointment.
  const apptRes = await apiFetch('/api/v1/doc/Appointment');
  const apptById = Object.fromEntries((apptRes && apptRes.ok ? await apptRes.json() : []).map(a => [a.id, a]));
  const delivery = r => {
    const a = apptById[r.appointment_id];
    if (!a) return '-';
    return `${escapeHTMLText(a.appointment_type || '')}${a.asn_id ? ` · ASN ${escapeHTMLText(a.asn_id)}` : a.po_id ? ` · PO ${escapeHTMLText(a.po_id)}` : ''}<div style="font-size: 11px; color: var(--text-muted);">booked ${escapeHTMLText(a.appointment_date)} ${escapeHTMLText(a.start_time)}, door ${escapeHTMLText(a.dock_door)}</div>`;
  };
  const receiveBtn = r => {
    const a = apptById[r.appointment_id];
    return r.status === 'AtDoor' && a && a.asn_id && a.appointment_type !== 'Outbound'
      ? `<button class="btn btn-primary btn-sm" ${actionAttrs('receiveYardASN', [a.asn_id])}>Receive</button>` : '';
  };

  const rows = active.map(r => `
    <tr>
      <td>${escapeHTMLText(r.trailer_no || '')}</td>
      <td>${escapeHTMLText(r.carrier || '-')}</td>
      <td>${escapeHTMLText(r.driver_name || '-')}</td>
      <td>${delivery(r)}</td>
      <td><span class="badge">${escapeHTMLText(r.status === 'AtDoor' ? 'At door' : r.status === 'InYard' ? 'In yard' : r.status || '')}</span></td>
      <td>${escapeHTMLText(r.dock_door || r.yard_location || '-')}</td>
      <td>
        ${receiveBtn(r)}
        ${r.status === 'InYard' ? `<button class="btn btn-outline btn-sm" ${actionAttrs('assignYardDoor', [r.id])}>Assign to Door</button>` : ''}
        <button class="btn btn-outline btn-sm" ${actionAttrs('checkOutYardTrailer', [r.id])}>Check Out</button>
      </td>
    </tr>`).join('');

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-wrapper"><table>
      <thead><tr><th>Trailer</th><th>Carrier</th><th>Driver</th><th>Delivery</th><th>Status</th><th>Door / Slot</th><th>Actions</th></tr></thead>
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
  // Stage 55.7: checked in by the server - once per trailer, and its
  // appointment (if picked) marked checked in.
  const payload = {
    trailer_no: trailerNo,
    carrier: document.getElementById('yard-carrier').value.trim(),
    driver_name: document.getElementById('yard-driver').value.trim(),
    yard_location: document.getElementById('yard-slot').value.trim(),
    appointment_id: document.getElementById('yard-appt')?.value || ''
  };
  const res = await apiFetch('/api/v1/wms/yard/check-in', { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to check in trailer.');
    return;
  }
  const apptSel = document.getElementById('yard-appt');
  if (apptSel) { apptSel.querySelector(`option[value="${CSS.escape(apptSel.value)}"]`)?.remove(); apptSel.value = ''; }
  document.getElementById('yard-trailer').value = '';
  document.getElementById('yard-carrier').value = '';
  document.getElementById('yard-driver').value = '';
  document.getElementById('yard-slot').value = '';
  (await loadTxnScreen()).txnDone('yard-screen');
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

// Stage 55.7: pick a door from the doors that exist, seeing which are taken
// and what is booked on them today; the server refuses an occupied door.
window.assignYardDoor = async function(id) {
  const [doorsRes, yardRes, apptRes] = await Promise.all([apiFetch('/api/v1/doc/DockDoor'), apiFetch('/api/v1/doc/YardCheckIn'), apiFetch('/api/v1/doc/Appointment')]);
  if (!doorsRes || !yardRes || !apptRes) return;
  const doors = doorsRes.ok ? (await doorsRes.json()).filter(d => d.status === 'Active').sort((a, b) => (a.code || '').localeCompare(b.code || '')) : [];
  const yard = yardRes.ok ? await yardRes.json() : [];
  const today = localISODate(new Date());
  const booked = apptRes.ok ? (await apptRes.json()).filter(a => a.appointment_date === today && a.status !== 'Cancelled' && a.status !== 'Completed') : [];
  document.getElementById('yard-door-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'yard-door-dialog';
  const rows = doors.map(d => {
    const at = yard.find(y => y.status === 'AtDoor' && y.dock_door === d.code && y.id !== id);
    const todays = booked.filter(a => a.dock_door === d.code).map(a => `${escapeHTMLText(a.start_time)} ${escapeHTMLText(a.trailer_no || a.carrier || a.appointment_type)}`).join(', ');
    return `<tr><td><b>${escapeHTMLText(d.code)}</b><div style="font-size: 11px; color: var(--text-muted);">${escapeHTMLText(d.door_type || '')}</div></td>
      <td>${at ? `<span class="badge badge-danger">Occupied</span> trailer ${escapeHTMLText(at.trailer_no)}` : '<span class="badge badge-success">Free</span>'}</td>
      <td style="font-size: 12px;">${todays || '-'}</td>
      <td>${at ? '' : `<button type="button" class="btn btn-primary btn-sm" data-door="${escapeHTMLText(d.code)}">Spot here</button>`}</td></tr>`;
  }).join('');
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 640px;" role="dialog" aria-modal="true" aria-labelledby="yard-door-title">
      <div class="modal-header"><h3 class="modal-title" id="yard-door-title">Assign to a dock door</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-yard-close><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></button></div>
      <div class="modal-body">${doors.length ? `<table><thead><tr><th>Door</th><th>Now</th><th>Booked today</th><th></th></tr></thead><tbody>${rows}</tbody></table>`
        : `<p>No active dock doors yet. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['DockDoor'])}>Set up dock doors</button></p>`}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-yard-close>Cancel</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-yard-close]').forEach(b => b.addEventListener('click', close));
  overlay.querySelectorAll('[data-act]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  overlay.querySelectorAll('[data-door]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Spotting...', async () => {
    const res = await apiFetch(`/api/v1/wms/yard/${encodeURIComponent(id)}/door`, { method: 'POST', body: JSON.stringify({ dock_door: btn.dataset.door }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to assign door.'); return; }
    close();
    showToast(`Trailer spotted at door ${btn.dataset.door}.`);
    await loadYardBoard();
  })));
  overlay.querySelector('[data-door]')?.focus();
};

// receiveYardASN opens RF Receiving with the trailer's ASN already loaded.
let rfPendingASN = '';
window.receiveYardASN = function(asnId) {
  rfPendingASN = asnId;
  window.location.hash = '#/view/rf-receiving';
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
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="cal-asn">ASN it brings (optional)</label><input type="text" id="cal-asn" class="form-input" style="width:150px;" autocomplete="off"></div>
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
  attachLinkTypeahead(document.getElementById('cal-asn'), 'ASN', { noSetupHint: true });

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
    body.innerHTML = `<div class="table-panel" style="padding:24px; color:var(--text-muted);">No active dock doors yet - appointments are booked against a door. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['DockDoor'])}>Set up dock doors</button></div>`;
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
  // Stage 55.7: the ASN it brings - the yard and RF receiving follow it.
  const asnId = document.getElementById('cal-asn').value.trim();
  if (asnId) {
    const ar = await apiFetch(`/api/v1/doc/ASN/${encodeURIComponent(asnId)}`);
    if (!ar || !ar.ok) { errorEl.textContent = `ASN ${asnId} not found.`; errorEl.classList.remove('hidden'); return; }
    const asn = await ar.json();
    payload.asn_id = asnId;
    if (asn.po_id) payload.po_id = asn.po_id;
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
// Stage 55.7: the line a scan picked, waiting for its quantity; and the
// location RF Receiving and Putaway share, so one hands over to the other.
let rfPendingLine = null;
let inboundLocation = '';

async function renderRFReceivingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">RF Receiving</h1>
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
      <label class="form-label" for="rf-location-input">Receiving Location</label>
      <input type="text" id="rf-location-input" class="form-input" autocomplete="off">
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="rf-asn-input">Scan or Enter ASN - or pick a delivery below</label>
      <input type="text" id="rf-asn-input" class="form-input" placeholder="ASN number, then Enter" autocomplete="off" style="font-size:18px; padding:14px;">
    </div>
    <div id="rf-setup-error" class="login-error hidden" style="margin-top: 12px;"></div>
    <div id="rf-expected"></div>
  `;
  container.appendChild(setupPanel);
  document.getElementById('rf-asn-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); loadRFReceivingASN(); } });
  const locInput = document.getElementById('rf-location-input');
  attachLinkTypeahead(locInput, 'Location');
  if (inboundLocation) locInput.value = inboundLocation;
  locInput.addEventListener('change', () => { inboundLocation = locInput.value.trim(); loadRFExpected(); });

  const body = document.createElement('div');
  body.id = 'rf-receiving-body';
  body.style.maxWidth = '480px';
  container.appendChild(body);

  rfReceivingExpected = [];
  rfReceivingLines = [];
  rfReceivingASNId = '';
  rfReceivingPOId = '';
  rfPendingLine = null;
  if (rfPendingASN) {
    document.getElementById('rf-asn-input').value = rfPendingASN;
    rfPendingASN = '';
    await loadRFReceivingASN();
  } else {
    await loadRFExpected();
  }
}

// loadRFExpected lists the deliveries expected at the location, the ones at a
// door first, so the operator picks one instead of typing its number.
async function loadRFExpected() {
  const el = document.getElementById('rf-expected');
  if (!el) return;
  const loc = (document.getElementById('rf-location-input')?.value || '').trim();
  const res = await apiFetch(`/api/v1/wms/inbound-work?location=${encodeURIComponent(loc)}`);
  if (!res || !res.ok) { el.innerHTML = ''; return; }
  const work = await res.json();
  const asns = work.asns || [];
  el.innerHTML = asns.length === 0
    ? `<p style="color: var(--text-muted); margin: 12px 0 0;">No deliveries expected${loc ? ' at ' + escapeHTMLText(loc) : ''}.</p>`
    : `<div style="margin-top: 14px; font-weight: 600;">Expected deliveries</div>` + asns.map(a => `
      <button type="button" data-rf-asn="${escapeHTMLText(a.id)}" style="display:block; width:100%; text-align:left; padding:12px; margin-top:8px; min-height:44px; border:1px solid var(--border-color); border-radius:8px; background:transparent; color:inherit; cursor:pointer;">
        <b>${escapeHTMLText(a.asn_number || a.id)}</b> · PO ${escapeHTMLText(a.po_id || '-')}${a.vendor ? ' · ' + escapeHTMLText(a.vendor) : ''}
        <div style="font-size:12px; color: var(--text-muted);">${a.lines} line(s), ${a.units} unit(s) at ${escapeHTMLText(a.location)} · ${escapeHTMLText(a.stage)}</div>
      </button>`).join('');
  el.querySelectorAll('[data-rf-asn]').forEach(b => b.addEventListener('click', () => {
    document.getElementById('rf-asn-input').value = b.dataset.rfAsn;
    loadRFReceivingASN();
  }));
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
  if (asn.status && asn.status !== 'Expected') {
    errorEl.textContent = `ASN ${asnId} is already ${asn.status} - nothing more to receive on it.`;
    errorEl.classList.remove('hidden');
    return;
  }
  let items = [];
  try { items = JSON.parse(asn.expected_items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    errorEl.textContent = `ASN ${asnId} has no recorded expected items.`;
    errorEl.classList.remove('hidden');
    return;
  }
  const locInput = document.getElementById('rf-location-input');
  if (!locInput.value.trim() && asn.location) locInput.value = asn.location;
  inboundLocation = locInput.value.trim();

  rfReceivingASNId = asnId;
  rfReceivingPOId = asn.po_id || '';
  rfReceivingExpected = items.map(it => ({ sku: it.sku || '', expectedQty: Number(it.qty) || 0, receivedQty: 0 }));
  rfReceivingLines = [];
  rfPendingLine = null;
  document.getElementById('rf-expected').innerHTML = '';
  renderRFReceivingBody();
}

function renderRFReceivingBody(status) {
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
  const pending = rfPendingLine;
  const left = pending ? pending.expectedQty - pending.receivedQty : 0;

  body.innerHTML = `
    <div class="table-panel" style="padding:20px; margin-bottom:16px;">
      <div class="text-muted" style="font-size:13px; margin-bottom:10px;">ASN ${escapeHTMLText(rfReceivingASNId)} &mdash; ${doneLines} of ${totalLines} SKU(s) fully received</div>
      <div class="form-group ${pending ? 'hidden' : ''}" style="margin-bottom:0;">
        <label class="form-label" for="rf-scan-input">Scan Carton SKU</label>
        <input type="text" id="rf-scan-input" class="form-input" placeholder="Scan or type SKU, then Enter" autocomplete="off" style="font-size:22px; padding:16px;">
      </div>
      <div class="form-group ${pending ? '' : 'hidden'}" style="margin-bottom:0;">
        <label class="form-label" for="rf-qty-input">How many ${escapeHTMLText(pending ? pending.sku : '')}? ${left} still expected</label>
        <input type="number" id="rf-qty-input" class="form-input" min="1" max="${left}" value="${left}" style="font-size:22px; padding:16px;">
        <div style="display:flex; gap:8px; margin-top:8px;">
          <button class="btn btn-primary" id="rf-qty-ok" type="button" style="flex:1; min-height:44px;">Confirm</button>
          <button class="btn btn-outline" id="rf-qty-cancel" type="button" style="min-height:44px;">Cancel</button>
        </div>
      </div>
      <div id="rf-scan-error" class="login-error ${status && status.error ? '' : 'hidden'}" style="margin-top:10px;">${status && status.error ? escapeHTMLText(status.error) : ''}</div>
      <div id="rf-scan-confirm" style="margin-top:10px; font-size:14px;">${status && status.ok ? `<span class="badge badge-success">${escapeHTMLText(status.ok)}</span>` : ''}</div>
      <button class="btn btn-outline btn-sm ${status && status.assist ? '' : 'hidden'}" id="rf-assist-btn" type="button" style="margin-top:8px; min-height:44px;">Get a supervisor</button>
    </div>
    <div class="table-panel" style="padding:0; margin-bottom:16px;">
      <div class="table-wrapper"><table>
        <thead><tr><th>SKU</th><th>Received / Expected</th></tr></thead>
        <tbody>${rows}</tbody>
      </table></div>
    </div>
    <button class="btn btn-primary" id="rf-post-btn" type="button" style="width:100%; padding:14px; font-size:16px;" ${rfReceivingLines.length === 0 || pending ? 'disabled' : ''}>Post Receipt (${rfReceivingLines.length} line(s))</button>
  `;
  const scanInput = document.getElementById('rf-scan-input');
  scanInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); confirmRFScan(); } });
  const qtyInput = document.getElementById('rf-qty-input');
  qtyInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); confirmRFQty(); }
    if (e.key === 'Escape') { e.preventDefault(); rfPendingLine = null; renderRFReceivingBody(); }
  });
  document.getElementById('rf-qty-ok').addEventListener('click', confirmRFQty);
  document.getElementById('rf-qty-cancel').addEventListener('click', () => { rfPendingLine = null; renderRFReceivingBody(); });
  document.getElementById('rf-assist-btn').addEventListener('click', requestRFReceivingAssist);
  if (pending) { qtyInput.focus(); qtyInput.select(); } else scanInput.focus();
  document.getElementById('rf-post-btn').addEventListener('click', postRFReceipt);
}

// confirmRFScan finds the scanned SKU's line; the quantity is the next,
// separate decision (prefilled with what is still expected).
function confirmRFScan() {
  const scanInput = document.getElementById('rf-scan-input');
  const scanned = scanInput.value.trim();
  scanInput.value = '';
  if (!scanned) return;
  const line = rfReceivingExpected.find(l => l.sku === scanned);
  if (!line) {
    renderRFReceivingBody({ error: `${scanned} is not on ASN ${rfReceivingASNId}. Set the carton aside - nothing was received. Ask a supervisor if it should be here.`, assist: true });
    return;
  }
  if (line.receivedQty >= line.expectedQty) {
    renderRFReceivingBody({ error: `${scanned} is already received in full (${line.expectedQty}) - this scan added nothing.` });
    return;
  }
  rfPendingLine = line;
  renderRFReceivingBody();
}

function confirmRFQty() {
  const line = rfPendingLine;
  if (!line) return;
  const left = line.expectedQty - line.receivedQty;
  const qty = parseInt(document.getElementById('rf-qty-input').value, 10);
  if (!qty || qty <= 0) {
    renderRFReceivingBody({ error: 'Enter how many arrived (1 or more).' });
    return;
  }
  if (qty > left) {
    renderRFReceivingBody({ error: `Only ${left} of ${line.sku} are still expected on this ASN - count again, or ask a supervisor about the extra.`, assist: true });
    return;
  }
  line.receivedQty += qty;
  const existingLine = rfReceivingLines.find(l => l.sku === line.sku);
  if (existingLine) {
    existingLine.qty += qty;
    existingLine.accepted_qty += qty;
  } else {
    rfReceivingLines.push({ sku: line.sku, qty, accepted_qty: qty, rejected_qty: 0, damaged_qty: 0 });
  }
  rfPendingLine = null;
  renderRFReceivingBody({ ok: `Confirmed ${qty} x ${line.sku}` });
}

// requestRFReceivingAssist raises the same FloorAssistRequest the RF task
// shell uses; the supervisor answers it on their own device.
async function requestRFReceivingAssist() {
  const reason = await showCustomPrompt('What do you need help with? A supervisor will see this on their own device.', '', 'Get a Supervisor');
  if (!reason || !reason.trim()) return;
  const res = await apiFetch('/api/v1/doc/FloorAssistRequest', { method: 'POST', body: JSON.stringify({
    task: `RF Receiving - ASN ${rfReceivingASNId}`, request: reason.trim(), location: inboundLocation, status: 'Open' }) });
  if (res && res.ok) renderRFReceivingBody({ ok: 'A supervisor has been asked to come to you.' });
  else renderRFReceivingBody({ error: 'The request could not be sent. Ask a colleague to call a supervisor.' });
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
  const partial = rfReceivingExpected.some(l => l.receivedQty < l.expectedQty);
  if (!(await showCustomConfirm(`Post this goods receipt against ${rfReceivingPOId}? ${totalQty} unit(s) will be added to stock at ${location}.${partial ? ' Some expected units have not arrived - the ASN stays open for them.' : ''}`, 'Post Goods Receipt'))) return;

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
  const grn = await res.json().catch(() => ({}));
  inboundLocation = location;
  const asnDone = rfReceivingASNId;
  rfReceivingExpected = [];
  rfReceivingLines = [];
  rfReceivingASNId = '';
  rfReceivingPOId = '';
  rfPendingLine = null;
  document.getElementById('rf-asn-input').value = '';
  const body = document.getElementById('rf-receiving-body');
  body.innerHTML = `
    <div class="table-panel" style="padding:20px;">
      <div><span class="badge badge-success">Received</span> ${totalQty} unit(s) on ${escapeHTMLText(grn.id || grn.code || 'the goods receipt')} against ASN ${escapeHTMLText(asnDone)}.</div>
      <p style="color: var(--text-muted); margin: 8px 0 12px;">The stock is at ${escapeHTMLText(location)} and not in a bin yet.</p>
      <button class="btn btn-primary" id="rf-putaway-now" type="button" style="width:100%; min-height:44px;">Put it away now</button>
      <button class="btn btn-outline" id="rf-next-asn" type="button" style="width:100%; min-height:44px; margin-top:8px;">Receive the next delivery</button>
    </div>`;
  document.getElementById('rf-putaway-now').addEventListener('click', () => { window.location.hash = '#/view/putaway'; });
  document.getElementById('rf-next-asn').addEventListener('click', () => { body.innerHTML = ''; loadRFExpected(); document.getElementById('rf-asn-input').focus(); });
  document.getElementById('rf-putaway-now').focus();
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
    </div>
  `;
  container.appendChild(header);

  const setupPanel = document.createElement('div');
  setupPanel.className = 'table-panel';
  setupPanel.style.padding = '20px';
  setupPanel.style.marginBottom = '16px';
  setupPanel.innerHTML = `
    <div id="load-open-list"><span style="color: var(--text-muted);">Loading open loads...</span></div>
    <hr style="margin:16px 0;">
    <div style="font-weight: 600; margin-bottom: 8px;">Open a new load</div>
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

  document.getElementById('load-create-btn').addEventListener('click', createLoadingDockTask);
  attachLinkTypeahead(document.getElementById('load-dock-door'), 'DockDoor', { valueFields: ['code'] });
  attachLinkTypeahead(document.getElementById('load-trailer'), 'Trailer', { valueFields: ['code'] });
  attachLinkTypeahead(document.getElementById('load-manifest'), 'Manifest', { noSetupHint: true });
  loadingCurrentTaskId = '';
  await loadOpenLoads();
}

// loadOpenLoads (Stage 55.8) lists the loads open at the doors - pick one to
// carry on - instead of asking for a loading task id.
let outboundReady = [];
async function loadOpenLoads() {
  const el = document.getElementById('load-open-list');
  if (!el) return;
  const res = await apiFetch('/api/v1/wms/outbound-work');
  if (!res || !res.ok) { el.innerHTML = ''; return; }
  const work = await res.json();
  outboundReady = work.ready_to_load || [];
  const loads = work.loads || [];
  el.innerHTML = `<div style="font-weight: 600; margin-bottom: 8px;">Open loads (${loads.length}) · ${outboundReady.length} package(s) ready to load</div>` + (loads.length === 0
    ? '<p style="margin: 0; color: var(--text-muted);">No load is open at a door.</p>'
    : `<div style="overflow-x: auto;"><table><thead><tr><th>Load</th><th>Door</th><th>Trailer</th><th>Manifest</th><th class="num">Scanned</th><th>Status</th><th></th></tr></thead><tbody>
      ${loads.map(l => `<tr><td style="font-family: monospace; font-size: 12px;">${escapeHTMLText(l.doc_id)}</td><td>${escapeHTMLText(l.dock_door)}</td><td>${escapeHTMLText(l.trailer_no)}</td><td>${escapeHTMLText(l.manifest_id || '-')}</td>
        <td class="num">${l.scanned_carton_count}${l.expected_carton_count ? ' / ' + l.expected_carton_count : ''}</td><td><span class="badge">${escapeHTMLText(l.status)}</span></td>
        <td><button type="button" class="btn btn-outline btn-sm" data-open-load="${escapeHTMLText(l.doc_id)}">Open</button></td></tr>`).join('')}</tbody></table></div>`);
  el.querySelectorAll('[data-open-load]').forEach(b => b.addEventListener('click', () => { loadingCurrentTaskId = b.dataset.openLoad; loadLoadingDock(); }));
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
  await loadOpenLoads(); // Stage 55.8: keep the open-load counts and still-to-load list current
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
    </div>
    ${(task.status === 'Planned' || task.status === 'Loading') ? (() => {
      // Stage 55.8: what is still to go on this trailer.
      const waiting = outboundReady.filter(p => !task.manifest_id || p.manifest_id === task.manifest_id);
      return `<div class="table-panel" style="padding:16px; margin-top:16px;"><b>Still to load${task.manifest_id ? ` on manifest ${escapeHTMLText(task.manifest_id)}` : ''}: ${waiting.length}</b>
        ${waiting.length ? `<div style="font-size: 13px; margin-top: 6px;">${waiting.map(p => `${escapeHTMLText(p.id)}${p.order_id ? ` (${escapeHTMLText(p.order_id)})` : ''}`).join(' · ')}</div>` : ''}</div>`;
    })() : ''}`;

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
    // Stage 55.8: a load short of what was expected is said out loud first.
    if (task.expected_carton_count && task.scanned_carton_count < task.expected_carton_count &&
        !(await showCustomConfirm(`Only ${task.scanned_carton_count} of ${task.expected_carton_count} expected cartons are scanned. Complete the load with ${task.scanned_carton_count}? The cartons not scanned stay off this trailer.`, 'Load short'))) return;
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
  // Stage 127.3.1: the Bill of Lading is a print like any other - silent
  // once this computer is set up, logged for Reprint.
  if (printBtn) printBtn.addEventListener('click', async () =>
    (await import('./trade-docs.js' + new URL(import.meta.url).search)).printTransactionDocument('LoadingTask', task.doc_id));
}

// Stage 42.2.10 - the warehouse cockpit: one screen combining open tasks by
// type/age, the exception queue, wave status, inbound due today, pick/pack
// throughput and bin utilisation - every section reads GetWarehouseCockpit's
// own best-effort aggregation, so one empty section never blocks the rest
// from rendering. No dock/appointment strip - 42.3 (dock scheduling)
// doesn't exist yet, and the plan's own note says that section is populated
// there, not here.
let cockpitTab = 'cockpit';
async function renderWarehouseCockpitView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Warehouse Cockpit</h1>
    </div>
  `;
  container.appendChild(header);

  // Stage 55.8: labour and billing live beside the cockpit, in their own
  // lazy module.
  const tabs = document.createElement('div');
  tabs.style.cssText = 'display: flex; gap: 8px; margin-bottom: 16px;';
  tabs.innerHTML = [['cockpit', 'Cockpit'], ['labour', 'Labour'], ['billing', 'Billing']].map(([id, label]) =>
    `<button type="button" class="btn ${cockpitTab === id ? 'btn-primary' : 'btn-outline'} btn-sm" data-cockpit-tab="${id}">${label}</button>`).join('');
  container.appendChild(tabs);
  tabs.querySelectorAll('[data-cockpit-tab]').forEach(b => b.addEventListener('click', () => { cockpitTab = b.dataset.cockpitTab; renderView('warehouse-cockpit'); }));
  if (cockpitTab !== 'cockpit') {
    const mod = await import('./view-wms-labour.js' + new URL(import.meta.url).search);
    if (cockpitTab === 'labour') await mod.renderLabourTab(container, inboundLocation);
    else await mod.renderBillingTab(container);
    return;
  }

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
    </div>
  `;
  container.appendChild(header);

  // Stage 85.B6: each condition is one of the ERP's stock locations (Stock ->
  // Stock Locations), named the way the store names it.
  const locRes = await apiFetch('/api/v1/stock-locations').catch(() => null);
  const locs = locRes && locRes.ok ? ((await locRes.json().catch(() => ({}))).locations || []) : [];
  const erpName = (c) => (locs.find(l => l.wms_condition === c) || {}).name;
  const options = BIN_STOCK_CONDITIONS.map(c => `<option value="${c}">${c}${erpName(c) && erpName(c) !== c ? ` - ${escapeHTMLText(erpName(c))}` : ''}</option>`).join('');
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
        <label class="form-label" for="bincond-owner">Owner (3PL client, optional)</label>
        <input type="text" id="bincond-owner" class="form-input" style="width: 160px;" autocomplete="off" placeholder="Whose stock">
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
    <p style="margin: 12px 0 0; color: var(--text-muted); font-size: 13px;">Each condition is a stock location of the store (Stock &rarr; Stock Locations): moving Good stock to Damaged, QC-Hold or RTV moves it to that location at once (on hand stays the same), and it sells only if that location is marked sellable; moving it back to Good returns it to Main. Units reserved for an open pick cannot change condition until that pick is short-picked or rejected. In a bin shared by several 3PL owners, name the owner.</p>
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
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty, from_condition: fromCondition, to_condition: toCondition, owner_id: (document.getElementById('bincond-owner')?.value || '').trim() })
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
//
// Stage 69 (the user: "TO, Stock movement ... different page"): the page is
// the history of moves - the completed Move warehouse tasks every MoveBinStock
// logs - and + New Movement opens the bin-stock picker as a page of its own.
let locMoveRows = [];

async function renderLocationMovementView(container) {
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  const history = document.createElement('div');
  history.className = 'table-panel';
  history.id = 'locmove-history';

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
  container.appendChild(txnPagedSection({ id: 'locmove-screen', title: 'Location Movement', page: 'New movement',
    count: '', newLabel: 'New Movement', form: panel, list: history }));
  txnRestorePane('locmove-screen');
  attachLinkTypeahead(document.getElementById('locmove-location'), 'Location');
  document.getElementById('locmove-load-btn').addEventListener('click', loadLocationMovementStock);
  document.getElementById('locmove-location').addEventListener('change', loadLocationMovementStock);
  await loadLocationMovementHistory();
}

// loadLocationMovementHistory fills the list page: the latest 200 bin-to-bin
// moves, newest first, with the count in the title.
async function loadLocationMovementHistory() {
  const el = document.getElementById('locmove-history');
  if (!el) return;
  const res = await apiFetch('/api/v1/doc/WarehouseTask?task_type=Move&sort=recent&limit=200');
  if (!res) return;
  if (!res.ok) {
    el.innerHTML = `<p style="padding: 16px; color: var(--danger-color);">Failed to load past movements. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['location-movement'])}>Try Again</button></p>`;
    return;
  }
  const moves = await res.json();
  const count = document.querySelector('#locmove-screen .txn-count');
  if (count) count.textContent = String(moves.length);
  el.innerHTML = `
    <table>
      <thead><tr><th>Moved</th><th>Location</th><th>Item</th><th>From bin</th><th>To bin</th><th class="num">Qty</th><th>Batch</th><th>Reason</th></tr></thead>
      <tbody>
        ${moves.length === 0
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No stock moved between bins yet. Click <b>+ New Movement</b>, choose a location, then <b>Move</b> on a bin's row.</td></tr>`
          : moves.map(m => `
            <tr>
              <td>${m.updated_at ? escapeHTMLText(new Date(m.updated_at).toLocaleString()) : ''}</td>
              <td>${escapeHTMLText(m.location_code || '')}</td>
              <td>${escapeHTMLText(m.item || '')}</td>
              <td>${escapeHTMLText(m.from_bin || '')}</td>
              <td>${escapeHTMLText(m.to_bin || '')}</td>
              <td class="num">${m.qty ?? ''}</td>
              <td>${escapeHTMLText(m.batch_no || '') || '&mdash;'}</td>
              <td>${escapeHTMLText(m.notes || '')}</td>
            </tr>`).join('')}
      </tbody>
    </table>`;
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
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="locmove-owner">Owner (3PL client, optional)</label>
        <input type="text" id="locmove-owner" class="form-input" style="width: 160px;" autocomplete="off" placeholder="Whose stock">
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
      body: JSON.stringify({ from_bin_code: r.bin_code, to_bin_code: toBin, sku: r.sku, qty, batch_no: batchNo, reason, owner_id: (document.getElementById('locmove-owner')?.value || '').trim() })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The stock could not be moved.', 'Move Failed'); return; }
    showToast(`Moved ${qty} x ${r.item_name || r.sku} from ${r.bin_code} to ${toBin}.`);
    // Back to the history, the move on top; the New page keeps its location
    // with fresh bin stock for the next move.
    (await loadTxnScreen()).txnDone('locmove-screen');
    await Promise.all([loadLocationMovementStock(), loadLocationMovementHistory()]);
  } finally {
    btn.disabled = false;
  }
}

// Stage 26.5.6: wave/batch pick-list grouping - tag a batch of open
// FulfillmentTasks into a wave, then generate one consolidated,
// zone-then-bin-sorted pick list covering every order in it.
// Stage 55.8: waves are built from a selection of the location's tasks that
// can still be waved, into a new wave or an open one - no typed ids - and the
// open waves show where their tasks are, with the next step for each.
let waveWork = null;
let waveLoadedLoc = null;
async function renderWavePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Wave / Batch Picking</h1>
    </div>
  `;
  container.appendChild(header);

  const locPanel = document.createElement('div');
  locPanel.className = 'table-panel';
  locPanel.style.padding = '16px 24px';
  locPanel.style.marginBottom = '16px';
  locPanel.innerHTML = `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="wave-location">Location</label>
      <input type="text" id="wave-location" class="form-input" style="width: 200px;" autocomplete="off">
    </div>`;
  container.appendChild(locPanel);
  const body = document.createElement('div');
  body.id = 'wave-body';
  container.appendChild(body);
  const loc = document.getElementById('wave-location');
  attachLinkTypeahead(loc, 'Location');
  if (inboundLocation) loc.value = inboundLocation;
  // A typeahead fires change again on blur; reload only for a new location.
  loc.addEventListener('change', () => { const v = loc.value.trim(); if (v === waveLoadedLoc) return; inboundLocation = v; loadWaveWork(); });
  await loadWaveWork();
}

async function loadWaveWork() {
  const body = document.getElementById('wave-body');
  if (!body) return;
  const loc = (document.getElementById('wave-location')?.value || '').trim();
  if (!loc) { body.innerHTML = '<div class="table-panel" style="padding: 24px; color: var(--text-muted);">Choose the location to see its orders ready to pick.</div>'; return; }
  const res = await apiFetch(`/api/v1/wms/outbound-work?location=${encodeURIComponent(loc)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the outbound work.', 'Wave Picking'); return; }
  waveWork = await res.json();
  waveLoadedLoc = loc;
  const keep = new Set([...document.querySelectorAll('.wave-pick:checked')].map(b => b.value));
  const eligible = waveWork.eligible || [], waves = waveWork.waves || [];
  const openWaves = waves.filter(w => !w.status || ['Planned', 'Released', 'In Progress'].includes(w.status));
  const st = (w, s) => (w.by_status || {})[s] || 0;
  const nextStep = w => {
    if (w.status === 'Planned') return `<button class="action-btn" data-wave-act="release" data-wave="${escapeHTMLText(w.wave_id)}">Release</button>`;
    const out = [];
    if (w.remaining > 0 && w.status !== 'Planned') out.push(`<button class="action-btn" data-wave-act="picklist" data-wave="${escapeHTMLText(w.wave_id)}">Pick list</button>`,
      `<button class="action-btn" data-wave-act="mobile" data-wave="${escapeHTMLText(w.wave_id)}">Pick on a phone</button>`);
    if (w.remaining === 0 && w.status && w.status !== 'Complete') out.push(`<button class="action-btn" data-wave-act="complete" data-wave="${escapeHTMLText(w.wave_id)}">Complete</button>`);
    if (st(w, 'Packed') > 0) out.push('<a href="#/view/loading-dock">Load</a>');
    return out.join(' ');
  };
  body.innerHTML = `
    <div class="table-panel" style="padding: 20px; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700; margin: 0 0 8px;">1. Orders ready to wave (${eligible.length})</h2>
      ${eligible.length === 0 ? '<p style="margin: 0; color: var(--text-muted);">Nothing to wave - every Pending or Picking task here is already in a wave. Released orders appear here.</p>' : `
      <div style="overflow-x: auto;"><table>
        <thead><tr><th><input type="checkbox" id="wave-all" aria-label="Select all"></th><th>Task</th><th>Order</th><th>Status</th><th class="num">Lines</th><th class="num">Units</th><th>Owner</th></tr></thead>
        <tbody>${eligible.map(t => `<tr><td><input type="checkbox" class="wave-pick" value="${escapeHTMLText(t.id)}" aria-label="Select ${escapeHTMLText(t.id)}"></td>
          <td style="font-family: monospace; font-size: 12px;">${escapeHTMLText(t.id)}</td><td>${escapeHTMLText(t.customer || t.order_id)}<div style="font-size: 11px; color: var(--text-muted);">${escapeHTMLText(t.order_id)}</div></td>
          <td>${escapeHTMLText(t.status)}</td><td class="num">${t.lines}</td><td class="num">${t.units}</td><td>${escapeHTMLText(t.owner_id || '')}</td></tr>`).join('')}</tbody></table></div>
      <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
        <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="wave-target">Into</label>
          <select id="wave-target" class="form-select"><option value="">A new wave</option>${openWaves.filter(w => w.status).map(w => `<option value="${escapeHTMLText(w.wave_id)}">${escapeHTMLText(w.wave_id)} (${escapeHTMLText(w.status)}, ${w.tasks} task(s))</option>`).join('')}</select></div>
        <button class="btn btn-primary" id="wave-assign-btn" type="button" disabled>Put 0 selected into the wave</button>
      </div>`}
      <div id="wave-assign-result" style="margin-top: 10px; font-size: 13px;"></div>
    </div>
    <div class="table-panel" style="padding: 20px; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700; margin: 0 0 8px;">2. Open waves (${openWaves.length})</h2>
      ${openWaves.length === 0 ? '<p style="margin: 0; color: var(--text-muted);">No open waves at this location.</p>' : `<div style="overflow-x: auto;"><table>
        <thead><tr><th>Wave</th><th>Status</th><th class="num">Tasks</th><th class="num">To pick</th><th class="num">Picking</th><th class="num">Packed</th><th class="num">Dispatched</th><th></th></tr></thead>
        <tbody>${openWaves.map(w => `<tr><td style="font-family: monospace; font-size: 12px;">${escapeHTMLText(w.wave_id)}</td><td>${escapeHTMLText(w.status || 'untracked')}</td>
          <td class="num">${w.tasks}</td><td class="num">${st(w, 'Pending')}</td><td class="num">${st(w, 'Picking')}</td><td class="num">${st(w, 'Packed')}</td><td class="num">${st(w, 'Dispatched')}</td>
          <td>${nextStep(w)}</td></tr>`).join('')}</tbody></table></div>`}
    </div>
    <div id="wave-pick-result"></div>`;
  const boxes = [...body.querySelectorAll('.wave-pick')];
  const btn = document.getElementById('wave-assign-btn');
  const recount = () => { const n = boxes.filter(b => b.checked).length; if (btn) { btn.disabled = n === 0; btn.textContent = `Put ${n} selected into the wave`; } };
  boxes.forEach(b => { if (keep.has(b.value)) b.checked = true; b.addEventListener('change', recount); });
  recount();
  document.getElementById('wave-all')?.addEventListener('change', e => { boxes.forEach(b => { b.checked = e.target.checked; }); recount(); });
  if (btn) btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', submitWaveAssign));
  body.querySelectorAll('[data-wave-act]').forEach(b => b.addEventListener('click', async () => {
    const wave = b.dataset.wave;
    if (b.dataset.waveAct === 'picklist') return submitWavePickList(wave);
    if (b.dataset.waveAct === 'mobile') { mobilePendingWave = wave; window.location.hash = '#/view/mobile-picking'; return; }
    const status = b.dataset.waveAct === 'release' ? 'Released' : 'Complete';
    const r = await apiFetch('/api/v1/wms/wave/transition', { method: 'POST', body: JSON.stringify({ wave_id: wave, status }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, `The wave could not be moved to ${status}.`, 'Wave'); return; }
    showToast(`Wave ${wave}: ${status}.`);
    loadWaveWork();
  }));
}

async function submitWaveAssign() {
  const resultEl = document.getElementById('wave-assign-result');
  const loc = (document.getElementById('wave-location')?.value || '').trim();
  const taskIds = [...document.querySelectorAll('.wave-pick:checked')].map(b => b.value);
  if (!taskIds.length) return;
  const res = await apiFetch('/api/v1/wms/wave/from-selection', { method: 'POST',
    body: JSON.stringify({ location: loc, wave_id: document.getElementById('wave-target')?.value || '', task_ids: taskIds }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The tasks could not go into a wave.', 'Wave Picking'); return; }
  const data = await res.json();
  const refused = Object.entries(data.refused || {});
  showToast(`${(data.tagged || []).length} task(s) in wave ${data.wave_id}.`, { variant: 'success' });
  await loadWaveWork();
  const el = document.getElementById('wave-assign-result');
  if (el && refused.length) el.innerHTML = `<span class="badge badge-warning">Not added</span> ${refused.map(([id, why]) => `${escapeHTMLText(id)} (${escapeHTMLText(why)})`).join(', ')}`;
  else if (resultEl && el) el.textContent = '';
}

async function submitWavePickList(waveId) {
  const resultEl = document.getElementById('wave-pick-result');
  if (!waveId || !resultEl) return;
  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to generate wave pick list.', 'Wave Pick List Failed'); return; }
  const data = await res.json();
  // Stage 42.1.5: the Batch/Expiry column appears only when this wave actually
  // allocated lots (FEFO), so a non-traceability warehouse sees the same six
  // columns it always has.
  const waveHasBatches = (data.pick_lines || []).some(l => l.batch_no);
  let html = `<div class="table-panel" style="padding: 20px;"><h3 style="font-size: 14px; font-weight: 700; margin: 0 0 8px;">Pick list for ${escapeHTMLText(waveId)} (zone/aisle/rack walking order)</h3>`;
  if (waveHasBatches) {
    html += `<p class="text-muted" style="font-size:12px; margin:0 0 8px;">Lines are allocated <b>FEFO</b> (first-expiry-first-out) &mdash; pick the exact batch shown. Expired lots and lots inside the item's minimum remaining shelf life are excluded automatically.</p>`;
  }
  html += `<table><thead><tr><th>Zone</th><th>Aisle</th><th>Rack</th><th>Bin</th><th>SKU</th>${waveHasBatches ? '<th>Batch / Expiry</th>' : ''}<th>Pick Qty</th></tr></thead><tbody>`;
  html += data.pick_lines.length === 0
    ? `<tr><td colspan="${waveHasBatches ? 7 : 6}" style="text-align:center; color:var(--text-muted);">No pick lines &mdash; nothing in this wave is currently stored in a bin.</td></tr>`
    : data.pick_lines.map(l => `<tr><td>${l.zone || ''}</td><td>${l.aisle || ''}</td><td>${l.rack || ''}</td><td>${l.bin_code}</td><td>${l.sku}</td>${waveHasBatches ? `<td>${batchCellHTML(l)}</td>` : ''}<td>${l.pick_qty}</td></tr>`).join('');
  html += `</tbody></table>`;
  html += `<h3 style="font-size: 14px; font-weight: 700; margin: 20px 0 8px;">Per-order allocation</h3>`;
  html += `<table><thead><tr><th>Task ID</th><th>SKU</th><th>Allocated Qty</th><th>Shortfall</th></tr></thead><tbody>`;
  html += data.allocations.length === 0
    ? `<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No allocations &mdash; no stock could be reserved for these orders. Check on-hand quantities under <b>Inventory</b>.</td></tr>`
    : data.allocations.map(a => `<tr><td>${a.task_id}</td><td>${a.sku}</td><td>${a.allocated_qty}</td><td>${a.shortfall ? `<span class="badge badge-warning">${a.shortfall}</span>` : '0'}</td></tr>`).join('');
  html += `</tbody></table></div>`;
  resultEl.innerHTML = html;
  resultEl.scrollIntoView({ block: 'nearest' });
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
// Stage 55.8: the wave is chosen from the open ones, each confirmed line is
// counted with what remains, and the end of the wave marks the fully picked
// orders Packed - a short order is left as it is, and named.
let mobilePickLines = [];
let mobilePickIndex = 0;
let mobilePickRecognition = null;
let mobilePickConfirmed = new Set();
let mobilePickAllocations = [];
let mobilePickWave = '';
let mobilePendingWave = '';

async function renderMobilePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Mobile Picking</h1>
    </div>
  `;
  container.appendChild(header);

  const loadPanel = document.createElement('div');
  loadPanel.className = 'table-panel';
  loadPanel.style.padding = '20px';
  loadPanel.style.marginBottom = '16px';
  loadPanel.style.maxWidth = '420px';
  loadPanel.innerHTML = `
    <div class="form-group" style="margin-bottom: 10px;">
      <label class="form-label" for="picker-location">Location</label>
      <input type="text" id="picker-location" class="form-input" autocomplete="off">
    </div>
    <div class="form-group" style="margin-bottom:0;">
      <label class="form-label" for="mobile-pick-wave-id">Wave</label>
      <select id="mobile-pick-wave-id" class="form-select" style="width: 100%; min-height: 44px;"><option value="">Choose a released wave...</option></select>
    </div>
    <div id="mobile-pick-error" class="login-error hidden" style="margin-top:10px;"></div>
  `;
  container.appendChild(loadPanel);

  const cardWrap = document.createElement('div');
  cardWrap.id = 'mobile-pick-card-wrap';
  cardWrap.style.maxWidth = '420px';
  container.appendChild(cardWrap);

  const loc = document.getElementById('picker-location');
  attachLinkTypeahead(loc, 'Location');
  if (inboundLocation) loc.value = inboundLocation;
  const sel = document.getElementById('mobile-pick-wave-id');
  const fillWaves = async () => {
    inboundLocation = loc.value.trim();
    sel.innerHTML = '<option value="">Choose a released wave...</option>';
    if (!inboundLocation && !mobilePendingWave) return;
    const res = await apiFetch(`/api/v1/wms/outbound-work?location=${encodeURIComponent(inboundLocation)}`);
    if (!res || !res.ok) return;
    const work = await res.json();
    for (const w of (work.waves || []).filter(x => x.remaining > 0 && x.status !== 'Planned')) {
      sel.insertAdjacentHTML('beforeend', `<option value="${escapeHTMLText(w.wave_id)}">${escapeHTMLText(w.wave_id)} - ${w.remaining} order(s) to pick</option>`);
    }
    if (mobilePendingWave) {
      if (![...sel.options].some(o => o.value === mobilePendingWave)) sel.insertAdjacentHTML('beforeend', `<option value="${escapeHTMLText(mobilePendingWave)}">${escapeHTMLText(mobilePendingWave)}</option>`);
      sel.value = mobilePendingWave;
      mobilePendingWave = '';
      await loadMobilePickList();
    }
  };
  loc.addEventListener('change', fillWaves);
  sel.addEventListener('change', loadMobilePickList);
  await fillWaves();
}

async function loadMobilePickList() {
  const errorEl = document.getElementById('mobile-pick-error');
  errorEl.classList.add('hidden');
  const waveId = document.getElementById('mobile-pick-wave-id').value.trim();
  if (!waveId) return;

  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'Failed to load the wave pick list.'); errorEl.classList.remove('hidden'); return; }
  const data = await res.json();
  mobilePickWave = waveId;
  mobilePickLines = data.pick_lines || [];
  mobilePickAllocations = data.allocations || [];
  mobilePickIndex = 0;
  mobilePickConfirmed = new Set();
  renderMobilePickCard();
}

function confirmMobilePickLine() {
  mobilePickConfirmed.add(mobilePickIndex);
  if (mobilePickConfirmed.size >= mobilePickLines.length) { renderMobilePickSummary(); return; }
  for (let i = 1; i <= mobilePickLines.length; i++) {
    const next = (mobilePickIndex + i) % mobilePickLines.length;
    if (!mobilePickConfirmed.has(next)) { mobilePickIndex = next; break; }
  }
  renderMobilePickCard();
}

function renderMobilePickCard() {
  const wrap = document.getElementById('mobile-pick-card-wrap');
  if (!wrap) return;
  if (mobilePickLines.length === 0) {
    // Nothing could be allocated: say which orders are short rather than
    // implying the wave is empty.
    if (mobilePickAllocations.length) { renderMobilePickSummary(); return; }
    wrap.innerHTML = `<div class="table-panel" style="padding:24px; text-align:center; color:var(--text-muted);">No pick lines for this wave - nothing in it is in a bin yet.</div>`;
    return;
  }
  const line = mobilePickLines[mobilePickIndex];
  const done = mobilePickConfirmed.size, total = mobilePickLines.length;
  const speechSupported = 'speechSynthesis' in window;
  const listenSupported = !!(window.SpeechRecognition || window.webkitSpeechRecognition);
  wrap.innerHTML = `
    <div class="table-panel" style="padding:24px; text-align:center;">
      <div class="text-muted" style="font-size:13px; margin-bottom:12px;">Line ${mobilePickIndex + 1} of ${total} · ${done} confirmed · ${total - done} to go</div>
      <div style="font-size:32px; font-weight:700; letter-spacing:-0.5px; margin-bottom:6px;">${escapeHTMLText(line.sku)}</div>
      <div style="font-size:15px; color:var(--text-muted); margin-bottom:16px;">Zone ${escapeHTMLText(line.zone || '-')} / Aisle ${escapeHTMLText(line.aisle || '-')} / Rack ${escapeHTMLText(line.rack || '-')} / Bin ${escapeHTMLText(line.bin_code)}</div>
      ${line.batch_no ? `<div style="font-size:15px; margin-bottom:16px; line-height:1.7;">Batch ${batchCellHTML(line)}</div>` : ''}
      <div style="font-size:48px; font-weight:800; color:var(--primary-color); margin-bottom:20px;">${line.pick_qty}${line.shortfall ? ` <span class="badge badge-warning" style="font-size:14px; vertical-align:middle;">short ${line.shortfall}</span>` : ''}</div>
      ${mobilePickConfirmed.has(mobilePickIndex) ? '<div style="margin-bottom: 12px;"><span class="badge badge-success">Confirmed</span></div>' : ''}
      <div style="display:flex; gap:10px; justify-content:center; margin-bottom:14px;">
        <button class="btn btn-outline" id="mobile-pick-prev" type="button" style="min-height:44px;" ${mobilePickIndex === 0 ? 'disabled' : ''}>Previous</button>
        <button class="btn btn-primary" id="mobile-pick-next" type="button" style="min-height:44px;">Confirm &amp; Next</button>
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
  if (nextBtn) { nextBtn.addEventListener('click', confirmMobilePickLine); nextBtn.focus(); }
  const speakBtn = document.getElementById('mobile-pick-speak');
  if (speakBtn) speakBtn.addEventListener('click', () => speakMobilePickLine(line));
  const listenBtn = document.getElementById('mobile-pick-listen');
  if (listenBtn) listenBtn.addEventListener('click', toggleMobilePickListening);
}

// renderMobilePickSummary: every line confirmed. Orders with nothing short
// can be marked Packed; a short order is left as it is, named, for a
// supervisor to finish or reject on Fulfillment.
function renderMobilePickSummary() {
  const wrap = document.getElementById('mobile-pick-card-wrap');
  if (!wrap) return;
  const byTask = {};
  for (const a of mobilePickAllocations) {
    const t = byTask[a.task_id] || (byTask[a.task_id] = { short: [] });
    if (a.shortfall) t.short.push(`${a.sku} short ${a.shortfall}`);
  }
  const full = Object.keys(byTask).filter(id => byTask[id].short.length === 0);
  const short = Object.keys(byTask).filter(id => byTask[id].short.length > 0);
  wrap.innerHTML = `
    <div class="table-panel" style="padding:24px;">
      <div style="font-size: 18px; font-weight: 700; margin-bottom: 8px;">${mobilePickLines.length ? `Wave ${escapeHTMLText(mobilePickWave)}: all ${mobilePickLines.length} line(s) picked` : `Nothing to pick in wave ${escapeHTMLText(mobilePickWave)} - the bin stock these orders need is missing or already held for other orders`}</div>
      <p style="margin: 0 0 8px;">${full.length} order(s) complete${short.length ? `, ${short.length} short` : ''}.</p>
      ${short.length ? `<ul style="margin: 0 0 12px; padding-left: 18px;">${short.map(id => `<li>${escapeHTMLText(id)}: ${escapeHTMLText(byTask[id].short.join(', '))} - not packed: finish it on Fulfillment when the stock is there, or reject it there to send it to another location</li>`).join('')}</ul>` : ''}
      <button class="btn btn-primary" id="mobile-pick-pack" type="button" style="width: 100%; min-height: 44px;" ${full.length ? '' : 'disabled'}>Mark ${full.length} order(s) packed</button>
      <div id="mobile-pick-pack-result" style="margin-top: 10px; font-size: 13px;"></div>
    </div>`;
  const btn = document.getElementById('mobile-pick-pack');
  btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Packing...', async () => {
    const out = [];
    for (const id of full) {
      const dr = await apiFetch(`/api/v1/doc/FulfillmentTask/${encodeURIComponent(id)}`);
      const doc = dr && dr.ok ? await dr.json() : null;
      const steps = doc && doc.status === 'Pending' ? ['Picking', 'Packed'] : doc && doc.status === 'Picking' ? ['Packed'] : [];
      let ok = steps.length > 0;
      for (const s of steps) {
        const r = await apiFetch('/api/v1/fulfillment/task/transition', { method: 'POST', body: JSON.stringify({ task_id: id, status: s }) });
        if (!r || !r.ok) { ok = false; break; }
      }
      out.push(`${id}: ${ok ? 'packed' : `not changed (${doc ? doc.status : 'not found'})`}`);
    }
    document.getElementById('mobile-pick-pack-result').innerHTML = `${escapeHTMLText(out.join(' · '))}<div style="margin-top: 8px;"><a href="#/view/loading-dock">Next: load the packed orders</a></div>`;
    btn.disabled = true;
  }));
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
      confirmMobilePickLine();
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
  let stockContext = null;
  try { stockContext = JSON.parse(sessionStorage.getItem('erp_stock_count_context') || 'null'); }
  catch (error) { /* Invalid navigation context never blocks ordinary counting. */ }
  sessionStorage.removeItem('erp_stock_count_context');
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Cycle Count</h1>
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
    ${stockContext?.sku && stockContext?.location ? `<div class="notice" style="margin-top:12px;">Count ${escapeHTMLText(stockContext.sku)} at ${escapeHTMLText(stockContext.location)}. The expected quantity stays hidden while you enter your count. <button class="btn btn-primary" id="cyclecount-start-source-btn" type="button">Start blind count for this item</button></div>` : ''}
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

  // Stage 55.6: the session's lines as a work table - reasons picked by name,
  // recount and posting from the row - instead of typing Line IDs. Book
  // quantity and difference appear only once a line is reconciled.
  const linesPanel = document.createElement('div');
  linesPanel.className = 'table-panel';
  linesPanel.style.padding = '24px';
  linesPanel.style.marginTop = '24px';
  linesPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">3. Session lines</h2>
    <p style="color: var(--text-muted); margin: 0 0 12px;">Lines of the session above. A difference needs a root-cause reason before it can post; a doubtful count can go back for a blind recount by someone else.</p>
    <button class="btn btn-outline" id="cyclecount-lines-btn" type="button">Show lines</button>
    <div id="cyclecount-lines" style="margin-top: 12px;"></div>
  `;
  container.appendChild(linesPanel);

  // Stage 26.5.9: ABC cycle-count planner - which SKUs are due for their
  // next count, ranked by velocity tier.
  const abcPanel = document.createElement('div');
  abcPanel.className = 'table-panel';
  abcPanel.style.padding = '24px';
  abcPanel.style.marginTop = '24px';
  abcPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">4. ABC cycle-count planner</h2>
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
  document.getElementById('cyclecount-start-source-btn')?.addEventListener('click', async () => {
    if (!canCreateDoctype('CycleCountLine')) return;
    const session = `CC-${Date.now()}-${crypto.randomUUID().slice(0, 8)}`;
    sessionStorage.setItem('erp_cycle_count_last_session', session);
    currentDoctype = 'CycleCountLine';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderView('doctype-table');
    if (currentView === 'doctype-table' && currentDoctype === 'CycleCountLine') {
      await openDynamicModal(undefined, { count_session: session, location: stockContext.location, sku: stockContext.sku, status: 'Draft' });
    }
  });
  document.getElementById('cyclecount-reconcile-btn').addEventListener('click', submitCycleCountReconcile);
  document.getElementById('cyclecount-lines-btn').addEventListener('click', loadCycleCountLines);
  document.getElementById('abc-plan-btn').addEventListener('click', fetchABCCycleCountPlan);
  attachLinkTypeahead(document.getElementById('abc-location'), 'Location');
  const lastSession = sessionStorage.getItem('erp_cycle_count_last_session');
  if (lastSession) document.getElementById('cyclecount-session').value = lastSession;
  if (stockContext?.location) document.getElementById('abc-location').value = stockContext.location;
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
  // Stage 73.1.8: lines past a configured recount trigger go back for a blind
  // recount (listed, with the new line to count) instead of to an approver.
  const recounts = data.recounts || [];
  resultEl.innerHTML = `
    <span class="badge badge-success">${data.posted_no_variance || 0} posted (no variance)</span>
    &nbsp;
    <span class="badge badge-warning">${data.pending_approval || 0} pending approval</span>
    ${data.recount_requested ? `&nbsp;<span class="badge badge-warning">${data.recount_requested} sent for recount</span>` : ''}
    ${data.awaiting_count ? `&nbsp;<span class="badge badge-secondary">${data.awaiting_count} recount${data.awaiting_count === 1 ? '' : 's'} not counted yet</span>` : ''}
    ${recounts.length ? `<table style="margin-top: 12px;"><thead><tr><th>Item</th><th>Location</th><th>Why</th><th>Recount line</th></tr></thead><tbody>
      ${recounts.map(rc => `<tr><td>${escapeHTMLText(rc.sku)}</td><td>${escapeHTMLText(rc.location)}</td><td>${escapeHTMLText(rc.trigger)}</td><td style="font-family: monospace;">${escapeHTMLText(rc.recount_line_id)}</td></tr>`).join('')}
      </tbody></table><p style="color: var(--text-muted); margin-top: 8px;">Give each recount line to a different counter: under <b>3. Session lines</b> they enter it blind (the first count and the book are not shown), then reconcile this session again.</p>` : ''}
  `;  await loadCycleCountLines(); // Stage 55.6
}

// Stage 55.6: the session work table.
let cycleCountReasons = null;
async function loadCycleCountLines() {
  const el = document.getElementById('cyclecount-lines');
  const session = document.getElementById('cyclecount-session').value.trim();
  if (!el) return;
  if (!session) { el.innerHTML = '<div class="login-error">Enter or reconcile a count session first.</div>'; return; }
  const res = await apiFetch(`/api/v1/wms/cycle-count/session?count_session=${encodeURIComponent(session)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The session lines could not be loaded.', 'Cycle Count'); return; }
  const lines = await res.json();
  if (cycleCountReasons === null) {
    const rr = await apiFetch('/api/v1/doc/ReasonCode?limit=500');
    const docs = rr && rr.ok ? await rr.json() : [];
    cycleCountReasons = (Array.isArray(docs) ? docs : (docs.data || [])).filter(d => d.category === 'Cycle Count Variance' && (d.status || 'Active') === 'Active');
  }
  const n = v => (v === undefined || v === null) ? '' : (Number(v) === Math.round(Number(v)) ? String(Number(v)) : Number(v).toFixed(2));
  const reasonCell = l => {
    if (l.status === 'Posted' || l.status === 'Recount Requested' || l.variance === undefined || Number(l.variance) === 0) return escapeHTMLText(l.variance_reason_code || '');
    if (!cycleCountReasons.length) return `No reasons yet <button class="action-btn" ${actionAttrs('openSetupDoctype', ['ReasonCode'])}>Add one</button> <small>(category Cycle Count Variance)</small>`;
    return `<select class="form-select cc-reason" data-line="${escapeHTMLText(l.id)}" aria-label="Reason for ${escapeHTMLText(l.sku)}"><option value="">Choose...</option>${cycleCountReasons.map(r => `<option value="${escapeHTMLText(r.id)}"${r.id === l.variance_reason_code ? ' selected' : ''}>${escapeHTMLText(r.code || r.id)}${r.description ? ` - ${escapeHTMLText(r.description)}` : ''}</option>`).join('')}</select>`;
  };
  const actions = l => {
    const out = [];
    if (l.recount_of && l.counted_qty === undefined && l.status === 'Draft') out.push(`<input type="number" class="form-input cc-recount-qty" data-line="${escapeHTMLText(l.id)}" style="width: 90px;" aria-label="Recount quantity"> <button class="action-btn" data-cc-act="recount-submit" data-line="${escapeHTMLText(l.id)}">Save recount</button>`);
    if (!l.recount_of && (l.status === 'Pending Approval' || l.status === 'Draft') && l.variance !== undefined && Number(l.variance) !== 0) out.push(`<button class="action-btn" data-cc-act="recount" data-line="${escapeHTMLText(l.id)}">Recount</button>`);
    if (l.status === 'Pending Approval') out.push('<a href="#/view/approvals">Approvals</a>');
    if (l.status === 'Approved') out.push(`<button class="action-btn" data-cc-act="post" data-line="${escapeHTMLText(l.id)}">Post</button>`);
    if (l.recount_line_id) out.push(`<small>recount: ${escapeHTMLText(l.recount_line_id)}</small>`);
    return out.join(' ');
  };
  el.innerHTML = lines.length === 0 ? '<p style="color: var(--text-muted);">No lines in this session yet.</p>' : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Item</th><th>Location / bin</th><th>Status</th><th class="num">Counted</th><th class="num">Book</th><th class="num">Difference</th><th>Reason</th><th></th></tr></thead>
    <tbody>${lines.map(l => `<tr>
      <td>${escapeHTMLText(l.item_name || l.sku)}<div style="font-size: 11px; color: var(--text-muted);">${escapeHTMLText(l.sku)}${l.recount_of ? ' · blind recount' : ''}</div></td>
      <td>${escapeHTMLText(l.location)}${l.bin ? ` / ${escapeHTMLText(l.bin)}` : ''}</td>
      <td><span class="badge ${l.status === 'Posted' ? 'badge-success' : l.status === 'Rejected' ? 'badge-danger' : 'badge-warning'}">${escapeHTMLText(l.status)}</span>${l.recount_trigger ? `<div style="font-size: 11px;">${escapeHTMLText(l.recount_trigger)}</div>` : ''}</td>
      <td class="num">${n(l.counted_qty)}${l.first_count_qty !== undefined ? `<div style="font-size: 11px;">first count ${n(l.first_count_qty)}</div>` : ''}</td>
      <td class="num">${n(l.system_qty)}</td><td class="num">${n(l.variance)}</td>
      <td>${reasonCell(l)}</td><td>${actions(l)}</td></tr>`).join('')}</tbody></table></div>`;
  el.querySelectorAll('.cc-reason').forEach(sel => sel.addEventListener('change', async () => {
    if (!sel.value) return;
    const r = await apiFetch('/api/v1/wms/cycle-count/variance-reason', { method: 'POST', body: JSON.stringify({ line_id: sel.dataset.line, reason_code: sel.value }) });
    if (r && !r.ok) { await showApiError(r, 'Failed to set the reason.', 'Cycle Count'); return; }
    showToast('Reason recorded.');
  }));
  el.querySelectorAll('[data-cc-act]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, '...', async () => {
    const line = btn.dataset.line;
    let r;
    if (btn.dataset.ccAct === 'recount') {
      r = await apiFetch('/api/v1/wms/cycle-count/recount/request', { method: 'POST', body: JSON.stringify({ line_id: line }) });
    } else if (btn.dataset.ccAct === 'recount-submit') {
      const qty = parseFloat(el.querySelector(`.cc-recount-qty[data-line="${CSS.escape(line)}"]`).value);
      if (isNaN(qty)) { await showCustomAlert('Enter the counted quantity.', 'Recount'); return; }
      r = await apiFetch('/api/v1/wms/cycle-count/recount/submit', { method: 'POST', body: JSON.stringify({ line_id: line, counted_qty: qty }) });
    } else {
      r = await apiFetch('/api/v1/wms/cycle-count/post-adjustment', { method: 'POST', body: JSON.stringify({ line_id: line }) });
    }
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'That did not go through.', 'Cycle Count'); return; }
    showToast(btn.dataset.ccAct === 'post' ? 'Posted to stock.' : btn.dataset.ccAct === 'recount' ? 'Sent for a blind recount.' : 'Recount recorded - reconcile the session again.');
    await loadCycleCountLines();
  })));
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

export { renderPutawayView, submitPlannedCrossDockPutaway, checkCrossDockOpportunity, submitCrossDockPutaway, suggestPutawayBin, submitPutaway, renderPlaceHoldView, submitPlaceHold, renderYardBoardView, loadYardBoard, submitYardCheckIn, submitYardCheckInInner, patchYardCheckIn, renderAppointmentCalendarView, shiftCalendarDate, calendarWeekDates, loadAppointmentCalendar, timeToMinutes, apptChip, submitNewAppointment, submitNewAppointmentInner, renderRFReceivingView, loadRFReceivingASN, renderRFReceivingBody, confirmRFScan, postRFReceipt, renderSortationView, loadSortationSlots, sortationAssignSlot, renderLoadingDockView, createLoadingDockTask, createLoadingDockTaskInner, loadLoadingDock, renderWarehouseCockpitView, loadWarehouseCockpit, renderBinConditionsView, submitBinConditionTransition, renderLPNView, submitLPNAssign, lookupLPNContents, renderBinReplenishmentView, fetchBinReplenishmentSuggestions, renderLocationMovementView, loadLocationMovementStock, renderWavePickingView, renderMobilePickingView, loadMobilePickList, renderMobilePickCard, speakMobilePickLine, toggleMobilePickListening, submitWaveAssign, submitWavePickList, renderCycleCountView, submitCycleCountReconcile, loadCycleCountLines, fetchABCCycleCountPlan };
