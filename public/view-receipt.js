// 140.12.5 / 140.12.6: the Receipt entry screen - money a customer paid at the
// back office: how it came (bank, cheque, UPI, card, cash), the bank account,
// TDS they deducted, the invoices it settles (oldest first), their open
// advances, and anything left over kept on account as a new advance.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const r2 = (v) => Math.round(v * 100) / 100;

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

export async function renderReceiptEntry(container, onDone) {
  const { sessionStore } = await import('./txn-screen.js' + Q);
  const S = { invoices: [], advances: [], modes: ['Bank Transfer', 'Cheque', 'UPI', 'Card', 'Cash'] };
  const banks = (await getJSON('/api/v1/doc/BankAccount?limit=200', [])).filter(b => (b.status || 'Active') === 'Active');
  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group s-116966"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  panel.innerHTML = `<div class="page-header s-249d01"><div class="page-title-section"><h1 class="page-title">New Receipt</h1></div>
      <div><button type="button" class="btn btn-ghost" data-rc="back">Back to invoices</button></div></div>
    <div class="s-d9da29">
      ${field('rc-customer', 'Customer', 220)}
      ${field('rc-store', 'Store', 150, `value="${esc(sessionStore())}"`)}
      ${field('rc-date', 'Date', 150, `type="date" value="${new Date().toISOString().slice(0, 10)}"`)}
      <div class="form-group s-116966"><label class="form-label" for="rc-mode">Mode</label><select class="form-input s-7c07cd" id="rc-mode">${S.modes.map(m => `<option>${esc(m)}</option>`).join('')}</select></div>
      <div class="form-group s-116966"><label class="form-label" for="rc-bank">Bank account</label><select class="form-input s-2acaf3" id="rc-bank"><option value="">-</option>${banks.map(b => `<option value="${esc(b.id)}">${esc(b.bank_name || '')} ${esc(String(b.account_number || '').slice(-4))}</option>`).join('')}</select></div>
      ${field('rc-ref', 'Reference', 150, 'placeholder="UTR / cheque no"')}
      ${field('rc-amount', 'Amount received', 130, 'type="number" inputmode="decimal" min="0" step="0.01"')}
      ${field('rc-tds', 'TDS deducted', 110, 'type="number" inputmode="decimal" min="0" step="0.01"')}
    </div>
    <div data-rc="items"><p class="text-muted">Choose the customer.</p></div>
    <div data-rc="totals" class="s-7cc550"></div>
    <div class="login-error hidden s-56f435" data-rc="error"></div>
    <div class="s-1b0f49"><button type="button" class="btn btn-primary" data-rc="save">Save receipt</button></div>`;
  container.appendChild(panel);
  const q = (k) => panel.querySelector(`[data-rc="${k}"]`);
  const v = (sel) => panel.querySelector(sel).value.trim();
  q('back').addEventListener('click', () => onDone && onDone());

  // Fill invoices oldest first from what arrived (money + TDS + advances used).
  const autoAllocate = () => {
    let left = num(v('#rc-amount')) + num(v('#rc-tds')) + S.advances.reduce((s, a) => s + num(a.use), 0);
    S.invoices.forEach(i => { const take = Math.max(0, Math.min(i.outstanding, left)); i.pay = r2(take); left = r2(left - take); });
  };
  const draw = () => {
    const money = num(v('#rc-amount')), tds = num(v('#rc-tds'));
    const advUsed = S.advances.reduce((s, a) => s + num(a.use), 0);
    const allocated = S.invoices.reduce((s, i) => s + num(i.pay), 0);
    const onAccount = r2(money + tds + advUsed - allocated);
    q('totals').innerHTML = `<table class="table-compact s-046678"><tbody>
      <tr><td>Received</td><td class="num">${formatMoney(money)}</td></tr>
      ${tds ? `<tr><td>TDS</td><td class="num">${formatMoney(tds)}</td></tr>` : ''}
      ${advUsed ? `<tr><td>Advances used</td><td class="num">${formatMoney(advUsed)}</td></tr>` : ''}
      <tr><td>To invoices</td><td class="num">${formatMoney(allocated)}</td></tr>
      <tr><td><b>${onAccount < 0 ? 'Over-allocated' : 'On account'}</b></td><td class="num"><b>${formatMoney(onAccount)}</b></td></tr></tbody></table>`;
  };
  const drawItems = () => {
    const host = q('items');
    if (!S.invoices.length && !S.advances.length) {
      host.innerHTML = '<p class="text-muted">No open invoices or advances - the whole amount is kept on account.</p>';
      draw();
      return;
    }
    host.innerHTML = `${S.invoices.length ? `<div class="table-wrapper"><table><thead><tr><th>Invoice</th><th>Date</th><th>Due</th><th class="num">Outstanding</th><th class="num">Pay</th></tr></thead><tbody>
      ${S.invoices.map((i, n) => `<tr><td>${esc(i.number)}</td><td>${esc(i.date)}</td><td>${esc(i.due_date || '')}</td><td class="num">${formatMoney(i.outstanding)}</td>
        <td class="num"><input class="form-input s-b54ee3" data-rc-pay="${n}" type="number" inputmode="decimal" min="0" step="0.01" value="${i.pay || ''}"></td></tr>`).join('')}
      </tbody></table></div>` : ''}
      ${S.advances.length ? `<div class="table-wrapper s-56f435"><table><thead><tr><th>Advance</th><th>Taken</th><th class="num">Left</th><th class="num">Use</th></tr></thead><tbody>
      ${S.advances.map((a, n) => `<tr><td>${esc(a.number)}</td><td>${esc(String(a.taken_at || '').slice(0, 10))}</td><td class="num">${formatMoney(a.balance)}</td>
        <td class="num"><input class="form-input s-b54ee3" data-rc-adv="${n}" type="number" inputmode="decimal" min="0" step="0.01" value="${a.use || ''}"></td></tr>`).join('')}
      </tbody></table></div>` : ''}`;
    host.querySelectorAll('[data-rc-pay]').forEach(el => el.addEventListener('input', () => { S.invoices[+el.dataset.rcPay].pay = num(el.value); draw(); }));
    host.querySelectorAll('[data-rc-adv]').forEach(el => el.addEventListener('input', () => {
      const a = S.advances[+el.dataset.rcAdv];
      a.use = Math.min(num(el.value), a.balance);
      autoAllocate();
      drawItems();
    }));
    draw();
  };
  const loadCustomer = async () => {
    const c = v('#rc-customer');
    if (!c) return;
    const data = await getJSON(`/api/v1/finance/receipts/open-items?customer=${encodeURIComponent(c)}`, { invoices: [], advances: [] });
    S.invoices = (data.invoices || []).map(i => ({ ...i, pay: 0 }));
    S.advances = (data.advances || []).map(a => ({ ...a, use: 0 }));
    autoAllocate();
    drawItems();
  };
  attachLinkTypeahead(panel.querySelector('#rc-customer'), 'Customer', { noSetupHint: true, onPick: () => loadCustomer() });
  panel.querySelector('#rc-customer').addEventListener('change', loadCustomer);
  ['#rc-amount', '#rc-tds'].forEach(sel => panel.querySelector(sel).addEventListener('input', () => { autoAllocate(); drawItems(); }));
  q('save').addEventListener('click', async e => {
    const btn = e.currentTarget;
    const err = q('error');
    const fail = (m) => { err.textContent = m; err.classList.remove('hidden'); };
    err.classList.add('hidden');
    if (!v('#rc-customer')) { fail('Choose the customer.'); return; }
    const mode = v('#rc-mode');
    if ((mode === 'Cheque' || mode === 'Bank Transfer') && !v('#rc-bank')) { fail(`Choose the bank account the ${mode.toLowerCase()} went into.`); return; }
    btn.disabled = true;
    const res = await apiFetch('/api/v1/finance/receipts', { method: 'POST', body: JSON.stringify({
      customer: v('#rc-customer'), location: v('#rc-store'), date: v('#rc-date'), mode, bank_account: v('#rc-bank'), reference: v('#rc-ref'),
      amount: num(v('#rc-amount')), tds: num(v('#rc-tds')),
      invoices: S.invoices.filter(i => num(i.pay) > 0).map(i => ({ ref: i.id, amount: num(i.pay) })),
      advances: S.advances.filter(a => num(a.use) > 0).map(a => ({ ref: a.number, amount: num(a.use) })),
      idempotency_key: `rc-${Date.now()}-${Math.random().toString(36).slice(2, 8)}` }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The receipt was not saved.')); return; }
    const rec = await res.json();
    showToast(rec.on_account_advance ? `Receipt ${rec.number} saved - ${formatMoney(rec.on_account_amount)} kept on account (${rec.on_account_advance}).` : `Receipt ${rec.number} saved.`, { variant: 'success' });
    if (onDone) onDone();
  });
  drawItems();
}
