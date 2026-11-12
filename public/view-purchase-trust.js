// Stage 148.12 / 148.13: two small panels, loaded only when a form is opened.
//  - attachRateHistory: an item's purchase-rate history (who, when, old, new,
//    the bill it came from).
//  - attachScorecard: a vendor's track record, worked out from its orders,
//    receipts and bills; the period defaults to the last 12 months.
// Server side: engines/purchase_rate.go, vendor_scorecard.go.

const e = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const rupees = n => formatMoney(n || 0);
const pct = x => (x == null ? 'No data' : `${x}%`);

function addSection(body, title) {
  const section = document.createElement('details');
  section.className = 'form-section';
  section.innerHTML = `<summary>${title}</summary><div class="trust-body"></div>`;
  body.appendChild(section);
  return { section, host: section.querySelector('.trust-body') };
}

export function attachRateHistory(body, record) {
  const sku = record && (record.code || record.id);
  if (!body || !sku || record.standard_cost === undefined) return;
  const { section, host } = addSection(body, 'Purchase rate history');
  let loaded = false;
  section.addEventListener('toggle', async () => {
    if (!section.open || loaded) return;
    loaded = true;
    host.textContent = 'Loading…';
    const res = await apiFetch(`/api/v1/procurement/purchase-rate-history?sku=${encodeURIComponent(sku)}`);
    if (res && res.status === 403) { section.remove(); return; }
    if (!res || !res.ok) { host.textContent = 'Could not be loaded.'; loaded = false; return; }
    const rows = (await res.json()).history || [];
    host.innerHTML = rows.length
      ? `<table><thead><tr><th>When</th><th>By</th><th class="num">Old</th><th class="num">New</th><th>Bill</th><th>Reason</th></tr></thead><tbody>${rows.map(r =>
        `<tr><td>${e(String(r.changed_at || '').slice(0, 16).replace('T', ' '))}</td><td>${e(r.changed_by)}</td><td class="num">${rupees(r.old_rate)}</td><td class="num">${rupees(r.new_rate)}</td><td>${e(r.source_ref || r.source_id)}</td><td>${e(r.reason || '')}</td></tr>`).join('')}</tbody></table>`
      : '<span class="s-d59bb5">No changes yet</span>';
  });
}

export function attachScorecard(body, record) {
  const vendor = record && (record.code || record.id);
  if (!body || !vendor) return;
  const { section, host } = addSection(body, 'Scorecard');
  let loaded = false;
  const load = async months => {
    const to = new Date();
    const from = new Date(to.getFullYear(), to.getMonth() - months, to.getDate() + 1);
    const day = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    const res = await apiFetch(`/api/v1/procurement/vendor-scorecard?vendor=${encodeURIComponent(vendor)}&from=${day(from)}&to=${day(to)}`);
    if (!res || !res.ok) { host.textContent = 'Could not be loaded.'; return; }
    const s = await res.json();
    const price = s.price_variance_pct == null ? 'No data' : `${s.price_variance_pct > 0 ? '+' : ''}${s.price_variance_pct}%`;
    host.innerHTML = `<div class="s-16949d"><select class="trust-period">${[3, 6, 12, 24].map(m => `<option value="${m}"${m === months ? ' selected' : ''}>Last ${m} months</option>`).join('')}</select></div>
      <table><tbody>
        <tr><td>Orders</td><td class="num">${s.orders}</td></tr>
        <tr><td>Receipts</td><td class="num">${s.receipts}</td></tr>
        <tr><td>On time</td><td class="num">${pct(s.on_time_pct)}</td></tr>
        <tr><td>Fill rate</td><td class="num">${pct(s.fill_pct)}</td></tr>
        <tr><td>QC reject</td><td class="num">${pct(s.qc_reject_pct)}</td></tr>
        <tr><td>Price variance</td><td class="num">${price}</td></tr>
        <tr><td>Bill mismatches</td><td class="num">${s.bill_mismatches}${s.bill_mismatch_pct == null ? '' : ` (${s.bill_mismatch_pct}%)`}</td></tr>
      </tbody></table>`;
    host.querySelector('.trust-period').addEventListener('change', ev => load(Number(ev.target.value)));
  };
  section.addEventListener('toggle', () => {
    if (!section.open || loaded) return;
    loaded = true;
    host.textContent = 'Loading…';
    load(12);
  });
}
