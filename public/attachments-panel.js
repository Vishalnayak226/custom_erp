// Stage 74.1.6: files attached to any document that takes them (the server's
// allow-list, engines/attachments.go) - a vendor's invoice copy on a goods
// receipt, a proof on a return. One panel for every screen that shows a
// document's files; the server decides who may add or remove, and when.
//
//   renderAttachmentsPanel(holder, 'GRN', grnId, { title: 'Vendor invoice copy' })

const TYPES = 'image/jpeg,image/png,image/webp,application/pdf';
const MAX_BYTES = 5 * 1024 * 1024;
const MAX_FILES = 5;
let previewURL = null;

const esc = (s) => escapeHTMLText(s == null ? '' : String(s));
const size = (b) => {
  const n = Number(b || 0);
  return n >= 1024 * 1024 ? `${(n / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;
};

export async function renderAttachmentsPanel(holder, doctype, id, opts = {}) {
  if (!holder) return;
  const base = `/api/v1/attachments/${encodeURIComponent(doctype)}/${encodeURIComponent(id)}`;
  const res = await apiFetch(base);
  if (!res) return;
  if (!res.ok) {
    holder.innerHTML = `<div class="text-muted">${esc(await getErrorMessage(res, 'The attached files could not be loaded.'))}</div>`;
    return;
  }
  const body = await res.json();
  const files = body.attachments || [];
  holder.innerHTML = `<div class="att-panel">
    <strong>${esc(opts.title || 'Attached files')}</strong>
    ${files.length === 0 ? `<div class="text-muted">${esc(opts.empty || 'Nothing attached.')}</div>` : `<table><thead><tr><th>File</th><th>Size</th><th>Added by</th><th>Added</th><th></th></tr></thead><tbody>
      ${files.map(f => `<tr><td>${esc(f.file_name)}</td><td>${size(f.size_bytes)}</td><td>${esc(f.uploaded_by || '')}</td><td>${esc((f.created_at || '').replace('T', ' ').slice(0, 16))}</td>
        <td><button type="button" class="action-btn" data-att-view="${esc(f.id)}">View</button>${f.can_remove ? ` <button type="button" class="action-btn" data-att-remove="${esc(f.id)}">Remove</button>` : ''}</td></tr>`).join('')}
    </tbody></table>`}
    ${body.can_add ? `<div class="att-add">
      <input type="file" class="form-input" accept="${TYPES}" multiple data-att-input aria-label="${esc(opts.title || 'Files')} to attach">
      <button type="button" class="btn btn-outline btn-sm" data-att-add>Attach</button></div>` : ''}
    <div class="login-error hidden" data-att-error></div>
    <div data-att-preview></div>
  </div>`;
  const errorEl = holder.querySelector('[data-att-error]');
  const showError = (msg) => { errorEl.textContent = msg; errorEl.classList.toggle('hidden', !msg); };
  holder.querySelectorAll('[data-att-view]').forEach(b => b.addEventListener('click', () => {
    const file = files.find(f => f.id === b.dataset.attView);
    if (file) viewFile(holder.querySelector('[data-att-preview]'), file);
  }));
  holder.querySelectorAll('[data-att-remove]').forEach(b => b.addEventListener('click', async () => {
    if (!await showCustomConfirm('Remove this file?', 'Remove file')) return;
    const del = await apiFetch(`/api/v1/attachments/file/${encodeURIComponent(b.dataset.attRemove)}`, { method: 'DELETE' });
    if (!del) return;
    if (!del.ok) { showError(await getErrorMessage(del, 'The file could not be removed.')); return; }
    await renderAttachmentsPanel(holder, doctype, id, opts);
  }));
  const addBtn = holder.querySelector('[data-att-add]');
  if (addBtn) addBtn.addEventListener('click', () => guardAgainstDoubleSubmit(addBtn, 'Attaching...', async () => {
    const picked = Array.from(holder.querySelector('[data-att-input]').files || []);
    if (!picked.length) { showError('Choose a file first.'); return; }
    if (picked.length + files.length > MAX_FILES) { showError(`A document can hold at most ${MAX_FILES} files.`); return; }
    const tooBig = picked.find(f => f.size > MAX_BYTES);
    if (tooBig) { showError(`${tooBig.name} is larger than 5 MB.`); return; }
    const failed = [];
    for (const file of picked) {
      const fd = new FormData();
      fd.append('file', file, file.name);
      const up = await apiUpload(base, fd);
      if (!up) failed.push(`${file.name}: not sent`);
      else if (!up.ok) failed.push(`${file.name}: ${await getErrorMessage(up, 'could not be attached')}`);
    }
    await renderAttachmentsPanel(holder, doctype, id, opts);
    if (failed.length) {
      const el = holder.querySelector('[data-att-error]');
      el.textContent = failed.join(' ');
      el.classList.remove('hidden');
    }
  }));
}

async function viewFile(previewEl, file) {
  const res = await apiFetch(`/api/v1/attachments/file/${encodeURIComponent(file.id)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The file could not be opened.'); return; }
  const blob = await res.blob();
  if (previewURL) URL.revokeObjectURL(previewURL);
  previewURL = URL.createObjectURL(blob);
  if (blob.type.startsWith('image/')) {
    previewEl.innerHTML = `<div><button type="button" class="action-btn" data-att-close>Close preview</button></div>
      <img alt="${esc(file.file_name)}" class="att-preview-img">`;
    previewEl.querySelector('img').src = previewURL;
    previewEl.querySelector('[data-att-close]').addEventListener('click', () => { previewEl.innerHTML = ''; });
    return;
  }
  window.open(previewURL, '_blank', 'noopener');
}
