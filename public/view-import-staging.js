// Stage 47.13.1: staged imports. Native ES module, mounted under the Import
// Batch list. A file is staged first (nothing is written to the records),
// then checked, previewed, approved, committed and reconciled, each its own
// button. Rows are only written by Commit.
//
// 297.2: a large file goes up in pieces that survive a dropped line and a
// closed tab, and its rows are read, checked and committed by a background
// job. The batch shows the job's bar while it runs (so does the Tasks bar),
// and a step that stopped - a failed job, a server restart - is resumed from
// where it got to.

const STEP_LABEL = {
  staging: 'Reading', parsed: 'Staged', validating: 'Checking', validated: 'Checked', previewed: 'Previewed',
  pending_approval: 'Awaiting approval', approved: 'Approved', rejected: 'Rejected', committing: 'Committing',
  committed: 'Committed', reconciled: 'Reconciled', expired: 'Expired',
};
const WORK_LABEL = { stage: 'Reading', validate: 'Checking', commit: 'Committing', reconcile: 'Reconciling' };
const stageLabel = s => STEP_LABEL[s] || s || '';
const num = v => escapeHTMLText(formatNumber(v || 0));
const when = v => { const d = v ? new Date(v) : null; return d && !Number.isNaN(d.getTime()) ? formatDateTime(d) : ''; };
const api = (batchID, step) => `/api/v1/import-batches${batchID ? `/${encodeURIComponent(batchID)}` : ''}${step ? `/${step}` : ''}`;
const workPct = w => (w && w.total > 0 ? Math.min(100, Math.floor((w.done || 0) * 100 / w.total)) : -1);
const tasksLocal = t => loadViewModule('/task-progress.js').then(m => m.tasksLocal(t)).catch(() => {});

// A background job's bar in the Tasks bar too, so the user can leave the screen.
function trackWork(b) {
  const id = b && b.work && b.work.job_id;
  if (id && !String(id).startsWith('inline:') && b.work_live) trackTask(`job:${id}`, `Import · ${b.code || b.file_name || ''}`);
}

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

// workHTML: the step running on the batch - its bar while it runs, where it
// stopped when it did not finish.
function workHTML(b) {
  const w = b.work;
  if (!w) return '';
  const what = WORK_LABEL[w.step] || w.step || '';
  const pct = workPct(w);
  // While reading, the total is guessed from the file's size.
  const count = `${num(w.done)}${w.total ? ` of ${w.step === 'stage' ? 'about ' : ''}${num(w.total)}` : ''} rows`;
  if (!b.work_live) {
    return `<p class="${w.error ? 'text-danger' : 'text-muted'}">${escapeHTMLText(what)} stopped · ${count}${w.error ? ` · ${escapeHTMLText(w.error)}` : ''}</p>`;
  }
  return `<div class="ib-progress"><div>${escapeHTMLText(what)} · ${count}${pct >= 0 ? ` · ${pct}%` : ''}</div>
    <progress max="100"${pct >= 0 ? ` value="${pct}"` : ''} aria-label="${escapeHTMLText(what)}"></progress></div>`;
}

