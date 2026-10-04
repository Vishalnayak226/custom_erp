// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
let currentFinanceTab = 'trial-balance';
const FINANCE_TABS = [
  { id: 'trial-balance', label: 'Trial Balance' },
  { id: 'chart-of-accounts', label: 'Chart of Accounts' },
  { id: 'periods', label: 'Accounting Periods' }
];

async function renderFinanceView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Finance / GL</h1>
      <p class="page-subtitle">Trial balance across all posted GL accounts, and accounting-period close control.</p>
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
  tabBar.innerHTML = FINANCE_TABS.map(t =>
    `<button class="btn ${t.id === currentFinanceTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-finance-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-finance-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentFinanceTab = btn.getAttribute('data-finance-tab');
      renderView('finance');
    });
  });

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
      <input type="date" id="tb-as-of" class="form-input" style="width:180px;" value="${financeTrialBalanceAsOf}">
    </div>
    <span style="color:var(--text-muted); font-size:12px; padding-bottom:10px;">
      Includes every GL posting up to and including this date.
    </span>
  `;
  container.appendChild(controls);
  document.getElementById('tb-as-of').addEventListener('change', (e) => {
    if (!e.target.value) return; // an emptied picker would 400; keep the last good date
    financeTrialBalanceAsOf = e.target.value;
    renderView('finance');
  });

  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(financeTrialBalanceAsOf)}`);
  if (!res) return;

  if (!res.ok) {
    // BLD-036: was a dead-end modal-then-nothing - Try Again re-renders the
    // whole view (tab bar, as-of picker and data all come back), same as the
    // already-fixed Approvals/Fulfillment/GRN pattern.
    renderErrorPanel(container, 'Failed to load trial balance.', () => renderView('finance'));
    return;
  }

  const data = await res.json();
  const balances = data.balances || [];

  const summaryRow = document.createElement('div');
  summaryRow.className = 'dashboard-stats-row';
  summaryRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Total Debits</span>
      <span class="stat-val">${(data.total_debits ?? 0).toLocaleString()}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Total Credits</span>
      <span class="stat-val">${(data.total_credits ?? 0).toLocaleString()}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Ledger Status</span>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
        <span class="pulse-dot" style="background: ${data.balanced ? '#10b981' : '#ef4444'};"></span>
        <span style="font-size: 16px; font-weight: 700; color: ${data.balanced ? '#10b981' : '#ef4444'};">${data.status || ''}</span>
      </div>
    </div>
    <div class="stat-card">
      <span class="stat-label">As Of</span>
      <span class="stat-val">${data.as_of || financeTrialBalanceAsOf}</span>
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
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No GL postings on or before this date. Postings are created automatically when a sale, receipt or invoice is posted &mdash; try a later As Of date.</td></tr>`;
  }
  balances.forEach(b => {
    html += `
      <tr>
        <td style="font-family: monospace;">${b.account_code}</td>
        <td style="font-weight:600;">${b.account_name}</td>
        <td>${b.account_type}</td>
        <td>${b.debit.toLocaleString()}</td>
        <td>${b.credit.toLocaleString()}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
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

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Account Code</th>
          <th>Account Name</th>
          <th>Type</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (accounts.length === 0) {
    html += `<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No GL accounts configured yet. The chart of accounts is seeded during installation &mdash; ask your administrator to apply any pending database migrations.</td></tr>`;
  }
  accounts.forEach(a => {
    html += `
      <tr>
        <td style="font-family: monospace;">${a.account_code}</td>
        <td style="font-weight:600;">${a.account_name}</td>
        <td>${a.account_type}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// Accounting Periods (Stage 20.34): this had no frontend at all before this
// stage even though Stage 17.4's create/list/close API has existed since
// then - a real pre-existing gap, same shape as the Transfers/Inventory/
// Users/Roles ones Stage 22 fixed. Create + list + close, plus the new
// read-only pre-close checklist (engines.GetPeriodCloseChecklist) surfaced
// before the user commits to closing a period.
async function renderAccountingPeriodsPanel(container) {
  const res = await apiFetch('/api/v1/finance/periods');
  if (!res) return;
  const periods = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Accounting Period</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-name">Period Name</label>
        <input type="text" id="period-name" class="form-input" placeholder="e.g. FY2026-Q3" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-start">Start Date</label>
        <input type="date" id="period-start" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-end">End Date</label>
        <input type="date" id="period-end" class="form-input">
      </div>
      <button class="btn btn-primary" id="period-create-btn">Create Period</button>
    </div>
    <div id="period-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Period Name</th><th>Start</th><th>End</th><th>Status</th><th>Closed By</th><th>Actions</th></tr></thead>
      <tbody>
        ${periods.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No accounting periods yet. Use <b>Create Period</b> above to open your first one; the close checklist needs a period to run against.</td></tr>`
          : periods.map(p => `
            <tr>
              <td style="font-weight:600;">${p.period_name}</td>
              <td>${p.start_date}</td>
              <td>${p.end_date}</td>
              <td><span class="badge ${p.status === 'Open' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
              <td>${p.closed_by || ''}</td>
              <td>
                ${p.status === 'Open' ? `<button class="action-btn" ${actionAttrs('showPeriodCloseChecklist', [p.id])}>Close Checklist</button>` : ''}
              </td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);

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
    renderView('finance');
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
    `${summary}\n\n${lines}\n\nClosing a period is permanent - there is no reopen. Close "${checklist.period_name}" now?`,
    'Period Close Checklist'
  );
  if (!proceed) return;
  const closeRes = await apiFetch(`/api/v1/finance/periods/${periodId}/close`, { method: 'POST' });
  if (!closeRes) return;
  if (!closeRes.ok) {
    await showApiError(closeRes, 'Failed to close period.');
    return;
  }
  renderView('finance');
}

// Vendor Invoices (Stage 20.27/20.28 prerequisite): VendorInvoice has
// existed since Stage 17.8 (3-way match + pay) with zero frontend of its
// own. Creation reuses the generic doctype-table's own New-record form
// (VendorInvoice's fields are already registered) rather than a parallel
// create form here - this view only adds the match/pay actions the generic
// CRUD screen can't express.
async function renderVendorInvoicesView(container) {
  const [invRes, sectionsRes] = await Promise.all([
    apiFetch('/api/v1/doc/VendorInvoice'),
    apiFetch('/api/v1/doc/TDSSection')
  ]);
  if (!invRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Vendor Invoice</h1>
      <p class="page-subtitle">3-way match against PO/GRN, then pay - plain or TDS-withheld.</p>
    </div>
    <button class="btn btn-primary" id="vi-new-btn">+ New Vendor Invoice</button>
  `;
  container.appendChild(header);
  document.getElementById('vi-new-btn').addEventListener('click', () => {
    currentDoctype = 'VendorInvoice'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });

  const invoices = invRes.ok ? await invRes.json() : [];
  const sections = (sectionsRes && sectionsRes.ok) ? await sectionsRes.json() : [];
  window.__tdsSections = sections;

  const STATUS_BADGE = { Draft: 'badge-secondary', Matched: 'badge-success', MismatchHold: 'badge-danger', Paid: 'badge-success' };
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Invoice #</th><th>Vendor</th><th>PO</th><th>GRN</th><th>Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No vendor invoices yet. Use <b>+ New Vendor Invoice</b> above &mdash; have the Purchase Order and Goods Receipt it should 3-way match against to hand.</td></tr>`
          : invoices.map(v => `
            <tr>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.vendor_id || ''}</td>
              <td>${v.po_id || ''}</td>
              <td>${v.grn_id || ''}</td>
              <td>${(v.invoice_amount ?? 0).toLocaleString()}</td>
              <td><span class="badge ${STATUS_BADGE[v.status] || 'badge-secondary'}">${v.status}</span></td>
              <td>${renderVendorInvoiceActions(v)}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

function renderVendorInvoiceActions(v) {
  const id = v.id;
  if (v.status === 'Draft' || v.status === 'MismatchHold') {
    // Stage 26.3.5: engines.PayVendorInvoice's override path (Stage 24.11 -
    // pay a MismatchHold invoice anyway with a business-justified reason,
    // routed to approval) already existed server-side but had no UI action
    // calling it - only Match did. Draft has nothing to override yet
    // (3-way match hasn't run), so this only shows for MismatchHold.
    return `<button class="action-btn" ${actionAttrs('matchVendorInvoice', [id])}>Match</button>${v.status === 'MismatchHold' ? `<button class="action-btn" style="margin-left:4px;" ${actionAttrs('overrideAndPayVendorInvoice', [id])}>Override &amp; Pay</button>` : ''}`;
  }
  if (v.status === 'Matched') {
    const sections = window.__tdsSections || [];
    const tdsOptions = sections.map(s => `<option value="${s.section_code || s.id}">${s.section_code || s.id} (${s.rate_percent}%)</option>`).join('');
    return `
      <button class="action-btn" ${actionAttrs('payVendorInvoicePlain', [id])}>Pay</button>
      ${sections.length > 0 ? `
        <select id="tds-select-${id}" class="form-select" style="width: 110px; display:inline-block; margin: 0 4px;">${tdsOptions}</select>
        <button class="action-btn" ${actionAttrs('payVendorInvoiceTDS', [id])}>Pay w/ TDS</button>
      ` : ''}
    `;
  }
  return '';
}

async function matchVendorInvoice(invoiceId) {
  const poId = await showCustomPrompt('Enter the PO ID/number this invoice matches:', '', 'Match Invoice');
  if (poId === null) return;
  const grnId = await showCustomPrompt('Enter the GRN ID/number this invoice matches:', '', 'Match Invoice');
  if (grnId === null) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/match', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, po_id: poId, grn_id: grnId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Match failed.');
    return;
  }
  renderView('vendor-invoices');
}

async function payVendorInvoicePlain(invoiceId) {
  const confirmed = await showCustomConfirm('Pay this vendor invoice in full via Cash/Bank?', 'Confirm Payment');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Payment failed.');
    return;
  }
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

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Payment Proposals</h1>
      <p class="page-subtitle">Group Matched vendor invoices into one payment run.</p>
    </div>
  `;
  container.appendChild(header);

  const invoices = (invRes.ok ? await invRes.json() : []).filter(v => v.status === 'Matched');
  const proposals = propRes.ok ? await propRes.json() : [];

  const builderPanel = document.createElement('div');
  builderPanel.className = 'table-panel';
  builderPanel.style.padding = '24px';
  builderPanel.style.marginBottom = '24px';
  builderPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Build a Proposal from Matched Invoices</h2>
    ${invoices.length === 0 ? `<p style="color:var(--text-muted);">No Matched vendor invoices available to batch.</p>` : `
      <table style="margin-bottom: 16px;">
        <thead><tr><th></th><th>Invoice #</th><th>Vendor</th><th>Amount</th></tr></thead>
        <tbody>
          ${invoices.map(v => `
            <tr>
              <td><input type="checkbox" class="pp-invoice-check" value="${v.id}"></td>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.vendor_id || ''}</td>
              <td>${(v.invoice_amount ?? 0).toLocaleString()}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
      <button class="btn btn-primary" id="pp-create-btn">Create Proposal from Selected</button>
    `}
    <div id="pp-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(builderPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `
    <table>
      <thead><tr><th>Proposal #</th><th>Invoices</th><th>Total Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${proposals.length === 0
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No payment proposals yet. Tick one or more Matched vendor invoices above, then <b>Create Proposal from Selected</b>.</td></tr>`
          : proposals.map(p => {
            let ids = [];
            try { ids = JSON.parse(p.invoice_ids || '[]'); } catch (e) { /* leave empty */ }
            return `
              <tr>
                <td style="font-family: monospace;">${p.proposal_number || p.id}</td>
                <td>${ids.length} invoice(s)</td>
                <td>${(p.total_amount ?? 0).toLocaleString()}</td>
                <td><span class="badge ${p.status === 'Executed' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
                <td>${p.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('executePaymentProposal', [p.id])}>Execute</button>` : ''}</td>
              </tr>
            `;
          }).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(listPanel);

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
    renderView('payment-proposals');
  });
}

