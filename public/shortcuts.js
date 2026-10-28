// Stage 139 (user, 2026-10-26): "We have to build erp shorcuts ... & Need
// report number as wel & Tell what else can be done for trasaction and access
// transaction to make user's life easy", then "Build all of the above".
// Plan: docs/product/shortcuts-plan-2026-10-26.md. One box and one key set:
//
//  * The top-bar search is the Ctrl+K box. It moved here from app.js (cold
//    core) and now also finds reports by number (R1001), short codes (PO, PO+,
//    PO 42 - public/shortcut-codes.json), documents by number (typed, or
//    scanned anywhere), favourites, recents and the My work counts.
//  * The standard keys (SHORTCUT_KEYS, shown by "?") press the screen's own
//    buttons, found by their label, so every screen has them without a change
//    of its own and every permission stays where it is.
//  * After a save: Print / New / Copy / the next step. Last-used store,
//    location and payment fields; an unsaved New form kept if the tab closes;
//    saved searches on record lists.
// Loaded once signed in (app.js); a screen needs nothing but its own DOM.

const V = new URL(import.meta.url).search;
const esc = s => window.escapeHTMLText(String(s ?? ''));
const MOD = /Mac|iPhone|iPad/.test(navigator.platform || '') ? '⌘' : 'Ctrl';
const user = () => localStorage.getItem('erp_username') || '';

if (!document.querySelector('link[href*="shortcuts.css"]')) {
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = `/shortcuts.css${V}`;
  document.head.appendChild(link);
}

// Per-person conveniences kept in this browser: recents, last-used values,
// drafts. Favourites and saved views are server records (UserShortcut), so
// they follow the person to any computer. Storage can be missing (a private
// window); everything still works without it.
const lsKey = k => `erp_sc_${k}:${localStorage.getItem('erp_tenant_id') || 'default'}:${user()}`;
function lsGet(k, fallback) {
  try { const v = JSON.parse(localStorage.getItem(lsKey(k)) || 'null'); return v ?? fallback; } catch (_) { return fallback; }
}
function lsSet(k, v) {
  try { if (v == null) localStorage.removeItem(lsKey(k)); else localStorage.setItem(lsKey(k), JSON.stringify(v)); } catch (_) { /* works without */ }
}

// The key list "?" shows, the Help article mirrors and the guide documents.
export const SHORTCUT_KEYS = [
  ['Anywhere', [[`${MOD}+K`, 'Search box: screens, codes, reports, documents'], ['/', 'Search box (when you are not typing)'],
    ['?', 'These keys and codes'], ['Alt+N', 'New record on this screen'], ['Esc', 'Close a dialog, menu or this list']]],
  ['Lists', [['↑ ↓', 'Move between rows'], ['Enter', 'Open the row'], ['Space', 'Tick the row']]],
  ['Forms', [[`${MOD}+S`, 'Save'], [`${MOD}+Enter`, 'Save and submit'], [`${MOD}+P`, 'Print the document'],
    ['Enter', 'Next field (New transaction pages)'], [`${MOD}+D`, 'Add a line'], [`${MOD}+Delete`, 'Remove the line you are on']]],
  ['Search box', [['PO', 'Open the screen with that code'], ['PO+', 'New record'], ['PO 42', 'Open number 42'],
    ['R1001', 'Open the report with that number'], ['PO/HO/26-27/00103', 'Open that document (or scan its barcode)']]],
];

// ---------------------------------------------------------------------------
// Codes, reports, destinations
// ---------------------------------------------------------------------------

let codes = [];
const codesReady = fetch(`/shortcut-codes.json${V}`).then(r => (r.ok ? r.json() : {})).then(j => { codes = j.codes || []; }).catch(() => {});
const codeFor = c => codes.find(x => x.code === String(c || '').toUpperCase());
// A code shows only where its screen does: the sidebar entry's own permission
// and module filtering decide, so a code never opens what a menu would not.
function codeOpen(c) {
  if (c.setup) return (state.activeDoctypes || []).some(d => d.name === c.setup) && window.canReadDoctype(c.setup);
  const el = document.getElementById(c.menu);
  return !!el && !el.closest('.perm-hidden, .module-hidden');
}
// May this role make one? Its own screen's New, so a record made only on its
// own screen (canCreateDoctype says no to the record form) still counts.
const codeCanNew = c => !!c.doctype && codeOpen(c) && (state.permissions.isAdmin || state.permissions.create.has(c.doctype));

let reports = null, reportsTried = 0;
async function loadReports() {
  if (reports) return reports;
  if (Date.now() - reportsTried < 60000) return []; // no Reports module, or offline: not on every keystroke
  reportsTried = Date.now();
  const res = await window.apiFetch('/api/v1/reports/catalog').catch(() => null);
  if (!res || !res.ok) return [];
  reports = await res.json();
  return reports;
}
const REPORT_AREAS = { 1: 'Sales & POS', 2: 'Procurement', 3: 'Inventory', 4: 'WMS', 5: 'Finance', 6: 'CRM & Commerce', 7: 'PIM & Manufacturing', 8: 'HR', 9: 'MIS, Master Data & Audit' };
const reportByRoute = route => (reports || []).find(r => `reports:${r.id}` === route);

// A destination is what a result, favourite or recent opens: plain data, so
// it can be saved. menu = click that sidebar entry; view = a route; setup = a
// record list; doc = one document, shown in its record list.
const destKey = d => [d.t, d.id || d.route || d.doctype || '', d.doc || ''].join(':');
async function openDest(d) {
  window.closeSubmenus?.();
  if (d.t === 'menu') {
    const el = document.getElementById(d.id);
    if (el) el.click();
    return !!el;
  }
  if (d.t === 'view') return window.navigateToDeepLink({ kind: 'view', view: d.route });
  if (d.t === 'setup') return window.navigateToDeepLink({ kind: 'setup', doctype: d.doctype });
  if (d.t === 'doc') {
    window.navigateHomeRecent(d.doctype, d.doc);
    recordRecent(d, `${window.getDoctypeLabel(d.doctype)} ${d.doc}`);
    return true;
  }
  return false;
}

// whenButton waits for a screen opened a moment ago to draw its button.
function whenButton(kind, ms = 4000) {
  return new Promise(resolve => {
    const t0 = performance.now();
    const tick = () => {
      const b = findButton(kind, document.getElementById('view-root'));
      if (b || performance.now() - t0 > ms) resolve(b);
      else setTimeout(tick, 120);
    };
    setTimeout(tick, 150);
  });
}

async function openCode(c, { fresh = false } = {}) {
  const ok = await openDest(c.setup ? { t: 'setup', doctype: c.setup } : { t: 'menu', id: c.menu });
  if (!ok || !fresh) return;
  const b = await whenButton('new');
  if (b) b.click();
  else window.showToast(`${c.name} has no New on this screen.`, { variant: 'warning' });
}

