// Stage 63.4: Print Template Studio - design how a PO, Vendor PO, GRN,
// Transfer Out / In or debit / credit note prints.
//
// Same way of working as the Sticker Studio: drag a field from the list on the
// left onto the page (or click Insert), move and resize it, set it up on the
// right, Save. Line fields dropped onto the items table become columns. The
// canvas is painted by trade-docs.js's own renderer, so the page shown here is
// the page that prints. A template can be limited to a vendor and/or product
// designs / categories; the best match is offered first when printing.

const Q = new URL(import.meta.url).search;
const T = await import('./trade-docs.js' + Q);
await T.tradeStylesReady;

const esc = (s) => window.escapeHTMLText(String(s ?? ''));
const api = (...a) => window.apiFetch(...a);
const clone = (o) => JSON.parse(JSON.stringify(o));
const r1 = (v) => Math.round(v * 10) / 10;

const INSERTS = [
  { type: 'text', label: 'Text', make: () => ({ text: 'Text', w: 50, h: 6 }) },
  { type: 'field', label: 'Field', make: () => ({ key: 'number', w: 50, h: 6 }) },
  { type: 'block', label: 'Store block', make: () => ({ block: 'store', title: 'From', w: 90, h: 28, border: true }) },
  { type: 'block', label: 'Vendor / party', make: () => ({ block: 'party', title: '{party_title}', w: 90, h: 28, border: true }) },
  { type: 'block', label: 'Ship-to block', make: () => ({ block: 'ship_to', title: 'Ship to', w: 90, h: 24, border: true }) },
  { type: 'table', label: 'Items table', make: (S) => ({ w: 190, h: 120, fontSize: 8, headerBg: '#f1f3f5', imageSize: 14, pages: 'all', columns: clone((T.defaultLayout(S.txn).elements.find(e => e.type === 'table') || {}).columns || []) }) },
  { type: 'taxSummary', label: 'Tax summary', make: () => ({ w: 110, h: 30, fontSize: 8, pages: 'last' }) },
  { type: 'totals', label: 'Totals', make: () => ({ w: 70, h: 32, pages: 'last' }) },
  { type: 'image', label: 'Logo / image', make: () => ({ w: 30, h: 15 }) },
  { type: 'line', label: 'Line', make: () => ({ w: 100, h: 0.4, thickness: 0.4 }) },
  { type: 'box', label: 'Box', make: () => ({ w: 60, h: 30, border: true }) },
  { type: 'pageNo', label: 'Page number', make: () => ({ w: 40, h: 5, fontSize: 7, align: 'center', pages: 'all' }) },
  { type: 'qr', label: 'QR code', make: () => ({ key: 'fields.irn_signed_qr', w: 30, h: 30 }) }
];
const TYPE_LABEL = { text: 'Text', field: 'Field', block: 'Block', table: 'Items table', taxSummary: 'Tax summary', totals: 'Totals', image: 'Image', line: 'Line', box: 'Box', pageNo: 'Page number', qr: 'QR code' };
const PAGES_LABEL = { first: 'first page', last: 'last page', all: 'every page' };
const FORMATS = [['text', 'Text'], ['money', 'Money (1,23,456.00)'], ['qty', 'Quantity'], ['percent', 'Percent'], ['date', 'Date (Date order setting)'], ['image', 'Picture']];

let S = null; // the open studio

// ---------------------------------------------------------------------------
// Opening, loading, saving
// ---------------------------------------------------------------------------

async function loadCatalog(txn) {
  const res = await api(`/api/v1/print/catalog?txn=${encodeURIComponent(txn)}`);
  if (!res || !res.ok) return { header: [], line: [], transactions: [] };
  return res.json();
}

function sampleDocument(S) {
  const fields = {};
  (S.catalog.header || []).forEach(f => { if (f.key.startsWith('fields.')) fields[f.key.slice(7)] = `[${f.label}]`; });
  const blk = (n) => ({ name: `${n} name`, legal_name: `${n} legal name`, address: '12, Sample Street, Market Road', city: 'Mumbai', pincode: '400001', state: 'Maharashtra', state_code: '27', gstin: '27ABCDE1234F1Z5', phone: '98200 00000', code: n.toUpperCase() });
  const lines = Array.from({ length: 6 }, (_, i) => {
    const qty = 10 + i * 2, rate = 450 + i * 35, taxable = qty * rate, tax = Math.round(taxable * 0.05 * 100) / 100;
    const l = { sr: i + 1, sku: `DSN-00${i + 1}-RED-M`, item_name: `Sample item ${i + 1}`, hsn_code: '6204', barcode: '0200000000' + (100 + i), uom: 'PCS', qty, rate, mrp: rate * 2, taxable, gst_rate: 5, cgst: tax / 2, sgst: tax / 2, igst: 0, tax, amount: taxable + tax, po_id: 'PO/HQ/000123', batch_no: `B261015-000${i + 1}`, accepted_qty: qty, rejected_qty: 0, damaged_qty: 0, po_rate: rate, invoice_rate: rate, rate_difference: 0, sent_qty: qty, item: {} };
    (S.catalog.line || []).forEach(f => { if (f.key.startsWith('item.')) l.item[f.key.slice(5)] = `[${f.label.replace(/^Item - /, '')}]`; });
    return l;
  });
  const t = lines.reduce((a, l) => ({ qty: a.qty + l.qty, taxable: a.taxable + l.taxable, tax: a.tax + l.tax, amount: a.amount + l.amount }), { qty: 0, taxable: 0, tax: 0, amount: 0 });
  return {
    transaction: S.txn, title: T.txnLabel(S.txn), number: 'DOC/HQ/000123', date: new Date().toISOString().slice(0, 10), status: 'Approved',
    brand: { name: 'Your Brand' }, store: blk('Store'), party: blk('Vendor'), ship_to: blk('Warehouse'), party_title: 'Vendor', interstate: false,
    fields, lines,
    tax_summary: [{ gst_rate: 5, taxable: t.taxable, cgst: t.tax / 2, sgst: t.tax / 2, igst: 0, tax: t.tax }],
    totals: { qty: t.qty, accepted_qty: t.qty, taxable: t.taxable, cgst: t.tax / 2, sgst: t.tax / 2, igst: 0, tax: t.tax, amount: t.amount, round_off: 0, grand_total: Math.round(t.amount), line_count: lines.length },
    amount_in_words: 'Rupees Sample Amount Only'
  };
}

export async function openPrintTemplateStudio({ txn = 'PurchaseOrder', templateId = '', sample = null, copyFrom = null, startLayout = null } = {}) {
  let doc = null;
  const sourceId = templateId || copyFrom;
  if (sourceId) {
    const res = await api(`/api/v1/doc/PrintTemplate/${encodeURIComponent(sourceId)}`);
    if (!res) return;
    if (!res.ok) { await window.showApiError(res, 'Could not open that template.'); return; }
    doc = await res.json();
    txn = doc.transaction || txn;
  }
  S = {
    id: templateId && doc ? doc.id : '', isNew: !(templateId && doc), txn,
    name: doc ? (copyFrom ? `${doc.name} (copy)` : doc.name) : startLayout ? `${T.txnLabel(txn)} - 3-inch roll` : `${T.txnLabel(txn)} - custom`,
    code: doc && !copyFrom ? doc.code : '',
    isDefault: doc && !copyFrom ? (doc.is_default || 'No') : 'No',
    vendor: doc ? (doc.vendor || '') : '', families: doc ? (doc.item_families || '') : '', categories: doc ? (doc.item_categories || '') : '',
    status: doc ? (doc.status || 'Active') : 'Active',
    layout: startLayout ? clone(startLayout) : T.parseLayout(doc ? doc.layout : '', txn), sel: null, zoom: 0, catalog: null, sample: null, sampleRef: sample, undo: [], redo: [], dirty: false,
    root: null
  };
  S.catalog = await loadCatalog(txn);
  S.sample = sampleDocument(S);
  buildShell();
  if (sample && sample.id) await loadSample(sample.id);
  render();
}

