// Stage 148.40: correct a batch's or serial's attributes (purity, karat,
// weights) from the Correct button on its row in Setup / Master Data. The
// values on the left, who changed what and why on the right. Everything goes
// through /api/v1/inventory/pieces/{Batch|SerialNumber}/{id}.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
// Stored in UTC; shown in the user's own time.
const when = (v) => {
  const d = new Date(v);
  if (!v || isNaN(d)) return v ? String(v) : '';
  const p = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
};

function historyRows(history) {
  if (!history.length) return '<tr><td colspan="4" class="text-center text-muted">No corrections</td></tr>';
  return history.slice().reverse().map(h => {
    const keys = Object.keys(h.new || {});
    const change = keys.map(k => `${esc(k)}: ${esc((h.old || {})[k] || '-')} → ${esc(h.new[k] || '-')}`).join('<br>');
    return `<tr><td>${esc(when(h.at))}</td><td>${esc(h.by)}</td><td>${change}</td><td>${esc(h.reason)}</td></tr>`;
  }).join('');
}

function fieldInput(f, value) {
  const name = `data-pc="${esc(f.key)}"`;
  if (f.options && f.options.length) {
    const opts = ['', ...f.options].map(o => `<option value="${esc(o)}"${o === value ? ' selected' : ''}>${esc(o)}</option>`).join('');
    return `<select class="form-input" ${name}>${opts}</select>`;
  }
  return `<input class="form-input" ${name} type="${f.number ? 'number' : 'text'}"${f.number ? ' min="0" step="any"' : ''} value="${esc(value)}">`;
}

export async function openPieceCorrection(doctype, id) {
  const url = `/api/v1/inventory/pieces/${encodeURIComponent(doctype)}/${encodeURIComponent(id)}`;
  const res = await apiFetch(url);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load this piece.'); return; }
  const view = await res.json();

  document.getElementById('piece-correction-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'piece-correction-modal';
  const draw = (v) => {
    overlay.innerHTML = `
      <div class="modal-container" style="max-width:920px;width:94vw;">
        <div class="modal-header"><h3 class="modal-title">Correct ${esc(v.ref || id)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
        <div class="modal-body" style="display:flex;gap:20px;flex-wrap:wrap;align-items:flex-start;">
          <div style="flex:1 1 280px;min-width:260px;">
            ${v.fields.map(f => `<div class="form-group"><label class="form-label">${esc(f.label)}</label>${fieldInput(f, v.values[f.key] || '')}</div>`).join('')}
            <div class="form-group"><label class="form-label">Reason</label><input class="form-input" data-pc-reason type="text"></div>
          </div>
          <div class="table-wrapper" style="flex:2 1 380px;min-width:300px;">
            <table><thead><tr><th>When</th><th>Who</th><th>Change</th><th>Reason</th></tr></thead><tbody>${historyRows(v.history)}</tbody></table>
          </div>
        </div>
        <div class="modal-footer"><button type="button" class="btn btn-secondary" data-pc-close>Close</button><button type="button" class="btn btn-primary" data-pc-save>Save</button></div>
      </div>`;
  };
  draw(view);
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.addEventListener('click', async (e) => {
    if (e.target.closest('.modal-close, [data-pc-close]')) return close();
    const save = e.target.closest('[data-pc-save]');
    if (!save) return;
    const values = {};
    overlay.querySelectorAll('[data-pc]').forEach(el => {
      const key = el.dataset.pc, cur = view.values[key] || '';
      if (el.value.trim() !== cur) values[key] = el.value.trim();
    });
    const reason = overlay.querySelector('[data-pc-reason]').value.trim();
    save.disabled = true;
    const out = await apiFetch(`${url}/correct`, { method: 'POST', body: JSON.stringify({ values, reason }) });
    save.disabled = false;
    if (!out) return;
    if (!out.ok) { await showApiError(out, 'Could not save the correction.'); return; }
    Object.assign(view, await out.json());
    draw(view);
    showToast('Saved', { variant: 'success' });
  });
}
