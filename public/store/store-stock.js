// Stage 102.3: stock work on the phone - receive by scanning (102.3.2), count
// by scanning (102.3.3, works offline), labels for the store's computer to
// print (102.3.4) and asking for stock (a transfer request). Loaded by
// store.js; precached by sw.js so a count opens without a connection.
//
// What is saved goes through the endpoints the desktop uses: a goods receipt
// check is its Verify step, a transfer-in is the transfer receipt, a request
// for stock is a Transfer Order sent for approval.

const COUNT_QUEUE = 'store_app_count_queue';
const countKey = (key) => `store_app_count_${key}`;
const receiveKey = (kind, id) => `store_app_receive_${kind}_${id}`;
const $$ = (root, sel) => root.querySelector(sel);
// Counts this phone holds; storage may be off (private mode).
function storedCounts(ls) {
  let keys = [];
  try { keys = Object.keys(localStorage); } catch (_) { return []; }
  return keys.filter(k => k.startsWith('store_app_count_') && k !== COUNT_QUEUE).map(k => ls.json(k, null)).filter(Boolean);
}

// matchLine finds the line a scanned code belongs to: one of its codes, else
// the item the code is (a batch number, another barcode) by a lookup.
async function matchLine(ctx, lines, code) {
  const c = code.trim().toLowerCase();
  let line = lines.find(l => (l.codes || []).some(x => String(x).toLowerCase() === c));
  if (line || !navigator.onLine) return line || null;
  try {
    const item = await ctx.lookupItem(code);
    line = lines.find(l => l.sku === item.code || (l.codes || []).includes(item.code));
  } catch (_) { /* not an item */ }
  return line || null;
}

function statusOf(counted, expected) {
  if (counted === expected) return ['ok', 'Matches'];
  if (counted < expected) return ['short', `${expected - counted} short`];
  return ['over', `${counted - expected} extra`];
}

// ------------------------------------------------------------------ receive

async function renderReceive(ctx, params) {
  const kind = params.get('kind');
  const id = params.get('id');
  if (kind && id) return renderReceiveDoc(ctx, kind, id);
  const { api, esc, qty, main, panelError } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let res;
  try {
    res = await api('/api/v1/store-app/receive');
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderReceive(ctx, params));
    return;
  }
  if (ctx.routeName() !== 'receive') return;
  const docs = res.documents || [];
  main.innerHTML = docs.length ? `
    <p class="hint">Deliveries waiting at this store. Open one and scan what arrived; the phone shows what is short or extra before you save.</p>
    ${docs.map(d => `<a class="card card-link" href="#/receive?kind=${esc(d.kind)}&id=${encodeURIComponent(d.id)}">
      <h2>${d.kind === 'grn' ? 'Goods receipt' : 'Transfer'} · ${esc(d.number)}</h2>
      <div class="sub">From ${esc(d.from || '—')} · ${esc(d.status)}${d.lines ? ` · ${qty(d.units)} item${d.units === 1 ? '' : 's'} on the way` : ''}${d.created ? ` · ${esc(ctx.fmtDate(d.created))}` : ''}</div></a>`).join('')}`
    : '<p class="empty">Nothing is waiting to be received at this store.</p>';
}