// The last number in a document id: PO/HO/26-27/00103 -> 103.
const lastNumber = id => { const m = String(id).match(/(\d+)\D*$/); return m ? Number(m[1]) : NaN; };

// "PO 42": the list API is already scoped to the person's stores, and
// sort=recent puts this year first. One match opens; several are offered.
async function findByCode(c, text) {
  const want = text.trim();
  const res = await window.apiFetch(`/api/v1/doc/${encodeURIComponent(c.doctype)}?q=${encodeURIComponent(want)}&limit=50&sort=recent`).catch(() => null);
  const rows = res && res.ok ? await res.json() : [];
  const n = /^\d+$/.test(want) ? Number(want) : NaN;
  const hits = rows.filter(r => {
    const id = String(r.id || '');
    return id === want || (!Number.isNaN(n) && lastNumber(id) === n) || String(r.code || '') === want || id.toUpperCase().endsWith(want.toUpperCase());
  });
  if (hits.length === 1) return openDest({ t: 'doc', doctype: c.doctype, doc: hits[0].id });
  if (hits.length) return bar.offer(hits.slice(0, 12).map(r => ({ kind: 'Document', label: r.id, context: `${c.name}${r.status ? ' · ' + r.status : ''}`, tag: c.code, dest: { t: 'doc', doctype: c.doctype, doc: r.id } })));
  window.showToast(`No ${c.name} ${want} that you can open.`, { variant: 'warning' });
}

// A full number, typed or scanned: the server says which record type holds
// it, and only if this person may read that type (GET /api/v1/doc-find).
async function openNumber(text) {
  const id = text.trim();
  const res = await window.apiFetch(`/api/v1/doc-find?id=${encodeURIComponent(id)}`).catch(() => null);
  if (!res) return;
  if (!res.ok) { window.showToast(`No document numbered ${id} that you can open.`, { variant: 'warning' }); return; }
  const found = await res.json();
  return openDest({ t: 'doc', doctype: found.doctype, doc: found.id });
}
const looksLikeNumber = q => /\d/.test(q) && /[/-]/.test(q) && /^[A-Za-z0-9][A-Za-z0-9/._-]{3,}$/.test(q.trim());

// ---------------------------------------------------------------------------
// Recents, favourites, My work
// ---------------------------------------------------------------------------

function recordRecent(dest, label) {
  if (!dest || !label) return;
  const k = destKey(dest);
  const list = lsGet('recent', []).filter(r => r && r.d && destKey(r.d) !== k);
  list.unshift({ d: dest, label });
  lsSet('recent', list.slice(0, 10));
}

let favs = null;
async function loadFavs() {
  if (favs) return favs;
  const res = await window.apiFetch(`/api/v1/doc/UserShortcut?owner=${encodeURIComponent(user())}&kind=Favourite&limit=200`).catch(() => null);
  if (!res || !res.ok) return [];
  favs = (await res.json()).map(r => { try { return { id: r.id, label: r.name, dest: JSON.parse(r.target) }; } catch (_) { return null; } }).filter(Boolean);
  return favs;
}
async function toggleFav(dest, label) {
  await loadFavs();
  if (!favs) favs = [];
  const k = destKey(dest);
  const have = favs.find(f => destKey(f.dest) === k);
  if (have) {
    const res = await window.apiFetch(`/api/v1/doc/UserShortcut/${encodeURIComponent(have.id)}`, { method: 'DELETE' });
    if (!res || !res.ok) { if (res) await window.showApiError(res, 'Could not remove the favourite.'); return; }
    favs = favs.filter(f => f !== have);
    window.showToast(`${label} removed from favourites.`);
  } else {
    const res = await window.apiFetch('/api/v1/doc/UserShortcut', { method: 'POST',
      body: JSON.stringify({ kind: 'Favourite', name: label, target: JSON.stringify(dest), owner: user(), status: 'Active' }) });
    if (!res || !res.ok) { if (res) await window.showApiError(res, 'Could not save the favourite.'); return; }
    const body = await res.json().catch(() => ({}));
    favs.push({ id: body.id, label, dest });
    window.showToast(`${label} added to favourites.`, { variant: 'success' });
  }
  placeStar();
}

let work = null, workAt = 0;
async function loadWork() {
  if (work && Date.now() - workAt < 60000) return work;
  const approvals = document.getElementById('menu-approvals');
  if (!approvals || approvals.closest('.perm-hidden, .module-hidden')) return [];
  const res = await window.apiFetch('/api/v1/inbox').catch(() => null);
  if (!res || !res.ok) return work || [];
  const by = {};
  ((await res.json()).items || []).forEach(i => { by[i.source] = (by[i.source] || 0) + 1; });
  work = Object.entries(by).map(([s, n]) => ({ kind: 'My work', label: s === 'Approval' ? 'Approvals' : s, tag: String(n), dest: { t: 'menu', id: 'menu-approvals' } }));
  workAt = Date.now();
  return work;
}

// ---------------------------------------------------------------------------
// The Ctrl+K box (was setupGlobalSearchSuggest in app.js, Stage 28.5)
// ---------------------------------------------------------------------------

const LIMIT = 12;

// Every sidebar destination (flyout entries included) and every record type,
// as before; a screen with a code carries it as its tag.
function buildGlobalSearchIndex() {
  const entries = [];
  const seen = new Set();
  const hidden = el => el.classList.contains('perm-hidden') || el.classList.contains('module-hidden');
  document.querySelectorAll('.sidebar-menu .menu-item-container').forEach(li => {
    if (hidden(li)) return;
    const group = li.querySelector(':scope > .menu-item-group');
    const moduleLabel = group ? group.textContent.trim() : '';
    const anchors = group
      ? [...li.querySelectorAll('.menu-flyout li:not(.has-subflyout)')]
          .filter(row => !row.closest('.perm-hidden, .module-hidden'))
          .map(row => row.querySelector('.menu-item, .submenu-item'))
          .filter(Boolean)
      : [li.querySelector(':scope > .menu-item')].filter(Boolean);
    anchors.forEach(a => {
      // A report's menu entry is listed from the catalog below, with its number.
      const go = a.dataset.go || '';
      if (go.startsWith('reports:') && (reports || []).some(r => `reports:${r.id}` === go)) return;
      const label = (a.querySelector('span') || a).textContent.trim();
      if (!label) return;
      const key = 'nav:' + (a.id || `${moduleLabel}/${label}`);
      if (seen.has(key)) return;
      seen.add(key);
      seen.add('label:' + label.toLowerCase());
      const code = codes.find(c => c.menu && c.menu === a.id && !c.doctype) || codes.find(c => c.menu && c.menu === a.id);
      entries.push({ kind: 'Screen', label, context: moduleLabel, el: a, tag: code ? code.code : '', code });
    });
  });
  (state.activeDoctypes || []).forEach(d => {
    // Personal records (favourites, saved filters, layouts) are not places.
    if (/^(UserShortcut|ReportFilterPreset|ReportColumnProfile|DashboardLayout|OMSSavedView)$/.test(d.name)) return;
    const label = window.getDoctypeLabel(d.name);
    if (seen.has('label:' + label.toLowerCase()) || seen.has('doc:' + d.name)) return;
    seen.add('doc:' + d.name);
    const code = codes.find(c => c.setup === d.name);
    entries.push({ kind: 'Record type', label, context: d.module || '', raw: d.name, dest: { t: 'setup', doctype: d.name }, tag: code ? code.code : '', code });
  });
  (reports || []).forEach(r => entries.push({ kind: 'Report', label: r.label, context: r.category || '', tag: r.number || '', dest: { t: 'view', route: `reports:${r.id}` } }));
  return entries;
}

