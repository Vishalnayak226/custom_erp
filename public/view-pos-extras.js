// The parts of the POS screen a cashier opens now and then: the Return tab,
// + New Customer, Check / Redeem Points, and the Packaging and Gift Card
// dialogs (Stage 72). They live here, not in view-pos.js, so the till - the
// first screen, counted in the cold-core budget (cmd/releasebudget,
// NFR-COST-001) - stays inside it. view-pos.js imports this module (same ?v=)
// as soon as the till renders.
//
// posCart and posLocation are app.js globals. The till's own state lives in
// view-pos.js and reaches the dialogs through `till`, which each entry point
// is handed.

let till = null;

// Stage 47.4.6 - the Returns surface, rebuilt.
//
// What it replaced, and why: the Stage 20.11 panel asked the clerk to type the
// SKU, the quantity, the SALE PRICE and the COST PRICE, and posted them to the
// retired POST /api/v1/fulfillment/return. Every one of those four was a
// question the server could answer better - and the two prices were questions
// the client had no business answering at all (audit A-02/A-04).
//
// It now works the other way round: the clerk types the bill number, the server
// says what is still returnable and at what price, and the clerk picks
// quantities. Nothing on this screen can set a price, and the eligibility
// explanation is shown in words so a refusal can be repeated to the customer
// standing at the counter rather than appearing as an unexplained rejection.
//
// This stays a thin, separate panel (its own posReturnCart) rather than a
// mode-toggle on the sale cart above, so a return in progress can never be
// confused with or accidentally merged into an in-progress sale.
let posReturnCart = []; // { sku, qty, unitPrice, remaining }
let posReturnEligibility = null;

export function renderPOSReturnPanel(panel) {
  // Stage 68: the Return tab of the POS screen - same frozen layout, the
  // refund and Raise Return at the top, the bill's lines as the scroller.
  // panel is the empty #pos-return-pane renderPOSView made.
  panel.innerHTML = `
    <div class="txn-head pos-return-head">
      <!-- Stage 85.E4 (77.8): find the bill by its number, the customer's
           mobile or name, or the day - at this store, or any of yours. -->
      <div class="form-group">
        <label class="form-label" for="pos-return-order-id">Bill no., mobile or name</label>
        <input type="text" id="pos-return-order-id" class="form-input" placeholder="Scan the bill, or type its number / the mobile" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="pos-return-date">Bill date</label>
        <input type="date" id="pos-return-date" class="form-input">
      </div>
      <div class="form-group pos-return-lookup">
        <label style="display:flex; gap:6px; align-items:center; font-size:12px;"><input type="checkbox" id="pos-return-all-stores"> All my stores</label>
        <button class="btn btn-outline" id="pos-return-lookup-btn" type="button">Find Bill</button>
      </div>
      <div class="form-group">
        <label class="form-label" for="pos-return-location-display">Return Location</label>
        <input type="text" id="pos-return-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
        <input type="hidden" id="pos-return-location" value="${cfgEsc(posLocation)}">
      </div>
      <div class="pos-return-total">
        <span>Refund if all accepted <b id="pos-return-total">0.00</b></span>
        <button class="btn btn-outline" id="pos-return-upload-btn" type="button" title="Return quantities from a CSV (Barcode, Qty) for this bill's items">Upload</button>
        <button class="btn btn-primary" id="pos-return-submit-btn" type="button" disabled>Raise Return</button>
      </div>
    </div>
    <div id="pos-return-results" class="hidden pos-search-results"></div>
    <div id="pos-return-eligibility" class="txn-hint"></div>
    <div id="pos-return-error" class="login-error hidden"></div>
    <div class="txn-grid">
      <table>
        <thead>
          <tr>
            <th>SKU</th><th>Sold</th><th>Already Returned</th><th>Still Returnable</th>
            <th>Return Qty</th><th>Price (from the bill)</th><th>Refund</th>
          </tr>
        </thead>
        <tbody id="pos-return-body"></tbody>
      </table>
      <p class="txn-empty">Prices come from the original bill and cannot be changed here. The refund is calculated by the server after the goods are received and inspected.</p>
    </div>
  `;
  attachCodeNamePicker(
    document.getElementById('pos-return-location-display'),
    document.getElementById('pos-return-location'),
    'Location');
  document.getElementById('pos-return-lookup-btn').addEventListener('click', findPOSReturnBill);
  document.getElementById('pos-return-order-id').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      findPOSReturnBill();
    }
  });
  document.getElementById('pos-return-submit-btn').addEventListener('click', submitPOSReturn);
  // Stage 85.E8: return quantities from a CSV, for the bill looked up.
  document.getElementById('pos-return-upload-btn').addEventListener('click', async () => {
    if (!posReturnCart.length) { await showCustomAlert('Find the bill first - the upload sets how many of its items come back.', 'Upload'); return; }
    const pickers = await loadViewModule('/view-pickers.js?v=1');
    pickers.openLineUpload({ title: 'Upload return quantities', template: 'sales-return', onRows: async (rows) => {
      const notOnBill = [];
      rows.forEach(r => {
        const line = posReturnCart.find(l => l.sku === r.sku);
        if (!line) { notOnBill.push(r.sku); return; }
        line.qty = Math.min(line.remaining, (line.qty || 0) + r.qty);
      });
      renderPOSReturnTable();
      if (notOnBill.length) await showCustomAlert(`Not on this bill (not added): ${notOnBill.join(', ')}`, 'Upload');
    } });
  });

  // Stage 55.5: a return is taken where the till is. The Return Location
  // follows the till's store whenever that is set and this box is still
  // blank, so a cashier is not asked again for the store they opened the
  // session at. It stays a picker - a return for another store can still be
  // keyed.
  const tillStore = document.getElementById('pos-location');
  const followTillStore = () => {
    const hidden = document.getElementById('pos-return-location');
    const display = document.getElementById('pos-return-location-display');
    if (!tillStore || !hidden || !display || hidden.value.trim() || !tillStore.value.trim()) return;
    hidden.value = tillStore.value.trim();
    display.value = (window.posStoreLabel && window.posStoreLabel()) || hidden.value;
    display.dataset.resolved = '1';
  };
  if (tillStore) tillStore.addEventListener('change', followTillStore);
  followTillStore();

  renderPOSReturnTable();
}

// findPOSReturnBill (Stage 85.E4) searches the store's paid bills; one match
// is looked up at once, several are listed to pick from.
let posReturnBillStore = '';
async function findPOSReturnBill() {
  const errorEl = document.getElementById('pos-return-error');
  const list = document.getElementById('pos-return-results');
  errorEl.classList.add('hidden');
  list.classList.add('hidden');
  const q = document.getElementById('pos-return-order-id').value.trim();
  const date = document.getElementById('pos-return-date').value;
  if (!q && !date) {
    errorEl.textContent = 'Type the bill number or the customer\'s mobile, or pick the bill date.';
    errorEl.classList.remove('hidden');
    return;
  }
  const scope = document.getElementById('pos-return-all-stores').checked ? 'mine' : '';
  const res = await apiFetch(`/api/v1/pos/bills?q=${encodeURIComponent(q)}&date=${encodeURIComponent(date)}&scope=${scope}`);
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'Bills could not be searched.'); errorEl.classList.remove('hidden'); return; }
  const bills = await res.json();
  const exact = bills.find(b => [b.cart_number, b.bill_number].some(n => n && n.toLowerCase() === q.toLowerCase()));
  if (exact || bills.length === 1) return pickPOSReturnBill(exact || bills[0]);
  if (!bills.length) {
    errorEl.textContent = scope ? 'No paid bill matches at your stores.' : 'No paid bill matches at this store. Tick All my stores to look further.';
    errorEl.classList.remove('hidden');
    return;
  }
  list.innerHTML = bills.map((b, i) => `<button type="button" class="pos-search-result" data-bill="${i}">
    <span><b>${cfgEsc(b.bill_number || b.cart_number)}</b> · ${cfgEsc(b.date)}</span>
    <span>${cfgEsc(b.customer || 'Walk-in')}${b.phone ? ' · ' + cfgEsc(b.phone) : ''} · ${Number(b.total).toFixed(2)}${b.store !== posLocation ? ' · ' + cfgEsc(b.store) : ''}</span></button>`).join('');
  list.classList.remove('hidden');
  list.querySelectorAll('[data-bill]').forEach(btn => btn.addEventListener('click', () => { list.classList.add('hidden'); pickPOSReturnBill(bills[Number(btn.dataset.bill)]); }));
}

function pickPOSReturnBill(b) {
  document.getElementById('pos-return-order-id').value = b.cart_number;
  posReturnBillStore = b.store || '';
  return lookUpPOSReturnBill();
}

// lookUpPOSReturnBill asks the server what is returnable for one bill.
async function lookUpPOSReturnBill() {
  const errorEl = document.getElementById('pos-return-error');
  errorEl.classList.add('hidden');
  const orderID = document.getElementById('pos-return-order-id').value.trim();
  posReturnCart = [];
  posReturnEligibility = null;
  if (!orderID) {
    errorEl.textContent = 'Enter the original bill or cart number.';
    errorEl.classList.remove('hidden');
    renderPOSReturnTable();
    return;
  }
  const res = await apiFetch(`/api/v1/returns/eligibility?original_order_id=${encodeURIComponent(orderID)}`);
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'That bill could not be looked up.');
    errorEl.classList.remove('hidden');
    renderPOSReturnTable();
    return;
  }
  posReturnEligibility = await res.json();
  // Every returnable line starts at zero: a return is something the clerk
  // chooses line by line with the customer, not a whole-bill default.
  posReturnCart = (posReturnEligibility.lines || [])
    .filter(l => l.remaining_qty > 0)
    .map(l => ({ sku: l.sku, qty: 0, unitPrice: Number(l.unit_price) || 0, remaining: l.remaining_qty, sold: l.sold_qty, returned: l.already_returned, reason: l.reason }));
  renderPOSReturnTable();
}

