// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentReportTab = 'exec-dashboard';

const REPORT_TABS = [
  { id: 'exec-dashboard', label: 'Dashboard' },
  { id: 'dashboards', label: 'Custom dashboards' }, // Stage 100 (view-dashboards.js)
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
const REPORT_TAB_AREAS = { 'payables-ageing': 'Finance', 'receivables-ageing': 'Finance', 'gst-return-summary': 'Finance' };
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
    // 162.5: the shell paints first; the layouts, catalog and tile reads start after it, off the critical path.
    // 234.1: a return visit draws the last dashboard at once and refreshes it behind.
    if (execDashboardLast) drawExecDashboardBody(panel, execDashboardLast.tiles, execDashboardLast.results);
    else panel.innerHTML = '<p class="s-de2e81">Loading dashboard&hellip;</p>';
    requestAnimationFrame(() => setTimeout(async () => {
      if (!panel.isConnected) return;
      try { await renderExecDashboard(panel); }
      catch (e) { if (panel.isConnected && !panel._sig) panel.innerHTML = '<p class="s-de2e81">The dashboard could not load. Refresh to try again.</p>'; }
    }, 0));
  } else if (currentReportTab === 'dashboards') {
    const open = sessionStorage.getItem('erp_open_dashboard') || '';
    sessionStorage.removeItem('erp_open_dashboard');
    await (await import('./view-dashboards.js' + new URL(import.meta.url).search)).renderDashboardsTab(panel, open);
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
  return { rows: body.rows || [], masked: body.code === 'REPORT-0287', contract: body.contract || null };
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
  if (!panel.firstChild) panel.innerHTML = `<p class="s-de2e81">Loading dashboard&hellip;</p>`;
  await loadDashboardLayouts();
  if (dashboardCurrentTiles === null) {
    dashboardCurrentTiles = defaultDashboardTiles();
  }
  await renderExecDashboardBody(panel);
}

// 234.1: the last dashboard read, drawn at once when Reports opens again.
let execDashboardLast = null;
let kpiContractSummary = null;

async function renderExecDashboardBody(panel) {
  const tiles = dashboardCurrentTiles || defaultDashboardTiles();
  const results = await Promise.all(tiles.map(t => fetchReportRows(t.report_id, {})));
  if (!kpiContractSummary) kpiContractSummary = (await import('./view-dashboards.js' + new URL(import.meta.url).search)).kpiContractSummary;
  execDashboardLast = { tiles, results };
  drawExecDashboardBody(panel, tiles, results);
}

