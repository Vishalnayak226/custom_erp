// Stage 57 (user QA round, 2026-10-04): the two shell widgets that are not
// needed until someone uses them - the shared date picker (57.6) and the
// inline quick-create dialog (57.2). Loaded by the shell's loadPickers()
// through loadViewModule on first use, like any view module, so the shell
// stays inside the NFR-COST-001 initial-JS and cold-core budgets. Shared app
// services (apiFetch, state, escapeHTMLText, attachLinkTypeahead, ...) resolve
// from the classic shell; the exports below are published onto window by the
// loader.

// The widgets' styles travel with them, injected once on first load, for the
// same budget reason (styles.css is part of the cold core).
if (!document.getElementById('view-pickers-css')) {
  const style = document.createElement('style');
  style.id = 'view-pickers-css';
  style.textContent = `
/* Inline quick-create (57.2) opens on top of a form that is already open. */
.modal-overlay.quick-create-overlay { z-index: 120; }
.quick-create-intro { font-size: 13px; color: var(--text-muted); margin: 0 0 12px; }
.quick-create-overlay .modal-footer { align-items: center; }
.quick-create-overlay .quick-create-full { margin-right: auto; font-size: 12.5px; }

.date-picker {
  position: fixed;
  z-index: 400;
  width: 288px;
  padding: 12px;
  background-color: var(--panel-bg);
  color: var(--text-main);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  box-shadow: var(--shadow-lg), 0 0 0 1px rgba(15, 23, 42, 0.02);
  opacity: 0;
  transform: translateY(-4px) scale(0.98);
  transform-origin: top left;
  transition: opacity 0.14s ease, transform 0.14s ease;
  font-size: 13px;
  user-select: none;
}
.date-picker.open { opacity: 1; transform: none; }
@media (prefers-reduced-motion: reduce) {
  .date-picker { transition: none; }
}
.date-picker button { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
.date-picker button:disabled { cursor: not-allowed; opacity: 0.35; }
.date-picker button:focus-visible { outline: 2px solid var(--primary-color); outline-offset: 1px; }
.date-picker .dp-header { display: flex; align-items: center; gap: 4px; margin-bottom: 8px; }
.date-picker .dp-nav { width: 32px; height: 32px; border-radius: 8px; font-size: 20px; line-height: 1; color: var(--text-muted); }
.date-picker .dp-nav:hover { background-color: var(--surface-subtle); color: var(--text-main); }
.date-picker .dp-title { flex: 1; height: 32px; border-radius: 8px; font-weight: 600; }
.date-picker .dp-title:hover { background-color: var(--surface-subtle); }
.date-picker .dp-weekdays, .date-picker .dp-grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2px; }
.date-picker .dp-weekdays span { text-align: center; font-size: 11px; font-weight: 600; color: var(--text-muted); padding: 4px 0; }
.date-picker .dp-day { height: 36px; border-radius: 8px; font-variant-numeric: tabular-nums; transition: background-color 0.1s ease; }
.date-picker .dp-day:hover:not(:disabled) { background-color: var(--surface-subtle); }
.date-picker .dp-day.is-outside { color: var(--text-muted); opacity: 0.55; }
.date-picker .dp-day.is-today { box-shadow: inset 0 0 0 1px var(--primary-color); font-weight: 600; }
.date-picker .dp-day.is-selected,
.date-picker .dp-cell.is-selected { background-color: var(--primary-color); color: #fff; font-weight: 600; }
.date-picker .dp-cells { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; }
.date-picker .dp-cell { height: 44px; border-radius: 8px; }
.date-picker .dp-cell:hover:not(.is-selected) { background-color: var(--surface-subtle); }
.date-picker .dp-footer { display: flex; justify-content: space-between; margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--border-color); }
.date-picker .dp-link { padding: 6px 10px; border-radius: 6px; color: var(--primary-color); font-weight: 600; }
.date-picker .dp-link:hover:not(:disabled) { background-color: var(--primary-light); }
/* Stage 66: one-click GST rates under the GST box - the HSN's own first. */
.hsn-rate-chips { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 6px; margin-top: 6px; font-size: 12px; }
.hsn-rate-chips:empty { display: none; }
.hsn-rate-label { color: var(--text-muted); }
.hsn-rate-label:not(:first-child) { margin-left: 4px; }
.hsn-rate-chip { border: 1px solid var(--border-color); background: var(--panel-bg); color: var(--text-main); border-radius: 999px; padding: 2px 9px; font: inherit; cursor: pointer; }
.hsn-rate-chip:hover { border-color: var(--border-hover); background: var(--surface-subtle); }
.hsn-rate-chip.primary { border-color: var(--primary-color); background: var(--primary-light); }
.hsn-rate-chip.active { background: var(--primary-color); border-color: var(--primary-color); color: var(--on-primary); }
.hsn-rate-note { flex-basis: 100%; color: var(--warning-strong); }
/* Stage 80.1: the record form's named sections (Item). */
#dynamic-modal-body .form-section { grid-column: 1 / -1; border-top: 1px solid var(--border-color); padding-top: 12px; }
#dynamic-modal-body .form-section:first-of-type { border-top: 0; padding-top: 0; }
.form-section > summary { display: flex; align-items: center; gap: 8px; cursor: pointer; list-style: none; font-size: 13px; font-weight: 700; color: var(--text-main); margin-bottom: 12px; }
.form-section > summary::-webkit-details-marker { display: none; }
.form-section > summary::before { content: '\\25B8'; color: var(--text-muted); transition: transform 0.12s ease; }
.form-section[open] > summary::before { transform: rotate(90deg); }
.form-section:not([open]) > summary { margin-bottom: 0; }
.form-section-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(240px, 100%), 1fr)); gap: 16px 20px; align-items: start; }
.form-section-grid .form-group-wide { grid-column: 1 / -1; }
/* Stage 80.2: variant options as option + value rows. */
.variant-editor { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; }
.variant-editor > div { display: flex; flex-direction: column; gap: 6px; width: 100%; }
.variant-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 8px; align-items: center; max-width: 560px; }
/* Stage 81: an attribute's values as removable chips. */
.value-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.value-chip { display: inline-flex; align-items: center; gap: 2px; padding: 2px 2px 2px 10px; border: 1px solid var(--border-color); border-radius: 999px; background: var(--panel-bg); font-size: 13px; }
.value-chip button { border: 0; background: none; color: var(--text-muted); cursor: pointer; font-size: 15px; line-height: 1; padding: 2px 6px; border-radius: 999px; }
.value-chip button:hover { color: var(--danger-color); background: var(--surface-subtle); }
.value-add { display: flex; gap: 8px; margin-top: 8px; max-width: 420px; }
`;
  document.head.appendChild(style);
}

// ---------------------------------------------------------------------------
// One date picker for every date field (Stage 57.6)
//
// "Appointment Calendar - calendar selection is basic, make it better and
// smooth. Change it in all calendars of the ERP." Every date field here is a
// native <input type="date">, whose picker differs per browser and cannot be
// themed. This replaces only the PICKER: the input itself stays a native
// date input, so its value stays "YYYY-MM-DD", typing a date still works,
// min/max still validate, and not one reader of any date field changes. One
// observer enhances every date input anywhere in the document - forms,
// filters, dialogs - including ones rendered long after this ran.
// ---------------------------------------------------------------------------
const DP_MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DP_WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];
let datePicker = null; // the one open picker: { input, el, view, focus, mode, min, max }

function parseISODate(s) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(s || ''));
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return isNaN(d.getTime()) ? null : d;
}

