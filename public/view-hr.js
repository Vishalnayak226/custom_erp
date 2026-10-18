// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
// Stage 69: each tab that records something is a list with + New, the form a
// page of its own (txn-screen.js, the standard ERP shape).
const { txnPagedSection, txnRestorePane, txnDone, txnPageOf } = await import('./txn-screen.js' + new URL(import.meta.url).search);

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
// Stage 55.14: the doctype each tab reads, for hiding what a role cannot open.
const HR_TAB_DOCTYPE = {
  attendance: 'Attendance', leave: 'Leave', roster: 'ShiftAssignment', payroll: 'Payslip',
  'payroll-export': 'Payslip', loans: 'EmployeeLoan', onboarding: 'OnboardingChecklist',
  appraisals: 'Appraisal', training: 'TrainingRecord', grievances: 'Grievance'
};
// Payroll runs, exports and loans are the hr.payroll capability's (the server
// refuses every one of their commands without it).
const HR_PAYROLL_TABS = ['payroll', 'payroll-export', 'loans'];
const hrCanRunPayroll = () => state.permissions.isAdmin || state.permissions.capabilities.has('hr.payroll');
const HR_TAB_GROUPS = [
  { label: 'My work', tabs: ['my-requests'] },
  { label: 'Team', tabs: ['attendance', 'leave', 'roster'] },
  { label: 'HR administration', tabs: ['payroll', 'payroll-export', 'loans', 'onboarding', 'appraisals', 'training', 'grievances'] }
];

async function renderHRView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">HR</h1>
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
  // Stage 55.14: three distinct paths - the employee's own requests, the
  // team a manager runs, and HR administration - and only the tabs this role
  // can actually read (no tab that opens on a permission error).
  const visible = HR_TABS.filter(t => (!HR_TAB_DOCTYPE[t.id] || canReadDoctype(HR_TAB_DOCTYPE[t.id]))
    && (!HR_PAYROLL_TABS.includes(t.id) || hrCanRunPayroll()));
  if (!visible.some(t => t.id === currentHRTab)) currentHRTab = (visible[0] || HR_TABS[HR_TABS.length - 1]).id;
  tabBar.style.alignItems = 'center';
  tabBar.innerHTML = HR_TAB_GROUPS.map(g => {
    const tabs = visible.filter(t => g.tabs.includes(t.id));
    if (tabs.length === 0) return '';
    return `<span style="font-size:12px;color:var(--text-muted);white-space:nowrap;margin-left:8px">${g.label}</span>` + tabs.map(t =>
      `<button class="btn ${t.id === currentHRTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-hr-tab="${t.id}">${t.label}</button>`).join('');
  }).join('');
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
    await renderOnboardingTab(container);
  } else if (currentHRTab === 'appraisals') {
    // 26.8.8 (P2, go-ahead 2026-07-27): AppraisalCycle (the KRA/KPI
    // template) is a Master, managed under Setup like every other master -
    // this tab is the per-employee Appraisal transactions against it.
    await renderAppraisalsTab(container);
  } else if (currentHRTab === 'training') {
    // TrainingProgram is a Master (managed under Setup); this tab is the
    // per-employee enrolment/completion records against it.
    await renderTrainingTab(container);
  } else if (currentHRTab === 'grievances') {
    await renderGrievancesTab(container);
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

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Date</th><th>Location</th><th>Status</th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No attendance records yet. Click <b>+ Mark Attendance</b> to record one.</td></tr>`
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
  container.appendChild(txnPagedSection({ id: 'hr-att', title: 'Attendance', page: 'Mark attendance', count: records.length,
    newLabel: 'Mark Attendance', form: formPanel, list: listPanel, heading: 'h2' }));
  txnRestorePane('hr-att');

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
  txnDone('hr-att');
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
    <div id="leave-info" class="page-subtitle" style="margin-top: 12px;"></div>
    <div id="leave-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Type</th><th>From</th><th>To</th><th>Days</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No leave applications yet. Click <b>+ Apply Leave</b> to record one on an employee's behalf.</td></tr>`
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
  container.appendChild(txnPagedSection({ id: 'hr-leave', title: 'Leave', page: 'Apply leave', count: records.length,
    newLabel: 'Apply Leave', form: formPanel, list: listPanel, heading: 'h2' }));
  txnRestorePane('hr-leave');

  document.getElementById('leave-save-btn').addEventListener('click', saveLeave);
  attachEmployeePicker('leave-employee');
  const refreshLeaveHelp = wireLeaveHelp({ employee: () => document.getElementById('leave-employee').value.trim(),
    typeId: 'leave-type', fromId: 'leave-from', toId: 'leave-to', daysId: 'leave-days', infoId: 'leave-info', records });
  document.getElementById('leave-employee').addEventListener('change', refreshLeaveHelp);
  document.getElementById('leave-employee').addEventListener('blur', refreshLeaveHelp);
}

// Stage 55.14: a leave form fills Days from the dates (calendar days,
// inclusive, counted on the dates themselves so no time zone can shift them),
// shows the employee's balance for the type from the server (scoped: an
// employee sees only their own) and warns of an overlap with leave already on
// the list, before the server refuses it.
function leaveDaysBetween(from, to) {
  const [y1, m1, d1] = from.split('-').map(Number);
  const [y2, m2, d2] = to.split('-').map(Number);
  if (!y1 || !y2) return 0;
  return Math.round((Date.UTC(y2, m2 - 1, d2) - Date.UTC(y1, m1 - 1, d1)) / 86400000) + 1;
}

function wireLeaveHelp({ employee, typeId, fromId, toId, daysId, infoId, records }) {
  const $ = id => document.getElementById(id);
  const info = $(infoId);
  if (!info) return;
  $(daysId).addEventListener('input', () => { $(daysId).dataset.auto = ''; });
  const refresh = async () => {
    const from = $(fromId).value, to = $(toId).value, emp = employee();
    const notes = [];
    if (from && to) {
      const days = leaveDaysBetween(from, to);
      if (days < 1) notes.push('To Date is before From Date.');
      else if (!$(daysId).value || $(daysId).dataset.auto) { $(daysId).value = days; $(daysId).dataset.auto = '1'; }
      const clash = (records || []).find(r => r.employee_id === emp && r.status !== 'Rejected' && r.status !== 'Cancelled'
        && r.from_date <= to && r.to_date >= from);
      if (clash) notes.push(`Overlaps ${clash.code || clash.id} (${clash.leave_type}, ${clash.from_date} to ${clash.to_date}, ${clash.status}) - change the dates.`);
    }
    if (emp) {
      const year = (from || localISODate(new Date())).slice(0, 4);
      const res = await apiFetch(`/api/v1/hr/leave-balance?employee_id=${encodeURIComponent(emp)}&year=${year}`);
      if (res && res.ok) {
        const { balances } = await res.json();
        const line = balances.find(b => b.leave_type === $(typeId).value);
        if (line && line.entitlement !== null) {
          notes.unshift(`${line.leave_type} ${year}: ${line.entitlement} days a year, ${line.approved} approved, ${line.pending} awaiting approval, ${line.remaining} left.`);
        } else if (line) {
          notes.unshift(`${line.leave_type} leave has no yearly limit.`);
        }
      }
    }
    info.textContent = notes.join(' ');
  };
  [typeId, fromId, toId].forEach(id => $(id).addEventListener('change', refresh));
  return refresh;
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
  txnDone('hr-leave');
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

  const payslipsRes = await apiFetch('/api/v1/doc/Payslip');
  const payslips = payslipsRes && payslipsRes.ok ? await payslipsRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Payslip</th><th>Employee</th><th>Period</th><th>Gross</th><th>Deductions</th><th>Net Pay</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  // Stage 55.14: the deductions between gross and net, itemised on hover.
  const dedParts = p => [['PF', p.pf_deduction], ['ESI', p.esi_deduction], ['PT', p.pt_deduction], ['TDS', p.tds_deduction], ['Loan', p.loan_deduction]]
    .filter(([, v]) => Number(v) > 0);
  html += payslips.length === 0
    ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No payslips yet. Click <b>+ Run Payroll</b> &mdash; each employee needs a Salary Structure first.</td></tr>`
    : payslips.map(p => `
        <tr>
          <td style="font-family: monospace;">${p.code || p.id}</td>
          <td>${p.employee_id || ''}</td>
          <td>${p.period_from || ''} to ${p.period_to || ''}</td>
          <td>${p.gross_pay ?? ''}</td>
          <td title="${dedParts(p).map(([k, v]) => `${k} ${v}`).join(', ') || 'none'}">${dedParts(p).reduce((s, [, v]) => s + Number(v), 0) || 0}${dedParts(p).length ? ` <small>(${dedParts(p).map(([k]) => k).join(', ')})</small>` : ''}</td>
          <td>${p.net_pay ?? ''}</td>
          <td><span class="badge ${p.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
          <td>${p.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postPayslipToGL', [p.id])}>Post to GL</button>` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(txnPagedSection({ id: 'hr-payroll', title: 'Payslips', page: 'Run payroll', count: payslips.length,
    newLabel: 'Run Payroll', form: formPanel, list: listPanel, heading: 'h2',
    extra: '<button class="btn btn-outline" id="payroll-reconcile-btn">Reconcile payroll</button> ' }));
  txnRestorePane('hr-payroll');

  document.getElementById('payroll-run-btn').addEventListener('click', runPayrollForEmployee);
  attachEmployeePicker('payroll-run-employee');
  // Stage 55.14: a run defaults to last month - the period payroll is
  // normally run for - written as plain dates, so no time zone can move it.
  const now = new Date();
  const lastMonthEnd = new Date(now.getFullYear(), now.getMonth(), 0);
  document.getElementById('payroll-run-from').value = localISODate(new Date(lastMonthEnd.getFullYear(), lastMonthEnd.getMonth(), 1));
  document.getElementById('payroll-run-to').value = localISODate(lastMonthEnd);
  // Stage 73.2.17: gross -> deductions -> net -> bank -> GL.
  document.getElementById('payroll-reconcile-btn').addEventListener('click', async () => {
    const reports = await loadViewModule('/view-reports.js?v=1');
    reports.execDashboardOpenReport('payroll-reconciliation');
  });
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
  txnDone('hr-payroll');
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
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No loans yet. Click <b>+ New Loan/Advance</b>, then <b>Disburse</b> it here to post it to the ledger.</td></tr>`
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
  container.appendChild(txnPagedSection({ id: 'hr-loans', title: 'Loans/Advances', page: 'New loan/advance', count: loans.length,
    newLabel: 'New Loan/Advance', form: formPanel, list: listPanel, heading: 'h2' }));
  txnRestorePane('hr-loans');

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
  txnDone('hr-loans');
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
    <div id="myreq-leave-info" class="page-subtitle" style="margin-top: 12px;"></div>
    <div id="myreq-leave-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

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
    <div id="myreq-griev-access" class="page-subtitle" style="margin-top: 12px;"></div>
    <div id="myreq-griev-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

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
        ${myGrievances.map(g => `<tr><td>Grievance</td><td>${g.category || ''} ${g.description || ''}</td><td><span class="badge badge-secondary">${hrEsc((HR_GRV_STATE[g.status] || [g.status])[0])}</span></td></tr>`).join('')}
        ${myLeaves.length === 0 && myExpenses.length === 0 && myGrievances.length === 0 ? `<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No requests submitted yet. Use <b>+ Request Leave</b>, <b>+ Expense Claim</b> or <b>+ Grievance</b>.</td></tr>` : ''}
      </tbody>
    </table>
  `;
  // Stage 69: one list of everything this employee asked for; each kind of
  // request is its own New page.
  container.appendChild(txnPagedSection({ id: 'hr-myreq', title: 'My Requests', heading: 'h2', list: historyPanel,
    count: myLeaves.length + myExpenses.length + myGrievances.length,
    forms: { leave: ['Request Leave', formPanel], expense: ['Expense Claim', expensePanel], grievance: ['Grievance', grievancePanel] } }));
  txnRestorePane('hr-myreq');

  document.getElementById('myreq-leave-btn').addEventListener('click', () => submitMyLeaveRequest(employee));
  // Stage 55.14: own balance and own overlaps only.
  wireLeaveHelp({ employee: () => employee.code || employee.id, typeId: 'myreq-leave-type', fromId: 'myreq-leave-from',
    toId: 'myreq-leave-to', daysId: 'myreq-leave-days', infoId: 'myreq-leave-info', records: myLeaves })?.();
  document.getElementById('myreq-exp-btn').addEventListener('click', () => submitMyExpenseClaim(employee));
  document.getElementById('myreq-griev-btn').addEventListener('click', () => submitMyGrievance(employee));
  // Stage 55.14: who can read a grievance, from the policy the server enforces.
  hrGetJSON('/api/v1/hr/grievance-access', null).then(access => {
    const el = document.getElementById('myreq-griev-access');
    if (el && access) el.textContent = hrGrievanceMineText(access);
  });
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
  txnDone('hr-myreq');
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
  txnDone('hr-myreq');
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
  txnDone('hr-myreq');
  renderView('hr');
}

