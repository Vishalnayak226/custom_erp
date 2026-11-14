// Supplier corrective actions (SCAR) and the AQL sample hint on the inspection
// form (170.15, 170.16). Loaded by view-quality.js, which passes its helpers.

const STEP_FOR = { Open: ['root_cause', 'Root cause'], 'Root cause found': ['action', 'Action'], 'Action taken': ['verification', 'Verify and close'] };

async function renderScarTab(container, h) {
  const { listOf, post, esc, muted, today, txnPagedSection, txnRestorePane, txnDone, openQualityDialog } = h;
  const scars = await listOf('SupplierCAR');
  const t = today();
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = scars.length === 0 ? '<p class="s-a63e81">No supplier actions.</p>' : `<div class="s-987eb2"><table>
    <thead><tr><th>SCAR</th><th>Vendor</th><th>Issue</th><th>Item / lot</th><th>Reply by</th><th>Progress</th><th>Status</th><th></th></tr></thead>
    <tbody>${scars.map(s => {
      const late = s.due_date && s.due_date < t && s.status !== 'Closed';
      const step = STEP_FOR[s.status];
      return `<tr><td>${esc(s.code || s.id)}</td><td>${esc(s.vendor)}</td><td>${esc(s.issue)}</td>
        <td>${esc(s.item || '')}${s.batch_no ? muted('lot ' + esc(s.batch_no)) : ''}</td>
        <td>${esc(s.due_date || '')}${late ? ' <span class="badge badge-danger">late</span>' : ''}</td>
        <td>${esc(s.root_cause || '')}${s.action ? muted('action: ' + esc(s.action)) : ''}${s.verification ? muted('verified: ' + esc(s.verification)) : ''}</td>
        <td><span class="badge ${s.status === 'Closed' ? 'badge-success' : 'badge-warning'}">${esc(s.status)}</span></td>
        <td>${step ? `<button class="action-btn" data-scar-step="${step[0]}" data-id="${esc(s.id)}">${step[1]}</button>` : ''}</td></tr>`;
    }).join('')}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div class="s-d3ed5a">
      <div class="form-group s-50435f"><label class="form-label" for="scar-vendor">Vendor</label><input type="text" id="scar-vendor" class="form-input" autocomplete="off"></div>
      <div class="form-group s-50435f"><label class="form-label" for="scar-ncr">Non-conformance</label><input type="text" id="scar-ncr" class="form-input"></div>
      <div class="form-group s-50435f"><label class="form-label" for="scar-due">Reply by</label><input type="date" id="scar-due" class="form-input"></div>
    </div>
    <div class="form-group"><label class="form-label" for="scar-issue">Issue</label><input type="text" id="scar-issue" class="form-input"></div>
    <div class="s-4c0544"><button class="btn btn-primary" id="scar-save">Open SCAR</button></div>`;

  container.appendChild(txnPagedSection({ id: 'scar-screen', title: 'Supplier Actions', heading: 'h2', list, count: scars.length,
    forms: { new: ['New SCAR', form] } }));
  txnRestorePane('scar-screen');
  attachLinkTypeahead(form.querySelector('#scar-vendor'), 'Vendor');
  form.querySelector('#scar-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const out = await post('/api/v1/quality/scar', {
      vendor: form.querySelector('#scar-vendor').value.trim(), ncr_id: form.querySelector('#scar-ncr').value.trim(),
      due_date: form.querySelector('#scar-due').value, issue: form.querySelector('#scar-issue').value.trim()
    }, 'The SCAR could not be opened.', 'Supplier action');
    if (!out) return;
    showToast(`${out.id} opened.`, { variant: 'success' });
    txnDone('scar-screen');
    renderView('manufacturing');
  }));
  list.querySelectorAll('[data-scar-step]').forEach(btn => btn.addEventListener('click', () => {
    const step = btn.dataset.scarStep;
    const label = { root_cause: 'Root cause', action: 'Action taken', verification: 'Verification' }[step];
    openQualityDialog(label, `<div class="form-group s-2a0ca8"><label class="form-label" for="scar-text">${esc(label)}</label><textarea id="scar-text" class="form-input" rows="2"></textarea></div>`, 'Save',
      async d => {
        const out = await post(`/api/v1/quality/scar/${encodeURIComponent(btn.dataset.id)}/step`, { step, text: d.querySelector('#scar-text').value.trim() }, 'The step could not be saved.', 'Supplier action');
        if (!out) return false;
        showToast('Saved.', { variant: 'success' });
        renderView('manufacturing');
        return true;
      });
  }));
}

// wireAqlHint shows the sample size and accept / reject numbers when the chosen
// plan names an AQL, and keeps them current as the lot quantity changes.
function wireAqlHint(form, plans) {
  const hint = form.querySelector('#qi-aql');
  const planSel = form.querySelector('#qi-plan');
  const qty = form.querySelector('#qi-qty');
  if (!hint || !planSel || !qty) return;
  const refresh = async () => {
    const plan = plans.find(p => p.id === planSel.value);
    const lot = parseInt(qty.value, 10);
    hint.textContent = '';
    if (!plan || !plan.aql || !(lot > 0)) return;
    const r = await apiFetch(`/api/v1/quality/aql-plan?lot_size=${lot}&aql=${encodeURIComponent(plan.aql)}`);
    if (!r || !r.ok) return;
    const p = await r.json();
    hint.textContent = `AQL ${p.aql}: sample ${p.sample_size}, accept at ${p.accept_no}, reject at ${p.reject_no}`;
  };
  planSel.addEventListener('change', refresh);
  qty.addEventListener('input', refresh);
}

export { renderScarTab, wireAqlHint };
