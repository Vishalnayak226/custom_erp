// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
function renderAgentSetupPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div class="s-d1fbbb">
      <div>
        <h2 class="s-d5d9be">Print Setup (PrintBridge)</h2>
        <p id="agent-status" class="page-subtitle s-2a0ca8">Checking for PrintBridge on this computer&hellip;</p>
      </div>
      <div class="s-aea80b">
        <a class="btn btn-outline" href="/downloads/printbridge-setup.exe" download title="Windows: run it once on each computer that prints">Download PrintBridge</a>
        <button class="btn btn-outline" ${actionAttrs('agentDetectPrinters')}>Detect Printers</button>
        <button class="btn btn-outline" ${actionAttrs('agentTestPrint')}>Test Print</button>
      </div>
    </div>
    <div id="agent-detected" class="s-4bb900"></div>
    <div class="s-e81bf2">
      <label class="form-label" for="agent-doc-file">Print a marketplace label or invoice (PDF from Myntra, or any channel)</label>
      <div class="s-ccacbc">
        <input type="file" id="agent-doc-file" class="form-input s-c3f3cb" accept=".pdf,.zpl,.txt,.prn">
        <button class="btn btn-primary" id="agent-doc-preview-btn" type="button">Print</button>
      </div>
      <p class="page-subtitle s-0f6f74">
        Prints to the <strong>Shipping Label</strong> default printer.
      </p>
    </div>
  `;
  container.appendChild(panel);
  panel.querySelector('#agent-doc-preview-btn').addEventListener('click', agentPreviewPickedDocument);
  agentRefreshStatus();
}

// agentPreviewPickedDocument (Stage 133.3) shows the picked file as the channel
// issued it before it prints: a PDF through a link to the browser's own viewer
// in a new tab (the page's security policy keeps PDFs out of its own frames),
// a ZPL or text file as its text - the printer's commands, which the printer
// draws from. Stage 143.4: it is what Print does; its Print prints.
async function agentPreviewPickedDocument() {
  const input = document.getElementById('agent-doc-file');
  if (!input || !input.files || input.files.length === 0) {
    await showCustomAlert('Choose a label or invoice file first.', 'Nothing Selected');
    return;
  }
  const file = input.files[0];
  const pdf = /\.pdf$/i.test(file.name) || file.type === 'application/pdf';
  const url = pdf ? URL.createObjectURL(file) : '';
  const body = pdf
    ? `<p class="s-2a0ca8"><a href="${url}" target="_blank" rel="noopener">Open the PDF</a></p>`
    : `<pre class="s-e3d67b">${escapeHTMLText(await file.text())}</pre>`;
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal-container s-c06a24" role="dialog" aria-modal="true" aria-labelledby="agent-doc-preview-title">
      <div class="modal-header"><h3 class="modal-title" id="agent-doc-preview-title">${escapeHTMLText(file.name)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-agent-preview="close">×</button></div>
      <div class="modal-body">
        ${body}
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" data-agent-preview="close">Close</button>
        <button type="button" class="btn btn-primary" data-agent-preview="print">Print</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  requestAnimationFrame(() => overlay.classList.add('open'));
  const close = () => { overlay.remove(); if (url) URL.revokeObjectURL(url); };
  overlay.querySelectorAll('[data-agent-preview="close"]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  overlay.querySelector('[data-agent-preview="print"]').addEventListener('click', () => { close(); agentPrintPickedDocument(); });
}

async function agentRefreshStatus() {
  const el = document.getElementById('agent-status');
  if (!el) return;
  const ok = await agentTryConnect();
  if (ok) {
    // Stage 137.4: PrintBridge's own release; one older than the download
    // (or one before 0.2.0, which cannot say) is offered the update.
    const [have, latest] = await Promise.all([
      window.PrintAgent.agentVersion ? window.PrintAgent.agentVersion().catch(() => '') : '',
      fetch('/downloads/printbridge-version.txt').then(r => (r.ok ? r.text() : '')).catch(() => '')
    ]);
    const older = latest && versionBelow(have || '0.1.0', latest.trim());
    // 203.3: from 0.3.0 PrintBridge replaces itself; only an older one needs
    // the download, once.
    // A failed update (bad download, or the same installer already run three
    // times) is shown, with the agent's reason on hover, not "updating".
    const upd = window.PrintAgent.updateStatus ? window.PrintAgent.updateStatus() : null;
    const stuck = older && upd && upd.status === 'failed';
    const selfUpdating = older && have && !versionBelow(have, '0.3.0') && !stuck;
    el.title = stuck ? String(upd.error || '') : '';
    el.textContent = !older
      ? `PrintBridge${have ? ' ' + have : ''} is running - printing is silent on this computer.`
      : selfUpdating
        ? `PrintBridge ${have} is running - updating to ${latest.trim()} by itself.`
        : stuck
          ? `PrintBridge ${have} could not update itself to ${latest.trim()}. Download PrintBridge and run it once.`
          : `${have ? `PrintBridge ${have}` : 'An older PrintBridge'} is running. ${latest.trim()} is out: Download PrintBridge and run it once - it updates itself after that.`;
    el.style.color = older && !selfUpdating ? 'var(--color-warning, #b45309)' : 'var(--color-success, #15803d)';
  } else {
    el.textContent = agentLastError + ' Download PrintBridge and run it once; until then prints open the browser dialog.';
    el.style.color = 'var(--color-warning, #b45309)';
  }
}

const versionBelow = (a, b) => {
  const pa = String(a).split('.').map(Number), pb = String(b).split('.').map(Number);
  for (let i = 0; i < 3; i++) if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) < (pb[i] || 0);
  return false;
};

// Lists the OS printer names PrintBridge can see. This is the value the operator
// copies into a Printer record's "OS Printer Name" field.
async function agentDetectPrinters() {
  const out = document.getElementById('agent-detected');
  if (!out) return;
  if (!await agentTryConnect(false)) return;
  out.textContent = 'Detecting...';
  try {
    const names = await window.PrintAgent.listOSPrinters();
    const list = Array.isArray(names) ? names : [names];
    if (list.length === 0) {
      out.textContent = 'PrintBridge reports no printers installed on this computer.';
      return;
    }
    let def = '';
    try { def = await window.PrintAgent.getDefaultPrinter(); } catch (e) { /* optional */ }
    out.innerHTML = `
      <p class="page-subtitle s-2c0002">Detected printers:</p>
      <ul class="s-0e0f8e">
        ${list.map(n => `<li><code>${escapeHTMLText(n)}</code>${n === def ? ' <em>(system default)</em>' : ''}</li>`).join('')}
      </ul>`;
  } catch (err) {
    out.textContent = 'Could not list printers: ' + (err.message || err);
  }
}

// Proves the whole chain - certificate, signature, socket, driver - before an
// operator depends on it during a packing run.
async function agentTestPrint() {
  if (!await agentTryConnect(false)) return;
  const printers = await apiFetch('/api/v1/print/agent/printers');
  if (!printers || !printers.ok) {
    await showApiError(printers, 'Could not load configured printers.', 'Test Print');
    return;
  }
  const list = await printers.json();
  const target = list.find(p => p.os_printer_name);
  if (!target) {
    await showCustomAlert(
      'No Printer record has an OS Printer Name yet. Click "Detect Printers", then edit a Printer ' +
      'under Masters and paste the exact name into "OS Printer Name".', 'Test Print');
    return;
  }
  const raw = ['ZPL', 'TSPL', 'ESC-POS'].includes((target.printer_language || '').toUpperCase());
  const stamp = formatDateTime(new Date());
  // Stage 143.4: the test page shows first; Print there sends it.
  if (!await agentPreviewTestPage(target, raw ? 'ERP test label' : 'ERP test print', stamp)) return;
  const items = raw
    ? [{ type: 'raw', format: 'command', flavor: 'plain',
         data: '^XA^CI28^FO30,30^A0N,40,40^FDERP test label^FS^FO30,90^A0N,28,28^FD' + stamp + '^FS^XZ' }]
    : [{ type: 'pixel', format: 'html', flavor: 'plain',
         data: '<h2>ERP test print</h2><p>' + escapeHTMLText(stamp) + '</p>' }];
  try {
    await window.PrintAgent.printItems(target.os_printer_name, items, 1);
    showToast(`Test page sent to ${target.os_printer_name}.`, { variant: 'success' });
  } catch (err) {
    await showCustomAlert(err.message || String(err), 'Test Print Failed');
  }
}

// agentPreviewTestPage (Stage 143.4) shows the test page and the printer it goes
// to, with Close and Print; resolves true on Print.
function agentPreviewTestPage(target, heading, stamp) {
  return new Promise(resolve => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open';
    overlay.innerHTML = `
      <div class="modal-container s-c31abe" role="dialog" aria-modal="true" aria-labelledby="agent-test-preview-title">
        <div class="modal-header"><h3 class="modal-title" id="agent-test-preview-title">Test print</h3>
          <button type="button" class="modal-close" aria-label="Close dialog" data-agent-test="close">×</button></div>
        <div class="modal-body">
          <p class="s-f041c9">On ${escapeHTMLText(target.name || target.os_printer_name)}</p>
          <div class="s-91d664">
            <div class="s-59f9d7">${escapeHTMLText(heading)}</div><div>${escapeHTMLText(stamp)}</div></div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" data-agent-test="close">Close</button>
          <button type="button" class="btn btn-primary" data-agent-test="print">Print</button>
        </div>
      </div>`;
    document.body.appendChild(overlay);
    const done = (v) => { overlay.remove(); document.removeEventListener('keydown', onKey); resolve(v); };
    const onKey = (e) => { if (e.key === 'Escape') done(false); };
    document.addEventListener('keydown', onKey);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) done(false); });
    overlay.querySelectorAll('[data-agent-test="close"]').forEach(b => b.addEventListener('click', () => done(false)));
    const print = overlay.querySelector('[data-agent-test="print"]');
    print.addEventListener('click', () => done(true));
    print.focus();
  });
}

async function agentPrintPickedDocument() {
  const input = document.getElementById('agent-doc-file');
  if (!input || !input.files || input.files.length === 0) {
    await showCustomAlert('Choose a label or invoice file first.', 'Nothing Selected');
    return;
  }
  const printed = await agentPrintMarketplaceDocument(input.files[0]);
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
  { doctype: 'TransferOrder', label: 'Transfer Order' },
  { doctype: 'PurchaseOrder', label: 'Purchase Order' }, // 146.4.6c
  { doctype: 'SalesOrder', label: 'Sales Order' } // 52.7: its lines are SalesOrderLine records
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
let stickerSourceChosen = '';   // 143.6: the document whose preview choosing it opened
let stickerSourceOpening = '';  // 246.2: the document being loaded or previewed now
let stickerSourceAwaiting = null; // 143.6: lines whose preview waits for a printer
let stickerSourceQueue = [];    // 320.2: the next goods receipts to print, {id, skus}
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
  // station (PrintBridge status, Print from Transaction, scan) as a page of its own
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
  let pre = null; // 146.4.6c: opened from a PO's Stickers button
  try { pre = JSON.parse(sessionStorage.getItem('erp-sticker-source') || 'null'); sessionStorage.removeItem('erp-sticker-source'); } catch (_) { /* storage off */ }
  if (pre) {
    window.txnShowPane('sticker-print:new');
    const sel = document.getElementById('sticker-source-doctype');
    sel.value = pre.doctype; sel.dispatchEvent(new Event('change'));
    document.getElementById('sticker-source-doc').value = pre.id;
    loadStickerSourcePreview();
  }

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div class="s-8a0cfa">
      <div class="form-group s-50435f">
        <label class="form-label" for="sticker-printer">Printer</label>
        <select id="sticker-printer" class="form-input s-0fff9b">
          <option value="">Select a printer</option>
          ${printers.map(p => `<option value="${escapeHTMLText(p.code || p.id)}">${escapeHTMLText(p.name || p.code || p.id)}</option>`).join('')}
        </select>
      </div>
      <button type="button" class="btn btn-outline" id="sticker-preset-btn" hidden>Printer preset</button>
      <div class="form-group s-50435f">
        <label class="form-label" for="sticker-copies">Copies per SKU</label>
        <input type="number" id="sticker-copies" class="form-input s-da190a" value="1" min="1">
      </div>
      <div class="form-group s-2ded36">
        <label class="form-label" for="sticker-reprint-reason">Reprint reason (kept in the print history)</label>
        <div class="s-b9b7b5">
          <select id="sticker-reprint-code" class="form-select hidden s-5e5c19" aria-label="Reprint reason code"></select>
          <input type="text" id="sticker-reprint-reason" class="form-input">
        </div>
      </div>
    </div>
    <div class="s-66c58d">
      <div class="form-group s-73a007">
        <label class="form-label" for="sticker-sku-input">Scan or Enter SKU</label>
        <input type="text" id="sticker-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <label class="s-4b57ec" title="Each scan prints its sticker straight away - with the loaded document's lot when the item is on it">
        <input type="checkbox" id="sticker-print-on-scan"> Print on scan
      </label>
      <button class="btn btn-outline" id="sticker-add-btn">Add</button>
    </div>
    <div id="sticker-sku-list" class="s-8eeb14"></div>
    <div id="sticker-form-error" class="login-error hidden s-29b798"></div>
    <div class="s-8f624a">
      <button class="btn btn-primary s-634a28" id="sticker-print-btn">Print Stickers</button>
    </div>
  `;
  printPage.appendChild(formPanel);
  stickerReprintCodeWanted = '';
  fillStickerReprintCodes();

  // Stage 55.19: printer setup (PrintBridge detect/test, channel label PDFs) is a
  // one-time path per computer, so it sits collapsed below the daily flow.
  const setup = document.createElement('details');
  setup.className = 'sticker-printer-setup';
  setup.innerHTML = '<summary class="s-a2b6bc">Printer setup on this computer (PrintBridge) and channel label PDFs</summary>';
  printPage.appendChild(setup);
  renderAgentSetupPanel(setup);

  let historyHtml = `
    <table>
      <thead><tr><th>SKU</th><th>Barcode</th><th>Printer</th><th>Printed By</th><th>Copies</th><th>Source</th><th>Lot</th><th>Reprint Reason</th><th>Date</th><th></th></tr></thead>
      <tbody>
  `;
  historyHtml += history.length === 0
    ? `<tr><td colspan="10" class="s-306094">No print history yet.</td></tr>`
    : history.map((h, i) => `
        <tr>
          <td class="s-355794">${escapeHTMLText(h.sku)}</td>
          <td class="s-355794">${escapeHTMLText(h.barcode)}</td>
          <td>${escapeHTMLText(h.printer_code)}</td>
          <td>${escapeHTMLText(h.printed_by)}</td>
          <td>${escapeHTMLText(h.copies)}</td>
          <td>${escapeHTMLText(h.source_doc_id ? `${h.source_doctype} ${h.source_doc_id}` : '')}</td>
          <td class="s-355794">${escapeHTMLText(h.batch_no || '')}</td>
          <td>${escapeHTMLText(h.reprint_reason || '')}</td>
          <td>${escapeHTMLText(formatDateTime(h.printed_at))}</td>
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
  printerSel.addEventListener('change', () => {
    try { localStorage.setItem('sticker-printer', printerSel.value); } catch (_) { /* storage off */ }
    renderStickerPresetButton();
    renderStickerSourceLines();
    if (stickerSourceAwaiting && printerSel.value) previewSourceThenPrint(stickerSourceAwaiting);
  });
  document.getElementById('sticker-preset-btn').addEventListener('click', openPrinterPreset);
  renderStickerPresetButton();
  // The roll's name and shape come with the template choices.
  loadStickerTemplateChoices().then(c => { if (!stickerTemplateChoices.length) stickerTemplateChoices = c; renderStickerPresetButton(); renderStickerSourceLines(); });
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
  document.getElementById('sticker-print-btn').addEventListener('click', previewStickers);

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
// 194.2: Reprint opens the preview at once, so its Print is click 2. The
// reason box stays optional (a blank reason is accepted), so it is not asked.
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
    stickerSourceChosen = h.source_doc_id;
    if (!(await loadStickerSourcePreview())) return;
    const picked = [];
    stickerSourceLines.forEach((l, i) => {
      const match = String(l.sku) === h.sku && (l.batch_no || '') === (h.batch_no || '');
      stickerSourceSelected[i] = match;
      if (match) { stickerSourceCopies[i] = copies; picked.push(i); }
    });
    renderStickerSourceLines();
    if (picked.length) { previewSourceThenPrint(picked); return; }
    if (stickerSourceLines.length) {
      const errorEl = document.getElementById('sticker-source-error');
      errorEl.textContent = `${h.sku}${h.batch_no ? ` (lot ${h.batch_no})` : ''} is no longer on ${h.source_doc_id}. Tick the line to print, or scan the item below.`;
      errorEl.classList.remove('hidden');
    }
    return;
  }
  stickerSKUs = [h.sku];
  document.getElementById('sticker-copies').value = copies;
  document.getElementById('sticker-print-on-scan').checked = false;
  renderStickerSKUList();
  previewStickers();
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
    <h2 class="s-bfc163">Print from Transaction</h2>
    <div class="s-8a0cfa">
      <div class="form-group s-50435f">
        <label class="form-label" for="sticker-source-doctype">Module</label>
        <select id="sticker-source-doctype" class="form-input s-0fff9b">
          ${stickerSourceDoctypes().map(d => `<option value="${d.doctype}">${escapeHTMLText(d.label)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group s-20be51">
        <label class="form-label" for="sticker-source-doc">Document</label>
        <input type="text" id="sticker-source-doc" class="form-input" placeholder="Search by number..." autocomplete="off">
      </div>
      <button class="btn btn-outline" id="sticker-source-load-btn" type="button">Load</button>
    </div>
    <div id="sticker-source-error" class="login-error hidden s-29b798"></div>
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
    stickerSourceChosen = '';
    stickerSourceAwaiting = null;
    stickerSourceLines = [];
    document.getElementById('sticker-source-lines').innerHTML = '';
    attachSourcePicker();
  });
  docInput.addEventListener('change', () => chooseStickerSourceDoc());
  document.getElementById('sticker-source-load-btn').addEventListener('click', () => chooseStickerSourceDoc(true));
  // 143.6: + Print Stickers opens on the latest documents, ready to pick one.
  container.closest('#sticker-print')?.querySelector('.page-section-actions')?.addEventListener('click', () => setTimeout(() => {
    if (!docInput.value.trim() && !stickerSKUs.length && docInput.offsetParent) docInput.focus();
  }, 0));

  // Stage 94: "Print stickers" on a posted child GRN opens here with that
  // receipt loaded and only that child's lines ticked.
  let pending = null;
  try {
    pending = JSON.parse(sessionStorage.getItem('grn-child-stickers') || 'null');
    sessionStorage.removeItem('grn-child-stickers');
  } catch (_) { /* storage can be disabled */ }
  if (pending && pending.id) {
    // 320.2: an upload's receipts print one after another.
    stickerSourceQueue = Array.isArray(pending.next) ? pending.next.filter(n => n && n.id) : [];
    // Stage 129: open the print page itself, not the history behind it.
    window.txnShowPane('sticker-print:new');
    doctypeSelect.value = 'GRN';
    stickerSourceDoctype = 'GRN';
    attachSourcePicker();
    docInput.value = pending.id;
    stickerSourceChosen = pending.id;
    loadStickerSourcePreview().then(loaded => {
      if (!loaded) return;
      if (Array.isArray(pending.skus) && pending.skus.length) {
        stickerSourceLines.forEach((l, i) => { stickerSourceSelected[i] = pending.skus.includes(l.sku); });
        renderStickerSourceLines();
      }
      // 143.6: straight to its preview; Print there prints.
      previewSourceThenPrint(stickerSourceLines.map((l, i) => i).filter(i => stickerSourceSelected[i]));
    });
  }
}

