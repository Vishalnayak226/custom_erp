// Items 324.1-324.4 (ask 324, 2026-11-28): stock requests. "Need a UI where
// store wh can request in ui & accept order & process & dispatch it. or auto
// trigger" - and "submenu of request reason or can be text like advance
// order", with the sourcing engine suggesting where the stock should come
// from (distance, what each location can spare, demand at both ends).
//
// Two pages, one job each:
//   stock-requests           - a location's own requests; New asks for stock.
//   stock-requests:incoming  - requests other locations sent here: Accept,
//                              Accept less or Reject, then Pack and Dispatch.
// A request is a transfer order (one per source); pack, dispatch, receive,
// cancel and details are the Stock Movement actions (view-transfers.js),
// which stay on this page afterwards.

const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const SCREEN = 'stock-request-screen';
const INCOMING = 'stock-request-incoming';

// The request's own words for the transfer statuses.
const SR_STATUS = {
  Draft: ['Draft', 'badge-secondary'], 'Pending Approval': ['Sent', 'badge-warning'], Approved: ['Accepted', 'badge-success'],
  Packed: ['Packed', 'badge-success'], Dispatched: ['On the way', 'badge-warning'], 'Partially Received': ['Part received', 'badge-warning'],
  Received: ['Received', 'badge-success'], Rejected: ['Rejected', 'badge-danger'], Cancelled: ['Cancelled', 'badge-danger'],
  Returned: ['Returned', 'badge-secondary']
};

let srLines = []; // the New page's lines: { sku, qty (number or ''), from }
let srPlan = null; // the last /sources answer for those lines
let srRows = []; // the list on screen
const srFilter = { mine: { state: 'open', loc: '' }, incoming: { state: 'open', loc: '' } };

const h = v => escapeHTMLText(v == null ? '' : String(v));
const units = n => `${Math.round(Number(n) || 0)}`;
const unitsText = n => `${units(n)} unit${units(n) === '1' ? '' : 's'}`;
const mayCreate = () => canDo('create', 'TransferOrder');
const mayUpdate = () => canDo('update', 'TransferOrder');
const pickAnyLocation = () => !!(state.permissions && state.permissions.isAdmin) || !sessionStore();

async function srPost(path, body) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return { ok: false, data: null, res };
  let data = null;
  try { data = await res.json(); } catch (e) { data = null; }
  return { ok: res.ok, data, res };
}

function srErrorText(data, fallback) {
  // The engine's own reason rides in detail; error is the catalog's line.
  return (data && (data.detail || data.error)) || fallback;
}

async function renderStockRequestsView(container) {
  // Pack, dispatch, receive, cancel and details come from Stock Movement.
  await loadViewModule('/view-transfers.js?v=1');
  const incoming = currentViewTab === 'incoming';
  const side = incoming ? 'incoming' : 'mine';
  const f = srFilter[side];
  if (!f.loc && !pickAnyLocation()) f.loc = sessionStore();

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  list.innerHTML = `<div class="txn-list-bar">
      <select class="form-select" id="sr-state" aria-label="Show">
        <option value="open"${f.state === 'open' ? ' selected' : ''}>Open</option>
        <option value="closed"${f.state === 'closed' ? ' selected' : ''}>Closed</option>
        <option value=""${f.state === '' ? ' selected' : ''}>All</option>
      </select>
      ${pickAnyLocation() ? `<input type="text" class="form-input" id="sr-loc" value="${h(f.loc)}" placeholder="All locations" aria-label="Location" autocomplete="off">` : ''}
    </div>
    <div class="table-wrapper" id="sr-list-body"><p class="txn-empty">Loading…</p></div>`;

  let section;
  if (incoming) {
    section = txnPagedSection({ id: INCOMING, title: 'Requests to Fulfil', count: 0, list, forms: {} });
  } else {
    const auto = mayCreate() ? '<button type="button" class="btn btn-outline txn-landing-only" id="sr-auto">Auto request</button>' : '';
    section = txnPagedSection({ id: SCREEN, title: 'Stock Requests', page: 'New request', count: 0,
      newLabel: 'New Request', form: await stockRequestForm(), list, extra: auto });
    if (!mayCreate()) section.querySelectorAll('.page-section-actions .btn-primary').forEach(b => b.remove());
  }
  container.appendChild(section);
  txnRestorePane(incoming ? INCOMING : SCREEN);

  list.querySelector('#sr-state').addEventListener('change', e => { f.state = e.target.value; refreshStockRequestList(); });
  const locEl = list.querySelector('#sr-loc');
  if (locEl) {
    attachLinkTypeahead(locEl, 'Location', { noSetupHint: true });
    locEl.addEventListener('change', () => { f.loc = locEl.value.trim(); refreshStockRequestList(); });
  }
  section.querySelector('#sr-auto')?.addEventListener('click', runAutoStockRequest);
  if (!incoming) wireStockRequestForm();
  await refreshStockRequestList();
}

