// Stage 102.2.1 / 102.1.3: the manager's Today and the store's low stock.
// Loaded by store.js the first time either is opened.

function pct(n) {
  if (n === undefined || n === null) return '';
  const v = Number(n);
  return `${v > 0 ? '+' : ''}${v.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;
}

async function renderToday(ctx, params) {
  const { api, esc, money, qty, main, panelError, state } = ctx;
  const which = params.get('store') || '';
  main.innerHTML = '<p class="loading">Loading…</p>';
  let t;
  try {
    t = await api(`/api/v1/store-app/today${which ? `?store=${encodeURIComponent(which)}` : ''}`);
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderToday(ctx, params));
    return;
  }
  if (ctx.routeName() !== 'today') return;
  const many = ((state.stores && state.stores.stores) || []).length > 1 || (state.stores && state.stores.all);
  const cur = t.currency;
  const change = t.change_pct !== undefined && t.change_pct !== null
    ? `<span class="trend ${t.change_pct >= 0 ? 'up' : 'down'}">${esc(pct(t.change_pct))} on last ${esc(new Date(t.date).toLocaleDateString(undefined, { weekday: 'long' }))}</span>`
    : (t.last_week.bills ? '' : '<span class="trend">No sales the same day last week</span>');
  main.innerHTML = `
    <div class="toolbar"><span>${esc(t.label)} · up to ${esc(t.as_of)}</span>
      ${many ? `<a class="link" href="#/today${which === 'all' ? '' : '?store=all'}">${which === 'all' ? 'This store' : 'All my stores'}</a>` : ''}</div>
    <div class="figures">
      <div class="figure wide"><span>Sales</span><strong>${esc(money(t.today.sales, cur))}</strong>${change}
        <small>Same time last week: ${esc(money(t.last_week.sales, cur))}</small></div>
      <div class="figure"><span>Bills</span><strong>${qty(t.today.bills)}</strong><small>Last week ${qty(t.last_week.bills)}</small></div>
      <div class="figure"><span>Average bill</span><strong>${esc(money(t.today.avg_bill, cur))}</strong><small>Last week ${esc(money(t.last_week.avg_bill, cur))}</small></div>
      <div class="figure"><span>Items per bill</span><strong>${qty(t.today.items_per_bill)}</strong><small>${qty(t.today.units)} items sold</small></div>
      <div class="figure"><span>Returns</span><strong>${qty(t.returns)}</strong><small>${t.refunds ? `${esc(money(t.refunds, cur))} to refund` : 'None today'}</small></div>
    </div>
    ${t.target_note ? `<p class="hint">${esc(t.target_note)}</p>` : ''}
    <h3 class="section-title">Top items today</h3>
    ${t.top_items.length ? `<ul class="rows">${t.top_items.map(i => `<li><span>${esc(i.name)}<small>${esc(i.sku)} · ${qty(i.qty)} sold</small></span><span class="qty">${esc(money(i.amount, cur))}</span></li>`).join('')}</ul>`
      : '<p class="empty">Nothing sold yet today.</p>'}
    <h3 class="section-title">Salesmen</h3>
    ${t.salesmen.length ? `<ol class="rows ranked">${t.salesmen.map(s => `<li><span>${esc(s.name)}<small>${qty(s.bills)} bill${s.bills === 1 ? '' : 's'}</small></span><span class="qty">${esc(money(s.amount, cur))}</span></li>`).join('')}</ol>`
      : '<p class="empty">No bills yet today.</p>'}
    <div class="links"><button type="button" class="link" id="today-refresh">Refresh</button></div>`;
  ctx.main.querySelector('#today-refresh').addEventListener('click', () => renderToday(ctx, params));
}

async function renderLowStock(ctx) {
  const { api, esc, qty, main, panelError } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let res;
  try {
    res = await api('/api/v1/store-app/low-stock');
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderLowStock(ctx));
    return;
  }
  if (ctx.routeName() !== 'low-stock') return;
  const items = res.items || [];
  main.innerHTML = items.length ? `
    <p class="hint">Items at or under their safety stock here, or with none left. Tap one to see where else it is.</p>
    <ul class="rows">${items.map(i => `<li><a class="row-link" href="#/scan?code=${encodeURIComponent(i.sku)}"><span>${esc(i.name)}<small>${esc(i.sku)}${i.safety ? ` · keep at least ${qty(i.safety)}` : ''}</small></span>
      <span class="qty${i.free > 0 ? '' : ' zero'}">${i.free > 0 ? `${qty(i.free)} left` : 'None left'}</span></a></li>`).join('')}</ul>`
    : '<p class="empty">Nothing is running low at this store.</p>';
}

export const routes = {
  today: { title: 'Today', render: renderToday },
  'low-stock': { title: 'Low stock', render: renderLowStock },
};
