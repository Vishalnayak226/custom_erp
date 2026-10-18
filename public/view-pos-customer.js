// Stage 85.C (user, 2026-10-18): "Customer phone number is not asked in bill
// ... Phone number is unque. Ask Always. Where address and all i ask asked.
// DOB etc ... PAN gst."
//
// The till's customer, phone first:
//   * the box takes a mobile number (a name still searches); Enter looks the
//     number up - one number is one customer (engines/pos_customers.go);
//   * a number on file attaches that customer; a new one opens the customer
//     form with the number filled in: name first, the rest under More
//     details (address lines, city, state, PIN, PAN, GSTIN, DOB, anniversary,
//     gender) - a GSTIN makes it a GST (B2B) bill;
//   * Complete Sale asks for the number when none was taken; Walk-in is the
//     answer when the customer will not give one (the store's default
//     customer, whose placeholder number is never printed);
//   * a bill over the country's limits asks for the address / PAN first, the
//     same rules the server enforces at checkout (POSOFF-0866/0867/0868).
// Lazy-loaded by view-pos.js, so none of this is in the till's first load.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const pickers = () => loadViewModule('/view-pickers.js?v=1');

let attached = null; // the CustomerRef on this bill, or null
let policy = null;

const hiddenEl = () => document.getElementById('pos-customer');
const displayEl = () => document.getElementById('pos-customer-display');
const digits = (s) => String(s || '').replace(/[^\d+]/g, '');

// posAttachCustomer puts a customer on the bill the way a pick does: the
// code in the hidden box, the name shown, then the change event the till
// listens for (quote, offers, loyalty).
export function posAttachCustomer(c) {
  attached = c && c.id ? c : null;
  const hidden = hiddenEl(), display = displayEl();
  if (display) { display.value = attached ? (attached.walk_in ? 'Walk-in' : attached.name) : ''; display.dataset.resolved = attached ? '1' : ''; }
  if (hidden) { hidden.value = attached ? attached.id : ''; hidden.dispatchEvent(new Event('change', { bubbles: true })); }
}

// posCustomerSummary is the one line under the box (85.C5): who, their
// number, and B2B when the bill will be a GST invoice.
export function posCustomerSummary(code, shown) {
  if (!code) return 'Ask the mobile number - Enter looks it up. Walk-in if they will not give one.';
  const c = attached && attached.id === code ? attached : null;
  if (c && c.walk_in) return 'Walk-in - no number taken. No points, not on the bill.';
  const parts = [c ? c.name : (shown || code)];
  if (c && c.phone) parts.push(c.phone);
  if (c && c.gstin) parts.push(`GST bill (${c.gstin})`);
  return parts.join(' · ');
}

async function loadPolicy() {
  if (!policy) {
    const res = await apiFetch('/api/v1/pos/high-value-policy').catch(() => null);
    policy = res && res.ok ? await res.json() : {};
  }
  return policy;
}

async function refreshAttached() {
  const code = (hiddenEl() || {}).value || '';
  if (!code) { attached = null; return null; }
  if (attached && attached.id === code && attached.fresh) return attached;
  const res = await apiFetch(`/api/v1/doc/Customer/${encodeURIComponent(code)}`).catch(() => null);
  if (!res || !res.ok) return attached;
  const d = await res.json();
  attached = {
    id: code, name: d.name || code, phone: d.walk_in === 'Yes' ? '' : (d.phone || ''), gstin: d.gstin || '', pan: d.pan || '',
    address: d.address || '', address_line2: d.address_line2 || '', city: d.city || '', state: d.state || '', pincode: d.pincode || '',
    walk_in: d.walk_in === 'Yes', fresh: true,
  };
  attached.address_complete = !!(attached.address && attached.state && (attached.city || attached.pincode));
  return attached;
}

