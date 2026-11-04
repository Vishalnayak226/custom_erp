// Stage 146.5.2 / 146.5.5 / 146.5.6 - PIM: marketplace templates, feeds, design values.
// Lazy: loaded by view-pim.js for the tabs 'channel-templates', 'feeds', 'design-values' and 'checklists'.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
await loadViewStyles('view-pim-data.css');

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const getList = async (doctype) => {
  const r = await apiFetch(`/api/v1/doc/${doctype}?limit=200`);
  return r && r.ok ? r.json() : [];
};
const options = (rows, blank) => (blank ? `<option value="">${esc(blank)}</option>` : '') +
  rows.map(r => `<option value="${esc(r.id)}">${esc(r.name || r.id)}</option>`).join('');

function panel(html) {
  const el = document.createElement('div');
  el.className = 'table-panel';
  el.innerHTML = html;
  return el;
}

export async function renderPIMDataPage(container, tab) {
  if (tab === 'channel-templates') return renderChannelTemplates(container);
  if (tab === 'feeds') return renderFeeds(container);
  if (tab === 'checklists') return renderChecklists(container);
  return renderDesignValues(container);
}

// ---- Marketplace templates -------------------------------------------------
async function renderChannelTemplates(container) {
  const [templates, channels] = await Promise.all([getList('ChannelCategoryTemplate'), getList('Channel')]);
  const list = panel(templates.length ? `<div class="table-wrapper"><table><thead><tr><th>Channel</th><th>Category</th><th>Columns</th><th>Status</th><th></th></tr></thead><tbody>
    ${templates.map(t => `<tr><td>${esc(t.channel)}</td><td>${esc(t.channel_category)}</td><td>${(t.columns || []).length}</td><td>${esc(t.status)}</td>
      <td><button type="button" class="btn btn-outline btn-sm" data-pd-cover="${esc(t.id)}">Coverage</button></td></tr>`).join('')}</tbody></table></div>
    <div id="pd-cover"></div>` : '<p class="text-muted">None yet.</p>');
  const form = panel(`<div class="pd-form">
    <label>Channel<select class="form-select" id="pd-ch">${options(channels, 'Choose')}</select></label>
    <label>Category<input class="form-input" id="pd-cat"></label>
    <label>File<input type="file" class="form-input" id="pd-file" accept=".xlsx"></label>
    <label>Parent rows<select class="form-select" id="pd-parent"><option value="No">No</option><option value="Yes">Yes</option></select></label>
    <div><button type="button" class="btn btn-primary" id="pd-load">Load</button></div><div id="pd-out"></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-ctpl', title: 'Marketplace templates', page: 'Load file', count: templates.length,
    newLabel: 'Load File', form, list, heading: 'h2' }));
  txnRestorePane('pim-ctpl');

  form.querySelector('#pd-load').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const file = form.querySelector('#pd-file').files[0];
    const out = form.querySelector('#pd-out');
    if (!file) { out.textContent = 'Choose the file.'; return; }
    const fd = new FormData();
    fd.append('file', file);
    fd.append('channel', form.querySelector('#pd-ch').value);
    fd.append('channel_category', form.querySelector('#pd-cat').value.trim());
    fd.append('parent_rows', form.querySelector('#pd-parent').value);
    btn.disabled = true;
    const r = await apiUpload('/api/v1/pim/channel-templates/load', fd);
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The file was not loaded.'); return; }
    const o = await r.json();
    const part = (label, arr) => (arr && arr.length ? `<p>${label}: ${arr.map(esc).join(', ')}</p>` : '');
    out.innerHTML = `<p>${o.columns} columns, ${o.mandatory} required.</p>${part('Added', o.added)}${part('Removed', o.removed)}${part('Not mapped yet', o.unmapped)}${part('Check', o.warnings)}`;
    txnDone('pim-ctpl');
    renderView('pim');
  });
  list.querySelectorAll('[data-pd-cover]').forEach(btn => btn.addEventListener('click', async () => {
    const r = await apiFetch(`/api/v1/pim/channel-templates/${encodeURIComponent(btn.dataset.pdCover)}/coverage`);
    if (!r) return;
    if (!r.ok) { showApiError(r, 'Coverage was not read.'); return; }
    const c = await r.json();
    list.querySelector('#pd-cover').innerHTML = `<p>${c.items} item(s)${c.capped ? ' (first 500)' : ''}. Required mapped: ${c.mandatory_mapped}/${c.mandatory_total}. Optional mapped: ${c.optional_mapped}/${c.optional_total}.</p>
      <div class="table-wrapper"><table><thead><tr><th>Column</th><th>Required</th><th>Mapped</th><th>Filled</th></tr></thead><tbody>
      ${c.columns.map(x => `<tr class="${x.mandatory && !x.mapped ? 'pd-gap' : ''}"><td>${esc(x.column)}</td><td>${x.mandatory ? 'Yes' : ''}</td><td>${x.mapped ? 'Yes' : 'No'}</td><td>${x.filled}/${c.items}</td></tr>`).join('')}</tbody></table></div>`;
  }));
}

// ---- Feeds -----------------------------------------------------------------
async function renderFeeds(container) {
  const [feeds, runs, templates, groups] = await Promise.all([getList('PIMFeed'), getList('PIMExportRun'), getList('PIMExportTemplate'), getList('PIMProductGroup')]);
  runs.sort((a, b) => String(b.started_at).localeCompare(String(a.started_at)));
  const list = panel(`${feeds.length ? `<div class="table-wrapper"><table><thead><tr><th>Feed</th><th>Group</th><th>Ready only</th><th>Goes to</th><th></th></tr></thead><tbody>
    ${feeds.map(f => `<tr><td>${esc(f.name || f.id)}</td><td>${esc(f.product_group)}</td><td>${esc(f.ready_only || 'No')}</td><td>${esc(f.destination || 'Download')}</td>
      <td>${f.status === 'Active' ? `<button type="button" class="btn btn-primary btn-sm" data-pd-run="${esc(f.id)}">Run</button>` : ''}</td></tr>`).join('')}</tbody></table></div>` : '<p class="text-muted">None yet.</p>'}
    ${runs.length ? `<h3 class="pd-sub">Runs</h3><div class="table-wrapper"><table><thead><tr><th>When</th><th>Feed</th><th>Status</th><th>Rows</th><th>Left out</th><th>Delivery</th></tr></thead><tbody>
    ${runs.slice(0, 30).map(x => `<tr><td>${esc(String(x.started_at).replace('T', ' ').slice(0, 16))}</td><td>${esc(x.feed)}</td><td>${esc(x.status)}</td><td>${x.rows}</td>
      <td title="${esc((x.skipped || []).slice(0, 20).map(s => `${s.item}: ${s.reason}`).join('\n'))}">${x.skipped_count}</td><td title="${esc(x.delivery_message)}">${esc(x.delivery_status)}</td></tr>`).join('')}</tbody></table></div>` : ''}`);
  const form = panel(`<div class="pd-form">
    <label>Code<input class="form-input" id="pf-code"></label><label>Name<input class="form-input" id="pf-name"></label>
    <label>Template<select class="form-select" id="pf-tpl">${options(templates, 'Choose')}</select></label>
    <label>Group<select class="form-select" id="pf-grp">${options(groups, 'All items')}</select></label>
    <label>Ready only<select class="form-select" id="pf-ready"><option>No</option><option>Yes</option></select></label>
    <label>Format<select class="form-select" id="pf-fmt"><option value="csv">CSV</option><option value="xlsx">XLSX</option></select></label>
    <label>Goes to<select class="form-select" id="pf-dest"><option>Download</option><option>Email</option></select></label>
    <label>Email to<input class="form-input" id="pf-mail"></label>
    <div><button type="button" class="btn btn-primary" id="pf-save">Save</button></div><div id="pf-err" class="form-error hidden"></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-feeds', title: 'Feeds', page: 'New feed', count: feeds.length, newLabel: 'New Feed', form, list, heading: 'h2' }));
  txnRestorePane('pim-feeds');

  form.querySelector('#pf-save').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const v = (id) => form.querySelector(id).value.trim();
    const err = form.querySelector('#pf-err');
    btn.disabled = true;
    const r = await apiFetch('/api/v1/doc/PIMFeed', { method: 'POST', body: JSON.stringify({
      code: v('#pf-code'), name: v('#pf-name'), export_template: v('#pf-tpl'), product_group: v('#pf-grp'), ready_only: v('#pf-ready'),
      format: v('#pf-fmt'), destination: v('#pf-dest'), recipient_email: v('#pf-mail'), status: 'Active' }) });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { const d = await r.json().catch(() => ({})); err.textContent = d.error || 'The feed was not saved.'; err.classList.remove('hidden'); return; }
    err.classList.add('hidden');
    txnDone('pim-feeds');
    renderView('pim');
  });
  list.querySelectorAll('[data-pd-run]').forEach(btn => btn.addEventListener('click', async () => {
    btn.disabled = true;
    const r = await apiFetch(`/api/v1/pim/feeds/${encodeURIComponent(btn.dataset.pdRun)}/run`, { method: 'POST' });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The feed did not run.'); return; }
    if ((r.headers.get('Content-Type') || '').includes('application/json')) {
      const o = await r.json();
      await showCustomAlert(`${o.rows} row(s), ${(o.skipped || []).length} left out.\n${o.delivery_status}${o.delivery_message ? `: ${o.delivery_message}` : ''}`, 'Feed');
    } else {
      const blob = await r.blob();
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = ((r.headers.get('Content-Disposition') || '').split('filename=')[1] || 'feed.csv').replace(/"/g, '');
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 5000);
      const n = Number(r.headers.get('X-Export-Skipped-Count') || 0);
      if (n) await showCustomAlert(`${r.headers.get('X-Export-Rows')} row(s), ${n} left out. The run list shows why.`, 'Feed');
    }
    renderView('pim');
  }));
}

