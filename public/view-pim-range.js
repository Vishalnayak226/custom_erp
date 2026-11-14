// PIM: store / channel range, markdown calendar, listing drift.
// Lazy: loaded by view-pim.js for the pages 'range', 'markdown' and 'drift'.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
await loadViewStyles('view-pim-data.css');

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const rootEl = () => document.getElementById('view-root');
const getList = async (doctype) => {
  const r = await apiFetch(`/api/v1/doc/${doctype}?limit=500`);
  return r && r.ok ? r.json() : [];
};
const opts = (rows, blank) => (blank ? `<option value="">${esc(blank)}</option>` : '') +
  rows.map(r => `<option value="${esc(r.id)}">${esc(r.name || r.id)}</option>`).join('');
const val = (root, sel) => (root.querySelector(sel).value || '').trim();

function panel(html) {
  const el = document.createElement('div');
  el.className = 'table-panel';
  el.innerHTML = html;
  return el;
}

export async function renderPIMRangePage(container, tab) {
  if (tab === 'markdown') return renderMarkdown(container);
  if (tab === 'drift') return renderDrift(container);
  return renderRange(container);
}

const rerender = (tab) => async () => {
  const host = rootEl();
  if (!host) return;
  host.innerHTML = '';
  await renderPIMRangePage(host, tab);
};

// ---- Range -----------------------------------------------------------------
async function renderRange(container) {
  const [rows, stores, channels] = await Promise.all([getList('Assortment'), getList('Location'), getList('Channel')]);
  const active = rows.filter(r => (r.status || 'Active') === 'Active');
  const canWrite = canCreateDoctype('Assortment');
  const list = panel(`<div class="table-wrapper"><table><thead><tr><th>Store or channel</th><th>Item or category</th><th>From</th><th>To</th><th></th></tr></thead><tbody>${
    active.length ? active.map(r => `<tr><td>${esc(r.store || r.channel)}</td><td>${esc(r.item || (r.category ? 'Category ' + r.category : ''))}</td>
      <td>${esc(r.valid_from)}</td><td>${esc(r.valid_to)}</td>
      <td>${canWrite ? `<button type="button" class="btn btn-outline btn-sm" data-range-off="${esc(r.id)}">Remove</button>` : ''}</td></tr>`).join('')
      : '<tr><td colspan="5" class="text-muted">No range rows.</td></tr>'}</tbody></table></div>`);
  const form = panel(`<div class="pd-form">
    <label>Stores<select id="rg-stores" multiple size="5">${opts(stores)}</select></label>
    <label>Channels<select id="rg-channels" multiple size="4">${opts(channels)}</select></label>
    <label>Category<input id="rg-cat" class="form-input"></label>
    <label>Items<input id="rg-items" class="form-input" placeholder="SKU, SKU"></label>
    <label>From<input id="rg-from" type="date" class="form-input"></label>
    <label>To<input id="rg-to" type="date" class="form-input"></label>
    <div><button type="button" class="btn btn-primary" id="rg-add">Add</button></div><div id="rg-out" class="text-muted"></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-range', title: 'Range', page: 'Add to range', count: active.length,
    newLabel: canWrite ? 'Add to Range' : '', form, list, heading: 'h2' }));
  txnRestorePane('pim-range');

  const picked = (sel) => [...form.querySelector(sel).selectedOptions].map(o => o.value);
  form.querySelector('#rg-add').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const out = form.querySelector('#rg-out');
    btn.disabled = true;
    const r = await apiFetch('/api/v1/pim/assortment/bulk', { method: 'POST', body: JSON.stringify({
      stores: picked('#rg-stores'), channels: picked('#rg-channels'), category: val(form, '#rg-cat'),
      items: val(form, '#rg-items').split(',').map(s => s.trim()).filter(Boolean),
      valid_from: val(form, '#rg-from'), valid_to: val(form, '#rg-to') }) });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Nothing was added.'); return; }
    const o = await r.json();
    out.textContent = `${o.added} added, ${o.skipped} already there.`;
    txnDone('pim-range');
    await rerender('range')();
  });
  list.querySelectorAll('[data-range-off]').forEach(b => b.addEventListener('click', async (e) => {
    const id = e.currentTarget.dataset.rangeOff;
    const row = rows.find(r => r.id === id);
    if (!row) return;
    e.currentTarget.disabled = true;
    const r = await apiFetch(`/api/v1/doc/Assortment/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify({ ...row, status: 'Inactive' }) });
    if (r && !r.ok) await showApiError(r, 'The row was not removed.');
    await rerender('range')();
  }));
}

