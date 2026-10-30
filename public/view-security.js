// Stage 49.11.3 / 49.11.7 / 49.12.3 / 49.12.4: Activity Log -> Security.
// Loaded on first open of the tab (view-admin.js). One client's detections,
// evidence health and risky grants; a request trace by correlation id; the
// forensic evidence pack; and the emergency controls, each asking a reason
// and the administrator's own password.

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const when = (v) => (v ? new Date(v).toLocaleString() : '-');

function sevBadge(sev) {
  return `<span class="badge ${sev === 'URGENT' ? 'badge-danger' : 'badge-warning'}">${sev === 'URGENT' ? 'Urgent' : 'Review'}</span>`;
}

// A detection row reads: title · who · count, the first response, then owner,
// runbook and the evidence it names - not the stored rule=/subject= prefix.
const DETECTION = /^rule=(\S+) subject=(.*?): (\d+) in (\d+) min - (.*?)\. Owner: (.*?)\. First response: (.*?) Runbook: (\S+ §\d+(?:\.\d+)?)\.(.*)$/;
function detectionHtml(details) {
  const m = DETECTION.exec(details || '');
  if (!m) return esc(details);
  return `<strong>${esc(m[5])}</strong> · ${esc(m[2])} · ${esc(m[3])} in ${esc(m[4])} min<br>${esc(m[7])}`
    + `<div style="font-size:11px; color:var(--text-muted);">${esc(m[6])} · ${esc(m[8])}${esc(m[9])}</div>`;
}

function stat(label, value, bad) {
  return `<div class="table-panel" style="padding:12px 16px; min-width:150px; flex:1; min-height:0;">
    <div style="font-size:12px; color:var(--text-muted);">${esc(label)}</div>
    <div style="font-size:15px; font-weight:600; color:${bad ? 'var(--danger-color)' : 'inherit'};">${esc(value)}</div>
  </div>`;
}

async function askReasonAndPassword(title) {
  const reason = await showCustomPrompt('Reason (goes into the audit log)', '', title);
  if (reason == null || reason.trim().length < 5) {
    if (reason != null) showToast('Write a reason of at least 5 characters.');
    return null;
  }
  const password = await showCustomPrompt('Your password', '', title, 'password');
  if (!password) return null;
  return { reason: reason.trim(), password };
}

async function runControl(url, body, title) {
  const auth = await askReasonAndPassword(title);
  if (!auth) return false;
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify({ ...body, ...auth }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'The control was not used.'); return false; }
  const out = await res.json();
  showToast(out.detail || 'Done.');
  return true;
}

