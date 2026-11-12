// Stage 47.13.1: staged imports. Native ES module, mounted under the Import
// Batch list. A file is staged first (nothing is written to the records),
// then checked, previewed, approved, committed and reconciled, each its own
// button. Rows are only written by Commit.

const STEP_LABEL = {
  parsed: 'Staged', validated: 'Checked', previewed: 'Previewed', pending_approval: 'Awaiting approval',
  approved: 'Approved', rejected: 'Rejected', committing: 'Committing', committed: 'Committed',
  reconciled: 'Reconciled', expired: 'Expired',
};
const stageLabel = s => STEP_LABEL[s] || s || '';
const num = v => escapeHTMLText(formatNumber(v || 0));
const when = v => { const d = v ? new Date(v) : null; return d && !Number.isNaN(d.getTime()) ? formatDateTime(d) : ''; };
const api = (batchID, step) => `/api/v1/import-batches${batchID ? `/${encodeURIComponent(batchID)}` : ''}${step ? `/${step}` : ''}`;

function importableTypes() {
  return (state.activeDoctypes || [])
    .filter(d => !['ImportBatch', 'ImportJob'].includes(d.name) && (typeof canCreateDoctype !== 'function' || canCreateDoctype(d.name)))
    .map(d => ({ name: d.name, label: getDoctypeLabel(d.name) }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

function samplesHTML(title, list) {
  if (!list || !list.length) return '';
  return `<h4>${escapeHTMLText(title)}</h4><ul>${list.map(s => {
    const what = (s.changes || []).map(c => `${escapeHTMLText(c.field)}: ${escapeHTMLText(c.from)} → ${escapeHTMLText(c.to)}`).join('; ');
    return `<li>Row ${num(s.row)} ${escapeHTMLText(s.id || '')} ${what} ${escapeHTMLText(s.message || '')}</li>`;
  }).join('')}</ul>`;
}

function countsHTML(c) {
  c = c || {};
  return `<p>${num(c.rows)} rows · ${num(c.valid)} valid · ${num(c.invalid)} not valid · ${num(c.duplicate)} duplicate · ${num(c.new)} new · ${num(c.update)} update` +
    `${c.committed ? ` · ${num(c.committed)} committed` : ''}${c.failed ? ` · ${num(c.failed)} refused` : ''}</p>`;
}

function reconcileHTML(r) {
  if (!r) return '';
  const issues = (r.issues || []).map(i => `<li>${escapeHTMLText(i)}</li>`).join('');
  return `<h4>${r.balanced ? 'Balanced' : 'Not balanced'}</h4>
    <p>${num(r.rows_in_file)} in file · ${num(r.committed)} committed (${num(r.created)} new, ${num(r.updated)} updated) · ${num(r.rejected)} not imported · ${num(r.keys_checked - r.keys_missing)} of ${num(r.keys_checked)} found</p>${issues ? `<ul>${issues}</ul>` : ''}`;
}

function buttonsFor(b) {
  const live = !['committed', 'reconciled', 'expired', 'rejected', 'committing'].includes(b.stage);
  const out = [];
  if (live) out.push(['validate', b.stage === 'parsed' ? 'Check' : 'Check again', b.stage === 'parsed']);
  if (live && b.stage !== 'parsed') out.push(['preview', 'Preview', b.stage === 'validated']);
  if (['validated', 'previewed'].includes(b.stage)) out.push(['approve', 'Approve', b.stage === 'previewed']);
  if (b.stage === 'approved') out.push(['commit', 'Commit', true]);
  if (['committed', 'reconciled'].includes(b.stage)) out.push(['reconcile', 'Reconcile', b.stage === 'committed']);
  if (live || b.stage === 'rejected') out.push(['discard', 'Discard', false]);
  return out;
}

async function openBatch(batchID, onChange) {
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.innerHTML = `<div class="modal-container s-e1dac7"><div id="ib-body" class="s-769fed"></div></div>`;
  document.body.appendChild(overlay);
  const body = overlay.querySelector('#ib-body');
  const close = () => { overlay.remove(); onChange(); };
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });

  let rowsFilter = '';
  async function draw(b) {
    const buttons = buttonsFor(b);
    const rowsRes = await apiFetch(`${api(batchID, 'rows')}?limit=50${rowsFilter ? `&status=${encodeURIComponent(rowsFilter)}` : ''}`);
    const rows = rowsRes && rowsRes.ok ? (await rowsRes.json()) || [] : [];
    const p = b.preview;
    body.innerHTML = `
      <div class="s-f1d51c">
        <h3>${escapeHTMLText(b.code || '')} · ${escapeHTMLText(getDoctypeLabel(b.doctype_name))}</h3>
        <button class="btn btn-outline btn-sm" id="ib-close">Close</button>
      </div>
      <p>${escapeHTMLText(b.file_name)} · ${stageLabel(b.stage)}${b.rules_current === false ? ' · rules changed, check again' : ''}</p>
      ${(b.declared_totals || []).length ? `<p>Stated: ${b.declared_totals.map(d => escapeHTMLText((d.label || d.field) + ' ' + d.expected)).join(' · ')}</p>` : ''}
      ${b.import_job_id ? `<p>Job ${escapeHTMLText(b.import_job_id)}</p>` : ''}
      ${(b.warnings || []).map(w => `<p class="text-muted">${escapeHTMLText(w)}</p>`).join('')}
      ${countsHTML(b.counts)}
      <div class="s-edee9a">${buttons.map(([step, label, primary]) =>
        `<button class="btn ${primary ? 'btn-primary' : 'btn-outline'} btn-sm" data-ib-step="${step}">${label}</button>`).join('')}</div>
      ${p ? samplesHTML('New', p.new_rows) + samplesHTML('Changes', p.updates) + samplesHTML('Duplicates', p.duplicates) + samplesHTML('Not valid', p.errors) : ''}
      ${reconcileHTML(b.reconcile)}
      <h4>Rows</h4>
      <select id="ib-filter" class="form-select s-28c0f6" aria-label="Rows">
        ${['', 'Valid', 'Invalid', 'Duplicate', 'Committed', 'Failed'].map(s => `<option value="${s}"${s === rowsFilter ? ' selected' : ''}>${s || 'All'}</option>`).join('')}
      </select>
      <div class="table-wrapper"><table>
        <thead><tr><th>Row</th><th>Status</th><th>Match</th><th>Key</th><th>Notes</th></tr></thead>
        <tbody>${rows.length === 0 ? '<tr><td colspan="5" class="text-muted">None</td></tr>' : rows.map(r => `<tr>
          <td>${num(r.row_no)}</td><td>${escapeHTMLText(r.status)}</td><td>${escapeHTMLText(r.match_kind || '')}</td>
          <td>${escapeHTMLText(r.natural_key || '')}</td><td>${escapeHTMLText((r.errors || []).join('; '))}</td></tr>`).join('')}</tbody>
      </table></div>`;
    body.querySelector('#ib-close').addEventListener('click', close);
    body.querySelector('#ib-filter').addEventListener('change', async e => { rowsFilter = e.target.value; draw(await load()); });
    body.querySelectorAll('[data-ib-step]').forEach(btn => btn.addEventListener('click', event => {
      const step = btn.dataset.ibStep;
      if (step === 'discard' && !window.confirm('Discard this batch?')) return;
      guardAgainstDoubleSubmit(event.currentTarget, 'Working…', async () => {
        const r = await apiFetch(api(batchID, step), { method: 'POST' });
        if (!r) return;
        if (!r.ok) { await showApiError(r, 'Not done.'); draw(await load()); return; }
        const nb = await r.json();
        if (step === 'discard') { close(); return; }
        draw(nb);
      });
    }));
  }
  async function load() {
    const r = await apiFetch(api(batchID));
    if (!r || !r.ok) { if (r) await showApiError(r, 'Not found.'); close(); return null; }
    return r.json();
  }
  const first = await load();
  if (first) await draw(first);
}

export async function renderImportBatchPanel(container) {
  const panel = document.createElement('div');
  container.appendChild(panel);
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

  async function refresh() {
    const res = await apiFetch(api());
    if (!res || !res.ok) { if (res) panel.remove(); return; }
    const batches = (await res.json()) || [];

    const form = document.createElement('div');
    form.className = 'table-panel';
    form.style.padding = '24px';
    form.innerHTML = `
      <div class="s-2ff289">
        <div class="form-group s-ef0b7a"><label class="form-label" for="ib-type">Record type</label>
          <select id="ib-type" class="form-select s-a20ea0">${importableTypes().map(t =>
            `<option value="${escapeHTMLText(t.name)}">${escapeHTMLText(t.label)}</option>`).join('')}</select></div>
        <div class="form-group s-ef0b7a"><label class="form-label" for="ib-file">CSV file</label>
          <input id="ib-file" type="file" accept=".csv,text/csv" class="form-input"></div>
        <div class="form-group s-ef0b7a"><label class="form-label" for="ib-totals">Stated totals</label>
          <input id="ib-totals" class="form-input s-0fac91" placeholder="amount=125000.50" autocomplete="off"></div>
      </div>
      <button class="btn btn-primary" id="ib-stage">Stage</button>`;

    const list = document.createElement('div');
    list.className = 'table-panel';
    list.innerHTML = `<div class="table-wrapper"><table>
      <thead><tr><th>Code</th><th>Record type</th><th>File</th><th>Step</th><th>Rows</th><th>Valid</th><th>Not valid</th><th>Staged</th><th></th></tr></thead>
      <tbody>${batches.length === 0 ? '<tr><td colspan="9" class="text-muted">None yet</td></tr>' : batches.map(b => `<tr>
        <td>${escapeHTMLText(b.code || '')}</td><td>${escapeHTMLText(getDoctypeLabel(b.doctype_name))}</td><td>${escapeHTMLText(b.file_name)}</td>
        <td>${escapeHTMLText(stageLabel(b.stage))}</td><td>${num(b.counts && b.counts.rows)}</td><td>${num(b.counts && b.counts.valid)}</td>
        <td>${num(b.counts && b.counts.invalid)}</td><td>${escapeHTMLText(when(b.uploaded_at))}</td>
        <td><button class="btn btn-outline btn-sm" data-ib-open="${escapeHTMLText(b.id)}">Open</button></td></tr>`).join('')}</tbody>
    </table></div>`;

    panel.innerHTML = '';
    panel.appendChild(txnPagedSection({ id: 'import-batches', title: 'Staged imports', page: 'New staged import', count: batches.length,
      newLabel: 'New staged import', form, list, heading: 'h2' }));
    txnRestorePane('import-batches');

    list.querySelectorAll('[data-ib-open]').forEach(btn => btn.addEventListener('click', () => openBatch(btn.dataset.ibOpen, refresh)));
    form.querySelector('#ib-stage').addEventListener('click', event => {
      const file = form.querySelector('#ib-file').files[0];
      const doctype = form.querySelector('#ib-type').value;
      if (!doctype) { showToast('Pick a record type.'); return; }
      if (!file) { showToast('Pick a file.'); return; }
      guardAgainstDoubleSubmit(event.currentTarget, 'Staging…', async () => {
        const body = new FormData();
        body.append('doctype', doctype);
        body.append('file', file);
        const stated = form.querySelector('#ib-totals').value.trim();
        if (stated) body.append('expected_totals', stated);
        const r = await apiUpload(api(), body);
        if (!r) return;
        if (!r.ok) { await showApiError(r, 'Not staged.'); return; }
        const b = await r.json();
        txnDone('import-batches');
        await refresh();
        openBatch(b.id, refresh);
      });
    });
  }
  await refresh();
}
