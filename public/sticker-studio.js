// Stage 58: Sticker Studio - the full-screen label designer and the template
// gallery. Everything it draws goes through sticker-engine.js's drawLabel, the
// same function the printers' bitmaps come from, so the canvas is the print.
//
// Layout (deliberately its own, not a copy of any other label tool):
//   top bar     - name, size chip, undo/redo, "preview with a real item",
//                 test print, save
//   insert rail - icon buttons; "Data" opens a searchable field drawer whose
//                 rows can be clicked or dragged onto the label
//   stage       - mm rulers, the label at any zoom, the rest of a multi-up
//                 row as ghosts, smart guides, marquee select, and a floating
//                 quick bar over the selection
//   dock        - zoom, grid, snap, thermal (dot-exact) view, align/distribute
//   inspector   - Element / Label / Layers tabs
//   status line - cursor position in mm, scan checks, warnings
//
// Vanilla JS on purpose (no framework, no build step), like the rest of public/.

const MODULE_QUERY = new URL(import.meta.url).search;
const E = await import('./sticker-engine.js' + MODULE_QUERY);
await E.stickerStylesReady; // the studio's .stk-* styles, before its first paint

const CSS_PX_PER_MM = 96 / 25.4;
const ZOOM_MIN = CSS_PX_PER_MM * 0.5;
const ZOOM_MAX = CSS_PX_PER_MM * 12;
const HISTORY_LIMIT = 100;
const ELEMENTS_CHAR_BUDGET = 10000; // platform.field_max_length default

const ICONS = {
  back: '<path d="M15 18l-6-6 6-6"/>',
  text: '<path d="M5 6V4h14v2M12 4v16M9 20h6"/>',
  data: '<path d="M8 3H6a2 2 0 0 0-2 2v4a2 2 0 0 1-2 2 2 2 0 0 1 2 2v4a2 2 0 0 0 2 2h2M16 3h2a2 2 0 0 1 2 2v4a2 2 0 0 0 2 2 2 2 0 0 0-2 2v4a2 2 0 0 1-2 2h-2"/>',
  barcode: '<path d="M3 5v14M6 5v14M10 5v14M12 5v14M15 5v14M19 5v14M21 5v14"/>',
  qr: '<rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><path d="M14 14h3v3h-3zM20 14v.01M14 20h.01M17 17h4v4h-4"/>',
  line: '<path d="M4 12h16"/>',
  box: '<rect x="4" y="6" width="16" height="12" rx="1.5"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="2"/><path d="M21 16l-5-5-9 9"/>',
  undo: '<path d="M9 14L4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/>',
  redo: '<path d="M15 14l5-5-5-5"/><path d="M20 9H9a5 5 0 0 0 0 10h3"/>',
  print: '<path d="M6 9V3h12v6"/><rect x="3" y="9" width="18" height="8" rx="2"/><path d="M6 14h12v7H6z"/>',
  minus: '<path d="M5 12h14"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  grid: '<path d="M3 9h18M3 15h18M9 3v18M15 3v18"/>',
  magnet: '<path d="M6 3v8a6 6 0 0 0 12 0V3M6 7h4M14 7h4"/>',
  dots: '<circle cx="6" cy="6" r="1.4"/><circle cx="12" cy="6" r="1.4"/><circle cx="18" cy="6" r="1.4"/><circle cx="6" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="18" cy="12" r="1.4"/><circle cx="6" cy="18" r="1.4"/><circle cx="12" cy="18" r="1.4"/><circle cx="18" cy="18" r="1.4"/>',
  rotate: '<path d="M21 12a9 9 0 1 1-3-6.7"/><path d="M21 3v6h-6"/>',
  copy: '<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M4 16V5a2 2 0 0 1 2-2h11"/>',
  front: '<rect x="8" y="8" width="12" height="12" rx="1.5"/><path d="M4 16V4h12"/>',
  back2: '<rect x="4" y="4" width="12" height="12" rx="1.5"/><path d="M20 8v12H8"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  unlock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.5-2"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  eyeoff: '<path d="M3 3l18 18M10.6 10.6a3 3 0 0 0 4.2 4.2M9.9 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3.2 4.2M6.1 6.1C3.6 7.8 2 12 2 12s3.5 7 10 7a9.8 9.8 0 0 0 4-.9"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  keyboard: '<rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10"/>',
  alignL: '<path d="M4 3v18M8 7h10v4H8zM8 14h6v4H8z"/>',
  alignCH: '<path d="M12 3v18M6 7h12v4H6zM8 14h8v4H8z"/>',
  alignR: '<path d="M20 3v18M6 7h10v4H6zM10 14h6v4h-6z"/>',
  alignT: '<path d="M3 4h18M7 8v10h4V8zM14 8v6h4V8z"/>',
  alignCV: '<path d="M3 12h18M7 6v12h4V6zM14 8v8h4V8z"/>',
  alignB: '<path d="M3 20h18M7 6v10h4V6zM14 10v6h4v-6z"/>',
  distH: '<path d="M4 3v18M20 3v18M9 8h6v8H9z"/>',
  distV: '<path d="M3 4h18M3 20h18M8 9h8v6H8z"/>',
  close: '<path d="M6 6l12 12M18 6L6 18"/>'
};

function icon(name, size = 18) {
  return `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name] || ''}</svg>`;
}

