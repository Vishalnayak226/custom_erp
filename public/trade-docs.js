// Stage 62/63: transaction prints and the Print Template Studio's renderer.
//
// One renderer draws a PrintTemplate layout (mm-positioned elements) over the
// print data the server assembles (GET /api/v1/print/document/{txn}/{id}) -
// the same code paints the studio canvas, the print dialog preview and the
// printed pages, so what is designed is what prints. A line-items table flows
// across pages with its column headings repeated. Around it the position
// decides: the header above it prints on the first page only, the tax
// summary, totals, words, signatures and anything else under it on the last
// page only; a page number, and an element beside the table, keep their own
// "Prints on". The brand always prints on the first page.
//
// Loaded on demand (with the calling view's ?v=) and kept out of the
// cold-core app.js/styles.css, which have a release budget. Shell services
// (apiFetch, escapeHTMLText, showApiError, ...) are the classic-script globals.

const VERSION_QUERY = new URL(import.meta.url).search;

export const tradeStylesReady = (() => {
  const id = 'trade-docs-css';
  if (document.getElementById(id)) return Promise.resolve();
  return new Promise(resolve => {
    const link = document.createElement('link');
    link.id = id;
    link.rel = 'stylesheet';
    link.href = '/trade-docs.css' + VERSION_QUERY;
    link.onload = () => resolve();
    link.onerror = () => resolve();
    document.head.appendChild(link);
  });
})();

const esc = (s) => window.escapeHTMLText ? window.escapeHTMLText(String(s ?? '')) : String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

export const PAGE_SIZES = { A4: [210, 297], A5: [148, 210], Letter: [215.9, 279.4] };
export const PX_PER_MM = 96 / 25.4;

export function pageDims(page) {
  const [w, h] = PAGE_SIZES[page?.size] || PAGE_SIZES.A4;
  return page?.orientation === 'landscape' ? [h, w] : [w, h];
}

export const TRANSACTIONS = [
  { key: 'PurchaseOrder', label: 'Purchase Order', doctype: 'PurchaseOrder' },
  { key: 'VendorPO', label: 'Vendor PO (copy for the vendor)', doctype: 'PurchaseOrder' },
  { key: 'GRN', label: 'Goods Receipt Note', doctype: 'GRN' },
  { key: 'TransferOut', label: 'Transfer Out (dispatch challan)', doctype: 'TransferOrder' },
  { key: 'TransferIn', label: 'Transfer In (receipt)', doctype: 'TransferOrder' },
  { key: 'DebitNote', label: 'Debit Note', doctype: 'DebitNote' },
  { key: 'VendorCreditNote', label: 'Credit Note (to vendor)', doctype: 'VendorCreditNote' },
  // Stage 71: the standard print formats pack.
  { key: 'GRNVendorCopy', label: 'Vendor Copy (goods receipt acknowledgement)', doctype: 'GRN' },
  { key: 'PurchaseReturn', label: 'Purchase Return (return to vendor)', doctype: 'PurchaseReturn' },
  { key: 'CreditNote', label: 'Credit Note (to customer)', doctype: 'CreditNote' },
  { key: 'SalesReturn', label: 'Sales Return', doctype: 'ReturnRequest' },
  // Stage 85.D2: a till sale to a GSTIN buyer.
  { key: 'POSTaxInvoice', label: 'Tax Invoice (GST bill from the till)', doctype: 'POSCart' }
];
export const txnLabel = (k) => (TRANSACTIONS.find(t => t.key === k) || {}).label || k;

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

export function resolvePath(obj, path) {
  if (!obj || !path) return undefined;
  let cur = obj;
  for (const part of String(path).split('.')) {
    if (cur == null) return undefined;
    cur = cur[part];
  }
  return cur;
}

const moneyFmt = new Intl.NumberFormat('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const qtyFmt = new Intl.NumberFormat('en-IN', { maximumFractionDigits: 3 });

export function formatValue(v, fmt) {
  if (v === undefined || v === null || v === '') return '';
  if (typeof v === 'object') return '';
  switch (fmt) {
    case 'money': return isNaN(Number(v)) ? String(v) : moneyFmt.format(Number(v));
    case 'qty': return isNaN(Number(v)) ? String(v) : qtyFmt.format(Number(v));
    case 'percent': return isNaN(Number(v)) ? String(v) : `${qtyFmt.format(Number(v))}%`;
    case 'date': {
      const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(v));
      return m ? `${m[3]}-${m[2]}-${m[1]}` : String(v);
    }
    default:
      return typeof v === 'boolean' ? (v ? 'Yes' : 'No') : String(v);
  }
}

// guessFormat picks a sensible format for a key when the template set none.
export function guessFormat(key) {
  const k = String(key || '').split('.').pop();
  if (/^(rate|mrp|taxable|cgst|sgst|igst|tax|amount|grand_total|round_off|po_rate|invoice_rate|rate_difference|po_value|invoice_value|invoice_gst|invoice_total|vendor_invoice_amount|invoice_amount_difference|total_amount|standard_cost|sale_price|gst_amount|total_with_gst|original_taxable|original_disc)$/.test(k)) return 'money';
  if (/(^|_)qty$|^qty|line_count/.test(k)) return 'qty';
  if (/gst_rate$/.test(k)) return 'percent';
  if (/date$|^date$/.test(k)) return 'date';
  return 'text';
}

// fillTokens replaces {path} or {path|format} in text from ctx.
export function fillTokens(text, ctx) {
  return String(text || '').replace(/\{([a-zA-Z0-9_.]+)(?:\|([a-z]+))?\}/g, (_, path, fmt) => formatValue(resolvePath(ctx, path), fmt || guessFormat(path)));
}

function tokensAllBlank(text, ctx) {
  const toks = [...String(text || '').matchAll(/\{([a-zA-Z0-9_.]+)(?:\|[a-z]+)?\}/g)];
  return toks.length > 0 && toks.every(m => formatValue(resolvePath(ctx, m[1])) === '');
}

