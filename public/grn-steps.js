// Stage 94: goods receipt in steps - the page one receipt is worked on.
//
//   ① Receive -> ② Verify -> ③ Price check -> ④ MRP and post -> ⑤ Pay
//
// Receive is the New goods receipt page (view-procurement.js); this module is
// everything after it, plus the inbox tabs on the list. Each step asks one
// thing, has one main button and a Back. The server (engines/grn_steps*.go)
// owns every figure: this page sends what was typed and draws what comes back.
// Buttons are wired here with data-gs (not the global data-act dispatcher),
// so nothing of this page lands on window.

const STEPS = [
  { key: 'receive', label: 'Receive' },
  { key: 'verify', label: 'Verify' },
  { key: 'price', label: 'Price check' },
  { key: 'post', label: 'MRP and post' },
  { key: 'pay', label: 'Pay' }
];
const GROUP_LABEL = { new: 'New item', same: 'Same price', changed: 'Price changed' };
const CHILD_GROUP = { A: 'new', B: 'same', C: 'changed' };

let gs = null; // { id, view, page, edit, mrp, moves, names, root, onChanged }

const esc = (s) => escapeHTMLText(s == null ? '' : String(s));
const money = (v) => formatMoney(Number(v) || 0);
// Stage 94.15: an MRP is a whole rupee, shown without paise.
const mrpText = (v) => { const n = Number(v) || 0; return Number.isInteger(n) ? n.toLocaleString('en-IN') : money(n); };
const num = (v) => (v === '' || v == null || isNaN(Number(v)) ? null : Number(v));
// Stage 110.1: a line of a GST-inclusive PO shows and takes its prices incl.
// GST, like its PO; the server keeps them ex-GST. A price typed back to what
// was shown keeps its exact ex-GST value, so no paisa of difference appears.
const PRICE_FIELDS = ['invoice_price', 'grn_price'];
const gstK = (l) => 1 + (Number(l.gst_rate) || 0) / 100;
const shownPrice = (l, ex) => (l.gst_inclusive && num(ex) !== null ? Math.round(num(ex) * gstK(l) * 100) / 100 : ex);
const inclTag = (l) => (l.gst_inclusive ? '<div class="text-muted">incl. GST</div>' : '');

// The page a receipt opens on: the step it is waiting for.
function pageFor(view) {
  switch (view.step) {
    case 'Received': return 'verify';
    case 'Verified': return 'price';
    case 'Priced': return 'post';
    default: return 'pay';
  }
}

export async function openGRNSteps(grnId, root, opts = {}) {
  gs = { id: grnId, root, onChanged: opts.onChanged || (() => {}), edit: {}, mrp: {}, moves: {}, names: {} };
  root.innerHTML = '<p class="txn-empty">Loading the goods receipt...</p>';
  // Stage 74.1.1: the reason codes for rejected and damaged units.
  try {
    gs.rp = await import('./reason-picker.js' + new URL(import.meta.url).search);
    gs.reasons = { rej: await gs.rp.loadReasons('Goods Receipt Rejection'), dmg: await gs.rp.loadReasons('Goods Receipt Damage') };
  } catch (_) { gs.rp = null; }
  if (!root.dataset.gsWired) {
    root.dataset.gsWired = '1';
    root.addEventListener('click', onClick);
    root.addEventListener('input', onInput);
    root.addEventListener('change', onInput);
  }
  await reload(opts.page);
}

async function reload(page) {
  const res = await apiFetch(`/api/v1/grn-steps/${encodeURIComponent(gs.id)}`);
  if (!res) return;
  if (!res.ok) {
    gs.root.innerHTML = `<div class="login-error">${esc(await getErrorMessage(res, 'Could not load this goods receipt.'))}</div>`;
    return;
  }
  setView(await res.json(), page);
}

function setView(view, page) {
  gs.view = view;
  gs.page = page || pageFor(view);
  gs.edit = {};
  gs.mrp = {};
  gs.moves = {};
  gs.names = {};
  (view.lines || []).forEach(l => {
    gs.edit[l.line_no] = {
      grn_qty: l.grn_qty, grn_price: l.grn_price, rejected_qty: l.rejected_qty, rejection_reason: l.rejection_reason || '',
      damaged_qty: l.damaged_qty, damage_reason: l.damage_reason || '', excess_action: l.excess_action || '',
      rejection_reason_code: l.rejection_reason_code || '', damage_reason_code: l.damage_reason_code || '',
      received_qty: l.received_qty, invoice_qty: l.invoice_qty, invoice_price: l.invoice_price
    };
    gs.mrp[l.line_no] = l.mrp || l.current_mrp || '';
  });
  render();
}

// --- drawing -------------------------------------------------------------------

function render() {
  const v = gs.view;
  const title = document.querySelector('#grn-screen .txn-page-only .txn-title');
  if (title) title.textContent = v.code;
  gs.root.innerHTML = `
    <div class="gs-page">
      ${stepperHTML(v)}
      <div class="gs-facts">
        <span><b>${esc(v.vendor_name || v.vendor)}</b></span>
        <span>Invoice <b>${esc(v.vendor_invoice_no)}</b> of ${esc(v.vendor_invoice_date)}</span>
        <span>Store <b>${esc(v.location)}</b></span>
        <span>PO ${(v.po_ids || []).map(esc).join(', ') || '&mdash;'}</span>
        <span class="gs-status">${esc(v.status)}${v.payment_status ? ' &middot; ' + esc(v.payment_status) : ''}</span>
        <button type="button" class="btn btn-outline btn-sm" data-gs="print">Print</button>
      </div>
      <div id="gs-totals" class="gs-totals">${totalsHTML(v)}</div>
      ${(v.warnings || []).length ? `<div class="txn-hint">${v.warnings.map(esc).join(' ')}</div>` : ''}
      <div id="gs-error" class="login-error hidden"></div>
      <div class="gs-body">${bodyHTML(v)}</div>
      <div id="gs-files"></div>
      ${changesHTML(v)}
    </div>`;
  // Stage 74.1.6: the vendor's invoice copy, attached while the receipt is
  // worked (the server allows it until posting) and shown afterwards.
  import('./attachments-panel.js' + new URL(import.meta.url).search)
    .then(m => m.renderAttachmentsPanel(document.getElementById('gs-files'), 'GRN', gs.id, { title: "Vendor's invoice copy", empty: 'No copy of the invoice attached.' }))
    .catch(() => {});
}

