// BLD-041: native ES module for POS.
// Loaded with import() only after the route entitlement check succeeds.
// Shared app services and offline-queue boot helpers remain in the shell.

// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnTabs, txnRestorePane, sessionStore, txnStockFromTo, mountStockFromTo, readStockFromTo } = await import('./txn-screen.js' + new URL(import.meta.url).search);

// ---------------------------------------------------------------------------
// Stage 53: a standard retail till. One working store (Stage 85.A5, locked
// while a session is open); customers by name or phone, else Walk-in; one
// box for scan and search; unsellable stock shown apart; cash tendered and
// change due; a finished sale stays frozen on screen until New Sale.
// ---------------------------------------------------------------------------

// Stage 53.10: the sale just completed, or null. While this is set the till is
// frozen: the completed bill is on screen and scanning, pricing and Complete
// Sale are all disabled until New Sale clears it. It is deliberately ONE
// variable rather than a flag plus a payload, so "is the till frozen" and
// "what is it frozen showing" can never disagree.
let posCompletedSale = null;
let posStoreHook = null;

// Stage 85.E6: the GST split of the last server quote, for the bar.
let posGST = null;
// Stage 85.E7: the cashier's reason for a discount over the limit.
let posDiscountReason = '';

// posManualDiscount is the bill discount box as the quote and checkout take it.
function posManualDiscount() {
  const v = parseFloat((document.getElementById('pos-discount-pct') || {}).value) || 0;
  const amt = (document.getElementById('pos-discount-mode') || {}).value === 'amt';
  return { manual_discount_pct: amt ? 0 : v, manual_discount_amount: amt ? v : 0 };
}

// Stage 53.6: the last product search's results, awaiting a pick. Held so the
// keyboard handler and the click handler resolve the same row.
let posSearchResults = [];
let posSearchActiveIndex = -1;
let posSearchTerm = '';      // the text the shown list was found for (Stage 108.2)
let posSearchTimer = null;   // the pause-in-typing suggestion lookup
let posSearchGen = 0;        // 214.5: bumped by every clear, so a late suggestion is dropped
let posScanInFlight = '';    // 214.5: the text one Enter is adding; a repeat Enter waits for it

