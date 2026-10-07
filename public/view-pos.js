// BLD-041: native ES module for POS.
// Loaded with import() only after the route entitlement check succeeds.
// Shared app services and offline-queue boot helpers remain in the shell.

// ---------------------------------------------------------------------------
// Stage 53: this screen rebuilt as a standard retail till.
//
// What it was: a form. A box labelled "Customer Code (optional)", a box
// labelled "Scan or Enter SKU" that posted whatever string was in it straight
// to the availability API, a location dropdown that stayed editable all shift,
// and a Complete Sale button that emptied the cart behind a confirm dialog and
// left the screen looking exactly as it had a moment earlier.
//
// What a till actually does, and what this now does:
//
//   * It is BOUND to a store. Once a session is open the location is locked
//     and shown, not editable (posTerminalLocked). Reset Terminal unbinds it.
//   * Nobody knows a customer code. You search a person by name or phone, or
//     you add them at the counter in two fields, or - the common case - you
//     sell to nobody at all and the field reads Walk-in.
//   * A scanner scans and a human searches, through the same box. An exact
//     code or barcode rings straight up; anything else lists what matched.
//   * Stock that cannot be sold is shown as such, instead of being quietly
//     folded into one "Available" number.
//   * Cash has a tendered box and a change-due figure, because a cashier
//     counting change out of a drawer needs one.
//   * A finished sale STAYS on the screen, frozen, until the cashier starts
//     the next one. The bill the customer is looking at and the screen the
//     cashier is looking at are the same thing until New Sale is pressed.
// ---------------------------------------------------------------------------

// Stage 53.9: is the till bound to its store? Set when a session is confirmed
// open at posLocation, cleared by Reset Terminal. While true the location
// picker is read-only - a cashier cannot wander onto another store's stock
// mid-shift, which before this was one stray click.
let posTerminalLocked = false;

// Stage 53.10: the sale just completed, or null. While this is set the till is
// frozen: the completed bill is on screen and scanning, pricing and Complete
// Sale are all disabled until New Sale clears it. It is deliberately ONE
// variable rather than a flag plus a payload, so "is the till frozen" and
// "what is it frozen showing" can never disagree.
let posCompletedSale = null;

// Stage 53.6: the last product search's results, awaiting a pick. Held so the
// keyboard handler and the click handler resolve the same row.
let posSearchResults = [];
let posSearchActiveIndex = -1;

