// Stage 129: Procurement -> Purchase Orders -> Upload from Excel. The
// client's Bulk Purchase workbook (Vendors, Purchase and Bills sheets) becomes
// vendors, designs, purchase orders and - for rows with a vendor bill - goods
// receipts, in one upload. Preview says what each row will do and changes
// nothing; Apply does it in the background; uploading the same file again
// makes nothing twice. Server: internal/server/bulk_purchase.go.

const BP_API = '/api/v1/procurement/bulk-upload';
const bpEsc = s => escapeHTMLText(String(s == null ? '' : s));
const bpMoney = n => `₹${Number(n || 0).toLocaleString('en-IN', { maximumFractionDigits: 2 })}`;

const BP_BADGES = {
  new: ['badge-success', 'New'],
  existing: ['badge-secondary', 'Already there'],
  'new design': ['badge-success', 'New design'],
  'new combination': ['badge-success', 'New combination'],
  error: ['badge-danger', 'Problem'],
  note: ['badge-secondary', 'Note'],
  done: ['badge-success', 'Done'],
  failed: ['badge-danger', 'Failed'],
  waiting: ['badge-warning', 'Waiting']
};

let bpFile = null;   // the workbook chosen for upload
let bpPoll = null;   // the run being followed
// The photos chosen with it, by lower-case file name: after Apply each goes to
// the Combination ID of the row that names it in its Photo column (129.8).
let bpPhotos = new Map();
const BP_PHOTO_RE = /\.(jpe?g|png|webp|gif)$/i;

function bpBadge(kind) {
  const [cls, text] = BP_BADGES[kind] || ['badge-secondary', kind];
  return `<span class="badge ${cls}">${bpEsc(text)}</span>`;
}

async function bpDownload(path, fallbackName, btn) {
  if (btn) btn.disabled = true;
  const res = await apiFetch(path, { timeoutMs: 60000 });
  if (btn) btn.disabled = false;
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The file could not be downloaded.'); return; }
  const m = (res.headers.get('Content-Disposition') || '').match(/filename="([^"]+)"/);
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = m ? m[1] : fallbackName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function renderBulkPurchase(screen, { head }) {
  if (bpPoll) { clearTimeout(bpPoll); bpPoll = null; }
  bpFile = null;
  bpPhotos = new Map();
  screen.innerHTML = `<div class="txn-bar">${head}</div>
    <div style="display:grid; gap:18px; max-width:1100px; padding:4px 0 24px;">
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">1. Template</h4>
        <button type="button" class="btn btn-outline" data-bp="template">Download template</button>
      </section>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">2. Upload and check</h4>
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
          <label class="btn btn-outline" style="cursor:pointer; margin:0;">Choose file
            <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" data-bp="file" style="display:none;">
          </label>
          <span data-bp="file-name" class="text-muted" style="font-size:13px;">No file chosen</span>
        </div>
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap; margin-top:10px;">
          <label class="btn btn-outline" style="cursor:pointer; margin:0;">Choose photos
            <input type="file" multiple accept="image/jpeg,image/png,image/webp,image/gif" data-bp="photos" style="display:none;">
          </label>
          <label class="btn btn-outline" style="cursor:pointer; margin:0;">Choose a photo folder
            <input type="file" multiple webkitdirectory data-bp="photo-folder" style="display:none;">
          </label>
          <span data-bp="photo-count" class="text-muted" style="font-size:13px;">No photos (optional)</span>
          <button type="button" class="btn btn-primary" data-bp="preview" disabled>Preview</button>
        </div>
      </section>
      <div data-bp="result"></div>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">Past uploads</h4>
        <div data-bp="runs"><p class="text-muted">Loading…</p></div>
      </section>
    </div>`;
  screen.querySelector('[data-bp="template"]').addEventListener('click', e => bpDownload(`${BP_API}/template`, 'bulk-purchase-template.xlsx', e.currentTarget));
  screen.querySelector('[data-bp="file"]').addEventListener('change', e => {
    bpFile = e.target.files && e.target.files[0] ? e.target.files[0] : null;
    screen.querySelector('[data-bp="file-name"]').textContent = bpFile ? bpFile.name : 'No file chosen';
    screen.querySelector('[data-bp="preview"]').disabled = !bpFile;
    screen.querySelector('[data-bp="result"]').innerHTML = '';
    e.target.value = '';
  });
  screen.querySelectorAll('[data-bp="photos"], [data-bp="photo-folder"]').forEach(input => input.addEventListener('change', e => {
    [...(e.target.files || [])].filter(f => BP_PHOTO_RE.test(f.name)).forEach(f => bpPhotos.set(f.name.toLowerCase(), f));
    screen.querySelector('[data-bp="photo-count"]').textContent = bpPhotos.size
      ? `${bpPhotos.size} photo${bpPhotos.size === 1 ? '' : 's'} chosen` : 'No photos (optional)';
    screen.querySelector('[data-bp="result"]').innerHTML = '';
    e.target.value = '';
  }));
  screen.querySelector('[data-bp="preview"]').addEventListener('click', e => bpPreview(screen, e.currentTarget));
  bpRenderRuns(screen);
}

