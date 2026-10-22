// Stage 101.6: the business-buyer pipeline - the deal board, leads, deals
// with their quotation, the forecast, and the stages / assignment rules.
// Loaded by view-crm.js; its helpers are globals published by the loader.

export async function crmRenderTab(tab, host, st) {
  const p = st.params || {};
  if (st.page === 'deal') return crmDeal(host, p.id);
  if (st.page === 'lead') return crmLead(host, p.id);
  if (st.page === 'new-lead') return crmLeadNew(host);
  if (st.page === 'new-deal') return crmDealNew(host);
  const view = p.view || 'board';
  const body = crmFrame(host, { title: 'Pipeline', actions: `<button class="btn btn-outline" type="button" id="pl-lead">+ New lead</button><button class="btn btn-primary" type="button" id="pl-deal">+ New deal</button>` });
  host.querySelector('#pl-lead').addEventListener('click', () => crmGo('pipeline', 'new-lead'));
  host.querySelector('#pl-deal').addEventListener('click', () => crmGo('pipeline', 'new-deal'));
  body.innerHTML = `<div class="crm-subtabs">${[['board', 'Deals'], ['leads', 'Leads'], ['forecast', 'Forecast'], ['setup', 'Stages and assignment']].map(([k, l]) => `<button type="button" class="btn btn-sm ${k === view ? 'btn-primary' : 'btn-outline'}" data-pl="${k}">${l}</button>`).join('')}</div><div id="pl-out"></div>`;
  body.querySelectorAll('[data-pl]').forEach(b => b.addEventListener('click', () => crmGo('pipeline', '', { view: b.dataset.pl })));
  const out = body.querySelector('#pl-out');
  if (view === 'board') return crmBoard(out);
  if (view === 'leads') return crmLeads(out);
  if (view === 'forecast') return crmForecast(out);
  return crmPipelineSetup(out);
}

async function crmBoard(out) {
  const r = await crmApi('opportunities');
  if (!r) return;
  const open = r.stages.filter(s => s.kind === 'open' && s.active);
  out.innerHTML = `<div class="crm-board">${open.map(s => {
    const cards = r.opportunities.filter(o => o.stage_id === s.id);
    const total = cards.reduce((a, o) => a + o.value, 0);
    return `<section class="crm-col"><h4>${crmEsc(s.name)} <small>${s.probability}% · ${cards.length} · ${crmMoney(total)}</small></h4>
      ${cards.map(o => `<button type="button" class="crm-card" data-deal="${crmEsc(o.id)}"><b>${crmEsc(o.title)}</b><span>${crmEsc(o.customer_name)}</span>
        <span>${crmMoney(o.value)}${o.expected_close ? ' · ' + crmDate(o.expected_close) : ''}</span><small>${crmEsc(o.owner || 'No owner')}${o.quotation_status ? ' · quotation ' + crmEsc(o.quotation_status) : ''}</small></button>`).join('') || '<p class="text-muted crm-small">None</p>'}</section>`;
  }).join('')}</div>
  <p class="text-muted crm-small">Won and lost deals leave the board; find them in the Sales Pipeline report or the Forecast.</p>`;
  out.querySelectorAll('[data-deal]').forEach(b => b.addEventListener('click', () => crmGo('pipeline', 'deal', { id: b.dataset.deal })));
}

async function crmDealNew(host) {
  const body = crmFrame(host, { title: 'New deal', back: () => crmGo('pipeline'), backLabel: 'Pipeline' });
  body.innerHTML = `<form class="crm-form" id="nd-form">${crmField('nd-c', 'Customer (business buyer)', crmInput('nd-c'))}${crmField('nd-t', 'Deal', crmInput('nd-t', '', 'placeholder="Uniforms for 2027"'))}
    <div class="crm-form-row">${crmField('nd-v', 'Value', crmInput('nd-v', '', 'inputmode="decimal"'))}${crmField('nd-d', 'Expected close', crmInput('nd-d', '', 'type="date"'))}${crmField('nd-o', 'Owner', crmInput('nd-o'))}</div>
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Add</button></div></form>`;
  const pick = crmCustomerPicker(body.querySelector('#nd-c'));
  body.querySelector('#nd-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const v = Number(crmVal(body, 'nd-v')) || 0, d = crmVal(body, 'nd-d');
    const o = await crmApi('opportunities', { method: 'POST', body: { customer_id: pick(), title: crmVal(body, 'nd-t'), value: v, expected_close: d, owner: crmVal(body, 'nd-o') } });
    if (o) crmGo('pipeline', 'deal', { id: o.id });
  });
}

