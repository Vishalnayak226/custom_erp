// Finance Verification -> Payment -> Accounting. Manager Approval reuses
// the existing Approval/Workflow Engine (Stage 13.8) - once submitted, a
// claim shows up in the existing "Approvals" screen automatically (it
// queries every approval-gated doctype), so this screen only needs to
// handle claim creation/submission plus the two stages after approval.
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
//
// Stage 69: the claims list is the page; + New Expense Claim opens the form as
// a page of its own (txn-screen.js, the standard ERP shape).
//
// Stage 55.15: the form defaults to the signed-in employee, their location and
// today; it shows what will actually be reimbursed (amount + GST - advance)
// and refuses impossible amounts before saving. The list says what is still
// owed at each stage, every claim shows its path (draft -> submitted ->
// decision -> verified -> paid) with the approver's comments, and a rejected
// claim can be corrected and resubmitted with what was entered kept.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const EXPENSE_STEPS = ['Draft', 'Pending Approval', 'Approved', 'Verified', 'Paid'];
let expenseEditingId = null; // a Rejected claim being corrected

// The session's store from the token's own claims (txn-screen.js reads it the
// same way for the context chip).
function expenseSessionStore() {
  try {
    const claims = (localStorage.getItem('erp_token') || '').split('.')[0];
    return new URLSearchParams(atob(claims.replace(/-/g, '+').replace(/_/g, '/'))).get('loc') || '';
  } catch (_) {
    return '';
  }
}

function expenseReimbursable(c) {
  return Number(c.amount || 0) + Number(c.gst_amount || 0) - Number(c.advance_adjusted || 0);
}