function dpSameDay(a, b) {
  return !!a && !!b && a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

function dpAddDays(d, n) { const out = new Date(d); out.setDate(out.getDate() + n); return out; }

function dpAddMonths(d, n) {
  const out = new Date(d.getFullYear(), d.getMonth() + n, 1);
  out.setDate(Math.min(d.getDate(), new Date(out.getFullYear(), out.getMonth() + 1, 0).getDate()));
  return out;
}

function dpInRange(d, p) {
  return !(p.min && d < p.min) && !(p.max && d > p.max);
}

function closeDatePicker(returnFocus) {
  if (!datePicker) return;
  const { el, input } = datePicker;
  datePicker = null;
  el.remove();
  input.removeEventListener('keydown', onDatePickerInputKey);
  input.removeEventListener('input', onDatePickerInputTyped);
  document.removeEventListener('mousedown', onDatePickerOutside, true);
  window.removeEventListener('resize', onDatePickerViewportChange);
  window.removeEventListener('scroll', onDatePickerViewportChange, true);
  if (returnFocus && input.isConnected) input.focus();
}

// Escape in the box closes the picker; typing a date moves it there.
function onDatePickerInputKey(e) {
  if (e.key === 'Escape' && datePicker) { e.preventDefault(); e.stopPropagation(); closeDatePicker(false); }
}

function onDatePickerInputTyped() {
  if (!datePicker) return;
  const d = parseISODate(datePicker.input.value);
  if (d) { datePicker.focus = d; datePicker.mode = 'days'; renderDatePicker(); }
}

function onDatePickerOutside(e) {
  if (datePicker && !datePicker.el.contains(e.target) && e.target !== datePicker.input) closeDatePicker(false);
}

function onDatePickerViewportChange(e) {
  if (datePicker && !(e && e.target && datePicker.el.contains(e.target))) positionDatePicker();
}

function openDatePicker(input, focusInside) {
  if (datePicker && datePicker.input === input) { if (focusInside) focusDatePickerCell(); return; }
  closeDatePicker(false);
  const selected = parseISODate(input.value);
  const p = { input, min: parseISODate(input.min), max: parseISODate(input.max), mode: 'days' };
  let start = selected || new Date();
  if (p.min && start < p.min) start = p.min;
  if (p.max && start > p.max) start = p.max;
  p.focus = new Date(start.getFullYear(), start.getMonth(), start.getDate());
  p.el = document.createElement('div');
  p.el.className = 'date-picker';
  p.el.setAttribute('role', 'dialog');
  p.el.setAttribute('aria-label', 'Choose a date');
  document.body.appendChild(p.el);
  datePicker = p;
  renderDatePicker();
  positionDatePicker();
  requestAnimationFrame(() => p.el.classList.add('open'));
  document.addEventListener('mousedown', onDatePickerOutside, true);
  window.addEventListener('resize', onDatePickerViewportChange);
  window.addEventListener('scroll', onDatePickerViewportChange, true);
  p.el.addEventListener('keydown', onDatePickerKey);
  input.addEventListener('keydown', onDatePickerInputKey);
  input.addEventListener('input', onDatePickerInputTyped);
  if (focusInside) focusDatePickerCell();
}

function positionDatePicker() {
  if (!datePicker) return;
  const { el, input } = datePicker;
  if (!input.isConnected) { closeDatePicker(false); return; }
  const r = input.getBoundingClientRect();
  const w = el.offsetWidth, h = el.offsetHeight;
  // Keep clear of the fixed environment banner, as the typeahead menu does.
  const banner = document.getElementById('environment-banner');
  const topLimit = (banner ? banner.getBoundingClientRect().bottom : 0) + 8;
  let top = r.bottom + 6;
  if (top + h > window.innerHeight - 8 && r.top - h - 6 > topLimit) top = r.top - h - 6;
  const left = Math.max(8, Math.min(r.left, window.innerWidth - w - 8));
  el.style.top = `${Math.max(topLimit, top)}px`;
  el.style.left = `${left}px`;
}

function renderDatePicker() {
  const p = datePicker;
  if (!p) return;
  const selected = parseISODate(p.input.value);
  const today = new Date();
  const y = p.focus.getFullYear();
  const m = p.focus.getMonth();
  let title, body;
  if (p.mode === 'days') {
    title = `${DP_MONTHS[m]} ${y}`;
    const first = new Date(y, m, 1);
    const gridStart = dpAddDays(first, -first.getDay());
    let cells = '';
    for (let i = 0; i < 42; i++) {
      const d = dpAddDays(gridStart, i);
      const cls = ['dp-day'];
      if (d.getMonth() !== m) cls.push('is-outside');
      if (dpSameDay(d, today)) cls.push('is-today');
      if (dpSameDay(d, selected)) cls.push('is-selected');
      if (dpSameDay(d, p.focus)) cls.push('is-focus');
      const ok = dpInRange(d, p);
      cells += `<button type="button" class="${cls.join(' ')}" data-date="${localISODate(d)}" tabindex="${dpSameDay(d, p.focus) ? '0' : '-1'}" ${ok ? '' : 'disabled'}
        aria-label="${d.getDate()} ${DP_MONTHS[d.getMonth()]} ${d.getFullYear()}" ${dpSameDay(d, selected) ? 'aria-pressed="true"' : ''}>${d.getDate()}</button>`;
    }
    body = `<div class="dp-weekdays">${DP_WEEKDAYS.map(w => `<span>${w}</span>`).join('')}</div><div class="dp-grid">${cells}</div>`;
  } else if (p.mode === 'months') {
    title = `${y}`;
    body = `<div class="dp-cells">${DP_MONTHS.map((name, i) =>
      `<button type="button" class="dp-cell${i === m ? ' is-focus' : ''}${selected && selected.getFullYear() === y && selected.getMonth() === i ? ' is-selected' : ''}" data-month="${i}" tabindex="${i === m ? '0' : '-1'}">${name.slice(0, 3)}</button>`).join('')}</div>`;
  } else {
    const base = y - (y % 12);
    title = `${base} – ${base + 11}`;
    body = `<div class="dp-cells">${Array.from({ length: 12 }, (_, i) => base + i).map(yr =>
      `<button type="button" class="dp-cell${yr === y ? ' is-focus' : ''}${selected && selected.getFullYear() === yr ? ' is-selected' : ''}" data-year="${yr}" tabindex="${yr === y ? '0' : '-1'}">${yr}</button>`).join('')}</div>`;
  }
  p.el.innerHTML = `
    <div class="dp-header">
      <button type="button" class="dp-nav" data-dp="prev" aria-label="Previous">&#8249;</button>
      <button type="button" class="dp-title" data-dp="zoom" aria-label="${p.mode === 'years' ? 'Years' : 'Change month or year'}">${title}</button>
      <button type="button" class="dp-nav" data-dp="next" aria-label="Next">&#8250;</button>
    </div>
    ${body}
    <div class="dp-footer">
      <button type="button" class="dp-link" data-dp="today" ${dpInRange(new Date(today.getFullYear(), today.getMonth(), today.getDate()), p) ? '' : 'disabled'}>Today</button>
      ${p.input.required ? '' : '<button type="button" class="dp-link" data-dp="clear">Clear</button>'}
    </div>`;
  p.el.querySelectorAll('[data-date]').forEach(b => b.addEventListener('click', () => chooseDate(parseISODate(b.dataset.date))));
  p.el.querySelectorAll('[data-month]').forEach(b => b.addEventListener('click', () => {
    p.focus = new Date(y, Number(b.dataset.month), Math.min(p.focus.getDate(), 28)); p.mode = 'days'; renderDatePicker(); focusDatePickerCell();
  }));
  p.el.querySelectorAll('[data-year]').forEach(b => b.addEventListener('click', () => {
    p.focus = new Date(Number(b.dataset.year), m, 1); p.mode = 'months'; renderDatePicker(); focusDatePickerCell();
  }));
  p.el.querySelector('[data-dp="prev"]').addEventListener('click', () => stepDatePicker(-1));
  p.el.querySelector('[data-dp="next"]').addEventListener('click', () => stepDatePicker(1));
  p.el.querySelector('[data-dp="zoom"]').addEventListener('click', () => {
    p.mode = p.mode === 'days' ? 'months' : 'years'; renderDatePicker(); focusDatePickerCell();
  });
  p.el.querySelector('[data-dp="today"]').addEventListener('click', () => chooseDate(new Date()));
  p.el.querySelector('[data-dp="clear"]')?.addEventListener('click', () => chooseDate(null));
}

function focusDatePickerCell() {
  datePicker?.el.querySelector('.is-focus')?.focus();
}

function stepDatePicker(dir) {
  const p = datePicker;
  if (p.mode === 'days') p.focus = dpAddMonths(p.focus, dir);
  else if (p.mode === 'months') p.focus = new Date(p.focus.getFullYear() + dir, p.focus.getMonth(), 1);
  else p.focus = new Date(p.focus.getFullYear() + 12 * dir, p.focus.getMonth(), 1);
  renderDatePicker();
  positionDatePicker();
  // The arrow just clicked was rebuilt; keep focus on it so repeated clicks
  // and keyboard users stay in place.
  p.el.querySelector(`[data-dp=""]`)?.focus();
}

function chooseDate(d) {
  const p = datePicker;
  if (!p) return;
  if (d && !dpInRange(new Date(d.getFullYear(), d.getMonth(), d.getDate()), p)) return;
  p.input.value = d ? localISODate(d) : '';
  p.input.dispatchEvent(new Event('input', { bubbles: true }));
  p.input.dispatchEvent(new Event('change', { bubbles: true }));
  closeDatePicker(true);
}

function onDatePickerKey(e) {
  const p = datePicker;
  if (!p) return;
  if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); closeDatePicker(true); return; }
  if (p.mode !== 'days' || !e.target.classList.contains('dp-day')) return;
  const moves = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
  let next = null;
  if (moves[e.key] !== undefined) next = dpAddDays(p.focus, moves[e.key]);
  else if (e.key === 'PageUp') next = e.shiftKey ? dpAddMonths(p.focus, -12) : dpAddMonths(p.focus, -1);
  else if (e.key === 'PageDown') next = e.shiftKey ? dpAddMonths(p.focus, 12) : dpAddMonths(p.focus, 1);
  else if (e.key === 'Home') next = dpAddDays(p.focus, -p.focus.getDay());
  else if (e.key === 'End') next = dpAddDays(p.focus, 6 - p.focus.getDay());
  if (!next) return;
  e.preventDefault();
  p.focus = next;
  renderDatePicker();
  focusDatePickerCell();
}

