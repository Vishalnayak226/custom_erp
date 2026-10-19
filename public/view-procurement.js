import { documentSearchLink } from './view-document-links.js';

// Stage 62/63: transaction prints, the print-template studio and this
// screen's GRN/design styles live in trade-docs.js (+ trade-docs.css), loaded
// with this module's own version so a deploy refreshes them together - and
// kept out of the cold-core app.js/styles.css, which have a release budget.
const TRADE_DOCS_QUERY = new URL(import.meta.url).search;
const tradeDocsReady = import('./trade-docs.js' + TRADE_DOCS_QUERY);
// Stage 68: the transaction-screen bar's shared pieces, same version.
const { txnContext, txnMoney, txnRestorePane, txnPageTitle, txnNewButton, txnDone, txnPageOf, txnOnPage, txnPagedSection } = await import('./txn-screen.js' + TRADE_DOCS_QUERY);
// Stage 94: goods receipt in steps - the page a receipt is worked on, loaded
// when first opened, same version.
const grnStepsModule = () => import('./grn-steps.js' + TRADE_DOCS_QUERY);

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
          <th>Document</th>
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
    const reference = item.code || item.name || item.id;
    html += `
      <tr>
        <td>${escapeHTMLText(getDoctypeLabel(item.doctype))}</td>
        <td>${escapeHTMLText(reference)}${reference !== item.id ? `<div class="text-muted" style="font-size:11px;">Record ${escapeHTMLText(item.id)}</div>` : ''}</td>
        <td>${amount !== '' ? Number(amount).toLocaleString() : ''}</td>
        <td>${escapeHTMLText(loc)}</td>
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

// sku is the picked item's code; text is what the box shows (its name, or a
// SKU typed / scanned without picking).
function newPOEntry() {
  return { sku: '', text: '', qty: '1', rate: '', mrp: '' };
}

function newPODraft() {
  return {
    id: '', vendor: '', target_warehouse: '', location: '',
    // '' means "inherit the tenant default", which the first preview response
    // resolves and displays - the screen never hardcodes Exclusive itself.
    gst_mode: '',
    interstate: false, interstate_override: false,
    lines: [],
    // Stage 65.1: the entry row at the top of the composer; Add moves it into lines.
    entry: newPOEntry(),
    wasApproved: false, version: null
  };
}

// Stage 65.8 (the user: "Two page for new and history not in one page") /
// Stage 69 (the standard ERP shape, txn-screen.js): the PO list is the
// landing page, + New Purchase Order opens the composer as a page of its own -
// Edit is the same page with the PO loaded. The page is a browser history
// entry, so Back returns to the list; poDraft keeps a half-typed PO across a
// look at the list. This screen re-renders per page (txnOnPage).
let poPage = 'history';
txnOnPage('po-composer', () => renderView('purchase-orders'));

window.showPOPage = function(page) {
  window.txnShowPane(`po-composer:${page === 'new' ? 'new' : 'history'}`);
};

// + New Purchase Order: a PO left half-typed is picked up again; an Edit left
// unsaved is dropped for a blank PO.
window.newPurchaseOrder = function() {
  if (poDraft && poDraft.id) { poDraft = newPODraft(); poPreview = null; }
  showPOPage('new');
};

// poPageHead is the title both pages share; its list h1 keeps .page-title so a
// detour (Edit store) can name its way back ("Back to Purchase Orders").
function poPageHead(count) {
  const editing = poDraft && poDraft.id;
  return txnPageTitle({ screen: 'po-composer', title: 'Purchase Orders', count,
    page: editing ? `Edit ${escapeHTMLText(poDraft.po_number || poDraft.id)}` : 'New purchase order' });
}

async function renderPurchaseOrdersView(container) {
  if (!poDraft) poDraft = newPODraft();
  poPage = txnPageOf('po-composer', 'history');

  const screen = document.createElement('div');
  screen.className = poPage === 'history' ? 'txn-screen txn-on-landing' : 'txn-screen';
  screen.id = 'po-composer';
  screen.dataset.landing = 'history';
  container.appendChild(screen);
  if (poPage === 'new') {
    renderPOComposer(screen);
    return;
  }
  if (poPage === 'images') {
    await renderPOImagesPage(screen);
    return;
  }

  const res = await apiFetch('/api/v1/doc/PurchaseOrder');
  if (!res) return;
  const ordersLoadFailed = !res.ok;
  const orders = res.ok ? await res.json() : [];

  screen.innerHTML = `<div class="txn-bar">${poPageHead(orders.length)}${txnContext()}
    <button type="button" class="btn btn-primary txn-landing-only" ${actionAttrs('newPurchaseOrder', [])}>+ New Purchase Order</button></div>`;
  const panel = document.createElement('div');
  panel.className = 'txn-grid';
  // BLD-036: the New tab still works even if this list failed to load, so
  // this stays a partial-section message rather than a full renderErrorPanel
  // takeover - but it used to have no way back short of navigating away.
  let html = ordersLoadFailed
    ? `<p style="padding: 16px; color: var(--danger-color); font-size: 13px;">Failed to load existing purchase orders. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['purchase-orders'])}>Try Again</button></p>`
    : '';
  html += `
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>PO Number</th>
          <th>Source requisition</th>
          <th>Vendor</th>
          <th>Billing address</th>
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
    html += `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No purchase orders yet. Click <b>+ New Purchase Order</b>, add a vendor and at least one item, then <b>Create Draft</b>.</td></tr>`;
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
    const sent = (po.sent_to_vendor_at
      ? `<div class="po-sent-stamp" title="Sent ${escapeHTMLText(po.sent_to_vendor_at)}">Sent to vendor</div>` : '')
      + (po.auto_cancelled ? `<div class="po-sent-stamp" title="${escapeHTMLText(po.cancel_reason || '')}">Auto-cancelled</div>` : '');
    const live = po.status !== 'Closed' && po.status !== 'Cancelled';
    html += `
      <tr>
        <td style="font-family: monospace;">${copyableCell(poNumber, poNumber)}</td>
        <td>${documentSearchLink('PurchaseRequisition', po.source_requisition_id)}</td>
        <td>${escapeHTMLText(po.vendor || '')}</td>
        <td>${escapeHTMLText(po.location || '')}</td>
        <td>${lineCount === 0 ? '<span class="po-no-lines" title="This PO was raised before line items existed, or was created through the API without them.">No lines</span>' : `${lineCount} item${lineCount === 1 ? '' : 's'}`}</td>
        <td class="num">${formatMoney(po.total_amount)}</td>
        <td class="num">${po.grand_total != null ? formatMoney(po.grand_total) : '<span class="text-muted">&mdash;</span>'}</td>
        <td><span class="badge ${statusBadge}">${escapeHTMLText(po.status || '')}</span>${sent}</td>
        <td class="po-row-actions">
          ${po.status === 'Draft' ? `<button class="action-btn" ${actionAttrs('submitPOForApproval', [po.id])}>Submit for Approval</button>` : ''}
          ${po.status === 'Approved' ? `<button class="action-btn" ${actionAttrs('startGRNFromPO', [po.id])}>Receive goods</button>` : ''}
          ${po.status === 'Approved' || po.status === 'Closed' ? `<button class="action-btn" ${actionAttrs('showPOBalance', [po.id])}>Balance</button>` : ''}
          ${live ? `<button class="action-btn" ${actionAttrs('amendPurchaseOrder', [po.id])}>Edit</button>` : ''}
          <button class="action-btn" ${actionAttrs('printPurchaseOrder', [po.id])}>Print</button>
          ${po.status !== 'Cancelled' && lineCount > 0 ? `<button class="action-btn" ${actionAttrs('openPOImages', [po.id])}>Images</button>` : ''}
          ${po.status !== 'Draft' && po.status !== 'Cancelled' ? `<button class="action-btn" ${actionAttrs('sendPurchaseOrderToVendor', [po.id])}>Send to Vendor</button>` : ''}
          ${canDeleteDoctype('PurchaseOrder') ? `<button class="action-btn action-btn-danger" ${actionAttrs('deletePurchaseOrder', [po.id, poNumber])}>Delete</button>` : ''}
        </td>
      </tr>
    `;
  });
  html += `</tbody></table></div>`;
  panel.innerHTML = html;
  screen.appendChild(panel);
}

// Stage 95: a PO's Images page - its designs and Combination IDs with their
// images (view-images.js), a page of this screen like New. Which PO it shows
// survives a refresh.
const PO_IMAGES_KEY = 'stage95-po-images';
window.openPOImages = function(poId) {
  try { sessionStorage.setItem(PO_IMAGES_KEY, String(poId)); } catch (_) { /* storage can be disabled */ }
  window.txnShowPane('po-composer:images');
};

async function renderPOImagesPage(screen) {
  let poId = '';
  try { poId = sessionStorage.getItem(PO_IMAGES_KEY) || ''; } catch (_) { /* storage can be disabled */ }
  const head = poNumber => txnPageTitle({ screen: 'po-composer', title: 'Purchase Orders', page: poNumber ? `Images · ${escapeHTMLText(poNumber)}` : 'Images' });
  if (!poId) {
    screen.innerHTML = `<div class="txn-bar">${head('')}</div><p class="text-muted" style="padding:16px;">Open a purchase order's <b>Images</b> from the list.</p>`;
    return;
  }
  const m = await loadViewModule('/view-images.js');
  await m.renderPOImages(screen, poId, { head });
}