export async function renderSecurityPane(target) {
  target.innerHTML = '<p style="padding:16px; color:var(--text-muted);">Loading…</p>';
  const [pRes, uRes] = await Promise.all([apiFetch('/api/v1/admin/security/posture'), apiFetch('/api/v1/admin/users')]);
  if (!pRes || !pRes.ok) {
    target.innerHTML = '<p style="padding:16px; color:var(--danger-color);">Could not load security. <button class="btn btn-outline btn-sm" data-sec="retry">Try Again</button></p>';
    target.querySelector('[data-sec="retry"]').addEventListener('click', () => renderSecurityPane(target));
    return;
  }
  const p = await pRes.json();
  const users = uRes && uRes.ok ? await uRes.json() : [];
  const sweepAge = p.last_sweep ? Math.round((Date.now() - new Date(p.last_sweep)) / 60000) : null;

  target.innerHTML = `
    <div style="display:flex; gap:12px; flex-wrap:wrap; margin-bottom:16px;">
      ${stat('Checks last ran', sweepAge == null ? 'Not yet on this server' : `${sweepAge} min ago`, sweepAge != null && sweepAge > 15)}
      ${stat('Audit evidence', p.audit_unsealed_hours >= 3 ? `Unsealed ${p.audit_unsealed_hours} h` : 'Sealed', p.audit_unsealed_hours >= 3 || p.audit_verify_failures_7d > 0)}
      ${stat('Verifier failures (7 days)', p.audit_verify_failures_7d, p.audit_verify_failures_7d > 0)}
      ${stat('Verifier last ran', p.last_verify ? `${esc(p.last_verify.outcome)} · ${esc(when(p.last_verify.at))}` : 'Not yet', !!p.last_verify && p.last_verify.outcome !== 'Passed')}
      ${stat('Audit writes lost', p.audit_writes_lost, p.audit_writes_lost > 0)}
      ${stat('Urgent alerts sent', p.alerts_delivered ? 'Yes' : 'Not set up', !p.alerts_delivered)}
      ${stat('Exports', p.exports_frozen ? 'Frozen' : 'Open', p.exports_frozen)}
    </div>

    <div class="table-panel">
      <div style="display:flex; justify-content:space-between; align-items:center; padding:16px 16px 0;">
        <h3 style="font-size:16px; font-weight:600;">Detections (14 days)</h3>
        <button class="btn btn-outline btn-sm" data-sec="run">Run checks now</button>
      </div>
      <div class="table-wrapper"><table>
        <thead><tr><th>When</th><th>Level</th><th>What happened and what to do</th></tr></thead>
        <tbody>${p.detections.length === 0 ? '<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">Nothing detected.</td></tr>'
          : p.detections.map(d => `<tr><td style="white-space:nowrap; font-size:12px;">${esc(when(d.at))}</td><td>${sevBadge(d.severity)}</td><td style="font-size:12px;">${detectionHtml(d.details)}</td></tr>`).join('')}</tbody>
      </table></div>
    </div>

    <div class="table-panel" style="margin-top:16px; padding:16px; min-height:0;">
      <h3 style="font-size:16px; font-weight:600; margin-bottom:8px;">To review</h3>
      <p style="font-size:13px;"><strong>Admins without 2FA:</strong> ${p.admins_without_2fa.length ? p.admins_without_2fa.map(esc).join(', ') : 'none'}</p>
      <p style="font-size:13px;"><strong>No sign-in for ${esc(p.dormant_days)} days${p.dormant_auto_disable ? ' (auto-disable on)' : ''}:</strong> ${(p.dormant_accounts || []).length ? p.dormant_accounts.map(a => esc(a.username)).join(', ') : 'none'}</p>
      <p style="font-size:13px;"><strong>API keys:</strong> ${p.credentials.length ? p.credentials.map(c => `${esc(c.name)} (${esc(c.why)})`).join(', ') : 'none to review'}</p>
      <p style="font-size:13px;"><strong>Jobs given up (7 days):</strong> ${esc(p.dead_lettered_jobs_7d)}</p>
    </div>

    <div class="table-panel" style="margin-top:16px; padding:16px; min-height:0;">
      <h3 style="font-size:16px; font-weight:600; margin-bottom:8px;">Trace a request</h3>
      <div style="display:flex; gap:8px; flex-wrap:wrap;">
        <input class="form-input" data-sec="corr" placeholder="Reference from an error message" style="flex:1; min-width:220px;">
        <button class="btn btn-outline" data-sec="trace">Trace</button>
      </div>
      <div data-sec="trail" style="margin-top:12px;"></div>
    </div>

    <div class="table-panel" style="margin-top:16px; padding:16px; min-height:0;">
      <h3 style="font-size:16px; font-weight:600; margin-bottom:8px;">Evidence pack</h3>
      <div style="display:flex; gap:8px; flex-wrap:wrap; align-items:center;">
        <label style="font-size:13px;">From <input type="date" class="form-input" data-sec="from"></label>
        <label style="font-size:13px;">To <input type="date" class="form-input" data-sec="to"></label>
        <button class="btn btn-outline" data-sec="pack">Download</button>
      </div>
    </div>

    <div class="table-panel" style="margin-top:16px; padding:16px; min-height:0; border-left:3px solid var(--danger-color);">
      <h3 style="font-size:16px; font-weight:600; margin-bottom:8px;">Emergency</h3>
      <div style="display:flex; gap:8px; flex-wrap:wrap; align-items:center;">
        <select class="form-input" data-sec="user" style="min-width:200px;">
          <option value="">Choose a user…</option>
          ${users.map(u => `<option value="${esc(u.id)}">${esc(u.username)} · ${esc(u.role)}</option>`).join('')}
        </select>
        <button class="btn btn-outline" data-sec="signout">Sign out everywhere</button>
        <button class="btn btn-outline" data-sec="signout-all">Sign out everyone</button>
        <button class="btn ${p.exports_frozen ? 'btn-primary' : 'btn-outline'}" data-sec="freeze">${p.exports_frozen ? 'Unfreeze exports' : 'Freeze exports'}</button>
      </div>
    </div>`;

  const $ = (k) => target.querySelector(`[data-sec="${k}"]`);
  const localDay = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  $('to').value = localDay(new Date());
  $('from').value = localDay(new Date(Date.now() - 7 * 86400000));

  $('run').addEventListener('click', async () => {
    const res = await apiFetch('/api/v1/admin/security/detections/run', { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Checks did not run.'); return; }
    const out = await res.json();
    showToast(out.fired.length ? `${out.fired.length} new detection(s).` : 'Nothing new.');
    renderSecurityPane(target);
  });

  $('trace').addEventListener('click', async () => {
    const id = $('corr').value.trim();
    if (!id) return;
    const res = await apiFetch(`/api/v1/admin/security/trail?correlation_id=${encodeURIComponent(id)}`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not trace that reference.'); return; }
    const { entries } = await res.json();
    $('trail').innerHTML = entries.length === 0 ? '<p style="font-size:13px; color:var(--text-muted);">Nothing found for that reference.</p>'
      : `<div class="table-wrapper"><table><thead><tr><th>When</th><th>Where</th><th>Who</th><th>What</th><th>Result</th><th>Detail</th></tr></thead><tbody>
        ${entries.map(e => `<tr><td style="white-space:nowrap; font-size:12px;">${esc(when(e.at))}</td><td>${esc(e.source)}</td><td>${esc(e.who)}</td><td>${esc(e.what)}</td><td>${esc(e.status)}</td><td style="font-size:12px;">${esc(e.detail)}</td></tr>`).join('')}
        </tbody></table></div>`;
  });

  $('pack').addEventListener('click', async () => {
    const from = $('from').value, to = $('to').value;
    if (!from || !to) { showToast('Choose both dates.'); return; }
    const res = await apiFetch(`/api/v1/admin/audit-logs/forensic-export?from=${from}&to=${to}`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not build the evidence pack.'); return; }
    const url = URL.createObjectURL(await res.blob());
    const a = document.createElement('a');
    a.href = url;
    a.download = `evidence-${from}-to-${to}.zip`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });

  $('signout').addEventListener('click', async () => {
    const id = $('user').value;
    if (!id) { showToast('Choose a user first.'); return; }
    await runControl('/api/v1/admin/security/sign-out-user', { id }, 'Sign out everywhere');
  });
  $('signout-all').addEventListener('click', async () => {
    if (!await showCustomConfirm('Sign out every user of this business except you?', 'Sign out everyone')) return;
    await runControl('/api/v1/admin/security/sign-out-all', {}, 'Sign out everyone');
  });
  $('freeze').addEventListener('click', async () => {
    const on = !p.exports_frozen;
    if (await runControl('/api/v1/admin/security/freeze-exports', { on }, on ? 'Freeze exports' : 'Unfreeze exports')) renderSecurityPane(target);
  });
}
