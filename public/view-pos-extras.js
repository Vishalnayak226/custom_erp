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
      <div class="form-group">
        <label class="form-label" for="pos-return-order-id">Original Bill / Cart Number</label>
        <input type="text" id="pos-return-order-id" class="form-input" placeholder="e.g. POS-HO-171..." autocomplete="off">
      </div>
      <div class="form-group pos-return-lookup">
        <button class="btn btn-outline" id="pos-return-lookup-btn" type="button">Look Up Bill</button>
      </div>
      <div class="form-group">
        <label class="form-label" for="pos-return-location-display">Return Location</label>
        <input type="text" id="pos-return-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
        <input type="hidden" id="pos-return-location" value="${posLocation}">
      </div>
      <div class="pos-return-total">
        <span>Refund if all accepted <b id="pos-return-total">0.00</b></span>
        <button class="btn btn-primary" id="pos-return-submit-btn" type="button" disabled>Raise Return</button>
      </div>
    </div>
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
  document.getElementById('pos-return-lookup-btn').addEventListener('click', lookUpPOSReturnBill);
  document.getElementById('pos-return-order-id').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      lookUpPOSReturnBill();
    }
  });
  document.getElementById('pos-return-submit-btn').addEventListener('click', submitPOSReturn);

  renderPOSReturnTable();
}

// lookUpPOSReturnBill asks the server what is returnable. Nothing is typed by
// the clerk except the bill number.
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
          ${posReturnEligibility.sale_date ? `<span style="color:var(--text-muted);"> Sold ${cfgEsc(posReturnEligibility.sale_date)}; return window ${posReturnEligibility.window_days} days.</span>` : ''}
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
  await showCustomAlert(
    data.replayed
      ? `This return was already raised as ${data.return_request_id}. Nothing was duplicated.`
      : `Return ${data.return_request_id} raised. It now needs approval, then the goods received and inspected before any refund is paid.`,
    'Return Raised');
}

// The quantity box in the Return table calls this through the data-act
// dispatcher, which only resolves names that are own properties of window.
Object.assign(window, { updatePOSReturnLine });

// quickAddPOSCustomer creates a customer from the till. The alternative is
// what the screen used to force: leave POS, go to the Customer master, create
// the record, come back, and find the cart gone - with a queue waiting.
//
// Deliberately asks for the two things a counter can actually get (a name and
// a phone number) and lets the server mint the code; everything else about the
// customer can be filled in later from the master.
export async function quickAddPOSCustomer() {
  const name = await showCustomPrompt('Customer name?', '', 'New Customer');
  if (name === null) return;
  if (!name.trim()) {
    await showCustomAlert('A customer needs a name.', 'New Customer');
    return;
  }
  const phone = await showCustomPrompt(`Phone number for ${name.trim()}? (optional)`, '', 'New Customer');
  if (phone === null) return;

  const res = await apiFetch('/api/v1/doc/Customer', {
    method: 'POST',
    body: JSON.stringify({ name: name.trim(), phone: phone.trim(), status: 'Active' })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'The customer was not created.');
    return;
  }
  const created = await res.json();
  const code = created.id || created.code || '';
  if (!code) {
    await showCustomAlert('The customer was created but the till could not read its code back - search for them by name.', 'New Customer');
    return;
  }
  const hidden = document.getElementById('pos-customer');
  const display = document.getElementById('pos-customer-display');
  if (display) { display.value = name.trim(); display.dataset.resolved = '1'; }
  if (hidden) {
    hidden.value = code;
    hidden.dispatchEvent(new Event('change', { bubbles: true }));
  }
  showToast(`${name.trim()} added and attached to this sale.`, { variant: 'success', title: 'New Customer' });
}

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
  const res = await apiFetch(`/api/v1/loyalty/ledger?customer_id=${encodeURIComponent(customerId)}`);
  if (!res) return;
  if (!res.ok) {
    infoEl.textContent = 'Failed to look up loyalty balance.';
    return;
  }
  const data = await res.json();
  infoEl.textContent = `${customerId} has ${data.balance} loyalty point(s).`;
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

  // Check the balance first so the cashier is told "you have N" rather than
  // finding out at the till.
  const balRes = await apiFetch(`/api/v1/loyalty/ledger?customer_id=${encodeURIComponent(customerId)}`);
  if (!balRes) return;
  if (!balRes.ok) {
    infoEl.textContent = 'Failed to look up loyalty balance.';
    return;
  }
  const balance = (await balRes.json()).balance || 0;
  if (balance <= 0) {
    infoEl.textContent = `${customerId} has no loyalty points to redeem.`;
    return;
  }

  const cartTotal = posCart.reduce((sum, line) => sum + line.qty * line.salePrice, 0);
  const pointsStr = await showCustomPrompt(`How many points to redeem? ${customerId} has ${balance}. 1 point = 1 off this sale.`);
  const points = parseInt(pointsStr, 10);
  if (!points || points <= 0) return;
  if (points > balance) {
    infoEl.textContent = `${customerId} only has ${balance} point(s).`;
    return;
  }
  if (points > cartTotal) {
    infoEl.textContent = `This sale is only ${cartTotal.toFixed(2)} - redeem at most ${Math.floor(cartTotal)} point(s).`;
    return;
  }

  // Nothing is burned here. The points ride on the cart and are spent by the
  // server only if the sale completes.
  till.redeemPoints = points;
  till.renderCart();
  infoEl.textContent = `${points} point(s) will be applied to this sale. They are only deducted once the sale completes.`;
}