function drawExecDashboardBody(panel, tiles, results) {
  const username = localStorage.getItem('erp_username') || '';

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
      <div class="s-7fdfed">
        <h3 class="s-cb8b0d">${trendLabel}</h3>
        <div id="exec-dashboard-trend" data-days='${JSON.stringify(days)}' data-values='${JSON.stringify(trendValues)}'></div>
      </div>`;
  }

  const layoutOptions = dashboardLayouts.map(l =>
    `<option value="${escapeHTMLText(l.id)}" ${l.id === dashboardSelectedLayoutId ? 'selected' : ''}>${escapeHTMLText(l.name)}${l.owner !== username ? ' (shared)' : ''}</option>`
  ).join('');
  const addTileOptions = dashboardReportCatalog.map(d =>
    `<option value="${escapeHTMLText(d.id)}">${escapeHTMLText(d.label)}</option>`
  ).join('');

  // 234.1: the refresh behind a drawn dashboard redraws only when something
  // shown changed; a card's hover text (its contract) is updated in place.
  const cardTitle = (i) => results[i].contract ? `Click to open in Report Catalog · ${kpiContractSummary(results[i].contract)}` : 'Click to open in Report Catalog';
  const sig = JSON.stringify([layoutOptions, addTileOptions, trendHtml, tiles, results.map(r => [r.rows.length, r.masked])]);
  if (panel._sig === sig) {
    panel.querySelectorAll('#exec-dashboard-cards > .stat-card').forEach((card, i) => { if (results[i]) card.title = cardTitle(i); });
    return;
  }
  panel._sig = sig;
  panel.innerHTML = `
    <div class="s-b7cc9b">
      <select id="dashboard-layout-picker" aria-label="Dashboard layout" class="form-input s-30e741">
        <option value="">Default</option>
        ${layoutOptions}
      </select>
      <button id="dashboard-save-btn" class="btn btn-outline btn-sm">Save as&hellip;</button>
      <button id="dashboard-delete-btn" class="btn btn-outline btn-sm" ${dashboardSelectedLayoutId ? '' : 'disabled'}>Delete Layout</button>
      <select id="dashboard-add-tile-picker" aria-label="Add a tile" class="form-input s-30e741">
        <option value="">+ Add tile&hellip;</option>
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
    card.title = cardTitle(i);
    card.innerHTML = `
      <span class="stat-label">${escapeHTMLText(t.title || t.report_id)}</span>
      <span class="stat-val ${value > 0 ? 'u-red' : 'u-green'}">${value}</span>
      <span data-remove-tile="${i}" title="Remove tile" class="s-596941">&times;</span>
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
  // 234.1: the catalog's ~460 reports go into the picker on its first use. Drawn with
  // the screen, every visit restyled them and measured each name to size the box
  // (~140 ms of the switch at 4x CPU); styles.css gives the box its width instead.
  const tilePicker = document.getElementById('dashboard-add-tile-picker');
  const fillTilePicker = () => { if (tilePicker.options.length === 1) tilePicker.insertAdjacentHTML('beforeend', addTileOptions); };
  ['pointerdown', 'mousedown', 'focus', 'keydown'].forEach(t => tilePicker.addEventListener(t, fillTilePicker));
  tilePicker.addEventListener('change', (e) => {
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
  container.innerHTML = `<svg viewBox="0 0 ${width} ${height}" data-css="width:100%; max-width:${width}px; height:${height}px;">${bars}</svg>`;
}

// Inventory (Stage 21 QA fix): "Inventory" routed to a view name the router
// had no case for, always falling through to the "Module Setup Pending"
// mock screen - despite user-guide.md §5 explicitly documenting a working
// search-by-item stock screen.
// Stage 145: the screen reads /api/v1/inventory/stock now (stockOnHandHTML).
let inventorySearchQuery = '';
// Stage 145.3 / 145.4 (user, 2026-10-31): Stock on Hand by design - Design
// No, Combination ID, Barcode, the short Item name ("Antique Necklace"), each
// attribute a column - at the top-bar store, with the columns each user adds
// and removes. The choice is the user's own UserShortcut (Stage 139's shape),
// so it follows them to any computer.
const STOCK_COLUMNS_TARGET = 'inventory:stock';
const STOCK_ITEM_COLUMNS = [
  { key: 'design_no', label: 'Design No' },
  { key: 'sku', label: 'Combination ID' },
  { key: 'barcode', label: 'Barcode' },
  { key: 'item_name', label: 'Item' },
  { key: 'full_name', label: 'Full name', off: true },
  { key: 'category', label: 'Category', off: true }
];
const STOCK_QTY_COLUMNS = [
  { key: 'location_code', label: 'Store' },
  { key: 'on_hand', label: 'On hand', num: true, title: 'Book stock before reservations and holds' },
  { key: 'available_to_sell', label: 'Available to sell', num: true, title: 'Matches the reservation admission calculation' },
  { key: 'reserved', label: 'Reserved', num: true },
  { key: 'hold_qty', label: 'Held', num: true },
  { key: 'breakdown', label: 'Why unavailable' }
];

async function loadStockColumnPrefs() {
  const user = localStorage.getItem('erp_username') || '';
  const res = await apiFetch(`/api/v1/doc/UserShortcut?owner=${encodeURIComponent(user)}&kind=List%20view&target=${encodeURIComponent(STOCK_COLUMNS_TARGET)}&limit=1`).catch(() => null);
  const list = res && res.ok ? await res.json().catch(() => []) : [];
  const rec = Array.isArray(list) && list[0];
  let cols = {};
  try { cols = (rec && JSON.parse(rec.params || '{}').cols) || {}; } catch (_) { /* unreadable: defaults */ }
  return { id: rec ? rec.id : '', cols };
}

async function saveStockColumnPrefs(prefs) {
  const body = { kind: 'List view', name: 'Stock on Hand columns', target: STOCK_COLUMNS_TARGET, params: JSON.stringify({ cols: prefs.cols }),
    owner: localStorage.getItem('erp_username') || '', status: 'Active' };
  const res = await apiFetch(prefs.id ? `/api/v1/doc/UserShortcut/${encodeURIComponent(prefs.id)}` : '/api/v1/doc/UserShortcut', { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not save your columns.'); return; }
  if (!prefs.id) prefs.id = (await res.json().catch(() => ({}))).id || '';
}

function stockOnHandHTML(rows, cols, showTasks, searched) {
  const quantity = value => Number.isFinite(Number(value)) ? formatNumber(value) : '—';
  const span = cols.length + (showTasks ? 1 : 0);
  // Every cell but Store opts out of the link sweep, which would turn a code
  // back into the item's long name.
  const cell = (c, r) => {
    if (c.attr) return `<td data-link-swept="1">${escapeHTMLText((r.attributes || {})[c.attr] || '')}</td>`;
    if (c.num) return `<td class="num" data-link-swept="1">${quantity(r[c.key])}</td>`;
    switch (c.key) {
      case 'design_no': case 'sku': case 'barcode':
        return `<td data-link-swept="1">${r[c.key] ? copyableCell(r[c.key], r[c.key]) : '—'}</td>`;
      case 'item_name':
        return `<td data-link-swept="1" title="${escapeHTMLText(r.full_name || '')}">${escapeHTMLText(r.item_name || r.sku || '')}</td>`;
      case 'location_code':
        return `<td data-link-doctype="Location" data-link-ref="${escapeHTMLText(r.location_code || '')}">${copyableCell(r.location_code, r.location_code)}</td>`;
      case 'breakdown':
        return `<td data-link-swept="1"><details><summary>Breakdown</summary><span>Base available: ${quantity(r.available)}; committed: ${quantity(r.committed)}; safety: ${quantity(r.safety_stock)}; blocked: ${quantity(r.blocked)}; QC hold: ${quantity(r.qc_hold)}; damaged: ${quantity(r.damaged)}; channel buffer: ${quantity(r.channel_buffer)}.</span></details></td>`;
    }
    return `<td data-link-swept="1">${escapeHTMLText(r[c.key] || '')}</td>`;
  };
  const tasks = r => {
    const attrs = `data-stock-sku="${escapeHTMLText(r.sku || '')}" data-stock-location="${escapeHTMLText(r.location_code || '')}"`;
    return `<td data-link-swept="1"><a href="#/view/transfers" data-stock-task="transfer" ${attrs}>Transfer</a> · <a href="#/view/cycle-count" data-stock-task="count" ${attrs}>Count</a> · <a href="#/view/place-hold" data-stock-task="hold" ${attrs}>Hold</a> · <a href="#/view/reports%3Astock-ledger" data-stock-task="movements" ${attrs} title="Every movement of this item here, with its source document">Movements</a></td>`;
  };
  return `<table>
    <thead><tr>${cols.map(c => `<th${c.num ? ' class="num"' : ''}${c.title ? ` title="${escapeHTMLText(c.title)}"` : ''}>${escapeHTMLText(c.label)}</th>`).join('')}${showTasks ? '<th>Next task</th>' : ''}</tr></thead>
    <tbody>${rows.length === 0
      ? `<tr><td colspan="${span}" class="text-center py-8 text-muted" data-link-swept="1">${searched ? 'Nothing matches this search.' : 'No stock here yet. Post a Goods Receipt to add stock.'}</td></tr>`
      : rows.map(r => `<tr>${cols.map(c => cell(c, r)).join('')}${showTasks ? tasks(r) : ''}</tr>`).join('')}</tbody></table>`;
}

async function renderInventoryView(container) {
  await loadViewStyles('inventory.css'); // Stage 145: the dashboard's and Stock on Hand's own styles
  // Stage 88.5: Inventory's Dashboard, Ledger and MRP Change are pages of their own (inventory:<page>).
  if (currentViewTab && await (await import('./view-inventory-erp.js' + new URL(import.meta.url).search)).renderInventoryPage(currentViewTab, container)) return;
  // 134.6 (47.10.3): one page at a time, searched on the server - the whole
  // stock table no longer comes to the browser.
  const pageSize = 100;
  let offset = 0;
  let data = { rows: [], total: 0, attributes: [], store: '' };
  const load = async () => {
    const res = await apiFetch(`/api/v1/inventory/stock?limit=${pageSize}&offset=${offset}&q=${encodeURIComponent(inventorySearchQuery)}`);
    if (!res || !res.ok) return res;
    data = await res.json();
    return res;
  };
  inventorySearchQuery = '';
  const [first, prefs] = await Promise.all([load(), loadStockColumnPrefs()]);
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
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        <input type="text" id="inventory-search-input" aria-label="Search stock" placeholder="Search design, barcode, item...">
      </div>
      <div class="stock-cols"><button type="button" class="btn btn-outline btn-sm" aria-haspopup="dialog" aria-expanded="false">Columns</button></div>
    </div>
    <div class="table-wrapper" id="inventory-table-wrapper"></div>
    <div class="table-controls s-4578ea" id="inventory-pager"></div>
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

  // The columns: the item's, one per attribute, then the quantities. Store
  // starts hidden when the top bar has picked one.
  const allColumns = () => [...STOCK_ITEM_COLUMNS,
    ...(data.attributes || []).map(a => ({ key: `attr:${a.code}`, label: a.label || a.code, attr: a.code })),
    ...STOCK_QTY_COLUMNS];
  const visible = c => (prefs.cols[c.key] !== undefined ? !!prefs.cols[c.key] : c.key === 'location_code' ? !data.store : !c.off);

  // Only the table body redraws on each keystroke - the search input itself
  // stays untouched so it doesn't lose focus/cursor position while typing
  // (matches renderDocTable()'s existing #doc-table-wrapper pattern).
  function draw() {
    const wrapper = document.getElementById('inventory-table-wrapper');
    if (!wrapper) return;
    const rows = data.rows || [], total = Number(data.total) || 0;
    wrapper.innerHTML = stockOnHandHTML(rows, allColumns().filter(visible), true, !!inventorySearchQuery);
    sweepLinkNames(wrapper);
    const pager = document.getElementById('inventory-pager');
    pager.innerHTML = total > rows.length || offset > 0
      ? `<span class="text-muted s-a54188">${formatNumber(offset + 1)}-${formatNumber(offset + rows.length)} of ${formatNumber(total)}</span>
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

  // Columns: tick to add, untick to remove; saved for this user a moment later.
  const colsWrap = panel.querySelector('.stock-cols');
  const colsBtn = colsWrap.querySelector('button');
  let saveTimer = null;
  const scheduleSave = () => { clearTimeout(saveTimer); saveTimer = setTimeout(() => saveStockColumnPrefs(prefs), 600); };
  const onOutside = e => { if (!colsWrap.contains(e.target)) closePop(); };
  const onKey = e => { if (e.key === 'Escape') { e.stopPropagation(); closePop(); colsBtn.focus(); } };
  function closePop() {
    colsWrap.querySelector('.stock-cols-pop')?.remove();
    colsBtn.setAttribute('aria-expanded', 'false');
    document.removeEventListener('mousedown', onOutside, true);
    document.removeEventListener('keydown', onKey, true);
  }
  function fillPop(pop) {
    pop.innerHTML = allColumns().map(c => `<label><input type="checkbox" data-col="${escapeHTMLText(c.key)}"${visible(c) ? ' checked' : ''}>${escapeHTMLText(c.label)}</label>`).join('')
      + '<div class="stock-cols-foot"><button type="button" class="btn btn-outline btn-sm" data-cols-reset>Reset</button></div>';
  }
  colsBtn.addEventListener('click', () => {
    if (colsWrap.querySelector('.stock-cols-pop')) { closePop(); return; }
    const pop = document.createElement('div');
    pop.className = 'stock-cols-pop';
    pop.setAttribute('role', 'dialog');
    pop.setAttribute('aria-label', 'Columns');
    fillPop(pop);
    pop.addEventListener('change', e => {
      const cb = e.target.closest('[data-col]');
      if (!cb) return;
      // At least one column stays.
      if (!cb.checked && allColumns().filter(visible).length <= 1) { cb.checked = true; return; }
      prefs.cols[cb.dataset.col] = cb.checked;
      draw();
      scheduleSave();
    });
    pop.addEventListener('click', e => {
      if (!e.target.closest('[data-cols-reset]')) return;
      prefs.cols = {};
      fillPop(pop);
      draw();
      scheduleSave();
    });
    colsWrap.appendChild(pop);
    colsBtn.setAttribute('aria-expanded', 'true');
    pop.querySelector('input')?.focus();
    document.addEventListener('mousedown', onOutside, true);
    document.addEventListener('keydown', onKey, true);
  });
}