function stepperHTML(v) {
  const current = STEPS.findIndex(s => s.key === gs.page);
  const done = STEPS.findIndex(s => s.key === pageFor(v));
  const started = (v.children || []).some(c => c.status === 'Posted');
  return `<ol class="gs-stepper" aria-label="Goods receipt steps">${STEPS.map((s, i) => {
    const state = i < done ? 'done' : (i === done ? 'now' : 'next');
    // Before posting every step can be gone back to; once a child has
    // posted, only Correct changes the receipt.
    const reachable = i >= 1 && i <= done && !(started && i < 3) && !(v.step === 'Posted' && i < 4);
    return `<li class="gs-step gs-${state}${i === current ? ' gs-viewing' : ''}">${reachable && i !== current
      ? `<button type="button" class="link-btn" data-gs="goto" data-page="${s.key}">${i + 1}. ${s.label}</button>`
      : `<span>${i + 1}. ${s.label}</span>`}${i < done ? ' <span aria-label="done">&#10003;</span>' : ''}</li>`;
  }).join('')}${v.step === 'Posted' ? '<li class="gs-step gs-done"><span>Posted</span> <span aria-label="done">&#10003;</span></li>' : ''}</ol>`;
}

// The totals bar: what the vendor billed, what we received and verified, and
// the note the difference becomes (D1). On Verify it follows the inputs.
function totalsHTML(v) {
  let ours = v.our_total;
  if (gs.page === 'verify' && v.step !== 'Posted') {
    ours = 0;
    (v.lines || []).forEach(l => {
      const e = gs.edit[l.line_no] || {};
      ours += keptQty(l, e) * (num(e.grn_price) ?? 0) * (1 + (Number(l.gst_rate) || 0) / 100);
    });
    ours = Math.round(ours * 100) / 100;
  }
  const diff = Math.round((v.vendor_invoice_amount - ours) * 100) / 100;
  const raised = v.step === 'Posted' ? 'raised' : 'will be raised';
  // Stage 110.3: while the bill does not add up, Verify waits - no note yet.
  const note = v.invoice_check ? '' : Math.abs(diff) < 0.005
    ? '<span class="gs-ok">Invoice and goods agree - no note</span>'
    : diff > 0
      ? `<span class="gs-amber">Debit note ${money(diff)} ${raised} (they billed more)</span>`
      : `<span class="gs-amber">Credit note ${money(-diff)} ${raised} (they billed less)</span>`;
  const unexplained = Math.abs(v.invoice_unexplained) > 1
    ? `<span class="gs-red" title="The bill's total minus its items - fix it on Receive">Bill total vs its items: ${money(v.invoice_unexplained)}</span>`
    : (Math.abs(v.invoice_unexplained) >= 0.005 ? `<span>Round-off ${money(v.invoice_unexplained)}</span>` : '');
  return `
    <span>Invoice total <b>${money(v.vendor_invoice_amount)}</b></span>
    <span>Invoice lines <b>${money(v.invoice_lines_total)}</b></span>
    <span>Received and verified <b>${money(ours)}</b></span>
    ${unexplained}
    ${note}
    ${v.step === 'Posted' ? `<span>Due <b>${money(v.net_payable)}</b></span>` : ''}`;
}

// beyondPO is a line's qty beyond what its PO has open (all of it when the
// item is not on the PO) - a preview; the server applies the tolerance.
function beyondPO(l, e) {
  const acc = Math.max(0, (num(e.grn_qty) ?? 0) - (num(e.rejected_qty) ?? 0) - (num(e.damaged_qty) ?? 0));
  const open = l.po_open_qty;
  return Math.max(0, open == null ? acc : Math.round((acc - open) * 1000) / 1000);
}

// keptQty is what we keep and owe for: in hand, less rejected and damaged,
// less goods beyond the PO sent back.
function keptQty(l, e) {
  const acc = Math.max(0, (num(e.grn_qty) ?? 0) - (num(e.rejected_qty) ?? 0) - (num(e.damaged_qty) ?? 0));
  return e.excess_action === 'return' ? Math.max(0, acc - beyondPO(l, e)) : acc;
}

function bodyHTML(v) {
  if (gs.page === 'receive') return receiveHTML(v);
  if (gs.page === 'verify') return verifyHTML(v);
  if (gs.page === 'price') return priceHTML(v);
  if (gs.page === 'post') return postHTML(v);
  return payHTML(v);
}

function actionsHTML(back, main, extra = '') {
  return `<div class="gs-actions">${back || ''}<span class="gs-spacer"></span>${extra}${main || ''}</div>`;
}

const canDo = (k) => !!(gs.view.allowed && gs.view.allowed[k]);
const notAllowed = (k) => canDo(k) ? '' : `<span class="text-muted">Your role does not do this step.</span>`;

