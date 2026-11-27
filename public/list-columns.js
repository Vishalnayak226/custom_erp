// 276.2: choose a list's columns - each user their own, and an admin or a
// Store Manager the default for everyone. Kept as report column profiles
// (Stage 28.3's ReportColumnProfile) with report_id "list:<list>": scope
// Personal (owner = the user) or Universal. The server keeps each user to
// their own (engines.ColumnProfileWriteAllowed).
//
// Two kinds of list use it:
//  - the shared record list (view-documents.js) draws the chosen fields:
//      const choice = await loadColumnChoice(doctype);
//      const keys = chosenKeys(choice, allKeys, defaultKeys);
//  - a screen that draws its own table (Purchase Orders, Users, Chart of
//    Accounts) still draws every column; tableColumns() then hides and orders
//    the cells, rows drawn later included, and returns the Columns button.
//
// The chooser: tick a column to show it, arrows to move it. The choice shows
// at once and is saved for the user when the chooser closes. Reset goes back
// to the default; Save for everyone (admin, Store Manager) sets the default.
// Styles: shell-steady.css (.col-chooser).

const PROFILE = '/api/v1/doc/ReportColumnProfile';
const esc = s => window.escapeHTMLText(String(s ?? ''));
const cache = new Map(); // listId -> Promise<{mine, everyone}>
const me = () => localStorage.getItem('erp_username') || '';

function canSetDefault() {
  const role = localStorage.getItem('erp_role') || '';
  return isAdminRoleName(role) || role === 'Store Manager';
}

// loadColumnChoice: this user's own choice and the default for everyone
// (either may be null). Read once a session per list; a save updates it.
export function loadColumnChoice(listId) {
  if (!cache.has(listId)) cache.set(listId, fetchChoice(listId));
  return cache.get(listId);
}

async function fetchChoice(listId) {
  const none = { mine: null, everyone: null };
  try {
    const res = await apiFetch(`${PROFILE}?report_id=${encodeURIComponent('list:' + listId)}&limit=50`);
    if (!res || !res.ok) return none;
    const rows = (await res.json()).filter(p => p.status !== 'Inactive')
      .sort((a, b) => String(b.id).localeCompare(String(a.id))); // newest first
    return {
      mine: rows.find(p => p.scope !== 'Universal' && p.owner === me()) || null,
      everyone: rows.find(p => p.scope === 'Universal') || null,
    };
  } catch (_) {
    return none;
  }
}

function savedColumns(profile) {
  if (!profile) return null;
  try {
    const cols = typeof profile.columns === 'string' ? JSON.parse(profile.columns) : profile.columns;
    return Array.isArray(cols) && cols.length ? cols : null;
  } catch (_) {
    return null;
  }
}

// chosenKeys: the keys to show, in order - the user's own choice, else the
// default for everyone, else defaultKeys. A column the choice does not know
// (added to the list since) stays off; a choice that shows nothing is ignored.
export function chosenKeys(choice, allKeys, defaultKeys) {
  const known = new Set(allKeys);
  for (const saved of [savedColumns(choice?.mine), savedColumns(choice?.everyone)]) {
    if (!saved) continue;
    const keys = saved.filter(c => c && c.visible !== false && known.has(c.key)).map(c => c.key);
    if (keys.length) return [...new Set(keys)];
  }
  return defaultKeys;
}

async function saveChoice(listId, scope, shown, allKeys) {
  const choice = await loadColumnChoice(listId);
  const existing = scope === 'Universal' ? choice.everyone : choice.mine;
  const columns = JSON.stringify([
    ...shown.map(key => ({ key, visible: true })),
    ...allKeys.filter(k => !shown.includes(k)).map(key => ({ key, visible: false })),
  ]);
  const record = {
    report_id: 'list:' + listId, name: scope === 'Universal' ? 'Default for everyone' : 'My columns',
    owner: me(), scope, columns, status: 'Active',
  };
  const id = existing ? existing.id : `RCP-${Date.now()}`;
  const res = await apiFetch(existing ? `${PROFILE}/${encodeURIComponent(id)}` : PROFILE, {
    method: 'POST', body: JSON.stringify(existing ? record : { id, ...record }),
  });
  if (!res) return false;
  if (!res.ok) {
    await showApiError(res, 'Your columns were not saved.');
    return false;
  }
  const next = { ...choice, [scope === 'Universal' ? 'everyone' : 'mine']: { ...record, id } };
  cache.set(listId, Promise.resolve(next));
  return true;
}

