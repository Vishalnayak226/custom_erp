// Journal Vouchers (2026-10-09). The engine (engines/journal_voucher.go) and
// its API have existed since Stage 26.6, but nothing in the UI could create
// one: the JournalVoucher record has no lines field, so the generic form
// could not take debits and credits. Finance had no way to post an accrual,
// a correction or an opening balance short of an API call. This screen is
// that missing front end - it adds no new posting path:
//   create  -> POST /api/v1/finance/journal-voucher (a Draft)
//   submit  -> POST /api/v1/approval/submit (the maker-checker engine; the
//              approver decides on the Approvals screen, and approval posts it)
//   reverse -> POST /api/v1/finance/journal-voucher/{id}/reverse
//   retry   -> POST /api/v1/finance/journal-voucher/{id}/retry-post
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.

let jvAccounts = null;
let jvLines = [];

const JV_STATUS_BADGE = {
  Draft: 'badge-secondary', 'Pending Approval': 'badge-warning', Approved: 'badge-warning',
  Posted: 'badge-success', Reversed: 'badge-secondary', Rejected: 'badge-danger'
};

async function loadJVAccounts() {
  if (jvAccounts) return jvAccounts;
  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(localISODate(new Date()))}`);
  jvAccounts = [];
  if (res && res.ok) {
    const tb = await res.json();
    jvAccounts = (tb.balances || []).map(b => ({ code: b.account_code, name: b.account_name, type: b.account_type }));
  }
  return jvAccounts;
}

function jvLineCount(v) {
  try { return (typeof v.lines === 'string' ? JSON.parse(v.lines) : (v.lines || [])).length; } catch { return 0; }
}

async function renderJournalVouchersView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Journal Vouchers</h1>
      <p class="page-subtitle">Manual entries - accruals, corrections, opening balances. Debits must equal credits; a voucher posts only after a second person approves it.</p>
    </div>
    <button class="btn btn-primary" id="jv-new-btn" type="button">+ New Journal Voucher</button>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel hidden';
  formPanel.id = 'jv-form-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  container.appendChild(formPanel);
  document.getElementById('jv-new-btn').addEventListener('click', openJournalVoucherForm);

  const res = await apiFetch('/api/v1/doc/JournalVoucher');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load journal vouchers.', () => renderView('journal-vouchers')); return; }
  const vouchers = (await res.json()).filter(v => v.status !== 'Recurring Template');
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Voucher</th><th>Date</th><th>Narration</th><th class="num">Lines</th><th class="num">Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${vouchers.length === 0
          ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No journal vouchers yet. Use <b>+ New Journal Voucher</b> above.</td></tr>`
          : vouchers.map(v => `
            <tr>
              <td style="font-family: monospace;">${escapeHTMLText(v.voucher_number || v.id)}</td>
              <td>${escapeHTMLText(v.voucher_date || '')}</td>
              <td>${escapeHTMLText(v.narration || '')}</td>
              <td class="num">${jvLineCount(v)}</td>
              <td class="num">${Number(v.total_amount || 0).toLocaleString()}</td>
              <td><span class="badge ${JV_STATUS_BADGE[v.status] || 'badge-secondary'}">${escapeHTMLText(v.status || '')}</span></td>
              <td>${renderJournalVoucherActions(v)}</td>
            </tr>`).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

function renderJournalVoucherActions(v) {
  if (v.status === 'Draft') return `<button class="action-btn" ${actionAttrs('submitJournalVoucher', [v.id])}>Submit for Approval</button>`;
  if (v.status === 'Pending Approval') return `<span style="color: var(--text-muted); font-size: 12px;">Awaiting approval on the Approvals screen</span>`;
  if (v.status === 'Approved') return `<button class="action-btn" ${actionAttrs('retryPostJournalVoucher', [v.id])}>Retry Post</button>`;
  if (v.status === 'Posted') return `<button class="action-btn" ${actionAttrs('reverseJournalVoucher', [v.id])}>Reverse</button>`;
  return '';
}

