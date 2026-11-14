// PrintBridge (QZ) connect, print and job log, and the browser print sheet (164.4):
// moved out of app.js; app.js keeps lazy stubs with the same names. qzLastError stays
// in app.js (view-printing.js reads it). Loaded on the first print.
export async function qzTryConnect(silent = true) {
  if (!window.QZPrint) return false;
  try {
    await window.QZPrint.connect();
    qzLastError = '';
    return true;
  } catch (err) {
    qzLastError = err.message || String(err);
    if (!silent) await showCustomAlert(qzLastError, 'QZ Tray Not Available');
    return false;
  }
}

// Records what actually reached the printer. Best-effort: a failed log write
// must never surface as a failed print, because the label is already out.
export async function qzLogJob(entry) {
  try {
    await apiFetch('/api/v1/print/qz/log', { method: 'POST', body: JSON.stringify(entry) });
  } catch (err) {
    console.debug('[QZ] print log write failed', err);
  }
}

/**
 * One-click print. Asks the server what to print (and on which printer),
 * hands it to QZ Tray, and records the outcome.
 *
 * @param jobType 'Shipping Label' | 'Sticker' | 'Receipt' | 'Invoice' | 'Purchase Order' | 'Document'
 * @param opts    { documentRef, printerCode, copies, skus, reprintReason,
 *                  dataBase64, docFormat, quiet }
 *
 * `quiet` (31.1.9) suppresses the dialog when the server cannot prepare the
 * job, for the call sites that resolve a printer by *role* rather than by an
 * explicit pick. "No Printer record is Default For Receipt" is the normal
 * state of a tenant that has not set QZ up at all - it must fall through to
 * the browser print sheet silently, not put an error in front of the cashier
 * on every sale. A failure at the tray itself is still shown either way:
 * that one means printing was really attempted and really failed.
 *
 * @returns true if it printed, false if the caller should fall back.
 */
export async function qzTryPrint(jobType, opts = {}) {
  // Stage 127.2: opts.why, when given, is told why nothing printed.
  const fail = (reason, setup) => { if (opts.why) Object.assign(opts.why, { reason, setup }); return false; };
  if (!await qzTryConnect()) return fail(qzLastError ? `PrintBridge is not reachable (${qzLastError}).` : 'PrintBridge is not reachable.');
  // Stage 98.4: this computer's own printer per kind of print (print-routing.js).
  const routing = await loadViewModule('./print-routing.js');

  const res = await apiFetch('/api/v1/print/qz/payload', {
    method: 'POST',
    body: JSON.stringify({
      job_type: jobType,
      document_ref: opts.documentRef || '',
      printer_code: opts.printerCode || '',
      copies: opts.copies || 1,
      skus: opts.skus || [],
      reprint_reason: opts.reprintReason || '',
      data_base64: opts.dataBase64 || '',
      doc_format: opts.docFormat || '',
      // Stage 52: bulk print from a GRN/Transfer Order - source_doctype +
      // source_doc_id tell the server to resolve the document's own lines
      // (engines.PrintStickersForDocument) instead of requiring skus.
      source_doctype: opts.sourceDoctype || '',
      source_doc_id: opts.sourceDocId || '',
      copies_override: opts.copiesOverride || undefined,
      // Stage 52.8: per-line selection (sku + batch_no + copies). A SKU
      // received on two lots is two lines, which skus/copies_override
      // cannot tell apart; the server accepts either shape.
      lines: opts.lines || undefined,
      computer: await routing.printComputer(),
      location: opts.location || (window.erpWorkingStore || {}).code || '',
      print_kind: opts.printKind || ''
    })
  });
  if (!res) return fail('The server could not be reached. Check this computer is online, then Retry.', 1);
  if (!res.ok) {
    if (!opts.printerCode && await routing.needsChoice(res)) return routing.chooseAndPrint(jobType, opts);
    if (opts.quiet) return fail(await getErrorMessage(res, 'no print payload'), 1);
    await showApiError(res, 'Could not prepare the print job.', 'Print Failed');
    return false;
  }

  const payload = await res.json();

  // A non-thermal sticker printer has no raw command form; the server says
  // so and the existing browser sheet renders it instead.
  if (payload.fallback === 'browser') {
    renderPrintSheet(payload.labels, opts.copies || 1);
    return true;
  }

  const printerName = (payload.printer && payload.printer.qz_printer_name) || '';
  if (!printerName) {
    const why = routing.notMapped((payload.printer && payload.printer.name) || opts.printerCode);
    if (opts.quiet) return fail(why + ' Then Retry.', 1);
    await showCustomAlert(why, 'Printer Not Mapped');
    return false;
  }

  try {
    await window.QZPrint.printItems(printerName, payload.items, payload.copies);
  } catch (err) {
    await qzLogJob({
      job_type: jobType, document_ref: opts.documentRef || '', printer_code: payload.printer.code,
      qz_printer_name: printerName, print_format: payload.format, copies: payload.copies,
      status: 'Failed', error_detail: err.message || String(err)
    });
    if (opts.quiet) return fail(`The printer refused the job: ${err.message || String(err)}.`);
    await showCustomAlert(err.message || String(err), 'Print Failed');
    return false;
  }

  await qzLogJob({
    job_type: jobType, document_ref: opts.documentRef || '', printer_code: payload.printer.code,
    qz_printer_name: printerName, print_format: payload.format, copies: payload.copies,
    status: 'Submitted', error_detail: ''
  });
  showToast(`Sent to ${printerName}.`, { variant: 'success' });
  return true;
}

