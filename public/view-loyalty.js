// Stage 73.2.11 / 73.2.12 / 73.3.1 / 55.17 - the Loyalty screen (Sales &
// Marketplace -> Loyalty). Lazy module; nothing here loads at start-up.
//
//   Customer - a customer's points: balance, tier and what it gives, every
//              ledger entry in words with the document it came from, and a
//              manual adjustment (a reason, and a manager above the value).
//   Tiers    - the tier rules: threshold, multiplier, benefits, grace days.
//   Movement - the Loyalty Points Movement report for a period: earned,
//              redeemed, given back, taken back, adjusted, expired, closing,
//              and (when points are accrued) the liability in the books.
//   Vouchers & campaigns (55.17) - each voucher in words (what, for whom,
//              where, when, usable now or why not, what it gave away) and
//              each campaign (trigger, message, who it picks today, results).
//   The Customer tab also shows the customer's recent bills, returns and
//   usable vouchers (55.17) - the till's own customer context.

let loyTiers = [];

function loyEsc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// What a ledger row was, in words, and whether it added or took points.
function loyEntry(e) {
  const ref = e.reference_id || '';
  if (e.transaction_type === 'Earn') {
    if (e.reference_doctype === 'RefundRequest') return ['Given back on a return', 1];
    if (e.reference_doctype === 'LoyaltyPointAdjustment') return ['Credited by adjustment', 1];
    if (ref.endsWith(':REDEMPTION-REVERSAL')) return ['Not needed on the bill - handed back', 1];
    if (e.reference_doctype === 'POSCart') return ['Earned on a sale', 1];
    return ['Credited', 1];
  }
  if (e.transaction_type === 'Burn') return e.reference_doctype === 'LoyaltyExpiry' ? ['Expired', -1] : ['Redeemed', -1];
  if (e.transaction_type === 'Reversal') return ['Taken back on a return', -1];
  if (e.transaction_type === 'Adjustment') return ['Debited by adjustment', -1];
  return [e.transaction_type, -1];
}

async function loyJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (e) { return fallback; }
}

