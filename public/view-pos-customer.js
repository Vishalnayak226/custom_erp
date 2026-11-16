// Stage 85.C (user, 2026-10-18): "Customer phone number is not asked in bill
// ... Phone number is unque. Ask Always. Where address and all i ask asked.
// DOB etc ... PAN gst."
//
// The till's customer, phone first:
//   * the box takes a mobile number (a name still searches); Enter looks the
//     number up - one number is one customer (engines/pos_customers.go);
//   * a number on file attaches that customer; a new one opens the customer
//     form with the number filled in, every detail asked in sections
//     (131.1: contact, address, tax, personal, preferences, offers) - a
//     GSTIN makes it a GST (B2B) bill;
//   * Complete Sale asks for the number when none was taken; Walk-in is the
//     answer when the customer will not give one (the store's default
//     customer, whose placeholder number is never printed);
//   * a bill over the country's limits asks for the address / PAN first, the
//     same rules the server enforces at checkout (POSOFF-0866/0867/0868);
//   * ask 215: Mobile and Name are separate boxes; a GSTIN in the GSTIN box
//     makes a B2B bill with the buyer's panel (POSOFF-0879/0880/0881); the
//     cash limit counts the same customer's cash earlier today.
// Lazy-loaded by view-pos.js, so none of this is in the till's first load.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const pickers = () => loadViewModule('/view-pickers.js?v=1');

let attached = null; // the CustomerRef on this bill, or null
let policy = null;
let b2b = null; // ask 215.2: the bill's B2B buyer block, or null for a B2C bill

const hiddenEl = () => document.getElementById('pos-customer');
const displayEl = () => document.getElementById('pos-customer-display');
const phoneEl = () => document.getElementById('pos-customer-phone'); // 215.1
const gstinEl = () => document.getElementById('pos-gstin'); // 215.2
const digits = (s) => String(s || '').replace(/[^\d+]/g, '');

// posAttachCustomer puts a customer on the bill the way a pick does: the
// code in the hidden box, the name and mobile shown, then the change event
// the till listens for (quote, offers, loyalty).
export function posAttachCustomer(c) {
  attached = c && c.id ? c : null;
  const hidden = hiddenEl(), display = displayEl(), phone = phoneEl();
  if (display) { display.value = attached ? (attached.walk_in ? 'Walk-in' : attached.name) : ''; display.dataset.resolved = attached ? '1' : ''; }
  if (phone && attached) { phone.value = attached.walk_in ? '' : (attached.phone || phone.value); phone.dataset.resolved = '1'; }
  syncB2BWithCustomer(); // before the change event, so the quote it starts carries the block
  if (hidden) { hidden.value = attached ? attached.id : ''; hidden.dispatchEvent(new Event('change', { bubbles: true })); }
}

// posSyncCustomer runs on every change of the bill's customer, including a
// pick from the Name box's search list, which sets only the hidden code: it
// loads that customer, shows their mobile and fills the GSTIN box (215.1/215.2).
export async function posSyncCustomer() {
  const code = (hiddenEl() || {}).value || '';
  if (!code) { attached = null; syncB2BWithCustomer(); return; }
  if (attached && attached.id === code) { syncB2BWithCustomer(); return; }
  const c = await refreshAttached();
  if (!c || c.id !== code) return;
  const phone = phoneEl();
  if (phone) { phone.value = c.walk_in ? '' : (c.phone || ''); phone.dataset.resolved = '1'; }
  syncB2BWithCustomer();
  if (typeof renderPOSCustomerNote === 'function') renderPOSCustomerNote();
}

// posCustomerSummary is the one line under the box (85.C5): who, their
// number, and B2B when the bill will be a GST invoice.
export function posCustomerSummary(code, shown) {
  if (!code) return 'No customer attached.';
  const c = attached && attached.id === code ? attached : null;
  if (c && c.walk_in) return 'Walk-in - no number taken. No points, not on the bill.';
  const parts = [c ? c.name : (shown || code)];
  if (c && c.phone) parts.push(c.phone);
  if (c && c.gstin && !b2b) parts.push(`GST bill (${c.gstin})`);
  return parts.join(' · ');
}

async function loadPolicy() {
  if (!policy && !navigator.onLine) return {}; // 76.10: offline, the server checks it when the sale syncs
  if (!policy) {
    const res = await apiFetch('/api/v1/pos/high-value-policy').catch(() => null);
    policy = res && res.ok ? await res.json() : {};
  }
  return policy;
}

