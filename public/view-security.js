// Stage 49.11.3 / 49.11.7 / 49.12.3 / 49.12.4: Settings -> Monitoring -> Security Events.
// Loaded on first open of the tab (view-admin.js). One client's detections,
// evidence health and risky grants; a request trace by correlation id; the
// forensic evidence pack; and the emergency controls, each asking a reason
// and the administrator's own password.

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const when = (v) => (v ? formatDateTime(v) : '-');

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
    + `<div class="s-69ffa6">${esc(m[6])} · ${esc(m[8])}${esc(m[9])}</div>`;
}

function stat(label, value, bad) {
  return `<div class="table-panel s-96a620">
    <div class="s-8cadba">${esc(label)}</div>
    <div class="u-fs15 u-fw6${bad ? ' u-bad' : ''}">${esc(value)}</div>
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
  target.innerHTML = '<p class="s-de2e81">Loading…</p>';
  const [pRes, uRes, sRes, aRes] = await Promise.all([apiFetch('/api/v1/admin/security/posture'), apiFetch('/api/v1/admin/users'), apiFetch('/api/v1/admin/security/sessions'), apiFetch('/api/v1/admin/security/access-reviews')]);
  if (!pRes || !pRes.ok) {
    target.innerHTML = '<p class="s-057137">Could not load security. <button class="btn btn-outline btn-sm" data-sec="retry">Try Again</button></p>';
    target.querySelector('[data-sec="retry"]').addEventListener('click', () => renderSecurityPane(target));
    return;
  }
  const p = await pRes.json();
  const users = uRes && uRes.ok ? await uRes.json() : [];
  const sessions = sRes && sRes.ok ? ((await sRes.json()).sessions || []) : [];
  const review = aRes && aRes.ok ? await aRes.json() : { days: 0, overdue: 0, rows: [] };
  const sweepAge = p.last_sweep ? Math.round((Date.now() - new Date(p.last_sweep)) / 60000) : null;

  target.innerHTML = `
    <div class="s-d9895d">
      ${stat('Checks last ran', sweepAge == null ? 'Not yet on this server' : `${sweepAge} min ago`, sweepAge != null && sweepAge > 15)}
      ${stat('Audit evidence', p.audit_unsealed_hours >= 3 ? `Unsealed ${p.audit_unsealed_hours} h` : 'Sealed', p.audit_unsealed_hours >= 3 || p.audit_verify_failures_7d > 0)}
      ${stat('Verifier failures (7 days)', p.audit_verify_failures_7d, p.audit_verify_failures_7d > 0)}
      ${stat('Verifier last ran', p.last_verify ? `${esc(p.last_verify.outcome)} · ${esc(when(p.last_verify.at))}` : 'Not yet', !!p.last_verify && p.last_verify.outcome !== 'Passed')}
      ${stat('Audit writes lost', p.audit_writes_lost, p.audit_writes_lost > 0)}
      ${stat('Urgent alerts sent', p.alerts_delivered ? 'Yes' : 'Not set up', !p.alerts_delivered)}
      ${p.test_alert && p.test_alert.every_hours > 0 ? stat('Test alert delivered', p.test_alert.last_delivered ? esc(when(p.test_alert.last_delivered)) : 'Not yet', p.test_alert.last_failed) : ''}
      ${stat('Exports', p.exports_frozen ? 'Frozen' : 'Open', p.exports_frozen)}
    </div>

    <div class="table-panel">
      <div class="s-7a1377">
        <h3 class="s-f38017">Detections (14 days)</h3>
        <button class="btn btn-outline btn-sm" data-sec="run">Run checks now</button>
      </div>
      <div class="table-wrapper"><table>
        <thead><tr><th>When</th><th>Level</th><th>What happened and what to do</th></tr></thead>
        <tbody>${p.detections.length === 0 ? '<tr><td colspan="3" class="s-306094">Nothing detected.</td></tr>'
          : p.detections.map(d => `<tr><td class="s-7e9fe5">${esc(when(d.at))}</td><td>${sevBadge(d.severity)}</td><td class="s-6cb285">${detectionHtml(d.details)}</td></tr>`).join('')}</tbody>
      </table></div>
    </div>

    <div class="table-panel s-ef1f18">
      <h3 class="s-c477f7">To review</h3>
      <p class="s-5e0faa"><strong>Admins without 2FA:</strong> ${p.admins_without_2fa.length ? p.admins_without_2fa.map(esc).join(', ') : 'none'}</p>
      <p class="s-5e0faa"><strong>Two-factor for sensitive actions:</strong> ${esc({off:'off', report:'report only', enforce:'required'}[p.mfa_tier_policy] || 'off')}${p.mfa_tier_policy === 'off' ? '' : ' (' + esc(p.mfa_tier_logged_14d || 0) + ' logged in 14 days)'}</p>
      <p class="s-5e0faa"><strong>No sign-in for ${esc(p.dormant_days)} days${p.dormant_auto_disable ? ' (auto-disable on)' : ''}:</strong> ${(p.dormant_accounts || []).length ? p.dormant_accounts.map(a => esc(a.username)).join(', ') : 'none'}</p>
      <p class="s-5e0faa"><strong>API keys:</strong> ${p.credentials.length ? p.credentials.map(c => `${esc(c.name)} (${esc(c.why)})`).join(', ') : 'none to review'}</p>
      <p class="s-5e0faa"><strong>Jobs given up (7 days):</strong> ${esc(p.dead_lettered_jobs_7d)}</p>
      <p class="s-5e0faa"><strong>Sign-in friction (14 days):</strong> ${((f) => `${esc(f.locked || 0)} locked · ${esc(f.password_resets || 0)} password resets · ${esc(f.lost_authenticator || 0)} lost authenticator · ${esc(f.help_not_helpful || 0)} "not helpful" on sign-in help`)(p.sign_in_friction_14d || {})}</p>
    </div>

    <div class="table-panel s-ef1f18">
      <h3 class="s-c477f7">Privileged sessions</h3>
      <div class="table-container"><table class="data-table s-046678">
        <thead><tr><th>User</th><th>Signed in</th><th>Last seen</th><th>From</th><th>Device</th><th>State</th><th></th></tr></thead>
        <tbody>${sessions.length === 0 ? '<tr><td colspan="7" class="s-5872de">None yet</td></tr>'
          : sessions.map(s => `<tr><td>${esc(s.username)}</td><td class="s-7e9fe5">${esc(when(s.started_at))}</td><td class="s-7e9fe5">${esc(when(s.last_seen_at))}</td><td>${esc(s.address || '-')}</td><td class="s-6cb285">${esc(s.device || '-')}</td><td>${esc(s.state)}</td><td>${s.state === 'Active' ? `<button class="btn btn-outline btn-sm" data-sec-end="${esc(s.id)}">End</button>` : ''}</td></tr>`).join('')}</tbody>
      </table></div>
    </div>

    <div class="table-panel s-1b0f49">
      <div class="s-8ac016">
        <h3 class="s-f38017">Access review${review.days ? ` · every ${esc(review.days)} days · ${esc(review.overdue)} overdue` : ''}</h3>
      </div>
      <div class="table-wrapper"><table>
        <thead><tr><th>User</th><th>Role</th><th>Stores</th><th>Last review</th><th></th></tr></thead>
        <tbody>${review.rows.length === 0 ? '<tr><td colspan="5" class="s-306094">No active users.</td></tr>'
          : review.rows.map(r => `<tr><td>${esc(r.username)}</td><td>${esc(r.role)}</td><td class="s-6cb285">${esc(r.location_code)}${r.store_access ? ' · ' + esc(r.store_access) : ''}</td>
            <td class="s-6cb285">${r.last_reviewed_at ? `${esc(r.last_decision)} by ${esc(r.last_reviewed_by)} · ${esc(when(r.last_reviewed_at))}` : 'Never'}${r.overdue ? ' <span class="badge badge-warning">Overdue</span>' : ''}</td>
            <td class="s-86abda"><button class="btn btn-outline btn-sm" data-rv-ok="${esc(r.id)}">Confirm</button> <button class="btn btn-outline btn-sm" data-rv-no="${esc(r.id)}">Revoke</button></td></tr>`).join('')}</tbody>
      </table></div>
    </div>

    <div class="table-panel s-ef1f18">
      <h3 class="s-c477f7">Trace a request</h3>
      <div class="s-b32917">
        <input class="form-input s-cdf035" data-sec="corr" placeholder="Reference from an error message">
        <button class="btn btn-outline" data-sec="trace">Trace</button>
      </div>
      <div data-sec="trail" class="s-56f435"></div>
    </div>

    <div class="table-panel s-ef1f18">
      <h3 class="s-c477f7">Evidence pack</h3>
      <div class="s-9260d3">
        <label class="s-5e0faa">From <input type="date" class="form-input" data-sec="from"></label>
        <label class="s-5e0faa">To <input type="date" class="form-input" data-sec="to"></label>
        <button class="btn btn-outline" data-sec="pack">Download</button>
      </div>
    </div>

    <div class="table-panel s-bb3791">
      <h3 class="s-c477f7">Emergency</h3>
      <div class="s-9260d3">
        <select class="form-input s-47b618" data-sec="user">
          <option value="">Choose a user…</option>
          ${users.map(u => `<option value="${esc(u.id)}">${esc(u.username)} · ${esc(u.role)}</option>`).join('')}
        </select>
        <button class="btn btn-outline" data-sec="signout">Sign out everywhere</button>
        <button class="btn btn-outline" data-sec="signout-all">Sign out everyone</button>
        <button class="btn ${p.exports_frozen ? 'btn-primary' : 'btn-outline'}" data-sec="freeze">${p.exports_frozen ? 'Unfreeze exports' : 'Freeze exports'}</button>
        <button class="btn ${p.integrations_frozen ? 'btn-primary' : 'btn-outline'}" data-sec="freeze-int">${p.integrations_frozen ? 'Unfreeze integrations' : 'Freeze integrations'}</button>
        <button class="btn ${p.read_only ? 'btn-primary' : 'btn-outline'}" data-sec="read-only">${p.read_only ? 'End read-only' : 'Read-only'}</button>
      </div>
    </div>`;

  const $ = (k) => target.querySelector(`[data-sec="${k}"]`);
  const localDay = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  $('to').value = localDay(new Date());
  $('from').value = localDay(new Date(Date.now() - 7 * 86400000));

  const decideReview = async (userId, decision) => {
    let note = '';
    if (decision === 'Revoked') {
      const n = await showCustomPrompt('Reason', '', 'Revoke access');
      if (n == null) return;
      if (!n.trim()) { showToast('A reason is required.'); return; }
      note = n.trim();
    }
    const res = await apiFetch('/api/v1/admin/security/access-reviews', { method: 'POST', body: JSON.stringify({ user_id: userId, decision, note }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The review was not saved.'); return; }
    showToast(decision === 'Revoked' ? 'Access revoked.' : 'Confirmed.');
    renderSecurityPane(target);
  };
  target.querySelectorAll('[data-rv-ok]').forEach(b => b.addEventListener('click', () => decideReview(b.dataset.rvOk, 'Confirmed')));
  target.querySelectorAll('[data-rv-no]').forEach(b => b.addEventListener('click', () => decideReview(b.dataset.rvNo, 'Revoked')));

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
    $('trail').innerHTML = entries.length === 0 ? '<p class="s-b14cdc">Nothing found for that reference.</p>'
      : `<div class="table-wrapper"><table><thead><tr><th>When</th><th>Where</th><th>Who</th><th>What</th><th>Result</th><th>Detail</th></tr></thead><tbody>
        ${entries.map(e => `<tr><td class="s-7e9fe5">${esc(when(e.at))}</td><td>${esc(e.source)}</td><td>${esc(e.who)}</td><td>${esc(e.what)}</td><td>${esc(e.status)}</td><td class="s-6cb285">${esc(e.detail)}</td></tr>`).join('')}
        </tbody></table></div>`;
  });

  $('pack').addEventListener('click', async () => {
    const from = $('from').value, to = $('to').value;
    if (!from || !to) { showToast('Choose both dates.'); return; }
    // 49.12.4: the pack is encrypted with a passphrase typed here, twice.
    const pass = await showCustomPrompt('Passphrase (12+ characters)', '', 'Evidence pack', 'password');
    if (pass == null) return;
    if ([...pass].length < 12) { showToast('At least 12 characters.'); return; }
    const again = await showCustomPrompt('Type it again', '', 'Evidence pack', 'password');
    if (again == null) return;
    if (again !== pass) { showToast('The passphrases differ.'); return; }
    const res = await apiFetch(`/api/v1/admin/audit-logs/forensic-export?from=${from}&to=${to}`, { headers: { 'X-Evidence-Passphrase': encodeURIComponent(pass) } });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not build the evidence pack.'); return; }
    const url = URL.createObjectURL(await res.blob());
    const a = document.createElement('a');
    a.href = url;
    a.download = `evidence-${from}-to-${to}.zip.enc`;
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
  target.querySelectorAll('[data-sec-end]').forEach(btn => btn.addEventListener('click', async () => {
    if (await runControl('/api/v1/admin/security/sessions/end', { id: btn.dataset.secEnd }, 'End session')) renderSecurityPane(target);
  }));
  $('freeze-int').addEventListener('click', async () => {
    const on = !p.integrations_frozen;
    if (await runControl('/api/v1/admin/security/freeze-integrations', { on }, on ? 'Freeze integrations' : 'Unfreeze integrations')) renderSecurityPane(target);
  });
  $('read-only').addEventListener('click', async () => {
    const on = !p.read_only; // 49.12.3
    if (on && !await showCustomConfirm('Refuse every change until you end it?', 'Read-only')) return;
    if (await runControl('/api/v1/admin/security/read-only', { on }, on ? 'Read-only' : 'End read-only')) renderSecurityPane(target);
  });
  $('freeze').addEventListener('click', async () => {
    const on = !p.exports_frozen;
    if (await runControl('/api/v1/admin/security/freeze-exports', { on }, on ? 'Freeze exports' : 'Unfreeze exports')) renderSecurityPane(target);
  });
  import('./view-security-notify.js' + new URL(import.meta.url).search).then(m => m.renderSecurityExtras(target)).catch(e => console.error(e));
}