// ① Receive, edited after saving (Back).
function receiveHTML(v) {
  const posted = (v.children || []).some(c => c.status === 'Posted');
  const c = v.invoice_check;
  const fix = (lineNo, field) => c && c.line_no === lineNo && c.field === field ? 'data-fix="1"' : '';
  return `
    ${invoiceCheckHTML(c, false)}
    <div class="gs-form-row">
      <div class="form-group"><label class="form-label" for="gs-inv-no">Vendor invoice no</label><input id="gs-inv-no" class="form-input" value="${esc(v.vendor_invoice_no)}"></div>
      <div class="form-group"><label class="form-label" for="gs-inv-date">Invoice date</label><input id="gs-inv-date" type="date" class="form-input" value="${esc(v.vendor_invoice_date)}"></div>
      <div class="form-group"><label class="form-label" for="gs-inv-total">Invoice total (incl. GST)</label><input id="gs-inv-total" type="number" step="0.01" min="0" class="form-input num${c ? ' gs-fix' : ''}" value="${esc(v.vendor_invoice_amount)}"></div>
    </div>
    <div class="txn-grid"><table>
      <thead><tr><th>#</th><th>Item</th><th class="num">Received</th><th class="num">Invoice qty</th><th class="num" title="Price per unit from the vendor's invoice - before GST, or incl. GST where the line says so">Invoice price</th><th class="num">GST %</th></tr></thead>
      <tbody>${v.lines.map(l => `<tr>
        <td>${l.line_no}</td><td>${itemCell(l)}</td>
        <td class="num">${numInput(l.line_no, 'received_qty', gs.edit[l.line_no].received_qty)}</td>
        <td class="num">${numInput(l.line_no, 'invoice_qty', gs.edit[l.line_no].invoice_qty, 'any', fix(l.line_no, 'invoice_qty'))}</td>
        <td class="num">${numInput(l.line_no, 'invoice_price', priceShown(l, 'invoice_price'), '0.01', fix(l.line_no, 'invoice_price'))}${inclTag(l)}</td>
        <td class="num">${esc(l.gst_rate)}</td></tr>`).join('')}</tbody>
    </table></div>
    ${actionsHTML(`<button type="button" class="btn btn-outline" data-gs="goto" data-page="${pageFor(v)}">Cancel</button>`,
      posted ? '<span class="text-muted">Posting has started - use Correct on step 5.</span>' : `<button type="button" class="btn btn-primary" data-gs="save-receive">Save and verify again</button>`)}`;
}

// Stage 110.3: why the bill's total and its items do not add up, in plain
// words, with the way to fix it. On Verify it offers the jump to Receive; on
// Receive the boxes to check are highlighted.
function invoiceCheckHTML(c, onVerify) {
  if (!c) return '';
  return `<div class="page-banner page-banner-warning gs-invoice-check" role="alert">
    <div class="page-banner-body"><b>The bill does not add up.</b> <span>${esc(c.message)}</span>
      ${onVerify ? '' : '<span>The boxes to check are highlighted. Fix them, then click <b>Save and verify again</b>.</span>'}</div>
    ${onVerify ? '<button type="button" class="btn btn-primary btn-sm" data-gs="goto" data-page="receive">Fix it on Receive</button>' : ''}
  </div>`;
}

function priceShown(l, field) {
  const e = gs.edit[l.line_no];
  return e[field + '_shown'] !== undefined ? e[field + '_shown'] : shownPrice(l, e[field]);
}

function itemCell(l) {
  return `<div class="gs-item"><b>${esc(l.item_name || l.sku)}</b>${l.item_name ? `<div class="text-muted">${esc(l.sku)}${l.batch_no ? ' &middot; batch ' + esc(l.batch_no) : ''}</div>` : ''}${l.gst_warning ? `<div class="gs-amber">${esc(l.gst_warning)}</div>` : ''}</div>`;
}

function numInput(lineNo, field, value, step = 'any', extra = '') {
  return `<input type="number" min="0" step="${step}" class="form-input num gs-num" data-line="${lineNo}" data-field="${field}" value="${esc(value ?? '')}" aria-label="${esc(field.replace(/_/g, ' '))} line ${lineNo}" ${extra}>`;
}

// reasonCell is a set-aside quantity's reason: a code from the list
// (74.1.1) and an optional note.
function reasonCell(lineNo, kind, field, code, text, placeholder) {
  const select = gs.rp && gs.reasons
    ? gs.rp.reasonSelectHTML(gs.reasons[kind], { value: code, attrs: `class="form-select gs-text" data-line="${lineNo}" data-field="${field}_code"`, label: `${placeholder}, line ${lineNo}` })
    : '';
  return select + textInput(lineNo, field, text, select ? 'note (optional)' : placeholder);
}

function textInput(lineNo, field, value, placeholder) {
  return `<input type="text" class="form-input gs-text" data-line="${lineNo}" data-field="${field}" value="${esc(value ?? '')}" placeholder="${esc(placeholder)}" aria-label="${esc(field.replace(/_/g, ' '))} line ${lineNo}">`;
}

