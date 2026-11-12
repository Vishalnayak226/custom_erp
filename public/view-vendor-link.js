// Stage 148.18: the vendor's signed link on a purchase order, a dialog opened by
// the list's "Vendor link" button. Shows "Not turned on" while the setting
// "procurement.vendor_link" is off. Server side: engines/vendor_po_link.go.

const e = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const q = v => (v == null ? '' : v);

export function openVendorLink(poId, poNumber) {
  if (!poId) return;
  document.getElementById('vl-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'vl-dialog';
  overlay.innerHTML = `<div class="modal-container s-fddc9b" role="dialog" aria-modal="true" aria-labelledby="vl-title">
    <div class="modal-header"><h3 class="modal-title" id="vl-title">Vendor link · ${e(poNumber || '')}</h3>
      <button type="button" class="modal-close" aria-label="Close dialog" data-vl-close>&times;</button></div>
    <div class="modal-body vl-body">Loading…</div>
    <div class="modal-footer"><button type="button" class="btn btn-outline" data-vl-close>Close</button></div></div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-vl-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', ev => { if (ev.key === 'Escape') close(); });
  requestAnimationFrame(() => overlay.classList.add('open'));
  const host = overlay.querySelector('.vl-body');
  const base = `/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/vendor-link`;

  async function post(path, payload, okMsg) {
    const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(payload || {}) });
    if (!res || !res.ok) { await showApiError(res, 'That did not work.'); return false; }
    if (okMsg) showToast(okMsg, { variant: 'success' });
    return true;
  }

  function paint(s) {
    const link = s.link;
    const props = s.proposals || [];
    const files = s.files || [];
    host.innerHTML = `
      <div class="s-15a923">
        ${link ? `<input type="text" readonly class="vl-url s-58d2ad" value="${e(link.url || location.origin + link.path)}" aria-label="Link">
          <button type="button" class="btn" data-vl="copy">Copy</button>
          <button type="button" class="btn" data-vl="revoke">Revoke</button>`
          : '<button type="button" class="btn" data-vl="make">Make link</button>'}
        ${link ? `<span class="s-d59bb5">To ${e(link.expires)} · opened ${e(s.opened)}${s.last_open ? ` · last ${e(s.last_open)}` : ''}${s.trusted ? ' · trusted' : ''}</span>` : ''}
      </div>
      <table><thead><tr><th>Line</th><th class="num">Qty</th><th class="num">New qty</th><th>Due</th><th>New date</th><th>Unit</th><th>Note</th><th>Status</th><th></th></tr></thead><tbody>${
        props.length ? props.map(p => `<tr>
          <td>${e(p.item || p.line)}</td><td class="num">${e(p.cur_qty)}</td><td class="num">${e(q(p.qty))}</td>
          <td>${e(p.cur_due)}</td><td>${e(q(p.date))}</td>
          <td>${e(q(p.vendor_unit))}${p.vendor_unit && !p.unit_known && p.status === 'Pending' ? ` <label><input type="checkbox" data-vl-unit="${p.id}"> Remember</label>` : ''}</td>
          <td>${e(q(p.note))}</td><td>${e(p.status)}${p.auto ? ' (auto)' : ''}<br><small>${e(p.at)}</small></td>
          <td>${p.status === 'Pending' ? `<button type="button" class="btn" data-vl-apply="${p.id}">Apply</button> <button type="button" class="btn" data-vl-reject="${p.id}">Reject</button>` : ''}</td></tr>`).join('')
          : '<tr><td colspan="9" class="s-d59bb5">No changes yet</td></tr>'}</tbody></table>
      ${files.length ? `<table class="s-8a77e5"><thead><tr><th>Invoice</th><th>Date</th><th class="num">Amount</th><th>File</th><th>Status</th><th></th></tr></thead><tbody>${files.map(f => `<tr>
        <td>${e(q(f.invoice_no))}</td><td>${e(q(f.invoice_date))}</td><td class="num">${e(q(f.amount))}</td>
        <td><a href="#" data-vl-file="${f.id}" data-name="${e(f.name)}">${e(f.name)}</a></td><td>${e(f.status)}<br><small>${e(f.at)}</small></td>
        <td>${f.status === 'Pending review' ? `<button type="button" class="btn" data-vl-review="Reviewed" data-id="${f.id}">Reviewed</button> <button type="button" class="btn" data-vl-review="Rejected" data-id="${f.id}">Reject</button>` : ''}</td></tr>`).join('')}</tbody></table>` : ''}`;
  }

  async function load() {
    const res = await apiFetch(base);
    if (!res) { close(); return; }
    if (!res.ok) { close(); await showApiError(res, 'The vendor link could not be loaded.'); return; }
    const s = await res.json();
    if (!s.enabled) { host.textContent = 'Not turned on'; return; }
    paint(s);
  }

  host.addEventListener('click', async ev => {
    const t = ev.target.closest('button, a');
    if (!t) return;
    const d = t.dataset;
    if (d.vl === 'make') { if (await post(base, {}, 'Link ready')) load(); }
    else if (d.vl === 'revoke') { if (await post(`${base}/revoke`, {}, 'Link revoked')) load(); }
    else if (d.vl === 'copy') {
      const input = host.querySelector('.vl-url');
      input.select();
      try { await navigator.clipboard.writeText(input.value); showToast('Copied', { variant: 'success' }); } catch (_) { document.execCommand('copy'); }
    } else if (d.vlApply) {
      const box = host.querySelector(`[data-vl-unit="${d.vlApply}"]`);
      if (await post(`/api/v1/procurement/vendor-link/proposal/${d.vlApply}/apply`, { remember_unit: !!(box && box.checked) }, 'Applied')) load();
    } else if (d.vlReject) {
      if (await post(`/api/v1/procurement/vendor-link/proposal/${d.vlReject}/reject`, {}, 'Rejected')) load();
    } else if (d.vlReview) {
      if (await post(`/api/v1/procurement/vendor-link/file/${d.id}/review`, { status: d.vlReview }, d.vlReview)) load();
    } else if (d.vlFile) {
      ev.preventDefault();
      const res = await apiFetch(`/api/v1/procurement/vendor-link/file/${d.vlFile}`);
      if (!res || !res.ok) { await showApiError(res, 'The file could not be opened.'); return; }
      const url = URL.createObjectURL(await res.blob());
      const a = document.createElement('a');
      a.href = url; a.download = d.name || 'invoice';
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 5000);
    }
  });

  load();
}
