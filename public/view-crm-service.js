// Stage 101.5 / 101.11.3 / 101.12: the CRM's service pages - the WhatsApp
// inbox, tickets, feedback (and referrals), and the store worklist (tasks,
// appointments, hold requests). Loaded by view-crm.js; its helpers are
// globals published by the shell's loader.

export async function crmRenderTab(tab, host, st) {
  const pages = { inbox: crmInbox, tickets: crmTickets, feedback: crmFeedback, worklist: crmWorklist };
  return pages[tab](host, st);
}

const crmCustLink = (id, name) => id ? `<a href="#" data-cust="${crmEsc(id)}">${crmEsc(name || id)}</a>` : crmEsc(name || '');
function crmWireCust(root) {
  root.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
}

// --- inbox -----------------------------------------------------------------

async function crmInbox(host, st) {
  if (st.page === 'thread') return crmThread(host, st.params.id);
  const body = crmFrame(host, { title: 'Inbox' });
  body.innerHTML = `<div class="crm-filters">${crmField('ib-s', 'Show', crmSelect('ib-s', [['Open', 'Open'], ['Closed', 'Closed'], ['', 'All']], (st.params || {}).status ?? 'Open'))}</div><div id="ib-list"></div>
    <p class="text-muted crm-small">Customers' WhatsApp replies arrive here (set the WhatsApp sender's callback URL at Meta). A reply is never consent. STOP stops offers on WhatsApp.</p>`;
  const load = async () => {
    const r = await crmApi('conversations?status=' + encodeURIComponent(crmVal(body, 'ib-s')));
    if (!r) return;
    body.querySelector('#ib-list').innerHTML = crmTable([
      { label: 'From', html: c => `<a href="#" data-th="${crmEsc(c.id)}">${crmEsc(c.customer_name || c.phone)}</a>${c.unread ? ` <span class="badge">${c.unread}</span>` : ''}` },
      { label: 'Phone', key: 'phone' }, { label: 'Handled by', html: c => c.mode === 'human' ? crmEsc(c.assigned_to || 'a person') : 'Automatic replies' },
      { label: 'Reply window', html: c => c.window_open ? crmChip('Open', 'ok') : crmChip('Closed - templates only', 'muted') },
      { label: 'Last message', html: c => crmDate(c.last_inbound_at || c.updated_at, true) }, { label: 'Ticket', key: 'ticket_id' }],
      r.conversations, { empty: 'No conversations.' });
    body.querySelectorAll('[data-th]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('inbox', 'thread', { id: a.dataset.th }); }));
  };
  body.querySelector('#ib-s').addEventListener('change', load);
  await load();
}

async function crmThread(host, id) {
  const c = await crmApi('conversations/' + encodeURIComponent(id));
  const body = crmFrame(host, { title: c ? (c.customer_name || c.phone) : 'Conversation', back: () => crmGo('inbox'), backLabel: 'Inbox' });
  if (!c) return;
  const tpls = ((await crmApi('templates?channel=WhatsApp')) || {}).templates || [];
  body.innerHTML = `<div class="crm-chips">${c.window_open ? crmChip('Reply window open', 'ok') : crmChip('Reply window closed - send a template', 'warn')}
      ${c.mode === 'human' ? crmChip('Taken over by ' + (c.assigned_to || 'a person')) : crmChip('Automatic replies')} ${c.customer_id ? crmCustLink(c.customer_id, 'Open the customer') : crmChip('Not a customer yet', 'muted')}</div>
    <div class="crm-actions-row">${c.mode === 'human' ? '<button class="btn btn-outline btn-sm" data-act="hand_back">Hand back</button>' : '<button class="btn btn-outline btn-sm" data-act="take_over">Take over</button>'}
      ${c.status === 'Open' ? '<button class="btn btn-outline btn-sm" data-act="close">Close</button>' : '<button class="btn btn-outline btn-sm" data-act="reopen">Reopen</button>'}
      ${c.ticket_id ? `<span class="crm-small">Ticket ${crmEsc(c.ticket_id)}</span>` : (c.customer_id ? '<button class="btn btn-outline btn-sm" id="th-ticket">Make a ticket</button>' : '')}</div>
    <ol class="crm-thread">${(c.messages || []).map(m => `<li class="crm-msg crm-msg-${m.direction}"><span>${crmEsc(m.body)}</span><small>${crmDate(m.created_at, true)}${m.sent_by ? ' · ' + crmEsc(m.sent_by) : ''}${m.status ? ' · ' + crmEsc(m.status) : ''}</small></li>`).join('') || '<li class="text-muted">No messages.</li>'}</ol>
    <form class="crm-form" id="th-form">${crmField('th-t', 'Template (needed when the window is closed)', crmSelect('th-t', [['', c.window_open ? 'None - type a reply' : 'Choose a template']].concat(tpls.filter(t => t.status === 'Active').map(t => [t.id, t.name]))))}
      ${crmField('th-x', 'Reply', '<textarea class="form-input" id="th-x" rows="2" maxlength="2000"></textarea>')}
      <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Send</button></div></form><div id="th-out"></div>`;
  crmWireCust(body);
  body.querySelectorAll('[data-act]').forEach(b => b.addEventListener('click', async () => {
    if (await crmApi(`conversations/${encodeURIComponent(id)}/state`, { method: 'POST', body: { action: b.dataset.act } })) crmThread(host, id);
  }));
  body.querySelector('#th-ticket')?.addEventListener('click', async () => {
    const last = (c.messages || []).filter(m => m.direction === 'in').pop();
    const t = await crmApi('tickets', { method: 'POST', body: { customer_id: c.customer_id, channel: 'WhatsApp', description: last ? last.body : 'WhatsApp conversation', priority: 'Medium' } });
    if (t && await crmApi(`conversations/${encodeURIComponent(id)}/state`, { method: 'POST', body: { action: 'ticket', value: t.id } })) crmThread(host, id);
  });
  body.querySelector('#th-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi(`conversations/${encodeURIComponent(id)}/reply`, { method: 'POST', body: { text: body.querySelector('#th-x').value.trim(), template_id: crmVal(body, 'th-t') } });
    if (!r) return;
    if (['Suppressed', 'Cancelled', 'Expired'].includes(r.status)) body.querySelector('#th-out').innerHTML = `<div class="crm-note crm-note-warn">Not sent: ${crmWhy(r)}</div>`;
    else crmThread(host, id);
  });
}

