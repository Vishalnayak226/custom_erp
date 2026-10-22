// Stage 102.4.1 + 101.12: serving a customer from the store app, and the
// salesman's own worklist. Loaded by store.js. Everything goes through the
// CRM's API (/api/v1/crm/...), so the phone follows the same rules as the
// desktop: contacts masked for roles without access, consent before an offer,
// and every message through the CRM's send gate.
//
//   #/customer          find by phone or name
//   #/customer?id=...    the customer: sizes, points, last bills, wish list,
//                        notes; add a note, record consent, send a WhatsApp look,
//                        ask the store to keep an item
//   #/tasks             my tasks (and nobody's at my store): done, take, message

function chip(esc, text) { return text ? `<span class="tag">${esc(text)}</span>` : ''; }

async function renderFind(ctx) {
  const { main, esc, api, toast } = ctx;
  main.innerHTML = `<form class="scan-form" id="cf-form" autocomplete="off">
      <input class="input" id="cf-q" inputmode="tel" placeholder="Phone number, or a name" aria-label="Find a customer">
      <button class="btn btn-primary" type="submit">Find</button></form>
    <div id="cf-out"></div>`;
  const out = main.querySelector('#cf-out');
  main.querySelector('#cf-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const q = main.querySelector('#cf-q').value.trim();
    if (!q) return;
    out.innerHTML = '<p class="loading">Finding…</p>';
    let r;
    try { r = await api(`/api/v1/crm/customers?q=${encodeURIComponent(q)}&page_size=20`); } catch (err) {
      out.innerHTML = err.quiet ? '' : `<div class="msg msg-error" role="alert">${esc(err.message)}</div>`;
      return;
    }
    if (!r.rows.length) {
      out.innerHTML = `<p class="empty">No customer matches.${/\d{6,}/.test(q) ? '' : ' Type the whole phone number to find them by phone.'}</p>`;
      return;
    }
    out.innerHTML = `<ul class="rows">${r.rows.map(c => `<li><a class="row-link" href="#/customer?id=${encodeURIComponent(c.id)}"><span>${esc(c.name || c.code)}<small>${esc(c.phone)} · ${esc(c.lifecycle)}</small></span>
      <span class="qty">${c.visits} bill(s)</span></a></li>`).join('')}</ul>`;
  });
  if (!matchMedia('(pointer: coarse)').matches) main.querySelector('#cf-q').focus();
  void toast;
}