function renderPOSView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">POS / Billing</h1>
      <p class="page-subtitle">Scan a barcode or search by name to add items, then take payment.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div id="pos-session-bar" style="display: flex; gap: 12px; align-items: center; margin-bottom: 16px; padding: 10px 12px; border: 1px solid var(--border-color); border-radius: 6px;">
      <span id="pos-session-status" style="font-size: 13px; color: var(--text-muted);">Checking session&hellip;</span>
      <span id="pos-offline-queue-badge" class="badge badge-secondary hidden" style="cursor: pointer;" title="Click to try syncing now" ${actionAttrs('trySyncOfflineQueue')}></span>
      <button class="btn btn-outline" id="pos-session-open-btn" type="button" style="margin-left: auto;">Open Session</button>
      <button class="btn btn-outline hidden" id="pos-session-close-btn" type="button">Close Session</button>
      <!-- Stage 53.9: the way out of a locked terminal. Always present, never
           hidden - a cashier who has bound the till to the wrong store must be
           able to see the fix, not go looking for it. -->
      <button class="btn btn-outline" id="pos-reset-terminal-btn" type="button" title="Unbind this till from its store and clear the cart">Reset Terminal</button>
    </div>

    <!-- Stage 53.10: the frozen completed bill. Rendered above everything the
         cashier would otherwise touch, because its whole purpose is to be in
         the way until it is dismissed. -->
    <div id="pos-completed-sale" class="hidden" style="margin-bottom: 20px; padding: 18px 20px; border: 2px solid var(--success-color, #1a7f37); border-radius: 8px; background: var(--bg-color);"></div>

    <div id="pos-till-body">
      <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
        <div class="form-group" style="max-width: 280px; margin-bottom: 0;">
          <!-- Stage 41: the cashier searches and sees the location's NAME;
               #pos-location stays the code, because every downstream call
               (session, availability, cart number, receipt) keys off it.
               Stage 53.3 narrows it to sellable locations only. -->
          <label class="form-label" for="pos-location-display">Store</label>
          <input type="text" id="pos-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
          <input type="hidden" id="pos-location" value="${posLocation}">
          <div id="pos-location-lock-note" class="hidden" style="font-size: 11.5px; color: var(--text-muted); margin-top: 4px;"></div>
        </div>
        <div class="form-group" style="max-width: 260px; margin-bottom: 0;">
          <!-- Stage 53.4: was "Customer Code (optional)" / "For loyalty points".
               Same split-value control as the store box: the cashier searches a
               NAME (phone and code match too - the server's ?q= scans every
               field), and #pos-customer still carries the code every existing
               reader of it expects. -->
          <label class="form-label" for="pos-customer-display">Customer (optional)</label>
          <input type="text" id="pos-customer-display" class="form-input" placeholder="Search by name or phone" autocomplete="off">
          <input type="hidden" id="pos-customer" value="">
          <div id="pos-customer-note" style="font-size: 11.5px; color: var(--text-muted); margin-top: 4px;">Walk-in &mdash; no customer attached.</div>
        </div>
        <button class="btn btn-outline" id="pos-customer-new-btn" type="button" title="Add a customer standing at the counter">+ New Customer</button>
        <button class="btn btn-outline" id="pos-loyalty-check-btn" type="button">Check Points</button>
        <button class="btn btn-outline" id="pos-loyalty-redeem-btn" type="button">Redeem Points</button>
      </div>
      <div id="pos-loyalty-info" style="margin: 8px 0 16px; font-size: 13px; color: var(--text-muted);"></div>

      <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 8px;">
        <div class="form-group" style="flex: 1; margin-bottom: 0;">
          <label class="form-label" for="pos-sku-input">Scan or search</label>
          <input type="text" id="pos-sku-input" class="form-input" style="font-size: 16px; padding: 10px 12px;"
                 placeholder="Scan a barcode, or type a product name / code" autocomplete="off">
        </div>
        <button class="btn btn-primary" id="pos-add-btn" style="padding: 10px 20px;">Add to Cart</button>
      </div>
      <!-- Stage 53.6: what matched, when the typed text was not an exact code
           or barcode. Empty and hidden the rest of the time, which is every
           scan - the scanner path must never be interrupted by a list. -->
      <div id="pos-search-results" class="hidden" style="margin-bottom: 16px; border: 1px solid var(--border-color); border-radius: 6px; overflow: hidden;"></div>
      <div id="pos-scan-error" class="login-error hidden" style="margin-bottom: 16px;"></div>

      <table>
        <thead>
          <tr>
            <th>Item</th>
            <th>Available</th>
            <th>Qty</th>
            <!-- Stage 47.2: Sale Price is now what the SERVER says, rendered
                 read-only with the source it came from; the Cost Price column
                 is gone entirely (a cashier never sees margin, and the server
                 no longer accepts a cost from the till at all). -->
            <th>Sale Price</th>
            <th>Line Total</th>
            <th></th>
          </tr>
        </thead>
        <tbody id="pos-cart-body"></tbody>
      </table>
      <!-- Stage 30.7: offers configured in the ERP (Offer master) are evaluated
           server-side and shown here. This block is display-only - the discount
           that reaches the sale is always recomputed at checkout. -->
      <div id="pos-offers-row" class="hidden" style="margin-top: 16px; padding: 12px 14px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-color);"></div>
      <div style="display: flex; justify-content: flex-end; align-items: flex-end; gap: 20px; margin-top: 20px; padding-top: 20px; border-top: 1px solid var(--border-color); flex-wrap: wrap;">
        <div class="form-group" style="max-width: 160px; margin-bottom: 0;">
          <label class="form-label" for="pos-coupon-code">Coupon code</label>
          <input type="text" id="pos-coupon-code" class="form-input" placeholder="Optional" autocomplete="off">
        </div>
        <div class="form-group" style="max-width: 110px; margin-bottom: 0;">
          <label class="form-label" for="pos-discount-pct">Discount %</label>
          <input type="number" min="0" max="100" step="0.1" value="0" id="pos-discount-pct" class="form-input">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="pos-payment-mode">Payment Mode</label>
          <select id="pos-payment-mode" class="form-input">
            <option value="Cash">Cash</option>
            <option value="Card">Card</option>
            <option value="UPI">UPI</option>
          </select>
        </div>
        <!-- Stage 53.11: cash tendered and change due. Shown for Cash only;
             a card or UPI sale is for the exact amount and has no change. -->
        <div class="form-group" id="pos-tender-group" style="max-width: 130px; margin-bottom: 0;">
          <label class="form-label" for="pos-cash-tendered">Cash tendered</label>
          <input type="number" min="0" step="0.01" id="pos-cash-tendered" class="form-input" placeholder="0.00">
        </div>
        <div id="pos-change-due" class="hidden" style="font-size: 14px; font-weight: 600;"></div>
        <div id="pos-loyalty-discount-row" class="hidden" style="font-size: 13px; font-weight: 600; color: var(--text-muted);"></div>
        <div style="font-size: 22px; font-weight: 700;">Total: <span id="pos-cart-total">0.00</span></div>
        <button class="btn btn-primary" id="pos-checkout-btn" style="padding: 10px 22px;">Complete Sale</button>
      </div>
    </div>
  `;
  container.appendChild(panel);

  // Stage 53.3: the till may only sell from a sellable location, so the picker
  // offers only those. Deliberately scoped to THIS picker rather than to the
  // shared Location defaults - a warehouse is still a perfectly valid transfer
  // destination on the other ~15 screens that pick a location.
  attachCodeNamePicker(
    document.getElementById('pos-location-display'),
    document.getElementById('pos-location'),
    'Location',
    { filters: { sellable: 'Yes' } });
  attachCodeNamePicker(
    document.getElementById('pos-customer-display'),
    document.getElementById('pos-customer'),
    'Customer');

  // Still the hidden input's change event: attachCodeNamePicker dispatches it
  // there precisely so this listener (and every other reader of
  // #pos-location) needed no change.
  document.getElementById('pos-location').addEventListener('change', (e) => {
    posLocation = e.target.value.trim();
    // Stage 53.12: the receipt header's three records are reloaded here,
    // because the bound store is the only thing that can invalidate them -
    // and here, not at print time, so a cashier is never waiting on three
    // round trips with a customer in front of them.
    posStoreProfile = null;
    loadPOSStoreProfile();
    refreshPOSSessionStatus();
  });
  // Stage 30.7: re-evaluate offers when the coupon code or the customer
  // changes - a tier-restricted or coupon-gated offer depends on both.
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.addEventListener('change', () => refreshPOSQuote());
  document.getElementById('pos-customer').addEventListener('change', () => {
    renderPOSCustomerNote();
    refreshPOSQuote();
  });
  document.getElementById('pos-customer-new-btn').addEventListener('click', quickAddPOSCustomer);
  document.getElementById('pos-add-btn').addEventListener('click', addSKUToPOSCart);
  document.getElementById('pos-sku-input').addEventListener('keydown', onPOSScanKeyDown);
  document.getElementById('pos-checkout-btn').addEventListener('click', submitPOSCheckout);
  document.getElementById('pos-loyalty-check-btn').addEventListener('click', checkPOSLoyaltyBalance);
  document.getElementById('pos-loyalty-redeem-btn').addEventListener('click', redeemPOSLoyaltyPoints);
  document.getElementById('pos-session-open-btn').addEventListener('click', openPOSSessionFlow);
  document.getElementById('pos-session-close-btn').addEventListener('click', closePOSSessionFlow);
  document.getElementById('pos-reset-terminal-btn').addEventListener('click', resetPOSTerminal);
  document.getElementById('pos-payment-mode').addEventListener('change', renderPOSTender);
  document.getElementById('pos-cash-tendered').addEventListener('input', renderPOSTender);

  renderPOSCustomerNote();
  renderPOSCartTable();
  refreshPOSSessionStatus();
  loadPOSStoreProfile();
  renderPOSReturnPanel(container);
  renderOfflineQueueBadge();
  trySyncOfflineQueue();
}

// --- Stage 53.9: terminal binding -------------------------------------------
//
// applyPOSTerminalLock is the single place that decides what a locked till
// looks like, called from refreshPOSSessionStatus (which is the only thing
// that knows whether a session is open) rather than from each of the places
// that might want to lock. The store box goes read-only and explains itself;
// the picker's own menu is suppressed by the readonly attribute, so there is
// no second control to disable.
function applyPOSTerminalLock(locked, storeLabel) {
  posTerminalLocked = !!locked;
  const display = document.getElementById('pos-location-display');
  const note = document.getElementById('pos-location-lock-note');
  if (!display || !note) return;
  display.readOnly = posTerminalLocked;
  display.style.background = posTerminalLocked ? 'var(--bg-color)' : '';
  display.style.cursor = posTerminalLocked ? 'not-allowed' : '';
  display.title = posTerminalLocked
    ? 'This till is bound to this store for the open session. Use Reset Terminal to change it.'
    : '';
  if (posTerminalLocked) {
    note.textContent = `Bound to ${storeLabel || posLocation} for this session.`;
    note.classList.remove('hidden');
  } else {
    note.textContent = '';
    note.classList.add('hidden');
  }
}

// clearPOSTillState wipes everything this screen is holding, silently and with
// no prompts. Split out from resetPOSTerminal so the two callers that need it
// cannot drift apart: the Reset Terminal button (which asks first, below) and
// the app-wide Reset in the header, which has already done its own asking.
//
// What it deliberately does NOT touch: the offline sale queue. That is not UI
// state, it is sales that happened and have not reached the server yet, and no
// reset of any kind may destroy them.
function clearPOSTillState() {
  posCart = [];
  posCartNumber = '';
  posCompletedSale = null;
  posStoreProfile = null;
  clearPOSRedemption();
  clearPOSProductSearch();

  const completedEl = document.getElementById('pos-completed-sale');
  if (completedEl) { completedEl.classList.add('hidden'); completedEl.innerHTML = ''; }
  const tillBody = document.getElementById('pos-till-body');
  if (tillBody) tillBody.classList.remove('hidden');
  const errorEl = document.getElementById('pos-scan-error');
  if (errorEl) errorEl.classList.add('hidden');
  const tendered = document.getElementById('pos-cash-tendered');
  if (tendered) tendered.value = '';
  const customerHidden = document.getElementById('pos-customer');
  const customerDisplay = document.getElementById('pos-customer-display');
  if (customerHidden) customerHidden.value = '';
  if (customerDisplay) { customerDisplay.value = ''; customerDisplay.dataset.resolved = ''; }
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.value = '';
  const discountEl = document.getElementById('pos-discount-pct');
  if (discountEl) discountEl.value = '0';
  const infoEl = document.getElementById('pos-loyalty-info');
  if (infoEl) infoEl.textContent = '';

  renderPOSCustomerNote();
}

// resetPOSTerminal unbinds the till. It confirms first and says what will be
// lost, because the cart is cleared with it: a part-built cart vanishing
// silently is a worse bug than the one this button exists to fix.
async function resetPOSTerminal() {
  const lines = posCart.length;
  const warning = lines > 0
    ? `This clears the ${lines} line(s) in the current cart and unbinds the till from ${posLocation || 'its store'}. The cart is NOT saved.`
    : `This unbinds the till from ${posLocation || 'its store'} so a different store can be selected.`;
  if (!await showCustomConfirm(`${warning}\n\nReset the terminal?`, 'Reset Terminal')) return;

  clearPOSTillState();

  // The store itself is only released when no session is holding it. A session
  // is a cash-accountability record, not a UI state, so Reset must not be a
  // way to walk away from one - it stays bound, and the cashier is told why.
  applyPOSTerminalLock(false, '');
  renderPOSCartTable();
  renderPOSTender();
  if (posOpenSessionId) {
    await showCustomAlert('The cart is cleared. The store stays bound because a cashier session is still open here - close the session to sell from a different store.', 'Session Still Open');
    await refreshPOSSessionStatus();
    return;
  }
  const display = document.getElementById('pos-location-display');
  if (display) display.focus();
}

// --- Stage 53.4/53.5: the customer ------------------------------------------

// renderPOSCustomerNote keeps the line under the customer box honest. "Walk-in"
// is the real default state of a till and is shown as such, rather than the
// box simply sitting empty and leaving the cashier to wonder whether they
// forgot something.
function renderPOSCustomerNote() {
  const note = document.getElementById('pos-customer-note');
  if (!note) return;
  const code = (document.getElementById('pos-customer') || {}).value || '';
  const shown = (document.getElementById('pos-customer-display') || {}).value || '';
  if (!code) {
    note.textContent = 'Walk-in — no customer attached.';
    return;
  }
  note.textContent = `${shown || code} (${code}) — loyalty points will be earned on this sale.`;
}

// quickAddPOSCustomer creates a customer from the till. The alternative is
// what the screen used to force: leave POS, go to the Customer master, create
// the record, come back, and find the cart gone - with a queue waiting.
//
// Deliberately asks for the two things a counter can actually get (a name and
// a phone number) and lets the server mint the code; everything else about the
// customer can be filled in later from the master.
async function quickAddPOSCustomer() {
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

function renderPOSReturnPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginTop = '20px';
  panel.innerHTML = `
    <h2 style="margin: 0 0 12px; font-size: 16px;">Process a Return</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="max-width: 260px; margin-bottom: 0;">
        <label class="form-label" for="pos-return-order-id">Original Bill / Cart Number</label>
        <input type="text" id="pos-return-order-id" class="form-input" placeholder="e.g. POS-HO-171..." autocomplete="off">
      </div>
      <button class="btn btn-outline" id="pos-return-lookup-btn" type="button">Look Up Bill</button>
      <div class="form-group" style="max-width: 220px; margin-bottom: 0;">
        <label class="form-label" for="pos-return-location-display">Return Location</label>
        <input type="text" id="pos-return-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
        <input type="hidden" id="pos-return-location" value="${posLocation}">
      </div>
    </div>
    <div id="pos-return-eligibility" style="margin-bottom: 16px; font-size: 13px;"></div>
    <div id="pos-return-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>SKU</th><th>Sold</th><th>Already Returned</th><th>Still Returnable</th>
          <th>Return Qty</th><th>Price (from the bill)</th><th>Refund</th>
        </tr>
      </thead>
      <tbody id="pos-return-body"></tbody>
    </table>
    <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 16px; gap: 16px;">
      <div style="font-size: 12.5px; color: var(--text-muted);">
        Prices come from the original bill and cannot be changed here. The refund is calculated by the server after the goods are received and inspected.
      </div>
      <div style="display:flex; align-items:center; gap:16px;">
        <div style="font-weight:600;">Refund if all accepted: <span id="pos-return-total">0.00</span></div>
        <button class="btn btn-primary" id="pos-return-submit-btn" type="button" disabled>Raise Return</button>
      </div>
    </div>
  `;
  container.appendChild(panel);

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


// Stage 20.7: reflects whether the acting cashier already has an Open
// session at posLocation, so the POS screen doesn't let a cashier build a
// whole cart before discovering handleCheckout's 400 at the very end.
async function refreshPOSSessionStatus() {
  const statusEl = document.getElementById('pos-session-status');
  const openBtn = document.getElementById('pos-session-open-btn');
  const closeBtn = document.getElementById('pos-session-close-btn');
  if (!statusEl) return;

  if (!posLocation) {
    posOpenSessionId = '';
    statusEl.textContent = 'Choose a location to check for an open cashier session.';
    openBtn.classList.add('hidden');
    closeBtn.classList.add('hidden');
    return;
  }

  const res = await apiFetch(`/api/v1/pos/session/current?location=${encodeURIComponent(posLocation)}`);
  if (!res || !res.ok) {
    statusEl.textContent = 'Failed to check session status.';
    return;
  }
  const data = await res.json();
  posOpenSessionId = data.open ? data.session_id : '';
  // Stage 41: name the location the way the cashier does. The display box
  // already holds the resolved name; the code is the fallback for the moment
  // before it resolves, and for a code typed but not yet matched.
  const shown = (document.getElementById('pos-location-display') || {}).value || posLocation;
  if (posOpenSessionId) {
    statusEl.textContent = `Session open at ${shown}.`;
    openBtn.classList.add('hidden');
    closeBtn.classList.remove('hidden');
  } else {
    statusEl.textContent = `No open session at ${shown} - open one before selling.`;
    openBtn.classList.remove('hidden');
    closeBtn.classList.add('hidden');
  }
  // Stage 53.9: an open session is what binds the till to its store, so this -
  // the one function that establishes whether there is one - is where the lock
  // is applied. A cart with lines in it also holds the binding: changing store
  // underneath a half-built cart would reprice and re-availability every line
  // against stock that is somewhere else.
  applyPOSTerminalLock(!!posOpenSessionId || posCart.length > 0, shown);
}

async function openPOSSessionFlow() {
  if (!posLocation) {
    await showCustomAlert('Choose a location first.', 'Location Required');
    return;
  }
  const openingStr = await showCustomPrompt('Opening cash float for this session?');
  if (openingStr === null) return;
  const opening = parseFloat(openingStr);
  const res = await apiFetch('/api/v1/pos/session/open', {
    method: 'POST',
    body: JSON.stringify({ location: posLocation, opening_cash: isNaN(opening) ? 0 : opening })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to open session.');
    return;
  }
  await refreshPOSSessionStatus();
}

async function closePOSSessionFlow() {
  if (!posOpenSessionId) return;

  // 20.13: the offline window is this cashier's own open session - refuse
  // to close (client-side; the server has no way to see a queue that
  // hasn't synced yet) while sales are still waiting to sync, so a synced
  // sale can never land against the *next* session's cash-variance figures.
  const stillQueued = getOfflineQueue().length;
  if (stillQueued > 0) {
    await showCustomAlert(`${stillQueued} sale${stillQueued === 1 ? '' : 's'} still need${stillQueued === 1 ? 's' : ''} to sync before this session can close. Reconnect and try again, or click the offline badge above to sync now.`, 'Offline Sales Pending');
    return;
  }

  const countedStr = await showCustomPrompt('Counted cash in the till?');
  if (countedStr === null) return;
  const counted = parseFloat(countedStr);
  const res = await apiFetch('/api/v1/pos/session/close', {
    method: 'POST',
    body: JSON.stringify({ session_id: posOpenSessionId, counted_cash: isNaN(counted) ? 0 : counted })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to close session.');
    return;
  }
  // 21.9 QA-follow-up: this alert previously referenced an undefined `data`
  // variable (the response body was never parsed) - would have thrown a
  // ReferenceError on every successful close. Found while touching this
  // function for the offline-queue guard above.
  const data = await res.json();
  let msg = `Session closed. Expected: ${data.expected_cash.toFixed(2)}, Counted: ${data.counted_cash.toFixed(2)}, Variance: ${data.variance.toFixed(2)}`;
  // 24.36: the server diffed this session's last offline-queue heartbeat
  // against what actually synced - a non-empty list here means at least
  // one sale was queued (and beaconed) but never arrived, logged to
  // POSOfflineQueueGap for HR/Admin and Store Manager to review. Shown to
  // the closing cashier too, not just the reviewers - deliberate, so
  // there's no incentive to stay quiet about it.
  if (data.offline_queue_gap && data.offline_queue_gap.length > 0) {
    msg += `\n\nWarning: ${data.offline_queue_gap.length} offline sale(s) were queued but never synced (${data.offline_queue_gap.join(', ')}). This has been flagged for manager review.`;
  }
  await showCustomAlert(msg, 'Session Closed');
  await refreshPOSSessionStatus();
}

// --- Stage 53.6: scan-or-search product entry -------------------------------
//
// What this replaced: addSKUToPOSCart took the raw contents of the box and
// sent it to /api/v1/availability as a SKU. That endpoint answers "zero" for a
// SKU it has never heard of rather than failing, so typing a product's NAME -
// the single most natural thing to do at a counter - added a line for a
// product that does not exist, priced at nothing, and said nothing was wrong.
//
// Resolution order, and why:
//
//   1. exact Item.code     } A scanner types a string and presses Enter. Both
//   2. exact Item.barcode  } must ring up instantly with no list, no dialog
//                            and no extra keystroke, or the till is slower
//                            than the one it replaced.
//   3. name search         - anything else. The cashier gets what matched and
//                            picks; a term matching nothing says so.
//
// One request serves all three: ?q= already scans every field server-side, so
// the exact-match test is done on the rows that come back rather than costing
// a second round trip.

function clearPOSProductSearch() {
  posSearchResults = [];
  posSearchActiveIndex = -1;
  const el = document.getElementById('pos-search-results');
  if (el) { el.classList.add('hidden'); el.innerHTML = ''; }
}

// onPOSScanKeyDown drives the box from the keyboard alone, which is how a till
// is actually used: Enter rings up or picks, the arrows walk the results,
// Escape abandons them.
function onPOSScanKeyDown(e) {
  if (e.key === 'Enter') {
    e.preventDefault();
    if (posSearchResults.length && posSearchActiveIndex >= 0) {
      pickPOSSearchResult(posSearchActiveIndex);
      return;
    }
    addSKUToPOSCart();
    return;
  }
  if (!posSearchResults.length) return;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    highlightPOSSearchResult(Math.min(posSearchActiveIndex + 1, posSearchResults.length - 1));
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    highlightPOSSearchResult(Math.max(posSearchActiveIndex - 1, 0));
  } else if (e.key === 'Escape') {
    e.preventDefault();
    clearPOSProductSearch();
  }
}

function highlightPOSSearchResult(idx) {
  posSearchActiveIndex = idx;
  const rows = document.querySelectorAll('#pos-search-results .pos-result-row');
  rows.forEach((r, i) => {
    r.style.background = i === idx ? 'var(--bg-color)' : '';
  });
  if (rows[idx]) rows[idx].scrollIntoView({ block: 'nearest' });
}

function renderPOSSearchResults(rows, term) {
  const el = document.getElementById('pos-search-results');
  if (!el) return;
  posSearchResults = rows;
  posSearchActiveIndex = rows.length ? 0 : -1;
  el.innerHTML = `
    <div style="padding: 8px 12px; font-size: 12px; color: var(--text-muted); border-bottom: 1px solid var(--border-color);">
      ${rows.length} product(s) matching &ldquo;${cfgEsc(term)}&rdquo; &mdash; click one, or use the arrow keys and Enter.
    </div>
    ${rows.map((r, i) => `
      <div class="pos-result-row" data-idx="${i}" style="padding: 10px 12px; cursor: pointer; display: flex; gap: 14px; align-items: baseline; border-bottom: 1px solid var(--border-color);">
        <span style="font-weight: 600; flex: 1;">${cfgEsc(r.name || r.code || r.id || '')}</span>
        <span style="font-size: 12px; color: var(--text-muted);">${cfgEsc(r.code || r.id || '')}</span>
        ${r.barcode ? `<span style="font-size: 12px; color: var(--text-muted);">${cfgEsc(r.barcode)}</span>` : ''}
      </div>
    `).join('')}
  `;
  el.classList.remove('hidden');
  el.querySelectorAll('.pos-result-row').forEach(row => {
    row.addEventListener('click', () => pickPOSSearchResult(parseInt(row.dataset.idx, 10)));
  });
  highlightPOSSearchResult(posSearchActiveIndex);
}

function pickPOSSearchResult(idx) {
  const doc = posSearchResults[idx];
  if (!doc) return;
  clearPOSProductSearch();
  addResolvedItemToPOSCart(doc.code || doc.id, doc.name || '');
}

async function addSKUToPOSCart() {
  const skuInput = document.getElementById('pos-sku-input');
  const errorEl = document.getElementById('pos-scan-error');
  const term = skuInput.value.trim();
  errorEl.classList.add('hidden');
  clearPOSProductSearch();

  // Stage 53.10: a frozen till takes nothing. Checked here as well as on the
  // disabled input, because a barcode scanner is a keyboard and focus can land
  // anywhere.
  if (posCompletedSale) {
    errorEl.textContent = 'This sale is complete. Press New Sale to start the next one.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (!posLocation) {
    errorEl.textContent = 'Choose the store you are selling from before adding items.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (!term) return;

  const res = await apiFetch(`/api/v1/doc/Item?q=${encodeURIComponent(term)}&limit=25`);
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = 'Failed to look up products.';
    errorEl.classList.remove('hidden');
    return;
  }
  const rows = (await res.json()) || [];
  const lower = term.toLowerCase();

  // The scanner path. An exact code or barcode is unambiguous by construction,
  // so it never shows a list even when the same string also appears inside
  // some other product's description.
  const exact = rows.find(r =>
    String(r.code || r.id || '').toLowerCase() === lower ||
    String(r.barcode || '').toLowerCase() === lower);
  if (exact) {
    await addResolvedItemToPOSCart(exact.code || exact.id, exact.name || '');
    return;
  }
  if (rows.length === 0) {
    errorEl.textContent = `No product matches "${term}". Check the barcode, or search by part of the product name.`;
    errorEl.classList.remove('hidden');
    return;
  }
  if (rows.length === 1) {
    await addResolvedItemToPOSCart(rows[0].code || rows[0].id, rows[0].name || '');
    return;
  }
  renderPOSSearchResults(rows, term);
}

// addResolvedItemToPOSCart is everything that happens once the product is known
// for certain. Both entry points - the scanner's exact match and the cashier's
// pick from the result list - come through here, so a line added by scanning
// and a line added by searching are identical in every respect.
async function addResolvedItemToPOSCart(sku, name) {
  const skuInput = document.getElementById('pos-sku-input');
  const errorEl = document.getElementById('pos-scan-error');
  if (!sku) return;

  const res = await apiFetch(`/api/v1/availability?sku=${encodeURIComponent(sku)}&location=${encodeURIComponent(posLocation)}`);
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = 'Failed to look up availability for this product.';
    errorEl.classList.remove('hidden');
    return;
  }
  const avail = await res.json();
  // Stage 53.7: the sellable figure and the withheld figure are both kept on
  // the line. GetAvailableToSell has always returned all of this - the screen
  // simply threw it away and printed one number, which is why a cashier could
  // see "12 available" for stock that was 12 units of quarantined goods.
  const sellable = avail.ats ?? avail.available ?? 0;
  const withheld = (avail.qc_hold || 0) + (avail.damaged || 0) + (avail.blocked || 0) + (avail.hold_qty || 0);

  const existing = posCart.find(line => line.sku === sku);
  if (existing) {
    existing.qty += 1;
    existing.available = sellable;
    existing.withheld = withheld;
  } else {
    // Stage 47.2: salePrice starts unknown and is filled in by the server's
    // own quote (refreshPOSQuote below). costPrice is gone from the line
    // shape entirely - the till neither collects nor sends a cost.
    posCart.push({ sku, name: name || sku, available: sellable, withheld, qty: 1, salePrice: 0, referencePrice: 0, priceSource: '', overrideId: '', unpriced: false });
  }
  if (skuInput) { skuInput.value = ''; skuInput.focus(); }
  if (errorEl) errorEl.classList.add('hidden');
  renderPOSCartTable();
  // Stage 53.9: the first line binds the till to the store it was priced
  // against, exactly as an open session does.
  applyPOSTerminalLock(!!posOpenSessionId || posCart.length > 0,
    (document.getElementById('pos-location-display') || {}).value || posLocation);
}

// Stage 53.8: the +/- steppers. A counter is operated with one hand and often
// without a mouse worth the name, so "two of these" has to be one press rather
// than select-the-field-and-retype. Going below 1 removes the line, which is
// what a cashier means by pressing minus on a single unit.
function stepPOSCartLine(sku, delta) {
  const line = posCart.find(l => l.sku === sku);
  if (!line) return;
  const next = line.qty + delta;
  if (next < 1) { removeSKUFromPOSCart(sku); return; }
  line.qty = next;
  renderPOSCartTable();
}
function removeSKUFromPOSCart(sku) {
  posCart = posCart.filter(line => line.sku !== sku);
  renderPOSCartTable();
}

function updatePOSCartLine(sku, field, value) {
  const line = posCart.find(l => l.sku === sku);
  if (!line) return;
  const num = parseFloat(value);
  line[field] = isNaN(num) ? 0 : num;
  renderPOSCartTable();
}

// skipQuote is set by refreshPOSQuote's own re-render after the server's
// prices land, so writing them onto the screen does not ask for them again.
function renderPOSCartTable(skipQuote) {
  const body = document.getElementById('pos-cart-body');
  if (!body) return;
  body.innerHTML = '';
  let total = 0;

  posCart.forEach(line => {
    const lineTotal = line.qty * line.salePrice;
    total += lineTotal;
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>
        <div style="font-weight:600;">${cfgEsc(line.name || line.sku)}</div>
        ${line.name && line.name !== line.sku ? `<div style="font-size:11.5px; color:var(--text-muted);">${cfgEsc(line.sku)}</div>` : ''}
      </td>
      <td>${posAvailabilityCell(line)}</td>
      <!-- Stage 53.8: steppers beside the box, not instead of it - a cashier
           ringing up 12 of something still types 12. -->
      <td style="white-space: nowrap;">
        <button class="action-btn" type="button" title="One less" ${actionAttrs('stepPOSCartLine', [line.sku, -1])}>&minus;</button>
        <input type="number" min="1" value="${line.qty}" class="form-input" style="width: 64px; display:inline-block; text-align:center;" ${actionAttrs('updatePOSCartLine', [line.sku, 'qty', ACTION_ARG_VALUE], { on: 'change' })}>
        <button class="action-btn" type="button" title="One more" ${actionAttrs('stepPOSCartLine', [line.sku, 1])}>+</button>
      </td>
      <td>${posPriceCell(line)}</td>
      <td>${lineTotal.toFixed(2)}</td>
      <td>
        ${posCanOverridePrice() && !line.unpriced ? `<button class="action-btn" ${actionAttrs('openPOSPriceOverride', [line.sku])}>Override</button> ` : ''}
        <button class="action-btn action-btn-danger" ${actionAttrs('removeSKUFromPOSCart', [line.sku])}>Remove</button>
      </td>
    `;
    body.appendChild(tr);
  });

  // Stage 30.2.5: a redemption on this cart is shown as its own line and
  // subtracted from the total the cashier reads out, instead of being a number
  // the cashier was told to type into a line's Sale Price by hand.
  const redeemRow = document.getElementById('pos-loyalty-discount-row');
  if (redeemRow) {
    if (posRedeemPoints > 0) {
      redeemRow.textContent = `Loyalty: -${posRedeemPoints.toFixed(2)} (${posRedeemPoints} pt)`;
      redeemRow.classList.remove('hidden');
    } else {
      redeemRow.textContent = '';
      redeemRow.classList.add('hidden');
    }
  }
  if (posRedeemPoints > total) {
    // The cart shrank below what was pledged - keep the two consistent
    // rather than showing a negative total.
    posRedeemPoints = Math.floor(total);
  }

  document.getElementById('pos-cart-total').textContent = Math.max(0, total - posRedeemPoints - posOfferDiscount).toFixed(2);
  renderPOSTender();
  if (!skipQuote) refreshPOSQuote();
}