// posCustomerFind runs on Enter in the customer box: a number is looked up.
export async function posCustomerFind() {
  const display = displayEl();
  const raw = display ? display.value.trim() : '';
  if (!raw || display.dataset.resolved === '1') return;
  const num = digits(raw);
  if (num.replace('+', '').length < 8) return; // a name: the search list handles it
  const res = await apiFetch(`/api/v1/pos/customer-lookup?phone=${encodeURIComponent(num)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'That number could not be looked up.'); return; }
  const data = await res.json();
  if (data.found) {
    posAttachCustomer(data.customer);
    showToast(`${data.customer.name} is on this bill.`, { variant: 'success' });
    return;
  }
  await openPOSCustomerForm({ phone: data.phone || num, title: 'New customer' });
}

export async function posAttachWalkIn() {
  const res = await apiFetch('/api/v1/pos/walk-in-customer', { method: 'POST', body: JSON.stringify({ store: posLocation }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'The walk-in customer could not be used.'); return false; }
  posAttachCustomer(await res.json());
  return true;
}

function field(id, label, value, attrs = '') {
  return `<div class="form-group" style="margin-bottom:0;"><label class="form-label" for="${id}">${label}</label>
    <input id="${id}" class="form-input" value="${esc(value)}" autocomplete="off" ${attrs}></div>`;
}

// openPOSCustomerForm: new (phone given) or edit (id given). need names what
// the bill is waiting for, so the dialog opens on More details when the
// address or PAN is what is missing. Resolves true when saved and attached.
let lastSaved = null; // the customer the form saved last ({id, name, phone})

export async function openPOSCustomerForm({ id = '', phone = '', title = '', need = '', attach = true } = {}) {
  let doc = {};
  if (id) {
    const res = await apiFetch(`/api/v1/doc/Customer/${encodeURIComponent(id)}`);
    if (!res || !res.ok) { await showApiError(res, 'The customer could not be opened.'); return false; }
    doc = await res.json();
  }
  const p = await pickers();
  const country = (window.FIELD_META && window.FIELD_META.country) || 'IN';
  const gstLabel = p.taxIdLabel ? p.taxIdLabel('gstin', 'GSTIN') : 'GSTIN';
  const isNew = !id;
  document.getElementById('pos-customer-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pos-customer-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 720px;" role="dialog" aria-modal="true" aria-labelledby="pos-cust-title">
      <div class="modal-header"><h3 class="modal-title" id="pos-cust-title">${esc(title || (isNew ? 'New customer' : `Customer - ${doc.name || id}`))}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-pc-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <form class="modal-body" id="pos-cust-form" novalidate style="display:flex; flex-direction:column; gap:12px;">
        ${need ? `<div class="login-error">${esc(need)}</div>` : ''}
        <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap:12px;">
          ${field('pc-phone', 'Mobile number<span class="required">*</span>', doc.walk_in === 'Yes' ? '' : (doc.phone || phone), 'type="tel" inputmode="tel" required')}
          ${field('pc-name', 'Name<span class="required">*</span>', doc.name || '', 'required')}
          ${field('pc-email', 'Email', doc.email || '', 'type="email"')}
        </div>
        <details id="pc-more" ${need ? 'open' : ''}>
          <summary style="cursor:pointer; font-weight:600;">More details - address, PAN, ${esc(gstLabel)}, birthday</summary>
          <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap:12px; margin-top:12px;">
            ${field('pc-address', 'Address Line 1', doc.address || '')}
            ${field('pc-address2', 'Address Line 2', doc.address_line2 || '')}
            ${field('pc-city', 'City', doc.city || '')}
            <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="pc-state">State</label><span id="pc-state-slot"></span></div>
            ${field('pc-pincode', country === 'IN' ? 'PIN Code' : 'Postal code', doc.pincode || '', 'inputmode="numeric"')}
            ${field('pc-gstin', esc(gstLabel) + ' <span class="txn-hint">(makes it a GST bill)</span>', doc.gstin || '', 'maxlength="20" style="text-transform:uppercase"')}
            ${country === 'IN' ? field('pc-pan', 'PAN', doc.pan || '', 'maxlength="10" style="text-transform:uppercase"') : ''}
            ${field('pc-dob', 'Date of birth', doc.date_of_birth || '', 'type="date"')}
            ${field('pc-doa', 'Anniversary', doc.date_of_anniversary || '', 'type="date"')}
            <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="pc-gender">Gender</label>
              <select id="pc-gender" class="form-select"><option value="">-</option>${['Female', 'Male', 'Other'].map(g => `<option ${doc.gender === g ? 'selected' : ''}>${g}</option>`).join('')}</select></div>
          </div>
        </details>
        ${isNew ? `<div><div class="form-label">Agrees to offers (tick only what they agreed to)</div>
          <label style="display:inline-flex; gap:8px; align-items:center; margin-right:16px;"><input type="checkbox" id="pc-sms"> SMS</label>
          <label style="display:inline-flex; gap:8px; align-items:center;"><input type="checkbox" id="pc-wa"> WhatsApp</label></div>` : ''}
        <div id="pc-error" class="login-error hidden"></div>
      </form>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-pc-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-pc-save>${isNew ? 'Add to bill' : 'Save'}</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const slot = overlay.querySelector('#pc-state-slot');
  if (p.buildStateSelect && (window.FIELD_META?.regions || []).length) {
    slot.replaceWith(p.buildStateSelect({ id: 'pc-state', name: 'state', value: doc.state || '' }));
  } else {
    slot.outerHTML = `<input id="pc-state" name="state" class="form-input" value="${esc(doc.state || '')}" autocomplete="off">`;
  }
  const gst = overlay.querySelector('#pc-gstin');
  if (gst && p.fillStateFromTaxId) gst.addEventListener('input', () => p.fillStateFromTaxId(gst));

  return new Promise(resolve => {
    const close = (v) => { overlay.remove(); resolve(v); };
    overlay.querySelectorAll('[data-pc-close]').forEach(b => b.addEventListener('click', () => close(false)));
    overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(false); });
    const save = overlay.querySelector('[data-pc-save]');
    save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
      const v = (sel) => (overlay.querySelector(sel) || {}).value?.trim() || '';
      const err = overlay.querySelector('#pc-error');
      err.classList.add('hidden');
      if (!v('#pc-phone') || !v('#pc-name')) {
        err.textContent = 'The mobile number and the name are needed.';
        err.classList.remove('hidden');
        return;
      }
      const body = {
        ...(isNew ? {} : doc), name: v('#pc-name'), phone: v('#pc-phone'), email: v('#pc-email'),
        address: v('#pc-address'), address_line2: v('#pc-address2'), city: v('#pc-city'), state: v('#pc-state'),
        pincode: v('#pc-pincode'), gstin: v('#pc-gstin').toUpperCase(), date_of_birth: v('#pc-dob'),
        date_of_anniversary: v('#pc-doa'), gender: v('#pc-gender'), status: doc.status || 'Active',
      };
      if (overlay.querySelector('#pc-pan')) body.pan = v('#pc-pan').toUpperCase();
      if (isNew) { body.consent_sms = !!overlay.querySelector('#pc-sms')?.checked; body.consent_whatsapp = !!overlay.querySelector('#pc-wa')?.checked; }
      if (doc.walk_in === 'Yes') { delete body.walk_in; delete body.walk_in_store; delete body.id; delete body.code; }
      const editing = !isNew && doc.walk_in !== 'Yes';
      const res = await apiFetch(editing ? `/api/v1/doc/Customer/${encodeURIComponent(id)}` : '/api/v1/doc/Customer', {
        method: 'POST', body: JSON.stringify(body),
      });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'The customer was not saved.'); return; }
      const saved = await res.json().catch(() => ({}));
      const code = editing ? id : (saved.id || saved.code || '');
      lastSaved = { id: code, name: body.name, phone: body.phone };
      // attach = false: the Advance tab's customer, not the sale's.
      if (attach) {
        attached = null;
        posAttachCustomer({ id: code, name: body.name });
        await refreshAttached();
        if (typeof renderPOSCustomerNote === 'function') renderPOSCustomerNote();
      }
      showToast(`${body.name} ${editing ? 'saved' : 'added to this bill'}.`, { variant: 'success' });
      close(true);
    }));
    (overlay.querySelector(phone ? '#pc-name' : '#pc-phone'))?.focus();
  });
}

// posAskMobile is Complete Sale with no customer: ask for the number, with
// Walk-in for a customer who will not give one. Resolves true once someone is
// on the bill.
async function posAskMobile() {
  const picked = await new Promise(resolve => {
    document.getElementById('pos-ask-dialog')?.remove();
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open';
    overlay.id = 'pos-ask-dialog';
    overlay.innerHTML = `
      <div class="modal-container" style="max-width: 440px;" role="dialog" aria-modal="true" aria-labelledby="pos-ask-title">
        <div class="modal-header"><h3 class="modal-title" id="pos-ask-title">Customer's mobile number</h3></div>
        <div class="modal-body" style="display:flex; flex-direction:column; gap:10px;">
          <input id="pos-ask-phone" class="form-input" type="tel" inputmode="tel" placeholder="Mobile number" autocomplete="off">
          <p class="txn-hint" style="margin:0;">Ask on every bill. If the customer will not give a number, use Walk-in.</p>
        </div>
        <div class="modal-footer"><button type="button" class="btn btn-outline" data-ask="cancel">Cancel</button>
          <button type="button" class="btn btn-outline" data-ask="walkin">Walk-in</button>
          <button type="button" class="btn btn-primary" data-ask="find">Continue</button></div>
      </div>`;
    document.body.appendChild(overlay);
    const input = overlay.querySelector('#pos-ask-phone');
    const done = (v) => { overlay.remove(); resolve(v); };
    overlay.querySelector('[data-ask="cancel"]').addEventListener('click', () => done(null));
    overlay.querySelector('[data-ask="walkin"]').addEventListener('click', () => done({ walkIn: true }));
    overlay.querySelector('[data-ask="find"]').addEventListener('click', () => done({ phone: input.value.trim() }));
    input.addEventListener('keydown', e => { if (e.key === 'Enter') done({ phone: input.value.trim() }); if (e.key === 'Escape') done(null); });
    input.focus();
  });
  if (!picked) return false;
  if (picked.walkIn) return posAttachWalkIn();
  if (!picked.phone) return posAskMobile();
  const display = displayEl();
  if (display) { display.value = picked.phone; display.dataset.resolved = ''; }
  await posCustomerFind();
  return !!((hiddenEl() || {}).value);
}

// posEnsureCustomer runs first in Complete Sale. Resolves { form97 } to go
// on, or null to stop (the cashier cancelled, or details are still missing).
export async function posEnsureCustomer({ total = 0, cash = 0 } = {}) {
  if (!((hiddenEl() || {}).value)) {
    if (!(await posAskMobile())) return null;
    // The customer changes the quote (their price list, offers, points): the
    // sale goes on with the fresh one, not a stale quote the server would query.
    if (typeof window.refreshPOSQuote === 'function') await window.refreshPOSQuote();
  }
  const p = await loadPolicy();
  const c = await refreshAttached();
  if (p.cash_limit > 0 && cash >= p.cash_limit) {
    await showCustomAlert(`Cash of ${cash.toFixed(2)} on one bill is over the limit (${p.cash_limit.toFixed(2)}). Take part of it by card or UPI (More... -> Split payment).`, 'Cash Limit');
    return null;
  }
  const needAddress = p.address_from > 0 && total >= p.address_from && (p.address_all || !(c && c.gstin));
  const needPAN = p.pan_above > 0 && total > p.pan_above && !(c && c.pan);
  if (!needAddress && !needPAN) return { form97: false };
  if (!c || c.walk_in) {
    await showCustomAlert(`A bill of ${total.toFixed(2)} needs the buyer's details. Take the customer's mobile number instead of Walk-in.`, 'Customer Details Needed');
    displayEl()?.focus();
    return null;
  }
  if (needAddress && !c.address_complete) {
    if (!(await openPOSCustomerForm({ id: c.id, need: `A bill of ${total.toFixed(2)} needs ${c.name}'s address (line 1, state and city or PIN).` }))) return null;
    return posEnsureCustomer({ total, cash });
  }
  if (needPAN) {
    const choice = await new Promise(resolve => {
      showCustomConfirm(`A bill above ${p.pan_above.toFixed(2)} needs ${c.name}'s PAN.\n\nOK - add the PAN now.\nCancel - they have no PAN and have signed Form 97.`, 'PAN Needed').then(resolve);
    });
    if (choice) {
      if (!(await openPOSCustomerForm({ id: c.id, need: `Add ${c.name}'s PAN for this bill.` }))) return null;
      return posEnsureCustomer({ total, cash });
    }
    return { form97: true };
  }
  return { form97: false };
}