async function openJournalVoucherForm() {
  const panel = document.getElementById('jv-form-panel');
  const accounts = await loadJVAccounts();
  jvLines = [{ account: '', debit: '', credit: '' }, { account: '', debit: '', credit: '' }];
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Journal Voucher</h2>
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="jv-date">Date<span class="required">*</span></label>
        <input type="date" id="jv-date" class="form-input" style="width: 170px;" value="${localISODate(new Date())}">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 260px;">
        <label class="form-label" for="jv-narration">Narration<span class="required">*</span></label>
        <input type="text" id="jv-narration" class="form-input" placeholder="What this entry is for, in words an auditor will understand">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="jv-cost-center">Cost Center</label>
        <input type="text" id="jv-cost-center" class="form-input" style="width: 180px;" autocomplete="off" placeholder="Optional">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="jv-department">Department</label>
        <input type="text" id="jv-department" class="form-input" style="width: 180px;" autocomplete="off" placeholder="Optional">
      </div>
    </div>
    <datalist id="jv-account-options">${accounts.map(a => `<option value="${escapeHTMLText(a.code)}">${escapeHTMLText(a.code + ' · ' + a.name)}</option>`).join('')}</datalist>
    <table>
      <thead><tr><th>Account</th><th class="num">Debit</th><th class="num">Credit</th><th></th></tr></thead>
      <tbody id="jv-lines-body"></tbody>
      <tfoot><tr><td><button class="btn btn-outline btn-sm" id="jv-add-line" type="button">+ Add line</button></td>
        <td class="num" id="jv-total-debit">0</td><td class="num" id="jv-total-credit">0</td><td id="jv-balance-note"></td></tr></tfoot>
    </table>
    <div id="jv-error" class="login-error hidden" style="margin-top: 12px;"></div>
    <div style="display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px;">
      <button class="btn btn-outline" id="jv-cancel-btn" type="button">Cancel</button>
      <button class="btn btn-primary" id="jv-save-btn" type="button">Save as Draft</button>
    </div>
  `;
  panel.classList.remove('hidden');
  attachLinkTypeahead(document.getElementById('jv-cost-center'), 'CostCenter', { noSetupHint: true });
  attachLinkTypeahead(document.getElementById('jv-department'), 'Department', { noSetupHint: true });
  document.getElementById('jv-add-line').addEventListener('click', () => { jvLines.push({ account: '', debit: '', credit: '' }); renderJournalVoucherLines(); });
  document.getElementById('jv-cancel-btn').addEventListener('click', () => panel.classList.add('hidden'));
  document.getElementById('jv-save-btn').addEventListener('click', () => guardAgainstDoubleSubmit(document.getElementById('jv-save-btn'), 'Saving...', saveJournalVoucher));
  renderJournalVoucherLines();
  document.getElementById('jv-narration').focus();
}

function jvAccountLabel(code) {
  const a = (jvAccounts || []).find(x => x.code === code);
  return a ? `${a.name} (${a.type})` : '';
}

function renderJournalVoucherLines() {
  const body = document.getElementById('jv-lines-body');
  if (!body) return;
  body.innerHTML = jvLines.map((l, i) => `
    <tr>
      <td>
        <input type="text" class="form-input" list="jv-account-options" data-jv-line="${i}" data-jv-field="account" value="${escapeHTMLText(l.account)}" placeholder="Account code or name" style="width: 220px;">
        <div class="jv-account-name" style="font-size: 12px; color: var(--text-muted);">${escapeHTMLText(jvAccountLabel(l.account))}</div>
      </td>
      <td class="num"><input type="number" class="form-input num" min="0" step="1" data-jv-line="${i}" data-jv-field="debit" value="${escapeHTMLText(l.debit)}" style="width: 130px;"></td>
      <td class="num"><input type="number" class="form-input num" min="0" step="1" data-jv-line="${i}" data-jv-field="credit" value="${escapeHTMLText(l.credit)}" style="width: 130px;"></td>
      <td>${jvLines.length > 2 ? `<button class="action-btn action-btn-danger" type="button" data-jv-remove="${i}">Remove</button>` : ''}</td>
    </tr>`).join('');
  body.querySelectorAll('[data-jv-field]').forEach(input => input.addEventListener('input', () => {
    const line = jvLines[Number(input.dataset.jvLine)];
    line[input.dataset.jvField] = input.value.trim();
    // A line is a debit OR a credit: typing one clears the other.
    if (input.dataset.jvField !== 'account' && input.value) {
      const other = input.dataset.jvField === 'debit' ? 'credit' : 'debit';
      line[other] = '';
      const otherEl = body.querySelector(`[data-jv-line="${input.dataset.jvLine}"][data-jv-field="${other}"]`);
      if (otherEl) otherEl.value = '';
    }
    if (input.dataset.jvField === 'account') {
      // Typing "Cash" should find 1100 Cash/Bank Account as well as typing 1100.
      const match = (jvAccounts || []).find(a => a.code === line.account) ||
        (jvAccounts || []).find(a => line.account && a.name.toLowerCase() === line.account.toLowerCase());
      if (match && match.code !== line.account) { line.account = match.code; input.value = match.code; }
      input.parentElement.querySelector('.jv-account-name').textContent = jvAccountLabel(line.account);
    }
    updateJournalVoucherTotals();
  }));
  body.querySelectorAll('[data-jv-remove]').forEach(btn => btn.addEventListener('click', () => {
    jvLines.splice(Number(btn.dataset.jvRemove), 1);
    renderJournalVoucherLines();
  }));
  updateJournalVoucherTotals();
}

function updateJournalVoucherTotals() {
  const sum = f => jvLines.reduce((s, l) => s + (parseInt(l[f], 10) || 0), 0);
  const dr = sum('debit'), cr = sum('credit');
  document.getElementById('jv-total-debit').textContent = dr.toLocaleString();
  document.getElementById('jv-total-credit').textContent = cr.toLocaleString();
  const note = document.getElementById('jv-balance-note');
  if (dr > 0 && dr === cr) {
    note.innerHTML = '<span class="badge badge-success">Balanced</span>';
  } else {
    note.innerHTML = `<span class="badge badge-warning">Off by ${Math.abs(dr - cr).toLocaleString()}</span>`;
  }
}

async function saveJournalVoucher() {
  const errorEl = document.getElementById('jv-error');
  errorEl.classList.add('hidden');
  const fail = msg => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const voucherDate = document.getElementById('jv-date').value;
  const narration = document.getElementById('jv-narration').value.trim();
  const lines = jvLines
    .filter(l => l.account || l.debit || l.credit)
    .map(l => ({ account_code: l.account, debit: parseInt(l.debit, 10) || 0, credit: parseInt(l.credit, 10) || 0 }));
  if (!voucherDate || !narration) return fail('Date and narration are required.');
  if (lines.length < 2) return fail('A journal voucher needs at least two lines.');
  const unknown = lines.find(l => !(jvAccounts || []).some(a => a.code === l.account_code));
  if (unknown) return fail(`"${unknown.account_code || '(blank)'}" is not an account in the Chart of Accounts.`);
  const dr = lines.reduce((s, l) => s + l.debit, 0), cr = lines.reduce((s, l) => s + l.credit, 0);
  if (dr !== cr || dr === 0) return fail(`Debits (${dr}) must equal credits (${cr}).`);

  const res = await apiFetch('/api/v1/finance/journal-voucher', {
    method: 'POST',
    body: JSON.stringify({
      voucher_date: voucherDate, narration, lines,
      cost_center: document.getElementById('jv-cost-center').value.trim(),
      department: document.getElementById('jv-department').value.trim()
    })
  });
  if (!res) return;
  if (!res.ok) { fail(await getErrorMessage(res, 'The voucher could not be saved.')); return; }
  showToast('Journal voucher saved as Draft. Submit it for approval to post it.');
  renderView('journal-vouchers');
}

async function submitJournalVoucher(id) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'JournalVoucher', document_id: id })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to submit the voucher for approval.'); return; }
  showToast('Submitted. An approver decides it on the Approvals screen; approval posts it to the ledger.');
  renderView('journal-vouchers');
}

async function reverseJournalVoucher(id) {
  if (!await showCustomConfirm('Create a reversing voucher? It swaps every debit and credit, and goes through approval like any other voucher.', 'Reverse Voucher')) return;
  const res = await apiFetch(`/api/v1/finance/journal-voucher/${encodeURIComponent(id)}/reverse`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to reverse the voucher.'); return; }
  showToast('Reversing voucher created as Draft.');
  renderView('journal-vouchers');
}

async function retryPostJournalVoucher(id) {
  const res = await apiFetch(`/api/v1/finance/journal-voucher/${encodeURIComponent(id)}/retry-post`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Posting failed again.'); return; }
  renderView('journal-vouchers');
}

export {
  renderJournalVouchersView,
  renderJournalVoucherActions,
  openJournalVoucherForm,
  saveJournalVoucher,
  submitJournalVoucher,
  reverseJournalVoucher,
  retryPostJournalVoucher
};