function updatePOSReturnLine(sku, value) {
  const line = posReturnCart.find(l => l.sku === sku);
  if (!line) return;
  let qty = parseInt(value, 10);
  if (isNaN(qty) || qty < 0) qty = 0;
  // Clamped at the source rather than left for the server to reject: the
  // server still enforces it under a lock, but a spinner that lets a clerk
  // type 99 and only learns at submit time is a worse counter experience.
  if (qty > line.remaining) qty = line.remaining;
  line.qty = qty;
  renderPOSReturnTable();
}

function renderPOSReturnTable() {
  const body = document.getElementById('pos-return-body');
  if (!body) return;
  body.innerHTML = '';

  const explain = document.getElementById('pos-return-eligibility');
  if (explain) {
    if (!posReturnEligibility) {
      explain.innerHTML = '<span style="color: var(--text-muted);">Enter a bill number and look it up to see what can be returned.</span>';
    } else {
      const ok = posReturnEligibility.found && posReturnEligibility.within_window;
      explain.innerHTML = `
        <div style="padding:10px 12px; border-radius:6px; border:1px solid ${ok ? 'var(--border-color)' : 'var(--warning-soft-border, var(--border-color))'};
                    background: ${ok ? 'var(--bg-color)' : 'var(--warning-soft-bg, var(--bg-color))'};">
          ${cfgEsc(posReturnEligibility.explanation || '')}
          ${posReturnBillStore && posReturnBillStore !== posLocation ? `<div style="margin-top:4px;"><b>Sold at ${cfgEsc(posReturnBillStore)}.</b> The return is taken here: the goods come back into this store's stock, and the return keeps the store that sold it.</div>` : ''}
          ${posReturnEligibility.sale_date ? `<span style="color:var(--text-muted);"> Sold ${cfgEsc(posReturnEligibility.sale_date)}; return window ${posReturnEligibility.window_days} days.</span>` : ''}
          ${posBillCancellable() ? `<div style="margin-top:8px;"><button type="button" class="btn btn-outline btn-sm" ${actionAttrs('cancelWholePOSBill', [])}>Cancel the whole bill</button> <span style="color:var(--text-muted);">Today's bill: everything comes back and the money goes back to how it was paid.</span></div>` : ''}
        </div>`;
    }
  }

  let refund = 0;
  posReturnCart.forEach(line => {
    refund += line.qty * line.unitPrice;
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td style="font-weight:600;">${cfgEsc(line.sku)}</td>
      <td>${line.sold}</td>
      <td>${line.returned}</td>
      <td>${line.remaining}</td>
      <td><input type="number" min="0" max="${line.remaining}" value="${line.qty}" class="form-input" style="width: 90px;"
                 ${actionAttrs('updatePOSReturnLine', [line.sku, ACTION_ARG_VALUE], { on: 'change' })}></td>
      <td>${line.unitPrice.toFixed(2)}</td>
      <td>${(line.qty * line.unitPrice).toFixed(2)}</td>
    `;
    body.appendChild(tr);
  });

  const totalEl = document.getElementById('pos-return-total');
  if (totalEl) totalEl.textContent = refund.toFixed(2);
  const submitBtn = document.getElementById('pos-return-submit-btn');
  if (submitBtn) submitBtn.disabled = !posReturnCart.some(l => l.qty > 0);
}

async function submitPOSReturn() {
  // BLD-036: converted from a bare manual disable/finally to the shared
  // helper for a busy label, consistent with the other guarded composers -
  // the server-side idempotency key below already made a double-click safe,
  // this only makes it clearer to the cashier that it's in flight.
  await guardAgainstDoubleSubmit(document.getElementById('pos-return-submit-btn'), 'Submitting...', submitPOSReturnInner);
}