export async function renderLoyaltyView(root) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="page-header" style="padding:16px 16px 0;"><h1 class="page-title">Loyalty</h1></div>
    <div style="padding:0 16px 16px; display:grid; gap:12px;">
      <div class="txn-tabs" role="tablist" style="display:flex; gap:6px;">
        <button class="btn btn-outline btn-sm" type="button" data-loy-tab="customer">Customer</button>
        <button class="btn btn-outline btn-sm" type="button" data-loy-tab="tiers">Tiers</button>
        <button class="btn btn-outline btn-sm" type="button" data-loy-tab="movement">Points movement</button>
        <button class="btn btn-outline btn-sm" type="button" data-loy-tab="offers">Vouchers &amp; campaigns</button>
      </div>
      <section data-loy-pane="customer">
        <div style="display:flex; gap:8px; align-items:flex-end; flex-wrap:wrap;">
          <div class="form-group" style="flex:1; min-width:240px;"><label class="form-label" for="loy-customer-display">Customer</label>
            <input type="text" id="loy-customer-display" class="form-input" placeholder="Search by name or phone" autocomplete="off">
            <input type="hidden" id="loy-customer"></div>
          <button class="btn btn-outline" type="button" id="loy-adjust" disabled>Adjust points</button>
        </div>
        <div id="loy-summary" class="txn-hint" style="margin:10px 0;">Pick a customer to see their points.</div>
        <div id="loy-context"></div>
        <div class="table-wrapper"><table>
          <thead><tr><th>When</th><th>What</th><th>Points</th><th>From</th><th>Expires</th></tr></thead>
          <tbody id="loy-ledger"></tbody>
        </table></div>
        <h3 style="font-size:14px; margin:14px 0 6px;">Adjustments</h3>
        <div class="table-wrapper"><table>
          <thead><tr><th>When</th><th>Points</th><th>Reason</th><th>Note</th><th>Status</th></tr></thead>
          <tbody id="loy-adjustments"></tbody>
        </table></div>
      </section>
      <section data-loy-pane="tiers" class="hidden">
        <p class="txn-hint" style="margin:0 0 8px;">A customer's tier follows what they spent, net of offers and refunds (all time, or the period in Settings -> Loyalty -> Tier spend counted over the last). Grace days keep a tier after spend drops below it.</p>
        <div class="table-wrapper"><table>
          <thead><tr><th>Tier</th><th>Spend from</th><th>Earn multiplier</th><th>Benefits (shown at the till)</th><th>Grace days</th><th></th></tr></thead>
          <tbody id="loy-tiers"></tbody>
        </table></div>
        <div style="margin-top:8px;"><button class="btn btn-outline btn-sm" type="button" id="loy-tier-add">Add a tier</button></div>
      </section>
      <section data-loy-pane="movement" class="hidden">
        <div style="display:flex; gap:8px; align-items:flex-end; flex-wrap:wrap;">
          <div class="form-group"><label class="form-label" for="loy-from">From</label><input type="date" id="loy-from" class="form-input"></div>
          <div class="form-group"><label class="form-label" for="loy-to">To</label><input type="date" id="loy-to" class="form-input"></div>
          <button class="btn btn-primary" type="button" id="loy-run">Show</button>
        </div>
        <div class="table-wrapper" style="margin-top:10px;"><table>
          <thead><tr><th>Movement</th><th>Points</th><th>Value</th></tr></thead>
          <tbody id="loy-movement"><tr><td colspan="3" class="txn-hint">Choose a period and click Show (blank = all time).</td></tr></tbody>
        </table></div>
      </section>
      <section data-loy-pane="offers" class="hidden">
        <h3 style="font-size:14px; margin:0 0 6px;">Vouchers</h3>
        <div class="table-wrapper"><table>
          <thead><tr><th>Code</th><th>Gives</th><th>For</th><th>Where</th><th>Valid</th><th>Used</th><th>Now</th><th>Discount given</th></tr></thead>
          <tbody id="loy-vouchers"></tbody>
        </table></div>
        <div style="margin:8px 0 14px;"><button class="btn btn-outline btn-sm" type="button" data-loy-open="Voucher">Add or edit vouchers</button></div>
        <h3 style="font-size:14px; margin:0 0 6px;">Campaigns</h3>
        <div class="table-wrapper"><table>
          <thead><tr><th>Campaign</th><th>Trigger</th><th>Sent by</th><th>Status</th><th>Message as sent</th><th>Would pick today</th><th>Skipped: no consent</th><th>Sent to</th><th>Sales since</th><th>Cost</th><th>Return</th></tr></thead>
          <tbody id="loy-campaigns"></tbody>
        </table></div>
        <div style="margin-top:8px;"><button class="btn btn-outline btn-sm" type="button" data-loy-open="Campaign">Add or edit campaigns</button></div>
        <p class="txn-hint" style="margin:10px 0 0;">A campaign hands each message to CleverTap; whether it reached the customer is tracked there, not here. Only customers who agreed to offers on the campaign's channel (ticked on their Customer record) are sent to - no consent, no SMS or WhatsApp. A customer is picked at most once a day.</p>
      </section>
      <div id="loy-error" class="login-error hidden"></div>
    </div>`;
  root.appendChild(panel);
  const q = id => panel.querySelector('#' + id);
  const show = tab => {
    panel.querySelectorAll('[data-loy-pane]').forEach(s => s.classList.toggle('hidden', s.dataset.loyPane !== tab));
    panel.querySelectorAll('[data-loy-tab]').forEach(b => b.classList.toggle('btn-primary', b.dataset.loyTab === tab));
    if (tab === 'tiers') loadTiers(panel);
    if (tab === 'offers') loadOffers(panel);
  };
  panel.addEventListener('click', e => {
    const t = e.target.dataset.loyTab;
    if (t) show(t);
    // Vouchers and campaigns are set up on their own records.
    const open = e.target.dataset.loyOpen;
    if (open) { currentDoctype = open; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); }
  });
  attachCodeNamePicker(q('loy-customer-display'), q('loy-customer'), 'Customer');
  q('loy-customer').addEventListener('change', () => loadCustomer(panel));
  q('loy-adjust').addEventListener('click', () => adjustPoints(panel));
  q('loy-run').addEventListener('click', () => runMovement(panel));
  q('loy-tier-add').addEventListener('click', () => { loyTiers.push({ id: 0, tier: '', min_spend: 0, earn_multiplier: 1, benefits: '', grace_days: 0 }); drawTiers(panel); });
  show('customer');
}

async function loadCustomer(panel) {
  const id = panel.querySelector('#loy-customer').value.trim();
  const summary = panel.querySelector('#loy-summary');
  panel.querySelector('#loy-adjust').disabled = !id;
  if (!id) return;
  const [ledger, cust, adj] = await Promise.all([
    loyJSON(`/api/v1/loyalty/ledger?customer_id=${encodeURIComponent(id)}`, null),
    loyJSON(`/api/v1/doc/Customer/${encodeURIComponent(id)}`, {}),
    loyJSON(`/api/v1/loyalty/adjustments?customer_id=${encodeURIComponent(id)}`, { adjustments: [] })
  ]);
  if (!loyTiers.length) loyTiers = await loyJSON('/api/v1/crm/loyalty-tier-rules', []) || [];
  const data = cust.data || cust;
  const tier = data.loyalty_tier || '';
  const rule = loyTiers.find(t => t.tier === tier);
  if (!ledger) { summary.textContent = 'The points could not be looked up.'; return; }
  summary.innerHTML = `<strong>${loyEsc(data.name || id)}</strong> has <strong>${ledger.balance}</strong> point${ledger.balance === 1 ? '' : 's'}${ledger.balance < 0 ? ' (negative: a return took back points already spent - new purchases pay it back)' : ''}.
    Tier: <strong>${loyEsc(tier || 'none yet')}</strong>${rule && rule.benefits ? ` &middot; ${loyEsc(rule.benefits)}` : ''}${data.loyalty_tier_below_since ? ` &middot; spend below this tier since ${loyEsc(data.loyalty_tier_below_since)} (kept for ${rule ? rule.grace_days : 0} grace days)` : ''}`;
  const entries = ledger.ledger || ledger.entries || [];
  panel.querySelector('#loy-ledger').innerHTML = entries.length ? entries.map(e => {
    const [what, sign] = loyEntry(e);
    return `<tr><td>${loyEsc(String(e.created_at || '').slice(0, 16).replace('T', ' '))}</td><td>${loyEsc(what)}</td>
      <td style="font-weight:600; color:${sign > 0 ? 'var(--success-color, #1a7f37)' : 'var(--danger-color, #b3261e)'};">${sign > 0 ? '+' : '-'}${e.points}</td>
      <td>${loyEsc(e.reference_id || '')}</td><td>${loyEsc(e.expires_at ? String(e.expires_at).slice(0, 10) : '')}</td></tr>`;
  }).join('') : '<tr><td colspan="5" class="txn-hint">No points yet.</td></tr>';
  loadCustomerContext(panel, id);
  const list = adj.adjustments || [];
  panel.querySelector('#loy-adjustments').innerHTML = list.length ? list.map(a => `<tr><td>${loyEsc(a.created_at)}</td>
      <td>${a.points > 0 ? '+' : ''}${a.points}</td><td>${loyEsc(a.reason_code)}</td><td>${loyEsc(a.note || '')}</td><td>${loyEsc(a.status)}</td></tr>`).join('')
    : '<tr><td colspan="5" class="txn-hint">No manual adjustments.</td></tr>';
}

async function adjustPoints(panel) {
  const id = panel.querySelector('#loy-customer').value.trim();
  if (!id) return;
  const codes = (await loyJSON('/api/v1/doc/ReasonCode?limit=500', [])) || [];
  const docs = (Array.isArray(codes) ? codes : (codes.data || codes.documents || [])).map(d => ({ id: d.id, ...(d.data || d) }))
    .filter(d => d.category === 'Loyalty Adjustment' && (d.status || 'Active') === 'Active');
  if (!docs.length) {
    await showCustomAlert('Adjustments need a reason, and no Loyalty Adjustment reason codes exist yet. Add one under Setup -> Reason Codes.', 'Adjust points');
    return;
  }
  const html = `<div class="form-group" style="margin-top:8px;"><label class="form-label">Points (+ to credit, - to debit)</label><input type="number" step="1" class="form-input" data-adj="points"></div>
    <div class="form-group"><label class="form-label">Reason</label><select class="form-select" data-adj="reason"><option value="">Choose a reason...</option>
      ${docs.map(c => `<option value="${loyEsc(c.id)}">${loyEsc(c.code || c.id)}${c.description ? ` - ${loyEsc(c.description)}` : ''}</option>`).join('')}</select></div>
    <div class="form-group"><label class="form-label">Note</label><input class="form-input" data-adj="note" placeholder="What happened (for the audit trail)"></div>`;
  const picked = await new Promise(resolve => {
    const backdrop = document.getElementById('custom-dialog-container');
    const extra = document.getElementById('custom-dialog-extra');
    document.getElementById('custom-dialog-title').textContent = 'Adjust points';
    document.getElementById('custom-dialog-message').textContent = 'A new ledger entry is added - nothing is edited. Above the approval value it waits for a manager under Approvals.';
    extra.innerHTML = html;
    extra.classList.remove('hidden');
    document.getElementById('custom-dialog-cancel-btn').style.display = '';
    backdrop.classList.remove('hidden');
    const done = v => {
      backdrop.classList.add('hidden');
      extra.innerHTML = '';
      extra.classList.add('hidden');
      ['custom-dialog-ok-btn', 'custom-dialog-cancel-btn', 'custom-dialog-close-btn'].forEach(b => { const el = document.getElementById(b); el.replaceWith(el.cloneNode(true)); });
      resolve(v);
    };
    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => done({
      points: parseInt(extra.querySelector('[data-adj="points"]').value, 10) || 0,
      reason_code: extra.querySelector('[data-adj="reason"]').value,
      note: extra.querySelector('[data-adj="note"]').value.trim()
    }));
    document.getElementById('custom-dialog-cancel-btn').addEventListener('click', () => done(null));
    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => done(null));
  });
  if (!picked) return;
  const res = await apiFetch('/api/v1/loyalty/adjustments', { method: 'POST', body: JSON.stringify({ customer_id: id, ...picked }) });
  if (!res || !res.ok) { await showApiError(res, 'The adjustment was not made.'); return; }
  const a = await res.json();
  await showCustomAlert(a.status === 'Posted'
    ? `${a.points > 0 ? 'Credited' : 'Debited'} ${Math.abs(a.points)} point(s).`
    : `${Math.abs(a.points)} point(s) (worth ${Number(a.points_value).toFixed(2)}) need a ${a.required_role || 'manager'}'s approval - the request is waiting under Approvals.`, 'Adjust points');
  loadCustomer(panel);
}