async function renderReceiveDoc(ctx, kind, id) {
  const { api, esc, qty, main, panelError, ls, setBar, toast } = ctx;
  setBar(kind === 'grn' ? 'Check a goods receipt' : 'Receive a transfer', '#/receive');
  main.innerHTML = '<p class="loading">Loading…</p>';
  let doc;
  try {
    doc = await api(`/api/v1/store-app/receive/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`);
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderReceiveDoc(ctx, kind, id));
    return;
  }
  const saved = ls.json(receiveKey(kind, id), null) || { counts: {}, extras: {}, receiptKey: ctx.newKey() };
  const persist = () => ls.setJSON(receiveKey(kind, id), saved);
  persist();
  const reasons = doc.reason_codes || [];

  main.innerHTML = `
    <article class="card"><h2>${esc(doc.number)}</h2><div class="sub">From ${esc(doc.from || '—')}</div></article>
    ${doc.can_save ? '' : `<div class="msg msg-info" role="status">${esc(doc.cannot_save)}</div>`}
    <div id="rcv-scan"></div>
    <div class="toolbar"><span id="rcv-summary"></span><button type="button" class="link" id="rcv-reset">Start again</button></div>
    <ul class="rows count-rows" id="rcv-lines"></ul>
    <div id="rcv-extras"></div>
    <div id="rcv-finish"></div>`;

  const draw = () => {
    let done = 0;
    let issues = 0;
    $$(main, '#rcv-lines').innerHTML = doc.lines.map(l => {
      const counted = Number(saved.counts[l.key] || 0);
      const [cls, label] = statusOf(counted, l.expected);
      if (counted === l.expected) done++; else issues++;
      return `<li class="count-row ${cls}" data-key="${esc(l.key)}">
        <span>${esc(l.name)}<small>${esc(l.sku)} · expected ${qty(l.expected)} · <b>${esc(label)}</b></small></span>
        <span class="stepper"><button type="button" class="btn btn-small" data-step="-1" aria-label="One less">−</button>
          <input class="input qty-input" type="number" inputmode="numeric" min="0" value="${counted}" aria-label="Counted">
          <button type="button" class="btn btn-small" data-step="1" aria-label="One more">+</button></span></li>`;
    }).join('');
    $$(main, '#rcv-summary').textContent = `${done} of ${doc.lines.length} lines match${issues ? ` · ${issues} to look at` : ''}`;
    const extras = Object.entries(saved.extras);
    $$(main, '#rcv-extras').innerHTML = extras.length ? `<div class="msg msg-error" role="status">Not on this delivery (not saved - sort these out on the computer): ${extras.map(([c, n]) => `${esc(c)} × ${n}`).join(', ')}</div>` : '';
    $$(main, '#rcv-lines').querySelectorAll('.count-row').forEach(row => {
      const key = row.dataset.key;
      const input = row.querySelector('input');
      row.querySelectorAll('[data-step]').forEach(b => b.addEventListener('click', () => {
        saved.counts[key] = Math.max(0, Number(saved.counts[key] || 0) + Number(b.dataset.step));
        persist(); draw();
      }));
      input.addEventListener('change', () => {
        saved.counts[key] = Math.max(0, Math.round(Number(input.value) || 0));
        persist(); draw();
      });
    });
    drawFinish();
  };

  const drawFinish = () => {
    const box = $$(main, '#rcv-finish');
    if (!doc.can_save) { box.innerHTML = ''; return; }
    const short = doc.lines.some(l => Number(saved.counts[l.key] || 0) < l.expected);
    const over = doc.lines.filter(l => Number(saved.counts[l.key] || 0) > (kind === 'grn' ? (l.po_open_qty ?? Infinity) : l.expected));
    const reasonSelect = reasons.length ? `<label class="field"><span>Reason for what is short or extra</span>
      <select class="input" id="rcv-reason"><option value="">Choose a reason</option>${reasons.map(r => `<option value="${esc(r.code)}">${esc(r.label)}</option>`).join('')}</select></label>` : '';
    if (kind === 'grn') {
      box.innerHTML = `
        ${over.length ? `<div class="card"><h2>More than the PO</h2>${over.map(l => `<label class="field"><span>${esc(l.name)}: keep the extra or send it back?</span>
          <select class="input" data-excess="${esc(l.key)}"><option value="keep">Keep it (needs approval)</option><option value="return">Send it back</option></select></label>`).join('')}</div>` : ''}
        <button type="button" class="btn btn-primary btn-block" id="rcv-save">Save the check</button>
        <p class="hint">Saving records what you counted as the goods receipt quantity (its Verify step). Prices and posting stay on the computer.</p>`;
    } else {
      box.innerHTML = `
        ${short ? `<div class="card"><h2>Some items are short</h2>
          <label class="check"><input type="radio" name="rcv-rest" value="open" checked> More is coming - keep the rest in transit</label>
          <label class="check"><input type="radio" name="rcv-rest" value="lost"> Nothing more is coming - the rest is lost</label></div>` : ''}
        ${reasonSelect}
        <button type="button" class="btn btn-primary btn-block" id="rcv-save">Receive into stock</button>`;
    }
    $$(main, '#rcv-save').addEventListener('click', save);
  };

  const save = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      if (kind === 'grn') {
        const lines = doc.lines.map(l => {
          const out = { line_no: l.line_no, grn_qty: Number(saved.counts[l.key] || 0) };
          const ex = main.querySelector(`[data-excess="${CSS.escape(l.key)}"]`);
          if (ex) out.excess_action = ex.value;
          return out;
        });
        await api(`/api/v1/grn-steps/${encodeURIComponent(id)}/verify`, { method: 'POST', body: { lines, reason: 'Checked on the store app' } });
        toast('Checked. The goods receipt goes on to the price check.');
      } else {
        const rest = main.querySelector('input[name="rcv-rest"]:checked');
        const keepOpen = !rest || rest.value === 'open';
        const reason = (main.querySelector('#rcv-reason') || {}).value || '';
        const items = doc.lines.map(l => {
          const counted = Number(saved.counts[l.key] || 0);
          const line = { sku: l.sku, qty: Math.min(counted, l.expected) };
          if (counted > l.expected) line.excess_qty = counted - l.expected;
          if (!keepOpen && counted < l.expected) line.lost_qty = l.expected - counted;
          if (line.excess_qty || line.lost_qty) line.reason_code = reason;
          return line;
        });
        if (items.some(l => (l.excess_qty || l.lost_qty) && !l.reason_code)) {
          toast('Choose a reason for what is short or extra.', true);
          btn.disabled = false;
          return;
        }
        const res = await api('/api/v1/transfer/receive', { method: 'POST', body: { transfer_order_id: id, received_items: items, keep_open: keepOpen, receipt_key: saved.receiptKey } });
        toast(`Received. The transfer is now ${String(res.status || '').toLowerCase()}.`);
      }
      ls.del(receiveKey(kind, id));
      location.hash = '#/receive';
    } catch (ex) {
      btn.disabled = false;
      if (!ex.quiet) toast(ex.message, true);
    }
  };

  ctx.scanBox($$(main, '#rcv-scan'), async (code) => {
    const line = await matchLine(ctx, doc.lines, code);
    if (line) {
      saved.counts[line.key] = Number(saved.counts[line.key] || 0) + 1;
      if (navigator.vibrate) navigator.vibrate(30);
    } else {
      saved.extras[code] = Number(saved.extras[code] || 0) + 1;
      toast(`${code} is not on this delivery.`, true);
    }
    persist();
    draw();
  });
  $$(main, '#rcv-reset').addEventListener('click', () => {
    saved.counts = {}; saved.extras = {}; persist(); draw();
  });
  draw();
}