function renderPOSView(container) {
  // Stage 85.A5: the store is the session's working store. A cart built at
  // another store does not follow the till here.
  const working = sessionStore();
  if (working !== posLocation) {
    if (posLocation && posCart.length) clearPOSTillState();
    posLocation = working;
    posStoreProfile = null;
  }
  // Stage 68: the frozen transaction layout (txn-screen.css) - the bar carries
  // who and where, the bill total and Complete Sale; only the cart scrolls.
  const panel = document.createElement('div');
  panel.className = 'txn-screen';
  panel.id = 'pos-screen';
  panel.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">POS / Billing</h1>
      ${txnTabs('txnShowPane', 'pos-screen:sale', [['pos-screen:sale', 'Sale'], ['pos-screen:return', 'Return'], ['pos-screen:advance', 'Advance']])}
      ${txnContext()}${txnStockFromTo('POS Sale', 'txn-main-only')}
      <div class="txn-sum txn-main-only">
        <span id="pos-rule-row" class="hidden pos-rule-row"></span>
        <span id="pos-tax-row" class="hidden"></span>
        <span id="pos-loyalty-discount-row" class="hidden"></span>
        <span id="pos-giftcard-row" class="hidden"></span>
        <span id="pos-advance-row" class="hidden"></span>
        <span id="pos-change-due" class="hidden"></span>
        <span class="txn-total">Total <span id="pos-cart-total">0.00</span></span>
      </div>
      <div class="txn-actions txn-main-only">
        <button class="btn btn-primary" id="pos-checkout-btn">Complete Sale</button>
      </div>
    </div>

    <div id="pos-sale-pane" class="txn-pane" data-pane="sale">
    <div id="pos-session-bar" class="txn-note pos-session">
      <span id="pos-session-status">Checking session&hellip;</span>
      <span id="pos-offline-queue-badge" class="badge badge-secondary hidden s-74fa97" title="Click to try syncing now" ${actionAttrs('trySyncOfflineQueue')}></span>
      <button class="btn btn-outline btn-sm s-7fd7ef" id="pos-session-open-btn" type="button">Open Session</button>
      <button class="btn btn-outline btn-sm hidden" id="pos-session-close-btn" type="button">Close Session</button>
      <!-- Stage 53.9: the way out of a locked terminal. Always present, never
           hidden - a cashier who has bound the till to the wrong store must be
           able to see the fix, not go looking for it. -->
      <button class="btn btn-outline btn-sm" id="pos-reset-terminal-btn" type="button" title="Unbind this till from its store and clear the cart">Reset Terminal</button>
    </div>

    <!-- Stage 53.10: the frozen completed bill, in place of the till until it
         is dismissed. -->
    <div id="pos-completed-sale" class="hidden pos-completed"></div>

    <div id="pos-till-body" class="txn-pane">
      <div class="txn-head">
        <!-- Stage 85.A5: the store is picked at the top of the screen. Every
             downstream call (session, availability, cart number, receipt)
             still reads its code from here. -->
        <input type="hidden" id="pos-location" value="${escapeHTMLText(posLocation)}">
        <!-- Ask 215.1: mobile and name in their own boxes. Enter in Mobile
             looks the number up; Name searches (Stage 53.4's split-value
             control) and #pos-customer still carries the code. -->
        <div class="form-group">
          <label class="form-label" for="pos-customer-phone">Mobile</label>
          <input type="tel" id="pos-customer-phone" class="form-input" placeholder="Mobile (Enter)" autocomplete="off" inputmode="tel" maxlength="20" data-phone-hint="off">
        </div>
        <div class="form-group">
          <label class="form-label" for="pos-customer-display">Name</label>
          <input type="text" id="pos-customer-display" class="form-input" placeholder="Search a name" autocomplete="off">
          <input type="hidden" id="pos-customer" value="">
          <div id="pos-customer-note" class="txn-hint">Walk-in &mdash; no customer attached.</div>
        </div>
        <!-- Ask 215.2: a buyer GSTIN makes it a B2B bill (view-pos-customer.js). -->
        <div class="form-group">
          <label class="form-label" for="pos-gstin">GSTIN</label>
          <input type="text" id="pos-gstin" class="form-input pos-gstin" placeholder="B2B only" autocomplete="off" maxlength="15" spellcheck="false">
          <button type="button" id="pos-gstin-note" class="txn-hint pos-gstin-note hidden"></button>
        </div>
        <div class="form-group">
          <label class="form-label" for="pos-coupon-code">Coupon code</label>
          <input type="text" id="pos-coupon-code" class="form-input" placeholder="Optional" autocomplete="off">
        </div>
        <div class="form-group">
          <!-- Stage 85.E7: a % or an amount, off the price on the server. -->
          <label class="form-label" for="pos-discount-pct">Discount</label>
          <div class="s-4c8197">
            <input type="number" min="0" step="0.01" value="0" id="pos-discount-pct" class="form-input">
            <select id="pos-discount-mode" class="form-input s-38ac3b" aria-label="Discount as"><option value="pct">%</option><option value="amt">Rs</option></select>
          </div>
        </div>
        <div class="form-group">
          <label class="form-label" for="pos-payment-mode">Payment Mode</label>
          <select id="pos-payment-mode" class="form-input">
            <option value="Cash">Cash</option>
            <option value="Card">Card</option>
            <option value="UPI">UPI</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="pos-remarks">Remarks</label>
          <input id="pos-remarks" class="form-input" maxlength="200" placeholder="Optional" autocomplete="off">
        </div>
        <!-- Stage 53.11: cash tendered and change due (in the bar). Shown for
             Cash only; a card or UPI sale is for the exact amount. -->
        <!-- Stage 85.E3: shown when the store asks for the salesman. -->
        <div class="form-group hidden" id="pos-salesman-group">
          <label class="form-label" for="pos-salesman">Salesman</label>
          <select id="pos-salesman" class="form-input"></select>
        </div>
        <div class="form-group" id="pos-tender-group">
          <label class="form-label" for="pos-cash-tendered">Cash tendered</label>
          <input type="number" min="0" step="0.01" id="pos-cash-tendered" class="form-input" placeholder="0.00">
        </div>
      </div>
      <div class="pos-customer-actions">
        <button class="btn btn-outline btn-sm" id="pos-customer-walkin-btn" type="button" title="The customer will not give a number - bill to the store's walk-in customer">Walk-in</button>
        <button class="btn btn-outline btn-sm" id="pos-customer-new-btn" type="button" title="Add a customer standing at the counter">+ New Customer</button>
        <button class="btn btn-outline btn-sm hidden" id="pos-advance-use-btn" type="button" title="Pay this bill from the customer's advance first">Use advance</button>
        <button class="btn btn-outline btn-sm" id="pos-loyalty-check-btn" type="button">Customer details</button>
        <button class="btn btn-outline btn-sm" id="pos-loyalty-redeem-btn" type="button">Redeem Points</button>
        <!-- Stage 72.8: pay with a gift card, issue one, check a balance. -->
        <button class="btn btn-outline btn-sm" id="pos-giftcard-btn" type="button">Gift Card</button>
        <button class="btn btn-outline btn-sm" id="pos-more-btn" type="button">More&hellip;</button>
        <span id="pos-loyalty-info"></span>
      </div>

      <div class="txn-entry">
        <div class="form-group txn-entry-main">
          <label class="form-label" for="pos-sku-input">Scan or search</label>
          <input type="text" id="pos-sku-input" class="form-input pos-scan-input"
                 placeholder="Scan a barcode, or type a design no., combination ID or name" autocomplete="off">
        </div>
        <button class="btn btn-primary" id="pos-add-btn">Add to Cart</button>
        <!-- Stage 72.4: shown only when a line's box (or other packaging) may
             be changed. The default is already chosen; this is for when the
             customer gets something else. -->
        <button class="btn btn-outline hidden" id="pos-packaging-btn" type="button" title="Change the box or packaging used">Packaging</button>
      </div>
      <!-- Stage 53.6: what matched, when the typed text was not an exact code
           or barcode. Empty and hidden the rest of the time, which is every
           scan - the scanner path must never be interrupted by a list. -->
      <div id="pos-search-results" class="hidden pos-search-results"></div>
      <div id="pos-scan-error" class="login-error hidden"></div>

      <div class="txn-grid">
        <table>
          <thead>
            <tr>
              <th>Item</th>
              <th>Available</th>
              <th>Qty</th>
              <!-- Stage 47.2: Sale Price is what the SERVER says, read-only
                   with its source; a cashier never sees cost or margin. -->
              <th>Sale Price</th>
              <th>Line Total</th>
              <th></th>
            </tr>
          </thead>
          <tbody id="pos-cart-body"></tbody>
        </table>
        <!-- Stage 30.7: offers configured in the ERP (Offer master), evaluated
             server-side. Display-only - the discount that reaches the sale is
             always recomputed at checkout. -->
        <div id="pos-offers-row" class="hidden pos-offers"></div>
        <!-- Stage 72.4 / 72.7: gifts this bill has earned, and the packaging
             it will use up (never printed on the bill). -->
        <div id="pos-packaging-row" class="hidden pos-offers"></div>
      </div>
    </div>
    </div>
  `;
  container.appendChild(panel);
  mountStockFromTo(panel);

  // Stage 85.C6: the box's "+ Create" opens the phone-first form, never the
  // generic quick-create (name + status, no mobile). The form attaches the
  // customer itself, so nothing is handed back to pick.
  attachCodeNamePicker(
    document.getElementById('pos-customer-display'),
    document.getElementById('pos-customer'),
    'Customer',
    { onCreate: (typed) => posCustomerMod().then(m => m.openPOSCustomerForm(
      /^\+?[\d\s-]{6,}$/.test(typed) ? { phone: typed.replace(/[\s-]/g, '') } : { name: typed })).then(() => null) });

  // Stage 30.7: re-evaluate offers when the coupon code or the customer
  // changes - a tier-restricted or coupon-gated offer depends on both.
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.addEventListener('change', () => refreshPOSQuote());
  ['pos-discount-pct', 'pos-discount-mode'].forEach(id => document.getElementById(id).addEventListener('change', () => { posDiscountReason = ''; refreshPOSQuote(); }));
  document.getElementById('pos-customer').addEventListener('change', () => {
    renderPOSCustomerNote();
    refreshPOSQuote();
    // Stage 85.D3: the customer's open advances, for Use advance. Ask 215.1:
    // a name picked from the list shows its mobile and GSTIN too.
    posCustomerMod().then(m => { m.posSyncCustomer(); return m.posLoadOpenAdvances(); }).catch(() => {});
  });
  document.getElementById('pos-advance-use-btn').addEventListener('click', () => posCustomerMod().then(m => m.posToggleAdvance()));
  // Stage 85.C3: phone first - Enter looks the number up (view-pos-customer.js).
  // With a customer on the bill this is Edit Customer (renderPOSCustomerNote).
  document.getElementById('pos-customer-new-btn').addEventListener('click', () => posCustomerMod().then(m =>
    m.openPOSCustomerForm({ id: (document.getElementById('pos-customer') || {}).value || '' })));
  document.getElementById('pos-customer-walkin-btn').addEventListener('click', () => posCustomerMod().then(m => m.posAttachWalkIn()));
  document.getElementById('pos-customer-display').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && /\d{6}/.test(e.target.value)) { e.preventDefault(); posCustomerMod().then(m => m.posCustomerFind(e.target)); }
  });
  // Ask 215.1/215.2: the Mobile box looks its number up; the GSTIN box makes
  // the bill B2B and opens the buyer's details.
  const phoneBox = document.getElementById('pos-customer-phone');
  phoneBox.addEventListener('input', () => { phoneBox.dataset.resolved = ''; });
  phoneBox.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); posCustomerMod().then(m => m.posCustomerFind(phoneBox)); }
  });
  const gstinBox = document.getElementById('pos-gstin');
  gstinBox.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); posCustomerMod().then(m => m.posGSTINEntered()); }
  });
  gstinBox.addEventListener('change', () => posCustomerMod().then(m => m.posGSTINEntered()));
  document.getElementById('pos-gstin-note').addEventListener('click', () => posCustomerMod().then(m => m.openPOSB2BPanel()));
  document.getElementById('pos-add-btn').addEventListener('click', addSKUToPOSCart);
  document.getElementById('pos-packaging-btn').addEventListener('click', openPOSPackaging);
  document.getElementById('pos-giftcard-btn').addEventListener('click', openPOSGiftCard);
  document.getElementById('pos-more-btn').addEventListener('click', openPOSMore);
  if (localStorage.getItem('erp_till_device_token')) posLazy('startPOSIdleLock')(); // 130.11
  document.getElementById('pos-sku-input').addEventListener('keydown', onPOSScanKeyDown);
  document.getElementById('pos-sku-input').addEventListener('input', onPOSScanInput);
  document.getElementById('pos-checkout-btn').addEventListener('click', submitPOSCheckout);
  document.getElementById('pos-loyalty-check-btn').addEventListener('click', checkPOSLoyaltyBalance);
  document.getElementById('pos-loyalty-redeem-btn').addEventListener('click', redeemPOSLoyaltyPoints);
  document.getElementById('pos-session-open-btn').addEventListener('click', openPOSSessionFlow);
  document.getElementById('pos-session-close-btn').addEventListener('click', closePOSSessionFlow);
  document.getElementById('pos-reset-terminal-btn').addEventListener('click', resetPOSTerminal);
  document.getElementById('pos-payment-mode').addEventListener('change', renderPOSTender);
  document.getElementById('pos-cash-tendered').addEventListener('input', renderPOSTender);

  // 214.2: a till left on a finished bill used to come back frozen with no New
  // Sale button (the panel is new DOM, the frozen flag was not). That bill is
  // saved and in Reprint, so coming back is the next customer: a fresh sale.
  if (posCompletedSale) startNewPOSSale();
  renderPOSCustomerNote();
  renderPOSCartTable();
  // Stage 85.A5: drawn before the top bar knew the working store - draw again
  // once it does, if it is not the store this till took.
  // BLD-045: drop the last visit's hook - unfired, it kept that whole screen alive.
  document.removeEventListener('erp:working-store', posStoreHook);
  document.addEventListener('erp:working-store', posStoreHook = (e) => {
    if (panel.isConnected && e.detail && e.detail.code && e.detail.code !== posLocation && currentView === 'pos') renderView('pos');
  }, { once: true });
  // Stage 53.12: the receipt header's records load once per store, before
  // the session line, which names the store from them.
  loadPOSStoreProfile().then(refreshPOSSessionStatus, refreshPOSSessionStatus);
  // Stage 68: the Return tab's pane, filled by view-pos-extras.js.
  const returnPane = document.createElement('div');
  returnPane.className = 'txn-pane hidden';
  returnPane.id = 'pos-return-pane';
  returnPane.dataset.pane = 'return';
  panel.appendChild(returnPane);
  posExtras().then(m => { if (returnPane.isConnected) { m.renderPOSReturnPanel(returnPane); m.wirePOSPayModeActions(posTill); } }, () => {}); // 260.1
  // Stage 85.D3: the Advance tab (view-pos-customer.js).
  const advancePane = document.createElement('div');
  advancePane.className = 'txn-pane hidden';
  advancePane.id = 'pos-advance-pane';
  advancePane.dataset.pane = 'advance';
  panel.appendChild(advancePane);
  posCustomerMod().then(m => { if (advancePane.isConnected) { m.renderPOSAdvancePanel(advancePane); m.posLoadTillOptions(posLocation); } }, () => {});
  txnRestorePane('pos-screen');
  renderOfflineQueueBadge();
  trySyncOfflineQueue();
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
  clearPOSPackagingState();

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
  const customerPhone = document.getElementById('pos-customer-phone');
  if (customerPhone) { customerPhone.value = ''; customerPhone.dataset.resolved = ''; }
  if (posCustomerRef) posCustomerRef.posClearCustomer(); // and the B2B buyer (215.2)
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.value = '';
  const discountEl = document.getElementById('pos-discount-pct');
  if (discountEl) discountEl.value = '0';
  posDiscountReason = '';
  const infoEl = document.getElementById('pos-loyalty-info');
  if (infoEl) infoEl.textContent = '';

  renderPOSCustomerNote();
}

// resetPOSTerminal clears the till. It confirms first and says what will be
// lost, because the cart is cleared with it: a part-built cart vanishing
// silently is a worse bug than the one this button exists to fix. The store
// is not part of it (Stage 85.A5): it is the working store at the top.
async function resetPOSTerminal() {
  const lines = posCart.length;
  const warning = lines > 0
    ? `This clears the ${lines} line(s) in the current cart. The cart is NOT saved.`
    : 'This clears the customer, coupon, discount and tender on the till.';
  if (!await showCustomConfirm(`${warning}\n\nReset the terminal?`, 'Reset Terminal')) return;

  clearPOSTillState();
  renderPOSCartTable();
  renderPOSTender();
  await loadPOSStoreProfile();
  await refreshPOSSessionStatus();
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
  const newBtn = document.getElementById('pos-customer-new-btn');
  if (newBtn) newBtn.textContent = code ? 'Edit Customer' : '+ New Customer';
  // Stage 85.C5: one line - who, their number, GST bill when it is one.
  note.textContent = posCustomerRef ? posCustomerRef.posCustomerSummary(code, shown)
    : (code ? shown || code : 'No customer attached.');
}

// Stage 20.7: reflects whether the acting cashier already has an Open
// session at posLocation, so the POS screen doesn't let a cashier build a
// whole cart before discovering handleCheckout's 400 at the very end.
async function refreshPOSSessionStatus() {
  const statusEl = document.getElementById('pos-session-status');
  const openBtn = document.getElementById('pos-session-open-btn');
  const closeBtn = document.getElementById('pos-session-close-btn');
  if (!statusEl) return;

  // Stage 85.A5: the store comes from the top of the screen, so that is
  // where both messages send the cashier.
  const shown = posStoreLabel();
  const notSelling = posStoreProfile && posStoreProfile.code === posLocation && !posStoreProfile.sellable;
  if (!posLocation || notSelling) {
    posOpenSessionId = '';
    statusEl.textContent = posLocation
      ? `${shown} is not a selling store. Switch to the store you are selling from at the top of the screen.`
      : 'Pick the store you are working at, at the top of the screen.';
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
  ensureTillSeries(posLocation); // 76.10: this till's offline bill series
  if (posOpenSessionId) {
    statusEl.textContent = `Session open at ${shown}.`;
    openBtn.classList.add('hidden');
    closeBtn.classList.remove('hidden');
  } else {
    statusEl.textContent = `No open session at ${shown} - open one before selling.`;
    openBtn.classList.remove('hidden');
    closeBtn.classList.add('hidden');
  }
}

async function openPOSSessionFlow() {
  if (!posLocation) {
    await showCustomAlert('Pick the store you are working at, at the top of the screen.', 'Store Required');
    return;
  }
  const openingStr = await showCustomPrompt('Opening cash float for this session?', '', 'Open Session', 'number');
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

// closePOSSessionFlow is in view-pos-extras.js (Stage 73.2.7 - it asks for the
// variance reason and handles a close that waits for a manager).

// --- Stage 53.6 / 108.2: scan-or-search product entry ---
// Only an exact barcode / GTIN rings up by itself (a scan's Enter wins); anything
// else lists the matching products, also on a pause in typing. One ?q= request
// serves both: the exact match is tested on the rows that come back.

function posExactBarcode(rows, term) {
  const lower = term.toLowerCase();
  return rows.find(r => String(r.barcode || '').toLowerCase() === lower || String(r.gtin || '').toLowerCase() === lower)
    // 49.5.6d: an item with no barcode carries its SKU on the sticker
    // (sticker-studio.js: barcode || code), so its SKU scans like a barcode -
    // and counts as scanned when an offline sale is validated.
    || rows.find(r => !String(r.barcode || '').trim() && String(r.code || r.id || '').toLowerCase() === lower);
}

// Ask 262.1: each match carries ats, its sellable quantity at this store, so
// the list shows only what can be sold here. Without the figures (the lookup
// failed) the list shows every match, as before.
async function fetchPOSProductMatches(term) {
  const res = await apiFetch(`/api/v1/doc/Item?q=${encodeURIComponent(term)}&limit=100`);
  if (!res) return null;
  if (!res.ok) return { error: true };
  const rows = (await res.json()) || [];
  const skus = rows.map(r => r.code || r.id).filter(Boolean);
  if (skus.length && posLocation) {
    // 272.2: describe=1 also brings each one's Design No, for the list.
    const a = await apiFetch(`/api/v1/availability?skus=${encodeURIComponent(skus.join(','))}&location=${encodeURIComponent(posLocation)}&describe=1`).catch(() => null);
    const body = a && a.ok ? ((await a.json()) || {}) : {};
    if (body.ats) rows.forEach(r => { r.ats = Number(body.ats[r.code || r.id]) || 0; });
    if (body.design_no) rows.forEach(r => { r.design_no = body.design_no[r.code || r.id] || ''; });
  }
  return { rows };
}

const POS_LIST_MAX = 25;
const posSellableRows = (rows) => rows.filter(r => r.ats === undefined || r.ats > 0).slice(0, POS_LIST_MAX);

function onPOSScanInput(e) {
  clearTimeout(posSearchTimer);
  const term = e.target.value.trim();
  if (term.length < 2 || posCompletedSale || !posLocation) { clearPOSProductSearch(); return; }
  posSearchTimer = setTimeout(async () => {
    const gen = posSearchGen;
    const found = await fetchPOSProductMatches(term);
    const input = document.getElementById('pos-sku-input');
    if (!found || found.error || !input || input.value.trim() !== term) return; // typed on since
    // 214.5: Enter (or a pick) since this lookup started already handled the
    // text; a list drawn now would offer the same item again after it was added.
    if (gen !== posSearchGen) return;
    if (found.rows.length) renderPOSSearchResults(found.rows, term);
    else clearPOSProductSearch();
  }, 350);
}

function clearPOSProductSearch() {
  posSearchGen++;
  posSearchResults = [];
  posSearchActiveIndex = -1;
  posSearchTerm = '';
  const el = document.getElementById('pos-search-results');
  if (el) { el.classList.add('hidden'); el.innerHTML = ''; }
}

// onPOSScanKeyDown drives the box from the keyboard alone, which is how a till
// is actually used: Enter rings up or picks, the arrows walk the results,
// Escape abandons them.
function onPOSScanKeyDown(e) {
  if (e.key === 'Enter') {
    e.preventDefault();
    clearTimeout(posSearchTimer);
    // Pick from the list only if it was found for this exact, non-barcode text.
    const term = e.target.value.trim();
    if (posSearchResults.length && posSearchActiveIndex >= 0 && posSearchTerm === term && !posExactBarcode(posSearchResults, term)) {
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
  rows.forEach((r, i) => r.classList.toggle('is-active', i === idx));
  if (rows[idx]) rows[idx].scrollIntoView({ block: 'nearest' });
}

function renderPOSSearchResults(all, term) {
  const el = document.getElementById('pos-search-results');
  if (!el) return;
  const rows = posSellableRows(all); // 262.1: only what can be sold here
  posSearchResults = rows;
  posSearchTerm = term;
  posSearchActiveIndex = rows.length ? 0 : -1;
  // 272.2: one row per combination, with its Design No, Combination ID and
  // barcode, so the cashier matches the tag and picks the row to bill it.
  const showAts = rows.some(r => r.ats !== undefined);
  el.innerHTML = `
    <div class="s-4777e4">
      ${rows.length ? `${rows.length} in stock &middot; &ldquo;${cfgEsc(term)}&rdquo;`
        : `None in stock here matches &ldquo;${cfgEsc(term)}&rdquo;.`}
    </div>
    ${rows.length ? `<table class="pos-result-table">
      <thead><tr><th>Design No</th><th>Combination ID</th><th>Barcode</th><th>Item</th>${showAts ? '<th class="pos-result-num">Available</th>' : ''}</tr></thead>
      <tbody>${rows.map((r, i) => `
        <tr class="pos-result-row" data-idx="${i}">
          <td>${cfgEsc(r.design_no || r.parent_product_code || r.family || '')}</td>
          <td>${cfgEsc(r.code || r.id || '')}</td>
          <td class="pos-result-code">${cfgEsc(r.barcode || '')}</td>
          <td class="pos-result-name">${cfgEsc(r.name || r.code || r.id || '')}</td>
          ${showAts ? `<td class="pos-result-num">${cfgEsc(r.ats ?? '')}</td>` : ''}
        </tr>`).join('')}
      </tbody>
    </table>` : ''}
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
  // 214.5: one Enter, one add. A scanner that sends Enter twice, or a second
  // press before the first add has cleared the box, reads the same text again.
  if (posScanInFlight === term) return;
  posScanInFlight = term;
  try { await addPOSScanTerm(term, errorEl); } finally { posScanInFlight = ''; }
}

