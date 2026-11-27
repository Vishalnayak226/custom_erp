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
  // 267.1: a booked pickup still waits for the goods; the server receives
  // from Approved or Pickup Scheduled, so the row offers Receive Goods.
  'Pickup Scheduled': ['receive'],
  'Received': ['qc'],
  'QC Complete': ['refund'],
  'Closed': [],
  'Rejected': []
};

const RETURN_DISPOSITIONS = ['Sellable', 'Damaged', 'Repairable', 'Missing', 'Wrong-Item', 'Rejected'];

// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnStockFromTo, mountStockFromTo, readStockFromTo } = await import('./txn-screen.js' + new URL(import.meta.url).search);

// 267.1: the work queue is paged on the server. Each filter is the status
// group the list is asked for (a repeated status filter matches any of them);
// Open is every status that still has a step to take.
const RETURN_STATUS_GROUPS = {
  open: Object.keys(RETURN_ACTIONS_BY_STATUS).filter(s => RETURN_ACTIONS_BY_STATUS[s].length),
  all: [],
  'Requested': ['Requested'],
  'Approved': ['Approved', 'Pickup Scheduled'],
  'Received': ['Received'],
  'QC Complete': ['QC Complete']
};
const RETURNS_PAGE = 50;

let returnsViewRows = [];
let returnsViewFilter = 'open';
let returnsViewSearch = '';
let returnsViewOffset = 0;
let returnsViewTotal = 0;
let returnsViewEstimated = false;
let returnsViewFailed = false;
let returnsViewLoad = 0; // a newer load wins over a slower older one

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
        <input id="returns-search" class="form-input" type="search" placeholder="Search" aria-label="Search returns" autocomplete="off">
        <button class="btn btn-outline btn-sm" id="returns-refresh-btn" type="button">Refresh</button>
        <button class="btn btn-primary btn-sm" id="returns-new-btn" type="button">+ New Return</button>
      </div>
    </div>
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
    <div class="s-493a8f txn-pager">
      <span id="returns-range" class="s-3330ed"></span>
      <button class="btn btn-outline btn-sm" id="returns-prev" type="button" disabled>Previous</button>
      <button class="btn btn-outline btn-sm" id="returns-next" type="button" disabled>Next</button>
    </div>
  `;
  root.appendChild(panel);

  // 267.1: the filter and search stay as they were left; the page starts
  // again at the first.
  returnsViewOffset = 0;
  const filterEl = document.getElementById('returns-filter');
  const searchEl = document.getElementById('returns-search');
  filterEl.value = returnsViewFilter;
  if (filterEl.value !== returnsViewFilter) { returnsViewFilter = 'open'; filterEl.value = 'open'; }
  searchEl.value = returnsViewSearch;
  let searchTimer = null;
  searchEl.addEventListener('input', () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      returnsViewSearch = searchEl.value.trim();
      returnsViewOffset = 0;
      loadReturnsView();
    }, 300);
  });
  document.getElementById('returns-prev').addEventListener('click', () => {
    returnsViewOffset = Math.max(0, returnsViewOffset - RETURNS_PAGE);
    loadReturnsView();
  });
  document.getElementById('returns-next').addEventListener('click', () => {
    returnsViewOffset += RETURNS_PAGE;
    loadReturnsView();
  });
  document.getElementById('returns-refresh-btn').addEventListener('click', loadReturnsView);
  // Stage 55.5: a return is raised at the till, against the original bill -
  // + New Return opens the till on its Return tab.
  document.getElementById('returns-new-btn').addEventListener('click', async () => {
    await renderView('pos');
    window.txnShowPane?.('pos-screen:return');
  });
  filterEl.addEventListener('change', (e) => {
    returnsViewFilter = e.target.value;
    returnsViewOffset = 0;
    loadReturnsView();
  });
  await loadReturnsView();
}

async function loadReturnsView() {
  const mine = ++returnsViewLoad;
  // Read through the ordinary generic doctype endpoint rather than a bespoke
  // list route: ReturnRequest is a registered doctype and already carries the
  // role/scope filtering every other document read gets, so a second endpoint
  // would be a second place for that filtering to drift.
  // 267.1: the status group, the search and the page go to the server, so a
  // return past the first page is found and the count is the whole result.
  const params = new URLSearchParams({ limit: String(RETURNS_PAGE), offset: String(returnsViewOffset), count: 'true' });
  (RETURN_STATUS_GROUPS[returnsViewFilter] || []).forEach(s => params.append('status', s));
  if (returnsViewSearch) params.set('q', returnsViewSearch);
  const res = await apiFetch('/api/v1/doc/ReturnRequest?' + params.toString());
  if (mine !== returnsViewLoad || !res) return;
  const errorEl = document.getElementById('returns-error');
  if (!res.ok) {
    const message = await getErrorMessage(res, 'Returns could not be loaded.');
    if (mine !== returnsViewLoad) return;
    // A failed load is said as such - never shown as "no returns".
    returnsViewFailed = true;
    returnsViewRows = [];
    returnsViewTotal = 0;
    if (errorEl) {
      errorEl.textContent = message;
      errorEl.classList.remove('hidden');
    }
    renderReturnsTable();
    return;
  }
  const payload = await res.json();
  if (mine !== returnsViewLoad) return;
  const docs = Array.isArray(payload) ? payload : (payload.data || payload.documents || []);
  // A step that closed the last row of a later page steps back a page.
  if (!docs.length && returnsViewOffset > 0) {
    returnsViewOffset = Math.max(0, returnsViewOffset - RETURNS_PAGE);
    return loadReturnsView();
  }
  const counted = Number(res.headers.get('X-Total-Count'));
  returnsViewTotal = Number.isFinite(counted) && res.headers.has('X-Total-Count') ? counted : returnsViewOffset + docs.length;
  returnsViewEstimated = res.headers.get('X-Total-Count-Estimated') === 'true';
  returnsViewFailed = false;
  if (errorEl) errorEl.classList.add('hidden');
  returnsViewRows = docs.map(d => {
    const data = d.data || d;
    return {
      id: d.id || data.code,
      number: data.code || d.id, // Stage 76.11: the SR series number; id stays the key
      requestType: data.request_type || '',
      reason: data.reason || data.reason_code || '', // Stage 148.28
      drafts: (Array.isArray(data.drafts) ? data.drafts : []).filter(x => x.id).length,
      originalOrderID: data.original_order_id || '',
      returnLocation: data.return_location || '',
      // Stage 77.8: a return booked at the selling store but taken here, and
      // the transfers that bring its goods here.
      collectedAt: data.collected_at || '',
      transfers: (Array.isArray(data.transfer_orders) ? data.transfer_orders : []).map(x => x.number || x.id).filter(Boolean),
      transferState: [data.transfer_state, data.transfer_note].filter(Boolean).join(' · '),
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

  // 267.1: the server has already filtered and paged; this is one page.
  const rows = returnsViewRows;
  const totalText = returnsViewEstimated ? `about ${Number(returnsViewTotal.toPrecision(2)).toLocaleString()}` : String(returnsViewTotal);

  // Stage 68: the bar's summary - the whole result's count, and the refund
  // of the rows on this page.
  const sumEl = document.getElementById('returns-sum');
  if (sumEl) {
    const refund = rows.reduce((n, r) => n + (r.refundEligible || 0), 0);
    sumEl.innerHTML = returnsViewFailed ? '' :
      `<span><b>${totalText}</b> return${returnsViewTotal === 1 ? '' : 's'}</span><span class="txn-total" title="Refund eligible · this page">${refund.toFixed(2)}</span>`;
  }

  const emptyEl = document.getElementById('returns-empty');
  if (emptyEl) {
    emptyEl.textContent = rows.length || returnsViewFailed ? '' :
      (returnsViewFilter === 'all' && !returnsViewSearch ? 'No returns have been raised yet.' : 'No returns match this filter.');
  }

  const rangeEl = document.getElementById('returns-range');
  const prevEl = document.getElementById('returns-prev');
  const nextEl = document.getElementById('returns-next');
  const end = returnsViewOffset + rows.length;
  if (rangeEl) rangeEl.textContent = rows.length ? `${returnsViewOffset + 1}-${end} of ${totalText}` : '';
  if (prevEl) prevEl.disabled = returnsViewOffset === 0;
  if (nextEl) nextEl.disabled = returnsViewEstimated ? rows.length < RETURNS_PAGE : end >= returnsViewTotal;

  rows.forEach(r => {
    const actions = RETURN_ACTIONS_BY_STATUS[r.status] || [];
    const qtySummary = r.items.reduce((n, it) => n + (Number(it.qty) || 0), 0);
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td class="s-eed0f8">${cfgEsc(r.number)}</td>
      <td title="${cfgEsc(r.reason)}">${cfgEsc(r.requestType)}</td>
      <td>${cfgEsc(r.originalOrderID)}</td>
      <td>${cfgEsc(r.returnLocation)}${r.collectedAt ? `<span class="s-5872de" title="${cfgEsc([r.transfers.join(', '), r.transferState].filter(Boolean).join(' · '))}"> · taken at ${cfgEsc(r.collectedAt)}${r.transfers.length ? ` · ${cfgEsc(r.transfers.join(', '))}` : ''}${r.transferState.startsWith('Open') ? ' · transfer open' : ''}</span>` : ''}</td>
      <td>${qtySummary} unit(s) across ${r.items.length} line(s)</td>
      <td>${r.refundEligible ? r.refundEligible.toFixed(2) : (r.expectedRefund ? `<span title="Expected - the money this bill's lines were paid. Fixed at inspection.">${r.expectedRefund.toFixed(2)} expected</span>` : '-')}</td>
      <td>${cfgEsc(r.status)}${r.drafts ? ` · ${r.drafts} draft${r.drafts === 1 ? "" : "s"}` : ""}</td>
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
      await openReversePickupDialog(row);
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

// Stage 135.11: the pickup needs a courier as well as the PIN code - the old
// prompt sent only the PIN, so every booking was refused. A blank courier is
// picked by PIN code from the courier service areas, as on OMS -> Ship.
async function openReversePickupDialog(row) {
  const { posFormDialog } = await import('./view-pos-extras.js' + new URL(import.meta.url).search);
  const field = (id, label, input) => `<div class="form-group"><label class="form-label" for="${id}">${label}</label>${input}</div>`;
  const picked = await posFormDialog(`Book pickup for ${row.number}`, '',
    field('rp-pin', 'PIN code', '<input id="rp-pin" class="form-input" inputmode="numeric" maxlength="6" autocomplete="postal-code">')
    + field('rp-courier', 'Courier', '<input id="rp-courier" class="form-input" placeholder="Automatic">')
    + field('rp-name', 'Contact name', '<input id="rp-name" class="form-input">')
    + field('rp-address', 'Pickup address', '<textarea id="rp-address" class="form-input" rows="2"></textarea>'),
    (el) => ({
      pickup_pincode: el.querySelector('#rp-pin').value.trim(),
      provider: el.querySelector('#rp-courier').value.trim(),
      pickup_name: el.querySelector('#rp-name').value.trim(),
      pickup_address: el.querySelector('#rp-address').value.trim()
    }));
  if (!picked) return;
  if (!/^\d{6}$/.test(picked.pickup_pincode)) {
    await showCustomAlert('Enter the 6-digit PIN code - the courier is chosen by it.', 'Book Pickup');
    return;
  }
  const res = await apiFetch(`/api/v1/returns/${encodeURIComponent(row.id)}/reverse-pickup`, { method: 'POST', body: JSON.stringify(picked) });
  if (!res) return;
  if (!res.ok) {
    // 47.11.8a: the courier may already have this pickup - resolve it from its portal.
    const code = await res.clone().json().then(b => b && b.code, () => '');
    await showApiError(res, 'That step could not be completed.');
    if (code === 'INTEGR-4718') await resolveReturnPickup(row.id);
    return;
  }
  const result = await res.json().catch(() => ({}));
  showToast(`Pickup booked as ${result.booking_id}${result.awb ? `, AWB ${result.awb}` : ''}.`, { variant: 'success' });
}

async function resolveReturnPickup(returnID) {
  const res = await apiFetch(`/api/v1/doc/LogisticsBooking?q=${encodeURIComponent(returnID)}&q_fields=return_request_id&limit=5`);
  const rows = res && res.ok ? await res.json().catch(() => []) : [];
  const b = (Array.isArray(rows) ? rows : []).find(x => x.return_request_id === returnID && x.awb_booking_state === 'uncertain');
  if (!b) return;
  const { resolveCourierBooking } = await import('./view-oms-steps.js' + new URL(import.meta.url).search);
  await resolveCourierBooking(b);
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
  // 267.2: what was entered, kept between tries - an exchange item that
  // matches nothing, or a refusal (no stock to give), reopens the form as it
  // was instead of throwing the inspection away.
  let kept = null;
  for (;;) {
    const picked = await inspectReturnForm(posFormDialog, row, names, kept);
    if (!picked) return;
    kept = picked;
    const problem = await resolveExchangeItems(picked.lines);
    if (problem) {
      await showCustomAlert(problem, 'Exchange');
      continue;
    }
    const dispositions = {};
    const exchanges = {};
    picked.lines.forEach(p => {
      dispositions[p.sku] = p.disposition;
      if (p.code && RETURN_EXCHANGEABLE.includes(p.disposition)) exchanges[p.sku] = p.code;
    });
    const payload = { dispositions, ...picked.stockTo };
    if (Object.keys(exchanges).length) payload.exchange_for = exchanges;
    const res = await apiFetch(`/api/v1/returns/${encodeURIComponent(row.id)}/qc`, { method: 'POST', body: JSON.stringify(payload) });
    if (!res) return;
    if (!res.ok) {
      await showApiError(res, 'The inspection was not recorded.');
      continue;
    }
    const result = await res.json().catch(() => ({}));
    await showInspectionResult(result);
    return;
  }
}

// The dispositions that take the goods back - only those can be exchanged
// (engines/returns.go refuses the rest).
const RETURN_EXCHANGEABLE = ['Sellable', 'Repairable', 'Damaged'];

// inspectReturnForm shows the inspection form, filled from kept when it is
// reopened. Resolves { lines: [{ sku, disposition, code, text }], stockTo },
// or null on cancel. code is the exchange item picked from the list; text is
// what was typed or scanned, resolved on OK when nothing was picked.
function inspectReturnForm(posFormDialog, row, names, kept) {
  const html = row.items.map((it, i) => `<div class="form-group">
      <label class="form-label" for="qc-disposition-${i}">${cfgEsc(names[i])} (${cfgEsc(it.sku)}) - ${Number(it.qty) || 0} unit(s)</label>
      <select id="qc-disposition-${i}" class="form-input" data-qc-line="${i}">${RETURN_DISPOSITIONS.map(d => `<option value="${d}">${d}</option>`).join('')}</select>
      <input type="text" class="form-input s-fe7b49" data-qc-exchange="${i}" aria-label="Exchange ${cfgEsc(names[i])} for" placeholder="Exchange for: name or barcode (optional)">
      <div class="s-3650c6" data-qc-exchange-sku="${i}"></div>
    </div>`).join('');
  const title = `Inspect ${row.number}${row.originalOrderID ? ` · Bill ${row.originalOrderID}` : ''}`;
  // Stage 106.5: where the goods checked Sellable land, for this return only.
  return posFormDialog(title, '', txnStockFromTo('Sales Return') + html, (el) => ({
    stockTo: readStockFromTo(el),
    lines: row.items.map((it, i) => {
      const box = el.querySelector(`[data-qc-exchange="${i}"]`);
      return {
        sku: it.sku,
        disposition: el.querySelector(`[data-qc-line="${i}"]`).value,
        code: box.dataset.qcCode || '',
        text: box.value.trim()
      };
    })
  }), (el) => {
    mountStockFromTo(el, row.returnLocation || '').then(() => {
      Object.entries((kept && kept.stockTo) || {}).forEach(([side, v]) => {
        const s = el.querySelector(`select[data-stock-side="${side}"]`);
        if (s) s.value = v;
      });
    });
    row.items.forEach((it, i) => {
      const was = kept && kept.lines[i];
      const select = el.querySelector(`[data-qc-line="${i}"]`);
      const box = el.querySelector(`[data-qc-exchange="${i}"]`);
      const sku = el.querySelector(`[data-qc-exchange-sku="${i}"]`);
      if (was) select.value = was.disposition;
      // Typed text goes back as typed, before the picker takes the box over.
      if (was && !was.code) box.value = was.text;
      // 267.2: the shared Item picker, by name as well as code or barcode -
      // the PO's search (147.2).
      attachLinkTypeahead(box, 'Item', {
        filters: { q_fields: 'name,code,barcode,parent_product_code,family' },
        labelFn: d => (d.name && d.code && d.name !== d.code ? `${d.name} · ${d.code}` : (d.name || d.code || d.id || '')),
        noSetupHint: true
      });
      const commit = (code) => {
        if (code) box.dataset.qcCode = code; else delete box.dataset.qcCode;
        sku.textContent = code ? `SKU ${code}` : '';
      };
      if (was && was.code) { box.value = was.code; commit(was.code); }
      // The picker's pick and its exact-match resolve send a scripted change;
      // the browser's own change on leaving a typed box is not a pick.
      box.addEventListener('change', (e) => { if (!e.isTrusted) commit(box.value); });
      box.addEventListener('input', () => commit(''));
      // A scan ends with Enter: with no row highlighted in the list, the
      // scanned code or barcode is resolved straight away.
      box.addEventListener('keydown', async (e) => {
        if (e.key !== 'Enter' || box.dataset.qcCode) return;
        const text = box.value.trim();
        if (!text) return;
        e.preventDefault();
        const found = await findExchangeItem(text);
        if (found.doc && box.value.trim() === text) {
          box.value = found.doc.code;
          commit(found.doc.code);
          box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
        } else if (found.problem) {
          sku.textContent = found.problem;
        }
      });
      const offerExchange = () => {
        box.disabled = !RETURN_EXCHANGEABLE.includes(select.value);
      };
      select.addEventListener('change', offerExchange);
      offerExchange();
    });
  });
}

// findExchangeItem finds the one item a typed or scanned text names - its
// code, then its barcode, then its name, ignoring case. { doc } or { problem }.
async function findExchangeItem(text) {
  const res = await apiFetch(`/api/v1/doc/Item?q=${encodeURIComponent(text)}&q_fields=name,code,barcode&limit=50`);
  if (!res || !res.ok) return { problem: `"${text}" could not be looked up.` };
  const rows = await res.json().catch(() => []);
  const lower = text.toLowerCase();
  for (const field of ['code', 'barcode', 'name']) {
    const hits = (Array.isArray(rows) ? rows : []).filter(d => d.code && String(d[field] || '').toLowerCase() === lower);
    if (hits.length === 1) return { doc: hits[0] };
    if (hits.length > 1) return { problem: `More than one item is called "${text}". Pick it from the list.` };
  }
  return { problem: `No item matches "${text}".` };
}

// resolveExchangeItems turns each exchange box that holds text but no picked
// item into that item's code. Returns what is wrong, or '' when all resolve.
async function resolveExchangeItems(lines) {
  for (const line of lines) {
    if (!RETURN_EXCHANGEABLE.includes(line.disposition)) continue;
    if (!line.code && line.text) {
      const found = await findExchangeItem(line.text);
      if (found.problem) return found.problem;
      line.code = found.doc.code;
    }
    if (line.code && line.code === line.sku) return `${line.sku} is the item coming back. Pick a different item to give in exchange, or leave the box blank to refund it.`;
  }
  return '';
}

async function showInspectionResult(result) {
  // Stage 55.5: the QC endpoint answers total_refund_eligible - this read a
  // total_refund that is never sent, so every inspection said "nothing is
  // refundable" while the refund waited under Refund.
  const refund = Number(result.total_refund_eligible) || 0;
  // 267.12: name the refund by its number (RF/...), not its internal id.
  let refundNo = result.refund_request_id || '';
  if (refundNo) {
    const r = await apiFetch(`/api/v1/doc/RefundRequest/${encodeURIComponent(refundNo)}`).catch(() => null);
    const doc = r && r.ok ? await r.json().catch(() => null) : null;
    if (doc && doc.code) refundNo = doc.code;
  }
  await showCustomAlert(
    refund > 0 || refundNo
      ? `Inspection recorded. A refund of ${refund.toFixed(2)} is now pending approval${refundNo ? ` as ${refundNo}` : ''}.`
      : 'Inspection recorded. Nothing on this return is refundable, so it is now closed.',
    'Inspection Complete');
}

// runReturnRefundFlow walks the RefundRequest's own two steps - approve, then
// process - rather than collapsing them, because they are deliberately two
// decisions: whether the money is owed, and paying it.
async function runReturnRefundFlow(row) {
  // 215.14: this return's refunds only. The first 200 of all refunds (the
  // list is ordered by id) left a newer refund out once a business had more,
  // and Refund said "No refund is pending".
  const res = await apiFetch(`/api/v1/doc/RefundRequest?return_request_id=${encodeURIComponent(row.id)}&limit=50`);
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