async function renderPayablesAgeingReport(panel) {
  const res = await apiFetch('/api/v1/reports/payables-ageing');
  if (!res) return;
  const buckets = res.ok ? await res.json() : [];
  panel.innerHTML = `
    <table>
      <thead><tr><th>Age Bucket</th><th>Open Items</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${formatNumber(b.amount)}</td>
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
    <table>
      <thead><tr><th>Age Bucket</th><th>Invoice Count</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${formatNumber(b.amount)}</td>
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
    <div class="s-153627">
      <div class="form-group s-50435f">
        <label class="form-label" for="gst-start-date">From</label>
        <input type="date" id="gst-start-date" class="form-input" value="${monthStart}">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="gst-end-date">To</label>
        <input type="date" id="gst-end-date" class="form-input" value="${today}">
      </div>
      <button class="btn btn-primary" id="gst-summary-btn">Run</button>
      <button class="btn btn-outline" id="gst-files-btn">GSTR-1 / 2B files</button>
    </div>
    <div id="gst-summary-result" class="s-644e5e"></div>
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
        <div class="stat-card"><span class="stat-label">Exempt Value</span><span class="stat-val">${formatNumber(s.exempt_value)}</span></div>
        <div class="stat-card"><span class="stat-label">Nil-Rated Value</span><span class="stat-val">${formatNumber(s.nil_rated_value)}</span></div>
        <div class="stat-card"><span class="stat-label">Zero-Rated Value</span><span class="stat-val">${formatNumber(s.zero_rated_value)}</span></div>
        <div class="stat-card"><span class="stat-label">Total Non-Taxable</span><span class="stat-val">${formatNumber(nonTaxable)}</span></div>
      </div>
    `;
    resultEl.innerHTML = `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Taxable Value</span><span class="stat-val">${formatNumber(s.taxable_value)}</span></div>
        <div class="stat-card"><span class="stat-label">Output CGST</span><span class="stat-val">${formatNumber(s.output_cgst)}</span></div>
        <div class="stat-card"><span class="stat-label">Output SGST</span><span class="stat-val">${formatNumber(s.output_sgst)}</span></div>
        <div class="stat-card"><span class="stat-label">Output IGST</span><span class="stat-val">${formatNumber(s.output_igst)}</span></div>
        <div class="stat-card"><span class="stat-label">Total Tax Liability</span><span class="stat-val">${formatNumber(s.total_tax_liability)}</span></div>
        <div class="stat-card"><span class="stat-label">Input Tax Credit</span><span class="stat-val">${formatNumber(s.input_tax_credit)}</span></div>
        <div class="stat-card"><span class="stat-label">Net GST Payable</span><span class="stat-val">${formatNumber(s.net_tax_payable ?? s.total_tax_liability)}</span></div>
        <div class="stat-card"><span class="stat-label">Transactions</span><span class="stat-val">${s.transaction_count}</span></div>
      </div>
      ${nonTaxableRow}
    `;
  };
  document.getElementById('gst-summary-btn').addEventListener('click', runReport);
  document.getElementById('gst-files-btn').addEventListener('click', async () => { await loadViewModule('/view-gstr-files.js?v=1'); window.openGstrFiles?.(); });
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
    panel.innerHTML = `<p class="login-error s-287f77">Failed to load report catalog.</p>`;
    return;
  }
  reportCatalogDefs = await res.json();
  if (reportCatalogDefs.length > 0 && !reportCatalogDefs.some(d => d.id === reportCatalogSelectedId)) {
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
    <div class="s-c7c7bb">
      <div class="form-group s-7ee7ac">
        <label class="form-label" for="rc-report-search">Find a report</label>
        <input id="rc-report-search" class="form-input" type="search" placeholder="e.g. who owes us, stock value" autocomplete="off">
      </div>
      <div class="form-group s-686bb2">
        <label class="form-label" for="rc-report-select">Report</label>
        <select id="rc-report-select" class="form-select">
          ${Object.keys(byCategory).sort().map(cat => `
            <optgroup label="${escapeHTMLText(cat)}">
              ${byCategory[cat].map(optionFor).join('')}
            </optgroup>
          `).join('')}
        </select>
      </div>
      <div id="rc-params" class="s-608a43"></div>
      <div class="form-group s-e4412e">
        <label class="form-label" for="rc-saved-filter">Saved Filter</label>
        <select id="rc-saved-filter" class="form-select"><option value="">— none —</option></select>
      </div>
      <div class="form-group s-e4412e">
        <label class="form-label" for="rc-column-profile">Column Profile</label>
        <select id="rc-column-profile" class="form-select"><option value="">— Default columns —</option></select>
      </div>
      <button class="btn btn-primary" id="rc-run-btn">Run</button>
      <button class="btn btn-outline" id="rc-columns-btn">Columns</button>
      <button class="btn btn-outline" id="rc-save-filter-btn">Save Filter</button>
      <button class="btn btn-outline" id="rc-export-btn">Export in Background</button>
    </div>
    <div id="rc-columns-panel" class="hidden s-fdeaf5"></div>
    <div id="rc-export-status" class="s-4c7b78"></div>
    <div id="rc-run-summary" class="s-498446"></div>
    <div id="rc-control-totals" class="s-4c7b78"></div>
    <div id="rc-results" class="s-c74152"></div>
    <details id="rc-recent-runs" class="s-1c1b91"><summary>Recent runs</summary><div id="rc-recent-runs-body"></div></details>
    <details id="rc-my-exports" class="s-644e5e"><summary>My exports</summary><div id="rc-my-exports-body"></div></details>
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
// 157.4b: a report opens a page at a time; the server counts and totals it all.
const REPORT_PAGE_SIZE = 500;
let reportCatalogFetchSeq = 0;

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
  const rows = reportCatalogColumnState.map((c, i) => `
    <div class="s-8eb9c7">
      <input type="checkbox" data-col-key="${cfgEsc(c.key)}" ${c.visible ? 'checked' : ''}>
      <span class="s-97445a">${cfgEsc(c.label)}</span>
      <button class="btn btn-outline rc-col-up rc-sm" data-idx="${i}" ${i === 0 ? 'disabled' : ''}>&uarr;</button>
      <button class="btn btn-outline rc-col-down rc-sm" data-idx="${i}" ${i === reportCatalogColumnState.length - 1 ? 'disabled' : ''}>&darr;</button>
    </div>`).join('');
  panel.innerHTML = `
    <div class="s-d2d902">
      <div class="s-a618cb">Show, hide, and reorder columns</div>
      ${rows || '<p class="page-subtitle s-116966">This report has no columns.</p>'}
      <div class="s-132391">
        <button class="btn btn-primary rc-sm" id="rc-col-apply">Apply</button>
        <button class="btn btn-outline rc-sm" id="rc-col-save">Save as Profile&hellip;</button>
        <button class="btn btn-outline rc-sm" id="rc-col-reset">Reset to default</button>
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
      return `<select id="rc-param-${p.key}" class="form-select s-f9de92" data-param-key="${p.key}">${any}${opts.map(o =>
        `<option value="${escapeHTMLText(o)}">${escapeHTMLText(o)}</option>`).join('')}</select>`;
    }
    // 140.12.27: a segment is chosen from the list, a record by typeahead.
    if (p.type === 'segment') {
      return `<select id="rc-param-${p.key}" class="form-select s-30080b" data-param-key="${p.key}" data-param-segment><option value="">Choose a segment</option></select>`;
    }
    return `<input type="${p.type === 'date' ? 'date' : 'text'}" id="rc-param-${p.key}" class="form-input s-56cae7"
             data-param-key="${p.key}" ${p.required ? 'required' : ''} ${p.type === 'link' && p.options ? `data-param-link="${escapeHTMLText(p.options)}"` : ''}>`;
  };
  container.innerHTML = (def.params || []).map(p => `
    <div class="form-group s-50435f">
      <label class="form-label" for="rc-param-${p.key}">${p.label}</label>
      ${control(p)}
    </div>
  `).join('');
  container.querySelectorAll('[data-param-link]').forEach(el => attachLinkTypeahead(el, el.dataset.paramLink, { noSetupHint: true }));
  container.querySelectorAll('[data-param-segment]').forEach(async el => {
    const res = await apiFetch('/api/v1/crm/segments');
    const segs = res && res.ok ? ((await res.json()).segments || []) : [];
    segs.forEach(s => el.insertAdjacentHTML('beforeend', `<option value="${escapeHTMLText(s.id)}">${escapeHTMLText(s.name)}</option>`));
  });
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
  el.textContent = `${def.number ? `${def.number} ` : ''}${def.label} - ${filters} - ${result.paging ? result.paging.total : (result.rows || []).length} row(s) - run ${formatDateTime(new Date())} (${tz}) - amounts in INR`;
}

// Stage 73.2.19: detail = summary = ledger, with its cut-off.
function renderReportControlTotals(ct) {
  const el = document.getElementById('rc-control-totals');
  if (!el) return;
  if (!ct) { el.innerHTML = ''; return; }
  const money = v => (v === null || v === undefined) ? '-' : formatMoney(v);
  const amounts = ct.amounts_hidden ? '<span>Amounts restricted for your role.</span>' : `
      <span>Detail (rows) <b>${money(ct.detail_total)}</b></span>
      <span>Summary <b>${money(ct.summary_total)}</b></span>
      <span>Ledger${ct.ledger_accounts && ct.ledger_accounts.length ? ` (${escapeHTMLText(ct.ledger_accounts.join(', '))})` : ''} <b>${ct.ledger_total === null || ct.ledger_total === undefined ? 'no ledger account' : money(ct.ledger_total)}</b></span>
      ${ct.detail_vs_summary ? `<span>Detail - summary <b>${money(ct.detail_vs_summary)}</b></span>` : ''}
      ${ct.summary_vs_ledger ? `<span>Summary - ledger <b>${money(ct.summary_vs_ledger)}</b></span>` : ''}`;
  el.innerHTML = `
    <div class="table-panel" data-css="padding:12px 16px;margin-top:12px;border-left:4px solid ${ct.tied ? 'var(--success-color, #10b981)' : 'var(--warning-color, #f59e0b)'}">
      <div class="s-ccd221">
        <span class="badge ${ct.tied ? 'badge-success' : 'badge-warning'}">${ct.tied ? 'Control totals tie' : 'Control totals do not tie'}</span>
        ${amounts}
      </div>
      <div class="s-bda799">${escapeHTMLText(ct.basis || '')}${ct.basis ? ' · ' : ''}${escapeHTMLText(ct.cut_off || '')}</div>
      ${(ct.notes || []).map(n => `<div class="s-dde55e">${escapeHTMLText(n)}</div>`).join('')}
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
  body.innerHTML = jobs.length === 0 ? '<p class="s-5872de">You have not exported a report yet.</p>' : `
    <table><thead><tr><th>Report</th><th>Filters</th><th>Requested</th><th>Status</th><th></th></tr></thead><tbody>
    ${jobs.map(j => `<tr><td>${escapeHTMLText(j.report_label || j.report_id)}</td>
      <td><small>${escapeHTMLText(Object.entries(j.params || {}).map(([k, v]) => `${k}=${v}`).join(', ') || '-')}</small></td>
      <td>${escapeHTMLText(reportCellText(j.created_at))}</td>
      <td><span class="badge ${j.status === 'Completed' ? 'badge-success' : j.status === 'Failed' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(j.status)}</span>${j.code ? ` <small>${escapeHTMLText(j.code)}</small>` : ''}</td>
      <td>${j.status === 'Completed' ? `<button class="action-btn" data-export-download="${escapeHTMLText(j.id)}" data-report-id="${escapeHTMLText(j.report_id || '')}">Download CSV</button>${j.expires_at ? ` <small>until ${escapeHTMLText(reportCellText(j.expires_at))}</small>` : ''}` : ''}
          ${j.status === 'Pending' ? `<button class="action-btn" data-export-cancel="${escapeHTMLText(j.id)}">Cancel</button>` : ''}
          ${j.status === 'Completed' || j.status === 'Failed' ? `<button class="action-btn" data-export-cancel="${escapeHTMLText(j.id)}">Revoke</button>` : ''}</td></tr>`).join('')}
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
  body.innerHTML = '<p class="s-5872de">Loading...</p>';
  const res = await apiFetch(`/api/v1/reports/runs/${encodeURIComponent(def.id)}`);
  if (!res) return;
  if (!res.ok) { body.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Could not load recent runs.'))}</p>`; return; }
  const { runs } = await res.json();
  if (def.id !== reportCatalogSelectedId) return; // the selection moved on while this loaded
  body.innerHTML = runs.length === 0 ? `<p class="s-5872de">No runs of ${escapeHTMLText(def.label)} yet.</p>` : `
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
  return d && !isNaN(d) ? formatDateTime(d, true) : (ymd || '');
}

function scheduleNextRunHTML(s) {
  if (s.status !== 'Active') return '<span class="s-5872de">Paused</span>';
  if (!s.next_run) return '<span class="s-5872de">Not set</span>';
  if (s.due) return `Due now<br><small class="s-5872de">goes out at the next hourly check, then ${escapeHTMLText(scheduleDateText(s.following_run))}</small>`;
  return `<span class="s-86abda">${escapeHTMLText(scheduleDateText(s.next_run))}</span>`;
}

function scheduleResultHTML(s) {
  if (s.last_result === 'ok') return `<span class="badge badge-success">Delivered</span>${s.last_rows !== undefined ? `<br><small>${escapeHTMLText(String(s.last_rows))} row${s.last_rows === 1 ? '' : 's'}</small>` : ''}`;
  if (s.last_result === 'running') return '<span class="badge badge-secondary">Running</span>';
  if (s.last_result === 'error') return `<span class="badge badge-danger">Failed</span><br><small>${escapeHTMLText(s.last_error || '')}</small>`;
  return '<span class="s-5872de">Not run yet</span>';
}

function scheduleRecipientsHTML(s) {
  const parts = [];
  if (s.recipient_count > 0) parts.push(`${s.recipient_count} email${s.recipient_count === 1 ? '' : 's'}<br><small>${escapeHTMLText(s.recipients.join(', '))}</small>`);
  if (s.webhook_host) parts.push(`<small>Webhook: ${escapeHTMLText(s.webhook_host)}</small>`);
  return parts.length ? parts.join('<br>') : '<span class="s-5872de">None set</span>';
}

async function renderReportSchedulesPanel(panel) {
  if (!panel) return;
  panel.id = 'rs-panel';
  const res = await apiFetch('/api/v1/reports/schedules');
  if (!res) return;
  if (!res.ok) { panel.innerHTML = `<p class="login-error s-287f77">${escapeHTMLText(await getErrorMessage(res, 'Could not load schedules.'))}</p>`; return; }
  const { schedules } = await res.json();
  const setupLinks = [['ScheduledReport', 'Set up a scheduled report'], ['DashboardDigest', 'Set up a dashboard digest']]
    .filter(([doctype]) => typeof canCreateDoctype === 'function' && canCreateDoctype(doctype))
    .map(([doctype, label]) => `<button class="btn btn-outline btn-sm" data-sched-setup="${doctype}">${label}</button>`).join('');
  const empty = 'No scheduled reports or digests yet.';
  panel.innerHTML = `
    <div class="s-d3dab6">
      ${setupLinks}<button class="btn btn-outline btn-sm" data-sched-refresh="1">Refresh</button>
    </div>
    <div class="s-ccc455">
    ${schedules.length === 0 ? `<p class="s-5872de">${empty}</p>` : `
      <table><thead><tr><th>Schedule</th><th>Owner</th><th>Frequency</th><th>Next run</th><th>Last run</th><th>Result</th><th>Recipients</th><th>Status</th><th></th></tr></thead><tbody>
      ${schedules.map(s => `<tr>
        <td><b>${escapeHTMLText(s.target_label || s.target || '-')}</b><br><small>${s.kind === 'digest' ? 'Dashboard digest' : 'Report'}${s.run_as_role ? ` · runs as ${escapeHTMLText(s.run_as_role)}` : ''}</small></td>
        <td>${escapeHTMLText(s.owner)}</td>
        <td>${escapeHTMLText(s.frequency)}</td>
        <td>${scheduleNextRunHTML(s)}</td>
        <td>${s.last_run_at ? escapeHTMLText(reportCellText(s.last_run_at)) : '<span class="s-5872de">Never</span>'}</td>
        <td>${scheduleResultHTML(s)}</td>
        <td>${scheduleRecipientsHTML(s)}</td>
        <td><span class="badge ${s.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(s.status)}</span></td>
        <td class="s-86abda">
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
  resultsEl.innerHTML = '<p class="s-5872de">Running...</p>';
  renderReportControlTotals(null);
  const result = await fetchReportCatalogPage(def, params, 0, resultsEl);
  if (!result) return;
  renderReportRunSummary(def, params, result);
  renderReportControlTotals(result.control_totals);
  renderReportCatalogResultTable(resultsEl, result, params);
  if (document.getElementById('rc-recent-runs')?.open) renderRecentReportRuns();
}