async function addPOSScanTerm(term, errorEl) {
  clearTimeout(posSearchTimer);
  const found = await fetchPOSProductMatches(term);
  if (!found) return;
  if (found.error) {
    errorEl.textContent = 'Failed to look up products.';
    errorEl.classList.remove('hidden');
    return;
  }
  const rows = found.rows;

  // The scanner path. An exact barcode is unambiguous by construction, so it
  // never shows a list even when the same string also appears inside some
  // other product's details. Everything else is picked from the list (108.2).
  const exact = posExactBarcode(rows, term);
  if (exact) {
    await addResolvedItemToPOSCart(exact.code || exact.id, exact.name || '', term);
    return;
  }
  if (rows.length === 0) {
    if (await (await posExtras()).posNamedScan(posTill, term, addResolvedItemToPOSCart)) return;
    errorEl.textContent = `No product matches "${term}". Check the barcode, or search by part of the design no., combination ID or product name.`;
    errorEl.classList.remove('hidden');
    return;
  }
  renderPOSSearchResults(rows, term);
}

// addResolvedItemToPOSCart is everything that happens once the product is known
// for certain. Both entry points - the scanner's exact match and the cashier's
// pick from the result list - come through here, so a line added by scanning
// and a line added by searching are identical in every respect.
async function addResolvedItemToPOSCart(sku, name, scanned) {
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
  // Stage 85.B5: what cannot be sold here, by the store's own location names.
  const heldText = (Array.isArray(avail.locations) ? avail.locations : [])
    .filter(l => !l.sellable && l.qty > 0).map(l => `${l.name} ${l.qty}`).join(' · ');

  // Ask 262.2: a scan or pick that would go past what can be sold here adds
  // nothing. An item that keeps no stock (a service) needs none.
  const existing = posCart.find(line => line.sku === sku);
  if (avail.tracked !== false && (existing ? existing.qty : 0) + 1 > sellable) {
    if (errorEl) {
      errorEl.textContent = `${name || sku}: ${sellable > 0 ? `only ${sellable} in stock here` : 'not in stock here'}. Not added.${heldText ? ` Not sellable here: ${heldText}.` : ''}`;
      errorEl.classList.remove('hidden');
    }
    if (skuInput) { skuInput.value = ''; skuInput.focus(); }
    return;
  }

  posLastAddedSku = sku;
  if (existing) {
    existing.qty += 1;
    if (!scanned) existing.scanned = ''; // 49.5.6a: scanned only if every add was
    existing.available = sellable;
    existing.withheld = withheld;
    existing.heldText = heldText;
  } else {
    // Stage 47.2: salePrice starts unknown and is filled in by the server's
    // own quote (refreshPOSQuote below). costPrice is gone from the line
    // shape entirely - the till neither collects nor sends a cost.
    // Stage 94.6: the scanned code rides along - a receipt-date barcode
    // names the receipt, so the server knows the MRP printed on it.
    posCart.push({ sku, name: name || sku, available: sellable, withheld, heldText, qty: 1, salePrice: 0, referencePrice: 0, priceSource: '', overrideId: '', unpriced: false, barcode: scanned && String(scanned).toLowerCase() !== String(sku).toLowerCase() ? String(scanned) : '', scanned: scanned || '' });
  }
  if (skuInput) { skuInput.value = ''; skuInput.focus(); }
  if (errorEl) errorEl.classList.add('hidden');
  renderPOSCartTable();
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
        <div class="s-eed0f8">${cfgEsc(line.name || line.sku)}</div>
        ${line.name && line.name !== line.sku ? `<div class="s-40ab95">${cfgEsc(line.sku)}</div>` : ''}
        ${line.namedCell?.() || ''}
        ${posMRPCell(line)}
      </td>
      <td>${posAvailabilityCell(line)}</td>
      <!-- Stage 53.8: steppers beside the box, not instead of it - a cashier
           ringing up 12 of something still types 12. -->
      <td class="s-1cd2ee">
        <button class="action-btn" type="button" title="One less" ${actionAttrs('stepPOSCartLine', [line.sku, -1])}>&minus;</button>
        <input type="number" min="1" value="${line.qty}" class="form-input s-c7a4d8" ${actionAttrs('updatePOSCartLine', [line.sku, 'qty', ACTION_ARG_VALUE], { on: 'change' })}>
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
  // subtracted from the total the cashier reads out. 73.1.5: what the points
  // are worth, and the cap, come from the quote - never 1 point = Rs 1 here.
  const redeemRow = document.getElementById('pos-loyalty-discount-row');
  const pointsValue = posRedeemPoints && posLoyalty ? Number(posLoyalty.value) || 0 : 0;
  if (redeemRow) {
    redeemRow.textContent = pointsValue ? `Loyalty: -${pointsValue.toFixed(2)} (${posRedeemPoints} pt)` : '';
    redeemRow.classList.toggle('hidden', !pointsValue);
  }

  // Stage 94.15: the bill is paid to the whole rupee, rounded exactly as the
  // server's quote rounds it.
  const bill = Math.max(0, Math.round((total - posOfferDiscount) * 100) / 100);
  const roundOff = Math.round((Math.round(bill) - bill) * 100) / 100;

  // Stage 85.E6 (77.12): the bar carries the discount and the tax split of
  // the server's quote, beside the total.
  const taxRow = document.getElementById('pos-tax-row');
  if (taxRow) {
    const g = posCart.length ? posGST : null;
    const parts = [];
    // 215.5: the three discounts apart - the lines' own cuts below list price,
    // the offers and vouchers, and the cashier's bill discount (a Manual offer).
    const itemDisc = posCart.reduce((s, l) => s + Math.max(0, (Number(l.referencePrice) || 0) - (Number(l.salePrice) || 0)) * (Number(l.qty) || 0), 0);
    const billDisc = posAppliedOffers.filter(o => o.offer_type === 'Manual').reduce((s, o) => s + (Number(o.discount) || 0), 0);
    const offerDisc = posOfferDiscount - billDisc;
    if (itemDisc > 0.004) parts.push(`Item -${itemDisc.toFixed(2)}`);
    if (offerDisc > 0.004) parts.push(`Offer -${offerDisc.toFixed(2)}`);
    if (billDisc > 0.004) parts.push(`Bill -${billDisc.toFixed(2)}`);
    // 215.3: a B2B bill shipped to another state says where it is supplied.
    if (g && g.interstate && g.igst) parts.push(`IGST ${Number(g.igst).toFixed(2)}${g.place_of_supply ? ` · to ${posStateName(g.place_of_supply)}` : ''}`);
    if (g && !g.interstate && (g.cgst || g.sgst)) parts.push(`CGST ${Number(g.cgst).toFixed(2)} · SGST ${Number(g.sgst).toFixed(2)}`);
    if (roundOff) parts.push(`Round off ${roundOff > 0 ? '+' : ''}${roundOff.toFixed(2)}`);
    taxRow.textContent = parts.join(' · ');
    taxRow.classList.toggle('hidden', !parts.length);
  }
  // Stage 72.8: gift cards paying the bill come off what is still due.
  const giftPaid = posGiftCardTenders.reduce((s, g) => s + g.amount, 0);
  const giftRow = document.getElementById('pos-giftcard-row');
  if (giftRow) {
    giftRow.textContent = giftPaid > 0 ? `Gift card: -${giftPaid.toFixed(2)} (${posGiftCardTenders.map(g => g.card).join(', ')})` : '';
    giftRow.classList.toggle('hidden', !(giftPaid > 0));
  }
  const due = bill + roundOff - pointsValue - giftPaid;
  const exRow = document.getElementById('pos-exchange-row');
  if (exRow && posExchange) exRow.textContent = `Exchange credit: -${posExchange.amount.toFixed(2)}${posExchange.amount > due ? ` (${(posExchange.amount - due).toFixed(2)} back to the customer)` : ''}`;
  // Stage 85.D3: the customer's advance pays next; the bar keeps the bill's
  // value (for the high-value rules) and what is due before the advance.
  const totalEl = document.getElementById('pos-cart-total');
  const beforeAdvance = Math.max(0, due - (posExchange ? posExchange.amount : 0));
  const advances = posCustomerRef ? posCustomerRef.posAdvanceApplied(beforeAdvance) : [];
  const advanceUsed = advances.reduce((s, a) => s + a.amount, 0);
  const advRow = document.getElementById('pos-advance-row');
  if (advRow) {
    advRow.textContent = advanceUsed > 0 ? `Advance / credit: -${advanceUsed.toFixed(2)}` : '';
    advRow.classList.toggle('hidden', !(advanceUsed > 0));
  }
  totalEl.dataset.gross = (bill + roundOff).toFixed(2);
  totalEl.dataset.beforeAdvance = beforeAdvance.toFixed(2);
  totalEl.textContent = Math.max(0, beforeAdvance - advanceUsed).toFixed(2);
  renderPOSTender();
  if (!skipQuote) refreshPOSQuote();
}

