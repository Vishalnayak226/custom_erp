// Stage 127.6 (user, 2026-10-21: "I need reprint setion for all soort of
// preint and if will limit client to client if i give less moduel access to
// them"): every document printed - what, when, how and by whom - for the
// modules the client has and the documents this person may open (decided
// 2026-10-21: only the modules given). Reprint prints it again and asks why;
// a document never printed is found by its type and number.

const esc = (s) => window.escapeHTMLText(s == null ? '' : String(s));
// printed_at is the database clock; shown as stored, not shifted by the
// browser's time zone.
const when = (ts) => {
  const m = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/.exec(String(ts || ''));
  return m ? `${m[3]}-${m[2]}-${m[1]} ${m[4]}:${m[5]}` : '';
};
const docs = () => import('./trade-docs.js' + new URL(import.meta.url).search);
let rpRows = [];

function documentTypes() {
  return ((typeof state !== 'undefined' && state.activeDoctypes) || [])
    .filter(d => d.document_type !== 'Master')
    .map(d => ({ name: d.name, label: typeof getDoctypeLabel === 'function' ? getDoctypeLabel(d.name) : d.name }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

export async function renderReprintView(container) {
  container.innerHTML = `
    <div class="page-header"><div class="page-title-section"><h1 class="page-title">Reprint</h1></div></div>
    <div class="table-panel" style="padding: 16px; margin-bottom: 16px;">
      <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
        <label class="form-label" style="margin-bottom: 0;">Document
          <select id="rp-doctype" class="form-input" style="width: 230px;">
            <option value="">All documents</option>
            ${documentTypes().map(d => `<option value="${esc(d.name)}">${esc(d.label)}</option>`).join('')}
          </select>
        </label>
        <label class="form-label" style="margin-bottom: 0; flex: 1; min-width: 220px;">Number or text
          <input id="rp-q" class="form-input" type="search" placeholder="Number, name or who printed it" autocomplete="off">
        </label>
        <button class="btn btn-outline" id="rp-search" type="button">Search</button>
        <button class="btn btn-primary" id="rp-print-any" type="button">Print this number</button>
      </div>
      <div id="rp-error" class="hidden" role="alert" style="color: var(--danger-color, #b3261e); margin-top: 8px;"></div>
    </div>
    <div class="table-panel" style="overflow-x: auto;">
      <table>
        <thead><tr><th>When</th><th>Document</th><th>Number</th><th>Store</th><th>How</th><th>By</th><th>Reprint reason</th><th></th></tr></thead>
        <tbody id="rp-rows"><tr><td colspan="8" class="text-muted">Loading…</td></tr></tbody>
      </table>
    </div>
    <p class="text-muted" style="margin-top: 12px; font-size: 12.5px;">Labels and barcode stickers are reprinted from Sticker Printing → Print history.</p>`;

  const q = document.getElementById('rp-q');
  const typeSel = document.getElementById('rp-doctype');
  document.getElementById('rp-search').addEventListener('click', loadReprintRows);
  typeSel.addEventListener('change', loadReprintRows);
  q.addEventListener('keydown', (e) => { if (e.key === 'Enter') loadReprintRows(); });
  document.getElementById('rp-print-any').addEventListener('click', printByNumber);
  document.getElementById('rp-rows').addEventListener('click', async (e) => {
    const b = e.target.closest('[data-rp]');
    if (!b) return;
    const row = rpRows[Number(b.dataset.rp)];
    if (row) (await docs()).printTransactionDocument(row.doctype, row.doc_id, { txn: row.txn, reprint: true, onClose: loadReprintRows });
  });
  await loadReprintRows();
}

function showReprintError(text) {
  const el = document.getElementById('rp-error');
  if (!el) return;
  el.textContent = text || '';
  el.classList.toggle('hidden', !text);
}

async function loadReprintRows() {
  showReprintError('');
  const body = document.getElementById('rp-rows');
  if (!body) return;
  const qs = new URLSearchParams({ doctype: document.getElementById('rp-doctype').value, q: document.getElementById('rp-q').value.trim() });
  const res = await window.apiFetch(`/api/v1/print/history?${qs}`);
  if (!res || !res.ok) {
    body.innerHTML = '<tr><td colspan="8" class="text-muted">The print history could not be loaded.</td></tr>';
    if (res) await window.showApiError(res, 'Could not load the print history.');
    return;
  }
  rpRows = await res.json();
  const label = (r) => r.title || (typeof getDoctypeLabel === 'function' ? getDoctypeLabel(r.doctype) : r.doctype);
  body.innerHTML = rpRows.length ? rpRows.map((r, i) => `<tr>
      <td style="white-space: nowrap;">${esc(when(r.printed_at))}</td><td>${esc(label(r))}</td><td>${esc(r.number || r.doc_id)}</td>
      <td>${esc(r.store)}</td><td>${esc(r.action)}</td><td>${esc(r.printed_by)}</td><td>${esc(r.reprint_reason || '')}</td>
      <td><button class="action-btn" type="button" data-rp="${i}">Reprint</button></td></tr>`).join('')
    : '<tr><td colspan="8" class="text-muted">Nothing printed yet matches. A document printed before this list began can still be printed with Print this number.</td></tr>';
}

// printByNumber finds one document of the chosen type by its number - printed
// before or not - and opens its print.
async function printByNumber() {
  showReprintError('');
  const doctype = document.getElementById('rp-doctype').value;
  const text = document.getElementById('rp-q').value.trim();
  if (!doctype || !text) { showReprintError('Pick the document and type its number first.'); return; }
  const res = await window.apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}?q=${encodeURIComponent(text)}&limit=20`);
  if (!res || !res.ok) { if (res) await window.showApiError(res, 'Could not look the document up.'); return; }
  const rows = (await res.json()) || [];
  const lower = text.toLowerCase();
  const idOf = d => String(d.id || d.code || '');
  const exact = rows.find(d => idOf(d).toLowerCase() === lower || String(d.code || '').toLowerCase() === lower);
  const ends = rows.filter(d => idOf(d).toLowerCase().endsWith(lower));
  const pick = exact || (ends.length === 1 ? ends[0] : (rows.length === 1 ? rows[0] : null));
  if (!pick) {
    showReprintError(rows.length ? `${rows.length} documents match "${text}" - type more of the number.` : `No document has "${text}" in its number.`);
    return;
  }
  (await docs()).printTransactionDocument(doctype, idOf(pick), { onClose: loadReprintRows });
}
