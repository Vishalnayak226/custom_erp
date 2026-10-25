// Stage 58: the sticker rendering engine - the ONE place a sticker template is
// turned into ink, shared by the studio canvas, the template thumbnails, the
// browser print sheet and the thermal (TSPL/ZPL) payload.
//
// "Exact look" is the design goal: what the studio shows is what the printer
// prints. Every element (text in any installed font, rotation, logos, white-on-
// black, lines, boxes) is drawn on a <canvas> at the printer's own DPI, turned
// into a 1-bit bitmap, and sent as a single TSPL BITMAP / ZPL ^GFA graphic. The
// printer never substitutes its own font, so there is nothing to drift.
//
// Barcodes and QR codes are encoded here (Code 128 auto-subset, EAN-13, QR
// byte mode, all hand-written - no library, per the repo's no-new-dependency
// rule) and drawn with every module snapped to a whole number of printer dots,
// which is exactly what the printer's own barcode firmware does; that is what
// keeps them scannable at 203 and 300 dpi.
//
// Template shape (stored on the StickerTemplate document):
//   label_width_mm / label_height_mm  - one label's size
//   elements  - JSON array of elements (see normaliseElement)
//   design    - JSON object { v: 2, settings: {...}, assets: { id: dataURL } }
// Stage-52 templates have no `design` and elements with no `kind`; both are
// read as before (normaliseElement maps them), so old templates keep working.

const MM_PER_INCH = 25.4;

// The studio and print-sheet styles (.stk-*) live in sticker-studio.css and
// are linked when this module first loads, not shipped in styles.css: they are
// only ever needed after a sticker screen has imported this module, and the
// cold-core stylesheet every screen downloads has a release budget. Resolves
// once the sheet has loaded (or failed - printing still works unstyled-ish
// rather than hanging).
export const stickerStylesReady = (() => {
  const id = 'sticker-studio-css';
  if (document.getElementById(id)) return Promise.resolve();
  return new Promise(resolve => {
    const link = document.createElement('link');
    link.id = id;
    link.rel = 'stylesheet';
    link.href = '/sticker-studio.css' + new URL(import.meta.url).search;
    link.onload = () => resolve();
    link.onerror = () => resolve();
    document.head.appendChild(link);
  });
})();

// ---------------------------------------------------------------------------
// Presets, fonts, defaults
// ---------------------------------------------------------------------------

export const FONT_FAMILIES = [
  { id: 'Arial', label: 'Arial', css: 'Arial, Helvetica, sans-serif' },
  { id: 'Arial Narrow', label: 'Arial Narrow (condensed)', css: '"Arial Narrow", "Roboto Condensed", "Liberation Sans Narrow", Arial, sans-serif' },
  { id: 'Segoe UI', label: 'Segoe UI', css: '"Segoe UI", Roboto, Arial, sans-serif' },
  { id: 'Verdana', label: 'Verdana (wide, very legible)', css: 'Verdana, Geneva, sans-serif' },
  { id: 'Tahoma', label: 'Tahoma', css: 'Tahoma, Verdana, sans-serif' },
  { id: 'Trebuchet MS', label: 'Trebuchet MS', css: '"Trebuchet MS", Arial, sans-serif' },
  { id: 'Georgia', label: 'Georgia (serif)', css: 'Georgia, "Times New Roman", serif' },
  { id: 'Times New Roman', label: 'Times New Roman', css: '"Times New Roman", Times, serif' },
  { id: 'Courier New', label: 'Courier New (mono)', css: '"Courier New", Courier, monospace' },
  { id: 'Consolas', label: 'Consolas (mono)', css: 'Consolas, "Cascadia Mono", "Courier New", monospace' }
];

// Stage 106.2 / 112.1: the client's two standard jewellery designs, read off
// the TSPL files of its old billing software (300 dpi, every element turned
// 180°; positions here are as the tag reads). Square: two labels across a
// 54.5 mm roll. Ring: one long tail tag. Each shows as a Built-in card on
// Sticker templates; Duplicate saves a template of your own that the
// template rules can then pick.
const SQUARE_ELEMENTS = [
  { kind: 'barcode', field: 'barcode', x_mm: 2.8, y_mm: 1.7, w_mm: 18.8, h_mm: 5.9, symbology: 'code128', show_text: true, quiet_zone: false, font_size_mm: 1.9 },
  { kind: 'text', field: 'name', x_mm: 2.8, y_mm: 7.8, w_mm: 18.8, h_mm: 2.3, font_size_mm: 2.1, bold: true },
  { kind: 'text', field: 'color', x_mm: 2.8, y_mm: 10.1, w_mm: 11.6, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'parent_product_code', x_mm: 2.8, y_mm: 12.4, w_mm: 11.6, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'polish', x_mm: 2.8, y_mm: 14.7, w_mm: 11.6, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'gross_weight', prefix: 'W: ', x_mm: 14.7, y_mm: 9.9, w_mm: 6.9, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'sku', x_mm: 14.7, y_mm: 12.4, w_mm: 6.9, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'size', prefix: 'S: ', x_mm: 14.7, y_mm: 14.7, w_mm: 6.9, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'mrp', prefix: 'MRP: ', x_mm: 2.8, y_mm: 17.0, w_mm: 18.8, h_mm: 2.6, font_size_mm: 2.5, bold: true },
  { kind: 'text', field: 'custom', text: 'Brand name', x_mm: 22.1, y_mm: 1.5, w_mm: 2.9, h_mm: 17.6, font_size_mm: 2.6, bold: true, rotation: 270, align: 'center' }
];
const RING_ELEMENTS = [
  { kind: 'text', field: 'custom', text: 'Brand name', x_mm: 4.6, y_mm: 0.4, w_mm: 2.9, h_mm: 11.1, font_size_mm: 2.6, bold: true, rotation: 270, align: 'center' },
  { kind: 'barcode', field: 'barcode', x_mm: 9.6, y_mm: 0.8, w_mm: 25.6, h_mm: 6.1, symbology: 'code128', show_text: true, quiet_zone: false, font_size_mm: 1.9 },
  { kind: 'text', field: 'name', x_mm: 9.6, y_mm: 8.9, w_mm: 25.6, h_mm: 2.4, font_size_mm: 2.1, bold: true },
  { kind: 'text', field: 'color', x_mm: 36.5, y_mm: 1.0, w_mm: 12.4, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'parent_product_code', x_mm: 36.5, y_mm: 3.7, w_mm: 12.4, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'size', prefix: 'S: ', x_mm: 36.5, y_mm: 6.3, w_mm: 12.4, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'mrp', prefix: 'MRP: ', x_mm: 36.5, y_mm: 8.8, w_mm: 24.8, h_mm: 2.6, font_size_mm: 2.5, bold: true },
  { kind: 'text', field: 'gross_weight', prefix: 'W: ', x_mm: 49.3, y_mm: 1.0, w_mm: 12.0, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'sku', x_mm: 49.3, y_mm: 3.9, w_mm: 12.0, h_mm: 2.3, font_size_mm: 2.1 },
  { kind: 'text', field: 'polish', x_mm: 49.3, y_mm: 6.5, w_mm: 12.0, h_mm: 2.3, font_size_mm: 2.1 }
];
// Stage 113.3: the Seznik 100 × 14.3 jewellery tag roll (Amazon B0FK2ZF5F8)
// is the Ring tag's three parts across - barcode panel, text panel, tail -
// on a taller tag, so its design is the Ring's stretched to fit. A sideways
// text's font follows the width it stands in, the rest follow the height.
const scaleElements = (els, sx, sy) => els.map(e => {
  const r = v => Math.round(v * 10) / 10;
  return Object.assign({}, e, {
    x_mm: r(e.x_mm * sx), y_mm: r(e.y_mm * sy), w_mm: r(e.w_mm * sx), h_mm: r(e.h_mm * sy),
    font_size_mm: r(e.font_size_mm * (e.rotation === 90 || e.rotation === 270 ? sx : sy))
  });
});
const SEZNIK_ELEMENTS = scaleElements(RING_ELEMENTS, 100 / 97.6, 14.3 / 11.9);

// Stage 119.1: the Seznik tag four ways, named by how the tag lies on the
// roll. Vertical prints the same design turned a quarter (direction), so
// three standing tags sit side by side; three lying tags stack down the roll
// (per_col). Horizontal × 1 keeps the code the first Seznik design shipped
// with, so a copy a client already saved still counts as this one.
export const READY_DESIGNS = [
  { id: 'jd-square-2up', code: 'SQUARE-2UP', label: 'Square 2-up (54.5 mm roll)', w: 26.25, h: 20.1, per_row: 2, gap_h: 2, gap_v: 3, elements: SQUARE_ELEMENTS },
  { id: 'jd-ring-tag', code: 'RING-TAG', label: 'Ring tag 97.6 × 11.9', w: 97.6, h: 11.9, per_row: 1, gap_v: 3, elements: RING_ELEMENTS },
  { id: 'jd-seznik-h1', code: 'SEZNIK-TAG-100X14-3', label: 'Seznik horizontal × 1', w: 100, h: 14.3, per_row: 1, gap_v: 3, elements: SEZNIK_ELEMENTS },
  { id: 'jd-seznik-h3', code: 'SEZNIK-H3', label: 'Seznik horizontal × 3', w: 100, h: 14.3, per_row: 1, per_col: 3, gap_v: 3, elements: SEZNIK_ELEMENTS },
  { id: 'jd-seznik-v1', code: 'SEZNIK-V1', label: 'Seznik vertical × 1', w: 100, h: 14.3, direction: 'vertical', per_row: 1, gap_v: 3, elements: SEZNIK_ELEMENTS },
  { id: 'jd-seznik-v3', code: 'SEZNIK-V3', label: 'Seznik vertical × 3', w: 100, h: 14.3, direction: 'vertical', per_row: 3, gap_h: 2, gap_v: 3, elements: SEZNIK_ELEMENTS }
].map(d => Object.assign({ group: 'Ready designs', dpi: 300 }, d));

// Common Indian retail / jewellery roll sizes. per_row > 1 is a multi-up roll
// (several labels side by side across the liner).
export const LABEL_PRESETS = [
  { id: 'jw-92x12', group: 'Jewellery tags', label: 'Tail tag 92 × 12', w: 92, h: 12, per_row: 1, gap_v: 3 },
  { id: 'jw-95x12', group: 'Jewellery tags', label: 'Tail tag 95 × 12', w: 95, h: 12, per_row: 1, gap_v: 3 },
  { id: 'jw-100x15', group: 'Jewellery tags', label: 'Tail tag 100 × 15', w: 100, h: 15, per_row: 1, gap_v: 3 },
  { id: 'jw-100x14.3', group: 'Jewellery tags', label: 'Tail tag 100 × 14.3', w: 100, h: 14.3, per_row: 1, gap_v: 3 },
  { id: 'jw-70x10', group: 'Jewellery tags', label: 'Small tag 70 × 10', w: 70, h: 10, per_row: 1, gap_v: 3 },
  { id: 'rt-38x25', group: 'Retail', label: '38 × 25', w: 38, h: 25, per_row: 1, gap_v: 3 },
  { id: 'rt-50x25', group: 'Retail', label: '50 × 25', w: 50, h: 25, per_row: 1, gap_v: 3 },
  { id: 'rt-50x38', group: 'Retail', label: '50 × 38', w: 50, h: 38, per_row: 1, gap_v: 3 },
  { id: 'rt-75x50', group: 'Retail', label: '75 × 50', w: 75, h: 50, per_row: 1, gap_v: 3 },
  { id: 'up-2x38x25', group: 'Multi-up rolls', label: '2-up 38 × 25', w: 38, h: 25, per_row: 2, gap_h: 2, gap_v: 3 },
  { id: 'up-3x32x25', group: 'Multi-up rolls', label: '3-up 32 × 25', w: 32, h: 25, per_row: 3, gap_h: 2, gap_v: 3 },
  { id: 'sh-100x50', group: 'Shipping', label: '100 × 50', w: 100, h: 50, per_row: 1, gap_v: 3 },
  { id: 'sh-100x150', group: 'Shipping', label: '4 × 6 in (100 × 150)', w: 100, h: 150, per_row: 1, gap_v: 3 },
  ...READY_DESIGNS
];

