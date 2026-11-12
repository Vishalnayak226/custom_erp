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

// Stage 148.1: a tracked item's line names its pieces or batch, so it books their own cost.
function namedStockInputs(l, i) {
  const box = (key, label) => `<input class="form-input s-96ad60" data-sil="${key}" data-i="${i}" placeholder="${label}" aria-label="${label}" value="${esc(l[key] || '')}">`;
  return (l.tracking === 'Serial' || l.tracking === 'Batch and Serial' ? box('serial_nos', 'Piece codes') : '')
    + (l.tracking === 'Batch' || l.tracking === 'Batch and Serial' ? box('batch_no', 'Batch') : '')
    + (l.free ? box('income_account', 'Income account (blank: Sales)') : '');  // Stage 148.22
}

export async function renderSalesInvoiceEntry(container, onDone) {
  const S = { lines: [], storeState: '', customer: null };
  const { sessionStore } = await import('./txn-screen.js' + Q);
  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group s-116966"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  panel.innerHTML = `<div class="page-header s-249d01"><div class="page-title-section"><h1 class="page-title">New Sales Invoice</h1></div>
      <div><button type="button" class="btn btn-ghost" data-si="back">Back to invoices</button></div></div>
    <div class="s-d9da29">
      ${field('si-customer', 'Customer', 220)}${field('si-gstin', 'Buyer GSTIN', 170, 'placeholder="for a registered buyer"')}
      ${field('si-store', 'Store', 150, `value="${esc(sessionStore())}"`)}
      ${field('si-date', 'Invoice date', 150, `type="date" value="${new Date().toISOString().slice(0, 10)}"`)}
      ${field('si-pos', 'Place of supply (state code)', 120, 'maxlength="2" placeholder="e.g. 29"')}
      ${field('si-due', 'Due date', 150, 'type="date"')}${field('si-ref', 'Customer reference', 150, 'placeholder="their PO no"')}
      ${field('si-challan', 'Delivery challan', 150, 'placeholder="if already sent"')}
    </div>
    <div class="txn-hint s-761d3a" data-si="supply"></div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th>HSN</th><th class="num">Qty</th><th class="num">Rate (before GST)</th><th class="num">Discount (₹)</th><th class="num">GST %</th><th class="num">Taxable</th><th class="num">GST</th><th class="num">Amount</th><th></th></tr></thead><tbody data-si="lines"></tbody></table></div>
    <div class="s-918f5b"><input class="form-input s-3521b3" data-si="add" placeholder="Add by Design, Combination ID or barcode" autocomplete="off"><input class="form-input s-a5d7a0" data-si="free" placeholder="Add a line by description" autocomplete="off"></div>
    <div data-si="totals" class="s-140432"></div>
    <div class="login-error hidden s-56f435" data-si="error"></div>
    <div class="s-1b0f49"><button type="button" class="btn btn-primary" data-si="save">Save as draft</button></div>`;
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
      // 142.4: the line's discount comes off before GST; it cannot exceed the line.
      l.discount = Math.min(Math.max(0, num(l.discount)), r2(num(l.qty) * num(l.rate)));
      l.taxable = r2(num(l.qty) * num(l.rate) - l.discount);
      l.tax = r2(l.taxable * num(l.gst_rate) / 100);
      taxable += l.taxable; tax += l.tax;
      return `<tr><td>${esc(l.name || l.sku)}<div class="text-muted s-6cb285">${esc(l.sku)}</div>${l.why ? `<div class="text-muted s-6cb285">${esc(l.why)}</div>` : ''}${S.needReason ? `<input class="form-input s-1ca389" placeholder="Why this rate" data-sil="floor_reason" data-i="${i}" value="${esc(l.floor_reason || '')}">` : ''}${namedStockInputs(l, i)}</td><td>${esc(l.hsn || '')}</td>
        <td class="num"><input type="number" min="0" step="any" class="form-input s-21b28d" data-sil="qty" data-i="${i}" value="${esc(l.qty)}"></td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input s-b54ee3" data-sil="rate" data-i="${i}" value="${esc(l.rate)}"></td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input s-d29533" data-sil="discount" data-i="${i}" value="${esc(l.discount)}" title="Taken off this line before GST"></td>
        <td class="num"><input type="number" min="0" step="0.01" class="form-input s-ac4691" data-sil="gst_rate" data-i="${i}" value="${esc(l.gst_rate)}"></td>
        <td class="num">${formatMoney(l.taxable)}</td><td class="num">${formatMoney(l.tax)}</td><td class="num">${formatMoney(l.taxable + l.tax)}</td>
        <td><button type="button" class="action-btn action-btn-danger" data-sil-remove="${i}">Remove</button></td></tr>`;
    }).join('') : '<tr><td colspan="10" class="s-f692de">Add the items billed.</td></tr>';
    taxable = r2(taxable); tax = r2(tax);
    const half = r2(tax / 2);
    S.totals = { taxable, tax, cgst: inter ? 0 : half, sgst: inter ? 0 : r2(tax - half), igst: inter ? tax : 0, total: r2(taxable + tax) };
    q('totals').innerHTML = `<table class="tdp-totals s-046678"><tr><td>Taxable value</td><td class="num">${formatMoney(taxable)}</td></tr>
      ${inter ? `<tr><td>IGST</td><td class="num">${formatMoney(S.totals.igst)}</td></tr>` : `<tr><td>CGST</td><td class="num">${formatMoney(S.totals.cgst)}</td></tr><tr><td>SGST</td><td class="num">${formatMoney(S.totals.sgst)}</td></tr>`}
      <tr><td><strong>Invoice total</strong></td><td class="num"><strong>${formatMoney(S.totals.total)}</strong></td></tr></table>`;
    panel.querySelectorAll('[data-sil]').forEach(el => el.addEventListener('change', async () => {
      if (!el.isConnected) return;
      const ln = S.lines[Number(el.dataset.i)];
      ln[el.dataset.sil] = el.value;
      if (el.dataset.sil === 'rate') { ln.rateTyped = true; ln.why = ''; } // a typed rate always wins (148.26)
      if (el.dataset.sil === 'qty') await priceLine(ln);
      draw();
    }));
    panel.querySelectorAll('[data-sil-remove]').forEach(el => el.addEventListener('click', () => { S.lines.splice(Number(el.dataset.silRemove), 1); draw(); }));
  };
  // 148.26: a blank rate comes from the customer's price list (setting off: nothing changes).
  const priceLine = async (l) => {
    if (l.rateTyped || !S.customer && !v('#si-customer')) return;
    const res = await apiFetch('/api/v1/pricing/line', { method: 'POST', body: JSON.stringify({ customer_id: (S.customer && (S.customer.id || S.customer.code)) || '', customer: v('#si-customer'), sku: l.sku, qty: num(l.qty), as_of: v('#si-date') }) });
    if (!res || !res.ok) return;
    const p = await res.json();
    if (p.found) { l.rate = p.rate; l.why = p.reason || ''; } else { l.why = ''; }
  };
  q('back').addEventListener('click', () => onDone && onDone());
  attachLinkTypeahead(panel.querySelector('#si-customer'), 'Customer', { keepCode: true, onPick: (c) => {
    S.customer = c;
    if (c.gstin) panel.querySelector('#si-gstin').value = c.gstin;
    const code = (c.gstin || '').slice(0, 2) || c.state_code || '';
    if (code) panel.querySelector('#si-pos').value = code;
    fillDue();
    Promise.all(S.lines.map(priceLine)).then(draw);
    draw();
  } });
  // Due date from the customer's terms (Stage 148.3); a date typed by hand is never replaced.
  const dueEl = panel.querySelector('#si-due');
  let dueAuto = false;
  dueEl.addEventListener('input', () => { dueAuto = false; });
  const fillDue = async () => {
    const term = S.customer && S.customer.payment_term;
    if (!term || (dueEl.value && !dueAuto)) return;
    const res = await apiFetch(`/api/v1/doc/PaymentTerm/${encodeURIComponent(term)}`);
    if (!res || !res.ok) return;
    const t = await res.json();
    const day = v('#si-date');
    if (!day || (dueEl.value && !dueAuto)) return;
    const d = new Date(day + 'T00:00:00');
    if (t.end_of_month === 'Yes') d.setMonth(d.getMonth() + 1, 0);
    d.setDate(d.getDate() + Number(t.net_days || 0));
    dueEl.value = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    dueAuto = true;
  };
  panel.querySelector('#si-date').addEventListener('change', fillDue);
  const storeEl = panel.querySelector('#si-store');
  attachLinkTypeahead(storeEl, 'Location', { keepCode: true });
  const loadState = async () => { S.storeState = storeEl.value.trim() ? await storeStateCode(storeEl.value.trim()) : ''; draw(); };
  storeEl.addEventListener('change', loadState);
  storeEl.addEventListener('blur', loadState);
  panel.querySelector('#si-gstin').addEventListener('change', () => { const g = v('#si-gstin'); if (g.length >= 2) panel.querySelector('#si-pos').value = g.slice(0, 2); draw(); });
  panel.querySelector('#si-pos').addEventListener('input', draw);
  const addInput = q('add');
  attachLinkTypeahead(addInput, 'Item', { noSetupHint: true, onPick: async (item) => {
    S.lines.push({ sku: item.code || item.id, name: item.name || '', hsn: item.hsn_code || '', qty: 1, rate: num(item.sale_price || item.price || item.mrp), discount: 0, gst_rate: num(item.gst_rate), tracking: item.tracking_mode || '' });
    await priceLine(S.lines[S.lines.length - 1]);
    addInput.value = '';
    addInput.dispatchEvent(new Event('input'));
    draw();
  } });
  // Stage 148.22: a line with no item: a description, its own GST and income account.
  q('free').addEventListener('keydown', e => {
    if (e.key !== 'Enter' || !e.target.value.trim()) return;
    e.preventDefault();
    S.lines.push({ sku: '', name: e.target.value.trim(), hsn: '', qty: 1, rate: 0, discount: 0, gst_rate: 0, tracking: '', free: true, income_account: '' });
    e.target.value = '';
    draw();
  });
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
      customer: v('#si-customer'), customer_id: (S.customer && (S.customer.code || S.customer.id) && v('#si-customer') === (S.customer.code || S.customer.id) ? (S.customer.id || S.customer.code) : ''), payment_term: (S.customer && S.customer.payment_term) || '', customer_gstin: g.toUpperCase(), location: v('#si-store'), invoice_date: v('#si-date'), place_of_supply: v('#si-pos'),
      due_date: v('#si-due'), customer_reference: v('#si-ref'), delivery_challan: v('#si-challan'), interstate: interstate(),
      items: JSON.stringify(S.lines.map(l => ({ sku: l.sku, item_name: l.name, hsn_code: l.hsn, qty: num(l.qty), rate: num(l.rate), discount: num(l.discount), gst_rate: num(l.gst_rate), taxable: l.taxable, tax: l.tax, amount: r2(l.taxable + l.tax), serial_nos: l.serial_nos || '', batch_no: l.batch_no || '', income_account: (l.income_account || '').trim(), floor_reason: l.floor_reason || '' }))),
      taxable_amount: S.totals.taxable, cgst: S.totals.cgst, sgst: S.totals.sgst, igst: S.totals.igst, total_tax: S.totals.tax, total_amount: S.totals.total, status: 'Draft' }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { const m = await getErrorMessage(res, 'The invoice was not saved.'); if (/lowest rate/.test(m)) { S.needReason = true; draw(); } fail(m); return; }
    showToast('Invoice saved as a draft - Post it from the list.', { variant: 'success' });
    if (onDone) onDone();
  });
  if (storeEl.value.trim()) loadState(); else draw();
}
