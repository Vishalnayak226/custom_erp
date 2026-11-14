// Ask 170.35 / 170.36: GSTR-1 JSON download and GSTR-2B import and match.
// Opened from the GST Return Summary screen; loaded on demand. Shared
// services (apiFetch, showApiError, showToast, escapeHTMLText) are globals.

const G2B_STATUSES = ['', 'Matched', 'Mismatch', 'In 2B not in books', 'In books not in 2B'];
const G2B_DECISIONS = ['', 'Accepted', 'Rejected', 'Query raised'];

function gstrFilesMonthNow() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

async function openGstrFiles() {
  document.getElementById('gf-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'gf-dialog';
  overlay.innerHTML = `<div class="modal-container s-5fa6d7" role="dialog" aria-modal="true" aria-labelledby="gf-title">
      <div class="modal-header"><h3 class="modal-title" id="gf-title">GSTR-1 and GSTR-2B</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-gf-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body">
        <div class="form-group"><label class="form-label" for="gf-month">Month</label><input id="gf-month" class="form-input" type="month" value="${gstrFilesMonthNow()}"></div>
        <h4>GSTR-1</h4>
        <div class="form-group"><label class="form-label" for="gf-gstin">GSTIN</label><input id="gf-gstin" class="form-input" maxlength="15"></div>
        <button type="button" class="btn btn-primary" data-gf-g1>Download JSON</button>
        <div id="gf-g1-note"></div>
        <h4>GSTR-2B</h4>
        <div class="form-group"><label class="form-label" for="gf-file">GSTR-2B JSON</label><input id="gf-file" class="form-input" type="file" accept=".json,application/json"></div>
        <button type="button" class="btn btn-primary" data-gf-import>Import</button>
        <button type="button" class="btn btn-outline" data-gf-reconcile>Match again</button>
        <div class="form-group"><label class="form-label" for="gf-status">Status</label>
          <select id="gf-status" class="form-input">${G2B_STATUSES.map(s => `<option value="${escapeHTMLText(s)}">${escapeHTMLText(s || 'All')}</option>`).join('')}</select></div>
        <div id="gf-lines"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-gf-close>Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const month = () => document.getElementById('gf-month').value;
  const close = () => overlay.remove();
  overlay.addEventListener('click', async (e) => {
    const t = e.target;
    if (t === overlay || t.closest('[data-gf-close]')) { close(); return; }
    if (t.closest('[data-gf-g1]')) await gstrDownload(month());
    else if (t.closest('[data-gf-import]')) await gstr2bImport(month());
    else if (t.closest('[data-gf-reconcile]')) {
      const res = await apiFetch(`/api/v1/gstr2b/reconcile?month=${encodeURIComponent(month())}`, { method: 'POST' });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'The month could not be matched.'); return; }
      showToast('Matched', 'success');
      await gstr2bLoadLines();
    }
  });
  overlay.addEventListener('change', async (e) => {
    if (e.target.id === 'gf-status' || e.target.id === 'gf-month') await gstr2bLoadLines();
    const sel = e.target.closest('[data-gf-decide]');
    if (sel) {
      const res = await apiFetch(`/api/v1/gstr2b/lines/${encodeURIComponent(sel.dataset.gfDecide)}/decision`, { method: 'POST', body: JSON.stringify({ decision: sel.value }) });
      if (res && !res.ok) await showApiError(res, 'The decision could not be saved.');
    }
  });
  await gstr2bLoadLines();
}

async function gstrDownload(month) {
  const gstin = document.getElementById('gf-gstin').value.trim().toUpperCase();
  const res = await apiFetch(`/api/v1/reports/gstr1-json?gstin=${encodeURIComponent(gstin)}&month=${encodeURIComponent(month)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The GSTR-1 file could not be made.'); return; }
  const out = await res.json();
  const blob = new Blob([JSON.stringify(out.file, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `GSTR1_${gstin}_${out.file.fp}.json`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  const r = out.reconciliation || {};
  const notes = (out.warnings || []).map(w => `<li>${escapeHTMLText(w)}</li>`).join('');
  document.getElementById('gf-g1-note').innerHTML = `<p>${r.matches ? 'Totals agree with the GST return summary.' : 'Totals differ from the GST return summary.'}</p>${notes ? `<ul>${notes}</ul>` : ''}`;
}

async function gstr2bImport(month) {
  const file = document.getElementById('gf-file').files[0];
  if (!file) { showToast('Choose the GSTR-2B JSON file', 'error'); return; }
  const res = await apiFetch(`/api/v1/gstr2b/import?month=${encodeURIComponent(month)}`, { method: 'POST', body: await file.text() });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The file could not be imported.'); return; }
  const out = await res.json();
  showToast(`${out.imported} lines imported`, 'success');
  await gstr2bLoadLines();
}

async function gstr2bLoadLines() {
  const box = document.getElementById('gf-lines');
  if (!box) return;
  const month = document.getElementById('gf-month').value;
  const status = document.getElementById('gf-status').value;
  const res = await apiFetch(`/api/v1/reports/run/gstr2b-reconciliation?month=${encodeURIComponent(month)}&status=${encodeURIComponent(status)}`);
  if (!res) return;
  if (!res.ok) { box.innerHTML = ''; return; }
  const rows = (await res.json()).rows || [];
  if (!rows.length) { box.innerHTML = '<p>No lines for this month.</p>'; return; }
  box.innerHTML = `<table><thead><tr><th>Status</th><th>Supplier</th><th>Invoice</th><th>Date</th><th>Value</th><th>Tax</th><th>Why</th><th>Decision</th></tr></thead><tbody>${rows.map(r => `<tr>
      <td>${escapeHTMLText(r.status)}</td><td>${escapeHTMLText(r.supplier_gstin)}</td><td>${escapeHTMLText(r.doc_number)}</td><td>${escapeHTMLText(r.doc_date || '')}</td>
      <td>${escapeHTMLText(String(r.total ?? ''))}</td><td>${escapeHTMLText(String(r.tax ?? ''))}</td><td>${escapeHTMLText(r.reason || '')}</td>
      <td>${r.source === '2B' ? `<select class="form-input" data-gf-decide="${escapeHTMLText(r.id)}">${G2B_DECISIONS.map(d => `<option value="${escapeHTMLText(d)}"${d === (r.decision || '') ? ' selected' : ''}>${escapeHTMLText(d || '-')}</option>`).join('')}</select>` : ''}</td></tr>`).join('')}</tbody></table>`;
}

window.openGstrFiles = openGstrFiles;