async function submitPOSReturnInner() {
  const errorEl = document.getElementById('pos-return-error');
  errorEl.classList.add('hidden');
  const orderID = document.getElementById('pos-return-order-id').value.trim();
  const returnLocation = document.getElementById('pos-return-location').value.trim();
  const lines = posReturnCart.filter(l => l.qty > 0);

  if (!orderID || !returnLocation) {
    errorEl.textContent = 'Original bill number and return location are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (lines.length === 0) {
    errorEl.textContent = 'Set a return quantity on at least one line.';
    errorEl.classList.remove('hidden');
    return;
  }

  // Stage 47.4.3: one key per bill + line set, so a double-click, a lost
  // response or a second tab raises ONE return. The server refuses a
  // different payload under the same key, which is why the key includes the
  // lines rather than only the bill.
  const idempotencyKey = `${orderID}|${lines.map(l => `${l.sku}:${l.qty}`).sort().join(',')}`;
  const res = await apiFetch('/api/v1/returns', {
    method: 'POST',
    body: JSON.stringify({
      request_type: 'Customer Return',
      return_location: returnLocation,
      original_order_id: orderID,
      idempotency_key: idempotencyKey,
      // Selection only. No price, no cost - the server resolves both from
      // the original sale lines.
      items: lines.map(l => ({ sku: l.sku, qty: l.qty }))
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'The return could not be raised.');
    errorEl.classList.remove('hidden');
    return;
  }
  const data = await res.json();
  posReturnCart = [];
  posReturnEligibility = null;
  document.getElementById('pos-return-order-id').value = '';
  renderPOSReturnTable();
  if (data.replayed) {
    await showCustomAlert(`This return was already raised as ${data.return_request_id}. Nothing was duplicated.`, 'Return Raised');
    return;
  }
  // Stage 55.5: say what the customer gets back and where the return goes
  // next, and offer to take the cashier there.
  const back = await describeReturnRefund(data.return_request_id);
  const open = await showCustomConfirm(
    `Return ${data.return_request_id} raised.${back ? ` When it is refunded the customer gets back ${back}.` : ''}` +
    ' Next, on Returns: Approve, Receive Goods, Inspect, then Refund - no refund is paid until the goods have been inspected. Open Returns now?',
    'Return Raised');
  if (open) renderView('returns');
}

// defaultPOSStore (Stage 55.5) is gone (Stage 85.A5): the till always works
// at the working store chosen at the top of the screen.

// describeReturnRefund reads the return just raised: its lines carry their
// share of what the bill was paid with (Stage 73.1.1), so the cashier can tell
// the customer "Cash 900.00 and 270 points" before anything is approved.
async function describeReturnRefund(returnID) {
  const res = await apiFetch(`/api/v1/doc/ReturnRequest/${encodeURIComponent(returnID)}`).catch(() => null);
  if (!res || !res.ok) return '';
  const doc = await res.json().catch(() => null);
  const tenders = {};
  let points = 0;
  let plain = 0;
  (Array.isArray(doc?.items) ? doc.items : []).forEach(it => {
    if (it.tenders) {
      Object.entries(it.tenders).forEach(([k, v]) => { if (k !== 'Loyalty Points') tenders[k] = (tenders[k] || 0) + Number(v || 0); });
    } else {
      plain += Number(it.original_unit_price || 0) * Number(it.qty || 0);
    }
    points += Number(it.points || 0);
  });
  const parts = Object.keys(tenders).filter(k => tenders[k] > 0).sort().map(k => `${k} ${(tenders[k] / 100).toFixed(2)}`);
  if (plain > 0) parts.push(plain.toFixed(2));
  if (points > 0) parts.push(`${points} points`);
  return parts.length > 1 ? parts.slice(0, -1).join(', ') + ' and ' + parts[parts.length - 1] : parts.join('');
}

// The quantity box in the Return table calls this through the data-act
// dispatcher, which only resolves names that are own properties of window.
// Stage 130.9: a manager may cancel today's bill outright while nothing on it
// has been returned - the server checks the same and does the reversal.
function posBillCancellable() {
  const e = posReturnEligibility;
  if (!e || !e.found || !posIsManager()) return false;
  const d = new Date();
  const today = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const lines = e.lines || [];
  return e.sale_date === today && lines.some(l => l.remaining_qty > 0) && lines.every(l => !l.already_returned);
}

async function cancelWholePOSBill() {
  const billNo = (posReturnEligibility || {}).original_order_id;
  if (!billNo) return;
  const picked = await posFormDialog('Cancel Bill', `Cancel bill ${billNo}? Every item comes back into stock and the money is paid back the way it was paid. This cannot be undone.`,
    '<input class="form-input" data-cancel="reason" placeholder="Why is the bill being cancelled?" autocomplete="off" style="margin-top:10px;">',
    extra => ({ reason: (extra.querySelector('[data-cancel="reason"]') || {}).value || '' }),
    extra => extra.querySelector('[data-cancel="reason"]')?.focus());
  if (!picked) return;
  if (!picked.reason.trim()) { await showCustomAlert('Give the reason the bill is being cancelled.', 'Cancel Bill'); return; }
  const res = await apiFetch(`/api/v1/pos/bills/${encodeURIComponent(billNo)}/cancel`, { method: 'POST', body: JSON.stringify({ location: posLocation, reason: picked.reason.trim() }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The bill was not cancelled - nothing changed.', 'Cancel Bill'); return; }
  const r = await res.json();
  await showCustomAlert(`Bill ${r.bill_no} ${r.replayed ? 'was already' : 'is'} cancelled. Pay back ${Number(r.refund).toFixed(2)} the way it was paid. Return ${r.return_request_id}.`, 'Bill Cancelled');
  posReturnCart = [];
  posReturnEligibility = null;
  renderPOSReturnTable();
}

Object.assign(window, { updatePOSReturnLine, cancelWholePOSBill });

// quickAddPOSCustomer (Stage 53.5: name + phone in two prompts) is replaced
// by the phone-first customer form in view-pos-customer.js (Stage 85.C3).

// CRM/Loyalty (Stage 13.13d, scoped MVP) - POS integration. Earning
// happens automatically server-side (handleCheckout) once customer_id is
// set; these two actions are the customer-facing "check balance" /
// "redeem" steps a cashier drives manually before completing the sale.
export async function checkPOSLoyaltyBalance() {
  const infoEl = document.getElementById('pos-loyalty-info');
  const customerId = document.getElementById('pos-customer').value.trim();
  if (!customerId) {
    infoEl.textContent = 'Search for the customer by name or phone first, or add them with + New Customer.';
    return;
  }
  // Stage 73.3.2: the customer on the bill, not just a balance - points and
  // their value, tier and what it gives, the way to the next tier, recent
  // bills and returns, and the vouchers they can use now.
  const res = await apiFetch(`/api/v1/pos/customer-context?customer_id=${encodeURIComponent(customerId)}`);
  if (!res) return;
  if (!res.ok) {
    infoEl.textContent = 'Failed to look up the customer.';
    return;
  }
  const c = await res.json();
  infoEl.textContent = `${c.name || customerId} has ${c.balance} loyalty point(s) (worth ${Number(c.points_value).toFixed(2)}).`;
  const money = n => Number(n || 0).toFixed(2);
  const section = (title, rows, empty) => `<h4 style="margin:12px 0 4px; font-size:13px;">${title}</h4>${rows.length ? '<ul style="margin:0; padding-left:18px; font-size:13px;">' + rows.map(r => `<li>${r}</li>`).join('') + '</ul>' : `<div class="txn-hint">${empty}</div>`}`;
  const html = `<div style="font-size:13px; margin-top:8px;">
      <div><strong>${cfgEsc(c.name || customerId)}</strong>${c.phone ? ` &middot; ${cfgEsc(c.phone)}` : ''}</div>
      <div>Points: <strong>${c.balance}</strong> (worth ${money(c.points_value)})${c.balance < 0 ? ' - negative after a return; new purchases pay it back' : ''}</div>
      <div>Tier: <strong>${cfgEsc(c.tier || 'none yet')}</strong>${c.benefits ? ` - ${cfgEsc(c.benefits)}` : ''}</div>
      <div>Offers: ${c.consent_sms || c.consent_whatsapp ? `agreed by ${[c.consent_sms ? 'SMS' : '', c.consent_whatsapp ? 'WhatsApp' : ''].filter(Boolean).join(' and ')}` : 'not agreed - no SMS or WhatsApp offers are sent'}</div>
      <div>${c.next_tier ? `${money(c.to_next_tier)} more to ${cfgEsc(c.next_tier)}` : 'At the top tier'} (spend counted: ${money(c.spend)})${c.below_tier_since ? ` &middot; below this tier since ${cfgEsc(c.below_tier_since)}` : ''}</div>
    </div>`
    + section('Vouchers they can use now (type the code in Coupon code)', (c.vouchers || []).map(v => `<strong>${cfgEsc(v.code)}</strong> - ${cfgEsc(v.offer)}${v.expires ? `, until ${cfgEsc(v.expires)}` : ''}${v.uses_left ? `, ${v.uses_left} use(s) left` : ''}`), 'None issued to this customer.')
    + section('Recent bills', (c.recent_bills || []).map(b => `${cfgEsc(b.date)} &middot; ${cfgEsc(b.cart_number)} &middot; ${money(b.amount)} (${b.lines} line${b.lines === 1 ? '' : 's'})`), 'No bills yet.')
    + section('Recent returns', (c.recent_returns || []).map(r => `${cfgEsc(r.date)} &middot; ${cfgEsc(r.id)} against ${cfgEsc(r.bill)} &middot; ${cfgEsc(r.status)}${r.refund ? ` &middot; ${money(r.refund)}` : ''}`), 'No returns.');
  await posFormDialog('Customer', '', html, () => true);
}

export async function redeemPOSLoyaltyPoints(t) {
  till = t;
  const infoEl = document.getElementById('pos-loyalty-info');
  const customerId = document.getElementById('pos-customer').value.trim();
  if (!customerId) {
    infoEl.textContent = 'Search for the customer by name or phone first, or add them with + New Customer.';
    return;
  }
  if (posCart.length === 0) {
    infoEl.textContent = 'Add items to the cart before redeeming points - the discount is applied to this sale.';
    return;
  }

  // Stage 73.1.5: the server prices points - the balance, what one point is
  // worth and the most this bill can take all come from the quote, so the
  // till never assumes 1 point = 1.
  till.redeemPoints = 0;
  till.auth = {};
  await till.quote();
  const l = till.loyalty;
  if (!l) {
    infoEl.textContent = 'Could not price points for this customer - check the connection and try again.';
    return;
  }
  if (l.max_points <= 0) {
    infoEl.textContent = l.balance > 0 ? `${customerId} has ${l.balance} point(s), but none can be used on this bill.` : `${customerId} has no loyalty points to redeem.`;
    return;
  }
  const pointsStr = await showCustomPrompt(`How many points to redeem? ${customerId} has ${l.balance}; 1 point = ${l.value_per_point} off. This bill can take up to ${l.max_points}.`, '', 'Redeem Points', 'number');
  const points = parseInt(pointsStr, 10);
  if (!points || points <= 0) return;
  if (points > l.max_points) {
    infoEl.textContent = `This bill can take at most ${l.max_points} point(s).`;
    return;
  }
  till.redeemPoints = points;
  await till.quote();
  const q = till.loyalty || {};

  // Stage 73.1.4: the controls - the customer's one-time code, and a manager's
  // approval above the approval value. Both are bound to this cart; the sale
  // spends them, and the server checks them again when the points burn.
  if (q.needs_otp || q.needs_approval) {
    let reason = '';
    if (q.needs_approval) {
      reason = await pickLoyaltyReason(q);
      if (reason === null) { till.redeemPoints = 0; till.renderCart(); return; }
    }
    const res = await apiFetch('/api/v1/pos/loyalty/redemption/start', {
      method: 'POST',
      body: JSON.stringify({ customer_id: customerId, points: q.points, cart_number: till.cart, location: till.location, reason_code: reason })
    });
    if (!res || !res.ok) {
      infoEl.textContent = await getErrorMessage(res, 'The redemption could not be started.');
      till.redeemPoints = 0;
      till.renderCart();
      return;
    }
    const started = await res.json();
    const auth = {};
    if (started.challenge_id) {
      const otp = await showCustomPrompt('A one-time code was sent to the customer. Enter the code they read back.');
      if (!otp) { till.redeemPoints = 0; till.renderCart(); return; }
      const v = await apiFetch('/api/v1/pos/loyalty/redemption/verify', { method: 'POST', body: JSON.stringify({ challenge_id: started.challenge_id, otp: otp.trim() }) });
      if (!v || !v.ok) {
        infoEl.textContent = await getErrorMessage(v, 'That code is not right. Choose Redeem Points to send a new one.');
        till.redeemPoints = 0;
        till.renderCart();
        return;
      }
      auth.redemption_challenge_id = started.challenge_id;
    }
    if (started.approval_id) auth.redemption_approval_id = started.approval_id;
    till.auth = auth;
    if (started.approval_id && started.approval_status !== 'Approved') {
      infoEl.textContent = `${q.points} point(s) (${Number(q.value).toFixed(2)} off) need a ${q.approver_role || 'manager'}'s approval - request ${started.approval_id} is waiting under Approvals. Complete the sale once it is approved.`;
      return;
    }
  }

  // Nothing is burned here. The points ride on the cart and are spent by the
  // server only if the sale completes.
  infoEl.textContent = `${q.points} point(s) (${Number(q.value).toFixed(2)} off) will be applied to this sale. They are only deducted once the sale completes.`;
}

// pickLoyaltyReason asks for the reason a large redemption is being made, from
// the active Loyalty Redemption reason codes. Resolves the code, or null.
function pickLoyaltyReason(q) {
  return pickPOSReason('Loyalty Redemption', 'Manager approval needed',
    `${q.points} point(s) (${Number(q.value).toFixed(2)} off) needs a ${q.approver_role || 'manager'}'s approval. Choose the reason; the request goes to Approvals.`);
}

// posReasonCodes lists the active reason codes of one category.
async function posReasonCodes(category) {
  const res = await apiFetch('/api/v1/doc/ReasonCode?limit=500');
  if (!res || !res.ok) return [];
  const payload = await res.json();
  const docs = Array.isArray(payload) ? payload : (payload.data || payload.documents || []);
  return docs.map(d => ({ id: d.id, ...(d.data || d) })).filter(d => d.category === category && (d.status || 'Active') === 'Active');
}

function posReasonSelect(codes) {
  return `<select class="form-select" data-reason><option value="">Choose a reason...</option>${codes.map(c => `<option value="${cfgEsc(c.id)}">${cfgEsc(c.code || c.id)}${c.description ? ` - ${cfgEsc(c.description)}` : ''}</option>`).join('')}</select>`;
}

// pickPOSReason asks for a reason code of one category. With none set up the
// cashier is told who adds them, rather than shown an empty list.
async function pickPOSReason(category, title, message) {
  const codes = await posReasonCodes(category);
  if (!codes.length) {
    await showCustomAlert(`This needs a reason, and no ${category} reason codes exist yet. Ask a manager to add one under Setup -> Reason Codes.`, title);
    return null;
  }
  const picked = await posFormDialog(title, message, posReasonSelect(codes), extra => extra.querySelector('[data-reason]').value);
  if (picked === null) return null;
  if (!picked) {
    await showCustomAlert('Choose a reason to go on.', title);
    return null;
  }
  return picked;
}

// --- Stage 73.2.7: the drawer ---------------------------------------------------
//
// Closing a session, cash in and out between sales, and handing the till to
// the next cashier. The server keeps the drawer's figures; this shows them.

function posCashBreakdown(p) {
  return `Float ${Number(p.opening_cash).toFixed(2)} + cash sales ${Number(p.cash_sales).toFixed(2)} - cash refunds ${Number(p.cash_refunds).toFixed(2)}`
    + ` + paid in ${Number(p.paid_in).toFixed(2)} - paid out ${Number(p.paid_out).toFixed(2)} - safe drops ${Number(p.safe_drops).toFixed(2)}`
    + (p.advances_in || p.advances_out ? ` + advances ${Number(p.advances_in || 0).toFixed(2)} - advance refunds ${Number(p.advances_out || 0).toFixed(2)}` : '')
    + ` = ${Number(p.expected_cash).toFixed(2)} expected.`
    // Stage 85.E5: what the session's bills took by each tender - card and
    // UPI to tally with the terminal and the bank, cash counted above.
    + (p.tenders && Object.keys(p.tenders).length ? `
By tender: ${Object.entries(p.tenders).map(([k, v]) => `${k} ${Number(v).toFixed(2)}`).join(' · ')}.` : '');
}

export async function closePOSSessionFlow(t) {
  till = t;
  const sessionId = till.session;
  if (!sessionId) return;
  // 20.13: never close while sales are still waiting to sync, so a synced sale
  // can never land against the next session's cash figures.
  const stillQueued = getOfflineQueue().length;
  if (stillQueued > 0) {
    await showCustomAlert(`${stillQueued} sale${stillQueued === 1 ? '' : 's'} still need${stillQueued === 1 ? 's' : ''} to sync before this session can close. Reconnect and try again, or click the offline badge above to sync now.`, 'Offline Sales Pending');
    return;
  }
  const countedStr = await showCustomPrompt('Counted cash in the till?', '', 'Close Session', 'number');
  if (countedStr === null) return;
  const counted = parseFloat(countedStr);
  const send = (reason) => apiFetch('/api/v1/pos/session/close', {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId, counted_cash: isNaN(counted) ? 0 : counted, variance_reason: reason || '' })
  });
  let res = await send('');
  if (!res) return;
  if (!res.ok) {
    let body = {};
    try { body = await res.clone().json(); } catch (e) {}
    if (body.code !== 'POSOFF-0240') { await showApiError(res, 'Failed to close session.'); return; }
    // The count is off by more than the tolerance: say by how much, and ask why.
    const reason = await showCustomPrompt(`${body.detail || body.error || 'The count is off by more than the tolerance.'}\n\nWhy is it different? A manager approves this close.`, '', 'Cash Variance');
    if (!reason || !reason.trim()) return;
    res = await send(reason.trim());
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to close session.'); return; }
  }
  const data = await res.json();
  let msg = `Expected: ${Number(data.expected_cash).toFixed(2)}, Counted: ${Number(data.counted_cash).toFixed(2)}, Variance: ${Number(data.variance).toFixed(2)}`;
  if (data.status === 'Pending Approval') {
    msg = `The count is off by more than the tolerance, so a ${data.required_role || 'manager'} has to accept it under Approvals before the session counts as closed. Nothing more can be sold on it.\n\n${msg}`;
  }
  // 24.36: queued offline sales that never arrived, flagged for review.
  if (data.offline_queue_gap && data.offline_queue_gap.length > 0) {
    msg += `\n\nWarning: ${data.offline_queue_gap.length} offline sale(s) were queued but never synced (${data.offline_queue_gap.join(', ')}). This has been flagged for manager review.`;
  }
  await showCustomAlert(msg, data.status === 'Pending Approval' ? 'Waiting for Approval' : 'Session Closed');
  await till.refreshSession();
}

async function openPOSCashMovement() {
  const sessionId = till.session;
  if (!sessionId) {
    await showCustomAlert('Open a session first - cash moves in and out of an open session.', 'Cash in / out');
    return;
  }
  const sumRes = await apiFetch(`/api/v1/pos/session/summary?session_id=${encodeURIComponent(sessionId)}`);
  const pos = sumRes && sumRes.ok ? await sumRes.json() : null;
  const codes = await posReasonCodes('Cash Movement');
  if (!codes.length) {
    await showCustomAlert('Cash movements need a reason, and no Cash Movement reason codes exist yet. Ask a manager to add one under Setup -> Reason Codes.', 'Cash in / out');
    return;
  }
  const html = `<div class="form-group" style="margin-top:8px;"><label class="form-label">Type</label>
      <select class="form-select" data-cm="type"><option>Paid In</option><option>Paid Out</option><option>Safe Drop</option></select></div>
    <div class="form-group"><label class="form-label">Amount</label><input type="number" min="0" step="0.01" class="form-input" data-cm="amount" placeholder="0.00"></div>
    <div class="form-group"><label class="form-label">Reason</label>${posReasonSelect(codes)}</div>
    <div class="form-group"><label class="form-label">Reference (voucher, bag no.)</label><input class="form-input" data-cm="ref"></div>`;
  const picked = await posFormDialog('Cash in / out',
    `${pos ? posCashBreakdown(pos) + ' ' : ''}Paid In: cash brought in from the safe. Paid Out: a petty expense paid from the till. Safe Drop: cash taken to the safe.`,
    html, extra => ({
      movement_type: extra.querySelector('[data-cm="type"]').value,
      amount: parseFloat(extra.querySelector('[data-cm="amount"]').value) || 0,
      reason_code: extra.querySelector('[data-reason]').value,
      reference: extra.querySelector('[data-cm="ref"]').value.trim()
    }));
  if (!picked) return;
  const res = await apiFetch('/api/v1/pos/session/cash-movement', { method: 'POST', body: JSON.stringify({ session_id: sessionId, ...picked }) });
  if (!res || !res.ok) { await showApiError(res, 'The cash movement was not recorded.'); return; }
  const data = await res.json();
  await showCustomAlert(`${picked.movement_type} of ${picked.amount.toFixed(2)} recorded.${data.position ? '\n\n' + posCashBreakdown(data.position) : ''}`, 'Cash in / out');
}

async function openPOSTakeover() {
  if (till.session) {
    await showCustomAlert('You already have this till. To hand it over, the next cashier signs in here and chooses Take over this till.', 'Take over');
    return;
  }
  if (!till.location) {
    await showCustomAlert('Pick the store first.', 'Take over');
    return;
  }
  const countedStr = await showCustomPrompt('Count the cash in the till and type it here. That closes the current cashier\'s session against your count and opens yours with it as the float.', '', 'Take over this till', 'number');
  if (countedStr === null) return;
  const counted = parseFloat(countedStr) || 0;
  const send = (reason) => apiFetch('/api/v1/pos/session/takeover', { method: 'POST', body: JSON.stringify({ location: till.location, counted_cash: counted, variance_reason: reason || '' }) });
  let res = await send('');
  if (res && !res.ok) {
    let body = {};
    try { body = await res.clone().json(); } catch (e) {}
    if (body.code === 'POSOFF-0240') {
      const reason = await showCustomPrompt(`${body.detail || body.error}\n\nWhy is it different? A manager approves the outgoing cashier's close.`, '', 'Cash Variance');
      if (!reason || !reason.trim()) return;
      res = await send(reason.trim());
    }
  }
  if (!res || !res.ok) { await showApiError(res, 'The till was not taken over.'); return; }
  const data = await res.json();
  const prev = data.previous || {};
  await showCustomAlert(`The till is yours, with ${counted.toFixed(2)} as your float. The previous session closed with a variance of ${Number(prev.variance || 0).toFixed(2)}${prev.status === 'Pending Approval' ? ' and waits for a manager under Approvals' : ''}.`, 'Take over');
  await till.refreshSession();
}

// posFormDialog shows the shared dialog (the one showCustomPrompt uses) with a
// small form in its extra area; collect(extraEl) reads the form on OK, before
// the dialog is cleared. Resolves the collected value, or null on cancel.
export function posFormDialog(title, message, html, collect, onOpen) {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    const extraEl = document.getElementById('custom-dialog-extra');
    document.getElementById('custom-dialog-title').textContent = title;
    document.getElementById('custom-dialog-message').textContent = message;
    extraEl.innerHTML = html;
    extraEl.classList.remove('hidden');
    document.getElementById('custom-dialog-cancel-btn').style.display = '';
    backdrop.classList.remove('hidden');
    if (onOpen) onOpen(extraEl);
    const done = (value) => {
      backdrop.classList.add('hidden');
      extraEl.innerHTML = '';
      extraEl.classList.add('hidden');
      ['custom-dialog-ok-btn', 'custom-dialog-cancel-btn', 'custom-dialog-close-btn'].forEach(id => {
        const b = document.getElementById(id);
        b.replaceWith(b.cloneNode(true));
      });
      resolve(value);
    };
    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => done(collect(extraEl)));
    document.getElementById('custom-dialog-cancel-btn').addEventListener('click', () => done(null));
    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => done(null));
  });
}

