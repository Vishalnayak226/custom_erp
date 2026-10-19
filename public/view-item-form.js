// Stage 89 - the Item master, asked once (user, 2026-10-18: "While creating
// item it self it should ask all this. User will never revisit this. & It need
// to smart filled / Auto filled with a single selection").
//
// Loaded on demand (loadViewModule) by the record form for Item / Product
// Type / Category, by the New Item quick-create, and by Configuration -> Item
// fields. Everything the org decides comes from one read,
// GET /api/v1/inventory/item-setup (engines/item_setup.go): its defaults, the
// kinds of item and what each passes on, the item name order and where each
// field is asked. Shared app services (apiFetch, escapeHTMLText,
// attachLinkTypeahead, lookupLinkDoc, ...) resolve from the classic shell.

if (!document.getElementById('view-item-form-css')) {
  const style = document.createElement('style');
  style.id = 'view-item-form-css';
  style.textContent = `
.item-name-hint { font-size: 12.5px; color: var(--text-muted); margin-top: 6px; }
.item-name-hint strong { color: var(--text-main); font-weight: 600; }
.item-attr-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px 16px; }
.item-attr-grid .form-group { margin: 0; }
.item-attr-kept { font-size: 12.5px; color: var(--text-muted); margin-top: 8px; }
.item-attr-add { display: flex; gap: 6px; }
.item-attr-add .form-input { flex: 1; min-width: 0; }
.item-locked-note { font-size: 12.5px; color: var(--warning-color, #b7791f); margin-top: 6px; }
.item-fields-table { width: 100%; border-collapse: separate; border-spacing: 0; font-size: 13.5px; }
.item-fields-table th, .item-fields-table td { padding: 8px 10px; border-bottom: 1px solid var(--border-color); text-align: left; vertical-align: middle; }
.item-fields-table th { font-size: 12px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: .03em; }
.item-fields-table td.c, .item-fields-table th.c { text-align: center; }
.item-fields-table tr.is-hidden td:not(:first-child):not(:last-child) { opacity: .45; }
.item-fields-table .form-select { min-width: 120px; padding-top: 4px; padding-bottom: 4px; }
.item-fields-group td { background: var(--bg-color); font-weight: 600; }
.item-fields-actions { display: flex; align-items: center; gap: 12px; margin: 16px 0 0; flex-wrap: wrap; }
.attr-map-row { border: 1px solid var(--border-color); border-radius: 8px; padding: 10px 12px; margin-bottom: 8px; }
.attr-map-row > label { display: flex; align-items: center; gap: 8px; font-weight: 600; cursor: pointer; }
.attr-map-values { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0 0 26px; }
.attr-map-chip { border: 1px solid var(--border-color); background: var(--panel-bg); color: var(--text-main); border-radius: 999px; padding: 3px 10px; font-size: 12.5px; cursor: pointer; }
.attr-map-chip.on { background: var(--primary-color); border-color: var(--primary-color); color: var(--on-primary); }
.attr-map-note { font-size: 12.5px; color: var(--text-muted); margin: 6px 0 10px; }
.attr-map-used { font-size: 12px; color: var(--text-muted); margin-left: 26px; }
@media (max-width: 640px) { .item-fields-table { font-size: 12.5px; } .item-fields-table th, .item-fields-table td { padding: 6px; } }
`;
  document.head.appendChild(style);
}

function loadItemSetup(params) {
  const qs = params ? `?${new URLSearchParams(params)}` : '';
  return apiFetch(`/api/v1/inventory/item-setup${qs}`).then(r => (r && r.ok ? r.json() : null)).catch(() => null);
}

// The three dimensions that decide how stock is posted (72.12 / MASTER-0407).
const TREATMENT_FIELDS = ['stock_tracking', 'valuation', 'accounting_treatment'];

const INHERIT_OPTION = /^(item type default|client default|company default)$/i;
const ADD_VALUE = '__add_value__';

function setFieldLabel(control, text, required) {
  const label = control?.closest('.form-group')?.querySelector('label.form-label');
  if (label) label.innerHTML = `${escapeHTMLText(text)}${required ? '<span class="required">*</span>' : ''}`;
}

// A short name for a kind of item: "Normal product - bought and sold" -> "Normal product".
function shortKindName(kind) { return String(kind?.label || kind?.code || '').split(' - ')[0]; }

function kindOptions(setup, current) {
  const opts = setup.kinds.map(k => ({ value: k.code, text: k.label }));
  if (current && !opts.some(o => o.value.toLowerCase() === String(current).toLowerCase())) opts.push({ value: current, text: current });
  return opts;
}

function buildKindSelect(setup, from, current) {
  const sel = document.createElement('select');
  sel.className = 'form-select';
  sel.name = 'item_type';
  if (from?.id) sel.id = from.id;
  sel.required = true;
  const opts = kindOptions(setup, current);
  sel.innerHTML = opts.map(o => `<option value="${escapeHTMLText(o.value)}">${escapeHTMLText(o.text)}</option>`).join('');
  const want = String(current || setup.default_kind || '').toLowerCase();
  sel.value = (opts.find(o => o.value.toLowerCase() === want) || opts[0] || {}).value || '';
  return sel;
}