// ---------------------------------------------------------------------------
// Stage 55.14: guided journeys - onboarding/offboarding, appraisal cycles,
// training and grievances - on each doctype's own fields (db/migrations_
// stage26_8_hr_payroll.sql, migrations_stage26_8_hr_process.sql) and the
// approval engine, in the list-first shape: the list, + New as a page of its
// own, and a record opening on its own page ("open", no + button). The server
// adds only what the generic API cannot give honestly (engines/hr_journeys.go):
// a step tick stamped with who and when, and who can read a grievance.
// ---------------------------------------------------------------------------
const hrEsc = s => escapeHTMLText(String(s ?? ''));
const hrBadge = (text, kind) => `<span class="badge badge-${kind}">${hrEsc(text)}</span>`;
const hrStepDone = s => !!s && (s.done === true || s.done === 'true' || s.done === 'yes');
const hrBar = 'display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;padding:12px 16px;';
const hrMuted = 'color:var(--text-muted);font-size:13px;';

async function hrGetJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

// The signed-in user (the token's claims; a hint only - the server decides).
function hrSessionUser() {
  try {
    const claims = (localStorage.getItem('erp_token') || '').split('.')[0];
    const p = new URLSearchParams(atob(claims.replace(/-/g, '+').replace(/_/g, '/')));
    return { id: p.get('id') || '', user: p.get('user') || '' };
  } catch (_) {
    return { id: '', user: localStorage.getItem('erp_username') || '' };
  }
}

async function hrEmployees() {
  // Stage 93.9: an employee's department is a Department record now - shown by its name.
  const [list, depts] = await Promise.all([
    canReadDoctype('Employee') ? hrGetJSON('/api/v1/doc/Employee?limit=1000', []) : [],
    canReadDoctype('Department') ? hrGetJSON('/api/v1/doc/Department', []) : [],
  ]);
  const byCode = new Map(list.map(e => [e.code || e.id, e]));
  const deptNames = new Map((depts || []).map(d => [d.id, d.name || d.id]));
  return { list, byCode, name: code => { const e = byCode.get(code); return e ? `${e.name || code} (${code})` : (code || ''); },
    dept: e => (e && e.department ? deptNames.get(e.department) || e.department : '') };
}

// A JSON list held in a Data field (the doctypes' convention). null = text
// that is not a list.
function hrParseList(raw) {
  if (Array.isArray(raw)) return raw;
  if (raw === undefined || raw === null || String(raw).trim() === '') return [];
  try { const v = JSON.parse(raw); return Array.isArray(v) ? v : null; } catch (_) { return null; }
}

function hrWhen(s) {
  if (!s) return '';
  const d = new Date(String(s).replace(' ', 'T'));
  return isNaN(d) ? String(s) : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function hrProgress(done, total) {
  const pct = total ? Math.round(done * 100 / total) : 0;
  return `<span style="display:inline-flex;align-items:center;gap:8px;"><span style="display:inline-block;width:90px;height:6px;border-radius:3px;background:var(--surface-subtle);overflow:hidden;"><span style="display:block;height:100%;width:${pct}%;background:${total && done === total ? 'var(--success-color)' : 'var(--primary-color)'};"></span></span><span style="font-size:12px;">${done} of ${total}</span></span>`;
}

function hrSelect(id, label, options, value, width = '180px') {
  return `<div class="form-group" style="margin-bottom:0;"><label class="form-label" for="${id}">${label}</label>
    <select id="${id}" class="form-input" style="width:${width};">${options.map(([v, l]) =>
      `<option value="${hrEsc(v)}"${v === value ? ' selected' : ''}>${hrEsc(l)}</option>`).join('')}</select></div>`;
}

function hrShowError(id, message) {
  const el = document.getElementById(id);
  if (!el) return;
  el.textContent = message;
  el.classList.toggle('hidden', !message);
}

// The generic document endpoint replaces the whole record on update, so a
// change is load -> mutate -> save, carrying the loaded version so a save over
// someone else's newer edit is refused rather than silently undoing it.
async function hrSaveDoc(doctype, id, mutate) {
  const url = `/api/v1/doc/${doctype}/${encodeURIComponent(id)}`;
  const res = await apiFetch(url);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'Could not load the record.', 'Not Saved'); return null; }
  const { version, ...doc } = await res.json();
  mutate(doc);
  doc.expected_version = version;
  const save = await apiFetch(url, { method: 'POST', body: JSON.stringify(doc) });
  if (!save) return null;
  if (!save.ok) { await showApiError(save, 'The change was not saved.', 'Not Saved'); return null; }
  return doc;
}