// --- Stage 53.7: sellable vs non-sellable -----------------------------------
//
// One number was never the truth. GetAvailableToSell returns the sellable
// figure (ats) AND the four buckets that are deliberately held back from it -
// QC hold, damaged, blocked, and units under an active hold - and the old cell
// printed only the first with no indication the others existed. A cashier
// looking at a shelf with twelve units on it, being told "0 available", had no
// way to know whether the system was wrong or the stock was quarantined.
//
// The withheld figure is shown only when it is non-zero, so the ordinary line -
// which is almost every line - stays a single clean number.
function posAvailabilityCell(line) {
  const sellable = Number(line.available) || 0;
  const withheld = Number(line.withheld) || 0;
  // A line asking for more than the store can sell is flagged HERE, as it is
  // entered, rather than discovered by the server at checkout with a customer
  // already waiting.
  const over = line.qty > sellable;
  const main = `<div style="font-weight:600; ${over ? 'color: var(--danger-color, #b3261e);' : ''}">${sellable} sellable</div>`;
  const held = withheld > 0
    ? `<div style="font-size:11.5px; color:var(--text-muted);">${withheld} not sellable (QC hold / damaged / blocked)</div>`
    : '';
  const warn = over
    ? `<div style="font-size:11.5px; color: var(--danger-color, #b3261e);">Only ${sellable} can be sold here</div>`
    : '';
  return main + held + warn;
}

