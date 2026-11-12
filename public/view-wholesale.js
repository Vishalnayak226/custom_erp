// Stages 148.31 / 148.32: summary invoices and drop-ship confirmation. Loaded
// on demand from the SalesInvoice and PurchaseOrder lists when the Wholesale
// module is on. Shared services (apiFetch, showApiError, showToast,
// guardAgainstDoubleSubmit, renderView, escapeHTMLText) are globals.

function wsOverlay(id, title, body, footer) {
  document.getElementById(id)?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = id;
  overlay.innerHTML = `
    <div class="modal-container s-c06a24" role="dialog" aria-modal="true" aria-labelledby="${id}-title">
      <div class="modal-header"><h3 class="modal-title" id="${id}-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-ws-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-63bcac">${body}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-ws-close>Close</button>${footer || ''}</div>
    </div>`;
  document.body.appendChild(overlay);
  overlay.querySelectorAll('[data-ws-close]').forEach(b => b.addEventListener('click', () => overlay.remove()));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') overlay.remove(); });
  return overlay;
}

async function wsCall(path, options, failText) {
  const res = await apiFetch(path, options);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

function openSummaryInvoices() {
  const overlay = wsOverlay('ws-summary', 'Summary invoices', `
    <div class="s-2ee67a">
      <div class="form-group"><label class="form-label" for="ws-from">From</label><input id="ws-from" class="form-input" type="date"></div>
      <div class="form-group"><label class="form-label" for="ws-to">To</label><input id="ws-to" class="form-input" type="date"></div>
      <button type="button" class="btn btn-primary" data-ws-show>Show groups</button>
    </div>
    <div data-ws-list></div>`, '<button type="button" class="btn btn-primary" data-ws-make disabled>Create invoices</button>');
  const list = overlay.querySelector('[data-ws-list]');
  const make = overlay.querySelector('[data-ws-make]');
  const range = () => ({ from: overlay.querySelector('#ws-from').value, to: overlay.querySelector('#ws-to').value });
  overlay.querySelector('[data-ws-show]').addEventListener('click', async () => {
    const { from, to } = range();
    const out = await wsCall(`/api/v1/finance/summary-invoices/preview?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`, {}, 'The groups could not be shown.');
    if (!out) return;
    const groups = out.groups || [];
    if (!groups.length) { list.innerHTML = '<p class="empty-state-text">Nothing to invoice.</p>'; make.disabled = true; return; }
    list.innerHTML = `<div class="table-panel"><table class="data-table"><thead><tr><th></th><th>Bill-to</th><th>Place of supply</th><th>PO</th><th class="num">Orders</th><th class="num">Total</th><th>Why</th></tr></thead><tbody>` +
      groups.map(g => `<tr><td><input type="checkbox" data-ws-key="${escapeHTMLText(g.key)}" ${g.blocked ? 'disabled' : 'checked'} aria-label="Include"></td>
        <td>${escapeHTMLText(g.payer_name || g.payer)}</td><td>${escapeHTMLText(g.place_of_supply || '')}</td><td>${escapeHTMLText(g.customer_po || '')}</td>
        <td class="num">${(g.orders || []).length}</td><td class="num">${Number(g.total || 0).toFixed(2)}</td>
        <td>${escapeHTMLText(g.blocked ? g.reason : (g.why || []).concat(g.gst_basis || []).join('; '))}</td></tr>`).join('') +
      '</tbody></table></div>';
    make.disabled = !list.querySelector('[data-ws-key]:not(:disabled)');
  });
  make.addEventListener('click', () => guardAgainstDoubleSubmit(make, 'Creating...', async () => {
    const keys = [...list.querySelectorAll('[data-ws-key]:checked')].map(c => c.dataset.wsKey);
    if (!keys.length) return;
    const out = await wsCall('/api/v1/finance/summary-invoices', { method: 'POST', body: JSON.stringify({ ...range(), keys }) }, 'The invoices were not created.');
    if (!out) return;
    showToast(`${(out.invoices || []).length} invoice(s) created.`, { variant: 'success' });
    overlay.remove();
    renderView('doctype-table');
  }));
}

function openDropShipConfirm() {
  const overlay = wsOverlay('ws-dropship', 'Drop-ship shipments', '<div data-ws-list></div>',
    '<button type="button" class="btn btn-primary" data-ws-ship disabled>Confirm shipped</button><button type="button" class="btn btn-outline" data-ws-all>Confirm all ready</button>');
  const list = overlay.querySelector('[data-ws-list]');
  const ship = overlay.querySelector('[data-ws-ship]');
  const load = async () => {
    const out = await wsCall('/api/v1/procurement/dropship-lines', {}, 'The list could not be shown.');
    if (!out) return;
    const rows = out.lines || [];
    if (!rows.length) { list.innerHTML = '<p class="empty-state-text">Nothing waiting on a vendor.</p>'; ship.disabled = true; return; }
    list.innerHTML = `<div class="table-panel"><table class="data-table"><thead><tr><th></th><th>Order</th><th>Ship to</th><th>Item</th><th class="num">Qty</th><th>Vendor</th><th>PO</th><th>Status</th></tr></thead><tbody>` +
      rows.map(l => `<tr><td><input type="checkbox" data-ws-line="${escapeHTMLText(l.line_id)}" ${l.ready ? '' : 'disabled'} aria-label="Shipped"></td>
        <td>${escapeHTMLText(l.order_id)}</td><td>${escapeHTMLText(l.address || l.customer)}</td><td>${escapeHTMLText(l.sku)}</td><td class="num">${l.qty}</td>
        <td>${escapeHTMLText(l.vendor)}</td><td>${escapeHTMLText(l.po_id || '')}</td><td>${escapeHTMLText(l.po_status || '')}</td></tr>`).join('') + '</tbody></table></div>';
    ship.disabled = !list.querySelector('[data-ws-line]:not(:disabled)');
  };
  const confirm = (body, button) => guardAgainstDoubleSubmit(button, 'Saving...', async () => {
    const out = await wsCall('/api/v1/procurement/dropship-confirm', { method: 'POST', body: JSON.stringify(body) }, 'The shipment was not saved.');
    if (!out) return;
    const skipped = (out.skipped || []).length;
    showToast(`${(out.confirmed || []).length} shipped${skipped ? `, ${skipped} not approved yet` : ''}.`, { variant: 'success' });
    await load();
  });
  ship.addEventListener('click', () => confirm({ line_ids: [...list.querySelectorAll('[data-ws-line]:checked')].map(c => c.dataset.wsLine) }, ship));
  const all = overlay.querySelector('[data-ws-all]');
  all.addEventListener('click', () => confirm({ all: true }, all));
  load();
}

// Adds a tool button to a list screen's action bar (module on only; the caller checks).
function addWholesaleButton(actions, label, open) {
  if (!actions || actions.querySelector(`[data-ws-tool="${label}"]`)) return;
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'btn btn-outline';
  b.dataset.wsTool = label;
  b.textContent = label;
  b.addEventListener('click', () => open());
  actions.prepend(b);
}

export { openSummaryInvoices, openDropShipConfirm, addWholesaleButton };
