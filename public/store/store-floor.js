// Stage 102.4: the selling floor - a basket for the till (102.4.2) and an
// order from another store for home delivery (102.4.3). Loaded by store.js.
//
// The basket is a held bill (Stage 73.2.5): the till lists it under More... >
// Resume a held bill, where the cashier picks it or types / scans its number,
// and prices it again when it is resumed. The order is an OMS order made by the
// Order Engine (POST /api/v1/store-app/orders).

const BASKET = 'store_app_basket';
const $$ = (root, sel) => root.querySelector(sel);

function loadBasket(ls) {
  const b = ls.json(BASKET, null);
  return b && Array.isArray(b.lines) ? b : { lines: [], customer: '' };
}

async function barcodeSVG(text) {
  try {
    const { encodeCode128 } = await import('/sticker-engine.js?v=2');
    const modules = encodeCode128(text);
    if (!modules) return '';
    const w = modules.length + 20;
    let x = 10;
    let bars = '';
    for (let i = 0; i < modules.length; i++) {
      if (modules[i]) bars += `<rect x="${x}" y="0" width="1" height="60"/>`;
      x++;
    }
    return `<svg class="barcode" viewBox="0 0 ${w} 60" preserveAspectRatio="none" role="img" aria-label="Barcode ${text}">${bars}</svg>`;
  } catch (_) {
    return '';
  }
}

async function renderBasket(ctx, params) {
  const { esc, qty, money, main, ls, toast } = ctx;
  const basket = loadBasket(ls);
  const save = () => ls.setJSON(BASKET, basket);
  const add = async (code) => {
    let item;
    try { item = await ctx.lookupItem(code); } catch (e) {
      if (!e.quiet) toast(e.message, true);
      return;
    }
    const existing = basket.lines.find(l => l.sku === item.code);
    if (existing) existing.qty += 1;
    else basket.lines.push({ sku: item.code, name: item.name, qty: 1, price: item.price || 0, currency: item.currency });
    save();
    draw();
    if (navigator.vibrate) navigator.vibrate(30);
  };

  main.innerHTML = `
    <div id="bk-scan"></div>
    <ul class="rows count-rows" id="bk-lines"></ul>
    <div id="bk-total" class="toolbar"></div>
    <label class="field"><span>Customer (name or phone, optional - the till finds them again)</span>
      <input class="input" id="bk-customer" maxlength="80" value="${esc(basket.customer || '')}"></label>
    <button type="button" class="btn btn-primary btn-block" id="bk-send">Send to the till</button>
    <div class="links"><button type="button" class="link" id="bk-clear">Empty the basket</button></div>`;
  const draw = () => {
    const cur = (basket.lines[0] || {}).currency;
    $$(main, '#bk-lines').innerHTML = basket.lines.length ? basket.lines.map((l, i) => `<li class="count-row" data-i="${i}">
      <span>${esc(l.name)}<small>${esc(l.sku)}${l.price ? ` · ${esc(money(l.price, l.currency))}` : ''}</small></span>
      <span class="stepper"><button type="button" class="btn btn-small" data-step="-1" aria-label="One less">−</button>
        <input class="input qty-input" type="number" inputmode="numeric" min="0" value="${l.qty}" aria-label="Quantity">
        <button type="button" class="btn btn-small" data-step="1" aria-label="One more">+</button></span></li>`).join('')
      : '<li class="empty">Scan what the customer wants.</li>';
    const total = basket.lines.reduce((a, l) => a + l.qty * (l.price || 0), 0);
    $$(main, '#bk-total').innerHTML = basket.lines.length ? `<span>${qty(basket.lines.reduce((a, l) => a + l.qty, 0))} item(s)</span><span>About ${esc(money(total, cur))} - the till prices it</span>` : '';
    $$(main, '#bk-lines').querySelectorAll('.count-row').forEach(row => {
      const l = basket.lines[Number(row.dataset.i)];
      const set = (v) => {
        if (v <= 0) basket.lines.splice(Number(row.dataset.i), 1); else l.qty = v;
        save(); draw();
      };
      row.querySelectorAll('[data-step]').forEach(b => b.addEventListener('click', () => set(l.qty + Number(b.dataset.step))));
      row.querySelector('input').addEventListener('change', (e) => set(Math.max(0, Math.round(Number(e.target.value) || 0))));
    });
  };
  ctx.scanBox($$(main, '#bk-scan'), add);
  $$(main, '#bk-customer').addEventListener('input', (e) => { basket.customer = e.target.value; save(); });
  $$(main, '#bk-clear').addEventListener('click', () => { basket.lines = []; basket.customer = ''; save(); renderBasket(ctx, new URLSearchParams()); });
  $$(main, '#bk-send').addEventListener('click', async (e) => {
    if (!basket.lines.length) { toast('The basket is empty.', true); return; }
    const btn = e.currentTarget;
    btn.disabled = true;
    const store = ctx.workingStore();
    // The number is the bill's identity at the till; kept with the basket so
    // a retry after a lost answer cannot make a second held bill.
    basket.number = basket.number || `PB-${store}-${Date.now().toString(36).toUpperCase()}`;
    save();
    const who = (ctx.state.profile && ctx.state.profile.username) || ls.get('erp_username');
    try {
      const bill = await ctx.api('/api/v1/pos/held-bills', {
        method: 'POST',
        body: {
          cart_number: basket.number, location: store,
          note: ['Phone basket', basket.customer && basket.customer.trim(), who && `by ${who}`].filter(Boolean).join(' · '),
          total: Math.round(basket.lines.reduce((a, l) => a + l.qty * (l.price || 0), 0) * 100) / 100,
          lines: basket.lines.map(l => ({ sku: l.sku, name: l.name, qty: l.qty })),
        },
      });
      ls.del(BASKET);
      showBasketSent(ctx, bill);
    } catch (ex) {
      btn.disabled = false;
      if (/already held/i.test(ex.message || '')) { ls.del(BASKET); showBasketSent(ctx, { cart_number: basket.number, line_count: basket.lines.length }); return; }
      if (!ex.quiet) toast(ex.message, true);
    }
  });
  draw();
  const code = params.get('code');
  if (code) {
    history.replaceState(null, '', '#/basket');
    add(code);
  }
}

