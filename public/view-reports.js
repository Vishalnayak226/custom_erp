// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentReportTab = 'exec-dashboard';

const REPORT_TABS = [
  { id: 'exec-dashboard', label: 'Dashboard' },
  { id: 'dashboards', label: 'Custom dashboards' }, // Stage 100 (view-dashboards.js)
  { id: 'current-stock', label: 'Current Stock' },
  { id: 'sales-register', label: 'Sales Register' },
  { id: 'vendor-ledger', label: 'Vendor Ledger' },
  { id: 'payables-ageing', label: 'Payables Ageing' },
  { id: 'receivables-ageing', label: 'Receivables Ageing' },
  { id: 'gst-return-summary', label: 'GST Return Summary' },
  { id: 'report-catalog', label: 'Report Catalog' },
  { id: 'schedules', label: 'Schedules' }
];

// Stage 88.1 (user, 2026-10-18: "Report also. It will be grouped with Sales and show sub group of Sales
// related in another flyout"): the Reports menu has one row per area, each opening its reports beside it -
// the fixed report pages filed under their area, then the catalog's. app.js calls this the first time the
// menu opens (data-fill). Every report is its own route: "reports:<tab>" or "reports:<catalog id>".
const REPORT_TAB_AREAS = { 'current-stock': 'Inventory', 'sales-register': 'Sales', 'vendor-ledger': 'Procurement', 'payables-ageing': 'Finance', 'receivables-ageing': 'Finance', 'gst-return-summary': 'Finance' };
// Stage 75.3: the user's report catalog groups by its own modules (Sales &
// POS, Procurement & GRN...); each sits beside the older group it extends.
const REPORT_AREA_ORDER = ['Sales & POS', 'Sales', 'Procurement & GRN', 'Procurement', 'Inventory', 'Stock Transfer', 'GST & Compliance',
  'E-commerce & Integration', 'OMS', 'WMS', 'CRM', 'Accounting', 'Finance', 'Expense Management', 'Manufacturing', 'PIM / PIMS', 'PIM',
  'HRMS', 'HR', 'MIS & Management', 'Master Data & Audit', 'Assets', 'Service'];

async function fillReportsMenu(flyout) {
  const res = await apiFetch('/api/v1/reports/catalog');
  if (!res || !res.ok) throw new Error('report catalog unavailable');
  const areas = {};
  const add = (area, id, label, number = '') => {
    const list = areas[area] = areas[area] || [];
    if (!list.some(r => r.label === label)) list.push({ id, label, number });
  };
  REPORT_TABS.filter(t => REPORT_TAB_AREAS[t.id]).forEach(t => add(REPORT_TAB_AREAS[t.id], t.id, t.label));
  (await res.json()).forEach(d => add(d.category || 'Other', d.id, d.label, d.number));
  const rank = area => (REPORT_AREA_ORDER.indexOf(area) + 1) || 99;
  const rows = Object.keys(areas).sort((a, b) => rank(a) - rank(b) || a.localeCompare(b)).map(area =>
    `<li class="has-subflyout"><a class="menu-item submenu-parent">${escapeHTMLText(area)}</a><ul class="menu-subflyout">${areas[area].map(r =>
      `<li><a class="menu-item" id="menu-report-${escapeHTMLText(r.id)}" data-go="reports:${escapeHTMLText(r.id)}" data-like="menu-reports"><span>${escapeHTMLText(r.label)}</span>${r.number ? `<span class="menu-code">${escapeHTMLText(r.number)}</span>` : ''}</a></li>`).join('')}</ul></li>`).join('');
  flyout.lastElementChild.insertAdjacentHTML('beforebegin', rows);
  // Stage 100: custom dashboards sit under the Dashboard.
  flyout.firstElementChild.insertAdjacentHTML('afterend', '<li><a class="menu-item" id="menu-report-dashboards" data-go="reports:dashboards" data-like="menu-reports"><span>Custom dashboards</span></a></li>');
}

