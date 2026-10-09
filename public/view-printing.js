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
        <button class="btn btn-primary" ${actionAttrs('qzPrintPickedDocument')}>Print</button>
      </div>
      <p class="page-subtitle" style="margin: 6px 0 0;">
        Goes to whichever Printer has <strong>Default For = Shipping Label</strong>, exactly as the channel issued it.
      </p>
    </div>
  `;
  container.appendChild(panel);
  qzRefreshStatus();
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
let currentStickerTab = 'print';
const STICKER_TABS = [
  { id: 'print', label: 'Print' },
  { id: 'templates', label: 'Templates' }
];
let stickerSourceDoctype = BULK_STICKER_SOURCE_DOCTYPES[0].doctype;
let stickerSourceDocId = '';
let stickerSourceLines = [];    // last GET /api/v1/stickers/preview response
let stickerSourceSelected = {}; // line index -> bool, defaults to all-selected on load
let stickerSourceCopies = {};   // line index -> int, defaults to the line's own qty
let stickerPrinters = [];       // Printer records, for the engine's language/DPI/agent name

async function renderStickersView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sticker Printing</h1>
      <p class="page-subtitle">Print item labels (barcode, name, HSN), print from a GRN/Transfer Order, and configure category-based label templates.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = STICKER_TABS.map(t =>
    `<button class="btn ${t.id === currentStickerTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-sticker-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-sticker-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentStickerTab = btn.getAttribute('data-sticker-tab');
      renderView('stickers');
    });
  });

  if (currentStickerTab === 'templates') {
    await renderStickerTemplatesTab(container);
    return;
  }
  await renderStickerPrintTab(container);
}

async function renderStickerPrintTab(container) {
  const [printersRes, historyRes] = await Promise.all([
    apiFetch('/api/v1/doc/Printer'),
    apiFetch('/api/v1/stickers/history')
  ]);
  if (!printersRes || !historyRes) return;

  renderQZSetupPanel(container);

  const printers = printersRes.ok ? await printersRes.json() : [];
  stickerPrinters = printers;
  const history = historyRes.ok ? await historyRes.json() : [];

  renderStickerSourcePanel(container, printers);

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
        <label class="form-label" for="sticker-reprint-reason">Reprint Reason (optional)</label>
        <input type="text" id="sticker-reprint-reason" class="form-input">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="flex: 1; margin-bottom: 0;">
        <label class="form-label" for="sticker-sku-input">Scan or Enter SKU</label>
        <input type="text" id="sticker-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="sticker-add-btn">Add</button>
    </div>
    <div id="sticker-sku-list" style="margin-bottom: 16px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="sticker-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <button class="btn btn-primary" id="sticker-print-btn">Print Stickers</button>
  `;
  container.appendChild(formPanel);

  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  let historyHtml = `
    <table>
      <thead><tr><th>SKU</th><th>Barcode</th><th>Printer</th><th>Printed By</th><th>Copies</th><th>Source</th><th>Lot</th><th>Reprint Reason</th><th>Date</th></tr></thead>
      <tbody>
  `;
  historyHtml += history.length === 0
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No print history yet. Add SKUs above and use <b>Print Stickers</b>.</td></tr>`
    : history.map(h => `
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
        </tr>
      `).join('');
  historyHtml += `</tbody></table>`;
  historyPanel.innerHTML = historyHtml;
  container.appendChild(historyPanel);

  document.getElementById('sticker-add-btn').addEventListener('click', addStickerSKU);
  document.getElementById('sticker-sku-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addStickerSKU();
    }
  });
  document.getElementById('sticker-print-btn').addEventListener('click', printStickers);

  renderStickerSKUList();
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
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 4px;">Print from Transaction</h2>
    <p class="page-subtitle" style="margin: 0 0 16px;">Load a GRN or Transfer Order's own lines - print the whole document, or just one line.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-source-doctype">Module</label>
        <select id="sticker-source-doctype" class="form-input" style="width: 200px;">
          ${BULK_STICKER_SOURCE_DOCTYPES.map(d => `<option value="${d.doctype}">${d.label}</option>`).join('')}
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
    attachLinkTypeahead(docInput, doctypeSelect.value, { noSetupHint: true });
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
}

