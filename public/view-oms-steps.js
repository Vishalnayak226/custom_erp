// Stage 88.2: one OMS page per step - Pending Orders, Pick, Pack and Ship -
// opened from the OMS menu as oms:<step> (renderOMSWorkbenchView hands over
// here). Built on the APIs that already existed without a screen: the pick
// worklist, scan-pick / scan-pack, packages and the pack bill, courier booking,
// manifests and gate passes. Replaces the combined Fulfillment and Marketplace
// screens, which now redirect to Pick and Ship.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const again = (step) => renderView(`oms:${step}`);
const num = (v) => Number(v) || 0;
const wmsOn = () => typeof isMenuModuleVisible === 'function' && isMenuModuleVisible('wms');

// stepBar is the page's title row; its Refresh re-renders the same step.
async function stepBar(container, title, step, extra = '') {
  await import('./txn-screen.js' + Q); // its stylesheet carries .page-section-bar
  const bar = document.createElement('div');
  bar.className = 'page-section-bar';
  bar.innerHTML = `<h1 class="page-title">${esc(title)}</h1><span class="page-section-actions">${extra}<button type="button" class="btn btn-outline" data-oms-refresh>Refresh</button></span>`;
  bar.querySelector('[data-oms-refresh]').addEventListener('click', () => again(step));
  container.appendChild(bar);
  return bar;
}

function panel(container, html = '') {
  const el = document.createElement('div');
  el.className = 'table-panel oms-step-panel';
  el.innerHTML = html;
  container.appendChild(el);
  return el;
}

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

// post returns the response body, or { error } with the server's message -
// the step chains below report a failed step in place rather than in a dialog.
async function post(url, body) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return { error: 'No response from the server.' };
  if (!res.ok) return { error: await getErrorMessage(res, 'The server refused this step.') };
  try { return await res.json(); } catch (_) { return {}; }
}

function taskItems(t) {
  let items = t.items || [];
  if (typeof items === 'string') { try { items = JSON.parse(items); } catch (_) { items = []; } }
  return Array.isArray(items) ? items : [];
}
const lineLeft = (it) => Math.max(0, num(it.qty) - num(it.picked_qty) - num(it.short_qty));
const pickDone = (t) => taskItems(t).length > 0 && taskItems(t).every(it => lineLeft(it) === 0);
const packLeft = (t) => taskItems(t).reduce((n, it) => n + Math.max(0, num(it.picked_qty) - num(it.packed_qty)), 0);
const taskID = (t) => t.code || t.id;

async function ordersByID() {
  const rows = await getJSON('/api/v1/doc/SalesOrder?limit=500', []);
  const out = {};
  // A task holds the order's id; the order's code is its printed number.
  (rows || []).forEach(o => { out[o.id] = o; if (o.code) out[o.code] = o; });
  return out;
}

// A picker knows an order by who it is for; its number (and the task's id,
// when given) stay small, to quote.
function orderCell(orderID, o, task = '') {
  const small = (s) => s ? `<div class="oms-channel-ref oms-mono">${esc(s)}</div>` : '';
  if (!orderID) return small(task) || '—';
  if (!o) return `<span class="oms-mono">${esc(orderID)}</span>${small(task)}`;
  const ref = o.channel_order_id ? ` · ${o.channel_order_id}` : '';
  return `${esc(o.customer_name || orderID)}<div class="oms-channel-ref">${esc((o.channel || 'Manual') + ref)}</div>${small(o.code || orderID)}${small(task)}`;
}

