import { documentSearchLink } from './view-document-links.js';

// Stage 62/63: debit / credit note prints come from trade-docs.js.
const tradeDocsReady = import('./trade-docs.js' + new URL(import.meta.url).search);
// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnTabs, txnMoney, txnRestorePane, txnPagedSection, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentFinanceTab = 'work';
// 140.12.9: posting and settling are a finance role's (finance.post).
const financeCanPost = () => !!(state.permissions && (state.permissions.isAdmin || (state.permissions.capabilities && state.permissions.capabilities.has('finance.post'))));
// Stage 73.1.10: the period whose close board is open (null = the list).
let currentClosePeriodId = null;
const PERIOD_TASK_COUNT = 5;
const FINANCE_TABS = [
  { id: 'work', label: 'Work to review' },
  { id: 'trial-balance', label: 'Trial Balance' },
  { id: 'chart-of-accounts', label: 'Chart of Accounts' },
  { id: 'periods', label: 'Accounting Periods' }
];

// Stage 155.1: each tab is its own sidebar entry ("finance:<tab>"); a bare link opens the first.
const financeReload = () => { currentClosePeriodId = null; return renderView(`finance:${currentFinanceTab}`); };

async function renderFinanceView(container) {
  currentFinanceTab = FINANCE_TABS.some(t => t.id === currentViewTab) ? currentViewTab : 'work';
  setActiveMenu(currentFinanceTab === 'work' ? 'menu-finance' : `menu-finance-${currentFinanceTab}`);
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${FINANCE_TABS.find(t => t.id === currentFinanceTab).label}</h1>
    </div>
  `;
  container.appendChild(header);

  if (currentFinanceTab === 'work') {
    await renderFinanceWorkPanel(container);
    return;
  }

  if (currentFinanceTab === 'periods') {
    await renderAccountingPeriodsPanel(container);
    return;
  }

  if (currentFinanceTab === 'chart-of-accounts') {
    await renderChartOfAccountsPanel(container);
    return;
  }

  // As-of picker (29.7.4). Re-renders the whole view on change rather than
  // patching the table in place - same approach the tab bar above uses.
  const controls = document.createElement('div');
  controls.className = 'form-group';
  controls.style.cssText = 'display:flex; align-items:flex-end; gap:12px; margin-bottom:16px;';
  controls.innerHTML = `
    <div>
      <label class="form-label" for="tb-as-of">As Of Date<span class="required">*</span></label>
      <input type="date" id="tb-as-of" class="form-input s-54f91a" value="${financeTrialBalanceAsOf}">
    </div>
    <span class="s-c45720">
      Includes every GL posting up to and including this date.
    </span>
  `;
  container.appendChild(controls);
  document.getElementById('tb-as-of').addEventListener('change', (e) => {
    if (!e.target.value) return; // an emptied picker would 400; keep the last good date
    financeTrialBalanceAsOf = e.target.value;
    financeReload();
  });

  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(financeTrialBalanceAsOf)}`);
  if (!res) return;

  if (!res.ok) {
    // BLD-036: was a dead-end modal-then-nothing - Try Again re-renders the
    // whole view (tab bar, as-of picker and data all come back), same as the
    // already-fixed Approvals/Fulfillment/GRN pattern.
    renderErrorPanel(container, 'Failed to load trial balance.', () => financeReload());
    return;
  }

  const data = await res.json();
  const balances = data.balances || [];

  const summaryRow = document.createElement('div');
  summaryRow.className = 'dashboard-stats-row';
  summaryRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Total Debits</span>
      <span class="stat-val">${formatNumber(data.total_debits)}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Total Credits</span>
      <span class="stat-val">${formatNumber(data.total_credits)}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Ledger Status</span>
      <div class="s-0afcb6">
        <span class="pulse-dot ${data.balanced ? 'u-bg-green' : 'u-bg-red'}"></span>
        <span class="u-fs16 u-fw7 ${data.balanced ? 'u-green' : 'u-red2'}">${escapeHTMLText(data.status || '')}</span>
      </div>
    </div>
    <div class="stat-card">
      <span class="stat-label">As Of</span>
      <span class="stat-val">${escapeHTMLText(data.as_of || financeTrialBalanceAsOf)}</span>
    </div>
  `;
  container.appendChild(summaryRow);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Account Code</th>
          <th>Account Name</th>
          <th>Type</th>
          <th>Debit</th>
          <th>Credit</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (balances.length === 0) {
    html += `<tr><td colspan="5" class="s-306094">No GL postings on or before this date. Postings are created automatically when a sale, receipt or invoice is posted &mdash; try a later As Of date.</td></tr>`;
  }
  balances.forEach(b => {
    html += `
      <tr>
        <td class="s-355794">${escapeHTMLText(b.account_code)}</td>
        <td class="s-eed0f8">${escapeHTMLText(b.account_name)}</td>
        <td>${escapeHTMLText(b.account_type)}</td>
        <td>${formatNumber(b.debit)}</td>
        <td>${formatNumber(b.credit)}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

async function renderFinanceWorkPanel(container) {
  const cards = [
    { title: 'Payables', href: '#/view/vendor-invoices' },
    { title: 'Receivables', href: '#/view/sales-invoices' },
    { title: 'Payment proposals', href: '#/view/payment-proposals' },
    { title: 'Bank reconciliation', href: '#/view/bank-reconciliation' },
    { title: 'Who posted what', report: 'gl-posting-review' },
    { title: 'General journal', report: 'gl-journal' },
    { title: 'Period close', tab: 'periods' }
  ];
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  const heading = document.createElement('h2');
  heading.textContent = 'Finance work to review';
  panel.appendChild(heading);
  const list = document.createElement('div');
  list.style.cssText = 'display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px';
  cards.forEach(card => {
    const link = document.createElement('a');
    link.className = 'btn btn-outline';
    link.style.cssText = 'display:block;text-align:left;white-space:normal;min-height:92px';
    link.href = card.href || '#/view/finance';
    link.textContent = card.title;
    if (card.tab) link.addEventListener('click', event => {
      event.preventDefault();
      renderView(`finance:${card.tab}`);
    });
    if (card.report) link.addEventListener('click', async event => {
      event.preventDefault();
      try {
        const reports = await loadViewModule('/view-reports.js?v=1');
        reports.execDashboardOpenReport(card.report);
      } catch (error) {
        console.error('Could not open the GL source report', error);
        showToast('Could not open the GL source report. Try Reports → Report Catalog.', { variant: 'warning' });
      }
    });
    list.appendChild(link);
  });
  // Stage 148.23: set-off has no menu line; its two lists show here once it is set up.
  apiFetch('/api/v1/finance/set-off/suggestions?enabled_only=1').then(res => (res && res.ok ? res.json() : null)).then(out => {
    if (!out || !out.enabled) return;
    [['Party pairs', '#/setup/PartyPair'], ['Set-off vouchers', '#/setup/SetOffVoucher']].forEach(([title, href]) => {
      const link = document.createElement('a');
      link.className = 'btn btn-outline';
      link.style.cssText = 'display:block;text-align:left;white-space:normal;min-height:92px';
      link.href = href;
      link.textContent = title;
      list.appendChild(link);
    });
  }).catch(() => {});
  panel.appendChild(list);
  container.appendChild(panel);
}