// posFixCustomerForBill handles the server's own refusal (another till, an
// older screen): open the customer on the bill at the missing details.
export async function posFixCustomerForBill(data) {
  const code = (hiddenEl() || {}).value || '';
  if (code && !(attached && attached.walk_in)) {
    await openPOSCustomerForm({ id: code, need: [data.error, data.detail].filter(Boolean).join(' ') });
  } else {
    await showCustomAlert([data.error, data.detail, data.user_action].filter(Boolean).join('\n\n'), 'Customer Details Needed');
  }
}

export function posClearCustomer() { attached = null; openAdvances = []; useAdvance = false; const b = document.getElementById('pos-advance-use-btn'); if (b) b.classList.add('hidden'); }

// posCustomerIsB2B: the bill's customer has a GSTIN, so the sale gets a GST
// tax invoice (Stage 85.D2) beside the receipt.
export function posCustomerIsB2B() { return !!(attached && attached.gstin && !attached.walk_in); }

// --- Stage 85.D3: customer advances ------------------------------------------
//
// The Advance tab takes an advance (receipt voucher CA..., Rule 50) and lists
// the customer's advances with Refund; on a sale, a customer with an open
// advance gets "Use advance", which pays the bill from it first. The server
// checks it is the same customer and takes it off inside the sale.