export const DEFAULT_SETTINGS = {
  dpi: 203,
  per_row: 1,
  per_col: 1,          // labels stacked down one print (Stage 119.3)
  direction: 'horizontal', // 'vertical' prints the design turned a quarter clockwise (Stage 119.2)
  gap_h_mm: 2,
  gap_v_mm: 3,
  media_w_mm: '',      // blank = per_row * width + gaps
  offset_x_mm: 0,      // printer calibration: shifts everything right/down
  offset_y_mm: 0,
  density: '',         // blank = printer's own setting; 0-15
  speed: '',           // blank = printer's own setting; inches/second
  flip: false,         // rotate the printed output 180 degrees
  font_family: 'Arial',
  sheet: { mode: 'roll', margin_top_mm: 10, margin_left_mm: 8, gap_h_mm: 2, gap_v_mm: 2 }
};

// The label printed for an item whose category resolves no template - same
// three facts the pre-Stage-58 fixed layout showed (name, barcode, SKU/HSN).
export const DEFAULT_STICKER_TEMPLATE = {
  id: '', code: '', name: 'Built-in default', w: 50, h: 25,
  settings: DEFAULT_SETTINGS, assets: {},
  elements: [
    { id: 'd1', kind: 'text', field: 'name', x_mm: 2, y_mm: 0.6, w_mm: 46, h_mm: 3.2, font_size_mm: 2.6, bold: true, fit: 'shrink', align: 'center', valign: 'middle' },
    // Stage 70: the variant's values ("Silver / 7") under the name; left off
    // for an item with no variant.
    { id: 'd6', kind: 'text', field: 'variant_option_values', x_mm: 2, y_mm: 3.8, w_mm: 46, h_mm: 2.4, font_size_mm: 2.0, fit: 'shrink', align: 'center', valign: 'middle' },
    { id: 'd2', kind: 'barcode', field: 'barcode', x_mm: 2, y_mm: 6.3, w_mm: 46, h_mm: 7.6, symbology: 'code128', show_text: true, quiet_zone: true, font_size_mm: 2.0 },
    { id: 'd3', kind: 'text', field: 'custom', text: 'SKU: {sku}', x_mm: 2, y_mm: 14.0, w_mm: 27, h_mm: 2.8, font_size_mm: 2.3, fit: 'shrink', valign: 'middle' },
    { id: 'd4', kind: 'text', field: 'hsn_code', prefix: 'HSN: ', x_mm: 29, y_mm: 14.0, w_mm: 19, h_mm: 2.8, font_size_mm: 2.3, fit: 'shrink', align: 'right', valign: 'middle' },
    // Stage 61: the GRN batch as its own scannable barcode, number printed
    // under it - the receipt date reads off the tag, and a scan in Batch
    // Lookup & Ageing finds this exact batch. Left off by the blank-value rule
    // when a label has no batch (a label printed without a document).
    { id: 'd5', kind: 'barcode', field: 'batch_no', x_mm: 1, y_mm: 17.0, w_mm: 48, h_mm: 7.6, symbology: 'code128', show_text: true, quiet_zone: true, font_size_mm: 2.0 }
  ]
};

// A batch number in the default GRN batch shape (B-YYMMDD-NNNN, Stage 61),
// dated today, so a design previews and scan-checks at the real length.
export const SAMPLE_BATCH_NO = (() => {
  const d = new Date();
  const p = n => String(n).padStart(2, '0');
  return `B-${String(d.getFullYear()).slice(-2)}${p(d.getMonth() + 1)}${p(d.getDate())}-0001`;
})();

// The built-in label fields every sticker carries (engines.StickerLabel);
// anything else is read off label.fields, i.e. the Item record.
export const BUILTIN_FIELDS = [
  { key: 'name', label: 'Item name', sample: 'Sample Item Name' },
  { key: 'sku', label: 'SKU', sample: 'SKU-0001' },
  { key: 'barcode', label: 'Barcode value', sample: '8901234567890' },
  { key: 'hsn_code', label: 'HSN code', sample: '7113' },
  { key: 'category', label: 'Category', sample: 'Rings' },
  { key: 'batch_no', label: 'Batch / Lot', sample: SAMPLE_BATCH_NO },
  { key: 'expiry_date', label: 'Expiry date', sample: '2027-01-01' },
  { key: 'mfg_date', label: 'Mfg date', sample: '2026-01-01' },
  { key: 'qty', label: 'Qty (copies on this line)', sample: '1' },
  { key: 'source_doc', label: 'Source document no.', sample: 'GRN-0001' }
];

// An Item's stored variant shorthand ("metal_type:Silver;size:7") as a tag
// shows it ("Silver / 7") - mirrors engines.StickerVariantText, which does the
// same for real prints.
export function variantText(raw) {
  return String(raw || '').split(';')
    .map(p => p.slice(p.indexOf(':') + 1).trim())
    .filter(Boolean)
    .join(' / ');
}

export const SAMPLE_LABEL = {
  sku: 'SKU-0001', name: 'Sample Item Name', barcode: '8901234567890', hsn_code: '7113',
  category: 'Rings', batch_no: SAMPLE_BATCH_NO, expiry_date: '2027-01-01', mfg_date: '2026-01-01',
  qty: 1, source_doc_id: 'GRN-0001', fields: {}
};

// ---------------------------------------------------------------------------
// Template / element normalisation
// ---------------------------------------------------------------------------

function num(v, fallback) {
  const n = Number(v);
  return Number.isFinite(n) ? n : fallback;
}

function parseJSON(raw, fallback) {
  if (raw && typeof raw === 'object') return raw;
  if (typeof raw !== 'string' || !raw.trim()) return fallback;
  try { return JSON.parse(raw); } catch (e) { return fallback; }
}

// Fills defaults in place of missing keys and maps a Stage-52 element (no
// `kind`; field 'barcode' meant a barcode, everything else wrapping text,
// top-aligned) onto the current shape without changing how it looked.
export function normaliseElement(raw) {
  const el = Object.assign({}, raw);
  if (!el.id) el.id = newElementId();
  if (!el.kind) {
    el.kind = el.field === 'barcode' ? 'barcode' : 'text';
    if (el.kind === 'text') {
      if (!el.fit) el.fit = 'wrap';
      if (!el.valign) el.valign = 'top';
    }
  }
  el.x_mm = num(el.x_mm, 0); el.y_mm = num(el.y_mm, 0);
  el.w_mm = Math.max(0.2, num(el.w_mm, 10)); el.h_mm = Math.max(0.2, num(el.h_mm, 5));
  el.rotation = [0, 90, 180, 270].includes(Number(el.rotation)) ? Number(el.rotation) : 0;
  if (el.kind === 'text') {
    if (!el.field) el.field = 'custom';
    if (el.field === 'static') el.field = 'custom';
    el.font_size_mm = num(el.font_size_mm, 3.5);
    if (!el.fit) el.fit = 'shrink';
    if (!el.align) el.align = 'left';
    if (!el.valign) el.valign = 'middle';
  } else if (el.kind === 'barcode') {
    if (!el.field) el.field = 'barcode';
    if (!el.symbology) el.symbology = 'code128';
    if (el.show_text === undefined) el.show_text = true;
    if (el.quiet_zone === undefined) el.quiet_zone = true;
  } else if (el.kind === 'qr') {
    if (!el.field) el.field = 'barcode';
    if (!el.ecc) el.ecc = 'M';
  } else if (el.kind === 'line' || el.kind === 'box') {
    el.thickness_mm = num(el.thickness_mm, 0.3);
  } else if (el.kind === 'image') {
    el.threshold = num(el.threshold, 128);
  }
  return el;
}

let elementSeq = 0;
export function newElementId() {
  elementSeq = (elementSeq + 1) % 1296;
  return 'e' + Date.now().toString(36) + elementSeq.toString(36);
}

// Elements are stored as compactly as possible: the server caps a single
// document field (platform.field_max_length, 10,000 characters by default),
// and a rich jewellery tag can carry two dozen elements.
const ELEMENT_DEFAULTS = { rotation: 0, bold: false, italic: false, invert: false, locked: false, hidden: false, group: false, quiet_zone: false, show_text: false, fill: false };
export function compactElement(el) {
  const out = {};
  for (const [k, v] of Object.entries(el)) {
    if (v === undefined || v === null || v === '') continue;
    if (k in ELEMENT_DEFAULTS && ELEMENT_DEFAULTS[k] === v && !(k === 'show_text' || k === 'quiet_zone')) continue;
    out[k] = typeof v === 'number' ? Math.round(v * 100) / 100 : v;
  }
  return out;
}

export function normaliseSettings(raw) {
  const s = Object.assign({}, DEFAULT_SETTINGS, raw || {});
  s.sheet = Object.assign({}, DEFAULT_SETTINGS.sheet, (raw && raw.sheet) || {});
  s.dpi = num(s.dpi, 203) || 203;
  s.per_row = Math.max(1, Math.min(10, Math.round(num(s.per_row, 1)) || 1));
  s.per_col = Math.max(1, Math.min(10, Math.round(num(s.per_col, 1)) || 1));
  s.direction = s.direction === 'vertical' ? 'vertical' : 'horizontal';
  s.gap_h_mm = num(s.gap_h_mm, 2);
  s.gap_v_mm = num(s.gap_v_mm, 3);
  s.offset_x_mm = num(s.offset_x_mm, 0);
  s.offset_y_mm = num(s.offset_y_mm, 0);
  return s;
}

// StickerTemplate document -> in-memory template.
export function templateFromDoc(doc) {
  const design = parseJSON(doc.design, {}) || {};
  const elements = parseJSON(doc.elements, []);
  return {
    id: doc.id || '', code: doc.code || '', name: doc.name || '',
    categories: doc.categories || '', is_default: doc.is_default === true || doc.is_default === 'true',
    status: doc.status || 'Active',
    w: num(doc.label_width_mm, 50) || 50, h: num(doc.label_height_mm, 25) || 25,
    settings: normaliseSettings(design.settings),
    assets: design.assets && typeof design.assets === 'object' ? design.assets : {},
    elements: Array.isArray(elements) ? elements.map(normaliseElement) : []
  };
}

// In-memory template -> the StickerTemplate document body the generic doc
// API saves. Unused logo assets are dropped so a deleted logo does not ride
// along forever.
export function templateToDoc(t) {
  const used = new Set(t.elements.filter(e => e.kind === 'image' && e.asset).map(e => e.asset));
  const assets = {};
  for (const id of used) if (t.assets[id]) assets[id] = t.assets[id];
  return {
    code: t.code, name: t.name, categories: t.categories || '', is_default: !!t.is_default,
    status: t.status || 'Active',
    label_width_mm: t.w, label_height_mm: t.h,
    elements: JSON.stringify(t.elements.map(compactElement)),
    design: JSON.stringify({ v: 2, settings: t.settings, assets })
  };
}

// A ready design as an unsaved template of the client's own.
export function designTemplate(d) {
  const t = templateFromDoc({ label_width_mm: d.w, label_height_mm: d.h });
  t.name = d.label;
  t.settings.per_row = d.per_row || 1;
  t.settings.per_col = d.per_col || 1;
  t.settings.direction = d.direction || 'horizontal';
  if (d.gap_h !== undefined) t.settings.gap_h_mm = d.gap_h;
  if (d.gap_v !== undefined) t.settings.gap_v_mm = d.gap_v;
  if (d.dpi) t.settings.dpi = d.dpi;
  t.elements = d.elements.map(e => normaliseElement(Object.assign({}, e, { id: newElementId() })));
  return t;
}