// chooseStickerSourceDoc (143.6, user: "Select grn and print thats all"):
// choosing a document shows its stickers' preview at once - every line, its
// own copies, this computer's printer - so Print there is the one other
// click. Close leaves the lines below to change and Print Selected. The same
// document chosen again (the box's own change on blur) does not reopen it;
// Load does.
async function chooseStickerSourceDoc(force = false) {
  const value = document.getElementById('sticker-source-doc').value.trim();
  if (!value || (!force && value === stickerSourceChosen)) return;
  // 246.2: typing a number then clicking Load fires the box's change and Load's
  // click together; one preview, not two stacked.
  if (value === stickerSourceOpening) return;
  stickerSourceQueue = [];
  stickerSourceChosen = value;
  stickerSourceOpening = value;
  try {
    if (await loadStickerSourcePreview()) await previewSourceThenPrint(stickerSourceLines.map((l, i) => i));
  } finally {
    stickerSourceOpening = '';
  }
}

// previewSourceThenPrint: Stage 143.4 - every Print shows the preview first;
// its Print prints.
async function previewSourceThenPrint(indexes) {
  // 143.6: no printer yet (a new computer) - picking one opens this preview.
  stickerSourceAwaiting = document.getElementById('sticker-printer').value ? null : indexes;
  const body = stickerSourceRequest(indexes);
  if (body && await previewStickerRequest(body, 1, document.getElementById('sticker-source-error'))) printStickerSourceLines(indexes);
}