// ---- Design values ---------------------------------------------------------
async function renderDesignValues(container) {
  const [groups, attrs] = await Promise.all([getList('PIMProductGroup'), getList('ProductAttributeDef')]);
  const attrOpts = options(attrs.map(a => ({ id: a.id, name: a.label || a.id })), 'Choose');
  const list = panel(`<div class="pd-bar"><select class="form-select" id="pv-grp">${options(groups, 'All designs')}</select>
    <select class="form-select" id="pv-attr">${attrOpts}</select>
    <button type="button" class="btn btn-outline" id="pv-show">Show</button>
    <button type="button" class="btn btn-primary hidden" id="pv-save">Save changes</button></div><div id="pv-grid"></div>`);
  const form = panel(`<div class="pd-form">
    <label>Group<select class="form-select" id="pb-grp">${options(groups, 'Choose')}</select></label>
    <label>Attribute<select class="form-select" id="pb-attr">${attrOpts}</select></label>
    <label>Value<input class="form-input" id="pb-val"></label>
    <div><button type="button" class="btn btn-primary" id="pb-set">Set for all designs</button></div><div id="pb-out"></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-dv', title: 'Design values', page: 'Set for group', count: 0, newLabel: 'Set For Group', form, list, heading: 'h2' }));
  txnRestorePane('pim-dv');

  const grid = list.querySelector('#pv-grid');
  const save = list.querySelector('#pv-save');
  const show = async () => {
    const attr = list.querySelector('#pv-attr').value;
    if (!attr) { grid.textContent = 'Choose an attribute.'; return; }
    const r = await apiFetch(`/api/v1/pim/design-values?group=${encodeURIComponent(list.querySelector('#pv-grp').value)}&attributes=${encodeURIComponent(attr)}`);
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The designs were not loaded.'); return; }
    const rows = (await r.json()).designs || [];
    grid.innerHTML = rows.length ? `<div class="table-wrapper"><table><thead><tr><th>Design</th><th>Name</th><th>Variants</th><th>Value</th></tr></thead><tbody>
      ${rows.map(d => `<tr><td>${esc(d.design)}</td><td>${esc(d.name)}</td><td>${d.variants}</td>
        <td><input class="form-input" data-pv-design="${esc(d.design)}" data-pv-old="${esc(d.values[attr] || '')}" value="${esc(d.values[attr] || '')}"></td></tr>`).join('')}</tbody></table></div>` : '<p class="text-muted">No designs.</p>';
    save.classList.toggle('hidden', !rows.length);
  };
  list.querySelector('#pv-show').addEventListener('click', show);
  save.addEventListener('click', async () => {
    const attr = list.querySelector('#pv-attr').value;
    const changed = [...grid.querySelectorAll('[data-pv-design]')].filter(i => i.value.trim() !== i.dataset.pvOld);
    save.disabled = true;
    for (const input of changed) {
      const r = await apiFetch('/api/v1/pim/design-values', { method: 'POST', body: JSON.stringify({ designs: [input.dataset.pvDesign], attribute: attr, value: input.value.trim() }) });
      if (!r) break;
      if (!r.ok) { showApiError(r, `${input.dataset.pvDesign} was not saved.`); break; }
      input.dataset.pvOld = input.value.trim();
    }
    save.disabled = false;
  });
  form.querySelector('#pb-set').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const out = form.querySelector('#pb-out');
    btn.disabled = true;
    const r = await apiFetch('/api/v1/pim/design-values', { method: 'POST', body: JSON.stringify({
      group: form.querySelector('#pb-grp').value, attribute: form.querySelector('#pb-attr').value, value: form.querySelector('#pb-val').value.trim() }) });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'Nothing was changed.'); return; }
    const o = await r.json();
    out.textContent = `${o.written} changed, ${o.unchanged} already had it.`;
  });

  // 146.5.5: tags on the content of a group's products.
  const tags = panel(`<h3 class="pd-sub">Tags</h3><div class="pd-form">
    <label>Group<select class="form-select" id="pt-grp">${options(groups, 'Choose')}</select></label>
    <label>Action<select class="form-select" id="pt-act"><option value="add">Add</option><option value="remove">Remove</option></select></label>
    <label>Tags<input class="form-input" id="pt-tags"></label>
    <div><button type="button" class="btn btn-primary" id="pt-go">Apply tags</button></div><div id="pt-out"></div></div>`);
  form.appendChild(tags);
  tags.querySelector('#pt-go').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const out = tags.querySelector('#pt-out');
    btn.disabled = true;
    const r = await apiFetch('/api/v1/pim/design-tags', { method: 'POST', body: JSON.stringify({
      group: tags.querySelector('#pt-grp').value, action: tags.querySelector('#pt-act').value, tags: tags.querySelector('#pt-tags').value.trim() }) });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'Nothing was changed.'); return; }
    const o = await r.json();
    out.textContent = `${o.changed} changed, ${o.unchanged} already right, ${o.no_content} without content.`;
  });
}

// ---- Checklists (146.5.10): tasks that list attributes to fill -------------
async function renderChecklists(container) {
  const [groups, attrs] = await Promise.all([getList('PIMProductGroup'), getList('ProductAttributeDef')]);
  const r0 = await apiFetch('/api/v1/pim/checklists');
  const rows = r0 && r0.ok ? ((await r0.json()).checklists || []) : [];
  const list = panel(rows.length ? `<div class="table-wrapper"><table><thead><tr><th>Task</th><th>For</th><th>Done</th><th>Due</th><th>Status</th><th></th></tr></thead><tbody>
    ${rows.map(c => `<tr><td>${esc(c.title)}</td><td>${esc(c.scope_ref)}</td><td>${c.complete} of ${c.products}${c.truncated ? '+' : ''}</td><td>${esc(c.due_date)}</td><td>${esc(c.status)}</td>
      <td><button type="button" class="btn btn-outline btn-sm" data-cl-open="${esc(c.task_id)}">Open</button></td></tr>`).join('')}</tbody></table></div><div id="cl-detail"></div>`
    : '<p class="text-muted">None yet.</p>');
  const form = panel(`<div class="pd-form">
    <label>Title<input class="form-input" id="cl-title"></label>
    <label>Group<select class="form-select" id="cl-grp">${options(groups, 'None')}</select></label>
    <label>Or product<input class="form-input" id="cl-item"></label>
    <label>Attributes<select class="form-select" id="cl-attrs" multiple size="6">${options(attrs.map(a => ({ id: a.id, name: a.label || a.id })))}</select></label>
    <label>Due<input type="date" class="form-input" id="cl-due"></label>
    <div><button type="button" class="btn btn-primary" id="cl-make">Create</button></div></div>`);
  container.appendChild(txnPagedSection({ id: 'pim-cl', title: 'Checklists', page: 'New checklist', count: rows.length, newLabel: 'New Checklist', form, list, heading: 'h2' }));
  txnRestorePane('pim-cl');

  list.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-cl-open]');
    if (!btn) return;
    const detail = list.querySelector('#cl-detail');
    const r = await apiFetch(`/api/v1/pim/checklists/${encodeURIComponent(btn.dataset.clOpen)}`);
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The checklist was not loaded.'); return; }
    const c = await r.json();
    const label = (a) => esc((c.labels && c.labels[a]) || a);
    detail.innerHTML = `<h3 class="pd-sub">${esc(c.title)}: ${c.complete} of ${c.products} done</h3><div class="table-wrapper"><table><thead><tr><th>Product</th><th>Name</th><th>Left</th></tr></thead><tbody>
      ${c.rows.map(x => `<tr${x.missing.length ? '' : ' class="text-muted"'}><td>${esc(x.item)}</td><td>${esc(x.name)}</td><td>${x.missing.length ? x.missing.map(label).join(', ') : 'Done'}</td></tr>`).join('')}</tbody></table></div>`;
  });
  form.querySelector('#cl-make').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const grp = form.querySelector('#cl-grp').value;
    const item = form.querySelector('#cl-item').value.trim();
    btn.disabled = true;
    const r = await apiFetch('/api/v1/pim/checklists', { method: 'POST', body: JSON.stringify({
      title: form.querySelector('#cl-title').value.trim(),
      scope_type: grp ? 'Product Group' : 'Product', scope_ref: grp || item,
      attributes: [...form.querySelector('#cl-attrs').selectedOptions].map(o => o.value),
      due_date: form.querySelector('#cl-due').value }) });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The checklist was not created.'); return; }
    txnDone('pim-cl');
    renderView('pim');
  });
}