// posStateName: a GST state code (27) as its name, from the field formats.
function posStateName(code) {
  const r = ((window.FIELD_META && window.FIELD_META.regions) || []).find(x => x.tax_code === code);
  return r ? r.name : code;
}

// posCashPart is the part of the bill paid in cash: all of it with Cash
// picked, less the split tenders; otherwise the split tenders taken in cash.
function posCashPart() {
  const totalBox = document.getElementById('pos-cart-total');
  const due = parseFloat(totalBox && totalBox.textContent) || 0;
  const mode = (document.getElementById('pos-payment-mode') || {}).value;
  return mode === 'Cash' ? due - posTenders.reduce((s, t) => s + (t.mode === 'Cash' ? 0 : t.amount), 0)
    : posTenders.reduce((s, t) => s + (t.mode === 'Cash' ? t.amount : 0), 0);
}

// Ask 215.4: the high-value rule the bill has reached, as it grows - the
// address, the PAN, the cash limit - before Complete Sale asks for them.
function renderPOSRuleRow() {
  const row = document.getElementById('pos-rule-row');
  if (!row) return;
  const totalBox = document.getElementById('pos-cart-total') || { dataset: {} };
  const text = posCart.length && posCustomerRef ? posCustomerRef.posRuleHint(parseFloat(totalBox.dataset.gross) || 0, posCashPart()) : '';
  row.textContent = text;
  row.classList.toggle('hidden', !text);
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
  // Stage 72.9: a service or non-stock item has no shelf to count.
  if (line.notStocked) return '<div class="txn-hint">Not stocked</div>';
  const sellable = Number(line.available) || 0;
  const withheld = Number(line.withheld) || 0;
  // A line asking for more than the store can sell is flagged HERE, as it is
  // entered, rather than discovered by the server at checkout with a customer
  // already waiting.
  const over = line.qty > sellable;
  const main = `<div data-css="font-weight:600; ${over ? 'color: var(--danger-color, #b3261e);' : ''}">${sellable} sellable</div>`;
  const held = line.heldText || withheld > 0
    ? `<div class="s-40ab95">${line.heldText ? `Not sellable here: ${escapeHTMLText(line.heldText)}` : `${withheld} not sellable (QC hold / damaged / blocked)`}</div>`
    : '';
  const warn = over
    ? `<div class="s-17811e">Only ${sellable} can be sold here</div>`
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
  renderPOSRuleRow(); // 215.4: the cash rule follows the payment mode
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
  const due = (parseFloat(totalEl.textContent) || 0) - posTenders.reduce((s, t) => s + t.amount, 0);
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
// Stage 72.4 / 72.7: from the quote - what this cart uses up with its lines
// (box, butter paper) and the choices per line, the gifts it has earned and
// any component the store is short of; plus the cashier's Packaging changes,
// keyed "sku|group", sent with the sale.
let posPackaging = [];
let posPackagingOverrides = {};
let posGifts = [];
let posPackagingShortages = [];
let posLastAddedSku = '';
// Stage 72.8: gift cards paying this bill - {card_number, card (masked), amount}.
// Held only in memory: never queued offline, never stored on the device.
let posGiftCardTenders = [];

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
  cashier_entered: 'Not priced - keyed in',
  item_type_free: 'Free (gift / free with item)',
  gwp_offer: 'Free gift (offer)'
};