async function dropMine(listId) {
  const choice = await loadColumnChoice(listId);
  if (!choice.mine) return true;
  const res = await apiFetch(`${PROFILE}/${encodeURIComponent(choice.mine.id)}`, { method: 'DELETE' });
  if (!res) return false;
  if (!res.ok && res.status !== 404) {
    await showApiError(res, 'Your columns were not reset.');
    return false;
  }
  cache.set(listId, Promise.resolve({ ...choice, mine: null }));
  return true;
}

// columnsButtonHTML: the toolbar button that opens the chooser.
export function columnsButtonHTML(id) {
  return `<button type="button" class="btn btn-outline" id="${esc(id)}" aria-haspopup="dialog" title="Choose columns">`
    + '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16M15 4v16"/></svg>'
    + '<span>Columns</span></button>';
}

// openColumnChooser: columns is [{key, label}] in the list's own order; shown
// the keys on screen now, in order; defaultKeys what Reset shows when there is
// no default for everyone. onChange(keys) redraws the list.
export async function openColumnChooser(button, { listId, columns, shown, defaultKeys, onChange }) {
  document.querySelector('.col-chooser')?.remove();
  const labels = new Map(columns.map(c => [c.key, c.label]));
  const allKeys = columns.map(c => c.key);
  let order = [...shown.filter(k => labels.has(k)), ...allKeys.filter(k => !shown.includes(k))];
  const on = new Set(shown);
  const visible = () => order.filter(k => on.has(k));
  const start = visible().join('\n');
  let settled = false; // saved, reset or saved for everyone - nothing left to save on close

  const panel = document.createElement('div');
  panel.className = 'col-chooser';
  panel.setAttribute('popover', 'auto');
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', 'Columns');
  const draw = () => {
    const last = on.size === 1;
    panel.innerHTML = `<div class="col-chooser-list">${order.map((k, i) => `
      <div class="col-chooser-row">
        <label><input type="checkbox" data-cc-key="${esc(k)}"${on.has(k) ? ' checked' : ''}${last && on.has(k) ? ' disabled' : ''}> ${esc(labels.get(k))}</label>
        <button type="button" class="action-btn" data-cc-move="-1" data-cc-at="${i}" aria-label="Move ${esc(labels.get(k))} up"${i === 0 ? ' disabled' : ''}>&uarr;</button>
        <button type="button" class="action-btn" data-cc-move="1" data-cc-at="${i}" aria-label="Move ${esc(labels.get(k))} down"${i === order.length - 1 ? ' disabled' : ''}>&darr;</button>
      </div>`).join('')}</div>
      <div class="col-chooser-foot">
        <button type="button" class="btn btn-outline btn-sm" data-cc="reset">Reset</button>
        ${canSetDefault() ? '<button type="button" class="btn btn-outline btn-sm" data-cc="everyone" title="The columns everyone sees until they choose their own">Save for everyone</button>' : ''}
        <button type="button" class="btn btn-primary btn-sm" data-cc="done">Done</button>
      </div>`;
  };
  draw();

  panel.addEventListener('change', e => {
    const k = e.target.dataset?.ccKey;
    if (k === undefined) return;
    if (e.target.checked) on.add(k); else on.delete(k);
    draw();
    panel.querySelector(`[data-cc-key="${CSS.escape(k)}"]`)?.focus();
    onChange(visible());
  });
  panel.addEventListener('click', async e => {
    const move = e.target.closest('[data-cc-move]');
    if (move) {
      const i = Number(move.dataset.ccAt), j = i + Number(move.dataset.ccMove);
      [order[i], order[j]] = [order[j], order[i]];
      draw();
      panel.querySelector(`[data-cc-move="${move.dataset.ccMove}"][data-cc-at="${j}"]`)?.focus();
      if (on.has(order[i]) && on.has(order[j])) onChange(visible());
      return;
    }
    const act = e.target.closest('[data-cc]')?.dataset.cc;
    if (act === 'done') panel.hidePopover();
    if (act === 'everyone') {
      if (!await saveChoice(listId, 'Universal', visible(), allKeys)) return;
      // The one who sets the default sees it too.
      await dropMine(listId);
      settled = true;
      showToast('Columns saved for everyone.', { variant: 'success' });
      panel.hidePopover();
    }
    if (act === 'reset') {
      if (!await dropMine(listId)) return;
      settled = true;
      const choice = await loadColumnChoice(listId);
      onChange(chosenKeys(choice, allKeys, defaultKeys));
      panel.hidePopover();
    }
  });
  panel.addEventListener('toggle', e => {
    if (e.newState !== 'closed') return;
    panel.remove();
    button.setAttribute('aria-expanded', 'false');
    button.focus({ preventScroll: true });
    if (!settled && visible().join('\n') !== start) saveChoice(listId, 'Personal', visible(), allKeys);
  });

  document.body.appendChild(panel);
  panel.showPopover();
  button.setAttribute('aria-expanded', 'true');
  // Under the button, left edges lined up; above it when there is no room;
  // always inside the window.
  const r = button.getBoundingClientRect();
  const w = panel.offsetWidth, h = panel.offsetHeight, edge = 8, gap = 4;
  const left = Math.max(edge, Math.min(r.left, innerWidth - w - edge));
  let top = r.bottom + gap;
  if (top + h > innerHeight - edge) top = r.top - h - gap >= edge ? r.top - h - gap : Math.max(edge, innerHeight - h - edge);
  panel.style.left = `${left}px`;
  panel.style.top = `${top}px`;
  panel.querySelector('input:not([disabled])')?.focus({ preventScroll: true });
}