// ② Verify: what is really in hand, at what price.
function verifyHTML(v) {
  const rows = v.lines.map(l => {
    const e = gs.edit[l.line_no];
    const open = l.po_open_qty;
    const beyond = beyondPO(l, e);
    const dq = (num(e.grn_qty) ?? 0) - l.invoice_qty;
    const grnShown = num(priceShown(l, 'grn_price')) ?? 0;
    const dp = grnShown - shownPrice(l, l.invoice_price);
    const vsPO = l.po_rate == null ? '' : (grnShown - shownPrice(l, l.po_rate));
    return `<tr>
      <td>${l.line_no}</td><td>${itemCell(l)}</td>
      <td class="num">${open == null ? '<span class="gs-amber">not on PO</span>' : open}</td>
      <td class="num">${l.received_qty}</td>
      <td class="num">${l.invoice_qty}<div class="text-muted">${money(shownPrice(l, l.invoice_price))}${l.gst_inclusive ? ' incl. GST' : ''}</div></td>
      <td class="num">${numInput(l.line_no, 'grn_qty', e.grn_qty)}${Math.abs(dq) > 1e-9 ? `<div class="gs-amber">${dq > 0 ? '+' : ''}${dq} vs invoice</div>` : ''}</td>
      <td class="num">${numInput(l.line_no, 'grn_price', priceShown(l, 'grn_price'), '0.01')}${inclTag(l)}${Math.abs(dp) >= 0.005 ? `<div class="gs-amber">${dp > 0 ? '+' : ''}${money(dp)} vs invoice</div>` : ''}${vsPO !== '' && Math.abs(vsPO) >= 0.005 ? `<div class="text-muted">${vsPO > 0 ? '+' : ''}${money(vsPO)} vs PO</div>` : ''}</td>
      <td>${numInput(l.line_no, 'rejected_qty', e.rejected_qty)}${(num(e.rejected_qty) ?? 0) > 0 ? reasonCell(l.line_no, 'rej', 'rejection_reason', e.rejection_reason_code, e.rejection_reason, 'why rejected') : ''}</td>
      <td>${numInput(l.line_no, 'damaged_qty', e.damaged_qty)}${(num(e.damaged_qty) ?? 0) > 0 ? reasonCell(l.line_no, 'dmg', 'damage_reason', e.damage_reason_code, e.damage_reason, 'what is damaged') : ''}</td>
      <td>${beyond > 1e-9 ? `<select class="form-select gs-text" data-line="${l.line_no}" data-field="excess_action" aria-label="Goods beyond the PO, line ${l.line_no}">
          <option value="">${beyond} beyond the PO - choose</option>
          <option value="keep"${e.excess_action === 'keep' ? ' selected' : ''}>Keep (asks for approval)</option>
          <option value="return"${e.excess_action === 'return' ? ' selected' : ''}>Return to vendor</option>
        </select>${l.excess_status === 'Pending' ? '<div class="gs-amber">Waiting for approval</div>' : ''}${l.excess_status === 'Approved' ? '<div class="gs-ok">Keep approved</div>' : ''}` : '<span class="text-muted">&mdash;</span>'}</td>
    </tr>`;
  }).join('');
  const quick = v.quick_path && canDo('verify') && canDo('price') && canDo('post')
    ? `<button type="button" class="btn btn-outline" data-gs="post-all" title="Everything matches - run every step now">Post all</button>` : '';
  return `
    ${invoiceCheckHTML(v.invoice_check, true)}
    <div class="txn-grid"><table>
      <thead><tr><th>#</th><th>Item</th><th class="num">PO open</th><th class="num">Received</th><th class="num">Invoice</th>
        <th class="num">GRN qty<span class="required">*</span></th><th class="num">GRN price<span class="required">*</span></th>
        <th>Rejected</th><th>Damaged</th><th>Beyond the PO</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
    ${v.lines.some(l => beyondPO(l, gs.edit[l.line_no]) > 1e-9) ? `<div class="gs-form-row"><div class="form-group gs-wide"><label class="form-label" for="gs-excess-reason">Why keep goods beyond the PO (for the approver)</label><input id="gs-excess-reason" class="form-input"></div></div>` : ''}
    ${actionsHTML(`<button type="button" class="btn btn-outline" data-gs="goto" data-page="receive">&lsaquo; Back to Receive</button>`,
      canDo('verify') ? `<button type="button" class="btn btn-primary" data-gs="verify">Verify &rsaquo;</button>` : notAllowed('verify'), quick)}`;
}

// ③ Price check: each line against the item's last purchase price.
function priceHTML(v) {
  const priced = v.step === 'Priced';
  const children = v.children || [];
  const nameInputs = priced ? `<div class="gs-form-row">${children.map(c => `
      <div class="form-group"><label class="form-label" for="gs-name-${c.key}">${esc(v.code)}/${c.key}</label>
      <input id="gs-name-${c.key}" class="form-input gs-name" data-group="${c.group}" value="${esc(gs.names[c.group] ?? c.name)}"></div>`).join('')}</div>` : '';
  const rows = v.lines.map(l => {
    const prev = l.prev_price == null ? '<span class="gs-amber">never bought</span>'
      : `${money(l.prev_price)}<div class="text-muted">${esc(l.prev_vendor || '')}${l.prev_date ? ' &middot; ' + esc(l.prev_date) : ''}</div>`;
    const pct = l.price_change_pct == null ? '' : `<span class="${l.price_change_pct > 0 ? 'gs-red' : (l.price_change_pct < 0 ? 'gs-ok' : '')}">${l.price_change_pct > 0 ? '&#9650; +' : (l.price_change_pct < 0 ? '&#9660; ' : '')}${l.price_change_pct}%</span>`;
    const group = priced
      ? `<select class="form-select gs-move" data-sku="${esc(l.sku)}" aria-label="Child for ${esc(l.sku)}">${Object.entries(GROUP_LABEL).map(([g, label]) =>
        `<option value="${g}"${(gs.moves[l.sku] || CHILD_GROUP[l.child]) === g ? ' selected' : ''}>${label}</option>`).join('')}</select>`
      : `<span class="badge badge-secondary">${l.prev_price == null ? GROUP_LABEL.new : (Math.abs((l.price_change_pct || 0)) > 0 ? GROUP_LABEL.changed : GROUP_LABEL.same)}</span>`;
    return `<tr><td>${l.line_no}</td><td>${itemCell(l)}</td><td class="num">${money(l.grn_price)}</td><td class="num">${prev}</td><td class="num">${pct}</td><td>${group}</td></tr>`;
  }).join('');
  return `
    ${nameInputs}
    <div class="txn-grid"><table>
      <thead><tr><th>#</th><th>Item</th><th class="num">GRN price</th><th class="num">Previous price</th><th class="num">Change</th><th>${priced ? 'Child GRN' : 'Goes to'}</th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
    ${actionsHTML(`<button type="button" class="btn btn-outline" data-gs="goto" data-page="verify">&lsaquo; Back to Verify</button>`,
      canDo('price') ? `<button type="button" class="btn btn-primary" data-gs="price">${priced ? 'Save and go to MRP' : 'Check prices'} &rsaquo;</button>` : notAllowed('price'),
      priced && canDo('price') ? `<button type="button" class="btn btn-outline" data-gs="regroup">Check prices again</button>` : '')}`;
}