// fillFromItemName copies what an item name carries into the form: category
// and type (shown, read-only), HSN and GST (the HSN picker then offers the
// code's rates), and the item's own name when it is still blank.
async function fillFromItemName(scope, ref, { userPicked, hint } = {}) {
  const el = name => scope.querySelector(`[name="${name}"]`);
  if (!ref) { if (hint) hint.textContent = ''; return null; }
  const doc = await lookupLinkDoc('ItemName', ref);
  if (!doc) return null;
  const [cat, typ] = await Promise.all([
    doc.category ? lookupLinkDoc('Category', doc.category) : null,
    doc.product_type ? lookupLinkDoc('ProductType', doc.product_type) : null
  ]);
  const catName = cat?.name || doc.category || '';
  const typName = typ?.name || doc.product_type || '';
  if (el('category')) el('category').value = catName;
  if (el('product_type')) el('product_type').value = typName;
  if (hint) {
    hint.innerHTML = [catName && `Category <strong>${escapeHTMLText(catName)}</strong>`, typName && `Type <strong>${escapeHTMLText(typName)}</strong>`,
      doc.hsn_code && `HSN <strong>${escapeHTMLText(doc.hsn_code)}</strong>`,
      (doc.gst_rate !== undefined && doc.gst_rate !== null && doc.gst_rate !== '') && `GST <strong>${escapeHTMLText(String(doc.gst_rate))}%</strong>`]
      .filter(Boolean).join(' · ');
  }
  if (userPicked) {
    const hsn = el('hsn_code');
    if (hsn && doc.hsn_code && hsn.value !== doc.hsn_code) {
      hsn.value = doc.hsn_code;
      hsn.dispatchEvent(new Event('change', { bubbles: true }));
    }
    const gst = el('gst_rate');
    if (gst && doc.gst_rate !== undefined && doc.gst_rate !== null && doc.gst_rate !== '') {
      gst.value = doc.gst_rate;
      gst.dispatchEvent(new Event('input', { bubbles: true }));
      gst.dispatchEvent(new Event('change', { bubbles: true }));
    }
    const nm = el('name');
    if (nm && !nm.value.trim() && doc.name) nm.value = doc.name;
  }
  return doc;
}

// The Item Name box: the shared picker, with "+ Create" making a new one from
// Category + Type right there.
function buildItemNameInput(from, value) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'form-input';
  input.name = 'item_name';
  if (from?.id) input.id = from.id;
  input.required = true;
  input.placeholder = 'Pick or type, e.g. Antique Necklace';
  input.autocomplete = 'off';
  from.replaceWith(input);
  attachLinkTypeahead(input, 'ItemName', { showAllOnFocus: true });
  if (value) input.value = value;
  return input;
}

// --- The full Item form --------------------------------------------------------

