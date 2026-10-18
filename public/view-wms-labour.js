// Labour & billing (Stage 55.8) - Warehouse Cockpit → Labour / Billing.
//
// Labour: the plan (open warehouse tasks turned into hours and headcount, with
// tasks that have no standard counted as uncovered rather than guessed), each
// labour standard's worked example for a quantity, and a step-by-step setup
// for a new standard - operation, work elements, travel, allowance - with the
// units on every field. Billing: one owner's period with the arithmetic behind
// every captured charge and storage line, what needs a look before invoicing,
// and the two existing actions (capture storage charges, raise the invoice).
// The masters stay ordinary records; captured charges and snapshots are
// written only by the system.
//
// BLD-041: native ES module loaded with import() by the Warehouse Cockpit.

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const num = (v, d = 2) => { const x = Number(v) || 0; return x === Math.round(x) ? String(x) : x.toFixed(d); };
const muted = t => `<div style="font-size: 11px; color: var(--text-muted);">${t}</div>`;
const today = () => localISODate(new Date());
const monthStart = () => today().slice(0, 8) + '01';

async function listOf(doctype) {
  const r = await apiFetch(`/api/v1/doc/${doctype}?limit=500`);
  if (!r || !r.ok) return [];
  const d = await r.json();
  return Array.isArray(d) ? d : (d.data || []);
}

async function post(path, body) {
  const r = await apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!r) return { ok: false };
  if (!r.ok) return { ok: false, res: r };
  return { ok: true, data: await r.json().catch(() => ({})) };
}

// ---------------------------------------------------------------- labour