// ---------------------------------------------------------------------------
// Built-in standard layouts (Stage 62.4: the "std print profile")
// ---------------------------------------------------------------------------

let nextId = 1;
export const newElementId = () => `e${Date.now().toString(36)}${(nextId++).toString(36)}`;

const col = (key, label, width, extra = {}) => ({ key, label, width, align: extra.align || (['text', 'image'].includes(extra.format || guessFormat(key)) && key !== 'sr' ? 'left' : 'right'), format: extra.format || guessFormat(key), ...extra });

// Stage 71: columns, header fields, totals and signatures follow the user's
// "Standard ERP Transaction Print Formats" pack. Fields the ERP does not hold
// (bin per line, cess, IRN, vehicle / LR, payment terms...) are left out
// rather than printed empty.
const TXN_COLUMNS = {
  PurchaseOrder: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Item / Barcode', 20), col('description', 'Description / Variant', 38), col('hsn_code', 'HSN', 9), col('uom', 'UOM', 7), col('qty', 'Qty', 8), col('rate', 'Unit Price', 12), col('gst_rate', 'Tax %', 7), col('amount', 'Line Total', 14)],
  VendorPO: [col('sr', 'Ln', 4, { align: 'center' }), col('image', 'Image', 16, { format: 'image', align: 'center' }), col('item_code', 'Item / Barcode', 18), col('description', 'Description / Variant', 32), col('hsn_code', 'HSN', 9), col('uom', 'UOM', 7), col('qty', 'Qty', 8), col('rate', 'Unit Price', 12), col('gst_rate', 'Tax %', 7), col('amount', 'Line Total', 14)],
  GRN: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Barcode / Item', 17), col('description', 'Design / Variant', 25), col('po_qty', 'PO Qty', 8), col('qty', 'Received', 8), col('accepted_qty', 'Accepted', 8), col('rejected_qty', 'Rejected', 8), col('damaged_qty', 'Damaged', 8), col('qc_status', 'QC Status', 11, { align: 'left' }), col('variance_reason', 'Variance Reason', 17)],
  GRNVendorCopy: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Item / Barcode', 18), col('description', 'Description', 27), col('po_qty', 'Ordered Qty', 10), col('qty', 'Delivered Qty', 10), col('accepted_qty', 'Accepted Qty', 10), col('rejected_qty', 'Rejected Qty', 10), col('damaged_qty', 'Damaged Qty', 11), col('variance_reason', 'Remarks', 18)],
  PurchaseReturn: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Barcode / Item', 16), col('description', 'Description', 27), col('received_qty', 'Received Qty', 9), col('return_qty', 'Return Qty', 9), col('uom', 'UOM', 6), col('rate', 'Rate', 11), col('gst_rate', 'Tax %', 7), col('taxable', 'Return Value', 13), col('reason', 'Reason', 16)],
  TransferOut: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Barcode / Combination', 18), col('description', 'Description', 31), col('hsn_code', 'HSN', 9), col('uom', 'UOM', 7), col('requested_qty', 'Requested Qty', 10), col('shipped_qty', 'Shipped Qty', 10), col('rate', 'Value / Unit', 11), col('taxable', 'Value', 13)],
  TransferIn: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Barcode / Combination', 20), col('description', 'Description', 36), col('shipped_qty', 'Shipped Qty', 11), col('received_qty', 'Received Qty', 11), col('short_qty', 'Short', 9), col('reason', 'Reason', 22)],
  DebitNote: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Item', 15), col('hsn_code', 'HSN', 8), col('description', 'Description / Reason', 28), col('original_taxable', 'Original Taxable', 13), col('taxable', 'Debit Taxable', 13), col('cgst', 'CGST', 10), col('sgst', 'SGST', 10), col('igst', 'IGST', 10), col('amount', 'Debit Total', 13)],
  CreditNote: [col('sr', 'Ln', 5, { align: 'center' }), col('description', 'Description / Reason', 120), col('amount', 'Credit Amount', 30)],
  POSTaxInvoice: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Item / Barcode', 16), col('description', 'Description', 30), col('hsn_code', 'HSN', 9), col('qty', 'Qty', 7), col('rate', 'Rate (incl. GST)', 12), col('discount', 'Discount', 10), col('taxable', 'Taxable', 12), col('gst_rate', 'GST %', 7), col('amount', 'Amount', 13)],
  SalesReturn: [col('sr', 'Ln', 4, { align: 'center' }), col('item_code', 'Barcode', 16), col('description', 'Item / Description', 28), col('original_qty', 'Original Qty', 9), col('return_qty', 'Return Qty', 9), col('mrp', 'MRP', 11), col('original_disc', 'Original Disc.', 11), col('tax', 'Tax', 10), col('amount', 'Refund Value', 13), col('condition', 'Condition', 12)]
};
TXN_COLUMNS.VendorCreditNote = TXN_COLUMNS.DebitNote.map(c => ({ ...c,
  label: { taxable: 'Credit Taxable', amount: 'Credit Total' }[c.key] || c.label }));