// Stage 113.2: the ready designs are offered wherever a template is picked
// (Print, Template rules). Picking one saves it once as a template of the
// client's own - the server's rules and prints only know saved templates -
// and every later pick reuses that one. A design counts as saved when an
// Active template carries its code or its name (Duplicate names it so).
const isSavedDesign = (doc, d) => new RegExp(`^${d.code}(-\\d+)?$`).test(String(doc.code || doc.id || '').toUpperCase()) ||
  String(doc.name || '').trim().toLowerCase() === d.label.toLowerCase();
const isActiveDoc = doc => (doc.status || 'Active') === 'Active';

export function unsavedDesigns(docs) {
  return READY_DESIGNS.filter(d => !docs.some(x => isActiveDoc(x) && isSavedDesign(x, d)));
}

// The record id of the design's saved template, saving it first if needed;
// '' when it could not be saved (the error is already shown).
export async function ensureDesignTemplate(designId) {
  const d = READY_DESIGNS.find(x => x.id === designId);
  if (!d) return '';
  const res = await window.apiFetch('/api/v1/doc/StickerTemplate');
  if (!res) return '';
  if (!res.ok) { await window.showApiError?.(res, 'Could not read the sticker templates.'); return ''; }
  const docs = await res.json();
  const saved = docs.find(x => isActiveDoc(x) && isSavedDesign(x, d));
  if (saved) return saved.id;
  const codes = new Set(docs.map(x => String(x.code || x.id).toUpperCase()));
  let code = d.code, n = 2;
  while (codes.has(code)) code = `${d.code}-${n++}`;
  const t = designTemplate(d);
  t.code = code;
  const out = await window.apiFetch('/api/v1/doc/StickerTemplate', { method: 'POST', body: JSON.stringify(templateToDoc(t)) });
  if (!out) return '';
  if (!out.ok) { await window.showApiError?.(out, `Could not save "${d.label}".`, 'Save failed'); return ''; }
  const body = await out.json().catch(() => ({}));
  window.showToast?.(`"${d.label}" saved as a template of your own.`, { variant: 'success' });
  return body.id || code;
}

export function fontCSS(id) {
  const f = FONT_FAMILIES.find(x => x.id === id);
  return f ? f.css : (id ? `"${String(id).replace(/"/g, '')}", Arial, sans-serif` : FONT_FAMILIES[0].css);
}

// ---------------------------------------------------------------------------
// Data: what an element prints for a given label
// ---------------------------------------------------------------------------

export function labelFieldValue(label, key) {
  if (!label) return '';
  switch (key) {
    case 'sku': return label.sku || '';
    case 'name': return label.name || '';
    case 'barcode': return label.barcode || '';
    case 'hsn_code': return label.hsn_code || '';
    case 'category': return label.category || '';
    case 'batch_no': return label.batch_no || '';
    case 'expiry_date': return label.expiry_date || '';
    case 'mfg_date': return label.mfg_date || '';
    case 'qty': return label.qty ? String(label.qty) : '';
    case 'source_doc': return label.source_doc_id || '';
    default: {
      const v = label.fields && label.fields[key];
      return v === undefined || v === null ? '' : String(v);
    }
  }
}

function formatValue(v, el) {
  let s = String(v);
  if (s === '') return '';
  const d = el.decimals;
  if ((d !== undefined && d !== '') || el.group_digits) {
    const n = Number(s);
    if (s.trim() !== '' && Number.isFinite(n)) {
      const places = d !== undefined && d !== '' ? Math.max(0, Math.min(6, Number(d))) : undefined;
      s = el.group_digits
        ? n.toLocaleString('en-IN', places === undefined ? {} : { minimumFractionDigits: places, maximumFractionDigits: places })
        : (places === undefined ? s : n.toFixed(places));
    }
  }
  if (el.transform === 'upper') s = s.toUpperCase();
  else if (el.transform === 'lower') s = s.toLowerCase();
  return s;
}

// The text (or barcode/QR data) an element prints. '' means "print nothing":
// a field-bound element whose value is blank disappears entirely, prefix and
// suffix included, so an item with no weight never shows "W: gm".
export function elementText(el, label) {
  if (el.field === 'custom' || el.field === 'static') {
    const raw = String(el.text || '');
    let fields = 0, filled = 0;
    const out = raw.replace(/\{([a-zA-Z0-9_]+)\}/g, (m, key) => {
      fields++;
      const v = formatValue(labelFieldValue(label, key), el);
      if (v !== '') filled++;
      return v;
    });
    // "W:{gross_weight} gm" on an item with no weight prints nothing at all;
    // a text combining several fields still prints whichever are present.
    if (fields > 0 && filled === 0) return '';
    return out.trim() === '' ? '' : out;
  }
  const v = formatValue(labelFieldValue(label, el.field), el);
  if (v === '') return '';
  return (el.prefix || '') + v + (el.suffix || '');
}

// ---------------------------------------------------------------------------
// Code 128 (auto subset B/C)
// ---------------------------------------------------------------------------

const C128 = [
  '212222', '222122', '222221', '121223', '121322', '131222', '122213', '122312', '132212', '221213',
  '221312', '231212', '112232', '122132', '122231', '113222', '123122', '123221', '223211', '221132',
  '221231', '213212', '223112', '312131', '311222', '321122', '321221', '312212', '322112', '322211',
  '212123', '212321', '232121', '111323', '131123', '131321', '112313', '132113', '132311', '211313',
  '231113', '231311', '112133', '112331', '132131', '113123', '113321', '133121', '313121', '211331',
  '231131', '213113', '213311', '213131', '311123', '311321', '331121', '312113', '312311', '332111',
  '314111', '221411', '431111', '111224', '111422', '121124', '121421', '141122', '141221', '112214',
  '112412', '122114', '122411', '142112', '142211', '241211', '221114', '413111', '241112', '134111',
  '111242', '121142', '121241', '114212', '124112', '124211', '411212', '421112', '421211', '212141',
  '214121', '412121', '111143', '111341', '131141', '114113', '114311', '411113', '411311', '113141',
  '114131', '311141', '411131', '211412', '211214', '211232'
];

// Returns the module pattern (array of 0/1, 1 = bar) or null when the value
// holds a character Code 128 B/C cannot carry. Long digit runs switch to
// subset C (two digits per symbol), which roughly halves an EAN-length
// barcode's width - the difference between fitting a jewellery tag or not.
export function encodeCode128(text) {
  const s = String(text || '');
  if (!s) return null;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 32 || c > 126) return null;
  }
  const isDigit = i => { const c = s.charCodeAt(i); return c >= 48 && c <= 57; };
  const digitRun = i => { let n = 0; while (i + n < s.length && isDigit(i + n)) n++; return n; };
  const codes = [];
  let set;
  const lead = digitRun(0);
  if (lead >= 4 || (lead === s.length && lead >= 2 && lead % 2 === 0)) { set = 'C'; codes.push(105); } else { set = 'B'; codes.push(104); }
  let i = 0;
  while (i < s.length) {
    if (set === 'C') {
      if (digitRun(i) >= 2) { codes.push(Number(s.substr(i, 2))); i += 2; continue; }
      set = 'B'; codes.push(100); continue;
    }
    const run = digitRun(i);
    if (run >= 6 || (run >= 4 && i + run === s.length)) {
      if (run % 2 === 1) { codes.push(s.charCodeAt(i) - 32); i++; }
      set = 'C'; codes.push(99); continue;
    }
    codes.push(s.charCodeAt(i) - 32); i++;
  }
  let sum = codes[0];
  for (let k = 1; k < codes.length; k++) sum += codes[k] * k;
  codes.push(sum % 103);
  const modules = [];
  const pushPattern = p => { for (let k = 0; k < p.length; k++) { const w = +p[k]; for (let j = 0; j < w; j++) modules.push(k % 2 === 0 ? 1 : 0); } };
  codes.forEach(c => pushPattern(C128[c]));
  pushPattern('2331112');
  return modules;
}

// ---------------------------------------------------------------------------
// EAN-13
// ---------------------------------------------------------------------------

const EAN_L = ['0001101', '0011001', '0010011', '0111101', '0100011', '0110001', '0101111', '0111011', '0110111', '0001011'];
const EAN_G = ['0100111', '0110011', '0011011', '0100001', '0011101', '0111001', '0000101', '0010001', '0001001', '0010111'];
const EAN_R = ['1110010', '1100110', '1101100', '1000010', '1011100', '1001110', '1010000', '1000100', '1001000', '1110100'];
const EAN_PARITY = ['LLLLLL', 'LLGLGG', 'LLGGLG', 'LLGGGL', 'LGLLGG', 'LGGLLG', 'LGGGLL', 'LGLGLG', 'LGLGGL', 'LGGLGL'];

// 12 digits (check digit computed) or 13 with a correct check digit; anything
// else is null and the caller falls back to Code 128.
export function ean13Digits(text) {
  const d = String(text || '').replace(/\s/g, '');
  if (!/^\d{12,13}$/.test(d)) return null;
  let sum = 0;
  for (let i = 0; i < 12; i++) sum += Number(d[i]) * (i % 2 ? 3 : 1);
  const check = (10 - (sum % 10)) % 10;
  if (d.length === 13 && Number(d[12]) !== check) return null;
  return d.slice(0, 12) + check;
}

export function encodeEAN13(digits) {
  const first = Number(digits[0]);
  let bits = '101';
  for (let i = 1; i <= 6; i++) bits += (EAN_PARITY[first][i - 1] === 'L' ? EAN_L : EAN_G)[Number(digits[i])];
  bits += '01010';
  for (let i = 7; i <= 12; i++) bits += EAN_R[Number(digits[i])];
  bits += '101';
  return Array.from(bits, b => (b === '1' ? 1 : 0));
}

// ---------------------------------------------------------------------------
// QR code (byte mode, versions 1-40, ECC L/M/Q/H)
// ---------------------------------------------------------------------------

const QR_ECC_ORDINAL = { L: 0, M: 1, Q: 2, H: 3 };
const QR_ECC_FORMAT = { L: 1, M: 0, Q: 3, H: 2 };
const QR_ECC_PER_BLOCK = [
  [-1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28],
  [-1, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  [-1, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30]
];
const QR_NUM_BLOCKS = [
  [-1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25],
  [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49],
  [-1, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68],
  [-1, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81]
];

function qrRawModules(ver) {
  let r = (16 * ver + 128) * ver + 64;
  if (ver >= 2) {
    const na = Math.floor(ver / 7) + 2;
    r -= (25 * na - 10) * na - 55;
    if (ver >= 7) r -= 36;
  }
  return r;
}

function qrDataCodewords(ver, ecc) {
  const o = QR_ECC_ORDINAL[ecc];
  return Math.floor(qrRawModules(ver) / 8) - QR_ECC_PER_BLOCK[o][ver] * QR_NUM_BLOCKS[o][ver];
}

function gfMul(x, y) {
  let z = 0;
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11D);
    z ^= ((y >>> i) & 1) * x;
  }
  return z & 0xFF;
}

function rsDivisor(degree) {
  const r = new Array(degree).fill(0);
  r[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < r.length; j++) {
      r[j] = gfMul(r[j], root);
      if (j + 1 < r.length) r[j] ^= r[j + 1];
    }
    root = gfMul(root, 0x02);
  }
  return r;
}

