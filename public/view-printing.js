// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
function renderQZSetupPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap;">
      <div>
        <h2 style="margin: 0 0 4px; font-size: 16px;">Print Setup (QZ Tray)</h2>
        <p id="qz-status" class="page-subtitle" style="margin: 0;">Checking for QZ Tray on this PC&hellip;</p>
      </div>
      <div style="display: flex; gap: 8px; flex-wrap: wrap;">
        <button class="btn btn-outline" ${actionAttrs('qzDetectPrinters')}>Detect Printers</button>
        <button class="btn btn-outline" ${actionAttrs('qzTestPrint')}>Test Print</button>
      </div>
    </div>
    <div id="qz-detected" style="margin-top: 12px;"></div>
    <div style="margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border-color, #e5e7eb);">
      <label class="form-label" for="qz-doc-file">Print a marketplace label or invoice (PDF from Myntra, or any channel)</label>
      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
        <input type="file" id="qz-doc-file" class="form-input" accept=".pdf,.zpl,.txt,.prn" style="max-width: 340px;">
        <button class="btn btn-outline" id="qz-doc-preview-btn" type="button">Preview</button>
        <button class="btn btn-primary" ${actionAttrs('qzPrintPickedDocument')}>Print</button>
      </div>
      <p class="page-subtitle" style="margin: 6px 0 0;">
        Goes to whichever Printer has <strong>Default For = Shipping Label</strong>, exactly as the channel issued it.
      </p>
    </div>
  `;
  container.appendChild(panel);
  panel.querySelector('#qz-doc-preview-btn').addEventListener('click', qzPreviewPickedDocument);
  qzRefreshStatus();
}

// qzPreviewPickedDocument (Stage 133.3) shows the picked file as the channel
// issued it before it prints: a PDF in the browser's own viewer in a new tab
// (the page's security policy keeps PDFs out of its own frames), a ZPL or text
// file as its text - the printer's commands, which the printer draws from.
async function qzPreviewPickedDocument() {
  const input = document.getElementById('qz-doc-file');
  if (!input || !input.files || input.files.length === 0) {
    await showCustomAlert('Choose a label or invoice file first.', 'Nothing Selected');
    return;
  }
  const file = input.files[0];
  if (/\.pdf$/i.test(file.name) || file.type === 'application/pdf') {
    const url = URL.createObjectURL(file);
    if (!window.open(url, '_blank')) await showCustomAlert('The browser blocked the preview tab - allow pop-ups for this site and try again.', 'Preview');
    setTimeout(() => URL.revokeObjectURL(url), 60000);
    return;
  }
  const text = await file.text();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 760px;" role="dialog" aria-modal="true" aria-labelledby="qz-doc-preview-title">
      <div class="modal-header"><h3 class="modal-title" id="qz-doc-preview-title">${escapeHTMLText(file.name)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-qz-preview="close">×</button></div>
      <div class="modal-body">
        <p style="margin: 0 0 8px; color: var(--text-muted);">This file is the label printer's own commands; the printer draws the label from them.</p>
        <pre style="white-space: pre-wrap; font-family: monospace; font-size: 12px; max-height: 60vh; overflow: auto; margin: 0;">${escapeHTMLText(text)}</pre>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" data-qz-preview="close">Close</button>
        <button type="button" class="btn btn-primary" data-qz-preview="print">Print</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  requestAnimationFrame(() => overlay.classList.add('open'));
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-qz-preview="close"]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  overlay.querySelector('[data-qz-preview="print"]').addEventListener('click', () => { close(); qzPrintPickedDocument(); });
}

async function qzRefreshStatus() {
  const el = document.getElementById('qz-status');
  if (!el) return;
  const ok = await qzTryConnect();
  if (ok) {
    const v = window.QZPrint.version();
    el.textContent = `Connected to QZ Tray${v ? ' ' + v : ''} - printing is silent on this PC.`;
    el.style.color = 'var(--color-success, #15803d)';
  } else {
    el.textContent = qzLastError + ' Printing falls back to the browser dialog until it is running.';
    el.style.color = 'var(--color-warning, #b45309)';
  }
}

// Lists the OS printer names QZ can see. This is the value the operator
// copies into a Printer record's "OS Printer Name" field.
async function qzDetectPrinters() {
  const out = document.getElementById('qz-detected');
  if (!out) return;
  if (!await qzTryConnect(false)) return;
  out.textContent = 'Detecting...';
  try {
    const names = await window.QZPrint.listOSPrinters();
    const list = Array.isArray(names) ? names : [names];
    if (list.length === 0) {
      out.textContent = 'QZ Tray reports no printers installed on this PC.';
      return;
    }
    let def = '';
    try { def = await window.QZPrint.getDefaultPrinter(); } catch (e) { /* optional */ }
    out.innerHTML = `
      <p class="page-subtitle" style="margin: 0 0 6px;">Copy the exact name into the matching Printer record:</p>
      <ul style="margin: 0; padding-left: 18px; line-height: 1.7;">
        ${list.map(n => `<li><code>${escapeHTMLText(n)}</code>${n === def ? ' <em>(system default)</em>' : ''}</li>`).join('')}
      </ul>`;
  } catch (err) {
    out.textContent = 'Could not list printers: ' + (err.message || err);
  }
}

// Proves the whole chain - certificate, signature, socket, driver - before an
// operator depends on it during a packing run.
async function qzTestPrint() {
  if (!await qzTryConnect(false)) return;
  const printers = await apiFetch('/api/v1/print/qz/printers');
  if (!printers || !printers.ok) {
    await showApiError(printers, 'Could not load configured printers.', 'Test Print');
    return;
  }
  const list = await printers.json();
  const target = list.find(p => p.qz_printer_name);
  if (!target) {
    await showCustomAlert(
      'No Printer record has an OS Printer Name yet. Click "Detect Printers", then edit a Printer ' +
      'under Masters and paste the exact name into "OS Printer Name".', 'Test Print');
    return;
  }
  const raw = ['ZPL', 'TSPL', 'ESC-POS'].includes((target.printer_language || '').toUpperCase());
  const items = raw
    ? [{ type: 'raw', format: 'command', flavor: 'plain',
         data: '^XA^CI28^FO30,30^A0N,40,40^FDERP test label^FS^FO30,90^A0N,28,28^FD' + new Date().toLocaleString() + '^FS^XZ' }]
    : [{ type: 'pixel', format: 'html', flavor: 'plain',
         data: '<h2>ERP test print</h2><p>' + escapeHTMLText(new Date().toLocaleString()) + '</p>' }];
  try {
    await window.QZPrint.printItems(target.qz_printer_name, items, 1);
    showToast(`Test page sent to ${target.qz_printer_name}.`, { variant: 'success' });
  } catch (err) {
    await showCustomAlert(err.message || String(err), 'Test Print Failed');
  }
}

async function qzPrintPickedDocument() {
  const input = document.getElementById('qz-doc-file');
  if (!input || !input.files || input.files.length === 0) {
    await showCustomAlert('Choose a label or invoice file first.', 'Nothing Selected');
    return;
  }
  const printed = await qzPrintMarketplaceDocument(input.files[0]);
  if (printed) input.value = '';
}

// Sticker / Barcode Printing (Stage 13.15) - Printer master creation/listing
// use the same generic doc API as Vendor/Customer/RFQ; this screen adds the
// print action (logs history, then renders a printable label sheet) and
// print-history view on top. Labels show the barcode value as clear text
// rather than a generated scannable barcode symbol/image - correctly
// implementing and verifying a real barcode symbology renderer isn't
// something that can be validated without a physical scanner in this
// environment, and shipping an unverified fake one would be worse than a
// clear text label (which is also how the rest of this app already treats
// barcodes - typed/scanned as text, e.g. the POS screen's SKU input).
let stickerSKUs = [];

// Stage 52: bulk print from a GRN/Transfer Order, and the category-based
// StickerTemplate designer. Adding a third source doctype later is one more
// entry here plus one more case in engines.ResolveDocumentStickerLines - not
// a redesign.
const BULK_STICKER_SOURCE_DOCTYPES = [
  { doctype: 'GRN', label: 'Goods Receipt (GRN)' },
  { doctype: 'TransferOrder', label: 'Transfer Order' }
];
// stickerSourceDoctypes (Stage 127.5: "any new taxn should load properly in
// print"): GRN and Transfer Order, then every other transaction the client's
// modules include whose lines carry items - a new one appears with no code.
function stickerSourceDoctypes() {
  const fixed = BULK_STICKER_SOURCE_DOCTYPES.map(d => d.doctype);
  const more = ((typeof state !== 'undefined' && state.activeDoctypes) || [])
    .filter(d => d.has_lines && d.document_type !== 'Master' && !fixed.includes(d.name))
    .map(d => ({ doctype: d.name, label: typeof getDoctypeLabel === 'function' ? getDoctypeLabel(d.name) : d.name }))
    .sort((a, b) => a.label.localeCompare(b.label));
  return [...BULK_STICKER_SOURCE_DOCTYPES, ...more];
}
let stickerSourceDoctype = BULK_STICKER_SOURCE_DOCTYPES[0].doctype;
let stickerSourceDocId = '';
let stickerSourceLines = [];    // last GET /api/v1/stickers/preview response
let stickerSourceSelected = {}; // line index -> bool, defaults to all-selected on load
let stickerSourceCopies = {};   // line index -> int, defaults to the line's own qty
// Stage 70: a template picked for a line at print time beats the template
// rules for that print only. '' = as per rules, 'standard' = the built-in label.
let stickerSourceTemplates = {}; // line index -> template id
let stickerTemplateChoices = []; // Active StickerTemplates: [{ id, name }]
const STICKER_RULE_TEXT = { 'Item': 'item', 'Item type': 'item type', 'Product type': 'product type', 'Category': 'category', 'Default': 'default' };
let stickerPrinters = [];       // Printer records, for the engine's language/DPI/agent name

async function renderStickersView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sticker Printing</h1>
    </div>
    <button type="button" class="btn btn-outline" data-sticker-templates>Templates</button>
  `;
  container.appendChild(header);
  // Stage 88.4: sticker and document templates and the template rules are on
  // Setup ▸ Printing ▸ Templates, with every other template.
  header.querySelector('[data-sticker-templates]').addEventListener('click', () => {
    currentDoctype = 'PrintTemplate'; currentSearchQuery = ''; currentTablePage = 1;
    setActiveMenu('menu-templates');
    renderView('doctype-table');
  });
  await renderStickerPrintTab(container);
}