async function loadStickerSourcePreview() {
  const errorEl = document.getElementById('sticker-source-error');
  errorEl.classList.add('hidden');
  const doctype = document.getElementById('sticker-source-doctype').value;
  const docId = document.getElementById('sticker-source-doc').value.trim();
  stickerSourceDoctype = doctype;
  stickerSourceDocId = docId;
  if (!docId) return;

  const res = await apiFetch(`/api/v1/stickers/preview?source_doctype=${encodeURIComponent(doctype)}&source_doc_id=${encodeURIComponent(docId)}`);
  if (!res) return;
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
  stickerSourceLines.forEach((l, i) => {
    stickerSourceSelected[i] = true;
    stickerSourceCopies[i] = l.qty;
  });
  renderStickerSourceLines();
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
  el.innerHTML = `
    <table style="margin-top: 4px;">
      <thead><tr><th></th><th>SKU</th><th>Name</th>${anyBatch ? '<th>Batch/Lot</th>' : ''}<th>Category</th><th>Template</th><th>Copies</th><th></th></tr></thead>
      <tbody>
        ${stickerSourceLines.map((l, i) => `
          <tr>
            <td><input type="checkbox" data-source-select="${i}" ${stickerSourceSelected[i] ? 'checked' : ''}></td>
            <td style="font-family: monospace;">${escapeHTMLText(l.sku)}</td>
            <td>${escapeHTMLText(l.name || '')}</td>
            ${anyBatch ? `<td style="font-family: monospace;">${escapeHTMLText(l.batch_no || '')}</td>` : ''}
            <td>${escapeHTMLText(l.category || '')}</td>
            <td>${escapeHTMLText(l.template_name)}</td>
            <td><input type="number" min="1" data-source-copies="${i}" class="form-input" style="width: 70px;" value="${escapeHTMLText(stickerSourceCopies[i] ?? l.qty)}"></td>
            <td><button class="action-btn" data-source-print-one="${i}">Print</button></td>
          </tr>
        `).join('')}
      </tbody>
    </table>
    <button class="btn btn-primary" id="sticker-source-print-btn" style="margin-top: 12px;">Print Selected</button>
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
  el.querySelectorAll('[data-source-print-one]').forEach(btn => {
    btn.addEventListener('click', () => printStickerSourceLines([Number(btn.getAttribute('data-source-print-one'))]));
  });
  document.getElementById('sticker-source-print-btn').addEventListener('click', () => {
    const selected = stickerSourceLines.map((l, i) => i).filter(i => stickerSourceSelected[i]);
    printStickerSourceLines(selected);
  });
}

// indexes are positions in stickerSourceLines. Sent as the `lines` request
// field (sku + batch_no + copies per line) so the server can address the exact
// lot's line; the older `skus`/`copies_override` fields are still accepted by
// both endpoints for the manual flow and any existing client.
async function printStickerSourceLines(indexes) {
  const errorEl = document.getElementById('sticker-source-error');
  errorEl.classList.add('hidden');
  if (!indexes || indexes.length === 0) {
    errorEl.textContent = 'Select at least one line first.';
    errorEl.classList.remove('hidden');
    return;
  }
  const printerCode = document.getElementById('sticker-printer').value;
  if (!printerCode) {
    errorEl.textContent = 'Select a printer first.';
    errorEl.classList.remove('hidden');
    return;
  }
  const reprintReason = document.getElementById('sticker-reprint-reason').value.trim();
  const lines = indexes
    .map(i => stickerSourceLines[i])
    .filter(Boolean)
    .map((l, n) => ({
      sku: l.sku,
      batch_no: l.batch_no || '',
      copies: stickerSourceCopies[indexes[n]] || l.qty || 1
    }));

  const res = await apiFetch('/api/v1/stickers/print', {
    method: 'POST',
    body: JSON.stringify({
      lines, printer_code: printerCode, reprint_reason: reprintReason,
      source_doctype: stickerSourceDoctype, source_doc_id: stickerSourceDocId
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to print stickers.';
    errorEl.classList.remove('hidden');
    return;
  }
  await printStickerRun(data, { printerCode, copies: 1, jobRef: stickerSourceDocId });
  renderView('stickers');
}

function addStickerSKU() {
  const input = document.getElementById('sticker-sku-input');
  const sku = input.value.trim();
  if (!sku) return;
  if (!stickerSKUs.includes(sku)) stickerSKUs.push(sku);
  input.value = '';
  input.focus();
  renderStickerSKUList();
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
  const reprintReason = document.getElementById('sticker-reprint-reason').value.trim();

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
    body: JSON.stringify({ skus: stickerSKUs, printer_code: printerCode, reprint_reason: reprintReason, copies })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to print stickers.';
    errorEl.classList.remove('hidden');
    return;
  }

  await printStickerRun(data, { printerCode, copies });
  stickerSKUs = [];
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

async function renderStickerTemplatesTab(container) {
  const { renderStickerTemplateGallery } = await loadStickerStudio();
  await renderStickerTemplateGallery(container);
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


export { renderQZSetupPanel, qzRefreshStatus, qzDetectPrinters, qzTestPrint, qzPrintPickedDocument, renderStickersView, renderStickerPrintTab, renderStickerSourcePanel, loadStickerSourcePreview, renderStickerSourceLines, printStickerSourceLines, addStickerSKU, removeStickerSKU, renderStickerSKUList, printStickers, stickerFieldText, renderStickerElementHTML, renderStickerTemplatesTab, printStickerRun };
