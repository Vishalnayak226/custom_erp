// Stage 170.3 / 170.4: bank files - statement import and payment response.
// Shared app services (apiFetch, showApiError, showToast, renderView, escapeHTMLText) come from the classic shell.

async function postFile(url, body) {
  const r = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!r) return null;
  if (!r.ok) { await showApiError(r, 'The file could not be loaded.'); return null; }
  return r.json();
}

function readFile(input) {
  return new Promise(resolve => {
    const f = input.files && input.files[0];
    if (!f) { resolve(null); return; }
    const fr = new FileReader();
    fr.onload = () => resolve({ name: f.name, text: String(fr.result || '') });
    fr.readAsText(f);
  });
}

async function renderBankFilesView(container) {
  const res = await apiFetch('/api/v1/doc/BankAccount');
  const accounts = res && res.ok ? await res.json() : [];
  const opts = accounts.map(a => `<option value="${escapeHTMLText(a.id)}">${escapeHTMLText((a.data && (a.data.name || a.data.account_name)) || a.id)}</option>`).join('');

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = '<div class="page-title-section"><h1 class="page-title">Bank Files</h1></div>';
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <h3>Statement</h3>
    <div class="filter-bar">
      <select class="form-select" data-st-account aria-label="Bank account">${opts}</select>
      <select class="form-select" data-st-format aria-label="Format"><option value="csv">CSV</option><option value="mt940">MT940</option></select>
      <input type="file" data-st-file accept=".csv,.txt,.sta,.mt940" aria-label="Statement file">
      <button type="button" class="btn btn-primary" data-st-go>Load</button>
    </div>
    <div data-st-out></div>
    <h3>Bank response</h3>
    <div class="filter-bar">
      <input class="form-input" data-pr-proposal placeholder="Payment proposal" aria-label="Payment proposal">
      <select class="form-select" data-pr-account aria-label="Bank account">${opts}</select>
      <input type="file" data-pr-file accept=".csv,.txt" aria-label="Response file">
      <button type="button" class="btn btn-primary" data-pr-go>Load</button>
    </div>
    <div data-pr-out></div>`;
  container.appendChild(panel);

  const table = (head, rows) => rows.length === 0 ? '' : `<table><thead><tr>${head.map(h => `<th>${h}</th>`).join('')}</tr></thead><tbody>${
    rows.map(r => `<tr>${r.map(c => `<td>${escapeHTMLText(String(c ?? ''))}</td>`).join('')}</tr>`).join('')}</tbody></table>`;

  panel.querySelector('[data-st-go]').addEventListener('click', async () => {
    const f = await readFile(panel.querySelector('[data-st-file]'));
    if (!f) { showToast('Choose a file.', { variant: 'error' }); return; }
    const out = await postFile('/api/v1/finance/bank-statement/import', {
      bank_account: panel.querySelector('[data-st-account]').value, format: panel.querySelector('[data-st-format]').value, content: f.text });
    if (!out) return;
        panel.querySelector('[data-st-out]').innerHTML = `<p>Loaded <b>${out.loaded ?? 0}</b> &middot; Skipped (already loaded) <b>${out.skipped ?? 0}</b> &middot; Errors <b>${(out.errors || []).length}</b></p>${
      table(['Row', 'Date', 'Reference', 'Amount', 'Dr/Cr', 'Result'], (out.lines || []).map(l => [l.row, l.txn_date, l.reference_number, l.amount, l.dr_cr, l.result]))}${table(['Problem'], (out.errors || []).map(e => [e]))}`;
    showToast('Statement processed.', { variant: 'success' });
  });

  panel.querySelector('[data-pr-go]').addEventListener('click', async () => {
    const id = panel.querySelector('[data-pr-proposal]').value.trim();
    const f = await readFile(panel.querySelector('[data-pr-file]'));
    if (!id || !f) { showToast('Enter the proposal and choose a file.', { variant: 'error' }); return; }
    const out = await postFile(`/api/v1/finance/payment-proposal/${encodeURIComponent(id)}/bank-response`, {
      bank_account: panel.querySelector('[data-pr-account]').value, content: f.text });
    if (!out) return;
    panel.querySelector('[data-pr-out]').innerHTML = `<p>Paid <b>${out.paid}</b> &middot; Rejected <b>${out.rejected}</b> &middot; Skipped <b>${out.skipped}</b> &middot; Unknown <b>${out.unknown}</b> &middot; Errors <b>${out.errors}</b></p>${
      table(['Row', 'Reference', 'Status', 'UTR', 'Result', 'Detail'], (out.rows || []).map(r => [r.row, r.payment_reference, r.status, r.utr, r.result, r.detail || r.reason]))}`;
  });
}

export { renderBankFilesView };