async function renderStickerPrintTab(container) {
  const [printersRes, historyRes] = await Promise.all([
    apiFetch('/api/v1/doc/Printer'),
    apiFetch('/api/v1/stickers/history')
  ]);
  if (!printersRes || !historyRes) return;

  const printers = printersRes.ok ? await printersRes.json() : [];
  stickerPrinters = printers;
  const history = historyRes.ok ? await historyRes.json() : [];

  // Stage 69: the print history is the page; + Print Stickers opens the print
  // station (QZ status, Print from Transaction, scan) as a page of its own
  // (txn-screen.js, the standard ERP shape). Attached first: the panels look
  // their own elements up as they are built.
  const { txnPagedSection, txnRestorePane } = await loadTxnScreen();
  const printPage = document.createElement('div');
  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  container.appendChild(txnPagedSection({ id: 'sticker-print', title: 'Print history', page: 'Print stickers',
    count: history.length, newLabel: 'Print Stickers', form: printPage, list: historyPanel, heading: 'h2' }));
  txnRestorePane('sticker-print');

  renderStickerSourcePanel(printPage, printers);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-printer">Printer</label>
        <select id="sticker-printer" class="form-input" style="width: 200px;">
          <option value="">Select a printer</option>
          ${printers.map(p => `<option value="${escapeHTMLText(p.code || p.id)}">${escapeHTMLText(p.name || p.code || p.id)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-copies">Copies per SKU</label>
        <input type="number" id="sticker-copies" class="form-input" style="width: 100px;" value="1" min="1">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 180px;">
        <label class="form-label" for="sticker-reprint-reason">Reprint reason (kept in the print history)</label>
        <div style="display: flex; gap: 8px;">
          <select id="sticker-reprint-code" class="form-select hidden" style="max-width: 240px;" aria-label="Reprint reason code"></select>
          <input type="text" id="sticker-reprint-reason" class="form-input">
        </div>
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="flex: 1; margin-bottom: 0;">
        <label class="form-label" for="sticker-sku-input">Scan or Enter SKU</label>
        <input type="text" id="sticker-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <label style="display: flex; align-items: center; gap: 6px; margin-bottom: 10px; white-space: nowrap;" title="Each scan prints its sticker straight away - with the loaded document's lot when the item is on it">
        <input type="checkbox" id="sticker-print-on-scan"> Print on scan
      </label>
      <button class="btn btn-outline" id="sticker-add-btn">Add</button>
    </div>
    <div id="sticker-sku-list" style="margin-bottom: 16px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="sticker-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div style="display: flex; gap: 12px;">
      <button class="btn btn-outline" id="sticker-preview-btn" type="button">Preview</button>
      <button class="btn btn-primary" id="sticker-print-btn" style="flex: 1;">Print Stickers</button>
    </div>
  `;
  printPage.appendChild(formPanel);
  stickerReprintCodeWanted = '';
  fillStickerReprintCodes();

  // Stage 55.19: printer setup (QZ Tray detect/test, channel label PDFs) is a
  // one-time path per computer, so it sits collapsed below the daily flow.
  const setup = document.createElement('details');
  setup.className = 'sticker-printer-setup';
  setup.innerHTML = '<summary style="cursor:pointer;font-weight:600;margin:0 0 12px;">Printer setup on this computer (QZ Tray) and channel label PDFs</summary>';
  printPage.appendChild(setup);
  renderQZSetupPanel(setup);

  let historyHtml = `
    <table>
      <thead><tr><th>SKU</th><th>Barcode</th><th>Printer</th><th>Printed By</th><th>Copies</th><th>Source</th><th>Lot</th><th>Reprint Reason</th><th>Date</th><th></th></tr></thead>
      <tbody>
  `;
  historyHtml += history.length === 0
    ? `<tr><td colspan="10" style="text-align:center; color:var(--text-muted);">No print history yet. Click <b>+ Print Stickers</b>, add SKUs or load a transaction, then print.</td></tr>`
    : history.map((h, i) => `
        <tr>
          <td style="font-family: monospace;">${escapeHTMLText(h.sku)}</td>
          <td style="font-family: monospace;">${escapeHTMLText(h.barcode)}</td>
          <td>${escapeHTMLText(h.printer_code)}</td>
          <td>${escapeHTMLText(h.printed_by)}</td>
          <td>${escapeHTMLText(h.copies)}</td>
          <td>${escapeHTMLText(h.source_doc_id ? `${h.source_doctype} ${h.source_doc_id}` : '')}</td>
          <td style="font-family: monospace;">${escapeHTMLText(h.batch_no || '')}</td>
          <td>${escapeHTMLText(h.reprint_reason || '')}</td>
          <td>${escapeHTMLText(new Date(h.printed_at).toLocaleString())}</td>
          <td><button type="button" class="action-btn" data-sticker-reprint="${i}">Reprint</button></td>
        </tr>
      `).join('');
  historyHtml += `</tbody></table>`;
  historyPanel.innerHTML = historyHtml;
  historyPanel.addEventListener('click', (e) => {
    const btn = e.target.closest('[data-sticker-reprint]');
    if (btn) reprintStickerRow(history[Number(btn.dataset.stickerReprint)]);
  });

  // Stage 111.2: on by default; the choice is remembered on this computer.
  const onScan = document.getElementById('sticker-print-on-scan');
  try { onScan.checked = localStorage.getItem('sticker-print-on-scan') !== '0'; } catch (_) { onScan.checked = true; }
  onScan.addEventListener('change', () => { try { localStorage.setItem('sticker-print-on-scan', onScan.checked ? '1' : '0'); } catch (_) { /* storage off */ } });
  // Stage 133.1 (user, 2026-10-22: "I want this to be fixed. not frozen for
  // new print"): the printer and copies are remembered on this computer, so
  // a new print opens ready; either can be changed for any one print, and a
  // change is kept for the next.
  const printerSel = document.getElementById('sticker-printer');
  const copiesInput = document.getElementById('sticker-copies');
  let keptPrinter = '', keptCopies = 0;
  try {
    keptPrinter = localStorage.getItem('sticker-printer') || '';
    keptCopies = parseInt(localStorage.getItem('sticker-copies'), 10) || 0;
  } catch (_) { /* storage off */ }
  printerSel.value = stickerStartPrinter(printers, keptPrinter);
  if (keptCopies > 0) copiesInput.value = keptCopies;
  printerSel.addEventListener('change', () => { try { localStorage.setItem('sticker-printer', printerSel.value); } catch (_) { /* storage off */ } });
  copiesInput.addEventListener('change', () => {
    const n = parseInt(copiesInput.value, 10);
    if (n > 0) { try { localStorage.setItem('sticker-copies', String(n)); } catch (_) { /* storage off */ } }
  });
  document.getElementById('sticker-add-btn').addEventListener('click', addStickerSKU);
  document.getElementById('sticker-sku-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addStickerSKU();
    }
  });
  document.getElementById('sticker-print-btn').addEventListener('click', printStickers);
  document.getElementById('sticker-preview-btn').addEventListener('click', previewStickers);

  // Stage 88.5.5: an MRP change hands its items over for the sticker reprint.
  let prefill = null;
  try { prefill = JSON.parse(sessionStorage.getItem('erp_sticker_prefill') || 'null'); sessionStorage.removeItem('erp_sticker_prefill'); } catch (_) { /* storage off */ }
  if (prefill && Array.isArray(prefill.skus) && prefill.skus.length) {
    stickerSKUs = [...new Set(prefill.skus)];
    document.getElementById('sticker-print-on-scan').checked = false;
    document.getElementById('sticker-reprint-reason').value = prefill.reason || '';
    stickerReprintCodeWanted = prefill.reason_code || '';
    window.txnShowPane('sticker-print:new');
  }
  renderStickerSKUList();
}

