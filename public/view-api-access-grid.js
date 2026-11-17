// 205.11 / 205.13: the module grid shared by New API access and a key's
// Modules tab. Off / View / View & change for each module; a module that
// cannot be given is greyed with its reason; a collapsed Fine-tune narrows a
// module to chosen document types and holds "Can approve".

const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');

// buildModuleGrid(host, mods, initial, onChange) -> { read, setLevels }
// mods: GET /api/v1/api-access/modules rows. initial: { modules, doctypes, can_approve }.
export function buildModuleGrid(host, mods, initial = {}, onChange = () => {}) {
  const levels = { ...(initial.modules || {}) };
  const narrow = {};
  Object.entries(initial.doctypes || {}).forEach(([m, list]) => { narrow[m] = new Set(list); });
  let canApprove = !!initial.can_approve;
  const byKey = new Map(mods.map(m => [m.key, m]));

  const reasonOf = (m) => m.reason || (m.in_plan === false ? 'Not in plan' : 'Not in your access');
  const lvlOf = (m) => levels[m.key] === 'write' ? 'write' : levels[m.key] === 'read' ? 'read' : 'off';

  function paint() {
    host.innerHTML = `<table class="api-grid"><thead><tr><th>Modules</th><th class="api-lvl">Off</th><th class="api-lvl">View</th><th class="api-lvl">View &amp; change</th></tr></thead><tbody>
      ${mods.map(m => {
        const cur = lvlOf(m);
        const blocked = m.in_plan === false || (!m.can_view && !m.can_change);
        const radio = (v, allowed) => `<td class="api-lvl"><input type="radio" name="api-lvl-${esc(m.key)}" value="${v}" data-mod="${esc(m.key)}" aria-label="${esc(m.name)} ${v}"${cur === v ? ' checked' : ''}${(allowed || cur === v) ? '' : ' disabled'}></td>`;
        return `<tr class="${blocked && cur === 'off' ? 'api-off' : ''}"><td><strong>${esc(m.name)}</strong>${m.summary ? `<div class="api-why">${esc(m.summary)}</div>` : ''}${blocked && cur === 'off' ? `<div class="api-why">${esc(reasonOf(m))}</div>` : ''}</td>
          ${radio('off', true)}${radio('read', !blocked && m.can_view)}${radio('write', !blocked && m.can_change)}</tr>`;
      }).join('')}</tbody></table><div data-fine></div>`;
    paintFine();
  }

  function paintFine() {
    const fine = host.querySelector('[data-fine]');
    const on = mods.filter(m => lvlOf(m) !== 'off');
    const anyWrite = on.some(m => lvlOf(m) === 'write');
    const narrowable = on.filter(m => (m.doctypes || []).length > 1);
    if (!narrowable.length && !anyWrite) { fine.innerHTML = ''; return; }
    fine.innerHTML = `<details class="api-fine"${Object.keys(narrow).length || canApprove ? ' open' : ''}><summary>Fine-tune</summary>
      ${narrowable.map(m => {
        const set = narrow[m.key];
        return `<div class="api-fine-mod"><strong>${esc(m.name)}</strong> <span class="api-muted">${set ? set.size : m.doctypes.length} of ${m.doctypes.length}</span>
          <div class="api-fine-list">${m.doctypes.map(d => `<label class="api-check"><input type="checkbox" data-dt="${esc(d.doctype)}" data-mod="${esc(m.key)}"${!set || set.has(d.doctype) ? ' checked' : ''}> ${esc(d.label || d.doctype)}${d.class === 'ro' && lvlOf(m) === 'write' ? ' <span class="api-muted">(view only)</span>' : ''}</label>`).join('')}</div></div>`;
      }).join('')}
      ${anyWrite ? `<label class="api-check"><input type="checkbox" data-approve${canApprove ? ' checked' : ''}> Can approve</label>
        <p class="api-warn">Approving by key skips the person who should check it.</p>` : ''}</details>`;
  }

  host.addEventListener('change', (e) => {
    const r = e.target.closest('input[type=radio][data-mod]');
    if (r) {
      levels[r.dataset.mod] = r.value;
      if (r.value === 'off') { delete levels[r.dataset.mod]; delete narrow[r.dataset.mod]; }
      if (!Object.values(levels).includes('write')) canApprove = false;
      const open = host.querySelector('.api-fine') && host.querySelector('.api-fine').open;
      paintFine();
      if (open) { const d = host.querySelector('.api-fine'); if (d) d.open = true; }
      onChange();
      return;
    }
    const c = e.target.closest('input[data-dt]');
    if (c) {
      const m = byKey.get(c.dataset.mod);
      if (!narrow[m.key]) narrow[m.key] = new Set(m.doctypes.map(d => d.doctype));
      if (c.checked) narrow[m.key].add(c.dataset.dt); else narrow[m.key].delete(c.dataset.dt);
      if (narrow[m.key].size === m.doctypes.length) delete narrow[m.key];
      const count = c.closest('.api-fine-mod').querySelector('.api-muted');
      count.textContent = `${narrow[m.key] ? narrow[m.key].size : m.doctypes.length} of ${m.doctypes.length}`;
      onChange();
      return;
    }
    const a = e.target.closest('input[data-approve]');
    if (a) { canApprove = a.checked; onChange(); }
  });

  paint();
  return {
    read() {
      const doctypes = {};
      Object.entries(narrow).forEach(([k, set]) => { if (levels[k]) doctypes[k] = [...set]; });
      return { modules: { ...levels }, doctypes, can_approve: canApprove };
    },
    setLevels(next) {
      Object.keys(levels).forEach(k => delete levels[k]);
      Object.keys(narrow).forEach(k => delete narrow[k]);
      Object.assign(levels, next);
      canApprove = false;
      paint();
      onChange();
    },
    // names of modules not currently on, for presets that must skip unknown keys
    has(key) { return byKey.has(key); },
    canGive(key, level) { const m = byKey.get(key); return !!m && m.in_plan !== false && (level === 'write' ? m.can_change : m.can_view); },
  };
}

// summaryOf: "View Inventory, Procurement · Change OMS" from a grid read().
export function summaryOf(read, mods) {
  const name = (k) => (mods.find(m => m.key === k) || { name: k }).name;
  const view = Object.entries(read.modules).filter(([, v]) => v === 'read').map(([k]) => name(k));
  const change = Object.entries(read.modules).filter(([, v]) => v === 'write').map(([k]) => name(k));
  const parts = [];
  if (view.length) parts.push(`View ${view.join(', ')}`);
  if (change.length) parts.push(`Change ${change.join(', ')}`);
  return parts.length ? parts.join(' · ') : 'No modules';
}
