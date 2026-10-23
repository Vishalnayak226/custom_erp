// BLD-041: native ES module for the standalone Returns workbench.

// Stage 71: the Sales Return print comes from trade-docs.js, which sets
// window.printTransactionDocument when it loads.
import('./trade-docs.js' + new URL(import.meta.url).search).catch(() => null);
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

// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnStockFromTo, mountStockFromTo, readStockFromTo } = await import('./txn-screen.js' + new URL(import.meta.url).search);

let returnsViewRows = [];
let returnsViewFilter = 'open';

async function renderReturnsView(root) {
  // Stage 68: the frozen transaction layout (txn-screen.css) - the bar carries
  // who and where, the count and refund total, the filter; the list scrolls.
  const panel = document.createElement('div');
  panel.className = 'txn-screen';
  panel.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">Returns</h1>
      ${txnContext()}
      <div id="returns-sum" class="txn-sum"></div>
      <div class="txn-actions">
        <select id="returns-filter" class="form-input" aria-label="Show">
          <option value="open">Open returns</option>
          <option value="all">All returns</option>
          <option value="Requested">Awaiting approval</option>
          <option value="Approved">Awaiting goods</option>
          <option value="Received">Awaiting inspection</option>
          <option value="QC Complete">Awaiting refund</option>
        </select>
        <button class="btn btn-outline btn-sm" id="returns-refresh-btn" type="button">Refresh</button>
        <button class="btn btn-primary btn-sm" id="returns-new-btn" type="button">+ New Return</button>
      </div>
    </div>
    <div class="txn-hint"><span>A return moves Requested &rarr; Approved &rarr; Received &rarr; QC Complete &rarr; Closed.
      Stock is only received back at inspection, and no refund is paid until the goods have been inspected.</span></div>
    <div id="returns-error" class="login-error hidden"></div>
    <div class="txn-grid">
      <table>
        <thead>
          <tr>
            <th>Return</th><th>Type</th><th>Original Bill</th><th>Location</th>
            <th>Items</th><th>Refund Eligible</th><th>Status</th><th>Actions</th>
          </tr>
        </thead>
        <tbody id="returns-body"></tbody>
      </table>
      <div id="returns-empty" class="txn-empty"></div>
    </div>
  `;
  root.appendChild(panel);

  document.getElementById('returns-refresh-btn').addEventListener('click', loadReturnsView);
  // Stage 55.5: a return is raised at the till, against the original bill -
  // + New Return opens the till on its Return tab.
  document.getElementById('returns-new-btn').addEventListener('click', async () => {
    await renderView('pos');
    window.txnShowPane?.('pos-screen:return');
  });
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
      number: data.code || d.id, // Stage 76.11: the SR series number; id stays the key
      requestType: data.request_type || '',
      originalOrderID: data.original_order_id || '',
      returnLocation: data.return_location || '',
      status: data.status || d.status || '',
      items: Array.isArray(data.items) ? data.items : [],
      refundEligible: Number(data.total_refund_eligible) || 0,
      // Stage 55.5: before inspection the refund is not fixed yet, but each
      // line already carries its share of what the bill was paid in money
      // (Stage 73.1.1) - shown as the expected refund.
      expectedRefund: (Array.isArray(data.items) ? data.items : []).reduce((n, it) =>
        n + Object.entries(it.tenders || {}).reduce((m, [k, v]) => m + (k === 'Loyalty Points' ? 0 : Number(v) || 0), 0), 0) / 100
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

  // Stage 68: the bar's summary for what is shown.
  const sumEl = document.getElementById('returns-sum');
  if (sumEl) {
    const refund = rows.reduce((n, r) => n + (r.refundEligible || 0), 0);
    sumEl.innerHTML = `<span><b>${rows.length}</b> return${rows.length === 1 ? '' : 's'}</span><span class="txn-total" title="Refund eligible">${refund.toFixed(2)}</span>`;
  }

  const emptyEl = document.getElementById('returns-empty');
  if (emptyEl) {
    emptyEl.textContent = rows.length ? '' :
      (returnsViewRows.length ? 'No returns match this filter.' : 'No returns have been raised yet. Click + New Return to take one back at the till, against the original bill.');
  }

  rows.forEach(r => {
    const actions = RETURN_ACTIONS_BY_STATUS[r.status] || [];
    const qtySummary = r.items.reduce((n, it) => n + (Number(it.qty) || 0), 0);
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td style="font-weight:600;">${cfgEsc(r.number)}</td>
      <td>${cfgEsc(r.requestType)}</td>
      <td>${cfgEsc(r.originalOrderID)}</td>
      <td>${cfgEsc(r.returnLocation)}</td>
      <td>${qtySummary} unit(s) across ${r.items.length} line(s)</td>
      <td>${r.refundEligible ? r.refundEligible.toFixed(2) : (r.expectedRefund ? `<span title="Expected - the money this bill's lines were paid. Fixed at inspection.">${r.expectedRefund.toFixed(2)} expected</span>` : '-')}</td>
      <td>${cfgEsc(r.status)}</td>
      <td>${actions.map(a => returnActionButton(r.id, a)).join(' ')} <button class="action-btn" ${actionAttrs('printTransactionDocument', ['ReturnRequest', r.id])}>Print</button></td>
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
  // Stage 55.5: one form - each line by its item name, its condition picked
  // from a list (Sellable first), and an optional item to exchange it for -
  // instead of a typed word per line that a typo would throw away.
  const { posFormDialog } = await import('./view-pos-extras.js' + new URL(import.meta.url).search);
  const names = await Promise.all(row.items.map(it => apiFetch(`/api/v1/doc/Item/${encodeURIComponent(it.sku)}`)
    .then(r => (r && r.ok ? r.json() : null)).then(d => (d && d.name) || it.sku, () => it.sku)));
  const html = row.items.map((it, i) => `<div class="form-group">
      <label class="form-label" for="qc-disposition-${i}">${cfgEsc(names[i])} (${cfgEsc(it.sku)}) - ${Number(it.qty) || 0} unit(s)</label>
      <select id="qc-disposition-${i}" class="form-input" data-qc-sku="${cfgEsc(it.sku)}">${RETURN_DISPOSITIONS.map(d => `<option value="${d}">${d}</option>`).join('')}</select>
      <input type="text" class="form-input" style="margin-top:6px;" data-qc-exchange="${cfgEsc(it.sku)}" aria-label="Exchange ${cfgEsc(names[i])} for (item code, optional)" placeholder="Exchange for item code (optional - blank refunds it)">
    </div>`).join('');
  // Stage 106.5: where the goods checked Sellable land, for this return only.
  let stockTo = {};
  const dialog = posFormDialog(`Inspect ${row.number}`,
    'Record the condition of each item. Sellable goes back to stock; an exchange applies only to Sellable, Repairable or Damaged items.',
    txnStockFromTo('Sales Return') + html, (el) => {
      stockTo = readStockFromTo(el);
      return [...el.querySelectorAll('[data-qc-sku]')].map(s => ({
        sku: s.dataset.qcSku, disposition: s.value,
        exchange: (el.querySelector(`[data-qc-exchange="${CSS.escape(s.dataset.qcSku)}"]`) || {}).value || ''
      }));
    });
  mountStockFromTo(document.body, row.returnLocation || '');
  const picked = await dialog;
  if (!picked) return;
  const dispositions = {};
  const exchanges = {};
  picked.forEach(p => {
    dispositions[p.sku] = p.disposition;
    if (p.exchange.trim() && ['Sellable', 'Repairable', 'Damaged'].includes(p.disposition)) exchanges[p.sku] = p.exchange.trim();
  });
  const payload = { dispositions, ...stockTo };
  if (Object.keys(exchanges).length) payload.exchange_for = exchanges;
  const result = await postReturnAction(`/api/v1/returns/${encodeURIComponent(row.id)}/qc`, payload);
  if (!result) return;
  // Stage 55.5: the QC endpoint answers total_refund_eligible - this read a
  // total_refund that is never sent, so every inspection said "nothing is
  // refundable" while the refund waited under Refund.
  const refund = Number(result.total_refund_eligible) || 0;
  await showCustomAlert(
    refund > 0 || result.refund_request_id
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
  // Stage 73.1.6: a till sale's refund goes back the way the returned items
  // were paid - the split is fixed by the sale, so it is shown, not asked.
  const split = refundTenderSummary(refund);
  if (refund.status === 'Pending') {
    const ok = await showCustomConfirm(`Approve a refund of ${Number(refund.amount).toFixed(2)} for return ${row.number}?${split ? `\n\nIt goes back as: ${split}.` : ''}`, 'Approve Refund');
    if (!ok) return;
    if (!await postReturnAction(`/api/v1/refunds/${encodeURIComponent(refund.id)}/approve`, {})) return;
  }
  let method = '';
  // Stage 130.12 (74.2.2): the customer may choose store credit instead -
  // kept on their account for a later bill, never paid out as cash.
  const storeCreditOffer = async () => showCustomConfirm(`Give refund ${refund.code || refund.id} as the customer's store credit instead? It stays on their account for a later bill at the till, does not expire, and is never paid out as cash. Choose this only if the customer asks for it.`, 'Store Credit');
  if (split) {
    const ok = await showCustomConfirm(`Pay refund ${refund.code || refund.id} back the way the items were paid: ${split}.\n\nReverse any card or UPI part on the terminal or app; cash comes out of the drawer.\n\n(Cancel to give it as store credit instead.)`, 'Process Refund');
    if (!ok) {
      if (!(await storeCreditOffer())) return;
      method = 'Store Credit';
    }
  } else {
    method = await showCustomPrompt('How is the refund being paid (Cash / Card / UPI, or Store Credit if the customer asks for it)?', 'Cash', 'Process Refund');
    if (!method) return;
    if (/^store\s*credit$/i.test(method.trim())) method = 'Store Credit';
  }
  if (await postReturnAction(`/api/v1/refunds/${encodeURIComponent(refund.id)}/process`, { refund_method: method.trim() })) {
    await showCustomAlert(`Refund ${refund.code || refund.id} processed${method === 'Store Credit' ? ' as store credit on the customer\'s account' : ''}. The return is now closed and the tax on the returned goods has been reversed.`, 'Refund Processed');
  }
}

// refundTenderSummary reads a refund's per-tender split (Stage 73.1.6) as
// words, e.g. "Card 400.00, Cash 66.67, 100 points". Empty when the refund has
// no split (an online-order return), which is when the clerk is asked.
function refundTenderSummary(refund) {
  const t = refund.tenders || {};
  const parts = Object.keys(t).filter(k => k !== 'Loyalty Points' && Number(t[k]) > 0)
    .sort().map(k => `${k} ${(Number(t[k]) / 100).toFixed(2)}`);
  if (Number(refund.points) > 0) parts.push(`${refund.points} points back to the customer`);
  return parts.join(', ');
}

export { renderReturnsView, loadReturnsView, renderReturnsTable, returnActionButton, runReturnAction, postReturnAction, openReturnQCDialog, runReturnRefundFlow };