async function hrSubmitForReview(doctype, id) {
  const res = await apiFetch('/api/v1/approval/submit', { method: 'POST', body: JSON.stringify({ doctype, document_id: id }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'Saved, but it could not be sent for review.', 'Not Sent'); return false; }
  return true;
}

async function hrDecide(doctype, id, decision, comment) {
  const res = await apiFetch('/api/v1/approval/decide', { method: 'POST', body: JSON.stringify({ doctype, document_id: id, decision, comment }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'The decision was not recorded.', 'Not Recorded'); return false; }
  return true;
}

// Who decides a doctype's approval (its approval rule) and whether this
// session may - DecideApproval's own rule: Super Admin, or exactly that role.
async function hrReviewerRole(doctype) {
  const rules = await hrGetJSON('/api/v1/approval/rules', []);
  const rule = rules.filter(r => r.doctype === doctype).sort((a, b) => a.min_amount - b.min_amount)[0];
  return rule ? rule.required_role : '';
}
const hrMayDecide = reviewer => state.permissions.isAdmin || (!!reviewer && localStorage.getItem('erp_role') === reviewer);
const hrRoleName = role => (/^hr\s*\/\s*admin$/i.test(role || '') ? 'Super Admin' : role);

function hrHistory(log, showComments) {
  if (!log || log.length === 0) return '';
  const verb = { Submitted: 'Submitted', Approved: 'Approved', Rejected: 'Returned', Modified: 'Edited after approval' };
  return `<h3 style="font-size:14px;font-weight:700;margin:16px 0 8px;">History</h3><ul style="margin:0 0 0 20px;padding:0;${hrMuted}">${log.map(e =>
    `<li>${hrEsc(verb[e.action] || e.action)} by ${hrEsc(e.actor_user_id)} (${hrEsc(hrRoleName(e.actor_role))}) · ${hrEsc(hrWhen(e.created_at))}${showComments && e.comment ? ` - ${hrEsc(e.comment)}` : ''}</li>`).join('')}</ul>`;
}

// A paged section plus one more page, "open", for a single record.
function hrSection(opts) {
  const section = txnPagedSection(opts);
  const open = document.createElement('div');
  open.dataset.pane = 'open';
  open.className = 'hidden';
  section.appendChild(open);
  return { section, open };
}

// Re-rendering on a record's page (a refresh, a setup detour returning) needs
// the record: drawn if it is still known, else back to the list.
function hrRestore(id, drawOpen) {
  if (txnPageOf(id, 'list') === 'open' && !drawOpen()) {
    txnRestorePane(id);
    txnDone(id);
    return;
  }
  txnRestorePane(id);
}

// ----- Onboarding / offboarding ---------------------------------------------

let hrOnbOpenId = '';
const hrOnbFilter = { type: '', status: '' };

async function renderOnboardingTab(container) {
  const [records, emps] = await Promise.all([
    hrGetJSON('/api/v1/doc/OnboardingChecklist?sort=recent&limit=1000', []), hrEmployees()]);
  const canCreate = canCreateDoctype('OnboardingChecklist');
  const canUpdate = canUpdateDoctype('OnboardingChecklist');
  const progressOf = r => { const s = hrParseList(r.items); return s ? { done: s.filter(hrStepDone).length, total: s.length } : null; };

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  const drawList = () => {
    const rows = records.filter(r => (!hrOnbFilter.type || r.checklist_type === hrOnbFilter.type) && (!hrOnbFilter.status || r.status === hrOnbFilter.status));
    const empty = records.length === 0
      ? (canCreate ? 'No onboarding or offboarding checklists yet. Click <b>+ New checklist</b> to start one for an employee.' : 'No onboarding or offboarding checklist has been started for you.')
      : 'No checklist matches these filters.';
    listPanel.innerHTML = `<div style="${hrBar}">
        ${hrSelect('hr-onb-ftype', 'Type', [['', 'All'], ['Onboarding', 'Onboarding'], ['Offboarding', 'Offboarding']], hrOnbFilter.type, '150px')}
        ${hrSelect('hr-onb-fstatus', 'Status', [['', 'All'], ['In Progress', 'In Progress'], ['Completed', 'Completed']], hrOnbFilter.status, '150px')}
      </div>
      <table><thead><tr><th>Checklist</th><th>Employee</th><th>Type</th><th>Progress</th><th>Status</th><th>Updated</th><th></th></tr></thead><tbody>
      ${rows.length === 0 ? `<tr><td colspan="7" style="text-align:center;color:var(--text-muted);">${empty}</td></tr>` : rows.map(r => {
        const p = progressOf(r);
        return `<tr><td style="font-family:monospace;">${hrEsc(r.code || r.id)}</td><td>${hrEsc(emps.name(r.employee_id))}</td>
          <td>${hrEsc(r.checklist_type)}</td><td>${p ? hrProgress(p.done, p.total) : `<span style="${hrMuted}">steps not readable</span>`}</td>
          <td>${hrBadge(r.status, r.status === 'Completed' ? 'success' : 'warning')}</td><td>${hrEsc(String(r.updated_at || '').slice(0, 10))}</td>
          <td><button class="action-btn" data-hr-onb-open="${hrEsc(r.id)}">Open</button></td></tr>`;
      }).join('')}</tbody></table>`;
    listPanel.querySelector('#hr-onb-ftype').addEventListener('change', e => { hrOnbFilter.type = e.target.value; drawList(); });
    listPanel.querySelector('#hr-onb-fstatus').addEventListener('change', e => { hrOnbFilter.status = e.target.value; drawList(); });
    listPanel.querySelectorAll('[data-hr-onb-open]').forEach(b => b.addEventListener('click', () => {
      hrOnbOpenId = b.dataset.hrOnbOpen;
      if (drawOpen()) window.txnShowPane('hr-onb:open');
    }));
  };

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.innerHTML = `
    <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;">
      ${hrSelect('hr-onb-type', 'Type', [['Onboarding', 'Onboarding'], ['Offboarding', 'Offboarding']], 'Onboarding', '150px')}
${employeePickerField('hr-onb-employee', '220px')}
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-onb-code">Checklist Code</label>
        <input type="text" id="hr-onb-code" class="form-input" style="width:200px;" data-auto="1"></div>
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-onb-copy">Copy steps from</label>
        <select id="hr-onb-copy" class="form-input" style="width:280px;"></select></div>
    </div>
    <div style="margin-top:16px;"><span class="form-label">Steps</span>
      <ol id="hr-onb-steps" style="margin:4px 0 8px 20px;padding:0;"></ol>
      <button type="button" class="btn btn-outline btn-sm" id="hr-onb-addstep">+ Add step</button></div>
    <div style="margin-top:16px;"><button class="btn btn-primary" id="hr-onb-save">Create checklist</button></div>
    <div id="hr-onb-error" class="login-error hidden" style="margin-top:16px;"></div>`;

  const { section, open } = hrSection({ id: 'hr-onb', title: 'Onboarding / Offboarding', heading: 'h2', count: records.length, list: listPanel,
    forms: canCreate ? { new: ['New checklist', formPanel] } : {} });

  const drawOpen = () => {
    const r = records.find(x => x.id === hrOnbOpenId);
    if (!r) return false;
    const steps = hrParseList(r.items);
    const docs = hrParseList(r.documents) || [];
    const p = steps ? { done: steps.filter(hrStepDone).length, total: steps.length } : null;
    const docLink = d => {
      const url = String((d && d.url) || ''), name = (d && (d.name || d.url)) || '';
      return /^https?:\/\//i.test(url) ? `<a href="${hrEsc(url)}" target="_blank" rel="noopener noreferrer">${hrEsc(name)}</a>` : `${hrEsc(name)}${url && url !== name ? ` (${hrEsc(url)})` : ''}`;
    };
    open.innerHTML = `<div class="table-panel" style="padding:24px;">
      <h2 style="font-size:16px;font-weight:700;margin:0 0 12px;">${hrEsc(r.checklist_type)} · ${hrEsc(emps.name(r.employee_id))}</h2>
      <div style="display:flex;gap:16px;align-items:center;flex-wrap:wrap;margin-bottom:16px;">
        <span style="font-family:monospace;">${hrEsc(r.code || r.id)}</span>${hrBadge(r.status, r.status === 'Completed' ? 'success' : 'warning')}${p ? hrProgress(p.done, p.total) : ''}</div>
      ${steps ? `<ol style="margin:0 0 12px 20px;padding:0;">${steps.length === 0 ? `<li style="${hrMuted}">No steps yet.</li>` : steps.map((s, i) => `
        <li style="margin-bottom:8px;"><label style="display:inline-flex;gap:8px;align-items:center;">
          <input type="checkbox" data-hr-onb-tick="${i}"${hrStepDone(s) ? ' checked' : ''}${canUpdate ? '' : ' disabled'}>
          <span>${hrEsc(s && (s.task ?? s.name ?? ''))}</span></label>
          ${hrStepDone(s) ? `<span style="${hrMuted}margin-left:8px;">${s.done_by ? `Done by ${hrEsc(s.done_by)}${s.done_at ? ` · ${hrEsc(hrWhen(s.done_at))}` : ''}` : 'Done (who and when not recorded)'}</span>` : ''}</li>`).join('')}</ol>`
        : `<p style="${hrMuted}">These steps are not a step list:</p><pre style="white-space:pre-wrap;">${hrEsc(r.items)}</pre>
          ${canUpdate ? '<button class="btn btn-outline btn-sm" id="hr-onb-convert">Use as steps (one per line)</button>' : ''}`}
      ${steps && canUpdate ? `<div style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;margin-bottom:8px;">
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-onb-newstep">Add a step</label>
        <input type="text" id="hr-onb-newstep" class="form-input" autocomplete="off" style="width:360px;"></div>
        <button class="btn btn-outline" id="hr-onb-newstep-btn">Add step</button></div>` : ''}
      <h3 style="font-size:14px;font-weight:700;margin:16px 0 8px;">Documents</h3>
      ${docs.length ? `<ul style="margin:0 0 8px 20px;padding:0;">${docs.map(d => `<li>${docLink(d)}</li>`).join('')}</ul>` : `<p style="${hrMuted}">No documents attached.</p>`}
      ${canUpdate ? `<div style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;">
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-onb-docname">Document</label>
        <input type="text" id="hr-onb-docname" class="form-input" autocomplete="off" style="width:200px;"></div>
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-onb-docurl">Link (https://...)</label>
        <input type="text" id="hr-onb-docurl" class="form-input" autocomplete="off" style="width:300px;"></div>
        <button class="btn btn-outline" id="hr-onb-doc-btn">Add document</button></div>` : ''}
      <div id="hr-onb-open-error" class="login-error hidden" style="margin-top:12px;"></div></div>`;

    open.querySelectorAll('[data-hr-onb-tick]').forEach(box => box.addEventListener('change', async () => {
      const done = box.checked;
      box.disabled = true;
      const res = await apiFetch(`/api/v1/hr/onboarding/${encodeURIComponent(r.id)}/steps`, {
        method: 'POST', body: JSON.stringify({ index: Number(box.dataset.hrOnbTick), done }) });
      if (!res || !res.ok) {
        box.checked = !done;
        box.disabled = false;
        if (res) await showApiError(res, 'The step was not updated.', 'Step Not Saved');
        return;
      }
      const progress = await res.json();
      r.items = JSON.stringify(progress.items);
      r.status = progress.status;
      drawOpen();
      drawList();
    }));
    // Edits beyond a tick go through the document itself.
    const change = async (mutate, check) => {
      hrShowError('hr-onb-open-error', '');
      const problem = check && check();
      if (problem) { hrShowError('hr-onb-open-error', problem); return; }
      const doc = await hrSaveDoc('OnboardingChecklist', r.id, mutate);
      if (!doc) return;
      Object.assign(r, { items: doc.items, documents: doc.documents, status: doc.status });
      drawOpen();
      drawList();
    };
    const val = id => (document.getElementById(id) || {}).value?.trim() || '';
    open.querySelector('#hr-onb-newstep-btn')?.addEventListener('click', () => change(doc => {
      const s = hrParseList(doc.items) || [];
      s.push({ task: val('hr-onb-newstep'), done: false });
      doc.items = JSON.stringify(s);
      doc.status = 'In Progress';
    }, () => (val('hr-onb-newstep') ? '' : 'Type the step first.')));
    open.querySelector('#hr-onb-convert')?.addEventListener('click', () => change(doc => {
      const text = String(doc.items || '');
      const parts = (text.includes('\n') ? text.split(/\r?\n/) : text.split(',')).map(t => t.trim()).filter(Boolean);
      doc.items = JSON.stringify(parts.map(task => ({ task, done: false })));
      doc.status = 'In Progress';
    }));
    open.querySelector('#hr-onb-doc-btn')?.addEventListener('click', () => change(doc => {
      const d = hrParseList(doc.documents) || [];
      d.push({ name: val('hr-onb-docname'), url: val('hr-onb-docurl') });
      doc.documents = JSON.stringify(d);
    }, () => (!val('hr-onb-docname') ? 'Name the document.' : (val('hr-onb-docurl') && !/^https?:\/\//i.test(val('hr-onb-docurl')) ? 'A link starts with https:// or http://.' : ''))));
    return true;
  };

  drawList();
  container.appendChild(section);
  if (canCreate) wireOnboardingForm(records, emps);
  hrRestore('hr-onb', drawOpen);
}

function wireOnboardingForm(records, emps) {
  const $ = id => document.getElementById(id);
  const stepsEl = $('hr-onb-steps');
  const addStep = task => {
    const li = document.createElement('li');
    li.style.marginBottom = '6px';
    li.innerHTML = `<input type="text" class="form-input" data-hr-onb-step style="width:420px;" aria-label="Step">
      <button type="button" class="action-btn action-btn-danger" data-hr-onb-remove>Remove</button>`;
    li.querySelector('input').value = task || '';
    li.querySelector('[data-hr-onb-remove]').addEventListener('click', () => li.remove());
    stepsEl.appendChild(li);
  };
  const fillCopy = () => {
    const type = $('hr-onb-type').value;
    const options = records.filter(r => r.checklist_type === type && (hrParseList(r.items) || []).length > 0);
    $('hr-onb-copy').innerHTML = `<option value="">None - type the steps</option>` + options.map(r =>
      `<option value="${hrEsc(r.id)}">${hrEsc(r.code || r.id)} · ${hrEsc(emps.name(r.employee_id))} (${hrParseList(r.items).length} steps)</option>`).join('');
  };
  const suggestCode = () => {
    const code = $('hr-onb-code');
    const emp = $('hr-onb-employee').value.trim();
    if (code.dataset.auto && emp) code.value = `${$('hr-onb-type').value === 'Offboarding' ? 'OFF' : 'ONB'}-${emp}`;
  };
  addStep('');
  fillCopy();
  attachEmployeePicker('hr-onb-employee');
  $('hr-onb-addstep').addEventListener('click', () => addStep(''));
  $('hr-onb-type').addEventListener('change', () => { fillCopy(); suggestCode(); });
  $('hr-onb-employee').addEventListener('change', suggestCode);
  $('hr-onb-employee').addEventListener('blur', suggestCode);
  $('hr-onb-code').addEventListener('input', () => { $('hr-onb-code').dataset.auto = ''; });
  $('hr-onb-copy').addEventListener('change', () => {
    const from = records.find(r => r.id === $('hr-onb-copy').value);
    if (!from) return;
    stepsEl.innerHTML = '';
    hrParseList(from.items).forEach(s => addStep(s && (s.task ?? s.name ?? '')));
  });
  $('hr-onb-save').addEventListener('click', () => guardAgainstDoubleSubmit($('hr-onb-save'), 'Saving...', async () => {
    hrShowError('hr-onb-error', '');
    const type = $('hr-onb-type').value;
    const employee = $('hr-onb-employee').value.trim();
    const code = $('hr-onb-code').value.trim();
    const tasks = [...stepsEl.querySelectorAll('[data-hr-onb-step]')].map(i => i.value.trim()).filter(Boolean);
    if (!employee || !code || tasks.length === 0) {
      hrShowError('hr-onb-error', 'Choose the employee, give the checklist a code and add at least one step.');
      return;
    }
    const openOne = records.find(r => r.employee_id === employee && r.checklist_type === type && r.status !== 'Completed');
    if (openOne && !(await showCustomConfirm(`${employee} already has an open ${type.toLowerCase()} checklist (${openOne.code || openOne.id}). Start another one anyway?`, 'Checklist Already Open'))) return;
    const res = await apiFetch('/api/v1/doc/OnboardingChecklist', { method: 'POST', body: JSON.stringify({
      code, employee_id: employee, checklist_type: type, status: 'In Progress',
      items: JSON.stringify(tasks.map(task => ({ task, done: false }))), documents: '[]' }) });
    if (!res) return;
    if (!res.ok) { hrShowError('hr-onb-error', await getErrorMessage(res, 'The checklist was not created.')); return; }
    txnDone('hr-onb');
    renderView('hr');
  }));
}

// ----- Appraisals ---------------------------------------------------------

let hrAprCycleId = '';
let hrAprOpenId = '';
let hrAprFilter = '';
const HR_APR_STATE = {
  'Not started': ['Not started', 'secondary'], Draft: ['Draft', 'secondary'], 'Pending Approval': ['Submitted for review', 'warning'],
  Approved: ['Reviewed', 'success'], Rejected: ['Returned for changes', 'danger']
};

async function renderAppraisalsTab(container) {
  // A role that reads only its own appraisals (Employee Self-Service) holds no
  // grant on the cycle master: its cycles are the ones its appraisals name.
  const canReadCycles = canReadDoctype('AppraisalCycle');
  const [cycleRows, mine, emps, reviewer] = await Promise.all([
    canReadCycles ? hrGetJSON('/api/v1/doc/AppraisalCycle?limit=1000', []) : [],
    canReadCycles ? null : hrGetJSON('/api/v1/doc/Appraisal?sort=recent&limit=1000', []), hrEmployees(), hrReviewerRole('Appraisal')]);
  const cycles = mine ? [...new Set(mine.map(a => a.cycle_id))].map(id => ({ id, code: id })) : cycleRows;
  cycles.sort((a, b) => String(b.period_start || '').localeCompare(String(a.period_start || '')));
  if (!cycles.some(c => c.id === hrAprCycleId)) hrAprCycleId = (cycles.find(c => c.status === 'Active') || cycles[0] || {}).id || '';
  const cycle = cycles.find(c => c.id === hrAprCycleId) || null;
  const appraisals = !cycle ? [] : mine ? mine.filter(a => a.cycle_id === cycle.id)
    : await hrGetJSON(`/api/v1/doc/Appraisal?cycle_id=${encodeURIComponent(cycle.id)}&sort=recent&limit=1000`, []);
  const cycleLabel = c => [c.code || c.id, c.period_start ? `${c.period_start} to ${c.period_end || '?'}` : '', c.status || ''].filter(Boolean).join(' · ');
  const kras = cycle ? (hrParseList(cycle.kra_template) || []) : [];
  const canCreate = canCreateDoctype('Appraisal');
  const ctx = { cycle, kras, emps, reviewer, canCreate, canUpdate: canUpdateDoctype('Appraisal'), canDecide: hrMayDecide(reviewer), appraisals };

  // One row per employee: every active employee in scope for a role that
  // starts appraisals; otherwise only the appraisals this role can read.
  const latest = new Map();
  appraisals.forEach(a => { if (!latest.has(a.employee_id)) latest.set(a.employee_id, a); });
  const roster = canCreate ? emps.list.filter(e => e.status !== 'Inactive' || latest.has(e.code || e.id)).map(e => e.code || e.id) : [];
  latest.forEach((a, code) => { if (!roster.includes(code)) roster.push(code); });
  const rows = roster.map(code => ({ code, emp: emps.byCode.get(code), a: latest.get(code) || null }));
  const stateOf = row => (row.a ? row.a.status : 'Not started');
  const counts = Object.keys(HR_APR_STATE).map(s => `${HR_APR_STATE[s][0]} ${rows.filter(r => stateOf(r) === s).length}`).join(' · ');

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  const shown = rows.filter(r => !hrAprFilter || stateOf(r) === hrAprFilter);
  listPanel.innerHTML = `<div style="${hrBar}">
      ${hrSelect('hr-apr-cycle', 'Appraisal cycle', cycles.length ? cycles.map(c => [c.id, cycleLabel(c)]) : [['', 'No cycles yet']], hrAprCycleId, '340px')}
      ${hrSelect('hr-apr-filter', 'Status', [['', 'All'], ...Object.keys(HR_APR_STATE).map(s => [s, HR_APR_STATE[s][0]])], hrAprFilter, '200px')}
      ${canCreateDoctype('AppraisalCycle') ? '<button type="button" class="btn btn-outline" id="hr-apr-cycles">Manage cycles</button>' : ''}
    </div>
    ${cycle ? `<div style="padding:0 16px 12px;${hrMuted}">${mine ? '' : `KRAs: ${kras.length ? kras.map(k => `${hrEsc(k.kra)} (${hrEsc(k.weight)})`).join(', ') : 'none set on this cycle'} · `}${counts}</div>` : ''}
    <table><thead><tr><th>Employee</th><th>Department</th><th>Status</th><th>Overall</th><th>Appraisal</th><th></th></tr></thead><tbody>
    ${!cycle ? `<tr><td colspan="6" style="text-align:center;color:var(--text-muted);">${mine ? 'No appraisal of yours has been started yet.' : canCreateDoctype('AppraisalCycle') ? 'No appraisal cycles yet. Use <b>Manage cycles</b> to set one up with its KRAs.' : 'No appraisal cycle has been set up yet.'}</td></tr>`
      : shown.length === 0 ? `<tr><td colspan="6" style="text-align:center;color:var(--text-muted);">${rows.length === 0 ? (canCreate ? 'No active employees in your scope.' : 'No appraisal of yours in this cycle yet.') : 'No employee matches this status.'}</td></tr>`
      : shown.map(r => {
        const [label, kind] = HR_APR_STATE[stateOf(r)] || [stateOf(r), 'secondary'];
        const action = r.a ? `<button class="action-btn" data-hr-apr-open="${hrEsc(r.a.id)}">Open</button>`
          : (canCreate ? `<button class="action-btn" data-hr-apr-start="${hrEsc(r.code)}">Start</button>` : '');
        return `<tr><td>${hrEsc(emps.name(r.code))}</td><td>${hrEsc(emps.dept(r.emp))}</td><td>${hrBadge(label, kind)}</td>
          <td>${hrEsc(r.a ? (r.a.overall_rating ?? '') : '')}</td><td style="font-family:monospace;">${hrEsc(r.a ? (r.a.code || '') : '')}</td><td>${action}</td></tr>`;
      }).join('')}</tbody></table>`;

  const newPanel = document.createElement('div');
  const { section, open } = hrSection({ id: 'hr-apr', title: 'Appraisals', heading: 'h2', count: appraisals.length, list: listPanel,
    forms: canCreate && cycle ? { new: ['Start appraisal', newPanel] } : {} });
  const drawOpen = () => {
    const a = appraisals.find(x => x.id === hrAprOpenId);
    if (!a) return false;
    drawAppraisalEditor(open, ctx, a, '');
    return true;
  };
  container.appendChild(section);
  if (canCreate && cycle) drawAppraisalEditor(newPanel, ctx, null, '');

  listPanel.querySelector('#hr-apr-cycle').addEventListener('change', e => { hrAprCycleId = e.target.value; renderView('hr'); });
  listPanel.querySelector('#hr-apr-filter').addEventListener('change', e => { hrAprFilter = e.target.value; renderView('hr'); });
  listPanel.querySelector('#hr-apr-cycles')?.addEventListener('click', () => window.openSetupDoctype('AppraisalCycle'));
  listPanel.querySelectorAll('[data-hr-apr-open]').forEach(b => b.addEventListener('click', () => {
    hrAprOpenId = b.dataset.hrAprOpen;
    if (drawOpen()) window.txnShowPane('hr-apr:open');
  }));
  listPanel.querySelectorAll('[data-hr-apr-start]').forEach(b => b.addEventListener('click', () => {
    drawAppraisalEditor(newPanel, ctx, null, b.dataset.hrAprStart);
    window.txnShowPane('hr-apr:new');
  }));
  hrRestore('hr-apr', drawOpen);
}

// One appraisal: KRA ratings (self and manager), overall rating, manager
// comments, and the action its status allows. a = null starts a new one
// for employee (or a picked one) with the cycle's KRAs.
async function drawAppraisalEditor(pane, ctx, a, employee) {
  const { cycle, kras, emps, reviewer, canCreate, canUpdate, canDecide } = ctx;
  const status = a ? a.status : 'Draft';
  const stored = a ? (hrParseList(a.ratings_json) || []) : [];
  const ratings = stored.length ? stored : kras.map(k => ({ kra: k.kra, weight: k.weight, self_rating: null, manager_rating: null }));
  const drafting = a ? (['Draft', 'Rejected'].includes(status) && canUpdate) : canCreate;
  const reviewing = !!a && status === 'Pending Approval' && canDecide && canUpdate;
  const log = a ? await hrGetJSON(`/api/v1/approval/log?doctype=Appraisal&document_id=${encodeURIComponent(a.id)}`, []) : [];
  const me = hrSessionUser();
  const submitted = [...log].reverse().find(e => e.action === 'Submitted');
  const ownSubmission = reviewing && submitted && (submitted.actor_user_id === me.id || submitted.actor_user_id === me.user);
  const returned = status === 'Rejected' ? [...log].reverse().find(e => e.action === 'Rejected') : null;
  const [label, kind] = HR_APR_STATE[status] || [status, 'secondary'];
  const num = (v, on, attr) => (on ? `<input type="number" class="form-input" style="width:90px;" min="1" max="5" step="0.5" ${attr} value="${v ?? ''}">` : hrEsc(v ?? ''));
  const reviewer1 = hrRoleName(reviewer) || 'Super Admin';
  pane.innerHTML = `<div class="table-panel" style="padding:24px;">
    <h2 style="font-size:16px;font-weight:700;margin:0 0 12px;">${a ? `Appraisal · ${hrEsc(emps.name(a.employee_id))}` : 'Start appraisal'}</h2>
    <div style="display:flex;gap:16px;align-items:center;flex-wrap:wrap;margin-bottom:16px;">
      <span>${hrEsc(cycle.code || cycle.id)}${cycle.period_start ? ` · ${hrEsc(cycle.period_start)} to ${hrEsc(cycle.period_end)}` : ''}</span>
      ${a ? `<span style="font-family:monospace;">${hrEsc(a.code || a.id)}</span>${hrBadge(label, kind)}` : ''}</div>
    ${a ? '' : `<div style="margin-bottom:16px;">${employeePickerField('hr-apr-employee', '240px')}</div>`}
    ${returned ? `<p style="color:var(--danger-color);font-size:13px;">Returned by ${hrEsc(returned.actor_user_id)}: ${hrEsc(returned.comment)}</p>` : ''}
    <table><thead><tr><th>KRA / KPI</th><th>Weight</th><th>Self rating (1-5)</th><th>Manager rating (1-5)</th></tr></thead><tbody>
    ${ratings.length === 0 ? `<tr><td colspan="4" style="text-align:center;color:var(--text-muted);">This cycle has no KRAs - add them to the cycle under Manage cycles.</td></tr>`
      : ratings.map((r, i) => `<tr><td>${hrEsc(r.kra)}</td><td>${hrEsc(r.weight ?? '')}</td>
        <td>${num(r.self_rating, drafting, `data-hr-apr-self="${i}"`)}</td><td>${num(r.manager_rating, drafting || reviewing, `data-hr-apr-mgr="${i}"`)}</td></tr>`).join('')}
    </tbody></table>
    <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;margin-top:16px;">
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-apr-overall">Overall Rating (1-5)</label>
        ${drafting || reviewing ? `<input type="number" id="hr-apr-overall" class="form-input" style="width:110px;" min="1" max="5" step="0.1" value="${a && a.overall_rating != null ? hrEsc(a.overall_rating) : ''}"${a && a.overall_rating != null ? '' : ' data-auto="1"'}>` : `<div>${hrEsc(a && a.overall_rating)}</div>`}</div>
      <div class="form-group" style="margin-bottom:0;flex:1;min-width:260px;"><label class="form-label" for="hr-apr-comments">Manager Comments</label>
        ${drafting || reviewing ? `<textarea id="hr-apr-comments" class="form-input" rows="2">${hrEsc(a && a.comments)}</textarea>` : `<div>${hrEsc(a && a.comments)}</div>`}</div>
    </div>
    <div id="hr-apr-weighted" style="${hrMuted}margin-top:8px;"></div>
    <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:16px;">
      ${drafting ? `<button class="btn btn-outline" data-hr-apr-act="draft">Save draft</button><button class="btn btn-primary" data-hr-apr-act="submit">Submit for review</button>` : ''}
      ${reviewing && !ownSubmission ? `<button class="btn btn-outline" data-hr-apr-act="save">Save review</button><button class="btn btn-primary" data-hr-apr-act="approve">Approve review</button><button class="btn btn-outline" data-hr-apr-act="return">Return for changes</button>` : ''}
    </div>
    ${a && status === 'Pending Approval' && (!reviewing || ownSubmission) ? `<p style="${hrMuted}margin-top:12px;">${ownSubmission ? `You submitted this appraisal, so another reviewer (${hrEsc(reviewer1)} or Super Admin) approves it.` : `Waiting for review by ${hrEsc(reviewer1)}.`}</p>` : ''}
    ${hrHistory(log, true)}
    <div id="hr-apr-error-${a ? 'open' : 'new'}" class="login-error hidden" style="margin-top:16px;"></div></div>`;

  const errId = `hr-apr-error-${a ? 'open' : 'new'}`;
  const field = sel => pane.querySelector(sel);
  if (!a) {
    attachEmployeePicker('hr-apr-employee');
    if (employee) field('#hr-apr-employee').value = employee;
  }
  // A suggestion only: the weighted average of the manager ratings.
  const weighted = () => {
    let w = 0, sum = 0;
    ratings.forEach((r, i) => {
      const v = parseFloat(field(`[data-hr-apr-mgr="${i}"]`)?.value ?? r.manager_rating);
      const wt = parseFloat(r.weight) || 0;
      if (!isNaN(v) && wt > 0) { w += wt; sum += v * wt; }
    });
    return w ? Math.round(sum / w * 10) / 10 : null;
  };
  const showWeighted = () => {
    const v = weighted();
    const hint = field('#hr-apr-weighted');
    if (hint) hint.textContent = v == null ? '' : `Weighted manager rating: ${v}`;
    const overall = field('#hr-apr-overall');
    if (overall && overall.dataset.auto && v != null) overall.value = v;
  };
  pane.querySelectorAll('[data-hr-apr-mgr]').forEach(i => i.addEventListener('input', showWeighted));
  field('#hr-apr-overall')?.addEventListener('input', e => { e.target.dataset.auto = ''; });
  showWeighted();

  const collect = () => {
    const rate = el => {
      if (!el) return undefined;
      const v = el.value.trim();
      if (v === '') return null;
      const n = Number(v);
      return n >= 1 && n <= 5 ? n : NaN;
    };
    const rows = ratings.map((r, i) => {
      const self = rate(field(`[data-hr-apr-self="${i}"]`));
      const mgr = rate(field(`[data-hr-apr-mgr="${i}"]`));
      return { ...r, self_rating: self === undefined ? (r.self_rating ?? null) : self, manager_rating: mgr === undefined ? (r.manager_rating ?? null) : mgr };
    });
    const overall = rate(field('#hr-apr-overall'));
    if (rows.some(r => Number.isNaN(r.self_rating) || Number.isNaN(r.manager_rating)) || Number.isNaN(overall)) return { error: 'Ratings are from 1 to 5.' };
    return { ratings_json: JSON.stringify(rows), overall_rating: overall === undefined ? (a ? a.overall_rating : null) : overall,
      comments: field('#hr-apr-comments') ? field('#hr-apr-comments').value.trim() : (a ? a.comments || '' : '') };
  };
  const apply = (doc, v) => {
    doc.ratings_json = v.ratings_json;
    doc.comments = v.comments;
    if (v.overall_rating == null) delete doc.overall_rating; else doc.overall_rating = v.overall_rating;
  };

  pane.querySelectorAll('[data-hr-apr-act]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
    hrShowError(errId, '');
    const act = btn.dataset.hrAprAct;
    const v = collect();
    if (v.error) { hrShowError(errId, v.error); return; }
    let id = a && a.id;
    if (!a) {
      const emp = field('#hr-apr-employee').value.trim();
      if (!emp) { hrShowError(errId, 'Choose the employee.'); return; }
      const existing = ctx.appraisals.find(x => x.employee_id === emp);
      if (existing) { hrShowError(errId, `${emp} already has an appraisal in this cycle (${existing.code || existing.id}) - open it from the list.`); return; }
      const doc = { code: `APR-${cycle.code || cycle.id}-${emp}`, cycle_id: cycle.id, employee_id: emp, status: 'Draft' };
      apply(doc, v);
      const res = await apiFetch('/api/v1/doc/Appraisal', { method: 'POST', body: JSON.stringify(doc) });
      if (!res) return;
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { hrShowError(errId, data.error || 'The appraisal was not saved.'); return; }
      id = data.id;
    } else {
      const saved = await hrSaveDoc('Appraisal', id, doc => { apply(doc, v); if (doc.status === 'Rejected') doc.status = 'Draft'; });
      if (!saved) return;
    }
    // A failed submit leaves a saved draft (its error already shown), so the
    // list is still the right place to land.
    if (act === 'submit') await hrSubmitForReview('Appraisal', id);
    if (act === 'approve' && !(await hrDecide('Appraisal', id, 'Approved', 'Review approved'))) return;
    if (act === 'return') {
      const reason = await showCustomPrompt('What needs to change before it is approved?', '', 'Return Appraisal');
      if (!reason || !reason.trim()) return;
      if (!(await hrDecide('Appraisal', id, 'Rejected', reason.trim()))) return;
    }
    txnDone('hr-apr');
    renderView('hr');
  })));
}

// ----- Training -----------------------------------------------------------

let hrTrnProgramId = '';
let hrTrnFilter = '';
let hrTrnResultId = '';
const HR_TRN_STATE = { 'Not enrolled': ['Not enrolled', 'secondary'], Enrolled: ['Enrolled - due', 'warning'], Completed: ['Completed', 'success'], Failed: ['Failed', 'danger'] };

async function renderTrainingTab(container) {
  // As with appraisal cycles: a role reading only its own records holds no
  // grant on the program master, so its programs are the ones it is in.
  const canReadPrograms = canReadDoctype('TrainingProgram');
  const [programRows, mine, emps] = await Promise.all([
    canReadPrograms ? hrGetJSON('/api/v1/doc/TrainingProgram?limit=1000', []) : [],
    canReadPrograms ? null : hrGetJSON('/api/v1/doc/TrainingRecord?sort=recent&limit=1000', []), hrEmployees()]);
  const programs = mine ? [...new Set(mine.map(r => r.program_id))].map(id => ({ id, code: id })) : programRows;
  programs.sort((a, b) => (a.status === 'Inactive') - (b.status === 'Inactive') || String(a.code || a.id).localeCompare(String(b.code || b.id)));
  if (!programs.some(p => p.id === hrTrnProgramId)) hrTrnProgramId = (programs[0] || {}).id || '';
  const program = programs.find(p => p.id === hrTrnProgramId) || null;
  const records = !program ? [] : mine ? mine.filter(r => r.program_id === program.id)
    : await hrGetJSON(`/api/v1/doc/TrainingRecord?program_id=${encodeURIComponent(program.id)}&sort=recent&limit=1000`, []);
  const canCreate = canCreateDoctype('TrainingRecord');
  const canUpdate = canUpdateDoctype('TrainingRecord');
  const latest = new Map();
  records.forEach(r => { if (!latest.has(r.employee_id)) latest.set(r.employee_id, r); });
  const roster = canCreate ? emps.list.filter(e => e.status !== 'Inactive' || latest.has(e.code || e.id)).map(e => e.code || e.id) : [];
  latest.forEach((r, code) => { if (!roster.includes(code)) roster.push(code); });
  const rows = roster.map(code => ({ code, emp: emps.byCode.get(code), r: latest.get(code) || null }));
  const stateOf = row => (row.r ? row.r.status : 'Not enrolled');
  const shown = rows.filter(r => !hrTrnFilter || stateOf(r) === hrTrnFilter);
  const enrollable = row => canCreate && (!row.r || row.r.status === 'Failed');

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `<div style="${hrBar}">
      ${hrSelect('hr-trn-program', 'Training program', programs.length ? programs.map(p => [p.id, `${p.code || p.id}${p.category ? ` · ${p.category}` : ''}${p.status === 'Inactive' ? ' · Inactive' : ''}`]) : [['', 'No programs yet']], hrTrnProgramId, '300px')}
      ${hrSelect('hr-trn-filter', 'Status', [['', 'All'], ...Object.keys(HR_TRN_STATE).map(s => [s, HR_TRN_STATE[s][0]])], hrTrnFilter, '170px')}
      ${canCreateDoctype('TrainingProgram') ? '<button type="button" class="btn btn-outline" id="hr-trn-programs">Manage programs</button>' : ''}
      ${canCreate && program ? '<button type="button" class="btn btn-outline" id="hr-trn-bulk" disabled>Enrol selected</button>' : ''}
    </div>
    ${program ? `<div style="padding:0 16px 12px;${hrMuted}">${program.description ? `${hrEsc(program.description)} · ` : ''}${Object.keys(HR_TRN_STATE).map(s => `${HR_TRN_STATE[s][0]} ${rows.filter(r => stateOf(r) === s).length}`).join(' · ')}</div>` : ''}
    <table><thead><tr>${canCreate ? '<th style="width:32px;"></th>' : ''}<th>Employee</th><th>Department</th><th>Status</th><th>Completed on</th><th>Score</th><th></th></tr></thead><tbody>
    ${!program ? `<tr><td colspan="7" style="text-align:center;color:var(--text-muted);">${mine ? 'You are not enrolled in any training yet.' : canCreateDoctype('TrainingProgram') ? 'No training programs yet. Use <b>Manage programs</b> to add one.' : 'No training program has been set up yet.'}</td></tr>`
      : shown.length === 0 ? `<tr><td colspan="7" style="text-align:center;color:var(--text-muted);">${rows.length === 0 ? (canCreate ? 'No active employees in your scope.' : 'You are not enrolled in this program.') : 'No employee matches this status.'}</td></tr>`
      : shown.map(row => {
        const [label, kind] = HR_TRN_STATE[stateOf(row)] || [stateOf(row), 'secondary'];
        const action = !row.r ? (canCreate ? `<button class="action-btn" data-hr-trn-enrol="${hrEsc(row.code)}">Enrol</button>` : '')
          : row.r.status === 'Enrolled' ? (canUpdate ? `<button class="action-btn" data-hr-trn-result="${hrEsc(row.r.id)}">Record result</button>` : '')
          : row.r.status === 'Failed' && canCreate ? `<button class="action-btn" data-hr-trn-enrol="${hrEsc(row.code)}">Enrol again</button>` : '';
        return `<tr>${canCreate ? `<td>${enrollable(row) ? `<input type="checkbox" data-hr-trn-pick="${hrEsc(row.code)}" aria-label="Select ${hrEsc(row.code)}">` : ''}</td>` : ''}
          <td>${hrEsc(emps.name(row.code))}</td><td>${hrEsc(emps.dept(row.emp))}</td><td>${hrBadge(label, kind)}</td>
          <td>${hrEsc(row.r && row.r.status !== 'Enrolled' ? row.r.completion_date : '')}</td><td>${hrEsc(row.r ? (row.r.score ?? '') : '')}</td><td>${action}</td></tr>`;
      }).join('')}</tbody></table>`;

  const enrolPanel = document.createElement('div');
  enrolPanel.className = 'table-panel';
  enrolPanel.style.padding = '24px';
  enrolPanel.innerHTML = `<div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0;"><span class="form-label">Training program</span><div>${hrEsc(program ? program.code || program.id : '')}</div></div>
${employeePickerField('hr-trn-employee', '240px')}
      <button class="btn btn-primary" id="hr-trn-enrol-btn">Enrol</button></div>
    <div id="hr-trn-enrol-error" class="login-error hidden" style="margin-top:16px;"></div>`;

  const { section, open } = hrSection({ id: 'hr-trn', title: 'Training', heading: 'h2', count: records.length, list: listPanel,
    forms: canCreate && program ? { new: ['Enrol employee', enrolPanel] } : {} });

  const enrol = async codes => {
    const failed = [];
    for (const code of codes) {
      const n = records.filter(r => r.employee_id === code).length + 1;
      const base = `TRN-${program.code || program.id}-${code}`;
      const res = await apiFetch('/api/v1/doc/TrainingRecord', { method: 'POST', body: JSON.stringify({
        code: n > 1 ? `${base}-${n}` : base, employee_id: code, program_id: program.id, status: 'Enrolled' }) });
      if (!res || !res.ok) failed.push(`${code}: ${res ? await getErrorMessage(res, 'not enrolled') : 'not enrolled'}`);
    }
    if (failed.length) await showCustomAlert(`Not enrolled:\n${failed.join('\n')}`, 'Enrolment');
    return failed.length === 0;
  };

  // Record the result of one enrolment: its own page.
  const drawOpen = () => {
    const r = records.find(x => x.id === hrTrnResultId && x.status === 'Enrolled');
    if (!r) return false;
    open.innerHTML = `<div class="table-panel" style="padding:24px;">
      <h2 style="font-size:16px;font-weight:700;margin:0 0 12px;">Record result · ${hrEsc(emps.name(r.employee_id))}</h2>
      <div style="${hrMuted}margin-bottom:16px;">${hrEsc(program.code || program.id)} · <span style="font-family:monospace;">${hrEsc(r.code || r.id)}</span></div>
      <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;">
        ${hrSelect('hr-trn-outcome', 'Result', [['Completed', 'Completed'], ['Failed', 'Failed']], 'Completed', '150px')}
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-trn-date">Completion Date</label>
          <input type="date" id="hr-trn-date" class="form-input" value="${localISODate(new Date())}"></div>
        <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="hr-trn-score">Score (optional)</label>
          <input type="number" id="hr-trn-score" class="form-input" style="width:110px;"></div>
        <button class="btn btn-primary" id="hr-trn-save">Save result</button></div>
      <div id="hr-trn-result-error" class="login-error hidden" style="margin-top:16px;"></div></div>`;
    const btn = open.querySelector('#hr-trn-save');
    btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
      const date = open.querySelector('#hr-trn-date').value;
      const scoreText = open.querySelector('#hr-trn-score').value.trim();
      if (!date) { hrShowError('hr-trn-result-error', 'Enter the completion date.'); return; }
      if (scoreText !== '' && Number.isNaN(Number(scoreText))) { hrShowError('hr-trn-result-error', 'The score is a number.'); return; }
      const saved = await hrSaveDoc('TrainingRecord', r.id, doc => {
        doc.status = open.querySelector('#hr-trn-outcome').value;
        doc.completion_date = date;
        if (scoreText === '') delete doc.score; else doc.score = Number(scoreText);
      });
      if (!saved) return;
      txnDone('hr-trn');
      renderView('hr');
    }));
    return true;
  };

  container.appendChild(section);
  listPanel.querySelector('#hr-trn-program').addEventListener('change', e => { hrTrnProgramId = e.target.value; renderView('hr'); });
  listPanel.querySelector('#hr-trn-filter').addEventListener('change', e => { hrTrnFilter = e.target.value; renderView('hr'); });
  listPanel.querySelector('#hr-trn-programs')?.addEventListener('click', () => window.openSetupDoctype('TrainingProgram'));
  listPanel.querySelectorAll('[data-hr-trn-enrol]').forEach(b => b.addEventListener('click', () => guardAgainstDoubleSubmit(b, 'Enrolling...', async () => {
    await enrol([b.dataset.hrTrnEnrol]);
    renderView('hr');
  })));
  listPanel.querySelectorAll('[data-hr-trn-result]').forEach(b => b.addEventListener('click', () => {
    hrTrnResultId = b.dataset.hrTrnResult;
    if (drawOpen()) window.txnShowPane('hr-trn:open');
  }));
  const bulk = listPanel.querySelector('#hr-trn-bulk');
  const picks = () => [...listPanel.querySelectorAll('[data-hr-trn-pick]:checked')].map(c => c.dataset.hrTrnPick);
  listPanel.querySelectorAll('[data-hr-trn-pick]').forEach(c => c.addEventListener('change', () => {
    bulk.disabled = picks().length === 0;
    bulk.textContent = picks().length ? `Enrol selected (${picks().length})` : 'Enrol selected';
  }));
  bulk?.addEventListener('click', () => guardAgainstDoubleSubmit(bulk, 'Enrolling...', async () => {
    await enrol(picks());
    renderView('hr');
  }));
  if (canCreate && program) {
    attachEmployeePicker('hr-trn-employee');
    const btn = document.getElementById('hr-trn-enrol-btn');
    btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Enrolling...', async () => {
      const code = document.getElementById('hr-trn-employee').value.trim();
      if (!code) { hrShowError('hr-trn-enrol-error', 'Choose the employee.'); return; }
      const current = latest.get(code);
      if (current && current.status !== 'Failed') { hrShowError('hr-trn-enrol-error', `${code} is already ${current.status === 'Enrolled' ? 'enrolled in' : 'through'} this program.`); return; }
      if (!(await enrol([code]))) return;
      txnDone('hr-trn');
      renderView('hr');
    }));
  }
  hrRestore('hr-trn', drawOpen);
}