// stickerStartPrinter (Stage 133.1) is the printer a new sticker print opens
// on: the one last used on this computer, else the Default For Sticker printer
// of this store (or of no store in particular), else the only Active printer.
function stickerStartPrinter(printers, kept) {
  const codeOf = p => p.code || p.id;
  const active = printers.filter(p => (p.status || 'Active') === 'Active');
  if (kept && active.some(p => codeOf(p) === kept)) return kept;
  const store = (window.erpWorkingStore || {}).code || '';
  const here = active.filter(p => p.print_role === 'Sticker' && (!store || !p.location || p.location === store));
  const pick = here.find(p => store && p.location === store) || here[0] || (active.length === 1 ? active[0] : null);
  return pick ? codeOf(pick) : '';
}

// reprintStickerRow (Stage 133.2) opens the print page set up to print a
// history row again, on its printer and copies. A row printed from a document
// loads that document with only its SKU and lot ticked; a scanned row goes to
// the scan list. The template follows today's rules, so a reprint matches a
// fresh print; it can still be changed before printing.
async function reprintStickerRow(h) {
  if (!h) return;
  window.txnShowPane('sticker-print:new');
  const printerSel = document.getElementById('sticker-printer');
  if (h.printer_code && [...printerSel.options].some(o => o.value === h.printer_code)) printerSel.value = h.printer_code;
  const copies = h.copies > 0 ? h.copies : 1;
  if (h.source_doctype && h.source_doc_id && stickerSourceDoctypes().some(d => d.doctype === h.source_doctype)) {
    const doctypeSelect = document.getElementById('sticker-source-doctype');
    doctypeSelect.value = h.source_doctype;
    doctypeSelect.dispatchEvent(new Event('change'));
    document.getElementById('sticker-source-doc').value = h.source_doc_id;
    await loadStickerSourcePreview();
    let found = false;
    stickerSourceLines.forEach((l, i) => {
      const match = String(l.sku) === h.sku && (l.batch_no || '') === (h.batch_no || '');
      stickerSourceSelected[i] = match;
      if (match) { stickerSourceCopies[i] = copies; found = true; }
    });
    renderStickerSourceLines();
    if (!found && stickerSourceLines.length) {
      const errorEl = document.getElementById('sticker-source-error');
      errorEl.textContent = `${h.sku}${h.batch_no ? ` (lot ${h.batch_no})` : ''} is no longer on ${h.source_doc_id}. Tick the line to print, or scan the item below.`;
      errorEl.classList.remove('hidden');
    }
  } else {
    stickerSKUs = [h.sku];
    document.getElementById('sticker-copies').value = copies;
    document.getElementById('sticker-print-on-scan').checked = false;
    renderStickerSKUList();
  }
  document.getElementById('sticker-reprint-reason').focus();
}

