// Stage 128: Settings -> Configuration -> Setup Workbook. A whole
// organisation's setup from one Excel file: download it (blank, or this
// organisation's setup as it is now), fill it in, upload it, read the
// preview, apply. Uploading the same file again changes nothing; a changed
// cell changes only that value. Server: internal/server/setup_workbook.go.

const SW_API = '/api/v1/admin/setup-workbook';
const swEsc = s => escapeHTMLText(String(s == null ? '' : s));

const SW_BADGES = {
  new: ['badge-success', 'New'],
  change: ['badge-warning', 'Change'],
  same: ['badge-secondary', 'Unchanged'],
  error: ['badge-danger', 'Problem'],
  note: ['badge-secondary', 'Note'],
  done: ['badge-success', 'Done'],
  failed: ['badge-danger', 'Failed']
};

let swFile = null;   // the workbook chosen for upload
let swPoll = null;   // the run being followed

function swBadge(kind) {
  const [cls, text] = SW_BADGES[kind] || ['badge-secondary', kind];
  return `<span class="badge ${cls}">${swEsc(text)}</span>`;
}

async function swDownload(path, fallbackName, btn) {
  if (btn) btn.disabled = true;
  const res = await apiFetch(path, { timeoutMs: 60000 });
  if (btn) btn.disabled = false;
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The workbook could not be downloaded.'); return; }
  const disposition = res.headers.get('Content-Disposition') || '';
  const m = disposition.match(/filename="([^"]+)"/);
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = m ? m[1] : fallbackName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function renderSetupWorkbook(host) {
  if (swPoll) { clearTimeout(swPoll); swPoll = null; }
  swFile = null;
  host.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; margin:0 0 14px;">Setup Workbook</h3>
    <div style="display:grid; gap:18px; max-width:980px;">
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">1. Download</h4>
        <div style="display:flex; gap:10px; flex-wrap:wrap;">
          <button type="button" class="btn btn-outline" data-sw="template">Blank workbook</button>
          <button type="button" class="btn btn-outline" data-sw="export">This organisation's setup</button>
        </div>
      </section>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">2. Upload and check</h4>
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
          <label class="btn btn-outline" style="cursor:pointer; margin:0;">Choose workbook
            <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" data-sw="file" style="display:none;">
          </label>
          <span data-sw="file-name" class="text-muted" style="font-size:13px;">No file chosen</span>
          <button type="button" class="btn btn-primary" data-sw="preview" disabled>Preview</button>
        </div>
      </section>
      <div data-sw="result"></div>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">Past uploads</h4>
        <div data-sw="runs"><p class="text-muted">Loading…</p></div>
      </section>
    </div>`;

  host.querySelector('[data-sw="template"]').addEventListener('click', e => swDownload(`${SW_API}/template`, 'setup-workbook-template.xlsx', e.currentTarget));
  host.querySelector('[data-sw="export"]').addEventListener('click', e => swDownload(`${SW_API}/export`, 'setup-workbook.xlsx', e.currentTarget));
  host.querySelector('[data-sw="file"]').addEventListener('change', e => {
    swFile = e.target.files && e.target.files[0] ? e.target.files[0] : null;
    host.querySelector('[data-sw="file-name"]').textContent = swFile ? swFile.name : 'No file chosen';
    host.querySelector('[data-sw="preview"]').disabled = !swFile;
    host.querySelector('[data-sw="result"]').innerHTML = '';
    e.target.value = '';
  });
  host.querySelector('[data-sw="preview"]').addEventListener('click', e => swPreview(host, e.currentTarget));
  swRenderRuns(host);
}

async function swPreview(host, btn) {
  if (!swFile) return;
  if (swFile.size > 8 * 1024 * 1024) { await showCustomAlert('The workbook is larger than 8 MB.', 'Setup Workbook'); return; }
  const out = host.querySelector('[data-sw="result"]');
  out.innerHTML = '<p class="text-muted">Reading the workbook…</p>';
  btn.disabled = true;
  const fd = new FormData();
  fd.append('file', swFile, swFile.name);
  const res = await apiUpload(`${SW_API}/preview`, fd);
  btn.disabled = false;
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${swEsc(await getErrorMessage(res, 'The workbook could not be read.'))}</p>`; return; }
  const plan = await res.json();
  const t = plan.totals || {};
  const toApply = (t.New || 0) + (t.Change || 0);
  out.innerHTML = `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">3. Preview - nothing has changed yet</h4>
      ${swTotalsHtml(t, false)}
      ${plan.from_organisation && !plan.same_organisation ? `<p style="font-size:13px; margin:0 0 10px;">This workbook was downloaded from another organisation (${swEsc(plan.from_organisation)}): its codes are ignored and rows are matched by name, GSTIN and the other keys.</p>` : ''}
      ${swSheetsHtml(plan.sheets || [], false)}
      <div style="display:flex; gap:12px; align-items:center; margin-top:12px; flex-wrap:wrap;">
        <button type="button" class="btn btn-primary" data-sw="apply"${toApply ? '' : ' disabled'}>${toApply ? `Apply ${toApply} row${toApply === 1 ? '' : 's'}` : 'Nothing to apply'}</button>
        ${t.Errors ? `<span style="font-size:13px; color:var(--danger-color);">${t.Errors} row${t.Errors === 1 ? ' has a problem and is' : 's have a problem and are'} left out - fix and upload again.</span>` : ''}
      </div>
    </section>`;
  swWireToggles(out);
  const apply = out.querySelector('[data-sw="apply"]');
  if (apply) apply.addEventListener('click', () => swApply(host, toApply, t.Errors || 0));
}