let openAdvances = [];   // the bill customer's open advances
let useAdvance = false;  // the cashier pressed Use advance

const money2 = (v) => Number(v || 0).toFixed(2);

// posLoadOpenAdvances reads the bill customer's open advances and shows or
// hides Use advance.
export async function posLoadOpenAdvances() {
  const code = (hiddenEl() || {}).value || '';
  openAdvances = [];
  useAdvance = false;
  const btn = document.getElementById('pos-advance-use-btn');
  if (btn) btn.classList.add('hidden');
  if (!code || (attached && attached.id === code && attached.walk_in)) return;
  const res = await apiFetch(`/api/v1/pos/advances?customer_id=${encodeURIComponent(code)}&open=1`).catch(() => null);
  if (!res || !res.ok || ((hiddenEl() || {}).value || '') !== code) return;
  openAdvances = (await res.json().catch(() => [])) || [];
  posRenderAdvanceButton();
}

function posRenderAdvanceButton() {
  const btn = document.getElementById('pos-advance-use-btn');
  if (!btn) return;
  const total = openAdvances.reduce((s, a) => s + a.balance, 0);
  btn.classList.toggle('hidden', !(total > 0));
  btn.textContent = useAdvance ? `Not using advance` : `Use advance ${money2(total)}`;
  btn.classList.toggle('btn-primary', useAdvance);
  btn.classList.toggle('btn-outline', !useAdvance);
}

