// Operation confirmations of a production order (170.10): one small table under
// the order's row - operation, good, scrap, minutes, who, when.
export async function showProductionConfirmations(orderId) {
  const row = document.getElementById(`mfg-conf-${orderId}`);
  if (!row) return;
  if (!row.classList.contains('hidden')) { row.classList.add('hidden'); return; }
  const cell = row.firstElementChild;
  cell.textContent = 'Loading...';
  row.classList.remove('hidden');
  const res = await apiFetch(`/api/v1/manufacturing/confirmations?order_id=${encodeURIComponent(orderId)}`);
  if (!res) return;
  if (!res.ok) { cell.innerHTML = `<div class="login-error">${escapeHTMLText((await res.json().catch(() => ({}))).error || 'Confirmations could not load.')}</div>`; return; }
  const rows = await res.json();
  if (!rows.length) { cell.textContent = 'No confirmations yet.'; return; }
  cell.innerHTML = `<table class="data-table"><thead><tr><th>Operation</th><th class="num">Good</th><th class="num">Scrap</th><th class="num">Minutes</th><th>By</th><th>When</th></tr></thead><tbody>${
    rows.map(c => `<tr><td>${c.seq}</td><td class="num">${c.good_qty}</td><td class="num">${c.scrap_qty}</td><td class="num">${c.minutes}</td><td>${escapeHTMLText(c.operator_kind)}: ${escapeHTMLText(c.operator_id)}</td><td>${escapeHTMLText(c.confirmed_at)}</td></tr>`).join('')
  }</tbody></table>`;
}