function matchEntries(entries, query) {
  const needle = query.trim().toLowerCase();
  if (!needle) return [];
  const scored = [];
  entries.forEach(entry => {
    const label = entry.label.toLowerCase();
    const tag = String(entry.tag || '').toLowerCase();
    const hay = `${label} ${entry.context} ${entry.raw || ''} ${tag}`.toLowerCase();
    const hayIdx = hay.indexOf(needle);
    if (hayIdx < 0) return;
    const labelIdx = label.indexOf(needle);
    // An exact code or number first; a hit on the name beats one on its
    // module; an earlier hit beats a later one.
    const rank = tag === needle ? -50 : labelIdx === 0 ? 0 : labelIdx > 0 ? 20 : 100;
    scored.push({ entry, score: rank + Math.min(hayIdx, 19) });
  });
  scored.sort((a, b) => a.score - b.score || a.entry.label.localeCompare(b.entry.label));
  return scored.slice(0, LIMIT).map(s => s.entry);
}

// The typed forms that are commands rather than names: PO+, PO 42, R1001, a
// document number. They go above the name matches.
function commandEntries(query) {
  const q = query.trim();
  const out = [];
  const m = q.match(/^([A-Za-z]{2,4})(\+|\s+(\S.*))?$/);
  const c = m && codeFor(m[1]);
  if (c && codeOpen(c)) {
    if (m[2] === '+') {
      out.push(codeCanNew(c)
        ? { kind: 'New', label: `New ${c.name}`, context: c.area, tag: `${c.code}+`, run: () => openCode(c, { fresh: true }) }
        : { kind: 'Screen', label: c.name, context: 'Your role cannot make a new one here', tag: c.code, run: () => openCode(c) });
    } else if (m[3] && c.doctype) {
      out.push({ kind: 'Find', label: `${c.name} ${m[3].trim()}`, context: 'Open by number', tag: c.code, run: () => findByCode(c, m[3]) });
    } else if (!m[2]) {
      out.push({ kind: 'Screen', label: c.name, context: c.area, tag: c.code, run: () => openCode(c), code: c });
      if (codeCanNew(c)) out.push({ kind: 'New', label: `New ${c.name}`, context: c.area, tag: `${c.code}+`, run: () => openCode(c, { fresh: true }) });
    }
  }
  if (looksLikeNumber(q)) out.push({ kind: 'Document', label: `Open ${q}`, context: 'Find this number', run: () => openNumber(q) });
  return out;
}

async function emptyEntries() {
  const sections = [];
  const f = await loadFavs();
  if (f.length) sections.push(['Favourites', f.map(x => ({ kind: 'Favourite', label: x.label, dest: x.dest, tag: '★' }))]);
  const w = await loadWork();
  if (w.length) sections.push(['My work', w]);
  const r = lsGet('recent', []).filter(x => x && x.d).slice(0, 6);
  if (r.length) sections.push(['Recent', r.map(x => ({ kind: 'Recent', label: x.label, dest: x.d }))]);
  return sections;
}

const bar = { offer: () => {}, run: () => {} };

function setupBar(inputEl) {
  if (!inputEl || inputEl.dataset.suggestBound) return;
  inputEl.dataset.suggestBound = '1';
  inputEl.setAttribute('autocomplete', 'off');
  inputEl.placeholder = `Search · code · number  (${MOD}+K)`;
  inputEl.setAttribute('aria-label', 'Search screens, codes, reports and documents');
  inputEl.setAttribute('aria-keyshortcuts', MOD === '⌘' ? 'Meta+K' : 'Control+K');

  let menu = null;
  let results = [];
  let activeIndex = -1;
  let seq = 0;

  const onDocMouseDown = (e) => {
    if (menu && !menu.contains(e.target) && e.target !== inputEl) close();
  };

  function close() {
    if (menu) { menu.remove(); menu = null; }
    document.removeEventListener('mousedown', onDocMouseDown, true);
    results = [];
    activeIndex = -1;
  }

  function highlight(idx) {
    if (!menu) return;
    const rows = menu.querySelectorAll('.typeahead-item');
    rows.forEach(r => r.classList.remove('active'));
    if (idx >= 0 && rows[idx]) {
      rows[idx].classList.add('active');
      rows[idx].scrollIntoView({ block: 'nearest' });
    }
    activeIndex = idx;
  }

  async function pick(entry) {
    close();
    inputEl.value = '';
    inputEl.blur();
    if (entry.run) return entry.run();
    if (entry.el) { entry.el.click(); return; }
    if (entry.dest) return openDest(entry.dest);
  }

  // sections: [[heading or '', entries]]
  function draw(sections, emptyText) {
    close();
    menu = document.createElement('div');
    menu.className = 'typeahead-menu global-search-menu';
    menu.setAttribute('role', 'listbox');
    const rect = inputEl.getBoundingClientRect();
    menu.style.left = `${Math.round(rect.left)}px`;
    menu.style.top = `${Math.round(rect.bottom + 6)}px`;
    menu.style.width = `${Math.round(Math.max(rect.width, 340))}px`;
    sections.forEach(([heading, list]) => {
      if (!list.length) return;
      if (heading) {
        const h = document.createElement('div');
        h.className = 'sc-section';
        h.textContent = heading;
        menu.appendChild(h);
      }
      list.forEach(entry => {
        results.push(entry);
        const row = document.createElement('div');
        row.className = 'typeahead-item global-search-item';
        row.setAttribute('role', 'option');
        row.innerHTML = `<span class="global-search-label">${esc(entry.label)}${entry.tag ? `<span class="sc-tag">${esc(entry.tag)}</span>` : ''}</span>`
          + `<span class="global-search-meta">${esc(entry.context ? `${entry.kind} · ${entry.context}` : entry.kind)}</span>`;
        row.addEventListener('mousedown', (e) => { e.preventDefault(); pick(entry); });
        menu.appendChild(row);
      });
    });
    if (!results.length) {
      const empty = document.createElement('div');
      empty.className = 'global-search-empty';
      empty.textContent = emptyText;
      menu.appendChild(empty);
    }
    const foot = document.createElement('div');
    foot.className = 'sc-foot';
    foot.innerHTML = `<span>↑↓ Enter</span><span>PO · PO+ · PO 42 · R1001</span><button type="button" class="sc-link">? Keys</button>`;
    foot.querySelector('button').addEventListener('mousedown', (e) => { e.preventDefault(); close(); inputEl.blur(); openKeys(); });
    menu.appendChild(foot);
    document.body.appendChild(menu);
    document.addEventListener('mousedown', onDocMouseDown, true);
  }

  async function render(query) {
    const mine = ++seq;
    await codesReady;
    await loadReports();
    if (mine !== seq) return;
    if (!query.trim()) {
      const sections = await emptyEntries();
      if (mine !== seq || document.activeElement !== inputEl || inputEl.value.trim()) return;
      draw(sections, 'Type a screen, a code (PO), a report number (R1001) or a document number.');
      return;
    }
    const commands = commandEntries(query);
    const keys = new Set(commands.map(e => `${e.kind}:${e.label}`));
    const names = matchEntries(buildGlobalSearchIndex(), query).filter(e => !keys.has(`${e.kind}:${e.label}`) && !(e.code && commands.some(c => c.code === e.code)));
    draw([['', commands], ['', names.slice(0, LIMIT - commands.length)]], `Nothing matches "${query.trim()}"`);
  }

  // offer shows chosen rows (several documents matched one number).
  bar.offer = (entries) => { inputEl.focus(); draw([['Pick one', entries]], ''); highlight(0); };
  // run acts on text as if it were typed and Enter pressed (a scan).
  bar.run = async (text) => {
    await codesReady;
    const commands = commandEntries(text);
    if (commands.length) return commands[0].run();
    return openNumber(text);
  };
  bar.focus = () => { inputEl.focus(); inputEl.select(); render(inputEl.value); };
  bar.close = close;

  inputEl.addEventListener('input', () => render(inputEl.value));
  inputEl.addEventListener('focus', () => render(inputEl.value));
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { close(); inputEl.blur(); return; }
    if (e.key === 'Enter' && !menu && inputEl.value.trim()) { e.preventDefault(); bar.run(inputEl.value); inputEl.value = ''; return; }
    if (!menu || results.length === 0) return;
    if (e.key === 'ArrowDown') { e.preventDefault(); highlight(Math.min(activeIndex + 1, results.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); highlight(Math.max(activeIndex - 1, 0)); }
    else if (e.key === 'Enter') {
      e.preventDefault();
      pick(results[activeIndex >= 0 ? activeIndex : 0]);
    }
  });
  window.addEventListener('resize', close);
}