async function wireItemForm(body, { isEdit = false, record = {} } = {}) {
  if (!body || body.dataset.itemForm) return;
  body.dataset.itemForm = '1';
  const rec = record || {};
  const setup = await loadItemSetup(isEdit && (rec.code || rec.id) ? { item: rec.code || rec.id } : null);
  if (!setup) return;
  const el = name => body.querySelector(`[name="${name}"]`);
  const groupOf = name => el(name)?.closest('.form-group');

  // 1. Where each field is asked: a field the org asks at the GRN, or hides,
  // leaves this form - hidden, not removed, so its stored value saves as is.
  setup.fields.forEach(r => {
    if (r.kind !== 'field' || r.fixed) return;
    const g = groupOf(r.key);
    if (!g || (r.item && !r.hidden)) return;
    g.classList.add('hidden');
    g.querySelectorAll('[required]').forEach(x => { x.required = false; });
  });

  // 2. What kind of item is this? - asked first, Normal product pre-selected.
  let kindSel = null;
  const oldKind = el('item_type');
  if (oldKind) {
    kindSel = buildKindSelect(setup, oldKind, rec.item_type);
    oldKind.replaceWith(kindSel);
    setFieldLabel(kindSel, 'What kind of item is this?', true);
  }
  const kindOf = () => setup.kinds.find(k => k.code.toLowerCase() === String(kindSel?.value || setup.default_kind).toLowerCase()) || setup.kinds[0];
  // Every dropdown that inherits says what it inherits, instead of a blank.
  const paintInherits = () => {
    const k = kindOf();
    if (!k) return;
    body.querySelectorAll('select').forEach(sel => {
      const inh = [...sel.options].find(o => INHERIT_OPTION.test(o.value));
      if (!inh) return;
      const v = (k.inherits || {})[sel.name];
      inh.textContent = v ? `${v} (from ${shortKindName(k)})` : `As set for ${shortKindName(k)}`;
      if (!sel.value) sel.value = inh.value;
    });
    const exp = el('expense_account');
    if (exp) exp.placeholder = k.inherits?.expense_account ? `${k.inherits.expense_account} (from ${shortKindName(k)})` : '';
  };
  kindSel?.addEventListener('change', paintInherits);
  paintInherits();

  // 2b. An item with stock (89.8): stock tracking, valuation and accounting
  // change only through the admin's correction. A new kind that keeps them
  // (Product -> Accessory) is a plain edit; one that changes them steps back
  // and opens the correction (an admin) or says to tell the admin.
  if (isEdit && setup.item_has_stock) {
    const ref = rec.code || rec.id;
    const label = rec.name || ref;
    const keepKind = kindSel ? kindSel.value : '';
    const treatmentOf = code => {
      const k = setup.kinds.find(x => x.code.toLowerCase() === String(code).toLowerCase());
      return k ? TREATMENT_FIELDS.map(n => k.inherits[n]).join('|') : '';
    };
    kindSel?.addEventListener('change', async () => {
      const ownSet = TREATMENT_FIELDS.some(n => { const v = el(n)?.value; return v && !INHERIT_OPTION.test(v); });
      if (ownSet || treatmentOf(kindSel.value) === treatmentOf(keepKind)) return;
      const wanted = kindSel.value;
      kindSel.value = keepKind;
      paintInherits();
      if (setup.is_admin) openTreatmentCorrection({ target: 'Item', ref, label, form: body, preset: { item_type: wanted } });
      else await showCustomAlert(`${label} has stock, so changing it to "${(setup.kinds.find(k => k.code === wanted) || {}).label || wanted}" changes how it is stocked or accounted. Only an admin can do that - tell your admin.`, 'Item has stock');
    });
    lockTreatment(body, setup, { target: 'Item', ref, label, anchor: kindSel });
  }

  // 3. A new item starts from the org's defaults (Configuration -> Inventory).
  if (!isEdit) {
    const setIfBlank = (name, v) => {
      const x = el(name);
      if (!x || v === undefined || v === null || v === '' || x.value) return;
      x.value = v;
      x.dispatchEvent(new Event('change', { bubbles: true }));
    };
    setIfBlank('tax_treatment', setup.defaults.tax_treatment);
    setIfBlank('tracking_mode', setup.defaults.tracking_mode);
    ['shelf_life_days', 'min_shelf_life_on_receipt_days', 'min_shelf_life_on_pick_days'].forEach(n => setIfBlank(n, setup.defaults[n]));
  }

  // 4. The item name: one pick fills category, type, HSN and GST.
  let nameInput = null;
  const hint = document.createElement('div');
  hint.className = 'item-name-hint';
  const oldName = el('item_name');
  if (oldName) {
    nameInput = buildItemNameInput(oldName, rec.item_name || '');
    setFieldLabel(nameInput, 'Item name (category + type)', true);
    nameInput.closest('.form-group').appendChild(hint);
    nameInput.addEventListener('change', async () => {
      await fillFromItemName(body, nameInput.value, { userPicked: true, hint });
      renderAttributes();
    });
  }
  ['category', 'product_type'].forEach(n => {
    const x = el(n);
    if (!x) return;
    x.readOnly = true;
    x.tabIndex = -1;
    x.title = 'Comes from the item name';
    x.placeholder = 'From the item name';
  });

  // 5. Basics in the order a person answers them.
  const basics = groupOf('name')?.parentElement;
  if (basics) ['item_type', 'item_name', 'name', 'code', 'category', 'product_type', 'barcode'].forEach(n => { const g = groupOf(n); if (g) basics.appendChild(g); });

  // 6. Attributes as dropdowns, each from its own list (89.3), following the
  // item name's type when the org maps them (89.5).
  const variantInput = el('variant_option_values');
  let attrHost = null;
  if (variantInput) {
    const g = variantInput.closest('.form-group');
    g?.querySelector('.variant-editor')?.remove();
    setFieldLabel(variantInput, 'Attributes', false);
    attrHost = document.createElement('div');
    variantInput.after(attrHost);
  }
  const linkedFields = setup.fields.filter(r => r.kind === 'field' && r.list && r.item && !r.hidden);
  const canAdd = canUpdateDoctype('ItemAttribute');
  let renderSeq = 0;
  async function renderAttributes() {
    const seq = ++renderSeq;
    const res = await apiFetch(`/api/v1/inventory/item-attributes?item_name=${encodeURIComponent(nameInput?.value || '')}`);
    const set = res && res.ok ? await res.json() : null;
    if (!set || seq !== renderSeq) return;
    const byCode = new Map(set.attributes.map(a => [a.code.toUpperCase(), a]));

    // Free-text fields that have their own list become dropdowns of it.
    linkedFields.forEach(r => {
      const box = el(r.key);
      const g = box?.closest('.form-group');
      if (!box || !g) return;
      const a = byCode.get(String(r.list).toUpperCase());
      if (!a && set.source !== 'all') { g.classList.add('hidden'); return; } // not for this type; value kept
      g.classList.remove('hidden');
      if (!a || a.value_type !== 'Select') return;
      g.replaceChild(valueSelect(a, box.value, box.name, box.id, nameInput), box);
    });

    // The item's other attributes, written into variant options.
    if (!attrHost || !variantInput) return;
    const asked = set.attributes.filter(a => a.item && !setup.fields.some(r => r.key === `attr:${a.code}` && r.linked_field));
    const pairs = String(variantInput.value || '').split(';').map(p => p.split(':')).filter(p => p[0] && p[0].trim())
      .map(([k, ...v]) => [k.trim(), v.join(':').trim()]);
    const current = new Map(pairs.map(([k, v]) => [k.toUpperCase(), v]));
    const kept = pairs.filter(([k]) => !asked.some(a => a.code.toUpperCase() === k.toUpperCase()));
    attrHost.innerHTML = asked.length ? '' : `<p class="text-muted" style="font-size:12.5px; margin:0;">${set.source === 'all' ? 'No item attributes yet - add them under Setup → Item Attribute.' : 'Nothing is mapped to ask for this type.'}</p>`;
    const grid = document.createElement('div');
    grid.className = 'item-attr-grid';
    asked.forEach(a => {
      const fg = document.createElement('div');
      fg.className = 'form-group';
      fg.innerHTML = `<label class="form-label">${escapeHTMLText(a.label)}</label>`;
      const v = current.get(a.code.toUpperCase()) || '';
      const control = a.value_type === 'Select' ? valueSelect(a, v, '', '', nameInput) : (() => {
        const i = document.createElement('input');
        i.className = 'form-input';
        i.type = a.value_type === 'Number' ? 'number' : 'text';
        i.value = v;
        return i;
      })();
      control.dataset.attr = a.code;
      fg.appendChild(control);
      grid.appendChild(fg);
    });
    if (asked.length) attrHost.appendChild(grid);
    if (kept.length) {
      const note = document.createElement('div');
      note.className = 'item-attr-kept';
      note.textContent = `Also on this item: ${kept.map(([k, v]) => `${k}: ${v}`).join(', ')}`;
      attrHost.appendChild(note);
    }
    const sync = () => {
      const chosen = [...grid.querySelectorAll('[data-attr]')]
        .map(c => [c.dataset.attr, String(c.value || '').replace(/[:;]+/g, ' ').trim()])
        .filter(([, val]) => val && val !== ADD_VALUE);
      variantInput.value = [...chosen, ...kept].map(([k, val]) => `${k}:${val}`).join(';');
    };
    grid.addEventListener('change', sync);
    grid.addEventListener('input', sync);
  }

  // valueSelect is one attribute's dropdown with "+ Add a new <label>" at the
  // end - the value goes on the attribute's own list (user: "It should ask to
  // add in Master if not present").
  function valueSelect(a, value, name, id, itemNameBox) {
    const sel = document.createElement('select');
    sel.className = 'form-select';
    if (name) sel.name = name;
    if (id) sel.id = id;
    const values = [...a.values];
    if (value && !values.some(v => v.toLowerCase() === value.toLowerCase())) values.push(value);
    sel.innerHTML = `<option value="">— Choose —</option>${values.map(v => `<option value="${escapeHTMLText(v)}">${escapeHTMLText(v)}</option>`).join('')}` +
      (canAdd ? `<option value="${ADD_VALUE}">+ Add a new ${escapeHTMLText(a.label)}…</option>` : '');
    sel.value = values.find(v => v.toLowerCase() === String(value || '').toLowerCase()) || '';
    let last = sel.value;
    sel.addEventListener('change', async e => {
      if (sel.value !== ADD_VALUE) { last = sel.value; return; }
      e.stopImmediatePropagation();
      sel.value = last;
      const typed = await showCustomPrompt(`New ${a.label} - it is added to the ${a.label} list for every item.`, '', `Add ${a.label}`);
      const v = String(typed || '').replace(/[,:;|]+/g, ' ').trim();
      if (!v) return;
      const res = await apiFetch('/api/v1/inventory/attribute-value', {
        method: 'POST', body: JSON.stringify({ code: a.code, value: v, item_name: itemNameBox?.value || '' })
      });
      if (!res) return;
      if (!res.ok) { await showApiError(res, `Could not add ${v}.`); return; }
      a.values.push(v);
      const opt = document.createElement('option');
      opt.value = v;
      opt.textContent = v;
      sel.insertBefore(opt, sel.querySelector(`option[value="${ADD_VALUE}"]`));
      sel.value = v;
      last = v;
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      showToast(`${v} added to ${a.label}.`, { variant: 'success' });
    });
    return sel;
  }

  if (nameInput && nameInput.value) await fillFromItemName(body, nameInput.value, { userPicked: false, hint });
  await renderAttributes();
  hideEmptySections(body);
  return { setup };
}

