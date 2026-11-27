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

// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnRestorePane, txnPageTitle, txnNewButton, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

let jvAccounts = null;
let jvLines = [];

const JV_STATUS_BADGE = {
  Draft: 'badge-secondary', 'Pending Approval': 'badge-warning', Approved: 'badge-warning',
  Posted: 'badge-success', Reversed: 'badge-secondary', Rejected: 'badge-danger', 'Recurring Template': 'badge-secondary'
};

let jvVouchers = [];
const jvTools = () => import('./view-journal-tools.js' + new URL(import.meta.url).search);
const JV_DETAIL_FIELDS = [['cost_center', 'Cost centre', 'CostCenter'], ['department', 'Department', 'Department'], ['store', 'Store', 'Location'], ['employee', 'Employee', 'Employee'], ['tag', 'Tag', null], ['narration', 'Narration', null]];

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
  const res = await apiFetch('/api/v1/doc/JournalVoucher');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load journal vouchers.', () => renderView('journal-vouchers')); return; }
  const vouchers = await res.json();
  jvVouchers = vouchers;

  // Stage 68: the frozen transaction layout (txn-screen.css) - the debit /
  // credit totals and Save live in the bar. Stage 69: opens on the list; New
  // is its own page.
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.id = 'jv-screen';
  screen.dataset.landing = 'history';
  screen.innerHTML = `
    <div class="txn-bar">
      ${txnPageTitle({ screen: 'jv-screen', title: 'Journal Vouchers', page: 'New journal voucher', count: vouchers.length })}
      ${txnContext()}
      ${txnNewButton({ screen: 'jv-screen', label: 'New Journal Voucher' })}
      <div class="txn-sum txn-main-only">
        <span>Debit <b id="jv-total-debit">0</b></span>
        <span>Credit <b id="jv-total-credit">0</b></span>
        <span id="jv-balance-note"></span>
      </div>
      <div class="txn-actions txn-main-only">
        <button class="btn btn-outline btn-sm" id="jv-use-tpl" type="button">Use template</button>
        <button class="btn btn-outline btn-sm" id="jv-save-tpl" type="button">Save as template</button>
        <button class="btn btn-outline btn-sm" id="jv-copy-last" type="button">Copy last month</button>
        <div id="jv-error" class="login-error hidden"></div>
        <button class="btn btn-outline btn-sm" id="jv-cancel-btn" type="button" title="Clear this voucher and start again">Clear</button>
        <button class="btn btn-primary" id="jv-save-btn" type="button">Save as Draft</button>
      </div>
    </div>
    <div class="txn-pane" data-pane="new" id="jv-form-panel"></div>
    <div class="txn-pane hidden" data-pane="history">
      <div class="s-2e43e0">
        <button class="btn btn-outline btn-sm" id="jv-fx" type="button">FX revaluation</button>
        <button class="btn btn-outline btn-sm" id="jv-ic" type="button">Intercompany entry</button>
      </div>
      <div class="txn-grid">
        <table>
          <thead><tr><th>Voucher</th><th>Date</th><th>Narration</th><th class="num">Lines</th><th class="num">Amount</th><th>Status</th><th>Actions</th></tr></thead>
          <tbody>
            ${vouchers.length === 0
              ? `<tr><td colspan="7" class="s-306094">No journal vouchers yet.</td></tr>`
              : vouchers.map(v => `
                <tr>
                  <td class="s-355794">${escapeHTMLText(v.voucher_number || v.id)}</td>
                  <td class="nowrap">${escapeHTMLText(v.voucher_date || '')}</td>
                  <td>${escapeHTMLText(v.narration || '')}</td>
                  <td class="num">${jvLineCount(v)}</td>
                  <td class="num">${formatNumber(v.total_amount || 0)}</td>
                  <td><span class="badge ${JV_STATUS_BADGE[v.status] || 'badge-secondary'}">${escapeHTMLText(v.status || '')}</span></td>
                  <td>${renderJournalVoucherActions(v, vouchers)}</td>
                </tr>`).join('')}
          </tbody>
        </table>
      </div>
    </div>
  `;
  container.appendChild(screen);
  document.getElementById('jv-cancel-btn').addEventListener('click', openJournalVoucherForm);
  document.getElementById('jv-save-btn').addEventListener('click', () => guardAgainstDoubleSubmit(document.getElementById('jv-save-btn'), 'Saving...', saveJournalVoucher));
  document.getElementById('jv-use-tpl').addEventListener('click', async () => (await jvTools()).openUseJournalTemplate(jvApplyLines));
  document.getElementById('jv-save-tpl').addEventListener('click', async () => (await jvTools()).openSaveJournalTemplate(jvLines, document.getElementById('jv-narration').value.trim()));
  document.getElementById('jv-copy-last').addEventListener('click', async () => (await jvTools()).openCopyLastMonth(jvVouchers, jvApplyLines));
  document.getElementById('jv-fx').addEventListener('click', async () => (await jvTools()).openFxRevaluation());
  document.getElementById('jv-ic').addEventListener('click', async () => (await jvTools()).openIntercompanyEntry());
  await openJournalVoucherForm();
  txnRestorePane('jv-screen');
}

