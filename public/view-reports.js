// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentReportTab = 'exec-dashboard';

const REPORT_TABS = [
  { id: 'exec-dashboard', label: 'Dashboard' },
  { id: 'current-stock', label: 'Current Stock' },
  { id: 'sales-register', label: 'Sales Register' },
  { id: 'vendor-ledger', label: 'Vendor Ledger' },
  { id: 'payables-ageing', label: 'Payables Ageing' },
  { id: 'receivables-ageing', label: 'Receivables Ageing' },
  { id: 'gst-return-summary', label: 'GST Return Summary' },
  { id: 'report-catalog', label: 'Report Catalog' }
];

async function renderReportsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Reports</h1>
      <p class="page-subtitle">Current Stock, Sales Register, Vendor Ledger, Payables/Receivables Ageing, and GST Return Summary.</p>
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
  tabBar.innerHTML = REPORT_TABS.map(t =>
    `<button class="btn ${t.id === currentReportTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-report-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-report-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentReportTab = btn.getAttribute('data-report-tab');
      renderView('reports');
    });
  });

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  container.appendChild(panel);

  if (currentReportTab === 'exec-dashboard') {
    await renderExecDashboard(panel);
  } else if (currentReportTab === 'current-stock') {
    await renderCurrentStockReport(panel);
  } else if (currentReportTab === 'sales-register') {
    await renderSalesRegisterReport(panel);
  } else if (currentReportTab === 'vendor-ledger') {
    await renderVendorLedgerReport(panel);
  } else if (currentReportTab === 'payables-ageing') {
    await renderPayablesAgeingReport(panel);
  } else if (currentReportTab === 'receivables-ageing') {
    await renderReceivablesAgeingReport(panel);
  } else if (currentReportTab === 'gst-return-summary') {
    await renderGSTReturnSummaryReport(panel);
  } else if (currentReportTab === 'report-catalog') {
    await renderReportCatalogPanel(panel);
  }
}

// Stage 26.10.3: role-based executive dashboard - a frontend-only layer
// over the existing ReportDefinition catalog, no new backend endpoint.
// Every card/chart below just calls RunReport (via the same /reports/run/
// path the Report Catalog tab already uses) against a report registered for
// 26.10.1/26.10.5/17.10/26.12.7 - role-based column masking (Stage 20.39)
// and the catalog's own REPORT-0287 "masked" annotation apply for free, so
// a role without full visibility sees a "restricted" fallback on a
// currency-bearing card/chart instead of silently summing a redacted value.
async function fetchReportRows(reportId, params) {
  const qs = params && Object.keys(params).length ? '?' + reportCatalogQueryString(params) : '';
  const res = await apiFetch(`/api/v1/reports/run/${reportId}${qs}`);
  if (!res || !res.ok) return { rows: [], masked: false };
  const body = await res.json();
  return { rows: body.rows || [], masked: body.code === 'REPORT-0287' };
}

function execDashboardOpenReport(reportId) {
  reportCatalogSelectedId = reportId;
  currentReportTab = 'report-catalog';
  renderView('reports');
}

// Stage 37.11: role dashboards - the four cards above are now this
// screen's *default* tile set (engines.DefaultDashboardTiles' exact
// values), not its only one. A user can save the tiles they're looking at
// as a named DashboardLayout (private, or shared with everyone in their
// role), switch between saved layouts, and add/remove tiles from the
// catalog - all against the same fetchReportRows/execDashboardOpenReport
// this screen already had, so drill-through (37.11.4) needs no new code:
// every tile, default or custom, opens the same Report Catalog drill-down.
function defaultDashboardTiles() {
  return [
    { report_id: 'exception-stale-approvals', title: 'Stale Approvals' },
    { report_id: 'exception-failed-syncs', title: 'Failed Syncs' },
    { report_id: 'exception-negative-stock', title: 'Negative Stock Flags' },
    { report_id: 'sla-breach', title: 'SLA Breaches' }
  ];
}

let dashboardLayouts = [];
let dashboardSelectedLayoutId = '';
let dashboardCurrentTiles = null;
let dashboardReportCatalog = [];

async function loadDashboardLayouts() {
  const res = await apiFetch('/api/v1/dashboards/layouts');
  dashboardLayouts = (res && res.ok) ? ((await res.json()).layouts || []) : [];
  if (dashboardReportCatalog.length === 0) {
    const catRes = await apiFetch('/api/v1/reports/catalog');
    dashboardReportCatalog = (catRes && catRes.ok) ? (await catRes.json() || []) : [];
  }
}

async function renderExecDashboard(panel) {
  panel.innerHTML = `<p style="padding:16px; color:var(--text-muted);">Loading dashboard&hellip;</p>`;
  await loadDashboardLayouts();
  if (dashboardCurrentTiles === null) {
    dashboardCurrentTiles = defaultDashboardTiles();
  }
  await renderExecDashboardBody(panel);
}

async function renderExecDashboardBody(panel) {
  const username = localStorage.getItem('erp_username') || '';
  const tiles = dashboardCurrentTiles || defaultDashboardTiles();
  const results = await Promise.all(tiles.map(t => fetchReportRows(t.report_id, {})));

  // The 7-day sales trend is a bonus, not a tile - shown only when a
  // sales-register tile happens to be on the current layout (true for the
  // default layout, and for any custom layout that kept it), so a fully
  // custom layout without it doesn't render an empty chart section.
  const salesTileIdx = tiles.findIndex(t => t.report_id === 'sales-register');
  let trendHtml = '';
  if (salesTileIdx !== -1) {
    const salesRows = results[salesTileIdx];
    const days = [];
    for (let i = 6; i >= 0; i--) {
      const d = new Date();
      d.setDate(d.getDate() - i);
      days.push(localISODate(d));
    }
    const totalsByDay = Object.fromEntries(days.map(d => [d, 0]));
    const countsByDay = Object.fromEntries(days.map(d => [d, 0]));
    let amountsUsable = !salesRows.masked;
    salesRows.rows.forEach(r => {
      const day = String(r.created_at || '').slice(0, 10);
      if (!(day in countsByDay)) return;
      countsByDay[day]++;
      const amt = Number(r.sale_total);
      if (Number.isFinite(amt)) {
        totalsByDay[day] += amt;
      } else {
        amountsUsable = false;
      }
    });
    const trendValues = days.map(d => (amountsUsable ? totalsByDay[d] : countsByDay[d]));
    const trendLabel = amountsUsable ? 'Sales Total (last 7 days)' : 'Orders (last 7 days) — amounts restricted for your role';
    trendHtml = `
      <div style="padding: 20px; border-top: 1px solid var(--border-color);">
        <h3 style="margin: 0 0 12px; font-size: 14px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em;">${trendLabel}</h3>
        <div id="exec-dashboard-trend" data-days='${JSON.stringify(days)}' data-values='${JSON.stringify(trendValues)}'></div>
      </div>`;
  }

  const layoutOptions = dashboardLayouts.map(l =>
    `<option value="${escapeHTMLText(l.id)}" ${l.id === dashboardSelectedLayoutId ? 'selected' : ''}>${escapeHTMLText(l.name)}${l.owner !== username ? ' (shared)' : ''}</option>`
  ).join('');
  const addTileOptions = dashboardReportCatalog.map(d =>
    `<option value="${escapeHTMLText(d.id)}">${escapeHTMLText(d.label)}</option>`
  ).join('');

  panel.innerHTML = `
    <div style="padding: 16px 16px 0; display:flex; flex-wrap:wrap; align-items:center; gap:8px;">
      <p style="color: var(--text-muted); font-size: 13px; margin: 0; flex: 1 1 auto;">Click a tile to drill into that report in the Report Catalog tab.</p>
      <select id="dashboard-layout-picker" class="form-input" style="width:auto;">
        <option value="">Default</option>
        ${layoutOptions}
      </select>
      <button id="dashboard-save-btn" class="btn btn-outline btn-sm">Save as&hellip;</button>
      <button id="dashboard-delete-btn" class="btn btn-outline btn-sm" ${dashboardSelectedLayoutId ? '' : 'disabled'}>Delete Layout</button>
      <select id="dashboard-add-tile-picker" class="form-input" style="width:auto;">
        <option value="">+ Add tile&hellip;</option>
        ${addTileOptions}
      </select>
    </div>
    <div class="dashboard-stats-row" id="exec-dashboard-cards"></div>
    ${trendHtml}
  `;

  // Found while live-verifying 30.5.8: Reports is DEFAULT_VIEW, so this runs
  // on every login, and navigating away before its fetches land leaves this
  // container detached - the forEach below then threw an uncaught TypeError
  // on a null appendChild. Guarded the same way renderExecDashboardTrendChart
  // below already guards its own container.
  const cardsRow = document.getElementById('exec-dashboard-cards');
  if (!cardsRow) return;
  tiles.forEach((t, i) => {
    const value = results[i].rows.length;
    const card = document.createElement('div');
    card.className = 'stat-card';
    card.style.cursor = 'pointer';
    card.style.position = 'relative';
    card.title = 'Click to open in Report Catalog';
    card.innerHTML = `
      <span class="stat-label">${escapeHTMLText(t.title || t.report_id)}</span>
      <span class="stat-val" style="color:${value > 0 ? '#dc2626' : '#10b981'};">${value}</span>
      <span data-remove-tile="${i}" title="Remove tile" style="position:absolute; top:4px; right:8px; color:var(--text-muted); font-size:14px; line-height:1;">&times;</span>
    `;
    makeClickable(card, (e) => {
      if (e.target.closest('[data-remove-tile]')) return;
      execDashboardOpenReport(t.report_id);
    });
    cardsRow.appendChild(card);
  });
  cardsRow.querySelectorAll('[data-remove-tile]').forEach(el => {
    el.addEventListener('click', (e) => {
      e.stopPropagation();
      const idx = Number(el.getAttribute('data-remove-tile'));
      dashboardCurrentTiles = tiles.filter((_, i) => i !== idx);
      renderExecDashboardBody(panel);
    });
  });

  const trendEl = document.getElementById('exec-dashboard-trend');
  if (trendEl) {
    renderExecDashboardTrendChart(trendEl, JSON.parse(trendEl.dataset.days), JSON.parse(trendEl.dataset.values));
  }

  document.getElementById('dashboard-layout-picker').addEventListener('change', (e) => {
    dashboardSelectedLayoutId = e.target.value;
    const layout = dashboardLayouts.find(l => l.id === dashboardSelectedLayoutId);
    dashboardCurrentTiles = layout ? layout.tiles : defaultDashboardTiles();
    renderExecDashboardBody(panel);
  });
  document.getElementById('dashboard-delete-btn').addEventListener('click', async () => {
    if (!dashboardSelectedLayoutId) return;
    const res = await apiFetch(`/api/v1/dashboards/layouts/${encodeURIComponent(dashboardSelectedLayoutId)}`, { method: 'DELETE' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to delete this layout.'); return; }
    showToast('Layout deleted.');
    dashboardSelectedLayoutId = '';
    dashboardCurrentTiles = defaultDashboardTiles();
    await loadDashboardLayouts();
    renderExecDashboardBody(panel);
  });
  document.getElementById('dashboard-save-btn').addEventListener('click', async () => {
    const name = await showCustomPrompt('Name this dashboard:', '', 'Save Dashboard');
    if (name === null || !name.trim()) return;
    const role = await showCustomPrompt('Share with role? (leave blank to keep this dashboard private)', '', 'Share (optional)');
    const res = await apiFetch('/api/v1/dashboards/layouts', {
      method: 'POST',
      body: JSON.stringify({ name: name.trim(), role: (role || '').trim(), tiles: dashboardCurrentTiles })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to save this dashboard.'); return; }
    showToast('Dashboard saved.');
    await loadDashboardLayouts();
    renderExecDashboardBody(panel);
  });
  document.getElementById('dashboard-add-tile-picker').addEventListener('change', (e) => {
    const reportId = e.target.value;
    if (!reportId) return;
    const def = dashboardReportCatalog.find(d => d.id === reportId);
    dashboardCurrentTiles = [...tiles, { report_id: reportId, title: def ? def.label : reportId }];
    renderExecDashboardBody(panel);
  });
}

// A plain inline-SVG bar chart - no charting library (this codebase stays
// vanilla JS/CSS, no new frontend dependency), just enough for a 7-day
// at-a-glance trend.
function renderExecDashboardTrendChart(container, labels, values) {
  if (!container) return;
  const width = 640, height = 180, padding = 28;
  const maxVal = Math.max(1, ...values);
  const slotWidth = (width - padding * 2) / values.length;
  const barWidth = Math.max(4, slotWidth - 10);
  const bars = values.map((v, i) => {
    const barHeight = (v / maxVal) * (height - padding * 2);
    const x = padding + i * slotWidth + (slotWidth - barWidth) / 2;
    const y = height - padding - barHeight;
    return `
      <rect x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${barWidth.toFixed(1)}" height="${barHeight.toFixed(1)}" fill="var(--primary-color)" rx="3"></rect>
      <text x="${(x + barWidth / 2).toFixed(1)}" y="${height - padding + 16}" font-size="10" fill="var(--text-muted)" text-anchor="middle">${labels[i].slice(5)}</text>
      <text x="${(x + barWidth / 2).toFixed(1)}" y="${(y - 4).toFixed(1)}" font-size="10" fill="var(--text-main)" text-anchor="middle">${Math.round(v)}</text>
    `;
  }).join('');
  container.innerHTML = `<svg viewBox="0 0 ${width} ${height}" style="width:100%; max-width:${width}px; height:${height}px;">${bars}</svg>`;
}

// Inventory (Stage 21 QA fix): "Inventory" routed to a view name the router
// had no case for, always falling through to the "Module Setup Pending"
// mock screen - despite USER_GUIDE.md §5 explicitly documenting a working
// search-by-item stock screen. Reuses the same /api/v1/reports/current-stock
// endpoint Reports > Current Stock already calls (no new backend), but adds
// the client-side search box that endpoint's own report tab never had.
let inventorySearchQuery = '';
async function renderInventoryView(container) {
  const res = await apiFetch('/api/v1/reports/current-stock');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load inventory.', () => renderView('inventory')); return; }
  const rows = await res.json();
  inventorySearchQuery = '';

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Inventory</h1>
      <p class="page-subtitle">How much stock you have right now, and how much is actually free to sell (already-reserved stock excluded).</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        <input type="text" id="inventory-search-input" placeholder="Search by SKU or location...">
      </div>
    </div>
    <div class="table-wrapper" id="inventory-table-wrapper"></div>
  `;
  container.appendChild(panel);

  // Only the table body redraws on each keystroke - the search input itself
  // stays untouched so it doesn't lose focus/cursor position while typing
  // (matches renderDocTable()'s existing #doc-table-wrapper pattern).
  function draw() {
    const wrapper = document.getElementById('inventory-table-wrapper');
    const filtered = inventorySearchQuery
      ? rows.filter(r => `${r.sku} ${r.location_code}`.toLowerCase().includes(inventorySearchQuery))
      : rows;
    let html = `
      <table>
        <thead><tr><th>SKU</th><th>Location</th><th>On Hand</th><th>Available</th><th>Committed</th><th>Reserved</th><th>Safety Stock</th></tr></thead>
        <tbody>
    `;
    html += filtered.length === 0
      ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No stock on hand anywhere yet. Stock appears once a Goods Receipt is posted against a Purchase Order &mdash; see <b>Procurement &raquo; Goods Receipt</b>.</td></tr>`
      : filtered.map(r => `
          <tr>
            <td style="font-family: monospace;">${copyableCell(r.sku, r.sku)}</td>
            <td>${copyableCell(r.location_code, r.location_code)}</td>
            <td>${r.on_hand}</td>
            <td>${r.available}</td>
            <td>${r.committed}</td>
            <td>${r.reserved}</td>
            <td>${r.safety_stock}</td>
          </tr>
        `).join('');
    html += `</tbody></table>`;
    wrapper.innerHTML = html;
  }
  draw();
  document.getElementById('inventory-search-input').addEventListener('input', (e) => {
    inventorySearchQuery = e.target.value.toLowerCase();
    draw();
  });
}

async function renderCurrentStockReport(panel) {
  const res = await apiFetch('/api/v1/reports/current-stock');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>SKU</th><th>Location</th><th>On Hand</th><th>Available</th><th>Committed</th><th>Reserved</th><th>Safety Stock</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No stock on hand yet. Stock appears once a Goods Receipt is posted against a Purchase Order.</td></tr>`
    : rows.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.sku}</td>
          <td>${r.location_code}</td>
          <td>${r.on_hand}</td>
          <td>${r.available}</td>
          <td>${r.committed}</td>
          <td>${r.reserved}</td>
          <td>${r.safety_stock}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderSalesRegisterReport(panel) {
  const res = await apiFetch('/api/v1/reports/sales-register');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>Cart Number</th><th>Location</th><th>Payment Mode</th><th>Status</th><th>Sale Total</th><th>Date</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No completed sales yet. Sales appear here as soon as a cart is checked out at <b>Point of Sale</b>.</td></tr>`
    : rows.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.cart_number}</td>
          <td>${r.location}</td>
          <td>${r.payment_mode}</td>
          <td><span class="badge badge-success">${r.status}</span></td>
          <td>${r.sale_total.toLocaleString()}</td>
          <td>${new Date(r.created_at).toLocaleString()}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderVendorLedgerReport(panel) {
  const res = await apiFetch('/api/v1/reports/vendor-ledger');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>Vendor</th><th>PO Number</th><th>Total Amount</th><th>Status</th><th>Date</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No purchase orders yet. Raise one under <b>Procurement &raquo; Purchase Order</b>; this ledger follows each one through receipt and payment.</td></tr>`
    : rows.map(r => `
        <tr>
          <td>${r.vendor || ''}</td>
          <td style="font-family: monospace;">${r.po_number || r.id}</td>
          <td>${(r.total_amount ?? 0).toLocaleString()}</td>
          <td>${r.status}</td>
          <td>${new Date(r.created_at).toLocaleString()}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderPayablesAgeingReport(panel) {
  const res = await apiFetch('/api/v1/reports/payables-ageing');
  if (!res) return;
  const buckets = res.ok ? await res.json() : [];
  panel.innerHTML = `
    <p style="padding: 16px 16px 0; font-size: 13px; color: var(--text-muted);">
      Buckets Approved-but-not-yet-Closed purchase orders by age since creation.
    </p>
    <table>
      <thead><tr><th>Age Bucket</th><th>PO Count</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${b.amount.toLocaleString()}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

async function renderReceivablesAgeingReport(panel) {
  const res = await apiFetch('/api/v1/reports/receivables-ageing');
  if (!res) return;
  const buckets = res.ok ? await res.json() : [];
  panel.innerHTML = `
    <p style="padding: 16px 16px 0; font-size: 13px; color: var(--text-muted);">
      Buckets Approved-but-not-yet-Paid sales invoices (Finance &gt; Sales Invoice) by age since creation.
    </p>
    <table>
      <thead><tr><th>Age Bucket</th><th>Invoice Count</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${b.amount.toLocaleString()}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

// GST Return Summary (Stage 20.29): report-only GSTR-1/3B-shaped
// aggregation, explicitly not e-filing/IRN. Defaults to the current
// calendar month since a GST return is always filed for a specific period.
async function renderGSTReturnSummaryReport(panel) {
  const now = new Date();
  const monthStart = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`;
  const today = localISODate(now);
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; padding: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="gst-start-date">From</label>
        <input type="date" id="gst-start-date" class="form-input" value="${monthStart}">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="gst-end-date">To</label>
        <input type="date" id="gst-end-date" class="form-input" value="${today}">
      </div>
      <button class="btn btn-primary" id="gst-summary-btn">Run</button>
    </div>
    <p style="padding: 0 16px; font-size: 13px; color: var(--text-muted);">
      Report-only summary of output tax already calculated per-transaction - not e-invoice/IRN filing.
    </p>
    <div id="gst-summary-result" style="padding: 0 16px 16px;"></div>
  `;
  const runReport = async () => {
    const startDate = document.getElementById('gst-start-date').value;
    const endDate = document.getElementById('gst-end-date').value;
    const resultEl = document.getElementById('gst-summary-result');
    const res = await apiFetch(`/api/v1/reports/gst-return-summary?start=${startDate}&end=${endDate}`);
    if (!res) return;
    if (!res.ok) {
      resultEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Failed to load GST return summary.')}</p>`;
      return;
    }
    const s = await res.json();
    // Stage 26.6.11: the non-taxable row only renders when there is something
    // in it. Most tenants sell nothing exempt, and three permanent zeroes
    // would be noise on the one report where every figure is meant to be a
    // number someone files.
    const nonTaxable = s.non_taxable_value || 0;
    const nonTaxableRow = nonTaxable === 0 ? '' : `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Exempt Value</span><span class="stat-val">${(s.exempt_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Nil-Rated Value</span><span class="stat-val">${(s.nil_rated_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Zero-Rated Value</span><span class="stat-val">${(s.zero_rated_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Total Non-Taxable</span><span class="stat-val">${nonTaxable.toLocaleString()}</span></div>
      </div>
    `;
    resultEl.innerHTML = `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Taxable Value</span><span class="stat-val">${s.taxable_value.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output CGST</span><span class="stat-val">${s.output_cgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output SGST</span><span class="stat-val">${s.output_sgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output IGST</span><span class="stat-val">${s.output_igst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Total Tax Liability</span><span class="stat-val">${s.total_tax_liability.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Input Tax Credit</span><span class="stat-val">${(s.input_tax_credit || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Net GST Payable</span><span class="stat-val">${(s.net_tax_payable ?? s.total_tax_liability).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Transactions</span><span class="stat-val">${s.transaction_count}</span></div>
      </div>
      ${nonTaxableRow}
    `;
  };
  document.getElementById('gst-summary-btn').addEventListener('click', runReport);
  await runReport();
}

// Report Catalog (Stage 20 Track B.4, 20.35-20.40): ONE generic panel
// driving every report in engines/report_registry.go's catalog - old (the
// 6 tabs above) and new alike, via GET /api/v1/reports/catalog's metadata.
// Adding a future report from here on means registering a Go function, not
// writing a new render function like the tabs above each needed. Saved
// filters (20.36) reuse the generic ReportFilterPreset doctype directly -
// no dedicated save/list endpoint exists or is needed. Async export
// (20.37) and drill-down (20.38) are both generic too, driven by
// has_drill_down/columns metadata rather than per-report frontend code.
let reportCatalogDefs = [];
let reportCatalogSelectedId = '';

async function renderReportCatalogPanel(panel) {
  const res = await apiFetch('/api/v1/reports/catalog');
  if (!res) return;
  if (!res.ok) {
    panel.innerHTML = `<p class="login-error" style="padding:16px;">Failed to load report catalog.</p>`;
    return;
  }
  reportCatalogDefs = await res.json();
  if (!reportCatalogSelectedId && reportCatalogDefs.length > 0) {
    reportCatalogSelectedId = reportCatalogDefs[0].id;
  }

  const byCategory = {};
  reportCatalogDefs.forEach(d => {
    const cat = d.category || 'Other';
    (byCategory[cat] = byCategory[cat] || []).push(d);
  });

  panel.innerHTML = `
    <div style="padding: 16px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; border-bottom: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0; min-width: 220px;">
        <label class="form-label" for="rc-report-select">Report</label>
        <select id="rc-report-select" class="form-select">
          ${Object.keys(byCategory).sort().map(cat => `
            <optgroup label="${cat}">
              ${byCategory[cat].map(d => `<option value="${d.id}" ${d.id === reportCatalogSelectedId ? 'selected' : ''}>${d.label}</option>`).join('')}
            </optgroup>
          `).join('')}
        </select>
      </div>
      <div id="rc-params" style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;"></div>
      <div class="form-group" style="margin-bottom: 0; min-width: 180px;">
        <label class="form-label" for="rc-saved-filter">Saved Filter</label>
        <select id="rc-saved-filter" class="form-select"><option value="">— none —</option></select>
      </div>
      <div class="form-group" style="margin-bottom: 0; min-width: 180px;">
        <label class="form-label" for="rc-column-profile">Column Profile</label>
        <select id="rc-column-profile" class="form-select"><option value="">— Default columns —</option></select>
      </div>
      <button class="btn btn-primary" id="rc-run-btn">Run</button>
      <button class="btn btn-outline" id="rc-columns-btn">Columns</button>
      <button class="btn btn-outline" id="rc-save-filter-btn">Save Filter</button>
      <button class="btn btn-outline" id="rc-export-btn">Export in Background</button>
    </div>
    <div id="rc-columns-panel" class="hidden" style="padding: 12px 16px 0;"></div>
    <div id="rc-export-status" style="padding: 0 16px;"></div>
    <div id="rc-results" style="padding: 16px;"></div>
  `;

  document.getElementById('rc-report-select').addEventListener('change', (e) => {
    reportCatalogSelectedId = e.target.value;
    renderReportCatalogParams();
    loadReportCatalogSavedFilters();
    initReportCatalogColumns();
    loadReportColumnProfiles();
    document.getElementById('rc-columns-panel').classList.add('hidden');
  });
  document.getElementById('rc-run-btn').addEventListener('click', runReportCatalogReport);
  document.getElementById('rc-columns-btn').addEventListener('click', toggleReportColumnsPanel);
  document.getElementById('rc-save-filter-btn').addEventListener('click', saveReportCatalogFilter);
  document.getElementById('rc-export-btn').addEventListener('click', exportReportCatalogReport);
  document.getElementById('rc-saved-filter').addEventListener('change', (e) => {
    applyReportCatalogSavedFilter(e.target.value);
  });
  document.getElementById('rc-column-profile').addEventListener('change', (e) => {
    applyReportColumnProfile(e.target.value);
  });

  renderReportCatalogParams();
  initReportCatalogColumns();
  await loadReportCatalogSavedFilters();
  await loadReportColumnProfiles();
}

function currentReportCatalogDef() {
  return reportCatalogDefs.find(d => d.id === reportCatalogSelectedId);
}

// ---- Report column control + profiles (Stage 28.3) ----
// A column chooser (show/hide + reorder) on the report catalog, plus saveable
// column profiles in two scopes: Personal (only the owner sees it) and Universal
// (shared with everyone; creatable only by privileged roles - also enforced
// server-side in handlers_core_doc_engine.go). Reuses the generic
// ReportColumnProfile doctype the same way saved filters reuse ReportFilterPreset.
let reportCatalogColumnState = []; // [{key, label, visible}] in display order, current report
let reportColumnProfiles = [];     // ReportColumnProfile docs for the current report
let reportCatalogLastResult = null;
let reportCatalogLastParams = {};

function initReportCatalogColumns() {
  const def = currentReportCatalogDef();
  reportCatalogColumnState = (def && def.columns ? def.columns : []).map(c => ({ key: c.key, label: c.label, visible: true }));
}

// effectiveReportColumns maps the current show/hide/order state onto the
// server-returned column set (which carries the authoritative label + any
// sensitive masking). Falls back to the server set if state is empty or would
// hide everything.
function effectiveReportColumns(resultColumns) {
  if (!reportCatalogColumnState.length) return resultColumns;
  const byKey = {};
  resultColumns.forEach(c => { byKey[c.key] = c; });
  const ordered = reportCatalogColumnState.filter(s => s.visible && byKey[s.key]).map(s => byKey[s.key]);
  return ordered.length ? ordered : resultColumns;
}

function toggleReportColumnsPanel() {
  const panel = document.getElementById('rc-columns-panel');
  if (!panel) return;
  if (panel.classList.contains('hidden')) {
    renderReportColumnsPanel();
    panel.classList.remove('hidden');
  } else {
    panel.classList.add('hidden');
  }
}

function renderReportColumnsPanel() {
  const panel = document.getElementById('rc-columns-panel');
  if (!panel) return;
  const role = localStorage.getItem('erp_role') || '';
  const canUniversal = role === 'Super Admin' || role === 'HR/Admin' || role === 'Store Manager';
  const smallBtn = 'padding:2px 8px; font-size:12px;';
  const rows = reportCatalogColumnState.map((c, i) => `
    <div style="display:flex; align-items:center; gap:8px; padding:4px 0;">
      <input type="checkbox" data-col-key="${cfgEsc(c.key)}" ${c.visible ? 'checked' : ''}>
      <span style="flex:1;">${cfgEsc(c.label)}</span>
      <button class="btn btn-outline rc-col-up" data-idx="${i}" style="${smallBtn}" ${i === 0 ? 'disabled' : ''}>&uarr;</button>
      <button class="btn btn-outline rc-col-down" data-idx="${i}" style="${smallBtn}" ${i === reportCatalogColumnState.length - 1 ? 'disabled' : ''}>&darr;</button>
    </div>`).join('');
  panel.innerHTML = `
    <div style="padding:14px; border:1px solid var(--border-color); border-radius:8px; background:var(--panel-bg); max-width:460px;">
      <div style="font-weight:600; margin-bottom:6px;">Show, hide, and reorder columns</div>
      ${rows || '<p class="page-subtitle" style="margin:0;">This report has no columns.</p>'}
      <div style="display:flex; gap:8px; margin-top:14px; flex-wrap:wrap;">
        <button class="btn btn-primary" id="rc-col-apply" style="${smallBtn}">Apply</button>
        <button class="btn btn-outline" id="rc-col-save" style="${smallBtn}">Save as Profile&hellip;</button>
        <button class="btn btn-outline" id="rc-col-reset" style="${smallBtn}">Reset to default</button>
      </div>
    </div>`;
  panel.querySelectorAll('[data-col-key]').forEach(cb => {
    cb.addEventListener('change', () => {
      const st = reportCatalogColumnState.find(s => s.key === cb.getAttribute('data-col-key'));
      if (st) st.visible = cb.checked;
    });
  });
  panel.querySelectorAll('.rc-col-up').forEach(b => b.addEventListener('click', () => moveReportColumn(parseInt(b.dataset.idx, 10), -1)));
  panel.querySelectorAll('.rc-col-down').forEach(b => b.addEventListener('click', () => moveReportColumn(parseInt(b.dataset.idx, 10), 1)));
  document.getElementById('rc-col-apply').addEventListener('click', applyReportColumnState);
  document.getElementById('rc-col-reset').addEventListener('click', () => {
    initReportCatalogColumns();
    renderReportColumnsPanel();
    const sel = document.getElementById('rc-column-profile'); if (sel) sel.value = '';
    applyReportColumnState();
  });
  document.getElementById('rc-col-save').addEventListener('click', () => saveReportColumnProfile(canUniversal));
}

function moveReportColumn(idx, dir) {
  const j = idx + dir;
  if (j < 0 || j >= reportCatalogColumnState.length) return;
  const arr = reportCatalogColumnState;
  const tmp = arr[idx]; arr[idx] = arr[j]; arr[j] = tmp;
  renderReportColumnsPanel();
}

// Re-render the already-fetched result with the current column state, without
// re-running the report.
function applyReportColumnState() {
  const resultsEl = document.getElementById('rc-results');
  if (resultsEl && reportCatalogLastResult) {
    renderReportCatalogResultTable(resultsEl, reportCatalogLastResult, reportCatalogLastParams);
  }
}

async function loadReportColumnProfiles() {
  const select = document.getElementById('rc-column-profile');
  if (!select) return;
  select.innerHTML = `<option value="">— Default columns —</option>`;
  const res = await apiFetch('/api/v1/doc/ReportColumnProfile');
  reportColumnProfiles = (res && res.ok) ? await res.json() : [];
  const username = localStorage.getItem('erp_username') || '';
  reportColumnProfiles
    .filter(p => p.report_id === reportCatalogSelectedId && (p.scope === 'Universal' || p.owner === username))
    .forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = (p.scope === 'Universal' ? '🌐 ' : '') + p.name;
      select.appendChild(opt);
    });
}