// Chart of Accounts (Stage 29.9): reference list of the master gl_accounts
// records themselves (code/name/type) rather than a balance report - reuses
// the trial-balance endpoint since that query already LEFT JOINs every
// account row regardless of postings, so no new backend endpoint is needed.
async function renderChartOfAccountsPanel(container) {
  // Only the account rows (code/name/type) are used here, never the balances,
  // so the as_of the endpoint now requires (29.7.4) is just "today" - the
  // gl_accounts side of that LEFT JOIN is unaffected by it either way.
  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(todayISO())}`);
  if (!res) return;
  if (!res.ok) {
    // BLD-036: was a dead-end static message with no retry. Appends rather
    // than using renderErrorPanel, which would wipe the finance header/tab
    // bar the caller already put in `container` - this panel only ever owns
    // its own content within it, never the whole view.
    const errPanel = document.createElement('div');
    errPanel.className = 'table-panel';
    errPanel.style.padding = '24px';
    errPanel.textContent = 'Failed to load chart of accounts. ';
    const retryBtn = document.createElement('button');
    retryBtn.className = 'btn btn-outline btn-sm';
    retryBtn.textContent = 'Try Again';
    // Resolves the live parent at click time, not the `container` argument
    // closed over here - that argument is the off-screen scratch buffer
    // renderView builds each view in before moving its children into the
    // real #view-root (see renderView's own comment on this exact class of
    // bug), so reusing it directly would silently re-render into a detached,
    // already-removed node.
    retryBtn.addEventListener('click', () => {
      const parent = errPanel.parentElement;
      errPanel.remove();
      renderChartOfAccountsPanel(parent);
    });
    errPanel.appendChild(retryBtn);
    container.appendChild(errPanel);
    return;
  }
  const data = await res.json();
  const accounts = data.balances || [];

  // Stage 148.5: list, parents and Add/Change/Block requests live in a lazy module.
  const mod = await import('./view-finance-coa.js' + new URL(import.meta.url).search);
  mod.renderChartOfAccounts(container, accounts);
}

// Accounting Periods (Stage 20.34): this had no frontend at all before this
// stage even though Stage 17.4's create/list/close API has existed since
// then - a real pre-existing gap, same shape as the Transfers/Inventory/
// Users/Roles ones Stage 22 fixed. Create + list + close, plus the new
// read-only pre-close checklist (engines.GetPeriodCloseChecklist) surfaced
// before the user commits to closing a period.
async function renderAccountingPeriodsPanel(container) {
  // Stage 73.1.10: one period's close board replaces the list until Back.
  if (currentClosePeriodId) {
    await renderPeriodCloseBoard(container, currentClosePeriodId);
    return;
  }
  const res = await apiFetch('/api/v1/finance/periods');
  if (!res) return;
  const periods = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.innerHTML = `
    <div class="s-8a0cfa">
      <div class="form-group s-50435f">
        <label class="form-label" for="period-name">Period Name</label>
        <input type="text" id="period-name" class="form-input s-56cae7" placeholder="e.g. FY2026-Q3">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="period-start">Start Date</label>
        <input type="date" id="period-start" class="form-input">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="period-end">End Date</label>
        <input type="date" id="period-end" class="form-input">
      </div>
      <button class="btn btn-primary" id="period-create-btn">Create Period</button>
    </div>
    <div id="period-form-error" class="login-error hidden s-29b798"></div>
  `;

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Period Name</th><th>Start</th><th>End</th><th>Status</th><th>Close tasks</th><th>Closed By</th><th>Actions</th></tr></thead>
      <tbody>
        ${periods.length === 0
          ? `<tr><td colspan="7" class="s-306094">No accounting periods yet. Use <b>Create Period</b> above to open your first one; its close tasks are created with it.</td></tr>`
          : periods.map(p => `
            <tr>
              <td class="s-eed0f8">${escapeHTMLText(p.period_name)}</td>
              <td>${p.start_date}</td>
              <td>${p.end_date}</td>
              <td><span class="badge ${p.status === 'Open' ? 'badge-success' : p.status === 'On Hold' ? 'badge-warning' : 'badge-secondary'}">${p.status}</span>${p.close_round > 1 ? ` <span class="badge badge-warning">round ${p.close_round}</span>` : ''}</td>
              <td>${p.status !== 'Closed' ? `${PERIOD_TASK_COUNT - p.tasks_unsigned} of ${PERIOD_TASK_COUNT} signed` : 'Signed and closed'}</td>
              <td>${escapeHTMLText(p.closed_by || '')}</td>
              <td><button class="action-btn" data-close-board="${escapeHTMLText(p.id)}">${p.status !== 'Closed' ? 'Close tasks' : 'Close record / reopen'}</button>${p.status === 'Open' || p.status === 'On Hold' ? ` <button class="action-btn" data-period-hold="${escapeHTMLText(p.id)}" data-hold="${p.status === 'Open' ? 'hold' : 'release'}">${p.status === 'Open' ? 'Hold' : 'Release'}</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  // List first, New as its own page (Stage 69 shape).
  container.appendChild(txnPagedSection({ id: 'fin-periods', title: 'Accounting Periods', page: 'New accounting period',
    count: periods.length, newLabel: 'New Period', form: formPanel, list: panel, heading: 'h2' }));
  txnRestorePane('fin-periods');
  // Stage 148.25: hold a period for sales / release it; the GSTR-1 months list.
  panel.querySelectorAll('[data-period-hold]').forEach(btn => btn.addEventListener('click', async () => {
    const r = await apiFetch(`/api/v1/finance/periods/${encodeURIComponent(btn.dataset.periodHold)}/${btn.dataset.hold}`, { method: 'POST', body: '{}' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The period was not changed.'); return; }
    financeReload();
  }));
  const g1 = document.createElement('button');
  g1.className = 'btn btn-outline';
  g1.textContent = 'GSTR-1 months';
  g1.addEventListener('click', async () => { await loadViewModule('/view-gstr1-lock.js?v=1'); window.openGstr1Months?.(); });
  container.appendChild(g1);
  panel.querySelectorAll('[data-close-board]').forEach(btn => btn.addEventListener('click', () => {
    currentClosePeriodId = btn.getAttribute('data-close-board');
    financeReload();
  }));

  document.getElementById('period-create-btn').addEventListener('click', async () => {
    const errorEl = document.getElementById('period-form-error');
    errorEl.classList.add('hidden');
    const periodName = document.getElementById('period-name').value.trim();
    const startDate = document.getElementById('period-start').value;
    const endDate = document.getElementById('period-end').value;
    if (!periodName || !startDate || !endDate) {
      errorEl.textContent = 'Period name, start date, and end date are all required.';
      errorEl.classList.remove('hidden');
      return;
    }
    const createRes = await apiFetch('/api/v1/finance/periods', {
      method: 'POST',
      body: JSON.stringify({ period_name: periodName, start_date: startDate, end_date: endDate })
    });
    if (!createRes) return;
    if (!createRes.ok) {
      errorEl.textContent = await getErrorMessage(createRes, 'Failed to create period.');
      errorEl.classList.remove('hidden');
      return;
    }
    txnDone('fin-periods');
    financeReload();
  });
}

// Shows the pre-close checklist in a confirm dialog and, if the user
// proceeds, calls the existing close endpoint - the checklist itself never
// blocks closing (it's advisory, see engines.PeriodCloseChecklist's own
// comment), it just makes the consequences visible first.
async function showPeriodCloseChecklist(periodId) {
  const res = await apiFetch(`/api/v1/finance/periods/${periodId}/close-checklist`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load close checklist.');
    return;
  }
  const checklist = await res.json();
  const lines = checklist.checks.map(c => `${c.passed ? '✓' : '✗'} ${c.name} - ${c.detail}`).join('\n');
  const summary = checklist.ready_to_close
    ? 'All checks passed.'
    : 'One or more checks did not pass - review before closing.';
  const proceed = await showCustomConfirm(
    `${summary}\n\n${lines}\n\nEvery close task must be signed off first. Once closed, reopening needs an approved request. Close "${checklist.period_name}" now?`,
    'Period Close Checklist'
  );
  if (!proceed) return;
  if (await closePeriodAsking(periodId)) financeReload();
}

// 162.12: a period that is not ready closes only with a typed reason. The
// server says when one is needed (FIN-0507 with the failing checks); the
// reason is logged with the close and in the audit log.
async function closePeriodAsking(periodId) {
  const url = `/api/v1/finance/periods/${encodeURIComponent(periodId)}/close`;
  let res = await apiFetch(url, { method: 'POST' });
  if (!res) return false;
  if (!res.ok && res.status === 422) {
    const body = await res.clone().json().catch(() => ({}));
    if (body.code === 'FIN-0507') {
      const reason = await showCustomPrompt(`${body.detail || 'This period is not ready to close.'}\n\nReason`, '', 'Close anyway?');
      if (!reason) return false;
      res = await apiFetch(url, { method: 'POST', body: JSON.stringify({ reason }) });
      if (!res) return false;
    }
  }
  if (!res.ok) { await showApiError(res, 'The period could not be closed.'); return false; }
  return true;
}

// Stage 73.1.10 (D09): one period's close board - owned tasks with live
// checks and evidence, the gated Close, the reopen request and the history.
// Every rule is enforced by the server; this screen only shows who can do
// what next.
function financeLink(link) {
  if (!link) return '';
  if (link.startsWith('report:')) return `<a href="#/view/reports" data-open-report="${escapeHTMLText(link.slice(7))}">Open report</a>`;
  return `<a href="${escapeHTMLText(link)}">Open</a>`;
}

async function renderPeriodCloseBoard(container, periodId) {
  const res = await apiFetch(`/api/v1/finance/periods/${encodeURIComponent(periodId)}/close-board`);
  if (!res) return;
  if (!res.ok) {
    renderErrorPanel(container, await getErrorMessage(res, 'Failed to load the close tasks.'), () => financeReload());
    return;
  }
  const b = await res.json();
  const me = localStorage.getItem('erp_username') || '';
  const isOpen = b.status === 'Open';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  const signedCount = b.tasks.filter(t => t.status !== 'Open' && t.status !== '').length;
  panel.innerHTML = `
    <p><a href="#/view/finance" id="close-board-back">&larr; All accounting periods</a></p>
    <h2 class="s-d92a09">${escapeHTMLText(b.period_name)} <span class="badge ${isOpen ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(b.status)}</span> ${b.close_round > 1 ? `<span class="badge badge-warning">close round ${b.close_round}</span>` : ''}</h2>
    <p class="s-fcf62d">${escapeHTMLText(b.start_date)} to ${escapeHTMLText(b.end_date)}.
      ${b.closed_by ? `Closed by ${escapeHTMLText(b.closed_by)} at ${escapeHTMLText(b.closed_at)}.` : ''}
      ${b.reopened_by ? `Last reopened by ${escapeHTMLText(b.reopened_by)} at ${escapeHTMLText(b.reopened_at)}.` : ''}
      ${isOpen ? `${signedCount} of ${b.tasks.length} close tasks signed.` : ''}</p>
    <h3 class="s-700061">Close tasks</h3>
    <table>
      <thead><tr><th>Task</th><th>Owner</th><th>System check (live)</th><th>Sign-off</th><th>Actions</th></tr></thead>
      <tbody>${b.tasks.map(t => `
        <tr data-task="${escapeHTMLText(t.key)}">
          <td><b title="${escapeHTMLText(t.hint)}">${escapeHTMLText(t.title)}</b><br><small>${financeLink(t.link)}</small></td>
          <td>${isOpen && b.can_manage && t.status === 'Open'
            ? `<input class="form-input s-c6fffd" data-owner-input value="${escapeHTMLText(t.owner)}" placeholder="username" aria-label="Owner of ${escapeHTMLText(t.title)}"> <button class="btn btn-outline btn-sm" data-assign>Assign</button>`
            : escapeHTMLText(t.owner || 'Unassigned')}</td>
          <td><span class="badge ${t.check_passed ? 'badge-success' : 'badge-warning'}">${t.check_passed ? 'Passes' : 'Needs a look'}</span><br><small>${escapeHTMLText(t.check_detail)}</small></td>
          <td>${t.status === 'Open' || !t.status ? '<span class="badge badge-secondary">Not signed</span>'
            : `<span class="badge badge-success">${escapeHTMLText(t.status)}</span> by ${escapeHTMLText(t.signed_by)} at ${escapeHTMLText(t.signed_at)}<br>
               <small><b>Evidence:</b> ${escapeHTMLText(t.evidence)}${t.evidence_ref ? ` (ref ${escapeHTMLText(t.evidence_ref)})` : ''}</small><br>
               <small class="s-5872de">${escapeHTMLText(t.check_snapshot)}</small>`}</td>
          <td>${isOpen && (t.status === 'Open' || !t.status) && t.owner && t.owner === me ? '<button class="btn btn-primary btn-sm" data-sign>Sign off</button>' : ''}
              ${isOpen && t.status && t.status !== 'Open' && (t.owner === me || b.can_manage) ? '<button class="btn btn-outline btn-sm" data-undo>Undo sign-off</button>' : ''}
              ${isOpen && (t.status === 'Open' || !t.status) && t.owner && t.owner !== me ? `<small class="s-5872de">Waiting for ${escapeHTMLText(t.owner)}</small>` : ''}
              ${isOpen && !t.owner && !b.can_manage ? '<small class="s-5872de">An Accountant assigns an owner</small>' : ''}</td>
        </tr>
        <tr class="hidden" data-sign-form="${escapeHTMLText(t.key)}"><td colspan="5">
          <div class="s-aad644">
            <div class="form-group s-116966"><label class="form-label" for="sign-status-${escapeHTMLText(t.key)}">Result</label>
              <select class="form-select" id="sign-status-${escapeHTMLText(t.key)}" data-sign-status><option>Done</option><option>Not applicable</option></select></div>
            <div class="form-group s-817b9b"><label class="form-label" for="sign-evidence-${escapeHTMLText(t.key)}">Evidence - what you checked and what it showed</label>
              <textarea class="form-input" id="sign-evidence-${escapeHTMLText(t.key)}" rows="2" data-sign-evidence></textarea></div>
            <div class="form-group s-116966"><label class="form-label" for="sign-ref-${escapeHTMLText(t.key)}">Reference (optional)</label>
              <input class="form-input" id="sign-ref-${escapeHTMLText(t.key)}" data-sign-ref placeholder="statement no., report run, file"></div>
            <button class="btn btn-primary btn-sm" data-sign-submit>Sign off</button>
          </div></td></tr>`).join('')}
      </tbody>
    </table>
    <div id="close-board-actions" class="s-f63b87"></div>
    <h3 class="s-700061">Advisory checks</h3>
    <ul class="s-2c0577">${(b.checks || []).map(c => `<li>${c.passed ? '&#10003;' : '&#10007;'} ${escapeHTMLText(c.name)} - ${escapeHTMLText(c.detail)}</li>`).join('')}</ul>
    <h3 class="s-700061">History</h3>
    ${b.log.length === 0 ? '<p class="s-5872de">Nothing recorded yet.</p>' : `<table><thead><tr><th>When</th><th>Round</th><th>Event</th><th>Task</th><th>By</th><th>Detail</th></tr></thead><tbody>
      ${b.log.map(e => `<tr><td>${escapeHTMLText(e.created_at)}</td><td>${e.close_round}</td><td>${escapeHTMLText(e.event)}</td><td>${escapeHTMLText(e.task_key)}</td><td>${escapeHTMLText(e.actor)}</td><td>${escapeHTMLText(e.detail)}</td></tr>`).join('')}
    </tbody></table>`}
  `;
  container.appendChild(panel);
  panel.querySelector('#close-board-back').addEventListener('click', e => { e.preventDefault(); currentClosePeriodId = null; financeReload(); });
  panel.querySelectorAll('[data-open-report]').forEach(a => a.addEventListener('click', async e => {
    e.preventDefault();
    const reports = await loadViewModule('/view-reports.js?v=1');
    reports.execDashboardOpenReport(a.getAttribute('data-open-report'));
  }));

  const taskAction = async (key, action, body) => {
    const r = await apiFetch(`/api/v1/finance/periods/${encodeURIComponent(periodId)}/close-tasks/${encodeURIComponent(key)}/${action}`, { method: 'POST', body: JSON.stringify(body) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The close task could not be updated.'); return; }
    financeReload();
  };
  panel.querySelectorAll('tr[data-task]').forEach(row => {
    const key = row.getAttribute('data-task');
    row.querySelector('[data-assign]')?.addEventListener('click', () => taskAction(key, 'assign', { owner: row.querySelector('[data-owner-input]').value.trim() }));
    row.querySelector('[data-sign]')?.addEventListener('click', () => {
      const form = panel.querySelector(`tr[data-sign-form="${CSS.escape(key)}"]`);
      form.classList.toggle('hidden');
      form.querySelector('[data-sign-evidence]').focus();
    });
    row.querySelector('[data-undo]')?.addEventListener('click', async () => {
      const reason = await showCustomPrompt('Why is this sign-off being undone?', '', 'Undo sign-off');
      if (reason) taskAction(key, 'undo', { reason });
    });
  });
  panel.querySelectorAll('tr[data-sign-form]').forEach(form => {
    const key = form.getAttribute('data-sign-form');
    form.querySelector('[data-sign-submit]').addEventListener('click', () => taskAction(key, 'sign', {
      status: form.querySelector('[data-sign-status]').value,
      evidence: form.querySelector('[data-sign-evidence]').value,
      evidence_ref: form.querySelector('[data-sign-ref]').value
    }));
  });

  const actions = panel.querySelector('#close-board-actions');
  if (isOpen) {
    const close = document.createElement('button');
    close.className = 'btn btn-primary';
    close.textContent = 'Close period';
    close.disabled = !b.all_signed || !b.can_manage;
    actions.appendChild(close);
    const why = document.createElement('span');
    why.style.cssText = 'margin-left:12px;color:var(--text-muted)';
    why.textContent = !b.can_manage ? 'An Accountant or a Super Admin closes the period.'
      : (b.all_signed ? 'Every task is signed.' : `${b.unsigned} task(s) still to be signed.`);
    actions.appendChild(why);
    close.addEventListener('click', async () => {
      if (!await showCustomConfirm(`Close "${b.period_name}"? Postings dated inside it will be refused. Reopening needs a reason and a second person's approval.`, 'Close period')) return;
      if (!await closePeriodAsking(periodId)) return;
      showToast('Period closed.', { variant: 'success' });
      financeReload();
    });
  } else {
    const pending = b.reopen_requests.find(q => q.id === b.reopen_pending_id);
    if (pending && pending.status === 'Pending Approval') {
      actions.innerHTML = `<p>Reopen request <b>${escapeHTMLText(pending.id)}</b> by ${escapeHTMLText(pending.requested_by)} is waiting for a second person's decision in <a href="#/view/approvals">Approvals</a>. Reason: ${escapeHTMLText(pending.reason)}</p>`;
    } else if (pending && pending.status === 'Approved') {
      actions.innerHTML = `<p>Reopen request <b>${escapeHTMLText(pending.id)}</b> is approved but the period did not reopen. <button class="btn btn-primary btn-sm" id="reopen-apply">Reopen now</button></p>`;
      actions.querySelector('#reopen-apply').addEventListener('click', async () => {
        const r = await apiFetch(`/api/v1/finance/period-reopen/${encodeURIComponent(pending.id)}/apply`, { method: 'POST', body: '{}' });
        if (!r) return;
        if (!r.ok) { await showApiError(r, 'The approved reopen could not be applied.'); return; }
        financeReload();
      });
    } else if (b.can_manage) {
      actions.innerHTML = `
        <div class="form-group s-e2b01b"><label class="form-label" for="reopen-reason">Request a reopen - why, and what will be corrected</label>
          <textarea class="form-input" id="reopen-reason" rows="2"></textarea></div>
        <button class="btn btn-outline" id="reopen-request">Request reopen</button>`;
      actions.querySelector('#reopen-request').addEventListener('click', async () => {
        const r = await apiFetch(`/api/v1/finance/periods/${encodeURIComponent(periodId)}/reopen-request`, { method: 'POST', body: JSON.stringify({ reason: document.getElementById('reopen-reason').value }) });
        if (!r) return;
        if (!r.ok) { await showApiError(r, 'The reopen could not be requested.'); return; }
        showToast('Reopen requested - it now waits in Approvals.', { variant: 'success' });
        financeReload();
      });
    } else {
      actions.innerHTML = '<p class="s-5872de">This period is closed. An Accountant or a Super Admin can request a reopen.</p>';
    }
  }
}