// One page of a run. An answer is dropped if a newer run or page was asked for
// meanwhile, or if the user has moved to another report.
async function fetchReportCatalogPage(def, params, offset, resultsEl) {
  const seq = ++reportCatalogFetchSeq;
  const filters = reportCatalogQueryString(params);
  const res = await apiFetch(`/api/v1/reports/run/${def.id}?${filters ? `${filters}&` : ''}page_size=${REPORT_PAGE_SIZE}&page_offset=${offset}`);
  const stale = () => seq !== reportCatalogFetchSeq || currentReportCatalogDef()?.id !== def.id;
  if (!res || stale()) return null;
  if (!res.ok) {
    const message = await getErrorMessage(res, 'Failed to run report.');
    if (!stale()) resultsEl.innerHTML = `<p class="login-error">${escapeHTMLText(message)}</p>`;
    return null;
  }
  const result = await res.json();
  return stale() ? null : result;
}

// A page flip keeps the filters of the run and its summary and control totals,
// which already describe the whole result.
async function flipReportCatalogPage(offset) {
  const def = currentReportCatalogDef();
  const resultsEl = document.getElementById('rc-results');
  if (!def || !resultsEl) return;
  resultsEl.querySelectorAll('[data-rc-page]').forEach((b) => { b.disabled = true; });
  const params = reportCatalogLastParams;
  const result = await fetchReportCatalogPage(def, params, offset, resultsEl);
  if (!result) return;
  renderReportCatalogResultTable(resultsEl, result, params);
  resultsEl.scrollIntoView({ block: 'start' });
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
    if (!isNaN(d)) return formatDateTime(d);
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
  // Stage 47.13.5: the duplicate lists carry a Merge action on every row that
  // is not the kept record of its group (admin only; the server checks too).
  const mergeDoctype = result.id === 'duplicate-suppliers' ? 'Vendor' : result.id === 'duplicate-items' ? 'Item' : '';
  const canMerge = !!mergeDoctype && columns.some(c => c.key === 'proposal');
  // Stage 49.5.5: an order-to-cash mismatch is dismissed only with a note.
  const canDismiss = result.id === 'order-to-cash';
  const extraCols = (result.has_drill_down ? 1 : 0) + (canAssign ? 1 : 0) + (canMerge ? 1 : 0) + (canDismiss ? 1 : 0);
  let html = `<table><thead><tr>`;
  columns.forEach(c => { html += `<th>${escapeHTMLText(c.label)}</th>`; });
  if (result.has_drill_down) html += `<th>Details</th>`;
  if (canAssign) html += `<th>Task</th>`;
  if (canMerge) html += `<th>Merge</th>`;
  if (canDismiss) html += `<th>Exception</th>`;
  html += `</tr></thead><tbody>`;
  if (rows.length === 0) {
    html += `<tr><td colspan="${columns.length + extraCols}" class="s-306094">No rows matched.</td></tr>`;
  }
  rows.forEach((row, idx) => {
    html += `<tr>`;
    columns.forEach(c => {
      // Stage 75.3.4: an image column carries a media id, drawn as a thumbnail below.
      // Stage 75.3: a column titled "... Code" / "... ID" asked for the code, so
      // the screen-wide code -> name swap (sweepLinkNames) leaves it alone.
      // 230.4: so did a column titled "SKU" - an item is known by its SKU (147.2).
      const keepCode = /\b(code|id|sku)$/i.test(c.label || '') ? ' data-link-swept="1"' : '';
      // Stage 148.34: a doclink column opens the document named by <key>_doctype and <key>_id (or the value).
      html += c.type === 'doclink' && row[c.key + '_doctype']
        ? `<td${keepCode}><a class="doc-link" href="#/setup/${encodeURIComponent(row[c.key + '_doctype'])}?q=${encodeURIComponent(String(row[c.key + '_id'] || row[c.key]))}">${escapeHTMLText(reportCellText(row[c.key]))}</a></td>`
        : c.type === 'image'
        ? `<td>${row[c.key] ? `<img class="rc-thumb s-561c2c" data-rc-media="${escapeHTMLText(row[c.key])}" alt="" width="40" height="40">` : ''}</td>`
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
    if (canMerge) {
      const keepId = row.keep_id;
      html += row.proposal !== 'Keep' && keepId
        ? `<td><button class="action-btn" data-merge-dup="${escapeHTMLText(String(row.id))}" data-merge-keep="${escapeHTMLText(String(keepId))}" data-merge-doctype="${mergeDoctype}">Merge</button></td>`
        : `<td></td>`;
    }
    if (canDismiss) {
      html += row.exception ? `<td>${escapeHTMLText(row.exception)} <button class="action-btn" data-reopen-kind="${escapeHTMLText(String(row.kind))}" data-reopen-ref="${escapeHTMLText(String(row.ref))}">Reopen</button></td>` : `<td><button class="action-btn" data-dismiss-kind="${escapeHTMLText(String(row.kind))}" data-dismiss-ref="${escapeHTMLText(String(row.ref))}">Dismiss</button></td>`;
    }
    html += `</tr><tr id="rc-drilldown-${idx}" class="hidden"><td colspan="${columns.length + extraCols}"></td></tr>`;
  });
  html += `</tbody>`;
  // Stage 75.3.4: a column marked total is summed under the table.
  // 157.4b: the server's totals cover every page; without them, say it is this page.
  const paging = result.paging;
  const allTotals = paging && paging.totals;
  const manyPages = !!paging && (paging.total > rows.length || paging.offset > 0);
  if (rows.length && columns.some(c => c.total)) {
    html += `<tfoot><tr>${columns.map((c, i) => {
      if (c.total) {
        const sum = allTotals ? allTotals[c.key] : Math.round(rows.reduce((s, r) => s + (Number(r[c.key]) || 0), 0) * 100) / 100;
        return sum === undefined ? '<td></td>' : `<td><b>${escapeHTMLText(reportCellText(sum))}</b></td>`;
      }
      return i === 0 ? `<td><b>${manyPages && !allTotals ? 'Page total' : 'Total'}</b></td>` : '<td></td>';
    }).join('')}${extraCols ? `<td colspan="${extraCols}"></td>` : ''}</tr></tfoot>`;
  }
  html += `</table>`;
  if (manyPages) {
    const count = (n) => formatNumber(n);
    const last = paging.offset + rows.length;
    html += `<div class="text-muted s-c7bd98"><button type="button" class="btn btn-outline" data-rc-page="${Math.max(0, paging.offset - paging.limit)}" ${paging.offset > 0 ? '' : 'disabled'}>Previous</button><span>${rows.length ? `${count(paging.offset + 1)}-${count(last)}` : '0'} of ${count(paging.total)}</span><button type="button" class="btn btn-outline" data-rc-page="${paging.offset + paging.limit}" ${last < paging.total ? '' : 'disabled'}>Next</button></div>`;
  }
  container.innerHTML = html;
  loadReportThumbnails(container);
  container.dataset.params = JSON.stringify(params);
  container.querySelectorAll('[data-rc-page]').forEach((button) => {
    button.addEventListener('click', () => flipReportCatalogPage(Number(button.dataset.rcPage)));
  });
  container.querySelectorAll('[data-report-drill-index]').forEach(button => {
    button.addEventListener('click', () => runReportCatalogDrillDown(
      button.dataset.reportId,
      button.dataset.reportRowKey,
      Number(button.dataset.reportDrillIndex)
    ));
  });
  container.querySelectorAll('[data-merge-dup]').forEach(button => {
    button.addEventListener('click', async () => {
      const { mergeDup, mergeKeep, mergeDoctype: doctype } = button.dataset;
      const reason = await showCustomPrompt(`Merge ${mergeDup} into ${mergeKeep}. Why?`, '', 'Merge');
      if (reason === null) return;
      const res = await apiFetch('/api/v1/admin/master-merges', { method: 'POST', body: JSON.stringify({ doctype, keep_id: mergeKeep, merge_id: mergeDup, reason }) });
      if (!res) return;
      if (!res.ok) { showCustomAlert(await getErrorMessage(res, 'Merge failed.'), 'Merge'); return; }
      const out = await res.json();
      // 47.13.5: with a MasterMerge approval rule the merge waits in Approvals
      if (out.status === 'Pending Approval') showToast(`Sent for approval - ${out.id}.`, { variant: 'success' });
      else showToast(`Merged - ${out.merge?.moved ?? 0} reference(s) moved.`, { variant: 'success' });
      button.closest('tr').remove();
    });
  });
  container.querySelectorAll('[data-dismiss-kind]').forEach(button => {
    button.addEventListener('click', async () => {
      const note = await showCustomPrompt(`Dismiss ${button.dataset.dismissKind} for ${button.dataset.dismissRef}. Note (required):`, '', 'Dismiss');
      if (note === null) return;
      if (!String(note).trim()) { showCustomAlert('A note is required.', 'Dismiss'); return; }
      const res = await apiFetch('/api/v1/finance/reconciliation/exceptions', { method: 'POST', body: JSON.stringify({ kind: button.dataset.dismissKind, ref: button.dataset.dismissRef, status: 'Dismissed', note }) });
      if (!res) return;
      if (!res.ok) { showCustomAlert(await getErrorMessage(res, 'Could not dismiss.'), 'Dismiss'); return; }
      showToast('Dismissed.', { variant: 'success' });
      button.closest('tr').remove();
    });
  });
  container.querySelectorAll('[data-reopen-kind]').forEach(button => {
    button.addEventListener('click', async () => {
      const note = await showCustomPrompt(`Reopen ${button.dataset.reopenKind} for ${button.dataset.reopenRef}. Note (required):`, '', 'Reopen');
      if (note === null) return;
      if (!String(note).trim()) { showCustomAlert('A note is required.', 'Reopen'); return; }
      const res = await apiFetch('/api/v1/finance/reconciliation/exceptions', { method: 'POST', body: JSON.stringify({ kind: button.dataset.reopenKind, ref: button.dataset.reopenRef, status: 'Open', note }) });
      if (!res) return;
      if (!res.ok) { showCustomAlert(await getErrorMessage(res, 'Could not reopen.'), 'Reopen'); return; }
      showToast('Reopened.', { variant: 'success' });
      button.closest('td').textContent = 'Open';
    });
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
  let inner = `<div class="s-d8c022"><div class="s-c7d929">${drillRows.length} line(s) behind "${escapeHTMLText(rowKey)}"${scope ? ` - same filters (${escapeHTMLText(scope)})` : ''}</div><table class="s-046678"><thead><tr>${keys.map(k => `<th>${escapeHTMLText(k.replace(/_/g, ' '))}</th>`).join('')}</tr></thead><tbody>`;
  inner += drillRows.length === 0
    ? `<tr><td colspan="${keys.length || 1}" class="s-306094">No underlying rows for this figure.</td></tr>`
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
  if (job.status === 'Cancelled' || job.status === 'Revoked') {
    statusEl.innerHTML = `<p>Export ${job.status === 'Revoked' ? 'revoked' : 'cancelled'}.</p>`;
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

// Silent printing through PrintBridge (Stage 31.1).
//
// Every print path in this app used to be window.print() into a hidden
// @media print area, which pops the browser dialog and cannot choose a
// printer. These helpers route a job to a *named* OS printer instead, so a
// packing bench with a thermal label printer and an A4 invoice printer sends
// each document to the right one with a single click.
//
// Degrades rather than breaks: if PrintBridge is not installed or not running,
// agentTryPrint returns false and the caller falls back to the existing print
// sheet. Nobody is blocked from printing because the bridge is down.


export { renderReportsView, fillReportsMenu, fetchReportRows, execDashboardOpenReport, defaultDashboardTiles, loadDashboardLayouts, renderExecDashboard, renderExecDashboardBody, renderExecDashboardTrendChart, renderInventoryView, renderPayablesAgeingReport, renderReceivablesAgeingReport, renderGSTReturnSummaryReport, renderReportCatalogPanel, currentReportCatalogDef, initReportCatalogColumns, effectiveReportColumns, toggleReportColumnsPanel, renderReportColumnsPanel, moveReportColumn, applyReportColumnState, loadReportColumnProfiles, applyReportColumnProfile, saveReportColumnProfile, renderReportCatalogParams, collectReportCatalogParams, reportCatalogQueryString, runReportCatalogReport, renderReportCatalogResultTable, runReportCatalogDrillDown, loadReportCatalogSavedFilters, applyReportCatalogSavedFilter, saveReportCatalogFilter, exportReportCatalogReport, pollReportExportJob, downloadReportExportCSV };

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
