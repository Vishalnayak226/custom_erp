// Ask 215 (215.12, user 2026-11-15: "There should an sales history page. To
// see sales and sales return."): POS > Sales History. Store bills, online
// bills and returns in one list, newest first, between two dates and/or by
// anything the customer brings - bill, order, AWB, mobile, GSTIN. The three
// discounts (item, offer, bill) are their own columns and totals. A bill
// opens as its own page; Return takes it to the till's Return tab already
// loaded (215.13), so an online bill is returned at the store in one click.

const V = new URL(import.meta.url).search;
const esc = (s) => window.escapeHTMLText(s == null ? '' : String(s));
const money = (v) => (typeof window.formatMoney === 'function' ? window.formatMoney(Number(v) || 0) : (Number(v) || 0).toFixed(2));
const SCREEN = 'slh-screen';
const PAGE = 50;

const isoDay = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
// "2026-11-16 14:05" from the server, shown as 16-11-2026 14:05 like the
// other POS lists.
const shown = (s) => {
  const m = /^(\d{4})-(\d{2})-(\d{2})(.*)$/.exec(s || '');
  return m ? `${m[3]}-${m[2]}-${m[1]}${m[4]}` : (s || '');
};

const slh = { q: '', from: '', to: '', kind: '', source: '', scope: 'mine', offset: 0 };
let slhRows = [];
let slhOpen = null;
let slhLoad = 0;
let slhTotals = { key: '', h: null };

function statusBadge(s) {
  const ok = ['Paid', 'Posted', 'Completed', 'Refunded', 'Closed', 'Delivered'];
  const bad = ['Rejected', 'Cancelled', 'Void'];
  const cls = ok.includes(s) ? 'badge-success' : bad.includes(s) ? 'badge-danger' : 'badge-warning';
  return s ? `<span class="badge ${cls}">${esc(s)}</span>` : '';
}

function typeCell(r) {
  return `${r.kind === 'Return' ? 'Return' : 'Sale'}${r.source === 'Online' ? `<div class="text-muted s-11fcdc">Online${r.channel ? ' · ' + esc(r.channel) : ''}</div>` : ''}`;
}

function refCell(r) {
  if (r.kind === 'Return') return r.against || r.against_id ? `Against ${esc(r.against || r.against_id)}` : '';
  const order = r.channel_order_id || r.order_no;
  return `${esc(order || '')}${r.awb ? `<div class="text-muted s-11fcdc">AWB ${esc(r.awb)}</div>` : ''}`;
}

const disc = (v) => (Number(v) ? esc(money(v)) : '');

