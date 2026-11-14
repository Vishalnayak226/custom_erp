// Ask 170 group F (170.22-170.25): the Calibration and Tax & Benefits HR tabs.
// A native ES module loaded by view-hr.js on first use; shared app services
// (apiFetch, showApiError, escapeHTMLText, openSetupDoctype) come from the shell.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const getJSON = async (url, fallback) => {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
};
const title = (container, text) => {
  const h = document.createElement('div');
  h.className = 'page-header';
  h.innerHTML = `<div class="page-title-section"><h1 class="page-title">${esc(text)}</h1></div>`;
  container.appendChild(h);
};

// ---- Calibration -------------------------------------------------------------

let calCycleId = '';

export async function renderCalibrationTab(container) {
  title(container, 'Calibration');
  const cycles = await getJSON('/api/v1/doc/AppraisalCycle?limit=1000', []);
  if (!cycles.some(c => c.id === calCycleId)) calCycleId = (cycles.find(c => c.status === 'Active') || cycles[0] || {}).id || '';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `<div class="toolbar"><select id="cal-cycle">${cycles.map(c => `<option value="${esc(c.id)}"${c.id === calCycleId ? ' selected' : ''}>${esc(c.code || c.id)}${c.status ? ' · ' + esc(c.status) : ''}</option>`).join('')}</select></div><div id="cal-body"></div>`;
  container.appendChild(panel);
  panel.querySelector('#cal-cycle').addEventListener('change', e => { calCycleId = e.target.value; load(); });
  const body = panel.querySelector('#cal-body');

  async function load() {
    if (!calCycleId) { body.innerHTML = '<p>No appraisal cycle.</p>'; return; }
    const [spread, appraisals] = await Promise.all([
      getJSON(`/api/v1/hr/appraisal-cycles/${encodeURIComponent(calCycleId)}/rating-spread`, { spread: [] }),
      getJSON(`/api/v1/doc/Appraisal?cycle_id=${encodeURIComponent(calCycleId)}&limit=1000`, [])]);
    const band = (arr) => arr.map(n => `<td>${n}</td>`).join('');
    const spreadRows = (spread.spread || []).map(r => `<tr><td>${esc(r.department)}</td>${band(r.original)}${band(r.calibrated)}<td>${r.unrated}</td></tr>`).join('');
    const apRows = appraisals.filter(a => a.status === 'Approved').map(a => `<tr data-id="${esc(a.id)}">
      <td>${esc(a.employee_id)}</td><td>${esc(a.overall_rating ?? '')}</td><td>${esc(a.calibrated_rating ?? '')}</td><td>${esc(a.calibration_reason ?? '')}</td>
      <td><input class="cal-rating hr-num70" type="number" min="1" max="5" step="0.5"></td>
      <td><input class="cal-reason hr-minw200" type="text" placeholder="Reason"></td>
      <td><button class="btn btn-primary cal-save">Calibrate</button></td></tr>`).join('');
    body.innerHTML = `
      <table class="data-table"><thead><tr><th>Department</th><th colspan="5">Original 1-5</th><th colspan="5">Calibrated 1-5</th><th>Unrated</th></tr></thead><tbody>${spreadRows || '<tr><td colspan="12">No ratings yet.</td></tr>'}</tbody></table>
      <table class="data-table"><thead><tr><th>Employee</th><th>Original</th><th>Calibrated</th><th>Reason</th><th>New rating</th><th>Reason</th><th></th></tr></thead><tbody>${apRows || '<tr><td colspan="7">No approved appraisal.</td></tr>'}</tbody></table>`;
    body.querySelectorAll('.cal-save').forEach(btn => btn.addEventListener('click', async () => {
      const tr = btn.closest('tr');
      const res = await apiFetch(`/api/v1/hr/appraisals/${encodeURIComponent(tr.dataset.id)}/calibrate`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rating: parseFloat(tr.querySelector('.cal-rating').value), reason: tr.querySelector('.cal-reason').value })
      });
      if (!res.ok) { await showApiError(res, 'The rating was not calibrated.', 'Not Calibrated'); return; }
      load();
    }));
  }
  await load();
}