// Stage 52: "Print from Transaction" - load a GRN/Transfer Order's own lines
// instead of scanning SKUs by hand. Shares the Printer/Copies/Reprint Reason
// inputs from the manual panel below it (one printer choice for the whole
// screen), and shares printStickers' printStickerRun path (silent thermal print,
// else the browser print dialog).
function renderStickerSourcePanel(container, printers) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Print from Transaction</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-source-doctype">Module</label>
        <select id="sticker-source-doctype" class="form-input" style="width: 200px;">
          ${stickerSourceDoctypes().map(d => `<option value="${d.doctype}">${escapeHTMLText(d.label)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 200px;">
        <label class="form-label" for="sticker-source-doc">Document</label>
        <input type="text" id="sticker-source-doc" class="form-input" placeholder="Search by number..." autocomplete="off">
      </div>
      <button class="btn btn-outline" id="sticker-source-load-btn" type="button">Load</button>
    </div>
    <div id="sticker-source-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div id="sticker-source-lines"></div>
  `;
  container.appendChild(panel);

  const doctypeSelect = document.getElementById('sticker-source-doctype');
  doctypeSelect.value = stickerSourceDoctype;
  const docInput = document.getElementById('sticker-source-doc');
  const attachSourcePicker = () => {
    // 130.2: the latest documents first, so a click shows today's receipts.
    // 134.1b: listed by the sticker screen's own search, so a role that prints
    // stickers but cannot open receipts (Store Manager, Cashier) still finds them.
    const doctype = doctypeSelect.value;
    attachLinkTypeahead(docInput, doctype, {
      noSetupHint: true, noQuickCreate: true, keepCode: true, showAllOnFocus: true,
      source: async (q, { browse }) => {
        const res = await apiFetch(`/api/v1/stickers/documents?source_doctype=${encodeURIComponent(doctype)}&q=${encodeURIComponent(q || '')}&limit=${browse ? 25 : 10}`);
        return res && res.ok ? res.json() : null;
      },
      titleFn: d => [d.posting_date, d.location, d.status].filter(Boolean).join(' · ')
    });
  };
  attachSourcePicker();

  doctypeSelect.addEventListener('change', () => {
    stickerSourceDoctype = doctypeSelect.value;
    docInput.value = '';
    stickerSourceLines = [];
    document.getElementById('sticker-source-lines').innerHTML = '';
    attachSourcePicker();
  });
  docInput.addEventListener('change', loadStickerSourcePreview);
  document.getElementById('sticker-source-load-btn').addEventListener('click', loadStickerSourcePreview);

  // Stage 94: "Print stickers" on a posted child GRN opens here with that
  // receipt loaded and only that child's lines ticked.
  let pending = null;
  try {
    pending = JSON.parse(sessionStorage.getItem('grn-child-stickers') || 'null');
    sessionStorage.removeItem('grn-child-stickers');
  } catch (_) { /* storage can be disabled */ }
  if (pending && pending.id) {
    // Stage 129: open the print page itself, not the history behind it.
    window.txnShowPane('sticker-print:new');
    doctypeSelect.value = 'GRN';
    stickerSourceDoctype = 'GRN';
    attachSourcePicker();
    docInput.value = pending.id;
    loadStickerSourcePreview().then(() => {
      if (!Array.isArray(pending.skus) || !pending.skus.length) return;
      stickerSourceLines.forEach((l, i) => { stickerSourceSelected[i] = pending.skus.includes(l.sku); });
      renderStickerSourceLines();
    });
  }
}

async function loadStickerSourcePreview() {
  const errorEl = document.getElementById('sticker-source-error');
  errorEl.classList.add('hidden');
  const doctype = document.getElementById('sticker-source-doctype').value;
  let docId = document.getElementById('sticker-source-doc').value.trim();
  stickerSourceDoctype = doctype;
  if (!docId) return;
  const resolved = await resolveStickerSourceDoc(doctype, docId);
  if (resolved.error) {
    errorEl.textContent = resolved.error;
    errorEl.classList.remove('hidden');
    stickerSourceLines = [];
    renderStickerSourceLines();
    return;
  }
  docId = resolved.id;
  stickerSourceDocId = docId;

  const [res, choices] = await Promise.all([
    apiFetch(`/api/v1/stickers/preview?source_doctype=${encodeURIComponent(doctype)}&source_doc_id=${encodeURIComponent(docId)}`),
    loadStickerTemplateChoices()
  ]);
  if (!res) return;
  stickerTemplateChoices = choices;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Could not load that document.');
    errorEl.classList.remove('hidden');
    stickerSourceLines = [];
    renderStickerSourceLines();
    return;
  }
  stickerSourceLines = await res.json();
  if (stickerSourceLines.length === 0) {
    errorEl.textContent = 'That document has no stickerable lines (nothing accepted, or every line was rejected/damaged).';
    errorEl.classList.remove('hidden');
  }
  stickerSourceSelected = {};
  stickerSourceCopies = {};
  stickerSourceTemplates = {};
  stickerSourceLines.forEach((l, i) => {
    stickerSourceSelected[i] = true;
    stickerSourceCopies[i] = l.qty;
  });
  renderStickerSourceLines();
}