// ----- Grievances ---------------------------------------------------------

let hrGrvOpenId = '';
let hrGrvFilter = '';
const HR_GRV_STATE = {
  Draft: ['Not sent to HR', 'secondary'], 'Pending Approval': ['With HR', 'warning'],
  Approved: ['Resolved - actioned', 'success'], Rejected: ['Closed - no action', 'danger']
};

// Who can read a grievance, in words, from GET /api/v1/hr/grievance-access
// (engines/hr_journeys.go reads it off the policy the server enforces).
function hrGrievanceAccessText(a) {
  const lists = a.case_list_roles.length ? ` ${a.case_list_roles.join(', ')} also see${a.case_list_roles.length === 1 ? 's' : ''} every case's number, employee, category and status - not what it says.` : '';
  return `Who can read grievances: what was raised and how it was resolved - ${a.content_readers.join(', ')} only.${lists} Any other role sees only the employee's own cases, by status. Cases are decided by ${hrRoleName(a.reviewer_role) || 'Super Admin'}.`;
}
function hrGrievanceYouText(a) {
  const y = a.you;
  return `You (${hrRoleName(y.role)}): ${y.all_cases ? 'every case' : 'your own cases only'}, ${y.read_content ? 'including what they say' : 'not what they say'}${y.decide ? '; you decide cases' : ''}.`;
}
function hrGrievanceMineText(a) {
  const lists = a.case_list_roles.length ? ` ${a.case_list_roles.join(', ')} can see that it exists - its number, your name, category and status - but not what it says.` : '';
  return `Who can read it: what you write and how it is resolved - ${a.content_readers.join(', ')} only.${lists} You follow its status under My Requests${a.you.read_content ? '' : '; the text is not shown back to you'}.`;
}

