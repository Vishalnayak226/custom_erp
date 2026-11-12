// Stage 148.18: the vendor's own page for one purchase order, opened from the
// signed link in the buyer's message (/p/vendor.html?t=<token>). No login: the
// link is the credential and it names this one order. The vendor proposes a
// quantity or date per line and may send an invoice; the buyer decides.
(() => {
  'use strict';
  const q = new URLSearchParams(location.search);
  const token = q.get('t') || '';
  const tenant = q.get('tenant_id') || '';
  const app = document.getElementById('app');
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const url = (tail) => `/api/v1/procurement/vendor-link/public${tail || ''}?t=${encodeURIComponent(token)}${tenant ? '&tenant_id=' + encodeURIComponent(tenant) : ''}`;
  const FAIL = 'This link is not valid or has expired.';

  async function call(tail, opts) {
    const res = await fetch(url(tail), Object.assign({ cache: 'no-store' }, opts || {}));
    let data = null;
    try { data = await res.json(); } catch (_) { /* no body */ }
    if (!res.ok) {
      const m = (data && (data.error || data.message)) || FAIL;
      const d = data && data.detail ? String(data.detail) : '';
      throw new Error(d && d.toLowerCase() !== m.toLowerCase().replace(/\.$/, '') ? `${m} ${d}` : m);
    }
    return data;
  }

  function paint(v, note, bad) {
    const rows = (v.lines || []).map(l => `<tr data-no="${l.no}">
      <td>${esc(l.item || l.sku)}</td><td>${esc(l.qty)} ${esc(l.unit)}</td><td>${esc(l.due || '')}</td>
      <td><input type="text" inputmode="decimal" data-f="qty" aria-label="New qty" placeholder="${esc(l.qty)}"></td>
      <td><input type="date" data-f="date" aria-label="New date"></td>
      <td><input type="text" data-f="unit" aria-label="Your unit" maxlength="20"></td>
      <td><input type="text" data-f="note" aria-label="Note" maxlength="200"></td></tr>`).join('');
    app.innerHTML = `
      <h1>${esc(v.vendor)} · ${esc(v.po)}</h1>
      <p class="muted">Link valid to ${esc(v.expires)}</p>
      ${note ? `<div class="msg ${bad ? 'bad' : 'ok'}" role="status">${esc(note)}</div>` : ''}
      <div class="scroll"><table class="tbl"><thead><tr><th>Item</th><th>Qty</th><th>Due</th><th>New qty</th><th>New date</th><th>Your unit</th><th>Note</th></tr></thead><tbody>${rows}</tbody></table></div>
      <button type="button" id="send">Send changes</button>
      <h1>Invoice</h1>
      <div class="grid2"><input type="text" id="inv-no" placeholder="Invoice no" maxlength="40"><input type="date" id="inv-date" aria-label="Invoice date"></div>
      <div class="grid2"><input type="text" id="inv-amt" inputmode="decimal" placeholder="Amount"><input type="file" id="inv-file" accept=".pdf,.png,.jpg,.jpeg" aria-label="Invoice file"></div>
      <button type="button" id="up" class="secondary">Send invoice</button>`;
    document.getElementById('send').onclick = () => send(v);
    document.getElementById('up').onclick = () => upload(v);
  }

  async function reload(note, bad) {
    try { paint(await call(''), note, bad); } catch (e) { app.innerHTML = `<div class="msg bad" role="alert">${esc(e.message || FAIL)}</div>`; }
  }

  async function send(v) {
    const lines = [];
    app.querySelectorAll('tbody tr').forEach(tr => {
      const f = (n) => (tr.querySelector(`[data-f="${n}"]`).value || '').trim();
      const o = { line: Number(tr.dataset.no) };
      if (f('qty') !== '') o.qty = Number(f('qty').replace(',', '.'));
      if (f('date')) o.date = f('date');
      if (f('unit')) o.unit = f('unit');
      if (f('note')) o.note = f('note');
      if (o.qty !== undefined || o.date || o.unit || o.note) lines.push(o);
    });
    if (!lines.length) return reload('Enter a change first', true);
    if (lines.some(o => o.qty !== undefined && !Number.isFinite(o.qty))) return reload('Qty must be a number', true);
    const b = document.getElementById('send'); b.disabled = true;
    try {
      const r = await call('/proposal', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ lines }) });
      await reload(r.applied ? 'Changes accepted' : 'Sent to the buyer');
    } catch (e) { b.disabled = false; reload(e.message, true); }
  }

  async function upload(v) {
    const file = document.getElementById('inv-file').files[0];
    if (!file) return reload('Choose a file', true);
    if (file.size > 2 * 1024 * 1024) return reload('The file is over 2 MB', true);
    const fd = new FormData();
    fd.append('file', file);
    fd.append('invoice_no', document.getElementById('inv-no').value);
    fd.append('invoice_date', document.getElementById('inv-date').value);
    fd.append('amount', document.getElementById('inv-amt').value);
    const b = document.getElementById('up'); b.disabled = true;
    try { await call('/invoice', { method: 'POST', body: fd }); await reload('Invoice sent for review'); }
    catch (e) { b.disabled = false; reload(e.message, true); }
  }

  if (!token) app.innerHTML = `<div class="msg bad" role="alert">${FAIL}</div>`; else reload();
})();