function rsRemainder(data, divisor) {
  const r = divisor.map(() => 0);
  for (const b of data) {
    const factor = b ^ r.shift();
    r.push(0);
    divisor.forEach((coef, i) => { r[i] ^= gfMul(coef, factor); });
  }
  return r;
}

function utf8Bytes(s) {
  return Array.from(new TextEncoder().encode(String(s)));
}

// Returns { size, modules: boolean[][] (row-major, true = dark) } or null when
// the data is too long even for version 40.
export function encodeQR(text, ecc = 'M') {
  if (!(ecc in QR_ECC_ORDINAL)) ecc = 'M';
  const data = utf8Bytes(text);
  let ver = 1;
  for (; ver <= 40; ver++) {
    const ccBits = ver <= 9 ? 8 : 16;
    if (4 + ccBits + data.length * 8 <= qrDataCodewords(ver, ecc) * 8) break;
  }
  if (ver > 40) return null;

  // Bit stream: byte mode, count, data, terminator, pad.
  const bits = [];
  const put = (val, len) => { for (let i = len - 1; i >= 0; i--) bits.push((val >>> i) & 1); };
  put(4, 4);
  put(data.length, ver <= 9 ? 8 : 16);
  data.forEach(b => put(b, 8));
  const capBits = qrDataCodewords(ver, ecc) * 8;
  put(0, Math.min(4, capBits - bits.length));
  put(0, (8 - (bits.length % 8)) % 8);
  for (let pad = 0xEC; bits.length < capBits; pad ^= 0xEC ^ 0x11) put(pad, 8);
  const codewords = [];
  for (let i = 0; i < bits.length; i += 8) {
    let b = 0;
    for (let j = 0; j < 8; j++) b = (b << 1) | bits[i + j];
    codewords.push(b);
  }

  // Error correction + interleave.
  const o = QR_ECC_ORDINAL[ecc];
  const numBlocks = QR_NUM_BLOCKS[o][ver];
  const eccLen = QR_ECC_PER_BLOCK[o][ver];
  const rawCodewords = Math.floor(qrRawModules(ver) / 8);
  const numShort = numBlocks - (rawCodewords % numBlocks);
  const shortLen = Math.floor(rawCodewords / numBlocks);
  const divisor = rsDivisor(eccLen);
  const blocks = [];
  for (let i = 0, k = 0; i < numBlocks; i++) {
    const dat = codewords.slice(k, k + shortLen - eccLen + (i < numShort ? 0 : 1));
    k += dat.length;
    const ec = rsRemainder(dat, divisor);
    if (i < numShort) dat.push(0);
    blocks.push(dat.concat(ec));
  }
  const final = [];
  for (let i = 0; i < blocks[0].length; i++) {
    blocks.forEach((block, j) => {
      if (i !== shortLen - eccLen || j >= numShort) final.push(block[i]);
    });
  }

  // Function patterns.
  const size = ver * 4 + 17;
  const mod = Array.from({ length: size }, () => new Array(size).fill(false));
  const fn = Array.from({ length: size }, () => new Array(size).fill(false));
  const setF = (x, y, dark) => { mod[y][x] = dark; fn[y][x] = true; };
  for (let i = 0; i < size; i++) { setF(6, i, i % 2 === 0); setF(i, 6, i % 2 === 0); }
  const finder = (cx, cy) => {
    for (let dy = -4; dy <= 4; dy++) {
      for (let dx = -4; dx <= 4; dx++) {
        const x = cx + dx, y = cy + dy;
        if (x < 0 || x >= size || y < 0 || y >= size) continue;
        const d = Math.max(Math.abs(dx), Math.abs(dy));
        setF(x, y, d !== 2 && d !== 4);
      }
    }
  };
  finder(3, 3); finder(size - 4, 3); finder(3, size - 4);
  if (ver > 1) {
    const na = Math.floor(ver / 7) + 2;
    const step = ver === 32 ? 26 : Math.ceil((ver * 4 + 4) / (na * 2 - 2)) * 2;
    const pos = [6];
    for (let p = size - 7; pos.length < na; p -= step) pos.splice(1, 0, p);
    for (let i = 0; i < na; i++) {
      for (let j = 0; j < na; j++) {
        if ((i === 0 && j === 0) || (i === 0 && j === na - 1) || (i === na - 1 && j === 0)) continue;
        for (let dy = -2; dy <= 2; dy++) {
          for (let dx = -2; dx <= 2; dx++) setF(pos[i] + dx, pos[j] + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
        }
      }
    }
  }
  const drawFormat = mask => {
    const dataBits = (QR_ECC_FORMAT[ecc] << 3) | mask;
    let rem = dataBits;
    for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
    const fb = ((dataBits << 10) | rem) ^ 0x5412;
    const bit = i => ((fb >>> i) & 1) !== 0;
    for (let i = 0; i <= 5; i++) setF(8, i, bit(i));
    setF(8, 7, bit(6)); setF(8, 8, bit(7)); setF(7, 8, bit(8));
    for (let i = 9; i < 15; i++) setF(14 - i, 8, bit(i));
    for (let i = 0; i < 8; i++) setF(size - 1 - i, 8, bit(i));
    for (let i = 8; i < 15; i++) setF(8, size - 15 + i, bit(i));
    setF(8, size - 8, true);
  };
  drawFormat(0);
  if (ver >= 7) {
    let rem = ver;
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1F25);
    const vb = (ver << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const dark = ((vb >>> i) & 1) !== 0;
      const a = size - 11 + (i % 3), b = Math.floor(i / 3);
      setF(a, b, dark); setF(b, a, dark);
    }
  }

  // Data placement (zig-zag).
  let bi = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j;
        const upward = ((right + 1) & 2) === 0;
        const y = upward ? size - 1 - vert : vert;
        if (!fn[y][x] && bi < final.length * 8) {
          mod[y][x] = ((final[bi >>> 3] >>> (7 - (bi & 7))) & 1) !== 0;
          bi++;
        }
      }
    }
  }

  // Mask: pick the lowest-penalty of the eight.
  const maskFn = [
    (x, y) => (x + y) % 2 === 0,
    (x, y) => y % 2 === 0,
    (x) => x % 3 === 0,
    (x, y) => (x + y) % 3 === 0,
    (x, y) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0,
    (x, y) => ((x * y) % 2) + ((x * y) % 3) === 0,
    (x, y) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0,
    (x, y) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0
  ];
  const applyMask = m => {
    for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (!fn[y][x] && maskFn[m](x, y)) mod[y][x] = !mod[y][x];
  };
  const penalty = () => {
    let p = 0, dark = 0;
    for (let y = 0; y < size; y++) {
      for (let pass = 0; pass < 2; pass++) {
        let run = 1;
        for (let i = 1; i <= size; i++) {
          const same = i < size && (pass ? mod[i][y] === mod[i - 1][y] : mod[y][i] === mod[y][i - 1]);
          if (same) run++; else { if (run >= 5) p += run - 2; run = 1; }
        }
      }
      for (let x = 0; x < size; x++) {
        if (mod[y][x]) dark++;
        if (x < size - 1 && y < size - 1 && mod[y][x] === mod[y][x + 1] && mod[y][x] === mod[y + 1][x] && mod[y][x] === mod[y + 1][x + 1]) p += 3;
      }
    }
    p += Math.floor(Math.abs(dark * 20 - size * size * 10) / (size * size)) * 10;
    return p;
  };
  let best = 0, bestP = Infinity;
  for (let m = 0; m < 8; m++) {
    applyMask(m); drawFormat(m);
    const p = penalty();
    if (p < bestP) { bestP = p; best = m; }
    applyMask(m);
  }
  applyMask(best); drawFormat(best);
  return { size, version: ver, modules: mod };
}

// ---------------------------------------------------------------------------
// Barcode geometry - always computed in printer dots, so the studio shows the
// exact width the printer will produce (a module is a whole number of dots).
// ---------------------------------------------------------------------------

const C128_QUIET = 10;

// Returns { modules: 0/1[], ean: digits|null, quietL, quietR } for a 1D
// barcode element, or null when the value cannot be encoded at all.
function linearPattern(el, text) {
  if (el.symbology === 'ean13') {
    const d = ean13Digits(text);
    if (d) return { modules: encodeEAN13(d), ean: d, quietL: el.quiet_zone ? 11 : 0, quietR: el.quiet_zone ? 7 : 0 };
  }
  const m = encodeCode128(text);
  if (!m) return null;
  return { modules: m, ean: null, quietL: el.quiet_zone ? C128_QUIET : 0, quietR: el.quiet_zone ? C128_QUIET : 0 };
}

// Scan-readiness check for the studio: how many dots one bar module gets at
// the given DPI, and whether that is enough. < 2 dots at 203 dpi (0.25 mm)
// is the practical floor for handheld scanners.
export function barcodeFit(el, label, dpi) {
  const dpm = dpi / MM_PER_INCH;
  const text = elementText(el, label);
  if (!text) return { ok: true, empty: true };
  if (el.kind === 'qr') {
    const qr = encodeQR(text, el.ecc);
    if (!qr) return { ok: false, message: 'Too much data for a QR code.' };
    const quiet = el.quiet_zone ? 4 : 0;
    const side = Math.min(el.w_mm, el.h_mm) * dpm;
    const dots = Math.floor(side / (qr.size + quiet * 2));
    return { ok: dots >= 2, dots, message: dots >= 2 ? `${qr.size}×${qr.size} modules, ${dots} dots each` : 'QR modules are under 2 dots - make it bigger or shorten the data.' };
  }
  const p = linearPattern(el, text);
  if (!p) return { ok: false, message: 'This value has characters a barcode cannot carry.' };
  const len = (el.rotation === 90 || el.rotation === 270) ? el.h_mm : el.w_mm;
  const total = p.modules.length + p.quietL + p.quietR;
  const dots = Math.floor((len * dpm) / total);
  const fallback = el.symbology === 'ean13' && !p.ean ? ' (not a valid EAN-13 - printing as Code 128)' : '';
  return { ok: dots >= 2, dots, message: (dots >= 2 ? `${dots} dots per bar` : 'Bars are under 2 dots wide - widen the barcode or shorten the value.') + fallback };
}

// ---------------------------------------------------------------------------
// Drawing
// ---------------------------------------------------------------------------

const imageCache = new Map();

// Logos are data URLs; decoding is async, so callers await this once before
// the (synchronous) draw.
export async function ensureTemplateAssets(t) {
  const waits = [];
  for (const el of t.elements) {
    if (el.kind !== 'image' || !el.asset) continue;
    const src = t.assets[el.asset];
    if (!src || imageCache.has(src)) continue;
    const img = new Image();
    const p = new Promise(resolve => { img.onload = resolve; img.onerror = resolve; });
    img.src = src;
    imageCache.set(src, img);
    waits.push(p);
  }
  await Promise.all(waits);
}

const monoImageCache = new Map();
function monoImage(img, threshold, invert) {
  const key = img.src.length + ':' + img.src.slice(-64) + ':' + threshold + ':' + (invert ? 1 : 0);
  if (monoImageCache.has(key)) return monoImageCache.get(key);
  const c = document.createElement('canvas');
  c.width = img.naturalWidth || 1; c.height = img.naturalHeight || 1;
  const g = c.getContext('2d', { willReadFrequently: true });
  g.drawImage(img, 0, 0);
  const d = g.getImageData(0, 0, c.width, c.height);
  for (let i = 0; i < d.data.length; i += 4) {
    const a = d.data[i + 3] / 255;
    const lum = (0.299 * d.data[i] + 0.587 * d.data[i + 1] + 0.114 * d.data[i + 2]) * a + 255 * (1 - a);
    let dark = lum < threshold;
    if (invert) dark = !dark;
    d.data[i] = d.data[i + 1] = d.data[i + 2] = dark ? 0 : 255;
    d.data[i + 3] = dark ? 255 : 0;
  }
  g.putImageData(d, 0, 0);
  monoImageCache.set(key, c);
  return c;
}