// ---------------------------------------------------------------------------
// The list
// ---------------------------------------------------------------------------
async function refreshStockRequestList() {
  const incoming = currentViewTab === 'incoming';
  const side = incoming ? 'incoming' : 'mine';
  const f = srFilter[side];
  const body = document.getElementById('sr-list-body');
  if (!body) return;
  const q = new URLSearchParams({ side, state: f.state });
  if (f.loc) q.set('location', f.loc);
  const res = await apiFetch(`/api/v1/stock-requests?${q}`);
  if (!res) return;
  if (!res.ok) {
    body.innerHTML = '<p class="txn-empty">Requests could not be loaded.</p>';
    return;
  }
  srRows = (await res.json()) || [];
  const count = document.querySelector(`#${incoming ? INCOMING : SCREEN} .txn-count`);
  if (count) count.textContent = srRows.length;
  if (!srRows.length) {
    body.innerHTML = `<p class="txn-empty">${incoming ? 'Nothing to send.' : 'No requests yet.'}</p>`;
    return;
  }
  body.innerHTML = `<table>
    <thead><tr><th>Request</th><th>${incoming ? 'For' : 'From'}</th><th>Items</th><th>Reason</th><th>Needed by</th><th>Status</th><th></th></tr></thead>
    <tbody>${srRows.map(r => stockRequestRow(r, incoming)).join('')}</tbody>
  </table>`;
}

function stockRequestRow(r, incoming) {
  const [label, badge] = SR_STATUS[r.status] || [r.status, 'badge-secondary'];
  const live = (r.lines || []).filter(l => !l.cancelled);
  const lines = (r.lines || []).map(l => `<li>${h(l.name || l.sku)} × ${units(l.qty)}${l.requested && l.requested !== l.qty ? ` <span class="text-muted">(asked ${units(l.requested)})</span>` : ''}${l.cancelled ? ' <span class="text-muted">(not sent)</span>' : ''}</li>`).join('');
  const where = incoming ? (r.to_name || r.to) : (r.from_name || r.from);
  const no = r.request_no && r.request_no !== r.id ? `${h(r.request_no)} <span class="text-muted">· ${h(r.id)}</span>` : h(r.request_no || r.id);
  const reason = `${h(r.reason)}${r.origin === 'Auto' ? ' <span class="badge badge-secondary">Auto</span>' : ''}${r.note ? `<div class="text-muted">${h(r.note)}</div>` : ''}`;
  const status = `<span class="badge ${badge}" title="${h(r.status)}">${h(label)}</span>${r.accepted_less ? ' <span class="text-muted">less</span>' : ''}${r.rejected_reason ? `<div class="text-muted">${h(r.rejected_reason)}</div>` : ''}`;
  return `<tr>
    <td class="s-355794">${no}</td>
    <td>${h(where)}</td>
    <td><details><summary>${live.length} item${live.length === 1 ? '' : 's'} · ${unitsText(r.units)}</summary><ul>${lines}</ul></details></td>
    <td>${reason}</td>
    <td>${h(r.needed_by || '')}</td>
    <td>${status}</td>
    <td>${incoming ? incomingActions(r) : mineActions(r)}</td>
  </tr>`;
}

