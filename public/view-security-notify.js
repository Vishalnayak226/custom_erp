// Stage 49.11.6 / 49.12.5: two of the Settings -> Monitoring security pages (ask 311).
// Event export, the security event export destination, sits on Trace & Evidence;
// Breach Notifications holds the notification decisions: a versioned matrix, the
// clocks, and a person's decision with a named approver. Loaded by
// view-security.js; nothing here says who must be told.

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const when = (v) => (v ? formatDateTime(v) : '-');
const ago = (s) => (s < 90 ? `${s} s` : s < 5400 ? `${Math.round(s / 60)} min` : `${Math.round(s / 3600)} h`);

// clock reads a deadline against now (the server's, not this machine's).
// 49.12.5: state is the server's check (due / overdue) shown as a badge.
function clock(at, now, state) {
  if (!at) return '-';
  const left = Math.round((new Date(at) - now) / 60000);
  const abs = Math.abs(left);
  const span = abs >= 120 ? `${Math.round(abs / 60)} h` : `${abs} min`;
  const badge = state === 'overdue' ? ' <span class="badge badge-danger">Overdue</span>' : state === 'due' ? ' <span class="badge badge-warning">Due</span>' : '';
  return (left < 0 ? `<span class="s-25f4ca">${span} over</span>` : `${span} left`) + badge;
}

async function getJSON(url) {
  const res = await apiFetch(url);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'Could not load.'); return null; }
  return res.json();
}

async function post(url, body, failText) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

export async function renderExport(host) {
  const s = await getJSON('/api/v1/admin/security/export');
  if (!s) return;
  const box = document.createElement('div');
  box.className = 'table-panel';
  box.style.cssText = 'padding:16px; margin-top:16px; min-height:0;';
  const state = !s.enabled ? 'Off' : s.last_error ? 'Failing' : s.last_success_at ? 'Sending' : 'Waiting';
  box.innerHTML = `<h3 class="s-249d01">Event export</h3>
    <div class="s-aad644">
      <label class="s-3ea979">Address (https)<input class="form-input" data-ex="url" value="${esc(s.url)}" maxlength="500"></label>
      <label class="s-7378da">Signing secret<input class="form-input" data-ex="secret" type="password" autocomplete="new-password" placeholder="${s.has_secret ? 'Saved' : '16 characters or more'}"></label>
      <label class="s-c303f5"><input type="checkbox" data-ex="on" ${s.enabled ? 'checked' : ''}> On</label>
      <button class="btn btn-primary" data-ex="save">Save</button>
      <button class="btn btn-outline" data-ex="test">Test send</button>
      ${s.enabled ? `<label class="s-5e0faa">History from<input class="form-input" type="date" data-ex="bfrom"></label><button class="btn btn-outline" data-ex="backfill">Send history</button>` : ''}
    </div>
    <div class="s-530743">
      <span>State: <strong>${state}</strong></span>
      <span>Last sent: ${esc(when(s.last_success_at))}</span>
      <span>Sent in all: ${esc(s.exported_total)}</span>
      ${s.backfill_from ? `<span>History from ${esc(String(s.backfill_from).slice(0, 10))}: ${esc(s.backfill_left)} events left</span>` : ''}
      ${s.enabled ? `<span>Waiting: ${esc(s.waiting)}${s.waiting ? ` · oldest ${esc(ago(s.lag_seconds))}` : ''}</span>` : ''}
      ${s.allowed ? '' : `<span class="s-25f4ca">${esc(s.allowed_why)}</span>`}
    </div>
    ${s.last_error ? `<div class="s-0e9ab0">${esc(s.last_error)}</div>` : ''}
    ${s.gap_note ? `<div class="s-0e9ab0">${esc(s.gap_note)}</div>` : ''}`;
  host.appendChild(box);
  const $ = (k) => box.querySelector(`[data-ex="${k}"]`);
  $('save').addEventListener('click', async () => {
    const password = await showCustomPrompt('Your password', '', 'Save event export', 'password');
    if (!password) return;
    const out = await post('/api/v1/admin/security/export', { url: $('url').value.trim(), secret: $('secret').value, enabled: $('on').checked, password }, 'Not saved.');
    if (!out) return;
    showToast('Saved.');
    box.remove();
    renderExport(host);
  });
  if ($('backfill')) $('backfill').addEventListener('click', async () => {
    const from = $('bfrom').value;
    if (!from) { showToast('Pick a start date.'); return; }
    const password = await showCustomPrompt('Your password', '', 'Send history', 'password');
    if (!password) return;
    const out = await post('/api/v1/admin/security/export/backfill', { from, password }, 'History not started.');
    if (!out) return;
    showToast(out.events + ' events will be sent.');
    box.remove();
    renderExport(host);
  });
  $('test').addEventListener('click', async () => {
    const out = await post('/api/v1/admin/security/export/test', {}, 'Test not sent.');
    if (out) showToast(out.detail || 'Sent.');
    box.remove();
    renderExport(host);
  });
}