// ---- Markdown calendar -----------------------------------------------------
async function renderMarkdown(container) {
  const r = await apiFetch('/api/v1/pim/markdown-calendar');
  if (!r) return;
  if (!r.ok) { await showApiError(r, 'The calendar was not loaded.'); return; }
  const plans = (await r.json()).plans || [];
  const stores = await getList('Location');
  const canWrite = canCreateDoctype('MarkdownPlan');
  let month = null;
  const body = plans.map(p => {
    const head = p.month !== month && p.state !== 'Draft' ? (month = p.month, `<tr class="pd-gap"><td colspan="7"><strong>${esc(p.month)}</strong></td></tr>`) : '';
    const act = !canWrite ? '' : (p.state === 'Draft' ? `<button type="button" class="btn btn-outline btn-sm" data-md-set="${esc(p.id)}:Scheduled">Schedule</button> ` : '') +
      (['Draft', 'Scheduled', 'Running'].includes(p.state) ? `<button type="button" class="btn btn-outline btn-sm" data-md-set="${esc(p.id)}:Cancelled">Cancel</button>` : '');
    return `${head}<tr><td>${esc(p.name)}</td><td>${esc(p.start_date)} to ${esc(p.end_date)}</td><td>${esc(p.state)}</td><td>${esc(p.scope)}</td>
      <td>${esc(p.discount)}</td><td>${esc(p.stores)}</td><td>${act}</td></tr>`;
  }).join('');
  const list = panel(`<div class="table-wrapper"><table><thead><tr><th>Plan</th><th>Dates</th><th>State</th><th>Items</th><th>Markdown</th><th>Stores</th><th></th></tr></thead><tbody>${
    body || '<tr><td colspan="7" class="text-muted">No markdown plans.</td></tr>'}</tbody></table></div>`);
  const form = panel(`<div class="pd-form">
    <label>Name<input id="md-name" class="form-input"></label>
    <label>Category<input id="md-cat" class="form-input"></label>
    <label>Items<input id="md-items" class="form-input" placeholder="SKU, SKU"></label>
    <label>Percent off<input id="md-pct" type="number" step="any" class="form-input"></label>
    <label>Fixed price<input id="md-fixed" type="number" step="any" class="form-input"></label>
    <label>Start<input id="md-start" type="date" class="form-input"></label>
    <label>End<input id="md-end" type="date" class="form-input"></label>
    <label>Stores<select id="md-stores" multiple size="4">${opts(stores)}</select></label>
    <div><button type="button" class="btn btn-primary" id="md-save">Schedule</button>
    <button type="button" class="btn btn-outline" id="md-draft">Save draft</button></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-markdown', title: 'Markdowns', page: 'New plan', count: plans.length,
    newLabel: canWrite ? 'New Plan' : '', form, list, heading: 'h2' }));
  txnRestorePane('pim-markdown');

  const save = async (status, btn) => {
    btn.disabled = true;
    const body = { name: val(form, '#md-name'), category: val(form, '#md-cat'), items: val(form, '#md-items'),
      percent_off: val(form, '#md-pct'), fixed_price: val(form, '#md-fixed'), start_date: val(form, '#md-start'), end_date: val(form, '#md-end'),
      stores: [...form.querySelector('#md-stores').selectedOptions].map(o => o.value).join(', '), status };
    const res = await apiFetch('/api/v1/doc/MarkdownPlan', { method: 'POST', body: JSON.stringify(body) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The plan was not saved.'); return; }
    txnDone('pim-markdown');
    await rerender('markdown')();
  };
  form.querySelector('#md-save').addEventListener('click', e => save('Scheduled', e.currentTarget));
  form.querySelector('#md-draft').addEventListener('click', e => save('Draft', e.currentTarget));
  list.querySelectorAll('[data-md-set]').forEach(b => b.addEventListener('click', async (e) => {
    const [id, status] = e.currentTarget.dataset.mdSet.split(':');
    e.currentTarget.disabled = true;
    const cur = await apiFetch(`/api/v1/doc/MarkdownPlan/${encodeURIComponent(id)}`);
    if (!cur || !cur.ok) { if (cur) await showApiError(cur, 'The plan was not loaded.'); return; }
    const doc = await cur.json();
    const res = await apiFetch(`/api/v1/doc/MarkdownPlan/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify({ ...doc, status }) });
    if (res && !res.ok) await showApiError(res, 'The plan was not changed.');
    await rerender('markdown')();
  }));
}

