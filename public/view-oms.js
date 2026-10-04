// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.

// omsConsoleState is the whole screen's state: the active filter, the current
// selection, and the last result. Module-level rather than re-derived from the
// DOM so a re-render after an action keeps the operator where they were -
// losing your filter every time you release a hold is what makes a queue
// screen unusable at 200 orders.
let omsConsoleState = {
  filter: { channel: '', status: '', hold_reason: '', location: '', from_date: '', to_date: '', sla_minutes: 0 },
  limit: 50,
  offset: 0,
  selected: new Set(),
  lastResult: null
};

function omsFilterQuery(extra = {}) {
  const params = new URLSearchParams();
  const merged = { ...omsConsoleState.filter, limit: omsConsoleState.limit, offset: omsConsoleState.offset, ...extra };
  Object.entries(merged).forEach(([key, value]) => {
    if (value !== '' && value !== 0 && value !== null && value !== undefined) params.set(key, value);
  });
  return params.toString();
}

async function renderOMSWorkbenchView(container) {
  container.innerHTML = `
    <div class="page-header">
      <div class="page-title-section">
        <h1 class="page-title">Order Management</h1>
        <p class="page-subtitle">Every channel's orders in one queue &mdash; filter, act in bulk, and open any order end to end.</p>
      </div>
      <div class="page-actions">
        <button class="btn btn-outline" id="oms-refresh">Refresh</button>
      </div>
    </div>
    <!-- dashboard-stats-row / stat-val, not stats-grid / stat-value: the two
         class names the previous version of this screen used do not exist in
         styles.css at all, which is why its four tiles rendered as full-width
         stacked bars with an unstyled number. -->
    <div class="dashboard-stats-row" id="oms-tiles"></div>
    <div class="table-panel" style="padding:24px;margin-bottom:24px;">
      <div class="oms-console-head">
        <div><h2 style="font-size:16px;margin:0;">Channel connectors</h2><p class="page-subtitle" style="margin:4px 0 0;">Sync health, lag and unmapped marketplace SKUs.</p></div>
        <button class="btn btn-outline btn-sm" id="oms-connectors-refresh">Refresh connectors</button>
      </div>
      <div id="oms-connector-health" class="table-wrapper" style="margin-top:16px;"></div>
      <h3 style="font-size:14px;margin:20px 0 8px;">Unmapped SKU exceptions</h3>
      <div id="oms-sku-exceptions" class="table-wrapper"></div>
    </div>
    <div class="table-panel" style="padding:24px;margin-bottom:24px;">
      <div class="oms-console-head">
        <div><h2 style="font-size:16px;margin:0;">Bundles and kits</h2><p class="page-subtitle" style="margin:4px 0 0;">Virtual availability is derived from components; stocked kits can be assembled or disassembled.</p></div>
        <button class="btn btn-outline btn-sm" id="oms-bundles-manage">Manage definitions</button>
      </div>
      <div id="oms-bundle-operations" class="table-wrapper" style="margin-top:16px;"></div>
    </div>
    <div class="table-panel oms-compact-panel" style="padding:16px 24px;margin-bottom:24px;">
      <div class="oms-search-row">
        <input type="search" id="oms-global-search" class="form-input" placeholder="Search any order: order id, channel order id, AWB, phone, customer or SKU">
        <button class="btn btn-outline" id="oms-search-btn">Search</button>
        <button class="btn btn-outline" id="oms-search-clear">Clear</button>
      </div>
      <div id="oms-search-results"></div>
    </div>
    <div class="table-panel" id="oms-manual-panel" style="padding:24px;margin-bottom:24px;"></div>
    <div class="table-panel" style="padding:24px;">
      <div class="oms-console-head">
        <h2 style="font-size:16px;margin:0;">Orders</h2>
        <div class="oms-view-actions">
          <select id="oms-saved-views" class="form-input" style="max-width:220px;"><option value="">Saved views…</option></select>
          <button class="btn btn-outline btn-sm" id="oms-save-view">Save this view</button>
          <button class="btn btn-outline btn-sm" id="oms-delete-view">Delete view</button>
        </div>
      </div>
      <div id="oms-facets" class="oms-facets"></div>
      <div class="bulk-edit-bar hidden" id="oms-bulk-bar">
        <span id="oms-selection-count">0 selected</span>
        <button class="btn btn-outline" id="oms-bulk-release">Release Hold</button>
        <button class="btn btn-outline" id="oms-bulk-hold">Hold</button>
        <button class="btn btn-outline" id="oms-bulk-cancel">Cancel</button>
      </div>
      <div id="oms-order-table"></div>
    </div>`;

  document.getElementById('oms-refresh').addEventListener('click', () => renderView('oms'));
  document.getElementById('oms-search-btn').addEventListener('click', runOMSGlobalSearch);
  document.getElementById('oms-global-search').addEventListener('keydown', e => { if (e.key === 'Enter') runOMSGlobalSearch(); });
  document.getElementById('oms-search-clear').addEventListener('click', () => {
    document.getElementById('oms-global-search').value = '';
    document.getElementById('oms-search-results').innerHTML = '';
  });
  document.getElementById('oms-bulk-release').addEventListener('click', () => runOMSBulkAction('release'));
  document.getElementById('oms-bulk-hold').addEventListener('click', () => runOMSBulkAction('hold'));
  document.getElementById('oms-bulk-cancel').addEventListener('click', () => runOMSBulkAction('cancel'));
  document.getElementById('oms-save-view').addEventListener('click', saveCurrentOMSView);
  document.getElementById('oms-delete-view').addEventListener('click', deleteSelectedOMSView);
  document.getElementById('oms-saved-views').addEventListener('change', applySelectedOMSView);
  document.getElementById('oms-connectors-refresh').addEventListener('click', loadOMSConnectorOperations);
  document.getElementById('oms-bundles-manage').addEventListener('click', () => openOMSDoctype('ProductBundle', ''));

  renderManualOrderPanel(document.getElementById('oms-manual-panel'));
  // The tiles, the saved views and the order list are independent reads, so
  // they go out together rather than in sequence.
  await Promise.all([loadOMSTiles(), loadOMSSavedViews(), loadOMSOrders(), loadOMSConnectorOperations(), loadOMSBundleOperations()]);
}

async function loadOMSBundleOperations() {
  const host = document.getElementById('oms-bundle-operations');
  if (!host) return;
  host.innerHTML = '<div class="text-muted">Loading bundles…</div>';
  const res = await apiFetch('/api/v1/doc/ProductBundle');
  if (!res || !res.ok) { host.innerHTML = '<div class="text-muted">Bundle definitions are unavailable.</div>'; return; }
  const bundles = (await res.json()).filter(bundle => (bundle.status || '') === 'Active');
  if (bundles.length === 0) { host.innerHTML = '<div class="text-muted">No active bundles. Use Manage definitions to create one.</div>'; return; }
  const availability = await Promise.all(bundles.map(async bundle => {
    const sku = bundle.bundle_sku || bundle.code || bundle.id;
    const response = await apiFetch(`/api/v1/oms/bundles/${encodeURIComponent(sku)}/availability`);
    if (!response || !response.ok) return [];
    const data = await response.json();
    return data.locations || [];
  }));
  host.innerHTML = `<table><thead><tr><th>Bundle</th><th>Mode</th><th>Pricing</th><th>Components</th><th>Available to sell</th><th>Warehouse operation</th></tr></thead><tbody>${bundles.map((bundle, index) => {
    const sku = bundle.bundle_sku || bundle.code || bundle.id;
    let components = bundle.components || [];
    if (typeof components === 'string') { try { components = JSON.parse(components); } catch (_) { components = []; } }
    const ats = availability[index].length ? availability[index].map(row => `${escapeHTMLText(row.location_code)}: ${Number(row.ats || 0)}`).join('<br>') : '0';
    const stocked = bundle.fulfillment_mode === 'Stocked';
    return `<tr><td><strong>${escapeHTMLText(bundle.name || sku)}</strong><div class="oms-channel-ref">${escapeHTMLText(sku)}</div></td><td><span class="badge ${stocked ? 'badge-warning' : 'badge-secondary'}">${escapeHTMLText(bundle.fulfillment_mode || '')}</span></td><td>${escapeHTMLText(bundle.pricing_mode || '')}</td><td>${components.map(c => `${escapeHTMLText(c.sku)} × ${Number(c.quantity || 0)}`).join('<br>')}</td><td>${ats}</td><td>${stocked ? `<div style="display:flex;gap:6px;flex-wrap:wrap;"><input class="form-input" data-bundle-location="${index}" placeholder="Location" style="width:110px;"><input class="form-input" data-bundle-qty="${index}" type="number" min="1" value="1" style="width:70px;"><button class="btn btn-outline btn-sm" data-bundle-operation="assemble" data-bundle-index="${index}" data-bundle-sku="${escapeHTMLText(sku)}">Assemble</button><button class="btn btn-outline btn-sm" data-bundle-operation="disassemble" data-bundle-index="${index}" data-bundle-sku="${escapeHTMLText(sku)}">Disassemble</button></div>` : '<span class="text-muted">Explodes at order creation</span>'}</td></tr>`;
  }).join('')}</tbody></table>`;
  host.querySelectorAll('[data-bundle-operation]').forEach(button => button.addEventListener('click', () => runOMSBundleOperation(button)));
}

