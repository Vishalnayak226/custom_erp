// Stage 130.16 (74.3.3): the Job Work workbench - Manufacturing ▸
// Subcontracting. An order sends many lines to a job worker on a delivery
// challan (the goods stay ours, in the worker's custody), takes partial
// returns line by line - the same item back, or made into another item -
// books scrap and loss (above the client's tolerance only with approval) and
// the job worker's bill. Each line shows Sent | Accepted | Rejected | Loss |
// Still out, in qty and weight.
const Q = new URL(import.meta.url).search;
const esc = (s) => escapeHTMLText(String(s ?? ''));
const num = (v) => Number(v) || 0;
const fmt = (v) => num(v).toLocaleString();

async function getJSON(url, fallback) {
  const res = await apiFetch(url);
  if (!res || !res.ok) return fallback;
  try { return await res.json(); } catch (_) { return fallback; }
}

let jwLines = [];
let jwView = 'all';

const parse = (v) => { try { return typeof v === 'string' ? JSON.parse(v || '[]') : (v || []); } catch (_) { return []; } };
const open = (b) => b.outward - b.accepted - b.rejected - b.loss;
const openW = (b, i) => Math.round(((b.out_w || [])[i] - (b.back_w || [])[i] - (b.loss_w || [])[i]) * 100) / 100 || 0;