// bpPhotoCell: a row's photo names, each marked when it was not chosen.
function bpPhotoCell(names) {
  return (names || []).map(n => bpPhotos.has(n.toLowerCase())
    ? bpEsc(n)
    : `<span style="color:var(--danger-color);" title="Not among the chosen photos">${bpEsc(n)} ✗</span>`).join(', ');
}

// bpPhotoJobs: every photo to upload after Apply - to the row's Combination
// ID, and a design's first photo to the design.
function bpPhotoJobs(result) {
  const jobs = [];
  const designs = new Set();
  (result.pos || []).forEach(p => {
    if (!p.po_id || p.result === 'failed' || p.action === 'error') return;
    (p.lines || []).forEach(l => {
      const names = l.photos || [];
      if (!l.sku || !names.length) return;
      names.forEach(name => jobs.push({ po: p.po_id, doctype: 'Item', code: l.sku, name }));
      if (l.design_code && !designs.has(l.design_code)) {
        designs.add(l.design_code);
        jobs.push({ po: p.po_id, doctype: 'ProductFamily', code: l.design_code, name: names[0] });
      }
    });
  });
  return jobs;
}

// bpUploadPhotos sends the chosen photos through the PO's Images upload, one
// at a time (the same storage and the same checks as the Images page).
async function bpUploadPhotos(box, result) {
  const jobs = bpPhotoJobs(result);
  if (!jobs.length) return;
  let done = 0;
  const missing = [], failed = [];
  for (let i = 0; i < jobs.length; i++) {
    const j = jobs[i];
    const file = bpPhotos.get(j.name.toLowerCase());
    if (!file) { if (j.doctype === 'Item') missing.push(j.name); continue; }
    box.innerHTML = `<p style="font-size:14px;">Adding photos… ${i + 1} of ${jobs.length}</p>`;
    let res = null;
    for (let attempt = 0; attempt < 4; attempt++) {
      const fd = new FormData();
      fd.append('file', file, file.name);
      fd.append('target_doctype', j.doctype);
      fd.append('target_code', j.code);
      res = await apiUpload(`/api/v1/procurement/purchase-order/${encodeURIComponent(j.po)}/images`, fd);
      if (!res || res.status !== 429) break;
      await new Promise(r => setTimeout(r, 4000)); // the upload rate limit; try again
    }
    if (res && res.ok) done++;
    else failed.push(`${j.name} → ${j.code}: ${res ? await getErrorMessage(res, 'not added') : 'not added'}`);
  }
  box.innerHTML = `
    <h4 style="font-size:14px; font-weight:600; margin:0 0 6px;">Photos</h4>
    <p style="font-size:13px; margin:0;">${bpBadge('done')} ${done} added${failed.length ? ` &nbsp; ${bpBadge('failed')} ${failed.length}` : ''}${missing.length ? ` &nbsp; <span style="color:var(--danger-color);">${missing.length} named in the file but not chosen</span>` : ''}</p>
    ${failed.length ? `<ul style="font-size:13px; color:var(--danger-color); margin:6px 0 0;">${failed.map(f => `<li>${bpEsc(f)}</li>`).join('')}</ul>` : ''}
    ${missing.length ? `<p class="text-muted" style="font-size:13px; margin:6px 0 0;">Not chosen: ${bpEsc([...new Set(missing)].join(', '))} - choose them and upload the same file again, or add them on the PO's Images page.</p>` : ''}`;
}

