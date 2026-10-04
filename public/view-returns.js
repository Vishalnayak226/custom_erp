// BLD-041: native ES module for the standalone Returns workbench.
// POS-specific return lookup stays in view-pos.js; this workbench is loaded
// only after the existing route-entitlement check succeeds.

// --- Returns management (Stage 47.4.6) -----------------------------------
//
// Stage 35.9 built the whole ReturnRequest/RefundRequest workflow - approve,
// reject, reverse pickup, receive, QC with per-line disposition, exchange,
// refund approval and refund processing - and its own checklist entry recorded
// that it had no management UI. Every one of those endpoints had zero caller
// anywhere in this file, which meant a return could be *raised* from the POS
// and then never progressed except by a developer with curl.
//
// This screen is that missing surface. It deliberately drives the existing
// endpoints rather than adding new ones, and it shows the aggregate's own
// state machine as the source of which actions are offered - so a state added
// server-side later cannot leave a stale button behind here.

const RETURN_ACTIONS_BY_STATUS = {
  'Requested': ['approve', 'reject'],
  'Approved': ['reverse-pickup', 'receive', 'reject'],
  'Received': ['qc'],
  'QC Complete': ['refund'],
  'Closed': [],
  'Rejected': []
};

const RETURN_DISPOSITIONS = ['Sellable', 'Damaged', 'Repairable', 'Missing', 'Wrong-Item', 'Rejected'];

let returnsViewRows = [];
let returnsViewFilter = 'open';

async function renderReturnsView(root) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:flex-end; gap:16px; margin-bottom:16px;">
      <div>
        <h2 style="margin:0 0 4px; font-size:16px;">Returns</h2>
        <div style="font-size:12.5px; color:var(--text-muted);">
          A return moves Requested &rarr; Approved &rarr; Received &rarr; QC Complete &rarr; Closed.
          Stock is only received back at inspection, and no refund is paid until the goods have been inspected.
        </div>
      </div>
      <div style="display:flex; gap:8px; align-items:flex-end;">
        <div class="form-group" style="max-width:180px; margin-bottom:0;">
          <label class="form-label" for="returns-filter">Show</label>
          <select id="returns-filter" class="form-input">
            <option value="open">Open returns</option>
            <option value="all">All returns</option>
            <option value="Requested">Awaiting approval</option>
            <option value="Approved">Awaiting goods</option>
            <option value="Received">Awaiting inspection</option>
            <option value="QC Complete">Awaiting refund</option>
          </select>
        </div>
        <button class="btn btn-outline" id="returns-refresh-btn" type="button">Refresh</button>
      </div>
    </div>
    <div id="returns-error" class="login-error hidden" style="margin-bottom:16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Return</th><th>Type</th><th>Original Bill</th><th>Location</th>
          <th>Items</th><th>Refund Eligible</th><th>Status</th><th>Actions</th>
        </tr>
      </thead>
      <tbody id="returns-body"></tbody>
    </table>
    <div id="returns-empty" style="padding:24px 4px; color:var(--text-muted); font-size:13px;"></div>
  `;
  root.appendChild(panel);

  document.getElementById('returns-refresh-btn').addEventListener('click', loadReturnsView);
  document.getElementById('returns-filter').addEventListener('change', (e) => {
    returnsViewFilter = e.target.value;
    renderReturnsTable();
  });
  await loadReturnsView();
}

async function loadReturnsView() {
  const errorEl = document.getElementById('returns-error');
  if (errorEl) errorEl.classList.add('hidden');
  // Read through the ordinary generic doctype endpoint rather than a bespoke
  // list route: ReturnRequest is a registered doctype and already carries the
  // role/scope filtering every other document read gets, so a second endpoint
  // would be a second place for that filtering to drift.
  const res = await apiFetch('/api/v1/doc/ReturnRequest?limit=200');
  if (!res) return;
  if (!res.ok) {
    if (errorEl) {
      errorEl.textContent = await getErrorMessage(res, 'Returns could not be loaded.');
      errorEl.classList.remove('hidden');
    }
    return;
  }
  const payload = await res.json();
  const docs = Array.isArray(payload) ? payload : (payload.data || payload.documents || []);
  returnsViewRows = docs.map(d => {
    const data = d.data || d;
    return {
      id: d.id || data.code,
      requestType: data.request_type || '',
      originalOrderID: data.original_order_id || '',
      returnLocation: data.return_location || '',
      status: data.status || d.status || '',
      items: Array.isArray(data.items) ? data.items : [],
      refundEligible: Number(data.total_refund_eligible) || 0
    };
  });
  renderReturnsTable();
}

function renderReturnsTable() {
  const body = document.getElementById('returns-body');
  if (!body) return;
  body.innerHTML = '';

  const rows = returnsViewRows.filter(r => {
    if (returnsViewFilter === 'all') return true;
    if (returnsViewFilter === 'open') return r.status !== 'Closed' && r.status !== 'Rejected';
    return r.status === returnsViewFilter;
  });

  const emptyEl = document.getElementById('returns-empty');
  if (emptyEl) {
    emptyEl.textContent = rows.length ? '' :
      (returnsViewRows.length ? 'No returns match this filter.' : 'No returns have been raised yet. A return starts at the till, from the original bill.');
  }

  rows.forEach(r => {
    const actions = RETURN_ACTIONS_BY_STATUS[r.status] || [];
    const qtySummary = r.items.reduce((n, it) => n + (Number(it.qty) || 0), 0);
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td style="font-weight:600;">${cfgEsc(r.id)}</td>
      <td>${cfgEsc(r.requestType)}</td>
      <td>${cfgEsc(r.originalOrderID)}</td>
      <td>${cfgEsc(r.returnLocation)}</td>
      <td>${qtySummary} unit(s) across ${r.items.length} line(s)</td>
      <td>${r.refundEligible ? r.refundEligible.toFixed(2) : '-'}</td>
      <td>${cfgEsc(r.status)}</td>
      <td>${actions.map(a => returnActionButton(r.id, a)).join(' ') || '<span style="color:var(--text-muted);">-</span>'}</td>
    `;
    body.appendChild(tr);
  });
}

