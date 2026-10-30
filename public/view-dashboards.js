
// 47.14.3: the KPI contract on a tile - the short lines in the title's
// tooltip, the whole contract behind the ⓘ button.
function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  return s < 90 ? 'just now' : s < 5400 ? `${Math.round(s / 60)} min ago` : s < 172800 ? `${Math.round(s / 3600)} h ago` : new Date(iso).toLocaleDateString();
}
export function kpiContractLines(c) {
  if (!c) return [];
  return [['Period', c.period], ['Scope', c.scope], ['Refreshed', c.as_of ? ago(c.as_of) : ''], ['Basis', c.basis], ['Owner', c.owner], ['Drill', c.drill]]
    .filter(([, v]) => v).map(([k, v]) => `${k}: ${v}`);
}
export const kpiContractSummary = (c) => kpiContractLines(c).slice(0, 4).join(' · ');
function showContract(w, c) {
  const box = document.createElement('div');
  box.innerHTML = `<p>${esc(c.definition || '')}</p>${kpiContractLines(c).map(l => `<div>${esc(l)}</div>`).join('')}${c.basis_note ? `<p class="text-muted">${esc(c.basis_note)}</p>` : ''}`;
  showCustomAlert(box, w.title);
}
// Stage 100 (user, 2026-10-19: "Data Custom Dasboard"; D1-D5 approved
// 2026-10-21): custom dashboards - build your own charts and number tiles
// from the ERP's data, filtered by date and store.
//
// Reports -> Dashboards lists yours, the ones shared with you and the
// ready-made five; a dashboard opens on a grid of widgets with its date /
// store / brand / category / channel filters (D3), refreshes while open
// (D5), copies, exports (a widget to CSV or Excel, the page to PDF) and
// emails itself on a schedule. Building needs Create on Dashboard Layout.
// Every number comes from POST /api/v1/dashboards/query, cut on the server
// to the viewer's stores and money rule (D4) - one dashboard serves every
// store manager. Charts: charts.js (SVG, no library).

const VQ = new URL(import.meta.url).search;
const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const KINDS = [['number', 'Number'], ['bar', 'Bar'], ['stacked', 'Stacked bar'], ['line', 'Line'], ['donut', 'Donut'], ['table', 'Table'], ['pivot', 'Pivot (rows × columns)']];
const RANGES = [['today', 'Today'], ['week', 'This week'], ['mtd', 'Month to date'], ['last30', 'Last 30 days'], ['month', 'Last month'], ['all', 'All dates'], ['custom', 'Choose dates']];
const AGGS = [['sum', 'Total of'], ['avg', 'Average'], ['min', 'Lowest'], ['max', 'Highest'], ['count', 'Count of'], ['distinct', 'How many different']];

let meta = null;
let catalog = null;
let myStores = null;
let refreshTimer = null;

const charts = () => import('./charts.js' + VQ);

async function loadMeta() {
  if (meta) return meta;
  await loadViewStyles('dashboards.css');
  const res = await apiFetch('/api/v1/dashboards/datasets');
  meta = res && res.ok ? await res.json() : { datasets: [], can_build: false, sees_money: false, refresh_min: 5 };
  return meta;
}

async function loadCatalog() {
  if (catalog) return catalog;
  const res = await apiFetch('/api/v1/reports/catalog');
  catalog = res && res.ok ? await res.json() : [];
  return catalog;
}

async function loadMyStores() {
  if (myStores) return myStores;
  const res = await apiFetch('/api/v1/me/stores');
  const data = res && res.ok ? await res.json() : null;
  myStores = (data && data.stores) || [];
  return myStores;
}

const datasetOf = (key) => (meta?.datasets || []).find(d => d.key === key);