async function loadTiers(panel) {
  loyTiers = (await loyJSON('/api/v1/crm/loyalty-tier-rules', [])) || [];
  drawTiers(panel);
}

function drawTiers(panel) {
  const body = panel.querySelector('#loy-tiers');
  body.innerHTML = loyTiers.length ? loyTiers.map((t, i) => `<tr data-tier="${i}">
      <td><input class="form-input" data-f="tier" value="${loyEsc(t.tier)}" ${t.id ? 'readonly' : ''} placeholder="e.g. Gold"></td>
      <td><input type="number" min="0" step="1" class="form-input" data-f="min_spend" value="${t.min_spend}"></td>
      <td><input type="number" min="0.01" step="0.05" class="form-input" data-f="earn_multiplier" value="${t.earn_multiplier}"></td>
      <td><input class="form-input" data-f="benefits" value="${loyEsc(t.benefits || '')}" placeholder="e.g. Free alterations"></td>
      <td><input type="number" min="0" step="1" class="form-input" data-f="grace_days" value="${t.grace_days || 0}"></td>
      <td style="white-space:nowrap;"><button class="action-btn" type="button" data-tier-save="${i}">Save</button>
        ${t.id ? `<button class="action-btn action-btn-danger" type="button" data-tier-del="${i}">Delete</button>` : ''}</td></tr>`).join('')
    : '<tr><td colspan="6" class="txn-hint">No tiers yet - add one.</td></tr>';
  body.onclick = async e => {
    const save = e.target.dataset.tierSave, del = e.target.dataset.tierDel;
    if (save !== undefined) {
      const row = body.querySelector(`[data-tier="${save}"]`);
      const val = f => row.querySelector(`[data-f="${f}"]`).value;
      const rule = { tier: val('tier').trim(), min_spend: parseFloat(val('min_spend')) || 0, earn_multiplier: parseFloat(val('earn_multiplier')) || 1,
        benefits: val('benefits').trim(), grace_days: parseInt(val('grace_days'), 10) || 0 };
      const res = await apiFetch('/api/v1/crm/loyalty-tier-rules', { method: 'POST', body: JSON.stringify(rule) });
      if (!res || !res.ok) { await showApiError(res, 'The tier was not saved.'); return; }
      showToast(`Tier ${rule.tier} saved.`, { variant: 'success' });
      loadTiers(panel);
    }
    if (del !== undefined) {
      const t = loyTiers[Number(del)];
      if (!await showCustomConfirm(`Delete the ${t.tier} tier? Customers on it move to the tier their spend reaches next time they earn.`, 'Delete tier')) return;
      const res = await apiFetch(`/api/v1/crm/loyalty-tier-rules?id=${t.id}`, { method: 'DELETE' });
      if (!res || !res.ok) { await showApiError(res, 'The tier was not deleted.'); return; }
      loadTiers(panel);
    }
  };
}

