// Stage 148.33: results of a lot held for inspection, entered at the store.
// Loaded by store.js. Lots come from /api/v1/store-app/inspections (this
// store's pending lots); a result goes to /api/v1/store-app/inspections/result.
// A line's result starts as Fail; the lines must total the lot. A failed lot
// keeps its units in QC hold (the vendor bill waits): ?status=Held lists them
// and /inspections/dispose scraps or releases them.
//
//   #/inspect   pending lots: lines (Pass / Fail, qty, a reading per test),
//               karat and weight, Save - or Waive where the check is optional;
//               then failed lots still held: Scrap or Release

function lineHtml(esc, lot, qty) {
  const reads = (lot.parameters || []).map(p => `<label class="field"><span>${esc(p)}</span><input class="input" type="number" step="any" data-read="${esc(p)}"></label>`).join('');
  return `<div class="il" data-line>
    <label class="field"><span>Result</span><select class="input" data-result><option value="Fail" selected>Fail</option><option value="Pass">Pass</option></select></label>
    <label class="field"><span>Qty</span><input class="input" type="number" min="0" step="1" inputmode="numeric" data-qty value="${qty}"></label>${reads}</div>`;
}

function heldHtml(esc, l, codes) {
  return `<article class="card" data-held="${esc(l.id)}">
    <h2>${esc(l.sku)} · ${esc(String(l.held_qty))} held</h2>
    <div class="sub">Failed · ${esc(l.status)}${l.batch_no ? ' · ' + esc(l.batch_no) : ''}</div>
    <label class="field"><span>Scrap reason</span><select class="input" data-code><option value="">Choose</option>${codes.map(c => `<option value="${esc(c.code)}">${esc(c.label || c.code)}</option>`).join('')}</select></label>
    <label class="field"><span>Note</span><input class="input" data-note maxlength="300"></label>
    <div class="actions"><button type="button" class="btn btn-primary" data-d="Scrap">Scrap</button><button type="button" class="btn" data-d="Release">Release</button></div>
  </article>`;
}

async function renderInspect(ctx) {
  const { main, esc, api, toast } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let r;
  let held = [];
  try { r = await api('/api/v1/store-app/inspections'); } catch (e) {
    main.innerHTML = e.quiet ? '' : `<div class="msg msg-error" role="alert">${esc(e.message)}</div>`;
    return;
  }
  try { held = (await api('/api/v1/store-app/inspections?status=Held')).lots || []; } catch (e) { held = []; }
  let codes = [];
  if (held.length) { try { codes = (await api('/api/v1/reason-codes?category=' + encodeURIComponent('Inventory Adjustment'))).codes || []; } catch (e) { codes = []; } }
  const lots = r.lots || [];
  if (!lots.length && !held.length) { main.innerHTML = '<p class="empty">Nothing to inspect.</p>'; return; }
  main.innerHTML = lots.map(l => `<article class="card" data-lot="${esc(l.id)}">
      <h2>${esc(l.sku)} · ${esc(String(l.qty))}</h2>
      <div class="sub">${esc(l.reason)} · sample ${esc(String(l.sample_size))}${l.batch_no ? ' · ' + esc(l.batch_no) : ''}</div>
      <div class="lines">${lineHtml(esc, l, l.qty)}</div>
      <div class="actions"><button type="button" class="btn" data-a="add">Add line</button></div>
      <div class="il"><label class="field"><span>Karat</span><input class="input" data-karat></label>
        <label class="field"><span>Weight</span><input class="input" data-weight></label></div>
      <label class="field" data-why hidden><span>Reason</span><textarea class="textarea" maxlength="300"></textarea></label>
      <div class="actions"><button type="button" class="btn btn-primary" data-a="save">Save</button>${l.can_waive ? '<button type="button" class="btn" data-a="waive">Waive</button>' : ''}</div>
    </article>`).join('') + held.map(l => heldHtml(esc, l, codes)).join('');
  const byId = Object.fromEntries(lots.map(l => [l.id, l]));
  main.querySelectorAll('[data-lot]').forEach(card => card.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-a]');
    if (!b) return;
    const lot = byId[card.dataset.lot];
    if (b.dataset.a === 'add') { card.querySelector('.lines').insertAdjacentHTML('beforeend', lineHtml(esc, lot, 0)); return; }
    const body = { lot_id: lot.id, karat: card.querySelector('[data-karat]').value.trim(), weight: card.querySelector('[data-weight]').value.trim() };
    if (b.dataset.a === 'waive') {
      const why = card.querySelector('[data-why]');
      if (why.hidden) { why.hidden = false; why.querySelector('textarea').focus(); return; }
      body.waive = true;
      body.note = why.querySelector('textarea').value.trim();
      if (!body.note) { toast('Give a reason.', true); return; }
    } else {
      body.lines = [...card.querySelectorAll('[data-line]')].map(row => {
        const readings = {};
        row.querySelectorAll('[data-read]').forEach(i => { if (i.value !== '') readings[i.dataset.read] = Number(i.value); });
        return { result: row.querySelector('[data-result]').value, qty: Number(row.querySelector('[data-qty]').value || 0), readings };
      });
    }
    b.disabled = true;
    try {
      const res = await api('/api/v1/store-app/inspections/result', { method: 'POST', body });
      toast(res.status + '.');
      renderInspect(ctx);
    } catch (err) { b.disabled = false; if (!err.quiet) toast(err.message, true); }
  }));
  main.querySelectorAll('[data-held]').forEach(card => card.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-d]');
    if (!b) return;
    const body = { lot_id: card.dataset.held, outcome: b.dataset.d, reason_code: card.querySelector('[data-code]').value, note: card.querySelector('[data-note]').value.trim() };
    b.disabled = true;
    try {
      const r = await api('/api/v1/store-app/inspections/dispose', { method: 'POST', body });
      toast(r && r.status === 'Pending Approval' ? 'Sent for approval.' : b.dataset.d === 'Scrap' ? 'Scrapped.' : 'Released.');
      renderInspect(ctx);
    } catch (err) { b.disabled = false; if (!err.quiet) toast(err.message, true); }
  }));
}

export const routes = {
  inspect: { title: 'Inspect', render: (ctx) => renderInspect(ctx) },
};
