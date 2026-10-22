// Stage 101: the CRM workspace (CRM menu). Native ES module, loaded on first
// visit; its other screens load on demand from view-crm-messaging.js
// (templates, campaigns, journeys, messages, senders), view-crm-service.js
// (inbox, tickets, feedback, worklist, appointments, holds, referrals) and
// view-crm-sales.js (leads, deals, forecast). The helpers exported here are
// published on window by the shell's loader, so those modules call them by
// name. Every page is a list first; New and a record's details open as their
// own page with a link back (Back works too).

const crmModules = {
  messaging: ['templates', 'campaigns', 'journeys', 'messages', 'settings'],
  service: ['inbox', 'tickets', 'feedback', 'worklist'],
  sales: ['pipeline'],
};
const crmTabs = [
  ['overview', 'Overview'], ['customers', 'Customers'], ['segments', 'Segments'], ['templates', 'Templates'], ['campaigns', 'Campaigns'],
  ['journeys', 'Journeys'], ['messages', 'Messages'], ['inbox', 'Inbox'], ['tickets', 'Tickets'], ['feedback', 'Feedback'],
  ['worklist', 'Worklist'], ['pipeline', 'Pipeline'], ['settings', 'Settings'],
];
let crmMeta = null;
let crmRoot = null;
const crmState = { tab: 'overview', page: '', params: {} };