async function crmDeal(host, id) {
  const [o, all, q] = await Promise.all([crmApi('opportunities/' + encodeURIComponent(id)), crmApi('opportunities?status=all'), crmApi(`opportunities/${encodeURIComponent(id)}/quotation`)]);
  const body = crmFrame(host, { title: o ? o.title : 'Deal', back: () => crmGo('pipeline'), backLabel: 'Pipeline', actions: o ? crmStatus(o.status) : '' });
  if (!o) return;
  const stages = (all || {}).stages || [];
  const lines = (q && q.lines) || [];
  const open = o.status === 'Open';
  const tasks = ((await crmApi('tasks?status=all&opportunity_id=' + encodeURIComponent(id))) || {}).tasks || [];
  body.innerHTML = `<div class="crm-grid"><section><form class="crm-form" id="dl-form">
      ${crmField('dl-t', 'Deal', crmInput('dl-t', o.title, open ? '' : 'disabled'))}
      <dl class="crm-kv"><dt>Customer</dt><dd><a href="#" data-cust="${crmEsc(o.customer_id)}">${crmEsc(o.customer_name)}</a></dd><dt>Stage</dt><dd>${crmEsc(o.stage)}</dd>
        <dt>Weighted</dt><dd>${crmMoney(o.weighted)}</dd>${o.sales_order_id ? `<dt>Sales order</dt><dd>${crmEsc(o.sales_order_id)}</dd>` : ''}${o.lost_reason ? `<dt>Lost because</dt><dd>${crmEsc(o.lost_reason)}</dd>` : ''}</dl>
      <div class="crm-form-row">${crmField('dl-v', 'Value', crmInput('dl-v', o.value, `inputmode="decimal" ${open ? '' : 'disabled'}`))}${crmField('dl-p', 'Chance %', crmInput('dl-p', o.probability, `inputmode="numeric" ${open ? '' : 'disabled'}`))}
        ${crmField('dl-d', 'Expected close', crmInput('dl-d', o.expected_close || '', `type="date" ${open ? '' : 'disabled'}`))}${crmField('dl-o', 'Owner', crmInput('dl-o', o.owner || '', open ? '' : 'disabled'))}</div>
      ${open ? '<div class="crm-form-actions"><button class="btn btn-outline" type="submit">Save</button></div>' : ''}</form>
      ${open ? `<h3 class="crm-section">Move to</h3><div class="crm-form-row">${crmSelect('dl-s', stages.filter(s => s.active || s.kind !== 'open').map(s => [s.id, `${s.name}${s.kind === 'open' ? ` (${s.probability}%)` : ''}${(s.requires || []).length ? ' - needs ' + s.requires.join(', ') : ''}`]), o.stage_id)}
        ${crmInput('dl-lr', '', 'placeholder="Why it was lost (for Lost)"')}<button class="btn btn-primary" type="button" id="dl-move">Move</button></div>
        <p class="text-muted crm-small">Won needs the quotation approved, and raises one Sales Order from it.</p>` : ''}
      ${o.status === 'Won' && !o.sales_order_id ? '<button class="btn btn-primary" type="button" id="dl-order">Raise the Sales Order</button>' : ''}
      <h3 class="crm-section">Follow-ups</h3>${crmTable([{ label: 'Task', html: t => `${crmChip(t.kind_label)} ${crmEsc(t.title)}` }, { label: 'Due', html: t => crmDate(t.due_at, true) }, { label: 'Status', html: t => crmStatus(t.status) }, { label: 'Outcome', key: 'outcome' }], tasks, { empty: 'No follow-ups.' })}
      <button class="btn btn-outline btn-sm" type="button" id="dl-fu">+ Call, meeting or task</button>
    </section><section><h3 class="crm-section">Quotation ${q && q.status ? crmStatus(q.status) : ''}</h3>
      <table class="crm-table" id="ql"><thead><tr><th>Item code</th><th class="num">Qty</th><th class="num">Rate</th><th class="num">Amount</th><th></th></tr></thead><tbody></tbody></table>
      ${open ? '<button class="btn btn-outline btn-sm" type="button" id="ql-add">+ Line</button>' : ''}
      <div class="crm-form-row">${crmField('ql-vu', 'Valid until', crmInput('ql-vu', (q && q.quotation && q.quotation.valid_until) || '', `type="date" ${open ? '' : 'disabled'}`))}
        ${crmField('ql-terms', 'Terms', crmInput('ql-terms', (q && q.quotation && q.quotation.terms) || '', open ? '' : 'disabled'))}</div>
      <p class="crm-small">Total <b id="ql-total"></b></p>
      ${open ? `<div class="crm-form-actions"><button class="btn btn-outline" type="button" id="ql-save">Save quotation</button><button class="btn btn-primary" type="button" id="ql-submit">Save and send for approval</button></div>
        <p class="text-muted crm-small">Editing an approved quotation sends it back for approval.</p>` : ''}
      <h3 class="crm-section">History</h3><ol class="crm-timeline">${(o.history || []).map(h => `<li><time>${crmDate(h.changed_at, true)}</time><span class="crm-tl-what">${crmEsc(h.field)}: ${crmEsc(h.from || '')} → ${crmEsc(h.to || '')}</span><span class="crm-tl-detail">${crmEsc(h.changed_by)}</span></li>`).join('')}</ol>
    </section></div>`;
  body.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
  const tbody = body.querySelector('#ql tbody');
  const rows = lines.map(l => ({ sku: l.sku, qty: l.qty, rate: l.rate, name: l.name }));
  const paintLines = () => {
    tbody.innerHTML = rows.map((l, i) => `<tr><td>${open ? `<input class="form-input" data-q="sku" data-i="${i}" value="${crmEsc(l.sku)}">` : crmEsc(l.sku)}${l.name ? `<small class="crm-sub">${crmEsc(l.name)}</small>` : ''}</td>
      <td class="num">${open ? `<input class="form-input crm-num" data-q="qty" data-i="${i}" value="${crmEsc(l.qty)}" inputmode="numeric">` : l.qty}</td>
      <td class="num">${open ? `<input class="form-input crm-num" data-q="rate" data-i="${i}" value="${crmEsc(l.rate)}" inputmode="decimal">` : crmMoney(l.rate)}</td>
      <td class="num">${crmMoney((Number(l.qty) || 0) * (Number(l.rate) || 0))}</td><td>${open ? `<button class="action-btn" data-qrm="${i}">Remove</button>` : ''}</td></tr>`).join('') || '<tr><td colspan="5" class="text-muted">No lines yet.</td></tr>';
    body.querySelector('#ql-total').textContent = crmMoney(rows.reduce((a, l) => a + (Number(l.qty) || 0) * (Number(l.rate) || 0), 0));
  };
  paintLines();
  tbody.addEventListener('change', (e) => { const el = e.target; if (el.dataset.q) { rows[Number(el.dataset.i)][el.dataset.q] = el.dataset.q === 'sku' ? el.value.trim() : Number(el.value); paintLines(); } });
  tbody.addEventListener('click', (e) => { const b = e.target.closest('[data-qrm]'); if (b) { rows.splice(Number(b.dataset.qrm), 1); paintLines(); } });
  body.querySelector('#ql-add')?.addEventListener('click', () => { rows.push({ sku: '', qty: 1, rate: 0 }); paintLines(); });
  const saveQ = async (submit) => {
    const r = await crmApi(`opportunities/${encodeURIComponent(id)}/quotation`, { method: 'POST', body: { lines: rows.map(l => ({ sku: l.sku, qty: Number(l.qty) || 0, rate: Number(l.rate) || 0 })), valid_until: crmVal(body, 'ql-vu'), terms: crmVal(body, 'ql-terms'), submit } });
    if (r) { showToast(submit ? 'Sent for approval (Approvals).' : 'Quotation saved.', { variant: 'success' }); crmDeal(host, id); }
  };
  body.querySelector('#ql-save')?.addEventListener('click', () => saveQ(false));
  body.querySelector('#ql-submit')?.addEventListener('click', () => saveQ(true));
  body.querySelector('#dl-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi('opportunities/' + encodeURIComponent(id), { method: 'PUT', body: { title: crmVal(body, 'dl-t'), owner: crmVal(body, 'dl-o'), value: Number(crmVal(body, 'dl-v')) || 0, probability: Number(crmVal(body, 'dl-p')) || 0, expected_close: crmVal(body, 'dl-d') } });
    if (r) { showToast('Saved.', { variant: 'success' }); crmDeal(host, id); }
  });
  body.querySelector('#dl-move')?.addEventListener('click', async () => {
    const r = await crmApi(`opportunities/${encodeURIComponent(id)}/move`, { method: 'POST', body: { stage_id: crmVal(body, 'dl-s'), lost_reason: crmVal(body, 'dl-lr') } });
    if (r) { showToast(r.status === 'Won' ? `Won - Sales Order ${r.sales_order_id || ''} raised.` : `Moved to ${r.stage}.`, { variant: 'success' }); crmDeal(host, id); }
  });
  body.querySelector('#dl-order')?.addEventListener('click', async () => { if (await crmApi(`opportunities/${encodeURIComponent(id)}/order`, { method: 'POST', body: {} })) crmDeal(host, id); });
  body.querySelector('#dl-fu').addEventListener('click', () => crmGo('worklist', 'new-task', { customer_id: o.customer_id, name: o.customer_name, opportunity_id: id, kind: 'call', title: 'Call about ' + o.title }));
}

