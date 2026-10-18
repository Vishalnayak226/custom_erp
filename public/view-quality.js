// Quality & maintenance (Stage 55.13) - Manufacturing → Quality Inspections,
// Certificates, Non-conformance and Maintenance.
//
// Inspections name what they inspect (goods received on a GRN, a production
// order's output, goods leaving on a sales order), take readings against an
// inspection plan, and end in a decision: production inspections go to the
// approvals inbox (the order's QC gate reads them); incoming and outgoing ones
// are decided here - a pass is accepted, a fail is held, sent to rework or
// used as-is by concession, and raises a non-conformance report. Reports are
// investigated, given an owned corrective action and closed on evidence.
// Maintenance orders are scheduled, started and completed with their
// checklist, parts, hours and notes.
//
// BLD-041: native ES module loaded with import() by the Manufacturing view.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const num = v => { const x = Number(v) || 0; return x === Math.round(x) ? String(x) : x.toFixed(2); };
const today = () => localISODate(new Date());
const muted = t => `<div style="font-size: 11px; color: var(--text-muted);">${t}</div>`;
const rowsOf = v => { try { const a = typeof v === 'string' ? JSON.parse(v || '[]') : v; return Array.isArray(a) ? a : []; } catch (e) { return []; } };

async function listOf(doctype) {
  const r = await apiFetch(`/api/v1/doc/${doctype}?limit=500`);
  if (!r || !r.ok) return [];
  const d = await r.json();
  return Array.isArray(d) ? d : (d.data || []);
}

async function getDoc(doctype, id) {
  const r = await apiFetch(`/api/v1/doc/${doctype}/${encodeURIComponent(id)}`);
  return r && r.ok ? r.json() : null;
}

async function post(path, body, failMsg, title) {
  const r = await apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!r) return null;
  if (!r.ok) { await showApiError(r, failMsg, title); return null; }
  return r.json().catch(() => ({}));
}

