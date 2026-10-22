// Stage 100.2.1 (D2, approved 2026-10-21): the dashboards' charts - number,
// line, bar, stacked bar, donut, table and pivot - drawn as SVG by this small
// helper, no chart library (first principle). Loaded only by the dashboards.
// Colours come from the --chart-N tokens (dashboards.css, light and dark),
// resolved to plain colours so a PDF copy of the page keeps them. Every mark
// carries its value as a hover title; every chart has the table behind it
// for screen readers; a mark or row is clickable when onPick is given.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));

// The element whose tokens the chart being drawn reads (a PDF copy is drawn
// inside a light-themed box).
let tokenHost = document.documentElement;

export function chartColor(i) {
  const css = getComputedStyle(tokenHost);
  const v = css.getPropertyValue(`--chart-${(i % 8) + 1}`).trim();
  return v || ['#4f6bed', '#16a34a', '#f59e0b', '#dc2626', '#0891b2', '#9333ea', '#db2777', '#65a30d'][i % 8];
}

function textColor(muted) {
  const v = getComputedStyle(tokenHost).getPropertyValue(muted ? '--text-muted' : '--text-main').trim();
  return v || (muted ? '#6b7280' : '#111827');
}

// fmt shows a value: money as ₹ with Indian grouping, numbers rounded,
// hidden money as "hidden".
export function fmtValue(v, col) {
  if (v === null || v === undefined) return col && col.money ? 'hidden' : '–';
  if (typeof v !== 'number') return String(v);
  const n = Math.abs(v) >= 100 ? Math.round(v) : Math.round(v * 100) / 100;
  const s = n.toLocaleString('en-IN');
  return col && col.money ? `₹${s}` : s;
}

function shortValue(v, col) {
  if (typeof v !== 'number') return fmtValue(v, col);
  const a = Math.abs(v);
  const s = a >= 1e7 ? `${(v / 1e7).toFixed(1)}Cr` : a >= 1e5 ? `${(v / 1e5).toFixed(1)}L` : a >= 1e3 ? `${(v / 1e3).toFixed(1)}K` : fmtValue(v);
  return col && col.money ? `₹${s}` : s;
}

function split(result) {
  const groups = result.columns.filter(c => c.type !== 'number');
  const measures = result.columns.filter(c => c.type === 'number');
  return { groups, measures };
}

// srTable is the data behind a chart, for screen readers.
function srTable(result, caption) {
  return `<table class="sr-only"><caption>${esc(caption || '')}</caption><thead><tr>${result.columns.map(c => `<th>${esc(c.label)}</th>`).join('')}</tr></thead>
    <tbody>${result.rows.map(r => `<tr>${result.columns.map(c => `<td>${esc(fmtValue(r[c.key], c))}</td>`).join('')}</tr>`).join('')}</tbody></table>`;
}

function wirePicks(host, rows, onPick) {
  if (!onPick) return;
  host.querySelectorAll('[data-pick]').forEach(el => {
    el.style.cursor = 'pointer';
    el.addEventListener('click', () => onPick(rows[Number(el.dataset.pick)], el.dataset.series || ''));
  });
}

// renderChart draws result (a dataset answer) as kind into host.
export function renderChart(host, kind, result, { title = '', onPick = null } = {}) {
  tokenHost = host;
  if (!result || !result.columns) { host.innerHTML = '<p class="dash-empty">No data.</p>'; return; }
  const { groups, measures } = split(result);
  if (!result.rows.length) { host.innerHTML = '<p class="dash-empty">Nothing in this period.</p>'; return; }
  if (kind === 'number' || !groups.length) return drawNumber(host, result, measures);
  if (kind === 'table') return drawTable(host, result, onPick);
  if (kind === 'pivot' && groups.length > 1) return drawPivot(host, result, groups, measures[0], onPick);
  if (kind === 'line') return drawLine(host, result, groups[0], measures, title, onPick);
  if (kind === 'donut') return drawDonut(host, result, groups[0], measures[0], title, onPick);
  if (kind === 'stacked') return drawBars(host, result, groups, measures, title, onPick, true);
  return drawBars(host, result, groups, measures, title, onPick, false);
}

