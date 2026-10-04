// Finance Verification -> Payment -> Accounting. Manager Approval reuses
// the existing Approval/Workflow Engine (Stage 13.8) - once submitted, a
// claim shows up in the existing "Approvals" screen automatically (it
// queries every approval-gated doctype), so this screen only needs to
// handle claim creation/submission plus the two stages after approval.
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
async function renderExpensesView(container) {
  // 30.5.8: the parallel Employee fetch that used to sit alongside this one
  // only existed to build the picker's <option>s; the picker is a typeahead
  // now and fetches on demand.
  const claimsRes = await apiFetch('/api/v1/doc/ExpenseClaim');
  if (!claimsRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Expenses</h1>
      <p class="page-subtitle">Claim &rarr; Manager Approval &rarr; Finance Verification &rarr; Payment.</p>
    </div>
  `;
  container.appendChild(header);

  const claims = claimsRes.ok ? await claimsRes.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Expense Claim</h2>
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
        <label class="form-label" for="exp-amount">Amount</label>
        <input type="number" id="exp-amount" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-gst">GST Amount</label>
        <input type="number" id="exp-gst" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-advance">Advance Adjusted</label>
        <input type="number" id="exp-advance" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 160px;">
        <label class="form-label" for="exp-purpose">Purpose</label>
        <input type="text" id="exp-purpose" class="form-input">
      </div>
      <button class="btn btn-primary" id="exp-create-btn">Create Draft</button>
    </div>
    <div id="exp-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr><th>Claim #</th><th>Employee</th><th>Category</th><th>Amount</th><th>GST</th><th>Status</th><th></th></tr>
      </thead>
      <tbody>
  `;
  html += claims.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No expense claims yet. Use <b>Create Draft</b> above to raise one.</td></tr>`
    : claims.map(c => `
        <tr>
          <td style="font-family: monospace;">${c.code || c.id}</td>
          <td>${c.employee_id || ''}</td>
          <td>${c.category || ''}</td>
          <td>${(c.amount ?? 0).toLocaleString()}</td>
          <td>${(c.gst_amount ?? 0).toLocaleString()}</td>
          <td><span class="badge ${expenseStatusBadge(c.status)}">${c.status}</span></td>
          <td>${renderExpenseActions(c)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('exp-create-btn').addEventListener('click', createExpenseClaim);
  attachEmployeePicker('exp-employee');
  attachLinkTypeahead(document.getElementById('exp-location'), 'Location');
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
  return '';
}

async function createExpenseClaim() {
  // BLD-036: guard against a double-click creating two ExpenseClaims.
  await guardAgainstDoubleSubmit(document.getElementById('exp-create-btn'), 'Creating...', createExpenseClaimInner);
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
  const purpose = document.getElementById('exp-purpose').value.trim();

  if (!employeeId || !location || !expenseDate || !amount) {
    errorEl.textContent = 'Employee, Location, Expense Date, and Amount are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ExpenseClaim', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employeeId, location, expense_date: expenseDate,
      category, amount, gst_amount: gstAmount, advance_adjusted: advanceAdjusted,
      purpose, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create expense claim.';
    errorEl.classList.remove('hidden');
    return;
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
  const confirmed = await showCustomConfirm('This will post the payment GL entry and mark the claim Paid. Continue?', 'Pay Expense Claim');
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
