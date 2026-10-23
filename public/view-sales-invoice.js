// Stage 130.16 (74.3.4): the Sales Invoice entry screen - a credit sale to a
// customer: the buyer's GSTIN and place of supply (from the customer, or
// typed), lines with their GST, and CGST + SGST within the store's state or
// IGST across states. Saved as a Draft; Post on the list books it (AR,
// revenue and output GST).
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const r2 = (v) => Math.round(v * 100) / 100;

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

// storeStateCode is a store's legal entity's state code.
async function storeStateCode(store) {
  const loc = (await getJSON(`/api/v1/doc/Location?code=${encodeURIComponent(store)}&limit=1`, []))[0];
  if (!loc || !loc.legal_entity) return '';
  const le = (await getJSON(`/api/v1/doc/LegalEntity?code=${encodeURIComponent(loc.legal_entity)}&limit=1`, []))[0]
    || await getJSON(`/api/v1/doc/LegalEntity/${encodeURIComponent(loc.legal_entity)}`, null);
  return String((le && (le.state_code || (le.gstin || '').slice(0, 2))) || '');
}

export async function renderSalesInvoiceEntry(container, onDone) {
  const S = { lines: [], storeState: '', customer: null };
  const { sessionStore } = await import('./txn-screen.js' + Q);
  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group" style="margin:0;"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  panel.innerHTML = `<div class="page-header" style="margin:0 0 12px;"><div class="page-title-section"><h1 class="page-title">New Sales Invoice</h1></div>
      <div><button type="button" class="btn btn-ghost" data-si="back">Back to invoices</button></div></div>
    <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      ${field('si-customer', 'Customer', 220)}${field('si-gstin', 'Buyer GSTIN', 170, 'placeholder="for a registered buyer"')}
      ${field('si-store', 'Store', 150, `value="${esc(sessionStore())}"`)}
      ${field('si-date', 'Invoice date', 150, `type="date" value="${new Date().toISOString().slice(0, 10)}"`)}
      ${field('si-pos', 'Place of supply (state code)', 120, 'maxlength="2" placeholder="e.g. 29"')}
      ${field('si-due', 'Due date', 150, 'type="date"')}${field('si-ref', 'Customer reference', 150, 'placeholder="their PO no"')}
    </div>
    <div class="txn-hint" data-si="supply" style="margin-bottom:10px;"></div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th>HSN</th><th class="num">Qty</th><th class="num">Rate (before GST)</th><th class="num">GST %</th><th class="num">Taxable</th><th class="num">GST</th><th class="num">Amount</th><th></th></tr></thead><tbody data-si="lines"></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;"><input class="form-input" data-si="add" placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off"></div>
    <div data-si="totals" style="margin-top:14px;max-width:320px;margin-left:auto;"></div>
    <div class="login-error hidden" data-si="error" style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-si="save">Save as draft</button> <span class="text-muted" style="font-size:12px;">Post it from the invoice list.</span></div>`;
  container.appendChild(panel);
  const q = (k) => panel.querySelector(`[data-si="${k}"]`);
  const v = (id) => panel.querySelector(id).value.trim();
  const interstate = () => { const p = v('#si-pos'); return !!(p && S.storeState && p !== S.storeState); };
  let drawing = false;
  const draw = () => {
    // A redraw replaces the line inputs; the one losing focus may fire its
    // own change on the way out - never redraw inside a redraw.
    if (drawing) return;
    drawing = true;
    try { drawNow(); } finally { drawing = false; }
  };
  const drawNow = () => {
    const inter = interstate();
    q('supply').textContent = !v('#si-store') ? 'Choose the store - its legal entity\'s state decides CGST + SGST or IGST.'
      : !S.storeState ? `Store ${v('#si-store')} has no state on its legal entity (Setup > Legal Entities) - tax is split CGST + SGST until it does.`
      : v('#si-pos') ? (inter ? `Inter-state supply (store ${S.storeState} → ${v('#si-pos')}): IGST.` : `Within state ${S.storeState}: CGST + SGST.`) : 'Enter the place of supply.';
    let taxable = 0, tax = 0;
    q('lines').innerHTML = S.lines.length ? S.lines.map((l, i) => {
      l.taxable = r2(num(l.qty) * num(l.rate));
      l.tax = r2(l.taxable * num(l.gst_rate) / 100);
      taxable += l.taxable; tax += l.tax;
      return `<tr><td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td><td>${esc(l.hsn || '')}</td>
        <td class="num"><input type="number" min="0" step="any" class="form-input" style="width:80px;text-align:right;" data-sil="qty" data-i="${i}" value="${esc(l.qty)}"></td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input" style="width:110px;text-align:right;" data-sil="rate" data-i="${i}" value="${esc(l.rate)}"></td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input" style="width:70px;text-align:right;" data-sil="gst_rate" data-i="${i}" value="${esc(l.gst_rate)}"></td>
        <td class="num">${formatMoney(l.taxable)}</td><td class="num">${formatMoney(l.tax)}</td><td class="num">${formatMoney(l.taxable + l.tax)}</td>
        <td><button type="button" class="action-btn action-btn-danger" data-sil-remove="${i}">Remove</button></td></tr>`;
    }).join('') : '<tr><td colspan="9" style="text-align:center;color:var(--text-muted);padding:16px;">Add the items billed.</td></tr>';
    taxable = r2(taxable); tax = r2(tax);
    const half = r2(tax / 2);
    S.totals = { taxable, tax, cgst: inter ? 0 : half, sgst: inter ? 0 : r2(tax - half), igst: inter ? tax : 0, total: r2(taxable + tax) };
    q('totals').innerHTML = `<table class="tdp-totals" style="width:100%;"><tr><td>Taxable value</td><td class="num">${formatMoney(taxable)}</td></tr>
      ${inter ? `<tr><td>IGST</td><td class="num">${formatMoney(S.totals.igst)}</td></tr>` : `<tr><td>CGST</td><td class="num">${formatMoney(S.totals.cgst)}</td></tr><tr><td>SGST</td><td class="num">${formatMoney(S.totals.sgst)}</td></tr>`}
      <tr><td><strong>Invoice total</strong></td><td class="num"><strong>${formatMoney(S.totals.total)}</strong></td></tr></table>`;
    panel.querySelectorAll('[data-sil]').forEach(el => el.addEventListener('change', () => { if (!el.isConnected) return; S.lines[Number(el.dataset.i)][el.dataset.sil] = el.value; draw(); }));
    panel.querySelectorAll('[data-sil-remove]').forEach(el => el.addEventListener('click', () => { S.lines.splice(Number(el.dataset.silRemove), 1); draw(); }));
  };
  q('back').addEventListener('click', () => onDone && onDone());
  attachLinkTypeahead(panel.querySelector('#si-customer'), 'Customer', { keepCode: true, onPick: (c) => {
    S.customer = c;
    if (c.gstin) panel.querySelector('#si-gstin').value = c.gstin;
    const code = (c.gstin || '').slice(0, 2) || c.state_code || '';
    if (code) panel.querySelector('#si-pos').value = code;
    draw();
  } });
  const storeEl = panel.querySelector('#si-store');
  attachLinkTypeahead(storeEl, 'Location', { keepCode: true });
  const loadState = async () => { S.storeState = storeEl.value.trim() ? await storeStateCode(storeEl.value.trim()) : ''; draw(); };
  storeEl.addEventListener('change', loadState);
  storeEl.addEventListener('blur', loadState);
  panel.querySelector('#si-gstin').addEventListener('change', () => { const g = v('#si-gstin'); if (g.length >= 2) panel.querySelector('#si-pos').value = g.slice(0, 2); draw(); });
  panel.querySelector('#si-pos').addEventListener('input', draw);
  const addInput = q('add');
  attachLinkTypeahead(addInput, 'Item', { noSetupHint: true, onPick: (item) => {
    S.lines.push({ sku: item.code || item.id, name: item.name || '', hsn: item.hsn_code || '', qty: 1, rate: num(item.sale_price || item.price || item.mrp), gst_rate: num(item.gst_rate) });
    addInput.value = '';
    addInput.dispatchEvent(new Event('input'));
    draw();
  } });
  q('save').addEventListener('click', async e => {
    const btn = e.currentTarget;
    const err = q('error');
    const fail = (m) => { err.textContent = m; err.classList.remove('hidden'); };
    err.classList.add('hidden');
    if (!v('#si-customer')) { fail('Choose the customer.'); return; }
    if (!v('#si-store')) { fail('Choose the store the invoice is issued from.'); return; }
    if (!S.lines.length || S.lines.some(l => num(l.qty) <= 0 || num(l.rate) <= 0)) { fail('Add the items, each with a quantity and a rate.'); return; }
    const g = v('#si-gstin');
    if (g && !/^[0-9]{2}[A-Z0-9]{13}$/i.test(g)) { fail('The buyer GSTIN is 15 characters, starting with the state code.'); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/doc/SalesInvoice', { method: 'POST', body: JSON.stringify({
      customer: v('#si-customer'), customer_gstin: g.toUpperCase(), location: v('#si-store'), invoice_date: v('#si-date'), place_of_supply: v('#si-pos'),
      due_date: v('#si-due'), customer_reference: v('#si-ref'), interstate: interstate(),
      items: JSON.stringify(S.lines.map(l => ({ sku: l.sku, item_name: l.name, hsn_code: l.hsn, qty: num(l.qty), rate: num(l.rate), gst_rate: num(l.gst_rate), taxable: l.taxable, tax: l.tax, amount: r2(l.taxable + l.tax) }))),
      taxable_amount: S.totals.taxable, cgst: S.totals.cgst, sgst: S.totals.sgst, igst: S.totals.igst, total_tax: S.totals.tax, total_amount: S.totals.total, status: 'Draft' }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The invoice was not saved.')); return; }
    showToast('Invoice saved as a draft - Post it from the list.', { variant: 'success' });
    if (onDone) onDone();
  });
  if (storeEl.value.trim()) loadState(); else draw();
}