function srBtn(fn, args, label, danger) {
  return `<button class="action-btn${danger ? ' action-btn-danger' : ''}" ${actionAttrs(fn, args)}>${label}</button>`;
}

function mineActions(r) {
  const out = [];
  if (mayUpdate()) {
    if (r.status === 'Draft' || r.status === 'Pending Approval') out.push(srBtn('stockRequestTransfer', ['cancelTransferOrder', r.id], 'Cancel', true));
    if (r.status === 'Dispatched' || r.status === 'Partially Received') out.push(srBtn('stockRequestTransfer', ['receiveTransferOrder', r.id], 'Receive'));
  }
  out.push(srBtn('stockRequestTransfer', ['showTransferDetails', r.id], 'Details'));
  return out.join(' ');
}

function incomingActions(r) {
  const out = [];
  if (mayUpdate()) {
    if (r.status === 'Draft' || r.status === 'Pending Approval') {
      out.push(srBtn('stockRequestAccept', [r.id], 'Accept'), srBtn('stockRequestAcceptLess', [r.id], 'Accept less'), srBtn('stockRequestReject', [r.id], 'Reject', true));
    }
    if (r.status === 'Approved') out.push(srBtn('stockRequestTransfer', ['packTransferOrder', r.id], 'Pack'));
    if (r.status === 'Approved' || r.status === 'Packed') out.push(srBtn('stockRequestTransfer', ['dispatchTransferOrder', r.id], 'Dispatch'));
  }
  out.push(srBtn('stockRequestTransfer', ['showTransferDetails', r.id], 'Details'));
  return out.join(' ');
}