// --- tickets ----------------------------------------------------------------

async function crmTickets(host, st) {
  if (st.page === 'ticket') return crmTicket(host, st.params.id);
  if (st.page === 'new') return crmTicketNew(host, st.params);
  const body = crmFrame(host, { title: 'Tickets', actions: '<button class="btn btn-primary" type="button" id="tk-new">+ New ticket</button>' });
  host.querySelector('#tk-new').addEventListener('click', () => crmGo('tickets', 'new', {}));
  body.innerHTML = `<div class="crm-filters">${crmField('tk-s', 'Show', crmSelect('tk-s', [['open', 'Open'], ['late', 'Late'], ['', 'All']], 'open'))}</div><div id="tk-list"></div>`;
  const load = async () => {
    const r = await crmApi('tickets?state=' + encodeURIComponent(crmVal(body, 'tk-s')));
    if (!r) return;
    body.querySelector('#tk-list').innerHTML = crmTable([
      { label: 'Ticket', html: t => `<a href="#" data-tk="${crmEsc(t.id)}">${crmEsc(t.id)}</a><small class="crm-sub">${crmEsc((t.description || '').slice(0, 80))}</small>` },
      { label: 'Customer', html: t => crmCustLink(t.customer, t.customer_name) }, { label: 'Store', key: 'store' }, { label: 'State', html: t => crmStatus(t.state) },
      { label: 'Priority', key: 'priority' }, { label: 'Respond by', html: t => `${crmDate(t.first_response_due, true)}${t.first_response_late ? ' ' + crmChip('Late', 'bad') : ''}` },
      { label: 'Resolve by', html: t => `${crmDate(t.resolve_due, true)}${t.resolve_late ? ' ' + crmChip('Late', 'bad') : ''}` }],
      r.tickets, { empty: 'No tickets.' });
    body.querySelectorAll('[data-tk]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('tickets', 'ticket', { id: a.dataset.tk }); }));
    crmWireCust(body);
  };
  body.querySelector('#tk-s').addEventListener('change', load);
  await load();
}

async function crmTicketNew(host, p) {
  const body = crmFrame(host, { title: 'New ticket', back: () => crmGo('tickets'), backLabel: 'Tickets' });
  body.innerHTML = `<form class="crm-form" id="tn-form">${crmField('tn-c', 'Customer', crmInput('tn-c', p.name || ''))}
    <div class="crm-form-row">${crmField('tn-b', 'Bill (optional)', crmInput('tn-b', p.bill_id || ''))}${crmField('tn-s', 'Store (blank = yours)', crmInput('tn-s'))}
    ${crmField('tn-ch', 'Raised by', crmSelect('tn-ch', ['Store', 'Phone', 'WhatsApp', 'Email']))}${crmField('tn-p', 'Priority', crmSelect('tn-p', ['Medium', 'Low', 'High', 'Critical']))}</div>
    ${crmField('tn-d', 'What the customer needs', '<textarea class="form-input" id="tn-d" rows="3"></textarea>')}
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Open the ticket</button></div></form>`;
  const pick = crmCustomerPicker(body.querySelector('#tn-c'), p.customer_id);
  body.querySelector('#tn-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const t = await crmApi('tickets', { method: 'POST', body: { customer_id: pick(), bill_id: crmVal(body, 'tn-b'), store: crmVal(body, 'tn-s'), channel: crmVal(body, 'tn-ch'), priority: crmVal(body, 'tn-p'), description: body.querySelector('#tn-d').value.trim() } });
    if (t) crmGo('tickets', 'ticket', { id: t.id });
  });
}

