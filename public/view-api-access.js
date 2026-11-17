// Stage 38.2d / 55.21 / 205.10-205.13, 205.18-205.21: API access - the screen for
// the keys an outside system uses on /api/public/v1, controlled module by module.
// Native ES module on its own Settings page. List first; New API access, the
// Created page (the key, shown once) and a key's Detail page are their own
// pages; Docs is the viewer for the generated spec. Nothing here holds a
// secret after the Created page. Routes: /api/v1/api-access/* (plan, "Interfaces").
import { buildModuleGrid, summaryOf } from './view-api-access-grid.js';
import { BASE, call, rowsOf, ago, statusBadge, chipsOf, attentionHTML, bindFix, mountShareDocs, renderKeyDetail } from './view-api-access-detail.js';
import { renderApiDocs, esc } from './api-docs-viewer.js';
import { renderKeyOnce } from './api-key-once.js';

const SCREEN = 'api-access';
const search = new URL(import.meta.url).search;

if (!document.querySelector('link[href*="api-access.css"]')) {
  await new Promise(done => {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = `/api-access.css${search}`;
    link.onload = link.onerror = done;
    document.head.appendChild(link);
  });
}

// Quick starts pre-tick modules. A module the asker cannot give is skipped.
const QUICK = [
  ['store', 'Online store sync', { pim: 'read', inventory: 'read', oms: 'write' }],
  ['accounting', 'Accounting export', { finance: 'read', procurement: 'read', oms: 'read' }],
  ['reporting', 'Reporting (view only)', 'all-view'],
  ['warehouse', 'Warehouse device', { wms: 'write', inventory: 'read' }],
  ['custom', 'Custom', {}],
];

const EXPIRY = [['30d', '30 days'], ['90d', '90 days'], ['1y', '1 year'], ['date', 'A date'], ['never', 'Never']];

function expiryDate(code, picked) {
  const d = new Date();
  if (code === 'never') return null;
  if (code === '30d') d.setDate(d.getDate() + 30);
  else if (code === '90d') d.setDate(d.getDate() + 90);
  else if (code === '1y') d.setFullYear(d.getFullYear() + 1);
  else if (code === 'date') return picked ? new Date(`${picked}T23:59:59`) : null;
  return d;
}

const fmtDay = (d) => d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });

async function loadStores() {
  try {
    const res = await apiFetch('/api/v1/doc/Location?limit=500');
    if (!res || !res.ok) return [];
    const rows = await res.json();
    return (Array.isArray(rows) ? rows : [])
      .map(r => ({ code: r.code || r.id, name: r.name || r.code || r.id }))
      .sort((a, b) => String(a.name).localeCompare(String(b.name)));
  } catch (_) { return []; }
}

function canUseApiAccess() {
  return state.permissions.isAdmin || state.permissions.capabilities.has('api.share');
}