// A section whose every field is asked elsewhere disappears too.
function hideEmptySections(body) {
  body.querySelectorAll('details.form-section').forEach(s => {
    const visible = [...s.querySelectorAll(':scope > .form-section-grid > .form-group')].some(g => !g.classList.contains('hidden'));
    s.classList.toggle('hidden', !visible);
  });
}

// --- Treatment correction after stock moves (89.8) ----------------------------------

function lockTreatment(body, setup, { target, ref, label, anchor }) {
  TREATMENT_FIELDS.forEach(n => {
    const c = body.querySelector(`select[name="${n}"]`);
    if (c) { c.disabled = true; c.title = 'Has stock - changed only by an admin correction'; }
  });
  const host = (anchor || body.querySelector('select[name="stock_tracking"]'))?.closest('.form-group');
  if (!host) return;
  const what = target === 'ItemType' ? 'item type has items with stock' : 'item has stock';
  const note = document.createElement('div');
  note.className = 'item-locked-note';
  note.textContent = setup.is_admin
    ? `This ${what}, so how it is stocked, valued and accounted changes only through a correction, which shows the effect first.`
    : `This ${what}. Only an admin can correct how it is stocked, valued or accounted - tell your admin.`;
  host.appendChild(note);
  if (setup.is_admin) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-outline btn-sm';
    btn.style.marginTop = '6px';
    btn.textContent = 'Correct treatment…';
    btn.addEventListener('click', () => openTreatmentCorrection({ target, ref, label, form: body }));
    host.appendChild(btn);
  }
}

const money = v => (Number(v) || 0).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

