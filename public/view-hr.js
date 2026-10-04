// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentHRTab = 'attendance';
const HR_TABS = [
  { id: 'attendance', label: 'Attendance' },
  { id: 'leave', label: 'Leave' },
  { id: 'payroll-export', label: 'Payroll Export' },
  { id: 'roster', label: 'Shift Roster' },
  { id: 'payroll', label: 'Payroll' },
  { id: 'loans', label: 'Loans/Advances' },
  { id: 'onboarding', label: 'Onboarding/Offboarding' },
  { id: 'appraisals', label: 'Appraisals' },
  { id: 'training', label: 'Training' },
  { id: 'grievances', label: 'Grievances' },
  { id: 'my-requests', label: 'My Requests' }
];

async function renderHRView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">HR</h1>
      <p class="page-subtitle">Attendance, leave, and payroll export. Manage employees under Setup.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  // Stage 45: at a narrow enough width/zoom this row is wider than the page
  // - .page-container clips overflow-x rather than scrolling it, so without
  // this the trailing tabs were just gone with no way back to them. Scoping
  // the scroll to the bar itself (not the whole page) keeps the rest of the
  // screen from picking up a horizontal scrollbar too.
  //
  // flex-shrink: 0 is load-bearing, not decorative. Verified live: setting
  // only overflow-x on a flex item inside .page-container's column flexbox
  // let the whole bar collapse to 0px height whenever the page's other
  // content was tall enough to overflow vertically - overflow-x non-visible
  // suppresses this item's automatic min-height (content-based sizing only
  // applies when overflow is 'visible'), and per the CSS Overflow spec the
  // *used* value of overflow-y becomes 'auto' here regardless of what's
  // declared (confirmed: getComputedStyle still reports 'auto' even with
  // overflow-y explicitly set to 'visible' below - that line documents
  // intent but does not itself prevent the collapse). flex-shrink: 0 is what
  // actually fixes it: it exempts the item from shrinking at all, which
  // sidesteps the automatic-minimum-size calculation entirely rather than
  // trying to win it.
  tabBar.style.overflowX = 'auto';
  tabBar.style.overflowY = 'visible';
  tabBar.style.flexShrink = '0';
  tabBar.innerHTML = HR_TABS.map(t =>
    `<button class="btn ${t.id === currentHRTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-hr-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-hr-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentHRTab = btn.getAttribute('data-hr-tab');
      renderView('hr');
    });
  });

  // Stage 30.5.8: the whole Employee list used to be fetched here, on every
  // HR tab render, purely to build <option>s - including for the eight tabs
  // that never showed an employee picker at all. The pickers are typeaheads
  // now and fetch what they need on demand, so the eager list is gone.
  if (currentHRTab === 'attendance') {
    await renderAttendanceTab(container);
  } else if (currentHRTab === 'leave') {
    await renderLeaveTab(container);
  } else if (currentHRTab === 'payroll-export') {
    renderPayrollExportTab(container);
  } else if (currentHRTab === 'roster') {
    currentDoctype = 'ShiftAssignment';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('hr', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentHRTab === 'payroll') {
    await renderPayrollTab(container);
  } else if (currentHRTab === 'loans') {
    await renderEmployeeLoansTab(container);
  } else if (currentHRTab === 'onboarding') {
    currentDoctype = 'OnboardingChecklist';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('hr', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentHRTab === 'appraisals') {
    // 26.8.8 (P2, go-ahead 2026-07-27): AppraisalCycle (the KRA/KPI
    // template) is a Master, managed under Setup like every other master -
    // this tab is just the per-employee Appraisal transactions against it.
    currentDoctype = 'Appraisal';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('hr', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentHRTab === 'training') {
    // TrainingProgram is a Master (managed under Setup); this tab is the
    // per-employee completion records against it.
    currentDoctype = 'TrainingRecord';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('hr', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentHRTab === 'grievances') {
    currentDoctype = 'Grievance';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderLazyView('hr', container, LAZY_VIEW_MODULES['doctype-table']);
  } else if (currentHRTab === 'my-requests') {
    await renderMyRequestsTab(container);
  }
}

// The employee picker, in one place (Stage 30.5.8).
//
// Six screens ask for an employee. Five hand-built a <select> listing every
// employee up front; Asset's custodian used the typeahead that every other
// master picker in this app uses. That split cost more than tidiness - the
// <select> made each of those five screens fetch the entire Employee list on
// load just to build <option>s, and gave no way to search it once a tenant
// has more staff than fit on a screen.
//
// Settled on the typeahead, being the control ~35 other pickers already use,
// with showAllOnFocus (see TYPEAHEAD_DOCTYPE_OPTS) so the one thing the
// <select> genuinely did better - "show me everyone without typing" - is
// still there. Two things this does not lose:
//   - 30.5.1's empty-state guidance. It gets better, in fact: an empty list
//     used to render a static hint underneath, and now focusing the field
//     opens the typeahead's dead-end row, which names the doctype and links
//     straight to where an Employee is created.
//   - validation. employee_id is a Link field, so an id that names no real
//     record is refused server-side with META-0198 whichever control typed
//     it (engines/doctype.go) - the free-text input is not a new hole.
async function renderAttendanceTab(container) {
  const res = await apiFetch('/api/v1/doc/Attendance');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Mark Attendance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Attendance Code', 'ATT', '160px')}
${employeePickerField('att-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-date">Date</label>
        <input type="date" id="att-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-location">Location</label>
        <input type="text" id="att-location" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-status">Status</label>
        <select id="att-status" class="form-input" style="width: 130px;">
          <option value="Present">Present</option>
          <option value="Absent">Absent</option>
          <option value="Late">Late</option>
          <option value="Leave">Leave</option>
          <option value="Holiday">Holiday</option>
          <option value="WeeklyOff">WeeklyOff</option>
        </select>
      </div>
      <button class="btn btn-primary" id="att-save-btn">Save</button>
    </div>
    <div id="att-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Date</th><th>Location</th><th>Status</th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No attendance records yet. Pick an employee and a date above, then <b>Save</b>.</td></tr>`
    : records.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.employee_id || ''}</td>
          <td>${r.date || ''}</td>
          <td>${r.location || ''}</td>
          <td><span class="badge ${r.status === 'Present' ? 'badge-success' : r.status === 'Absent' ? 'badge-danger' : 'badge-secondary'}">${r.status}</span></td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('att-save-btn').addEventListener('click', saveAttendance);
  attachEmployeePicker('att-employee');
  attachLinkTypeahead(document.getElementById('att-location'), 'Location');
}