async function renderGrievancesTab(container) {
  const [access, records, emps] = await Promise.all([
    hrGetJSON('/api/v1/hr/grievance-access', null), hrGetJSON('/api/v1/doc/Grievance?sort=recent&limit=1000', []), hrEmployees()]);
  const you = access ? access.you : { all_cases: false, read_content: false, write_notes: false, decide: false };
  const canFile = canCreateDoctype('Grievance') && you.all_cases;
  const shown = records.filter(r => !hrGrvFilter || r.status === hrGrvFilter);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `<div style="${hrBar}">
      ${hrSelect('hr-grv-filter', 'Status', [['', 'All'], ...Object.keys(HR_GRV_STATE).map(s => [s, HR_GRV_STATE[s][0]])], hrGrvFilter, '200px')}</div>
    ${access ? `<div id="hr-grv-access" style="padding:0 16px 12px;${hrMuted}">${hrEsc(hrGrievanceAccessText(access))}<br>${hrEsc(hrGrievanceYouText(access))}</div>` : ''}
    <table><thead><tr><th>Case</th><th>Employee</th><th>Category</th><th>Status</th><th>Updated</th><th></th></tr></thead><tbody>
    ${shown.length === 0 ? `<tr><td colspan="6" style="text-align:center;color:var(--text-muted);">${records.length === 0 ? (you.all_cases ? 'No grievances have been raised.' : 'You have raised no grievance. Raise one from My Requests.') : 'No case matches this status.'}</td></tr>`
      : shown.map(r => {
        const [label, kind] = HR_GRV_STATE[r.status] || [r.status, 'secondary'];
        return `<tr><td style="font-family:monospace;">${hrEsc(r.code || r.id)}</td><td>${hrEsc(emps.name(r.employee_id))}</td><td>${hrEsc(r.category)}</td>
          <td>${hrBadge(label, kind)}</td><td>${hrEsc(String(r.updated_at || '').slice(0, 10))}</td>
          <td><button class="action-btn" data-hr-grv-open="${hrEsc(r.id)}">Open</button></td></tr>`;
      }).join('')}</tbody></table>`;

  const newPanel = document.createElement('div');
  newPanel.className = 'table-panel';
  newPanel.style.padding = '24px';
  newPanel.innerHTML = `<div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;">
${employeePickerField('hr-grv-employee', '220px')}
      ${hrSelect('hr-grv-category', 'Category', ['Harassment', 'Compensation', 'Workplace Safety', 'Discrimination', 'Other'].map(c => [c, c]), 'Harassment', '180px')}</div>
    <div class="form-group" style="margin:16px 0 0;"><label class="form-label" for="hr-grv-desc">Description</label>
      <textarea id="hr-grv-desc" class="form-input" rows="4"></textarea></div>
    ${access ? `<div style="${hrMuted}margin-top:8px;">${hrEsc(hrGrievanceAccessText(access))}</div>` : ''}
    <div style="margin-top:16px;"><button class="btn btn-primary" id="hr-grv-save">Raise and send to HR</button></div>
    <div id="hr-grv-error" class="login-error hidden" style="margin-top:16px;"></div>`;

  const { section, open } = hrSection({ id: 'hr-grv', title: 'Grievances', heading: 'h2', count: records.length, list: listPanel,
    forms: canFile ? { new: ['Grievance', newPanel] } : {} });

  const drawOpen = async () => {
    const r = records.find(x => x.id === hrGrvOpenId);
    if (!r) return false;
    const [label, kind] = HR_GRV_STATE[r.status] || [r.status, 'secondary'];
    const deciding = r.status === 'Pending Approval' && you.decide;
    const hidden = `<span style="${hrMuted}">Hidden from your role - only ${hrEsc(access ? access.content_readers.join(', ') : 'Super Admin')} can read it.</span>`;
    open.innerHTML = `<div class="table-panel" style="padding:24px;">
      <h2 style="font-size:16px;font-weight:700;margin:0 0 12px;">Grievance ${hrEsc(r.code || r.id)}</h2>
      <div style="display:flex;gap:16px;align-items:center;flex-wrap:wrap;margin-bottom:16px;">${hrBadge(label, kind)}
        <span>${hrEsc(emps.name(r.employee_id))}</span><span>${hrEsc(r.category)}</span></div>
      <div class="form-group"><span class="form-label">Description</span>
        <div style="white-space:pre-wrap;">${you.read_content ? hrEsc(r.description) : hidden}</div></div>
      <div class="form-group"><label class="form-label" for="hr-grv-notes">Resolution Notes</label>
        ${deciding && you.write_notes ? `<textarea id="hr-grv-notes" class="form-input" rows="4">${hrEsc(r.resolution_notes)}</textarea>`
          : `<div style="white-space:pre-wrap;">${you.read_content ? (hrEsc(r.resolution_notes) || `<span style="${hrMuted}">None yet.</span>`) : hidden}</div>`}</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;">
        ${r.status === 'Draft' && canUpdateDoctype('Grievance') ? '<button class="btn btn-primary" data-hr-grv-act="send">Send to HR</button>' : ''}
        ${deciding ? '<button class="btn btn-primary" data-hr-grv-act="Approved">Resolve - actioned</button><button class="btn btn-outline" data-hr-grv-act="Rejected">Close - no action</button>' : ''}
      </div>
      ${r.status === 'Pending Approval' && !you.decide ? `<p style="${hrMuted}">With HR - decided by ${hrEsc(access ? hrRoleName(access.reviewer_role) || 'Super Admin' : 'Super Admin')}.</p>` : ''}
      <div id="hr-grv-history"></div>
      <div id="hr-grv-open-error" class="login-error hidden" style="margin-top:16px;"></div></div>`;
    hrGetJSON(`/api/v1/approval/log?doctype=Grievance&document_id=${encodeURIComponent(r.id)}`, []).then(log => {
      const el = open.querySelector('#hr-grv-history');
      if (el) el.innerHTML = hrHistory(log, you.read_content);
    });
    open.querySelectorAll('[data-hr-grv-act]').forEach(btn => btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
      hrShowError('hr-grv-open-error', '');
      const act = btn.dataset.hrGrvAct;
      if (act === 'send') {
        if (await hrSubmitForReview('Grievance', r.id)) renderView('hr');
        return;
      }
      const notesEl = open.querySelector('#hr-grv-notes');
      if (notesEl) {
        const notes = notesEl.value.trim();
        if (!notes) { hrShowError('hr-grv-open-error', 'Write the resolution notes first - what was found and what was done.'); return; }
        if (!(await hrSaveDoc('Grievance', r.id, doc => { doc.resolution_notes = notes; }))) return;
      }
      // The decision's comment goes to the approval history, which is not
      // field-protected - so it stays neutral and the notes stay on the case.
      const comment = act === 'Approved' ? 'Resolved - see the resolution notes on the case' : 'Closed without action - see the resolution notes on the case';
      if (!(await hrDecide('Grievance', r.id, act, comment))) return;
      renderView('hr');
    })));
    return true;
  };

  container.appendChild(section);
  listPanel.querySelector('#hr-grv-filter').addEventListener('change', e => { hrGrvFilter = e.target.value; renderView('hr'); });
  listPanel.querySelectorAll('[data-hr-grv-open]').forEach(b => b.addEventListener('click', async () => {
    hrGrvOpenId = b.dataset.hrGrvOpen;
    if (await drawOpen()) window.txnShowPane('hr-grv:open');
  }));
  if (canFile) {
    attachEmployeePicker('hr-grv-employee');
    const btn = document.getElementById('hr-grv-save');
    btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Saving...', async () => {
      hrShowError('hr-grv-error', '');
      const employee = document.getElementById('hr-grv-employee').value.trim();
      const description = document.getElementById('hr-grv-desc').value.trim();
      if (!employee || !description) { hrShowError('hr-grv-error', 'Choose the employee and describe the grievance.'); return; }
      const res = await apiFetch('/api/v1/doc/Grievance', { method: 'POST', body: JSON.stringify({
        employee_id: employee, category: document.getElementById('hr-grv-category').value, description, status: 'Draft' }) });
      if (!res) return;
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { hrShowError('hr-grv-error', data.error || 'The grievance was not saved.'); return; }
      await hrSubmitForReview('Grievance', data.id);
      txnDone('hr-grv');
      renderView('hr');
    }));
  }
  const onCase = txnPageOf('hr-grv', 'list') === 'open';
  if (onCase && !(await drawOpen())) { txnRestorePane('hr-grv'); txnDone('hr-grv'); return; }
  txnRestorePane('hr-grv');
}

export { renderHRView, renderAttendanceTab, saveAttendance, renderLeaveTab, saveLeave, decideLeave, renderPayrollExportTab, runPayrollExport, renderPayrollTab, runPayrollForEmployee, postPayslipToGL, renderEmployeeLoansTab, createEmployeeLoan, createEmployeeLoanInner, disburseEmployeeLoan, renderMyRequestsTab, submitMyGrievance, submitMyGrievanceInner, submitMyLeaveRequest, submitMyLeaveRequestInner, submitMyExpenseClaim, submitMyExpenseClaimInner };