export async function renderSalesHistoryView(container) {
  const { txnPageTitle, txnRestorePane, txnPageOf } = await import('./txn-screen.js' + V);
  if (!slh.from) {
    const today = new Date();
    const week = new Date(today);
    week.setDate(today.getDate() - 6);
    slh.from = isoDay(week);
    slh.to = isoDay(today);
  }
  container.innerHTML = `
    <div id="${SCREEN}" class="txn-paged txn-on-landing" data-landing="list">
      <div class="page-section-bar">
        ${txnPageTitle({ screen: SCREEN, title: 'Sales History', page: '<span id="slh-page-title"></span>', count: '', landing: 'list' })}
        <span class="page-section-actions txn-landing-only">
          <input id="slh-q" class="form-input" type="search" placeholder="Bill, order, AWB, mobile, GSTIN" aria-label="Search" value="${esc(slh.q)}">
          <input id="slh-from" class="form-input" type="date" aria-label="From" value="${esc(slh.from)}">
          <input id="slh-to" class="form-input" type="date" aria-label="To" value="${esc(slh.to)}">
          <select id="slh-kind" class="form-input" aria-label="Type">
            ${[['', 'Sales and returns'], ['Sale', 'Sales'], ['Return', 'Returns']].map(([v, l]) => `<option value="${v}"${v === slh.kind ? ' selected' : ''}>${l}</option>`).join('')}
          </select>
          <select id="slh-source" class="form-input" aria-label="Channel">
            ${[['', 'Store and online'], ['Store', 'Store'], ['Online', 'Online']].map(([v, l]) => `<option value="${v}"${v === slh.source ? ' selected' : ''}>${l}</option>`).join('')}
          </select>
          <select id="slh-scope" class="form-input" aria-label="Stores">
            ${[['mine', 'My stores'], ['', 'This store']].map(([v, l]) => `<option value="${v}"${v === slh.scope ? ' selected' : ''}>${l}</option>`).join('')}
          </select>
          <button type="button" id="slh-search" class="btn btn-primary">Search</button>
        </span>
      </div>
      <div data-pane="list">
        <div id="slh-summary" class="text-muted s-11fcdc"></div>
        <div class="table-panel s-987eb2">
          <table>
            <thead><tr><th>Date</th><th>Type</th><th>No.</th><th>Store</th><th>Customer</th><th>Mobile</th><th>Order · AWB</th>
              <th class="s-30489c">Item disc.</th><th class="s-30489c">Offer disc.</th><th class="s-30489c">Bill disc.</th><th class="s-30489c">Total</th><th>Status</th></tr></thead>
            <tbody id="slh-rows"><tr><td colspan="12" class="text-muted">Loading…</td></tr></tbody>
          </table>
        </div>
        <div class="page-section-actions">
          <button type="button" id="slh-prev" class="btn btn-outline" disabled>Previous</button>
          <span id="slh-range" class="text-muted"></span>
          <button type="button" id="slh-next" class="btn btn-outline" disabled>Next</button>
        </div>
      </div>
      <div data-pane="bill" class="hidden"><div id="slh-bill"></div></div>
    </div>`;
  // A new search adds up afresh; only a page change keeps the totals.
  slhTotals = { key: '', h: null };
  const reload = () => { slh.offset = 0; slhTotals = { key: '', h: null }; loadSalesHistory(); };
  const q = container.querySelector('#slh-q');
  q.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); slh.q = q.value.trim(); reload(); } });
  container.querySelector('#slh-search').addEventListener('click', () => { slh.q = q.value.trim(); reload(); });
  for (const [id, key] of [['slh-from', 'from'], ['slh-to', 'to'], ['slh-kind', 'kind'], ['slh-source', 'source'], ['slh-scope', 'scope']]) {
    container.querySelector('#' + id).addEventListener('change', (e) => { slh[key] = e.target.value; slh.q = q.value.trim(); reload(); });
  }
  container.querySelector('#slh-prev').addEventListener('click', () => { slh.offset = Math.max(0, slh.offset - PAGE); loadSalesHistory(); });
  container.querySelector('#slh-next').addEventListener('click', () => { slh.offset += PAGE; loadSalesHistory(); });
  container.querySelector('#slh-rows').addEventListener('click', (e) => {
    const tr = e.target.closest('tr[data-i]');
    if (tr && slhRows[Number(tr.dataset.i)]) openSalesHistoryRow(slhRows[Number(tr.dataset.i)]);
  });
  container.querySelector('#slh-bill').addEventListener('click', onBillClick);
  txnRestorePane(SCREEN);
  const onBill = txnPageOf(SCREEN, 'list') === 'bill';
  await loadSalesHistory();
  if (onBill) {
    if (slhOpen) await openSalesHistoryRow(slhOpen, false);
    else window.txnShowPane(`${SCREEN}:list`);
  }
}

function historyURL(extra = {}) {
  const p = new URLSearchParams({ from: slh.from, to: slh.to, q: slh.q, kind: slh.kind, source: slh.source, scope: slh.scope, limit: String(PAGE), offset: String(slh.offset), ...extra });
  return `/api/v1/pos/sales-history?${p}`;
}

