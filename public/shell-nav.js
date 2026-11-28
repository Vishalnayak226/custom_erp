// Stage 47.14.5: sidebar maturity labels and the role's landing screen.
// /api/v1/me/modules sends `nav`; app.js calls applyShellNav once, before the
// first screen. Entries below Production carry a short "Preview" /
// "Experimental" tag, or are hidden for non-administrators when the
// navigation.hide_unfinished setting is on. Returns the landing screen only
// if the role can open it, else ''.

// 49.4.3: a constructed sheet (adoptCss in app.js), which a strict style-src allows.
adoptCss('shell-nav-css', `
.maturity-tag { margin-left: 6px; padding: 1px 6px; border-radius: 999px; font-size: 12px; font-weight: 600; background: var(--warning-soft-bg); color: var(--warning-strong); white-space: nowrap; }
.maturity-tag.maturity-experimental { background: var(--danger-soft-bg, var(--warning-soft-bg)); color: var(--danger-strong, var(--warning-strong)); }
li.maturity-hidden { display: none !important; }`);

function markMaturity(el, level, hide) {
  if (!el) return;
  const li = el.closest('li');
  if (hide) { if (li) li.classList.add('maturity-hidden'); return; }
  const tag = el.querySelector('.maturity-tag');
  if (tag) { tag.textContent = level; tag.className = 'maturity-tag maturity-' + level.toLowerCase(); return; }
  const span = document.createElement('span');
  span.className = 'maturity-tag maturity-' + level.toLowerCase();
  span.textContent = level;
  el.appendChild(span);
}

export function applyShellNav(nav, menuMap, wantLanding) {
  watchEmptyStates();
  watchBigTables();
  window.markMenuCodes?.(); // 208.1: codes beside the menu entries, once nav says whether they are on
  const hide = !!nav.hide;
  const byMenu = nav.menus || {};
  for (const [id, level] of Object.entries(byMenu)) markMaturity(document.getElementById(id), level, hide);
  for (const [id, info] of Object.entries(menuMap || {})) {
    const level = (nav.modules || {})[info.module];
    const el = document.getElementById(id);
    if (!level || byMenu[id] || !el || el.closest('.menu-flyout')) continue;
    markMaturity(el, level, hide);
  }
  if (!wantLanding || !nav.landing) return '';
  const el = document.getElementById('menu-' + nav.landing);
  return el && !el.closest('.perm-hidden, .module-hidden, .maturity-hidden') ? nav.landing : '';
}

// Stage 47.14.6: a list's lone "No ... yet" / "Nothing ..." row is an empty
// state like the others; tag it so it takes the shared look (shell-steady.css).
const EMPTY_ROW = 'tbody > tr:only-child > td[colspan]:not(.state-empty)';
let emptyTimer = 0;
function tagEmptyRows() {
  emptyTimer = 0;
  document.querySelectorAll('#view-root ' + EMPTY_ROW).forEach(td => {
    if (!td.children.length && /^(No|Nothing|There are no)\b/.test(td.textContent.trim())) td.classList.add('state-empty');
  });
}
export function watchEmptyStates() {
  const root = document.getElementById('view-root');
  if (!root || root.dataset.emptyWatch) return;
  root.dataset.emptyWatch = '1';
  new MutationObserver(() => { if (!emptyTimer) emptyTimer = requestAnimationFrame(tagEmptyRows); })
    .observe(root, { childList: true, subtree: true });
  tagEmptyRows();
}

