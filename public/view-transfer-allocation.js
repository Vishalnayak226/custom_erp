// Stage 146.3.12: Stock Movement > Allocation. One file (store, sku, qty) makes
// one Draft transfer per store; Preview checks the rows first and shows what
// the source cannot cover. Submit and Approve then act on the whole run
// through the ordinary transfer approval.
// Ask 237.7: "Download sheet" gives a posted goods receipt's SKUs with a
// column per store (and sets From to the receipt's location); the file may
// be that .xlsx or a CSV. A run makes the good rows' Drafts; "Rows to fix"
// downloads the rest with a reason, to upload again under a new run ID.

const esc = escapeHTMLText;
const API = '/api/v1/transfer/allocation';
const VQ = new URL(import.meta.url).search;
let taFile = null;
let taRun = '';
let taBody = null;

export async function renderTransferAllocationView(container) {
  const res = await apiFetch('/api/v1/doc/Location');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Locations could not be loaded.'); return; }
  // 276.12: From reads "name (code)"; the code is what is sent.
  const locs = (await res.json()).map(l => { const d = l.data || l; return { code: d.code || l.id, name: d.name || '' }; })
    .filter(l => l.code).sort((a, b) => (a.name || a.code).localeCompare(b.name || b.code));
  taFile = null; taRun = ''; taBody = null;
  // 276.12: the standard page header, like every other screen.
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = '<div class="page-title-section"><h1 class="page-title">Allocation</h1></div>';
  container.appendChild(header);
  const box = document.createElement('div');
  box.className = 'table-panel';
  box.style.padding = '24px';
  box.innerHTML = `
    <div class="s-7e6838">
      <div class="form-group"><label class="form-label" for="ta-grn">From a goods receipt</label><input id="ta-grn" class="form-input" autocomplete="off" placeholder="GRN no."></div>
    </div>
    <div class="s-881549"><button class="btn" id="ta-grn-sheet" disabled>Download sheet</button></div>
    <div class="s-7e6838">
      <div class="form-group"><label class="form-label" for="ta-run">Run ID</label><input id="ta-run" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="ta-from">From</label>
        <select id="ta-from" class="form-select">${locs.map(l => `<option value="${esc(l.code)}">${esc(l.name ? `${l.name} (${l.code})` : l.code)}</option>`).join('')}</select></div>
      <div class="form-group"><label class="form-label" for="ta-purpose">Purpose</label>
        <select id="ta-purpose" class="form-select"><option value="">Replenishment</option><option>New store</option><option>Launch allocation</option></select></div>
    </div>
    <div class="form-group"><span class="form-label">File (Excel or CSV)</span>
      <div><label class="btn btn-outline">Choose file<input id="ta-file" type="file" hidden accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"></label>
        <span id="ta-file-name" class="text-muted s-5e0faa">No file chosen</span></div></div>
    <div class="s-881549">
      <button class="btn" id="ta-preview" disabled>Preview</button>
      <button class="btn btn-primary" id="ta-make" disabled>Make Drafts</button>
    </div>
    <div id="ta-out"></div>
    <div id="ta-runbox"></div>`;
  container.appendChild(box);
  const q = (s) => document.getElementById(s);
  q('ta-grn').addEventListener('input', () => { q('ta-grn-sheet').disabled = !q('ta-grn').value.trim(); });
  q('ta-grn-sheet').addEventListener('click', () => taGRNSheet(q('ta-grn-sheet')));
  q('ta-file').addEventListener('change', (e) => {
    taFile = e.target.files[0] || null;
    q('ta-file-name').textContent = taFile ? taFile.name : 'No file chosen';
    q('ta-preview').disabled = q('ta-make').disabled = !taFile;
  });
  q('ta-preview').addEventListener('click', () => taSend(true));
  q('ta-make').addEventListener('click', () => taSend(false));
}

async function taSave(blob, name) {
  const { downloadBlob } = await import('./print-pdf.js' + VQ);
  downloadBlob(blob, name);
}

async function taGRNSheet(btn) {
  const grn = document.getElementById('ta-grn').value.trim();
  if (!grn) return;
  btn.disabled = true;
  const res = await apiFetch(`${API}/grn-sheet?grn=${encodeURIComponent(grn)}`, { timeoutMs: 60000 });
  btn.disabled = false;
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The sheet could not be made.'); return; }
  const from = res.headers.get('X-Allocation-From') || '';
  const sel = document.getElementById('ta-from');
  if (from && [...sel.options].some(o => o.value === from)) sel.value = from;
  const run = document.getElementById('ta-run');
  if (!run.value.trim()) run.value = grn;
  const m = (res.headers.get('Content-Disposition') || '').match(/filename="([^"]+)"/);
  await taSave(await res.blob(), m ? m[1] : 'allocation.xlsx');
}