// Stage 65.2: Delete for roles with Delete on Purchase Order (Settings -> Roles).
// Any status, until goods are received against it - the server refuses that
// with PURCHA-0320 and names the receipt.
window.deletePurchaseOrder = async function(poId, poNumber) {
  if (!await showCustomConfirm(`Delete purchase order ${poNumber}? This cannot be undone from this screen.`, 'Delete Purchase Order')) return;
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`, { method: 'DELETE' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to delete the purchase order.', 'Delete Purchase Order'); return; }
  if (poDraft && poDraft.id === poId) { poDraft = newPODraft(); poPreview = null; }
  showToast(`Purchase order ${poNumber} deleted.`);
  renderView('purchase-orders');
};

// Stage 73.2.4 (D05): one view of a PO end to end - ordered, received,
// accepted, rejected, damaged, returned, short-closed, open, invoiced, paid -
// and the short-close of its open balance. A rejected unit stays open for the
// vendor to replace until the buyer short-closes; the short-close puts the
// rejected / damaged goods still held on a Draft purchase return (the claim).
window.showPOBalance = async function(poId) {
  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/reconciliation`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The purchase order balance could not be loaded.', 'PO Balance'); return; }
  const rec = await res.json();
  const n = v => { const x = Number(v) || 0; return x === Math.round(x) ? String(x) : x.toFixed(2); };
  const cell = v => `<td class="num">${n(v)}</td>`;
  const totalOpen = (rec.lines || []).reduce((s, l) => s + (Number(l.open) || 0), 0);
  let reasons = [];
  if (rec.status === 'Approved' && totalOpen > 0) {
    const rr = await apiFetch('/api/v1/doc/ReasonCode?limit=500');
    if (rr && rr.ok) {
      const docs = await rr.json();
      reasons = (Array.isArray(docs) ? docs : (docs.data || [])).filter(d => d.category === 'Cancellation' && (d.status || 'Active') === 'Active');
    }
  }
  document.getElementById('po-balance-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'po-balance-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 980px;" role="dialog" aria-modal="true" aria-labelledby="po-balance-title">
      <div class="modal-header">
        <h3 class="modal-title" id="po-balance-title">PO ${escapeHTMLText(rec.po_number)} - receipts and balance</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-po-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button>
      </div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">
        <p style="margin: 0;">${escapeHTMLText(rec.vendor_name || rec.vendor || '')} · <span class="badge ${rec.status === 'Closed' ? 'badge-secondary' : 'badge-success'}">${escapeHTMLText(rec.status)}</span>
          ${rec.short_close_info ? ` · ${escapeHTMLText(rec.short_close_info)}` : ''}</p>
        <div class="table-panel" style="overflow-x: auto;"><table>
          <thead><tr><th>Item</th><th class="num">Ordered</th><th class="num">Received</th><th class="num">Accepted</th><th class="num">Rejected</th><th class="num">Damaged</th><th class="num">Returned</th><th class="num">Short-closed</th><th class="num">Open</th><th class="num">Invoiced</th><th class="num">Paid</th></tr></thead>
          <tbody>${(rec.lines || []).map(l => `<tr><td>${escapeHTMLText(l.name || l.sku)}${l.name ? `<div style="font-size: 11px; color: var(--text-muted);">${escapeHTMLText(l.sku)}</div>` : ''}</td>
            ${cell(l.ordered)}${cell(l.received)}${cell(l.accepted)}${cell(l.rejected)}${cell(l.damaged)}${cell(l.returned)}${cell(l.short_closed)}<td class="num"><b>${n(l.open)}</b></td>${cell(l.invoiced)}${cell(l.paid)}</tr>`).join('')}</tbody>
        </table></div>
        <p style="margin: 0;">Ordered ₹${formatMoney(rec.ordered_value)} · Accepted ₹${formatMoney(rec.accepted_value)} · Open ₹${formatMoney(rec.open_value)} · Invoiced ₹${formatMoney(rec.invoiced_amount)} · Paid ₹${formatMoney(rec.paid_amount)}</p>
        <p style="margin: 0; color: var(--text-muted);">Receipts: ${(rec.grns || []).length ? rec.grns.map(g => `${escapeHTMLText(g.number)}${g.invoices && g.invoices.length ? ` (invoice ${escapeHTMLText(g.invoices.join(', '))})` : ' (not invoiced)'}`).join(' · ') : 'none yet'}
          ${(rec.claims || []).length ? `<br>Claims raised on short-close (Procurement → Purchase Return): ${escapeHTMLText(rec.claims.join(', '))}` : ''}</p>
        ${rec.status === 'Approved' && totalOpen > 0 ? `
        <div class="table-panel" style="padding: 12px;">
          <b>Short-close the open balance</b>
          <p style="margin: 4px 0 8px; color: var(--text-muted);">Rejected units stay open for the vendor to replace. If no replacement is coming, close the remaining ${n(totalOpen)} unit(s): the PO closes, takes no more receipts, and any rejected or damaged goods still held go on a Draft purchase return (the claim).</p>
          ${reasons.length ? `<div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
            <div class="form-group" style="margin: 0;"><label class="form-label" for="po-sc-reason">Reason</label>
              <select id="po-sc-reason" class="form-select"><option value="">Choose a reason...</option>${reasons.map(r => `<option value="${escapeHTMLText(r.id)}">${escapeHTMLText(r.code || r.id)}${r.description ? ` - ${escapeHTMLText(r.description)}` : ''}</option>`).join('')}</select></div>
            <div class="form-group" style="margin: 0;"><label class="form-label" for="po-sc-note">Note</label><input id="po-sc-note" class="form-input" maxlength="200"></div>
            <button type="button" class="btn btn-primary" id="po-sc-btn">Short-close</button></div>
            <div id="po-sc-error" class="login-error hidden"></div>`
          : `<p style="margin: 0;">No active reason codes of type <b>Cancellation</b> yet. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['ReasonCode'])}>Add a reason code</button></p>`}
        </div>` : ''}
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-po-close>Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-po-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  overlay.addEventListener('click', e => { if (e.target.closest('[data-act="openSetupDoctype"]')) close(); });
  requestAnimationFrame(() => overlay.classList.add('open'));
  const btn = overlay.querySelector('#po-sc-btn');
  if (btn) btn.addEventListener('click', () => guardAgainstDoubleSubmit(btn, 'Closing...', async () => {
    const errEl = overlay.querySelector('#po-sc-error');
    errEl.classList.add('hidden');
    const reason = overlay.querySelector('#po-sc-reason').value;
    if (!reason) { errEl.textContent = 'Choose a reason.'; errEl.classList.remove('hidden'); return; }
    if (!(await showCustomConfirm(`Close the remaining ${n(totalOpen)} unit(s) of PO ${rec.po_number}? No more goods can be received against it.`, 'Short-close'))) return;
    const r = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/short-close`, {
      method: 'POST', body: JSON.stringify({ reason_code: reason, note: overlay.querySelector('#po-sc-note').value.trim() }) });
    if (!r) return;
    if (!r.ok) { close(); await showApiError(r, 'The purchase order could not be short-closed.', 'Short-close'); return; }
    const out = await r.json();
    close();
    await showCustomAlert(`PO ${rec.po_number} closed: ${n(out.closed_units)} open unit(s) closed.${(out.claims || []).length ? ` Claim${out.claims.length === 1 ? '' : 's'} raised for the rejected / damaged goods: ${out.claims.join(', ')} (Procurement → Purchase Return, post to send them back).` : ''}`, 'Short-close');
    renderView('purchase-orders');
  }));
};

window.startGRNFromPO = function(poId) {
  try { sessionStorage.setItem('stage55-grn-source-po', String(poId)); } catch (_) { /* storage can be disabled */ }
  renderView('grn');
};

// formatMoney groups the Indian way (12,34,567.89), matching the printed PO
// and engines/amount_words.go's FormatIndianCurrency. en-IN is a built-in
// locale, so this needs no table of its own.
function renderPOComposer(panel) {
  const d = poDraft;
  const editing = !!d.id;
  // Stage 65.8 / 66: the frozen transaction layout (txn-screen.css) - one bar
  // (title, New / History, who and where, totals and tax, actions), the
  // header fields, the entry row, and the lines as the only scroller.
  // Editing an existing PO is this same page with its number fixed.
  panel.innerHTML = `
    <div class="txn-bar">
      ${poPageHead()}
      ${txnContext()}
      <div id="po-totals" class="txn-sum"></div>
      <div class="txn-actions">
        <div id="po-form-error" class="login-error hidden"></div>
        ${editing ? `<button class="btn btn-ghost btn-sm" id="po-cancel-edit">Cancel</button>` : ''}
        <button class="btn btn-primary" id="po-create-btn">${editing ? 'Save changes' : 'Create Draft'}</button>
      </div>
    </div>

    <div class="txn-head">
      ${editing ? `<div class="form-group">
        <label class="form-label" for="po-number">PO Number</label>
        <input type="text" id="po-number" class="form-input" readonly tabindex="-1" value="${escapeHTMLText(d.po_number || d.id)}" title="Editing keeps the same PO number">
      </div>` : autoNumberField('PO Number', 'PO', '100%')}
      <div class="form-group">
        <label class="form-label" for="po-vendor">Vendor</label>
        <input type="text" id="po-vendor" class="form-input" autocomplete="off" placeholder="Click to choose, or type to search">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-location">Billing address</label>
        <input type="text" id="po-location" class="form-input" placeholder="Pick a store or warehouse" value="${escapeHTMLText(d.location)}">
        <div class="txn-hint" id="po-bill-addr"></div>
      </div>
      <div class="form-group">
        <label class="form-label" for="po-warehouse">Shipping address</label>
        <input type="text" id="po-warehouse" class="form-input" placeholder="Where the goods are delivered" value="${escapeHTMLText(d.target_warehouse)}">
        <div class="txn-hint" id="po-ship-addr"></div>
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

    <div class="txn-entry" id="po-entry">
      <div class="form-group txn-entry-main">
        <label class="form-label" for="po-entry-sku">Item name or SKU</label>
        <input type="text" id="po-entry-sku" class="form-input" autocomplete="off" placeholder="Type an item name to choose its colour, size... or scan / search a SKU">
      </div>
      <div class="form-group txn-entry-wide pod-dno-group hidden" id="po-entry-dno-group">
        <label class="form-label" for="pod-dno">Design number</label>
        <input type="text" id="pod-dno" class="form-input" autocomplete="off" placeholder="e.g. the vendor's no">
        <div id="pod-dno-suggest" class="pod-suggest hidden"></div>
      </div>
      <div class="form-group">
        <label class="form-label" for="po-entry-qty" id="po-entry-qty-label">Qty</label>
        <input type="number" id="po-entry-qty" class="form-input num" min="1" step="1">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-entry-rate">Purchase price</label>
        <input type="number" id="po-entry-rate" class="form-input num" min="0" step="0.01">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-entry-mrp">MRP <span class="po-optional">optional</span></label>
        <input type="number" id="po-entry-mrp" class="form-input num" min="0" step="0.01" placeholder="&mdash;">
      </div>
      <button class="btn btn-primary btn-sm" id="po-entry-add" type="button" title="Add to the PO (Enter)">Add</button>
    </div>
    <div id="po-entry-error" class="po-line-error hidden"></div>
    <div id="po-entry-attrs" class="po-entry-attrs hidden"></div>

    <div class="txn-grid">
      <table class="po-lines">
        <thead><tr id="po-lines-head"></tr></thead>
        <tbody id="po-lines-body"></tbody>
      </table>
    </div>
  `;

  const vendorEl = document.getElementById('po-vendor');
  // Stage 64.7 (the user: "vendor should show me a drop down to select and ask
  // to create if not present"): the shell's standard picker, as Location
  // below - clicking the empty box lists vendors, and text that matches none
  // offers "Create <text>" inline and picks the new vendor.
  attachLinkTypeahead(vendorEl, 'Vendor');
  vendorEl.value = d.vendor || '';
  vendorEl.addEventListener('change', () => { d.vendor = vendorEl.value.trim(); schedulePOPreview(); });
  // A pick fires `change`, but a typed-and-blurred value has to be caught
  // too, or a vendor typed by hand never reaches the preview and the supply
  // type silently stays underived.
  vendorEl.addEventListener('blur', () => { if (vendorEl.value.trim() !== d.vendor) { d.vendor = vendorEl.value.trim(); schedulePOPreview(); } });

  const locEl = document.getElementById('po-location');
  const whEl = document.getElementById('po-warehouse');
  attachLinkTypeahead(locEl, 'Location');
  attachLinkTypeahead(whEl, 'Location');
  // Shipping starts as the billing store - the usual case - and stays the
  // user's to change; it is only filled while still empty.
  const commitBilling = () => {
    d.location = locEl.value.trim();
    if (d.location && !d.target_warehouse) { d.target_warehouse = d.location; whEl.value = d.location; }
    schedulePOPreview();
  };
  locEl.addEventListener('change', commitBilling);
  locEl.addEventListener('blur', commitBilling);
  const commitShipping = () => {
    if (whEl.value.trim() === d.target_warehouse) return;
    d.target_warehouse = whEl.value.trim();
    schedulePOPreview();
  };
  whEl.addEventListener('change', commitShipping);
  whEl.addEventListener('blur', commitShipping);

  document.getElementById('po-gst-mode').addEventListener('change', (e) => {
    d.gst_mode = e.target.value;
    schedulePOPreview();
  });

  // Stage 65.1 (the user: "my screen will be the create zone always"): one
  // entry row at the top; Add (or Enter) appends it to the list below, which
  // scrolls on its own while the header, totals and this row stay in view.
  // Stage 87 (the user: "It should ask attribute in PO itself"): the box takes
  // an item name or a SKU. A SKU adds one line; an item name opens its
  // attributes under the row (renderPOEntryAttrs) and Add makes the design.
  if (!d.entry) d.entry = newPOEntry();
  if (poDesign) { poDesign = null; d.entry.text = ''; } // not kept across a re-render
  const skuEl = document.getElementById('po-entry-sku');
  attachLinkTypeahead(skuEl, 'ItemName', { source: poEntrySearch, groupBy: '_group', onPick: pickPOEntry, noSetupHint: true });
  skuEl.value = d.entry.text || d.entry.sku || '';
  skuEl.addEventListener('input', () => {
    d.entry.text = skuEl.value;
    d.entry.sku = '';
    skuEl.title = '';
    if (poDesign) leavePODesignMode();
  });
  // Enter adds the line; an open menu takes Enter first (the picker stops it).
  // On an item name, Enter moves on to its design number.
  skuEl.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    if (poDesign) document.getElementById('pod-dno')?.focus();
    else addPOEntry();
  });
  ['qty', 'rate', 'mrp'].forEach(field => {
    const el = document.getElementById(`po-entry-${field}`);
    el.value = d.entry[field] || '';
    // With an item name open, these are every combination's defaults.
    const commit = () => {
      d.entry[field] = el.value.trim();
      if (poDesign && poDesign[field] !== d.entry[field]) { poDesign[field] = d.entry[field]; renderPOEntryAttrs(); }
    };
    el.addEventListener('change', commit);
    el.addEventListener('blur', commit);
    el.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); commit(); addPOEntry(); } });
  });
  document.getElementById('po-entry-add').addEventListener('click', addPOEntry);
  bindPODesignNumber();
  document.getElementById('po-create-btn').addEventListener('click', savePurchaseOrder);
  const cancelBtn = document.getElementById('po-cancel-edit');
  if (cancelBtn) cancelBtn.addEventListener('click', () => { poDraft = newPODraft(); poPreview = null; showPOPage('history'); });

  renderPOLines();
  renderPOAddresses();
  // Coming back to this page (from History, or from Edit store) re-reads the
  // stores, so a corrected address shows without retyping anything.
  if (d.vendor || d.location || d.target_warehouse || d.lines.length) schedulePOPreview();
}

// renderPOAddresses fills the line under Billing / Shipping address with the
// chosen store's address from the store master, and Edit store - which opens
// that store's own form and comes back here once it is saved (the address is
// the store's, so it is corrected there, for every document that uses it).
function renderPOAddresses() {
  const d = poDraft;
  const fill = (elId, code, store, sameAsBilling) => {
    const el = document.getElementById(elId);
    if (!el) return;
    if (!code) { el.innerHTML = ''; return; }
    const s = store || {};
    const text = sameAsBilling ? 'Same as billing' : !store ? ''
      : [s.address, [s.city, s.pincode].filter(Boolean).join(' '), s.state, s.gstin ? `GSTIN ${s.gstin}` : '']
          .filter(Boolean).join(', ') || 'No address on this store yet';
    el.innerHTML = `<span></span>${canUpdateDoctype('Location') ? ` <a href="#" class="empty-state-link" ${actionAttrs('editPOStore', [code], { prevent: true })}>Edit store</a>` : ''}`;
    el.firstChild.textContent = text;
    el.title = sameAsBilling ? '' : text;
  };
  fill('po-bill-addr', d.location, poPreview && poPreview.bill_to);
  fill('po-ship-addr', d.target_warehouse, poPreview && poPreview.ship_to, !!d.location && d.target_warehouse === d.location);
}

window.editPOStore = function(code) {
  openSetupDoctype('Location', code);
};

// poEntrySearch is the Add row's list: item names first (pick one to choose
// its attributes), then items by SKU / name. No match offers to create the
// item name, asking its Category, Product Type, HSN and GST once.
const PO_ENTRY_NAMES = 'Item names - choose colour, size...';
const PO_ENTRY_ITEMS = 'Items (SKU)';
async function poEntrySearch(q) {
  if (!q) return null;
  // Only the name / code (and an item's barcode) are matched - "a" finds the
  // names with an "a", not every row whose status reads "Active".
  const list = async (doctype, fields, limit) => {
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(q)}&q_fields=${fields}&limit=${limit}`).catch(() => null);
    const rows = res && res.ok ? await res.json() : [];
    return Array.isArray(rows) ? rows : [];
  };
  const [names, items] = await Promise.all([list('ItemName', 'name,code', 5), list('Item', 'name,code,barcode', 8)]);
  return [...names.map(doc => ({ ...doc, _group: PO_ENTRY_NAMES })), ...items.map(doc => ({ ...doc, _group: PO_ENTRY_ITEMS }))];
}

// pickPOEntry: an item opens nothing - Enter / Add puts it on the PO; an item
// name (or one just created from the list) opens its attributes.
// Stage 94.22: a plain SKU (not a design's combination) whose item name has
// attributes asked in the PO opens that item name's matrix instead - the
// colour / size are chosen, not skipped. A SKU with no item name has no
// matrix; it goes on as it is, with a hint how to order it in variants.
async function pickPOEntry(doc) {
  const skuEl = document.getElementById('po-entry-sku');
  if (!skuEl || !doc) return;
  const errEl = document.getElementById('po-entry-error');
  if (errEl) errEl.classList.add('hidden');
  if (doc._group === PO_ENTRY_ITEMS && !doc.family && !doc.parent_product_code) {
    if (doc.item_name) {
      const res = await apiFetch(`/api/v1/procurement/design-matrix?item_name=${encodeURIComponent(doc.item_name)}`).catch(() => null);
      const m = res && res.ok ? await res.json() : null;
      if (m && Array.isArray(m.attributes) && m.attributes.length) { enterPODesignMode(doc.item_name); return; }
    } else if (errEl) {
      errEl.textContent = `${doc.name || doc.code || doc.id} has no item name, so it has no colour / size to choose. To order it in variants, create its item name (Setup → Item Name) and pick that from the list.`;
      errEl.classList.remove('hidden');
    }
  }
  if (doc._group === PO_ENTRY_ITEMS) {
    if (poDesign) leavePODesignMode();
    poDraft.entry.sku = doc.code || doc.id;
    poDraft.entry.text = skuEl.value = doc.name || poDraft.entry.sku;
    skuEl.title = poDraft.entry.sku;
    return;
  }
  enterPODesignMode(doc.code || doc.id);
}

async function addPOEntry() {
  if (!poDesign) { await addPOEntryLine(); return; }
  await guardAgainstDoubleSubmit(document.getElementById('po-entry-add'), 'Adding...', createPODesign);
  syncPOEntryButton(); // the guard puts back the label it found
}

// addPOEntryLine moves the entry row into the PO's lines and clears it for the
// next item, keeping the cursor in the item box.
// Text that was typed, not picked, goes on only when it is an item's exact SKU
// or barcode (a scan + Enter) - a name nobody picked or created is refused.
async function addPOEntryLine() {
  const d = poDraft;
  const errEl = document.getElementById('po-entry-error');
  const el = id => document.getElementById(id);
  const typed = el('po-entry-sku').value.trim();
  const refuse = (msg) => {
    errEl.textContent = msg;
    errEl.classList.remove('hidden');
    el('po-entry-sku').focus();
  };
  if (!d.entry.sku && !typed) { refuse('Choose an item first.'); return; }
  const sku = d.entry.sku || await resolvePOEntrySKU(typed);
  if (!sku) {
    refuse(`"${typed}" is not an item. Choose one from the list, or pick + Create to add it.`);
    return;
  }
  const line = {
    sku,
    qty: el('po-entry-qty').value.trim() || '1',
    rate: el('po-entry-rate').value.trim(),
    mrp: el('po-entry-mrp').value.trim()
  };
  if (!(Number(line.qty) > 0)) {
    errEl.textContent = 'Quantity must be at least 1.';
    errEl.classList.remove('hidden');
    el('po-entry-qty').focus();
    return;
  }
  errEl.classList.add('hidden');
  d.lines.push(line);
  d.entry = newPOEntry();
  ['po-entry-sku', 'po-entry-rate', 'po-entry-mrp'].forEach(id => { el(id).value = ''; });
  el('po-entry-qty').value = d.entry.qty;
  renderPOLines();
  const added = document.querySelector(`[data-po-line="${d.lines.length - 1}"]`);
  if (added) {
    added.classList.add('po-line-just-added');
    setTimeout(() => added.classList.remove('po-line-just-added'), 1600);
  }
  schedulePOPreview();
  el('po-entry-sku').focus();
}

