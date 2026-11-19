// Ask 207: Data Healer. A heal is a one-time fix a client runs after a release
// (engines/data_healer.go). While one waits: a strip above every screen that
// cannot be closed (Fix now for whoever can run it, "Ask your admin" for the
// rest), a panel on the list of each record type it covers, and those
// records' form refuses to save until it has run. Settings > Data Healer
// lists every heal; a waiting one opens to its records with the value each
// gets, any of which can be typed over before Run heal. Done is final.
// Item 201.4: a Super Admin also gets Past names - change an item's name on
// documents already made (engines/data_healer_rename.go): preview, then YES.

const esc = (s) => escapeHTMLText(String(s ?? ''));
// 234.5: app.js's cached en-IN formatter. toLocaleString built a second ICU
// formatter at boot (177 ms on the phone profile) and grouped by browser locale.
const num = (n) => formatNumber(n || 0);
let cache = null;
let openHealID = '';
const PAST = '#past-names';

adoptCss('data-healer-css', `
.heal-strip { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 16px; padding: 8px 16px; margin: 0 0 8px;
  background: var(--warning-bg, #fff4e5); color: var(--warning-text, #7a4a00); border-bottom: 1px solid var(--warning-border, #f0c36d); font-size: 13px; }
.heal-strip .btn { padding: 4px 12px; }
.heal-panel { display: flex; flex-direction: row; flex-wrap: wrap; align-items: center; justify-content: flex-start; gap: 8px 16px; padding: 12px 16px; margin-bottom: 12px; }
.heal-alias-input { width: 100%; min-width: 9.5em; max-width: 220px; }
.heal-empty { padding: 16px; margin: 0; }
.heal-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 8px 12px; padding: 12px 16px; }
.heal-form .form-group { margin: 0; flex: 1 1 12em; max-width: 260px; }
.heal-form .heal-date { flex: 0 1 10em; }
.heal-sum { padding: 8px 16px; margin: 0; }
`);

// inflight: boot mounts the strip while the first view renders and mounts it
// again (162.5), so the two share one request instead of sending two.
let inflight = null;
async function loadHeals(force) {
  if (!force && cache && Date.now() - cache.at < 60000) return cache;
  if (!force && inflight) return inflight;
  const p = (async () => {
    const res = await apiFetch('/api/v1/heals');
    if (!res || !res.ok) return cache || { heals: [], can_run: false, can_rename: false, at: 0 };
    const j = await res.json();
    cache = { heals: j.heals || [], can_run: !!j.can_run, can_rename: !!j.can_rename, at: Date.now() };
    return cache;
  })();
  inflight = p;
  try { return await p; } finally { if (inflight === p) inflight = null; }
}

const waiting = (c) => (c.heals || []).filter(h => h.status === 'Waiting');

function openHealer(id) {
  openHealID = id || '';
  if (typeof setActiveMenu === 'function') setActiveMenu('menu-datahealer');
  renderView('datahealer');
}

// runHeal posts one run; typed maps "Doctype/id" to a typed value. True when
// the heal is done, now or already.
async function runHeal(h, typed) {
  const res = await apiFetch('/api/v1/heals/' + encodeURIComponent(h.id) + '/run', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ typed: typed || {} }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'The heal did not run.'); return false; }
  const r = await res.json();
  showToast(r.already_done ? `${h.title}: already healed.` : `${h.title}: ${num(r.fixed)} healed.`);
  await loadHeals(true);
  renderStrip(cache);
  return true;
}

function renderStrip(c) {
  const root = document.getElementById('view-root');
  let strip = document.getElementById('heal-strip');
  const w = waiting(c);
  if (!w.length || !root) { strip?.remove(); return; }
  if (!strip) {
    strip = document.createElement('div');
    strip.id = 'heal-strip';
    strip.className = 'heal-strip';
    strip.setAttribute('role', 'status');
    root.parentNode.insertBefore(strip, root);
    strip.addEventListener('click', (e) => { if (e.target.closest('[data-heal-open]')) openHealer(w.length === 1 ? w[0].id : ''); });
  }
  const html = w.map(h => `<span><strong>${esc(h.title)}</strong> · ${num(h.count)} to heal</span>`).join('') +
    (c.can_run ? '<button type="button" class="btn btn-primary" data-heal-open>Fix now</button>' : '<span>Ask your admin</span>');
  if (strip._html !== html) { strip.innerHTML = html; strip._html = html; } // 234.1: unchanged, left alone
}