async function crmTicket(host, id) {
  const t = await crmApi('tickets/' + encodeURIComponent(id));
  const body = crmFrame(host, { title: 'Ticket ' + id, back: () => crmGo('tickets'), backLabel: 'Tickets', actions: t ? crmStatus(t.state) : '' });
  if (!t) return;
  const open = !['Resolved', 'Closed', 'Cancelled'].includes(t.status);
  body.innerHTML = `<dl class="crm-kv"><dt>Customer</dt><dd>${crmCustLink(t.customer, t.customer_name)}</dd><dt>Bill</dt><dd>${crmEsc(t.bill_id || '-')}</dd><dt>Store</dt><dd>${crmEsc(t.store || '-')}</dd>
      <dt>Raised by</dt><dd>${crmEsc(t.channel || '-')}</dd><dt>Priority</dt><dd>${crmEsc(t.priority)}</dd><dt>Assigned to</dt><dd>${crmEsc(t.assigned_to || '-')}</dd>
      <dt>First response</dt><dd>${t.first_response_at ? crmDate(t.first_response_at, true) : 'due ' + crmDate(t.first_response_due, true)}${t.first_response_late ? ' ' + crmChip('Late', 'bad') : ''}</dd>
      <dt>Resolution</dt><dd>${t.resolved_at ? crmDate(t.resolved_at, true) : 'due ' + crmDate(t.resolve_due, true)}${t.resolve_late ? ' ' + crmChip('Late', 'bad') : ''}</dd></dl>
    <p>${crmEsc(t.description)}</p>
    ${open ? `<div class="crm-actions-row">${t.status === 'Draft' ? '<button class="btn btn-outline btn-sm" data-act="assign">Assign to me</button>' : ''}
      ${t.status !== 'InProgress' ? '<button class="btn btn-outline btn-sm" data-act="start">Start</button>' : ''}
      ${t.state === 'Open' ? '<button class="btn btn-outline btn-sm" data-act="wait_customer">Waiting on customer</button><button class="btn btn-outline btn-sm" data-act="wait_internal">Waiting internally</button>' : ''}
      ${t.state.startsWith('Waiting') ? '<button class="btn btn-outline btn-sm" data-act="resume">Back to open</button>' : ''}
      <button class="btn btn-primary btn-sm" data-act="resolve">Resolve</button><button class="btn btn-outline btn-sm" data-act="cancel">Cancel</button></div>`
      : (t.status === 'Resolved' ? '<button class="btn btn-outline btn-sm" data-act="close">Close</button>' : '')}
    <h3 class="crm-section">Notes and replies</h3>
    <ol class="crm-thread">${(t.notes || []).map(n => `<li class="crm-msg ${n.private ? 'crm-msg-note' : 'crm-msg-out'}"><span>${crmEsc(n.note)}</span><small>${n.private ? 'Private note' : 'Sent to the customer'} · ${crmEsc(n.created_by)} · ${crmDate(n.created_at, true)}</small></li>`).join('') || '<li class="text-muted">Nothing yet.</li>'}</ol>
    <form class="crm-form" id="tk-note"><textarea class="form-input" id="tk-x" rows="2"></textarea><div class="crm-form-row">
      ${crmField('tk-how', 'As', crmSelect('tk-how', [['private', 'A private note (never sent)'], ['WhatsApp', 'A reply by WhatsApp'], ['SMS', 'A reply by SMS (needs a template - use WhatsApp or email)'], ['Email', 'A reply by email']]))}
      <button class="btn btn-outline" type="submit">Add</button></div></form>`;
  crmWireCust(body);
  body.querySelectorAll('[data-act]').forEach(b => b.addEventListener('click', async () => {
    let value = '';
    if (b.dataset.act === 'resolve') { value = await showCustomPrompt('What was done?', '', 'Resolve the ticket'); if (!value) return; }
    if (b.dataset.act === 'cancel') { value = await showCustomPrompt('Why is it cancelled?', '', 'Cancel the ticket'); if (!value) return; }
    if (await crmApi(`tickets/${encodeURIComponent(id)}/state`, { method: 'POST', body: { action: b.dataset.act, value } })) crmTicket(host, id);
  }));
  body.querySelector('#tk-note').addEventListener('submit', async (e) => {
    e.preventDefault();
    const how = crmVal(body, 'tk-how');
    if (await crmApi(`tickets/${encodeURIComponent(id)}/notes`, { method: 'POST', body: { note: body.querySelector('#tk-x').value.trim(), private: how === 'private', channel: how === 'private' ? '' : how } })) crmTicket(host, id);
  });
}