// tableColumns: a screen that draws its own <table> calls this after each
// draw. Every header cell with text is a column (its text is its key); the
// tick-box column and an empty-headed actions column stay where they are.
// off is the header texts hidden by default. Returns {open(button)} for the
// screen's Columns button, or null when the table has no header row.
export async function tableColumns(table, listId, off = []) {
  const head = table?.tHead?.rows[0];
  if (!head) return null;
  // In the order drawn, even when an earlier call has already moved them.
  const cells = [...head.cells]
    .map((c, i) => [c.dataset.colI === undefined ? i : Number(c.dataset.colI), c])
    .sort((a, b) => a[0] - b[0]).map(x => x[1]);
  const n = cells.length;
  const keyOf = c => (c.dataset.colKey ?? c.textContent).trim();
  // A fixed cell: no title (actions), a tick box, the sticky actions column,
  // or one marked data-col-fixed (an actions column with a title).
  const fixed = new Set(cells.map((c, i) => (!keyOf(c) || c.querySelector('input') || c.classList.contains('row-actions-cell') || c.hasAttribute('data-col-fixed') ? i : -1)).filter(i => i >= 0));
  const columns = cells.map((c, i) => ({ key: keyOf(c), i })).filter(c => !fixed.has(c.i));
  const indexOf = new Map(columns.map(c => [c.key, c.i]));
  const allKeys = columns.map(c => c.key);
  const defaultKeys = allKeys.filter(k => !off.includes(k));

  const arrange = (row, shownIdx, hiddenIdx) => {
    const now = [...row.cells];
    if (now.length !== n) return; // an empty-state row spans the table
    const byI = [];
    now.forEach((c, i) => {
      if (c.dataset.colI === undefined) c.dataset.colI = String(i);
      byI[Number(c.dataset.colI)] = c;
    });
    const fill = [...shownIdx, ...hiddenIdx];
    let k = 0;
    const hidden = new Set(hiddenIdx);
    byI.map((c, i) => (fixed.has(i) ? c : byI[fill[k++]])).forEach(c => {
      c.hidden = hidden.has(Number(c.dataset.colI));
      row.appendChild(c);
    });
  };
  let current = chosenKeys(await loadColumnChoice(listId), allKeys, defaultKeys);
  const apply = keys => {
    current = keys;
    const shownIdx = keys.map(k => indexOf.get(k)).filter(i => i !== undefined);
    const hiddenIdx = columns.map(c => c.i).filter(i => !shownIdx.includes(i));
    for (const row of table.rows) arrange(row, shownIdx, hiddenIdx);
    table.dataset.colsApplied = '1';
  };
  apply(current);
  // Rows a list draws later (on scroll) are arranged as they arrive.
  for (const body of table.tBodies) {
    new MutationObserver(list => {
      const shownIdx = current.map(k => indexOf.get(k)).filter(i => i !== undefined);
      const hiddenIdx = columns.map(c => c.i).filter(i => !shownIdx.includes(i));
      for (const m of list) for (const node of m.addedNodes) if (node.nodeName === 'TR') arrange(node, shownIdx, hiddenIdx);
    }).observe(body, { childList: true });
  }
  return {
    open: button => openColumnChooser(button, {
      listId, columns: columns.map(c => ({ key: c.key, label: c.key })), shown: current, defaultKeys, onChange: apply,
    }),
  };
}

// wireColumns: tableColumns plus the screen's Columns button, in one call.
// A table with no header row has nothing to choose, so its button goes.
export async function wireColumns(table, button, listId, off) {
  const cols = await tableColumns(table, listId, off);
  if (!button) return;
  if (!cols) { button.remove(); return; }
  button.onclick = () => cols.open(button);
}
