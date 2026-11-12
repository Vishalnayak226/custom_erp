// Stage 148.23: customer-vendor set-off. Loaded on demand by the PartyPair and
// SetOffVoucher lists. Shared services (apiFetch, showApiError, showToast,
// renderView, escapeHTMLText, formatMoney) are globals.

function soDialog(id, title, body, footer) {
  document.getElementById(id)?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = id;
  overlay.innerHTML = `<div class="modal-container s-5fa6d7" role="dialog" aria-modal="true" aria-labelledby="${id}-title">
      <div class="modal-header"><h3 class="modal-title" id="${id}-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-so-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body">${body}</div>
      <div class="modal-footer">${footer || ''}<button type="button" class="btn btn-outline" data-so-close>Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  overlay.querySelectorAll('[data-so-close]').forEach(b => b.addEventListener('click', () => overlay.remove()));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') overlay.remove(); });
  return overlay;
}

async function soGet(path, failText) {
  const res = await apiFetch(path);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

// Two parties can share a name, so a row that makes a pair shows the code as well.
const soParty = (name, id) => escapeHTMLText(name && id && name !== id ? `${name} · ${id}` : (name || id || ''));

async function openSetOffSuggestions() {
  const out = await soGet('/api/v1/finance/set-off/suggestions', 'Suggestions could not be loaded.');
  if (!out) return;
  const list = out.suggestions || [];
  const rows = list.length ? list.map((s, i) => `<tr><td>${soParty(s.customer_name, s.customer_id)}</td><td>${soParty(s.vendor_name, s.vendor_id)}</td>
      <td>${escapeHTMLText(s.basis)} ${escapeHTMLText(s.tax_id)}</td><td><button type="button" class="btn btn-outline btn-sm" data-so-pair="${i}">Pair</button></td></tr>`).join('')
    : '<tr><td colspan="4" class="s-306094">No matches</td></tr>';
  const overlay = soDialog('so-suggest', 'Suggested pairs', `<table><thead><tr><th>Customer</th><th>Vendor</th><th>Matched on</th><th></th></tr></thead><tbody>${rows}</tbody></table>`);
  overlay.querySelectorAll('[data-so-pair]').forEach(btn => btn.addEventListener('click', async () => {
    const s = list[Number(btn.dataset.soPair)];
    btn.disabled = true;
    const res = await apiFetch('/api/v1/doc/PartyPair', { method: 'POST', body: JSON.stringify({ customer_id: s.customer_id, vendor_id: s.vendor_id, status: 'Active' }) });
    if (!res) { btn.disabled = false; return; }
    if (!res.ok) { btn.disabled = false; await showApiError(res, 'The pair was not made.'); return; }
    showToast('Paired.', { variant: 'success' });
    overlay.remove();
    renderView('doctype-table');
  }));
}

async function openSetOffBalances(pairId) {
  const b = await soGet(`/api/v1/finance/set-off/pairs/${encodeURIComponent(pairId)}/balances`, 'Balances could not be loaded.');
  if (!b) return;
  soDialog('so-bal', 'Balances', `<table><tbody>
    <tr><td>${escapeHTMLText(b.customer_name || b.customer_id)} owes us</td><td class="num">${formatMoney(b.customer_owes)}</td><td class="num">${b.customer_invoices} invoices</td></tr>
    <tr><td>We owe ${escapeHTMLText(b.vendor_name || b.vendor_id)}</td><td class="num">${formatMoney(b.we_owe_vendor)}</td><td class="num">${b.vendor_bills} bills</td></tr>
    <tr><td><strong>Most that can be set off</strong></td><td class="num"><strong>${formatMoney(b.max_set_off)}</strong></td><td></td></tr></tbody></table>`);
}

async function openSetOffStatement(voucherId) {
  const s = await soGet(`/api/v1/finance/set-off/${encodeURIComponent(voucherId)}/statement`, 'The statement could not be loaded.');
  if (!s) return;
  const lines = (ls) => ls.length ? ls.map(l => `<tr><td>${escapeHTMLText(l.id)}</td><td class="num">${formatMoney(l.amount)}</td></tr>`).join('') : '<tr><td colspan="2">None</td></tr>';
  const html = `<div class="so-statement"><h3>Set-off ${escapeHTMLText(s.id)}</h3>
    <p>${escapeHTMLText(s.status)} · ${escapeHTMLText(s.date)}${s.reversed_on ? ` · reversed ${escapeHTMLText(s.reversed_on)}` : ''} · ${escapeHTMLText(s.posted_by || '')}</p>
    <table><tbody><tr><td>Customer</td><td>${escapeHTMLText(s.customer_name)}</td><td class="num">${formatMoney(s.customer_before)} → ${formatMoney(s.customer_after)}</td></tr>
    <tr><td>Vendor</td><td>${escapeHTMLText(s.vendor_name)}</td><td class="num">${formatMoney(s.vendor_before)} → ${formatMoney(s.vendor_after)}</td></tr>
    <tr><td>Set off</td><td>Account ${escapeHTMLText(s.clearing_account)}</td><td class="num"><strong>${formatMoney(s.amount)}</strong></td></tr></tbody></table>
    <h4>Invoices cleared</h4><table><tbody>${lines(s.invoices || [])}</tbody></table>
    <h4>Bills cleared</h4><table><tbody>${lines(s.bills || [])}</tbody></table></div>`;
  const overlay = soDialog('so-stmt', 'Set-off statement', html, '<button type="button" class="btn btn-primary" data-so-print>Print</button>');
  overlay.querySelector('[data-so-print]').addEventListener('click', () => {
    const area = document.getElementById('invoice-print-area');
    if (!area) { window.print(); return; }
    area.replaceChildren(overlay.querySelector('.so-statement').cloneNode(true));
    area.classList.add('printing');
    window.print();
    area.classList.remove('printing');
    area.replaceChildren();
  });
}

async function reverseSetOff(voucherId) {
  const date = await showCustomPrompt('Reverse on (YYYY-MM-DD, blank: today):', '', 'Reverse set-off');
  if (date === null || date === undefined) return;
  const res = await apiFetch(`/api/v1/finance/set-off/${encodeURIComponent(voucherId)}/reverse`, { method: 'POST', body: JSON.stringify({ date: String(date).trim() }) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The set-off was not reversed.'); return; }
  showToast('Reversed.', { variant: 'success' });
  renderView('doctype-table');
}

window.openSetOffSuggestions = openSetOffSuggestions;
window.openSetOffBalances = openSetOffBalances;
window.openSetOffStatement = openSetOffStatement;
window.reverseSetOff = reverseSetOff;
