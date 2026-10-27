'use strict';

// BLD-041 print-path pass: every visible print action on a lazily loaded
// screen, clicked in Chromium with PrintBridge (QZ Tray) unavailable, so the
// browser fallback runs. window.print() is stubbed to record each call and a
// snapshot of what was on the print sheet at that moment.
//
// Run (scratch server and scratch DB only):
//   node docs/qa/bld041-print-paths.cjs <evidence-dir> <scratch-origin> <token-file> [fixture.json]
//
// Without a fixture file it first seeds what each path needs through the real
// API, as the app's own screens do (masters B41-*, a PO -> GRN for stock, a
// posted and a draft Sales Invoice with a part receipt, OMS orders picked,
// packed, billed and booked, a Loaded WMS load, a POS sale and its return, a
// purchase return, notes, a POS advance, a transfer order and a delivery
// challan), and writes the ids to <evidence-dir>/fixture.json. Masters are
// reused; documents are new each run. The browser part then writes like a
// user would too (one POS sale, one load departure, one OMS pack/bill/book,
// print-log rows). The token file holds a scratch Super Admin bearer token;
// neither credentials nor results enter source.
const fs = require('node:fs');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

const [evidence, base, tokenFile, fixtureFile] = process.argv.slice(2);
if (!evidence || !base || !tokenFile) throw new Error('Evidence dir, scratch origin and token file are required.');
const origin = new URL(base);
if (origin.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(origin.hostname) || origin.pathname !== '/') {
  throw new Error('Only a loopback scratch-server origin is allowed.');
}
let token = fs.readFileSync(tokenFile, 'utf8').replace(/^﻿/, '').trim();
fs.mkdirSync(evidence, { recursive: true });

// Stay inside the scratch API's real per-session sliding windows
// (internal/server/middleware.go rateLimitCategory: GET /api/v1/doc/* 100/min,
// reports 60/min, everything else 60/min). A call passes straight through
// while its window has room and waits only when it would be refused.
const LIMITS = { search: 94, report: 54, default: 54 };
const windows = { search: [], report: [], default: [] };
const sleep = ms => new Promise(r => setTimeout(r, ms));
const categoryOf = (method, pathname) => (pathname.startsWith('/api/v1/doc/') && method === 'GET') ? 'search'
  : (pathname.startsWith('/api/v1/reports/') || pathname.endsWith('/finance/trial-balance')) ? 'report' : 'default';
async function gate(method, pathname) {
  const cat = categoryOf(method, pathname), w = windows[cat];
  for (;;) {
    const now = Date.now();
    while (w.length && now - w[0] > 61000) w.shift();
    if (w.length < LIMITS[cat]) { w.push(now); return; }
    await sleep(61000 - (now - w[0]) + 50);
  }
}
// Before each check: wait until every window has room for a whole check, so
// no call inside it is held near the app's own 30 s request timeout.
async function headroom(n = 25) {
  for (;;) {
    const now = Date.now();
    let wait = 0;
    for (const [cat, w] of Object.entries(windows)) {
      while (w.length && now - w[0] > 61000) w.shift();
      const over = w.length - (LIMITS[cat] - n);
      if (over > 0) wait = Math.max(wait, 61000 - (now - w[over - 1]) + 50);
    }
    if (!wait) return;
    await sleep(wait);
  }
}

