// Stage 148.19: bills with no PO. Opened from Vendor Invoices (view-finance.js).
// The list is the page; New bill is a page of its own.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const EB_ID = 'eb-screen';
const ebMoney = v => formatMoney(v || 0);

async function renderExpenseBillsView() {
  const root = document.getElementById('view-root');
  if (!root) return;
  const res = await apiFetch('/api/v1/procurement/expense-bills');
  if (!res) return;
  const data = res.ok ? await res.json() : { bills: [], accounts: [], enabled: false };
  const bills = data.bills || [];
  const accounts = data.accounts || [];
  root.innerHTML = '';

  const form = document.createElement('div');
  form.className = 'form-panel';
  const acctOptions = `<option value="">Account</option>` + accounts.map(a => `<option value="${escapeHTMLText(a.code)}">${escapeHTMLText(a.code)} ${escapeHTMLText(a.name)}</option>`).join('');
  form.innerHTML = `
    <div class="form-row s-aad644">
      <div class="form-group"><label class="form-label" for="eb-vendor">Vendor</label><input id="eb-vendor" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="eb-number">Bill no.</label><input id="eb-number" class="form-input"></div>
      <div class="form-group"><label class="form-label" for="eb-date">Bill date</label><input type="date" id="eb-date" class="form-input"></div>
      <div class="form-group"><label class="form-label" for="eb-po">PO</label><input id="eb-po" class="form-input" autocomplete="off"></div>
    </div>
    <div class="table-panel"><table>
      <thead><tr><th>Account</th><th>Note</th><th class="num">Amount</th><th class="num">GST %</th><th>GST credit</th><th>Cost centre</th><th></th></tr></thead>
      <tbody id="eb-lines"></tbody>
    </table></div>
    <div class="s-8a77e5"><button class="btn" id="eb-add">Add line</button></div>
    <div id="eb-total" class="s-56f435"></div>
    <div id="eb-error" class="login-error hidden s-56f435"></div>
    <div class="s-56f435"><button class="btn btn-primary" id="eb-save">Save bill</button></div>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table>
    <thead><tr><th>Bill no.</th><th>Vendor</th><th>Date</th><th class="num">Amount</th><th class="num">GST credit</th><th class="num">GST in cost</th><th>Status</th><th>PO</th><th></th></tr></thead>
    <tbody>${bills.length === 0
      ? `<tr><td colspan="9" class="s-306094">${data.enabled ? 'No bills yet.' : 'Switched off.'}</td></tr>`
      : bills.map(b => `<tr>
        <td>${escapeHTMLText(b.invoice_number)}</td><td>${escapeHTMLText(b.vendor)}</td><td>${escapeHTMLText(b.invoice_date)}</td>
        <td class="num">${ebMoney(b.amount)}</td><td class="num">${ebMoney(b.gst_credit)}</td><td class="num">${ebMoney(b.gst_blocked)}</td>
        <td><span class="badge ${b.status === 'Paid' ? 'badge-success' : b.status === 'Pending Approval' ? 'badge-warning' : b.status === 'Cancelled' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(b.status)}</span></td>
        <td>${b.po ? escapeHTMLText(b.po) : (b.status === 'Cancelled' ? '' : `<button class="action-btn" data-eb-link="${escapeHTMLText(b.id)}">Link PO</button>`)}</td>
        <td></td></tr>`).join('')}</tbody></table>`;

  root.appendChild(txnPagedSection({ id: EB_ID, title: 'Bills without a PO', page: 'New bill', count: bills.length,
    newLabel: 'New bill', form, list }));
  txnRestorePane(EB_ID);
  if (!data.enabled) {
    const nb = root.querySelector('[data-txn-new], .txn-landing-only button');
    if (nb) nb.disabled = true;
  }

  const body = form.querySelector('#eb-lines');
  const addLine = () => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td><select class="form-input" data-f="account">${acctOptions}</select></td>
      <td><input class="form-input" data-f="description"></td>
      <td><input type="number" step="0.01" min="0" class="form-input s-607468" data-f="amount"></td>
      <td><input type="number" step="0.01" min="0" class="form-input s-8573ba" data-f="gst_rate"></td>
      <td><select class="form-input" data-f="itc"><option value="">Default</option><option>Eligible</option><option>Blocked</option></select></td>
      <td><input class="form-input" data-f="cost_center" autocomplete="off"></td>
      <td><button class="action-btn" data-eb-del>Remove</button></td>`;
    body.appendChild(tr);
    if (typeof attachLinkTypeahead === 'function') attachLinkTypeahead(tr.querySelector('[data-f="cost_center"]'), 'CostCenter');
    tr.querySelector('[data-eb-del]').addEventListener('click', () => { if (body.children.length > 1) { tr.remove(); total(); } });
    tr.querySelectorAll('input').forEach(i => i.addEventListener('input', total));
  };
  const lines = () => Array.from(body.children).map(tr => {
    const v = f => tr.querySelector(`[data-f="${f}"]`).value;
    return { account: v('account'), description: v('description').trim(), amount: parseFloat(v('amount')) || 0,
      gst_rate: parseFloat(v('gst_rate')) || 0, itc: v('itc'), cost_center: v('cost_center').trim() };
  });
  const total = () => {
    const ls = lines();
    const net = ls.reduce((s, l) => s + l.amount, 0);
    const gst = ls.reduce((s, l) => s + l.amount * l.gst_rate / 100, 0);
    form.querySelector('#eb-total').textContent = net > 0 ? `${ebMoney(net)} + GST ${ebMoney(gst)} = ${ebMoney(net + gst)}` : '';
  };
  addLine();
  form.querySelector('#eb-add').addEventListener('click', addLine);
  form.querySelector('#eb-date').value = localISODate(new Date());
  if (typeof attachLinkTypeahead === 'function') {
    attachLinkTypeahead(form.querySelector('#eb-vendor'), 'Vendor');
    attachLinkTypeahead(form.querySelector('#eb-po'), 'PurchaseOrder');
  }

  const err = form.querySelector('#eb-error');
  const save = form.querySelector('#eb-save');
  save.addEventListener('click', async () => {
    err.classList.add('hidden');
    const payload = { vendor_id: form.querySelector('#eb-vendor').value.trim(), invoice_number: form.querySelector('#eb-number').value.trim(),
      invoice_date: form.querySelector('#eb-date').value, linked_po_id: form.querySelector('#eb-po').value.trim(), lines: lines() };
    const btn = save;
    btn.disabled = true;
    try {
      const r = await apiFetch('/api/v1/procurement/expense-bills', { method: 'POST', body: JSON.stringify(payload) });
      if (!r) return;
      if (!r.ok) {
        err.textContent = await getErrorMessage(r, 'The bill could not be saved.');
        err.classList.remove('hidden');
        return;
      }
      txnDone(EB_ID);
      await renderExpenseBillsView();
    } finally {
      btn.disabled = false;
    }
  });

  list.querySelectorAll('[data-eb-link]').forEach(b => b.addEventListener('click', async () => {
    const po = await showCustomPrompt("PO");
    if (!po) return;
    const r = await apiFetch(`/api/v1/procurement/expense-bills/${encodeURIComponent(b.dataset.ebLink)}/link-po`, { method: 'POST', body: JSON.stringify({ po_id: po.trim() }) });
    if (r && !r.ok) {
      err.textContent = await getErrorMessage(r, 'The PO could not be linked.');
      err.classList.remove('hidden');
      window.txnShowPane(`${EB_ID}:new`);
      return;
    }
    await renderExpenseBillsView();
  }));
}

export { renderExpenseBillsView };