async function renderCustomer(ctx, id) {
  const { main, esc, api, toast, money, fmtDate, setBar } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let c, bills;
  try {
    [c, bills] = await Promise.all([api(`/api/v1/crm/customers/${encodeURIComponent(id)}`),
      api(`/api/v1/crm/customers/${encodeURIComponent(id)}/timeline?kinds=bill,return&limit=5`)]);
  } catch (err) {
    main.innerHTML = err.quiet ? '' : `<div class="msg msg-error" role="alert">${esc(err.message)}</div>`;
    return;
  }
  const cu = c.customer || {};
  const s = c.summary || {};
  setBar(cu.name || 'Customer', true);
  const wa = (c.consent || []).find(k => k.channel === 'WhatsApp' && k.purpose === 'marketing') || {};
  main.innerHTML = `<article class="card"><h2>${esc(cu.name || cu.code)}</h2>
      <div class="sub">${esc(cu.phone || '')}${cu.city ? ' · ' + esc(cu.city) : ''}</div>
      <div>${chip(esc, s.lifecycle)} ${cu.loyalty_tier ? chip(esc, cu.loyalty_tier) : ''} ${chip(esc, wa.reachable ? 'Offers on WhatsApp: yes' : 'Offers on WhatsApp: no')}</div>
      <dl class="kv"><dt>Sizes</dt><dd>${esc(cu.sizes || '-')}</dd><dt>Points</dt><dd>${s.points}${s.points_pending ? ` (+${s.points_pending} pending)` : ''}</dd>
        <dt>Spend</dt><dd>${esc(money(s.spend))} over ${s.visits} bill(s)</dd><dt>Last bill</dt><dd>${s.last_bill ? esc(fmtDate(s.last_bill)) : '-'}</dd>
        ${cu.preferred_salesman ? `<dt>Salesman</dt><dd>${esc(cu.preferred_salesman)}</dd>` : ''}${cu.tags ? `<dt>Tags</dt><dd>${esc(cu.tags)}</dd>` : ''}</dl></article>
    <h3 class="section-title">Last bills</h3>
    <ul class="rows">${(bills.entries || []).map(e => `<li><span>${esc(e.title)}<small>${esc(fmtDate(e.at))} · ${esc(e.detail || '')}</small></span><span class="qty">${e.amount != null ? esc(money(e.amount)) : ''}</span></li>`).join('') || '<li class="empty">No bills yet.</li>'}</ul>
    <h3 class="section-title">Wish list</h3>
    <ul class="rows">${(c.wishlist || []).map(w => `<li><span>${esc(w.item_name || w.item_code)}</span><button type="button" class="link" data-keep="${esc(w.item_code)}">Ask the store to keep it</button></li>`).join('') || '<li class="empty">Nothing yet.</li>'}</ul>
    <div id="cw-scan"></div>
    <h3 class="section-title">Send a look on WhatsApp</h3>
    <div id="cl-scan"></div><div id="cl-out"></div>
    <h3 class="section-title">Notes</h3>
    <ul class="rows">${(c.notes || []).slice(0, 5).map(n => `<li><span>${esc(n.note)}<small>${esc(n.created_by)} · ${esc(fmtDate(n.created_at))}</small></span></li>`).join('') || '<li class="empty">No notes.</li>'}</ul>
    <label class="field"><span>Add a note</span><textarea class="textarea" id="cn-x" maxlength="2000"></textarea></label>
    <button type="button" class="btn btn-block" id="cn-add">Save the note</button>
    ${wa.reachable ? '' : `<div class="card"><p class="sub">Has ${esc(cu.name || 'the customer')} agreed to get offers on WhatsApp?</p>
      <button type="button" class="btn btn-block" id="cc-yes">Yes - they agreed just now</button></div>`}`;
  const reload = () => renderCustomer(ctx, id);
  main.querySelector('#cn-add').addEventListener('click', async () => {
    const note = main.querySelector('#cn-x').value.trim();
    if (!note) return;
    try { await api(`/api/v1/crm/customers/${encodeURIComponent(id)}/notes`, { method: 'POST', body: { note } }); toast('Note saved.'); reload(); } catch (e) { if (!e.quiet) toast(e.message, true); }
  });
  main.querySelector('#cc-yes')?.addEventListener('click', async () => {
    try {
      await api(`/api/v1/crm/customers/${encodeURIComponent(id)}/consent`, { method: 'POST', body: { channel: 'WhatsApp', purpose: 'marketing', action: 'granted', source: 'store_app', evidence: 'Asked in store, recorded on the store app' } });
      toast('Recorded.'); reload();
    } catch (e) { if (!e.quiet) toast(e.message, true); }
  });
  main.querySelectorAll('[data-keep]').forEach(b => b.addEventListener('click', async () => {
    try {
      await api('/api/v1/crm/holds', { method: 'POST', body: { customer_id: id, item_code: b.dataset.keep, qty: 1 } });
      toast('Asked the store to keep it. The customer is told once the store confirms.');
    } catch (e) { if (!e.quiet) toast(e.message, true); }
  }));
  ctx.scanBox(main.querySelector('#cw-scan'), async (code) => {
    try { await api(`/api/v1/crm/customers/${encodeURIComponent(id)}/wishlist`, { method: 'POST', body: { item_code: code } }); toast('Added to the wish list.'); reload(); } catch (e) { if (!e.quiet) toast(e.message, true); }
  }, { placeholder: 'Scan to add to the wish list', button: 'Add' });
  ctx.scanBox(main.querySelector('#cl-scan'), async (code) => {
    const out = main.querySelector('#cl-out');
    let item;
    try { item = await ctx.lookupItem(code); } catch (e) { if (!e.quiet) out.innerHTML = `<div class="msg msg-error">${esc(e.message)}</div>`; return; }
    // WhatsApp takes free text only within 24 hours of the customer writing
    // in; otherwise an approved template ({{item.name}} filled in) goes.
    let tpls = [];
    try { tpls = ((await api('/api/v1/crm/templates')).templates || []).filter(t => t.channel === 'WhatsApp' && t.purpose === 'marketing' && t.status === 'Active' && t.ready); } catch (_) { /* own text only */ }
    out.innerHTML = `<div class="card"><b>${esc(item.name)}</b><div class="sub">${item.price > 0 ? esc(money(item.price, item.currency)) : ''}</div>
      ${tpls.length ? `<label class="field"><span>Send as</span><select class="input" id="cl-t"><option value="">My own text</option>${tpls.map(t => `<option value="${esc(t.id)}">${esc(t.name)}</option>`).join('')}</select></label>` : ''}
      <label class="field" id="cl-xf"><span>Message</span><textarea class="textarea" id="cl-x" maxlength="1000">Hi ${esc((cu.name || '').split(' ')[0] || '')}, this just came in and I thought of you: ${esc(item.name)}${item.price > 0 ? ' - ' + esc(money(item.price, item.currency)) : ''}. Shall I keep one for you?</textarea></label>
      <p class="hint">Your own text reaches them only if they wrote to the store on WhatsApp in the last 24 hours${tpls.length ? '; otherwise choose a template' : ''}.</p>
      <button type="button" class="btn btn-primary btn-block" id="cl-send">Send on WhatsApp</button></div>`;
    const tSel = out.querySelector('#cl-t');
    tSel?.addEventListener('change', () => { out.querySelector('#cl-xf').hidden = !!tSel.value; });
    out.querySelector('#cl-send').addEventListener('click', async (ev) => {
      ev.target.disabled = true;
      const templateID = tSel ? tSel.value : '';
      try {
        const r = await api('/api/v1/crm/messages', { method: 'POST', body: { customer_id: id, channel: 'WhatsApp', purpose: 'marketing', template_id: templateID, text: templateID ? '' : out.querySelector('#cl-x').value.trim(),
          vars: { 'item.name': item.name, 'item.price': item.price > 0 ? money(item.price, item.currency) : '' } } });
        const bad = ['Suppressed', 'Cancelled', 'Expired'].includes(r.status);
        out.innerHTML = `<div class="msg ${bad ? 'msg-error' : 'msg-info'}" role="status">${bad ? 'Not sent: ' + esc([r.reason_label || r.reason, r.detail && r.detail !== r.reason_label ? r.detail : ''].filter(Boolean).join(' - ')) : 'Sent (' + esc(r.status) + ').'}</div>`;
      } catch (e) { ev.target.disabled = false; if (!e.quiet) toast(e.message, true); }
    });
  }, { placeholder: 'Scan the item to show', button: 'Show' });
}

