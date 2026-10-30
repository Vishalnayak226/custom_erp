// Stage 142.3: Financial Accounting -> Sales Invoice -> Upload from Excel.
// One workbook (Customers and Sales sheets) becomes the customers it needs
// and one Sales Invoice per Invoice Ref + customer, each line found by its
// barcode with its own discount, posted where the uploader may post. Preview
// says what each row will do and changes nothing; Apply does it in the
// background; the same file again makes nothing twice. Server:
// internal/server/bulk_sales.go. Model: view-bulk-purchase.js (Stage 129).

const BS_API = '/api/v1/sales/bulk-upload';
const bsEsc = s => escapeHTMLText(String(s == null ? '' : s));
const bsMoney = n => `₹${Number(n || 0).toLocaleString('en-IN', { maximumFractionDigits: 2 })}`;

const BS_BADGES = {
  new: ['badge-success', 'New'],
  existing: ['badge-secondary', 'Already there'],
  ok: ['badge-success', 'OK'],
  error: ['badge-danger', 'Problem'],
  note: ['badge-secondary', 'Note'],
  done: ['badge-success', 'Done'],
  failed: ['badge-danger', 'Failed']
};

let bsFile = null;
let bsPoll = null;

function bsBadge(kind) {
  const [cls, text] = BS_BADGES[kind] || ['badge-secondary', kind];
  return `<span class="badge ${cls}">${bsEsc(text)}</span>`;
}

