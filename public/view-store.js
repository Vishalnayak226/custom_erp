// Stage 85.A5 (user, 2026-10-18: "Store needs to be Top we discussed"): the
// working store in the top bar.
//
// A user with one store sees its name. A user with more gets a picker of the
// stores an administrator has given them (role first, then user -
// engines/store_access.go). A switch is the server's decision: it checks the
// store is allowed and that no till session is open here, then re-issues the
// session for the new store. The browser never just changes a value, so
// numbering, list scoping and the till all agree with what the bar says.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const STORE_ICON = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 9l1.5-5h15L21 9"/><path d="M4 9v11h16V9"/><path d="M9 20v-6h6v6"/></svg>';

let stores = [];
let myStores = null;
// Stage 106.6: "All stores" in the picker (canViewAll) and whether the lists
// show every store now (viewAll) - picking a store narrows them to it.
let canViewAll = false;
let viewAll = false;

// loadMyStores is the server's answer to "which stores, and which one now",
// one request shared by the top bar and any screen that needs the store
// before the bar has drawn (a deep link). fresh = after a switch.
function loadMyStores(fresh) {
  if (!myStores || fresh) {
    myStores = apiFetch('/api/v1/me/stores').then(r => (r && r.ok ? r.json() : null)).catch(() => null).then(data => {
      if (!data) { myStores = null; return null; }
      stores = Array.isArray(data.stores) ? data.stores : [];
      canViewAll = !!data.can_view_all;
      viewAll = !!data.view_all;
      window.erpWorkingStore = stores.find(s => s.code === data.working) || { code: data.working || '', name: data.working || '' };
      return data;
    });
  }
  return myStores;
}

// ensureWorkingStore is the working store {code, name}, from the server.
export async function ensureWorkingStore() {
  if (!window.erpWorkingStore) await loadMyStores(false);
  return window.erpWorkingStore || { code: '', name: '' };
}

// mountStoreSwitch (re)draws the top-bar store. Called at start-up and after
// every switch.
export async function mountStoreSwitch(fresh) {
  const host = document.getElementById('store-switch');
  if (!host) return;
  const [data] = await Promise.all([loadMyStores(fresh), loadViewStyles('store.css')]);
  if (!data) return;
  const working = window.erpWorkingStore;
  const name = viewAll ? 'All stores' : esc(working.name || working.code);
  // A chip drawn before the name arrived shows the code; give it the name.
  document.querySelectorAll('.txn-ctx-store').forEach(el => { el.textContent = working.name || working.code; });
  // A screen drawn before the server's answer (the till) re-checks its store.
  document.dispatchEvent(new CustomEvent('erp:working-store', { detail: working }));
  if (stores.length > 1 || canViewAll) {
    host.innerHTML = `<button type="button" class="btn btn-outline store-switch-btn" id="store-switch-btn" aria-haspopup="dialog" aria-expanded="false" title="${viewAll ? `Lists show every store; new documents are made at ${esc(working.name || working.code)}` : 'The store you are working at - click to switch'}">${STORE_ICON}<span class="store-switch-label">${name}</span><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="6 9 12 15 18 9"></polyline></svg></button>`;
    host.querySelector('button').addEventListener('click', toggleStorePicker);
  } else {
    host.innerHTML = `<span class="store-switch-name" title="The store you are working at">${STORE_ICON}<span class="store-switch-label">${name}</span></span>`;
  }
  host.classList.remove('hidden');
}

function closeStorePicker() {
  const pop = document.getElementById('store-switch-pop');
  if (pop) pop.remove();
  const btn = document.getElementById('store-switch-btn');
  if (btn) btn.setAttribute('aria-expanded', 'false');
  document.removeEventListener('mousedown', onOutside, true);
  document.removeEventListener('keydown', onKey, true);
}

function onOutside(e) {
  const host = document.getElementById('store-switch');
  if (host && !host.contains(e.target)) closeStorePicker();
}

function onKey(e) {
  if (e.key === 'Escape') { e.stopPropagation(); closeStorePicker(); document.getElementById('store-switch-btn')?.focus(); }
}