async function renderTasks(ctx) {
  const { main, esc, api, toast, fmtDate } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let r;
  try { r = await api('/api/v1/crm/tasks?mine=1'); } catch (e) {
    main.innerHTML = e.quiet ? '' : `<div class="msg msg-error" role="alert">${esc(e.message)}</div>`;
    return;
  }
  const tasks = r.tasks || [];
  main.innerHTML = tasks.length ? tasks.map(t => `<article class="card" data-task="${esc(t.id)}">
      <h2>${esc(t.kind_label)}: ${esc(t.title)}</h2>
      <div class="sub">${t.customer_name ? esc(t.customer_name) + ' · ' : ''}${t.due_at ? 'due ' + esc(fmtDate(t.due_at)) : 'no due date'}${t.overdue ? ' · <b>overdue</b>' : ''}${t.owner ? '' : ' · anyone at the store'}</div>
      <div class="actions">${t.customer_id ? `<a class="btn" href="#/customer?id=${encodeURIComponent(t.customer_id)}">Customer</a>` : ''}
        ${t.owner ? '' : '<button type="button" class="btn" data-a="take">Take it</button>'}<button type="button" class="btn btn-primary" data-a="done">Done</button></div>
      <label class="field outcome" hidden><span>How did it go?</span><textarea class="textarea" maxlength="500"></textarea></label></article>`).join('')
    : '<p class="empty">Nothing on your worklist.</p>';
  main.querySelectorAll('[data-task]').forEach(card => card.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-a]');
    if (!b) return;
    const box = card.querySelector('.outcome');
    if (b.dataset.a === 'done' && box.hidden) { box.hidden = false; b.textContent = 'Save - done'; box.querySelector('textarea').focus(); return; }
    const outcome = box.querySelector('textarea').value.trim();
    if (b.dataset.a === 'done' && !outcome) { toast('Say how it went.', true); return; }
    try { await api(`/api/v1/crm/tasks/${encodeURIComponent(card.dataset.task)}/${b.dataset.a}`, { method: 'POST', body: { outcome } }); toast(b.dataset.a === 'done' ? 'Done.' : 'It is yours.'); renderTasks(ctx); } catch (err) { if (!err.quiet) toast(err.message, true); }
  }));
}

export const routes = {
  customer: { title: 'Serve a customer', render: (ctx, params) => (params.get('id') ? renderCustomer(ctx, params.get('id')) : renderFind(ctx)) },
  tasks: { title: 'My tasks', render: (ctx) => renderTasks(ctx) },
};
