// Item 301 (ask 301): Settings › Stores, the screen over the Location records -
// stores, warehouses and the HO. The user looked under Settings for a store's
// State and found nothing: Location was only a list under Setup. Not a second
// master (Stage 30.5.5 retired one): it reads and saves /api/v1/doc/Location,
// so ValidateDocument and the hooks run as for any Location save. List first;
// New Store and each store are their own pages, the fields in sections. A
// store a purchase order would refuse (PURCHA-4965) is flagged No State.
const Q = new URL(import.meta.url).search;
const SCREEN = 'stores-screen';
const esc = (s) => escapeHTMLText(String(s ?? ''));
const VIEWS = [['all', 'All types'], ['Store', 'Stores'], ['Warehouse', 'Warehouses'], ['HO', 'HO'], ['nostate', 'No State']];
const SECTIONS = [
  { title: 'Identity', fields: ['code', 'name', 'type', 'short_code', 'parent', 'level', 'status', 'legal_entity', 'cost_center'] },
  { title: 'Address & GST', fields: ['address', 'address_line2', 'city', 'state', 'state_code', 'pincode'] },
  { title: 'Contact', fields: ['contact_phone', 'email', 'manager'] },
  { title: 'Selling', fields: ['sellable', 'salesman_mode', 'upi_id', 'items_not_sold_here', 'items_not_stocked_here'] },
  { title: 'Online orders', closed: true, fields: ['facility_code', 'ship_online', 'ship_channels', 'ship_daily_cap', 'ship_priority'] },
  { title: 'Transfers', closed: true, fields: ['transfer_fill_policy', 'transfer_min_fill_pct'] }
];

let listState = null;     // the list's view, search and page (txn-screen)
let statusFilter = '';
let openRecord = null;    // the store on its own page, kept for a re-render there
let fromPO = '';          // a purchase order's Edit store opened this store: saving goes back
let linkLists = {};       // doctype -> its records, for the Link boxes; read once per render

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

const entityOf = (ents, ref) => ref ? ents.find(e => String(e.id) === String(ref)) || ents.find(e => String(e.code || '') === String(ref)) : null;

// buyerStateCode's order (engines/gst_place_of_supply.go): the legal entity's
// GSTIN, then its State, then the store's own. None of them = PO refused.
function hasNoState(d, ents) {
  if (String(d.state || '').trim()) return false;
  const e = entityOf(ents, d.legal_entity);
  return !e || !(/^\d{2}/.test(String(e.gstin || '').trim()) || String(e.state || '').trim());
}