async function loadSalesHistory() {
  const body = document.getElementById('slh-rows');
  if (!body) return;
  body.innerHTML = '<tr><td colspan="12" class="text-muted">Loading…</td></tr>';
  // A newer search (a date changed while this one ran) wins.
  const mine = ++slhLoad;
  // 297.4: the rows first, then the totals apart, so adding up a big book
  // never holds the list.
  const res = await window.apiFetch(historyURL({ part: 'rows' }));
  if (mine !== slhLoad || !document.getElementById('slh-rows')) return;
  if (!res || !res.ok) {
    body.innerHTML = '<tr><td colspan="12" class="text-muted">The sales history could not be loaded.</td></tr>';
    slhTotals = { key: '', h: null };
    showSalesHistoryTotals({ failed: true });
    if (res) await window.showApiError(res, 'Could not load the sales history.');
    return;
  }
  const h = (await res.json()) || {};
  if (mine !== slhLoad) return;
  slhRows = h.rows || [];
  loadSalesHistoryTotals();
  const noMatch = h.search_scope === 'number' ? 'No match. Type the full bill number or mobile.' : 'Nothing matches this search in these dates.';
  body.innerHTML = slhRows.length ? slhRows.map((r, i) => `<tr data-i="${i}" class="s-74fa97">
      <td class="s-1cd2ee">${esc(shown(r.date))}</td><td>${typeCell(r)}</td>
      <td class="s-1cd2ee">${esc(r.number || r.id)}</td><td>${esc(r.store)}</td>
      <td>${esc(r.customer || (r.kind === 'Return' ? '' : 'Walk-in'))}${r.gstin ? `<div class="text-muted s-11fcdc">${esc(r.gstin)}</div>` : ''}</td>
      <td class="s-1cd2ee">${esc(r.phone)}</td><td>${refCell(r)}</td>
      <td class="s-30489c">${disc(r.item_discount)}</td><td class="s-30489c">${disc(r.offer_discount)}</td><td class="s-30489c">${disc(r.bill_discount)}</td>
      <td class="s-30489c s-1cd2ee">${r.kind === 'Return' ? '−' : ''}${esc(money(r.total))}</td>
      <td>${statusBadge(r.status)}${r.returned ? ' <span class="badge badge-warning">Returned</span>' : ''}</td></tr>`).join('')
    : `<tr><td colspan="12" class="text-muted">${slh.q ? noMatch : 'No sales or returns in these dates.'}</td></tr>`;
  const prev = document.getElementById('slh-prev');
  const next = document.getElementById('slh-next');
  const range = document.getElementById('slh-range');
  if (prev) prev.disabled = slh.offset === 0;
  if (next) next.disabled = !h.more;
  if (range) range.textContent = slhRows.length ? `${slh.offset + 1}-${slh.offset + slhRows.length}` : '';
}

// The totals of the search, asked for once per search: a page change keeps
// them. Until they come the line says so; the list works meanwhile.
async function loadSalesHistoryTotals() {
  const key = [slh.from, slh.to, slh.q, slh.kind, slh.source, slh.scope].join('\u0001');
  if (slhTotals.key === key) return showSalesHistoryTotals(slhTotals.h);
  slhTotals = { key, h: null };
  showSalesHistoryTotals(null);
  const res = await window.apiFetch(historyURL({ part: 'totals', offset: '0' }));
  if (slhTotals.key !== key) return;
  const h = res && res.ok ? await res.json().catch(() => ({ failed: true })) : { failed: true };
  if (slhTotals.key !== key) return;
  // A failed add-up is asked for again on the next load.
  slhTotals = { key: h && !h.failed ? key : '', h };
  showSalesHistoryTotals(h);
}

