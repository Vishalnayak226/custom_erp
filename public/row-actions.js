// 276.5: a list row's actions on one line. The one or two actions that fit
// the row's status stay as buttons (the caller draws those); the rest go in
// a "⋯" menu drawn by rowMenu().
//
// The menu is a native popover (popover="auto"): it shows above the table's
// sideways clip and above sticky cells, closes on Escape or a click
// elsewhere, and its buttons stay inside the row in the DOM, so app.js's
// data-act dispatcher and a list's own data-doc-action listener still reach
// them. Without popover support (an old browser) the items are drawn as
// plain buttons, as before.
//
//   rowMenu([
//     {label: 'Edit', act: ['amendPurchaseOrder', [po.id]]},
//     canCopy && {label: 'Copy', act: ['copyPurchaseOrder', [po.id]], title: '...'},
//     {label: 'Delete', attrs: 'data-doc-action="delete" data-doc-id="..."', danger: true},
//   ])
//
// An item is {label, act: [fn, args]} or {label, attrs: '<escaped data
// attributes>'}, with optional danger and title. A falsy item is skipped; a
// menu of one item is drawn as a plain button. Styles: shell-steady.css
// (.row-menu) and styles.css (.account-popover-item, the user menu's rows).

const esc = s => window.escapeHTMLText(String(s ?? ''));
const POPOVER = typeof HTMLElement !== 'undefined' && Object.prototype.hasOwnProperty.call(HTMLElement.prototype, 'popover');
// A module imported under two query strings runs twice; ids stay unique and
// the document listeners are bound once.
const prefix = 'rm' + Math.random().toString(36).slice(2, 7);
let seq = 0;

function itemAttrs(item) {
  if (item.act) return window.actionAttrs(item.act[0], item.act[1] || []);
  return item.attrs || '';
}

function plainButton(item) {
  return `<button type="button" class="action-btn${item.danger ? ' action-btn-danger' : ''}"${item.title ? ` title="${esc(item.title)}"` : ''} ${itemAttrs(item)}>${esc(item.label)}</button>`;
}

export function rowMenu(items) {
  const list = (items || []).filter(Boolean);
  if (!list.length) return '';
  if (list.length === 1 || !POPOVER) return list.map(plainButton).join('');
  const id = `${prefix}-${++seq}`;
  return `<button type="button" class="action-btn row-menu-btn" popovertarget="${id}" aria-haspopup="menu" aria-label="More actions" title="More actions">⋯</button>`
    + `<div id="${id}" popover="auto" class="row-menu" role="menu">${list.map(item =>
      `<button type="button" role="menuitem" class="account-popover-item row-menu-item${item.danger ? ' account-popover-item-danger' : ''}"${item.title ? ` title="${esc(item.title)}"` : ''} ${itemAttrs(item)}>${esc(item.label)}</button>`).join('')}</div>`;
}

// Place an open menu under its "⋯" button, right edges lined up; above it
// when there is no room below; always inside the window.
function place(menu) {
  const btn = document.querySelector(`[popovertarget="${menu.id}"]`);
  if (!btn) return;
  const r = btn.getBoundingClientRect();
  const w = menu.offsetWidth, h = menu.offsetHeight, gap = 4, edge = 8;
  const left = Math.max(edge, Math.min(r.right - w, innerWidth - w - edge));
  let top = r.bottom + gap;
  if (top + h > innerHeight - edge) top = r.top - h - gap >= edge ? r.top - h - gap : Math.max(edge, innerHeight - h - edge);
  menu.style.left = `${left}px`;
  menu.style.top = `${top}px`;
  menu.classList.add('row-menu-placed');
}

function openMenus() {
  return document.querySelectorAll('.row-menu:popover-open');
}

function closeMenu(menu) {
  try { if (menu.matches(':popover-open')) menu.hidePopover(); } catch (_) { /* already detached */ }
}

if (POPOVER && !window.rowMenuWired) {
  window.rowMenuWired = true;
  document.addEventListener('beforetoggle', e => {
    if (e.newState === 'open' && e.target.classList?.contains('row-menu')) e.target.classList.remove('row-menu-placed');
  }, true);
  document.addEventListener('toggle', e => {
    if (e.newState !== 'open' || !e.target.classList?.contains('row-menu')) return;
    place(e.target);
    e.target.querySelector('.row-menu-item')?.focus({ preventScroll: true });
  }, true);
  // An action inside the menu closes it; the action's own handler has run by
  // then (the list's listener sits below document, app.js's was bound first).
  document.addEventListener('click', e => {
    const item = e.target.closest?.('.row-menu-item');
    if (item) closeMenu(item.closest('.row-menu'));
  });
  // The menu is placed against the window: a scroll or resize under it
  // would leave it floating away from its row, so it closes instead.
  document.addEventListener('scroll', e => {
    for (const m of openMenus()) if (!m.contains(e.target)) closeMenu(m);
  }, true);
  addEventListener('resize', () => openMenus().forEach(closeMenu));
  document.addEventListener('keydown', e => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    const menu = e.target.closest?.('.row-menu');
    if (!menu) return;
    const items = [...menu.querySelectorAll('.row-menu-item')];
    const at = items.indexOf(e.target);
    items[(at + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length]?.focus();
    e.preventDefault();
  });
}