// ------------------------------------------------------------------- count

async function renderCount(ctx, params) {
  const key = params.get('session');
  if (key) return renderCountSession(ctx, key);
  const { api, esc, main, ls } = ctx;
  const queue = ls.json(COUNT_QUEUE, []) || [];
  const open = storedCounts(ls).filter(c => !queue.includes(c.key));
  main.innerHTML = `
    <button type="button" class="btn btn-primary btn-block" id="cnt-spot">Start a spot count</button>
    <p class="hint">Scan every item on a shelf or in a bin and send the count. It works without a connection: the count stays on this phone and is sent when you are back online.</p>
    ${open.length ? `<h3 class="section-title">Counts on this phone</h3>${open.map(s => `<a class="card card-link" href="#/count?session=${encodeURIComponent(s.key)}"><h2>${esc(s.label)}</h2><div class="sub">${Object.keys(s.lines || {}).length} code(s) scanned · started ${esc(ctx.ago(s.created))}</div></a>`).join('')}` : ''}
    ${queue.length ? `<div class="msg msg-info" role="status">${queue.length} finished count(s) waiting for a connection to be sent.</div>` : ''}
    <h3 class="section-title">Full counts at this store</h3>
    <div id="cnt-full"><p class="loading">Loading…</p></div>`;
  $$(main, '#cnt-spot').addEventListener('click', () => {
    const s = { key: ctx.newKey(), mode: 'spot', label: `Spot count ${new Date().toLocaleString()}`, lines: {}, names: {}, created: new Date().toISOString(), store: ctx.workingStore() };
    ls.setJSON(countKey(s.key), s);
    location.hash = `#/count?session=${encodeURIComponent(s.key)}`;
  });
  const full = $$(main, '#cnt-full');
  if (!navigator.onLine) { full.innerHTML = '<p class="empty">Full counts are listed when you are online.</p>'; return; }
  try {
    const res = await api('/api/v1/store-app/counts');
    const list = res.full_counts || [];
    full.innerHTML = list.length ? list.map(p => `<button type="button" class="card card-link" data-pi="${esc(p.id)}">
      <h2>Full count ${esc(p.id)}${p.zone ? ` · ${esc(p.zone)}` : ''}</h2><div class="sub">${p.counted} of ${p.lines} items counted · started ${esc(p.started)}</div></button>`).join('')
      : '<p class="empty">No full count is open here. A full count (physical inventory) is started by the warehouse team; it shows here while it is open.</p>';
    full.querySelectorAll('[data-pi]').forEach(b => b.addEventListener('click', () => {
      const s = { key: ctx.newKey(), mode: 'full', pi: b.dataset.pi, label: `Full count ${b.dataset.pi}`, lines: {}, names: {}, created: new Date().toISOString(), store: ctx.workingStore() };
      ls.setJSON(countKey(s.key), s);
      location.hash = `#/count?session=${encodeURIComponent(s.key)}`;
    }));
  } catch (e) {
    full.innerHTML = e.quiet ? '' : `<p class="empty">${esc(e.message)}</p>`;
  }
}