// resolvePOEntrySKU returns the code of the item whose SKU or barcode is
// exactly the typed text, or '' when none is.
async function resolvePOEntrySKU(text) {
  const res = await apiFetch(`/api/v1/doc/Item?q=${encodeURIComponent(text)}&q_fields=code,barcode&limit=20`).catch(() => null);
  const rows = res && res.ok ? await res.json() : [];
  if (!Array.isArray(rows)) return '';
  const same = (v) => String(v || '').toLowerCase() === text.toLowerCase();
  const hit = rows.find(r => (r.code || r.id) === text || r.barcode === text)
    || rows.find(r => same(r.code || r.id) || same(r.barcode));
  return hit ? (hit.code || hit.id) : '';
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
  // Stage 87: a column per attribute the lines carry (Colour, Size...), from
  // the preview - the same values the print shows.
  const attrCols = (poPreview && poPreview.attribute_columns) || [];
  const head = document.getElementById('po-lines-head');
  if (head) {
    head.innerHTML = `<th style="min-width:190px;">Item</th>
      ${attrCols.map(a => `<th style="width:90px;">${escapeHTMLText(a.label)}</th>`).join('')}
      <th class="num" style="width:80px;">Qty</th>
      <th class="num" style="width:120px;">Purchase Price</th>
      <th class="num" style="width:110px;">MRP <span class="po-optional">optional</span></th>
      <th style="width:90px;">HSN</th>
      <th class="num" style="width:70px;">GST %</th>
      <th class="num" style="width:110px;">Taxable</th>
      <th class="num" style="width:100px;">Tax</th>
      <th class="num" style="width:120px;">Line Total</th>
      <th style="width:36px;"></th>`;
  }

  poLinesRebuilding = true;
  body.innerHTML = d.lines.length === 0
    ? `<tr><td colspan="${10 + attrCols.length}" class="po-lines-empty">No items yet &mdash; type an item name or a SKU above.</td></tr>`
    : d.lines.map((line, i) => {
    const p = previewLines[i] || {};
    const err = p.error ? `<div class="po-line-error">${escapeHTMLText(p.error)}</div>` : '';
    const derived = (v, suffix = '') => (v === undefined || v === null || v === '' ? '<span class="text-muted">&mdash;</span>' : escapeHTMLText(String(v)) + suffix);
    return `
      <tr data-po-line="${i}"${p.error ? ' class="po-line-flagged"' : ''}>
        <td>
          <input type="text" class="form-input" data-po-field="sku" value="${escapeHTMLText(line.sku)}" placeholder="Search item..." title="${escapeHTMLText(p.item_name || '')}">
          ${err}
        </td>
        ${attrCols.map(a => `<td class="po-derived">${derived((p.attributes || {})[a.code])}</td>`).join('')}
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
    attachLinkTypeahead(skuInput, 'Item', { filters: { q_fields: 'name,code,barcode' } });
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
      renderPOLines();
      schedulePOPreview();
    });
  });

  renderPOTotals();
}

// ---------------------------------------------------------------------------
// Add by item name (Stage 62.5/62.6, reworked in Stage 65.4; in the Add row
// itself since Stage 87 - it was a panel behind "+ Add design" before)
//
// The user: "it should always ask item name and all attributes - parent and
// child. Parent = item name, HSN, tax; child = variant attributes, several
// per parent; once added it auto-creates the design id and combination id.
// HSN once defined for an item name is never asked again."
//
// Parent: an Item Name (Category + Product Type, Stage 65.3) picked in the
// Add row or created from its list; its HSN and GST are shown, not asked. Then a design
// number, typed: the item name's existing designs that START with what is
// typed are suggested in a fixed order, and when none match the list simply
// empties - a new number is a new design. Child: the org's attribute matrix
// (every Active ItemAttribute, the ERP's list) - one value picked is a design attribute,
// several make combinations - with a value, or a whole attribute, addable
// right here (saved to PIM). Add makes the design (ProductFamily, numbered
// Design/..., linked to the item name) and one Item per combination through
// the ordinary record API, adds them as PO lines, and keeps the item name for
// the next design number.
// ---------------------------------------------------------------------------
let poDesign = null;

function newPODesignState(matrix) {
  return {
    matrix, itemName: null, designs: [], designNumber: '', loaded: null,
    rate: '', mrp: '', qty: '1',
    // vrows: one per variant - its attribute values, and qty / rate / mrp
    // only where they differ from the Add row's.
    typed: {}, vrows: [{ values: {} }]
  };
}

// enterPODesignMode: an item name picked in the Add row. The row's qty /
// price / MRP become every combination's defaults.
async function enterPODesignMode(code) {
  await tradeDocsReady.catch(() => null); // styles
  // Stage 89: only the attributes asked in the PO, as the item name's type maps them.
  const res = await apiFetch(`/api/v1/procurement/design-matrix?item_name=${encodeURIComponent(code || '')}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load the product attributes.'); return; }
  const d = newPODesignState(await res.json());
  const e = poDraft.entry;
  d.qty = e.qty || '1'; d.rate = e.rate || ''; d.mrp = e.mrp || '';
  d.before = { qty: e.qty, rate: e.rate, mrp: e.mrp };
  poDesign = d;
  await pickPOItemName(code);
}

// leavePODesignMode closes the attributes; the box keeps what is typed in it,
// and the row gets back its own qty / price / MRP - a design's price must not
// carry on to the next SKU.
function leavePODesignMode() {
  const before = poDesign && poDesign.before;
  poDesign = null;
  if (before) {
    ['qty', 'rate', 'mrp'].forEach(f => {
      poDraft.entry[f] = before[f] || (f === 'qty' ? '1' : '');
      const el = document.getElementById(`po-entry-${f}`);
      if (el) el.value = poDraft.entry[f];
    });
  }
  renderPOEntryAttrs();
}

// poDesignValues: the distinct values the variant rows give an attribute.
function poDesignValues(d, code) {
  return [...new Set(d.vrows.map(r => r.values[code]).filter(Boolean))];
}

// poDesignAxes / poDesignAttributes split the rows' values into combination
// axes (2+ values across the rows) and design-level attributes (one value,
// the same on every row, or typed).
function poDesignAxes(d) {
  return d.matrix.attributes.filter(a => a.value_type === 'Select' && poDesignValues(d, a.code).length > 1);
}
function poDesignAttributes(d) {
  const out = {};
  d.matrix.attributes.forEach(a => {
    if (a.value_type === 'Select') {
      const vals = poDesignValues(d, a.code);
      if (vals.length === 1) out[a.code] = vals[0];
    } else if ((d.typed[a.code] || '').trim()) {
      out[a.code] = d.typed[a.code].trim();
    }
  });
  return out;
}

// poDesignCombinations is one combination per variant row, in row order, with
// its axis values and a stable key ("color:Red;size:M") used to spot a repeated
// row and to match an existing Item of a loaded design.
function poDesignCombinations(d) {
  const axes = poDesignAxes(d);
  return d.vrows.map(r => {
    const c = {};
    axes.forEach(a => { if (r.values[a.code]) c[a.code] = r.values[a.code]; });
    return { values: c, row: r, key: Object.keys(c).sort().map(k => `${k}:${c[k]}`).join(';') };
  });
}

// poDesignRowProblem: why the variant rows cannot be added yet, or ''.
function poDesignRowProblem(d) {
  for (const a of d.matrix.attributes.filter(x => x.value_type === 'Select')) {
    const filled = d.vrows.filter(r => r.values[a.code]).length;
    if (filled && filled < d.vrows.length) return `Choose ${a.label} on every row, or leave it empty on all of them.`;
  }
  const keys = poDesignCombinations(d).map(c => c.key);
  const dup = keys.find((k, i) => keys.indexOf(k) !== i);
  if (dup !== undefined) return `Two rows are the same variant${dup ? ` (${dup.replace(/;/g, ', ')})` : ''} - change one or remove it.`;
  return '';
}

function poDesignExistingFor(d, values) {
  if (!d.loaded) return null;
  const want = Object.keys(values).sort().map(k => `${k.toLowerCase()}:${String(values[k]).toLowerCase()}`).join(';');
  return (d.loaded.combinations || []).find(c => {
    const o = c.options || {};
    const keys = Object.keys(o).filter(k => o[k] !== '' && Object.prototype.hasOwnProperty.call(values, k));
    const have = keys.sort().map(k => `${k.toLowerCase()}:${String(o[k]).toLowerCase()}`).join(';');
    return keys.length === Object.keys(values).length && have === want;
  }) || null;
}

// The design number a design is known by: the one the buyer typed, else its
// system code (designs made before Stage 65.4 have no typed number).
const poDesignLabel = f => (f.design_number || f.code || f.id || '');

// poDesignSuggestions: the item name's designs whose number starts with what
// is typed, in one fixed order - never re-sorted by the typing, and empty
// (not repopulated with something else) when nothing matches.
function poDesignSuggestions(d, typed) {
  const t = String(typed || '').trim().toLowerCase();
  return d.designs
    .filter(f => !t || poDesignLabel(f).toLowerCase().startsWith(t))
    .slice(0, 8);
}

async function pickPOItemName(code) {
  const d = poDesign;
  if (!d) return;
  if (!code) { leavePODesignMode(); return; }
  const res = await apiFetch(`/api/v1/doc/ItemName/${encodeURIComponent(code)}`);
  if (!res) return;
  if (!res.ok) { poDesign = null; await showApiError(res, 'Could not load that item name.'); return; }
  const doc = await res.json();
  const cat = doc.category ? await lookupLinkDoc('Category', doc.category).catch(() => null) : null;
  d.itemName = { code: doc.code || doc.id, name: doc.name || doc.code, hsn_code: doc.hsn_code || '', gst_rate: doc.gst_rate, category: (cat && cat.name) || doc.category || '' };
  const fRes = await apiFetch(`/api/v1/doc/ProductFamily?item_name=${encodeURIComponent(d.itemName.code)}&limit=500`);
  const fams = fRes && fRes.ok ? await fRes.json() : [];
  d.designs = (Array.isArray(fams) ? fams : []).slice().sort((a, b) => poDesignLabel(a).localeCompare(poDesignLabel(b), undefined, { numeric: true }));
  d.designNumber = '';
  d.loaded = null;
  if (poDesign !== d) return; // the box changed while this loaded
  const skuEl = document.getElementById('po-entry-sku');
  if (skuEl) { skuEl.value = poDraft.entry.text = d.itemName.name; skuEl.title = d.itemName.code; }
  poDraft.entry.sku = '';
  renderPOEntryAttrs();
  const dnoEl = document.getElementById('pod-dno');
  if (dnoEl) { dnoEl.value = ''; dnoEl.focus(); }
}

// syncPOEntryButton: on an item name, Add says how many combinations it adds
// and is off while every one is removed.
function syncPOEntryButton() {
  const btn = document.getElementById('po-entry-add');
  if (!btn) return;
  const d = poDesign;
  const kept = d && d.itemName ? d.vrows.length : -1;
  btn.textContent = kept > 1 ? `Add ${kept}` : 'Add';
  btn.disabled = kept === 0;
  btn.title = kept >= 0 ? 'Create the design and add its combinations to the PO (Enter)' : 'Add to the PO (Enter)';
}

// renderPOEntryAttrs draws the picked item name's attributes under the Add
// row: what the item name brings (HSN, GST), the design number's state, a chip
// per value, and - with more than one combination - each combination's qty /
// price / MRP. Nothing when no item name is picked.
function renderPOEntryAttrs() {
  const box = document.getElementById('po-entry-attrs');
  if (!box) return;
  const d = poDesign;
  const inm = d && d.itemName;
  box.classList.toggle('hidden', !inm);
  document.getElementById('po-entry-dno-group')?.classList.toggle('hidden', !inm);
  const qtyLabel = document.getElementById('po-entry-qty-label');
  if (qtyLabel) qtyLabel.textContent = inm ? 'Qty each' : 'Qty';
  syncPOEntryButton();
  if (!inm) { box.innerHTML = ''; return; }
  // Stage 94.28 (the user: "1 Parent row than varinat columns Variant row. I
  // will choose those matrix and fill"): the Add row is the parent; under it
  // one row per variant, a column per attribute, each value chosen in its cell.
  const selectAttrs = d.matrix.attributes.filter(a => a.value_type === 'Select');
  const otherAttrs = d.matrix.attributes.filter(a => a.value_type !== 'Select');
  const combos = poDesignCombinations(d);
  // Stage 81: a client whose attributes were set up from the library adds
  // values here, never a new attribute.
  const canAddAttr = canCreateDoctype('ItemAttribute') && !(d.matrix && d.matrix.attributes_closed);
  const canAddValue = canUpdateDoctype('ItemAttribute');
  const exact = d.designNumber.trim() && !d.loaded
    ? d.designs.find(f => poDesignLabel(f).toLowerCase() === d.designNumber.trim().toLowerCase()) : null;
  const dnoEl = document.getElementById('pod-dno');
  if (dnoEl) {
    dnoEl.readOnly = !!d.loaded;
    if (d.loaded) dnoEl.value = poDesignLabel(d.loaded);
  }
  const cell = (a, i) => {
    const v = d.vrows[i].values[a.code] || '';
    const opts = (a.options || []).slice();
    if (v && !opts.includes(v)) opts.push(v);
    return `<select class="form-input" data-pod-cell="${i}" data-pod-attr="${escapeHTMLText(a.code)}" aria-label="${escapeHTMLText(a.label)}">
      <option value="">&mdash;</option>
      ${opts.map(o => `<option value="${escapeHTMLText(o)}"${o === v ? ' selected' : ''}>${escapeHTMLText(o)}</option>`).join('')}
      ${canAddValue ? '<option value="__new">+ New value...</option>' : ''}
    </select>`;
  };
  const num = (i, field, step) => `<td><input type="number" class="form-input num" min="0" step="${step}" data-pod-vrow="${i}" data-pod-field="${field}" value="${escapeHTMLText(d.vrows[i][field] ?? d[field])}"></td>`;
  box.innerHTML = `
    <div class="po-entry-attrs-head">
      <span><strong>${escapeHTMLText(inm.name)}</strong>${inm.category ? ` &middot; ${escapeHTMLText(inm.category)}` : ''} &middot; HSN <strong>${escapeHTMLText(inm.hsn_code || '—')}</strong> &middot; GST <strong>${escapeHTMLText(String(inm.gst_rate ?? '—'))}%</strong></span>
      <span class="po-design-mode">${d.loaded
        ? `Adding to design <strong>${escapeHTMLText(poDesignLabel(d.loaded))}</strong> (${escapeHTMLText(d.loaded.code)}) &mdash; its ${(d.loaded.combinations || []).length} existing combination(s) are listed. <button type="button" class="link-btn" id="pod-unload">Another design number</button>`
        : exact ? `Design <strong>${escapeHTMLText(poDesignLabel(exact))}</strong> exists &mdash; <button type="button" class="link-btn" id="pod-use-exact">load it</button>`
        : d.designNumber.trim() ? '<span class="badge badge-secondary">New design</span>'
        : `${d.designs.length} design(s) under this item name.`}</span>
      <button type="button" class="link-btn" id="pod-close">Clear item name</button>
    </div>
    ${otherAttrs.length ? `<div class="po-design-row">${otherAttrs.map(a => `
      <div class="form-group"><label class="form-label">${escapeHTMLText(a.label)}</label><input type="${a.value_type === 'Number' ? 'number' : a.value_type === 'Date' ? 'date' : 'text'}" class="form-input" style="width:150px;" data-pod-typed="${escapeHTMLText(a.code)}" value="${escapeHTMLText(d.typed[a.code] || '')}"></div>`).join('')}</div>` : ''}
    ${selectAttrs.length === 0 && otherAttrs.length === 0 ? `<p class="text-muted">No product attributes yet.${canAddAttr ? ' Add one with <b>+ Attribute</b> below (e.g. <i>Colour</i> with <i>Gold</i>).' : ' Ask an administrator to add them under Setup &rarr; Item Attribute.'}</p>` : ''}
    <div class="table-wrapper">
      <table class="po-lines pod-variants">
        <thead><tr>${selectAttrs.map(a => `<th>${escapeHTMLText(a.label)}</th>`).join('')}${selectAttrs.length ? '' : '<th>Item</th>'}<th class="num" style="width:90px;">Qty</th><th class="num" style="width:120px;">Purchase price</th><th class="num" style="width:110px;">MRP</th>${d.loaded ? '<th>Combination ID</th>' : ''}<th style="width:36px;"></th></tr></thead>
        <tbody>
          ${d.vrows.map((r, i) => {
            const existing = d.loaded ? poDesignExistingFor(d, combos[i].values) : null;
            return `<tr>
              ${selectAttrs.map(a => `<td>${cell(a, i)}</td>`).join('')}${selectAttrs.length ? '' : `<td>${escapeHTMLText(inm.name)}</td>`}
              ${num(i, 'qty', 1)}${num(i, 'rate', 0.01)}${num(i, 'mrp', 0.01)}
              ${d.loaded ? `<td>${existing ? `<span class="badge badge-secondary" title="Already exists - reused">${escapeHTMLText(existing.sku)}</span>` : '<span class="text-muted">new</span>'}</td>` : ''}
              <td><button type="button" class="po-line-remove" data-pod-delrow="${i}" title="Remove this row" aria-label="Remove row">&times;</button></td>
            </tr>`;
          }).join('')}
        </tbody>
      </table>
    </div>
    <div class="pod-combos-head">
      <button type="button" class="btn btn-outline btn-sm" id="pod-add-row" title="Another variant - starts as a copy of the last row">+ Row</button>
      ${canAddAttr ? `<span class="pod-add-attr">
        <input type="text" class="form-input" id="pod-new-attr" placeholder="New attribute, e.g. Stone">
        <input type="text" class="form-input" id="pod-new-attr-value" placeholder="first value, e.g. Ruby">
        <button type="button" class="btn btn-outline btn-sm" id="pod-new-attr-add">+ Attribute</button>
      </span>` : ''}
      <span class="text-muted">Qty, price and MRP come from the row above; change any one here.</span>
    </div>
  `;

  document.getElementById('pod-close').addEventListener('click', () => {
    leavePODesignMode();
    poDraft.entry.text = '';
    poDraft.entry.sku = '';
    const skuEl = document.getElementById('po-entry-sku');
    if (skuEl) { skuEl.value = ''; skuEl.title = ''; skuEl.focus(); }
  });
  const unload = document.getElementById('pod-unload');
  if (unload) unload.addEventListener('click', () => {
    d.loaded = null; d.designNumber = ''; d.vrows = [{ values: {} }];
    if (dnoEl) { dnoEl.readOnly = false; dnoEl.value = ''; }
    renderPOEntryAttrs();
    dnoEl?.focus();
  });
  const useExact = document.getElementById('pod-use-exact');
  if (useExact && exact) useExact.addEventListener('click', () => loadPODesign(exact.code || exact.id));
  // A changed row makes a refusal shown at Add stale.
  const clearErr = () => document.getElementById('po-entry-error')?.classList.add('hidden');
  const focusCell = (i, code) => box.querySelector(`[data-pod-cell="${i}"]${code ? `[data-pod-attr="${CSS.escape(code)}"]` : ''}`)?.focus();
  box.querySelectorAll('[data-pod-cell]').forEach(sel => sel.addEventListener('change', async () => {
    const i = Number(sel.getAttribute('data-pod-cell'));
    const code = sel.getAttribute('data-pod-attr');
    clearErr();
    if (sel.value === '__new') {
      const a = d.matrix.attributes.find(x => x.code === code) || { label: code };
      const v = await showCustomPrompt(`New ${a.label} value:`, '', `Add a ${a.label}`);
      if (v && String(v).trim()) await addPODesignAttributeValue(code, '', v, i);
      else renderPOEntryAttrs();
      focusCell(i, code);
      return;
    }
    if (sel.value) d.vrows[i].values[code] = sel.value; else delete d.vrows[i].values[code];
    renderPOEntryAttrs();
    focusCell(i, code);
  }));
  const newAttrBtn = document.getElementById('pod-new-attr-add');
  if (newAttrBtn) newAttrBtn.addEventListener('click', () => addPODesignAttributeValue('', document.getElementById('pod-new-attr').value, document.getElementById('pod-new-attr-value').value));
  box.querySelectorAll('[data-pod-typed]').forEach(input => input.addEventListener('input', () => {
    d.typed[input.getAttribute('data-pod-typed')] = input.value;
  }));
  box.querySelectorAll('[data-pod-vrow]').forEach(input => input.addEventListener('input', () => {
    d.vrows[Number(input.getAttribute('data-pod-vrow'))][input.getAttribute('data-pod-field')] = input.value;
  }));
  box.querySelectorAll('[data-pod-delrow]').forEach(btn => btn.addEventListener('click', () => {
    clearErr();
    d.vrows.splice(Number(btn.getAttribute('data-pod-delrow')), 1);
    if (!d.vrows.length) d.vrows.push({ values: {} }); // there is always a row to fill
    renderPOEntryAttrs();
  }));
  document.getElementById('pod-add-row').addEventListener('click', () => {
    clearErr();
    const last = d.vrows[d.vrows.length - 1];
    d.vrows.push({ values: { ...(last ? last.values : {}) } });
    renderPOEntryAttrs();
    focusCell(d.vrows.length - 1, selectAttrs[0] && selectAttrs[0].code);
  });
}

// bindPODesignNumber wires the Add row's design-number box once per render of
// the screen. Suggestions update in place while typing (no rebuild, so the
// caret never jumps); the state line follows on blur.
function bindPODesignNumber() {
  const dnoEl = document.getElementById('pod-dno');
  const suggestEl = document.getElementById('pod-dno-suggest');
  if (!dnoEl || !suggestEl) return;
  const paintSuggest = () => {
    const d = poDesign;
    if (!d || d.loaded) { suggestEl.classList.add('hidden'); return; }
    const list = poDesignSuggestions(d, dnoEl.value);
    suggestEl.innerHTML = list.map(f => `<button type="button" class="pod-suggest-item" data-pod-design="${escapeHTMLText(f.code || f.id)}"><strong>${escapeHTMLText(poDesignLabel(f))}</strong>${f.design_number ? ` <span class="text-muted">${escapeHTMLText(f.code || '')}</span>` : ''}</button>`).join('');
    suggestEl.classList.toggle('hidden', list.length === 0);
    suggestEl.querySelectorAll('[data-pod-design]').forEach(btn => btn.addEventListener('mousedown', (e) => {
      e.preventDefault();
      loadPODesign(btn.getAttribute('data-pod-design'));
    }));
  };
  dnoEl.addEventListener('input', () => { if (poDesign) { poDesign.designNumber = dnoEl.value; paintSuggest(); } });
  dnoEl.addEventListener('focus', paintSuggest);
  dnoEl.addEventListener('blur', () => setTimeout(() => { suggestEl.classList.add('hidden'); if (poDesign && !poDesign.loaded) renderPOEntryAttrs(); }, 150));
  // Enter moves on along the row.
  dnoEl.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); document.getElementById('po-entry-qty')?.focus(); } });
}