// ---------------------------------------------------------------------------
// Inline quick-create (Stage 57.2) - the dialog. The shell's openQuickCreate()
// is the entry point; see its comment and quickCreateIntoInput beside it.
// ---------------------------------------------------------------------------
const quickCreateMetaCache = {};

async function quickCreateFields(doctype) {
  if (!quickCreateMetaCache[doctype]) {
    const res = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}/meta`);
    if (!res || !res.ok) return null;
    quickCreateMetaCache[doctype] = await res.json();
  }
  return quickCreateMetaCache[doctype];
}

// openQuickCreate resolves to the created record, or null if the user
// cancelled or it could not be created inline.
async function openQuickCreateDialog(doctype, seedText) {
  const label = getDoctypeLabel(doctype);
  if (!canCreateDoctype(doctype)) {
    await showCustomAlert(SETUP_MSG.missingNoAccess(label).replace(/&mdash;/g, '-'), `Add ${label}`);
    return null;
  }
  const fields = await quickCreateFields(doctype);
  if (!fields) return null;
  const activeDoc = state.activeDoctypes.find(d => d.name === doctype);
  const isMaster = activeDoc && activeDoc.document_type === 'Master';
  // Stage 76.5: a master's code is numbered on the server; a typed-code
  // master (Currency, UOM, HSN) asks for it like any required field.
  const series = isMaster ? await loadMasterCodeSeries(doctype) : null;
  const isCode = f => f.auto_generated || (isMaster && f.fieldname.toLowerCase() === 'code' && !(series && series.typed));
  const seedField = ['name', 'hsn', 'description'].find(n => fields.some(f => f.fieldname === n));
  // Stage 89: a new Item is asked its kind and its item name first - the
  // item name fills HSN, GST and the name (view-item-form.js).
  const itemAsks = doctype === 'Item' ? ['item_type', 'item_name'] : [];
  const asked = fields.filter(f =>
    f.fieldname !== 'id' && !f.mirrored && !isCode(f) && !isDerivedCompanionField(f.fieldname) && !isStateCodeFieldName(f.fieldname) &&
    (f.mandatory || f.fieldname === seedField || itemAsks.includes(f.fieldname)))
    .map(f => (itemAsks.includes(f.fieldname) ? { ...f, mandatory: true } : f))
    .sort((a, b) => (itemAsks.includes(b.fieldname) ? 1 : 0) - (itemAsks.includes(a.fieldname) ? 1 : 0) || itemAsks.indexOf(a.fieldname) - itemAsks.indexOf(b.fieldname));
  // A required table/map field needs the full editor - hand over to the
  // full form rather than render half a record.
  if (asked.some(f => f.fieldtype === 'JSONTable' || f.fieldtype === 'JSONMap')) {
    openSetupDoctype(doctype);
    return null;
  }

  return new Promise(resolve => {
    // Focus goes back to whatever opened the dialog - usually the field the
    // new record is about to fill.
    const returnFocus = document.activeElement;
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open quick-create-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.innerHTML = `
      <div class="modal-container">
        <div class="modal-header">
          <h3 class="modal-title"></h3>
          <button type="button" class="modal-close" aria-label="Close">&times;</button>
        </div>
        <form novalidate>
          <div class="modal-body">
            <p class="quick-create-intro"></p>
            <div class="quick-create-fields"></div>
            <div class="login-error hidden" role="alert"></div>
          </div>
          <div class="modal-footer">
            <a href="${deepLinkForDoctype(doctype)}" target="_blank" rel="noopener" class="empty-state-link quick-create-full">Open the full ${escapeHTMLText(label)} form</a>
            <button type="button" class="btn btn-secondary">Cancel</button>
            <button type="submit" class="btn btn-primary">Create ${escapeHTMLText(label)}</button>
          </div>
        </form>
      </div>`;
    overlay.querySelector('.modal-title').textContent = `New ${label}`;
    overlay.querySelector('.quick-create-intro').textContent =
      `Only what a ${label} needs. You stay where you are, and it is filled in for you once created.`;
    const holder = overlay.querySelector('.quick-create-fields');
    const errorEl = overlay.querySelector('.login-error');
    const form = overlay.querySelector('form');
    const submitBtn = overlay.querySelector('button[type="submit"]');
    if (series && !series.typed && !series.configured) holder.appendChild(masterCodeSetupRow(series, label));

    asked.forEach(f => {
      const fg = document.createElement('div');
      fg.className = 'form-group';
      const id = `qc-${doctype}-${f.fieldname}`;
      fg.innerHTML = `<label class="form-label" for="${id}"></label>`;
      fg.querySelector('label').innerHTML = `${escapeHTMLText(taxIdLabel(f.fieldname, getTranslatedLabel(f.label)))}${f.mandatory ? '<span class="required">*</span>' : ''}`;
      let input;
      if (f.fieldtype === 'Data' && isStateFieldName(f.fieldname) && fieldRegions().length) {
        input = buildStateSelect({ name: f.fieldname, required: f.mandatory });
      } else if (f.fieldtype === 'Select') {
        input = document.createElement('select');
        input.className = 'form-select';
        const opts = String(f.options || '').split(',').map(o => o.trim()).filter(Boolean);
        input.innerHTML = '<option value="">— Select —</option>' +
          opts.map(o => `<option value="${escapeHTMLText(o)}">${escapeHTMLText(o)}</option>`).join('');
        input.value = defaultSelectValue(f, opts);
      } else if (f.fieldtype === 'Check') {
        input = document.createElement('input');
        input.type = 'checkbox';
      } else {
        input = document.createElement('input');
        input.className = 'form-input';
        input.type = f.fieldtype === 'Number' || f.fieldtype === 'Currency' ? 'number'
          : f.fieldtype === 'Date' ? 'date' : 'text';
        if (input.type === 'number') input.step = 'any';
        if (f.fieldname === seedField && seedText) input.value = seedText;
      }
      input.id = id;
      input.name = f.fieldname;
      fg.appendChild(input);
      holder.appendChild(fg);
      if (f.fieldtype === 'Link' && f.options) attachLinkTypeahead(input, f.options);
      // The one field this ERP mints on request (Stage 36.7.4) - offered here
      // too, or a required barcode would make "create Item" a dead end.
      if (doctype === 'Item' && f.fieldname === 'barcode') {
        const gen = document.createElement('button');
        gen.type = 'button';
        gen.className = 'btn btn-outline btn-sm';
        gen.textContent = 'Generate';
        gen.style.marginTop = '6px';
        gen.addEventListener('click', async () => {
          gen.disabled = true;
          const res = await apiFetch('/api/v1/pim/barcode/generate', { method: 'POST' });
          gen.disabled = false;
          if (res && res.ok) input.value = (await res.json()).barcode;
          else if (res) await showApiError(res, 'Failed to generate a barcode.');
        });
        fg.appendChild(gen);
      }
    });
    // Stage 66: every record with an HSN box (Item, Item Name, a design) gets
    // the suggesting HSN picker and the rate that follows it.
    const gstBox = holder.querySelector('[name="gst_rate"]');
    const hsnBox = holder.querySelector('[name="hsn_code"]');
    if (hsnBox) attachSmartHSNPicker(hsnBox, gstBox);
    if (doctype === 'Item') loadViewModule('/view-item-form.js').then(m => m.wireItemQuickCreate(holder)); // Stage 89
    // A 0% rate is valid only with a non-taxable Tax Treatment (MASTER-0044),
    // which this dialog does not otherwise ask - so the choice appears the
    // moment the rate is 0, and is sent only then.
    const treatmentField = fields.find(f => f.fieldname === 'tax_treatment' && f.fieldtype === 'Select');
    let treatmentGroup = null;
    if (gstBox && treatmentField && !asked.includes(treatmentField)) {
      const tid = `qc-${doctype}-tax_treatment`;
      const opts = String(treatmentField.options || '').split(',').map(o => o.trim()).filter(o => o && o !== 'Taxable');
      treatmentGroup = document.createElement('div');
      treatmentGroup.className = 'form-group hidden';
      treatmentGroup.innerHTML = `<label class="form-label" for="${tid}">${escapeHTMLText(getTranslatedLabel(treatmentField.label))}<span class="required">*</span></label>
        <select class="form-select" id="${tid}" name="tax_treatment"><option value="">— Select —</option>${opts.map(o => `<option value="${escapeHTMLText(o)}">${escapeHTMLText(o)}</option>`).join('')}</select>`;
      gstBox.closest('.form-group').insertAdjacentElement('afterend', treatmentGroup);
      const syncTreatment = () => treatmentGroup.classList.toggle('hidden', !(gstBox.value !== '' && Number(gstBox.value) === 0));
      gstBox.addEventListener('input', syncTreatment);
      gstBox.addEventListener('change', syncTreatment);
    }

    const close = (result) => {
      overlay.remove();
      document.removeEventListener('keydown', onKey, true);
      if (returnFocus && returnFocus.isConnected) returnFocus.focus();
      resolve(result);
    };
    const onKey = (e) => {
      if (e.key === 'Escape' && !document.querySelector('.typeahead-menu')) { e.stopPropagation(); close(null); }
    };
    document.addEventListener('keydown', onKey, true);
    overlay.querySelector('.modal-close').addEventListener('click', () => close(null));
    overlay.querySelector('.btn-secondary').addEventListener('click', () => close(null));

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      errorEl.classList.add('hidden');
      const missing = asked.filter(f => f.mandatory && f.fieldtype !== 'Check' &&
        !String(form.querySelector(`[name="${f.fieldname}"]`).value || '').trim());
      if (missing.length) {
        errorEl.textContent = `Please fill in: ${missing.map(f => getTranslatedLabel(f.label)).join(', ')}.`;
        errorEl.classList.remove('hidden');
        form.querySelector(`[name="${missing[0].fieldname}"]`).focus();
        return;
      }
      const payload = {};
      asked.forEach(f => {
        const el = form.querySelector(`[name="${f.fieldname}"]`);
        if (f.fieldtype === 'Check') payload[f.fieldname] = el.checked;
        else if (f.fieldtype === 'Number' || f.fieldtype === 'Currency') { if (el.value !== '') payload[f.fieldname] = parseFloat(el.value); }
        else payload[f.fieldname] = el.value;
      });
      if (treatmentGroup && !treatmentGroup.classList.contains('hidden')) {
        const treatment = treatmentGroup.querySelector('select');
        if (!treatment.value) {
          errorEl.textContent = 'A 0% GST rate needs a Tax Treatment - choose Exempt, Nil-Rated or Zero-Rated.';
          errorEl.classList.remove('hidden');
          treatment.focus();
          return;
        }
        payload.tax_treatment = treatment.value;
      }
      submitBtn.disabled = true;
      submitBtn.textContent = 'Creating…';
      try {
        // Stage 76.5: the server numbers the master (V00001); the first one
        // of its type carries the code format chosen above.
        const codeFormat = readMasterCodeFormat(form);
        if (codeFormat) payload._code_format = codeFormat;
        const res = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}`, { method: 'POST', body: JSON.stringify(payload) });
        if (!res) return;
        if (!res.ok) {
          errorEl.textContent = await getErrorMessage(res, `Could not create the ${label}.`);
          errorEl.classList.remove('hidden');
          return;
        }
        const saved = await res.json().catch(() => ({}));
        const docRes = saved.id ? await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}/${encodeURIComponent(saved.id)}`) : null;
        const doc = docRes && docRes.ok ? await docRes.json() : { ...payload, id: saved.id, code: payload.code || saved.id };
        rememberLinkDoc(doctype, doc);
        showToast(describeDocumentStatusOutcome(label, saved.status), {
          variant: saved.status === 'Pending Approval' ? 'warning' : 'success'
        });
        // The first record of its type turns every "not set up yet" hint
        // into the quieter "can't find it?" one.
        await refreshSetupStatus();
        close(doc);
      } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = `Create ${label}`;
      }
    });

    document.body.appendChild(overlay);
    const first = holder.querySelector('input:not([type="checkbox"]), select');
    (first || submitBtn).focus();
  });
}

// attachSmartHSNPicker (Stage 57.3; smart in Stage 66) - app.js attachHSNPicker() lands here. The HSN box opens on a
// list ranked for the item being named - codes the client's own similar
// items carry, the master list read against the name ("silver" is not
// "zircon"), the client's HSN list and its industry's common codes - from
// GET /api/v1/hsn/suggest (engines/hsn_suggest.go). Typing searches all of
// them; a code nobody suggested is still created inline. The box keeps the
// plain number (engines/hsn_catalog.go).
//
// The GST rate follows the code: filled when the code has one rate and the
// rate box is empty or was filled by this picker, cleared again when the new
// code's rate depends on the price, and always offered as one-click rates
// under the box (the code's own first, with when each applies).

// The fields whose text describes the item - what the suggestions read.
const HSN_SIGNAL_FIELDS = ['name', 'item_name', 'category', 'sub_category', 'product_type', 'item_type',
  'metal_type', 'stone_type', 'fabric', 'device_category', 'type', 'description'];
// One-click rates offered beside a code's own (GST 2.0 slabs, and 3% for
// precious metals and jewellery).
const GST_RATE_SLABS = [0, 3, 5, 18, 40];

function hsnRateOf(s) {
  if (!s) return null;
  if (s.gst_rate !== undefined && s.gst_rate !== null && s.gst_rate !== '') return Number(s.gst_rate);
  return s.rates && s.rates.length === 1 ? Number(s.rates[0].rate) : null;
}

function attachSmartHSNPicker(hsnInput, gstInput) {
  if (!hsnInput || hsnInput.dataset.hsnPicker === '1') return;
  hsnInput.dataset.hsnPicker = '1';
  const scope = hsnInput.closest('form, .quick-create-fields, .modal-body') || document;
  const known = new Map(); // hsn -> suggestion row, from the lists shown so far
  const itemText = () => HSN_SIGNAL_FIELDS
    .map(n => scope.querySelector(`[name="${n}"]`))
    .filter(el => el && el !== hsnInput)
    .map(el => (el.tagName === 'SELECT' ? (el.value ? (el.selectedOptions[0] || {}).textContent || '' : '') : typedTextOf(el)).trim())
    .filter(Boolean)
    .join(' ');
  attachLinkTypeahead(hsnInput, 'HSNCode', {
    valueFields: ['hsn'],
    showAllOnFocus: true,
    groupBy: 'group',
    source: async (q) => {
      const res = await apiFetch(`/api/v1/hsn/suggest?name=${encodeURIComponent(itemText())}&q=${encodeURIComponent(q)}`);
      if (!res || !res.ok) return null;
      const rows = ((await res.json()) || {}).suggestions || [];
      rows.forEach(s => known.set(s.hsn, s));
      return rows;
    },
    // Rate before description: a long description is cut off at the end of
    // the row, and the rate is the part that must stay visible.
    labelFn: doc => {
      const rates = doc.rates && doc.rates.length > 1 ? doc.rates.map(r => r.rate) : [hsnRateOf(doc)].filter(r => r !== null);
      const head = `${doc.hsn || doc.code}${rates.length ? ` (GST ${rates.join(' / ')}%)` : ''}`;
      return doc.description ? `${head} — ${doc.description}` : head;
    },
    titleFn: doc => [doc.description, doc.reason ? `Why: ${doc.reason}` : '',
      ...(doc.rates || []).filter(r => r.note).map(r => `${r.rate}%: ${r.note}`)].filter(Boolean).join('\n')
  });
  if (!gstInput) return;

  const chips = document.createElement('div');
  chips.className = 'hsn-rate-chips';
  gstInput.insertAdjacentElement('afterend', chips);
  let current = null; // the suggestion row for the code now in the HSN box
  // Stage 80.3: the rate follows Tax Treatment on the same form - the server
  // rejects a positive rate on an Exempt / Nil-Rated / Zero-Rated item and a
  // 0 on a Taxable one (MASTER-0044), so the form stops offering either.
  const treatment = scope.querySelector('[name="tax_treatment"]');
  const untaxed = () => !!(treatment && treatment.value && treatment.value !== 'Taxable');

  const paint = () => {
    const own = current ? ((current.rates && current.rates.length) ? current.rates : (hsnRateOf(current) !== null ? [{ rate: hsnRateOf(current) }] : [])) : [];
    const chip = (rate, note, primary) => {
      const label = `${rate}%${note ? ` · ${note}` : ''}`;
      return `<button type="button" class="hsn-rate-chip${primary ? ' primary' : ''}" data-rate="${escapeHTMLText(String(rate))}" title="${escapeHTMLText(note || `GST ${rate}%`)}">${escapeHTMLText(label)}</button>`;
    };
    const others = GST_RATE_SLABS.filter(s => !own.some(r => Number(r.rate) === s));
    let html = '';
    if (own.length) html += `<span class="hsn-rate-label">For HSN ${escapeHTMLText(current.hsn)}:</span>${own.map(r => chip(r.rate, r.note, true)).join('')}`;
    html += `<span class="hsn-rate-label">${own.length ? 'Other' : 'Common rates'}:</span>${others.map(s => chip(s, '', false)).join('')}`;
    if (gstInput.value !== '' && Number(gstInput.value) === 0 && !untaxed()) {
      html += '<span class="hsn-rate-note">A 0% rate needs Tax Treatment set to Exempt, Nil-Rated or Zero-Rated.</span>';
    }
    chips.innerHTML = html;
    chips.querySelectorAll('[data-rate]').forEach(b => b.classList.toggle('active', gstInput.value !== '' && Number(b.dataset.rate) === Number(gstInput.value)));
  };
  const setRate = (rate, byPicker) => {
    gstInput.value = rate === null ? '' : rate;
    gstInput.dispatchEvent(new Event('input', { bubbles: true }));
    gstInput.dispatchEvent(new Event('change', { bubbles: true }));
    if (byPicker && rate !== null) gstInput.dataset.hsnFilled = '1';
    else delete gstInput.dataset.hsnFilled;
    paint();
  };
  // Keep focus where it is when a rate is clicked.
  chips.addEventListener('mousedown', e => e.preventDefault());
  chips.addEventListener('click', e => {
    const b = e.target.closest('[data-rate]');
    if (b) setRate(Number(b.dataset.rate), false);
  });
  gstInput.addEventListener('input', e => { if (e.isTrusted) { delete gstInput.dataset.hsnFilled; paint(); } });

  const follow = async () => {
    const hsn = hsnInput.value.replace(/[\s.]/g, '');
    if (!hsn) { current = null; paint(); return; }
    let s = known.get(hsn);
    if (!s) {
      const res = await apiFetch(`/api/v1/hsn/suggest?hsn=${encodeURIComponent(hsn)}`);
      s = res && res.ok ? (((await res.json()) || {}).suggestions || [])[0] : null;
    }
    if (hsnInput.value.replace(/[\s.]/g, '') !== hsn) return; // changed again meanwhile
    current = s || { hsn, rates: [] };
    const rate = hsnRateOf(current);
    const ours = gstInput.value === '' || gstInput.dataset.hsnFilled === '1';
    if (rate !== null && ours) setRate(rate, true);
    else if (rate === null && gstInput.dataset.hsnFilled === '1') setRate(null, false);
    else paint();
  };
  hsnInput.addEventListener('change', follow);
  follow();

  const followTreatment = (opening) => {
    const off = untaxed();
    gstInput.readOnly = off;
    chips.style.display = off ? 'none' : '';
    if (off && gstInput.value !== '0') setRate(0, false);
    else if (!off && !opening && gstInput.value === '0') setRate(null, false);
  };
  treatment?.addEventListener('change', () => followTreatment(false));
  followTreatment(true);
}

// ---------------------------------------------------------------------------
// Stage 76: states as a dropdown, the GSTIN choosing the state, a master's
// first-time code format. Kept here with the other shared form widgets so the
// shell stays inside its NFR-COST-001 budget. The home country, its states
// and its tax-ID rule arrive with the field formats (window.FIELD_META).
// ---------------------------------------------------------------------------
function fieldRegions() { return (window.FIELD_META && window.FIELD_META.regions) || []; }

// isStateFieldName mirrors the server's isStateField: "state" or "<x>_state"
// on a Data field (a Select "*_state" is a workflow state, not a place).
function isStateFieldName(name) {
  const n = String(name || '').toLowerCase().trim();
  return n === 'state' || n.endsWith('_state');
}

// The short code (KA) next to a state is set by the server, never typed.
function isStateCodeFieldName(name) { return /(^|_)state_code$/.test(String(name || '').toLowerCase()); }

// buildStateSelect renders a state field as the home country's list. The
// value is the state's name - what the server stores. A value saved before
// states were a list stays visible, flagged, so an edit cannot drop it
// silently; the server asks for a real one on save.
function buildStateSelect({ id, name, value, required }) {
  const sel = document.createElement('select');
  sel.className = 'form-select';
  if (id) sel.id = id;
  sel.name = name;
  sel.required = !!required;
  sel.dataset.stateField = '1';
  const current = String(value ?? '').trim();
  const key = current.toLowerCase();
  let found = '';
  const opts = ['<option value="">— Select State —</option>'];
  fieldRegions().forEach(r => {
    if (!found && key && (r.name.toLowerCase() === key || r.short.toLowerCase() === key || r.tax_code === current)) found = r.name;
    opts.push(`<option value="${escapeHTMLText(r.name)}">${escapeHTMLText(r.name)}</option>`);
  });
  if (current && !found) opts.push(`<option value="${escapeHTMLText(current)}">${escapeHTMLText(current)} (not on the list - choose one)</option>`);
  sel.innerHTML = opts.join('');
  sel.value = found || current;
  return sel;
}

// GSTIN check digit: Luhn mod 36, as GSTN issues them (engines/tax_ids.go).
function gstinChecksumOK(v) {
  const C = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  v = String(v || '').toUpperCase();
  if (v.length !== 15) return false;
  let total = 0;
  for (let i = 0; i < 14; i++) {
    const c = C.indexOf(v[i]);
    if (c < 0) return false;
    const d = c * (i % 2 ? 2 : 1);
    total += Math.floor(d / 36) + d % 36;
  }
  return C[(36 - total % 36) % 36] === v[14];
}

// Australian Business Number: first digit minus 1, weighted, divides by 89.
function abnChecksumOK(v) {
  const w = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19];
  if (!/^[0-9]{11}$/.test(v)) return false;
  return [...v].reduce((sum, ch, i) => sum + (Number(ch) - (i ? 0 : 1)) * w[i], 0) % 89 === 0;
}

function fieldChecksumOK(kind, v) {
  if (kind === 'gstin') return gstinChecksumOK(v);
  if (kind === 'abn') return abnChecksumOK(v);
  return true;
}

// fillStateFromTaxId: a complete, real GSTIN names its state in its first
// two digits - choose it in the same form's State box, as the server would.
function fillStateFromTaxId(input) {
  if (!window.FIELD_META || window.FIELD_META.country !== 'IN') return;
  const v = String(input.value || '').trim().toUpperCase();
  if (v.length !== 15 || !gstinChecksumOK(v)) return;
  const region = fieldRegions().find(r => r.tax_code === v.slice(0, 2));
  const scope = input.closest('form') || input.closest('.modal-body') || document;
  const sel = scope.querySelector('select[data-state-field="1"][name="state"]');
  if (region && sel && sel.value !== region.name) {
    sel.value = region.name;
    sel.dispatchEvent(new Event('change', { bubbles: true }));
  }
}

// taxIdLabel: the field the product calls "gstin" is a TRN in the UAE, a VAT
// number in the UK... - shown by the home country's own name for it.
function taxIdLabel(fieldname, label) {
  const spec = typeof detectFieldFormat === 'function' ? detectFieldFormat(fieldname) : null;
  if (spec && spec.key === 'gstin' && window.FIELD_META && window.FIELD_META.country !== 'IN' && spec.label) return spec.label;
  return label;
}

async function loadMasterCodeSeries(doctype) {
  try {
    const res = await apiFetch(`/api/v1/master-codes/${encodeURIComponent(doctype)}`);
    if (res && res.ok) return await res.json();
  } catch (e) { /* no format shown; the server still numbers the record */ }
  return null;
}

// masterCodeSetupRow: the first time a master type is created, its form asks
// how the codes look - prefix and number of digits, with the first code
// shown as you type. Sent with that first record as _code_format; afterwards
// every new record takes the next number with no question.
function masterCodeSetupRow(series, label, codeInput) {
  const row = document.createElement('div');
  row.className = 'form-group form-group-wide master-code-setup';
  const lengths = [3, 4, 5, 6, 7, 8].map(n => `<option value="${n}"${n === series.length ? ' selected' : ''}>${n}</option>`).join('');
  row.innerHTML = `
    <span class="form-label">${escapeHTMLText(label)} code format (asked once)</span>
    <div style="display:flex; gap:12px; align-items:center; flex-wrap:wrap;">
      <label style="display:flex; gap:6px; align-items:center;">Prefix
        <input class="form-input" name="_code_prefix" maxlength="6" style="width:90px; text-transform:uppercase;" value="${escapeHTMLText(series.prefix)}"></label>
      <label style="display:flex; gap:6px; align-items:center;">Digits
        <select class="form-select" name="_code_length" style="width:80px;">${lengths}</select></label>
      <span>First code: <strong class="master-code-preview"></strong></span>
    </div>`;
  const prefix = row.querySelector('[name="_code_prefix"]');
  const length = row.querySelector('[name="_code_length"]');
  const preview = row.querySelector('.master-code-preview');
  const paint = () => {
    prefix.value = prefix.value.toUpperCase().replace(/[^A-Z0-9]/g, '').replace(/^[0-9]+/, '').slice(0, 6);
    const code = `${prefix.value}${'1'.padStart(Number(length.value), '0')}`;
    preview.textContent = code;
    if (codeInput) codeInput.placeholder = `${code} (first code)`;
  };
  prefix.addEventListener('input', paint);
  length.addEventListener('change', paint);
  paint();
  return row;
}

function readMasterCodeFormat(form) {
  const prefix = form.querySelector('[name="_code_prefix"]');
  const length = form.querySelector('[name="_code_length"]');
  if (!prefix || !length) return null;
  return { prefix: prefix.value.trim().toUpperCase(), length: Number(length.value) };
}

// ---------------------------------------------------------------------------
// Stage 80.1 (user, 2026-10-18, on the New Item form): "Make it easy and
// co-related." Item's ~40 fields were one flat grid in registry order, so
// HSN sat beside Weight and Traceability beside Parent Product Code. The
// generic form still builds every field exactly as before; this only moves
// the finished .form-group nodes into named sections, so nothing the save
// reads changes. A field not listed (an industry profile's own, a tenant's
// custom field) lands in "More details" rather than disappearing.
// ---------------------------------------------------------------------------
const FORM_SECTIONS = {
  Item: [
    { title: 'Basics', fields: ['item_type', 'item_name', 'name', 'code', 'category', 'product_type', 'barcode'] },
    { title: 'Price and tax', fields: ['sale_price', 'mrp', 'standard_cost', 'hsn_code', 'tax_treatment', 'gst_rate'] },
    { title: 'Design and variant', fields: ['family', 'parent_product_code', 'variant_option_values'] },
    { title: 'Batch, serial and expiry', fields: ['tracking_mode', 'grn_batch_number', 'shelf_life_days', 'min_shelf_life_on_receipt_days', 'min_shelf_life_on_pick_days'] },
    { title: 'Barcode and label', closed: true, fields: ['barcode_generate_at', 'barcode_per_receipt_date', 'gtin', 'sticker_template'] },
    { title: 'Handling and storage', closed: true, fields: ['weight', 'volume', 'is_catch_weight', 'temperature_class', 'hazmat_class', 'country_of_origin'] },
    { title: 'Selling and accounting', closed: true, fields: ['commercial_class', 'free_with', 'sellability', 'bill_mode', 'capture_method', 'consumption_method', 'stock_tracking', 'valuation', 'accounting_treatment', 'expense_account'] }
  ]
};

// A closed section still opens on its own when it holds something to see:
// a required box, or (editing) a value other than blank / the item type's.
function groupFormSections(body, doctype, record) {
  const plan = FORM_SECTIONS[doctype];
  if (!plan || body.querySelector(':scope > .form-section, :scope > fieldset, :scope > details')) return;
  const groupOf = name => body.querySelector(`[name="${name}"]`)?.closest('.form-group');
  const placed = new Set();
  const make = (title, groups, closed) => {
    if (!groups.length) return null;
    const section = document.createElement('details');
    section.className = 'form-section';
    const set = groups.some(g => [...g.querySelectorAll('[name]')].some(el => {
      const v = record ? record[el.name] : undefined;
      return el.required || (v !== undefined && v !== null && v !== '' && v !== false && v !== 'Item type default');
    }));
    section.open = !closed || set;
    section.innerHTML = `<summary>${escapeHTMLText(title)}</summary><div class="form-section-grid"></div>`;
    groups.forEach(g => { section.lastChild.appendChild(g); placed.add(g); });
    return section;
  };
  const sections = plan.map(s => make(s.title, s.fields.map(groupOf).filter(Boolean), s.closed));
  const rest = [...body.querySelectorAll(':scope > .form-group')].filter(g => !placed.has(g) && !g.classList.contains('form-group-wide'));
  sections.splice(4, 0, make('More details', rest, false));
  sections.filter(Boolean).forEach(s => body.appendChild(s));
  // Browser validation cannot point at a box inside a closed section.
  if (!body.dataset.sectionHook) {
    body.dataset.sectionHook = '1';
    body.addEventListener('invalid', e => { const s = e.target.closest('details.form-section'); if (s) s.open = true; }, true);
  }
}

// Stage 80.2: variant options were one text box asking the user to type
// "Key:Value;Key:Value". Rows of attribute + value now write that same string
// into the same input, so every reader of variant_option_values (variant
// codes, the duplicate-combination check, PO designs, stickers) is unchanged.
// Attributes come from Setup -> Item Attribute (the ERP's own list, Stage 86.3,
// not PIM's) and are stored by their code, as PO designs store them; a key
// that matches none stays as typed.
async function attachVariantOptionsEditor(input) {
  if (!input || input.dataset.variantEditor) return;
  input.dataset.variantEditor = '1';
  let defs = [];
  let closed = false;
  if (canReadDoctype('ItemAttribute')) {
    const res = await apiFetch('/api/v1/doc/ItemAttribute?limit=500');
    const all = res && res.ok ? ((await res.json()) || []) : [];
    // Stage 81: once the operator has set attributes up from the library they
    // are the whole list - the form offers no "Other..." to invent one.
    defs = all.filter(d => d.status !== 'Inactive');
    closed = all.some(d => d.library_id);
  }
  const byCode = new Map(defs.map(d => [String(d.code || d.id), d]));
  const clean = s => String(s || '').replace(/[:;\s]+/g, ' ').trim();
  const opt = (value, text, on) => `<option value="${escapeHTMLText(value)}"${on ? ' selected' : ''}>${escapeHTMLText(text)}</option>`;
  const OTHER = '__other__';

  input.type = 'hidden';
  input.closest('.form-group')?.classList.add('form-group-wide');
  const wrap = document.createElement('div');
  wrap.className = 'variant-editor';
  input.after(wrap);
  const rows = document.createElement('div');
  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'btn btn-outline btn-sm';
  add.textContent = '+ Add option';
  wrap.append(rows, add);

  const sync = () => {
    input.value = [...rows.children].map(row => {
      const k = clean(row.querySelector('[data-v="key"]').value), v = clean(row.querySelector('[data-v="val"]').value);
      return k && k !== OTHER && v ? `${k}:${v}` : '';
    }).filter(Boolean).join(';');
  };
  const choicesOf = key => String((byCode.get(key) || {}).value_type === 'Select' ? byCode.get(key).value_options || '' : '')
    .split(',').map(s => s.trim()).filter(Boolean);
  const valueControl = (key, value) => {
    const choices = choicesOf(key);
    if (!choices.length) return `<input type="text" class="form-input" data-v="val" placeholder="Value" value="${escapeHTMLText(value)}">`;
    if (value && !choices.includes(value)) choices.push(value);
    return `<select class="form-select" data-v="val">${opt('', '— Value —', !value)}${choices.map(c => opt(c, c, c === value)).join('')}</select>`;
  };
  const keyControl = (key, typed) => {
    if (typed || !defs.length || (key && !byCode.has(key))) {
      return `<input type="text" class="form-input" data-v="key" placeholder="Option, e.g. Size" value="${escapeHTMLText(key)}">`;
    }
    return `<select class="form-select" data-v="key">${opt('', '— Option —', !key)}${defs.map(d => opt(String(d.code || d.id), d.label || d.code, String(d.code || d.id) === key)).join('')}${closed ? '' : opt(OTHER, 'Other…', false)}</select>`;
  };
  const addRow = (key = '', value = '') => {
    const row = document.createElement('div');
    row.className = 'variant-row';
    row.innerHTML = `${keyControl(key)}${valueControl(key, value)}<button type="button" class="btn btn-ghost btn-sm" data-v="remove" aria-label="Remove option">&times;</button>`;
    rows.appendChild(row);
    return row;
  };
  // Only an option picked from the list can change what its value box should
  // be; a typed option name always takes a typed value, so leave it alone.
  rows.addEventListener('change', e => {
    const row = e.target.closest('.variant-row');
    if (e.target.dataset.v === 'key' && e.target.tagName === 'SELECT') {
      if (e.target.value === OTHER) {
        e.target.outerHTML = keyControl('', true);
        row.querySelector('[data-v="key"]').focus();
      }
      const key = row.querySelector('[data-v="key"]').value;
      const val = row.querySelector('[data-v="val"]');
      val.outerHTML = valueControl(key, val.tagName === 'INPUT' && !choicesOf(key).length ? val.value : '');
    }
    sync();
  });
  rows.addEventListener('input', sync);
  rows.addEventListener('click', e => {
    if (e.target.dataset.v !== 'remove') return;
    e.target.closest('.variant-row').remove();
    if (!rows.children.length) addRow();
    sync();
  });
  add.addEventListener('click', () => addRow().querySelector('[data-v="key"]').focus());

  const pairs = String(input.value || '').split(';').map(p => p.split(':')).filter(p => p[0] && p[0].trim());
  pairs.forEach(([k, ...v]) => addRow(k.trim(), v.join(':').trim()));
  if (!pairs.length) addRow();
}

// Stage 81: "All attribute value deletable." An attribute's Select values were
// one comma-separated box; now each value is a chip with × and new ones are
// typed below. Writes the same comma list into the same (hidden) input, so
// every reader of value_options is unchanged. Items that already carry a
// removed value keep it - only new choices stop offering it.
// On an attribute that came from the library, its code and library id are
// shown but not editable (the server keeps them too).
function wireAttributeDefForm(body) {
  const input = body.querySelector('input[name="value_options"]');
  const libraryBox = body.querySelector('input[name="library_id"]');
  const fromLibrary = !!(libraryBox && libraryBox.value);
  if (libraryBox) {
    libraryBox.readOnly = true;
    if (!fromLibrary) libraryBox.closest('.form-group').style.display = 'none';
  }
  const code = body.querySelector('input[name="code"]');
  if (code && fromLibrary) code.readOnly = true;
  if (!input || input.dataset.valueList) return;
  input.dataset.valueList = '1';
  input.type = 'hidden';
  const group = input.closest('.form-group');
  group?.classList.add('form-group-wide');
  const box = document.createElement('div');
  box.className = 'value-list';
  box.innerHTML = '<div class="value-chips"></div><div class="value-add"><input type="text" class="form-input" placeholder="Add a value" aria-label="Add a value"><button type="button" class="btn btn-outline btn-sm">Add</button></div>';
  input.after(box);
  const chips = box.querySelector('.value-chips');
  const add = box.querySelector('.value-add input');
  const values = () => String(input.value || '').split(',').map(s => s.trim()).filter(Boolean);
  const paint = () => {
    chips.innerHTML = values().map((v, i) => `<span class="value-chip">${escapeHTMLText(v)}<button type="button" data-i="${i}" aria-label="Remove ${escapeHTMLText(v)}">&times;</button></span>`).join('')
      || '<span class="text-muted">No values yet.</span>';
  };
  const push = () => {
    const v = add.value.replace(/,/g, ' ').replace(/\s+/g, ' ').trim();
    if (!v) return;
    const list = values();
    if (!list.some(x => x.toLowerCase() === v.toLowerCase())) list.push(v);
    input.value = list.join(',');
    add.value = '';
    paint();
    add.focus();
  };
  chips.addEventListener('click', e => {
    const b = e.target.closest('button[data-i]');
    if (!b) return;
    const list = values();
    list.splice(Number(b.dataset.i), 1);
    input.value = list.join(',');
    paint();
  });
  add.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); push(); } });
  box.querySelector('.value-add button').addEventListener('click', push);
  paint();
  // Values belong to a Select attribute only.
  const type = body.querySelector('[name="value_type"]');
  const follow = () => { if (group) group.style.display = !type || !type.value || type.value === 'Select' ? '' : 'none'; };
  type?.addEventListener('change', follow);
  follow();
}


// --- Stage 85.E8 (77.14): lines from a template ---------------------------------
//
// openLineUpload is the one "Upload lines" every transaction screen uses: a
// template to download (Barcode, Qty), a CSV to pick, each code checked on
// the server (POST /api/v1/lines/resolve - item code, id or barcode), and the
// rows that match nothing LISTED back, never dropped. onRows gets
// [{ sku, name, qty }] for the rows that matched, and adds them its own way.
function parseLineCSV(text) {
  const rows = [];
  String(text || '').split(/\r?\n/).forEach(raw => {
    const line = raw.trim();
    if (!line) return;
    const cells = line.split(/[,;\t]/).map(c => c.trim().replace(/^"(.*)"$/, '$1'));
    rows.push(cells);
  });
  if (rows.length && isNaN(parseFloat(rows[0][1]))) rows.shift(); // the header
  return rows.map(c => ({ code: c[0] || '', qty: parseFloat(c[1]) || 0 })).filter(r => r.code);
}

function openLineUpload({ title = 'Upload lines', hint = '', template = 'lines', onRows }) {
  document.getElementById('line-upload-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'line-upload-dialog';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 560px;" role="dialog" aria-modal="true" aria-labelledby="line-upload-title">
      <div class="modal-header"><h3 class="modal-title" id="line-upload-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-lu-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body" style="display:flex; flex-direction:column; gap:12px;">
        <p style="margin:0;">${escapeHTMLText(hint || 'A CSV with two columns: Barcode (or item code) and Qty. Download the template, fill it in, then pick it here.')}</p>
        <div><button type="button" class="btn btn-outline btn-sm" data-lu-template>Download template</button></div>
        <input type="file" accept=".csv,text/csv,text/plain" class="form-input" data-lu-file aria-label="CSV file">
        <div data-lu-summary class="txn-hint"></div>
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-lu-close>Cancel</button>
        <button type="button" class="btn btn-primary" data-lu-add disabled>Add lines</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-lu-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  overlay.querySelector('[data-lu-template]').addEventListener('click', () => {
    const blob = new Blob(['Barcode,Qty\r\n'], { type: 'text/csv' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `${template}-template.csv`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  });
  let ready = [];
  const summary = overlay.querySelector('[data-lu-summary]');
  const add = overlay.querySelector('[data-lu-add]');
  overlay.querySelector('[data-lu-file]').addEventListener('change', async (e) => {
    const file = e.target.files && e.target.files[0];
    ready = [];
    add.disabled = true;
    if (!file) return;
    const rows = parseLineCSV(await file.text());
    if (!rows.length) { summary.textContent = 'No lines found in that file - the first column is the barcode, the second the quantity.'; return; }
    summary.textContent = 'Checking the codes...';
    const res = await apiFetch('/api/v1/lines/resolve', { method: 'POST', body: JSON.stringify({ codes: rows.map(r => r.code) }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The codes could not be checked.'); summary.textContent = ''; return; }
    const resolved = await res.json();
    const missing = [];
    const badQty = [];
    resolved.forEach((r, i) => {
      if (!r.found) missing.push(rows[i].code);
      else if (!(rows[i].qty > 0)) badQty.push(rows[i].code);
      else ready.push({ sku: r.sku, name: r.name, qty: rows[i].qty });
    });
    summary.innerHTML = `<b>${ready.length} line${ready.length === 1 ? '' : 's'} ready.</b>`
      + (missing.length ? `<div style="color:var(--danger-color);">Not an item (${missing.length}): ${escapeHTMLText(missing.slice(0, 20).join(', '))}${missing.length > 20 ? ', ...' : ''}</div>` : '')
      + (badQty.length ? `<div style="color:var(--danger-color);">No quantity (${badQty.length}): ${escapeHTMLText(badQty.slice(0, 20).join(', '))}</div>` : '');
    add.disabled = !ready.length;
  });
  add.addEventListener('click', () => guardAgainstDoubleSubmit(add, 'Adding...', async () => {
    await onRows(ready);
    close();
    showToast(`${ready.length} line${ready.length === 1 ? '' : 's'} added.`, { variant: 'success' });
  }));
}

// Stage 93.6 (user, 2026-10-18): "It should ask Mandatory field and auto
// linked". A location's Cost Center is never blank: its blank choice reads
// "New: <location name>" and is the one a new location starts on - the
// server makes that cost center on save (engines/location_cost_center.go).
// Picking an existing one links that instead. The options arrive after the
// form is built, so the label is repainted whenever they are replaced.
function wireLocationCostCenter(body) {
  const select = body.querySelector('select[name="cost_center"]');
  if (!select) return;
  const nameInput = body.querySelector('[name="name"]');
  const label = select.closest('.form-group')?.querySelector('.form-label');
  if (label && !label.querySelector('.required')) label.insertAdjacentHTML('beforeend', '<span class="required">*</span>');
  const paint = () => {
    const blank = [...select.options].find(o => o.value === '');
    if (!blank) return;
    const name = String(nameInput?.value || '').trim();
    blank.disabled = false;
    blank.textContent = name ? `New: ${name}` : 'New: named after this location';
  };
  new MutationObserver(paint).observe(select, { childList: true });
  nameInput?.addEventListener('input', paint);
  paint();
}

export { openDatePicker, closeDatePicker, openQuickCreateDialog, attachSmartHSNPicker, wireAttributeDefForm,
  isStateFieldName, isStateCodeFieldName, buildStateSelect, fieldChecksumOK, fillStateFromTaxId, taxIdLabel,
  loadMasterCodeSeries, masterCodeSetupRow, readMasterCodeFormat, groupFormSections, attachVariantOptionsEditor, openLineUpload,
  wireLocationCostCenter };