function localDay(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

// rangeDates mirrors engines.ResolveDashboardRange.
function rangeDates(f) {
  const now = new Date();
  switch (f.range || 'mtd') {
    case 'today': return [localDay(now), localDay(now)];
    case 'week': { const d = new Date(now); d.setDate(d.getDate() - ((d.getDay() + 6) % 7)); return [localDay(d), localDay(now)]; }
    case 'mtd': return [localDay(new Date(now.getFullYear(), now.getMonth(), 1)), localDay(now)];
    case 'last30': { const d = new Date(now); d.setDate(d.getDate() - 29); return [localDay(d), localDay(now)]; }
    case 'month': return [localDay(new Date(now.getFullYear(), now.getMonth() - 1, 1)), localDay(new Date(now.getFullYear(), now.getMonth(), 0))];
    case 'custom': return [f.from || '', f.to || ''];
    default: return ['', ''];
  }
}

// widgetQuery puts the dashboard's filters on a widget (D3) unless it keeps
// its own - the same rule as engines.ApplyDashboardFilters.
function widgetQuery(w, f) {
  const q = JSON.parse(JSON.stringify(w.query || {}));
  if (w.own_filters) return q;
  const ds = datasetOf(q.dataset);
  if (!ds) return q;
  if (ds.date_field) [q.date_from, q.date_to] = rangeDates(f);
  if (f.store) q.store = f.store;
  q.filters = q.filters || [];
  [['brand', f.brand], ['category', f.category], ['channel', f.channel]].forEach(([field, v]) => {
    if (v && ds.fields.some(x => x.key === field)) q.filters.push({ field, op: 'eq', value: v });
  });
  return q;
}

// reportResult turns a report run into the dataset answer shape.
function reportResult(def, rows) {
  const cols = (def?.columns || []).map(c => ({ key: c.key, label: c.label,
    type: rows.length && rows.every(r => r[c.key] === null || r[c.key] === '' || typeof r[c.key] === 'number') ? 'number' : 'category' }));
  return { columns: cols, rows: rows.slice(0, 200), truncated: rows.length > 200 };
}

async function runReportWidget(w) {
  await loadCatalog();
  const def = catalog.find(d => d.id === w.report_id);
  const qs = new URLSearchParams(w.report_params || {}).toString();
  const res = await apiFetch(`/api/v1/reports/run/${encodeURIComponent(w.report_id)}${qs ? '?' + qs : ''}`);
  if (!res || !res.ok) return { error: res ? await getErrorMessage(res, 'The report did not run.') : 'The report did not run.' };
  const body = await res.json();
  return { result: reportResult(def, body.rows || []) };
}

// runWidgets answers every widget: the dataset ones in one request.
async function runWidgets(cd, f) {
  const out = new Array(cd.widgets.length);
  const idx = [];
  const queries = [];
  cd.widgets.forEach((w, i) => {
    if (w.source === 'dataset') { idx.push(i); queries.push(widgetQuery(w, f)); }
  });
  const jobs = [];
  if (queries.length) {
    jobs.push(apiFetch('/api/v1/dashboards/query', { method: 'POST', body: JSON.stringify({ queries }) }).then(async res => {
      if (!res || !res.ok) {
        const msg = res ? await getErrorMessage(res, 'The figures could not be loaded.') : 'The figures could not be loaded.';
        idx.forEach(i => { out[i] = { error: msg }; });
        return;
      }
      const { results = [] } = await res.json();
      idx.forEach((i, k) => { out[i] = results[k] || { error: 'No answer.' }; });
    }));
  }
  cd.widgets.forEach((w, i) => { if (w.source === 'report') jobs.push(runReportWidget(w).then(r => { out[i] = r; })); });
  await Promise.all(jobs);
  return out;
}

// --- the list ----------------------------------------------------------------

export async function renderDashboardsTab(panel, openId) {
  await loadMeta();
  if (openId) return openDashboard(panel, openId);
  panel.innerHTML = '<p class="dash-empty" style="padding:16px;">Loading dashboards…</p>';
  const res = await apiFetch('/api/v1/dashboards/custom');
  if (!res || !res.ok) { panel.innerHTML = `<p class="dash-error" style="padding:16px;">${esc(res ? await getErrorMessage(res, 'Dashboards could not be loaded.') : 'Dashboards could not be loaded.')}</p>`; return; }
  const { dashboards = [] } = await res.json();
  const card = d => `<button type="button" class="dash-card" data-dash-open="${esc(d.id)}">
      <strong>${esc(d.name)}</strong>
      <span class="dash-card-meta">${d.widgets.length} widget${d.widgets.length === 1 ? '' : 's'}${d.ready_made && !d.owner ? ' · ready-made' : d.mine ? ' · yours' : d.owner_name ? ` · from ${esc(d.owner_name)}` : ''}${(d.home_roles || []).length ? ` · home for ${esc(d.home_roles.join(', '))}` : ''}</span></button>`;
  const mine = dashboards.filter(d => d.mine);
  const shared = dashboards.filter(d => !d.mine && d.owner);
  const ready = dashboards.filter(d => !d.owner);
  panel.innerHTML = `<div style="padding:16px 20px;">
    <div class="dash-bar"><div class="text-muted">Charts and numbers from your data, for the stores you may see.</div>
      <div class="dash-actions">${meta.can_build ? '<button type="button" class="btn btn-primary" id="dash-new">+ New dashboard</button>' : ''}</div></div>
    ${mine.length ? `<h3 class="dash-section-title">Yours</h3><div class="dash-list">${mine.map(card).join('')}</div>` : ''}
    ${shared.length ? `<h3 class="dash-section-title">Shared with you</h3><div class="dash-list">${shared.map(card).join('')}</div>` : ''}
    <h3 class="dash-section-title">Ready-made</h3><div class="dash-list">${ready.map(card).join('')}</div>
    ${!meta.can_build ? '<p class="dash-note" style="margin-top:16px;">To build your own, ask an administrator for Create on Dashboard Layout (Roles).</p>' : ''}</div>`;
  panel.querySelectorAll('[data-dash-open]').forEach(b => b.addEventListener('click', () => openDashboard(panel, b.dataset.dashOpen)));
  panel.querySelector('#dash-new')?.addEventListener('click', () => openDashboard(panel, null, { edit: true,
    draft: { id: '', name: 'New dashboard', widgets: [], filters: { range: 'mtd' }, share_users: [], share_roles: [], share_stores: [], home_roles: [] } }));
}

// --- one dashboard -------------------------------------------------------------

async function fetchDashboard(id) {
  const res = await apiFetch(`/api/v1/dashboards/custom/${encodeURIComponent(id)}`);
  if (!res || !res.ok) throw new Error(res ? await getErrorMessage(res, 'This dashboard could not be opened.') : 'This dashboard could not be opened.');
  return res.json();
}

function filterBar(cd, f, stores) {
  const ds = cd.widgets.map(w => w.query && datasetOf(w.query.dataset)).filter(Boolean);
  const has = k => ds.some(d => d.fields.some(x => x.key === k));
  const dated = ds.some(d => d.date_field);
  return `${dated ? `<div class="form-group"><label class="form-label" for="dash-range">Dates</label><select id="dash-range" class="form-select">${RANGES.map(([v, l]) => `<option value="${v}"${(f.range || 'mtd') === v ? ' selected' : ''}>${l}</option>`).join('')}</select></div>
    <div class="form-group dash-custom${f.range === 'custom' ? '' : ' hidden'}"><label class="form-label" for="dash-from">From</label><input type="date" id="dash-from" class="form-input" value="${esc(f.from || '')}"></div>
    <div class="form-group dash-custom${f.range === 'custom' ? '' : ' hidden'}"><label class="form-label" for="dash-to">To</label><input type="date" id="dash-to" class="form-input" value="${esc(f.to || '')}"></div>` : ''}
    ${stores.length > 1 ? `<div class="form-group"><label class="form-label" for="dash-store">Store</label><select id="dash-store" class="form-select"><option value="">All my stores</option>${stores.map(s => `<option value="${esc(s.code)}"${f.store === s.code ? ' selected' : ''}>${esc(s.name || s.code)}</option>`).join('')}</select></div>` : ''}
    ${has('brand') ? `<div class="form-group"><label class="form-label" for="dash-brand">Brand</label><input id="dash-brand" class="form-input" value="${esc(f.brand || '')}" placeholder="Any"></div>` : ''}
    ${has('category') ? `<div class="form-group"><label class="form-label" for="dash-category">Category</label><input id="dash-category" class="form-input" value="${esc(f.category || '')}" placeholder="Any"></div>` : ''}
    ${has('channel') ? `<div class="form-group"><label class="form-label" for="dash-channel">Channel</label><input id="dash-channel" class="form-input" value="${esc(f.channel || '')}" placeholder="Any"></div>` : ''}`;
}

function readFilters(root, f) {
  const v = id => root.querySelector(`#${id}`)?.value ?? undefined;
  const out = { ...f };
  [['range', 'dash-range'], ['from', 'dash-from'], ['to', 'dash-to'], ['store', 'dash-store'], ['brand', 'dash-brand'], ['category', 'dash-category'], ['channel', 'dash-channel']]
    .forEach(([k, id]) => { const x = v(id); if (x !== undefined) out[k] = String(x).trim(); });
  return out;
}

function widgetBox(w, i, editing) {
  const tools = editing
    ? `<button type="button" data-dash-move="${i}:-1" title="Earlier" aria-label="Move earlier">◀</button><button type="button" data-dash-move="${i}:1" title="Later" aria-label="Move later">▶</button>
       <button type="button" data-dash-width="${i}:-3" title="Narrower" aria-label="Narrower">−</button><button type="button" data-dash-width="${i}:3" title="Wider" aria-label="Wider">+</button>
       <button type="button" data-dash-height="${i}" title="Taller / shorter" aria-label="Change height">↕</button>
       <button type="button" data-dash-edit="${i}" title="Edit" aria-label="Edit widget">✎</button><button type="button" data-dash-remove="${i}" title="Remove" aria-label="Remove widget">✕</button>`
    : `${w.source === 'dataset' ? `<button type="button" data-dash-info="${i}" title="What this shows" aria-label="What this shows">ⓘ</button><button type="button" data-dash-csv="${i}" title="Download CSV" aria-label="Download CSV">CSV</button><button type="button" data-dash-xlsx="${i}" title="Download Excel" aria-label="Download Excel">XLSX</button>` : ''}`;
  return `<div class="dash-widget" style="grid-column: span ${w.w || 6}; grid-row: span ${w.h || 2};" data-dash-widget="${i}">
    <div class="dash-widget-head"><span class="dash-widget-title" title="${esc(w.title)}">${esc(w.title)}</span><span class="dash-widget-tools">${tools}</span></div>
    <div class="dash-widget-body" data-dash-body="${i}"><p class="dash-empty">Loading…</p></div></div>`;
}

async function paintWidgets(grid, cd, f, editing) {
  const { renderChart } = await charts();
  const results = await runWidgets(cd, f);
  results.forEach((r, i) => {
    const body = grid.querySelector(`[data-dash-body="${i}"]`);
    if (!body) return;
    const w = cd.widgets[i];
    if (!r || r.error) { body.innerHTML = `<p class="dash-error">${esc(r ? r.error : 'No answer.')}</p>`; return; }
    body._result = r.result;
    const c = r.result.contract;
    if (c) {
      body._contract = c;
      const head = body.previousElementSibling;
      const title = head && head.querySelector('.dash-widget-title');
      if (title) title.title = `${w.title} · ${kpiContractSummary(c)}`;
      const info = head && head.querySelector('[data-dash-info]');
      if (info) info.onclick = () => showContract(w, c);
    }
    body._draw = () => renderChart(body, w.kind, r.result, { title: w.title, onPick: editing || w.source !== 'dataset' ? null : (row) => drill(w, row, f) });
    body._draw();
  });
  return results;
}

async function openDashboard(panel, id, { edit = false, draft = null } = {}) {
  clearInterval(refreshTimer);
  await loadMeta();
  let cd;
  try {
    cd = draft || await fetchDashboard(id);
  } catch (e) {
    panel.innerHTML = `<p class="dash-error" style="padding:16px;">${esc(e.message)}</p>`;
    return;
  }
  cd = JSON.parse(JSON.stringify(cd));
  const stores = await loadMyStores();
  let f = { range: 'mtd', ...(cd.filters || {}) };
  const editing = edit;
  panel.innerHTML = `<div style="padding:16px 20px;" class="${editing ? 'dash-editing' : ''}">
    <div class="dash-bar">
      <button type="button" class="btn btn-outline btn-sm" id="dash-back">← Dashboards</button>
      ${editing ? `<div class="form-group"><label class="form-label" for="dash-name">Name</label><input id="dash-name" class="form-input" value="${esc(cd.name)}" maxlength="80"></div>`
        : `<h2 style="margin:0;font-size:18px;">${esc(cd.name)}</h2>`}
      <div class="dash-actions">${editing
        ? `<button type="button" class="btn btn-outline" id="dash-add">+ Add widget</button><button type="button" class="btn btn-outline" id="dash-share">Share and home</button>
           <button type="button" class="btn btn-secondary" id="dash-cancel">Cancel</button><button type="button" class="btn btn-primary" id="dash-save">Save</button>`
        : `<button type="button" class="btn btn-outline btn-sm" id="dash-refresh">Refresh</button>
           ${cd.can_edit ? '<button type="button" class="btn btn-outline btn-sm" id="dash-edit">Edit</button>' : ''}
           ${meta.can_build ? '<button type="button" class="btn btn-outline btn-sm" id="dash-copy">Copy</button>' : ''}
           <button type="button" class="btn btn-outline btn-sm" id="dash-pdf">PDF</button>
           ${cd.mine || (cd.can_edit && cd.owner) ? '<button type="button" class="btn btn-outline btn-sm" id="dash-mail">Email me this</button>' : ''}
           ${cd.mine ? '<button type="button" class="btn btn-outline btn-sm" id="dash-delete">Delete</button>' : ''}`}</div>
    </div>
    <div class="dash-bar" id="dash-filters">${filterBar(cd, f, stores)}${editing ? '<span class="dash-note">These filters are saved with the dashboard as where it starts.</span>' : ''}</div>
    <div class="dash-grid" id="dash-grid">${cd.widgets.map((w, i) => widgetBox(w, i, editing)).join('') || '<p class="dash-empty">No widgets yet - add one.</p>'}</div>
    <p class="dash-note" id="dash-asof"></p></div>`;
  const root = panel.firstElementChild;
  const grid = root.querySelector('#dash-grid');
  const repaint = () => {
    grid.innerHTML = cd.widgets.map((w, i) => widgetBox(w, i, editing)).join('') || '<p class="dash-empty">No widgets yet - add one.</p>';
    wireGrid();
    paintWidgets(grid, cd, f, editing).then(() => { const a = root.querySelector('#dash-asof'); if (a) a.textContent = `As of ${new Date().toLocaleTimeString()}.`; });
  };
  const wireGrid = () => {
    grid.querySelectorAll('[data-dash-move]').forEach(b => b.addEventListener('click', () => {
      const [i, d] = b.dataset.dashMove.split(':').map(Number);
      const j = i + d;
      if (j < 0 || j >= cd.widgets.length) return;
      [cd.widgets[i], cd.widgets[j]] = [cd.widgets[j], cd.widgets[i]];
      repaint();
    }));
    grid.querySelectorAll('[data-dash-width]').forEach(b => b.addEventListener('click', () => {
      const [i, d] = b.dataset.dashWidth.split(':').map(Number);
      cd.widgets[i].w = Math.min(12, Math.max(3, (cd.widgets[i].w || 6) + d));
      repaint();
    }));
    grid.querySelectorAll('[data-dash-height]').forEach(b => b.addEventListener('click', () => {
      const i = Number(b.dataset.dashHeight);
      cd.widgets[i].h = ((cd.widgets[i].h || 2) % 4) + 1;
      repaint();
    }));
    grid.querySelectorAll('[data-dash-remove]').forEach(b => b.addEventListener('click', () => { cd.widgets.splice(Number(b.dataset.dashRemove), 1); repaint(); }));
    grid.querySelectorAll('[data-dash-edit]').forEach(b => b.addEventListener('click', async () => {
      const i = Number(b.dataset.dashEdit);
      const w = await openWidgetEditor(cd.widgets[i], f);
      if (w) { cd.widgets[i] = w; repaint(); }
    }));
    grid.querySelectorAll('[data-dash-csv]').forEach(b => b.addEventListener('click', () => downloadCSV(cd.widgets[Number(b.dataset.dashCsv)], grid.querySelector(`[data-dash-body="${b.dataset.dashCsv}"]`)?._result)));
    grid.querySelectorAll('[data-dash-xlsx]').forEach(b => b.addEventListener('click', () => downloadXLSX(cd.widgets[Number(b.dataset.dashXlsx)], f)));
  };
  root.querySelector('#dash-filters').addEventListener('change', () => {
    f = readFilters(root, f);
    root.querySelectorAll('.dash-custom').forEach(el => el.classList.toggle('hidden', f.range !== 'custom'));
    if (f.range === 'custom' && (!f.from || !f.to)) return;
    if (editing) cd.filters = f;
    repaint();
  });
  root.querySelector('#dash-back').addEventListener('click', () => { clearInterval(refreshTimer); renderDashboardsTab(panel); });
  root.querySelector('#dash-refresh')?.addEventListener('click', repaint);
  root.querySelector('#dash-edit')?.addEventListener('click', () => openDashboard(panel, cd.id, { edit: true, draft: cd }));
  root.querySelector('#dash-copy')?.addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/dashboards/custom/${encodeURIComponent(cd.id)}/copy`, { method: 'POST', body: '{}' });
    if (!res || !res.ok) { await showApiError(res, 'The dashboard was not copied.'); return; }
    const { id: newId } = await res.json();
    showToast('Copied - this one is yours to change.', { variant: 'success' });
    openDashboard(panel, newId, { edit: true });
  });
  root.querySelector('#dash-delete')?.addEventListener('click', async () => {
    if (!await showCustomConfirm(`Delete the dashboard "${cd.name}"? Anyone it is shared with loses it too.`, 'Delete dashboard')) return;
    const res = await apiFetch(`/api/v1/dashboards/custom/${encodeURIComponent(cd.id)}`, { method: 'DELETE' });
    if (!res || !res.ok) { await showApiError(res, 'The dashboard was not deleted.'); return; }
    renderDashboardsTab(panel);
  });
  root.querySelector('#dash-pdf')?.addEventListener('click', () => exportPDF(cd, f));
  root.querySelector('#dash-mail')?.addEventListener('click', () => scheduleDigest(cd));
  root.querySelector('#dash-add')?.addEventListener('click', async () => {
    const w = await openWidgetEditor(null, f);
    if (w) { cd.widgets.push(w); repaint(); }
  });
  root.querySelector('#dash-share')?.addEventListener('click', () => openShareDialog(cd));
  root.querySelector('#dash-cancel')?.addEventListener('click', () => (cd.id ? openDashboard(panel, cd.id) : renderDashboardsTab(panel)));
  root.querySelector('#dash-save')?.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    cd.name = root.querySelector('#dash-name').value.trim();
    cd.filters = readFilters(root, f);
    btn.disabled = true;
    const res = await apiFetch('/api/v1/dashboards/custom', { method: 'POST', body: JSON.stringify(cd) });
    btn.disabled = false;
    if (!res || !res.ok) { await showApiError(res, 'The dashboard was not saved.'); return; }
    const { id: savedId } = await res.json();
    showToast('Dashboard saved.', { variant: 'success' });
    openDashboard(panel, savedId);
  });
  wireGrid();
  repaint();
  // A resized window redraws the charts from the figures already in hand.
  let resizeTimer = null;
  const onResize = () => {
    if (!grid.isConnected) { window.removeEventListener('resize', onResize); return; }
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => grid.querySelectorAll('[data-dash-body]').forEach(b => b._draw && b._draw()), 250);
  };
  window.addEventListener('resize', onResize);
  // D5: reloaded while open, every N minutes (Configuration -> Reports).
  if (!editing) {
    const every = Math.max(1, Number(cd.refresh_minutes || meta.refresh_min || 5));
    refreshTimer = setInterval(() => { if (!grid.isConnected) { clearInterval(refreshTimer); return; } if (!document.hidden) repaint(); }, every * 60000);
  }
}

// --- click through (100.2.3) -------------------------------------------------

async function drill(w, row, f) {
  const ds = datasetOf(w.query.dataset);
  if (!ds) return;
  const q = widgetQuery(w, f);
  const groups = (q.group_by || []);
  q.filters = [...(q.filters || []), ...groups.filter(g => row[g] !== undefined && ds.fields.find(x => x.key === g)?.type !== 'date').map(g => ({ field: g, op: 'eq', value: String(row[g]) }))];
  const dateG = groups.find(g => ds.fields.find(x => x.key === g)?.type === 'date');
  if (dateG && row[dateG]) {
    const start = new Date(row[dateG]);
    const end = new Date(start);
    if (q.grain === 'month') end.setMonth(end.getMonth() + 1, 0);
    else if (q.grain === 'week') end.setDate(end.getDate() + 6);
    q.date_from = String(row[dateG]).slice(0, 10);
    q.date_to = localDay(end);
  }
  const title = `${w.title}: ${groups.map(g => row[g]).join(' · ')}`;
  if (!ds.doc_field) {
    if (ds.list) { setActiveMenu(`menu-${ds.list}`); renderView(ds.list); }
    return;
  }
  q.group_by = [ds.doc_field];
  q.grain = '';
  q.limit = 100;
  q.sort = '';
  const res = await apiFetch('/api/v1/dashboards/query', { method: 'POST', body: JSON.stringify({ query: q }) });
  if (!res || !res.ok) { await showApiError(res, 'The records behind this could not be loaded.'); return; }
  const result = await res.json();
  const { documentSearchLink } = await import('./view-document-links.js' + VQ);
  const { fmtValue } = await charts();
  const docCol = result.columns[0];
  const rows = result.rows.map(r => `<tr><td>${documentSearchLink(ds.doctype, r[docCol.key])}</td>${result.columns.slice(1).map(c => `<td class="num">${esc(fmtValue(r[c.key], c))}</td>`).join('')}</tr>`).join('');
  dialog(title, `<div class="dash-table" style="max-height:60vh;overflow:auto;"><table><thead><tr>${result.columns.map(c => `<th class="${c.type === 'number' ? 'num' : ''}">${esc(c.label)}</th>`).join('')}</tr></thead><tbody>${rows || '<tr><td>None.</td></tr>'}</tbody></table></div>
    ${result.truncated ? '<p class="dash-note">The first 100 are shown.</p>' : ''}${ds.list ? `<p><a href="#" data-dash-list>Open ${esc(ds.label)}</a></p>` : ''}`, (box, close) => {
    box.querySelectorAll('a.doc-link').forEach(a => a.addEventListener('click', close));
    box.querySelector('[data-dash-list]')?.addEventListener('click', (e) => { e.preventDefault(); close(); setActiveMenu(`menu-${ds.list}`); renderView(ds.list); });
  });
}

// --- a small dialog (own attributes: data-act is the app's dispatcher) -----------

function dialog(title, body, wire, { wide = true, ok = '', onOk = null } = {}) {
  document.getElementById('dash-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'dash-dialog';
  overlay.innerHTML = `<div class="modal-container" style="max-width:${wide ? '980px' : '560px'};width:96vw;" role="dialog" aria-modal="true" aria-labelledby="dash-dialog-title">
    <div class="modal-header"><h3 class="modal-title" id="dash-dialog-title">${esc(title)}</h3><button type="button" class="modal-close" data-dash-close aria-label="Close">&times;</button></div>
    <div class="modal-body">${body}<div class="dash-error hidden" id="dash-dialog-error"></div></div>
    <div class="modal-footer"><button type="button" class="btn btn-secondary" data-dash-close>${ok ? 'Cancel' : 'Close'}</button>${ok ? `<button type="button" class="btn btn-primary" id="dash-dialog-ok">${esc(ok)}</button>` : ''}</div></div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-dash-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  if (wire) wire(overlay, close);
  overlay.querySelector('#dash-dialog-ok')?.addEventListener('click', async () => {
    const err = overlay.querySelector('#dash-dialog-error');
    const msg = onOk ? await onOk(overlay) : '';
    if (msg) { err.textContent = msg; err.classList.remove('hidden'); return; }
    close();
  });
  (overlay.querySelector('input, select, button.btn-primary') || overlay.querySelector('.modal-close'))?.focus();
  return overlay;
}

// --- the widget editor (100.2.2) ----------------------------------------------

const needGroups = { number: 0, bar: 1, line: 1, donut: 1, table: 1, stacked: 2, pivot: 2 };

function openWidgetEditor(widget, f) {
  return new Promise(async (resolve) => {
    await loadMeta();
    await loadCatalog();
    const stores = await loadMyStores();
    const w = JSON.parse(JSON.stringify(widget || { title: '', kind: 'bar', source: 'dataset', w: 6, h: 2,
      query: { dataset: (meta.datasets[0] || {}).key || '', group_by: [], measures: [{ agg: 'count', field: '' }], filters: [] } }));
    w.query = w.query || { dataset: (meta.datasets[0] || {}).key || '', group_by: [], measures: [{ agg: 'count', field: '' }], filters: [] };
    let done = false;
    const opt = (list, v, blank) => `${blank !== undefined ? `<option value="">${esc(blank)}</option>` : ''}${list.map(([k, l]) => `<option value="${esc(k)}"${k === v ? ' selected' : ''}>${esc(l)}</option>`).join('')}`;
    const form = () => {
      const q = w.query;
      const ds = datasetOf(q.dataset) || { fields: [] };
      const cats = ds.fields.filter(x => x.type !== 'number').map(x => [x.key, x.label]);
      const nums = ds.fields.filter(x => x.type === 'number').map(x => [x.key, x.label]);
      const n = needGroups[w.kind] ?? 1;
      const g = q.group_by || [];
      const isDate = k => ds.fields.find(x => x.key === k)?.type === 'date';
      const rep = catalog.find(d => d.id === w.report_id);
      return `<div class="dash-editor"><div>
        <div class="form-group"><label class="form-label" for="dwe-title">Title</label><input id="dwe-title" class="form-input" value="${esc(w.title)}" maxlength="80" placeholder="e.g. Sales by store"></div>
        <div class="form-group"><label class="form-label" for="dwe-source">Data from</label><select id="dwe-source" class="form-select">${opt([['dataset', 'A dataset (choose fields)'], ['report', 'A report']], w.source)}</select></div>
        ${w.source === 'report' ? `
          <div class="form-group"><label class="form-label" for="dwe-report">Report</label><select id="dwe-report" class="form-select">${opt(catalog.map(d => [d.id, `${d.category} · ${d.label}`]), w.report_id, 'Choose a report…')}</select></div>
          ${(rep?.params || []).map(p => `<div class="form-group"><label class="form-label" for="dwe-p-${esc(p.key)}">${esc(p.label)}</label><input id="dwe-p-${esc(p.key)}" data-dwe-param="${esc(p.key)}" class="form-input" type="${p.type === 'date' ? 'date' : 'text'}" value="${esc((w.report_params || {})[p.key] || '')}"></div>`).join('')}
          <div class="form-group"><label class="form-label" for="dwe-kind">Show as</label><select id="dwe-kind" class="form-select">${opt([['table', 'Table'], ['bar', 'Bar'], ['number', 'Number']], w.kind)}</select></div>`
        : `
          <div class="form-group"><label class="form-label" for="dwe-dataset">Dataset</label><select id="dwe-dataset" class="form-select">${opt(meta.datasets.map(d => [d.key, d.label]), q.dataset)}</select><div class="dash-note">${esc(ds.about || '')}</div></div>
          <div class="form-group"><label class="form-label" for="dwe-kind">Chart</label><select id="dwe-kind" class="form-select">${opt(KINDS, w.kind)}</select></div>
          ${n >= 1 ? `<div class="form-group"><label class="form-label" for="dwe-g0">${n === 2 ? 'Rows (or bars) by' : 'By'}</label><select id="dwe-g0" class="form-select">${opt(cats, g[0], 'Choose…')}</select></div>` : ''}
          ${n === 2 ? `<div class="form-group"><label class="form-label" for="dwe-g1">${w.kind === 'pivot' ? 'Columns by' : 'Split each bar by'}</label><select id="dwe-g1" class="form-select">${opt(cats.filter(c => c[0] !== g[0]), g[1], 'Choose…')}</select></div>` : ''}
          ${g.some(isDate) ? `<div class="form-group"><label class="form-label" for="dwe-grain">Per</label><select id="dwe-grain" class="form-select">${opt([['day', 'Day'], ['week', 'Week'], ['month', 'Month']], q.grain || 'day')}</select></div>` : ''}
          <div class="form-group"><span class="form-label">Show</span>${(q.measures || []).map((m, i) => `<div class="dash-editor-row">
            <select class="form-select" data-dwe-agg="${i}" aria-label="How">${opt(AGGS, m.agg)}</select>
            <select class="form-select" data-dwe-mfield="${i}" aria-label="Of">${m.agg === 'count' ? `<option value="">rows</option>` : ''}${opt(m.agg === 'count' || m.agg === 'distinct' ? cats.concat(nums) : nums, m.field)}</select>
            ${i ? `<button type="button" class="action-btn" data-dwe-mdel="${i}" aria-label="Remove">✕</button>` : ''}</div>`).join('')}
            ${(q.measures || []).length < 3 && w.kind !== 'donut' && w.kind !== 'pivot' ? '<button type="button" class="action-btn" id="dwe-madd">+ Another figure</button>' : ''}</div>
          <div class="form-group"><span class="form-label">Only where</span>${(q.filters || []).map((x, i) => `<div class="dash-editor-row">
            <select class="form-select" data-dwe-ffield="${i}" aria-label="Field">${opt(cats.concat(nums), x.field)}</select>
            <select class="form-select" data-dwe-fop="${i}" aria-label="Test">${opt([['eq', 'is'], ['ne', 'is not'], ['in', 'is one of (a,b)'], ['contains', 'contains'], ['gte', 'at least'], ['lte', 'at most']], x.op)}</select>
            <input class="form-input" data-dwe-fval="${i}" value="${esc(x.value)}" aria-label="Value" style="width:120px;"><button type="button" class="action-btn" data-dwe-fdel="${i}" aria-label="Remove">✕</button></div>`).join('')}
            ${(q.filters || []).length < 4 ? '<button type="button" class="action-btn" id="dwe-fadd">+ Condition</button>' : ''}</div>
          ${n >= 1 ? `<div class="dash-editor-row"><label class="form-label" for="dwe-sort" style="margin:0;">Order</label><select id="dwe-sort" class="form-select">${opt([['', 'Highest first'], ['asc', 'Lowest first'], ['group', 'By name / date']], q.sort || '')}</select>
            <label class="form-label" for="dwe-limit" style="margin:0;">Show</label><input id="dwe-limit" class="form-input" type="number" min="1" max="1000" value="${esc(q.limit || 10)}" style="width:80px;"></div>` : ''}
          ${w.kind === 'number' ? `<label class="dash-editor-row"><input type="checkbox" id="dwe-compare"${q.compare ? ' checked' : ''}> Show the change on the period before</label>` : ''}
          <label class="dash-editor-row"><input type="checkbox" id="dwe-own"${w.own_filters ? ' checked' : ''}> Keep its own dates and store (ignore the dashboard's)</label>
          ${w.own_filters ? `<div class="dash-editor-row"><input type="date" id="dwe-from" class="form-input" value="${esc(q.date_from || '')}" aria-label="From"><input type="date" id="dwe-to" class="form-input" value="${esc(q.date_to || '')}" aria-label="To">
            ${stores.length > 1 ? `<select id="dwe-store" class="form-select" aria-label="Store">${opt(stores.map(s => [s.code, s.name || s.code]), q.store || '', 'All my stores')}</select>` : ''}</div>` : ''}`}
        <div class="dash-editor-row"><label class="form-label" for="dwe-w" style="margin:0;">Width</label><select id="dwe-w" class="form-select">${opt([['3', 'Quarter'], ['4', 'Third'], ['6', 'Half'], ['8', 'Two thirds'], ['12', 'Full']], String(w.w || 6))}</select>
          <label class="form-label" for="dwe-h" style="margin:0;">Height</label><select id="dwe-h" class="form-select">${opt([['1', 'Short'], ['2', 'Medium'], ['3', 'Tall'], ['4', 'Taller']], String(w.h || 2))}</select></div>
      </div><div><div class="form-label">Preview</div><div class="dash-preview" id="dwe-preview"><p class="dash-empty">…</p></div></div></div>`;
    };
    const read = (box) => {
      const q = w.query;
      const v = id => box.querySelector(`#${id}`);
      w.title = v('dwe-title').value.trim();
      const src = v('dwe-source').value;
      if (src !== w.source) { w.source = src; w.kind = src === 'report' ? 'table' : 'bar'; return; }
      if (w.source === 'report') {
        w.report_id = v('dwe-report').value;
        w.report_params = {};
        box.querySelectorAll('[data-dwe-param]').forEach(i => { if (i.value) w.report_params[i.dataset.dweParam] = i.value; });
        w.kind = v('dwe-kind').value;
      } else {
        const dsKey = v('dwe-dataset').value;
        if (dsKey !== q.dataset) { w.query = { dataset: dsKey, group_by: [], measures: [{ agg: 'count', field: '' }], filters: [] }; return; }
        w.kind = v('dwe-kind').value;
        const n = needGroups[w.kind] ?? 1;
        q.group_by = [v('dwe-g0')?.value, v('dwe-g1')?.value].filter(Boolean).slice(0, n);
        q.grain = v('dwe-grain')?.value || '';
        q.measures = (q.measures || []).map((m, i) => ({ agg: box.querySelector(`[data-dwe-agg="${i}"]`)?.value || m.agg, field: box.querySelector(`[data-dwe-mfield="${i}"]`)?.value ?? m.field }));
        q.measures.forEach(m => { if (m.agg !== 'count' && m.agg !== 'distinct' && !(datasetOf(q.dataset)?.fields || []).some(x => x.key === m.field && x.type === 'number')) m.field = (datasetOf(q.dataset)?.fields || []).find(x => x.type === 'number')?.key || ''; });
        if (w.kind === 'donut' || w.kind === 'pivot') q.measures = q.measures.slice(0, 1);
        q.filters = (q.filters || []).map((x, i) => ({ field: box.querySelector(`[data-dwe-ffield="${i}"]`)?.value || x.field, op: box.querySelector(`[data-dwe-fop="${i}"]`)?.value || x.op, value: box.querySelector(`[data-dwe-fval="${i}"]`)?.value ?? x.value }));
        q.sort = v('dwe-sort')?.value || '';
        q.limit = Number(v('dwe-limit')?.value) || (w.kind === 'number' ? 0 : 10);
        q.compare = !!v('dwe-compare')?.checked;
        w.own_filters = !!v('dwe-own')?.checked;
        if (w.own_filters) { q.date_from = v('dwe-from')?.value || ''; q.date_to = v('dwe-to')?.value || ''; q.store = v('dwe-store')?.value || ''; } else { delete q.date_from; delete q.date_to; delete q.store; }
      }
      w.w = Number(v('dwe-w').value);
      w.h = Number(v('dwe-h').value);
    };
    const problem = () => {
      if (!w.title) return 'Give the widget a title.';
      if (w.source === 'report') return w.report_id ? '' : 'Choose a report.';
      const n = needGroups[w.kind] ?? 1;
      if ((w.query.group_by || []).length < n) return n === 2 ? 'Choose both groupings.' : 'Choose what to show it by.';
      return '';
    };
    let timer = null;
    const preview = async (box) => {
      const host = box.querySelector('#dwe-preview');
      const p = problem();
      if (p && p !== 'Give the widget a title.') { host.innerHTML = `<p class="dash-empty">${esc(p)}</p>`; return; }
      host.innerHTML = '<p class="dash-empty">Loading…</p>';
      const [r] = await runWidgets({ widgets: [w] }, f);
      if (!host.isConnected) return;
      if (!r || r.error) { host.innerHTML = `<p class="dash-error">${esc(r ? r.error : 'No answer.')}</p>`; return; }
      (await charts()).renderChart(host, w.kind, r.result, { title: w.title });
    };
    // The form is redrawn inside one stable holder: a change can fire again
    // (on blur) while the last redraw is still settling.
    let redrawing = false;
    const redraw = (box) => {
      if (redrawing) return;
      redrawing = true;
      try {
        // Blurring first lets a typed value's own change be read now, not
        // fire again while its box is being replaced.
        if (box.contains(document.activeElement)) document.activeElement.blur();
        box.querySelector('#dwe-host').innerHTML = form();
      } finally {
        redrawing = false;
      }
      wire(box);
      clearTimeout(timer);
      timer = setTimeout(() => preview(box), 300);
    };
    const wire = (box) => {
      const body = box.querySelector('.dash-editor');
      body.addEventListener('change', () => { read(box); if (!redrawing) redraw(box); });
      body.addEventListener('input', (e) => { if (e.target.id === 'dwe-title') return; clearTimeout(timer); timer = setTimeout(() => { read(box); preview(box); }, 600); });
      body.querySelector('#dwe-madd')?.addEventListener('click', () => { read(box); w.query.measures.push({ agg: 'sum', field: '' }); read(box); redraw(box); });
      body.querySelector('#dwe-fadd')?.addEventListener('click', () => { read(box); w.query.filters.push({ field: '', op: 'eq', value: '' }); redraw(box); });
      body.querySelectorAll('[data-dwe-mdel]').forEach(b => b.addEventListener('click', () => { read(box); w.query.measures.splice(Number(b.dataset.dweMdel), 1); redraw(box); }));
      body.querySelectorAll('[data-dwe-fdel]').forEach(b => b.addEventListener('click', () => { read(box); w.query.filters.splice(Number(b.dataset.dweFdel), 1); redraw(box); }));
    };
    const box = dialog(widget ? 'Edit widget' : 'Add a widget', `<div id="dwe-host">${form()}</div>`, (b) => { wire(b); preview(b); }, {
      ok: widget ? 'Update' : 'Add',
      onOk: async (b) => {
        read(b);
        const p = problem();
        if (p) return p;
        w.query.filters = (w.query.filters || []).filter(x => x.field && x.value !== '');
        done = true;
        resolve(w);
        return '';
      }
    });
    new MutationObserver((_, obs) => { if (!box.isConnected) { obs.disconnect(); if (!done) resolve(null); } }).observe(document.body, { childList: true });
  });
}

// --- sharing and home (100.3.2) ----------------------------------------------

async function openShareDialog(cd) {
  const stores = await loadMyStores();
  let roles = [];
  const res = await apiFetch('/api/v1/admin/roles').catch(() => null);
  if (res && res.ok) {
    const data = await res.json();
    roles = (Array.isArray(data) ? data : data.roles || []).map(r => (typeof r === 'string' ? r : r.name || r.role)).filter(Boolean);
  }
  const chips = (name, list, picked) => `<div class="dash-chips">${list.map(v => `<label><input type="checkbox" data-dsh-${name}="${esc(v.code || v)}"${picked.includes(v.code || v) ? ' checked' : ''}> ${esc(v.name || v)}</label>`).join('') || '<span class="dash-note">None to pick.</span>'}</div>`;
  dialog('Share and home', `<p class="dash-note">Everyone it is shared with sees only their own stores' figures, and money only if their role may.</p>
    <div class="dash-share">
      <div class="form-group"><label class="form-label" for="dsh-users">Users (their sign-in names, comma between)</label><input id="dsh-users" class="form-input" value="${esc((cd.share_users || []).join(', '))}"></div>
      <div class="form-group"><span class="form-label">Roles</span>${roles.length ? chips('role', roles, cd.share_roles || []) : `<input id="dsh-roles" class="form-input" value="${esc((cd.share_roles || []).join(', '))}" placeholder="e.g. Store Manager">`}</div>
      <div class="form-group"><span class="form-label">Stores (everyone working at them)</span>${chips('store', stores, cd.share_stores || [])}</div>
      <div class="form-group"><span class="form-label">Home screen for these roles</span>${roles.length ? chips('home', roles, cd.home_roles || []) : `<input id="dsh-home" class="form-input" value="${esc((cd.home_roles || []).join(', '))}" placeholder="e.g. Store Manager">`}</div>
    </div>`, null, { ok: 'Done', onOk: (b) => {
    const list = (id) => (b.querySelector(`#${id}`)?.value || '').split(',').map(s => s.trim()).filter(Boolean);
    const picked = (name) => [...b.querySelectorAll(`[data-dsh-${name}]:checked`)].map(i => i.getAttribute(`data-dsh-${name}`));
    cd.share_users = list('dsh-users');
    cd.share_roles = roles.length ? picked('role') : list('dsh-roles');
    cd.share_stores = picked('store');
    cd.home_roles = roles.length ? picked('home') : list('dsh-home');
    showToast('Sharing set - Save the dashboard to keep it.', { variant: 'info' });
    return '';
  } });
}

// --- export (100.3.4) ---------------------------------------------------------

function downloadCSV(w, result) {
  if (!result) return;
  const cell = v => { let s = v === null || v === undefined ? '' : String(v); if (/^[=+\-@]/.test(s) && isNaN(Number(s))) s = `'${s}`; return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s; };
  const text = [result.columns.map(c => cell(c.label)).join(','), ...result.rows.map(r => result.columns.map(c => cell(r[c.key])).join(','))].join('\r\n');
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob(['﻿' + text], { type: 'text/csv' }));
  a.download = `${w.title || 'widget'}.csv`;
  document.body.appendChild(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

async function downloadXLSX(w, f) {
  const res = await apiFetch('/api/v1/dashboards/export', { method: 'POST', body: JSON.stringify({ title: w.title, query: widgetQuery(w, f) }) });
  if (!res || !res.ok) { await showApiError(res, 'The file could not be made.'); return; }
  const blob = await res.blob();
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${w.title || 'widget'}.xlsx`;
  document.body.appendChild(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

// exportPDF draws the dashboard, light-themed, onto an A4 landscape page and
// saves it as a PDF - print-pdf.js's way (98.2), no library.
async function exportPDF(cd, f) {
  showToast('Making the PDF…', { variant: 'info' });
  const { renderChart } = await charts();
  const { canvasesToPDF, downloadBlob } = await import('./print-pdf.js' + VQ);
  const results = await runWidgets(cd, f);
  const W = 1400;
  const box = document.createElement('div');
  box.className = 'dash-print';
  box.style.cssText = `position:fixed;left:-20000px;top:0;width:${W}px;box-sizing:border-box;background:#fff;color:#111827;padding:24px;font-family:Arial,Helvetica,sans-serif;` +
    '--text-main:#111827;--text-muted:#6b7280;--border-color:#e5e7eb;--panel-bg:#ffffff;--surface-subtle:#f3f4f6;--success-color:#15803d;--danger-color:#b91c1c;' +
    '--chart-1:#4f6bed;--chart-2:#16a34a;--chart-3:#f59e0b;--chart-4:#dc2626;--chart-5:#0891b2;--chart-6:#9333ea;--chart-7:#db2777;--chart-8:#65a30d;';
  const [from, to] = rangeDates(f);
  box.innerHTML = `<h2 style="margin:0 0 4px;">${esc(cd.name)}</h2><p style="margin:0 0 14px;color:#6b7280;">${from ? `${esc(from)} to ${esc(to)}` : 'All dates'}${f.store ? ` · ${esc(f.store)}` : ''}</p>
    <div class="dash-grid">${cd.widgets.map((w, i) => `<div class="dash-widget" style="grid-column: span ${w.w || 6}; grid-row: span ${w.h || 2};"><div class="dash-widget-head"><span class="dash-widget-title">${esc(w.title)}</span></div><div class="dash-widget-body" data-p="${i}"></div></div>`).join('')}</div>`;
  document.body.appendChild(box);
  results.forEach((r, i) => {
    const host = box.querySelector(`[data-p="${i}"]`);
    if (!r || r.error) host.innerHTML = `<p>${esc(r ? r.error : '')}</p>`;
    else renderChart(host, cd.widgets[i].kind, r.result, { title: cd.widgets[i].title });
  });
  box.querySelectorAll('.sr-only').forEach(el => el.remove());
  try {
    const css = await (await fetch('/dashboards.css' + VQ)).text();
    const h = box.scrollHeight;
    const scale = 1.25;
    const copy = box.cloneNode(true);
    copy.style.position = 'static';
    copy.style.left = '0';
    const xhtml = new XMLSerializer().serializeToString(copy);
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W * scale}" height="${h * scale}"><foreignObject x="0" y="0" width="${W * scale}" height="${h * scale}">` +
      `<div xmlns="http://www.w3.org/1999/xhtml" style="transform:scale(${scale});transform-origin:0 0;width:${W}px;"><style>${css.replace(/<\/style/gi, '')}</style>${xhtml}</div></foreignObject></svg>`;
    const img = new Image();
    img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
    await img.decode();
    // A4 landscape pages; a long dashboard runs on to more pages.
    const pageW = W * scale, pageH = Math.round(pageW * 210 / 297);
    const canvases = [];
    for (let y = 0; y < h * scale; y += pageH) {
      const c = document.createElement('canvas');
      c.width = pageW;
      c.height = pageH;
      const g = c.getContext('2d');
      g.fillStyle = '#fff';
      g.fillRect(0, 0, pageW, pageH);
      g.drawImage(img, 0, -y);
      canvases.push(c);
    }
    downloadBlob(canvasesToPDF(canvases, 297, 210), `${cd.name || 'dashboard'}.pdf`);
  } catch (e) {
    await showCustomAlert('This browser could not make the PDF. Use the browser\'s Print and choose Save as PDF instead.', 'PDF');
  } finally {
    box.remove();
  }
}

function scheduleDigest(cd) {
  const tomorrow = new Date();
  tomorrow.setDate(tomorrow.getDate() + 1);
  dialog('Email me this dashboard', `<p class="dash-note">The email carries each widget as numbers and bars, with the rows attached as a CSV - for the stores you may see.</p>
    <div class="form-group"><label class="form-label" for="dmail-to">Email to</label><input id="dmail-to" class="form-input" type="email" placeholder="you@example.com"></div>
    <div class="dash-editor-row"><label class="form-label" for="dmail-freq" style="margin:0;">How often</label><select id="dmail-freq" class="form-select"><option>Daily</option><option>Weekly</option><option>Monthly</option></select>
      <label class="form-label" for="dmail-first" style="margin:0;">First email on</label><input id="dmail-first" class="form-input" type="date" value="${localDay(tomorrow)}"></div>
    <p class="dash-note">See and stop it under Reports → Schedules.</p>`, null, { wide: false, ok: 'Schedule', onOk: async (b) => {
    const res = await apiFetch(`/api/v1/dashboards/custom/${encodeURIComponent(cd.id)}/digest`, { method: 'POST', body: JSON.stringify({
      email: b.querySelector('#dmail-to').value, frequency: b.querySelector('#dmail-freq').value, first_run: b.querySelector('#dmail-first').value }) });
    if (!res || !res.ok) return res ? getErrorMessage(res, 'It was not scheduled.') : 'It was not scheduled.';
    showToast('Scheduled - see it under Reports → Schedules.', { variant: 'success' });
    return '';
  } });
}

// --- Home (91.3 + 100.3.2) ----------------------------------------------------

// mountHomeExtras fills Home's slot under the title: an administrator's
// "finish setting up" card, then the dashboard set as this role's home.
export async function mountHomeExtras(host) {
  if (isAdminRoleName(localStorage.getItem('erp_role'))) {
    const card = host.appendChild(document.createElement('div'));
    loadViewModule('/view-admin.js').then(m => m.mountSetupGuideCard(card)).catch(() => {});
  }
  const res = await apiFetch('/api/v1/dashboards/home').catch(() => null);
  const data = res && res.ok ? await res.json() : null;
  if (!data || !data.dashboard) return;
  await loadMeta();
  const cd = data.dashboard;
  const f = { range: 'mtd', ...(cd.filters || {}) };
  const slot = host.appendChild(document.createElement('div'));
  slot.className = 'table-panel';
  slot.style.cssText = 'padding:16px 20px;margin-bottom:20px;';
  slot.innerHTML = `<div class="dash-bar"><h2 class="card-title" style="margin:0;">${esc(cd.name)}</h2>
    <div class="dash-actions"><button type="button" class="btn btn-outline btn-sm" id="home-dash-open">Open in Dashboards</button></div></div>
    <div class="dash-grid">${cd.widgets.map((w, i) => widgetBox(w, i, false).replace(/<button[^>]*data-dash-(csv|xlsx)[^>]*>[^<]*<\/button>/g, '')).join('')}</div>`;
  slot.querySelector('#home-dash-open').addEventListener('click', () => { sessionStorage.setItem('erp_open_dashboard', cd.id); setActiveMenu('menu-reports'); renderView('reports:dashboards'); });
  paintWidgets(slot.querySelector('.dash-grid'), cd, f, false);
}