function showSalesHistoryTotals(h) {
  const count = document.querySelector(`#${SCREEN} .txn-count`);
  const summary = document.getElementById('slh-summary');
  // On a big book, too many bills to add up gives no totals.
  const none = !h || h.failed || h.totals_skipped;
  if (count) count.textContent = none ? '' : (h.sales || 0) + (h.returns || 0);
  if (!summary) return;
  summary.textContent = !h ? 'Adding up…' : h.failed ? 'Totals not loaded'
    : h.totals_skipped ? 'Too many bills to total · pick fewer days'
    : `Sales ${h.sales || 0} · ${money(h.sales_total)} · Returns ${h.returns || 0} · ${money(h.returns_total)} · Net ${money((h.sales_total || 0) - (h.returns_total || 0))}`
      + ` · Discounts: item ${money(h.item_discount)} · offer ${money(h.offer_discount)} · bill ${money(h.bill_discount)}`;
}

async function openSalesHistoryRow(row, push = true) {
  slhOpen = row;
  const el = document.getElementById('slh-bill');
  const title = document.getElementById('slh-page-title');
  if (!el) return;
  if (title) title.textContent = row.number || row.id;
  el.innerHTML = billHTML(row, null);
  if (push) window.txnShowPane(`${SCREEN}:bill`);
  // The lines: a sale's from the return check (sold, returned, left), so the
  // page also says whether Return can still take anything; a return's from
  // the return itself.
  let lines = null;
  if (row.kind === 'Return') {
    const res = await window.apiFetch(`/api/v1/doc/ReturnRequest/${encodeURIComponent(row.id)}`);
    if (res && res.ok) {
      const doc = (await res.json()) || {};
      lines = { ret: true, rows: (doc.items || []).map(l => ({ sku: l.sku, qty: l.qty, price: l.original_unit_price, reason: l.reason })) };
    }
  } else {
    const res = await window.apiFetch(`/api/v1/returns/eligibility?original_order_id=${encodeURIComponent(row.id)}`);
    if (res && res.ok) {
      const el2 = (await res.json()) || {};
      lines = { ret: false, found: el2.found, within: el2.within_window, days: el2.window_days,
        rows: (el2.lines || []).map(l => ({ sku: l.sku, sold: l.sold_qty, returned: l.already_returned, left: l.remaining_qty, price: l.unit_price })) };
    }
  }
  if (slhOpen === row && document.getElementById('slh-bill')) document.getElementById('slh-bill').innerHTML = billHTML(row, lines);
}