async function renderLabourTab(container, location) {
  const plan = document.createElement('div');
  plan.className = 'table-panel';
  plan.style.padding = '20px';
  plan.style.marginBottom = '16px';
  plan.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin: 0 0 12px;">Labour plan</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="lp-start">From</label><input type="date" id="lp-start" class="form-input" value="${today()}"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="lp-end">To</label><input type="date" id="lp-end" class="form-input" value="${today()}"></div>
      <button class="btn btn-primary" id="lp-run" type="button">Plan${location ? ' for ' + esc(location) : ' (all locations)'}</button>
    </div>
    <div id="lp-result" style="margin-top: 12px;"></div>`;
  container.appendChild(plan);
  plan.querySelector('#lp-run').addEventListener('click', async () => {
    const el = plan.querySelector('#lp-result');
    const q = new URLSearchParams({ location_code: location || '', start: plan.querySelector('#lp-start').value, end: plan.querySelector('#lp-end').value });
    const r = await apiFetch(`/api/v1/wms/labor/plan?${q}`);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The plan could not be worked out.', 'Labour plan'); return; }
    const rows = (await r.json()).rows || [];
    el.innerHTML = rows.length === 0 ? '<p style="margin: 0; color: var(--text-muted);">No open warehouse tasks (Pending, Assigned or In Progress) to plan for.</p>' : `<div style="overflow-x: auto;"><table>
      <thead><tr><th>Department</th><th class="num">Open tasks</th><th class="num">No standard</th><th class="num">Planned hours</th><th class="num">Hours per worker</th><th class="num">Headcount</th></tr></thead>
      <tbody>${rows.map(p => `<tr><td>${esc(p.department)}</td><td class="num">${p.open_tasks}</td>
        <td class="num">${p.uncovered_tasks ? `<b style="color: var(--danger, #b91c1c);">${p.uncovered_tasks}</b>` : 0}</td>
        <td class="num">${num(p.planned_hours, 3)}</td><td class="num">${num(p.available_hours_per_worker, 2)}</td><td class="num"><b>${p.forecast_headcount}</b></td></tr>`).join('')}</tbody></table></div>
      <p style="font-size: 12px; color: var(--text-muted); margin: 8px 0 0;">Headcount = planned hours ÷ hours one worker has in the period (from the department's weekly schedule and shift, or 8 h a day), rounded up. Tasks under <b>No standard</b> are not in the hours - give their task type a labour standard below.</p>`;
  });

  const [standards, operations] = await Promise.all([listOf('LaborStandard'), listOf('LaborOperation')]);
  const std = document.createElement('div');
  std.className = 'table-panel';
  std.style.padding = '20px';
  std.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-bottom: 12px;">
      <h2 style="font-size: 16px; font-weight: 700; margin: 0; flex: 1;">Labour standards</h2>
      <button class="btn btn-primary" id="ls-new" type="button">+ New labour standard</button>
    </div>
    ${standards.length === 0 ? '<p style="margin: 0; color: var(--text-muted);">No labour standards yet. A standard says how long one warehouse task takes: its work elements, travel and allowances.</p>' : `<div style="overflow-x: auto;"><table>
      <thead><tr><th>Standard</th><th>Operation</th><th>Elements</th><th>Travel</th><th>Allowances</th><th>Worked example</th></tr></thead>
      <tbody>${standards.map(s => {
        const op = operations.find(o => o.code === s.operation_code);
        return `<tr><td><b>${esc(s.code)}</b>${muted(esc(s.name || ''))}</td>
          <td>${esc(s.operation_code)}${op ? muted(`${esc(op.task_type)} · ${op.labor_rate_per_hour ? esc(num(op.labor_rate_per_hour)) + ' per hour' : 'no hourly cost'}`) : muted('operation missing')}</td>
          <td>${esc(s.element_codes || '')}</td><td>${esc(s.travel_section_code || '-')}</td><td>${esc(s.allowance_codes || '-')}</td>
          <td style="white-space: nowrap;"><input type="number" class="form-input ls-qty" data-code="${esc(s.code)}" value="10" min="1" step="any" style="width: 80px;" aria-label="Quantity for ${esc(s.code)}">
            <button class="action-btn" data-ls-preview="${esc(s.code)}" type="button">Preview</button></td></tr>
          <tr class="hidden" id="ls-detail-${esc(s.code)}"><td colspan="6"></td></tr>`;
      }).join('')}</tbody></table></div>`}`;
  container.appendChild(std);
  std.querySelector('#ls-new').addEventListener('click', () => openStandardWizard(operations));
  std.querySelectorAll('[data-ls-preview]').forEach(btn => btn.addEventListener('click', () => previewStandard(btn.dataset.lsPreview, std.querySelector(`.ls-qty[data-code="${CSS.escape(btn.dataset.lsPreview)}"]`).value)));
}

async function previewStandard(code, qty) {
  const row = document.getElementById(`ls-detail-${code}`);
  if (!row) return;
  const r = await apiFetch(`/api/v1/wms/labor/standard-preview?code=${encodeURIComponent(code)}&qty=${encodeURIComponent(qty)}`);
  if (!r) return;
  if (!r.ok) { await showApiError(r, 'The standard could not be worked out.', 'Labour standard'); return; }
  const p = await r.json();
  row.firstElementChild.innerHTML = `<table>
    <thead><tr><th>Part</th><th>Code</th><th>How it is worked out</th><th class="num">Seconds</th></tr></thead>
    <tbody>${(p.lines || []).map(l => `<tr><td>${esc(l.component)}</td><td>${esc(l.code)}</td><td>${esc(l.basis)}</td><td class="num">${num(l.seconds)}</td></tr>`).join('')}
      <tr><td colspan="3"><b>Total for ${esc(num(p.quantity))} unit(s)</b></td><td class="num"><b>${num(p.total_seconds)}</b></td></tr></tbody></table>
    <p style="margin: 8px 0 0;">= ${esc(num(p.minutes))} minutes${p.labor_rate_per_hour ? ` · cost ${esc(num(p.estimated_labor_cost))} at ${esc(num(p.labor_rate_per_hour))} per hour` : ' · no hourly cost on the operation'}
      ${p.agrees ? '<span class="badge badge-success">matches the planning engine</span>' : '<span class="badge badge-danger">differs from the planning engine - check the masters</span>'}</p>`;
  row.classList.remove('hidden');
}

// openStandardWizard sets up a standard in one pass: operation (existing or
// new), work elements, optional travel and allowance, then the standard -
// each saved as its ordinary master record, in that order.
function openStandardWizard(operations) {
  document.getElementById('ls-wizard')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'ls-wizard';
  const n = (id, label, unit, w = 110) => `<div class="form-group" style="margin: 0;"><label class="form-label" for="${id}">${label}${unit ? ` <span style="color: var(--text-muted);">(${unit})</span>` : ''}</label><input type="number" id="${id}" class="form-input" min="0" step="any" style="width: ${w}px;"></div>`;
  const t = (id, label, w = 160) => `<div class="form-group" style="margin: 0;"><label class="form-label" for="${id}">${label}</label><input type="text" id="${id}" class="form-input" style="width: ${w}px;"></div>`;
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 860px;" role="dialog" aria-modal="true" aria-labelledby="ls-wizard-title">
      <div class="modal-header"><h3 class="modal-title" id="ls-wizard-title">New labour standard</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-ls-close><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></button></div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 16px;">
        <section><b>1. Operation</b> - the kind of task and what an hour of it costs
          <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; margin-top: 8px;">
            <div class="form-group" style="margin: 0;"><label class="form-label" for="lw-op">Operation</label>
              <select id="lw-op" class="form-select"><option value="">New operation...</option>${operations.map(o => `<option value="${esc(o.code)}">${esc(o.code)} - ${esc(o.task_type)}</option>`).join('')}</select></div>
            <div id="lw-op-new" style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
              ${t('lw-op-code', 'Code', 120)}${t('lw-op-name', 'Name')}
              <div class="form-group" style="margin: 0;"><label class="form-label" for="lw-op-type">Task type</label><select id="lw-op-type" class="form-select">${['Putaway', 'Pick', 'Replenish', 'Count', 'Move', 'VAS', 'Load', 'Unload'].map(x => `<option>${x}</option>`).join('')}</select></div>
              ${t('lw-op-dept', 'Department', 130)}${n('lw-op-rate', 'Cost per hour', 'money')}
            </div></div></section>
        <section><b>2. Work elements</b> - the steps of one task
          <table style="margin-top: 8px;"><thead><tr><th>Code</th><th>Description</th><th class="num">Fixed (seconds per task)</th><th class="num">Per unit (seconds)</th><th></th></tr></thead><tbody id="lw-elements"></tbody></table>
          <button type="button" class="btn btn-outline btn-sm" id="lw-add-el" style="margin-top: 6px;">+ Add element</button></section>
        <section><b>3. Travel</b> (optional) - walking or driving to the work
          <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; margin-top: 8px;">${t('lw-tr-code', 'Code', 120)}${n('lw-tr-task', 'Per task', 'seconds')}${n('lw-tr-unit', 'Per unit', 'seconds')}</div></section>
        <section><b>4. Allowance</b> (optional) - rest and delay on top of the work and travel
          <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; margin-top: 8px;">${t('lw-al-code', 'Code', 120)}${n('lw-al-pct', 'Allowance', '% of work + travel')}${n('lw-al-fixed', 'Fixed', 'seconds per task')}</div></section>
        <section><b>5. The standard</b>
          <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; margin-top: 8px;">${t('lw-code', 'Standard code', 140)}${t('lw-name', 'Name', 220)}</div></section>
        <div id="lw-status" style="font-size: 13px;"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-ls-close>Close</button><button type="button" class="btn btn-primary" id="lw-save">Save standard</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-ls-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const opSel = overlay.querySelector('#lw-op');
  opSel.addEventListener('change', () => { overlay.querySelector('#lw-op-new').style.display = opSel.value ? 'none' : 'flex'; });
  const addEl = () => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td><input type="text" class="form-input lw-el-code" style="width: 100px;" aria-label="Element code"></td><td><input type="text" class="form-input lw-el-desc" aria-label="Element description"></td>
      <td class="num"><input type="number" class="form-input lw-el-fixed" min="0" step="any" style="width: 90px;" aria-label="Fixed seconds"></td>
      <td class="num"><input type="number" class="form-input lw-el-unit" min="0" step="any" style="width: 90px;" aria-label="Seconds per unit"></td>
      <td><button type="button" class="action-btn">Remove</button></td>`;
    tr.querySelector('button').addEventListener('click', () => tr.remove());
    overlay.querySelector('#lw-elements').appendChild(tr);
  };
  overlay.querySelector('#lw-add-el').addEventListener('click', addEl);
  addEl();
  const v = id => overlay.querySelector('#' + id).value.trim();
  const nv = id => parseFloat(overlay.querySelector('#' + id).value) || 0;
  const status = overlay.querySelector('#lw-status');
  const save = overlay.querySelector('#lw-save');
  save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
    const say = (msg, bad) => { status.innerHTML = bad ? `<div class="login-error">${esc(msg)}</div>` : esc(msg); };
    const opCode = opSel.value || v('lw-op-code');
    const elements = [...overlay.querySelectorAll('#lw-elements tr')].map(tr => ({ code: tr.querySelector('.lw-el-code').value.trim(), description: tr.querySelector('.lw-el-desc').value.trim(),
      fixed: parseFloat(tr.querySelector('.lw-el-fixed').value) || 0, unit: parseFloat(tr.querySelector('.lw-el-unit').value) || 0 })).filter(e => e.code || e.description);
    if (!opCode) return say('Choose an operation or give the new one a code.', true);
    if (!opSel.value && !v('lw-op-name')) return say('Give the new operation a name.', true);
    if (!elements.length || elements.some(e => !e.code || !e.description)) return say('Add at least one work element, each with a code and a description.', true);
    if (!v('lw-code') || !v('lw-name')) return say('Give the standard a code and a name.', true);
    const steps = [];
    if (!opSel.value) steps.push(['operation ' + opCode, 'LaborOperation', { id: opCode, code: opCode, name: v('lw-op-name'), task_type: overlay.querySelector('#lw-op-type').value, department: v('lw-op-dept'), labor_rate_per_hour: nv('lw-op-rate'), status: 'Active' }]);
    elements.forEach(e => steps.push(['element ' + e.code, 'LaborElement', { id: e.code, code: e.code, operation_code: opCode, description: e.description, fixed_seconds: e.fixed, standard_seconds_per_unit: e.unit, status: 'Active' }]));
    if (v('lw-tr-code')) steps.push(['travel ' + v('lw-tr-code'), 'TravelSection', { id: v('lw-tr-code'), code: v('lw-tr-code'), description: 'Travel for ' + v('lw-code'), seconds_per_task: nv('lw-tr-task'), seconds_per_unit: nv('lw-tr-unit'), status: 'Active' }]);
    if (v('lw-al-code')) steps.push(['allowance ' + v('lw-al-code'), 'LaborAllowance', { id: v('lw-al-code'), code: v('lw-al-code'), description: 'Allowance for ' + v('lw-code'), allowance_pct: nv('lw-al-pct'), fixed_seconds: nv('lw-al-fixed'), status: 'Active' }]);
    steps.push(['standard ' + v('lw-code'), 'LaborStandard', { id: v('lw-code'), code: v('lw-code'), name: v('lw-name'), operation_code: opCode, element_codes: elements.map(e => e.code).join(','),
      travel_section_code: v('lw-tr-code'), allowance_codes: v('lw-al-code'), status: 'Active' }]);
    for (const [label, doctype, body] of steps) {
      say(`Saving ${label}...`);
      const r = await post(`/api/v1/doc/${doctype}`, body);
      if (!r.ok) { if (r.res) await showApiError(r.res, `The ${label} could not be saved - the steps before it were saved.`, 'Labour standard'); return say(`Stopped at the ${label}.`, true); }
    }
    close();
    showToast(`Labour standard ${v('lw-code')} saved.`, { variant: 'success' });
    renderView('warehouse-cockpit');
  }));
}