function applyReportColumnProfile(profileId) {
  const def = currentReportCatalogDef();
  if (!def) return;
  if (!profileId) { initReportCatalogColumns(); applyReportColumnState(); return; }
  const p = reportColumnProfiles.find(x => x.id === profileId);
  if (!p) return;
  let saved = [];
  try { saved = JSON.parse(p.columns || '[]'); } catch (e) { /* ignore */ }
  const labelByKey = {};
  (def.columns || []).forEach(c => { labelByKey[c.key] = c.label; });
  // Honor the profile's saved order/visibility for columns that still exist,
  // then append any columns added to the report since the profile was saved.
  const seen = new Set();
  const state = [];
  saved.forEach(c => {
    if (labelByKey[c.key] !== undefined && !seen.has(c.key)) {
      state.push({ key: c.key, label: labelByKey[c.key], visible: c.visible !== false });
      seen.add(c.key);
    }
  });
  (def.columns || []).forEach(c => {
    if (!seen.has(c.key)) state.push({ key: c.key, label: c.label, visible: true });
  });
  reportCatalogColumnState = state;
  const panel = document.getElementById('rc-columns-panel');
  if (panel && !panel.classList.contains('hidden')) renderReportColumnsPanel();
  applyReportColumnState();
}

async function saveReportColumnProfile(canUniversal) {
  const def = currentReportCatalogDef();
  if (!def) return;
  const name = await showCustomPrompt('Name this column profile:', '', 'Save Column Profile');
  if (!name) return;
  let scope = 'Personal';
  if (canUniversal) {
    scope = (await showCustomConfirm('Save as a Universal profile, shared with everyone? Choose Cancel to keep it Personal (visible only to you).'))
      ? 'Universal' : 'Personal';
  }
  const username = localStorage.getItem('erp_username') || '';
  const columns = reportCatalogColumnState.map(c => ({ key: c.key, visible: c.visible }));
  const id = `RCP-${Date.now()}`;
  const res = await apiFetch('/api/v1/doc/ReportColumnProfile', {
    method: 'POST',
    body: JSON.stringify({ id, report_id: def.id, name, owner: username, scope, columns: JSON.stringify(columns), status: 'Active' })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to save column profile.'); return; }
  showToast(`Column profile saved (${scope}).`, { variant: 'success' });
  await loadReportColumnProfiles();
  const sel = document.getElementById('rc-column-profile'); if (sel) sel.value = id;
}

function renderReportCatalogParams() {
  const def = currentReportCatalogDef();
  const container = document.getElementById('rc-params');
  if (!def || !container) return;
  container.innerHTML = (def.params || []).map(p => `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="rc-param-${p.key}">${p.label}</label>
      <input type="${p.type === 'date' ? 'date' : 'text'}" id="rc-param-${p.key}" class="form-input"
             data-param-key="${p.key}" ${p.required ? 'required' : ''} style="width: 160px;">
    </div>
  `).join('');
}

function collectReportCatalogParams() {
  const params = {};
  document.querySelectorAll('#rc-params [data-param-key]').forEach(input => {
    if (input.value) params[input.getAttribute('data-param-key')] = input.value;
  });
  return params;
}

function reportCatalogQueryString(params) {
  return Object.entries(params).map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
}

async function runReportCatalogReport() {
  const def = currentReportCatalogDef();
  const resultsEl = document.getElementById('rc-results');
  if (!def || !resultsEl) return;
  const params = collectReportCatalogParams();
  for (const p of (def.params || [])) {
    if (p.required && !params[p.key]) {
      resultsEl.innerHTML = `<p class="login-error">"${p.label}" is required.</p>`;
      return;
    }
  }
  const res = await apiFetch(`/api/v1/reports/run/${def.id}?${reportCatalogQueryString(params)}`);
  if (!res) return;
  if (!res.ok) {
    resultsEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Failed to run report.'))}</p>`;
    return;
  }
  const result = await res.json();
  renderReportCatalogResultTable(resultsEl, result, params);
}

// 2026-10-10: report timestamps (Stock Ledger's Date, created/submitted
// columns) showed as raw "2026-10-07T08:08:13.022073Z". A value that is a full
// timestamp reads as a local date and time; plain dates and everything else
// are shown as they are. The CSV export is unchanged.
const REPORT_TIMESTAMP_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;
function reportCellText(val) {
  if (val === null || val === undefined) return '';
  if (typeof val === 'string' && REPORT_TIMESTAMP_RE.test(val)) {
    const d = new Date(val);
    if (!isNaN(d)) return d.toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });
  }
  return val;
}