// Active templates, then the built-in designs not saved yet (Stage 113.2).
async function loadStickerTemplateChoices() {
  const [tplRes, E] = await Promise.all([apiFetch('/api/v1/doc/StickerTemplate'), loadStickerEngine()]);
  const docs = tplRes && tplRes.ok ? await tplRes.json() : [];
  return [
    ...docs.filter(t => (t.status || 'Active') === 'Active').map(t => ({ id: t.id, name: t.name || t.code || t.id })),
    ...E.unsavedDesigns(docs).map(d => ({ id: `design:${d.id}`, name: `${d.label} (built-in)` }))
  ];
}

// Stage 119.5: a pick kept as the Product type rule. The lines it now applies
// to show that rule instead of a one-off choice; an Item or Item type rule
// still comes first, as on Template rules.
async function keepStickerTemplateFor(types, templateId) {
  const { rememberProductTypeTemplate } = await loadStickerStudio();
  const name = (stickerTemplateChoices.find(t => t.id === templateId) || {}).name || templateId;
  const kept = [];
  for (const pt of types) {
    if (!await rememberProductTypeTemplate(pt, templateId)) break;
    kept.push(pt);
  }
  if (!kept.length) return;
  stickerSourceLines.forEach((l, i) => {
    if (!kept.includes(l.product_type) || l.template_rule === 'Item' || l.template_rule === 'Item type') return;
    Object.assign(l, { template_id: templateId, template_name: name, template_rule: 'Product type' });
    if (stickerSourceTemplates[i] === templateId) stickerSourceTemplates[i] = '';
  });
  window.showToast?.(`${kept.join(', ')} items now print with ${name}. Change it here or in Template rules.`, { variant: 'success' });
  renderStickerSourceLines();
}

// A built-in design picked for a print is saved once as a template of the
// client's own, then used like any other; '' (rules decide) if it could not be.
async function pickStickerChoice(value) {
  if (!value.startsWith('design:')) return value;
  const E = await loadStickerEngine();
  const id = await E.ensureDesignTemplate(value.slice('design:'.length));
  if (id) stickerTemplateChoices = await loadStickerTemplateChoices();
  return id;
}

// Stage 111.1: "00048" or "26-27/00048" finds GRN/HO/26-27/00048 when only one
// document ends that way; several or none say so in words.
async function resolveStickerSourceDoc(doctype, text) {
  const res = await apiFetch(`/api/v1/stickers/documents?source_doctype=${encodeURIComponent(doctype)}&q=${encodeURIComponent(text)}&limit=20`);
  if (!res || !res.ok) return { id: text };
  const rows = (await res.json()) || [];
  const lower = text.toLowerCase();
  const idOf = d => String(d.id || d.code || '');
  const exact = rows.find(d => idOf(d).toLowerCase() === lower || String(d.code || '').toLowerCase() === lower);
  if (exact) return { id: idOf(exact) };
  const ends = rows.filter(d => idOf(d).toLowerCase().endsWith(lower));
  const pool = ends.length ? ends : rows;
  if (pool.length === 1) {
    document.getElementById('sticker-source-doc').value = idOf(pool[0]);
    return { id: idOf(pool[0]) };
  }
  const what = doctype === 'GRN' ? 'goods receipt' : 'document';
  if (pool.length === 0) return { error: `No ${what} has "${text}" in its number. Click the box to see the latest ones and pick one.` };
  return { error: `${pool.length} ${what}s have "${text}" in their number (${pool.slice(0, 4).map(idOf).join(', ')}${pool.length > 4 ? ', ...' : ''}). Type more of the number or pick one from the list.` };
}

