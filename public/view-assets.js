// Fixed Asset Management (Stage 13.13b, MB 16.1) - lifecycle:
// Draft -> Capitalised -> (Transfer any number of times) -> Disposed.
// Stage 55.16 (Batch D): depreciation is posted monthly by a job (Dr 5310 /
// Cr 1410), so the register's accumulated depreciation is what the books
// hold, with any not-yet-posted lag shown beside it; Dispose records the
// proceeds and previews the gain or loss before it posts.
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
//
// Stage 69: the register is the page; + New Asset opens the form as a page of
// its own (txn-screen.js, the standard ERP shape).
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);

async function renderAssetsView(container) {
  const res = await apiFetch('/api/v1/assets/register');
  if (!res) return;

  const assets = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-code">Asset Number</label>
        <input type="text" id="asset-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-category">Category</label>
        <input type="text" id="asset-category" class="form-input" style="width: 130px;" list="asset-category-list" autocomplete="off">
        <datalist id="asset-category-list">${[...new Set(assets.map(a => a.category).filter(Boolean))].sort().map(c => `<option value="${escapeHTMLText(c)}">`).join('')}</datalist>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-cost">Cost</label>
        <input type="number" id="asset-cost" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-useful-life">Useful Life (yrs)</label>
        <input type="number" id="asset-useful-life" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-location">Location</label>
        <input type="text" id="asset-location" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-custodian">Custodian</label>
        <input type="text" id="asset-custodian" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-acquisition-date">Acquisition Date</label>
        <input type="date" id="asset-acquisition-date" class="form-input">
      </div>
      <button class="btn btn-primary" id="asset-create-btn">Create</button>
    </div>
    <div id="asset-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Asset #</th><th>Category</th><th>Location</th><th>Custodian</th>
          <th>Cost</th><th>Accum. Depreciation</th><th>Net Block</th><th>Status</th><th></th>
        </tr>
      </thead>
      <tbody>
  `;
  html += assets.length === 0
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No assets yet. Click <b>+ New Asset</b> to add your first fixed asset, then <b>Capitalise</b> it here.</td></tr>`
    : assets.map(a => `
        <tr>
          <td style="font-family: monospace;">${escapeHTMLText(a.code || a.id)}${a.source_grn ? `<div style="font-family: inherit; font-size: 12px; color: var(--text-muted);">${escapeHTMLText(a.item_code || '')} from GRN ${escapeHTMLText(a.source_grn)}</div>` : ''}</td>
          <td>${escapeHTMLText(a.category || '')}</td>
          <td>${escapeHTMLText(a.location || '')}</td>
          <td>${escapeHTMLText(a.custodian || '')}</td>
          <td>${assetMoney(a.cost)}</td>
          <td>${assetMoney(a.accumulated_depreciation)}${a.status === 'Capitalised' && Number(a.calculated_depreciation) - Number(a.accumulated_depreciation) > 0.004
            ? `<div style="font-size: 12px; color: var(--warning-color, #b45309);">${assetMoney(a.calculated_depreciation - a.accumulated_depreciation)} due, not posted</div>` : ''}</td>
          <td>${assetMoney(a.net_block)}</td>
          <td><span class="badge ${a.status === 'Capitalised' ? 'badge-success' : a.status === 'Disposed' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(a.status)}</span></td>
          <td>${renderAssetActions(a)} <button class="action-btn" data-asset-action="details" data-asset-id="${escapeHTMLText(a.id)}">Details</button></td>
        </tr>
        <tr class="hidden" data-asset-detail="${escapeHTMLText(a.id)}"><td colspan="9"></td></tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  // Stage 55.16 (Batch D): Run depreciation is period-close work - offered to
  // an Accountant or a Super Admin only (the server enforces the same rule).
  const runButton = canRunAssetDepreciation()
    ? '<button class="btn btn-outline" type="button" data-asset-run-depreciation="1">Run depreciation</button>' : '';
  container.appendChild(txnPagedSection({ id: 'asset-screen', title: 'Fixed Assets', page: 'New asset (draft)', count: assets.length,
    newLabel: 'New Asset', form: formPanel, list: listPanel, extra: runButton }));
  txnRestorePane('asset-screen');
  const runBtn = document.querySelector('#asset-screen [data-asset-run-depreciation]');
  if (runBtn) runBtn.addEventListener('click', () => guardAgainstDoubleSubmit(runBtn, 'Working...', runAssetDepreciation));
  listPanel.addEventListener('click', event => {
    const button = event.target.closest('[data-asset-action]');
    if (!button) return;
    const assetID = button.dataset.assetId;
    const asset = assets.find(a => a.id === assetID) || {};
    if (button.dataset.assetAction === 'capitalize') capitalizeAsset(assetID, Number(button.dataset.assetLife) || 0, asset);
    else if (button.dataset.assetAction === 'transfer') promptTransferAsset(assetID);
    else if (button.dataset.assetAction === 'dispose') promptDisposeAsset(assetID, asset);
    else if (button.dataset.assetAction === 'details') toggleAssetLifecycle(assetID);
  });

  document.getElementById('asset-create-btn').addEventListener('click', createAsset);
  attachLinkTypeahead(document.getElementById('asset-location'), 'Location');
  attachLinkTypeahead(document.getElementById('asset-custodian'), 'Employee');
}

function renderAssetActions(asset) {
  if (asset.status === 'Draft') {
    return `<button class="action-btn" data-asset-action="capitalize" data-asset-id="${escapeHTMLText(asset.id)}" data-asset-life="${Number(asset.useful_life_years) || 0}">Capitalise</button>`;
  }
  if (asset.status === 'Capitalised') {
    return `
      <button class="action-btn" data-asset-action="transfer" data-asset-id="${escapeHTMLText(asset.id)}">Transfer</button>
      <button class="action-btn action-btn-danger" data-asset-action="dispose" data-asset-id="${escapeHTMLText(asset.id)}">Dispose</button>
    `;
  }
  return '';
}

async function createAsset() {
  // BLD-036: guard against a double-click creating two Asset records.
  await guardAgainstDoubleSubmit(document.getElementById('asset-create-btn'), 'Creating...', createAssetInner);
}

async function createAssetInner() {
  const errorEl = document.getElementById('asset-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('asset-code').value.trim();
  const category = document.getElementById('asset-category').value.trim();
  const cost = parseFloat(document.getElementById('asset-cost').value);
  const usefulLife = parseFloat(document.getElementById('asset-useful-life').value);
  const location = document.getElementById('asset-location').value.trim();
  const custodian = document.getElementById('asset-custodian').value.trim();
  const acquisitionDate = document.getElementById('asset-acquisition-date').value;

  if (!code || !cost || !usefulLife || !location || !acquisitionDate) {
    errorEl.textContent = 'Asset Number, Cost, Useful Life, Location, and Acquisition Date are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Asset', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, category, cost, useful_life_years: usefulLife,
      location, custodian, acquisition_date: acquisitionDate, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create asset.';
    errorEl.classList.remove('hidden');
    return;
  }
  txnDone('asset-screen');
  renderView('assets');
}

export {
  renderAssetsView,
  renderAssetActions,
  createAsset,
  createAssetInner,
  capitalizeAsset,
  promptTransferAsset,
  promptDisposeAsset
};

async function capitalizeAsset(assetId, usefulLifeYears, asset = {}) {
  // Stage 57.8: an asset raised from a goods receipt has no useful life yet,
  // and depreciation cannot be worked out without one - ask for it here.
  let life = Number(usefulLifeYears) || 0;
  if (life <= 0) {
    const answer = await showCustomPrompt('Useful life in years (needed to work out depreciation):', '5');
    if (answer === null || answer === undefined || answer === '') return;
    life = Number(answer);
    if (!(life > 0)) {
      showToast('Enter the useful life as a number of years, for example 5.', { variant: 'error', title: 'Not capitalised' });
      return;
    }
  }
  // Stage 55.16: what capitalising will post, before it is posted.
  const cost = Number(asset.cost) || 0;
  const fmt = v => Number(v).toLocaleString('en-IN');
  if (cost > 0 && !await showCustomConfirm(
    `Capitalise ${asset.code || assetId}?\n\nPosts: Dr 1400 Fixed Assets ${fmt(cost)} / Cr 2100 GRN Suspense ${fmt(cost)}.\nDepreciation: straight line, ${assetMoney(Math.floor(cost * 100 / Math.round(life * 12)) / 100)} a month for ${Math.round(life * 12)} months from the month of ${asset.capitalisation_date || 'today'} - posted monthly, Dr 5310 Depreciation Expense / Cr 1410 Accumulated Depreciation.`,
    'Capitalise asset')) return;
  const res = await apiFetch('/api/v1/assets/capitalize', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId, useful_life_years: life })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to capitalise asset.', 'Capitalisation Failed');
    return;
  }
  renderView('assets');
}

// 2026-10-09: Transfer and Dispose used plain text prompts - "New location:",
// "New custodian (optional):", "Disposal type (Sale, Scrap, or WriteOff):" -
// so the user had to type a location code, an employee code and an exact
// spelling from memory (Stage 57.1, names not codes). They now open one small
// panel above the register with name pickers and a fixed list; the requests
// they send are unchanged.
function openAssetActionPanel(title, bodyHTML, onConfirm) {
  document.getElementById('asset-action-panel')?.remove();
  const panel = document.createElement('div');
  panel.id = 'asset-action-panel';
  panel.className = 'table-panel';
  panel.style.cssText = 'padding: 20px 24px; margin-bottom: 24px;';
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">${escapeHTMLText(title)}</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${bodyHTML}
      <button class="btn btn-primary" id="asset-action-confirm" type="button">Confirm</button>
      <button class="btn btn-outline" id="asset-action-cancel" type="button">Cancel</button>
    </div>
    <div id="asset-action-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  // Stage 69: on the register page, above the list (the create form is its
  // own page now).
  const anchor = document.querySelector('#asset-screen > [data-pane="list"]');
  if (anchor) anchor.prepend(panel); else document.getElementById('view-root').prepend(panel);
  panel.querySelector('#asset-action-cancel').addEventListener('click', () => panel.remove());
  const confirmBtn = panel.querySelector('#asset-action-confirm');
  confirmBtn.addEventListener('click', () => guardAgainstDoubleSubmit(confirmBtn, 'Working...', onConfirm));
  panel.scrollIntoView({ block: 'nearest' });
  return panel;
}

function assetActionError(message) {
  const el = document.getElementById('asset-action-error');
  if (!el) return;
  el.textContent = message;
  el.classList.remove('hidden');
}

async function promptTransferAsset(assetId) {
  const panel = openAssetActionPanel(`Transfer asset ${assetId}`, `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-transfer-location">New Location<span class="required">*</span></label>
      <input type="text" id="asset-transfer-location" class="form-input" style="width: 220px;" autocomplete="off" placeholder="Search by name">
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-transfer-custodian">New Custodian</label>
      <input type="text" id="asset-transfer-custodian" class="form-input" style="width: 220px;" autocomplete="off" placeholder="Optional - who is responsible">
    </div>
  `, async () => {
    const newLocation = document.getElementById('asset-transfer-location').value.trim();
    const newCustodian = document.getElementById('asset-transfer-custodian').value.trim();
    if (!newLocation) { assetActionError('Choose the location the asset is moving to.'); return; }
    const res = await apiFetch('/api/v1/assets/transfer', {
      method: 'POST',
      body: JSON.stringify({ asset_id: assetId, new_location: newLocation, new_custodian: newCustodian })
    });
    if (!res) return;
    if (!res.ok) { assetActionError(await getErrorMessage(res, 'Failed to transfer asset.')); return; }
    renderView('assets');
  });
  attachLinkTypeahead(panel.querySelector('#asset-transfer-location'), 'Location');
  attachLinkTypeahead(panel.querySelector('#asset-transfer-custodian'), 'Employee', { noSetupHint: true });
  panel.querySelector('#asset-transfer-location').focus();
}

// Stage 55.16 (Batch D): Dispose asks what was received and into which
// cash/bank account, shows the gain or loss as the proceeds are typed (from
// the server's preview - book value after any depreciation still due), and
// confirms the exact entry before posting it.
function assetDisposalPreviewURL(assetId, proceeds, receivedInto) {
  const q = new URLSearchParams({ proceeds: String(proceeds || 0), received_into: receivedInto || '1100' });
  return `/api/v1/assets/${encodeURIComponent(assetId)}/disposal-preview?${q}`;
}

async function promptDisposeAsset(assetId, asset = {}) {
  const panel = openAssetActionPanel(`Dispose of asset ${assetId}`, `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-dispose-type">How is it leaving?<span class="required">*</span></label>
      <select id="asset-dispose-type" class="form-input" style="width: 200px;">
        <option value="Sale">Sold</option>
        <option value="Scrap" selected>Scrapped</option>
        <option value="WriteOff">Written off (lost, stolen)</option>
      </select>
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-dispose-proceeds">Proceeds</label>
      <input type="number" id="asset-dispose-proceeds" class="form-input" style="width: 140px;" min="0" step="0.01" value="0">
    </div>
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-dispose-into">Received into</label>
      <select id="asset-dispose-into" class="form-input" style="width: 220px;"><option value="1100">1100 Cash/Bank Account</option></select>
    </div>
    <div id="asset-dispose-preview" style="flex-basis: 100%; font-size: 13px;">Working out the book value...</div>
  `, async () => {
    const disposalType = document.getElementById('asset-dispose-type').value;
    const proceedsRaw = document.getElementById('asset-dispose-proceeds').value;
    const proceeds = proceedsRaw === '' ? 0 : Number(proceedsRaw);
    if (!Number.isFinite(proceeds) || proceeds < 0) { assetActionError('Enter the proceeds as 0 or more.'); return; }
    const receivedInto = document.getElementById('asset-dispose-into').value;
    // The exact entry, from the same calculation the disposal will post.
    const pres = await apiFetch(assetDisposalPreviewURL(assetId, proceeds, receivedInto));
    if (!pres) return;
    if (!pres.ok) { assetActionError(await getErrorMessage(pres, 'Could not work out the disposal.')); return; }
    const p = await pres.json();
    const lines = (p.lines || []).map(l => `${l.debit ? 'Dr' : 'Cr'} ${l.account_code} ${l.account_name || ''} ${assetMoney(l.debit || l.credit)}`).join('\n');
    const first = (p.depreciation_first || []).length
      ? `\nDepreciation posted first: ${p.depreciation_first.length} month(s), ${p.depreciation_first[0]} to ${p.depreciation_first[p.depreciation_first.length - 1]}.` : '';
    const skipped = (p.depreciation_skipped || []).length
      ? `\nNot posted (closed period): ${p.depreciation_skipped.map(s => s.month).join(', ')} - that depreciation becomes part of the ${p.gain_loss >= 0 ? 'gain' : 'loss'}.` : '';
    if (!await showCustomConfirm(
      `Dispose of ${p.code || assetId}? This cannot be undone.${first}${skipped}\n\nBook value ${assetMoney(p.book_value)}, proceeds ${assetMoney(p.proceeds)}: ${assetGainLossText(p.gain_loss)}.\n\nPosts:\n${lines}`,
      'Dispose asset')) return;
    const res = await apiFetch('/api/v1/assets/dispose', {
      method: 'POST',
      body: JSON.stringify({ asset_id: assetId, disposal_type: disposalType, proceeds, received_into: receivedInto })
    });
    if (!res) return;
    if (!res.ok) { assetActionError(await getErrorMessage(res, 'Failed to dispose asset.')); return; }
    renderView('assets');
  });

  const status = panel.querySelector('#asset-dispose-preview');
  const proceedsEl = panel.querySelector('#asset-dispose-proceeds');
  const intoEl = panel.querySelector('#asset-dispose-into');
  const res = await apiFetch(assetDisposalPreviewURL(assetId, 0, '1100'));
  if (!res) return;
  if (!res.ok) { status.textContent = ''; assetActionError(await getErrorMessage(res, 'Could not work out the book value.')); return; }
  const base = await res.json();
  if ((base.receipt_accounts || []).length) {
    intoEl.innerHTML = base.receipt_accounts.map(a =>
      `<option value="${escapeHTMLText(a.code)}"${a.code === '1100' ? ' selected' : ''}>${escapeHTMLText(a.code)} ${escapeHTMLText(a.name || '')}</option>`).join('');
  }
  const showPreview = () => {
    const proceeds = proceedsEl.value === '' ? 0 : Number(proceedsEl.value);
    if (!Number.isFinite(proceeds) || proceeds < 0) { status.textContent = 'Enter the proceeds as 0 or more.'; return; }
    const gainLoss = Math.round((proceeds - base.book_value) * 100) / 100;
    const due = (base.depreciation_first || []).length;
    status.textContent = `Book value ${assetMoney(base.book_value)} (cost ${assetMoney(base.cost)} less depreciation ${assetMoney(base.accumulated_depreciation)}`
      + `${due ? `, including ${due} month(s) still to post` : ''}). Proceeds ${assetMoney(proceeds)}: ${assetGainLossText(gainLoss)}.`;
  };
  proceedsEl.addEventListener('input', showPreview);
  showPreview();
  proceedsEl.focus();
}

function assetGainLossText(v) {
  const n = Number(v) || 0;
  if (n > 0) return `gain of ${assetMoney(n)} to 4300 Gain on Asset Disposal`;
  if (n < 0) return `loss of ${assetMoney(-n)} to 5300 Loss on Asset Disposal`;
  return 'no gain or loss';
}

function assetMoney(v) {
  return Number(v || 0).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// Super Admin, an Accountant, or a role named Finance Head - the server's
// CanManagePeriodClose.
function canRunAssetDepreciation() {
  const role = String(localStorage.getItem('erp_role') || '').toLowerCase().replace(/\s+/g, '');
  return ['superadmin', 'hr/admin', 'hradmin', 'admin', 'accountant', 'financehead'].includes(role);
}

async function runAssetDepreciation() {
  if (!await showCustomConfirm(
    'Post depreciation for every capitalised asset, for each month that has ended and is not posted yet?\n\n'
    + 'Each month posts Dr 5310 Depreciation Expense / Cr 1410 Accumulated Depreciation, dated at its month-end. '
    + 'A month inside a closed accounting period is skipped and posts once that period is reopened. '
    + 'Running it again never posts a month twice.',
    'Run depreciation')) return;
  const res = await apiFetch('/api/v1/assets/depreciation/run', { method: 'POST', body: '{}' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Depreciation was not run.', 'Run depreciation'); return; }
  const r = await res.json();
  const box = document.createElement('div');
  const line = text => { const d = document.createElement('div'); d.textContent = text; box.appendChild(d); return d; };
  line(`Through ${r.through_month}: ${r.months_posted} month(s) posted, ${assetMoney(r.amount_posted)} in all, across ${r.assets_checked} capitalised asset(s).`);
  (r.assets || []).forEach(a => {
    const parts = [];
    if (a.months_posted.length) parts.push(`${a.months_posted.length} month(s) posted (${assetMoney(a.amount_posted)})`);
    a.months_skipped.forEach(s => parts.push(`${s.month} not posted: ${s.reason}`));
    if (a.note) parts.push(a.note);
    line(`${a.code || a.asset_id}: ${parts.join('; ')}`).style.marginTop = '6px';
  });
  if (!r.months_posted && !r.months_skipped) line('Nothing was due - every month that has ended is already posted.').style.marginTop = '6px';
  await showCustomAlert(box, 'Depreciation run');
  renderView('assets');
}

// Stage 55.16: one asset's lifecycle - source, straight-line schedule, the GL
// lines posted for it (and by whom), transfers, and what the books do not hold.
async function toggleAssetLifecycle(assetId) {
  const row = document.querySelector(`tr[data-asset-detail="${CSS.escape(assetId)}"]`);
  if (!row) return;
  if (!row.classList.contains('hidden')) { row.classList.add('hidden'); return; }
  const res = await apiFetch(`/api/v1/assets/${encodeURIComponent(assetId)}/lifecycle`);
  if (!res) return;
  const cell = row.querySelector('td');
  if (!res.ok) {
    cell.innerHTML = `<span class="login-error">${escapeHTMLText(await getErrorMessage(res, 'Could not load the asset history.'))}</span>`;
    row.classList.remove('hidden');
    return;
  }
  const lc = await res.json();
  const fmt = v => Number(v || 0).toLocaleString('en-IN');
  const a = lc.asset;
  const acq = lc.acquisition || {};
  const dep = lc.depreciation || {};
  cell.innerHTML = `
    <div style="padding:8px 0;display:grid;gap:12px">
      <div><b>Source:</b> ${escapeHTMLText(acq.source || '')}${acq.item_code ? ` (item ${escapeHTMLText(acq.item_code)})` : ''}
        ${acq.acquisition_date ? ` &middot; acquired ${escapeHTMLText(acq.acquisition_date)}` : ''}
        &middot; cost ${fmt(a.cost)} &middot; useful life ${a.useful_life_years || 'not set'} yr
        ${a.capitalisation_date ? ` &middot; capitalised ${escapeHTMLText(a.capitalisation_date)}` : ''}
        ${acq.disposal_type ? ` &middot; disposed (${escapeHTMLText(acq.disposal_type)})` : ''}
        ${acq.disposal_proceeds != null ? ` on ${escapeHTMLText(acq.disposal_date || '')}: proceeds ${assetMoney(acq.disposal_proceeds)} into ${escapeHTMLText(acq.disposal_received_into || '')}, book value ${assetMoney(acq.disposal_book_value)}, ${escapeHTMLText(assetGainLossText(acq.disposal_gain_loss))}` : ''}</div>
      ${lc.schedule.length ? `<table><thead><tr><th>Year</th><th>From</th><th>To</th><th>Opening</th><th>Depreciation</th><th>Posted</th><th>To come</th><th>Closing</th></tr></thead><tbody>
        ${lc.schedule.map(s => `<tr style="${s.current ? 'font-weight:700' : ''}"><td>${s.year}${s.current ? ' (now)' : ''}</td><td>${escapeHTMLText(s.from)}</td><td>${escapeHTMLText(s.to)}</td><td>${assetMoney(s.opening)}</td><td>${assetMoney(s.depreciation)}</td><td>${assetMoney(s.posted)}${s.months_posted ? ` (${s.months_posted} of ${s.months} mo)` : ''}</td><td>${assetMoney(s.to_come)}</td><td>${assetMoney(s.closing)}</td></tr>`).join('')}
        </tbody></table>` : '<div style="color:var(--text-muted)">No schedule until a useful life is set.</div>'}
      ${dep.total_months ? `<div><b>Depreciation:</b> ${assetMoney(dep.monthly_amount)} a month for ${dep.total_months} months &middot; ${dep.months_posted} month(s) posted, ${assetMoney(dep.posted)}${a.status === 'Capitalised' ? ` &middot; ${dep.months_due} month(s) ended so far, ${assetMoney(dep.due)} due` : ''}</div>` : ''}
      ${(dep.postings || []).length ? `<details><summary>Depreciation posted (${dep.postings.length} month${dep.postings.length === 1 ? '' : 's'})</summary>
        <table><thead><tr><th>Month</th><th>Dr 5310 / Cr 1410</th><th>Dated</th><th>Posted by</th></tr></thead><tbody>
        ${dep.postings.map(p => `<tr><td>${escapeHTMLText(p.month)}</td><td>${assetMoney(p.amount)}</td><td>${escapeHTMLText(p.dated)}</td><td>${escapeHTMLText(p.posted_by)}</td></tr>`).join('')}
        </tbody></table></details>` : ''}
      <div><b>Posted to the GL:</b> ${lc.gl_postings.length === 0 ? 'nothing yet' : ''}</div>
      ${lc.gl_postings.length ? `<table><thead><tr><th>When</th><th>Event</th><th>Account</th><th>Debit</th><th>Credit</th><th>Posted by</th></tr></thead><tbody>
        ${lc.gl_postings.map(p => `<tr><td>${escapeHTMLText(p.posted_at)}</td><td>${escapeHTMLText(p.event)}</td><td>${escapeHTMLText(p.account_code)}</td><td>${p.debit ? fmt(p.debit) : ''}</td><td>${p.credit ? fmt(p.credit) : ''}</td><td>${escapeHTMLText(p.posted_by)}</td></tr>`).join('')}
        </tbody></table>` : ''}
      ${lc.transfers.length ? `<div><b>Transfers:</b><ul style="margin:4px 0 0 18px">${lc.transfers.map(t => `<li>${escapeHTMLText(t.at)} by ${escapeHTMLText(t.by)} - ${escapeHTMLText(t.detail)}</li>`).join('')}</ul></div>` : ''}
      ${lc.gaps.map(g => `<div style="color:var(--warning-color, #b45309)">${escapeHTMLText(g)}</div>`).join('')}
    </div>`;
  row.classList.remove('hidden');
}