// --- Stage 53.11: cash tendered and change due ------------------------------
//
// A till that cannot tell a cashier what change to hand back is not a till.
// Shown for Cash only - a card or UPI sale is for the exact amount, and an
// empty "change due: 0.00" sitting next to it is noise.
//
// Deliberately display-only: nothing about the tendered amount is sent to the
// server or affects what is charged. It is arithmetic for the person at the
// drawer, and treating it as anything more would make a mistyped tender into a
// pricing bug.
function renderPOSTender() {
  const modeEl = document.getElementById('pos-payment-mode');
  const group = document.getElementById('pos-tender-group');
  const changeEl = document.getElementById('pos-change-due');
  const tenderedEl = document.getElementById('pos-cash-tendered');
  const totalEl = document.getElementById('pos-cart-total');
  if (!modeEl || !group || !changeEl || !tenderedEl || !totalEl) return;

  if (modeEl.value !== 'Cash') {
    group.classList.add('hidden');
    changeEl.classList.add('hidden');
    return;
  }
  group.classList.remove('hidden');
  const due = parseFloat(totalEl.textContent) || 0;
  const tendered = parseFloat(tenderedEl.value);
  if (isNaN(tendered) || tenderedEl.value === '') {
    changeEl.classList.add('hidden');
    changeEl.textContent = '';
    return;
  }
  const change = tendered - due;
  changeEl.classList.remove('hidden');
  changeEl.textContent = change >= 0
    ? `Change due: ${change.toFixed(2)}`
    : `Short by ${Math.abs(change).toFixed(2)}`;
  changeEl.style.color = change >= 0 ? 'var(--success-color, #1a7f37)' : 'var(--danger-color, #b3261e)';
}

