// Stages 148.15 / 148.17: vendor advances and volume rebate claims. Loaded on
// demand by the VendorAdvance / VendorRebateClaim lists; shared services
// (apiFetch, showApiError, showToast, guardAgainstDoubleSubmit,
// attachLinkTypeahead, renderView, escapeHTMLText) are globals.

function vaDialog(id, title, fields, saveLabel, onSave, view = 'doctype-table') {
  document.getElementById(id)?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = id;
  const rows = fields.map(f => `<div class="form-group"><label class="form-label" for="${id}-${f.name}">${escapeHTMLText(f.label)}</label>` +
    (f.options
      ? `<select id="${id}-${f.name}" class="form-select">${f.options.map(o => `<option value="${escapeHTMLText(o.value)}">${escapeHTMLText(o.label)}</option>`).join('')}</select></div>`
      : `<input id="${id}-${f.name}" class="form-input${f.type === 'number' ? ' num' : ''}" type="${f.type || 'text'}" ${f.type === 'number' ? 'min="0" step="0.01"' : ''} autocomplete="off" value="${escapeHTMLText(f.value || '')}"></div>`)).join('');
  overlay.innerHTML = `
    <div class="modal-container s-c31abe" role="dialog" aria-modal="true" aria-labelledby="${id}-title">
      <div class="modal-header"><h3 class="modal-title" id="${id}-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-va-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-63bcac">${rows}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-va-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-va-save>${escapeHTMLText(saveLabel)}</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-va-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const get = name => overlay.querySelector(`#${id}-${name}`).value.trim();
  const save = overlay.querySelector('[data-va-save]');
  save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
    if (await onSave(get)) { close(); renderView(view); }
  }));
  return overlay;
}

async function vaPost(path, body, failText) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

function openVendorAdvanceGive() {
  const overlay = vaDialog('va-give', 'Give advance', [
    { name: 'vendor', label: 'Vendor' }, { name: 'po', label: 'Purchase order' },
    { name: 'amount', label: 'Amount', type: 'number' }, { name: 'date', label: 'Date', type: 'date' },
    { name: 'ref', label: 'Reference' },
  ], 'Give advance', async get => {
    const out = await vaPost('/api/v1/procurement/vendor-advances', {
      vendor_id: get('vendor'), po_id: get('po'), amount: Number(get('amount')) || 0,
      advance_date: get('date'), reference: get('ref'),
    }, 'The advance was not given.');
    if (out) showToast('Advance given.', { variant: 'success' });
    return !!out;
  });
  attachLinkTypeahead(overlay.querySelector('#va-give-vendor'), 'Vendor', { keepCode: true });
  attachLinkTypeahead(overlay.querySelector('#va-give-po'), 'PurchaseOrder', { keepCode: true });
}

// What a bill still owes: the server's outstanding amount once anything is paid.
function vaBillDue(b) {
  const o = b.outstanding_amount;
  return Number(o === undefined || o === null || o === '' ? b.invoice_amount : o) || 0;
}

// A record's rows for one vendor; the ids behind them can't be typed by anyone.
async function vaVendorRows(doctype, vendorId, keep) {
  const res = await apiFetch(`/api/v1/doc/${doctype}?vendor_id=${encodeURIComponent(vendorId || '')}&limit=500`);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'The list could not be loaded.'); return null; }
  return (await res.json()).filter(keep);
}

function vendorAdvanceOp(op, id, openAmount, vendorId, po) {
  const base = `/api/v1/procurement/vendor-advances/${encodeURIComponent(id)}`;
  if (op === 'cancel') {
    showCustomConfirm('Cancel this advance?', 'Cancel advance').then(ok => {
      if (!ok) return;
      vaPost(`${base}/cancel`, {}, 'The advance was not cancelled.').then(out => { if (out) { showToast('Advance cancelled.', { variant: 'success' }); renderView('doctype-table'); } });
    });
    return;
  }
  const open = openAmount ? String(openAmount) : '';
  if (op === 'apply') {
    vaVendorRows('VendorInvoice', vendorId, b => b.status === 'Matched' && (!po || b.po_id === po) && (!b.currency || b.currency === 'INR') && vaBillDue(b) > 0).then(bills => {
      if (!bills) return;
      if (!bills.length) { showToast(po ? 'No matched bill on this purchase order.' : 'No matched bill for this vendor.', { variant: 'error' }); return; }
      const overlay = vaDialog('va-apply', 'Apply advance', [
        { name: 'bill', label: 'Bill', options: bills.map(b => ({ value: b.id, label: `${b.invoice_number || b.id} (${formatMoney(vaBillDue(b))} due)` })) },
        { name: 'amount', label: 'Amount', type: 'number', value: open },
      ], 'Apply', async get => {
        const out = await vaPost(`${base}/apply`, { invoice_id: get('bill'), amount: Number(get('amount')) || 0 }, 'The advance was not applied.');
        if (out) showToast('Advance applied.', { variant: 'success' });
        return !!out;
      });
      vaFillSmaller(overlay, '#va-apply-bill', '#va-apply-amount', () => Number(open) || 0, v => {
        return vaBillDue(bills.find(x => x.id === v));
      });
    });
  } else if (op === 'refund') {
    vaDialog('va-refund', 'Refund advance', [
      { name: 'amount', label: 'Amount', type: 'number', value: open }, { name: 'date', label: 'Date', type: 'date' },
    ], 'Refund', async get => {
      const out = await vaPost(`${base}/refund`, { amount: Number(get('amount')) || 0, date: get('date') }, 'The refund was not posted.');
      if (out) showToast('Refund posted.', { variant: 'success' });
      return !!out;
    });
  }
}