// posFormDialog shows the shared dialog (the one showCustomPrompt uses) with a
// small form in its extra area; collect(extraEl) reads the form on OK, before
// the dialog is cleared. Resolves the collected value, or null on cancel.
function posFormDialog(title, message, html, collect, onOpen) {
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
  const mode = (document.getElementById('pos-payment-mode') || {}).value || 'Cash';
  const html = `
    <div style="display:grid; gap:8px; margin-top:10px;">
      <select class="form-input" data-gc="action">${actions.map(([v, l]) => `<option value="${v}">${cfgEsc(l)}</option>`).join('')}</select>
      <input class="form-input" data-gc="card" data-show="pay issue balance block unblock cancel" placeholder="Scan or type the card number" autocomplete="off">
      <input class="form-input" data-gc="amount" data-show="pay issue" type="number" min="0" step="0.01" placeholder="Amount (pay: blank = as much as the card covers)">
      <select class="form-input" data-gc="mode" data-show="issue">${['Cash', 'Card', 'UPI'].map(m => `<option ${m === mode ? 'selected' : ''}>${m}</option>`).join('')}</select>
      <input class="form-input" data-gc="reason" data-show="block unblock cancel" placeholder="Reason">
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
      return { action: v('action'), card: v('card').trim(), amount: parseFloat(v('amount')) || 0, mode: v('mode'), reason: v('reason').trim(), profile: v('profile').trim(), generate: parseInt(v('generate'), 10) || 0, numbers: v('numbers') };
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
  const w = window.open('', '_blank', 'width=360,height=520');
  if (!w) { showCustomAlert(`${r.card} loaded with ${Number(r.amount).toFixed(2)}. Balance ${Number(r.balance).toFixed(2)}.`, 'Gift Card Issued'); return; }
  const store = till.storeLabel();
  w.document.write(`<!doctype html><title>Gift Card</title><body style="font-family:monospace; font-size:13px; padding:12px;">
    <div style="text-align:center; font-weight:bold;">GIFT CARD ${r.event === 'RELOAD' ? 'TOP-UP' : 'ISSUE'}</div>
    <div style="text-align:center;">${cfgEsc(store)}</div><hr>
    <div>Card: ${cfgEsc(r.card)}</div><div>Amount: ${Number(r.amount).toFixed(2)}</div><div>Balance: ${Number(r.balance).toFixed(2)}</div>
    ${r.expires_on ? `<div>Valid until: ${cfgEsc(r.expires_on)}</div>` : ''}<div>Paid by: ${cfgEsc(r.payment_mode || '')}</div>
    <div>Ref: GC-${r.entry_id}</div><div>${cfgEsc(new Date(r.at).toLocaleString())}</div><hr>
    <div style="font-size:11px;">Keep this card safe - its balance is spent by whoever presents it.</div>
    <script>window.print();<\/script></body>`);
  w.document.close();
}

async function posGiftCardAction(p) {
  if (!p.card) return;
  const res = await apiFetch('/api/v1/stored-value/cards/action', { method: 'POST', body: JSON.stringify({ card_number: p.card, action: p.action, reason: p.reason, location: posLocation }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'That did not go through.', 'Gift Card'); return; }
  const r = await res.json();
  if (p.action === 'cancel') {
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
    // Shown once, for printing - the ERP never shows a full number again.
    const w = window.open('', '_blank', 'width=420,height=600');
    if (w) {
      w.document.write(`<!doctype html><title>New gift cards</title><body style="font-family:monospace; padding:12px;"><h3>${r.registered} new ${cfgEsc(p.profile)} numbers</h3><p>Print them on the cards now - they are not shown again.</p><ol>${r.numbers.map(n => `<li>${cfgEsc(n.replace(/(.{4})/g, '$1 ').trim())}</li>`).join('')}</ol></body>`);
      w.document.close();
    }
  }
  await showCustomAlert(`${r.registered} card(s) registered as not yet issued.`, 'Gift Card');
}