async function api(p, method = 'GET', body) {
  await gate(method, p.split('?')[0]);
  const res = await fetch(origin.origin + p, {
    method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json; try { json = JSON.parse(text); } catch { json = text; }
  return { status: res.status, body: json };
}
const must = async (what, p, method, body) => {
  const r = await api(p, method, body);
  if (r.status >= 300) throw new Error(`seed: ${what}: ${r.status} ${JSON.stringify(r.body).slice(0, 300)}`);
  return r.body;
};
const enc = encodeURIComponent;
const localDay = () => { const d = new Date(); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 10); };

// The till sells at the working store, which the server carries in the token.
async function switchStore(store) {
  const body = await must('switch working store', '/api/v1/me/working-store', 'POST', { store });
  if (!body.token) throw new Error('working-store switch returned no token');
  token = body.token;
}

// ---------------------------------------------------------------------------
// Fixture: every document a print path needs, made through the real API.
// ---------------------------------------------------------------------------
const STORE = 'B41-ST', SKU = 'B41-ITEM-1', SKU2 = 'B41-ITEM-2';
async function ensure(doctype, code, body) {
  const g = await api(`/api/v1/doc/${doctype}/${enc(code)}`);
  if (g.status === 200 && g.body && g.body.id) return g.body;
  const c = await must(`create ${doctype} ${code}`, `/api/v1/doc/${doctype}`, 'POST', { code, ...body });
  return (await api(`/api/v1/doc/${doctype}/${enc(c.id || code)}`)).body;
}
async function approve(doctype, id) {
  const sub = await api('/api/v1/approval/submit', 'POST', { doctype, document_id: id });
  if (sub.status < 300 && (await api('/api/v1/approval/decide', 'POST', { doctype, document_id: id, decision: 'Approved', comment: 'BLD-041 print pass' })).status < 300) return;
  // No approval rule for this doctype (ADMINC-0032): approved directly, as the screen does.
  const fresh = (await api(`/api/v1/doc/${doctype}/${enc(id)}`)).body;
  if (!/approved/i.test(fresh.status || '')) await must(`approve ${doctype}`, `/api/v1/doc/${doctype}/${enc(id)}`, 'POST', { status: 'Approved', expected_version: fresh.version });
}

async function seed() {
  const RUN = Date.now().toString(36).toUpperCase();
  const fx = { run: RUN, store: STORE, sku: SKU, sku2: SKU2, customer: 'B41-C1', vendor: 'B41-V1' };
  const state = 'Maharashtra', stateCode = '27';
  const le = await ensure('LegalEntity', 'B41-LE', { name: 'B41 Retail Pvt Ltd', gstin: '27AAPFU0939F1ZV', status: 'Active', state, state_code: stateCode, address: '1 Test Road', city: 'Mumbai', pincode: '400001' });
  await ensure('Location', STORE, { name: 'B41 Test Store', type: 'Store', legal_entity: le.id, status: 'Active', sellable: 'Yes', state, state_code: stateCode, short_code: 'B41', address: '2 Shop Lane', city: 'Mumbai', pincode: '400002' });
  await ensure('Vendor', fx.vendor, { name: 'B41 Vendor', status: 'Active', state });
  for (const [sku, name] of [[SKU, 'B41 Tee'], [SKU2, 'B41 Polo']]) await ensure('Item', sku, { name, hsn_code: '6109', gst_rate: 5, mrp: 299, sale_price: 299, status: 'Active' });
  await ensure('Customer', fx.customer, { name: 'B41 Customer', status: 'Active', phone: '9800000041', state, state_code: stateCode, address: '3 Buyer St', city: 'Mumbai', pincode: '400003' });
  // A registered business customer (valid GSTIN): the till then offers Print GST Invoice.
  fx.b2bPhone = '9800000045'; fx.b2bGstin = '27AABCB4141R1ZF';
  await ensure('Customer', 'B41-C2', { name: 'B41 Trade Buyer', status: 'Active', phone: fx.b2bPhone, gstin: fx.b2bGstin, state, state_code: stateCode, address: '7 Trade Park', city: 'Mumbai', pincode: '400007' });
  // Pack's one action books the courier by the delivery pincode.
  await ensure('CourierServiceArea', 'B41-CSA-400', { courier: 'B41 Express', pincode_prefix: '400', priority: 1, status: 'Active' });
  await switchStore(STORE);

  // Stock, and the PO / GRN this pass prints: PO -> approve -> GRN steps -> post.
  const po = await must('purchase order', '/api/v1/doc/PurchaseOrder', 'POST', { vendor: fx.vendor, vendor_id: fx.vendor, location: STORE, target_warehouse: STORE,
    items: JSON.stringify([{ sku: SKU, qty: 10, rate: 100, mrp: 299 }, { sku: SKU2, qty: 10, rate: 100, mrp: 299 }]), gst_mode: 'Exclusive', expected_delivery_date: localDay(), interstate: false, interstate_override: false, total_amount: 0, status: 'Draft' });
  fx.po = po.id;
  await approve('PurchaseOrder', fx.po);
  const grn = await must('GRN', '/api/v1/doc/GRN', 'POST', { flow: 'steps', po_id: fx.po, po_ids: fx.po, location: STORE, vendor_invoice_no: `B41-${RUN}`, vendor_invoice_date: localDay(), vendor_invoice_amount: 2100,
    received_items: JSON.stringify([SKU, SKU2].map(sku => ({ po_id: fx.po, sku, qty: 10, accepted_qty: 10, rejected_qty: 0, damaged_qty: 0, mrp: 299 }))) });
  fx.grn = grn.id;
  const G = `/api/v1/grn-steps/${enc(fx.grn)}`;
  let view = (await api(G)).body || {};
  if (Math.abs(Number(view.invoice_unexplained) || 0) >= 1) view = await must('GRN receive', `${G}/receive`, 'POST', { vendor_invoice_amount: view.invoice_lines_total, reason: 'BLD-041: match the lines' });
  for (const step of ['verify', 'price']) view = await must(`GRN ${step}`, `${G}/${step}`, 'POST', {});
  for (const ch of view.children || []) {
    const mrp = {}; for (const l of (view.lines || []).filter(l => l.child === ch.key)) mrp[String(l.line_no)] = 299;
    view = await must('GRN post', `${G}/post/${enc(ch.key)}`, 'POST', { mrp });
  }
  for (const sku of [SKU, SKU2]) {
    const L = ((await api(`/api/v1/stock-locations/balance?store=${STORE}&sku=${sku}`)).body || {}).locations || [];
    const from = L.filter(x => !x.sellable && Number(x.qty) > 0 && !/hold|transit|damage/i.test(x.code)).sort((a, b) => b.qty - a.qty)[0];
    const to = L.find(x => x.sellable);
    if (from && to) await must('put away', '/api/v1/stock-locations/move', 'POST', { store: STORE, sku, qty: Number(from.qty), from_location: from.code, to_location: to.code, reason: 'BLD-041 put away' });
  }

  // Sales Invoices: one posted with a part receipt, one left as a draft.
  const invoice = async (qty) => {
    const taxable = 100 * qty, tax = Math.round(taxable * 5) / 100;
    return (await must('sales invoice', '/api/v1/doc/SalesInvoice', 'POST', { customer: fx.customer, customer_gstin: '', location: STORE, invoice_date: localDay(), place_of_supply: state,
      due_date: localDay(), customer_reference: `B41-REF-${RUN}`, interstate: false,
      items: JSON.stringify([{ sku: SKU, item_name: 'B41 Tee', hsn_code: '6109', qty, rate: 100, gst_rate: 5, taxable, tax, amount: taxable + tax }]),
      taxable_amount: taxable, cgst: tax / 2, sgst: tax / 2, igst: 0, total_tax: tax, total_amount: taxable + tax, status: 'Draft' })).id;
  };
  fx.si = await invoice(2);
  await must('post sales invoice', `/api/v1/finance/sales-invoice/${enc(fx.si)}/post`, 'POST', {});
  await must('settle sales invoice', `/api/v1/finance/sales-invoice/${enc(fx.si)}/settle`, 'POST', { reference: `B41-RCPT-${RUN}`, amount: 100 });
  fx.siDraft = await invoice(1);

  // OMS: manual orders through pick, pack, package, bill and courier booking.
  const order = async (tag) => (await must('OMS order', '/api/v1/orders', 'POST', { channel: 'Manual', channel_order_id: `B41-${RUN}-${tag}`, customer_name: `B41 Buyer ${tag}`, customer_phone: '9800000043',
    shipping_address: '5 Courier Road', shipping_city: 'Mumbai', shipping_state: state, shipping_pincode: '400005', payment_status: 'Confirmed', manual: true,
    lines: [{ sku: SKU2, qty: 1, unit_price: 299 }] })).order_id;
  const taskOf = async (orderId) => {
    const rows = (await api(`/api/v1/doc/FulfillmentTask?order_id=${enc(orderId)}`)).body;
    const t = (Array.isArray(rows) ? rows : []).find(x => x.order_id === orderId);
    if (!t) throw new Error(`seed: no fulfillment task for ${orderId}`);
    return t.id;
  };
  const pack = async (task) => {
    await must('pick scan', '/api/v1/wms/pick-scan', 'POST', { task_id: task, scan: SKU2 });
    await must('pack scan', '/api/v1/wms/pack-scan', 'POST', { task_id: task, scan: SKU2 });
    await must('pack complete', '/api/v1/wms/pack-complete', 'POST', { task_id: task, weight_kg: 0.4, documents_confirmed: true });
  };
  const ship = async (tag) => {
    const o = { order: await order(tag) };
    o.task = await taskOf(o.order);
    await pack(o.task);
    o.pkg = (await must('shipping package', '/api/v1/oms/shipping-packages', 'POST', { fulfillment_task_id: o.task })).package_id;
    o.bill = (await must('OMS bill', `/api/v1/oms/shipping-packages/${enc(o.pkg)}/invoice`, 'POST', {})).bill_number;
    o.booking = (await must('courier booking', '/api/v1/marketplace/logistics/book', 'POST', { order_id: o.order, fulfillment_task_id: o.task, destination_pincode: '400005', carrier: 'B41 Express' })).booking_id;
    o.awb = ((await api(`/api/v1/doc/LogisticsBooking/${enc(o.booking)}`)).body || {}).awb_number;
    return o;
  };
  fx.omsA = await ship('A'); // AWB assigned, not manifested: Ship step's Print label
  fx.omsB = await ship('B'); // goes onto the load
  fx.load = (await must('loading task', '/api/v1/wms/loading/create', 'POST', { dock_door: 'B41-DOOR-1', trailer_no: 'B41-TRL-1', manifest_id: '', expected_carton_count: 1 })).loading_task_id;
  await must('load scan', '/api/v1/wms/loading/scan', 'POST', { loading_task_id: fx.load, package_code: fx.omsB.pkg });
  await must('load complete', '/api/v1/wms/loading/complete', 'POST', { loading_task_id: fx.load });
  fx.omsE = await ship('E'); // goes onto a second load, departed before its first print
  fx.load2 = (await must('loading task 2', '/api/v1/wms/loading/create', 'POST', { dock_door: 'B41-DOOR-2', trailer_no: 'B41-TRL-2', manifest_id: '', expected_carton_count: 1 })).loading_task_id;
  await must('load 2 scan', '/api/v1/wms/loading/scan', 'POST', { loading_task_id: fx.load2, package_code: fx.omsE.pkg });
  await must('load 2 complete', '/api/v1/wms/loading/complete', 'POST', { loading_task_id: fx.load2 });
  fx.omsC = { order: await order('C') }; // pending: Pick list
  fx.omsC.task = await taskOf(fx.omsC.order);
  fx.omsD = { order: await order('D') }; // packed, not billed or booked: Pack > Finish
  fx.omsD.task = await taskOf(fx.omsD.order);
  await pack(fx.omsD.task);

  // POS: an open till for the browser's sale, an API sale and its customer return.
  const sess = (await api(`/api/v1/pos/session/current?location=${STORE}`)).body || {};
  if (!sess.open) await must('open till', '/api/v1/pos/session/open', 'POST', { location: STORE, opening_cash: 0 });
  fx.posCart = `B41-API-${RUN}`;
  const sale = await must('POS checkout', '/api/v1/checkout', 'POST', { cart_number: fx.posCart, idempotency_key: `b41-${RUN}-sale`, location: STORE, payment_mode: 'Cash', customer_id: '', items: [{ sku: SKU, qty: 1, sale_price: 0, mrp: 0, barcode: '' }], tenders: [] });
  if (sale.status !== 'completed') throw new Error(`seed: POS checkout ${JSON.stringify(sale).slice(0, 200)}`);
  fx.returnId = (await must('customer return', '/api/v1/returns', 'POST', { request_type: 'Customer Return', return_location: STORE, original_order_id: fx.posCart, idempotency_key: `b41-${RUN}-return`, items: [{ sku: SKU, qty: 1 }] })).return_request_id;
  fx.returnNo = ((await api(`/api/v1/doc/ReturnRequest/${enc(fx.returnId)}`)).body || {}).code || fx.returnId;
  // One registered, not-yet-issued gift card for POS > Gift Card > Issue.
  fx.giftCard = ((await must('register gift card', '/api/v1/stored-value/cards', 'POST', { profile: 'Standard Gift Card', generate: 1, numbers: [] })).numbers || [])[0];
  fx.adv = (await must('POS advance', '/api/v1/pos/advances', 'POST', { location: STORE, customer_id: fx.customer, amount: 500, payment_mode: 'Cash', purpose: 'B41 alteration deposit', expected_date: localDay(), supply_type: 'Goods', gst_rate: 0, idempotency_key: `b41-${RUN}-adv` })).number;

  // Purchase return, credit and debit notes, transfer order, delivery challan.
  const ctx = (await api(`/api/v1/procurement/purchase-returns/context?grn_id=${enc(fx.grn)}`)).body || {};
  const line = ((ctx.returnable || {}).lines || []).find(l => l.sku === SKU);
  fx.prt = (await must('purchase return', '/api/v1/doc/PurchaseReturn', 'POST', { grn_id: fx.grn, reason: 'B41 damaged in transit', return_items: JSON.stringify([{ sku: SKU, batch_no: line ? line.batch_no : '', stock_bucket: line ? line.stock_bucket : 'Accepted', qty: 1 }]), status: 'Draft' })).id;
  fx.cn = (await must('credit note', '/api/v1/doc/CreditNote', 'POST', { note_number: `CN-B41-${RUN}`, customer_id: fx.customer, amount: 50, reason: 'B41 goodwill credit', status: 'Draft', sales_invoice_id: fx.si })).id;
  fx.dn = (await must('debit note', '/api/v1/doc/DebitNote', 'POST', { note_number: `DN-B41-${RUN}`, vendor_id: fx.vendor, amount: 40, reason: 'B41 short supply', status: 'Draft', grn_id: fx.grn })).id;
  fx.to = (await must('transfer order', '/api/v1/doc/TransferOrder', 'POST', { transfer_number: `TO-B41-${RUN}`, from_warehouse: STORE, to_warehouse: 'HO', status: 'Draft', items: JSON.stringify([{ sku: SKU, qty: 2 }]) })).id;
  await approve('TransferOrder', fx.to);
  fx.dc = (await must('delivery challan', '/api/v1/doc/DeliveryChallan', 'POST', { code: `DC-B41-${RUN}`, purpose: 'Repair', location: STORE, party_type: 'Vendor', party_name: 'B41 Vendor', party_state: state,
    items: JSON.stringify([{ sku: SKU2, item_name: 'B41 Polo', qty: 1, rate: 100 }]), amount: 100, status: 'Draft' })).id;
  await approve('DeliveryChallan', fx.dc);
  return fx;
}

// ---------------------------------------------------------------------------
// Browser pass
// ---------------------------------------------------------------------------
async function main() {
  let fx;
  if (fixtureFile) {
    fx = JSON.parse(fs.readFileSync(fixtureFile, 'utf8'));
    if (fx.store) await switchStore(fx.store);
  } else {
    fx = await seed();
    fs.writeFileSync(path.join(evidence, 'fixture.json'), JSON.stringify(fx, null, 2) + '\n');
    process.stdout.write(`seeded run ${fx.run}: SI ${fx.si}, load ${fx.load}, booking ${fx.omsA.booking}\n`);
  }
  const me = (await api('/api/v1/me')).body;
  const { chromium } = resolvePlaywright();
  const browser = await chromium.launch({ headless: true });
  const result = { generatedAt: new Date().toISOString(), browser: browser.version(), origin: origin.origin, identity: { username: me.username, role: me.role }, fixture: fx, checks: [] };
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
  // PrintBridge is never reachable from this browser: its sockets close at
  // once, and QZPrint.connect() rejects the way a computer without the agent does.
  await context.routeWebSocket(/:(8181|8182|8282|8283)(\/|$)/, ws => ws.close());
  await context.addInitScript(({ token, me }) => {
    localStorage.setItem('erp_token', token);
    localStorage.setItem('erp_role', me.role || '');
    localStorage.setItem('erp_username', me.username || '');
    localStorage.setItem('erp_tenant_id', 'default');
    // Idempotent: a slip window written with document.write runs this again.
    if (window.__b41) return;
    window.__b41 = { prints: [], qzConnects: 0 };
    let qz;
    Object.defineProperty(window, 'QZPrint', {
      configurable: true,
      get() { return qz; },
      set(v) {
        if (v && typeof v === 'object') v.connect = () => { window.__b41.qzConnects++; return Promise.reject(new Error('PrintBridge is not running on this computer (BLD-041 harness)')); };
        qz = v;
      },
    });
    const squash = s => String(s || '').replace(/\s+/g, ' ').trim();
    window.print = function () {
      // The sheet the print stylesheet shows: #trade-print-area under
      // body.trade-printing, or a legacy area marked .printing (an older
      // area keeps its last content, hidden, so it is not the sheet).
      const filled = [...document.querySelectorAll('[id$="print-area"]')].filter(a => a.children.length || squash(a.textContent));
      const live = filled.filter(a => a.classList.contains('printing') || (a.id === 'trade-print-area' && document.body.classList.contains('trade-printing')));
      window.__b41.prints.push({
        at: Date.now(),
        bodyClass: document.body.className,
        areas: (live.length ? live : filled).map(a => ({ id: a.id, pages: a.children.length, text: squash(a.textContent).slice(0, 3000) })),
      });
    };
    // A slip printed in its own small window (window.open + document.write +
    // an inline print()) is recorded here too, with what that window showed.
    const realOpen = window.open;
    window.open = function (...args) {
      const w = realOpen.apply(this, args);
      if (w) {
        try {
          w.print = () => window.__b41.prints.push({ at: Date.now(), bodyClass: 'popup-window',
            areas: [{ id: 'popup-window', pages: 1, text: squash(w.document.body && w.document.body.textContent).slice(0, 3000) }] });
        } catch (_) { /* cross-origin window: not a slip */ }
      }
      return w;
    };
  }, { token, me });

  const page = await context.newPage();
  page.setDefaultTimeout(20000);
  const pageErrors = [], consoleErrors = [], badResponses = [];
  const pending = new Set();
  page.on('pageerror', e => pageErrors.push(e.message));
  page.on('console', m => { if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 300)); });
  page.on('response', r => { if (r.status() >= 400) badResponses.push(`${r.request().method()} ${new URL(r.url()).pathname} -> ${r.status()}`); });
  page.on('request', r => { if (new URL(r.url()).pathname.startsWith('/api/')) pending.add(r); });
  page.on('requestfinished', r => pending.delete(r));
  page.on('requestfailed', r => pending.delete(r));
  await page.route('**/api/**', async route => { await gate(route.request().method(), new URL(route.request().url()).pathname); await route.continue(); });

  const quiet = async (ms = 400, timeout = 30000) => {
    const until = Date.now() + timeout;
    let calm = Date.now();
    while (Date.now() < until) {
      if (pending.size) calm = Date.now();
      else if (Date.now() - calm >= ms) return;
      await page.waitForTimeout(50);
    }
  };
  const open = async (view) => {
    await page.evaluate(v => renderView(v), view);
    await quiet();
  };
  const prints = () => page.evaluate(() => window.__b41.prints.length);
  const waitPrints = n => page.waitForFunction(k => window.__b41.prints.length >= k, n, { timeout: 30000 });
  const argsHas = id => `[data-act-args*='"${id}"']`; // the JSON argument list holds the id, exactly

  // A check: reset counters, run the clicks, settle, then record what printed.
  async function check(name, screen, docId, run, expect) {
    await headroom();
    const marks = { pe: pageErrors.length, ce: consoleErrors.length, br: badResponses.length, pr: await prints(), qz: await page.evaluate(() => window.__b41.qzConnects) };
    const record = { name, screen, docId, ok: false, notes: [] };
    try {
      await run(record);
    } catch (e) {
      record.error = e.message.split('\n')[0];
      await page.screenshot({ path: path.join(evidence, `fail-${name}.png`) }).catch(() => {});
    }
    await page.waitForTimeout(1200); // a second print would land here
    await quiet();
    const after = await page.evaluate(() => window.__b41);
    if (typeof expect === 'function') expect = expect(record);
    const sheets = after.prints.slice(marks.pr);
    const sheetText = sheets.map(p => p.areas.map(a => a.text).join(' ')).join(' ');
    // A check that withholds a response on purpose names it (allowHttp); its
    // own refusal and the browser's console line for it are expected.
    let httpErrors = badResponses.slice(marks.br), consoleErrs = consoleErrors.slice(marks.ce);
    const expectedHttp = expect.allowHttp ? httpErrors.filter(h => expect.allowHttp.test(h)) : [];
    if (expectedHttp.length) {
      httpErrors = httpErrors.filter(h => !expect.allowHttp.test(h));
      let drop = expectedHttp.length;
      consoleErrs = consoleErrs.filter(c => !(drop > 0 && /^Failed to load resource/.test(c) && drop--));
    }
    Object.assign(record, {
      printCalls: sheets.length,
      qzConnectAttempts: after.qzConnects - marks.qz,
      printSheets: sheets.map(p => ({ bodyClass: p.bodyClass, areas: p.areas.map(a => ({ id: a.id, pages: a.pages, text: a.text.slice(0, 700) })) })),
      pageErrors: pageErrors.slice(marks.pe),
      consoleErrors: consoleErrs,
      httpErrors,
      expectedHttpErrors: expectedHttp,
      expected: { ...expect, allowHttp: expect.allowHttp && String(expect.allowHttp) },
      missingFromSheet: (expect.sheetHas || []).filter(t => !sheetText.includes(t)),
      unexpectedOnSheet: (expect.sheetLacks || []).filter(t => sheetText.includes(t)),
      missingFromScreen: (expect.screenHas || []).filter(t => !(record.screenText || '').includes(t)),
      verifyFailures: expect.verify ? expect.verify(record) : [],
    });
    record.ok = !record.error && record.printCalls === expect.printCalls && !record.missingFromSheet.length && !record.unexpectedOnSheet.length && !record.missingFromScreen.length &&
      !record.verifyFailures.length && !record.pageErrors.length && !record.consoleErrors.length && !record.httpErrors.length;
    result.checks.push(record);
    fs.writeFileSync(path.join(evidence, 'bld041-print-paths.json'), JSON.stringify(result, null, 2) + '\n');
    const extra = [record.error && `error=${record.error}`, record.missingFromSheet.length && `missing=${JSON.stringify(record.missingFromSheet)}`, record.unexpectedOnSheet.length && `unexpected=${JSON.stringify(record.unexpectedOnSheet)}`,
      record.verifyFailures.length && `verify=${JSON.stringify(record.verifyFailures)}`,
      record.missingFromScreen.length && `missingOnScreen=${JSON.stringify(record.missingFromScreen)}`, record.pageErrors.length && `pageErrors=${JSON.stringify(record.pageErrors)}`,
      record.consoleErrors.length && `consoleErrors=${JSON.stringify(record.consoleErrors)}`, record.httpErrors.length && `http=${JSON.stringify(record.httpErrors)}`].filter(Boolean).join(' ');
    process.stdout.write(`${record.ok ? 'PASS' : 'FAIL'} ${name} [${record.docId}]: prints=${record.printCalls} qz=${record.qzConnectAttempts} ${extra}\n`);
    await page.evaluate(() => document.querySelectorAll('.td-overlay, #shipping-label-modal').forEach(el => el.remove()));
  }

  // The shared print dialog (trade-docs.js): wait for its live preview, press Print.
  async function printViaDialog(record, { reason } = {}) {
    const overlay = page.locator('.td-overlay').last();
    await overlay.locator('.td-preview .td-preview-page').first().waitFor({ state: 'attached', timeout: 30000 });
    record.dialogTransaction = ((await overlay.locator('.td-txn, [data-td="txn"] option:checked').first().textContent().catch(() => '')) || '').trim();
    record.previewText = (await overlay.locator('.td-preview').innerText()).replace(/\s+/g, ' ').slice(0, 400);
    const before = await prints();
    await overlay.locator('[data-td="print"]').click();
    if (reason) {
      await page.locator('#custom-dialog-prompt-input').fill(reason);
      await page.locator('#custom-dialog-ok-btn').click();
    }
    await waitPrints(before + 1);
    await overlay.waitFor({ state: 'detached', timeout: 15000 }).catch(() => record.notes.push('print dialog stayed open'));
  }

  try {
    // The seeding just spent most of the minute's budget: start the app's own
    // ~40-call bootstrap on empty windows, never held near its 30 s timeout.
    await headroom(50);
    await page.goto(`${origin.origin}/?r=1#/view/sales-invoices`, { waitUntil: 'domcontentloaded' });
    await page.locator('#app-root:not(.hidden)').waitFor({ state: 'visible', timeout: 120000 });
    await page.waitForFunction(() => typeof renderView === 'function' && state.modules.loaded, null, { timeout: 120000 });
    await quiet(800);

    // 1. Sales Invoices: posted invoice, draft (proforma), receipt voucher, customer statement.
    await check('sales-invoice-posted', 'sales-invoices', fx.si, async (rec) => {
      await open('sales-invoices');
      await page.locator(`#view-root [data-act="printSalesInvoice"]${argsHas(fx.si)}`).click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.si, 'B41 Customer', 'Tax Invoice', '210.00'] });

    await check('sales-invoice-draft', 'sales-invoices', fx.siDraft, async (rec) => {
      await open('sales-invoices');
      await page.locator(`#view-root [data-act="printSalesInvoice"]${argsHas(fx.siDraft)}`).click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.siDraft, 'B41 Customer', 'Draft'] });

    await check('sales-invoice-receipt-voucher', 'sales-invoices', fx.si, async (rec) => {
      await open('sales-invoices');
      await page.locator(`#view-root [data-pay-hist-toggle="${fx.si}"]`).click();
      await page.locator(`#view-root [data-pay-print="ReceiptVoucher"][data-pay-id^="${fx.si}~"]`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Receipt Voucher', 'B41 Customer', `Receipt against ${fx.si}`, '100.00'] });

    await check('customer-statement', 'sales-invoices', fx.customer, async (rec) => {
      await open('sales-invoices');
      await page.getByRole('button', { name: 'Statement', exact: true }).click();
      await page.locator('#custom-dialog-prompt-input').fill(fx.customer);
      await page.locator('#custom-dialog-ok-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Statement of Account', 'B41 Customer', fx.si] });

    // 2. POS: a sale completed on the till, then Print Receipt and Print GST Invoice.
    let bill = '';
    await check('pos-receipt', 'pos', '(new sale)', async (rec) => {
      await open('pos');
      // Typed text lists matches; the cashier picks the item, as the till asks.
      await page.locator('#pos-sku-input').fill(SKU);
      await page.locator('#pos-search-results').getByText('B41 Tee').first().click();
      await page.locator('#pos-cart-body tr').filter({ hasText: 'B41 Tee' }).first().waitFor();
      await quiet();
      // A registered business customer (GSTIN) by mobile, so the bill also offers Print GST Invoice.
      await page.locator('#pos-customer-display').fill(fx.b2bPhone);
      await page.locator('#pos-customer-display').press('Enter');
      await page.locator('.toast', { hasText: 'B41 Trade Buyer' }).first().waitFor({ timeout: 15000 }).catch(() => rec.notes.push('no customer toast'));
      await quiet();
      await page.locator('#pos-checkout-btn').click();
      for (let i = 0; i < 4; i++) {
        if (await page.locator('#pos-completed-sale:not(.hidden) #pos-print-receipt-btn').isVisible().catch(() => false)) break;
        const ok = page.locator('#custom-dialog-container:not(.hidden) #custom-dialog-ok-btn');
        if (await ok.isVisible().catch(() => false)) { rec.notes.push('dialog: ' + (await page.locator('#custom-dialog-message').innerText()).slice(0, 160)); await ok.click(); }
        await page.waitForTimeout(1500);
        await quiet();
      }
      await page.locator('#pos-completed-sale #pos-print-receipt-btn').waitFor({ state: 'visible', timeout: 30000 });
      rec.screenText = (await page.locator('#pos-completed-sale').innerText()).replace(/\s+/g, ' ');
      bill = (/Bill (\S+)/.exec(rec.screenText) || [])[1] || '';
      rec.docId = bill;
      const before = await prints();
      await page.locator('#pos-print-receipt-btn').click();
      await waitPrints(before + 1);
    }, rec => ({ printCalls: 1, sheetHas: [rec.docId || '(bill)', 'B41 Retail Pvt Ltd', 'B41 Tee', '299.00', 'B41 Trade Buyer', 'Paid: Cash 299.00'] }));

    await check('pos-gst-invoice', 'pos', bill, async (rec) => {
      await page.locator('#pos-print-gst-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [bill || '(bill)', 'B41 Tee', 'B41 Trade Buyer', fx.b2bGstin] });

    // 2a. Preview beside Print Receipt: its own Print prints the bill.
    await check('pos-receipt-preview-print', 'pos', bill, async (rec) => {
      await page.locator('#pos-preview-receipt-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [bill || '(bill)', 'B41 Retail Pvt Ltd', 'B41 Tee', 'B41 Trade Buyer'] });

    // 2b. The till's older plain receipt (view-pos-extras.js printPOSReceipt):
    // what Print Receipt prints when the server cannot build the roll bill.
    // The bill's print data is withheld once (HTTP 500) to reach it.
    const billData = '**/api/v1/print/document/POSTaxInvoice/**';
    const withhold = route => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"BLD-041 harness: bill print data withheld"}' });
    await check('pos-receipt-plain-fallback', 'pos', bill, async () => {
      await page.route(billData, withhold);
      try {
        const before = await prints();
        await page.locator('#pos-print-receipt-btn').click();
        await waitPrints(before + 1);
      } finally { await page.unroute(billData, withhold); }
    }, { printCalls: 1, sheetHas: [bill || '(bill)', 'B41 Test Store', 'B41 Tee', 'Total (Cash)', '299.00'], allowHttp: /POSTaxInvoice/ });

    // 2c. POS > Gift Card > Issue: the issue slip opens in its own small
    // window and prints there (view-pos-extras.js posPrintGiftCardReceipt).
    await check('pos-gift-card-issue-slip', 'pos (Gift Card)', fx.giftCard ? `card ending ${fx.giftCard.slice(-4)}` : '(no card)', async (rec) => {
      await page.locator('#pos-new-sale-btn').click();
      await page.locator('#pos-giftcard-btn').click();
      const dlg = page.locator('#custom-dialog-container:not(.hidden)');
      await dlg.locator('[data-gc="action"]').selectOption('issue');
      await dlg.locator('[data-gc="card"]').fill(fx.giftCard);
      await dlg.locator('[data-gc="amount"]').fill('500');
      await dlg.locator('[data-gc="mode"]').selectOption('Cash');
      await page.locator('#custom-dialog-ok-btn').click();
      const popupP = page.waitForEvent('popup', { timeout: 30000 });
      await page.locator('#custom-dialog-container:not(.hidden) #custom-dialog-ok-btn').click(); // "Load 500.00 on card ending ...?"
      const popup = await popupP;
      popup.on('pageerror', e => pageErrors.push(`popup: ${e.message}`));
      await popup.waitForFunction(() => document.body && /GIFT CARD/.test(document.body.innerText), null, { timeout: 15000 });
      rec.screenText = (await popup.locator('body').innerText()).replace(/\s+/g, ' ');
      await popup.close();
    }, { printCalls: 1, sheetHas: ['GIFT CARD ISSUE', 'B41 Test Store', fx.giftCard ? fx.giftCard.slice(-4) : '(card)', 'Amount: 500.00'], screenHas: ['GIFT CARD ISSUE'] });

    // 3. WMS Loading Dock. Load 1: Bill of Lading while Loaded; after it
    // departs, the same button prints the frozen original marked REPRINT
    // (Stage 130.16 / DEC-17: a reprint is the original again). Load 2:
    // departed first, so its Bill of Lading is an original saying Departed.
    await check('bill-of-lading-loaded', 'loading-dock', fx.load, async (rec) => {
      await open('loading-dock');
      await page.locator(`#view-root [data-open-load="${fx.load}"]`).click();
      await page.locator('#load-print-bol-btn').waitFor({ state: 'visible' });
      rec.screenText = (await page.locator('#loading-body').innerText()).replace(/s+/g, ' ').slice(0, 400);
      await page.locator('#load-print-bol-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Bill of Lading', fx.load, 'Status: Loaded', 'B41-TRL-1', fx.omsB.pkg], sheetLacks: ['REPRINT'], screenHas: ['Loaded'] });

    await check('bill-of-lading-reprint-after-depart', 'loading-dock', fx.load, async (rec) => {
      await page.locator('#load-depart-btn').click();
      await quiet();
      await page.locator('#loading-body .badge', { hasText: 'Departed' }).waitFor();
      rec.screenText = (await page.locator('#loading-body').innerText()).replace(/s+/g, ' ').slice(0, 400);
      await page.locator('#load-print-bol-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Bill of Lading', fx.load, 'REPRINT', fx.omsB.pkg], screenHas: ['Departed'] });

    await check('bill-of-lading-departed', 'loading-dock', fx.load2, async (rec) => {
      await page.locator(`#view-root [data-open-load="${fx.load2}"]`).click();
      await page.locator('#load-depart-btn').click();
      await quiet();
      await page.locator('#loading-body .badge', { hasText: 'Departed' }).waitFor();
      rec.screenText = (await page.locator('#loading-body').innerText()).replace(/s+/g, ' ').slice(0, 400);
      await page.locator('#load-print-bol-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Bill of Lading', fx.load2, 'Status: Departed', 'B41-TRL-2', fx.omsE.pkg], sheetLacks: ['REPRINT'], screenHas: ['Departed'] });

    // 4. OMS Ship: Print label on an AWB-assigned booking. With PrintBridge down
    // its designed fallback is the on-screen label (view-oms.js
    // printShippingLabel), not window.print(): zero print calls is the pass.
    await check('shipping-label-ship-step', 'oms:ship', fx.omsA.booking, async (rec) => {
      await open('oms:ship');
      await page.locator(`#view-root [data-label-print="${fx.omsA.booking}"]`).click();
      await page.locator('#shipping-label-modal').waitFor({ state: 'visible' });
      rec.screenText = (await page.locator('#shipping-label-modal').innerText()).replace(/\s+/g, ' ').slice(0, 600);
    }, { printCalls: 0, screenHas: [fx.omsA.booking, fx.omsA.awb, 'B41 Express', '400005'] });

    // 5. OMS Pick: Pick list on a pending task.
    await check('oms-pick-list', 'oms:pick', fx.omsC.task, async (rec) => {
      await open('oms:pick');
      await page.locator(`#view-root [data-pick-act="print"][data-id="${fx.omsC.task}"]`).click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Pick List', SKU2] });

    // 6. OMS Pack: Finish a packed task (package, bill, booking), then Print bill and Print label.
    let packAwb = '';
    await check('oms-pack-print-bill', 'oms:pack', fx.omsD.task, async (rec) => {
      await open('oms:pack');
      await page.locator(`#view-root [data-pack-open="${fx.omsD.task}"]`).click();
      await page.locator('#view-root [data-pack-finish]').click();
      await page.locator('#view-root [data-print-bill]').waitFor({ state: 'visible', timeout: 30000 });
      rec.screenText = (await page.locator('#view-root [data-pack-result]').innerText()).replace(/\s+/g, ' ');
      packAwb = (/AWB (\S+)/.exec(rec.screenText) || [])[1] || '';
      rec.docId = (/Bill (\S+)/.exec(rec.screenText) || [])[1] || fx.omsD.task;
      await page.locator('#view-root [data-print-bill]').click();
      await printViaDialog(rec);
    }, rec => ({ printCalls: 1, sheetHas: [rec.docId, SKU2], screenHas: ['Packed', 'Courier B41 Express', 'AWB AWB'] }));

    await check('shipping-label-pack-step', 'oms:pack', packAwb, async (rec) => {
      await page.locator('#view-root [data-print-label]').click();
      await page.locator('#shipping-label-modal').waitFor({ state: 'visible' });
      rec.screenText = (await page.locator('#shipping-label-modal').innerText()).replace(/\s+/g, ' ').slice(0, 600);
    }, () => ({ printCalls: 0, screenHas: [packAwb || '(awb)', 'B41 Express'] }));

    // 7. Procurement: Purchase Order, GRN, Purchase Return.
    await check('purchase-order', 'purchase-orders', fx.po, async (rec) => {
      await open('purchase-orders');
      await page.locator(`#view-root [data-act="printPurchaseOrder"]${argsHas(fx.po)}`).click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.po, 'B41 Vendor', SKU] });

    await check('goods-receipt', 'grn', fx.grn, async (rec) => {
      await open('grn');
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.grn)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.grn, 'B41 Vendor', SKU] });

    await check('purchase-return', 'purchase-returns', fx.prt, async (rec) => {
      await open('purchase-returns');
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.prt)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.prt, SKU] });

    // 8. Customer return request.
    await check('return-request', 'returns', fx.returnId, async (rec) => {
      await open('returns');
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.returnId)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Sales Return', fx.returnNo, fx.posCart, SKU, 'B41 Tee'] });

    // 9. Reprint: the Sales Invoice and its Receipt Voucher printed above, again, with a reason.
    const re = s => new RegExp(String(s).replace(/[.*+?^${}()|[\]\\/]/g, '\\$&'));
    const reprintRow = (doc, number) => page.locator('#rp-rows tr')
      .filter({ has: page.locator('td:nth-child(2)', { hasText: doc }) })
      .filter({ has: page.locator('td:nth-child(3)', { hasText: number }) }).first();
    await check('reprint-sales-invoice', 'reprint', fx.si, async (rec) => {
      await open('reprint');
      await reprintRow('Tax Invoice', new RegExp(`^${re(fx.si).source}$`)).locator('[data-rp]').click();
      await printViaDialog(rec, { reason: 'BLD-041 reprint check' });
    }, { printCalls: 1, sheetHas: ['Tax Invoice', fx.si, 'B41 Customer', '210.00'] });

    await check('reprint-receipt-voucher', 'reprint', fx.si, async (rec) => {
      await open('reprint');
      await reprintRow('Receipt Voucher', re(fx.si)).locator('[data-rp]').click();
      await printViaDialog(rec, { reason: 'BLD-041 reprint check' });
    }, { printCalls: 1, sheetHas: ['Receipt Voucher', `Receipt against ${fx.si}`, '100.00'] });

    await check('reprint-print-this-number', 'reprint', fx.siDraft, async (rec) => {
      await open('reprint');
      await page.locator('#rp-doctype').selectOption('SalesInvoice');
      await quiet();
      await page.locator('#rp-q').fill(fx.siDraft);
      await page.locator('#rp-print-any').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.siDraft, 'B41 Customer'] });

    // 10. Finance notes: customer credit note and vendor debit note.
    await check('credit-note', 'finance-notes', fx.cn, async (rec) => {
      await open('finance-notes');
      await page.locator('#view-root .txn-tab', { hasText: 'Customer credit notes' }).click();
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.cn)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.cn, 'B41 Customer', '50.00'] });

    await check('debit-note', 'finance-notes', fx.dn, async (rec) => {
      await open('finance-notes');
      await page.locator('#view-root .txn-tab', { hasText: 'Debit notes' }).click(); // the screen reopens on the last tab used
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.dn)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.dn, 'B41 Vendor', '40.00'] });

    // 11. Vendor statement (Statement beside New on Vendor Invoices).
    await check('vendor-statement', 'vendor-invoices', fx.vendor, async (rec) => {
      await open('vendor-invoices');
      await page.getByRole('button', { name: 'Statement', exact: true }).click();
      await page.locator('#custom-dialog-prompt-input').fill(fx.vendor);
      await page.locator('#custom-dialog-ok-btn').click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['Statement of Account', 'B41 Vendor'] });

    // 12. Stock Movement: Transfer Out on an approved transfer order.
    await check('transfer-out', 'transfers', fx.to, async (rec) => {
      await open('transfers');
      await page.locator(`#view-root [data-act="printTransactionDocument"]${argsHas(fx.to)}`).first().click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: [fx.to, SKU] });

    // 13. Inventory > Delivery Challans: Print on an approved challan.
    await check('delivery-challan', 'inventory:challans', fx.dc, async (rec) => {
      await open('inventory:challans');
      await page.locator(`#view-root [data-dc-act="print"][data-id="${fx.dc}"]`).click();
      await printViaDialog(rec);
    }, { printCalls: 1, sheetHas: ['B41 Vendor', 'B41 Polo'] });

    // 14. POS > Advance: the advance receipt voucher slip (receipt-print-area).
    await check('pos-advance-voucher', 'pos (Advance tab)', fx.adv, async () => {
      await open('pos');
      await page.locator('#pos-screen .txn-tab', { hasText: 'Advance' }).click();
      await quiet();
      const before = await prints();
      await page.locator(`[data-adv-print="${fx.adv}"]`).click();
      await waitPrints(before + 1);
    }, { printCalls: 1, sheetHas: ['ADVANCE RECEIPT VOUCHER', fx.adv, 'B41 Customer', '500.00'] });

    // 15. Records (doctype-table): a saved transaction's own Print, on its form.
    await check('record-form-print', 'doctype-table (CreditNote)', fx.cn, async (rec) => {
      await page.evaluate(() => { currentDoctype = 'CreditNote'; });
      await open('doctype-table');
      await page.locator(`#view-root [data-doc-action="edit"][data-doc-id="${fx.cn}"]`).first().click();
      await page.locator('#dynamic-modal.open').getByRole('button', { name: 'Print', exact: true }).click();
      await printViaDialog(rec);
      await page.evaluate(() => window.closeDynamicModal?.());
    }, { printCalls: 1, sheetHas: [fx.cn, '50'] });
  } finally {
    const failed = result.checks.filter(c => !c.ok);
    result.summary = { checks: result.checks.length, passed: result.checks.length - failed.length, failed: failed.map(c => c.name) };
    fs.writeFileSync(path.join(evidence, 'bld041-print-paths.json'), JSON.stringify(result, null, 2) + '\n');
    process.stdout.write(`summary: ${result.summary.passed}/${result.summary.checks} passed${failed.length ? ' - failed: ' + result.summary.failed.join(', ') : ''}\n`);
    await browser.close();
    if (failed.length) process.exitCode = 1;
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
