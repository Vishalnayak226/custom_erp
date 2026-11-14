// Supplier portal: what an outside supplier sees of its own orders, advance
// ship notices, bills and payments, and where it sends a notice. Lazy module;
// shared services (apiFetch, showApiError, showToast, renderView,
// escapeHTMLText, formatMoney, guardAgainstDoubleSubmit) are globals.

const SP_TABS = [['orders', 'Orders'], ['asns', 'ASNs'], ['bills', 'Bills'], ['payments', 'Payments']];
let spOrderId = '';
let spOrders = [];

const spEsc = s => escapeHTMLText(String(s == null ? '' : s));

const SP_COLUMNS = {
  orders: [['Order', r => r.po_number], ['Status', r => r.status], ['Expected', r => r.expected_date],
    ['Items', r => (r.lines || []).map(l => `${l.sku} ${l.open_qty}/${l.qty}`).join(', ')], ['Open', r => r.open_qty]],
  asns: [['Notice', r => r.asn_number], ['Order', r => r.po_number], ['Status', r => r.status], ['Expected', r => r.expected_date],
    ['Carrier', r => r.carrier], ['Tracking', r => r.tracking_number], ['Items', r => (r.lines || []).map(l => `${l.sku} ${l.qty}`).join(', ')]],
  bills: [['Bill', r => r.invoice_number], ['Date', r => r.invoice_date], ['Due', r => r.due_date], ['Amount', r => formatMoney(r.amount)],
    ['Paid', r => formatMoney(r.paid_amount)], ['Owed', r => formatMoney(r.outstanding_amount)], ['Match', r => r.match_status], ['Payment', r => r.payment_status]],
  payments: [['Date', r => r.date], ['Bill', r => r.invoice_number], ['Amount', r => formatMoney(r.amount)], ['Reference', r => r.reference]],
};

function spTabs(tab) {
  return `<div class="sp-tabs" role="tablist">${SP_TABS.map(([id, label]) =>
    `<button type="button" class="btn btn-outline btn-sm${id === tab ? ' active' : ''}" role="tab" aria-selected="${id === tab}" data-sp-tab="${id}">${label}</button>`).join('')}</div>`;
}

function spTable(tab, rows) {
  if (!rows.length) return '<div class="sp-empty">Nothing here yet.</div>';
  const cols = SP_COLUMNS[tab];
  const act = r => tab !== 'orders' ? ''
    : r.status === 'Approved' && Number(r.open_qty) > 0
      ? `<td><button type="button" class="btn btn-outline btn-sm" data-sp-asn="${spEsc(r.id)}">Send notice</button></td>` : '<td></td>';
  return `<div class="table-wrapper sp-wrap"><table><thead><tr>${cols.map(c => `<th>${c[0]}</th>`).join('')}${tab === 'orders' ? '<th></th>' : ''}</tr></thead>
    <tbody>${rows.map(r => `<tr>${cols.map(c => `<td>${spEsc(c[1](r))}</td>`).join('')}${act(r)}</tr>`).join('')}</tbody></table></div>`;
}

function spNewASN(order) {
  const lines = (order.lines || []).filter(l => Number(l.open_qty) > 0);
  return `<div class="sp-form">
    <p>${spEsc(order.po_number)}</p>
    ${lines.map((l, i) => `<div class="sp-lines"><div class="form-group"><label class="form-label" for="sp-sku-${i}">Item</label>
      <input id="sp-sku-${i}" class="form-input" value="${spEsc(l.sku)}" readonly></div>
      <div class="form-group"><label class="form-label" for="sp-qty-${i}">Qty (open ${spEsc(l.open_qty)})</label>
      <input id="sp-qty-${i}" class="form-input num" type="number" min="0" step="any" value="${spEsc(l.open_qty)}" autocomplete="off"></div></div>`).join('')}
    <div class="form-group"><label class="form-label" for="sp-date">Expected</label><input id="sp-date" class="form-input" type="date"></div>
    <div class="form-group"><label class="form-label" for="sp-carrier">Carrier</label><input id="sp-carrier" class="form-input" autocomplete="off"></div>
    <div class="form-group"><label class="form-label" for="sp-track">Tracking</label><input id="sp-track" class="form-input" autocomplete="off"></div>
    <div class="form-actions"><button type="button" class="btn btn-outline" data-sp-tab="orders">Cancel</button>
      <button type="button" class="btn btn-primary" id="sp-send">Send notice</button></div></div>`;
}

async function spGet(path) {
  const res = await apiFetch(path);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'This could not be loaded.'); return null; }
  return res.json();
}

export async function renderSupplierPortalView(root) {
  const tab = SP_TABS.some(t => t[0] === currentViewTab) || currentViewTab === 'new-asn' ? currentViewTab : 'orders';
  currentViewTab = tab;
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  root.replaceChildren(panel);
  const data = await spGet(`/api/v1/supplier-portal/${tab === 'new-asn' ? 'orders' : tab}`);
  const title = (data && data.vendor) || 'Supplier portal';
  let body;
  if (!data) body = '<div class="sp-empty">Not available.</div>';
  else if (tab === 'new-asn') {
    spOrders = data.rows || [];
    const order = spOrders.find(o => o.id === spOrderId);
    body = order ? spNewASN(order) : '<div class="sp-empty">Pick an order first.</div>';
  } else body = spTable(tab, data.rows || []);
  panel.innerHTML = `<div class="page-header"><h1 class="page-title">${spEsc(title)}</h1></div>${tab === 'new-asn' ? '' : spTabs(tab)}${body}`;
  panel.addEventListener('click', e => {
    const btn = e.target.closest('[data-sp-tab],[data-sp-asn],#sp-send');
    if (!btn) return;
    if (btn.dataset.spTab) { renderView(`supplier-portal:${btn.dataset.spTab}`); return; }
    if (btn.dataset.spAsn) { spOrderId = btn.dataset.spAsn; renderView('supplier-portal:new-asn'); return; }
    const order = spOrders.find(o => o.id === spOrderId);
    const lines = (order ? order.lines : []).filter(l => Number(l.open_qty) > 0)
      .map((l, i) => ({ sku: l.sku, qty: Number(document.getElementById(`sp-qty-${i}`).value) || 0 })).filter(l => l.qty > 0);
    guardAgainstDoubleSubmit(btn, 'Sending...', async () => {
      const res = await apiFetch('/api/v1/supplier-portal/asn', { method: 'POST', body: JSON.stringify({
        po_id: spOrderId, expected_date: document.getElementById('sp-date').value, carrier: document.getElementById('sp-carrier').value.trim(),
        tracking_number: document.getElementById('sp-track').value.trim(), lines }) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'The notice was not sent.'); return; }
      showToast('Notice sent.', { variant: 'success' });
      renderView('supplier-portal:asns');
    });
  });
}