function renderCountSession(ctx, key) {
  const { esc, qty, main, ls, setBar, toast } = ctx;
  const s = ls.json(countKey(key), null);
  if (!s) { location.hash = '#/count'; return; }
  setBar(s.mode === 'full' ? 'Full count' : 'Spot count', '#/count');
  const persist = () => ls.setJSON(countKey(key), s);
  main.innerHTML = `
    <article class="card"><h2>${esc(s.label)}</h2><div class="sub">Scan each item once per piece. Fix a number by tapping − / +.</div></article>
    <div id="cnt-scan"></div>
    <div class="toolbar"><span id="cnt-summary"></span><button type="button" class="link" id="cnt-discard">Discard this count</button></div>
    <ul class="rows count-rows" id="cnt-lines"></ul>
    <button type="button" class="btn btn-primary btn-block" id="cnt-send">Finish and send</button>`;
  const draw = () => {
    const entries = Object.entries(s.lines);
    $$(main, '#cnt-summary').textContent = `${entries.length} item(s), ${qty(entries.reduce((a, [, n]) => a + n, 0))} piece(s)`;
    $$(main, '#cnt-lines').innerHTML = entries.reverse().map(([code, n]) => `<li class="count-row" data-code="${esc(code)}">
      <span>${esc(s.names[code] || code)}<small>${esc(code)}</small></span>
      <span class="stepper"><button type="button" class="btn btn-small" data-step="-1" aria-label="One less">−</button>
        <input class="input qty-input" type="number" inputmode="numeric" min="0" value="${n}" aria-label="Counted">
        <button type="button" class="btn btn-small" data-step="1" aria-label="One more">+</button></span></li>`).join('');
    $$(main, '#cnt-lines').querySelectorAll('.count-row').forEach(row => {
      const code = row.dataset.code;
      const set = (v) => { if (v <= 0) delete s.lines[code]; else s.lines[code] = v; persist(); draw(); };
      row.querySelectorAll('[data-step]').forEach(b => b.addEventListener('click', () => set((s.lines[code] || 0) + Number(b.dataset.step))));
      row.querySelector('input').addEventListener('change', (e) => set(Math.max(0, Math.round(Number(e.target.value) || 0))));
    });
  };
  ctx.scanBox($$(main, '#cnt-scan'), async (code) => {
    s.lines[code] = (s.lines[code] || 0) + 1;
    persist();
    draw();
    if (navigator.vibrate) navigator.vibrate(30);
    if (!s.names[code] && navigator.onLine) {
      try {
        const item = await ctx.lookupItem(code);
        s.names[code] = item.name;
        persist();
        draw();
      } catch (_) {
        toast(`${code} is not an item - it will not be counted.`, true);
      }
    }
  });
  $$(main, '#cnt-discard').addEventListener('click', (e) => {
    if (!e.currentTarget.dataset.armed) { e.currentTarget.dataset.armed = '1'; e.currentTarget.textContent = 'Tap again to discard'; return; }
    ls.del(countKey(key));
    location.hash = '#/count';
  });
  $$(main, '#cnt-send').addEventListener('click', async (e) => {
    if (!Object.keys(s.lines).length) { toast('Nothing has been counted yet.', true); return; }
    e.currentTarget.disabled = true;
    const result = await sendCount(ctx, s);
    if (result) {
      ls.del(countKey(key));
      showCountResult(ctx, result);
    } else {
      const queue = ls.json(COUNT_QUEUE, []) || [];
      if (!queue.includes(key)) ls.setJSON(COUNT_QUEUE, [...queue, key]);
      toast('Saved on this phone. It is sent by itself when you are back online.');
      location.hash = '#/count';
    }
  });
  draw();
}