function returnActionButton(returnID, action) {
  const labels = {
    'approve': 'Approve', 'reject': 'Reject', 'receive': 'Receive Goods',
    'qc': 'Inspect', 'refund': 'Refund', 'reverse-pickup': 'Book Pickup'
  };
  const cls = action === 'reject' ? 'action-btn action-btn-danger' : 'action-btn';
  return `<button class="${cls}" ${actionAttrs('runReturnAction', [returnID, action])}>${labels[action] || action}</button>`;
}

async function runReturnAction(returnID, action) {
  const row = returnsViewRows.find(r => r.id === returnID);
  if (!row) return;
  try {
    if (action === 'qc') {
      await openReturnQCDialog(row);
    } else if (action === 'refund') {
      await runReturnRefundFlow(row);
    } else if (action === 'reject') {
      const reason = await showCustomPrompt(`Why is return ${returnID} being rejected?`, '', 'Reject Return');
      if (!reason) return;
      await postReturnAction(`/api/v1/returns/${encodeURIComponent(returnID)}/reject`, { reason_code: reason.trim() });
    } else if (action === 'reverse-pickup') {
      const pincode = await showCustomPrompt('Pickup PIN code for the reverse shipment:', '', 'Book Reverse Pickup');
      if (!pincode) return;
      await postReturnAction(`/api/v1/returns/${encodeURIComponent(returnID)}/reverse-pickup`, { pickup_pincode: pincode.trim() });
    } else {
      await postReturnAction(`/api/v1/returns/${encodeURIComponent(returnID)}/${action}`, {});
    }
  } finally {
    await loadReturnsView();
  }
}

async function postReturnAction(url, body) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return null;
  if (!res.ok) {
    await showApiError(res, 'That step could not be completed.');
    return null;
  }
  return res.json().catch(() => ({}));
}