function renderReportCatalogResultTable(container, result, params) {
  reportCatalogLastResult = result;
  reportCatalogLastParams = params;
  const columns = effectiveReportColumns(result.columns || []);
  const rows = result.rows || [];
  const drillKey = columns.length > 0 ? columns[0].key : null;
  // Stage 36.2.6: any report row that names a product can put that product in
  // someone's inbox. Keyed off the presence of an item_code column rather than
  // a hardcoded list of report ids, so every present and future PIM readiness
  // report gets the affordance without being enumerated here - which is the
  // whole reason the report catalog describes its columns as data.
  const assignKey = columns.some(c => c.key === 'item_code') ? 'item_code' : null;
  const canAssign = assignKey !== null && typeof canCreateDoctype === 'function' && canCreateDoctype('PIMTask');
  const extraCols = (result.has_drill_down ? 1 : 0) + (canAssign ? 1 : 0);
  let html = `<table><thead><tr>`;
  columns.forEach(c => { html += `<th>${escapeHTMLText(c.label)}</th>`; });
  if (result.has_drill_down) html += `<th>Details</th>`;
  if (canAssign) html += `<th>Task</th>`;
  html += `</tr></thead><tbody>`;
  if (rows.length === 0) {
    html += `<tr><td colspan="${columns.length + extraCols}" style="text-align:center; color:var(--text-muted);">No rows matched. Widen the date range or clear a filter above, then run the report again.</td></tr>`;
  }
  rows.forEach((row, idx) => {
    html += `<tr>`;
    columns.forEach(c => {
      html += `<td>${escapeHTMLText(reportCellText(row[c.key]))}</td>`;
    });
    if (result.has_drill_down) {
      const rowKeyVal = drillKey ? String(row[drillKey]) : '';
      html += `<td><button class="action-btn" data-report-drill-index="${idx}" data-report-id="${escapeHTMLText(result.id)}" data-report-row-key="${escapeHTMLText(rowKeyVal)}">View Details</button></td>`;
    }
    if (canAssign) {
      const itemCode = row[assignKey] === null || row[assignKey] === undefined ? '' : String(row[assignKey]);
      html += itemCode
        ? `<td><button class="action-btn" data-report-assign-item="${escapeHTMLText(itemCode)}" data-report-label="${escapeHTMLText(result.label || result.id)}">Assign task</button></td>`
        : `<td></td>`;
    }
    html += `</tr><tr id="rc-drilldown-${idx}" class="hidden"><td colspan="${columns.length + extraCols}"></td></tr>`;
  });
  html += `</tbody></table>`;
  container.innerHTML = html;
  container.dataset.params = JSON.stringify(params);
  container.querySelectorAll('[data-report-drill-index]').forEach(button => {
    button.addEventListener('click', () => runReportCatalogDrillDown(
      button.dataset.reportId,
      button.dataset.reportRowKey,
      Number(button.dataset.reportDrillIndex)
    ));
  });
  container.querySelectorAll('[data-report-assign-item]').forEach(button => {
    button.addEventListener('click', () => {
      if (typeof window.openPIMAssignTaskModal === 'function') {
        window.openPIMAssignTaskModal(button.dataset.reportAssignItem, button.dataset.reportLabel);
      }
    });
  });
}