function renderPanel(c, view, doctype) {
  document.getElementById('heal-panel')?.remove();
  const root = document.getElementById('view-root');
  if (view !== 'doctype-table' || !root) return;
  const h = waiting(c).find(x => (x.doctypes || []).includes(doctype));
  if (!h) return;
  const panel = document.createElement('div');
  panel.id = 'heal-panel';
  panel.className = 'table-panel heal-panel';
  panel.innerHTML = `<span><strong>${esc(h.title)}</strong> · ${num(h.count)} to heal · saving waits</span>` + (c.can_run
    ? '<button type="button" class="btn btn-primary" data-heal-run>Run heal</button><button type="button" class="btn btn-secondary" data-heal-review>Review</button>'
    : '<span>Ask your admin</span>');
  panel.addEventListener('click', async (e) => {
    if (e.target.closest('[data-heal-review]')) { openHealer(h.id); return; }
    const b = e.target.closest('[data-heal-run]');
    if (!b) return;
    if (!await showCustomConfirm(`Give ${num(h.count)} records their proposed ${h.field_label}? It runs once.`, 'Run heal')) return;
    b.disabled = true;
    if (await runHeal(h)) renderView(view); else b.disabled = false;
  });
  root.insertBefore(panel, root.firstChild);
}

// mount runs after every screen renders: the strip everywhere, the panel on
// a covered record type's list.
// 234.1: drawn from the cache in the screen's own pass. A cache past its
// minute is refreshed behind it and redrawn only if a waiting heal changed,
// so the panel no longer lands a moment after the screen.
const healsKey = (c) => JSON.stringify([waiting(c).map(h => [h.id, h.count, h.doctypes]), c.can_run]);
async function mount(view, doctype) {
  const draw = (c) => { renderStrip(c); renderPanel(c, view, doctype); };
  const shown = cache;
  if (shown) draw(shown);
  if (shown && Date.now() - shown.at < 60000) return;
  const c = await loadHeals(false);
  const still = typeof currentView === 'undefined' || (currentView === view && (view !== 'doctype-table' || currentDoctype === doctype));
  if (still && (!shown || healsKey(c) !== healsKey(shown))) draw(c);
}

// allowSave is asked before a form saves. A record type a waiting heal covers
// does not save until it has run: whoever can run it is offered Run heal
// (then the save goes on, 'ran'); anyone else is told to ask their admin.
async function allowSave(doctype) {
  const c = await loadHeals(true);
  const h = waiting(c).find(x => (x.doctypes || []).includes(doctype));
  if (!h) return true;
  if (!c.can_run) {
    await showCustomAlert(`Saving waits for the ${h.title} heal. Ask your admin to run it in Settings › Data Healer.`, 'Heal first');
    return false;
  }
  if (!await showCustomConfirm(`Saving waits for the ${h.title} heal (${num(h.count)} records). Run it now?`, 'Heal first')) return false;
  return (await runHeal(h)) ? 'ran' : false;
}

window.dataHealer = { mount, allowSave };

export async function renderDataHealerView(container) {
  const c = await loadHeals(true);
  renderStrip(c);
  const h = openHealID && c.heals.find(x => x.id === openHealID && x.status === 'Waiting');
  const past = openHealID === PAST && c.can_rename;
  openHealID = '';
  if (past) { renderPastNames(container); return; }
  if (h) { await renderHealDetail(container, h, c); return; }
  const list = document.createElement('div');
  list.innerHTML = `<div class="page-section-bar"><h1 class="page-title">Data Healer</h1>${c.can_rename
    ? '<span class="page-section-actions"><button type="button" class="btn btn-secondary" data-heal-past>Past names</button></span>' : ''}</div>
    <div class="table-panel">` + (c.heals.length === 0 ? '<p class="text-muted heal-empty">Nothing to heal.</p>' : `
    <div class="table-wrapper"><table><thead><tr><th>Heal</th><th>Item</th><th>Status</th><th>Records</th><th>Done</th><th></th></tr></thead>
      <tbody>${c.heals.map((x, i) => `<tr>
        <td>${esc(x.title)}</td><td>${esc(x.item)}</td><td>${esc(x.status)}</td>
        <td>${x.status === 'Waiting' ? num(x.count) + ' to heal' : num(x.fixed) + ' healed' + (x.typed ? ' · ' + num(x.typed) + ' typed' : '')}</td>
        <td>${x.done_at ? esc(String(x.done_at).slice(0, 10)) + ' · ' + esc(x.done_by) : ''}</td>
        <td>${x.status === 'Waiting' ? `<button type="button" class="action-btn" data-heal-i="${i}">${c.can_run ? 'Open' : 'View'}</button>` : ''}</td></tr>`).join('')}</tbody></table></div>`) + '</div>';
  list.addEventListener('click', (e) => {
    if (e.target.closest('[data-heal-past]')) { openHealer(PAST); return; }
    const b = e.target.closest('[data-heal-i]');
    if (b) openHealer(c.heals[Number(b.dataset.healI)].id);
  });
  container.appendChild(list);
}