// Lines are addressed by their index in stickerSourceLines, not by SKU: a SKU
// received on two lots is two lines (the backend merges only within a lot), so
// SKU-keyed state made the two rows share one checkbox and one copy count, and
// a per-row Print printed both. Index is stable for as long as a preview is on
// screen, and every reload resets this state anyway.
function renderStickerSourceLines() {
  const el = document.getElementById('sticker-source-lines');
  if (!el) return;
  if (stickerSourceLines.length === 0) {
    el.innerHTML = '';
    return;
  }
  const anyBatch = stickerSourceLines.some(l => l.batch_no);
  const choiceOptions = (selected) => [{ id: 'standard', name: 'Standard label' }, ...stickerTemplateChoices]
    .map(t => `<option value="${escapeHTMLText(t.id)}" ${t.id === selected ? 'selected' : ''}>${escapeHTMLText(t.name)}</option>`).join('');
  const values = stickerSourceLines.map((l, i) => stickerSourceTemplates[i] || '');
  const runValue = values.every(v => v === values[0]) ? values[0] : '__mixed';
  // Stage 119.5: a saved template picked here can be kept as the Product type rule.
  const keepable = v => v && v !== 'standard' && v !== '__mixed';
  const allTypes = [...new Set(stickerSourceLines.map(l => l.product_type).filter(Boolean))];
  const keepTitle = types => `Print every ${types} item with this template from now on (the Product type rule in Template rules)`;
  el.innerHTML = `
    <label class="form-label" style="display: flex; align-items: center; gap: 8px; margin: 4px 0 8px;">
      Template for every line
      <select id="sticker-source-template-all" class="form-input" style="width: auto;">
        ${runValue === '__mixed' ? '<option value="__mixed" selected>Chosen per line</option>' : ''}
        <option value="" ${runValue === '' ? 'selected' : ''}>As per template rules</option>
        ${choiceOptions(runValue)}
      </select>
      ${keepable(runValue) && allTypes.length ? `<button type="button" class="action-btn" id="sticker-source-keep-all" title="${escapeHTMLText(keepTitle(allTypes.join(', ')))}">Keep for ${escapeHTMLText(allTypes.join(', '))}</button>` : ''}
    </label>
    <table style="margin-top: 4px;">
      <thead><tr><th></th><th>SKU</th><th>Name</th>${anyBatch ? '<th>Batch/Lot</th>' : ''}<th>Category</th><th>Template</th><th>Copies</th><th></th></tr></thead>
      <tbody>
        ${stickerSourceLines.map((l, i) => `
          <tr>
            <td><input type="checkbox" data-source-select="${i}" ${stickerSourceSelected[i] ? 'checked' : ''} aria-label="Print ${escapeHTMLText(l.sku)}"></td>
            <td style="font-family: monospace;">${escapeHTMLText(l.sku)}</td>
            <td>${escapeHTMLText(l.name || '')}</td>
            ${anyBatch ? `<td style="font-family: monospace;">${escapeHTMLText(l.batch_no || '')}</td>` : ''}
            <td>${escapeHTMLText(l.category || '')}</td>
            <td><select data-source-template="${i}" class="form-input" style="min-width: 160px;" aria-label="Template for ${escapeHTMLText(l.sku)}" title="Rule that picked it: ${escapeHTMLText(l.template_rule || 'Standard')}">
              <option value="">${escapeHTMLText(l.template_name)} (${escapeHTMLText(STICKER_RULE_TEXT[l.template_rule] || 'no rule')})</option>
              ${choiceOptions(values[i])}
            </select>${keepable(values[i]) && l.product_type ? ` <button type="button" class="action-btn" data-source-keep="${i}" title="${escapeHTMLText(keepTitle(l.product_type))}">Keep for ${escapeHTMLText(l.product_type)}</button>` : ''}</td>
            <td><input type="number" min="1" data-source-copies="${i}" class="form-input" style="width: 70px;" aria-label="Copies of ${escapeHTMLText(l.sku)}" value="${escapeHTMLText(stickerSourceCopies[i] ?? l.qty)}"></td>
            <td><button class="action-btn" data-source-print-one="${i}">Print</button></td>
          </tr>
        `).join('')}
      </tbody>
    </table>
    <div style="display: flex; gap: 12px; margin-top: 12px;">
      <button class="btn btn-outline" id="sticker-source-preview-btn" type="button">Preview</button>
      <button class="btn btn-primary" id="sticker-source-print-btn">Print Selected</button>
    </div>
  `;
  el.querySelectorAll('[data-source-select]').forEach(cb => {
    cb.addEventListener('change', () => {
      stickerSourceSelected[Number(cb.getAttribute('data-source-select'))] = cb.checked;
    });
  });
  el.querySelectorAll('[data-source-copies]').forEach(inp => {
    inp.addEventListener('change', () => {
      const n = parseInt(inp.value, 10);
      stickerSourceCopies[Number(inp.getAttribute('data-source-copies'))] = n > 0 ? n : 1;
    });
  });
  el.querySelectorAll('[data-source-template]').forEach(sel => {
    sel.addEventListener('change', async () => {
      sel.disabled = true;
      stickerSourceTemplates[Number(sel.getAttribute('data-source-template'))] = await pickStickerChoice(sel.value);
      renderStickerSourceLines();
    });
  });
  document.getElementById('sticker-source-template-all').addEventListener('change', async e => {
    if (e.target.value === '__mixed') return;
    e.target.disabled = true;
    const value = await pickStickerChoice(e.target.value);
    stickerSourceLines.forEach((l, i) => { stickerSourceTemplates[i] = value; });
    renderStickerSourceLines();
  });
  el.querySelectorAll('[data-source-keep]').forEach(btn => btn.addEventListener('click', () => {
    const i = Number(btn.getAttribute('data-source-keep'));
    keepStickerTemplateFor([stickerSourceLines[i].product_type], stickerSourceTemplates[i]);
  }));
  document.getElementById('sticker-source-keep-all')?.addEventListener('click', () => keepStickerTemplateFor(allTypes, runValue));
  el.querySelectorAll('[data-source-print-one]').forEach(btn => {
    btn.addEventListener('click', () => printStickerSourceLines([Number(btn.getAttribute('data-source-print-one'))]));
  });
  const selectedLines = () => stickerSourceLines.map((l, i) => i).filter(i => stickerSourceSelected[i]);
  document.getElementById('sticker-source-print-btn').addEventListener('click', () => printStickerSourceLines(selectedLines()));
  document.getElementById('sticker-source-preview-btn').addEventListener('click', async () => {
    const indexes = selectedLines();
    const body = stickerSourceRequest(indexes);
    if (body && await previewStickerRequest(body, 1, document.getElementById('sticker-source-error'))) printStickerSourceLines(indexes);
  });
}

// previewStickerRequest (Stage 133.3) asks for a print's labels without
// logging them (`preview`), shows them as they will print, and says whether
// the user pressed Print there.
async function previewStickerRequest(body, copies, errorEl) {
  const res = await apiFetch('/api/v1/stickers/print', { method: 'POST', body: JSON.stringify({ ...body, preview: true }) });
  if (!res) return false;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Could not prepare the preview.';
    errorEl.classList.remove('hidden');
    return false;
  }
  const { previewStickerLabels } = await loadStickerEngine();
  const printer = stickerPrinters.find(p => (p.code || p.id) === body.printer_code) || null;
  return previewStickerLabels(data, { printer, copies, apiFetch });
}

// stickerSourceRequest is the request for the chosen document lines - the
// same for Print and Preview - or null with the reason shown.
function stickerSourceRequest(indexes) {
  const errorEl = document.getElementById('sticker-source-error');
  errorEl.classList.add('hidden');
  if (!indexes || indexes.length === 0) {
    errorEl.textContent = 'Select at least one line first.';
    errorEl.classList.remove('hidden');
    return null;
  }
  const printerCode = document.getElementById('sticker-printer').value;
  if (!printerCode) {
    errorEl.textContent = 'Select a printer first.';
    errorEl.classList.remove('hidden');
    return null;
  }
  const lines = indexes
    .map(i => stickerSourceLines[i])
    .filter(Boolean)
    .map((l, n) => ({
      sku: l.sku,
      batch_no: l.batch_no || '',
      copies: stickerSourceCopies[indexes[n]] || l.qty || 1,
      template_id: stickerSourceTemplates[indexes[n]] || ''
    }));
  return {
    lines, printer_code: printerCode, reprint_reason: stickerReprintReason(),
    source_doctype: stickerSourceDoctype, source_doc_id: stickerSourceDocId
  };
}

// indexes are positions in stickerSourceLines. Sent as the `lines` request
// field (sku + batch_no + copies per line) so the server can address the exact
// lot's line; the older `skus`/`copies_override` fields are still accepted by
// both endpoints for the manual flow and any existing client.
async function printStickerSourceLines(indexes) {
  const errorEl = document.getElementById('sticker-source-error');
  const body = stickerSourceRequest(indexes);
  if (!body) return;
  const printerCode = body.printer_code;
  const res = await apiFetch('/api/v1/stickers/print', { method: 'POST', body: stickerPrintBody(body) });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to print stickers.';
    errorEl.classList.remove('hidden');
    return;
  }
  stickerPendingPrint = null;
  await printStickerRun(data, { printerCode, copies: 1, jobRef: stickerSourceDocId });
  (await loadTxnScreen()).txnDone('sticker-print');
  renderView('stickers');
}

