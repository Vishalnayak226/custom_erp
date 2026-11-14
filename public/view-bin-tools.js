// Stage 146.3.4 - Bin Generator and bin labels.
// Lazy view: POST /api/v1/wms/bin-generator (preview / create). Stage 146.3.4b:
// labels print through a sticker template (the built-in "Bin label 50 × 25"
// or any template of the client's own) to this computer's sticker printer;
// A4 sheet keeps the plain Code 128 sheet for an office printer.

async function renderBinToolsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `<div class="page-title-section"><h1 class="page-title">Bin Generator</h1></div>`;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  const field = (id, label, w, ph) => `<div class="form-group"><label class="form-label" for="${id}">${label}</label>
    <input type="text" id="${id}" class="form-input" style="width: ${w}px;" autocomplete="off" placeholder="${ph || ''}"></div>`;
  panel.innerHTML = `
    <div class="s-608a43">
      ${field('bt-location', 'Location', 150)}
      ${field('bt-zone', 'Zone (optional)', 130)}
      ${field('bt-aisles', 'Aisles', 100, 'A-C')}
      ${field('bt-racks', 'Racks', 100, '01-12')}
      ${field('bt-shelves', 'Shelves', 100, '1-4')}
      ${field('bt-bins', 'Bins', 100, '1-6')}
      ${field('bt-format', 'Code format', 220, '{aisle}-{rack}-{shelf}-{bin}')}
      <button class="btn btn-outline" id="bt-preview" type="button">Preview</button>
      <button class="btn btn-primary" id="bt-create" type="button">Create</button>
      <button class="btn btn-outline" id="bt-print" type="button">Print labels</button>
    </div>
    <div id="bt-summary" class="s-e7706a"></div>
    <div id="bt-error" class="login-error hidden s-4c0544"></div>
    <div id="bt-rows" class="s-4bb900"></div>`;
  container.appendChild(panel);

  attachLinkTypeahead(document.getElementById('bt-location'), 'Location');
  attachLinkTypeahead(document.getElementById('bt-zone'), 'Zone');
  let lastBins = [];
  const val = id => document.getElementById(id).value.trim();

  async function run(preview) {
    const errEl = document.getElementById('bt-error');
    errEl.classList.add('hidden');
    const res = await apiFetch('/api/v1/wms/bin-generator', {
      method: 'POST',
      body: JSON.stringify({
        location: val('bt-location'), zone: val('bt-zone'), format: val('bt-format'),
        aisles: val('bt-aisles'), racks: val('bt-racks'), shelves: val('bt-shelves'), bins: val('bt-bins'), preview,
      }),
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not generate bins.', 'Bin Generator'); return; }
    const data = await res.json();
    lastBins = (data.rows || []).map(r => ({ bin_code: r.code, location: val('bt-location'), zone: val('bt-zone'),
      aisle: r.aisle, rack: r.rack, level: r.level, position: r.position }));
    const fresh = preview ? data.total - data.existing : data.created;
    document.getElementById('bt-summary').textContent =
      `${data.total} bins - ${fresh} ${preview ? 'new' : 'created'}, ${data.existing} already there`;
    const shown = (data.rows || []).slice(0, 200);
    document.getElementById('bt-rows').innerHTML = `<div class="s-987eb2"><table class="data-table"><thead><tr>
      <th>Bin</th><th>Aisle</th><th>Rack</th><th>Shelf</th><th>Position</th><th></th></tr></thead><tbody>${shown.map(r =>
      `<tr><td>${escapeHTMLText(r.code)}</td><td>${escapeHTMLText(r.aisle)}</td><td>${escapeHTMLText(r.rack)}</td>
       <td>${escapeHTMLText(r.level)}</td><td>${escapeHTMLText(r.position)}</td><td>${r.exists ? 'Exists' : ''}</td></tr>`).join('')}
      </tbody></table></div>${data.rows.length > shown.length ? `<p class="s-d59bb5">Showing ${shown.length} of ${data.rows.length}.</p>` : ''}`;
  }

  document.getElementById('bt-preview').addEventListener('click', () => run(true));
  document.getElementById('bt-create').addEventListener('click', () => run(false));
  document.getElementById('bt-print').addEventListener('click', async () => {
    if (!val('bt-aisles') && !val('bt-racks') && !val('bt-shelves') && !val('bt-bins')) {
      // No ranges: print the bins already set up at the location (and zone).
      const res = await apiFetch('/api/v1/doc/Bin?limit=2000');
      if (!res || !res.ok) return;
      const loc = val('bt-location'), zone = val('bt-zone');
      lastBins = (await res.json() || []).filter(b => b.bin_code && (!loc || b.location === loc) && (!zone || b.zone === zone))
        .sort((a, b) => String(a.bin_code).localeCompare(String(b.bin_code)));
    } else if (!lastBins.length) await run(true);
    await printBinLabels(lastBins);
  });
}

const BIN_TEMPLATE_KEY = 'erp.binLabelTemplate';
const engine = () => import('./sticker-engine.js' + new URL(import.meta.url).search);

// printBinLabels asks for the template and copies, then prints one label per
// bin (Bin records or bin codes) - preview first, like every sticker print.
async function printBinLabels(bins) {
  const list = (bins || []).map(b => (typeof b === 'string' ? { bin_code: b } : b)).filter(b => b && b.bin_code);
  if (!list.length) { showToast('No bins to print.', { variant: 'info' }); return; }
  const E = await engine();
  const res = await apiFetch('/api/v1/doc/StickerTemplate');
  const docs = res && res.ok ? await res.json() : [];
  const choices = [
    ...E.READY_DESIGNS.filter(d => d.use === 'bin').map(d => ({ id: `design:${d.id}`, name: `${d.label} (built-in)`, make: () => E.designTemplate(d) })),
    ...docs.filter(t => (t.status || 'Active') === 'Active')
      .map(t => ({ id: t.id, name: t.name || t.code || t.id, bin: E.isBinDesign(t), make: () => E.templateFromDoc(t) }))
  ];
  let last = '';
  try { last = localStorage.getItem(BIN_TEMPLATE_KEY) || ''; } catch (e) { /* no storage: the default is fine */ }
  const picked = choices.find(c => c.id === last) || choices.find(c => c.bin) || choices[0];
  const n = list.length;

  document.getElementById('bin-label-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'bin-label-dialog';
  overlay.innerHTML = `
    <div class="modal-container s-41d843" role="dialog" aria-modal="true" aria-labelledby="bin-label-title">
      <div class="modal-header"><h3 class="modal-title" id="bin-label-title">Bin labels · ${n}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-bl-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-a4aea4">
        <div class="form-group"><label class="form-label" for="bl-template">Template</label>
          <select id="bl-template" class="form-input">${choices.map(c => `<option value="${escapeHTMLText(c.id)}" ${c === picked ? 'selected' : ''}>${escapeHTMLText(c.name)}</option>`).join('')}</select></div>
        <div class="form-group"><label class="form-label" for="bl-copies">Copies of each</label>
          <input type="number" id="bl-copies" class="form-input" min="1" max="20" step="1" value="1"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-bl-close>Cancel</button>
        <button type="button" class="btn btn-outline" data-bl-sheet>A4 sheet</button>
        <button type="button" class="btn btn-primary" data-bl-print>Print</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-bl-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  overlay.querySelector('[data-bl-sheet]').addEventListener('click', () => { close(); printBinSheet(list.map(b => b.bin_code), E); });
  overlay.querySelector('[data-bl-print]').addEventListener('click', async () => {
    const c = choices.find(x => x.id === overlay.querySelector('#bl-template').value) || picked;
    const copies = Math.min(20, Math.max(1, Math.round(Number(overlay.querySelector('#bl-copies').value) || 1)));
    try { localStorage.setItem(BIN_TEMPLATE_KEY, c.id); } catch (e) { /* remembered next time only when storage works */ }
    close();
    const r = await window.loadViewModule('./print-routing.js');
    const printer = await r.chosenPrinter('Sticker', (window.erpWorkingStore || {}).code || '');
    await E.printLabelsWithTemplate(c.make(), list.map(E.binLabel), printer, { title: `Bin labels · ${n}`, jobRef: 'Bin labels', copies });
  });
  overlay.querySelector('#bl-template').focus();
}

// printBinSheet opens a print window with one Code 128 label per bin code,
// laid out for an A4 office printer.
function printBinSheet(codes, { encodeCode128 }) {
  const labels = [];
  for (const code of codes) {
    const m = encodeCode128(code);
    if (!m) { labels.push(`<div class="lb"><b>${escapeHTMLText(code)}</b><small>cannot be barcoded</small></div>`); continue; }
    const q = 10, n = m.length;
    let path = '';
    for (let i = 0; i < n; i++) { if (m[i]) { let j = i; while (j < n && m[j]) j++; path += `M${i + q},0h${j - i}v40h-${j - i}z`; i = j - 1; } }
    labels.push(`<div class="lb"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n + 2 * q} 40" preserveAspectRatio="none" width="100%" height="38" shape-rendering="crispEdges"><path d="${path}"/></svg><b>${escapeHTMLText(code)}</b></div>`);
  }
  const w = window.open('', '_blank');
  if (!w) return;
  w.document.write(`<!doctype html><title>Bin labels</title><style>
    body{margin:8px;font-family:sans-serif}.g{display:flex;flex-wrap:wrap;gap:6px}
    .lb{width:60mm;border:1px solid #999;padding:3mm;text-align:center;break-inside:avoid;box-sizing:border-box}
    .lb b{display:block;font-size:14px;margin-top:2px}.lb small{display:block;color:#a00}
    @media print{.lb{border:1px dashed #bbb}}</style><div class="g">${labels.join('')}</div>`);
  w.document.close();
  w.focus();
  setTimeout(() => w.print(), 300);
}

window.printBinLabels = printBinLabels;
export { renderBinToolsView, printBinLabels };
