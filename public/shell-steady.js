// Stage 152.3 / 152.4: a steady shell.
// - On leaving the page (F5, Refresh of the browser, closing the tab) the
//   menu's shape and the screen are kept in this tab's sessionStorage, and
//   app.js paints them back before anything has loaded (paintLastShell), so a
//   reload does not look like one.
// - A screen redrawn in place (the Refresh button, a kept re-render) keeps its
//   inner scroll positions and focus, and only the parts whose text changed
//   flash (renderView calls steadyMemo before the swap, steadyApply after).
// Loaded once signed in (app.js); exports land on window.

const V = new URL(import.meta.url).search;
const KEY = 'erp_shell';
const MAX_VIEW = 800000; // characters; a bigger screen is redrawn, not kept

if (!document.querySelector('link[href*="shell-steady.css"]')) {
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = `/shell-steady.css${V}`;
  document.head.appendChild(link);
}

// The saved copy has been painted by now (paintLastShell runs before init):
// its inner lists go back where they were scrolled, then it is dropped - it
// must not sit in the tab's storage quota for the whole session.
try {
  const s = JSON.parse(sessionStorage.getItem(KEY) || 'null'), root = document.getElementById('view-root');
  if (s?.sc && root?.getAttribute('aria-busy')) placeScrolls(s.sc, root);
  // The top bar's store, as it was, until mountStoreSwitch has asked the
  // server (about a second): it needs store.css for its width.
  const host = document.getElementById('store-switch');
  if (s?.store && host && s.who === localStorage.getItem('erp_username')) {
    window.loadViewStyles?.('store.css').then(() => {
      if (host.classList.contains('hidden')) { host.innerHTML = s.store; host.classList.remove('hidden'); }
    });
  }
  const ver = document.getElementById('app-version-line');
  if (s?.ver && ver?.classList.contains('hidden')) { ver.textContent = s.ver; ver.classList.remove('hidden'); }
  sessionStorage.removeItem(KEY);
} catch (e) { /* storage off */ }

addEventListener('pagehide', () => {
  try {
    const app = document.getElementById('app-root'), root = document.getElementById('view-root');
    if (!app || app.classList.contains('hidden') || !root) { sessionStorage.removeItem(KEY); return; }
    let view = root.getAttribute('aria-busy') ? '' : root.innerHTML;
    if (view.length > MAX_VIEW) view = '';
    // Object URLs die with the page; the redraw brings the pictures back.
    view = view.replace(/\ssrc="blob:[^"]*"/g, '');
    // 200.2: the page's CSP drops a style="" read back from HTML, so a screen
    // that set its own padding painted without it and jumped when the real one
    // came; paintLastShell puts these back through the style object.
    view = view.replace(/(<[a-z][^<>]*?\s)style="/gi, '$1data-snap-style="');
    const shell = {
      who: localStorage.getItem('erp_username'),
      hash: location.hash,
      li: [...document.querySelectorAll('.sidebar-menu > li')].map(li => li.className),
      act: document.querySelector('.sidebar-menu > li > a.active')?.id || '',
      view,
      sc: view ? scrollsOf(root) : [],
      store: document.querySelector('#store-switch:not(.hidden)')?.innerHTML || '',
      ver: document.querySelector('#app-version-line:not(.hidden)')?.textContent || ''
    };
    try {
      sessionStorage.setItem(KEY, JSON.stringify(shell));
    } catch (e) {
      shell.view = ''; // over the quota: keep the menu at least
      sessionStorage.setItem(KEY, JSON.stringify(shell));
    }
  } catch (e) { /* storage off - the reload just draws as before */ }
});

// The child-index path from root to el.
function pathOf(el, root) {
  const p = [];
  for (let n = el; n && n !== root; n = n.parentElement) {
    if (!n.parentElement) return null;
    p.unshift(Array.prototype.indexOf.call(n.parentElement.children, n));
  }
  return p;
}

function atPath(root, p, tag) {
  let n = root;
  for (const i of p) { n = n?.children[i]; if (!n) return null; }
  return n.tagName === tag ? n : null;
}

function scrollsOf(root) {
  const out = [];
  for (const el of root.querySelectorAll('*')) {
    if (el.scrollTop || el.scrollLeft) {
      const p = pathOf(el, root);
      if (p) out.push({ p, tag: el.tagName, top: el.scrollTop, left: el.scrollLeft });
    }
  }
  return out;
}

function placeScrolls(list, root) {
  for (const s of list) {
    const el = atPath(root, s.p, s.tag);
    if (el) { el.scrollTop = s.top; el.scrollLeft = s.left; }
  }
}

// Read while the old screen is still attached: a detached element reports a
// scroll of 0.
export function steadyMemo(root) {
  const scrolls = scrollsOf(root);
  let focus = null;
  const a = document.activeElement;
  if (a && a !== root && root.contains(a)) {
    focus = { p: pathOf(a, root), tag: a.tagName };
    try { focus.s = a.selectionStart; focus.e = a.selectionEnd; } catch (e) { /* not a text box */ }
  }
  return { scrolls, focus, old: [...root.children] };
}

const SKIP = new Set(['INPUT', 'SELECT', 'TEXTAREA', 'OPTION', 'SCRIPT', 'STYLE']);
const FLASH_LIMIT = 60; // more than this many changes is a new screen, not an update

// Walks the old and new trees side by side; a leaf whose text differs, or a
// child that was not there before, is a change.
function diff(a, b, out, count) {
  if (SKIP.has(b.tagName)) return;
  // The title's ☆ row (shortcuts.js) is added just after this runs.
  if (a.classList.contains('sc-title-row')) a = a.firstElementChild || a;
  if (a.tagName !== b.tagName) { out.push(b); return; }
  const ac = a.children, bc = b.children;
  if (!bc.length) {
    count.leaves++;
    const t = b.textContent.trim();
    // A code still waiting for its name (the name's tooltip is the code).
    if (ac.length || (a.textContent.trim() !== t && a.title !== t)) out.push(b);
    return;
  }
  for (let i = 0; i < bc.length; i++) {
    if (out.length > FLASH_LIMIT) return;
    if (i < ac.length) diff(ac[i], bc[i], out, count);
    else out.push(bc[i]);
  }
}

export function steadyApply(memo, root) {
  if (!memo) return;
  placeScrolls(memo.scrolls, root);
  if (memo.focus?.p) {
    const el = atPath(root, memo.focus.p, memo.focus.tag);
    if (el && document.activeElement !== el) {
      el.focus({ preventScroll: true });
      try { if (memo.focus.s != null) el.setSelectionRange(memo.focus.s, memo.focus.e); } catch (e) { /* not a text box */ }
    }
  }
  const out = [], count = { leaves: 0 }, now = root.children;
  for (let i = 0; i < now.length && out.length <= FLASH_LIMIT; i++) {
    if (i < memo.old.length) diff(memo.old[i], now[i], out, count);
  }
  // Nothing changed: nothing moves. Most of it changed: it is a different
  // screen, and a flash everywhere would be the blink this is here to stop.
  if (!out.length || out.length > FLASH_LIMIT || out.length > count.leaves / 4) return;
  for (const el of out) {
    el.classList.add('ui-changed');
    el.addEventListener('animationend', () => el.classList.remove('ui-changed'), { once: true });
  }
}