// stockRequestTransfer runs a Stock Movement action on one request: those
// actions read the transfer from state.docData, so it is loaded first.
async function stockRequestTransfer(fn, id) {
  const res = await apiFetch(`/api/v1/doc/TransferOrder/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The transfer could not be opened.', 'Stock request'); return; }
  state.docData = [await res.json()];
  if (typeof window[fn] === 'function') await window[fn](id);
}

async function stockRequestAccept(id) {
  const r = srRows.find(x => x.id === id);
  if (!r || !(await showCustomConfirm(`Send ${unitsText(r.units)} to ${r.to_name || r.to}?`, 'Accept request'))) return;
  const out = await srPost('/api/v1/stock-requests/accept', { id });
  if (!out.ok) { if (out.res) await showCustomAlert(srErrorText(out.data, 'The request was not accepted.'), 'Accept request'); return; }
  showToast('Accepted. Pack or dispatch it next.', { variant: 'success' });
  refreshStockRequestList();
}

function stockRequestAcceptLess(id) {
  const r = srRows.find(x => x.id === id);
  if (!r) return;
  const live = (r.lines || []).filter(l => !l.cancelled);
  const body = `<table><thead><tr><th>Item</th><th class="num">Asked</th><th class="num">Send</th></tr></thead><tbody>
    ${live.map((l, i) => `<tr><td>${h(l.name || l.sku)}</td><td class="num">${units(l.qty)}</td>
      <td class="num"><input type="number" class="form-input" min="0" max="${units(l.qty)}" value="${units(l.qty)}" data-sr-less="${i}" aria-label="Send quantity for ${h(l.sku)}"></td></tr>`).join('')}
  </tbody></table>`;
  stockRequestDialog({ title: `Accept less · ${r.request_no || r.id}`, body, submitLabel: 'Accept', onSubmit: async form => {
    const lines = live.map((l, i) => ({ sku: l.sku, qty: Number(form.querySelector(`[data-sr-less="${i}"]`).value || 0) }));
    const out = await srPost('/api/v1/stock-requests/accept', { id, lines });
    if (!out.ok) return srErrorText(out.data, 'The request was not accepted.');
    showToast('Accepted with less. Pack or dispatch it next.', { variant: 'success' });
    refreshStockRequestList();
    return '';
  } });
}

async function stockRequestReject(id) {
  const r = srRows.find(x => x.id === id);
  if (!r) return;
  const reason = await showCustomPrompt('Why can this not be sent?', '', `Reject ${r.request_no || r.id}`);
  if (reason === null) return;
  if (!reason.trim()) { await showCustomAlert('Give a reason to reject.', 'Reject request'); return; }
  const out = await srPost('/api/v1/stock-requests/reject', { id, reason: reason.trim() });
  if (!out.ok) { if (out.res) await showCustomAlert(srErrorText(out.data, 'The request was not rejected.'), 'Reject request'); return; }
  showToast('Rejected.', { variant: 'info' });
  refreshStockRequestList();
}

async function runAutoStockRequest() {
  const loc = (document.getElementById('sr-loc')?.value || '').trim() || sessionStore();
  if (!loc) { await showCustomAlert('Pick a location first.', 'Auto request'); return; }
  if (!(await showCustomConfirm(`Ask for everything ${loc} is short of by its reorder rules?`, 'Auto request'))) return;
  const out = await srPost('/api/v1/stock-requests/auto-run', { location: loc });
  if (!out.ok) { if (out.res) await showCustomAlert(srErrorText(out.data, 'The auto request did not run.'), 'Auto request'); return; }
  const lines = (out.data || []).map(a => `${a.name || a.location}: ${a.request ? `request ${a.request.request_no} (${(a.request.transfers || []).length} transfer${(a.request.transfers || []).length === 1 ? '' : 's'})` : (a.message || 'nothing to ask for')}`);
  await showCustomAlert(lines.join('\n') || 'Nothing to ask for.', 'Auto request');
  refreshStockRequestList();
}

// One small dialog (Accept less), built on demand and removed on close.
function stockRequestDialog({ title, body, submitLabel, onSubmit }) {
  document.getElementById('sr-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'sr-dialog';
  overlay.innerHTML = `
    <div class="modal-container" role="dialog" aria-modal="true" aria-labelledby="sr-dialog-title">
      <div class="modal-header">
        <h3 class="modal-title" id="sr-dialog-title">${h(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-sr-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button>
      </div>
      <form id="sr-dialog-form" novalidate>
        <div class="modal-body">${body}<div id="sr-dialog-error" class="login-error hidden"></div></div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" data-sr-close>Cancel</button>
          <button type="submit" class="btn btn-primary" id="sr-dialog-submit">${h(submitLabel)}</button>
        </div>
      </form>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-sr-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const form = overlay.querySelector('#sr-dialog-form');
  form.addEventListener('submit', async e => {
    e.preventDefault();
    const errEl = overlay.querySelector('#sr-dialog-error');
    errEl.classList.add('hidden');
    await guardAgainstDoubleSubmit(overlay.querySelector('#sr-dialog-submit'), 'Saving...', async () => {
      const message = await onSubmit(form);
      if (message) { errEl.textContent = message; errEl.classList.remove('hidden'); return; }
      close();
    });
  });
  requestAnimationFrame(() => overlay.classList.add('open'));
  overlay.querySelector('input, button[type=submit]')?.focus();
}

// ---------------------------------------------------------------------------
// New request: the reason menu (built-in, the client's own, or typed), the
// lines, and the sourcing engine's suggestion per line.
// ---------------------------------------------------------------------------
async function stockRequestForm() {
  const res = await apiFetch('/api/v1/stock-requests/reasons');
  const reasons = res && res.ok ? await res.json() : { built_in: [], codes: [] };
  const form = document.createElement('div');
  form.className = 'txn-screen';
  form.innerHTML = `
    <div class="txn-head">
      <div class="form-group">
        <label class="form-label" for="sr-for">For location</label>
        <input type="text" id="sr-for" class="form-input" autocomplete="off" value="${h(sessionStore())}"${pickAnyLocation() ? '' : ' readonly'}>
      </div>
      <div class="form-group">
        <label class="form-label" for="sr-reason">Reason</label>
        <select id="sr-reason" class="form-select">
          <option value="">Type a reason</option>
          <optgroup label="Common">${(reasons.built_in || []).map(r => `<option value="b:${h(r)}">${h(r)}</option>`).join('')}</optgroup>
          ${(reasons.codes || []).length ? `<optgroup label="Your reasons">${reasons.codes.map(c => `<option value="c:${h(c.code)}" title="${h(c.when_to_use || '')}">${h(c.label || c.code)}</option>`).join('')}</optgroup>` : ''}
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="sr-reason-text">Reason text</label>
        <input type="text" id="sr-reason-text" class="form-input" maxlength="120" placeholder="e.g. Advance order">
      </div>
      <div class="form-group">
        <label class="form-label" for="sr-needed">Needed by</label>
        <input type="date" id="sr-needed" class="form-input">
      </div>
      <div class="form-group">
        <label class="form-label" for="sr-note">Note</label>
        <input type="text" id="sr-note" class="form-input" maxlength="200">
      </div>
    </div>
    <div class="txn-entry">
      <div class="form-group txn-entry-main">
        <label class="form-label" for="sr-line-sku">Item</label>
        <input type="text" id="sr-line-sku" class="form-input">
      </div>
      <div class="form-group">
        <label class="form-label" for="sr-line-qty">Qty</label>
        <input type="number" id="sr-line-qty" class="form-input" min="1" placeholder="Suggested">
      </div>
      <button class="btn btn-outline" id="sr-add-line" type="button">Add line</button>
      <button class="btn btn-outline" id="sr-upload" type="button">Upload lines</button>
      <button class="btn btn-outline" id="sr-find" type="button">Find sources</button>
    </div>
    <div id="sr-plan-sum" class="txn-sum"></div>
    <div class="txn-grid"><div id="sr-lines"></div></div>
    <div class="txn-actions">
      <div id="sr-error" class="login-error hidden"></div>
      <button class="btn btn-primary" id="sr-send" type="button">Send Request</button>
    </div>`;
  return form;
}

function wireStockRequestForm() {
  attachLinkTypeahead(document.getElementById('sr-line-sku'), 'Item');
  const forEl = document.getElementById('sr-for');
  if (!forEl.readOnly) attachLinkTypeahead(forEl, 'Location');
  forEl.addEventListener('change', () => { srPlan = null; renderStockRequestLines(); });
  const reasonEl = document.getElementById('sr-reason');
  const textEl = document.getElementById('sr-reason-text');
  // A built-in reason needs no text; the client's own code may take one.
  const syncReason = () => {
    const builtIn = reasonEl.value.startsWith('b:');
    textEl.disabled = builtIn;
    if (builtIn) textEl.value = '';
    textEl.placeholder = reasonEl.value.startsWith('c:') ? 'Optional' : builtIn ? '' : 'e.g. Advance order';
  };
  reasonEl.addEventListener('change', syncReason);
  syncReason();
  document.getElementById('sr-add-line').addEventListener('click', addStockRequestLine);
  document.getElementById('sr-line-qty').addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); addStockRequestLine(); } });
  document.getElementById('sr-upload').addEventListener('click', async () => {
    const pickers = await loadViewModule('/view-pickers.js?v=1');
    pickers.openLineUpload({ title: 'Upload request lines', template: 'stock-transfer', onRows: rows => {
      rows.forEach(r => mergeStockRequestLine(r.sku, Math.round(r.qty) || ''));
      srPlan = null;
      renderStockRequestLines();
    } });
  });
  document.getElementById('sr-find').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Finding...', findStockRequestSources));
  document.getElementById('sr-send').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Sending...', sendStockRequest));
  renderStockRequestLines();
}