async function openTreatmentCorrection({ target, ref, label, form, preset = {} }) {
  const setup = await loadItemSetup();
  if (!setup) return;
  document.getElementById('treatment-correction')?.remove();
  const overlay = document.createElement('div');
  overlay.id = 'treatment-correction';
  overlay.className = 'modal-overlay open quick-create-overlay';
  overlay.setAttribute('role', 'dialog');
  overlay.setAttribute('aria-modal', 'true');
  const optionsOf = n => {
    const src = form?.querySelector(`select[name="${n}"]`);
    return src ? [...src.options].filter(o => o.value).map(o => `<option value="${escapeHTMLText(o.value)}"${o.value === src.value ? ' selected' : ''}>${escapeHTMLText(o.textContent)}</option>`).join('') : '';
  };
  const dimLabel = { stock_tracking: 'Stock tracking', valuation: 'Valuation', accounting_treatment: 'Accounting treatment' };
  overlay.innerHTML = `
    <div class="modal-container modal-container-wide">
      <div class="modal-header"><h3 class="modal-title"></h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <div style="display:grid; grid-template-columns:repeat(auto-fill,minmax(220px,1fr)); gap:12px 16px;">
          ${target === 'Item' ? `<div class="form-group"><label class="form-label" for="tc-kind">It should be</label>
            <select class="form-select" id="tc-kind">${setup.kinds.map(k => `<option value="${escapeHTMLText(k.code)}">${escapeHTMLText(k.label)}</option>`).join('')}</select></div>` : ''}
          ${TREATMENT_FIELDS.map(n => `<div class="form-group"><label class="form-label" for="tc-${n}">${dimLabel[n]}</label>
            <select class="form-select" id="tc-${n}" data-dim="${n}">${optionsOf(n)}</select></div>`).join('')}
        </div>
        <button type="button" class="btn btn-primary" id="tc-review" style="margin-top:12px;">Review the impact</button>
        <div id="tc-preview" style="margin-top:16px;"></div>
      </div>
    </div>`;
  overlay.querySelector('.modal-title').textContent = `Correct treatment - ${label}`;
  document.body.appendChild(overlay);
  const kindBox = overlay.querySelector('#tc-kind');
  if (kindBox) kindBox.value = preset.item_type || form?.querySelector('[name="item_type"]')?.value || setup.default_kind;
  // A new kind resets the item's own overrides to "as the kind".
  kindBox?.addEventListener('change', () => TREATMENT_FIELDS.forEach(n => {
    const sel = overlay.querySelector(`#tc-${n}`);
    const inh = [...sel.options].find(o => INHERIT_OPTION.test(o.value));
    if (inh) sel.value = inh.value;
  }));
  if (preset.item_type) kindBox?.dispatchEvent(new Event('change'));
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  const out = overlay.querySelector('#tc-preview');
  let unitValue = null;

  const change = () => {
    const ch = { target, ref };
    if (kindBox) ch.item_type = kindBox.value;
    TREATMENT_FIELDS.forEach(n => { ch[n] = overlay.querySelector(`#tc-${n}`).value; });
    if (unitValue !== null) ch.unit_value = unitValue;
    return ch;
  };
  const review = async () => {
    out.innerHTML = '<p class="text-muted">Working out the impact…</p>';
    const res = await apiFetch('/api/v1/inventory/treatment-correction/preview', { method: 'POST', body: JSON.stringify(change()) });
    if (!res) return;
    if (!res.ok) { out.innerHTML = ''; await showApiError(res, 'Could not work out the impact.'); return; }
    renderPreview(await res.json());
  };
  overlay.querySelector('#tc-review').addEventListener('click', review);

  function renderPreview(pv) {
    if (pv.no_correction_needed) {
      out.innerHTML = `<p>This change does not touch stock tracking, valuation or accounting, so nothing needs correcting - close this and save it on the form.</p>`;
      return;
    }
    const rows = [['kind', 'Kind of item'], ['stock_tracking', 'Stock tracking'], ['valuation', 'Valuation'], ['accounting_treatment', 'Accounting'], ['sellability', 'At the till'], ['expense_account', 'Expense account']];
    const changed = k => String(pv.before[k] || '') !== String(pv.after[k] || '');
    const stockRows = pv.items.filter(i => i.on_hand > 0);
    const askUnit = pv.needs_unit_value || pv.items.some(i => ['entered', 'standard cost', 'last costed receipt'].includes(i.unit_value_source));
    out.innerHTML = `
      <h4 style="margin:0 0 6px;">What changes</h4>
      <table class="item-fields-table"><thead><tr><th></th><th>Now</th><th>After</th></tr></thead><tbody>
        ${rows.map(([k, t]) => `<tr${changed(k) ? ' style="font-weight:600;"' : ''}><td>${t}</td><td>${escapeHTMLText(pv.before[k] || '—')}</td><td>${escapeHTMLText(pv.after[k] || '—')}</td></tr>`).join('')}
      </tbody></table>
      <h4 style="margin:16px 0 6px;">Stock it affects</h4>
      ${stockRows.length ? `<div style="overflow-x:auto;"><table class="item-fields-table"><thead><tr><th>Item</th><th>Stores (on hand)</th><th>Qty</th><th>Value per unit</th><th>Value</th></tr></thead><tbody>
        ${stockRows.map(i => `<tr><td>${escapeHTMLText(i.code)}${i.name ? ` - ${escapeHTMLText(i.name)}` : ''}</td>
          <td>${i.stores.map(st => `${escapeHTMLText(st.location)}: ${st.on_hand}`).join(', ')}</td><td>${i.on_hand}${i.write_off ? ' <em>(leaves stock)</em>' : ''}</td>
          <td>₹${money(i.unit_value)} <span class="text-muted">(${escapeHTMLText(i.unit_value_source || '—')})</span></td><td>₹${money(i.value)}</td></tr>`).join('')}
      </tbody></table></div>` : '<p class="text-muted">No stock on hand now - nothing moves; only the item changes.</p>'}
      ${askUnit ? `<div class="form-group" style="max-width:300px; margin-top:8px;">
        <label class="form-label" for="tc-unit">Value per unit coming into Inventory (₹)</label>
        <div class="item-attr-add"><input class="form-input" type="number" step="any" min="0" id="tc-unit" value="${unitValue ?? ''}"><button type="button" class="btn btn-outline btn-sm" id="tc-recalc">Recalculate</button></div></div>` : ''}
      <h4 style="margin:16px 0 6px;">Entry that will post</h4>
      ${pv.journal.length ? `<table class="item-fields-table"><thead><tr><th>Account</th><th class="c">Debit</th><th class="c">Credit</th></tr></thead><tbody>
        ${pv.journal.map(j => `<tr><td>${escapeHTMLText(j.account)} ${escapeHTMLText(j.name || '')}</td><td class="c">${j.debit ? '₹' + money(j.debit) : ''}</td><td class="c">${j.credit ? '₹' + money(j.credit) : ''}</td></tr>`).join('')}
      </tbody></table>` : '<p class="text-muted">No journal entry - no stock value moves.</p>'}
      <h4 style="margin:16px 0 6px;">From now on</h4>
      <ul style="margin:0 0 0 18px;">${pv.from_now.map(t => `<li>${escapeHTMLText(t)}</li>`).join('')}</ul>
      ${pv.open_documents.length ? `<h4 style="margin:16px 0 6px;">Open documents that name it</h4><ul style="margin:0 0 0 18px;">${pv.open_documents.map(t => `<li>${escapeHTMLText(t)}</li>`).join('')}</ul>
        <p class="text-muted" style="font-size:12.5px;">They are not changed; lines posted after this follow the new treatment.</p>` : ''}
      ${pv.blockers.length ? `<div class="login-error" style="margin-top:14px;"><strong>Cannot be made yet:</strong><ul style="margin:6px 0 0 18px;">${pv.blockers.map(t => `<li>${escapeHTMLText(t)}</li>`).join('')}</ul></div>` : `
      <h4 style="margin:18px 0 6px;">Confirm</h4>
      <div class="form-group"><label class="form-label" for="tc-reason">Why is this being corrected?<span class="required">*</span></label>
        <textarea class="form-input" id="tc-reason" rows="2" placeholder="e.g. Created as a normal product by mistake - it is packaging"></textarea></div>
      <div class="form-group" style="max-width:320px;"><label class="form-label" for="tc-confirm">Type <strong>${escapeHTMLText(pv.code)}</strong> to confirm<span class="required">*</span></label>
        <input class="form-input" id="tc-confirm" autocomplete="off"></div>
      <button type="button" class="btn btn-primary" id="tc-apply" disabled>${pv.second_admin ? 'Send to a second admin for approval' : 'Apply the correction'}</button>`}`;
    out.querySelector('#tc-recalc')?.addEventListener('click', () => {
      const v = out.querySelector('#tc-unit').value;
      unitValue = v === '' ? null : Number(v);
      review();
    });
    const reason = out.querySelector('#tc-reason');
    const confirmBox = out.querySelector('#tc-confirm');
    const apply = out.querySelector('#tc-apply');
    if (!apply) return;
    const ready = () => { apply.disabled = !(reason.value.trim() && confirmBox.value.trim().toLowerCase() === String(pv.code).toLowerCase()); };
    reason.addEventListener('input', ready);
    confirmBox.addEventListener('input', ready);
    apply.addEventListener('click', async () => {
      apply.disabled = true;
      const res = await apiFetch('/api/v1/inventory/treatment-correction/apply', {
        method: 'POST', body: JSON.stringify({ ...change(), reason: reason.value.trim(), confirm: confirmBox.value.trim(), token: pv.token })
      });
      if (!res) { ready(); return; }
      if (!res.ok) { await showApiError(res, 'The correction was not made.'); ready(); return; }
      const done = await res.json();
      close();
      document.querySelector('#dynamic-modal.open .modal-close')?.click();
      showToast(done.status === 'Pending Approval'
        ? `Sent for approval (${done.id}) - another admin approves it under Approvals.`
        : `Corrected (${done.id}).${done.preview?.total_value ? ` ₹${money(done.preview.total_value)} moved.` : ''}`, { variant: 'success' });
      if (typeof renderView === 'function' && typeof currentView !== 'undefined') renderView(currentView);
    });
  }
}