export const crmEsc = (s) => escapeHTMLText(s == null ? '' : String(s));
export const crmMoney = (n) => (n == null || n === '' ? '' : formatMoney(n));
export function crmDate(v, withTime) {
  if (!v || String(v).startsWith('0001-')) return '';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  return withTime ? d.toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' })
    : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

// crmApi calls the CRM API; an error is shown in the shell's own way and
// null comes back.
export async function crmApi(path, opts = {}) {
  const init = { method: opts.method || 'GET' };
  if (opts.body !== undefined) {
    if (opts.body instanceof FormData) init.body = opts.body;
    else { init.body = JSON.stringify(opts.body); init.headers = { 'Content-Type': 'application/json' }; }
  }
  const res = await apiFetch(path.startsWith('/api/') ? path : '/api/v1/crm/' + path, init);
  if (!res) { showToast('Could not reach the server. Check your connection and try again.', { variant: 'danger' }); return null; }
  if (!res.ok) { if (!opts.quiet) await showApiError(res, opts.fail || 'That did not work.'); return null; }
  try { return await res.json(); } catch (_) { return {}; }
}

export async function crmGetMeta(force) {
  if (!crmMeta || force) crmMeta = await crmApi('meta') || { segment_fields: [], events: [], template_variables: [], lifecycle_states: [], reasons: {}, task_kinds: {}, lead_sources: [] };
  window.crmMeta = crmMeta; // the other CRM modules read it by name
  return crmMeta;
}

// crmGo opens a CRM page; Back returns to where it was opened from.
// A tab is its own screen (the shell's #/view/crm:<tab> entry); a page within
// a tab is a history entry of its own on the same address.
export function crmGo(tab, page = '', params = {}) {
  const sameTab = tab === crmState.tab && crmRoot && document.body.contains(crmRoot);
  crmState.tab = tab; crmState.page = page; crmState.params = params;
  if (!sameTab) {
    crmPending = { tab, page, params };
    renderView('crm:' + tab);
    return;
  }
  try { history.pushState({ crm: { tab, page, params } }, '', location.href); } catch (_) { /* sandboxed */ }
  crmPaint();
}
let crmPending = null;
window.addEventListener('popstate', (e) => {
  if (!crmRoot || !document.body.contains(crmRoot)) return;
  // Back across CRM tabs changes the hash as well; the app's hashchange route
  // draws that one, so only a page inside the same tab is handled here.
  let hash = location.hash;
  try { hash = decodeURIComponent(hash); } catch (_) { /* keep it raw */ }
  const m = hash.match(/^#\/view\/crm(?::(\w+))?/);
  if (!m || (m[1] || 'overview') !== crmState.tab) return;
  const s = e.state && e.state.crm;
  crmState.tab = s ? s.tab : crmState.tab; crmState.page = s ? s.page : ''; crmState.params = s ? s.params : {};
  crmPaint();
});

// crmFrame is a page: a breadcrumb back (for a sub-page), the title, actions.
export function crmFrame(host, { title, back, backLabel, actions = '', count }) {
  host.innerHTML = `<div class="crm-head">
      ${back ? `<button type="button" class="crm-crumb" data-crm-back>${crmEsc(backLabel || 'Back')}</button><span class="crm-crumb-sep" aria-hidden="true">›</span>` : ''}
      <h2 class="crm-title">${crmEsc(title)}</h2>${count != null ? `<span class="crm-count">${count}</span>` : ''}
      <span class="crm-actions">${actions}</span></div><div class="crm-body"></div>`;
  const b = host.querySelector('[data-crm-back]');
  if (b) b.addEventListener('click', () => { if (history.state && history.state.crm) history.back(); else back(); });
  return host.querySelector('.crm-body');
}

export function crmTable(cols, rows, { empty = 'Nothing here yet.', rowAttr } = {}) {
  if (!rows || !rows.length) return `<p class="text-muted crm-empty">${empty}</p>`;
  return `<div class="table-wrapper"><table class="crm-table"><thead><tr>${cols.map(c => `<th${c.num ? ' class="num"' : ''}>${crmEsc(c.label)}</th>`).join('')}</tr></thead>
    <tbody>${rows.map((r, i) => `<tr${rowAttr ? ' ' + rowAttr(r, i) : ''}>${cols.map(c => `<td${c.num ? ' class="num"' : ''}>${c.html ? c.html(r, i) : crmEsc(r[c.key])}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
}

export function crmChip(text, tone) { return text ? `<span class="crm-chip${tone ? ' crm-chip-' + tone : ''}">${crmEsc(text)}</span>` : ''; }

export function crmField(id, label, input) {
  return `<div class="form-group"><label class="form-label" for="${id}">${crmEsc(label)}</label>${input}</div>`;
}
export const crmInput = (id, value = '', attrs = '') => `<input class="form-input" id="${id}" value="${crmEsc(value)}" ${attrs}>`;
export function crmSelect(id, options, value = '', attrs = '') {
  return `<select class="form-input" id="${id}" ${attrs}>${options.map(o => {
    const [v, l] = Array.isArray(o) ? o : [o, o];
    return `<option value="${crmEsc(v)}"${String(v) === String(value) ? ' selected' : ''}>${crmEsc(l)}</option>`;
  }).join('')}</select>`;
}
export const crmVal = (host, id) => { const el = host.querySelector('#' + id); return el ? (el.type === 'checkbox' ? el.checked : el.value.trim()) : ''; };

// crmCustomerPicker turns an input into a customer search; returns a getter
// for the chosen customer's id.
export function crmCustomerPicker(input, initial) {
  let chosen = initial || '';
  attachTypeahead(input, 'Customer', { onPick: (doc) => { chosen = doc.id || doc.code; input.value = doc.name || doc.code || doc.id; } });
  input.addEventListener('input', () => { if (!input.value) chosen = ''; });
  return () => chosen || input.value.trim();
}

function crmStatusTone(s) {
  return ({ Accepted: 'ok', Delivered: 'ok', Read: 'ok', Simulated: 'ok', Running: 'ok', Approved: 'ok', Done: 'ok', Won: 'ok', granted: 'ok', Confirmed: 'ok',
    Failed: 'bad', Unknown: 'warn', Suppressed: 'muted', Cancelled: 'muted', Expired: 'muted', HeldOut: 'muted', Lost: 'bad', withdrawn: 'bad', denied: 'bad',
    'Pending Approval': 'warn', Paused: 'warn', Deferred: 'warn', Queued: 'warn', Waiting: 'warn' })[s] || '';
}
export const crmStatus = (s) => crmChip(s, crmStatusTone(s));
// crmWhy is a message's reason: the plain label, then the specifics when they add something.
export const crmWhy = (r) => { const l = r.reason_label || r.reason || ''; const d = r.detail && r.detail !== l ? r.detail : ''; return crmEsc(l && d ? `${l} - ${d}` : (l || d)); };

export async function renderCRMView(root) {
  crmRoot = document.createElement('div');
  crmRoot.className = 'crm-root';
  root.appendChild(crmRoot);
  const tab = (typeof currentViewTab === 'string' && currentViewTab) || 'overview';
  const fromHistory = history.state && history.state.crm && history.state.crm.tab === tab;
  if (crmPending && crmPending.tab === tab) {
    // opened from another CRM tab straight onto one of its pages
    Object.assign(crmState, crmPending);
    if (crmState.page) { try { history.pushState({ crm: { ...crmPending } }, '', location.href); } catch (_) { /* sandboxed */ } }
  } else if (fromHistory) {
    Object.assign(crmState, history.state.crm);
  } else {
    crmState.tab = tab; crmState.page = ''; crmState.params = {};
  }
  crmPending = null;
  await crmGetMeta();
  await crmPaint();
}

async function crmPaint() {
  const tab = crmState.tab;
  const tabs = crmTabs.filter(([k]) => k !== 'settings' || (crmMeta && crmMeta.can_manage));
  crmRoot.innerHTML = `<div class="page-header crm-page-header"><div class="page-title-section"><h1 class="page-title">CRM</h1></div></div>
    <nav class="crm-tabs" aria-label="CRM">${tabs.map(([k, l]) => `<button type="button" class="crm-tab${k === tab ? ' active' : ''}" data-crm-tab="${k}" aria-current="${k === tab}">${l}</button>`).join('')}</nav>
    <div class="table-panel crm-panel" id="crm-page"></div>`;
  crmRoot.querySelectorAll('[data-crm-tab]').forEach(b => b.addEventListener('click', () => crmGo(b.dataset.crmTab)));
  const host = crmRoot.querySelector('#crm-page');
  host.innerHTML = '<p class="text-muted">Loading…</p>';
  const local = { overview: crmOverview, customers: crmCustomers, segments: crmSegments };
  if (local[tab]) return local[tab](host, crmState);
  const mod = Object.entries(crmModules).find(([, tabs]) => tabs.includes(tab));
  if (!mod) return crmOverview(host, crmState);
  try {
    const m = await loadViewModule(`/view-crm-${mod[0]}.js?v=1`);
    await m.crmRenderTab(tab, host, crmState);
  } catch (e) {
    console.error(e);
    renderErrorPanel(host, 'This CRM page could not be loaded. Check your connection and try again.', () => crmPaint());
  }
}

// --- overview ------------------------------------------------------------------

async function crmOverview(host) {
  const body = crmFrame(host, { title: 'Overview' });
  const d = await crmApi('dashboard');
  if (!d) { body.innerHTML = ''; return; }
  const tile = (label, value, go, note) => `<button type="button" class="crm-tile" ${go ? `data-go-tab="${go}"` : ''}><span class="crm-tile-value">${crmEsc(value)}</span><span class="crm-tile-label">${crmEsc(label)}</span>${note ? `<span class="crm-tile-note">${crmEsc(note)}</span>` : ''}</button>`;
  const month = d.messages_month || {};
  const sentMonth = ['Sending', 'Accepted', 'Delivered', 'Read', 'Unknown', 'Simulated', 'Failed'].reduce((a, k) => a + (month[k] || 0), 0);
  const notSent = ['Suppressed', 'Expired', 'Cancelled'].reduce((a, k) => a + (month[k] || 0), 0);
  const lc = d.lifecycle || [];
  const max = Math.max(1, ...lc.map(l => l.count));
  const setup = [];
  ['SMS', 'WhatsApp', 'Email'].forEach(ch => { if (!(d.senders_ready || {})[ch]) setup.push(`No ${ch} sender is set up - ${ch} messages are recorded as NO_SENDER.`); });
  if (!d.journeys_on) setup.push('No journey is switched on. Journeys > Add the ready-made journeys, then switch on the ones you want.');
  body.innerHTML = `
    <div class="crm-tiles">
      ${tile('Customers', d.customers, 'customers')}
      ${tile('Reachable by WhatsApp', d.reachable.WhatsApp, 'segments')}
      ${tile('Reachable by SMS', d.reachable.SMS, 'segments')}
      ${tile('Reachable by email', d.reachable.Email, 'segments')}
      ${tile('Messages sent this month', sentMonth, 'messages', notSent ? `${notSent} not sent (with reasons)` : '')}
      ${tile('Campaigns running', d.campaigns_live, 'campaigns')}
      ${tile('Journeys on', d.journeys_on, 'journeys', `${d.journey_runs_live} customers in one now`)}
      ${tile('NPS (90 days)', d.nps == null ? '-' : d.nps, 'feedback', d.nps == null ? 'No answers yet' : `${d.nps_answers} answers`)}
      ${tile('Open tickets', d.open_tickets, 'tickets')}
      ${tile('Open tasks', d.open_tasks, 'worklist')}
      ${tile('Unread in the inbox', d.unread_inbox, 'inbox')}
      ${tile('Weighted pipeline', crmMoney(d.pipeline_weighted), 'pipeline')}
    </div>
    ${d.unknown_outcomes ? `<div class="crm-note crm-note-warn">${d.unknown_outcomes} message(s) have an unknown outcome. Messages > Unknown to settle them.</div>` : ''}
    ${setup.length ? `<div class="crm-note"><strong>To finish setting up</strong><ul>${setup.map(s => `<li>${crmEsc(s)}</li>`).join('')}</ul></div>` : ''}
    <h3 class="crm-section">Customers by lifecycle</h3>
    <div class="crm-bars">${lc.map(l => `<button type="button" class="crm-bar" data-life="${crmEsc(l.state)}"><span class="crm-bar-label">${crmEsc(l.state)}</span>
      <span class="crm-bar-track"><span style="width:${Math.round(100 * l.count / max)}%"></span></span><span class="crm-bar-n">${l.count}</span></button>`).join('')}</div>
    <p class="text-muted crm-small">Counted now, in ${crmEsc(d.time_zone)} time. Lifecycle comes from each customer's bills; a return or merge shows the next time you look.</p>`;
  body.querySelectorAll('[data-go-tab]').forEach(b => b.addEventListener('click', () => crmGo(b.dataset.goTab)));
  body.querySelectorAll('[data-life]').forEach(b => b.addEventListener('click', () => crmGo('customers', '', { lifecycle: b.dataset.life })));
}

// --- customers -----------------------------------------------------------------

async function crmCustomers(host, st) {
  if (st.page === 'customer') return crmCustomerPage(host, st.params.id);
  if (st.page === 'duplicates') return crmDuplicates(host);
  if (st.page === 'profile') return crmProfileEdit(host, st.params.id);
  const p = st.params;
  const body = crmFrame(host, { title: 'Customers', actions: `
    <button class="btn btn-outline" type="button" id="crm-dups">Possible duplicates</button>
    <button class="btn btn-primary" type="button" id="crm-new-cust">+ New customer</button>` });
  body.innerHTML = `<div class="crm-filters">
      ${crmField('crm-cq', 'Find', crmInput('crm-cq', p.q || '', 'placeholder="Name, code, city, tag - or the whole phone number" autocomplete="off"'))}
      ${crmField('crm-cl', 'Lifecycle', crmSelect('crm-cl', [['', 'All']].concat((crmMeta.lifecycle_states || []).map(s => [s, s])), p.lifecycle || ''))}
    </div><div id="crm-clist"></div>`;
  host.querySelector('#crm-dups').addEventListener('click', () => crmGo('customers', 'duplicates'));
  host.querySelector('#crm-new-cust').addEventListener('click', async () => {
    currentDoctype = 'Customer'; currentSearchQuery = ''; currentTablePage = 1;
    await renderView('doctype-table');
    document.getElementById('doc-create-button')?.click();
  });
  let page = 1;
  const load = async () => {
    const q = encodeURIComponent(crmVal(body, 'crm-cq')), lc = encodeURIComponent(crmVal(body, 'crm-cl'));
    const data = await crmApi(`customers?q=${q}&lifecycle=${lc}&page=${page}&page_size=50`);
    const list = body.querySelector('#crm-clist');
    if (!data) { list.innerHTML = ''; return; }
    const pages = Math.max(1, Math.ceil(data.total / data.page_size));
    list.innerHTML = crmTable([
      { label: 'Customer', html: r => `<a href="#" data-cust="${crmEsc(r.id)}">${crmEsc(r.name || r.code)}</a>${(r.tags || []).map(t => ' ' + crmChip(t)).join('')}` },
      { label: 'Phone', key: 'phone' }, { label: 'City', key: 'city' }, { label: 'Tier', key: 'tier' },
      { label: 'Bills', key: 'visits', num: true }, { label: 'Spend', num: true, html: r => crmMoney(r.spend) },
      { label: 'Last bill', html: r => crmDate(r.last_bill) }, { label: 'Lifecycle', html: r => crmChip(r.lifecycle) },
    ], data.rows, { empty: data.total === 0 && !crmVal(body, 'crm-cq') ? 'No customers yet. Customers are added at the till, here (+ New customer) or by import.' : 'No customer matches.' }) +
      `<div class="crm-pager"><span>${data.total} customer(s)${data.masked ? ' · phone numbers and emails are masked for your role' : ''}</span>
       ${pages > 1 ? `<button class="btn btn-outline btn-sm" data-pg="-1" ${page <= 1 ? 'disabled' : ''}>Previous</button><span>Page ${page} of ${pages}</span><button class="btn btn-outline btn-sm" data-pg="1" ${page >= pages ? 'disabled' : ''}>Next</button>` : ''}</div>`;
    list.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
    list.querySelectorAll('[data-pg]').forEach(b => b.addEventListener('click', () => { page += Number(b.dataset.pg); load(); }));
  };
  let t = 0;
  body.querySelector('#crm-cq').addEventListener('input', () => { clearTimeout(t); t = setTimeout(() => { page = 1; load(); }, 300); });
  body.querySelector('#crm-cl').addEventListener('change', () => { page = 1; load(); });
  await load();
}

const crmConsentWords = { granted: 'Agreed', denied: 'Said no', withdrawn: 'Withdrew', none: 'Not asked' };

export async function crmCustomerPage(host, id) {
  const c = await crmApi('customers/' + encodeURIComponent(id));
  if (!c) { crmFrame(host, { title: 'Customer', back: () => crmGo('customers'), backLabel: 'Customers' }); return; }
  const cu = c.customer || {};
  const s = c.summary || {};
  const body = crmFrame(host, { title: cu.name || cu.code || id, back: () => crmGo('customers'), backLabel: 'Customers', actions: `
      <button class="btn btn-outline" type="button" id="cp-msg">Send a message</button>
      <button class="btn btn-outline" type="button" id="cp-task">+ Task</button>
      <button class="btn btn-outline" type="button" id="cp-appt">+ Appointment</button>
      <button class="btn btn-primary" type="button" id="cp-edit">Edit profile</button>` });
  const src = (f) => { const p = (c.profile_sources || {})[f]; return p ? `<span class="crm-src" title="Last changed ${crmEsc(p.at || '')} by ${crmEsc(p.by || '')}">${crmEsc(p.source || '')}</span>` : ''; };
  const kv = (label, f, value) => { const v = value != null && value !== '' ? value : (f && cu[f]) || ''; return `<dt>${crmEsc(label)}</dt><dd>${crmEsc(v === '' ? '-' : v)} ${f ? src(f) : ''}</dd>`; };
  const consentRow = (ch) => ['marketing', 'service'].map(p => {
    const x = (c.consent || []).find(k => k.channel === ch && k.purpose === p) || {};
    return `<td>${crmChip(crmConsentWords[x.state] || x.state, x.reachable ? 'ok' : (x.state === 'none' ? '' : 'bad'))}${x.reachable ? '' : `<small class="crm-reason">${crmEsc((crmMeta.reasons || {})[x.reason] || x.reason || '')}</small>`}</td>`;
  }).join('');
  body.innerHTML = `
    ${c.merged_into ? `<div class="crm-note crm-note-warn">Merged into <a href="#" data-cust="${crmEsc(c.merged_into)}">${crmEsc(c.merged_into)}</a>.</div>` : ''}
    <div class="crm-chips">${crmChip(s.lifecycle, 'ok')} ${s.rfm ? crmChip('RFM: ' + s.rfm) : ''} ${cu.loyalty_tier ? crmChip(cu.loyalty_tier + ' tier') : ''}
      ${c.masked ? crmChip('Contacts masked for your role', 'muted') : ''}</div>
    <div class="crm-tiles crm-tiles-sm">
      <div class="crm-tile"><span class="crm-tile-value">${s.visits}</span><span class="crm-tile-label">Bills</span></div>
      <div class="crm-tile"><span class="crm-tile-value">${crmMoney(s.spend)}</span><span class="crm-tile-label">Spend (net of returns)</span></div>
      <div class="crm-tile"><span class="crm-tile-value">${s.avg_bill == null ? '-' : crmMoney(s.avg_bill)}</span><span class="crm-tile-label">Average bill</span></div>
      <div class="crm-tile"><span class="crm-tile-value">${s.last_bill ? crmDate(s.last_bill) : '-'}</span><span class="crm-tile-label">Last bill${s.days_since != null ? ` · ${s.days_since} days ago` : ''}</span></div>
      <div class="crm-tile"><span class="crm-tile-value">${s.points}</span><span class="crm-tile-label">Points${s.points_pending ? ` · ${s.points_pending} pending` : ''}</span></div>
      <div class="crm-tile"><span class="crm-tile-value">${s.open_tickets} / ${s.open_tasks}</span><span class="crm-tile-label">Open tickets / tasks</span></div>
    </div>
    <div class="crm-grid">
      <section><h3 class="crm-section">Profile</h3><dl class="crm-kv">
        ${kv('Phone', 'phone')}${kv('Email', 'email')}${kv('Birthday', 'date_of_birth')}${kv('Anniversary', 'date_of_anniversary')}${kv('Gender', 'gender')}
        ${kv('Sizes', 'sizes')}${kv('Preferred store', 'preferred_store')}${kv('Preferred salesman', 'preferred_salesman')}${kv('Tags', 'tags')}
        ${kv('City', 'city')}${kv('Referral code', '', s.referral_code || '')}${s.next_appointment ? kv('Next appointment', '', crmDate(s.next_appointment, true)) : ''}
      </dl>${!s.referral_code ? '<button class="btn btn-outline btn-sm" type="button" id="cp-ref">Give a referral code</button>' : ''}</section>
      <section><h3 class="crm-section">Consent</h3>
        <table class="crm-table crm-consent"><thead><tr><th></th><th>Offers</th><th>Service messages</th></tr></thead>
        <tbody>${['SMS', 'WhatsApp', 'Email'].map(ch => `<tr><th>${ch}</th>${consentRow(ch)}</tr>`).join('')}</tbody></table>
        <details class="crm-details"><summary>Record what the customer said</summary>
          <div class="crm-form-row">${crmField('cp-cc', 'Channel', crmSelect('cp-cc', ['WhatsApp', 'SMS', 'Email', ['All', 'Every channel (stop all)']]))}
          ${crmField('cp-cp', 'For', crmSelect('cp-cp', [['marketing', 'Offers'], ['service', 'Service messages'], ['all', 'Everything']]))}
          ${crmField('cp-ca', 'The customer', crmSelect('cp-ca', [['granted', 'Agrees'], ['withdrawn', 'Withdraws'], ['denied', 'Says no']]))}</div>
          ${crmField('cp-ce', 'How (evidence)', crmInput('cp-ce', '', 'placeholder="Asked at the counter, signed form no. 123..."'))}
          <button class="btn btn-primary btn-sm" type="button" id="cp-csave">Record</button> <button class="btn btn-outline btn-sm" type="button" id="cp-cledger">History</button>
          <div id="cp-ledger"></div></details>
      </section>
    </div>
    <div class="crm-grid">
      <section><h3 class="crm-section">Notes</h3>
        <div class="crm-form-row"><textarea class="form-input" id="cp-note" rows="2" maxlength="2000" placeholder="Add a note for the team"></textarea><button class="btn btn-outline" type="button" id="cp-note-add">Add</button></div>
        <ul class="crm-notes">${(c.notes || []).map(n => `<li><span>${crmEsc(n.note)}</span><small>${crmEsc(n.created_by)} · ${crmDate(n.created_at, true)}</small></li>`).join('') || '<li class="text-muted">No notes yet.</li>'}</ul></section>
      <section><h3 class="crm-section">Wish list</h3>
        <div class="crm-form-row">${crmInput('cp-wish', '', 'placeholder="Item code or barcode"')}<button class="btn btn-outline" type="button" id="cp-wish-add">Add</button></div>
        ${crmTable([{ label: 'Item', html: w => crmEsc(w.item_name || w.item_code) }, { label: 'Added', html: w => crmDate(w.created_at) },
          { label: '', html: w => `<button class="action-btn" data-hold="${crmEsc(w.item_code)}">Ask the store to keep it</button> <button class="action-btn" data-unwish="${w.id}">Remove</button>` }], c.wishlist, { empty: 'Nothing on the wish list.' })}</section>
    </div>
    <h3 class="crm-section">Timeline</h3>
    <div class="crm-filters">${crmField('cp-tk', 'Show', crmSelect('cp-tk', [['', 'Everything'], ['bill,return,order,invoice', 'Bills and returns'], ['points,voucher', 'Points and vouchers'],
      ['message', 'Messages'], ['ticket,feedback', 'Tickets and feedback'], ['note,task,appointment', 'Notes, tasks, appointments'], ['consent', 'Consent'], ['journey', 'Journeys']]))}</div>
    <ol class="crm-timeline" id="cp-tl"></ol><button class="btn btn-outline btn-sm" type="button" id="cp-more" hidden>Show older</button>`;
  body.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
  host.querySelector('#cp-edit').addEventListener('click', () => crmGo('customers', 'profile', { id }));
  host.querySelector('#cp-msg').addEventListener('click', () => crmGo('messages', 'new', { customer_id: id, name: cu.name }));
  host.querySelector('#cp-task').addEventListener('click', () => crmGo('worklist', 'new-task', { customer_id: id, name: cu.name }));
  host.querySelector('#cp-appt').addEventListener('click', () => crmGo('worklist', 'new-appointment', { customer_id: id, name: cu.name }));
  body.querySelector('#cp-ref')?.addEventListener('click', async () => { if (await crmApi(`customers/${encodeURIComponent(id)}/referral-code`, { method: 'POST', body: {} })) crmPaint(); });
  body.querySelector('#cp-csave').addEventListener('click', async () => {
    const r = await crmApi(`customers/${encodeURIComponent(id)}/consent`, { method: 'POST', body: { channel: crmVal(body, 'cp-cc'), purpose: crmVal(body, 'cp-cp'), action: crmVal(body, 'cp-ca'), evidence: crmVal(body, 'cp-ce'), source: 'form' } });
    if (r) { showToast('Recorded.', { variant: 'success' }); crmPaint(); }
  });
  body.querySelector('#cp-cledger').addEventListener('click', async () => {
    const r = await crmApi(`customers/${encodeURIComponent(id)}/consent`);
    if (r) body.querySelector('#cp-ledger').innerHTML = crmTable([{ label: 'When', html: x => crmDate(x.created_at, true) }, { label: 'Channel', key: 'channel' }, { label: 'For', key: 'purpose' },
      { label: 'Action', html: x => crmStatus(x.action) }, { label: 'How', html: x => crmEsc(x.source_label || x.source) }, { label: 'By', key: 'recorded_by' }, { label: 'Evidence', key: 'evidence' }, { label: 'Notice', key: 'notice_version' }], r.ledger, { empty: 'No consent recorded yet.' });
  });
  body.querySelector('#cp-note-add').addEventListener('click', async () => {
    const note = crmVal(body, 'cp-note');
    if (note && await crmApi(`customers/${encodeURIComponent(id)}/notes`, { method: 'POST', body: { note } })) crmPaint();
  });
  body.querySelector('#cp-wish-add').addEventListener('click', async () => {
    const code = crmVal(body, 'cp-wish');
    if (code && await crmApi(`customers/${encodeURIComponent(id)}/wishlist`, { method: 'POST', body: { item_code: code } })) crmPaint();
  });
  body.querySelectorAll('[data-unwish]').forEach(b => b.addEventListener('click', async () => {
    if (await crmApi(`customers/${encodeURIComponent(id)}/wishlist?wish_id=${b.dataset.unwish}`, { method: 'DELETE' })) crmPaint();
  }));
  body.querySelectorAll('[data-hold]').forEach(b => b.addEventListener('click', () => crmGo('worklist', 'new-hold', { customer_id: id, name: cu.name, item_code: b.dataset.hold })));
  let before = '';
  const tl = body.querySelector('#cp-tl'), more = body.querySelector('#cp-more');
  const loadTL = async (reset) => {
    if (reset) { before = ''; tl.innerHTML = ''; }
    const kinds = encodeURIComponent(crmVal(body, 'cp-tk'));
    const r = await crmApi(`customers/${encodeURIComponent(id)}/timeline?limit=30&kinds=${kinds}&before=${encodeURIComponent(before)}`);
    if (!r) return;
    tl.insertAdjacentHTML('beforeend', r.entries.map(e => `<li class="crm-tl-${crmEsc(e.kind)}"><time>${crmDate(e.at, true)}</time><span class="crm-tl-what">${crmEsc(e.title)}${e.amount != null ? ` · ${crmMoney(e.amount)}` : ''}</span>
      ${e.detail ? `<span class="crm-tl-detail">${crmEsc(e.detail)}</span>` : ''}</li>`).join('') || (reset ? '<li class="text-muted">Nothing yet.</li>' : ''));
    more.hidden = !r.more; before = r.next || '';
  };
  body.querySelector('#cp-tk').addEventListener('change', () => loadTL(true));
  more.addEventListener('click', () => loadTL(false));
  await loadTL(true);
}

// crmProfileEdit edits the profile fields through the ordinary record save,
// so the Customer's own checks (and the CRM's) all apply.
async function crmProfileEdit(host, id) {
  const res = await apiFetch(`/api/v1/doc/Customer/${encodeURIComponent(id)}`);
  const body = crmFrame(host, { title: 'Edit profile', back: () => crmGo('customers', 'customer', { id }), backLabel: 'Customer' });
  if (!res || !res.ok) { if (res) await showApiError(res, 'Could not open the customer.'); return; }
  const doc = await res.json();
  const fields = [['name', 'Name'], ['phone', 'Phone'], ['email', 'Email'], ['date_of_birth', 'Birthday', 'date'], ['date_of_anniversary', 'Anniversary', 'date'],
    ['gender', 'Gender', 'select', ['', 'Female', 'Male', 'Other']], ['sizes', 'Sizes (e.g. Shirt M, Trouser 32, Shoe 8)'], ['preferred_store', 'Preferred store'],
    ['preferred_salesman', 'Preferred salesman'], ['tags', 'Tags (comma-separated)'], ['city', 'City']];
  body.innerHTML = `<form class="crm-form" id="crm-prof">${fields.map(([k, l, t, opts]) => crmField('pf-' + k, l,
    t === 'select' ? crmSelect('pf-' + k, opts, doc[k] || '') : crmInput('pf-' + k, doc[k] || '', t === 'date' ? 'type="date"' : ''))).join('')}
    <p class="text-muted crm-small">Consent is recorded on the customer page, not here.</p>
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Save</button></div></form>`;
  body.querySelector('#crm-prof').addEventListener('submit', async (e) => {
    e.preventDefault();
    const data = { ...doc };
    ['id', 'status', 'version'].forEach(k => delete data[k]);
    fields.forEach(([k]) => { data[k] = crmVal(body, 'pf-' + k); });
    data.expected_version = doc.version;
    const r = await apiFetch(`/api/v1/doc/Customer/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify(data) });
    if (!r || !r.ok) { if (r) await showApiError(r, 'The profile was not saved.'); return; }
    showToast('Saved.', { variant: 'success' });
    crmGo('customers', 'customer', { id });
  });
}

async function crmDuplicates(host) {
  const body = crmFrame(host, { title: 'Possible duplicates', back: () => crmGo('customers'), backLabel: 'Customers' });
  const r = await crmApi('duplicates');
  if (!r) return;
  body.innerHTML = `<p class="text-muted crm-small">Two records with the same email, the same name and birthday, or the same CleverTap identity. One phone is always one customer, so a duplicate has another number. A merge waits in Approvals when your business requires it.</p>
    ${crmTable([{ label: 'Customer A', html: p => `${crmEsc(p.a.name)}<small class="crm-sub">${crmEsc(p.a.phone)} · ${p.a.visits} bills</small>` },
      { label: 'Customer B', html: p => `${crmEsc(p.b.name)}<small class="crm-sub">${crmEsc(p.b.phone)} · ${p.b.visits} bills</small>` },
      { label: 'Why', html: p => crmEsc(p.reasons.join(', ')) },
      { label: '', html: (p, i) => `<button class="action-btn" data-merge="${i}" data-keep="a">Keep A</button> <button class="action-btn" data-merge="${i}" data-keep="b">Keep B</button>` }], r.pairs, { empty: 'No possible duplicates found.' })}
    <div id="crm-merge-preview"></div>`;
  body.querySelectorAll('[data-merge]').forEach(b => b.addEventListener('click', async () => {
    const pair = r.pairs[Number(b.dataset.merge)];
    const keep = b.dataset.keep === 'a' ? pair.a : pair.b, merge = b.dataset.keep === 'a' ? pair.b : pair.a;
    const p = await crmApi('merges', { method: 'POST', body: { keep_id: keep.id, merge_id: merge.id, preview: true } });
    if (!p) return;
    const box = body.querySelector('#crm-merge-preview');
    box.innerHTML = `<div class="crm-note"><strong>Merge ${crmEsc(merge.name)} into ${crmEsc(keep.name)}</strong>
      <ul><li>${p.bills} bill(s), ${p.invoices} invoice(s), ${p.vouchers} voucher(s) and ${p.points} point(s) move</li>
      <li>${p.consent_rows} consent record(s) move; the latest answer per channel counts</li>
      <li>${p.live_journeys} live journey(s)${p.journeys_ended ? `, ${p.journeys_ended} ended because ${crmEsc(keep.name)} is already in them` : ''}</li>
      <li>${p.queued_messages} waiting message(s)${p.messages_dropped ? `, ${p.messages_dropped} dropped so the customer gets one copy` : ''}</li>
      <li>${p.tickets} ticket(s), ${p.notes} note(s), ${p.tasks} task(s), ${p.appointments} appointment(s)</li></ul>
      ${crmField('crm-mreason', 'Why', crmInput('crm-mreason', '', 'placeholder="Same person - second number"'))}
      <button class="btn btn-primary" type="button" id="crm-mgo">Merge</button></div>`;
    box.querySelector('#crm-mgo').addEventListener('click', async () => {
      const res = await crmApi('merges', { method: 'POST', body: { keep_id: keep.id, merge_id: merge.id, reason: crmVal(box, 'crm-mreason') } });
      if (res) { showToast(res.status === 'Merged' ? 'Merged.' : 'Sent for approval (Approvals).', { variant: 'success' }); crmDuplicates(host); }
    });
  }));
}

// --- segments ------------------------------------------------------------------

async function crmSegments(host, st) {
  if (st.page === 'edit') return crmSegmentEdit(host, st.params.id);
  if (st.page === 'lists') return crmLists(host);
  const body = crmFrame(host, { title: 'Segments', actions: `<button class="btn btn-outline" type="button" id="crm-lists">Uploaded lists</button>${crmMeta.can_manage ? '<button class="btn btn-primary" type="button" id="crm-seg-new">+ New segment</button>' : ''}` });
  host.querySelector('#crm-lists').addEventListener('click', () => crmGo('segments', 'lists'));
  host.querySelector('#crm-seg-new')?.addEventListener('click', () => crmGo('segments', 'edit', {}));
  const r = await crmApi('segments');
  if (!r) return;
  body.innerHTML = crmTable([{ label: 'Segment', html: s => `<a href="#" data-seg="${crmEsc(s.id)}">${crmEsc(s.name)}</a>` }, { label: 'About', key: 'description' },
    { label: 'Version', key: 'current_version', num: true }, { label: 'Changed', html: s => crmDate(s.updated_at) }, { label: 'By', key: 'created_by' }],
    r.segments, { empty: 'No segments yet. A segment picks customers by what they did - for example "bought shirts in the last 90 days and agree to WhatsApp".' });
  body.querySelectorAll('[data-seg]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('segments', 'edit', { id: a.dataset.seg }); }));
}

const crmCmp = [['>=', 'at least'], ['>', 'more than'], ['<=', 'at most'], ['<', 'less than'], ['=', 'exactly'], ['between', 'between']];

function crmRuleHTML(rule, path) {
  const fields = crmMeta.segment_fields || [];
  const f = fields.find(x => x.Field === rule.field) || fields[0] || {};
  const kind = f.Kind;
  let value = '';
  if (kind === 'number') {
    value = crmSelect(`r-cmp-${path}`, crmCmp, rule.cmp || '>=', 'data-k="cmp"') + crmInput(`r-v-${path}`, rule.value ?? '', 'data-k="value" inputmode="decimal" class="form-input crm-num"') +
      (rule.cmp === 'between' ? crmInput(`r-v2-${path}`, rule.value2 ?? '', 'data-k="value2" inputmode="decimal" class="form-input crm-num"') : '');
  } else if (kind === 'select') {
    value = (['lifecycle', 'rfm', 'gender'].includes(f.Field) ? crmSelect(`r-cmp-${path}`, [['=', 'is'], ['!=', 'is not']], rule.cmp || '=', 'data-k="cmp"') : '') +
      crmSelect(`r-v-${path}`, f.Options || [], rule.value || '', 'data-k="value"');
  } else if (kind === 'bought') {
    value = `in the last ${crmInput(`r-d-${path}`, rule.days || '', 'data-k="days" inputmode="numeric" class="form-input crm-num" placeholder="days (blank = ever)"')} days
      ${crmInput(`r-cat-${path}`, rule.category || '', 'data-k="category" placeholder="category (optional)"')}
      ${crmInput(`r-br-${path}`, rule.brand || '', 'data-k="brand" placeholder="brand (optional)"')}
      ${crmInput(`r-it-${path}`, rule.item || '', 'data-k="item" placeholder="item code (optional)"')}
      ${crmInput(`r-st-${path}`, rule.store || '', 'data-k="store" placeholder="store code (optional)"')}`;
  } else if (kind === 'list') {
    value = `<select class="form-input" data-k="value" data-lists="${crmEsc(rule.value || '')}"><option value="${crmEsc(rule.value || '')}">${crmEsc(rule.value || 'Choose a list')}</option></select>`;
  } else {
    value = (f.Field !== 'tags' ? crmSelect(`r-cmp-${path}`, [['=', 'is'], ['!=', 'is not']], rule.cmp || '=', 'data-k="cmp"') : '') + crmInput(`r-v-${path}`, rule.value || '', 'data-k="value"');
  }
  return `<div class="crm-rule" data-path="${path}">${crmSelect(`r-f-${path}`, fields.map(x => [x.Field, x.Label]), f.Field, 'data-k="field"')}<span class="crm-rule-value">${value}</span>
    <button type="button" class="action-btn" data-del="${path}" aria-label="Remove rule">Remove</button></div>`;
}

function crmGroupHTML(g, path, depth) {
  return `<div class="crm-group" data-gpath="${path}">
    <div class="crm-group-head">${crmSelect(`g-op-${path}`, [['and', 'All of these'], ['or', 'Any of these']], g.op || 'and', 'data-gk="op"')}
      <label class="crm-check"><input type="checkbox" data-gk="not" ${g.not ? 'checked' : ''}> NOT (exclude these)</label>
      ${depth > 0 ? `<button type="button" class="action-btn" data-del="${path}">Remove group</button>` : ''}</div>
    ${(g.rules || []).map((r, i) => r.group ? crmGroupHTML(r.group, `${path}.${i}`, depth + 1) : crmRuleHTML(r, `${path}.${i}`)).join('')}
    <div class="crm-group-add"><button type="button" class="btn btn-outline btn-sm" data-add-rule="${path}">+ Rule</button>
      ${depth < 1 ? `<button type="button" class="btn btn-outline btn-sm" data-add-group="${path}">+ Group</button>` : ''}</div></div>`;
}

function crmNode(def, path) {
  const parts = path.split('.').slice(1).map(Number);
  let g = def;
  parts.forEach(i => { g = g.rules[i].group || g.rules[i]; });
  return g;
}

async function crmSegmentEdit(host, id) {
  let seg = { name: '', description: '', definition: { op: 'and', rules: [{ field: 'spend', cmp: '>=', value: 1000 }] } };
  if (id) { const r = await crmApi('segments/' + encodeURIComponent(id)); if (!r) return; seg = r; }
  const def = seg.definition;
  const body = crmFrame(host, { title: id ? seg.name : 'New segment', back: () => crmGo('segments'), backLabel: 'Segments' });
  const lists = (await crmApi('lists', { quiet: true }) || {}).lists || [];
  body.innerHTML = `<div class="crm-grid crm-grid-seg"><div>
      <div class="crm-form-row">${crmField('sg-name', 'Name', crmInput('sg-name', seg.name))}${crmField('sg-desc', 'About', crmInput('sg-desc', seg.description || ''))}</div>
      <div id="sg-tree"></div>
      ${crmMeta.can_manage ? `<div class="crm-form-actions"><button class="btn btn-primary" type="button" id="sg-save">Save${id ? ' as a new version' : ''}</button></div>` : ''}
      ${id ? `<p class="text-muted crm-small">Version ${seg.current_version}. Saving a change makes a new version; campaigns keep the version they were approved with.</p>` : ''}
    </div><aside class="crm-aside"><h3 class="crm-section">Who is in it now</h3>
      ${crmField('sg-ch', 'For a message by', crmSelect('sg-ch', [['', 'Any channel'], 'WhatsApp', 'SMS', 'Email']))}
      <div id="sg-count" class="crm-waterfall"><p class="text-muted">Counting…</p></div>
      <h3 class="crm-section">Why is a customer in it?</h3>
      <div class="crm-form-row"><input class="form-input" id="sg-why-c" placeholder="Customer"><button class="btn btn-outline btn-sm" type="button" id="sg-why">Check</button></div><div id="sg-why-out"></div>
    </aside></div>`;
  const tree = body.querySelector('#sg-tree');
  const whyPick = crmCustomerPicker(body.querySelector('#sg-why-c'));
  let countTimer = 0;
  const count = () => {
    clearTimeout(countTimer);
    countTimer = setTimeout(async () => {
      const out = body.querySelector('#sg-count');
      const r = await crmApi('segments/count', { method: 'POST', body: { definition: def, channel: crmVal(body, 'sg-ch') }, quiet: true });
      if (!r) { out.innerHTML = '<p class="text-muted">Finish the rules to see the count.</p>'; return; }
      out.innerHTML = `<div class="crm-wf"><span>Matched</span><b>${r.matched}</b></div>
        ${r.channel ? `<div class="crm-wf crm-wf-minus"><span>No consent for ${crmEsc(r.channel)}</span><b>- ${r.no_consent}</b></div>
        <div class="crm-wf crm-wf-minus"><span>No ${r.channel === 'Email' ? 'email' : 'phone'}</span><b>- ${r.no_contact}</b></div>
        <div class="crm-wf crm-wf-total"><span>Can be messaged</span><b>${r.final}</b></div>` : ''}
        <p class="text-muted crm-small">${(r.lifecycle || []).filter(l => l.count).map(l => `${crmEsc(l.state)} ${l.count}`).join(' · ')}</p>
        ${(r.sample || []).length ? `<p class="crm-small">For example: ${r.sample.slice(0, 5).map(s => crmEsc(s.name)).join(', ')}</p>` : ''}
        <p class="text-muted crm-small">Days are counted in ${crmEsc(r.time_zone)} time. A customer is checked again for consent when the message goes.</p>`;
    }, 350);
  };
  const paint = () => {
    tree.innerHTML = crmGroupHTML(def, 'root', 0);
    tree.querySelectorAll('[data-lists]').forEach(sel => {
      sel.innerHTML = `<option value="">Choose a list</option>` + lists.map(l => `<option value="${crmEsc(l.id)}"${l.id === sel.dataset.lists ? ' selected' : ''}>${crmEsc(l.name)}${l.expired ? ' (expired)' : ''}</option>`).join('');
    });
    count();
  };
  tree.addEventListener('change', (e) => {
    const el = e.target;
    const ruleEl = el.closest('[data-path]'), groupEl = el.closest('[data-gpath]');
    if (el.dataset.gk && groupEl) {
      const g = crmNode(def, groupEl.dataset.gpath);
      g[el.dataset.gk] = el.type === 'checkbox' ? el.checked : el.value;
      count(); return;
    }
    if (!ruleEl || !el.dataset.k) return;
    const r = crmNode(def, ruleEl.dataset.path);
    const k = el.dataset.k;
    let v = el.value;
    if (['days'].includes(k)) v = Number(v) || 0;
    if (k === 'value' || k === 'value2') { const f = (crmMeta.segment_fields || []).find(x => x.Field === r.field); if (f && f.Kind === 'number') v = v === '' ? '' : Number(v); }
    r[k] = v;
    if (k === 'field') { Object.keys(r).forEach(x => { if (x !== 'field') delete r[x]; }); paint(); return; }
    if (k === 'cmp') { paint(); return; }
    count();
  });
  tree.addEventListener('input', (e) => { if (e.target.dataset.k && e.target.tagName === 'INPUT') e.target.dispatchEvent(new Event('change', { bubbles: true })); });
  tree.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    if (b.dataset.addRule) { crmNode(def, b.dataset.addRule).rules.push({ field: 'visits', cmp: '>=', value: 2 }); paint(); }
    if (b.dataset.addGroup) { crmNode(def, b.dataset.addGroup).rules.push({ group: { op: 'or', rules: [{ field: 'tier', value: 'Gold' }] } }); paint(); }
    if (b.dataset.del) {
      const parts = b.dataset.del.split('.');
      const idx = Number(parts.pop());
      crmNode(def, parts.join('.')).rules.splice(idx, 1); paint();
    }
  });
  body.querySelector('#sg-ch').addEventListener('change', count);
  body.querySelector('#sg-why').addEventListener('click', async () => {
    const r = await crmApi('segments/explain', { method: 'POST', body: { definition: def, customer_id: whyPick() } });
    if (r) body.querySelector('#sg-why-out').innerHTML = `<p><b>${r.matched ? 'In the segment' : 'Not in the segment'}</b></p><ul class="crm-why">${(r.rules || []).map(x =>
      `<li style="margin-left:${x.depth * 14}px">${x.matched ? '✓' : '✗'} ${crmEsc(x.text)}</li>`).join('')}</ul>`;
  });
  body.querySelector('#sg-save')?.addEventListener('click', async () => {
    const r = await crmApi(id ? 'segments/' + encodeURIComponent(id) : 'segments', { method: id ? 'PUT' : 'POST', body: { name: crmVal(body, 'sg-name'), description: crmVal(body, 'sg-desc'), definition: def } });
    if (r) { showToast(`Saved - version ${r.current_version}.`, { variant: 'success' }); crmGo('segments'); }
  });
  paint();
}

async function crmLists(host) {
  const body = crmFrame(host, { title: 'Uploaded lists', back: () => crmGo('segments'), backLabel: 'Segments' });
  const r = await crmApi('lists');
  if (!r) return;
  body.innerHTML = `${crmMeta.can_manage ? `<form class="crm-form crm-form-inline" id="ls-form">
      ${crmField('ls-name', 'Name', crmInput('ls-name', '', 'required'))}
      ${crmField('ls-src', 'Where the numbers came from', crmInput('ls-src', '', 'required placeholder="Exhibition sign-up sheet, Oct 2026"'))}
      ${crmField('ls-exp', 'Use until', crmInput('ls-exp', '', 'type="date"'))}
      ${crmField('ls-file', 'CSV file (a phone column)', '<input class="form-input" type="file" id="ls-file" accept=".csv,text/csv" required>')}
      <button class="btn btn-primary" type="submit">Upload</button></form>
      <p class="text-muted crm-small">A list never gives consent: everyone on it is still checked for consent when a message goes. An expired list matches nobody.</p>` : ''}
    <div id="ls-out"></div>
    ${crmTable([{ label: 'List', key: 'name' }, { label: 'Source', key: 'source' }, { label: 'Numbers', key: 'rows', num: true }, { label: 'Customers found', key: 'matched', num: true },
      { label: 'Until', html: l => l.expires_on ? crmDate(l.expires_on) + (l.expired ? ' ' + crmChip('Expired', 'muted') : '') : 'No end' }, { label: 'Added', html: l => crmDate(l.created_at) }], r.lists, { empty: 'No lists uploaded.' })}`;
  body.querySelector('#ls-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData();
    fd.append('name', crmVal(body, 'ls-name')); fd.append('source', crmVal(body, 'ls-src')); fd.append('expires_on', crmVal(body, 'ls-exp'));
    fd.append('file', body.querySelector('#ls-file').files[0]);
    const res = await crmApi('lists', { method: 'POST', body: fd });
    if (res) {
      showToast(`Uploaded: ${res.list.rows} numbers, ${res.list.matched} customers found.`, { variant: 'success' });
      if ((res.errors || []).length) body.querySelector('#ls-out').innerHTML = `<div class="crm-note crm-note-warn">${res.errors.length} row(s) skipped: ${res.errors.slice(0, 10).map(x => `row ${x.row_number} ${crmEsc(x.message)}`).join('; ')}</div>`;
      else crmLists(host);
    }
  });
}