// --- POS pricing (Stage 47.2) and offers (Stage 30.7) --------------------
// Every price on this screen comes from POST /api/v1/pos/quote. Nothing about
// what an item costs, what offer applies, or how much tax is on it lives in
// this file - the cashier sees whatever the ERP currently says, so a price
// list approved in the back office applies at the till immediately, with no
// POS reload.
//
// The quote is display-only and cannot set a price: checkout re-resolves the
// identical quote server-side and charges THAT, so what the customer pays
// never depends on this call having run or on anything the browser could
// tamper with. posQuoteVersion is carried into checkout for one purpose - so
// the server can tell the cashier "these prices changed since you quoted
// them" instead of silently charging either the old or the new figure.
let posOfferDiscount = 0;
let posAppliedOffers = [];
let posOfferPreviewSeq = 0;
let posQuoteVersion = '';
// Set for exactly one retry after the cashier confirms a changed price, and
// cleared immediately afterwards - so a confirmation can never leak into the
// next sale and silently accept a price change nobody looked at.
let posAcceptPriceChange = false;

// posCanOverridePrice gates the per-line Override button on the capability
// the server actually checks (see engines/role_templates.go), never on a role
// name. A cashier simply does not see it; a supervisor does.
function posCanOverridePrice() {
  return !!(state.permissions && state.permissions.capabilities && state.permissions.capabilities.has('pos.price_override'));
}

// How each price source reads at the till. The point is that the cashier can
// see WHY a line is priced the way it is - "this came from the customer's
// contract", "a supervisor authorised this" - rather than a bare number.
const POS_PRICE_SOURCE_LABELS = {
  override: 'Supervisor override',
  contract_price_list: 'Contract price list',
  default_price_list: 'Price list',
  item_sale_price: 'Item price',
  item_mrp: 'MRP',
  cashier_entered: 'Not priced - keyed in'
};

// posPriceCell renders one line's price. A server-priced line is read-only
// text; only a line the server could not price at all (assisted mode) keeps an
// input, and it is labelled as unverified so nobody mistakes it for a
// system price.
function posPriceCell(line) {
  const label = POS_PRICE_SOURCE_LABELS[line.priceSource] || '';
  if (line.unpriced) {
    return `
      <input type="number" min="0" step="0.01" value="${line.salePrice}" class="form-input" style="width: 100px;"
             ${actionAttrs('updatePOSCartLine', [line.sku, 'salePrice', ACTION_ARG_VALUE], { on: 'change' })}>
      <div style="font-size:11.5px; color:var(--warning-color, #b26a00); margin-top:2px;">No price on record - needs approval</div>`;
  }
  const struck = (line.referencePrice > line.salePrice)
    ? `<span style="text-decoration:line-through; color:var(--text-muted); margin-right:6px;">${Number(line.referencePrice).toFixed(2)}</span>`
    : '';
  return `
    <div style="font-weight:600;">${struck}${Number(line.salePrice).toFixed(2)}</div>
    ${label ? `<div style="font-size:11.5px; color:var(--text-muted);">${cfgEsc(label)}</div>` : ''}`;
}

// openPOSPriceOverride raises the capability-gated override command. It is
// deliberately NOT a way to edit the price box: the server resolves its own
// reference price, measures the real reduction against it, checks that against
// the tenant's approval slab and writes immutable evidence - so a supervisor
// granting a discount and a supervisor exceeding their limit are different
// outcomes, and both are recorded.
async function openPOSPriceOverride(sku) {
  const line = posCart.find(l => l.sku === sku);
  if (!line) return;
  const newPrice = await showCustomPrompt(
    `New unit price for ${sku} (current ${Number(line.salePrice).toFixed(2)}).`,
    String(line.salePrice), 'Price Override', 'number');
  if (newPrice === null || newPrice === '') return;
  const reason = await showCustomPrompt(`Why is ${sku} being sold below its list price?`, '', 'Override Reason');
  if (!reason) {
    await showCustomAlert('A reason is required for a price override.', 'Override Not Recorded');
    return;
  }
  const res = await apiFetch('/api/v1/pos/price-override', {
    method: 'POST',
    body: JSON.stringify({
      cart_number: posCurrentCartNumber(),
      sku,
      qty: line.qty,
      location: posLocation,
      customer_id: (document.getElementById('pos-customer') || {}).value || '',
      override_price: parseFloat(newPrice) || 0,
      reason: reason.trim()
    })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'The price override was not applied.');
    await refreshPOSQuote();
    return;
  }
  await refreshPOSQuote();
  showToast(`Override recorded for ${sku}.`, { variant: 'success', title: 'Price Override' });
}

// posCurrentCartNumber pins ONE cart number for the cart being built, so a
// price override raised mid-cart and the checkout that consumes it agree on
// which sale they belong to. It is cleared with the cart.
let posCartNumber = '';
function posCurrentCartNumber() {
  if (!posCartNumber) {
    posCartNumber = `POS-${posLocation}-${Date.now()}`;
  }
  return posCartNumber;
}

function currentPOSCouponCodes() {
  const el = document.getElementById('pos-coupon-code');
  if (!el) return [];
  // Accept several codes separated by comma/space, so a cashier can key in
  // more than one without a second field.
  return el.value.split(/[,\s]+/).map(c => c.trim()).filter(Boolean);
}

