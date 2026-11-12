// Stage 146.4.5: a channel's listings. Pull, preview a link by SKU / barcode,
// link the matches, and switch stock sync per listing. Opened from the channel
// health table in the OMS view.

export async function showChannelListings(host, channel) {
  const base = `/api/v1/marketplace/channels/${encodeURIComponent(channel)}/listings`;
  const esc = escapeHTMLText;
  host.querySelector('[data-channel-listings-panel]')?.remove();
  const panel = document.createElement('div');
  panel.dataset.channelListingsPanel = '1';
  panel.className = 'table-panel';
  panel.style.cssText = 'padding:12px 16px;margin-top:10px;';
  host.appendChild(panel);

  const load = async (filter = '') => {
    const res = await apiFetch(`${base}${filter ? '?filter=' + encodeURIComponent(filter) : ''}`);
    if (!res || !res.ok) { if (res) await showApiError(res, 'Listings could not be loaded.'); return; }
    const { listings = [], can_pull: canPull } = await res.json();
    panel.innerHTML = `<div class="s-73d754">
        <strong>${esc(channel)} listings</strong>
        <select class="form-select s-30e741" data-cl-filter>
          ${[['', 'All'], ['unlinked', 'Not linked'], ['linked', 'Linked'], ['off', 'Sync off']].map(([v, t]) => `<option value="${v}"${v === filter ? ' selected' : ''}>${t}</option>`).join('')}</select>
        ${canPull ? '<button class="btn btn-outline btn-sm" data-cl-pull>Pull</button>' : ''}
        <button class="btn btn-outline btn-sm" data-cl-preview>Link by SKU / barcode</button>
        <button class="btn btn-outline btn-sm" data-cl-close>Close</button></div>
      <div data-cl-preview-box></div>
      ${listings.length === 0 ? `<p class="text-muted s-8a77e5">${canPull ? 'None pulled yet.' : 'This channel cannot list.'}</p>` : `
      <div class="table-wrapper s-8a77e5"><table><thead><tr><th>Listing</th><th>SKU</th><th>Barcode</th><th>Price</th><th>Item</th><th>State</th><th>Stock sync</th></tr></thead>
        <tbody>${listings.map(l => `<tr><td>${esc(l.title || l.channel_sku)}</td><td>${esc(l.channel_sku || '—')}</td><td>${esc(l.barcode || '—')}</td>
          <td>${Number(l.price || 0).toFixed(2)}</td><td>${l.sku ? esc(l.sku) + (l.item_name ? ' · ' + esc(l.item_name) : '') : '<span class="badge badge-warning">Not linked</span>'}</td>
          <td>${l.state === 'Listed' ? 'Listed' : `<span class="badge badge-danger">${esc(l.state)}</span>`}</td>
          <td><label><input type="checkbox" data-cl-sync="${esc(l.ref)}"${l.stock_sync ? ' checked' : ''}> On</label></td></tr>`).join('')}</tbody></table></div>`}`;
    panel.querySelector('[data-cl-close]').addEventListener('click', () => panel.remove());
    panel.querySelector('[data-cl-filter]').addEventListener('change', e => load(e.target.value));
    panel.querySelector('[data-cl-pull]')?.addEventListener('click', async (e) => {
      e.target.disabled = true;
      const r = await apiFetch(`${base}/pull`, { method: 'POST' });
      if (r && !r.ok) await showApiError(r, 'Could not pull the listings.');
      await load(filter);
    });
    panel.querySelectorAll('[data-cl-sync]').forEach(box => box.addEventListener('change', async () => {
      const r = await apiFetch(`${base}/stock-sync`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ref: box.dataset.clSync, on: box.checked }) });
      if (!r || !r.ok) { box.checked = !box.checked; if (r) await showApiError(r, 'Could not save.'); }
    }));
    panel.querySelector('[data-cl-preview]').addEventListener('click', () => preview(filter));
  };

  const preview = async (filter) => {
    const res = await apiFetch(`${base}/link-preview`);
    if (!res || !res.ok) { if (res) await showApiError(res, 'Could not preview.'); return; }
    const pv = await res.json();
    const box = panel.querySelector('[data-cl-preview-box]');
    const rows = pv.rows || [];
    box.innerHTML = `<div class="s-8a77e5"><span class="badge badge-success">${pv.match} match</span>
      <span class="badge badge-danger">${pv.conflict} conflict</span> <span class="badge badge-warning">${pv.unlinked} not found</span>
      ${pv.match ? '<button class="btn btn-primary btn-sm" data-cl-commit>Link matches</button>' : ''}</div>
      ${rows.length ? `<div class="table-wrapper"><table><thead><tr><th>Listing</th><th>SKU</th><th>Barcode</th><th>Result</th><th>Item</th><th>Note</th></tr></thead>
        <tbody>${rows.map(r => `<tr><td>${esc(r.title || r.channel_sku)}</td><td>${esc(r.channel_sku || '—')}</td><td>${esc(r.barcode || '—')}</td>
          <td>${r.result === 'Match' ? 'Match' : `<span class="badge ${r.result === 'Conflict' ? 'badge-danger' : 'badge-warning'}">${esc(r.result)}</span>`}</td>
          <td>${esc(r.item || '—')}</td><td>${esc(r.reason || (r.matched_by ? 'by ' + r.matched_by : ''))}</td></tr>`).join('')}</tbody></table></div>` : ''}`;
    box.querySelector('[data-cl-commit]')?.addEventListener('click', async () => {
      const refs = rows.filter(r => r.result === 'Match').map(r => r.ref);
      const r = await apiFetch(`${base}/link`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refs }) });
      if (r && !r.ok) { await showApiError(r, 'Could not link.'); return; }
      await load(filter);
    });
  };

  await load();
}
