// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
import { documentSearchLink } from './view-document-links.js';

// Stage 73.2.14: this module's own stylesheet, linked once on first load so
// the cold-core styles.css stays inside its release budget.
if (!document.querySelector('link[data-oms-css]')) {
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = 'oms.css' + new URL(import.meta.url).search;
  link.dataset.omsCss = '1';
  document.head.appendChild(link);
}

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
const omsSyncInFlight = new Set();

// Stage 69: list / New pages (txn-screen.js), loaded with the OMS and
// Marketplace screens.
const loadTxnScreen = () => import('./txn-screen.js' + new URL(import.meta.url).search);
let omsSettlementOrderIDs = [];

function omsFilterQuery(extra = {}) {
  const params = new URLSearchParams();
  const merged = { ...omsConsoleState.filter, limit: omsConsoleState.limit, offset: omsConsoleState.offset, ...extra };
  Object.entries(merged).forEach(([key, value]) => {
    if (value !== '' && value !== 0 && value !== null && value !== undefined) params.set(key, value);
  });
  return params.toString();
}

async function renderOMSWorkbenchView(container) {
  // Stage 88.2: each OMS step is its own page (oms:<step> from the OMS menu);
  // Orders is the console without its configuration, which is OMS settings.
  const tab = typeof currentViewTab === 'string' ? currentViewTab : '';
  if (tab === 'settlements') return renderOMSSettlementsView(container);
  if (tab === 'settings') return renderOMSSettingsView(container);
  if (tab) {
    const steps = await import('./view-oms-steps.js' + new URL(import.meta.url).search);
    if (steps.isOMSStep(tab)) return steps.renderOMSStep(tab, container);
  }
  // Stage 69: the console is the page; + New Order opens the manual order as a
  // page of its own (txn-screen.js, the standard ERP shape).
  const { txnPageTitle, txnNewButton, txnRestorePane } = await loadTxnScreen();
  container.innerHTML = `
   <div id="oms-screen" class="txn-paged txn-on-landing" data-landing="list">
    <div class="page-section-bar">
      ${txnPageTitle({ screen: 'oms-screen', title: 'Orders', page: 'New manual order', landing: 'list' })}
      <span class="page-section-actions txn-landing-only">
        <button class="btn btn-outline" id="oms-refresh">Refresh</button>
        ${txnNewButton({ screen: 'oms-screen', label: 'New Order' })}
      </span>
    </div>
    <div data-pane="list">
    <!-- dashboard-stats-row / stat-val, not stats-grid / stat-value: the two
         class names the previous version of this screen used do not exist in
         styles.css at all, which is why its four tiles rendered as full-width
         stacked bars with an unstyled number. -->
    <div class="dashboard-stats-row" id="oms-tiles"></div>
    <div class="table-panel" style="padding:16px 24px;margin-bottom:24px;">
      <h2 style="font-size:16px;margin:0 0 8px;">Start with the work needing attention</h2>
      <div style="display:flex;gap:8px;flex-wrap:wrap;">
        <button class="btn btn-outline btn-sm" data-oms-queue="hold">Allocation or payment holds</button>
        <button class="btn btn-outline btn-sm" data-oms-go="pending">Pending Orders</button>
        <button class="btn btn-outline btn-sm" data-oms-queue="overdue">Overdue open orders</button>
        <button class="btn btn-outline btn-sm" data-oms-go="settings">Unmapped channel items</button>
        <a class="btn btn-outline btn-sm" href="#/setup/ReturnRequest">Return requests</a>
      </div>
      <div id="oms-exception-queues" class="oms-queue-buttons" aria-label="Exception queues"></div>
      <div id="oms-queue-rows"></div>
    </div>
    <div class="table-panel oms-compact-panel" style="padding:16px 24px;margin-bottom:24px;">
      <div class="oms-search-row">
        <input type="search" id="oms-global-search" class="form-input" aria-label="Search orders" placeholder="Search any order: order id, channel order id, AWB, phone, customer or SKU">
        <button class="btn btn-outline" id="oms-search-btn">Search</button>
        <button class="btn btn-outline" id="oms-search-clear">Clear</button>
      </div>
      <div id="oms-search-results"></div>
    </div>
    <div class="table-panel" style="padding:24px;">
      <div class="oms-console-head">
        <h2 style="font-size:16px;margin:0;">Orders</h2>
        <div class="oms-view-actions">
          <select id="oms-saved-views" class="form-input" style="max-width:220px;" aria-label="Saved views"><option value="">Saved views…</option></select>
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
    </div>
    </div>
    <div data-pane="new" class="hidden">
      <div class="table-panel" id="oms-manual-panel" style="padding:24px;margin-bottom:24px;"></div>
    </div>
   </div>`;
  txnRestorePane('oms-screen');

  document.getElementById('oms-refresh').addEventListener('click', () => renderView('oms'));
  container.querySelectorAll('[data-oms-go]').forEach(button => button.addEventListener('click', () => {
    const page = button.dataset.omsGo;
    setActiveMenu(`menu-oms-${page}`);
    renderView(`oms:${page}`);
  }));
  container.querySelectorAll('[data-oms-queue]').forEach(button => button.addEventListener('click', async () => {
    const queue = button.dataset.omsQueue;
    omsConsoleState.filter = { channel: '', status: queue === 'hold' ? 'On Hold' : '', hold_reason: '', location: '', from_date: '', to_date: '', sla_minutes: queue === 'overdue' ? 60 : 0 };
    omsConsoleState.offset = 0;
    omsConsoleState.selected.clear();
    await loadOMSOrders();
    document.getElementById('oms-order-table')?.scrollIntoView({ block: 'start' });
  }));
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

  renderManualOrderPanel(document.getElementById('oms-manual-panel'));
  // The tiles, the saved views and the order list are independent reads, so
  // they go out together rather than in sequence.
  await Promise.all([loadOMSTiles(), loadOMSExceptionQueues(), loadOMSSavedViews(), loadOMSOrders()]);
}

// Stage 88.2.9: OMS settings on one page, reached from Setup ▸ OMS - channel
// connectors, unmapped channel SKUs, bundles and every OMS list - moved off
// the Orders console so it holds working pages only (D10).
async function renderOMSSettingsView(container) {
  const lists = (state.activeDoctypes || []).filter(d => d.module_key === 'oms' && canReadDoctype(d.name))
    .sort((a, b) => String(a.label || a.name).localeCompare(String(b.label || b.name)));
  container.innerHTML = `
    <div class="page-section-bar"><h1 class="page-title">OMS settings</h1></div>
    <div class="table-panel oms-step-panel">
      <div class="oms-console-head">
        <h2 class="oms-step-heading">Channel connectors</h2>
        <button class="btn btn-outline btn-sm" id="oms-connectors-refresh">Refresh connectors</button>
      </div>
      <div id="oms-connector-health" class="table-wrapper"></div>
      <h3 class="oms-step-heading">Unmapped channel SKUs</h3>
      <div id="oms-sku-exceptions" class="table-wrapper"></div>
    </div>
    <div class="table-panel oms-step-panel">
      <div class="oms-console-head">
        <h2 class="oms-step-heading">Bundles and kits</h2>
        <button class="btn btn-outline btn-sm" id="oms-bundles-manage">Manage definitions</button>
      </div>
      <div id="oms-bundle-operations" class="table-wrapper"></div>
    </div>
    <div class="table-panel oms-step-panel">
      <h2 class="oms-step-heading">OMS lists</h2>
      <div class="oms-queue-buttons">${lists.length ? lists.map(d => `<button class="btn btn-outline btn-sm" data-oms-list="${escapeHTMLText(d.name)}">${escapeHTMLText(d.label || d.name)}</button>`).join('') : '<span class="text-muted">No OMS list is open to your role.</span>'}</div>
    </div>`;
  document.getElementById('oms-connectors-refresh').addEventListener('click', loadOMSConnectorOperations);
  document.getElementById('oms-bundles-manage').addEventListener('click', () => openOMSDoctype('ProductBundle', ''));
  container.querySelectorAll('[data-oms-list]').forEach(b => b.addEventListener('click', () => openOMSDoctype(b.dataset.omsList, '')));
  await Promise.all([loadOMSConnectorOperations(), loadOMSBundleOperations()]);
}

