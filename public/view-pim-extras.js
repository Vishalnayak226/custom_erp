// Stage 146.5.12 / 146.5.13 - PIM item page: typed relations and "Live on" links.
// Lazy: loaded by view-pim.js when an item's PIM page opens.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const RELATION_TYPES = ['Similar', 'Complete the look', 'Alternative', 'Upsell', 'Cross-sell', 'Add-on'];

export async function renderPIMItemExtras(container, itemCode) {
  await renderLiveOn(container, itemCode);
  await renderRelations(container, itemCode);
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