// openQualityDialog shows a modal; onSave returns true to close it.
function openQualityDialog(title, bodyHTML, saveLabel, onSave) {
  document.getElementById('quality-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'quality-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 720px;" role="dialog" aria-modal="true" aria-labelledby="quality-dialog-title">
      <div class="modal-header"><h3 class="modal-title" id="quality-dialog-title">${esc(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-q-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">${bodyHTML}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-q-close>Cancel</button>
        ${saveLabel ? `<button type="button" class="btn btn-primary" data-q-save>${esc(saveLabel)}</button>` : ''}</div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-q-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const save = overlay.querySelector('[data-q-save]');
  if (save) save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => { if (await onSave(overlay)) close(); }));
  overlay.querySelector('select, input, textarea')?.focus();
  return overlay;
}

// A readings table: parameter, spec (inspections only), reading, Pass/Fail.
function readingsTable(id, withSpec) {
  return `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Parameter</th>${withSpec ? '<th>Specification</th>' : ''}<th>${withSpec ? 'Reading' : 'Actual value'}</th><th>Pass / Fail</th><th></th></tr></thead>
    <tbody id="${id}"></tbody></table></div>
    <button type="button" class="btn btn-outline btn-sm" data-add-reading="${id}" style="margin-top: 8px;">+ Add reading</button>`;
}

function addReadingRow(tbody, r = {}, fixed = false) {
  const withSpec = tbody.dataset.spec === '1';
  const tr = document.createElement('tr');
  tr.innerHTML = `
    <td><input type="text" class="form-input rd-param" value="${esc(r.parameter_name)}" ${fixed ? 'readonly' : ''} aria-label="Parameter"></td>
    ${withSpec ? `<td><input type="text" class="form-input rd-spec" value="${esc(r.spec)}" ${fixed ? 'readonly' : ''} aria-label="Specification"></td>` : ''}
    <td><input type="text" class="form-input rd-reading" aria-label="Reading"></td>
    <td><select class="form-select rd-result" aria-label="Pass or fail"><option value="">Choose...</option><option>Pass</option><option>Fail</option></select></td>
    <td>${fixed ? '' : '<button type="button" class="action-btn" aria-label="Remove reading">Remove</button>'}</td>`;
  tr.querySelector('button')?.addEventListener('click', () => tr.remove());
  tbody.appendChild(tr);
}

function readReadings(tbody) {
  return [...tbody.querySelectorAll('tr')].map(tr => ({
    parameter_name: tr.querySelector('.rd-param').value.trim(),
    spec: tr.querySelector('.rd-spec')?.value.trim() || '',
    reading: tr.querySelector('.rd-reading').value.trim(),
    pass_fail: tr.querySelector('.rd-result').value
  })).filter(r => r.parameter_name || r.reading);
}

function wireReadings(root, tbodyId, withSpec, planSelect) {
  const tbody = root.querySelector('#' + tbodyId);
  tbody.dataset.spec = withSpec ? '1' : '0';
  root.querySelector(`[data-add-reading="${tbodyId}"]`).addEventListener('click', () => addReadingRow(tbody));
  addReadingRow(tbody);
  if (!planSelect) return;
  planSelect.addEventListener('change', async () => {
    tbody.innerHTML = '';
    if (!planSelect.value) { addReadingRow(tbody); return; }
    const plan = await getDoc('InspectionPlan', planSelect.value);
    const params = rowsOf(plan && plan.test_parameters);
    params.forEach(p => addReadingRow(tbody, p, true));
    if (!params.length) addReadingRow(tbody);
  });
}

function readingsDetail(rows, withSpec) {
  if (!rows.length) return '<p style="color: var(--text-muted); margin: 0;">No readings recorded.</p>';
  return `<table><thead><tr><th>Parameter</th>${withSpec ? '<th>Specification</th>' : ''}<th>Reading</th><th>Result</th></tr></thead><tbody>
    ${rows.map(r => `<tr><td>${esc(r.parameter_name)}</td>${withSpec ? `<td>${esc(r.spec)}</td>` : ''}<td>${esc(r.reading ?? r.actual_value)}</td>
      <td>${(r.pass_fail || '') === 'Fail' ? '<b style="color: var(--danger, #b91c1c);">Fail</b>' : esc(r.pass_fail)}</td></tr>`).join('')}</tbody></table>`;
}

function toggleDetail(rowId, html) {
  const row = document.getElementById(rowId);
  if (!row) return;
  if (!row.classList.contains('hidden')) { row.classList.add('hidden'); return; }
  row.firstElementChild.innerHTML = html;
  row.classList.remove('hidden');
}

const resultBadge = r => r ? `<span class="badge ${r === 'Pass' ? 'badge-success' : 'badge-danger'}">${esc(r)}</span>` : '';
const sourceDoctype = { Incoming: 'GRN', Production: 'ProductionOrder', Outgoing: 'SalesOrder' };
const sourceLabel = { Incoming: 'GRN', Production: 'Production order', Outgoing: 'Sales order' };

// ---------------------------------------------------------------- inspections

async function renderInspections(container) {
  const [qis, plans] = await Promise.all([listOf('QualityInspection'), listOf('InspectionPlan')]);
  const decision = q => {
    const prod = !q.source_type || q.source_type === 'Production';
    if (q.status === 'Draft') return prod ? 'Submit for approval - the order\'s QC gate reads it' : 'Awaiting a decision';
    if (q.status === 'Pending Approval') return 'Waiting in Approvals';
    const parts = [q.disposition || q.status];
    if (q.hold_id) parts.push(`stock held (${esc(q.hold_id)}) - released only through an approved hold release`);
    if (q.disposition_note) parts.push(`"${esc(q.disposition_note)}"`);
    if (q.disposed_by) parts.push(`by ${esc(q.disposed_by)}`);
    return parts.join(' · ');
  };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = qis.length === 0 ? '<p style="padding: 24px; color: var(--text-muted);">No inspections yet. Use <b>+ New Inspection</b>.</p>' : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Inspection</th><th>Inspecting</th><th>Item / lot</th><th class="num">Qty</th><th>Result</th><th>Decision</th><th></th></tr></thead>
    <tbody>${qis.map(q => {
      const type = q.source_type || 'Production', src = q.source_id || q.production_order_id || '';
      const acts = [`<button class="action-btn" data-qi-act="readings" data-id="${esc(q.id)}">Readings</button>`];
      if (q.status === 'Draft' && type === 'Production') acts.push(`<button class="action-btn" data-qi-act="submit" data-id="${esc(q.id)}">Submit for approval</button>`);
      if (q.status === 'Draft' && type !== 'Production') acts.push(`<button class="action-btn" data-qi-act="decide" data-id="${esc(q.id)}">Decide</button>`);
      if (q.ncr_id) acts.push(`<button class="action-btn" data-qi-act="ncr" data-id="${esc(q.ncr_id)}">${esc(q.ncr_id)}</button>`);
      return `<tr><td style="font-family: monospace;">${esc(q.code || q.id)}${q.inspected_by ? muted('by ' + esc(q.inspected_by)) : ''}</td>
        <td>${esc(type)}${muted(`${esc(sourceLabel[type] || '')} ${esc(src)}`)}</td>
        <td>${esc(q.item || '')}${q.batch_no ? muted('lot ' + esc(q.batch_no)) : ''}${q.location ? muted(esc(q.location)) : ''}</td>
        <td class="num">${q.qty_inspected ? num(q.qty_inspected) : ''}</td><td>${resultBadge(q.result)}</td>
        <td style="font-size: 12px;">${decision(q)}</td><td>${acts.join(' ')}</td></tr>
        <tr class="hidden" id="qi-detail-${esc(q.id)}"><td colspan="7"></td></tr>`;
    }).join('')}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  const active = plans.filter(p => (p.status || 'Active') === 'Active');
  form.innerHTML = `
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-type">Inspecting</label>
        <select id="qi-type" class="form-select"><option value="Incoming">Goods received (GRN)</option><option value="Production">Production output</option><option value="Outgoing">Goods going out (sales order)</option></select></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-source" id="qi-source-label">GRN</label>
        <input type="text" id="qi-source" class="form-input" style="width: 220px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-item">Item</label>
        <input type="text" id="qi-item" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-batch">Batch / lot</label>
        <input type="text" id="qi-batch" class="form-input" style="width: 120px;"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-loc">Location</label>
        <input type="text" id="qi-loc" class="form-input" style="width: 150px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-qty">Quantity inspected</label>
        <input type="number" id="qi-qty" class="form-input" style="width: 110px;" min="0" step="any"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="qi-plan">Inspection plan</label>
        <select id="qi-plan" class="form-select"><option value="">No plan - enter readings</option>${active.map(p => `<option value="${esc(p.id)}">${esc(p.code || p.id)} - ${esc(p.item || '')}</option>`).join('')}</select></div>
    </div>
    <p id="qi-hint" style="color: var(--text-muted); font-size: 12px; margin: 8px 0 0;"></p>
    <div style="margin-top: 16px;">${readingsTable('qi-readings', true)}</div>
    <div class="form-group" style="margin: 16px 0 0;"><label class="form-label" for="qi-remarks">Remarks</label>
      <input type="text" id="qi-remarks" class="form-input"></div>
    <div style="margin-top: 16px;"><button class="btn btn-primary" id="qi-save">Record inspection</button></div>`;

  container.appendChild(txnPagedSection({ id: 'qi-screen', title: 'Quality Inspections', heading: 'h2', list, count: qis.length,
    forms: { new: ['New Inspection', form] } }));
  txnRestorePane('qi-screen');

  const hint = {
    Incoming: 'Name the GRN and an item received on it. A failed inspection holds the inspected quantity at the location until a hold release is approved.',
    Production: 'Name the production order; item and location come from the order. A failed inspection, once approved, blocks completing the order.',
    Outgoing: 'Name the sales order, the item, where it is and how many. A failed inspection holds that quantity so it cannot be sold.'
  };
  const typeSel = form.querySelector('#qi-type');
  const setType = () => {
    const old = form.querySelector('#qi-source');
    const fresh = document.createElement('input');
    fresh.type = 'text'; fresh.id = 'qi-source'; fresh.className = 'form-input'; fresh.style.width = '220px'; fresh.autocomplete = 'off';
    old.replaceWith(fresh);
    attachLinkTypeahead(fresh, sourceDoctype[typeSel.value], { noSetupHint: true });
    form.querySelector('#qi-source-label').textContent = sourceLabel[typeSel.value];
    form.querySelector('#qi-hint').textContent = hint[typeSel.value];
  };
  typeSel.addEventListener('change', setType);
  setType();
  attachLinkTypeahead(form.querySelector('#qi-item'), 'Item');
  attachLinkTypeahead(form.querySelector('#qi-loc'), 'Location');
  wireReadings(form, 'qi-readings', true, form.querySelector('#qi-plan'));
  form.querySelector('#qi-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const body = {
      source_type: typeSel.value, source_id: form.querySelector('#qi-source').value.trim(),
      item: form.querySelector('#qi-item').value.trim(), batch_no: form.querySelector('#qi-batch').value.trim(),
      location: form.querySelector('#qi-loc').value.trim(), qty_inspected: parseFloat(form.querySelector('#qi-qty').value) || 0,
      inspection_plan: form.querySelector('#qi-plan').value, remarks: form.querySelector('#qi-remarks').value.trim(),
      readings: readReadings(form.querySelector('#qi-readings'))
    };
    const out = await post('/api/v1/quality/inspection', body, 'The inspection could not be recorded.', 'Quality Inspection');
    if (!out) return;
    showToast(`Inspection ${out.id} recorded: ${out.result}.${body.source_type === 'Production' ? ' Submit it for approval.' : ' Decide it from the list.'}`, { variant: out.result === 'Pass' ? 'success' : 'info' });
    txnDone('qi-screen');
    renderView('manufacturing');
  }));

  list.querySelectorAll('[data-qi-act]').forEach(btn => btn.addEventListener('click', async () => {
    const id = btn.dataset.id;
    const q = qis.find(x => x.id === id);
    if (btn.dataset.qiAct === 'readings') toggleDetail(`qi-detail-${id}`, readingsDetail(rowsOf(q.readings), true) + (q.remarks ? `<p style="margin: 8px 0 0;">Remarks: ${esc(q.remarks)}</p>` : ''));
    else if (btn.dataset.qiAct === 'submit') {
      if (await post('/api/v1/approval/submit', { doctype: 'QualityInspection', document_id: id }, 'Could not submit the inspection.', 'Quality Inspection')) {
        showToast('Submitted - an approver decides it under Approvals.'); renderView('manufacturing');
      }
    } else if (btn.dataset.qiAct === 'decide') await decideInspection(q);
    else if (btn.dataset.qiAct === 'ncr') window.openManufacturingTab?.('ncr');
  }));
}