export function posToggleAdvance() {
  useAdvance = !useAdvance && openAdvances.length > 0;
  posRenderAdvanceButton();
  if (typeof renderPOSCartTable === 'function') renderPOSCartTable(true);
}

// posAdvanceApplied is what the advances pay of due: oldest first, each up to
// its balance, together no more than the bill.
export function posAdvanceApplied(due) {
  if (!useAdvance) return [];
  let left = Math.max(0, Number(due) || 0);
  const out = [];
  [...openAdvances].reverse().forEach(a => {
    if (left <= 0) return;
    const amount = Math.min(left, a.balance);
    if (amount > 0) { out.push({ number: a.number, amount: Math.round(amount * 100) / 100 }); left -= amount; }
  });
  return out;
}

function voucherHTML(title, rows, note) {
  return `<div class="receipt"><div class="receipt-header">
      <div style="font-weight:700;">${esc(window.posStoreLabel ? window.posStoreLabel() : '')}</div>
      <div style="font-weight:700; margin-top:4px;">${esc(title)}</div>
      <div>${new Date().toLocaleString()}</div></div><hr>
    ${rows.filter(r => r[1] !== '' && r[1] !== undefined && r[1] !== null).map(([k, v]) => `<div class="receipt-line"><span>${esc(k)}</span><span>${esc(v)}</span></div>`).join('')}
    <hr>${note ? `<div style="font-size:11px;">${esc(note)}</div>` : ''}</div>`;
}