async function crmLeads(out) {
  out.innerHTML = `<div class="crm-filters">${crmField('ld-s', 'Show', crmSelect('ld-s', [['open', 'Open'], ['unassigned', 'Unassigned'], ['mine', 'Mine'], ['Converted', 'Converted'], ['Disqualified', 'Disqualified'], ['all', 'All']], 'open'))}</div><div id="ld-list"></div>`;
  const load = async () => {
    const v = crmVal(out, 'ld-s');
    const q = v === 'unassigned' ? 'unassigned=1' : (v === 'mine' ? 'mine=1' : 'status=' + encodeURIComponent(v));
    const r = await crmApi('leads?' + q);
    if (!r) return;
    out.querySelector('#ld-list').innerHTML = crmTable([{ label: 'Lead', html: l => `<a href="#" data-lead="${crmEsc(l.id)}">${crmEsc(l.name)}</a><small class="crm-sub">${crmEsc(l.company || '')}</small>` },
      { label: 'Source', key: 'source' }, { label: 'Owner', html: l => crmEsc(l.owner || 'Unassigned') }, { label: 'Status', html: l => crmStatus(l.status) }, { label: 'Added', html: l => crmDate(l.created_at) }],
      r.leads, { empty: 'No leads here.' });
    out.querySelectorAll('[data-lead]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('pipeline', 'lead', { id: a.dataset.lead }); }));
  };
  out.querySelector('#ld-s').addEventListener('change', load);
  await load();
}

async function crmLeadNew(host) {
  const body = crmFrame(host, { title: 'New lead', back: () => crmGo('pipeline', '', { view: 'leads' }), backLabel: 'Leads' });
  body.innerHTML = `<form class="crm-form" id="nl-form"><div class="crm-form-row">${crmField('nl-n', 'Name', crmInput('nl-n'))}${crmField('nl-c', 'Company', crmInput('nl-c'))}</div>
    <div class="crm-form-row">${crmField('nl-p', 'Phone', crmInput('nl-p'))}${crmField('nl-e', 'Email', crmInput('nl-e'))}${crmField('nl-s', 'Source', crmSelect('nl-s', crmMeta.lead_sources || ['Other']))}
    ${crmField('nl-o', 'Owner (blank = by the assignment rules)', crmInput('nl-o'))}</div>${crmField('nl-x', 'Notes', crmInput('nl-x'))}
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Add</button></div></form>`;
  body.querySelector('#nl-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const l = await crmApi('leads', { method: 'POST', body: { name: crmVal(body, 'nl-n'), company: crmVal(body, 'nl-c'), phone: crmVal(body, 'nl-p'), email: crmVal(body, 'nl-e'), source: crmVal(body, 'nl-s'), owner: crmVal(body, 'nl-o'), notes: crmVal(body, 'nl-x') } });
    if (l) crmGo('pipeline', 'lead', { id: l.id });
  });
}