// --- feedback and referrals -----------------------------------------------------

async function crmFeedback(host, st) {
  if (st.page === 'survey') return crmSurveyEdit(host, st.params.id);
  if (st.page === 'summary') return crmSurveySummary(host, st.params.id);
  const body = crmFrame(host, { title: 'Feedback', actions: crmMeta.can_manage ? '<button class="btn btn-primary" type="button" id="sv-new">+ New survey</button>' : '' });
  host.querySelector('#sv-new')?.addEventListener('click', () => crmGo('feedback', 'survey', {}));
  const [r, refs] = await Promise.all([crmApi('surveys'), crmApi('referrals')]);
  if (!r) return;
  body.innerHTML = crmTable([{ label: 'Survey', html: s => `<a href="#" data-sv="${crmEsc(s.id)}">${crmEsc(s.name)}</a><small class="crm-sub">${crmEsc(s.question)}</small>` },
    { label: 'On', html: s => s.enabled ? crmChip('On', 'ok') : crmChip('Off', 'muted') }, { label: 'Scale', html: s => s.scale === 'nps' ? '0-10 (NPS)' : '1-5 rating' }, { label: 'By', key: 'channel' },
    { label: '', html: s => `<button class="action-btn" data-sum="${crmEsc(s.id)}">Results</button>` }], r.surveys,
    { empty: 'No survey yet. Journeys > Add the ready-made journeys adds "After a bill" (off until you switch it on).' }) +
    `<p class="text-muted crm-small">When a survey is on, each bill's customer gets one link (not again within its cool-down). A low score opens a ticket for the store.</p>
    <h3 class="crm-section">Referrals</h3>
    <form class="crm-form crm-form-inline" id="rf-form">${crmField('rf-f', 'New customer (the friend)', crmInput('rf-f'))}${crmField('rf-c', 'Referral code they gave', crmInput('rf-c'))}<button class="btn btn-outline" type="submit">Record</button></form>
    ${crmTable([{ label: 'Referred by', html: x => crmCustLink(x.referrer_id, x.referrer_name) }, { label: 'Friend', html: x => crmCustLink(x.friend_id, x.friend_name) }, { label: 'Code', key: 'code' },
      { label: 'Status', html: x => crmStatus(x.status) + (x.flag_reason ? `<small class="crm-reason">${crmEsc(x.flag_reason)}</small>` : '') },
      { label: 'Reward on', html: x => x.qualifies_on ? crmDate(x.qualifies_on) : 'after the first bill' }, { label: 'Points', key: 'reward_points', num: true },
      { label: '', html: x => x.status === 'Flagged' && crmMeta.can_manage ? `<button class="action-btn" data-rf="${crmEsc(x.id)}" data-ok="1">Allow</button> <button class="action-btn" data-rf="${crmEsc(x.id)}" data-ok="0">Cancel</button>` : '' }],
      (refs || {}).referrals || [], { empty: 'No referrals yet. A customer\'s code is on their customer page.' })}`;
  body.querySelectorAll('[data-sv]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('feedback', 'survey', { id: a.dataset.sv }); }));
  body.querySelectorAll('[data-sum]').forEach(b => b.addEventListener('click', () => crmGo('feedback', 'summary', { id: b.dataset.sum })));
  crmWireCust(body);
  const friend = crmCustomerPicker(body.querySelector('#rf-f'));
  body.querySelector('#rf-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const x = await crmApi('referrals', { method: 'POST', body: { friend_id: friend(), code: crmVal(body, 'rf-c') } });
    if (x) { showToast(x.status === 'Flagged' ? 'Recorded, but flagged: ' + x.flag_reason : 'Recorded. The reward follows the friend\'s first bill.', { variant: x.status === 'Flagged' ? 'warning' : 'success' }); crmFeedback(host, st); }
  });
  body.querySelectorAll('[data-rf]').forEach(b => b.addEventListener('click', async () => {
    if (await crmApi(`referrals/${encodeURIComponent(b.dataset.rf)}/decide`, { method: 'POST', body: { approve: b.dataset.ok === '1' } })) crmFeedback(host, st);
  }));
}