// ---- Listing drift ---------------------------------------------------------
async function renderDrift(container) {
  const channels = await getList('Channel');
  const host = document.createElement('div');
  const bar = panel(`<div class="pd-bar"><h2>Listing drift</h2>
    <select id="dr-ch" class="form-select">${opts(channels, 'Channel')}</select>
    <button type="button" class="btn btn-outline btn-sm hidden" id="dr-refresh">Refresh</button></div><div id="dr-out"></div>`);
  host.appendChild(bar);
  container.appendChild(host);
  const out = bar.querySelector('#dr-out');
  const sel = bar.querySelector('#dr-ch');
  const refresh = bar.querySelector('#dr-refresh');

  const load = async () => {
    const channel = sel.value;
    if (!channel) { out.innerHTML = '<p class="text-muted">Choose a channel.</p>'; refresh.classList.add('hidden'); return; }
    const r = await apiFetch(`/api/v1/pim/listing-drift?channel=${encodeURIComponent(channel)}`);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The report was not loaded.'); return; }
    const j = await r.json();
    const rows = j.rows || [];
    refresh.classList.toggle('hidden', !j.can_refresh);
    out.innerHTML = `<div class="table-wrapper"><table><thead><tr><th>Item</th><th>Differs</th><th>Listing</th><th>Item master</th><th></th></tr></thead><tbody>${
      rows.length ? rows.map(d => `<tr><td>${esc(d.sku)} ${esc(d.item_name)}</td><td>${esc(d.kind)}</td><td>${esc(d.listing)}</td><td>${esc(d.master)}</td>
        <td>${d.can_publish ? `<button type="button" class="btn btn-outline btn-sm" data-dr-pub="${esc(d.sku)}">Republish</button>` : ''}</td></tr>`).join('')
        : '<tr><td colspan="5" class="text-muted">No drift.</td></tr>'}</tbody></table></div>`;
    out.querySelectorAll('[data-dr-pub]').forEach(b => b.addEventListener('click', async (e) => {
      const btn = e.currentTarget;
      btn.disabled = true;
      const res = await apiFetch('/api/v1/pim/publish', { method: 'POST', body: JSON.stringify({ item_code: btn.dataset.drPub, channel }) });
      if (res && !res.ok) { await showApiError(res, 'The publish was not queued.'); btn.disabled = false; return; }
      btn.textContent = 'Queued';
    }));
  };
  sel.addEventListener('change', load);
  refresh.addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    const r = await apiFetch(`/api/v1/marketplace/channels/${encodeURIComponent(sel.value)}/listings/pull`, { method: 'POST' });
    btn.disabled = false;
    if (r && !r.ok) await showApiError(r, 'The listings were not refreshed.');
    await load();
  });
  await load();
}