// The field grid under the parties: [text, column, row]. column 0 / 1 is the
// left / right half, 2 the full width. A piece whose fields are all blank
// drops out instead of printing a label with nothing after it.
const TXN_INFO = {
  PurchaseOrder: [['Ship to: {ship_to.name}, {ship_to.address} {ship_to.city}', 2, 0], ['Price basis: {fields.gst_mode} of GST', 0, 1], ['Currency: {fields.currency}', 1, 1], ['Buyer: {audit.created_by}', 0, 2]],
  GRN: [['PO No.: {fields.po_numbers}', 0, 0], ['Vendor invoice / challan: {fields.vendor_invoice_no}', 1, 0], ['ASN: {fields.asn_id}', 0, 1], ['Invoice date: {fields.vendor_invoice_date|date}', 1, 1]],
  PurchaseReturn: [['Original PO: {fields.po_numbers}', 0, 0], ['Original GRN: {fields.grn_id}', 1, 0], ['Vendor invoice: {fields.vendor_invoice_no}', 0, 1], ['Invoice date: {fields.vendor_invoice_date|date}', 1, 1], ['Return reason: {fields.reason}', 0, 2], ['Linked debit note: {fields.debit_note_id}', 1, 2]],
  TransferOut: [['Master transfer order: {number}', 0, 0], ['Transfer type: {fields.supply_type}', 1, 0], ['Value basis: {fields.value_basis}', 0, 1], ['Stock status after posting: {fields.stock_status_after}', 1, 1]],
  TransferIn: [['Master transfer order: {number}', 0, 0], ['Receipt status: {fields.receipt_status}', 1, 0], ['Transfer type: {fields.supply_type}', 0, 1], ['Stock status after posting: {fields.stock_status_after}', 1, 1]],
  DebitNote: [['Original invoice no.: {fields.original_invoice_no}', 0, 0], ['Original invoice date: {fields.original_invoice_date|date}', 1, 0], ['Against GRN: {fields.grn_id}', 0, 1], ['PO: {fields.po_numbers}', 1, 1], ['Document direction: {fields.document_direction}', 0, 2], ['Place of supply: {fields.place_of_supply}', 1, 2], ['Reason: {fields.reason}', 2, 3]],
  CreditNote: [['Original invoice no.: {fields.original_invoice_no}', 0, 0], ['Original invoice date: {fields.original_invoice_date|date}', 1, 0], ['Reason: {fields.reason}', 2, 1]],
  POSTaxInvoice: [['Buyer GSTIN: {fields.buyer_gstin}', 0, 0], ['Place of supply: {fields.place_of_supply}', 1, 0], ['Supply: {fields.supply_type} · Reverse charge: {fields.reverse_charge}', 0, 1], ['Payment: {fields.payment_mode}', 1, 1]],
  SalesReturn: [['Original bill no.: {fields.original_order}', 0, 0], ['Original bill date: {fields.original_invoice_date|date}', 1, 0], ['Refund method: {fields.refund_method}', 0, 1], ['Refund no.: {fields.refund_no}', 1, 1], ['Return type: {fields.return_type}', 0, 2]]
};
TXN_INFO.VendorPO = TXN_INFO.PurchaseOrder;
TXN_INFO.GRNVendorCopy = TXN_INFO.GRN;
TXN_INFO.VendorCreditNote = TXN_INFO.DebitNote;

