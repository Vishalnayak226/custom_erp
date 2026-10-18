// Stage 79 (user, 2026-10-18: "Role assignment is bad ... I will go inside role
// and browse and give access. All module will be shown and I will select at
// one time what to give and what access to give."): the Roles screen. It opens
// on the role list; a role opens its own page listing every module, each one
// expandable to its record types, with Full / Read / Create / Update / Delete
// ticks and one Save for the lot. New Role is a page of its own (txn-screen.js,
// list first). Moved out of the cold-core app.js, where the old one-grant-at-a-
// time form lived. Shared shell services (apiFetch, state, getDoctypeLabel,
// isAdminRoleName, showToast ...) resolve from the classic shell.

const { txnPageTitle, txnNewButton, txnRestorePane, txnPageOf } = await import('./txn-screen.js' + new URL(import.meta.url).search);

if (!document.getElementById('view-roles-css')) {
  const style = document.createElement('style');
  style.id = 'view-roles-css';
  style.textContent = `
.roles-list-table tbody tr { cursor: pointer; }
.roles-list-table .roles-muted, .roles-count { color: var(--text-muted); font-weight: 400; }
.roles-badge { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; background: var(--success-soft-bg); color: var(--success-strong); }
.roles-toolbar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 12px 16px; border-bottom: 1px solid var(--border-color); }
.roles-toolbar .form-input { width: 260px; max-width: 100%; }
.roles-toolbar .roles-held { margin-left: auto; font-size: 13px; color: var(--text-muted); }
.roles-matrix-wrap { overflow-x: auto; }
.roles-matrix th.roles-col, .roles-matrix td.roles-col { text-align: center; width: 96px; }
.roles-matrix input[type="checkbox"] { width: 16px; height: 16px; cursor: pointer; vertical-align: middle; }
.roles-matrix tr.roles-mod td { background: var(--surface-subtle); font-weight: 600; }
.roles-matrix tr.roles-dt td:first-child { padding-left: 44px; }
.roles-caret { background: none; border: 0; padding: 0; color: inherit; font: inherit; cursor: pointer; display: inline-flex; align-items: center; gap: 8px; text-align: left; }
.roles-caret::before { content: '\\25B8'; display: inline-block; width: 12px; transition: transform var(--transition-fast); }
.roles-caret[aria-expanded="true"]::before { transform: rotate(90deg); }
.roles-savebar { position: sticky; bottom: 0; z-index: 2; display: flex; gap: 8px; align-items: center; justify-content: flex-end; flex-wrap: wrap; padding: 12px 16px; background: var(--panel-bg); border-top: 1px solid var(--border-color); }
.roles-savebar-note { margin-right: auto; font-size: 13px; color: var(--text-muted); }
.roles-savebar-note.dirty { color: var(--warning-strong); font-weight: 600; }
.roles-new-form { padding: 24px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; }
.roles-new-form .form-group { margin-bottom: 0; }
`;
  document.head.appendChild(style);
}

const SCREEN = 'roles-screen';
const COLS = [['r', 'Read'], ['c', 'Create'], ['u', 'Update'], ['d', 'Delete']];

// Survives a re-render of the screen (a refresh of the view, not of the page).
let selectedRole = '';
let saved = new Map();   // doctype -> {r,c,u,d} as stored
let draft = new Map();   // doctype -> {r,c,u,d} as ticked
let expanded = new Set();
let filterText = '';
let grantsByRole = new Map();
let usersByRole = new Map();

const blank = () => ({ r: false, c: false, u: false, d: false });
const same = (a, b) => COLS.every(([k]) => !!a[k] === !!b[k]);
const anyRight = g => COLS.some(([k]) => g[k]);