async function crmLead(host, id) {
  const l = await crmApi('leads/' + encodeURIComponent(id));
  const body = crmFrame(host, { title: l ? l.name : 'Lead', back: () => crmGo('pipeline', '', { view: 'leads' }), backLabel: 'Leads', actions: l ? crmStatus(l.status) : '' });
  if (!l) return;
  const tasks = ((await crmApi('tasks?status=all&lead_id=' + encodeURIComponent(id))) || {}).tasks || [];
  const done = l.status === 'Converted';
  body.innerHTML = `<dl class="crm-kv"><dt>Company</dt><dd>${crmEsc(l.company || '-')}</dd><dt>Phone</dt><dd>${crmEsc(l.phone || '-')}</dd><dt>Email</dt><dd>${crmEsc(l.email || '-')}</dd>
      <dt>Source</dt><dd>${crmEsc(l.source)}</dd><dt>Owner</dt><dd>${crmEsc(l.owner || 'Unassigned')}</dd><dt>Notes</dt><dd>${crmEsc(l.notes || '')}</dd>
      ${l.disqualify_reason ? `<dt>Disqualified</dt><dd>${crmEsc(l.disqualify_reason)}</dd>` : ''}</dl>
    ${done ? `<p>Converted: <a href="#" data-cust="${crmEsc(l.customer_id)}">the customer</a> · <a href="#" data-deal="${crmEsc(l.opportunity_id)}">the deal</a></p>` : `
    <div class="crm-actions-row"><button class="btn btn-outline btn-sm" data-ls="Contacted">Contacted</button><button class="btn btn-outline btn-sm" data-ls="Qualified">Qualified</button><button class="btn btn-outline btn-sm" data-ls="Disqualified">Disqualify</button></div>
    <h3 class="crm-section">Convert to a customer and a deal</h3><form class="crm-form crm-form-inline" id="lc-form">${crmField('lc-t', 'Deal', crmInput('lc-t', (l.company || l.name) + ' - first order'))}
      ${crmField('lc-v', 'Value', crmInput('lc-v', '', 'inputmode="decimal"'))}${crmField('lc-d', 'Expected close', crmInput('lc-d', '', 'type="date"'))}<button class="btn btn-primary" type="submit">Convert</button></form>
    <p class="text-muted crm-small">Converting again returns the same customer and deal. A phone number already on a customer joins that customer.</p>`}
    <h3 class="crm-section">Follow-ups</h3>${crmTable([{ label: 'Task', html: t => `${crmChip(t.kind_label)} ${crmEsc(t.title)}` }, { label: 'Due', html: t => crmDate(t.due_at, true) }, { label: 'Status', html: t => crmStatus(t.status) }, { label: 'Outcome', key: 'outcome' }], tasks, { empty: 'No follow-ups.' })}
    <button class="btn btn-outline btn-sm" type="button" id="ld-fu">+ Call, meeting or task</button>`;
  body.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
  body.querySelectorAll('[data-deal]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('pipeline', 'deal', { id: a.dataset.deal }); }));
  body.querySelectorAll('[data-ls]').forEach(b => b.addEventListener('click', async () => {
    let reason = '';
    if (b.dataset.ls === 'Disqualified') { reason = await showCustomPrompt('Why is the lead disqualified?', '', 'Disqualify'); if (!reason) return; }
    if (await crmApi(`leads/${encodeURIComponent(id)}/status`, { method: 'POST', body: { status: b.dataset.ls, reason } })) crmLead(host, id);
  }));
  body.querySelector('#lc-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi(`leads/${encodeURIComponent(id)}/convert`, { method: 'POST', body: { title: crmVal(body, 'lc-t'), value: Number(crmVal(body, 'lc-v')) || 0, expected_close: crmVal(body, 'lc-d') } });
    if (r) crmGo('pipeline', 'deal', { id: r.opportunity_id });
  });
  body.querySelector('#ld-fu').addEventListener('click', () => crmGo('worklist', 'new-task', { lead_id: id, kind: 'call', title: 'Call ' + l.name }));
}

