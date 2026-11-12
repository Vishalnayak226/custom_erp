// Stage 146.3.4 - Bin Generator and Code 128 bin labels.
// Lazy view: POST /api/v1/wms/bin-generator (preview / create), and the labels
// are drawn with the sticker engine's Code 128 encoder into a print window.

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
  let lastCodes = [];
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
    lastCodes = (data.rows || []).map(r => r.code);
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
      lastCodes = (await res.json() || []).filter(b => (!loc || b.location === loc) && (!zone || b.zone === zone))
        .map(b => b.bin_code).filter(Boolean).sort();
    } else if (!lastCodes.length) await run(true);
    await printBinLabels(lastCodes);
  });
}

// printBinLabels opens a print window with one Code 128 label per bin code.
async function printBinLabels(codes) {
  if (!codes || !codes.length) return;
  const { encodeCode128 } = await import('./sticker-engine.js' + new URL(import.meta.url).search);
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
