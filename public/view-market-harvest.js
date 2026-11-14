// 34.4 - Myntra market prices: start a run, read the log, re-parse, see the prices.
// Lazy: loaded by view-pim.js on the PIM Reports page.
await loadViewStyles('view-market-harvest.css');

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const API = '/api/v1/pim/market-harvest';

function logLine(run) {
  const r = run.result || {};
  if (run.status === 'failed') return esc(run.last_error || 'Failed');
  if (run.status !== 'completed') return esc(run.status);
  return `${esc(r.fetched || 0)} fetched · ${esc(r.cached || 0)} cached · ${esc(r.refused_by_robots || 0)} refused · ${esc(r.stop_reason || '')}`;
}

export async function renderMarketHarvest(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel mh-panel';
  container.appendChild(panel);

  const draw = async () => {
    const res = await apiFetch(API);
    if (!res || !res.ok) { panel.innerHTML = '<h3>Market prices · Myntra</h3><p class="text-muted">Unable to load.</p>'; return; }
    const st = await res.json();
    const priceRes = await apiFetch('/api/v1/doc/CompetitorPrice?q=Myntra&q_fields=platform&limit=100');
    const prices = (priceRes && priceRes.ok ? await priceRes.json() : []).filter(p => p.platform === 'Myntra');
    prices.sort((a, b) => String(b.observed_at || '').localeCompare(String(a.observed_at || '')));
    const runs = st.runs || [];
    const latest = runs.find(r => r.result && r.result.date);
    const form = !st.enabled
      ? '<p class="text-muted">Off. An admin turns it on in Settings.</p>'
      : st.can_run
        ? `<div class="mh-bar">
            <label>Keywords<input class="form-input" id="mh-keywords" placeholder="kurta, saree"></label>
            <label>Category<input class="form-input" id="mh-category" placeholder="optional"></label>
            <label>Max pages<input class="form-input mh-pages" id="mh-pages" type="number" min="1" max="50" value="${esc(st.max_pages)}"></label>
            <button type="button" class="btn btn-primary" id="mh-run">Start run</button>
            <button type="button" class="btn btn-outline" id="mh-reparse"${latest ? '' : ' disabled'}>Re-parse</button>
          </div><div class="mh-msg text-muted" id="mh-msg" role="status"></div>`
        : '<p class="text-muted">Only an admin can start a run.</p>';
    const runRows = runs.length
      ? `<div class="table-wrapper"><table><thead><tr><th>Started</th><th>By</th><th>Result</th></tr></thead><tbody>${runs.map(r =>
        `<tr><td>${esc(String(r.created_at || '').replace('T', ' ').slice(0, 16))}</td><td>${esc(r.actor)}</td><td>${logLine(r)}</td></tr>`).join('')}</tbody></table></div>`
      : '<p class="text-muted">No runs yet.</p>';
    const priceRows = prices.length
      ? `<div class="table-wrapper"><table><thead><tr><th>Product</th><th>Brand</th><th>Price</th><th>MRP</th><th>Off %</th><th>Rating</th><th>Keyword</th><th>Date</th></tr></thead><tbody>${prices.map(p =>
        `<tr><td>${/^https:\/\/www\.myntra\.com\//.test(p.source_url || '') ? `<a href="${esc(p.source_url)}" target="_blank" rel="noopener noreferrer">${esc(p.competitor_product)}</a>` : esc(p.competitor_product)}</td><td>${esc(p.brand)}</td><td>${esc(p.competitor_price)}</td><td>${esc(p.mrp)}</td><td>${esc(p.discount_pct)}</td><td>${esc(p.rating)}</td><td>${esc(p.keyword)}</td><td>${esc(p.observed_at)}</td></tr>`).join('')}</tbody></table></div>`
      : '<p class="text-muted">No prices yet.</p>';
    panel.innerHTML = `<h3>Market prices · Myntra</h3>${form}<h4 class="mh-sub">Runs</h4>${runRows}<h4 class="mh-sub">Prices</h4>${priceRows}`;
    const msg = panel.querySelector('#mh-msg');
    const post = async (path, body, done) => {
      msg.textContent = 'Working…';
      const r = await apiFetch(`${API}/${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      if (!r) return;
      if (!r.ok) { msg.textContent = ''; await showApiError(r, 'Could not do that.'); return; }
      msg.textContent = done;
      setTimeout(() => { if (document.body.contains(panel)) draw(); }, 1500);
    };
    const runBtn = panel.querySelector('#mh-run');
    if (runBtn) {
      runBtn.addEventListener('click', () => post('runs', {
        keywords: panel.querySelector('#mh-keywords').value.split(',').map(s => s.trim()).filter(Boolean),
        category: panel.querySelector('#mh-category').value.trim(),
        max_pages: Number(panel.querySelector('#mh-pages').value) || 0,
      }, 'Queued. It runs in the background.'));
      panel.querySelector('#mh-reparse').addEventListener('click', () => post('reparse', { date: latest.result.date }, 'Parsed again.'));
    }
  };
  await draw();
}