async function refreshAttached() {
  const code = (hiddenEl() || {}).value || '';
  if (!code) { attached = null; return null; }
  if ((attached && attached.id === code && attached.fresh) || !navigator.onLine) return attached;
  const res = await apiFetch(`/api/v1/doc/Customer/${encodeURIComponent(code)}`).catch(() => null);
  if (!res || !res.ok) return attached;
  const d = await res.json();
  attached = {
    id: code, name: d.name || code, phone: d.walk_in === 'Yes' ? '' : (d.phone || ''), email: d.email || '', gstin: d.gstin || '', pan: d.pan || '',
    address: d.address || '', address_line2: d.address_line2 || '', city: d.city || '', state: d.state || '', pincode: d.pincode || '',
    walk_in: d.walk_in === 'Yes', fresh: true,
  };
  attached.address_complete = !!(attached.address && attached.state && (attached.city || attached.pincode));
  return attached;
}

// posCustomerFind runs on Enter in the Mobile box (215.1), or a number typed
// in the Name box: the number is looked up.
export async function posCustomerFind(box = phoneEl() || displayEl()) {
  const raw = box ? box.value.trim() : '';
  if (!raw || box.dataset.resolved === '1') return;
  const num = digits(raw);
  if (num.replace('+', '').length < 8) {
    if (box === phoneEl()) showToast('Enter the full mobile number.', { variant: 'warning' });
    return; // in the Name box, a name: the search list handles it
  }
  const res = await apiFetch(`/api/v1/pos/customer-lookup?phone=${encodeURIComponent(num)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'That number could not be looked up.'); return; }
  const data = await res.json();
  if (data.found) {
    posAttachCustomer(data.customer);
    showToast(`${data.customer.name} is on this bill.`, { variant: 'success' });
    return;
  }
  // 215.1: a name already typed in the Name box goes into the new customer.
  const nameBox = displayEl();
  const typedName = nameBox && box !== nameBox && nameBox.dataset.resolved !== '1' ? nameBox.value.trim() : '';
  await openPOSCustomerForm({ phone: data.phone || num, title: 'New customer', name: typedName });
}

export async function posAttachWalkIn() {
  // 76.10: with no connection the bill goes on with no customer; the server
  // bills the store's walk-in customer when the queued sale syncs (85.C3).
  if (!navigator.onLine) { showToast('Offline · Walk-in on sync'); return true; }
  const res = await apiFetch('/api/v1/pos/walk-in-customer', { method: 'POST', body: JSON.stringify({ store: posLocation }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'The walk-in customer could not be used.'); return false; }
  posAttachCustomer(await res.json());
  return true;
}

// openPOSCustomerForm: new (phone given) or edit (id given). need names what
// the bill is waiting for (the address or PAN). Stage 131.1: the form is the
// shared customer form (view-pickers.js openCustomerForm) - every detail
// asked, the mobile required. Resolves true when saved (and attached).
let lastSaved = null; // the customer the form saved last ({id, name, phone})

// name: what was typed in the customer box (85.C6) - the mobile is still asked.
export async function openPOSCustomerForm({ id = '', phone = '', name = '', title = '', need = '', attach = true } = {}) {
  const p = await pickers();
  const fresh = !id || !!(attached && attached.id === id && attached.walk_in); // the walk-in becomes a new customer
  // 215.2: the B2B buyer's GSTIN and address fill what the record leaves blank.
  const seed = attach && b2b ? { gstin: b2b.gstin, address: b2b.address, city: b2b.city, pincode: b2b.pincode, state: b2b.state, email: b2b.email } : {};
  const r = await p.openCustomerForm({ id, phone, name: name || (fresh && b2b ? b2b.legal_name : ''), title, need, seed, till: true, saveLabel: fresh && attach ? 'Add to bill' : '' });
  if (!r) return false;
  const code = r.doc.id || r.doc.code || '';
  lastSaved = { id: code, name: r.doc.name, phone: r.doc.phone };
  // attach = false: the Advance tab's customer, not the sale's.
  if (attach) {
    attached = null;
    posAttachCustomer({ id: code, name: r.doc.name });
    await refreshAttached();
    syncB2BWithCustomer(); // a GSTIN just saved on the record makes the bill B2B
    if (typeof renderPOSCustomerNote === 'function') renderPOSCustomerNote();
  }
  showToast(`${r.doc.name} ${r.editing ? 'saved' : attach ? 'added to this bill' : 'added'}.`, { variant: 'success' });
  return true;
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
      <div class="modal-container s-c31abe" role="dialog" aria-modal="true" aria-labelledby="pos-ask-title">
        <div class="modal-header"><h3 class="modal-title" id="pos-ask-title">Customer's mobile number</h3></div>
        <div class="modal-body s-48c2b9">
          <input id="pos-ask-phone" class="form-input" type="tel" inputmode="tel" placeholder="Mobile number" autocomplete="off">
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
  const phone = phoneEl() || displayEl();
  if (phone) { phone.value = picked.phone; phone.dataset.resolved = ''; }
  await posCustomerFind(phone);
  return !!((hiddenEl() || {}).value);
}

// posEnsureCustomer runs first in Complete Sale. Resolves { form97 } to go
// on, or null to stop (the cashier cancelled, or details are still missing).
export async function posEnsureCustomer({ total = 0, cash = 0, cart = '' } = {}) {
  // 215.2: a GSTIN typed without Enter is taken in now; a B2B bill goes on
  // only with the buyer's details complete.
  const gBox = gstinEl();
  if (gBox && gBox.value.trim() && !(b2b && b2b.gstin === gBox.value.replace(/\s+/g, '').toUpperCase())) {
    await posGSTINEntered();
    if (!b2b) return null;
  }
  if (b2b) {
    await loadPolicy();
    if (b2bMissing(b2b).length) await openPOSB2BPanel();
    if (!b2b) return posEnsureCustomer({ total, cash, cart }); // Not B2B was chosen
    if (b2bMissing(b2b).length) return null;
  }
  if (!((hiddenEl() || {}).value)) {
    if (!(await posAskMobile())) return null;
    // The customer changes the quote (their price list, offers, points): the
    // sale goes on with the fresh one, not a stale quote the server would query.
    if (navigator.onLine && typeof window.refreshPOSQuote === 'function') await window.refreshPOSQuote();
  }
  const p = await loadPolicy();
  const c = await refreshAttached();
  // 215.4 (Income Tax Act s.269ST): the limit is per person per day, so the
  // cash this customer paid on earlier bills today counts with this one.
  let cashToday = 0;
  if (p.cash_limit > 0 && cash > 0 && c && !c.walk_in && navigator.onLine) {
    const res = await apiFetch(`/api/v1/pos/high-value-policy?customer_id=${encodeURIComponent(c.id)}&cart_number=${encodeURIComponent(cart)}`).catch(() => null);
    if (res && res.ok) cashToday = Number((await res.json()).cash_today || 0);
  }
  if (p.cash_limit > 0 && cash > 0 && cash + cashToday >= p.cash_limit) {
    await showCustomAlert(cashToday > 0
      ? `${c.name} paid ${cashToday.toFixed(2)} in cash today. With ${cash.toFixed(2)} more that is over the limit (${p.cash_limit.toFixed(2)}). Take the rest by card or UPI (More... -> Split payment).`
      : `Cash of ${cash.toFixed(2)} on one bill is over the limit (${p.cash_limit.toFixed(2)}). Take part of it by card or UPI (More... -> Split payment).`, 'Cash Limit');
    return null;
  }
  // 215.3: a B2B bill names the buyer by GSTIN, legal name and address (CGST
  // Rule 46), and the GSTIN carries the PAN - no Form 97, no extra address.
  if (b2b) return { form97: false };
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
    return posEnsureCustomer({ total, cash, cart });
  }
  if (needPAN) {
    const choice = await new Promise(resolve => {
      showCustomConfirm(`A bill above ${p.pan_above.toFixed(2)} needs ${c.name}'s PAN.\n\nOK - add the PAN now.\nCancel - they have no PAN and have signed Form 97.`, 'PAN Needed').then(resolve);
    });
    if (choice) {
      if (!(await openPOSCustomerForm({ id: c.id, need: `Add ${c.name}'s PAN for this bill.` }))) return null;
      return posEnsureCustomer({ total, cash, cart });
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

export function posClearCustomer() {
  attached = null; openAdvances = []; useAdvance = false; b2b = null;
  const b = document.getElementById('pos-advance-use-btn'); if (b) b.classList.add('hidden');
  const g = gstinEl(); if (g) g.value = '';
  renderB2BNote();
}

// posCustomerIsB2B: the bill has a buyer GSTIN (the B2B block, or the
// customer's own), so the sale gets a GST tax invoice (Stage 85.D2) beside
// the receipt.
export function posCustomerIsB2B() { return !!(b2b && b2b.gstin) || !!(attached && attached.gstin && !attached.walk_in); }

// --- Ask 215.2/215.3: the B2B bill -------------------------------------------
//
// A GSTIN in the till's GSTIN box makes the bill a B2B tax invoice. The
// buyer's panel asks what CGST Rule 46 and a buyer's accounts team need: the
// legal name, address, city and PIN (the state comes from the GSTIN), the
// buyer's PO number and date (optional with the setting pos.b2b.po_optional),
// and how the goods leave - collected here, or shipped, which moves the place
// of supply to the buyer's state (IGST Act s.10(1)(b)). A GSTIN on file fills
// the panel and attaches that customer. The block goes with the quote and the
// sale; the server checks it again (engines/pos_b2b.go).

const PIN = /^[1-9][0-9]{5}$/;
const regions = () => (window.FIELD_META && window.FIELD_META.regions) || [];
const stateOfGSTIN = (g) => (regions().find(r => r.tax_code === String(g).slice(0, 2)) || {}).name || '';

function localToday() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function seedFromCustomer(c) {
  return {
    gstin: String(c.gstin || '').toUpperCase(), legal_name: c.name || '',
    address: [c.address, c.address_line2].filter(Boolean).join(', '), city: c.city || '', pincode: c.pincode || '',
    email: c.email || '', contact: c.phone || '', delivery: 'collect', fromCustomer: c.id,
  };
}

// b2bMissing lists what the server would refuse (POSOFF-0879/0880/0881);
// MISS_FIELD is the panel field each one points at.
const MISS_FIELD = {
  'legal name': 'legal_name', address: 'address', city: 'city', '6-digit PIN': 'pincode', 'PO number': 'po_number', 'PO date': 'po_date',
  'ship-to address': 'ship_address', 'ship-to city': 'ship_city', 'ship-to PIN': 'ship_pincode', 'ship-to state': 'ship_state',
};
function b2bMissing(b) {
  const miss = [];
  if (!b.legal_name) miss.push('legal name');
  if (!b.address) miss.push('address');
  if (!b.city) miss.push('city');
  if (!PIN.test(b.pincode || '')) miss.push('6-digit PIN');
  if (!(policy && policy.b2b_po_optional)) {
    if (!b.po_number) miss.push('PO number');
    if (!b.po_date) miss.push('PO date');
  }
  if (b.delivery === 'ship') {
    if (!b.ship_address) miss.push('ship-to address');
    if (!b.ship_city) miss.push('ship-to city');
    if (!PIN.test(b.ship_pincode || '')) miss.push('ship-to PIN');
    if (!b.ship_state) miss.push('ship-to state');
  }
  return miss;
}

// posB2BBlock is what the quote and the sale send, or null for B2C.
export function posB2BBlock() {
  if (!b2b || !b2b.gstin) return null;
  const { fromCustomer, ...out } = b2b; // eslint-disable-line no-unused-vars
  return out;
}

function renderB2BNote() {
  const note = document.getElementById('pos-gstin-note');
  if (!note) return;
  note.classList.toggle('hidden', !b2b);
  if (!b2b) { note.textContent = ''; return; }
  note.textContent = b2bMissing(b2b).length ? 'B2B · details needed'
    : ['B2B', b2b.po_number, b2b.delivery === 'ship' ? 'shipped' : ''].filter(Boolean).join(' · ');
}

function afterB2BChange() {
  renderB2BNote();
  if (typeof renderPOSCustomerNote === 'function') renderPOSCustomerNote();
  if (navigator.onLine && typeof window.refreshPOSQuote === 'function') window.refreshPOSQuote();
}

// syncB2BWithCustomer keeps the GSTIN box with the bill's customer: a
// customer with a GSTIN on file fills it (their bill is B2B, as the tax
// invoice and GSTR-1 treat it); moving off the customer a block came from
// clears it.
function syncB2BWithCustomer() {
  const box = gstinEl();
  const c = attached && !attached.walk_in ? attached : null;
  if (b2b && b2b.fromCustomer && (!c || c.id !== b2b.fromCustomer)) {
    b2b = null;
    if (box) box.value = '';
  }
  const g = c && c.gstin ? String(c.gstin).toUpperCase() : '';
  if (!b2b && g && box && (!box.value.trim() || box.value.trim().toUpperCase() === g)) {
    box.value = g;
    b2b = seedFromCustomer(c);
  }
  renderB2BNote();
  if (b2b && !policy) loadPolicy().then(renderB2BNote).catch(() => {}); // whether the PO is asked
}

// posGSTINEntered runs on Enter or change in the GSTIN box. Enter and the
// change that follows it share one run.
let entering = null;
export function posGSTINEntered() {
  if (!entering) entering = gstinEntered().finally(() => { entering = null; });
  return entering;
}

async function gstinEntered() {
  const box = gstinEl();
  if (!box) return;
  const v = box.value.replace(/\s+/g, '').toUpperCase();
  box.value = v;
  const c = attached && !attached.walk_in ? attached : null;
  if (!v) {
    if (c && c.gstin) { // the record's GSTIN decides: its bill stays B2B
      box.value = String(c.gstin).toUpperCase();
      syncB2BWithCustomer();
      showToast('GSTIN on file · B2B bill', { variant: 'warning' });
      return;
    }
    if (b2b) { b2b = null; afterB2BChange(); }
    return;
  }
  if (b2b && b2b.gstin === v) {
    if (b2bMissing(b2b).length) await openPOSB2BPanel();
    return;
  }
  const p = await pickers();
  if (v.length !== 15 || !p.fieldChecksumOK('gstin', v) || (regions().length && !stateOfGSTIN(v))) {
    if (b2b) { b2b = null; afterB2BChange(); }
    showToast('Not a valid GSTIN.', { variant: 'warning' });
    box.focus();
    return;
  }
  await loadPolicy();
  let block = null;
  if (c && c.gstin && String(c.gstin).toUpperCase() === v) block = seedFromCustomer(c);
  else if (navigator.onLine) {
    const res = await apiFetch(`/api/v1/pos/customer-lookup?gstin=${encodeURIComponent(v)}`).catch(() => null);
    const data = res && res.ok ? await res.json() : null;
    if (data && data.found && data.customer && data.customer.id) {
      block = seedFromCustomer(data.customer);
      if (c) block.fromCustomer = ''; // a mobile already taken stays on the bill
      else {
        b2b = block;
        posAttachCustomer(data.customer);
        showToast(`${data.customer.name} is on this bill.`, { variant: 'success' });
      }
    }
  }
  // A new GSTIN: the customer on the bill (no GSTIN yet) fills what it can.
  b2b = block || (c ? { ...seedFromCustomer(c), gstin: v, fromCustomer: '' } : { gstin: v, delivery: 'collect' });
  afterB2BChange();
  await openPOSB2BPanel();
}

// openPOSB2BPanel: the buyer's details. need is the server's refusal, shown on
// top. Resolves true when saved.
let panelOpen = null;
export function openPOSB2BPanel(need = '') {
  if (!panelOpen) panelOpen = b2bPanel(need).finally(() => { panelOpen = null; });
  return panelOpen;
}

async function b2bPanel(need) {
  if (!b2b) {
    showToast("Enter the buyer's GSTIN.", { variant: 'warning' });
    gstinEl()?.focus();
    return false;
  }
  const p = await pickers();
  const pol = await loadPolicy();
  const b = b2b;
  const poReq = !pol.b2b_po_optional;
  const ship = b.delivery === 'ship';
  const c = attached && !attached.walk_in ? attached : null;
  const onFile = !!(c && c.gstin && String(c.gstin).toUpperCase() === b.gstin); // Not B2B cannot apply
  const offerSave = !c || !c.gstin;
  const star = '<span class="required">*</span>';
  const field = (name, label, value, { req = false, type = 'text', extra = '' } = {}) =>
    `<div class="form-group"><label class="form-label" for="b2b-${name}">${label}${req ? star : ''}</label>
      <input type="${type}" id="b2b-${name}" name="${name}" class="form-input" value="${esc(value)}" autocomplete="off" ${extra}></div>`;
  return new Promise(resolve => {
    const returnFocus = document.activeElement;
    document.getElementById('pos-b2b-dialog')?.remove();
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open quick-create-overlay';
    overlay.id = 'pos-b2b-dialog';
    overlay.innerHTML = `
      <div class="modal-container customer-form" role="dialog" aria-modal="true" aria-labelledby="b2b-title">
        <div class="modal-header"><h3 class="modal-title" id="b2b-title">B2B bill · ${esc(b.gstin)}</h3>
          <button type="button" class="modal-close" aria-label="Close">&times;</button></div>
        <form novalidate>
          <div class="modal-body">
            ${need ? `<div class="login-error">${esc(need)}</div>` : ''}
            <div class="login-error hidden" role="alert" data-b2b-error></div>
            <details class="form-section" open><summary>Buyer${stateOfGSTIN(b.gstin) ? ` · ${esc(stateOfGSTIN(b.gstin))}` : ''}</summary><div class="form-section-grid">
              ${field('legal_name', 'Legal name', b.legal_name, { req: true, extra: 'maxlength="100"' })}
              ${field('trade_name', 'Trade name', b.trade_name, { extra: 'maxlength="100"' })}
              ${field('address', 'Address', b.address, { req: true, extra: 'maxlength="200"' })}
              ${field('city', 'City', b.city, { req: true })}
              ${field('pincode', 'PIN', b.pincode, { req: true, extra: 'inputmode="numeric" maxlength="6"' })}
              ${field('email', 'Email', b.email, { type: 'email' })}
              ${field('contact', 'Contact', b.contact, { type: 'tel', extra: 'inputmode="tel"' })}
            </div></details>
            <details class="form-section" open><summary>Order</summary><div class="form-section-grid">
              ${field('po_number', 'PO number', b.po_number, { req: poReq, extra: 'maxlength="40"' })}
              ${field('po_date', 'PO date', b.po_date, { req: poReq, type: 'date', extra: `max="${localToday()}"` })}
              <div class="form-group"><label class="form-label" for="b2b-delivery">Delivery</label>
                <select id="b2b-delivery" name="delivery" class="form-select">
                  <option value="collect">Collected here</option><option value="ship" ${ship ? 'selected' : ''}>Shipped</option></select></div>
            </div></details>
            <details class="form-section pos-b2b-ship ${ship ? '' : 'hidden'}" open><summary>Ship to</summary><div class="form-section-grid">
              ${field('ship_name', 'Name', b.ship_name, { extra: 'maxlength="100"' })}
              ${field('ship_address', 'Address', b.ship_address, { req: true, extra: 'maxlength="200"' })}
              ${field('ship_city', 'City', b.ship_city, { req: true })}
              ${field('ship_pincode', 'PIN', b.ship_pincode, { req: true, extra: 'inputmode="numeric" maxlength="6"' })}
              <div class="form-group"><label class="form-label" for="b2b-ship_state">State${star}</label><span data-b2b-ship-state></span></div>
            </div></details>
            ${offerSave ? '<div class="cf-consent"><label><input type="checkbox" name="save_customer" checked> Save to customer</label></div>' : ''}
          </div>
          <div class="modal-footer"><button type="button" class="btn btn-secondary" data-b2b-cancel>Cancel</button>
            ${onFile ? '' : '<button type="button" class="btn btn-outline" data-b2b-remove>Not B2B</button>'}
            <button type="submit" class="btn btn-primary">Save</button></div>
        </form>
      </div>`;
    const form = overlay.querySelector('form');
    const f = form.elements;
    const errorEl = overlay.querySelector('[data-b2b-error]');
    const slot = overlay.querySelector('[data-b2b-ship-state]');
    slot.replaceWith(regions().length
      ? p.buildStateSelect({ id: 'b2b-ship_state', name: 'ship_state', value: b.ship_state || '', required: true })
      : Object.assign(document.createElement('input'), { id: 'b2b-ship_state', name: 'ship_state', className: 'form-input', value: b.ship_state || '' }));
    const shipBox = overlay.querySelector('.pos-b2b-ship');
    f.delivery.addEventListener('change', () => shipBox.classList.toggle('hidden', f.delivery.value !== 'ship'));
    const done = (v) => {
      overlay.remove();
      if (returnFocus && typeof returnFocus.focus === 'function' && document.contains(returnFocus)) returnFocus.focus();
      resolve(v);
    };
    overlay.querySelector('.modal-close').addEventListener('click', () => done(false));
    overlay.querySelector('[data-b2b-cancel]').addEventListener('click', () => done(false));
    overlay.addEventListener('keydown', e => { if (e.key === 'Escape') done(false); });
    overlay.querySelector('[data-b2b-remove]')?.addEventListener('click', () => {
      b2b = null;
      const g = gstinEl(); if (g) g.value = '';
      afterB2BChange();
      done(false);
    });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const next = { ...b, delivery: f.delivery.value === 'ship' ? 'ship' : 'collect' };
      ['legal_name', 'trade_name', 'address', 'city', 'pincode', 'email', 'contact', 'po_number', 'po_date',
        'ship_name', 'ship_address', 'ship_city', 'ship_pincode', 'ship_state'].forEach(k => { next[k] = String(f[k] ? f[k].value : '').trim(); });
      const miss = b2bMissing(next);
      if (miss.length || (next.po_date && next.po_date > localToday())) {
        errorEl.textContent = miss.length ? `Add the ${miss.join(', ')}.` : 'The PO date is after today.';
        errorEl.classList.remove('hidden');
        const first = f[MISS_FIELD[miss[0]]] || f.po_date;
        if (first) first.focus();
        return;
      }
      b2b = next;
      const save = !!(f.save_customer && f.save_customer.checked);
      overlay.remove();
      afterB2BChange();
      // Saved on the customer, the GSTIN fills this panel next time. The
      // panel resolves after, so Complete Sale does not open over the form.
      if (save) await openPOSCustomerForm({ id: c ? c.id : '', phone: c ? '' : next.contact, title: c ? '' : 'New customer' });
      done(true);
    });
    document.body.appendChild(overlay);
    (b.legal_name ? (poReq && !b.po_number ? f.po_number : f.legal_name) : f.legal_name).focus();
  });
}

