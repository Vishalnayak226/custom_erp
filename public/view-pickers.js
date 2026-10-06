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
  const isCode = f => f.auto_generated || (isMaster && f.fieldname.toLowerCase() === 'code');
  const seedField = ['name', 'hsn', 'description'].find(n => fields.some(f => f.fieldname === n));
  const asked = fields.filter(f =>
    f.fieldname !== 'id' && !f.mirrored && !isCode(f) && !isDerivedCompanionField(f.fieldname) &&
    (f.mandatory || f.fieldname === seedField));
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

    asked.forEach(f => {
      const fg = document.createElement('div');
      fg.className = 'form-group';
      const id = `qc-${doctype}-${f.fieldname}`;
      fg.innerHTML = `<label class="form-label" for="${id}"></label>`;
      fg.querySelector('label').innerHTML = `${escapeHTMLText(getTranslatedLabel(f.label))}${f.mandatory ? '<span class="required">*</span>' : ''}`;
      let input;
      if (f.fieldtype === 'Select') {
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
    if (doctype === 'Item') {
      attachHSNPicker(holder.querySelector('[name="hsn_code"]'), holder.querySelector('[name="gst_rate"]'));
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
      submitBtn.disabled = true;
      submitBtn.textContent = 'Creating…';
      try {
        // Same numbering the full form uses for a master's code (Stage 30.6
        // leaves masters on the sequence endpoint).
        const codeField = isMaster && fields.find(f => f.fieldname.toLowerCase() === 'code' && !f.auto_generated);
        if (codeField) {
          const seqRes = await apiFetch('/api/v1/sequence', {
            method: 'POST',
            body: JSON.stringify({ doc_type: doctype, store_code: 'HQ', financial_year: new Date().getFullYear().toString() })
          });
          if (!seqRes || !seqRes.ok) {
            errorEl.textContent = seqRes ? await getErrorMessage(seqRes, 'Could not number the new record.') : 'Could not number the new record.';
            errorEl.classList.remove('hidden');
            return;
          }
          payload[codeField.fieldname] = (await seqRes.json()).code;
        }
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

export { openDatePicker, closeDatePicker, openQuickCreateDialog };