// ④ MRP and posting, one child at a time.
function postHTML(v) {
  const children = v.children || [];
  const anyPosted = children.some(c => c.status === 'Posted');
  const cards = children.map(c => {
    const lines = v.lines.filter(l => l.child === c.key);
    const posted = c.status === 'Posted';
    const rows = lines.map(l => {
      const m = num(gs.mrp[l.line_no]);
      const exGST = m == null ? null : m / (1 + (Number(l.gst_rate) || 0) / 100);
      const margin = exGST ? Math.round((exGST - l.grn_price) / exGST * 1000) / 10 : null;
      const warn = m == null || m <= 0 ? (l.needs_mrp ? '<span class="gs-red">MRP needed</span>' : '')
        : (exGST < l.grn_price ? '<span class="gs-red">Below cost</span>' : (l.current_mrp && m < l.current_mrp ? '<span class="gs-amber">Below the current MRP</span>' : ''));
      return `<tr>
        <td>${itemCell(l)}</td><td class="num">${l.accepted_qty}</td><td class="num">${money(l.grn_price)}</td>
        <td class="num">${l.current_mrp ? mrpText(l.current_mrp) : '<span class="text-muted">none</span>'}</td>
        <td class="num">${posted ? mrpText(l.mrp) : numInput(l.line_no, 'mrp', gs.mrp[l.line_no], '1')}</td>
        <td class="num">${l.suggested_mrp ? mrpText(l.suggested_mrp) : ''}${l.suggested_mrp && l.suggested_mrp_rule ? `<div class="text-muted" style="font-size: 11px;" title="MRP rule (Setup &gt; MRP Rule)">${esc(l.suggested_mrp_rule)}</div>` : ''}</td>
        <td class="num">${margin == null ? '' : margin + '%'}</td><td>${posted ? '' : warn}</td></tr>`;
    }).join('');
    return `<section class="gs-child">
      <div class="gs-child-head"><h3>${esc(v.code)}/${c.key} &middot; ${esc(c.name)}</h3><span class="text-muted">${lines.length} line${lines.length === 1 ? '' : 's'}</span>
        ${posted ? `<span class="badge badge-success">Posted</span><button type="button" class="btn btn-outline btn-sm" data-gs="stickers" data-child="${c.key}">Print stickers</button>`
          : `<button type="button" class="btn btn-outline btn-sm" data-gs="suggested" data-child="${c.key}">Use suggested</button>`}</div>
      <div class="txn-grid"><table>
        <thead><tr><th>Item</th><th class="num">Qty</th><th class="num">Cost</th><th class="num">Current MRP</th><th class="num">MRP${posted ? '' : '<span class="required">*</span>'}</th><th class="num">Suggested</th><th class="num">Margin</th><th></th></tr></thead>
        <tbody>${rows}</tbody>
      </table></div>
      ${posted ? '' : `<div class="gs-actions"><span class="gs-spacer"></span>${canDo('post') ? `<button type="button" class="btn btn-primary" data-gs="post-child" data-child="${c.key}">Save and post ${esc(v.code)}/${c.key}</button>` : notAllowed('post')}</div>`}
    </section>`;
  }).join('');
  return `${v.excess_pending ? '<div class="txn-hint gs-amber">Keeping goods beyond the PO is waiting for approval (My work &rsaquo; Approvals); that child posts once it is approved.</div>' : ''}
    ${cards}
    ${anyPosted ? '' : actionsHTML(`<button type="button" class="btn btn-outline" data-gs="goto" data-page="price">&lsaquo; Back to Price check</button>`, '')}`;
}