export async function openPOSPackaging(t) {
  till = t;
  const rows = till.packaging.filter(p => p.override_allowed && ((p.choices || []).length > 1 || p.none_allowed));
  if (!rows.length) return;
  const html = rows.map(p => {
    const key = till.key(p);
    const current = till.choice(p);
    const line = posCart.find(l => l.sku === p.for_sku);
    const opts = (p.choices || []).map(c =>
      `<option value="${cfgEsc(c.component)}" ${c.component === current.component ? 'selected' : ''}>${cfgEsc(c.name || c.component)}${c.component === p.default_component ? ' (default)' : ''}</option>`).join('')
      + (p.none_allowed ? `<option value="" ${current.component === '' ? 'selected' : ''}>None</option>` : '');
    const o = till.overrides[key];
    return `<div data-key="${cfgEsc(key)}" style="display:grid; grid-template-columns: 1fr 1fr; gap:6px; margin-top:10px;">
      <div style="grid-column: 1 / -1; font-size:13px; font-weight:600;">${cfgEsc(line ? line.name : p.for_sku)} <span style="font-weight:400; color:var(--text-muted);">&middot; ${cfgEsc(p.group)} &middot; ${p.qty}</span></div>
      <select class="form-input" data-pkg="choice">${opts}</select>
      <input class="form-input" data-pkg="reason" placeholder="Reason${p.reason_required ? ' (needed if changed)' : ''}" value="${cfgEsc(o ? o.reason : '')}">
    </div>`;
  }).join('');
  const picked = await posFormDialog('Packaging', 'Each item already has its usual packaging. Change it only if the customer gets something else.', html,
    extra => Array.from(extra.querySelectorAll('[data-key]')).map(el => ({
      key: el.dataset.key,
      component: el.querySelector('[data-pkg="choice"]').value,
      reason: el.querySelector('[data-pkg="reason"]').value.trim()
    })));
  if (!picked) return;
  const missing = [];
  picked.forEach(pick => {
    const p = rows.find(r => till.key(r) === pick.key);
    if (!p) return;
    if (pick.component === p.default_component) {
      delete till.overrides[pick.key];
      return;
    }
    if (p.reason_required && !pick.reason) {
      missing.push(`${p.for_sku} (${p.group})`);
      return;
    }
    const choice = (p.choices || []).find(c => c.component === pick.component);
    till.overrides[pick.key] = { sku: p.for_sku, group: p.group, component: pick.component, reason: pick.reason, name: choice ? choice.name : '' };
  });
  till.renderPackaging();
  if (missing.length) {
    await showCustomAlert(`A reason is needed to change: ${missing.join(', ')}. Those kept their usual packaging.`, 'Packaging');
  }
}