// posPriceCell renders one line's price. A server-priced line is read-only
// text; only a line the server could not price at all (assisted mode) keeps an
// input, and it is labelled as unverified so nobody mistakes it for a
// system price.
// Stage 94.15: an MRP is a whole rupee, shown without paise (one saved with
// paise before then still shows them).
function posMRPText(mrp) {
  const n = Number(mrp) || 0;
  return Number.isInteger(n) ? String(n) : n.toFixed(2);
}

// Stage 94.6 (D2): an item on the shelf at two or more MRPs - one tap picks
// the MRP printed on what the customer is buying; the whole line sells at it.
function posMRPCell(line) {
  const choices = Array.isArray(line.mrpChoices) ? line.mrpChoices : [];
  if (choices.length > 1 && !line.mrpPicked) {
    return `<div class="pos-mrp-choice" role="group" aria-label="MRP printed on ${cfgEsc(line.name || line.sku)}">
      <span>Which MRP is on it?</span>
      ${choices.map(m => `<button class="btn btn-outline btn-sm" type="button" ${actionAttrs('pickPOSLineMRP', [line.sku, m])}>${posMRPText(m)}</button>`).join('')}
    </div>`;
  }
  if (!line.mrp) return '';
  return `<div class="s-40ab95">MRP ${posMRPText(line.mrp)}${line.mrpPicked ? ` <button class="link-btn" type="button" ${actionAttrs('pickPOSLineMRP', [line.sku, 0])}>change</button>` : ''}</div>`;
}

function pickPOSLineMRP(sku, mrp) {
  const line = posCart.find(l => l.sku === sku);
  if (!line) return;
  line.mrp = Number(mrp) || 0;
  line.mrpPicked = line.mrp > 0;
  renderPOSCartTable();
}

function posPriceCell(line) {
  const label = POS_PRICE_SOURCE_LABELS[line.priceSource] || '';
  if (line.unpriced) {
    return `
      <input type="number" min="0" step="0.01" value="${line.salePrice}" class="form-input s-da190a"
             ${actionAttrs('updatePOSCartLine', [line.sku, 'salePrice', ACTION_ARG_VALUE], { on: 'change' })}>
      <div class="s-72dd0b">No price on record - needs approval</div>`;
  }
  const struck = (line.referencePrice > line.salePrice)
    ? `<span class="s-d62799">${Number(line.referencePrice).toFixed(2)}</span>`
    : '';
  return `
    <div class="s-eed0f8">${struck}${Number(line.salePrice).toFixed(2)}</div>
    ${label ? `<div class="s-40ab95">${cfgEsc(label)}</div>` : ''}`;
}