async function renderReportsView(container) {
  if (REPORT_TABS.some(t => t.id === currentViewTab)) currentReportTab = currentViewTab;
  else if (currentViewTab) { currentReportTab = 'report-catalog'; reportCatalogSelectedId = currentViewTab; }
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Reports</h1>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  container.appendChild(panel);

  if (currentReportTab === 'exec-dashboard') {
    await renderExecDashboard(panel);
  } else if (currentReportTab === 'dashboards') {
    const open = sessionStorage.getItem('erp_open_dashboard') || '';
    sessionStorage.removeItem('erp_open_dashboard');
    await (await import('./view-dashboards.js' + new URL(import.meta.url).search)).renderDashboardsTab(panel, open);
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
  } else if (currentReportTab === 'schedules') {
    await renderReportSchedulesPanel(panel);
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
  renderView(`reports:${reportId}`);
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
function stockTableHTML(rows, showTasks = false, searched = false) {
  const quantity = value => Number.isFinite(Number(value)) ? Number(value).toLocaleString() : '—';
  const actions = showTasks ? '<th>Next task</th>' : '';
  const cells = showTasks ? 8 : 7;
  return `<table>
    <thead><tr><th>Item</th><th>Location</th><th title="Book stock before reservations and holds">On hand</th><th title="Matches the reservation admission calculation">Available to sell</th><th>Reserved</th><th>Held</th><th>Why unavailable</th>${actions}</tr></thead>
    <tbody>${rows.length === 0
      ? `<tr><td colspan="${cells}" class="text-center py-8 text-muted">${searched ? 'No stock rows match this search. Clear the search to see all locations.' : 'No stock on hand yet. Post a Goods Receipt against a Purchase Order to add stock.'}</td></tr>`
      : rows.map(r => {
          const sku = String(r.sku || '');
          const location = String(r.location_code || '');
          const holdRoute = '#/view/place-hold';
          return `<tr>
            <td data-link-doctype="Item" data-link-ref="${escapeHTMLText(sku)}">${copyableCell(sku, sku)}</td>
            <td data-link-doctype="Location" data-link-ref="${escapeHTMLText(location)}">${copyableCell(location, location)}</td>
            <td>${quantity(r.on_hand)}</td><td>${quantity(r.available_to_sell)}</td>
            <td>${quantity(r.reserved)}</td><td>${quantity(r.hold_qty)}</td>
            <td><details><summary>Breakdown</summary><span>Base available: ${quantity(r.available)}; committed: ${quantity(r.committed)}; safety: ${quantity(r.safety_stock)}; blocked: ${quantity(r.blocked)}; QC hold: ${quantity(r.qc_hold)}; damaged: ${quantity(r.damaged)}; channel buffer: ${quantity(r.channel_buffer)}.</span></details></td>
            ${showTasks ? `<td><a href="#/view/transfers" data-stock-task="transfer" data-stock-sku="${escapeHTMLText(sku)}" data-stock-location="${escapeHTMLText(location)}">Transfer</a> · <a href="#/view/cycle-count" data-stock-task="count" data-stock-sku="${escapeHTMLText(sku)}" data-stock-location="${escapeHTMLText(location)}">Count</a> · <a href="${holdRoute}" data-stock-task="hold" data-stock-sku="${escapeHTMLText(sku)}" data-stock-location="${escapeHTMLText(location)}">Hold</a> · <a href="#/view/reports%3Astock-ledger" data-stock-task="movements" data-stock-sku="${escapeHTMLText(sku)}" data-stock-location="${escapeHTMLText(location)}" title="Every movement of this item here, with its source document">Movements</a></td>` : ''}
          </tr>`;
        }).join('')}</tbody></table>`;
}

async function renderInventoryView(container) {
  // Stage 88.5: Inventory's Dashboard, Ledger and MRP Change are pages of their own (inventory:<page>).
  if (currentViewTab && await (await import('./view-inventory-erp.js' + new URL(import.meta.url).search)).renderInventoryPage(currentViewTab, container)) return;
  // 134.6 (47.10.3): one page at a time, searched on the server - the whole
  // stock table no longer comes to the browser.
  const pageSize = 100;
  let offset = 0;
  let rows = [];
  let total = 0;
  const load = async () => {
    const res = await apiFetch(`/api/v1/reports/current-stock?limit=${pageSize}&offset=${offset}&q=${encodeURIComponent(inventorySearchQuery)}`);
    if (!res || !res.ok) return res;
    rows = await res.json();
    total = Number(res.headers.get('X-Total-Count')) || rows.length;
    return res;
  };
  inventorySearchQuery = '';
  const first = await load();
  if (!first) return;
  if (!first.ok) { renderErrorPanel(container, 'Failed to load inventory.', () => renderView('inventory')); return; }

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Stock on Hand</h1>
    </div>
  `;
  // Stage 72.6: butter paper counted at week end, tape used for the shop -
  // posted as consumption (store manager / admin, as the server enforces).
  const role = localStorage.getItem('erp_role') || '';
  if (role === 'Store Manager' || isAdminRoleName(role)) {
    const ceBtn = document.createElement('button');
    ceBtn.className = 'btn btn-outline';
    ceBtn.type = 'button';
    ceBtn.textContent = 'Consumption Entry';
    ceBtn.addEventListener('click', async () => {
      const m = await import('./view-consumption.js' + new URL(import.meta.url).search);
      m.openConsumptionEntry(header.parentNode || container);
    });
    header.appendChild(ceBtn);
  }
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <p style="padding:16px 16px 0; color:var(--text-muted); font-size:13px;">On hand is book stock. Available to sell subtracts reservations, safety stock, blocked/QC/damaged stock, channel buffer and active holds from base availability. Open Breakdown for the exact buckets; a hold does not change physical on-hand.</p>
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        <input type="text" id="inventory-search-input" placeholder="Search by SKU or location...">
      </div>
    </div>
    <div class="table-wrapper" id="inventory-table-wrapper"></div>
    <div class="table-controls" id="inventory-pager" style="justify-content: flex-end; gap: 8px;"></div>
  `;
  container.appendChild(panel);
  panel.querySelector('#inventory-table-wrapper').addEventListener('click', event => {
    const link = event.target.closest('[data-stock-task]');
    if (!link) return;
    const context = JSON.stringify({ sku: link.dataset.stockSku, location: link.dataset.stockLocation });
    const task = link.dataset.stockTask;
    if (task === 'hold' || task === 'transfer' || task === 'count') {
      sessionStorage.setItem(`erp_stock_${task}_context`, context);
    }
    // 73.B.7: Movements opens the Stock Ledger report on this item and
    // location, already run.
    if (task === 'movements') {
      reportParamMemory['stock-ledger'] = { sku: link.dataset.stockSku, location_code: link.dataset.stockLocation };
      reportAutoRunId = 'stock-ledger';
    }
  });

  // Only the table body redraws on each keystroke - the search input itself
  // stays untouched so it doesn't lose focus/cursor position while typing
  // (matches renderDocTable()'s existing #doc-table-wrapper pattern).
  function draw() {
    const wrapper = document.getElementById('inventory-table-wrapper');
    if (!wrapper) return;
    wrapper.innerHTML = stockTableHTML(rows, true, !!inventorySearchQuery);
    sweepLinkNames(wrapper);
    const pager = document.getElementById('inventory-pager');
    pager.innerHTML = total > rows.length || offset > 0
      ? `<span class="text-muted" style="font-size: 13px;">${(offset + 1).toLocaleString()}-${(offset + rows.length).toLocaleString()} of ${total.toLocaleString()}</span>
         <button type="button" class="btn btn-outline btn-sm" data-page="-1" ${offset === 0 ? 'disabled' : ''}>Previous</button>
         <button type="button" class="btn btn-outline btn-sm" data-page="1" ${offset + rows.length >= total ? 'disabled' : ''}>Next</button>`
      : '';
  }
  draw();
  document.getElementById('inventory-pager').addEventListener('click', async (e) => {
    const b = e.target.closest('[data-page]');
    if (!b || b.disabled) return;
    offset = Math.max(0, offset + Number(b.dataset.page) * pageSize);
    if (await load()) draw();
  });
  let searchTimer = null;
  document.getElementById('inventory-search-input').addEventListener('input', (e) => {
    inventorySearchQuery = e.target.value.trim().toLowerCase();
    clearTimeout(searchTimer);
    searchTimer = setTimeout(async () => { offset = 0; if (await load()) draw(); }, 250);
  });
}

async function renderCurrentStockReport(panel) {
  const res = await apiFetch('/api/v1/reports/current-stock?limit=200');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  const total = Number(res.headers.get('X-Total-Count')) || rows.length;
  panel.innerHTML = stockTableHTML(rows) + (total > rows.length
    ? `<p class="text-muted" style="padding: 12px 16px; font-size: 13px;">The first ${rows.length} of ${total.toLocaleString()} rows. Search any item or location in <a href="#/view/inventory">Inventory → Stock on Hand</a>.</p>` : '');
  sweepLinkNames(panel);
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
          <td style="font-family: monospace;">${escapeHTMLText(r.cart_number)}</td>
          <td>${escapeHTMLText(r.location)}</td>
          <td>${escapeHTMLText(r.payment_mode)}</td>
          <td><span class="badge badge-success">${escapeHTMLText(r.status)}</span></td>
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
          <td>${escapeHTMLText(r.vendor || '')}</td>
          <td style="font-family: monospace;">${escapeHTMLText(r.po_number || r.id)}</td>
          <td>${(r.total_amount ?? 0).toLocaleString()}</td>
          <td>${escapeHTMLText(r.status)}</td>
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
      Goods received and not yet paid (GRN Suspense 2100), by age from the bill's due date or else the receipt date. Open purchase orders are in the Report Catalog as Open Purchase Commitments.
    </p>
    <table>
      <thead><tr><th>Age Bucket</th><th>Open Items</th><th>Outstanding Amount</th></tr></thead>
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
      resultEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Failed to load GST return summary.'))}</p>`;
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

  // Stage 55.20: each report's business question is its option's tooltip and
  // is searchable, so "who owes us" finds Receivables Ageing.
  const optionFor = d => `<option value="${escapeHTMLText(d.id)}" ${d.id === reportCatalogSelectedId ? 'selected' : ''} title="${escapeHTMLText(d.question || '')}">${d.number ? `${escapeHTMLText(d.number)} · ` : ''}${escapeHTMLText(d.label)}${d.question ? ` - ${escapeHTMLText(d.question)}` : ''}</option>`;
  panel.innerHTML = `
    <div style="padding: 16px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; border-bottom: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0; min-width: 200px;">
        <label class="form-label" for="rc-report-search">Find a report</label>
        <input id="rc-report-search" class="form-input" type="search" placeholder="e.g. who owes us, stock value" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0; min-width: 260px;">
        <label class="form-label" for="rc-report-select">Report</label>
        <select id="rc-report-select" class="form-select">
          ${Object.keys(byCategory).sort().map(cat => `
            <optgroup label="${escapeHTMLText(cat)}">
              ${byCategory[cat].map(optionFor).join('')}
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
    <div id="rc-run-summary" style="padding: 12px 16px 0; color: var(--text-muted); font-size: 13px;"></div>
    <div id="rc-control-totals" style="padding: 0 16px;"></div>
    <div id="rc-results" style="padding: 16px;"></div>
    <details id="rc-recent-runs" style="padding: 0 16px 8px;"><summary>Recent runs</summary><div id="rc-recent-runs-body"></div></details>
    <details id="rc-my-exports" style="padding: 0 16px 16px;"><summary>My exports</summary><div id="rc-my-exports-body"></div></details>
  `;
  document.getElementById('rc-report-search').addEventListener('input', (e) => {
    const q = e.target.value.trim().toLowerCase();
    const select = document.getElementById('rc-report-select');
    let first = null;
    select.querySelectorAll('option').forEach(o => {
      const d = reportCatalogDefs.find(x => x.id === o.value);
      const hay = d ? `${d.number || ''} ${d.label} ${d.question || ''} ${d.description || ''} ${d.category || ''}`.toLowerCase() : '';
      const show = !q || q.split(/\s+/).every(w => hay.includes(w));
      o.hidden = !show;
      if (show && !first) first = o;
    });
    select.querySelectorAll('optgroup').forEach(g => { g.hidden = ![...g.querySelectorAll('option')].some(o => !o.hidden); });
    if (first && select.selectedOptions[0]?.hidden) {
      select.value = first.value;
      select.dispatchEvent(new Event('change'));
    }
  });
  document.getElementById('rc-my-exports').addEventListener('toggle', (e) => { if (e.target.open) renderMyReportExports(); });
  document.getElementById('rc-recent-runs').addEventListener('toggle', (e) => { if (e.target.open) renderRecentReportRuns(); });

  document.getElementById('rc-report-select').addEventListener('change', (e) => {
    reportCatalogSelectedId = e.target.value;
    renderReportCatalogParams();
    loadReportCatalogSavedFilters();
    initReportCatalogColumns();
    loadReportColumnProfiles();
    document.getElementById('rc-columns-panel').classList.add('hidden');
    if (document.getElementById('rc-recent-runs').open) renderRecentReportRuns();
  });
  document.getElementById('rc-run-btn').addEventListener('click', runReportCatalogReport);
  // Enter in a filter runs the report - a barcode scanner types the code and
  // presses Enter, so a scan box works without reaching for the button.
  document.getElementById('rc-params').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.matches('[data-param-key]')) {
      e.preventDefault();
      runReportCatalogReport();
    }
  });
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
  if (reportAutoRunId && reportAutoRunId === reportCatalogSelectedId) {
    reportAutoRunId = '';
    runReportCatalogReport();
  }
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
  const canUniversal = role === 'Super Admin' || role === 'HR/Admin' || role === 'Admin' || role === 'Store Manager';
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
  // A "select" param is a real dropdown of its options (it used to render as a
  // free-text box, so the user had to type an option exactly). An optional one
  // leads with a blank "Any"; otherwise its first option is the default.
  const control = p => {
    if (p.type === 'select' && p.options) {
      const opts = p.options.split(',').map(o => o.trim()).filter(Boolean);
      const any = /\(optional\)/i.test(p.label) ? '<option value="">Any</option>' : '';
      return `<select id="rc-param-${p.key}" class="form-select" data-param-key="${p.key}" style="width: 180px;">${any}${opts.map(o =>
        `<option value="${escapeHTMLText(o)}">${escapeHTMLText(o)}</option>`).join('')}</select>`;
    }
    return `<input type="${p.type === 'date' ? 'date' : 'text'}" id="rc-param-${p.key}" class="form-input"
             data-param-key="${p.key}" ${p.required ? 'required' : ''} style="width: 160px;">`;
  };
  container.innerHTML = (def.params || []).map(p => `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="rc-param-${p.key}">${p.label}</label>
      ${control(p)}
    </div>
  `).join('');
  // Stage 55.20: the filters last run for this report come back (Back and
  // re-opening keep them); otherwise a required date starts on a sensible
  // default - this month to date, or today for an as-of date.
  const remembered = reportParamMemory[def.id] || {};
  const today = localISODate(new Date());
  const monthStart = today.slice(0, 8) + '01';
  (def.params || []).forEach(p => {
    const el = document.getElementById(`rc-param-${p.key}`);
    if (!el) return;
    if (remembered[p.key] !== undefined) { el.value = remembered[p.key]; return; }
    if (p.type !== 'date' || !p.required) return;
    if (/^(start|from|period_from)$/.test(p.key)) el.value = monthStart;
    else el.value = today;
  });
}

// Stage 55.20: filters per report for this session, so drill-through and Back
// return to the same scoped result.
const reportParamMemory = {};
// reportAutoRunId (73.B.7) is a report another screen opened with its
// filters filled in (reportParamMemory), to run as soon as it shows.
let reportAutoRunId = '';

// One line saying exactly what was run: report, filters, row count, when, in
// what currency and time zone.
function renderReportRunSummary(def, params, result) {
  const el = document.getElementById('rc-run-summary');
  if (!el) return;
  const labelOf = k => ((def.params || []).find(p => p.key === k) || {}).label || k;
  const filters = Object.entries(params).map(([k, v]) => `${labelOf(k)}: ${v}`).join(' · ') || 'no filters';
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  el.textContent = `${def.number ? `${def.number} ` : ''}${def.label} - ${filters} - ${(result.rows || []).length} row(s) - run ${new Date().toLocaleString('en-IN')} (${tz}) - amounts in INR`;
}

// Stage 73.2.19: detail = summary = ledger, with its cut-off.
function renderReportControlTotals(ct) {
  const el = document.getElementById('rc-control-totals');
  if (!el) return;
  if (!ct) { el.innerHTML = ''; return; }
  const money = v => (v === null || v === undefined) ? '-' : Number(v).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const amounts = ct.amounts_hidden ? '<span>Amounts restricted for your role.</span>' : `
      <span>Detail (rows) <b>${money(ct.detail_total)}</b></span>
      <span>Summary <b>${money(ct.summary_total)}</b></span>
      <span>Ledger${ct.ledger_accounts && ct.ledger_accounts.length ? ` (${escapeHTMLText(ct.ledger_accounts.join(', '))})` : ''} <b>${ct.ledger_total === null || ct.ledger_total === undefined ? 'no ledger account' : money(ct.ledger_total)}</b></span>
      ${ct.detail_vs_summary ? `<span>Detail - summary <b>${money(ct.detail_vs_summary)}</b></span>` : ''}
      ${ct.summary_vs_ledger ? `<span>Summary - ledger <b>${money(ct.summary_vs_ledger)}</b></span>` : ''}`;
  el.innerHTML = `
    <div class="table-panel" style="padding:12px 16px;margin-top:12px;border-left:4px solid ${ct.tied ? 'var(--success-color, #10b981)' : 'var(--warning-color, #f59e0b)'}">
      <div style="display:flex;gap:18px;flex-wrap:wrap;align-items:center">
        <span class="badge ${ct.tied ? 'badge-success' : 'badge-warning'}">${ct.tied ? 'Control totals tie' : 'Control totals do not tie'}</span>
        ${amounts}
      </div>
      <div style="font-size:12px;color:var(--text-muted);margin-top:6px">${escapeHTMLText(ct.basis || '')}${ct.basis ? ' · ' : ''}${escapeHTMLText(ct.cut_off || '')}</div>
      ${(ct.notes || []).map(n => `<div style="font-size:12px;margin-top:4px">${escapeHTMLText(n)}</div>`).join('')}
    </div>`;
}

// Stage 55.20: the requester's own background exports - status, error and
// the download, private to them.
async function renderMyReportExports() {
  const body = document.getElementById('rc-my-exports-body');
  if (!body) return;
  const res = await apiFetch('/api/v1/reports/exports');
  if (!res) return;
  if (!res.ok) { body.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Could not load your exports.'))}</p>`; return; }
  const { jobs } = await res.json();
  body.innerHTML = jobs.length === 0 ? '<p style="color:var(--text-muted)">You have not exported a report yet. Use "Export in Background" above.</p>' : `
    <table><thead><tr><th>Report</th><th>Filters</th><th>Requested</th><th>Status</th><th></th></tr></thead><tbody>
    ${jobs.map(j => `<tr><td>${escapeHTMLText(j.report_label || j.report_id)}</td>
      <td><small>${escapeHTMLText(Object.entries(j.params || {}).map(([k, v]) => `${k}=${v}`).join(', ') || '-')}</small></td>
      <td>${escapeHTMLText(reportCellText(j.created_at))}</td>
      <td><span class="badge ${j.status === 'Completed' ? 'badge-success' : j.status === 'Failed' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(j.status)}</span>${j.code ? ` <small>${escapeHTMLText(j.code)}</small>` : ''}</td>
      <td>${j.status === 'Completed' ? `<button class="action-btn" data-export-download="${escapeHTMLText(j.id)}" data-report-id="${escapeHTMLText(j.report_id || '')}">Download CSV</button>` : ''}
          ${j.status === 'Pending' ? `<button class="action-btn" data-export-cancel="${escapeHTMLText(j.id)}">Cancel</button>` : ''}</td></tr>`).join('')}
    </tbody></table>`;
  body.querySelectorAll('[data-export-download]').forEach(b => b.addEventListener('click', () => downloadReportExportCSV(b.dataset.exportDownload, b.dataset.reportId)));
  body.querySelectorAll('[data-export-cancel]').forEach(b => b.addEventListener('click', async () => {
    const r = await apiFetch(`/api/v1/reports/export/${encodeURIComponent(b.dataset.exportCancel)}/cancel`, { method: 'POST', body: '{}' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The export could not be cancelled.'); return; }
    renderMyReportExports();
  }));
}

// Stage 55.20: the selected report's recent runs - your own, or everyone's
// (the scheduler's too) for a Super Admin.
function formatRunDuration(ms) {
  const n = Number(ms) || 0;
  return n < 1000 ? `${n} ms` : `${(n / 1000).toFixed(1)} s`;
}

async function renderRecentReportRuns() {
  const body = document.getElementById('rc-recent-runs-body');
  const def = currentReportCatalogDef();
  if (!body || !def) return;
  body.innerHTML = '<p style="color:var(--text-muted)">Loading...</p>';
  const res = await apiFetch(`/api/v1/reports/runs/${encodeURIComponent(def.id)}`);
  if (!res) return;
  if (!res.ok) { body.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Could not load recent runs.'))}</p>`; return; }
  const { runs } = await res.json();
  if (def.id !== reportCatalogSelectedId) return; // the selection moved on while this loaded
  body.innerHTML = runs.length === 0 ? `<p style="color:var(--text-muted)">No runs of ${escapeHTMLText(def.label)} yet. Press Run above.</p>` : `
    <table><thead><tr><th>Run by</th><th>When</th><th>Took</th><th>Rows</th></tr></thead><tbody>
    ${runs.map(r => `<tr><td>${escapeHTMLText(r.run_by || 'Schedule')}</td><td>${escapeHTMLText(reportCellText(r.run_at))}</td>
      <td>${escapeHTMLText(formatRunDuration(r.duration_ms))}</td><td>${escapeHTMLText(String(r.row_count))}</td></tr>`).join('')}
    </tbody></table>`;
}

// Stage 55.20: scheduled reports and dashboard digests - yours, or everyone's
// for a Super Admin. "Next run" is the scheduler's own answer and recipients
// are masked; schedules are created in Setup (Scheduled Report / Dashboard
// Digest), so this list links there instead of carrying a form of its own.
function scheduleDateText(ymd) {
  const d = /^\d{4}-\d{2}-\d{2}$/.test(ymd || '') ? new Date(`${ymd}T00:00:00`) : null;
  return d && !isNaN(d) ? d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }) : (ymd || '');
}

function scheduleNextRunHTML(s) {
  if (s.status !== 'Active') return '<span style="color:var(--text-muted)">Paused</span>';
  if (!s.next_run) return '<span style="color:var(--text-muted)">Not set</span>';
  if (s.due) return `Due now<br><small style="color:var(--text-muted)">goes out at the next hourly check, then ${escapeHTMLText(scheduleDateText(s.following_run))}</small>`;
  return `<span style="white-space:nowrap">${escapeHTMLText(scheduleDateText(s.next_run))}</span>`;
}

function scheduleResultHTML(s) {
  if (s.last_result === 'ok') return `<span class="badge badge-success">Delivered</span>${s.last_rows !== undefined ? `<br><small>${escapeHTMLText(String(s.last_rows))} row${s.last_rows === 1 ? '' : 's'}</small>` : ''}`;
  if (s.last_result === 'running') return '<span class="badge badge-secondary">Running</span>';
  if (s.last_result === 'error') return `<span class="badge badge-danger">Failed</span><br><small>${escapeHTMLText(s.last_error || '')}</small>`;
  return '<span style="color:var(--text-muted)">Not run yet</span>';
}

function scheduleRecipientsHTML(s) {
  const parts = [];
  if (s.recipient_count > 0) parts.push(`${s.recipient_count} email${s.recipient_count === 1 ? '' : 's'}<br><small>${escapeHTMLText(s.recipients.join(', '))}</small>`);
  if (s.webhook_host) parts.push(`<small>Webhook: ${escapeHTMLText(s.webhook_host)}</small>`);
  return parts.length ? parts.join('<br>') : '<span style="color:var(--text-muted)">None set</span>';
}

async function renderReportSchedulesPanel(panel) {
  if (!panel) return;
  panel.id = 'rs-panel';
  const res = await apiFetch('/api/v1/reports/schedules');
  if (!res) return;
  if (!res.ok) { panel.innerHTML = `<p class="login-error" style="padding:16px;">${escapeHTMLText(await getErrorMessage(res, 'Could not load schedules.'))}</p>`; return; }
  const { schedules } = await res.json();
  const setupLinks = [['ScheduledReport', 'Set up a scheduled report'], ['DashboardDigest', 'Set up a dashboard digest']]
    .filter(([doctype]) => typeof canCreateDoctype === 'function' && canCreateDoctype(doctype))
    .map(([doctype, label]) => `<button class="btn btn-outline btn-sm" data-sched-setup="${doctype}">${label}</button>`).join('');
  const empty = setupLinks
    ? 'No scheduled reports or digests yet. Set one up in Setup > Reports: a Scheduled Report sends one report, a Dashboard Digest sends every tile of a saved dashboard layout - pick the frequency, the first run date and the recipient email.'
    : 'No scheduled reports or digests yet. Ask a Store Manager or Super Admin to set one up for you.';
  panel.innerHTML = `
    <div style="padding: 16px; display: flex; gap: 8px; flex-wrap: wrap; align-items: center; border-bottom: 1px solid var(--border-color);">
      ${setupLinks}<button class="btn btn-outline btn-sm" data-sched-refresh="1">Refresh</button>
    </div>
    <div style="padding: 16px; overflow-x: auto;">
    ${schedules.length === 0 ? `<p style="color:var(--text-muted)">${empty}</p>` : `
      <table><thead><tr><th>Schedule</th><th>Owner</th><th>Frequency</th><th>Next run</th><th>Last run</th><th>Result</th><th>Recipients</th><th>Status</th><th></th></tr></thead><tbody>
      ${schedules.map(s => `<tr>
        <td><b>${escapeHTMLText(s.target_label || s.target || '-')}</b><br><small>${s.kind === 'digest' ? 'Dashboard digest' : 'Report'}${s.run_as_role ? ` · runs as ${escapeHTMLText(s.run_as_role)}` : ''}</small></td>
        <td>${escapeHTMLText(s.owner)}</td>
        <td>${escapeHTMLText(s.frequency)}</td>
        <td>${scheduleNextRunHTML(s)}</td>
        <td>${s.last_run_at ? escapeHTMLText(reportCellText(s.last_run_at)) : '<span style="color:var(--text-muted)">Never</span>'}</td>
        <td>${scheduleResultHTML(s)}</td>
        <td>${scheduleRecipientsHTML(s)}</td>
        <td><span class="badge ${s.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(s.status)}</span></td>
        <td style="white-space:nowrap">
          <button class="action-btn" data-sched-action="${s.status === 'Active' ? 'pause' : 'resume'}" data-sched-kind="${escapeHTMLText(s.kind)}" data-sched-id="${escapeHTMLText(s.id)}">${s.status === 'Active' ? 'Pause' : 'Resume'}</button>
          <button class="action-btn" data-sched-action="run" data-sched-kind="${escapeHTMLText(s.kind)}" data-sched-id="${escapeHTMLText(s.id)}" data-sched-label="${escapeHTMLText(s.target_label || s.target || '')}">Run now</button>
        </td></tr>`).join('')}
      </tbody></table>`}
    </div>`;
  panel.querySelector('[data-sched-refresh]').addEventListener('click', () => renderReportSchedulesPanel(panel));
  panel.querySelectorAll('[data-sched-setup]').forEach(b => b.addEventListener('click', () => { window.location.hash = `#/setup/${b.dataset.schedSetup}`; }));
  panel.querySelectorAll('[data-sched-action]').forEach(btn => btn.addEventListener('click', async () => {
    const action = btn.dataset.schedAction;
    if (action === 'run' && !(await showCustomConfirm(`Run "${btn.dataset.schedLabel}" now and send it to its recipients?`, 'Run now'))) return;
    btn.disabled = true;
    const r = await apiFetch(`/api/v1/reports/schedules/${encodeURIComponent(btn.dataset.schedKind)}/${encodeURIComponent(btn.dataset.schedId)}/${action}`, { method: 'POST', body: '{}' });
    if (!r) { btn.disabled = false; return; }
    if (!r.ok) { btn.disabled = false; await showApiError(r, 'The schedule could not be changed.'); return; }
    showToast(action === 'run' ? 'Running now - the result shows here when it finishes.' : action === 'pause' ? 'Schedule paused.' : 'Schedule resumed.', { variant: 'success' });
    await renderReportSchedulesPanel(panel);
    if (action === 'run') setTimeout(() => { if (panel.isConnected) renderReportSchedulesPanel(panel); }, 4000);
  }));
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
  reportParamMemory[def.id] = { ...params };
  resultsEl.innerHTML = '<p style="color:var(--text-muted)">Running...</p>';
  renderReportControlTotals(null);
  const res = await apiFetch(`/api/v1/reports/run/${def.id}?${reportCatalogQueryString(params)}`);
  if (!res) return;
  if (!res.ok) {
    resultsEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Failed to run report.'))}</p>`;
    return;
  }
  const result = await res.json();
  renderReportRunSummary(def, params, result);
  renderReportControlTotals(result.control_totals);
  renderReportCatalogResultTable(resultsEl, result, params);
  if (document.getElementById('rc-recent-runs')?.open) renderRecentReportRuns();
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
      // Stage 75.3.4: an image column carries a media id, drawn as a thumbnail below.
      // Stage 75.3: a column titled "... Code" / "... ID" asked for the code, so
      // the screen-wide code -> name swap (sweepLinkNames) leaves it alone.
      const keepCode = /\b(code|id)$/i.test(c.label || '') ? ' data-link-swept="1"' : '';
      html += c.type === 'image'
        ? `<td>${row[c.key] ? `<img class="rc-thumb" data-rc-media="${escapeHTMLText(row[c.key])}" alt="" width="40" height="40" style="object-fit:cover;border-radius:4px;">` : ''}</td>`
        : `<td${keepCode}>${escapeHTMLText(reportCellText(row[c.key]))}</td>`;
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
  html += `</tbody>`;
  // Stage 75.3.4: a column marked total is summed under the table.
  if (rows.length && columns.some(c => c.total)) {
    html += `<tfoot><tr>${columns.map((c, i) => {
      if (c.total) return `<td><b>${escapeHTMLText(reportCellText(Math.round(rows.reduce((s, r) => s + (Number(r[c.key]) || 0), 0) * 100) / 100))}</b></td>`;
      return i === 0 ? '<td><b>Total</b></td>' : '<td></td>';
    }).join('')}${extraCols ? `<td colspan="${extraCols}"></td>` : ''}</tr></tfoot>`;
  }
  html += `</table>`;
  container.innerHTML = html;
  loadReportThumbnails(container);
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
  // Stage 55.20: says which row and filters the lines belong to, with
  // readable headings and dates.
  const scope = Object.entries(params).map(([k, v]) => `${k} ${v}`).join(', ');
  let inner = `<div style="padding:8px 0;"><div style="font-size:12px;color:var(--text-muted);margin-bottom:6px">${drillRows.length} line(s) behind "${escapeHTMLText(rowKey)}"${scope ? ` - same filters (${escapeHTMLText(scope)})` : ''}</div><table style="width:100%;"><thead><tr>${keys.map(k => `<th>${escapeHTMLText(k.replace(/_/g, ' '))}</th>`).join('')}</tr></thead><tbody>`;
  inner += drillRows.length === 0
    ? `<tr><td colspan="${keys.length || 1}" style="text-align:center; color:var(--text-muted);">No underlying rows for this figure &mdash; it is a computed total with no individual transactions behind it in the selected period.</td></tr>`
    : drillRows.map(r => `<tr>${keys.map(k => `<td>${escapeHTMLText(reportCellText(r[k] === null || r[k] === undefined ? '' : r[k]))}</td>`).join('')}</tr>`).join('');
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
    statusEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Failed to queue export.'))}</p>`;
    return;
  }
  const job = await res.json();
  statusEl.innerHTML = `<p>Export queued (job ${escapeHTMLText(job.id)})... waiting for it to complete. <button class="action-btn" id="rc-export-cancel-btn">Cancel</button></p>`;
  document.getElementById('rc-export-cancel-btn')?.addEventListener('click', async () => {
    const r = await apiFetch(`/api/v1/reports/export/${encodeURIComponent(job.id)}/cancel`, { method: 'POST', body: '{}' });
    if (r && !r.ok) await showApiError(r, 'The export could not be cancelled.');
  });
  pollReportExportJob(job.id, statusEl);
}

async function pollReportExportJob(jobId, statusEl) {
  const res = await apiFetch(`/api/v1/reports/export/${jobId}`);
  if (!res) return;
  if (!res.ok) {
    statusEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Export job lookup failed.'))}</p>`;
    return;
  }
  const job = await res.json();
  if (job.status === 'Pending') {
    setTimeout(() => pollReportExportJob(jobId, statusEl), 2000);
    return;
  }
  if (job.status === 'Failed') {
    statusEl.innerHTML = `<p class="login-error">Export failed${job.message ? `: ${escapeHTMLText(job.message)}` : '.'} Narrow the filters and export again.</p>`;
    return;
  }
  if (job.status === 'Cancelled') {
    statusEl.innerHTML = `<p>Export cancelled.</p>`;
    return;
  }
  statusEl.innerHTML = `<p><button class="action-btn" id="rc-download-btn">Download CSV</button></p>`;
  document.getElementById('rc-download-btn').addEventListener('click', () => downloadReportExportCSV(jobId, reportCatalogSelectedId));
}

// This endpoint requires the same Bearer-token auth as every other API call
// (apiMiddleware has no query-string-token fallback), so a plain <a href>
// opened in a new tab can't authenticate itself - fetch the CSV through the
// normal authenticated apiFetch() and hand the browser a Blob URL instead.
async function downloadReportExportCSV(jobId, reportId = '') {
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
  // Stage 139.2: R1001-Sales-Register-2026-10-26.csv, not the job's id.
  const def = reportCatalogDefs.find(d => d.id === reportId);
  a.download = def ? `${[def.number, def.label].filter(Boolean).join('-').replace(/[^A-Za-z0-9]+/g, '-')}-${new Date().toISOString().slice(0, 10)}.csv` : `${jobId}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// RFQ / Vendor Quotes moved to its own lazy module, view-rfq.js (Stage 73.2.1-73.2.3).

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


export { renderReportsView, fillReportsMenu, fetchReportRows, execDashboardOpenReport, defaultDashboardTiles, loadDashboardLayouts, renderExecDashboard, renderExecDashboardBody, renderExecDashboardTrendChart, renderInventoryView, renderCurrentStockReport, renderSalesRegisterReport, renderVendorLedgerReport, renderPayablesAgeingReport, renderReceivablesAgeingReport, renderGSTReturnSummaryReport, renderReportCatalogPanel, currentReportCatalogDef, initReportCatalogColumns, effectiveReportColumns, toggleReportColumnsPanel, renderReportColumnsPanel, moveReportColumn, applyReportColumnState, loadReportColumnProfiles, applyReportColumnProfile, saveReportColumnProfile, renderReportCatalogParams, collectReportCatalogParams, reportCatalogQueryString, runReportCatalogReport, renderReportCatalogResultTable, runReportCatalogDrillDown, loadReportCatalogSavedFilters, applyReportCatalogSavedFilter, saveReportCatalogFilter, exportReportCatalogReport, pollReportExportJob, downloadReportExportCSV };

// loadReportThumbnails (Stage 75.3.4) fetches an image column's thumbnails with
// the session (an <img src> cannot send it), the first 200 only; one that
// cannot be read (no PIM, no image) just stays blank.
async function loadReportThumbnails(container) {
  const imgs = [...container.querySelectorAll('img[data-rc-media]')].slice(0, 200);
  const headers = { Authorization: 'Bearer ' + (localStorage.getItem('erp_token') || ''), 'X-Tenant-ID': localStorage.getItem('erp_tenant_id') || 'default' };
  for (const img of imgs) {
    try {
      const res = await fetch(`/api/v1/pim/media/${encodeURIComponent(img.dataset.rcMedia)}/thumbnail`, { headers });
      if (res.ok) img.src = URL.createObjectURL(await res.blob());
    } catch (e) { /* stays blank */ }
  }
}