// refreshPOSQuote asks the server what this cart costs and renders the answer.
// It replaced refreshPOSOffers (Stage 30.7), which asked only about offers and
// took the cashier's typed prices as given - one call now settles price, tax
// and offers together, from the same computation checkout will run, so the
// three can never disagree on this screen.
async function refreshPOSQuote() {
  const row = document.getElementById('pos-offers-row');
  if (!row) return;
  if (!posCart.length) {
    posOfferDiscount = 0;
    posAppliedOffers = [];
    posQuoteVersion = '';
    row.classList.add('hidden');
    return;
  }

  // Guard against out-of-order responses: only the newest request may write
  // back, so a slow earlier quote can't overwrite a newer cart's result.
  const seq = ++posOfferPreviewSeq;
  const customerId = (document.getElementById('pos-customer') || {}).value || '';
  let res;
  try {
    res = await apiFetch('/api/v1/pos/quote', {
      method: 'POST',
      body: JSON.stringify({
        cart_number: posCurrentCartNumber(),
        location: posLocation,
        customer_id: customerId.trim(),
        coupon_codes: currentPOSCouponCodes(),
        // Selection only, plus the operator's own figure for anything the
        // tenant has not priced - which the server uses ONLY if it can find
        // no price of its own (and then flags for approval).
        items: posCart.map(l => ({ sku: l.sku, qty: l.qty, unit_price: l.unpriced ? l.salePrice : 0 }))
      })
    });
  } catch (e) {
    // Offline (20.13's own scenario). A line already priced by an earlier
    // quote keeps that price; one added while disconnected has none, so it
    // falls back to being keyed in - and the server re-resolves it when the
    // queued sale finally syncs, where a real master price still wins. This
    // is the only path on which the till prices anything, and it exists
    // because the alternative is refusing to sell while the link is down.
    posCart.filter(l => !l.salePrice).forEach(l => { l.unpriced = true; });
    renderPOSCartTable(true);
    return;
  }
  if (seq !== posOfferPreviewSeq) return;
  if (!res || !res.ok) {
    // A quote that will not resolve is a real, blocking condition in strict
    // mode (an item with no price cannot be sold), so say so rather than
    // leaving a silently unpriced cart on screen.
    row.classList.remove('hidden');
    row.innerHTML = `<div style="font-size:13px; color:var(--danger-color, #b00020);">${cfgEsc(await getErrorMessage(res, 'These items could not be priced.'))}</div>`;
    return;
  }

  const data = await res.json();
  if (seq !== posOfferPreviewSeq) return;

  // Write the server's own prices back onto the cart. This is the line that
  // makes the screen authoritative: whatever the cashier typed is replaced by
  // what the ERP says, every time the cart changes.
  posQuoteVersion = data.quote_version || '';
  (data.lines || []).forEach(ql => {
    const line = posCart.find(l => l.sku === ql.sku);
    if (!line) return;
    line.salePrice = Number(ql.unit_price) || 0;
    line.referencePrice = Number(ql.reference_price) || 0;
    line.priceSource = ql.price_source || '';
    line.overrideId = ql.override_id || '';
    line.unpriced = ql.price_source === 'cashier_entered';
  });

  posAppliedOffers = Array.isArray(data.applied_offers) ? data.applied_offers : [];
  const newDiscount = Number(data.offer_discount) || 0;
  const unmatched = Array.isArray(data.unmatched_coupon_codes) ? data.unmatched_coupon_codes : [];

  if (!posAppliedOffers.length && !unmatched.length) {
    posOfferDiscount = 0;
    row.classList.add('hidden');
    renderPOSCartTable(true);
    return;
  }

  row.classList.remove('hidden');
  row.innerHTML = `
    ${posAppliedOffers.length ? `
      <div style="font-size:13px; font-weight:600; margin-bottom:6px;">Offers applied</div>
      <ul style="margin:0 0 4px; padding-left:18px; font-size:13px;">
        ${posAppliedOffers.map(o => `<li>${cfgEsc(o.name)} <span style="color:var(--text-muted);">- ${cfgEsc(o.description || '')}</span> <strong>-${Number(o.discount).toFixed(2)}</strong></li>`).join('')}
      </ul>
      <div style="font-size:13px; font-weight:600;">Total offer discount: -${newDiscount.toFixed(2)}</div>` : ''}
    ${unmatched.length ? `<div style="font-size:12.5px; color:var(--warning-color, #b26a00); margin-top:${posAppliedOffers.length ? '6px' : '0'};">
        Coupon code${unmatched.length > 1 ? 's' : ''} ${unmatched.map(cfgEsc).join(', ')} did not match any live offer for this cart.
      </div>` : ''}
  `;

  posOfferDiscount = newDiscount;
  // Stage 47.2: the quote changed the LINES, not just the total, so the whole
  // table is redrawn - with skipQuote set, so writing the server's prices onto
  // the screen does not immediately ask for them again. This replaces
  // updatePOSTotalForOffers, which only had a total to rewrite back when the
  // browser still owned the prices.
  renderPOSCartTable(true);
}

// 20.13 Offline-first POS queue.
//
// Decisions (user, 2026-07-22): the offline window is one shift, tied to
// the cashier's own open POSSession - see closePOSSessionFlow's guard
// below, which refuses to close a session while sales are still queued
// rather than the server trying to police a client-side queue it can't
// see. A sale that finally syncs after stock changed while offline always
// posts (the goods already physically left the store and payment was
// already taken) and is allowed to push inventory negative rather than
// rejected - engines/pos_checkout.go's recordOfflineSyncVariance flags the
// shortfall on a new POSOfflineSyncVariance record for a manager to review.
//
// cart_number (client-generated in submitPOSCheckout, unchanged from
// before this stage) doubles as the idempotency key handleCheckout already
// enforces server-side (see its own "Idempotency guard" comment) - reusing
// the exact same cart_number on every sync retry is what makes retrying a
// still-queued cart safe with zero new server-side mechanism.

// BLD-036: same double-submit guard as the generic record form/PO composer/
// GRN workbench - a repeat click on "Complete Sale" before the first response
// returns used to be able to ring up the same cart twice.
async function submitPOSCheckout() {
  await guardAgainstDoubleSubmit(document.getElementById('pos-checkout-btn'), 'Completing...', submitPOSCheckoutInner);
}