// ---------------------------------------------------------------- billing

async function renderBillingTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="bl-owner">Owner (client)</label><input type="text" id="bl-owner" class="form-input" style="width: 180px;"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="bl-start">From</label><input type="date" id="bl-start" class="form-input" value="${monthStart()}"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="bl-end">To</label><input type="date" id="bl-end" class="form-input" value="${today()}"></div>
      <button class="btn btn-primary" id="bl-run" type="button">Review</button>
    </div>
    <p style="font-size: 12px; color: var(--text-muted); margin: 8px 0 0;">Amounts follow your charge codes, rate groups and contracts. Review the points listed before raising an invoice - what to charge is your decision, the system only applies it.</p>
    <div id="bl-result" style="margin-top: 16px;"></div>`;
  container.appendChild(panel);
  panel.querySelector('#bl-run').addEventListener('click', () => loadBillingReview(panel));
}

async function loadBillingReview(panel) {
  const owner = panel.querySelector('#bl-owner').value.trim(), start = panel.querySelector('#bl-start').value, end = panel.querySelector('#bl-end').value;
  const el = panel.querySelector('#bl-result');
  const r = await apiFetch(`/api/v1/wms/billing/review?${new URLSearchParams({ owner, start, end })}`);
  if (!r) return;
  if (!r.ok) { await showApiError(r, 'The review could not be run.', 'Billing'); return; }
  const b = await r.json();
  el.innerHTML = `
    <p style="margin: 0 0 12px;">Captured ${esc(num(b.captured_total))} · not yet invoiced <b>${esc(num(b.not_invoiced_total))}</b> · storage still to capture ${esc(num(b.storage_to_capture_total))}</p>
    ${b.exceptions.length ? `<div class="table-panel" style="padding: 12px; margin-bottom: 12px; border-left: 3px solid var(--warning, #b45309);"><b>Check before invoicing</b>
      <ul style="margin: 6px 0 0; padding-left: 18px;">${b.exceptions.map(x => `<li><b>${esc(x.kind)}</b> - ${esc(x.detail)}</li>`).join('')}</ul></div>` : '<p style="margin: 0 0 12px;"><span class="badge badge-success">Nothing to check</span></p>'}
    <h3 style="font-size: 14px; margin: 0 0 6px;">Captured charges</h3>
    ${b.charges.length === 0 ? '<p style="color: var(--text-muted);">No charges captured for this owner in the period.</p>' : `<div style="overflow-x: auto;"><table>
      <thead><tr><th>Charge</th><th>From</th><th>Date</th><th>How it is worked out</th><th class="num">Total</th><th>Invoice</th></tr></thead>
      <tbody>${b.charges.map(c => `<tr><td>${esc(c.charge_code)}${muted(esc(c.id))}</td><td>${esc(c.trigger_event)}${muted(esc(c.event_key))}</td><td>${esc(c.occurred_on)}</td>
        <td style="font-size: 12px;">${esc(c.explanation)}</td><td class="num">${esc(num(c.total_amount))}</td><td>${c.invoice_id ? esc(c.invoice_id) : '<span class="badge badge-warning">not invoiced</span>'}</td></tr>`).join('')}</tbody></table></div>`}
    <h3 style="font-size: 14px; margin: 16px 0 6px;">Storage for the period</h3>
    ${b.storage.length === 0 ? '<p style="color: var(--text-muted);">No storage rates for this owner.</p>' : `<div style="overflow-x: auto;"><table>
      <thead><tr><th>Rate</th><th>Location</th><th class="num">Days with a snapshot</th><th>How it is worked out</th><th class="num">Total</th></tr></thead>
      <tbody>${b.storage.map(s => `<tr><td>${esc(s.rate_code)}${s.item ? muted(esc(s.item)) : ''}</td><td>${esc(s.location_code)}</td><td class="num">${s.snapshot_days} / ${s.period_days}</td>
        <td style="font-size: 12px;">${esc(s.explanation)}</td><td class="num">${esc(num(s.total_amount))}</td></tr>`).join('')}</tbody></table></div>`}
    <div style="display: flex; gap: 12px; margin-top: 16px; flex-wrap: wrap;">
      <button class="btn btn-outline" id="bl-capture" type="button" ${b.storage.length ? '' : 'disabled'}>Capture storage charges for the period</button>
      <button class="btn btn-primary" id="bl-invoice" type="button" ${b.not_invoiced_total > 0 ? '' : 'disabled'}>Raise invoice for ${esc(num(b.not_invoiced_total))}</button>
    </div>`;
  el.querySelector('#bl-capture').addEventListener('click', async () => {
    if (!await showCustomConfirm(`Capture the storage charges for ${owner}, ${start} to ${end}? Each rate is captured once for this period.`, 'Capture storage')) return;
    const res = await post('/api/v1/wms/billing/storage/capture', { owner_id: owner, start, end });
    if (!res.ok) { if (res.res) await showApiError(res.res, 'Storage charges could not be captured.', 'Billing'); return; }
    showToast(`${res.data.captured ?? 0} storage charge(s) captured.`);
    loadBillingReview(panel);
  });
  el.querySelector('#bl-invoice').addEventListener('click', async () => {
    if (!await showCustomConfirm(`Raise an invoice to ${owner} for the ${b.charges.filter(c => !c.invoice_id).length} charge(s) not yet invoiced (${num(b.not_invoiced_total)})? It is posted like any credit sale.`, 'Raise invoice')) return;
    const res = await post('/api/v1/wms/billing/invoices', { owner_id: owner, start, end });
    if (!res.ok) { if (res.res) await showApiError(res.res, 'The invoice could not be raised.', 'Billing'); return; }
    showToast(`Invoice ${res.data.invoice_id || ''} raised for ${num(res.data.total_amount)}.`, { variant: 'success' });
    loadBillingReview(panel);
  });
}

export { renderLabourTab, renderBillingTab, previewStandard, openStandardWizard, loadBillingReview };