async function crmForecast(out) {
  const r = await crmApi('pipeline/forecast');
  if (!r) return;
  out.innerHTML = crmTable([{ label: 'Month', key: 'month' }, { label: 'Open deals', key: 'open', num: true }, { label: 'Pipeline', num: true, html: x => crmMoney(x.pipeline) },
    { label: 'Weighted', num: true, html: x => crmMoney(x.weighted) }, { label: 'Committed', num: true, html: x => crmMoney(x.committed) }, { label: 'Booked (won)', num: true, html: x => crmMoney(x.booked) }],
    r.rows, { empty: 'No deals yet.' }) +
    `<p class="text-muted crm-small">Weighted = value × chance. Committed = deals in a stage marked committed. Booked = won deals' orders. They are never added together.</p>
    <form class="crm-form crm-form-inline" id="fc-form">${crmField('fc-l', 'Snapshot name', crmInput('fc-l', '', 'placeholder="e.g. Oct 2026"'))}<button class="btn btn-outline" type="submit">Take a snapshot</button></form>
    <h3 class="crm-section">Snapshots (never changed once taken)</h3>${(r.snapshots || []).map(s => `<details class="crm-details"><summary>${crmEsc(s.label)} - ${crmDate(s.taken_at, true)} by ${crmEsc(s.taken_by)}</summary>
      ${crmTable([{ label: 'Month', key: 'month' }, { label: 'Weighted', num: true, html: x => crmMoney(x.weighted) }, { label: 'Committed', num: true, html: x => crmMoney(x.committed) }, { label: 'Booked', num: true, html: x => crmMoney(x.booked) }], s.rows)}</details>`).join('') || '<p class="text-muted">None yet.</p>'}`;
  out.querySelector('#fc-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    if (await crmApi('pipeline/forecast', { method: 'POST', body: { label: crmVal(out, 'fc-l') } })) crmForecast(out);
  });
}