async function submitPOSCheckoutInner() {
  const errorEl = document.getElementById('pos-scan-error');
  errorEl.classList.add('hidden');

  // Stage 53.10: a frozen till cannot ring a second sale onto a closed bill.
  if (posCompletedSale) {
    errorEl.textContent = 'This sale is already complete. Press New Sale to start the next one.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (!posLocation) {
    errorEl.textContent = 'Choose the store you are selling from before completing the sale.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (posCart.length === 0) {
    errorEl.textContent = 'Add at least one item to the cart first.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (posCart.some(line => line.qty <= 0 || line.salePrice <= 0)) {
    errorEl.textContent = 'Every line needs a quantity and a price greater than zero.';
    errorEl.classList.remove('hidden');
    return;
  }

    // Stage 47.2.3: the same cart number the quote and any price override
    // were raised against, so the override the supervisor authorised applies
    // to the sale it was authorised for.
    const cartNumber = posCurrentCartNumber();
    const paymentMode = document.getElementById('pos-payment-mode').value;
    const discountPct = parseFloat(document.getElementById('pos-discount-pct').value) || 0;
    // Stage 47.2.2: selection inputs only. sale_price rides along solely for
    // a line nothing on the server prices (assisted mode) and is ignored for
    // every other line; cost_price is not sent at all any more.
    const cartItems = posCart.map(line => ({
      sku: line.sku,
      qty: line.qty,
      sale_price: line.unpriced ? line.salePrice : 0
    }));
    const customerId = document.getElementById('pos-customer').value.trim();
    // Stage 30.2.5: the redemption travels with the sale and is burned
    // server-side only if the sale completes.
    const redeemPoints = customerId ? posRedeemPoints : 0;
    const res = await checkoutOnlineOrQueue({
      cart_number: cartNumber,
      location: posLocation,
      payment_mode: paymentMode,
      customer_id: customerId,
      discount_pct: discountPct,
      redeem_points: redeemPoints,
      coupon_codes: currentPOSCouponCodes(),
      // 47.2.4: what the cashier is looking at. If the server re-resolves to
      // something else, it says so rather than charging either figure.
      quote_version: posQuoteVersion,
      accept_price_change: posAcceptPriceChange,
      items: cartItems
    });
    if (res === 'queued') {
      // Stage 53.10: an offline sale freezes the till exactly as a live one
      // does. It is a completed transaction as far as the customer walking out
      // of the shop is concerned, and the cashier needs the same "this is done,
      // start the next one" boundary - arguably more, since there is no server
      // receipt to confirm it against.
      const offlineTotal = cartItems.reduce((sum, it, i) => sum + it.qty * (posCart[i] ? posCart[i].salePrice : 0), 0);
      freezePOSTill({
        cartNumber,
        status: 'queued',
        heading: 'Sale queued offline',
        note: 'No connection. This sale is saved on this device and will sync automatically once the connection is back. Do not ring it up again.',
        paymentMode,
        items: posReceiptItems(cartItems),
        saleTotal: offlineTotal,
        amountDue: offlineTotal,
        loyaltyDiscount: 0,
        offerDiscount: 0,
        canPrint: false
      });
      showToast(`No connection - sale ${cartNumber} queued offline and will sync automatically once reconnected.`, { variant: 'warning', title: 'Offline' });
      return;
    }
    if (!res) return;
    const data = await res.json();
    // 47.2.4: prices moved between quoting this cart and ringing it up.
    // Never silently charge either the stale figure or the new one - show
    // what changed, reprice the screen, and require an explicit confirmation.
    if (res.status === 409 && data.status === 'price_changed') {
      if (data.quote) {
        posQuoteVersion = data.quote.quote_version || '';
        (data.quote.lines || []).forEach(ql => {
          const line = posCart.find(l => l.sku === ql.sku);
          if (!line) return;
          line.salePrice = Number(ql.unit_price) || 0;
          line.referencePrice = Number(ql.reference_price) || 0;
          line.priceSource = ql.price_source || '';
          line.unpriced = ql.price_source === 'cashier_entered';
        });
        posOfferDiscount = Number(data.quote.offer_discount) || 0;
        posAppliedOffers = data.quote.applied_offers || [];
        renderPOSCartTable(true);
      }
      const confirmed = await showCustomConfirm(
        `${data.message || 'Prices for this cart changed.'}\n\nThe cart now totals ${Number((data.quote || {}).total || 0).toFixed(2)}. Charge the new price?`,
        'Price Changed');
      if (!confirmed) return;
      posAcceptPriceChange = true;
      try {
        // Calls the unguarded inner function directly, not submitPOSCheckout -
        // the guard already has the button disabled/busy for this whole retry,
        // and re-entering the guard here would see it disabled and no-op.
        await submitPOSCheckoutInner();
      } finally {
        posAcceptPriceChange = false;
      }
      return;
    }
    if (!res.ok) {
      errorEl.textContent = data.error || 'Checkout failed.';
      errorEl.classList.remove('hidden');
      return;
    }

    // Stage 20.10: a discount above the configured threshold doesn't
    // complete the sale here - it's now Pending Approval and will finalize
    // (inventory/GL) once a manager decides it Approved from the Approvals screen.
    if (data.status === 'pending_approval') {
      // Stage 53.10: still a terminal outcome for THIS cart - the cashier is
      // done with it either way - so the till freezes on it rather than
      // emptying behind an alert. The panel says it is not a completed sale
      // and offers no receipt, which is the distinction that matters.
      freezePOSTill({
        cartNumber: data.cart_number || cartNumber,
        status: 'pending_approval',
        heading: 'Waiting for manager approval',
        note: data.message || 'This sale needs a manager to approve the discount before it completes. It is NOT paid yet - do not hand over the goods.',
        paymentMode,
        items: posReceiptItems(cartItems),
        saleTotal: Number(data.sale_total) || 0,
        amountDue: Number(data.sale_total) || 0,
        loyaltyDiscount: 0,
        offerDiscount: 0,
        canPrint: false
      });
      return;
    }

    // amount_due is the sale total less any loyalty points spent on it
    // (Stage 30.2.5); it equals sale_total when no points were redeemed.
    const amountDue = data.amount_due !== undefined ? data.amount_due : data.sale_total;
    // Stage 53.11: the change figure is computed from what the cashier keyed
    // in as tendered, against what the server says is actually due - not
    // against the screen's own pre-checkout total, which a server-side
    // reprice could have moved.
    const tenderedRaw = (document.getElementById('pos-cash-tendered') || {}).value;
    const tendered = paymentMode === 'Cash' ? parseFloat(tenderedRaw) : NaN;
    freezePOSTill({
      cartNumber: data.cart_number,
      status: 'paid',
      heading: 'Sale complete',
      note: '',
      paymentMode,
      items: posReceiptItems(cartItems),
      saleTotal: Number(data.sale_total) || 0,
      amountDue: Number(amountDue) || 0,
      loyaltyDiscount: Number(data.loyalty_discount) || 0,
      loyaltyPoints: Number(data.loyalty_points_redeemed) || 0,
      offerDiscount: Number(data.offer_discount) || 0,
      tendered: isNaN(tendered) ? null : tendered,
      changeDue: isNaN(tendered) ? null : tendered - (Number(amountDue) || 0),
      canPrint: true
    });
}

// posReceiptItems turns the CHECKOUT payload's lines back into lines a person
// can read, by pairing each with the cart line it came from.
//
// This also fixes a defect found while building 53.10, present since Stage
// 47.2 and never reported: the checkout payload deliberately carries
// `sale_price: 0` for every line the SERVER priced (only an unpriced,
// assisted-mode line sends a figure at all), and the browser-fallback receipt
// was handed that same array - so every ordinary line printed as "0.00" and
// the line totals summed to nothing. It went unnoticed because the QZ/ESC-POS
// path, which rebuilds the receipt server-side from the stored cart, is the
// one that runs wherever a receipt printer is actually configured; the
// fallback only prints in shops without one.
//
// The price shown is the cart line's resolved price - which is what the quote
// and the checkout both settled on - not a figure the receipt invents.
function posReceiptItems(cartItems) {
  return cartItems.map((it, i) => {
    const line = posCart[i];
    return {
      sku: it.sku,
      qty: it.qty,
      name: line ? line.name : it.sku,
      sale_price: line ? Number(line.salePrice) || 0 : Number(it.sale_price) || 0
    };
  });
}

// --- Stage 53.10: the frozen completed bill ---------------------------------
//
// What this replaced: the cart was emptied, a confirm dialog asked "print
// receipt?", and the screen went back to looking exactly as it had before the
// sale. Nothing on it said what had just happened, what was collected, or what
// the bill number was - so the only record of the sale a cashier could point
// at, thirty seconds later with the next customer at the counter, was a dialog
// they had already dismissed.
//
// Now the till stops. The bill stays up, the inputs below it are gone, and the
// only way on is New Sale. That is both the standard behaviour of every retail
// till and the fix for the specific failure mode: a second customer's items
// being scanned onto a screen the cashier believed was still showing the first
// customer's sale.
//
// Every terminal outcome funnels through here - paid, queued offline, pending
// approval - so none of them can leave a half-live cart behind. (The 409
// price-changed path is deliberately NOT terminal: it re-enters checkout and
// freezes on whatever that returns.)
function freezePOSTill(sale) {
  posCompletedSale = sale;
  posCart = [];
  posCartNumber = '';
  clearPOSRedemption();
  clearPOSProductSearch();
  renderPOSCartTable();

  const tillBody = document.getElementById('pos-till-body');
  const panel = document.getElementById('pos-completed-sale');
  if (!panel) return;
  if (tillBody) tillBody.classList.add('hidden');

  const colour = sale.status === 'paid' ? 'var(--success-color, #1a7f37)' : 'var(--warning-color, #b26a00)';
  panel.style.borderColor = colour;
  const lines = (sale.items || []).map(it => `
    <div style="display:flex; justify-content:space-between; padding:3px 0; font-size:13px;">
      <span>${cfgEsc(it.name || it.sku)} &times; ${it.qty}</span>
      <span>${(it.qty * (Number(it.sale_price) || 0)).toFixed(2)}</span>
    </div>`).join('');
  const money = (label, value, strong) => `
    <div style="display:flex; justify-content:space-between; padding:3px 0; ${strong ? 'font-size:18px; font-weight:700; padding-top:8px; border-top:1px solid var(--border-color);' : 'font-size:13px;'}">
      <span>${label}</span><span>${Number(value).toFixed(2)}</span>
    </div>`;

  panel.innerHTML = `
    <div style="display:flex; align-items:baseline; gap:12px; flex-wrap:wrap; margin-bottom:10px;">
      <span style="font-size:19px; font-weight:700; color:${colour};">${cfgEsc(sale.heading)}</span>
      <span style="font-size:13px; color:var(--text-muted);">${cfgEsc(sale.cartNumber || '')}</span>
      <span style="font-size:13px; color:var(--text-muted);">${cfgEsc(posStoreLabel())}</span>
      <span style="font-size:13px; color:var(--text-muted);">${cfgEsc(sale.paymentMode || '')}</span>
    </div>
    ${sale.note ? `<div style="font-size:13px; margin-bottom:10px;">${cfgEsc(sale.note)}</div>` : ''}
    <div style="max-width:420px;">
      ${lines}
      ${sale.offerDiscount > 0 ? money('Offer discount', -sale.offerDiscount) : ''}
      ${sale.loyaltyDiscount > 0 ? money(`Loyalty points applied${sale.loyaltyPoints ? ` (${sale.loyaltyPoints} pt)` : ''}`, -sale.loyaltyDiscount) : ''}
      ${money(sale.status === 'paid' ? 'Collected' : 'Total', sale.amountDue, true)}
      ${sale.tendered !== null && sale.tendered !== undefined ? money('Cash tendered', sale.tendered) : ''}
      ${sale.changeDue !== null && sale.changeDue !== undefined ? money('Change due', sale.changeDue, true) : ''}
    </div>
    <div style="display:flex; gap:10px; margin-top:16px;">
      <button class="btn btn-primary" id="pos-new-sale-btn" type="button">New Sale (Reset)</button>
      ${sale.canPrint ? '<button class="btn btn-outline" id="pos-print-receipt-btn" type="button">Print Receipt</button>' : ''}
    </div>
  `;
  panel.classList.remove('hidden');

  const newBtn = document.getElementById('pos-new-sale-btn');
  if (newBtn) { newBtn.addEventListener('click', startNewPOSSale); newBtn.focus(); }
  const printBtn = document.getElementById('pos-print-receipt-btn');
  if (printBtn) printBtn.addEventListener('click', () => printCompletedPOSSale());
}