// Stage 73.2.14 - stuck payment, carrier, return/refund and settlement work.
// Each button carries its live count; a row names the order, why it is stuck,
// for how long, and the step the engines will accept next.
function omsWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString();
}

async function loadOMSExceptionQueues() {
  const host = document.getElementById('oms-exception-queues');
  if (!host) return;
  const res = await apiFetch('/api/v1/oms/queues');
  if (!res || !res.ok) { host.innerHTML = '<p class="text-muted">Exception queues are unavailable right now.</p>'; return; }
  const { queues = [] } = await res.json();
  host.innerHTML = queues.map(q => `
    <button class="btn btn-outline btn-sm" data-oms-exception="${escapeHTMLText(q.id)}" title="${escapeHTMLText(q.hint || '')}">
      ${escapeHTMLText(q.label)} <span class="badge">${q.error ? '!' : escapeHTMLText(String(q.count))}</span>
    </button>`).join('');
  host.querySelectorAll('[data-oms-exception]').forEach(btn => btn.addEventListener('click', () => openOMSExceptionQueue(btn.dataset.omsException, btn.textContent.trim())));
  // Stage 73.3.3: My work's Open lands here with its queue already showing.
  let wanted = '';
  try { wanted = sessionStorage.getItem('erp_oms_open_queue') || ''; sessionStorage.removeItem('erp_oms_open_queue'); } catch (_) { /* storage off */ }
  const target = wanted && host.querySelector(`[data-oms-exception="${CSS.escape(wanted)}"]`);
  if (target) {
    await openOMSExceptionQueue(wanted, target.textContent.trim());
    document.getElementById('oms-queue-rows')?.scrollIntoView({ block: 'start' });
  }
}

async function openOMSExceptionQueue(queue, label) {
  const host = document.getElementById('oms-queue-rows');
  if (!host) return;
  host.innerHTML = '<p class="text-muted">Loading…</p>';
  const res = await apiFetch(`/api/v1/oms/queues/${encodeURIComponent(queue)}`);
  if (!res) return;
  if (!res.ok) { host.innerHTML = ''; await showApiError(res, 'Failed to load this queue.'); return; }
  const { rows = [] } = await res.json();
  if (rows.length === 0) {
    host.innerHTML = `<p class="text-muted oms-detail-empty">Nothing is stuck in ${escapeHTMLText(label.replace(/\s*\d+$/, ''))}.</p>`;
    return;
  }
  host.innerHTML = `
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Why it is here</th><th>Since</th><th class="num">Hours</th><th>Next step</th><th>Document</th></tr></thead>
      <tbody>${rows.map(r => `<tr>
        <td>${r.order_id ? `<button class="action-btn" data-open-queue-order="${escapeHTMLText(r.order_id)}">${escapeHTMLText(r.order_id)}</button>` : '—'}</td>
        <td>${escapeHTMLText(r.reason || '')}</td>
        <td>${escapeHTMLText(omsWhen(r.since))}</td>
        <td class="num">${escapeHTMLText(String(r.age_hours ?? ''))}</td>
        <td>${escapeHTMLText(r.next_action || '')}</td>
        <td>${r.ref_id && r.ref_doctype !== 'SalesOrder' ? documentSearchLink(r.ref_doctype, r.ref_id) : ''}${queue === 'channel' ? `<button class="btn btn-outline btn-sm" data-channel-done="${escapeHTMLText(r.order_id)}">Done</button>` : ''}</td>
      </tr>`).join('')}</tbody>
    </table></div>`;
  host.querySelectorAll('[data-open-queue-order]').forEach(btn => btn.addEventListener('click', () => openOMSOrderDetail(btn.dataset.openQueueOrder)));
  // Stage 99.5.2: a web shop change made by hand is marked done here.
  host.querySelectorAll('[data-channel-done]').forEach(btn => btn.addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/oms/orders/${encodeURIComponent(btn.dataset.channelDone)}/channel-change-done`, { method: 'POST', body: '{}' });
    if (!res || !res.ok) { await showApiError(res, 'Could not mark it done.'); return; }
    openOMSExceptionQueue(queue, label);
  }));
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
        <tr><td>${escapeHTMLText(h.channel)}</td><td>${escapeHTMLText(h.platform)}</td><td><span class="badge ${h.last_status === 'Failed' ? 'badge-danger' : h.last_status ? 'badge-success' : 'badge-warning'}">${escapeHTMLText(h.last_status || (h.credentials_set ? 'Never synced' : 'Not connected'))}</span><div class="oms-channel-ref">${escapeHTMLText(h.last_sync_at || (h.credentials_set ? '—' : 'Credentials not set'))}</div>${h.last_status === 'Failed' && h.last_error ? `<div class="oms-channel-ref" style="color:var(--danger-strong);">${escapeHTMLText(h.last_error)}</div>` : ''}</td><td>${formatOMSLag(h.lag_seconds)}</td><td>${Number(h.failures_24h || 0)} / ${Number(h.runs_24h || 0)}</td><td>${Number(h.open_exceptions || 0)}</td><td>${h.can_pull_orders ? `<button class="btn btn-outline btn-sm" data-channel-sync="pull-orders" data-channel="${escapeHTMLText(h.channel)}">Pull orders</button>` : ''} ${h.can_push_ats ? `<button class="btn btn-outline btn-sm" data-channel-sync="sync-inventory" data-channel="${escapeHTMLText(h.channel)}">Push ATS</button>` : ''}${!h.can_pull_orders && !h.can_push_ats ? '<span class="text-muted">Catalogue only</span>' : ''} <button class="btn btn-outline btn-sm" data-channel-setup="${escapeHTMLText(h.channel)}">Check setup</button></td></tr>`).join('')}
      </tbody></table>`;
    healthHost.querySelectorAll('[data-channel-sync]').forEach(button => button.addEventListener('click', () => runOMSChannelSync(button)));
    // Stage 130.19 (55.21): what a channel needs to connect, and where to find it.
    healthHost.querySelectorAll('[data-channel-setup]').forEach(button => button.addEventListener('click', () => showOMSChannelSetup(button, healthHost)));
  }
  if (!exceptionRes || !exceptionRes.ok) {
    exceptionHost.innerHTML = '<div class="text-muted">SKU exceptions are unavailable.</div>';
  } else {
    const { exceptions = [] } = await exceptionRes.json();
    exceptionHost.innerHTML = exceptions.length === 0 ? '<div class="text-muted">No open mapping exceptions.</div>' : `
      <table><thead><tr><th>Channel</th><th>Channel SKU</th><th>Latest order</th><th>Count</th><th>Map to ERP item</th><th>External product</th><th>Location</th><th></th></tr></thead><tbody>${exceptions.map((e, index) => `
        <tr><td>${escapeHTMLText(e.channel)}</td><td style="font-family:monospace;">${escapeHTMLText(e.channel_sku)}</td><td>${escapeHTMLText(e.last_order_id || '—')}</td><td>${Number(e.occurrences || 1)}</td><td><input class="form-input" data-map-sku="${index}" placeholder="ERP SKU" aria-label="ERP SKU for ${escapeHTMLText(e.channel_sku)}"></td><td><input class="form-input" data-map-product="${index}" placeholder="Product ID" aria-label="Product ID for ${escapeHTMLText(e.channel_sku)}"></td><td><input class="form-input" data-map-location="${index}" placeholder="Location" aria-label="Location for ${escapeHTMLText(e.channel_sku)}"></td><td><button class="btn btn-primary btn-sm" data-map-exception="${index}" data-channel="${escapeHTMLText(e.channel)}" data-channel-sku="${escapeHTMLText(e.channel_sku)}">Map</button></td></tr>`).join('')}
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

