// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
async function renderApprovalsView(container) {
  const res = await apiFetch('/api/v1/approval/pending');
  // BLD-036: apiFetch already surfaces its own connection/timeout dialog on a
  // hard network failure - returning silently here used to leave the screen
  // blank underneath it, with no way back in except navigating away and
  // back. renderErrorPanel gives a real, in-place retry instead.
  if (!res) {
    renderErrorPanel(container, 'Could not reach the server to load pending approvals.', () => renderView('approvals'));
    return;
  }

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Approvals</h1>
      <p class="page-subtitle">Documents awaiting your sign-off.</p>
    </div>
  `;
  container.appendChild(header);

  if (!res.ok) {
    // BLD-036: same as the !res branch above - was a dead-end panel with no
    // way to retry short of navigating away and back.
    renderErrorPanel(container, 'Failed to load pending approvals.', () => renderView('approvals'));
    return;
  }

  const items = await res.json();
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Record Type</th>
          <th>Document ID</th>
          <th>Amount</th>
          <th>Location</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (!items || items.length === 0) {
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">Nothing awaiting approval.</td></tr>`;
  }
  (items || []).forEach(item => {
    const amount = item.total_amount ?? item.amount ?? '';
    const loc = item.location || item.location_code || '';
    html += `
      <tr>
        <td>${escapeHTMLText(getDoctypeLabel(item.doctype))}</td>
        <td style="font-family: monospace;">${item.id}</td>
        <td>${amount !== '' ? Number(amount).toLocaleString() : ''}</td>
        <td>${loc}</td>
        <td>
          <button class="action-btn" ${actionAttrs('decideApproval', [ACTION_ARG_ELEMENT, item.doctype, item.id, 'Approved'])}>Approve</button>
          <button class="action-btn action-btn-danger" ${actionAttrs('decideApproval', [ACTION_ARG_ELEMENT, item.doctype, item.id, 'Rejected'])}>Reject</button>
        </td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// BLD-036: guards Approve/Reject the same way the generic form and PO
// composer are guarded - a double-click used to be able to fire the same
// decision twice (harmless server-side on Approve, since DecideApproval's own
// row lock + status check makes a second one a no-op, but it still surfaced a
// confusing second error toast for no reason).
async function decideApproval(button, doctype, documentId, decision) {
  const busyLabel = decision === 'Approved' ? 'Approving...' : 'Rejecting...';
  await guardAgainstDoubleSubmit(button, busyLabel, () => decideApprovalInner(doctype, documentId, decision));
}

async function decideApprovalInner(doctype, documentId, decision) {
  let comment = '';
  if (decision === 'Rejected') {
    comment = (await showCustomPrompt('Reason for rejection (optional):')) || '';
  }
  const res = await apiFetch('/api/v1/approval/decide', {
    method: 'POST',
    body: JSON.stringify({ doctype, document_id: documentId, decision, comment })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record decision.');
    return;
  }
  // 49.2.4: a decision hook can carry a result the approver must see right
  // now and nowhere else - e.g. PasswordResetRequest's one-time password,
  // which this response is the only place it is ever shown. Generic on
  // purpose (keyed on `detail` being present, not on doctype), so a future
  // hook can use the same path with no frontend change.
  const data = await res.json();
  if (data && data.detail) {
    await showCustomAlert(data.detail, 'Decision Recorded');
  } else {
    // BLD-035 (MC-042): this used to re-render the queue with zero
    // confirmation - approving a VendorInvoice override actually finalizes it
    // straight to Paid (see FinalizeVendorInvoiceOverridePayment), so showing
    // just "Approved" here would still be untruthful about the real outcome.
    // document_status is the row's real post-decision status (new field on
    // this response, see handleDecideApproval) - fall back to the bare
    // decision only if that lookup came back empty.
    const label = getDoctypeLabel(doctype);
    const finalStatus = data && data.document_status;
    const message = finalStatus
      ? describeDocumentStatusOutcome(label, finalStatus)
      : `${label} ${decision === 'Approved' ? 'approved' : 'rejected'}.`;
    showToast(message, { variant: decision === 'Rejected' ? 'warning' : 'success' });
  }
  renderView('approvals');
}

// ---------------------------------------------------------------------------
// Purchase Orders screen (Stage 13.8's maker side, rebuilt in Stage 40.1).
//
// This screen used to ask for a vendor, a warehouse and one hand-typed
// "Total Amount", and posted items: '[]'. A PO therefore recorded what it
// cost but never what was being bought - so GRN receipt had nothing to match,
// the GST engine had nothing to classify, and there was nothing to send a
// vendor. It now edits real lines.
//
// Everything derived is derived server-side, by design: HSN, GST rate,
// per-line tax and the inter-state decision all come back from
// /api/v1/procurement/purchase-order/preview, which runs the same engine
// functions the save path runs. Nothing here recomputes tax in JavaScript,
// because a second implementation is exactly how a screen ends up showing a
// total the saved document disagrees with.
// ---------------------------------------------------------------------------

// The PO being composed or amended. Module-level rather than passed around so
// the line editor, the preview debounce and the save handler all read one
// object - the same shape the API takes, so saving is a POST of this plus the
// serialised lines and no field-by-field assembly.
let poDraft = null;
let poPreview = null;
let poPreviewTimer = null;

function newPODraft() {
  return {
    id: '', vendor: '', target_warehouse: '', location: '',
    // '' means "inherit the tenant default", which the first preview response
    // resolves and displays - the screen never hardcodes Exclusive itself.
    gst_mode: '',
    interstate: false, interstate_override: false,
    lines: [{ sku: '', qty: '', rate: '', mrp: '' }],
    wasApproved: false, version: null
  };
}

async function renderPurchaseOrdersView(container) {
  const res = await apiFetch('/api/v1/doc/PurchaseOrder');
  if (!res) return;

  if (!poDraft) poDraft = newPODraft();

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Purchase Order</h1>
      <p class="page-subtitle">Pick items, set purchase prices, then submit for approval. GST and the supply type are worked out for you.</p>
    </div>
  `;
  container.appendChild(header);

  const ordersLoadFailed = !res.ok;
  const orders = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel po-composer';
  formPanel.id = 'po-composer';
  container.appendChild(formPanel);
  renderPOComposer(formPanel);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  // BLD-036: the composer above still works even if this list failed to
  // load, so this stays a partial-section message rather than a full
  // renderErrorPanel takeover - but it used to have no way back short of
  // navigating away and back.
  let html = ordersLoadFailed
    ? `<p style="padding: 16px; color: var(--danger-color); font-size: 13px;">Failed to load existing purchase orders. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['purchase-orders'])}>Try Again</button></p>`
    : '';
  html += `
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>PO Number</th>
          <th>Vendor</th>
          <th>Location</th>
          <th>Items</th>
          <th class="num">Taxable</th>
          <th class="num">Grand Total</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (orders.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No purchase orders yet. Add a vendor and at least one item above, then <b>Create Draft</b>.</td></tr>`;
  }
  orders.forEach(po => {
    const statusBadge = po.status === 'Approved' ? 'badge-success'
      : po.status === 'Rejected' ? 'badge-danger'
      : po.status === 'Pending Approval' ? 'badge-warning'
      : 'badge-secondary';
    let lineCount = 0;
    try { lineCount = (JSON.parse(po.items || '[]') || []).length; } catch (e) { lineCount = 0; }
    const poNumber = po.po_number || po.code || po.id;
    // Sent-to-vendor is shown next to the status rather than as its own
    // column: it is the answer to "did this actually go out?", which is only
    // ever asked about a PO that is already approved.
    const sent = po.sent_to_vendor_at
      ? `<div class="po-sent-stamp" title="Sent ${escapeHTMLText(po.sent_to_vendor_at)}">Sent to vendor</div>` : '';
    html += `
      <tr>
        <td style="font-family: monospace;">${copyableCell(poNumber, poNumber)}</td>
        <td>${escapeHTMLText(po.vendor || '')}</td>
        <td>${escapeHTMLText(po.location || '')}</td>
        <td>${lineCount === 0 ? '<span class="po-no-lines" title="This PO was raised before line items existed, or was created through the API without them.">No lines</span>' : `${lineCount} item${lineCount === 1 ? '' : 's'}`}</td>
        <td class="num">${formatMoney(po.total_amount)}</td>
        <td class="num">${po.grand_total != null ? formatMoney(po.grand_total) : '<span class="text-muted">&mdash;</span>'}</td>
        <td><span class="badge ${statusBadge}">${escapeHTMLText(po.status || '')}</span>${sent}</td>
        <td class="po-row-actions">
          ${po.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('submitPOForApproval', [po.id])}>Submit for Approval</button>` : ''}
          ${po.status !== 'Closed' ? `<button class="action-btn" ${actionAttrs('amendPurchaseOrder', [po.id])}>Amend</button>` : ''}
          <button class="action-btn" ${actionAttrs('printPurchaseOrder', [po.id])}>Print</button>
          ${po.status !== 'Draft' ? `<button class="action-btn" ${actionAttrs('sendPurchaseOrderToVendor', [po.id])}>Send to Vendor</button>` : ''}
        </td>
      </tr>
    `;
  });
  html += `</tbody></table></div>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// formatMoney groups the Indian way (12,34,567.89), matching the printed PO
// and engines/amount_words.go's FormatIndianCurrency. en-IN is a built-in
// locale, so this needs no table of its own.
function renderPOComposer(panel) {
  const d = poDraft;
  const editing = !!d.id;
  panel.innerHTML = `
    <div class="po-composer-head">
      <h2>${editing ? `Amend ${escapeHTMLText(d.po_number || d.id)}` : 'New Purchase Order'}</h2>
      ${editing ? `<button class="btn btn-ghost btn-sm" id="po-cancel-edit">Cancel</button>` : ''}
    </div>

    <div class="po-header-grid">
      ${editing ? '' : autoNumberField('PO Number', 'PO', '100%')}
      <div class="form-group">
        <label class="form-label" for="po-vendor">Vendor</label>
        <erp-typeahead id="po-vendor" doctype="Vendor"></erp-typeahead>
      </div>
      <div class="form-group">
        <label class="form-label" for="po-location">Location (billing entity)</label>
        <input type="text" id="po-location" class="form-input" value="${escapeHTMLText(d.location)}">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-warehouse">Target Warehouse (ship to)</label>
        <input type="text" id="po-warehouse" class="form-input" value="${escapeHTMLText(d.target_warehouse)}">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-gst-mode">GST treatment of purchase price</label>
        <select id="po-gst-mode" class="form-input">
          <option value="">Tenant default</option>
          <option value="Exclusive"${d.gst_mode === 'Exclusive' ? ' selected' : ''}>Exclusive &mdash; GST added on top</option>
          <option value="Inclusive"${d.gst_mode === 'Inclusive' ? ' selected' : ''}>Inclusive &mdash; price already has GST</option>
        </select>
      </div>
    </div>

    <div id="po-supply-banner" class="po-supply-banner"></div>

    <div class="po-lines-head">
      <h3>Items</h3>
      <button class="btn btn-outline btn-sm" id="po-add-line" type="button">+ Add item</button>
    </div>
    <div class="table-wrapper">
      <table class="po-lines">
        <thead>
          <tr>
            <th style="min-width:190px;">Item</th>
            <th class="num" style="width:80px;">Qty</th>
            <th class="num" style="width:120px;">Purchase Price</th>
            <th class="num" style="width:110px;">MRP <span class="po-optional">optional</span></th>
            <th style="width:90px;">HSN</th>
            <th class="num" style="width:70px;">GST %</th>
            <th class="num" style="width:110px;">Taxable</th>
            <th class="num" style="width:100px;">Tax</th>
            <th class="num" style="width:120px;">Line Total</th>
            <th style="width:36px;"></th>
          </tr>
        </thead>
        <tbody id="po-lines-body"></tbody>
      </table>
    </div>

    <div class="po-footer">
      <div id="po-totals" class="po-totals"></div>
      <div class="po-actions">
        <div id="po-form-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="po-create-btn">${editing ? 'Save Amendment' : 'Create Draft'}</button>
      </div>
    </div>
  `;

  const vendorEl = document.getElementById('po-vendor');
  vendorEl.value = d.vendor || '';
  vendorEl.addEventListener('change', () => { d.vendor = vendorEl.value.trim(); schedulePOPreview(); });
  // <erp-typeahead> fires `change` on pick, but a typed-and-blurred value has
  // to be caught too, or a vendor typed by hand never reaches the preview and
  // the supply type silently stays underived.
  vendorEl.addEventListener('blur', () => { if (vendorEl.value.trim() !== d.vendor) { d.vendor = vendorEl.value.trim(); schedulePOPreview(); } });

  const locEl = document.getElementById('po-location');
  const whEl = document.getElementById('po-warehouse');
  attachLinkTypeahead(locEl, 'Location');
  attachLinkTypeahead(whEl, 'Location');
  locEl.addEventListener('change', () => { d.location = locEl.value.trim(); schedulePOPreview(); });
  locEl.addEventListener('blur', () => { d.location = locEl.value.trim(); schedulePOPreview(); });
  whEl.addEventListener('change', () => { d.target_warehouse = whEl.value.trim(); });
  whEl.addEventListener('blur', () => { d.target_warehouse = whEl.value.trim(); });

  document.getElementById('po-gst-mode').addEventListener('change', (e) => {
    d.gst_mode = e.target.value;
    schedulePOPreview();
  });

  document.getElementById('po-add-line').addEventListener('click', () => {
    d.lines.push({ sku: '', qty: '', rate: '', mrp: '' });
    renderPOLines();
  });
  document.getElementById('po-create-btn').addEventListener('click', savePurchaseOrder);
  const cancelBtn = document.getElementById('po-cancel-edit');
  if (cancelBtn) cancelBtn.addEventListener('click', () => { poDraft = newPODraft(); poPreview = null; renderView('purchase-orders'); });

  renderPOLines();
}

// renderPOLines redraws the line rows and reattaches their handlers.
//
// Full redraw rather than surgical row patching because a line's derived
// columns (HSN, GST %, tax) all change together when the preview comes back,
// and the row count is small enough - a PO with hundreds of lines is a CSV
// import, not something typed here.
let poLinesRebuilding = false;
function renderPOLines() {
  const body = document.getElementById('po-lines-body');
  if (!body) return;
  const d = poDraft;
  const previewLines = (poPreview && poPreview.lines) || [];
  // Stage 57 (SOP recording): a price preview can land while someone is
  // typing in a line, and this rebuilds every row - which destroyed the box
  // being typed in, cutting the text off mid-word. Keep the focused line
  // field's typed text, caret and focus across the rebuild.
  const focused = body.contains(document.activeElement) ? document.activeElement : null;
  const keep = focused && focused.matches('[data-po-field]') ? {
    line: focused.closest('[data-po-line]')?.getAttribute('data-po-line'),
    field: focused.getAttribute('data-po-field'),
    text: typedTextOf(focused),
    start: focused.selectionStart,
    end: focused.selectionEnd
  } : null;

  // The rebuild below removes the focused input, which fires its blur; that
  // blur must not commit, or it re-prices, re-renders and blurs again - a loop
  // that ran every few hundred ms while someone typed.
  poLinesRebuilding = true;
  body.innerHTML = d.lines.map((line, i) => {
    const p = previewLines[i] || {};
    const err = p.error ? `<div class="po-line-error">${escapeHTMLText(p.error)}</div>` : '';
    const derived = (v, suffix = '') => (v === undefined || v === null || v === '' ? '<span class="text-muted">&mdash;</span>' : escapeHTMLText(String(v)) + suffix);
    return `
      <tr data-po-line="${i}"${p.error ? ' class="po-line-flagged"' : ''}>
        <td>
          <input type="text" class="form-input" data-po-field="sku" value="${escapeHTMLText(line.sku)}" placeholder="Search item...">
          ${p.item_name ? `<div class="po-line-name">${escapeHTMLText(p.item_name)}</div>` : ''}
          ${err}
        </td>
        <td><input type="number" class="form-input num" data-po-field="qty" min="1" step="1" value="${escapeHTMLText(line.qty)}"></td>
        <td><input type="number" class="form-input num" data-po-field="rate" min="0" step="0.01" value="${escapeHTMLText(line.rate)}"></td>
        <td><input type="number" class="form-input num" data-po-field="mrp" min="0" step="0.01" value="${escapeHTMLText(line.mrp)}" placeholder="&mdash;"></td>
        <td class="po-derived">${derived(p.hsn_code)}</td>
        <td class="po-derived num">${p.gst_rate ? escapeHTMLText(String(p.gst_rate)) + '%' : (p.tax_treatment && p.tax_treatment !== 'Taxable' ? escapeHTMLText(p.tax_treatment) : '<span class="text-muted">&mdash;</span>')}</td>
        <td class="po-derived num">${p.taxable ? formatMoney(p.taxable) : '<span class="text-muted">&mdash;</span>'}</td>
        <td class="po-derived num">${p.tax_amount ? formatMoney(p.tax_amount) : '<span class="text-muted">&mdash;</span>'}</td>
        <td class="po-derived num po-line-total">${p.line_total ? formatMoney(p.line_total) : '<span class="text-muted">&mdash;</span>'}</td>
        <td><button type="button" class="po-line-remove" data-po-remove="${i}" title="Remove this line" aria-label="Remove line ${i + 1}">&times;</button></td>
      </tr>
    `;
  }).join('');

  body.querySelectorAll('[data-po-line]').forEach(tr => {
    const i = Number(tr.getAttribute('data-po-line'));
    const skuInput = tr.querySelector('[data-po-field="sku"]');
    attachLinkTypeahead(skuInput, 'Item');
    tr.querySelectorAll('[data-po-field]').forEach(input => {
      const field = input.getAttribute('data-po-field');
      const commit = () => {
        if (poLinesRebuilding || !input.isConnected) return;
        const v = input.value.trim();
        if (poDraft.lines[i][field] === v) return;
        poDraft.lines[i][field] = v;
        schedulePOPreview();
      };
      input.addEventListener('change', commit);
      input.addEventListener('blur', commit);
    });
  });

  poLinesRebuilding = false;

  if (keep) {
    const again = body.querySelector(`[data-po-line="${keep.line}"] [data-po-field="${keep.field}"]`);
    if (again) {
      // Restore what is visibly typed, not the committed value: the user is
      // still mid-entry, and the typeahead searches from the visible text.
      const native = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
      if (typedTextOf(again) !== keep.text) {
        native.set.call(again, keep.text);
        again.dispatchEvent(new Event('input', { bubbles: true }));
      }
      again.focus();
      try { if (keep.start !== null) again.setSelectionRange(keep.start, keep.end); } catch (e) { /* number inputs have no caret */ }
    }
  }

  body.querySelectorAll('[data-po-remove]').forEach(btn => {
    btn.addEventListener('click', () => {
      const i = Number(btn.getAttribute('data-po-remove'));
      poDraft.lines.splice(i, 1);
      if (poDraft.lines.length === 0) poDraft.lines.push({ sku: '', qty: '', rate: '', mrp: '' });
      renderPOLines();
      schedulePOPreview();
    });
  });

  renderPOTotals();
}

// schedulePOPreview debounces the pricing call. 250ms is long enough that
// typing a 6-digit price is one request rather than six, and short enough
// that the totals land before the eye moves to them.
function schedulePOPreview() {
  clearTimeout(poPreviewTimer);
  poPreviewTimer = setTimeout(runPOPreview, 250);
}

async function runPOPreview() {
  const d = poDraft;
  if (!d) return;
  const res = await apiFetch('/api/v1/procurement/purchase-order/preview', {
    method: 'POST',
    body: JSON.stringify(poDraftPayload(d))
  });
  if (!res) return;
  if (!res.ok) {
    // A preview failure is never fatal - the maker can still save and get the
    // authoritative error from the save path. Showing the derived columns as
    // blank is the honest outcome.
    poPreview = null;
    renderPOLines();
    return;
  }
  poPreview = await res.json();
  // The server resolves '' to the tenant default; reflect what it actually
  // chose so the select stops saying "Tenant default" without saying which.
  const modeSel = document.getElementById('po-gst-mode');
  if (modeSel && !d.gst_mode && poPreview.gst_mode) {
    const opt = modeSel.querySelector('option[value=""]');
    if (opt) opt.textContent = `Tenant default (${poPreview.gst_mode})`;
  }
  renderPOLines();
  renderPOSupplyBanner();
}

// poDraftPayload is the one place the draft becomes an API body, so the
// preview call and the save call cannot drift into sending different shapes.
function poDraftPayload(d) {
  const lines = d.lines
    .filter(l => l.sku || l.qty || l.rate)
    .map(l => ({
      sku: l.sku || '',
      qty: Number(l.qty) || 0,
      rate: Number(l.rate) || 0,
      ...(l.mrp !== '' && l.mrp != null ? { mrp: Number(l.mrp) || 0 } : {})
    }));
  return {
    vendor: d.vendor,
    vendor_id: d.vendor,
    target_warehouse: d.target_warehouse,
    location: d.location,
    items: JSON.stringify(lines),
    gst_mode: d.gst_mode,
    interstate: d.interstate,
    interstate_override: d.interstate_override
  };
}

// renderPOSupplyBanner shows what the two addresses decided, and offers the
// override. This is the visible half of "interstate is worked out from the
// addresses": if it cannot be worked out, the banner says exactly which
// master is missing a GSTIN rather than silently defaulting to intra-state.
function renderPOSupplyBanner() {
  const el = document.getElementById('po-supply-banner');
  if (!el) return;
  const pos = poPreview && poPreview.place_of_supply;
  const d = poDraft;

  if (!pos || (!pos.derived && !d.vendor && !d.location)) {
    el.className = 'po-supply-banner po-supply-idle';
    el.innerHTML = `<span>Pick a vendor and a location &mdash; the supply type is worked out from their states.</span>`;
    return;
  }

  if (!pos.derived) {
    el.className = 'po-supply-banner po-supply-warn';
    el.innerHTML = `
      <span><strong>Supply type could not be derived</strong> &mdash; ${escapeHTMLText(pos.reason || 'a state is missing')}.
      Set it manually below, or add the missing GSTIN/state on the master.</span>
      <label class="po-override"><input type="checkbox" id="po-interstate" ${d.interstate ? 'checked' : ''}> Inter-state (IGST)</label>`;
  } else {
    const kind = pos.interstate ? 'Inter-state' : 'Intra-state';
    const tax = pos.interstate ? 'IGST' : 'CGST + SGST';
    el.className = `po-supply-banner ${d.interstate_override ? 'po-supply-warn' : 'po-supply-ok'}`;
    el.innerHTML = `
      <span><strong>${kind} (${tax})</strong> &mdash; vendor in ${escapeHTMLText(pos.vendor_state_label || '?')}, billing entity in ${escapeHTMLText(pos.buyer_state_label || '?')}.</span>
      <label class="po-override"><input type="checkbox" id="po-interstate-override" ${d.interstate_override ? 'checked' : ''}> Override</label>
      ${d.interstate_override ? `<label class="po-override"><input type="checkbox" id="po-interstate" ${d.interstate ? 'checked' : ''}> Inter-state (IGST)</label>` : ''}`;
  }

  const overrideBox = document.getElementById('po-interstate-override');
  if (overrideBox) overrideBox.addEventListener('change', (e) => {
    d.interstate_override = e.target.checked;
    // Seed the manual flag from what was derived, so ticking Override does
    // not flip the tax treatment as a side effect of opening the control.
    if (d.interstate_override && pos.derived) d.interstate = pos.interstate;
    runPOPreview();
  });
  const interBox = document.getElementById('po-interstate');
  if (interBox) interBox.addEventListener('change', (e) => {
    d.interstate = e.target.checked;
    if (pos && pos.derived) d.interstate_override = true;
    runPOPreview();
  });
}

function renderPOTotals() {
  const el = document.getElementById('po-totals');
  if (!el) return;
  const p = poPreview;
  if (!p || !p.breakdown || (!p.breakdown.taxable_amount && !p.grand_total)) {
    el.innerHTML = `<div class="po-total-hint">Add an item to see the tax breakdown.</div>`;
    return;
  }
  const b = p.breakdown;
  const row = (label, value, cls = '') => `<div class="po-total-row ${cls}"><span>${label}</span><span>${formatMoney(value)}</span></div>`;
  const nonTaxable = (b.exempt_amount || 0) + (b.nil_rated_amount || 0) + (b.zero_rated_amount || 0);
  el.innerHTML = `
    ${row('Taxable value', b.taxable_amount)}
    ${nonTaxable ? row('Exempt / nil / zero-rated', nonTaxable) : ''}
    ${b.interstate ? row(`IGST`, b.igst) : row(`CGST`, b.cgst) + row(`SGST`, b.sgst)}
    ${row('Grand total', p.grand_total, 'po-total-grand')}
    <div class="po-total-mode">Prices entered are <strong>${escapeHTMLText(p.gst_mode || '')}</strong> of GST.</div>
  `;
}

async function savePurchaseOrder() {
  // BLD-036: same double-submit guard as the generic record form - a slow
  // save (or a repeat click on "Save Purchase Order" before the first
  // response returns) used to be able to create two POs from one intent.
  await guardAgainstDoubleSubmit(document.getElementById('po-create-btn'), 'Saving...', savePurchaseOrderInner);
}

async function savePurchaseOrderInner() {
  const errorEl = document.getElementById('po-form-error');
  errorEl.classList.add('hidden');
  const d = poDraft;
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };

  if (!d.vendor || !d.target_warehouse || !d.location) {
    fail('Vendor, Location and Target Warehouse are all required.');
    return;
  }
  const payload = poDraftPayload(d);
  const lines = JSON.parse(payload.items);
  if (lines.length === 0) {
    fail('Add at least one item - a purchase order with no lines cannot be received against.');
    return;
  }
  const bad = lines.findIndex(l => !l.sku || l.qty <= 0);
  if (bad !== -1) {
    fail(`Line ${bad + 1} needs an item and a quantity of at least 1.`);
    return;
  }
  if (poPreview && poPreview.blocking) {
    fail('One or more lines could not be priced - see the red rows above. Usually the Item is missing its HSN code or GST rate.');
    return;
  }

  // The generic document validator checks PurchaseOrder.total_amount before
  // the server-side GST handler derives the canonical value from `items`.
  // Supply the last server-previewed taxable/non-taxable amount to satisfy
  // that required-field check; the save handler recomputes and overwrites it
  // from the submitted lines before persistence, so this is never the source
  // of truth. A zero fallback is safe for a preview outage for the same
  // reason: the save path still performs its authoritative calculation.
  const breakdown = poPreview && poPreview.breakdown;
  payload.total_amount = breakdown
    ? (Number(breakdown.taxable_amount) || 0) +
      (Number(breakdown.exempt_amount) || 0) +
      (Number(breakdown.nil_rated_amount) || 0) +
      (Number(breakdown.zero_rated_amount) || 0)
    : 0;

  if (d.id) {
    if (d.wasApproved) {
      if (!await showCustomConfirm('This PO is Approved. Amending it will reset it to Pending Approval for re-approval. Continue?', 'Amend Purchase Order')) return;
      // PURCHA-0085: the server requires a reason when an Approved PO's
      // items/total actually change. Collected unconditionally here (rather
      // than trying to mirror the server's own before/after diff) so a save
      // never fails on a missing field the composer had no way to fill in -
      // sending it on an edit the server doesn't require it for is harmless.
      const amendReason = await showCustomPrompt('Reason for this amendment:', '');
      if (amendReason === null) return;
      payload.amendment_reason = amendReason;
    }
    if (typeof d.version === 'number') payload.expected_version = d.version;
    payload.status = d.status || 'Draft';
  } else {
    // The PO number is issued server-side from the PO series (Stage 30.6) and
    // is deliberately not sent.
    payload.status = 'Draft';
  }

  const url = d.id ? `/api/v1/doc/PurchaseOrder/${encodeURIComponent(d.id)}` : '/api/v1/doc/PurchaseOrder';
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    fail(await getErrorMessage(res, d.id ? 'Failed to save amendment - someone else may have edited this record, refresh and try again.' : 'Failed to create purchase order.'));
    return;
  }
  if (d.id && d.wasApproved) {
    await showCustomAlert('Purchase order amended. It now requires re-approval.', 'Amend Purchase Order');
  } else {
    // BLD-035: every other save through this composer (plain create, plain
    // edit) previously returned to the list with no confirmation at all.
    // Reads the document's real resulting status off the now-truthful
    // generic-doc-engine response instead of assuming Draft, since d.status
    // can carry a later status (e.g. re-saving a Rejected PO) forward.
    const savedData = await res.json().catch(() => null);
    const resultStatus = (savedData && savedData.status) || 'Draft';
    showToast(describeDocumentStatusOutcome(getDoctypeLabel('PurchaseOrder'), resultStatus), {
      variant: resultStatus === 'Pending Approval' ? 'warning' : 'success'
    });
  }
  poDraft = newPODraft();
  poPreview = null;
  renderView('purchase-orders');
}

