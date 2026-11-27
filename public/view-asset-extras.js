// Fixed assets: capital work in progress, impairment and revaluation, meter
// readings, and leases (ask 170, items 170.5-170.9). Loaded by view-assets.js,
// which owns the page; this file only adds buttons, panels and the lease list.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const money = v => formatMoney(Number(v) || 0);
const field = (id, label, input) => `<div class="form-group s-50435f"><label class="form-label" for="${id}">${label}</label>${input}</div>`;

// assetExtraButtons are the row buttons this file adds to the register.
export function assetExtraButtons(asset) {
  const id = esc(asset.id);
  const b = (act, label) => `<button class="action-btn" data-asset-action="${act}" data-asset-id="${id}">${label}</button>`;
  if (asset.status === 'Draft') return b('cwip-start', 'To CWIP');
  if (asset.status === 'CWIP') return b('cwip-capitalize', 'Capitalise');
  if (asset.status === 'Capitalised') return b('adjust', 'Adjust') + b('reading', 'Reading');
  return '';
}

// runAssetExtra handles a click on one of those buttons. ctx comes from
// view-assets.js: openPanel(title, bodyHTML, onConfirm), panelError(message), reload().
export async function runAssetExtra(action, asset, ctx) {
  const { openPanel, panelError, reload } = ctx;
  const send = async (path, body) => {
    const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(body) });
    if (!res) return null;
    if (!res.ok) { panelError(await apiErrorText(res, 'Could not save.')); return null; }
    return res.json().catch(() => ({}));
  };
  if (action === 'cwip-start') {
    openPanel(`Start CWIP: ${asset.code || asset.id}`, field('cwip-budget', 'Budget', '<input type="number" id="cwip-budget" class="form-input">'), async () => {
      const out = await send('/api/v1/assets/cwip/start', { asset_id: asset.id, budget: parseFloat(document.getElementById('cwip-budget').value) || 0 });
      if (out) { showToast('Asset is in CWIP. Charge bill lines to it.', { variant: 'success' }); reload(); }
    });
  } else if (action === 'cwip-capitalize') {
    const r = await apiFetch(`/api/v1/assets/${encodeURIComponent(asset.id)}/cwip`);
    const collected = r && r.ok ? (await r.json()).collected : 0;
    openPanel(`Capitalise: ${asset.code || asset.id}`,
      `<div class="form-group s-50435f"><div class="form-label">Collected</div><b>${money(collected)}</b></div>` +
      field('cwip-life', 'Life (yrs)', `<input type="number" id="cwip-life" class="form-input" value="${Number(asset.useful_life_years) || ''}">`) +
      field('cwip-date', 'From', `<input type="date" id="cwip-date" class="form-input" value="${localISODate(new Date())}">`), async () => {
        const out = await send('/api/v1/assets/cwip/capitalize', { asset_id: asset.id,
          useful_life_years: parseFloat(document.getElementById('cwip-life').value) || 0, date: document.getElementById('cwip-date').value });
        if (out) { showToast('Capitalised. Depreciation starts from that date.', { variant: 'success' }); reload(); }
      });
  } else if (action === 'adjust') {
    openPanel(`Adjust: ${asset.code || asset.id}`,
      field('adj-kind', 'Kind', '<select id="adj-kind" class="form-input"><option>Impairment</option><option>Revaluation</option></select>') +
      field('adj-amount', 'Loss', '<input type="number" id="adj-amount" class="form-input">') +
      field('adj-date', 'Date', `<input type="date" id="adj-date" class="form-input" value="${localISODate(new Date())}">`) +
      field('adj-reason', 'Reason', '<input type="text" id="adj-reason" class="form-input">'), async () => {
        const kind = document.getElementById('adj-kind').value, v = parseFloat(document.getElementById('adj-amount').value) || 0;
        const body = { asset_id: asset.id, kind, date: document.getElementById('adj-date').value, reason: document.getElementById('adj-reason').value.trim(),
          idempotency_key: `adj-${asset.id}-${Date.now()}` };
        if (kind === 'Impairment') body.amount = v; else body.new_value = v;
        const out = await send('/api/v1/assets/adjust', body);
        if (out) { showToast(`Book value now ${money(out.carrying_after)}.`, { variant: 'success' }); reload(); }
      });
    const kind = document.getElementById('adj-kind'), lbl = document.querySelector('label[for="adj-amount"]');
    if (kind && lbl) kind.addEventListener('change', () => { lbl.textContent = kind.value === 'Impairment' ? 'Loss' : 'New value'; });
  } else if (action === 'reading') {
    openPanel(`Reading: ${asset.code || asset.id}`,
      field('mr-date', 'Date', `<input type="date" id="mr-date" class="form-input" value="${localISODate(new Date())}">`) +
      field('mr-reading', 'Reading', '<input type="number" id="mr-reading" class="form-input">') +
      field('mr-unit', 'Unit', '<input type="text" id="mr-unit" class="form-input" placeholder="km, hours">'), async () => {
        const date = document.getElementById('mr-date').value;
        const id = `MR-${asset.id}-${date}-${Date.now().toString(36)}`;
        const out = await send('/api/v1/doc/AssetMeterReading', { id, code: id, asset: asset.id, reading_date: date,
          reading: parseFloat(document.getElementById('mr-reading').value), unit: document.getElementById('mr-unit').value.trim() });
        if (out) { showToast('Reading saved.', { variant: 'success' }); reload(); }
      });
  }
}