// sendCount posts one count; null means "not sent now" (offline or a network
// failure) and the count stays queued. The count's key makes a resend safe.
async function sendCount(ctx, s) {
  if (!navigator.onLine) return null;
  try {
    return await ctx.api('/api/v1/store-app/counts', {
      method: 'POST',
      body: { session_key: s.key, mode: s.mode, physical_inventory: s.pi || '', lines: Object.entries(s.lines).map(([code, q]) => ({ code, qty: q })) },
    });
  } catch (e) {
    if (e.status >= 400 && e.status < 500 && e.status !== 429 && e.status !== 401) {
      // The server refused it for a reason a resend will not change.
      ctx.toast(e.message, true);
      return { message: e.message, refused: true, unknown: [] };
    }
    return null;
  }
}

function showCountResult(ctx, r) {
  const { esc, main, setBar } = ctx;
  setBar('Count sent', '#/count');
  main.innerHTML = `<div class="msg ${r.refused ? 'msg-error' : 'msg-info'}" role="status">${esc(r.message)}</div>
    ${(r.unknown || []).length ? `<p class="hint">Not items, so not counted: ${r.unknown.map(esc).join(', ')}</p>` : ''}
    ${r.pending_approval ? '<p class="hint">A count that differs from the stock goes to the store manager for approval before stock changes.</p>' : ''}
    <a class="btn btn-block" href="#/count">Back to counts</a>`;
}

export async function syncQueuedCounts(ctx) {
  const queue = ctx.ls.json(COUNT_QUEUE, []) || [];
  if (!queue.length || !navigator.onLine) return;
  const left = [];
  let sent = 0;
  for (const key of queue) {
    const s = ctx.ls.json(countKey(key), null);
    if (!s) continue;
    const r = await sendCount(ctx, s);
    if (r) { ctx.ls.del(countKey(key)); sent++; } else left.push(key);
  }
  ctx.ls.setJSON(COUNT_QUEUE, left);
  if (sent) ctx.toast(`${sent} count(s) saved on this phone were sent.`);
}

// ------------------------------------------------------------------ labels

async function renderLabels(ctx, params) {
  const { api, esc, main, toast } = ctx;
  const code = params.get('code') || '';
  main.innerHTML = `<div id="lbl-form"></div><h3 class="section-title">Asked from this store</h3><div id="lbl-list"><p class="loading">Loading…</p></div>`;
  const form = $$(main, '#lbl-form');
  const drawAsk = async (c) => {
    if (!c) {
      form.innerHTML = '<p class="hint">Scan an item to ask the store\'s computer to print its labels.</p><div id="lbl-scan"></div>';
      ctx.scanBox($$(form, '#lbl-scan'), (x) => drawAsk(x), { button: 'Look up' });
      return;
    }
    form.innerHTML = '<p class="loading">Looking up…</p>';
    let item;
    try { item = await ctx.lookupItem(c); } catch (e) {
      form.innerHTML = `<div class="msg msg-error" role="alert">${esc(e.message)}</div><div id="lbl-scan"></div>`;
      ctx.scanBox($$(form, '#lbl-scan'), (x) => drawAsk(x), { button: 'Look up' });
      return;
    }
    const batch = item.batch ? item.batch.batch_no : '';
    form.innerHTML = `<form class="card" id="lbl-ask">
      <h2>${esc(item.name)}</h2><div class="sub">${esc(item.code)}${batch ? ` · lot ${esc(batch)}` : ''}</div>
      <label class="field"><span>How many labels</span><input class="input qty-input" id="lbl-copies" type="number" inputmode="numeric" min="1" max="500" value="1" required></label>
      <label class="field"><span>Note for the computer (optional)</span><input class="input" id="lbl-note" maxlength="200" placeholder="e.g. rack 4"></label>
      <div class="actions"><a class="btn" href="#/labels">Cancel</a><button type="submit" class="btn btn-primary">Send to the store's printer</button></div></form>`;
    $$(form, '#lbl-ask').addEventListener('submit', async (e) => {
      e.preventDefault();
      try {
        const req = await api('/api/v1/store-app/labels', { method: 'POST', body: { lines: [{ code: batch || item.code, copies: Math.round(Number($$(form, '#lbl-copies').value) || 0), batch_no: batch }], note: $$(form, '#lbl-note').value } });
        toast(`${req.labels} label(s) sent. They print at the store's computer (Sticker Printing > Print from Transaction > Store Label Request ${req.id}).`);
        location.hash = '#/labels';
      } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
    });
  };
  drawAsk(code);
  try {
    const res = await api('/api/v1/store-app/labels');
    const list = res.requests || [];
    $$(main, '#lbl-list').innerHTML = list.length ? `<ul class="rows">${list.map(r => `<li><span>${esc(r.items.map(i => i.name || i.sku).join(', '))}<small>${esc(r.id)} · ${r.labels} label(s) · asked by ${esc(r.requested_by)} ${esc(r.created)}</small></span>
      <span class="tag ${r.status === 'Printed' ? 'tag-ok' : ''}">${r.status === 'Printed' ? `Printed${r.printed_at ? ` ${esc(r.printed_at.slice(11))}` : ''}` : 'Waiting'}</span></li>`).join('')}</ul>
      <p class="hint">A phone cannot reach the label printer itself. The store's computer prints these from Sticker Printing > Print from Transaction > Store Label Request.</p>`
      : '<p class="empty">No labels asked for yet.</p>';
  } catch (e) {
    if (!e.quiet) $$(main, '#lbl-list').innerHTML = `<p class="empty">${esc(e.message)}</p>`;
  }
}