// The Item Type form (89.7.3 / 89.8.6): "Client default" says what it means,
// and a type whose items have stock is corrected the same way as an item.
async function wireItemTypeForm(body, record) {
  if (!body || body.dataset.itemTypeForm) return;
  body.dataset.itemTypeForm = '1';
  const rec = record || {};
  const setup = await loadItemSetup(rec.id ? { item_type: rec.id } : null);
  if (!setup) return;
  body.querySelectorAll('select').forEach(sel => {
    const inh = [...sel.options].find(o => INHERIT_OPTION.test(o.value));
    const v = setup.company?.[sel.name];
    if (inh && v) inh.textContent = `Client default (${v})`;
  });
  if (rec.id && setup.item_has_stock) lockTreatment(body, setup, { target: 'ItemType', ref: rec.id, label: rec.code || rec.id });
}

// --- The New Item quick-create (from a PO or any picker) -------------------------

async function wireItemQuickCreate(holder) {
  if (!holder || holder.dataset.itemForm) return;
  holder.dataset.itemForm = '1';
  const setup = await loadItemSetup();
  if (!setup) return;
  const el = name => holder.querySelector(`[name="${name}"]`);
  const oldKind = el('item_type');
  if (oldKind) {
    const sel = buildKindSelect(setup, oldKind, '');
    oldKind.replaceWith(sel);
    setFieldLabel(sel, 'What kind of item is this?', true);
  }
  const oldName = el('item_name');
  if (oldName) {
    const input = buildItemNameInput(oldName, '');
    setFieldLabel(input, 'Item name (category + type)', true);
    const hint = document.createElement('div');
    hint.className = 'item-name-hint';
    input.closest('.form-group').appendChild(hint);
    input.addEventListener('change', () => fillFromItemName(holder, input.value, { userPicked: true, hint }));
  }
}

// --- Attributes for this type / category (89.5) -------------------------------------