async function crmSurveyEdit(host, id) {
  let s = { scale: 'nps', channel: 'WhatsApp', delay_hours: 2, cooldown_days: 30, low_score: 6, enabled: false };
  if (id) { const r = await crmApi('surveys'); s = ((r || {}).surveys || []).find(x => x.id === id) || s; }
  const tpls = ((await crmApi('templates')) || {}).templates || [];
  const body = crmFrame(host, { title: id ? s.name : 'New survey', back: () => crmGo('feedback'), backLabel: 'Feedback' });
  body.innerHTML = `<form class="crm-form" id="sv-form"><div class="crm-form-row">${crmField('sv-n', 'Name', crmInput('sv-n', s.name || ''))}
    ${crmField('sv-sc', 'Scale', crmSelect('sv-sc', [['nps', '0-10 (NPS)'], ['rating', '1-5 stars']], s.scale))}</div>
    ${crmField('sv-q', 'Question', crmInput('sv-q', s.question || 'How likely are you to recommend us to a friend?'))}
    <div class="crm-form-row">${crmField('sv-ch', 'Send by', crmSelect('sv-ch', ['WhatsApp', 'SMS', 'Email'], s.channel))}
    ${crmField('sv-t', 'Message (must contain the feedback link)', crmSelect('sv-t', [['', 'A plain message with the link']].concat(tpls.filter(t => t.purpose === 'service').map(t => [t.id, `${t.name} (${t.channel})`])), s.template_id || ''))}</div>
    <div class="crm-form-row">${crmField('sv-d', 'Hours after the bill', crmInput('sv-d', s.delay_hours, 'inputmode="numeric"'))}${crmField('sv-cd', 'Not again for (days)', crmInput('sv-cd', s.cooldown_days, 'inputmode="numeric"'))}
    ${crmField('sv-low', 'A score this low or lower opens a ticket', crmInput('sv-low', s.low_score, 'inputmode="numeric"'))}</div>
    <label class="crm-check"><input type="checkbox" id="sv-on" ${s.enabled ? 'checked' : ''}> On - ask after every bill</label>
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Save</button></div>
    <p class="text-muted crm-small">Changing the question or scale starts a new version; results are always read against the question they answered.</p></form>`;
  body.querySelector('#sv-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi(id ? 'surveys/' + encodeURIComponent(id) : 'surveys', { method: id ? 'PUT' : 'POST', body: { name: crmVal(body, 'sv-n'), question: crmVal(body, 'sv-q'), scale: crmVal(body, 'sv-sc'),
      channel: crmVal(body, 'sv-ch'), template_id: crmVal(body, 'sv-t'), delay_hours: Number(crmVal(body, 'sv-d')), cooldown_days: Number(crmVal(body, 'sv-cd')), low_score: Number(crmVal(body, 'sv-low')), enabled: crmVal(body, 'sv-on') } });
    if (r) { showToast('Saved.', { variant: 'success' }); crmGo('feedback'); }
  });
}

async function crmSurveySummary(host, id) {
  const body = crmFrame(host, { title: 'Survey results', back: () => crmGo('feedback'), backLabel: 'Feedback' });
  const r = await crmApi(`surveys/${encodeURIComponent(id)}/summary`);
  if (!r) return;
  body.innerHTML = crmTable([{ label: 'Version', key: 'version', num: true }, { label: 'Question', key: 'question' }, { label: 'Asked', key: 'asked', num: true }, { label: 'Answers', key: 'responses', num: true },
    { label: 'Promoters', key: 'promoters', num: true }, { label: 'Detractors', key: 'detractors', num: true }, { label: 'NPS', html: v => v.nps == null ? (v.average == null ? '-' : 'avg ' + v.average) : String(v.nps), num: true }], r.versions) +
    `<h3 class="crm-section">Latest answers</h3>` + crmTable([{ label: 'When', html: x => crmDate(x.at, true) }, { label: 'Customer', key: 'customer' }, { label: 'Store', key: 'store' },
      { label: 'Score', key: 'score', num: true }, { label: 'Comment', key: 'comment' }, { label: 'Ticket', key: 'ticket_id' }], r.recent, { empty: 'No answers yet.' }) +
    '<p class="text-muted crm-small">NPS = % of 9-10 answers minus % of 0-6 answers, over the answers shown.</p>';
}

// --- worklist ------------------------------------------------------------------------