// Sets up an axis-aligned frame for an element so that content drawn at
// (0,0)-(iw,ih) lands inside the element's box at its rotation. With `snap`,
// the frame origin is a whole device pixel, so whole-dot bars stay crisp.
function withElementFrame(ctx, el, scale, snap, fn) {
  let X = el.x_mm * scale, Y = el.y_mm * scale, W = el.w_mm * scale, H = el.h_mm * scale;
  if (snap) { X = Math.round(X); Y = Math.round(Y); W = Math.round(W); H = Math.round(H); }
  ctx.save();
  const r = el.rotation || 0;
  if (r === 90) { ctx.translate(X + W, Y); ctx.rotate(Math.PI / 2); }
  else if (r === 180) { ctx.translate(X + W, Y + H); ctx.rotate(Math.PI); }
  else if (r === 270) { ctx.translate(X, Y + H); ctx.rotate(-Math.PI / 2); }
  else ctx.translate(X, Y);
  const sideways = r === 90 || r === 270;
  const iw = sideways ? H : W, ih = sideways ? W : H;
  ctx.beginPath(); ctx.rect(0, 0, iw, ih); ctx.clip();
  fn(iw, ih);
  ctx.restore();
}

function setFont(ctx, el, px, family) {
  ctx.font = `${el.italic ? 'italic ' : ''}${el.bold ? '700' : '400'} ${px}px ${fontCSS(el.font_family || family)}`;
}

function wrapLines(ctx, text, maxW) {
  const out = [];
  for (const para of String(text).split('\n')) {
    const words = para.split(/(\s+)/);
    let line = '';
    for (const w of words) {
      const test = line + w;
      if (line && ctx.measureText(test).width > maxW && w.trim()) {
        out.push(line.trimEnd());
        line = w.trimStart();
        // A single word wider than the box is broken by characters.
        while (ctx.measureText(line).width > maxW && line.length > 1) {
          let k = line.length - 1;
          while (k > 1 && ctx.measureText(line.slice(0, k)).width > maxW) k--;
          out.push(line.slice(0, k));
          line = line.slice(k);
        }
      } else {
        line = test;
      }
    }
    out.push(line.trimEnd());
  }
  return out;
}

function drawTextContent(ctx, el, text, iw, ih, scale, family, ink) {
  let px = Math.max(1, el.font_size_mm * scale);
  setFont(ctx, el, px, family);
  let lines;
  if (el.fit === 'wrap') {
    lines = wrapLines(ctx, text, iw);
  } else {
    lines = String(text).split('\n');
    if (el.fit === 'shrink') {
      const widest = Math.max(...lines.map(l => ctx.measureText(l).width));
      const lh = px * 1.15 * lines.length;
      const k = Math.min(1, widest > 0 ? iw / widest : 1, lh > 0 ? ih / lh : 1);
      if (k < 1) { px = Math.max(1, px * k * 0.995); setFont(ctx, el, px, family); }
    }
  }
  const lh = px * 1.15;
  const blockH = lh * lines.length;
  let y0 = 0;
  if (el.valign === 'middle') y0 = (ih - blockH) / 2;
  else if (el.valign === 'bottom') y0 = ih - blockH;
  ctx.fillStyle = ink;
  ctx.textBaseline = 'middle';
  ctx.textAlign = el.align === 'center' ? 'center' : el.align === 'right' ? 'right' : 'left';
  const x = el.align === 'center' ? iw / 2 : el.align === 'right' ? iw : 0;
  lines.forEach((line, i) => ctx.fillText(line, x, y0 + lh * i + lh / 2));
}

// Linear barcode inside an element frame. Module width is computed in printer
// dots (dpm = dots per mm) then drawn at `scale` px per mm.
function drawLinear(ctx, el, text, iw, ih, scale, dpm, snap, family, ink) {
  const p = linearPattern(el, text);
  if (!p) {
    drawTextContent(ctx, Object.assign({}, el, { fit: 'shrink', valign: 'middle', align: 'center', font_size_mm: 2.5 }), '⚠ ' + text, iw, ih, scale, family, ink);
    return;
  }
  const total = p.modules.length + p.quietL + p.quietR;
  const lenDots = (iw / scale) * dpm;
  const mDots = Math.max(1, Math.floor(lenDots / total));
  const mw = (mDots / dpm) * scale;
  const barsW = mw * total;
  let x0 = (iw - barsW) / 2 + p.quietL * mw;
  if (snap) x0 = Math.round(x0);
  const textPx = el.show_text ? Math.max(1, Math.min(ih * 0.28, (el.font_size_mm || 2.4) * scale)) : 0;
  const gap = el.show_text ? textPx * 0.15 : 0;
  const barH = Math.max(1, ih - textPx - gap);
  ctx.fillStyle = ink;
  let i = 0;
  while (i < p.modules.length) {
    if (!p.modules[i]) { i++; continue; }
    let j = i;
    while (j < p.modules.length && p.modules[j]) j++;
    const guard = p.ean && (i < 3 || (i >= 45 && i < 50) || i >= 92);
    const h = guard && el.show_text ? barH + textPx * 0.5 : barH;
    ctx.fillRect(x0 + i * mw, 0, (j - i) * mw, snap ? Math.round(h) : h);
    i = j;
  }
  if (!el.show_text) return;
  setFont(ctx, Object.assign({}, el, { bold: false }), textPx, family);
  ctx.textBaseline = 'bottom';
  if (p.ean) {
    ctx.textAlign = 'right';
    ctx.fillText(p.ean[0], x0 - mw, ih);
    ctx.textAlign = 'center';
    ctx.fillText(p.ean.slice(1, 7), x0 + 24 * mw, ih);
    ctx.fillText(p.ean.slice(7), x0 + 71 * mw, ih);
  } else {
    ctx.textAlign = 'center';
    ctx.fillText(text, x0 + (p.modules.length * mw) / 2, ih);
  }
}

function drawQR(ctx, el, text, iw, ih, scale, dpm, snap, ink) {
  const qr = encodeQR(text, el.ecc);
  if (!qr) return;
  const quiet = el.quiet_zone ? 4 : 0;
  const sideDots = (Math.min(iw, ih) / scale) * dpm;
  const mDots = Math.max(1, Math.floor(sideDots / (qr.size + quiet * 2)));
  const m = (mDots / dpm) * scale;
  const full = m * (qr.size + quiet * 2);
  let x0 = (iw - full) / 2 + quiet * m, y0 = (ih - full) / 2 + quiet * m;
  if (snap) { x0 = Math.round(x0); y0 = Math.round(y0); }
  ctx.fillStyle = ink;
  for (let y = 0; y < qr.size; y++) {
    let x = 0;
    while (x < qr.size) {
      if (!qr.modules[y][x]) { x++; continue; }
      let e = x;
      while (e < qr.size && qr.modules[y][e]) e++;
      ctx.fillRect(x0 + x * m, y0 + y * m, (e - x) * m, m);
      x = e;
    }
  }
}

function roundRectPath(ctx, x, y, w, h, r) {
  r = Math.max(0, Math.min(r, w / 2, h / 2));
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

// Draws one label's elements onto ctx, with the label's top-left at the
// current origin. opts: { scale: px per mm, dpi: printer dpi for barcode
// geometry, snap: whole-pixel geometry (print), mono: thermal look,
// skipIds: elements not to draw }.
export function drawLabel(ctx, t, label, opts) {
  const scale = opts.scale;
  const dpm = (opts.dpi || t.settings.dpi || 203) / MM_PER_INCH;
  const snap = !!opts.snap;
  const family = t.settings.font_family;
  for (const el of t.elements) {
    if (el.hidden) continue;
    if (opts.skipIds && opts.skipIds.has(el.id)) continue;
    withElementFrame(ctx, el, scale, snap, (iw, ih) => {
      let ink = '#000';
      if (el.invert && (el.kind === 'text' || el.kind === 'barcode' || el.kind === 'qr')) {
        ctx.fillStyle = '#000';
        ctx.fillRect(0, 0, iw, ih);
        ink = '#fff';
      }
      if (el.kind === 'text') {
        const text = elementText(el, label);
        if (text) drawTextContent(ctx, el, text, iw, ih, scale, family, ink);
      } else if (el.kind === 'barcode') {
        const text = elementText(el, label);
        if (text) drawLinear(ctx, el, text, iw, ih, scale, dpm, snap, family, ink);
      } else if (el.kind === 'qr') {
        const text = elementText(el, label);
        if (text) drawQR(ctx, el, text, iw, ih, scale, dpm, snap, ink);
      } else if (el.kind === 'line') {
        const t2 = Math.max(snap ? 1 : 0.5, el.thickness_mm * scale);
        ctx.fillStyle = '#000';
        if (iw >= ih) ctx.fillRect(0, (ih - t2) / 2, iw, t2);
        else ctx.fillRect((iw - t2) / 2, 0, t2, ih);
      } else if (el.kind === 'box') {
        const t2 = Math.max(snap ? 1 : 0.5, el.thickness_mm * scale);
        const r = (el.radius_mm || 0) * scale;
        if (el.fill) {
          ctx.fillStyle = '#000';
          roundRectPath(ctx, 0, 0, iw, ih, r);
          ctx.fill();
        } else {
          ctx.strokeStyle = '#000';
          ctx.lineWidth = t2;
          roundRectPath(ctx, t2 / 2, t2 / 2, iw - t2, ih - t2, r);
          ctx.stroke();
        }
      } else if (el.kind === 'image') {
        const src = el.asset && t.assets[el.asset];
        const img = src && imageCache.get(src);
        if (img && img.naturalWidth) {
          const k = Math.min(iw / img.naturalWidth, ih / img.naturalHeight);
          const dw = img.naturalWidth * k, dh = img.naturalHeight * k;
          const source = opts.mono || el.invert ? monoImage(img, el.threshold, el.invert) : img;
          ctx.drawImage(source, (iw - dw) / 2, (ih - dh) / 2, dw, dh);
        }
      }
    });
  }
}

// ---------------------------------------------------------------------------
// Bitmaps (thermal)
// ---------------------------------------------------------------------------

function newCanvas(w, h) {
  const c = document.createElement('canvas');
  c.width = Math.max(1, w); c.height = Math.max(1, h);
  return c;
}

// Stage 119.2/119.3: a label's cell on the roll - the design's own size, or
// turned (its height wide) when the template prints vertical - and one print:
// per_row cells across by per_col down, the gaps between them.
export function cellSize(t) {
  return t.settings.direction === 'vertical' ? { w: t.h, h: t.w } : { w: t.w, h: t.h };
}

export function printHeightMM(t) {
  const s = t.settings;
  return s.per_col * cellSize(t).h + (s.per_col - 1) * s.gap_v_mm;
}

// Cell k of a print, from its top-left corner (offsets not included).
export function cellOrigin(t, k) {
  const s = t.settings, c = cellSize(t);
  return { x: (k % s.per_row) * (c.w + s.gap_h_mm), y: Math.floor(k / s.per_row) * (c.h + s.gap_v_mm) };
}

// drawLabel into a cell at the current origin, turned a quarter clockwise
// for a vertical template: what reads left to right reads top to bottom.
export function drawCell(ctx, t, label, opts) {
  if (t.settings.direction !== 'vertical') { drawLabel(ctx, t, label, opts); return; }
  ctx.save();
  const x = t.h * opts.scale;
  ctx.translate(opts.snap ? Math.round(x) : x, 0);
  ctx.rotate(Math.PI / 2);
  drawLabel(ctx, t, label, opts);
  ctx.restore();
}

export function mediaWidthMM(t) {
  const s = t.settings;
  const auto = s.per_row * cellSize(t).w + (s.per_row - 1) * s.gap_h_mm;
  const set = Number(s.media_w_mm);
  return set > 0 ? Math.max(set, auto) : auto;
}

// One print (per_row × per_col labels) as a 1-bit bitmap: rows of
// bytesPerRow bytes, MSB = leftmost dot, 1 = black.
export function renderRowBitmap(t, labels, dpi) {
  const dpm = dpi / MM_PER_INCH;
  const s = t.settings;
  const wDots = Math.round(mediaWidthMM(t) * dpm);
  const hDots = Math.round(printHeightMM(t) * dpm);
  const c = newCanvas(wDots, hDots);
  const ctx = c.getContext('2d', { willReadFrequently: true });
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, wDots, hDots);
  labels.forEach((label, i) => {
    if (!label) return;
    const o = cellOrigin(t, i);
    ctx.save();
    ctx.translate(Math.round((s.offset_x_mm + o.x) * dpm), Math.round((s.offset_y_mm + o.y) * dpm));
    drawCell(ctx, t, label, { scale: dpm, dpi, snap: true, mono: true });
    ctx.restore();
  });
  return canvasToBits(ctx, wDots, hDots);
}