const tot = (key, label, extra = {}) => ({ key: `totals.${key}`, label, ...extra });
const TAX_ROWS = [tot('cgst', 'CGST', { when: 'intra', hideZero: true }), tot('sgst', 'SGST', { when: 'intra', hideZero: true }), tot('igst', 'IGST', { when: 'inter', hideZero: true })];
const TXN_TOTALS = {
  PurchaseOrder: [tot('taxable', 'Subtotal'), ...TAX_ROWS, tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'PO Total', { strong: true })],
  GRN: [tot('po_qty', 'Total PO qty'), tot('qty', 'Total received'), tot('accepted_qty', 'Accepted', { strong: true }), tot('rejected_qty', 'Rejected'), tot('damaged_qty', 'Damaged', { hideZero: true })],
  GRNVendorCopy: [tot('po_qty', 'Total ordered'), tot('qty', 'Total delivered'), tot('accepted_qty', 'Accepted', { strong: true }), tot('rejected_qty', 'Rejected'), tot('damaged_qty', 'Damaged', { hideZero: true })],
  PurchaseReturn: [tot('return_qty', 'Total returned qty'), tot('taxable', 'Taxable return value'), tot('tax', 'Tax adjustment'), tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Total return value', { strong: true })],
  TransferOut: [tot('shipped_qty', 'Total pieces'), tot('taxable', 'Value'), ...TAX_ROWS, tot('grand_total', 'Total value', { strong: true })],
  TransferIn: [tot('shipped_qty', 'Total shipped'), tot('received_qty', 'Total received', { strong: true }), tot('short_qty', 'Short')],
  DebitNote: [tot('taxable', 'Taxable amount debited'), tot('tax', 'Tax debited'), tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Total debit note', { strong: true })],
  VendorCreditNote: [tot('taxable', 'Taxable amount credited'), tot('tax', 'Tax credited'), tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Total credit note', { strong: true })],
  CreditNote: [tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Total credit note', { strong: true })],
  POSTaxInvoice: [tot('taxable', 'Taxable value'), ...TAX_ROWS, tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Invoice total', { strong: true })],
  SalesReturn: [tot('taxable', 'Return subtotal'), tot('tax', 'Tax reversal'), tot('round_off', 'Round off', { hideZero: true }), tot('grand_total', 'Net refund / credit', { strong: true })]
};
TXN_TOTALS.VendorPO = TXN_TOTALS.PurchaseOrder;

const TXN_SIGNATURES = {
  PurchaseOrder: ['Prepared By', 'Approved By', 'Supplier Acceptance'],
  GRN: ['Received By', 'QC / Verification', 'Posted By / Approver'],
  GRNVendorCopy: ['Vendor / Driver', 'Received By', 'Security / Gate'],
  PurchaseReturn: ['Prepared / Packed By', 'Approved By', 'Vendor / Transporter'],
  TransferOut: ['Picked / Packed By', 'Dispatched By', 'Transporter / Handover'],
  TransferIn: ['Received By', 'Verified By', 'Posted / Approved By'],
  DebitNote: ['Prepared By', 'Approved By', 'Authorised Signatory'],
  SalesReturn: ['Processed By', 'Verified / Approved By', 'Customer Acknowledgement'],
  POSTaxInvoice: ['Prepared By', "Customer's Signature", 'Authorised Signatory']
};
TXN_SIGNATURES.VendorPO = TXN_SIGNATURES.PurchaseOrder;
TXN_SIGNATURES.VendorCreditNote = TXN_SIGNATURES.CreditNote = TXN_SIGNATURES.DebitNote;

// Prints that carry no money (quantities only) have no tax summary and no
// amount in words.
const QTY_ONLY = { GRN: 1, GRNVendorCopy: 1, TransferIn: 1, CreditNote: 1 };
// The one note the pack prints on the document itself.
const TXN_NOTE = {
  GRNVendorCopy: 'This acknowledges the quantity received at this location. It is not a confirmation of invoice acceptance, payment approval, final QC acceptance or agreed valuation.'
};

export function defaultLayout(txn) {
  const blockTitle = { PurchaseOrder: 'Bill to', VendorPO: 'Bill to', GRN: 'Received at', GRNVendorCopy: 'Received at', PurchaseReturn: 'Dispatched from', TransferOut: 'From location', TransferIn: 'To location', DebitNote: 'Issuer', VendorCreditNote: 'Issuer', CreditNote: 'Issuer', SalesReturn: 'Store', POSTaxInvoice: 'Supplier' }[txn] || 'From';
  const E = (o) => ({ id: newElementId(), fontSize: 9, align: 'left', pages: 'first', ...o });
  const info = TXN_INFO[txn] || [];
  const infoRows = info.reduce((n, i) => Math.max(n, i[2] + 1), 0);
  const tableY = 63 + infoRows * 5 + 2;
  const noMoney = !!QTY_ONLY[txn];
  const note = TXN_NOTE[txn];
  const signs = TXN_SIGNATURES[txn] || TXN_SIGNATURES.DebitNote;
  return {
    version: 1,
    page: { size: 'A4', orientation: 'portrait', margin: 10, font: 'Inter, Arial, sans-serif' },
    elements: [
      E({ type: 'field', key: 'brand.name', x: 10, y: 8, w: 105, h: 9, fontSize: 16, bold: true }),
      E({ type: 'text', text: 'Financial year: {fields.financial_year}', x: 10, y: 17, w: 80, h: 4.5, fontSize: 8 }),
      E({ type: 'text', text: 'Status: {status}', x: 10, y: 21.5, w: 80, h: 4.5, fontSize: 8 }),
      E({ type: 'field', key: 'title', x: 105, y: 8, w: 95, h: 7, fontSize: 12, bold: true, align: 'right' }),
      E({ type: 'text', text: 'No: {number}', x: 125, y: 15, w: 75, h: 5, align: 'right' }),
      E({ type: 'text', text: 'Date: {date|date}', x: 125, y: 20, w: 75, h: 5, align: 'right' }),
      E({ type: 'line', x: 10, y: 27, w: 190, h: 0.4, thickness: 0.4 }),
      E({ type: 'block', block: 'store', title: blockTitle, x: 10, y: 30, w: 93, h: 30, border: true }),
      E({ type: 'block', block: 'party', title: '{party_title}', x: 107, y: 30, w: 93, h: 30, border: true }),
      ...info.map(([text, c, r]) => E({ type: 'text', text, x: c === 1 ? 107 : 10, y: 62 + r * 5, w: c === 2 ? 190 : 93, h: 5, fontSize: 8 })),
      E({ type: 'table', x: 10, y: tableY, w: 190, h: 218 - tableY, columns: TXN_COLUMNS[txn] || TXN_COLUMNS.PurchaseOrder, fontSize: 8, headerBg: '#f1f3f5', imageSize: 14, pages: 'all' }),
      ...(noMoney ? [] : [E({ type: 'taxSummary', x: 10, y: 222, w: 112, h: 30, fontSize: 8, pages: 'last' })]),
      E({ type: 'totals', x: 128, y: 222, w: 72, h: 32, fontSize: 9, pages: 'last', rows: TXN_TOTALS[txn] || TXN_TOTALS.PurchaseOrder }),
      ...(noMoney ? [] : [E({ type: 'text', text: 'Amount in words: {amount_in_words}', x: 10, y: 255, w: 190, h: 6, fontSize: 8.5, italic: true, pages: 'last' })]),
      ...(note ? [E({ type: 'text', text: note, x: 10, y: 222, w: 112, h: 18, fontSize: 8, italic: true, border: true, pages: 'last' })] : []),
      ...signs.flatMap((label, i) => [
        E({ type: 'line', x: 10 + i * 65, y: 275, w: 55, h: 0.3, thickness: 0.3, pages: 'last' }),
        E({ type: 'text', text: label, x: 10 + i * 65, y: 276, w: 55, h: 5, fontSize: 8, align: 'center', pages: 'last' })
      ]),
      E({ type: 'text', text: 'Created: {audit.created_by} {audit.created_at}', x: 10, y: 283, w: 62, h: 4, fontSize: 6.5, pages: 'last' }),
      E({ type: 'text', text: 'Approved: {audit.approved_by} {audit.approved_at}', x: 72, y: 283, w: 62, h: 4, fontSize: 6.5, pages: 'last' }),
      E({ type: 'text', text: 'Printed: {audit.printed_by} {audit.printed_at}', x: 134, y: 283, w: 66, h: 4, fontSize: 6.5, align: 'right', pages: 'last' }),
      E({ type: 'pageNo', x: 10, y: 288, w: 190, h: 5, fontSize: 7, align: 'center', pages: 'all' })
    ]
  };
}

export function parseLayout(raw, txn) {
  if (raw && typeof raw === 'object') return raw;
  try {
    const l = JSON.parse(raw || '');
    if (l && Array.isArray(l.elements)) return l;
  } catch (e) { /* fall through */ }
  return defaultLayout(txn);
}

export function layoutUsesImages(layout) {
  return (layout.elements || []).some(el =>
    (el.type === 'table' && (el.columns || []).some(c => c.key === 'image')) || (el.type === 'field' && el.key === 'image'));
}

// ---------------------------------------------------------------------------
// Element painting (mm everywhere; the page is a positioned box in mm)
// ---------------------------------------------------------------------------

function boxStyle(el) {
  const s = [`left:${el.x}mm`, `top:${el.y}mm`, `width:${el.w}mm`, `height:${el.h}mm`,
    `font-size:${el.fontSize || 9}pt`, `text-align:${el.align || 'left'}`];
  if (el.bold) s.push('font-weight:700');
  if (el.italic) s.push('font-style:italic');
  if (el.color) s.push(`color:${el.color}`);
  if (el.bg) s.push(`background:${el.bg}`);
  if (el.border) s.push(`border:0.25mm solid ${el.borderColor || '#333'}`);
  return s.join(';');
}

const STORE_LINES = [
  ['name', 'Name'], ['legal_name', 'Legal name'], ['address', 'Address'], ['city_pin', 'City / PIN'],
  ['state', 'State'], ['gstin', 'GSTIN'], ['phone', 'Phone'], ['email', 'Email']
];
export const BLOCK_LINE_OPTIONS = STORE_LINES;

function blockHTML(el, ctx) {
  const p = resolvePath(ctx, el.block || 'store') || {};
  const show = el.show || ['name', 'legal_name', 'address', 'city_pin', 'state', 'gstin', 'phone'];
  const rows = [];
  show.forEach(k => {
    let v = '';
    if (k === 'city_pin') v = [p.city, p.pincode].filter(Boolean).join(' - ');
    else if (k === 'state') v = p.state ? `State: ${p.state}` : '';
    else if (k === 'gstin') v = p.gstin ? `GSTIN: ${p.gstin}` : '';
    else if (k === 'phone') v = p.phone ? `Ph: ${p.phone}` : '';
    else if (k === 'legal_name') v = p.legal_name && p.legal_name !== p.name ? p.legal_name : '';
    else v = p[k] || '';
    if (v) rows.push(`<div class="${k === 'name' ? 'tdp-strong' : ''}">${esc(v)}</div>`);
  });
  const title = fillTokens(el.title || '', ctx);
  return `${title ? `<div class="tdp-block-title">${esc(title)}</div>` : ''}${rows.join('')}`;
}

function cellHTML(colDef, line, el) {
  if (colDef.key === 'image') {
    const src = line.image;
    const size = el.imageSize || 14;
    return src ? `<img src="${esc(src)}" alt="" style="width:${size}mm;height:${size}mm;object-fit:contain;">` : '';
  }
  return esc(formatValue(resolvePath(line, colDef.key), colDef.format || guessFormat(colDef.key)));
}

function tableHead(el) {
  const cols = el.columns || [];
  const total = cols.reduce((s, c) => s + (Number(c.width) || 1), 0) || 1;
  return `<colgroup>${cols.map(c => `<col style="width:${((Number(c.width) || 1) / total * 100).toFixed(3)}%">`).join('')}</colgroup>
    <thead><tr>${cols.map(c => `<th style="text-align:${c.align || 'left'};${el.headerBg ? `background:${el.headerBg};` : ''}">${esc(c.label || '')}</th>`).join('')}</tr></thead>`;
}

function tableRowsHTML(el, lines) {
  const cols = el.columns || [];
  return lines.map(l => `<tr>${cols.map(c => `<td style="text-align:${c.align || 'left'}">${cellHTML(c, l, el)}</td>`).join('')}</tr>`).join('');
}

function taxSummaryHTML(el, ctx) {
  const rows = ctx.tax_summary || [];
  const inter = !!ctx.interstate;
  // Stage 85.D2: a tax invoice's summary is HSN-wise.
  const hsn = rows.some(r => r.hsn_code !== undefined);
  const head = (hsn ? ['HSN'] : []).concat(inter ? ['GST %', 'Taxable', 'IGST', 'Total tax'] : ['GST %', 'Taxable', 'CGST', 'SGST', 'Total tax']);
  const body = rows.map(r => `<tr>${hsn ? `<td>${esc(r.hsn_code || '-')}</td>` : ''}<td>${formatValue(r.gst_rate, 'percent')}</td><td class="num">${formatValue(r.taxable, 'money')}</td>${inter ? `<td class="num">${formatValue(r.igst, 'money')}</td>` : `<td class="num">${formatValue(r.cgst, 'money')}</td><td class="num">${formatValue(r.sgst, 'money')}</td>`}<td class="num">${formatValue(r.tax, 'money')}</td></tr>`).join('');
  return `<table class="tdp-table tdp-tax"><thead><tr>${head.map((h, i) => `<th${i ? ' class="num"' : ''}>${h}</th>`).join('')}</tr></thead><tbody>${body || `<tr><td colspan="${head.length}">No tax</td></tr>`}</tbody></table>`;
}

export const DEFAULT_TOTAL_ROWS = [
  { key: 'totals.taxable', label: 'Taxable value' },
  { key: 'totals.cgst', label: 'CGST', when: 'intra' },
  { key: 'totals.sgst', label: 'SGST', when: 'intra' },
  { key: 'totals.igst', label: 'IGST', when: 'inter' },
  { key: 'totals.round_off', label: 'Round off', hideZero: true },
  { key: 'totals.grand_total', label: 'Grand total', strong: true }
];

function totalsHTML(el, ctx) {
  const rows = (el.rows && el.rows.length ? el.rows : DEFAULT_TOTAL_ROWS).filter(r => {
    if (r.when === 'intra' && ctx.interstate) return false;
    if (r.when === 'inter' && !ctx.interstate) return false;
    if (r.hideZero && !Number(resolvePath(ctx, r.key))) return false;
    return true;
  });
  return `<table class="tdp-totals">${rows.map(r => `<tr class="${r.strong ? 'tdp-strong' : ''}"><td>${esc(r.label)}</td><td class="num">${esc(formatValue(resolvePath(ctx, r.key), r.format || guessFormat(r.key)))}</td></tr>`).join('')}</table>`;
}

// elementHTML paints one non-table element's inner HTML.
export function elementHTML(el, ctx, pageInfo) {
  switch (el.type) {
    case 'text': {
      if (tokensAllBlank(el.text, ctx)) return '';
      return esc(fillTokens(el.text, ctx)).replace(/\n/g, '<br>');
    }
    case 'field': {
      const v = formatValue(resolvePath(ctx, el.key), el.format || guessFormat(el.key));
      if (v === '' && el.key !== 'brand.name') return '';
      return `${el.label ? `<span class="tdp-label">${esc(el.label)}</span> ` : ''}${esc(v)}`;
    }
    case 'block': return blockHTML(el, ctx);
    case 'image': return el.src ? `<img src="${esc(el.src)}" alt="" style="width:100%;height:100%;object-fit:${el.fit || 'contain'};">` : '';
    case 'line': return `<div style="position:absolute;inset:0;${el.w >= el.h ? `border-top:${el.thickness || 0.3}mm solid ${el.color || '#333'}` : `border-left:${el.thickness || 0.3}mm solid ${el.color || '#333'}`}"></div>`;
    case 'box': return '';
    case 'taxSummary': return taxSummaryHTML(el, ctx);
    case 'totals': return totalsHTML(el, ctx);
    case 'pageNo': return esc(fillTokens(el.text || 'Page {page} of {pages}', { ...ctx, page: pageInfo?.page ?? 1, pages: pageInfo?.pages ?? 1 }));
    default: return '';
  }
}

export function elementClass(el) {
  return `tdp-el tdp-${el.type}${el.type === 'box' ? ' tdp-boxed' : ''}`;
}

// ---------------------------------------------------------------------------
// Which page an element prints on, and where the items table sits on a page.
// ---------------------------------------------------------------------------

const zoneOf = (el, table) => {
  const mid = el.y + el.h / 2;
  return mid < table.y ? 'above' : mid > table.y + table.h ? 'below' : 'beside';
};

// pagesByPosition is the page an element's position fixes - 'first' above the
// items table, 'last' under it - or '' where its own setting applies (no
// table, a page number, beside the table).
export function pagesByPosition(el, table) {
  if (!table || el === table || el.type === 'table' || el.type === 'pageNo') return '';
  const z = zoneOf(el, table);
  return z === 'above' ? 'first' : z === 'below' ? 'last' : '';
}

// printsOn is 'first', 'last' or 'all' for an element of the layout.
export function printsOn(el, table) {
  if (el.type === 'table') return 'all';
  return pagesByPosition(el, table) || (el.pages === 'all' || el.pages === 'last' ? el.pages : 'first');
}

const itemsTable = (layout) => (layout.elements || []).find(el => el.type === 'table');
const printsOnPage = (el, table, first, last) => {
  const on = printsOn(el, table);
  return on === 'all' || (on === 'last' ? last : first);
};

// tableBox is the table's top and bottom on a page: its designed box on the
// first / last page; a later page starts at the top margin, and a page before
// the last runs to the bottom margin - or 2 mm clear of a page number or
// other element still printing there.
function tableBox(layout, table, first, last) {
  const ph = pageDims(layout.page)[1];
  const margin = layout.page?.margin ?? 10;
  const others = (layout.elements || []).filter(el => el !== table && !el.hidden && printsOnPage(el, table, first, last));
  let top = table.y, bottom = table.y + table.h;
  if (!first) top = Math.min(top, others.filter(el => zoneOf(el, table) === 'above').reduce((t, el) => Math.max(t, el.y + el.h + 2), margin));
  if (!last) bottom = Math.max(bottom, others.filter(el => zoneOf(el, table) === 'below').reduce((b, el) => Math.min(b, el.y - 2), ph - margin));
  return { top, bottom };
}

// ---------------------------------------------------------------------------
// Pagination: measure every row once, then fill each page's table box. The
// rows left go to the last page once they fit above its footer; a page that
// takes the final rows but leaves no room for the footer hands one row on.
// design: one page, every element, the designed box (the studio canvas).
// ---------------------------------------------------------------------------

function measureTable(el, lines, fontFamily) {
  const probe = document.createElement('div');
  probe.className = 'tdp-page tdp-measure';
  probe.style.cssText = `position:absolute;left:-10000px;top:0;width:${el.w}mm;font-family:${fontFamily};`;
  probe.innerHTML = `<table class="tdp-table" style="width:${el.w}mm;font-size:${el.fontSize || 8}pt">${tableHead(el)}<tbody>${tableRowsHTML(el, lines)}</tbody></table>`;
  document.body.appendChild(probe);
  const head = probe.querySelector('thead');
  const headMM = head ? head.getBoundingClientRect().height / PX_PER_MM : 6;
  const rowsMM = [...probe.querySelectorAll('tbody tr')].map(tr => tr.getBoundingClientRect().height / PX_PER_MM);
  probe.remove();
  return { headMM, rowsMM };
}

export function paginate(layout, doc, opts = {}) {
  const table = itemsTable(layout);
  const lines = doc.lines || [];
  if (!table) return [lines];
  const { headMM, rowsMM } = measureTable(table, lines, layout.page?.font || 'Arial');
  const cap = (first, last) => {
    if (opts.design) return table.h - headMM;
    const b = tableBox(layout, table, first, last);
    return b.bottom - b.top - headMM;
  };
  const h = (i) => rowsMM[i] || 5;
  const rest = [];
  for (let i = lines.length - 1, s = 0; i >= 0; i--) rest[i] = (s += h(i));
  const pages = [];
  let i = 0;
  for (;;) {
    const first = !pages.length;
    if (i >= lines.length || rest[i] <= cap(first, true) + 0.01) {
      pages.push(lines.slice(i));
      return pages;
    }
    const room = cap(first, false);
    let j = i, used = 0;
    while (j < lines.length && (j === i || used + h(j) <= room + 0.01)) used += h(j++);
    if (j === lines.length && j - i > 1) j--;
    pages.push(lines.slice(i, j));
    i = j;
  }
}

// printContext is what tokens and field keys resolve against.
export function printContext(doc) {
  return {
    ...doc,
    brand: doc.brand || {},
    store: doc.store || {}, party: doc.party || {}, ship_to: doc.ship_to || {},
    fields: doc.fields || {}, totals: doc.totals || {},
    // Stage 71: who printed it and when, beside the server's created /
    // approved audit fields.
    audit: { ...(doc.audit || {}), printed_by: printedBy(), printed_at: printedAt() }
  };
}

function printedBy() {
  try { return localStorage.getItem('erp_username') || ''; } catch (e) { return ''; }
}

function printedAt() {
  const d = new Date();
  const p = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// renderPages returns page elements (not yet attached) for the layout;
// opts.design paints the studio's one design page.
export function renderPages(layout, doc, opts = {}) {
  const [pw, ph] = pageDims(layout.page);
  const ctx = printContext(doc);
  const table = itemsTable(layout);
  const chunks = paginate(layout, doc, opts);
  const total = chunks.length;
  const hasBrand = (layout.elements || []).some(el =>
    (el.type === 'field' && el.key === 'brand.name') || (el.type === 'text' && /\{brand\.name\}/.test(el.text || '')));
  return (opts.design ? chunks.slice(0, 1) : chunks).map((rows, i) => {
    const pageNo = i + 1;
    const first = opts.design || pageNo === 1, last = opts.design || pageNo === total;
    const page = document.createElement('div');
    page.className = 'tdp-page';
    page.style.width = `${pw}mm`;
    page.style.height = `${ph}mm`;
    page.style.fontFamily = layout.page?.font || 'Inter, Arial, sans-serif';
    let html = '';
    if (first && !hasBrand && ctx.brand.name) {
      html += `<div class="tdp-el tdp-brand-auto" style="left:${layout.page?.margin ?? 10}mm;top:3mm;width:${pw - 2 * (layout.page?.margin ?? 10)}mm;height:6mm;">${esc(ctx.brand.name)}</div>`;
    }
    (layout.elements || []).forEach(el => {
      if (el.hidden) return;
      if (!printsOnPage(el, table, first, last)) return;
      if (el.type === 'table') {
        const { top, bottom } = el === table && !opts.design ? tableBox(layout, el, first, last) : { top: el.y, bottom: el.y + el.h };
        html += `<div class="${elementClass(el)}" style="${boxStyle({ ...el, y: top, h: bottom - top, border: false, bg: '' })}"><table class="tdp-table${el.rowLines === false ? ' tdp-norules' : ''}" style="font-size:${el.fontSize || 8}pt">${tableHead(el)}<tbody>${tableRowsHTML(el, rows)}</tbody></table></div>`;
        return;
      }
      const inner = elementHTML(el, ctx, { page: pageNo, pages: total });
      if (inner === '' && !['box', 'line'].includes(el.type)) return;
      html += `<div class="${elementClass(el)}" style="${boxStyle(el)}">${inner}</div>`;
    });
    page.innerHTML = html;
    return page;
  });
}

// ---------------------------------------------------------------------------
// Printing
// ---------------------------------------------------------------------------

export async function printPages(pages, layout) {
  await tradeStylesReady;
  const [pw, ph] = pageDims(layout.page);
  let area = document.getElementById('trade-print-area');
  if (!area) {
    area = document.createElement('div');
    area.id = 'trade-print-area';
    document.body.appendChild(area);
  }
  area.innerHTML = '';
  pages.forEach(p => area.appendChild(p));
  let style = document.getElementById('trade-page-style');
  if (!style) {
    style = document.createElement('style');
    style.id = 'trade-page-style';
    document.head.appendChild(style);
  }
  style.textContent = `@page { size: ${pw}mm ${ph}mm; margin: 0; }`;
  // Wait for embedded images so none prints blank.
  await Promise.all([...area.querySelectorAll('img')].map(img => img.complete ? null : new Promise(r => { img.onload = img.onerror = r; })));
  document.body.classList.add('trade-printing');
  const done = () => {
    document.body.classList.remove('trade-printing');
    area.innerHTML = '';
    window.removeEventListener('afterprint', done);
  };
  window.addEventListener('afterprint', done);
  window.print();
  setTimeout(() => { if (document.body.classList.contains('trade-printing')) done(); }, 60000);
}

async function fetchPrintData(txn, id, store, images) {
  const qs = new URLSearchParams();
  if (store) qs.set('store', store);
  if (images) qs.set('images', '1');
  const res = await window.apiFetch(`/api/v1/print/document/${encodeURIComponent(txn)}/${encodeURIComponent(id)}?${qs}`);
  if (!res) return null;
  if (!res.ok) { await window.showApiError(res, 'Could not prepare this document for printing.'); return null; }
  return res.json();
}

// transactionsFor is which print transactions a document offers.
function transactionsFor(doctype, doc) {
  if (doctype === 'PurchaseOrder') return ['PurchaseOrder', 'VendorPO'];
  if (doctype === 'GRN') return ['GRN', 'GRNVendorCopy'];
  if (doctype === 'TransferOrder') return (doc && doc.status === 'Received') ? ['TransferIn', 'TransferOut'] : ['TransferOut', 'TransferIn'];
  const t = TRANSACTIONS.find(x => x.doctype === doctype);
  return t ? [t.key] : [];
}

// printTransactionDocument opens the print dialog: transaction (PO / Vendor
// PO, Transfer Out / In), template (best match first, or the standard one),
// the store the print is generated from, a live preview, Print, and for a PO
// the Excel download.
export async function printTransactionDocument(doctype, id, opts = {}) {
  await tradeStylesReady;
  const txns = transactionsFor(doctype, opts.doc);
  if (!txns.length) { await window.showCustomAlert?.(`${doctype} has no print layout.`, 'Print'); return; }
  const S = { txn: opts.txn && txns.includes(opts.txn) ? opts.txn : txns[0], store: '', templateId: '', data: null, images: false };

  const overlay = document.createElement('div');
  overlay.className = 'td-overlay';
  overlay.innerHTML = `
    <div class="td-dialog" role="dialog" aria-modal="true" aria-label="Print">
      <div class="td-dialog-head">
        <h2>Print</h2>
        <button type="button" class="btn btn-ghost btn-sm" data-td="close" aria-label="Close">&times;</button>
      </div>
      <div class="td-dialog-controls">
        ${txns.length > 1 ? `<label class="form-label">Print as<select class="form-input" data-td="txn">${txns.map(t => `<option value="${t}"${t === S.txn ? ' selected' : ''}>${esc(txnLabel(t))}</option>`).join('')}</select></label>` : `<div class="td-txn">${esc(txnLabel(S.txn))}</div>`}
        <label class="form-label">Template<select class="form-input" data-td="template"></select></label>
        <label class="form-label">Generating store<input type="text" class="form-input" data-td="store" placeholder="Document's own store" autocomplete="off"></label>
        <div class="td-dialog-actions">
          <button type="button" class="btn btn-outline btn-sm" data-td="design">Design templates</button>
          <button type="button" class="btn btn-outline btn-sm hidden" data-td="excel">Excel</button>
          <button type="button" class="btn btn-primary btn-sm" data-td="print">Print</button>
        </div>
      </div>
      <div class="td-preview" data-td="preview"><div class="td-loading">Preparing…</div></div>
    </div>`;
  document.body.appendChild(overlay);
  const q = (k) => overlay.querySelector(`[data-td="${k}"]`);
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  q('close').addEventListener('click', close);
  if (window.attachLinkTypeahead) window.attachLinkTypeahead(q('store'), 'Location');

  const currentLayout = () => {
    const t = (S.data?.templates || []).find(x => x.id === S.templateId);
    return parseLayout(t ? t.layout : '', S.txn);
  };
  const paint = () => {
    const layout = currentLayout();
    const pages = renderPages(layout, S.data.document);
    const prev = q('preview');
    prev.innerHTML = '';
    const [pw] = pageDims(layout.page);
    const scale = Math.min(1, (prev.clientWidth - 32) / (pw * PX_PER_MM));
    pages.forEach(p => {
      const wrap = document.createElement('div');
      wrap.className = 'td-preview-page';
      wrap.style.width = `${pw * PX_PER_MM * scale}px`;
      wrap.style.height = `${pageDims(layout.page)[1] * PX_PER_MM * scale}px`;
      p.style.transform = `scale(${scale})`;
      p.style.transformOrigin = 'top left';
      wrap.appendChild(p);
      prev.appendChild(wrap);
    });
    q('excel').classList.toggle('hidden', !(S.txn === 'VendorPO' || S.txn === 'PurchaseOrder'));
  };
  const load = async () => {
    q('preview').innerHTML = '<div class="td-loading">Preparing…</div>';
    let data = await fetchPrintData(S.txn, id, S.store, S.images);
    if (!data) { close(); return; }
    const sel = q('template');
    const templates = data.templates || [];
    if (!templates.some(t => t.id === S.templateId)) S.templateId = (templates.find(t => t.recommended) || {}).id || '';
    sel.innerHTML = templates.map(t => `<option value="${esc(t.id)}"${t.id === S.templateId ? ' selected' : ''}>${esc(t.name)}${t.recommended ? ' (best match)' : ''}</option>`).join('')
      + `<option value=""${S.templateId ? '' : ' selected'}>Standard layout (built-in)</option>`;
    S.data = data;
    const needImages = layoutUsesImages(currentLayout());
    if (needImages && !S.images) {
      S.images = true;
      data = await fetchPrintData(S.txn, id, S.store, true);
      if (!data) { close(); return; }
      S.data = data;
    }
    if (!q('store').value) q('store').placeholder = `${data.document.store.name || 'Document store'} (document's own)`;
    paint();
  };
  q('template').addEventListener('change', async () => {
    S.templateId = q('template').value;
    if (layoutUsesImages(currentLayout()) && !S.images) { S.images = true; await load(); } else paint();
  });
  const txnSel = q('txn');
  if (txnSel) txnSel.addEventListener('change', () => { S.txn = txnSel.value; S.templateId = ''; S.images = false; load(); });
  const storeChanged = () => { const v = q('store').value.trim(); if (v !== S.store) { S.store = v; load(); } };
  q('store').addEventListener('change', storeChanged);
  q('store').addEventListener('blur', storeChanged);
  q('print').addEventListener('click', async () => {
    if (!S.data) return;
    const layout = currentLayout();
    await printPages(renderPages(layout, S.data.document), layout);
  });
  q('excel').addEventListener('click', async () => {
    const res = await window.apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(id)}/excel${S.store ? `?store=${encodeURIComponent(S.store)}` : ''}`);
    if (!res) return;
    if (!res.ok) { await window.showApiError(res, 'Could not build the Excel file.'); return; }
    const blob = await res.blob();
    const cd = res.headers.get('Content-Disposition') || '';
    const m = /filename="([^"]+)"/.exec(cd);
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = m ? m[1] : 'purchase-order.xlsx';
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
  });
  q('design').addEventListener('click', async () => {
    const studio = await import('./print-template-studio.js' + VERSION_QUERY);
    close();
    studio.openPrintTemplateStudio({ txn: S.txn, templateId: S.templateId, sample: { txn: S.txn, id } });
  });
  await load();
}

window.printTransactionDocument = (doctype, id, txn) => printTransactionDocument(doctype, id, { txn });

// Printing → Document templates tab (the gallery lives in the studio module).
export async function renderPrintTemplateGallery(container) {
  const studio = await import('./print-template-studio.js' + VERSION_QUERY);
  return studio.renderPrintTemplateGallery(container);
}
