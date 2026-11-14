// Stage 146.5.12 / 146.5.13 - PIM item page: typed relations and "Live on" links.
// Lazy: loaded by view-pim.js when an item's PIM page opens.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const RELATION_TYPES = ['Similar', 'Complete the look', 'Alternative', 'Upsell', 'Cross-sell', 'Add-on'];

export async function renderPIMItemExtras(container, itemCode) {
  await renderLiveOn(container, itemCode);
  await renderRelations(container, itemCode);
  await loadViewStyles('view-pim-data.css');
  await renderPIMItemChanges(container, itemCode);
  await renderPIMItemMerge(container, itemCode);
}

async function renderLiveOn(container, itemCode) {
  const r = await apiFetch(`/api/v1/pim/live-on/${encodeURIComponent(itemCode)}`);
  const links = r && r.ok ? await r.json() : [];
  if (!links.length) return;
  const el = document.createElement('div');
  el.className = 'table-panel';
  el.style.cssText = 'padding:16px;margin-top:16px';
  el.innerHTML = `<h3>Live on</h3><div class="table-wrapper"><table><thead><tr><th>Channel</th><th>Published</th><th>Page</th></tr></thead><tbody>
    ${links.map(l => `<tr><td>${esc(l.channel)}</td><td>${esc(l.published_at)}</td><td>${/^https?:\/\//.test(l.url || '')
      ? `<a href="${esc(l.url)}" target="_blank" rel="noopener noreferrer">Open</a>` : esc(l.external_id)}</td></tr>`).join('')}</tbody></table></div>`;
  container.appendChild(el);
}