async function renderAPIAccessPanel(container) {
  const panel = document.createElement('div');
  panel.id = 'api-access-panel';
  container.appendChild(panel);
  const isAdmin = !!state.permissions.isAdmin;

  const [modRes, stores, txn] = await Promise.all([
    call('/modules'), loadStores(), import('./txn-screen.js' + search),
  ]);
  if (!modRes) return;
  if (!modRes.ok) {
    panel.innerHTML = '<h1 class="page-title">API access</h1>';
    const msg = document.createElement('p');
    msg.className = 'text-muted';
    msg.textContent = modRes.status === 403 ? 'No access.' : 'API access could not be loaded.';
    panel.appendChild(msg);
    return;
  }
  const mods = rowsOf(await modRes.json(), 'modules');
  const { txnPagedSection, txnDone } = txn;

  // ---- list ---------------------------------------------------------------
  const list = document.createElement('div');
  list.className = 'table-panel api-pad';
  list.innerHTML = `<div class="api-bar">
      ${isAdmin ? '<select class="form-select" id="api-f-scope" aria-label="Whose keys"><option value="mine">Mine</option><option value="all">All</option></select>' : ''}
      <select class="form-select" id="api-f-status" aria-label="Status"><option value="">Any status</option><option value="active">Active</option><option value="expiring">Expires soon</option><option value="owner_inactive">Owner inactive</option><option value="rotating">Rotating</option><option value="pending_claim">Waiting for developer</option><option value="expired">Expired</option><option value="revoked">Revoked</option></select></div>
    <div id="api-att"></div>
    <div id="api-list-body"><p class="api-muted">Loading…</p></div>`;
  let keys = [];
  let attention = [];

  async function refreshList() {
    const scope = isAdmin ? list.querySelector('#api-f-scope').value : 'mine';
    const status = list.querySelector('#api-f-status').value;
    const [kr, ar] = await Promise.all([call(`/keys?scope=${scope}${status ? `&status=${encodeURIComponent(status)}` : ''}`), call('/attention')]);
    const body = list.querySelector('#api-list-body');
    if (!kr) return;
    if (!kr.ok) { body.innerHTML = ''; await showApiError(kr, 'API access could not be loaded.'); return; }
    keys = rowsOf(await kr.json(), 'keys', 'items');
    attention = ar && ar.ok ? rowsOf(await ar.json().catch(() => []), 'items', 'attention') : [];
    const att = list.querySelector('#api-att');
    att.innerHTML = attentionHTML(attention);
    bindFix(att, attention, refreshList);
    const count = section.querySelector('.txn-count');
    if (count) count.textContent = String(keys.length);
    body.innerHTML = keys.length === 0 ? '<p class="api-muted">No API access yet.</p>'
      : `<div class="table-wrapper"><table><thead><tr><th>Name</th><th>Modules</th><th>Status</th><th>Last use</th><th>Today</th></tr></thead><tbody>${keys.map(k => `<tr>
        <td><button type="button" class="api-link" data-open="${esc(k.id)}">${esc(k.name)}</button><div class="api-muted">${esc(k.owner_name || '')}</div></td>
        <td>${chipsOf(k, mods)}</td><td>${statusBadge(k)}</td><td>${esc(ago(k.last_used_at))}</td>
        <td>${esc(k.calls_today ?? 0)} calls${(k.errors_today || 0) > 0 ? ` · <span class="api-bad">${esc(k.errors_today)} errors</span>` : ''}</td></tr>`).join('')}</tbody></table></div>`;
  }
  list.addEventListener('change', (e) => { if (e.target.closest('#api-f-scope, #api-f-status')) refreshList(); });
  list.addEventListener('click', (e) => {
    const open = e.target.closest('[data-open]');
    if (open) openDetail(open.dataset.open);
  });

  // ---- New API access -----------------------------------------------------
  const form = document.createElement('div');
  form.className = 'table-panel api-pad';
  form.innerHTML = `<div class="api-row">
      <div class="form-group"><label class="form-label" for="api-n-name">Name</label><input id="api-n-name" class="form-input" maxlength="80" placeholder="Shopify sync"></div>
      <div class="form-group"><label class="form-label" for="api-n-quick">Start from</label><select id="api-n-quick" class="form-select">${QUICK.map(([v, l]) => `<option value="${v}"${v === 'custom' ? ' selected' : ''}>${esc(l)}</option>`).join('')}</select></div></div>
    <div id="api-n-grid"></div>
    <div class="api-row">
      <div class="form-group"><label class="form-label" for="api-n-mode">Sign-in</label><select id="api-n-mode" class="form-select"><option value="api_key">Secret key</option><option value="oauth_client">App login (OAuth)</option></select></div>
      <div class="form-group"><label class="form-label" for="api-n-exp">Expires</label><select id="api-n-exp" class="form-select">${EXPIRY.map(([v, l]) => `<option value="${v}"${v === '1y' ? ' selected' : ''}>${l}</option>`).join('')}</select>
        <input id="api-n-date" class="form-input" type="date" hidden aria-label="Expiry date"></div>
      <div class="form-group"><label class="form-label">Stores</label>
        <details class="api-fine"><summary id="api-n-stores-sum">All stores</summary><div class="api-fine-list" id="api-n-stores">${stores.map(s => `<label class="api-check"><input type="checkbox" value="${esc(s.code)}" data-store> ${esc(s.name)}</label>`).join('') || '<span class="api-muted">No stores.</span>'}</div></details></div>
      <div class="form-group"><label class="form-label" for="api-n-ip">IPs</label><input id="api-n-ip" class="form-input" placeholder="Any"></div></div>
    <label class="api-check"><input type="checkbox" id="api-n-dev"> Send to developer instead</label>
    <div class="api-summary" id="api-n-sum"></div>
    <button type="button" class="btn btn-primary" id="api-n-create">Create</button>`;
  const grid = buildModuleGrid(form.querySelector('#api-n-grid'), mods, {}, updateSummary);

  const q = (sel) => form.querySelector(sel);
  const pickedStores = () => [...form.querySelectorAll('[data-store]:checked')].map(c => c.value);

  function updateSummary() {
    const names = pickedStores();
    q('#api-n-stores-sum').textContent = names.length ? `${names.length} store${names.length === 1 ? '' : 's'}` : 'All stores';
    q('#api-n-date').hidden = q('#api-n-exp').value !== 'date';
    const until = expiryDate(q('#api-n-exp').value, q('#api-n-date').value);
    q('#api-n-sum').textContent = [q('#api-n-name').value.trim() || 'New key', summaryOf(grid.read(), mods),
      names.length ? `${names.length} store${names.length === 1 ? '' : 's'}` : 'All stores', until ? `Until ${fmtDay(until)}` : 'Never expires'].join(' · ');
  }
  form.addEventListener('input', updateSummary);
  form.addEventListener('change', updateSummary);
  q('#api-n-quick').addEventListener('change', (e) => {
    const [, label, preset] = QUICK.find(x => x[0] === e.target.value);
    const next = {};
    if (preset === 'all-view') mods.forEach(m => { if (grid.canGive(m.key, 'read')) next[m.key] = 'read'; });
    else Object.entries(preset).forEach(([k, lvl]) => {
      if (!grid.has(k)) return;
      if (grid.canGive(k, lvl)) next[k] = lvl;
      else if (lvl === 'write' && grid.canGive(k, 'read')) next[k] = 'read';
    });
    grid.setLevels(next);
    const name = q('#api-n-name');
    if (!name.value.trim() || QUICK.some(x => x[1] === name.value)) name.value = e.target.value === 'custom' ? '' : label;
    updateSummary();
  });
  updateSummary();

  q('#api-n-create').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Creating…', async () => {
    const name = q('#api-n-name').value.trim();
    const sel = grid.read();
    if (!name) { showToast('Give it a name.'); q('#api-n-name').focus(); return; }
    if (!Object.keys(sel.modules).length) { showToast('Turn on a module.'); return; }
    const exp = q('#api-n-exp').value;
    if (exp === 'date' && !q('#api-n-date').value) { showToast('Pick a date.'); return; }
    const deliver = q('#api-n-dev').checked ? 'developer_link' : 'show';
    const payload = {
      name, quick_start: q('#api-n-quick').value, modules: sel.modules, doctypes: sel.doctypes, can_approve: sel.can_approve,
      auth_mode: q('#api-n-mode').value, expires: exp === 'date' ? q('#api-n-date').value : exp,
      stores: pickedStores(), ip_allow: q('#api-n-ip').value.split(/[,\s]+/).filter(Boolean), deliver,
    };
    const r = await call('/keys', 'POST', payload);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The key was not made.'); return; }
    showCreated(await r.json(), { name });
  }));

  // ---- Created, Detail, Docs panes ---------------------------------------
  const created = document.createElement('div');
  const detail = document.createElement('div');
  const docs = document.createElement('div');
  const docsBtn = '<button type="button" class="btn btn-outline txn-landing-only" id="api-docs-open">Docs</button>';
  const section = txnPagedSection({ id: SCREEN, title: 'API access', page: 'New API access', count: 0,
    newLabel: 'New API access', form, list, extra: docsBtn });
  [['created', created], ['detail', detail], ['docs', docs]].forEach(([pane, el]) => {
    const p = document.createElement('div');
    p.dataset.pane = pane;
    p.className = 'hidden';
    p.appendChild(el);
    section.appendChild(p);
  });
  panel.appendChild(section);

  const setTitle = (text) => {
    const h = section.querySelector('.txn-page-only h1.txn-title');
    if (h) h.textContent = text;
  };
  const go = (pane, title) => { setTitle(title); window.txnShowPane(`${SCREEN}:${pane}`); };
  const toList = () => { window.txnShowPane(`${SCREEN}:list`); refreshList(); };

  let saved = true; // the Created page holds the Back button until the key is saved
  section.addEventListener('click', (e) => {
    const crumb = e.target.closest('.txn-crumb');
    if (!crumb) return;
    if (!saved) { e.stopPropagation(); e.preventDefault(); showToast('Tick "I\'ve saved the key" first.'); return; }
    refreshList();
  }, true);

  function showCreated(body, info = {}) {
    const cred = body.credential;
    const id = (cred && typeof cred === 'object' ? cred.id : cred) || info.id || '';
    if (body.developer_link) {
      saved = true;
      const link = body.developer_link;
      const full = /^https?:/.test(link.url) ? link.url : location.origin + link.url;
      created.className = 'table-panel api-pad';
      created.innerHTML = `<p class="api-muted">One use. Valid until ${esc(link.expires_at ? new Date(link.expires_at).toLocaleString() : '48 hours')}.</p>
        <div class="api-keybox"><span class="api-mono">${esc(full)}</span><button type="button" class="btn btn-primary btn-sm" data-copy-link>Copy</button>
        <a class="btn btn-outline btn-sm" href="mailto:?subject=${encodeURIComponent('Your API key')}&body=${encodeURIComponent(full)}">Email</a></div>
        <div class="api-actions"><button type="button" class="btn btn-primary" data-done>Done</button></div>`;
      created.querySelector('[data-copy-link]').addEventListener('click', async () => {
        const { copyText } = await import('./api-docs-viewer.js');
        showToast(await copyText(full) ? 'Copied.' : 'Select and copy.');
      });
      created.querySelector('[data-done]').addEventListener('click', () => { txnDone(SCREEN); refreshList(); });
      go('created', 'Link ready');
      return;
    }
    saved = false;
    created.className = 'table-panel api-pad';
    created.innerHTML = `<div data-key></div>
      <div class="api-h2">Share</div><div data-share></div>
      <label class="api-check"><input type="checkbox" id="api-saved"> I've saved the key</label>
      <div class="api-actions"><button type="button" class="btn btn-primary" id="api-created-done" disabled>Done</button></div>`;
    renderKeyOnce(created.querySelector('[data-key]'), body);
    if (id) mountShareDocs(created.querySelector('[data-share]'), id);
    const tick = created.querySelector('#api-saved');
    const done = created.querySelector('#api-created-done');
    tick.addEventListener('change', () => { saved = tick.checked; done.disabled = !tick.checked; });
    done.addEventListener('click', () => {
      saved = true;
      created.innerHTML = ''; // the secret leaves the page
      txnDone(SCREEN);
      refreshList();
    });
    go('created', info.rotated ? 'New key' : 'API access created');
  }

  async function openDetail(id) {
    const row = keys.find(k => String(k.id) === String(id));
    go('detail', row ? row.name : 'API access');
    await renderKeyDetail(detail, id, ctx);
  }
  const ctx = { mods, isAdmin, onChanged: (kid) => { renderKeyDetail(detail, kid, ctx); refreshList(); }, onClosed: toList, showCreated };

  section.querySelector('#api-docs-open').addEventListener('click', async () => {
    go('docs', 'Docs');
    if (docs.dataset.loaded) return;
    docs.innerHTML = '<p class="api-muted">Loading…</p>';
    const r = await call('/openapi.json');
    if (!r) return;
    if (!r.ok) { docs.innerHTML = ''; await showApiError(r, 'The docs could not be loaded.'); return; }
    docs.dataset.loaded = '1';
    docs.className = 'table-panel api-pad';
    renderApiDocs(docs, await r.json(), { tenant: localStorage.getItem('erp_tenant_id') || '' });
  });

  await refreshList();
}

// 205.20: the Needs-attention tile on Home. Shown only when something needs
// attention, and only to someone who may see API access. host: Home's stat row.
async function mountApiAccessAttention(host) {
  if (!canUseApiAccess()) return;
  const r = await call('/attention');
  if (!r || !r.ok) return;
  const items = rowsOf(await r.json().catch(() => []), 'items', 'attention');
  if (!items.length) return;
  const card = document.createElement('div');
  card.className = 'stat-card';
  card.style.cursor = 'pointer';
  card.innerHTML = `<div class="stat-info"><span class="stat-val">${items.length}</span><span class="stat-label">API access need${items.length === 1 ? 's' : ''} attention</span></div>`;
  const open = () => { setActiveMenu('menu-api-access'); renderView('api-access'); };
  if (typeof makeClickable === 'function') makeClickable(card, open); else card.addEventListener('click', open);
  host.appendChild(card);
}

export { renderAPIAccessPanel, mountApiAccessAttention, BASE };