async function crmPipelineSetup(out, pre) {
  const [st, rules] = pre ? [pre.st, pre.rules] : await Promise.all([crmApi('pipeline/stages'), crmApi('pipeline/lead-rules')]);
  if (!st || !rules) return;
  const stages = st.stages.filter(s => s.kind === 'open');
  const req = [['value', 'a value'], ['expected_close', 'a close date'], ['quotation', 'a quotation'], ['owner', 'an owner']];
  out.innerHTML = `<h3 class="crm-section">Stages</h3><table class="crm-table" id="ps"><thead><tr><th>Stage</th><th class="num">Chance %</th><th>To enter, needs</th><th>Committed</th><th>In use</th></tr></thead><tbody>
    ${stages.map((s, i) => `<tr data-i="${i}"><td>${crmInput('ps-n-' + i, s.name)}</td><td class="num">${crmInput('ps-p-' + i, s.probability, 'inputmode="numeric" class="form-input crm-num"')}</td>
      <td>${req.map(([k, l]) => `<label class="crm-check"><input type="checkbox" data-req="${k}" ${(s.requires || []).includes(k) ? 'checked' : ''}> ${l}</label>`).join(' ')}</td>
      <td><input type="checkbox" data-com ${s.committed ? 'checked' : ''}></td><td><input type="checkbox" data-act ${s.active ? 'checked' : ''}></td></tr>`).join('')}</tbody></table>
    <button class="btn btn-outline btn-sm" type="button" id="ps-add">+ Stage</button> <button class="btn btn-primary btn-sm" type="button" id="ps-save">Save stages</button>
    <p class="text-muted crm-small">Won (needs an approved quotation) and Lost (needs a reason) are always there.</p>
    <h3 class="crm-section">Who gets new leads</h3><table class="crm-table" id="lr"><thead><tr><th>Source</th><th>Goes to (user name)</th><th></th></tr></thead><tbody>
    ${rules.rules.map((r, i) => `<tr><td>${crmSelect('lr-s-' + i, ['Any'].concat(crmMeta.lead_sources || []), r.source)}</td><td>${crmInput('lr-o-' + i, r.owner)}</td><td><button class="action-btn" data-lrm="${i}">Remove</button></td></tr>`).join('')}</tbody></table>
    <button class="btn btn-outline btn-sm" type="button" id="lr-add">+ Rule</button> <button class="btn btn-primary btn-sm" type="button" id="lr-save">Save rules</button>
    <p class="text-muted crm-small">Among the rules that fit a lead's source, it goes to the person with the fewest open leads. No rule: it waits in Unassigned.</p>`;
  const readStages = () => [...out.querySelectorAll('#ps tbody tr')].map((tr, i) => ({ id: stages[i] ? stages[i].id : '', name: crmVal(out, 'ps-n-' + i), probability: Number(crmVal(out, 'ps-p-' + i)) || 0,
    requires: [...tr.querySelectorAll('[data-req]:checked')].map(x => x.dataset.req), committed: tr.querySelector('[data-com]').checked, active: tr.querySelector('[data-act]').checked }));
  out.querySelector('#ps-add').addEventListener('click', () => {
    const list = readStages();
    list.push({ id: '', name: 'New stage', probability: 50, requires: [], committed: false, active: true });
    crmPipelineSetup(out, { st: { stages: list.map(s => ({ ...s, kind: 'open' })) }, rules });
  });
  out.querySelector('#ps-save').addEventListener('click', async () => { if (await crmApi('pipeline/stages', { method: 'PUT', body: { stages: readStages() } })) { showToast('Stages saved.', { variant: 'success' }); crmPipelineSetup(out); } });
  const readRules = () => rules.rules.map((_, i) => ({ source: crmVal(out, 'lr-s-' + i), owner: crmVal(out, 'lr-o-' + i) }));
  out.querySelector('#lr-add').addEventListener('click', async () => { const list = readRules(); list.push({ source: 'Any', owner: '' }); rules.rules = list; crmPipelineSetup(out, { st: { stages: readStages().map(s => ({ ...s, kind: 'open' })) }, rules }); });
  out.querySelectorAll('[data-lrm]').forEach(b => b.addEventListener('click', () => { const list = readRules(); list.splice(Number(b.dataset.lrm), 1); rules.rules = list; crmPipelineSetup(out, { st: { stages: readStages().map(s => ({ ...s, kind: 'open' })) }, rules }); }));
  out.querySelector('#lr-save').addEventListener('click', async () => { if (await crmApi('pipeline/lead-rules', { method: 'PUT', body: { rules: readRules() } })) { showToast('Rules saved.', { variant: 'success' }); crmPipelineSetup(out); } });
}