async function runMovement(panel) {
  const params = new URLSearchParams();
  const from = panel.querySelector('#loy-from').value, to = panel.querySelector('#loy-to').value;
  if (from) params.set('from_date', from);
  if (to) params.set('to_date', to);
  const body = await loyJSON(`/api/v1/reports/run/loyalty-points-movement${params.toString() ? '?' + params : ''}`, null);
  const out = panel.querySelector('#loy-movement');
  if (!body) { out.innerHTML = '<tr><td colspan="3">The report could not be run.</td></tr>'; return; }
  out.innerHTML = (body.rows || []).map(r => {
    const strong = /balance|books/i.test(r.movement) || /DIFFERENCE/.test(r.movement);
    return `<tr${strong ? ' style="font-weight:600;"' : ''}><td>${loyEsc(r.movement)}</td><td>${r.points == null ? '' : r.points}</td>
      <td>${r.value == null ? '' : (typeof r.value === 'number' ? r.value.toFixed(2) : loyEsc(r.value))}</td></tr>`;
  }).join('');
}

// loadCustomerContext (55.17): the customer's sales side next to their
// points - what the points are worth, how far the next tier is, the last
// bills and returns, and the vouchers they can use now. The till's own
// customer context (GET /api/v1/pos/customer-context), so both screens say
// the same; left out quietly for a user without the till.
async function loadCustomerContext(panel, id) {
  const box = panel.querySelector('#loy-context');
  box.innerHTML = '';
  const c = await loyJSON(`/api/v1/pos/customer-context?customer_id=${encodeURIComponent(id)}`, null);
  if (!c || panel.querySelector('#loy-customer').value.trim() !== id) return;
  const money = n => Number(n || 0).toFixed(2);
  const rows = (list, cells, empty, cols) => list.length ? list.map(x => `<tr>${cells(x).map(v => `<td>${loyEsc(v)}</td>`).join('')}</tr>`).join('')
    : `<tr><td colspan="${cols}" class="txn-hint">${empty}</td></tr>`;
  const offers = c.consent_sms || c.consent_whatsapp
    ? `Agreed to offers by ${[c.consent_sms ? 'SMS' : '', c.consent_whatsapp ? 'WhatsApp' : ''].filter(Boolean).join(' and ')}.`
    : 'Has not agreed to offers - no SMS or WhatsApp campaign reaches them (tick it on their Customer record once they agree).';
  box.innerHTML = `<div class="txn-hint" style="margin:0 0 10px;">${loyEsc(offers)} Points worth <strong>${money(c.points_value)}</strong>.
      Spend counted for tiers: <strong>${money(c.spend)}</strong>${c.next_tier ? ` &middot; ${money(c.to_next_tier)} more reaches <strong>${loyEsc(c.next_tier)}</strong>` : ''}.</div>
    <div style="display:grid; grid-template-columns:repeat(auto-fit, minmax(280px, 1fr)); gap:12px; margin-bottom:12px;">
      <div class="table-wrapper"><table><thead><tr><th>Last bills</th><th>Date</th><th>Amount</th><th>Lines</th></tr></thead>
        <tbody>${rows(c.recent_bills || [], b => [b.cart_number, b.date, money(b.amount), b.lines], 'No bills yet.', 4)}</tbody></table></div>
      <div class="table-wrapper"><table><thead><tr><th>Returns</th><th>Bill</th><th>Status</th><th>Refund</th></tr></thead>
        <tbody>${rows(c.recent_returns || [], r => [r.id, r.bill, r.status, r.refund ? money(r.refund) : '-'], 'No returns.', 4)}</tbody></table></div>
      <div class="table-wrapper"><table><thead><tr><th>Vouchers to use</th><th>Gives</th><th>Expires</th></tr></thead>
        <tbody>${rows(c.vouchers || [], v => [v.code, v.offer, v.expires || '-'], 'None right now.', 3)}</tbody></table></div>
    </div>`;
}