async function runReportCatalogDrillDown(reportId, rowKey, rowIdx) {
  const params = JSON.parse(document.getElementById('rc-results').dataset.params || '{}');
  const res = await apiFetch(`/api/v1/reports/drilldown/${encodeURIComponent(reportId)}?row=${encodeURIComponent(rowKey)}&${reportCatalogQueryString(params)}`);
  if (!res) return;
  const targetRow = document.getElementById(`rc-drilldown-${rowIdx}`);
  if (!targetRow) return;
  if (!res.ok) {
    targetRow.classList.remove('hidden');
    targetRow.querySelector('td').innerHTML = `<span class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Drill-down failed.'))}</span>`;
    return;
  }
  const data = await res.json();
  const drillRows = data.rows || [];
  const keys = drillRows.length > 0 ? Object.keys(drillRows[0]) : [];
  let inner = `<div style="padding:8px 0;"><table style="width:100%;"><thead><tr>${keys.map(k => `<th>${escapeHTMLText(k)}</th>`).join('')}</tr></thead><tbody>`;
  inner += drillRows.length === 0
    ? `<tr><td colspan="${keys.length || 1}" style="text-align:center; color:var(--text-muted);">No underlying rows for this figure &mdash; it is a computed total with no individual transactions behind it in the selected period.</td></tr>`
    : drillRows.map(r => `<tr>${keys.map(k => `<td>${escapeHTMLText(r[k] === null || r[k] === undefined ? '' : r[k])}</td>`).join('')}</tr>`).join('');
  inner += `</tbody></table></div>`;
  targetRow.classList.remove('hidden');
  targetRow.querySelector('td').innerHTML = inner;
}