async function renderExpensesView(container) {
  // 30.5.8: the parallel Employee fetch that used to sit alongside this one
  // only existed to build the picker's <option>s; the picker is a typeahead
  // now and fetches on demand.
  const [claimsRes, meRes] = await Promise.all([apiFetch('/api/v1/doc/ExpenseClaim'), apiFetch('/api/v1/hr/my-employee')]);
  if (!claimsRes) return;
  const claims = claimsRes.ok ? await claimsRes.json() : [];
  const me = meRes && meRes.ok ? ((await meRes.json()).employee || null) : null;

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Claim Number', 'EXP', '160px')}
${employeePickerField('exp-employee', '180px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-location">Location</label>
        <input type="text" id="exp-location" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-date">Expense Date</label>
        <input type="date" id="exp-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-category">Category</label>
        <select id="exp-category" class="form-input" style="width: 130px;">
          <option value="Conveyance">Conveyance</option>
          <option value="Travel">Travel</option>
          <option value="Food">Food</option>
          <option value="Hotel">Hotel</option>
          <option value="Fuel">Fuel</option>
          <option value="Repair">Repair</option>
          <option value="Medical">Medical</option>
          <option value="Marketing">Marketing</option>
          <option value="StoreExpense">StoreExpense</option>
          <option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-amount">Amount (before GST)</label>
        <input type="number" id="exp-amount" class="form-input" style="width: 110px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-gst">GST on the bill</label>
        <input type="number" id="exp-gst" class="form-input" style="width: 100px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-advance">Advance already received</label>
        <input type="number" id="exp-advance" class="form-input" style="width: 110px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-receipt">Bill / receipt no.</label>
        <input type="text" id="exp-receipt" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-files">Receipt photo / PDF</label>
        <input type="file" id="exp-files" class="form-input" style="width: 230px;" accept="${RECEIPT_TYPES}" multiple>
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 160px;">
        <label class="form-label" for="exp-purpose">Purpose</label>
        <input type="text" id="exp-purpose" class="form-input">
      </div>
      <button class="btn btn-primary" id="exp-create-btn">Create Draft</button>
    </div>
    <div id="exp-total" class="page-subtitle" style="margin-top: 12px;"></div>
    <div id="exp-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

  // What is still owed, by where it waits.
  const by = s => claims.filter(c => c.status === s);
  const sum = list => list.reduce((t, c) => t + expenseReimbursable(c), 0);
  const money = v => Number(v || 0).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const stats = document.createElement('div');
  stats.className = 'dashboard-stats-row';
  stats.innerHTML = [
    ['Awaiting approval', by('Pending Approval')], ['Approved - to verify', by('Approved')],
    ['Verified - to pay', by('Verified')], ['Rejected - to correct', by('Rejected')]
  ].map(([label, list]) => `<div class="stat-card"><span class="stat-label">${label}</span><span class="stat-val">${list.length} &middot; ${money(sum(list))}</span></div>`).join('');

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr><th>Claim #</th><th>Employee</th><th>Date</th><th>Category</th><th>Amount</th><th>GST</th><th>Reimbursable</th><th>Status</th><th></th></tr>
      </thead>
      <tbody>
  `;
  html += claims.length === 0
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No expense claims yet. Click <b>+ New Expense Claim</b> to raise one.</td></tr>`
    : claims.map(c => `
        <tr>
          <td style="font-family: monospace;">${escapeHTMLText(c.code || c.id)}<br><small data-exp-clip="${escapeHTMLText(c.id)}"></small></td>
          <td>${escapeHTMLText(c.employee_id || '')}</td>
          <td>${escapeHTMLText(c.expense_date || '')}</td>
          <td>${escapeHTMLText(c.category || '')}${c.receipt_number ? `<br><small>Bill ${escapeHTMLText(c.receipt_number)}</small>` : ''}</td>
          <td>${money(c.amount)}</td>
          <td>${money(c.gst_amount)}</td>
          <td>${money(expenseReimbursable(c))}${Number(c.advance_adjusted) > 0 ? `<br><small>after ${money(c.advance_adjusted)} advance</small>` : ''}</td>
          <td><span class="badge ${expenseStatusBadge(c.status)}">${escapeHTMLText(c.status)}</span></td>
          <td>${renderExpenseActions(c)} <button class="action-btn" data-exp-history="${escapeHTMLText(c.id)}">History</button></td>
        </tr>
        <tr class="hidden" data-exp-history-row="${escapeHTMLText(c.id)}"><td colspan="9"></td></tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  const listWrap = document.createElement('div');
  listWrap.appendChild(stats);
  listWrap.appendChild(listPanel);
  container.appendChild(txnPagedSection({ id: 'exp-screen', title: 'Expenses', page: 'New expense claim', count: claims.length,
    newLabel: 'New Expense Claim', form: formPanel, list: listWrap }));
  txnRestorePane('exp-screen');

  document.getElementById('exp-create-btn').addEventListener('click', createExpenseClaim);
  attachEmployeePicker('exp-employee');
  attachLinkTypeahead(document.getElementById('exp-location'), 'Location');

  // Defaults: the signed-in employee, the store this session works at (the
  // claims list is scoped to it, so a claim filed elsewhere would vanish from
  // its own submitter's list) or else the employee's home location, today.
  if (me) document.getElementById('exp-employee').value = me.code || me.id;
  document.getElementById('exp-location').value = expenseSessionStore() || (me && me.location) || '';
  document.getElementById('exp-date').value = localISODate(new Date());
  const totalEl = document.getElementById('exp-total');
  const showTotal = () => {
    const a = Number(document.getElementById('exp-amount').value || 0);
    const g = Number(document.getElementById('exp-gst').value || 0);
    const adv = Number(document.getElementById('exp-advance').value || 0);
    const problems = [];
    if (g > a && a > 0) problems.push('GST is more than the amount');
    if (adv > a + g) problems.push('the advance is more than the bill');
    totalEl.textContent = a > 0 ? `To reimburse: ${money(a)} + GST ${money(g)} - advance ${money(adv)} = ${money(a + g - adv)}${problems.length ? ` - check: ${problems.join('; ')}` : ''}` : '';
  };
  ['exp-amount', 'exp-gst', 'exp-advance'].forEach(id => document.getElementById(id).addEventListener('input', showTotal));

  listPanel.querySelectorAll('[data-exp-history]').forEach(b => b.addEventListener('click', () => showExpenseHistory(b.dataset.expHistory, claims)));
  loadExpenseReceiptCounts(claims.map(c => c.id));
  listPanel.querySelectorAll('[data-exp-correct]').forEach(b => b.addEventListener('click', () => {
    const c = claims.find(x => x.id === b.dataset.expCorrect);
    if (!c) return;
    expenseEditingId = c.id;
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v ?? ''; };
    set('exp-employee', c.employee_id); set('exp-location', c.location); set('exp-date', c.expense_date);
    set('exp-category', c.category); set('exp-amount', c.amount); set('exp-gst', c.gst_amount);
    set('exp-advance', c.advance_adjusted); set('exp-receipt', c.receipt_number); set('exp-purpose', c.purpose);
    document.getElementById('exp-create-btn').textContent = `Save correction to ${c.code || c.id}`;
    showTotal();
    window.txnShowPane('exp-screen:new');
  }));
}

// The claim's path through its states, with the approval log's comments.
async function showExpenseHistory(claimId, claims) {
  const row = document.querySelector(`tr[data-exp-history-row="${CSS.escape(claimId)}"]`);
  if (!row) return;
  if (!row.classList.contains('hidden')) { row.classList.add('hidden'); return; }
  const claim = claims.find(c => c.id === claimId) || {};
  const res = await apiFetch(`/api/v1/approval/log?doctype=ExpenseClaim&document_id=${encodeURIComponent(claimId)}`);
  const log = res && res.ok ? await res.json() : [];
  const entries = Array.isArray(log) ? log : (log.entries || []);
  const reached = claim.status === 'Rejected' ? ['Draft', 'Pending Approval'] : EXPENSE_STEPS.slice(0, EXPENSE_STEPS.indexOf(claim.status) + 1);
  const path = EXPENSE_STEPS.map(s => `<span class="badge ${reached.includes(s) ? (s === claim.status ? 'badge-success' : 'badge-secondary') : ''}" style="${reached.includes(s) ? '' : 'opacity:.45'}">${s}</span>`).join(' &rarr; ')
    + (claim.status === 'Rejected' ? ' &rarr; <span class="badge badge-danger">Rejected</span>' : '');
  row.querySelector('td').innerHTML = `<div style="padding:8px 0">${path}</div>
    ${entries.length === 0 ? '<div style="color:var(--text-muted)">No approval decisions yet.</div>' : `<table><thead><tr><th>When</th><th>Action</th><th>By</th><th>Comment</th></tr></thead><tbody>
      ${entries.map(e => `<tr><td>${escapeHTMLText((e.created_at || '').replace('T', ' ').slice(0, 16))}</td><td>${escapeHTMLText(e.action || '')}</td><td>${escapeHTMLText(e.actor_user_id || '')} (${escapeHTMLText(e.actor_role || '')})</td><td>${escapeHTMLText(e.comment || '')}</td></tr>`).join('')}
    </tbody></table>`}
    <div data-exp-receipts></div>`;
  row.classList.remove('hidden');
  await renderExpenseReceipts(row.querySelector('[data-exp-receipts]'), claimId);
}

// Stage 55.15b: receipt files, kept in the server's attachment store and
// readable by whoever can read the claim. Files are fetched with the session's
// token and shown from a Blob URL - a plain link would carry no token.
const RECEIPT_TYPES = 'image/jpeg,image/png,image/webp,application/pdf';
const RECEIPT_MAX_BYTES = 5 * 1024 * 1024;
const RECEIPT_MAX_FILES = 5;
const RECEIPT_CLIP_ICON = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" style="vertical-align:-2px"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>';

function receiptSelectionProblem(files, alreadyAttached = 0) {
  if (files.length + alreadyAttached > RECEIPT_MAX_FILES) return `A claim can hold at most ${RECEIPT_MAX_FILES} receipt files.`;
  const tooBig = files.find(f => f.size > RECEIPT_MAX_BYTES);
  if (tooBig) return `${tooBig.name} is larger than 5 MB.`;
  const wrongType = files.find(f => f.type && !RECEIPT_TYPES.split(',').includes(f.type));
  if (wrongType) return `${wrongType.name} is not a JPEG, PNG or WebP picture or a PDF.`;
  return '';
}

// Sends each file in turn; returns one line per file that did not attach.
async function uploadExpenseReceipts(claimId, files) {
  const failed = [];
  for (const file of files) {
    const fd = new FormData();
    fd.append('file', file, file.name);
    const res = await apiUpload(`/api/v1/attachments/ExpenseClaim/${encodeURIComponent(claimId)}`, fd);
    if (!res) failed.push(`${file.name}: not sent`);
    else if (!res.ok) failed.push(`${file.name}: ${await getErrorMessage(res, 'could not be attached')}`);
  }
  return failed;
}

function setExpenseClip(claimId, count) {
  const el = document.querySelector(`[data-exp-clip="${CSS.escape(claimId)}"]`);
  if (!el) return;
  if (count > 0) {
    el.innerHTML = `${RECEIPT_CLIP_ICON} ${count}`;
    el.title = `${count} receipt file${count === 1 ? '' : 's'}`;
    el.style.color = '';
  } else {
    el.textContent = 'no receipt';
    el.title = '';
    el.style.color = 'var(--text-muted)';
  }
}

async function loadExpenseReceiptCounts(ids) {
  for (let i = 0; i < ids.length; i += 200) {
    const chunk = ids.slice(i, i + 200);
    const res = await apiFetch(`/api/v1/attachments/ExpenseClaim?ids=${encodeURIComponent(chunk.join(','))}`);
    if (!res || !res.ok) return;
    const counts = (await res.json()).counts || {};
    chunk.forEach(id => setExpenseClip(id, counts[id] || 0));
  }
}

function receiptSize(bytes) {
  const n = Number(bytes || 0);
  return n >= 1024 * 1024 ? `${(n / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;
}

let receiptPreviewURL = null;

async function renderExpenseReceipts(holder, claimId) {
  if (!holder) return;
  const res = await apiFetch(`/api/v1/attachments/ExpenseClaim/${encodeURIComponent(claimId)}`);
  if (!res) return;
  if (!res.ok) {
    holder.innerHTML = `<div style="margin-top:12px; color:var(--text-muted)">${escapeHTMLText(await getErrorMessage(res, 'Receipts could not be loaded.'))}</div>`;
    return;
  }
  const body = await res.json();
  const files = body.attachments || [];
  setExpenseClip(claimId, files.length);
  holder.innerHTML = `<div style="margin-top:12px"><strong>Receipts</strong></div>
    ${files.length === 0 ? '<div style="color:var(--text-muted)">No receipt attached.</div>' : `<table><thead><tr><th>File</th><th>Size</th><th>Added by</th><th>Added</th><th></th></tr></thead><tbody>
      ${files.map(f => `<tr><td>${escapeHTMLText(f.file_name)}</td><td>${receiptSize(f.size_bytes)}</td><td>${escapeHTMLText(f.uploaded_by || '')}</td><td>${escapeHTMLText((f.created_at || '').replace('T', ' ').slice(0, 16))}</td>
        <td><button class="action-btn" data-exp-receipt-view="${escapeHTMLText(f.id)}">View</button>${f.can_remove ? ` <button class="action-btn" data-exp-receipt-remove="${escapeHTMLText(f.id)}">Remove</button>` : ''}</td></tr>`).join('')}
    </tbody></table>`}
    ${body.can_add ? `<div style="display:flex; gap:8px; align-items:center; flex-wrap:wrap; margin-top:8px">
      <input type="file" class="form-input" style="width:260px" accept="${RECEIPT_TYPES}" multiple data-exp-receipt-input aria-label="Receipt files">
      <button class="btn btn-secondary" data-exp-receipt-add>Attach</button></div>` : ''}
    <div class="login-error hidden" style="margin-top:8px" data-exp-receipt-error></div>
    <div style="margin-top:8px" data-exp-receipt-preview></div>`;

  const errorEl = holder.querySelector('[data-exp-receipt-error]');
  const showError = msg => { errorEl.textContent = msg; errorEl.classList.toggle('hidden', !msg); };
  holder.querySelectorAll('[data-exp-receipt-view]').forEach(b => b.addEventListener('click', () => {
    const file = files.find(f => f.id === b.dataset.expReceiptView);
    if (file) viewExpenseReceipt(holder.querySelector('[data-exp-receipt-preview]'), file);
  }));
  holder.querySelectorAll('[data-exp-receipt-remove]').forEach(b => b.addEventListener('click', async () => {
    if (!await showCustomConfirm('Remove this receipt from the claim?', 'Remove Receipt')) return;
    const del = await apiFetch(`/api/v1/attachments/file/${encodeURIComponent(b.dataset.expReceiptRemove)}`, { method: 'DELETE' });
    if (!del) return;
    if (!del.ok) { showError(await getErrorMessage(del, 'The receipt could not be removed.')); return; }
    await renderExpenseReceipts(holder, claimId);
  }));
  const addBtn = holder.querySelector('[data-exp-receipt-add]');
  if (addBtn) addBtn.addEventListener('click', () => guardAgainstDoubleSubmit(addBtn, 'Attaching...', async () => {
    const picked = Array.from(holder.querySelector('[data-exp-receipt-input]').files || []);
    if (!picked.length) { showError('Choose a receipt file first.'); return; }
    const problem = receiptSelectionProblem(picked, files.length);
    if (problem) { showError(problem); return; }
    const failed = await uploadExpenseReceipts(claimId, picked);
    await renderExpenseReceipts(holder, claimId);
    if (failed.length) {
      const el = holder.querySelector('[data-exp-receipt-error]');
      el.textContent = failed.join(' ');
      el.classList.remove('hidden');
    }
  }));
}

async function viewExpenseReceipt(previewEl, file) {
  const res = await apiFetch(`/api/v1/attachments/file/${encodeURIComponent(file.id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'The receipt could not be opened.');
    return;
  }
  const blob = await res.blob();
  if (receiptPreviewURL) URL.revokeObjectURL(receiptPreviewURL);
  receiptPreviewURL = URL.createObjectURL(blob);
  if (blob.type.startsWith('image/')) {
    previewEl.innerHTML = `<div><button class="action-btn" data-exp-receipt-close>Close preview</button></div>
      <img alt="${escapeHTMLText(file.file_name)}" style="max-width:100%; max-height:520px; margin-top:6px; border:1px solid var(--border-color)">`;
    previewEl.querySelector('img').src = receiptPreviewURL;
    previewEl.querySelector('[data-exp-receipt-close]').addEventListener('click', () => { previewEl.innerHTML = ''; });
    return;
  }
  // A PDF is saved rather than shown inline: the page's CSP allows no
  // embedded viewer, and the server sends PDFs as downloads anyway.
  const a = document.createElement('a');
  a.href = receiptPreviewURL;
  a.download = file.file_name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function expenseStatusBadge(status) {
  if (status === 'Paid') return 'badge-success';
  if (status === 'Rejected') return 'badge-danger';
  if (status === 'Pending Approval') return 'badge-warning';
  return 'badge-secondary';
}

function renderExpenseActions(claim) {
  if (claim.status === 'Draft') {
    return `<button class="action-btn" ${actionAttrs('submitExpenseForApproval', [claim.id])}>Submit for Approval</button>`;
  }
  if (claim.status === 'Approved') {
    return `<button class="action-btn" ${actionAttrs('verifyExpenseClaim', [claim.id])}>Finance Verify</button>`;
  }
  if (claim.status === 'Verified') {
    return `<button class="action-btn" ${actionAttrs('payExpenseClaim', [claim.id])}>Mark Paid</button>`;
  }
  if (claim.status === 'Rejected') {
    return `<button class="action-btn" data-exp-correct="${escapeHTMLText(claim.id)}">Correct and resubmit</button>`;
  }
  return '';
}

async function createExpenseClaim() {
  // BLD-036: guard against a double-click creating two ExpenseClaims.
  await guardAgainstDoubleSubmit(document.getElementById('exp-create-btn'), 'Saving...', createExpenseClaimInner);
}

async function createExpenseClaimInner() {
  const errorEl = document.getElementById('exp-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('exp-employee').value;
  const location = document.getElementById('exp-location').value.trim();
  const expenseDate = document.getElementById('exp-date').value;
  const category = document.getElementById('exp-category').value;
  const amount = parseFloat(document.getElementById('exp-amount').value);
  const gstAmount = parseFloat(document.getElementById('exp-gst').value) || 0;
  const advanceAdjusted = parseFloat(document.getElementById('exp-advance').value) || 0;
  const receiptNumber = document.getElementById('exp-receipt').value.trim();
  const purpose = document.getElementById('exp-purpose').value.trim();

  const missing = [!employeeId && 'Employee', !location && 'Location', !expenseDate && 'Expense Date', !(amount > 0) && 'Amount'].filter(Boolean);
  if (missing.length) {
    errorEl.textContent = `Fill in: ${missing.join(', ')}.`;
    errorEl.classList.remove('hidden');
    return;
  }
  if (gstAmount > amount || advanceAdjusted > amount + gstAmount) {
    errorEl.textContent = 'GST cannot be more than the amount, and the advance cannot be more than the bill.';
    errorEl.classList.remove('hidden');
    return;
  }
  const receiptFiles = Array.from(document.getElementById('exp-files').files || []);
  const receiptProblem = receiptSelectionProblem(receiptFiles);
  if (receiptProblem) {
    errorEl.textContent = receiptProblem;
    errorEl.classList.remove('hidden');
    return;
  }

  const body = {
    employee_id: employeeId, location, expense_date: expenseDate,
    category, amount, gst_amount: gstAmount, advance_adjusted: advanceAdjusted,
    receipt_number: receiptNumber, purpose, status: 'Draft'
  };
  // A correction keeps the claim (and its history) and returns it to Draft;
  // the server decides whether that edit is allowed.
  let url = '/api/v1/doc/ExpenseClaim';
  if (expenseEditingId) {
    const cur = await apiFetch(`/api/v1/doc/ExpenseClaim/${encodeURIComponent(expenseEditingId)}`);
    if (cur && cur.ok) Object.assign(body, { ...(await cur.json()), ...body });
    url = `/api/v1/doc/ExpenseClaim/${encodeURIComponent(expenseEditingId)}`;
  }
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to save the expense claim.');
    errorEl.classList.remove('hidden');
    return;
  }
  const corrected = expenseEditingId;
  const saved = await res.json().catch(() => ({}));
  const claimId = corrected || saved.id;
  // The claim is saved first, so a receipt that fails to upload never loses
  // what was typed; it can be attached again from the claim's History.
  const failed = receiptFiles.length && claimId ? await uploadExpenseReceipts(claimId, receiptFiles) : [];
  expenseEditingId = null;
  txnDone('exp-screen');
  if (failed.length) {
    showToast(`Claim saved as Draft, but ${failed.length === 1 ? 'a receipt was' : `${failed.length} receipts were`} not attached - ${failed.join(' ')} Attach it again from History.`, { variant: 'warning' });
  } else if (corrected) {
    showToast('Correction saved as Draft - submit it for approval again.', { variant: 'success' });
  }
  renderView('expenses');
}

export {
  renderExpensesView,
  expenseStatusBadge,
  renderExpenseActions,
  createExpenseClaim,
  createExpenseClaimInner,
  submitExpenseForApproval,
  verifyExpenseClaim,
  payExpenseClaim
};

async function submitExpenseForApproval(claimId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'ExpenseClaim', document_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.', 'Submit Failed');
    return;
  }
  renderView('expenses');
}

async function verifyExpenseClaim(claimId) {
  const res = await apiFetch('/api/v1/expenses/verify', {
    method: 'POST',
    body: JSON.stringify({ claim_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to verify claim.', 'Verification Failed');
    return;
  }
  renderView('expenses');
}

async function payExpenseClaim(claimId) {
  const confirmed = await showCustomConfirm('This posts the reimbursement (the claim less any advance) to the GL and marks the claim Paid. It can be paid once. Continue?', 'Pay Expense Claim');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/expenses/pay', {
    method: 'POST',
    body: JSON.stringify({ claim_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to pay claim.', 'Payment Failed');
    return;
  }
  renderView('expenses');
}