// posRuleHint is the short warning in the total bar (215.4): what Complete
// Sale will ask for, before the cashier gets there.
export function posRuleHint(total = 0, cash = 0) {
  if (!policy) { loadPolicy().catch(() => {}); return ''; }
  const p = policy, c = attached && !attached.walk_in ? attached : null;
  const out = [];
  if (p.cash_limit > 0 && cash >= p.cash_limit) out.push('Cash over limit');
  if (b2b) {
    if (b2bMissing(b2b).length) out.push('B2B details needed');
  } else {
    if (p.address_from > 0 && total >= p.address_from && (p.address_all || !(c && c.gstin)) && !(c && c.address_complete)) out.push('Address needed');
    if (p.pan_above > 0 && total > p.pan_above && !(c && c.pan)) out.push('PAN or Form 97');
  }
  return out.join(' · ');
}

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
  // Stage 130.12: with the customer's store credit, offered behind the same button.
  const res = await apiFetch(`/api/v1/pos/advances?customer_id=${encodeURIComponent(code)}&open=1&with_store_credit=1`).catch(() => null);
  if (!res || !res.ok || ((hiddenEl() || {}).value || '') !== code) return;
  openAdvances = (await res.json().catch(() => [])) || [];
  posRenderAdvanceButton();
}

function posRenderAdvanceButton() {
  const btn = document.getElementById('pos-advance-use-btn');
  if (!btn) return;
  const total = openAdvances.reduce((s, a) => s + a.balance, 0);
  btn.classList.toggle('hidden', !(total > 0));
  const credit = openAdvances.some(a => a.number === 'STORE-CREDIT');
  const what = credit && openAdvances.length > 1 ? 'advance + store credit' : (credit ? 'store credit' : 'advance');
  btn.textContent = useAdvance ? `Not using ${what}` : `Use ${what} ${money2(total)}`;
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
      <div class="s-e3ec02">${esc(window.posStoreLabel ? window.posStoreLabel() : '')}</div>
      <div class="s-2e15bb">${esc(title)}</div>
      <div>${formatDateTime(new Date())}</div></div><hr>
    ${rows.filter(r => r[1] !== '' && r[1] !== undefined && r[1] !== null).map(([k, v]) => `<div class="receipt-line"><span>${esc(k)}</span><span>${esc(v)}</span></div>`).join('')}
    <hr>${note ? `<div class="s-11a508">${esc(note)}</div>` : ''}</div>`;
}

// previewVoucher (Stage 143.4) shows the slip as it will print, with Close
// and Print; resolves true on Print. The gift-card slip uses it too.
export function previewVoucher(html) {
  return new Promise(resolve => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open';
    overlay.innerHTML = `<div class="modal-container s-c31abe" role="dialog" aria-modal="true" aria-label="Preview">
      <div class="modal-header"><h3 class="modal-title">Preview</h3><button type="button" class="modal-close" aria-label="Close dialog" data-vp="close">×</button></div>
      <div class="modal-body">
        <div class="vp-slip s-12e162">${html}</div></div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-vp="close">Close</button><button type="button" class="btn btn-primary" data-vp="print">Print</button></div></div>`;
    document.body.appendChild(overlay);
    const done = (v) => { overlay.remove(); document.removeEventListener('keydown', onKey); resolve(v); };
    const onKey = (e) => { if (e.key === 'Escape') done(false); };
    document.addEventListener('keydown', onKey);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) done(false); });
    overlay.querySelectorAll('[data-vp="close"]').forEach(b => b.addEventListener('click', () => done(false)));
    const print = overlay.querySelector('[data-vp="print"]');
    print.addEventListener('click', () => done(true));
    print.focus();
  });
}

async function printVoucher(html, ref) {
  if (!await previewVoucher(html)) return;
  const browser = () => {
    const area = document.getElementById('receipt-print-area');
    if (!area) return;
    area.innerHTML = html;
    area.classList.add('printing');
    window.print();
    setTimeout(() => area.classList.remove('printing'), 500);
  };
  // Stage 127.2: on the receipt printer like the bill, no browser dialog once set up.
  import('./trade-docs.js' + new URL(import.meta.url).search)
    .then(docs => docs.printSlip(html, ref || 'Voucher', browser))
    .catch(e => { console.debug('[POS] voucher slip unavailable, using the browser', e); browser(); });
}

export function printAdvanceReceipt(a) {
  const taxable = a.tax ? a.amount - a.tax : 0;
  printVoucher(voucherHTML('ADVANCE RECEIPT VOUCHER', [
    ['Voucher', a.number], ['Customer', a.customer_name], ['For', a.purpose], ['Expected', a.expected_date],
    ['Supply', a.supply_type], ...(a.tax ? [['Taxable value', money2(taxable)], [`GST ${a.gst_rate}%`, money2(a.tax)]] : []),
    ['Received', `${money2(a.amount)} (${a.payment_mode})`], ['Balance', money2(a.balance)],
  ], 'Advance received against a future purchase. Not a tax invoice - it is adjusted on the final bill.'), a.number);
}

function printAdvanceRefund(a, voucher, amount, reason) {
  printVoucher(voucherHTML('ADVANCE REFUND VOUCHER', [
    ['Voucher', voucher], ['Against advance', a.number], ['Customer', a.customer_name],
    ['Refunded', `${money2(amount)} (${a.payment_mode})`], ['Reason', reason], ['Balance', money2(a.balance)],
  ], 'Refund of an advance, back the way it was paid.'), voucher);
}

let advanceCustomer = null;

// renderPOSAdvancePanel fills the till's Advance tab.
export function renderPOSAdvancePanel(pane) {
  pane.innerHTML = `
    <div class="txn-head s-9c1366">
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
      <td class="s-86abda"><button type="button" class="action-btn" data-adv-print="${esc(a.number)}">Print</button>
        ${a.balance > 0 ? `<button type="button" class="action-btn action-btn-danger" data-adv-refund="${esc(a.number)}">Refund</button>` : ''}</td></tr>`).join('')
    : `<tr><td colspan="10" class="s-306094">${advanceCustomer ? 'No advances for this customer yet.' : 'No open advances at this store. Type a mobile number to see a customer\'s advances.'}</td></tr>`;
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
  const res = await apiFetch('/api/v1/pos/advances', { method: 'POST', body: JSON.stringify(body), commandKey: 'pos.advance' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The advance was not taken.'); return; }
  const a = await res.json();
  showToast(`${a.number}: ${money2(a.amount)} taken from ${a.customer_name}.`, { variant: 'success', title: 'Advance Taken' });
  pane.querySelector('#adv-amount').value = '';
  pane.querySelector('#adv-purpose').value = '';
  loadAdvanceList(pane);
  printAdvanceReceipt(a); // Stage 143.4: its preview asks Close or Print
}

async function refundAdvance(pane, a) {
  if (!a) return;
  const amountStr = await showCustomPrompt(`Refund how much of ${a.number}? It goes back by ${a.payment_mode}, the way it was paid.`, money2(a.balance), 'Refund Advance', 'number');
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
    sel.innerHTML = storeTenders.map(t => `<option value="${esc(t.kind)}" data-tender="${esc(t.name)}" data-ref="${t.needs_reference ? '1' : ''}" data-upi="${esc(t.upi_id || '')}" data-payee="${esc(t.payee || '')}">${esc(t.name)}</option>`).join('');
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

// upiPayQR is the NPCI pay link for a store's UPI ID and the exact amount
// (185.1), drawn as an SVG by sticker-engine.js's QR encoder. Any UPI app
// (BharatPe, PhonePe, Paytm, a bank's) reads it.
async function upiPayQR(upiID, payee, amount, bill) {
  const text = `upi://pay?pa=${upiID}&pn=${encodeURIComponent(payee || upiID)}&am=${amount.toFixed(2)}&cu=INR${bill ? `&tn=${encodeURIComponent('Bill ' + bill)}` : ''}`;
  const { encodeQR } = await import('./sticker-engine.js' + new URL(import.meta.url).search);
  const qr = encodeQR(text, 'M');
  if (!qr) return '';
  const n = qr.size, q = 4, cells = [];
  qr.modules.forEach((row, y) => row.forEach((on, x) => { if (on) cells.push(`M${x + q},${y + q}h1v1h-1z`); }));
  return `<div id="pos-upi-qr"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n + 2 * q} ${n + 2 * q}" width="100%" shape-rendering="crispEdges" role="img" aria-label="UPI pay QR"><rect width="100%" height="100%" fill="#fff"/><path d="${cells.join('')}" fill="#000"/></svg></div>`;
}