function drawNumber(host, result, measures) {
  const m = measures[0];
  const row = result.rows[0] || {};
  const v = row[m.key];
  let change = '';
  const prev = result.previous && result.previous[m.key];
  if (typeof v === 'number' && typeof prev === 'number' && prev !== 0) {
    const pct = Math.round((v - prev) / Math.abs(prev) * 1000) / 10;
    change = `<div class="dash-change ${pct >= 0 ? 'up' : 'down'}" title="Before: ${esc(fmtValue(prev, m))}">${pct >= 0 ? '▲' : '▼'} ${Math.abs(pct)}% on the period before</div>`;
  }
  const rest = measures.slice(1).map(c => `<div class="dash-sub">${esc(c.label)}: <b>${esc(fmtValue(row[c.key], c))}</b></div>`).join('');
  host.innerHTML = `<div class="dash-number"><div class="dash-big" title="${esc(fmtValue(v, m))}">${esc(shortValue(v, m))}</div>
    <div class="dash-sub">${esc(m.label)}</div>${change}${rest}</div>`;
}

function drawTable(host, result, onPick) {
  host.innerHTML = `<div class="dash-table"><table><thead><tr>${result.columns.map(c => `<th class="${c.type === 'number' ? 'num' : ''}">${esc(c.label)}</th>`).join('')}</tr></thead>
    <tbody>${result.rows.map((r, i) => `<tr data-pick="${i}">${result.columns.map(c => `<td class="${c.type === 'number' ? 'num' : ''}">${esc(fmtValue(r[c.key], c))}</td>`).join('')}</tr>`).join('')}</tbody></table>
    ${result.truncated ? '<p class="dash-note">Only the first rows are shown.</p>' : ''}</div>`;
  wirePicks(host, result.rows, onPick);
}

function drawPivot(host, result, groups, m, onPick) {
  const [rk, ck] = [groups[0].key, groups[1].key];
  const rowsKeys = [...new Set(result.rows.map(r => r[rk]))];
  const colKeys = [...new Set(result.rows.map(r => r[ck]))].slice(0, 12);
  const cell = {};
  result.rows.forEach((r, i) => { cell[`${r[rk]}\u0000${r[ck]}`] = { v: r[m.key], i }; });
  const total = (vals) => vals.reduce((a, b) => a + (typeof b === 'number' ? b : 0), 0);
  host.innerHTML = `<div class="dash-table"><table><thead><tr><th>${esc(groups[0].label)} \\ ${esc(groups[1].label)}</th>${colKeys.map(c => `<th class="num">${esc(c)}</th>`).join('')}<th class="num">Total</th></tr></thead>
    <tbody>${rowsKeys.map(rv => {
      const vals = colKeys.map(cv => cell[`${rv}\u0000${cv}`]);
      return `<tr><th>${esc(rv)}</th>${vals.map(x => `<td class="num"${x ? ` data-pick="${x.i}"` : ''}>${x ? esc(fmtValue(x.v, m)) : ''}</td>`).join('')}<td class="num"><b>${esc(fmtValue(total(vals.map(x => x && x.v)), m))}</b></td></tr>`;
    }).join('')}</tbody></table></div>`;
  wirePicks(host, result.rows, onPick);
}

// Charts draw at the tile's own pixel size, so their text stays readable in
// a narrow tile.
const widthOf = (host) => Math.max(260, Math.round(host.clientWidth || 600));