async function crmWorklist(host, st) {
  const p = st.params || {};
  if (st.page === 'new-task') return crmTaskNew(host, p);
  if (st.page === 'new-appointment') return crmAppointmentNew(host, p);
  if (st.page === 'new-hold') return crmHoldNew(host, p);
  const view = p.view || 'tasks';
  const body = crmFrame(host, { title: 'Worklist', actions: `<button class="btn btn-outline" type="button" id="wl-hold">+ Hold request</button>
    <button class="btn btn-outline" type="button" id="wl-appt">+ Appointment</button><button class="btn btn-primary" type="button" id="wl-task">+ Task</button>` });
  host.querySelector('#wl-task').addEventListener('click', () => crmGo('worklist', 'new-task', {}));
  host.querySelector('#wl-appt').addEventListener('click', () => crmGo('worklist', 'new-appointment', {}));
  host.querySelector('#wl-hold').addEventListener('click', () => crmGo('worklist', 'new-hold', {}));
  body.innerHTML = `<div class="crm-subtabs">${[['tasks', 'Tasks'], ['appointments', 'Appointments'], ['holds', 'Hold requests']].map(([k, l]) => `<button type="button" class="btn btn-sm ${k === view ? 'btn-primary' : 'btn-outline'}" data-wl="${k}">${l}</button>`).join('')}</div><div id="wl-out"></div>`;
  body.querySelectorAll('[data-wl]').forEach(b => b.addEventListener('click', () => crmGo('worklist', '', { view: b.dataset.wl })));
  const out = body.querySelector('#wl-out');
  if (view === 'tasks') {
    out.innerHTML = `<div class="crm-filters">${crmField('wt-w', 'Whose', crmSelect('wt-w', [['mine', 'Mine and my store\'s'], ['', 'Everyone\'s']], 'mine'))}${crmField('wt-s', 'Status', crmSelect('wt-s', [['open', 'Open'], ['done', 'Done'], ['all', 'All']], 'open'))}</div><div id="wt-list"></div>`;
    const load = async () => {
      const r = await crmApi(`tasks?status=${crmVal(out, 'wt-s')}${crmVal(out, 'wt-w') === 'mine' ? '&mine=1' : ''}`);
      if (!r) return;
      out.querySelector('#wt-list').innerHTML = crmTable([{ label: 'Task', html: t => `${crmChip(t.kind_label)} ${crmEsc(t.title)}${t.outcome ? `<small class="crm-sub">${crmEsc(t.outcome)}</small>` : ''}` },
        { label: 'Customer', html: t => crmCustLink(t.customer_id, t.customer_name) }, { label: 'Store', key: 'store' }, { label: 'Owner', html: t => crmEsc(t.owner || 'Anyone') },
        { label: 'Due', html: t => `${crmDate(t.due_at, true)}${t.overdue ? ' ' + crmChip('Overdue', 'bad') : ''}` }, { label: 'Status', html: t => crmStatus(t.status) },
        { label: '', html: t => t.status === 'Open' ? `<button class="action-btn" data-tk="${crmEsc(t.id)}" data-a="done">Done</button>${t.owner ? '' : ` <button class="action-btn" data-tk="${crmEsc(t.id)}" data-a="take">Take</button>`} <button class="action-btn" data-tk="${crmEsc(t.id)}" data-a="cancel">Cancel</button>${t.customer_id ? ` <button class="action-btn" data-msg="${crmEsc(t.customer_id)}">Message</button>` : ''}` : `<button class="action-btn" data-tk="${crmEsc(t.id)}" data-a="reopen">Reopen</button>` }],
        r.tasks, { empty: 'Nothing on the worklist.' });
      crmWireCust(out);
      out.querySelectorAll('[data-tk]').forEach(b => b.addEventListener('click', async () => {
        let outcome = '';
        if (b.dataset.a === 'done') { outcome = await showCustomPrompt('How did it go?', '', 'Task done'); if (!outcome) return; }
        if (await crmApi(`tasks/${encodeURIComponent(b.dataset.tk)}/${b.dataset.a}`, { method: 'POST', body: { outcome } })) load();
      }));
      out.querySelectorAll('[data-msg]').forEach(b => b.addEventListener('click', () => crmGo('messages', 'new', { customer_id: b.dataset.msg })));
    };
    out.querySelectorAll('select').forEach(s => s.addEventListener('change', load));
    await load();
  } else if (view === 'appointments') {
    out.innerHTML = `<div class="crm-filters">${crmField('wa-s', 'Store (blank = all)', crmInput('wa-s'))}${crmField('wa-f', 'From', crmInput('wa-f', new Date().toISOString().slice(0, 10), 'type="date"'))}</div><div id="wa-list"></div>`;
    const load = async () => {
      const r = await crmApi(`appointments?store=${encodeURIComponent(crmVal(out, 'wa-s'))}&from_date=${encodeURIComponent(crmVal(out, 'wa-f'))}`);
      if (!r) return;
      out.querySelector('#wa-list').innerHTML = crmTable([{ label: 'When', key: 'slot_start' }, { label: 'Customer', html: a => crmCustLink(a.customer_id, a.customer_name) }, { label: 'Store', key: 'store' },
        { label: 'For', key: 'kind' }, { label: 'Note', key: 'note' }, { label: 'Status', html: a => crmStatus(a.status) },
        { label: '', html: a => a.status === 'Booked' ? `<button class="action-btn" data-ap="${crmEsc(a.id)}" data-a="done">Done</button> <button class="action-btn" data-ap="${crmEsc(a.id)}" data-a="no_show">No-show</button> <button class="action-btn" data-ap="${crmEsc(a.id)}" data-a="cancel">Cancel</button>` : crmEsc(a.cancel_reason || '') }],
        r.appointments, { empty: 'No appointments.' });
      crmWireCust(out);
      out.querySelectorAll('[data-ap]').forEach(b => b.addEventListener('click', async () => {
        let reason = '';
        if (b.dataset.a === 'cancel') { reason = await showCustomPrompt('Why is it cancelled? (Its reminders stop.)', '', 'Cancel the appointment'); if (!reason) return; }
        if (await crmApi(`appointments/${encodeURIComponent(b.dataset.ap)}/${b.dataset.a}`, { method: 'POST', body: { reason } })) load();
      }));
    };
    out.querySelectorAll('input').forEach(s => s.addEventListener('change', load));
    await load();
  } else {
    out.innerHTML = `<div class="crm-filters">${crmField('wh-s', 'Status', crmSelect('wh-s', [['Requested', 'Waiting for the store'], ['Confirmed', 'Kept'], ['Declined', 'Declined'], ['', 'All']], 'Requested'))}</div><div id="wh-list"></div>`;
    const load = async () => {
      const r = await crmApi('holds?status=' + encodeURIComponent(crmVal(out, 'wh-s')));
      if (!r) return;
      out.querySelector('#wh-list').innerHTML = crmTable([{ label: 'Customer', html: h => crmCustLink(h.customer_id, h.customer_name) }, { label: 'Item', html: h => crmEsc(h.item_name || h.item_code) },
        { label: 'Store', key: 'store' }, { label: 'Qty', key: 'qty', num: true }, { label: 'Available there', num: true, html: h => h.stock_here == null ? '-' : String(h.stock_here) },
        { label: 'Keep until', key: 'hold_until' }, { label: 'Status', html: h => crmStatus(h.status) + (h.decline_reason ? `<small class="crm-reason">${crmEsc(h.decline_reason)}</small>` : '') },
        { label: '', html: h => h.status === 'Requested' ? `<button class="action-btn" data-hd="${crmEsc(h.id)}" data-a="confirm">Confirm - kept</button> <button class="action-btn" data-hd="${crmEsc(h.id)}" data-a="decline">Decline</button>`
          : (h.status === 'Confirmed' && !h.ready_send_id ? `<button class="action-btn" data-hd="${crmEsc(h.id)}" data-a="ready">Tell the customer</button>` : (h.ready_send_id ? 'Customer told' : '')) }],
        r.holds, { empty: 'No hold requests.' });
      crmWireCust(out);
      out.querySelectorAll('[data-hd]').forEach(b => b.addEventListener('click', async () => {
        const hid = encodeURIComponent(b.dataset.hd);
        let ok;
        if (b.dataset.a === 'ready') {
          const ch = await showCustomPrompt('Tell them by WhatsApp, SMS or Email?', 'WhatsApp', 'Tell the customer');
          if (!ch) return;
          const s = await crmApi(`holds/${hid}/ready`, { method: 'POST', body: { channel: ch } });
          if (s) showToast(['Suppressed', 'Cancelled', 'Expired'].includes(s.status) ? 'Not sent: ' + (s.reason_label || s.reason) : 'Sent.', { variant: ['Suppressed', 'Cancelled', 'Expired'].includes(s.status) ? 'warning' : 'success' });
          ok = s;
        } else {
          let reason = '';
          if (b.dataset.a === 'decline') { reason = await showCustomPrompt('Why can it not be kept?', '', 'Decline the hold'); if (!reason) return; }
          ok = await crmApi(`holds/${hid}/decide`, { method: 'POST', body: { confirm: b.dataset.a === 'confirm', reason } });
        }
        if (ok) load();
      }));
    };
    out.querySelector('#wh-s').addEventListener('change', load);
    await load();
  }
}