/**
 * Prints a marketplace-issued label or invoice exactly as the channel
 * produced it. Myntra, and every other channel that hands back a finished
 * PDF, goes through here - the file is passed to the printer untouched,
 * because re-rendering a courier's label risks altering a barcode they scan.
 */
export async function qzPrintMarketplaceDocument(file, opts = {}) {
  if (!file) return false;
  const base64 = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '');
    reader.onerror = () => reject(new Error('Could not read the file.'));
    reader.readAsDataURL(file);
  });
  const isPDF = /\.pdf$/i.test(file.name) || file.type === 'application/pdf';
  return qzTryPrint('Document', {
    documentRef: opts.documentRef || file.name,
    printerCode: opts.printerCode || '',
    copies: opts.copies || 1,
    dataBase64: base64,
    docFormat: opts.docFormat || (isPDF ? 'pdf' : 'command')
  });
}

// Print Setup panel. Lives on the Sticker Printing screen because that is
// already where Printer records are managed. Its job is the one thing the
// generic Master form cannot do: ask the local machine what its printers are
// actually called, since "OS Printer Name" has to match verbatim (a Zebra
// commonly reports as e.g. "ZDesigner ZD220-203dpi ZPL").
function renderPrintSheet(labels, copies) {
  const area = document.getElementById('sticker-print-area');
  let html = '';
  labels.forEach((label, idx) => {
    const labelCopies = label.qty > 0 ? label.qty : copies;
    const nextCategory = idx < labels.length - 1 ? (labels[idx + 1].category || '') : null;
    const isLastOfCategoryGroup = nextCategory === null || nextCategory !== (label.category || '');
    let elements = [];
    if (label.template_elements) {
      try { elements = JSON.parse(label.template_elements); } catch (e) { elements = []; }
    }
    for (let i = 0; i < labelCopies; i++) {
      const breakClass = (i === labelCopies - 1 && isLastOfCategoryGroup) ? ' sticker-label-group-end' : '';
      if (elements.length > 0 && label.label_width_mm && label.label_height_mm) {
        html += `
          <div class="sticker-label sticker-label-templated${breakClass}" style="width:${label.label_width_mm}mm; height:${label.label_height_mm}mm;">
            ${elements.map(el => renderStickerElementHTML(el, label)).join('')}
          </div>
        `;
      } else {
        html += `
          <div class="sticker-label${breakClass}">
            <div class="sticker-name">${escapeHTMLText(label.name || label.sku)}</div>
            <div class="sticker-barcode">${label.barcode_svg || escapeHTMLText(label.barcode || '')}</div>
            <div class="sticker-meta">SKU: ${escapeHTMLText(label.sku)}${label.hsn_code ? ' &nbsp;|&nbsp; HSN: ' + escapeHTMLText(label.hsn_code) : ''}</div>
          </div>
        `;
      }
    }
  });
  area.innerHTML = html;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}
