// My Profile screen (164.4): moved out of app.js so the cold core stays small.
// Loaded on first use through LAZY_VIEW_MODULES (profile); uses the shell globals.
// My Profile (Stage 21): self-service account view - read-only account
// info (including a best-effort linked Employee lookup), change own
// password, and set the personal idle-timeout preference that drives
// setupIdleTimeout() (see logout()/armIdleTimer() above). Reached from
// the sidebar account-menu popover, not a sidebar list item of its own.
const IDLE_TIMEOUT_OPTIONS = [
  { value: 0, label: 'Never' },
  { value: 15, label: '15 minutes' },
  { value: 30, label: '30 minutes' },
  { value: 60, label: '1 hour' },
  { value: 120, label: '2 hours' },
  { value: 720, label: '12 hours' }
];

export async function renderProfileView(container) {
  const res = await apiFetch('/api/v1/me');
  if (!res) return;
  if (!res.ok) {
    renderErrorPanel(container, 'Failed to load your profile.', () => renderView('profile'));
    return;
  }
  const data = await res.json();

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">My Profile</h1>
    </div>
  `;
  container.appendChild(header);

  const infoPanel = document.createElement('div');
  infoPanel.className = 'table-panel';
  infoPanel.style.padding = '24px';
  const employeeText = data.employee_id
    ? `${data.employee_name || data.employee_id} (${data.employee_id})`
    : 'Not linked';
  infoPanel.innerHTML = `
    <h3 class="card-title s-29b798">Account Info</h3>
    <div class="s-1e0e35">
      <div><span class="stat-label">Username</span><div class="s-f20693">${escapeHTMLText(data.username)}</div></div>
      <div><span class="stat-label">Role</span><div class="s-f20693">${escapeHTMLText(data.role)}</div></div>
      <div><span class="stat-label">Status</span><div class="s-96ad60"><span class="badge ${data.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${escapeHTMLText(data.status)}</span></div></div>
      <div><span class="stat-label">Employee</span><div class="s-f20693">${escapeHTMLText(employeeText)}</div></div>
      <div><span class="stat-label">Two-Factor Authentication</span><div class="s-96ad60"><span class="badge ${data.mfa_enabled ? 'badge-success' : 'badge-secondary'}">${data.mfa_enabled ? 'Enabled' : 'Not enabled'}</span></div></div>
    </div>
  `;
  container.appendChild(infoPanel);

  const settingsPanel = document.createElement('div');
  settingsPanel.className = 'table-panel';
  settingsPanel.style.padding = '24px';
  settingsPanel.innerHTML = `
    <h3 class="card-title s-29b798">Contact &amp; Session</h3>
    <form id="profile-settings-form">
      <div class="s-e1e267">
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-email">Email</label>
          <input type="email" id="profile-email" class="form-input" value="${escapeHTMLText(data.email || '')}">
        </div>
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-idle-timeout">Auto Logout (inactivity)</label>
          <select id="profile-idle-timeout" class="form-select">
            ${IDLE_TIMEOUT_OPTIONS.map(o => `<option value="${o.value}" ${o.value === data.idle_timeout_minutes ? 'selected' : ''}>${o.label}</option>`).join('')}
          </select>
        </div>
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-settings-current-password">Current Password (only needed if changing Email)</label>
          <input type="password" id="profile-settings-current-password" class="form-input" autocomplete="current-password">
        </div>
      </div>
      <button type="submit" class="btn btn-primary s-4c0544">Save Changes</button>
    </form>
  `;
  container.appendChild(settingsPanel);

  const passwordPanel = document.createElement('div');
  passwordPanel.className = 'table-panel';
  passwordPanel.style.padding = '24px';
  passwordPanel.innerHTML = `
    <h3 class="card-title s-29b798">Change Password</h3>
    <form id="profile-password-form">
      <div class="s-e1e267">
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-current-password">Current Password</label>
          <input type="password" id="profile-current-password" class="form-input" autocomplete="current-password" required>
        </div>
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-new-password">New Password</label>
          <input type="password" id="profile-new-password" class="form-input" autocomplete="new-password" minlength="12" required>
        </div>
        <div class="form-group s-50435f">
          <label class="form-label" for="profile-confirm-password">Confirm New Password</label>
          <input type="password" id="profile-confirm-password" class="form-input" autocomplete="new-password" minlength="12" required>
        </div>
      </div>
      <button type="submit" class="btn btn-primary s-4c0544">Update Password</button>
    </form>
  `;
  container.appendChild(passwordPanel);

  // 32.5: two-factor recovery, only for accounts with MFA enrolled (nothing
  // to manage otherwise).
  if (data.mfa_enabled) {
    const mfaPanel = document.createElement('div');
    mfaPanel.className = 'table-panel';
    mfaPanel.style.padding = '24px';
    mfaPanel.id = 'profile-mfa-panel';
    container.appendChild(mfaPanel);
    await renderMFARecoveryPanel(mfaPanel);
  }

  document.getElementById('profile-settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const email = document.getElementById('profile-email').value.trim();
    const idleTimeoutMinutesVal = parseInt(document.getElementById('profile-idle-timeout').value, 10);
    // 49.2.4: only required by the server when the email is actually
    // changing - left blank here is fine for an idle-timeout-only save.
    const currentPassword = document.getElementById('profile-settings-current-password').value;
    const saveRes = await apiFetch('/api/v1/me', {
      method: 'PUT',
      body: JSON.stringify({ email, current_password: currentPassword, idle_timeout_minutes: idleTimeoutMinutesVal })
    });
    if (!saveRes) return;
    if (!saveRes.ok) {
      await showApiError(saveRes, 'Failed to save changes.');
      return;
    }
    document.getElementById('profile-settings-current-password').value = '';
    setupIdleTimeout(idleTimeoutMinutesVal);
    if (state.profile) {
      state.profile.email = email;
      state.profile.idle_timeout_minutes = idleTimeoutMinutesVal;
    }
    const emailEl = document.getElementById('account-popover-email');
    if (emailEl) emailEl.textContent = email;
    await showCustomAlert('Your profile has been updated.', 'Saved');
  });

  document.getElementById('profile-password-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const currentPassword = document.getElementById('profile-current-password').value;
    const newPassword = document.getElementById('profile-new-password').value;
    const confirmPassword = document.getElementById('profile-confirm-password').value;
    if (newPassword !== confirmPassword) {
      await showCustomAlert('New password and confirmation do not match.', 'Error');
      return;
    }
    const changeRes = await apiFetch('/api/v1/me/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
    });
    if (!changeRes) return;
    if (!changeRes.ok) {
      await showApiError(changeRes, 'Failed to change password.');
      return;
    }
    document.getElementById('profile-password-form').reset();
    await showCustomAlert('Your password has been updated.', 'Saved');
  });
}

// --- 32.5: profile-side two-factor recovery ------------------------------
//
// The counterpart to the login-screen recovery flow. Between them these close
// the lockout hole: a recovery code gets you in without your phone, and this
// panel is where you get a fresh set and move the authenticator to a new
// device. Before Stage 32.5 neither existed, and a replaced phone meant SSH
// to the server plus a hand-written UPDATE against the users table.

// buildRecoveryCodesNode renders codes for showCustomAlert, which already
// accepts a DOM node - so this reuses the existing dialog rather than adding
// a third dialog system. Copy/Download match the login screen's buttons so
// the two places these codes appear behave identically.
function buildRecoveryCodesNode(codes) {
  const wrap = document.createElement('div');

  const warning = document.createElement('p');
  warning.style.cssText = 'font-size: 13px; margin: 0 0 10px;';
  warning.textContent = 'Shown only once - save them. Old codes no longer work.';
  wrap.appendChild(warning);

  const list = document.createElement('pre');
  list.className = 'recovery-code-list';
  list.textContent = codes.join('\n');
  wrap.appendChild(list);

  const row = document.createElement('div');
  row.style.cssText = 'display: flex; gap: 8px;';
  const copyBtn = document.createElement('button');
  copyBtn.type = 'button';
  copyBtn.className = 'btn btn-secondary';
  copyBtn.style.flex = '1';
  copyBtn.textContent = 'Copy';
  copyBtn.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(codes.join('\n'));
      showToast('Recovery codes copied to the clipboard', { variant: 'success' });
    } catch (err) {
      showToast('Could not copy - please select the codes and copy them manually', { variant: 'warning' });
    }
  });
  const dlBtn = document.createElement('button');
  dlBtn.type = 'button';
  dlBtn.className = 'btn btn-secondary';
  dlBtn.style.flex = '1';
  dlBtn.textContent = 'Download';
  dlBtn.addEventListener('click', () => {
    const blob = new Blob(
      ['CustomERP two-factor recovery codes\n' +
       'Each code can be used once, in place of your authenticator code.\n\n' +
       codes.join('\n') + '\n'],
      { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'custom-erp-recovery-codes.txt';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  });
  row.appendChild(copyBtn);
  row.appendChild(dlBtn);
  wrap.appendChild(row);
  return wrap;
}

// buildAuthenticatorSecretNode shows the manual-entry secret for a new
// device, laid out the same way the login screen's enrollment step does it.
function buildAuthenticatorSecretNode(secret) {
  const wrap = document.createElement('div');

  const intro = document.createElement('p');
  intro.style.cssText = 'font-size: 13px; margin: 0 0 10px;';
  intro.textContent = 'Add this code to your authenticator app.';
  wrap.appendChild(intro);

  const code = document.createElement('code');
  code.style.cssText = 'display: block; word-break: break-all; padding: 10px; background: var(--bg-color); border: 1px solid var(--border-color); border-radius: 8px; font-size: 13px; user-select: all;';
  code.textContent = secret;
  wrap.appendChild(code);

  return wrap;
}

async function renderMFARecoveryPanel(panel) {
  const res = await apiFetch('/api/v1/me/mfa/recovery-codes');
  if (!res || !res.ok) {
    panel.innerHTML = `
      <h3 class="card-title s-16949d">Two-Factor Recovery</h3>
      <p class="s-17d019">Could not load your recovery-code status.</p>`;
    return;
  }
  const info = await res.json();
  const remaining = Number(info.remaining || 0);
  const perSet = Number(info.issued_per_set || 10);

  // "N ways back in if your phone dies"; zero is shown in danger colours.
  let statusBadge, statusNote;
  if (remaining === 0) {
    statusBadge = '<span class="badge badge-danger">No recovery codes</span>';
    statusNote = 'Lose your phone and you are locked out. Generate a set now.';
  } else if (remaining <= 2) {
    statusBadge = `<span class="badge badge-warning">${remaining} of ${perSet} left</span>`;
    statusNote = 'Nearly out. Generate a fresh set.';
  } else {
    statusBadge = `<span class="badge badge-success">${remaining} of ${perSet} left</span>`;
    statusNote = 'Each code works once.';
  }

  const pendingNote = info.reenroll_in_progress
    ? `<p class="s-02213e">
         Device change part-finished. Resume or cancel it.</p>`
    : '';

  panel.innerHTML = `
    <h3 class="card-title s-5beb8f">Two-Factor Recovery</h3>
    <div class="s-16949d">${statusBadge}</div>
    <p class="s-c80544">${statusNote}</p>
    ${pendingNote}
    <div class="s-192902">
      <button type="button" class="btn btn-secondary" id="mfa-regen-btn">Generate new recovery codes</button>
      <button type="button" class="btn btn-secondary" id="mfa-newdevice-btn">Set up a new authenticator device</button>
      ${info.reenroll_in_progress ? '<button type="button" class="btn btn-secondary" id="mfa-cancel-reenroll-btn">Cancel device change</button>' : ''}
      ${info.remembered_devices ? `<button type="button" class="btn btn-secondary" id="mfa-forget-devices-btn">Forget remembered devices (${info.remembered_devices})</button>` : ''}
    </div>
  `;

  document.getElementById('mfa-forget-devices-btn')?.addEventListener('click', async () => {
    const r = await apiFetch('/api/v1/me/mfa/devices/forget', { method: 'POST', body: '{}' });
    if (r && !r.ok) return showApiError(r, 'Failed to forget your devices.');
    localStorage.removeItem('erp_mfa_device:' + localStorage.getItem('erp_username'));
    await renderMFARecoveryPanel(panel);
  });
  document.getElementById('mfa-regen-btn').addEventListener('click', async () => {
    const password = await showCustomPrompt(
      'Confirm your password to generate a new set. This immediately invalidates any codes you already hold.',
      '', 'Generate Recovery Codes', 'password');
    if (password === null) return;
    const genRes = await apiFetch('/api/v1/me/mfa/recovery-codes/regenerate', {
      method: 'POST',
      body: JSON.stringify({ password })
    });
    if (!genRes) return;
    if (!genRes.ok) {
      await showApiError(genRes, 'Failed to generate recovery codes.');
      return;
    }
    const out = await genRes.json();
    await showCustomAlert(buildRecoveryCodesNode(out.recovery_codes || []), 'Your New Recovery Codes');
    await renderMFARecoveryPanel(panel);
  });

  document.getElementById('mfa-newdevice-btn').addEventListener('click', () => startMFADeviceChange(panel));

  const cancelBtn = document.getElementById('mfa-cancel-reenroll-btn');
  if (cancelBtn) {
    cancelBtn.addEventListener('click', async () => {
      const cancelRes = await apiFetch('/api/v1/me/mfa/reenroll/cancel', { method: 'POST' });
      if (!cancelRes) return;
      if (!cancelRes.ok) {
        await showApiError(cancelRes, 'Failed to cancel the device change.');
        return;
      }
      await renderMFARecoveryPanel(panel);
    });
  }
}

// startMFADeviceChange walks the "my phone was replaced" flow. The new secret
// is parked server-side and the existing authenticator keeps working until a
// code from the new device is accepted - so abandoning this halfway cannot
// itself cause a lockout.
async function startMFADeviceChange(panel) {
  const password = await showCustomPrompt(
    'Confirm your password to set up a new authenticator device. Your current device keeps working until the new one is confirmed.',
    '', 'New Authenticator Device', 'password');
  if (password === null) return;

  const startRes = await apiFetch('/api/v1/me/mfa/reenroll', {
    method: 'POST',
    body: JSON.stringify({ password })
  });
  if (!startRes) return;
  if (!startRes.ok) {
    await showApiError(startRes, 'Failed to start the device change.');
    return;
  }
  const { secret } = await startRes.json();

  // The secret goes in its own dialog rather than inline in the prompt text:
  // showCustomPrompt sets its message with textContent, so a newline there
  // would collapse and leave a 32-character base32 string running into the
  // sentence around it - unreadable for something that gets typed by hand.
  await showCustomAlert(buildAuthenticatorSecretNode(secret), 'New Authenticator Device');

  const code = await showCustomPrompt(
    'Enter the 6-digit code your authenticator app now shows for this account.',
    '', 'New Authenticator Device');
  if (code === null) {
    await renderMFARecoveryPanel(panel);
    return;
  }

  const confirmRes = await apiFetch('/api/v1/me/mfa/reenroll/confirm', {
    method: 'POST',
    body: JSON.stringify({ code: (code || '').trim() })
  });
  if (!confirmRes) return;
  if (!confirmRes.ok) {
    await showApiError(confirmRes, 'That code did not match. Your previous device is still active.');
    await renderMFARecoveryPanel(panel);
    return;
  }
  const out = await confirmRes.json();
  if (Array.isArray(out.recovery_codes) && out.recovery_codes.length) {
    await showCustomAlert(buildRecoveryCodesNode(out.recovery_codes), 'New Device Active - Save These Codes');
  } else {
    await showCustomAlert('Your new authenticator device is active.', 'Done');
  }
  await renderMFARecoveryPanel(panel);
}