async function loadSample(id) {
  const res = await api(`/api/v1/print/document/${encodeURIComponent(S.txn)}/${encodeURIComponent(id)}${T.layoutUsesImages(S.layout) ? '?images=1' : ''}`);
  if (!res) return;
  if (!res.ok) { await window.showApiError(res, 'That document could not be loaded for preview.'); return; }
  S.sample = (await res.json()).document;
  S.sampleRef = { txn: S.txn, id };
  render();
}

function payload() {
  return {
    code: S.code, name: S.name.trim() || T.txnLabel(S.txn), transaction: S.txn, is_default: S.isDefault,
    vendor: S.vendor || '', item_families: S.families || '', item_categories: S.categories || '',
    layout: JSON.stringify(S.layout), status: S.status
  };
}

async function updateDocFields(id, patch) {
  const res = await api(`/api/v1/doc/PrintTemplate/${encodeURIComponent(id)}`);
  if (!res || !res.ok) return false;
  const d = await res.json();
  const body = { code: d.code, name: d.name, transaction: d.transaction, is_default: d.is_default, vendor: d.vendor || '', item_families: d.item_families || '', item_categories: d.item_categories || '', layout: d.layout, status: d.status, ...patch };
  const up = await api(`/api/v1/doc/PrintTemplate/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify(body) });
  return !!(up && up.ok);
}

// clearOtherDefaults keeps one default per transaction.
async function clearOtherDefaults(txn, keepId) {
  const res = await api('/api/v1/doc/PrintTemplate');
  if (!res || !res.ok) return;
  const all = await res.json();
  for (const t of all) {
    if (t.id !== keepId && t.transaction === txn && t.is_default === 'Yes') await updateDocFields(t.id, { is_default: 'No' });
  }
}

async function save() {
  if (!S.code) {
    const res = await api('/api/v1/doc/PrintTemplate');
    const existing = new Set(res && res.ok ? (await res.json()).map(x => String(x.code || x.id).toUpperCase()) : []);
    const base = `PT-${S.txn.toUpperCase()}`;
    let code = base, n = 2;
    while (existing.has(code)) code = `${base}-${n++}`;
    S.code = code;
  }
  const url = S.isNew ? '/api/v1/doc/PrintTemplate' : `/api/v1/doc/PrintTemplate/${encodeURIComponent(S.id)}`;
  const btn = S.root.querySelector('[data-ps="save"]');
  btn.disabled = true;
  const res = await api(url, { method: 'POST', body: JSON.stringify(payload()) });
  btn.disabled = false;
  if (!res) return;
  if (!res.ok) { await window.showApiError(res, 'Could not save the template.'); return; }
  const out = await res.json();
  if (S.isNew) { S.id = out.id; S.isNew = false; }
  if (S.isDefault === 'Yes') await clearOtherDefaults(S.txn, S.id);
  S.dirty = false;
  // Stage 130.16 (74.3.4): every save is a version; with approval on it waits.
  const versions = await loadVersions();
  const latest = versions[0];
  window.showToast?.(latest && latest.status === 'Pending'
    ? `Saved as version ${latest.version} - it prints once someone else approves it (Versions).`
    : `Template "${S.name}" saved${latest ? ` as version ${latest.version}` : ''}.`, { variant: 'success' });
  renderTop();
}

async function loadVersions() {
  if (!S || S.isNew || !S.id) return [];
  const res = await api(`/api/v1/print/templates/${encodeURIComponent(S.id)}/versions`);
  return res && res.ok ? res.json() : [];
}

// showVersions (Stage 130.16, 74.3.4): the template's versions - approve or
// reject a change waiting for approval, restore an old one.
async function showVersions() {
  const versions = await loadVersions();
  S.root.querySelector('[data-ps="versions-panel"]')?.remove();
  const panel = document.createElement('div');
  panel.dataset.ps = 'versions-panel';
  panel.className = 'table-panel';
  panel.style.cssText = 'position:absolute;right:16px;top:64px;z-index:20;max-width:560px;padding:14px 16px;box-shadow:var(--shadow-lg, 0 8px 24px rgba(0,0,0,.18));';
  const when = (v) => { const d = new Date(v); return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(); };
  panel.innerHTML = `<div style="display:flex;align-items:center;gap:8px;margin-bottom:8px;"><strong>Versions</strong><span class="pts-spacer" style="flex:1"></span><button type="button" class="btn btn-ghost btn-sm" data-ver-close>Close</button></div>
    <div class="table-wrapper"><table><thead><tr><th>Version</th><th>Status</th><th>Saved</th><th>Approved by</th><th></th></tr></thead><tbody>
    ${versions.length ? versions.map(v => `<tr><td>${v.version}</td><td>${esc(v.status)}${v.note ? `<div class="text-muted" style="font-size:12px;">${esc(v.note)}</div>` : ''}</td>
      <td>${esc(v.saved_by)}<div class="text-muted" style="font-size:12px;">${esc(when(v.saved_at))}</div></td><td>${esc(v.approved_by || '')}</td>
      <td style="white-space:nowrap;">${v.status === 'Pending' ? `<button type="button" class="action-btn" data-ver="approve" data-v="${v.version}">Approve</button> <button type="button" class="action-btn action-btn-danger" data-ver="reject" data-v="${v.version}">Reject</button>` : ''}
        ${v.status === 'Superseded' || v.status === 'Rejected' ? `<button type="button" class="action-btn" data-ver="restore" data-v="${v.version}">Restore</button>` : ''}</td></tr>`).join('')
      : '<tr><td colspan="5" class="text-muted" style="text-align:center;">No versions yet - the next save is version 1.</td></tr>'}</tbody></table></div>`;
  S.root.appendChild(panel);
  panel.querySelector('[data-ver-close]').addEventListener('click', () => panel.remove());
  panel.querySelectorAll('[data-ver]').forEach(b => b.addEventListener('click', async () => {
    const action = b.dataset.ver;
    const note = action === 'reject' ? String((await window.showCustomPrompt('Why is this change rejected?', '', 'Reject')) || '').trim() : '';
    if (action === 'reject' && !note) return;
    const res = await api(`/api/v1/print/templates/${encodeURIComponent(S.id)}/versions/${b.dataset.v}`, { method: 'POST', body: JSON.stringify({ action, note }) });
    if (!res) return;
    if (!res.ok) { await window.showApiError(res, 'That did not go through.'); return; }
    window.showToast?.({ approve: 'Approved - it prints from now on. Reopen the template to edit it.', reject: 'Change rejected.', restore: 'Restored as a new version.' }[action], { variant: 'success' });
    showVersions();
  }));
}

async function closeStudio() {
  if (S.dirty && !(await window.showCustomConfirm('Close without saving your changes?', 'Unsaved changes'))) return;
  document.removeEventListener('keydown', onKey, true);
  S.root.remove();
  S = null;
  // The gallery sits in Sticker Printing and in Setup → Print Templates;
  // redraw whichever one is open rather than jumping to Sticker Printing.
  if (document.querySelector('[data-pts-gallery]')) window.renderView?.(currentView);
}

// ---------------------------------------------------------------------------
// Edits (with undo)
// ---------------------------------------------------------------------------

function snapshot() {
  S.undo.push(JSON.stringify(S.layout));
  if (S.undo.length > 60) S.undo.shift();
  S.redo = [];
  S.dirty = true;
}
function undo() { if (!S.undo.length) return; S.redo.push(JSON.stringify(S.layout)); S.layout = JSON.parse(S.undo.pop()); S.dirty = true; render(); }
function redo() { if (!S.redo.length) return; S.undo.push(JSON.stringify(S.layout)); S.layout = JSON.parse(S.redo.pop()); S.dirty = true; render(); }
const selected = () => S.layout.elements.find(e => e.id === S.sel) || null;

function addElement(type, props, at) {
  const ins = INSERTS.find(i => i.type === type && (!props.block || i.make(S).block === props.block)) || INSERTS.find(i => i.type === type);
  if (type === 'table' && S.layout.elements.some(e => e.type === 'table')) {
    window.showToast?.('A template has one items table - drop line fields onto it to add columns.', { variant: 'warning' });
    return;
  }
  snapshot();
  const [pw, ph] = T.pageDims(S.layout.page);
  const base = { id: T.newElementId(), type, fontSize: 9, align: 'left', pages: 'first', ...(ins ? ins.make(S) : {}), ...props };
  base.x = r1(Math.max(0, Math.min(pw - base.w, at ? at.x : (pw - base.w) / 2)));
  base.y = r1(Math.max(0, Math.min(ph - base.h, at ? at.y : 40)));
  S.layout.elements.push(base);
  S.sel = base.id;
  render();
}

function addColumn(table, field) {
  if ((table.columns || []).some(c => c.key === field.key)) { window.showToast?.(`"${field.label}" is already a column.`, { variant: 'warning' }); return; }
  snapshot();
  const fmt = field.key === 'image' ? 'image' : T.guessFormat(field.key);
  table.columns = [...(table.columns || []), { key: field.key, label: field.label.replace(/^Item - /, ''), width: field.key === 'image' ? 14 : 12, format: fmt, align: ['money', 'qty', 'percent'].includes(fmt) ? 'right' : 'left' }];
  S.sel = table.id;
  render();
}

// ---------------------------------------------------------------------------
// Shell
// ---------------------------------------------------------------------------

function buildShell() {
  const root = document.createElement('div');
  root.className = 'pts-root';
  root.innerHTML = `
    <div class="pts-top" data-ps="top"></div>
    <div class="pts-body">
      <aside class="pts-left">
        <div class="pts-section-title">Insert</div>
        <div class="pts-insert">${INSERTS.map((i, n) => `<button type="button" draggable="true" data-ins="${n}">${esc(i.label)}</button>`).join('')}</div>
        <div class="pts-section-title">Fields &mdash; drag onto the page</div>
        <input type="search" class="form-input pts-search" data-ps="search" placeholder="Search fields...">
        <div class="pts-fields" data-ps="fields"></div>
      </aside>
      <main class="pts-stage" data-ps="stage">
        <div class="pts-stage-inner"><div class="pts-canvas" data-ps="canvas"></div></div>
        <div class="pts-bottom">
          <button type="button" class="btn btn-ghost btn-sm" data-ps="zoom-out" aria-label="Zoom out">&minus;</button>
          <span data-ps="zoom-label">100%</span>
          <button type="button" class="btn btn-ghost btn-sm" data-ps="zoom-in" aria-label="Zoom in">+</button>
          <button type="button" class="btn btn-ghost btn-sm" data-ps="zoom-fit">Fit</button>
        </div>
      </main>
      <aside class="pts-right" data-ps="inspector"></aside>
    </div>`;
  document.body.appendChild(root);
  S.root = root;
  // A non-production server pins an environment banner to the top of the
  // page; start below it (as the Sticker Studio does) so it never covers this
  // top bar's Save / Close.
  const banner = document.getElementById('environment-banner');
  if (banner && banner.offsetHeight) root.style.top = banner.offsetHeight + 'px';
  const q = (k) => root.querySelector(`[data-ps="${k}"]`);

  root.querySelectorAll('[data-ins]').forEach(btn => {
    const ins = INSERTS[Number(btn.getAttribute('data-ins'))];
    btn.addEventListener('click', () => addElement(ins.type, { ...ins.make(S) }));
    btn.addEventListener('dragstart', e => e.dataTransfer.setData('text/pts-insert', btn.getAttribute('data-ins')));
  });
  q('search').addEventListener('input', renderFields);
  q('zoom-in').addEventListener('click', () => { S.zoom = Math.min(3, (S.zoom || 1) + 0.15); renderCanvas(); });
  q('zoom-out').addEventListener('click', () => { S.zoom = Math.max(0.3, (S.zoom || 1) - 0.15); renderCanvas(); });
  q('zoom-fit').addEventListener('click', () => { S.zoom = 0; renderCanvas(); });

  const canvas = q('canvas');
  canvas.addEventListener('dragover', e => {
    e.preventDefault();
    const hit = e.target.closest?.('.pts-hit');
    root.querySelectorAll('.pts-hit.drop').forEach(h => h.classList.remove('drop'));
    if (hit && hit.dataset.type === 'table') hit.classList.add('drop');
  });
  canvas.addEventListener('dragleave', () => root.querySelectorAll('.pts-hit.drop').forEach(h => h.classList.remove('drop')));
  canvas.addEventListener('drop', e => {
    e.preventDefault();
    root.querySelectorAll('.pts-hit.drop').forEach(h => h.classList.remove('drop'));
    const at = pointToMM(e);
    const ins = e.dataTransfer.getData('text/pts-insert');
    if (ins !== '') { const i = INSERTS[Number(ins)]; addElement(i.type, { ...i.make(S) }, at); return; }
    const raw = e.dataTransfer.getData('text/pts-field');
    if (!raw) return;
    const f = JSON.parse(raw);
    const table = S.layout.elements.find(el => el.type === 'table');
    if (f.line) {
      if (!table) { window.showToast?.('Insert an Items table first, then drop line fields onto it.', { variant: 'warning' }); return; }
      addColumn(table, f);
      return;
    }
    addElement('field', { key: f.key, label: '', w: 60, h: 6 }, at);
  });
  canvas.addEventListener('pointerdown', e => { if (e.target === canvas || e.target.closest('.tdp-page')) { S.sel = null; renderOverlay(); renderInspector(); } });
  document.addEventListener('keydown', onKey, true);
  window.addEventListener('resize', () => { if (S && !S.zoom) renderCanvas(); });
}

function onKey(e) {
  if (!S) return;
  const typing = e.target.closest && e.target.closest('input, textarea, select, [contenteditable]');
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); save(); return; }
  if (typing) return;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo(); return; }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'y') { e.preventDefault(); redo(); return; }
  const el = selected();
  if (!el) { if (e.key === 'Escape') closeStudio(); return; }
  if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); snapshot(); S.layout.elements = S.layout.elements.filter(x => x.id !== el.id); S.sel = null; render(); return; }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'd') { e.preventDefault(); duplicate(el); return; }
  if (e.key === 'Escape') { S.sel = null; render(); return; }
  const step = e.shiftKey ? 5 : 0.5;
  const mv = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
  if (mv) { e.preventDefault(); snapshot(); el.x = r1(el.x + mv[0]); el.y = r1(el.y + mv[1]); render(); }
}

function duplicate(el) {
  if (el.type === 'table') return;
  snapshot();
  const c = clone(el);
  c.id = T.newElementId();
  c.x = r1(c.x + 3); c.y = r1(c.y + 3);
  S.layout.elements.push(c);
  S.sel = c.id;
  render();
}

function render() { renderTop(); renderFields(); renderCanvas(); renderInspector(); }

function renderTop() {
  const top = S.root.querySelector('[data-ps="top"]');
  top.innerHTML = `
    <input class="pts-name" data-ps="name" value="${esc(S.name)}" aria-label="Template name">
    <span class="pts-chip">${esc(T.txnLabel(S.txn))}</span>
    ${S.isDefault === 'Yes' ? '<span class="pts-chip">Default</span>' : ''}
    ${S.dirty ? '<span class="text-muted" style="font-size:12px;">Unsaved</span>' : ''}
    <span class="pts-spacer"></span>
    <span class="text-muted" style="font-size:12px;">Preview with</span>
    <input class="form-input" style="width:170px;" data-ps="sample" placeholder="document no" value="${esc(S.sampleRef?.id || '')}">
    <button type="button" class="btn btn-ghost btn-sm" data-ps="undo" ${S.undo.length ? '' : 'disabled'}>Undo</button>
    <button type="button" class="btn btn-ghost btn-sm" data-ps="redo" ${S.redo.length ? '' : 'disabled'}>Redo</button>
    <button type="button" class="btn btn-outline btn-sm" data-ps="test">Test print</button>
    ${S.isNew ? '' : '<button type="button" class="btn btn-outline btn-sm" data-ps="versions">Versions</button>'}
    <button type="button" class="btn btn-primary btn-sm" data-ps="save">Save</button>
    <button type="button" class="btn btn-ghost btn-sm" data-ps="close">Close</button>`;
  const q = (k) => top.querySelector(`[data-ps="${k}"]`);
  q('name').addEventListener('input', () => { S.name = q('name').value; S.dirty = true; });
  q('undo').addEventListener('click', undo);
  q('redo').addEventListener('click', redo);
  q('save').addEventListener('click', save);
  q('versions')?.addEventListener('click', showVersions);
  q('close').addEventListener('click', closeStudio);
  // Stage 127.2: a test print goes where a real one would, with no browser dialog once set up.
  q('test').addEventListener('click', () => T.printLayout(S.txn, S.layout, S.sample));
  const sampleEl = q('sample');
  const doctype = (T.TRANSACTIONS.find(t => t.key === S.txn) || {}).doctype;
  if (window.attachLinkTypeahead && doctype) window.attachLinkTypeahead(sampleEl, doctype);
  const go = () => { const v = sampleEl.value.trim(); if (v && v !== (S.sampleRef?.id || '')) loadSample(v); };
  sampleEl.addEventListener('change', go);
  sampleEl.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); go(); } });
}

function renderFields() {
  const box = S.root.querySelector('[data-ps="fields"]');
  const term = (S.root.querySelector('[data-ps="search"]').value || '').toLowerCase();
  const all = [
    ...(S.catalog.header || []).map(f => ({ ...f, line: false })),
    ...(S.catalog.line || []).map(f => ({ ...f, group: f.group === 'Line' ? 'Items table columns' : 'Items table - item master', line: true }))
  ].filter(f => !term || f.label.toLowerCase().includes(term) || f.key.toLowerCase().includes(term));
  let html = '', group = '';
  all.forEach(f => {
    if (f.group !== group) { group = f.group; html += `<div class="pts-field-group">${esc(group)}</div>`; }
    html += `<div class="pts-field" draggable="true" data-field='${esc(JSON.stringify(f))}' title="${esc(f.key)}">${esc(f.label)}</div>`;
  });
  box.innerHTML = html || '<div class="pts-hint">No field matches.</div>';
  box.querySelectorAll('[data-field]').forEach(div => {
    div.addEventListener('dragstart', e => e.dataTransfer.setData('text/pts-field', div.getAttribute('data-field')));
    div.addEventListener('dblclick', () => {
      const f = JSON.parse(div.getAttribute('data-field'));
      const table = S.layout.elements.find(el => el.type === 'table');
      if (f.line) { if (table) addColumn(table, f); } else addElement('field', { key: f.key, label: '', w: 60, h: 6 });
    });
  });
}

// ---------------------------------------------------------------------------
// Canvas: the real renderer underneath, a hit layer on top
// ---------------------------------------------------------------------------

function scale() {
  const [pw] = T.pageDims(S.layout.page);
  if (S.zoom) return S.zoom;
  const stage = S.root.querySelector('[data-ps="stage"]');
  return Math.max(0.3, Math.min(1.6, (stage.clientWidth - 70) / (pw * T.PX_PER_MM)));
}

function pointToMM(e) {
  const canvas = S.root.querySelector('[data-ps="canvas"]');
  const r = canvas.getBoundingClientRect();
  const k = scale() * T.PX_PER_MM;
  return { x: r1((e.clientX - r.left) / k), y: r1((e.clientY - r.top) / k) };
}

function renderCanvas() {
  const canvas = S.root.querySelector('[data-ps="canvas"]');
  const [pw, ph] = T.pageDims(S.layout.page);
  const k = scale();
  canvas.style.width = `${pw * T.PX_PER_MM * k}px`;
  canvas.style.height = `${ph * T.PX_PER_MM * k}px`;
  // Every element shown on the design page, whichever pages it prints on.
  const page = T.renderPages(S.layout, S.sample, { design: true })[0];
  page.style.transform = `scale(${k})`;
  canvas.innerHTML = '';
  canvas.appendChild(page);
  const m = S.layout.page?.margin ?? 10;
  const margin = document.createElement('div');
  margin.className = 'pts-margin';
  Object.assign(margin.style, { left: `${m * T.PX_PER_MM * k}px`, top: `${m * T.PX_PER_MM * k}px`, right: `${m * T.PX_PER_MM * k}px`, bottom: `${m * T.PX_PER_MM * k}px` });
  canvas.appendChild(margin);
  const overlay = document.createElement('div');
  overlay.className = 'pts-overlay';
  overlay.dataset.ps = 'overlay';
  canvas.appendChild(overlay);
  S.root.querySelector('[data-ps="zoom-label"]').textContent = `${Math.round(k * 100)}%`;
  renderOverlay();
}

function renderOverlay() {
  const overlay = S.root.querySelector('[data-ps="overlay"]');
  if (!overlay) return;
  const k = scale() * T.PX_PER_MM;
  const table = S.layout.elements.find(el => el.type === 'table');
  overlay.innerHTML = S.layout.elements.map(el => `
    <div class="pts-hit${el.id === S.sel ? ' sel' : ''}" data-id="${esc(el.id)}" data-type="${esc(el.type)}" style="left:${el.x * k}px;top:${el.y * k}px;width:${Math.max(4, el.w * k)}px;height:${Math.max(4, el.h * k)}px;">
      <span class="pts-tag">${esc(TYPE_LABEL[el.type] || el.type)}${el.key ? ` · ${esc(el.key)}` : ''} · ${PAGES_LABEL[T.printsOn(el, table)]}</span>
      ${el.id === S.sel ? '<span class="pts-handle" data-h="e"></span><span class="pts-handle" data-h="s"></span><span class="pts-handle" data-h="se"></span>' : ''}
    </div>`).join('');
  overlay.querySelectorAll('.pts-hit').forEach(hit => hit.addEventListener('pointerdown', startDrag));
}

// Dragging snaps to a 0.5 mm grid and to the page centre, margins and every
// other element's edges (within 1.5 mm), with a pink guide where it snapped.
function snapTargets(exceptId) {
  const [pw, ph] = T.pageDims(S.layout.page);
  const m = S.layout.page?.margin ?? 10;
  const xs = [m, pw / 2, pw - m], ys = [m, ph / 2, ph - m];
  S.layout.elements.forEach(el => { if (el.id !== exceptId) { xs.push(el.x, el.x + el.w); ys.push(el.y, el.y + el.h); } });
  return { xs, ys };
}

function snapAxis(edges, targets) {
  let best = null;
  edges.forEach((edge, i) => targets.forEach(t => {
    const d = t - edge;
    if (Math.abs(d) <= 1.5 && (!best || Math.abs(d) < Math.abs(best.d))) best = { d, t, i };
  }));
  return best;
}

function startDrag(e) {
  if (e.button !== 0) return;
  e.preventDefault();
  e.stopPropagation();
  const id = e.currentTarget.getAttribute('data-id');
  const handle = e.target.getAttribute('data-h');
  if (S.sel !== id) { S.sel = id; renderOverlay(); renderInspector(); }
  const el = selected();
  if (!el || el.locked) return;
  const k = scale() * T.PX_PER_MM;
  const start = { x: e.clientX, y: e.clientY, ex: el.x, ey: el.y, ew: el.w, eh: el.h };
  let moved = false;
  const targets = snapTargets(id);
  const overlay = S.root.querySelector('[data-ps="overlay"]');
  const move = (ev) => {
    const dx = (ev.clientX - start.x) / k, dy = (ev.clientY - start.y) / k;
    if (!moved && Math.abs(dx) + Math.abs(dy) < 0.3) return;
    if (!moved) { snapshot(); moved = true; }
    overlay.querySelectorAll('.pts-guide').forEach(g => g.remove());
    const grid = (v) => ev.altKey ? r1(v) : Math.round(v * 2) / 2;
    if (!handle) {
      let nx = grid(start.ex + dx), ny = grid(start.ey + dy);
      if (!ev.altKey) {
        const sx = snapAxis([nx, nx + el.w / 2, nx + el.w], targets.xs);
        const sy = snapAxis([ny, ny + el.h / 2, ny + el.h], targets.ys);
        if (sx) { nx = r1(nx + sx.d); guide(overlay, 'x', sx.t, k); }
        if (sy) { ny = r1(ny + sy.d); guide(overlay, 'y', sy.t, k); }
      }
      el.x = nx; el.y = ny;
    } else {
      if (handle.includes('e')) el.w = Math.max(1, grid(start.ew + dx));
      if (handle.includes('s')) el.h = Math.max(el.type === 'line' ? 0.2 : 2, grid(start.eh + dy));
    }
    const hit = overlay.querySelector(`[data-id="${id}"]`);
    Object.assign(hit.style, { left: `${el.x * k}px`, top: `${el.y * k}px`, width: `${el.w * k}px`, height: `${el.h * k}px` });
  };
  const up = () => {
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', up);
    if (moved) { renderCanvas(); renderInspector(); renderTop(); }
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', up);
}

function guide(overlay, axis, mm, k) {
  const g = document.createElement('div');
  g.className = 'pts-guide';
  Object.assign(g.style, axis === 'x' ? { left: `${mm * k}px`, top: 0, bottom: 0, width: '1px' } : { top: `${mm * k}px`, left: 0, right: 0, height: '1px' });
  overlay.appendChild(g);
}

// ---------------------------------------------------------------------------
// Inspector
// ---------------------------------------------------------------------------

const opt = (v, label, cur) => `<option value="${esc(v)}"${String(v) === String(cur) ? ' selected' : ''}>${esc(label)}</option>`;
const num = (k, label, v, step = 0.5) => `<div class="form-group"><label class="form-label">${label}</label><input type="number" class="form-input" data-num="${k}" step="${step}" value="${esc(v ?? '')}"></div>`;

function renderInspector() {
  const box = S.root.querySelector('[data-ps="inspector"]');
  const el = selected();
  box.innerHTML = el ? elementInspector(el) : templateInspector();
  bindInspector(box, el);
}

function templateInspector() {
  const p = S.layout.page || {};
  return `
    <div class="pts-section-title" style="margin-top:0;">Template</div>
    <div class="form-group"><label class="form-label">Transaction</label><div>${esc(T.txnLabel(S.txn))}</div></div>
    <div class="pts-grid2">
      <div class="form-group"><label class="form-label">Page size</label><select class="form-input" data-tp="size">${Object.keys(T.PAGE_SIZES).map(s => opt(s, s, p.size || 'A4')).join('')}</select></div>
      <div class="form-group"><label class="form-label">Orientation</label><select class="form-input" data-tp="orientation">${opt('portrait', 'Portrait', p.orientation || 'portrait')}${opt('landscape', 'Landscape', p.orientation)}</select></div>
      <div class="form-group"><label class="form-label">Margin (mm)</label><input type="number" class="form-input" data-tp="margin" step="1" min="0" value="${esc(p.margin ?? 10)}"></div>
      <div class="form-group"><label class="form-label">Font</label><select class="form-input" data-tp="font">${['Inter, Arial, sans-serif', 'Arial, sans-serif', 'Georgia, serif', 'Times New Roman, serif', 'Courier New, monospace', 'Noto Sans, Arial, sans-serif'].map(f => opt(f, f.split(',')[0], p.font || 'Inter, Arial, sans-serif')).join('')}</select></div>
    </div>
    <div class="pts-section-title">When to use it</div>
    <div class="form-group"><label class="form-label">Default for ${esc(T.txnLabel(S.txn))}</label><select class="form-input" data-tm="isDefault">${opt('No', 'No', S.isDefault)}${opt('Yes', 'Yes', S.isDefault)}</select></div>
    <div class="form-group"><label class="form-label">Only for vendor</label><input class="form-input" data-tm="vendor" value="${esc(S.vendor)}" placeholder="any vendor"></div>
    <div class="form-group"><label class="form-label">Only for designs / families</label><input class="form-input" data-tm="families" value="${esc(S.families)}" placeholder="any - comma-separated"></div>
    <div class="form-group"><label class="form-label">Only for categories</label><input class="form-input" data-tm="categories" value="${esc(S.categories)}" placeholder="any - comma-separated"></div>
    <div class="form-group"><label class="form-label">Status</label><select class="form-input" data-tm="status">${opt('Active', 'Active', S.status)}${opt('Inactive', 'Inactive', S.status)}</select></div>
    <p class="pts-hint">When printing, the most specific template that fits is offered first: vendor, then design / family, then category, then the default. The brand name always prints - if this layout does not place it, it goes at the top of every page.</p>
    <div class="pts-section-title">How to</div>
    <p class="pts-hint">Drag a field from the left onto the page. Drop line fields (or item master fields) onto the items table to add a column. Arrows move 0.5 mm, Shift+arrows 5 mm, Alt while dragging skips snapping, Delete removes, Ctrl+D duplicates, Ctrl+Z undoes, Ctrl+S saves.</p>`;
}

function elementInspector(el) {
  const byPosition = T.pagesByPosition(el, S.layout.elements.find(e => e.type === 'table'));
  const common = `
    <div class="pts-section-title" style="margin-top:0;">${esc(TYPE_LABEL[el.type] || el.type)}</div>
    <div class="pts-grid4">${num('x', 'X', el.x)}${num('y', 'Y', el.y)}${num('w', 'W', el.w)}${num('h', 'H', el.h, el.type === 'line' ? 0.1 : 0.5)}</div>
    ${el.type === 'table' ? '' : byPosition ? `<div class="form-group"><label class="form-label">Prints on</label><select class="form-input" disabled>${opt(byPosition, byPosition === 'first' ? 'First page (above the items table)' : 'Last page (below the items table)', byPosition)}</select></div>` : `<div class="form-group"><label class="form-label">Prints on</label><select class="form-input" data-prop="pages">${opt('first', 'First page', el.pages)}${opt('all', 'Every page', el.pages)}${opt('last', 'Last page', el.pages)}</select></div>`}
    ${['line', 'image', 'box', 'qr'].includes(el.type) ? '' : `
    <div class="pts-grid2">${num('fontSize', 'Font size (pt)', el.fontSize ?? 9)}<div class="form-group"><label class="form-label">Colour</label><input type="color" class="form-input" data-prop="color" value="${esc(el.color || '#111111')}"></div></div>
    <div class="pts-toggles">
      <button type="button" data-tog="bold" class="${el.bold ? 'on' : ''}"><b>B</b></button>
      <button type="button" data-tog="italic" class="${el.italic ? 'on' : ''}"><i>I</i></button>
      <button type="button" data-tog="border" class="${el.border ? 'on' : ''}">Border</button>
      ${['left', 'center', 'right'].map(a => `<button type="button" data-align="${a}" class="${(el.align || 'left') === a ? 'on' : ''}">${a[0].toUpperCase() + a.slice(1)}</button>`).join('')}
    </div>
    <div class="form-group" style="margin-top:8px;"><label class="form-label">Background</label><input type="text" class="form-input" data-prop="bg" value="${esc(el.bg || '')}" placeholder="none, or #f1f3f5"></div>`}`;
  let extra = '';
  const headerFields = S.catalog.header || [];
  switch (el.type) {
    case 'text':
    case 'pageNo':
      extra = `<div class="form-group"><label class="form-label">${el.type === 'pageNo' ? 'Text ({page}, {pages})' : 'Text - {field} inserts a value'}</label><textarea class="form-input" rows="4" data-prop="text">${esc(el.text || (el.type === 'pageNo' ? 'Page {page} of {pages}' : ''))}</textarea></div>
        <div class="form-group"><label class="form-label">Insert field</label><select class="form-input" data-insert-token><option value="">Choose...</option>${headerFields.map(f => opt(f.key, `${f.group}: ${f.label}`, '')).join('')}</select></div>`;
      break;
    case 'field':
      extra = `<div class="form-group"><label class="form-label">Field</label><select class="form-input" data-prop="key">${headerFields.map(f => opt(f.key, `${f.group}: ${f.label}`, el.key)).join('')}</select></div>
        <div class="form-group"><label class="form-label">Label before it</label><input class="form-input" data-prop="label" value="${esc(el.label || '')}" placeholder="e.g. PO No:"></div>
        <div class="form-group"><label class="form-label">Format</label><select class="form-input" data-prop="format">${opt('', 'Automatic', el.format || '')}${FORMATS.filter(f => f[0] !== 'image').map(f => opt(f[0], f[1], el.format || '')).join('')}</select></div>`;
      break;
    case 'block':
      extra = `<div class="form-group"><label class="form-label">Block</label><select class="form-input" data-prop="block">${opt('store', 'Generating store', el.block)}${opt('party', 'Vendor / other store', el.block)}${opt('ship_to', 'Ship to', el.block)}</select></div>
        <div class="form-group"><label class="form-label">Title</label><input class="form-input" data-prop="title" value="${esc(el.title || '')}"></div>
        <div class="form-group"><label class="form-label">Show</label>${T.BLOCK_LINE_OPTIONS.map(([k, l]) => `<label style="display:flex;gap:6px;font-size:12.5px;"><input type="checkbox" data-show="${k}" ${(el.show || ['name', 'legal_name', 'address', 'city_pin', 'state', 'gstin', 'phone']).includes(k) ? 'checked' : ''}>${esc(l)}</label>`).join('')}</div>`;
      break;
    case 'image':
      extra = `<div class="form-group"><label class="form-label">Picture (logo) - PNG / JPG up to 400 KB</label><input type="file" accept="image/png,image/jpeg,image/svg+xml" class="form-input" data-upload></div>
        <div class="form-group"><label class="form-label">Fit</label><select class="form-input" data-prop="fit">${opt('contain', 'Fit inside', el.fit || 'contain')}${opt('cover', 'Fill', el.fit)}</select></div>
        ${el.src ? '<button type="button" class="btn btn-ghost btn-sm" data-clear-src>Remove picture</button>' : ''}`;
      break;
    case 'line':
      extra = `<div class="pts-grid2">${num('thickness', 'Thickness (mm)', el.thickness ?? 0.3, 0.1)}<div class="form-group"><label class="form-label">Colour</label><input type="color" class="form-input" data-prop="color" value="${esc(el.color || '#333333')}"></div></div>`;
      break;
    case 'table':
      extra = `<div class="pts-grid2">${num('fontSize', 'Font size (pt)', el.fontSize ?? 8)}${num('imageSize', 'Picture size (mm)', el.imageSize ?? 14, 1)}
          <div class="form-group"><label class="form-label">Header shade</label><input type="text" class="form-input" data-prop="headerBg" value="${esc(el.headerBg || '')}"></div></div>
        <label style="display:flex;gap:6px;font-size:12.5px;margin-bottom:8px;"><input type="checkbox" data-rowlines ${el.rowLines === false ? '' : 'checked'}>Line between rows</label>
        <div class="pts-section-title">Columns (width is relative)</div>
        <div class="pts-cols">${(el.columns || []).map((c, i) => `
          <div class="pts-col">
            <input class="form-input" data-col="${i}" data-colprop="label" value="${esc(c.label)}">
            <input class="form-input" type="number" min="1" data-col="${i}" data-colprop="width" value="${esc(c.width)}">
            <button type="button" data-colalign="${i}" title="Alignment">${(c.align || 'left')[0].toUpperCase()}</button>
            <button type="button" data-colmove="${i}" data-dir="-1" title="Move left">&uarr;</button>
            <button type="button" data-coldel="${i}" title="Remove column">&times;</button>
            <div class="pts-col-key">${esc(c.key)} · ${esc(c.format || 'auto')}</div>
            <div class="pts-col-under">${(c.under || []).map((u, j) => `<span class="pts-chip">${esc(u)} <button type="button" data-underdel="${i}" data-j="${j}" title="Remove this line">&times;</button></span>`).join('')}
              <select class="form-input" data-underadd="${i}" aria-label="Add a line under this column"><option value="">+ line under...</option>${(S.catalog.line || []).map(f => opt(f.key, `${f.group}: ${f.label}`, '')).join('')}</select></div>
          </div>`).join('')}</div>
        <div class="form-group" style="margin-top:8px;"><label class="form-label">Add column</label><select class="form-input" data-addcol><option value="">Choose...</option>${(S.catalog.line || []).map(f => opt(f.key, `${f.group}: ${f.label}`, '')).join('')}</select></div>
        <p class="pts-hint">Rows that do not fit continue on the next page with the column headings repeated. Everything above the table prints on the first page only, everything below it (tax summary, totals, signatures) on the last page only.</p>`;
      break;
    case 'totals': {
      const rows = el.rows && el.rows.length ? el.rows : T.DEFAULT_TOTAL_ROWS;
      extra = `<div class="pts-section-title">Rows</div>
        <div class="pts-cols">${rows.map((r, i) => `<div class="pts-col" style="grid-template-columns:1fr 26px 26px;"><input class="form-input" data-trow="${i}" value="${esc(r.label)}"><button type="button" data-trowmove="${i}">&uarr;</button><button type="button" data-trowdel="${i}">&times;</button><div class="pts-col-key">${esc(r.key)}${r.when ? ` · ${r.when === 'intra' ? 'within state' : 'across states'} only` : ''}</div></div>`).join('')}</div>
        <div class="form-group" style="margin-top:8px;"><label class="form-label">Add row</label><select class="form-input" data-addtrow><option value="">Choose...</option>${headerFields.filter(f => f.key.startsWith('totals.') || f.key.startsWith('fields.')).map(f => opt(f.key, f.label, '')).join('')}</select></div>`;
      break;
    }
    case 'qr':
      extra = `<div class="form-group"><label class="form-label">Value to encode</label><select class="form-input" data-prop="key">${headerFields.map(f => opt(f.key, `${f.group}: ${f.label}`, el.key)).join('')}</select></div>
        <p class="pts-hint">Prints nothing while the value is blank - an e-invoice QR appears once the IRP has acknowledged the document.</p>`;
      break;
    case 'taxSummary':
      extra = '<p class="pts-hint">One row per GST rate: taxable value with CGST + SGST within a state, or IGST across states - chosen automatically per document.</p>';
      break;
  }
  return `${common}${extra}
    <div style="display:flex;gap:6px;margin-top:12px;flex-wrap:wrap;">
      ${el.type === 'table' ? '' : '<button type="button" class="btn btn-outline btn-sm" data-act-el="dup">Duplicate</button>'}
      <button type="button" class="btn btn-outline btn-sm" data-act-el="front">To front</button>
      <button type="button" class="btn btn-outline btn-sm" data-act-el="back">To back</button>
      <button type="button" class="btn btn-ghost btn-sm" style="color:var(--danger-color);" data-act-el="del">Delete</button>
    </div>`;
}

function bindInspector(box, el) {
  const commit = (fn, full = true) => { snapshot(); fn(); full ? render() : (renderCanvas(), renderTop()); };
  if (!el) {
    box.querySelectorAll('[data-tp]').forEach(inp => inp.addEventListener('change', async () => {
      const k = inp.getAttribute('data-tp');
      // Stage 106.3: moving a till / sales invoice onto roll paper offers
      // the 3-inch bill layout rather than squeezing the A4 one.
      const toRoll = k === 'size' && T.isRollPage({ size: inp.value }) && !T.isRollPage(S.layout.page) && T.offersRollBill(S.txn);
      const relay = toRoll && await window.showCustomConfirm('Lay the page out as the 3-inch bill? Your current elements are replaced; Undo brings them back.', 'Roll paper');
      commit(() => {
        if (relay) S.layout = { ...T.rollBillLayout(S.txn), page: { ...T.rollBillLayout(S.txn).page, size: inp.value } };
        else S.layout.page = { ...(S.layout.page || {}), [k]: k === 'margin' ? Number(inp.value) || 0 : inp.value };
      });
    }));
    box.querySelectorAll('[data-tm]').forEach(inp => inp.addEventListener('change', () => { S[inp.getAttribute('data-tm')] = inp.value.trim(); S.dirty = true; renderTop(); }));
    const v = box.querySelector('[data-tm="vendor"]');
    if (v && window.attachLinkTypeahead) window.attachLinkTypeahead(v, 'Vendor');
    return;
  }
  box.querySelectorAll('[data-num]').forEach(inp => inp.addEventListener('change', () => commit(() => {
    const k = inp.getAttribute('data-num');
    el[k] = inp.value === '' ? undefined : Number(inp.value);
  }, false)));
  box.querySelectorAll('[data-prop]').forEach(inp => inp.addEventListener('change', () => commit(() => {
    el[inp.getAttribute('data-prop')] = inp.value;
  }, inp.getAttribute('data-prop') === 'block' || inp.getAttribute('data-prop') === 'pages')));
  const textArea = box.querySelector('textarea[data-prop="text"]');
  if (textArea) textArea.addEventListener('input', () => { el.text = textArea.value; S.dirty = true; renderCanvas(); });
  box.querySelectorAll('[data-tog]').forEach(b => b.addEventListener('click', () => commit(() => { const k = b.getAttribute('data-tog'); el[k] = !el[k]; })));
  box.querySelectorAll('[data-align]').forEach(b => b.addEventListener('click', () => commit(() => { el.align = b.getAttribute('data-align'); })));
  const tok = box.querySelector('[data-insert-token]');
  if (tok) tok.addEventListener('change', () => { if (tok.value) commit(() => { el.text = `${el.text || ''}{${tok.value}}`; }); });
  box.querySelectorAll('[data-show]').forEach(cb => cb.addEventListener('change', () => commit(() => {
    el.show = [...box.querySelectorAll('[data-show]')].filter(c => c.checked).map(c => c.getAttribute('data-show'));
  })));
  const up = box.querySelector('[data-upload]');
  if (up) up.addEventListener('change', () => {
    const f = up.files && up.files[0];
    if (!f) return;
    if (f.size > 400 * 1024) { window.showToast?.('That picture is over 400 KB - use a smaller logo.', { variant: 'warning' }); return; }
    const reader = new FileReader();
    reader.onload = () => commit(() => { el.src = reader.result; });
    reader.readAsDataURL(f);
  });
  const clr = box.querySelector('[data-clear-src]');
  if (clr) clr.addEventListener('click', () => commit(() => { delete el.src; }));
  const rl = box.querySelector('[data-rowlines]');
  if (rl) rl.addEventListener('change', () => commit(() => { el.rowLines = rl.checked; }));
  box.querySelectorAll('[data-col]').forEach(inp => inp.addEventListener('change', () => commit(() => {
    const c = el.columns[Number(inp.getAttribute('data-col'))];
    const k = inp.getAttribute('data-colprop');
    c[k] = k === 'width' ? Math.max(1, Number(inp.value) || 1) : inp.value;
  })));
  box.querySelectorAll('[data-colalign]').forEach(b => b.addEventListener('click', () => commit(() => {
    const c = el.columns[Number(b.getAttribute('data-colalign'))];
    c.align = { left: 'center', center: 'right', right: 'left' }[c.align || 'left'];
  })));
  box.querySelectorAll('[data-colmove]').forEach(b => b.addEventListener('click', () => commit(() => {
    const i = Number(b.getAttribute('data-colmove'));
    if (i > 0) [el.columns[i - 1], el.columns[i]] = [el.columns[i], el.columns[i - 1]];
  })));
  box.querySelectorAll('[data-coldel]').forEach(b => b.addEventListener('click', () => commit(() => { el.columns.splice(Number(b.getAttribute('data-coldel')), 1); })));
  // Stage 106.3: fields printed under a column's own, one per line.
  box.querySelectorAll('[data-underadd]').forEach(s => s.addEventListener('change', () => { if (s.value) commit(() => {
    const c = el.columns[Number(s.getAttribute('data-underadd'))];
    c.under = [...(c.under || []), s.value];
  }); }));
  box.querySelectorAll('[data-underdel]').forEach(b => b.addEventListener('click', () => commit(() => {
    const c = el.columns[Number(b.getAttribute('data-underdel'))];
    c.under = (c.under || []).filter((_, j) => j !== Number(b.getAttribute('data-j')));
  })));
  const addcol = box.querySelector('[data-addcol]');
  if (addcol) addcol.addEventListener('change', () => {
    const f = (S.catalog.line || []).find(x => x.key === addcol.value);
    if (f) addColumn(el, f);
  });
  const ensureRows = () => { if (!el.rows || !el.rows.length) el.rows = clone(T.DEFAULT_TOTAL_ROWS); };
  box.querySelectorAll('[data-trow]').forEach(inp => inp.addEventListener('change', () => commit(() => { ensureRows(); el.rows[Number(inp.getAttribute('data-trow'))].label = inp.value; })));
  box.querySelectorAll('[data-trowmove]').forEach(b => b.addEventListener('click', () => commit(() => {
    ensureRows();
    const i = Number(b.getAttribute('data-trowmove'));
    if (i > 0) [el.rows[i - 1], el.rows[i]] = [el.rows[i], el.rows[i - 1]];
  })));
  box.querySelectorAll('[data-trowdel]').forEach(b => b.addEventListener('click', () => commit(() => { ensureRows(); el.rows.splice(Number(b.getAttribute('data-trowdel')), 1); })));
  const addtrow = box.querySelector('[data-addtrow]');
  if (addtrow) addtrow.addEventListener('change', () => {
    const f = (S.catalog.header || []).find(x => x.key === addtrow.value);
    if (f) commit(() => { ensureRows(); el.rows.push({ key: f.key, label: f.label }); });
  });
  box.querySelectorAll('[data-act-el]').forEach(b => b.addEventListener('click', () => {
    const a = b.getAttribute('data-act-el');
    if (a === 'dup') return duplicate(el);
    commit(() => {
      const list = S.layout.elements;
      const i = list.indexOf(el);
      if (a === 'del') { list.splice(i, 1); S.sel = null; }
      if (a === 'front') { list.splice(i, 1); list.push(el); }
      if (a === 'back') { list.splice(i, 1); list.unshift(el); }
    });
  }));
}

// ---------------------------------------------------------------------------
// Gallery (Printing -> Document templates)
// ---------------------------------------------------------------------------

// Stage 88.4: Setup ▸ Printing ▸ Templates shows the gallery per module -
// only names the transactions of one section, title heads it.
export async function renderPrintTemplateGallery(container, { only = null, title = 'Document print templates' } = {}) {
  const txns = only ? T.TRANSACTIONS.filter(t => only.includes(t.key)) : T.TRANSACTIONS;
  const res = await api('/api/v1/doc/PrintTemplate');
  if (!res) return;
  if (!res.ok) { window.renderErrorPanel?.(container, 'Failed to load print templates.', () => window.renderView(currentView)); return; }
  const all = await res.json();
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.setAttribute('data-pts-gallery', '1');
  panel.innerHTML = `
    <div style="display:flex;justify-content:space-between;align-items:flex-end;gap:12px;flex-wrap:wrap;margin-bottom:14px;">
      <div>
        <h2 style="font-size:16px;font-weight:700;margin:0;">${esc(title)}</h2>
      </div>
      <div style="display:flex;gap:8px;align-items:flex-end;">
        <label class="form-label" style="display:flex;flex-direction:column;gap:4px;font-size:12px;">New template for
          <select class="form-input" data-gal="txn">${txns.map(t => `<option value="${t.key}">${esc(t.label)}</option>`).join('')}</select></label>
        <button type="button" class="btn btn-primary btn-sm" data-gal="new">+ New template</button>
      </div>
    </div>
    ${txns.map(t => {
      const list = all.filter(x => x.transaction === t.key);
      return `<div class="pts-section-title">${esc(t.label)}</div>
        <div class="pts-gallery">
          ${list.map(x => `
            <div class="pts-card">
              <h3>${esc(x.name || x.code)} ${x.is_default === 'Yes' ? '<span class="badge badge-success">Default</span>' : ''} ${x.status === 'Inactive' ? '<span class="badge badge-secondary">Inactive</span>' : ''}</h3>
              <div class="pts-card-meta">${esc([x.vendor ? `Vendor ${x.vendor}` : '', x.item_families ? `Designs ${x.item_families}` : '', x.item_categories ? `Categories ${x.item_categories}` : ''].filter(Boolean).join(' · ') || 'Any vendor, any product')}</div>
              <div class="pts-card-actions">
                <button type="button" class="btn btn-outline btn-sm" data-gal-edit="${esc(x.id)}">Design</button>
                <button type="button" class="btn btn-ghost btn-sm" data-gal-copy="${esc(x.id)}">Duplicate</button>
                ${x.is_default === 'Yes' ? '' : `<button type="button" class="btn btn-ghost btn-sm" data-gal-default="${esc(x.id)}" data-txn="${t.key}">Make default</button>`}
                <button type="button" class="btn btn-ghost btn-sm" style="color:var(--danger-color);" data-gal-del="${esc(x.id)}" data-name="${esc(x.name || x.code)}">Delete</button>
              </div>
            </div>`).join('')}
          <div class="pts-card">
            <h3>Standard layout <span class="badge badge-secondary">Built-in</span></h3>
            <div class="pts-card-meta">${list.length ? 'Used when none of the templates above fits.' : 'Used today - nothing customised yet.'} Brand, store and vendor blocks, items, GST split, totals, amount in words, signature.</div>
            <div class="pts-card-actions"><button type="button" class="btn btn-outline btn-sm" data-gal-std="${t.key}">Customise</button></div>
          </div>
        </div>`;
    }).join('')}`;
  container.appendChild(panel);
  const q = (k) => panel.querySelector(`[data-gal="${k}"]`);
  q('new').addEventListener('click', () => openPrintTemplateStudio({ txn: q('txn').value }));
  panel.querySelectorAll('[data-gal-std]').forEach(b => b.addEventListener('click', () => openPrintTemplateStudio({ txn: b.getAttribute('data-gal-std') })));
  panel.querySelectorAll('[data-gal-edit]').forEach(b => b.addEventListener('click', () => openPrintTemplateStudio({ templateId: b.getAttribute('data-gal-edit') })));
  panel.querySelectorAll('[data-gal-copy]').forEach(b => b.addEventListener('click', () => openPrintTemplateStudio({ copyFrom: b.getAttribute('data-gal-copy') })));
  panel.querySelectorAll('[data-gal-default]').forEach(b => b.addEventListener('click', async () => {
    const id = b.getAttribute('data-gal-default');
    if (await updateDocFields(id, { is_default: 'Yes' })) await clearOtherDefaults(b.getAttribute('data-txn'), id);
    window.renderView(currentView);
  }));
  panel.querySelectorAll('[data-gal-del]').forEach(b => b.addEventListener('click', async () => {
    if (!(await window.showCustomConfirm(`Delete the print template "${b.getAttribute('data-name')}"? Documents will print with the next best template, or the standard layout.`, 'Delete template'))) return;
    const r = await api(`/api/v1/doc/PrintTemplate/${encodeURIComponent(b.getAttribute('data-gal-del'))}`, { method: 'DELETE' });
    if (r && !r.ok) { await window.showApiError(r, 'Could not delete the template.'); return; }
    window.renderView(currentView);
  }));
}