// ---- Tax & Benefits ----------------------------------------------------------

async function download(url, name) {
  const res = await apiFetch(url);
  if (!res.ok) { await showApiError(res, 'The file could not be made.', 'Not Downloaded'); return; }
  const a = document.createElement('a');
  a.href = URL.createObjectURL(await res.blob());
  a.download = name;
  document.body.appendChild(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

export async function renderTaxBenefitsTab(container) {
  title(container, 'Tax & Benefits');
  const now = new Date();
  const fyStart = now.getMonth() >= 3 ? now.getFullYear() : now.getFullYear() - 1;
  const fy = `${fyStart}-${String((fyStart + 1) % 100).padStart(2, '0')}`;
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="toolbar">
      ${['TaxSlab', 'TaxRegimeRule', 'LWFRate', 'BenefitPlan', 'BenefitEnrolment', 'SalaryRevision'].map(d => `<button class="btn" data-open="${d}">${esc(d.replace(/([a-z])([A-Z])/g, '$1 $2'))}</button>`).join(' ')}
    </div>
    <div class="toolbar"><strong>PF ECR</strong> <input id="tb-ecr-month" type="month" value="${now.toISOString().slice(0, 7)}"> <button class="btn btn-primary" id="tb-ecr">Download</button></div>
    <div class="toolbar"><strong>Form 24Q</strong>
      <input id="tb-fy" type="text" value="${fy}" size="8">
      <select id="tb-q"><option>Q1</option><option>Q2</option><option>Q3</option><option>Q4</option></select>
      <select id="tb-ann"><option value="I">Annexure I</option><option value="II">Annexure II (Q4)</option></select>
      <button class="btn btn-primary" id="tb-24q">Download</button></div>
    <div class="toolbar"><strong>Arrears</strong> <input id="tb-rev" type="text" placeholder="Revision id"> <button class="btn" id="tb-arrears">List</button></div>
    <div id="tb-out"></div>`;
  container.appendChild(panel);
  panel.querySelectorAll('[data-open]').forEach(b => b.addEventListener('click', () => window.openSetupDoctype(b.dataset.open)));
  panel.querySelector('#tb-ecr').addEventListener('click', () => {
    const m = panel.querySelector('#tb-ecr-month').value;
    download(`/api/v1/hr/pf-ecr?month=${encodeURIComponent(m)}`, `ECR-${m}.txt`);
  });
  panel.querySelector('#tb-24q').addEventListener('click', () => {
    const f = panel.querySelector('#tb-fy').value, q = panel.querySelector('#tb-q').value, a = panel.querySelector('#tb-ann').value;
    download(`/api/v1/hr/form-24q?fin_year=${encodeURIComponent(f)}&quarter=${q}&annexure=${a}`, `Form24Q-${f}-${q}-${a}.csv`);
  });
  panel.querySelector('#tb-arrears').addEventListener('click', async () => {
    const id = panel.querySelector('#tb-rev').value.trim();
    const res = await apiFetch(`/api/v1/hr/salary-revisions/${encodeURIComponent(id)}/arrears`);
    if (!res.ok) { await showApiError(res, 'Arrears could not be listed.', 'Not Listed'); return; }
    const d = await res.json();
    panel.querySelector('#tb-out').innerHTML = `<table class="data-table"><thead><tr><th>Payslip</th><th>Period</th><th>Old gross</th><th>New gross</th><th>Arrears</th></tr></thead><tbody>${(d.rows || []).map(r =>
      `<tr><td>${esc(r.payslip_id)}</td><td>${esc(r.period_from)} to ${esc(r.period_to)}</td><td>${r.old_gross}</td><td>${r.new_gross}</td><td>${r.arrears}</td></tr>`).join('') || '<tr><td colspan="5">None.</td></tr>'}</tbody><tfoot><tr><td colspan="4">Total (listed, not paid)</td><td>${d.total_arrears}</td></tr></tfoot></table>`;
  });
}