function esc(s) {
  return window.escapeHTMLText ? window.escapeHTMLText(s) : String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function round2(v) { return Math.round(v * 100) / 100; }
function clamp(v, a, b) { return Math.max(a, Math.min(b, v)); }

// ---------------------------------------------------------------------------
// Studio state
// ---------------------------------------------------------------------------

const S = {
  root: null,
  t: null,              // engine template
  isNew: true,
  sel: new Set(),
  zoom: CSS_PX_PER_MM * 4,
  grid: true,
  snap: true,
  mono: false,
  dirty: false,
  undo: [],
  redo: [],
  label: E.SAMPLE_LABEL,
  previewName: '',
  fields: [],           // [{ key, label, type }]
  tab: 'element',
  clipboard: null,
  drawer: false,
  drawerQuery: '',
  onClose: null,
  keyHandler: null,
  resizeHandler: null,
  cursor: null
};

function snapshot() {
  const t = S.t;
  return JSON.stringify({ w: t.w, h: t.h, settings: t.settings, elements: t.elements, name: t.name, categories: t.categories, is_default: t.is_default, status: t.status, code: t.code });
}

function restore(snap) {
  const o = JSON.parse(snap);
  Object.assign(S.t, o);
  S.sel = new Set([...S.sel].filter(id => S.t.elements.some(e => e.id === id)));
}

let pendingBefore = null;
// Call before a change; the snapshot is pushed once the change is committed.
function beginChange() {
  if (pendingBefore === null) pendingBefore = snapshot();
}
function commitChange() {
  if (pendingBefore === null) return;
  const now = snapshot();
  if (now !== pendingBefore) {
    S.undo.push(pendingBefore);
    if (S.undo.length > HISTORY_LIMIT) S.undo.shift();
    S.redo = [];
    setDirty(true);
  }
  pendingBefore = null;
  updateHistoryButtons();
}
function change(fn, opts = {}) {
  beginChange();
  fn();
  commitChange();
  refresh(opts);
}

function undo() {
  if (!S.undo.length) return;
  S.redo.push(snapshot());
  restore(S.undo.pop());
  setDirty(true);
  refresh({ inspector: true });
  updateHistoryButtons();
}
function redo() {
  if (!S.redo.length) return;
  S.undo.push(snapshot());
  restore(S.redo.pop());
  setDirty(true);
  refresh({ inspector: true });
  updateHistoryButtons();
}

function setDirty(d) {
  S.dirty = d;
  const dot = S.root && S.root.querySelector('.stk-dirty');
  if (dot) dot.hidden = !d;
}

function updateHistoryButtons() {
  if (!S.root) return;
  const u = S.root.querySelector('[data-sa="undo"]');
  const r = S.root.querySelector('[data-sa="redo"]');
  if (u) u.disabled = !S.undo.length;
  if (r) r.disabled = !S.redo.length;
}

function selected() {
  return S.t.elements.filter(e => S.sel.has(e.id));
}

function primary() {
  const s = selected();
  return s.length === 1 ? s[0] : null;
}

function fieldLabel(key) {
  if (key === 'custom') return 'Custom text';
  const f = S.fields.find(x => x.key === key);
  return f ? f.label : key;
}

function elementName(el) {
  if (el.name) return el.name;
  switch (el.kind) {
    case 'text': return el.field === 'custom' ? (String(el.text || '').slice(0, 28) || 'Text') : fieldLabel(el.field);
    case 'barcode': return `Barcode · ${el.field === 'custom' ? 'custom' : fieldLabel(el.field)}`;
    case 'qr': return `QR · ${el.field === 'custom' ? 'custom' : fieldLabel(el.field)}`;
    case 'line': return 'Line';
    case 'box': return el.fill ? 'Filled box' : 'Box';
    case 'image': return 'Logo / image';
    default: return el.kind;
  }
}

// ---------------------------------------------------------------------------
// Data fields
// ---------------------------------------------------------------------------

async function loadFields() {
  const base = E.BUILTIN_FIELDS.map(f => ({ key: f.key, label: f.label, type: 'builtin', sample: f.sample, group: 'Label' }));
  const known = new Set(base.map(f => f.key));
  const extra = [];
  try {
    const res = await window.apiFetch('/api/v1/doc/Item/meta');
    if (res && res.ok) {
      const meta = await res.json();
      for (const f of meta || []) {
        if (!f.fieldname || known.has(f.fieldname) || f.fieldname === 'id') continue;
        if (f.fieldtype === 'JSONTable' || f.fieldtype === 'JSONMap') continue;
        extra.push({ key: f.fieldname, label: f.label || f.fieldname, type: f.fieldtype, options: f.options, group: 'Item' });
      }
    }
  } catch (e) { /* the built-in fields still work */ }
  extra.sort((a, b) => a.label.localeCompare(b.label));
  S.fields = base.concat(extra);
  const sampleFields = {};
  for (const f of extra) sampleFields[f.key] = sampleFor(f);
  S.label = Object.assign({}, E.SAMPLE_LABEL, { fields: sampleFields });
}

function sampleFor(f) {
  const k = f.key.toLowerCase();
  if (/weight|wt/.test(k)) return '38.505';
  if (/mrp|price|cost|amount|rate/.test(k)) return '12499';
  if (/purity|karat|carat/.test(k)) return '925';
  if (/size/.test(k)) return '12';
  if (f.type === 'Number') return '12.5';
  if (f.type === 'Date') return '2026-10-07';
  if (f.type === 'Check') return 'true';
  if (f.type === 'Select' && f.options) return String(f.options).split(/[,\n]/)[0].trim() || f.label;
  return f.label;
}

// ---------------------------------------------------------------------------
// Open / close
// ---------------------------------------------------------------------------

export async function openStickerStudio({ templateId = null, copyFrom = null, onClose = null } = {}) {
  S.onClose = onClose;
  S.sel = new Set();
  S.undo = []; S.redo = [];
  S.dirty = false;
  S.mono = false;
  S.tab = 'element';
  S.previewName = '';
  pendingBefore = null;

  const source = templateId || copyFrom;
  let t = null;
  if (source) {
    const res = await window.apiFetch(`/api/v1/doc/StickerTemplate/${encodeURIComponent(source)}`);
    if (!res) return;
    if (!res.ok) {
      await window.showApiError?.(res, 'Could not open that template.');
      return;
    }
    t = E.templateFromDoc(await res.json());
  }
  if (copyFrom && t) {
    t = Object.assign(t, { id: '', code: '', name: `${t.name} copy`, is_default: false });
  }
  S.isNew = !templateId;
  if (!t) {
    t = E.templateFromDoc({ label_width_mm: 50, label_height_mm: 25 });
    t.name = '';
  }
  S.t = t;
  await loadFields();
  await E.ensureTemplateAssets(S.t);
  mount();
  if (S.isNew && !copyFrom) S.tab = 'label';
  fitZoom();
  refresh({ inspector: true });
  if (S.isNew && !copyFrom) showStarter();
  else S.root.querySelector('.stk-name').focus();
}

async function closeStudio(force = false) {
  if (!force && S.dirty) {
    const ok = await window.showCustomConfirm('Leave the studio without saving your changes?', 'Unsaved changes');
    if (!ok) return;
  }
  document.removeEventListener('keydown', S.keyHandler, true);
  window.removeEventListener('resize', S.resizeHandler);
  S.stageObserver?.disconnect();
  S.root?.remove();
  S.root = null;
  document.body.classList.remove('stk-studio-open');
  if (S.onClose) S.onClose();
}

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

function mount() {
  document.querySelector('.stk-studio')?.remove();
  const root = document.createElement('div');
  root.className = 'stk-studio';
  root.setAttribute('role', 'dialog');
  root.setAttribute('aria-label', 'Sticker Studio');
  root.innerHTML = `
    <header class="stk-bar">
      <button class="stk-icon-btn" data-sa="close" title="Back to templates (Esc)">${icon('back')}</button>
      <div class="stk-title">
        <input class="stk-name" type="text" placeholder="Name this template" aria-label="Template name" maxlength="80">
        <span class="stk-dirty" title="Unsaved changes" hidden></span>
      </div>
      <button class="stk-chip" data-sa="size" title="Label size and roll setup"></button>
      <div class="stk-bar-group">
        <button class="stk-icon-btn" data-sa="undo" title="Undo (Ctrl+Z)" disabled>${icon('undo')}</button>
        <button class="stk-icon-btn" data-sa="redo" title="Redo (Ctrl+Shift+Z)" disabled>${icon('redo')}</button>
      </div>
      <div class="stk-preview-with">
        <span class="stk-preview-label">Preview with</span>
        <input type="text" class="stk-preview-input" placeholder="Sample data - search an item…" autocomplete="off" aria-label="Preview with a real item">
        <button class="stk-icon-btn stk-preview-clear" data-sa="preview-clear" title="Back to sample data" hidden>${icon('close', 14)}</button>
      </div>
      <div class="stk-bar-spacer"></div>
      <button class="btn btn-outline btn-sm stk-btn-icon" data-sa="test-print">${icon('print', 16)}<span>Test print</span></button>
      <button class="btn btn-primary btn-sm" data-sa="save">Save</button>
    </header>
    <div class="stk-body">
      <nav class="stk-rail" aria-label="Insert">
        <span class="stk-rail-title">Insert</span>
        <button class="stk-rail-btn" data-add="text" title="Text">${icon('text', 20)}<span>Text</span></button>
        <button class="stk-rail-btn" data-sa="drawer" title="Item data field">${icon('data', 20)}<span>Data</span></button>
        <button class="stk-rail-btn" data-add="barcode" title="Barcode">${icon('barcode', 20)}<span>Barcode</span></button>
        <button class="stk-rail-btn" data-add="qr" title="QR code">${icon('qr', 20)}<span>QR</span></button>
        <button class="stk-rail-btn" data-add="line" title="Line">${icon('line', 20)}<span>Line</span></button>
        <button class="stk-rail-btn" data-add="box" title="Box">${icon('box', 20)}<span>Box</span></button>
        <button class="stk-rail-btn" data-add="image" title="Logo or image">${icon('image', 20)}<span>Logo</span></button>
        <div class="stk-rail-spacer"></div>
        <button class="stk-rail-btn" data-sa="shortcuts" title="Keyboard shortcuts">${icon('keyboard', 20)}<span>Keys</span></button>
      </nav>
      <aside class="stk-drawer" hidden>
        <div class="stk-drawer-head">
          <strong>Data fields</strong>
          <button class="stk-icon-btn" data-sa="drawer" title="Close">${icon('close', 14)}</button>
        </div>
        <input type="search" class="form-input stk-drawer-search" placeholder="Search fields…" aria-label="Search fields">
        <p class="stk-drawer-hint">Click to add, or drag onto the label. Every Item field is here - including ones added later.</p>
        <div class="stk-drawer-list"></div>
      </aside>
      <main class="stk-stage">
        <div class="stk-scroll">
          <div class="stk-sheet">
            <canvas class="stk-ruler stk-ruler-x"></canvas>
            <canvas class="stk-ruler stk-ruler-y"></canvas>
            <div class="stk-roll">
              <div class="stk-board">
                <canvas class="stk-paint"></canvas>
                <div class="stk-grid"></div>
                <div class="stk-overlay"></div>
              </div>
            </div>
          </div>
        </div>
        <div class="stk-quickbar" hidden>
          <button data-q="rotate" title="Rotate 90° (R)">${icon('rotate', 16)}</button>
          <button data-q="duplicate" title="Duplicate (Ctrl+D)">${icon('copy', 16)}</button>
          <button data-q="front" title="Bring to front">${icon('front', 16)}</button>
          <button data-q="back" title="Send to back">${icon('back2', 16)}</button>
          <button data-q="lock" title="Lock / unlock">${icon('lock', 16)}</button>
          <button data-q="delete" class="stk-danger" title="Delete (Del)">${icon('trash', 16)}</button>
        </div>
        <div class="stk-dock">
          <button class="stk-icon-btn" data-sa="zoom-out" title="Zoom out (-)">${icon('minus', 16)}</button>
          <button class="stk-zoom-val" data-sa="zoom-fit" title="Fit to screen (0)">100%</button>
          <button class="stk-icon-btn" data-sa="zoom-in" title="Zoom in (+)">${icon('plus', 16)}</button>
          <button class="stk-dock-text" data-sa="zoom-actual" title="Actual size on a 96-dpi screen">1:1</button>
          <span class="stk-dock-sep"></span>
          <button class="stk-toggle" data-sa="grid" title="Show 1 mm grid">${icon('grid', 16)}<span>Grid</span></button>
          <button class="stk-toggle" data-sa="snap" title="Snap to grid, edges and centres (hold Alt to bypass)">${icon('magnet', 16)}<span>Snap</span></button>
          <button class="stk-toggle" data-sa="mono" title="Show exactly the dots the printer will burn">${icon('dots', 16)}<span>Printer view</span></button>
          <span class="stk-dock-align" hidden>
            <span class="stk-dock-sep"></span>
            <button class="stk-icon-btn" data-align="left" title="Align left">${icon('alignL', 16)}</button>
            <button class="stk-icon-btn" data-align="hcenter" title="Align centres horizontally">${icon('alignCH', 16)}</button>
            <button class="stk-icon-btn" data-align="right" title="Align right">${icon('alignR', 16)}</button>
            <button class="stk-icon-btn" data-align="top" title="Align top">${icon('alignT', 16)}</button>
            <button class="stk-icon-btn" data-align="vcenter" title="Align middles">${icon('alignCV', 16)}</button>
            <button class="stk-icon-btn" data-align="bottom" title="Align bottom">${icon('alignB', 16)}</button>
            <button class="stk-icon-btn" data-align="dist-h" title="Distribute horizontally (3+)">${icon('distH', 16)}</button>
            <button class="stk-icon-btn" data-align="dist-v" title="Distribute vertically (3+)">${icon('distV', 16)}</button>
          </span>
        </div>
        <div class="stk-starter" hidden></div>
      </main>
      <aside class="stk-inspector">
        <div class="stk-tabs" role="tablist">
          <button role="tab" data-tab="element">Element</button>
          <button role="tab" data-tab="label">Label</button>
          <button role="tab" data-tab="layers">Layers</button>
        </div>
        <div class="stk-tab-body"></div>
      </aside>
    </div>
    <footer class="stk-status">
      <span class="stk-status-cursor">—</span>
      <span class="stk-status-sel"></span>
      <span class="stk-status-warn"></span>
    </footer>
    <input type="file" class="stk-file" accept="image/png,image/jpeg,image/svg+xml,image/webp,image/gif" hidden>
  `;
  document.body.appendChild(root);
  document.body.classList.add('stk-studio-open');
  S.root = root;
  clearEnvironmentBanner();

  const name = root.querySelector('.stk-name');
  name.value = S.t.name || '';
  name.addEventListener('focus', beginChange);
  name.addEventListener('input', () => { S.t.name = name.value; setDirty(true); });
  name.addEventListener('change', commitChange);
  name.addEventListener('blur', commitChange);

  root.addEventListener('click', onRootClick);
  root.querySelector('.stk-file').addEventListener('change', onImageFile);
  root.querySelector('.stk-drawer-search').addEventListener('input', e => { S.drawerQuery = e.target.value; renderDrawer(); });
  root.querySelectorAll('.stk-tabs [data-tab]').forEach(b => b.addEventListener('click', () => { S.tab = b.dataset.tab; renderInspector(); }));
  root.querySelector('.stk-quickbar').addEventListener('click', onQuickbar);

  const overlay = root.querySelector('.stk-overlay');
  overlay.addEventListener('pointerdown', onOverlayPointerDown);
  overlay.addEventListener('dblclick', onOverlayDblClick);
  const scroll = root.querySelector('.stk-scroll');
  scroll.addEventListener('pointerdown', e => { if (e.target === scroll || e.target.classList.contains('stk-sheet') || e.target.classList.contains('stk-roll')) startMarquee(e); });
  scroll.addEventListener('pointermove', onCursorMove);
  scroll.addEventListener('pointerleave', () => { S.cursor = null; renderStatus(); drawRulers(); });
  scroll.addEventListener('wheel', e => {
    if (!e.ctrlKey) return;
    e.preventDefault();
    setZoom(S.zoom * (e.deltaY < 0 ? 1.12 : 1 / 1.12));
  }, { passive: false });
  const board = root.querySelector('.stk-board');
  board.addEventListener('dragover', e => { if (e.dataTransfer.types.includes('text/x-stk-field')) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; } });
  board.addEventListener('drop', e => {
    const key = e.dataTransfer.getData('text/x-stk-field');
    if (!key) return;
    e.preventDefault();
    const r = board.getBoundingClientRect();
    addElement('text', { field: key, at: { x: (e.clientX - r.left) / S.zoom, y: (e.clientY - r.top) / S.zoom } });
  });

  attachPreviewPicker(root.querySelector('.stk-preview-input'));

  S.keyHandler = onKey;
  document.addEventListener('keydown', S.keyHandler, true);
  S.resizeHandler = () => { clearEnvironmentBanner(); positionQuickbar(); };
  window.addEventListener('resize', S.resizeHandler);
  // Stay fitted to the workspace (window resize, drawer opening) until the
  // user picks a zoom of their own.
  S.autoFit = true;
  S.stageObserver = new ResizeObserver(() => {
    if (!S.root || !S.autoFit) return;
    const before = S.zoom;
    fitZoom();
    if (Math.abs(before - S.zoom) > 0.01) refresh({ inspector: false });
  });
  S.stageObserver.observe(root.querySelector('.stk-scroll'));
  updateHistoryButtons();
  renderDrawer();
}

