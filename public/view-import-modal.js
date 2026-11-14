// Bulk CSV import dialog handlers (164.4): moved out of app.js. They assign the same
// window.* names the page buttons call; app.js keeps lazy stubs until this loads.
window.openImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.inert = false;
    modal.classList.add('open');
    document.getElementById('import-result-summary').classList.add('hidden');
    // Stage 51.10: the SKU-from-Design template only exists for Item.
    const familyBtn = document.getElementById('import-family-template-btn');
    if (familyBtn) familyBtn.classList.toggle('hidden', currentDoctype !== 'Item');
  }
};

window.closeImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.classList.remove('open');
    modal.inert = true;
    document.getElementById('import-modal-form').reset();
  }
};

// The template endpoint is behind apiMiddleware, so it needs the bearer
// token. A plain anchor href sends no Authorization header, and the browser
// answers the resulting 401 with its own credential prompt - hence fetch it
// through apiFetch and hand the browser a blob it can save locally, the same
// object-URL approach the recovery-code and report exports already take.
window.downloadImportTemplate = async function(variant) {
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const variantParam = typeof variant === 'string' && variant ? `&variant=${encodeURIComponent(variant)}` : '';
  const url = `/api/v1/import/${currentDoctype}/template?tenant_id=${tenantID}${variantParam}`;

  let blob;
  try {
    const response = await apiFetch(url);
    if (!response.ok) {
      await showApiError(response, 'Could not download the import template.');
      return;
    }
    blob = await response.blob();
  } catch (err) {
    showToast('Could not download the import template. Check your connection and try again.',
      { variant: 'error', title: 'Download failed' });
    return;
  }

  const objectURL = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = objectURL;
  link.download = variantParam ? `${currentDoctype}_${variant}_template.csv` : `${currentDoctype}_template.csv`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(objectURL);
};

window.handleBulkImportSubmit = async function(e) {
  e.preventDefault();
  const fileInput = document.getElementById('import-file-input');
  if (!fileInput.files.length) return;

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);

  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';

  const headers = {
    'X-Tenant-ID': tenantID
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const summary = document.getElementById('import-result-summary');
  let res;
  try {
    res = await fetch(`/api/v1/import/${currentDoctype}`, {
      method: 'POST',
      headers,
      body: formData
    });
  } catch (err) {
    summary.classList.remove('hidden');
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Import Failed:</strong> Unable to reach the server. Please check your connection and try again.`;
    return;
  }

  if (res.ok) {
    const result = await res.json();
    summary.classList.remove('hidden');
    summary.style.backgroundColor = 'rgba(46, 213, 115, 0.1)';
    summary.style.border = '1px solid rgba(46, 213, 115, 0.3)';
    summary.style.color = '#2ed573';

    let html = `
      <div class="s-6928c9">Import Processed Successfully:</div>
      <div>Total Rows Parsed: ${result.total_rows}</div>
      <div>Created: ${(result.created_ids || []).length}</div>
      <div>Updated: ${(result.updated_ids || []).length}</div>
      <div>Failed Rows: ${result.failed_rows}</div>
      ${importTotalsHTML(result)}
    `;

    if (result.errors && result.errors.length > 0) {
      html += `<div class="s-d55493">Validation Errors:</div><ul class="s-7ac255">`;
      result.errors.forEach(err => {
        html += `<li>Row ${escapeHTMLText(err.row_number)}: ${escapeHTMLText(err.message)}</li>`;
      });
      html += `</ul>`;
      if (result.import_job_id) {
        html += `<button type="button" class="btn btn-primary btn-sm s-8a77e5" id="import-fix-resume">Fix and resume the failed rows</button>`;
      }
    }

    summary.innerHTML = html;
    // Stage 55.11: a run with failed rows stays open on its resume step.
    const resumeBtn = document.getElementById('import-fix-resume');
    if (resumeBtn) {
      resumeBtn.onclick = () => { closeImportModal(); loadViewModule('/view-import-jobs.js?v=1').then(m => m.openImportResume(result.import_job_id)); };
      return;
    }

    setTimeout(() => {
      closeImportModal();
      renderView('doctype-table');
    }, 3000);
  } else {
    summary.classList.remove('hidden');
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Import Failed:</strong> Server returned an error processing the CSV request.`;
  }
};

// Stage 47.13.3: each Number column's total, accepted of the file's
// ("Qty 5 of 10"), and a rerun that added nothing new.
function importTotalsHTML(result) {
  const c = result && result.control_totals;
  if (!c) return '';
  const cols = (c.columns || []).map(col => `${escapeHTMLText(col.label || col.field)} ${escapeHTMLText(formatNumber(col.accepted))} of ${escapeHTMLText(formatNumber(col.read))}`);
  return `${cols.length ? `<div>Totals: ${cols.join(' · ')}</div>` : ''}${c.rerun ? '<div>Rerun: nothing new</div>' : ''}`;
}

// Preview (Stage 15.2): dry-run of the same file - nothing is written,
// shows the create/update/reject breakdown before the user commits.
window.handleBulkImportPreview = async function() {
  const fileInput = document.getElementById('import-file-input');
  if (!fileInput.files.length) {
    await showCustomAlert('Select a CSV file first.', 'No File Selected');
    return;
  }

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);

  const summary = document.getElementById('import-result-summary');
  const res = await apiUpload(`/api/v1/pim/import/${currentDoctype}/preview`, formData);
  if (!res) return;

  summary.classList.remove('hidden');
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Preview Failed:</strong> ${escapeHTMLText(data.error || 'Server returned an error processing the CSV request.')}`;
    return;
  }

  const result = await res.json();
  summary.style.backgroundColor = 'rgba(255, 165, 2, 0.1)';
  summary.style.border = '1px solid rgba(255, 165, 2, 0.3)';
  summary.style.color = '#ffa502';
  let html = `
    <div class="s-6928c9">Preview (nothing written yet):</div>
    <div>Total Rows: ${escapeHTMLText(result.total_rows)}</div>
    <div>Would Create: ${(result.created_ids || []).length}</div>
    <div>Would Update: ${(result.updated_ids || []).length}</div>
    <div>Would Reject: ${escapeHTMLText(result.failed_rows)}</div>
    ${importTotalsHTML(result)}
  `;
  if (result.errors && result.errors.length > 0) {
    html += `<div class="s-ef59b6">Row Errors:</div><ul class="s-7ac255">`;
    result.errors.forEach(err => { html += `<li>Row ${escapeHTMLText(err.row_number)}: ${escapeHTMLText(err.message)}</li>`; });
    html += `</ul>`;
  }
  summary.innerHTML = html;
};
