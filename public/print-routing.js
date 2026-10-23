// Stage 98.4 (user, 2026-10-19): "It will call diff printer for barcode and
// other a4 or a5 or pos bill style ... automatically if all connected. ERP
// will be installed in 100 stores. All printer will be different ... It should
// ask one time which printer for what txn once then nver ask. But user can
// change it. & I am ok if it happen automatically with out asking user."
//
// Each computer keeps its own printer for each kind of print, on the server
// (decision D3; engines/printer_choice.go). The computer is known by the name
// QZ Tray reports, so a browser reset does not lose its choices. The first
// print of a kind with no printer picks one by itself when the choice is
// clear - A4 / A5 documents the computer's default printer, bills the one
// receipt printer, stickers and labels the one label printer - and asks once
// only when two could fit (decision D4). Change is always one click away.
//
// Loaded on demand by qzTryPrint (app.js) and the print dialog; kept out of
// the cold core.

const COMPUTER_KEY = 'erp_print_computer';

// Print transaction -> kind of print (engines.PrintKinds).
const KIND_OF = {
  PurchaseOrder: 'Purchase Order', VendorPO: 'Purchase Order',
  GRN: 'Goods Receipt', GRNVendorCopy: 'Goods Receipt',
  TransferOut: 'Transfer', TransferIn: 'Transfer',
  SalesInvoice: 'Sales Invoice', POSTaxInvoice: 'Sales Invoice', OMSBill: 'Sales Invoice', // Stage 88.3.2
  PickList: 'Pick List', // Stage 88.2.3
  DebitNote: 'Returns and Notes', VendorCreditNote: 'Returns and Notes', PurchaseReturn: 'Returns and Notes',
  CreditNote: 'Returns and Notes', SalesReturn: 'Returns and Notes',
  // QZ job types
  Receipt: 'Receipt', Sticker: 'Sticker', 'Shipping Label': 'Shipping Label', Invoice: 'Sales Invoice',
  'Purchase Order': 'Purchase Order', Document: 'Shipping Label'
};
// Stage 127.3: a transaction printed through the generic format is an Other
// Document; a value that already is a kind stays as it is.
const KINDS = new Set([...Object.values(KIND_OF), 'Other Documents']);
export const printKindFor = (txnOrJob) => KIND_OF[txnOrJob] || (KINDS.has(txnOrJob) ? txnOrJob : 'Other Documents');

const RECEIPT_RE = /(pos|thermal|receipt|\btm-?[a-z]?\d|\brp-?\d|xp-?\d|80\s?mm|58\s?mm|retsol|xprinter)/i;
const LABEL_RE = /(zebra|\bzd\d|\bgk4|\bgx4|\btsc|\btte|\btlp|godex|argox|label|barcode|datamax|honeywell|snbc)/i;
const VIRTUAL_RE = /(pdf|xps|onenote|fax|send to|microsoft print)/i;

let computerName = null;
// printComputer is this computer's name: QZ Tray's, else one remembered in
// this browser (an old tray that cannot tell its name).
export async function printComputer() {
  if (computerName) return computerName;
  let name = '';
  // Stage 127.2: only a connected tray is asked, so a computer whose tray is
  // down still knows its name (and its printers) without waiting on it.
  if (window.QZPrint?.isConnected?.()) {
    try { name = String(await window.QZPrint.hostname() || '').trim(); } catch (_) { /* old tray */ }
  }
  if (!name) { try { name = localStorage.getItem(COMPUTER_KEY) || ''; } catch (_) { /* storage blocked */ } }
  if (!name) name = 'PC-' + Math.random().toString(36).slice(2, 8).toUpperCase();
  try { localStorage.setItem(COMPUTER_KEY, name); } catch (_) { /* storage blocked */ }
  computerName = name;
  return name;
}

const storeCode = (location) => location || (window.erpWorkingStore || {}).code || '';

// chosenPrinter is where a kind would print now on this computer (its own
// choice, else the store's default), or null.
export async function chosenPrinter(kind, location) {
  const qs = new URLSearchParams({ computer: await printComputer(), kind, location: storeCode(location) });
  const res = await window.apiFetch(`/api/v1/print/qz/assignments?${qs}`);
  if (!res || !res.ok) return null;
  return (await res.json()).printer || null;
}