async function bpPreview(screen, btn) {
  if (!bpFile) return;
  if (bpFile.size > 8 * 1024 * 1024) { await showCustomAlert('The file is larger than 8 MB.', 'Upload from Excel'); return; }
  const out = screen.querySelector('[data-bp="result"]');
  out.innerHTML = '<p class="text-muted">Reading the file…</p>';
  btn.disabled = true;
  const fd = new FormData();
  fd.append('file', bpFile, bpFile.name);
  const res = await apiUpload(`${BP_API}/preview`, fd);
  btn.disabled = false;
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${bpEsc(await getErrorMessage(res, 'The file could not be read.'))}</p>`; return; }
  const { result } = await res.json();
  const t = result.totals || {};
  const pos = result.pos || [];
  const bills = result.bills || [];
  const newPOs = pos.filter(p => p.action === 'new').length;
  const toReceive = bills.filter(b => b.action === 'new' || (b.action === 'existing' && b.step !== 'Posted')).length;
  // Photos count as work too: the same file again, with its photos, adds them.
  const photos = bpPhotos.size ? pos.filter(p => p.action !== 'error').reduce((n, p) => n + (p.lines || [])
    .reduce((m, l) => m + (l.photos || []).filter(x => bpPhotos.has(x.toLowerCase())).length, 0), 0) : 0;
  const work = newPOs + toReceive + photos + (t.new_vendors || 0) + (t.new_masters || 0) +
    pos.filter(p => p.action === 'existing' && (p.status === 'Draft' || p.status === 'Pending Approval')).length;
  out.innerHTML = `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">3. Preview - nothing has changed yet</h4>
      ${bpTotalsHtml(t, false)}
      ${bpPhotoSummary(result)}
      ${bpResultHtml(result, false)}
      <div style="display:flex; gap:12px; align-items:center; margin-top:12px; flex-wrap:wrap;">
        <button type="button" class="btn btn-primary" data-bp="apply"${work ? '' : ' disabled'}>${work ? bpApplyLabel(newPOs, toReceive, photos) : 'Nothing to do'}</button>
        ${t.errors ? `<span style="font-size:13px; color:var(--danger-color);">${t.errors} problem${t.errors === 1 ? '' : 's'}: what has a problem is left out - fix the file and upload it again.</span>` : ''}
      </div>
    </section>`;
  const apply = out.querySelector('[data-bp="apply"]');
  if (apply) apply.addEventListener('click', () => bpApply(screen, newPOs, toReceive, t.errors || 0, photos));
}

function bpApplyLabel(pos, bills, photos) {
  const parts = [];
  if (pos) parts.push(`make ${pos} PO${pos === 1 ? '' : 's'}`);
  if (bills) parts.push(`receive ${bills} bill${bills === 1 ? '' : 's'}`);
  if (photos) parts.push(`add ${photos} photo${photos === 1 ? '' : 's'}`);
  const s = parts.join(' and ') || 'apply';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

async function bpApply(screen, pos, bills, problems, photos) {
  const msg = `${bpApplyLabel(pos, bills, photos)} from ${bpFile.name}?` +
    (pos ? ' New POs are sent for approval and approved as you where your approval rules allow.' : '') +
    (bills ? ' Bills are received, verified and posted with the MRP in the file.' : '') +
    (problems ? ` What has a problem (${problems}) is left out.` : '') +
    (bpPhotos.size ? ' The chosen photos are added after that - keep this page open until they are.' : '');
  if (!await showCustomConfirm(msg, 'Upload from Excel')) return;
  const out = screen.querySelector('[data-bp="result"]');
  out.innerHTML = '<p class="text-muted">Starting…</p>';
  const fd = new FormData();
  fd.append('file', bpFile, bpFile.name);
  const res = await apiUpload(`${BP_API}/apply`, fd);
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${bpEsc(await getErrorMessage(res, 'The file could not be applied.'))}</p>`; return; }
  const { run_id: runID } = await res.json();
  bpFollow(screen, runID, true);
}

