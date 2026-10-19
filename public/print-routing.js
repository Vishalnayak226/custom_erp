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
  SalesInvoice: 'Sales Invoice', POSTaxInvoice: 'Sales Invoice',
  DebitNote: 'Returns and Notes', VendorCreditNote: 'Returns and Notes', PurchaseReturn: 'Returns and Notes',
  CreditNote: 'Returns and Notes', SalesReturn: 'Returns and Notes',
  // QZ job types
  Receipt: 'Receipt', Sticker: 'Sticker', 'Shipping Label': 'Shipping Label', Invoice: 'Sales Invoice',
  'Purchase Order': 'Purchase Order', Document: 'Shipping Label'
};
export const printKindFor = (txnOrJob) => KIND_OF[txnOrJob] || txnOrJob;

const RECEIPT_RE = /(pos|thermal|receipt|\btm-?[a-z]?\d|\brp-?\d|xp-?\d|80\s?mm|58\s?mm|retsol|xprinter)/i;
const LABEL_RE = /(zebra|\bzd\d|\bgk4|\bgx4|\btsc|\btte|\btlp|godex|argox|label|barcode|datamax|honeywell|snbc)/i;
const VIRTUAL_RE = /(pdf|xps|onenote|fax|send to|microsoft print)/i;

let computerName = null;
// printComputer is this computer's name: QZ Tray's, else one remembered in
// this browser (an old tray that cannot tell its name).
export async function printComputer() {
  if (computerName) return computerName;
  let name = '';
  try { name = String(await window.QZPrint.hostname() || '').trim(); } catch (_) { /* old tray */ }
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

function languageFor(kind, name) {
  if (kind === 'Receipt') return 'ESC-POS';
  if (kind === 'Sticker') return /(tsc|tte|snbc)/i.test(name) ? 'TSPL' : 'ZPL';
  return 'PDF';
}

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
  if (!printer) return false;
  return window.qzTryPrint(jobType, { ...opts, printerCode: printer.code, chosen: true });
}