function autoPick(kind, printers, def) {
  const plain = (n) => !RECEIPT_RE.test(n) && !LABEL_RE.test(n) && !VIRTUAL_RE.test(n);
  const only = (list) => (list.length === 1 ? list[0] : '');
  if (kind === 'Receipt') return only(printers.filter(n => RECEIPT_RE.test(n)));
  if (kind === 'Sticker' || kind === 'Shipping Label') return only(printers.filter(n => LABEL_RE.test(n)));
  if (def && plain(def)) return def;
  return only(printers.filter(plain));
}

// Stage 133.4: ZPL only for a printer named as a Zebra. Other label printers
// (TSC, TVS, Xprinter, Gprinter, the 4-inch Y468BT kind) mostly speak TSPL,
// and a TSPL printer sent ZPL feeds blank tags. The Printer record shows the
// pick and is one edit away.
const ZEBRA_RE = /(zebra|zdesigner|\bz[dt]\d|\bg[kxt]\d|\btlp ?\d|\blp ?28)/i;
function languageFor(kind, name) {
  if (kind === 'Receipt') return 'ESC-POS';
  if (kind === 'Sticker') return ZEBRA_RE.test(name) ? 'ZPL' : 'TSPL';
  return 'PDF';
}

// notMapped (Stage 133.5) is what to do about a Printer record with no OS
// printer name - for qzTryPrint's alert and its silent-print failure alike.
export const notMapped = (name) => `Printer "${name}" has no OS Printer Name. Open Setup → Printing → Printer, ` +
  `open "${name}" and enter the exact Windows printer name (Detect Printers, under Printer setup on the Sticker Printing page, lists them).`;

// askPrinter is the one question, in the app's own dialog.
function askPrinter(kind, printers, preselect, computer) {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    if (!backdrop) { resolve(''); return; }
    const extra = document.getElementById('custom-dialog-extra');
    document.getElementById('custom-dialog-title').textContent = `Printer for ${kind}`;
    document.getElementById('custom-dialog-message').textContent =
      `Which printer prints ${kind} on this computer (${computer})? It is asked once; change it any time from the print dialog.`;
    extra.innerHTML = `<select id="custom-dialog-printer" class="form-input" style="width:100%;margin-top:12px;">${printers.map(n =>
      `<option${n === preselect ? ' selected' : ''}>${window.escapeHTMLText(n)}</option>`).join('')}</select>`;
    extra.classList.remove('hidden');
    const cancel = document.getElementById('custom-dialog-cancel-btn');
    cancel.style.display = '';
    backdrop.classList.remove('hidden');
    document.getElementById('custom-dialog-printer').focus();
    const done = (value) => {
      backdrop.classList.add('hidden');
      extra.innerHTML = '';
      extra.classList.add('hidden');
      ['custom-dialog-ok-btn', 'custom-dialog-cancel-btn', 'custom-dialog-close-btn'].forEach(id => {
        const b = document.getElementById(id);
        b.replaceWith(b.cloneNode(true));
      });
      resolve(value);
    };
    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => done(document.getElementById('custom-dialog-printer').value));
    cancel.addEventListener('click', () => done(''));
    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => done(''));
  });
}

// choosePrinter picks (or, with ask, asks for) this computer's printer for a
// kind and saves it. Returns the Printer record, or null when the tray lists
// no printer or the question was cancelled.
export async function choosePrinter(kind, location, { ask = false } = {}) {
  const computer = await printComputer();
  let printers = [], def = '';
  try { printers = (await window.QZPrint.listOSPrinters()) || []; } catch (_) { return null; }
  try { def = (await window.QZPrint.getDefaultPrinter()) || ''; } catch (_) { /* no default */ }
  if (!printers.length) return null;
  let pick = ask ? '' : autoPick(kind, printers, def);
  const automatic = !!pick;
  if (!pick) pick = await askPrinter(kind, printers, autoPick(kind, printers, '') || def, computer);
  if (!pick) return null;
  const res = await window.apiFetch('/api/v1/print/qz/assignments', {
    method: 'POST',
    body: JSON.stringify({ computer, location: storeCode(location), print_kind: kind, qz_printer_name: pick, printer_language: languageFor(kind, pick) })
  });
  if (!res) return null;
  if (!res.ok) { await window.showApiError(res, 'Could not remember the printer.'); return null; }
  if (automatic) window.showToast?.(`${kind} will print on ${pick} on this computer. Change it from the print dialog.`, { variant: 'info' });
  return (await res.json()).printer || null;
}

// needsChoice says whether a refused QZ payload means "no printer chosen yet".
export async function needsChoice(res) {
  try { return (await res.clone().json()).code === 'DEVICE-0298'; } catch (_) { return false; }
}

