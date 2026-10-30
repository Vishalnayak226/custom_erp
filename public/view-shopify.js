// Stage 99.2 (user, 2026-10-19: "Shopify Integration"; D1-D6 approved
// 2026-10-21): a Shopify channel's own page, opened from Setup ▸ PIM ▸
// Channel (the Shopify button on a Shopify channel's row).
//
// Connect (shop address, the custom app's admin token, its webhook secret,
// Test connection - which names any access the app is missing), map each
// Shopify location to a store (D3) with a safety buffer, the price list
// Shopify sells at, the sync switches, register the webhooks; then the
// day-to-day buttons: publish designs, link existing products, send prices
// or stock now, the stock mismatch report, pull orders, bring in payouts, and
// the recent runs. Everything goes through /api/v1/shopify/{channel}.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
// 49.8.5: a credential timestamp on one short line.
const shpWhen = (v) => v ? String(v).slice(0, 16).replace('T', ' ') : '—';
let lastCheck = null;

async function call(path, body) {
  const res = await apiFetch(path, body === undefined ? undefined : { method: 'POST', body: JSON.stringify(body) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'Shopify did not answer that.'); return null; }
  return res.json().catch(() => ({}));
}

export async function renderShopifyPage(root, channel) {
  if (!root) return;
  const base = `/api/v1/shopify/${encodeURIComponent(channel)}`;
  const [ov, storesRes, linksRes] = await Promise.all([
    call(base),
    apiFetch('/api/v1/doc/Location?limit=500').then(r => (r && r.ok ? r.json() : [])).catch(() => []),
    apiFetch(`${base}/links`).then(r => (r && r.ok ? r.json() : { links: [] })).catch(() => ({ links: [] }))
  ]);
  if (!ov) return;
  const stores = (storesRes || []).filter(s => (s.status || 'Active') === 'Active');
  const links = linksRes.links || [];
  const s = ov.settings || {};
  const locs = (lastCheck && lastCheck.channel === channel ? lastCheck.locations : null) || Object.keys(s.locations || {}).map(id => ({ id, name: id.split('/').pop() }));
  const storeOpts = (v) => `<option value="">— not used —</option>${stores.map(st => `<option value="${esc(st.id)}"${st.id === v ? ' selected' : ''}>${esc(st.name || st.id)}</option>`).join('')}`;
  const tick = (id, label, on) => `<label style="display:inline-flex;gap:6px;align-items:center;margin-right:16px;"><input type="checkbox" id="${id}"${on ? ' checked' : ''}> ${label}</label>`;
  root.innerHTML = `
    <div class="page-header"><div class="page-title-section"><h1 class="page-title">Shopify — ${esc(ov.name || channel)}</h1></div>
      <div><button type="button" class="btn btn-outline" id="shp-back">← Channels</button></div></div>
    <div class="table-panel" style="padding:16px 20px;margin-bottom:16px;">
      <h2 class="card-title">1. Connect</h2>
      <p class="text-muted" style="margin:4px 0 12px;">In Shopify admin: Settings → Apps and sales channels → Develop apps → create an app, give it the Admin API access listed under Test connection, install it, then copy its Admin API access token and its API secret key here.</p>
      <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">
        <div class="form-group"><label class="form-label" for="shp-domain">Shop address</label><input id="shp-domain" class="form-input" style="width:260px;" placeholder="yourstore.myshopify.com" value="${esc(ov.shop_domain || '')}"></div>
        <div class="form-group"><label class="form-label" for="shp-token">Admin API access token</label><input id="shp-token" type="password" class="form-input" style="width:240px;" autocomplete="off" placeholder="${ov.credentials_set ? 'saved - type to replace' : 'shpat_...'}"></div>
        <div class="form-group"><label class="form-label" for="shp-secret">Webhook secret (API secret key)</label><input id="shp-secret" type="password" class="form-input" style="width:220px;" autocomplete="off" placeholder="${ov.webhook_secret_set ? 'saved - type to replace' : 'required for orders'}"></div>
        <button type="button" class="btn btn-primary" id="shp-save-cred">Save</button>
        <button type="button" class="btn btn-outline" id="shp-check"${ov.credentials_set ? '' : ' disabled'}>Test connection</button>
        ${ov.credentials_set ? '<button type="button" class="btn btn-outline" id="shp-disconnect">Disconnect</button>' : ''}
      </div>
      ${ov.credentials_set ? `<div class="text-muted" style="margin-top:8px;">Owner ${esc((ov.credential || {}).owner || '—')} · Last used ${esc(shpWhen((ov.credential || {}).last_used_at))} · Expires ${(ov.credential || {}).expired ? '<span class="badge badge-danger">Expired</span>' : esc(shpWhen((ov.credential || {}).expires_at))}</div>` : ''}
      <div id="shp-check-out" style="margin-top:10px;">${s.shop ? `<span class="badge badge-success">Connected to ${esc(s.shop)}</span>` : ''}</div>
    </div>
    <div class="table-panel" style="padding:16px 20px;margin-bottom:16px;">
      <h2 class="card-title">2. Stores, prices and what syncs</h2>
      <p class="text-muted" style="margin:4px 0 12px;">Each Shopify location sells the mapped store's stock (less the safety buffer, never below 0); an online order ships from the store mapped to the location Shopify chose. Test connection lists the shop's locations.</p>
      <table class="dash-table" style="width:auto;min-width:420px;"><thead><tr><th>Shopify location</th><th>Store</th></tr></thead>
        <tbody>${locs.length ? locs.map(l => `<tr><td>${esc(l.name)}</td><td><select class="form-select" data-shp-loc="${esc(l.id)}">${storeOpts((s.locations || {})[l.id])}</select></td></tr>`).join('') : '<tr><td colspan="2" class="text-muted">Test the connection to list the shop\'s locations.</td></tr>'}</tbody></table>
      <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-top:12px;">
        <div class="form-group"><label class="form-label" for="shp-buffer">Safety buffer (units kept back)</label><input id="shp-buffer" type="number" min="0" class="form-input" style="width:120px;" value="${esc(ov.inventory_buffer ?? 0)}"></div>
        <div class="form-group"><label class="form-label" for="shp-pricelist">Price list Shopify sells at</label><input id="shp-pricelist" class="form-input" style="width:200px;" placeholder="blank = the item's sale price" value="${esc(s.price_list || '')}"></div>
      </div>
      <div style="margin:10px 0;">${tick('shp-sync-prices', 'Send prices', s.sync_prices !== false)}${tick('shp-sync-stock', 'Send stock', s.sync_stock !== false)}${tick('shp-sync-orders', 'Take orders in', s.sync_orders !== false)}</div>
      <div style="display:flex;"><button type="button" class="btn btn-primary" id="shp-save-settings">Save</button></div>
    </div>
    <div class="table-panel" style="padding:16px 20px;margin-bottom:16px;">
      <h2 class="card-title">3. Webhooks</h2>
      <p class="text-muted" style="margin:4px 0 8px;">Shopify tells the ERP at once about new, changed and cancelled orders, refunds and fulfilments here; a pull every sync interval catches anything missed.</p>
      <div style="font-family:monospace;font-size:12.5px;word-break:break-all;margin-bottom:8px;">${esc(s.webhook_url || '')}</div>
      <div>${s.webhooks_registered_at ? `<span class="badge badge-success">Registered ${esc(s.webhooks_registered_at.slice(0, 16).replace('T', ' '))}</span> ` : ''}
        <button type="button" class="btn btn-outline" id="shp-webhooks"${ov.credentials_set ? '' : ' disabled'}>${s.webhooks_registered_at ? 'Register again' : 'Register'}</button></div>
    </div>
    <div class="table-panel" style="padding:16px 20px;margin-bottom:16px;">
      <h2 class="card-title">4. Products, prices and stock</h2>
      <p class="text-muted" style="margin:4px 0 12px;">${ov.links} item(s) in ${ov.products} Shopify product(s). A design goes as one product with its sizes / colours as variants, MRP as the compare-at price; publishing again updates it, an inactive item drops out, an inactive design is archived. The ERP's prices and stock win: a change made in Shopify admin is overwritten at the next send.</p>
      <div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px;">
        <button type="button" class="btn btn-primary" id="shp-publish">Publish designs not on Shopify yet</button>
        <input id="shp-design" class="form-input" style="width:200px;" placeholder="or a design / item code">
        <button type="button" class="btn btn-outline" id="shp-publish-one">Publish it</button>
        <button type="button" class="btn btn-outline" id="shp-link">Link existing Shopify products (by SKU / barcode)</button>
      </div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;">
        <button type="button" class="btn btn-outline" id="shp-prices">Send changed prices</button>
        <button type="button" class="btn btn-outline" id="shp-prices-all">Send all prices</button>
        <button type="button" class="btn btn-outline" id="shp-stock">Send changed stock</button>
        <button type="button" class="btn btn-outline" id="shp-stock-all">Send all stock</button>
        <button type="button" class="btn btn-outline" id="shp-mismatch">Stock mismatch report</button>
      </div>
      <div id="shp-out" style="margin-top:10px;"></div>
      <details style="margin-top:10px;"><summary style="cursor:pointer;">Linked items (${links.length})</summary>
        <div class="dash-table" style="max-height:320px;overflow:auto;margin-top:8px;"><table><thead><tr><th>Combination ID</th><th>Item</th><th>Design</th><th class="num">Price sent</th><th class="num">MRP sent</th><th>Stock sent</th></tr></thead>
          <tbody>${links.map(l => `<tr><td>${esc(l.sku)}</td><td>${esc(l.name)}</td><td>${esc(l.design)}</td><td class="num">${esc(l.last_price || '')}</td><td class="num">${esc(l.last_compare_at || '')}</td>
            <td>${esc(Object.values(l.last_stock || {}).join(' / '))}</td></tr>`).join('') || '<tr><td colspan="6" class="text-muted">Nothing linked yet.</td></tr>'}</tbody></table></div></details>
    </div>
    <div class="table-panel" style="padding:16px 20px;margin-bottom:16px;">
      <h2 class="card-title">5. Orders and payouts</h2>
      <p class="text-muted" style="margin:4px 0 12px;">Orders come in whole - number, discounts, shipping, GST, payment and COD, the customer (found by mobile number or added). An edit or cancellation in Shopify is applied until picking starts; after that it waits in OMS → <b>Channel changes</b>${ov.channel_changes ? ` (<b>${ov.channel_changes}</b> waiting)` : ''}. Shipping, cancellation and RTO go back to Shopify; a refund there raises a return here. Shopify Payments payouts come in once a day as settlement lines and are reconciled with the orders.</p>
      <button type="button" class="btn btn-outline" id="shp-pull">Pull orders now</button>
      <button type="button" class="btn btn-outline" id="shp-payouts">Bring in payouts now</button>
    </div>
    <div class="table-panel" style="padding:16px 20px;">
      <h2 class="card-title">Recent runs</h2>
      <div class="dash-table"><table><thead><tr><th>When</th><th>What</th><th>Result</th><th class="num">Done</th><th class="num">Failed</th><th>Error</th></tr></thead>
        <tbody>${(ov.runs || []).map(r => `<tr><td>${esc(r.at)}</td><td>${esc(r.operation)}</td><td>${esc(r.status)}</td><td class="num">${r.processed}</td><td class="num">${r.failed}</td><td title="${esc(r.error)}">${esc((r.error || '').slice(0, 80))}</td></tr>`).join('') || '<tr><td colspan="6" class="text-muted">Nothing has run yet.</td></tr>'}</tbody></table></div>
    </div>`;
  await loadViewStyles('dashboards.css'); // the compact .dash-table
  const $ = (id) => root.querySelector(`#${id}`);
  const out = (html) => { $('shp-out').innerHTML = html; };
  const busy = async (btn, fn) => { btn.disabled = true; try { await fn(); } finally { btn.disabled = false; } };
  const again = () => renderShopifyPage(root, channel);

  $('shp-back').addEventListener('click', () => window.openSetupDoctype('Channel'));
  $('shp-save-cred').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    const r = await call(`${base}/credentials`, { shop_domain: $('shp-domain').value, access_token: $('shp-token').value, webhook_secret: $('shp-secret').value });
    if (r) { showToast('Saved. Test the connection next.', { variant: 'success' }); again(); }
  }));
  // 49.8.5: Disconnect removes the token and cancels queued syncs; links and settings stay.
  $('shp-disconnect')?.addEventListener('click', (e) => busy(e.currentTarget, async () => {
    if (!await showCustomConfirm('Disconnect this shop? The saved token is removed and queued syncs are cancelled; product links and settings stay.', 'Disconnect')) return;
    const r = await call('/api/v1/integrations/credentials/disconnect', { kind: 'channel', code: channel });
    if (r) { showToast('Disconnected.', { variant: 'success' }); again(); }
  }));
  $('shp-check').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    const r = await call(`${base}/do/check`, {});
    if (!r) return;
    lastCheck = { channel, locations: r.locations || [] };
    showToast(r.missing_scopes?.length ? 'Connected, but the app is missing some access.' : `Connected to ${r.shop}.`, { variant: r.missing_scopes?.length ? 'warning' : 'success' });
    await again();
    $('shp-check-out').innerHTML = `<span class="badge badge-success">Connected to ${esc(r.shop)} (${esc(r.domain)})</span> ${(r.locations || []).length} location(s).
      ${r.missing_scopes?.length ? `<div class="login-error" style="margin-top:8px;">The app is missing: <b>${esc(r.missing_scopes.join(', '))}</b>. Add them under the app's Configuration → Admin API access scopes, save, reinstall, and test again.</div>` : ''}
      ${r.payouts ? '' : '<div class="text-muted" style="margin-top:6px;">Payouts: give the app read_shopify_payments_payouts to bring Shopify Payments payouts in.</div>'}`;
  }));
  $('shp-save-settings').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    const locations = {};
    root.querySelectorAll('[data-shp-loc]').forEach(sel => { if (sel.value) locations[sel.dataset.shpLoc] = sel.value; });
    const r = await call(`${base}/settings`, { locations, price_list: $('shp-pricelist').value, inventory_buffer: Number($('shp-buffer').value) || 0,
      sync_products: true, sync_prices: $('shp-sync-prices').checked, sync_stock: $('shp-sync-stock').checked, sync_orders: $('shp-sync-orders').checked });
    if (r) { showToast('Saved.', { variant: 'success' }); again(); }
  }));
  $('shp-webhooks').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    const r = await call(`${base}/do/webhooks`, {});
    if (r) { showToast(`Webhooks registered: ${(r.topics || []).length}.`, { variant: 'success' }); again(); }
  }));
  const publish = async (btn, codes) => busy(btn, async () => {
    out('<p class="text-muted">Publishing… (a big batch takes a minute)</p>');
    const r = await call(`${base}/do/publish`, { codes });
    if (!r) { out(''); return; }
    out(`<p>${r.products} product(s), ${r.variants} variant(s) sent${r.archived ? `, ${r.archived} archived` : ''}.${r.left ? ` <b>${r.left}</b> more to go - click again.` : ''}</p>
      ${(r.errors || []).length ? `<div class="login-error">${r.errors.map(esc).join('<br>')}</div>` : ''}`);
  });
  $('shp-publish').addEventListener('click', (e) => publish(e.currentTarget, []));
  $('shp-publish-one').addEventListener('click', (e) => { const c = $('shp-design').value.trim(); if (c) publish(e.currentTarget, [c]); });
  $('shp-link').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    const r = await call(`${base}/do/link`, {});
    if (r) out(`<p>${r.linked} Shopify variant(s) linked by SKU or barcode; ${r.unmatched} not found in the ERP.</p>`);
  }));
  const send = (path, body, label) => (e) => busy(e.currentTarget, async () => {
    const r = await call(`${base}/do/${path}`, body);
    if (r) out(`<p>${label}: ${r.sent ?? r.processed ?? r.lines ?? 0}${r.failed ? ` (${r.failed} failed)` : ''}.</p>`);
  });
  $('shp-prices').addEventListener('click', send('prices', {}, 'Prices sent'));
  $('shp-prices-all').addEventListener('click', send('prices', { full: true }, 'Prices sent'));
  $('shp-stock').addEventListener('click', send('stock', { full: false, skus: [] }, 'Stock quantities sent'));
  $('shp-stock-all').addEventListener('click', send('stock', { full: true }, 'Stock quantities sent'));
  $('shp-pull').addEventListener('click', send('pull', {}, 'Orders taken in or updated'));
  $('shp-payouts').addEventListener('click', send('payouts', {}, 'Payout lines brought in'));
  $('shp-mismatch').addEventListener('click', (e) => busy(e.currentTarget, async () => {
    out('<p class="text-muted">Reading Shopify…</p>');
    const r = await call(`${base}/mismatch`);
    if (!r) { out(''); return; }
    const rows = r.rows || [];
    const storeName = (code) => (stores.find(s => s.id === code) || {}).name || code;
    out(rows.length ? `<div class="dash-table" style="max-height:340px;overflow:auto;"><table><thead><tr><th><input type="checkbox" id="shp-mm-all" aria-label="All"></th><th>Combination ID</th><th>Item</th><th>Store</th><th class="num">ERP sends</th><th class="num">Shopify shows</th></tr></thead>
      <tbody>${rows.map(m => `<tr><td><input type="checkbox" data-shp-mm="${esc(m.sku)}" aria-label="Send ${esc(m.sku)}"></td><td>${esc(m.sku)}</td><td>${esc(m.name)}</td><td>${esc(storeName(m.store))}</td><td class="num">${m.erp}</td><td class="num">${m.shopify}</td></tr>`).join('')}</tbody></table></div>
      <button type="button" class="btn btn-primary btn-sm" id="shp-mm-send" style="margin-top:8px;">Send now</button>`
      : '<p>Shopify shows what the ERP would send, everywhere.</p>');
    root.querySelector('#shp-mm-all')?.addEventListener('change', (ev) => root.querySelectorAll('[data-shp-mm]').forEach(c => { c.checked = ev.target.checked; }));
    root.querySelector('#shp-mm-send')?.addEventListener('click', async (ev) => {
      const skus = [...new Set([...root.querySelectorAll('[data-shp-mm]:checked')].map(c => c.dataset.shpMm))];
      if (!skus.length) { showToast('Tick the items to send.', { variant: 'info' }); return; }
      await busy(ev.currentTarget, async () => {
        const res = await call(`${base}/do/stock`, { skus });
        if (res) showToast(`Sent ${res.sent} quantity(ies).`, { variant: 'success' });
      });
    });
  }));
}