async function crmTaskNew(host, p) {
  const body = crmFrame(host, { title: 'New task', back: () => crmGo('worklist'), backLabel: 'Worklist' });
  const kinds = Object.entries(crmMeta.task_kinds || {}).filter(([k]) => k !== 'hold');
  body.innerHTML = `<form class="crm-form" id="nt-form"><div class="crm-form-row">${crmField('nt-k', 'Kind', crmSelect('nt-k', kinds, p.kind || 'follow_up'))}${crmField('nt-c', 'Customer', crmInput('nt-c', p.name || ''))}</div>
    ${crmField('nt-t', 'What has to be done', crmInput('nt-t', p.title || ''))}
    <div class="crm-form-row">${crmField('nt-d', 'Due', crmInput('nt-d', '', 'type="datetime-local"'))}${crmField('nt-o', 'For (blank = anyone at the store)', crmInput('nt-o'))}${crmField('nt-s', 'Store (blank = yours)', crmInput('nt-s'))}</div>
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Add</button></div></form>`;
  const pick = crmCustomerPicker(body.querySelector('#nt-c'), p.customer_id);
  body.querySelector('#nt-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const t = await crmApi('tasks', { method: 'POST', body: { kind: crmVal(body, 'nt-k'), customer_id: pick(), title: crmVal(body, 'nt-t'), due_at: crmVal(body, 'nt-d'), owner: crmVal(body, 'nt-o'), store: crmVal(body, 'nt-s'),
      lead_id: p.lead_id || '', opportunity_id: p.opportunity_id || '' } });
    if (t) { showToast('Added to the worklist.', { variant: 'success' }); history.back(); }
  });
}