// 47.10.4: a table of more than CHUNK_MIN rows shows CHUNK rows and adds more as
// the end scrolls into view. aria-rowcount/rowindex keep screen readers right.
// Everything is shown on Ctrl+F, print, a sort click or a filter keystroke.
// Exports and CSV never read the DOM, so they still get every row.
// 319.4: rows are the ones a user sees. A detail row a screen keeps hidden under
// each row (a report's drill-down) is not counted and moves with its row.
// 319.5: the button has its own footer row, so the table's own footer (a Total line) stays.
const CHUNK = 200, CHUNK_MIN = 400;
const heldRows = new Map();
const detailRows = new WeakSet();
const shown = rows => rows.reduce((n, r) => n + (detailRows.has(r) ? 0 : 1), 0);
// How many of rows hold the next n shown rows and the detail rows under them.
function upTo(rows, n) {
  let k = 0;
  for (let i = 0; i < rows.length; i++) if (!detailRows.has(rows[i]) && k++ === n) return i;
  return rows.length;
}
function addRows(t, n) {
  const rest = heldRows.get(t), tb = t.tBodies[0];
  const batch = rest.splice(0, upTo(rest, n)), base = tb.rows.length + t.tHead.rows.length;
  const frag = document.createDocumentFragment();
  batch.forEach((r, i) => { r.setAttribute('aria-rowindex', base + i + 1); frag.appendChild(r); });
  tb.appendChild(frag);
  const more = t.tFoot && t.tFoot.querySelector('.chunk-more');
  if (!rest.length) {
    heldRows.delete(t);
    more?.closest('tr').remove();
    if (t.tFoot && !t.tFoot.rows.length) t.tFoot.remove();
  } else if (more) {
    const now = shown([...tb.rows]), all = now + shown(rest);
    more.textContent = now + ' / ' + all;
    more.setAttribute('aria-label', 'Show all ' + all + ' rows');
  }
}
export function showAllRows() { [...heldRows.keys()].forEach(t => addRows(t, Infinity)); }
function chunkTable(t) {
  const tb = t.tBodies[0];
  if (!tb || !t.tHead || t.dataset.chunked || tb.rows.length <= CHUNK_MIN || t.dataset.chunkSeen === String(tb.rows.length)) return;
  t.dataset.chunkSeen = tb.rows.length;
  const all = [...tb.rows];
  if (all.filter(r => !r.classList.contains('hidden')).length <= CHUNK_MIN) return;
  t.dataset.chunked = '1';
  all.forEach(r => { if (r.classList.contains('hidden')) detailRows.add(r); });
  const head = t.tHead.rows.length, cut = upTo(all, CHUNK), total = shown(all);
  t.setAttribute('aria-rowcount', all.length + head);
  [...t.tHead.rows].forEach((r, i) => r.setAttribute('aria-rowindex', i + 1));
  all.forEach((r, i) => r.setAttribute('aria-rowindex', head + i + 1));
  heldRows.set(t, all.slice(cut));
  all.slice(cut).forEach(r => r.remove());
  const cols = t.tHead.rows[0].cells.length;
  const row = t.createTFoot().insertRow(0);
  row.innerHTML = '<td colspan="' + cols + '"><button type="button" class="btn chunk-more"></button></td>';
  const btn = row.querySelector('.chunk-more');
  btn.textContent = CHUNK + ' / ' + total;
  btn.setAttribute('aria-label', 'Show all ' + total + ' rows');
  btn.addEventListener('click', () => { addRows(t, Infinity); const r = t.tBodies[0].rows[cut]; if (r) { r.tabIndex = -1; r.focus(); } });
  new IntersectionObserver((es, io) => { if (es[0].isIntersecting) { if (heldRows.has(t)) addRows(t, CHUNK); else io.disconnect(); } }, { rootMargin: '400px' }).observe(btn);
}
export function watchBigTables() {
  const root = document.getElementById('view-root');
  if (!root) return;
  root.querySelectorAll('table').forEach(chunkTable);
  if (root.dataset.chunkWatch) return;
  root.dataset.chunkWatch = '1';
  new MutationObserver(() => { heldRows.forEach((_, t) => { if (!t.isConnected) heldRows.delete(t); }); root.querySelectorAll('table').forEach(chunkTable); }).observe(root, { childList: true, subtree: true });
  document.addEventListener('keydown', e => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f') showAllRows(); });
  window.addEventListener('beforeprint', showAllRows);
  // Capture phase: the held rows must be in the DOM BEFORE the screen's own handler
  // runs its DOM-row filter or sort, or the late rows appear unfiltered afterwards.
  const all = () => { if (heldRows.size) showAllRows(); };
  root.addEventListener('click', e => { if (e.target.closest('th')) all(); }, true);
  root.addEventListener('input', all, true);
  root.addEventListener('change', all, true);
  root.addEventListener('search', all, true);
  root.addEventListener('keydown', e => { if (e.key === 'Enter' && e.target.closest('input, select')) all(); }, true);
}
