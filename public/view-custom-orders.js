// Stage 148.36: custom-order configurator (module custom_orders). Opened by New
// order on the Custom order quotes list. Pick the choices, see every part of the
// price at today's rate, then make the item and a Draft sales order. Shared
// services (apiFetch, showApiError, showToast, guardAgainstDoubleSubmit,
// renderView, escapeHTMLText) are globals.

const esc = (s) => escapeHTMLText(String(s ?? ''));
const money = (n) => (typeof formatMoney === 'function' ? formatMoney(n) : Number(n).toFixed(2));
const KINDS = [['metal', 'Metal', 'Metal', true], ['purity', 'Purity', 'Purity', true], ['stone', 'Stone', 'Stone', false],
  ['size', 'Size', 'Size', false], ['making_type', 'Making type', 'Making type', true]];

function select(id, kind, label, required, choices) {
  const opts = (required ? [] : [{ value: '' }]).concat(choices[kind] || []);
  return `<div class="form-group"><label class="form-label" for="co-${id}">${esc(label)}</label>
    <select id="co-${id}" class="form-select">${opts.map(o => `<option value="${esc(o.value)}">${esc(o.value)}</option>`).join('')}</select></div>`;
}

function input(id, label, type = 'text') {
  return `<div class="form-group"><label class="form-label" for="co-${id}">${esc(label)}</label>
    <input id="co-${id}" class="form-input${type === 'number' ? ' num' : ''}" type="${type}" ${type === 'number' ? 'min="0" step="any"' : ''} autocomplete="off"></div>`;
}

async function post(path, body, failText, commandKey) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(body), ...(commandKey ? { commandKey } : {}) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

export async function openCustomOrder() {
  const res = await apiFetch('/api/v1/sales/custom-orders/choices');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the choices.'); return; }
  const choices = await res.json();

  document.getElementById('co-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'co-modal';
  overlay.innerHTML = `
    <div class="modal-container s-daf2eb" role="dialog" aria-modal="true" aria-labelledby="co-title">
      <div class="modal-header"><h3 class="modal-title" id="co-title">New custom order</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-co-close>&times;</button></div>
      <div class="modal-body s-d1b80a">
        <div class="s-1d9e83">
          ${KINDS.map(([id, kind, label, req]) => select(id, kind, label, req, choices)).join('')}
          ${input('weight', 'Weight (g)', 'number')}${input('stones', 'Stones', 'number')}
        </div>
        <div class="s-1d9e83">
          ${input('customer', 'Customer')}${input('phone', 'Phone')}${input('address', 'Address')}
          <div id="co-price" class="table-wrapper"></div>
        </div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-co-close>Close</button>
        <button type="button" class="btn btn-outline" data-co-price>Price</button>
        <button type="button" class="btn btn-primary" data-co-save>Make order</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const q = (id) => overlay.querySelector(`#co-${id}`);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-co-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });

  const config = () => ({
    metal: q('metal').value, purity: q('purity').value, stone: q('stone').value, size: q('size').value, making_type: q('making_type').value,
    net_weight: Number(q('weight').value) || 0, stone_count: Number(q('stones').value) || 0,
  });
  const drawPrice = (p) => {
    q('price').innerHTML = `<table><tbody>${p.lines.map(l => `<tr><td>${esc(l.part)}</td><td>${esc(l.label)}</td><td class="num">${esc(money(l.amount))}</td></tr>`).join('')}
      <tr><td colspan="2"><strong>Total incl. GST</strong></td><td class="num"><strong>${esc(money(p.total))}</strong></td></tr>${p.gst_amount ? `<tr><td colspan="2">GST ${esc(p.gst_rate)}%</td><td class="num">${esc(money(p.gst_amount))}</td></tr>` : ''}</tbody></table>`;
  };

  const priceBtn = overlay.querySelector('[data-co-price]');
  priceBtn.addEventListener('click', () => guardAgainstDoubleSubmit(priceBtn, 'Pricing...', async () => {
    const p = await post('/api/v1/sales/custom-orders/quote', config(), 'Could not price this.');
    if (p) drawPrice(p);
  }));
  const saveBtn = overlay.querySelector('[data-co-save]');
  saveBtn.addEventListener('click', () => guardAgainstDoubleSubmit(saveBtn, 'Saving...', async () => {
    const out = await post('/api/v1/sales/custom-orders', {
      ...config(), customer_name: q('customer').value.trim(), customer_phone: q('phone').value.trim(), shipping_address: q('address').value.trim(),
    }, 'The order was not made.', 'custom-order');
    if (!out) return;
    showToast(`Draft order ${out.order_number || ''} made.`, { variant: 'success' });
    close();
    renderView('doctype-table');
  }));
}