async function renderRelations(container, itemCode) {
  const r = await apiFetch(`/api/v1/doc/ProductRelation?q=${encodeURIComponent(itemCode)}&q_fields=item&limit=200`);
  const all = r && r.ok ? await r.json() : [];
  const rows = all.filter(x => x.item === itemCode && x.status !== 'Inactive')
    .sort((a, b) => (a.relation_type || '').localeCompare(b.relation_type || '') || (Number(a.position) || 0) - (Number(b.position) || 0));
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = rows.length ? `<div class="table-wrapper"><table><thead><tr><th>Relation</th><th>Item</th><th></th></tr></thead><tbody>
    ${rows.map(x => `<tr><td>${esc(x.relation_type)}</td><td data-link-ref="${esc(x.related_item)}" data-link-doctype="Item">${esc(x.related_item)}</td>
      <td><button type="button" class="btn btn-outline btn-sm" data-pr-del="${esc(x.id)}">Remove</button></td></tr>`).join('')}</tbody></table></div>`
    : '<p class="text-muted">None yet.</p>';
  const form = document.createElement('div');
  form.className = 'table-panel';
  form.innerHTML = `<div class="form-row s-287f77">
    <label>Relation<select class="form-select" id="pr-type">${RELATION_TYPES.map(t => `<option>${esc(t)}</option>`).join('')}</select></label>
    <label>Item<input class="form-input" id="pr-item"></label>
    <div><button type="button" class="btn btn-primary" id="pr-save">Save</button></div><div id="pr-err" class="form-error hidden"></div></div>`;
  const id = 'pim-rel-' + itemCode.replace(/[^A-Za-z0-9]/g, '-');
  const section = txnPagedSection({ id, title: 'Related items', page: 'New relation', count: rows.length, newLabel: 'New Relation', form, list, heading: 'h3' });
  container.appendChild(section);
  txnRestorePane(id);

  form.querySelector('#pr-save').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const err = form.querySelector('#pr-err');
    btn.disabled = true;
    const res = await apiFetch('/api/v1/doc/ProductRelation', { method: 'POST', body: JSON.stringify({
      item: itemCode, relation_type: form.querySelector('#pr-type').value, related_item: form.querySelector('#pr-item').value.trim(), status: 'Active' }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { const d = await res.json().catch(() => ({})); err.textContent = d.error || 'The relation was not saved.'; err.classList.remove('hidden'); return; }
    err.classList.add('hidden');
    txnDone(id);
    renderView('pim');
  });
  list.querySelectorAll('[data-pr-del]').forEach(btn => btn.addEventListener('click', async () => {
    btn.disabled = true;
    const res = await apiFetch(`/api/v1/doc/ProductRelation/${encodeURIComponent(btn.dataset.prDel)}`, { method: 'DELETE' });
    if (res && !res.ok) { btn.disabled = false; showApiError(res, 'The relation was not removed.'); return; }
    renderView('pim');
  }));
}

// 168.10 / 168.9 - scheduled changes and "Merge into..." on the item page.
const CHANGE_FIELDS = [['sale_price', 'Selling price'], ['mrp', 'MRP'], ['standard_cost', 'Purchase price'], ['gst_rate', 'GST rate'],
  ['hsn_code', 'HSN code'], ['tax_treatment', 'Tax treatment'], ['attribute', 'Attribute']];
const fieldLabel = (f) => (CHANGE_FIELDS.find(x => x[0] === f) || [f, f])[1];

async function formError(res, err, fallback) {
  const d = await res.json().catch(() => ({}));
  err.textContent = d.error || d.message || fallback;
  err.classList.remove('hidden');
}

async function renderPIMItemChanges(container, itemCode) {
  const r = await apiFetch(`/api/v1/doc/ScheduledItemChange?q=${encodeURIComponent(itemCode)}&q_fields=item&limit=200`);
  const all = r && r.ok ? await r.json() : [];
  const rows = all.filter(x => x.item === itemCode).sort((a, b) => String(a.effective_date).localeCompare(String(b.effective_date)));
  const ar = await apiFetch('/api/v1/doc/ProductAttributeDef?limit=200');
  const attrs = ar && ar.ok ? await ar.json() : [];
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = rows.length ? `<div class="table-wrapper"><table><thead><tr><th>Day</th><th>Field</th><th>New value</th><th>Status</th><th></th></tr></thead><tbody>
    ${rows.map(x => `<tr><td>${esc(x.effective_date)}</td><td>${esc(fieldLabel(x.field))}${x.attribute ? ` · ${esc(x.attribute)}` : ''}</td><td>${esc(x.new_value)}</td>
      <td title="${esc(x.failure_reason)}">${esc(x.status)}</td>
      <td>${x.status === 'Scheduled' ? `<button type="button" class="btn btn-outline btn-sm" data-sc-cancel="${esc(x.id)}">Cancel</button>` : ''}</td></tr>`).join('')}</tbody></table></div>`
    : '<p class="text-muted">None yet.</p>';
  const form = document.createElement('div');
  form.className = 'table-panel';
  form.innerHTML = `<div class="pd-form">
    <label>Field<select class="form-select" id="sc-field">${CHANGE_FIELDS.map(([v, l]) => `<option value="${v}">${esc(l)}</option>`).join('')}</select></label>
    <label class="hidden" id="sc-attr-row">Attribute<select class="form-select" id="sc-attr">${attrs.map(a => `<option value="${esc(a.id)}">${esc(a.name || a.id)}</option>`).join('')}</select></label>
    <label>New value<input class="form-input" id="sc-value"></label>
    <label>Day<input class="form-input" type="date" id="sc-date"></label>
    <div><button type="button" class="btn btn-primary" id="sc-save">Save</button></div><div id="sc-err" class="form-error hidden"></div></div>`;
  const id = 'pim-chg-' + itemCode.replace(/[^A-Za-z0-9]/g, '-');
  container.appendChild(txnPagedSection({ id, title: 'Changes scheduled', page: 'New change', count: rows.length, newLabel: 'New Change', form, list, heading: 'h3' }));
  txnRestorePane(id);
  const isAttr = () => form.querySelector('#sc-field').value === 'attribute';
  form.querySelector('#sc-field').addEventListener('change', () => form.querySelector('#sc-attr-row').classList.toggle('hidden', !isAttr()));
  form.querySelector('#sc-save').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const err = form.querySelector('#sc-err');
    btn.disabled = true;
    const res = await apiFetch('/api/v1/doc/ScheduledItemChange', { method: 'POST', body: JSON.stringify({
      item: itemCode, field: form.querySelector('#sc-field').value, attribute: isAttr() ? form.querySelector('#sc-attr').value : '',
      new_value: form.querySelector('#sc-value').value.trim(), effective_date: form.querySelector('#sc-date').value, status: 'Scheduled' }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { await formError(res, err, 'The change was not saved.'); return; }
    err.classList.add('hidden');
    txnDone(id);
    renderView('pim');
  });
  list.querySelectorAll('[data-sc-cancel]').forEach(btn => btn.addEventListener('click', async () => {
    btn.disabled = true;
    const row = rows.find(x => x.id === btn.dataset.scCancel);
    const res = await apiFetch(`/api/v1/doc/ScheduledItemChange/${encodeURIComponent(btn.dataset.scCancel)}`, { method: 'POST', body: JSON.stringify({ ...row, status: 'Cancelled' }) });
    if (res && !res.ok) { btn.disabled = false; showApiError(res, 'The change was not cancelled.'); return; }
    renderView('pim');
  }));
}

async function renderPIMItemMerge(container, itemCode) {
  const ir = await apiFetch(`/api/v1/doc/Item/${encodeURIComponent(itemCode)}`);
  const item = ir && ir.ok ? await ir.json() : {};
  const info = document.createElement('div');
  info.className = 'table-panel';
  info.innerHTML = item.merged_into ? `<p>Merged into ${esc(item.merged_into)}.</p>` : item.alias_codes ? `<p>Old codes: ${esc(item.alias_codes)}</p>` : '<p class="text-muted">Not merged.</p>';
  const form = document.createElement('div');
  form.className = 'table-panel';
  form.innerHTML = `<div class="pd-form"><label>Keep this item<input class="form-input" id="mg-keep"></label>
    <div><button type="button" class="btn btn-outline" id="mg-prev">Preview</button></div>
    <div id="mg-out" class="pd-sub hidden"></div>
    <div><button type="button" class="btn btn-primary hidden" id="mg-go">Merge</button></div><div id="mg-err" class="form-error hidden"></div></div>`;
  const id = 'pim-merge-' + itemCode.replace(/[^A-Za-z0-9]/g, '-');
  container.appendChild(txnPagedSection({ id, title: 'Merge', page: 'Merge into', count: '', newLabel: 'Merge into...', form, list: info, heading: 'h3' }));
  txnRestorePane(id);
  const call = (dry) => apiFetch('/api/v1/pim/items/merge', { method: 'POST', body: JSON.stringify({ keep: form.querySelector('#mg-keep').value.trim(), duplicate: itemCode, dry_run: dry }) });
  const err = form.querySelector('#mg-err');
  form.querySelector('#mg-prev').addEventListener('click', async () => {
    err.classList.add('hidden');
    const res = await call(true);
    if (!res) return;
    if (!res.ok) { await formError(res, err, 'Could not preview.'); return; }
    const p = await res.json();
    const out = form.querySelector('#mg-out');
    out.innerHTML = p.refusals.length ? `<ul>${p.refusals.map(x => `<li>${esc(x)}</li>`).join('')}</ul>`
      : `<p>${esc(p.duplicate)} into ${esc(p.keep)}: ${p.stock_moves.reduce((n, m) => n + m.qty, 0)} unit(s) move, ${p.draft_documents.length} draft(s) re-pointed.</p>`;
    out.classList.remove('hidden');
    form.querySelector('#mg-go').classList.toggle('hidden', p.refusals.length > 0);
  });
  form.querySelector('#mg-go').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    const res = await call(false);
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { await formError(res, err, 'The merge was not done.'); return; }
    txnDone(id);
    renderView('pim');
  });
}