function mergeStockRequestLine(sku, qty) {
  const line = srLines.find(l => l.sku === sku);
  if (!line) { srLines.push({ sku, qty, from: '' }); return; }
  if (qty === '' || line.qty === '') line.qty = qty === '' ? line.qty : qty;
  else line.qty += qty;
}

function addStockRequestLine() {
  const skuEl = document.getElementById('sr-line-sku');
  const qtyEl = document.getElementById('sr-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku) return;
  mergeStockRequestLine(sku, qty > 0 ? qty : '');
  skuEl.value = '';
  qtyEl.value = '';
  srPlan = null;
  renderStockRequestLines();
  skuEl.focus();
}

window.removeStockRequestLine = function (idx) {
  srLines.splice(idx, 1);
  srPlan = null;
  renderStockRequestLines();
};

function planLine(sku) {
  return srPlan && (srPlan.lines || []).find(l => l.sku === sku);
}

function distanceText(o) {
  if (o.distance_km >= 0) return `${Math.round(o.distance_km)} km`;
  return o.distance_note || '';
}

function renderStockRequestLines() {
  const el = document.getElementById('sr-lines');
  if (!el) return;
  const sum = document.getElementById('sr-plan-sum');
  if (sum) {
    const ships = (srPlan && srPlan.shipments) || [];
    sum.innerHTML = !srLines.length ? '<span>Add an item to start.</span>'
      : ships.length ? `<span>Ships from</span>${ships.map(s => `<span><b>${h(s.from_name || s.from)}</b> ${s.lines} line${s.lines === 1 ? '' : 's'} · ${unitsText(s.units)}${distanceText(s) ? ` · ${h(distanceText(s))}` : ''}</span>`).join('')}`
        : `<span><b>${srLines.length}</b> line${srLines.length === 1 ? '' : 's'}</span><span>Find sources to see where it comes from.</span>`;
  }
  if (!srLines.length) { el.innerHTML = '<p class="txn-empty">No lines added yet.</p>'; return; }
  el.innerHTML = `<table>
    <thead><tr><th>Item</th><th class="num">Qty</th>${srPlan ? '<th class="num">Suggested</th><th class="num">Here</th><th>From</th><th>Why</th>' : ''}<th></th></tr></thead>
    <tbody>${srLines.map((line, idx) => {
      const p = planLine(line.sku);
      const opts = (p && p.options) || [];
      const pick = p ? (p.plan || []).map(x => `${h(x.from_name || x.from)} ${units(x.qty)}`).join(', ') : '';
      const best = opts.find(o => (p.plan || []).some(x => x.from === o.location)) || opts[0];
      const why = p ? `${p.short > 0 ? `<span class="badge badge-danger">Short ${units(p.short)}</span> ` : ''}${pick ? `<b>${pick}</b>` : (opts.length ? '' : 'No location has it to spare')}
        ${best && best.why && best.why.length ? `<div class="text-muted">${h(best.why.join(' · '))}</div>` : ''}
        ${opts.length ? `<details><summary>${opts.length} source${opts.length === 1 ? '' : 's'}</summary><table>
          <thead><tr><th>Location</th><th class="num">Spare</th><th class="num">Has</th><th>Distance</th><th class="num">Cover</th><th class="num">Score</th></tr></thead>
          <tbody>${opts.map(o => `<tr title="${h((o.why || []).join(' · '))}"><td>${h(o.name || o.location)}</td><td class="num">${units(o.spare)}</td><td class="num">${units(o.available)}</td><td>${h(distanceText(o))}</td><td class="num">${o.cover_days >= 0 ? `${Math.round(o.cover_days)} d` : '-'}</td><td class="num">${Math.round(o.score)}</td></tr>`).join('')}</tbody>
        </table></details>` : ''}` : '';
      const from = p ? `<select class="form-select" data-sr-from="${idx}" aria-label="Source for ${h(line.sku)}">
          <option value="">Best</option>${opts.map(o => `<option value="${h(o.location)}"${o.location === line.from ? ' selected' : ''}>${h(o.name || o.location)} · ${units(o.spare)} spare</option>`).join('')}
        </select>` : '';
      return `<tr>
        <td data-link-doctype="Item" data-link-ref="${h(line.sku)}">${h(p && p.name ? `${p.name} (${line.sku})` : line.sku)}</td>
        <td class="num"><input type="number" class="form-input" min="1" value="${line.qty === '' ? '' : units(line.qty)}" placeholder="${p && p.suggested ? units(p.suggested) : 'Suggested'}" data-sr-qty="${idx}" aria-label="Quantity for ${h(line.sku)}"></td>
        ${srPlan ? `<td class="num">${p ? units(p.suggested) : ''}</td><td class="num">${p ? units(p.have_here) : ''}</td><td>${from}</td><td>${why}</td>` : ''}
        <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeStockRequestLine', [idx])}>Remove</button></td>
      </tr>`;
    }).join('')}</tbody>
  </table>`;
  el.querySelectorAll('[data-sr-qty]').forEach(inp => inp.addEventListener('change', () => {
    const n = parseInt(inp.value, 10);
    srLines[Number(inp.dataset.srQty)].qty = n > 0 ? n : '';
    if (srPlan) findStockRequestSources();
  }));
  el.querySelectorAll('[data-sr-from]').forEach(sel => sel.addEventListener('change', () => {
    srLines[Number(sel.dataset.srFrom)].from = sel.value;
    findStockRequestSources();
  }));
}

function stockRequestError(message) {
  const el = document.getElementById('sr-error');
  if (!el) return;
  el.textContent = message || '';
  el.classList.toggle('hidden', !message);
}

async function findStockRequestSources() {
  stockRequestError('');
  if (!srLines.length) { stockRequestError('Add an item first.'); return false; }
  const location = document.getElementById('sr-for').value.trim();
  const out = await srPost('/api/v1/stock-requests/sources', {
    location, lines: srLines.map(l => ({ sku: l.sku, qty: Number(l.qty) || 0, from: l.from || '' }))
  });
  if (!out.ok) { stockRequestError(srErrorText(out.data, 'Sources could not be found.')); return false; }
  srPlan = out.data;
  renderStockRequestLines();
  return true;
}

async function sendStockRequest() {
  stockRequestError('');
  if (!srLines.length) { stockRequestError('Add an item first.'); return; }
  // A blank quantity takes the suggestion, which needs the sources first.
  if (srLines.some(l => l.qty === '') && !srPlan && !(await findStockRequestSources())) return;
  const lines = srLines.map(l => ({ sku: l.sku, qty: Number(l.qty) || (planLine(l.sku) || {}).qty || 0, from: l.from || '' }));
  const none = lines.find(l => l.qty < 1);
  if (none) { stockRequestError(`Give a quantity for ${none.sku}: nothing is suggested.`); return; }
  const pick = document.getElementById('sr-reason').value;
  const typed = document.getElementById('sr-reason-text').value.trim();
  const payload = {
    location: document.getElementById('sr-for').value.trim(),
    reason_code: pick.startsWith('c:') ? pick.slice(2) : '',
    reason: pick.startsWith('b:') ? pick.slice(2) : typed,
    note: document.getElementById('sr-note').value.trim(),
    needed_by: document.getElementById('sr-needed').value,
    lines
  };
  if (!payload.reason_code && !payload.reason) { stockRequestError('Pick a reason or type one.'); return; }
  const out = await srPost('/api/v1/stock-requests', payload);
  if (!out.ok) { stockRequestError(srErrorText(out.data, 'The request was not sent.')); return; }
  const r = out.data || {};
  const sent = (r.transfers || []).map(t => `From ${t.from}: ${t.lines} line${t.lines === 1 ? '' : 's'}, ${unitsText(t.units)}${t.note ? ` (${t.note})` : ''}`);
  const left = (r.unplaced || []).map(l => `${l.sku}: ${units(l.qty)}`);
  srLines = [];
  srPlan = null;
  txnDone(SCREEN);
  await showCustomAlert([`Request ${r.request_no || ''} sent.`, ...sent, ...(left.length ? ['', 'No location can spare:', ...left] : [])].join('\n'), 'Stock request');
  renderView('stock-requests');
}

export {
  renderStockRequestsView,
  stockRequestTransfer,
  stockRequestAccept,
  stockRequestAcceptLess,
  stockRequestReject
};