async function showBasketSent(ctx, bill) {
  const { esc, main, setBar } = ctx;
  setBar('Basket sent', true);
  const short = String(bill.cart_number).split('-').pop();
  main.innerHTML = `
    <article class="card basket-ticket">
      <h2>Basket ${esc(short)}</h2>
      <div class="sub">Send the customer to the till with this number.</div>
      <div id="bk-code"></div>
      <div class="basket-number">${esc(bill.cart_number)}</div>
    </article>
    <p class="hint">At the till: More... > Resume a held bill, then pick it, or type or scan ${esc(short)} in Find a basket. The till prices it again when it is resumed.</p>
    <a class="btn btn-primary btn-block" href="#/basket">New basket</a>`;
  $$(main, '#bk-code').innerHTML = await barcodeSVG(bill.cart_number);
}

async function renderOrder(ctx, params) {
  const { esc, qty, money, main, panelError, toast, ls } = ctx;
  const code = params.get('code') || '';
  if (!code) { location.hash = '#/scan'; return; }
  main.innerHTML = '<p class="loading">Loading…</p>';
  let item;
  try { item = await ctx.lookupItem(code); } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderOrder(ctx, params));
    return;
  }
  const keyName = `store_app_order_key_${item.code}`;
  const others = (item.stock || []).filter(s => !s.working && s.available > 0);
  main.innerHTML = `<form class="card" id="ord-form" autocomplete="on">
    <h2>${esc(item.name)}</h2><div class="sub">${esc(item.code)}${item.price ? ` · ${esc(money(item.price, item.currency))}` : ''}</div>
    <label class="field"><span>How many</span><input class="input qty-input" id="ord-qty" type="number" inputmode="numeric" min="1" value="1" required></label>
    <label class="field"><span>Ship from</span><select class="input" id="ord-from">
      <option value="">Let the order desk choose</option>
      ${others.map(s => `<option value="${esc(s.store)}">${esc(s.name)} · ${qty(s.available)} available</option>`).join('')}
    </select></label>
    ${others.length ? '' : '<p class="hint">No other store you can see has it free; the order desk sources it (another store or the warehouse).</p>'}
    <h3 class="section-title">Deliver to</h3>
    <label class="field"><span>Customer name</span><input class="input" id="ord-name" autocomplete="name" required></label>
    <label class="field"><span>Phone</span><input class="input" id="ord-phone" type="tel" inputmode="tel" autocomplete="tel" required></label>
    <label class="field"><span>Address</span><input class="input" id="ord-addr" autocomplete="address-line1" required></label>
    <label class="field"><span>Address line 2 (optional)</span><input class="input" id="ord-addr2" autocomplete="address-line2"></label>
    <div class="field-row">
      <label class="field"><span>City</span><input class="input" id="ord-city" autocomplete="address-level2"></label>
      <label class="field"><span>PIN code</span><input class="input" id="ord-pin" inputmode="numeric" autocomplete="postal-code" required></label>
    </div>
    <label class="field"><span>State</span><input class="input" id="ord-state" autocomplete="address-level1"></label>
    <label class="field"><span>Payment</span><select class="input" id="ord-pay">
      <option value="cod">Customer pays on delivery</option><option value="paid">Already paid</option></select></label>
    <label class="field"><span>Note for the order desk (optional)</span><input class="input" id="ord-note" maxlength="300" placeholder="e.g. size M, gift wrap"></label>
    <div class="actions"><a class="btn" href="#/scan?code=${encodeURIComponent(code)}">Cancel</a><button type="submit" class="btn btn-primary">Book the order</button></div></form>`;
  $$(main, '#ord-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.submitter || $$(main, '#ord-form button[type="submit"]');
    btn.disabled = true;
    const key = ls.get(keyName) || ctx.newKey();
    ls.set(keyName, key);
    const v = (id) => $$(main, id).value.trim();
    try {
      const res = await ctx.api('/api/v1/store-app/orders', {
        method: 'POST',
        body: {
          request_key: key, customer_name: v('#ord-name'), customer_phone: v('#ord-phone'), address: v('#ord-addr'),
          address_line2: v('#ord-addr2'), city: v('#ord-city'), state: v('#ord-state'), pincode: v('#ord-pin'),
          payment: v('#ord-pay'), from_store: v('#ord-from'), note: v('#ord-note'),
          lines: [{ code: item.code, qty: Math.round(Number(v('#ord-qty')) || 0) }],
        },
      });
      ls.del(keyName);
      ctx.setBar('Order booked', true);
      main.innerHTML = `<div class="msg ${res.hold_reason ? 'msg-error' : 'msg-info'}" role="status">${esc(res.message)}</div>
        <article class="card"><h2>${esc(res.order_id)}</h2><div class="sub">${esc(item.name)} · ${esc(money(res.total, item.currency))}${res.from_store ? ` · ships from ${esc(ctx.storeName(res.from_store))}` : ''}</div></article>
        <p class="hint">It is on the order desk now (OMS > Pick, Pack, Ship), like any other order.</p>
        <a class="btn btn-block" href="#/home">Done</a>`;
    } catch (ex) {
      btn.disabled = false;
      if (!ex.quiet) toast(ex.message, true);
    }
  });
}

export const routes = {
  basket: { title: 'Basket for the till', render: renderBasket },
  order: { title: 'Order for home delivery', render: renderOrder },
};