function toggleStorePicker() {
  if (document.getElementById('store-switch-pop')) { closeStorePicker(); return; }
  const host = document.getElementById('store-switch');
  const current = (window.erpWorkingStore || {}).code;
  const pop = document.createElement('div');
  pop.id = 'store-switch-pop';
  pop.className = 'store-switch-pop';
  pop.setAttribute('role', 'dialog');
  pop.setAttribute('aria-label', 'Switch store');
  pop.innerHTML = `
    ${stores.length > 6 ? '<input type="search" class="form-input store-switch-search" placeholder="Find a store" aria-label="Find a store" autocomplete="off">' : ''}
    <div class="store-switch-list" role="listbox">
      ${canViewAll ? `<button type="button" role="option" class="store-switch-opt${viewAll ? ' active' : ''}" aria-selected="${viewAll}" data-store="*">
        <span>All stores</span><span class="store-switch-meta">every store's documents</span></button>` : ''}
      ${stores.map(s => `<button type="button" role="option" class="store-switch-opt${!viewAll && s.code === current ? ' active' : ''}" aria-selected="${!viewAll && s.code === current}" data-store="${esc(s.code)}">
        <span>${esc(s.name || s.code)}</span><span class="store-switch-meta">${esc(s.type || '')}${s.sellable ? ' · sells' : ''}</span></button>`).join('')}
    </div>`;
  host.appendChild(pop);
  document.getElementById('store-switch-btn').setAttribute('aria-expanded', 'true');
  pop.querySelectorAll('.store-switch-opt').forEach(b => b.addEventListener('click', () => switchStore(b.dataset.store)));
  const search = pop.querySelector('.store-switch-search');
  if (search) {
    search.addEventListener('input', () => {
      const q = search.value.trim().toLowerCase();
      pop.querySelectorAll('.store-switch-opt').forEach(b => { b.hidden = q !== '' && !b.textContent.toLowerCase().includes(q); });
    });
    search.focus();
  } else {
    (pop.querySelector('.store-switch-opt.active') || pop.querySelector('.store-switch-opt'))?.focus();
  }
  document.addEventListener('mousedown', onOutside, true);
  document.addEventListener('keydown', onKey, true);
}

async function switchStore(code) {
  closeStorePicker();
  const current = (window.erpWorkingStore || {}).code;
  // 106.6: picking the working store again leaves "All stores" for it.
  if (!code || (code === '*' ? viewAll : code === current && !viewAll)) return;
  const target = code === '*' ? { code, name: 'All stores' } : stores.find(s => s.code === code) || { code, name: code };
  // A half-built bill belongs to the store it was priced at.
  const lines = typeof posCart !== 'undefined' && Array.isArray(posCart) ? posCart.length : 0;
  if (code !== '*' && code !== current && lines > 0 && !(await showCustomConfirm(`The till has ${lines} line(s) in the cart. Switching to ${target.name} clears them - the cart is not saved.\n\nSwitch store?`, 'Switch Store'))) return;

  const res = await apiFetch('/api/v1/me/working-store', { method: 'POST', body: JSON.stringify({ store: code }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not switch store.'); return; }
  const data = await res.json().catch(() => null);
  if (!data || !data.token) return;
  localStorage.setItem('erp_token', data.token);

  // Lists are scoped to the working store on the server, so every cached
  // list belongs to the old one. Collect first, then delete (see Reset).
  try {
    const keys = [];
    for (let i = 0; i < sessionStorage.length; i++) {
      const k = sessionStorage.key(i);
      if (k && k.startsWith(SWR_PREFIX)) keys.push(k);
    }
    keys.forEach(k => sessionStorage.removeItem(k));
  } catch (_) { /* nothing cached, nothing stale */ }
  if (typeof window.clearPOSTillState === 'function') { try { window.clearPOSTillState(); } catch (_) { /* rebuilt below */ } }

  await mountStoreSwitch(true);
  renderView(currentView);
  showToast(code === '*' ? 'Lists show every store again.' : `Working at ${target.name || code}; lists show its documents.`, { variant: 'success', title: 'Store Switched' });
}

// --- Stage 85.A1/85.A2: the Stores editor (Users and Roles screens) ----------
//
// One editor for both, so "what a role gives" and "what a user gets" read the
// same way. kind 'user' adds the home store and "Same as role"; kind 'role'
// has neither. Saves through /api/v1/admin/{users,roles}/stores, which check
// every store exists (STORE-0852) and that Super Admin is not narrowed.

const MODE_TEXT = {
  All: 'All stores',
  Selected: 'Selected stores',
  Home: 'Home store only',
};

// storeAccessSummary is the one-line reading of a setting, for tables.
export function storeAccessSummary(access, stores, roleAccess) {
  const n = Array.isArray(stores) ? stores.length : 0;
  if (access === 'All') return 'All stores';
  if (access === 'Home') return 'Home store only';
  if (access === 'Selected') return `Home + ${n} store${n === 1 ? '' : 's'}`;
  const base = roleAccess ? `Same as role (${(MODE_TEXT[roleAccess] || roleAccess).toLowerCase()})` : 'Same as role';
  return n ? `${base} + ${n} more` : base;
}

export async function openStoreAccessEditor({ kind, id, title, onSaved }) {
  const url = kind === 'user'
    ? `/api/v1/admin/users/stores?id=${encodeURIComponent(id)}`
    : `/api/v1/admin/roles/stores?role=${encodeURIComponent(id)}`;
  const [res] = await Promise.all([apiFetch(url), loadViewStyles('store.css')]);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the stores.'); return; }
  const data = await res.json();
  const s = data.setting || {};
  const all = Array.isArray(data.stores) ? data.stores : [];
  const ticked = new Set(s.stores || []);
  const user = kind === 'user';
  // A Super Admin works at every store: only their home store is theirs to set.
  const admin = user && isAdminRoleName(s.role);
  const roleLine = user ? `${esc(s.role)}: ${esc(storeAccessSummary(s.role_access, s.role_stores))}` : '';
  const modes = (user ? [['Inherit', `Same as role <span class="sa-hint">(${roleLine})</span>`]] : [])
    .concat([['All', 'All stores <span class="sa-hint">- includes stores added later</span>'],
      ['Selected', 'Selected stores <span class="sa-hint">- the ones ticked below</span>'],
      ['Home', `Home store only${user ? '' : ' <span class="sa-hint">- each user&rsquo;s own store</span>'}`]]);
  const current = s.access || (user ? 'Inherit' : 'Home');
  const homeOptions = all.some(o => o.code === s.home) || !s.home ? '' : `<option value="${esc(s.home)}" selected>${esc(s.home)}</option>`;

  document.getElementById('store-access-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'store-access-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 560px;" role="dialog" aria-modal="true" aria-labelledby="store-access-title">
      <div class="modal-header"><h3 class="modal-title" id="store-access-title">${esc(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-sa-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body sa-body">
        ${user ? `<div class="form-group">
          <label class="form-label" for="sa-home">Home store</label>
          <select id="sa-home" class="form-input">${homeOptions}${all.map(o => `<option value="${esc(o.code)}" ${o.code === s.home ? 'selected' : ''}>${esc(o.name)}</option>`).join('')}</select>
        </div>` : ''}
        ${admin ? '<p class="sa-hint">Super Admin works at every store, including stores added later. Only the home store can be set here.</p>' : ''}
        <fieldset class="sa-modes${admin ? ' hidden' : ''}"><legend class="form-label">Store access</legend>
          ${modes.map(([v, label]) => `<label class="sa-mode"><input type="radio" name="sa-mode" value="${v}" ${v === current ? 'checked' : ''}> <span>${label}</span></label>`).join('')}
        </fieldset>
        <div class="sa-list-wrap" id="sa-list-wrap">
          <div class="form-label" id="sa-list-label">Stores</div>
          ${all.length > 8 ? '<input type="search" class="form-input sa-search" id="sa-search" placeholder="Find a store" aria-label="Find a store" autocomplete="off">' : ''}
          <div class="sa-list">${all.length ? all.map(o => `<label class="sa-store"><input type="checkbox" value="${esc(o.code)}" ${ticked.has(o.code) ? 'checked' : ''}> <span>${esc(o.name)}</span><span class="store-switch-meta">${esc(o.type || '')}${o.sellable ? ' · sells' : ''}</span></label>`).join('')
            : '<div class="sa-hint">No stores yet. Add them in Setup &rarr; Location.</div>'}</div>
        </div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-sa-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-sa-save>Save</button></div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  const mode = () => (overlay.querySelector('input[name="sa-mode"]:checked') || {}).value || current;
  const syncList = () => {
    const m = mode();
    overlay.querySelector('#sa-list-wrap').classList.toggle('hidden', admin || m === 'All' || m === 'Home');
    overlay.querySelector('#sa-list-label').textContent = m === 'Inherit' ? 'Extra stores on top of the role (optional)' : 'Stores';
  };
  overlay.querySelectorAll('input[name="sa-mode"]').forEach(r => r.addEventListener('change', syncList));
  syncList();
  const search = overlay.querySelector('#sa-search');
  if (search) search.addEventListener('input', () => {
    const q = search.value.trim().toLowerCase();
    overlay.querySelectorAll('.sa-store').forEach(l => { l.hidden = q !== '' && !l.textContent.toLowerCase().includes(q); });
  });
  overlay.querySelectorAll('[data-sa-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const save = overlay.querySelector('[data-sa-save]');
  save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
    const m = mode();
    const picked = m === 'All' || m === 'Home' ? [] : [...overlay.querySelectorAll('.sa-store input:checked')].map(c => c.value);
    if (user) {
      const home = (overlay.querySelector('#sa-home') || {}).value || '';
      if (home && home !== s.home) {
        const r = await apiFetch('/api/v1/admin/users/location', { method: 'POST', body: JSON.stringify({ id, location_code: home }) });
        if (!r || !r.ok) { await showApiError(r, 'Could not change the home store.'); return; }
      }
    }
    if (admin) { close(); showToast('Home store saved.', { variant: 'success' }); if (onSaved) onSaved(); return; }
    const r = await apiFetch(user ? '/api/v1/admin/users/stores' : '/api/v1/admin/roles/stores', {
      method: 'POST',
      body: JSON.stringify(user ? { id, access: m, stores: picked } : { role: id, access: m, stores: picked }),
    });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Could not save the stores.'); return; }
    close();
    showToast('Stores saved.', { variant: 'success' });
    if (onSaved) onSaved();
  }));
  (overlay.querySelector('input[name="sa-mode"]:checked') || overlay.querySelector('select, input'))?.focus();
}