async function apiErrorText(res, fallback) {
  try { const d = await res.json(); return d.error || d.message || fallback; } catch (e) { return fallback; }
}

// ----------------------------------------------------------------- leases

const scheduleRows = v => { try { const a = typeof v === 'string' ? JSON.parse(v || '[]') : v; return Array.isArray(a) ? a : []; } catch (e) { return []; } };

// renderLeaseSection draws the Leases page (288.2: its own page, assets:leases;
// list first, New on its own page).
export async function renderLeaseSection(container) {
  const r = await apiFetch('/api/v1/doc/Lease?limit=500');
  const body = r && r.ok ? await r.json() : [];
  const leases = Array.isArray(body) ? body : (body.data || []);

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.cssText = 'padding: 24px; margin-bottom: 24px;';
  form.innerHTML = `<div class="s-608a43">
    ${field('ls-code', 'Lease no', '<input type="text" id="ls-code" class="form-input">')}
    ${field('ls-type', 'Type', '<select id="ls-type" class="form-input"><option>Lessee</option><option>Lessor</option></select>')}
    ${field('ls-party', 'Vendor', '<input type="text" id="ls-party" class="form-input" autocomplete="off">')}
    ${field('ls-asset', 'Asset leased out', '<input type="text" id="ls-asset" class="form-input" autocomplete="off">')}
    ${field('ls-start', 'Start', `<input type="date" id="ls-start" class="form-input" value="${localISODate(new Date())}">`)}
    ${field('ls-term', 'Months', '<input type="number" id="ls-term" class="form-input">')}
    ${field('ls-pay', 'Payment a month', '<input type="number" id="ls-pay" class="form-input">')}
    ${field('ls-rate', 'Rate % a year', '<input type="number" id="ls-rate" class="form-input">')}
    ${field('ls-timing', 'Paid', '<select id="ls-timing" class="form-input"><option>Arrears</option><option>Advance</option></select>')}
    <button class="btn btn-primary" id="ls-save" type="button">Save</button>
  </div><div id="ls-error" class="login-error hidden s-4c0544"></div>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table><thead><tr><th>Lease</th><th>Type</th><th>Start</th><th>Months</th><th>Payment</th><th>Liability</th><th>Posted</th><th>Status</th><th></th></tr></thead><tbody>
    ${leases.length === 0 ? '<tr><td colspan="9" class="s-306094">No leases.</td></tr>' : leases.map(l => `
      <tr><td>${esc(l.code || l.id)}</td><td>${esc(l.lease_type)}</td><td>${esc(l.start_date)}</td><td>${esc(l.term_months)}</td><td>${money(l.payment_amount)}</td>
        <td>${l.liability_opening ? money(l.liability_opening) : ''}</td><td>${esc(l.periods_posted || 0)}</td>
        <td><span class="badge ${l.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${esc(l.status)}</span></td>
        <td>${(l.status || 'Draft') === 'Draft' ? `<button class="action-btn" data-lease-act="activate" data-id="${esc(l.id)}">Activate</button>` : ''}
          ${scheduleRows(l.schedule).length ? `<button class="action-btn" data-lease-act="schedule" data-id="${esc(l.id)}">Schedule</button>` : ''}</td></tr>
      <tr class="hidden" data-lease-detail="${esc(l.id)}"><td colspan="9">${leaseScheduleTable(l)}</td></tr>`).join('')}
  </tbody></table>`;

  const extra = '<button class="btn btn-outline" type="button" id="ls-run">Post due</button>';
  container.appendChild(txnPagedSection({ id: 'lease-screen', title: 'Leases', page: 'New lease', count: leases.length, newLabel: 'New Lease', form, list, extra }));
  txnRestorePane('lease-screen');

  const syncType = () => {
    const lessor = form.querySelector('#ls-type').value === 'Lessor';
    form.querySelector('label[for="ls-party"]').textContent = lessor ? 'Customer' : 'Vendor';
    form.querySelector('#ls-asset').closest('.form-group').classList.toggle('hidden', !lessor);
  };
  form.querySelector('#ls-type').addEventListener('change', syncType);
  syncType();
  attachLinkTypeahead(form.querySelector('#ls-asset'), 'Asset');

  const reload = () => renderView('assets:leases');
  const fail = m => { const el = form.querySelector('#ls-error'); el.textContent = m; el.classList.remove('hidden'); };
  form.querySelector('#ls-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const code = form.querySelector('#ls-code').value.trim();
    const lessor = form.querySelector('#ls-type').value === 'Lessor';
    if (!code) { fail('Lease no is required.'); return; }
    const data = { id: code, code, lease_type: form.querySelector('#ls-type').value, start_date: form.querySelector('#ls-start').value,
      term_months: parseInt(form.querySelector('#ls-term').value, 10) || 0, payment_amount: parseFloat(form.querySelector('#ls-pay').value) || 0,
      discount_rate: parseFloat(form.querySelector('#ls-rate').value) || 0, payment_timing: form.querySelector('#ls-timing').value, status: 'Draft' };
    data[lessor ? 'customer' : 'vendor'] = form.querySelector('#ls-party').value.trim();
    if (lessor) data.asset = form.querySelector('#ls-asset').value.trim();
    const res = await apiFetch('/api/v1/doc/Lease', { method: 'POST', body: JSON.stringify(data) });
    if (!res) return;
    if (!res.ok) { fail(await apiErrorText(res, 'Could not save the lease.')); return; }
    txnDone('lease-screen');
    reload();
  }));
  list.addEventListener('click', async ev => {
    const btn = ev.target.closest('[data-lease-act]');
    if (!btn) return;
    if (btn.dataset.leaseAct === 'schedule') {
      list.querySelector(`[data-lease-detail="${CSS.escape(btn.dataset.id)}"]`)?.classList.toggle('hidden');
      return;
    }
    guardAgainstDoubleSubmit(btn, 'Working...', async () => {
      const res = await apiFetch(`/api/v1/assets/leases/${encodeURIComponent(btn.dataset.id)}/activate`, { method: 'POST', body: '{}' });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Could not activate the lease.', 'Lease'); return; }
      showToast('Lease active. The books show it from the start date.', { variant: 'success' });
      reload();
    });
  });
  const run = container.querySelector('#ls-run');
  if (run) run.addEventListener('click', () => guardAgainstDoubleSubmit(run, 'Working...', async () => {
    const res = await apiFetch('/api/v1/assets/leases/run', { method: 'POST', body: '{}' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not post.', 'Lease'); return; }
    const out = await res.json().catch(() => ({}));
    showToast(`${out.periods_posted || 0} month(s) posted.`, { variant: 'success' });
    reload();
  }));
}

function leaseScheduleTable(l) {
  const rows = scheduleRows(l.schedule);
  if (!rows.length) return '';
  const lessor = l.lease_type === 'Lessor';
  const p = v => money((Number(v) || 0) / 100);
  return `<table><thead><tr><th>Month</th>${lessor ? '' : '<th>Opening</th><th>Interest</th>'}<th>Payment</th>${lessor ? '' : '<th>Closing</th><th>Depreciation</th>'}<th>Posted</th></tr></thead><tbody>
    ${rows.map(r => `<tr><td>${esc(r.month)}</td>${lessor ? '' : `<td>${p(r.opening)}</td><td>${p(r.interest)}</td>`}<td>${p(r.payment)}</td>${lessor ? '' : `<td>${p(r.closing)}</td><td>${p(r.depreciation)}</td>`}<td>${r.posted ? 'Yes' : ''}</td></tr>`).join('')}
  </tbody></table>`;
}