// addPODesignAttributeValue saves a new value (or a new attribute with its
// first value) to PIM and sets it on variant row `row` - or, for a new
// attribute, on every row.
async function addPODesignAttributeValue(code, label, value, row) {
  const d = poDesign;
  if (!d) return;
  value = String(value || '').trim();
  label = String(label || '').trim();
  if (!value || (!code && !label)) return;
  const res = await apiFetch('/api/v1/procurement/design-attribute', { method: 'POST', body: JSON.stringify({ code, label, value, item_name: poDesign?.itemName?.code || '' }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not add the value.'); return; }
  const attr = await res.json();
  const i = d.matrix.attributes.findIndex(a => a.code === attr.code);
  if (i >= 0) d.matrix.attributes[i] = { ...d.matrix.attributes[i], options: attr.options };
  else d.matrix.attributes.unshift(attr);
  const picked = attr.options.find(o => o.toLowerCase() === value.toLowerCase()) || value;
  if (row != null && d.vrows[row]) d.vrows[row].values[attr.code] = picked;
  else d.vrows.forEach(r => { if (!r.values[attr.code]) r.values[attr.code] = picked; });
  renderPOEntryAttrs();
}

// loadPODesign: an existing design number always takes that same design; its
// attributes and existing combinations come back, and any value picked beyond
// them becomes a new combination.
async function loadPODesign(code) {
  const d = poDesign;
  if (!d || !code) return;
  const res = await apiFetch(`/api/v1/procurement/design/${encodeURIComponent(code)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, `Design ${code} was not found.`); return; }
  const design = await res.json();
  const listed = d.designs.find(f => (f.code || f.id) === design.code);
  design.design_number = listed ? listed.design_number : '';
  applyLoadedDesign(d, design);
  // The design's own price / MRP fill the row where it is still empty.
  ['rate', 'mrp'].forEach(f => {
    const el = document.getElementById(`po-entry-${f}`);
    if (el && !el.value && d[f]) el.value = poDraft.entry[f] = d[f];
  });
  renderPOEntryAttrs();
}

function applyLoadedDesign(d, design) {
  d.loaded = design;
  d.designNumber = poDesignLabel(design);
  if (design.purchase_rate && !d.rate) d.rate = String(design.purchase_rate);
  if (design.mrp && !d.mrp) d.mrp = String(design.mrp);
  const attrs = design.attributes || {};
  const byLower = {};
  d.matrix.attributes.forEach(a => { byLower[a.code.toLowerCase()] = a; });
  const opt = (a, v) => (a.options || []).find(o => o.toLowerCase() === String(v).toLowerCase()) || v;
  const base = {};
  Object.keys(attrs).forEach(k => {
    const a = byLower[k.toLowerCase()];
    if (!a || !attrs[k]) return;
    if (a.value_type === 'Select') base[a.code] = opt(a, attrs[k]);
    else d.typed[a.code] = attrs[k];
  });
  // One row per existing combination (marked with its combination ID); new
  // variants are added as rows next to them.
  const rows = (design.combinations || []).map(c => {
    const values = { ...base };
    Object.keys(c.options || {}).forEach(k => {
      const a = byLower[k.toLowerCase()];
      if (a && a.value_type === 'Select' && c.options[k]) values[a.code] = opt(a, c.options[k]);
    });
    return { values };
  });
  d.vrows = rows.length ? rows : [{ values: base }];
}

async function createPODesign() {
  const d = poDesign;
  if (!d) return;
  const errEl = document.getElementById('po-entry-error');
  const fail = (msg) => { errEl.textContent = msg; errEl.classList.remove('hidden'); };
  errEl.classList.add('hidden');
  const inm = d.itemName;
  if (!inm) return fail('Pick the item name first.');
  if (!poDraft.vendor) {
    document.getElementById('po-vendor')?.focus();
    return fail('Pick the vendor first - a new design is recorded as bought from this PO\'s vendor.');
  }
  const hsn = String(inm.hsn_code || '').trim();
  const gst = Number(inm.gst_rate);
  if (!hsn || inm.gst_rate === '' || inm.gst_rate == null || isNaN(gst)) {
    return fail(`${inm.name} has no HSN / GST yet - open it under Setup and fill them in once; every design under it then uses them.`);
  }
  const problem = poDesignRowProblem(d);
  if (problem) return fail(problem);
  const kept = poDesignCombinations(d);
  const attrs = poDesignAttributes(d);
  const attrString = Object.keys(attrs).sort().map(k => `${k}:${attrs[k]}`).join(';');
  const typedNo = d.designNumber.trim();

  // 1. The design: the loaded one, an existing one with this number, a
  // matching one (Reuse mode, no number typed), or a new one.
  if (!d.loaded && typedNo) {
    const same = d.designs.find(f => poDesignLabel(f).toLowerCase() === typedNo.toLowerCase());
    if (same) {
      await loadPODesign(same.code || same.id);
      return fail(`Design ${typedNo} already exists, so it was loaded - check its combinations, then Add again.`);
    }
  }
  let designCode = d.loaded ? d.loaded.code : '';
  if (!designCode && !typedNo && d.matrix.design_number_mode === 'match') {
    const mRes = await apiFetch('/api/v1/procurement/design/match', { method: 'POST', body: JSON.stringify({ name: inm.name, vendor: poDraft.vendor || '', attributes: attrs }) });
    if (mRes && mRes.ok) {
      const m = await mRes.json();
      if (m.code) {
        const gRes = await apiFetch(`/api/v1/procurement/design/${encodeURIComponent(m.code)}`);
        if (gRes && gRes.ok) { d.loaded = await gRes.json(); designCode = d.loaded.code; }
      }
    }
  }
  let designName = d.loaded ? (d.loaded.name || inm.name) : (typedNo ? `${inm.name} ${typedNo}` : inm.name);
  if (!designCode) {
    const fRes = await apiFetch('/api/v1/doc/ProductFamily', {
      method: 'POST',
      body: JSON.stringify({
        name: designName, status: 'Active', item_name: inm.code, design_number: typedNo || undefined,
        description: attrString ? `Design attributes: ${attrString}` : undefined,
        design_attributes: attrString || undefined, vendor: poDraft.vendor || undefined, hsn_code: hsn,
        gst_rate: gst, mrp: d.mrp === '' ? undefined : Number(d.mrp), purchase_rate: d.rate === '' ? undefined : Number(d.rate)
      })
    });
    if (!fRes) return;
    if (!fRes.ok) return fail(await getErrorMessage(fRes, 'Could not create the design.'));
    const created = await fRes.json();
    designCode = created.id;
    d.designs.push({ code: designCode, id: designCode, design_number: typedNo, name: designName });
    d.designs.sort((a, b) => poDesignLabel(a).localeCompare(poDesignLabel(b), undefined, { numeric: true }));
  }

  // 2. One Item per combination (an existing one is reused, not duplicated).
  const added = [];
  const failures = [];
  for (const c of kept) {
    const row = c.row;
    const qty = Number(row.qty ?? d.qty) || 0;
    const rate = Number(row.rate ?? d.rate) || 0;
    const mrpRaw = row.mrp ?? d.mrp;
    const existing = poDesignExistingFor(d, c.values);
    let sku = existing ? existing.sku : '';
    if (!sku) {
      const valuesText = Object.keys(c.values).sort().map(k => c.values[k]).join(' ');
      const iRes = await apiFetch('/api/v1/doc/Item', {
        method: 'POST',
        body: JSON.stringify({
          name: valuesText ? `${designName} ${valuesText}` : designName,
          family: designCode, parent_product_code: designCode,
          variant_option_values: c.key || undefined,
          category: inm.category || undefined,
          hsn_code: hsn, gst_rate: gst,
          mrp: mrpRaw === '' || mrpRaw == null ? undefined : Number(mrpRaw),
          standard_cost: rate || undefined
        })
      });
      if (!iRes) return;
      if (!iRes.ok) { failures.push(`${c.key || designName}: ${await getErrorMessage(iRes, 'could not be created')}`); continue; }
      sku = (await iRes.json()).id;
    }
    if (qty > 0) added.push({ sku, qty: String(qty), rate: rate ? String(rate) : '', mrp: mrpRaw === '' || mrpRaw == null ? '' : String(mrpRaw) });
  }

  // 3. Onto the PO.
  poDraft.lines.push(...added);
  renderPOLines();
  schedulePOPreview();
  const label = typedNo || designCode;
  if (failures.length) {
    fail(`Design ${label}: ${added.length} combination(s) added; ${failures.length} could not be created - ${failures.join('; ')}`);
    return;
  }
  showToast(`Design ${label}: ${added.length} combination(s) added to the PO.`, { variant: 'success' });
  // A clean slate for the next item (user, 94.27): the item name, its
  // attributes and the row's qty / price / MRP are all cleared.
  leavePODesignMode();
  poDraft.entry = newPOEntry();
  const el = id => document.getElementById(id);
  ['po-entry-sku', 'po-entry-rate', 'po-entry-mrp'].forEach(id => { if (el(id)) el(id).value = ''; });
  if (el('po-entry-qty')) el('po-entry-qty').value = poDraft.entry.qty;
  if (el('po-entry-sku')) { el('po-entry-sku').title = ''; el('po-entry-sku').focus(); }
  syncPOEntryButton();
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
  renderPOAddresses();
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
    el.innerHTML = `<span>Pick a vendor and a billing address &mdash; the supply type is worked out from their states.</span>`;
    return;
  }

  if (!pos.derived) {
    el.className = 'po-supply-banner po-supply-warn';
    el.innerHTML = `
      <span><strong>Supply type could not be derived</strong> &mdash; ${escapeHTMLText(pos.reason || 'a state is missing')}.
      Tick it here, or add the GSTIN/state on the store or vendor.</span>
      <label class="po-override"><input type="checkbox" id="po-interstate" ${d.interstate ? 'checked' : ''}> Inter-state (IGST)</label>`;
  } else {
    const kind = pos.interstate ? 'Inter-state' : 'Intra-state';
    const tax = pos.interstate ? 'IGST' : 'CGST + SGST';
    el.className = `po-supply-banner ${d.interstate_override ? 'po-supply-warn' : 'po-supply-ok'}`;
    el.innerHTML = `
      <span><strong>${kind} (${tax})</strong> &mdash; vendor in ${escapeHTMLText(pos.vendor_state_label || '?')}, billing address in ${escapeHTMLText(pos.buyer_state_label || '?')}.</span>
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
  // Stage 68: the bar's summary - lines, quantity, taxable, the tax split and
  // the grand total in one place (the GST treatment field says inclusive or
  // exclusive, so it is not repeated here).
  const lines = poDraft.lines.length;
  const qty = poDraft.lines.reduce((s, l) => s + (Number(l.qty) || 0), 0);
  const count = lines ? `<span><b>${lines}</b> item${lines === 1 ? '' : 's'} · Qty <b>${qty}</b></span>` : '';
  const p = poPreview;
  if (!p || !p.breakdown || (!p.breakdown.taxable_amount && !p.grand_total)) {
    el.innerHTML = count || '<span>Add an item to see the totals and tax.</span>';
    return;
  }
  const b = p.breakdown;
  const part = (label, value) => `<span>${label} <b>${txnMoney(value)}</b></span>`;
  const nonTaxable = (b.exempt_amount || 0) + (b.nil_rated_amount || 0) + (b.zero_rated_amount || 0);
  el.innerHTML = `${count}${part('Taxable', b.taxable_amount)}${nonTaxable ? part('Exempt / nil', nonTaxable) : ''}
    ${b.interstate ? part('IGST', b.igst) : part('CGST', b.cgst) + part('SGST', b.sgst)}
    <span class="txn-total" title="Grand total">${txnMoney(p.grand_total)}</span>`;
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
    fail('Vendor, Billing address and Shipping address are all required.');
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
      if (!await showCustomConfirm('This PO is Approved. Saving your changes sends it back to Pending Approval for re-approval. Continue?', 'Edit Purchase Order')) return;
      // PURCHA-0085: the server requires a reason when an Approved PO's
      // items/total actually change. Collected unconditionally here (rather
      // than trying to mirror the server's own before/after diff) so a save
      // never fails on a missing field the composer had no way to fill in -
      // sending it on an edit the server doesn't require it for is harmless.
      const amendReason = await showCustomPrompt('Reason for this change:', '');
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
    fail(await getErrorMessage(res, d.id ? 'Failed to save your changes - someone else may have edited this record, refresh and try again.' : 'Failed to create purchase order.'));
    return;
  }
  if (d.id && d.wasApproved) {
    await showCustomAlert('Purchase order saved. It now needs re-approval.', 'Edit Purchase Order');
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
  // To History, where the saved PO's next steps are (Submit, Print, Send).
  showPOPage('history');
}

// amendPurchaseOrder (the History row's Edit) loads an existing PO back into
// the same composer.
//
// Stage 26.3.6 built this as a chain of four showCustomPrompt() dialogs,
// because the screen had no form worth reusing. It does now - and a prompt
// chain could never have edited the lines, which is the thing an amendment is
// usually about.
window.amendPurchaseOrder = async function(poId) {
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the purchase order for editing.');
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
    })) : [],
    entry: newPOEntry(),
    wasApproved: record.status === 'Approved',
    status: record.status,
    version: typeof record.version === 'number' ? record.version : null
  };
  poPreview = null;
  // Stage 65.8 (the user: "Edit of everything ... like a brand new po screen.
  // But PO number will be same"): the New page, every field editable, number
  // fixed. The composer runs its own preview once rendered.
  showPOPage('new');
};