async function swApply(host, count, problems) {
  const msg = `Apply ${count} row${count === 1 ? '' : 's'} from ${swFile.name}?` +
    (problems ? ` The ${problems} row${problems === 1 ? '' : 's'} with a problem ${problems === 1 ? 'is' : 'are'} left out.` : '') +
    ' Nothing is deleted, and an existing user\'s password is never changed.';
  if (!await showCustomConfirm(msg, 'Setup Workbook')) return;
  const out = host.querySelector('[data-sw="result"]');
  out.innerHTML = '<p class="text-muted">Starting…</p>';
  const fd = new FormData();
  fd.append('file', swFile, swFile.name);
  const res = await apiUpload(`${SW_API}/apply`, fd);
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${swEsc(await getErrorMessage(res, 'The workbook could not be applied.'))}</p>`; return; }
  const { run_id: runID } = await res.json();
  swFollow(host, runID);
}

// swFollow shows a run's progress until it finishes, then its result.
async function swFollow(host, runID) {
  const out = host.querySelector('[data-sw="result"]');
  if (!out || !out.isConnected) return;
  const res = await apiFetch(`${SW_API}/runs/${encodeURIComponent(runID)}`);
  if (!res || !res.ok) { out.innerHTML = '<p class="text-muted">The run could not be read - see Past uploads.</p>'; return; }
  const run = await res.json();
  if (run.status === 'Running') {
    const done = run.progress_done || 0, total = run.progress_total || 0;
    out.innerHTML = `<p style="font-size:14px;">Applying${run.progress_sheet ? ` <b>${swEsc(run.progress_sheet)}</b>` : ''}… ${total ? `${done} of ${total} sheets` : ''}</p>`;
    swPoll = setTimeout(() => swFollow(host, runID), 1200);
    return;
  }
  swPoll = null;
  out.innerHTML = swRunHtml(run);
  swWireToggles(out);
  swRenderRuns(host);
}

function swRunHtml(run) {
  const t = run.totals || {};
  const ok = run.status === 'Completed';
  return `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">${ok ? 'Applied' : swEsc(run.status || 'Finished')}${run.file_name ? ` - ${swEsc(run.file_name)}` : ''}</h4>
      ${run.message ? `<p style="font-size:13px;">${swEsc(run.message)}</p>` : ''}
      ${swTotalsHtml(t, true)}
      ${(t.Failed || t.Errors) ? '<p style="font-size:13px; margin:0 0 10px;">Fix the rows marked Failed or Problem in the workbook and upload it again - rows already applied show as Unchanged.</p>' : ''}
      ${swSheetsHtml(run.sheets || [], true)}
    </section>`;
}

function swTotalsHtml(t, applied) {
  const parts = applied
    ? [['done', t.Done], ['failed', t.Failed], ['error', t.Errors], ['same', t.Same], ['note', t.Notes]]
    : [['new', t.New], ['change', t.Change], ['same', t.Same], ['error', t.Errors], ['note', t.Notes]];
  return `<div style="display:flex; gap:14px; flex-wrap:wrap; margin:0 0 12px; font-size:13px;">${parts
    .filter(([, n]) => n)
    .map(([k, n]) => `<span>${swBadge(k)} ${swEsc(n)}</span>`).join('') || '<span class="text-muted">The workbook has no rows.</span>'}</div>`;
}

function swRowKind(r, applied) {
  if (applied && r.result) return r.result;
  return r.action;
}

function swChangesHtml(r) {
  const lines = (r.changes || []).map(c => c.from
    ? `${swEsc(c.field)}: <span class="text-muted">${swEsc(c.from)}</span> → ${swEsc(c.to)}`
    : `${swEsc(c.field)}: ${swEsc(c.to)}`);
  if (r.message) lines.push(`<span${r.action === 'error' || r.result === 'failed' ? ' style="color:var(--danger-color);"' : ''}>${swEsc(r.message)}</span>`);
  return lines.join('<br>');
}

// One table per sheet that the workbook has; unchanged rows are folded away.
function swSheetsHtml(sheets, applied) {
  const present = sheets.filter(s => s.present);
  if (!present.length) return '<p class="text-muted">None of the workbook\'s sheets have rows.</p>';
  return present.map(s => {
    const rows = s.rows || [];
    const shown = rows.filter(r => r.action !== 'same');
    const same = rows.length - shown.length;
    const counts = applied
      ? [['done', s.done], ['failed', s.failed], ['error', s.errors], ['same', s.same]]
      : [['new', s.new], ['change', s.change], ['error', s.errors], ['same', s.same], ['note', s.notes]];
    const open = shown.some(r => r.action === 'error' || r.result === 'failed');
    return `
      <details class="table-panel" style="margin:0 0 10px; padding:0; min-height:0; display:block;"${open ? ' open' : ''}>
        <summary style="padding:10px 14px; cursor:pointer; display:flex; gap:12px; align-items:center; flex-wrap:wrap;">
          <b style="min-width:140px;">${swEsc(s.sheet)}</b>
          ${counts.filter(([, n]) => n).map(([k, n]) => `<span style="font-size:13px;">${swBadge(k)} ${swEsc(n)}</span>`).join('') || '<span class="text-muted" style="font-size:13px;">no rows</span>'}
        </summary>
        ${rows.length ? `
        <div style="overflow-x:auto;">
          <table>
            <thead><tr><th style="width:60px;">Row</th><th>Record</th><th style="width:110px;"></th><th>What changes</th></tr></thead>
            <tbody>
              ${shown.map(r => `<tr><td>${swEsc(r.row || '')}</td><td>${swEsc(r.key)}</td><td>${swBadge(swRowKind(r, applied))}</td><td style="font-size:13px;">${swChangesHtml(r)}</td></tr>`).join('')}
              ${same ? `<tr data-sw-same-row><td colspan="4" class="text-muted" style="font-size:13px;"><a href="#" data-sw-show-same>Show ${same} unchanged</a></td></tr>` : ''}
              ${rows.filter(r => r.action === 'same').map(r => `<tr data-sw-same class="hidden"><td>${swEsc(r.row || '')}</td><td>${swEsc(r.key)}</td><td>${swBadge('same')}</td><td></td></tr>`).join('')}
            </tbody>
          </table>
        </div>` : ''}
      </details>`;
  }).join('');
}

function swWireToggles(root) {
  root.querySelectorAll('[data-sw-show-same]').forEach(a => a.addEventListener('click', e => {
    e.preventDefault();
    const body = a.closest('tbody');
    body.querySelectorAll('[data-sw-same]').forEach(tr => tr.classList.remove('hidden'));
    a.closest('[data-sw-same-row]').remove();
  }));
}

async function swRenderRuns(host) {
  const box = host.querySelector('[data-sw="runs"]');
  if (!box) return;
  const res = await apiFetch(`${SW_API}/runs`);
  if (!res || !res.ok) { box.innerHTML = '<p class="text-muted">Past uploads could not be loaded.</p>'; return; }
  const runs = await res.json();
  if (!runs.length) { box.innerHTML = '<p class="text-muted">No workbook has been applied here yet.</p>'; return; }
  box.innerHTML = `
    <div class="table-panel" style="overflow-x:auto; min-height:0;">
      <table>
        <thead><tr><th>When</th><th>File</th><th>By</th><th>Status</th><th>Done</th><th>Failed</th><th></th></tr></thead>
        <tbody>${runs.map(r => {
          // started_at is UTC; created_at is the database's local clock without a zone.
          const at = r.started_at || r.created_at;
          const when = at ? new Date(at).toLocaleString('en-IN', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
          const bad = r.status === 'Failed' || r.status === 'Interrupted' || r.status === 'Completed with errors';
          return `<tr>
            <td>${swEsc(when)}</td><td>${swEsc(r.file_name || '')}</td><td>${swEsc(r.created_by || '')}</td>
            <td><span class="badge ${bad ? 'badge-warning' : r.status === 'Running' ? 'badge-secondary' : 'badge-success'}">${swEsc(r.status || '')}</span></td>
            <td>${swEsc(r.success_rows || 0)}</td><td>${swEsc(r.failed_rows || 0)}</td>
            <td style="white-space:nowrap;">
              <button type="button" class="btn btn-outline btn-sm" data-sw-run="${swEsc(r.id)}">Details</button>
              <button type="button" class="btn btn-outline btn-sm" data-sw-file="${swEsc(r.id)}">Download file</button>
            </td></tr>`;
        }).join('')}</tbody>
      </table>
    </div>`;
  box.querySelectorAll('[data-sw-run]').forEach(b => b.addEventListener('click', () => {
    swFollow(host, b.getAttribute('data-sw-run'));
    host.querySelector('[data-sw="result"]').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }));
  box.querySelectorAll('[data-sw-file]').forEach(b => b.addEventListener('click', e =>
    swDownload(`${SW_API}/runs/${encodeURIComponent(b.getAttribute('data-sw-file'))}/file`, 'setup-workbook.xlsx', e.currentTarget)));
}