export async function renderJobWorkTab(container) {
  const { txnPagedSection, txnRestorePane, txnDone, sessionStore } = await import('./txn-screen.js' + Q);
  const docs = await getJSON('/api/v1/doc/SubcontractOrder?sort=recent&limit=200', null);
  if (!docs) { renderErrorPanel(container, 'Failed to load job work orders.', () => renderView('manufacturing:subcontracting')); return; }
  const today = new Date().toISOString().slice(0, 10);
  const soon = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
  const isOut = (d) => d.status === 'Sent' || d.status === 'Partly Received';
  const views = { all: () => true, send: d => d.status === 'Draft' || d.status === 'Challan Pending', out: isOut,
    exceptions: d => isOut(d) && ((d.due_date && d.due_date < today) || (d.statutory_due && d.statutory_due <= soon) || parse(d.jw_balance).some(b => b.pending_loss > 0)) };
  const shown = docs.filter(views[jwView] || views.all);
  const badge = { Sent: 'badge-warning', 'Partly Received': 'badge-warning', Received: 'badge-success', Draft: 'badge-secondary', 'Challan Pending': 'badge-warning', Cancelled: 'badge-danger' };
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.style.padding = '16px 20px';
  const lineRows = (d) => {
    const bal = parse(d.jw_balance);
    if (!bal.length) {
      return parse(d.lines).map(l => `<div>${fmt(l.qty)} ${esc(l.sku)}${l.output_sku && l.output_sku !== l.sku ? ` → ${esc(l.output_sku)}` : ''}</div>`).join('')
        || (d.sent_item_id ? `<div>${fmt(d.sent_qty)} ${esc(d.sent_item_id)} → ${esc(d.received_item_id)}</div>` : '');
    }
    // One line each: Sent | Accepted | Rejected | Loss | Still out, and the weight still out.
    return bal.map(b => `<div style="margin-bottom:4px;">${esc(b.sku)}${b.output_sku && b.output_sku !== b.sku ? ` → ${esc(b.output_sku)} <span class="text-muted">(${fmt(b.output_qty)} made)</span>` : ''}
      <div class="text-muted" style="font-size:12px;">Sent ${fmt(b.outward)} · accepted ${fmt(b.accepted)} · rejected ${fmt(b.rejected)} · loss ${fmt(b.loss)}${b.pending_loss ? ` <span class="badge badge-warning" title="Waiting for approval">+${fmt(b.pending_loss)}</span>` : ''}
        · <strong>still out ${fmt(open(b))}</strong>${(b.out_w || []).some(Boolean) ? ` · net / fine wt out ${openW(b, 1)} / ${openW(b, 2)}` : ''}</div></div>`).join('');
  };
  list.innerHTML = `<div style="margin-bottom:10px;"><select class="form-select" data-jw-view style="width:200px;" aria-label="Show">
      ${[['all', 'All orders'], ['send', 'To send'], ['out', 'With job workers'], ['exceptions', 'Exceptions']].map(([k, l]) => `<option value="${k}" ${k === jwView ? 'selected' : ''}>${l}</option>`).join('')}</select></div>
    <div class="table-wrapper"><table><thead><tr><th>Order</th><th>Job worker</th><th>Lines</th><th>Due back</th><th>Status</th><th></th></tr></thead>
    <tbody>${shown.length ? shown.map(d => {
      const late = isOut(d) && d.due_date && d.due_date < today;
      const statSoon = isOut(d) && d.statutory_due && d.statutory_due <= soon;
      const bal = parse(d.jw_balance);
      return `<tr><td style="font-family:monospace;">${esc(d.code || d.id)}${d.challan_number ? `<div class="text-muted" style="font-size:12px;">${esc(d.challan_number)}</div>` : ''}</td>
        <td>${esc(d.vendor_id)}<div class="text-muted" style="font-size:12px;">${esc(d.location)}</div></td>
        <td>${lineRows(d)}</td>
        <td>${esc(d.due_date || '')}${late ? ' <span class="badge badge-danger">overdue</span>' : ''}${d.statutory_due ? `<div class="text-muted" style="font-size:12px;" title="ITC-04: inputs back within a year, capital goods within three">Statutory: ${esc(d.statutory_due)}${statSoon ? ' ⚠' : ''}</div>` : ''}</td>
        <td><span class="badge ${badge[d.status] || 'badge-secondary'}">${esc(d.status)}</span>${num(d.processing_charges) ? `<div class="text-muted" style="font-size:12px;">Charges ${formatMoney(d.processing_charges)}</div>` : ''}</td>
        <td style="white-space:nowrap;">${d.status === 'Draft' || d.status === 'Challan Pending' ? `<button type="button" class="action-btn" data-jw-act="send" data-id="${esc(d.id)}">Send</button> ` : ''}
          ${isOut(d) && bal.length ? `<button type="button" class="action-btn" data-jw-act="receive" data-id="${esc(d.id)}">Receive</button> ` : ''}
          ${d.status === 'Sent' && !d.challan_id ? `<button type="button" class="action-btn" data-jw-act="legacy" data-id="${esc(d.id)}" title="Sent before job work lines: receive the output in one go">Receive</button> ` : ''}
          ${bal.some(b => b.accepted > 0) ? `<button type="button" class="action-btn" data-jw-act="charge" data-id="${esc(d.id)}">Book bill</button> ` : ''}
          ${d.challan_id ? `<button type="button" class="action-btn" data-jw-act="print" data-id="${esc(d.id)}">Print challan</button>` : ''}</td></tr>`;
    }).join('') : `<tr><td colspan="6" style="text-align:center;color:var(--text-muted);padding:20px;">${docs.length ? 'Nothing here in this view.' : 'No job work yet. + New Job Work Order starts one.'}</td></tr>`}</tbody></table></div>
    <div data-jw-pane style="margin-top:14px;"></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '20px';
  const field = (id, label, width, extra = '') => `<div class="form-group" style="margin:0;"><label class="form-label" for="${id}">${label}</label><input class="form-input" id="${id}" autocomplete="off" style="width:${width}px;" ${extra}></div>`;
  form.innerHTML = `<div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:14px;">
      ${field('jw-vendor', 'Job worker', 200)}${field('jw-store', 'From store', 150, `value="${esc(sessionStore())}"`)}${field('jw-due', 'Due back', 150, 'type="date"')}
      <div class="form-group" style="margin:0;"><label class="form-label" for="jw-capital">Capital goods</label><select class="form-select" id="jw-capital" style="width:110px;"><option>No</option><option>Yes</option></select></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item sent</th><th class="num">Qty</th><th>Lot</th><th class="num">Gross wt</th><th class="num">Net wt</th><th class="num">Fine wt</th><th>Purity</th><th>Comes back as</th><th class="num">Output per unit</th><th></th></tr></thead><tbody data-jw-lines></tbody></table></div>
    <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;">
      <input class="form-input" data-jw-add-input placeholder="Add an item by code, barcode or name" style="max-width:320px;" autocomplete="off">
    </div>
    <div class="login-error hidden" data-jw-error style="margin-top:12px;"></div>
    <div style="margin-top:16px;"><button type="button" class="btn btn-primary" data-jw-save>Save order</button></div>`;

  container.appendChild(txnPagedSection({ id: 'jw-screen', title: 'Job Work', page: 'New job work order', count: docs.length, newLabel: 'New Job Work Order', form, list }));
  txnRestorePane('jw-screen');
  attachLinkTypeahead(form.querySelector('#jw-vendor'), 'Vendor', { keepCode: true });
  attachLinkTypeahead(form.querySelector('#jw-store'), 'Location', { keepCode: true });

  const pane = list.querySelector('[data-jw-pane]');
  const post = async (url, body, okMsg) => {
    const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'That did not go through.'); return; }
    const outcome = await res.json().catch(() => ({}));
    showToast(outcome.order_status === 'Challan Pending' ? 'The challan waits for approval - send again once it is approved.' : okMsg, { variant: 'success' });
    renderView('manufacturing:subcontracting');
  };
  list.querySelector('[data-jw-view]').addEventListener('change', e => { jwView = e.target.value; renderView('manufacturing:subcontracting'); });
  list.querySelectorAll('[data-jw-act]').forEach(b => b.addEventListener('click', async () => {
    const d = docs.find(x => x.id === b.dataset.id);
    const act = b.dataset.jwAct;
    if (act === 'print') { (await import('./trade-docs.js' + Q)).printTransactionDocument('DeliveryChallan', d.challan_id); return; }
    if (act === 'legacy') {
      const qty = num(await showCustomPrompt(`How many ${d.received_item_id || 'finished units'} came back?`, String(d.expected_received_qty || ''), 'Receive', 'number'));
      if (qty > 0) await post('/api/v1/manufacturing/subcontract-order/receive', { id: d.id, qty }, 'Received.');
      return;
    }
    if (act === 'send') {
      pane.innerHTML = `<h3 style="margin:0 0 8px;">Send ${esc(d.code || d.id)}</h3>
        <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">${field('jws-vehicle', 'Vehicle no', 130)}${field('jws-lr', 'LR / AWB no', 130)}${field('jws-tr', 'Transporter', 160)}
          <button type="button" class="btn btn-primary" data-jws-go>Send on a delivery challan</button></div>`;
      pane.querySelector('[data-jws-go]').addEventListener('click', () => post('/api/v1/manufacturing/subcontract-order/send', { id: d.id, transport_mode: 'Road',
        vehicle_no: pane.querySelector('#jws-vehicle').value, lr_awb_no: pane.querySelector('#jws-lr').value, transporter: pane.querySelector('#jws-tr').value }, `${d.code || d.id} sent.`));
      return;
    }
    if (act === 'charge') {
      pane.innerHTML = `<h3 style="margin:0 0 8px;">Job worker's bill for ${esc(d.code || d.id)}</h3>
        <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">${field('jwc-bill', 'Bill no', 150)}${field('jwc-amt', 'Amount', 130, 'type="number" min="0" step="0.01"')}
          <button type="button" class="btn btn-primary" data-jwc-go>Book</button></div>
        <div class="txn-hint" style="margin-top:6px;">Booked to job-work conversion cost and spread over the lines by what was accepted.</div>`;
      pane.querySelector('[data-jwc-go]').addEventListener('click', () => post('/api/v1/manufacturing/subcontract-order/charge', { id: d.id,
        bill_no: pane.querySelector('#jwc-bill').value.trim(), amount: num(pane.querySelector('#jwc-amt').value) }, 'Bill booked.'));
      return;
    }
    const bal = parse(d.jw_balance).filter(x => open(x) - x.pending_loss > 0);
    const transforms = (x) => x.output_sku && x.output_sku !== x.sku;
    const inp = (k, i, w = 70, extra = '') => `<input type="number" min="0" step="1" class="form-input" style="width:${w}px;text-align:right;" data-jwr="${k}" data-i="${i}" placeholder="0" ${extra}>`;
    pane.innerHTML = `<h3 style="margin:0 0 8px;">What came back on ${esc(d.code || d.id)}</h3>
      <div class="table-wrapper"><table><thead><tr><th>Line</th><th class="num">Still out</th><th class="num">Accepted</th><th class="num">Rejected</th><th class="num">Scrap</th><th class="num">Loss</th><th class="num">Output made</th><th class="num">Output rejected</th><th class="num">Net wt back</th><th class="num">Fine wt back</th><th>QC / reason</th></tr></thead>
      <tbody>${bal.map((x, i) => `<tr><td>${esc(x.sku)}${transforms(x) ? ` → ${esc(x.output_sku)}` : ''}</td><td class="num">${fmt(open(x) - x.pending_loss)}</td>
        <td class="num">${inp('accepted_qty', i)}</td><td class="num">${inp('rejected_qty', i)}</td><td class="num">${inp('scrap_qty', i)}</td><td class="num">${inp('loss_qty', i)}</td>
        <td class="num">${transforms(x) ? inp('output_qty', i, 80, 'placeholder="auto"') : '-'}</td><td class="num">${transforms(x) ? inp('output_rejected_qty', i, 80, 'placeholder="auto"') : '-'}</td>
        <td class="num"><input type="number" min="0" step="0.001" class="form-input" style="width:80px;text-align:right;" data-jwr="net_weight" data-i="${i}"></td>
        <td class="num"><input type="number" min="0" step="0.001" class="form-input" style="width:80px;text-align:right;" data-jwr="fine_weight" data-i="${i}"></td>
        <td><input class="form-input" data-jwr="reason" data-i="${i}" style="min-width:140px;"></td></tr>`).join('')}</tbody></table></div>
      <div class="txn-hint" style="margin-top:6px;">Quantities are in the item sent. Rejected pieces go to QC hold, never to sellable stock. Scrap and loss over the allowed share wait for approval.</div>
      <div style="margin-top:10px;"><button type="button" class="btn btn-primary" data-jwr-go>Receive</button></div>`;
    pane.querySelector('[data-jwr-go]').addEventListener('click', () => {
      const v = (k, i) => pane.querySelector(`[data-jwr="${k}"][data-i="${i}"]`);
      const lines = bal.map((x, i) => {
        const l = { line_no: x.line_no, reason: v('reason', i).value.trim() };
        ['accepted_qty', 'rejected_qty', 'scrap_qty', 'loss_qty'].forEach(k => { l[k] = num(v(k, i).value); });
        ['net_weight', 'fine_weight'].forEach(k => { l[k] = num(v(k, i).value); });
        ['output_qty', 'output_rejected_qty'].forEach(k => { const el = v(k, i); if (el && el.value !== '') l[k] = num(el.value); });
        return l;
      }).filter(l => l.accepted_qty + l.rejected_qty + l.scrap_qty + l.loss_qty > 0);
      if (!lines.length) { showToast('Enter what came back.', { variant: 'warning' }); return; }
      post('/api/v1/manufacturing/subcontract-order/receive-lines', { id: d.id, key: `${d.id}:${Date.now()}`, lines }, 'Received.');
    });
  }));

  const errorEl = form.querySelector('[data-jw-error]');
  const input = form.querySelector('[data-jw-add-input]');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  const draw = () => {
    const cell = (k, i, w, type = 'number', step = 'any') => `<input type="${type}" ${type === 'number' ? `step="${step}" min="0"` : ''} class="form-input" style="width:${w}px;${type === 'number' ? 'text-align:right;' : ''}" data-jwl="${k}" data-i="${i}" value="${esc(jwLines[i][k] ?? '')}">`;
    form.querySelector('[data-jw-lines]').innerHTML = jwLines.length ? jwLines.map((l, i) => `<tr>
        <td>${esc(l.name || l.sku)}<div class="text-muted" style="font-size:12px;">${esc(l.sku)}</div></td>
        <td class="num">${cell('qty', i, 80, 'number', '1')}</td><td>${cell('batch_no', i, 90, 'text')}</td>
        <td class="num">${cell('gross_weight', i, 80)}</td><td class="num">${cell('net_weight', i, 80)}</td><td class="num">${cell('fine_weight', i, 80)}</td>
        <td>${cell('purity', i, 70, 'text')}</td><td><input class="form-input" data-jwl="output_sku" data-i="${i}" value="${esc(l.output_sku || '')}" style="width:150px;" placeholder="same item"></td>
        <td class="num">${cell('output_per_unit', i, 80)}</td>
        <td><button type="button" class="action-btn action-btn-danger" data-jw-remove="${i}">Remove</button></td></tr>`).join('')
      : '<tr><td colspan="10" style="text-align:center;color:var(--text-muted);padding:16px;">Add what goes to the job worker. Leave "Comes back as" blank when the same item comes back (polishing, repair); name the output item and how many come out of one unit when it is made into something else.</td></tr>';
    form.querySelectorAll('[data-jwl]').forEach(el => el.addEventListener('input', () => { jwLines[Number(el.dataset.i)][el.dataset.jwl] = el.value; }));
    form.querySelectorAll('[data-jwl="output_sku"]').forEach(el => attachLinkTypeahead(el, 'Item', { keepCode: true, noSetupHint: true, onPick: (doc) => { jwLines[Number(el.dataset.i)].output_sku = doc.code || doc.id; } }));
    form.querySelectorAll('[data-jw-remove]').forEach(el => el.addEventListener('click', () => { jwLines.splice(Number(el.dataset.jwRemove), 1); draw(); }));
  };
  attachLinkTypeahead(input, 'Item', { noSetupHint: true, onPick: (item) => {
    errorEl.classList.add('hidden');
    jwLines.push({ sku: item.code || item.id, name: item.name || '', qty: '' });
    input.value = '';
    input.dispatchEvent(new Event('input'));
    draw();
    form.querySelector(`[data-jwl="qty"][data-i="${jwLines.length - 1}"]`)?.focus();
  } });
  form.querySelector('[data-jw-save]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    errorEl.classList.add('hidden');
    const v = (id) => form.querySelector(id).value.trim();
    if (!v('#jw-vendor') || !v('#jw-store')) { fail('Choose the job worker and the store the goods leave from.'); return; }
    if (!jwLines.length) { fail('Add at least one line.'); return; }
    const bad = jwLines.find(l => !Number.isInteger(Number(l.qty)) || Number(l.qty) <= 0);
    if (bad) { fail(`Enter how many ${bad.sku} go out (a whole number).`); return; }
    const lines = jwLines.map((l, i) => ({ line_no: i + 1, sku: l.sku, qty: Number(l.qty), batch_no: (l.batch_no || '').trim(), gross_weight: num(l.gross_weight),
      net_weight: num(l.net_weight), fine_weight: num(l.fine_weight), purity: (l.purity || '').trim(), output_sku: (l.output_sku || '').trim(), output_per_unit: num(l.output_per_unit) }));
    btn.disabled = true;
    const res = await apiFetch('/api/v1/doc/SubcontractOrder', { method: 'POST', body: JSON.stringify({ vendor_id: v('#jw-vendor'), location: v('#jw-store'),
      due_date: v('#jw-due'), capital_goods: v('#jw-capital'), lines: JSON.stringify(lines), status: 'Draft' }) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { fail(await getErrorMessage(res, 'The order was not saved.')); return; }
    jwLines = [];
    txnDone('jw-screen');
    showToast('Job work order saved - Send puts it on a delivery challan.', { variant: 'success' });
    renderView('manufacturing:subcontracting');
  });
  draw();
}