// Every record type the tenant has, grouped by module, in label order.
function moduleGroups() {
  const groups = new Map();
  for (const d of state.activeDoctypes || []) {
    const mod = d.module || 'Other';
    if (!groups.has(mod)) groups.set(mod, []);
    groups.get(mod).push({ name: d.name, label: getDoctypeLabel(d.name) });
  }
  for (const list of groups.values()) list.sort((a, b) => a.label.localeCompare(b.label));
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function dirtyNames() {
  const out = [];
  for (const [name, g] of draft) if (!same(g, saved.get(name) || blank())) out.push(name);
  return out;
}

function summarise(role) {
  const grants = grantsByRole.get(role) || new Map();
  const mods = new Set();
  let types = 0;
  for (const d of state.activeDoctypes || []) {
    const g = grants.get(d.name);
    if (g && anyRight(g)) { types++; mods.add(d.module || 'Other'); }
  }
  return { mods: mods.size, types };
}

export async function renderRolesView(container) {
  const [permsRes, rolesRes, usersRes, storesRes] = await Promise.all([
    apiFetch('/api/v1/admin/role-permissions'),
    apiFetch('/api/v1/admin/roles'),
    apiFetch('/api/v1/admin/users'),
    apiFetch('/api/v1/admin/roles/stores')
  ]);
  if (!permsRes || !rolesRes) return;
  if (!permsRes.ok || !rolesRes.ok) { renderErrorPanel(container, 'Failed to load roles.', () => renderView('roles')); return; }
  const grants = await permsRes.json();
  const roles = await rolesRes.json();
  const users = usersRes && usersRes.ok ? await usersRes.json().catch(() => []) : [];
  // Stage 85.A1: each role's store access, for the list's Stores column.
  storesByRole = storesRes && storesRes.ok ? ((await storesRes.json().catch(() => ({}))).roles || {}) : {};
  storeModule = await loadViewModule('/view-store.js?v=1').catch(() => null);

  grantsByRole = new Map();
  for (const g of grants) {
    if (!grantsByRole.has(g.role)) grantsByRole.set(g.role, new Map());
    grantsByRole.get(g.role).set(g.doctype_name, { r: !!g.allow_read, c: !!g.allow_create, u: !!g.allow_update, d: !!g.allow_delete });
  }
  usersByRole = new Map();
  for (const u of users) {
    if (!usersByRole.has(u.role)) usersByRole.set(u.role, []);
    usersByRole.get(u.role).push(u.username);
  }
  // Built-in full-access roles first, then the rest by name.
  const ordered = [...roles].sort((a, b) => (isAdminRoleName(b) - isAdminRoleName(a)) || a.localeCompare(b));

  const section = document.createElement('div');
  section.id = SCREEN;
  section.className = 'txn-paged txn-on-landing';
  section.dataset.landing = 'list';
  section.innerHTML = `
    <div class="page-section-bar">
      ${txnPageTitle({ screen: SCREEN, title: 'Roles', page: '<span id="roles-page-name">New Role</span>', count: roles.length, landing: 'list' })}
      <span class="page-section-actions txn-landing-only">${txnNewButton({ screen: SCREEN, label: 'New Role' })}</span>
    </div>
    <div data-pane="list"><div class="table-panel" id="roles-list"></div></div>
    <div data-pane="new" class="hidden">
      <div class="table-panel">
        <form class="roles-new-form" id="roles-new-form" novalidate>
          <div class="form-group">
            <label class="form-label" for="new-role-name">Role name<span class="required">*</span></label>
            <input type="text" id="new-role-name" class="form-input" style="width: 220px;" placeholder="e.g. Purchase Clerk" maxlength="60" autocomplete="off">
          </div>
          <div class="form-group" style="flex: 1; min-width: 220px;">
            <label class="form-label" for="new-role-description">Description</label>
            <input type="text" id="new-role-description" class="form-input" placeholder="What this role is for" autocomplete="off">
          </div>
          <button class="btn btn-primary" type="submit">Create Role</button>
        </form>
        <div id="new-role-error" class="login-error hidden" style="margin: 0 24px 24px;"></div>
      </div>
    </div>
    <div data-pane="access" class="hidden"><div class="table-panel" id="roles-access"></div></div>
  `;
  container.appendChild(section);

  drawList(ordered);
  section.querySelector('#roles-new-form').addEventListener('submit', e => { e.preventDefault(); createRole(); });

  // Leaving a role's page with ticks not saved asks first. The breadcrumb is a
  // data-act button; the shell dispatches those on document in the bubble
  // phase, so stopping the click here keeps it from leaving.
  section.addEventListener('click', async e => {
    if (!e.target.closest('.txn-crumb') || !section.querySelector('[data-pane="access"]:not(.hidden)') || !dirtyNames().length) return;
    e.stopPropagation();
    if (await showCustomConfirm('The access you ticked on this role is not saved yet. Leave without saving?', 'Unsaved changes')) {
      draft = cloneGrants(saved);
      window.txnShowPane(`${SCREEN}:list`);
    }
  }, true);

  // A refresh on a role's page comes back on the list unless that role is
  // still known (the module outlives a re-render, not a page reload).
  if (txnPageOf(SCREEN, 'list') === 'access') {
    if (selectedRole && roles.includes(selectedRole)) openRole(selectedRole, true);
    else { selectedRole = ''; history.replaceState({ ...history.state, txnPane: '' }, '', location.href); }
  }
  txnRestorePane(SCREEN);
}

function drawList(ordered) {
  const host = document.getElementById('roles-list');
  if (!host) return;
  if (!ordered.length) {
    host.innerHTML = `<div class="empty-state-hint" style="padding: 24px;">No roles yet. Use <b>+ New Role</b>.</div>`;
    return;
  }
  host.innerHTML = `
    <table class="roles-list-table">
      <thead><tr><th>Role</th><th>Access</th><th>Stores</th><th>Users</th><th></th></tr></thead>
      <tbody>${ordered.map(role => {
        const admin = isAdminRoleName(role);
        const { mods, types } = summarise(role);
        const holders = usersByRole.get(role) || [];
        const access = admin ? '<span class="roles-badge">Full access (built in)</span>'
          : types ? `${mods} module${mods === 1 ? '' : 's'} <span class="roles-muted">· ${types} record type${types === 1 ? '' : 's'}</span>`
          : '<span class="roles-muted">No access yet</span>';
        const who = holders.length ? `${holders.length} <span class="roles-muted">· ${escapeHTMLText(holders.slice(0, 3).join(', '))}${holders.length > 3 ? ', …' : ''}</span>` : '<span class="roles-muted">None</span>';
        return `<tr data-role="${escapeHTMLText(role)}"${admin ? ' data-admin="1"' : ''}>
          <td style="font-weight: 600;">${escapeHTMLText(role)}</td>
          <td>${access}</td>
          <td>${admin ? 'All stores' : escapeHTMLText(roleStoresText(role))}</td>
          <td>${who}</td>
          <td style="text-align: right;">${admin ? '' : `<button type="button" class="btn btn-outline btn-sm" data-role-stores>Stores</button> <button type="button" class="btn btn-outline btn-sm">Set access</button>`}</td>
        </tr>`;
      }).join('')}</tbody>
    </table>`;
  host.querySelectorAll('tbody tr').forEach(tr => tr.addEventListener('click', e => {
    if (e.target.closest('[data-role-stores]')) { editRoleStores(tr.dataset.role); return; }
    if (tr.dataset.admin) { showToast(`${tr.dataset.role} has full access to everything - there is nothing to set.`, { variant: 'info' }); return; }
    openRole(tr.dataset.role);
  }));
}

// Stage 85.A1 (user: "First role than user"): a role's store access - All
// stores (new ones included), Selected, or each user's home store only.
let storesByRole = {};
let storeModule = null;
function roleStoresText(role) {
  const s = storesByRole[role];
  if (!s) return 'Home store only';
  return storeModule ? storeModule.storeAccessSummary(s.access, s.stores) : s.access;
}
async function editRoleStores(role) {
  if (!storeModule) return;
  await storeModule.openStoreAccessEditor({ kind: 'role', id: role, title: `Stores - ${role}`, onSaved: () => renderView('roles') });
}

function cloneGrants(map) {
  const out = new Map();
  for (const [k, v] of map) out.set(k, { ...v });
  return out;
}

// openRole shows a role's access page. restoring = the page is already the
// current history entry (a re-render), so it only redraws.
function openRole(role, restoring = false) {
  selectedRole = role;
  saved = cloneGrants(grantsByRole.get(role) || new Map());
  draft = cloneGrants(saved);
  expanded = new Set();
  filterText = '';
  const nameEl = document.getElementById('roles-page-name');
  if (nameEl) nameEl.textContent = role;
  drawAccess();
  if (!restoring) window.txnShowPane(`${SCREEN}:access`);
  document.getElementById('roles-access')?.scrollIntoView({ block: 'start' });
}

function drawAccess() {
  const host = document.getElementById('roles-access');
  if (!host) return;
  const holders = usersByRole.get(selectedRole) || [];
  host.innerHTML = `
    <div class="roles-toolbar">
      <input type="search" class="form-input" id="roles-filter" placeholder="Find a module or record type" aria-label="Find a module or record type" value="${escapeHTMLText(filterText)}" autocomplete="off">
      <button type="button" class="btn btn-outline btn-sm" id="roles-expand">Expand all</button>
      <button type="button" class="btn btn-outline btn-sm" id="roles-collapse">Collapse all</button>
      <button type="button" class="btn btn-outline btn-sm" id="roles-stores" title="Which stores holders of this role work at">Stores: ${escapeHTMLText(roleStoresText(selectedRole))}</button>
      <span class="roles-held">${holders.length ? `Held by ${holders.length} user${holders.length === 1 ? '' : 's'}: ${escapeHTMLText(holders.slice(0, 5).join(', '))}${holders.length > 5 ? ', …' : ''}` : 'No user holds this role yet - assign it on the Users screen.'}</span>
    </div>
    <div class="roles-matrix-wrap">
      <table class="roles-matrix">
        <thead><tr><th>Module / record type</th><th class="roles-col">Full access</th>${COLS.map(([, l]) => `<th class="roles-col">${l}</th>`).join('')}</tr></thead>
        <tbody id="roles-matrix-body"></tbody>
      </table>
    </div>
    <div class="roles-savebar">
      <span class="roles-savebar-note" id="roles-dirty-note"></span>
      <button type="button" class="btn btn-outline" id="roles-discard">Discard changes</button>
      <button type="button" class="btn btn-primary" id="roles-save">Save access</button>
    </div>`;

  const filterEl = host.querySelector('#roles-filter');
  filterEl.addEventListener('input', () => { filterText = filterEl.value; drawRows(); });
  host.querySelector('#roles-expand').addEventListener('click', () => { expanded = new Set(moduleGroups().map(([m]) => m)); drawRows(); });
  host.querySelector('#roles-collapse').addEventListener('click', () => { expanded = new Set(); drawRows(); });
  host.querySelector('#roles-discard').addEventListener('click', () => { draft = cloneGrants(saved); syncTicks(); });
  host.querySelector('#roles-save').addEventListener('click', saveAccess);
  host.querySelector('#roles-stores').addEventListener('click', () => editRoleStores(selectedRole));

  const body = host.querySelector('#roles-matrix-body');
  body.addEventListener('change', e => {
    const box = e.target;
    if (!(box instanceof HTMLInputElement) || !box.dataset.col) return;
    const names = box.dataset.dt ? [box.dataset.dt] : (moduleGroups().find(([m]) => m === box.dataset.mod)?.[1] || []).map(t => t.name);
    for (const name of names) setRight(name, box.dataset.col, box.checked);
    syncTicks();
  });
  body.addEventListener('click', e => {
    const caret = e.target.closest('.roles-caret');
    if (!caret) return;
    const mod = caret.dataset.mod;
    if (expanded.has(mod)) expanded.delete(mod); else expanded.add(mod);
    drawRows();
    document.querySelector(`.roles-caret[data-mod="${CSS.escape(mod)}"]`)?.focus();
  });
  drawRows();
}

// setRight applies one tick. Create, Update and Delete need Read, so ticking
// one of them ticks Read, and clearing Read clears them; Full is all four.
function setRight(name, col, on) {
  const g = { ...(draft.get(name) || blank()) };
  if (col === 'all') COLS.forEach(([k]) => { g[k] = on; });
  else {
    g[col] = on;
    if (on && col !== 'r') g.r = true;
    if (!on && col === 'r') { g.c = false; g.u = false; g.d = false; }
  }
  draft.set(name, g);
}

function drawRows() {
  const body = document.getElementById('roles-matrix-body');
  if (!body) return;
  const q = filterText.trim().toLowerCase();
  const tick = (attrs, label) => `<input type="checkbox" ${attrs} aria-label="${escapeHTMLText(label)}">`;
  const rowTicks = (attrs, label) =>
    `<td class="roles-col">${tick(`${attrs} data-col="all"`, `${label}: full access`)}</td>` +
    COLS.map(([k, l]) => `<td class="roles-col">${tick(`${attrs} data-col="${k}"`, `${label}: ${l}`)}</td>`).join('');
  let html = '';
  for (const [mod, types] of moduleGroups()) {
    const modHit = !q || mod.toLowerCase().includes(q);
    const shown = modHit ? types : types.filter(t => t.label.toLowerCase().includes(q) || t.name.toLowerCase().includes(q));
    if (!shown.length) continue;
    const open = expanded.has(mod) || (!!q && !modHit);
    const modAttr = `data-mod="${escapeHTMLText(mod)}"`;
    html += `<tr class="roles-mod"><td><button type="button" class="roles-caret" ${modAttr} aria-expanded="${open}">${escapeHTMLText(mod)} <span class="roles-count" data-mod-count="${escapeHTMLText(mod)}"></span></button></td>${rowTicks(modAttr, `${mod} (whole module)`)}</tr>`;
    if (open) {
      for (const t of shown) {
        html += `<tr class="roles-dt"><td>${escapeHTMLText(t.label)}</td>${rowTicks(`data-dt="${escapeHTMLText(t.name)}"`, t.label)}</tr>`;
      }
    }
  }
  body.innerHTML = html || `<tr><td colspan="${COLS.length + 2}" class="roles-muted" style="text-align: center;">Nothing matches "${escapeHTMLText(filterText)}".</td></tr>`;
  syncTicks();
}

// syncTicks puts every box on the page in step with the draft: a record
// type's own rights, a module's box ticked when all its record types have that
// right and half-ticked when some do, and the save bar's count.
function syncTicks() {
  const groups = new Map(moduleGroups());
  document.querySelectorAll('#roles-matrix-body input[type="checkbox"]').forEach(box => {
    const col = box.dataset.col;
    const has = name => {
      const g = draft.get(name) || blank();
      return col === 'all' ? COLS.every(([k]) => g[k]) : !!g[col];
    };
    if (box.dataset.dt) {
      box.checked = has(box.dataset.dt);
      box.indeterminate = false;
    } else {
      const names = (groups.get(box.dataset.mod) || []).map(t => t.name);
      const n = names.filter(has).length;
      box.checked = n > 0 && n === names.length;
      box.indeterminate = n > 0 && n < names.length;
    }
  });
  document.querySelectorAll('#roles-matrix-body [data-mod-count]').forEach(el => {
    const names = (groups.get(el.dataset.modCount) || []).map(t => t.name);
    const n = names.filter(name => anyRight(draft.get(name) || blank())).length;
    el.textContent = `${n} of ${names.length}`;
  });
  const changed = dirtyNames().length;
  const note = document.getElementById('roles-dirty-note');
  if (note) {
    note.textContent = changed ? `${changed} record type${changed === 1 ? '' : 's'} changed - not saved yet` : 'All changes saved';
    note.classList.toggle('dirty', !!changed);
  }
  const saveBtn = document.getElementById('roles-save');
  const discardBtn = document.getElementById('roles-discard');
  if (saveBtn) saveBtn.disabled = !changed;
  if (discardBtn) discardBtn.disabled = !changed;
}

async function saveAccess() {
  const names = dirtyNames();
  if (!names.length) return;
  const saveBtn = document.getElementById('roles-save');
  if (saveBtn) saveBtn.disabled = true;
  const grants = names.map(name => {
    const g = draft.get(name);
    return { doctype_name: name, allow_read: g.r, allow_create: g.c, allow_update: g.u, allow_delete: g.d };
  });
  const res = await apiFetch('/api/v1/admin/role-permissions', { method: 'POST', body: JSON.stringify({ role: selectedRole, grants }) });
  if (!res) { syncTicks(); return; }
  if (!res.ok) {
    await showApiError(res, 'Failed to save the access.');
    syncTicks();
    return;
  }
  saved = cloneGrants(draft);
  grantsByRole.set(selectedRole, cloneGrants(draft));
  syncTicks();
  showToast(`${selectedRole}: access saved for ${names.length} record type${names.length === 1 ? '' : 's'}. Users holding it see the change at their next sign-in or refresh.`, { variant: 'success' });
  const roles = [...document.querySelectorAll('#roles-list tbody tr')].map(tr => tr.dataset.role);
  drawList(roles);
}

async function createRole() {
  const nameEl = document.getElementById('new-role-name');
  const errorEl = document.getElementById('new-role-error');
  errorEl.classList.add('hidden');
  const name = nameEl.value.trim();
  if (!name) { errorEl.textContent = 'Enter a role name.'; errorEl.classList.remove('hidden'); nameEl.focus(); return; }
  const res = await apiFetch('/api/v1/admin/roles', {
    method: 'POST',
    body: JSON.stringify({ name, description: document.getElementById('new-role-description').value.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to create the role.');
    errorEl.classList.remove('hidden');
    return;
  }
  const data = await res.json().catch(() => ({}));
  const created = data.name || name;
  nameEl.value = '';
  document.getElementById('new-role-description').value = '';
  showToast(`Role "${created}" created. Tick what it may use, then Save access.`, { variant: 'success' });
  // Straight on to the new role's access - the next thing to do with it.
  const rows = [...document.querySelectorAll('#roles-list tbody tr')].map(tr => tr.dataset.role);
  if (!rows.includes(created)) rows.push(created);
  drawList(rows.sort((a, b) => (isAdminRoleName(b) - isAdminRoleName(a)) || a.localeCompare(b)));
  openRole(created);
}