// openPOSPriceOverride and its line-discount dialog (215.6) are in
// view-pos-extras.js: a cashier opens them now and then.

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
    clearPOSPackagingState();
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
        redeem_points: posRedeemPoints,
        ...posManualDiscount(),
        b2b: posCustomerRef ? posCustomerRef.posB2BBlock() : null, // 215.3: place of supply, IGST
        // Selection only, plus the operator's own figure for anything the
        // tenant has not priced - which the server uses ONLY if it can find
        // no price of its own (and then flags for approval).
        items: posCart.map(l => ({ sku: l.sku, qty: l.qty, unit_price: l.unpriced ? l.salePrice : 0, mrp: l.mrpPicked ? l.mrp : 0, barcode: l.barcode || '' }))
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
  if (res && !res.ok && await dropPOSRefusedLine(res)) return;
  if (!res || !res.ok) {
    // A quote that will not resolve is a real, blocking condition in strict
    // mode (an item with no price cannot be sold), so say so rather than
    // leaving a silently unpriced cart on screen.
    row.classList.remove('hidden');
    row.innerHTML = `<div class="s-88686e">${cfgEsc(await getErrorMessage(res, 'These items could not be priced.'))}</div>`;
    return;
  }

  const data = await res.json();
  if (seq !== posOfferPreviewSeq) return;

  // Write the server's own prices back onto the cart. This is the line that
  // makes the screen authoritative: whatever the cashier typed is replaced by
  // what the ERP says, every time the cart changes.
  posQuoteVersion = data.quote_version || '';
  posLoyalty = data.loyalty || null;
  posGST = data.gst_breakdown ? { ...data.gst_breakdown, place_of_supply: data.place_of_supply || '' } : null;
  (data.warnings || []).forEach(w => { if (!window._posWarned) window._posWarned = new Set(); if (!window._posWarned.has(w)) { window._posWarned.add(w); showToast(w, { variant: "warning", title: "Item lifecycle" }); } });
  { const e = document.getElementById('pos-loyalty-info'), p = posLoyalty; if (e && p) e.textContent = p.earn_blocked ? 'No points: ' + p.earn_blocked : p.earn_points ? 'Earns ' + p.earn_points + ' pts' : ''; }
  posRedeemPoints = posLoyalty ? posLoyalty.points : 0;
  (data.lines || []).forEach(ql => {
    const line = posCart.find(l => l.sku === ql.sku);
    if (!line) return;
    line.salePrice = Number(ql.unit_price) || 0;
    line.referencePrice = Number(ql.reference_price) || 0;
    line.priceSource = ql.price_source || '';
    line.overrideId = ql.override_id || '';
    line.unpriced = ql.price_source === 'cashier_entered';
    line.notStocked = !!ql.not_stocked;
    line.billMode = ql.bill_mode || '';
    line.mrp = Number(ql.mrp) || 0;
    line.mrpChoices = Array.isArray(ql.mrp_choices) ? ql.mrp_choices : (line.mrpPicked ? line.mrpChoices : []);
  });
  posPackaging = Array.isArray(data.packaging) ? data.packaging : [];
  posGifts = Array.isArray(data.gifts) ? data.gifts : [];
  posPackagingShortages = Array.isArray(data.packaging_shortages) ? data.packaging_shortages : [];
  const liveKeys = new Set(posPackaging.map(posPackagingKey));
  Object.keys(posPackagingOverrides).forEach(k => { if (!liveKeys.has(k)) delete posPackagingOverrides[k]; });
  renderPOSPackagingRow();

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
      <div class="s-2c237e">Offers applied</div>
      <ul class="s-9193c6">
        ${posAppliedOffers.map(o => `<li>${cfgEsc(o.name)} <span class="s-5872de">- ${cfgEsc(o.description || '')}</span> <strong>-${Number(o.discount).toFixed(2)}</strong></li>`).join('')}
      </ul>
      <div class="s-203476">Total offer discount: -${newDiscount.toFixed(2)}</div>` : ''}
    ${unmatched.length ? `<div data-css="font-size:12px; color:var(--warning-color, #b26a00); margin-top:${posAppliedOffers.length ? '6px' : '0'};">
        Coupon code${unmatched.length > 1 ? 's' : ''} ${unmatched.map(c => cfgEsc(c) + ((data.coupon_messages || {})[c] ? ` (${cfgEsc(data.coupon_messages[c])})` : '')).join(', ')} did not apply to this cart.
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
  // Stage 85.C3/85.C4: a mobile number on every bill (or Walk-in), and the
  // buyer's address / PAN when the bill's size needs them.
  const totalBox = document.getElementById('pos-cart-total') || { dataset: {} };
  const billTotal = parseFloat(totalBox.dataset.gross || totalBox.textContent) || 0;
  // 215.2/215.4: a B2B bill's buyer details, and the day's cash for the customer.
  const customerStep = await (await posCustomerMod()).posEnsureCustomer({ total: billTotal, cash: posCashPart(), cart: posCurrentCartNumber() });
  if (!customerStep) return;
  // Stage 85.E2/85.E3: the tender's own name and reference, and the salesman.
  // 185.1: what the picked tender pays - the rest after any split tenders -
  // and the bill, so each bill asks for its own reference.
  const tillPayment = await posCustomerRef.posTillPayment({ amount: Math.round(((parseFloat(totalBox.textContent) || 0) - posTenders.reduce((s, t) => s + t.amount, 0)) * 100) / 100, cart: posCurrentCartNumber() });
  if (!tillPayment) return;
  // Stage 72.2: a gift / free-with-item line is priced 0 by the server on
  // purpose (it used to be refused here, so no free item could ever be billed).
  if (posCart.some(line => line.qty <= 0 || (line.salePrice <= 0 && !POS_FREE_SOURCES.has(line.priceSource)))) {
    errorEl.textContent = 'Every line needs a quantity and a price greater than zero.';
    errorEl.classList.remove('hidden');
    return;
  }

    // Stage 47.2.3: the same cart number the quote and any price override
    // were raised against, so the override the supervisor authorised applies
    // to the sale it was authorised for.
    const cartNumber = posCurrentCartNumber();
    const paymentMode = document.getElementById('pos-payment-mode').value;
    const discount = posManualDiscount();
    // Stage 47.2.2: selection inputs only. sale_price rides along solely for
    // a line nothing on the server prices (assisted mode) and is ignored for
    // every other line; cost_price is not sent at all any more.
    const cartItems = posCart.map(line => ({
      sku: line.sku,
      qty: line.qty,
      sale_price: line.unpriced ? line.salePrice : 0,
      mrp: line.mrpPicked ? line.mrp : 0, // Stage 94.6
      barcode: line.barcode || '',
      scanned: line.scanned || '', // 49.5.6a: checked on POS > Offline Sales
      ...line.namedFields?.()
    }));
    const customerId = document.getElementById('pos-customer').value.trim();
    // Stage 30.2.5: the redemption travels with the sale and is burned
    // server-side only if the sale completes.
    const redeemPoints = customerId ? posRedeemPoints : 0;
    const checkoutPayload = {
      cart_number: cartNumber,
      location: posLocation,
      ...readStockFromTo(document),
      payment_mode: paymentMode,
      customer_id: customerId,
      discount_pct: discount.manual_discount_pct,
      ...discount,
      discount_reason: posDiscountReason,
      redeem_points: redeemPoints,
      form_97: customerStep.form97,
      b2b: posCustomerRef ? posCustomerRef.posB2BBlock() : null, // 215.2: the buyer of a B2B bill
      ...tillPayment,
      advances: posCustomerRef ? posCustomerRef.posAdvanceApplied(parseFloat(totalBox.dataset.beforeAdvance) || 0) : [],
      ...posRedemptionAuth,
      // 49.5.4: a tender repeating one taken minutes ago is refused unless
      // the cashier confirmed it is a second payment.
      tenders: posSecondPayment ? posTenders.map(t => ({ ...t, second_payment: true })) : posTenders,
      second_payment: posSecondPayment,
      below_cost_ok: posBelowCostOK,
      exchange_credit_id: posExchange ? posExchange.id : '',
      coupon_codes: currentPOSCouponCodes(),
      remarks: (document.getElementById('pos-remarks')?.value || '').trim(), // 260.3
      // 47.2.4: what the cashier is looking at. If the server re-resolves to
      // something else, it says so rather than charging either figure.
      quote_version: posQuoteVersion,
      accept_price_change: posAcceptPriceChange,
      // Stage 72.4: only the lines whose packaging the cashier changed.
      packaging_overrides: Object.values(posPackagingOverrides).map(o => ({ sku: o.sku, group: o.group, component: o.component, reason: o.reason })),
      items: cartItems
    };
    let res;
    if (posGiftCardTenders.length || posExchange) {
      // Stage 72.8: a gift card sale is online only - it is never queued, so a
      // card number never lands in the device's offline store.
      checkoutPayload.gift_cards = posGiftCardTenders.map(g => ({ card_number: g.card_number, amount: g.amount }));
      if (!navigator.onLine) {
        errorEl.textContent = posExchange ? 'No connection - an exchange is settled online. Try again once the connection is back.' : 'No connection - a gift card cannot be used offline. Remove it (Gift Card -> Remove) and take another payment.';
        errorEl.classList.remove('hidden');
        return;
      }
      try {
        res = await apiFetch('/api/v1/checkout', { method: 'POST', body: JSON.stringify(checkoutPayload) });
      } catch (e) {
        errorEl.textContent = 'The connection dropped - nothing was charged to the gift card. Try again once you are back online.';
        errorEl.classList.remove('hidden');
        return;
      }
    } else {
      res = await checkoutOnlineOrQueue(checkoutPayload);
    }
    if (res === 'queued') {
      // Stage 53.10: an offline sale freezes the till exactly as a live one
      // does. It is a completed transaction as far as the customer walking out
      // of the shop is concerned, and the cashier needs the same "this is done,
      // start the next one" boundary - arguably more, since there is no server
      // receipt to confirm it against.
      const offlineTotal = cartItems.reduce((sum, it, i) => sum + it.qty * (posCart[i] ? posCart[i].salePrice : 0), 0);
      const offlineBill = checkoutPayload.offline_bill_number || ''; // 76.10: the till's own number
      freezePOSTill({
        cartNumber,
        billNumber: offlineBill,
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
      showToast(`No connection - sale ${offlineBill || cartNumber} queued offline and will sync automatically once reconnected.`, { variant: 'warning', title: 'Offline' });
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
        posLoyalty = data.quote.loyalty || null;
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
    if (!res.ok && data.code === 'POSOFF-0877') {
      // Stage 85.E7: over the limit the discount needs its reason.
      const reason = await showCustomPrompt(`${data.detail || 'This discount needs a reason.'}`, '', 'Discount Reason');
      if (reason && reason.trim()) { posDiscountReason = reason.trim(); await submitPOSCheckoutInner(); }
      return;
    }
    if (!res.ok && data.code === 'POSOFF-1363' && !posSecondPayment) {
      // 49.5.4: the same card slip / UTR was taken minutes ago. Only the
      // cashier can say the customer really paid twice.
      if (await showCustomConfirm(`${data.detail || 'This payment was already taken on an earlier bill.'}\n\nIs this a second payment with the same reference?`, 'Second payment')) {
        posSecondPayment = true;
        await submitPOSCheckoutInner();
      }
      return;
    }
    if (!res.ok && data.code === 'POSOFF-1372' && !posBelowCostOK) {
      // 273.4: an item is sold below cost; the cashier confirms the price.
      if (await showCustomConfirm(`${data.detail || 'An item on this bill is sold below cost.'}\n\nBill it at this price?`, 'Below cost')) {
        posBelowCostOK = true;
        await submitPOSCheckoutInner();
      }
      return;
    }
    if (!res.ok) {
      errorEl.textContent = composeErrorText({ message: data.error, detail: data.detail, userAction: data.user_action }) || 'Checkout failed.';
      errorEl.classList.remove('hidden');
      if (data.code === 'POSOFF-0866' || data.code === 'POSOFF-0867') (await posCustomerMod()).posFixCustomerForBill(data);
      // 215.2: the server refused the B2B buyer's details - open them there.
      if (['POSOFF-0879', 'POSOFF-0880', 'POSOFF-0881'].includes(data.code) || (data.code === 'CUSTOM-0869' && posCustomerRef && posCustomerRef.posB2BBlock())) {
        (await posCustomerMod()).openPOSB2BPanel([data.error, data.detail].filter(Boolean).join(' '));
      }
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
    // 73.1.6: a split bill shows each part; the cash change is on the rest.
    const rest = (Number(amountDue) || 0) - posTenders.reduce((s, t) => s + t.amount, 0);
    freezePOSTill({
      cartNumber: data.cart_number,
      billNumber: data.bill_number || '',
      gstBill: !!(posCustomerRef && posCustomerRef.posCustomerIsB2B()),
      status: 'paid',
      heading: 'Sale complete',
      // 73.2.6: an exchange whose credit was more than the new bill.
      note: data.exchange_paid_back > 0 ? `Exchange: give the customer ${Number(data.exchange_paid_back).toFixed(2)} back from the till.` : '',
      paymentMode: posTenders.length ? posTenders.map(t => `${t.mode} ${t.amount.toFixed(2)} + `).join('') + `${paymentMode} ${rest.toFixed(2)}` : paymentMode,
      items: posReceiptItems(cartItems),
      saleTotal: Number(data.sale_total) || 0,
      amountDue: Number(amountDue) || 0,
      loyaltyDiscount: Number(data.loyalty_discount) || 0,
      loyaltyPoints: Number(data.loyalty_points_redeemed) || 0,
      offerDiscount: Number(data.offer_discount) || 0,
      // Stage 72.8: read before freezePOSTill clears the tenders.
      giftCards: posGiftCardTenders.map(g => ({ card: g.card, amount: g.amount })),
      advanceUsed: (checkoutPayload.advances || []).reduce((sum, a) => sum + a.amount, 0),
      remarks: checkoutPayload.remarks,
      tendered: isNaN(tendered) ? null : tendered,
      changeDue: isNaN(tendered) ? null : tendered - rest,
      canPrint: true
    });
}

// posReceiptItems turns the CHECKOUT payload's lines back into lines a person
// can read, by pairing each with the cart line it came from.
//
// The checkout payload sends sale_price 0 for server-priced lines, so the
// receipt takes the cart line's resolved price (bug found in 53.10).
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
      sale_price: line ? Number(line.salePrice) || 0 : Number(it.sale_price) || 0,
      hidden: !!(line && line.billMode === 'HIDDEN_INTERNAL' && !(Number(line.salePrice) > 0))
    };
  // Stage 72: an item whose profile keeps it off the customer's bill.
  }).filter(it => !it.hidden);
}