// Vendor Invoices (Stage 20.27/20.28 prerequisite): VendorInvoice has
// existed since Stage 17.8 (3-way match + pay) with zero frontend of its
// own. Creation reuses the generic doctype-table's own New-record form
// (VendorInvoice's fields are already registered) rather than a parallel
// create form here - this view only adds the match/pay actions the generic
// CRUD screen can't express.
async function renderVendorInvoicesView(container) {
  let fromGRN = null;
  try {
    const raw = sessionStorage.getItem('stage55-invoice-source-grn');
    sessionStorage.removeItem('stage55-invoice-source-grn');
    if (raw) {
      const parsed = JSON.parse(raw);
      if (typeof parsed.grnId === 'string' && typeof parsed.poId === 'string') fromGRN = parsed;
    }
  } catch (_) { /* storage can be disabled or stale */ }
  const [invRes, sectionsRes] = await Promise.all([
    apiFetch('/api/v1/doc/VendorInvoice'),
    apiFetch('/api/v1/doc/TDSSection')
  ]);
  if (!invRes) return;

  const invoices = invRes.ok ? await invRes.json() : [];
  const sections = (sectionsRes && sectionsRes.ok) ? await sectionsRes.json() : [];
  window.__tdsSections = sections;

  const STATUS_BADGE = { Draft: 'badge-secondary', Matched: 'badge-success', MismatchHold: 'badge-danger', Paid: 'badge-success' };
  financeListScreen(container, {
    title: 'Vendor Invoice',
    hint: fromGRN ? `<b>From GRN ${escapeHTMLText(fromGRN.grnId)}</b>: confirm the vendor's invoice number, amount and financial year before matching.` : '',
    count: invoices.length,
    total: invoices.reduce((s, v) => s + (Number(v.invoice_amount) || 0), 0),
    newId: 'vi-new-btn',
    newLabel: '+ New Vendor Invoice',
    table: `
    <table>
      <thead><tr><th>Invoice #</th><th>Vendor</th><th>PO</th><th>GRN</th><th>Amount</th><th class="num">Outstanding</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="8" class="s-306094">No vendor invoices yet. Use <b>+ New Vendor Invoice</b> above &mdash; have the Purchase Order and Goods Receipt it should 3-way match against to hand.</td></tr>`
          : invoices.map(v => {
            // Stage 73 / 55.10: what is still owed, and the payments behind it.
            const history = settlementHistory(v, 'payments');
            const partPaid = v.status === 'Matched' && history.length > 0;
            return `
            <tr>
              <td class="s-355794">${escapeHTMLText(v.invoice_number || v.id)}</td>
              <td>${escapeHTMLText(v.vendor_id || '')}</td>
              <td>${documentSearchLink('PurchaseOrder', v.po_id)}</td>
              <td>${documentSearchLink('GRN', v.grn_id)}</td>
              <td>${formatNumber(v.invoice_amount)}</td>
              <td class="num">${v.status === 'Draft' || v.status === 'Cancelled' ? '&mdash;' : formatMoney(openBalance(v, 'invoice_amount', ['Matched', 'MismatchHold', 'Pending Approval', 'Approved']))}</td>
              <td><span class="badge ${partPaid ? 'badge-warning' : (STATUS_BADGE[v.status] || 'badge-secondary')}">${partPaid ? 'Part paid' : escapeHTMLText(v.status)}</span></td>
              <td>${renderVendorInvoiceActions(v)}${history.length > 0 ? ` <button class="action-btn" data-pay-hist-toggle="${escapeHTMLText(v.id)}">Payments (${history.length})</button>` : ''}</td>
            </tr>${history.length > 0 ? paymentHistoryRow(v.id, history, 8, 'PaymentVoucher') : ''}`;
          }).join('')}
      </tbody>
    </table>`
  });
  container.querySelectorAll('[data-vi-advance]').forEach(btn => btn.addEventListener('click', async () => { await loadViewModule('/view-vendor-advance.js?v=1'); window.applyAdvanceToBill?.(btn.getAttribute('data-vi-advance'), btn.dataset.vendor, btn.dataset.due, btn.dataset.po); }));
  container.querySelectorAll('[data-vi-pay]').forEach(btn => btn.addEventListener('click', () => payVendorInvoicePlain(btn.getAttribute('data-vi-pay'))));
  wirePaymentHistory(container);
  addStatementButton('vi-new-btn', 'VendorStatement', 'Vendor', 'vendor');
  addExpenseBillButton('vi-new-btn');
  document.getElementById('vi-new-btn').addEventListener('click', async () => {
    const defaults = {};
    if (fromGRN) {
      defaults.grn_id = fromGRN.grnId;
      defaults.po_id = fromGRN.poId;
      const poRes = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(fromGRN.poId)}`);
      if (poRes?.ok) {
        const po = await poRes.json();
        defaults.vendor_id = po.vendor_id || po.vendor || '';
      }
    }
    currentDoctype = 'VendorInvoice'; currentSearchQuery = ''; currentTablePage = 1;
    await renderView('doctype-table');
    if (fromGRN && currentView === 'doctype-table' && currentDoctype === 'VendorInvoice' && canCreateDoctype('VendorInvoice')) {
      await openDynamicModal(undefined, defaults);
    }
  });
}

