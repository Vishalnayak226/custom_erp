// Stage 146.3.12: Stock Movement > Allocation. One file (store, sku, qty) makes
// one Draft transfer per store; Preview checks the rows first and shows what
// the source cannot cover. Submit and Approve then act on the whole run
// through the ordinary transfer approval.

const esc = escapeHTMLText;
const API = '/api/v1/transfer/allocation';
let taFile = null;
let taRun = '';

export async function renderTransferAllocationView(container) {
  const res = await apiFetch('/api/v1/doc/Location');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Locations could not be loaded.'); return; }
  const locs = (await res.json()).map(l => (l.data || l).code || l.id).filter(Boolean).sort();
  taFile = null; taRun = '';
  const box = document.createElement('div');
  box.className = 'table-panel';
  box.style.padding = '24px';
  box.innerHTML = `
    <h3 style="margin:0 0 12px;font-size:16px;font-weight:600;">Allocation</h3>
    <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:0 16px;max-width:760px;">
      <div class="form-group"><label class="form-label" for="ta-run">Run ID</label><input id="ta-run" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="ta-from">From</label>
        <select id="ta-from" class="form-select">${locs.map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join('')}</select></div>
      <div class="form-group"><label class="form-label" for="ta-purpose">Purpose</label>
        <select id="ta-purpose" class="form-select"><option value="">Replenishment</option><option>New store</option><option>Launch allocation</option></select></div>
    </div>
    <div class="form-group"><label class="form-label" for="ta-file">File (CSV: store, sku, qty)</label><input id="ta-file" type="file" accept=".csv,text/csv"></div>
    <div style="display:flex;gap:8px;margin:8px 0 16px;">
      <button class="btn" id="ta-preview" disabled>Preview</button>
      <button class="btn btn-primary" id="ta-make" disabled>Make Drafts</button>
    </div>
    <div id="ta-out"></div>`;
  container.appendChild(box);
  const q = (s) => document.getElementById(s);
  q('ta-file').addEventListener('change', (e) => {
    taFile = e.target.files[0] || null;
    q('ta-preview').disabled = q('ta-make').disabled = !taFile;
  });
  q('ta-preview').addEventListener('click', () => taSend(true));
  q('ta-make').addEventListener('click', () => taSend(false));
}

async function taSend(dry) {
  const out = document.getElementById('ta-out');
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
  if (!res.ok && !(body.errors && body.errors.length)) {
    out.innerHTML = `<p style="color:var(--danger-color);font-size:14px;">${esc(await getErrorMessage(res, 'The file could not be read.'))}</p>`;
    return;
  }
  let html = '';
  if (body.errors && body.errors.length) {
    html += `<ul style="font-size:13px;color:var(--danger-color);margin:0 0 12px;">${body.errors.map(e => `<li>Row ${e.row_number}: ${esc(e.message)}</li>`).join('')}</ul>`;
  }
  if (body.stores && body.stores.length) {
    html += `<table class="data-table"><thead><tr><th>Store</th><th>Lines</th><th>Units</th></tr></thead><tbody>${body.stores.map(s => `<tr><td>${esc(s.store)}</td><td>${s.lines}</td><td>${s.units}</td></tr>`).join('')}</tbody></table>`;
  }
  if (body.shortfalls && body.shortfalls.length) {
    html += `<p style="font-size:13px;margin:12px 0 4px;">Source is short</p><table class="data-table"><thead><tr><th>SKU</th><th>Wanted</th><th>Available</th><th>Short</th></tr></thead><tbody>${body.shortfalls.map(s => `<tr><td>${esc(s.sku)}</td><td>${s.wanted}</td><td>${s.available}</td><td>${s.short}</td></tr>`).join('')}</tbody></table>`;
  }
  out.innerHTML = html;
  if (!dry && res.ok) { taRun = runId; await taShowRun(); }
}

async function taShowRun() {
  const out = document.getElementById('ta-out');
  const res = await apiFetch(`${API}?run_id=${encodeURIComponent(taRun)}`);
  if (!res || !res.ok) return;
  const { transfers = [] } = await res.json();
  out.innerHTML = `
    <p style="font-size:14px;margin:0 0 8px;">${esc(taRun)}: ${transfers.length} transfer(s)</p>
    <table class="data-table"><thead><tr><th>Transfer</th><th>Store</th><th>Lines</th><th>Status</th></tr></thead>
    <tbody>${transfers.map(t => `<tr><td>${esc(t.id)}</td><td>${esc(t.store)}</td><td>${t.lines}</td><td>${esc(t.status)}</td></tr>`).join('')}</tbody></table>
    <div style="display:flex;gap:8px;margin-top:12px;">
      <button class="btn" id="ta-submit">Submit all</button>
      <button class="btn btn-primary" id="ta-approve">Approve all</button>
    </div><div id="ta-note" style="font-size:13px;margin-top:8px;"></div>`;
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
  document.getElementById('ta-note').innerHTML = `${done.length} done` + (bad.length ? `<ul style="color:var(--danger-color);margin:4px 0 0;">${bad.map(([id, m]) => `<li>${esc(id)}: ${esc(m)}</li>`).join('')}</ul>` : '');
}