// --- Stage 73: the More menu ------------------------------------------------
//
// The till's occasional actions sit behind one More... button so the first
// screen stays inside the cold-core budget. Each entry is a function here.
const POS_MORE_ACTIONS = [
  ['split', 'Split payment', () => openPOSSplitPayment()],
  ['cash', 'Cash in / out (paid in, paid out, safe drop)', () => openPOSCashMovement()],
  ['takeover', 'Take over this till (shift handover)', () => openPOSTakeover()],
  ['hold', 'Hold this bill', () => holdPOSBill()],
  ['resume', 'Resume a held bill', () => resumePOSBill()],
  ['exchange', 'Exchange (take items back against this bill)', () => openPOSExchange()],
  // Stage 85.E8: a bill's lines from a CSV (Barcode, Qty).
  ['upload', 'Upload lines (CSV)', () => uploadPOSLines()],
  // Stage 130.11 (49.14.3): a shared till - each person by their own PIN.
  ['switch', 'Switch staff (PIN) - the bill on screen is kept', () => switchPOSStaff(false)],
  ['pin', 'Set my PIN for this till', () => setMyPOSPin()],
  ['tillpins', 'Register this till for staff PINs (store manager)', () => registerPOSTillForPINs()]
];

// --- Stage 130.11 (49.14.3, user decision 2026-10-22): fast staff switch ----
// A shared till is registered once by a store manager, the way a store phone
// is (Stage 102): it then holds a device token, and staff switch in with their
// own name and PIN (/api/v1/store-app/pin/login) - no shared password, and a
// supervisor's override is made under the supervisor's own PIN, then the
// cashier switches back. The bill on screen stays: only the session changes.
// After a few idle minutes the till locks until someone enters their PIN.
const TILL_DEVICE_KEY = 'erp_till_device_token';
const tillDeviceToken = () => { try { return localStorage.getItem(TILL_DEVICE_KEY) || ''; } catch (_) { return ''; } };
let posIdleMinutes = 5;
let posIdleTimer = null;
let posLocked = false;