// ---------------------------------------------------------------------------
// Printed PO and vendor dispatch (Stage 40.1; designed prints Stage 63)
//
// The print dialog (trade-docs.js): Purchase Order or Vendor PO, the
// best-matching designed template (or the standard layout), the store it is
// generated from, a preview, CSV / Excel / PDF / Send, and Print - straight to
// this computer's PO printer when QZ Tray runs (Stage 98.4; this replaced
// 40.10's QZ print of a fixed server layout, so a PO prints in its template).
// ---------------------------------------------------------------------------
window.printPurchaseOrder = async function(poId) {
  const m = await tradeDocsReady;
  await m.printTransactionDocument('PurchaseOrder', poId);
};

// sendPurchaseOrderToVendor (Stage 98.3) sends the Vendor PO as a PDF in its
// best-match template: the ERP emails it to the vendor (address filled in,
// editable), or the PDF is downloaded to send yourself. Either way the PO is
// recorded as sent and PurchaseOrderIssued fires, as before. Sending still
// never needs a vendor email (92.4).
window.sendPurchaseOrderToVendor = async function(poId) {
  const m = await tradeDocsReady;
  await m.openSendDocument('VendorPO', poId, { onDone: () => renderView('purchase-orders') });
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
// Stage 62: the POs this receipt takes lines from - several are allowed, all
// of one vendor - as { id, label, vendor }. Each line carries its own po_id,
// po_rate and gst_rate (ex-GST, from the PO's own pricing preview, which is
// the function the server re-prices with on save) and an editable
// invoice_rate that defaults to the PO rate.
let grnPOs = [];
// Stage 84.4: sku -> { po_id, po_rate, gst_rate } of the loaded POs, so a line
// removed and added back to record rejected / damaged units (USER_GUIDE §6
// step 9) keeps its PO and rates. A SKU on two POs is ambiguous: not carried.
let grnPOLineContext = {};
// Stage 89: the Item fields and attributes the org asks at the GRN (weights,
// purity - Configuration -> Item fields), as a column each on the lines. A
// line keeps them as item_values; posting puts them on its batch.
let grnAskedFields = [];

async function renderGRNWorkbenchView(container) {
  const res = await apiFetch('/api/v1/doc/GRN');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load goods receipts.', () => renderView('grn')); return; }
  const grns = await res.json();
  state.docData = grns;
  grnLineItems = [];
  grnLoadedASNId = '';
  grnPOs = [];
  grnPOLineContext = {};
  grnAskedFields = [];
  apiFetch('/api/v1/inventory/item-setup').then(r => (r && r.ok ? r.json() : null)).then(setup => {
    if (!setup) return;
    grnAskedFields = setup.fields.filter(r => r.grn && !r.hidden && !(r.kind === 'attribute' && r.linked_field));
    renderGRNLinesList();
  }).catch(() => {});

  // Stage 68: the frozen transaction layout (txn-screen.css). Stage 69: opens
  // on the list; New is its own page, switched in place - the receipt being
  // keyed in lives only in this page (grnLineItems), so it has to survive a
  // look at the list.
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.id = 'grn-screen';
  screen.dataset.landing = 'history';
  screen.innerHTML = `
    <div class="txn-bar">
      ${txnPageTitle({ screen: 'grn-screen', title: 'Goods Receipt', page: 'New goods receipt', count: grns.length })}
      ${txnContext()}
      ${txnNewButton({ screen: 'grn-screen', label: 'New Goods Receipt' })}
      <div id="grn-totals" class="txn-sum txn-main-only"></div>
      <div class="txn-actions txn-main-only">
        <div id="grn-form-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="grn-create-btn">Save receipt</button>
      </div>
    </div>

    <div class="txn-pane" data-pane="new">
    <div class="txn-head">
      ${autoNumberField('GRN Number', 'GRN', '100%')}
      <!-- Stage 94.2: the vendor first; their open POs then show to tick. -->
      <div class="form-group">
        <label class="form-label" for="grn-vendor">Vendor</label>
        <input type="text" id="grn-vendor" class="form-input" autocomplete="off" placeholder="Pick the vendor...">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-po">Purchase Order</label>
        <input type="text" id="grn-po" class="form-input" autocomplete="off" placeholder="Search PO...">
      </div>
      <div class="form-group txn-head-btn">
        <button class="btn btn-outline" id="grn-load-po-btn" type="button">Add PO &amp; Load Items</button>
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-location">Receiving Location</label>
        <input type="text" id="grn-location" class="form-input" autocomplete="off">
      </div>
      <!-- Stage 62: the vendor's invoice. Its amount is a control total - the
           lines below carry the invoice price; any difference is shown, never
           a reason to hold the receipt. -->
      <div class="form-group">
        <label class="form-label" for="grn-inv-no">Vendor Invoice No<span class="required">*</span></label>
        <input type="text" id="grn-inv-no" class="form-input" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-inv-date">Invoice Date<span class="required">*</span></label>
        <input type="date" id="grn-inv-date" class="form-input">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-inv-amount">Invoice Total (incl. GST)<span class="required">*</span></label>
        <input type="number" id="grn-inv-amount" class="form-input num" min="0" step="0.01">
      </div>
      <!-- Stage 26.5.1: ASN capture ahead of a GRN - an optional prefill
           source alongside the PO one, same "Load Items from X" pattern. -->
      <div class="form-group">
        <label class="form-label" for="grn-asn">ASN Reference (optional)</label>
        <input type="text" id="grn-asn" class="form-input" autocomplete="off">
      </div>
      <div class="form-group txn-head-btn">
        <button class="btn btn-outline" id="grn-load-asn-btn" type="button">Load Items from ASN</button>
      </div>
    </div>
    <div id="grn-open-pos" class="grn-open-pos"></div>
    <div id="grn-po-chips" class="grn-po-chips"></div>
    <div id="grn-po-note" class="txn-hint"></div>
    <div id="grn-asn-note" class="txn-hint"></div>
    <div id="grn-notes-preview" class="txn-hint grn-notes-preview"></div>

    <div class="txn-entry">
      <div class="form-group txn-entry-main">
        <label class="form-label" for="grn-line-sku">SKU</label>
        <input type="text" id="grn-line-sku" class="form-input" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-line-ordered">Ordered Qty</label>
        <input type="number" id="grn-line-ordered" class="form-input" min="0">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-line-received">Received Qty</label>
        <input type="number" id="grn-line-received" class="form-input" min="0">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-line-rejected">Rejected Qty</label>
        <input type="number" id="grn-line-rejected" class="form-input" min="0" value="0">
      </div>
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-reject-reason">Rejection Reason</label>
        <input type="text" id="grn-line-reject-reason" class="form-input" placeholder="required if rejected > 0">
      </div>
      <!-- Stage 26.5.2: QC sampling's third bucket - damaged is tracked
           separately from rejected, each with its own required reason, and
           each posts to a different inventory_availability bucket. -->
      <div class="form-group">
        <label class="form-label" for="grn-line-damaged">Damaged Qty</label>
        <input type="number" id="grn-line-damaged" class="form-input" min="0" value="0">
      </div>
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-damage-reason">Damage Reason</label>
        <input type="text" id="grn-line-damage-reason" class="form-input" placeholder="required if damaged > 0">
      </div>
      <button class="btn btn-outline" id="grn-add-line-btn" type="button">Add Line</button>
    </div>

    <!-- Stage 42.1.4: batch/lot capture at receipt, its own row; hidden for
         an item that is not batch-tracked (populateGRNBatchRow below). -->
    <div id="grn-batch-row" class="txn-entry hidden">
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-batch">Batch / Lot No</label>
        <input type="text" id="grn-line-batch" class="form-input" autocomplete="off">
      </div>
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-mfg">Manufacture Date</label>
        <input type="date" id="grn-line-mfg" class="form-input">
      </div>
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-expiry">Expiry Date</label>
        <input type="date" id="grn-line-expiry" class="form-input">
      </div>
      <div class="form-group txn-entry-wide">
        <label class="form-label" for="grn-line-supplier-batch">Supplier Batch No</label>
        <input type="text" id="grn-line-supplier-batch" class="form-input" autocomplete="off">
      </div>
      <div id="grn-batch-note" class="txn-hint"></div>
    </div>

    <!-- Stage 42.1.8: serial capture at receipt - one serial number per
         accepted unit; hidden unless the SKU is serial-tracked. -->
    <div id="grn-serial-row" class="txn-entry hidden">
      <div class="form-group txn-entry-main">
        <label class="form-label" for="grn-line-serials">Serial Numbers (one per unit)</label>
        <textarea id="grn-line-serials" class="form-input" rows="2" style="font-family: Consolas, Monaco, monospace;" placeholder="one per line, or comma-separated"></textarea>
      </div>
      <div id="grn-serial-note" class="txn-hint"></div>
    </div>
    <!-- Stage 42.3.7: catch weight (required for an is_catch_weight item) and
         optional dimensions, offered for every line. -->
    <div id="grn-weight-row" class="txn-entry">
      <div class="form-group">
        <label class="form-label" for="grn-line-weight">Actual Weight</label>
        <input type="number" id="grn-line-weight" class="form-input" min="0" step="0.01">
      </div>
      <div class="form-group">
        <label class="form-label" for="grn-line-weight-uom">Weight UOM</label>
        <input type="text" id="grn-line-weight-uom" class="form-input" placeholder="kg" autocomplete="off">
      </div>
      <div class="form-group txn-entry-narrow">
        <label class="form-label" for="grn-line-length">L</label>
        <input type="number" id="grn-line-length" class="form-input" min="0" step="0.01">
      </div>
      <div class="form-group txn-entry-narrow">
        <label class="form-label" for="grn-line-width">W</label>
        <input type="number" id="grn-line-width" class="form-input" min="0" step="0.01">
      </div>
      <div class="form-group txn-entry-narrow">
        <label class="form-label" for="grn-line-height">H</label>
        <input type="number" id="grn-line-height" class="form-input" min="0" step="0.01">
      </div>
      <div class="form-group txn-entry-narrow">
        <label class="form-label" for="grn-line-dim-uom">Dim UOM</label>
        <input type="text" id="grn-line-dim-uom" class="form-input" placeholder="cm" autocomplete="off">
      </div>
      <div id="grn-weight-note" class="txn-hint"></div>
    </div>
    <div class="txn-grid"><div id="grn-lines-list"></div></div>
    </div>

    <div class="txn-pane hidden" data-pane="history">
      <div class="txn-grid">
        <div id="grn-inbox"></div>
        <h2 class="grn-all-title">All goods receipts</h2>
        <div id="grn-history"></div>
      </div>
    </div>

    <!-- Stage 94: one receipt, worked on step by step (grn-steps.js). -->
    <div class="txn-pane hidden" data-pane="steps">
      <div id="grn-steps-root" class="txn-grid"></div>
    </div>
  `;
  container.appendChild(screen);
  txnOnPage('grn-screen', showGRNScreenPage);

  attachLinkTypeahead(document.getElementById('grn-vendor'), 'Vendor');
  document.getElementById('grn-vendor').addEventListener('change', loadGRNOpenPOs);
  attachLinkTypeahead(document.getElementById('grn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('grn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('grn-asn'), 'ASN');
  attachLinkTypeahead(document.getElementById('grn-line-sku'), 'Item');

  // Stage 42.1.4: the batch row follows whatever SKU is in the line field, so
  // the clerk is asked for a lot number exactly when the item needs one and
  // never otherwise. `change` (not `input`) so the typeahead's resolved value
  // is what gets looked up, matching how grn-po already drives its own reload.
  document.getElementById('grn-line-sku').addEventListener('change', populateGRNBatchRow);
  // Stage 94.2: a scan (or Enter) in the SKU box with no qty typed counts one
  // unit of a line already on the receipt (addGRNLine).
  document.getElementById('grn-line-sku').addEventListener('keydown', e => {
    if (e.key === 'Enter' && document.getElementById('grn-line-received').value === '') { e.preventDefault(); addGRNLine(); }
  });

  document.getElementById('grn-po').addEventListener('change', loadGRNItemsFromPO);
  document.getElementById('grn-load-po-btn').addEventListener('click', loadGRNItemsFromPO);
  document.getElementById('grn-load-asn-btn').addEventListener('click', loadGRNItemsFromASN);
  document.getElementById('grn-add-line-btn').addEventListener('click', addGRNLine);
  document.getElementById('grn-create-btn').addEventListener('click', createGRN);
  document.getElementById('grn-inv-amount').addEventListener('input', renderGRNTotals);

  // Stage 42.1.4: drop the per-SKU tracking cache each time the screen is
  // opened, so an item switched to batch-tracked in Setup is picked up on the
  // clerk's next visit rather than surviving until a page reload.
  grnBatchTracked = {};
  grnBatchShelfLife = {};
  grnSerialTracked = {};
  grnCatchWeight = {};

  renderGRNLinesList();

  let html = `
    <table>
      <thead>
        <tr><th>GRN #</th><th>PO</th><th>Location</th><th>Lines</th><th>Received / Rejected / Damaged</th><th>Vendor Invoice</th><th>Price Notes</th><th>Status</th></tr>
      </thead>
      <tbody>
  `;
  if (grns.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No goods receipts yet. Click <b>+ New Goods Receipt</b>, add an approved Purchase Order with <b>Add PO &amp; Load Items</b>, then <b>Post Receipt</b>.</td></tr>`;
  }
  // Newest first (the list API returns creation order), so a receipt just
  // posted - and the notes it raised - is the row at the top.
  grns.slice().reverse().forEach(g => {
    let lines = [];
    try { lines = JSON.parse(g.received_items || '[]'); } catch (e) { lines = []; }
    const receivedTotal = lines.reduce((s, l) => s + (Number(l.qty) || 0), 0);
    const rejectedTotal = lines.reduce((s, l) => s + (Number(l.rejected_qty) || 0), 0);
    const damagedTotal = lines.reduce((s, l) => s + (Number(l.damaged_qty) || 0), 0);
    const statusBadge = g.status === 'Approved' ? 'badge-success' : g.status === 'Cancelled' ? 'badge-danger' : 'badge-warning';
    html += `
      <tr>
        <td style="font-family: monospace;">${g.code || g.id}</td>
        <td>${(g.po_ids ? String(g.po_ids).split(',') : [g.po_id]).filter(Boolean).map(p => documentSearchLink('PurchaseOrder', p)).join('<br>')}</td>
        <td>${g.location || ''}</td>
        <td>${lines.length}</td>
        <td>${receivedTotal} / ${rejectedTotal} / ${damagedTotal}</td>
        <td>${g.vendor_invoice_no ? escapeHTMLText(g.vendor_invoice_no) : '<span class="text-muted">&mdash;</span>'}${(g.flow === 'steps' ? g.vendor_invoice_amount : g.invoice_total) ? `<div class="text-muted" style="font-size:12px;">${formatMoney(g.flow === 'steps' ? g.vendor_invoice_amount : g.invoice_total)}</div>` : ''}</td>
        <td>${grnNotesCell(g)}</td>
        <td><span class="badge ${statusBadge}">${escapeHTMLText(g.flow === 'steps' && g.grn_step !== 'Posted' ? (g.grn_step || g.status || '') : (g.status || ''))}</span><div>${g.flow === 'steps'
          ? `<button class="action-btn" type="button" data-grn-steps-open="${escapeHTMLText(g.id)}">Open</button>`
          : `<button class="action-btn" ${actionAttrs('startVendorInvoiceFromGRN', [g.id, g.po_id])}>Record vendor invoice</button>`} <button class="action-btn" ${actionAttrs('printTransactionDocument', ['GRN', g.id])}>Print</button></div></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  const historyEl = document.getElementById('grn-history');
  historyEl.innerHTML = html;
  historyEl.addEventListener('click', e => {
    const b = e.target.closest('[data-grn-steps-open]');
    if (b) openGRNStepsPage(b.dataset.grnStepsOpen);
  });
  loadGRNInbox();
  txnRestorePane('grn-screen');

  let sourcePO = '';
  try {
    sourcePO = sessionStorage.getItem('stage55-grn-source-po') || '';
    sessionStorage.removeItem('stage55-grn-source-po');
  } catch (_) { /* storage can be disabled */ }
  if (sourcePO) {
    window.txnShowPane('grn-screen:new'); // "Receive goods" on a PO opens the New page
    document.getElementById('grn-po').value = sourcePO;
    await loadGRNItemsFromPO();
  }
}

// --- Stage 94: goods receipt in steps ---------------------------------------
// The screen has three pages: the list (with the inbox tabs), New goods
// receipt (step 1, Receive) and one receipt's steps. showGRNScreenPage is the
// screen's page switch (txnOnPage): it reopens the receipt a refresh left
// open, and redraws a list a step has changed.
let grnBlindCount = false;
let grnListStale = false;

function showGRNScreenPage(pane) {
  const screen = document.getElementById('grn-screen');
  if (!screen) return;
  const panes = [...screen.querySelectorAll(':scope > [data-pane]')];
  panes.forEach(p => p.classList.toggle('hidden', p.dataset.pane !== pane));
  screen.classList.toggle('txn-aside', !!panes[0] && panes[0].dataset.pane !== pane);
  screen.classList.toggle('txn-on-landing', pane === screen.dataset.landing);
  const crumb = screen.querySelector('.txn-page-only .txn-title');
  if (crumb && pane === 'new') crumb.textContent = 'New goods receipt';
  if (pane === 'history' && grnListStale) {
    grnListStale = false;
    renderView('grn');
    return;
  }
  if (pane === 'steps') {
    const root = document.getElementById('grn-steps-root');
    let id = '';
    try { id = sessionStorage.getItem('grn-steps-open') || ''; } catch (_) { /* storage can be disabled */ }
    if (root && !root.dataset.grnId && id) openGRNStepsPage(id, true);
  }
}

async function openGRNStepsPage(grnId, alreadyShown) {
  try { sessionStorage.setItem('grn-steps-open', grnId); } catch (_) { /* storage can be disabled */ }
  if (!alreadyShown) window.txnShowPane('grn-screen:steps');
  const root = document.getElementById('grn-steps-root');
  if (!root) return;
  root.dataset.grnId = grnId;
  const m = await grnStepsModule();
  await m.openGRNSteps(grnId, root, { onChanged: () => { grnListStale = true; } });
}

// loadGRNInbox draws the inbox tabs over the list: To verify, To price,
// Ready to pay, Done - each with its count and its oldest receipt's age.
async function loadGRNInbox() {
  const el = document.getElementById('grn-inbox');
  if (!el) return;
  const res = await apiFetch('/api/v1/grn-steps');
  if (!res || !res.ok) return;
  const data = await res.json();
  grnBlindCount = !!data.blind_count;
  renderGRNLinesList();
  if (!(data.rows || []).length) return;
  const m = await grnStepsModule();
  m.renderGRNInbox(el, data.rows, id => openGRNStepsPage(id));
}

// loadGRNOpenPOs (94.2) lists the picked vendor's open POs to tick; Load adds
// each ticked PO's open lines, exactly as adding it by number does.
async function loadGRNOpenPOs() {
  const el = document.getElementById('grn-open-pos');
  const vendor = document.getElementById('grn-vendor').value.trim();
  if (!el) return;
  if (!vendor) { el.innerHTML = ''; return; }
  const res = await apiFetch(`/api/v1/grn-steps/open-pos?vendor=${encodeURIComponent(vendor)}`);
  if (!res) return;
  if (!res.ok) { el.innerHTML = `<span class="login-error">${escapeHTMLText(await getErrorMessage(res, "Could not load the vendor's open POs."))}</span>`; return; }
  const pos = (await res.json()) || [];
  const onReceipt = new Set(grnPOs.map(p => p.id));
  const open = pos.filter(p => !onReceipt.has(p.id));
  if (!open.length) {
    el.innerHTML = `<span class="text-muted">${pos.length ? 'Every open PO of this vendor is on the receipt.' : 'This vendor has no approved PO with anything left to receive.'}</span>`;
    return;
  }
  el.innerHTML = `<b>Open POs of ${escapeHTMLText(vendor)}</b>
    ${open.map(p => `<label><input type="checkbox" data-open-po="${escapeHTMLText(p.id)}" data-open-po-label="${escapeHTMLText(p.code)}">
      ${escapeHTMLText(p.code)} &middot; ${escapeHTMLText(p.date)} &middot; ${p.lines} item${p.lines === 1 ? '' : 's'} &middot; ${p.open_qty} open &middot; ${formatMoney(p.open_value)}</label>`).join('')}
    <div><button type="button" class="btn btn-outline btn-sm" id="grn-load-ticked">Load ticked POs</button></div>`;
  document.getElementById('grn-load-ticked').addEventListener('click', async () => {
    const ticked = [...el.querySelectorAll('[data-open-po]:checked')];
    if (!ticked.length) return;
    for (const cb of ticked) {
      showGRNPOInBox(cb.dataset.openPo, cb.dataset.openPoLabel);
      await loadGRNItemsFromPO();
    }
    loadGRNOpenPOs();
  });
}

window.startVendorInvoiceFromGRN = function(grnId, poId) {
  try { sessionStorage.setItem('stage55-invoice-source-grn', JSON.stringify({ grnId, poId })); } catch (_) { /* storage can be disabled */ }
  renderView('vendor-invoices');
};

function renderGRNLinesList() {
  const el = document.getElementById('grn-lines-list');
  if (!el) return;
  if (grnLineItems.length === 0) {
    el.innerHTML = `<p class="txn-empty">No lines added yet &mdash; add a PO above, or a line by SKU.</p>`;
    renderGRNTotals();
    return;
  }
  // Stage 42.1.4/42.1.8: the Batch/Expiry and Serials columns each appear
  // only once a line on this receipt actually carries one.
  const grnHasBatches = grnLineItems.some(l => l.batch_no);
  const grnHasSerials = grnLineItems.some(l => l.serial_numbers && l.serial_numbers.length);
  // Stage 62: the PO column appears once a second PO is on the receipt.
  const grnMultiPO = new Set(grnLineItems.map(l => l.po_id).filter(Boolean)).size > 1;
  const askedCell = (line, idx, f) => {
    const v = String((line.item_values || {})[f.key] ?? '');
    const attrs = `data-grn-ask="${idx}" data-key="${escapeHTMLText(f.key)}" aria-label="${escapeHTMLText(f.label)} for ${escapeHTMLText(line.sku)}"`;
    if (f.options && f.options.length) {
      const opts = v && !f.options.includes(v) ? [...f.options, v] : f.options;
      return `<select class="form-select" ${attrs}><option value="">—</option>${opts.map(o => `<option value="${escapeHTMLText(o)}"${o === v ? ' selected' : ''}>${escapeHTMLText(o)}</option>`).join('')}</select>`;
    }
    return `<input class="form-input${f.type === 'Number' ? ' num' : ''}" type="${f.type === 'Number' ? 'number' : 'text'}" step="any" ${attrs} value="${escapeHTMLText(v)}">`;
  };
  // Stage 94.14: rejected / damaged are set on the row; a reason box opens
  // under the qty once it is above 0. A serial-tracked line keeps them
  // fixed - its serial list is tied to the accepted count (remove and re-add).
  const setAsideCell = (line, idx, kind) => {
    const q = Number(line[`${kind}_qty`]) || 0;
    if (line.serial_numbers && line.serial_numbers.length) return q > 0 ? `<span class="badge badge-danger">${q}</span>` : '0';
    const reasonKey = kind === 'rejected' ? 'rejection_reason' : 'damage_reason';
    const reason = String(line[reasonKey] || '');
    return `<input type="number" class="form-input num grn-qty-input" min="0" step="any" data-grn-setaside="${idx}" data-kind="${kind}" value="${q}" aria-label="${kind === 'rejected' ? 'Rejected' : 'Damaged'} qty for ${escapeHTMLText(line.sku)}">`
      + (q > 0 ? `<input type="text" class="form-input grn-reason-input${reason.trim() ? '' : ' grn-reason-missing'}" data-grn-reason="${idx}" data-kind="${kind}" placeholder="Reason" value="${escapeHTMLText(reason)}" aria-label="${kind === 'rejected' ? 'Rejection' : 'Damage'} reason for ${escapeHTMLText(line.sku)}">` : '');
  };
  el.innerHTML = `
    <table class="grn-lines-table">
      <thead><tr>${grnMultiPO ? '<th>PO</th>' : ''}<th>SKU</th>${grnHasBatches ? '<th>Batch / Expiry</th>' : ''}${grnHasSerials ? '<th>Serials</th>' : ''}${grnAskedFields.map(f => `<th>${escapeHTMLText(f.label)}${f.grn === 'required' ? '<span class="required">*</span>' : ''}</th>`).join('')}${grnBlindCount ? '' : '<th class="num">Ordered</th>'}<th class="num">Received</th><th class="num" title="Received minus rejected and damaged - goes into stock">Accepted</th><th class="num">Rejected</th><th class="num">Damaged</th>${grnBlindCount ? '' : '<th class="num" title="Ordered minus received - still owed on the PO">Short</th><th class="num" title="Received over the PO - keep or return is decided at Verify">Excess</th>'}<th class="num" title="Quantity the vendor's invoice bills">Invoice Qty</th><th class="num" title="Agreed price per unit before GST, from the PO">PO Rate</th><th class="num" title="Price per unit before GST on the vendor's invoice">Invoice Rate</th><th class="num" title="Invoice rate x invoice qty, before GST">Value</th><th></th></tr></thead>
      <tbody>
        ${grnLineItems.map((line, idx) => {
          const ordered = line.ordered_qty;
          const hasOrdered = ordered !== null && ordered !== undefined && ordered !== '';
          const short = hasOrdered ? Math.max(0, ordered - line.qty) : null;
          const excess = hasOrdered ? Math.max(0, line.qty - ordered) : null;
          const hasPORate = line.po_rate !== undefined && line.po_rate !== null && line.po_rate !== '';
          const invRate = (line.invoice_rate === '' || line.invoice_rate === undefined || line.invoice_rate === null) ? (hasPORate ? line.po_rate : '') : line.invoice_rate;
          const diff = hasPORate && invRate !== '' ? Number(invRate) - Number(line.po_rate) : 0;
          // Stage 94.14: the row's value is what the invoice bills, so the
          // rows add up to the bar's Invoice value.
          const billed = grnBilledQty(line);
          const value = invRate === '' ? null : Number(invRate) * billed;
          const billGap = billed - (Number(line.accepted_qty) || 0);
          return `
            <tr${Math.abs(diff) >= 0.005 ? ' class="grn-rate-diff"' : ''}>
              ${grnMultiPO ? `<td style="font-family: monospace; font-size: 12px;">${escapeHTMLText(line.po_id || '')}</td>` : ''}
              <td style="font-family: monospace;">${line.sku}${line.barcode && line.barcode !== line.sku ? `<div><span class="badge badge-secondary" style="font-family: Consolas, Monaco, monospace; letter-spacing: 1px;">${line.barcode}</span></div>` : ''}</td>
              ${grnHasBatches ? `<td>${batchCellHTML({ batch_no: line.batch_no, expiry_date: line.expiry_date })}</td>` : ''}
              ${grnHasSerials ? `<td>${line.serial_numbers && line.serial_numbers.length ? `<span class="badge badge-secondary" title="${line.serial_numbers.map(escapeHTMLText).join(', ')}">${line.serial_numbers.length} unit(s)</span>` : ''}</td>` : ''}
              ${grnAskedFields.map(f => `<td>${askedCell(line, idx, f)}</td>`).join('')}
              ${grnBlindCount ? '' : `<td class="num">${hasOrdered ? ordered : '&mdash;'}</td>`}
              <td class="num"><input type="number" class="form-input num grn-qty-input" min="0" step="any" data-grn-recv="${idx}" value="${escapeHTMLText(String(line.qty))}" aria-label="Received qty for ${escapeHTMLText(line.sku)}"></td>
              <td class="num grn-calc">${line.accepted_qty}</td>
              <td class="num">${setAsideCell(line, idx, 'rejected')}</td>
              <td class="num">${setAsideCell(line, idx, 'damaged')}</td>
              ${grnBlindCount ? '' : `<td class="num grn-calc">${short === null ? '&mdash;' : short > 0 ? `<span class="badge badge-warning">${short}</span>` : '0'}</td>
              <td class="num grn-calc">${excess === null ? '&mdash;' : excess > 0 ? `<span class="badge badge-warning">${excess}</span>` : '0'}</td>`}
              <td class="num"><input type="number" class="form-input num grn-qty-input" min="0" step="any" data-grn-invqty="${idx}" value="${escapeHTMLText(String(line.invoice_qty ?? line.qty))}" aria-label="Invoice qty for ${escapeHTMLText(line.sku)}">${Math.abs(billGap) > 0.0001 ? `<div class="grn-rate-delta up">${billGap > 0 ? '+' : '&minus;'}${Math.round(Math.abs(billGap) * 1000) / 1000} vs accepted</div>` : ''}</td>
              <td class="num">${hasPORate ? formatMoney(line.po_rate) : '<span class="text-muted">&mdash;</span>'}</td>
              <td class="num"><input type="number" class="form-input num grn-rate-input" min="0" step="0.01" data-grn-rate="${idx}" value="${escapeHTMLText(String(invRate))}" aria-label="Invoice rate for ${escapeHTMLText(line.sku)}">${Math.abs(diff) >= 0.005 ? `<div class="grn-rate-delta ${diff > 0 ? 'up' : 'down'}">${diff > 0 ? '+' : '&minus;'}${formatMoney(Math.abs(diff))} vs PO</div>` : ''}</td>
              <td class="num">${value === null ? '<span class="text-muted">&mdash;</span>' : formatMoney(value)}</td>
              <td><button class="action-btn action-btn-danger" type="button" ${actionAttrs('removeGRNLine', [idx])}>Remove</button></td>
            </tr>
          `;
        }).join('')}
      </tbody>
    </table>
  `;
  el.querySelectorAll('[data-grn-ask]').forEach(input => {
    input.addEventListener('change', () => {
      const line = grnLineItems[Number(input.dataset.grnAsk)];
      const v = String(input.value || '').trim();
      line.item_values = { ...(line.item_values || {}) };
      if (v) line.item_values[input.dataset.key] = v; else delete line.item_values[input.dataset.key];
    });
  });
  // Stage 94.2: what actually arrived, keyed against the PO qty. The invoice
  // qty follows it unless it was typed on its own.
  el.querySelectorAll('[data-grn-recv]').forEach(input => {
    input.addEventListener('change', () => {
      const i = Number(input.getAttribute('data-grn-recv'));
      const line = grnLineItems[i];
      const v = input.value.trim();
      const n = Number(v);
      const setAside = (Number(line.rejected_qty) || 0) + (Number(line.damaged_qty) || 0);
      if (v === '' || isNaN(n) || n < setAside) { input.value = line.qty; return; }
      if (line.invoice_qty === undefined || Number(line.invoice_qty) === Number(line.qty)) line.invoice_qty = n;
      line.qty = n;
      line.accepted_qty = n - setAside;
      renderGRNLinesList();
    });
  });
  // Stage 94.2: the vendor's invoice qty, when it differs from what arrived.
  el.querySelectorAll('[data-grn-invqty]').forEach(input => {
    input.addEventListener('change', () => {
      const i = Number(input.getAttribute('data-grn-invqty'));
      const v = input.value.trim();
      if (v === '' || isNaN(Number(v)) || Number(v) < 0) { input.value = grnLineItems[i].invoice_qty ?? grnLineItems[i].qty; return; }
      grnLineItems[i].invoice_qty = Number(v);
      renderGRNLinesList();
    });
  });
  // Stage 94.14: rejected / damaged on the row. Together they can't exceed
  // what arrived; accepted is what is left. Back to 0 clears the reason.
  el.querySelectorAll('[data-grn-setaside]').forEach(input => {
    input.addEventListener('change', () => {
      const i = Number(input.dataset.grnSetaside);
      const kind = input.dataset.kind;
      const line = grnLineItems[i];
      const other = kind === 'rejected' ? (Number(line.damaged_qty) || 0) : (Number(line.rejected_qty) || 0);
      const v = input.value.trim();
      const n = v === '' ? 0 : Number(v);
      if (isNaN(n) || n < 0 || n + other > Number(line.qty)) { input.value = Number(line[`${kind}_qty`]) || 0; return; }
      line[`${kind}_qty`] = n;
      if (n === 0) line[kind === 'rejected' ? 'rejection_reason' : 'damage_reason'] = '';
      line.accepted_qty = Number(line.qty) - n - other;
      renderGRNLinesList();
      if (n > 0) document.querySelector(`[data-grn-reason="${i}"][data-kind="${kind}"]`)?.focus();
    });
  });
  el.querySelectorAll('[data-grn-reason]').forEach(input => {
    input.addEventListener('input', () => {
      const line = grnLineItems[Number(input.dataset.grnReason)];
      line[input.dataset.kind === 'rejected' ? 'rejection_reason' : 'damage_reason'] = input.value.trim();
      input.classList.toggle('grn-reason-missing', !input.value.trim());
    });
  });
  el.querySelectorAll('[data-grn-rate]').forEach(input => {
    input.addEventListener('change', () => {
      const i = Number(input.getAttribute('data-grn-rate'));
      const v = input.value.trim();
      if (v !== '' && (isNaN(Number(v)) || Number(v) < 0)) { input.value = grnLineItems[i].invoice_rate ?? ''; return; }
      grnLineItems[i].invoice_rate = v === '' ? '' : Math.round(Number(v) * 100) / 100;
      renderGRNLinesList();
    });
  });
  renderGRNTotals();
}

// grnNotesCell lists the debit / credit notes a posted GRN raised for its
// price differences (Stage 62), each printable.
function grnNotesCell(g) {
  const parts = [];
  if (g.debit_note_id) parts.push(`<div>Debit <button class="link-btn" type="button" ${actionAttrs('printTransactionDocument', ['DebitNote', g.debit_note_id])}>${escapeHTMLText(g.debit_note_id)}</button></div>`);
  if (g.vendor_credit_note_id) parts.push(`<div>Credit <button class="link-btn" type="button" ${actionAttrs('printTransactionDocument', ['VendorCreditNote', g.vendor_credit_note_id])}>${escapeHTMLText(g.vendor_credit_note_id)}</button></div>`);
  return parts.length ? parts.join('') : '<span class="text-muted">&mdash;</span>';
}

// renderGRNTotals is the "section to see" the difference: value at PO price,
// value at invoice price with GST, the vendor's typed invoice amount, and the
// notes the receipt will raise. Accepted qty only - rejected and damaged units
// are not owed (they go back on a Purchase Return). GST here is an estimate
// at each line's PO GST rate; the server prices the saved receipt itself.
function renderGRNTotals() {
  const el = document.getElementById('grn-totals');
  const notesEl = document.getElementById('grn-notes-preview');
  if (!el) return;
  // Stage 68: the bar's summary - lines and accepted units always, the money
  // once a line is priced; the note preview is the one-line notice below.
  const accepted = grnLineItems.reduce((s, l) => s + (Number(l.accepted_qty) || 0), 0);
  const count = grnLineItems.length ? `<span><b>${grnLineItems.length}</b> line${grnLineItems.length === 1 ? '' : 's'} · Accepted <b>${accepted}</b></span>` : '';
  const priced = grnLineItems.filter(l => l.po_rate !== undefined && l.po_rate !== '' && l.po_rate !== null);
  if (grnLineItems.length === 0 || priced.length === 0) {
    el.innerHTML = count || '<span>Add a PO or a line to see the totals.</span>';
    if (notesEl) notesEl.innerHTML = '';
    return;
  }
  let poValue = 0, invValue = 0, invGST = 0, up = 0, down = 0;
  grnLineItems.forEach(l => {
    const acc = Number(l.accepted_qty) || 0;
    const hasPO = l.po_rate !== undefined && l.po_rate !== '' && l.po_rate !== null;
    const inv = (l.invoice_rate === '' || l.invoice_rate === undefined || l.invoice_rate === null) ? (hasPO ? Number(l.po_rate) : 0) : Number(l.invoice_rate);
    const gst = Number(l.gst_rate) || 0;
    // Stage 94: the invoice is what it bills - its own qty, not what arrived.
    const billed = grnBilledQty(l);
    if (hasPO) poValue += Number(l.po_rate) * acc;
    invValue += inv * billed;
    invGST += inv * billed * gst / 100;
    if (hasPO) {
      const d = (inv - Number(l.po_rate)) * acc * (1 + gst / 100);
      if (d > 0.004) up += d; else if (d < -0.004) down += -d;
    }
  });
  const invTotal = invValue + invGST;
  const typedRaw = document.getElementById('grn-inv-amount')?.value || '';
  const typed = typedRaw === '' ? null : Number(typedRaw);
  const gap = typed === null ? null : typed - invTotal;
  const gapBad = gap !== null && Math.abs(gap) >= 1;
  el.innerHTML = `${count}
    <span title="Value at PO price">PO value <b>${formatMoney(poValue)}</b></span>
    <span title="Value at invoice price">Invoice value <b>${formatMoney(invValue)}</b></span>
    <span>GST ≈ <b>${formatMoney(invGST)}</b></span>
    ${typed === null ? '' : `<span class="${gapBad ? 'grn-gap' : ''}" title="Vendor invoice amount minus these lines">Invoice − lines <b>${formatMoney(gap)}</b></span>`}
    <span class="txn-total" title="Invoice value incl. GST">${formatMoney(invTotal)}</span>`;
  // Stage 94.14: lines whose invoice bills other than the accepted qty, and
  // lines that brought more than the PO asked for.
  const billMismatch = grnLineItems.filter(l => Math.abs(grnBilledQty(l) - (Number(l.accepted_qty) || 0)) > 0.0001)
    .map(l => `${l.sku} (billed ${grnBilledQty(l)}, accepted ${Number(l.accepted_qty) || 0})`);
  const excessLines = grnBlindCount ? [] : grnLineItems.filter(l => l.ordered_qty !== null && l.ordered_qty !== undefined && l.ordered_qty !== '' && Number(l.qty) > Number(l.ordered_qty))
    .map(l => `${l.sku} (+${Number(l.qty) - Number(l.ordered_qty)})`);
  // Stage 94: nothing is settled here - Verify sets what was really received
  // and at what price, and the note is invoice total against that.
  if (notesEl) notesEl.innerHTML = `
      ${up > 0.004 || down > 0.004 ? `<span class="text-muted">Invoice prices differ from the PO on some lines. Verify (next step) settles what you pay; any difference to the invoice becomes a debit or credit note.</span>` : '<span class="text-muted">Invoice prices match the PO.</span>'}
      ${gapBad ? `<span class="grn-gap-note">The vendor's invoice total does not equal these lines. Check the invoice qty and rates - Verify will not go ahead while they differ by more than 1.00.</span>` : ''}
      ${billMismatch.length ? `<span class="grn-gap-note">Invoice qty is not the accepted qty on ${billMismatch.map(escapeHTMLText).join(', ')}. Check the vendor's invoice before saving.</span>` : ''}
      ${excessLines.length ? `<span class="text-muted">More than the PO arrived on ${excessLines.map(escapeHTMLText).join(', ')}. Keep or return is decided at Verify.</span>` : ''}`;
}

// grnBilledQty is the qty the vendor's invoice bills for a line - its own
// invoice qty once typed, otherwise what arrived.
function grnBilledQty(l) {
  return l.invoice_qty === undefined || l.invoice_qty === '' || l.invoice_qty === null ? (Number(l.qty) || 0) : Number(l.invoice_qty);
}

// renderGRNPOChips shows the POs on this receipt, each removable (with its lines).
function renderGRNPOChips() {
  const el = document.getElementById('grn-po-chips');
  if (!el) return;
  el.innerHTML = grnPOs.map((p, i) => `
    <span class="grn-po-chip">${escapeHTMLText(p.label)}
      <button type="button" class="grn-po-chip-x" ${actionAttrs('removeGRNPO', [i])} aria-label="Remove ${escapeHTMLText(p.label)}">&times;</button>
    </span>`).join('');
  const vendorEl = document.getElementById('grn-vendor');
  if (vendorEl) {
    // Stage 94.2: once a PO is on the receipt, its vendor is the receipt's.
    if (grnPOs.length && grnPOs[0].vendor) vendorEl.value = grnPOs[0].vendor;
    vendorEl.readOnly = grnPOs.length > 0;
  }
}

window.removeGRNPO = function(i) {
  const p = grnPOs[i];
  if (!p) return;
  grnPOs.splice(i, 1);
  grnLineItems = grnLineItems.filter(l => l.po_id !== p.id);
  Object.keys(grnPOLineContext).forEach(sku => { if (grnPOLineContext[sku].po_id === p.id) delete grnPOLineContext[sku]; });
  renderGRNPOChips();
  renderGRNLinesList();
};

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

// grnScanBeep: one short tone per counted unit (Stage 94.2), quiet when the
// browser has no audio.
let grnAudio = null;
function grnScanBeep() {
  try {
    grnAudio = grnAudio || new (window.AudioContext || window.webkitAudioContext)();
    const osc = grnAudio.createOscillator();
    const gain = grnAudio.createGain();
    osc.frequency.value = 1200;
    gain.gain.value = 0.05;
    osc.connect(gain).connect(grnAudio.destination);
    osc.start();
    osc.stop(grnAudio.currentTime + 0.06);
  } catch (_) { /* no audio - counting still works */ }
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

  // Stage 94.2: a scan of an item already on the receipt, with no qty typed,
  // counts one more unit of it - received, accepted and (unless the invoice
  // qty was typed separately) invoiced - with a short beep.
  if (sku && receivedEl.value === '') {
    const lower = sku.toLowerCase();
    const line = grnLineItems.find(l => String(l.sku).toLowerCase() === lower || String(l.barcode || '').toLowerCase() === lower);
    if (line) {
      const before = Number(line.qty) || 0;
      if (line.invoice_qty === undefined || Number(line.invoice_qty) === before) line.invoice_qty = before + 1;
      line.qty = before + 1;
      line.accepted_qty = (Number(line.accepted_qty) || 0) + 1;
      skuEl.value = '';
      grnScanBeep();
      renderGRNLinesList();
      skuEl.focus();
      return;
    }
  }
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
  const poLine = grnPOLineContext[sku];
  if (poLine && !poLine.ambiguous) {
    line.po_id = poLine.po_id;
    if (poLine.po_rate !== undefined) Object.assign(line, { po_rate: poLine.po_rate, gst_rate: poLine.gst_rate, invoice_rate: poLine.po_rate });
  }
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
  if (batchEl) batchEl.placeholder = tracked ? 'required for this item' : 'auto if blank';
  if (note) {
    const shelfLife = grnBatchShelfLife[sku] || 0;
    note.innerHTML = tracked
      ? `<b>${escapeHTMLText(sku)}</b> is batch-tracked &mdash; a lot number is required.${shelfLife > 0 ? ` If you enter a manufacture date and leave Expiry blank, expiry is derived as manufacture + ${shelfLife} days.` : ''}`
      : `<b>${escapeHTMLText(sku)}</b> is not batch-tracked. Leave Batch / Lot No blank and it gets a batch number with today's date (e.g. B-261015-0001) when the GRN is saved - unless batch numbers are switched off for it - or type your own.`;
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

// Stage 84.2: the PO box shows the PO's number, never its internal id (on
// older POs the id is not the number). The id it was loaded by is kept on
// the box, so loading and posting still use the id.
function showGRNPOInBox(id, label) {
  const el = document.getElementById('grn-po');
  el.value = label;
  el.dataset.poLabel = label;
  el.dataset.poId = id;
}

function grnPOBoxRef() {
  const el = document.getElementById('grn-po');
  const typed = el.value.trim();
  return typed && typed === el.dataset.poLabel ? (el.dataset.poId || typed) : typed;
}

// fetchGRNSourcePO reads a PO by id, else by its number (what the box's
// picker fills in). undefined = the request itself was cut off.
async function fetchGRNSourcePO(ref) {
  const byId = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(ref)}`);
  if (!byId) return undefined;
  if (byId.ok) return byId.json();
  const search = await apiFetch(`/api/v1/doc/PurchaseOrder?q=${encodeURIComponent(ref)}&limit=10`);
  if (!search || !search.ok) return null;
  const lower = ref.toLowerCase();
  const hit = ((await search.json()) || []).find(d => [d.po_number, d.code, d.id].some(v => String(v || '').toLowerCase() === lower));
  if (!hit || !hit.id) return null;
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(hit.id)}`);
  return res && res.ok ? res.json() : null;
}

async function loadGRNItemsFromPOInner() {
  const poId = grnPOBoxRef();
  const noteEl = document.getElementById('grn-po-note');
  if (!poId) { noteEl.textContent = ''; return; }

  const po = await fetchGRNSourcePO(poId);
  if (po === undefined) return;
  if (!po) {
    noteEl.textContent = 'Could not find that PO - enter lines manually below.';
    return;
  }
  const poKey = po.id || poId;
  const poLabel = po.po_number || po.code || poKey;
  const poVendor = po.vendor || po.vendor_id || '';
  showGRNPOInBox(poKey, poLabel);

  // Stage 62: several POs may go on one receipt, but only of one vendor
  // (GOODSR-0318 on the server says the same).
  if (grnPOs.some(p => p.id === poKey)) {
    noteEl.textContent = `PO ${poLabel} is already on this receipt.`;
    document.getElementById('grn-po').value = '';
    return;
  }
  if (grnPOs.length && poVendor && grnPOs[0].vendor && poVendor !== grnPOs[0].vendor) {
    noteEl.textContent = `PO ${poLabel} is for vendor ${poVendor}, but this receipt is for ${grnPOs[0].vendor}. Receive it on its own goods receipt.`;
    return;
  }

  const locationEl = document.getElementById('grn-location');
  if (!locationEl.value && (po.location || po.target_warehouse)) {
    locationEl.value = po.target_warehouse || po.location;
  }

  let items = [];
  try { items = JSON.parse(po.items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `PO ${poLabel} has no recorded item lines - add lines manually below.`;
    return;
  }

  // PO rate and GST rate per SKU, ex-GST, from the same preview the PO screen
  // shows - the server re-reads them on save, so this is display only.
  const rates = {};
  const prevRes = await apiFetch('/api/v1/procurement/purchase-order/preview', {
    method: 'POST',
    body: JSON.stringify({ vendor: poVendor, vendor_id: poVendor, location: po.location || '', target_warehouse: po.target_warehouse || '', items: po.items || '[]', gst_mode: po.gst_mode || '', interstate: !!po.interstate, interstate_override: !!po.interstate_override })
  });
  if (prevRes && prevRes.ok) {
    const prev = await prevRes.json();
    const acc = {};
    (prev.lines || []).forEach(l => {
      if (!l.sku || l.error || !(l.qty > 0)) return;
      const a = acc[l.sku] || (acc[l.sku] = { taxable: 0, qty: 0, gst: 0 });
      a.taxable += Number(l.taxable) || 0; a.qty += Number(l.qty) || 0; a.gst = Number(l.gst_rate) || 0;
    });
    Object.keys(acc).forEach(sku => { rates[sku] = { rate: Math.round(acc[sku].taxable / acc[sku].qty * 100) / 100, gst: acc[sku].gst }; });
  }

  // Stage 73.2.4 (D05): the open quantity comes from the server - ordered
  // less ACCEPTED (a rejected unit stays open for its replacement) less any
  // short-closed balance - so this screen and the receipt check agree.
  const openBySku = {};
  const recRes = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poKey)}/reconciliation`);
  if (recRes && recRes.ok) {
    const rec = await recRes.json();
    if (rec.status === 'Closed') {
      noteEl.textContent = `PO ${poLabel} is closed${rec.short_closed ? ' (short-closed)' : ''} - nothing more can be received against it.`;
      return;
    }
    (rec.lines || []).forEach(l => { openBySku[l.sku] = Number(l.open) || 0; });
  }

  let added = 0, closed = 0;
  const seenSku = {};
  for (const it of items) {
    const sku = it.sku || it.item_id || '';
    if (!sku || seenSku[sku]) continue;
    seenSku[sku] = true;
    const orderedQty = items.filter(x => (x.sku || x.item_id) === sku).reduce((n, x) => n + (Number(x.qty) || 0), 0);
    const open = sku in openBySku ? openBySku[sku] : orderedQty;
    if (open <= 0) { closed++; continue; }
    const barcode = await lookupGRNBarcode(sku);
    const r = rates[sku];
    const known = grnPOLineContext[sku];
    grnPOLineContext[sku] = known && known.po_id !== poKey ? { ambiguous: true }
      : { po_id: poKey, ...(r ? { po_rate: r.rate, gst_rate: r.gst } : {}) };
    grnLineItems.push({
      po_id: poKey, sku, ordered_qty: open, qty: open, accepted_qty: open, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode,
      ...(r ? { po_rate: r.rate, gst_rate: r.gst, invoice_rate: r.rate } : {})
    });
    added++;
  }
  if (added === 0) {
    noteEl.textContent = `PO ${poLabel} has nothing left to receive${closed ? ' - every line is fully received' : ''}.`;
    return;
  }
  grnPOs.push({ id: poKey, label: poLabel, vendor: poVendor });
  grnLoadedASNId = '';
  document.getElementById('grn-po').value = '';
  noteEl.textContent = `Loaded ${added} open line(s) from PO ${poLabel}${closed ? ` (${closed} already fully received)` : ''}. Add another PO of the same vendor, or adjust quantities and invoice rates, then Save receipt.`;
  renderGRNPOChips();
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
  if (!poEl.value && asn.po_id) {
    poEl.value = asn.po_id;
    const po = await fetchGRNSourcePO(asn.po_id);
    if (po) showGRNPOInBox(po.id || asn.po_id, po.po_number || po.code || asn.po_id);
  }

  let items = [];
  try { items = JSON.parse(asn.expected_items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `ASN ${asnId} has no recorded expected items - add lines manually below.`;
    return;
  }

  grnLineItems = [];
  grnPOs = [];
  grnPOLineContext = {};
  renderGRNPOChips();
  for (const it of items) {
    const sku = it.sku || '';
    const expectedQty = Number(it.qty) || 0;
    if (!sku) continue;
    const barcode = await lookupGRNBarcode(sku);
    grnLineItems.push({ sku, ordered_qty: expectedQty, qty: expectedQty, accepted_qty: expectedQty, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode });
  }
  grnLoadedASNId = asnId;
  noteEl.textContent = `Loaded ${grnLineItems.length} line(s) from ASN ${asnId}. Adjust Received/Rejected/Damaged qty for any variance, then Save receipt.`;
  renderGRNLinesList();
}

// BLD-036: same double-submit guard as the generic record form and PO
// composer - a slow post (or a repeat click on "Post Receipt" before the
// first response returns) used to be able to post two GRNs from one intent.
async function createGRN() {
  await guardAgainstDoubleSubmit(document.getElementById('grn-create-btn'), 'Saving...', createGRNInner);
}

async function createGRNInner() {
  const errorEl = document.getElementById('grn-form-error');
  errorEl.classList.add('hidden');

  // Stage 62: the POs added above; a PO typed but not added (or one an ASN
  // filled in) still counts as the receipt's PO.
  const typedPO = grnPOBoxRef();
  const poIds = grnPOs.length ? grnPOs.map(p => p.id) : (typedPO ? [typedPO] : []);
  const poId = poIds.join(', ');
  const location = document.getElementById('grn-location').value.trim();

  if (poIds.length === 0 || !location || grnLineItems.length === 0) {
    errorEl.textContent = 'A Purchase Order, the Receiving Location, and at least one line item are all required.';
    errorEl.classList.remove('hidden');
    return;
  }
  // Stage 94.2: the vendor's invoice is keyed in full - number, date and
  // total - and the receipt saves as step 1; nothing posts until step 4.
  const invNo = document.getElementById('grn-inv-no').value.trim();
  const invDate = document.getElementById('grn-inv-date').value;
  const invAmountRaw = document.getElementById('grn-inv-amount').value.trim();
  if (!invNo || !invDate || invAmountRaw === '' || isNaN(Number(invAmountRaw)) || Number(invAmountRaw) <= 0) {
    errorEl.textContent = "Enter the vendor invoice number, date and total (incl. GST) from the vendor's invoice.";
    errorEl.classList.remove('hidden');
    return;
  }
  // Stage 94.14: rejected / damaged set on the row need their reason too.
  const noReason = grnLineItems.find(l => (Number(l.rejected_qty) > 0 && !String(l.rejection_reason || '').trim()) || (Number(l.damaged_qty) > 0 && !String(l.damage_reason || '').trim()));
  if (noReason) {
    errorEl.textContent = `${noReason.sku}: enter the reason for the rejected or damaged qty.`;
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/GRN', {
    method: 'POST',
    body: JSON.stringify({
      flow: 'steps',
      po_id: poIds[0],
      po_ids: poIds.join(','),
      asn_id: grnLoadedASNId || undefined,
      location,
      vendor_invoice_no: invNo,
      vendor_invoice_date: invDate,
      vendor_invoice_amount: Number(invAmountRaw),
      received_items: JSON.stringify(grnLineItems.map(l => ({ ...l, po_id: l.po_id || poIds[0] })))
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to save the goods receipt.');
    errorEl.classList.remove('hidden');
    return;
  }
  let saved = {};
  try { saved = await res.json(); } catch (e) { saved = {}; }
  showToast(`Goods receipt saved - nothing is in stock yet. Verify it next.`, { variant: 'success' });
  grnLineItems = [];
  grnPOs = [];
  grnListStale = true;
  if (saved.id) {
    await openGRNStepsPage(saved.id, false);
    return;
  }
  txnDone('grn-screen');
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

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>ASN #</th><th>PO</th><th>Location</th><th>Vendor</th><th>Carrier</th><th>Expected Date</th><th>Lines</th><th>Status</th></tr></thead>
      <tbody>
  `;
  if (asns.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No ASNs yet. Click <b>+ New ASN</b> to record a shipment your vendor has despatched.</td></tr>`;
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
  // Stage 69: the ASN list is the page; + New ASN opens the form as a page of
  // its own (txn-screen.js, the standard ERP shape).
  container.appendChild(txnPagedSection({ id: 'asn-screen', title: 'Advance Shipment Notices (ASN)', page: 'New ASN',
    count: asns.length, newLabel: 'New ASN', form: formPanel, list: listPanel }));
  txnRestorePane('asn-screen');

  attachLinkTypeahead(document.getElementById('asn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('asn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('asn-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('asn-line-sku'), 'Item');
  document.getElementById('asn-add-line-btn').addEventListener('click', addASNLine);
  document.getElementById('asn-create-btn').addEventListener('click', createASN);
  renderASNLinesList();
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
  txnDone('asn-screen');
  renderView('asn');
}

// Purchase Return (Stage 57.15): return goods to the vendor against the GRN
// they arrived on. The return is an ordinary PurchaseReturn document (saved
// through the generic doc API, which checks it against the GRN); this screen
// adds what that form cannot: the GRN's returnable lines, and Post, which
// moves the stock out and raises the vendor's debit note.
let prtContext = null;

async function renderPurchaseReturnsView(container) {
  const [listRes, ctxRes, grnRes, reasonPicker] = await Promise.all([
    apiFetch('/api/v1/doc/PurchaseReturn'),
    apiFetch('/api/v1/procurement/purchase-returns/context'),
    apiFetch('/api/v1/doc/GRN'),
    import('./reason-picker.js' + TRADE_DOCS_QUERY).catch(() => null)
  ]);
  // Stage 74.1.1: the reason codes for a return to the vendor.
  const prtReasons = reasonPicker ? await reasonPicker.loadReasons('Purchase Return') : null;
  if (!listRes || !ctxRes || !grnRes) return;
  if (!listRes.ok || !ctxRes.ok) {
    renderErrorPanel(container, 'Failed to load purchase returns.', () => renderView('purchase-returns'));
    return;
  }
  const returns = await listRes.json();
  const approvalRequired = !!(await ctxRes.json()).approval_required;
  const grns = grnRes.ok ? (await grnRes.json()).filter(g => g.status !== 'Cancelled') : [];
  prtContext = null;

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
    // Stage 71: the Return to Vendor print (trade-docs.js).
    if (r.status !== 'Cancelled') {
      out.push(`<button class="action-btn" ${actionAttrs('printTransactionDocument', ['PurchaseReturn', r.id])}>Print</button>`);
    }
    return out.join(' ');
  };

  // Stage 68: the frozen transaction layout (txn-screen.css). Stage 69: opens
  // on the list; New is its own page, switched in place so a half-entered
  // return survives a look at the list.
  const screen = document.createElement('div');
  screen.className = 'txn-screen';
  screen.id = 'prt-screen';
  screen.dataset.landing = 'history';
  screen.innerHTML = `
    <div class="txn-bar">
      ${txnPageTitle({ screen: 'prt-screen', title: 'Purchase Return', page: 'New purchase return', count: returns.length })}
      ${txnContext()}
      ${txnNewButton({ screen: 'prt-screen', label: 'New Purchase Return' })}
      <div id="prt-sum" class="txn-sum txn-main-only"><span>Choose a GRN to see what can be returned.</span></div>
      <div class="txn-actions txn-main-only">
        <div id="prt-form-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="prt-save-btn" disabled title="${approvalRequired
          ? 'Returns need approval before they can be posted.'
          : 'Saved as a Draft; nothing moves until you Post it.'}">Save Return</button>
      </div>
    </div>

    <div class="txn-pane" data-pane="new">
      <div class="txn-head">
        <div class="form-group">
          <label class="form-label" for="prt-grn">Goods Receipt (GRN)<span class="required">*</span></label>
          <select id="prt-grn" class="form-input">
            <option value="">Choose the GRN the goods came in on</option>
            ${grns.map(g => `<option value="${escapeHTMLText(g.id)}">${escapeHTMLText(g.id)}${g.vendor ? ' - ' + escapeHTMLText(g.vendor) : ''}${g.po_id ? ' (PO ' + escapeHTMLText(g.po_id) + ')' : ''}</option>`).join('')}
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="prt-reason-code">Reason for Return<span class="required">*</span></label>
          ${reasonPicker ? reasonPicker.reasonSelectHTML(prtReasons, { attrs: 'id="prt-reason-code"', label: 'Reason for return' }) : ''}
          <input type="text" id="prt-reason" class="form-input" placeholder="${reasonPicker ? 'note (optional)' : 'e.g. wrong size supplied, QC rejected'}" autocomplete="off" aria-label="Reason for return, note">
        </div>
      </div>
      <div class="txn-hint"><span>Send goods back to the vendor against the GRN they arrived on. ${approvalRequired
        ? 'Returns need approval before they can be posted.'
        : 'Saved as a Draft; posting takes the stock out and raises the vendor\'s debit note.'}</span></div>
      <div class="txn-grid"><div id="prt-lines" class="txn-empty">Choose a GRN to see what can be returned.</div></div>
    </div>

    <div class="txn-pane hidden" data-pane="history">
      <div class="txn-grid">
        <table>
          <thead><tr><th>Return #</th><th>GRN</th><th>Vendor</th><th>Reason</th><th>Value (ex-GST)</th><th>Debit Note</th><th>Status</th><th>Actions</th></tr></thead>
          <tbody>
            ${returns.length === 0
              ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No purchase returns yet. Click <b>+ New Purchase Return</b> and choose a GRN to raise one.</td></tr>`
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
      </div>
    </div>
  `;
  container.appendChild(screen);

  document.getElementById('prt-grn').addEventListener('change', e => loadPurchaseReturnLines(e.target.value));
  document.getElementById('prt-save-btn').addEventListener('click', savePurchaseReturn);
  document.getElementById('prt-lines').addEventListener('input', renderPurchaseReturnSum);
  txnRestorePane('prt-screen');
}

// renderPurchaseReturnSum is the bar's summary for the return being keyed in:
// lines and units going back, and their value at cost (display only - the
// server values the saved return itself).
function renderPurchaseReturnSum() {
  const el = document.getElementById('prt-sum');
  if (!el || !prtContext) return;
  let lines = 0, units = 0, value = 0;
  document.querySelectorAll('.prt-qty').forEach(input => {
    const qty = Number(input.value) || 0;
    if (qty <= 0) return;
    const l = prtContext.lines[Number(input.dataset.line)] || {};
    lines += 1;
    units += qty;
    if (l.stock_bucket === 'Accepted') value += qty * (Number(l.unit_cost) || 0);
  });
  el.innerHTML = `<span><b>${lines}</b> line${lines === 1 ? '' : 's'} · Units <b>${units}</b></span>
    <span class="txn-total" title="Value going back, ex-GST">${txnMoney(value)}</span>`;
}

async function loadPurchaseReturnLines(grnID) {
  const linesEl = document.getElementById('prt-lines');
  const saveBtn = document.getElementById('prt-save-btn');
  document.getElementById('prt-form-error').classList.add('hidden');
  prtContext = null;
  saveBtn.disabled = true;
  linesEl.className = 'txn-empty';
  const sumEl = document.getElementById('prt-sum');
  if (sumEl) sumEl.innerHTML = '<span>Choose a GRN to see what can be returned.</span>';
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
  linesEl.className = '';
  linesEl.innerHTML = `
    <div class="txn-hint prt-context">Vendor <b data-link-doctype="Vendor" data-link-ref="${escapeHTMLText(prtContext.vendor_id || '')}">${escapeHTMLText(prtContext.vendor_id || '-')}</b>, returning from <b data-link-doctype="Location" data-link-ref="${escapeHTMLText(prtContext.location || '')}">${escapeHTMLText(prtContext.location || '-')}</b>. Enter the quantity going back on each line.</div>
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
  renderPurchaseReturnSum();
}

async function savePurchaseReturn() {
  const errorEl = document.getElementById('prt-form-error');
  errorEl.classList.add('hidden');
  const showError = msg => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  if (!prtContext) { showError('Choose a GRN first.'); return; }
  const reason = document.getElementById('prt-reason').value.trim();
  const reasonCode = (document.getElementById('prt-reason-code') || {}).value || '';
  if (!reason && !reasonCode) { showError('Choose or type the reason the goods are going back.'); return; }
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
    body: JSON.stringify({ grn_id: prtContext.grn_id, reason, reason_code: reasonCode || undefined, return_items: JSON.stringify(items), status: 'Draft' })
  });
  if (!res) return;
  if (!res.ok) { showError(await getErrorMessage(res, 'Failed to save the purchase return.')); return; }
  const saved = await res.json();
  showToast(`Purchase return ${saved.id} saved as a draft.`, { variant: 'info' });
  txnDone('prt-screen');
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
    const gstNote = result.gst_reversed ? ` Input GST of ${formatMoney(result.gst_reversed)} already claimed on these goods was reversed.` : '';
    showToast(`Posted. Debit note ${result.debit_note_id} for ${formatMoney(result.return_value)} raised and posted to the vendor.${gstNote}`, { variant: 'success' });
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

export { renderApprovalsView, decideApproval, decideApprovalInner, newPODraft, renderPurchaseOrdersView, renderPOComposer, renderPOLines, schedulePOPreview, runPOPreview, poDraftPayload, renderPOSupplyBanner, renderPOTotals, savePurchaseOrder, savePurchaseOrderInner, submitPOForApproval, submitDocForApproval, submitQualityInspectionForApproval, sendSubcontractOrder, receiveSubcontractOrder, mergeCustomerRow, renderGRNWorkbenchView, renderGRNLinesList, lookupGRNBarcode, addGRNLine, populateGRNBatchRow, loadGRNItemsFromPO, loadGRNItemsFromPOInner, loadGRNItemsFromASN, createGRN, createGRNInner, renderASNView, renderASNLinesList, addASNLine, createASN, createASNInner, renderPurchaseReturnsView, loadPurchaseReturnLines, savePurchaseReturn, postPurchaseReturn, submitPurchaseReturnForApproval, cancelPurchaseReturn };