function buttonsFor(b) {
  if (b.work) {
    // A step is on the batch: it runs (nothing else to ask) or it stopped (resume it).
    const out = [];
    if (!b.work_live) out.push(['resume', 'Resume', true]);
    if (!['approved', 'committing', 'committed', 'reconciled', 'expired'].includes(b.stage)) out.push(['discard', 'Discard', false]);
    return out;
  }
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
  let timer = null;
  const close = () => { clearTimeout(timer); overlay.remove(); onChange(); };
  overlay.addEventListener('click', e => { if (e.target === overlay) close(); });

  // While a step runs the batch is read again every few seconds (slower in a
  // hidden tab) until it ends; the rows are listed once it has.
  function follow(b) {
    clearTimeout(timer);
    if (!b || !b.work || !b.work_live) return;
    timer = setTimeout(async () => {
      if (!overlay.isConnected) return;
      const nb = await load();
      if (nb && overlay.isConnected) draw(nb);
    }, document.hidden ? 15000 : 3000);
  }

  let rowsFilter = '';
  async function draw(b) {
    const buttons = buttonsFor(b);
    const running = !!(b.work && b.work_live);
    let rows = [];
    if (!running) {
      const rowsRes = await apiFetch(`${api(batchID, 'rows')}?limit=50${rowsFilter ? `&status=${encodeURIComponent(rowsFilter)}` : ''}`);
      rows = rowsRes && rowsRes.ok ? (await rowsRes.json()) || [] : [];
    }
    if (!overlay.isConnected) return;
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
      ${workHTML(b)}
      ${countsHTML(b.counts)}
      <div class="s-edee9a">${buttons.map(([step, label, primary]) =>
        `<button class="btn ${primary ? 'btn-primary' : 'btn-outline'} btn-sm" data-ib-step="${step}">${label}</button>`).join('')}</div>
      ${p ? samplesHTML('New', p.new_rows) + samplesHTML('Changes', p.updates) + samplesHTML('Duplicates', p.duplicates) + samplesHTML('Not valid', p.errors) : ''}
      ${reconcileHTML(b.reconcile)}
      ${running ? '' : `<h4>Rows</h4>
      <select id="ib-filter" class="form-select s-28c0f6" aria-label="Rows">
        ${['', 'Valid', 'Invalid', 'Duplicate', 'Committed', 'Failed'].map(s => `<option value="${s}"${s === rowsFilter ? ' selected' : ''}>${s || 'All'}</option>`).join('')}
      </select>
      <div class="table-wrapper"><table>
        <thead><tr><th>Row</th><th>Status</th><th>Match</th><th>Key</th><th>Notes</th></tr></thead>
        <tbody>${rows.length === 0 ? '<tr><td colspan="5" class="text-muted">None</td></tr>' : rows.map(r => `<tr>
          <td>${num(r.row_no)}</td><td>${escapeHTMLText(r.status)}</td><td>${escapeHTMLText(r.match_kind || '')}</td>
          <td>${escapeHTMLText(r.natural_key || '')}</td><td>${escapeHTMLText((r.errors || []).join('; '))}</td></tr>`).join('')}</tbody>
      </table></div>`}`;
    body.querySelector('#ib-close').addEventListener('click', close);
    body.querySelector('#ib-filter')?.addEventListener('change', async e => { rowsFilter = e.target.value; const nb = await load(); if (nb) draw(nb); });
    body.querySelectorAll('[data-ib-step]').forEach(btn => btn.addEventListener('click', event => {
      const step = btn.dataset.ibStep;
      if (step === 'discard' && !window.confirm('Discard this batch?')) return;
      guardAgainstDoubleSubmit(event.currentTarget, 'Working…', async () => {
        const r = await apiFetch(api(batchID, step), { method: 'POST' });
        if (!r) return;
        if (!r.ok) { await showApiError(r, 'Not done.'); const nb = await load(); if (nb) draw(nb); return; }
        const nb = await r.json();
        if (step === 'discard') { close(); return; }
        trackWork(nb);
        draw(nb);
      });
    }));
    follow(b);
  }
  async function load() {
    const r = await apiFetch(api(batchID));
    if (!r || !r.ok) { if (r) await showApiError(r, 'Not found.'); close(); return null; }
    return r.json();
  }
  const first = await load();
  if (first) await draw(first);
}

// ---- 297.2: a large file goes up in pieces -----------------------------------
//
// A file over RESUMABLE_MIN is sent as a resumable upload: pieces of 64 KB to
// 1 MB sized to the line (a quick piece doubles the next, a slow one halves
// it), each sent again after a dropped line until it lands. The server keeps
// what it has, so a reload or a closed tab carries on from there when the same
// file is chosen again. The bar shows here and in the Tasks bar.

const RESUMABLE_MIN = 1 << 20;
const PIECE_MIN = 64 << 10;
const PIECE_MAX = 1 << 20;
const PIECE_TIMEOUT_MS = 120000;
const WAITS_MS = [1000, 2000, 5000, 10000, 20000, 30000];
const SERVER_TRIES = 10;
const sending = new Map(); // storage key -> {name, pct, note}: uploads running in this tab
let upBox = null;          // the open screen's upload area, painted on each piece

const upPrefix = () => `erp_upload:${localStorage.getItem('erp_tenant_id') || 'default'}:${localStorage.getItem('erp_username') || ''}:`;
const upKey = (doctype, f) => `${upPrefix()}${doctype}:${f.name}:${f.size}:${f.lastModified}`;
function lsGet(k) { try { return JSON.parse(localStorage.getItem(k) || 'null'); } catch (_) { return null; } }
function lsPut(k, v) {
  try { if (v) localStorage.setItem(k, JSON.stringify(v)); else localStorage.removeItem(k); } catch (_) { /* private window: no carrying on after a reload */ }
}
const mb = n => (n / 1048576).toFixed(n < 10 * 1048576 ? 1 : 0);

// pausedUploads: uploads this user started in this browser that are not
// running now; the server keeps an unfinished one for a day.
function pausedUploads() {
  const out = [];
  try {
    const pre = upPrefix();
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const k = localStorage.key(i);
      if (!k || !k.startsWith(pre) || sending.has(k)) continue;
      const v = lsGet(k);
      if (!v || Date.now() - (v.at || 0) > 23 * 3600 * 1000) { lsPut(k, null); continue; }
      out.push(v);
    }
  } catch (_) { /* */ }
  return out;
}

function paintUploads() {
  if (!upBox || !upBox.isConnected) return;
  const running = [...sending.values()].map(u => `<div class="ib-progress"><div>${escapeHTMLText(u.name)} · ${escapeHTMLText(u.note)}</div>
    <progress max="100" value="${u.pct}" aria-label="${escapeHTMLText(u.name)}"></progress></div>`);
  const paused = pausedUploads().map(u => `<p class="text-muted">Paused · ${escapeHTMLText(u.name)} · ${Math.floor((u.received || 0) * 100 / (u.size || 1))}% · choose it again</p>`);
  upBox.innerHTML = running.join('') + paused.join('');
}

function waitFor(ms, stop) {
  return new Promise(resolve => {
    const done = () => { clearTimeout(t); window.removeEventListener('online', done); stop.removeEventListener('abort', done); resolve(); };
    const t = setTimeout(done, ms);
    window.addEventListener('online', done); // the line is back: try at once
    stop.addEventListener('abort', done);
  });
}

// sendPiece goes straight through fetch: apiFetch stops the user with an
// alert on a dropped line, and here a dropped line only means "send it again".
async function sendPiece(id, offset, blob, stop) {
  const headers = { 'Content-Type': 'application/octet-stream', 'Upload-Offset': String(offset), 'X-Tenant-ID': localStorage.getItem('erp_tenant_id') || 'default' };
  const token = localStorage.getItem('erp_token');
  if (token) headers.Authorization = `Bearer ${token}`;
  const ctl = new AbortController();
  const onStop = () => ctl.abort();
  stop.addEventListener('abort', onStop);
  const timer = setTimeout(() => ctl.abort(), PIECE_TIMEOUT_MS);
  try {
    return await fetch(`/api/v1/uploads/${encodeURIComponent(id)}`, { method: 'PUT', headers, body: blob, signal: ctl.signal });
  } catch (_) {
    return null;
  } finally {
    clearTimeout(timer);
    stop.removeEventListener('abort', onStop);
  }
}

// sendFile uploads f and stages it. It returns the batch, or null when the
// user stopped it or the server refused it (the reason already shown).
async function sendFile(f, doctype, stated) {
  const key = upKey(doctype, f);
  if (sending.has(key)) { showToast('This file is going up already.'); return null; }
  const stop = new AbortController();
  const taskID = `upload:${doctype}:${f.name}:${f.size}`;
  const label = `Upload · ${f.name}`;
  const me = { name: f.name, pct: 0, note: 'Starting' };
  sending.set(key, me);
  const show = (note, pct, eta) => {
    me.note = note;
    me.pct = pct;
    paintUploads();
    tasksLocal({ id: taskID, label, state: 'running', pct, note, eta_sec: eta || 0, onCancel: () => stop.abort() });
  };
  show('Starting', 0);
  try {
    let saved = lsGet(key);
    let id = saved && saved.id;
    let received = 0;
    if (id) {
      const r = await apiFetch(`/api/v1/uploads/${encodeURIComponent(id)}`);
      if (!r) return null;
      if (r.ok) received = (await r.json()).received || 0;
      else id = ''; // left over a day: start again
    }
    if (!id) {
      const r = await apiFetch('/api/v1/uploads', { method: 'POST', body: JSON.stringify({ file_name: f.name, size: f.size, doctype, purpose: 'import' }) });
      if (!r) return null;
      if (!r.ok) { await showApiError(r, 'Not uploaded.'); return null; }
      id = (await r.json()).id;
    }
    saved = { id, name: f.name, size: f.size, doctype, received, at: Date.now() };
    lsPut(key, saved);
    let piece = 256 << 10;
    let tries = 0;
    let serverErrors = 0;
    const startedAt = Date.now();
    const from = received;
    while (received < f.size) {
      if (stop.signal.aborted) return null;
      const end = Math.min(f.size, received + piece);
      const t0 = Date.now();
      const res = await sendPiece(id, received, f.slice(received, end), stop.signal);
      if (stop.signal.aborted) return null;
      if (res && res.ok) {
        const at = Number(res.headers.get('Upload-Offset'));
        received = Number.isFinite(at) && at > 0 ? at : end;
        tries = 0;
        serverErrors = 0;
        const took = Date.now() - t0;
        if (took < 2000) piece = Math.min(PIECE_MAX, piece * 2);
        else if (took > 8000) piece = Math.max(PIECE_MIN, piece / 2);
        saved.received = received;
        saved.at = Date.now();
        lsPut(key, saved);
        const rate = (received - from) / Math.max(1, (Date.now() - startedAt) / 1000);
        show(`${mb(received)} of ${mb(f.size)} MB`, Math.floor(received * 100 / f.size), rate > 0 ? Math.round((f.size - received) / rate) : 0);
        continue;
      }
      if (res && res.status === 409 && res.headers.get('Upload-Offset') != null) {
        received = Number(res.headers.get('Upload-Offset')) || 0; // the server says where it got to
        continue;
      }
      if (res && res.status === 401) { await apiFetch(`/api/v1/uploads/${encodeURIComponent(id)}`); return null; } // signs out as any call does
      if (res && res.status === 429) { await waitFor((Number(res.headers.get('Retry-After')) || 5) * 1000, stop.signal); continue; }
      if (res && res.status >= 400 && res.status < 500) { await showApiError(res, 'Not uploaded.'); lsPut(key, null); return null; }
      if (res && ++serverErrors >= SERVER_TRIES) { await showApiError(res, 'Not uploaded.'); return null; } // kept: choose it again later
      // A dropped line, a piece too slow or the server busy: a smaller piece, after a wait.
      piece = Math.max(PIECE_MIN, piece / 2);
      show(navigator.onLine === false ? 'Waiting for the line' : 'Sending again', Math.floor(received * 100 / f.size), 0);
      await waitFor(WAITS_MS[Math.min(tries++, WAITS_MS.length - 1)], stop.signal);
    }
    show('Reading', 100, 0);
    const r = await apiFetch(api(), { method: 'POST', body: JSON.stringify({ upload_id: id, doctype, expected_totals: stated || '' }) });
    if (!r) return null;
    if (!r.ok) { await showApiError(r, 'Not staged.'); return null; }
    lsPut(key, null);
    return await r.json();
  } finally {
    sending.delete(key);
    tasksLocal({ id: taskID, state: 'gone' });
    paintUploads();
  }
}

export async function renderImportBatchPanel(container) {
  const panel = document.createElement('div');
  container.appendChild(panel);
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  let listTimer = null;

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
      <p class="text-muted" id="ib-order"></p>
      <div id="ib-up"></div>
      <button class="btn btn-primary" id="ib-stage">Stage</button>`;

    const list = document.createElement('div');
    list.className = 'table-panel';
    const stepText = b => {
      if (!b.work) return stageLabel(b.stage);
      const pct = workPct(b.work);
      return `${WORK_LABEL[b.work.step] || stageLabel(b.stage)}${b.work_live ? (pct >= 0 ? ` · ${pct}%` : '') : ' · stopped'}`;
    };
    const listHTML = bs => `<div class="table-wrapper"><table>
      <thead><tr><th>Code</th><th>Record type</th><th>File</th><th>Step</th><th>Rows</th><th>Valid</th><th>Not valid</th><th>Staged</th><th></th></tr></thead>
      <tbody>${bs.length === 0 ? '<tr><td colspan="9" class="text-muted">None yet</td></tr>' : bs.map(b => `<tr>
        <td>${escapeHTMLText(b.code || '')}</td><td>${escapeHTMLText(getDoctypeLabel(b.doctype_name))}</td><td>${escapeHTMLText(b.file_name)}</td>
        <td>${escapeHTMLText(stepText(b))}</td><td>${num(b.counts && b.counts.rows)}</td><td>${num(b.counts && b.counts.valid)}</td>
        <td>${num(b.counts && b.counts.invalid)}</td><td>${escapeHTMLText(when(b.uploaded_at))}</td>
        <td><button class="btn btn-outline btn-sm" data-ib-open="${escapeHTMLText(b.id)}">Open</button></td></tr>`).join('')}</tbody>
    </table></div>`;
    const bindOpen = () => list.querySelectorAll('[data-ib-open]').forEach(btn => btn.addEventListener('click', () => openBatch(btn.dataset.ibOpen, refresh)));
    // While a batch works in the background its row follows the step: the
    // list alone (never the New form) is read again every few seconds, slower
    // in a hidden tab, until none is working. An open batch reads itself.
    const followList = bs => {
      clearTimeout(listTimer);
      if (!bs.some(b => b.work && b.work_live)) return;
      listTimer = setTimeout(async () => {
        if (!list.isConnected) return;
        if (document.getElementById('ib-body')) { followList(bs); return; }
        const r = await apiFetch(api()).catch(() => null);
        if (!r || !r.ok || !list.isConnected) return;
        const nb = (await r.json()) || [];
        list.innerHTML = listHTML(nb);
        bindOpen();
        followList(nb);
      }, document.hidden ? 15000 : 3000);
    };
    list.innerHTML = listHTML(batches);

    panel.innerHTML = '';
    panel.appendChild(txnPagedSection({ id: 'import-batches', title: 'Staged imports', page: 'New staged import', count: batches.length,
      newLabel: 'New staged import', form, list, heading: 'h2' }));
    txnRestorePane('import-batches');
    upBox = form.querySelector('#ib-up');
    paintUploads();

    bindOpen();
    followList(batches);
    // Stage 170.42: what to load first for the chosen record type.
    const showOrder = async () => {
      const box = form.querySelector('#ib-order');
      const t = form.querySelector('#ib-type').value;
      box.textContent = '';
      try {
        const r = await apiFetch('/api/v1/import/' + encodeURIComponent(t) + '/load-order');
        if (!r || !r.ok) return;
        const steps = ((await r.json()).steps) || [];
        if (steps.length) box.textContent = 'Load first: ' + steps.map(x => x.doctype + (x.loaded ? '' : ' (empty)')).join(', ');
      } catch (_) { /* the order is a hint only */ }
    };
    form.querySelector('#ib-type').addEventListener('change', showOrder);
    showOrder();
    form.querySelector('#ib-stage').addEventListener('click', event => {
      const file = form.querySelector('#ib-file').files[0];
      const doctype = form.querySelector('#ib-type').value;
      if (!doctype) { showToast('Pick a record type.'); return; }
      if (!file) { showToast('Pick a file.'); return; }
      const stated = form.querySelector('#ib-totals').value.trim();
      guardAgainstDoubleSubmit(event.currentTarget, file.size > RESUMABLE_MIN ? 'Uploading…' : 'Staging…', async () => {
        let b;
        if (file.size > RESUMABLE_MIN) {
          b = await sendFile(file, doctype, stated);
          if (!b) return;
        } else {
          const body = new FormData();
          body.append('doctype', doctype);
          body.append('file', file);
          if (stated) body.append('expected_totals', stated);
          const r = await apiUpload(api(), body);
          if (!r) return;
          if (!r.ok) { await showApiError(r, 'Not staged.'); return; }
          b = await r.json();
        }
        trackWork(b);
        if (!panel.isConnected) return; // the user moved on: the Tasks bar has it
        txnDone('import-batches');
        await refresh();
        openBatch(b.id, refresh);
      });
    });
  }
  await refresh();
}