function canvasToBits(ctx, w, h) {
  const px = ctx.getImageData(0, 0, w, h).data;
  const bpr = Math.ceil(w / 8);
  const bits = new Uint8Array(bpr * h);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = (y * w + x) * 4;
      const lum = 0.299 * px[i] + 0.587 * px[i + 1] + 0.114 * px[i + 2];
      if (lum < 128) bits[y * bpr + (x >> 3)] |= 0x80 >> (x & 7);
    }
  }
  return { width: w, height: h, bytesPerRow: bpr, bits };
}

// ---------------------------------------------------------------------------
// Printer languages
// ---------------------------------------------------------------------------

const HEX = '0123456789ABCDEF';

function zplCount(n) {
  let s = '';
  while (n >= 400) { s += 'z'; n -= 400; }
  if (n >= 20) { s += String.fromCharCode(102 + Math.floor(n / 20)); n %= 20; }
  if (n > 0) s += String.fromCharCode(70 + n);
  return s;
}

function zplRLE(hex) {
  let out = '';
  let i = 0;
  while (i < hex.length) {
    let j = i + 1;
    while (j < hex.length && hex[j] === hex[i]) j++;
    const n = j - i;
    out += (n > 1 ? zplCount(n) : '') + hex[i];
    i = j;
  }
  return out;
}

// ZPL II ^GFA data with the standard ASCII compression (repeat counts, ','
// for a zero-filled rest of row, ':' for a repeated row) - typically 10-20x
// smaller than raw hex, which is what keeps a 300-dpi tag fast to send.
export function zplGraphicData(bmp) {
  let out = '';
  let prev = null;
  for (let y = 0; y < bmp.height; y++) {
    let row = '';
    for (let i = 0; i < bmp.bytesPerRow; i++) {
      const b = bmp.bits[y * bmp.bytesPerRow + i];
      row += HEX[b >> 4] + HEX[b & 15];
    }
    if (row === prev) { out += ':'; continue; }
    prev = row;
    const trimmed = row.replace(/0+$/, '');
    out += trimmed.length < row.length ? zplRLE(trimmed) + ',' : zplRLE(row);
  }
  return out;
}

function zplDensity(d) {
  const n = Math.max(0, Math.min(30, Math.round(Number(d) * 2)));
  return String(n).padStart(2, '0');
}

export function buildZPL(rows, t) {
  const s = t.settings;
  let z = '';
  for (const r of rows) {
    const total = r.bmp.bytesPerRow * r.bmp.height;
    if (s.density !== '' && s.density !== undefined) z += `~SD${zplDensity(s.density)}\n`;
    z += '^XA\n';
    z += `^PW${r.bmp.width}\n^LL${r.bmp.height}\n^LH0,0\n${s.flip ? '^POI' : '^PON'}\n`;
    if (s.speed !== '' && s.speed !== undefined) z += `^PR${Math.round(Number(s.speed))}\n`;
    z += `^FO0,0^GFA,${total},${total},${r.bmp.bytesPerRow},${zplGraphicData(r.bmp)}^FS\n`;
    z += `^PQ${r.copies},0,1,Y\n^XZ\n`;
  }
  return z;
}

function mmStr(v) {
  return String(Math.round(v * 10) / 10);
}

// TSPL: text setup + raw BITMAP bytes. In TSPL a 0 bit prints a dot, so the
// bitmap is inverted on the way out.
export function buildTSPL(rows, t) {
  const s = t.settings;
  const enc = new TextEncoder();
  const parts = [];
  const text = str => parts.push(enc.encode(str));
  text(`SIZE ${mmStr(mediaWidthMM(t))} mm,${mmStr(printHeightMM(t))} mm\r\n`);
  text(`GAP ${mmStr(s.gap_v_mm)} mm,0 mm\r\n`);
  text(`DIRECTION ${s.flip ? 0 : 1}\r\nREFERENCE 0,0\r\n`);
  if (s.density !== '' && s.density !== undefined) text(`DENSITY ${Math.max(0, Math.min(15, Math.round(Number(s.density))))}\r\n`);
  if (s.speed !== '' && s.speed !== undefined) text(`SPEED ${Number(s.speed)}\r\n`);
  for (const r of rows) {
    text('CLS\r\n');
    text(`BITMAP 0,0,${r.bmp.bytesPerRow},${r.bmp.height},0,`);
    const inv = new Uint8Array(r.bmp.bits.length);
    for (let i = 0; i < inv.length; i++) inv[i] = ~r.bmp.bits[i] & 0xFF;
    parts.push(inv);
    text(`\r\nPRINT 1,${r.copies}\r\n`);
  }
  const len = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(len);
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}

