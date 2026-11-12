// Stage 148.41: hold a serial piece for a customer. Loaded on demand by the
// Serial number list. Shared services (apiFetch, showApiError, showToast,
// showCustomConfirm, guardAgainstDoubleSubmit, attachLinkTypeahead, renderView,
// escapeHTMLText) are globals.

async function phPost(path, body, failText) {
  const res = await apiFetch(path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

// Shows the header button only when holds are switched on.
async function pieceHoldHeaderButton(header) {
  const res = await apiFetch('/api/v1/inventory/piece-holds').catch(() => null);
  const info = res && res.ok ? await res.json() : null;
  if (!info || !info.enabled || !header || header.querySelector('#doc-piece-hold')) return;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'btn btn-outline';
  btn.id = 'doc-piece-hold';
  btn.textContent = 'Hold piece';
  btn.addEventListener('click', openPieceHold);
  header.appendChild(btn);
}

function openPieceHold() {
  document.getElementById('ph-hold')?.remove();
  const fields = [
    { name: 'sku', label: 'Item' }, { name: 'serial', label: 'Piece' }, { name: 'customer', label: 'Customer' },
    { name: 'until', label: 'Hold until', type: 'date' }, { name: 'advance', label: 'Advance', type: 'number' }, { name: 'note', label: 'Note' },
  ];
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'ph-hold';
  const rows = fields.map(f => `<div class="form-group"><label class="form-label" for="ph-hold-${f.name}">${escapeHTMLText(f.label)}</label>` +
    `<input id="ph-hold-${f.name}" class="form-input${f.type === 'number' ? ' num' : ''}" type="${f.type || 'text'}" ${f.type === 'number' ? 'min="0" step="0.01"' : ''} autocomplete="off"></div>`).join('');
  overlay.innerHTML = `
    <div class="modal-container s-c31abe" role="dialog" aria-modal="true" aria-labelledby="ph-hold-title">
      <div class="modal-header"><h3 class="modal-title" id="ph-hold-title">Hold piece</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-ph-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-63bcac">${rows}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-ph-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-ph-save>Hold</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-ph-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const get = name => overlay.querySelector(`#ph-hold-${name}`).value.trim();
  attachLinkTypeahead(overlay.querySelector('#ph-hold-customer'), 'Customer', { keepCode: true });
  const save = overlay.querySelector('[data-ph-save]');
  save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
    const out = await phPost('/api/v1/inventory/piece-holds', {
      sku: get('sku'), serial_no: get('serial'), customer_id: get('customer'), until: get('until'),
      advance: Number(get('advance')) || 0, note: get('note'),
    }, 'The piece was not held.');
    if (out) { showToast('Piece held.', { variant: 'success' }); close(); renderView('doctype-table'); }
  }));
}

async function releasePieceHold(row) {
  if (!row || !row.serial_no) return;
  if (!(await showCustomConfirm(`Release the hold on ${row.serial_no}?`, 'Release'))) return;
  const out = await phPost('/api/v1/inventory/piece-holds/release', { sku: row.item || '', serial_no: row.serial_no, reason: '' }, 'The hold was not released.');
  if (out) { showToast('Hold released.', { variant: 'success' }); renderView('doctype-table'); }
}

export { pieceHoldHeaderButton, openPieceHold, releasePieceHold };