async function decideInspection(q) {
  const pass = q.result === 'Pass';
  const codes = pass ? [] : (await listOf('HoldCode')).filter(h => h.category === 'Quality' && (h.status || 'Active') === 'Active');
  const body = pass
    ? `<p style="margin: 0;">${resultBadge('Pass')} ${esc(q.item)}${q.batch_no ? ' lot ' + esc(q.batch_no) : ''}: every reading passed. Accepting changes no stock.</p>
       <input type="hidden" id="qd-disp" value="Accept">`
    : `<p style="margin: 0;">${resultBadge('Fail')} ${esc(q.item)}${q.batch_no ? ' lot ' + esc(q.batch_no) : ''}, ${num(q.qty_inspected)} at ${esc(q.location)}. A failed inspection cannot be accepted, and it raises a non-conformance report.</p>
       <div class="form-group" style="margin: 0;"><label class="form-label" for="qd-disp">Decision</label>
         <select id="qd-disp" class="form-select">
           <option value="Hold stock">Hold stock - stays on hand, cannot be sold or issued until a hold release is approved</option>
           <option value="Rework">Rework - held the same way until it is reworked and released</option>
           <option value="Use as-is (concession)">Use as-is (concession) - no hold; needs a reason and someone other than the inspector</option>
         </select></div>
       <div class="form-group" style="margin: 0;" id="qd-code-wrap"><label class="form-label" for="qd-code">Hold code</label>
         ${codes.length ? `<select id="qd-code" class="form-select">${codes.map(h => `<option value="${esc(h.code || h.id)}">${esc(h.code || h.id)}${h.description ? ' - ' + esc(h.description) : ''}</option>`).join('')}</select>`
           : `<p style="margin: 0;">No active hold codes of category <b>Quality</b> yet. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['HoldCode'])}>Add a hold code</button></p>`}</div>
       <div class="form-group" style="margin: 0;"><label class="form-label" for="qd-note">Note</label>
         <textarea id="qd-note" class="form-input" rows="2" placeholder="Required for a concession: why the stock can be used"></textarea></div>`;
  const dlg = openQualityDialog(`Decide inspection ${q.code || q.id}`, body, pass ? 'Accept' : 'Record decision', async d => {
    const out = await post(`/api/v1/quality/inspection/${encodeURIComponent(q.id)}/dispose`, {
      disposition: d.querySelector('#qd-disp').value, note: d.querySelector('#qd-note')?.value || '', hold_code: d.querySelector('#qd-code')?.value || ''
    }, 'The decision could not be recorded.', 'Quality Inspection');
    if (!out) return false;
    showToast([`${out.disposition} recorded.`, out.hold_id ? `Stock held: ${out.hold_id}.` : '', out.ncr_id ? `Non-conformance ${out.ncr_id} raised.` : ''].filter(Boolean).join(' '), { variant: 'success' });
    renderView('manufacturing');
    return true;
  });
  const disp = dlg.querySelector('select#qd-disp');
  if (disp) disp.addEventListener('change', () => { dlg.querySelector('#qd-code-wrap').style.display = disp.value.startsWith('Use as-is') ? 'none' : ''; });
}