// bpFollow shows a run's progress until it finishes, then its result.
async function bpFollow(screen, runID, withPhotos) {
  const out = screen.querySelector('[data-bp="result"]');
  if (!out || !out.isConnected) return;
  const res = await apiFetch(`${BP_API}/runs/${encodeURIComponent(runID)}`);
  if (!res || !res.ok) { out.innerHTML = '<p class="text-muted">The upload could not be read - see Past uploads.</p>'; return; }
  const run = await res.json();
  if (run.status === 'Running') {
    out.innerHTML = `<p style="font-size:14px;">Working${run.progress_sheet ? `: <b>${bpEsc(run.progress_sheet)}</b>` : ''}…</p>`;
    bpPoll = setTimeout(() => bpFollow(screen, runID, withPhotos), 1200);
    return;
  }
  bpPoll = null;
  const t = run.totals || {};
  out.innerHTML = `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">${run.status === 'Completed' ? 'Done' : bpEsc(run.status || 'Finished')}${run.file_name ? ` - ${bpEsc(run.file_name)}` : ''}</h4>
      ${run.message ? `<p style="font-size:13px;">${bpEsc(run.message)}</p>` : ''}
      ${bpTotalsHtml(t, true)}
      ${(t.failed || t.errors || t.waiting) ? '<p style="font-size:13px; margin:0 0 10px;">Fix what is marked Failed or Problem (or approve what is Waiting), then upload the same file again: what is already made is found again, not made twice.</p>' : ''}
      ${run.result ? bpResultHtml(run.result, true) : ''}
    </section>`;
  bpWireStickers(out);
  bpRenderRuns(screen);
  if (withPhotos && bpPhotos.size && run.result) {
    const box = document.createElement('section');
    box.style.marginTop = '12px';
    out.appendChild(box);
    await bpUploadPhotos(box, run.result);
  }
}

function bpPhotoSummary(result) {
  const names = [];
  (result.pos || []).forEach(p => (p.lines || []).forEach(l => (l.photos || []).forEach(n => names.push(n))));
  if (!names.length && !bpPhotos.size) return '';
  const missing = names.filter(n => !bpPhotos.has(n.toLowerCase()));
  return `<p style="font-size:13px; margin:0 0 10px;">Photos: ${names.length} named in the file, ${bpPhotos.size} chosen${missing.length
    ? ` - <span style="color:var(--danger-color);">${missing.length} not chosen (marked ✗)</span>` : ''}. They are added after Apply.</p>`;
}

function bpTotalsHtml(t, applied) {
  const parts = [
    [t.new_pos, `${t.new_pos} new PO${t.new_pos === 1 ? '' : 's'}`],
    [t.existing_pos, `${t.existing_pos} PO${t.existing_pos === 1 ? '' : 's'} already made`],
    [t.lines, `${t.lines} row${t.lines === 1 ? '' : 's'}, ${t.qty || 0} pcs, ${bpMoney(t.amount)}`],
    [t.bills, `${t.bills} bill${t.bills === 1 ? '' : 's'}`],
    [t.new_vendors, `${t.new_vendors} new vendor${t.new_vendors === 1 ? '' : 's'}`]
  ].filter(([n]) => n).map(([, s]) => `<span>${bpEsc(s)}</span>`);
  const badges = (applied
    ? [['done', t.done], ['waiting', t.waiting], ['failed', t.failed], ['error', t.errors]]
    : [['error', t.errors]]).filter(([, n]) => n).map(([k, n]) => `<span>${bpBadge(k)} ${bpEsc(n)}</span>`);
  return `<div style="display:flex; gap:14px; flex-wrap:wrap; margin:0 0 12px; font-size:13px;">${parts.concat(badges).join('')}</div>`;
}

function bpKind(r, applied) {
  return applied && r.result ? r.result : r.action;
}

function bpMsg(r) {
  if (!r.message) return '';
  const bad = r.action === 'error' || r.result === 'failed';
  return `<span style="font-size:13px;${bad ? ' color:var(--danger-color);' : ''}">${bpEsc(r.message)}</span>`;
}

function bpSection(title, counts, body, open) {
  return `
    <details class="table-panel" style="margin:0 0 10px; padding:0; min-height:0; display:block;"${open ? ' open' : ''}>
      <summary style="padding:10px 14px; cursor:pointer; display:flex; gap:12px; align-items:center; flex-wrap:wrap;">
        <b style="min-width:140px;">${bpEsc(title)}</b>${counts}
      </summary>
      <div style="overflow-x:auto;">${body}</div>
    </details>`;
}