async function runOMSBundleOperation(button) {
  const index = button.getAttribute('data-bundle-index');
  const operation = button.getAttribute('data-bundle-operation');
  const bundleSKU = button.getAttribute('data-bundle-sku');
  const location = document.querySelector(`[data-bundle-location="${index}"]`)?.value.trim();
  const quantity = Number(document.querySelector(`[data-bundle-qty="${index}"]`)?.value || 0);
  if (!location || !Number.isInteger(quantity) || quantity <= 0) { showToast('Enter a location and positive whole quantity.', { variant: 'warning' }); return; }
  const requestKey = (globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`);
  button.disabled = true;
  const res = await apiFetch(`/api/v1/wms/bundles/${operation}`, { method: 'POST', body: JSON.stringify({ bundle_sku: bundleSKU, location_code: location, quantity, idempotency_key: requestKey }) });
  button.disabled = false;
  if (!res || !res.ok) { if (res) await showApiError(res, `Could not ${operation} this kit.`); return; }
  const result = await res.json();
  showToast(`${operation === 'assemble' ? 'Assembled' : 'Disassembled'} ${quantity} × ${bundleSKU} (${result.operation_id}).`, { variant: 'success' });
  loadOMSBundleOperations();
}

async function loadOMSConnectorOperations() {
  const healthHost = document.getElementById('oms-connector-health');
  const exceptionHost = document.getElementById('oms-sku-exceptions');
  if (!healthHost || !exceptionHost) return;
  healthHost.innerHTML = '<div class="text-muted">Loading connector health…</div>';
  exceptionHost.innerHTML = '<div class="text-muted">Loading exceptions…</div>';
  const [healthRes, exceptionRes] = await Promise.all([
    apiFetch('/api/v1/marketplace/connectors/health'),
    apiFetch('/api/v1/marketplace/sku-exceptions')
  ]);
  if (!healthRes || !healthRes.ok) {
    healthHost.innerHTML = '<div class="text-muted">Connector health is unavailable.</div>';
  } else {
    const { channels = [] } = await healthRes.json();
    healthHost.innerHTML = channels.length === 0 ? '<div class="text-muted">No active channels configured.</div>' : `
      <table><thead><tr><th>Channel</th><th>Platform</th><th>Last sync</th><th>Lag</th><th>24h failures</th><th>Exceptions</th><th>Actions</th></tr></thead><tbody>${channels.map(h => `
        <tr><td>${escapeHTMLText(h.channel)}</td><td>${escapeHTMLText(h.platform)}</td><td><span class="badge ${h.last_status === 'Failed' ? 'badge-danger' : 'badge-success'}">${escapeHTMLText(h.last_status || 'Never')}</span><div class="oms-channel-ref">${escapeHTMLText(h.last_sync_at || '—')}</div></td><td>${formatOMSLag(h.lag_seconds)}</td><td>${Number(h.failures_24h || 0)} / ${Number(h.runs_24h || 0)}</td><td>${Number(h.open_exceptions || 0)}</td><td>${h.can_pull_orders ? `<button class="btn btn-outline btn-sm" data-channel-sync="pull-orders" data-channel="${escapeHTMLText(h.channel)}">Pull orders</button>` : ''} ${h.can_push_ats ? `<button class="btn btn-outline btn-sm" data-channel-sync="sync-inventory" data-channel="${escapeHTMLText(h.channel)}">Push ATS</button>` : ''}${!h.can_pull_orders && !h.can_push_ats ? '<span class="text-muted">Catalogue only</span>' : ''}</td></tr>`).join('')}
      </tbody></table>`;
    healthHost.querySelectorAll('[data-channel-sync]').forEach(button => button.addEventListener('click', () => runOMSChannelSync(button)));
  }
  if (!exceptionRes || !exceptionRes.ok) {
    exceptionHost.innerHTML = '<div class="text-muted">SKU exceptions are unavailable.</div>';
  } else {
    const { exceptions = [] } = await exceptionRes.json();
    exceptionHost.innerHTML = exceptions.length === 0 ? '<div class="text-muted">No open mapping exceptions.</div>' : `
      <table><thead><tr><th>Channel</th><th>Channel SKU</th><th>Latest order</th><th>Count</th><th>Map to ERP item</th><th>External product</th><th>Location</th><th></th></tr></thead><tbody>${exceptions.map((e, index) => `
        <tr><td>${escapeHTMLText(e.channel)}</td><td style="font-family:monospace;">${escapeHTMLText(e.channel_sku)}</td><td>${escapeHTMLText(e.last_order_id || '—')}</td><td>${Number(e.occurrences || 1)}</td><td><input class="form-input" data-map-sku="${index}" placeholder="ERP SKU"></td><td><input class="form-input" data-map-product="${index}" placeholder="Product ID"></td><td><input class="form-input" data-map-location="${index}" placeholder="Location"></td><td><button class="btn btn-primary btn-sm" data-map-exception="${index}" data-channel="${escapeHTMLText(e.channel)}" data-channel-sku="${escapeHTMLText(e.channel_sku)}">Map</button></td></tr>`).join('')}
      </tbody></table>`;
    exceptionHost.querySelectorAll('[data-map-exception]').forEach(button => button.addEventListener('click', () => resolveOMSChannelSKU(button)));
  }
}

function formatOMSLag(seconds) {
  const value = Number(seconds || 0);
  if (!value) return '—';
  if (value < 3600) return `${Math.floor(value / 60)}m`;
  if (value < 86400) return `${Math.floor(value / 3600)}h`;
  return `${Math.floor(value / 86400)}d`;
}

async function runOMSChannelSync(button) {
  const channel = button.getAttribute('data-channel');
  const operation = button.getAttribute('data-channel-sync');
  button.disabled = true;
  const res = await apiFetch(`/api/v1/marketplace/channels/${encodeURIComponent(channel)}/${operation}`, { method: 'POST' });
  button.disabled = false;
  if (!res || !res.ok) { if (res) await showApiError(res, 'Channel sync failed.'); return; }
  const result = await res.json();
  showToast(`${channel}: ${result.processed || 0} processed, ${result.failed || 0} failed.`, { variant: result.failed ? 'warning' : 'success' });
  loadOMSConnectorOperations();
}

async function resolveOMSChannelSKU(button) {
  const index = button.getAttribute('data-map-exception');
  const sku = document.querySelector(`[data-map-sku="${index}"]`)?.value.trim();
  if (!sku) { showToast('Enter the ERP SKU to map.', { variant: 'warning' }); return; }
  const body = {
    sku,
    channel: button.getAttribute('data-channel'),
    channel_sku: button.getAttribute('data-channel-sku'),
    external_product_id: document.querySelector(`[data-map-product="${index}"]`)?.value.trim() || '',
    location_code: document.querySelector(`[data-map-location="${index}"]`)?.value.trim() || ''
  };
  const res = await apiFetch('/api/v1/marketplace/sku-mappings', { method: 'POST', body: JSON.stringify(body) });
  if (!res || !res.ok) { if (res) await showApiError(res, 'Could not save SKU mapping.'); return; }
  showToast(`${body.channel_sku} mapped to ${body.sku}.`, { variant: 'success' });
  loadOMSConnectorOperations();
}

// 35.2.4 - four tiles, each the row count of an already-registered report.
async function loadOMSTiles() {
  const host = document.getElementById('oms-tiles');
  if (!host) return;
  const res = await apiFetch('/api/v1/oms/tiles');
  if (!res || !res.ok) { host.innerHTML = ''; return; }
  const { tiles = [] } = await res.json();
  host.innerHTML = tiles.map(t => `
    <div class="stat-card oms-tile" data-report="${escapeHTMLText(t.report_id)}" title="Open the ${escapeHTMLText(t.label)} report">
      <span class="stat-label">${escapeHTMLText(t.label)}</span>
      <span class="stat-val">${t.error ? '—' : escapeHTMLText(String(t.count))}</span>
      ${t.error ? `<div class="oms-tile-error" title="${escapeHTMLText(t.error)}">unavailable</div>` : ''}
    </div>`).join('');
  // A tile is a shortcut into its own report, not a dead number.
  host.querySelectorAll('.oms-tile').forEach(tile => {
    makeClickable(tile, () => execDashboardOpenReport(tile.getAttribute('data-report')));
  });
}

// 35.2.1 - the faceted, paginated list.
async function loadOMSOrders(liveHost = null) {
  const host = liveHost?.isConnected ? liveHost : document.getElementById('oms-order-table');
  if (!host) return;
  host.innerHTML = '<div class="text-muted" style="padding:16px;">Loading orders…</div>';
  const res = await apiFetch(`/api/v1/oms/orders?${omsFilterQuery()}`);
  if (!res) return;
  if (!res.ok) {
    // BLD-036: used to leave the modal's OK click landing on a blank host
    // with no retry short of navigating away and back.
    renderErrorPanel(host, 'Failed to load orders.', retryHost => loadOMSOrders(retryHost));
    return;
  }
  const result = await res.json();
  omsConsoleState.lastResult = result;
  renderOMSFacets(result.facets || {});
  renderOMSOrderTable(result);
}

function renderOMSFacets(facets) {
  const host = document.getElementById('oms-facets');
  if (!host) return;
  const group = (key, label, values) => {
    const options = (values || []).map(v =>
      `<option value="${escapeHTMLText(v.value)}"${omsConsoleState.filter[key] === v.value ? ' selected' : ''}>${escapeHTMLText(v.value)} (${v.count})</option>`).join('');
    return `<label class="oms-facet"><span>${escapeHTMLText(label)}</span>
      <select class="form-input" data-facet="${key}"><option value="">All</option>${options}</select></label>`;
  };
  host.innerHTML = `
    ${group('channel', 'Channel', facets.channel)}
    ${group('status', 'Status', facets.status)}
    ${group('hold_reason', 'Hold reason', facets.hold_reason)}
    ${group('location', 'Location', facets.location)}
    <label class="oms-facet"><span>From</span><input type="date" class="form-input" data-facet="from_date" value="${escapeHTMLText(omsConsoleState.filter.from_date || '')}"></label>
    <label class="oms-facet"><span>To</span><input type="date" class="form-input" data-facet="to_date" value="${escapeHTMLText(omsConsoleState.filter.to_date || '')}"></label>
    <label class="oms-facet"><span>SLA breach over</span>
      <select class="form-input" data-facet="sla_minutes">
        <option value="0">Any age</option>
        <option value="60"${omsConsoleState.filter.sla_minutes == 60 ? ' selected' : ''}>1 hour</option>
        <option value="240"${omsConsoleState.filter.sla_minutes == 240 ? ' selected' : ''}>4 hours</option>
        <option value="1440"${omsConsoleState.filter.sla_minutes == 1440 ? ' selected' : ''}>24 hours</option>
      </select></label>
    <button class="btn btn-outline btn-sm" id="oms-clear-filters">Clear filters</button>`;

  host.querySelectorAll('[data-facet]').forEach(control => {
    control.addEventListener('change', () => {
      const key = control.getAttribute('data-facet');
      omsConsoleState.filter[key] = key === 'sla_minutes' ? Number(control.value) : control.value;
      // Changing a filter invalidates both the page and the selection - acting
      // in bulk on rows that scrolled out of the filter is exactly the kind of
      // surprise a queue screen must not spring on anyone.
      omsConsoleState.offset = 0;
      omsConsoleState.selected.clear();
      loadOMSOrders();
    });
  });
  document.getElementById('oms-clear-filters').addEventListener('click', () => {
    omsConsoleState.filter = { channel: '', status: '', hold_reason: '', location: '', from_date: '', to_date: '', sla_minutes: 0 };
    omsConsoleState.offset = 0;
    omsConsoleState.selected.clear();
    loadOMSOrders();
  });
}

function omsStatusBadge(status, holdReason) {
  const cls = status === 'On Hold' ? 'badge-warning' : (status === 'Delivered' || status === 'Shipped') ? 'badge-success' : status === 'Cancelled' ? 'badge-danger' : 'badge-secondary';
  return `<span class="badge ${cls}">${escapeHTMLText(status || '—')}</span>` +
    (holdReason ? `<div class="oms-hold-reason">${escapeHTMLText(holdReason)}</div>` : '');
}

function renderOMSOrderTable(result) {
  const host = document.getElementById('oms-order-table');
  const rows = result.rows || [];
  const from = result.total === 0 ? 0 : result.offset + 1;
  const to = result.offset + rows.length;
  host.innerHTML = `
    <div class="table-wrapper">
      <table>
        <thead><tr>
          <th style="width:32px;"><input type="checkbox" id="oms-select-all" aria-label="Select all orders on this page"></th>
          <th>Order</th><th>Source</th><th>Customer</th><th>Status</th><th>Lines</th><th>Location</th><th>Age</th><th class="num">Value</th><th>Actions</th>
        </tr></thead>
        <tbody>
        ${rows.length === 0
          ? `<tr><td colspan="10" style="text-align:center;color:var(--text-muted);padding:24px;">No orders match this filter. Clear the filters above, use <b>New manual order</b>, or let a channel import create one &mdash; all of them land here.</td></tr>`
          : rows.map(o => {
            const channel = o.channel || 'Manual';
            const source = `${escapeHTMLText(channel)}${o.channel_order_id ? `<div class="oms-channel-ref" title="The order id in ${escapeHTMLText(channel)}">${escapeHTMLText(o.channel_order_id)}</div>` : ''}`;
            const age = o.age_minutes < 60 ? `${o.age_minutes}m` : o.age_minutes < 1440 ? `${Math.floor(o.age_minutes / 60)}h` : `${Math.floor(o.age_minutes / 1440)}d`;
            return `<tr>
              <td><input type="checkbox" class="oms-row-select" data-order="${escapeHTMLText(o.order_id)}"${omsConsoleState.selected.has(o.order_id) ? ' checked' : ''}></td>
              <td style="font-family:monospace;">${copyableCell(o.order_id, o.order_id)}${o.priority === 'Expedite' ? '<div class="badge badge-warning oms-expedite">Expedite</div>' : ''}</td>
              <td>${source}</td>
              <td>${escapeHTMLText(o.customer_name || '—')}${o.customer_phone ? `<div class="oms-channel-ref">${escapeHTMLText(o.customer_phone)}</div>` : ''}</td>
              <td>${omsStatusBadge(o.status, o.hold_reason)}</td>
              <td>${escapeHTMLText(String(o.line_count))}</td>
              <td>${escapeHTMLText(o.locations || '—')}</td>
              <td title="${escapeHTMLText(String(o.created_at))}">${escapeHTMLText(age)}</td>
              <td class="num">${formatMoney(o.total_amount)}</td>
              <td><button class="action-btn" data-open-order="${escapeHTMLText(o.order_id)}">Open</button></td>
            </tr>`;
          }).join('')}
        </tbody>
      </table>
    </div>
    <div class="oms-pager">
      <span class="text-muted">${result.total === 0 ? 'No orders' : `Showing ${from}–${to} of ${result.total}`}</span>
      <span>
        <button class="btn btn-outline btn-sm" id="oms-prev"${result.offset === 0 ? ' disabled' : ''}>Previous</button>
        <button class="btn btn-outline btn-sm" id="oms-next"${to >= result.total ? ' disabled' : ''}>Next</button>
      </span>
    </div>`;

  host.querySelectorAll('[data-open-order]').forEach(btn => {
    btn.addEventListener('click', () => openOMSOrderDetail(btn.getAttribute('data-open-order')));
  });
  host.querySelectorAll('.oms-row-select').forEach(box => {
    box.addEventListener('change', () => {
      const id = box.getAttribute('data-order');
      if (box.checked) omsConsoleState.selected.add(id); else omsConsoleState.selected.delete(id);
      updateOMSBulkBar();
    });
  });
  document.getElementById('oms-select-all').addEventListener('change', e => {
    host.querySelectorAll('.oms-row-select').forEach(box => {
      box.checked = e.target.checked;
      const id = box.getAttribute('data-order');
      if (e.target.checked) omsConsoleState.selected.add(id); else omsConsoleState.selected.delete(id);
    });
    updateOMSBulkBar();
  });
  document.getElementById('oms-prev').addEventListener('click', () => {
    omsConsoleState.offset = Math.max(0, omsConsoleState.offset - omsConsoleState.limit);
    loadOMSOrders();
  });
  document.getElementById('oms-next').addEventListener('click', () => {
    omsConsoleState.offset += omsConsoleState.limit;
    loadOMSOrders();
  });
  updateOMSBulkBar();
}

function updateOMSBulkBar() {
  const bar = document.getElementById('oms-bulk-bar');
  const count = omsConsoleState.selected.size;
  if (!bar) return;
  bar.classList.toggle('hidden', count === 0);
  document.getElementById('oms-selection-count').textContent = `${count} selected`;
}

// 35.2.5 - bulk hold/release/cancel. The endpoint reports per-order outcomes,
// so a partially-applicable selection tells the operator exactly which orders
// refused and why instead of a single "some failed".
async function runOMSBulkAction(action) {
  const orderIDs = Array.from(omsConsoleState.selected);
  if (orderIDs.length === 0) return;
  let reasonCode = '';
  if (action === 'hold' || action === 'cancel') {
    const label = action === 'hold' ? 'Active Hold reason-code:' : 'Active Cancellation reason-code:';
    reasonCode = await showCustomPrompt(label, '', `Bulk ${action} ${orderIDs.length} order(s)`);
    if (reasonCode === null || !reasonCode.trim()) return;
    reasonCode = reasonCode.trim();
  }
  const res = await apiFetch('/api/v1/oms/orders/bulk', {
    method: 'POST',
    body: JSON.stringify({ action, order_ids: orderIDs, reason_code: reasonCode })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, `Failed to ${action} the selected orders.`); return; }
  const result = await res.json();
  const failed = Object.entries(result.failed || {});
  if (failed.length === 0) {
    showToast(`${result.succeeded.length} order(s) ${action === 'release' ? 'released' : action + 'ed'}.`);
  } else {
    showToast(`${result.succeeded.length} succeeded, ${failed.length} refused. First: ${failed[0][0]} — ${failed[0][1]}`, { duration: 9000 });
  }
  omsConsoleState.selected.clear();
  await Promise.all([loadOMSOrders(), loadOMSTiles()]);
}

// 35.2.6 - global search.
async function runOMSGlobalSearch() {
  const query = document.getElementById('oms-global-search').value.trim();
  const host = document.getElementById('oms-search-results');
  if (!query) { host.innerHTML = ''; return; }
  const res = await apiFetch(`/api/v1/oms/orders/search?q=${encodeURIComponent(query)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Search failed.'); return; }
  const { results = [] } = await res.json();
  if (results.length === 0) {
    host.innerHTML = `<div class="text-muted" style="padding:12px 0;">Nothing matched “${escapeHTMLText(query)}”. Order id, channel order id, AWB, phone, customer name and SKU are all searchable.</div>`;
    return;
  }
  host.innerHTML = `
    <div class="table-wrapper" style="margin-top:12px;">
      <table><thead><tr><th>Order</th><th>Matched on</th><th>Source</th><th>Customer</th><th>Status</th><th></th></tr></thead>
      <tbody>${results.map(r => `
        <tr>
          <td style="font-family:monospace;">${escapeHTMLText(r.order_id)}</td>
          <td>${escapeHTMLText(r.matched_on)}</td>
          <td>${escapeHTMLText(r.channel)}${r.channel_order_id ? `<div class="oms-channel-ref">${escapeHTMLText(r.channel_order_id)}</div>` : ''}</td>
          <td>${escapeHTMLText(r.customer_name || '—')}</td>
          <td>${omsStatusBadge(r.status, '')}</td>
          <td><button class="action-btn" data-open-search="${escapeHTMLText(r.order_id)}">Open</button></td>
        </tr>`).join('')}</tbody></table>
    </div>`;
  host.querySelectorAll('[data-open-search]').forEach(btn => {
    btn.addEventListener('click', () => openOMSOrderDetail(btn.getAttribute('data-open-search')));
  });
}

// 35.2.1's saved views.
async function loadOMSSavedViews() {
  const select = document.getElementById('oms-saved-views');
  if (!select) return;
  const res = await apiFetch('/api/v1/oms/views');
  if (!res || !res.ok) return;
  const { views = [] } = await res.json();
  select.innerHTML = '<option value="">Saved views…</option>' +
    views.map(v => `<option value="${escapeHTMLText(v.id)}">${escapeHTMLText(v.name)}</option>`).join('');
  select._views = views;
}

async function saveCurrentOMSView() {
  const name = await showCustomPrompt('Name this view:', '', 'Save View');
  if (name === null || !name.trim()) return;
  // The saved filter uses the Go struct's field names, which is what
  // OrderConsoleFilter unmarshals - the query-string names are a separate,
  // lowercase vocabulary and would silently save an empty filter.
  const f = omsConsoleState.filter;
  const res = await apiFetch('/api/v1/oms/views', {
    method: 'POST',
    body: JSON.stringify({
      name: name.trim(),
      filter: {
        Channel: f.channel, Status: f.status, HoldReason: f.hold_reason, Location: f.location,
        FromDate: f.from_date, ToDate: f.to_date, SLAMinutes: Number(f.sla_minutes) || 0
      }
    })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to save this view.'); return; }
  showToast('View saved.');
  loadOMSSavedViews();
}

function applySelectedOMSView() {
  const select = document.getElementById('oms-saved-views');
  const view = (select._views || []).find(v => v.id === select.value);
  if (!view) return;
  const f = view.filter || {};
  omsConsoleState.filter = {
    channel: f.Channel || '', status: f.Status || '', hold_reason: f.HoldReason || '',
    location: f.Location || '', from_date: f.FromDate || '', to_date: f.ToDate || '',
    sla_minutes: f.SLAMinutes || 0
  };
  omsConsoleState.offset = 0;
  omsConsoleState.selected.clear();
  loadOMSOrders();
}

async function deleteSelectedOMSView() {
  const select = document.getElementById('oms-saved-views');
  if (!select.value) { showToast('Pick a saved view to delete first.'); return; }
  const res = await apiFetch(`/api/v1/oms/views/${encodeURIComponent(select.value)}`, { method: 'DELETE' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to delete this view.'); return; }
  showToast('View deleted.');
  loadOMSSavedViews();
}

// 35.2.2 / 35.2.3 - the order detail, with the action bar on it.
//
// One modal built on the existing .modal-overlay/.modal-container primitives
// rather than a third dialog mechanism, and one fetch rather than nine: the
// detail endpoint assembles lines, reservations, tasks, shipments, invoices,
// returns, refunds, notifications and the audit trail server-side.
window.openOMSOrderDetail = async function(orderID) {
  const res = await apiFetch(`/api/v1/oms/orders/${encodeURIComponent(orderID)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load this order.'); return; }
  const d = await res.json();
  const order = d.order || {};
  const status = order.order_status || '';
  const terminal = ['Shipped', 'Delivered', 'Closed', 'Cancelled'].includes(status);

  const section = (title, rows, columns) => `
    <h4 class="oms-detail-heading">${escapeHTMLText(title)} <span class="text-muted">(${rows.length})</span></h4>
    ${rows.length === 0
      ? `<p class="text-muted oms-detail-empty">None.</p>`
      : `<div class="table-wrapper"><table><thead><tr>${columns.map(c => `<th>${escapeHTMLText(c.label)}</th>`).join('')}</tr></thead>
         <tbody>${rows.map(r => `<tr>${columns.map(c => `<td>${escapeHTMLText(String(r[c.key] ?? '—'))}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`}`;

  const lineRows = (d.lines || []).map(l => `
    <tr>
      <td style="font-family:monospace;">${escapeHTMLText(l.line_id)}</td>
      <td>${escapeHTMLText(l.sku)}</td>
      <td class="num">${escapeHTMLText(String(l.qty))}</td>
      <td class="num">${formatMoney(l.unit_price)}</td>
      <td>${escapeHTMLText(l.location_code || '—')}</td>
      <td>${omsStatusBadge(l.line_status, l.hold_reason)}</td>
      <td>${l.line_status === 'On Hold'
            ? `<button class="action-btn" data-line-release="${escapeHTMLText(l.line_id)}">Release line</button>`
            : (['Dispatched', 'Cancelled', 'Returned'].includes(l.line_status) ? '' : `<button class="action-btn" data-line-hold="${escapeHTMLText(l.line_id)}">Hold line</button>`)}
          <label class="oms-split-pick"><input type="checkbox" class="oms-split-line" data-line="${escapeHTMLText(l.line_id)}"> split</label></td>
    </tr>`).join('');

  document.getElementById('oms-order-detail-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-order-detail-modal';
  overlay.innerHTML = `
    <div class="modal-container oms-detail-container">
      <div class="modal-header">
        <h3 class="modal-title">Order ${escapeHTMLText(orderID)}</h3>
        <button type="button" class="modal-close" aria-label="Close">×</button>
      </div>
      <div class="modal-body">
        <div class="oms-detail-summary">
          <div><span class="stat-label">Status</span><div>${omsStatusBadge(status, order.hold_reason)}</div></div>
          <div><span class="stat-label">Source</span><div>${escapeHTMLText(order.channel || 'Manual')}${order.channel_order_id ? `<div class="oms-channel-ref">${escapeHTMLText(order.channel_order_id)}</div>` : ''}</div></div>
          <div><span class="stat-label">Customer</span><div>${escapeHTMLText(order.customer_name || '—')}${order.customer_phone ? `<div class="oms-channel-ref">${escapeHTMLText(order.customer_phone)}</div>` : ''}</div></div>
          <div><span class="stat-label">Payment</span><div>${escapeHTMLText(order.payment_status || '—')}</div></div>
          <div><span class="stat-label">Priority</span><div>${escapeHTMLText(order.priority || 'Normal')}</div></div>
          <div><span class="stat-label">Value</span><div>${formatMoney(order.total_amount)}</div></div>
        </div>
        <div class="oms-detail-address"><span class="stat-label">Ship to</span><div>${escapeHTMLText(order.shipping_address || '—')}</div></div>

        <div class="oms-action-bar">
          ${status === 'On Hold' ? `<button class="btn btn-primary btn-sm" data-action="release">Release hold</button>` : `<button class="btn btn-outline btn-sm" data-action="hold"${terminal ? ' disabled' : ''}>Hold</button>`}
          <button class="btn btn-outline btn-sm" data-action="edit"${terminal ? ' disabled' : ''}>Edit</button>
          <button class="btn btn-outline btn-sm" data-action="reallocate"${terminal ? ' disabled' : ''}>Reallocate</button>
          <button class="btn btn-outline btn-sm" data-action="switch"${terminal ? ' disabled' : ''}>Switch facility</button>
          <button class="btn btn-outline btn-sm" data-action="priority"${terminal ? ' disabled' : ''}>${order.priority === 'Expedite' ? 'Set Normal' : 'Expedite'}</button>
          <button class="btn btn-outline btn-sm" data-action="split"${terminal ? ' disabled' : ''}>Split selected lines</button>
          <button class="btn btn-outline btn-sm" data-action="cancel"${terminal ? ' disabled' : ''}>Cancel order</button>
        </div>
        ${terminal ? `<p class="text-muted oms-detail-empty">This order is ${escapeHTMLText(status)}, so its actions are closed. A tenant can reopen any of them by configuring a StatusTransitionRule.</p>` : ''}

        <h4 class="oms-detail-heading">Lines <span class="text-muted">(${(d.lines || []).length})</span></h4>
        <div class="table-wrapper"><table>
          <thead><tr><th>Line</th><th>SKU</th><th class="num">Qty</th><th class="num">Unit price</th><th>Allocated to</th><th>Status</th><th>Actions</th></tr></thead>
          <tbody>${lineRows || `<tr><td colspan="7" class="text-center text-muted">No lines.</td></tr>`}</tbody>
        </table></div>

        ${section('Reservations', d.reservations || [], [{ key: 'sku', label: 'SKU' }, { key: 'location_code', label: 'Location' }, { key: 'quantity', label: 'Qty' }, { key: 'reservation_type', label: 'Type' }, { key: 'expires_at', label: 'Expires' }])}
        ${section('Fulfillment tasks', d.fulfillment_tasks || [], [{ key: 'id', label: 'Task' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Location' }, { key: 'created_at', label: 'Created' }])}
        ${section('Shipments', d.shipments || [], [{ key: 'id', label: 'Booking' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'AWB' }, { key: 'created_at', label: 'Created' }])}
        ${section('Invoices', d.invoices || [], [{ key: 'id', label: 'Invoice' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Amount' }, { key: 'created_at', label: 'Created' }])}
        ${section('Returns', d.returns || [], [{ key: 'id', label: 'Return' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Type' }, { key: 'created_at', label: 'Created' }])}
        ${section('Refunds', d.refunds || [], [{ key: 'id', label: 'Refund' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Mode' }, { key: 'created_at', label: 'Created' }])}
        ${section('Notifications', d.notifications || [], [{ key: 'id', label: 'Log' }, { key: 'status', label: 'Dispatch' }, { key: 'detail', label: 'Event' }, { key: 'created_at', label: 'At' }])}
        ${section('Audit trail', d.audit_trail || [], [{ key: 'created_at', label: 'At' }, { key: 'user_id', label: 'User' }, { key: 'action', label: 'Action' }, { key: 'status', label: 'Result' }, { key: 'details', label: 'Detail' }])}
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.modal-footer .btn-secondary').addEventListener('click', close);

  const after = async () => {
    close();
    await Promise.all([loadOMSOrders(), loadOMSTiles()]);
  };

  overlay.querySelectorAll('[data-line-hold]').forEach(btn => btn.addEventListener('click', async () => {
    const reasonCode = await showCustomPrompt('Active Hold reason-code:', '', 'Hold Line');
    if (reasonCode === null || !reasonCode.trim()) return;
    await omsPost(`/api/v1/order-lines/${encodeURIComponent(btn.getAttribute('data-line-hold'))}/hold`, { reason_code: reasonCode.trim() }, 'Failed to hold this line.', () => openOMSOrderDetail(orderID));
  }));
  overlay.querySelectorAll('[data-line-release]').forEach(btn => btn.addEventListener('click', async () => {
    await omsPost(`/api/v1/order-lines/${encodeURIComponent(btn.getAttribute('data-line-release'))}/release-hold`, {}, 'Failed to release this line.', () => openOMSOrderDetail(orderID));
  }));

  overlay.querySelectorAll('[data-action]').forEach(btn => btn.addEventListener('click', async () => {
    const action = btn.getAttribute('data-action');
    const path = `/api/v1/orders/${encodeURIComponent(orderID)}`;
    if (action === 'release') return omsPost(`${path}/release-hold`, {}, 'Failed to release the hold.', after);
    if (action === 'hold') {
      const reasonCode = await showCustomPrompt('Active Hold reason-code:', '', 'Hold Order');
      if (reasonCode === null || !reasonCode.trim()) return;
      return omsPost(`${path}/hold`, { reason_code: reasonCode.trim() }, 'Failed to hold this order.', after);
    }
    if (action === 'cancel') {
      const reasonCode = await showCustomPrompt('Active Cancellation reason-code:', '', 'Cancel Order');
      if (reasonCode === null || !reasonCode.trim()) return;
      return omsPost(`${path}/cancel`, { reason_code: reasonCode.trim() }, 'Failed to cancel this order.', after);
    }
    if (action === 'reallocate') {
      // An empty location asks the allocation engine to re-plan rather than
      // forcing a node - that is the difference between Reallocate and Switch.
      return omsPost(`${path}/switch-facility`, { location_code: '' }, 'Failed to reallocate this order.', after);
    }
    if (action === 'switch') {
      const location = await showCustomPrompt('Move unpicked lines to which location code?', '', 'Switch Facility');
      if (location === null || !location.trim()) return;
      return omsPost(`${path}/switch-facility`, { location_code: location.trim() }, 'Failed to switch facility.', after);
    }
    if (action === 'priority') {
      const next = order.priority === 'Expedite' ? 'Normal' : 'Expedite';
      return omsPost(`${path}/priority`, { priority: next }, 'Failed to change priority.', after);
    }
    if (action === 'split') {
      const lineIDs = Array.from(overlay.querySelectorAll('.oms-split-line:checked')).map(b => b.getAttribute('data-line'));
      if (lineIDs.length === 0) { showToast('Tick the lines to split out first.'); return; }
      return omsPost(`${path}/split`, { line_ids: lineIDs }, 'Failed to split this order.', after);
    }
    if (action === 'edit') return openOMSOrderEdit(orderID, order, after);
  }));
};

// omsPost is the one place the console's actions POST from, so the
// error-surfacing and refresh behaviour cannot drift between eight buttons.
async function omsPost(url, body, failureMessage, onSuccess) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, failureMessage); return; }
  showToast('Done.');
  if (onSuccess) await onSuccess();
}

// 35.3.2 - the order edit form.
function openOMSOrderEdit(orderID, order, onSaved) {
  document.getElementById('oms-order-edit-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-order-edit-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Edit order ${escapeHTMLText(orderID)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <form class="modal-body" id="oms-edit-form">
        <div class="form-group"><label class="form-label" for="oms-edit-customer">Customer name</label>
          <input type="text" id="oms-edit-customer" class="form-input" value="${escapeHTMLText(order.customer_name || '')}"></div>
        <div class="form-group"><label class="form-label" for="oms-edit-phone">Customer phone</label>
          <input type="text" id="oms-edit-phone" name="customer_phone" class="form-input" value="${escapeHTMLText(order.customer_phone || '')}"></div>
        <div class="form-group"><label class="form-label" for="oms-edit-ship">Shipping address</label>
          <textarea id="oms-edit-ship" class="form-textarea" rows="2">${escapeHTMLText(order.shipping_address || '')}</textarea></div>
        <div class="form-group"><label class="form-label" for="oms-edit-bill">Billing address</label>
          <textarea id="oms-edit-bill" class="form-textarea" rows="2">${escapeHTMLText(order.billing_address || '')}</textarea></div>
        <div class="form-group"><label class="form-label" for="oms-edit-payment">Payment status</label>
          <select id="oms-edit-payment" class="form-input">
            ${['Pending', 'Confirmed', 'COD'].map(v => `<option value="${v}"${order.payment_status === v ? ' selected' : ''}>${v}</option>`).join('')}
          </select></div>
        <p class="text-muted">Saving re-runs the same address and payment checks a new order goes through. If the edit leaves the order unfulfillable it is placed On Hold with a reason rather than saved silently broken.</p>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-edit-cancel>Cancel</button>
        <button type="button" class="btn btn-primary" data-edit-save>Save changes</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-edit-cancel]').addEventListener('click', close);
  decorateFieldFormats(overlay);
  overlay.querySelector('[data-edit-save]').addEventListener('click', async () => {
    const payload = {
      customer_name: document.getElementById('oms-edit-customer').value,
      customer_phone: document.getElementById('oms-edit-phone').value,
      shipping_address: document.getElementById('oms-edit-ship').value,
      billing_address: document.getElementById('oms-edit-bill').value,
      payment_status: document.getElementById('oms-edit-payment').value
    };
    const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(orderID)}/edit`, { method: 'POST', body: JSON.stringify(payload) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to save this edit.'); return; }
    close();
    showToast('Order updated.');
    if (onSaved) await onSaved();
  });
}

// ---------------------------------------------------------------------------
// Manual order entry (Stage 40.6)
//
// POST /api/v1/orders has existed since the Order Engine was built and is the
// same entry point every channel import ends up at (engines/channel_orders.go's
// ImportChannelSalesOrder maps a payload and then calls CreateSalesOrder, and
// ImportUnicommerceSalesOrder goes through that). What was missing was any way
// to reach it by hand - so a phone order, a walk-in wholesale order or a
// replacement order had no path in the UI at all.
//
// Deliberately posts to that same endpoint rather than creating a SalesOrder
// through the generic doc API: allocation, reservation, hold evaluation and
// idempotency all live behind CreateSalesOrder, and a hand-made document
// would skip every one of them. A manual order is a real order and has to go
// down the same road as a Myntra one.
// ---------------------------------------------------------------------------
let manualOrderLines = [{ sku: '', qty: '', unit_price: '' }];

function renderManualOrderPanel(panel) {
  if (!panel) return;
  panel.innerHTML = `
    <div class="po-composer-head">
      <h2>New manual order</h2>
      <span style="font-size:12px;color:var(--text-muted);">Goes through the same Order Engine as a channel import &mdash; allocation, reservations and holds all apply.</span>
    </div>
    <div class="po-header-grid">
      <div class="form-group">
        <label class="form-label" for="mo-customer">Customer name</label>
        <input type="text" id="mo-customer" class="form-input" placeholder="Who the order is for">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-phone">Customer phone</label>
        <input type="text" id="mo-phone" name="customer_phone" class="form-input">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-channel">Source</label>
        <input type="text" id="mo-channel" class="form-input" value="Manual" title="Recorded on the order so this screen can show where it came from.">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-ref">Reference <span class="po-optional">optional</span></label>
        <input type="text" id="mo-ref" class="form-input" placeholder="Your own order/PO reference" title="Stored as the channel order id. Re-sending the same reference returns the existing order instead of creating a duplicate.">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-payment">Payment</label>
        <select id="mo-payment" class="form-input">
          <option value="Confirmed">Confirmed (paid)</option>
          <option value="Pending">Pending</option>
          <option value="COD">Cash on delivery</option>
        </select>
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="mo-address">Shipping address</label>
      <textarea id="mo-address" class="form-textarea" rows="2" placeholder="Full delivery address including PIN code"></textarea>
    </div>

    <div class="po-lines-head">
      <h3>Items</h3>
      <button class="btn btn-outline btn-sm" id="mo-add-line" type="button">+ Add item</button>
    </div>
    <div class="table-wrapper">
      <table class="po-lines">
        <thead><tr><th style="min-width:200px;">Item</th><th class="num" style="width:90px;">Qty</th><th class="num" style="width:130px;">Unit price</th><th style="width:36px;"></th></tr></thead>
        <tbody id="mo-lines-body"></tbody>
      </table>
    </div>
    <div class="po-footer">
      <div class="po-total-hint" id="mo-hint">The order appears in the table below the moment it is created, with its source and reference shown.</div>
      <div class="po-actions">
        <div id="mo-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="mo-create">Create Order</button>
      </div>
    </div>
  `;
  renderManualOrderLines();
  document.getElementById('mo-add-line').addEventListener('click', () => {
    manualOrderLines.push({ sku: '', qty: '', unit_price: '' });
    renderManualOrderLines();
  });
  document.getElementById('mo-create').addEventListener('click', createManualOrder);
  decorateFieldFormats(panel);
}

function renderManualOrderLines() {
  const body = document.getElementById('mo-lines-body');
  if (!body) return;
  body.innerHTML = manualOrderLines.map((l, i) => `
    <tr data-mo-line="${i}">
      <td><input type="text" class="form-input" data-mo-field="sku" value="${escapeHTMLText(l.sku)}" placeholder="Search item..."></td>
      <td><input type="number" class="form-input num" data-mo-field="qty" min="1" step="1" value="${escapeHTMLText(l.qty)}"></td>
      <td><input type="number" class="form-input num" data-mo-field="unit_price" min="0" step="0.01" value="${escapeHTMLText(l.unit_price)}"></td>
      <td><button type="button" class="po-line-remove" data-mo-remove="${i}" aria-label="Remove line ${i + 1}">&times;</button></td>
    </tr>`).join('');

  body.querySelectorAll('[data-mo-line]').forEach(tr => {
    const i = Number(tr.getAttribute('data-mo-line'));
    attachLinkTypeahead(tr.querySelector('[data-mo-field="sku"]'), 'Item');
    tr.querySelectorAll('[data-mo-field]').forEach(input => {
      const field = input.getAttribute('data-mo-field');
      const commit = () => { manualOrderLines[i][field] = input.value.trim(); };
      input.addEventListener('change', commit);
      input.addEventListener('blur', commit);
    });
  });
  body.querySelectorAll('[data-mo-remove]').forEach(btn => {
    btn.addEventListener('click', () => {
      manualOrderLines.splice(Number(btn.getAttribute('data-mo-remove')), 1);
      if (manualOrderLines.length === 0) manualOrderLines.push({ sku: '', qty: '', unit_price: '' });
      renderManualOrderLines();
    });
  });
}

async function createManualOrder() {
  // BLD-036: guard against a double-click creating two orders.
  await guardAgainstDoubleSubmit(document.getElementById('mo-create'), 'Creating...', createManualOrderInner);
}

async function createManualOrderInner() {
  const errorEl = document.getElementById('mo-error');
  errorEl.classList.add('hidden');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };

  const address = document.getElementById('mo-address').value.trim();
  if (!address) { fail('A shipping address is required - the Order Engine needs somewhere to ship to.'); return; }

  const lines = manualOrderLines
    .filter(l => l.sku || l.qty)
    .map(l => ({ sku: l.sku, qty: Number(l.qty) || 0, unit_price: Number(l.unit_price) || 0 }));
  if (lines.length === 0) { fail('Add at least one item.'); return; }
  const bad = lines.findIndex(l => !l.sku || l.qty <= 0);
  if (bad !== -1) { fail(`Line ${bad + 1} needs an item and a quantity of at least 1.`); return; }

  const res = await apiFetch('/api/v1/orders', {
    method: 'POST',
    body: JSON.stringify({
      channel: document.getElementById('mo-channel').value.trim() || 'Manual',
      channel_order_id: document.getElementById('mo-ref').value.trim(),
      customer_name: document.getElementById('mo-customer').value.trim(),
      customer_phone: document.getElementById('mo-phone').value.trim(),
      shipping_address: address,
      payment_status: document.getElementById('mo-payment').value,
      lines
    })
  });
  if (!res) return;
  if (!res.ok) { fail(await getErrorMessage(res, 'Failed to create the order.')); return; }

  const data = await res.json();
  manualOrderLines = [{ sku: '', qty: '', unit_price: '' }];
  await showCustomAlert(`Order ${data.order_id} created. It is now in the Orders table below with its fulfillment, shipment and invoice state, exactly like a channel order.`, 'Order Created');
  renderView('oms');
}

function openOMSDoctype(doctype, id) { currentDoctype = doctype; currentSearchQuery = id; currentTablePage = 1; renderView('doctype-table'); }
async function releaseOMSOrder(id) { const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/release-hold`, { method: 'POST' }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to release order hold.'); return; } renderView('oms'); }
async function cancelOMSOrder(id) { const reasonCode = await showCustomPrompt('Active Cancellation reason-code:', '', 'Cancel Order'); if (reasonCode === null || !reasonCode.trim()) return; const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: JSON.stringify({ reason_code: reasonCode.trim() }) }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to cancel order.'); return; } renderView('oms'); }

// Marketplace settlement + logistics booking screen (Stage 13.7) - both
// MarketplaceSettlement and LogisticsBooking are already real doctypes
// (listed via the generic GET /api/v1/doc/... endpoint, no new backend code
// needed for reading), and reconciliation/booking already work via
// POST /api/v1/marketplace/settlement/reconcile and .../logistics/book.
async function renderMarketplaceView(container) {
  const [settlementsRes, bookingsRes, manifestsRes] = await Promise.all([
    apiFetch('/api/v1/doc/MarketplaceSettlement'),
    apiFetch('/api/v1/doc/LogisticsBooking'),
    apiFetch('/api/v1/doc/Manifest')
  ]);
  if (!settlementsRes || !bookingsRes || !manifestsRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Marketplace</h1>
      <p class="page-subtitle">Channel settlement reconciliation and logistics bookings.</p>
    </div>
  `;
  container.appendChild(header);

  const settlements = settlementsRes.ok ? await settlementsRes.json() : [];
  const bookings = bookingsRes.ok ? await bookingsRes.json() : [];
  const manifests = manifestsRes.ok ? await manifestsRes.json() : [];

  // --- Settlements panel ---
  const settlementPanel = document.createElement('div');
  settlementPanel.className = 'table-panel';
  settlementPanel.style.padding = '24px';
  settlementPanel.style.marginBottom = '24px';
  settlementPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Settlements</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-settlement-id">Settlement ID</label>
        <input type="text" id="mkt-settlement-id" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-channel">Channel</label>
        <select id="mkt-channel" class="form-input" style="width: 130px;">
          <option value="Shopify">Shopify</option>
          <option value="Amazon">Amazon</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-total-sale">Total Sale</label>
        <input type="number" id="mkt-total-sale" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-commission">Commission</label>
        <input type="number" id="mkt-commission" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-net-payout">Net Payout</label>
        <input type="number" id="mkt-net-payout" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 180px;">
        <label class="form-label" for="mkt-order-ids">Order IDs (comma-separated)</label>
        <input type="text" id="mkt-order-ids" class="form-input">
      </div>
      <button class="btn btn-primary" id="mkt-reconcile-btn">Reconcile</button>
    </div>
    <div id="mkt-settlement-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Settlement ID</th>
          <th>Channel</th>
          <th>Total Sale</th>
          <th>Commission</th>
          <th>Net Payout</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        ${settlements.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No settlements yet. Use <b>Reconcile</b> above once a marketplace payout file is available.</td></tr>`
          : settlements.map(s => `
            <tr>
              <td style="font-family: monospace;">${s.code || s.id}</td>
              <td>${s.channel || ''}</td>
              <td>${(s.total_sale ?? 0).toLocaleString()}</td>
              <td>${(s.commission ?? 0).toLocaleString()}</td>
              <td>${(s.net_payout ?? 0).toLocaleString()}</td>
              <td><span class="badge ${s.status === 'Reconciled' ? 'badge-success' : 'badge-warning'}">${s.status}</span></td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(settlementPanel);

  // --- Settlement Reconciliation panel (Stage 35.8, the "UniReco" gap) ---
  // The panel above stays exactly as it was (a manual one-shot reconcile for
  // a caller who already knows the payout math). This one is the real
  // per-order-line workflow: import a marketplace's settlement file as
  // MarketplaceSettlementLine rows (reuses the generic Bulk Import UI, one
  // click away via "Import / View Settlement Lines" below - no new upload
  // code needed), auto-match them against what was actually invoiced, and
  // work the Variance queue by hand when a line doesn't match within
  // tolerance.
  await renderSettlementReconciliationPanel(container);

  // --- Logistics bookings panel (Stage 26.12.4: serviceability-driven AWB
  // assignment - Carrier/Tracking Number are now optional, auto-resolved by
  // engines.CreateLogisticsBooking off the CourierServiceArea master when a
  // Destination Pincode is given) ---
  const bookingPanel = document.createElement('div');
  bookingPanel.className = 'table-panel';
  bookingPanel.style.padding = '24px';
  bookingPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Logistics Bookings</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-order-id">Order ID</label>
        <input type="text" id="mkt-order-id" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-fulfillment-task-id">Fulfillment Task (optional)</label>
        <input type="text" id="mkt-fulfillment-task-id" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-pincode">Destination Pincode</label>
        <input type="text" id="mkt-pincode" class="form-input" style="width: 120px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-carrier">Carrier (blank = auto)</label>
        <input type="text" id="mkt-carrier" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-tracking">Tracking Number (optional)</label>
        <input type="text" id="mkt-tracking" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-shipping-charge">Shipping Charge</label>
        <input type="number" id="mkt-shipping-charge" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-primary" id="mkt-book-btn">Book</button>
    </div>
    <div id="mkt-booking-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>Booking ID</th>
          <th>Order ID</th>
          <th>Carrier</th>
          <th>AWB Number</th>
          <th>Pincode</th>
          <th>Manifest</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        ${bookings.length === 0
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No logistics bookings yet. Use <b>Book</b> above to book a shipment with a courier.</td></tr>`
          : bookings.map(b => `
            <tr>
              <td style="font-family: monospace;">${b.code || b.id}</td>
              <td>${b.order_id || ''}</td>
              <td>${b.carrier || ''}</td>
              <td style="font-family: monospace;">${b.awb_number || ''}</td>
              <td>${b.destination_pincode || ''}</td>
              <td>${b.manifest_id || ''}</td>
              <td><span class="badge ${b.status === 'RTO' ? 'badge-danger' : b.status === 'Delivered' ? 'badge-success' : 'badge-secondary'}">${b.status}</span></td>
              <td>${renderLogisticsBookingActions(b)}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
    </div>
  `;
  container.appendChild(bookingPanel);

  // --- Manifests panel (Stage 26.12.4) ---
  const manifestPanel = document.createElement('div');
  manifestPanel.className = 'table-panel';
  manifestPanel.style.padding = '24px';
  manifestPanel.style.marginTop = '24px';
  manifestPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Manifests</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-top: -12px; margin-bottom: 16px;">Groups every AWB-assigned shipment for one courier at one location. Handing over a manifest dispatches its fulfillment tasks and, once every task on an order has shipped, flips the order to Shipped.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-manifest-courier">Courier</label>
        <input type="text" id="mkt-manifest-courier" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-manifest-location">Location Code</label>
        <input type="text" id="mkt-manifest-location" class="form-input" style="width: 150px;">
      </div>
      <button class="btn btn-primary" id="mkt-manifest-btn">Generate Manifest</button>
    </div>
    <div id="mkt-manifest-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Manifest ID</th>
          <th>Courier</th>
          <th>Location</th>
          <th>Shipments</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        ${manifests.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No manifests yet. Use <b>Generate Manifest</b> above once shipments have been booked.</td></tr>`
          : manifests.map(m => `
            <tr>
              <td style="font-family: monospace;">${m.code || m.id}</td>
              <td>${m.courier || ''}</td>
              <td>${m.location_code || ''}</td>
              <td>${m.shipment_count ?? 0}</td>
              <td><span class="badge ${m.status === 'Handed Over' ? 'badge-success' : 'badge-warning'}">${m.status}</span></td>
              <td>${m.status === 'Open' ? `<button class="action-btn" ${actionAttrs('handoverManifest', [m.code || m.id])}>Hand Over</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(manifestPanel);

  document.getElementById('mkt-reconcile-btn').addEventListener('click', submitMarketplaceReconcile);
  document.getElementById('mkt-book-btn').addEventListener('click', submitLogisticsBooking);
  document.getElementById('mkt-manifest-btn').addEventListener('click', submitGenerateManifest);
  attachLinkTypeahead(document.getElementById('mkt-manifest-location'), 'Location');
  populateMarketplaceChannelOptions();
}

// Stage 35.8: Settlement Reconciliation panel - reads the
// oms-settlement-variance report (registered report catalog, same
// GET /api/v1/reports/run/{id} every other report uses) for the queue table,
// and drives ReconcileMarketplaceSettlements/RaiseSettlementDispute/
// ResolveSettlementDispute/WriteOffSettlementVariance directly.
async function renderSettlementReconciliationPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:flex-start; flex-wrap:wrap; gap:12px; margin-bottom:16px;">
      <div>
        <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 4px;">Settlement Reconciliation</h2>
        <p style="color: var(--text-muted); font-size: 13px; margin: 0;">Import a marketplace payout file, then auto-match each line against what was actually invoiced. Anything outside tolerance lands in the queue below.</p>
      </div>
      <div style="display:flex; gap:8px; flex-wrap:wrap;">
        <button class="btn btn-outline" id="stl-import-btn">Import / View Settlement Lines</button>
        <button class="btn btn-primary" id="stl-reconcile-btn">Run Auto-Reconcile</button>
      </div>
    </div>
    <div id="stl-recon-summary" style="margin-bottom:16px;"></div>
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>Line</th><th>Channel</th><th>Channel Order</th><th>Order</th><th>Batch</th>
          <th>Gross</th><th>Expected (Invoiced)</th><th>Variance</th><th>Status</th><th>Actions</th>
        </tr>
      </thead>
      <tbody id="stl-variance-body">
        <tr><td colspan="10" style="text-align:center; color:var(--text-muted);">Loading&hellip;</td></tr>
      </tbody>
    </table>
    </div>
  `;
  container.appendChild(panel);

  document.getElementById('stl-import-btn').addEventListener('click', () => openOMSDoctype('MarketplaceSettlementLine'));
  document.getElementById('stl-reconcile-btn').addEventListener('click', runSettlementReconcile);
  await loadSettlementVarianceQueue();
}

async function loadSettlementVarianceQueue() {
  const body = document.getElementById('stl-variance-body');
  if (!body) return;
  const res = await apiFetch('/api/v1/reports/run/oms-settlement-variance');
  if (!res || !res.ok) {
    body.innerHTML = `<tr><td colspan="10" style="text-align:center; color:var(--text-muted);">Could not load the variance queue.</td></tr>`;
    return;
  }
  const rows = await res.json();
  const list = Array.isArray(rows) ? rows : (rows.rows || []);
  if (list.length === 0) {
    body.innerHTML = `<tr><td colspan="10" style="text-align:center; color:var(--text-muted);">No open variance - every reconciled line matched or has been resolved.</td></tr>`;
    return;
  }
  body.innerHTML = list.map(r => `
    <tr>
      <td style="font-family: monospace;">${r.line_id || ''}</td>
      <td>${r.channel || ''}</td>
      <td style="font-family: monospace;">${r.channel_order_id || ''}</td>
      <td style="font-family: monospace;">${r.order_id || ''}</td>
      <td>${r.settlement_batch_id || ''}</td>
      <td>${(r.gross_amount ?? 0).toLocaleString()}</td>
      <td>${(r.expected_amount ?? 0).toLocaleString()}</td>
      <td>${(r.variance_amount ?? 0).toLocaleString()}</td>
      <td><span class="badge ${r.match_status === 'Disputed' ? 'badge-warning' : 'badge-danger'}">${r.match_status || ''}</span></td>
      <td style="white-space:nowrap;">
        ${r.match_status === 'Variance' ? `<button class="action-btn" ${actionAttrs('raiseSettlementDisputeUI', [r.line_id])}>Dispute</button>` : ''}
        ${r.match_status === 'Disputed' ? `<button class="action-btn" ${actionAttrs('resolveSettlementDisputeUI', [r.line_id])}>Resolve</button>` : ''}
        <button class="action-btn" ${actionAttrs('writeOffSettlementVarianceUI', [r.line_id])}>Write Off</button>
      </td>
    </tr>
  `).join('');
}

async function runSettlementReconcile() {
  const summaryEl = document.getElementById('stl-recon-summary');
  const res = await apiFetch('/api/v1/oms/settlements/reconcile', { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    if (summaryEl) summaryEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Reconciliation pass failed.')}</p>`;
    return;
  }
  const result = await res.json();
  if (summaryEl) {
    summaryEl.innerHTML = `<span class="badge badge-success">Scanned ${result.scanned ?? 0}</span> ` +
      `<span class="badge badge-success">Matched ${result.matched ?? 0}</span> ` +
      `<span class="badge badge-warning">Variance ${result.variance ?? 0}</span> ` +
      `<span class="badge badge-danger">Invalid ${result.invalid ?? 0}</span> ` +
      `<span class="badge badge-secondary">Unresolved (no order mapping yet) ${result.unresolved ?? 0}</span>`;
  }
  await loadSettlementVarianceQueue();
}

async function raiseSettlementDisputeUI(lineId) {
  const reasonCode = await showCustomPrompt('Settlement dispute reason code:', '', 'Raise Dispute');
  if (reasonCode === null || !reasonCode.trim()) return;
  const note = await showCustomPrompt('Note (optional):', '', 'Raise Dispute');
  const res = await apiFetch(`/api/v1/oms/settlements/${encodeURIComponent(lineId)}/dispute`, {
    method: 'POST',
    body: JSON.stringify({ reason_code: reasonCode.trim(), note: (note || '').trim() })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to raise the dispute.'); return; }
  await loadSettlementVarianceQueue();
}

async function resolveSettlementDisputeUI(lineId) {
  const corrected = await showCustomPrompt('Corrected gross amount from the marketplace:', '', 'Resolve Dispute');
  if (corrected === null || corrected.trim() === '' || isNaN(parseFloat(corrected))) return;
  const res = await apiFetch(`/api/v1/oms/settlements/${encodeURIComponent(lineId)}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ corrected_gross_amount: parseFloat(corrected) })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to resolve the dispute.'); return; }
  await loadSettlementVarianceQueue();
}

async function writeOffSettlementVarianceUI(lineId) {
  const reasonCode = await showCustomPrompt('Write-off reason code:', '', 'Write Off Variance');
  if (reasonCode === null || !reasonCode.trim()) return;
  const confirmed = await showCustomConfirm('Write off this variance? This posts it to Settlement Variance Written Off and closes the line.', 'Confirm Write-Off');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/oms/settlements/${encodeURIComponent(lineId)}/write-off`, {
    method: 'POST',
    body: JSON.stringify({ reason_code: reasonCode.trim() })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to write off the variance.'); return; }
  await loadSettlementVarianceQueue();
}

// Stage 18.3: the Channel select here was a hardcoded Shopify/Amazon
// <option> list, unlike PIM's channel picker (renderPIMPublishSection)
// which fetches the real Channel master. Extends rather than replaces the
// hardcoded pair - appending any real Channel records not already covered -
// so this can't regress to an empty dropdown if no Channel docs exist yet.
async function populateMarketplaceChannelOptions() {
  const select = document.getElementById('mkt-channel');
  if (!select) return;
  const res = await apiFetch('/api/v1/doc/Channel');
  if (!res || !res.ok) return;
  const channels = await res.json();
  const existing = new Set(Array.from(select.options).map(o => o.value));
  channels.forEach(c => {
    const value = c.code || c.id;
    if (!value || existing.has(value)) return;
    const opt = document.createElement('option');
    opt.value = value;
    opt.textContent = c.name || value;
    select.appendChild(opt);
    existing.add(value);
  });
}

async function submitMarketplaceReconcile() {
  const errorEl = document.getElementById('mkt-settlement-error');
  errorEl.classList.add('hidden');

  const settlementId = document.getElementById('mkt-settlement-id').value.trim();
  const channel = document.getElementById('mkt-channel').value;
  const totalSale = parseFloat(document.getElementById('mkt-total-sale').value);
  const commission = parseFloat(document.getElementById('mkt-commission').value) || 0;
  const netPayout = parseFloat(document.getElementById('mkt-net-payout').value) || 0;
  const orderIds = document.getElementById('mkt-order-ids').value.split(',').map(s => s.trim()).filter(Boolean);

  if (!settlementId || !totalSale || totalSale <= 0) {
    errorEl.textContent = 'Settlement ID and a positive Total Sale are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/settlement/reconcile', {
    method: 'POST',
    body: JSON.stringify({
      settlement_id: settlementId,
      channel,
      total_sale: totalSale,
      commission,
      net_payout: netPayout,
      order_ids: orderIds
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Reconciliation failed.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

// Stage 26.12.4: Carrier and Tracking Number are now optional - a blank
// Carrier auto-selects the top-priority courier serviceable for Destination
// Pincode (engines.CheckCourierServiceability), and a blank Tracking Number
// defaults to the generated AWB number. Only Order ID and Destination
// Pincode are required (a pincode is needed either way, to validate an
// explicit carrier or to auto-select one).
async function submitLogisticsBooking() {
  const errorEl = document.getElementById('mkt-booking-error');
  errorEl.classList.add('hidden');

  const orderId = document.getElementById('mkt-order-id').value.trim();
  const fulfillmentTaskId = document.getElementById('mkt-fulfillment-task-id').value.trim();
  const pincode = document.getElementById('mkt-pincode').value.trim();
  const carrier = document.getElementById('mkt-carrier').value.trim();
  const trackingNumber = document.getElementById('mkt-tracking').value.trim();
  const shippingCharge = parseFloat(document.getElementById('mkt-shipping-charge').value) || 0;

  if (!orderId || !pincode) {
    errorEl.textContent = 'Order ID and Destination Pincode are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/logistics/book', {
    method: 'POST',
    body: JSON.stringify({
      order_id: orderId,
      fulfillment_task_id: fulfillmentTaskId,
      destination_pincode: pincode,
      carrier,
      tracking_number: trackingNumber,
      shipping_charge: shippingCharge
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Booking failed.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

// renderLogisticsBookingActions (Stage 26.12.4) shows the tracking-sync/RTO
// actions valid from a booking's current Shipment-engine status - a label
// is always viewable once AWB-assigned; In-Transit/Delivered/RTO only apply
// once the shipment has actually been handed over to the courier (a
// Manifested-but-not-yet-Handed-Over booking has nothing to track yet).
function renderLogisticsBookingActions(b) {
  const id = b.code || b.id;
  const status = b.status;
  // 31.1.9: "Print Label" is the one-click path (server picks the printer
  // whose Default For is Shipping Label, and a thermal unit gets a real
  // Code 128 AWB rather than digits); "Label" stays as the on-screen read,
  // and is also what Print falls back to when QZ Tray is not running.
  const buttons = [
    `<button class="action-btn" ${actionAttrs('printShippingLabel', [id])}>Print Label</button>`,
    `<button class="action-btn" ${actionAttrs('viewShippingLabel', [id])}>Label</button>`
  ];
  if (status === 'Handed Over') {
    buttons.push(`<button class="action-btn" ${actionAttrs('recordShipmentTracking', [id, 'In-Transit'])}>Mark In-Transit</button>`);
  }
  if (status === 'Handed Over' || status === 'In-Transit') {
    buttons.push(`<button class="action-btn" ${actionAttrs('recordShipmentTracking', [id, 'Delivered'])}>Mark Delivered</button>`);
    buttons.push(`<button class="action-btn action-btn-danger" ${actionAttrs('reportShipmentRTO', [id])}>Report RTO</button>`);
  }
  return buttons.join(' ');
}

async function submitGenerateManifest() {
  const errorEl = document.getElementById('mkt-manifest-error');
  errorEl.classList.add('hidden');

  const courier = document.getElementById('mkt-manifest-courier').value.trim();
  const locationCode = document.getElementById('mkt-manifest-location').value.trim();
  if (!courier || !locationCode) {
    errorEl.textContent = 'Courier and Location Code are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/logistics/manifest', {
    method: 'POST',
    body: JSON.stringify({ courier, location_code: locationCode })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'No AWB-assigned shipments found for that courier/location.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

window.handoverManifest = async function(manifestId) {
  const confirmed = await showCustomConfirm(`Hand over manifest ${manifestId} to the courier? This dispatches every fulfillment task in it and may flip the parent order(s) to Shipped.`, 'Hand Over Manifest');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/marketplace/logistics/manifest/handover', {
    method: 'POST',
    body: JSON.stringify({ manifest_id: manifestId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to hand over manifest.');
    return;
  }
  renderView('marketplace');
};

window.recordShipmentTracking = async function(bookingId, status) {
  const res = await apiFetch('/api/v1/marketplace/logistics/tracking', {
    method: 'POST',
    body: JSON.stringify({ booking_id: bookingId, status })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update tracking status.');
    return;
  }
  renderView('marketplace');
};

window.reportShipmentRTO = async function(bookingId) {
  const reason = await showCustomPrompt(`Reason the courier is returning booking ${bookingId} undelivered:`, '', 'Report RTO');
  if (!reason) return;
  const res = await apiFetch('/api/v1/marketplace/logistics/rto', {
    method: 'POST',
    body: JSON.stringify({ booking_id: bookingId, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record RTO.');
    return;
  }
  renderView('marketplace');
};

// printShippingLabel (Stage 31.1.9) sends a booking's label straight to the
// bench's label printer. The payload is built server-side from the booking
// itself (engines.BuildShippingLabelPayload), so no label data passes
// through the browser and a thermal printer gets ZPL with a scannable AWB.
//
// Falls back to the on-screen label rather than to window.print(): the
// plain-text label is not a printable sheet, and someone whose QZ Tray is
// down still needs to read the AWB off the screen to write the docket.
window.printShippingLabel = async function(bookingId) {
  if (await qzTryPrint('Shipping Label', { documentRef: bookingId, quiet: true })) return;
  await viewShippingLabel(bookingId);
};

// viewShippingLabel (Stage 26.12.4) shows GenerateShippingLabel's plain-text
// label in a lightweight read-only modal, the same .modal-overlay/
// .modal-container primitives viewPickList already uses.
window.viewShippingLabel = async function(bookingId) {
  const res = await apiFetch(`/api/v1/marketplace/logistics/label?booking_id=${encodeURIComponent(bookingId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the shipping label.');
    return;
  }
  const label = await res.text();

  document.getElementById('shipping-label-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'shipping-label-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Shipping Label: ${bookingId}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><pre style="white-space: pre-wrap; font-family: monospace; font-size: 13px;">${label}</pre></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.modal-footer .btn-secondary').addEventListener('click', close);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
};

// Approvals inbox (Stage 13.8) - the checker side of the maker-checker
// engine. Lists every Pending Approval document across all approval-gated
// doctypes (GET /api/v1/approval/pending, already scoped server-side to the
// caller's role/location) with Approve/Reject actions against the already-
// working POST /api/v1/approval/decide.

export { renderOMSWorkbenchView, loadOMSBundleOperations, runOMSBundleOperation, loadOMSConnectorOperations, formatOMSLag, runOMSChannelSync, resolveOMSChannelSKU, loadOMSTiles, loadOMSOrders, renderOMSFacets, omsStatusBadge, renderOMSOrderTable, updateOMSBulkBar, runOMSBulkAction, runOMSGlobalSearch, loadOMSSavedViews, saveCurrentOMSView, applySelectedOMSView, deleteSelectedOMSView, omsPost, openOMSOrderEdit, renderManualOrderPanel, renderManualOrderLines, createManualOrder, createManualOrderInner, openOMSDoctype, releaseOMSOrder, cancelOMSOrder, renderMarketplaceView, renderSettlementReconciliationPanel, loadSettlementVarianceQueue, runSettlementReconcile, raiseSettlementDisputeUI, resolveSettlementDisputeUI, writeOffSettlementVarianceUI, populateMarketplaceChannelOptions, submitMarketplaceReconcile, submitLogisticsBooking, renderLogisticsBookingActions, submitGenerateManifest };