// --------------------------------------------------------------- certificates

async function renderCertificates(container) {
  const [coas, plans] = await Promise.all([listOf('CertificateOfAnalysis'), listOf('InspectionPlan')]);
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = coas.length === 0 ? '<p style="padding: 24px; color: var(--text-muted);">No certificates yet. Use <b>+ New Certificate</b> to record a lot\'s test results.</p>' : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Certificate</th><th>Item</th><th>Lot</th><th>Plan</th><th>Result</th><th>Status</th><th></th></tr></thead>
    <tbody>${coas.map(c => `<tr><td style="font-family: monospace;">${esc(c.code || c.id)}</td><td>${esc(c.item)}</td><td>${esc(c.batch_no)}</td><td>${esc(c.inspection_plan || '')}</td>
      <td>${resultBadge(c.overall_result)}</td><td><span class="badge ${c.status === 'Released' ? 'badge-success' : c.status === 'Rejected' ? 'badge-danger' : 'badge-secondary'}">${esc(c.status)}</span></td>
      <td><button class="action-btn" data-coa-act="results" data-id="${esc(c.id)}">Results</button>
        ${c.status === 'Draft' && c.overall_result === 'Pass' ? `<button class="action-btn" data-coa-act="release" data-id="${esc(c.id)}">Release</button>` : ''}
        ${c.status === 'Draft' ? `<button class="action-btn" data-coa-act="reject" data-id="${esc(c.id)}">Reject</button>` : ''}</td></tr>
      <tr class="hidden" id="coa-detail-${esc(c.id)}"><td colspan="7"></td></tr>`).join('')}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="coa-item">Item</label><input type="text" id="coa-item" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="coa-batch">Batch / lot</label><input type="text" id="coa-batch" class="form-input" style="width: 140px;"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="coa-plan">Inspection plan</label>
        <select id="coa-plan" class="form-select"><option value="">No plan</option>${plans.filter(p => (p.status || 'Active') === 'Active').map(p => `<option value="${esc(p.id)}">${esc(p.code || p.id)} - ${esc(p.item || '')}</option>`).join('')}</select></div>
    </div>
    <div style="margin-top: 16px;">${readingsTable('coa-results', false)}</div>
    <p style="color: var(--text-muted); font-size: 12px;">One failed result fails the certificate. A passed certificate can be released; rejecting one moves every bin holding this lot to QC-Hold. A certificate records your own test results - it is not a regulatory certification.</p>
    <button class="btn btn-primary" id="coa-save">Save certificate</button>`;

  container.appendChild(txnPagedSection({ id: 'coa-screen', title: 'Certificates of Analysis', heading: 'h2', list, count: coas.length,
    forms: { new: ['New Certificate', form] } }));
  txnRestorePane('coa-screen');
  attachLinkTypeahead(form.querySelector('#coa-item'), 'Item');
  wireReadings(form, 'coa-results', false, form.querySelector('#coa-plan'));
  form.querySelector('#coa-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const results = readReadings(form.querySelector('#coa-results')).map(r => ({ parameter_name: r.parameter_name, actual_value: r.reading, pass_fail: r.pass_fail }));
    if (results.some(r => !r.parameter_name || !r.actual_value || !r.pass_fail)) { await showCustomAlert('Give every result a parameter, the actual value and Pass or Fail.', 'Certificate'); return; }
    const out = await post('/api/v1/quality/coa', { item: form.querySelector('#coa-item').value.trim(), batch_no: form.querySelector('#coa-batch').value.trim(),
      inspection_plan: form.querySelector('#coa-plan').value, test_results: results }, 'The certificate could not be saved.', 'Certificate');
    if (!out) return;
    showToast(`Certificate ${out.id} saved as Draft.`, { variant: 'success' });
    txnDone('coa-screen');
    renderView('manufacturing');
  }));
  list.querySelectorAll('[data-coa-act]').forEach(btn => btn.addEventListener('click', async () => {
    const c = coas.find(x => x.id === btn.dataset.id);
    if (btn.dataset.coaAct === 'results') { toggleDetail(`coa-detail-${c.id}`, readingsDetail(rowsOf(c.test_results), false)); return; }
    if (btn.dataset.coaAct === 'release') {
      if (await post(`/api/v1/quality/coa/${encodeURIComponent(c.id)}/release`, {}, 'The certificate could not be released.', 'Certificate')) { showToast('Certificate released.'); renderView('manufacturing'); }
      return;
    }
    if (!await showCustomConfirm(`Reject certificate ${c.code || c.id}? Every bin holding lot ${c.batch_no} of ${c.item} moves to QC-Hold.`, 'Reject certificate')) return;
    const out = await post(`/api/v1/quality/coa/${encodeURIComponent(c.id)}/reject`, {}, 'The certificate could not be rejected.', 'Certificate');
    if (out) { showToast(`Rejected - ${out.bins_quarantined || 0} bin(s) moved to QC-Hold.`); renderView('manufacturing'); }
  }));
}

// ------------------------------------------------------------ non-conformance

const ncrStatusLabel = { Draft: 'Raised', Investigating: 'Investigating', CorrectiveActionPlanned: 'Action planned', Closed: 'Closed' };

async function renderNonConformance(container) {
  const ncrs = await listOf('NonConformanceReport');
  const t = today();
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = ncrs.length === 0 ? '<p style="padding: 24px; color: var(--text-muted);">No non-conformance reports. Failed inspections raise one automatically; use <b>+ New Report</b> for anything else.</p>' : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Report</th><th>What went wrong</th><th>Source</th><th>Item / lot</th><th>Root cause</th><th>Corrective action</th><th>Status</th><th></th></tr></thead>
    <tbody>${ncrs.map(n => {
      const overdue = n.due_date && n.due_date < t && n.status !== 'Closed';
      const act = n.status === 'Draft' ? 'investigate' : n.status === 'Investigating' ? 'plan' : n.status === 'CorrectiveActionPlanned' ? 'close' : '';
      const label = { investigate: 'Investigate', plan: 'Plan action', close: 'Close' }[act];
      return `<tr><td style="font-family: monospace;">${esc(n.code || n.id)}</td><td>${esc(n.description)}</td>
        <td>${n.source_id ? `${esc(n.source_doctype === 'QualityInspection' ? 'Inspection' : n.source_doctype || '')} ${esc(n.source_id)}` : ''}</td>
        <td>${esc(n.item || '')}${n.batch_no ? muted('lot ' + esc(n.batch_no)) : ''}</td><td>${esc(n.root_cause_reason_code || '')}</td>
        <td>${esc(n.corrective_action || '')}${n.owner ? muted('owner ' + esc(n.owner) + (n.due_date ? ' · due ' + esc(n.due_date) : '')) : ''}${overdue ? '<span class="badge badge-danger">overdue</span>' : ''}
          ${n.closure_evidence ? muted('evidence: ' + esc(n.closure_evidence) + (n.closed_by ? ' · closed by ' + esc(n.closed_by) : '')) : ''}</td>
        <td><span class="badge ${n.status === 'Closed' ? 'badge-success' : 'badge-warning'}">${esc(ncrStatusLabel[n.status] || n.status)}</span></td>
        <td>${act ? `<button class="action-btn" data-ncr-act="${act}" data-id="${esc(n.id)}">${label}</button>` : ''}</td></tr>`;
    }).join('')}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div class="form-group"><label class="form-label" for="ncr-desc">What went wrong</label><input type="text" id="ncr-desc" class="form-input"></div>
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ncr-src-type">Found on</label>
        <select id="ncr-src-type" class="form-select"><option value="">Nothing specific</option><option value="GRN">GRN</option><option value="ProductionOrder">Production order</option><option value="SalesOrder">Sales order</option><option value="QualityInspection">Inspection</option></select></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ncr-src">Document</label><input type="text" id="ncr-src" class="form-input" style="width: 200px;"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ncr-item">Item</label><input type="text" id="ncr-item" class="form-input" style="width: 200px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ncr-batch">Batch / lot</label><input type="text" id="ncr-batch" class="form-input" style="width: 140px;"></div>
    </div>
    <div style="margin-top: 16px;"><button class="btn btn-primary" id="ncr-save">Raise report</button></div>`;

  container.appendChild(txnPagedSection({ id: 'ncr-screen', title: 'Non-conformance', heading: 'h2', list, count: ncrs.length,
    forms: { new: ['New Report', form] } }));
  txnRestorePane('ncr-screen');
  attachLinkTypeahead(form.querySelector('#ncr-item'), 'Item');
  form.querySelector('#ncr-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const out = await post('/api/v1/quality/ncr', { description: form.querySelector('#ncr-desc').value.trim(), source_doctype: form.querySelector('#ncr-src-type').value,
      source_id: form.querySelector('#ncr-src').value.trim(), item: form.querySelector('#ncr-item').value.trim(), batch_no: form.querySelector('#ncr-batch').value.trim() },
      'The report could not be raised.', 'Non-conformance');
    if (!out) return;
    showToast(`Report ${out.id} raised.`, { variant: 'success' });
    txnDone('ncr-screen');
    renderView('manufacturing');
  }));
  list.querySelectorAll('[data-ncr-act]').forEach(btn => btn.addEventListener('click', () => ncrStep(ncrs.find(x => x.id === btn.dataset.id), btn.dataset.ncrAct)));
}