function bpCount(list, applied) {
  const by = {};
  list.forEach(r => { const k = bpKind(r, applied); by[k] = (by[k] || 0) + 1; });
  return Object.keys(by).map(k => `<span style="font-size:13px;">${bpBadge(k)} ${by[k]}</span>`).join('');
}

function bpResultHtml(r, applied) {
  const vendors = (r.vendors || []).filter(v => v.action !== 'note');
  const leftOut = (r.vendors || []).length - vendors.length;
  const masters = r.masters || [];
  const pos = r.pos || [];
  const bills = r.bills || [];
  let html = '';
  if (vendors.length || leftOut) {
    html += bpSection('Vendors', bpCount(vendors, applied) + (leftOut ? `<span class="text-muted" style="font-size:13px;">${leftOut} not used, left out</span>` : ''), vendors.length ? `
      <table><thead><tr><th style="width:60px;">Row</th><th>Vendor</th><th style="width:130px;"></th><th></th></tr></thead><tbody>
      ${vendors.map(v => `<tr><td>${bpEsc(v.row || '')}</td><td>${bpEsc(v.key)}</td><td>${bpBadge(bpKind(v, applied))}</td><td>${bpMsg(v)}</td></tr>`).join('')}
      </tbody></table>` : '', vendors.some(v => v.action === 'error' || v.result === 'failed'));
  }
  if (masters.length) {
    html += bpSection('New setup records', bpCount(masters, applied), `
      <table><thead><tr><th>What</th><th style="width:130px;"></th><th></th></tr></thead><tbody>
      ${masters.map(m => `<tr><td>${bpEsc(m.key)}</td><td>${bpBadge(bpKind(m, applied))}</td><td>${bpMsg(m)}</td></tr>`).join('')}
      </tbody></table>`, masters.some(m => m.result === 'failed'));
  }
  pos.forEach(p => {
    const kind = bpKind(p, applied);
    const title = `${p.ref} · ${p.vendor}`;
    const counts = `${bpBadge(kind)}
      <span style="font-size:13px;">${bpEsc(p.store)} · ${p.qty} pcs · ${bpMoney(p.amount)}</span>
      ${p.po_number ? `<span style="font-size:13px;"><b>${bpEsc(p.po_number)}</b>${p.status ? ` ${bpEsc(p.status)}` : ''}</span>` : ''}
      ${p.message ? bpMsg(p) : ''}`;
    html += bpSection(title, counts, `
      <table><thead><tr><th style="width:60px;">Row</th><th>Design</th><th>Item</th><th style="text-align:right;">Qty</th><th style="text-align:right;">Rate</th><th style="text-align:right;">MRP</th><th>Bill</th><th>Photo</th><th style="width:140px;"></th><th>Combination ID</th><th></th></tr></thead><tbody>
      ${(p.lines || []).map(l => `<tr><td>${bpEsc(l.row)}</td><td>${bpEsc(l.design || '')}${l.design_code ? ` <span class="text-muted">→ ${bpEsc(l.design_code)}</span>` : ''}</td><td style="font-size:13px;">${bpEsc(l.variant || '')}</td>
        <td style="text-align:right;">${bpEsc(l.qty)}</td><td style="text-align:right;">${bpEsc(l.rate)}</td><td style="text-align:right;">${bpEsc(l.mrp)}</td>
        <td>${bpEsc(l.bill || '')}</td><td style="font-size:12px;">${bpPhotoCell(l.photos)}</td><td>${bpBadge(l.action)}</td><td>${bpEsc(l.sku || '')}</td><td>${bpMsg(l)}</td></tr>`).join('')}
      </tbody></table>`, p.action === 'error' || p.result === 'failed');
  });
  if (bills.length) {
    html += bpSection('Bills (goods receipts)', bpCount(bills, applied), `
      <table><thead><tr><th style="width:60px;">Row</th><th>Bill no</th><th>Vendor</th><th>Date</th><th style="text-align:right;">Total</th><th style="text-align:right;">Pcs</th><th style="width:130px;"></th><th>Goods receipt</th><th></th></tr></thead><tbody>
      ${bills.map(b => `<tr><td>${bpEsc(b.row)}</td><td>${bpEsc(b.invoice)}</td><td>${bpEsc(b.vendor)}</td><td>${bpEsc(b.date)}</td>
        <td style="text-align:right;">${bpMoney(b.total)}</td><td style="text-align:right;">${bpEsc(b.qty)}</td><td>${bpBadge(bpKind(b, applied))}</td>
        <td style="white-space:nowrap;">${bpEsc(b.grn_code || '')}${b.step && b.step !== 'Posted' ? ` <span class="text-muted" style="font-size:12px;">${bpEsc(b.step)}</span>` : ''}
          ${applied && b.step === 'Posted' && (b.skus || []).length ? ` <button type="button" class="btn btn-outline btn-sm" data-bp-stickers="${bpEsc(b.grn_id)}" data-skus="${bpEsc((b.skus || []).join(','))}">Print stickers</button>` : ''}</td>
        <td>${bpMsg(b)}</td></tr>`).join('')}
      </tbody></table>`, applied || bills.some(b => b.action === 'error'));
  }
  return html || '<p class="text-muted">The file has no rows.</p>';
}