// loadStickerSourcePreview loads the document's lines, all ticked; true when
// there is a line to print.
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
  return stickerSourceLines.length > 0;
}

// Active templates, then the built-in designs not saved yet (Stage 113.2).
async function loadStickerTemplateChoices() {
  const [tplRes, E] = await Promise.all([apiFetch('/api/v1/doc/StickerTemplate'), loadStickerEngine()]);
  const docs = tplRes && tplRes.ok ? await tplRes.json() : [];
  stickerEngineSync = E;
  return [
    ...docs.filter(t => (t.status || 'Active') === 'Active').map(t => ({ id: t.id, name: t.name || t.code || t.id, shape: E.rollShape(E.templateFromDoc(t)), is_default: t.is_default === true || t.is_default === 'true' })),
    ...E.unsavedDesigns(docs).map(d => ({ id: `design:${d.id}`, name: `${d.label} (built-in)`, shape: E.rollShape(E.designTemplate(d)), design: d }))
  ];
}

// Stage 137: the printer picked on the print page, and the roll its preset
// names ({ id, name, shape } from the template choices) or null.
let stickerEngineSync = null; // the sticker engine once loaded, for the synchronous renders
const stickerSelectedPrinter = () => {
  const code = (document.getElementById('sticker-printer') || {}).value || '';
  return stickerPrinters.find(p => (p.code || p.id) === code) || null;
};
function stickerRoll() {
  const id = (stickerSelectedPrinter() || {}).sticker_template || '';
  return id ? stickerTemplateChoices.find(t => t.id === id) || null : null;
}
// stickerFitsRoll mirrors the server's applyPrinterRoll: '' or 'standard' is
// the built-in label. True when there is no roll to fit.
function stickerFitsRoll(id, roll) {
  if (!roll || !stickerEngineSync) return true;
  const shape = !id || id === 'standard' ? stickerEngineSync.builtInShape() : (stickerTemplateChoices.find(t => t.id === id) || {}).shape;
  return !!shape && stickerEngineSync.fitsRoll(shape, roll.shape);
}