async function saveAttendance() {
  const errorEl = document.getElementById('att-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('att-employee').value;
  const date = document.getElementById('att-date').value;
  const location = document.getElementById('att-location').value.trim();
  const status = document.getElementById('att-status').value;

  if (!employeeId || !date) {
    errorEl.textContent = 'Employee and Date are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Attendance', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, date, location, status })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to save attendance.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function renderLeaveTab(container) {
  const res = await apiFetch('/api/v1/doc/Leave');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Apply Leave</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Leave Code', 'LV', '160px')}
${employeePickerField('leave-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-type">Leave Type</label>
        <select id="leave-type" class="form-input" style="width: 130px;">
          <option value="Casual">Casual</option>
          <option value="Sick">Sick</option>
          <option value="Earned">Earned</option>
          <option value="Unpaid">Unpaid</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-from">From Date</label>
        <input type="date" id="leave-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-to">To Date</label>
        <input type="date" id="leave-to" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-days">Days</label>
        <input type="number" id="leave-days" class="form-input" style="width: 90px;" min="1">
      </div>
      <button class="btn btn-primary" id="leave-save-btn">Apply</button>
    </div>
    <div id="leave-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Type</th><th>From</th><th>To</th><th>Days</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No leave applications yet. Use <b>Apply</b> above to record one on an employee's behalf.</td></tr>`
    : records.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.employee_id || ''}</td>
          <td>${r.leave_type || ''}</td>
          <td>${r.from_date || ''}</td>
          <td>${r.to_date || ''}</td>
          <td>${r.days ?? ''}</td>
          <td><span class="badge ${r.status === 'Approved' ? 'badge-success' : r.status === 'Rejected' ? 'badge-danger' : 'badge-warning'}">${r.status}</span></td>
          <td>${r.status === 'Applied' ? `
            <button class="action-btn" ${actionAttrs('decideLeave', [r.id, 'Approved'])}>Approve</button>
            <button class="action-btn action-btn-danger" ${actionAttrs('decideLeave', [r.id, 'Rejected'])}>Reject</button>
          ` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('leave-save-btn').addEventListener('click', saveLeave);
  attachEmployeePicker('leave-employee');
}

async function saveLeave() {
  const errorEl = document.getElementById('leave-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('leave-employee').value;
  const leaveType = document.getElementById('leave-type').value;
  const fromDate = document.getElementById('leave-from').value;
  const toDate = document.getElementById('leave-to').value;
  const days = parseFloat(document.getElementById('leave-days').value);

  if (!employeeId || !fromDate || !toDate || !days) {
    errorEl.textContent = 'Employee, From/To Date, and Days are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Leave', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, leave_type: leaveType, from_date: fromDate, to_date: toDate, days, status: 'Applied' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to apply leave.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function decideLeave(leaveId, decision) {
  // The generic doc endpoint replaces the whole document on update, not a
  // partial patch - fetch the current record first and resubmit it with
  // just status changed (same pattern required when editing an Approved
  // PurchaseOrder, Stage 13.8).
  const getRes = await apiFetch(`/api/v1/doc/Leave/${encodeURIComponent(leaveId)}`);
  if (!getRes) return;
  if (!getRes.ok) {
    await showApiError(getRes, 'Failed to load leave record.', 'Update Failed');
    return;
  }
  const leave = await getRes.json();
  leave.status = decision;

  const res = await apiFetch(`/api/v1/doc/Leave/${encodeURIComponent(leaveId)}`, {
    method: 'POST',
    body: JSON.stringify(leave)
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update leave status.', 'Update Failed');
    return;
  }
  renderView('hr');
}

function renderPayrollExportTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 20px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-from">From</label>
        <input type="date" id="payroll-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-to">To</label>
        <input type="date" id="payroll-to" class="form-input">
      </div>
      <button class="btn btn-primary" id="payroll-export-btn">Export</button>
    </div>
    <div id="payroll-export-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div id="payroll-export-results"></div>
  `;
  container.appendChild(panel);

  document.getElementById('payroll-export-btn').addEventListener('click', runPayrollExport);
}

async function runPayrollExport() {
  const errorEl = document.getElementById('payroll-export-error');
  const resultsEl = document.getElementById('payroll-export-results');
  errorEl.classList.add('hidden');

  const from = document.getElementById('payroll-from').value;
  const to = document.getElementById('payroll-to').value;
  if (!from || !to) {
    errorEl.textContent = 'Select both From and To dates.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch(`/api/v1/hr/payroll-export?from=${from}&to=${to}`);
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Export failed.';
    errorEl.classList.remove('hidden');
    return;
  }

  let html = `
    <table>
      <thead><tr><th>Employee</th><th>Present Days</th><th>Absent Days</th><th>Late Days</th><th>Approved Leave Days</th></tr></thead>
      <tbody>
  `;
  html += data.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No records in this period. Mark attendance for the month under the <b>Attendance</b> tab, then run the export again.</td></tr>`
    : data.map(e => `
        <tr>
          <td>${e.employee_id}</td>
          <td>${e.present_days}</td>
          <td>${e.absent_days}</td>
          <td>${e.late_days}</td>
          <td>${e.approved_leave_days}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  resultsEl.innerHTML = html;
}

// Stage 26.8.2/26.8.3: Payroll - Run Payroll (attendance + SalaryStructure +
// TDS + active loan deductions -> a Draft Payslip) and Post to GL (requires
// employee bank details on file). SalaryStructure itself is a Master
// doctype, managed under Setup like any other master - this tab is just the
// payroll *run*, not salary-structure maintenance.
async function renderPayrollTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Run Payroll</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">Salary structures are configured under Setup &rarr; Salary Structure.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
${employeePickerField('payroll-run-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-run-from">Period From</label>
        <input type="date" id="payroll-run-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-run-to">Period To</label>
        <input type="date" id="payroll-run-to" class="form-input">
      </div>
      <button class="btn btn-primary" id="payroll-run-btn">Run Payroll</button>
    </div>
    <div id="payroll-run-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const payslipsRes = await apiFetch('/api/v1/doc/Payslip');
  const payslips = payslipsRes && payslipsRes.ok ? await payslipsRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Payslip</th><th>Employee</th><th>Period</th><th>Gross</th><th>Net Pay</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += payslips.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No payslips yet. Use <b>Run Payroll</b> above &mdash; each employee needs a Salary Structure first.</td></tr>`
    : payslips.map(p => `
        <tr>
          <td style="font-family: monospace;">${p.code || p.id}</td>
          <td>${p.employee_id || ''}</td>
          <td>${p.period_from || ''} to ${p.period_to || ''}</td>
          <td>${p.gross_pay ?? ''}</td>
          <td>${p.net_pay ?? ''}</td>
          <td><span class="badge ${p.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
          <td>${p.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postPayslipToGL', [p.id])}>Post to GL</button>` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('payroll-run-btn').addEventListener('click', runPayrollForEmployee);
  attachEmployeePicker('payroll-run-employee');
}

async function runPayrollForEmployee() {
  const errorEl = document.getElementById('payroll-run-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('payroll-run-employee').value;
  const periodFrom = document.getElementById('payroll-run-from').value;
  const periodTo = document.getElementById('payroll-run-to').value;
  if (!employeeId || !periodFrom || !periodTo) {
    errorEl.textContent = 'Employee, Period From, and Period To are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/hr/run-payroll', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, period_from: periodFrom, period_to: periodTo })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to run payroll.', 'Payroll Run Failed');
    return;
  }
  renderView('hr');
}

async function postPayslipToGL(payslipId) {
  const confirmed = await showCustomConfirm('This will post the payslip amounts to the GL and mark it Posted. Continue?', 'Post Payslip');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/hr/post-payslip', {
    method: 'POST',
    body: JSON.stringify({ payslip_id: payslipId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post payslip.', 'Posting Failed');
    return;
  }
  renderView('hr');
}

// Stage 26.8.4: Loans/advances against salary. Custom create form + action
// list (not the generic doctype table) since Disburse needs real logic
// (GL posting + initializing outstanding_balance) beyond generic CRUD -
// same reasoning the Manufacturing screen's BOM/Production Order panels
// already established.
async function renderEmployeeLoansTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Loan/Advance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Loan Code', 'LOAN', '160px')}
${employeePickerField('loan-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="loan-principal">Principal Amount</label>
        <input type="number" id="loan-principal" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="loan-monthly">Monthly Deduction</label>
        <input type="number" id="loan-monthly" class="form-input" style="width: 140px;">
      </div>
      <button class="btn btn-primary" id="loan-create-btn">Create Loan</button>
    </div>
    <div id="loan-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const loansRes = await apiFetch('/api/v1/doc/EmployeeLoan');
  const loans = loansRes && loansRes.ok ? await loansRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Loan</th><th>Employee</th><th>Principal</th><th>Monthly Deduction</th><th>Outstanding</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += loans.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No loans yet. Use <b>Create Loan</b> above, then <b>Disburse</b> to post it to the ledger.</td></tr>`
    : loans.map(l => `
        <tr>
          <td style="font-family: monospace;">${l.code || l.id}</td>
          <td>${l.employee_id || ''}</td>
          <td>${l.principal_amount ?? ''}</td>
          <td>${l.monthly_deduction ?? ''}</td>
          <td>${l.outstanding_balance ?? ''}</td>
          <td><span class="badge ${l.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${l.status}</span></td>
          <td>${l.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('disburseEmployeeLoan', [l.id])}>Disburse</button>` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('loan-create-btn').addEventListener('click', createEmployeeLoan);
  attachEmployeePicker('loan-employee');
}

async function createEmployeeLoan() {
  // BLD-036: guard against a double-click creating two EmployeeLoans.
  await guardAgainstDoubleSubmit(document.getElementById('loan-create-btn'), 'Creating...', createEmployeeLoanInner);
}

async function createEmployeeLoanInner() {
  const errorEl = document.getElementById('loan-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('loan-employee').value;
  const principal = parseFloat(document.getElementById('loan-principal').value);
  const monthly = parseFloat(document.getElementById('loan-monthly').value);

  if (!employeeId || !principal || !monthly) {
    errorEl.textContent = 'Employee, Principal Amount, and Monthly Deduction are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/EmployeeLoan', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, principal_amount: principal, monthly_deduction: monthly, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create loan.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function disburseEmployeeLoan(loanId) {
  const confirmed = await showCustomConfirm('This will post the disbursement to the GL and activate the loan. Continue?', 'Disburse Loan');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/hr/disburse-loan', {
    method: 'POST',
    body: JSON.stringify({ loan_id: loanId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to disburse loan.', 'Disbursement Failed');
    return;
  }
  renderView('hr');
}

// Stage 26.8.5: Employee self-service - leave request + expense-claim
// submission from the employee's own login. Resolves the current user's
// own Employee record (GET /api/v1/hr/my-employee) so they don't need to
// know their own employee code, then reuses the existing generic doc-create
// endpoints for Leave/ExpenseClaim (the approval flow itself, Stage
// 13.13c, is untouched - this is only about self-initiated submission).
async function renderMyRequestsTab(container) {
  const empRes = await apiFetch('/api/v1/hr/my-employee');
  const empData = empRes && empRes.ok ? await empRes.json() : { employee: null };
  const employee = empData.employee;

  if (!employee) {
    const panel = document.createElement('div');
    panel.className = 'table-panel';
    panel.style.padding = '24px';
    panel.innerHTML = `<p style="color: var(--text-muted);">Your login is not linked to an Employee record, so there is nothing to self-service here. Ask a Super Admin to set the Employee master's "Linked ERP User ID" field.</p>`;
    container.appendChild(panel);
    return;
  }

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Request Leave</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">Employee: ${employee.code || employee.id} - ${employee.name || ''}</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Leave Code', 'LV', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-type">Leave Type</label>
        <select id="myreq-leave-type" class="form-input" style="width: 130px;">
          <option value="Casual">Casual</option>
          <option value="Sick">Sick</option>
          <option value="Earned">Earned</option>
          <option value="Unpaid">Unpaid</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-from">From Date</label>
        <input type="date" id="myreq-leave-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-to">To Date</label>
        <input type="date" id="myreq-leave-to" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-days">Days</label>
        <input type="number" id="myreq-leave-days" class="form-input" style="width: 80px;">
      </div>
      <button class="btn btn-primary" id="myreq-leave-btn">Submit Leave Request</button>
    </div>
    <div id="myreq-leave-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const expensePanel = document.createElement('div');
  expensePanel.className = 'table-panel';
  expensePanel.style.padding = '24px';
  expensePanel.style.marginBottom = '24px';
  expensePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Submit Expense Claim</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Claim Number', 'EXP', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-date">Expense Date</label>
        <input type="date" id="myreq-exp-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-category">Category</label>
        <select id="myreq-exp-category" class="form-input" style="width: 140px;">
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
        <label class="form-label" for="myreq-exp-amount">Amount</label>
        <input type="number" id="myreq-exp-amount" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-purpose">Purpose</label>
        <input type="text" id="myreq-exp-purpose" class="form-input" style="width: 180px;">
      </div>
      <button class="btn btn-primary" id="myreq-exp-btn">Submit Expense Claim</button>
    </div>
    <div id="myreq-exp-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(expensePanel);

  // Stage 26.8.10 (P2, go-ahead 2026-07-27): Grievance submission, same
  // self-service shape as Leave/Expense above - create Draft then submit
  // into the existing approval engine (HR/Admin per this doctype's
  // approval_rules row) rather than a new case-management workflow.
  const grievancePanel = document.createElement('div');
  grievancePanel.className = 'table-panel';
  grievancePanel.style.padding = '24px';
  grievancePanel.style.marginBottom = '24px';
  grievancePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Submit Grievance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Grievance Number', 'GRV', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-griev-category">Category</label>
        <select id="myreq-griev-category" class="form-input" style="width: 160px;">
          <option value="Harassment">Harassment</option>
          <option value="Compensation">Compensation</option>
          <option value="Workplace Safety">Workplace Safety</option>
          <option value="Discrimination">Discrimination</option>
          <option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0; flex:1; min-width:220px;">
        <label class="form-label" for="myreq-griev-desc">Description</label>
        <input type="text" id="myreq-griev-desc" class="form-input">
      </div>
      <button class="btn btn-primary" id="myreq-griev-btn">Submit Grievance</button>
    </div>
    <div id="myreq-griev-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(grievancePanel);

  const [leavesRes, expensesRes, grievancesRes] = await Promise.all([
    apiFetch('/api/v1/doc/Leave'),
    apiFetch('/api/v1/doc/ExpenseClaim'),
    apiFetch('/api/v1/doc/Grievance')
  ]);
  const myLeaves = (leavesRes && leavesRes.ok ? await leavesRes.json() : []).filter(l => l.employee_id === (employee.code || employee.id));
  const myExpenses = (expensesRes && expensesRes.ok ? await expensesRes.json() : []).filter(e => e.employee_id === (employee.code || employee.id));
  const myGrievances = (grievancesRes && grievancesRes.ok ? await grievancesRes.json() : []).filter(g => g.employee_id === (employee.code || employee.id));

  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  historyPanel.innerHTML = `
    <table>
      <thead><tr><th>Type</th><th>Detail</th><th>Status</th></tr></thead>
      <tbody>
        ${myLeaves.map(l => `<tr><td>Leave</td><td>${l.leave_type || ''} ${l.from_date || ''} to ${l.to_date || ''}</td><td><span class="badge badge-secondary">${l.status}</span></td></tr>`).join('')}
        ${myExpenses.map(e => `<tr><td>Expense</td><td>${e.category || ''} ${e.amount ?? ''}</td><td><span class="badge badge-secondary">${e.status}</span></td></tr>`).join('')}
        ${myGrievances.map(g => `<tr><td>Grievance</td><td>${g.category || ''} ${g.description || ''}</td><td><span class="badge badge-secondary">${g.status}</span></td></tr>`).join('')}
        ${myLeaves.length === 0 && myExpenses.length === 0 && myGrievances.length === 0 ? `<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No requests submitted yet. Use <b>Submit Leave Request</b>, <b>Submit Expense Claim</b> or <b>Submit Grievance</b> above.</td></tr>` : ''}
      </tbody>
    </table>
  `;
  container.appendChild(historyPanel);

  document.getElementById('myreq-leave-btn').addEventListener('click', () => submitMyLeaveRequest(employee));
  document.getElementById('myreq-exp-btn').addEventListener('click', () => submitMyExpenseClaim(employee));
  document.getElementById('myreq-griev-btn').addEventListener('click', () => submitMyGrievance(employee));
}

async function submitMyGrievance(employee) {
  // BLD-036: guard against a double-click creating two Grievance records.
  await guardAgainstDoubleSubmit(document.getElementById('myreq-griev-btn'), 'Submitting...', () => submitMyGrievanceInner(employee));
}

async function submitMyGrievanceInner(employee) {
  const errorEl = document.getElementById('myreq-griev-error');
  errorEl.classList.add('hidden');

  const category = document.getElementById('myreq-griev-category').value;
  const description = document.getElementById('myreq-griev-desc').value.trim();

  if (!description) {
    errorEl.textContent = 'Description is required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const createRes = await apiFetch('/api/v1/doc/Grievance', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employee.code || employee.id, category, description, status: 'Draft' })
  });
  if (!createRes) return;
  const createData = await createRes.json();
  if (!createRes.ok) {
    errorEl.textContent = createData.error || 'Failed to submit grievance.';
    errorEl.classList.remove('hidden');
    return;
  }
  // The grievance number is issued by the server (Stage 30.6), so the routing
  // step below has to use the id it came back with - there is no longer a
  // client-side value that is guaranteed to match the saved document.
  const submitRes = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'Grievance', document_id: createData.id })
  });
  if (!submitRes) return;
  if (!submitRes.ok) {
    errorEl.textContent = await getErrorMessage(submitRes, 'Grievance was saved but could not be routed for HR review.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function submitMyLeaveRequest(employee) {
  // BLD-036: guard against a double-click creating two Leave records.
  await guardAgainstDoubleSubmit(document.getElementById('myreq-leave-btn'), 'Submitting...', () => submitMyLeaveRequestInner(employee));
}

async function submitMyLeaveRequestInner(employee) {
  const errorEl = document.getElementById('myreq-leave-error');
  errorEl.classList.add('hidden');

  const leaveType = document.getElementById('myreq-leave-type').value;
  const fromDate = document.getElementById('myreq-leave-from').value;
  const toDate = document.getElementById('myreq-leave-to').value;
  const days = parseFloat(document.getElementById('myreq-leave-days').value);

  if (!fromDate || !toDate || !days) {
    errorEl.textContent = 'From Date, To Date, and Days are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Leave', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employee.code || employee.id, leave_type: leaveType,
      from_date: fromDate, to_date: toDate, days, status: 'Applied'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit leave request.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function submitMyExpenseClaim(employee) {
  // BLD-036: guard against a double-click creating two ExpenseClaim records.
  await guardAgainstDoubleSubmit(document.getElementById('myreq-exp-btn'), 'Submitting...', () => submitMyExpenseClaimInner(employee));
}

async function submitMyExpenseClaimInner(employee) {
  const errorEl = document.getElementById('myreq-exp-error');
  errorEl.classList.add('hidden');

  const expenseDate = document.getElementById('myreq-exp-date').value;
  const category = document.getElementById('myreq-exp-category').value;
  const amount = parseFloat(document.getElementById('myreq-exp-amount').value);
  const purpose = document.getElementById('myreq-exp-purpose').value.trim();

  if (!expenseDate || !amount) {
    errorEl.textContent = 'Expense Date and Amount are both required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ExpenseClaim', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employee.code || employee.id, location: employee.location || '',
      expense_date: expenseDate, category, amount, purpose, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit expense claim.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}


export { renderHRView, renderAttendanceTab, saveAttendance, renderLeaveTab, saveLeave, decideLeave, renderPayrollExportTab, runPayrollExport, renderPayrollTab, runPayrollForEmployee, postPayslipToGL, renderEmployeeLoansTab, createEmployeeLoan, createEmployeeLoanInner, disburseEmployeeLoan, renderMyRequestsTab, submitMyGrievance, submitMyGrievanceInner, submitMyLeaveRequest, submitMyLeaveRequestInner, submitMyExpenseClaim, submitMyExpenseClaimInner };