// chooseAndPrint is qzTryPrint's second try: choose this computer's printer
// for the job's kind, then print on it.
export async function chooseAndPrint(jobType, opts) {
  const printer = await choosePrinter(printKindFor(opts.printKind || jobType), opts.location);
  if (!printer) {
    if (opts.why) Object.assign(opts.why, { setup: true,
      reason: 'No printer is chosen for this print on this computer. Retry to choose one; if the list is empty, install the printer in Windows first.' });
    return false;
  }
  return window.qzTryPrint(jobType, { ...opts, printerCode: printer.code, chosen: true });
}

// --- Stage 127.2: no browser print dialog once PrintBridge is set up ---------
//
// User, 2026-10-21: "if the app installed and we configured the qz silence for
// print it should not ask any where the computer print page for any print".
// Set up = this computer has a printer chosen for that kind of print. Then a
// print of that kind goes to it, or says why it could not and offers Retry or
// the browser - it never opens the browser's print dialog by itself. A
// computer with nothing chosen prints through the browser exactly as before.

// silentSetUp says whether this computer prints this kind through PrintBridge.
export async function silentSetUp(kind, location) {
  try { return !!(await chosenPrinter(printKindFor(kind), location)); } catch (_) { return false; }
}

// explainFailure is the one question after a silent print failed:
// 'retry' | 'browser' | 'cancel'. Stage 133.5: a setup reason (no printer
// chosen, no OS printer name, the server refused the print) says itself what
// to fix; the PrintBridge advice is only for the agent and the printer.
export function explainFailure(kind, reason, setup = false) {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    if (!backdrop) { resolve('cancel'); return; }
    const extra = document.getElementById('custom-dialog-extra');
    const ok = document.getElementById('custom-dialog-ok-btn');
    const okText = ok.textContent;
    document.getElementById('custom-dialog-title').textContent = `${kind} did not print`;
    document.getElementById('custom-dialog-message').textContent = setup && reason ? reason :
      `${reason || 'PrintBridge did not answer.'} Check that PrintBridge is running on this computer and the printer is on and loaded, then Retry.`;
    extra.innerHTML = '<button type="button" class="btn btn-outline btn-sm" id="custom-dialog-browser" style="margin-top:12px;">Print with the browser instead</button>';
    extra.classList.remove('hidden');
    ok.textContent = 'Retry';
    const cancel = document.getElementById('custom-dialog-cancel-btn');
    cancel.style.display = '';
    backdrop.classList.remove('hidden');
    ok.focus();
    const done = (value) => {
      backdrop.classList.add('hidden');
      extra.innerHTML = '';
      extra.classList.add('hidden');
      ok.textContent = okText;
      ['custom-dialog-ok-btn', 'custom-dialog-cancel-btn', 'custom-dialog-close-btn'].forEach(id => {
        const b = document.getElementById(id);
        b.replaceWith(b.cloneNode(true));
      });
      resolve(value);
    };
    ok.addEventListener('click', () => done('retry'));
    cancel.addEventListener('click', () => done('cancel'));
    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => done('cancel'));
    document.getElementById('custom-dialog-browser').addEventListener('click', () => done('browser'));
  });
}

// printNoDialog is every print's way out (Stage 127.2). trySilent(why) prints
// through PrintBridge and returns true, or false with why.reason set;
// browserPrint opens the browser's print. Returns false only when the user
// gave up on a failed silent print.
export async function printNoDialog(kind, location, trySilent, browserPrint) {
  lastPrint.channel = '';
  for (;;) {
    const why = {};
    let printed = false;
    try { printed = await trySilent(why); } catch (e) { why.reason = why.reason || (e && e.message) || String(e); }
    if (printed) { lastPrint.channel = 'qz'; return true; }
    if (!await silentSetUp(kind, location)) { lastPrint.channel = 'browser'; await browserPrint(); return true; }
    const choice = await explainFailure(printKindFor(kind), why.reason || '', why.setup);
    if (choice === 'retry') continue;
    if (choice === 'browser') { lastPrint.channel = 'browser'; await browserPrint(); return true; }
    return false;
  }
}

// lastPrint (Stage 130.16, 74.1.4) is how the last print went out - 'qz'
// (submitted to PrintBridge) or 'browser' (handed to the browser) - for the
// print log; neither is proof of paper.
export const lastPrint = { channel: '' };