function addStickerSKU() {
  const input = document.getElementById('sticker-sku-input');
  const sku = input.value.trim();
  if (!sku) return;
  const onScan = document.getElementById('sticker-print-on-scan');
  if (onScan && onScan.checked) {
    input.value = '';
    input.focus();
    printScannedSticker(sku);
    return;
  }
  if (!stickerSKUs.includes(sku)) stickerSKUs.push(sku);
  input.value = '';
  input.focus();
  renderStickerSKUList();
}

// Stage 111.2: one scan, one print. With a document loaded and the item on it,
// the sticker carries that line's lot; otherwise it prints from the item.
// The station stays open for the next scan.
async function printScannedSticker(code) {
  const errorEl = document.getElementById('sticker-form-error');
  const listEl = document.getElementById('sticker-sku-list');
  errorEl.classList.add('hidden');
  const printerCode = document.getElementById('sticker-printer').value;
  if (!printerCode) {
    errorEl.textContent = 'Select a printer first, then scan again.';
    errorEl.classList.remove('hidden');
    return;
  }
  const copies = parseInt(document.getElementById('sticker-copies').value, 10) || 1;
  const { body, line } = stickerScanBody(code, printerCode, copies);
  const res = await apiFetch('/api/v1/stickers/print', { method: 'POST', body: stickerPrintBody(body) });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = `${code}: ${data.error || 'could not print.'}`;
    errorEl.classList.remove('hidden');
    return;
  }
  stickerPendingPrint = null;
  await printStickerRun(data, { printerCode, copies: line ? 1 : copies, jobRef: line ? stickerSourceDocId : '' });
  if (listEl) listEl.textContent = `Printed ${copies} × ${line ? line.sku : code}${line && line.batch_no ? ` (lot ${line.batch_no})` : ''}. Scan the next one.`;
  document.getElementById('sticker-sku-input').focus();
}

// stickerScanBody is one scan's request: with a document loaded and the item
// on it, that line and its lot; otherwise the item itself.
function stickerScanBody(code, printerCode, copies) {
  const reprintReason = stickerReprintReason();
  const lower = code.toLowerCase();
  const idx = stickerSourceLines.findIndex(l => String(l.sku).toLowerCase() === lower || String(l.barcode || '').toLowerCase() === lower);
  const line = idx >= 0 ? stickerSourceLines[idx] : null;
  const body = line
    ? { lines: [{ sku: line.sku, batch_no: line.batch_no || '', copies, template_id: stickerSourceTemplates[idx] || '' }],
        printer_code: printerCode, reprint_reason: reprintReason, source_doctype: stickerSourceDoctype, source_doc_id: stickerSourceDocId }
    : { skus: [code], printer_code: printerCode, reprint_reason: reprintReason, copies };
  return { body, line };
}

// previewStickers (Stage 133.3) previews the scan list - or, when the list is
// empty (Print on scan), what is typed in the scan box, as that scan would
// print. Print in the preview prints it the same way.
async function previewStickers() {
  const errorEl = document.getElementById('sticker-form-error');
  errorEl.classList.add('hidden');
  const printerCode = document.getElementById('sticker-printer').value;
  const copies = parseInt(document.getElementById('sticker-copies').value, 10) || 1;
  const typed = document.getElementById('sticker-sku-input').value.trim();
  if (!printerCode) {
    errorEl.textContent = 'Select a printer first.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (stickerSKUs.length === 0 && !typed) {
    errorEl.textContent = 'Scan or type a SKU first, then Preview.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (stickerSKUs.length) {
    const body = { skus: stickerSKUs, printer_code: printerCode, reprint_reason: stickerReprintReason(), copies };
    if (await previewStickerRequest(body, copies, errorEl)) printStickers();
    return;
  }
  const { body, line } = stickerScanBody(typed, printerCode, copies);
  if (await previewStickerRequest(body, line ? 1 : copies, errorEl)) {
    document.getElementById('sticker-sku-input').value = '';
    printScannedSticker(typed);
  }
}

function removeStickerSKU(sku) {
  stickerSKUs = stickerSKUs.filter(s => s !== sku);
  renderStickerSKUList();
}

function renderStickerSKUList() {
  const listEl = document.getElementById('sticker-sku-list');
  if (!listEl) return;
  listEl.innerHTML = stickerSKUs.length === 0
    ? 'No SKUs added yet.'
    : stickerSKUs.map(sku => `${escapeHTMLText(sku)} <button class="action-btn action-btn-danger" style="padding: 2px 8px;" data-remove-sticker-sku="${escapeHTMLText(sku)}">x</button>`).join(' &nbsp; ');
  listEl.querySelectorAll('[data-remove-sticker-sku]').forEach(button => {
    button.addEventListener('click', () => removeStickerSKU(button.dataset.removeStickerSku));
  });
}