function drawBars(host, result, groups, measures, title, onPick, stacked) {
  const W = widthOf(host), rowH = 26, labelW = Math.min(170, Math.round(W * 0.34)), valueW = 64;
  let cats, series, val;
  if (groups.length > 1) {
    // two groups: categories x series of the first measure
    const m = measures[0];
    cats = [...new Set(result.rows.map(r => r[groups[0].key]))].slice(0, 15);
    series = [...new Set(result.rows.map(r => r[groups[1].key]))].slice(0, 8).map(s => ({ key: s, label: String(s), col: m }));
    const idx = {};
    result.rows.forEach((r, i) => { idx[`${r[groups[0].key]}\u0000${r[groups[1].key]}`] = i; });
    val = (c, s) => { const i = idx[`${c}\u0000${s.key}`]; return i === undefined ? { v: 0, i: -1 } : { v: result.rows[i][m.key] || 0, i }; };
  } else {
    cats = result.rows.slice(0, 15).map(r => r[groups[0].key]);
    series = measures.slice(0, 3).map(c => ({ key: c.key, label: c.label, col: c }));
    val = (c, s) => { const i = cats.indexOf(c); return { v: result.rows[i][s.key] || 0, i }; };
  }
  const totals = cats.map(c => series.reduce((a, s) => a + Math.max(0, val(c, s).v), 0));
  const max = Math.max(1, ...(stacked ? totals : cats.flatMap(c => series.map(s => val(c, s).v))));
  // Side by side, money and counts each keep their own scale.
  const mixed = !stacked && new Set(series.map(s => !!(s.col && s.col.money))).size > 1;
  const seriesMax = s => Math.max(1, ...cats.map(c => val(c, s).v));
  const barW = W - labelW - valueW;
  const per = stacked ? 1 : series.length;
  const h = cats.length * rowH * (per > 1 ? 1.25 : 1) + 6;
  const lane = (per > 1 ? rowH * 1.25 : rowH);
  let svg = '';
  cats.forEach((c, ci) => {
    const y0 = ci * lane + 4;
    svg += `<text x="${labelW - 8}" y="${y0 + lane / 2 + 4}" text-anchor="end" font-size="12" fill="${textColor(false)}">${esc(String(c).slice(0, Math.max(8, Math.floor(labelW / 7))))}</text>`;
    let x = labelW;
    series.forEach((s, si) => {
      const { v, i } = val(c, s);
      const w = Math.max(0, v) / (mixed ? seriesMax(s) : max) * barW;
      const bh = stacked ? lane - 8 : (lane - 6) / per;
      const y = stacked ? y0 : y0 + si * bh;
      svg += `<rect x="${stacked ? x : labelW}" y="${y}" width="${Math.max(w, v > 0 ? 1 : 0)}" height="${Math.max(bh - 2, 3)}" rx="2" fill="${chartColor(si)}"${i >= 0 ? ` data-pick="${i}"` : ''} data-series="${esc(s.key)}"><title>${esc(c)} · ${esc(s.label)}: ${esc(fmtValue(v, s.col))}</title></rect>`;
      if (stacked) x += w;
    });
    const tv = stacked ? totals[ci] : val(c, series[0]).v;
    svg += `<text x="${W - 4}" y="${y0 + lane / 2 + 4}" text-anchor="end" font-size="12" fill="${textColor(true)}">${esc(shortValue(tv, series[0].col))}</text>`;
  });
  const legend = series.length > 1 ? `<div class="dash-legend">${series.map((s, i) => `<span><i style="background:${chartColor(i)}"></i>${esc(s.label)}</span>`).join('')}</div>` : '';
  host.innerHTML = `<svg viewBox="0 0 ${W} ${h}" width="${W}" height="${h}" role="img" aria-label="${esc(title)}">${svg}</svg>${legend}${srTable(result, title)}`;
  wirePicks(host, result.rows, onPick);
}