async function renderHealDetail(container, h, c) {
  const res = await apiFetch('/api/v1/heals/' + encodeURIComponent(h.id) + '/records');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The heal could not be loaded.'); return; }
  const j = await res.json();
  const recs = j.records || [];
  const st = j.heal || h;
  const max = st.max_len ? ` maxlength="${Number(st.max_len)}"` : '';
  const panel = document.createElement('div');
  panel.innerHTML = `<div class="page-section-bar"><h1 class="page-title">${esc(st.title)}</h1>
      <span class="text-muted">${num(st.count)} to heal${recs.length < st.count ? ` · first ${num(recs.length)} shown` : ''}</span>
      <span class="page-section-actions"><button type="button" class="btn btn-secondary" data-heal-back>Back</button>
      ${c.can_run ? '<button type="button" class="btn btn-primary" data-heal-go>Run heal</button>' : ''}</span></div>
    <div class="table-panel"><div class="table-wrapper"><table><thead><tr><th>Type</th><th>Record</th><th>${esc(st.field_label)}</th></tr></thead>
      <tbody>${recs.map((r, i) => `<tr><td>${esc(r.doctype === 'ItemName' ? 'Item name' : r.doctype)}</td><td>${esc(r.label)}</td>
        <td><input type="text" class="form-input heal-alias-input" data-heal-rec="${i}" value="${esc(r.proposed)}"${max}${c.can_run ? '' : ' disabled'} aria-label="${esc(st.field_label)}"></td></tr>`).join('')}</tbody></table></div></div>`;
  panel.addEventListener('click', async (e) => {
    if (e.target.closest('[data-heal-back]')) { openHealer(''); return; }
    const b = e.target.closest('[data-heal-go]');
    if (!b) return;
    const typed = {};
    panel.querySelectorAll('[data-heal-rec]').forEach(inp => {
      const r = recs[Number(inp.dataset.healRec)];
      const v = inp.value.trim();
      if (v && v !== r.proposed) typed[r.doctype + '/' + r.id] = v;
    });
    if (!await showCustomConfirm(`Give ${num(st.count)} records their ${st.field_label}? It runs once.`, 'Run heal')) return;
    b.disabled = true;
    if (await runHeal(st, typed)) openHealer(''); else b.disabled = false;
  });
  container.appendChild(panel);
}

async function pastPost(kind, body) {
  const res = await apiFetch('/api/v1/heals/past-names/' + kind, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, kind === 'apply' ? 'The names were not changed.' : 'The preview did not load.'); return null; }
  return res.json();
}

