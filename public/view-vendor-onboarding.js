// Vendor onboarding: request, approve or reject a vendor's status from the
// vendor list. Loaded on demand by the Vendor list; shared services (apiFetch,
// showApiError, showToast, renderView, escapeHTMLText, showCustomPrompt) are globals.

async function voJson(path, opts) {
  const res = await apiFetch(path, opts);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'Not saved.'); return null; }
  return res.json();
}

async function voPendingRequest(vendorId) {
  const rows = await voJson(`/api/v1/doc/VendorOnboarding?vendor_id=${encodeURIComponent(vendorId)}&limit=50`);
  return (Array.isArray(rows) ? rows : []).find(r => r.status === 'Pending Approval') || null;
}

function voButtons(status, pending) {
  if (pending) {
    return `<button type="button" class="btn btn-primary" data-vo-do="Approved">Approve</button>
      <button type="button" class="btn btn-outline" data-vo-do="Rejected">Reject</button>`;
  }
  if (status === 'Approved') return '<button type="button" class="btn btn-outline" data-vo-ask="Blocked">Request block</button>';
  return '<button type="button" class="btn btn-primary" data-vo-ask="Approved">Request approval</button>';
}

export async function openVendorOnboarding(vendorId, status) {
  const pending = await voPendingRequest(vendorId);
  document.getElementById('vo-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'vo-dialog';
  const summary = pending ? (pending.summary || `Waiting: ${pending.target_status || ''}`) : '';
  overlay.innerHTML = `
    <div class="modal-container vo-dialog" role="dialog" aria-modal="true" aria-labelledby="vo-title">
      <div class="modal-header"><h3 class="modal-title" id="vo-title">${escapeHTMLText(vendorId)} · ${escapeHTMLText(status || 'Approved')}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-vo-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body">${summary ? `<p class="vo-note">${escapeHTMLText(summary)}</p>` : ''}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-vo-close>Close</button>${voButtons(status, pending)}</div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-vo-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  overlay.querySelectorAll('[data-vo-ask]').forEach(b => b.addEventListener('click', async () => {
    b.disabled = true;
    const out = await voJson('/api/v1/procurement/vendor-onboarding/request', { method: 'POST', body: JSON.stringify({ vendor_id: vendorId, decision: b.dataset.voAsk }) });
    if (out) { showToast('Sent for approval.', { variant: 'success' }); close(); renderView('doctype-table'); } else b.disabled = false;
  }));
  overlay.querySelectorAll('[data-vo-do]').forEach(b => b.addEventListener('click', async () => {
    const decision = b.dataset.voDo;
    let comment = '';
    if (decision === 'Rejected') {
      comment = (await showCustomPrompt('Reason for rejection:')) || '';
      if (!comment.trim()) return;
    }
    b.disabled = true;
    const out = await voJson('/api/v1/approval/decide', { method: 'POST', body: JSON.stringify({ doctype: 'VendorOnboarding', document_id: pending.id, decision, comment }) });
    if (out) { showToast(decision === 'Approved' ? 'Approved.' : 'Rejected.', { variant: 'success' }); close(); renderView('doctype-table'); } else b.disabled = false;
  }));
}