// ⑤ Pay (optional), and Correct after posting.
function payHTML(v) {
  const today = new Date().toISOString().slice(0, 10);
  const notes = [
    v.debit_note_id ? `Debit note <button type="button" class="link-btn" data-gs="print-note" data-doctype="DebitNote" data-id="${esc(v.debit_note_id)}">${esc(v.debit_note_id)}</button>` : '',
    v.vendor_credit_note_id ? `Credit note <button type="button" class="link-btn" data-gs="print-note" data-doctype="VendorCreditNote" data-id="${esc(v.vendor_credit_note_id)}">${esc(v.vendor_credit_note_id)}</button>` : ''
  ].filter(Boolean).join(' &middot; ');
  const payments = (v.payments || []).length ? `<div class="txn-grid"><table>
      <thead><tr><th>Date</th><th>Paid by</th><th>Reference</th><th class="num">Amount</th><th class="num">TDS</th></tr></thead>
      <tbody>${v.payments.map(p => `<tr><td>${esc(p.date)}</td><td>${esc(p.mode)}</td><td>${esc(p.reference)}</td><td class="num">${money(p.amount)}</td><td class="num">${p.tds ? money(p.tds) : ''}</td></tr>`).join('')}</tbody>
    </table></div>` : '';
  const form = v.net_payable > 0.004 && canDo('pay') ? `
    <div class="gs-form-row">
      <div class="form-group"><label class="form-label" for="gs-pay-amount">Amount</label><input id="gs-pay-amount" type="number" step="0.01" min="0" class="form-input num" value="${esc(v.net_payable)}"></div>
      <div class="form-group"><label class="form-label" for="gs-pay-mode">Paid by</label><select id="gs-pay-mode" class="form-select">
        ${['Bank', 'UPI', 'Cheque', 'Cash'].map(m => `<option>${m}</option>`).join('')}</select></div>
      <div class="form-group"><label class="form-label" for="gs-pay-ref">UTR / cheque no</label><input id="gs-pay-ref" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="gs-pay-date">Date</label><input id="gs-pay-date" type="date" class="form-input" value="${today}"></div>
      ${v.vendor_tds_section ? `<label class="gs-check"><input type="checkbox" id="gs-pay-tds"> Withhold TDS (${esc(v.vendor_tds_section)}) - whole bill only</label>` : ''}
    </div>` : '';
  const correct = gs.correcting ? correctHTML(v) : '';
  return `
    <div class="gs-summary">
      <span>Vendor bill ${v.vendor_bill_id ? `<b title="${esc(v.vendor_bill_id)}">${esc(v.vendor_invoice_no)}</b> (${esc(v.vendor_bill_status || '')})` : '<span class="gs-amber">not made yet</span>'}</span>
      ${notes ? `<span>${notes}</span>` : ''}
      <span>Paid <b>${money(v.paid_amount)}</b></span>
      <span>Due <b>${money(v.net_payable)}</b></span>
    </div>
    ${payments}
    ${form}
    ${correct}
    ${actionsHTML(canDo('correct') && !gs.correcting ? `<button type="button" class="btn btn-outline" data-gs="correct-open">Correct</button>` : '',
      form ? `<button type="button" class="btn btn-primary" data-gs="pay">Pay</button>` : (v.net_payable > 0.004 ? notAllowed('pay') : '<span class="gs-ok">Nothing is due.</span>'),
      form ? `<button type="button" class="btn btn-outline" data-gs="leave">Leave it in Payables</button>` : '')}`;
}

function correctHTML(v) {
  const eff = gs.effect;
  return `<section class="gs-child">
    <div class="gs-child-head"><h3>Correct the posted receipt</h3><span class="text-muted">Only the difference is reversed and posted again</span></div>
    <div class="txn-grid"><table>
      <thead><tr><th>#</th><th>Item</th><th class="num">GRN qty</th><th class="num">GRN price</th><th class="num">MRP</th></tr></thead>
      <tbody>${v.lines.map(l => `<tr><td>${l.line_no}</td><td>${itemCell(l)}</td>
        <td class="num">${numInput(l.line_no, 'c_grn_qty', gs.edit[l.line_no].c_grn_qty ?? l.grn_qty)}</td>
        <td class="num">${numInput(l.line_no, 'c_grn_price', gs.edit[l.line_no].c_grn_price ?? l.grn_price, '0.01')}</td>
        <td class="num">${numInput(l.line_no, 'c_mrp', gs.edit[l.line_no].c_mrp ?? l.mrp, '1')}</td></tr>`).join('')}</tbody>
    </table></div>
    ${eff ? `<div class="gs-effect">
      ${(eff.lines || []).map(x => `<div>Line ${x.line_no} ${esc(x.sku)}: ${esc(x.field.replace(/_/g, ' '))} ${x.old} &rarr; ${x.new}${x.stock_delta ? `, stock ${x.stock_delta > 0 ? '+' : ''}${x.stock_delta}` : ''}${x.value_delta ? `, value ${x.value_delta > 0 ? '+' : ''}${money(x.value_delta)}` : ''}</div>`).join('') || '<div>Nothing changes.</div>'}
      ${eff.note_kind ? `<div>${eff.note_kind === 'DebitNote' ? 'Debit' : 'Credit'} note ${money(eff.note_amount)} will be raised.</div>` : ''}
      ${(eff.refusals || []).map(r => `<div class="gs-red">${esc(r)}</div>`).join('')}
    </div>` : ''}
    <div class="gs-actions"><button type="button" class="btn btn-outline" data-gs="correct-cancel">Cancel</button><span class="gs-spacer"></span>
      <button type="button" class="btn btn-outline" data-gs="correct-preview">Show the effect</button>
      ${eff && !(eff.refusals || []).length && (eff.lines || []).length ? `<button type="button" class="btn btn-primary" data-gs="correct-apply">Apply the correction</button>` : ''}</div>
  </section>`;
}

function changesHTML(v) {
  const rows = v.changes || [];
  if (!rows.length) return '';
  return `<details class="gs-log"><summary>Changes (${rows.length})</summary><div class="txn-grid"><table>
    <thead><tr><th>When</th><th>Step</th><th>Line</th><th>Field</th><th>Was</th><th>Now</th><th>Why</th><th>By</th></tr></thead>
    <tbody>${rows.map(c => `<tr><td>${esc(new Date(c.changed_at).toLocaleString())}</td><td>${esc(c.step)}</td><td>${c.line_no || ''} ${esc(c.sku || '')}</td>
      <td>${esc(c.field.replace(/_/g, ' '))}</td><td>${esc(c.old)}</td><td>${esc(c.new)}</td><td>${esc(c.reason || '')}</td><td>${esc(c.by || '')}</td></tr>`).join('')}</tbody>
  </table></div></details>`;
}

// --- events -----------------------------------------------------------------------

