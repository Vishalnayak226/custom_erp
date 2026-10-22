// Stage 101.8.2: the customer's preference page, opened from the link in a
// message (/p/?t=<token>). The token names no customer; the server finds them
// by its hash. The customer chooses, per channel, offers and service
// messages, or stops everything; choosing again is harmless.
(() => {
  'use strict';
  const q = new URLSearchParams(location.search);
  const token = q.get('t') || '';
  const tenant = q.get('tenant_id') || '';
  const app = document.getElementById('app');
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const url = () => `/api/v1/crm/public/preferences?t=${encodeURIComponent(token)}${tenant ? '&tenant_id=' + encodeURIComponent(tenant) : ''}`;
  const names = { WhatsApp: 'WhatsApp', SMS: 'Text messages (SMS)', Email: 'Email' };

  async function call(method, body) {
    const res = await fetch(url(), { method, headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined, cache: 'no-store' });
    let data = null;
    try { data = await res.json(); } catch (_) { /* no body */ }
    if (!res.ok) throw new Error((data && data.error) || 'This link is not valid or has expired.');
    return data;
  }

  function paint(p, note) {
    const channels = (p.channels || []).map(c => `<div class="row"><div><b>${esc(names[c.channel] || c.channel)}</b><small>${esc(c.contact)}</small></div>
      <div><label class="switch"><input type="checkbox" data-ch="${esc(c.channel)}" data-k="marketing" ${c.marketing ? 'checked' : ''} ${p.stop_all ? 'disabled' : ''}> Offers</label>
      <label class="switch"><input type="checkbox" data-ch="${esc(c.channel)}" data-k="service" ${c.service ? 'checked' : ''} ${p.stop_all ? 'disabled' : ''}> About my orders and visits</label></div></div>`).join('');
    app.innerHTML = `<h1>${p.first_name ? 'Hi ' + esc(p.first_name) + ', what' : 'What'} would you like to hear from ${esc(p.business || 'us')}?</h1>
      ${note ? `<div class="msg ok" role="status">${esc(note)}</div>` : ''}
      <div>${channels || '<p class="muted">We have no phone number or email for you.</p>'}</div>
      ${p.stop_all ? '<div class="msg">You have stopped all messages.</div><button type="button" id="resume" class="secondary">Start messages again</button>'
        : '<button type="button" id="save">Save my choices</button><button type="button" id="stop" class="secondary">Stop all messages</button>'}
      <p class="muted">We only send offers you have agreed to. You can change this at any time from the link in our messages.</p>`;
    const save = app.querySelector('#save');
    if (save) save.addEventListener('click', () => submit({ channels: [...new Set([...app.querySelectorAll('[data-ch]')].map(i => i.dataset.ch))].map(ch => ({
      channel: ch, marketing: app.querySelector(`[data-ch="${ch}"][data-k="marketing"]`).checked, service: app.querySelector(`[data-ch="${ch}"][data-k="service"]`).checked })) }, 'Saved. Thank you.'));
    const stop = app.querySelector('#stop');
    if (stop) stop.addEventListener('click', () => submit({ stop_all: true, channels: [] }, 'Done - you will not get any more messages from us.'));
    const resume = app.querySelector('#resume');
    if (resume) resume.addEventListener('click', () => submit({ stop_all: false, channels: [] }, 'Messages are back on. Choose what you would like below.'));
  }

  async function submit(change, note) {
    app.querySelectorAll('button').forEach(b => { b.disabled = true; });
    try { paint(await call('POST', change), note); } catch (e) { app.insertAdjacentHTML('afterbegin', `<div class="msg bad" role="alert">${esc(e.message)}</div>`); app.querySelectorAll('button').forEach(b => { b.disabled = false; }); }
  }

  if (!token) { app.innerHTML = '<div class="msg bad">This link is not complete. Use the link in your latest message from us.</div>'; return; }
  call('GET').then(p => paint(p)).catch(e => { app.innerHTML = `<div class="msg bad" role="alert">${esc(e.message)}</div>`; });
})();