// startNewPOSSale is the only way out of the freeze. It restores the till to a
// clean state but deliberately leaves the STORE binding alone: the next
// customer is served at the same counter, and re-picking the store between
// every sale would be exactly the busywork 53.9 removed.
function startNewPOSSale() {
  posCompletedSale = null;
  const panel = document.getElementById('pos-completed-sale');
  if (panel) { panel.classList.add('hidden'); panel.innerHTML = ''; }
  const tillBody = document.getElementById('pos-till-body');
  if (tillBody) tillBody.classList.remove('hidden');

  // The customer, coupon, discount and tender all belong to the sale that just
  // closed. Carrying any of them onto the next customer is a real till's
  // classic bug, so they are cleared explicitly rather than left to chance.
  const customerHidden = document.getElementById('pos-customer');
  const customerDisplay = document.getElementById('pos-customer-display');
  if (customerHidden) customerHidden.value = '';
  if (customerDisplay) { customerDisplay.value = ''; customerDisplay.dataset.resolved = ''; }
  const discountEl = document.getElementById('pos-discount-pct');
  if (discountEl) discountEl.value = '0';
  const tenderedEl = document.getElementById('pos-cash-tendered');
  if (tenderedEl) tenderedEl.value = '';
  const infoEl = document.getElementById('pos-loyalty-info');
  if (infoEl) infoEl.textContent = '';
  const errorEl = document.getElementById('pos-scan-error');
  if (errorEl) errorEl.classList.add('hidden');

  renderPOSCustomerNote();
  renderPOSCartTable();
  renderPOSTender();
  const skuInput = document.getElementById('pos-sku-input');
  if (skuInput) skuInput.focus();
}

// printCompletedPOSSale reprints the frozen bill. Same two-path behaviour the
// old inline confirm had (31.1.9): the server's own ESC-POS rebuild first, the
// browser print sheet as the fallback. Moved onto a button so a receipt can be
// printed twice - a customer asking for one after saying no is not an error
// state the cashier should have to re-ring a sale to recover from.
async function printCompletedPOSSale() {
  const sale = posCompletedSale;
  if (!sale) return;
  // 31.1.9: silent path first - the server rebuilds the receipt from the
  // Paid POSCart, so a thermal till printer gets ESC-POS with no browser
  // dialog in the cashier's way. quiet: true because a shop with no
  // Receipt printer configured is the normal case, not an error, and
  // must simply fall through to the print sheet below.
  if (await qzTryPrint('Receipt', { documentRef: sale.cartNumber, quiet: true })) return;
  printPOSReceipt(sale.cartNumber, posLocation, sale.paymentMode, sale.items,
    sale.saleTotal, sale.loyaltyDiscount || 0, sale.offerDiscount || 0);
}

// posStoreLabel is how the store reads to a human - the resolved name the
// picker is showing, falling back to the code only before it resolves. Used on
// the frozen bill and on the browser-printed receipt, so both say the same
// thing. Name only (Stage 57.1): the code no longer rides along in brackets.
function posStoreLabel() {
  const shown = (document.getElementById('pos-location-display') || {}).value || '';
  return shown || posLocation || '';
}

// --- Stage 53.12: who the bill is from --------------------------------------
//
// The receipt printed a bare location code - "HO", "BKC01" - and nothing else.
// Not the shop's name, not the company's, not a GSTIN, not the cashier. A
// customer could not tell from it which of a chain's shops they had bought
// from, and it would not stand up as a tax document.
//
// posStoreProfile caches the three records a receipt header needs (Location,
// its LegalEntity, and who is on the till) so printing does not make three
// round trips with a customer waiting. Reloaded whenever the bound store
// changes, which is the only thing that can invalidate it.
let posStoreProfile = null;

async function loadPOSStoreProfile() {
  const code = posLocation;
  if (!code) { posStoreProfile = null; return; }
  if (posStoreProfile && posStoreProfile.code === code) return;

  const profile = { code, name: code, type: '', entityName: '', gstin: '', stateName: '', cashier: '' };
  const locRes = await apiFetch(`/api/v1/doc/Location/${encodeURIComponent(code)}`);
  if (locRes && locRes.ok) {
    const loc = await locRes.json();
    profile.name = loc.name || code;
    profile.type = loc.type || '';
    if (loc.legal_entity) {
      const entRes = await apiFetch(`/api/v1/doc/LegalEntity/${encodeURIComponent(loc.legal_entity)}`);
      if (entRes && entRes.ok) {
        const ent = await entRes.json();
        profile.entityName = ent.name || loc.legal_entity;
        profile.gstin = ent.gstin || '';
        profile.stateName = ent.state || '';
      }
    }
  }
  // Who rang it up. The server's own ESC-POS receipt takes this from the
  // stored cart; the browser fallback has to ask, and asks once per binding
  // rather than once per sale.
  const meRes = await apiFetch('/api/v1/me');
  if (meRes && meRes.ok) {
    const me = await meRes.json();
    profile.cashier = me.username || '';
  }
  posStoreProfile = profile;
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
function printPOSReceipt(cartNumber, location, paymentMode, items, saleTotal, loyaltyDiscount = 0, offerDiscount = 0) {
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
      <div>${cfgEsc(storeName)}${storeName !== location ? ` (${cfgEsc(location)})` : ''}</div>
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
      <div class="receipt-line receipt-total"><span>Total (${paymentMode})</span><span>${amountDue.toFixed(2)}</span></div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// CRM/Loyalty (Stage 13.13d, scoped MVP) - POS integration. Earning
// happens automatically server-side (handleCheckout) once customer_id is
// set; these two actions are the customer-facing "check balance" /
// "redeem" steps a cashier drives manually before completing the sale.
async function checkPOSLoyaltyBalance() {
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

// Stage 30.2.5: the points a cashier has added to THIS cart, not yet burned.
// The burn happens server-side when the sale completes. Cleared whenever the
// cart is (completed, queued offline, or abandoned by leaving the screen), so
// points can never be spent on a sale that didn't happen.
let posRedeemPoints = 0;

function clearPOSRedemption() {
  posRedeemPoints = 0;
  // Stage 30.7: a completed/queued sale must not leave its offers or coupon
  // sitting on the next customer's cart.
  posOfferDiscount = 0;
  posAppliedOffers = [];
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.value = '';
  const offersRow = document.getElementById('pos-offers-row');
  if (offersRow) { offersRow.classList.add('hidden'); offersRow.innerHTML = ''; }
}

async function redeemPOSLoyaltyPoints() {
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
  posRedeemPoints = points;
  renderPOSCartTable();
  infoEl.textContent = `${points} point(s) will be applied to this sale. They are only deducted once the sale completes.`;
}

// Stage 53: the new names are exported for the same reason the old ones are -
// app.js does Object.assign(window, module) on import, and the cart table's
// inline onclick handlers (stepPOSCartLine, removeSKUFromPOSCart) resolve off
// window.
export { renderPOSView, renderPOSReturnPanel, lookUpPOSReturnBill, updatePOSReturnLine, renderPOSReturnTable, submitPOSReturn, submitPOSReturnInner, refreshPOSSessionStatus, openPOSSessionFlow, closePOSSessionFlow, addSKUToPOSCart, removeSKUFromPOSCart, updatePOSCartLine, renderPOSCartTable, posCanOverridePrice, posPriceCell, openPOSPriceOverride, posCurrentCartNumber, currentPOSCouponCodes, refreshPOSQuote, submitPOSCheckout, submitPOSCheckoutInner, printPOSReceipt, checkPOSLoyaltyBalance, clearPOSRedemption, redeemPOSLoyaltyPoints, applyPOSTerminalLock, resetPOSTerminal, renderPOSCustomerNote, quickAddPOSCustomer, clearPOSProductSearch, onPOSScanKeyDown, highlightPOSSearchResult, renderPOSSearchResults, pickPOSSearchResult, addResolvedItemToPOSCart, stepPOSCartLine, posAvailabilityCell, renderPOSTender, posReceiptItems, freezePOSTill, startNewPOSSale, printCompletedPOSSale, posStoreLabel, loadPOSStoreProfile, clearPOSTillState };