// taFixFile: the rows that were left out, as a CSV the upload reads again.
function taFixFile() {
  if (!taBody || !taBody.fix_rows || !taBody.fix_rows.length) return;
  const cell = v => { let s = v === null || v === undefined ? '' : String(v); if (/^[=+\-@]/.test(s) && isNaN(Number(s))) s = `'${s}`; return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s; };
  const text = [taBody.fix_header || [], ...taBody.fix_rows].map(r => r.map(cell).join(',')).join('\r\n');
  const name = `${(taBody.run_id || 'allocation').replace(/[^A-Za-z0-9_-]+/g, '-')}-rows-to-fix.csv`;
  taSave(new Blob([String.fromCharCode(0xFEFF) + text], { type: 'text/csv' }), name);
}

async function taSend(dry) {
  const out = document.getElementById('ta-out');
  document.getElementById('ta-runbox').innerHTML = '';
  const runId = document.getElementById('ta-run').value.trim();
  const fd = new FormData();
  fd.append('file', taFile, taFile.name);
  fd.append('run_id', runId);
  fd.append('from_warehouse', document.getElementById('ta-from').value);
  fd.append('purpose', document.getElementById('ta-purpose').value);
  fd.append('dry_run', dry ? 'true' : 'false');
  const res = await apiUpload(API, fd);
  if (!res) return;
  const body = await res.clone().json().catch(() => ({}));
  taBody = null;
  if (!res.ok && !(body.errors && body.errors.length)) {
    out.innerHTML = `<p class="s-196e02">${esc(await getErrorMessage(res, 'The file could not be read.'))}</p>`;
    return;
  }
  taBody = body;
  let html = '';
  if (body.errors && body.errors.length) {
    html += `<ul class="s-cbbc5d">${body.errors.map(e => `<li>Row ${e.row_number}: ${esc(e.message)}</li>`).join('')}</ul>`;
  }
  if (body.fix_rows && body.fix_rows.length) {
    html += `<div class="s-881549"><button class="btn" id="ta-fix">Rows to fix (${body.fix_rows.length})</button></div>`;
  }
  if (body.stores && body.stores.length) {
    html += `<table class="data-table"><thead><tr><th>Store</th><th>Lines</th><th>Units</th></tr></thead><tbody>${body.stores.map(s => `<tr><td>${esc(s.store)}</td><td>${s.lines}</td><td>${s.units}</td></tr>`).join('')}</tbody></table>`;
  }
  if (body.shortfalls && body.shortfalls.length) {
    html += `<p class="s-62cb07">Source is short</p><table class="data-table"><thead><tr><th>SKU</th><th>Wanted</th><th>Available</th><th>Short</th></tr></thead><tbody>${body.shortfalls.map(s => `<tr><td>${esc(s.sku)}</td><td>${s.wanted}</td><td>${s.available}</td><td>${s.short}</td></tr>`).join('')}</tbody></table>`;
  }
  out.innerHTML = html;
  const fix = document.getElementById('ta-fix');
  if (fix) fix.addEventListener('click', taFixFile);
  if (!dry && res.ok) { taRun = runId; await taShowRun(); }
}

async function taShowRun() {
  const box = document.getElementById('ta-runbox');
  const res = await apiFetch(`${API}?run_id=${encodeURIComponent(taRun)}`);
  if (!res || !res.ok) return;
  const { transfers = [] } = await res.json();
  box.innerHTML = `
    <p class="s-b5b844">${esc(taRun)}: ${transfers.length} transfer(s)</p>
    <table class="data-table"><thead><tr><th>Transfer</th><th>Store</th><th>Lines</th><th>Status</th></tr></thead>
    <tbody>${transfers.map(t => `<tr><td>${esc(t.id)}</td><td>${esc(t.store)}</td><td>${t.lines}</td><td>${esc(t.status)}</td></tr>`).join('')}</tbody></table>
    <div class="s-918f5b">
      <button class="btn" id="ta-submit">Submit all</button>
      <button class="btn btn-primary" id="ta-approve">Approve all</button>
    </div><div id="ta-note" class="s-932645"></div>`;
  document.getElementById('ta-submit').addEventListener('click', () => taDecide('submit'));
  document.getElementById('ta-approve').addEventListener('click', () => taDecide('approve'));
}

async function taDecide(action) {
  const res = await apiFetch(`${API}/decide`, { method: 'POST', body: JSON.stringify({ run_id: taRun, action }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not update the run.'); return; }
  const { done = [], failed = {} } = await res.json();
  await taShowRun();
  const bad = Object.entries(failed);
  document.getElementById('ta-note').innerHTML = `${done.length} done` + (bad.length ? `<ul class="s-1e9d0d">${bad.map(([id, m]) => `<li>${esc(id)}: ${esc(m)}</li>`).join('')}</ul>` : '');
}