// loadOffers (55.17): vouchers and campaigns in words.
async function loadOffers(panel) {
  const [v, c] = await Promise.all([
    loyJSON('/api/v1/loyalty/vouchers', null),
    loyJSON('/api/v1/loyalty/campaigns', null)
  ]);
  const money = n => Number(n || 0).toFixed(2);
  const vouchers = (v && v.vouchers) || [];
  panel.querySelector('#loy-vouchers').innerHTML = !v ? '<tr><td colspan="8" class="txn-hint">The vouchers could not be looked up.</td></tr>'
    : vouchers.length ? vouchers.map(x => `<tr><td style="font-weight:600;">${loyEsc(x.code)}</td><td>${loyEsc(x.gives)}</td><td>${loyEsc(x.for)}</td><td>${loyEsc(x.where)}</td>
        <td>${loyEsc(x.valid_from || 'now')} to ${loyEsc(x.expiry_date || 'no end')}</td><td>${x.used_count}${x.max_uses ? ` of ${x.max_uses}` : ''}</td>
        <td style="color:${x.usable ? 'var(--success-color, #1a7f37)' : 'var(--text-muted, #6b7280)'};">${loyEsc(x.state)}</td><td>${money(x.discount_given)}</td></tr>`).join('')
    : '<tr><td colspan="8" class="txn-hint">No vouchers yet. Add one with the button below.</td></tr>';
  const campaigns = (c && c.campaigns) || [];
  panel.querySelector('#loy-campaigns').innerHTML = !c ? '<tr><td colspan="11" class="txn-hint">The campaigns could not be looked up.</td></tr>'
    : campaigns.length ? campaigns.map(x => `<tr><td style="font-weight:600;">${loyEsc(x.name || x.id)}</td><td>${loyEsc(x.trigger)}</td>
        <td>${x.note ? `<span style="color:var(--danger-color, #b3261e);">${loyEsc(x.note)}</span>` : loyEsc(x.channel)}</td><td>${loyEsc(x.status)}</td>
        <td>${loyEsc(x.message_preview || '-')}</td><td>${x.note ? 'No one' : x.status === 'Active' ? `${x.picks_today} customer${x.picks_today === 1 ? '' : 's'}` : 'Paused'}</td><td>${x.skipped_no_consent || 0}</td><td>${x.sent_to}</td>
        <td>${money(x.sales_since)}</td><td>${x.cost ? money(x.cost) : '-'}</td><td>${!x.sent_to ? 'Nothing sent yet' : x.roi == null ? '-' : `${Math.round(x.roi * 100)}%`}</td></tr>`).join('')
    : '<tr><td colspan="11" class="txn-hint">No campaigns yet. Add one with the button below.</td></tr>';
}