async function crmAppointmentNew(host, p) {
  const body = crmFrame(host, { title: 'New appointment', back: () => crmGo('worklist', '', { view: 'appointments' }), backLabel: 'Worklist' });
  const tomorrow = new Date(Date.now() + 86400000).toISOString().slice(0, 10);
  body.innerHTML = `<form class="crm-form" id="na-form"><div class="crm-form-row">${crmField('na-c', 'Customer', crmInput('na-c', p.name || ''))}
    ${crmField('na-k', 'For', crmSelect('na-k', [['visit', 'A visit'], ['trial', 'A trial'], ['fitting', 'A fitting']]))}</div>
    <div class="crm-form-row">${crmField('na-s', 'Store code', crmInput('na-s'))}${crmField('na-d', 'Day', crmInput('na-d', tomorrow, 'type="date"'))}<button class="btn btn-outline" type="button" id="na-load">Show free slots</button></div>
    <div id="na-slots" class="crm-slots"></div>${crmField('na-n', 'Note', crmInput('na-n'))}
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Book</button></div></form>`;
  const pick = crmCustomerPicker(body.querySelector('#na-c'), p.customer_id);
  let slot = '';
  body.querySelector('#na-load').addEventListener('click', async () => {
    const r = await crmApi(`stores/${encodeURIComponent(crmVal(body, 'na-s'))}/slots/${encodeURIComponent(crmVal(body, 'na-d'))}`);
    if (!r) return;
    const box = body.querySelector('#na-slots');
    box.innerHTML = r.slots.length ? r.slots.map(s => `<button type="button" class="btn btn-sm ${s.left > 0 ? 'btn-outline' : 'btn-outline crm-slot-full'}" data-slot="${crmEsc(s.start)}" ${s.left > 0 ? '' : 'disabled'}>${crmEsc(s.start.slice(11))}${s.left > 0 ? '' : ' full'}</button>`).join('')
      : '<p class="text-muted">No slots that day (closed, or set the store\'s hours under CRM > Settings).</p>';
    box.querySelectorAll('[data-slot]').forEach(b => b.addEventListener('click', () => { slot = b.dataset.slot; box.querySelectorAll('[data-slot]').forEach(x => x.classList.toggle('btn-primary', x === b)); }));
  });
  body.querySelector('#na-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!slot) { showToast('Choose a slot first.', { variant: 'warning' }); return; }
    const a = await crmApi('appointments', { method: 'POST', body: { customer_id: pick(), store: crmVal(body, 'na-s'), kind: crmVal(body, 'na-k'), slot_start: slot, note: crmVal(body, 'na-n') } });
    if (a) { showToast(`Booked for ${a.slot_start}. Reminders go a day and two hours before.`, { variant: 'success' }); crmGo('worklist', '', { view: 'appointments' }); }
  });
}

async function crmHoldNew(host, p) {
  const body = crmFrame(host, { title: 'Hold request', back: () => crmGo('worklist', '', { view: 'holds' }), backLabel: 'Worklist' });
  body.innerHTML = `<form class="crm-form" id="nh-form"><div class="crm-form-row">${crmField('nh-c', 'Customer', crmInput('nh-c', p.name || ''))}${crmField('nh-i', 'Item code or barcode', crmInput('nh-i', p.item_code || ''))}</div>
    <div class="crm-form-row">${crmField('nh-s', 'Store that keeps it (blank = yours)', crmInput('nh-s'))}${crmField('nh-q', 'Qty', crmInput('nh-q', '1', 'inputmode="numeric"'))}${crmField('nh-u', 'Keep until', crmInput('nh-u', '', 'type="date"'))}</div>
    ${crmField('nh-n', 'Note', crmInput('nh-n'))}<div class="crm-form-actions"><button class="btn btn-primary" type="submit">Ask the store</button></div>
    <p class="text-muted crm-small">The store confirms or declines on its worklist. The customer is told only after the store confirms.</p></form>`;
  const pick = crmCustomerPicker(body.querySelector('#nh-c'), p.customer_id);
  body.querySelector('#nh-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const h = await crmApi('holds', { method: 'POST', body: { customer_id: pick(), item_code: crmVal(body, 'nh-i'), store: crmVal(body, 'nh-s'), qty: Number(crmVal(body, 'nh-q')) || 1, hold_until: crmVal(body, 'nh-u'), note: crmVal(body, 'nh-n') } });
    if (h) { showToast('Sent to the store.', { variant: 'success' }); crmGo('worklist', '', { view: 'holds' }); }
  });
}
