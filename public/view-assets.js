// Fixed Asset Management (Stage 13.13b, MB 16.1) - lifecycle:
// Draft -> Capitalised -> (Transfer any number of times) -> Disposed.
// Depreciation/net block are calculated by the backend on every fetch, not
// stored, so they're always current as of "now."
//
// BLD-041: native ES module loaded with import() by the authorized view
// dispatcher. Shared services resolve from the classic app shell.
async function renderAssetsView(container) {
  const res = await apiFetch('/api/v1/assets/register');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Fixed Assets</h1>
      <p class="page-subtitle">Asset register with calculated straight-line depreciation and net block.</p>
    </div>
  `;
  container.appendChild(header);

  const assets = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Asset (Draft)</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-code">Asset Number</label>
        <input type="text" id="asset-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-category">Category</label>
        <input type="text" id="asset-category" class="form-input" style="width: 130px;">
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
  container.appendChild(formPanel);

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
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No assets yet. Use <b>Create</b> above to capitalise your first fixed asset.</td></tr>`
    : assets.map(a => `
        <tr>
          <td style="font-family: monospace;">${escapeHTMLText(a.code || a.id)}${a.source_grn ? `<div style="font-family: inherit; font-size: 12px; color: var(--text-muted);">${escapeHTMLText(a.item_code || '')} from GRN ${escapeHTMLText(a.source_grn)}</div>` : ''}</td>
          <td>${escapeHTMLText(a.category || '')}</td>
          <td>${escapeHTMLText(a.location || '')}</td>
          <td>${escapeHTMLText(a.custodian || '')}</td>
          <td>${a.cost.toLocaleString()}</td>
          <td>${a.accumulated_depreciation.toLocaleString()}</td>
          <td>${a.net_block.toLocaleString()}</td>
          <td><span class="badge ${a.status === 'Capitalised' ? 'badge-success' : a.status === 'Disposed' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(a.status)}</span></td>
          <td>${renderAssetActions(a)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);
  listPanel.addEventListener('click', event => {
    const button = event.target.closest('[data-asset-action]');
    if (!button) return;
    const assetID = button.dataset.assetId;
    if (button.dataset.assetAction === 'capitalize') capitalizeAsset(assetID, Number(button.dataset.assetLife) || 0);
    else if (button.dataset.assetAction === 'transfer') promptTransferAsset(assetID);
    else if (button.dataset.assetAction === 'dispose') promptDisposeAsset(assetID);
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

async function capitalizeAsset(assetId, usefulLifeYears) {
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
  const anchor = document.getElementById('asset-create-btn')?.closest('.table-panel');
  if (anchor) anchor.after(panel); else document.getElementById('view-root').prepend(panel);
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

async function promptDisposeAsset(assetId) {
  openAssetActionPanel(`Dispose of asset ${assetId}`, `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="asset-dispose-type">How is it leaving?<span class="required">*</span></label>
      <select id="asset-dispose-type" class="form-input" style="width: 200px;">
        <option value="Sale">Sold</option>
        <option value="Scrap" selected>Scrapped</option>
        <option value="WriteOff">Written off (lost, stolen)</option>
      </select>
    </div>
    <p style="margin: 0; font-size: 13px; color: var(--text-muted); max-width: 360px;">The remaining book value is written off and the asset is closed. This cannot be undone.</p>
  `, async () => {
    const disposalType = document.getElementById('asset-dispose-type').value;
    const res = await apiFetch('/api/v1/assets/dispose', {
      method: 'POST',
      body: JSON.stringify({ asset_id: assetId, disposal_type: disposalType })
    });
    if (!res) return;
    if (!res.ok) { assetActionError(await getErrorMessage(res, 'Failed to dispose asset.')); return; }
    renderView('assets');
  });
}