function pastPreviewHTML(pv) {
  const types = pv.types || [], docs = pv.docs || [];
  if (!types.length) return '<p class="text-muted heal-sum">Nothing to change.</p>';
  const label = Object.fromEntries(types.map(t => [t.doctype, t.label || t.doctype]));
  const total = pv.documents + pv.filed;
  return `<p class="heal-sum">${num(pv.documents)} documents · ${num(pv.lines)} lines · ${num(pv.prints)} prints${pv.filed ? ` · ${num(pv.filed)} filed, not changed` : ''}</p>
    <div class="table-wrapper"><table><thead><tr><th></th><th>Type</th><th>Documents</th><th>Lines</th><th>Prints</th><th>Filed</th></tr></thead>
      <tbody>${types.map(t => `<tr><td><input type="checkbox" data-pn-type value="${esc(t.doctype)}"${t.documents ? ' checked' : ' disabled'} aria-label="${esc(label[t.doctype])}"></td>
        <td>${esc(label[t.doctype])}</td><td>${num(t.documents)}</td><td>${num(t.lines)}</td><td>${num(t.prints)}</td><td>${t.filed ? num(t.filed) : ''}</td></tr>`).join('')}</tbody></table></div>
    ${pv.documents ? '<div class="heal-form"><button type="button" class="btn btn-primary" data-pn-apply>Apply</button></div>' : ''}
    <div class="table-wrapper"><table><thead><tr><th>Type</th><th>Document</th><th>Date</th><th>Lines</th><th>Prints</th><th></th></tr></thead>
      <tbody>${docs.map(d => `<tr><td>${esc(label[d.doctype] || d.doctype)}</td><td>${esc(d.id)}</td><td>${esc(d.date)}</td>
        <td>${num(d.lines)}</td><td>${num(d.prints)}</td><td>${d.filed ? 'Filed, not changed' : ''}</td></tr>`).join('')}</tbody></table></div>
    ${docs.length < total ? `<p class="text-muted heal-sum">First ${num(docs.length)} of ${num(total)} shown</p>` : ''}`;
}

// renderPastNames is the Past names page. Changing the form clears the
// preview, so Apply always sends what was previewed.
function renderPastNames(container) {
  const page = document.createElement('div');
  page.innerHTML = `<div class="page-section-bar"><h1 class="page-title">Past document names</h1>
      <span class="page-section-actions"><button type="button" class="btn btn-secondary" data-heal-back>Back</button></span></div>
    <div class="table-panel"><form class="heal-form" data-pn-form autocomplete="off">
      <div class="form-group"><label class="form-label" for="pn-old">Old name</label><input type="text" id="pn-old" class="form-input" maxlength="200" required></div>
      <div class="form-group"><label class="form-label" for="pn-new">New name</label><input type="text" id="pn-new" class="form-input" maxlength="200" required></div>
      <div class="form-group heal-date"><label class="form-label" for="pn-from">From</label><input type="date" id="pn-from" class="form-input"></div>
      <div class="form-group heal-date"><label class="form-label" for="pn-to">To</label><input type="date" id="pn-to" class="form-input"></div>
      <button type="submit" class="btn btn-primary">Preview</button></form>
    <div data-pn-out></div></div>`;
  const form = page.querySelector('[data-pn-form]');
  const out = page.querySelector('[data-pn-out]');
  const val = (id) => page.querySelector('#' + id).value.trim();
  let shown = null;
  form.addEventListener('input', () => { shown = null; out.innerHTML = ''; });
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const req = { old: val('pn-old'), new: val('pn-new'), from: val('pn-from'), to: val('pn-to') };
    const b = form.querySelector('[type=submit]');
    b.disabled = true;
    const pv = await pastPost('preview', req);
    b.disabled = false;
    if (!pv) return;
    shown = { req, pv };
    out.innerHTML = pastPreviewHTML(pv);
  });
  page.addEventListener('click', async (e) => {
    if (e.target.closest('[data-heal-back]')) { openHealer(''); return; }
    const b = e.target.closest('[data-pn-apply]');
    if (!b || !shown) return;
    const ticked = [...out.querySelectorAll('[data-pn-type]:checked')].map(x => x.value);
    if (!ticked.length) { showToast('Tick a record type.'); return; }
    const { req, pv } = shown;
    const n = pv.types.filter(t => ticked.includes(t.doctype)).reduce((s, t) => s + t.documents, 0);
    if (!await showCustomConfirm(`"${req.old}" becomes "${req.new}" on ${num(n)} documents. Type YES.`, 'Change past names', { typed: 'YES' })) return;
    b.disabled = true;
    const r = await pastPost('apply', { ...req, doctypes: ticked, confirm: 'YES' });
    b.disabled = false;
    if (!r) return;
    showToast(`${num(r.documents)} documents changed${r.filed ? ` · ${num(r.filed)} filed, not changed` : ''}`);
    form.requestSubmit();
  });
  container.appendChild(page);
}