async function showOMSChannelSetup(button, host) {
  const channel = button.getAttribute('data-channel-setup');
  const res = await apiFetch(`/api/v1/marketplace/channels/${encodeURIComponent(channel)}/setup-check`);
  if (!res || !res.ok) { if (res) await showApiError(res, 'Could not check the setup.'); return; }
  const c = await res.json();
  host.querySelector('[data-channel-setup-panel]')?.remove();
  const panel = document.createElement('div');
  panel.dataset.channelSetupPanel = '1';
  panel.className = 'table-panel';
  panel.style.cssText = 'padding:12px 16px;margin-top:10px;';
  panel.innerHTML = `<strong>${escapeHTMLText(c.channel)} (${escapeHTMLText(c.platform)})</strong>
    <span class="badge ${c.ready ? 'badge-success' : 'badge-warning'}" style="margin-left:6px;">${c.ready ? 'Ready to connect' : 'Not ready'}</span>
    ${c.problem ? `<div style="color:var(--danger-strong);margin-top:6px;">${escapeHTMLText(c.problem)}</div>` : ''}
    ${(c.fields || []).length ? `<table style="margin-top:8px;"><thead><tr><th>Needs</th><th>Saved</th><th>Where to find it</th></tr></thead><tbody>${c.fields.map(f => `<tr><td>${escapeHTMLText(f.label)}</td><td>${f.set ? '✓' : '—'}</td><td>${escapeHTMLText(f.where)}</td></tr>`).join('')}</tbody></table>` : ''}
    ${(c.steps || []).length ? `<ol style="margin:8px 0 0 18px;">${c.steps.map(s => `<li>${escapeHTMLText(s)}</li>`).join('')}</ol>` : ''}
    ${c.last_status ? `<div class="text-muted" style="margin-top:6px;">Last sync: ${escapeHTMLText(c.last_status)} ${escapeHTMLText(c.last_sync_at || '')}${c.last_error ? ' - ' + escapeHTMLText(c.last_error) : ''}</div>` : ''}`;
  host.appendChild(panel);
}