async function loadReportCatalogSavedFilters() {
  const select = document.getElementById('rc-saved-filter');
  if (!select) return;
  select.innerHTML = `<option value="">— none —</option>`;
  const res = await apiFetch('/api/v1/doc/ReportFilterPreset');
  if (!res || !res.ok) return;
  const presets = await res.json();
  const username = localStorage.getItem('erp_username') || '';
  presets
    .filter(p => p.report_id === reportCatalogSelectedId && p.owner === username)
    .forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = p.name;
      select.appendChild(opt);
    });
}

function applyReportCatalogSavedFilter(presetId) {
  if (!presetId) return;
  apiFetch(`/api/v1/doc/ReportFilterPreset/${encodeURIComponent(presetId)}`).then(async (res) => {
    if (!res || !res.ok) return;
    const preset = await res.json();
    let params = {};
    try { params = JSON.parse(preset.params || '{}'); } catch (e) { /* ignore */ }
    Object.entries(params).forEach(([k, v]) => {
      const input = document.querySelector(`#rc-params [data-param-key="${k}"]`);
      if (input) input.value = v;
    });
  });
}

async function saveReportCatalogFilter() {
  const def = currentReportCatalogDef();
  if (!def) return;
  const name = await showCustomPrompt('Name this saved filter:', '', 'Save Filter');
  if (!name) return;
  const params = collectReportCatalogParams();
  const username = localStorage.getItem('erp_username') || '';
  const presetId = `RFP-${Date.now()}`;
  const res = await apiFetch('/api/v1/doc/ReportFilterPreset', {
    method: 'POST',
    body: JSON.stringify({
      id: presetId, report_id: def.id, name, owner: username,
      params: JSON.stringify(params), status: 'Active'
    })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to save filter.');
    return;
  }
  await loadReportCatalogSavedFilters();
  const savedFilterSelect = document.getElementById('rc-saved-filter');
  if (savedFilterSelect) savedFilterSelect.value = presetId;
}

async function exportReportCatalogReport() {
  const def = currentReportCatalogDef();
  const statusEl = document.getElementById('rc-export-status');
  if (!def || !statusEl) return;
  const params = collectReportCatalogParams();
  const res = await apiFetch('/api/v1/reports/export', {
    method: 'POST',
    body: JSON.stringify({ report_id: def.id, params })
  });
  if (!res) return;
  if (!res.ok) {
    statusEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Failed to queue export.')}</p>`;
    return;
  }
  const job = await res.json();
  statusEl.innerHTML = `<p>Export queued (job ${job.id})... waiting for it to complete.</p>`;
  pollReportExportJob(job.id, statusEl);
}

async function pollReportExportJob(jobId, statusEl) {
  const res = await apiFetch(`/api/v1/reports/export/${jobId}`);
  if (!res) return;
  if (!res.ok) {
    statusEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Export job lookup failed.')}</p>`;
    return;
  }
  const job = await res.json();
  if (job.status === 'Pending') {
    setTimeout(() => pollReportExportJob(jobId, statusEl), 2000);
    return;
  }
  if (job.status === 'Failed') {
    statusEl.innerHTML = `<p class="login-error">Export failed.</p>`;
    return;
  }
  statusEl.innerHTML = `<p><button class="action-btn" id="rc-download-btn">Download CSV</button></p>`;
  document.getElementById('rc-download-btn').addEventListener('click', () => downloadReportExportCSV(jobId));
}

// This endpoint requires the same Bearer-token auth as every other API call
// (apiMiddleware has no query-string-token fallback), so a plain <a href>
// opened in a new tab can't authenticate itself - fetch the CSV through the
// normal authenticated apiFetch() and hand the browser a Blob URL instead.
async function downloadReportExportCSV(jobId) {
  const res = await apiFetch(`/api/v1/reports/export/${jobId}?download=1`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to download export.');
    return;
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${jobId}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// RFQ / Vendor Quote / Quote Comparison (Stage 13.12) - RFQ/VendorQuote
// creation and listing use the same generic doc API as Vendor/Customer
// (Stage 13.9); this screen adds the comparison view and winner-selection
// action on top, which the generic endpoint doesn't provide.
let selectedRFQId = '';

async function renderRFQView(container) {
  const res = await apiFetch('/api/v1/doc/RFQ');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">RFQ / Quotes</h1>
      <p class="page-subtitle">Request quotes from vendors and compare them before creating a Purchase Order.</p>
    </div>
  `;
  container.appendChild(header);

  const rfqs = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New RFQ</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('RFQ Number', 'RFQ', '160px')}
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 200px;">
        <label class="form-label" for="rfq-description">Item / Requirement Description</label>
        <input type="text" id="rfq-description" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="rfq-quantity">Quantity</label>
        <input type="number" id="rfq-quantity" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="rfq-target-date">Target Date</label>
        <input type="date" id="rfq-target-date" class="form-input">
      </div>
      <button class="btn btn-primary" id="rfq-create-btn">Create RFQ</button>
    </div>
    <div id="rfq-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.style.marginBottom = '24px';
  let listHtml = `
    <table>
      <thead><tr><th>RFQ Number</th><th>Description</th><th>Quantity</th><th>Target Date</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  listHtml += rfqs.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No RFQs yet. Use <b>Create RFQ</b> above to invite quotes from your vendors.</td></tr>`
    : rfqs.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.description || ''}</td>
          <td>${r.quantity ?? ''}</td>
          <td>${r.target_date || ''}</td>
          <td><span class="badge ${r.status === 'Closed' ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(r.status || '')}</span></td>
          <td style="white-space: nowrap;">
            <button class="action-btn" ${actionAttrs('viewRFQQuotes', [r.id])}>${r.status === 'Closed' ? 'View Quotes' : 'Vendors &amp; Quotes'}</button>
            ${r.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('setRFQStatus', [r.id, 'Sent'])}>Mark as Sent</button>` : ''}
            ${r.status === 'Sent' ? `<button class="action-btn" ${actionAttrs('setRFQStatus', [r.id, 'Closed'])}>Close</button>` : ''}
          </td>
        </tr>
      `).join('');
  listHtml += `</tbody></table>`;
  listPanel.innerHTML = listHtml;
  container.appendChild(listPanel);

  document.getElementById('rfq-create-btn').addEventListener('click', createRFQ);

  if (selectedRFQId) {
    const quotesContainer = document.createElement('div');
    quotesContainer.id = 'rfq-quotes-container';
    container.appendChild(quotesContainer);
    await renderRFQQuotesPanel(quotesContainer, selectedRFQId, rfqs.find(r => r.id === selectedRFQId));
  }
}

async function createRFQ() {
  // BLD-036: guard against a double-click creating two RFQs.
  await guardAgainstDoubleSubmit(document.getElementById('rfq-create-btn'), 'Creating...', createRFQInner);
}

async function createRFQInner() {
  const errorEl = document.getElementById('rfq-form-error');
  errorEl.classList.add('hidden');

  const description = document.getElementById('rfq-description').value.trim();
  const quantity = parseFloat(document.getElementById('rfq-quantity').value) || 0;
  const targetDate = document.getElementById('rfq-target-date').value;

  if (!description || !quantity) {
    errorEl.textContent = 'Description and Quantity are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/RFQ', {
    method: 'POST',
    body: JSON.stringify({ description, quantity, target_date: targetDate, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create RFQ.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('rfq');
}

function viewRFQQuotes(rfqId) {
  selectedRFQId = rfqId;
  renderView('rfq');
}

// Stage 55.4: "Unable to change status of RFQ / how to assign vendors to an
// RFQ?" The status had no control at all on this screen, and an RFQ had no
// record of who it was sent to. Status moves along the transitions the
// server already enforces (Draft -> Sent -> Closed; selecting a winning quote
// also closes it), and the invited vendors are kept on the RFQ itself.

// rfqInvitedVendors reads the stored list; tolerant of an empty or
// hand-edited value so a malformed field never breaks the screen.
function rfqInvitedVendors(rfq) {
  try {
    const list = JSON.parse((rfq && rfq.invited_vendors) || '[]');
    return Array.isArray(list) ? list.map(String).filter(Boolean) : [];
  } catch (e) {
    return [];
  }
}

// saveRFQ writes the whole record back - the generic update replaces a
// document's data rather than merging into it - with its version, so a
// concurrent edit is refused instead of silently overwritten.
async function saveRFQ(rfqId, changes) {
  const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (!loaded) return false;
  if (!loaded.ok) { await showApiError(loaded, 'Could not load the RFQ.'); return false; }
  const { id, ...rfq } = await loaded.json();
  const payload = { ...rfq, ...changes };
  if (typeof rfq.version === 'number') payload.expected_version = rfq.version;
  const res = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'Could not update the RFQ.'); return false; }
  return true;
}

async function setRFQStatus(rfqId, status) {
  if (status === 'Sent') {
    const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
    const rfq = loaded && loaded.ok ? await loaded.json() : null;
    if (rfq && rfqInvitedVendors(rfq).length === 0 &&
        !await showCustomConfirm('No vendors are invited to this RFQ yet. Mark it as sent anyway?', 'Mark RFQ as Sent')) return;
  }
  if (status === 'Closed' &&
      !await showCustomConfirm('Close this RFQ without choosing a winning quote? No further quotes can be recorded against it.', 'Close RFQ')) return;
  if (await saveRFQ(rfqId, { status })) {
    showToast(`RFQ marked ${status}.`, { variant: 'success' });
    renderView('rfq');
  }
}

async function inviteRFQVendor(rfqId) {
  const input = document.getElementById('rfq-invite-vendor');
  const code = input ? input.value.trim() : '';
  if (!code) { input?.focus(); return; }
  const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (!loaded || !loaded.ok) return;
  const list = rfqInvitedVendors(await loaded.json());
  if (list.some(v => v.toLowerCase() === code.toLowerCase())) {
    showToast('That vendor is already invited.', { variant: 'warning' });
    return;
  }
  if (await saveRFQ(rfqId, { invited_vendors: JSON.stringify([...list, code]) })) renderView('rfq');
}

async function removeRFQVendor(rfqId, code) {
  const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (!loaded || !loaded.ok) return;
  const list = rfqInvitedVendors(await loaded.json()).filter(v => v !== code);
  if (await saveRFQ(rfqId, { invited_vendors: JSON.stringify(list) })) renderView('rfq');
}

window.setRFQStatus = setRFQStatus;
window.inviteRFQVendor = inviteRFQVendor;
window.removeRFQVendor = removeRFQVendor;

async function renderRFQQuotesPanel(container, rfqId, rfq) {
  const res = await apiFetch(`/api/v1/rfq/quotes?rfq_id=${encodeURIComponent(rfqId)}`);
  if (!res) return;
  const quotes = res.ok ? await res.json() : [];
  const isClosed = rfq && rfq.status === 'Closed';

  // Stage 55.4: who this RFQ went to. Invite while it is open; a quote from a
  // vendor who was never invited still records, and invites them on the way.
  const invited = rfqInvitedVendors(rfq);
  const vendorsPanel = document.createElement('div');
  vendorsPanel.className = 'table-panel';
  vendorsPanel.style.padding = '24px';
  vendorsPanel.style.marginBottom = '16px';
  vendorsPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 4px;">Invited vendors</h2>
    <p style="font-size: 13px; color: var(--text-muted); margin-bottom: 12px;">
      ${isClosed ? 'This RFQ is closed.' : 'Add the vendors you are asking for a quote. Mark the RFQ as <b>Sent</b> once you have sent it to them.'}
    </p>
    <div class="rfq-vendor-chips" style="display:flex; flex-wrap:wrap; gap:8px; margin-bottom:${isClosed ? '0' : '12px'};">
      ${invited.length === 0 ? '<span style="color: var(--text-muted); font-size: 13px;">No vendors invited yet.</span>' :
        invited.map(v => `<span class="badge badge-secondary" style="display:inline-flex; align-items:center; gap:6px;">
            <span data-link-doctype="Vendor" data-link-ref="${escapeHTMLText(v)}">${escapeHTMLText(v)}</span>
            ${isClosed ? '' : `<button type="button" class="copy-chip" aria-label="Remove ${escapeHTMLText(v)}" title="Remove" ${actionAttrs('removeRFQVendor', [rfqId, v])}>&times;</button>`}
          </span>`).join('')}
    </div>
    ${isClosed ? '' : `
      <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
        <div class="form-group" style="margin-bottom:0;">
          <label class="form-label" for="rfq-invite-vendor">Vendor</label>
          <input type="text" id="rfq-invite-vendor" class="form-input" style="width: 220px;" autocomplete="off">
        </div>
        <button class="btn btn-outline" id="rfq-invite-btn" type="button">Invite vendor</button>
      </div>`}
  `;
  container.appendChild(vendorsPanel);
  const inviteInput = document.getElementById('rfq-invite-vendor');
  if (inviteInput) {
    attachLinkTypeahead(inviteInput, 'Vendor');
    document.getElementById('rfq-invite-btn').addEventListener('click', () => inviteRFQVendor(rfqId));
  }

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Quotes for ${escapeHTMLText((rfq && rfq.code) || rfqId)}</h2>
    ${isClosed ? '' : `
      <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 20px;">
        ${autoNumberField('Quote Number', 'QTN', '160px')}
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-vendor">Vendor</label>
          <input type="text" id="quote-vendor" class="form-input" style="width: 160px;">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-price">Quoted Price</label>
          <input type="number" id="quote-price" class="form-input" style="width: 130px;">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-lead-time">Lead Time (days)</label>
          <input type="number" id="quote-lead-time" class="form-input" style="width: 130px;">
        </div>
        <button class="btn btn-primary" id="quote-submit-btn">Submit Quote</button>
      </div>
      <div id="quote-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    `}
    <table>
      <thead><tr><th>Quote Number</th><th>Vendor</th><th>Quoted Price</th><th>Lead Time (days)</th><th>Status</th><th></th></tr></thead>
      <tbody>
        ${quotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No quotes submitted yet. Use <b>Submit Quote</b> above to record a vendor's response, then <b>Select as Winner</b>.</td></tr>`
          : quotes.map(q => `
            <tr>
              <td style="font-family: monospace;">${q.code || q.id}</td>
              <td>${q.vendor || ''}</td>
              <td>${(q.quoted_price ?? 0).toLocaleString()}</td>
              <td>${q.lead_time_days ?? ''}</td>
              <td><span class="badge ${q.status === 'Selected' ? 'badge-success' : q.status === 'Rejected' ? 'badge-danger' : 'badge-secondary'}">${q.status}</span></td>
              <td>${!isClosed && q.status === 'Submitted' ? `<button class="action-btn" ${actionAttrs('selectWinningQuote', [rfqId, q.id])}>Select as Winner</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);

  const submitBtn = document.getElementById('quote-submit-btn');
  if (submitBtn) submitBtn.addEventListener('click', () => submitVendorQuote(rfqId));
  const quoteVendorInput = document.getElementById('quote-vendor');
  if (quoteVendorInput) attachLinkTypeahead(quoteVendorInput, 'Vendor');
}

async function submitVendorQuote(rfqId) {
  // BLD-036: guard against a double-click creating two VendorQuotes.
  await guardAgainstDoubleSubmit(document.getElementById('quote-submit-btn'), 'Submitting...', () => submitVendorQuoteInner(rfqId));
}

async function submitVendorQuoteInner(rfqId) {
  const errorEl = document.getElementById('quote-form-error');
  errorEl.classList.add('hidden');

  const vendor = document.getElementById('quote-vendor').value.trim();
  const quotedPrice = parseFloat(document.getElementById('quote-price').value);
  const leadTime = parseFloat(document.getElementById('quote-lead-time').value) || 0;

  if (!vendor || !quotedPrice) {
    errorEl.textContent = 'Vendor and Quoted Price are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/VendorQuote', {
    method: 'POST',
    body: JSON.stringify({
      rfq_id: rfqId, vendor,
      quoted_price: quotedPrice, lead_time_days: leadTime, status: 'Submitted'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit quote.';
    errorEl.classList.remove('hidden');
    return;
  }
  // A quote from a vendor who was not on the invite list puts them on it,
  // so the list stays a true record of who is in this RFQ.
  const loaded = await apiFetch(`/api/v1/doc/RFQ/${encodeURIComponent(rfqId)}`);
  if (loaded && loaded.ok) {
    const list = rfqInvitedVendors(await loaded.json());
    if (!list.some(v => v.toLowerCase() === vendor.toLowerCase())) {
      await saveRFQ(rfqId, { invited_vendors: JSON.stringify([...list, vendor]) });
    }
  }
  renderView('rfq');
}

async function selectWinningQuote(rfqId, quoteId) {
  const confirmed = await showCustomConfirm('This will mark this quote as the winner, reject all other quotes, and close the RFQ. Continue?', 'Select Winning Quote');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/rfq/select-quote', {
    method: 'POST',
    body: JSON.stringify({ rfq_id: rfqId, quote_id: quoteId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to select winning quote.', 'Selection Failed');
    return;
  }
  renderView('rfq');
}

// QZ Tray silent printing (Stage 31.1).
//
// Every print path in this app used to be window.print() into a hidden
// @media print area, which pops the browser dialog and cannot choose a
// printer. These helpers route a job to a *named* OS printer instead, so a
// packing bench with a thermal label printer and an A4 invoice printer sends
// each document to the right one with a single click.
//
// Degrades rather than breaks: if QZ Tray is not installed or not running,
// qzTryPrint returns false and the caller falls back to the existing print
// sheet. Nobody is blocked from printing because the bridge is down.


export { renderReportsView, fetchReportRows, execDashboardOpenReport, defaultDashboardTiles, loadDashboardLayouts, renderExecDashboard, renderExecDashboardBody, renderExecDashboardTrendChart, renderInventoryView, renderCurrentStockReport, renderSalesRegisterReport, renderVendorLedgerReport, renderPayablesAgeingReport, renderReceivablesAgeingReport, renderGSTReturnSummaryReport, renderReportCatalogPanel, currentReportCatalogDef, initReportCatalogColumns, effectiveReportColumns, toggleReportColumnsPanel, renderReportColumnsPanel, moveReportColumn, applyReportColumnState, loadReportColumnProfiles, applyReportColumnProfile, saveReportColumnProfile, renderReportCatalogParams, collectReportCatalogParams, reportCatalogQueryString, runReportCatalogReport, renderReportCatalogResultTable, runReportCatalogDrillDown, loadReportCatalogSavedFilters, applyReportCatalogSavedFilter, saveReportCatalogFilter, exportReportCatalogReport, pollReportExportJob, downloadReportExportCSV, renderRFQView, createRFQ, createRFQInner, viewRFQQuotes, renderRFQQuotesPanel, submitVendorQuote, submitVendorQuoteInner, selectWinningQuote };