async function bsDownload(path, fallbackName, btn) {
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

export async function renderBulkSales(host, { onBack } = {}) {
  if (bsPoll) { clearTimeout(bsPoll); bsPoll = null; }
  bsFile = null;
  host.innerHTML = '';
  const screen = document.createElement('div');
  screen.className = 'table-panel';
  screen.style.padding = '20px';
  screen.innerHTML = `<div class="page-header" style="margin:0 0 12px;"><div class="page-title-section"><h1 class="page-title">Sales · Upload from Excel</h1></div>
      <div><button type="button" class="btn btn-ghost" data-bs="back">Back to invoices</button></div></div>
    <div style="display:grid; gap:18px; max-width:1100px; padding:4px 0 24px;">
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">1. Template</h4>
        <button type="button" class="btn btn-outline" data-bs="template">Download template</button>
        <span class="text-muted" style="font-size:13px; margin-left:10px;">Sales rows by barcode, a discount per line; Customers for anyone new.</span>
      </section>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">2. Upload and check</h4>
        <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap;">
          <label class="btn btn-outline" style="cursor:pointer; margin:0;">Choose file
            <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" data-bs="file" style="display:none;">
          </label>
          <span data-bs="file-name" class="text-muted" style="font-size:13px;">No file chosen</span>
          <button type="button" class="btn btn-primary" data-bs="preview" disabled>Preview</button>
        </div>
      </section>
      <div data-bs="result"></div>
      <section>
        <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">Past uploads</h4>
        <div data-bs="runs"><p class="text-muted">Loading…</p></div>
      </section>
    </div>`;
  host.appendChild(screen);
  screen.querySelector('[data-bs="back"]').addEventListener('click', () => onBack && onBack());
  screen.querySelector('[data-bs="template"]').addEventListener('click', e => bsDownload(`${BS_API}/template`, 'sales-upload-template.xlsx', e.currentTarget));
  screen.querySelector('[data-bs="file"]').addEventListener('change', e => {
    bsFile = e.target.files && e.target.files[0] ? e.target.files[0] : null;
    screen.querySelector('[data-bs="file-name"]').textContent = bsFile ? bsFile.name : 'No file chosen';
    screen.querySelector('[data-bs="preview"]').disabled = !bsFile;
    screen.querySelector('[data-bs="result"]').innerHTML = '';
    e.target.value = '';
  });
  screen.querySelector('[data-bs="preview"]').addEventListener('click', e => bsPreview(screen, e.currentTarget));
  bsRenderRuns(screen);
}

async function bsPreview(screen, btn) {
  if (!bsFile) return;
  if (bsFile.size > 8 * 1024 * 1024) { await showCustomAlert('The file is larger than 8 MB.', 'Upload from Excel'); return; }
  const out = screen.querySelector('[data-bs="result"]');
  out.innerHTML = '<p class="text-muted">Reading the file…</p>';
  btn.disabled = true;
  const fd = new FormData();
  fd.append('file', bsFile, bsFile.name);
  const res = await apiUpload(`${BS_API}/preview`, fd);
  btn.disabled = false;
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${bsEsc(await getErrorMessage(res, 'The file could not be read.'))}</p>`; return; }
  const { result } = await res.json();
  const t = result.totals || {};
  const invoices = result.invoices || [];
  const newInv = invoices.filter(i => i.action === 'new').length;
  const toPost = result.can_post ? invoices.filter(i => i.action === 'existing' && i.status === 'Draft').length : 0;
  const work = newInv + toPost + (t.new_customers || 0);
  out.innerHTML = `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">3. Preview - nothing has changed yet</h4>
      ${bsTotalsHtml(t, false)}
      ${result.can_post ? '' : '<p style="font-size:13px; margin:0 0 10px;">Your role may not post invoices: they are saved as Drafts, to be posted from the invoice list.</p>'}
      ${bsResultHtml(result, false)}
      <div style="display:flex; gap:12px; align-items:center; margin-top:12px; flex-wrap:wrap;">
        <button type="button" class="btn btn-primary" data-bs="apply"${work ? '' : ' disabled'}>${work ? bsApplyLabel(newInv, toPost, result.can_post) : 'Nothing to do'}</button>
        ${t.errors ? `<span style="font-size:13px; color:var(--danger-color);">${t.errors} problem${t.errors === 1 ? '' : 's'}: what has a problem is left out - fix the file and upload it again.</span>` : ''}
      </div>
    </section>`;
  const apply = out.querySelector('[data-bs="apply"]');
  if (apply) apply.addEventListener('click', () => bsApply(screen, newInv, toPost, t.errors || 0, result.can_post));
}

function bsApplyLabel(invoices, toPost, canPost) {
  const parts = [];
  if (invoices) parts.push(`${canPost ? 'make and post' : 'make'} ${invoices} invoice${invoices === 1 ? '' : 's'}`);
  if (toPost) parts.push(`post ${toPost} draft${toPost === 1 ? '' : 's'}`);
  const s = parts.join(' and ') || 'apply';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

async function bsApply(screen, invoices, toPost, problems, canPost) {
  const msg = `${bsApplyLabel(invoices, toPost, canPost)} from ${bsFile.name}?` +
    (canPost ? ' Posting books the receivable, the revenue and the output GST, and takes the items out of the store\'s stock.' : ' They are saved as Drafts.') +
    (problems ? ` What has a problem (${problems}) is left out.` : '');
  if (!await showCustomConfirm(msg, 'Upload from Excel')) return;
  const out = screen.querySelector('[data-bs="result"]');
  out.innerHTML = '<p class="text-muted">Starting…</p>';
  const fd = new FormData();
  fd.append('file', bsFile, bsFile.name);
  const res = await apiUpload(`${BS_API}/apply`, fd);
  if (!res) { out.innerHTML = ''; return; }
  if (!res.ok) { out.innerHTML = `<p style="color:var(--danger-color); font-size:14px;">${bsEsc(await getErrorMessage(res, 'The file could not be applied.'))}</p>`; return; }
  const { run_id: runID } = await res.json();
  bsFollow(screen, runID);
}

// bsFollow shows a run's progress until it finishes, then its result.
async function bsFollow(screen, runID) {
  const out = screen.querySelector('[data-bs="result"]');
  if (!out || !out.isConnected) return;
  const res = await apiFetch(`${BS_API}/runs/${encodeURIComponent(runID)}`);
  if (!res || !res.ok) { out.innerHTML = '<p class="text-muted">The upload could not be read - see Past uploads.</p>'; return; }
  const run = await res.json();
  if (run.status === 'Running') {
    out.innerHTML = `<p style="font-size:14px;">Working${run.progress_sheet ? `: <b>${bsEsc(run.progress_sheet)}</b>` : ''}…</p>`;
    bsPoll = setTimeout(() => bsFollow(screen, runID), 1200);
    return;
  }
  bsPoll = null;
  const t = run.totals || {};
  out.innerHTML = `
    <section>
      <h4 style="font-size:14px; font-weight:600; margin:0 0 8px;">${run.status === 'Completed' ? 'Done' : bsEsc(run.status || 'Finished')}${run.file_name ? ` - ${bsEsc(run.file_name)}` : ''}</h4>
      ${run.message ? `<p style="font-size:13px;">${bsEsc(run.message)}</p>` : ''}
      ${bsTotalsHtml(t, true)}
      ${(t.failed || t.errors) ? '<p style="font-size:13px; margin:0 0 10px;">Fix what is marked Failed or Problem, then upload the same file again: what is already made is found again, not made twice.</p>' : ''}
      ${run.result ? bsResultHtml(run.result, true) : ''}
    </section>`;
  bsRenderRuns(screen);
}

function bsTotalsHtml(t, applied) {
  const parts = [
    [t.new_invoices, `${t.new_invoices} new invoice${t.new_invoices === 1 ? '' : 's'}`],
    [t.existing_invoices, `${t.existing_invoices} invoice${t.existing_invoices === 1 ? '' : 's'} already made`],
    [t.lines, `${t.lines} row${t.lines === 1 ? '' : 's'}, ${t.qty || 0} pcs, ${bsMoney(t.amount)}${t.discount ? ` after ${bsMoney(t.discount)} discount` : ''}`],
    [t.new_customers, `${t.new_customers} new customer${t.new_customers === 1 ? '' : 's'}`],
    [applied && t.posted, `${t.posted} posted`],
    [applied && t.drafts, `${t.drafts} left as Draft`]
  ].filter(([n]) => n).map(([, s]) => `<span>${bsEsc(s)}</span>`);
  const badges = (applied
    ? [['done', t.done], ['failed', t.failed], ['error', t.errors]]
    : [['error', t.errors]]).filter(([, n]) => n).map(([k, n]) => `<span>${bsBadge(k)} ${bsEsc(n)}</span>`);
  return `<div style="display:flex; gap:14px; flex-wrap:wrap; margin:0 0 12px; font-size:13px;">${parts.concat(badges).join('')}</div>`;
}

function bsKind(r, applied) {
  return applied && r.result ? r.result : r.action;
}

function bsMsg(r) {
  if (!r.message) return '';
  const bad = r.action === 'error' || r.result === 'failed';
  return `<span style="font-size:13px;${bad ? ' color:var(--danger-color);' : ''}">${bsEsc(r.message)}</span>`;
}

function bsSection(title, counts, body, open) {
  return `
    <details class="table-panel" style="margin:0 0 10px; padding:0; min-height:0; display:block;"${open ? ' open' : ''}>
      <summary style="padding:10px 14px; cursor:pointer; display:flex; gap:12px; align-items:center; flex-wrap:wrap;">
        <b style="min-width:140px;">${bsEsc(title)}</b>${counts}
      </summary>
      <div style="overflow-x:auto;">${body}</div>
    </details>`;
}

function bsCount(list, applied) {
  const by = {};
  list.forEach(r => { const k = bsKind(r, applied); by[k] = (by[k] || 0) + 1; });
  return Object.keys(by).map(k => `<span style="font-size:13px;">${bsBadge(k)} ${by[k]}</span>`).join('');
}

function bsResultHtml(r, applied) {
  const customers = r.customers || [];
  const invoices = r.invoices || [];
  let html = '';
  if (customers.length) {
    html += bsSection('Customers', bsCount(customers, applied), `<table>
      <thead><tr><th>Row</th><th>Customer</th><th></th><th></th></tr></thead>
      <tbody>${customers.map(c => `<tr><td>${bsEsc(c.row || '')}</td><td>${bsEsc(c.key)}</td><td>${bsBadge(bsKind(c, applied))}</td><td>${bsMsg(c)}</td></tr>`).join('')}</tbody></table>`,
      customers.some(c => c.action === 'error' || c.result === 'failed'));
  }
  invoices.forEach(inv => {
    const kind = bsKind(inv, applied);
    const title = `${inv.ref} · ${inv.customer || ''}`;
    const head = `<span style="font-size:13px;">${bsBadge(kind)}</span>
      <span style="font-size:13px;">${inv.store ? `${bsEsc(inv.store)} · ` : ''}${inv.date ? `${bsEsc(inv.date)} · ` : ''}${inv.qty || 0} pcs · ${bsMoney(inv.amount)}${inv.discount ? ` (−${bsMoney(inv.discount)})` : ''}</span>
      ${inv.invoice_number ? `<span class="oms-mono" style="font-size:13px;">${bsEsc(inv.invoice_number)}${inv.status ? ` · ${bsEsc(inv.status)}` : ''}</span>` : ''}
      ${bsMsg(inv)}`;
    const body = `<table>
      <thead><tr><th>Row</th><th>Barcode</th><th>Item</th><th class="num">Qty</th><th class="num">Rate</th><th class="num">Discount</th><th class="num">Amount</th><th></th><th></th></tr></thead>
      <tbody>${(inv.lines || []).map(l => `<tr>
        <td>${bsEsc(l.row)}</td><td class="oms-mono">${bsEsc(l.barcode)}</td><td>${bsEsc(l.name || '')}${l.sku ? `<div class="text-muted" style="font-size:12px;">${bsEsc(l.sku)}</div>` : ''}</td>
        <td class="num">${bsEsc(l.qty)}</td><td class="num">${l.rate ? bsMoney(l.rate) : '<span class="text-muted">item\'s own</span>'}</td><td class="num">${l.discount ? bsMoney(l.discount) : '—'}</td><td class="num">${l.amount ? bsMoney(l.amount) : '—'}</td>
        <td>${bsBadge(l.action)}</td><td>${bsMsg(l)}</td></tr>`).join('')}</tbody></table>`;
    html += bsSection(title, head, body, kind === 'error' || kind === 'failed');
  });
  return html || '<p class="text-muted">Nothing in the file.</p>';
}

async function bsRenderRuns(screen) {
  const box = screen.querySelector('[data-bs="runs"]');
  if (!box) return;
  const res = await apiFetch(`${BS_API}/runs`);
  if (!res || !res.ok) { box.innerHTML = '<p class="text-muted">Past uploads could not be loaded.</p>'; return; }
  const runs = await res.json();
  if (!runs.length) { box.innerHTML = '<p class="text-muted">No file has been uploaded here yet.</p>'; return; }
  box.innerHTML = `
    <div class="table-panel" style="overflow-x:auto; min-height:0;">
      <table>
        <thead><tr><th>When</th><th>File</th><th>By</th><th>Status</th><th>Invoices</th><th>Posted</th><th></th></tr></thead>
        <tbody>${runs.map(r => {
          const at = r.started_at || r.created_at;
          const when = at ? new Date(at).toLocaleString('en-IN', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
          const t = r.totals || {};
          const bad = r.status === 'Failed' || r.status === 'Interrupted' || r.status === 'Completed with errors';
          return `<tr>
            <td>${bsEsc(when)}</td><td>${bsEsc(r.file_name || '')}</td><td>${bsEsc(r.created_by || '')}</td>
            <td><span class="badge ${bad ? 'badge-warning' : r.status === 'Running' ? 'badge-secondary' : 'badge-success'}">${bsEsc(r.status || '')}</span></td>
            <td>${bsEsc(t.new_invoices || 0)}</td><td>${bsEsc(t.posted || 0)}</td>
            <td style="white-space:nowrap;">
              <button type="button" class="btn btn-outline btn-sm" data-bs-run="${bsEsc(r.id)}">Details</button>
              <button type="button" class="btn btn-outline btn-sm" data-bs-file="${bsEsc(r.id)}">Download file</button>
            </td></tr>`;
        }).join('')}</tbody>
      </table>
    </div>`;
  box.querySelectorAll('[data-bs-run]').forEach(b => b.addEventListener('click', () => {
    bsFollow(screen, b.getAttribute('data-bs-run'));
    screen.querySelector('[data-bs="result"]').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }));
  box.querySelectorAll('[data-bs-file]').forEach(b => b.addEventListener('click', e =>
    bsDownload(`${BS_API}/runs/${encodeURIComponent(b.getAttribute('data-bs-file'))}/file`, 'sales-upload.xlsx', e.currentTarget)));
}