// The map is "COLOR:Red|Gold;POLISH": an attribute ticked with no value picked
// offers all its values. Shown on the Product Type and Category forms.
async function wireAttributeMapForm(body, doctype, record) {
  const input = body.querySelector('[name="attribute_map"]');
  if (!input || input.dataset.attrMap) return;
  input.dataset.attrMap = '1';
  const [setup, attrsRes] = await Promise.all([loadItemSetup(), apiFetch('/api/v1/doc/ItemAttribute?limit=500')]);
  const attrs = ((attrsRes && attrsRes.ok ? await attrsRes.json() : []) || []).filter(a => a.status !== 'Inactive');
  input.type = 'hidden';
  const g = input.closest('.form-group');
  g?.classList.add('form-group-wide');
  const host = document.createElement('div');
  input.after(host);
  const parse = s => {
    const m = new Map();
    String(s || '').split(';').forEach(part => {
      const [code, vals = ''] = part.split(':');
      if (code && code.trim()) m.set(code.trim().toUpperCase(), vals.split('|').map(v => v.trim()).filter(Boolean));
    });
    return m;
  };
  let map = parse(input.value);
  const write = () => {
    input.value = [...map.entries()].map(([code, vals]) => (vals.length ? `${code}:${vals.join('|')}` : code)).join(';');
  };
  const what = doctype === 'Category' ? 'category' : 'type';
  const note = setup && !setup.attribute_mapping
    ? `Used only once "Attributes follow the item's type" is on (Configuration → Inventory). Until then every item offers every attribute.`
    : `Tick what a ${what} offers; pick values to offer only those. Nothing ticked = everything is offered.`;
  const draw = (used) => {
    if (!attrs.length) { host.innerHTML = '<p class="text-muted" style="font-size:12.5px;">No item attributes yet - add them under Setup → Item Attribute.</p>'; return; }
    host.innerHTML = `<div class="attr-map-note">${escapeHTMLText(note)}</div>` + attrs.map(a => {
      const code = String(a.code || a.id).toUpperCase();
      const on = map.has(code);
      const picked = map.get(code) || [];
      const values = String(a.value_options || '').split(',').map(v => v.trim()).filter(Boolean);
      const u = used?.find(x => String(x.code).toUpperCase() === code);
      return `<div class="attr-map-row" data-code="${escapeHTMLText(code)}">
        <label><input type="checkbox" data-map-attr ${on ? 'checked' : ''}> ${escapeHTMLText(a.label || a.code)}</label>
        ${u ? `<div class="attr-map-used">Used on ${u.items} of your items: ${u.values.slice(0, 8).map(v => `${escapeHTMLText(v.value)} (${v.items})`).join(', ')}</div>` : ''}
        ${on && a.value_type === 'Select' && values.length ? `<div class="attr-map-values">${values.map(v =>
          `<button type="button" class="attr-map-chip${picked.some(p => p.toLowerCase() === v.toLowerCase()) ? ' on' : ''}" data-map-value="${escapeHTMLText(v)}">${escapeHTMLText(v)}</button>`).join('')}</div>` : ''}
      </div>`;
    }).join('') + (record && record.id ? `<button type="button" class="btn btn-outline btn-sm" data-map-suggest>Suggest from my items</button>` : '');
  };
  let used = null;
  draw(used);
  host.addEventListener('change', e => {
    if (!e.target.matches('[data-map-attr]')) return;
    const code = e.target.closest('.attr-map-row').dataset.code;
    if (e.target.checked) map.set(code, map.get(code) || []); else map.delete(code);
    write();
    draw(used);
  });
  host.addEventListener('click', async e => {
    const chip = e.target.closest('[data-map-value]');
    if (chip) {
      const code = chip.closest('.attr-map-row').dataset.code;
      const vals = map.get(code) || [];
      const v = chip.dataset.mapValue;
      const i = vals.findIndex(x => x.toLowerCase() === v.toLowerCase());
      if (i >= 0) vals.splice(i, 1); else vals.push(v);
      map.set(code, vals);
      write();
      draw(used);
      return;
    }
    if (e.target.closest('[data-map-suggest]')) {
      const res = await apiFetch(`/api/v1/inventory/attribute-map/suggest?doctype=${encodeURIComponent(doctype)}&ref=${encodeURIComponent(record.id)}`);
      if (!res || !res.ok) { if (res) await showApiError(res, 'Could not read your items.'); return; }
      used = ((await res.json()) || {}).suggestions || [];
      if (!used.length) { showToast(`No items of this ${what} carry attributes yet.`); return; }
      // Tick what the items use, with the values they use.
      used.forEach(u => map.set(String(u.code).toUpperCase(), u.values.map(v => v.value)));
      write();
      draw(used);
      showToast(`Ticked what your items of this ${what} use - check and save.`, { variant: 'success' });
    }
  });
}

// --- Configuration -> Item fields (89.4) ---------------------------------------------

