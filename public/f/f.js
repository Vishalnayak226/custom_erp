// Stage 101.5.1: the feedback page behind the link sent after a bill
// (/f/?t=<token>). One answer per bill; answering again is thanked, not
// recorded twice.
(() => {
  'use strict';
  const q = new URLSearchParams(location.search);
  const token = q.get('t') || '';
  const tenant = q.get('tenant_id') || '';
  const app = document.getElementById('app');
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const url = () => `/api/v1/crm/public/feedback?t=${encodeURIComponent(token)}${tenant ? '&tenant_id=' + encodeURIComponent(tenant) : ''}`;

  async function call(method, body) {
    const res = await fetch(url(), { method, headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined, cache: 'no-store' });
    let data = null;
    try { data = await res.json(); } catch (_) { /* no body */ }
    if (!res.ok) throw new Error((data && data.error) || 'This link is not valid or has expired.');
    return data;
  }

  function thanks(f) {
    app.innerHTML = `<h1>Thank you!</h1><p>Your answer helps ${esc(f.business || 'us')} do better.</p>`;
  }

  function paint(f) {
    if (f.answered) { thanks(f); return; }
    const nps = f.scale === 'nps';
    const lo = nps ? 0 : 1, hi = nps ? 10 : 5;
    let score = null;
    const buttons = [];
    for (let i = lo; i <= hi; i++) buttons.push(`<button type="button" data-s="${i}" aria-pressed="false">${i}</button>`);
    app.innerHTML = `<h1>${esc(f.question)}</h1>
      <div class="scale${nps ? '' : ' rating'}" role="group" aria-label="Your score">${buttons.join('')}</div>
      <div class="ends"><span>${nps ? 'Not likely' : 'Poor'}</span><span>${nps ? 'Very likely' : 'Excellent'}</span></div>
      <label class="muted" for="c">Anything you would like to tell us? (optional)</label><textarea id="c" rows="3" maxlength="1000"></textarea>
      <button type="button" id="send" disabled>Send</button><div id="err"></div>`;
    app.querySelectorAll('[data-s]').forEach(b => b.addEventListener('click', () => {
      score = Number(b.dataset.s);
      app.querySelectorAll('[data-s]').forEach(x => x.setAttribute('aria-pressed', String(x === b)));
      app.querySelector('#send').disabled = false;
    }));
    app.querySelector('#send').addEventListener('click', async (e) => {
      e.target.disabled = true;
      try { thanks(await call('POST', { score, comment: app.querySelector('#c').value })); } catch (err) {
        app.querySelector('#err').innerHTML = `<div class="msg bad" role="alert">${esc(err.message)}</div>`;
        e.target.disabled = false;
      }
    });
  }

  if (!token) { app.innerHTML = '<div class="msg bad">This link is not complete. Use the link in your message from us.</div>'; return; }
  call('GET').then(paint).catch(e => { app.innerHTML = `<div class="msg bad" role="alert">${esc(e.message)}</div>`; });
})();