function drawLine(host, result, g, measures, title, onPick) {
  const W = widthOf(host), H = Math.max(140, Math.round((host.clientHeight || 220) - (measures.length > 1 ? 26 : 6))), padL = 56, padB = 26, padT = 10, padR = 12;
  const rows = result.rows;
  const series = measures.slice(0, 3);
  const vals = rows.flatMap(r => series.map(s => r[s.key]).filter(v => typeof v === 'number'));
  const max = Math.max(1, ...vals), min = Math.min(0, ...vals);
  const x = i => padL + (rows.length <= 1 ? (W - padL - padR) / 2 : i * (W - padL - padR) / (rows.length - 1));
  const y = v => padT + (H - padT - padB) * (1 - (v - min) / (max - min || 1));
  let svg = '';
  [0, 0.5, 1].forEach(t => {
    const v = min + (max - min) * t;
    svg += `<line x1="${padL}" x2="${W - padR}" y1="${y(v)}" y2="${y(v)}" stroke="${textColor(true)}" stroke-opacity="0.2"/>` +
      `<text x="${padL - 6}" y="${y(v) + 4}" text-anchor="end" font-size="11" fill="${textColor(true)}">${esc(shortValue(v, series[0]))}</text>`;
  });
  const step = Math.max(1, Math.ceil(rows.length / 8));
  rows.forEach((r, i) => {
    if (i % step === 0 || i === rows.length - 1) svg += `<text x="${x(i)}" y="${H - 8}" text-anchor="middle" font-size="11" fill="${textColor(true)}">${esc(String(r[g.key]).slice(5) || r[g.key])}</text>`;
  });
  series.forEach((s, si) => {
    const pts = rows.map((r, i) => `${x(i)},${y(typeof r[s.key] === 'number' ? r[s.key] : 0)}`).join(' ');
    svg += `<polyline points="${pts}" fill="none" stroke="${chartColor(si)}" stroke-width="2"/>`;
    rows.forEach((r, i) => {
      svg += `<circle cx="${x(i)}" cy="${y(typeof r[s.key] === 'number' ? r[s.key] : 0)}" r="3.5" fill="${chartColor(si)}" data-pick="${i}"><title>${esc(r[g.key])} · ${esc(s.label)}: ${esc(fmtValue(r[s.key], s))}</title></circle>`;
    });
  });
  const legend = series.length > 1 ? `<div class="dash-legend">${series.map((s, i) => `<span><i style="background:${chartColor(i)}"></i>${esc(s.label)}</span>`).join('')}</div>` : '';
  host.innerHTML = `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="${esc(title)}">${svg}</svg>${legend}${srTable(result, title)}`;
  wirePicks(host, rows, onPick);
}

function drawDonut(host, result, g, m, title, onPick) {
  const rows = result.rows.filter(r => typeof r[m.key] === 'number' && r[m.key] > 0);
  if (!rows.length) { host.innerHTML = `<p class="dash-empty">${result.rows.some(r => r[m.key] === null) ? 'Hidden for your role.' : 'Nothing to show.'}</p>`; return; }
  const top = rows.slice(0, 7);
  const other = rows.slice(7).reduce((a, r) => a + r[m.key], 0);
  const slices = top.map(r => ({ label: r[g.key], v: r[m.key], i: result.rows.indexOf(r) }));
  if (other > 0) slices.push({ label: 'Other', v: other, i: -1 });
  const total = slices.reduce((a, s) => a + s.v, 0);
  const R = 80, r0 = 50, cx = 100, cy = 100;
  let a0 = -Math.PI / 2, svg = '';
  slices.forEach((s, k) => {
    const a1 = a0 + s.v / total * Math.PI * 2;
    const large = a1 - a0 > Math.PI ? 1 : 0;
    const p = (a, rad) => `${cx + rad * Math.cos(a)},${cy + rad * Math.sin(a)}`;
    const d = slices.length === 1
      ? `M ${cx} ${cy - R} A ${R} ${R} 0 1 1 ${cx - 0.01} ${cy - R} L ${cx - 0.01} ${cy - r0} A ${r0} ${r0} 0 1 0 ${cx} ${cy - r0} Z`
      : `M ${p(a0, R)} A ${R} ${R} 0 ${large} 1 ${p(a1, R)} L ${p(a1, r0)} A ${r0} ${r0} 0 ${large} 0 ${p(a0, r0)} Z`;
    svg += `<path d="${d}" fill="${chartColor(k)}"${s.i >= 0 ? ` data-pick="${s.i}"` : ''}><title>${esc(s.label)}: ${esc(fmtValue(s.v, m))} (${Math.round(s.v / total * 100)}%)</title></path>`;
    a0 = a1;
  });
  svg += `<text x="${cx}" y="${cy + 5}" text-anchor="middle" font-size="15" font-weight="600" pointer-events="none" fill="${textColor(false)}">${esc(shortValue(total, m))}</text>`;
  host.innerHTML = `<div class="dash-donut"><svg viewBox="0 0 200 200" role="img" aria-label="${esc(title)}">${svg}</svg>
    <ul class="dash-legend dash-legend-list">${slices.map((s, k) => `<li><i style="background:${chartColor(k)}"></i>${esc(s.label)} <b>${Math.round(s.v / total * 100)}%</b></li>`).join('')}</ul></div>${srTable(result, title)}`;
  wirePicks(host, result.rows, onPick);
}