async function ncrStep(n, act) {
  const path = `/api/v1/quality/ncr/${encodeURIComponent(n.id)}`;
  const done = msg => { showToast(msg, { variant: 'success' }); renderView('manufacturing'); return true; };
  if (act === 'investigate') {
    const reasons = (await listOf('ReasonCode')).filter(r => r.category === 'Quality' && (r.status || 'Active') === 'Active');
    openQualityDialog(`Investigate ${n.code || n.id}`, reasons.length
      ? `<div class="form-group" style="margin: 0;"><label class="form-label" for="ncr-rc">Root cause</label><select id="ncr-rc" class="form-select">${reasons.map(r => `<option value="${esc(r.id)}">${esc(r.code || r.id)}${r.description ? ' - ' + esc(r.description) : ''}</option>`).join('')}</select></div>`
      : `<p style="margin: 0;">No active reason codes of category <b>Quality</b> yet. <button type="button" class="action-btn" ${actionAttrs('openSetupDoctype', ['ReasonCode'])}>Add a reason code</button></p>`,
      reasons.length ? 'Start investigation' : '', async d => (await post(`${path}/investigate`, { root_cause_reason_code: d.querySelector('#ncr-rc').value }, 'Could not start the investigation.', 'Non-conformance')) && done('Investigation started.'));
  } else if (act === 'plan') {
    openQualityDialog(`Corrective action for ${n.code || n.id}`, `
      <div class="form-group" style="margin: 0;"><label class="form-label" for="ncr-action">Corrective action</label><textarea id="ncr-action" class="form-input" rows="2"></textarea></div>
      <div style="display: flex; gap: 12px; flex-wrap: wrap;">
        <div class="form-group" style="margin: 0;"><label class="form-label" for="ncr-owner">Owner (who does it)</label><input type="text" id="ncr-owner" class="form-input" style="width: 220px;"></div>
        <div class="form-group" style="margin: 0;"><label class="form-label" for="ncr-due">Due</label><input type="date" id="ncr-due" class="form-input"></div></div>`,
      'Plan action', async d => (await post(`${path}/plan-corrective-action`, { corrective_action: d.querySelector('#ncr-action').value, owner: d.querySelector('#ncr-owner').value, due_date: d.querySelector('#ncr-due').value },
        'Could not record the corrective action.', 'Non-conformance')) && done('Corrective action planned.'));
  } else if (act === 'close') {
    openQualityDialog(`Close ${n.code || n.id}`, `<p style="margin: 0;">Action: ${esc(n.corrective_action)}${n.owner ? ` (owner ${esc(n.owner)})` : ''}</p>
      <div class="form-group" style="margin: 0;"><label class="form-label" for="ncr-evidence">Evidence the action was done</label><textarea id="ncr-evidence" class="form-input" rows="2" placeholder="e.g. vendor letter received, next three lots passed inspection"></textarea></div>`,
      'Close report', async d => (await post(`${path}/close`, { closure_evidence: d.querySelector('#ncr-evidence').value }, 'Could not close the report.', 'Non-conformance')) && done('Report closed.'));
  }
}