// --- Stage 53.10: the frozen completed bill ---------------------------------
//
// The till stops: the bill stays up, the inputs go, and New Sale is the only
// way on, so the next customer's items cannot land on this bill.
//
// Every terminal outcome funnels through here - paid, queued offline, pending
// approval - so none of them can leave a half-live cart behind. (The 409
// price-changed path is deliberately NOT terminal: it re-enters checkout and
// freezes on whatever that returns.)
function freezePOSTill(sale) {
  sale.cashTendered = +document.getElementById('pos-cash-tendered')?.value || 0;
  posCompletedSale = sale;
  posCart = [];
  posCartNumber = '';
  clearPOSRedemption();
  clearPOSProductSearch();
  clearPOSPackagingState();
  renderPOSCartTable();

  const tillBody = document.getElementById('pos-till-body');
  const panel = document.getElementById('pos-completed-sale');
  if (!panel) return;
  if (tillBody) tillBody.classList.add('hidden');

  const colour = sale.status === 'paid' ? 'var(--success-color, #1a7f37)' : 'var(--warning-color, #b26a00)';
  panel.style.borderColor = colour;
  const lines = (sale.items || []).map(it => `
    <div class="s-7f3b27">
      <span>${cfgEsc(it.name || it.sku)} &times; ${it.qty}</span>
      <span>${(it.qty * (Number(it.sale_price) || 0)).toFixed(2)}</span>
    </div>`).join('');
  const money = (label, value, strong) => `
    <div data-css="display:flex; justify-content:space-between; padding:3px 0; ${strong ? 'font-size:18px; font-weight:700; padding-top:8px; border-top:1px solid var(--border-color);' : 'font-size:13.5px;'}">
      <span>${cfgEsc(label)}</span><span>${Number(value).toFixed(2)}</span>
    </div>`;

  panel.innerHTML = `
    <div class="s-74f21a">
      <span data-css="font-size:18px; font-weight:700; color:${colour};">${cfgEsc(sale.heading)}</span>
      <span class="s-b14cdc">${cfgEsc(sale.billNumber ? `Bill ${sale.billNumber}` : (sale.cartNumber || ''))}</span>
      <span class="s-b14cdc">${cfgEsc(posStoreLabel())}</span>
      <span class="s-b14cdc">${cfgEsc(sale.paymentMode || '')}</span>
    </div>
    ${sale.note ? `<div class="s-afa980">${cfgEsc(sale.note)}</div>` : ''}
    ${sale.remarks ? `<div class="s-b14cdc">Remarks: ${cfgEsc(sale.remarks)}</div>` : ''}
    <div class="s-0fd874">
      ${lines}
      ${sale.offerDiscount > 0 ? money('Discount (offers and bill)', -sale.offerDiscount) : ''}
      ${sale.loyaltyDiscount > 0 ? money(`Loyalty points applied${sale.loyaltyPoints ? ` (${sale.loyaltyPoints} pt)` : ''}`, -sale.loyaltyDiscount) : ''}
      ${(sale.giftCards || []).map(g => money(`Gift card ${g.card}`, -g.amount)).join('')}
      ${sale.advanceUsed > 0 ? money('Advance / credit used', -sale.advanceUsed) : ''}
      ${money(sale.status === 'paid' ? 'Collected' : 'Total', sale.amountDue, true)}
      ${sale.tendered !== null && sale.tendered !== undefined ? money('Cash tendered', sale.tendered) : ''}
      ${sale.changeDue !== null && sale.changeDue !== undefined ? money('Change due', sale.changeDue, true) : ''}
    </div>
    <div class="s-a1aa18">
      <button class="btn btn-primary" id="pos-new-sale-btn" type="button">New Sale (Reset)</button>
      ${sale.canPrint ? '<button class="btn btn-outline" id="pos-print-receipt-btn" type="button">Print Receipt</button>' : ''}
      ${sale.gstBill && sale.status === 'paid' ? '<button class="btn btn-outline" id="pos-print-gst-btn" type="button">Print GST Invoice</button>' : ''}
    </div>
  `;
  panel.classList.remove('hidden');

  const newBtn = document.getElementById('pos-new-sale-btn');
  if (newBtn) { newBtn.addEventListener('click', startNewPOSSale); newBtn.focus(); }
  // Stage 143.4: Print Receipt shows the bill first; Print there prints.
  // 213.3: in the print dialog - paper, template and printer for this bill,
  // the print profile's (or the roll) first; the plain receipt when the bill
  // cannot print that way.
  const printBtn = document.getElementById('pos-print-receipt-btn');
  if (printBtn) printBtn.addEventListener('click', async () => {
    let docs = null;
    try { docs = await import('./trade-docs.js' + new URL(import.meta.url).search); } catch (e) {
      console.debug('[POS] print dialog unavailable, using the plain receipt', e);
      printCompletedPOSSale();
      return;
    }
    docs.printTransactionDocument('POSCart', sale.cartNumber, { txn: 'POSTaxInvoice', roll: true, cashTendered: sale.cashTendered, onUnavailable: printCompletedPOSSale });
  });
  // Stage 85.D2: the tax invoice, from the Stage 71 print pack.
  const gstBtn = document.getElementById('pos-print-gst-btn');
  if (gstBtn) gstBtn.addEventListener('click', () => import('./trade-docs.js' + new URL(import.meta.url).search)
    .then(m => m.printTransactionDocument('POSCart', sale.cartNumber, { txn: 'POSTaxInvoice' })));
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
  const customerPhone = document.getElementById('pos-customer-phone');
  if (customerPhone) { customerPhone.value = ''; customerPhone.dataset.resolved = ''; }
  if (posCustomerRef) posCustomerRef.posClearCustomer(); // and the B2B buyer (215.2)
  const discountEl = document.getElementById('pos-discount-pct');
  if (discountEl) discountEl.value = '0';
  posDiscountReason = '';
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

// printCompletedPOSSale (the reprint) is in view-pos-extras.js.

// posStoreLabel is how the store reads to a human - its name, falling back to
// the code only before the record loads. Used on the session line, the frozen
// bill and the browser-printed receipt, so all say the same thing. Name only
// (Stage 57.1): the code no longer rides along in brackets.
function posStoreLabel() {
  const p = posStoreProfile, w = window.erpWorkingStore;
  return (p && p.code === posLocation && p.name) || (w && w.code === posLocation && w.name) || posLocation || '';
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

  // sellable follows engines.LocationIsSellable: no Location record sells
  // (unregistered codes keep working), a blank flag follows the type.
  const profile = { code, name: code, type: '', sellable: true, entityName: '', gstin: '', stateName: '', cashier: '' };
  const locRes = await apiFetch(`/api/v1/doc/Location/${encodeURIComponent(code)}`);
  if (locRes && locRes.ok) {
    const loc = await locRes.json();
    profile.name = loc.name || code;
    profile.type = loc.type || '';
    profile.sellable = loc.sellable ? loc.sellable === 'Yes' : profile.type === 'Store';
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

// The browser receipt (the fallback behind PrintBridge) is in view-pos-extras.js;
// Stage 73.1.6 moved it out of the cold core.
const printPOSReceipt = (...a) => posExtras().then(m => m.printPOSReceipt(posTill, ...a));

// Stage 30.2.5: the points a cashier has added to THIS cart, not yet burned.
// The burn happens server-side when the sale completes. Cleared whenever the
// cart is (completed, queued offline, or abandoned by leaving the screen), so
// points can never be spent on a sale that didn't happen.
let posRedeemPoints = 0;
// 73.1.4/73.1.5: the quote's points pricing, and what authorised the
// redemption (sent with the sale as redemption_challenge_id/_approval_id).
let posLoyalty = null;
let posRedemptionAuth = {};
// 73.1.6: fixed parts of a split payment ({mode, amount}); Payment Mode takes
// the rest.
let posTenders = [];
// 49.5.4: the cashier confirmed this bill's tender repeats an earlier one on purpose.
let posSecondPayment = false;
// 273.4: the cashier confirmed this bill's below-cost price.
let posBelowCostOK = false;
// 73.2.6: an exchange's credit ({id, amount}) paying this bill.
let posExchange = null;

function clearPOSRedemption() {
  posRedeemPoints = 0;
  posLoyalty = null;
  posRedemptionAuth = {};
  posTenders = [];
  posSecondPayment = false;
  posBelowCostOK = false;
  posExchange = null;
  document.getElementById('pos-split-row')?.remove();
  document.getElementById('pos-exchange-row')?.remove();
  // Stage 30.7: a completed/queued sale must not leave its offers or coupon
  // sitting on the next customer's cart.
  posOfferDiscount = 0;
  posAppliedOffers = [];
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.value = '';
  (document.getElementById('pos-remarks') || {}).value = '';
  const offersRow = document.getElementById('pos-offers-row');
  if (offersRow) { offersRow.classList.add('hidden'); offersRow.innerHTML = ''; }
}

// Stage 53: the new names are exported for the same reason the old ones are -
// app.js does Object.assign(window, module) on import, and the cart table's
// inline onclick handlers (stepPOSCartLine, removeSKUFromPOSCart) resolve off
// window.
// --- Stage 72: packaging, gifts and items the till does not sell -------------
//
// The quote says what the cart uses up with its lines - the Backflush Rules'
// default box per line, butter paper - and which other boxes each line may
// switch to. Nothing is scanned: the default is already chosen, and Packaging
// is for the customer who gets something else. None of it is printed on the
// bill; it leaves stock with the sale, linked to the bill number.

// Price sources the server prices at 0 on purpose.
const POS_FREE_SOURCES = new Set(['item_type_free', 'gwp_offer']);

// Also drops the gift cards paying the bill - every place that ends a cart
// (clear, freeze, empty) calls this.
function clearPOSPackagingState() {
  posPackaging = [];
  posPackagingOverrides = {};
  posGifts = [];
  posPackagingShortages = [];
  posGiftCardTenders = [];
  renderPOSPackagingRow();
}

function posPackagingKey(p) {
  return `${p.for_sku}|${p.group}`.toLowerCase();
}

// What a packaging line consumes once the cashier's change is applied.
function posPackagingChoice(p) {
  const o = posPackagingOverrides[posPackagingKey(p)];
  if (!o) return { component: p.component, name: p.component_name || p.component, qty: p.qty, changed: false };
  return { component: o.component, name: o.component ? (o.name || o.component) : 'None', qty: o.component ? p.qty : 0, changed: true };
}

function renderPOSPackagingRow() {
  const row = document.getElementById('pos-packaging-row');
  const btn = document.getElementById('pos-packaging-btn');
  const changeable = posPackaging.filter(p => p.override_allowed && ((p.choices || []).length > 1 || p.none_allowed));
  if (btn) btn.classList.toggle('hidden', !changeable.length);
  if (!row) return;
  const parts = [];
  posGifts.forEach(g => {
    const taken = posCart.filter(l => String(l.sku).toLowerCase() === String(g.gift_item).toLowerCase()).reduce((s, l) => s + l.qty, 0);
    const left = g.qty - taken;
    parts.push(`<div class="s-203476">Gift eligible: ${g.qty} &times; ${cfgEsc(g.gift_name)}
      <span class="s-3650c6">(${cfgEsc(g.name)}) &mdash; ${left > 0 ? `scan ${left === g.qty ? 'it' : left + ' more'} to hand it over` : 'scanned'}</span></div>`);
  });
  const totals = {};
  posPackaging.forEach(p => {
    const c = posPackagingChoice(p);
    if (!c.component || !c.qty) return;
    const key = c.name + (c.changed ? ' *' : '');
    totals[key] = (totals[key] || 0) + c.qty;
  });
  const used = Object.keys(totals).map(k => `${totals[k]} &times; ${cfgEsc(k)}`);
  if (used.length) {
    parts.push(`<div class="s-2d9834">Uses (not on the bill): ${used.join(', ')}${Object.keys(totals).some(k => k.endsWith(' *')) ? ' &middot; * changed' : ''}</div>`);
  }
  posPackagingShortages.forEach(s => {
    const blocking = s.action === 'block';
    parts.push(`<div data-css="font-size:12px; color:${blocking ? 'var(--danger-color, #b3261e)' : 'var(--warning-color, #b26a00)'};">
      Short of ${cfgEsc(s.name || s.component)}: store has ${s.available}, this sale uses ${s.needed} &mdash; ${blocking ? 'choose another option under Packaging' : 'the sale goes ahead and the shortfall is listed under Item Exceptions'}</div>`);
  });
  row.innerHTML = parts.join('');
  row.classList.toggle('hidden', !parts.length);
}

// The Return tab, + New Customer, Check / Redeem Points and the Packaging /
// Gift Card dialogs are in view-pos-extras.js, so this first screen stays
// inside the cold-core budget (cmd/releasebudget). posTill is how they reach
// this module's state.
const posExtras = () => import('./view-pos-extras.js' + new URL(import.meta.url).search)
  .catch(e => { showCustomAlert('Part of the till did not load. Check the connection, then reload the page.', 'POS'); throw e; });
// Stage 85.C: the phone-first customer (view-pos-customer.js); posCustomerRef
// once loaded, for the synchronous one-line note.
let posCustomerRef = null;
const posCustomerMod = () => import('./view-pos-customer.js' + new URL(import.meta.url).search)
  .then(m => (posCustomerRef = m))
  .catch(e => { showCustomAlert('Part of the till did not load. Check the connection, then reload the page.', 'POS'); throw e; });
const posTill = {
  get packaging() { return posPackaging; },
  get overrides() { return posPackagingOverrides; },
  get tenders() { return posGiftCardTenders; },
  set tenders(v) { posGiftCardTenders = v; },
  set redeemPoints(v) { posRedeemPoints = v; },
  get loyalty() { return posLoyalty; },
  set auth(v) { posRedemptionAuth = v; },
  get split() { return posTenders; },
  set split(v) { posTenders = v; renderPOSTender(); },
  set exchange(v) { posExchange = v; renderPOSCartTable(true); },
  get exchangeActive() { return !!posExchange; },
  get storeProfile() { return posStoreProfile; },
  get cart() { return posCurrentCartNumber(); },
  get completedSale() { return posCompletedSale; },
  get location() { return posLocation; },
  quote: refreshPOSQuote,
  get session() { return posOpenSessionId; },
  refreshSession: refreshPOSSessionStatus,
  set cartNumber(v) { posCartNumber = v; },
  clear: clearPOSTillState,
  key: posPackagingKey,
  choice: posPackagingChoice,
  renderPackaging: renderPOSPackagingRow,
  renderCart: renderPOSCartTable,
  storeLabel: posStoreLabel
};
const posLazy = (name) => async () => (await posExtras())[name](posTill);
const openPOSPackaging = posLazy('openPOSPackaging');
const openPOSGiftCard = posLazy('openPOSGiftCard');
const openPOSMore = posLazy('openPOSMore');
const closePOSSessionFlow = posLazy('closePOSSessionFlow');
const printCompletedPOSSale = posLazy('printCompletedPOSSale');
const openPOSPriceOverride = async (sku) => (await posExtras()).openPOSPriceOverride(posTill, sku);
const checkPOSLoyaltyBalance = posLazy('checkPOSLoyaltyBalance');
const redeemPOSLoyaltyPoints = posLazy('redeemPOSLoyaltyPoints');

// dropPOSRefusedLine handles a quote refused because of ONE line's item policy
// - packaging scanned onto the bill, a gift the bill has not earned, a gift
// card scanned as merchandise: that line comes off and the cashier is told
// why, instead of the whole cart going unpriced. The server names the line as
// "Name (SKU)"; the line just scanned is the fallback.
async function dropPOSRefusedLine(res) {
  let body = {};
  try { body = await res.clone().json(); } catch (e) { return false; }
  if (!['POSOFF-0411', 'POSOFF-0412', 'POSOFF-0413'].includes(body.code)) return false;
  const msg = body.detail || body.error || 'This item cannot be billed here.';
  const bad = posCart.find(l => msg.includes(`(${l.sku})`)) || posCart.find(l => l.sku === posLastAddedSku);
  if (!bad) return false;
  posCart = posCart.filter(l => l !== bad);
  const errorEl = document.getElementById('pos-scan-error');
  if (errorEl) {
    errorEl.textContent = msg;
    errorEl.classList.remove('hidden');
  }
  renderPOSCartTable();
  return true;
}

export { posMRPCell, pickPOSLineMRP, renderPOSView, refreshPOSSessionStatus, openPOSSessionFlow, closePOSSessionFlow, addSKUToPOSCart, removeSKUFromPOSCart, updatePOSCartLine, renderPOSCartTable, posCanOverridePrice, posPriceCell, openPOSPriceOverride, posCurrentCartNumber, currentPOSCouponCodes, refreshPOSQuote, submitPOSCheckout, submitPOSCheckoutInner, printPOSReceipt, checkPOSLoyaltyBalance, clearPOSRedemption, redeemPOSLoyaltyPoints, resetPOSTerminal, renderPOSCustomerNote, clearPOSProductSearch, onPOSScanKeyDown, highlightPOSSearchResult, renderPOSSearchResults, pickPOSSearchResult, addResolvedItemToPOSCart, stepPOSCartLine, posAvailabilityCell, renderPOSTender, posReceiptItems, freezePOSTill, startNewPOSSale, printCompletedPOSSale, posStoreLabel, loadPOSStoreProfile, clearPOSTillState };