async function renderConfigItemFields(outer) {
  // A fresh container per render, so a re-render never stacks listeners.
  const host = document.createElement('div');
  outer.replaceChildren(host);
  host.innerHTML = '<p class="text-muted">Loading…</p>';
  const setup = await loadItemSetup();
  if (!setup) { host.innerHTML = '<p class="text-muted">Could not load the item fields.</p>'; return; }
  const attrs = setup.fields.filter(r => r.kind === 'attribute');
  const fields = setup.fields.filter(r => r.kind === 'field' && !r.fixed);
  const rows = new Map(setup.fields.map(r => [r.key, { ...r }]));
  const grnSelect = r => `<select class="form-select" data-k="${escapeHTMLText(r.key)}" data-f="grn" aria-label="${escapeHTMLText(r.label)} at the GRN">
      ${[['', 'No'], ['optional', 'Optional'], ['required', 'Required']].map(([v, t]) => `<option value="${v}"${r.grn === v ? ' selected' : ''}>${t}</option>`).join('')}</select>`;
  const check = (r, f, on, disabled) => `<input type="checkbox" data-k="${escapeHTMLText(r.key)}" data-f="${f}"${on ? ' checked' : ''}${disabled ? ' disabled' : ''} aria-label="${escapeHTMLText(r.label)} ${f}">`;
  const listCell = r => {
    if (r.kind === 'attribute') return r.linked_field ? `Through the field <em>${escapeHTMLText((rows.get(r.linked_field) || {}).label || r.linked_field)}</em>` : 'Its own list';
    if (r.type !== 'Data' && r.type !== 'Select') return '<span class="text-muted">—</span>';
    return `<select class="form-select" data-k="${escapeHTMLText(r.key)}" data-f="list" aria-label="${escapeHTMLText(r.label)} values from">
        <option value="-"${!r.list ? ' selected' : ''}>Typed</option>
        ${attrs.map(a => { const code = a.key.slice(5); return `<option value="${escapeHTMLText(code)}"${r.list === code ? ' selected' : ''}>${escapeHTMLText(a.label)} list</option>`; }).join('')}
      </select>${!r.list ? ` <button type="button" class="btn btn-ghost btn-sm" data-make-list="${escapeHTMLText(r.key)}">Make a list</button>` : ''}`;
  };
  const rowHTML = r => `<tr class="${r.hidden ? 'is-hidden' : ''}">
      <td>${escapeHTMLText(r.label)}</td>
      <td class="c">${check(r, 'item', r.item, r.hidden)}</td>
      <td class="c">${r.kind === 'attribute' ? check(r, 'po', r.po, r.hidden) : '<span class="text-muted">—</span>'}</td>
      <td>${grnSelect(r)}</td>
      <td>${listCell(r)}</td>
      <td class="c">${check(r, 'hidden', r.hidden, false)}</td>
    </tr>`;
  host.innerHTML = `
    <h2 style="font-size:18px; margin:0 0 14px;">Item fields</h2>
    <div>
    <table class="item-fields-table">
      <thead><tr><th>Field</th><th class="c">On the item</th><th class="c">In the PO</th><th>At the GRN</th><th>Values from</th><th class="c">Hidden</th></tr></thead>
      <tbody>
        <tr class="item-fields-group"><td colspan="6">Attributes</td></tr>
        ${attrs.length ? attrs.map(rowHTML).join('') : '<tr><td colspan="6" class="text-muted">No item attributes yet - Setup → Item Attribute.</td></tr>'}
        <tr class="item-fields-group"><td colspan="6">Item fields</td></tr>
        ${fields.map(rowHTML).join('')}
      </tbody>
    </table></div>
    <p class="attr-map-note" style="margin-top:10px;">Name, code, kind of item, item name, category, type, HSN, GST, tax treatment, prices, barcode and design are always asked on the item. A hidden field keeps what items already hold.</p>
    <div class="item-fields-actions">
      <button type="button" class="btn btn-primary" data-save-fields>Save item fields</button>
      ${setup.attribute_mapping ? `<span>Attributes follow the item's type: map them on each <a href="#" data-open="ProductType">Product Type</a> or <a href="#" data-open="Category">Category</a>.</span>` : `<span class="text-muted">To offer only some attributes per type, turn on "Attributes follow the item's type" under Inventory, then map them on each Product Type.</span>`}
    </div>`;

  host.addEventListener('change', e => {
    const k = e.target.dataset.k, f = e.target.dataset.f;
    if (!k || !f) return;
    const r = rows.get(k);
    if (f === 'grn' || f === 'list') r[f] = e.target.value === '-' ? '' : e.target.value;
    else r[f] = e.target.checked;
    if (f === 'list' && e.target.value === '-') r.list = '-';
    if (f === 'hidden') {
      const tr = e.target.closest('tr');
      tr.classList.toggle('is-hidden', r.hidden);
      tr.querySelectorAll('input[data-f="item"], input[data-f="po"]').forEach(c => { c.disabled = r.hidden; });
    }
  });
  host.addEventListener('click', async e => {
    const open = e.target.closest('[data-open]');
    if (open) { e.preventDefault(); openSetupDoctype(open.dataset.open); return; }
    const make = e.target.closest('[data-make-list]');
    if (make) {
      const r = rows.get(make.dataset.makeList);
      if (!await showCustomConfirm(`Make ${r.label} a list? A new Item Attribute "${r.label.split('/')[0].trim()}" is created holding every ${r.label} your items already have, and the field becomes a dropdown of it. Nothing on any item changes.`, 'Make a list')) return;
      const res = await apiFetch('/api/v1/inventory/item-setup/fields/list', { method: 'POST', body: JSON.stringify({ field: r.key }) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Could not make the list.'); return; }
      showToast(`${r.label} is now a list.`, { variant: 'success' });
      renderConfigItemFields(outer);
      return;
    }
    if (e.target.closest('[data-save-fields]')) {
      const btn = e.target.closest('[data-save-fields]');
      btn.disabled = true;
      const res = await apiFetch('/api/v1/inventory/item-setup/fields', {
        method: 'PUT',
        body: JSON.stringify({ fields: [...rows.values()].filter(r => !r.fixed).map(r => ({ ...r, list: r.list || '' })) })
      });
      btn.disabled = false;
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Could not save the item fields.'); return; }
      showToast('Item fields saved.', { variant: 'success' });
      renderConfigItemFields(outer);
    }
  });
}

export { wireItemForm, wireItemQuickCreate, wireAttributeMapForm, wireItemTypeForm, renderConfigItemFields, loadItemSetup };