// ---------------------------------------------------------------- maintenance

async function renderMaintenance(container) {
  const [orders, schedules] = await Promise.all([listOf('MaintenanceOrder'), listOf('MaintenanceSchedule')]);
  const t = today();
  const open = s => s === 'Draft' || s === 'Scheduled' || s === 'InProgress';
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = orders.length === 0 ? '<p style="padding: 24px; color: var(--text-muted);">No maintenance orders yet. Use <b>+ New Order</b>, or <b>+ New Schedule</b> for recurring work - the scheduler raises its orders on the due date.</p>' : `<div style="overflow-x: auto;"><table>
    <thead><tr><th>Order</th><th>Asset</th><th>Work</th><th>Scheduled</th><th>Status</th><th>Checklist</th><th>Done</th><th></th></tr></thead>
    <tbody>${orders.map(o => {
      const tasks = rowsOf(o.checklist), parts = rowsOf(o.parts_used);
      const overdue = open(o.status) && o.scheduled_date && o.scheduled_date < t;
      const acts = [];
      if (o.status === 'Draft') acts.push(['schedule', 'Schedule']);
      if (o.status === 'Scheduled') acts.push(['start', 'Start']);
      if (o.status === 'InProgress') acts.push(['complete', 'Complete']);
      if (open(o.status)) acts.push(['cancel', 'Cancel']);
      return `<tr><td style="font-family: monospace;">${esc(o.code || o.id)}${o.maintenance_schedule_id ? muted('from ' + esc(o.maintenance_schedule_id)) : ''}</td>
        <td>${esc(o.asset)}</td><td>${esc(o.description)}</td>
        <td>${esc(o.scheduled_date || '')}${overdue ? ' <span class="badge badge-danger">overdue</span>' : ''}</td>
        <td><span class="badge ${o.status === 'Completed' ? 'badge-success' : o.status === 'Cancelled' ? 'badge-secondary' : 'badge-warning'}">${esc(o.status === 'InProgress' ? 'In progress' : o.status)}</span>${o.started_by ? muted('started by ' + esc(o.started_by)) : ''}</td>
        <td>${tasks.length ? `${tasks.filter(x => x.done).length} / ${tasks.length}` : ''}</td>
        <td style="font-size: 12px;">${o.status === 'Completed' ? `${esc(o.completed_by || '')}${o.labour_hours ? ` · ${num(o.labour_hours)} h` : ''}${parts.length ? ` · parts: ${parts.map(p => `${esc(p.sku)} × ${num(p.qty)}`).join(', ')}` : ''}${muted(esc(o.completion_notes || ''))}` : ''}</td>
        <td>${acts.map(([a, l]) => `<button class="action-btn" data-mo-act="${a}" data-id="${esc(o.id)}">${l}</button>`).join(' ')}</td></tr>`;
    }).join('')}</tbody></table></div>
    ${schedules.length ? `<h3 style="font-size: 14px; margin: 20px 16px 8px;">Recurring schedules</h3><div style="overflow-x: auto;"><table>
      <thead><tr><th>Schedule</th><th>Asset</th><th>Work</th><th class="num">Every (days)</th><th>Next due</th><th class="num">Tasks</th><th>Status</th></tr></thead>
      <tbody>${schedules.map(s => `<tr><td style="font-family: monospace;">${esc(s.code || s.id)}</td><td>${esc(s.asset)}</td><td>${esc(s.description)}</td><td class="num">${num(s.interval_days)}</td>
        <td>${esc(s.next_due_date || '')}</td><td class="num">${String(s.checklist || '').split('\n').filter(x => x.trim()).length || ''}</td><td>${esc(s.status)}</td></tr>`).join('')}</tbody></table></div>` : ''}`;

  const orderForm = document.createElement('div');
  orderForm.className = 'table-panel';
  orderForm.style.padding = '24px';
  orderForm.innerHTML = `
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="mo-asset">Asset</label><input type="text" id="mo-asset" class="form-input" style="width: 220px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 220px;"><label class="form-label" for="mo-desc">Work to be done</label><input type="text" id="mo-desc" class="form-input"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="mo-date">Scheduled for (optional)</label><input type="date" id="mo-date" class="form-input"></div>
    </div>
    <div class="form-group" style="margin: 16px 0 0;"><label class="form-label" for="mo-checklist">Task checklist - one task per line (optional)</label><textarea id="mo-checklist" class="form-input" rows="3"></textarea></div>
    <div style="margin-top: 16px;"><button class="btn btn-primary" id="mo-save">Create order</button></div>`;

  const schedForm = document.createElement('div');
  schedForm.className = 'table-panel';
  schedForm.style.padding = '24px';
  schedForm.innerHTML = `
    <div style="display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ms-code">Schedule code</label><input type="text" id="ms-code" class="form-input" style="width: 140px;"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ms-asset">Asset</label><input type="text" id="ms-asset" class="form-input" style="width: 220px;" autocomplete="off"></div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 200px;"><label class="form-label" for="ms-desc">Work</label><input type="text" id="ms-desc" class="form-input"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ms-every">Every (days)</label><input type="number" id="ms-every" class="form-input" style="width: 90px;" min="1"></div>
      <div class="form-group" style="margin-bottom: 0;"><label class="form-label" for="ms-next">First due</label><input type="date" id="ms-next" class="form-input"></div>
    </div>
    <div class="form-group" style="margin: 16px 0 0;"><label class="form-label" for="ms-checklist">Task checklist - one task per line; each order raised gets it</label><textarea id="ms-checklist" class="form-input" rows="3"></textarea></div>
    <div style="margin-top: 16px;"><button class="btn btn-primary" id="ms-save">Save schedule</button></div>`;

  container.appendChild(txnPagedSection({ id: 'mo-screen', title: 'Maintenance', heading: 'h2', list, count: orders.length,
    forms: { new: ['New Order', orderForm], schedule: ['New Schedule', schedForm] } }));
  txnRestorePane('mo-screen');
  attachLinkTypeahead(orderForm.querySelector('#mo-asset'), 'Asset');
  attachLinkTypeahead(schedForm.querySelector('#ms-asset'), 'Asset');
  orderForm.querySelector('#mo-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const out = await post('/api/v1/quality/maintenance-order', { asset: orderForm.querySelector('#mo-asset').value.trim(), description: orderForm.querySelector('#mo-desc').value.trim(),
      scheduled_date: orderForm.querySelector('#mo-date').value, checklist: orderForm.querySelector('#mo-checklist').value }, 'The order could not be created.', 'Maintenance');
    if (!out) return;
    showToast(`Maintenance order ${out.id} created.`, { variant: 'success' });
    txnDone('mo-screen');
    renderView('manufacturing');
  }));
  schedForm.querySelector('#ms-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const code = schedForm.querySelector('#ms-code').value.trim();
    const out = await post('/api/v1/doc/MaintenanceSchedule', { id: code, code, asset: schedForm.querySelector('#ms-asset').value.trim(), description: schedForm.querySelector('#ms-desc').value.trim(),
      interval_days: parseInt(schedForm.querySelector('#ms-every').value, 10) || 0, next_due_date: schedForm.querySelector('#ms-next').value,
      checklist: schedForm.querySelector('#ms-checklist').value, status: 'Active' }, 'The schedule could not be saved.', 'Maintenance');
    if (!out) return;
    showToast(`Schedule ${code} saved - its orders are raised on each due date.`, { variant: 'success' });
    txnDone('mo-screen');
    renderView('manufacturing');
  }));
  list.querySelectorAll('[data-mo-act]').forEach(btn => btn.addEventListener('click', () => maintenanceStep(orders.find(x => x.id === btn.dataset.id), btn.dataset.moAct)));
}