// amendPurchaseOrder loads an existing PO back into the same composer.
//
// Stage 26.3.6 built this as a chain of four showCustomPrompt() dialogs,
// because the screen had no form worth reusing. It does now - and a prompt
// chain could never have edited the lines, which is the thing an amendment is
// usually about.
window.amendPurchaseOrder = async function(poId) {
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load purchase order for amendment.');
    return;
  }
  const record = await res.json();
  let lines = [];
  try { lines = JSON.parse(record.items || '[]') || []; } catch (e) { lines = []; }

  poDraft = {
    id: poId,
    po_number: record.po_number || record.code || poId,
    vendor: record.vendor || record.vendor_id || '',
    target_warehouse: record.target_warehouse || '',
    location: record.location || '',
    gst_mode: record.gst_mode || '',
    interstate: !!record.interstate,
    interstate_override: !!record.interstate_override,
    lines: lines.length ? lines.map(l => ({
      sku: l.sku || '', qty: l.qty ?? '', rate: l.rate ?? '', mrp: l.mrp ?? ''
    })) : [{ sku: '', qty: '', rate: '', mrp: '' }],
    wasApproved: record.status === 'Approved',
    status: record.status,
    version: typeof record.version === 'number' ? record.version : null
  };
  poPreview = null;
  renderView('purchase-orders');
  runPOPreview();
  const composer = document.getElementById('po-composer');
  if (composer) composer.scrollIntoView({ behavior: 'smooth', block: 'start' });
};