const MATRIX_COLS = [['name', 'Name'], ['trigger', 'Trigger'], ['decider', 'Decider'], ['contact', 'Contact'], ['source', 'Source']];

// Returns false when the matrix or the decisions did not load, so the page offers Try Again.
export async function renderNotify(host) {
  const [m, d] = await Promise.all([getJSON('/api/v1/admin/security/notify/matrix'), getJSON('/api/v1/admin/security/notify/decisions')]);
  if (!m || !d) return false;
  // 49.12.5: the server says who may be named - an Admin or a Privacy Approver, not you.
  const apRes = await apiFetch('/api/v1/admin/security/notify/approvers');
  const users = apRes && apRes.ok ? ((await apRes.json()).approvers || []).map(username => ({ username })) : [];
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  const now = new Date(d.now);
  const decisions = d.decisions || [];
  const rows = m.current.entries || [];

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<div class="s-69ffa6"><a href="/help/incident-response" target="_blank" rel="noopener">Incident runbook</a></div><div class="table-wrapper"><table>
    <thead><tr><th>Incident</th><th>Who to tell</th><th>Version</th><th>Noticed</th><th>Assess</th><th>Target</th><th>Status</th><th>Decision</th><th></th></tr></thead>
    <tbody>${decisions.length === 0 ? '<tr><td colspan="9" class="text-muted">None yet</td></tr>' : decisions.map(x => `<tr>
      <td>${esc(x.incident_title)}</td>
      <td>${esc(x.row.name)}${x.row.needs_counsel ? ' <span class="badge badge-warning">Needs counsel review</span>' : ''}</td>
      <td>${esc(x.matrix_version)}</td>
      <td class="s-7e9fe5">${esc(when(x.discovered_at))}</td>
      <td>${x.status === 'Closed' ? '-' : clock(x.assessment_due_at, now, x.assess_state)}</td>
      <td>${x.status === 'Closed' ? '-' : clock(x.target_at, now, x.target_state)}</td>
      <td>${esc(x.status)}</td>
      <td>${x.decision === 'pending' ? 'Pending' : x.decision === 'notify' ? 'Notify' : 'Do not notify'}${x.reason ? `<div class="s-69ffa6">${esc(x.decided_by)} · ${esc(x.evidence_ref)}${x.approver ? ` · approver ${esc(x.approver)}` : ''}${x.approval_note ? ` · ${esc(x.approval_note)}` : ''}</div>` : ''}</td>
      <td class="s-86abda">${x.status === 'Closed' ? '' : `<button class="btn btn-outline btn-sm" data-nd-decide="${esc(x.id)}">Decide</button>`}
        ${x.status === 'Awaiting approval' && (x.approver || '').toLowerCase() === (localStorage.getItem('erp_username') || '').toLowerCase() ? `<button class="btn btn-primary btn-sm" data-nd-ok="${esc(x.id)}">Approve</button> <button class="btn btn-outline btn-sm" data-nd-back="${esc(x.id)}">Send back</button>` : ''}</td>
    </tr>`).join('')}</tbody></table></div>`;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  const iso = (dt) => new Date(dt.getTime() - dt.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  form.innerHTML = `<div class="s-8d094f">
      <label class="s-75b088">Incident<input class="form-input" data-ni="title" maxlength="200"></label>
      <label class="s-5e0faa">Noticed at<input type="datetime-local" class="form-input" data-ni="at" value="${iso(new Date())}"></label>
    </div>
    <div class="s-da12f2">${rows.map(r => `<label class="s-82e6e0"><input type="checkbox" data-ni-key="${esc(r.key)}" checked> ${esc(r.name)}</label>`).join('')}</div>
    <button class="btn btn-primary" data-ni="make">Start the clocks</button>`;

  const matrix = document.createElement('div');
  matrix.className = 'table-panel';
  matrix.style.padding = '24px';
  const rowHtml = (r) => `<tr data-mx-row data-key="${esc(r.key)}" data-kind="${esc(r.kind || 'jurisdiction')}">
    <td><input class="form-input s-607468" data-f="key" value="${esc(r.key)}" ${r.key ? 'readonly' : ''}></td>
    ${MATRIX_COLS.map(([f]) => `<td><input class="form-input s-4592c7" data-f="${f}" value="${esc(r[f])}"></td>`).join('')}
    <td><input class="form-input s-bd09c4" data-f="target_hours" type="text" inputmode="numeric" value="${esc(r.target_hours || 0)}"></td>
    <td><input class="form-input s-bd09c4" data-f="assessment_hours" type="text" inputmode="numeric" value="${esc(r.assessment_hours || 0)}"></td>
    <td><input type="checkbox" data-f="needs_counsel" ${r.needs_counsel ? 'checked' : ''}></td></tr>`;
  matrix.innerHTML = `<div class="s-04dd4d">Version ${esc(m.current.version)}</div>
    <div class="table-wrapper"><table><thead><tr><th>Key</th>${MATRIX_COLS.map(([, l]) => `<th>${l}</th>`).join('')}<th>Hours to tell</th><th>Hours to assess</th><th>Counsel</th></tr></thead>
    <tbody data-mx="body">${rows.map(rowHtml).join('')}</tbody></table></div>
    <div class="s-e20666">
      <label class="s-aa8788">What changed<input class="form-input" data-mx="note" maxlength="300"></label>
      <button class="btn btn-outline" data-mx="add">Add row</button>
      <button class="btn btn-primary" data-mx="save">Save as new version</button></div>`;

  const decide = document.createElement('div');
  decide.className = 'table-panel hidden';
  decide.style.cssText = 'padding:16px; margin-top:16px; min-height:0;';
  decide.innerHTML = `<h3 class="s-249d01">Decision</h3>
    <div class="s-aad644">
      <label class="s-5e0faa">Decision<select class="form-select" data-dc="decision"><option value="pending">Pending</option><option value="notify">Notify</option><option value="do_not_notify">Do not notify</option></select></label>
      <label class="s-af83c7">Reason<input class="form-input" data-dc="reason" maxlength="1000"></label>
      <label class="s-a6e80c">Evidence reference<input class="form-input" data-dc="evidence" maxlength="500"></label>
      <label class="s-5e0faa">Approver<select class="form-select" data-dc="approver">${users.map(u => `<option value="${esc(u.username)}">${esc(u.username)}</option>`).join('')}</select></label>
      <button class="btn btn-primary" data-dc="save">Save</button>
      <button class="btn btn-outline" data-dc="cancel">Cancel</button>
    </div>`;

  const wrap = document.createElement('div');
  wrap.style.marginTop = '16px';
  wrap.appendChild(txnPagedSection({ id: 'sec-notify', title: 'Notification decisions', page: 'New incident', count: decisions.length,
    forms: { new: ['New incident', form], matrix: ['Matrix', matrix] }, list, heading: 'h2' }));
  wrap.appendChild(decide);
  host.appendChild(wrap);
  txnRestorePane('sec-notify');

  const again = () => { wrap.remove(); renderNotify(host); };
  const q = (root, sel) => root.querySelector(sel);

  q(form, '[data-ni="make"]').addEventListener('click', async () => {
    const title = q(form, '[data-ni="title"]').value.trim();
    const keys = Array.from(form.querySelectorAll('[data-ni-key]:checked')).map(b => b.dataset.niKey);
    if (!title) { showToast('Name the incident.'); return; }
    if (!keys.length) { showToast('Tick one.'); return; }
    const out = await post('/api/v1/admin/security/notify/incidents', { title, discovered_at: new Date(q(form, '[data-ni="at"]').value).toISOString(), keys }, 'Not started.');
    if (!out) return;
    showToast('Started.');
    txnDone('sec-notify');
    again();
  });

  q(matrix, '[data-mx="add"]').addEventListener('click', () => {
    q(matrix, '[data-mx="body"]').insertAdjacentHTML('beforeend', rowHtml({ key: '', kind: 'contract', target_hours: 0, assessment_hours: 0 }));
  });
  q(matrix, '[data-mx="save"]').addEventListener('click', async () => {
    const entries = Array.from(matrix.querySelectorAll('[data-mx-row]')).map(tr => {
      const f = (n) => tr.querySelector(`[data-f="${n}"]`);
      return { key: f('key').value, kind: tr.dataset.kind, name: f('name').value, trigger: f('trigger').value, decider: f('decider').value,
        contact: f('contact').value, source: f('source').value, target_hours: parseInt(f('target_hours').value, 10) || 0,
        assessment_hours: parseInt(f('assessment_hours').value, 10) || 0, needs_counsel: f('needs_counsel').checked };
    });
    const out = await post('/api/v1/admin/security/notify/matrix', { entries, note: q(matrix, '[data-mx="note"]').value }, 'Not saved.');
    if (!out) return;
    showToast(`Version ${out.version} saved.`);
    txnDone('sec-notify');
    again();
  });

  let deciding = '';
  list.querySelectorAll('[data-nd-decide]').forEach(b => b.addEventListener('click', () => {
    deciding = b.dataset.ndDecide;
    decide.classList.remove('hidden');
    q(decide, '[data-dc="reason"]').focus();
  }));
  q(decide, '[data-dc="cancel"]').addEventListener('click', () => decide.classList.add('hidden'));
  q(decide, '[data-dc="save"]').addEventListener('click', async () => {
    const out = await post('/api/v1/admin/security/notify/decide', { id: deciding, decision: q(decide, '[data-dc="decision"]').value,
      reason: q(decide, '[data-dc="reason"]').value, evidence_ref: q(decide, '[data-dc="evidence"]').value, approver: q(decide, '[data-dc="approver"]').value }, 'Not saved.');
    if (!out) return;
    showToast('Saved.');
    again();
  });
  const review = async (id, approve) => {
    let note = '';
    if (!approve) {
      note = await showCustomPrompt('Why is it sent back', '', 'Send back');
      if (note == null) return;
    }
    const out = await post('/api/v1/admin/security/notify/review', { id, approve, note, decided_at: (decisions.find(x => x.id === id) || {}).decided_at }, 'Not saved.');
    if (out) { showToast(approve ? 'Approved.' : 'Sent back.'); again(); }
  };
  list.querySelectorAll('[data-nd-ok]').forEach(b => b.addEventListener('click', () => review(b.dataset.ndOk, true)));
  list.querySelectorAll('[data-nd-back]').forEach(b => b.addEventListener('click', () => review(b.dataset.ndBack, false)));
}

// 49.12.5: the Privacy Approver's own screen - only the decisions that name them
// approver, with Approve and Send back. The server filters the list; nothing here decides.
export async function renderPrivacyApprovalsView(container) {
  container.innerHTML = '<div class="page-header"><div class="page-title-section"><h1 class="page-title">Privacy approvals</h1></div></div>';
  const host = document.createElement('div');
  container.appendChild(host);
  await drawPrivacyApprovals(host);
}

// The view is built off-screen and its children moved onto the page, so a
// refresh redraws this host, which moves with them, not the original container.
async function drawPrivacyApprovals(host) {
  const d = await getJSON('/api/v1/admin/security/notify/decisions');
  host.replaceChildren();
  if (!d) return;
  const me = (localStorage.getItem('erp_username') || '').toLowerCase();
  const mine = (d.decisions || []).filter(x => x.status === 'Awaiting approval' && (x.approver || '').toLowerCase() === me);
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<div class="table-wrapper"><table>
    <thead><tr><th>Incident</th><th>Who to tell</th><th>Decision</th><th>Decided by</th><th>Evidence</th><th>Reason</th><th></th></tr></thead>
    <tbody>${mine.length === 0 ? '<tr><td colspan="7" class="text-muted">Nothing waiting</td></tr>' : mine.map(x => `<tr>
      <td>${esc(x.incident_title)}</td><td>${esc(x.row.name)}</td>
      <td>${x.decision === 'notify' ? 'Notify' : 'Do not notify'}</td>
      <td>${esc(x.decided_by)}</td><td>${esc(x.evidence_ref)}</td><td>${esc(x.reason)}</td>
      <td class="s-86abda"><button class="btn btn-primary btn-sm" data-pa-ok="${esc(x.id)}">Approve</button> <button class="btn btn-outline btn-sm" data-pa-back="${esc(x.id)}">Send back</button></td>
    </tr>`).join('')}</tbody></table></div>`;
  host.appendChild(list);
  const review = async (id, approve) => {
    let note = '';
    if (!approve) {
      note = await showCustomPrompt('Why is it sent back', '', 'Send back');
      if (note == null) return;
    }
    const out = await post('/api/v1/admin/security/notify/review', { id, approve, note, decided_at: (mine.find(x => x.id === id) || {}).decided_at }, 'Not saved.');
    if (out) { showToast(approve ? 'Approved.' : 'Sent back.'); drawPrivacyApprovals(host); }
  };
  list.querySelectorAll('[data-pa-ok]').forEach(b => b.addEventListener('click', () => review(b.dataset.paOk, true)));
  list.querySelectorAll('[data-pa-back]').forEach(b => b.addEventListener('click', () => review(b.dataset.paBack, false)));
}