async function maintenanceStep(o, act) {
  const path = `/api/v1/quality/maintenance-order/${encodeURIComponent(o.id)}`;
  const done = msg => { showToast(msg, { variant: 'success' }); renderView('manufacturing'); return true; };
  if (act === 'schedule') {
    openQualityDialog(`Schedule ${o.code || o.id}`, `<div class="form-group" style="margin: 0;"><label class="form-label" for="mo-sched-date">Scheduled for</label><input type="date" id="mo-sched-date" class="form-input" value="${today()}"></div>`,
      'Schedule', async d => (await post(`${path}/schedule`, { scheduled_date: d.querySelector('#mo-sched-date').value }, 'Could not schedule the order.', 'Maintenance')) && done('Scheduled.'));
  } else if (act === 'start') {
    if (await post(`${path}/start`, {}, 'Could not start the order.', 'Maintenance')) done('Started.');
  } else if (act === 'cancel') {
    if (await showCustomConfirm(`Cancel maintenance order ${o.code || o.id}?`, 'Cancel order') && await post(`${path}/cancel`, {}, 'Could not cancel the order.', 'Maintenance')) done('Cancelled.');
  } else if (act === 'complete') {
    const tasks = rowsOf(o.checklist);
    const dlg = openQualityDialog(`Complete ${o.code || o.id}`, `
      ${tasks.length ? `<fieldset style="border: 0; padding: 0; margin: 0;"><legend class="form-label">Checklist - every task must be ticked</legend>
        ${tasks.map((x, i) => `<label style="display: block; margin: 4px 0;"><input type="checkbox" class="mo-task" data-i="${i}"> ${esc(x.task)}</label>`).join('')}</fieldset>` : ''}
      <div><label class="form-label">Parts used (recorded on the order - not taken from stock)</label>
        <table><thead><tr><th>Item</th><th class="num">Qty</th><th></th></tr></thead><tbody id="mo-parts"></tbody></table>
        <button type="button" class="btn btn-outline btn-sm" id="mo-add-part" style="margin-top: 6px;">+ Add part</button></div>
      <div class="form-group" style="margin: 0;"><label class="form-label" for="mo-hours">Hours worked</label><input type="number" id="mo-hours" class="form-input" style="width: 100px;" min="0" step="any"></div>
      <div class="form-group" style="margin: 0;"><label class="form-label" for="mo-notes">What was done</label><textarea id="mo-notes" class="form-input" rows="2"></textarea></div>`,
      'Complete', async d => {
        const parts = [...d.querySelectorAll('#mo-parts tr')].map(tr => ({ sku: tr.querySelector('.mo-part-sku').value.trim(), qty: parseFloat(tr.querySelector('.mo-part-qty').value) || 0 })).filter(p => p.sku || p.qty);
        const checklist = tasks.map((x, i) => ({ task: x.task, done: d.querySelector(`.mo-task[data-i="${i}"]`).checked }));
        return (await post(`${path}/complete`, { completion_notes: d.querySelector('#mo-notes').value, labour_hours: parseFloat(d.querySelector('#mo-hours').value) || 0, checklist, parts_used: parts },
          'Could not complete the order.', 'Maintenance')) && done('Completed.');
      });
    const addPart = () => {
      const tr = document.createElement('tr');
      tr.innerHTML = `<td><input type="text" class="form-input mo-part-sku" aria-label="Part" autocomplete="off"></td><td class="num"><input type="number" class="form-input mo-part-qty" style="width: 80px;" min="0" step="any" aria-label="Quantity"></td>
        <td><button type="button" class="action-btn">Remove</button></td>`;
      tr.querySelector('button').addEventListener('click', () => tr.remove());
      dlg.querySelector('#mo-parts').appendChild(tr);
      attachLinkTypeahead(tr.querySelector('.mo-part-sku'), 'Item');
    };
    dlg.querySelector('#mo-add-part').addEventListener('click', addPart);
  }
}

async function renderQualityTab(container, tab) {
  if (tab === 'certificates') return renderCertificates(container);
  if (tab === 'ncr') return renderNonConformance(container);
  if (tab === 'maintenance') return renderMaintenance(container);
  return renderInspections(container);
}

export { renderQualityTab, decideInspection, ncrStep, maintenanceStep };