// ---------------------------------------------------------------------------
// Printed PO and vendor dispatch (Stage 40.1)
//
// The payload is assembled server-side (GET .../print) so the sheet does not
// have to fetch the vendor, the legal entity and every item and stitch them
// together itself - and so MRP is stripped before it ever reaches the page.
// ---------------------------------------------------------------------------
// Stage 40.10: silent-prints via QZ first (job_type 'Purchase Order', quiet -
// a tenant with no QZ printer configured for POs is the normal case, not an
// error). Falls back to the existing browser @media print sheet exactly like
// printSalesInvoice, so nobody is blocked when QZ Tray or a mapped printer
// isn't present.
window.printPurchaseOrder = async function(poId) {
  if (await qzTryPrint('Purchase Order', { documentRef: poId, quiet: true })) return;

  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/print`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to prepare the purchase order for printing.');
    return;
  }
  renderPOPrintSheet(await res.json());
};

function renderPOPrintSheet(po) {
  const area = document.getElementById('invoice-print-area');
  if (!area) return;
  const party = (title, p) => `
    <div class="po-print-party">
      <div class="po-print-party-title">${title}</div>
      <div class="po-print-party-name">${escapeHTMLText(p.name || '—')}</div>
      ${p.address ? `<div>${escapeHTMLText(p.address)}</div>` : ''}
      ${p.gstin ? `<div>GSTIN: ${escapeHTMLText(p.gstin)}</div>` : ''}
      ${p.state && p.state !== 'Not set' ? `<div>State: ${escapeHTMLText(p.state)}</div>` : ''}
      ${p.email ? `<div>${escapeHTMLText(p.email)}</div>` : ''}
      ${p.phone ? `<div>${escapeHTMLText(p.phone)}</div>` : ''}
    </div>`;

  const b = po.breakdown || {};
  // No MRP column: the server already zeroes it, and the buying side's
  // expected retail price is not the vendor's business.
  area.innerHTML = `
    <div class="invoice-sheet po-print">
      <div class="invoice-title">Purchase Order</div>
      ${po.status !== 'Approved' ? `<div class="invoice-draft">${escapeHTMLText((po.status || '').toUpperCase())}</div>` : ''}
      <div class="po-print-meta">
        <div><strong>PO No:</strong> ${escapeHTMLText(po.po_number || '')}</div>
        ${po.order_date ? `<div><strong>Date:</strong> ${escapeHTMLText(po.order_date)}</div>` : ''}
        ${po.ship_to ? `<div><strong>Ship to:</strong> ${escapeHTMLText(po.ship_to)}</div>` : ''}
      </div>
      <div class="po-print-parties">
        ${party('Buyer', po.buyer || {})}
        ${party('Vendor', po.vendor || {})}
      </div>
      <table class="po-print-lines">
        <thead><tr><th>#</th><th>Item</th><th>HSN</th><th class="num">Qty</th><th class="num">Rate</th><th class="num">GST %</th><th class="num">Taxable</th><th class="num">Amount</th></tr></thead>
        <tbody>
          ${(po.lines || []).map((l, i) => `
            <tr>
              <td>${i + 1}</td>
              <td>${escapeHTMLText(l.sku || '')}${l.item_name ? `<div class="po-print-itemname">${escapeHTMLText(l.item_name)}</div>` : ''}</td>
              <td>${escapeHTMLText(l.hsn_code || '')}</td>
              <td class="num">${escapeHTMLText(String(l.qty ?? ''))}</td>
              <td class="num">${formatMoney(l.rate)}</td>
              <td class="num">${l.gst_rate ? l.gst_rate + '%' : (l.tax_treatment && l.tax_treatment !== 'Taxable' ? escapeHTMLText(l.tax_treatment) : '—')}</td>
              <td class="num">${formatMoney(l.taxable)}</td>
              <td class="num">${formatMoney(l.line_total)}</td>
            </tr>`).join('')}
        </tbody>
      </table>
      <table class="po-print-totals">
        <tr><td>Taxable value</td><td class="num">${formatMoney(b.taxable_amount)}</td></tr>
        ${b.interstate
          ? `<tr><td>IGST</td><td class="num">${formatMoney(b.igst)}</td></tr>`
          : `<tr><td>CGST</td><td class="num">${formatMoney(b.cgst)}</td></tr><tr><td>SGST</td><td class="num">${formatMoney(b.sgst)}</td></tr>`}
        <tr class="po-print-grand"><td>Grand Total</td><td class="num">${formatMoney(po.grand_total)}</td></tr>
      </table>
      ${po.amount_in_words ? `<div class="po-print-words">${escapeHTMLText(po.amount_in_words)}</div>` : ''}
      ${po.place_of_supply && po.place_of_supply.derived ? `<div class="po-print-pos">Place of supply: ${escapeHTMLText(po.place_of_supply.buyer_state_label || '')}</div>` : ''}
      <div class="po-print-foot">
        <div>Prices are <strong>${escapeHTMLText(po.gst_mode || '')}</strong> of GST.</div>
        <div class="po-print-sign">Authorised Signatory</div>
      </div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// sendPurchaseOrderToVendor records the dispatch and fires the notification
// engine's PurchaseOrderIssued event. When no notification channel is
// configured - the normal state of a tenant that has not set one up - it falls
// back to the user's own mail client rather than dead-ending.
window.sendPurchaseOrderToVendor = async function(poId) {
  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/send`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to send this purchase order.');
    return;
  }
  const data = await res.json();
  const po = data.purchase_order || {};
  if (data.vendor_email) {
    const subject = `Purchase Order ${po.po_number || poId}`;
    const body = [
      `Please find our purchase order ${po.po_number || poId}.`,
      '',
      ...(po.lines || []).map((l, i) => `${i + 1}. ${l.sku}  x${l.qty}  @ ${formatMoney(l.rate)}`),
      '',
      `Grand total: ${formatMoney(po.grand_total)}`,
      po.amount_in_words || ''
    ].join('\n');
    // Opened only after the server has already recorded the dispatch, so the
    // audit trail is written whether or not a mail client actually opens.
    window.location.href = `mailto:${encodeURIComponent(data.vendor_email)}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;
  } else {
    await showCustomAlert('Recorded as sent. This vendor has no contact email on their master record, so nothing could be mailed automatically - add one under Setup > Vendors, or configure a notification channel.', 'Sent to Vendor');
  }
  renderView('purchase-orders');
};

