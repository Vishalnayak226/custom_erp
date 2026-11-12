// Stage 148.26: price tools for customer price lists. Loaded on demand by the
// Price list versions screen. Shared services (apiFetch, showApiError,
// showToast, guardAgainstDoubleSubmit, attachLinkTypeahead, formatMoney,
// escapeHTMLText) are globals.

function ptPost(path, body) {
  return apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
}

window.openPriceTools = function openPriceTools() {
  document.getElementById('pt-overlay')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pt-overlay';
  overlay.innerHTML = `
    <div class="modal-container s-5fa6d7" role="dialog" aria-modal="true" aria-labelledby="pt-title">
      <div class="modal-header"><h3 class="modal-title" id="pt-title">Price tools</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-pt-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-63bcac">
        <div class="form-group"><label class="form-label" for="pt-version">Price list version</label>
          <input id="pt-version" class="form-input" autocomplete="off" placeholder="Draft version"></div>
        <div class="form-group"><label class="form-label" for="pt-ending">Ending</label>
          <select id="pt-ending" class="form-select"><option value="99">99</option><option value="49">49</option><option value="9">9</option><option value="0">Whole rupee</option><option value="none">None</option></select></div>
        <div id="pt-rows"></div>
        <hr>
        <div class="form-group"><label class="form-label" for="pt-order">Margin hold</label>
          <input id="pt-order" class="form-input" autocomplete="off" placeholder="Order no.">
          <input id="pt-reason" class="form-input s-fe7b49" autocomplete="off" placeholder="Reason"></div>
        <div id="pt-flags"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-pt-close>Close</button>
        <button type="button" class="btn btn-outline" id="pt-release">Release hold</button>
        <button type="button" class="btn btn-outline" id="pt-preview">Preview</button>
        <button type="button" class="btn btn-primary" id="pt-apply" disabled>Apply</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const q = (id) => overlay.querySelector(id);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-pt-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  attachLinkTypeahead(q('#pt-version'), 'PriceListVersion', { keepCode: true });
  const body = () => ({ version_id: q('#pt-version').value.trim(), ending: q('#pt-ending').value });
  q('#pt-preview').addEventListener('click', () => guardAgainstDoubleSubmit(q('#pt-preview'), 'Loading...', async () => {
    q('#pt-apply').disabled = true;
    const res = await ptPost('/api/v1/pricing/endings/preview', body());
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'No preview.'); return; }
    const rows = (await res.json()).rows || [];
    const changed = rows.filter(r => r.to !== r.from);
    q('#pt-rows').innerHTML = changed.length
      ? `<div class="table-panel"><table class="data-table"><thead><tr><th>Item</th><th class="num">Now</th><th class="num">After</th></tr></thead><tbody>${
        changed.map(r => `<tr><td>${escapeHTMLText(r.name || '')}</td><td class="num">${formatMoney(r.from)}</td><td class="num">${formatMoney(r.to)}</td></tr>`).join('')}</tbody></table></div>`
      : '<div class="text-muted">Nothing changes.</div>';
    q('#pt-apply').disabled = !changed.length;
  }));
  q('#pt-apply').addEventListener('click', () => guardAgainstDoubleSubmit(q('#pt-apply'), 'Applying...', async () => {
    const res = await ptPost('/api/v1/pricing/endings/apply', body());
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Not applied.'); return; }
    const out = await res.json();
    showToast(`${out.changed} prices changed.`, { variant: 'success' });
    close();
    if (typeof renderView === 'function') renderView('doctype-table');
  }));
  q('#pt-release').addEventListener('click', () => guardAgainstDoubleSubmit(q('#pt-release'), 'Releasing...', async () => {
    const id = q('#pt-order').value.trim();
    if (!id) { showToast('Enter the order no.', { variant: 'error' }); return; }
    const res = await ptPost(`/api/v1/orders/${encodeURIComponent(id)}/margin-release`, { reason: q('#pt-reason').value.trim() });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Not released.'); return; }
    showToast('Hold released.', { variant: 'success' });
  }));
  apiFetch('/api/v1/pricing/flags').then(async res => {
    if (!res || !res.ok) return;
    const lines = (await res.json()).lines || [];
    if (!lines.length) return;
    q('#pt-flags').innerHTML = `<div class="form-label">Price changed on open lines</div><div class="table-panel"><table class="data-table"><thead><tr><th>Order</th><th>Customer</th><th>Item</th><th class="num">Rate</th></tr></thead><tbody>${
      lines.map(l => `<tr><td>${escapeHTMLText(l.order_id)}</td><td>${escapeHTMLText(l.customer || '')}</td><td>${escapeHTMLText(l.sku)}</td><td class="num">${formatMoney(l.rate)}</td></tr>`).join('')}</tbody></table></div>`;
  });
};
