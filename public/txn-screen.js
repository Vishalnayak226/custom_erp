// Stage 68: the shared pieces of a transaction screen's bar (layout in
// txn-screen.css). Each transaction view imports this with its own ?v=, so a
// deploy refreshes both together.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));

// sessionStore is the store this session works at. The server's own answer
// (view-store.js, from /api/v1/me/stores) wins once it is in; before that the
// token's `loc` claim (base64 of "id=..&user=..&loc=.."). Display and routing
// only - the server re-checks everything it is sent.
export function sessionStore() {
  if (window.erpWorkingStore && window.erpWorkingStore.code) return window.erpWorkingStore.code;
  try {
    const claims = (localStorage.getItem('erp_token') || '').split('.')[0];
    return new URLSearchParams(atob(claims.replace(/-/g, '+').replace(/_/g, '/'))).get('loc') || '';
  } catch (_) {
    return '';
  }
}

// txnContext is the "User/Store detail in top" chip: who is working and where.
// store overrides the session's store where the screen is bound to its own
// (the POS till). Stage 83 (user, 2026-10-18): "No need to show superadmin in
// top. As it will be shown in profile." - the role is in the profile menu and
// this chip's hover text, not on the chip.
export function txnContext(store) {
  const user = localStorage.getItem('erp_username') || '';
  const role = localStorage.getItem('erp_role') || '';
  // Stage 85.A5: the working store's name once view-store.js has it.
  const code = sessionStore(), w = window.erpWorkingStore;
  const where = store || (w && w.code === code && w.name) || code;
  return `<span class="txn-ctx" title="Signed in as ${esc(user)}${role ? ` (${esc(role)})` : ''}"><b>${esc(user)}</b>${where ? ` · <span class="txn-ctx-store">${esc(where)}</span>` : ''}</span>`;
}

// txnTabs renders New / History style page tabs; fn is the window function
// the global data-act dispatcher calls with the tab's key.
export function txnTabs(fn, active, tabs) {
  return `<div class="txn-tabs" role="tablist">${tabs.map(([key, label]) =>
    `<button type="button" role="tab" class="txn-tab${key === active ? ' active' : ''}" aria-selected="${key === active}" ${window.actionAttrs(fn, [key])}>${label}</button>`).join('')}</div>`;
}

// Stage 69 (the user: "All txn where there is create and history both will be
// different page ... Make it standard practice how it is in best erp being
// followed"): the shape Odoo, ERPNext, Zoho and SAP Fiori share. A record
// opens on its list (the history) with + New in the bar; New is a page of its
// own with a breadcrumb back to the list. A screen opts in with
// data-landing="<list pane>". Each page is a browser history entry on the
// same address, so Back leaves New for the list without a re-render (the hash
// does not change, so the router stays out of it) and work being keyed in
// survives a look at the list. history.state is the one record of which page
// shows: a screen re-rendered on a New entry (a setup detour returning, a
// refresh) comes back on New; a save ends with txnDone(), back on the list.
const lastPane = {};
const forceLanding = {};
const pageHandlers = {};

function statePane(screenId) {
  const t = history.state && history.state.txnPane;
  return t && t.startsWith(`${screenId}:`) ? t.slice(screenId.length + 1) : '';
}

// showPage applies a page without touching history. target's screen
// [data-pane] children show only the named one, its tabs follow, the bar's
// .txn-main-only parts (a total, Post) hide off the first pane, and on a
// paged screen .txn-landing-only / .txn-page-only follow the list.
function showPage(screenId, pane) {
  const screen = document.getElementById(screenId);
  if (!screen) return;
  lastPane[screenId] = pane;
  if (pageHandlers[screenId]) { pageHandlers[screenId](pane); return; }
  const panes = [...screen.querySelectorAll(':scope > [data-pane]')];
  panes.forEach(p => p.classList.toggle('hidden', p.dataset.pane !== pane));
  screen.classList.toggle('txn-aside', !!panes[0] && panes[0].dataset.pane !== pane);
  if (screen.dataset.landing) screen.classList.toggle('txn-on-landing', pane === screen.dataset.landing);
  screen.querySelectorAll('.txn-tab').forEach(t => {
    const on = (t.dataset.actArgs || '').includes(`:${pane}"`);
    t.classList.toggle('active', on);
    t.setAttribute('aria-selected', String(on));
  });
}

// txnShowPane switches a screen's panes in place - for screens whose work in
// progress lives in the page (GRN lines, the POS cart's fields), where a
// re-render would lose it. target is "<screen id>:<pane>". On a paged screen
// it also moves history: a page off the list is pushed, the list is Back.
window.txnShowPane = function(target) {
  const [screenId, pane] = String(target).split(':');
  const screen = document.getElementById(screenId);
  if (!screen) return;
  const landing = screen.dataset.landing;
  if (landing) {
    const current = statePane(screenId);
    try {
      if (pane === landing) {
        // Back to the list entry this page was pushed from. The popstate that
        // follows finds the page already applied; a re-render meanwhile (a
        // save) reads forceLanding instead of the not-yet-popped state.
        if (current) { forceLanding[screenId] = true; history.back(); }
      } else if (!current) {
        // On the list entry, so no Back is pending: a leftover flag is stale.
        delete forceLanding[screenId];
        history.pushState({ txnPane: target }, '', location.href);
      } else if (current !== pane) {
        history.replaceState({ ...history.state, txnPane: target }, '', location.href);
      }
    } catch (_) { /* a sandboxed frame - the page still switches */ }
  }
  showPage(screenId, pane);
};