// --------------------------------------------------------------- ask for stock

async function renderAsk(ctx, params) {
  const { api, esc, qty, main, panelError, toast } = ctx;
  const code = params.get('code') || '';
  if (!code) { location.hash = '#/scan'; return; }
  main.innerHTML = '<p class="loading">Loading…</p>';
  let item;
  let sources;
  try {
    [item, sources] = await Promise.all([ctx.lookupItem(code), api('/api/v1/store-app/sources')]);
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderAsk(ctx, params));
    return;
  }
  const stockAt = Object.fromEntries((item.stock || []).map(s => [s.store, s.available]));
  const list = (sources.sources || []).map(s => ({ ...s, stock: stockAt[s.code] }));
  main.innerHTML = `<form class="card" id="ask-form">
    <h2>${esc(item.name)}</h2><div class="sub">${esc(item.code)} · sent to ${esc(ctx.storeName(ctx.workingStore()))}</div>
    <label class="field"><span>From</span><select class="input" id="ask-from" required>
      <option value="">Choose where it comes from</option>
      ${list.map(s => `<option value="${esc(s.code)}">${esc(s.name)}${s.type ? ` (${esc(s.type)})` : ''}${s.stock !== undefined ? ` · ${qty(s.stock)} available` : ''}</option>`).join('')}
    </select></label>
    <label class="field"><span>How many</span><input class="input qty-input" id="ask-qty" type="number" inputmode="numeric" min="1" value="1" required></label>
    <p class="hint">This makes a transfer request and sends it for approval, the same as Stock Movement on the computer.</p>
    <div class="actions"><a class="btn" href="#/scan?code=${encodeURIComponent(code)}">Cancel</a><button type="submit" class="btn btn-primary">Ask for it</button></div></form>`;
  $$(main, '#ask-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.submitter || $$(main, '#ask-form button[type="submit"]');
    btn.disabled = true;
    const from = $$(main, '#ask-from').value;
    const n = Math.round(Number($$(main, '#ask-qty').value) || 0);
    let created;
    try {
      created = await api('/api/v1/doc/TransferOrder', { method: 'POST', body: { from_warehouse: from, to_warehouse: ctx.workingStore(), items: JSON.stringify([{ sku: item.code, qty: n }]), status: 'Draft' } });
    } catch (ex) {
      btn.disabled = false;
      if (!ex.quiet) toast(ex.message, true);
      return;
    }
    try {
      await api('/api/v1/transfer/submit', { method: 'POST', body: { transfer_order_id: created.id } });
      main.innerHTML = `<div class="msg msg-info" role="status">Asked for ${qty(n)} × ${esc(item.name)}. Transfer ${esc(created.id)} is waiting for approval; the sender packs and sends it from there.</div><a class="btn btn-block" href="#/home">Done</a>`;
    } catch (ex) {
      main.innerHTML = `<div class="msg msg-info" role="status">Transfer ${esc(created.id)} is saved as a draft, but it could not be sent for approval${ex.message ? `: ${esc(ex.message)}` : ''}. Send it from Stock Movement on the computer.</div><a class="btn btn-block" href="#/home">Done</a>`;
    }
  });
}

export const routes = {
  receive: { title: 'Receive', render: renderReceive },
  count: { title: 'Count', render: renderCount },
  labels: { title: 'Labels', render: renderLabels },
  ask: { title: 'Ask for stock', render: renderAsk },
};