// Fill the New pane from a template or last month's voucher.
function jvApplyLines(lines, narration, date) {
  if (lines.length) jvLines = lines;
  while (jvLines.length < 2) jvLines.push({ account: '', debit: '', credit: '' });
  if (narration) document.getElementById('jv-narration').value = narration;
  if (date) document.getElementById('jv-date').value = date;
  renderJournalVoucherLines();
}

function renderJournalVoucherActions(v, all = []) {
  if (v.status === 'Recurring Template') return `<span class="s-817a95">${escapeHTMLText(v.recurring_frequency || '')} · next ${escapeHTMLText(v.next_run_date || '')}</span>`;
  if (v.status === 'Draft') return `<button class="action-btn" ${actionAttrs('submitJournalVoucher', [v.id])}>Submit for Approval</button>`;
  if (v.status === 'Pending Approval') return `<span class="s-817a95">Awaiting approval on the Approvals screen</span>`;
  if (v.status === 'Approved') return `<button class="action-btn" ${actionAttrs('retryPostJournalVoucher', [v.id])}>Retry Post</button>`;
  if (v.status === 'Posted') {
    // Stage 132.17: a voucher stays Posted until its reversal posts; while
    // one is on its way, say so instead of offering a second.
    const rev = v.reversed_by && all.find(x => x.id === v.reversed_by);
    if (rev && !['Rejected', 'Cancelled'].includes(rev.status)) return `<span class="s-817a95">Being reversed by ${escapeHTMLText(rev.voucher_number || rev.id)} (${escapeHTMLText(rev.status)})</span>`;
    return `<button class="action-btn" ${actionAttrs('reverseJournalVoucher', [v.id])}>Reverse</button>`;
  }
  return '';
}