async function printStickers() {
  const errorEl = document.getElementById('sticker-form-error');
  errorEl.classList.add('hidden');

  const printerCode = document.getElementById('sticker-printer').value;
  const copies = parseInt(document.getElementById('sticker-copies').value, 10) || 1;
  const reprintReason = stickerReprintReason();

  if (!printerCode) {
    errorEl.textContent = 'Select a printer first.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (stickerSKUs.length === 0) {
    errorEl.textContent = 'Add at least one SKU first.';
    errorEl.classList.remove('hidden');
    return;
  }

  // Stage 58: one request validates the SKUs, runs the DEVICE-0298 printer
  // check and writes the sticker print log; the engine then decides whether
  // the labels go silently to a thermal printer or to the print dialog.
  const res = await apiFetch('/api/v1/stickers/print', {
    method: 'POST',
    body: stickerPrintBody({ skus: stickerSKUs, printer_code: printerCode, reprint_reason: reprintReason, copies })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to print stickers.';
    errorEl.classList.remove('hidden');
    return;
  }
  stickerPendingPrint = null;

  await printStickerRun(data, { printerCode, copies });
  stickerSKUs = [];
  (await loadTxnScreen()).txnDone('sticker-print');
  renderView('stickers');
}

// Stage 52: the value a template element's `field` reads off a resolved
// label - mirrors engines.StickerFieldText (engines/stickers.go) exactly, so
// the designer canvas, the print-preview sheet, and the ZPL a thermal
// printer actually produces all agree on what a given field prints as.
function stickerFieldText(field, textLiteral, label) {
  switch (field) {
    case 'static': return textLiteral || '';
    case 'sku': return label.sku || '';
    case 'name': return label.name || '';
    case 'hsn_code': return label.hsn_code || '';
    case 'category': return label.category || '';
    case 'batch_no': return label.batch_no || '';
    case 'expiry_date': return label.expiry_date || '';
    case 'mfg_date': return label.mfg_date || '';
    case 'qty': return label.qty ? String(label.qty) : '';
    case 'source_doc': return label.source_doc_id || '';
    case 'barcode': return label.barcode || '';
    default: return '';
  }
}

// Renders one StickerElement as an absolutely-positioned, mm-sized child -
// shared between the live print sheet (renderPrintSheet) and the designer
// canvas's own preview mode, so "what you designed" and "what prints" use
// literally the same markup function.
function renderStickerElementHTML(el, label) {
  const baseStyle = `position:absolute; left:${el.x_mm || 0}mm; top:${el.y_mm || 0}mm; width:${el.w_mm || 0}mm; height:${el.h_mm || 0}mm; overflow:hidden;`;
  if (el.field === 'barcode') {
    const content = label.barcode_svg || escapeHTMLText(label.barcode || '');
    return `<div class="sticker-el sticker-el-barcode" style="${baseStyle}">${content}</div>`;
  }
  const text = stickerFieldText(el.field, el.text, label);
  if (!text) return '';
  const align = el.align === 'center' ? 'center' : el.align === 'right' ? 'right' : 'left';
  const style = `${baseStyle} font-size:${el.font_size_mm || 3.5}mm; text-align:${align}; font-weight:${el.bold ? 700 : 400};`;
  return `<div class="sticker-el" style="${style}">${escapeHTMLText(text)}</div>`;
}

// renderPrintSheet is the browser @media print fallback for both the manual
// SKU-scan flow (a flat `copies` applies to every label) and the Stage 52
// document-driven flow (each label carries its own resolved `qty`, which
// wins when present - mirrors BuildStickerPayload's identical precedence in
// engines/qz_payload.go). A label with no resolved template (template
// unconfigured for its category, or none at all) still gets the original
// fixed 3-line layout untouched. Labels arrive pre-sorted by category
// (PrintStickersForDocument), so marking the last copy of each category run
// with a page break is enough to make a mixed-category batch tear apart as
// separate per-category stacks.
// Stage 58: the Templates tab is the Sticker Studio gallery (public/sticker-studio.js);
// the studio itself opens full-screen over the app. Both are imported with this
// module's own ?v= query so a deploy refreshes them together with this screen.
const PRINTING_MODULE_QUERY = new URL(import.meta.url).search;
function loadStickerStudio() { return import('./sticker-studio.js' + PRINTING_MODULE_QUERY); }
function loadStickerEngine() { return import('./sticker-engine.js' + PRINTING_MODULE_QUERY); }
// Stage 69: the Print tab's list / New pages.
function loadTxnScreen() { return import('./txn-screen.js' + PRINTING_MODULE_QUERY); }

// 134.1c: the Reprint reason codes beside the note, shown when the client has
// any. stickerReprintReason is the one reprint_reason the server reads -
// "CODE", "CODE: note" or the note alone; with reasons.codes_required on, a
// note alone is refused, so the codes list is what a reprint needs.
let stickerReprintCodeWanted = ''; // a hand-over's code (an MRP change: RPT-MRP), picked once the list is in
async function fillStickerReprintCodes() {
  const sel = document.getElementById('sticker-reprint-code');
  if (!sel) return;
  const picker = await import('./reason-picker.js' + PRINTING_MODULE_QUERY).catch(() => null);
  const reasons = picker ? await picker.loadReasons('Reprint') : null;
  if (!reasons || !(reasons.codes || []).length || !sel.isConnected) return;
  const value = reasons.codes.some(c => c.code === stickerReprintCodeWanted) ? stickerReprintCodeWanted : '';
  sel.outerHTML = picker.reasonSelectHTML(reasons, { value, attrs: 'id="sticker-reprint-code" style="max-width: 240px;"', label: 'Reprint reason code' });
  document.getElementById('sticker-reprint-reason').placeholder = 'note (optional)';
}

function stickerReprintReason() {
  const code = (document.getElementById('sticker-reprint-code') || {}).value || '';
  const note = document.getElementById('sticker-reprint-reason').value.trim();
  return code ? (note ? `${code}: ${note}` : code) : note;
}

// Every sticker print ends here: the labels came back from
// POST /api/v1/stickers/print (validated and written to the print log), and the
// engine draws them with the same renderer the studio shows - silently to a
// TSC/Zebra printer through the print agent when it can, else the browser
// print dialog (roll pages or an A4 sticker sheet).
async function printStickerRun(labels, { printerCode, copies = 1, jobRef = '' } = {}) {
  const { printStickerLabels } = await loadStickerEngine();
  const printer = stickerPrinters.find(p => (p.code || p.id) === printerCode) || null;
  await printStickerLabels(labels, { printer, copies, apiFetch, jobRef });
}

// HR Foundation (Stage 13.13a, MB 16.3) - Employee is a Master-type doctype
// so it already gets a full CRUD screen for free under Master Definition;
// this screen covers Attendance, Leave, and the Payroll Export, which
// aren't master data and need their own UI.


export { renderQZSetupPanel, qzRefreshStatus, qzDetectPrinters, qzTestPrint, qzPrintPickedDocument, renderStickersView, renderStickerPrintTab, renderStickerSourcePanel, loadStickerSourcePreview, renderStickerSourceLines, printStickerSourceLines, addStickerSKU, removeStickerSKU, renderStickerSKUList, printStickers, stickerFieldText, renderStickerElementHTML, printStickerRun };

// Stage 55.19: a print request carries a key that survives a failed or
// timed-out attempt at the same selection, so pressing Print again re-sends
// the same key and the server writes the print history once. Cleared on
// success; a different selection gets a new key.
let stickerPendingPrint = null;
function stickerPrintBody(payload) {
  const body = JSON.stringify(payload);
  if (!stickerPendingPrint || stickerPendingPrint.body !== body) {
    stickerPendingPrint = { body, key: (crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`) };
  }
  return JSON.stringify({ ...payload, request_key: stickerPendingPrint.key });
}