// openReturnQCDialog is the inspection step: a disposition per line, and
// optionally an exchange SKU. This is the one place a human decides whether
// goods actually came back sellable - which is why the refund total is
// computed by the SERVER from these dispositions and shown afterwards, never
// typed here.
async function openReturnQCDialog(row) {
  const dispositions = {};
  const exchanges = {};
  for (const item of row.items) {
    const choice = await showCustomPrompt(
      `Disposition for ${item.sku} (${item.qty} unit(s)).\nOne of: ${RETURN_DISPOSITIONS.join(', ')}`,
      'Sellable', `Inspect ${row.id}`);
    if (!choice) return;
    const normalized = RETURN_DISPOSITIONS.find(d => d.toLowerCase() === choice.trim().toLowerCase());
    if (!normalized) {
      await showCustomAlert(`"${choice}" is not a valid disposition. Nothing was inspected.`, 'Invalid Disposition');
      return;
    }
    dispositions[item.sku] = normalized;
    if (normalized === 'Sellable' || normalized === 'Repairable' || normalized === 'Damaged') {
      const exchangeSKU = await showCustomPrompt(
        `Exchange ${item.sku} for a different SKU? Leave blank to refund instead.`, '', 'Exchange (optional)');
      if (exchangeSKU && exchangeSKU.trim()) exchanges[item.sku] = exchangeSKU.trim();
    }
  }
  const payload = { dispositions };
  if (Object.keys(exchanges).length) payload.exchange_for = exchanges;
  const result = await postReturnAction(`/api/v1/returns/${encodeURIComponent(row.id)}/qc`, payload);
  if (!result) return;
  const refund = Number(result.total_refund) || 0;
  await showCustomAlert(
    refund > 0
      ? `Inspection recorded. A refund of ${refund.toFixed(2)} is now pending approval${result.refund_request_id ? ` as ${result.refund_request_id}` : ''}.`
      : 'Inspection recorded. Nothing on this return is refundable, so it is now closed.',
    'Inspection Complete');
}

// runReturnRefundFlow walks the RefundRequest's own two steps - approve, then
// process - rather than collapsing them, because they are deliberately two
// decisions: whether the money is owed, and paying it.
async function runReturnRefundFlow(row) {
  const res = await apiFetch(`/api/v1/doc/RefundRequest?limit=200`);
  if (!res || !res.ok) {
    await showCustomAlert('The refund for this return could not be looked up.', 'Refund');
    return;
  }
  const payload = await res.json();
  const docs = Array.isArray(payload) ? payload : (payload.data || payload.documents || []);
  const refund = docs.map(d => ({ id: d.id, ...(d.data || d) }))
    .find(d => d.return_request_id === row.id && d.status !== 'Rejected' && d.status !== 'Processed');
  if (!refund) {
    await showCustomAlert('No refund is pending for this return.', 'Refund');
    return;
  }
  if (refund.status === 'Pending') {
    const ok = await showCustomConfirm(`Approve a refund of ${Number(refund.amount).toFixed(2)} for return ${row.id}?`, 'Approve Refund');
    if (!ok) return;
    if (!await postReturnAction(`/api/v1/refunds/${encodeURIComponent(refund.id)}/approve`, {})) return;
  }
  const method = await showCustomPrompt('How is the refund being paid (Cash / Card / UPI / Store Credit)?', 'Cash', 'Process Refund');
  if (!method) return;
  if (await postReturnAction(`/api/v1/refunds/${encodeURIComponent(refund.id)}/process`, { refund_method: method.trim() })) {
    await showCustomAlert(`Refund ${refund.id} processed. The return is now closed and the tax on the returned goods has been reversed.`, 'Refund Processed');
  }
}

export { renderReturnsView, loadReturnsView, renderReturnsTable, returnActionButton, runReturnAction, postReturnAction, openReturnQCDialog, runReturnRefundFlow };