function bytesToBase64(bytes) {
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

// ---------------------------------------------------------------------------
// Print runs
// ---------------------------------------------------------------------------

// labels (from POST /api/v1/stickers/print) -> segments of consecutive
// labels sharing a template, each label repeated by its copy count.
function expandRuns(labels, templates, copies) {
  const segs = [];
  for (const label of labels) {
    const t = (label.template_id && templates.get(label.template_id)) || defaultTemplate();
    const n = label.qty > 0 ? label.qty : Math.max(1, copies || 1);
    let seg = segs[segs.length - 1];
    if (!seg || seg.t !== t) { seg = { t, items: [] }; segs.push(seg); }
    for (let i = 0; i < n; i++) seg.items.push(label);
  }
  return segs;
}

// Prints of per_row × per_col labels; consecutive identical prints collapse
// into one with a copy count, so 50 copies of one tag are sent as one bitmap.
function segmentRows(seg) {
  const per = seg.t.settings.per_row * seg.t.settings.per_col;
  const rows = [];
  for (let i = 0; i < seg.items.length; i += per) {
    const cells = seg.items.slice(i, i + per);
    const last = rows[rows.length - 1];
    if (last && last.cells.length === cells.length && last.cells.every((c, k) => c === cells[k])) last.copies++;
    else rows.push({ cells, copies: 1 });
  }
  return rows;
}

let DEFAULT_STICKER_TEMPLATE_N = null;
function defaultTemplate() {
  if (!DEFAULT_STICKER_TEMPLATE_N) {
    DEFAULT_STICKER_TEMPLATE_N = Object.assign({}, DEFAULT_STICKER_TEMPLATE, {
      settings: normaliseSettings({}), elements: DEFAULT_STICKER_TEMPLATE.elements.map(normaliseElement)
    });
  }
  return DEFAULT_STICKER_TEMPLATE_N;
}

async function loadTemplates(labels, apiFetch) {
  const ids = [...new Set(labels.map(l => l.template_id).filter(Boolean))];
  const map = new Map();
  await Promise.all(ids.map(async id => {
    const res = await apiFetch(`/api/v1/doc/StickerTemplate/${encodeURIComponent(id)}`);
    if (res && res.ok) map.set(id, templateFromDoc(await res.json()));
  }));
  for (const t of map.values()) await ensureTemplateAssets(t);
  return map;
}

// Builds the QZ Tray data items for a thermal printer. Exposed separately so
// the studio's Test Print can send one design without a print-log row.
// Rows are packed THERMAL_ROWS_PER_ITEM to an item, and each item is sent as
// its own print call: the print agent (QZ Tray or our PrintBridge, which
// speaks the same protocol) caps one WebSocket message at 32MB, and a GRN
// with a thousand distinct SKUs at 300 dpi would otherwise approach it.
const THERMAL_ROWS_PER_ITEM = 50;
export function buildThermalItems(segments, language, dpi) {
  const items = [];
  for (const seg of segments) {
    const all = segmentRows(seg);
    for (let i = 0; i < all.length; i += THERMAL_ROWS_PER_ITEM) {
      const rows = all.slice(i, i + THERMAL_ROWS_PER_ITEM).map(r => ({ bmp: renderRowBitmap(seg.t, r.cells, dpi), copies: r.copies }));
      if (language === 'TSPL') {
        items.push({ type: 'raw', format: 'command', flavor: 'base64', data: bytesToBase64(buildTSPL(rows, seg.t)) });
      } else {
        items.push({ type: 'raw', format: 'command', flavor: 'plain', data: buildZPL(rows, seg.t) });
      }
    }
  }
  return items;
}

function printerDPI(printer, t) {
  return Number(printer && printer.dpi) || t.settings.dpi || 203;
}

// Stage 137.3: a printer's own alignment - its preset's shift, darkness and
// speed - on top of each template, so a printer that prints 1 mm off is set
// once for every template. Blank darkness / speed keep the template's. The
// shift is in the paper's frame, like the template's own offsets.
const presetNum = v => (v === '' || v === null || v === undefined || !isFinite(Number(v)) ? null : Number(v));
export function withPrinterPreset(t, printer) {
  const sx = presetNum(printer && printer.shift_right_mm), sy = presetNum(printer && printer.shift_down_mm);
  const d = presetNum(printer && printer.darkness), sp = presetNum(printer && printer.print_speed);
  if (sx === null && sy === null && d === null && sp === null) return t;
  const s = Object.assign({}, t.settings, { offset_x_mm: t.settings.offset_x_mm + (sx || 0), offset_y_mm: t.settings.offset_y_mm + (sy || 0) });
  if (d !== null) s.density = d;
  if (sp !== null) s.speed = sp;
  return Object.assign({}, t, { settings: s });
}
function tuned(segments, printer) {
  const seen = new Map();
  return segments.map(seg => {
    if (!seen.has(seg.t)) seen.set(seg.t, withPrinterPreset(seg.t, printer));
    return Object.assign({}, seg, { t: seen.get(seg.t) });
  });
}

// Stage 137.2: how a template's labels sit on the roll, as the server's
// stickerRollShape - one label's size, labels across and down, direction,
// roll or A4 sheet. A template fits a printer's roll when the shapes match.
export const rollShape = t => ({ w: t.w, h: t.h, across: t.settings.per_row, down: t.settings.per_col,
  vertical: t.settings.direction === 'vertical', sheet: t.settings.sheet.mode === 'a4' });
export const fitsRoll = (a, b) => Math.abs(a.w - b.w) < 0.05 && Math.abs(a.h - b.h) < 0.05 &&
  a.across === b.across && a.down === b.down && a.vertical === b.vertical && a.sheet === b.sheet;
export const builtInShape = () => rollShape(defaultTemplate());

// Silent thermal print through QZ Tray. Returns true when sent (or handled),
// false when the caller should fall back to the browser sheet. A failed send
// (printer off, out of labels) asks Retry / browser print (134.1a) - Retry
// resumes at the item that failed, so the labels already out are not repeated;
// with ask false it throws instead, for printNoDialog to ask.
async function printThermal(segments, printer, jobRef, { ask = true } = {}) {
  const lang = String((printer && printer.printer_language) || '').toUpperCase();
  if (lang !== 'ZPL' && lang !== 'TSPL') return false;
  if (!printer.qz_printer_name || typeof window.qzTryConnect !== 'function' || !window.QZPrint) return false;
  if (!await window.qzTryConnect()) return false;
  segments = tuned(segments, printer);
  const dpi = printerDPI(printer, segments[0].t);
  const items = buildThermalItems(segments, lang, dpi);
  const entry = {
    job_type: 'Sticker', document_ref: jobRef || '', printer_code: printer.code || printer.id || '',
    qz_printer_name: printer.qz_printer_name, print_format: lang, copies: 1
  };
  for (let next = 0; next < items.length;) {
    try {
      for (; next < items.length; next++) await window.QZPrint.printItems(printer.qz_printer_name, [items[next]], 1);
    } catch (err) {
      if (window.qzLogJob) await window.qzLogJob(Object.assign({ status: 'Failed', error_detail: err.message || String(err) }, entry));
      if (!ask || !window.loadViewModule) throw err;
      const r = await window.loadViewModule('./print-routing.js');
      const choice = await r.explainFailure('Sticker', err.message || String(err));
      if (choice === 'retry') continue;
      if (choice === 'browser') await printSheet(segments, Number(printer.dpi) || 0);
      return true;
    }
  }
  if (window.qzLogJob) await window.qzLogJob(Object.assign({ status: 'Submitted', error_detail: '' }, entry));
  if (window.showToast) window.showToast(`Sent to ${printer.qz_printer_name}.`, { variant: 'success' });
  return true;
}

// Browser print: every label is rendered by the same drawLabel to a PNG and
// laid out either as roll pages (one print per page, page sized to the roll -
// for a thermal printer driven by its Windows driver) or as an A4 sheet grid
// (laser/inkjet sticker sheets). CSS named pages let one print job mix label
// sizes. Stage 119.4: a roll label is 1-bit at the printer's own dots, shown
// unsmoothed, so the driver gets the bars the printer will burn instead of
// greys to dither; and the first page's size is also the document's default
// page, which is what Chromium hands the driver as the paper size.
// labelDots draws one label at the print's own resolution: a roll label 1-bit
// at the printer's dots, an A4 sheet label at 300 dpi or more. Shared by the
// browser sheet and the silent PDF (Stage 127.2), so both print the same bars.
function printDPI(t, dpi) {
  return t.settings.sheet.mode !== 'a4' ? (dpi || t.settings.dpi || 203) : Math.max(300, t.settings.dpi);
}

function labelDots(t, label, dpi) {
  const roll = t.settings.sheet.mode !== 'a4';
  const pdpi = printDPI(t, dpi);
  const dpm = pdpi / MM_PER_INCH;
  const cell = cellSize(t);
  const c = newCanvas(Math.round(cell.w * dpm), Math.round(cell.h * dpm));
  const ctx = c.getContext('2d', { willReadFrequently: true });
  ctx.fillStyle = '#fff'; ctx.fillRect(0, 0, c.width, c.height);
  drawCell(ctx, t, label, { scale: dpm, dpi: pdpi, snap: true, mono: roll });
  if (roll) {
    const bmp = canvasToBits(ctx, c.width, c.height);
    const img = ctx.createImageData(c.width, c.height);
    for (let y = 0; y < c.height; y++) {
      for (let x = 0; x < c.width; x++) {
        const v = bmp.bits[y * bmp.bytesPerRow + (x >> 3)] & (0x80 >> (x & 7)) ? 0 : 255;
        const i = (y * c.width + x) * 4;
        img.data[i] = img.data[i + 1] = img.data[i + 2] = v; img.data[i + 3] = 255;
      }
    }
    ctx.putImageData(img, 0, 0);
  }
  return { canvas: c, dpm };
}

// sheetPages is where every label of a print lands: one entry per page with
// its size and each label's top-left, in mm (Stage 127.2: one layout for the
// browser sheet and the silent PDF).
function sheetPages(segments) {
  const pages = [];
  segments.forEach((seg, si) => {
    const t = seg.t, s = t.settings, cell = cellSize(t);
    if (s.sheet.mode === 'a4') {
      const sh = s.sheet;
      const cols = Math.max(1, Math.floor((210 - 2 * sh.margin_left_mm + sh.gap_h_mm) / (cell.w + sh.gap_h_mm)));
      const rowsPer = Math.max(1, Math.floor((297 - 2 * sh.margin_top_mm + sh.gap_v_mm) / (cell.h + sh.gap_v_mm)));
      const perPage = cols * rowsPer;
      for (let p = 0; p < seg.items.length; p += perPage) {
        pages.push({ si, t, a4: true, w: 210, h: 297, flip: false, cells: seg.items.slice(p, p + perPage).map((label, k) => ({ label,
          x: sh.margin_left_mm + (k % cols) * (cell.w + sh.gap_h_mm) + s.offset_x_mm,
          y: sh.margin_top_mm + Math.floor(k / cols) * (cell.h + sh.gap_v_mm) + s.offset_y_mm })) });
      }
    } else {
      const mw = mediaWidthMM(t), ph = printHeightMM(t);
      for (const row of segmentRows(seg)) {
        for (let c = 0; c < row.copies; c++) {
          pages.push({ si, t, a4: false, w: mw, h: ph, flip: !!s.flip, cells: row.cells.map((label, k) => {
            const o = cellOrigin(t, k);
            return { label, x: s.offset_x_mm + o.x, y: s.offset_y_mm + o.y };
          }) });
        }
      }
    }
  });
  return pages;
}

// stickerPDFs (Stage 127.2) is the print as PDFs for a label printer driven
// by its Windows driver: each page drawn at the printer's dots, labels copied
// in pixel for pixel, one PDF per run of same-sized pages.
async function stickerPDFs(segments, dpi) {
  const pdf = await import('./print-pdf.js' + new URL(import.meta.url).search);
  const cache = new Map();
  const out = [];
  let run = null;
  for (const pg of sheetPages(segments)) {
    const page = pageCanvas(pg, dpi, cache, pg.flip);
    if (!run || run.w !== pg.w || run.h !== pg.h) { run = { w: pg.w, h: pg.h, canvases: [] }; out.push(run); }
    run.canvases.push(page);
  }
  return Promise.all(out.map(async r => pdf.blobToBase64(pdf.canvasesToPDF(r.canvases, r.w, r.h))));
}

// pageCanvas draws one page of a print at the printer's dots, its labels
// copied in pixel for pixel - the silent PDF's page and the preview's.
function pageCanvas(pg, dpi, cache, flip) {
  let perT = cache.get(pg.t);
  if (!perT) { perT = new Map(); cache.set(pg.t, perT); }
  const dpm = printDPI(pg.t, dpi) / MM_PER_INCH;
  const page = newCanvas(Math.round(pg.w * dpm), Math.round(pg.h * dpm));
  const g = page.getContext('2d');
  g.imageSmoothingEnabled = false;
  g.fillStyle = '#fff'; g.fillRect(0, 0, page.width, page.height);
  if (flip) { g.translate(page.width, page.height); g.rotate(Math.PI); }
  for (const c of pg.cells) {
    let dots = perT.get(c.label);
    if (!dots) { dots = labelDots(pg.t, c.label, dpi); perT.set(c.label, dots); }
    g.drawImage(dots.canvas, Math.round(c.x * dpm), Math.round(c.y * dpm));
  }
  return page;
}

// previewStickerLabels (Stage 133.3, user 2026-10-22: "Need a preview option
// aswell to how it will come") shows a print before it is sent: each print
// drawn at the printer's own dots by the same code the print uses, the way
// the tag reads, with the label edges dashed in grey (not ink). Identical
// prints in a row show once with their count. Resolves true when the user
// presses Print.
export async function previewStickerLabels(labels, { printer, copies = 1, apiFetch } = {}) {
  if (!labels || labels.length === 0) return false;
  defaultTemplate();
  const templates = await loadTemplates(labels, apiFetch || window.apiFetch);
  const segments = tuned(expandRuns(labels, templates, copies), printer);
  const dpi = Number(printer && printer.dpi) || 0;
  const same =(a, b) => a.t === b.t && a.cells.length === b.cells.length &&
    a.cells.every((c, k) => c.label === b.cells[k].label && c.x === b.cells[k].x && c.y === b.cells[k].y);
  const prints = [];
  for (const pg of sheetPages(segments)) {
    const last = prints[prints.length - 1];
    if (last && same(last.pg, pg)) last.n++;
    else prints.push({ pg, n: 1 });
  }
  const stickers = segments.reduce((n, s) => n + s.items.length, 0);
  const esc = window.escapeHTMLText || (s => String(s));
  const cache = new Map();

  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal-container" style="max-width: 920px;" role="dialog" aria-modal="true" aria-labelledby="stk-preview-title">
      <div class="modal-header">
        <h3 class="modal-title" id="stk-preview-title">Sticker preview</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-stk-preview="close">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button>
      </div>
      <div class="modal-body">
        <p style="margin: 0 0 12px; color: var(--text-muted);">${stickers} sticker${stickers === 1 ? '' : 's'}${printer && printer.name ? ` on ${esc(printer.name)}` : ''}. Grey dashes mark each label's edge; they do not print.</p>
        <div data-stk-preview="pages" style="display: flex; flex-wrap: wrap; gap: 20px; align-items: flex-start;"></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" data-stk-preview="close">Close</button>
        <button type="button" class="btn btn-primary" data-stk-preview="print">Print</button>
      </div>
    </div>`;
  const pagesEl = overlay.querySelector('[data-stk-preview="pages"]');
  for (const { pg, n } of prints) {
    const page = pageCanvas(pg, dpi, cache, false);
    const dpm = page.width / pg.w;
    const g = page.getContext('2d');
    g.save();
    g.strokeStyle = '#9ca3af';
    g.lineWidth = Math.max(1, Math.round(dpm / 4));
    g.setLineDash([Math.round(dpm), Math.round(dpm)]);
    const cell = cellSize(pg.t);
    if (pg.a4) {
      pg.cells.forEach(c => g.strokeRect(c.x * dpm, c.y * dpm, cell.w * dpm, cell.h * dpm));
    } else {
      const s = pg.t.settings;
      for (let k = 0; k < s.per_row * s.per_col; k++) {
        const o = cellOrigin(pg.t, k);
        g.strokeRect((s.offset_x_mm + o.x) * dpm, (s.offset_y_mm + o.y) * dpm, cell.w * dpm, cell.h * dpm);
      }
    }
    g.restore();
    const scale = Math.min(6, 520 / pg.h); // screen px per mm
    page.style.cssText = `width: ${Math.round(pg.w * scale)}px; max-width: 100%; height: auto; background: #fff; image-rendering: pixelated; box-shadow: var(--shadow-sm, 0 1px 3px rgba(0,0,0,.2));`;
    const fig = document.createElement('figure');
    fig.style.cssText = 'margin: 0; display: flex; flex-direction: column; gap: 6px; max-width: 100%;';
    fig.appendChild(page);
    const cap = document.createElement('figcaption');
    cap.style.cssText = 'font-size: 12px; color: var(--text-muted);';
    const filled = pg.cells.length, slots = pg.a4 ? filled : pg.t.settings.per_row * pg.t.settings.per_col;
    cap.textContent = [
      pg.t.name || 'Standard label',
      `${Math.round(pg.w * 10) / 10} × ${Math.round(pg.h * 10) / 10} mm`,
      n > 1 ? `printed ${n} times` : '',
      slots > filled ? `${slots - filled} of ${slots} labels left blank` : '',
      pg.flip ? 'comes out turned 180°' : ''
    ].filter(Boolean).join(' · ');
    fig.appendChild(cap);
    pagesEl.appendChild(fig);
  }
  document.body.appendChild(overlay);
  requestAnimationFrame(() => overlay.classList.add('open'));
  return new Promise(resolve => {
    const done = (v) => { overlay.remove(); document.removeEventListener('keydown', onKey); resolve(v); };
    const onKey = (e) => { if (e.key === 'Escape') done(false); };
    document.addEventListener('keydown', onKey);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) done(false); });
    overlay.querySelectorAll('[data-stk-preview="close"]').forEach(b => b.addEventListener('click', () => done(false)));
    const print = overlay.querySelector('[data-stk-preview="print"]');
    print.addEventListener('click', () => done(true));
    print.focus();
  });
}