async function executePaymentProposal(proposalId) {
  const confirmed = await showCustomConfirm('Execute this payment run? Every invoice in it will be paid via the standard vendor-invoice payment path.', 'Execute Payment Proposal');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/finance/payment-proposal/${proposalId}/execute`, { method: 'POST' });
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

// Bank Reconciliation (Stage 20.25/20.26): BankAccount/BankStatementLine
// creation and CSV import reuse the generic doctype-table screen (linked
// below) - this view only adds the reconcile action the generic CRUD
// screen can't express.
async function renderBankReconciliationView(container) {
  const res = await apiFetch('/api/v1/doc/BankAccount');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bank Reconciliation</h1>
      <p class="page-subtitle">Match imported bank-statement lines against GL postings for a bank account.</p>
    </div>
  `;
  container.appendChild(header);

  const accounts = res.ok ? await res.json() : [];
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0; min-width: 220px;">
        <label class="form-label" for="bank-recon-account">Bank Account</label>
        <select id="bank-recon-account" class="form-select">
          <option value="">Select a bank account...</option>
          ${accounts.map(a => `<option value="${a.id}">${a.bank_name || ''} - ${a.account_number || a.id}</option>`).join('')}
        </select>
      </div>
      <button class="btn btn-primary" id="bank-recon-btn">Reconcile</button>
      <button class="btn btn-outline" id="bank-recon-manage-accounts-btn">Manage Bank Accounts</button>
      <button class="btn btn-outline" id="bank-recon-manage-lines-btn">Statement Lines / Import CSV</button>
    </div>
    ${accounts.length === 0 ? `<p style="color:var(--text-muted);">No bank accounts yet - use "Manage Bank Accounts" to add one.</p>` : ''}
    <div id="bank-recon-result"></div>
  `;
  container.appendChild(panel);

  document.getElementById('bank-recon-manage-accounts-btn').addEventListener('click', () => {
    currentDoctype = 'BankAccount'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-recon-manage-lines-btn').addEventListener('click', () => {
    currentDoctype = 'BankStatementLine'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-recon-btn').addEventListener('click', async () => {
    const bankAccount = document.getElementById('bank-recon-account').value;
    const resultEl = document.getElementById('bank-recon-result');
    if (!bankAccount) {
      resultEl.innerHTML = `<p class="login-error">Select a bank account first.</p>`;
      return;
    }
    const reconRes = await apiFetch('/api/v1/finance/bank-reconcile', {
      method: 'POST',
      body: JSON.stringify({ bank_account: bankAccount })
    });
    if (!reconRes) return;
    if (!reconRes.ok) {
      resultEl.innerHTML = `<p class="login-error">${await getErrorMessage(reconRes, 'Reconciliation failed.')}</p>`;
      return;
    }
    const result = await reconRes.json();
    resultEl.innerHTML = `
      <div class="table-panel" style="padding: 16px; margin-top: 8px;">
        <p><strong>${result.matched}</strong> line(s) matched.</p>
        <p>${result.unmatched_statement_lines.length} statement line(s) still unmatched: ${result.unmatched_statement_lines.join(', ') || 'none'}</p>
        <p>${result.unmatched_gl_postings.length} GL posting(s) still unmatched: ${result.unmatched_gl_postings.join(', ') || 'none'}</p>
      </div>
    `;
  });
}

// Debit / Credit Notes (Stage 20.32). Creation reuses the generic
// doctype-table New-record form for each doctype; this view adds the Post
// action (GL reversal) the generic CRUD screen can't express.
async function renderFinanceNotesView(container) {
  const [debitRes, creditRes] = await Promise.all([
    apiFetch('/api/v1/doc/DebitNote'),
    apiFetch('/api/v1/doc/CreditNote')
  ]);
  if (!debitRes || !creditRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Debit / Credit Notes</h1>
      <p class="page-subtitle">Post-facto vendor and customer adjustments, GL-reversing on Post.</p>
    </div>
  `;
  container.appendChild(header);

  const debitNotes = debitRes.ok ? await debitRes.json() : [];
  const creditNotes = creditRes.ok ? await creditRes.json() : [];

  const debitPanel = document.createElement('div');
  debitPanel.className = 'table-panel';
  debitPanel.style.padding = '24px';
  debitPanel.style.marginBottom = '24px';
  debitPanel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700;">Debit Notes (to Vendors)</h2>
      <button class="btn btn-outline btn-sm" id="dn-new-btn">+ New Debit Note</button>
    </div>
    <table>
      <thead><tr><th>Note #</th><th>Vendor</th><th>Amount</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${debitNotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No debit notes yet. Fill in the form above and <b>Post</b> to raise one against a vendor.</td></tr>`
          : debitNotes.map(n => `
            <tr>
              <td style="font-family: monospace;">${n.note_number || n.id}</td>
              <td>${n.vendor_id || ''}</td>
              <td>${(n.amount ?? 0).toLocaleString()}</td>
              <td>${n.reason || ''}</td>
              <td><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${n.status}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postFinanceNote', ['DebitNote', n.id])}>Post</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(debitPanel);

  const creditPanel = document.createElement('div');
  creditPanel.className = 'table-panel';
  creditPanel.style.padding = '24px';
  creditPanel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700;">Credit Notes (to Customers)</h2>
      <button class="btn btn-outline btn-sm" id="cn-new-btn">+ New Credit Note</button>
    </div>
    <table>
      <thead><tr><th>Note #</th><th>Customer</th><th>Amount</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${creditNotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No credit notes yet. Fill in the form above and <b>Post</b> to raise one against a customer.</td></tr>`
          : creditNotes.map(n => `
            <tr>
              <td style="font-family: monospace;">${n.note_number || n.id}</td>
              <td>${n.customer_id || ''}</td>
              <td>${(n.amount ?? 0).toLocaleString()}</td>
              <td>${n.reason || ''}</td>
              <td><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${n.status}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postFinanceNote', ['CreditNote', n.id])}>Post</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(creditPanel);

  document.getElementById('dn-new-btn').addEventListener('click', () => {
    currentDoctype = 'DebitNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('cn-new-btn').addEventListener('click', () => {
    currentDoctype = 'CreditNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
}

async function postFinanceNote(doctype, id) {
  const confirmed = await showCustomConfirm('Post this note? This books the GL reversal immediately and cannot be undone.', 'Confirm Post');
  if (!confirmed) return;
  const endpoint = doctype === 'DebitNote' ? `/api/v1/finance/debit-note/${id}/post` : `/api/v1/finance/credit-note/${id}/post`;
  const res = await apiFetch(endpoint, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post note.');
    return;
  }
  renderView('finance-notes');
}

// Sales Invoices (Stage 20.33 prerequisite): SalesInvoice has existed since
// Stage 1 as a registered doctype with zero GL/amount/frontend - this view
// plus engines/sales_invoice.go make it a real credit-sales flow, the
// source Receivables Ageing reads.
async function renderSalesInvoicesView(container) {
  const res = await apiFetch('/api/v1/doc/SalesInvoice');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sales Invoice</h1>
      <p class="page-subtitle">Credit sales to customers - Post to recognize the receivable, Settle once paid.</p>
    </div>
    <button class="btn btn-primary" id="si-new-btn">+ New Sales Invoice</button>
  `;
  container.appendChild(header);
  document.getElementById('si-new-btn').addEventListener('click', () => {
    currentDoctype = 'SalesInvoice'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });

  const invoices = res.ok ? await res.json() : [];
  const STATUS_BADGE = { Draft: 'badge-secondary', Approved: 'badge-warning', Paid: 'badge-success', Cancelled: 'badge-danger' };
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Invoice #</th><th>Customer</th><th>Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No sales invoices yet. Use <b>+ New Sales Invoice</b> above to bill a customer on credit; POS sales are settled immediately and do not appear here.</td></tr>`
          : invoices.map(v => `
            <tr>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.customer || ''}</td>
              <td>${(v.total_amount ?? 0).toLocaleString()}</td>
              <td><span class="badge ${STATUS_BADGE[v.status] || 'badge-secondary'}">${v.status}</span></td>
              <td>
                ${v.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('postSalesInvoiceAction', [v.id])}>Post</button>` : ''}
                ${v.status === 'Approved' ? `<button class="action-btn" ${actionAttrs('settleSalesInvoiceAction', [v.id])}>Settle</button>` : ''}
                <button class="action-btn" ${actionAttrs('printSalesInvoice', [v.id])}>Print</button>
              </td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

async function postSalesInvoiceAction(id) {
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${id}/post`, { method: 'POST' });
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
window.printSalesInvoice = async function(id) {
  if (await qzTryPrint('Invoice', { documentRef: id, quiet: true })) return;

  const res = await apiFetch(`/api/v1/doc/SalesInvoice/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the invoice.');
    return;
  }
  renderInvoicePrintSheet(await res.json());
};

function renderInvoicePrintSheet(invoice) {
  const area = document.getElementById('invoice-print-area');
  if (!area) return;
  const status = invoice.status || '';
  const row = (label, value) => value
    ? `<tr><td class="invoice-key">${label}</td><td>${value}</td></tr>`
    : '';
  const draft = (status !== 'Approved' && status !== 'Paid')
    ? `<div class="invoice-draft">${status.toUpperCase()}</div>`
    : '';
  area.innerHTML = `
    <div class="invoice-sheet">
      <div class="invoice-title">Tax Invoice</div>
      ${draft}
      <hr>
      <table>
        ${row('Invoice', invoice.invoice_number || invoice.id)}
        ${row('Customer', invoice.customer)}
        ${row('Location', invoice.location)}
        ${row('Status', status)}
      </table>
      <div class="invoice-total">Total: ${Number(invoice.total_amount || 0).toFixed(2)}</div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

async function settleSalesInvoiceAction(id) {
  const confirmed = await showCustomConfirm('Mark this invoice as settled (customer paid in full)?', 'Confirm Settlement');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${id}/settle`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to settle invoice.');
    return;
  }
  renderView('sales-invoices');
}

// Fulfillment / reservation workbench (Stage 13.6) - pick/pack/dispatch
// against FulfillmentTask documents (already a real doctype, stored via the
// generic documents table - GET /api/v1/doc/FulfillmentTask lists them with
// no new backend endpoint needed) and the already-working
// POST /api/v1/fulfillment/task/transition. The backend doesn't enforce a
// specific transition order (see engines.TransitionTaskStatus), so the
// "next status" buttons below are a UX guardrail, not a hard constraint.
export { renderFinanceView, renderChartOfAccountsPanel, renderAccountingPeriodsPanel, showPeriodCloseChecklist, renderVendorInvoicesView, renderVendorInvoiceActions, matchVendorInvoice, payVendorInvoicePlain, overrideAndPayVendorInvoice, payVendorInvoiceTDS, renderPaymentProposalsView, executePaymentProposal, renderBankReconciliationView, renderFinanceNotesView, postFinanceNote, renderSalesInvoicesView, postSalesInvoiceAction, renderInvoicePrintSheet, settleSalesInvoiceAction };