function renderVendorInvoiceActions(v) {
  const id = v.id;
  if (v.status === 'Draft' || v.status === 'MismatchHold') {
    // Stage 26.3.5: engines.PayVendorInvoice's override path (Stage 24.11 -
    // pay a MismatchHold invoice anyway with a business-justified reason,
    // routed to approval) already existed server-side but had no UI action
    // calling it - only Match did. Draft has nothing to override yet
    // (3-way match hasn't run), so this only shows for MismatchHold.
    return `<button class="action-btn" ${actionAttrs('matchVendorInvoice', [id])}>Match</button>${v.status === 'MismatchHold' ? `<button class="action-btn s-46cec8" ${actionAttrs('overrideAndPayVendorInvoice', [id])}>Override &amp; Pay</button>` : ''}`;
  }
  if (v.status === 'Matched') {
    const sections = window.__tdsSections || [];
    const tdsOptions = sections.map(s => `<option value="${escapeHTMLText(s.section_code || s.id)}">${escapeHTMLText(s.section_code || s.id)} (${escapeHTMLText(s.rate_percent)}%)</option>`).join('');
    // Stage 73 / 55.10: TDS is withheld on a whole bill, so once a bill is
    // part-paid only plain Pay (of what is left) is offered.
    return `
      <button class="action-btn" data-vi-pay="${escapeHTMLText(id)}">Pay</button>
      <button class="action-btn s-46cec8" data-vi-advance="${escapeHTMLText(id)}" data-vendor="${escapeHTMLText(v.vendor_id || '')}" data-po="${escapeHTMLText(v.po_id || '')}" data-due="${escapeHTMLText(String(openBalance(v, 'invoice_amount', ['Matched'])))}">Advance</button>
      ${sections.length > 0 && settlementHistory(v, 'payments').length === 0 ? `
        <select id="tds-select-${id}" class="form-select s-9c5c7b">${tdsOptions}</select>
        <button class="action-btn" ${actionAttrs('payVendorInvoiceTDS', [id])}>Pay w/ TDS</button>
      ` : ''}
    `;
  }
  return '';
}

// FA-20261005-01/06: the invoice already names its PO and GRN, so Match no
// longer asks for them again, and a hold now says what did not agree.
async function matchVendorInvoice(invoiceId) {
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/match', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Match failed.');
    return;
  }
  const data = await res.json();
  const d = data.match_details || {};
  const fmt = (n) => formatMoney(n || 0);
  const lines = [
    `Invoice amount: ${fmt(d.invoice_amount)}`,
    `Accepted goods value (incl. tax): ${fmt(d.grn_value)}`,
    `PO total (incl. tax): ${fmt(d.po_amount)}${d.already_billed_po ? ` - already billed ${fmt(d.already_billed_po)}` : ''}`,
  ];
  if (data.matched) {
    await showCustomAlert(`Matched. The invoice can now be paid.\n\n${lines.join('\n')}`, 'Invoice Matched');
  } else {
    await showCustomAlert(`On hold - correct the invoice amount, or use Override & Pay with a reason.\n\n${(data.reasons || []).join('\n')}\n\n${lines.join('\n')}`, 'Invoice On Hold');
  }
  renderView('vendor-invoices');
}

// payVendorInvoicePlain (Stage 73 / 55.10): asks how much to pay via
// Cash/Bank, offering the whole outstanding balance - the same shape as
// settling a sales invoice. Any other figure is a part-payment.
async function payVendorInvoicePlain(invoiceId) {
  const docRes = await apiFetch(`/api/v1/doc/VendorInvoice/${encodeURIComponent(invoiceId)}`);
  if (!docRes) return;
  if (!docRes.ok) {
    await showApiError(docRes, 'Failed to load the invoice.');
    return;
  }
  const inv = await docRes.json();
  const outstanding = openBalance(inv, 'invoice_amount', ['Matched']);
  const currency = inv.currency ? ` ${inv.currency}` : '';
  const body = await askSettlementAmount(`Amount to pay the vendor via Cash/Bank. Outstanding: ${formatMoney(outstanding)}${currency}.`, outstanding, 'Pay Vendor Invoice');
  if (!body) return;
  body.invoice_id = invoiceId;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay', {
    method: 'POST',
    body: JSON.stringify(body)
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Payment failed.');
    return;
  }
  const out = await res.json();
  await showCustomAlert(out.status === 'Paid'
    ? `Paid ${formatMoney(out.amount_paid)}${currency}. The invoice is fully paid.`
    : `Paid ${formatMoney(out.amount_paid)}${currency}. Still outstanding: ${formatMoney(out.outstanding_amount)}${currency}.`, 'Payment Recorded');
  renderView('vendor-invoices');
}

// overrideAndPayVendorInvoice (Stage 26.3.5): the UI action for
// engines.PayVendorInvoice's pre-existing override path - a MismatchHold
// invoice can be paid anyway with a mandatory business reason, routed
// through the approval engine (VendorInvoice's own approval_rules) rather
// than paying immediately, same as VENDOR-0092's own message states.
async function overrideAndPayVendorInvoice(invoiceId) {
  const reason = await showCustomPrompt('This invoice failed 3-way match. Reason to pay anyway (routes to approval):', '', 'Override & Pay');
  if (!reason || !reason.trim()) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, override_reason: reason.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit payment override.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(data.status === 'pending_approval' ? 'Override submitted - routed for approval.' : 'Invoice paid.', 'Override & Pay');
  renderView('vendor-invoices');
}

async function payVendorInvoiceTDS(invoiceId) {
  const select = document.getElementById(`tds-select-${invoiceId}`);
  const tdsSection = select ? select.value : '';
  if (!tdsSection) return;
  const confirmed = await showCustomConfirm(`Pay this vendor invoice with TDS withheld under section ${tdsSection}?`, 'Confirm Payment');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay-with-tds', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, tds_section: tdsSection })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Payment failed.');
    return;
  }
  renderView('vendor-invoices');
}