function billHTML(r, lines) {
  const fact = (label, value) => value ? `<div><div class="text-muted s-11fcdc">${label}</div><div>${value}</div></div>` : '';
  const isReturn = r.kind === 'Return';
  let table = '<p class="text-muted">Loading the lines…</p>';
  let canReturn = !isReturn;
  let why = '';
  if (lines) {
    if (lines.ret) {
      table = `<div class="table-panel s-6075cf"><table>
        <thead><tr><th>Item</th><th class="s-30489c">Qty</th><th class="s-30489c">Price</th><th>Reason</th></tr></thead>
        <tbody>${lines.rows.map(l => `<tr><td>${esc(l.sku)}</td><td class="s-30489c">${esc(l.qty)}</td><td class="s-30489c">${esc(money(l.price))}</td><td>${esc(l.reason)}</td></tr>`).join('')
          || '<tr><td colspan="4" class="text-muted">No lines.</td></tr>'}</tbody></table></div>`;
    } else {
      table = `<div class="table-panel s-6075cf"><table>
        <thead><tr><th>Item</th><th class="s-30489c">Sold</th><th class="s-30489c">Returned</th><th class="s-30489c">Left</th><th class="s-30489c">Price</th></tr></thead>
        <tbody>${lines.rows.map(l => `<tr><td>${esc(l.sku)}</td><td class="s-30489c">${esc(l.sold)}</td><td class="s-30489c">${esc(l.returned)}</td><td class="s-30489c">${esc(l.left)}</td><td class="s-30489c">${esc(money(l.price))}</td></tr>`).join('')
          || '<tr><td colspan="5" class="text-muted">No lines.</td></tr>'}</tbody></table></div>`;
      const left = lines.rows.reduce((n, l) => n + (Number(l.left) || 0), 0);
      if (!lines.found || !lines.rows.length) { canReturn = false; why = 'No lines to return'; }
      else if (!left) { canReturn = false; why = 'Everything is returned'; }
      else if (lines.within === false) { canReturn = false; why = `Past the ${lines.days}-day return window`; }
    }
  }
  return `
    <div class="table-panel s-d4fd04">
      ${fact('Type', `${isReturn ? 'Return' : 'Sale'} · ${esc(r.source)}`)}
      ${fact('Date', esc(shown(r.date)))}
      ${fact('Store', esc(r.store))}
      ${fact('Customer', esc(r.customer))}
      ${fact('Mobile', esc(r.phone))}
      ${fact('GSTIN', esc(r.gstin))}
      ${fact(isReturn ? 'Return no.' : 'Bill no.', esc(r.number || r.id))}
      ${fact('Against bill', esc(r.against || r.against_id))}
      ${fact('Channel', esc(r.channel))}
      ${fact('Channel order', esc(r.channel_order_id))}
      ${fact('Order', esc(r.order_no))}
      ${fact('AWB', esc(r.awb))}
      ${fact('Status', statusBadge(r.status) + (r.returned ? ' <span class="badge badge-warning">Returned</span>' : ''))}
      ${fact('Item discount', disc(r.item_discount))}
      ${fact('Offer discount', disc(r.offer_discount))}
      ${fact('Bill discount', disc(r.bill_discount))}
      ${fact(isReturn ? 'Return value' : 'Total', esc(money(r.total)))}
    </div>
    ${table}
    <div id="slh-error" class="hidden s-b9cceb" role="alert"></div>
    <div class="s-b9b7b5">
      ${isReturn ? '' : `<button type="button" class="btn btn-primary${canReturn ? '' : ' u-off'}" data-slh-do="return"${canReturn ? '' : ` disabled title="${esc(why)}"`}>Return</button>`}
      <button type="button" class="btn btn-outline" data-slh-do="print">Reprint</button>
      ${isReturn ? '<button type="button" class="btn btn-outline" data-slh-do="returns">Open in Returns</button>' : ''}
      ${isReturn && r.against_id ? '<button type="button" class="btn btn-outline" data-slh-do="original">Original bill</button>' : ''}
    </div>`;
}

function showBillError(text) {
  const el = document.getElementById('slh-error');
  if (!el) return;
  el.textContent = text || '';
  el.classList.toggle('hidden', !text);
}

async function onBillClick(e) {
  const btn = e.target.closest('[data-slh-do]');
  const r = slhOpen;
  if (!btn || btn.disabled || !r) return;
  showBillError('');
  const act = btn.dataset.slhDo;
  if (act === 'return') {
    // The till's Return tab, with this bill looked up (215.13). The till
    // checks the return window and quantities again, and the store it is
    // returned at is the till's own.
    await window.renderView('pos');
    (await import('./view-pos-extras.js' + V)).openPOSReturnFor(r.id, r.store);
    return;
  }
  if (act === 'print') {
    const [doctype, txn] = r.kind === 'Return' ? ['ReturnRequest', 'SalesReturn']
      : r.source === 'Online' ? ['SalesInvoice', 'SalesInvoice'] : ['POSCart', 'POSTaxInvoice'];
    (await import('./trade-docs.js' + V)).printTransactionDocument(doctype, r.id, { txn });
    return;
  }
  if (act === 'returns') { await window.renderView('returns'); return; }
  if (act === 'original') {
    const res = await window.apiFetch(historyURL({ q: r.against_id, kind: 'Sale', source: '', from: '', to: '', scope: 'mine', offset: '0' }));
    if (!res || !res.ok) { if (res) await window.showApiError(res, 'Could not open the original bill.'); return; }
    const found = (((await res.json()) || {}).rows || []).find(x => x.id === r.against_id);
    if (!found) { showBillError('The original bill is not in your stores.'); return; }
    await openSalesHistoryRow(found, false);
  }
}