// openJournalVoucherForm (re)starts the New pane with a blank two-line voucher.
async function openJournalVoucherForm() {
  const panel = document.getElementById('jv-form-panel');
  const accounts = await loadJVAccounts();
  jvLines = [{ account: '', debit: '', credit: '' }, { account: '', debit: '', credit: '' }];
  document.getElementById('jv-error')?.classList.add('hidden');
  panel.innerHTML = `
    <div class="txn-head">
      <div class="form-group">
        <label class="form-label" for="jv-date">Date<span class="required">*</span></label>
        <input type="date" id="jv-date" class="form-input" value="${localISODate(new Date())}">
      </div>
      <div class="form-group jv-narration">
        <label class="form-label" for="jv-narration">Narration<span class="required">*</span></label>
        <input type="text" id="jv-narration" class="form-input" placeholder="What this entry is for, in words an auditor will understand">
      </div>
      <div class="form-group">
        <label class="form-label" for="jv-cost-center">Cost Center</label>
        <input type="text" id="jv-cost-center" class="form-input" autocomplete="off" placeholder="Optional">
      </div>
      <div class="form-group">
        <label class="form-label" for="jv-department">Department</label>
        <input type="text" id="jv-department" class="form-input" autocomplete="off" placeholder="Optional">
      </div>
    </div>
    <div class="txn-grid">
      <table>
        <thead><tr><th>Account</th><th class="num">Debit</th><th class="num">Credit</th><th></th></tr></thead>
        <tbody id="jv-lines-body"></tbody>
        <tfoot><tr><td colspan="4"><button class="btn btn-outline btn-sm" id="jv-add-line" type="button">+ Add line</button></td></tr></tfoot>
      </table>
    </div>
  `;
  attachLinkTypeahead(document.getElementById('jv-cost-center'), 'CostCenter', { noSetupHint: true });
  attachLinkTypeahead(document.getElementById('jv-department'), 'Department', { noSetupHint: true });
  document.getElementById('jv-add-line').addEventListener('click', () => { jvLines.push({ account: '', debit: '', credit: '' }); renderJournalVoucherLines(); });
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
        <input type="text" class="form-input s-30080b" data-jv-line="${i}" data-jv-field="account" value="${escapeHTMLText(l.account)}" placeholder="Account code or name">
        <div class="jv-account-name s-6e0fd9">${escapeHTMLText(jvAccountLabel(l.account))}</div>
      </td>
      <td class="num"><input type="number" class="form-input num s-27652d" min="0" step="0.01" data-jv-line="${i}" data-jv-field="debit" value="${escapeHTMLText(l.debit)}"></td>
      <td class="num"><input type="number" class="form-input num s-27652d" min="0" step="0.01" data-jv-line="${i}" data-jv-field="credit" value="${escapeHTMLText(l.credit)}"></td>
      <td>${jvLines.length > 2 ? `<button class="action-btn action-btn-danger" type="button" data-jv-remove="${i}">Remove</button>` : ''}
        <button class="action-btn" type="button" data-jv-more="${i}">Details</button></td>
    </tr>
    <tr class="${JV_DETAIL_FIELDS.some(([k]) => l[k]) ? '' : 'hidden'}" data-jv-detail="${i}"><td colspan="4"><div class="s-790047">
      ${JV_DETAIL_FIELDS.map(([k, label]) => `<input type="text" class="form-input s-0a9e77" autocomplete="off" placeholder="${label}" aria-label="${label}" data-jv-line="${i}" data-jv-field="${k}" value="${escapeHTMLText(l[k] || '')}">`).join('')}
    </div></td></tr>`).join('');
  JV_DETAIL_FIELDS.forEach(([k, , doctype]) => { if (doctype) body.querySelectorAll(`[data-jv-field="${k}"]`).forEach(el => attachLinkTypeahead(el, doctype, { noSetupHint: true })); });
  const accountRows = (jvAccounts || []).map(a => ({ value: a.code, label: `${a.code} · ${a.name}` }));
  body.querySelectorAll('[data-jv-field="account"]').forEach(el => attachSuggestList(el, accountRows)); // 294.1
  body.querySelectorAll('[data-jv-more]').forEach(btn => btn.addEventListener('click', () => body.querySelector(`[data-jv-detail="${btn.dataset.jvMore}"]`).classList.toggle('hidden')));
  body.querySelectorAll('[data-jv-field]').forEach(input => input.addEventListener('input', () => {
    const line = jvLines[Number(input.dataset.jvLine)];
    line[input.dataset.jvField] = input.value.trim();
    // A line is a debit OR a credit: typing one clears the other.
    if ((input.dataset.jvField === 'debit' || input.dataset.jvField === 'credit') && input.value) {
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

// Amounts are rupees with paise; balance is checked in whole paise (193.2).
const jvPaise = v => Math.round((parseFloat(v) || 0) * 100);
const jvMoney = n => n.toLocaleString('en-IN', { minimumFractionDigits: 0, maximumFractionDigits: 2 });

function updateJournalVoucherTotals() {
  const sum = f => jvLines.reduce((s, l) => s + jvPaise(l[f]), 0) / 100;
  const dr = sum('debit'), cr = sum('credit');
  document.getElementById('jv-total-debit').textContent = jvMoney(dr);
  document.getElementById('jv-total-credit').textContent = jvMoney(cr);
  const note = document.getElementById('jv-balance-note');
  if (dr > 0 && dr === cr) {
    note.innerHTML = '<span class="badge badge-success">Balanced</span>';
  } else {
    note.innerHTML = `<span class="badge badge-warning">Off by ${jvMoney(Math.abs(dr - cr))}</span>`;
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
    .map(l => {
      const row = { account_code: l.account, debit: parseFloat(l.debit) || 0, credit: parseFloat(l.credit) || 0 };
      JV_DETAIL_FIELDS.forEach(([k]) => { if (l[k]) row[k] = l[k]; });
      return row;
    });
  if (!voucherDate || !narration) return fail('Date and narration are required.');
  if (lines.length < 2) return fail('A journal voucher needs at least two lines.');
  const unknown = lines.find(l => !(jvAccounts || []).some(a => a.code === l.account_code));
  if (unknown) return fail(`"${unknown.account_code || '(blank)'}" is not an account in the Chart of Accounts.`);
  const dr = lines.reduce((s, l) => s + jvPaise(l.debit), 0) / 100, cr = lines.reduce((s, l) => s + jvPaise(l.credit), 0) / 100;
  if (dr !== cr || dr === 0) return fail(`Debits (${jvMoney(dr)}) must equal credits (${jvMoney(cr)}).`);

  const res = await apiFetch('/api/v1/finance/journal-voucher', {
    method: 'POST', commandKey: 'jv.create', // 49.4.7a: a re-send after no answer posts once
    body: JSON.stringify({
      voucher_date: voucherDate, narration, lines,
      cost_center: document.getElementById('jv-cost-center').value.trim(),
      department: document.getElementById('jv-department').value.trim()
    })
  });
  if (!res) return;
  if (!res.ok) { fail(await getErrorMessage(res, 'The voucher could not be saved.')); return; }
  showToast('Journal voucher saved as Draft. Submit it for approval to post it.');
  txnDone('jv-screen');
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
  showToast('Reversing voucher created as Draft - this one shows Reversed once that posts.');
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