function printVoucher(html) {
  const area = document.getElementById('receipt-print-area');
  if (!area) return;
  area.innerHTML = html;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

export function printAdvanceReceipt(a) {
  const taxable = a.tax ? a.amount - a.tax : 0;
  printVoucher(voucherHTML('ADVANCE RECEIPT VOUCHER', [
    ['Voucher', a.number], ['Customer', a.customer_name], ['For', a.purpose], ['Expected', a.expected_date],
    ['Supply', a.supply_type], ...(a.tax ? [['Taxable value', money2(taxable)], [`GST ${a.gst_rate}%`, money2(a.tax)]] : []),
    ['Received', `${money2(a.amount)} (${a.payment_mode})`], ['Balance', money2(a.balance)],
  ], 'Advance received against a future purchase. Not a tax invoice - it is adjusted on the final bill.'));
}

function printAdvanceRefund(a, voucher, amount, reason) {
  printVoucher(voucherHTML('ADVANCE REFUND VOUCHER', [
    ['Voucher', voucher], ['Against advance', a.number], ['Customer', a.customer_name],
    ['Refunded', `${money2(amount)} (${a.payment_mode})`], ['Reason', reason], ['Balance', money2(a.balance)],
  ], 'Refund of an advance, back the way it was paid.'));
}

let advanceCustomer = null;

// renderPOSAdvancePanel fills the till's Advance tab.
export function renderPOSAdvancePanel(pane) {
  pane.innerHTML = `
    <div class="txn-head" style="grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));">
      <div class="form-group"><label class="form-label" for="adv-phone">Customer mobile</label>
        <input id="adv-phone" class="form-input" type="tel" inputmode="tel" placeholder="Mobile number (Enter)" autocomplete="off">
        <div id="adv-customer" class="txn-hint">The advance belongs to this customer.</div></div>
      <div class="form-group"><label class="form-label" for="adv-amount">Amount</label>
        <input id="adv-amount" class="form-input" type="number" min="0" step="0.01" placeholder="0.00"></div>
      <div class="form-group"><label class="form-label" for="adv-mode">Paid by</label>
        <select id="adv-mode" class="form-input"><option>Cash</option><option>Card</option><option>UPI</option></select></div>
      <div class="form-group"><label class="form-label" for="adv-purpose">For</label>
        <input id="adv-purpose" class="form-input" placeholder="e.g. custom order, item on hold" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="adv-expected">Expected on</label>
        <input id="adv-expected" class="form-input" type="date"></div>
      <div class="form-group"><label class="form-label" for="adv-supply">Supply</label>
        <select id="adv-supply" class="form-input"><option value="Goods">Goods (no GST now)</option><option value="Services">Services (GST now)</option></select></div>
      <div class="form-group hidden" id="adv-rate-group"><label class="form-label" for="adv-rate">GST %</label>
        <input id="adv-rate" class="form-input" type="number" min="0" max="28" step="0.01" value="18"></div>
    </div>
    <div class="txn-entry"><button type="button" class="btn btn-primary" id="adv-take-btn">Take Advance</button></div>
    <div id="adv-error" class="login-error hidden"></div>
    <div class="txn-grid"><table><thead><tr><th>Voucher</th><th>Date</th><th>Customer</th><th>For</th><th>Taken</th><th>Used</th><th>Refunded</th><th>Balance</th><th>Status</th><th></th></tr></thead>
      <tbody id="adv-list"></tbody></table></div>`;
  const supply = pane.querySelector('#adv-supply');
  supply.addEventListener('change', () => pane.querySelector('#adv-rate-group').classList.toggle('hidden', supply.value !== 'Services'));
  pane.querySelector('#adv-phone').addEventListener('keydown', async (e) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    await findAdvanceCustomer(pane);
  });
  pane.querySelector('#adv-take-btn').addEventListener('click', () => guardAgainstDoubleSubmit(pane.querySelector('#adv-take-btn'), 'Taking...', () => takeAdvance(pane)));
  loadAdvanceList(pane);
}