async function runOMSChannelSync(button) {
  const channel = button.getAttribute('data-channel');
  const operation = button.getAttribute('data-channel-sync');
  const key = `${channel}:${operation}`;
  if (omsSyncInFlight.has(key)) return;
  omsSyncInFlight.add(key);
  button.disabled = true;
  button.setAttribute('aria-busy', 'true');
  try {
    const res = await apiFetch(`/api/v1/marketplace/channels/${encodeURIComponent(channel)}/${operation}`, { method: 'POST' });
    if (!res || !res.ok) { if (res) await showApiError(res, 'Channel sync failed.'); return; }
    const result = await res.json();
    showToast(`${channel}: ${result.processed || 0} processed (including existing order IDs), ${result.failed || 0} failed. Review exceptions before retrying.`, { variant: result.failed ? 'warning' : 'success' });
    await loadOMSConnectorOperations();
  } finally {
    omsSyncInFlight.delete(key);
    button.disabled = false;
    button.removeAttribute('aria-busy');
  }
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
              <td><input type="checkbox" class="oms-row-select" aria-label="Select order ${escapeHTMLText(o.order_id)}" data-order="${escapeHTMLText(o.order_id)}"${omsConsoleState.selected.has(o.order_id) ? ' checked' : ''}></td>
              <td style="font-family:monospace;">${copyableCell(o.order_number || o.order_id, o.order_number || o.order_id)}${o.priority === 'Expedite' ? '<div class="badge badge-warning oms-expedite">Expedite</div>' : ''}</td>
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
          <td style="font-family:monospace;">${escapeHTMLText(r.order_number || r.order_id)}</td>
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
  const hasLiveBooking = (d.shipments || []).some(shipment => !['Cancelled', 'RTO'].includes(shipment.status));
  const taskForBooking = !terminal && !hasLiveBooking
    ? (d.fulfillment_tasks || []).find(task => ['Packed', 'Dispatched'].includes(task.status))
    : null;
  // Stage 73.2.14: the server's next allowed step and one dated timeline.
  const nextActions = d.next_actions || [];
  const timeline = d.timeline || [];

  const section = (title, rows, columns, doctype = '') => `
    <h4 class="oms-detail-heading">${escapeHTMLText(title)} <span class="text-muted">(${rows.length})</span></h4>
    ${rows.length === 0
      ? `<p class="text-muted oms-detail-empty">None.</p>`
      : `<div class="table-wrapper"><table><thead><tr>${columns.map(c => `<th>${escapeHTMLText(c.label)}</th>`).join('')}</tr></thead>
         <tbody>${rows.map(r => `<tr>${columns.map(c => `<td>${doctype && c.key === 'id' ? documentSearchLink(doctype, r.id) : (c.key === 'created_at' || c.key === 'expires_at' ? escapeHTMLText(omsWhen(r[c.key])) : escapeHTMLText(String(r[c.key] ?? '—')))}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`}`;

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
        <h3 class="modal-title">Order ${escapeHTMLText(order.code || orderID)}</h3>
        <button type="button" class="modal-close" aria-label="Close">×</button>
      </div>
      <div class="modal-body">
        <div class="oms-detail-summary">
          <div><span class="stat-label">Status</span><div>${omsStatusBadge(status, order.hold_reason)}</div></div>
          <div><span class="stat-label">Source</span><div>${escapeHTMLText(order.channel || 'Manual')}${order.channel_order_id ? `<div class="oms-channel-ref">${escapeHTMLText(order.channel_order_id)}</div>` : ''}</div></div>
          <div><span class="stat-label">Customer</span><div>${escapeHTMLText(order.customer_name || '—')}${order.customer_phone ? `<div class="oms-channel-ref">${escapeHTMLText(order.customer_phone)}</div>` : ''}</div></div>
          ${order.customer_reference ? `<div><span class="stat-label">Customer PO / Ref</span><div>${escapeHTMLText(order.customer_reference)}</div></div>` : ''}
          <div><span class="stat-label">Payment</span><div>${escapeHTMLText(order.payment_status || '—')}</div></div>
          <div><span class="stat-label">Priority</span><div>${escapeHTMLText(order.priority || 'Normal')}</div></div>
          ${order.channel_push && order.channel_push.decision ? `<div><span class="stat-label">Channel told</span><div title="${escapeHTMLText(order.channel_push.error || '')}">${escapeHTMLText(order.channel_push.decision)} · ${escapeHTMLText(order.channel_push.status || '')}</div></div>` : ''}
          <div><span class="stat-label">Value</span><div>${formatMoney(order.total_amount)}</div></div>
          ${order.original_order_id ? `<div><span class="stat-label">${escapeHTMLText(order.replacement_kind || 'Replacement')} of</span><div><button class="action-btn" data-open-original="${escapeHTMLText(order.original_order_id)}">${escapeHTMLText(order.original_order_id)}</button></div></div>` : ''}
        </div>
        ${nextActions.length ? `<div class="oms-next-step" role="status"><strong>Next step:</strong> ${escapeHTMLText(nextActions[0].label)} <span class="text-muted">- ${escapeHTMLText(nextActions[0].reason)}</span> <button class="btn btn-primary btn-sm" data-next-step="${escapeHTMLText(nextActions[0].action)}">${escapeHTMLText(nextActions[0].label)}</button>${nextActions.slice(1).map(a => `<div class="text-muted">Also: ${escapeHTMLText(a.label)} - ${escapeHTMLText(a.reason)}</div>`).join('')}</div>` : ''}
        <div class="oms-detail-address"><span class="stat-label">Ship to</span><div>${escapeHTMLText(omsShipTo(order) || '—')}</div></div>

        <div class="oms-action-bar">
          ${status === 'Reserved' ? `<button class="btn btn-primary btn-sm" data-action="fulfil" title="Create the pick tasks for this order at its sourcing location(s)">Accept - send to Pick</button>` : ''}
          ${taskForBooking ? `<button class="btn btn-primary btn-sm" data-action="book" title="Create a carrier booking and AWB for this order">Book shipment</button>` : ''}
          ${status === 'On Hold' ? `<button class="btn btn-primary btn-sm" data-action="release">Release hold</button>` : `<button class="btn btn-outline btn-sm" data-action="hold"${terminal ? ' disabled' : ''}>Hold</button>`}
          <button class="btn btn-outline btn-sm" data-action="edit"${terminal ? ' disabled' : ''}>Edit</button>
          ${['Draft', 'On Hold', 'Reserved', 'Released'].includes(status) ? `<button class="btn btn-outline btn-sm" data-action="lines" title="Change quantities, remove or add lines until the warehouse starts picking">Edit lines</button>` : ''}
          ${['Shipped', 'Delivered', 'Closed', 'Dispatched', 'Partially Fulfilled'].includes(status) ? `<button class="btn btn-outline btn-sm" data-action="replace" title="Send lost, damaged or wrong goods again on a linked, no-charge order">Reship / replace</button>` : ''}
          ${['Shipped', 'Delivered', 'Closed', 'Dispatched', 'Partially Fulfilled'].includes(status) ? `<button class="btn btn-outline btn-sm" data-action="return">Return / RTO</button>` : ''}
          ${status === 'Cancelled' ? `<button class="btn btn-outline btn-sm" data-action="creditnote" title="Credit the customer for a cancelled, prepaid order">Credit note</button>` : ''}
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
        <h4 class="oms-detail-heading">Timeline <span class="text-muted">(${timeline.length})</span></h4>
        <div class="table-wrapper"><table>
          <thead><tr><th>When</th><th>What</th><th>Status</th><th>Document / by</th></tr></thead>
          <tbody>${timeline.map(e => `<tr>
            <td>${escapeHTMLText(omsWhen(e.at))}</td>
            <td>${escapeHTMLText(e.title || '')}${e.detail ? `<div class="text-muted oms-timeline-detail">${escapeHTMLText(e.detail)}</div>` : ''}</td>
            <td>${escapeHTMLText(e.status || '')}</td>
            <td>${e.ref_id && e.ref_id !== orderID ? (e.ref_doctype === 'SalesOrder' ? `<button class="action-btn" data-open-original="${escapeHTMLText(e.ref_id)}">${escapeHTMLText(e.ref_id)}</button>` : documentSearchLink(e.ref_doctype, e.ref_id)) : escapeHTMLText(e.by || '')}</td>
          </tr>`).join('')}</tbody>
        </table></div>
        <details class="oms-related-docs"><summary>Related documents by type</summary>
        ${section('Fulfillment tasks', d.fulfillment_tasks || [], [{ key: 'id', label: 'Task' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Location' }, { key: 'created_at', label: 'Created' }], 'FulfillmentTask')}
        ${section('Shipments', d.shipments || [], [{ key: 'id', label: 'Booking' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'AWB' }, { key: 'created_at', label: 'Created' }], 'LogisticsBooking')}
        ${section('Invoices', d.invoices || [], [{ key: 'id', label: 'Invoice' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Amount' }, { key: 'created_at', label: 'Created' }], 'SalesInvoice')}
        ${section('Returns', d.returns || [], [{ key: 'id', label: 'Return' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Type' }, { key: 'created_at', label: 'Created' }])}
        ${section('Refunds', d.refunds || [], [{ key: 'id', label: 'Refund' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Mode' }, { key: 'created_at', label: 'Created' }])}
        ${section('Notifications', d.notifications || [], [{ key: 'id', label: 'Log' }, { key: 'status', label: 'Dispatch' }, { key: 'detail', label: 'Event' }, { key: 'created_at', label: 'At' }])}
        ${section('Replacements and reships', d.replacements || [], [{ key: 'id', label: 'Order' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Kind' }, { key: 'created_at', label: 'Created' }])}
        ${section('Audit trail', d.audit_trail || [], [{ key: 'created_at', label: 'At' }, { key: 'user_id', label: 'User' }, { key: 'action', label: 'Action' }, { key: 'status', label: 'Result' }, { key: 'details', label: 'Detail' }])}
        </details>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.addEventListener('click', event => { if (event.target.closest('a[href^="#/setup/"]')) close(); });
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
    // FA-20261005-03: the Reserved order's next step - creates its pick
    // tasks, which then appear on the Pick page (Stage 88.2).
    // Stage 88.3.1: Accept goes through the Pending gate, which also answers the channel.
    if (action === 'fulfil') {
      const res = await apiFetch('/api/v1/oms/orders/bulk', { method: 'POST', body: JSON.stringify({ action: 'accept', order_ids: [orderID] }) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Failed to accept this order.'); return; }
      const why = Object.values((await res.json()).failed || {})[0];
      if (why) { await showCustomAlert(why, 'Accept'); return; }
      return after();
    }
    if (action === 'book') {
      sessionStorage.setItem('erp_marketplace_booking_context', JSON.stringify({
        order_id: orderID,
        fulfillment_task_id: taskForBooking.id,
        destination_pincode: order.shipping_pincode || String(order.shipping_address || '').match(/\b\d{6}\b/)?.[0] || ''
      }));
      close();
      setActiveMenu('menu-oms-ship');
      return renderView('oms:ship');
    }
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
    if (action === 'lines') return openOMSOrderLinesEdit(orderID, d.lines || [], () => openOMSOrderDetail(orderID));
    if (action === 'replace') return openOMSReplacement(orderID, d.lines || []);
    if (action === 'return') return openOMSReturn(orderID, d.lines || [], d.shipments || []);
    if (action === 'creditnote') {
      const reason = String((await showCustomPrompt('Reason code for the credit note?', 'CANCELLED', 'Credit note')) || '').trim();
      if (!reason) return;
      return omsPost(`/api/v1/orders/${encodeURIComponent(orderID)}/credit-notes`, { reason_code: reason }, 'The credit note was not raised.', after);
    }
  }));
  overlay.querySelectorAll('[data-open-original]').forEach(btn => btn.addEventListener('click', () => openOMSOrderDetail(btn.dataset.openOriginal)));
  // The next step's button: an action on this order presses its action-bar
  // button; a step done elsewhere opens that screen (Stage 73.2.14).
  const omsNextScreens = { returns: ['returns', 'menu-returns'], invoice: ['sales-invoices', 'menu-sales-invoices'], fulfillment: ['oms:pick', 'menu-oms-pick'], marketplace: ['oms:ship', 'menu-oms-ship'] };
  overlay.querySelector('[data-next-step]')?.addEventListener('click', event => {
    const step = event.currentTarget.dataset.nextStep;
    const inBar = overlay.querySelector(`.oms-action-bar [data-action="${CSS.escape(step)}"]`);
    if (inBar) { inBar.click(); return; }
    const target = omsNextScreens[step];
    if (!target) return;
    close();
    setActiveMenu(target[1]);
    renderView(target[0]);
  });
};

// Stage 73.2.15 - line changes up to the pick cutoff. The server re-reserves
// stock, recomputes the total and rechecks payment in one transaction; a
// released order goes back to Reserved so the next release picks the edit.
function openOMSOrderLinesEdit(orderID, lines, onSaved) {
  const editable = lines.filter(l => ['Reserved', 'Pending', 'On Hold'].includes(l.line_status));
  document.getElementById('oms-lines-edit-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-lines-edit-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Edit lines - ${escapeHTMLText(orderID)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body">
        <div class="table-wrapper"><table>
          <thead><tr><th>Line</th><th>SKU</th><th class="num">Qty</th><th class="num">Unit price</th><th>Remove</th></tr></thead>
          <tbody>${editable.map(l => `<tr data-line-row="${escapeHTMLText(l.line_id)}">
            <td style="font-family:monospace;">${escapeHTMLText(l.line_id)}</td>
            <td>${escapeHTMLText(l.sku)}</td>
            <td class="num"><input type="number" min="1" step="1" class="form-input oms-line-qty" aria-label="Quantity for ${escapeHTMLText(l.sku)}" value="${escapeHTMLText(String(l.qty))}" data-original="${escapeHTMLText(String(l.qty))}" style="max-width:90px;"></td>
            <td class="num"><input type="number" min="0" step="0.01" class="form-input oms-line-price" aria-label="Unit price for ${escapeHTMLText(l.sku)}" value="${escapeHTMLText(String(l.unit_price))}" data-original="${escapeHTMLText(String(l.unit_price))}" style="max-width:110px;"></td>
            <td><input type="checkbox" class="oms-line-remove" aria-label="Remove ${escapeHTMLText(l.sku)}"></td>
          </tr>`).join('') || '<tr><td colspan="5" class="text-muted">No line can change - every line is dispatched, cancelled or returned.</td></tr>'}</tbody>
        </table></div>
        <h4 class="oms-detail-heading">Add a line</h4>
        <div class="oms-add-line-row">
          <label class="form-label" for="oms-add-sku">Item code</label><input id="oms-add-sku" class="form-input" placeholder="Leave blank to add nothing">
          <label class="form-label" for="oms-add-qty">Qty</label><input id="oms-add-qty" type="number" min="1" step="1" class="form-input" value="1">
          <label class="form-label" for="oms-add-price">Unit price</label><input id="oms-add-price" type="number" min="0" step="0.01" class="form-input">
        </div>
        <div class="form-group"><label class="form-label" for="oms-lines-reason">Reason (kept on the order's history)</label>
          <input id="oms-lines-reason" class="form-input" placeholder="e.g. customer called to add a second pair"></div>
        <p class="text-muted">Lines change only until the warehouse starts picking and before the order is invoiced. More stock is reserved for any increase; if the confirmed payment no longer covers the new total, the order waits for payment.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-lines-cancel>Cancel</button>
        <button type="button" class="btn btn-primary" data-lines-save>Save line changes</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-lines-cancel]').addEventListener('click', close);
  overlay.querySelector('[data-lines-save]').addEventListener('click', async () => {
    const changes = [];
    overlay.querySelectorAll('[data-line-row]').forEach(row => {
      const lineID = row.dataset.lineRow;
      const qtyInput = row.querySelector('.oms-line-qty');
      const priceInput = row.querySelector('.oms-line-price');
      if (row.querySelector('.oms-line-remove').checked) { changes.push({ line_id: lineID, remove: true }); return; }
      const qtyChanged = qtyInput.value !== qtyInput.dataset.original;
      const priceChanged = priceInput.value !== priceInput.dataset.original;
      if (!qtyChanged && !priceChanged) return;
      const change = { line_id: lineID, qty: parseInt(qtyInput.value, 10) };
      if (priceChanged) change.unit_price = parseFloat(priceInput.value);
      changes.push(change);
    });
    const addSKU = document.getElementById('oms-add-sku').value.trim();
    if (addSKU) {
      const price = document.getElementById('oms-add-price').value;
      if (price === '') { showToast('Give the unit price for the new line.'); document.getElementById('oms-add-price').focus(); return; }
      changes.push({ sku: addSKU, qty: parseInt(document.getElementById('oms-add-qty').value, 10), unit_price: parseFloat(price) });
    }
    if (changes.length === 0) { showToast('Nothing changed.'); return; }
    const reason = document.getElementById('oms-lines-reason').value.trim();
    if (!reason) { showToast('Say why the lines are changing.'); document.getElementById('oms-lines-reason').focus(); return; }
    const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(orderID)}/lines`, { method: 'POST', body: JSON.stringify({ changes, reason }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The line changes were not saved.'); return; }
    const result = await res.json();
    close();
    showToast(`Lines saved. Total ${formatMoney(result.total_before)} -> ${formatMoney(result.total_after)}; order ${result.order_status}.${result.payment_consequence ? ' ' + result.payment_consequence + '.' : ''}`);
    if (onSaved) await onSaved();
  });
  overlay.querySelector('.oms-line-qty, #oms-add-sku')?.focus();
}

// Stage 73.2.15 - a no-charge Reship (same goods again) or Replacement (same
// or exchanged item) linked to the shipped original. The request key is made
// once per dialog, so a double click returns the first order, not a second.
// 140.12.18: a customer return or an RTO raised on the order itself. The
// store it comes back to may be left blank - it is routed to the nearest store
// by the delivery pincode.
function openOMSReturn(orderID, lines, shipments) {
  const shipped = lines.filter(l => ['Dispatched', 'Partially Dispatched', 'Delivered', 'Shipped'].includes(l.line_status) || Number(l.shipped_qty) > 0);
  const rtoBookings = shipments.filter(b => b.status === 'RTO');
  const requestKey = (crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random());
  document.getElementById('oms-return-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-return-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Return - ${escapeHTMLText(orderID)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body">
        <div class="form-group"><label class="form-label" for="oms-ret-type">Type</label>
          <select id="oms-ret-type" class="form-input"><option value="Customer Return">Customer return</option>${rtoBookings.length ? '<option value="RTO">RTO - parcel came back</option>' : ''}</select></div>
        ${rtoBookings.length ? `<div class="form-group"><label class="form-label" for="oms-ret-booking">Booking</label>
          <select id="oms-ret-booking" class="form-input">${rtoBookings.map(b => `<option value="${escapeHTMLText(b.id)}">${escapeHTMLText(b.detail || b.id)}</option>`).join('')}</select></div>` : ''}
        <div class="table-wrapper"><table>
          <thead><tr><th>SKU</th><th class="num">Shipped</th><th class="num">Return</th></tr></thead>
          <tbody>${shipped.map(l => `<tr data-ret-sku="${escapeHTMLText(l.sku)}">
            <td>${escapeHTMLText(l.sku)}</td><td class="num">${escapeHTMLText(String(l.shipped_qty || l.qty))}</td>
            <td class="num"><input type="number" min="0" max="${escapeHTMLText(String(l.shipped_qty || l.qty))}" step="1" value="0" class="form-input oms-ret-qty" aria-label="Quantity of ${escapeHTMLText(l.sku)} coming back" style="max-width:90px;"></td></tr>`).join('')}</tbody>
        </table></div>
        <div class="form-group"><label class="form-label" for="oms-ret-loc">Back to store</label>
          <input id="oms-ret-loc" class="form-input" placeholder="nearest by pincode"></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-ret-cancel>Cancel</button>
        <button type="button" class="btn btn-primary" data-ret-save>Raise return</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-ret-cancel]').addEventListener('click', close);
  overlay.querySelector('[data-ret-save]').addEventListener('click', async () => {
    const items = [];
    overlay.querySelectorAll('[data-ret-sku]').forEach(row => {
      const qty = parseInt(row.querySelector('.oms-ret-qty').value, 10) || 0;
      if (qty > 0) items.push({ sku: row.dataset.retSku, qty });
    });
    if (!items.length) { showToast('Enter how many of which item came back.'); return; }
    const type = overlay.querySelector('#oms-ret-type').value;
    const booking = overlay.querySelector('#oms-ret-booking');
    const res = await apiFetch('/api/v1/returns', { method: 'POST', body: JSON.stringify({
      request_type: type, original_order_id: orderID, booking_id: type === 'RTO' && booking ? booking.value : '',
      return_location: overlay.querySelector('#oms-ret-loc').value.trim(), items, idempotency_key: requestKey }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The return was not raised.'); return; }
    const out = await res.json().catch(() => ({}));
    close();
    showToast(`Return ${out.id || ''} raised - approve and receive it under Returns.`, { variant: 'success' });
    openOMSOrderDetail(orderID);
  });
}

function openOMSReplacement(orderID, lines) {
  const shipped = lines.filter(l => !['Cancelled'].includes(l.line_status));
  const requestKey = (crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random());
  document.getElementById('oms-replacement-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-replacement-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Reship or replace - ${escapeHTMLText(orderID)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body">
        <div class="form-group"><label class="form-label" for="oms-repl-kind">Kind</label>
          <select id="oms-repl-kind" class="form-input">
            <option value="Reship">Reship - the same goods again (lost or undelivered)</option>
            <option value="Replacement">Replacement - same or exchanged item (damaged or wrong)</option>
          </select></div>
        <div class="table-wrapper"><table>
          <thead><tr><th>Original line</th><th>SKU shipped</th><th class="num">Shipped</th><th class="num">Send again</th><th>Exchange to SKU</th></tr></thead>
          <tbody>${shipped.map(l => `<tr data-repl-line="${escapeHTMLText(l.line_id)}">
            <td style="font-family:monospace;">${escapeHTMLText(l.line_id)}</td>
            <td>${escapeHTMLText(l.sku)}</td>
            <td class="num">${escapeHTMLText(String(l.qty))}</td>
            <td class="num"><input type="number" min="0" max="${escapeHTMLText(String(l.qty))}" step="1" value="0" class="form-input oms-repl-qty" aria-label="Quantity to send again for ${escapeHTMLText(l.sku)}" style="max-width:90px;"></td>
            <td><input class="form-input oms-repl-sku" aria-label="Exchange ${escapeHTMLText(l.sku)} to" placeholder="same item" disabled></td>
          </tr>`).join('')}</tbody>
        </table></div>
        <div class="form-group"><label class="form-label" for="oms-repl-reason">Reason</label>
          <input id="oms-repl-reason" class="form-input" placeholder="e.g. courier lost the parcel"></div>
        <p class="text-muted">The new order is free of charge, ships from available stock and links both ways. Collect the damaged or wrong item with a return request.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-repl-cancel>Cancel</button>
        <button type="button" class="btn btn-primary" data-repl-save>Create order</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-repl-cancel]').addEventListener('click', close);
  const kind = overlay.querySelector('#oms-repl-kind');
  kind.addEventListener('change', () => overlay.querySelectorAll('.oms-repl-sku').forEach(input => { input.disabled = kind.value !== 'Replacement'; if (input.disabled) input.value = ''; }));
  overlay.querySelector('[data-repl-save]').addEventListener('click', async () => {
    const replLines = [];
    overlay.querySelectorAll('[data-repl-line]').forEach(row => {
      const qty = parseInt(row.querySelector('.oms-repl-qty').value, 10) || 0;
      if (qty > 0) replLines.push({ original_line_id: row.dataset.replLine, qty, sku: row.querySelector('.oms-repl-sku').value.trim() });
    });
    if (replLines.length === 0) { showToast('Enter how many to send again on at least one line.'); return; }
    const reason = document.getElementById('oms-repl-reason').value.trim();
    if (!reason) { showToast('Give a reason.'); document.getElementById('oms-repl-reason').focus(); return; }
    const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(orderID)}/replacement`, {
      method: 'POST', body: JSON.stringify({ kind: kind.value, lines: replLines, reason, request_key: requestKey })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The order was not created.'); return; }
    const result = await res.json();
    close();
    showToast(result.created ? `${kind.value} order ${result.order_id} created.` : `This request already created order ${result.order_id}.`);
    await openOMSOrderDetail(result.order_id);
  });
  kind.focus();
}

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
        <div class="form-group"><label class="form-label" for="oms-edit-custref">Customer PO / reference</label>
          <input type="text" id="oms-edit-custref" class="form-input" value="${escapeHTMLText(order.customer_reference || '')}"></div>
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
      customer_reference: document.getElementById('oms-edit-custref').value.trim(),
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
        <input type="text" id="mo-ref" class="form-input" placeholder="Your own order reference" title="Stored as the channel order id. Re-sending the same reference returns the existing order instead of creating a duplicate.">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-custref">Customer PO / reference <span class="po-optional">optional</span></label>
        <input type="text" id="mo-custref" class="form-input" placeholder="The customer's purchase order number" title="A business customer's own PO number or reference. Printed on the invoice.">
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
    <div class="po-header-grid">
      <div class="form-group"><label class="form-label" for="mo-address">Address line 1</label>
        <input id="mo-address" class="form-input" placeholder="House / flat, street" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="mo-address2">Address line 2</label>
        <input id="mo-address2" class="form-input" placeholder="Area, landmark" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="mo-city">City</label>
        <input id="mo-city" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="mo-state">State</label>
        <input id="mo-state" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="mo-pin">PIN code</label>
        <input id="mo-pin" class="form-input" inputmode="numeric" maxlength="6" autocomplete="off"></div>
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
  // Stage 76.3: State is the home country's list, as on every other form.
  if ((window.FIELD_META?.regions || []).length) {
    loadViewModule('/view-pickers.js?v=1').then(p => {
      const slot = document.getElementById('mo-state');
      if (slot && p.buildStateSelect) slot.replaceWith(p.buildStateSelect({ id: 'mo-state', name: 'shipping_state', value: '' }));
    }).catch(() => {});
  }
}

function renderManualOrderLines() {
  const body = document.getElementById('mo-lines-body');
  if (!body) return;
  body.innerHTML = manualOrderLines.map((l, i) => `
    <tr data-mo-line="${i}">
      <td><input type="text" class="form-input" data-mo-field="sku" value="${escapeHTMLText(l.sku)}" placeholder="Search item..." aria-label="Item for line ${i + 1}"></td>
      <td><input type="number" class="form-input num" data-mo-field="qty" min="1" step="1" value="${escapeHTMLText(l.qty)}" aria-label="Quantity for line ${i + 1}"></td>
      <td><input type="number" class="form-input num" data-mo-field="unit_price" min="0" step="0.01" value="${escapeHTMLText(l.unit_price)}" aria-label="Unit price for line ${i + 1}"></td>
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

  const val = id => (document.getElementById(id)?.value || '').trim();
  const address = val('mo-address');
  if (!address) { fail('Address line 1 is required - the Order Engine needs somewhere to ship to.'); return; }
  if (!/^\d{6}$/.test(val('mo-pin'))) { fail('Enter the 6-digit PIN code - couriers and stock sourcing go by it.'); return; }

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
      customer_reference: document.getElementById('mo-custref').value.trim(),
      shipping_address: address,
      shipping_address_line2: val('mo-address2'),
      shipping_city: val('mo-city'),
      shipping_state: val('mo-state'),
      shipping_pincode: val('mo-pin'),
      payment_status: document.getElementById('mo-payment').value,
      manual: true, // Stage 88.3.1 (D12): keyed in by staff, so it skips Pending
      lines
    })
  });
  if (!res) return;
  if (!res.ok) { fail(await getErrorMessage(res, 'Failed to create the order.')); return; }

  const data = await res.json();
  manualOrderLines = [{ sku: '', qty: '', unit_price: '' }];
  // FA-20261005-06: a repeated reference returns the order it already made.
  if (data.existing) {
    await showCustomAlert(`This reference already has order ${data.order_number || data.order_id}. Nothing new was created - the existing order is unchanged.`, 'Existing Order');
  } else {
    await showCustomAlert(`Order ${data.order_number || data.order_id} created. It is now in the Orders table with its fulfillment, shipment and invoice state, exactly like a channel order.`, 'Order Created');
  }
  (await loadTxnScreen()).txnDone('oms-screen');
  renderView('oms');
}

function openOMSDoctype(doctype, id) { currentDoctype = doctype; currentSearchQuery = id; currentTablePage = 1; renderView('doctype-table'); }
async function releaseOMSOrder(id) { const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/release-hold`, { method: 'POST' }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to release order hold.'); return; } renderView('oms'); }
async function cancelOMSOrder(id) { const reasonCode = await showCustomPrompt('Active Cancellation reason-code:', '', 'Cancel Order'); if (reasonCode === null || !reasonCode.trim()) return; const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: JSON.stringify({ reason_code: reasonCode.trim() }) }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to cancel order.'); return; } renderView('oms'); }

// Stage 88.2.7: OMS ▸ Settlements - marketplace payouts and the per-line
// reconciliation queue (Stage 13.7 / 35.8). Courier bookings and manifests,
// which shared the old Marketplace screen, are on OMS ▸ Ship.
async function renderOMSSettlementsView(container) {
  const settlementsRes = await apiFetch('/api/v1/doc/MarketplaceSettlement');
  if (!settlementsRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Settlements</h1>
    </div>
  `;
  container.appendChild(header);

  const settlements = settlementsRes.ok ? await settlementsRes.json() : [];
  // Stage 69: Settlements is a list with + New, the form a page of its own
  // (txn-screen.js).
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  const section = (el) => { el.style.marginBottom = '24px'; return el; };

  // --- Settlements panel ---
  const settlementPanel = document.createElement('div');
  settlementPanel.className = 'table-panel';
  settlementPanel.style.padding = '24px';
  settlementPanel.style.marginBottom = '24px';
  settlementPanel.innerHTML = `
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
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 220px;">
        <label class="form-label" for="mkt-order-pick">Orders in this payout</label>
        <div style="display:flex;gap:8px;"><input type="text" id="mkt-order-pick" class="form-input" placeholder="Find a saved order"><button class="btn btn-outline" id="mkt-order-add" type="button">Add</button></div>
        <div id="mkt-order-selected" aria-live="polite"></div>
      </div>
      <button class="btn btn-primary" id="mkt-reconcile-btn">Reconcile</button>
    </div>
    <div id="mkt-settlement-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
  `;
  const settlementList = document.createElement('div');
  settlementList.className = 'table-panel';
  settlementList.innerHTML = `
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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No settlements yet. Click <b>+ Reconcile Payout</b> once a marketplace payout file is available.</td></tr>`
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
  container.appendChild(section(txnPagedSection({ id: 'mkt-settlements', title: 'Settlements', page: 'Reconcile a payout',
    count: settlements.length, newLabel: 'Reconcile Payout', form: settlementPanel, list: settlementList, heading: 'h2' })));
  txnRestorePane('mkt-settlements');

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

  document.getElementById('mkt-reconcile-btn').addEventListener('click', submitMarketplaceReconcile);
  document.getElementById('mkt-order-add').addEventListener('click', addOMSSettlementOrder);
  document.getElementById('mkt-channel').addEventListener('change', () => { omsSettlementOrderIDs = []; renderOMSSettlementOrderSelection(); });
  attachLinkTypeahead(document.getElementById('mkt-order-pick'), 'SalesOrder', { noSetupHint: true });
  renderOMSSettlementOrderSelection();
  populateMarketplaceChannelOptions();
}

function renderOMSSettlementOrderSelection() {
  const host = document.getElementById('mkt-order-selected');
  if (!host) return;
  host.innerHTML = omsSettlementOrderIDs.length
    ? omsSettlementOrderIDs.map(id => `<span class="badge badge-secondary" style="margin:4px 4px 0 0;">${escapeHTMLText(id)} <button type="button" class="action-btn" data-remove-settlement-order="${escapeHTMLText(id)}" aria-label="Remove order ${escapeHTMLText(id)}">×</button></span>`).join('')
    : '<small class="text-muted">Choose saved orders; no comma-separated IDs needed.</small>';
  host.querySelectorAll('[data-remove-settlement-order]').forEach(button => button.addEventListener('click', () => {
    omsSettlementOrderIDs = omsSettlementOrderIDs.filter(id => id !== button.dataset.removeSettlementOrder);
    renderOMSSettlementOrderSelection();
  }));
}

async function addOMSSettlementOrder() {
  const input = document.getElementById('mkt-order-pick');
  const id = input?.value.trim();
  if (!id) { showToast('Choose an existing order first.', { variant: 'warning' }); return; }
  const res = await apiFetch(`/api/v1/doc/SalesOrder/${encodeURIComponent(id)}`);
  if (!res || !res.ok) { if (res) await showApiError(res, 'That order was not found.'); return; }
  const order = await res.json();
  const channel = document.getElementById('mkt-channel').value;
  if (String(order.channel || '') !== channel) {
    showToast(`Order ${id} belongs to ${order.channel || 'another source'}, not ${channel}.`, { variant: 'warning' });
    return;
  }
  if (!omsSettlementOrderIDs.includes(id)) omsSettlementOrderIDs.push(id);
  input.value = '';
  renderOMSSettlementOrderSelection();
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
        <h2 style="font-size: 16px; font-weight: 700; margin: 0;">Settlement Reconciliation</h2>
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
      <td style="font-family: monospace;">${escapeHTMLText(r.line_id || '')}</td>
      <td>${escapeHTMLText(r.channel || '')}</td>
      <td style="font-family: monospace;">${escapeHTMLText(r.channel_order_id || '')}</td>
      <td style="font-family: monospace;">${escapeHTMLText(r.order_id || '')}</td>
      <td>${escapeHTMLText(r.settlement_batch_id || '')}</td>
      <td>${(r.gross_amount ?? 0).toLocaleString()}</td>
      <td>${(r.expected_amount ?? 0).toLocaleString()}</td>
      <td>${(r.variance_amount ?? 0).toLocaleString()}</td>
      <td><span class="badge ${r.match_status === 'Disputed' ? 'badge-warning' : 'badge-danger'}">${escapeHTMLText(r.match_status || '')}</span></td>
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
    if (summaryEl) summaryEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Reconciliation pass failed.'))}</p>`;
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
  const corrected = await showCustomPrompt('Corrected gross amount from the marketplace:', '', 'Resolve Dispute', 'number');
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
  const orderIds = [...omsSettlementOrderIDs];

  if (!settlementId || !totalSale || totalSale <= 0 || orderIds.length === 0) {
    errorEl.textContent = 'Settlement ID, a positive Total Sale and at least one saved order from this channel are required.';
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
  omsSettlementOrderIDs = [];
  (await loadTxnScreen()).txnDone('mkt-settlements');
  renderView('oms:settlements');
}

// printShippingLabel (Stage 31.1.9) sends a booking's label straight to the
// bench's label printer. The payload is built server-side from the booking
// itself (engines.BuildShippingLabelPayload), so no label data passes
// through the browser and a thermal printer gets ZPL with a scannable AWB.
//
// Falls back to the on-screen label rather than to window.print(): the
// plain-text label is not a printable sheet, and someone whose QZ Tray is
// down still needs to read the AWB off the screen to write the docket.
//
// Stage 143.4: Print label shows the label first (viewShippingLabel); its
// Print sends it, and on failure the label stays on screen.
window.printShippingLabel = (bookingId) => viewShippingLabel(bookingId);

// viewShippingLabel (Stage 26.12.4) shows GenerateShippingLabel's plain-text
// label in a lightweight modal, the same .modal-overlay/.modal-container
// primitives viewPickList already uses, with Close and Print.
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
      <div class="modal-header"><h3 class="modal-title">Shipping Label: ${escapeHTMLText(bookingId)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><pre style="white-space: pre-wrap; font-family: monospace; font-size: 13px;">${escapeHTMLText(label)}</pre></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button><button type="button" class="btn btn-primary" data-label-send>Print</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.modal-footer .btn-secondary').addEventListener('click', close);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  const send = overlay.querySelector('[data-label-send]');
  send.addEventListener('click', async () => {
    send.disabled = true;
    if (await qzTryPrint('Shipping Label', { documentRef: bookingId, quiet: true })) { close(); showToast(`Label ${bookingId} sent to the printer.`, { variant: 'success' }); return; }
    send.disabled = false;
    showToast('The label printer is not reachable - read the AWB off this label.', { variant: 'warning' });
  });
  send.focus();
};

// Approvals inbox (Stage 13.8) - the checker side of the maker-checker
// engine. Lists every Pending Approval document across all approval-gated
// doctypes (GET /api/v1/approval/pending, already scoped server-side to the
// caller's role/location) with Approve/Reject actions against the already-
// working POST /api/v1/approval/decide.

export { renderOMSWorkbenchView, loadOMSBundleOperations, runOMSBundleOperation, loadOMSConnectorOperations, formatOMSLag, runOMSChannelSync, resolveOMSChannelSKU, loadOMSTiles, loadOMSOrders, renderOMSFacets, omsStatusBadge, renderOMSOrderTable, updateOMSBulkBar, runOMSBulkAction, runOMSGlobalSearch, loadOMSSavedViews, saveCurrentOMSView, applySelectedOMSView, deleteSelectedOMSView, omsPost, openOMSOrderEdit, renderManualOrderPanel, renderManualOrderLines, createManualOrder, createManualOrderInner, openOMSDoctype, releaseOMSOrder, cancelOMSOrder, renderOMSSettlementsView, renderOMSSettingsView, renderSettlementReconciliationPanel, loadSettlementVarianceQueue, runSettlementReconcile, raiseSettlementDisputeUI, resolveSettlementDisputeUI, writeOffSettlementVarianceUI, populateMarketplaceChannelOptions, submitMarketplaceReconcile };

// omsShipTo (Stage 76.3) is the delivery address on one line: line 1 plus the
// parts a manual order keeps apart (engines.ShippingAddressText).
function omsShipTo(o) {
  const tail = [o.shipping_state, o.shipping_pincode].filter(Boolean).join(' ');
  if (!o.shipping_address_line2 && !o.shipping_city && !o.shipping_pincode) return o.shipping_address || '';
  return [o.shipping_address, o.shipping_address_line2, o.shipping_city, tail].filter(Boolean).join(', ');
}