export async function renderStoresView(container) {
  await loadPickers();
  const txn = await import('./txn-screen.js' + Q);
  listState = listState || txn.txnListState();
  linkLists = {};
  let handoff = '';
  try { handoff = sessionStorage.getItem('erp-store-open') || ''; sessionStorage.removeItem('erp-store-open'); } catch (_) { /* storage off: the list opens */ }
  const whole = listState.view === 'nostate';
  const filters = {};
  if (!whole && listState.view !== 'all') filters.type = listState.view;
  if (statusFilter) filters.status = statusFilter;
  const [page, meta, entities] = await Promise.all([
    txn.txnLoadList('Location', listState, { filters, whole }),
    getJSON('/api/v1/doc/Location/meta', null),
    getJSON('/api/v1/doc/LegalEntity?limit=500', [])
  ]);
  const again = () => renderView('stores');
  if (page.error) { renderErrorPanel(container, page.error, again); return; }
  if (!Array.isArray(meta)) { renderErrorPanel(container, 'The store form could not be loaded.', again); return; }
  const ents = Array.isArray(entities) ? entities : [];
  linkLists.LegalEntity = Promise.resolve(ents);
  const shown = whole ? page.docs.filter(d => hasNoState(d, ents)) : page.docs;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  const statusPick = `<select class="form-select" data-st-status aria-label="Status">${[['', 'Any status'], ['Active', 'Active'], ['Inactive', 'Inactive']]
    .map(([v, l]) => `<option value="${v}"${v === statusFilter ? ' selected' : ''}>${l}</option>`).join('')}</select>`;
  const stateCell = d => d.state ? esc(d.state)
    : hasNoState(d, ents) ? '<span class="badge badge-danger" title="Purchase orders refused until set">No State</span>'
      : '<span class="text-muted">From legal entity</span>';
  const entityCell = d => {
    if (!d.legal_entity) return '<span class="badge badge-warning">None</span>';
    const e = entityOf(ents, d.legal_entity);
    return esc(e ? (e.legal_name || e.name || e.code || e.id) : d.legal_entity);
  };
  list.innerHTML = `${txn.txnListBar(listState, page, VIEWS, 'Search stores').replace('<input type="search"', `${statusPick}<input type="search"`)}
    <div class="table-wrapper"><table><thead><tr><th>Code</th><th>Name</th><th>Type</th><th>State</th><th>City</th><th>Legal entity</th><th>Status</th></tr></thead>
    <tbody>${shown.length ? shown.map(d => `<tr>
      <td class="s-72126d nowrap"><a href="#" class="empty-state-link" data-st-open="${esc(d.id)}">${esc(d.code || d.id)}</a></td>
      <td>${esc(d.name)}</td><td>${esc(d.type)}</td><td>${stateCell(d)}</td><td>${esc(d.city)}</td><td>${entityCell(d)}</td>
      <td><span class="badge ${d.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${esc(d.status)}</span></td></tr>`).join('')
    : `<tr><td colspan="7" class="s-b4fdf8">${listState.view !== 'all' || listState.q || statusFilter ? 'Nothing here in this view.' : 'No stores yet.'}</td></tr>`}</tbody></table></div>`;

  const mayCreate = canCreateDoctype('Location');
  const newPane = document.createElement('div');
  const section = txn.txnPagedSection({ id: SCREEN, title: 'Stores', count: txn.txnListCount(page, shown.length), list,
    forms: mayCreate ? { new: ['New Store', newPane] } : {} });
  const openPane = document.createElement('div');
  openPane.dataset.pane = 'open';
  openPane.className = 'hidden';
  section.appendChild(openPane);
  container.appendChild(section);

  // New Store's form is drawn when its page opens, not with every list.
  let newDrawn = false;
  const drawNew = async () => {
    if (newDrawn || !mayCreate) return;
    newDrawn = true;
    newPane.replaceChildren(await storeForm(meta, null, ents));
  };
  section.addEventListener('click', e => {
    if ((e.target.closest('[data-act="txnShowPane"]')?.getAttribute('data-act-args') || '').includes(':new')) drawNew();
  }, true);
  const openStore = async (id) => {
    const res = await apiFetch(`/api/v1/doc/Location/${encodeURIComponent(id)}`);
    if (!res || !res.ok) { await showApiError(res, 'This store could not be opened.'); return; }
    openRecord = await res.json();
    openPane.replaceChildren(await storeForm(meta, openRecord, ents));
    window.txnShowPane(`${SCREEN}:open`);
  };

  txn.txnWireListBar(list, listState, again);
  list.querySelector('[data-st-status]').addEventListener('change', e => { statusFilter = e.target.value; listState.offset = 0; again(); });
  list.querySelectorAll('[data-st-open]').forEach(a => a.addEventListener('click', e => { e.preventDefault(); openStore(a.dataset.stOpen); }));

  const at = txn.txnPageOf(SCREEN, 'list');
  if (handoff) {
    fromPO = handoff;
    txn.txnRestorePane(SCREEN);
    await openStore(handoff);
    return;
  }
  if (at === 'open' && openRecord) {
    openPane.replaceChildren(await storeForm(meta, openRecord, ents));
  } else if (at === 'new') {
    await drawNew();
  } else {
    fromPO = '';
  }
  txn.txnRestorePane(SCREEN);
  // A refresh on a store's page has no store to show: back to the list.
  if (at === 'open' && !openRecord) window.txnShowPane(`${SCREEN}:list`);
}

// fillLink loads a Link box: blank first (a required one stays unchosen), then
// the target's records by name. A saved value not on the list stays chosen.
async function fillLink(sel, doctype, value, selfId) {
  linkLists[doctype] = linkLists[doctype] || getJSON(`/api/v1/doc/${encodeURIComponent(doctype)}?limit=500`, []);
  const docs = ((await linkLists[doctype]) || []).filter(d => d.status !== 'Cancelled' && !(selfId && String(d.id) === String(selfId)));
  const name = d => doctype === 'LegalEntity' ? String(d.legal_name || d.name || d.code || d.id) : linkDisplayName(d);
  const cur = String(value ?? '');
  const hit = cur && (docs.find(d => String(d.id) === cur) || docs.find(d => String(d.code || '') === cur));
  sel.innerHTML = '<option value="">Choose…</option>' + docs.map(d => `<option value="${esc(d.id)}">${esc(name(d))}</option>`).join('')
    + (cur && !hit ? `<option value="${esc(cur)}">${esc(cur)}</option>` : '');
  sel.value = hit ? String(hit.id) : cur;
}

function fieldGroup(f, rec, series) {
  const fg = document.createElement('div');
  fg.className = 'form-group';
  const id = `st-f-${f.fieldname}`;
  const val = rec ? rec[f.fieldname] : undefined;
  const isCode = f.fieldname === 'code';
  const numbered = isCode && !rec && !(series && series.typed);
  const derived = isStateCodeFieldName(f.fieldname);
  fg.innerHTML = `<label class="form-label" for="${id}">${esc(taxIdLabel(f.fieldname, getTranslatedLabel(f.label)))}${f.mandatory && !numbered && !derived ? '<span class="required">*</span>' : ''}</label>`;
  let el;
  if (f.fieldtype === 'Data' && isStateFieldName(f.fieldname) && (window.FIELD_META?.regions || []).length) {
    el = buildStateSelect({ id, name: f.fieldname, value: val, required: f.mandatory });
  } else if (f.fieldtype === 'Select') {
    el = document.createElement('select');
    el.className = 'form-select';
    const cur = String(val ?? '');
    const opts = String(f.options || '').split(',').map(o => o.trim()).filter(o => o && (!(f.approval_only || []).includes(o) || o === cur));
    el.innerHTML = '<option value=""></option>' + opts.map(o => `<option value="${esc(o)}">${esc(o)}</option>`).join('');
    el.value = rec ? cur : ({ status: 'Active', type: 'Store' }[f.fieldname] || '');
  } else if (f.fieldtype === 'Link') {
    el = document.createElement('select');
    el.className = 'form-select';
    el.dataset.linkDoctype = f.options;
    el.innerHTML = '<option value="">Loading…</option>';
    fillLink(el, f.options, val, rec && rec.id);
  } else if (/Text/.test(f.fieldtype)) {
    el = document.createElement('textarea');
    el.className = 'form-input';
    el.rows = 2;
    el.value = val ?? '';
    fg.classList.add('form-group-wide');
  } else {
    el = document.createElement('input');
    el.className = 'form-input';
    el.autocomplete = 'off';
    el.type = /^(Number|Currency|Int|Float)$/.test(f.fieldtype) ? 'number' : f.fieldtype === 'Date' ? 'date' : f.fieldtype === 'Check' ? 'checkbox' : 'text';
    if (el.type === 'number') el.step = 'any';
    if (el.type === 'checkbox') el.checked = !!val; else el.value = val ?? '';
  }
  el.id = id;
  el.name = f.fieldname;
  el.required = !!f.mandatory && !numbered && !derived;
  if (isCode && rec) { el.value = rec.code || rec.id; el.readOnly = true; }
  if (numbered) { el.readOnly = true; el.placeholder = series && series.configured && series.next ? `${series.next} (given on save)` : 'Given on save'; }
  if (derived) { el.readOnly = true; if (!el.value) el.placeholder = 'Set on save'; }
  fg.appendChild(el);
  return fg;
}

// storeForm is New Store (rec null) or a store's own page.
async function storeForm(meta, rec, ents) {
  const series = rec ? null : await loadMasterCodeSeries('Location');
  const mayEdit = rec ? canUpdateDoctype('Location') : canCreateDoctype('Location');
  const fields = meta.filter(f => f.fieldname !== 'id' && !f.mirrored && !f.server_set);
  const byName = new Map(fields.map(f => [f.fieldname, f]));
  const listed = new Set(SECTIONS.flatMap(s => s.fields));
  const form = document.createElement('form');
  form.className = 'table-panel s-ddaf9e';
  form.innerHTML = `<h2 class="s-db89ac">${rec ? esc(rec.name || rec.code || rec.id) : 'New Store'}</h2>`;
  [...SECTIONS, { title: 'More details', closed: true, fields: fields.map(f => f.fieldname).filter(n => !listed.has(n)) }].forEach(s => {
    const group = s.fields.map(n => byName.get(n)).filter(Boolean);
    if (!group.length) return;
    const det = document.createElement('details');
    det.className = 'form-section';
    if (form.querySelector('details')) det.style.cssText = 'border-top:1px solid var(--border-color);padding-top:12px;margin-top:12px;';
    // A closed section opens when it holds a required box or a value.
    det.open = !s.closed || group.some(f => f.mandatory || (rec && rec[f.fieldname] != null && String(rec[f.fieldname]) !== ''));
    det.innerHTML = `<summary>${esc(s.title)}</summary>`;
    const grid = document.createElement('div');
    grid.className = 'form-section-grid';
    group.forEach(f => grid.appendChild(fieldGroup(f, rec, series)));
    det.appendChild(grid);
    form.appendChild(det);
  });
  const code = form.querySelector('[name="code"]');
  if (!rec && series && !series.typed && !series.configured && code) code.closest('.form-group').after(masterCodeSetupRow(series, 'Store', code));
  const back = rec && fromPO && String(rec.id) === fromPO;
  const actions = document.createElement('div');
  actions.className = 's-1b0f49';
  actions.innerHTML = `${mayEdit ? '<button type="submit" class="btn btn-primary">Save store</button>' : ''}${back ? ' <button type="button" class="btn btn-outline" data-st-back>Back to PO</button>' : ''}`;
  form.appendChild(actions);

  if (!mayEdit) form.querySelectorAll('input, select, textarea').forEach(el => { el.disabled = true; });
  else {
    wireLocationCostCenter(form);
    if (!rec) {
      wireNewLocationDefaults(form);
      // D5: the legal entity is preselected only when there is one to choose.
      if (ents.length > 1) form.querySelector('[name="legal_entity"]')?.dispatchEvent(new Event('change', { bubbles: true }));
    } else wireLocationCodeEdit(form, rec);
  }
  actions.querySelector('[data-st-back]')?.addEventListener('click', () => { fromPO = ''; openRecord = null; history.go(-2); });
  form.addEventListener('submit', e => {
    e.preventDefault();
    guardAgainstDoubleSubmit(form.querySelector('button[type="submit"]'), 'Saving...', () => saveStore(form, rec, fields));
  });
  return form;
}

async function saveStore(form, rec, fields) {
  let id = rec ? String(rec.id) : '';
  let version = rec ? rec.version : undefined;
  if (rec) {
    // Stage 123.3: a new code renames the store everywhere first.
    const renamed = await changeLocationCodeBeforeSave(form, id);
    if (renamed === null) return;
    if (renamed !== id) {
      id = renamed;
      version = (await getJSON(`/api/v1/doc/Location/${encodeURIComponent(id)}`, {})).version;
    }
  }
  const payload = {};
  fields.forEach(f => {
    const el = form.querySelector(`[name="${f.fieldname}"]`);
    if (!el || isStateCodeFieldName(f.fieldname)) return;
    if (f.fieldname === 'code') { if (!rec && el.value.trim()) payload.code = el.value.trim(); return; }
    if (el.type === 'checkbox') payload[f.fieldname] = el.checked;
    else if (el.type === 'number') payload[f.fieldname] = el.value === '' ? null : Number(el.value);
    else payload[f.fieldname] = el.value;
  });
  if (!rec && !payload.code) {
    const format = readMasterCodeFormat(form);
    if (format) payload._code_format = format;
  }
  if (typeof version === 'number') payload.expected_version = version;
  const res = await apiFetch(rec ? `/api/v1/doc/Location/${encodeURIComponent(id)}` : '/api/v1/doc/Location', { method: 'POST', body: JSON.stringify(payload) });
  if (!res || !res.ok) { await showApiError(res, 'The store was not saved.'); return; }
  const saved = await res.json().catch(() => null);
  const status = saved && saved.status;
  showToast(describeDocumentStatusOutcome('Store', status), { variant: status === 'Pending Approval' ? 'warning' : 'success' });
  if (saved && saved.held_fields) showToast(`Waiting for approval: ${saved.held_fields.replace(/_/g, ' ')}`, { variant: 'warning' });
  const savedId = (saved && saved.id) || id;
  rememberLinkDoc('Location', { ...(rec || {}), ...payload, id: savedId, code: savedId });
  await refreshSetupStatus();
  openRecord = null;
  // From a purchase order: back to it (the store page and the Stores entry
  // are the two history steps), where the draft is still open.
  if (rec && fromPO && String(rec.id) === fromPO && history.state && history.state.txnPane === `${SCREEN}:open`) {
    fromPO = '';
    history.go(-2);
    return;
  }
  const { txnDone } = await import('./txn-screen.js' + Q);
  txnDone(SCREEN);
  renderView('stores');
}