function openVendorRebateCalculate() {
  const overlay = vaDialog('vr-calc', 'Calculate rebates', [
    { name: 'from', label: 'From', type: 'date' }, { name: 'to', label: 'To', type: 'date' }, { name: 'vendor', label: 'Vendor' },
  ], 'Calculate', async get => {
    const out = await vaPost('/api/v1/procurement/vendor-rebates/calculate', { from: get('from'), to: get('to'), vendor_id: get('vendor') }, 'The rebates were not calculated.');
    const n = out ? (out.claims || []).length : 0;
    if (out) showToast(`${n} ${n === 1 ? 'vendor' : 'vendors'} checked.`, { variant: 'success' });
    return !!out;
  });
  attachLinkTypeahead(overlay.querySelector('#vr-calc-vendor'), 'Vendor', { keepCode: true });
}

async function vendorRebateOp(op, id) {
  const out = await vaPost(`/api/v1/procurement/vendor-rebates/${encodeURIComponent(id)}/${op}`, {}, op === 'submit' ? 'The claim was not sent.' : 'The claim was not settled.');
  if (!out) return;
  showToast(op === 'submit' ? 'Sent for approval.' : 'Claim settled.', { variant: 'success' });
  renderView('doctype-table');
}

// The amount starts at the smaller of what is open and what is due.
function vaFillSmaller(overlay, pickSel, amountSel, fixed, picked) {
  const pick = overlay.querySelector(pickSel), amount = overlay.querySelector(amountSel);
  const fill = () => { const n = Math.min(fixed(), picked(pick.value)); amount.value = n > 0 ? String(n) : ''; };
  pick.addEventListener('change', fill);
  fill();
}

// From a Matched bill: pick the advance to apply to it.
async function applyAdvanceToBill(invoiceId, vendorId, due, po) {
  const advances = await vaVendorRows('VendorAdvance', vendorId, a => (a.status === 'Open' || a.status === 'Part Applied') && (!a.po_id || a.po_id === po));
  if (!advances) return;
  if (!advances.length) { showToast('No open advance for this vendor.', { variant: 'error' }); return; }
  const overlay = vaDialog('va-bill', 'Apply advance', [
    { name: 'adv', label: 'Advance', options: advances.map(a => ({ value: a.id, label: `${[a.reference, a.advance_date].filter(Boolean).join(" · ") || a.id} (${formatMoney(a.open_amount)} open)` })) },
    { name: 'amount', label: 'Amount', type: 'number' },
  ], 'Apply', async get => {
    const out = await vaPost(`/api/v1/procurement/vendor-advances/${encodeURIComponent(get('adv'))}/apply`, { invoice_id: invoiceId, amount: Number(get('amount')) || 0 }, 'The advance was not applied.');
    if (out) showToast('Advance applied.', { variant: 'success' });
    return !!out;
  }, 'vendor-invoices');
  vaFillSmaller(overlay, '#va-bill-adv', '#va-bill-amount', () => Number(due) || Infinity, v => Number(advances.find(x => x.id === v).open_amount) || 0);
}

// Stage 148.14: a purchase request's lines, combined into draft POs by vendor.
function openCombineRequisitions(defaultVendor) {
  const overlay = vaDialog('pr-combine', 'Combine open requests', [
    { name: 'vendor', label: 'Vendor', value: defaultVendor || '' },
  ], 'Combine', async get => {
    const out = await vaPost('/api/v1/procurement/combine-requisitions', { vendor_id: get('vendor') }, 'Could not combine the requests.');
    if (out) await showCustomAlert((out.documents || []).map(d => `${d.po_id} (${d.store}, ${d.lines} line${d.lines === 1 ? '' : 's'})`).join('\n'), 'Draft POs made');
    return !!out;
  });
  attachLinkTypeahead(overlay.querySelector('#pr-combine-vendor'), 'Vendor', { keepCode: true });
}

export { applyAdvanceToBill, openVendorAdvanceGive, vendorAdvanceOp, openVendorRebateCalculate, vendorRebateOp, openCombineRequisitions };