// A non-production server shows a fixed environment banner at the top of the
// page (and pads <body> by its height); the studio starts below it so the
// banner stays readable and never covers the studio's own top bar.
function clearEnvironmentBanner() {
  const banner = document.getElementById('environment-banner');
  if (S.root) S.root.style.top = banner ? banner.offsetHeight + 'px' : '0px';
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

function refresh(opts = {}) {
  if (!S.root) return;
  paint();
  renderOverlay();
  drawRulers();
  renderTopBar();
  renderDock();
  renderStatus();
  if (opts.inspector !== false) renderInspector();
}

function renderTopBar() {
  const t = S.t;
  const chip = S.root.querySelector('[data-sa="size"]');
  const per = t.settings.per_row > 1 ? ` · ${t.settings.per_row}-up` : '';
  chip.textContent = `${round2(t.w)} × ${round2(t.h)} mm · ${t.settings.dpi} dpi${per}`;
  const name = S.root.querySelector('.stk-name');
  if (document.activeElement !== name) name.value = t.name || '';
}

function paint() {
  const t = S.t;
  const board = S.root.querySelector('.stk-board');
  const canvas = S.root.querySelector('.stk-paint');
  board.style.width = t.w * S.zoom + 'px';
  board.style.height = t.h * S.zoom + 'px';
  E.paintPreview(canvas, t, S.label, { scale: S.zoom, mono: S.mono });

  const grid = S.root.querySelector('.stk-grid');
  grid.hidden = !S.grid;
  if (S.grid) {
    const mm = S.zoom, five = S.zoom * 5;
    grid.style.backgroundSize = `${five}px ${five}px, ${five}px ${five}px, ${mm}px ${mm}px, ${mm}px ${mm}px`;
    grid.classList.toggle('stk-grid-fine', mm >= 6);
  }

  // The rest of a multi-up row, drawn as faded ghosts so the gap and liner
  // read at a glance.
  const roll = S.root.querySelector('.stk-roll');
  roll.querySelectorAll('.stk-ghost').forEach(g => g.remove());
  const s = t.settings;
  roll.style.paddingRight = '0px';
  for (let i = 1; i < s.per_row; i++) {
    const g = document.createElement('canvas');
    g.className = 'stk-ghost';
    g.style.left = (i * (t.w + s.gap_h_mm)) * S.zoom + 'px';
    roll.appendChild(g);
    E.paintPreview(g, t, S.label, { scale: S.zoom, mono: S.mono });
  }
  const mediaW = Math.max(Number(s.media_w_mm) || 0, s.per_row * t.w + (s.per_row - 1) * s.gap_h_mm);
  roll.style.width = mediaW * S.zoom + 'px';
  roll.style.height = t.h * S.zoom + 'px';
  roll.classList.toggle('stk-roll-multi', s.per_row > 1);
}

function renderOverlay() {
  const overlay = S.root.querySelector('.stk-overlay');
  const z = S.zoom;
  const single = primary();
  let html = '';
  for (const el of S.t.elements) {
    const cls = ['stk-el', S.sel.has(el.id) ? 'is-sel' : '', el.locked ? 'is-locked' : '', el.hidden ? 'is-hidden' : ''].join(' ');
    html += `<div class="${cls}" data-id="${esc(el.id)}" style="left:${el.x_mm * z}px; top:${el.y_mm * z}px; width:${el.w_mm * z}px; height:${el.h_mm * z}px;">`;
    if (single && single.id === el.id && !el.locked) {
      for (const h of ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w']) html += `<span class="stk-h stk-h-${h}" data-h="${h}"></span>`;
      html += `<span class="stk-size-tag">${round2(el.w_mm)} × ${round2(el.h_mm)}</span>`;
    }
    html += '</div>';
  }
  overlay.innerHTML = html;
  positionQuickbar();
}

function positionQuickbar() {
  if (!S.root) return;
  const qb = S.root.querySelector('.stk-quickbar');
  const els = selected();
  if (!els.length) { qb.hidden = true; return; }
  const stage = S.root.querySelector('.stk-stage').getBoundingClientRect();
  const board = S.root.querySelector('.stk-board').getBoundingClientRect();
  const bb = bbox(els);
  qb.hidden = false;
  const lockBtn = qb.querySelector('[data-q="lock"]');
  const allLocked = els.every(e => e.locked);
  lockBtn.innerHTML = icon(allLocked ? 'unlock' : 'lock', 16);
  lockBtn.title = allLocked ? 'Unlock' : 'Lock';
  // Parked outside the label (above its ruler, else below the label) so it
  // never covers the content of a short tag; it follows the selection
  // horizontally.
  const x = board.left - stage.left + (bb.x + bb.w / 2) * S.zoom - qb.offsetWidth / 2;
  let y = board.top - stage.top - 30 - qb.offsetHeight - 8;
  if (y < 6) y = board.bottom - stage.top + 14;
  qb.style.left = clamp(x, 6, stage.width - qb.offsetWidth - 6) + 'px';
  qb.style.top = clamp(y, 6, stage.height - qb.offsetHeight - 60) + 'px';
}

function renderDock() {
  const r = S.root;
  r.querySelector('.stk-zoom-val').textContent = Math.round((S.zoom / CSS_PX_PER_MM) * 100) + '%';
  r.querySelector('[data-sa="grid"]').classList.toggle('is-on', S.grid);
  r.querySelector('[data-sa="snap"]').classList.toggle('is-on', S.snap);
  r.querySelector('[data-sa="mono"]').classList.toggle('is-on', S.mono);
  r.querySelector('.stk-dock-align').hidden = S.sel.size < 2;
}

function drawRulers() {
  if (!S.root) return;
  const t = S.t, z = S.zoom;
  const s = t.settings;
  const mediaW = Math.max(Number(s.media_w_mm) || 0, s.per_row * t.w + (s.per_row - 1) * s.gap_h_mm);
  const ratio = window.devicePixelRatio || 1;
  const styles = getComputedStyle(S.root);
  const ink = styles.getPropertyValue('--stk-ruler-ink').trim() || '#64748b';
  const accent = styles.getPropertyValue('--stk-accent').trim() || '#2563eb';
  const sel = selected().length ? bbox(selected()) : null;

  const draw = (canvas, lengthMM, horizontal) => {
    const len = (lengthMM + 6) * z;
    const thick = 20;
    canvas.style.width = (horizontal ? len : thick) + 'px';
    canvas.style.height = (horizontal ? thick : len) + 'px';
    canvas.width = Math.round((horizontal ? len : thick) * ratio);
    canvas.height = Math.round((horizontal ? thick : len) * ratio);
    const c = canvas.getContext('2d');
    c.setTransform(ratio, 0, 0, ratio, 0, 0);
    c.clearRect(0, 0, len, thick);
    if (sel) {
      c.fillStyle = accent + '33';
      const a = (horizontal ? sel.x : sel.y) * z, b = (horizontal ? sel.w : sel.h) * z;
      if (horizontal) c.fillRect(a, 0, b, thick); else c.fillRect(0, a, thick, b);
    }
    c.strokeStyle = ink; c.fillStyle = ink;
    c.font = '9px ' + (styles.getPropertyValue('--font-numeric') || 'monospace');
    c.lineWidth = 1;
    const labelEvery = z >= 12 ? 1 : z >= 6 ? 5 : 10;
    for (let mm = 0; mm <= lengthMM + 5; mm++) {
      const p = Math.round(mm * z) + 0.5;
      const tick = mm % 10 === 0 ? 10 : mm % 5 === 0 ? 7 : 4;
      if (z < 3 && mm % 5 !== 0) continue;
      c.beginPath();
      if (horizontal) { c.moveTo(p, thick); c.lineTo(p, thick - tick); } else { c.moveTo(thick, p); c.lineTo(thick - tick, p); }
      c.stroke();
      if (mm % labelEvery === 0 && mm > 0) {
        if (horizontal) c.fillText(String(mm), p + 2, 9);
        else { c.save(); c.translate(9, p - 2); c.rotate(-Math.PI / 2); c.fillText(String(mm), 0, 0); c.restore(); }
      }
    }
    if (S.cursor) {
      c.strokeStyle = accent;
      const p = Math.round((horizontal ? S.cursor.x : S.cursor.y) * z) + 0.5;
      c.beginPath();
      if (horizontal) { c.moveTo(p, 0); c.lineTo(p, thick); } else { c.moveTo(0, p); c.lineTo(thick, p); }
      c.stroke();
    }
  };
  draw(S.root.querySelector('.stk-ruler-x'), mediaW, true);
  draw(S.root.querySelector('.stk-ruler-y'), t.h, false);
}

function renderStatus() {
  if (!S.root) return;
  const r = S.root;
  r.querySelector('.stk-status-cursor').textContent = S.cursor ? `x ${S.cursor.x.toFixed(1)}  y ${S.cursor.y.toFixed(1)} mm` : `${S.t.elements.length} element${S.t.elements.length === 1 ? '' : 's'}`;
  const els = selected();
  if (els.length === 1) {
    const e = els[0];
    r.querySelector('.stk-status-sel').textContent = `${elementName(e)} · ${round2(e.x_mm)}, ${round2(e.y_mm)} · ${round2(e.w_mm)} × ${round2(e.h_mm)} mm`;
  } else {
    r.querySelector('.stk-status-sel').textContent = els.length ? `${els.length} selected` : '';
  }
  const warns = collectWarnings();
  const w = r.querySelector('.stk-status-warn');
  w.textContent = warns.length ? `⚠ ${warns[0]}${warns.length > 1 ? `  (+${warns.length - 1} more)` : ''}` : '✓ Ready to print';
  w.title = warns.join('\n');
  w.classList.toggle('is-warn', warns.length > 0);
}

function collectWarnings() {
  const t = S.t, out = [];
  for (const el of t.elements) {
    if (el.hidden) continue;
    if (el.x_mm < -0.01 || el.y_mm < -0.01 || el.x_mm + el.w_mm > t.w + 0.01 || el.y_mm + el.h_mm > t.h + 0.01) out.push(`"${elementName(el)}" runs off the label`);
    if (el.kind === 'barcode' || el.kind === 'qr') {
      const fit = E.barcodeFit(el, S.label, t.settings.dpi);
      if (!fit.ok) out.push(`${elementName(el)}: ${fit.message}`);
    }
  }
  const size = JSON.stringify(t.elements.map(E.compactElement)).length;
  if (size > ELEMENTS_CHAR_BUDGET * 0.9) out.push(`Layout is ${size.toLocaleString()} of ${ELEMENTS_CHAR_BUDGET.toLocaleString()} characters the server stores per template`);
  return out;
}

// ---------------------------------------------------------------------------
// Inspector
// ---------------------------------------------------------------------------

function renderInspector() {
  if (!S.root) return;
  S.root.querySelectorAll('.stk-tabs [data-tab]').forEach(b => {
    b.classList.toggle('is-active', b.dataset.tab === S.tab);
    b.setAttribute('aria-selected', b.dataset.tab === S.tab ? 'true' : 'false');
  });
  const body = S.root.querySelector('.stk-tab-body');
  if (S.tab === 'label') body.innerHTML = labelTabHTML();
  else if (S.tab === 'layers') body.innerHTML = layersTabHTML();
  else body.innerHTML = elementTabHTML();
  bindInspector(body);
}

function fieldOptions(current, { allowCustom = true } = {}) {
  let html = allowCustom ? `<option value="custom" ${current === 'custom' ? 'selected' : ''}>Custom text / combine fields…</option>` : '';
  const groups = [['Label', 'On every label'], ['Item', 'Item fields']];
  for (const [g, title] of groups) {
    const list = S.fields.filter(f => f.group === g);
    if (!list.length) continue;
    html += `<optgroup label="${title}">${list.map(f => `<option value="${esc(f.key)}" ${current === f.key ? 'selected' : ''}>${esc(f.label)}</option>`).join('')}</optgroup>`;
  }
  return html;
}

function seg(key, value, options, { title } = {}) {
  return `<div class="stk-seg" role="group" ${title ? `aria-label="${esc(title)}"` : ''}>${options.map(o => `<button type="button" data-seg="${key}" data-v="${esc(o.v)}" class="${String(value) === String(o.v) ? 'is-on' : ''}" title="${esc(o.title || o.label)}">${o.label}</button>`).join('')}</div>`;
}

// One on/off button inside a .stk-seg group (bold, italic, uppercase).
function toggle(key, on, label, title, onValue = 'true') {
  return `<button type="button" data-toggle="${key}" data-on="${onValue}" class="${on ? 'is-on' : ''}" title="${esc(title)}" aria-pressed="${on ? 'true' : 'false'}">${label}</button>`;
}

function numInput(key, value, { step = 0.1, min, max, unit = 'mm', label, scope = 'el' } = {}) {
  return `<label class="stk-num"><span>${esc(label)}</span><input type="number" step="${step}" ${min !== undefined ? `min="${min}"` : ''} ${max !== undefined ? `max="${max}"` : ''} data-${scope}="${key}" data-type="num" value="${value === '' || value === undefined ? '' : round2(Number(value))}"><em>${unit}</em></label>`;
}

function elementTabHTML() {
  const els = selected();
  if (!els.length) {
    return `
      <div class="stk-empty">
        <div class="stk-empty-icon">${icon('dots', 28)}</div>
        <p><strong>Nothing selected.</strong></p>
        <p>Click an element on the label, drag a box around several, or add one from the <b>Insert</b> rail.</p>
        <ul class="stk-tips">
          <li><kbd>Shift</kbd> / <kbd>Ctrl</kbd>-click to select several</li>
          <li>Arrow keys nudge 0.1 mm, <kbd>Shift</kbd> + arrow 1 mm</li>
          <li>Hold <kbd>Alt</kbd> while dragging to skip snapping</li>
        </ul>
      </div>`;
  }
  if (els.length > 1) {
    const kinds = new Set(els.map(e => e.kind));
    const allText = kinds.size === 1 && kinds.has('text');
    return `
      <section class="stk-sec">
        <h4>${els.length} elements</h4>
        <p class="stk-muted">Use the align tools in the bottom dock, or change shared settings below - they apply to every selected element.</p>
        ${allText ? `
          <div class="stk-row">${fontSelect(els[0].font_family || '')}</div>
          <div class="stk-row">${numInput('font_size_mm', els[0].font_size_mm, { label: 'Size', step: 0.1, min: 0.5 })}
            ${seg('bold', els.every(e => e.bold) ? 'true' : 'false', [{ v: 'true', label: '<b>B</b>', title: 'Bold' }, { v: 'false', label: 'Regular' }])}</div>` : ''}
        <div class="stk-row">${seg('rotation', els.every(e => e.rotation === els[0].rotation) ? els[0].rotation : '', [{ v: 0, label: '0°' }, { v: 90, label: '90°' }, { v: 180, label: '180°' }, { v: 270, label: '270°' }])}</div>
        <div class="stk-actions">
          <button class="btn btn-outline btn-sm" data-q="duplicate">Duplicate</button>
          <button class="btn btn-outline btn-sm stk-danger-btn" data-q="delete">Delete ${els.length}</button>
        </div>
      </section>`;
  }
  const el = els[0];
  let content = '';
  if (el.kind === 'text') {
    content = `
      <section class="stk-sec">
        <h4>Content</h4>
        <label class="stk-field"><span>Prints</span><select data-el="field" data-type="text">${fieldOptions(el.field)}</select></label>
        ${el.field === 'custom' ? customTextHTML(el) : affixHTML(el)}
      </section>
      ${typographyHTML(el)}`;
  } else if (el.kind === 'barcode') {
    const fit = E.barcodeFit(el, S.label, S.t.settings.dpi);
    content = `
      <section class="stk-sec">
        <h4>Barcode</h4>
        <label class="stk-field"><span>Encodes</span><select data-el="field" data-type="text">${fieldOptions(el.field)}</select></label>
        ${el.field === 'custom' ? customTextHTML(el) : ''}
        <div class="stk-row">${seg('symbology', el.symbology, [{ v: 'code128', label: 'Code 128', title: 'Any text; switches to compact numeric mode for digit runs' }, { v: 'ean13', label: 'EAN-13', title: 'Retail 13-digit (falls back to Code 128 if the value is not a valid EAN)' }])}</div>
        <label class="stk-check"><input type="checkbox" data-el="show_text" data-type="bool" ${el.show_text ? 'checked' : ''}> Print the value under the bars</label>
        <label class="stk-check"><input type="checkbox" data-el="quiet_zone" data-type="bool" ${el.quiet_zone ? 'checked' : ''}> Keep a quiet zone (recommended)</label>
        ${el.show_text ? `<div class="stk-row">${numInput('font_size_mm', el.font_size_mm || 2.4, { label: 'Text size', step: 0.1, min: 0.5 })}</div>` : ''}
        ${scanBadge(fit)}
      </section>`;
  } else if (el.kind === 'qr') {
    const fit = E.barcodeFit(el, S.label, S.t.settings.dpi);
    content = `
      <section class="stk-sec">
        <h4>QR code</h4>
        <label class="stk-field"><span>Encodes</span><select data-el="field" data-type="text">${fieldOptions(el.field)}</select></label>
        ${el.field === 'custom' ? customTextHTML(el, 'e.g. https://shop.example/p/{sku}') : ''}
        <div class="stk-row"><span class="stk-row-label">Error correction</span>${seg('ecc', el.ecc, [{ v: 'L', label: 'L', title: 'Low (7%) - smallest' }, { v: 'M', label: 'M', title: 'Medium (15%)' }, { v: 'Q', label: 'Q', title: 'Quartile (25%)' }, { v: 'H', label: 'H', title: 'High (30%) - survives scratches' }])}</div>
        <label class="stk-check"><input type="checkbox" data-el="quiet_zone" data-type="bool" ${el.quiet_zone ? 'checked' : ''}> Keep a 4-module quiet zone</label>
        ${scanBadge(fit)}
      </section>`;
  } else if (el.kind === 'line') {
    content = `<section class="stk-sec"><h4>Line</h4><div class="stk-row">${numInput('thickness_mm', el.thickness_mm, { label: 'Thickness', step: 0.05, min: 0.05 })}</div><p class="stk-muted">Draws along the longer side of its box.</p></section>`;
  } else if (el.kind === 'box') {
    content = `<section class="stk-sec"><h4>Box</h4>
      <div class="stk-row">${numInput('thickness_mm', el.thickness_mm, { label: 'Border', step: 0.05, min: 0.05 })}${numInput('radius_mm', el.radius_mm || 0, { label: 'Corner', step: 0.1, min: 0 })}</div>
      <label class="stk-check"><input type="checkbox" data-el="fill" data-type="bool" ${el.fill ? 'checked' : ''}> Solid fill (put white-on-black text on top)</label></section>`;
  } else if (el.kind === 'image') {
    content = `<section class="stk-sec"><h4>Logo / image</h4>
      <button class="btn btn-outline btn-sm" data-sa="replace-image">Replace image…</button>
      <label class="stk-range"><span>Ink threshold</span><input type="range" min="20" max="235" step="1" data-el="threshold" data-type="num" value="${el.threshold}"><em>${el.threshold}</em></label>
      <label class="stk-check"><input type="checkbox" data-el="invert" data-type="bool" ${el.invert ? 'checked' : ''}> Invert (negative)</label>
      <p class="stk-muted">Thermal printers burn black or nothing - turn on <b>Printer view</b> to tune the threshold.</p></section>`;
  }
  const canInvert = el.kind === 'text' || el.kind === 'barcode' || el.kind === 'qr';
  return `
    <div class="stk-el-head">
      <input class="stk-el-name" data-el="name" data-type="text" value="${esc(el.name || '')}" placeholder="${esc(elementName(Object.assign({}, el, { name: '' })))}" aria-label="Element name">
      <button class="stk-icon-btn ${el.locked ? 'is-on' : ''}" data-q="lock" title="${el.locked ? 'Unlock' : 'Lock position'}">${icon(el.locked ? 'lock' : 'unlock', 15)}</button>
      <button class="stk-icon-btn ${el.hidden ? 'is-on' : ''}" data-q="hide" title="${el.hidden ? 'Show' : 'Hide (not printed)'}">${icon(el.hidden ? 'eyeoff' : 'eye', 15)}</button>
    </div>
    ${content}
    <section class="stk-sec">
      <h4>Placement</h4>
      <div class="stk-grid2">
        ${numInput('x_mm', el.x_mm, { label: 'X' })}${numInput('y_mm', el.y_mm, { label: 'Y' })}
        ${numInput('w_mm', el.w_mm, { label: 'W', min: 0.2 })}${numInput('h_mm', el.h_mm, { label: 'H', min: 0.2 })}
      </div>
      <div class="stk-row"><span class="stk-row-label">Rotate</span>${seg('rotation', el.rotation, [{ v: 0, label: '0°' }, { v: 90, label: '90°' }, { v: 180, label: '180°' }, { v: 270, label: '270°' }])}</div>
      ${canInvert ? `<label class="stk-check"><input type="checkbox" data-el="invert" data-type="bool" ${el.invert ? 'checked' : ''}> White on black</label>` : ''}
      <div class="stk-row stk-snap-row">
        <button class="btn btn-outline btn-sm" data-place="hcenter" title="Centre horizontally on the label">Centre ↔</button>
        <button class="btn btn-outline btn-sm" data-place="vcenter" title="Centre vertically on the label">Centre ↕</button>
        <button class="btn btn-outline btn-sm" data-place="fill-w" title="Stretch to the label width minus 1 mm margins">Full width</button>
      </div>
    </section>
    <div class="stk-actions">
      <button class="btn btn-outline btn-sm" data-q="duplicate">Duplicate</button>
      <button class="btn btn-outline btn-sm stk-danger-btn" data-q="delete">Delete</button>
    </div>`;
}

function scanBadge(fit) {
  if (!fit || fit.empty) return '<p class="stk-scan stk-muted">Prints nothing for the current preview item (blank value).</p>';
  return `<p class="stk-scan ${fit.ok ? 'is-ok' : 'is-bad'}">${fit.ok ? '✓ Scannable' : '⚠ Check'} · ${esc(fit.message)} at ${S.t.settings.dpi} dpi</p>`;
}

function customTextHTML(el, placeholder = 'e.g. W: {gross_weight} gm') {
  return `
    <label class="stk-field"><span>Text</span><textarea rows="2" data-el="text" data-type="text" placeholder="${esc(placeholder)}">${esc(el.text || '')}</textarea></label>
    <div class="stk-row stk-insert-row">
      <select class="stk-insert-field" aria-label="Insert a field">${'<option value="">Insert field…</option>' + S.fields.map(f => `<option value="${esc(f.key)}">${esc(f.label)}</option>`).join('')}</select>
      <span class="stk-muted">as <code>{field}</code></span>
    </div>`;
}

function affixHTML(el) {
  const f = S.fields.find(x => x.key === el.field);
  const numeric = f && (f.type === 'Number' || /weight|price|mrp|qty|rate/.test(f.key));
  return `
    <div class="stk-grid2">
      <label class="stk-field"><span>Before</span><input type="text" data-el="prefix" data-type="text" value="${esc(el.prefix || '')}" placeholder="e.g. W:"></label>
      <label class="stk-field"><span>After</span><input type="text" data-el="suffix" data-type="text" value="${esc(el.suffix || '')}" placeholder="e.g. gm"></label>
    </div>
    <div class="stk-row">
      <label class="stk-num"><span>Decimals</span><select data-el="decimals" data-type="text">
        <option value="" ${el.decimals === undefined || el.decimals === '' ? 'selected' : ''}>As stored</option>
        ${[0, 1, 2, 3, 4].map(d => `<option value="${d}" ${String(el.decimals) === String(d) ? 'selected' : ''}>${d}</option>`).join('')}
      </select></label>
      ${numeric ? `<label class="stk-check"><input type="checkbox" data-el="group_digits" data-type="bool" ${el.group_digits ? 'checked' : ''}> 1,23,456 grouping</label>` : ''}
    </div>
    <p class="stk-muted">If the item has no value, the whole element is left off - no stray "W: gm".</p>`;
}

function fontSelect(current, scope = 'el', key = 'font_family') {
  const inherit = scope === 'el' ? `<option value="" ${!current ? 'selected' : ''}>Template default (${esc(S.t.settings.font_family)})</option>` : '';
  return `<label class="stk-field"><span>Font</span><select data-${scope}="${key}" data-type="text">${inherit}${E.FONT_FAMILIES.map(f => `<option value="${esc(f.id)}" ${current === f.id ? 'selected' : ''} style="font-family:${esc(f.css)}">${esc(f.label)}</option>`).join('')}</select></label>`;
}

function typographyHTML(el) {
  const pt = round2((el.font_size_mm / 25.4) * 72);
  return `
    <section class="stk-sec">
      <h4>Type</h4>
      ${fontSelect(el.font_family || '')}
      <div class="stk-row">
        ${numInput('font_size_mm', el.font_size_mm, { label: 'Size', step: 0.1, min: 0.5 })}
        <span class="stk-muted">≈ ${pt} pt</span>
      </div>
      <div class="stk-row">
        <div class="stk-seg" role="group" aria-label="Style">${toggle('bold', el.bold, '<b>B</b>', 'Bold')}${toggle('italic', el.italic, '<i>I</i>', 'Italic')}${toggle('transform', el.transform === 'upper', 'AA', 'UPPERCASE', 'upper')}</div>
      </div>
      <div class="stk-row">
        ${seg('align', el.align, [{ v: 'left', label: '⇤', title: 'Align left' }, { v: 'center', label: '↔', title: 'Centre' }, { v: 'right', label: '⇥', title: 'Align right' }])}
        ${seg('valign', el.valign, [{ v: 'top', label: '⤒', title: 'Top' }, { v: 'middle', label: '↕', title: 'Middle' }, { v: 'bottom', label: '⤓', title: 'Bottom' }])}
      </div>
      <div class="stk-row"><span class="stk-row-label">Long values</span>
        ${seg('fit', el.fit, [{ v: 'shrink', label: 'Shrink to fit', title: 'One size smaller until it fits the box' }, { v: 'wrap', label: 'Wrap', title: 'Break onto more lines' }, { v: 'clip', label: 'Cut off', title: 'Keep size, cut at the box edge' }])}
      </div>
    </section>`;
}

function labelTabHTML() {
  const t = S.t, s = t.settings;
  const groups = [...new Set(E.LABEL_PRESETS.map(p => p.group))];
  const presetMatch = E.LABEL_PRESETS.find(p => p.w === t.w && p.h === t.h && (p.per_row || 1) === s.per_row);
  const autoMedia = round2(s.per_row * t.w + (s.per_row - 1) * s.gap_h_mm);
  return `
    <section class="stk-sec">
      <h4>Size</h4>
      <label class="stk-field"><span>Standard size</span><select data-sa-change="preset">
        <option value="">${presetMatch ? '' : 'Custom size'}</option>
        ${groups.map(g => `<optgroup label="${esc(g)}">${E.LABEL_PRESETS.filter(p => p.group === g).map(p => `<option value="${p.id}" ${presetMatch && presetMatch.id === p.id ? 'selected' : ''}>${esc(p.label)} mm</option>`).join('')}</optgroup>`).join('')}
      </select></label>
      <div class="stk-grid2">
        ${numInput('w', t.w, { label: 'Width', min: 5, scope: 'tpl' })}
        ${numInput('h', t.h, { label: 'Height', min: 3, scope: 'tpl' })}
      </div>
      <div class="stk-row"><span class="stk-row-label">Printer DPI</span>${seg('s.dpi', s.dpi, [{ v: 203, label: '203' }, { v: 300, label: '300' }, { v: 600, label: '600' }])}</div>
      <p class="stk-muted">Used for the scan checks and Printer view. When printing, the Printer record's own DPI wins.</p>
    </section>
    <section class="stk-sec">
      <h4>Roll</h4>
      <div class="stk-grid2">
        ${numInput('per_row', s.per_row, { label: 'Across', step: 1, min: 1, max: 10, unit: 'labels', scope: 's' })}
        ${numInput('gap_h_mm', s.gap_h_mm, { label: 'Gap ↔', min: 0, scope: 's' })}
        ${numInput('gap_v_mm', s.gap_v_mm, { label: 'Gap ↕', min: 0, scope: 's' })}
        ${numInput('media_w_mm', s.media_w_mm, { label: 'Roll width', min: 0, scope: 's' })}
      </div>
      <p class="stk-muted">Roll width blank = ${autoMedia} mm (labels + gaps). Set it when the liner is wider than the labels.</p>
    </section>
    <section class="stk-sec">
      <h4>Printer tuning</h4>
      <div class="stk-grid2">
        ${numInput('offset_x_mm', s.offset_x_mm, { label: 'Shift →', scope: 's' })}
        ${numInput('offset_y_mm', s.offset_y_mm, { label: 'Shift ↓', scope: 's' })}
        ${numInput('density', s.density, { label: 'Darkness', step: 1, min: 0, max: 15, unit: '0-15', scope: 's' })}
        ${numInput('speed', s.speed, { label: 'Speed', step: 1, min: 1, max: 14, unit: 'in/s', scope: 's' })}
      </div>
      <label class="stk-check"><input type="checkbox" data-s="flip" data-type="bool" ${s.flip ? 'checked' : ''}> Rotate the print 180° (label comes out upside down)</label>
      <p class="stk-muted">Print is off-centre? Do a <b>Test print</b>, measure, and shift. Blank darkness/speed keep the printer's own settings.</p>
    </section>
    <section class="stk-sec">
      <h4>Text</h4>
      ${fontSelect(s.font_family, 's', 'font_family')}
    </section>
    <section class="stk-sec">
      <h4>Office printer / PDF</h4>
      <p class="stk-muted">Used when the printer is not a TSC (TSPL) or Zebra (ZPL) thermal printer, or the print agent isn't running.</p>
      <div class="stk-row">${seg('s.sheet.mode', s.sheet.mode, [{ v: 'roll', label: 'Roll - a page per row' }, { v: 'a4', label: 'A4 sticker sheet' }])}</div>
      ${s.sheet.mode === 'a4' ? `
        <div class="stk-grid2">
          ${numInput('sheet.margin_top_mm', s.sheet.margin_top_mm, { label: 'Top margin', min: 0, scope: 's' })}
          ${numInput('sheet.margin_left_mm', s.sheet.margin_left_mm, { label: 'Left margin', min: 0, scope: 's' })}
          ${numInput('sheet.gap_h_mm', s.sheet.gap_h_mm, { label: 'Gap ↔', min: 0, scope: 's' })}
          ${numInput('sheet.gap_v_mm', s.sheet.gap_v_mm, { label: 'Gap ↕', min: 0, scope: 's' })}
        </div>
        <p class="stk-muted">${a4Summary()}</p>` : ''}
    </section>
    <section class="stk-sec">
      <h4>Which items use it</h4>
      <label class="stk-field"><span>Item categories</span><input type="text" data-tpl="categories" data-type="text" value="${esc(t.categories || '')}" placeholder="e.g. Rings, Bridal, Earrings"></label>
      <label class="stk-check"><input type="checkbox" data-tpl="is_default" data-type="bool" ${t.is_default ? 'checked' : ''}> Default for categories with no template of their own</label>
      <div class="stk-row"><span class="stk-row-label">Status</span>${seg('tpl.status', t.status, [{ v: 'Active', label: 'Active' }, { v: 'Inactive', label: 'Inactive' }])}</div>
      <label class="stk-field"><span>Template code</span><input type="text" data-tpl="code" data-type="text" value="${esc(t.code || '')}" ${S.isNew ? 'placeholder="Made from the name if blank"' : 'readonly'}></label>
    </section>`;
}

function a4Summary() {
  const t = S.t, sh = t.settings.sheet;
  const cols = Math.max(1, Math.floor((210 - 2 * sh.margin_left_mm + sh.gap_h_mm) / (t.w + sh.gap_h_mm)));
  const rows = Math.max(1, Math.floor((297 - 2 * sh.margin_top_mm + sh.gap_v_mm) / (t.h + sh.gap_v_mm)));
  return `Fits ${cols} × ${rows} = <b>${cols * rows}</b> labels per A4 page.`;
}

function layersTabHTML() {
  if (!S.t.elements.length) return '<div class="stk-empty"><p>No elements yet.</p></div>';
  const list = [...S.t.elements].reverse();
  return `<p class="stk-muted stk-layers-hint">Top of the list prints on top.</p><ul class="stk-layers">${list.map(el => `
    <li class="${S.sel.has(el.id) ? 'is-sel' : ''} ${el.hidden ? 'is-hidden' : ''}" data-layer="${esc(el.id)}">
      <span class="stk-layer-icon">${icon(el.kind === 'text' ? (el.field === 'custom' ? 'text' : 'data') : el.kind, 15)}</span>
      <span class="stk-layer-name">${esc(elementName(el))}</span>
      <button class="stk-icon-btn" data-layer-act="up" title="Bring forward">▲</button>
      <button class="stk-icon-btn" data-layer-act="down" title="Send backward">▼</button>
      <button class="stk-icon-btn ${el.locked ? 'is-on' : ''}" data-layer-act="lock" title="Lock">${icon(el.locked ? 'lock' : 'unlock', 14)}</button>
      <button class="stk-icon-btn ${el.hidden ? 'is-on' : ''}" data-layer-act="hide" title="Hide">${icon(el.hidden ? 'eyeoff' : 'eye', 14)}</button>
    </li>`).join('')}</ul>`;
}

function setPath(obj, path, v) {
  const parts = path.split('.');
  let o = obj;
  for (let i = 0; i < parts.length - 1; i++) o = o[parts[i]];
  o[parts[parts.length - 1]] = v;
}
function getPath(obj, path) {
  return path.split('.').reduce((o, k) => (o == null ? o : o[k]), obj);
}

function readInput(inp) {
  if (inp.dataset.type === 'bool') return inp.checked;
  if (inp.dataset.type === 'num') {
    if (inp.value === '') return '';
    const n = Number(inp.value);
    return Number.isFinite(n) ? n : '';
  }
  return inp.value;
}

function bindInspector(body) {
  // Element properties apply to every selected element (multi-edit).
  body.querySelectorAll('[data-el]').forEach(inp => {
    const key = inp.dataset.el;
    const apply = live => {
      const v = readInput(inp);
      for (const el of selected()) {
        if (inp.dataset.type === 'num' && v === '' && key !== 'decimals') continue;
        el[key] = v;
        if (key === 'w_mm' || key === 'h_mm') el[key] = Math.max(0.2, Number(v) || 0.2);
      }
      if (inp.type === 'range') { const em = inp.parentElement.querySelector('em'); if (em) em.textContent = inp.value; }
      paint(); renderOverlay(); renderStatus(); drawRulers();
      if (!live) { commitChange(); if (key === 'field' || key === 'show_text' || key === 'symbology' || key === 'ecc') renderInspector(); }
    };
    inp.addEventListener('focus', beginChange);
    inp.addEventListener('pointerdown', beginChange);
    inp.addEventListener('input', () => { beginChange(); apply(true); });
    inp.addEventListener('change', () => { beginChange(); apply(false); });
  });
  // Template-level fields and settings.
  body.querySelectorAll('[data-tpl], [data-s]').forEach(inp => {
    const isS = inp.dataset.s !== undefined;
    const key = isS ? inp.dataset.s : inp.dataset.tpl;
    const apply = live => {
      let v = readInput(inp);
      if (!isS && (key === 'w' || key === 'h')) { if (v === '' || v <= 0) return; v = Math.max(key === 'w' ? 5 : 3, v); }
      if (isS && key === 'per_row') v = clamp(Math.round(Number(v) || 1), 1, 10);
      if (isS && (key === 'gap_h_mm' || key === 'gap_v_mm' || key === 'offset_x_mm' || key === 'offset_y_mm') && v === '') v = 0;
      if (isS) setPath(S.t.settings, key, v); else S.t[key] = v;
      paint(); renderOverlay(); drawRulers(); renderTopBar(); renderStatus();
      if (!live) { commitChange(); if (key === 'per_row' || key === 'gap_h_mm' || key.startsWith('sheet')) renderInspector(); }
    };
    inp.addEventListener('focus', beginChange);
    inp.addEventListener('pointerdown', beginChange);
    inp.addEventListener('input', () => { beginChange(); apply(true); });
    inp.addEventListener('change', () => { beginChange(); apply(false); });
  });
  body.querySelectorAll('[data-seg]').forEach(btn => btn.addEventListener('click', () => {
    const key = btn.dataset.seg;
    let v = btn.dataset.v;
    if (v === 'true') v = true; else if (v === 'false') v = false;
    else if (/^\d+$/.test(v) && (key === 'rotation' || key === 's.dpi')) v = Number(v);
    change(() => {
      if (key.startsWith('s.')) setPath(S.t.settings, key.slice(2), v);
      else if (key.startsWith('tpl.')) S.t[key.slice(4)] = v;
      else for (const el of selected()) {
        if (key === 'rotation' && (((el.rotation || 0) / 90) % 2) !== ((v / 90) % 2)) {
          // Swap the box so rotated text keeps its proportions.
          swapAndKeepInside(el);
        }
        el[key] = v;
      }
    });
  }));
  body.querySelectorAll('[data-toggle]').forEach(btn => btn.addEventListener('click', () => {
    const key = btn.dataset.toggle;
    const onValue = btn.dataset.on === 'true' ? true : btn.dataset.on;
    const turnOn = !btn.classList.contains('is-on');
    change(() => { for (const el of selected()) el[key] = turnOn ? onValue : (onValue === true ? false : ''); });
  }));
  const preset = body.querySelector('[data-sa-change="preset"]');
  if (preset) preset.addEventListener('change', () => applyPreset(preset.value));
  const ins = body.querySelector('.stk-insert-field');
  if (ins) ins.addEventListener('change', () => {
    const ta = body.querySelector('textarea[data-el="text"]');
    if (!ta || !ins.value) return;
    const token = `{${ins.value}}`;
    const s = ta.selectionStart ?? ta.value.length, e = ta.selectionEnd ?? ta.value.length;
    ta.value = ta.value.slice(0, s) + token + ta.value.slice(e);
    beginChange();
    ta.dispatchEvent(new Event('change'));
    ins.value = '';
    ta.focus();
    ta.setSelectionRange(s + token.length, s + token.length);
  });
  body.querySelectorAll('[data-place]').forEach(b => b.addEventListener('click', () => {
    change(() => {
      for (const el of selected()) {
        if (b.dataset.place === 'hcenter') el.x_mm = round2((S.t.w - el.w_mm) / 2);
        if (b.dataset.place === 'vcenter') el.y_mm = round2((S.t.h - el.h_mm) / 2);
        if (b.dataset.place === 'fill-w') { el.x_mm = 1; el.w_mm = round2(S.t.w - 2); }
      }
    });
  }));
  body.querySelectorAll('[data-layer]').forEach(li => li.addEventListener('click', e => {
    const id = li.dataset.layer;
    const act = e.target.closest('[data-layer-act]')?.dataset.layerAct;
    const el = S.t.elements.find(x => x.id === id);
    if (!el) return;
    if (!act) {
      if (e.shiftKey || e.ctrlKey || e.metaKey) { S.sel.has(id) ? S.sel.delete(id) : S.sel.add(id); } else S.sel = new Set([id]);
      refresh();
      return;
    }
    change(() => {
      const i = S.t.elements.indexOf(el);
      if (act === 'up' && i < S.t.elements.length - 1) S.t.elements.splice(i, 2, S.t.elements[i + 1], el);
      if (act === 'down' && i > 0) S.t.elements.splice(i - 1, 2, el, S.t.elements[i - 1]);
      if (act === 'lock') el.locked = !el.locked;
      if (act === 'hide') el.hidden = !el.hidden;
    });
  }));
}

function applyPreset(id) {
  const p = E.LABEL_PRESETS.find(x => x.id === id);
  if (!p) return;
  change(() => {
    const kx = p.w / S.t.w, ky = p.h / S.t.h;
    // Existing elements scale with the label so a layout survives a resize.
    if (S.t.elements.length && (kx !== 1 || ky !== 1)) {
      for (const el of S.t.elements) {
        el.x_mm = round2(el.x_mm * kx); el.w_mm = round2(el.w_mm * kx);
        el.y_mm = round2(el.y_mm * ky); el.h_mm = round2(el.h_mm * ky);
        if (el.font_size_mm) el.font_size_mm = round2(el.font_size_mm * Math.min(kx, ky));
      }
    }
    S.t.w = p.w; S.t.h = p.h;
    S.t.settings.per_row = p.per_row || 1;
    if (p.gap_h !== undefined) S.t.settings.gap_h_mm = p.gap_h;
    if (p.gap_v !== undefined) S.t.settings.gap_v_mm = p.gap_v;
  });
  fitZoom();
  refresh();
}

// ---------------------------------------------------------------------------
// Drawer (data fields)
// ---------------------------------------------------------------------------

function renderDrawer() {
  if (!S.root) return;
  const drawer = S.root.querySelector('.stk-drawer');
  drawer.hidden = !S.drawer;
  S.root.querySelector('[data-sa="drawer"]').classList.toggle('is-on', S.drawer);
  if (!S.drawer) return;
  const q = S.drawerQuery.trim().toLowerCase();
  const list = S.fields.filter(f => !q || f.label.toLowerCase().includes(q) || f.key.includes(q));
  const groups = [['Label', 'On every label'], ['Item', 'From the item record']];
  drawer.querySelector('.stk-drawer-list').innerHTML = groups.map(([g, title]) => {
    const items = list.filter(f => f.group === g);
    if (!items.length) return '';
    return `<h5>${title}</h5>${items.map(f => `
      <button class="stk-field-chip" draggable="true" data-field="${esc(f.key)}" title="${esc(f.key)}">
        <span class="stk-field-name">${esc(f.label)}</span>
        <span class="stk-field-sample">${esc(E.labelFieldValue(S.label, f.key) || '—')}</span>
      </button>`).join('')}`;
  }).join('') || '<p class="stk-muted">No field matches.</p>';
  drawer.querySelectorAll('[data-field]').forEach(b => {
    b.addEventListener('click', () => addElement('text', { field: b.dataset.field }));
    b.addEventListener('dragstart', e => { e.dataTransfer.setData('text/x-stk-field', b.dataset.field); e.dataTransfer.effectAllowed = 'copy'; });
  });
}

// ---------------------------------------------------------------------------
// Adding elements
// ---------------------------------------------------------------------------

function addElement(kind, opts = {}) {
  const t = S.t;
  const el = { id: E.newElementId(), kind };
  if (kind === 'text') {
    el.field = opts.field || 'custom';
    if (el.field === 'custom') el.text = opts.text || 'Text';
    el.h_mm = round2(clamp(t.h * 0.3, 2.5, 7));
    el.w_mm = round2(Math.min(t.w - 2, Math.max(20, t.w * 0.4)));
    el.font_size_mm = round2(Math.min(3.5, el.h_mm * 0.75));
    el.fit = 'shrink'; el.align = 'left'; el.valign = 'middle';
  } else if (kind === 'barcode') {
    el.field = 'barcode';
    el.w_mm = round2(Math.min(t.w - 2, 40)); el.h_mm = round2(Math.min(t.h - 1, 12));
    el.symbology = 'code128'; el.show_text = el.h_mm >= 7; el.quiet_zone = true; el.font_size_mm = 2.2;
  } else if (kind === 'qr') {
    el.field = 'barcode';
    const side = round2(Math.min(t.h - 1, t.w - 2, 20));
    el.w_mm = side; el.h_mm = side; el.ecc = 'M';
  } else if (kind === 'line') {
    el.w_mm = round2(t.w * 0.6); el.h_mm = 1; el.thickness_mm = 0.3;
  } else if (kind === 'box') {
    el.w_mm = round2(t.w * 0.5); el.h_mm = round2(t.h * 0.6); el.thickness_mm = 0.3;
  } else if (kind === 'image') {
    el.asset = opts.asset;
    const k = Math.min((t.w * 0.4) / opts.iw, (t.h * 0.8) / opts.ih);
    el.w_mm = round2(opts.iw * k); el.h_mm = round2(opts.ih * k); el.threshold = 128;
  }
  const at = opts.at || { x: t.w / 2, y: t.h / 2 };
  el.x_mm = round2(clamp(at.x - el.w_mm / 2, 0, Math.max(0, t.w - el.w_mm)));
  el.y_mm = round2(clamp(at.y - el.h_mm / 2, 0, Math.max(0, t.h - el.h_mm)));
  change(() => {
    S.t.elements.push(E.normaliseElement(el));
    S.sel = new Set([el.id]);
    S.tab = 'element';
  }, { inspector: true });
  hideStarter();
}

function onImageFile(e) {
  const file = e.target.files && e.target.files[0];
  e.target.value = '';
  if (!file) return;
  if (file.size > 400 * 1024) {
    window.showCustomAlert?.('That image is over 400 KB. Use a small logo (a few hundred pixels wide is plenty for a label).', 'Image too large');
    return;
  }
  const reader = new FileReader();
  reader.onload = () => {
    const img = new Image();
    img.onload = async () => {
      // Downscale to what a 600-dpi label could ever use, so the template
      // stays small.
      const maxPx = 800;
      const k = Math.min(1, maxPx / Math.max(img.naturalWidth, img.naturalHeight));
      const c = document.createElement('canvas');
      c.width = Math.max(1, Math.round(img.naturalWidth * k)); c.height = Math.max(1, Math.round(img.naturalHeight * k));
      const g = c.getContext('2d');
      g.fillStyle = '#fff'; g.fillRect(0, 0, c.width, c.height);
      g.drawImage(img, 0, 0, c.width, c.height);
      const url = c.toDataURL('image/png');
      const id = 'a' + Date.now().toString(36);
      S.t.assets[id] = url;
      await E.ensureTemplateAssets({ elements: [{ kind: 'image', asset: id }], assets: S.t.assets });
      const target = S.replaceImageFor && S.t.elements.find(x => x.id === S.replaceImageFor);
      S.replaceImageFor = null;
      if (target) change(() => { target.asset = id; });
      else addElement('image', { asset: id, iw: c.width, ih: c.height });
    };
    img.src = reader.result;
  };
  reader.readAsDataURL(file);
}

// ---------------------------------------------------------------------------
// Starter (new template)
// ---------------------------------------------------------------------------

function showStarter() {
  const box = S.root.querySelector('.stk-starter');
  const groups = [...new Set(E.LABEL_PRESETS.map(p => p.group))];
  box.innerHTML = `
    <div class="stk-starter-card">
      <h3>Start a new label</h3>
      <p class="stk-muted">Pick the size of the roll you print on - you can change it any time.</p>
      ${groups.map(g => `<h5>${esc(g)}</h5><div class="stk-starter-grid">${E.LABEL_PRESETS.filter(p => p.group === g).map(p => {
        const k = 56 / Math.max(p.w * (p.per_row || 1), p.h * 1.6);
        return `<button class="stk-starter-opt" data-preset="${p.id}">
          <span class="stk-starter-shape">${Array.from({ length: p.per_row || 1 }, () => `<i style="width:${Math.max(6, p.w * k)}px; height:${Math.max(4, p.h * k)}px"></i>`).join('')}</span>
          <span>${esc(p.label)}</span></button>`;
      }).join('')}</div>`).join('')}
      <div class="stk-starter-foot">
        <button class="btn btn-outline btn-sm" data-starter="default">Use the built-in retail layout (50 × 25)</button>
        <button class="btn btn-outline btn-sm" data-starter="blank">Blank 50 × 25</button>
      </div>
    </div>`;
  box.hidden = false;
  box.querySelectorAll('[data-preset]').forEach(b => b.addEventListener('click', () => {
    applyPreset(b.dataset.preset);
    S.undo = []; updateHistoryButtons();
    hideStarter();
    S.root.querySelector('.stk-name').focus();
  }));
  box.querySelector('[data-starter="default"]').addEventListener('click', () => {
    S.t.w = 50; S.t.h = 25;
    S.t.elements = E.DEFAULT_STICKER_TEMPLATE.elements.map(e => E.normaliseElement(Object.assign({}, e, { id: E.newElementId() })));
    hideStarter(); fitZoom(); refresh();
    S.root.querySelector('.stk-name').focus();
  });
  box.querySelector('[data-starter="blank"]').addEventListener('click', () => { hideStarter(); S.root.querySelector('.stk-name').focus(); });
}

function hideStarter() {
  const box = S.root && S.root.querySelector('.stk-starter');
  if (box) box.hidden = true;
}

// ---------------------------------------------------------------------------
// Pointer interaction
// ---------------------------------------------------------------------------

function bbox(els) {
  const x = Math.min(...els.map(e => e.x_mm)), y = Math.min(...els.map(e => e.y_mm));
  const r = Math.max(...els.map(e => e.x_mm + e.w_mm)), b = Math.max(...els.map(e => e.y_mm + e.h_mm));
  return { x, y, w: r - x, h: b - y };
}

function boardPoint(e) {
  const r = S.root.querySelector('.stk-board').getBoundingClientRect();
  return { x: (e.clientX - r.left) / S.zoom, y: (e.clientY - r.top) / S.zoom };
}

function onCursorMove(e) {
  const p = boardPoint(e);
  S.cursor = p;
  renderStatus();
  drawRulers();
}

// Snap candidates: the label's edges and centre plus every unselected
// element's edges and centre.
function snapTargets(excludeIds) {
  const xs = [0, S.t.w / 2, S.t.w], ys = [0, S.t.h / 2, S.t.h];
  for (const el of S.t.elements) {
    if (excludeIds.has(el.id) || el.hidden) continue;
    xs.push(el.x_mm, el.x_mm + el.w_mm / 2, el.x_mm + el.w_mm);
    ys.push(el.y_mm, el.y_mm + el.h_mm / 2, el.y_mm + el.h_mm);
  }
  return { xs, ys };
}

function nearest(values, targets, thr) {
  let best = null;
  for (const v of values) {
    for (const t of targets) {
      const d = t - v;
      if (Math.abs(d) <= thr && (!best || Math.abs(d) < Math.abs(best.d))) best = { d, at: t };
    }
  }
  return best;
}

function showGuides(gx, gy) {
  const overlay = S.root.querySelector('.stk-overlay');
  overlay.querySelectorAll('.stk-guide').forEach(g => g.remove());
  for (const x of gx) overlay.insertAdjacentHTML('beforeend', `<div class="stk-guide stk-guide-v" style="left:${x * S.zoom}px"></div>`);
  for (const y of gy) overlay.insertAdjacentHTML('beforeend', `<div class="stk-guide stk-guide-h" style="top:${y * S.zoom}px"></div>`);
}

// The canvas takes keyboard focus away from any inspector field: pointerdown
// is preventDefault-ed for dragging, which would otherwise leave the caret in
// e.g. the name box and swallow every shortcut (Del, arrows, Ctrl+Z).
function releaseFieldFocus() {
  const a = document.activeElement;
  if (a && S.root && S.root.contains(a) && isTyping(a)) a.blur();
}

function onOverlayPointerDown(e) {
  if (e.button !== 0) return;
  releaseFieldFocus();
  const handle = e.target.closest('[data-h]');
  const box = e.target.closest('.stk-el');
  if (!box) { startMarquee(e); return; }
  const id = box.dataset.id;
  e.preventDefault();
  if (e.shiftKey || e.ctrlKey || e.metaKey) {
    if (S.sel.has(id)) S.sel.delete(id); else S.sel.add(id);
    S.tab = 'element';
    refresh();
    return;
  }
  if (!S.sel.has(id)) { S.sel = new Set([id]); S.tab = 'element'; refresh(); }
  if (handle) startResize(e, S.t.elements.find(x => x.id === id), handle.dataset.h);
  else startMove(e, id);
}

function onOverlayDblClick(e) {
  const box = e.target.closest('.stk-el');
  if (!box) return;
  S.sel = new Set([box.dataset.id]);
  S.tab = 'element';
  refresh();
  const ta = S.root.querySelector('.stk-tab-body textarea[data-el="text"]') || S.root.querySelector('.stk-tab-body [data-el="prefix"]');
  if (ta) { ta.focus(); ta.select?.(); }
}

// Listens on window, not the pressed node: selecting an element re-renders the
// overlay, which replaces that node mid-gesture, and a detached node never
// receives the rest of the drag.
function trackPointer(e, onMove, onUp) {
  const id = e.pointerId;
  const move = ev => { if (ev.pointerId === id) onMove(ev); };
  const up = ev => {
    if (ev.pointerId !== id) return;
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', up);
    window.removeEventListener('pointercancel', up);
    onUp(ev);
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', up);
  window.addEventListener('pointercancel', up);
}

function startMove(e, pressedId) {
  const els = selected().filter(el => !el.locked);
  if (!els.length) return;
  const start = { x: e.clientX, y: e.clientY };
  const orig = els.map(el => ({ el, x: el.x_mm, y: el.y_mm }));
  const bb0 = bbox(els);
  const targets = snapTargets(new Set(els.map(x => x.id)));
  let moved = false;
  beginChange();
  trackPointer(e, ev => {
    let dx = (ev.clientX - start.x) / S.zoom, dy = (ev.clientY - start.y) / S.zoom;
    if (!moved && Math.hypot(ev.clientX - start.x, ev.clientY - start.y) < 3) return;
    moved = true;
    if (ev.shiftKey) { if (Math.abs(dx) > Math.abs(dy)) dy = 0; else dx = 0; }
    const gx = [], gy = [];
    if (S.snap && !ev.altKey) {
      const thr = 6 / S.zoom;
      const sx = nearest([bb0.x + dx, bb0.x + bb0.w / 2 + dx, bb0.x + bb0.w + dx], targets.xs, thr);
      const sy = nearest([bb0.y + dy, bb0.y + bb0.h / 2 + dy, bb0.y + bb0.h + dy], targets.ys, thr);
      if (sx) { dx += sx.d; gx.push(sx.at); } else dx = Math.round((bb0.x + dx) / 0.5) * 0.5 - bb0.x;
      if (sy) { dy += sy.d; gy.push(sy.at); } else dy = Math.round((bb0.y + dy) / 0.5) * 0.5 - bb0.y;
    }
    for (const o of orig) { o.el.x_mm = round2(o.x + dx); o.el.y_mm = round2(o.y + dy); }
    paint();
    const overlay = S.root.querySelector('.stk-overlay');
    for (const o of orig) {
      const node = overlay.querySelector(`[data-id="${CSS.escape(o.el.id)}"]`);
      if (node) { node.style.left = o.el.x_mm * S.zoom + 'px'; node.style.top = o.el.y_mm * S.zoom + 'px'; }
    }
    showGuides(gx, gy);
    positionQuickbar();
    S.cursor = boardPoint(ev);
    renderStatus(); drawRulers();
  }, () => {
    showGuides([], []);
    // A click (no drag) on one member of a multi-selection narrows to it.
    if (!moved && pressedId && S.sel.size > 1) S.sel = new Set([pressedId]);
    commitChange();
    refresh();
  });
}

function startResize(e, el, h) {
  const start = { x: e.clientX, y: e.clientY };
  const o = { x: el.x_mm, y: el.y_mm, w: el.w_mm, h: el.h_mm };
  const targets = snapTargets(new Set([el.id]));
  const keepRatio = el.kind === 'qr' || el.kind === 'image';
  beginChange();
  trackPointer(e, ev => {
    const dx = (ev.clientX - start.x) / S.zoom, dy = (ev.clientY - start.y) / S.zoom;
    let { x, y, w, h: hh } = o;
    const gx = [], gy = [];
    const snapV = (v, list, guides) => {
      if (!S.snap || ev.altKey) return v;
      const s = nearest([v], list, 6 / S.zoom);
      if (s) { guides.push(s.at); return s.at; }
      return Math.round(v / 0.5) * 0.5;
    };
    if (h.includes('e')) w = snapV(o.x + o.w + dx, targets.xs, gx) - x;
    if (h.includes('s')) hh = snapV(o.y + o.h + dy, targets.ys, gy) - y;
    if (h.includes('w')) { const nx = snapV(o.x + dx, targets.xs, gx); w = o.x + o.w - nx; x = nx; }
    if (h.includes('n')) { const ny = snapV(o.y + dy, targets.ys, gy); hh = o.y + o.h - ny; y = ny; }
    if (keepRatio || ev.shiftKey) {
      const r = o.w / o.h;
      if (h === 'n' || h === 's') w = hh * r; else hh = w / r;
      if (h.includes('w')) x = o.x + o.w - w;
      if (h.includes('n')) y = o.y + o.h - hh;
    }
    el.x_mm = round2(x); el.y_mm = round2(y);
    el.w_mm = round2(Math.max(0.5, w)); el.h_mm = round2(Math.max(0.5, hh));
    paint();
    const node = S.root.querySelector(`.stk-overlay [data-id="${CSS.escape(el.id)}"]`);
    if (node) {
      node.style.left = el.x_mm * S.zoom + 'px'; node.style.top = el.y_mm * S.zoom + 'px';
      node.style.width = el.w_mm * S.zoom + 'px'; node.style.height = el.h_mm * S.zoom + 'px';
      const tag = node.querySelector('.stk-size-tag');
      if (tag) tag.textContent = `${round2(el.w_mm)} × ${round2(el.h_mm)}`;
    }
    showGuides(gx, gy);
    positionQuickbar();
    renderStatus(); drawRulers();
  }, () => {
    showGuides([], []);
    commitChange();
    refresh();
  });
}

function startMarquee(e) {
  if (e.button !== 0) return;
  releaseFieldFocus();
  const add = e.shiftKey || e.ctrlKey || e.metaKey;
  const before = new Set(S.sel);
  const p0 = boardPoint(e);
  const overlay = S.root.querySelector('.stk-overlay');
  const mq = document.createElement('div');
  mq.className = 'stk-marquee';
  overlay.appendChild(mq);
  let moved = false;
  trackPointer(e, ev => {
    const p = boardPoint(ev);
    moved = true;
    const x = Math.min(p0.x, p.x), y = Math.min(p0.y, p.y), w = Math.abs(p.x - p0.x), h = Math.abs(p.y - p0.y);
    Object.assign(mq.style, { left: x * S.zoom + 'px', top: y * S.zoom + 'px', width: w * S.zoom + 'px', height: h * S.zoom + 'px' });
    const hit = S.t.elements.filter(el => !el.hidden && el.x_mm < x + w && el.x_mm + el.w_mm > x && el.y_mm < y + h && el.y_mm + el.h_mm > y).map(el => el.id);
    S.sel = new Set(add ? [...before, ...hit] : hit);
    overlay.querySelectorAll('.stk-el').forEach(n => n.classList.toggle('is-sel', S.sel.has(n.dataset.id)));
  }, () => {
    mq.remove();
    if (!moved && !add) S.sel = new Set();
    S.tab = S.tab === 'label' && !S.sel.size ? 'label' : S.sel.size ? 'element' : S.tab;
    refresh();
  });
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

function duplicateSelection(offset = 1) {
  const els = selected();
  if (!els.length) return;
  change(() => {
    const ids = [];
    for (const el of els) {
      const c = JSON.parse(JSON.stringify(el));
      c.id = E.newElementId();
      c.x_mm = round2(c.x_mm + offset); c.y_mm = round2(c.y_mm + offset);
      c.locked = false;
      S.t.elements.push(c);
      ids.push(c.id);
    }
    S.sel = new Set(ids);
  });
}

function deleteSelection() {
  if (!S.sel.size) return;
  change(() => {
    S.t.elements = S.t.elements.filter(e => !S.sel.has(e.id));
    S.sel = new Set();
  });
}

// Turning a box sideways swaps its width and height about its centre; on a
// short jewellery tag that can push it off the label, so it is pulled back
// in (and trimmed to the label if it is longer than the label is tall).
function swapAndKeepInside(el) {
  const cx = el.x_mm + el.w_mm / 2, cy = el.y_mm + el.h_mm / 2;
  [el.w_mm, el.h_mm] = [el.h_mm, el.w_mm];
  el.w_mm = round2(Math.min(el.w_mm, S.t.w));
  el.h_mm = round2(Math.min(el.h_mm, S.t.h));
  el.x_mm = round2(clamp(cx - el.w_mm / 2, 0, S.t.w - el.w_mm));
  el.y_mm = round2(clamp(cy - el.h_mm / 2, 0, S.t.h - el.h_mm));
}

function rotateSelection() {
  change(() => {
    for (const el of selected()) {
      swapAndKeepInside(el);
      el.rotation = ((el.rotation || 0) + 90) % 360;
    }
  });
}

function reorder(toFront) {
  change(() => {
    const sel = S.t.elements.filter(e => S.sel.has(e.id));
    const rest = S.t.elements.filter(e => !S.sel.has(e.id));
    S.t.elements = toFront ? rest.concat(sel) : sel.concat(rest);
  });
}

function align(mode) {
  const els = selected().filter(e => !e.locked);
  if (els.length < 2) return;
  const bb = bbox(els);
  change(() => {
    if (mode === 'dist-h' || mode === 'dist-v') {
      if (els.length < 3) return;
      const horiz = mode === 'dist-h';
      const sorted = [...els].sort((a, b) => horiz ? a.x_mm - b.x_mm : a.y_mm - b.y_mm);
      const total = sorted.reduce((s, e) => s + (horiz ? e.w_mm : e.h_mm), 0);
      const gap = ((horiz ? bb.w : bb.h) - total) / (sorted.length - 1);
      let pos = horiz ? bb.x : bb.y;
      for (const e of sorted) {
        if (horiz) e.x_mm = round2(pos); else e.y_mm = round2(pos);
        pos += (horiz ? e.w_mm : e.h_mm) + gap;
      }
      return;
    }
    for (const e of els) {
      if (mode === 'left') e.x_mm = bb.x;
      if (mode === 'right') e.x_mm = round2(bb.x + bb.w - e.w_mm);
      if (mode === 'hcenter') e.x_mm = round2(bb.x + bb.w / 2 - e.w_mm / 2);
      if (mode === 'top') e.y_mm = bb.y;
      if (mode === 'bottom') e.y_mm = round2(bb.y + bb.h - e.h_mm);
      if (mode === 'vcenter') e.y_mm = round2(bb.y + bb.h / 2 - e.h_mm / 2);
    }
  });
}

function nudge(dx, dy) {
  const els = selected().filter(e => !e.locked);
  if (!els.length) return;
  beginChange();
  for (const e of els) { e.x_mm = round2(e.x_mm + dx); e.y_mm = round2(e.y_mm + dy); }
  clearTimeout(S.nudgeTimer);
  S.nudgeTimer = setTimeout(commitChange, 400);
  setDirty(true);
  refresh();
}

function onQuickbar(e) {
  const b = e.target.closest('[data-q]');
  if (!b) return;
  runQuick(b.dataset.q);
}

function runQuick(q) {
  if (q === 'rotate') rotateSelection();
  if (q === 'duplicate') duplicateSelection();
  if (q === 'front') reorder(true);
  if (q === 'back') reorder(false);
  if (q === 'delete') deleteSelection();
  if (q === 'lock') { const lock = !selected().every(x => x.locked); change(() => selected().forEach(x => { x.locked = lock; })); }
  if (q === 'hide') { const hide = !selected().every(x => x.hidden); change(() => selected().forEach(x => { x.hidden = hide; })); }
}

function setZoom(z) {
  S.autoFit = false;
  S.zoom = clamp(z, ZOOM_MIN, ZOOM_MAX);
  refresh({ inspector: false });
}

function fitZoom() {
  if (!S.root) return;
  const scroll = S.root.querySelector('.stk-scroll');
  const s = S.t.settings;
  const mediaW = Math.max(Number(s.media_w_mm) || 0, s.per_row * S.t.w + (s.per_row - 1) * s.gap_h_mm);
  const availW = Math.max(200, scroll.clientWidth - 120);
  const availH = Math.max(120, scroll.clientHeight - 160);
  S.zoom = clamp(Math.min(availW / mediaW, availH / S.t.h), ZOOM_MIN, ZOOM_MAX);
}

function onRootClick(e) {
  const btn = e.target.closest('[data-sa], [data-add], [data-align], [data-q]');
  if (!btn || !S.root.contains(btn)) return;
  if (btn.closest('.stk-quickbar')) return; // handled by onQuickbar
  if (btn.dataset.q) { runQuick(btn.dataset.q); return; }
  if (btn.dataset.align) { align(btn.dataset.align); return; }
  if (btn.dataset.add) {
    if (btn.dataset.add === 'image') { S.replaceImageFor = null; S.root.querySelector('.stk-file').click(); return; }
    addElement(btn.dataset.add);
    return;
  }
  switch (btn.dataset.sa) {
    case 'close': closeStudio(); break;
    case 'undo': undo(); break;
    case 'redo': redo(); break;
    case 'save': save(); break;
    case 'test-print': testPrint(btn); break;
    case 'size': S.tab = 'label'; S.sel = new Set(); refresh(); break;
    case 'drawer': S.drawer = !S.drawer; renderDrawer(); if (S.drawer) S.root.querySelector('.stk-drawer-search').focus(); break;
    case 'zoom-in': setZoom(S.zoom * 1.25); break;
    case 'zoom-out': setZoom(S.zoom / 1.25); break;
    case 'zoom-fit': S.autoFit = true; fitZoom(); refresh({ inspector: false }); break;
    case 'zoom-actual': setZoom(CSS_PX_PER_MM); break;
    case 'grid': S.grid = !S.grid; refresh({ inspector: false }); break;
    case 'snap': S.snap = !S.snap; renderDock(); break;
    case 'mono': S.mono = !S.mono; refresh({ inspector: false }); break;
    case 'preview-clear': clearPreviewItem(); break;
    case 'shortcuts': showShortcuts(); break;
    case 'replace-image': { const el = primary(); if (el) { S.replaceImageFor = el.id; S.root.querySelector('.stk-file').click(); } break; }
    default: break;
  }
}

function isTyping(target) {
  if (!target) return false;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
}

function onKey(e) {
  if (!S.root) return;
  // An app dialog (confirm/alert, or a generic modal) open over the studio owns
  // the keyboard. The app keeps closed modals in the DOM, so test the open state.
  if (document.querySelector('#custom-dialog-container:not(.hidden), .modal-overlay.open')) return;
  const mod = e.ctrlKey || e.metaKey;
  const k = e.key;
  if (mod && k.toLowerCase() === 's') { e.preventDefault(); save(); return; }
  if (isTyping(e.target)) {
    if (k === 'Escape') e.target.blur();
    return;
  }
  if (mod && k.toLowerCase() === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo(); return; }
  if (mod && k.toLowerCase() === 'y') { e.preventDefault(); redo(); return; }
  if (mod && k.toLowerCase() === 'a') { e.preventDefault(); S.sel = new Set(S.t.elements.map(x => x.id)); S.tab = 'element'; refresh(); return; }
  if (mod && k.toLowerCase() === 'c') { if (S.sel.size) { S.clipboard = JSON.stringify(selected()); e.preventDefault(); } return; }
  if (mod && k.toLowerCase() === 'v') {
    if (!S.clipboard) return;
    e.preventDefault();
    const items = JSON.parse(S.clipboard);
    change(() => {
      const ids = [];
      for (const c of items) { c.id = E.newElementId(); c.x_mm = round2(c.x_mm + 1); c.y_mm = round2(c.y_mm + 1); S.t.elements.push(c); ids.push(c.id); }
      S.sel = new Set(ids);
      S.clipboard = JSON.stringify(items);
    });
    return;
  }
  if (mod && k.toLowerCase() === 'd') { e.preventDefault(); duplicateSelection(); return; }
  if (k === 'Delete' || k === 'Backspace') { if (S.sel.size) { e.preventDefault(); deleteSelection(); } return; }
  if (k === 'Escape') {
    e.preventDefault();
    if (S.sel.size) { S.sel = new Set(); refresh(); } else closeStudio();
    return;
  }
  const step = e.shiftKey ? 1 : 0.1;
  if (k === 'ArrowLeft') { e.preventDefault(); nudge(-step, 0); return; }
  if (k === 'ArrowRight') { e.preventDefault(); nudge(step, 0); return; }
  if (k === 'ArrowUp') { e.preventDefault(); nudge(0, -step); return; }
  if (k === 'ArrowDown') { e.preventDefault(); nudge(0, step); return; }
  if (k === '+' || k === '=') { setZoom(S.zoom * 1.25); return; }
  if (k === '-') { setZoom(S.zoom / 1.25); return; }
  if (k === '0') { S.autoFit = true; fitZoom(); refresh({ inspector: false }); return; }
  if (k.toLowerCase() === 'r' && S.sel.size) { rotateSelection(); return; }
  if (k.toLowerCase() === 'g') { S.grid = !S.grid; refresh({ inspector: false }); return; }
  if (k.toLowerCase() === 'p') { S.mono = !S.mono; refresh({ inspector: false }); }
}

function showShortcuts() {
  const rows = [
    ['Click / Shift-click', 'Select / add to selection'], ['Drag on empty space', 'Box-select'],
    ['Arrow keys', 'Nudge 0.1 mm (Shift: 1 mm)'], ['Alt while dragging', 'Move without snapping'],
    ['Shift while dragging', 'Lock to one axis / keep proportions'], ['R', 'Rotate 90°'],
    ['Ctrl+D / Ctrl+C / Ctrl+V', 'Duplicate / copy / paste'], ['Del', 'Delete'],
    ['Ctrl+Z / Ctrl+Shift+Z', 'Undo / redo'], ['+ / − / 0', 'Zoom in / out / fit (Ctrl+wheel too)'],
    ['G / P', 'Grid / Printer view'], ['Ctrl+S', 'Save'], ['Esc', 'Deselect, then leave']
  ];
  window.showCustomAlert?.(rows.map(r => `${r[0]} — ${r[1]}`).join('\n'), 'Keyboard shortcuts');
}

// ---------------------------------------------------------------------------
// Preview with a real item
// ---------------------------------------------------------------------------

function attachPreviewPicker(input) {
  if (typeof window.attachLinkTypeahead === 'function') {
    window.attachLinkTypeahead(input, 'Item', { onPick: doc => usePreviewItem(doc && (doc.code || doc.id)), noSetupHint: true });
  }
  input.addEventListener('keydown', async e => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    const q = input.value.trim();
    if (!q) return;
    const res = await window.apiFetch(`/api/v1/doc/Item?q=${encodeURIComponent(q)}&limit=1`);
    if (res && res.ok) {
      const rows = await res.json();
      if (rows && rows[0]) usePreviewItem(rows[0].code || rows[0].id);
    }
  });
}

async function usePreviewItem(code) {
  if (!code) return;
  const res = await window.apiFetch(`/api/v1/doc/Item/${encodeURIComponent(code)}`);
  if (!res || !res.ok) return;
  const item = await res.json();
  const fields = {};
  for (const [k, v] of Object.entries(item)) {
    if (v === null || v === undefined || typeof v === 'object') continue;
    const s = String(v);
    if (s && s.length <= 500) fields[k] = s;
  }
  S.label = {
    sku: item.code || code, name: item.name || '', barcode: item.barcode || item.code || code,
    hsn_code: item.hsn_code || '', category: item.category || '', batch_no: 'LOT-24A',
    expiry_date: '', mfg_date: '', qty: 1, source_doc_id: 'GRN-0001', fields
  };
  S.previewName = item.name || code;
  const input = S.root.querySelector('.stk-preview-input');
  input.value = S.previewName;
  input.classList.add('is-live');
  S.root.querySelector('.stk-preview-clear').hidden = false;
  refresh();
  renderDrawer();
}

function clearPreviewItem() {
  S.label = Object.assign({}, E.SAMPLE_LABEL, { fields: Object.fromEntries(S.fields.filter(f => f.group === 'Item').map(f => [f.key, sampleFor(f)])) });
  S.previewName = '';
  const input = S.root.querySelector('.stk-preview-input');
  input.value = '';
  input.classList.remove('is-live');
  S.root.querySelector('.stk-preview-clear').hidden = true;
  refresh();
  renderDrawer();
}

// ---------------------------------------------------------------------------
// Save / test print
// ---------------------------------------------------------------------------

function slugCode(name) {
  return String(name || '').toUpperCase().replace(/[^A-Z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 30) || 'STICKER';
}

async function save() {
  commitChange();
  const t = S.t;
  t.name = (t.name || '').trim();
  if (!t.name) {
    const n = S.root.querySelector('.stk-name');
    n.focus();
    n.classList.add('is-invalid');
    setTimeout(() => n.classList.remove('is-invalid'), 1600);
    window.showToast?.('Give the template a name first.', { variant: 'error' });
    return;
  }
  if (S.isNew) {
    // Codes are record ids; never let a new template land on an existing one.
    const base = (t.code || '').trim() ? slugCode(t.code) : slugCode(t.name);
    const res = await window.apiFetch('/api/v1/doc/StickerTemplate');
    const existing = new Set(res && res.ok ? (await res.json()).map(x => String(x.code || x.id).toUpperCase()) : []);
    let code = base, n = 2;
    while (existing.has(code)) code = `${base}-${n++}`;
    t.code = code;
  }
  const body = E.templateToDoc(t);
  if (body.elements.length > ELEMENTS_CHAR_BUDGET) {
    window.showCustomAlert?.(`This layout needs ${body.elements.length.toLocaleString()} characters but the server stores at most ${ELEMENTS_CHAR_BUDGET.toLocaleString()} per template. Remove a few elements, or shorten custom texts.`, 'Layout too large');
    return;
  }
  const url = S.isNew ? '/api/v1/doc/StickerTemplate' : `/api/v1/doc/StickerTemplate/${encodeURIComponent(t.id)}`;
  const saveBtn = S.root.querySelector('[data-sa="save"]');
  saveBtn.disabled = true;
  const res = await window.apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  saveBtn.disabled = false;
  if (!res) return;
  if (!res.ok) {
    await window.showApiError?.(res, 'Could not save the template.', 'Save failed');
    return;
  }
  const out = await res.json().catch(() => ({}));
  if (S.isNew) { t.id = out.id || t.code; S.isNew = false; }
  setDirty(false);
  renderInspector();
  window.showToast?.(`Saved "${t.name}".`, { variant: 'success' });
}

async function testPrint(anchor) {
  document.querySelector('.stk-pop')?.remove();
  const res = await window.apiFetch('/api/v1/doc/Printer');
  const printers = res && res.ok ? await res.json() : [];
  const pop = document.createElement('div');
  pop.className = 'stk-pop';
  pop.innerHTML = `
    <h4>Test print</h4>
    <p class="stk-muted">One ${S.t.settings.per_row > 1 ? 'row' : 'label'} of this design${S.previewName ? ` with <b>${esc(S.previewName)}</b>` : ' with sample data'} - nothing is logged.</p>
    <label class="stk-field"><span>Printer</span><select class="stk-pop-printer">
      <option value="">Browser print dialog</option>
      ${printers.map(p => `<option value="${esc(p.code || p.id)}">${esc(p.name || p.code)}${p.printer_language ? ` · ${esc(p.printer_language)}` : ''}</option>`).join('')}
    </select></label>
    <div class="stk-actions"><button class="btn btn-outline btn-sm" data-pop="cancel">Cancel</button><button class="btn btn-primary btn-sm" data-pop="go">Print</button></div>`;
  S.root.appendChild(pop);
  const r = anchor.getBoundingClientRect();
  pop.style.top = r.bottom + 8 + 'px';
  pop.style.right = Math.max(8, window.innerWidth - r.right) + 'px';
  const remembered = (() => { try { return localStorage.getItem('stk.testPrinter') || ''; } catch (e) { return ''; } })();
  const sel = pop.querySelector('.stk-pop-printer');
  if (remembered && printers.some(p => (p.code || p.id) === remembered)) sel.value = remembered;
  pop.addEventListener('click', async e => {
    const act = e.target.closest('[data-pop]')?.dataset.pop;
    if (!act) return;
    if (act === 'cancel') { pop.remove(); return; }
    const code = sel.value;
    try { localStorage.setItem('stk.testPrinter', code); } catch (err) { /* private mode */ }
    const printer = printers.find(p => (p.code || p.id) === code) || null;
    pop.remove();
    await E.printTestLabel(S.t, S.label, printer);
  });
}

// ---------------------------------------------------------------------------
// Template gallery (Sticker Printing > Templates)
// ---------------------------------------------------------------------------

export async function renderStickerTemplateGallery(container, { onChange } = {}) {
  const res = await window.apiFetch('/api/v1/doc/StickerTemplate');
  if (!res) return;
  if (!res.ok) { window.renderErrorPanel?.(container, 'Failed to load sticker templates.', () => window.renderView('stickers')); return; }
  const docs = await res.json();
  const templates = docs.map(d => Object.assign(E.templateFromDoc(d), { id: d.id }));
  const panel = document.createElement('div');
  panel.className = 'table-panel stk-gallery';
  panel.innerHTML = `
    <div class="stk-gallery-head">
      <div>
        <h2>Sticker templates</h2>
        <p class="page-subtitle">Each template is one label design. Items pick theirs by category; the default covers the rest.</p>
      </div>
      <div class="stk-gallery-tools">
        ${templates.length > 6 ? '<input type="search" class="form-input stk-gallery-search" placeholder="Filter templates…" aria-label="Filter templates">' : ''}
        <button class="btn btn-primary" data-g="new">New template</button>
      </div>
    </div>
    ${templates.length === 0 ? `
      <div class="stk-gallery-empty">
        <div class="stk-gallery-empty-art">${icon('barcode', 40)}</div>
        <h3>No templates yet</h3>
        <p>Until you make one, every sticker prints the built-in layout: name, barcode, SKU and HSN on 50 × 25 mm.</p>
        <button class="btn btn-primary" data-g="new">Design your first label</button>
      </div>` : `
      <div class="stk-cards">
        ${templates.map((t, i) => `
          <article class="stk-card ${t.status !== 'Active' ? 'is-inactive' : ''}" data-i="${i}" data-name="${esc((t.name + ' ' + t.code + ' ' + t.categories).toLowerCase())}">
            <button class="stk-card-thumb" data-g="open" data-i="${i}" title="Open in the studio"><canvas></canvas></button>
            <div class="stk-card-body">
              <div class="stk-card-title">
                <strong>${esc(t.name || t.code)}</strong>
                ${t.is_default ? '<span class="badge badge-secondary">Default</span>' : ''}
                ${t.status !== 'Active' ? '<span class="badge">Inactive</span>' : ''}
              </div>
              <div class="stk-card-meta">${round2(t.w)} × ${round2(t.h)} mm · ${t.settings.dpi} dpi${t.settings.per_row > 1 ? ` · ${t.settings.per_row}-up` : ''} · ${t.elements.length} element${t.elements.length === 1 ? '' : 's'}</div>
              <div class="stk-card-cats">${(t.categories || '').split(',').map(c => c.trim()).filter(Boolean).map(c => `<span class="stk-cat">${esc(c)}</span>`).join('') || '<span class="stk-muted">No categories</span>'}</div>
              <div class="stk-card-actions">
                <button class="btn btn-outline btn-sm" data-g="open" data-i="${i}">Edit</button>
                <button class="btn btn-outline btn-sm" data-g="dup" data-i="${i}">Duplicate</button>
              </div>
            </div>
          </article>`).join('')}
      </div>`}`;
  container.appendChild(panel);

  // Thumbnails show plausible values for any Item field a template prints.
  const label = Object.assign({}, E.SAMPLE_LABEL, {
    fields: new Proxy({}, { get: (o, k) => (typeof k === 'string' ? sampleFor({ key: k, label: k.replace(/_/g, ' '), type: '' }) : undefined) })
  });
  for (let i = 0; i < templates.length; i++) {
    const t = templates[i];
    await E.ensureTemplateAssets(t);
    const canvas = panel.querySelector(`.stk-card[data-i="${i}"] canvas`);
    const scale = Math.min(260 / t.w, 120 / t.h);
    E.paintPreview(canvas, t, label, { scale });
  }
  const reopen = () => (onChange ? onChange() : window.renderView('stickers'));
  panel.addEventListener('click', e => {
    const b = e.target.closest('[data-g]');
    if (!b) return;
    const t = b.dataset.i !== undefined ? templates[Number(b.dataset.i)] : null;
    if (b.dataset.g === 'new') openStickerStudio({ onClose: reopen });
    if (b.dataset.g === 'open' && t) openStickerStudio({ templateId: t.id, onClose: reopen });
    if (b.dataset.g === 'dup' && t) openStickerStudio({ copyFrom: t.id, onClose: reopen });
  });
  const search = panel.querySelector('.stk-gallery-search');
  if (search) search.addEventListener('input', () => {
    const q = search.value.trim().toLowerCase();
    panel.querySelectorAll('.stk-card').forEach(c => { c.hidden = q && !c.dataset.name.includes(q); });
  });
}