function ageText(when) {
  const t = new Date(when).getTime();
  if (!t) return '—';
  const m = Math.max(0, Math.round((Date.now() - t) / 60000));
  return m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h` : `${Math.floor(m / 1440)}d`;
}

function orderPincode(o) {
  if (!o) return '';
  return String(o.shipping_pincode || String(o.shipping_address || '').match(/\b\d{6}\b/)?.[0] || '');
}

const emptyRow = (cols, text) => `<tr><td colspan="${cols}" class="oms-step-empty">${text}</td></tr>`;

function locationFilter(tasks, current) {
  const locs = [...new Set(tasks.map(t => t.location_code).filter(Boolean))].sort();
  if (locs.length < 2) return '';
  return `<label class="oms-facet"><span>Location</span><select class="form-input" data-oms-loc><option value="">All</option>${locs.map(l => `<option${l === current ? ' selected' : ''}>${esc(l)}</option>`).join('')}</select></label>`;
}

let stepLocation = '';
function wireLocationFilter(host, step) {
  host.querySelector('[data-oms-loc]')?.addEventListener('change', e => { stepLocation = e.target.value; again(step); });
}

// remember which task's scan panel is open, so a refresh comes back to it.
function openTask(step, id) {
  try { if (id) sessionStorage.setItem(`erp_oms_${step}_task`, id); else sessionStorage.removeItem(`erp_oms_${step}_task`); } catch (_) { /* storage off */ }
}
function openedTask(step) {
  try { return sessionStorage.getItem(`erp_oms_${step}_task`) || ''; } catch (_) { return ''; }
}

async function loadTask(id) {
  return getJSON(`/api/v1/doc/FulfillmentTask/${encodeURIComponent(id)}`, null);
}

// ---------------------------------------------------------------------------
// 88.2.2 Pending Orders - accept or reject, one or many.
// ---------------------------------------------------------------------------

const pendingSelected = new Set();

async function renderPending(container) {
  await stepBar(container, 'Pending Orders', 'pending');
  const host = panel(container);
  const result = await getJSON('/api/v1/oms/orders?status=Reserved&limit=200', null);
  if (!result) { renderErrorPanel(host, 'Failed to load pending orders.', () => again('pending')); return; }
  const rows = result.rows || [];
  const ids = new Set(rows.map(o => o.order_id));
  [...pendingSelected].forEach(id => { if (!ids.has(id)) pendingSelected.delete(id); });
  const owner = wmsOn();
  host.innerHTML = `
    <div class="bulk-edit-bar hidden" data-pending-bulk>
      <span data-pending-count></span>
      <button type="button" class="btn btn-primary btn-sm" data-pending-bulk-act="accept">Accept</button>
      <button type="button" class="btn btn-outline btn-sm" data-pending-bulk-act="reject">Reject</button>
    </div>
    <div class="table-wrapper"><table>
      <thead><tr><th style="width:32px;"><input type="checkbox" data-pending-all aria-label="Select every pending order"></th>
        <th>Order</th><th>Source</th><th>Customer</th><th class="num">Lines</th><th>Location</th><th>Waiting</th><th class="num">Value</th><th>Actions</th></tr></thead>
      <tbody>${rows.length === 0 ? emptyRow(9, 'No order is waiting. Channel and website orders land here once their stock is reserved; Accept sends them to Pick.') : rows.map(o => `<tr>
        <td><input type="checkbox" data-pending-pick="${esc(o.order_id)}" aria-label="Select order ${esc(o.order_number || o.order_id)}"${pendingSelected.has(o.order_id) ? ' checked' : ''}></td>
        <td class="oms-mono">${esc(o.order_number || o.order_id)}${o.priority === 'Expedite' ? '<div class="badge badge-warning">Expedite</div>' : ''}${o.acceptance_status === 'Accepted' ? '<div class="badge badge-secondary" title="Accepted before, then changed - accepting it again re-releases it without telling the channel twice">Changed after acceptance</div>' : ''}</td>
        <td>${esc(o.channel || 'Manual')}${o.channel_order_id ? `<div class="oms-channel-ref">${esc(o.channel_order_id)}</div>` : ''}</td>
        <td>${esc(o.customer_name || '—')}${o.customer_phone ? `<div class="oms-channel-ref">${esc(o.customer_phone)}</div>` : ''}</td>
        <td class="num">${esc(o.line_count)}</td>
        <td>${esc(o.locations || '—')}</td>
        <td title="${esc(o.created_at)}">${esc(ageText(o.created_at))}</td>
        <td class="num">${formatMoney(o.total_amount)}</td>
        <td class="oms-step-actions">
          <button type="button" class="btn btn-primary btn-sm" data-pending-act="accept" data-id="${esc(o.order_id)}">Accept</button>
          <button type="button" class="btn btn-outline btn-sm" data-pending-act="reject" data-id="${esc(o.order_id)}">Reject</button>
          ${owner ? `<button type="button" class="action-btn" data-pending-act="owner" data-id="${esc(o.order_id)}" title="The 3PL client whose stock this order is picked from">Owner</button>` : ''}
          <button type="button" class="action-btn" data-pending-act="open" data-id="${esc(o.order_id)}">Open</button>
        </td></tr>`).join('')}</tbody>
    </table></div>
    ${result.total > rows.length ? `<p class="text-muted oms-step-note">Showing the oldest ${rows.length} of ${result.total}; accept or reject these to see the rest.</p>` : ''}`;

  const syncBulk = () => {
    const bar = host.querySelector('[data-pending-bulk]');
    bar.classList.toggle('hidden', pendingSelected.size === 0);
    host.querySelector('[data-pending-count]').textContent = `${pendingSelected.size} selected`;
  };
  host.querySelectorAll('[data-pending-pick]').forEach(box => box.addEventListener('change', () => {
    if (box.checked) pendingSelected.add(box.dataset.pendingPick); else pendingSelected.delete(box.dataset.pendingPick);
    syncBulk();
  }));
  host.querySelector('[data-pending-all]').addEventListener('change', e => {
    host.querySelectorAll('[data-pending-pick]').forEach(box => {
      box.checked = e.target.checked;
      if (box.checked) pendingSelected.add(box.dataset.pendingPick); else pendingSelected.delete(box.dataset.pendingPick);
    });
    syncBulk();
  });
  host.querySelectorAll('[data-pending-bulk-act]').forEach(btn => btn.addEventListener('click', () =>
    decidePending(btn.dataset.pendingBulkAct, [...pendingSelected])));
  host.querySelectorAll('[data-pending-act]').forEach(btn => btn.addEventListener('click', async () => {
    const id = btn.dataset.id, act = btn.dataset.pendingAct;
    if (act === 'open') return window.openOMSOrderDetail(id);
    if (act === 'owner') return setOrderOwner(id);
    return decidePending(act, [id]);
  }));
  syncBulk();
  await renderPushProblems(container);
  await renderOnHold(container);
}

// 142.1: what a hold reason means to the person who has to clear it.
const HOLD_WORDS = {
  ALLOCATION_FAILED: 'No stock to reserve',
  PAYMENT_PENDING: 'Payment not confirmed',
  ADDR_INVALID: 'Address incomplete',
  SKU_MAPPING_FAILED: 'An item is not mapped'
};
const holdWords = (code) => HOLD_WORDS[code] || (code ? String(code).replace(/_/g, ' ').toLowerCase() : 'On hold');

// 142.1: the orders that arrived but could not be reserved - no stock, payment
// pending, a bad address. Without this they were on OMS → Orders only, and a
// channel order could "disappear" between arriving and being pickable.
async function renderOnHold(container) {
  const result = await getJSON('/api/v1/oms/orders?status=On%20Hold&limit=200', null);
  const rows = result ? (result.rows || []) : [];
  if (!result || rows.length === 0) return;
  const host = panel(container);
  host.innerHTML = `
    <div class="oms-console-head"><h2>On hold · ${rows.length}</h2></div>
    <p class="text-muted oms-step-note" style="margin:0 0 10px;">Arrived, but not reserved yet. Fix the cause (receive the stock, confirm the payment, complete the address) and click Retry; the order then comes to the list above.</p>
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Source</th><th>Customer</th><th class="num">Lines</th><th>Why</th><th>Waiting</th><th class="num">Value</th><th>Actions</th></tr></thead>
      <tbody>${rows.map(o => `<tr>
        <td class="oms-mono">${esc(o.order_number || o.order_id)}</td>
        <td>${esc(o.channel || 'Manual')}${o.channel_order_id ? `<div class="oms-channel-ref">${esc(o.channel_order_id)}</div>` : ''}</td>
        <td>${esc(o.customer_name || '—')}${o.customer_phone ? `<div class="oms-channel-ref">${esc(o.customer_phone)}</div>` : ''}</td>
        <td class="num">${esc(o.line_count)}</td>
        <td><span class="badge badge-warning" title="${esc(o.hold_reason || '')}">${esc(holdWords(o.hold_reason))}</span></td>
        <td title="${esc(o.created_at)}">${esc(ageText(o.created_at))}</td>
        <td class="num">${formatMoney(o.total_amount)}</td>
        <td class="oms-step-actions">
          <button type="button" class="btn btn-outline btn-sm" data-hold-act="retry" data-id="${esc(o.order_id)}" title="Try to reserve the stock again">Retry</button>
          <button type="button" class="action-btn" data-hold-act="open" data-id="${esc(o.order_id)}">Open</button>
        </td></tr>`).join('')}</tbody>
    </table></div>`;
  host.querySelectorAll('[data-hold-act]').forEach(btn => btn.addEventListener('click', async () => {
    const id = btn.dataset.id;
    if (btn.dataset.holdAct === 'open') return window.openOMSOrderDetail(id);
    btn.disabled = true;
    const r = await post(`/api/v1/orders/${encodeURIComponent(id)}/release-hold`, {});
    btn.disabled = false;
    if (r.error) { await showCustomAlert(r.error, 'Retry'); return; }
    if (r.order_status === 'On Hold') { await showCustomAlert(`Still on hold: ${holdWords(r.hold_reason)}.`, 'Retry'); return; }
    showToast('Reserved - the order is now in the list above.', { variant: 'success' });
    again('pending');
  }));
}

// 142.2: open a task by scanning or typing what the picker holds - the pick
// list number (the task), the order number or the channel's order number.
function loadOrderBox(container, step, tasks, orders, open) {
  const box = document.createElement('div');
  box.className = 'table-panel oms-step-panel';
  box.innerHTML = `
    <form class="oms-scan-row" data-load-form style="margin:0;">
      <input class="form-input" data-load-input autocomplete="off" placeholder="Scan or type the pick list, order or channel order number" aria-label="Load order">
      <button type="submit" class="btn btn-primary">Load</button>
    </form>
    <div class="oms-scan-msg" data-load-msg role="status"></div>`;
  container.appendChild(box);
  const input = box.querySelector('[data-load-input]');
  const msg = box.querySelector('[data-load-msg]');
  box.querySelector('[data-load-form]').addEventListener('submit', async e => {
    e.preventDefault();
    const v = input.value.trim().toUpperCase();
    if (!v) return;
    input.value = '';
    const same = (s) => String(s || '').trim().toUpperCase() === v;
    const hit = tasks.find(t => {
      const o = orders[t.order_id] || {};
      return same(taskID(t)) || same(t.id) || same(t.order_id) || same(o.code) || same(o.channel_order_id);
    });
    if (!hit) {
      msg.textContent = `${v} is not on this list. A ${step === 'pick' ? 'pick' : 'pack'} task appears here ${step === 'pick' ? 'once its order is accepted on Pending Orders' : 'once every line is picked on Pick'}.`;
      msg.classList.add('oms-scan-bad');
      input.focus();
      return;
    }
    msg.textContent = '';
    msg.classList.remove('oms-scan-bad');
    await open(taskID(hit));
  });
  if (!openedTask(step)) input.focus();
}

// Stage 88.3.1: Accept and Reject go through the Pending gate (the bulk
// route's accept / reject), which also sends the answer to the channel.
async function decidePending(act, ids) {
  if (ids.length === 0) return;
  let reasonCode = '';
  if (act === 'reject') {
    const { chooseReason } = await import('./reason-picker.js' + Q);
    const reason = await chooseReason({ category: 'Cancellation', title: ids.length > 1 ? `Reject ${ids.length} orders` : 'Reject order',
      question: 'Why is this order rejected? It is cancelled, its reserved stock is released and the channel is told.' });
    if (!reason) return;
    reasonCode = reason.code || reason.text;
  }
  const r = await post('/api/v1/oms/orders/bulk', { action: act, order_ids: ids, reason_code: reasonCode });
  if (r.error) { await showCustomAlert(r.error, act === 'accept' ? 'Accept' : 'Reject'); return; }
  reportDecision((r.succeeded || []).length, Object.entries(r.failed || {}).map(([id, why]) => `${id}: ${why}`),
    act === 'accept' ? 'accepted and sent to Pick' : 'rejected');
  ids.forEach(id => pendingSelected.delete(id));
  again('pending');
}

// The answers a channel did not get - a push that failed every retry, or a
// channel with no credentials yet - each with a Retry.
async function renderPushProblems(container) {
  const result = await getJSON('/api/v1/oms/orders?channel_push=problem&limit=100', null);
  const rows = (result && result.rows) || [];
  if (rows.length === 0) return;
  const host = panel(container, `<h2 class="oms-step-heading">Answers the channel did not get <span class="text-muted">(${rows.length})</span></h2>
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Channel</th><th>Answer</th><th>Push</th><th>Why</th><th></th></tr></thead>
      <tbody>${rows.map(o => `<tr><td class="oms-mono">${esc(o.order_number || o.order_id)}</td>
        <td>${esc(o.channel)}${o.channel_order_id ? `<div class="oms-channel-ref">${esc(o.channel_order_id)}</div>` : ''}</td>
        <td>${esc(o.push_decision)}</td><td><span class="badge badge-danger">${esc(o.push_status)}</span></td>
        <td class="oms-timeline-detail">${esc(o.push_error || '')}</td>
        <td><button type="button" class="btn btn-outline btn-sm" data-push-retry="${esc(o.order_id)}">Retry</button></td></tr>`).join('')}</tbody>
    </table></div>`);
  host.querySelectorAll('[data-push-retry]').forEach(btn => btn.addEventListener('click', async () => {
    const r = await post('/api/v1/oms/orders/bulk', { action: 'retry_push', order_ids: [btn.dataset.pushRetry] });
    const why = r.error || Object.values(r.failed || {})[0];
    if (why) { await showCustomAlert(why, 'Retry'); return; }
    showToast('Queued again - the channel gets it on the next run.', { variant: 'success' });
    again('pending');
  }));
}

function reportDecision(ok, refused, verb) {
  if (refused.length === 0) { showToast(`${ok} order(s) ${verb}.`, { variant: 'success' }); return; }
  showToast(`${ok} order(s) ${verb}; ${refused.length} refused. ${refused[0]}`, { variant: 'warning', duration: 9000 });
}

// Stage 59.2's 3PL owner, set before the order is picked (was a box on Fulfillment).
async function setOrderOwner(orderID) {
  const owner = await showCustomPrompt(`Owner (3PL client) of order ${orderID} - its stock is picked only from this owner. Leave blank to clear.`, '', 'Order owner');
  if (owner === null) return;
  const r = await post('/api/v1/wms/order-owner', { order_id: orderID, owner_id: owner.trim() });
  if (r.error) { await showCustomAlert(r.error, 'Order owner'); return; }
  showToast(owner.trim() ? `Order ${orderID}: owner ${owner.trim()}.` : `Order ${orderID}: owner cleared.`, { variant: 'success' });
}

// ---------------------------------------------------------------------------
// 88.2.3 Pick - the priority worklist, the pick list, scan-pick, short-pick.
// ---------------------------------------------------------------------------

async function pickData() {
  const [queue, tasks, orders] = await Promise.all([
    getJSON('/api/v1/oms/pick-queue', null),
    getJSON('/api/v1/doc/FulfillmentTask?limit=500', []),
    ordersByID()
  ]);
  const byID = {};
  (tasks || []).forEach(t => { byID[t.id] = t; byID[taskID(t)] = t; });
  return { queue: queue ? queue.tasks || [] : null, byID, orders };
}

async function renderPick(container) {
  await stepBar(container, 'Pick', 'pick', wmsOn() ? '<button type="button" class="btn btn-outline" data-oms-mobile>Mobile Picking</button>' : '');
  container.querySelector('[data-oms-mobile]')?.addEventListener('click', () => { setActiveMenu('menu-mobile-picking'); renderView('mobile-picking'); });
  const loadHost = document.createElement('div');
  container.appendChild(loadHost);
  const scanHost = document.createElement('div');
  container.appendChild(scanHost);
  const host = panel(container);
  const { queue, byID, orders } = await pickData();
  if (!queue) { renderErrorPanel(host, 'Failed to load the pick worklist.', () => again('pick')); return; }
  const open = queue.map(q => ({ ...byID[q.task_id], ...q, code: q.task_id }))
    .filter(t => ['Pending', 'Picking'].includes(t.status) && !pickDone(t));
  loadOrderBox(loadHost, 'pick', open, orders, async id => { openTask('pick', id); await showScanPanel('pick', scanHost, id, orders); });
  const shown = open.filter(t => !stepLocation || t.location_code === stepLocation);
  host.innerHTML = `
    <div class="oms-facets">${locationFilter(open, stepLocation)}</div>
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Location</th><th class="num">To pick</th><th>Status</th><th>Waiting</th><th>Actions</th></tr></thead>
      <tbody>${shown.length === 0 ? emptyRow(6, 'Nothing to pick. Orders appear here once they are accepted on Pending Orders; fully picked ones move to Pack.') : shown.map(t => {
        const left = taskItems(t).reduce((n, it) => n + lineLeft(it), 0);
        const total = taskItems(t).reduce((n, it) => n + num(it.qty), 0);
        const actions = t.wave_id
          ? `<span class="text-muted">In wave ${esc(t.wave_id)}</span>${wmsOn() ? ' <button type="button" class="action-btn" data-pick-act="mobile">Mobile Picking</button>' : ''}`
          : `<button type="button" class="btn btn-primary btn-sm" data-pick-act="scan" data-id="${esc(t.code)}">Pick</button>
             <button type="button" class="action-btn" data-pick-act="print" data-id="${esc(t.code)}">Pick list</button>
             <button type="button" class="action-btn action-btn-danger" data-pick-act="reject" data-id="${esc(t.code)}">Reject</button>`;
        return `<tr><td>${t.priority === 'Expedite' ? '<div class="badge badge-warning">Expedite</div>' : ''}${orderCell(t.order_id, orders[t.order_id], t.code)}</td><td>${esc(t.location_code || '—')}</td>
          <td class="num">${left} / ${total}</td><td><span class="badge ${t.status === 'Picking' ? 'badge-warning' : 'badge-secondary'}">${esc(t.status)}</span></td>
          <td>${esc(ageText(t.created_at))}</td><td class="oms-step-actions">${actions}</td></tr>`;
      }).join('')}</tbody>
    </table></div>`;
  wireLocationFilter(host, 'pick');
  host.querySelectorAll('[data-pick-act]').forEach(btn => btn.addEventListener('click', async () => {
    const act = btn.dataset.pickAct, id = btn.dataset.id;
    if (act === 'mobile') { setActiveMenu('menu-mobile-picking'); return renderView('mobile-picking'); }
    if (act === 'print') return printPickList(id);
    if (act === 'reject') return rejectTask(id);
    openTask('pick', id);
    await showScanPanel('pick', scanHost, id, orders);
  }));
  const remembered = openedTask('pick');
  if (remembered && open.some(t => t.code === remembered)) await showScanPanel('pick', scanHost, remembered, orders);
}

async function rejectTask(id) {
  const ok = await showCustomConfirm(`Reject task ${id}? Its lines are routed to another location that has the stock, or the order waits for stock.`, 'Reject task');
  if (!ok) return;
  const r = await post('/api/v1/fulfillment/task/transition', { task_id: id, status: 'Rejected' });
  if (r.error) { await showCustomAlert(r.error, 'Reject task'); return; }
  showToast(`Task ${id} rejected.`, { variant: 'success' });
  again('pick');
}

// The scan panel shared by Pick and Pack: one input that keeps focus, the
// lines with their counts, and the step's next move once every line is done.
async function showScanPanel(step, host, id, orders) {
  const task = await loadTask(id);
  if (!task) { openTask(step, ''); host.innerHTML = ''; await showCustomAlert(`Task ${id} could not be loaded.`, step === 'pick' ? 'Pick' : 'Pack'); return; }
  const order = orders[task.order_id];
  const isPick = step === 'pick';
  // 140.12.20: where each item is (the task's bin reservation), and the bin
  // the picker last scanned - an item scanned there must be one of its own.
  const bins = {};
  if (isPick) (await getJSON(`/api/v1/wms/pick-list?task_id=${encodeURIComponent(id)}`, [])).forEach(b => { if (b.bin_code) (bins[b.sku] = bins[b.sku] || []).push(b); });
  const binCodes = new Set(Object.values(bins).flat().map(b => b.bin_code.toUpperCase()));
  let atBin = '';
  host.innerHTML = `
    <div class="table-panel oms-scan-panel" aria-live="polite">
      <div class="oms-console-head">
        <h2>${isPick ? 'Picking' : 'Packing'} ${esc(id)} · ${esc(order ? order.customer_name || task.order_id : task.order_id || '')}</h2>
        <button type="button" class="btn btn-outline btn-sm" data-scan-close>Close</button>
      </div>
      <form class="oms-scan-row" data-scan-form>
        <input class="form-input" data-scan-input autocomplete="off" placeholder="${isPick && binCodes.size ? 'Scan the bin, then the item' : 'Scan or type the barcode / SKU'}" aria-label="Scan">
        <button type="submit" class="btn btn-primary">${isPick ? 'Pick' : 'Pack'}</button>
      </form>
      <div class="oms-scan-msg" data-scan-msg role="status"></div>
      <div data-scan-lines></div>
      <div data-scan-next></div>
    </div>`;
  const msg = host.querySelector('[data-scan-msg]');
  const input = host.querySelector('[data-scan-input]');
  const say = (text, bad) => { msg.textContent = text; msg.classList.toggle('oms-scan-bad', !!bad); };
  const draw = (t) => drawScanLines(step, host, t, orders, say, bins);
  host.querySelector('[data-scan-close]').addEventListener('click', () => { openTask(step, ''); host.innerHTML = ''; });
  host.querySelector('[data-scan-form]').addEventListener('submit', async e => {
    e.preventDefault();
    const scan = input.value.trim();
    if (!scan) return;
    input.value = '';
    if (isPick && binCodes.has(scan.toUpperCase())) {
      atBin = scan.toUpperCase();
      say(`At bin ${atBin} - scan the items.`);
      input.focus();
      return;
    }
    if (isPick && atBin) {
      const t = await loadTask(id);
      const it = t && taskItems(t).find(x => x.sku === scan || x.barcode === scan);
      const own = it && bins[it.sku];
      if (own && !own.some(b => b.bin_code.toUpperCase() === atBin)) {
        say(`${it.sku} is not in bin ${atBin} - it is in ${own.map(b => b.bin_code).join(', ')}.`, true);
        input.focus();
        return;
      }
    }
    const r = await post(`/api/v1/wms/${isPick ? 'pick' : 'pack'}-scan`, { task_id: id, scan });
    if (r.error) { say(r.error, true); input.focus(); return; }
    say(`${r.sku}: ${isPick ? `picked ${r.picked_qty}` : `packed ${r.packed_qty}`}.`);
    const fresh = await loadTask(id);
    if (fresh) draw(fresh);
    input.focus();
  });
  draw(task);
  host.scrollIntoView({ block: 'start' });
  input.focus();
}

function drawScanLines(step, host, task, orders, say, bins = {}) {
  const id = taskID(task);
  const isPick = step === 'pick';
  const items = taskItems(task);
  const showBins = isPick && Object.keys(bins).length > 0;
  const where = (sku) => (bins[sku] || []).map(b => `${esc(b.bin_code)}${[b.zone, b.aisle, b.rack].filter(Boolean).length ? ` <span class="text-muted">${esc([b.zone, b.aisle, b.rack].filter(Boolean).join('/'))}</span>` : ''}`).join('<br>');
  host.querySelector('[data-scan-lines]').innerHTML = `<div class="table-wrapper"><table>
    <thead><tr><th>SKU</th>${showBins ? '<th>Bin</th>' : ''}<th class="num">Ordered</th><th class="num">Picked</th>${isPick ? '<th class="num">Short</th>' : '<th class="num">Packed</th>'}<th class="num">Left</th>${isPick ? '<th></th>' : ''}</tr></thead>
    <tbody>${items.map(it => {
      const left = isPick ? lineLeft(it) : Math.max(0, num(it.picked_qty) - num(it.packed_qty));
      return `<tr class="${left === 0 ? 'oms-line-done' : ''}"><td class="oms-mono">${esc(it.sku)}</td>${showBins ? `<td>${where(it.sku) || '-'}</td>` : ''}<td class="num">${num(it.qty)}</td><td class="num">${num(it.picked_qty)}</td>
        <td class="num">${isPick ? num(it.short_qty) : num(it.packed_qty)}</td><td class="num">${left}</td>
        ${isPick ? `<td>${left > 0 ? `<button type="button" class="action-btn" data-short="${esc(it.sku)}" title="The rest of this line cannot be found">Short-pick</button>` : ''}</td>` : ''}</tr>`;
    }).join('')}</tbody></table></div>`;
  host.querySelectorAll('[data-short]').forEach(btn => btn.addEventListener('click', async () => {
    const { chooseReason } = await import('./reason-picker.js' + Q);
    const reason = await chooseReason({ category: 'Short Pick', title: `Short-pick ${btn.dataset.short}`, question: 'Why can the rest of this line not be picked? Its reserved stock is released.' });
    if (!reason) return;
    const r = await post('/api/v1/wms/short-pick', { task_id: id, sku: btn.dataset.short, reason_code: reason.code || reason.text });
    if (r.error) { say(r.error, true); return; }
    say(`${btn.dataset.short}: the rest is short-picked.`);
    const fresh = await loadTask(id);
    if (fresh) drawScanLines(step, host, fresh, orders, say, bins);
  }));
  const next = host.querySelector('[data-scan-next]');
  if (isPick) {
    next.innerHTML = pickDone(task) ? `<div class="oms-next-step">Every line is picked or short-picked. <button type="button" class="btn btn-primary btn-sm" data-to-pack>Go to Pack</button></div>` : '';
    next.querySelector('[data-to-pack]')?.addEventListener('click', () => { openTask('pick', ''); openTask('pack', id); goStep('pack'); });
  } else {
    drawPackFinish(next, task, orders, say);
  }
}

function goStep(step) {
  setActiveMenu(`menu-oms-${step}`);
  again(step);
}

// The pick list is a print template (PickList, Setup ▸ Templates), so it
// prints like every document: its own printer through PrintBridge, else the
// browser. A WMS location's bin-by-bin walk is Mobile Picking's.
async function printPickList(id) {
  (await import('./trade-docs.js' + Q)).printTransactionDocument('FulfillmentTask', id);
}

// A package billed before Stage 88.3 has a Sales Invoice, not an OMS bill.
async function printBill(bill) {
  const docs = await import('./trade-docs.js' + Q);
  return bill.bill_number ? docs.printTransactionDocument('OMSBill', bill.bill_id) : docs.printTransactionDocument('SalesInvoice', bill.bill_id);
}

// ---------------------------------------------------------------------------
// 88.2.4 Pack - scan-pack, then one action: the box, its bill, its label.
// ---------------------------------------------------------------------------

async function renderPack(container) {
  await stepBar(container, 'Pack', 'pack');
  const loadHost = document.createElement('div');
  container.appendChild(loadHost);
  const scanHost = document.createElement('div');
  container.appendChild(scanHost);
  const host = panel(container);
  const [tasks, bookings, orders] = await Promise.all([
    getJSON('/api/v1/doc/FulfillmentTask?limit=500', null),
    getJSON('/api/v1/doc/LogisticsBooking?limit=500', []),
    ordersByID()
  ]);
  if (!tasks) { renderErrorPanel(host, 'Failed to load the pack worklist.', () => again('pack')); return; }
  const booked = new Set((bookings || []).filter(b => !['Cancelled', 'RTO'].includes(b.status)).map(b => b.fulfillment_task_id));
  // Ready to pack: every line picked or short-picked. Packed but not yet
  // billed and booked: the one action stopped part-way, so Finish runs the rest.
  const work = tasks.filter(t => (['Pending', 'Picking'].includes(t.status) && pickDone(t) && packLeft(t) > 0) ||
    (t.status === 'Packed' && !booked.has(taskID(t))));
  loadOrderBox(loadHost, 'pack', work, orders, async id => { openTask('pack', id); await showScanPanel('pack', scanHost, id, orders); });
  const shown = work.filter(t => !stepLocation || t.location_code === stepLocation);
  host.innerHTML = `
    <div class="oms-facets">${locationFilter(work, stepLocation)}</div>
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Location</th><th class="num">Units</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>${shown.length === 0 ? emptyRow(5, 'Nothing to pack. Tasks arrive here once every line is picked on Pick.') : shown.map(t => {
        const units = taskItems(t).reduce((n, it) => n + num(it.picked_qty), 0);
        const packed = t.status === 'Packed';
        return `<tr><td>${orderCell(t.order_id, orders[t.order_id], taskID(t))}</td><td>${esc(t.location_code || '—')}</td>
          <td class="num">${units}</td><td><span class="badge ${packed ? 'badge-warning' : 'badge-secondary'}">${packed ? 'Packed - not booked' : 'Picked'}</span></td>
          <td class="oms-step-actions"><button type="button" class="btn btn-primary btn-sm" data-pack-open="${esc(taskID(t))}">${packed ? 'Finish' : 'Pack'}</button></td></tr>`;
      }).join('')}</tbody>
    </table></div>`;
  wireLocationFilter(host, 'pack');
  host.querySelectorAll('[data-pack-open]').forEach(btn => btn.addEventListener('click', () => {
    openTask('pack', btn.dataset.packOpen);
    showScanPanel('pack', scanHost, btn.dataset.packOpen, orders);
  }));
  const remembered = openedTask('pack');
  if (remembered && work.some(t => taskID(t) === remembered)) await showScanPanel('pack', scanHost, remembered, orders);
}

function drawPackFinish(next, task, orders, say) {
  const ready = task.status === 'Packed' || packLeft(task) === 0;
  if (!ready) { next.innerHTML = ''; return; }
  next.innerHTML = `<div class="oms-next-step oms-pack-finish">
    <strong>Every unit is in the box.</strong>
    <label class="oms-facet"><span>Weight (kg)</span><input type="number" min="0" step="0.01" class="form-input" data-pack-weight></label>
    <label class="oms-pack-docs"><input type="checkbox" data-pack-docs> Bill and documents are in the box</label>
    <button type="button" class="btn btn-primary" data-pack-finish>Pack, bill and label</button>
    <div data-pack-result></div>
  </div>`;
  next.querySelector('[data-pack-finish]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      await finishPack(task, orders, {
        weight: Number(next.querySelector('[data-pack-weight]').value) || 0,
        docs: next.querySelector('[data-pack-docs]').checked
      }, next.querySelector('[data-pack-result]'), say);
    } finally { btn.disabled = false; }
  });
}

// finishPack is D3's one action. Each step is safe to repeat (pack-complete is
// skipped once Packed, the package and its bill are idempotent, an existing
// booking is reused), so a run that stops part-way is finished by running it
// again. No booking is not a failure: the courier can be booked on Ship.
async function finishPack(task, orders, opts, out, say) {
  const id = taskID(task);
  const done = [];
  const show = (extra = '') => { out.innerHTML = `<ul class="oms-pack-steps">${done.map(s => `<li>${s}</li>`).join('')}</ul>${extra}`; };
  if (task.status !== 'Packed') {
    const r = await post('/api/v1/wms/pack-complete', { task_id: id, weight_kg: opts.weight, documents_confirmed: opts.docs });
    if (r.error) { say(r.error, true); return; }
  }
  done.push('Packed');
  const pkg = await post('/api/v1/oms/shipping-packages', { fulfillment_task_id: id });
  if (pkg.error) { show(`<p class="oms-scan-bad">${esc(pkg.error)}</p>`); return; }
  if (opts.weight) await post(`/api/v1/oms/shipping-packages/${encodeURIComponent(pkg.package_id)}/update`, { weight_kg: opts.weight });
  done.push(`Package ${esc(pkg.package_id)}`);
  const bill = await post(`/api/v1/oms/shipping-packages/${encodeURIComponent(pkg.package_id)}/invoice`, {});
  if (bill.error) { show(`<p class="oms-scan-bad">The bill was not made: ${esc(bill.error)}</p>`); return; }
  done.push(`Bill ${esc(bill.bill_number || bill.bill_id)}`);
  const bookings = await getJSON(`/api/v1/doc/LogisticsBooking?fulfillment_task_id=${encodeURIComponent(id)}`, []);
  let booking = (bookings || []).find(b => !['Cancelled', 'RTO'].includes(b.status));
  let bookingNote = '';
  if (!booking) {
    const order = orders[task.order_id] || await getJSON(`/api/v1/doc/SalesOrder/${encodeURIComponent(task.order_id)}`, null);
    const pin = orderPincode(order);
    const r = pin ? await post('/api/v1/marketplace/logistics/book', { order_id: task.order_id, fulfillment_task_id: id, destination_pincode: pin })
      : { error: 'the order has no delivery pincode' };
    if (r.error) bookingNote = `<p class="text-muted">No courier booked yet (${esc(r.error)}) - book it on Ship.</p>`;
    else booking = await getJSON(`/api/v1/doc/LogisticsBooking/${encodeURIComponent(r.booking_id)}`, null) || { code: r.booking_id };
  }
  if (booking) done.push(`Courier ${esc(booking.carrier || '')}, AWB ${esc(booking.awb_number || booking.code || '')}`);
  openTask('pack', '');
  show(`<div class="oms-step-actions">
    <button type="button" class="btn btn-primary btn-sm" data-print-bill>Print bill</button>
    ${booking ? '<button type="button" class="btn btn-primary btn-sm" data-print-label>Print label</button>' : ''}
    <button type="button" class="btn btn-outline btn-sm" data-to-ship>Go to Ship</button>
    <button type="button" class="btn btn-outline btn-sm" data-pack-more>Pack the next one</button></div>${bookingNote}`);
  out.querySelector('[data-print-bill]').addEventListener('click', () => printBill(bill));
  // Stage 143.4: Print label shows the label first; Print there prints it.
  out.querySelector('[data-print-label]')?.addEventListener('click', () => window.printShippingLabel(booking.code || booking.id));
  out.querySelector('[data-to-ship]').addEventListener('click', () => goStep('ship'));
  out.querySelector('[data-pack-more]').addEventListener('click', () => again('pack'));
}

// ---------------------------------------------------------------------------
// 88.2.5 Ship - book the courier, manifest, gate pass, hand over, track.
// ---------------------------------------------------------------------------

const liveBooking = (b) => !['Cancelled', 'RTO'].includes(b.status);

// 47.11.8a: the courier did not answer after the booking was sent, so it may
// have it. Resolve records the AWB its portal shows, or Not booked so the next
// try books (the server refuses a retry until then when it cannot ask).
const courierUnconfirmed = (b) => b.awb_booking_state === 'uncertain' && !b.provider_awb;

export async function resolveCourierBooking(b) {
  const { posFormDialog } = await import('./view-pos-extras.js' + Q);
  const id = b.code || b.id, courier = b.awb_booking_provider || b.carrier || '';
  const picked = await posFormDialog('Unconfirmed booking', `Find ${b.courier_reference || id} on the ${courier} portal.`,
    '<div class="form-group"><label class="form-label" for="cr-awb">AWB found</label><input id="cr-awb" class="form-input" autocomplete="off"></div>'
    + '<label class="form-label"><input type="checkbox" id="cr-none"> Not booked there</label>',
    (el) => ({ awb: el.querySelector('#cr-awb').value.trim(), none: el.querySelector('#cr-none').checked }));
  if (!picked) return false;
  if (!picked.none && !picked.awb) { await showCustomAlert('Enter the AWB, or tick Not booked there.', 'Unconfirmed booking'); return false; }
  const r = await post(`/api/v1/marketplace/couriers/${encodeURIComponent(courier)}/resolve`, { booking_id: id, outcome: picked.none ? 'not_booked' : 'booked', awb: picked.none ? '' : picked.awb });
  if (r.error) { await showCustomAlert(r.error, 'Unconfirmed booking'); return false; }
  showToast(picked.none ? 'Marked not booked. Book again.' : `AWB ${picked.awb} saved.`, { variant: 'success' });
  return true;
}

async function renderShip(container) {
  await stepBar(container, 'Ship', 'ship');
  const [tasks, bookings, manifests, passes, orders] = await Promise.all([
    getJSON('/api/v1/doc/FulfillmentTask?limit=500', null),
    getJSON('/api/v1/doc/LogisticsBooking?limit=500', []),
    getJSON('/api/v1/doc/Manifest?limit=200', []),
    getJSON('/api/v1/oms/gate-passes', []),
    ordersByID()
  ]);
  if (!tasks) { renderErrorPanel(panel(container), 'Failed to load shipments.', () => again('ship')); return; }
  const taskByID = {};
  tasks.forEach(t => { taskByID[taskID(t)] = t; });
  const bookedTasks = new Set(bookings.filter(liveBooking).map(b => b.fulfillment_task_id));

  // Order detail's "Book shipment" lands here with its order named.
  let context = null;
  try { context = JSON.parse(sessionStorage.getItem('erp_marketplace_booking_context') || 'null'); sessionStorage.removeItem('erp_marketplace_booking_context'); } catch (_) { context = null; }

  const toBook = tasks.filter(t => t.status === 'Packed' && !bookedTasks.has(taskID(t)));
  const book = panel(container, `<h2 class="oms-step-heading">Book the courier <span class="text-muted">(${toBook.length})</span></h2>
    <div class="table-wrapper"><table>
      <thead><tr><th>Order</th><th>Location</th><th>Pincode</th><th>Courier</th><th></th></tr></thead>
      <tbody>${toBook.length === 0 ? emptyRow(5, 'Every packed parcel has a courier. A parcel packed without one waits here.') : toBook.map(t => {
        const id = taskID(t), o = orders[t.order_id];
        const pin = context && context.fulfillment_task_id === id && context.destination_pincode ? context.destination_pincode : orderPincode(o);
        return `<tr data-book-row="${esc(id)}"><td>${orderCell(t.order_id, o, id)}</td><td>${esc(t.location_code || '—')}</td>
          <td><input class="form-input oms-input-sm" data-book-pin value="${esc(pin)}" aria-label="Destination pincode for ${esc(id)}"></td>
          <td><input class="form-input oms-input-sm" data-book-carrier placeholder="Automatic" aria-label="Courier for ${esc(id)}"></td>
          <td><button type="button" class="btn btn-primary btn-sm" data-book="${esc(id)}" data-order="${esc(t.order_id)}">Book</button></td></tr>`;
      }).join('')}</tbody></table></div>`);
  book.querySelectorAll('[data-book]').forEach(btn => btn.addEventListener('click', async () => {
    const row = btn.closest('tr');
    const pin = row.querySelector('[data-book-pin]').value.trim();
    if (!pin) { await showCustomAlert('Enter the delivery pincode - the courier is chosen by it.', 'Book the courier'); return; }
    btn.disabled = true;
    const r = await post('/api/v1/marketplace/logistics/book', { order_id: btn.dataset.order, fulfillment_task_id: btn.dataset.book,
      destination_pincode: pin, carrier: row.querySelector('[data-book-carrier]').value.trim() });
    btn.disabled = false;
    if (r.error) { await showCustomAlert(r.error, 'Book the courier'); return; }
    showToast(`Booked as ${r.booking_id}. Print its label next.`, { variant: 'success' });
    again('ship');
  }));
  if (context) book.querySelector(`[data-book-row="${CSS.escape(context.fulfillment_task_id || '')}"]`)?.scrollIntoView({ block: 'center' });

  // Booked parcels, grouped as a manifest groups them: one courier at one location.
  const awaiting = bookings.filter(b => b.status === 'AWB Assigned' && !b.manifest_id);
  const groups = {};
  awaiting.forEach(b => {
    const loc = (taskByID[b.fulfillment_task_id] || {}).location_code || '';
    const key = `${b.carrier}\u0000${loc}`;
    (groups[key] = groups[key] || { carrier: b.carrier, loc, rows: [] }).rows.push(b);
  });
  const manifestable = panel(container, `<h2 class="oms-step-heading">Booked - make the manifest <span class="text-muted">(${awaiting.length})</span></h2>
    ${awaiting.length === 0 ? '<p class="oms-step-empty">No booked parcel is waiting for a manifest.</p>' : Object.values(groups).map(g => `
      <div class="oms-ship-group">
        <div class="oms-console-head"><h3>${esc(g.carrier || 'No courier')} at ${esc(g.loc || 'no location')} <span class="text-muted">(${g.rows.length})</span></h3>
          ${g.loc ? `<button type="button" class="btn btn-primary btn-sm" data-manifest-carrier="${esc(g.carrier)}" data-manifest-loc="${esc(g.loc)}">Make manifest</button>` : '<span class="text-muted">Booked without a task - cannot be manifested</span>'}</div>
        <div class="table-wrapper"><table><thead><tr><th>Booking</th><th>Order</th><th>AWB</th><th>Label</th><th></th></tr></thead>
        <tbody>${g.rows.map(b => `<tr><td class="oms-mono">${esc(b.code || b.id)}</td><td>${orderCell(b.order_id, orders[b.order_id])}</td><td class="oms-mono">${esc(b.awb_number || '')}${courierUnconfirmed(b) ? ' <span class="badge badge-warning">Unconfirmed</span>' : ''}</td>
          <td>${b.label_generated_at ? '<span class="badge badge-success">Printed</span>' : '<span class="badge badge-warning">Not printed</span>'}</td>
          <td class="oms-step-actions"><button type="button" class="action-btn" data-label-print="${esc(b.code || b.id)}">Print label</button>${courierUnconfirmed(b) ? ` <button type="button" class="action-btn" data-courier-resolve="${esc(b.code || b.id)}">Resolve</button>` : ''}</td></tr>`).join('')}</tbody></table></div>
      </div>`).join('')}`);
  manifestable.querySelectorAll('[data-label-print]').forEach(btn => btn.addEventListener('click', async () => { await window.printShippingLabel(btn.dataset.labelPrint); }));
  manifestable.querySelectorAll('[data-courier-resolve]').forEach(btn => btn.addEventListener('click', async () => {
    const b = awaiting.find(x => (x.code || x.id) === btn.dataset.courierResolve);
    if (b && await resolveCourierBooking(b)) again('ship');
  }));
  manifestable.querySelectorAll('[data-manifest-carrier]').forEach(btn => btn.addEventListener('click', async () => {
    btn.disabled = true;
    const r = await post('/api/v1/marketplace/logistics/manifest', { courier: btn.dataset.manifestCarrier, location_code: btn.dataset.manifestLoc });
    btn.disabled = false;
    if (r.error) { await showCustomAlert(r.error, 'Make manifest'); return; }
    showToast(`Manifest ${r.manifest_id || ''} made.`, { variant: 'success' });
    again('ship');
  }));

  // Manifests: gate pass, then hand over.
  const passFor = {};
  (passes || []).filter(p => p.status !== 'Discarded').forEach(p => { if (p.manifest_id && !passFor[p.manifest_id]) passFor[p.manifest_id] = p; });
  const openManifests = manifests.filter(m => m.status === 'Open');
  const handed = manifests.filter(m => m.status !== 'Open').slice(-20).reverse();
  const manifestRow = (m) => {
    const id = m.code || m.id, gp = passFor[id];
    const pass = gp ? `${esc(gp.status)} · ${esc(gp.vehicle_number || 'no vehicle')}${gp.status === 'Draft' ? ` <button type="button" class="action-btn" data-gp-issue="${esc(gp.id)}">Issue</button>` : ''}`
      : (m.status === 'Open' ? `<button type="button" class="action-btn" data-gp-new="${esc(id)}" data-gp-loc="${esc(m.location_code || '')}">Gate pass</button>` : '—');
    return `<tr><td class="oms-mono">${esc(id)}</td><td>${esc(m.courier || '')}</td><td>${esc(m.location_code || '')}</td><td class="num">${num(m.shipment_count)}</td><td>${pass}</td>
      <td><span class="badge ${m.status === 'Open' ? 'badge-warning' : 'badge-success'}">${esc(m.status)}</span></td>
      <td>${m.status === 'Open' ? `<button type="button" class="btn btn-primary btn-sm" data-handover="${esc(id)}">Hand over</button>` : ''}</td></tr>`;
  };
  const head = '<thead><tr><th>Manifest</th><th>Courier</th><th>Location</th><th class="num">Parcels</th><th>Gate pass</th><th>Status</th><th></th></tr></thead>';
  const man = panel(container, `<h2 class="oms-step-heading">Manifests <span class="text-muted">(${openManifests.length} open)</span></h2>
    <div class="table-wrapper"><table>${head}<tbody>${openManifests.length === 0 ? emptyRow(7, 'No open manifest. Make one from the booked parcels above.') : openManifests.map(manifestRow).join('')}</tbody></table></div>
    ${handed.length ? `<details class="oms-related-docs"><summary>Handed over (last ${handed.length})</summary><div class="table-wrapper"><table>${head}<tbody>${handed.map(manifestRow).join('')}</tbody></table></div></details>` : ''}`);
  man.querySelectorAll('[data-gp-new]').forEach(btn => btn.addEventListener('click', () => newGatePass(btn.dataset.gpNew, btn.dataset.gpLoc)));
  man.querySelectorAll('[data-gp-issue]').forEach(btn => btn.addEventListener('click', async () => {
    const r = await post(`/api/v1/oms/gate-passes/${encodeURIComponent(btn.dataset.gpIssue)}/issue`, {});
    if (r.error) { await showCustomAlert(r.error, 'Gate pass'); return; }
    again('ship');
  }));
  man.querySelectorAll('[data-handover]').forEach(btn => btn.addEventListener('click', () => handOver(btn.dataset.handover, passFor[btn.dataset.handover])));

  // On the way: tracking until delivered or returned.
  const moving = bookings.filter(b => ['Handed Over', 'In-Transit'].includes(b.status));
  const closed = bookings.filter(b => ['Delivered', 'RTO'].includes(b.status)).slice(-50).reverse();
  const trackRow = (b, actions) => `<tr><td class="oms-mono">${esc(b.code || b.id)}</td><td>${orderCell(b.order_id, orders[b.order_id])}</td><td>${esc(b.carrier || '')}</td><td class="oms-mono">${esc(b.awb_number || '')}</td>
    <td><span class="badge ${b.status === 'RTO' ? 'badge-danger' : b.status === 'Delivered' ? 'badge-success' : 'badge-secondary'}">${esc(b.status)}</span></td><td class="oms-step-actions">${actions}</td></tr>`;
  const thead = '<thead><tr><th>Booking</th><th>Order</th><th>Courier</th><th>AWB</th><th>Status</th><th></th></tr></thead>';
  const track = panel(container, `<h2 class="oms-step-heading">On the way <span class="text-muted">(${moving.length})</span></h2>
    <div class="table-wrapper"><table>${thead}<tbody>${moving.length === 0 ? emptyRow(6, 'Nothing is with a courier right now.') : moving.map(b => trackRow(b,
      `${b.status === 'Handed Over' ? `<button type="button" class="action-btn" data-track="In-Transit" data-id="${esc(b.code || b.id)}">In transit</button>` : ''}
       <button type="button" class="action-btn" data-track="Delivered" data-id="${esc(b.code || b.id)}">Delivered</button>
       <button type="button" class="action-btn action-btn-danger" data-rto="${esc(b.code || b.id)}">RTO</button>`)).join('')}</tbody></table></div>
    ${closed.length ? `<details class="oms-related-docs"><summary>Delivered and returned (last ${closed.length})</summary><div class="table-wrapper"><table>${thead}<tbody>${closed.map(b => trackRow(b, '')).join('')}</tbody></table></div></details>` : ''}`);
  track.querySelectorAll('[data-track]').forEach(btn => btn.addEventListener('click', async () => {
    const r = await post('/api/v1/marketplace/logistics/tracking', { booking_id: btn.dataset.id, status: btn.dataset.track });
    if (r.error) { await showCustomAlert(r.error, 'Tracking'); return; }
    again('ship');
  }));
  track.querySelectorAll('[data-rto]').forEach(btn => btn.addEventListener('click', async () => {
    const reason = await showCustomPrompt(`Why is the courier returning ${btn.dataset.rto} undelivered?`, '', 'Return to origin (RTO)');
    if (!reason || !reason.trim()) return;
    const r = await post('/api/v1/marketplace/logistics/rto', { booking_id: btn.dataset.rto, reason: reason.trim() });
    if (r.error) { await showCustomAlert(r.error, 'RTO'); return; }
    again('ship');
  }));
}

// A gate pass is the vehicle's pass out of the gate (35.4.4): raised with the
// vehicle and driver, issued at once when the vehicle number is known.
async function newGatePass(manifestID, locationCode) {
  const vehicle = await showCustomPrompt(`Vehicle number collecting manifest ${manifestID}:`, '', 'Gate pass');
  if (vehicle === null) return;
  const driver = await showCustomPrompt('Driver name and phone (optional), e.g. "Ravi 9876543210":', '', 'Gate pass');
  if (driver === null) return;
  const phone = (driver.match(/\+?\d[\d\s-]{6,}\d/) || [''])[0];
  const r = await post('/api/v1/oms/gate-passes', { manifest_id: manifestID, location_code: locationCode, vehicle_number: vehicle.trim(),
    driver_name: driver.replace(phone, '').trim(), driver_phone: phone.replace(/[\s-]/g, '') });
  if (r.error) { await showCustomAlert(r.error, 'Gate pass'); return; }
  if (vehicle.trim()) {
    const issued = await post(`/api/v1/oms/gate-passes/${encodeURIComponent(r.gate_pass_id)}/issue`, {});
    if (issued.error) await showCustomAlert(`Gate pass ${r.gate_pass_id} saved as Draft: ${issued.error}`, 'Gate pass');
  }
  again('ship');
}

async function handOver(manifestID, gatePass) {
  const ok = await showCustomConfirm(`Hand manifest ${manifestID} to the courier? Its parcels are dispatched and their orders move to Shipped.`, 'Hand over');
  if (!ok) return;
  const r = await post('/api/v1/marketplace/logistics/manifest/handover', { manifest_id: manifestID });
  if (r.error) { await showCustomAlert(r.error, 'Hand over'); return; }
  // The vehicle leaves with the manifest, so its issued pass closes with it.
  if (gatePass && gatePass.status === 'Issued') {
    const done = await post(`/api/v1/oms/gate-passes/${encodeURIComponent(gatePass.id)}/complete`, {});
    if (done.error) showToast(`Handed over; gate pass ${gatePass.id} is still open: ${done.error}`, { variant: 'warning', duration: 9000 });
  }
  showToast(`Manifest ${manifestID} handed over.`, { variant: 'success' });
  again('ship');
}

const STEPS = { pending: renderPending, pick: renderPick, pack: renderPack, ship: renderShip };

export function isOMSStep(tab) { return Object.prototype.hasOwnProperty.call(STEPS, tab); }

export async function renderOMSStep(tab, container) {
  await STEPS[tab](container);
}
