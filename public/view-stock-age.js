// Stage 146.3.11: stock age inspections (the fading-polish check), shown under
// Quality Inspections when Settings, Quality, Re-inspect stock by age is on.
// Off, the API refuses and nothing is drawn. A row is decided here: Pass,
// Re-polish (a job-work order picked from the list), Markdown (a flag) or
// Scrap (items, quantities and a reason code make a stock adjustment).
//
// Native ES module, loaded with import() by view-quality.js.
const esc = v => escapeHTMLText(v == null ? '' : String(v));

async function jsonOf(path) {
  const r = await apiFetch(path);
  return r && r.ok ? r.json().catch(() => null) : null;
}

async function listOf(doctype) {
  const d = await jsonOf(`/api/v1/doc/${doctype}?limit=500`);
  return Array.isArray(d) ? d : ((d && d.data) || []);
}

async function decide(helpers, row, reload) {
  const { openQualityDialog } = helpers;
  const [orders, reasons] = await Promise.all([listOf('SubcontractOrder'), listOf('ReasonCode')]);
  const codes = reasons.filter(x => (x.status || 'Active') === 'Active' && x.category === 'Inventory Adjustment');
  const body = `
    <div class="form-group"><label class="form-label" for="sai-outcome">Outcome</label>
      <select id="sai-outcome" class="form-select"><option>Pass</option><option>Re-polish</option><option>Markdown</option><option>Scrap</option></select></div>
    <div class="form-group" id="sai-job" hidden><label class="form-label" for="sai-order">Job-work order</label>
      <select id="sai-order" class="form-select"><option value="">Choose an order</option>${orders.map(o => `<option value="${esc(o.id)}">${esc(o.code || o.id)}${o.status ? ' · ' + esc(o.status) : ''}</option>`).join('')}</select></div>
    <div id="sai-scrap" hidden>
      <div class="form-group"><label class="form-label" for="sai-reason">Reason code</label>
        <select id="sai-reason" class="form-select"><option value="">Choose a reason</option>${codes.map(c => `<option value="${esc(c.code || c.id)}">${esc(c.label || c.name || c.code || c.id)}</option>`).join('')}</select></div>
      <div class="table-panel"><table><thead><tr><th>Item</th><th class="num">Qty</th><th></th></tr></thead><tbody id="sai-lines"></tbody></table></div>
      <button type="button" class="btn btn-outline" id="sai-add">Add item</button></div>
    <div class="form-group"><label class="form-label" for="sai-note">Note</label><input type="text" id="sai-note" class="form-input" maxlength="300"></div>`;
  openQualityDialog(`${row.id} · ${row.location}`, body, 'Save', async (dlg) => {
    const outcome = dlg.querySelector('#sai-outcome').value;
    const req = { id: row.id, outcome, note: dlg.querySelector('#sai-note').value.trim() };
    if (outcome === 'Re-polish') req.job_work_ref = dlg.querySelector('#sai-order').value;
    if (outcome === 'Scrap') {
      req.reason_code = dlg.querySelector('#sai-reason').value;
      req.lines = [...dlg.querySelectorAll('#sai-lines tr')].map(tr => ({ sku: tr.querySelector('.sai-sku').value.trim(), qty: parseInt(tr.querySelector('.sai-qty').value, 10) || 0 })).filter(l => l.sku);
    }
    const r = await apiFetch('/api/v1/stock-age-inspections', { method: 'POST', body: JSON.stringify(req) });
    if (!r) return false;
    if (!r.ok) { await showApiError(r, 'The decision could not be saved.', 'Stock age inspection'); return false; }
    showToast(`${row.id}: ${outcome}.`);
    reload();
    return true;
  });
  const dlg = document.getElementById('quality-dialog');
  const pick = () => {
    const o = dlg.querySelector('#sai-outcome').value;
    dlg.querySelector('#sai-job').hidden = o !== 'Re-polish';
    dlg.querySelector('#sai-scrap').hidden = o !== 'Scrap';
  };
  dlg.querySelector('#sai-outcome').addEventListener('change', pick);
  const addLine = () => {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td><input type="text" class="form-input sai-sku" aria-label="Item" autocomplete="off"></td><td class="num"><input type="number" class="form-input sai-qty s-dabaad" min="1" step="1" aria-label="Quantity"></td><td><button type="button" class="action-btn">Remove</button></td>';
    tr.querySelector('button').addEventListener('click', () => tr.remove());
    dlg.querySelector('#sai-lines').appendChild(tr);
    attachLinkTypeahead(tr.querySelector('.sai-sku'), 'Item');
  };
  dlg.querySelector('#sai-add').addEventListener('click', addLine);
  addLine();
}

// renderStockAgeSection draws the list under the inspections, or nothing.
async function renderStockAgeSection(container, helpers) {
  const data = await jsonOf('/api/v1/stock-age-inspections');
  if (!data || data.off) return; // the check is off, or not allowed
  const sec = document.createElement('div');
  sec.className = 'table-panel';
  sec.style.marginTop = '24px';
  container.appendChild(sec);
  const draw = async () => {
    const d = await jsonOf('/api/v1/stock-age-inspections');
    const rows = (d && d.inspections) || [];
    sec.innerHTML = `<h2 class="s-aaf8f1">Stock age inspections</h2>` + (rows.length === 0
      ? '<p class="s-d8b3a4">Nothing due.</p>'
      : `<div class="s-987eb2"><table><thead><tr><th>Inspection</th><th>Store</th><th>Rule</th><th>Age</th><th>Due</th><th>Days left</th><th></th></tr></thead><tbody>${rows.map(r =>
        `<tr><td class="s-355794">${esc(r.id)}</td><td>${esc(r.location)}</td><td>${esc(r.rule_ref)}</td>
          <td>${esc(r.age_band)}${r.age_source ? `<div class="s-428d6b">${esc(r.age_source)}</div>` : ''}</td>
          <td>${esc(r.due_date)}</td><td class="num">${esc(r.days_left)}</td>
          <td><button class="action-btn" data-sai="${esc(r.id)}">Decide</button></td></tr>`).join('')}</tbody></table></div>`);
    sec.querySelectorAll('[data-sai]').forEach(b => b.addEventListener('click', () => decide(helpers, rows.find(x => x.id === b.dataset.sai), draw)));
  };
  await draw();
}

export { renderStockAgeSection };