async function findAdvanceCustomer(pane) {
  const num = digits(pane.querySelector('#adv-phone').value);
  const note = pane.querySelector('#adv-customer');
  if (num.replace('+', '').length < 8) { note.textContent = 'Type the mobile number, then Enter.'; return; }
  const res = await apiFetch(`/api/v1/pos/customer-lookup?phone=${encodeURIComponent(num)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'That number could not be looked up.'); return; }
  const data = await res.json();
  if (!data.found) {
    // A new customer: the same form as the till, then back here.
    if (!(await openPOSCustomerForm({ phone: data.phone || num, title: 'New customer for the advance', attach: false }))) return;
    advanceCustomer = lastSaved;
  } else {
    advanceCustomer = data.customer;
  }
  note.textContent = advanceCustomer ? `${advanceCustomer.name}${advanceCustomer.phone ? ' · ' + advanceCustomer.phone : ''}` : '';
  loadAdvanceList(pane);
}

async function loadAdvanceList(pane) {
  const body = pane.querySelector('#adv-list');
  if (!body) return;
  const qs = advanceCustomer ? `customer_id=${encodeURIComponent(advanceCustomer.id)}` : `store=${encodeURIComponent(posLocation)}&open=1`;
  const res = await apiFetch(`/api/v1/pos/advances?${qs}`).catch(() => null);
  const list = res && res.ok ? (await res.json().catch(() => [])) || [] : [];
  body.innerHTML = list.length ? list.map(a => `<tr>
      <td>${esc(a.number)}</td><td>${esc(a.taken_at)}</td><td>${esc(a.customer_name)}</td><td>${esc(a.purpose)}${a.expected_date ? `<div class="txn-hint">by ${esc(a.expected_date)}</div>` : ''}</td>
      <td>${money2(a.amount)}<div class="txn-hint">${esc(a.payment_mode)}${a.tax ? ` · GST ${money2(a.tax)}` : ''}</div></td>
      <td>${money2(a.applied)}</td><td>${money2(a.refunded)}</td><td><b>${money2(a.balance)}</b></td><td>${esc(a.status)}</td>
      <td style="white-space:nowrap;"><button type="button" class="action-btn" data-adv-print="${esc(a.number)}">Print</button>
        ${a.balance > 0 ? `<button type="button" class="action-btn action-btn-danger" data-adv-refund="${esc(a.number)}">Refund</button>` : ''}</td></tr>`).join('')
    : `<tr><td colspan="10" style="text-align:center; color:var(--text-muted);">${advanceCustomer ? 'No advances for this customer yet.' : 'No open advances at this store. Type a mobile number to see a customer\'s advances.'}</td></tr>`;
  body.querySelectorAll('[data-adv-print]').forEach(b => b.addEventListener('click', () => printAdvanceReceipt(list.find(a => a.number === b.dataset.advPrint))));
  body.querySelectorAll('[data-adv-refund]').forEach(b => b.addEventListener('click', () => refundAdvance(pane, list.find(a => a.number === b.dataset.advRefund))));
}

async function takeAdvance(pane) {
  const err = pane.querySelector('#adv-error');
  err.classList.add('hidden');
  if (!advanceCustomer) {
    err.textContent = 'Type the customer\'s mobile number and press Enter first - an advance belongs to a customer.';
    err.classList.remove('hidden');
    return;
  }
  const body = {
    location: posLocation, customer_id: advanceCustomer.id, amount: parseFloat(pane.querySelector('#adv-amount').value) || 0,
    payment_mode: pane.querySelector('#adv-mode').value, purpose: pane.querySelector('#adv-purpose').value.trim(),
    expected_date: pane.querySelector('#adv-expected').value, supply_type: pane.querySelector('#adv-supply').value,
    gst_rate: parseFloat(pane.querySelector('#adv-rate').value) || 0, idempotency_key: `adv-${posLocation}-${Date.now()}`,
  };
  const res = await apiFetch('/api/v1/pos/advances', { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The advance was not taken.'); return; }
  const a = await res.json();
  showToast(`${a.number}: ${money2(a.amount)} taken from ${a.customer_name}.`, { variant: 'success', title: 'Advance Taken' });
  pane.querySelector('#adv-amount').value = '';
  pane.querySelector('#adv-purpose').value = '';
  loadAdvanceList(pane);
  if (await showCustomConfirm('Print the advance receipt voucher for the customer?', 'Advance Receipt')) printAdvanceReceipt(a);
}

async function refundAdvance(pane, a) {
  if (!a) return;
  const amountStr = await showCustomPrompt(`Refund how much of ${a.number}? It goes back by ${a.payment_mode}, the way it was paid.`, money2(a.balance), 'Refund Advance');
  if (amountStr === null) return;
  const reason = await showCustomPrompt('Why is the advance refunded?', '', 'Refund Advance');
  if (reason === null) return;
  const amount = parseFloat(amountStr) || 0;
  const res = await apiFetch('/api/v1/pos/advances/refund', { method: 'POST', body: JSON.stringify({
    number: a.number, amount, reason: reason.trim(), location: posLocation, idempotency_key: `advr-${a.number}-${Date.now()}` }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The advance was not refunded.'); return; }
  const out = await res.json();
  showToast(`${out.voucher}: ${money2(amount || a.balance)} refunded by ${a.payment_mode}.`, { variant: 'success', title: 'Advance Refunded' });
  loadAdvanceList(pane);
  printAdvanceRefund(out.advance || a, out.voucher, amount || a.balance, reason.trim());
}

// --- Stage 85.E2/85.E3: the store's tenders and salesmen on the till ---------

let storeTenders = [];
let refCache = { key: '', ref: '' };
let salesmanMode = 'Off';

// posLoadTillOptions fills Payment Mode with the store's own tenders (the
// value stays the kind the ledger posts by; the text and data-tender are the
// tender's name) and shows the salesman picker when the store asks for one.
export async function posLoadTillOptions(store) {
  const [tRes, sRes] = await Promise.all([
    apiFetch(`/api/v1/pos/tenders?store=${encodeURIComponent(store || '')}`).catch(() => null),
    apiFetch(`/api/v1/pos/salesmen?store=${encodeURIComponent(store || '')}`).catch(() => null),
  ]);
  if (tRes && tRes.ok) storeTenders = (await tRes.json().catch(() => [])) || [];
  const sel = document.getElementById('pos-payment-mode');
  if (sel && storeTenders.length) {
    const was = sel.selectedOptions[0] ? (sel.selectedOptions[0].dataset.tender || sel.value) : '';
    sel.innerHTML = storeTenders.map(t => `<option value="${esc(t.kind)}" data-tender="${esc(t.name)}" data-ref="${t.needs_reference ? '1' : ''}">${esc(t.name)}</option>`).join('');
    const keep = [...sel.options].find(o => o.dataset.tender === was);
    if (keep) keep.selected = true;
    sel.dispatchEvent(new Event('change', { bubbles: true }));
  }
  const group = document.getElementById('pos-salesman-group');
  const pick = document.getElementById('pos-salesman');
  if (sRes && sRes.ok && group && pick) {
    const data = await sRes.json().catch(() => ({}));
    salesmanMode = data.mode || 'Off';
    const list = data.salesmen || [];
    pick.innerHTML = (salesmanMode === 'Mandatory' ? '' : '<option value="">-</option>') + list.map(s => `<option value="${esc(s.id)}" ${s.is_default ? 'selected' : ''}>${esc(s.name)}</option>`).join('');
    group.classList.toggle('hidden', salesmanMode === 'Off');
    const label = group.querySelector('label');
    if (label) label.innerHTML = `Salesman${salesmanMode === 'Mandatory' ? '<span class="required">*</span>' : ''}`;
  }
}

export function posStoreTenders() { return storeTenders; }

// posTillPayment is what Complete Sale sends about the money and the
// salesman, after asking for a reference when the tender takes one. null =
// the cashier cancelled, or a salesman is needed.
export async function posTillPayment() {
  const sel = document.getElementById('pos-payment-mode');
  const opt = sel && sel.selectedOptions[0];
  const out = { payment_tender: opt ? (opt.dataset.tender || opt.value) : '', payment_reference: '', salesman_id: (document.getElementById('pos-salesman') || {}).value || '' };
  if (salesmanMode === 'Mandatory' && !out.salesman_id) {
    await showCustomAlert('This store records the salesman on every bill - pick one.', 'Salesman');
    document.getElementById('pos-salesman')?.focus();
    return null;
  }
  if (opt && opt.dataset.ref) {
    // Asked once per bill and tender - a re-try (a discount reason, a changed
    // price) does not ask again.
    const key = `${typeof posCurrentCartNumber === 'function' ? posCurrentCartNumber() : ''}|${out.payment_tender}`;
    if (refCache.key !== key) {
      const ref = await showCustomPrompt(`${out.payment_tender}: the card slip, approval code or UPI reference (UTR).`, '', 'Payment Reference');
      if (ref === null) return null;
      refCache = { key, ref: ref.trim() };
    }
    out.payment_reference = refCache.ref;
  }
  return out;
}