function onInput(e) {
  const t = e.target;
  if (t.classList.contains('gs-move')) { gs.moves[t.dataset.sku] = t.value; return; }
  if (t.classList.contains('gs-name')) { gs.names[t.dataset.group] = t.value; return; }
  const line = t.dataset.line;
  const field = t.dataset.field;
  if (!line || !field) return;
  if (field === 'mrp') {
    gs.mrp[line] = t.value;
    if (e.type === 'change') render();
    return;
  }
  const lv = PRICE_FIELDS.includes(field) && (gs.view.lines || []).find(x => String(x.line_no) === String(line));
  if (lv && lv.gst_inclusive) {
    const orig = field === 'grn_price' ? lv.grn_price : lv.invoice_price;
    const typed = num(t.value);
    gs.edit[line][field + '_shown'] = t.value;
    gs.edit[line][field] = typed === null ? '' : (Math.abs(typed - shownPrice(lv, orig)) < 0.005 ? orig : Math.round(typed / gstK(lv) * 100) / 100);
  } else {
    gs.edit[line][field] = t.value;
  }
  if (gs.page === 'verify') {
    const totals = document.getElementById('gs-totals');
    if (totals) totals.innerHTML = totalsHTML(gs.view);
    // A reason box appears once a rejected / damaged qty is typed.
    if (e.type === 'change' && (field === 'rejected_qty' || field === 'damaged_qty' || field === 'grn_qty')) render();
  }
}

function showError(msg) {
  const el = document.getElementById('gs-error');
  if (!el) return;
  el.textContent = msg;
  el.classList.remove('hidden');
  el.scrollIntoView({ block: 'nearest' });
}