async function submitPOForApproval(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'PurchaseOrder', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  // BLD-035: this used to re-render with no confirmation at all - a maker had
  // no way to tell a submit actually queued the PO for a manager's decision.
  showToast(`${getDoctypeLabel('PurchaseOrder')} submitted for approval.`, { variant: 'info' });
  renderView('purchase-orders');
}

// Generic submit-for-approval (Stage 26.8.8/26.8.10) - Appraisal/Grievance
// have no other doctype-specific behavior around this action, so one
// shared function covers both instead of two near-identical copies.
async function submitDocForApproval(doctype, documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype, document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  // BLD-035: see submitPOForApproval's own note just above - same gap, same fix.
  showToast(`${getDoctypeLabel(doctype)} submitted for approval.`, { variant: 'info' });
  renderView('doctype-table');
}

async function submitQualityInspectionForApproval(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'QualityInspection', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  // BLD-035: see submitPOForApproval's own note above - same gap, same fix.
  showToast(`${getDoctypeLabel('QualityInspection')} submitted for approval.`, { variant: 'info' });
  renderView('doctype-table');
}

// Stage 26.9.11: SubcontractOrder row actions (Send/Receive).
async function sendSubcontractOrder(id) {
  const res = await apiFetch('/api/v1/manufacturing/subcontract-order/send', {
    method: 'POST',
    body: JSON.stringify({ id })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to send the subcontract order.');
    return;
  }
  showToast('Raw material sent to subcontractor.', { variant: 'success' });
  renderView('doctype-table');
}

async function receiveSubcontractOrder(id, expectedQty) {
  const qtyStr = await showCustomPrompt('Actual quantity received back from the subcontractor:', expectedQty || '', 'Receive Subcontract Order');
  if (qtyStr === null) return;
  const qty = Number(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/subcontract-order/receive', {
    method: 'POST',
    body: JSON.stringify({ id, qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to receive the subcontract order.');
    return;
  }
  showToast('Processed/finished goods received from subcontractor.', { variant: 'success' });
  renderView('doctype-table');
}

// Stage 26.7.9: Customer merge row action - the clicked row is the
// duplicate; the user supplies which customer id it should merge into.
async function mergeCustomerRow(duplicateId) {
  const primaryId = await showCustomPrompt(`Merge customer "${duplicateId}" into which surviving customer id? All their orders, invoices, vouchers, and loyalty points move to that customer.`, '', 'Merge Customer');
  if (primaryId === null) return;
  if (!primaryId.trim() || primaryId.trim() === duplicateId) {
    await showCustomAlert('Enter a different, valid customer id to merge into.', 'Invalid Customer ID');
    return;
  }
  const confirmed = await showCustomConfirm(`This cannot be undone. Merge "${duplicateId}" into "${primaryId.trim()}"?`, 'Confirm Merge');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/crm/customer/merge', {
    method: 'POST',
    body: JSON.stringify({ primary_customer_id: primaryId.trim(), duplicate_customer_id: duplicateId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to merge customers.');
    return;
  }
  showToast('Customers merged.', { variant: 'success' });
  renderView('doctype-table');
}

// GRN Workbench (Stage 26.3.1). GRN's own registered schema
// (db/migrations_phase3.sql) has always had a mandatory received_items JSON
// field but no screen to fill it in - the only prior path was the generic
// doctype form, and GRN is a Transaction (not Master) doctype so it was
// never even reachable from the Setup submenu that path relies on. This
// posts through the exact same /api/v1/doc/GRN endpoint the (nonexistent)
// generic form would have, so it inherits every existing server-side rule
// for free: the GOODSR-0089/0090 accepted/rejected-qty checks and the
// PURCHA-0082/0084/0086/0087/0088 PO cross-checks (engines/
// transactional_validation.go's validateGRNRules), and the inventory-ledger
// posting hook (internal/server/handlers_core_doc_engine.go). Line shape
// matches validateGRNRules' grnReceivedLine exactly: {sku, qty,
// accepted_qty, rejected_qty, rejection_reason} - qty is the physical
// quantity received (what's checked against the PO's open quantity and
// what actually posts to stock), accepted_qty is auto-derived as qty minus
// rejected_qty rather than asked as a separate input, so it can never
// violate GOODSR-0089's accepted-qty-cannot-exceed-received-qty rule by
// construction. ordered_qty/barcode are carried along for this screen's own
// variance display and label preview - extra keys neither validator nor the
// posting hook look at, so they're harmless to store alongside.
let grnLineItems = [];
// grnLoadedASNId (26.5.1) tracks which ASN, if any, a receipt's lines were
// prefilled from, so createGRN can carry that reference through as GRN's
// new optional asn_id field.
let grnLoadedASNId = '';

async function renderGRNWorkbenchView(container) {
  const res = await apiFetch('/api/v1/doc/GRN');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load goods receipts.', () => renderView('grn')); return; }
  const grns = await res.json();
  state.docData = grns;
  grnLineItems = [];
  grnLoadedASNId = '';

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Goods Receipt</h1>
      <p class="page-subtitle">Receive against a PO: capture accepted, short, and damaged quantities per line, then post to stock.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Goods Receipt</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('GRN Number', 'GRN', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-po">PO Reference</label>
        <input type="text" id="grn-po" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-location">Receiving Location</label>
        <input type="text" id="grn-location" class="form-input" style="width: 130px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="grn-load-po-btn" type="button">Load Items from PO</button>
    </div>
    <div id="grn-po-note" style="margin-top: 8px; font-size: 12.5px; color: var(--text-muted);"></div>

    <!-- Stage 26.5.1: ASN capture ahead of a GRN - an optional prefill
         source alongside the PO one above, same "Load Items from X" pattern. -->
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-asn">ASN Reference (optional)</label>
        <input type="text" id="grn-asn" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="grn-load-asn-btn" type="button">Load Items from ASN</button>
    </div>
    <div id="grn-asn-note" style="margin-top: 8px; font-size: 12.5px; color: var(--text-muted);"></div>

    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-sku">SKU</label>
        <input type="text" id="grn-line-sku" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-ordered">Ordered Qty</label>
        <input type="number" id="grn-line-ordered" class="form-input" style="width: 85px;" min="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-received">Received Qty</label>
        <input type="number" id="grn-line-received" class="form-input" style="width: 90px;" min="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-rejected">Rejected Qty</label>
        <input type="number" id="grn-line-rejected" class="form-input" style="width: 85px;" min="0" value="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-reject-reason">Rejection Reason</label>
        <input type="text" id="grn-line-reject-reason" class="form-input" style="width: 150px;" placeholder="required if rejected > 0">
      </div>
      <!-- Stage 26.5.2: QC sampling's third bucket - damaged is now tracked
           separately from rejected instead of one combined field, each with
           its own required reason, and each actually posts to a different
           inventory_availability bucket server-side (PostGRNReceiptWithQC). -->
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-damaged">Damaged Qty</label>
        <input type="number" id="grn-line-damaged" class="form-input" style="width: 85px;" min="0" value="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-damage-reason">Damage Reason</label>
        <input type="text" id="grn-line-damage-reason" class="form-input" style="width: 150px;" placeholder="required if damaged > 0">
      </div>
      <button class="btn btn-outline" id="grn-add-line-btn" type="button">Add Line</button>
    </div>

    <!-- Stage 42.1.4: batch/lot capture at receipt. Its own row rather than
         three more fields squeezed into the QC row above, because a receiving
         clerk types these off the carton in one go and they belong together.
         The whole row hides itself for an item that is not batch-tracked
         (populateGRNBatchRow below), so a warehouse that has opted no item
         into traceability never sees it. -->
    <div id="grn-batch-row" class="hidden" style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px; padding-top: 12px; border-top: 1px dashed var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-batch">Batch / Lot No</label>
        <input type="text" id="grn-line-batch" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-mfg">Manufacture Date</label>
        <input type="date" id="grn-line-mfg" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-expiry">Expiry Date</label>
        <input type="date" id="grn-line-expiry" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-supplier-batch">Supplier Batch No</label>
        <input type="text" id="grn-line-supplier-batch" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div id="grn-batch-note" style="font-size: 12.5px; color: var(--text-muted); align-self: center;"></div>
    </div>

    <!-- Stage 42.1.8: serial capture at receipt. Its own row, hidden unless
         the SKU in the line field is Serial or Batch and Serial tracked
         (populateGRNBatchRow below also drives this one) - one serial number
         per accepted unit, which is what makes this a textarea rather than a
         single input. -->
    <div id="grn-serial-row" class="hidden" style="display: flex; gap: 12px; align-items: flex-start; flex-wrap: wrap; margin-top: 12px; padding-top: 12px; border-top: 1px dashed var(--border-color);">
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 260px;">
        <label class="form-label" for="grn-line-serials">Serial Numbers (one per unit)</label>
        <textarea id="grn-line-serials" class="form-input" rows="2" style="width: 100%; font-family: Consolas, Monaco, monospace;" placeholder="one per line, or comma-separated"></textarea>
      </div>
      <div id="grn-serial-note" style="font-size: 12.5px; color: var(--text-muted); align-self: center;"></div>
    </div>
    <!-- Stage 42.3.7: catch weight + dimensional capture at receipt. Weight
         is shown (and required) only for an item flagged is_catch_weight;
         dimensions are always optional and offered for every line, same
         "no gate, just an optional capture" shape as the batch fields for a
         non-tracked item above. -->
    <div id="grn-weight-row" style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px; padding-top: 12px; border-top: 1px dashed var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-weight">Actual Weight</label>
        <input type="number" id="grn-line-weight" class="form-input" style="width: 110px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-weight-uom">Weight UOM</label>
        <input type="text" id="grn-line-weight-uom" class="form-input" style="width: 90px;" placeholder="kg" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-length">L</label>
        <input type="number" id="grn-line-length" class="form-input" style="width: 70px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-width">W</label>
        <input type="number" id="grn-line-width" class="form-input" style="width: 70px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-height">H</label>
        <input type="number" id="grn-line-height" class="form-input" style="width: 70px;" min="0" step="0.01">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-dim-uom">Dim UOM</label>
        <input type="text" id="grn-line-dim-uom" class="form-input" style="width: 80px;" placeholder="cm" autocomplete="off">
      </div>
      <div id="grn-weight-note" style="font-size: 12.5px; color: var(--text-muted); align-self: center;"></div>
    </div>
    <div id="grn-lines-list" style="margin: 12px 0;"></div>
    <div id="grn-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="grn-create-btn">Post Receipt</button>
  `;
  container.appendChild(formPanel);

  attachLinkTypeahead(document.getElementById('grn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('grn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('grn-asn'), 'ASN');
  attachLinkTypeahead(document.getElementById('grn-line-sku'), 'Item');

  // Stage 42.1.4: the batch row follows whatever SKU is in the line field, so
  // the clerk is asked for a lot number exactly when the item needs one and
  // never otherwise. `change` (not `input`) so the typeahead's resolved value
  // is what gets looked up, matching how grn-po already drives its own reload.
  document.getElementById('grn-line-sku').addEventListener('change', populateGRNBatchRow);

  document.getElementById('grn-po').addEventListener('change', loadGRNItemsFromPO);
  document.getElementById('grn-load-po-btn').addEventListener('click', loadGRNItemsFromPO);
  document.getElementById('grn-load-asn-btn').addEventListener('click', loadGRNItemsFromASN);
  document.getElementById('grn-add-line-btn').addEventListener('click', addGRNLine);
  document.getElementById('grn-create-btn').addEventListener('click', createGRN);

  // Stage 42.1.4: drop the per-SKU tracking cache each time the screen is
  // opened, so an item switched to batch-tracked in Setup is picked up on the
  // clerk's next visit rather than surviving until a page reload.
  grnBatchTracked = {};
  grnBatchShelfLife = {};
  grnSerialTracked = {};
  grnCatchWeight = {};

  renderGRNLinesList();

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr><th>GRN #</th><th>PO</th><th>Location</th><th>Lines</th><th>Received / Rejected / Damaged</th><th>Status</th></tr>
      </thead>
      <tbody>
  `;
  if (grns.length === 0) {
    html += `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No goods receipts yet. Use <b>Load Items from PO</b> above to receive against an approved Purchase Order, then <b>Post Receipt</b>.</td></tr>`;
  }
  grns.forEach(g => {
    let lines = [];
    try { lines = JSON.parse(g.received_items || '[]'); } catch (e) { lines = []; }
    const receivedTotal = lines.reduce((s, l) => s + (Number(l.qty) || 0), 0);
    const rejectedTotal = lines.reduce((s, l) => s + (Number(l.rejected_qty) || 0), 0);
    const damagedTotal = lines.reduce((s, l) => s + (Number(l.damaged_qty) || 0), 0);
    const statusBadge = g.status === 'Approved' ? 'badge-success' : g.status === 'Cancelled' ? 'badge-danger' : 'badge-warning';
    html += `
      <tr>
        <td style="font-family: monospace;">${g.code || g.id}</td>
        <td>${g.po_id || ''}</td>
        <td>${g.location || ''}</td>
        <td>${lines.length}</td>
        <td>${receivedTotal} / ${rejectedTotal} / ${damagedTotal}</td>
        <td><span class="badge ${statusBadge}">${g.status}</span></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);
}

function renderGRNLinesList() {
  const el = document.getElementById('grn-lines-list');
  if (!el) return;
  if (grnLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  // Stage 42.1.4/42.1.8: the Batch/Expiry and Serials columns each appear
  // only once a line on this receipt actually carries one.
  const grnHasBatches = grnLineItems.some(l => l.batch_no);
  const grnHasSerials = grnLineItems.some(l => l.serial_numbers && l.serial_numbers.length);
  el.innerHTML = `
    <table style="margin-top: 4px;">
      <thead><tr><th>SKU</th><th>Barcode</th>${grnHasBatches ? '<th>Batch / Expiry</th>' : ''}${grnHasSerials ? '<th>Serials</th>' : ''}<th>Ordered</th><th>Received</th><th>Accepted</th><th>Rejected</th><th>Damaged</th><th>Short</th><th></th></tr></thead>
      <tbody>
        ${grnLineItems.map((line, idx) => {
          const ordered = line.ordered_qty;
          const short = (ordered !== null && ordered !== undefined && ordered !== '') ? Math.max(0, ordered - line.qty) : null;
          return `
            <tr>
              <td style="font-family: monospace;">${line.sku}</td>
              <td><span class="badge badge-secondary" style="font-family: Consolas, Monaco, monospace; letter-spacing: 1px;">${line.barcode || line.sku}</span></td>
              ${grnHasBatches ? `<td>${batchCellHTML({ batch_no: line.batch_no, expiry_date: line.expiry_date })}</td>` : ''}
              ${grnHasSerials ? `<td>${line.serial_numbers && line.serial_numbers.length ? `<span class="badge badge-secondary" title="${line.serial_numbers.map(escapeHTMLText).join(', ')}">${line.serial_numbers.length} unit(s)</span>` : ''}</td>` : ''}
              <td>${(ordered === null || ordered === undefined || ordered === '') ? '&mdash;' : ordered}</td>
              <td>${line.qty}</td>
              <td>${line.accepted_qty}</td>
              <td>${line.rejected_qty > 0 ? `<span class="badge badge-danger">${line.rejected_qty}</span>` : '0'}</td>
              <td>${line.damaged_qty > 0 ? `<span class="badge badge-danger">${line.damaged_qty}</span>` : '0'}</td>
              <td>${short === null ? '&mdash;' : short > 0 ? `<span class="badge badge-warning">${short}</span>` : '0'}</td>
              <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeGRNLine', [idx])}>Remove</button></td>
            </tr>
          `;
        }).join('')}
      </tbody>
    </table>
  `;
}

// Best-effort barcode lookup for the line-list preview badge - falls back
// to the SKU itself on any miss, same degrade-gracefully behavior
// engines.PrintStickers already uses server-side for an unregistered SKU.
async function lookupGRNBarcode(sku) {
  const itemRes = await apiFetch(`/api/v1/doc/Item/${encodeURIComponent(sku)}`);
  if (itemRes && itemRes.ok) {
    const item = await itemRes.json();
    return item.barcode || sku;
  }
  return sku;
}

async function addGRNLine() {
  const skuEl = document.getElementById('grn-line-sku');
  const orderedEl = document.getElementById('grn-line-ordered');
  const receivedEl = document.getElementById('grn-line-received');
  const rejectedEl = document.getElementById('grn-line-rejected');
  const reasonEl = document.getElementById('grn-line-reject-reason');
  const damagedEl = document.getElementById('grn-line-damaged');
  const damageReasonEl = document.getElementById('grn-line-damage-reason');
  const errorEl = document.getElementById('grn-form-error');
  errorEl.classList.add('hidden');

  const sku = skuEl.value.trim();
  const ordered = orderedEl.value === '' ? null : parseInt(orderedEl.value, 10);
  const qty = parseInt(receivedEl.value, 10);
  const rejectedQty = parseInt(rejectedEl.value, 10) || 0;
  const rejectionReason = reasonEl.value.trim();
  const damagedQty = parseInt(damagedEl.value, 10) || 0;
  const damageReason = damageReasonEl.value.trim();

  if (!sku || isNaN(qty) || qty < 0) return;
  if (rejectedQty + damagedQty > qty) {
    errorEl.textContent = 'Rejected plus Damaged qty cannot exceed Received qty.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (rejectedQty > 0 && !rejectionReason) {
    errorEl.textContent = 'A rejection reason is required when Rejected qty is greater than 0.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (damagedQty > 0 && !damageReason) {
    errorEl.textContent = 'A damage reason is required when Damaged qty is greater than 0.';
    errorEl.classList.remove('hidden');
    return;
  }

  // Stage 42.1.4: batch capture. The mandatory check is repeated server-side
  // in PostGRNReceiptWithQC (INVENT-0115) - this copy exists only so the clerk
  // finds out while the carton is still in their hands, not after posting.
  const batchEl = document.getElementById('grn-line-batch');
  const mfgEl = document.getElementById('grn-line-mfg');
  const expiryEl = document.getElementById('grn-line-expiry');
  const supplierBatchEl = document.getElementById('grn-line-supplier-batch');
  const batchNo = (batchEl?.value || '').trim();
  const mfgDate = (mfgEl?.value || '').trim();
  const expiryDate = (expiryEl?.value || '').trim();
  const supplierBatch = (supplierBatchEl?.value || '').trim();

  if (grnBatchTracked[sku] && !batchNo) {
    errorEl.textContent = `${sku} is batch-tracked - enter the batch/lot number printed on the carton before adding this line.`;
    errorEl.classList.remove('hidden');
    return;
  }
  if (mfgDate && expiryDate && expiryDate <= mfgDate) {
    errorEl.textContent = 'Expiry date must be after the manufacture date.';
    errorEl.classList.remove('hidden');
    return;
  }

  // Stage 42.1.8: serial capture. Same "server re-checks, this copy is so the
  // clerk finds out with the carton still in hand" reasoning as batch above -
  // the server-side gate is ValidateReceiptSerialLine (INVENT-0115).
  const serialsEl = document.getElementById('grn-line-serials');
  const acceptedQty = qty - rejectedQty - damagedQty;
  const serialNumbers = (serialsEl?.value || '')
    .split(/[\n,]/).map(s => s.trim()).filter(Boolean);
  if (grnSerialTracked[sku] && acceptedQty > 0) {
    const unique = new Set(serialNumbers);
    if (unique.size !== serialNumbers.length) {
      errorEl.textContent = `${sku} has a serial number listed more than once - each unit needs its own serial number.`;
      errorEl.classList.remove('hidden');
      return;
    }
    if (serialNumbers.length !== acceptedQty) {
      errorEl.textContent = `${sku} is serial-tracked - ${acceptedQty} unit(s) accepted but ${serialNumbers.length} serial number(s) entered. List exactly one per accepted unit.`;
      errorEl.classList.remove('hidden');
      return;
    }
  }

  // Stage 42.3.7: catch weight + dimensions. The mandatory check is repeated
  // server-side in ValidateReceiptCatchWeightLine (GOODSR-0098) - same
  // "clerk finds out with the carton still in hand" reasoning as batch/
  // serial above.
  const weightEl = document.getElementById('grn-line-weight');
  const weightUomEl = document.getElementById('grn-line-weight-uom');
  const lengthEl = document.getElementById('grn-line-length');
  const widthEl = document.getElementById('grn-line-width');
  const heightEl = document.getElementById('grn-line-height');
  const dimUomEl = document.getElementById('grn-line-dim-uom');
  const actualWeight = (weightEl?.value || '') === '' ? null : parseFloat(weightEl.value);
  const weightUom = (weightUomEl?.value || '').trim();
  const length = (lengthEl?.value || '') === '' ? null : parseFloat(lengthEl.value);
  const width = (widthEl?.value || '') === '' ? null : parseFloat(widthEl.value);
  const height = (heightEl?.value || '') === '' ? null : parseFloat(heightEl.value);
  const dimUom = (dimUomEl?.value || '').trim();

  if (grnCatchWeight[sku] && (actualWeight === null || actualWeight <= 0)) {
    errorEl.textContent = `${sku} is a catch weight item - enter the actual weight before adding this line.`;
    errorEl.classList.remove('hidden');
    return;
  }

  const barcode = await lookupGRNBarcode(sku);
  const line = {
    sku, ordered_qty: ordered, qty,
    accepted_qty: acceptedQty,
    rejected_qty: rejectedQty,
    rejection_reason: rejectionReason,
    damaged_qty: damagedQty,
    damage_reason: damageReason,
    barcode
  };
  // Only set when present, so a non-traceability receipt posts the exact same
  // received_items JSON it always has.
  if (batchNo) line.batch_no = batchNo;
  if (mfgDate) line.mfg_date = mfgDate;
  if (expiryDate) line.expiry_date = expiryDate;
  if (supplierBatch) line.supplier_batch = supplierBatch;
  if (serialNumbers.length) line.serial_numbers = serialNumbers;
  if (actualWeight !== null) line.actual_weight = actualWeight;
  if (weightUom) line.weight_uom = weightUom;
  if (length !== null) line.length = length;
  if (width !== null) line.width = width;
  if (height !== null) line.height = height;
  if (dimUom) line.dim_uom = dimUom;
  grnLineItems.push(line);

  skuEl.value = '';
  orderedEl.value = '';
  receivedEl.value = '';
  rejectedEl.value = '0';
  reasonEl.value = '';
  damagedEl.value = '0';
  damageReasonEl.value = '';
  if (batchEl) batchEl.value = '';
  if (mfgEl) mfgEl.value = '';
  if (expiryEl) expiryEl.value = '';
  if (supplierBatchEl) supplierBatchEl.value = '';
  if (serialsEl) serialsEl.value = '';
  if (weightEl) weightEl.value = '';
  if (weightUomEl) weightUomEl.value = '';
  if (lengthEl) lengthEl.value = '';
  if (widthEl) widthEl.value = '';
  if (heightEl) heightEl.value = '';
  if (dimUomEl) dimUomEl.value = '';
  populateGRNBatchRow();
  renderGRNLinesList();
}

// Stage 42.1.4: which SKUs are batch-tracked, cached per screen visit so
// re-typing the same SKU does not re-fetch the Item. Keyed by SKU, value is
// the boolean - an absent key means "not looked up yet".
let grnBatchTracked = {};

// populateGRNBatchRow shows or hides the batch/lot row for whatever SKU is
// currently in the line field, and says WHY it is showing - a receiving clerk
// should not have to know which items someone configured as batch-tracked.
async function populateGRNBatchRow() {
  const row = document.getElementById('grn-batch-row');
  const note = document.getElementById('grn-batch-note');
  const batchEl = document.getElementById('grn-line-batch');
  const serialRow = document.getElementById('grn-serial-row');
  const serialNote = document.getElementById('grn-serial-note');
  if (!row) return;
  const sku = (document.getElementById('grn-line-sku')?.value || '').trim();
  if (!sku) {
    row.classList.add('hidden');
    if (serialRow) serialRow.classList.add('hidden');
    return;
  }

  if (!(sku in grnBatchTracked)) {
    // The Item doc endpoint keys on document id, and an Item's id is not
    // reliably its code in this tree, so a miss here is "unknown", never
    // "not tracked" - the server-side gate still catches a genuinely
    // batch-tracked item whose lookup failed.
    const res = await apiFetch(`/api/v1/doc/Item/${encodeURIComponent(sku)}`);
    let mode = '';
    let shelfLife = 0;
    let catchWeight = false;
    if (res && res.ok) {
      const item = await res.json();
      mode = item.tracking_mode || '';
      shelfLife = Number(item.shelf_life_days) || 0;
      catchWeight = item.is_catch_weight === 'Yes';
    }
    grnBatchTracked[sku] = (mode === 'Batch' || mode === 'Batch and Serial');
    grnBatchShelfLife[sku] = shelfLife;
    grnSerialTracked[sku] = (mode === 'Serial' || mode === 'Batch and Serial');
    grnCatchWeight[sku] = catchWeight;
  }

  const tracked = grnBatchTracked[sku];
  row.classList.remove('hidden');
  if (batchEl) batchEl.placeholder = tracked ? 'required for this item' : 'optional';
  if (note) {
    const shelfLife = grnBatchShelfLife[sku] || 0;
    note.innerHTML = tracked
      ? `<b>${escapeHTMLText(sku)}</b> is batch-tracked &mdash; a lot number is required.${shelfLife > 0 ? ` If you enter a manufacture date and leave Expiry blank, expiry is derived as manufacture + ${shelfLife} days.` : ''}`
      : `<b>${escapeHTMLText(sku)}</b> is not batch-tracked. You may still record a lot number; it will be kept for traceability.`;
  }

  // Stage 42.1.8: the serial row only shows for a Serial / Batch and Serial
  // item - unlike the batch row above, which stays visible (but optional)
  // for every item, since recording a stray serial on an untracked item has
  // no use the way a stray batch number does.
  const serialTracked = grnSerialTracked[sku];
  if (serialRow) serialRow.classList.toggle('hidden', !serialTracked);
  if (serialNote && serialTracked) {
    serialNote.innerHTML = `<b>${escapeHTMLText(sku)}</b> is serial-tracked &mdash; list exactly one serial number per accepted unit.`;
  }

  // Stage 42.3.7: catch weight. The row itself always shows (dimensions are
  // a free capture for any item), but only a catch-weight item's note/
  // placeholder says weight is required - addGRNLine is what actually
  // enforces it.
  const catchWeight = grnCatchWeight[sku];
  const weightEl = document.getElementById('grn-line-weight');
  const weightNote = document.getElementById('grn-weight-note');
  if (weightEl) weightEl.placeholder = catchWeight ? 'required for this item' : 'optional';
  if (weightNote) {
    weightNote.innerHTML = catchWeight
      ? `<b>${escapeHTMLText(sku)}</b> is a catch weight item &mdash; the actual weight is required.`
      : '';
  }
}

// Shelf life per SKU, cached alongside grnBatchTracked, so the note above can
// explain the derive-expiry-from-manufacture-date behaviour concretely rather
// than in the abstract.
let grnBatchShelfLife = {};

// Stage 42.1.8: which SKUs are serial-tracked, same per-screen-visit cache
// shape as grnBatchTracked.
let grnSerialTracked = {};

// Stage 42.3.7: which SKUs are catch-weight items, same per-screen-visit
// cache shape as grnBatchTracked.
let grnCatchWeight = {};

window.removeGRNLine = function(idx) {
  grnLineItems.splice(idx, 1);
  renderGRNLinesList();
};

// loadGRNItemsFromPO pre-fills lines from the PO's own "items" JSON (Received
// Qty defaulting to the ordered qty - the common case where everything
// ordered showed up intact; the maker adjusts Received/Rejected per line for
// any actual variance before posting). A PO with no item lines at all is not
// an error here, it just falls through to manual line entry below - but that
// is now the exception rather than the rule: Stage 40.1 rebuilt the PO
// composer to save real lines (poDraftPayload sends items: JSON.stringify
// (lines), and savePurchaseOrder refuses a PO with none), so only POs raised
// before 40.1, or created through the API without lines, arrive empty. The
// note this comment used to carry - "the PO create screen only ever saves
// items: '[]'" - described the pre-40.1 screen and was left stale; it cost a
// later session a wrong first hypothesis, hence this correction.
// Reentrancy-guarded: this fires from both the PO field's own 'change'
// event (typeahead pick, or tabbing off a typed value) and the explicit
// "Load Items from PO" button, so a user picking a PO and immediately
// clicking the button (or a script driving both in the same tick) can
// launch two overlapping calls - each does `grnLineItems = []` then awaits
// a barcode lookup per line, so an interleaved second call's reset/pushes
// land on the same shared array the first call resumes into, duplicating
// every line. Caught live while verifying this screen (Playwright triggered
// exactly this sequence), not a theoretical case.
let grnPOLoadInFlight = false;
async function loadGRNItemsFromPO() {
  if (grnPOLoadInFlight) return;
  grnPOLoadInFlight = true;
  try {
    await loadGRNItemsFromPOInner();
  } finally {
    grnPOLoadInFlight = false;
  }
}

async function loadGRNItemsFromPOInner() {
  const poId = document.getElementById('grn-po').value.trim();
  const noteEl = document.getElementById('grn-po-note');
  if (!poId) { noteEl.textContent = ''; return; }

  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`);
  if (!res) return;
  if (!res.ok) {
    noteEl.textContent = 'Could not find that PO - enter lines manually below.';
    return;
  }
  const po = await res.json();

  const locationEl = document.getElementById('grn-location');
  if (!locationEl.value && (po.location || po.target_warehouse)) {
    locationEl.value = po.location || po.target_warehouse;
  }

  let items = [];
  try { items = JSON.parse(po.items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `PO ${poId} has no recorded item lines - add lines manually below.`;
    return;
  }

  grnLineItems = [];
  for (const it of items) {
    const sku = it.sku || it.item_id || '';
    const orderedQty = Number(it.qty) || 0;
    if (!sku) continue;
    const barcode = await lookupGRNBarcode(sku);
    grnLineItems.push({ sku, ordered_qty: orderedQty, qty: orderedQty, accepted_qty: orderedQty, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode });
  }
  grnLoadedASNId = '';
  noteEl.textContent = `Loaded ${grnLineItems.length} line(s) from PO ${poId}. Adjust Received/Rejected/Damaged qty for any variance, then Post Receipt.`;
  renderGRNLinesList();
}

// loadGRNItemsFromASN (26.5.1) mirrors loadGRNItemsFromPOInner exactly, off
// ASN's expected_items instead of a PO's items - the ASN's own po_id is
// used to fill the PO Reference field too if it isn't already set, so the
// GRN still cross-checks against the right PO (validateASNRules already
// confirmed at ASN-creation time that these SKUs belong to that PO).
async function loadGRNItemsFromASN() {
  const asnId = document.getElementById('grn-asn').value.trim();
  const noteEl = document.getElementById('grn-asn-note');
  if (!asnId) { noteEl.textContent = ''; return; }

  const res = await apiFetch(`/api/v1/doc/ASN/${encodeURIComponent(asnId)}`);
  if (!res) return;
  if (!res.ok) {
    noteEl.textContent = 'Could not find that ASN - enter lines manually below.';
    return;
  }
  const asn = await res.json();

  const poEl = document.getElementById('grn-po');
  if (!poEl.value && asn.po_id) poEl.value = asn.po_id;

  let items = [];
  try { items = JSON.parse(asn.expected_items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `ASN ${asnId} has no recorded expected items - add lines manually below.`;
    return;
  }

  grnLineItems = [];
  for (const it of items) {
    const sku = it.sku || '';
    const expectedQty = Number(it.qty) || 0;
    if (!sku) continue;
    const barcode = await lookupGRNBarcode(sku);
    grnLineItems.push({ sku, ordered_qty: expectedQty, qty: expectedQty, accepted_qty: expectedQty, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode });
  }
  grnLoadedASNId = asnId;
  noteEl.textContent = `Loaded ${grnLineItems.length} line(s) from ASN ${asnId}. Adjust Received/Rejected/Damaged qty for any variance, then Post Receipt.`;
  renderGRNLinesList();
}

// BLD-036: same double-submit guard as the generic record form and PO
// composer - a slow post (or a repeat click on "Post Receipt" before the
// first response returns) used to be able to post two GRNs from one intent.
async function createGRN() {
  await guardAgainstDoubleSubmit(document.getElementById('grn-create-btn'), 'Posting...', createGRNInner);
}

async function createGRNInner() {
  const errorEl = document.getElementById('grn-form-error');
  errorEl.classList.add('hidden');

  const poId = document.getElementById('grn-po').value.trim();
  const location = document.getElementById('grn-location').value.trim();

  if (!poId || !location || grnLineItems.length === 0) {
    errorEl.textContent = 'PO Reference, Receiving Location, and at least one line item are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // The GRN number does not exist yet at confirm time - it is issued by the
  // server on save (Stage 30.6) - so this asks about the receipt's effect,
  // which is what actually needs confirming, rather than naming a number the
  // maker typed.
  const totalQty = grnLineItems.reduce((s, l) => s + l.qty, 0);
  if (!(await showCustomConfirm(`Post this goods receipt against ${poId}? ${totalQty} unit(s) will be added to stock at ${location}.`, 'Post Goods Receipt'))) return;

  const res = await apiFetch('/api/v1/doc/GRN', {
    method: 'POST',
    body: JSON.stringify({
      po_id: poId,
      asn_id: grnLoadedASNId || undefined,
      location,
      received_items: JSON.stringify(grnLineItems),
      status: 'Approved'
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to post goods receipt.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('grn');
}

// Stage 26.5.1: ASN (Advance Shipment Notice) capture - a lightweight
// counterpart to the GRN Workbench above (same line-list pattern, just
// sku/expected_qty instead of a full accept/reject/damage split, since an
// ASN is what the vendor SAID is coming, not what actually arrived).
let asnLineItems = [];

async function renderASNView(container) {
  const res = await apiFetch('/api/v1/doc/ASN');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load ASNs.', () => renderView('asn')); return; }
  const asns = await res.json();
  asnLineItems = [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Advance Shipment Notices (ASN)</h1>
      <p class="page-subtitle">Capture what a vendor says is coming, ahead of the actual GRN - the GRN Workbench can prefill its lines from an ASN.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  // ASN's asn_number/status/location fields (and the Expected/Received/
  // Cancelled status vocabulary) are the doctype's original, pre-Stage-26.5
  // fields (db/migration.sql) - reused here rather than duplicated, per
  // this repo's "extend the existing doctype, don't build a parallel one"
  // rule. po_id/vendor/carrier/tracking_number/expected_date/expected_items
  // are this Stage's additive fields.
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New ASN</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('ASN Number', 'ASN', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-po">PO Reference</label>
        <input type="text" id="asn-po" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-location">Location</label>
        <input type="text" id="asn-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-vendor">Vendor</label>
        <input type="text" id="asn-vendor" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-carrier">Carrier</label>
        <input type="text" id="asn-carrier" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-tracking">Tracking Number</label>
        <input type="text" id="asn-tracking" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-expected-date">Expected Date</label>
        <input type="date" id="asn-expected-date" class="form-input" style="width: 150px;">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-line-sku">SKU</label>
        <input type="text" id="asn-line-sku" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-line-qty">Expected Qty</label>
        <input type="number" id="asn-line-qty" class="form-input" style="width: 100px;" min="1">
      </div>
      <button class="btn btn-outline" id="asn-add-line-btn" type="button">Add Line</button>
    </div>
    <div id="asn-lines-list" style="margin: 12px 0;"></div>
    <div id="asn-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="asn-create-btn">Save ASN</button>
  `;
  container.appendChild(formPanel);

  attachLinkTypeahead(document.getElementById('asn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('asn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('asn-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('asn-line-sku'), 'Item');
  document.getElementById('asn-add-line-btn').addEventListener('click', addASNLine);
  document.getElementById('asn-create-btn').addEventListener('click', createASN);
  renderASNLinesList();

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>ASN #</th><th>PO</th><th>Location</th><th>Vendor</th><th>Carrier</th><th>Expected Date</th><th>Lines</th><th>Status</th></tr></thead>
      <tbody>
  `;
  if (asns.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No ASNs yet. Use <b>Add Line</b> then <b>Save ASN</b> above to record a shipment your vendor has despatched.</td></tr>`;
  }
  asns.forEach(a => {
    let lines = [];
    try { lines = JSON.parse(a.expected_items || '[]'); } catch (e) { lines = []; }
    html += `
      <tr>
        <td style="font-family: monospace;">${a.asn_number || a.id}</td>
        <td>${a.po_id || a.po_number || ''}</td>
        <td>${a.location || ''}</td>
        <td>${a.vendor || ''}</td>
        <td>${a.carrier || ''}</td>
        <td>${a.expected_date || ''}</td>
        <td>${lines.length}</td>
        <td><span class="badge badge-secondary">${a.status}</span></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);
}

function renderASNLinesList() {
  const el = document.getElementById('asn-lines-list');
  if (!el) return;
  if (asnLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = `
    <table style="margin-top: 4px;">
      <thead><tr><th>SKU</th><th>Expected Qty</th><th></th></tr></thead>
      <tbody>
        ${asnLineItems.map((line, idx) => `
          <tr>
            <td style="font-family: monospace;">${line.sku}</td>
            <td>${line.qty}</td>
            <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeASNLine', [idx])}>Remove</button></td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

function addASNLine() {
  const skuEl = document.getElementById('asn-line-sku');
  const qtyEl = document.getElementById('asn-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku || isNaN(qty) || qty <= 0) return;
  asnLineItems.push({ sku, qty });
  skuEl.value = '';
  qtyEl.value = '';
  renderASNLinesList();
}

window.removeASNLine = function(idx) {
  asnLineItems.splice(idx, 1);
  renderASNLinesList();
};

async function createASN() {
  // BLD-036: guard against a double-click creating two ASNs.
  await guardAgainstDoubleSubmit(document.getElementById('asn-create-btn'), 'Saving...', createASNInner);
}

async function createASNInner() {
  const errorEl = document.getElementById('asn-form-error');
  errorEl.classList.add('hidden');

  const poId = document.getElementById('asn-po').value.trim();
  const location = document.getElementById('asn-location').value.trim();
  if (!poId || !location || asnLineItems.length === 0) {
    errorEl.textContent = 'PO Reference, Location, and at least one line item are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // asn_number is issued server-side from the ASN series (Stage 30.6).
  // po_number stays the referenced PO's number - despite the name it is this
  // ASN's link to its purchase order, not its own identifier.
  const res = await apiFetch('/api/v1/doc/ASN', {
    method: 'POST',
    body: JSON.stringify({
      po_number: poId,
      po_id: poId,
      location,
      vendor: document.getElementById('asn-vendor').value.trim(),
      carrier: document.getElementById('asn-carrier').value.trim(),
      tracking_number: document.getElementById('asn-tracking').value.trim(),
      expected_date: document.getElementById('asn-expected-date').value,
      expected_items: JSON.stringify(asnLineItems),
      status: 'Expected'
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to save ASN.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('asn');
}

// Purchase Return (Stage 57.15): return goods to the vendor against the GRN
// they arrived on. The return is an ordinary PurchaseReturn document (saved
// through the generic doc API, which checks it against the GRN); this screen
// adds what that form cannot: the GRN's returnable lines, and Post, which
// moves the stock out and raises the vendor's debit note.
let prtContext = null;

async function renderPurchaseReturnsView(container) {
  const [listRes, ctxRes, grnRes] = await Promise.all([
    apiFetch('/api/v1/doc/PurchaseReturn'),
    apiFetch('/api/v1/procurement/purchase-returns/context'),
    apiFetch('/api/v1/doc/GRN')
  ]);
  if (!listRes || !ctxRes || !grnRes) return;
  if (!listRes.ok || !ctxRes.ok) {
    renderErrorPanel(container, 'Failed to load purchase returns.', () => renderView('purchase-returns'));
    return;
  }
  const returns = await listRes.json();
  const approvalRequired = !!(await ctxRes.json()).approval_required;
  const grns = grnRes.ok ? (await grnRes.json()).filter(g => g.status !== 'Cancelled') : [];
  prtContext = null;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Purchase Return</h1>
      <p class="page-subtitle">Send goods back to the vendor against the GRN they arrived on. Posting takes the stock out and raises the vendor's debit note.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Purchase Return</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="prt-grn">Goods Receipt (GRN)<span class="required">*</span></label>
        <select id="prt-grn" class="form-input" style="min-width: 260px;">
          <option value="">Choose the GRN the goods came in on</option>
          ${grns.map(g => `<option value="${escapeHTMLText(g.id)}">${escapeHTMLText(g.id)}${g.vendor ? ' - ' + escapeHTMLText(g.vendor) : ''}${g.po_id ? ' (PO ' + escapeHTMLText(g.po_id) + ')' : ''}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 240px;">
        <label class="form-label" for="prt-reason">Reason for Return<span class="required">*</span></label>
        <input type="text" id="prt-reason" class="form-input" placeholder="e.g. wrong size supplied, QC rejected" autocomplete="off">
      </div>
    </div>
    <div id="prt-lines" style="margin-bottom: 16px; font-size: 13px; color: var(--text-muted);">Choose a GRN to see what can be returned.</div>
    <div id="prt-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div style="display: flex; align-items: center; gap: 12px;">
      <button class="btn btn-primary" id="prt-save-btn" disabled>Save Return</button>
      <span style="font-size: 13px; color: var(--text-muted);">${approvalRequired
        ? 'Returns need approval before they can be posted.'
        : 'Saved as a Draft; nothing moves until you Post it.'}</span>
    </div>
  `;
  container.appendChild(formPanel);

  const badge = s => s === 'Posted' ? 'badge-success' : (s === 'Cancelled' || s === 'Rejected') ? 'badge-danger' : 'badge-secondary';
  const actions = r => {
    const out = [];
    if ((r.status === 'Draft' && !approvalRequired) || r.status === 'Approved') {
      out.push(`<button class="action-btn" ${actionAttrs('postPurchaseReturn', [r.id])}>Post</button>`);
    }
    if (r.status === 'Draft' && approvalRequired) {
      out.push(`<button class="action-btn" ${actionAttrs('submitPurchaseReturnForApproval', [r.id])}>Submit for approval</button>`);
    }
    if (r.status === 'Draft' || r.status === 'Rejected') {
      out.push(`<button class="action-btn" ${actionAttrs('cancelPurchaseReturn', [r.id])}>Cancel</button>`);
    }
    return out.join(' ');
  };
  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `
    <table>
      <thead><tr><th>Return #</th><th>GRN</th><th>Vendor</th><th>Reason</th><th>Value (ex-GST)</th><th>Debit Note</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${returns.length === 0
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No purchase returns yet. Choose a GRN above to raise one.</td></tr>`
          : returns.map(r => `
            <tr>
              <td style="font-family: monospace;">${escapeHTMLText(r.code || r.id)}</td>
              <td style="font-family: monospace;">${escapeHTMLText(r.grn_id || '')}</td>
              <td>${escapeHTMLText(r.vendor_id || '')}</td>
              <td>${escapeHTMLText(r.reason || '')}</td>
              <td>${formatMoney(r.total_amount || 0)}</td>
              <td style="font-family: monospace;">${escapeHTMLText(r.debit_note_id || '')}</td>
              <td><span class="badge ${badge(r.status)}">${escapeHTMLText(r.status || '')}</span></td>
              <td>${actions(r)}</td>
            </tr>`).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(listPanel);

  document.getElementById('prt-grn').addEventListener('change', e => loadPurchaseReturnLines(e.target.value));
  document.getElementById('prt-save-btn').addEventListener('click', savePurchaseReturn);
}

async function loadPurchaseReturnLines(grnID) {
  const linesEl = document.getElementById('prt-lines');
  const saveBtn = document.getElementById('prt-save-btn');
  document.getElementById('prt-form-error').classList.add('hidden');
  prtContext = null;
  saveBtn.disabled = true;
  if (!grnID) {
    linesEl.textContent = 'Choose a GRN to see what can be returned.';
    return;
  }
  linesEl.textContent = 'Loading...';
  const res = await apiFetch(`/api/v1/procurement/purchase-returns/context?grn_id=${encodeURIComponent(grnID)}`);
  if (!res) return;
  if (!res.ok) {
    linesEl.textContent = await getErrorMessage(res, 'Could not load that GRN.');
    return;
  }
  prtContext = (await res.json()).returnable;
  const lines = prtContext.lines || [];
  if (!lines.some(l => l.returnable > 0)) {
    linesEl.textContent = `Nothing left to return on ${grnID} - everything it received is already on a purchase return.`;
    return;
  }
  linesEl.innerHTML = `
    <div style="margin-bottom: 8px;">Vendor <b>${escapeHTMLText(prtContext.vendor_id || '-')}</b>, returning from <b>${escapeHTMLText(prtContext.location || '-')}</b>. Enter the quantity going back on each line.</div>
    <table>
      <thead><tr><th>SKU</th><th>Lot</th><th>Stock</th><th>Received</th><th>Returned</th><th>On open returns</th><th>Can return</th><th>Unit cost (ex-GST)</th><th>Return qty</th></tr></thead>
      <tbody>
        ${lines.map((l, i) => `
          <tr>
            <td style="font-family: monospace;">${escapeHTMLText(l.sku)}</td>
            <td style="font-family: monospace;">${escapeHTMLText(l.batch_no || '')}</td>
            <td>${escapeHTMLText(l.stock_bucket)}</td>
            <td>${l.received}</td>
            <td>${l.returned}</td>
            <td>${l.on_open_returns}</td>
            <td><b>${l.returnable}</b></td>
            <td>${l.stock_bucket === 'Accepted' ? formatMoney(l.unit_cost) : '<span title="Set aside at receipt and never costed, so it carries no debit note value">-</span>'}</td>
            <td><input type="number" class="form-input prt-qty" data-line="${i}" min="0" max="${l.returnable}" step="1" value="0" style="width: 90px;" ${l.returnable > 0 ? '' : 'disabled'} aria-label="Return quantity for ${escapeHTMLText(l.sku)}"></td>
          </tr>`).join('')}
      </tbody>
    </table>
  `;
  saveBtn.disabled = false;
}

async function savePurchaseReturn() {
  const errorEl = document.getElementById('prt-form-error');
  errorEl.classList.add('hidden');
  const showError = msg => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  if (!prtContext) { showError('Choose a GRN first.'); return; }
  const reason = document.getElementById('prt-reason').value.trim();
  if (!reason) { showError('Enter the reason the goods are going back.'); return; }
  const items = [];
  document.querySelectorAll('.prt-qty').forEach(input => {
    const qty = Number(input.value);
    if (qty > 0) {
      const l = prtContext.lines[Number(input.dataset.line)];
      items.push({ sku: l.sku, batch_no: l.batch_no || '', stock_bucket: l.stock_bucket, qty });
    }
  });
  if (items.length === 0) { showError('Enter a return quantity on at least one line.'); return; }

  const res = await apiFetch('/api/v1/doc/PurchaseReturn', {
    method: 'POST',
    body: JSON.stringify({ grn_id: prtContext.grn_id, reason, return_items: JSON.stringify(items), status: 'Draft' })
  });
  if (!res) return;
  if (!res.ok) { showError(await getErrorMessage(res, 'Failed to save the purchase return.')); return; }
  const saved = await res.json();
  showToast(`Purchase return ${saved.id} saved as a draft.`, { variant: 'info' });
  renderView('purchase-returns');
}

async function postPurchaseReturn(id) {
  const confirmed = await showCustomConfirm(`Post purchase return ${id}? The stock leaves now and the vendor's debit note is raised and posted. This cannot be undone.`, 'Confirm Post');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/procurement/purchase-returns/${encodeURIComponent(id)}/post`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to post the purchase return.'); return; }
  const result = await res.json();
  if (result.debit_note_warning) {
    showToast(result.debit_note_warning, { variant: 'error', title: 'Debit note not posted' });
  } else if (result.debit_note_id) {
    showToast(`Posted. Debit note ${result.debit_note_id} for ${formatMoney(result.return_value)} raised and posted to the vendor.`, { variant: 'success' });
  } else {
    showToast('Posted. Only set-aside (rejected/damaged) stock went back, so no debit note was needed.', { variant: 'success' });
  }
  renderView('purchase-returns');
}

async function submitPurchaseReturnForApproval(id) {
  const res = await apiFetch('/api/v1/approval/submit', { method: 'POST', body: JSON.stringify({ doctype: 'PurchaseReturn', document_id: id }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to submit for approval.'); return; }
  showToast(`Purchase return ${id} submitted for approval.`, { variant: 'info' });
  renderView('purchase-returns');
}

async function cancelPurchaseReturn(id) {
  const confirmed = await showCustomConfirm(`Cancel purchase return ${id}? Its quantities become returnable again.`, 'Cancel Return');
  if (!confirmed) return;
  const getRes = await apiFetch(`/api/v1/doc/PurchaseReturn/${encodeURIComponent(id)}`);
  if (!getRes) return;
  if (!getRes.ok) { await showApiError(getRes, 'Failed to load the purchase return.'); return; }
  const doc = await getRes.json();
  doc.status = 'Cancelled';
  const res = await apiFetch(`/api/v1/doc/PurchaseReturn/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify(doc) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to cancel the purchase return.'); return; }
  renderView('purchase-returns');
}

export { renderApprovalsView, decideApproval, decideApprovalInner, newPODraft, renderPurchaseOrdersView, renderPOComposer, renderPOLines, schedulePOPreview, runPOPreview, poDraftPayload, renderPOSupplyBanner, renderPOTotals, savePurchaseOrder, savePurchaseOrderInner, renderPOPrintSheet, submitPOForApproval, submitDocForApproval, submitQualityInspectionForApproval, sendSubcontractOrder, receiveSubcontractOrder, mergeCustomerRow, renderGRNWorkbenchView, renderGRNLinesList, lookupGRNBarcode, addGRNLine, populateGRNBatchRow, loadGRNItemsFromPO, loadGRNItemsFromPOInner, loadGRNItemsFromASN, createGRN, createGRNInner, renderASNView, renderASNLinesList, addASNLine, createASN, createASNInner, renderPurchaseReturnsView, loadPurchaseReturnLines, savePurchaseReturn, postPurchaseReturn, submitPurchaseReturnForApproval, cancelPurchaseReturn };