// Print stickers: the same hand-over the goods receipt's own button makes.
function bpWireStickers(root) {
  root.querySelectorAll('[data-bp-stickers]').forEach(b => b.addEventListener('click', () => {
    const skus = (b.dataset.skus || '').split(',').filter(Boolean);
    try { sessionStorage.setItem('grn-child-stickers', JSON.stringify({ id: b.dataset.bpStickers, skus })); } catch (_) { /* storage can be disabled */ }
    renderView('stickers');
  }));
}

async function bpRenderRuns(screen) {
  const box = screen.querySelector('[data-bp="runs"]');
  if (!box) return;
  const res = await apiFetch(`${BP_API}/runs`);
  if (!res || !res.ok) { box.innerHTML = '<p class="text-muted">Past uploads could not be loaded.</p>'; return; }
  const runs = await res.json();
  if (!runs.length) { box.innerHTML = '<p class="text-muted">No file has been uploaded here yet.</p>'; return; }
  box.innerHTML = `
    <div class="table-panel" style="overflow-x:auto; min-height:0;">
      <table>
        <thead><tr><th>When</th><th>File</th><th>By</th><th>Status</th><th>New POs</th><th>Bills</th><th></th></tr></thead>
        <tbody>${runs.map(r => {
          // started_at is UTC; created_at is the database's local clock without a zone.
          const at = r.started_at || r.created_at;
          const when = at ? new Date(at).toLocaleString('en-IN', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
          const t = r.totals || {};
          const bad = r.status === 'Failed' || r.status === 'Interrupted' || r.status === 'Completed with errors';
          return `<tr>
            <td>${bpEsc(when)}</td><td>${bpEsc(r.file_name || '')}</td><td>${bpEsc(r.created_by || '')}</td>
            <td><span class="badge ${bad ? 'badge-warning' : r.status === 'Running' ? 'badge-secondary' : 'badge-success'}">${bpEsc(r.status || '')}</span></td>
            <td>${bpEsc(t.new_pos || 0)}</td><td>${bpEsc(t.bills || 0)}</td>
            <td style="white-space:nowrap;">
              <button type="button" class="btn btn-outline btn-sm" data-bp-run="${bpEsc(r.id)}">Details</button>
              <button type="button" class="btn btn-outline btn-sm" data-bp-file="${bpEsc(r.id)}">Download file</button>
            </td></tr>`;
        }).join('')}</tbody>
      </table>
    </div>`;
  box.querySelectorAll('[data-bp-run]').forEach(b => b.addEventListener('click', () => {
    bpFollow(screen, b.getAttribute('data-bp-run'));
    screen.querySelector('[data-bp="result"]').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }));
  box.querySelectorAll('[data-bp-file]').forEach(b => b.addEventListener('click', e =>
    bpDownload(`${BP_API}/runs/${encodeURIComponent(b.getAttribute('data-bp-file'))}/file`, 'bulk-purchase.xlsx', e.currentTarget)));
}