async function registerPOSTillForPINs() {
  if (!posIsManager()) { await showCustomAlert('A store manager registers the till for staff PINs.', 'Staff PINs'); return; }
  const res = await apiFetch('/api/v1/store-app/devices', { method: 'POST', body: JSON.stringify({ label: `Till ${posLocation || ''}`.trim() }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The till was not registered.', 'Staff PINs'); return; }
  const r = await res.json();
  try { localStorage.setItem(TILL_DEVICE_KEY, r.device_token); } catch (_) { /* storage blocked: nothing to keep it in */ }
  await showCustomAlert('This till is registered. Each person sets their PIN once (More… → Set my PIN), then switches in with it (More… → Switch staff). The till locks after a few idle minutes.', 'Staff PINs');
  startPOSIdleLock();
}

async function setMyPOSPin() {
  const picked = await posFormDialog('Set my PIN', 'Your PIN signs you in at a registered till or store phone. Store roles only - an administrator uses their password.',
    `<input class="form-input" data-pin="pass" type="password" placeholder="Your password" autocomplete="current-password" style="margin-top:10px;">
     <input class="form-input" data-pin="pin" type="password" inputmode="numeric" placeholder="New PIN (4-8 digits)" autocomplete="new-password" style="margin-top:8px;">`,
    extra => ({ password: extra.querySelector('[data-pin="pass"]').value, pin: extra.querySelector('[data-pin="pin"]').value.trim() }),
    extra => extra.querySelector('[data-pin="pass"]')?.focus());
  if (!picked) return;
  const res = await apiFetch('/api/v1/store-app/pin', { method: 'POST', body: JSON.stringify(picked) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Your PIN was not set.', 'Set my PIN'); return; }
  showToast('Your PIN is set.', { variant: 'success' });
}

async function switchPOSStaff(locked) {
  const device = tillDeviceToken();
  if (!device) { await showCustomAlert('A store manager first registers this till for staff PINs (More… → Register this till for staff PINs).', 'Switch staff'); return false; }
  const title = locked ? 'Till locked' : 'Switch staff';
  const picked = await posFormDialog(title, locked ? 'Enter your name and PIN to carry on - the bill on screen is kept.' : 'Who is at the till? Their PIN signs them in here; the bill on screen is kept.',
    `<input class="form-input" data-sw="user" placeholder="Username" autocomplete="off" style="margin-top:10px;">
     <input class="form-input" data-sw="pin" type="password" inputmode="numeric" placeholder="PIN" autocomplete="off" style="margin-top:8px;">`,
    extra => ({ user: extra.querySelector('[data-sw="user"]').value.trim(), pin: extra.querySelector('[data-sw="pin"]').value.trim() }),
    extra => extra.querySelector('[data-sw="user"]')?.focus());
  if (!picked || !picked.user || !picked.pin) return false;
  // A plain fetch: a wrong PIN is a 401, which apiFetch would treat as a lost session.
  const res = await fetch('/api/v1/store-app/pin/login', { method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Tenant-ID': localStorage.getItem('erp_tenant_id') || 'default' },
    body: JSON.stringify({ device_token: device, username: picked.user, pin: picked.pin }) }).catch(() => null);
  if (!res || !res.ok) {
    const msg = res ? ((await res.json().catch(() => ({}))).error || 'Wrong name or PIN.') : 'The till is offline.';
    if (res && res.status === 401 && /no longer registered/i.test(msg)) { try { localStorage.removeItem(TILL_DEVICE_KEY); } catch (_) {} }
    await showCustomAlert(msg, title);
    return false;
  }
  const s = await res.json();
  localStorage.setItem('erp_token', s.token);
  localStorage.setItem('erp_role', s.role);
  localStorage.setItem('erp_username', s.user);
  posIdleMinutes = Number(s.idle_minutes) || 5;
  await Promise.all([window.fetchAndApplyPermissions?.(), window.fetchAndApplyProfile?.()].map(p => Promise.resolve(p).catch(() => {})));
  if (typeof updateSidebarUserInfo === 'function') updateSidebarUserInfo();
  showToast(`${s.user} is at the till.`, { variant: 'success' });
  startPOSIdleLock();
  return true;
}

function bumpPOSIdle() {
  if (posLocked) return;
  clearTimeout(posIdleTimer);
  posIdleTimer = setTimeout(lockPOSTill, posIdleMinutes * 60000);
}

async function lockPOSTill() {
  if (posLocked || !tillDeviceToken() || !document.getElementById('pos-checkout-btn')) return;
  posLocked = true;
  // Kept across a reload, so reloading the page does not get past the lock.
  try { localStorage.setItem('erp_till_locked', '1'); } catch (_) {}
  let ok = false;
  while (!ok) {
    ok = await switchPOSStaff(true);
    if (!ok && !tillDeviceToken()) break;
  }
  posLocked = false;
  try { localStorage.removeItem('erp_till_locked'); } catch (_) {}
  bumpPOSIdle();
}

// startPOSIdleLock arms the idle lock on a registered till.
export function startPOSIdleLock() {
  if (!tillDeviceToken()) return;
  if (!window.__posIdleWired) {
    window.__posIdleWired = true;
    ['pointerdown', 'keydown'].forEach(ev => document.addEventListener(ev, bumpPOSIdle, { passive: true }));
  }
  let lockedBefore = false;
  try { lockedBefore = localStorage.getItem('erp_till_locked') === '1'; } catch (_) {}
  if (lockedBefore) { lockPOSTill(); return; }
  bumpPOSIdle();
}

// uploadPOSLines adds uploaded lines to the cart the way a scan does (price
// and stock from the server), then sets each line's quantity.
async function uploadPOSLines() {
  const pickers = await loadViewModule('/view-pickers.js?v=1');
  pickers.openLineUpload({ title: 'Upload bill lines', template: 'pos-bill', onRows: async (rows) => {
    for (const r of rows) {
      const before = (posCart.find(l => l.sku === r.sku) || {}).qty || 0;
      await window.addResolvedItemToPOSCart(r.sku, r.name);
      const line = posCart.find(l => l.sku === r.sku);
      if (line) line.qty = before + r.qty;
    }
    window.renderPOSCartTable();
  } });
}

// Stage 73.2.6: exchange. The items coming back are returned (received and
// inspected here, at the counter); their value becomes a credit that pays
// this bill. The customer pays only the difference - or gets it back.
async function openPOSExchange() {
  const infoEl = document.getElementById('pos-loyalty-info');
  if (!till.location) { infoEl.textContent = 'Pick the store first.'; return; }
  if (till.exchangeActive) { await showCustomAlert('This bill already has an exchange credit on it.', 'Exchange'); return; }
  const billNo = await showCustomPrompt('Bill number the items were bought on (from the receipt)?', '', 'Exchange');
  if (!billNo || !billNo.trim()) return;
  const eRes = await apiFetch(`/api/v1/returns/eligibility?original_order_id=${encodeURIComponent(billNo.trim())}`);
  if (!eRes || !eRes.ok) { await showApiError(eRes, 'That bill could not be looked up.'); return; }
  const elig = await eRes.json();
  const open = (elig.lines || []).filter(l => l.remaining_qty > 0);
  if (!elig.found || !open.length) { await showCustomAlert(elig.explanation || 'Nothing on that bill can be returned.', 'Exchange'); return; }
  const html = open.map(l => `<div data-ex="${cfgEsc(l.sku)}" style="display:grid; grid-template-columns: 2fr 1fr 1fr; gap:6px; margin-top:8px; align-items:center;">
      <span>${cfgEsc(l.sku)} <span style="color:var(--text-muted);">&middot; ${Number(l.unit_price).toFixed(2)} each, ${l.remaining_qty} can come back</span></span>
      <input type="number" min="0" max="${l.remaining_qty}" step="1" class="form-input" data-exq placeholder="Qty">
      <select class="form-select" data-exc><option>Sellable</option><option>Damaged</option><option>Repairable</option></select></div>`).join('');
  const picked = await posFormDialog('Exchange', `${elig.explanation || ''} Type how many of each are coming back and their condition.`, html,
    extra => Array.from(extra.querySelectorAll('[data-ex]')).map(el => ({ sku: el.dataset.ex, qty: parseInt(el.querySelector('[data-exq]').value, 10) || 0, cond: el.querySelector('[data-exc]').value })).filter(x => x.qty > 0));
  if (!picked || !picked.length) return;
  const res = await apiFetch('/api/v1/pos/exchange/start', {
    method: 'POST',
    body: JSON.stringify({
      original_order_id: billNo.trim(), location: till.location, idempotency_key: `exchange:${billNo.trim()}:${Date.now()}`,
      lines: picked.map(p => ({ sku: p.sku, qty: p.qty })), dispositions: Object.fromEntries(picked.map(p => [p.sku, p.cond]))
    })
  });
  if (!res || !res.ok) { await showApiError(res, 'The exchange could not be started.'); return; }
  const credit = await res.json();
  let row = document.getElementById('pos-exchange-row');
  if (!row) {
    row = document.createElement('span');
    row.id = 'pos-exchange-row';
    document.getElementById('pos-giftcard-row').after(row);
  }
  till.exchange = { id: credit.id, amount: Number(credit.amount) || 0 };
  infoEl.textContent = `Taken back against ${billNo.trim()}: ${Number(credit.amount).toFixed(2)} credit${credit.points_restored ? ` and ${credit.points_restored} points back to the customer` : ''}. Ring up what they take instead; they pay only the difference.`;
}

// Stage 73.2.5: hold and resume. A held bill keeps its bill number, so when it
// is resumed and paid it posts under that number, once. Points, a split and
// gift cards are not kept - add them again after resuming.
async function holdPOSBill() {
  const infoEl = document.getElementById('pos-loyalty-info');
  if (!posCart.length) {
    infoEl.textContent = 'There is nothing on the bill to hold.';
    return;
  }
  const note = await showCustomPrompt('Hold this bill? Add a note so it is easy to find (optional) - e.g. the customer\'s name.', '', 'Hold this bill');
  if (note === null) return;
  const customerId = document.getElementById('pos-customer').value.trim();
  const coupon = (document.getElementById('pos-coupon-code') || {}).value || '';
  const res = await apiFetch('/api/v1/pos/held-bills', {
    method: 'POST',
    body: JSON.stringify({
      cart_number: till.cart, location: till.location, customer_id: customerId, note: note.trim(),
      coupon_codes: coupon.split(/[,\s]+/).map(c => c.trim()).filter(Boolean),
      total: parseFloat(document.getElementById('pos-cart-total').textContent) || 0,
      lines: posCart.map(l => ({ sku: l.sku, name: l.name, qty: l.qty, unit_price: l.unpriced ? l.salePrice : 0 }))
    })
  });
  if (!res || !res.ok) { await showApiError(res, 'The bill was not held.'); return; }
  const bill = await res.json();
  till.clear();
  till.renderCart();
  document.getElementById('pos-loyalty-info').textContent = `Bill ${bill.cart_number} held (${bill.line_count} item line${bill.line_count === 1 ? '' : 's'}). Resume it from More... -> Resume a held bill.`;
}

async function resumePOSBill() {
  if (posCart.length) {
    await showCustomAlert('Finish or hold the bill on the till first - a held bill comes back onto an empty till.', 'Resume a held bill');
    return;
  }
  const listRes = await apiFetch(`/api/v1/pos/held-bills?location=${encodeURIComponent(till.location)}`);
  if (!listRes || !listRes.ok) { await showApiError(listRes, 'Held bills could not be listed.'); return; }
  const bills = (await listRes.json()).bills || [];
  if (!bills.length) {
    await showCustomAlert('No bills are held at this store.', 'Resume a held bill');
    return;
  }
  // Stage 102.4.2: a basket built on the store app is a held bill here too; its
  // number (PB-<store>-<code>) can be typed or scanned into Find a basket.
  const basketCode = (b) => String(b.cart_number || '').split('-').pop();
  const html = `<div class="form-group"><label class="form-label">Find a basket (type or scan its number)</label><input class="form-input" data-held="find" autocomplete="off"></div>`
    + bills.map((b, i) => `<label style="display:block; margin-top:8px;"><input type="radio" name="pos-held" value="${cfgEsc(b.id)}" ${i === 0 ? 'checked' : ''}>
      ${cfgEsc(b.note || b.customer_id || 'Walk-in')} &middot; ${cfgEsc(basketCode(b))} &middot; ${b.line_count} line(s) &middot; ${Number(b.total).toFixed(2)}
      <span style="color:var(--text-muted);">&middot; ${cfgEsc(b.cashier)}, ${cfgEsc(b.held_at || '')}</span></label>`).join('')
    + `<div class="form-group" style="margin-top:12px;"><label class="form-label">Do</label><select class="form-select" data-held="act"><option value="resume">Resume it on this till</option><option value="discard">Discard it (the customer is not coming back)</option></select></div>
       <input class="form-input" data-held="reason" placeholder="Reason (needed to discard)">`;
  const picked = await posFormDialog('Resume a held bill', 'Bills held at this store, oldest first.', html, extra => {
    const find = extra.querySelector('[data-held="find"]').value.trim().toUpperCase();
    const match = find ? bills.find(b => String(b.cart_number).toUpperCase() === find || basketCode(b).toUpperCase() === find) : null;
    return {
      id: find ? (match || {}).id : (extra.querySelector('input[name="pos-held"]:checked') || {}).value,
      find,
      act: extra.querySelector('[data-held="act"]').value,
      reason: extra.querySelector('[data-held="reason"]').value.trim()
    };
  });
  if (picked && picked.find && !picked.id) {
    await showCustomAlert(`No bill held at this store has the number ${picked.find}.`, 'Resume a held bill');
    return;
  }
  if (!picked || !picked.id) return;
  const res = await apiFetch(`/api/v1/pos/held-bills/${encodeURIComponent(picked.id)}/${picked.act}`, {
    method: 'POST', body: JSON.stringify({ location: till.location, reason: picked.reason })
  });
  if (!res || !res.ok) { await showApiError(res, picked.act === 'discard' ? 'The held bill was not discarded.' : 'The bill could not be resumed.'); return; }
  if (picked.act === 'discard') {
    document.getElementById('pos-loyalty-info').textContent = 'Held bill discarded.';
    return;
  }
  const bill = await res.json();
  till.cartNumber = bill.cart_number;
  const lines = [];
  for (const l of bill.lines || []) {
    const a = await apiFetch(`/api/v1/availability?sku=${encodeURIComponent(l.sku)}&location=${encodeURIComponent(till.location)}`);
    const av = a && a.ok ? await a.json() : {};
    lines.push({ sku: l.sku, name: l.name || l.sku, qty: l.qty, available: av.ats ?? av.available ?? 0,
      withheld: (av.qc_hold || 0) + (av.damaged || 0) + (av.blocked || 0) + (av.hold_qty || 0),
      salePrice: l.unit_price || 0, referencePrice: 0, priceSource: '', overrideId: '', unpriced: !!l.unit_price });
  }
  posCart = lines;
  const coupon = document.getElementById('pos-coupon-code');
  if (coupon) coupon.value = (bill.coupon_codes || []).join(' ');
  const cust = document.getElementById('pos-customer');
  const custDisplay = document.getElementById('pos-customer-display');
  if (cust && bill.customer_id) {
    cust.value = bill.customer_id;
    if (custDisplay) custDisplay.value = bill.customer_id;
    cust.dispatchEvent(new Event('change'));
  }
  till.renderCart();
  document.getElementById('pos-loyalty-info').textContent = `Bill ${bill.cart_number} resumed - prices are checked again. Add any points, split or gift card again before completing.`;
}

export async function openPOSMore(t) {
  till = t;
  const html = POS_MORE_ACTIONS.map(([key, label], i) =>
    `<label style="display:block; margin-top:8px;"><input type="radio" name="pos-more" value="${key}" ${i === 0 ? 'checked' : ''}> ${cfgEsc(label)}</label>`).join('');
  const picked = await posFormDialog('More', 'Choose what to do.', html,
    extra => (extra.querySelector('input[name="pos-more"]:checked') || {}).value || null);
  const action = POS_MORE_ACTIONS.find(([key]) => key === picked);
  if (action) await action[2]();
}

// Stage 73.1.6: split payment. The cashier enters the fixed parts (say Card
// 1,200 and UPI 300); the Payment Mode the till shows takes whatever is left,
// so the split always adds up to the bill. The server checks it again and
// posts each part to its own clearing account, and a later return refunds
// each part back the way it was paid.
async function openPOSSplitPayment() {
  const infoEl = document.getElementById('pos-loyalty-info');
  if (!posCart.length) {
    infoEl.textContent = 'Add items to the cart before splitting the payment.';
    return;
  }
  const due = parseFloat(document.getElementById('pos-cart-total').textContent) || 0;
  const rest = document.getElementById('pos-payment-mode').value;
  // Stage 85.E2: the store's own tenders (each posts by its kind).
  const custMod = await import('./view-pos-customer.js' + new URL(import.meta.url).search).catch(() => null);
  const tenders = (custMod && custMod.posStoreTenders().length) ? custMod.posStoreTenders() : ['Cash', 'Card', 'UPI'].map(k => ({ name: k, kind: k }));
  const current = Object.fromEntries((till.split || []).map(s => [s.name || s.mode, s.amount]));
  const html = tenders.map((t, i) => `<div class="form-group" style="margin-top:8px;">
      <label class="form-label" for="pos-split-${i}">${cfgEsc(t.name)}</label>
      <input type="number" min="0" step="0.01" id="pos-split-${i}" class="form-input" data-split="${cfgEsc(t.kind)}" data-name="${cfgEsc(t.name)}" placeholder="0.00" value="${current[t.name] ? current[t.name].toFixed(2) : ''}">
    </div>`).join('');
  const picked = await posFormDialog('Split payment',
    `The bill is ${due.toFixed(2)}. Enter what is paid each way; whatever is left is taken by ${rest} (the Payment Mode). Leave all blank to pay one way.`,
    html, extra => Array.from(extra.querySelectorAll('[data-split]')).map(el => ({ mode: el.dataset.split, name: el.dataset.name, amount: Math.round((parseFloat(el.value) || 0) * 100) / 100 })).filter(s => s.amount > 0));
  if (picked === null) return;
  const fixed = picked.reduce((s, p) => s + p.amount, 0);
  if (fixed - due > 0.004) {
    await showCustomAlert(`Those add up to ${fixed.toFixed(2)}, more than the ${due.toFixed(2)} bill. Lower one of them.`, 'Split payment');
    return;
  }
  till.split = picked;
  renderPOSSplitRow(picked);
}

function renderPOSSplitRow(parts) {
  let row = document.getElementById('pos-split-row');
  if (!parts.length) { if (row) row.remove(); return; }
  if (!row) {
    row = document.createElement('span');
    row.id = 'pos-split-row';
    document.getElementById('pos-giftcard-row').after(row);
  }
  row.textContent = `Split: ${parts.map(p => `${p.name || p.mode} ${p.amount.toFixed(2)}`).join(' + ')}; the rest by Payment Mode`;
}

// --- Stage 72.8: gift cards --------------------------------------------------
//
// A gift card is stored value, not merchandise: it is never a bill line.
// Pay - takes part or all of THIS bill off a card (checked now, redeemed by the
// server inside the sale). Issue - loads a card once the customer has paid, on
// its own receipt. Balance - what is left. A manager also blocks / releases a
// card, cancels an unused issue, and registers new cards.

function posIsManager() {
  const role = localStorage.getItem('erp_role') || '';
  return role === 'Store Manager' || (typeof isAdminRoleName === 'function' && isAdminRoleName(role));
}

export async function openPOSGiftCard(t) {
  till = t;
  const manager = posIsManager();
  const actions = [['pay', 'Pay this bill with a gift card'], ['issue', 'Issue / add value (customer pays now)'], ['balance', 'Check balance']];
  if (till.tenders.length) actions.push(['remove', 'Remove the gift cards from this bill']);
  if (manager) actions.push(['block', 'Block a card'], ['unblock', 'Release a blocked card'], ['cancel', 'Cancel an unused issue (refund)'], ['register', 'Register new cards']);
  // Stage 130.8: a lost card's number, shown again to an administrator only.
  if (typeof isAdminRoleName === 'function' && isAdminRoleName(localStorage.getItem('erp_role') || '')) actions.push(['reveal', "Show a lost card's full number"]);
  const mode = (document.getElementById('pos-payment-mode') || {}).value || 'Cash';
  const html = `
    <div style="display:grid; gap:8px; margin-top:10px;">
      <select class="form-input" data-gc="action">${actions.map(([v, l]) => `<option value="${v}">${cfgEsc(l)}</option>`).join('')}</select>
      <input class="form-input" data-gc="card" data-show="pay issue balance block unblock cancel" placeholder="Scan or type the card number" autocomplete="off">
      <input class="form-input" data-gc="amount" data-show="pay issue" type="number" min="0" step="0.01" placeholder="Amount (pay: blank = as much as the card covers)">
      <select class="form-input" data-gc="mode" data-show="issue">${['Cash', 'Card', 'UPI'].map(m => `<option ${m === mode ? 'selected' : ''}>${m}</option>`).join('')}</select>
      <input class="form-input" data-gc="reason" data-show="block unblock cancel" placeholder="Reason">
      <input class="form-input" data-gc="cardid" data-show="reveal" placeholder="Card ID (Reports > Gift Card Liability)" autocomplete="off">
      <input class="form-input" data-gc="profile" data-show="register" value="Standard Gift Card" placeholder="Gift card profile">
      <input class="form-input" data-gc="generate" data-show="register" type="number" min="0" step="1" placeholder="How many new numbers to generate (or list printed numbers below)">
      <textarea class="form-input" data-gc="numbers" data-show="register" rows="3" placeholder="Printed card numbers, one per line"></textarea>
    </div>`;
  const syncFields = (extra) => {
    const action = extra.querySelector('[data-gc="action"]').value;
    extra.querySelectorAll('[data-show]').forEach(el => { el.style.display = el.dataset.show.split(' ').includes(action) ? '' : 'none'; });
  };
  const picked = await posFormDialog('Gift Card', 'A gift card is stored value - it is never a line on the bill.', html,
    extra => {
      const v = k => (extra.querySelector(`[data-gc="${k}"]`) || {}).value || '';
      return { action: v('action'), card: v('card').trim(), amount: parseFloat(v('amount')) || 0, mode: v('mode'), reason: v('reason').trim(), profile: v('profile').trim(), generate: parseInt(v('generate'), 10) || 0, numbers: v('numbers'), cardId: v('cardid').trim() };
    },
    extra => {
      syncFields(extra);
      extra.querySelector('[data-gc="action"]').addEventListener('change', () => syncFields(extra));
      const cardEl = extra.querySelector('[data-gc="card"]');
      if (cardEl) cardEl.focus();
    });
  if (!picked) return;
  switch (picked.action) {
    case 'pay': return posPayWithGiftCard(picked);
    case 'issue': return posIssueGiftCard(picked);
    case 'balance': return posGiftCardBalance(picked.card);
    case 'remove':
      till.tenders = [];
      till.renderCart(true);
      return;
    case 'register': return posRegisterGiftCards(picked);
    default: return posGiftCardAction(picked);
  }
}

async function posGiftCardBalanceOf(card) {
  const res = await apiFetch('/api/v1/pos/gift-card/balance', { method: 'POST', body: JSON.stringify({ card_number: card }) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'This gift card could not be checked.', 'Gift Card'); return null; }
  return res.json();
}

async function posGiftCardBalance(card) {
  if (!card) return;
  const v = await posGiftCardBalanceOf(card);
  if (!v) return;
  await showCustomAlert(`${v.card} - ${v.status}\nBalance ${Number(v.balance).toFixed(2)} of ${Number(v.loaded).toFixed(2)} loaded${v.expires_on ? `\nValid until ${v.expires_on}` : ''}${v.part_use_allowed ? '' : '\nMust be used in full in one bill'}`, 'Gift Card Balance');
}

async function posPayWithGiftCard(p) {
  if (!posCart.length) { await showCustomAlert('Add the items to the bill first, then pay with the gift card.', 'Gift Card'); return; }
  if (!p.card) return;
  const v = await posGiftCardBalanceOf(p.card);
  if (!v) return;
  if (v.status !== 'Active' && v.status !== 'Partially redeemed') {
    await showCustomAlert(`${v.card} is ${v.status.toLowerCase()} and cannot pay.`, 'Gift Card');
    return;
  }
  // What is still due, giving back this card's own earlier amount if it is being re-applied.
  const sameCard = till.tenders.filter(g => g.card_number === p.card).reduce((s, g) => s + g.amount, 0);
  const due = Math.max(0, (parseFloat(document.getElementById('pos-cart-total').textContent) || 0) + sameCard);
  let amount = p.amount > 0 ? p.amount : Math.min(Number(v.balance), due);
  if (!v.part_use_allowed) amount = Number(v.balance);
  if (amount <= 0 || amount > Number(v.balance) || amount > due + 0.001) {
    await showCustomAlert(`${v.card} has ${Number(v.balance).toFixed(2)}; ${due.toFixed(2)} is still due. Choose an amount within both.`, 'Gift Card');
    return;
  }
  till.tenders = till.tenders.filter(g => g.card_number !== p.card);
  till.tenders.push({ card_number: p.card, card: v.card, amount: Math.round(amount * 100) / 100 });
  till.renderCart(true);
  showToast(`${v.card}: ${amount.toFixed(2)} will be taken when the sale completes.`, { variant: 'success', title: 'Gift Card' });
}

async function posIssueGiftCard(p) {
  if (!posLocation) { await showCustomAlert('Choose the store first.', 'Gift Card'); return; }
  if (!p.card || !(p.amount > 0)) { await showCustomAlert('Scan the card and enter the amount the customer is paying.', 'Gift Card'); return; }
  const ok = await showCustomConfirm(`Load ${p.amount.toFixed(2)} on card ending ${p.card.slice(-4)}, paid by ${p.mode}? Take the payment first - the card is funded only now.`, 'Issue Gift Card');
  if (!ok) return;
  const key = `gc-${posLocation}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const res = await apiFetch('/api/v1/pos/gift-card/issue', { method: 'POST', body: JSON.stringify({
    card_number: p.card, amount: p.amount, payment_mode: p.mode, location: posLocation,
    customer_id: (document.getElementById('pos-customer') || {}).value || '', idempotency_key: key }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The gift card was not issued - nothing was charged to it.', 'Gift Card'); return; }
  const r = await res.json();
  posPrintGiftCardReceipt(r);
}

// The issue receipt is separate from any sale bill (blueprint 16.1).
function posPrintGiftCardReceipt(r) {
  const store = till.storeLabel();
  const slip = `<div style="text-align:center; font-weight:bold;">GIFT CARD ${r.event === 'RELOAD' ? 'TOP-UP' : 'ISSUE'}</div>
    <div style="text-align:center;">${cfgEsc(store)}</div><hr>
    <div>Card: ${cfgEsc(r.card)}</div><div>Amount: ${Number(r.amount).toFixed(2)}</div><div>Balance: ${Number(r.balance).toFixed(2)}</div>
    ${r.expires_on ? `<div>Valid until: ${cfgEsc(r.expires_on)}</div>` : ''}<div>Paid by: ${cfgEsc(r.payment_mode || '')}</div>
    <div>Ref: GC-${r.entry_id}</div><div>${cfgEsc(new Date(r.at).toLocaleString())}</div><hr>
    <div style="font-size:11px;">Keep this card safe - its balance is spent by whoever presents it.</div>`;
  const browser = () => {
    const w = window.open('', '_blank', 'width=360,height=520');
    if (!w) { showCustomAlert(`${r.card} loaded with ${Number(r.amount).toFixed(2)}. Balance ${Number(r.balance).toFixed(2)}.`, 'Gift Card Issued'); return; }
    w.document.write(`<!doctype html><title>Gift Card</title><body style="font-family:monospace; font-size:13px; padding:12px;">${slip}
    <script>window.print();<\/script></body>`);
    w.document.close();
  };
  // Stage 127.2: on the receipt printer like the bill, no browser dialog once set up.
  import('./trade-docs.js' + new URL(import.meta.url).search)
    .then(docs => docs.printSlip(slip, `GC-${r.entry_id}`, browser))
    .catch(e => { console.debug('[POS] gift card slip unavailable, using the browser', e); browser(); });
}

async function posGiftCardAction(p) {
  if (!p.card && !(p.action === 'reveal' && p.cardId)) return;
  const res = await apiFetch('/api/v1/stored-value/cards/action', { method: 'POST', body: JSON.stringify({ card_number: p.card, action: p.action, reason: p.reason, location: posLocation, instrument_id: p.cardId }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'That did not go through.', 'Gift Card'); return; }
  const r = await res.json();
  if (p.action === 'reveal') {
    await showCustomAlert(`Card ${r.instrument_id} (${r.profile}): ${String(r.card_number).replace(/(.{4})/g, '$1 ').trim()}. This look-up is recorded in the audit log.`, 'Gift Card Number');
  } else if (p.action === 'cancel') {
    await showCustomAlert(`${r.card} cancelled. Refund ${Number(r.amount).toFixed(2)} by ${r.payment_mode || 'the original payment mode'}.`, 'Gift Card');
  } else {
    await showCustomAlert(`${r.card} is now ${r.status}.`, 'Gift Card');
  }
}

async function posRegisterGiftCards(p) {
  const numbers = p.numbers.split(/[\s,;]+/).map(s => s.trim()).filter(Boolean);
  const res = await apiFetch('/api/v1/stored-value/cards', { method: 'POST', body: JSON.stringify({ profile: p.profile, generate: numbers.length ? 0 : p.generate, numbers }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'No cards were registered.', 'Gift Card'); return; }
  const r = await res.json();
  if (r.numbers && r.numbers.length) {
    // Shown once, for printing - after this only an administrator can show a
    // number again, for a lost card (Stage 130.8).
    const w = window.open('', '_blank', 'width=420,height=600');
    if (w) {
      w.document.write(`<!doctype html><title>New gift cards</title><body style="font-family:monospace; padding:12px;"><h3>${r.registered} new ${cfgEsc(p.profile)} numbers</h3><p>Print them on the cards now - after this only an administrator can show a number again.</p><ol>${r.numbers.map(n => `<li>${cfgEsc(n.replace(/(.{4})/g, '$1 ').trim())}</li>`).join('')}</ol></body>`);
      w.document.close();
    }
  }
  await showCustomAlert(`${r.registered} card(s) registered as not yet issued.`, 'Gift Card');
}

// Stage 20.14: reuses the sticker-print-area's hidden-until-printing @media
// print pattern (styles.css) rather than a new PDF/print dependency. This is
// the fallback behind the 31.1.9 QZ path - what prints when QZ Tray is not
// running, or when no printer is set as Default For Receipt.
//
// 31.1.9 fix: offerDiscount was missing here. Checkout returns amount_due as
// sale_total - loyalty_discount - offer_discount, so a sale with a Stage 30.7
// offer applied printed a receipt whose total was higher than the cash
// actually collected. Kept as a defaulted parameter so the shape of the call
// is unchanged for anything that does not pass it.
export function printPOSReceipt(t, cartNumber, location, paymentMode, items, saleTotal, loyaltyDiscount = 0, offerDiscount = 0) {
  till = t;
  const posStoreProfile = till.storeProfile;
  const area = document.getElementById('receipt-print-area');
  if (!area) return;
  // Stage 53.12: name the product the way the customer recognises it. The code
  // stays as the fallback for a line whose name never resolved.
  const lines = items.map(it => `
    <div class="receipt-line"><span>${cfgEsc(it.name || it.sku)} x${it.qty}</span><span>${(it.qty * it.sale_price).toFixed(2)}</span></div>
  `).join('');
  // Stage 30.2.5: points spent on the sale are shown on the receipt as their
  // own line, so the customer can see what their points paid for and the
  // printed total matches what was actually collected.
  const subtotalLine = (loyaltyDiscount > 0 || offerDiscount > 0) ? `
      <div class="receipt-line"><span>Subtotal</span><span>${Number(saleTotal).toFixed(2)}</span></div>
  ` : '';
  const offerLine = offerDiscount > 0 ? `
      <div class="receipt-line"><span>Offer discount</span><span>-${Number(offerDiscount).toFixed(2)}</span></div>
  ` : '';
  const loyaltyLine = loyaltyDiscount > 0 ? `
      <div class="receipt-line"><span>Loyalty points applied</span><span>-${Number(loyaltyDiscount).toFixed(2)}</span></div>
  ` : '';
  const amountDue = Number(saleTotal) - Number(loyaltyDiscount) - Number(offerDiscount);
  // Stage 53.12: the header says WHICH SHOP and WHICH COMPANY, not just a
  // location code. Kept deliberately in step with BuildReceiptPayload's
  // ESC-POS header (engines/qz_payload.go) - a receipt that reads differently
  // depending on which printer it reached is the same class of defect as
  // 31.1.9's missing offer line.
  const p = posStoreProfile && posStoreProfile.code === location ? posStoreProfile : null;
  const storeName = p ? p.name : location;
  const header = `
      <div class="receipt-title">${cfgEsc(p && p.entityName ? p.entityName : 'Sales Receipt')}</div>
      ${p && p.entityName ? '<div>Sales Receipt</div>' : ''}
      <div>${cfgEsc(storeName)}</div>
      ${p && p.gstin ? `<div>GSTIN ${cfgEsc(p.gstin)}</div>` : ''}
      ${p && p.stateName ? `<div>${cfgEsc(p.stateName)}</div>` : ''}
      <div>${cfgEsc(cartNumber)}</div>
      <div>${new Date().toLocaleString()}</div>
      ${p && p.cashier ? `<div>Cashier: ${cfgEsc(p.cashier)}</div>` : ''}`;
  area.innerHTML = `
    <div class="receipt">
      <div class="receipt-header">
        ${header}
      </div>
      <hr>
      ${lines}
      <hr>
      ${subtotalLine}
      ${offerLine}
      ${loyaltyLine}
      <div class="receipt-line receipt-total"><span>Total (${cfgEsc(paymentMode)})</span><span>${amountDue.toFixed(2)}</span></div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}
