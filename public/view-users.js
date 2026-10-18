// Users (Stage 21 QA fix; moved out of app.js and given its Stores section in
// Stage 85.A2). tenant users is a raw SQL table, not a generic doctype, so
// internal/server/handlers_admin_identity.go backs this screen - every call
// Super-Admin-only on the server.
//
// Stage 85.A2 (user, 2026-10-18): "User wise Admin will give the store
// allocation. First role than user ... In User, There will a section to
// allocate store." Each user's Stores button opens the shared editor
// (view-store.js): home store, Same as role / All / Selected / Home only, and
// the stores. The list follows the user's own rule: list first, + New User as
// its own page (Stage 69).

const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
const storeModule = () => loadViewModule('/view-store.js?v=1');

const esc = (s) => window.escapeHTMLText(String(s ?? ''));

export async function renderUsersView(container) {
  const [usersRes, rolesRes, storesRes, store] = await Promise.all([
    apiFetch('/api/v1/admin/users'),
    apiFetch('/api/v1/admin/roles'),
    apiFetch('/api/v1/admin/users/stores'),
    storeModule(),
  ]);
  if (!usersRes || !rolesRes) return;
  if (!usersRes.ok) { renderErrorPanel(container, 'Failed to load users.', () => renderView('users')); return; }
  const users = await usersRes.json();
  const roles = rolesRes.ok ? await rolesRes.json() : [];
  const stores = storesRes && storesRes.ok ? ((await storesRes.json()).stores || []) : [];
  const storeName = (code) => (stores.find(o => o.code === code) || {}).name || code || 'HO';

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = `
    <div class="form-grid" style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; align-items: end;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-username">Username</label>
        <input type="text" id="user-username" class="form-input" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-password">Password</label>
        <input type="password" id="user-password" class="form-input" autocomplete="new-password" minlength="12">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-email">Email</label>
        <input type="email" id="user-email" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-role">Role</label>
        <select id="user-role" class="form-select">
          ${roles.map(r => `<option value="${esc(r)}">${esc(r)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-location">Home store</label>
        <select id="user-location" class="form-select">
          ${stores.length ? stores.map(o => `<option value="${esc(o.code)}" ${o.code === 'HO' ? 'selected' : ''}>${esc(o.name)}</option>`).join('')
            : '<option value="HO">HO</option>'}
        </select>
      </div>
    </div>
    <p class="txn-hint" style="margin-top: 12px;">The user works at their home store and at the stores their role gives them. Use <b>Stores</b> on the list to give them more.</p>
    <div style="margin-top: 16px;"><button class="btn btn-primary" id="user-create-btn">Create User</button></div>
    <div id="user-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `
    <table>
      <thead><tr><th>Username</th><th>Email</th><th>Role</th><th>Home store</th><th>Stores</th><th>Status</th><th></th></tr></thead>
      <tbody>${users.length === 0
        ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No users yet. Use <b>+ New User</b> to add the first one.</td></tr>`
        : users.map(u => `
        <tr>
          <td style="font-weight:600;">${esc(u.username)}</td>
          <td>${esc(u.email || '')}</td>
          <td>${esc(u.role)}</td>
          <td>${esc(storeName(u.location_code))}</td>
          <td>${isAdminRoleName(u.role) ? 'All stores' : esc(store.storeAccessSummary(u.store_access, u.stores))}</td>
          <td><span class="badge ${u.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${esc(u.status)}</span></td>
          <td>
            <button class="action-btn" ${actionAttrs('editUserStores', [u.id, u.username])}>Stores</button>
            <button class="action-btn" ${actionAttrs('resetUserMFA', [u.id, u.username])}>Reset 2FA</button>
            <button class="action-btn" ${actionAttrs('resetUserPassword', [u.id, u.username])}>Reset Password</button>
            ${u.role === 'Supplier' ? `<button class="action-btn" ${actionAttrs('setUserSupplier', [u.id, u.supplier_code || ''])}>Link Vendor</button>` : ''}
            ${u.status === 'Active'
              ? `<button class="action-btn action-btn-danger" ${actionAttrs('setUserStatus', [u.id, 'Inactive'])}>Deactivate</button>`
              : `<button class="action-btn" ${actionAttrs('setUserStatus', [u.id, 'Active'])}>Reactivate</button>`}
          </td>
        </tr>`).join('')}
      </tbody>
    </table>`;

  container.appendChild(txnPagedSection({ id: 'users-screen', title: 'Users', page: 'New user', count: users.length, newLabel: 'New User', form, list }));
  txnRestorePane('users-screen');
  document.getElementById('user-create-btn').addEventListener('click', createUser);
}

async function createUser() {
  // BLD-036: same double-submit guard as savePurchaseOrder/createGRN - a
  // repeat click before the first response returns used to be able to
  // create two user accounts from one intent.
  await guardAgainstDoubleSubmit(document.getElementById('user-create-btn'), 'Creating...', createUserInner);
}

async function createUserInner() {
  const errorEl = document.getElementById('user-form-error');
  errorEl.classList.add('hidden');

  const username = document.getElementById('user-username').value.trim();
  const password = document.getElementById('user-password').value;
  const email = document.getElementById('user-email').value.trim();
  const role = document.getElementById('user-role').value;
  const location_code = document.getElementById('user-location').value.trim();

  if (!username || !password || !role) {
    errorEl.textContent = 'Username, password, and role are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/admin/users', {
    method: 'POST',
    body: JSON.stringify({ username, password, email, role, location_code })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create user.';
    errorEl.classList.remove('hidden');
    return;
  }
  // Stage 57.17: the password just typed is the administrator's, so the
  // server will ask the user to choose their own at first sign-in.
  showToast(`User ${username} created. Give them this password - they will be asked to set their own when they first sign in.`, { variant: 'success' });
  txnDone('users-screen');
  renderView('users');
}

// Stage 85.A2: the user's Stores section - home store and store access.
export async function editUserStores(id, username) {
  const store = await storeModule();
  await store.openStoreAccessEditor({ kind: 'user', id, title: `Stores - ${username}`, onSaved: () => renderView('users') });
}

export async function setUserStatus(id, status) {
  const verb = status === 'Active' ? 'reactivate' : 'deactivate';
  if (!(await showCustomConfirm(`Are you sure you want to ${verb} this user?`))) return;
  const res = await apiFetch('/api/v1/admin/users/status', {
    method: 'POST',
    body: JSON.stringify({ id, status })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, `Failed to ${verb} user.`);
    return;
  }
  renderView('users');
}

// 32.5: the admin-side escape hatch for a colleague who lost both their phone
// and their recovery codes. Clears the enrollment rather than disabling MFA,
// so their next login is forced through setup on a new device - previously
// the only route was SSH to the server plus a hand-written UPDATE.
export async function resetUserMFA(id, username) {
  const ok = await showCustomConfirm(
    `Reset two-factor authentication for ${username}? Their authenticator and any recovery codes stop working immediately, and they will be asked to set up a new device at their next login.`,
    'Reset Two-Factor');
  if (!ok) return;
  const res = await apiFetch('/api/v1/admin/users/reset-mfa', {
    method: 'POST',
    body: JSON.stringify({ id })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to reset two-factor authentication.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(data.detail || 'Two-factor authentication has been reset.', 'Done');
}

// 49.2.4: the admin-assisted "helpdesk" password reset. An ordinary account
// is reset immediately and the one-time password is shown once, right here -
// it is never shown again and never emailed, so it must be relayed to the
// user through a secure out-of-band channel (phone call, in person). A Super
// Admin target instead comes back "pending_approval": the request now shows
// up on a second Super Admin's Approvals screen, and only THAT approver ever
// sees the generated password (via decideApproval) - the point of requiring
// a second admin is that the requester alone must not learn it.
export async function resetUserPassword(id, username) {
  const reason = await showCustomPrompt(
    `Reason for resetting ${username}'s password (required if they are a Super Admin):`, '', 'Reset Password');
  if (reason === null) return;
  const res = await apiFetch('/api/v1/admin/users/reset-password', {
    method: 'POST',
    body: JSON.stringify({ id, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to reset password.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(data.detail || 'Password reset.', data.status === 'pending_approval' ? 'Approval Required' : 'Done');
}

// 26.4.10: links a Supplier login to the Vendor it speaks for. Until this is
// set the account can sign in but every screen refuses it - deliberately, an
// unscoped supplier session is the one thing the row-level scoping exists to
// prevent - so this is the step that finishes creating a supplier account.
export async function setUserSupplier(id, currentCode) {
  const supplier_code = await showCustomPrompt(
    'Vendor code this supplier login speaks for. Leave blank to unlink the account.',
    currentCode || '', 'Link Vendor');
  if (supplier_code === null) return;
  const res = await apiFetch('/api/v1/admin/users/supplier', {
    method: 'POST',
    body: JSON.stringify({ id, supplier_code: supplier_code.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update the supplier link.');
    return;
  }
  renderView('users');
}