function renderStickerPresetButton() {
  const btn = document.getElementById('sticker-preset-btn');
  const p = stickerSelectedPrinter();
  if (!btn) return;
  btn.hidden = !p;
  if (!p) return;
  const roll = stickerRoll();
  btn.textContent = roll ? `Roll: ${roll.name}` : 'Printer preset';
  btn.title = `Roll, shift (right ${Number(p.shift_right_mm) || 0} mm, down ${Number(p.shift_down_mm) || 0} mm, skew ${Number(p.skew_deg) || 0}°), darkness and speed of ${p.name || p.code}`;
}

// openPrinterPreset (Stage 137.1, 137.3; user, 2026-10-25: "preset i need in
// config, once i set, it will not ask") edits the selected printer's preset:
// the roll loaded, its alignment, darkness and speed. Saved on the Printer
// record, so every computer printing to it uses it. Test alignment prints
// with the values on screen, saved or not, so the shift is tuned on paper.
async function openPrinterPreset() {
  const printer = stickerSelectedPrinter();
  if (!printer) { await showCustomAlert('Select a printer first.', 'Printer preset'); return; }
  const E = await loadStickerEngine();
  if (!stickerTemplateChoices.length) stickerTemplateChoices = await loadStickerTemplateChoices();
  const may = typeof canUpdateDoctype !== 'function' || canUpdateDoctype('Printer');
  const fields = [['shift_right_mm', 'Shift right (mm)', '0'], ['shift_down_mm', 'Shift down (mm)', '0'],
    ['darkness', 'Darkness (0-15)', 'Template'], ['print_speed', 'Speed (inches/s)', 'Template'], ['skew_deg', 'Skew (°)', '0']];
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal-container s-6a9c85" role="dialog" aria-modal="true" aria-labelledby="stk-preset-title">
      <div class="modal-header"><h3 class="modal-title" id="stk-preset-title">Printer preset · ${escapeHTMLText(printer.name || printer.code || printer.id)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-p="close">×</button></div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label" for="stk-preset-roll">Roll loaded</label>
          <select id="stk-preset-roll" class="form-input">
            <option value="">None - template rules decide</option>
            ${stickerTemplateChoices.map(t => `<option value="${escapeHTMLText(t.id)}"${t.id === printer.sticker_template ? ' selected' : ''}>${escapeHTMLText(t.name)}</option>`).join('')}
          </select>
        </div>
        <div class="s-ba0ec7">
          ${fields.map(([k, label, hint]) => `<div class="form-group s-2a0ca8">
            <label class="form-label" for="stk-preset-${k}">${label}</label>
            <input type="number" step="any" id="stk-preset-${k}" class="form-input" placeholder="${hint}" value="${escapeHTMLText(printer[k] ?? '')}"></div>`).join('')}
        </div>
        <div class="form-group" id="stk-lineup-pick" hidden>
          <span class="form-label">Best row</span>
          <div class="flex-row-gap">${E.LINE_UP_STEPS_MM.map((s, i) => `<button type="button" class="btn btn-outline" data-pick="${i}">${String.fromCharCode(65 + i)}</button>`).join('')}</div>
        </div>
        <div id="stk-preset-error" class="login-error hidden s-4bb900"></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" data-p="photo">Fix from photo</button>
        <button type="button" class="btn btn-outline" data-p="lineup">Line up</button>
        <button type="button" class="btn btn-outline" data-p="test">Test alignment</button>
        <button type="button" class="btn btn-outline" data-p="calibrate">Calibrate roll</button>
        <button type="button" class="btn btn-outline" data-p="close">Cancel</button>
        <button type="button" class="btn btn-primary" data-p="save"${may ? '' : ' disabled title="Your role cannot change printers"'}>Save</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  requestAnimationFrame(() => overlay.classList.add('open'));
  const errorEl = overlay.querySelector('#stk-preset-error');
  const say = (msg) => { errorEl.textContent = msg; errorEl.classList.toggle('hidden', !msg); };
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  const read = () => {
    const p = { sticker_template: overlay.querySelector('#stk-preset-roll').value };
    for (const [k] of fields) {
      const v = overlay.querySelector(`#stk-preset-${k}`).value.trim();
      p[k] = v === '' ? '' : Number(v);
    }
    return p;
  };
  const problem = (p) => {
    if (p.darkness !== '' && !(p.darkness >= 0 && p.darkness <= 15)) return 'Darkness is 0 to 15.';
    if (p.print_speed !== '' && !(p.print_speed > 0)) return 'Speed is more than 0.';
    if ([p.shift_right_mm, p.shift_down_mm].some(v => v !== '' && Math.abs(v) > 20)) return 'A shift is at most 20 mm either way.';
    if (p.skew_deg !== '' && Math.abs(p.skew_deg) > 5) return 'Skew is at most 5° either way.';
    return '';
  };
  // 194.1: with no roll set, the template a print falls back to (the one
  // ticked Default, else the only label-roll template), not the built-in
  // 50 x 25 - a 50 x 25 page on a 100 mm tag feeds the roll wrongly.
  const rollTemplate = async (id) => {
    if (!id) {
      const saved = stickerTemplateChoices.filter(t => !t.design);
      const rolls = saved.filter(t => !t.shape.sheet);
      id = ((saved.find(t => t.is_default) || (rolls.length === 1 ? rolls[0] : null)) || {}).id;
    }
    const choice = stickerTemplateChoices.find(t => t.id === id);
    if (!choice) return null;
    if (choice.design) return E.designTemplate(choice.design);
    const res = await apiFetch(`/api/v1/doc/StickerTemplate/${encodeURIComponent(id)}`);
    return res && res.ok ? E.templateFromDoc(await res.json()) : null;
  };
  // 194.1: the rows the last Line up printed; tapping a letter saves its shift.
  let lineRows = null;
  const pickRow = overlay.querySelector('#stk-lineup-pick');
  overlay.addEventListener('click', async (e) => {
    const pick = e.target.closest('[data-pick]');
    const act = pick && lineRows ? 'save' : e.target.closest('[data-p]')?.dataset.p;
    if (!act) return;
    if (act === 'close') { close(); return; }
    if (pick) overlay.querySelector('#stk-preset-shift_right_mm').value = lineRows[Number(pick.dataset.pick)].shift_right_mm;
    const p = read();
    say(problem(p));
    if (problem(p)) return;
    if (pick && !may) { say('Your role cannot change printers.'); return; }
    const btn = e.target.closest('button');
    btn.disabled = true;
    try {
      if (act === 'photo') {
        // 251.1: the ERP reads the shift off a photo of a check label and
        // saves it on the printer; this dialog's fields would be stale.
        const roll = await rollTemplate(p.sticker_template);
        if (roll && roll.settings.sheet.mode === 'a4') { say('Fix from photo is for label rolls.'); return; }
        close();
        const { openFixFromPhoto } = await import('./print-check.js' + PRINTING_MODULE_QUERY);
        await openFixFromPhoto(printer, roll, { onSaved: () => { renderStickerPresetButton(); renderStickerSourceLines(); } });
      } else if (act === 'lineup') {
        const roll = await rollTemplate(p.sticker_template);
        if (roll && roll.settings.sheet.mode === 'a4') { say('Line up is for label rolls.'); return; }
        lineRows = await E.printLineUp(roll, Object.assign({}, printer, p));
        pickRow.hidden = !lineRows;
        if (lineRows) pickRow.querySelectorAll('[data-pick]').forEach((b, i) => { b.title = `Shift right ${lineRows[i].shift_right_mm} mm`; });
      } else if (act === 'test') {
        // Stage 143.4: the test label shows first; Print there prints it.
        await E.printAlignmentTest(await rollTemplate(p.sticker_template), Object.assign({}, printer, p));
      } else if (act === 'calibrate') {
        await E.calibrateRoll(printer);
        window.showToast?.(`${printer.name || printer.code} is measuring the roll.`, { variant: 'info' });
      } else if (act === 'save') {
        if (p.sticker_template.startsWith('design:')) {
          p.sticker_template = await pickStickerChoice(p.sticker_template);
          if (!p.sticker_template) return;
        }
        const { patchDoc } = await loadStickerStudio();
        if (!await patchDoc('Printer', printer.id, p)) return;
        Object.assign(printer, p);
        close();
        renderStickerPresetButton();
        renderStickerSourceLines();
        window.showToast?.(pick ? `Row ${pick.textContent} saved: shift right ${p.shift_right_mm} mm.` : 'Printer preset saved.', { variant: 'success' });
      }
    } catch (err) {
      say(err.message || String(err));
    } finally {
      btn.disabled = act === 'save' && !pick && !may;
    }
  });
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
  // Stage 137.2: with a roll set on the printer, only templates that fit it
  // are offered, and a line whose rule picks one that does not fit shows the
  // roll's template - what the server prints.
  const roll = stickerRoll();
  const choiceOptions = (selected) => [{ id: 'standard', name: 'Standard label' }, ...stickerTemplateChoices]
    .filter(t => stickerFitsRoll(t.id, roll))
    .map(t => `<option value="${escapeHTMLText(t.id)}" ${t.id === selected ? 'selected' : ''}>${escapeHTMLText(t.name)}</option>`).join('');
  stickerSourceLines.forEach((l, i) => { if (stickerSourceTemplates[i] && !stickerFitsRoll(stickerSourceTemplates[i], roll)) stickerSourceTemplates[i] = ''; });
  const ruleText = l => (roll && !stickerFitsRoll(l.template_id, roll)
    ? `${roll.name} (printer preset)` : `${l.template_name} (${STICKER_RULE_TEXT[l.template_rule] || 'no rule'})`);
  const values = stickerSourceLines.map((l, i) => stickerSourceTemplates[i] || '');
  const runValue = values.every(v => v === values[0]) ? values[0] : '__mixed';
  // Stage 119.5: a saved template picked here can be kept as the Product type rule.
  const keepable = v => v && v !== 'standard' && v !== '__mixed';
  const allTypes = [...new Set(stickerSourceLines.map(l => l.product_type).filter(Boolean))];
  const keepTitle = types => `Print every ${types} item with this template from now on (the Product type rule in Template rules)`;
  el.innerHTML = `
    <label class="form-label s-406c5f">
      Template for every line
      <select id="sticker-source-template-all" class="form-input s-7e375a">
        ${runValue === '__mixed' ? '<option value="__mixed" selected>Chosen per line</option>' : ''}
        <option value="" ${runValue === '' ? 'selected' : ''}>As per template rules</option>
        ${choiceOptions(runValue)}
      </select>
      ${keepable(runValue) && allTypes.length ? `<button type="button" class="action-btn" id="sticker-source-keep-all" title="${escapeHTMLText(keepTitle(allTypes.join(', ')))}">Keep for ${escapeHTMLText(allTypes.join(', '))}</button>` : ''}
    </label>
    <table class="s-6b1f0d">
      <thead><tr><th><input type="checkbox" id="sticker-source-select-all" aria-label="Select all lines"></th><th>SKU</th><th>Name</th>${anyBatch ? '<th>Batch/Lot</th>' : ''}<th>Category</th><th>Template</th><th>Copies</th><th></th></tr></thead>
      <tbody>
        ${stickerSourceLines.map((l, i) => `
          <tr>
            <td><input type="checkbox" data-source-select="${i}" ${stickerSourceSelected[i] ? 'checked' : ''} aria-label="Print ${escapeHTMLText(l.sku)}"></td>
            <td class="s-355794">${escapeHTMLText(l.sku)}</td>
            <td>${escapeHTMLText(l.name || '')}</td>
            ${anyBatch ? `<td class="s-355794">${escapeHTMLText(l.batch_no || '')}</td>` : ''}
            <td>${escapeHTMLText(l.category || '')}</td>
            <td><select data-source-template="${i}" class="form-input s-91cb78" aria-label="Template for ${escapeHTMLText(l.sku)}" title="Rule that picked it: ${escapeHTMLText(l.template_rule || 'Standard')}">
              <option value="">${escapeHTMLText(ruleText(l))}</option>
              ${choiceOptions(values[i])}
            </select>${keepable(values[i]) && l.product_type ? ` <button type="button" class="action-btn" data-source-keep="${i}" title="${escapeHTMLText(keepTitle(l.product_type))}">Keep for ${escapeHTMLText(l.product_type)}</button>` : ''}</td>
            <td><input type="number" min="1" data-source-copies="${i}" class="form-input s-825185" aria-label="Copies of ${escapeHTMLText(l.sku)}" value="${escapeHTMLText(stickerSourceCopies[i] ?? l.qty)}"></td>
            <td><button class="action-btn" data-source-print-one="${i}">Print</button></td>
          </tr>
        `).join('')}
      </tbody>
    </table>
    <div class="s-ca5a7e">
      <button class="btn btn-primary" id="sticker-source-print-btn">Print Selected</button>
    </div>
  `;
  // Stage 153.1: the header box ticks / clears every line.
  const selectAll = document.getElementById('sticker-source-select-all');
  const lineBoxes = [...el.querySelectorAll('[data-source-select]')];
  const syncSelectAll = () => {
    const n = lineBoxes.filter(cb => cb.checked).length;
    selectAll.checked = n === lineBoxes.length;
    selectAll.indeterminate = n > 0 && n < lineBoxes.length;
  };
  lineBoxes.forEach(cb => {
    cb.addEventListener('change', () => {
      stickerSourceSelected[Number(cb.getAttribute('data-source-select'))] = cb.checked;
      syncSelectAll();
    });
  });
  selectAll.addEventListener('change', () => {
    lineBoxes.forEach(cb => {
      cb.checked = selectAll.checked;
      stickerSourceSelected[Number(cb.getAttribute('data-source-select'))] = cb.checked;
    });
    syncSelectAll();
  });
  syncSelectAll();
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
    btn.addEventListener('click', () => previewSourceThenPrint([Number(btn.getAttribute('data-source-print-one'))]));
  });
  const selectedLines = () => stickerSourceLines.map((l, i) => i).filter(i => stickerSourceSelected[i]);
  document.getElementById('sticker-source-print-btn').addEventListener('click', () => previewSourceThenPrint(selectedLines()));
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
  if (stickerSourceQueue.length) {
    const [next, ...rest] = stickerSourceQueue;
    try { sessionStorage.setItem('grn-child-stickers', JSON.stringify({ ...next, next: rest })); } catch (_) { /* storage can be disabled */ }
  }
  stickerSourceQueue = [];
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
// print. Print in the preview prints it the same way. Stage 143.4: it is
// what Print Stickers does - there is no separate Preview button.
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
    errorEl.textContent = 'Scan or type a SKU first, then Print.';
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
    : stickerSKUs.map(sku => `${escapeHTMLText(sku)} <button class="action-btn action-btn-danger s-a49d10" data-remove-sticker-sku="${escapeHTMLText(sku)}">x</button>`).join(' &nbsp; ');
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
    return `<div class="sticker-el sticker-el-barcode" data-css="${baseStyle}">${content}</div>`;
  }
  const text = stickerFieldText(el.field, el.text, label);
  if (!text) return '';
  const align = el.align === 'center' ? 'center' : el.align === 'right' ? 'right' : 'left';
  const style = `${baseStyle} font-size:${el.font_size_mm || 3.5}mm; text-align:${align}; font-weight:${el.bold ? 700 : 400};`;
  return `<div class="sticker-el" data-css="${style}">${escapeHTMLText(text)}</div>`;
}

// renderPrintSheet is the browser @media print fallback for both the manual
// SKU-scan flow (a flat `copies` applies to every label) and the Stage 52
// document-driven flow (each label carries its own resolved `qty`, which
// wins when present - mirrors BuildStickerPayload's identical precedence in
// engines/print_agent_payload.go). A label with no resolved template (template
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
  sel.outerHTML = picker.reasonSelectHTML(reasons, { value, attrs: 'id="sticker-reprint-code" data-css="max-width: 240px;"', label: 'Reprint reason code' });
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


export { renderAgentSetupPanel, agentRefreshStatus, agentDetectPrinters, agentTestPrint, agentPrintPickedDocument, renderStickersView, renderStickerPrintTab, renderStickerSourcePanel, loadStickerSourcePreview, renderStickerSourceLines, printStickerSourceLines, addStickerSKU, removeStickerSKU, renderStickerSKUList, printStickers, stickerFieldText, renderStickerElementHTML, printStickerRun };

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