// Payment Proposals (Stage 20.27): batches multiple Matched VendorInvoices
// into one payment run via engines.CreatePaymentProposal/ExecutePaymentProposal.
async function renderPaymentProposalsView(container) {
  const [invRes, propRes] = await Promise.all([
    apiFetch('/api/v1/doc/VendorInvoice'),
    apiFetch('/api/v1/doc/PaymentProposal')
  ]);
  if (!invRes || !propRes) return;

  const invoices = (invRes.ok ? await invRes.json() : []).filter(v => v.status === 'Matched');
  const proposals = propRes.ok ? await propRes.json() : [];

  const builderPanel = document.createElement('div');
  builderPanel.className = 'table-panel';
  builderPanel.style.padding = '24px';
  builderPanel.style.marginBottom = '24px';
  builderPanel.innerHTML = `
    ${invoices.length === 0 ? `<p class="s-5872de">No Matched vendor invoices available to batch.</p>` : `
      <table class="s-29b798">
        <thead><tr><th></th><th>Invoice #</th><th>Vendor</th><th>Outstanding</th></tr></thead>
        <tbody>
          ${invoices.map(v => `
            <tr>
              <td><input type="checkbox" class="pp-invoice-check" value="${v.id}"></td>
              <td class="s-355794">${v.invoice_number || v.id}</td>
              <td>${v.vendor_id || ''}</td>
              <td>${formatMoney(openBalance(v, 'invoice_amount', ['Matched']))}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
      <button class="btn btn-primary" id="pp-create-btn">Create Proposal from Selected</button>
    `}
    <div id="pp-form-error" class="login-error hidden s-4c0544"></div>
  `;

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `
    <table>
      <thead><tr><th>Proposal #</th><th>Invoices</th><th>Total Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${proposals.length === 0
          ? `<tr><td colspan="5" class="s-306094">No payment proposals yet. Click <b>+ New Proposal</b>, tick one or more Matched vendor invoices, then <b>Create Proposal from Selected</b>.</td></tr>`
          : proposals.map(p => {
            let ids = [];
            try { ids = JSON.parse(p.invoice_ids || '[]'); } catch (e) { /* leave empty */ }
            return `
              <tr>
                <td class="s-355794">${p.proposal_number || p.id}</td>
                <td>${ids.length} invoice(s)</td>
                <td>${formatNumber(p.total_amount)}</td>
                <td><span class="badge ${p.status === 'Executed' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
                <td>${p.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('executePaymentProposal', [p.id])}>Execute</button>` : ''}</td>
              </tr>
            `;
          }).join('')}
      </tbody>
    </table>
  `;
  // Stage 69: the proposals are the page; + New Proposal opens the invoice
  // picker as a page of its own (txn-screen.js, the standard ERP shape).
  container.appendChild(txnPagedSection({ id: 'pp-screen', title: 'Payment Proposals', page: 'New proposal from matched invoices',
    count: proposals.length, newLabel: 'New Proposal', form: builderPanel, list: listPanel }));
  txnRestorePane('pp-screen');

  const createBtn = document.getElementById('pp-create-btn');
  if (createBtn) createBtn.addEventListener('click', async () => {
    const errorEl = document.getElementById('pp-form-error');
    errorEl.classList.add('hidden');
    const selected = Array.from(document.querySelectorAll('.pp-invoice-check:checked')).map(c => c.value);
    if (selected.length === 0) {
      errorEl.textContent = 'Select at least one invoice.';
      errorEl.classList.remove('hidden');
      return;
    }
    const res = await apiFetch('/api/v1/finance/payment-proposal', {
      method: 'POST',
      body: JSON.stringify({ invoice_ids: selected })
    });
    if (!res) return;
    if (!res.ok) {
      errorEl.textContent = await getErrorMessage(res, 'Failed to create proposal.');
      errorEl.classList.remove('hidden');
      return;
    }
    txnDone('pp-screen');
    renderView('payment-proposals');
  });
}

async function executePaymentProposal(proposalId) {
  const confirmed = await showCustomConfirm('Execute this payment run? Every invoice in it will be paid via the standard vendor-invoice payment path.', 'Execute Payment Proposal');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/finance/payment-proposal/${proposalId}/execute`, { method: 'POST', commandKey: 'payment-proposal.execute' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Execution failed.');
    return;
  }
  const results = await res.json();
  const failed = (results.results || []).filter(r => !r.paid);
  if (failed.length > 0) {
    await showCustomAlert(`Proposal executed with ${failed.length} failure(s):\n${failed.map(f => `${f.invoice_id}: ${f.error}`).join('\n')}`, 'Partial Failure');
  }
  renderView('payment-proposals');
}

// Stage 73.2.16: the reconciliation workbench. Exact match (the original
// one-click pass), a rule pass (amount tolerance, date window, reference),
// manual match against candidates, the exception queue (owner + reason,
// aged) and payouts - a marketplace settlement batch, or a card / UPI payout
// cleared from 1101 / 1102 - matched to a bank credit. The server enforces
// every rule; a line in a closed period is read-only there.
const bankRecon = { account: '', from: '', to: '', filter: 'open' };

async function renderBankReconciliationView(container) {
  const res = await apiFetch('/api/v1/doc/BankAccount');
  if (!res) return;
  const accounts = res.ok ? await res.json() : [];
  if (!bankRecon.account && accounts.length === 1) bankRecon.account = accounts[0].id;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `<div class="page-title-section"><h1 class="page-title">Bank Reconciliation</h1></div>
    <div>${financeCanPost() ? `<button type="button" class="btn btn-outline" data-acq="file">Settlement file</button> <button type="button" class="btn btn-outline" data-acq="match">Match card / UPI</button> <button type="button" class="btn btn-outline" data-bank-files>Bank files</button>` : ''}</div>`;
  container.appendChild(header);
  // 140.12.12: the acquirer's daily card / UPI settlement file (imported on
  // its own list) matched to bank credits; only the exceptions come back.
  header.querySelector('[data-bank-files]')?.addEventListener('click', () => renderView('bank-files'));
  const acqFile = header.querySelector('[data-acq="file"]');
  if (acqFile) acqFile.addEventListener('click', () => { currentDoctype = 'AcquirerSettlement'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  const acqMatch = header.querySelector('[data-acq="match"]');
  if (acqMatch) acqMatch.addEventListener('click', async () => {
    acqMatch.disabled = true;
    const r = await apiFetch('/api/v1/finance/acquirer-settlements/match', { method: 'POST' });
    acqMatch.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'Matching failed.'); return; }
    const out = await r.json();
    const ex = (out.exceptions || []).map(e => `${e.settlement_date || ''} ${e.mode || ''} ${Number(e.net || 0).toFixed(2)}: ${e.reason}`).join('\n');
    await showCustomAlert(`${out.matched} payout(s) matched.${ex ? `\n\nNot matched:\n${ex}` : ''}`, 'Card / UPI settlements');
    if (out.matched) renderView(currentView);
  });

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div class="s-2ff289">
      <div class="form-group s-208691">
        <label class="form-label" for="bank-recon-account">Bank Account</label>
        <select id="bank-recon-account" class="form-select">
          <option value="">Select a bank account...</option>
          ${accounts.map(a => `<option value="${escapeHTMLText(a.id)}" ${a.id === bankRecon.account ? 'selected' : ''}>${escapeHTMLText(a.bank_name || '')} - ${escapeHTMLText(a.account_number || a.id)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group s-116966"><label class="form-label" for="bank-recon-from">From</label><input type="date" id="bank-recon-from" class="form-input" value="${escapeHTMLText(bankRecon.from)}"></div>
      <div class="form-group s-116966"><label class="form-label" for="bank-recon-to">To</label><input type="date" id="bank-recon-to" class="form-input" value="${escapeHTMLText(bankRecon.to)}"></div>
      <div class="form-group s-116966"><label class="form-label" for="bank-recon-filter">Show</label>
        <select id="bank-recon-filter" class="form-select">
          ${[['open', 'Open (unmatched + exceptions)'], ['Exception', 'Exception queue'], ['Matched', 'Matched'], ['Explained', 'Explained'], ['all', 'All lines']]
            .map(([v, l]) => `<option value="${v}" ${v === bankRecon.filter ? 'selected' : ''}>${l}</option>`).join('')}
        </select></div>
      <button class="btn btn-primary" id="bank-recon-btn">Exact match</button>
      <button class="btn btn-outline" id="bank-rule-toggle">Rule match</button>
      <button class="btn btn-outline" id="bank-recon-manage-accounts-btn">Manage Bank Accounts</button>
      <button class="btn btn-outline" id="bank-recon-manage-lines-btn">Statement Lines / Import CSV</button>
    </div>
    <div id="bank-rule-form" class="hidden s-2ff289">
      <div class="form-group s-116966"><label class="form-label" for="bank-rule-tolerance">Amount tolerance (Rs)</label><input type="number" id="bank-rule-tolerance" class="form-input s-465bfe" value="0" min="0" max="10000" step="0.01"></div>
      <div class="form-group s-116966"><label class="form-label" for="bank-rule-window">Date window (days)</label><input type="number" id="bank-rule-window" class="form-input s-db4cf1" value="3" min="0" max="31"></div>
      <label class="s-dd294b"><input type="checkbox" id="bank-rule-reference" checked> Use the statement reference</label>
      <button class="btn btn-primary" id="bank-rule-run">Run rule match</button>
    </div>
    ${accounts.length === 0 ? `<p class="s-5872de">No bank accounts yet - use "Manage Bank Accounts" to add one.</p>` : ''}
    <div id="bank-recon-result"></div>
    <div id="bank-recon-workbench"></div>
  `;
  container.appendChild(panel);

  const reload = () => renderView('bank-reconciliation');
  const readFilters = () => {
    bankRecon.account = document.getElementById('bank-recon-account').value;
    bankRecon.from = document.getElementById('bank-recon-from').value;
    bankRecon.to = document.getElementById('bank-recon-to').value;
    bankRecon.filter = document.getElementById('bank-recon-filter').value;
  };
  ['bank-recon-account', 'bank-recon-from', 'bank-recon-to', 'bank-recon-filter'].forEach(id =>
    document.getElementById(id).addEventListener('change', () => { readFilters(); reload(); }));
  document.getElementById('bank-recon-manage-accounts-btn').addEventListener('click', () => {
    currentDoctype = 'BankAccount'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-recon-manage-lines-btn').addEventListener('click', () => {
    currentDoctype = 'BankStatementLine'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-rule-toggle').addEventListener('click', () => {
    const f = document.getElementById('bank-rule-form');
    f.classList.toggle('hidden');
    f.style.display = f.classList.contains('hidden') ? 'none' : 'flex';
  });
  document.getElementById('bank-rule-form').style.display = 'none';
  const resultEl = document.getElementById('bank-recon-result');
  const needAccount = () => {
    if (bankRecon.account) return false;
    resultEl.innerHTML = `<p class="login-error">Select a bank account first.</p>`;
    return true;
  };
  document.getElementById('bank-recon-btn').addEventListener('click', async () => {
    if (needAccount()) return;
    const r = await apiFetch('/api/v1/finance/bank-reconcile', { method: 'POST', body: JSON.stringify({ bank_account: bankRecon.account }) });
    if (!r) return;
    if (!r.ok) { resultEl.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(r, 'Reconciliation failed.'))}</p>`; return; }
    const out = await r.json();
    showToast(`${out.matched} line(s) matched exactly; ${out.unmatched_statement_lines.length} still open.`, { variant: 'success' });
    reload();
  });
  document.getElementById('bank-rule-run').addEventListener('click', async () => {
    if (needAccount()) return;
    const r = await apiFetch('/api/v1/finance/bank-reconciliation/rule-match', { method: 'POST', body: JSON.stringify({
      bank_account: bankRecon.account,
      amount_tolerance: Number(document.getElementById('bank-rule-tolerance').value || 0),
      date_window_days: parseInt(document.getElementById('bank-rule-window').value || '0', 10),
      use_reference: document.getElementById('bank-rule-reference').checked }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Rule match failed.'); return; }
    const out = await r.json();
    showToast(`${out.matched} matched (${out.matched_with_difference} with a difference), ${out.ambiguous_lines.length} ambiguous, ${out.no_candidate_lines.length} with no candidate.`, { variant: 'success' });
    reload();
  });
  if (bankRecon.account) await renderBankWorkbench(document.getElementById('bank-recon-workbench'));
}

async function renderBankWorkbench(host) {
  const q = new URLSearchParams({ bank_account: bankRecon.account, from: bankRecon.from, to: bankRecon.to });
  const res = await apiFetch(`/api/v1/finance/bank-reconciliation/workbench?${q}`);
  if (!res) return;
  if (!res.ok) { host.innerHTML = `<p class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Failed to load the statement.'))}</p>`; return; }
  const wb = await res.json();
  bankRecon.from = bankRecon.from || wb.from;
  bankRecon.to = bankRecon.to || wb.to;
  const fromEl = document.getElementById('bank-recon-from');
  const toEl = document.getElementById('bank-recon-to');
  if (fromEl && !fromEl.value) fromEl.value = wb.from;
  if (toEl && !toEl.value) toEl.value = wb.to;
  const c = wb.counts || {};
  const lines = wb.lines.filter(l => bankRecon.filter === 'all' ? true
    : bankRecon.filter === 'open' ? (l.match_status === 'Unmatched' || l.match_status === 'Exception') : l.match_status === bankRecon.filter);
  const money = v => formatMoney(v || 0);
  host.innerHTML = `
    <div class="dashboard-stats-row s-2da9f5">
      <div class="stat-card"><span class="stat-label">Unmatched</span><span class="stat-val">${c.Unmatched || 0}</span></div>
      <div class="stat-card"><span class="stat-label">Exceptions</span><span class="stat-val">${c.Exception || 0}</span></div>
      <div class="stat-card"><span class="stat-label">Matched / Explained</span><span class="stat-val">${(c.Matched || 0)} / ${(c.Explained || 0)}</span></div>
      <div class="stat-card"><span class="stat-label">Book balance (${escapeHTMLText(wb.gl_account_code)}) at ${escapeHTMLText(wb.to)}</span><span class="stat-val">${money(wb.book_balance)}</span></div>
      <div class="stat-card"><span class="stat-label">Matched with a difference</span><span class="stat-val">${money(wb.open_differences)}</span></div>
    </div>
    <table>
      <thead><tr><th>Date</th><th>Description / reference</th><th>Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>${lines.length === 0 ? `<tr><td colspan="5" class="s-306094">No statement lines in this view. Import a statement with "Statement Lines / Import CSV", or widen the dates or the Show filter.</td></tr>` : lines.map(l => `
        <tr data-line="${escapeHTMLText(l.id)}" data-drcr="${escapeHTMLText(l.dr_cr)}" data-amount="${l.amount}">
          <td>${escapeHTMLText(l.txn_date)}</td>
          <td>${escapeHTMLText(l.description || '')}${l.reference_number ? `<br><small>Ref ${escapeHTMLText(l.reference_number)}</small>` : ''}</td>
          <td class="s-86abda">${l.dr_cr === 'Credit' ? 'In' : 'Out'} ${money(l.amount)}</td>
          <td><span class="badge ${l.match_status === 'Matched' ? 'badge-success' : l.match_status === 'Exception' ? 'badge-danger' : l.match_status === 'Explained' ? 'badge-secondary' : 'badge-warning'}">${escapeHTMLText(l.match_status)}</span>
            ${l.match_method ? `<br><small>${escapeHTMLText(l.match_method)} by ${escapeHTMLText(l.matched_by)}${l.match_difference ? `, difference ${money(l.match_difference)}` : ''}</small>` : ''}
            ${l.match_status === 'Exception' ? `<br><small>Owner ${escapeHTMLText(l.exception_owner)}, ${l.exception_age_days} day(s): ${escapeHTMLText(l.exception_reason)}</small>` : ''}
            ${l.resolution_note ? `<br><small>${escapeHTMLText(l.resolution_note)}</small>` : ''}
            ${(l.matched_postings || []).map(p => `<br><small>${documentSearchLink(p.document_type, p.document_id, `${getDoctypeLabel(p.document_type)} ${p.document_id}`)}</small>`).join('')}</td>
          <td>${l.match_status === 'Unmatched' || l.match_status === 'Exception' ? `
              <button class="action-btn" data-bank-find>Find match</button>
              ${l.dr_cr === 'Credit' ? '<button class="action-btn" data-bank-payout>Payout</button>' : ''}
              ${l.match_status === 'Unmatched' ? '<button class="action-btn" data-bank-exception>Raise exception</button>' : '<button class="action-btn" data-bank-explain>Explain</button>'}`
            : '<button class="action-btn" data-bank-unmatch>Unmatch</button>'}</td>
        </tr>
        <tr class="hidden" data-line-detail="${escapeHTMLText(l.id)}"><td colspan="5"></td></tr>`).join('')}
      </tbody>
    </table>
    <h3 class="s-9e9099">Unmatched book entries on ${escapeHTMLText(wb.gl_account_code)} (${wb.unmatched_book.length})</h3>
    ${wb.unmatched_book.length === 0 ? '<p class="s-5872de">None.</p>' : `<table><thead><tr><th>Posted</th><th>Document</th><th>Debit (in)</th><th>Credit (out)</th><th>Posted by</th></tr></thead><tbody>
      ${wb.unmatched_book.slice(0, 50).map(p => `<tr><td>${escapeHTMLText(p.date.slice(0, 16))}</td><td>${documentSearchLink(p.document_type, p.document_id, `${getDoctypeLabel(p.document_type)} ${p.document_id}`)}</td><td>${p.debit ? money(p.debit) : ''}</td><td>${p.credit ? money(p.credit) : ''}</td><td>${escapeHTMLText(p.posted_by || 'before attribution')}</td></tr>`).join('')}
    </tbody></table>`}
    <h3 class="s-9e9099">Payouts waiting for a bank line</h3>
    <p>Card clearing (1101): <b>${money(wb.card_clearing_balance)}</b> &middot; UPI clearing (1102): <b>${money(wb.upi_clearing_balance)}</b> &middot; Marketplace batches: <b>${wb.marketplace_payouts.length}</b></p>
    ${wb.marketplace_payouts.length === 0 ? '' : `<table><thead><tr><th>Settlement batch</th><th>Channel</th><th>Lines</th><th>Net payout</th><th>Settled</th></tr></thead><tbody>
      ${wb.marketplace_payouts.map(s => `<tr><td>${escapeHTMLText(s.settlement_batch_id)}</td><td>${escapeHTMLText(s.channel)}</td><td>${s.lines}</td><td>${money(s.net_payout)}</td><td>${escapeHTMLText(s.settlement_date)}</td></tr>`).join('')}
    </tbody></table>`}
  `;

  const lineAction = async (lineId, action, body, done) => {
    const r = await apiFetch(`/api/v1/finance/bank-reconciliation/lines/${encodeURIComponent(lineId)}/${action}`, { method: 'POST', body: JSON.stringify(body || {}) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The statement line could not be updated.'); return; }
    showToast(done, { variant: 'success' });
    renderView('bank-reconciliation');
  };
  host.querySelectorAll('tr[data-line]').forEach(row => {
    const lineId = row.getAttribute('data-line');
    const amount = Number(row.getAttribute('data-amount'));
    const credit = row.getAttribute('data-drcr') === 'Credit';
    const detail = host.querySelector(`tr[data-line-detail="${CSS.escape(lineId)}"]`);
    const cell = detail.querySelector('td');
    const open = html => { cell.innerHTML = html; detail.classList.remove('hidden'); };

    row.querySelector('[data-bank-find]')?.addEventListener('click', async () => {
      const r = await apiFetch(`/api/v1/finance/bank-reconciliation/lines/${encodeURIComponent(lineId)}/candidates`);
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'Could not load candidates.'); return; }
      const { candidates } = await r.json();
      open(candidates.length === 0 ? `<p class="s-5872de">No unmatched book entry on this side of the account. Post the missing entry (or the bank charge), or raise an exception.</p>` : `
        <table><thead><tr><th></th><th>Posted</th><th>Document</th><th>Amount</th><th>Difference</th><th>Days apart</th></tr></thead><tbody>
        ${candidates.map(p => `<tr><td><input type="checkbox" data-cand="${escapeHTMLText(p.posting_id)}" data-cand-amount="${credit ? p.debit : p.credit}" aria-label="Select ${escapeHTMLText(p.document_id)}"></td>
          <td>${escapeHTMLText(p.date.slice(0, 16))}</td><td>${documentSearchLink(p.document_type, p.document_id, `${getDoctypeLabel(p.document_type)} ${p.document_id}`)}</td>
          <td>${money(credit ? p.debit : p.credit)}</td><td>${money(p.amount_difference)}</td><td>${p.days_apart || 0}</td></tr>`).join('')}
        </tbody></table>
        <div class="s-c052f6"><span data-cand-sum>Selected 0.00 of ${money(amount)}</span>
          <input class="form-input s-2f3f7d" data-cand-note placeholder="Note (optional)" aria-label="Match note">
          <button class="btn btn-primary btn-sm" data-cand-match disabled>Match selected</button> <button class="btn btn-outline btn-sm" data-cand-part disabled>Part match</button></div>`);
      const boxes = cell.querySelectorAll('[data-cand]');
      const sumEl = cell.querySelector('[data-cand-sum]');
      const btn = cell.querySelector('[data-cand-match]');
      const partBtn = cell.querySelector('[data-cand-part]');
      boxes.forEach(b => b.addEventListener('change', () => {
        const sum = [...boxes].filter(x => x.checked).reduce((s, x) => s + Number(x.getAttribute('data-cand-amount')), 0);
        sumEl.textContent = `Selected ${money(sum)} of ${money(amount)}${Math.abs(sum - amount) >= 0.005 && sum > 0 ? ` - difference ${money(amount - sum)}` : ''}`;
        btn.disabled = Math.abs(sum - amount) >= 0.005;
        partBtn.disabled = !(sum > 0 && sum < amount - 0.005);
      }));
      partBtn?.addEventListener('click', () => lineAction(lineId, 'part-match', {
        parts: [...boxes].filter(x => x.checked).map(x => ({ posting_id: x.getAttribute('data-cand'), amount: Number(x.getAttribute('data-cand-amount')) })),
        note: cell.querySelector('[data-cand-note]').value }, 'Part matched.'));
      btn?.addEventListener('click', () => lineAction(lineId, 'match', {
        posting_ids: [...boxes].filter(x => x.checked).map(x => x.getAttribute('data-cand')),
        note: cell.querySelector('[data-cand-note]').value }, 'Line matched.'));
    });

    row.querySelector('[data-bank-exception]')?.addEventListener('click', () => {
      open(`<div class="s-aad644">
        <div class="form-group s-116966"><label class="form-label" for="exc-owner-${escapeHTMLText(lineId)}">Owner (username)</label><input class="form-input" id="exc-owner-${escapeHTMLText(lineId)}" data-exc-owner value="${escapeHTMLText(localStorage.getItem('erp_username') || '')}"></div>
        <div class="form-group s-817b9b"><label class="form-label" for="exc-reason-${escapeHTMLText(lineId)}">Reason</label><input class="form-input" id="exc-reason-${escapeHTMLText(lineId)}" data-exc-reason></div>
        <button class="btn btn-primary btn-sm" data-exc-save>Raise exception</button></div>`);
      cell.querySelector('[data-exc-save]').addEventListener('click', () => lineAction(lineId, 'exception', {
        owner: cell.querySelector('[data-exc-owner]').value, reason: cell.querySelector('[data-exc-reason]').value }, 'Exception raised.'));
    });

    row.querySelector('[data-bank-explain]')?.addEventListener('click', async () => {
      const note = await showCustomPrompt('Why will nothing in the books match this line? (Accountant or Super Admin)', '', 'Close as explained');
      if (note) lineAction(lineId, 'explain', { note }, 'Exception closed as explained.');
    });

    row.querySelector('[data-bank-unmatch]')?.addEventListener('click', async () => {
      const reason = await showCustomPrompt('Why is this match being undone?', '', 'Unmatch');
      if (reason) lineAction(lineId, 'unmatch', { reason }, 'Match undone.');
    });

    row.querySelector('[data-bank-payout]')?.addEventListener('click', () => {
      open(`<div class="s-aad644">
        <div class="form-group s-116966"><label class="form-label" for="payout-kind-${escapeHTMLText(lineId)}">Payout from</label>
          <select class="form-select" id="payout-kind-${escapeHTMLText(lineId)}" data-payout-kind>
            ${wb.marketplace_payouts.map(s => `<option value="batch:${escapeHTMLText(s.settlement_batch_id)}">Marketplace batch ${escapeHTMLText(s.settlement_batch_id)} (${money(s.net_payout)})</option>`).join('')}
            <option value="1101">Card acquirer - clears 1101 (${money(wb.card_clearing_balance)} waiting)</option>
            <option value="1102">UPI - clears 1102 (${money(wb.upi_clearing_balance)} waiting)</option>
          </select></div>
        <div class="form-group s-116966" data-payout-fee-wrap><label class="form-label" for="payout-fee-${escapeHTMLText(lineId)}">Acquirer fee deducted (Rs)</label><input type="number" class="form-input s-465bfe" id="payout-fee-${escapeHTMLText(lineId)}" data-payout-fee value="0" min="0" step="0.01"></div>
        <button class="btn btn-primary btn-sm" data-payout-save>Match payout</button></div>`);
      const kind = cell.querySelector('[data-payout-kind]');
      const feeWrap = cell.querySelector('[data-payout-fee-wrap]');
      const syncFee = () => { feeWrap.style.display = kind.value.startsWith('batch:') ? 'none' : ''; };
      kind.addEventListener('change', syncFee);
      syncFee();
      cell.querySelector('[data-payout-save]').addEventListener('click', () => {
        if (kind.value.startsWith('batch:')) {
          lineAction(lineId, 'match-marketplace', { settlement_batch_id: kind.value.slice(6) }, 'Marketplace payout matched.');
        } else {
          lineAction(lineId, 'match-pos-payout', { clearing_account: kind.value, fee: Number(cell.querySelector('[data-payout-fee]').value || 0) }, 'Payout booked and matched.');
        }
      });
    });
  });
}

// Debit / Credit Notes (Stage 20.32). Creation reuses the generic
// doctype-table New-record form for each doctype; this view adds the Post
// action (GL reversal) the generic CRUD screen can't express.
// Stage 155.2: each note book is its own sidebar entry ("finance-notes:<book>").
const FN_BOOKS = { debit: ['menu-finance-notes', 'Debit Notes'], 'vendor-credit': ['menu-finance-vcn', 'Vendor Credit Notes'], 'customer-credit': ['menu-finance-cn', 'Customer Credit Notes'] };
async function renderFinanceNotesView(container) {
  const book = FN_BOOKS[currentViewTab] ? currentViewTab : 'debit';
  currentViewTab = book;
  setActiveMenu(FN_BOOKS[book][0]);
  const [debitRes, creditRes, vendorCreditRes] = await Promise.all([
    apiFetch('/api/v1/doc/DebitNote'),
    apiFetch('/api/v1/doc/CreditNote'),
    apiFetch('/api/v1/doc/VendorCreditNote')
  ]);
  if (!debitRes || !creditRes) return;
  await tradeDocsReady.catch(() => null);

  const debitNotes = debitRes.ok ? await debitRes.json() : [];
  const creditNotes = creditRes.ok ? await creditRes.json() : [];
  // Stage 62: the vendor-side credit note - raised automatically when a goods
  // receipt is billed below the PO price (the PO price is the agreed one).
  const vendorCreditNotes = vendorCreditRes && vendorCreditRes.ok ? await vendorCreditRes.json() : [];

  // Stage 68: the frozen transaction layout (txn-screen.css) - the three note
  // books are tabs of one screen, each list its own scroller.
  const pane = (key, label, newId, newLabel, table) => `
    <div class="txn-pane${key === book ? '' : ' hidden'}" data-pane="${key}">
      <div class="txn-note fn-pane-head"><span>${label}</span><button class="btn btn-outline btn-sm" id="${newId}">${newLabel}</button></div>
      <div class="txn-grid">${table}</div>
    </div>`;
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.id = 'fn-screen';
  screen.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">${FN_BOOKS[book][1]}</h1>
      ${txnContext()}
    </div>
    ${pane('debit', 'Debit Notes (to Vendors)', 'dn-new-btn', '+ New Debit Note',
      vendorNotesTable(debitNotes, 'DebitNote', 'No debit notes yet. A goods receipt billed above the PO price raises one automatically; use <b>+ New Debit Note</b> for any other claim on a vendor.'))}
    ${pane('vendor-credit', 'Credit Notes (to Vendors)', 'vcn-new-btn', '+ New Vendor Credit Note',
      vendorNotesTable(vendorCreditNotes, 'VendorCreditNote', 'No vendor credit notes yet. A goods receipt billed below the PO price raises one automatically.'))}
    ${pane('customer-credit', 'Credit Notes (to Customers)', 'cn-new-btn', '+ New Credit Note', `
    <table>
      <thead><tr><th>Note #</th><th>Customer</th><th>Invoice</th><th>Amount</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${creditNotes.length === 0
          ? `<tr><td colspan="7" class="s-306094">No credit notes yet. Use <b>+ New Credit Note</b>, then <b>Post</b> it to raise one against a customer.</td></tr>`
          : creditNotes.map(n => `
            <tr>
              <td class="s-355794">${n.note_number || n.id}</td>
              <td>${n.customer_id || ''}</td>
              <td>${documentSearchLink('SalesInvoice', n.sales_invoice_id)}${n.status === 'Posted' && n.applied_to_invoice != null ? `<div class="s-8cadba">Applied ${formatMoney(n.applied_to_invoice)}${Number(n.refunded_amount) > 0 ? `, refunded ${formatMoney(n.refunded_amount)}` : ''}</div>` : ''}</td>
              <td>${formatNumber(n.amount)}</td>
              <td>${n.reason || ''}</td>
              <td data-einv-id="${escapeHTMLText(n.id)}"><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${n.status}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postFinanceNote', ['CreditNote', n.id])}>Post</button> ` : ''}<button class="action-btn" ${actionAttrs('printTransactionDocument', ['CreditNote', n.id])}>Print</button></td>
            </tr>
          `).join('')}
      </tbody>
    </table>`)}
  `;
  container.appendChild(screen);

  document.getElementById('dn-new-btn').addEventListener('click', () => {
    currentDoctype = 'DebitNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('cn-new-btn').addEventListener('click', () => {
    currentDoctype = 'CreditNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('vcn-new-btn').addEventListener('click', () => {
    currentDoctype = 'VendorCreditNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  // Stage 75.2.9: each customer credit note's IRN state, from one call.
  const einvCells = [...container.querySelectorAll('[data-einv-id]')];
  einvoiceModule().then(m => m.einvoiceBadgesInto(einvCells, 'CreditNote')).catch(() => {});
}

// vendorNotesTable lists vendor-side notes (debit and Stage 62 credit) with
// their GST, total and source GRN, and Print / Post actions.
function vendorNotesTable(notes, doctype, emptyText) {
  return `
    <table>
      <thead><tr><th>Note #</th><th>Vendor</th><th>GRN</th><th class="num">Amount</th><th class="num">GST</th><th class="num">Total</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${notes.length === 0
          ? `<tr><td colspan="9" class="s-306094">${emptyText}</td></tr>`
          : notes.map(n => `
            <tr>
              <td class="s-355794">${escapeHTMLText(n.note_number || n.id)}</td>
              <td>${escapeHTMLText(n.vendor_id || '')}</td>
              <td>${n.grn_id ? documentSearchLink('GRN', n.grn_id) : '<span class="text-muted">&mdash;</span>'}</td>
              <td class="num">${formatMoney(n.amount ?? 0)}</td>
              <td class="num">${n.gst_amount ? formatMoney(n.gst_amount) : '&mdash;'}</td>
              <td class="num">${formatMoney(n.total_with_gst ?? n.amount ?? 0)}</td>
              <td>${escapeHTMLText(n.reason || '')}</td>
              <td><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(n.status || '')}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postFinanceNote', [doctype, n.id])}>Post</button> ` : ''}<button class="action-btn" ${actionAttrs('printTransactionDocument', [doctype, n.id])}>Print</button></td>
            </tr>
          `).join('')}
      </tbody>
    </table>`;
}

async function postFinanceNote(doctype, id) {
  const confirmed = await showCustomConfirm('Post this note? This books the GL reversal immediately and cannot be undone.', 'Confirm Post');
  if (!confirmed) return;
  const endpoint = doctype === 'DebitNote' ? `/api/v1/finance/debit-note/${encodeURIComponent(id)}/post`
    : doctype === 'VendorCreditNote' ? `/api/v1/finance/vendor-credit-note/${encodeURIComponent(id)}/post`
    : `/api/v1/finance/credit-note/${encodeURIComponent(id)}/post`;
  const res = await apiFetch(endpoint, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post note.');
    return;
  }
  renderView(`finance-notes:${doctype === 'DebitNote' ? 'debit' : doctype === 'VendorCreditNote' ? 'vendor-credit' : 'customer-credit'}`);
}

// Sales Invoices (Stage 20.33 prerequisite): SalesInvoice has existed since
// Stage 1 as a registered doctype with zero GL/amount/frontend - this view
// plus engines/sales_invoice.go make it a real credit-sales flow, the
// source Receivables Ageing reads.
// financeListScreen (Stage 68) is a finance list as a frozen transaction
// screen (txn-screen.css): title, who and where, the count and total, the New
// button in one bar, a one-line hint, and the list as the only scroller.
function financeListScreen(container, { title, hint, count, total, newId, newLabel, table }) {
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">${title}</h1>
      ${txnContext()}
      <div class="txn-sum"><span><b>${count}</b> document${count === 1 ? '' : 's'}</span>${total == null ? '' : `<span class="txn-total" title="Total of the documents listed">${txnMoney(total)}</span>`}</div>
      <div class="txn-actions"><button class="btn btn-primary" id="${newId}">${newLabel}</button></div>
    </div>
    ${hint ? `<div class="txn-hint"><span>${hint}</span></div>` : ''}
    <div class="txn-grid">${table}</div>`;
  container.appendChild(screen);
}

async function renderSalesInvoicesView(container) {
  const res = await apiFetch('/api/v1/doc/SalesInvoice');
  if (!res) return;

  const invoices = res.ok ? await res.json() : [];
  const STATUS_BADGE = { Draft: 'badge-secondary', Approved: 'badge-warning', Paid: 'badge-success', Cancelled: 'badge-danger' };
  financeListScreen(container, {
    title: 'Sales Invoice',
    count: invoices.length,
    total: invoices.reduce((s, v) => s + (Number(v.total_amount) || 0), 0),
    newId: 'si-new-btn',
    newLabel: '+ New Sales Invoice',
    table: `
    <table>
      <thead><tr><th>Invoice #</th><th>Customer</th><th>Amount</th><th class="num">Outstanding</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="6" class="s-306094">No sales invoices yet. Use <b>+ New Sales Invoice</b> above to bill a customer on credit; POS sales are settled immediately and do not appear here.</td></tr>`
          : invoices.map(v => {
            // Stage 73 / 55.10: what is still owed, and the receipts behind it.
            const history = settlementHistory(v, 'settlements');
            const partPaid = v.status === 'Approved' && history.length > 0;
            return `
            <tr>
              <td class="s-355794">${escapeHTMLText(v.invoice_number || v.id)}</td>
              <td>${escapeHTMLText(v.customer || '')}</td>
              <td>${formatNumber(v.total_amount)}</td>
              <td class="num">${v.status === 'Draft' || v.status === 'Cancelled' ? '&mdash;' : formatMoney(openBalance(v, 'total_amount', ['Approved']))}</td>
              <td data-einv-id="${escapeHTMLText(v.id)}"><span class="badge ${partPaid ? 'badge-warning' : (STATUS_BADGE[v.status] || 'badge-secondary')}">${partPaid ? 'Part paid' : escapeHTMLText(v.status)}</span></td>
              <td>
                ${v.status === 'Draft' && financeCanPost() ? `<button class="action-btn" ${actionAttrs('postSalesInvoiceAction', [v.id])}>Post</button>` : ''}
                ${v.status === 'Approved' && financeCanPost() ? `<button class="action-btn" data-si-settle="${escapeHTMLText(v.id)}">Settle</button>` : ''}
                ${(v.status === 'Draft' || (v.status === 'Approved' && history.length === 0)) && financeCanPost() ? `<button class="action-btn" data-si-cancel="${escapeHTMLText(v.id)}">Cancel</button>` : ''}
                ${history.length > 0 ? `<button class="action-btn" data-pay-hist-toggle="${escapeHTMLText(v.id)}">Payments (${history.length})</button>` : ''}
                <button class="action-btn" ${actionAttrs('printSalesInvoice', [v.id])}>Print</button>
              </td>
            </tr>${history.length > 0 ? paymentHistoryRow(v.id, history, 6, 'ReceiptVoucher') : ''}`;
          }).join('')}
      </tbody>
    </table>`
  });
  container.querySelectorAll('[data-si-settle]').forEach(btn => btn.addEventListener('click', () => settleSalesInvoiceAction(btn.getAttribute('data-si-settle'))));
  // 140.12.8: cancel an invoice that took no money - what it posted is reversed.
  container.querySelectorAll('[data-si-cancel]').forEach(btn => btn.addEventListener('click', async () => {
    const reason = String((await showCustomPrompt('Why is this invoice being cancelled?', '', 'Cancel invoice')) || '').trim();
    if (!reason) return;
    const res = await apiFetch(`/api/v1/finance/sales-invoice/${encodeURIComponent(btn.getAttribute('data-si-cancel'))}/cancel`, { method: 'POST', body: JSON.stringify({ reason }) });
    if (!res) return;
    if (!res.ok) { showApiError(res, 'The invoice was not cancelled.'); return; }
    showToast('Invoice cancelled.', { variant: 'success' });
    renderView('sales-invoices');
  }));
  addStatementButton('si-new-btn', 'CustomerStatement', 'Customer', 'customer');
  if (state.modules.enabled?.has('wholesale')) import('./view-wholesale.js' + new URL(import.meta.url).search).then(m => m.addWholesaleButton(document.getElementById('si-new-btn')?.parentElement, 'Summary invoices', m.openSummaryInvoices)).catch(() => {}); // 148.31
  // 140.12.5: a receipt across invoices, with bank, TDS and on account.
  const newInv = document.getElementById('si-new-btn');
  if (newInv && financeCanPost()) {
    const rb = document.createElement('button');
    rb.type = 'button';
    rb.className = 'btn btn-outline';
    rb.textContent = '+ Receipt';
    rb.style.marginRight = '8px';
    rb.addEventListener('click', async (e) => {
      const host = e.currentTarget.closest('.txn-screen').parentElement;
      const mod = await import('./view-receipt.js' + new URL(import.meta.url).search);
      await mod.renderReceiptEntry(host, () => renderView(currentView));
    });
    newInv.parentElement.insertBefore(rb, newInv);
  }
  // 142.3: sales from one Excel file - rows by barcode, a discount per line
  // (view-bulk-sales.js), the way Purchase Orders has Upload from Excel.
  if (newInv && canCreateDoctype('SalesInvoice')) {
    const ub = document.createElement('button');
    ub.type = 'button';
    ub.className = 'btn btn-outline';
    ub.textContent = 'Upload from Excel';
    ub.style.marginRight = '8px';
    ub.addEventListener('click', async (e) => {
      const host = e.currentTarget.closest('.txn-screen').parentElement;
      const mod = await import('./view-bulk-sales.js' + new URL(import.meta.url).search);
      await mod.renderBulkSales(host, { onBack: () => renderView(currentView) });
    });
    newInv.parentElement.insertBefore(ub, newInv);
  }
  wirePaymentHistory(container);
  // Stage 75.2.9: each invoice's IRN state, from one call.
  const einvCells = [...container.querySelectorAll('[data-einv-id]')];
  einvoiceModule().then(m => m.einvoiceBadgesInto(einvCells, 'SalesInvoice')).catch(() => {});
  // Stage 130.16 (74.3.4): its own entry screen - buyer GSTIN, place of supply, GST lines.
  document.getElementById('si-new-btn').addEventListener('click', async (e) => {
    // The live page element: the container handed to a view may be staged.
    const host = e.currentTarget.closest('.txn-screen').parentElement;
    const mod = await import('./view-sales-invoice.js' + new URL(import.meta.url).search);
    await mod.renderSalesInvoiceEntry(host, () => renderView(currentView));
  });
}

async function postSalesInvoiceAction(id) {
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${encodeURIComponent(id)}/post`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post invoice.');
    return;
  }
  renderView('sales-invoices');
}

// printSalesInvoice (Stage 31.1.9). Every status prints, deliberately - a
// Draft invoice is a legitimate proforma to hand a customer. What it must not
// do is look like a posted one, so both this sheet and the server-built
// payload stamp the status on the page; a Draft comes out marked DRAFT.
// Stage 98.1: the template print dialog (PDF, CSV / Excel, Send, and Print
// straight to this computer's invoice printer when QZ Tray runs - 98.4). A
// Draft is titled "Proforma Invoice (Draft ...)" by the print data; an
// e-invoice's IRN and signed QR print as before.
window.printSalesInvoice = async function(id) {
  (await import('./trade-docs.js' + new URL(import.meta.url).search)).printTransactionDocument('SalesInvoice', id);
};

const einvoiceModule = () => import('./view-einvoice.js' + new URL(import.meta.url).search);

async function renderInvoicePrintSheet(invoice, irn, einv) {
  const area = document.getElementById('invoice-print-area');
  if (!area) return;
  const status = invoice.status || '';
  const row = (label, value) => value
    ? `<tr><td class="invoice-key">${label}</td><td>${value}</td></tr>`
    : '';
  const draft = (status !== 'Approved' && status !== 'Paid')
    ? `<div class="invoice-draft">${escapeHTMLText(status.toUpperCase())}</div>`
    : '';
  area.innerHTML = `
    <div class="invoice-sheet">
      <div class="invoice-title">Tax Invoice</div>
      ${draft}
      <hr>
      <table>
        ${row('Invoice', escapeHTMLText(invoice.invoice_number || invoice.id))}
        ${row('Customer', escapeHTMLText(invoice.customer || ''))}
        ${row('Customer PO / Ref', escapeHTMLText(invoice.customer_reference || ''))}
        ${row('Location', escapeHTMLText(invoice.location || ''))}
        ${row('Status', escapeHTMLText(status))}
      </table>
      <div class="invoice-total">Total: ${Number(invoice.total_amount || 0).toFixed(2)}</div>
      ${einv ? einv.einvoicePrintBlock(irn) : ''}
    </div>
  `;
  if (einv && irn && irn.signed_qr) await einv.einvoiceQRInto(area.querySelector('.einv-print-qr'), irn.signed_qr);
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// settleSalesInvoiceAction (Stage 73 / 55.10): asks how much the customer
// paid, offering the whole outstanding balance. Accepting the offer sends no
// amount, so the server settles exactly what its ledger carries; any other
// figure is a part-receipt. The reference is made once per dialog, so a
// retried request posts once.
async function settleSalesInvoiceAction(id) {
  const docRes = await apiFetch(`/api/v1/doc/SalesInvoice/${encodeURIComponent(id)}`);
  if (!docRes) return;
  if (!docRes.ok) {
    await showApiError(docRes, 'Failed to load the invoice.');
    return;
  }
  const inv = await docRes.json();
  const outstanding = openBalance(inv, 'total_amount', ['Approved']);
  const currency = inv.currency ? ` ${inv.currency}` : '';
  const body = await askSettlementAmount(`Amount received from the customer. Outstanding: ${formatMoney(outstanding)}${currency}.`, outstanding, 'Settle Invoice');
  if (!body) return;
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${encodeURIComponent(id)}/settle`, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to settle invoice.');
    return;
  }
  const out = await res.json();
  await showCustomAlert(out.status === 'Paid'
    ? `Received ${formatMoney(out.amount)}${currency}. The invoice is fully settled.`
    : `Received ${formatMoney(out.amount)}${currency}. Still outstanding: ${formatMoney(out.outstanding_amount)}${currency}.`, 'Receipt Recorded');
  renderView('sales-invoices');
}

// --- Stage 73 / 55.10: part-settlement helpers shared by both invoice lists. ---

// openBalance is what an invoice still owes: the server's outstanding_amount
// once anything is received or paid, else the whole amount while it is open.
function openBalance(v, amountField, openStatuses) {
  if (v.outstanding_amount !== undefined && v.outstanding_amount !== null && v.outstanding_amount !== '') return Number(v.outstanding_amount) || 0;
  return openStatuses.includes(v.status) ? (Number(v[amountField]) || 0) : 0;
}

function settlementHistory(v, field) {
  return Array.isArray(v[field]) ? v[field] : [];
}

function settlementReference() {
  try {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') return window.crypto.randomUUID();
  } catch (_) { /* fall back to a time-based reference */ }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// askSettlementAmount returns the request body, or null when cancelled.
async function askSettlementAmount(message, outstanding, title) {
  const entered = await showCustomPrompt(message, outstanding.toFixed(2), title, 'number');
  if (entered === null) return null;
  const amount = Number(String(entered).trim());
  if (!(amount > 0)) {
    await showCustomAlert('Enter an amount above zero.', title);
    return null;
  }
  const body = { reference: settlementReference() };
  if (amount.toFixed(2) !== outstanding.toFixed(2)) body.amount = amount;
  return body;
}

// paymentHistoryRow is the Payments expansion under an invoice row: every
// receipt, payment and applied credit note, oldest first.
function paymentHistoryRow(id, entries, colspan, voucher = '') {
  // Stage 130.17 (127.3.2): each receipt / payment prints as its own voucher.
  const lines = entries.map(e => `
    <tr>
      <td>${escapeHTMLText(e.date || '')}</td>
      <td>${escapeHTMLText(e.kind || '')}</td>
      <td class="s-355794">${escapeHTMLText(e.reference || '-')}</td>
      <td class="num">${formatMoney(e.amount ?? 0)}</td>
      <td>${escapeHTMLText(e.by || '')}</td>
      <td>${voucher && e.reference ? `<button type="button" class="action-btn" data-pay-print="${escapeHTMLText(voucher)}" data-pay-id="${escapeHTMLText(`${id}~${e.reference}`)}">Voucher</button>` : ''}</td>
    </tr>`).join('');
  return `<tr class="hidden" data-pay-hist-row="${escapeHTMLText(id)}"><td colspan="${colspan}">
    <table><thead><tr><th>Date</th><th>Type</th><th>Reference</th><th class="num">Amount</th><th>By</th><th></th></tr></thead><tbody>${lines}</tbody></table>
  </td></tr>`;
}

// printFinanceDocument (Stage 130.17) opens the print window on one
// voucher or statement.
async function printFinanceDocument(txn, doctype, id) {
  const docs = await import('./trade-docs.js' + new URL(import.meta.url).search);
  docs.printTransactionDocument(doctype, id, { txn, only: true });
}

// Stage 148.19: Bills without a PO (view-expense-bill.js), shown only when an admin has switched it on.
async function addExpenseBillButton(newBtnId) {
  const res = await apiFetch('/api/v1/procurement/expense-bills');
  const newBtn = document.getElementById(newBtnId);
  if (!res || !res.ok || !newBtn) return;
  const data = await res.json();
  if (!data.enabled) return;
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'btn btn-outline';
  b.textContent = 'Bills without a PO';
  b.style.marginRight = '8px';
  b.addEventListener('click', async () => {
    const mod = await loadViewModule('/view-expense-bill.js?v=1');
    await mod.renderExpenseBillsView();
  });
  newBtn.parentElement.insertBefore(b, newBtn);
}

// addStatementButton puts "Statement" beside a list's New button.
function addStatementButton(newBtnId, txn, doctype, who) {
  const newBtn = document.getElementById(newBtnId);
  if (!newBtn) return;
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'btn btn-outline';
  b.textContent = 'Statement';
  b.style.marginRight = '8px';
  b.addEventListener('click', async () => {
    const code = String((await showCustomPrompt(`Which ${who}? (their code)`, '', 'Statement of account')) || '').trim();
    if (code) printFinanceDocument(txn, doctype, code);
  });
  newBtn.parentElement.insertBefore(b, newBtn);
}

function wirePaymentHistory(root) {
  root.querySelectorAll('[data-pay-print]').forEach(btn => btn.addEventListener('click', () =>
    printFinanceDocument(btn.dataset.payPrint, btn.dataset.payPrint === 'ReceiptVoucher' ? 'SalesInvoice' : 'VendorInvoice', btn.dataset.payId)));
  root.querySelectorAll('[data-pay-hist-toggle]').forEach(btn => btn.addEventListener('click', () => {
    const id = btn.getAttribute('data-pay-hist-toggle');
    // Looked up from the button, not root: the shell may move the rendered
    // screen out of the container it was built in.
    const table = btn.closest('table');
    const row = table && Array.from(table.querySelectorAll('[data-pay-hist-row]')).find(r => r.getAttribute('data-pay-hist-row') === id);
    if (row) row.classList.toggle('hidden');
  }));
}

// Fulfillment / reservation workbench (Stage 13.6) - pick/pack/dispatch
// against FulfillmentTask documents (already a real doctype, stored via the
// generic documents table - GET /api/v1/doc/FulfillmentTask lists them with
// no new backend endpoint needed) and the already-working
// POST /api/v1/fulfillment/task/transition. The backend doesn't enforce a
// specific transition order (see engines.TransitionTaskStatus), so the
// "next status" buttons below are a UX guardrail, not a hard constraint.
export { renderFinanceView, renderChartOfAccountsPanel, renderAccountingPeriodsPanel, showPeriodCloseChecklist, renderVendorInvoicesView, renderVendorInvoiceActions, matchVendorInvoice, payVendorInvoicePlain, overrideAndPayVendorInvoice, payVendorInvoiceTDS, renderPaymentProposalsView, executePaymentProposal, renderBankReconciliationView, renderFinanceNotesView, postFinanceNote, renderSalesInvoicesView, postSalesInvoiceAction, renderInvoicePrintSheet, settleSalesInvoiceAction };