// posTillPayment is what Complete Sale sends about the money and the
// salesman, after asking for a reference when the tender takes one. null =
// the cashier cancelled, or a salesman is needed. amount is what this tender
// pays; a UPI tender with a UPI ID shows a pay QR for it (185.1). cart is the
// bill's number, so the next bill asks again.
export async function posTillPayment({ amount = 0, cart = '' } = {}) {
  const sel = document.getElementById('pos-payment-mode');
  const opt = sel && sel.selectedOptions[0];
  const out = { payment_tender: opt ? (opt.dataset.tender || opt.value) : '', payment_reference: '', salesman_id: (document.getElementById('pos-salesman') || {}).value || '' };
  if (salesmanMode === 'Mandatory' && !out.salesman_id) {
    await showCustomAlert('This store records the salesman on every bill - pick one.', 'Salesman');
    document.getElementById('pos-salesman')?.focus();
    return null;
  }
  const upi = opt && opt.value === 'UPI' && amount > 0 ? opt.dataset.upi : '';
  if (opt && (opt.dataset.ref || upi)) {
    // Asked once per bill and tender - a re-try (a discount reason, a changed
    // price) does not ask again. A QR is for one amount, so a new amount asks.
    const key = `${cart}|${out.payment_tender}${upi ? '|' + amount.toFixed(2) : ''}`;
    if (refCache.key !== key) {
      const qr = upi ? await upiPayQR(upi, opt.dataset.payee, amount, cart).catch(() => '') : '';
      const asked = qr
        ? showCustomPrompt(`UPI ₹${amount.toFixed(2)} · scan, then type the UTR.`, '', 'UPI Payment')
        : showCustomPrompt(`${out.payment_tender}: the card slip, approval code or UPI reference (UTR).`, '', 'Payment Reference');
      if (qr) document.getElementById('custom-dialog-extra')?.insertAdjacentHTML('afterbegin', qr);
      const ref = await asked;
      if (ref === null) return null;
      refCache = { key, ref: ref.trim() };
    }
    out.payment_reference = refCache.ref;
  }
  return out;
}