window.addEventListener('popstate', () => {
  // The Back txnShowPane started has landed: clear its forceLanding for every
  // screen, not just those in #view-root - a re-render is often mid-flight here
  // (renderView builds off-root while it fetches), and a flag left set made
  // every later + New render the list (the PO screen's dead + New, 2026-10-17).
  Object.keys(forceLanding).forEach(id => { if (!statePane(id)) delete forceLanding[id]; });
  document.querySelectorAll('#view-root [data-landing]').forEach(screen => {
    const pane = statePane(screen.id);
    const want = pane || screen.dataset.landing;
    if (lastPane[screen.id] !== want) showPage(screen.id, want);
  });
});

// txnPageOf is the page a paged screen renders on: New on a New entry, else
// its list.
export function txnPageOf(screenId, landing) {
  const pane = forceLanding[screenId] ? '' : statePane(screenId);
  return (lastPane[screenId] = pane || landing);
}

// txnOnPage hands a paged screen's page switches to fn instead of toggling
// panes - for a screen that re-renders per page (the Purchase Order).
export function txnOnPage(screenId, fn) { pageHandlers[screenId] = fn; }

// txnDone ends a save on a paged screen: back to the list, where the new
// record now shows. Called before the screen re-renders.
export function txnDone(screenId) {
  const screen = document.getElementById(screenId);
  if (screen && screen.dataset.landing) window.txnShowPane(`${screenId}:${screen.dataset.landing}`);
}

// txnRestorePane puts a re-rendered screen on its page: a paged screen on the
// page its history entry names; others back on the pane they were showing (a
// Return done on POS's Return tab stays there).
export function txnRestorePane(screenId) {
  const screen = document.getElementById(screenId);
  if (screen && screen.dataset.landing) showPage(screenId, txnPageOf(screenId, screen.dataset.landing));
  else if (lastPane[screenId]) showPage(screenId, lastPane[screenId]);
}

// txnPageTitle is a paged screen's title: on the list, its name and how many
// records it holds; on New, a breadcrumb back to the list and the page's own
// title. The list's h1 comes first and keeps .page-title, the name a setup
// detour reads for its "Back to ..." link.
// A section inside a page (an HR tab) passes heading 'h2'; a page with
// several New pages passes no page and names each inside its form.
export function txnPageTitle({ screen, title, page, count, landing = 'history', heading = 'h1' }) {
  const h = heading === 'h1' ? 'h1' : 'h2';
  return `<div class="txn-titlebox txn-landing-only"><${h} class="${h === 'h1' ? 'page-title ' : ''}txn-title">${title}</${h}>${count == null ? '' : `<span class="txn-count">${count}</span>`}</div>
    <div class="txn-titlebox txn-page-only"><button type="button" class="txn-crumb" ${window.actionAttrs('txnShowPane', [`${screen}:${landing}`])}>${title}</button>${page ? `<span class="txn-crumb-sep" aria-hidden="true">›</span><${h} class="txn-title">${page}</${h}>` : ''}</div>`;
}

// txnNewButton is the list page's + New.
export function txnNewButton({ screen, label, page = 'new', id = '', primary = true }) {
  return `<button type="button" class="btn ${primary ? 'btn-primary' : 'btn-outline'} txn-landing-only"${id ? ` id="${id}"` : ''} ${window.actionAttrs('txnShowPane', [`${screen}:${page}`])}>+ ${label}</button>`;
}

// txnPagedSection makes an ordinary page's form panel and list panel the two
// pages of one record: the list (landing) under a bar with its count and
// + New, the form as the New page under a breadcrumb back to the list.
// forms maps further New pages ({ pane: [label, element] }) for a list fed by
// several forms (My Requests). Call txnRestorePane(id) once it is attached.
export function txnPagedSection({ id, title, page, count, newLabel, form, list, forms, heading = 'h1', extra = '' }) {
  const section = document.createElement('div');
  section.id = id;
  section.className = 'txn-paged txn-on-landing';
  section.dataset.landing = 'list';
  const pages = forms || { new: [newLabel, form] };
  section.innerHTML = `<div class="page-section-bar">${txnPageTitle({ screen: id, title, page, count, landing: 'list', heading })}
    <span class="page-section-actions txn-landing-only">${extra}${Object.entries(pages).map(([pane, [label]], i) =>
      txnNewButton({ screen: id, label, page: pane, primary: i === 0 })).join('')}</span></div>`;
  const listPane = document.createElement('div');
  listPane.dataset.pane = 'list';
  listPane.appendChild(list);
  section.appendChild(listPane);
  Object.entries(pages).forEach(([pane, [, el]]) => {
    const p = document.createElement('div');
    p.dataset.pane = pane;
    p.className = 'hidden';
    p.appendChild(el);
    section.appendChild(p);
  });
  return section;
}

// A view importing this module gets its stylesheet too, though its registry
// entry may name none (HR, Expenses...); awaited, so it never paints unstyled.
if (!document.querySelector('link[href*="txn-screen.css"]')) {
  await new Promise(done => {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = `/txn-screen.css${new URL(import.meta.url).search}`;
    link.onload = link.onerror = done;
    document.head.appendChild(link);
  });
}

// txnMoney formats an amount the way every total in the bar reads.
export function txnMoney(v) {
  return typeof window.formatMoney === 'function' ? window.formatMoney(Number(v) || 0) : (Number(v) || 0).toFixed(2);
}
