// Stage 55.11: resume an import from its failed rows. Native ES module,
// opened from an import's result (PIM import templates, the record list's
// Bulk Import), from My work and from the Import Job list. Download the
// job's failed rows, fix them, check them, resume - rows that already
// imported are never run again.

function importWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString();
}

async function downloadFailedRows(jobID) {
  const res = await apiFetch(`/api/v1/import-jobs/${encodeURIComponent(jobID)}/failed-rows.csv`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The failed rows could not be downloaded.'); return; }
  const url = URL.createObjectURL(await res.blob());
  const link = document.createElement('a');
  link.href = url;
  link.download = `failed_rows_${jobID}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

function summaryHTML(job) {
  return `<p><strong>${escapeHTMLText(job.id)}</strong> - ${escapeHTMLText(job.template ? `template ${job.template}` : job.doctype || '')}, ${escapeHTMLText(job.status)}${job.resumed_from ? ` (resumed from ${escapeHTMLText(job.resumed_from)})` : ''}</p>
    <p>${escapeHTMLText(String(job.total_rows))} row(s): ${escapeHTMLText(String(job.success_rows))} imported, ${escapeHTMLText(String(job.failed_rows))} failed.
      ${job.keeps_failed_rows ? `<strong>${escapeHTMLText(String(job.open_failed_rows))} still to fix</strong>${job.resolved_rows ? `, ${escapeHTMLText(String(job.resolved_rows))} fixed since` : ''}.` : 'This job was recorded before failed rows were kept, so it cannot be resumed - correct and re-import the original file.'}</p>
    ${(job.resumes || []).length ? `<ul>${job.resumes.map(r => `<li>${escapeHTMLText(importWhen(r.at))}: ${escapeHTMLText(String(r.imported))} of ${escapeHTMLText(String(r.attempted))} imported by ${escapeHTMLText(r.by || '')} (job ${escapeHTMLText(r.job)})</li>`).join('')}</ul>` : ''}`;
}

async function openImportResume(jobID) {
  const res = await apiFetch(`/api/v1/import-jobs/${encodeURIComponent(jobID)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'This import job could not be opened.'); return; }
  let job = await res.json();
  document.getElementById('import-resume-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'import-resume-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Fix and resume import</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body">
        <div id="import-resume-summary">${summaryHTML(job)}</div>
        <div id="import-resume-steps"${job.resumable ? '' : ' hidden'}>
          <ol style="padding-left:18px;margin:8px 0;">
            <li><button type="button" class="btn btn-outline btn-sm" id="import-resume-download">Download failed rows</button> - each row with its reason; keep the <code>original_row</code> column.</li>
            <li>Fix the rows (delete any you will not import now), then choose the file:
              <input type="file" id="import-resume-file" accept=".csv" class="form-input" aria-label="Corrected failed rows CSV" style="margin-top:6px;"></li>
            <li><button type="button" class="btn btn-outline btn-sm" id="import-resume-check">Check</button> validates without writing, then <button type="button" class="btn btn-primary btn-sm" id="import-resume-run" disabled>Resume</button> imports only these rows.</li>
          </ol>
        </div>
        <div id="import-resume-result" role="status"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary" data-import-resume-close>Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-import-resume-close]').addEventListener('click', close);
  overlay.querySelector('#import-resume-download')?.addEventListener('click', () => downloadFailedRows(jobID));
  const fileInput = overlay.querySelector('#import-resume-file');
  const runButton = overlay.querySelector('#import-resume-run');
  const resultEl = overlay.querySelector('#import-resume-result');
  fileInput?.addEventListener('change', () => { runButton.disabled = true; resultEl.textContent = ''; });

  const send = async (dryRun, button) => {
    const file = fileInput.files[0];
    if (!file) { resultEl.textContent = 'Choose the corrected failed-rows file first.'; fileInput.focus(); return; }
    const body = new FormData();
    body.append('file', file);
    await guardAgainstDoubleSubmit(button, dryRun ? 'Checking…' : 'Resuming…', async () => {
      const r = await apiUpload(`/api/v1/import-jobs/${encodeURIComponent(jobID)}/resume${dryRun ? '?dry_run=1' : ''}`, body);
      if (!r) return;
      if (!r.ok) { resultEl.textContent = ''; await showApiError(r, dryRun ? 'The file could not be checked.' : 'The rows were not resumed.'); return; }
      const out = await r.json();
      const errors = out.errors || [];
      resultEl.innerHTML = `<p><strong>${dryRun ? 'Check only - nothing written' : 'Resumed'}</strong>: ${escapeHTMLText(String(out.total_rows))} row(s), ${escapeHTMLText(String((out.created_ids || []).length))} created, ${escapeHTMLText(String((out.updated_ids || []).length))} updated, ${escapeHTMLText(String(out.failed_rows || 0))} still failing.</p>
        ${errors.length ? `<ul>${errors.slice(0, 20).map(e => `<li>Row ${escapeHTMLText(String(e.row_number))} of this file: ${escapeHTMLText(e.message)}</li>`).join('')}</ul>` : ''}`;
      if (dryRun) {
        runButton.disabled = (out.failed_rows || 0) > 0 || out.total_rows < 1;
        if (runButton.disabled && out.total_rows > 0) resultEl.insertAdjacentHTML('beforeend', '<p>Fix these and check again - or delete those rows from the file to resume the rest now.</p>');
      } else {
        runButton.disabled = true;
        fileInput.value = '';
        if (out.job) {
          job = out.job;
          overlay.querySelector('#import-resume-summary').innerHTML = summaryHTML(job);
          overlay.querySelector('#import-resume-steps').hidden = !job.resumable;
        }
      }
    });
  };
  overlay.querySelector('#import-resume-check')?.addEventListener('click', e => send(true, e.currentTarget));
  runButton?.addEventListener('click', e => send(false, e.currentTarget));
  (overlay.querySelector('#import-resume-download') || overlay.querySelector('[data-import-resume-close]')).focus();
}

// The Import Job list's lookup: open a job by its id.
function renderImportJobPanel(host) {
  host.innerHTML = `
    <div class="table-panel" style="padding:16px 24px;margin-bottom:24px;">
      <h2 style="font-size:16px;margin:0 0 8px;">Fix and resume an import</h2>
      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="import-job-lookup">Import job</label>
          <input id="import-job-lookup" class="form-input" style="width:240px;" placeholder="IMPJOB-…"></div>
        <button class="btn btn-outline" id="import-job-open">Open</button>
      </div>
    </div>`;
  const input = host.querySelector('#import-job-lookup');
  const open = () => { const id = input.value.trim(); if (id) openImportResume(id); };
  host.querySelector('#import-job-open').addEventListener('click', open);
  input.addEventListener('keydown', e => { if (e.key === 'Enter') open(); });
  if (typeof attachLinkTypeahead === 'function') attachLinkTypeahead(input, 'ImportJob');
  let wanted = '';
  try { wanted = sessionStorage.getItem('erp_open_import_job') || ''; sessionStorage.removeItem('erp_open_import_job'); } catch (_) { /* storage off */ }
  if (wanted) { input.value = wanted; openImportResume(wanted); }
}

export { openImportResume, renderImportJobPanel };