async function send(path, body, okMsg) {
  const res = await apiFetch(`/api/v1/grn-steps/${encodeURIComponent(gs.id)}/${path}`, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return null;
  if (!res.ok) {
    let code = '';
    try { code = ((await res.clone().json()) || {}).code || ''; } catch (_) { /* not JSON */ }
    if (code === 'GOODSR-0942' && gs.view.invoice_check) {
      gs.page = 'receive';
      render();
      const first = gs.root.querySelector('[data-fix], .gs-fix');
      if (first) first.focus();
      return null;
    }
    showError(await getErrorMessage(res, 'That step could not be saved.'));
    return null;
  }
  const data = await res.json();
  if (okMsg) showToast(okMsg, { variant: 'success' });
  gs.onChanged();
  return data;
}

async function onClick(e) {
  const b = e.target.closest('[data-gs]');
  if (!b || !gs) return;
  const act = b.dataset.gs;
  const v = gs.view;
  if (b.disabled) return;
  b.disabled = true;
  try {
    switch (act) {
      case 'goto': gs.page = b.dataset.page; gs.correcting = false; render(); break;
      case 'print': window.printTransactionDocument && window.printTransactionDocument('GRN', gs.id); break;
      case 'print-note': window.printTransactionDocument && window.printTransactionDocument(b.dataset.doctype, b.dataset.id); break;
      case 'save-receive': {
        const lines = v.lines.map(l => ({ line_no: l.line_no, received_qty: num(gs.edit[l.line_no].received_qty), invoice_qty: num(gs.edit[l.line_no].invoice_qty), invoice_price: num(gs.edit[l.line_no].invoice_price) }));
        const data = await send('receive', {
          vendor_invoice_no: document.getElementById('gs-inv-no').value,
          vendor_invoice_date: document.getElementById('gs-inv-date').value,
          vendor_invoice_amount: num(document.getElementById('gs-inv-total').value) ?? 0,
          lines, reason: 'Receive corrected'
        }, 'Receive saved - verify it again.');
        if (data) setView(data, 'verify');
        break;
      }
      case 'verify': {
        const lines = v.lines.map(l => {
          const x = gs.edit[l.line_no];
          return { line_no: l.line_no, grn_qty: num(x.grn_qty), grn_price: num(x.grn_price), rejected_qty: num(x.rejected_qty) ?? 0, rejection_reason: x.rejection_reason || '',
            damaged_qty: num(x.damaged_qty) ?? 0, damage_reason: x.damage_reason || '', excess_action: x.excess_action || '',
            rejection_reason_code: x.rejection_reason_code || '', damage_reason_code: x.damage_reason_code || '' };
        });
        const data = await send('verify', { lines, excess_reason: (document.getElementById('gs-excess-reason') || {}).value || '' }, 'Verified.');
        if (data) setView(data, 'price');
        break;
      }
      case 'post-all': {
        if (!(await showCustomConfirm('Everything matches. Verify, price-check and post this receipt now? Stock goes in at the invoice price with the current MRPs.', 'Post all'))) break;
        const data = await send('post-all', {}, 'Posted.');
        if (data) setView(data, 'pay');
        break;
      }
      case 'price':
      case 'regroup': {
        const body = act === 'regroup' ? { regroup: true } : { moves: gs.moves, names: gs.names };
        const data = await send('price', body, act === 'regroup' ? 'Prices checked again.' : '');
        if (data) setView(data, act === 'regroup' ? 'price' : 'post');
        break;
      }
      case 'suggested':
        v.lines.filter(l => l.child === b.dataset.child && l.suggested_mrp).forEach(l => { gs.mrp[l.line_no] = l.suggested_mrp; });
        render();
        break;
      case 'post-child': {
        const key = b.dataset.child;
        const mrp = {};
        v.lines.filter(l => l.child === key).forEach(l => { const m = num(gs.mrp[l.line_no]); if (m != null) mrp[String(l.line_no)] = m; });
        const below = v.lines.filter(l => l.child === key && num(gs.mrp[l.line_no]) != null && num(gs.mrp[l.line_no]) / (1 + (Number(l.gst_rate) || 0) / 100) < l.grn_price);
        if (below.length && !(await showCustomConfirm(`The MRP of ${below.map(l => l.sku).join(', ')} is below its cost. Post anyway?`, 'MRP below cost'))) break;
        const data = await send(`post/${encodeURIComponent(key)}`, { mrp }, `${v.code}/${key} posted - stock is in.`);
        if (data) setView(data, data.step === 'Posted' ? 'pay' : 'post');
        break;
      }
      case 'stickers': {
        const skus = v.lines.filter(l => l.child === b.dataset.child).map(l => l.sku);
        try { sessionStorage.setItem('grn-child-stickers', JSON.stringify({ id: gs.id, skus })); } catch (_) { /* storage can be disabled */ }
        renderView('stickers');
        break;
      }
      case 'pay': {
        const mode = document.getElementById('gs-pay-mode').value;
        let ref = document.getElementById('gs-pay-ref').value.trim();
        if (!ref && mode === 'Cash') ref = `CASH-${Date.now()}`;
        if (!ref) { showError('Enter the UTR or cheque number.'); break; }
        const data = await send('pay', {
          amount: num(document.getElementById('gs-pay-amount').value) ?? 0, mode, reference: ref,
          date: document.getElementById('gs-pay-date').value, with_tds: !!(document.getElementById('gs-pay-tds') || {}).checked
        }, 'Payment recorded.');
        if (data) setView(data, 'pay');
        break;
      }
      case 'leave': renderView('grn'); break;
      case 'correct-open': gs.correcting = true; gs.effect = null; render(); break;
      case 'correct-cancel': gs.correcting = false; gs.effect = null; render(); break;
      case 'correct-preview':
      case 'correct-apply': {
        const lines = v.lines.map(l => {
          const x = gs.edit[l.line_no];
          const out = { line_no: l.line_no };
          if (x.c_grn_qty != null && num(x.c_grn_qty) !== l.grn_qty) out.grn_qty = num(x.c_grn_qty);
          if (x.c_grn_price != null && num(x.c_grn_price) !== l.grn_price) out.grn_price = num(x.c_grn_price);
          if (x.c_mrp != null && num(x.c_mrp) !== l.mrp) out.mrp = num(x.c_mrp);
          return out;
        }).filter(o => Object.keys(o).length > 1);
        let reason = '';
        if (act === 'correct-apply') {
          reason = await showCustomPrompt('Why is the posted receipt being corrected?', '', 'Correct the receipt');
          if (!reason || !reason.trim()) break;
        }
        const res = await apiFetch(`/api/v1/grn-steps/${encodeURIComponent(gs.id)}/correct`, { method: 'POST', body: JSON.stringify({ lines, reason, apply: act === 'correct-apply' }) });
        if (!res) break;
        if (!res.ok) { showError(await getErrorMessage(res, 'The correction could not be worked out.')); break; }
        const eff = await res.json();
        if (eff.applied) {
          showToast(`Corrected${eff.note_id ? ` - note ${eff.note_id} raised` : ''}.`, { variant: 'success' });
          gs.correcting = false;
          gs.onChanged();
          await reload('pay');
        } else {
          gs.effect = eff;
          render();
        }
        break;
      }
    }
  } finally {
    b.disabled = false;
  }
}

// --- the inbox tabs on the list ------------------------------------------------------

const TABS = [
  { key: 'verify', label: 'To verify' },
  { key: 'price', label: 'To price' },
  { key: 'pay', label: 'Ready to pay' },
  { key: 'done', label: 'Done' }
];

function ageText(h) {
  if (h < 1) return 'under an hour';
  if (h < 48) return `${Math.round(h)} h`;
  return `${Math.round(h / 24)} days`;
}

// renderGRNInbox draws the tabs - each with its count and the age of its
// oldest receipt - and that tab's receipts; onOpen(id) opens one.
export function renderGRNInbox(el, rows, onOpen, tab) {
  const current = tab || (TABS.find(t => rows.some(r => r.tab === t.key)) || TABS[0]).key;
  const counts = {};
  TABS.forEach(t => {
    const mine = rows.filter(r => r.tab === t.key);
    counts[t.key] = { n: mine.length, oldest: mine.reduce((m, r) => Math.max(m, r.age_hours), 0) };
  });
  const list = rows.filter(r => r.tab === current);
  el.innerHTML = `
    <div class="gs-tabs" role="tablist">${TABS.map(t => `<button type="button" role="tab" class="gs-tab${t.key === current ? ' active' : ''}" aria-selected="${t.key === current}" data-gs-tab="${t.key}">
      ${t.label} <span class="txn-count">${counts[t.key].n}</span>${t.key !== 'done' && counts[t.key].n ? `<span class="text-muted"> oldest ${ageText(counts[t.key].oldest)}</span>` : ''}</button>`).join('')}</div>
    <table><thead><tr><th>GRN</th><th>Vendor</th><th>Invoice</th><th class="num">Invoice total</th><th>Waiting</th><th></th></tr></thead>
    <tbody>${list.length ? list.map(r => `<tr>
      <td style="font-family: monospace;">${esc(r.code)}</td><td>${esc(r.vendor)}</td><td>${esc(r.vendor_invoice_no)}</td>
      <td class="num">${money(r.vendor_invoice_amount)}</td>
      <td>${esc(ageText(r.age_hours))}${r.excess_pending ? ' <span class="gs-amber">approval pending</span>' : ''}${r.payment_status ? ` &middot; ${esc(r.payment_status)}` : ''}</td>
      <td><button type="button" class="btn btn-outline btn-sm" data-gs-open="${esc(r.id)}">Open</button></td></tr>`).join('')
      : `<tr><td colspan="6" class="txn-empty">Nothing ${esc((TABS.find(t => t.key === current) || {}).label || '').toLowerCase()}.</td></tr>`}</tbody></table>`;
  el.querySelectorAll('[data-gs-tab]').forEach(btn => btn.addEventListener('click', () => renderGRNInbox(el, rows, onOpen, btn.dataset.gsTab)));
  el.querySelectorAll('[data-gs-open]').forEach(btn => btn.addEventListener('click', () => onOpen(btn.dataset.gsOpen)));
}
