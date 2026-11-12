// Stage 148.25: months whose GSTR-1 is filed. Loaded on demand from the
// Accounting Periods list. Shared services (apiFetch, showApiError, showToast,
// renderView, escapeHTMLText, showCustomPrompt) are globals.

async function openGstr1Months() {
  document.getElementById('g1-dialog')?.remove();
  const res = await apiFetch('/api/v1/finance/gstr1-filed');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The months could not be loaded.'); return; }
  const months = (await res.json()).months || [];
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'g1-dialog';
  const rows = months.length ? months.map(m => `<tr><td>${escapeHTMLText(m.month)}</td><td>${m.locked ? 'Filed' : 'Unmarked'}</td>
      <td>${escapeHTMLText(m.filed_by)} · ${escapeHTMLText(m.filed_at)}</td>
      <td>${m.locked ? `<button type="button" class="btn btn-outline btn-sm" data-g1-unmark="${escapeHTMLText(m.month)}">Unmark</button>` : escapeHTMLText(m.unmark_reason || '')}</td></tr>`).join('')
    : '<tr><td colspan="4" class="s-306094">None yet</td></tr>';
  overlay.innerHTML = `<div class="modal-container s-5fa6d7" role="dialog" aria-modal="true" aria-labelledby="g1-title">
      <div class="modal-header"><h3 class="modal-title" id="g1-title">GSTR-1 filed months</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-g1-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body">
        <div class="s-f8e760">
          <div class="form-group s-116966"><label class="form-label" for="g1-month">Month</label><input id="g1-month" class="form-input" type="month"></div>
          <button type="button" class="btn btn-primary" data-g1-mark>Mark filed</button>
        </div>
        <table><thead><tr><th>Month</th><th>Status</th><th>Marked by</th><th></th></tr></thead><tbody>${rows}</tbody></table>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-g1-close>Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  overlay.querySelectorAll('[data-g1-close]').forEach(b => b.addEventListener('click', () => overlay.remove()));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') overlay.remove(); });
  const reopen = () => { overlay.remove(); openGstr1Months(); };
  overlay.querySelector('[data-g1-mark]').addEventListener('click', async () => {
    const month = overlay.querySelector('#g1-month').value;
    const r = await apiFetch(`/api/v1/finance/gstr1-filed/${encodeURIComponent(month || 'none')}`, { method: 'POST', body: '{}' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The month was not marked.'); return; }
    showToast('Marked filed.', { variant: 'success' });
    reopen();
  });
  overlay.querySelectorAll('[data-g1-unmark]').forEach(btn => btn.addEventListener('click', async () => {
    const reason = await showCustomPrompt('Why unmark this month?', '', 'Unmark month');
    if (!reason) return;
    const r = await apiFetch(`/api/v1/finance/gstr1-filed/${encodeURIComponent(btn.dataset.g1Unmark)}/unmark`, { method: 'POST', body: JSON.stringify({ reason }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The month was not unmarked.'); return; }
    showToast('Unmarked.', { variant: 'success' });
    reopen();
  }));
}

window.openGstr1Months = openGstr1Months;
