// Stage 146.5.4: one wide product sheet on the Item list. Item fields,
// "attr:<code>" attribute columns and "content:<lang>:<field>" content columns
// in one CSV: preview first, then import all or nothing.

export function renderProductSheetButton(host) {
  const esc = escapeHTMLText;
  host.style.cssText = 'margin:0 0 10px;';
  host.innerHTML = '<button type="button" class="btn btn-outline btn-sm" data-sheet-open>Product sheet</button><div data-sheet-panel></div>';
  const panelHost = host.querySelector('[data-sheet-panel]');
  host.querySelector('[data-sheet-open]').addEventListener('click', () => {
    if (panelHost.firstChild) { panelHost.innerHTML = ''; return; }
    panelHost.innerHTML = `<div class="table-panel s-247f09">
      <div class="s-73d754">
        <strong>Product sheet</strong>
        <input class="form-input s-30e741" type="file" accept=".csv,text/csv" data-sheet-file>
        <button type="button" class="btn btn-outline btn-sm" data-sheet-preview>Preview</button>
        <button type="button" class="btn btn-primary btn-sm" data-sheet-commit disabled>Import</button>
        <button type="button" class="btn btn-outline btn-sm" data-sheet-close>Close</button></div>
      <div data-sheet-result role="status" class="s-8a77e5"></div></div>`;
    const file = panelHost.querySelector('[data-sheet-file]');
    const out = panelHost.querySelector('[data-sheet-result]');
    const preview = panelHost.querySelector('[data-sheet-preview]');
    const commit = panelHost.querySelector('[data-sheet-commit]');
    panelHost.querySelector('[data-sheet-close]').addEventListener('click', () => { panelHost.innerHTML = ''; });
    file.addEventListener('change', () => { commit.disabled = true; out.textContent = ''; });
    const run = async (doCommit) => {
      if (!file.files[0]) { out.textContent = 'Choose a file.'; return; }
      preview.disabled = commit.disabled = true;
      const body = new FormData();
      body.append('file', file.files[0]);
      const res = await apiUpload(`/api/v1/pim/product-sheet/${doCommit ? 'import' : 'preview'}`, body);
      preview.disabled = false;
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'The sheet could not be read.'); return; }
      const r = await res.json();
      const errors = r.errors || [];
      const unknown = r.unknown_headers || [];
      const created = r.items?.created_ids?.length || 0;
      const updated = r.items?.updated_ids?.length || 0;
      out.innerHTML = `<p><strong>${r.committed ? 'Imported' : doCommit ? 'Nothing imported' : 'Preview, nothing written'}</strong>:
          ${esc(r.rows)} rows, ${esc(created)} new, ${esc(updated)} changed, ${esc(r.attributes)} attributes, ${esc(r.contents)} content</p>
        ${unknown.length ? `<p>Not used: ${unknown.map(esc).join(', ')}</p>` : ''}
        ${errors.length ? `<div class="table-wrapper"><table><thead><tr><th>Row</th><th>Problem</th></tr></thead><tbody>${errors.map(e => `<tr><td>${esc(e.row_number)}</td><td>${esc(e.message)}</td></tr>`).join('')}</tbody></table></div>` : ''}`;
      commit.disabled = doCommit || r.rows < 1 || errors.length > 0;
      if (doCommit && r.committed) file.value = '';
    };
    preview.addEventListener('click', () => run(false));
    commit.addEventListener('click', () => run(true));
  });
}