// ---------------------------------------------------------------------------
// Buttons: the keys press what the screen already shows
// ---------------------------------------------------------------------------

const BUTTONS = {
  new: /^\+?\s*new\b/i,
  save: /^(save|save draft|save as draft|save changes|update)\b/i,
  submit: /^(submit|post|save (and|&) (submit|post)|(submit|send) for approval)\b/i,
  print: /^print\b(?!.*\b(sticker|label|barcode)s?\b)/i,
  line: /^\+?\s*add( a)?( new)? (line|row|item|design|sku)\b|^\+?\s*add$/i,
};
const btnText = b => (b.textContent || b.value || b.getAttribute('aria-label') || '').replace(/\s+/g, ' ').trim();
const shown = el => !!(el && (el.offsetWidth || el.offsetHeight || el.getClientRects().length));
const keysOpen = () => { const b = document.getElementById('sc-keys'); return !!b && !b.hidden; };
// The open dialog, of the shell's two kinds or the document print dialog.
function openDialog() {
  return document.querySelector('#custom-dialog-container:not(.hidden)') || document.querySelector('.modal-overlay.open') || document.querySelector('.td-overlay');
}
// The page being worked on: an open dialog, else a New page, else the screen.
function workRoot() {
  return openDialog() || formHosts().find(h => !h.matches('.modal-overlay')) || document.getElementById('view-root');
}
function findButton(kind, root) {
  root = root || workRoot();
  if (!root) return null;
  const re = BUTTONS[kind];
  const hits = [...root.querySelectorAll('button, a.btn, input[type="submit"]')].filter(b =>
    !b.disabled && shown(b) && !b.closest('.sc-keys, #sc-saved, .sc-star')
    && (re.test(btnText(b)) || (kind === 'new' && (b.id === 'doc-create-button' || /:new"/.test(b.dataset.actArgs || '')))));
  return hits.find(b => b.classList.contains('btn-primary')) || hits[0] || null;
}
// Save from inside a form prefers that form's own submit button, whatever it
// is called ("Create", "Receive").
function saveButton() {
  const f = document.activeElement && document.activeElement.closest && document.activeElement.closest('form');
  if (f) {
    const own = findButton('save', f) || [...f.querySelectorAll('button[type="submit"], input[type="submit"]')].find(b => !b.disabled && shown(b));
    if (own) return own;
  }
  return findButton('save');
}

// Tooltips and aria-keyshortcuts name each key on the button it presses.
const HINTS = [['new', 'Alt+N', 'Alt+N'], ['save', `${MOD}+S`, 'Control+S'], ['submit', `${MOD}+Enter`, 'Control+Enter'], ['print', `${MOD}+P`, 'Control+P'], ['line', `${MOD}+D`, 'Control+D']];
function tagButtons(root) {
  if (!root) return;
  HINTS.forEach(([kind, label, aria]) => {
    const b = findButton(kind, root);
    if (!b || b.hasAttribute('aria-keyshortcuts')) return;
    b.setAttribute('aria-keyshortcuts', aria);
    b.title = b.title ? `${b.title} · ${label}` : `${btnText(b)} · ${label}`;
  });
}

// ---------------------------------------------------------------------------
// The keys
// ---------------------------------------------------------------------------

const typing = t => !!t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
const isScanBox = t => t.tagName === 'INPUT' && (t.classList.contains('pos-scan-input') || /^scan/i.test(t.placeholder || '') || !!window.isScanField?.(t.id || t.name));

let rowTable = null;
function listRows() {
  const root = document.getElementById('view-root');
  const table = root && [...root.querySelectorAll('table')].find(t => shown(t) && t.tBodies[0] && t.tBodies[0].rows.length && !t.closest('form, [data-pane="new"]'));
  if (!table) return null;
  if (table !== rowTable) { rowTable = table; }
  return [...table.tBodies[0].rows].filter(r => shown(r) && r.cells.length > 1);
}
function moveRow(step) {
  const rows = listRows();
  if (!rows || !rows.length) return false;
  const at = rows.findIndex(r => r.classList.contains('sc-row'));
  const next = Math.max(0, Math.min(rows.length - 1, at < 0 ? 0 : at + step));
  rows.forEach((r, i) => r.classList.toggle('sc-row', i === next));
  rows[next].scrollIntoView({ block: 'nearest' });
  return true;
}
function activeRow() {
  return rowTable && rowTable.isConnected ? rowTable.querySelector('tr.sc-row') : null;
}
function openRow(r) {
  const b = r.querySelector('[data-doc-action="edit"], a.doc-link, a[href]:not([href="#"]), button[title="Edit" i], button[title^="Open" i], button[title="View" i]');
  (b || r.cells[0]).click();
}

// Enter moves to the next field on a New transaction page - a paged screen's
// page that is not its list (txn-screen.js). The last field hands over to
// Save, so one more Enter saves (D6: transaction entry only, not notes boxes
// or pickers, which keep Enter for themselves).
// A screen that redraws per page (the Purchase Order) has no panes: off its
// list it is the New page itself.
function entryPane(t) {
  const screen = t.closest('[data-landing]');
  if (!screen) return null;
  const pane = t.closest('[data-pane]');
  if (pane && pane.parentElement === screen) return pane.dataset.pane !== screen.dataset.landing ? pane : null;
  return screen.classList.contains('txn-on-landing') ? null : screen;
}
// The first fillable field after this one in page order - from a read-only
// box (the series-made number) too.
function nextField(from, scope) {
  return [...scope.querySelectorAll('input, select, textarea, button[type="submit"]')].find(f =>
    f !== from && (from.compareDocumentPosition(f) & Node.DOCUMENT_POSITION_FOLLOWING)
    && !f.disabled && !f.readOnly && f.tabIndex >= 0 && shown(f) && !/^(hidden|checkbox|radio|file|button)$/.test(f.type || '')) || null;
}

// A scanner types a whole number in a few milliseconds and ends with Enter.
// Outside any field that is a document to open (D: scan to open).
let scanBuf = '', scanLast = 0;

function onKey(e) {
  if (e.defaultPrevented || e.isComposing) return;
  const t = e.target;
  const ctrl = e.ctrlKey || e.metaKey;
  const dialog = !!openDialog() || keysOpen();
  const k = e.key;

  if (ctrl && !e.altKey && (k === 'k' || k === 'K')) { e.preventDefault(); closeKeys(); bar.focus?.(); return; }
  if (keysOpen() && k === 'Escape') { closeKeys(); return; }

  const inPOS = currentView === 'pos';
  const now = performance.now();
  const fast = now - scanLast < 60;
  if (!typing(e.target) && !ctrl && !e.altKey && !dialog) {
    if (k === 'Enter' && scanBuf.length >= 4 && fast) { e.preventDefault(); const v = scanBuf; scanBuf = ''; if (!inPOS) bar.run(v); return; }
    if (k.length === 1) {
      const scanning = scanBuf && fast;
      scanBuf = scanning ? scanBuf + k : k;
      scanLast = now;
      if (scanning) return;
      if (k === '/') { e.preventDefault(); bar.focus?.(); return; }
      if (k === '?') { e.preventDefault(); openKeys(); return; }
      if (k === ' ' && activeRow()) { const box = activeRow().querySelector('input[type="checkbox"]'); if (box) { e.preventDefault(); box.click(); } return; }
      return;
    }
    if ((k === 'ArrowDown' || k === 'ArrowUp') && !inPOS && moveRow(k === 'ArrowDown' ? 1 : -1)) { e.preventDefault(); return; }
    if (k === 'Enter' && activeRow()) { e.preventDefault(); openRow(activeRow()); return; }
  }

  if (inPOS) return; // POS keeps its own keys
  if (e.altKey && !ctrl && (e.code === 'KeyN') && !dialog) {
    const b = findButton('new', document.getElementById('view-root'));
    if (b) { e.preventDefault(); b.click(); }
    return;
  }
  if (ctrl && !e.altKey && (k === 's' || k === 'S')) {
    e.preventDefault(); // never the browser's Save page
    const b = saveButton();
    if (b) b.click(); else window.showToast('Nothing to save here.');
    return;
  }
  if (ctrl && k === 'Enter') {
    const b = findButton('submit') || saveButton();
    if (b) { e.preventDefault(); b.click(); }
    return;
  }
  if (ctrl && !e.altKey && (k === 'p' || k === 'P')) {
    const row = activeRow();
    const b = (row && findButton('print', row)) || findButton('print');
    if (b) { e.preventDefault(); b.click(); } // else the browser prints the page
    return;
  }
  if (ctrl && !e.altKey && (k === 'd' || k === 'D')) {
    const host = document.activeElement?.closest?.('[data-pane], form, .modal-overlay.open');
    const b = findButton('line', host || undefined);
    if (b) { e.preventDefault(); b.click(); }
    return;
  }
  if (ctrl && k === 'Delete') {
    const tr = t.closest && t.closest('tr');
    const b = tr && [...tr.querySelectorAll('button, a')].find(x => /remove|delete|^[×✕x]$/i.test(`${btnText(x)} ${x.title || ''}`));
    if (b) { e.preventDefault(); b.click(); }
    return;
  }
  if (k === 'Enter' && !ctrl && !e.altKey && !e.shiftKey && !dialog
    && /^(INPUT|SELECT)$/.test(t.tagName) && !/^(button|submit|checkbox|radio|file)$/.test(t.type || '') && !isScanBox(t)
    && ![...document.querySelectorAll('.typeahead-menu')].some(shown)) {
    const pane = entryPane(t);
    if (!pane) return;
    if (t.form) e.preventDefault(); // no surprise submit from the middle of a form
    // The screen's own Enter (add a line, look a code up) runs first and may
    // move focus or redraw; only a field Enter left alone moves on.
    setTimeout(() => {
      if (document.activeElement !== t || !t.isConnected) return;
      const next = nextField(t, pane);
      if (next) next.focus();
      else { const s = findButton('save', pane) || findButton('submit', pane); if (s) s.focus(); }
    }, 0);
  }
}

// ---------------------------------------------------------------------------
// "?" - the keys, the codes and the report numbers, printable
// ---------------------------------------------------------------------------

function openKeys() {
  let box = document.getElementById('sc-keys');
  if (!box) {
    box = document.createElement('div');
    box.id = 'sc-keys';
    box.className = 'sc-keys';
    box.setAttribute('role', 'dialog');
    box.setAttribute('aria-modal', 'true');
    box.setAttribute('aria-labelledby', 'sc-keys-title');
    box.addEventListener('mousedown', (e) => { if (e.target === box) closeKeys(); });
    document.body.appendChild(box);
  }
  const visibleCodes = codes.filter(codeOpen);
  const areas = [...new Set(visibleCodes.map(c => c.area))];
  box.innerHTML = `<div class="sc-keys-panel">
    <div class="sc-keys-head"><h2 id="sc-keys-title">Keyboard shortcuts</h2>
      <span><button type="button" class="btn btn-outline btn-sm" data-sc="reports">Report numbers</button>
      <a class="btn btn-outline btn-sm" href="/help/keyboard-shortcuts" target="_blank" rel="noopener">Guide</a>
      <button type="button" class="btn btn-outline btn-sm" data-sc="print">Print</button>
      <button type="button" class="btn btn-outline btn-sm" data-sc="close" aria-label="Close">Esc</button></span></div>
    <div class="sc-keys-grid">${SHORTCUT_KEYS.map(([area, rows]) => `<section><h3>${esc(area)}</h3><dl>${rows.map(([key, what]) =>
      `<dt><kbd>${esc(key)}</kbd></dt><dd>${esc(what)}</dd>`).join('')}</dl></section>`).join('')}</div>
    <h3 class="sc-keys-sub">Codes</h3>
    <div class="sc-keys-grid">${areas.map(area => `<section><h3>${esc(area)}</h3><dl>${visibleCodes.filter(c => c.area === area).map(c =>
      `<dt><kbd>${esc(c.code)}</kbd></dt><dd>${esc(c.name)}</dd>`).join('')}</dl></section>`).join('')}</div>
    <div class="sc-keys-reports" hidden></div></div>`;
  box.querySelector('[data-sc="close"]').addEventListener('click', closeKeys);
  // Printed through the shell's invoice print area - the one thing its print
  // rules let onto paper (styles.css), as the tax invoice sheet does.
  box.querySelector('[data-sc="print"]').addEventListener('click', () => {
    const area = document.getElementById('invoice-print-area');
    if (!area) { window.print(); return; }
    area.replaceChildren(box.querySelector('.sc-keys-panel').cloneNode(true));
    area.classList.add('printing', 'sc-keys-card');
    window.print();
    area.classList.remove('printing', 'sc-keys-card');
    area.replaceChildren();
  });
  box.querySelector('[data-sc="reports"]').addEventListener('click', async (e) => {
    const host = box.querySelector('.sc-keys-reports');
    if (!host.hidden) { host.hidden = true; return; }
    const list = (await loadReports()).filter(r => r.number).sort((a, b) => a.number.localeCompare(b.number));
    const groups = [...new Set(list.map(r => r.number.charAt(1)))];
    host.innerHTML = `<h3 class="sc-keys-sub">Report numbers</h3><div class="sc-keys-grid">${groups.map(g =>
      `<section><h3>R${g}xxx · ${esc(REPORT_AREAS[g] || '')}</h3><dl>${list.filter(r => r.number.charAt(1) === g).map(r =>
        `<dt><kbd>${esc(r.number)}</kbd></dt><dd>${esc(r.label)}</dd>`).join('')}</dl></section>`).join('')}</div>`;
    host.hidden = false;
    e.currentTarget?.blur?.();
  });
  box.hidden = false;
  box.querySelector('[data-sc="close"]').focus();
}
function closeKeys() {
  const box = document.getElementById('sc-keys');
  if (box) box.hidden = true;
}

// ---------------------------------------------------------------------------
// After a screen draws: recents, the favourite star, key hints, remembered
// values, a kept draft, saved views
// ---------------------------------------------------------------------------

function currentDest() {
  if (currentView === 'doctype-table' && currentDoctype) return { t: 'setup', doctype: currentDoctype };
  if (!currentView || currentView === 'home') return null;
  return { t: 'view', route: currentViewTab ? `${currentView}:${currentViewTab}` : currentView };
}
function currentTitle(d) {
  if (d.t === 'view' && d.route.startsWith('reports:')) {
    const r = reportByRoute(d.route);
    if (r) return r.number ? `${r.number} ${r.label}` : r.label;
  }
  const h = [...document.querySelectorAll('#view-root h1')].find(shown);
  return h ? h.textContent.replace(/\s+/g, ' ').trim() : '';
}

function placeStar() {
  document.querySelectorAll('#view-root .sc-star').forEach(s => s.remove());
  const d = currentDest();
  // A screen's own title (h1.page-title), never a heading inside its content.
  const h = d && [...document.querySelectorAll('#view-root h1.page-title')].find(shown);
  if (!h) return;
  const on = !!(favs || []).find(f => destKey(f.dest) === destKey(d));
  const star = document.createElement('button');
  star.type = 'button';
  star.className = `sc-star${on ? ' on' : ''}`;
  star.textContent = on ? '★' : '☆';
  star.title = on ? 'Remove from favourites' : 'Add to favourites (Ctrl+K shows them)';
  star.setAttribute('aria-pressed', String(on));
  star.addEventListener('click', () => toggleFav(d, currentTitle(d)));
  // A plain page's title block stacks its children: the title and its star
  // share one row there. The h1 itself is left as it is (a setup detour reads
  // its text for the Back link).
  let row = h.parentElement;
  if (row.classList.contains('page-title-section')) {
    row = document.createElement('div');
    row.className = 'sc-title-row';
    h.replaceWith(row);
    row.appendChild(h);
  }
  if (row.classList.contains('sc-title-row')) row.appendChild(star);
  else h.insertAdjacentElement('afterend', star);
}

// Remembered values: the fields people pick the same way every time.
const REMEMBER = /(^|_|-)(store|location|warehouse|target_warehouse|from_location|to_location|payment_mode|payment_method|tender|counter|salesman|cost_center)$/i;
const screenKey = () => `${currentView}:${currentView === 'doctype-table' ? currentDoctype : (currentViewTab || '')}`;
const fieldKey = f => f.name || f.id || '';
function formHosts() {
  const hosts = [];
  const modal = document.querySelector('#dynamic-modal.open');
  if (modal && /^New /.test(document.getElementById('dynamic-modal-title')?.textContent || '')) hosts.push(modal);
  document.querySelectorAll('#view-root [data-landing]').forEach(screen => {
    const panes = [...screen.querySelectorAll(':scope > [data-pane]')];
    if (!panes.length) { if (!screen.classList.contains('txn-on-landing') && shown(screen)) hosts.push(screen); return; }
    panes.forEach(p => { if (p.dataset.pane !== screen.dataset.landing && shown(p)) hosts.push(p); });
  });
  return hosts;
}
function applyRemembered(host) {
  const saved = lsGet(`last:${screenKey()}`, {});
  host.querySelectorAll('select').forEach(f => {
    const k = fieldKey(f), want = saved[k];
    if (!REMEMBER.test(k) || !want || f.dataset.scRemembered) return;
    f.dataset.scRemembered = '1';
    const fill = () => {
      if (f.value || ![...f.options].some(o => o.value === want)) return false;
      f.value = want;
      f.classList.add('sc-remembered');
      f.title = 'Last used - change it if needed';
      f.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    };
    if (fill()) return;
    // A record form's lookups arrive a moment after it opens.
    const watch = new MutationObserver(() => { if (fill() || f.value) watch.disconnect(); });
    watch.observe(f, { childList: true });
    setTimeout(() => watch.disconnect(), 5000);
  });
}
document.addEventListener('change', (e) => {
  const f = e.target;
  if (!f || f.tagName !== 'SELECT' || !e.isTrusted || !REMEMBER.test(fieldKey(f)) || !f.value || f.value.startsWith('__')) return;
  if (!f.closest('#dynamic-modal, #view-root [data-landing]')) return;
  const k = `last:${screenKey()}`;
  lsSet(k, { ...lsGet(k, {}), [fieldKey(f)]: f.value });
  f.classList.remove('sc-remembered');
}, true);

// Drafts: a New form's own fields, kept while typing, offered back after a
// closed tab or a crash. Line tables are the screen's and are not kept.
const draftFields = host => [...host.querySelectorAll('input, select, textarea')].filter(f =>
  fieldKey(f) && !f.closest('table') && !/^(hidden|password|file|button|submit|checkbox|radio)$/.test(f.type || ''));
let draftTimer = 0;
document.addEventListener('input', (e) => {
  const host = e.target && e.target.closest && formHosts().find(h => h.contains(e.target));
  if (!host || !e.isTrusted || editing(host)) return;
  clearTimeout(draftTimer);
  draftTimer = setTimeout(() => {
    const values = {};
    draftFields(host).forEach(f => { if (f.value) values[fieldKey(f)] = f.value; });
    lsSet(`draft:${screenKey()}`, Object.keys(values).length ? { at: Date.now(), values } : null);
  }, 800);
}, true);
// An Edit page is off the list too, but its values are the record's own.
const editing = host => /^\s*Edit\b/.test((host.closest('[data-landing]')?.querySelector('.txn-page-only') || host.querySelector('h1, h2, .modal-title') || {}).textContent || '');
function offerDraft(host) {
  if (editing(host)) return;
  const draft = lsGet(`draft:${screenKey()}`, null);
  if (!draft || host.querySelector('.sc-draft') || Date.now() - draft.at > 7 * 86400000) return;
  const fields = draftFields(host);
  if (fields.some(f => f.value && !f.classList.contains('sc-remembered') && f.tagName !== 'SELECT')) return; // already being filled
  if (!fields.some(f => draft.values[fieldKey(f)] !== undefined)) return;
  const note = document.createElement('div');
  note.className = 'sc-draft';
  const when = new Date(draft.at).toLocaleString([], { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  note.innerHTML = `<span>Unsaved entry from ${esc(when)}</span><button type="button" class="btn btn-sm btn-primary">Restore</button><button type="button" class="btn btn-sm btn-outline">Discard</button>`;
  const [restore, discard] = note.querySelectorAll('button');
  restore.addEventListener('click', () => {
    draftFields(host).forEach(f => {
      const v = draft.values[fieldKey(f)];
      if (v === undefined) return;
      f.value = v;
      f.dispatchEvent(new Event('input', { bubbles: true }));
      f.dispatchEvent(new Event('change', { bubbles: true }));
    });
    note.remove();
  });
  discard.addEventListener('click', () => { lsSet(`draft:${screenKey()}`, null); note.remove(); });
  host.prepend(note);
}

// Saved views on a record list: a named search, kept as a UserShortcut.
const viewCache = new Map();
async function placeViews(fresh = false) {
  document.querySelectorAll('#view-root .sc-views').forEach(v => v.remove());
  if (currentView !== 'doctype-table' || !currentDoctype) return;
  const search = document.querySelector('#view-root .table-controls .search-box');
  if (!search) return;
  const doctype = currentDoctype;
  if (fresh || !viewCache.has(doctype)) {
    const res = await window.apiFetch(`/api/v1/doc/UserShortcut?owner=${encodeURIComponent(user())}&kind=List%20view&target=${encodeURIComponent(doctype)}&limit=100`).catch(() => null);
    if (!res || !res.ok) return;
    viewCache.set(doctype, await res.json());
  }
  const views = viewCache.get(doctype);
  if (currentDoctype !== doctype || document.querySelector('#view-root .sc-views')) return;
  const sel = document.createElement('select');
  sel.className = 'form-select sc-views';
  sel.setAttribute('aria-label', 'Saved views');
  const q = String(currentSearchQuery || '');
  const viewQ = v => { try { return JSON.parse(v.params || '{}').q || ''; } catch (_) { return ''; } };
  const current = views.find(v => viewQ(v).toLowerCase() === q.toLowerCase() && q);
  sel.innerHTML = `<option value="">${current ? esc(current.name) : 'All records'}</option>`
    + (current ? '<option value="@all">All records</option>' : '')
    + views.filter(v => v !== current).map(v => `<option value="${esc(v.id)}">${esc(v.name)}</option>`).join('')
    + (q && !current ? '<option value="@save">Save this search as a view…</option>' : '')
    + (current ? '<option value="@delete">Delete this view</option>' : '');
  sel.addEventListener('change', async () => {
    const v = sel.value;
    sel.value = '';
    if (v === '@save') {
      const name = ((await window.showCustomPrompt?.('Name for this view:', q)) || '').trim();
      if (!name) return;
      const r = await window.apiFetch('/api/v1/doc/UserShortcut', { method: 'POST',
        body: JSON.stringify({ kind: 'List view', name, target: doctype, params: JSON.stringify({ q }), owner: user(), status: 'Active' }) });
      if (r && !r.ok) await window.showApiError(r, 'Could not save the view.');
      return placeViews(true);
    }
    if (v === '@delete' && current) {
      const r = await window.apiFetch(`/api/v1/doc/UserShortcut/${encodeURIComponent(current.id)}`, { method: 'DELETE' });
      if (r && !r.ok) await window.showApiError(r, 'Could not delete the view.');
      return placeViews(true);
    }
    const view = views.find(x => x.id === v);
    applySearch(view ? viewQ(view) : '');
  });
  search.insertAdjacentElement('afterend', sel);
}
// A view runs through the list's own search box, as if typed.
function applySearch(q) {
  const box = document.getElementById('doc-table-search');
  if (!box) return;
  box.value = q;
  box.dispatchEvent(new Event('input', { bubbles: true }));
  setTimeout(() => placeViews(), 700);
}
// Typing a search offers to save it; the list redraws only its rows.
let viewsTimer = 0;
document.addEventListener('input', (e) => {
  if (e.target && e.target.id === 'doc-table-search' && e.isTrusted) { clearTimeout(viewsTimer); viewsTimer = setTimeout(() => placeViews(), 700); }
});

let afterTimer = 0;
function afterRender() {
  clearTimeout(afterTimer);
  afterTimer = setTimeout(async () => {
    const root = document.getElementById('view-root');
    tagButtons(root);
    const d = currentDest();
    if (d) recordRecent(d, currentTitle(d));
    await loadFavs();
    placeStar();
    placeViews();
    afterNewShown();
  }, 300);
}
function afterNewShown() {
  formHosts().forEach(h => { applyRemembered(h); offerDraft(h); tagButtons(h); });
}

// ---------------------------------------------------------------------------
// After a save: Print · New · Copy · the next step
// ---------------------------------------------------------------------------

// Records that are settings or logs, not work a person moves on from.
const QUIET = /^(UserShortcut|ReportFilterPreset|ReportColumnProfile|DashboardLayout|OMSSavedView|ReportExportJob|ReportRunLog|NotificationLog|HelpArticleFeedback|ContentAssistLog)$/;
let lastWrite = null, savedTimer = 0;

window.addEventListener('erp:write', (e) => {
  const { url, method, response } = e.detail || {};
  if (!response || !response.ok || !/^(POST|PUT)$/i.test(method || '')) return;
  const m = String(url).match(/^\/api\/v1\/doc\/([A-Za-z]+)(?:\/([^/?]+))?(?:\?|$)/);
  if (m && QUIET.test(m[1])) return;
  response.clone().json().then(body => {
    const id = body && typeof body === 'object' ? (body.id || body.document_id || body.number || '') : '';
    if (!id) return;
    lastWrite = { doctype: m ? m[1] : '', id: String(id), status: String(body.status || ''), at: Date.now(), generic: !!m && currentView === 'doctype-table' };
    lsSet(`draft:${screenKey()}`, null);
    // The record form saves through the record API and closes: show now. A
    // transaction screen says when its save is done (erp:txn-done).
    if (lastWrite.generic) { clearTimeout(savedTimer); savedTimer = setTimeout(() => showSaved(lastWrite), 350); }
  }).catch(() => {});
});
window.addEventListener('erp:txn-done', () => {
  setTimeout(() => {
    if (lastWrite && Date.now() - lastWrite.at < 15000) showSaved(lastWrite);
    lsSet(`draft:${screenKey()}`, null);
  }, 50);
});

function codeOfScreen() {
  const active = document.querySelector('.sidebar-menu .menu-item.active:not(.menu-item-group)');
  return codes.find(c => c.menu && active && c.menu === active.id) || null;
}

function showSaved(w) {
  if (currentView === 'pos' || !w) return;
  const screenCode = codeOfScreen();
  const doctype = w.doctype || (screenCode && screenCode.doctype) || '';
  const code = codes.find(c => c.doctype && c.doctype === doctype) || screenCode;
  const meta = (state.activeDoctypes || []).find(d => d.name === doctype);
  const isTxn = meta && meta.document_type === 'Transaction';
  // The next document only once this one can move on: a draft or one
  // waiting for approval is not received, invoiced or paid yet.
  const next = code && code.next && !/^(draft|pending|rejected|cancel)/i.test(w.status || '') && codeFor(code.next);
  let box = document.getElementById('sc-saved');
  if (!box) {
    box = document.createElement('div');
    box.id = 'sc-saved';
    box.setAttribute('role', 'status');
    document.body.appendChild(box);
  }
  box.innerHTML = `<span>Saved <b>${esc(w.id)}</b></span>
    ${isTxn ? '<button type="button" class="btn btn-sm btn-outline" data-sc="print">Print</button>' : ''}
    <button type="button" class="btn btn-sm btn-outline" data-sc="new">New</button>
    ${canCopy(doctype) ? '<button type="button" class="btn btn-sm btn-outline" data-sc="copy">Copy</button>' : ''}
    ${next && codeCanNew(next) ? `<button type="button" class="btn btn-sm btn-primary" data-sc="next">${esc(next.name)} →</button>` : ''}
    <button type="button" class="sc-saved-x" data-sc="close" aria-label="Close">×</button>`;
  const hide = () => { box.hidden = true; };
  box.hidden = false;
  box.querySelector('[data-sc="close"]').addEventListener('click', hide);
  box.querySelector('[data-sc="print"]')?.addEventListener('click', async () => {
    hide();
    const m = await import(`./trade-docs.js${V}`);
    m.printTransactionDocument(doctype, w.id);
  });
  box.querySelector('[data-sc="new"]').addEventListener('click', async () => {
    hide();
    const b = findButton('new', document.getElementById('view-root'));
    if (b) b.click(); else if (code) openCode(code, { fresh: true });
  });
  box.querySelector('[data-sc="copy"]')?.addEventListener('click', () => { hide(); copyRecord(doctype, w.id); });
  box.querySelector('[data-sc="next"]')?.addEventListener('click', () => {
    hide();
    // A PO is received against itself (its own Receive goods), not as a blank GRN.
    if (doctype === 'PurchaseOrder' && typeof window.startGRNFromPO === 'function') window.startGRNFromPO(w.id);
    else openCode(next, { fresh: true });
  });
  clearTimeout(box._t);
  box._t = setTimeout(hide, 15000);
}

// Copy: the record's own values into a New form - the Purchase Order through
// its composer (copyPurchaseOrder), any other record through the record form.
// Never the number, the status or the audit fields: those are the new one's.
const NOT_COPIED = /^(id|name|code|status|version|docstatus|workflow_state|created_at|updated_at|created_by|updated_by|modified_by|deleted_at|approved_by|approved_at|submitted_at|posted_at|po_number|grn_number|invoice_number|irn|ack_no|ack_date)$/;
function canCopy(doctype) {
  if (doctype === 'PurchaseOrder') return window.canCreateDoctype('PurchaseOrder');
  return !!doctype && !!(state.activeDoctypes || []).find(d => d.name === doctype) && window.canCreateDoctype(doctype)
    && (state.activeDocFields || []).length > 0 && currentView === 'doctype-table' && currentDoctype === doctype;
}
export async function copyRecord(doctype, id) {
  if (doctype === 'PurchaseOrder') {
    if (typeof window.copyPurchaseOrder !== 'function') { await window.loadViewModule('/view-procurement.js?v=1'); }
    if (currentView !== 'purchase-orders') { await openDest({ t: 'menu', id: 'menu-purchase-orders' }); await whenButton('new'); }
    return window.copyPurchaseOrder(id);
  }
  const res = await window.apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) { await window.showApiError(res, 'Could not load the record to copy.'); return; }
  const rec = await res.json();
  const defaults = {};
  Object.entries(rec).forEach(([k, v]) => { if (!NOT_COPIED.test(k) && v !== null && v !== '') defaults[k] = v; });
  if (currentView !== 'doctype-table' || currentDoctype !== doctype) await openDest({ t: 'setup', doctype });
  await window.openDynamicModal(undefined, defaults);
  window.showToast(`Copied from ${id}. Check it and save.`);
}

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

setupBar(document.getElementById('global-search'));
window.addEventListener('keydown', onKey);
const viewRoot = document.getElementById('view-root');
if (viewRoot) new MutationObserver(afterRender).observe(viewRoot, { childList: true });
const dynModal = document.getElementById('dynamic-modal');
if (dynModal) new MutationObserver(() => { if (dynModal.classList.contains('open')) setTimeout(afterNewShown, 150); }).observe(dynModal, { attributes: true, attributeFilter: ['class'] });
// A New button shows its page in place (txn-screen.js): look again after it.
document.addEventListener('click', (e) => {
  const b = e.target && e.target.closest && e.target.closest('button, a.btn');
  if (b && (BUTTONS.new.test(btnText(b)) || /:new"/.test(b.dataset.actArgs || '') || b.id === 'doc-create-button')) setTimeout(afterNewShown, 300);
});
window.addEventListener('popstate', () => setTimeout(afterNewShown, 300));
afterRender();

export { openKeys as openShortcutKeys };