// printStickersSilently (Stage 127.2) sends the print to this computer's
// Sticker printer through PrintBridge: raw commands when it speaks ZPL or
// TSPL, else the PDF. false with why.reason when it cannot.
async function printStickersSilently(segments, formPrinter, jobRef, why) {
  if (!window.QZPrint || !window.qzTryPrint || !window.qzTryConnect || !await window.qzTryConnect()) { why.reason = 'PrintBridge is not reachable.'; return false; }
  const r = await window.loadViewModule('./print-routing.js');
  const location = (window.erpWorkingStore || {}).code || '';
  const printer = await r.chosenPrinter('Sticker', location) || await r.choosePrinter('Sticker', location);
  if (!printer) { Object.assign(why, { setup: true, reason: 'No sticker printer is chosen for this computer. Retry to choose one.' }); return false; }
  if (await printThermal(segments, printer, jobRef, { ask: false })) return true;
  const dpi = Number(printer.dpi) || Number(formPrinter && formPrinter.dpi) || 0;
  for (const dataBase64 of await stickerPDFs(tuned(segments, printer), dpi)) {
    if (!await window.qzTryPrint('Document', { documentRef: jobRef || 'Stickers', dataBase64, docFormat: 'pdf',
      printKind: 'Sticker', printerCode: printer.code, location, quiet: true, why })) return false;
  }
  return true;
}

async function printSheet(segments, dpi) {
  const area = document.getElementById('sticker-print-area');
  if (!area) return;
  const pngCache = new Map();
  const png = (t, label) => {
    let perT = pngCache.get(t);
    if (!perT) { perT = new Map(); pngCache.set(t, perT); }
    if (perT.has(label)) return perT.get(label);
    const url = labelDots(t, label, dpi).canvas.toDataURL('image/png');
    perT.set(label, url);
    return url;
  };

  let css = '@media print { html, body { margin: 0 !important; padding: 0 !important; background: #fff !important; } }\n';
  segments.forEach((seg, si) => {
    const t = seg.t, pageName = `stk${si}`;
    const [mw, ph] = t.settings.sheet.mode === 'a4' ? [210, 297] : [mediaWidthMM(t), printHeightMM(t)];
    css += `${si === 0 ? `@page { size: ${mw}mm ${ph}mm; margin: 0; }\n` : ''}@page ${pageName} { size: ${mw}mm ${ph}mm; margin: 0; }\n`;
  });
  let html = '';
  for (const pg of sheetPages(segments)) {
    const cell = cellSize(pg.t);
    html += `<div class="stk-page" style="page:stk${pg.si}; width:${pg.w}mm; height:${pg.h}mm;${pg.flip ? ' transform: rotate(180deg);' : ''}">`;
    pg.cells.forEach(c => {
      html += `<img src="${png(pg.t, c.label)}" style="left:${c.x}mm; top:${c.y}mm; width:${cell.w}mm; height:${cell.h}mm;${pg.a4 ? '' : ' image-rendering: pixelated;'}" alt="">`;
    });
    html += '</div>';
  }

  const style = document.createElement('style');
  style.id = 'sticker-print-page-style';
  style.textContent = css;
  document.getElementById('sticker-print-page-style')?.remove();
  document.head.appendChild(style);
  area.innerHTML = html;
  await Promise.all(Array.from(area.querySelectorAll('img')).map(img => (img.decode ? img.decode().catch(() => {}) : null)));
  area.classList.add('printing');
  await stickerStylesReady; // .stk-page lives in sticker-studio.css
  window.print();
  setTimeout(() => {
    area.classList.remove('printing');
    area.innerHTML = '';
    style.remove();
  }, 500);
}

// printStickersNoDialog (Stage 127.2): the form's thermal printer first, then
// this computer's Sticker printer; the browser sheet only when this computer
// is not set up for silent stickers, or when the user picks it after a failure.
async function printStickersNoDialog(segments, printer, jobRef) {
  if (await printThermal(segments, printer, jobRef)) return;
  const dpi = Number(printer && printer.dpi) || 0;
  const r = window.loadViewModule ? await window.loadViewModule('./print-routing.js') : null;
  if (!r) { await printSheet(tuned(segments, printer), dpi); return; }
  await r.printNoDialog('Sticker', (window.erpWorkingStore || {}).code || '',
    (why) => printStickersSilently(segments, printer, jobRef, why), () => printSheet(tuned(segments, printer), dpi));
}

// Entry point for every sticker print in the app. `labels` is what
// POST /api/v1/stickers/print returned (already validated and written to the
// sticker print log); this only decides how the ink reaches the paper.
export async function printStickerLabels(labels, { printer, copies = 1, apiFetch, jobRef = '' } = {}) {
  if (!labels || labels.length === 0) return;
  defaultTemplate();
  const templates = await loadTemplates(labels, apiFetch || window.apiFetch);
  const segments = expandRuns(labels, templates, copies);
  await printStickersNoDialog(segments, printer, jobRef);
}

// One design, one print - the studio's Test Print (no print-log row: nothing
// real is being labelled).
export async function printTestLabel(t, label, printer) {
  await ensureTemplateAssets(t);
  const segments = [{ t, items: Array(t.settings.per_row * t.settings.per_col).fill(label) }];
  await printStickersNoDialog(segments, printer, 'Test print');
}

// Stage 137.3: Test alignment - on every label of the roll, a box 0.5 mm in
// from each edge, a cross through the middle and the shift in use. Even gaps
// all round mean the printer is aligned; otherwise the gap says which way to
// shift. Printed like a sticker, with the printer's preset; nothing logged.
// preset is the printer record with the values being tried, saved or not.
export async function printAlignmentTest(roll, preset) {
  const t = Object.assign({}, roll || defaultTemplate(), { assets: {} });
  const { w, h } = t;
  const shift = `R ${Number(preset.shift_right_mm) || 0}  D ${Number(preset.shift_down_mm) || 0} mm`;
  t.elements = [
    { kind: 'box', x_mm: 0.5, y_mm: 0.5, w_mm: w - 1, h_mm: h - 1, thickness_mm: 0.3 },
    { kind: 'line', x_mm: 0, y_mm: h / 2 - 0.15, w_mm: w, h_mm: 0.3, thickness_mm: 0.3 },
    { kind: 'line', x_mm: w / 2 - 0.15, y_mm: 0, w_mm: 0.3, h_mm: h, thickness_mm: 0.3 },
    { kind: 'text', field: 'custom', text: shift, x_mm: 1.2, y_mm: 1.2, w_mm: w / 2 - 2, h_mm: Math.min(4, h / 2 - 2), font_size_mm: 2.2, fit: 'shrink' }
  ].map(normaliseElement);
  await printTestLabel(t, { sku: '', name: '' }, preset);
}

// Stage 137.3: Calibrate roll - the printer feeds a few labels and measures
// the label and the gap (TSPL GAPDETECT, ZPL ~JC), so each print starts where
// the template expects. Needed after loading a different roll.
export async function calibrateRoll(printer) {
  const lang = String((printer && printer.printer_language) || '').toUpperCase();
  const data = { TSPL: 'GAPDETECT\r\n', ZPL: '~JC' }[lang];
  if (!data) throw new Error('Calibrate needs a label printer whose Printer Language is TSPL or ZPL.');
  if (!printer.qz_printer_name) throw new Error('This printer has no OS Printer Name.');
  if (!window.QZPrint || !await window.qzTryConnect()) throw new Error('PrintBridge is not running on this computer.');
  await window.QZPrint.printItems(printer.qz_printer_name, [{ type: 'raw', format: 'command', flavor: 'plain', data }], 1);
}

// Draws a template thumbnail/preview into an existing canvas at `scale` px/mm
// (device-pixel-ratio aware). With mono, renders at printer DPI as the
// printer would and scales up without smoothing - the "thermal view".
// asPrinted draws the label's cell as it comes off the roll (turned for a
// vertical template) instead of the design as it reads.
export function paintPreview(canvas, t, label, { scale, mono = false, dpi, asPrinted = false } = {}) {
  const ratio = window.devicePixelRatio || 1;
  const box = asPrinted ? cellSize(t) : { w: t.w, h: t.h };
  const draw = asPrinted ? drawCell : drawLabel;
  const cssW = box.w * scale, cssH = box.h * scale;
  canvas.style.width = cssW + 'px';
  canvas.style.height = cssH + 'px';
  canvas.width = Math.max(1, Math.round(cssW * ratio));
  canvas.height = Math.max(1, Math.round(cssH * ratio));
  const ctx = canvas.getContext('2d');
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const pdpi = dpi || t.settings.dpi || 203;
  if (mono) {
    const dpm = pdpi / MM_PER_INCH;
    const w = Math.round(box.w * dpm), h = Math.round(box.h * dpm);
    const off = newCanvas(w, h);
    const octx = off.getContext('2d', { willReadFrequently: true });
    octx.fillStyle = '#fff'; octx.fillRect(0, 0, w, h);
    draw(octx, t, label, { scale: dpm, dpi: pdpi, snap: true, mono: true });
    const bmp = canvasToBits(octx, w, h);
    const img = octx.createImageData(w, h);
    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        const dark = bmp.bits[y * bmp.bytesPerRow + (x >> 3)] & (0x80 >> (x & 7));
        const i = (y * w + x) * 4;
        img.data[i] = img.data[i + 1] = img.data[i + 2] = dark ? 17 : 255;
        img.data[i + 3] = 255;
      }
    }
    octx.putImageData(img, 0, 0);
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(off, 0, 0, canvas.width, canvas.height);
    return;
  }
  ctx.scale(ratio, ratio);
  draw(ctx, t, label, { scale, dpi: pdpi, snap: false, mono: false });
}

// Stage 119: one whole print as it comes off the roll - every cell with
// `label`, on a faint liner - for the template cards and the studio's
// One print view, so horizontal / vertical and × 1 / × 3 read at a glance.
export function paintPrint(canvas, t, label, { scale } = {}) {
  const ratio = window.devicePixelRatio || 1;
  const s = t.settings, c = cellSize(t);
  const w = mediaWidthMM(t), h = printHeightMM(t);
  canvas.style.width = w * scale + 'px';
  canvas.style.height = h * scale + 'px';
  canvas.width = Math.max(1, Math.round(w * scale * ratio));
  canvas.height = Math.max(1, Math.round(h * scale * ratio));
  const ctx = canvas.getContext('2d');
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.fillStyle = '#dbe2ea';
  ctx.fillRect(0, 0, w * scale, h * scale);
  for (let k = 0; k < s.per_row * s.per_col; k++) {
    const o = cellOrigin(t, k);
    ctx.save();
    ctx.translate(o.x * scale, o.y * scale);
    ctx.fillStyle = '#fff';
    ctx.fillRect(0, 0, c.w * scale, c.h * scale);
    drawCell(ctx, t, label, { scale, dpi: s.dpi || 203, snap: false, mono: false });
    ctx.restore();
  }
}
