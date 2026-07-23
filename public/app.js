// Custom Dialog Helper Utilities
function showCustomAlert(message, title = 'Notification') {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    const titleEl = document.getElementById('custom-dialog-title');
    const msgEl = document.getElementById('custom-dialog-message');
    const okBtn = document.getElementById('custom-dialog-ok-btn');
    const cancelBtn = document.getElementById('custom-dialog-cancel-btn');
    const closeBtn = document.getElementById('custom-dialog-close-btn');

    titleEl.textContent = title;
    msgEl.textContent = message;
    
    cancelBtn.style.display = 'none';
    backdrop.classList.remove('hidden');

    const cleanUp = () => {
      backdrop.classList.add('hidden');
      cancelBtn.style.display = '';
      okBtn.replaceWith(okBtn.cloneNode(true));
      closeBtn.replaceWith(closeBtn.cloneNode(true));
    };

    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => {
      cleanUp();
      resolve(true);
    });

    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => {
      cleanUp();
      resolve(true);
    });
  });
}

function showCustomConfirm(message, title = 'Confirm Action') {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    const titleEl = document.getElementById('custom-dialog-title');
    const msgEl = document.getElementById('custom-dialog-message');
    const okBtn = document.getElementById('custom-dialog-ok-btn');
    const cancelBtn = document.getElementById('custom-dialog-cancel-btn');
    const closeBtn = document.getElementById('custom-dialog-close-btn');

    titleEl.textContent = title;
    msgEl.textContent = message;
    
    cancelBtn.style.display = '';
    backdrop.classList.remove('hidden');

    const cleanUp = () => {
      backdrop.classList.add('hidden');
      okBtn.replaceWith(okBtn.cloneNode(true));
      cancelBtn.replaceWith(cancelBtn.cloneNode(true));
      closeBtn.replaceWith(closeBtn.cloneNode(true));
    };

    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => {
      cleanUp();
      resolve(true);
    });

    document.getElementById('custom-dialog-cancel-btn').addEventListener('click', () => {
      cleanUp();
      resolve(false);
    });

    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => {
      cleanUp();
      resolve(false);
    });
  });
}

function showCustomPrompt(message, defaultValue = '', title = 'Input Required') {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    const titleEl = document.getElementById('custom-dialog-title');
    const msgEl = document.getElementById('custom-dialog-message');
    const extraEl = document.getElementById('custom-dialog-extra');
    const okBtn = document.getElementById('custom-dialog-ok-btn');
    const cancelBtn = document.getElementById('custom-dialog-cancel-btn');
    const closeBtn = document.getElementById('custom-dialog-close-btn');

    titleEl.textContent = title;
    msgEl.textContent = message;
    
    // Create an input field dynamically
    extraEl.innerHTML = `<input type="text" id="custom-dialog-prompt-input" class="form-input" style="width: 100%; margin-top: 12px;" value="${defaultValue}">`;
    extraEl.classList.remove('hidden');
    cancelBtn.style.display = '';

    backdrop.classList.remove('hidden');
    
    const inputEl = document.getElementById('custom-dialog-prompt-input');
    if (inputEl) {
      inputEl.focus();
      inputEl.select();
    }

    const cleanUp = () => {
      backdrop.classList.add('hidden');
      extraEl.innerHTML = '';
      extraEl.classList.add('hidden');
      okBtn.replaceWith(okBtn.cloneNode(true));
      cancelBtn.replaceWith(cancelBtn.cloneNode(true));
      closeBtn.replaceWith(closeBtn.cloneNode(true));
    };

    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => {
      const val = document.getElementById('custom-dialog-prompt-input').value;
      cleanUp();
      resolve(val);
    });

    document.getElementById('custom-dialog-cancel-btn').addEventListener('click', () => {
      cleanUp();
      resolve(null);
    });

    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => {
      cleanUp();
      resolve(null);
    });
  });
}


// Error-reporting helpers - every save/load failure must reach the user
// through the same centered custom dialog used everywhere else, never a
// silent no-op and never a native browser dialog.
// getErrorDetails (Stage 23) - the backend now returns a standardized
// {error, code, correlation_id, retryable} body on every error response
// (internal/server/apierror.go), so this also surfaces the catalog `code`
// for console traceability. getErrorMessage/showApiError keep their
// original signatures for their ~20 existing callers.
async function getErrorDetails(res, fallback) {
  try {
    const data = await res.clone().json();
    if (data && data.error) return { message: data.error, code: data.code || '', displayStyle: data.display_style || '' };
  } catch (e) {
    // Body wasn't JSON (a call site not yet migrated to the standardized
    // envelope) - fall through to the fallback message.
  }
  return { message: fallback, code: '', displayStyle: '' };
}

async function getErrorMessage(res, fallback) {
  return (await getErrorDetails(res, fallback)).message;
}

// Stage 23.8: dispatches by the catalog's own display_style instead of
// always showing the blocking modal. Only "Toast" and "Page banner" are
// generic enough to render without knowing which field/form the error
// belongs to (Inline field message, Modal popup, etc. all keep the modal
// fallback here - see apierror.go's apiErrorBody comment). title is only
// used by the modal fallback, so existing callers passing just (res,
// fallback) are unaffected.
async function showApiError(res, fallback, title = 'Error') {
  const { message, code, displayStyle } = await getErrorDetails(res, fallback);
  if (code) console.debug(`[API error] ${code}`);
  if (displayStyle === 'Toast') {
    showToast(message, { variant: 'warning' });
    return;
  }
  if (displayStyle === 'Page banner') {
    const container = document.getElementById('view-root');
    if (container) {
      renderPageBanner(container, message);
      return;
    }
  }
  await showCustomAlert(message, title);
}

// Inline centered retry panel for full-page load failures, so a failed GET
// doesn't just leave the user staring at a blank view after they dismiss a
// dialog. Mirrors the centered-card layout already used by renderMockModuleView.
function renderErrorPanel(container, message, retryFn) {
  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '48px';
  panel.style.textAlign = 'center';
  panel.innerHTML = `
    <div style="max-width: 480px; margin: 0 auto; display: flex; flex-direction: column; gap: 16px; align-items: center;">
      <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="1.5">
        <circle cx="12" cy="12" r="10"></circle>
        <line x1="12" y1="8" x2="12" y2="12"></line>
        <line x1="12" y1="16" x2="12.01" y2="16"></line>
      </svg>
      <h2 style="font-size: 20px; font-weight: 600;">Something Went Wrong</h2>
      <p class="text-muted" style="font-size: 14px; line-height: 1.6;">${message}</p>
      <button class="btn btn-primary" id="error-panel-retry-btn">Try Again</button>
    </div>
  `;
  container.appendChild(panel);
  const btn = panel.querySelector('#error-panel-retry-btn');
  if (btn && retryFn) btn.addEventListener('click', retryFn);
}

// Toast (Stage 23) - non-blocking, auto-dismissing notice for messages the
// standardized message catalog (docs/specs/message_catalog.md) marks
// Display Style "Toast" (rate-limit, async retry notices, etc.). Distinct
// from showCustomAlert's blocking modal, which stays the right choice for
// anything the user must acknowledge before continuing.
function showToast(message, opts = {}) {
  const variant = opts.variant || 'info'; // 'info' | 'warning' | 'danger' | 'success'
  let container = document.getElementById('toast-container');
  if (!container) {
    container = document.createElement('div');
    container.id = 'toast-container';
    document.body.appendChild(container);
  }

  const toast = document.createElement('div');
  toast.className = `toast toast-${variant}`;
  if (opts.title) {
    const titleEl = document.createElement('div');
    titleEl.className = 'toast-title';
    titleEl.textContent = opts.title;
    toast.appendChild(titleEl);
  }
  const msgEl = document.createElement('div');
  msgEl.className = 'toast-message';
  msgEl.textContent = message;
  toast.appendChild(msgEl);

  container.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add('toast-visible'));

  const dismiss = () => {
    toast.classList.remove('toast-visible');
    setTimeout(() => toast.remove(), 300);
  };
  toast.addEventListener('click', dismiss);
  setTimeout(dismiss, opts.ms || 5000);
}

// Page banner (Stage 23) - dismissible bar at the top of a screen container,
// for messages the catalog marks Display Style "Page banner" (its largest
// single category - module/tenant-level blocks like "Financial period
// locked"). Unlike renderErrorPanel above, this doesn't replace the
// container's content - it sits above a screen that otherwise still renders.
function renderPageBanner(container, message, opts = {}) {
  const variant = opts.variant || 'danger';
  const existing = container.querySelector(':scope > .page-banner');
  if (existing) existing.remove();

  const banner = document.createElement('div');
  banner.className = `page-banner page-banner-${variant}`;

  const msgEl = document.createElement('span');
  msgEl.textContent = message;
  banner.appendChild(msgEl);

  const closeBtn = document.createElement('button');
  closeBtn.type = 'button';
  closeBtn.className = 'page-banner-close';
  closeBtn.setAttribute('aria-label', 'Dismiss');
  closeBtn.textContent = '×';
  closeBtn.addEventListener('click', () => banner.remove());
  banner.appendChild(closeBtn);

  container.insertBefore(banner, container.firstChild);
}

let state = {
  activeDoctypes: [],
  activeDocFields: [],
  docData: [],
  prefixConfigs: [],
  labels: {},
  auditLogs: [],
  systemLogs: [],
  profile: null,
  // 22.6: defaults to "show everything" until the real grant set loads
  // (fetchAndApplyPermissions) - a brief full-menu flash is a better
  // failure mode than a brief empty-sidebar flash, and the server's own
  // checkPermission() is the actual enforcement point regardless of what
  // the sidebar shows.
  permissions: { isAdmin: true, doctypes: new Set(), loaded: false }
};

let currentView = 'dashboard';
let currentDoctype = '';
let posCart = []; // { sku, available, qty, salePrice, costPrice }
let posLocation = '';
let posOpenSessionId = ''; // Stage 20.7: '' means no open cashier session at posLocation
const OFFLINE_QUEUE_KEY = 'erp_pos_offline_queue'; // 20.13, see checkoutOnlineOrQueue below
let offlineSyncInFlight = false;
let currentSearchQuery = '';
let currentTablePage = 1;
let currentExtensionHookLogId = ''; // which hook's log renderExtensionHookLogView shows
const itemsPerPage = 10;
let bulkSelectedDocIDs = new Set();
// 21.9 QA-follow-up: set while the dynamic modal is editing an existing
// record rather than creating a new one - null means "create mode".
let editingDocID = null;
let editingDocVersion = null;

// Selection persistence - so refreshing the browser lands the user back on
// the same view/doctype/search/page instead of always bouncing to Dashboard.
const NAV_STATE_KEY = 'erp_nav_state';

function saveNavState() {
  try {
    localStorage.setItem(NAV_STATE_KEY, JSON.stringify({
      view: currentView,
      doctype: currentDoctype,
      searchQuery: currentSearchQuery,
      page: currentTablePage
    }));
  } catch (e) {
    // localStorage unavailable (private browsing quota, etc.) - not fatal,
    // the app just won't restore the last view on next load.
  }
}

function loadNavState() {
  try {
    const raw = localStorage.getItem(NAV_STATE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch (e) {
    return null;
  }
}

// API Helper wrapper
async function apiFetch(url, options = {}) {
  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';

  const headers = {
    'Content-Type': 'application/json',
    'X-Tenant-ID': tenantID,
    ...options.headers
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  let response;
  try {
    response = await fetch(url, {
      ...options,
      headers
    });
  } catch (err) {
    await showCustomAlert('Unable to reach the server. Please check your connection and try again.', 'Connection Error');
    return null;
  }

  if (response.status === 401) {
    logout(await getErrorMessage(response, 'Session expired. Please log in again.'));
    return null;
  }
  if (response.status === 429) {
    showToast(await getErrorMessage(response, 'Rate limit exceeded. Please throttle your requests.'), { variant: 'warning', title: 'Rate Limit' });
    return null;
  }

  return response;
}

// apiUpload (Stage 15.2): apiFetch always forces 'Content-Type':
// 'application/json', which breaks a multipart/form-data upload (the
// browser needs to set that header itself, with the boundary parameter).
// This duplicates apiFetch's auth/tenant/401/429 handling but omits
// Content-Type entirely so fetch can set it correctly for FormData bodies.
async function apiUpload(url, formData) {
  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const headers = { 'X-Tenant-ID': tenantID };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let response;
  try {
    response = await fetch(url, { method: 'POST', headers, body: formData });
  } catch (err) {
    await showCustomAlert('Unable to reach the server. Please check your connection and try again.', 'Connection Error');
    return null;
  }
  if (response.status === 401) {
    logout(await getErrorMessage(response, 'Session expired. Please log in again.'));
    return null;
  }
  if (response.status === 429) {
    showToast(await getErrorMessage(response, 'Rate limit exceeded. Please throttle your requests.'), { variant: 'warning', title: 'Rate Limit' });
    return null;
  }
  return response;
}

// Reusable master-data autosuggest (Stage 18.2, docs/micro_checklist.md
// Stage 18). Wires a plain text <input> to live search against the
// existing generic GET /api/v1/doc/{doctype}?q=... endpoint, so screens get
// a search-as-you-type picker without a bespoke fetch/debounce/dropdown per
// field. Deliberately additive only: the input stays a normal free-text
// field underneath - picking a suggestion just fills in a value. Nothing
// here adds server-side validation or blocks typing something that doesn't
// match a real record (Stage 17.9, db/migrations_stage17h_location_masters.sql,
// already decided against retrofitting hard validation onto these existing
// free-text columns - this keeps that same call for the same reason).
function attachTypeahead(inputEl, doctype, opts = {}) {
  const valueFields = opts.valueFields || ['code', 'name', 'id'];
  const labelFn = opts.labelFn || (doc => {
    const code = doc.code || doc.id || '';
    const name = doc.name || '';
    return name && name !== code ? `${code} — ${name}` : (code || name);
  });
  const limit = opts.limit || 8;

  let menu = null;
  let items = [];
  let activeIndex = -1;
  let debounceTimer = null;
  let requestSeq = 0;

  function onDocMouseDown(e) {
    if (menu && !menu.contains(e.target) && e.target !== inputEl) closeMenu();
  }

  function removeMenuElement() {
    if (menu) { menu.remove(); menu = null; }
    document.removeEventListener('mousedown', onDocMouseDown, true);
  }

  function closeMenu() {
    removeMenuElement();
    items = [];
    activeIndex = -1;
  }

  function pick(doc) {
    let val = '';
    for (const f of valueFields) {
      if (doc[f] !== undefined && doc[f] !== null && doc[f] !== '') { val = doc[f]; break; }
    }
    inputEl.value = val;
    closeMenu();
    inputEl.dispatchEvent(new Event('change', { bubbles: true }));
    inputEl.focus();
  }

  function highlight(idx) {
    if (!menu) return;
    const rows = menu.querySelectorAll('.typeahead-item');
    rows.forEach(r => r.classList.remove('active'));
    if (idx >= 0 && rows[idx]) {
      rows[idx].classList.add('active');
      rows[idx].scrollIntoView({ block: 'nearest' });
    }
    activeIndex = idx;
  }

  function openMenu() {
    removeMenuElement();
    activeIndex = -1;
    if (items.length === 0) return;
    menu = document.createElement('div');
    menu.className = 'typeahead-menu';
    const rect = inputEl.getBoundingClientRect();
    menu.style.left = `${rect.left}px`;
    menu.style.top = `${rect.bottom + 4}px`;
    menu.style.width = `${Math.max(rect.width, 180)}px`;
    items.forEach((doc) => {
      const row = document.createElement('div');
      row.className = 'typeahead-item';
      row.textContent = labelFn(doc);
      row.addEventListener('mousedown', (e) => { e.preventDefault(); pick(doc); });
      menu.appendChild(row);
    });
    document.body.appendChild(menu);
    document.addEventListener('mousedown', onDocMouseDown, true);
  }

  async function search(q) {
    const seq = ++requestSeq;
    if (!q) { closeMenu(); return; }
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(q)}&limit=${limit}`);
    if (seq !== requestSeq) return; // a newer keystroke's request already superseded this one
    if (!res || !res.ok) { closeMenu(); return; }
    items = await res.json();
    if (seq !== requestSeq) return;
    openMenu();
  }

  inputEl.setAttribute('autocomplete', 'off');
  inputEl.addEventListener('input', () => {
    clearTimeout(debounceTimer);
    const q = inputEl.value.trim();
    debounceTimer = setTimeout(() => search(q), 250);
  });
  inputEl.addEventListener('keydown', (e) => {
    if (!menu || items.length === 0) return;
    if (e.key === 'ArrowDown') { e.preventDefault(); highlight(Math.min(activeIndex + 1, items.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); highlight(Math.max(activeIndex - 1, 0)); }
    else if (e.key === 'Enter') {
      if (activeIndex >= 0) {
        e.preventDefault();
        // A screen-specific Enter handler (e.g. POS's scan-to-add) may also
        // be registered on this same input - stop it from also firing when
        // a suggestion is being picked instead. Only works if this listener
        // was attached before that one, so attachTypeahead() call sites
        // that share Enter with another handler must run first.
        e.stopImmediatePropagation();
        pick(items[activeIndex]);
      }
    }
    else if (e.key === 'Escape') { closeMenu(); }
  });
}

// Auth: login screen, logout, and app-shell visibility

// Holds the short-lived enrollment/challenge token between the initial
// username+password submit and the follow-up TOTP code submit, for
// MFA-mandatory roles (see engines.RequiresMFA / Stage 13.3). Never
// persisted - it's only good for one MFA step and expires in minutes.
let pendingMFAToken = null;

function showLoginScreen() {
  document.getElementById('login-screen').classList.remove('hidden');
  document.getElementById('app-root').classList.add('hidden');
  // Always land back on the username/password step, not a stale MFA screen
  // left over from a previous, unfinished login attempt.
  pendingMFAToken = null;
  document.getElementById('login-form').classList.remove('hidden');
  document.getElementById('mfa-enroll-screen').classList.add('hidden');
  document.getElementById('mfa-challenge-screen').classList.add('hidden');
}

function showApp() {
  document.getElementById('login-screen').classList.add('hidden');
  document.getElementById('app-root').classList.remove('hidden');
  updateSidebarUserInfo();
  restoreIndustrySelector();
}

// There's no backend "current industry" endpoint to read back - the industry
// switch is a one-time overlay operation, not stored state. This is just
// client-side memory of the last profile this browser switched to, same
// tier of persistence as erp_tenant_id.
function restoreIndustrySelector() {
  const saved = localStorage.getItem('erp_industry_code');
  const sel = document.getElementById('industry-selector');
  if (sel && saved && Array.from(sel.options).some(o => o.value === saved)) {
    sel.value = saved;
  }
}

function updateSidebarUserInfo() {
  const username = localStorage.getItem('erp_username') || '';
  const role = localStorage.getItem('erp_role') || '';
  const avatarEl = document.getElementById('sidebar-avatar');
  const nameEl = document.getElementById('sidebar-username');
  const roleEl = document.getElementById('sidebar-role');
  const popoverNameEl = document.getElementById('account-popover-name');
  const popoverRoleEl = document.getElementById('account-popover-role');
  if (nameEl) nameEl.textContent = username;
  if (roleEl) roleEl.textContent = role;
  if (popoverNameEl) popoverNameEl.textContent = username;
  if (popoverRoleEl) popoverRoleEl.textContent = role;
  if (avatarEl) avatarEl.textContent = (username.slice(0, 2) || '??').toUpperCase();
}

// Fetches the logged-in user's own profile (email, linked employee, saved
// idle-timeout preference) once per session - used to fill in the account
// popover's email line and to seed the idle-timeout auto-logout timer.
// Silent no-op on failure (e.g. offline): the sidebar already has
// username/role from localStorage, this is just the enrichment layer.
async function fetchAndApplyProfile() {
  const res = await apiFetch('/api/v1/me');
  if (!res || !res.ok) return;
  const data = await res.json();
  state.profile = data;
  const emailEl = document.getElementById('account-popover-email');
  if (emailEl) emailEl.textContent = data.email || '';
  setupIdleTimeout(data.idle_timeout_minutes);
}

function logout(message) {
  stopIdleTimeout();
  const overlay = document.getElementById('signout-overlay');
  if (overlay) overlay.classList.remove('hidden');
  localStorage.removeItem('erp_token');
  localStorage.removeItem('erp_username');
  localStorage.removeItem('erp_role');
  state.profile = null;
  setTimeout(() => {
    if (overlay) overlay.classList.add('hidden');
    showLoginScreen();
    if (message) {
      showCustomAlert(message, 'Signed Out');
    }
  }, 500);
}

// Idle-timeout / auto-logout (Stage 21): a client-side inactivity timer
// seeded from the user's own Profile-screen preference, separate from and
// shorter than the server-side JWT session TTL (engines/auth.go's
// tokenTTL(), a hard expiry the client can't change). 0 means "never" - no
// timer is armed and only the JWT's own expiry ever signs the user out.
let idleTimeoutTimer = null;
let idleTimeoutMinutes = 0;
const IDLE_ACTIVITY_EVENTS = ['mousemove', 'keydown', 'click', 'scroll'];

function resetIdleTimer() {
  if (!idleTimeoutMinutes) return;
  if (idleTimeoutTimer) clearTimeout(idleTimeoutTimer);
  idleTimeoutTimer = setTimeout(() => {
    logout('You were signed out due to inactivity.');
  }, idleTimeoutMinutes * 60 * 1000);
}

function setupIdleTimeout(minutes) {
  stopIdleTimeout();
  idleTimeoutMinutes = minutes || 0;
  if (!idleTimeoutMinutes) return;
  IDLE_ACTIVITY_EVENTS.forEach(evt => document.addEventListener(evt, resetIdleTimer));
  resetIdleTimer();
}

function stopIdleTimeout() {
  if (idleTimeoutTimer) clearTimeout(idleTimeoutTimer);
  idleTimeoutTimer = null;
  IDLE_ACTIVITY_EVENTS.forEach(evt => document.removeEventListener(evt, resetIdleTimer));
}

async function handleLoginSubmit(event) {
  event.preventDefault();
  const username = document.getElementById('login-username').value.trim();
  const password = document.getElementById('login-password').value;
  const errorEl = document.getElementById('login-error');
  const submitBtn = document.getElementById('login-submit-btn');
  errorEl.classList.add('hidden');
  submitBtn.disabled = true;

  try {
    const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
    const res = await fetch('/api/v1/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Tenant-ID': tenantID },
      body: JSON.stringify({ username, password })
    });
    const data = await res.json();
    if (!res.ok) {
      errorEl.textContent = data.error || 'Login failed. Please check your credentials.';
      errorEl.classList.remove('hidden');
      return;
    }

    if (data.mfa_enrollment_required) {
      pendingMFAToken = data.enrollment_token;
      await startMFAEnrollment();
      return;
    }
    if (data.mfa_required) {
      pendingMFAToken = data.challenge_token;
      document.getElementById('login-form').classList.add('hidden');
      document.getElementById('mfa-challenge-screen').classList.remove('hidden');
      return;
    }

    completeLogin(data);
  } catch (err) {
    errorEl.textContent = 'Unable to reach the server. Please try again.';
    errorEl.classList.remove('hidden');
  } finally {
    submitBtn.disabled = false;
  }
}

// completeLogin stores the session and enters the app - the shared final
// step whether login was a single step (non-MFA role) or ended via MFA
// enrollment/verification.
function completeLogin(data) {
  localStorage.setItem('erp_token', data.token);
  localStorage.setItem('erp_username', data.user);
  localStorage.setItem('erp_role', data.role);
  pendingMFAToken = null;
  document.getElementById('login-form').reset();
  document.getElementById('mfa-enroll-form').reset();
  document.getElementById('mfa-challenge-form').reset();
  showApp();
  init();
}

// startMFAEnrollment fetches a fresh TOTP secret for a first-time MFA login
// and reveals the enrollment screen (manual-entry code + confirmation form).
async function startMFAEnrollment() {
  const errorEl = document.getElementById('login-error');
  try {
    const res = await fetch('/api/v1/auth/mfa/enroll', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${pendingMFAToken}` }
    });
    const data = await res.json();
    if (!res.ok) {
      errorEl.textContent = data.error || 'Failed to start MFA enrollment. Please try logging in again.';
      errorEl.classList.remove('hidden');
      pendingMFAToken = null;
      return;
    }
    document.getElementById('mfa-enroll-secret').textContent = data.secret;
    document.getElementById('login-form').classList.add('hidden');
    document.getElementById('mfa-enroll-screen').classList.remove('hidden');
  } catch (err) {
    errorEl.textContent = 'Unable to reach the server. Please try again.';
    errorEl.classList.remove('hidden');
  }
}

async function submitMFACode(url, codeInputId, errorElId, submitBtnId) {
  const code = document.getElementById(codeInputId).value.trim();
  const errorEl = document.getElementById(errorElId);
  const submitBtn = document.getElementById(submitBtnId);
  errorEl.classList.add('hidden');
  submitBtn.disabled = true;
  try {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${pendingMFAToken}` },
      body: JSON.stringify({ code })
    });
    const data = await res.json();
    if (!res.ok) {
      errorEl.textContent = data.error || 'Invalid code. Please try again.';
      errorEl.classList.remove('hidden');
      return;
    }
    completeLogin(data);
  } catch (err) {
    errorEl.textContent = 'Unable to reach the server. Please try again.';
    errorEl.classList.remove('hidden');
  } finally {
    submitBtn.disabled = false;
  }
}

async function handleMFAEnrollSubmit(event) {
  event.preventDefault();
  await submitMFACode('/api/v1/auth/mfa/activate', 'mfa-enroll-code', 'mfa-enroll-error', 'mfa-enroll-submit-btn');
}

async function handleMFAChallengeSubmit(event) {
  event.preventDefault();
  await submitMFACode('/api/v1/auth/mfa/verify', 'mfa-challenge-code', 'mfa-challenge-error', 'mfa-challenge-submit-btn');
}

function bootstrap() {
  document.getElementById('login-form').addEventListener('submit', handleLoginSubmit);
  document.getElementById('mfa-enroll-form').addEventListener('submit', handleMFAEnrollSubmit);
  document.getElementById('mfa-challenge-form').addEventListener('submit', handleMFAChallengeSubmit);

  if (localStorage.getItem('erp_token')) {
    showApp();
    init();
  } else {
    showLoginScreen();
  }
}

// Initializer
async function init() {
  setupEventListeners();
  setupModuleFlyouts();
  setupOfflineSync();
  await fetchLabels();
  await fetchRegisteredDoctypes();
  await fetchAndApplyPermissions();
  await restoreLastView();
  fetchAndApplyProfile();
}

async function fetchLabels() {
  try {
    const res = await apiFetch('/api/v1/labels');
    if (!res) return;
    if (res.ok) {
      state.labels = await res.json();
    } else {
      await showApiError(res, 'Failed to load label overlays.');
    }
  } catch (err) {
    console.error('Error fetching labels:', err);
  }
}

// 22.6: which doctype(s) gate each sidebar item's visibility, derived from
// what the backend actually enforces today (grepped every handler's own
// role check, not guessed) - not a hand-maintained per-role allowlist. An
// item is visible if the caller's role has allow_read on ANY listed
// doctype (`doctypes`), is HR/Admin (`adminOnly` - matches requireHRAdmin,
// a literal role check with no role_permissions row to key off of, e.g.
// Users/Roles/DocType Builder), or is explicitly `open` (the item's own
// backing handler has no role_permissions gate at all server-side - e.g.
// Reports, Finance/GL, POS billing, Approvals' per-transaction slab+role
// routing, Fulfillment/Marketplace, Fixed Assets' bespoke
// /api/v1/assets/register endpoint - showing these to every authenticated
// role matches current server behavior exactly, not a new restriction).
// Any menu id not listed here (Dashboard) defaults open the same way.
const MENU_PERMISSION_MAP = {
  'menu-pos': { open: true },
  'menu-pos-profiles': { doctypes: ['POSProfile'] },
  'menu-pos-offline-sync': { doctypes: ['POSOfflineSyncVariance'] },

  'menu-finance': { open: true },
  'menu-approvals': { open: true },
  'menu-vendor-invoices': { doctypes: ['VendorInvoice'] },
  'menu-payment-proposals': { doctypes: ['PaymentProposal', 'VendorInvoice'] },
  'menu-bank-reconciliation': { doctypes: ['BankAccount', 'BankStatementLine'] },
  'menu-finance-notes': { doctypes: ['DebitNote', 'CreditNote'] },
  'menu-sales-invoices': { doctypes: ['SalesInvoice'] },

  'menu-fulfillment': { open: true },
  'menu-marketplace': { open: true },

  'menu-reports': { open: true },

  'menu-purchase-orders': { doctypes: ['PurchaseOrder'] },
  'menu-vendors': { doctypes: ['Vendor'] },
  'menu-rfq': { doctypes: ['RFQ'] },

  'menu-inventory': { open: true },
  'menu-transfers': { doctypes: ['TransferOrder'] },
  'menu-bins': { doctypes: ['Bin'] },
  'menu-stores': { doctypes: ['Stores'] },
  'menu-stickers': { open: true },

  'menu-hr': { doctypes: ['Employee'] },
  'menu-assets': { open: true },
  'menu-expenses': { doctypes: ['ExpenseClaim'] },

  'menu-manufacturing': { doctypes: ['BOM', 'ProductionOrder'] },
  'menu-pim': { open: true },

  'menu-users': { adminOnly: true },
  'menu-roles': { adminOnly: true },
  'menu-prefix-configs': { adminOnly: true },
  'menu-dynamic-labels': { adminOnly: true },
  'menu-doctype-builder': { adminOnly: true },
  'menu-extension-hooks': { adminOnly: true },
  'menu-audit-logs': { adminOnly: true }
};

function canReadDoctype(doctype) {
  return state.permissions.isAdmin || state.permissions.doctypes.has(doctype);
}

function isMenuRuleVisible(rule) {
  if (!rule || rule.open) return true;
  if (rule.adminOnly) return state.permissions.isAdmin;
  if (rule.doctypes) return state.permissions.isAdmin || rule.doctypes.some(canReadDoctype);
  return true;
}

// applySidebarPermissions hides (rather than removes) menu items the
// current role has no read access to, then hides a whole flyout module
// once every one of its own flyout children is hidden - an empty arrow
// with nothing behind it is worse than no entry at all. Re-run whenever
// permissions or the dynamic Setup submenu (renderSidebarSubmenu) change.
function applySidebarPermissions() {
  Object.keys(MENU_PERMISSION_MAP).forEach(id => {
    const el = document.getElementById(id);
    if (!el) return;
    // closest('li') (not '.menu-item-container') so a flyout child's own
    // <li> is what gets hidden, not the whole module's outer
    // '.menu-item-container.has-flyout' <li> it's nested inside.
    const item = el.closest('li');
    if (!item) return;
    item.classList.toggle('perm-hidden', !isMenuRuleVisible(MENU_PERMISSION_MAP[id]));
  });

  // Hide a flyout module once it has no visible children left - including
  // Setup, whose submenu is built dynamically (renderSidebarSubmenu) and
  // may legitimately have zero <li> children for a role with no Master
  // doctype read access at all; [].every(...) is vacuously true, which is
  // exactly "hide" for that empty case too.
  document.querySelectorAll('.has-flyout').forEach(container => {
    const items = container.querySelectorAll('.menu-flyout > li');
    const allHidden = Array.from(items).every(li => li.classList.contains('perm-hidden'));
    container.classList.toggle('perm-hidden', allHidden);
  });
}

async function fetchAndApplyPermissions() {
  try {
    const res = await apiFetch('/api/v1/me/permissions');
    if (res && res.ok) {
      const data = await res.json();
      state.permissions = { isAdmin: !!data.is_admin, doctypes: new Set(data.doctypes || []), loaded: true };
    }
  } catch (err) {
    console.error('Error fetching permissions:', err);
  }
  // renderSidebarSubmenu() re-filters the dynamic Setup submenu by the
  // permissions just loaded, and itself calls applySidebarPermissions() at
  // the end - covers both the static menu items and the dynamic ones in
  // one pass.
  renderSidebarSubmenu();
}

async function fetchRegisteredDoctypes() {
  try {
    const res = await apiFetch('/api/v1/meta/doctypes');
    if (!res) return;
    if (res.ok) {
      state.activeDoctypes = await res.json();
      renderSidebarSubmenu();
    } else {
      await showApiError(res, 'Failed to load registered record types.');
    }
  } catch (err) {
    console.error('Error fetching doctypes:', err);
  }
}

function renderSidebarSubmenu() {
  const sub = document.getElementById('submenu-master');
  if (!sub) return;
  sub.innerHTML = '';
  
  state.activeDoctypes.forEach(d => {
    if (d.document_type === 'Master' && canReadDoctype(d.name)) {
      const li = document.createElement('li');
      li.innerHTML = `<a class="submenu-item" data-view="${d.name}">${getTranslatedLabel(d.name)}</a>`;
      sub.appendChild(li);
    }
  });
  // Setup's own flyout trigger has no read access left once every Master
  // doctype it lists is filtered out - re-evaluate the flyout-hiding pass
  // now that the list this depends on just changed.
  applySidebarPermissions();

  // Rebind event listeners to submenu items
  sub.querySelectorAll('.submenu-item').forEach(item => {
    item.addEventListener('click', (e) => {
      e.preventDefault();
      document.querySelectorAll('.submenu-item').forEach(i => i.classList.remove('active'));
      document.querySelectorAll('.menu-item').forEach(i => i.classList.remove('active'));
      
      document.getElementById('menu-master-definition').classList.add('active');
      item.classList.add('active');
      
      const doctype = item.getAttribute('data-view');
      currentDoctype = doctype;
      currentSearchQuery = '';
      currentTablePage = 1;
      renderView('doctype-table');
    });
  });
}

function setupEventListeners() {
  // Main Navigation links
  document.getElementById('menu-dashboard').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-dashboard');
    closeSubmenus();
    renderView('dashboard');
  });

  document.getElementById('menu-doctype-builder').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-doctype-builder');
    closeSubmenus();
    renderView('doctype-builder');
  });

  document.getElementById('menu-pos').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-pos');
    closeSubmenus();
    renderView('pos');
  });

  document.getElementById('menu-finance').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-finance');
    closeSubmenus();
    renderView('finance');
  });

  document.getElementById('menu-fulfillment').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-fulfillment');
    closeSubmenus();
    renderView('fulfillment');
  });

  document.getElementById('menu-marketplace').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-marketplace');
    closeSubmenus();
    renderView('marketplace');
  });

  document.getElementById('menu-approvals').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-approvals');
    closeSubmenus();
    renderView('approvals');
  });

  document.getElementById('menu-vendor-invoices').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-vendor-invoices');
    closeSubmenus();
    renderView('vendor-invoices');
  });

  document.getElementById('menu-payment-proposals').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-payment-proposals');
    closeSubmenus();
    renderView('payment-proposals');
  });

  document.getElementById('menu-bank-reconciliation').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-bank-reconciliation');
    closeSubmenus();
    renderView('bank-reconciliation');
  });

  document.getElementById('menu-finance-notes').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-finance-notes');
    closeSubmenus();
    renderView('finance-notes');
  });

  document.getElementById('menu-sales-invoices').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-sales-invoices');
    closeSubmenus();
    renderView('sales-invoices');
  });

  document.getElementById('menu-reports').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-reports');
    closeSubmenus();
    renderView('reports');
  });

  document.getElementById('menu-rfq').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-rfq');
    closeSubmenus();
    renderView('rfq');
  });

  document.getElementById('menu-stickers').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-stickers');
    closeSubmenus();
    renderView('stickers');
  });

  document.getElementById('menu-hr').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-hr');
    closeSubmenus();
    renderView('hr');
  });

  document.getElementById('menu-assets').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-assets');
    closeSubmenus();
    renderView('assets');
  });

  document.getElementById('menu-expenses').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-expenses');
    closeSubmenus();
    renderView('expenses');
  });

  document.getElementById('menu-manufacturing').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-manufacturing');
    closeSubmenus();
    renderView('manufacturing');
  });

  document.getElementById('menu-pim').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-pim');
    closeSubmenus();
    currentPIMTab = 'workbench';
    currentPIMSelectedItem = '';
    renderView('pim');
  });

  document.getElementById('menu-purchase-orders').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-purchase-orders');
    closeSubmenus();
    renderView('purchase-orders');
  });

  // "Vendors" is a real doctype now (Stage 13.9) - point it at the same
  // generic doctype-table view the Master Definition submenu already uses,
  // rather than a bespoke screen.
  document.getElementById('menu-vendors').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-vendors');
    closeSubmenus();
    currentDoctype = 'Vendor';
    currentSearchQuery = '';
    currentTablePage = 1;
    renderView('doctype-table');
  });

  document.getElementById('menu-stores').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-stores'); closeSubmenus(); currentDoctype = 'Stores'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // POS Profile (Stage 20.6) - same generic doctype-table pattern as Vendors/Stores above.
  document.getElementById('menu-pos-profiles').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-profiles'); closeSubmenus(); currentDoctype = 'POSProfile'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Bin (Stage 20.16) - same generic doctype-table pattern as POS Profile/Vendors/Stores above.
  document.getElementById('menu-bins').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-bins'); closeSubmenus(); currentDoctype = 'Bin'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Offline Sync Review (Stage 20.13) - same generic doctype-table pattern as POS Profile/Bin above.
  document.getElementById('menu-pos-offline-sync').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-offline-sync'); closeSubmenus(); currentDoctype = 'POSOfflineSyncVariance'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  ['menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-dynamic-labels', 'menu-extension-hooks', 'menu-audit-logs'].forEach(id => {
    const btn = document.getElementById(id);
    if (btn) {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        setActiveMenu(id);
        closeSubmenus();
        const viewName = id.replace('menu-', '');
        renderView(viewName);
      });
    }
  });

  const globalSearch = document.getElementById('global-search');
  globalSearch.addEventListener('input', (e) => {
    currentSearchQuery = e.target.value.toLowerCase();
    currentTablePage = 1;
    if (currentView === 'doctype-table') {
      renderDocTable();
      saveNavState();
    }
  });

  // Sync / Reset Database
  document.getElementById('sync-btn').addEventListener('click', async () => {
    if (await showCustomConfirm('Re-fetch translation cache and active schema fields?')) {
      await fetchLabels();
      await fetchRegisteredDoctypes();
      renderView(currentView);
    }
  });

  const indSelector = document.getElementById('industry-selector');
  if (indSelector) {
    indSelector.addEventListener('change', async (e) => {
      const code = e.target.value;
      if (!code) return;
      if (await showCustomConfirm(`Switch to active industry profile: ${code}? This will re-load preset table field configurations.`)) {
        const res = await apiFetch('/api/v1/admin/industry', {
          method: 'POST',
          body: JSON.stringify({ industry_code: code })
        });
        if (res && res.ok) {
          localStorage.setItem('erp_industry_code', code);
          await showCustomAlert('Industry configuration updated successfully!', 'Success');
          await fetchLabels();
          await fetchRegisteredDoctypes();
          renderView('dashboard');
        } else if (res) {
          await showApiError(res, 'Failed to switch industry profile.');
        }
      }
    });
  }

  setupAccountMenu();
}

// Account menu: a single clickable avatar/name trigger in the sidebar
// footer that opens a small popover (My Profile / Sign Out), replacing the
// old bare logout icon button. Closes on an outside click, Escape, or after
// either action so it never lingers open behind a navigated-away view.
function setupAccountMenu() {
  const menu = document.getElementById('account-menu');
  const trigger = document.getElementById('account-menu-trigger');
  const popover = document.getElementById('account-popover');
  if (!menu || !trigger || !popover) return;

  const closeAccountMenu = () => {
    menu.classList.remove('open');
    popover.classList.add('hidden');
    trigger.setAttribute('aria-expanded', 'false');
  };
  const toggleAccountMenu = () => {
    const opening = popover.classList.contains('hidden');
    if (opening) {
      menu.classList.add('open');
      popover.classList.remove('hidden');
      trigger.setAttribute('aria-expanded', 'true');
    } else {
      closeAccountMenu();
    }
  };

  trigger.addEventListener('click', (e) => {
    e.stopPropagation();
    toggleAccountMenu();
  });
  document.addEventListener('click', (e) => {
    if (!menu.contains(e.target)) closeAccountMenu();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAccountMenu();
  });

  document.getElementById('account-menu-profile-btn').addEventListener('click', () => {
    closeAccountMenu();
    setActiveMenu(null);
    closeSubmenus();
    renderView('profile');
  });

  document.getElementById('logout-btn').addEventListener('click', async () => {
    closeAccountMenu();
    if (await showCustomConfirm('Are you sure you want to log out?')) {
      logout();
    }
  });
}

function setActiveMenu(menuId) {
  document.querySelectorAll('.menu-item').forEach(item => item.classList.remove('active'));
  document.querySelectorAll('.submenu-item').forEach(item => item.classList.remove('active'));
  const activeMenu = document.getElementById(menuId);
  if (!activeMenu) return;
  activeMenu.classList.add('active');
  // Nav redesign: a screen inside a module's flyout also marks that
  // module's own trigger active, so the sidebar still shows which module
  // you're in once the flyout itself closes (mouse moves away).
  const flyoutParent = activeMenu.closest('.has-flyout');
  if (flyoutParent) {
    const groupTrigger = flyoutParent.querySelector('.menu-item-group');
    if (groupTrigger && groupTrigger !== activeMenu) groupTrigger.classList.add('active');
  }
}

// Positions a module's flyout beside its trigger (JS-computed, not CSS
// position:absolute, so it's never clipped by .sidebar-menu's own
// overflow-y:auto) and shows it.
function openFlyout(container) {
  const trigger = container.querySelector('.menu-item-group');
  const flyout = container.querySelector('.menu-flyout');
  if (!trigger || !flyout) return;
  const rect = trigger.getBoundingClientRect();
  const margin = 12;
  // A long flyout (e.g. Master Definition's ~25 master doctypes) anchored
  // near the bottom of the sidebar would otherwise run off the bottom of
  // the viewport - cap its height to the space actually available below
  // the trigger (it scrolls internally via its own overflow-y) rather than
  // a flat viewport-height max-height that ignores where the trigger sits.
  const availableBelow = window.innerHeight - rect.top - margin;
  flyout.style.top = `${Math.round(rect.top)}px`;
  flyout.style.left = `${Math.round(rect.right + 8)}px`;
  flyout.style.maxHeight = `${Math.max(120, Math.round(availableBelow))}px`;
  flyout.classList.add('open');
  container.classList.add('flyout-open');
}

function closeSubmenus() {
  document.querySelectorAll('.menu-flyout.open').forEach(f => f.classList.remove('open'));
  document.querySelectorAll('.has-flyout.flyout-open').forEach(c => c.classList.remove('flyout-open'));
}

// Module-grouped sidebar (Stage 20 nav redesign): the left sidebar shows
// only module-level entries; hovering (or clicking, for keyboard/touch
// users) reveals the module's actual screens in a flyout beside it.
// Idempotent per-container (marked via data-flyout-bound) and safe to call
// again after a view (e.g. Database Schema Design) injects its own new
// `.has-flyout` markup - re-running only binds the newly-added containers,
// it never double-binds the sidebar's own.
let moduleFlyoutDocListenersBound = false;
function setupModuleFlyouts() {
  document.querySelectorAll('.has-flyout').forEach(container => {
    if (container.dataset.flyoutBound) return;
    const trigger = container.querySelector('.menu-item-group');
    const flyout = container.querySelector('.menu-flyout');
    if (!trigger || !flyout) return;
    container.dataset.flyoutBound = '1';
    let hideTimer = null;
    const show = () => { clearTimeout(hideTimer); openFlyout(container); };
    const scheduleHide = () => { hideTimer = setTimeout(closeSubmenus, 200); };

    container.addEventListener('mouseenter', show);
    container.addEventListener('mouseleave', scheduleHide);
    flyout.addEventListener('mouseenter', () => clearTimeout(hideTimer));
    flyout.addEventListener('mouseleave', scheduleHide);

    trigger.addEventListener('click', (e) => {
      e.preventDefault();
      const wasOpen = flyout.classList.contains('open');
      closeSubmenus();
      if (!wasOpen) show();
    });
  });

  if (moduleFlyoutDocListenersBound) return;
  moduleFlyoutDocListenersBound = true;
  document.addEventListener('click', (e) => {
    if (!e.target.closest('.has-flyout')) closeSubmenus();
  });
  window.addEventListener('resize', closeSubmenus);
  const sidebarMenu = document.querySelector('.sidebar-menu');
  if (sidebarMenu) sidebarMenu.addEventListener('scroll', closeSubmenus);
}

// Maps a static view name to the sidebar menu item that represents it, for
// restoring the correct highlighted item after a refresh. doctype-table is
// handled separately below since it points at a submenu item, not a top-level one.
const STATIC_VIEW_MENU_IDS = {
  dashboard: 'menu-dashboard',
  pos: 'menu-pos',
  finance: 'menu-finance',
  fulfillment: 'menu-fulfillment',
  marketplace: 'menu-marketplace',
  approvals: 'menu-approvals',
  reports: 'menu-reports',
  rfq: 'menu-rfq',
  stickers: 'menu-stickers',
  hr: 'menu-hr',
  assets: 'menu-assets',
  expenses: 'menu-expenses',
  manufacturing: 'menu-manufacturing',
  pim: 'menu-pim',
  'doctype-builder': 'menu-doctype-builder',
  vendors: 'menu-vendors',
  stores: 'menu-stores',
  'purchase-orders': 'menu-purchase-orders',
  inventory: 'menu-inventory',
  transfers: 'menu-transfers',
  users: 'menu-users',
  roles: 'menu-roles',
  'prefix-configs': 'menu-prefix-configs',
  'dynamic-labels': 'menu-dynamic-labels',
  'extension-hooks': 'menu-extension-hooks',
  'extension-hook-log': 'menu-extension-hooks',
  'audit-logs': 'menu-audit-logs',
  'vendor-invoices': 'menu-vendor-invoices',
  'payment-proposals': 'menu-payment-proposals',
  'bank-reconciliation': 'menu-bank-reconciliation',
  'finance-notes': 'menu-finance-notes',
  'sales-invoices': 'menu-sales-invoices'
};

// Only called once, from restoreLastView() below, when the app first loads
// (or after a browser refresh) and needs to re-highlight whatever the user
// was last on. Deliberately NOT called from ordinary sidebar clicks - the
// user just clicked that item themselves and can already see it, so forcing
// a scroll there would just be unwanted extra motion on every click.
// {block: 'center'} rather than 'nearest' so a below-the-fold item lands
// comfortably mid-list instead of snapped flush against the bottom edge.
function scrollActiveMenuIntoView() {
  const active = document.querySelector('.sidebar-menu .menu-item.active, .sidebar-menu .submenu-item.active');
  if (active) active.scrollIntoView({ block: 'center' });
}

function restoreActiveMenuState(view, doctype) {
  closeSubmenus();
  if (view === 'doctype-table' && doctype) {
    const submenu = document.getElementById('submenu-master');
    const item = submenu ? submenu.querySelector(`.submenu-item[data-view="${doctype}"]`) : null;
    if (item) {
      document.querySelectorAll('.menu-item').forEach(i => i.classList.remove('active'));
      document.querySelectorAll('.submenu-item').forEach(i => i.classList.remove('active'));
      document.getElementById('menu-master-definition').classList.add('active');
      item.classList.add('active');
      // Flyout itself stays closed on restore (it's a hover overlay now,
      // not an inline-expand section) - only the highlight is restored.
      scrollActiveMenuIntoView();
      return;
    }
  }
  // Also runs (and correctly clears any stale highlight) for a view with no
  // sidebar entry of its own, e.g. the Profile screen - setActiveMenu(undefined)
  // still clears every .active class even though it won't find an element to add one to.
  setActiveMenu(STATIC_VIEW_MENU_IDS[view]);
  scrollActiveMenuIntoView();
}

// Restores whatever view/doctype/search/page the user was last on instead of
// always bouncing back to Dashboard after a refresh. Falls back to Dashboard
// if the saved doctype no longer exists (e.g. it was deleted elsewhere).
async function restoreLastView() {
  const saved = loadNavState();
  let view = 'dashboard';
  let doctype = '';
  let searchQuery = '';
  let page = 1;

  if (saved && saved.view) {
    if (saved.view === 'doctype-table') {
      if (state.activeDoctypes.some(d => d.name === saved.doctype)) {
        view = 'doctype-table';
        doctype = saved.doctype;
        searchQuery = saved.searchQuery || '';
        page = saved.page || 1;
      }
    } else {
      view = saved.view;
    }
  }

  currentDoctype = doctype;
  currentSearchQuery = searchQuery;
  currentTablePage = page;
  restoreActiveMenuState(view, doctype);
  await renderView(view);

  const searchBox = document.getElementById('global-search');
  if (searchBox) searchBox.value = view === 'doctype-table' ? searchQuery : '';
}

// Router
async function renderView(view) {
  currentView = view;
  saveNavState();
  const root = document.getElementById('view-root');
  root.innerHTML = '';

  if (view === 'dashboard') {
    renderDashboard(root);
  } else if (view === 'pos') {
    renderPOSView(root);
  } else if (view === 'finance') {
    await renderFinanceView(root);
  } else if (view === 'fulfillment') {
    await renderFulfillmentView(root);
  } else if (view === 'marketplace') {
    await renderMarketplaceView(root);
  } else if (view === 'approvals') {
    await renderApprovalsView(root);
  } else if (view === 'reports') {
    await renderReportsView(root);
  } else if (view === 'rfq') {
    await renderRFQView(root);
  } else if (view === 'stickers') {
    await renderStickersView(root);
  } else if (view === 'hr') {
    await renderHRView(root);
  } else if (view === 'assets') {
    await renderAssetsView(root);
  } else if (view === 'expenses') {
    await renderExpensesView(root);
  } else if (view === 'manufacturing') {
    await renderManufacturingView(root);
  } else if (view === 'pim') {
    await renderPIMView(root);
  } else if (view === 'purchase-orders') {
    await renderPurchaseOrdersView(root);
  } else if (view === 'doctype-table') {
    await renderDocTableView(root);
  } else if (view === 'doctype-builder') {
    await renderDocTypeBuilderView(root);
  } else if (view === 'prefix-configs') {
    await renderPrefixConfigsView(root);
  } else if (view === 'dynamic-labels') {
    renderDynamicLabelsView(root);
  } else if (view === 'extension-hooks') {
    await renderExtensionHooksView(root);
  } else if (view === 'extension-hook-log') {
    await renderExtensionHookLogView(root);
  } else if (view === 'audit-logs') {
    await renderLogHubView(root);
  } else if (view === 'profile') {
    await renderProfileView(root);
  } else if (view === 'transfers') {
    await renderTransfersView(root);
  } else if (view === 'inventory') {
    await renderInventoryView(root);
  } else if (view === 'users') {
    await renderUsersView(root);
  } else if (view === 'roles') {
    await renderRolesView(root);
  } else if (view === 'vendor-invoices') {
    await renderVendorInvoicesView(root);
  } else if (view === 'payment-proposals') {
    await renderPaymentProposalsView(root);
  } else if (view === 'bank-reconciliation') {
    await renderBankReconciliationView(root);
  } else if (view === 'finance-notes') {
    await renderFinanceNotesView(root);
  } else if (view === 'sales-invoices') {
    await renderSalesInvoicesView(root);
  } else {
    renderMockModuleView(root, view);
  }
  setTimeout(translateDOM, 50);
}

// Translate labels in DOM dynamically
function translateDOM() {
  const elements = document.querySelectorAll('.page-title, .page-subtitle, .card-title, .card-desc, th, td, label, span, h1, h2, h3, a');
  elements.forEach(el => {
    if (el.children.length === 0 && el.textContent.trim() !== '') {
      const orig = el.textContent.trim();
      const trans = getTranslatedLabel(orig);
      if (trans !== orig) {
        el.textContent = trans;
      }
    }
  });
}

function getTranslatedLabel(text) {
  if (!text) return '';
  const clean = text.toLowerCase();
  for (const [orig, custom] of Object.entries(state.labels)) {
    if (orig.toLowerCase() === clean) {
      return custom;
    }
  }
  return text;
}

// My Profile (Stage 21): self-service account view - read-only account
// info (including a best-effort linked Employee lookup), change own
// password, and set the personal idle-timeout preference that drives
// setupIdleTimeout() (see logout()/resetIdleTimer() above). Reached from
// the sidebar account-menu popover, not a sidebar list item of its own.
const IDLE_TIMEOUT_OPTIONS = [
  { value: 0, label: 'Never' },
  { value: 15, label: '15 minutes' },
  { value: 30, label: '30 minutes' },
  { value: 60, label: '1 hour' },
  { value: 120, label: '2 hours' }
];

async function renderProfileView(container) {
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
      <p class="page-subtitle">View your account details, change your password, and manage session preferences.</p>
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
    <h3 class="card-title" style="margin-bottom: 16px;">Account Info</h3>
    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 20px;">
      <div><span class="stat-label">Username</span><div style="font-weight:600; margin-top:4px;">${data.username}</div></div>
      <div><span class="stat-label">Role</span><div style="font-weight:600; margin-top:4px;">${data.role}</div></div>
      <div><span class="stat-label">Status</span><div style="margin-top:4px;"><span class="badge ${data.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${data.status}</span></div></div>
      <div><span class="stat-label">Employee</span><div style="font-weight:600; margin-top:4px;">${employeeText}</div></div>
      <div><span class="stat-label">Two-Factor Authentication</span><div style="margin-top:4px;"><span class="badge ${data.mfa_enabled ? 'badge-success' : 'badge-secondary'}">${data.mfa_enabled ? 'Enabled' : 'Not enabled'}</span></div></div>
    </div>
  `;
  container.appendChild(infoPanel);

  const settingsPanel = document.createElement('div');
  settingsPanel.className = 'table-panel';
  settingsPanel.style.padding = '24px';
  settingsPanel.innerHTML = `
    <h3 class="card-title" style="margin-bottom: 16px;">Contact &amp; Session</h3>
    <form id="profile-settings-form">
      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px;">
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-email">Email</label>
          <input type="email" id="profile-email" class="form-input" value="${data.email || ''}">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-idle-timeout">Auto Logout (inactivity)</label>
          <select id="profile-idle-timeout" class="form-select">
            ${IDLE_TIMEOUT_OPTIONS.map(o => `<option value="${o.value}" ${o.value === data.idle_timeout_minutes ? 'selected' : ''}>${o.label}</option>`).join('')}
          </select>
        </div>
      </div>
      <button type="submit" class="btn btn-primary" style="margin-top: 16px;">Save Changes</button>
    </form>
  `;
  container.appendChild(settingsPanel);

  const passwordPanel = document.createElement('div');
  passwordPanel.className = 'table-panel';
  passwordPanel.style.padding = '24px';
  passwordPanel.innerHTML = `
    <h3 class="card-title" style="margin-bottom: 16px;">Change Password</h3>
    <form id="profile-password-form">
      <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px;">
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-current-password">Current Password</label>
          <input type="password" id="profile-current-password" class="form-input" autocomplete="current-password" required>
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-new-password">New Password</label>
          <input type="password" id="profile-new-password" class="form-input" autocomplete="new-password" minlength="8" required>
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-confirm-password">Confirm New Password</label>
          <input type="password" id="profile-confirm-password" class="form-input" autocomplete="new-password" minlength="8" required>
        </div>
      </div>
      <button type="submit" class="btn btn-primary" style="margin-top: 16px;">Update Password</button>
    </form>
  `;
  container.appendChild(passwordPanel);

  document.getElementById('profile-settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const email = document.getElementById('profile-email').value.trim();
    const idleTimeoutMinutesVal = parseInt(document.getElementById('profile-idle-timeout').value, 10);
    const saveRes = await apiFetch('/api/v1/me', {
      method: 'PUT',
      body: JSON.stringify({ email, idle_timeout_minutes: idleTimeoutMinutesVal })
    });
    if (!saveRes) return;
    if (!saveRes.ok) {
      await showApiError(saveRes, 'Failed to save changes.');
      return;
    }
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

// Users (Stage 21 QA fix): "Users" routed to a view name the router had no
// case for, always falling through to "Module Setup Pending" - despite
// ADMIN_GUIDE.md §B.2 explicitly documenting this as how new users are
// created. Nothing backed it: tenant_default.users is a raw SQL table, not
// a generic doctype, so /api/v1/doc/{doctype} could never have reached it -
// new internal/server/handlers_admin_identity.go endpoints back this view,
// all HR/Admin-only (enforced server-side, matching every other admin screen).
async function renderUsersView(container) {
  const [usersRes, rolesRes] = await Promise.all([
    apiFetch('/api/v1/admin/users'),
    apiFetch('/api/v1/admin/roles')
  ]);
  if (!usersRes || !rolesRes) return;
  if (!usersRes.ok) { renderErrorPanel(container, 'Failed to load users.', () => renderView('users')); return; }
  const users = await usersRes.json();
  const roles = rolesRes.ok ? await rolesRes.json() : [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Users</h1>
      <p class="page-subtitle">Create and manage user accounts. Roles determine what each user can see and do.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New User</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-username">Username</label>
        <input type="text" id="user-username" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-password">Password</label>
        <input type="password" id="user-password" class="form-input" style="width: 150px;" autocomplete="new-password" minlength="8">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-email">Email</label>
        <input type="email" id="user-email" class="form-input" style="width: 190px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-role">Role</label>
        <select id="user-role" class="form-select" style="width: 150px;">
          ${roles.map(r => `<option value="${r}">${r}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="user-location">Location Code</label>
        <input type="text" id="user-location" class="form-input" style="width: 130px;" placeholder="HO" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="user-create-btn">Create User</button>
    </div>
    <div id="user-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Username</th><th>Email</th><th>Role</th><th>Location</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += users.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No users found.</td></tr>`
    : users.map(u => `
        <tr>
          <td style="font-weight:600;">${u.username}</td>
          <td>${u.email || ''}</td>
          <td>${u.role}</td>
          <td>${u.location_code || 'HO'}</td>
          <td><span class="badge ${u.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${u.status}</span></td>
          <td>
            <button class="action-btn" onclick="setUserLocation('${u.id}', '${u.location_code || 'HO'}')">Set Location</button>
            ${u.status === 'Active'
              ? `<button class="action-btn action-btn-danger" onclick="setUserStatus('${u.id}', 'Inactive')">Deactivate</button>`
              : `<button class="action-btn" onclick="setUserStatus('${u.id}', 'Active')">Reactivate</button>`}
          </td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('user-create-btn').addEventListener('click', createUser);
}

async function createUser() {
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
  renderView('users');
}

window.setUserStatus = async function(id, status) {
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
};

// 24.1: real per-user location, used for location-scoped authorization
// (handleGenericDoc) - previously every user's token silently claimed "HO".
window.setUserLocation = async function(id, currentLocation) {
  const location_code = await showCustomPrompt('New location code for this user:', currentLocation, 'Set Location');
  if (location_code === null || location_code.trim() === '') return;
  const res = await apiFetch('/api/v1/admin/users/location', {
    method: 'POST',
    body: JSON.stringify({ id, location_code: location_code.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update user location.');
    return;
  }
  renderView('users');
};

// Roles (Stage 21 QA fix): "Roles" had the exact same dead-mock-screen bug
// as Users above. Shows every currently-granted (role, doctype) permission
// row and lets an HR/Admin edit or add one - directly usable to close gaps
// like the one Stage 18 flagged and deliberately left unfixed ("Store
// Manager/Cashier lack read access to Vendor/Item, so the new typeahead
// pickers are code-correct but not usable by their intended roles").
async function renderRolesView(container) {
  const [permsRes, rolesRes] = await Promise.all([
    apiFetch('/api/v1/admin/role-permissions'),
    apiFetch('/api/v1/admin/roles')
  ]);
  if (!permsRes || !rolesRes) return;
  if (!permsRes.ok) { renderErrorPanel(container, 'Failed to load role permissions.', () => renderView('roles')); return; }
  const grants = await permsRes.json();
  const roles = rolesRes.ok ? await rolesRes.json() : [];
  const doctypeOptions = state.activeDoctypes.map(d => d.name).sort();

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Roles</h1>
      <p class="page-subtitle">What each role can see and do, per record type. HR/Admin can always do everything; this only governs the other roles.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Add or Update a Grant</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grant-role">Role</label>
        <select id="grant-role" class="form-select" style="width: 150px;">
          ${roles.map(r => `<option value="${r}">${r}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grant-doctype">Record Type</label>
        <select id="grant-doctype" class="form-select" style="width: 190px;">
          ${doctypeOptions.map(d => `<option value="${d}">${d}</option>`).join('')}
        </select>
      </div>
      <label style="display:flex; align-items:center; gap:6px; font-size:13.5px;"><input type="checkbox" id="grant-read" checked> Read</label>
      <label style="display:flex; align-items:center; gap:6px; font-size:13.5px;"><input type="checkbox" id="grant-create"> Create</label>
      <label style="display:flex; align-items:center; gap:6px; font-size:13.5px;"><input type="checkbox" id="grant-update"> Update</label>
      <label style="display:flex; align-items:center; gap:6px; font-size:13.5px;"><input type="checkbox" id="grant-delete"> Delete</label>
      <button class="btn btn-primary" id="grant-save-btn">Save Grant</button>
    </div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Role</th><th>Record Type</th><th>Read</th><th>Create</th><th>Update</th><th>Delete</th></tr></thead>
      <tbody>
  `;
  html += grants.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No grants configured yet.</td></tr>`
    : grants.map(g => `
        <tr>
          <td style="font-weight:600;">${g.role}</td>
          <td>${g.doctype_name}</td>
          <td>${g.allow_read ? '&#10003;' : '&mdash;'}</td>
          <td>${g.allow_create ? '&#10003;' : '&mdash;'}</td>
          <td>${g.allow_update ? '&#10003;' : '&mdash;'}</td>
          <td>${g.allow_delete ? '&#10003;' : '&mdash;'}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('grant-save-btn').addEventListener('click', saveRoleGrant);
}

async function saveRoleGrant() {
  const role = document.getElementById('grant-role').value;
  const doctypeName = document.getElementById('grant-doctype').value;
  const res = await apiFetch('/api/v1/admin/role-permissions', {
    method: 'POST',
    body: JSON.stringify({
      role, doctype_name: doctypeName,
      allow_read: document.getElementById('grant-read').checked,
      allow_create: document.getElementById('grant-create').checked,
      allow_update: document.getElementById('grant-update').checked,
      allow_delete: document.getElementById('grant-delete').checked
    })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to save grant.');
    return;
  }
  renderView('roles');
}

// Dashboard Page
function renderDashboard(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Dashboard</h1>
      <p class="page-subtitle">Welcome to Custom ERP. Choose a module to get started.</p>
    </div>
  `;
  container.appendChild(header);

  // Quick Stats Summary Row
  const statsRow = document.createElement('div');
  statsRow.className = 'dashboard-stats-row';
  statsRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Record Types Registered</span>
      <span class="stat-val">${state.activeDoctypes.length || 0}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Audit History Count</span>
      <span class="stat-val">${state.auditLogs.length || 0}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Active Schema Tenant</span>
      <span class="stat-val" style="text-transform: uppercase;">${localStorage.getItem('erp_tenant_id') || 'default'}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Platform Core Health</span>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
        <span class="pulse-dot"></span>
        <span style="font-size: 16px; font-weight: 700; color: #10b981;">Operational</span>
      </div>
    </div>
  `;
  container.appendChild(statsRow);

  const grid = document.createElement('div');
  grid.className = 'dashboard-grid';

  const modules = [
    { title: 'Database Schema Design', desc: 'Build schemas and customize properties', action: () => { setActiveMenu('menu-doctype-builder'); renderView('doctype-builder'); } },
    { title: 'Dynamic Labels', desc: 'Configure customized nomenclature', action: () => { setActiveMenu('menu-dynamic-labels'); renderView('dynamic-labels'); } },
    { title: 'Prefix Configs', desc: 'Configure sequential transaction prefixes', action: () => { setActiveMenu('menu-prefix-configs'); renderView('prefix-configs'); } },
    { title: 'Extension Hooks', desc: 'Manage 3rd-party webhook hooks and scoped tokens', action: () => { setActiveMenu('menu-extension-hooks'); renderView('extension-hooks'); } },
    { title: 'Activity Log', desc: 'Track audits, panics, and payloads', action: () => { setActiveMenu('menu-audit-logs'); renderView('audit-logs'); } }
  ];

  modules.forEach(m => {
    const card = document.createElement('div');
    card.className = 'dashboard-card';
    card.innerHTML = `
      <div class="card-content">
        <h3 class="card-title">${m.title}</h3>
        <p class="card-desc">${m.desc}</p>
      </div>
    `;
    card.addEventListener('click', m.action);
    grid.appendChild(card);
  });

  container.appendChild(grid);
}

// POS / Billing screen - cashier/barcode-scan-to-sell UI against the
// already-working checkout/availability APIs (Stage 13.4). Kept independent
// of the generic DocType table view since a checkout cart isn't a plain
// CRUD record: it's built up client-side line by line before a single
// POST /api/v1/checkout submits the whole thing atomically.
function renderPOSView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">POS / Billing</h1>
      <p class="page-subtitle">Scan or enter a SKU to add it to the cart, then complete the sale.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div id="pos-session-bar" style="display: flex; gap: 12px; align-items: center; margin-bottom: 16px; padding: 10px 12px; border: 1px solid var(--border-color); border-radius: 6px;">
      <span id="pos-session-status" style="font-size: 13px; color: var(--text-muted);">Checking session&hellip;</span>
      <span id="pos-offline-queue-badge" class="badge badge-secondary hidden" style="cursor: pointer;" title="Click to try syncing now" onclick="trySyncOfflineQueue()"></span>
      <button class="btn btn-outline" id="pos-session-open-btn" type="button" style="margin-left: auto;">Open Session</button>
      <button class="btn btn-outline hidden" id="pos-session-close-btn" type="button">Close Session</button>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end;">
      <div class="form-group" style="max-width: 280px; margin-bottom: 0;">
        <label class="form-label" for="pos-location">Location Code</label>
        <input type="text" id="pos-location" class="form-input" placeholder="e.g. HO" value="${posLocation}">
      </div>
      <div class="form-group" style="max-width: 220px; margin-bottom: 0;">
        <label class="form-label" for="pos-customer">Customer Code (optional)</label>
        <input type="text" id="pos-customer" class="form-input" placeholder="For loyalty points">
      </div>
      <button class="btn btn-outline" id="pos-loyalty-check-btn" type="button">Check Points</button>
      <button class="btn btn-outline" id="pos-loyalty-redeem-btn" type="button">Redeem Points</button>
    </div>
    <div id="pos-loyalty-info" style="margin: 8px 0 16px; font-size: 13px; color: var(--text-muted);"></div>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 20px;">
      <div class="form-group" style="flex: 1; margin-bottom: 0;">
        <label class="form-label" for="pos-sku-input">Scan or Enter SKU</label>
        <input type="text" id="pos-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="pos-add-btn">Add to Cart</button>
    </div>
    <div id="pos-scan-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>SKU</th>
          <th>Available</th>
          <th>Qty</th>
          <th>Sale Price</th>
          <th>Cost Price</th>
          <th>Line Total</th>
          <th></th>
        </tr>
      </thead>
      <tbody id="pos-cart-body"></tbody>
    </table>
    <div style="display: flex; justify-content: flex-end; align-items: center; gap: 24px; margin-top: 20px; padding-top: 20px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="max-width: 120px; margin-bottom: 0;">
        <label class="form-label" for="pos-discount-pct">Discount %</label>
        <input type="number" min="0" max="100" step="0.1" value="0" id="pos-discount-pct" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pos-payment-mode">Payment Mode</label>
        <select id="pos-payment-mode" class="form-input">
          <option value="Cash">Cash</option>
          <option value="Card">Card</option>
          <option value="UPI">UPI</option>
        </select>
      </div>
      <div style="font-size: 20px; font-weight: 700;">Total: <span id="pos-cart-total">0.00</span></div>
      <button class="btn btn-primary" id="pos-checkout-btn">Complete Sale</button>
    </div>
  `;
  container.appendChild(panel);

  attachTypeahead(document.getElementById('pos-location'), 'Location');
  attachTypeahead(document.getElementById('pos-customer'), 'Customer');
  attachTypeahead(document.getElementById('pos-sku-input'), 'Item');

  document.getElementById('pos-location').addEventListener('change', (e) => {
    posLocation = e.target.value.trim();
    refreshPOSSessionStatus();
  });
  document.getElementById('pos-add-btn').addEventListener('click', addSKUToPOSCart);
  document.getElementById('pos-sku-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addSKUToPOSCart();
    }
  });
  document.getElementById('pos-checkout-btn').addEventListener('click', submitPOSCheckout);
  document.getElementById('pos-loyalty-check-btn').addEventListener('click', checkPOSLoyaltyBalance);
  document.getElementById('pos-loyalty-redeem-btn').addEventListener('click', redeemPOSLoyaltyPoints);
  document.getElementById('pos-session-open-btn').addEventListener('click', openPOSSessionFlow);
  document.getElementById('pos-session-close-btn').addEventListener('click', closePOSSessionFlow);

  renderPOSCartTable();
  refreshPOSSessionStatus();
  renderPOSReturnPanel(container);
  renderOfflineQueueBadge();
  trySyncOfflineQueue();
}

// Stage 20.11: audited first per the checklist item's own instruction -
// engines.ProcessReturnAnywhere + POST /api/v1/fulfillment/return (Stage
// 7.2/13.6) already do the real work; there was just no screen calling it.
// This is a thin, separate panel (own posReturnCart array) rather than a
// mode-toggle on the sale cart above, so a return in progress can never be
// confused with or accidentally merged into an in-progress sale.
let posReturnCart = []; // { sku, qty, salePrice, costPrice }

function renderPOSReturnPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginTop = '20px';
  panel.innerHTML = `
    <h2 style="margin: 0 0 12px; font-size: 16px;">Process a Return</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="max-width: 240px; margin-bottom: 0;">
        <label class="form-label" for="pos-return-order-id">Original Order / Cart Number</label>
        <input type="text" id="pos-return-order-id" class="form-input" placeholder="e.g. POS-HO-171...">
      </div>
      <div class="form-group" style="max-width: 200px; margin-bottom: 0;">
        <label class="form-label" for="pos-return-location">Return Location</label>
        <input type="text" id="pos-return-location" class="form-input" placeholder="e.g. HO" value="${posLocation}">
      </div>
      <div class="form-group" style="flex: 1; margin-bottom: 0;">
        <label class="form-label" for="pos-return-sku-input">SKU to Return</label>
        <input type="text" id="pos-return-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="pos-return-add-btn" type="button">Add Line</button>
    </div>
    <div id="pos-return-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr><th>SKU</th><th>Qty</th><th>Sale Price</th><th>Cost Price</th><th></th></tr>
      </thead>
      <tbody id="pos-return-body"></tbody>
    </table>
    <div style="display: flex; justify-content: flex-end; margin-top: 16px;">
      <button class="btn btn-primary" id="pos-return-submit-btn" type="button">Submit Return</button>
    </div>
  `;
  container.appendChild(panel);

  attachTypeahead(document.getElementById('pos-return-sku-input'), 'Item');
  document.getElementById('pos-return-add-btn').addEventListener('click', addSKUToPOSReturn);
  document.getElementById('pos-return-sku-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addSKUToPOSReturn();
    }
  });
  document.getElementById('pos-return-submit-btn').addEventListener('click', submitPOSReturn);

  renderPOSReturnTable();
}

function addSKUToPOSReturn() {
  const skuInput = document.getElementById('pos-return-sku-input');
  const sku = skuInput.value.trim();
  if (!sku) return;
  const existing = posReturnCart.find(line => line.sku === sku);
  if (existing) {
    existing.qty += 1;
  } else {
    posReturnCart.push({ sku, qty: 1, salePrice: 0, costPrice: 0 });
  }
  skuInput.value = '';
  skuInput.focus();
  renderPOSReturnTable();
}

function removeSKUFromPOSReturn(sku) {
  posReturnCart = posReturnCart.filter(line => line.sku !== sku);
  renderPOSReturnTable();
}

function updatePOSReturnLine(sku, field, value) {
  const line = posReturnCart.find(l => l.sku === sku);
  if (!line) return;
  const num = parseFloat(value);
  line[field] = isNaN(num) ? 0 : num;
  renderPOSReturnTable();
}

function renderPOSReturnTable() {
  const body = document.getElementById('pos-return-body');
  if (!body) return;
  body.innerHTML = '';
  posReturnCart.forEach(line => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td style="font-weight:600;">${line.sku}</td>
      <td><input type="number" min="1" value="${line.qty}" class="form-input" style="width: 80px;" onchange="updatePOSReturnLine('${line.sku}', 'qty', this.value)"></td>
      <td><input type="number" min="0" step="0.01" value="${line.salePrice}" class="form-input" style="width: 100px;" onchange="updatePOSReturnLine('${line.sku}', 'salePrice', this.value)"></td>
      <td><input type="number" min="0" step="0.01" value="${line.costPrice}" class="form-input" style="width: 100px;" onchange="updatePOSReturnLine('${line.sku}', 'costPrice', this.value)"></td>
      <td><button class="action-btn action-btn-danger" onclick="removeSKUFromPOSReturn('${line.sku}')">Remove</button></td>
    `;
    body.appendChild(tr);
  });
}

async function submitPOSReturn() {
  const errorEl = document.getElementById('pos-return-error');
  errorEl.classList.add('hidden');
  const orderID = document.getElementById('pos-return-order-id').value.trim();
  const returnLocation = document.getElementById('pos-return-location').value.trim();

  if (!orderID || !returnLocation) {
    errorEl.textContent = 'Original order/cart number and return location are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (posReturnCart.length === 0) {
    errorEl.textContent = 'Add at least one SKU to return.';
    errorEl.classList.remove('hidden');
    return;
  }

  const submitBtn = document.getElementById('pos-return-submit-btn');
  submitBtn.disabled = true;
  try {
    const res = await apiFetch('/api/v1/fulfillment/return', {
      method: 'POST',
      body: JSON.stringify({
        return_location: returnLocation,
        original_order_id: orderID,
        items: posReturnCart.map(line => ({
          sku: line.sku,
          qty: line.qty,
          sale_price: line.salePrice,
          cost_price: line.costPrice
        }))
      })
    });
    if (!res) return;
    if (!res.ok) {
      errorEl.textContent = await getErrorMessage(res, 'Return failed.');
      errorEl.classList.remove('hidden');
      return;
    }
    posReturnCart = [];
    renderPOSReturnTable();
    await showCustomAlert('Return processed and stock restocked.', 'Return Complete');
  } finally {
    submitBtn.disabled = false;
  }
}

// Stage 20.7: reflects whether the acting cashier already has an Open
// session at posLocation, so the POS screen doesn't let a cashier build a
// whole cart before discovering handleCheckout's 400 at the very end.
async function refreshPOSSessionStatus() {
  const statusEl = document.getElementById('pos-session-status');
  const openBtn = document.getElementById('pos-session-open-btn');
  const closeBtn = document.getElementById('pos-session-close-btn');
  if (!statusEl) return;

  if (!posLocation) {
    posOpenSessionId = '';
    statusEl.textContent = 'Enter a location to check for an open cashier session.';
    openBtn.classList.add('hidden');
    closeBtn.classList.add('hidden');
    return;
  }

  const res = await apiFetch(`/api/v1/pos/session/current?location=${encodeURIComponent(posLocation)}`);
  if (!res || !res.ok) {
    statusEl.textContent = 'Failed to check session status.';
    return;
  }
  const data = await res.json();
  posOpenSessionId = data.open ? data.session_id : '';
  if (posOpenSessionId) {
    statusEl.textContent = `Session open at ${posLocation}.`;
    openBtn.classList.add('hidden');
    closeBtn.classList.remove('hidden');
  } else {
    statusEl.textContent = `No open session at ${posLocation} - open one before selling.`;
    openBtn.classList.remove('hidden');
    closeBtn.classList.add('hidden');
  }
}

async function openPOSSessionFlow() {
  if (!posLocation) {
    await showCustomAlert('Enter a location code first.', 'Location Required');
    return;
  }
  const openingStr = await showCustomPrompt('Opening cash float for this session?');
  if (openingStr === null) return;
  const opening = parseFloat(openingStr);
  const res = await apiFetch('/api/v1/pos/session/open', {
    method: 'POST',
    body: JSON.stringify({ location: posLocation, opening_cash: isNaN(opening) ? 0 : opening })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to open session.');
    return;
  }
  await refreshPOSSessionStatus();
}

async function closePOSSessionFlow() {
  if (!posOpenSessionId) return;

  // 20.13: the offline window is this cashier's own open session - refuse
  // to close (client-side; the server has no way to see a queue that
  // hasn't synced yet) while sales are still waiting to sync, so a synced
  // sale can never land against the *next* session's cash-variance figures.
  const stillQueued = getOfflineQueue().length;
  if (stillQueued > 0) {
    await showCustomAlert(`${stillQueued} sale${stillQueued === 1 ? '' : 's'} still need${stillQueued === 1 ? 's' : ''} to sync before this session can close. Reconnect and try again, or click the offline badge above to sync now.`, 'Offline Sales Pending');
    return;
  }

  const countedStr = await showCustomPrompt('Counted cash in the till?');
  if (countedStr === null) return;
  const counted = parseFloat(countedStr);
  const res = await apiFetch('/api/v1/pos/session/close', {
    method: 'POST',
    body: JSON.stringify({ session_id: posOpenSessionId, counted_cash: isNaN(counted) ? 0 : counted })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to close session.');
    return;
  }
  // 21.9 QA-follow-up: this alert previously referenced an undefined `data`
  // variable (the response body was never parsed) - would have thrown a
  // ReferenceError on every successful close. Found while touching this
  // function for the offline-queue guard above.
  const data = await res.json();
  await showCustomAlert(`Session closed. Expected: ${data.expected_cash.toFixed(2)}, Counted: ${data.counted_cash.toFixed(2)}, Variance: ${data.variance.toFixed(2)}`, 'Session Closed');
  await refreshPOSSessionStatus();
}

async function addSKUToPOSCart() {
  const skuInput = document.getElementById('pos-sku-input');
  const errorEl = document.getElementById('pos-scan-error');
  const sku = skuInput.value.trim();
  errorEl.classList.add('hidden');

  if (!posLocation) {
    errorEl.textContent = 'Enter a location code before adding items.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (!sku) return;

  const res = await apiFetch(`/api/v1/availability?sku=${encodeURIComponent(sku)}&location=${encodeURIComponent(posLocation)}`);
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = 'Failed to look up availability for this SKU.';
    errorEl.classList.remove('hidden');
    return;
  }
  const avail = await res.json();

  const existing = posCart.find(line => line.sku === sku);
  if (existing) {
    existing.qty += 1;
  } else {
    posCart.push({ sku, available: avail.ats ?? avail.available ?? 0, qty: 1, salePrice: 0, costPrice: 0 });
  }
  skuInput.value = '';
  skuInput.focus();
  renderPOSCartTable();
}

function removeSKUFromPOSCart(sku) {
  posCart = posCart.filter(line => line.sku !== sku);
  renderPOSCartTable();
}

function updatePOSCartLine(sku, field, value) {
  const line = posCart.find(l => l.sku === sku);
  if (!line) return;
  const num = parseFloat(value);
  line[field] = isNaN(num) ? 0 : num;
  renderPOSCartTable();
}

function renderPOSCartTable() {
  const body = document.getElementById('pos-cart-body');
  if (!body) return;
  body.innerHTML = '';
  let total = 0;

  posCart.forEach(line => {
    const lineTotal = line.qty * line.salePrice;
    total += lineTotal;
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td style="font-weight:600;">${line.sku}</td>
      <td>${line.available}</td>
      <td><input type="number" min="1" value="${line.qty}" class="form-input" style="width: 80px;" onchange="updatePOSCartLine('${line.sku}', 'qty', this.value)"></td>
      <td><input type="number" min="0" step="0.01" value="${line.salePrice}" class="form-input" style="width: 100px;" onchange="updatePOSCartLine('${line.sku}', 'salePrice', this.value)"></td>
      <td><input type="number" min="0" step="0.01" value="${line.costPrice}" class="form-input" style="width: 100px;" onchange="updatePOSCartLine('${line.sku}', 'costPrice', this.value)"></td>
      <td>${lineTotal.toFixed(2)}</td>
      <td><button class="action-btn action-btn-danger" onclick="removeSKUFromPOSCart('${line.sku}')">Remove</button></td>
    `;
    body.appendChild(tr);
  });

  document.getElementById('pos-cart-total').textContent = total.toFixed(2);
}

// 20.13 Offline-first POS queue.
//
// Decisions (user, 2026-07-22): the offline window is one shift, tied to
// the cashier's own open POSSession - see closePOSSessionFlow's guard
// below, which refuses to close a session while sales are still queued
// rather than the server trying to police a client-side queue it can't
// see. A sale that finally syncs after stock changed while offline always
// posts (the goods already physically left the store and payment was
// already taken) and is allowed to push inventory negative rather than
// rejected - engines/pos_checkout.go's recordOfflineSyncVariance flags the
// shortfall on a new POSOfflineSyncVariance record for a manager to review.
//
// cart_number (client-generated in submitPOSCheckout, unchanged from
// before this stage) doubles as the idempotency key handleCheckout already
// enforces server-side (see its own "Idempotency guard" comment) - reusing
// the exact same cart_number on every sync retry is what makes retrying a
// still-queued cart safe with zero new server-side mechanism.

function getOfflineQueue() {
  try {
    return JSON.parse(localStorage.getItem(OFFLINE_QUEUE_KEY) || '[]');
  } catch (e) {
    return [];
  }
}

function saveOfflineQueue(queue) {
  localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(queue));
  renderOfflineQueueBadge();
}

function queueOfflinePOSCart(payload) {
  const queue = getOfflineQueue();
  queue.push({ cartNumber: payload.cart_number, location: payload.location, payload, queuedAt: new Date().toISOString() });
  saveOfflineQueue(queue);
}

// Renders/updates the small badge in the POS session bar showing how many
// sales are still queued offline. A no-op on any screen other than POS
// (the element just won't exist), so this is safe to call from anywhere -
// in particular from the global online-event/poll handlers in
// setupOfflineSync(), which don't know or care which view is on screen.
function renderOfflineQueueBadge() {
  const badge = document.getElementById('pos-offline-queue-badge');
  if (!badge) return;
  const queue = getOfflineQueue();
  if (queue.length === 0) {
    badge.classList.add('hidden');
    badge.textContent = '';
    return;
  }
  badge.classList.remove('hidden');
  badge.textContent = `${queue.length} sale${queue.length === 1 ? '' : 's'} queued offline`;
}

// Dedicated from apiFetch (same reasoning apiUpload's own header comment
// gives for its own bespoke fetch wrapper): a genuine network failure here
// means "queue this sale and keep selling," not apiFetch's default of a
// blocking "Unable to reach the server" alert - a cashier mid-shift can't
// stop to dismiss a dialog every time connectivity blips. A reachable
// server that responds 401/429 is not an offline condition, so those still
// get apiFetch's normal handling. Returns 'queued' (queued locally), null
// (401/429 already handled, same convention apiFetch itself uses), or the
// raw Response for the caller to interpret as usual.
async function checkoutOnlineOrQueue(payload) {
  if (!navigator.onLine) {
    queueOfflinePOSCart(payload);
    return 'queued';
  }
  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const headers = { 'Content-Type': 'application/json', 'X-Tenant-ID': tenantID };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  let response;
  try {
    response = await fetch('/api/v1/checkout', { method: 'POST', headers, body: JSON.stringify(payload) });
  } catch (err) {
    queueOfflinePOSCart(payload);
    return 'queued';
  }
  if (response.status === 401) {
    logout(await getErrorMessage(response, 'Session expired. Please log in again.'));
    return null;
  }
  if (response.status === 429) {
    showToast(await getErrorMessage(response, 'Rate limit exceeded. Please throttle your requests.'), { variant: 'warning', title: 'Rate Limit' });
    return null;
  }
  return response;
}

// Replays the offline queue in original order (oldest first) once back
// online. Stops at the first entry that still can't reach the server (a
// flaky reconnect, not just a flat-out offline/online flag) and leaves
// that one plus everything after it queued for the next attempt - never
// reorders or drops a cart just because a later one in the queue happened
// to succeed first. A cart the server outright rejects (not a network
// failure - a real validation error) is surfaced to the user and dropped
// rather than retried forever, which would otherwise block the shift from
// ever closing over one sale that can never succeed as-is.
async function trySyncOfflineQueue() {
  if (offlineSyncInFlight || !navigator.onLine) return;
  const queue = getOfflineQueue();
  if (queue.length === 0) return;
  offlineSyncInFlight = true;

  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const headers = { 'Content-Type': 'application/json', 'X-Tenant-ID': tenantID };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let syncedCount = 0;
  let i = 0;
  // Only true when the loop stopped because the server genuinely couldn't
  // be reached (or the session expired) - never for a rejection, which is
  // deliberately dropped instead of retried forever (see header comment).
  let stoppedForReconnect = false;
  try {
    for (; i < queue.length; i++) {
      const entry = queue[i];
      let response;
      try {
        response = await fetch('/api/v1/checkout', {
          method: 'POST',
          headers,
          body: JSON.stringify({ ...entry.payload, offline_synced: true })
        });
      } catch (err) {
        stoppedForReconnect = true;
        break;
      }
      if (response.ok) {
        syncedCount++;
        continue;
      }
      if (response.status === 401) {
        stoppedForReconnect = true;
        logout('Session expired while syncing offline sales. Log in again to finish syncing.');
        break;
      }
      const msg = await getErrorMessage(response, 'Offline sale failed to sync.');
      await showCustomAlert(`Sale ${entry.cartNumber} could not be synced and was removed from the offline queue - it needs manual attention: ${msg}`, 'Offline Sync Failed');
      // Dropped: the loop continues to i+1 without adding this entry back.
    }
  } finally {
    saveOfflineQueue(stoppedForReconnect ? queue.slice(i) : []);
    offlineSyncInFlight = false;
    if (syncedCount > 0) showToast(`${syncedCount} offline sale${syncedCount === 1 ? '' : 's'} synced.`, { variant: 'success' });
  }
}

// Registered once at app init (see init()). The 'online' browser event is
// the primary trigger; the 30s poll is a fallback for the cases that event
// doesn't reliably fire (some OS/browser network-transition paths), and is
// cheap to skip when the queue is already empty.
function setupOfflineSync() {
  window.addEventListener('online', trySyncOfflineQueue);
  setInterval(() => {
    if (getOfflineQueue().length > 0) trySyncOfflineQueue();
  }, 30000);
}

async function submitPOSCheckout() {
  const errorEl = document.getElementById('pos-scan-error');
  errorEl.classList.add('hidden');

  if (!posLocation) {
    errorEl.textContent = 'Enter a location code before completing the sale.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (posCart.length === 0) {
    errorEl.textContent = 'Add at least one item to the cart first.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (posCart.some(line => line.qty <= 0 || line.salePrice <= 0)) {
    errorEl.textContent = 'Every line needs a quantity and sale price greater than zero.';
    errorEl.classList.remove('hidden');
    return;
  }

  const checkoutBtn = document.getElementById('pos-checkout-btn');
  checkoutBtn.disabled = true;
  try {
    const cartNumber = `POS-${posLocation}-${Date.now()}`;
    const paymentMode = document.getElementById('pos-payment-mode').value;
    const discountPct = parseFloat(document.getElementById('pos-discount-pct').value) || 0;
    const cartItems = posCart.map(line => ({
      sku: line.sku,
      qty: line.qty,
      sale_price: line.salePrice,
      cost_price: line.costPrice
    }));
    const res = await checkoutOnlineOrQueue({
      cart_number: cartNumber,
      location: posLocation,
      payment_mode: paymentMode,
      customer_id: document.getElementById('pos-customer').value.trim(),
      discount_pct: discountPct,
      items: cartItems
    });
    if (res === 'queued') {
      posCart = [];
      renderPOSCartTable();
      showToast(`No connection - sale ${cartNumber} queued offline and will sync automatically once reconnected.`, { variant: 'warning', title: 'Offline' });
      return;
    }
    if (!res) return;
    const data = await res.json();
    if (!res.ok) {
      errorEl.textContent = data.error || 'Checkout failed.';
      errorEl.classList.remove('hidden');
      return;
    }

    // Stage 20.10: a discount above the configured threshold doesn't
    // complete the sale here - it's now Pending Approval and will finalize
    // (inventory/GL) once a manager decides it Approved from the Approvals screen.
    if (data.status === 'pending_approval') {
      posCart = [];
      renderPOSCartTable();
      await showCustomAlert(data.message || 'This sale requires manager approval before it completes.', 'Approval Required');
      return;
    }

    posCart = [];
    renderPOSCartTable();
    const printReceipt = await showCustomConfirm(`Sale ${data.cart_number} completed. Total: ${data.sale_total}. Print receipt?`, 'Sale Complete');
    if (printReceipt) {
      printPOSReceipt(data.cart_number, posLocation, paymentMode, cartItems, data.sale_total);
    }
  } finally {
    checkoutBtn.disabled = false;
  }
}

// Stage 20.14: reuses the sticker-print-area's hidden-until-printing @media
// print pattern (styles.css) rather than a new PDF/print dependency.
function printPOSReceipt(cartNumber, location, paymentMode, items, saleTotal) {
  const area = document.getElementById('receipt-print-area');
  if (!area) return;
  const lines = items.map(it => `
    <div class="receipt-line"><span>${it.sku} x${it.qty}</span><span>${(it.qty * it.sale_price).toFixed(2)}</span></div>
  `).join('');
  area.innerHTML = `
    <div class="receipt">
      <div class="receipt-header">
        <div class="receipt-title">Sales Receipt</div>
        <div>${cartNumber}</div>
        <div>${location} &middot; ${new Date().toLocaleString()}</div>
      </div>
      <hr>
      ${lines}
      <hr>
      <div class="receipt-line receipt-total"><span>Total (${paymentMode})</span><span>${Number(saleTotal).toFixed(2)}</span></div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// CRM/Loyalty (Stage 13.13d, scoped MVP) - POS integration. Earning
// happens automatically server-side (handleCheckout) once customer_id is
// set; these two actions are the customer-facing "check balance" /
// "redeem" steps a cashier drives manually before completing the sale.
async function checkPOSLoyaltyBalance() {
  const infoEl = document.getElementById('pos-loyalty-info');
  const customerId = document.getElementById('pos-customer').value.trim();
  if (!customerId) {
    infoEl.textContent = 'Enter a customer code first.';
    return;
  }
  const res = await apiFetch(`/api/v1/loyalty/ledger?customer_id=${encodeURIComponent(customerId)}`);
  if (!res) return;
  if (!res.ok) {
    infoEl.textContent = 'Failed to look up loyalty balance.';
    return;
  }
  const data = await res.json();
  infoEl.textContent = `${customerId} has ${data.balance} loyalty point(s).`;
}

async function redeemPOSLoyaltyPoints() {
  const infoEl = document.getElementById('pos-loyalty-info');
  const customerId = document.getElementById('pos-customer').value.trim();
  if (!customerId) {
    infoEl.textContent = 'Enter a customer code first.';
    return;
  }
  const pointsStr = await showCustomPrompt('How many points to redeem?');
  const points = parseInt(pointsStr, 10);
  if (!points || points <= 0) return;

  const res = await apiFetch('/api/v1/loyalty/redeem', {
    method: 'POST',
    body: JSON.stringify({ customer_id: customerId, points })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    infoEl.textContent = data.error || 'Redemption failed.';
    return;
  }
  infoEl.textContent = `Redeemed ${points} point(s) for a discount of ${data.discount_value}. Apply this manually to a cart line's Sale Price before completing the sale.`;
}

// Finance / GL screen - read-only trial balance view against the already-
// working GET /api/v1/finance/trial-balance API (Stage 13.5). Same story as
// the POS screen: the double-entry posting engine and API already work and
// are tested, there was just no screen to see them.
let currentFinanceTab = 'trial-balance';
const FINANCE_TABS = [
  { id: 'trial-balance', label: 'Trial Balance' },
  { id: 'periods', label: 'Accounting Periods' }
];

async function renderFinanceView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Finance / GL</h1>
      <p class="page-subtitle">Trial balance across all posted GL accounts, and accounting-period close control.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = FINANCE_TABS.map(t =>
    `<button class="btn ${t.id === currentFinanceTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-finance-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-finance-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentFinanceTab = btn.getAttribute('data-finance-tab');
      renderView('finance');
    });
  });

  if (currentFinanceTab === 'periods') {
    await renderAccountingPeriodsPanel(container);
    return;
  }

  const res = await apiFetch('/api/v1/finance/trial-balance');
  if (!res) return;

  if (!res.ok) {
    const panel = document.createElement('div');
    panel.className = 'table-panel';
    panel.style.padding = '24px';
    panel.textContent = 'Failed to load trial balance.';
    container.appendChild(panel);
    return;
  }

  const data = await res.json();
  const balances = data.balances || [];

  const summaryRow = document.createElement('div');
  summaryRow.className = 'dashboard-stats-row';
  summaryRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Total Debits</span>
      <span class="stat-val">${(data.total_debits ?? 0).toLocaleString()}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Total Credits</span>
      <span class="stat-val">${(data.total_credits ?? 0).toLocaleString()}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Ledger Status</span>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
        <span class="pulse-dot" style="background: ${data.balanced ? '#10b981' : '#ef4444'};"></span>
        <span style="font-size: 16px; font-weight: 700; color: ${data.balanced ? '#10b981' : '#ef4444'};">${data.status || ''}</span>
      </div>
    </div>
  `;
  container.appendChild(summaryRow);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Account Code</th>
          <th>Account Name</th>
          <th>Type</th>
          <th>Debit</th>
          <th>Credit</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (balances.length === 0) {
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No GL postings yet.</td></tr>`;
  }
  balances.forEach(b => {
    html += `
      <tr>
        <td style="font-family: monospace;">${b.account_code}</td>
        <td style="font-weight:600;">${b.account_name}</td>
        <td>${b.account_type}</td>
        <td>${b.debit.toLocaleString()}</td>
        <td>${b.credit.toLocaleString()}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// Accounting Periods (Stage 20.34): this had no frontend at all before this
// stage even though Stage 17.4's create/list/close API has existed since
// then - a real pre-existing gap, same shape as the Transfers/Inventory/
// Users/Roles ones Stage 22 fixed. Create + list + close, plus the new
// read-only pre-close checklist (engines.GetPeriodCloseChecklist) surfaced
// before the user commits to closing a period.
async function renderAccountingPeriodsPanel(container) {
  const res = await apiFetch('/api/v1/finance/periods');
  if (!res) return;
  const periods = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Accounting Period</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-name">Period Name</label>
        <input type="text" id="period-name" class="form-input" placeholder="e.g. FY2026-Q3" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-start">Start Date</label>
        <input type="date" id="period-start" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="period-end">End Date</label>
        <input type="date" id="period-end" class="form-input">
      </div>
      <button class="btn btn-primary" id="period-create-btn">Create Period</button>
    </div>
    <div id="period-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Period Name</th><th>Start</th><th>End</th><th>Status</th><th>Closed By</th><th>Actions</th></tr></thead>
      <tbody>
        ${periods.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No accounting periods yet.</td></tr>`
          : periods.map(p => `
            <tr>
              <td style="font-weight:600;">${p.period_name}</td>
              <td>${p.start_date}</td>
              <td>${p.end_date}</td>
              <td><span class="badge ${p.status === 'Open' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
              <td>${p.closed_by || ''}</td>
              <td>
                ${p.status === 'Open' ? `<button class="action-btn" onclick="showPeriodCloseChecklist('${p.id}')">Close Checklist</button>` : ''}
              </td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);

  document.getElementById('period-create-btn').addEventListener('click', async () => {
    const errorEl = document.getElementById('period-form-error');
    errorEl.classList.add('hidden');
    const periodName = document.getElementById('period-name').value.trim();
    const startDate = document.getElementById('period-start').value;
    const endDate = document.getElementById('period-end').value;
    if (!periodName || !startDate || !endDate) {
      errorEl.textContent = 'Period name, start date, and end date are all required.';
      errorEl.classList.remove('hidden');
      return;
    }
    const createRes = await apiFetch('/api/v1/finance/periods', {
      method: 'POST',
      body: JSON.stringify({ period_name: periodName, start_date: startDate, end_date: endDate })
    });
    if (!createRes) return;
    if (!createRes.ok) {
      errorEl.textContent = await getErrorMessage(createRes, 'Failed to create period.');
      errorEl.classList.remove('hidden');
      return;
    }
    renderView('finance');
  });
}

// Shows the pre-close checklist in a confirm dialog and, if the user
// proceeds, calls the existing close endpoint - the checklist itself never
// blocks closing (it's advisory, see engines.PeriodCloseChecklist's own
// comment), it just makes the consequences visible first.
async function showPeriodCloseChecklist(periodId) {
  const res = await apiFetch(`/api/v1/finance/periods/${periodId}/close-checklist`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load close checklist.');
    return;
  }
  const checklist = await res.json();
  const lines = checklist.checks.map(c => `${c.passed ? '✓' : '✗'} ${c.name} - ${c.detail}`).join('\n');
  const summary = checklist.ready_to_close
    ? 'All checks passed.'
    : 'One or more checks did not pass - review before closing.';
  const proceed = await showCustomConfirm(
    `${summary}\n\n${lines}\n\nClosing a period is permanent - there is no reopen. Close "${checklist.period_name}" now?`,
    'Period Close Checklist'
  );
  if (!proceed) return;
  const closeRes = await apiFetch(`/api/v1/finance/periods/${periodId}/close`, { method: 'POST' });
  if (!closeRes) return;
  if (!closeRes.ok) {
    await showApiError(closeRes, 'Failed to close period.');
    return;
  }
  renderView('finance');
}

// Vendor Invoices (Stage 20.27/20.28 prerequisite): VendorInvoice has
// existed since Stage 17.8 (3-way match + pay) with zero frontend of its
// own. Creation reuses the generic doctype-table's own New-record form
// (VendorInvoice's fields are already registered) rather than a parallel
// create form here - this view only adds the match/pay actions the generic
// CRUD screen can't express.
async function renderVendorInvoicesView(container) {
  const [invRes, sectionsRes] = await Promise.all([
    apiFetch('/api/v1/doc/VendorInvoice'),
    apiFetch('/api/v1/doc/TDSSection')
  ]);
  if (!invRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Vendor Invoices</h1>
      <p class="page-subtitle">3-way match against PO/GRN, then pay - plain or TDS-withheld.</p>
    </div>
    <button class="btn btn-primary" id="vi-new-btn">+ New Vendor Invoice</button>
  `;
  container.appendChild(header);
  document.getElementById('vi-new-btn').addEventListener('click', () => {
    currentDoctype = 'VendorInvoice'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });

  const invoices = invRes.ok ? await invRes.json() : [];
  const sections = (sectionsRes && sectionsRes.ok) ? await sectionsRes.json() : [];
  window.__tdsSections = sections;

  const STATUS_BADGE = { Draft: 'badge-secondary', Matched: 'badge-success', MismatchHold: 'badge-danger', Paid: 'badge-success' };
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Invoice #</th><th>Vendor</th><th>PO</th><th>GRN</th><th>Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No vendor invoices yet.</td></tr>`
          : invoices.map(v => `
            <tr>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.vendor_id || ''}</td>
              <td>${v.po_id || ''}</td>
              <td>${v.grn_id || ''}</td>
              <td>${(v.invoice_amount ?? 0).toLocaleString()}</td>
              <td><span class="badge ${STATUS_BADGE[v.status] || 'badge-secondary'}">${v.status}</span></td>
              <td>${renderVendorInvoiceActions(v)}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

function renderVendorInvoiceActions(v) {
  const id = v.id;
  if (v.status === 'Draft' || v.status === 'MismatchHold') {
    return `<button class="action-btn" onclick="matchVendorInvoice('${id}')">Match</button>`;
  }
  if (v.status === 'Matched') {
    const sections = window.__tdsSections || [];
    const tdsOptions = sections.map(s => `<option value="${s.section_code || s.id}">${s.section_code || s.id} (${s.rate_percent}%)</option>`).join('');
    return `
      <button class="action-btn" onclick="payVendorInvoicePlain('${id}')">Pay</button>
      ${sections.length > 0 ? `
        <select id="tds-select-${id}" class="form-select" style="width: 110px; display:inline-block; margin: 0 4px;">${tdsOptions}</select>
        <button class="action-btn" onclick="payVendorInvoiceTDS('${id}')">Pay w/ TDS</button>
      ` : ''}
    `;
  }
  return '';
}

async function matchVendorInvoice(invoiceId) {
  const poId = await showCustomPrompt('Enter the PO ID/number this invoice matches:', '', 'Match Invoice');
  if (poId === null) return;
  const grnId = await showCustomPrompt('Enter the GRN ID/number this invoice matches:', '', 'Match Invoice');
  if (grnId === null) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/match', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, po_id: poId, grn_id: grnId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Match failed.');
    return;
  }
  renderView('vendor-invoices');
}

async function payVendorInvoicePlain(invoiceId) {
  const confirmed = await showCustomConfirm('Pay this vendor invoice in full via Cash/Bank?', 'Confirm Payment');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Payment failed.');
    return;
  }
  renderView('vendor-invoices');
}

async function payVendorInvoiceTDS(invoiceId) {
  const select = document.getElementById(`tds-select-${invoiceId}`);
  const tdsSection = select ? select.value : '';
  if (!tdsSection) return;
  const confirmed = await showCustomConfirm(`Pay this vendor invoice with TDS withheld under section ${tdsSection}?`, 'Confirm Payment');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay-with-tds', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, tds_section: tdsSection })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Payment failed.');
    return;
  }
  renderView('vendor-invoices');
}

// Payment Proposals (Stage 20.27): batches multiple Matched VendorInvoices
// into one payment run via engines.CreatePaymentProposal/ExecutePaymentProposal.
async function renderPaymentProposalsView(container) {
  const [invRes, propRes] = await Promise.all([
    apiFetch('/api/v1/doc/VendorInvoice'),
    apiFetch('/api/v1/doc/PaymentProposal')
  ]);
  if (!invRes || !propRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Payment Proposals</h1>
      <p class="page-subtitle">Group Matched vendor invoices into one payment run.</p>
    </div>
  `;
  container.appendChild(header);

  const invoices = (invRes.ok ? await invRes.json() : []).filter(v => v.status === 'Matched');
  const proposals = propRes.ok ? await propRes.json() : [];

  const builderPanel = document.createElement('div');
  builderPanel.className = 'table-panel';
  builderPanel.style.padding = '24px';
  builderPanel.style.marginBottom = '24px';
  builderPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Build a Proposal from Matched Invoices</h2>
    ${invoices.length === 0 ? `<p style="color:var(--text-muted);">No Matched vendor invoices available to batch.</p>` : `
      <table style="margin-bottom: 16px;">
        <thead><tr><th></th><th>Invoice #</th><th>Vendor</th><th>Amount</th></tr></thead>
        <tbody>
          ${invoices.map(v => `
            <tr>
              <td><input type="checkbox" class="pp-invoice-check" value="${v.id}"></td>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.vendor_id || ''}</td>
              <td>${(v.invoice_amount ?? 0).toLocaleString()}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
      <button class="btn btn-primary" id="pp-create-btn">Create Proposal from Selected</button>
    `}
    <div id="pp-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(builderPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.innerHTML = `
    <table>
      <thead><tr><th>Proposal #</th><th>Invoices</th><th>Total Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${proposals.length === 0
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No payment proposals yet.</td></tr>`
          : proposals.map(p => {
            let ids = [];
            try { ids = JSON.parse(p.invoice_ids || '[]'); } catch (e) { /* leave empty */ }
            return `
              <tr>
                <td style="font-family: monospace;">${p.proposal_number || p.id}</td>
                <td>${ids.length} invoice(s)</td>
                <td>${(p.total_amount ?? 0).toLocaleString()}</td>
                <td><span class="badge ${p.status === 'Executed' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
                <td>${p.status === 'Draft' ? `<button class="action-btn" onclick="executePaymentProposal('${p.id}')">Execute</button>` : ''}</td>
              </tr>
            `;
          }).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(listPanel);

  const createBtn = document.getElementById('pp-create-btn');
  if (createBtn) createBtn.addEventListener('click', async () => {
    const errorEl = document.getElementById('pp-form-error');
    errorEl.classList.add('hidden');
    const selected = Array.from(document.querySelectorAll('.pp-invoice-check:checked')).map(c => c.value);
    if (selected.length === 0) {
      errorEl.textContent = 'Select at least one invoice.';
      errorEl.classList.remove('hidden');
      return;
    }
    const res = await apiFetch('/api/v1/finance/payment-proposal', {
      method: 'POST',
      body: JSON.stringify({ invoice_ids: selected })
    });
    if (!res) return;
    if (!res.ok) {
      errorEl.textContent = await getErrorMessage(res, 'Failed to create proposal.');
      errorEl.classList.remove('hidden');
      return;
    }
    renderView('payment-proposals');
  });
}

async function executePaymentProposal(proposalId) {
  const confirmed = await showCustomConfirm('Execute this payment run? Every invoice in it will be paid via the standard vendor-invoice payment path.', 'Execute Payment Proposal');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/finance/payment-proposal/${proposalId}/execute`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Execution failed.');
    return;
  }
  const results = await res.json();
  const failed = (results.results || []).filter(r => !r.paid);
  if (failed.length > 0) {
    await showCustomAlert(`Proposal executed with ${failed.length} failure(s):\n${failed.map(f => `${f.invoice_id}: ${f.error}`).join('\n')}`, 'Partial Failure');
  }
  renderView('payment-proposals');
}

// Bank Reconciliation (Stage 20.25/20.26): BankAccount/BankStatementLine
// creation and CSV import reuse the generic doctype-table screen (linked
// below) - this view only adds the reconcile action the generic CRUD
// screen can't express.
async function renderBankReconciliationView(container) {
  const res = await apiFetch('/api/v1/doc/BankAccount');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bank Reconciliation</h1>
      <p class="page-subtitle">Match imported bank-statement lines against GL postings for a bank account.</p>
    </div>
  `;
  container.appendChild(header);

  const accounts = res.ok ? await res.json() : [];
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0; min-width: 220px;">
        <label class="form-label" for="bank-recon-account">Bank Account</label>
        <select id="bank-recon-account" class="form-select">
          <option value="">Select a bank account...</option>
          ${accounts.map(a => `<option value="${a.id}">${a.bank_name || ''} - ${a.account_number || a.id}</option>`).join('')}
        </select>
      </div>
      <button class="btn btn-primary" id="bank-recon-btn">Reconcile</button>
      <button class="btn btn-outline" id="bank-recon-manage-accounts-btn">Manage Bank Accounts</button>
      <button class="btn btn-outline" id="bank-recon-manage-lines-btn">Statement Lines / Import CSV</button>
    </div>
    ${accounts.length === 0 ? `<p style="color:var(--text-muted);">No bank accounts yet - use "Manage Bank Accounts" to add one.</p>` : ''}
    <div id="bank-recon-result"></div>
  `;
  container.appendChild(panel);

  document.getElementById('bank-recon-manage-accounts-btn').addEventListener('click', () => {
    currentDoctype = 'BankAccount'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-recon-manage-lines-btn').addEventListener('click', () => {
    currentDoctype = 'BankStatementLine'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('bank-recon-btn').addEventListener('click', async () => {
    const bankAccount = document.getElementById('bank-recon-account').value;
    const resultEl = document.getElementById('bank-recon-result');
    if (!bankAccount) {
      resultEl.innerHTML = `<p class="login-error">Select a bank account first.</p>`;
      return;
    }
    const reconRes = await apiFetch('/api/v1/finance/bank-reconcile', {
      method: 'POST',
      body: JSON.stringify({ bank_account: bankAccount })
    });
    if (!reconRes) return;
    if (!reconRes.ok) {
      resultEl.innerHTML = `<p class="login-error">${await getErrorMessage(reconRes, 'Reconciliation failed.')}</p>`;
      return;
    }
    const result = await reconRes.json();
    resultEl.innerHTML = `
      <div class="table-panel" style="padding: 16px; margin-top: 8px;">
        <p><strong>${result.matched}</strong> line(s) matched.</p>
        <p>${result.unmatched_statement_lines.length} statement line(s) still unmatched: ${result.unmatched_statement_lines.join(', ') || 'none'}</p>
        <p>${result.unmatched_gl_postings.length} GL posting(s) still unmatched: ${result.unmatched_gl_postings.join(', ') || 'none'}</p>
      </div>
    `;
  });
}

// Debit / Credit Notes (Stage 20.32). Creation reuses the generic
// doctype-table New-record form for each doctype; this view adds the Post
// action (GL reversal) the generic CRUD screen can't express.
async function renderFinanceNotesView(container) {
  const [debitRes, creditRes] = await Promise.all([
    apiFetch('/api/v1/doc/DebitNote'),
    apiFetch('/api/v1/doc/CreditNote')
  ]);
  if (!debitRes || !creditRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Debit / Credit Notes</h1>
      <p class="page-subtitle">Post-facto vendor and customer adjustments, GL-reversing on Post.</p>
    </div>
  `;
  container.appendChild(header);

  const debitNotes = debitRes.ok ? await debitRes.json() : [];
  const creditNotes = creditRes.ok ? await creditRes.json() : [];

  const debitPanel = document.createElement('div');
  debitPanel.className = 'table-panel';
  debitPanel.style.padding = '24px';
  debitPanel.style.marginBottom = '24px';
  debitPanel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700;">Debit Notes (to Vendors)</h2>
      <button class="btn btn-outline btn-sm" id="dn-new-btn">+ New Debit Note</button>
    </div>
    <table>
      <thead><tr><th>Note #</th><th>Vendor</th><th>Amount</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${debitNotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No debit notes yet.</td></tr>`
          : debitNotes.map(n => `
            <tr>
              <td style="font-family: monospace;">${n.note_number || n.id}</td>
              <td>${n.vendor_id || ''}</td>
              <td>${(n.amount ?? 0).toLocaleString()}</td>
              <td>${n.reason || ''}</td>
              <td><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${n.status}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" onclick="postFinanceNote('DebitNote', '${n.id}')">Post</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(debitPanel);

  const creditPanel = document.createElement('div');
  creditPanel.className = 'table-panel';
  creditPanel.style.padding = '24px';
  creditPanel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h2 style="font-size: 16px; font-weight: 700;">Credit Notes (to Customers)</h2>
      <button class="btn btn-outline btn-sm" id="cn-new-btn">+ New Credit Note</button>
    </div>
    <table>
      <thead><tr><th>Note #</th><th>Customer</th><th>Amount</th><th>Reason</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${creditNotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No credit notes yet.</td></tr>`
          : creditNotes.map(n => `
            <tr>
              <td style="font-family: monospace;">${n.note_number || n.id}</td>
              <td>${n.customer_id || ''}</td>
              <td>${(n.amount ?? 0).toLocaleString()}</td>
              <td>${n.reason || ''}</td>
              <td><span class="badge ${n.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${n.status}</span></td>
              <td>${n.status === 'Draft' ? `<button class="action-btn" onclick="postFinanceNote('CreditNote', '${n.id}')">Post</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(creditPanel);

  document.getElementById('dn-new-btn').addEventListener('click', () => {
    currentDoctype = 'DebitNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('cn-new-btn').addEventListener('click', () => {
    currentDoctype = 'CreditNote'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });
}

async function postFinanceNote(doctype, id) {
  const confirmed = await showCustomConfirm('Post this note? This books the GL reversal immediately and cannot be undone.', 'Confirm Post');
  if (!confirmed) return;
  const endpoint = doctype === 'DebitNote' ? `/api/v1/finance/debit-note/${id}/post` : `/api/v1/finance/credit-note/${id}/post`;
  const res = await apiFetch(endpoint, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post note.');
    return;
  }
  renderView('finance-notes');
}

// Sales Invoices (Stage 20.33 prerequisite): SalesInvoice has existed since
// Stage 1 as a registered doctype with zero GL/amount/frontend - this view
// plus engines/sales_invoice.go make it a real credit-sales flow, the
// source Receivables Ageing reads.
async function renderSalesInvoicesView(container) {
  const res = await apiFetch('/api/v1/doc/SalesInvoice');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sales Invoices</h1>
      <p class="page-subtitle">Credit sales to customers - Post to recognize the receivable, Settle once paid.</p>
    </div>
    <button class="btn btn-primary" id="si-new-btn">+ New Sales Invoice</button>
  `;
  container.appendChild(header);
  document.getElementById('si-new-btn').addEventListener('click', () => {
    currentDoctype = 'SalesInvoice'; currentSearchQuery = ''; currentTablePage = 1;
    renderView('doctype-table');
  });

  const invoices = res.ok ? await res.json() : [];
  const STATUS_BADGE = { Draft: 'badge-secondary', Approved: 'badge-warning', Paid: 'badge-success', Cancelled: 'badge-danger' };
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Invoice #</th><th>Customer</th><th>Amount</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody>
        ${invoices.length === 0
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No sales invoices yet.</td></tr>`
          : invoices.map(v => `
            <tr>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.customer || ''}</td>
              <td>${(v.total_amount ?? 0).toLocaleString()}</td>
              <td><span class="badge ${STATUS_BADGE[v.status] || 'badge-secondary'}">${v.status}</span></td>
              <td>
                ${v.status === 'Draft' ? `<button class="action-btn" onclick="postSalesInvoiceAction('${v.id}')">Post</button>` : ''}
                ${v.status === 'Approved' ? `<button class="action-btn" onclick="settleSalesInvoiceAction('${v.id}')">Settle</button>` : ''}
              </td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

async function postSalesInvoiceAction(id) {
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${id}/post`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post invoice.');
    return;
  }
  renderView('sales-invoices');
}

async function settleSalesInvoiceAction(id) {
  const confirmed = await showCustomConfirm('Mark this invoice as settled (customer paid in full)?', 'Confirm Settlement');
  if (!confirmed) return;
  const res = await apiFetch(`/api/v1/finance/sales-invoice/${id}/settle`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to settle invoice.');
    return;
  }
  renderView('sales-invoices');
}

// Fulfillment / reservation workbench (Stage 13.6) - pick/pack/dispatch
// against FulfillmentTask documents (already a real doctype, stored via the
// generic documents table - GET /api/v1/doc/FulfillmentTask lists them with
// no new backend endpoint needed) and the already-working
// POST /api/v1/fulfillment/task/transition. The backend doesn't enforce a
// specific transition order (see engines.TransitionTaskStatus), so the
// "next status" buttons below are a UX guardrail, not a hard constraint.
const FULFILLMENT_STATUS_BADGE = {
  Pending: 'badge-warning',
  Picking: 'badge-secondary',
  Packed: 'badge-secondary',
  Dispatched: 'badge-success',
  Rejected: 'badge-danger'
};

async function renderFulfillmentView(container) {
  const res = await apiFetch('/api/v1/doc/FulfillmentTask');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Fulfillment</h1>
      <p class="page-subtitle">Pick, pack, and dispatch tasks routed to your location.</p>
    </div>
  `;
  container.appendChild(header);

  if (!res.ok) {
    const panel = document.createElement('div');
    panel.className = 'table-panel';
    panel.style.padding = '24px';
    panel.textContent = 'Failed to load fulfillment tasks.';
    container.appendChild(panel);
    return;
  }

  const tasks = await res.json();
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Task ID</th>
          <th>Order ID</th>
          <th>Location</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (!tasks || tasks.length === 0) {
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No fulfillment tasks.</td></tr>`;
  }
  (tasks || []).forEach(t => {
    const badgeClass = FULFILLMENT_STATUS_BADGE[t.status] || 'badge-secondary';
    html += `
      <tr>
        <td style="font-family: monospace;">${t.code || t.id}</td>
        <td>${t.order_id || ''}</td>
        <td>${t.location_code || ''}</td>
        <td><span class="badge ${badgeClass}">${t.status}</span></td>
        <td>${renderFulfillmentActions(t)}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

function renderFulfillmentActions(task) {
  const id = task.code || task.id;
  switch (task.status) {
    case 'Pending':
      return `
        <button class="action-btn" onclick="transitionFulfillmentTask('${id}', 'Picking')">Start Picking</button>
        <button class="action-btn action-btn-danger" onclick="transitionFulfillmentTask('${id}', 'Rejected')">Reject</button>
      `;
    case 'Picking':
      return `
        <button class="action-btn" onclick="transitionFulfillmentTask('${id}', 'Packed')">Mark Packed</button>
        <button class="action-btn action-btn-danger" onclick="transitionFulfillmentTask('${id}', 'Rejected')">Reject</button>
      `;
    case 'Packed':
      return `<button class="action-btn" onclick="transitionFulfillmentTask('${id}', 'Dispatched')">Dispatch</button>`;
    default:
      return '';
  }
}

async function transitionFulfillmentTask(taskId, newStatus) {
  const res = await apiFetch('/api/v1/fulfillment/task/transition', {
    method: 'POST',
    body: JSON.stringify({ task_id: taskId, status: newStatus })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update task status.');
    return;
  }
  renderView('fulfillment');
}

// Marketplace settlement + logistics booking screen (Stage 13.7) - both
// MarketplaceSettlement and LogisticsBooking are already real doctypes
// (listed via the generic GET /api/v1/doc/... endpoint, no new backend code
// needed for reading), and reconciliation/booking already work via
// POST /api/v1/marketplace/settlement/reconcile and .../logistics/book.
async function renderMarketplaceView(container) {
  const [settlementsRes, bookingsRes] = await Promise.all([
    apiFetch('/api/v1/doc/MarketplaceSettlement'),
    apiFetch('/api/v1/doc/LogisticsBooking')
  ]);
  if (!settlementsRes || !bookingsRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Marketplace</h1>
      <p class="page-subtitle">Channel settlement reconciliation and logistics bookings.</p>
    </div>
  `;
  container.appendChild(header);

  const settlements = settlementsRes.ok ? await settlementsRes.json() : [];
  const bookings = bookingsRes.ok ? await bookingsRes.json() : [];

  // --- Settlements panel ---
  const settlementPanel = document.createElement('div');
  settlementPanel.className = 'table-panel';
  settlementPanel.style.padding = '24px';
  settlementPanel.style.marginBottom = '24px';
  settlementPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Settlements</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-settlement-id">Settlement ID</label>
        <input type="text" id="mkt-settlement-id" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-channel">Channel</label>
        <select id="mkt-channel" class="form-input" style="width: 130px;">
          <option value="Shopify">Shopify</option>
          <option value="Amazon">Amazon</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-total-sale">Total Sale</label>
        <input type="number" id="mkt-total-sale" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-commission">Commission</label>
        <input type="number" id="mkt-commission" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-net-payout">Net Payout</label>
        <input type="number" id="mkt-net-payout" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 180px;">
        <label class="form-label" for="mkt-order-ids">Order IDs (comma-separated)</label>
        <input type="text" id="mkt-order-ids" class="form-input">
      </div>
      <button class="btn btn-primary" id="mkt-reconcile-btn">Reconcile</button>
    </div>
    <div id="mkt-settlement-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Settlement ID</th>
          <th>Channel</th>
          <th>Total Sale</th>
          <th>Commission</th>
          <th>Net Payout</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        ${settlements.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No settlements yet.</td></tr>`
          : settlements.map(s => `
            <tr>
              <td style="font-family: monospace;">${s.code || s.id}</td>
              <td>${s.channel || ''}</td>
              <td>${(s.total_sale ?? 0).toLocaleString()}</td>
              <td>${(s.commission ?? 0).toLocaleString()}</td>
              <td>${(s.net_payout ?? 0).toLocaleString()}</td>
              <td><span class="badge ${s.status === 'Reconciled' ? 'badge-success' : 'badge-warning'}">${s.status}</span></td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(settlementPanel);

  // --- Logistics bookings panel ---
  const bookingPanel = document.createElement('div');
  bookingPanel.className = 'table-panel';
  bookingPanel.style.padding = '24px';
  bookingPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Logistics Bookings</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-order-id">Order ID</label>
        <input type="text" id="mkt-order-id" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-carrier">Carrier</label>
        <input type="text" id="mkt-carrier" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-tracking">Tracking Number</label>
        <input type="text" id="mkt-tracking" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-shipping-charge">Shipping Charge</label>
        <input type="number" id="mkt-shipping-charge" class="form-input" style="width: 130px;">
      </div>
      <button class="btn btn-primary" id="mkt-book-btn">Book</button>
    </div>
    <div id="mkt-booking-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Booking ID</th>
          <th>Order ID</th>
          <th>Carrier</th>
          <th>Tracking Number</th>
          <th>Shipping Charge</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        ${bookings.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No logistics bookings yet.</td></tr>`
          : bookings.map(b => `
            <tr>
              <td style="font-family: monospace;">${b.code || b.id}</td>
              <td>${b.order_id || ''}</td>
              <td>${b.carrier || ''}</td>
              <td>${b.tracking_number || ''}</td>
              <td>${(b.shipping_charge ?? 0).toLocaleString()}</td>
              <td><span class="badge badge-secondary">${b.status}</span></td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(bookingPanel);

  document.getElementById('mkt-reconcile-btn').addEventListener('click', submitMarketplaceReconcile);
  document.getElementById('mkt-book-btn').addEventListener('click', submitLogisticsBooking);
  populateMarketplaceChannelOptions();
}

// Stage 18.3: the Channel select here was a hardcoded Shopify/Amazon
// <option> list, unlike PIM's channel picker (renderPIMPublishSection)
// which fetches the real Channel master. Extends rather than replaces the
// hardcoded pair - appending any real Channel records not already covered -
// so this can't regress to an empty dropdown if no Channel docs exist yet.
async function populateMarketplaceChannelOptions() {
  const select = document.getElementById('mkt-channel');
  if (!select) return;
  const res = await apiFetch('/api/v1/doc/Channel');
  if (!res || !res.ok) return;
  const channels = await res.json();
  const existing = new Set(Array.from(select.options).map(o => o.value));
  channels.forEach(c => {
    const value = c.code || c.id;
    if (!value || existing.has(value)) return;
    const opt = document.createElement('option');
    opt.value = value;
    opt.textContent = c.name || value;
    select.appendChild(opt);
    existing.add(value);
  });
}

async function submitMarketplaceReconcile() {
  const errorEl = document.getElementById('mkt-settlement-error');
  errorEl.classList.add('hidden');

  const settlementId = document.getElementById('mkt-settlement-id').value.trim();
  const channel = document.getElementById('mkt-channel').value;
  const totalSale = parseFloat(document.getElementById('mkt-total-sale').value);
  const commission = parseFloat(document.getElementById('mkt-commission').value) || 0;
  const netPayout = parseFloat(document.getElementById('mkt-net-payout').value) || 0;
  const orderIds = document.getElementById('mkt-order-ids').value.split(',').map(s => s.trim()).filter(Boolean);

  if (!settlementId || !totalSale || totalSale <= 0) {
    errorEl.textContent = 'Settlement ID and a positive Total Sale are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/settlement/reconcile', {
    method: 'POST',
    body: JSON.stringify({
      settlement_id: settlementId,
      channel,
      total_sale: totalSale,
      commission,
      net_payout: netPayout,
      order_ids: orderIds
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Reconciliation failed.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

async function submitLogisticsBooking() {
  const errorEl = document.getElementById('mkt-booking-error');
  errorEl.classList.add('hidden');

  const orderId = document.getElementById('mkt-order-id').value.trim();
  const carrier = document.getElementById('mkt-carrier').value.trim();
  const trackingNumber = document.getElementById('mkt-tracking').value.trim();
  const shippingCharge = parseFloat(document.getElementById('mkt-shipping-charge').value) || 0;

  if (!orderId || !carrier || !trackingNumber) {
    errorEl.textContent = 'Order ID, Carrier, and Tracking Number are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/logistics/book', {
    method: 'POST',
    body: JSON.stringify({
      order_id: orderId,
      carrier,
      tracking_number: trackingNumber,
      shipping_charge: shippingCharge
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Booking failed.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

// Approvals inbox (Stage 13.8) - the checker side of the maker-checker
// engine. Lists every Pending Approval document across all approval-gated
// doctypes (GET /api/v1/approval/pending, already scoped server-side to the
// caller's role/location) with Approve/Reject actions against the already-
// working POST /api/v1/approval/decide.
async function renderApprovalsView(container) {
  const res = await apiFetch('/api/v1/approval/pending');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Approvals</h1>
      <p class="page-subtitle">Documents awaiting your sign-off.</p>
    </div>
  `;
  container.appendChild(header);

  if (!res.ok) {
    const panel = document.createElement('div');
    panel.className = 'table-panel';
    panel.style.padding = '24px';
    panel.textContent = 'Failed to load pending approvals.';
    container.appendChild(panel);
    return;
  }

  const items = await res.json();
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Record Type</th>
          <th>Document ID</th>
          <th>Amount</th>
          <th>Location</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (!items || items.length === 0) {
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">Nothing awaiting approval.</td></tr>`;
  }
  (items || []).forEach(item => {
    const amount = item.total_amount ?? item.amount ?? '';
    const loc = item.location || item.location_code || '';
    html += `
      <tr>
        <td>${item.doctype}</td>
        <td style="font-family: monospace;">${item.id}</td>
        <td>${amount !== '' ? Number(amount).toLocaleString() : ''}</td>
        <td>${loc}</td>
        <td>
          <button class="action-btn" onclick="decideApproval('${item.doctype}', '${item.id}', 'Approved')">Approve</button>
          <button class="action-btn action-btn-danger" onclick="decideApproval('${item.doctype}', '${item.id}', 'Rejected')">Reject</button>
        </td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

async function decideApproval(doctype, documentId, decision) {
  let comment = '';
  if (decision === 'Rejected') {
    comment = (await showCustomPrompt('Reason for rejection (optional):')) || '';
  }
  const res = await apiFetch('/api/v1/approval/decide', {
    method: 'POST',
    body: JSON.stringify({ doctype, document_id: documentId, decision, comment })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record decision.');
    return;
  }
  renderView('approvals');
}

// Purchase Orders screen (Stage 13.8's maker side) - this sidebar item was
// previously a placeholder ("Module Setup Pending"); it's the pilot doctype
// for the approval engine, so a maker needs somewhere to actually create
// and submit one. Deliberately minimal (no line items/RFQ) - full PO
// functional breadth is a separate, larger gap (Stage 13.12).
async function renderPurchaseOrdersView(container) {
  const res = await apiFetch('/api/v1/doc/PurchaseOrder');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Purchase Orders</h1>
      <p class="page-subtitle">Create a PO as Draft, then submit it for approval.</p>
    </div>
  `;
  container.appendChild(header);

  const ordersLoadFailed = !res.ok;
  const orders = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Purchase Order</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-number">PO Number</label>
        <input type="text" id="po-number" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-vendor">Vendor</label>
        <input type="text" id="po-vendor" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-warehouse">Target Warehouse</label>
        <input type="text" id="po-warehouse" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-location">Location</label>
        <input type="text" id="po-location" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-amount">Total Amount (taxable value)</label>
        <input type="number" id="po-amount" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-gst-rate">GST Rate (%)</label>
        <input type="number" id="po-gst-rate" class="form-input" style="width: 90px;" placeholder="e.g. 18">
      </div>
      <div class="form-group" style="margin-bottom: 0; display: flex; align-items: center; gap: 6px; padding-bottom: 8px;">
        <input type="checkbox" id="po-gst-interstate" style="width: auto;">
        <label class="form-label" for="po-gst-interstate" style="margin-bottom: 0;">Interstate</label>
      </div>
      <button class="btn btn-outline" id="po-gst-calc-btn" type="button">Calculate GST</button>
      <button class="btn btn-primary" id="po-create-btn">Create Draft</button>
    </div>
    <div id="po-gst-breakdown" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="po-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  document.getElementById('po-gst-calc-btn').addEventListener('click', calculatePOGst);
  attachTypeahead(document.getElementById('po-vendor'), 'Vendor');
  attachTypeahead(document.getElementById('po-warehouse'), 'Location');
  attachTypeahead(document.getElementById('po-location'), 'Location');

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = ordersLoadFailed
    ? `<p style="padding: 16px; color: #ef4444; font-size: 13px;">Failed to load existing purchase orders.</p>`
    : '';
  html += `
    <table>
      <thead>
        <tr>
          <th>PO Number</th>
          <th>Vendor</th>
          <th>Location</th>
          <th>Total Amount</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (orders.length === 0) {
    html += `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No purchase orders yet.</td></tr>`;
  }
  orders.forEach(po => {
    const statusBadge = po.status === 'Approved' ? 'badge-success'
      : po.status === 'Rejected' ? 'badge-danger'
      : po.status === 'Pending Approval' ? 'badge-warning'
      : 'badge-secondary';
    html += `
      <tr>
        <td style="font-family: monospace;">${po.po_number || po.code || po.id}</td>
        <td>${po.vendor || ''}</td>
        <td>${po.location || ''}</td>
        <td>${(po.total_amount ?? 0).toLocaleString()}</td>
        <td><span class="badge ${statusBadge}">${po.status}</span></td>
        <td>${po.status === 'Draft' ? `<button class="action-btn" onclick="submitPOForApproval('${po.id}')">Submit for Approval</button>` : ''}</td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);

  document.getElementById('po-create-btn').addEventListener('click', createDraftPurchaseOrder);
}

// calculatePOGst calls the real GST engine (Stage 13.10) against whatever
// amount/rate/interstate the maker has entered so far, purely as a helper -
// it doesn't change what total_amount gets saved as (this codebase treats
// total_amount as the taxable value throughout, matching engines.PostDoubleEntry's
// existing accounting; adding a separate tax-liability GL posting is future
// integration work, not part of this item).
async function calculatePOGst() {
  const breakdownEl = document.getElementById('po-gst-breakdown');
  const amount = parseFloat(document.getElementById('po-amount').value);
  const rate = parseFloat(document.getElementById('po-gst-rate').value);
  const interstate = document.getElementById('po-gst-interstate').checked;

  if (isNaN(amount) || isNaN(rate)) {
    breakdownEl.textContent = 'Enter a Total Amount and GST Rate first.';
    return;
  }

  const res = await apiFetch('/api/v1/gst/calculate', {
    method: 'POST',
    body: JSON.stringify({ taxable_amount: amount, gst_rate: rate, interstate })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    breakdownEl.textContent = data.error || 'GST calculation failed.';
    return;
  }
  breakdownEl.innerHTML = interstate
    ? `IGST: <strong>${data.igst.toLocaleString()}</strong> &nbsp;|&nbsp; Total tax: <strong>${data.total_tax.toLocaleString()}</strong> &nbsp;|&nbsp; Total with GST: <strong>${data.total_amount.toLocaleString()}</strong>`
    : `CGST: <strong>${data.cgst.toLocaleString()}</strong> &nbsp;|&nbsp; SGST: <strong>${data.sgst.toLocaleString()}</strong> &nbsp;|&nbsp; Total tax: <strong>${data.total_tax.toLocaleString()}</strong> &nbsp;|&nbsp; Total with GST: <strong>${data.total_amount.toLocaleString()}</strong>`;
}

async function createDraftPurchaseOrder() {
  const errorEl = document.getElementById('po-form-error');
  errorEl.classList.add('hidden');

  const poNumber = document.getElementById('po-number').value.trim();
  const vendor = document.getElementById('po-vendor').value.trim();
  const warehouse = document.getElementById('po-warehouse').value.trim();
  const location = document.getElementById('po-location').value.trim();
  const amount = parseFloat(document.getElementById('po-amount').value) || 0;

  if (!poNumber || !vendor || !warehouse || !location) {
    errorEl.textContent = 'PO Number, Vendor, Target Warehouse, and Location are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // PurchaseOrder has two overlapping field registrations from this
  // project's history (po_number/code, vendor/vendor_id both mandatory) -
  // sending both pairs the same value matches what the one real seeded PO
  // document already does, rather than trying to untangle that mismatch here.
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder`, {
    method: 'POST',
    body: JSON.stringify({
      id: poNumber,
      po_number: poNumber,
      code: poNumber,
      vendor,
      vendor_id: vendor,
      target_warehouse: warehouse,
      location,
      total_amount: amount,
      items: '[]',
      status: 'Draft'
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to create purchase order.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('purchase-orders');
}

async function submitPOForApproval(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'PurchaseOrder', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  renderView('purchase-orders');
}

// Report catalog (Stage 13.11) - Current Stock, Sales Register, Vendor
// Ledger, Payables Ageing, the four reports the gap analysis prioritized.
let currentReportTab = 'current-stock';

const REPORT_TABS = [
  { id: 'current-stock', label: 'Current Stock' },
  { id: 'sales-register', label: 'Sales Register' },
  { id: 'vendor-ledger', label: 'Vendor Ledger' },
  { id: 'payables-ageing', label: 'Payables Ageing' },
  { id: 'receivables-ageing', label: 'Receivables Ageing' },
  { id: 'gst-return-summary', label: 'GST Return Summary' },
  { id: 'report-catalog', label: 'Report Catalog' }
];

async function renderReportsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Reports</h1>
      <p class="page-subtitle">Current Stock, Sales Register, Vendor Ledger, Payables/Receivables Ageing, and GST Return Summary.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = REPORT_TABS.map(t =>
    `<button class="btn ${t.id === currentReportTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-report-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-report-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentReportTab = btn.getAttribute('data-report-tab');
      renderView('reports');
    });
  });

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  container.appendChild(panel);

  if (currentReportTab === 'current-stock') {
    await renderCurrentStockReport(panel);
  } else if (currentReportTab === 'sales-register') {
    await renderSalesRegisterReport(panel);
  } else if (currentReportTab === 'vendor-ledger') {
    await renderVendorLedgerReport(panel);
  } else if (currentReportTab === 'payables-ageing') {
    await renderPayablesAgeingReport(panel);
  } else if (currentReportTab === 'receivables-ageing') {
    await renderReceivablesAgeingReport(panel);
  } else if (currentReportTab === 'gst-return-summary') {
    await renderGSTReturnSummaryReport(panel);
  } else if (currentReportTab === 'report-catalog') {
    await renderReportCatalogPanel(panel);
  }
}

// Inventory (Stage 21 QA fix): "Inventory" routed to a view name the router
// had no case for, always falling through to the "Module Setup Pending"
// mock screen - despite USER_GUIDE.md §5 explicitly documenting a working
// search-by-item stock screen. Reuses the same /api/v1/reports/current-stock
// endpoint Reports > Current Stock already calls (no new backend), but adds
// the client-side search box that endpoint's own report tab never had.
let inventorySearchQuery = '';
async function renderInventoryView(container) {
  const res = await apiFetch('/api/v1/reports/current-stock');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load inventory.', () => renderView('inventory')); return; }
  const rows = await res.json();
  inventorySearchQuery = '';

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Inventory</h1>
      <p class="page-subtitle">How much stock you have right now, and how much is actually free to sell (already-reserved stock excluded).</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        <input type="text" id="inventory-search-input" placeholder="Search by SKU or location...">
      </div>
    </div>
    <div class="table-wrapper" id="inventory-table-wrapper"></div>
  `;
  container.appendChild(panel);

  // Only the table body redraws on each keystroke - the search input itself
  // stays untouched so it doesn't lose focus/cursor position while typing
  // (matches renderDocTable()'s existing #doc-table-wrapper pattern).
  function draw() {
    const wrapper = document.getElementById('inventory-table-wrapper');
    const filtered = inventorySearchQuery
      ? rows.filter(r => `${r.sku} ${r.location_code}`.toLowerCase().includes(inventorySearchQuery))
      : rows;
    let html = `
      <table>
        <thead><tr><th>SKU</th><th>Location</th><th>On Hand</th><th>Available</th><th>Committed</th><th>Reserved</th><th>Safety Stock</th></tr></thead>
        <tbody>
    `;
    html += filtered.length === 0
      ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No inventory records.</td></tr>`
      : filtered.map(r => `
          <tr>
            <td style="font-family: monospace;">${r.sku}</td>
            <td>${r.location_code}</td>
            <td>${r.on_hand}</td>
            <td>${r.available}</td>
            <td>${r.committed}</td>
            <td>${r.reserved}</td>
            <td>${r.safety_stock}</td>
          </tr>
        `).join('');
    html += `</tbody></table>`;
    wrapper.innerHTML = html;
  }
  draw();
  document.getElementById('inventory-search-input').addEventListener('input', (e) => {
    inventorySearchQuery = e.target.value.toLowerCase();
    draw();
  });
}

async function renderCurrentStockReport(panel) {
  const res = await apiFetch('/api/v1/reports/current-stock');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>SKU</th><th>Location</th><th>On Hand</th><th>Available</th><th>Committed</th><th>Reserved</th><th>Safety Stock</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No inventory records.</td></tr>`
    : rows.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.sku}</td>
          <td>${r.location_code}</td>
          <td>${r.on_hand}</td>
          <td>${r.available}</td>
          <td>${r.committed}</td>
          <td>${r.reserved}</td>
          <td>${r.safety_stock}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderSalesRegisterReport(panel) {
  const res = await apiFetch('/api/v1/reports/sales-register');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>Cart Number</th><th>Location</th><th>Payment Mode</th><th>Status</th><th>Sale Total</th><th>Date</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No completed sales yet.</td></tr>`
    : rows.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.cart_number}</td>
          <td>${r.location}</td>
          <td>${r.payment_mode}</td>
          <td><span class="badge badge-success">${r.status}</span></td>
          <td>${r.sale_total.toLocaleString()}</td>
          <td>${new Date(r.created_at).toLocaleString()}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderVendorLedgerReport(panel) {
  const res = await apiFetch('/api/v1/reports/vendor-ledger');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];
  let html = `
    <table>
      <thead><tr><th>Vendor</th><th>PO Number</th><th>Total Amount</th><th>Status</th><th>Date</th></tr></thead>
      <tbody>
  `;
  html += rows.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No purchase orders yet.</td></tr>`
    : rows.map(r => `
        <tr>
          <td>${r.vendor || ''}</td>
          <td style="font-family: monospace;">${r.po_number || r.id}</td>
          <td>${(r.total_amount ?? 0).toLocaleString()}</td>
          <td>${r.status}</td>
          <td>${new Date(r.created_at).toLocaleString()}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  panel.innerHTML = html;
}

async function renderPayablesAgeingReport(panel) {
  const res = await apiFetch('/api/v1/reports/payables-ageing');
  if (!res) return;
  const buckets = res.ok ? await res.json() : [];
  panel.innerHTML = `
    <p style="padding: 16px 16px 0; font-size: 13px; color: var(--text-muted);">
      Buckets Approved-but-not-yet-Closed purchase orders by age since creation.
    </p>
    <table>
      <thead><tr><th>Age Bucket</th><th>PO Count</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${b.amount.toLocaleString()}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

async function renderReceivablesAgeingReport(panel) {
  const res = await apiFetch('/api/v1/reports/receivables-ageing');
  if (!res) return;
  const buckets = res.ok ? await res.json() : [];
  panel.innerHTML = `
    <p style="padding: 16px 16px 0; font-size: 13px; color: var(--text-muted);">
      Buckets Approved-but-not-yet-Paid sales invoices (Finance &gt; Sales Invoices) by age since creation.
    </p>
    <table>
      <thead><tr><th>Age Bucket</th><th>Invoice Count</th><th>Outstanding Amount</th></tr></thead>
      <tbody>
        ${buckets.map(b => `
          <tr>
            <td>${b.bucket}</td>
            <td>${b.count}</td>
            <td>${b.amount.toLocaleString()}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

// GST Return Summary (Stage 20.29): report-only GSTR-1/3B-shaped
// aggregation, explicitly not e-filing/IRN. Defaults to the current
// calendar month since a GST return is always filed for a specific period.
async function renderGSTReturnSummaryReport(panel) {
  const now = new Date();
  const monthStart = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`;
  const today = now.toISOString().slice(0, 10);
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; padding: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="gst-start-date">From</label>
        <input type="date" id="gst-start-date" class="form-input" value="${monthStart}">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="gst-end-date">To</label>
        <input type="date" id="gst-end-date" class="form-input" value="${today}">
      </div>
      <button class="btn btn-primary" id="gst-summary-btn">Run</button>
    </div>
    <p style="padding: 0 16px; font-size: 13px; color: var(--text-muted);">
      Report-only summary of output tax already calculated per-transaction - not e-invoice/IRN filing.
    </p>
    <div id="gst-summary-result" style="padding: 0 16px 16px;"></div>
  `;
  const runReport = async () => {
    const startDate = document.getElementById('gst-start-date').value;
    const endDate = document.getElementById('gst-end-date').value;
    const resultEl = document.getElementById('gst-summary-result');
    const res = await apiFetch(`/api/v1/reports/gst-return-summary?start=${startDate}&end=${endDate}`);
    if (!res) return;
    if (!res.ok) {
      resultEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Failed to load GST return summary.')}</p>`;
      return;
    }
    const s = await res.json();
    resultEl.innerHTML = `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Taxable Value</span><span class="stat-val">${s.taxable_value.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output CGST</span><span class="stat-val">${s.output_cgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output SGST</span><span class="stat-val">${s.output_sgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output IGST</span><span class="stat-val">${s.output_igst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Total Tax Liability</span><span class="stat-val">${s.total_tax_liability.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Transactions</span><span class="stat-val">${s.transaction_count}</span></div>
      </div>
    `;
  };
  document.getElementById('gst-summary-btn').addEventListener('click', runReport);
  await runReport();
}

// Report Catalog (Stage 20 Track B.4, 20.35-20.40): ONE generic panel
// driving every report in engines/report_registry.go's catalog - old (the
// 6 tabs above) and new alike, via GET /api/v1/reports/catalog's metadata.
// Adding a future report from here on means registering a Go function, not
// writing a new render function like the tabs above each needed. Saved
// filters (20.36) reuse the generic ReportFilterPreset doctype directly -
// no dedicated save/list endpoint exists or is needed. Async export
// (20.37) and drill-down (20.38) are both generic too, driven by
// has_drill_down/columns metadata rather than per-report frontend code.
let reportCatalogDefs = [];
let reportCatalogSelectedId = '';

async function renderReportCatalogPanel(panel) {
  const res = await apiFetch('/api/v1/reports/catalog');
  if (!res) return;
  if (!res.ok) {
    panel.innerHTML = `<p class="login-error" style="padding:16px;">Failed to load report catalog.</p>`;
    return;
  }
  reportCatalogDefs = await res.json();
  if (!reportCatalogSelectedId && reportCatalogDefs.length > 0) {
    reportCatalogSelectedId = reportCatalogDefs[0].id;
  }

  const byCategory = {};
  reportCatalogDefs.forEach(d => {
    const cat = d.category || 'Other';
    (byCategory[cat] = byCategory[cat] || []).push(d);
  });

  panel.innerHTML = `
    <div style="padding: 16px; display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; border-bottom: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0; min-width: 220px;">
        <label class="form-label" for="rc-report-select">Report</label>
        <select id="rc-report-select" class="form-select">
          ${Object.keys(byCategory).sort().map(cat => `
            <optgroup label="${cat}">
              ${byCategory[cat].map(d => `<option value="${d.id}" ${d.id === reportCatalogSelectedId ? 'selected' : ''}>${d.label}</option>`).join('')}
            </optgroup>
          `).join('')}
        </select>
      </div>
      <div id="rc-params" style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;"></div>
      <div class="form-group" style="margin-bottom: 0; min-width: 180px;">
        <label class="form-label" for="rc-saved-filter">Saved Filter</label>
        <select id="rc-saved-filter" class="form-select"><option value="">— none —</option></select>
      </div>
      <button class="btn btn-primary" id="rc-run-btn">Run</button>
      <button class="btn btn-outline" id="rc-save-filter-btn">Save Filter</button>
      <button class="btn btn-outline" id="rc-export-btn">Export in Background</button>
    </div>
    <div id="rc-export-status" style="padding: 0 16px;"></div>
    <div id="rc-results" style="padding: 16px;"></div>
  `;

  document.getElementById('rc-report-select').addEventListener('change', (e) => {
    reportCatalogSelectedId = e.target.value;
    renderReportCatalogParams();
    loadReportCatalogSavedFilters();
  });
  document.getElementById('rc-run-btn').addEventListener('click', runReportCatalogReport);
  document.getElementById('rc-save-filter-btn').addEventListener('click', saveReportCatalogFilter);
  document.getElementById('rc-export-btn').addEventListener('click', exportReportCatalogReport);
  document.getElementById('rc-saved-filter').addEventListener('change', (e) => {
    applyReportCatalogSavedFilter(e.target.value);
  });

  renderReportCatalogParams();
  await loadReportCatalogSavedFilters();
}

function currentReportCatalogDef() {
  return reportCatalogDefs.find(d => d.id === reportCatalogSelectedId);
}

function renderReportCatalogParams() {
  const def = currentReportCatalogDef();
  const container = document.getElementById('rc-params');
  if (!def || !container) return;
  container.innerHTML = (def.params || []).map(p => `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label" for="rc-param-${p.key}">${p.label}</label>
      <input type="${p.type === 'date' ? 'date' : 'text'}" id="rc-param-${p.key}" class="form-input"
             data-param-key="${p.key}" ${p.required ? 'required' : ''} style="width: 160px;">
    </div>
  `).join('');
}

function collectReportCatalogParams() {
  const params = {};
  document.querySelectorAll('#rc-params [data-param-key]').forEach(input => {
    if (input.value) params[input.getAttribute('data-param-key')] = input.value;
  });
  return params;
}

function reportCatalogQueryString(params) {
  return Object.entries(params).map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&');
}

async function runReportCatalogReport() {
  const def = currentReportCatalogDef();
  const resultsEl = document.getElementById('rc-results');
  if (!def || !resultsEl) return;
  const params = collectReportCatalogParams();
  for (const p of (def.params || [])) {
    if (p.required && !params[p.key]) {
      resultsEl.innerHTML = `<p class="login-error">"${p.label}" is required.</p>`;
      return;
    }
  }
  const res = await apiFetch(`/api/v1/reports/run/${def.id}?${reportCatalogQueryString(params)}`);
  if (!res) return;
  if (!res.ok) {
    resultsEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Failed to run report.')}</p>`;
    return;
  }
  const result = await res.json();
  renderReportCatalogResultTable(resultsEl, result, params);
}

function renderReportCatalogResultTable(container, result, params) {
  const columns = result.columns || [];
  const rows = result.rows || [];
  const drillKey = columns.length > 0 ? columns[0].key : null;
  let html = `<table><thead><tr>`;
  columns.forEach(c => { html += `<th>${c.label}</th>`; });
  if (result.has_drill_down) html += `<th>Details</th>`;
  html += `</tr></thead><tbody>`;
  if (rows.length === 0) {
    html += `<tr><td colspan="${columns.length + (result.has_drill_down ? 1 : 0)}" style="text-align:center; color:var(--text-muted);">No rows.</td></tr>`;
  }
  rows.forEach((row, idx) => {
    html += `<tr>`;
    columns.forEach(c => {
      const val = row[c.key];
      html += `<td>${val === null || val === undefined ? '' : val}</td>`;
    });
    if (result.has_drill_down) {
      const rowKeyVal = drillKey ? String(row[drillKey]) : '';
      html += `<td><button class="action-btn" onclick='runReportCatalogDrillDown(${JSON.stringify(result.id)}, ${JSON.stringify(rowKeyVal)}, ${idx})'>View Details</button></td>`;
    }
    html += `</tr><tr id="rc-drilldown-${idx}" class="hidden"><td colspan="${columns.length + 1}"></td></tr>`;
  });
  html += `</tbody></table>`;
  container.innerHTML = html;
  container.dataset.params = JSON.stringify(params);
}

async function runReportCatalogDrillDown(reportId, rowKey, rowIdx) {
  const params = JSON.parse(document.getElementById('rc-results').dataset.params || '{}');
  const res = await apiFetch(`/api/v1/reports/drilldown/${reportId}?row=${encodeURIComponent(rowKey)}&${reportCatalogQueryString(params)}`);
  if (!res) return;
  const targetRow = document.getElementById(`rc-drilldown-${rowIdx}`);
  if (!targetRow) return;
  if (!res.ok) {
    targetRow.classList.remove('hidden');
    targetRow.querySelector('td').innerHTML = `<span class="login-error">${await getErrorMessage(res, 'Drill-down failed.')}</span>`;
    return;
  }
  const data = await res.json();
  const drillRows = data.rows || [];
  const keys = drillRows.length > 0 ? Object.keys(drillRows[0]) : [];
  let inner = `<div style="padding:8px 0;"><table style="width:100%;"><thead><tr>${keys.map(k => `<th>${k}</th>`).join('')}</tr></thead><tbody>`;
  inner += drillRows.length === 0
    ? `<tr><td colspan="${keys.length || 1}" style="text-align:center; color:var(--text-muted);">No underlying rows.</td></tr>`
    : drillRows.map(r => `<tr>${keys.map(k => `<td>${r[k] === null || r[k] === undefined ? '' : r[k]}</td>`).join('')}</tr>`).join('');
  inner += `</tbody></table></div>`;
  targetRow.classList.remove('hidden');
  targetRow.querySelector('td').innerHTML = inner;
}

async function loadReportCatalogSavedFilters() {
  const select = document.getElementById('rc-saved-filter');
  if (!select) return;
  select.innerHTML = `<option value="">— none —</option>`;
  const res = await apiFetch('/api/v1/doc/ReportFilterPreset');
  if (!res || !res.ok) return;
  const presets = await res.json();
  const username = localStorage.getItem('erp_username') || '';
  presets
    .filter(p => p.report_id === reportCatalogSelectedId && p.owner === username)
    .forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = p.name;
      select.appendChild(opt);
    });
}

function applyReportCatalogSavedFilter(presetId) {
  if (!presetId) return;
  apiFetch(`/api/v1/doc/ReportFilterPreset/${presetId}`).then(async (res) => {
    if (!res || !res.ok) return;
    const preset = await res.json();
    let params = {};
    try { params = JSON.parse(preset.params || '{}'); } catch (e) { /* ignore */ }
    Object.entries(params).forEach(([k, v]) => {
      const input = document.querySelector(`#rc-params [data-param-key="${k}"]`);
      if (input) input.value = v;
    });
  });
}

async function saveReportCatalogFilter() {
  const def = currentReportCatalogDef();
  if (!def) return;
  const name = await showCustomPrompt('Name this saved filter:', '', 'Save Filter');
  if (!name) return;
  const params = collectReportCatalogParams();
  const username = localStorage.getItem('erp_username') || '';
  const presetId = `RFP-${Date.now()}`;
  const res = await apiFetch('/api/v1/doc/ReportFilterPreset', {
    method: 'POST',
    body: JSON.stringify({
      id: presetId, report_id: def.id, name, owner: username,
      params: JSON.stringify(params), status: 'Active'
    })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to save filter.');
    return;
  }
  await loadReportCatalogSavedFilters();
  document.getElementById('rc-saved-filter').value = presetId;
}

async function exportReportCatalogReport() {
  const def = currentReportCatalogDef();
  const statusEl = document.getElementById('rc-export-status');
  if (!def || !statusEl) return;
  const params = collectReportCatalogParams();
  const res = await apiFetch('/api/v1/reports/export', {
    method: 'POST',
    body: JSON.stringify({ report_id: def.id, params })
  });
  if (!res) return;
  if (!res.ok) {
    statusEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Failed to queue export.')}</p>`;
    return;
  }
  const job = await res.json();
  statusEl.innerHTML = `<p>Export queued (job ${job.id})... waiting for it to complete.</p>`;
  pollReportExportJob(job.id, statusEl);
}

async function pollReportExportJob(jobId, statusEl) {
  const res = await apiFetch(`/api/v1/reports/export/${jobId}`);
  if (!res) return;
  if (!res.ok) {
    statusEl.innerHTML = `<p class="login-error">${await getErrorMessage(res, 'Export job lookup failed.')}</p>`;
    return;
  }
  const job = await res.json();
  if (job.status === 'Pending') {
    setTimeout(() => pollReportExportJob(jobId, statusEl), 2000);
    return;
  }
  if (job.status === 'Failed') {
    statusEl.innerHTML = `<p class="login-error">Export failed.</p>`;
    return;
  }
  statusEl.innerHTML = `<p><button class="action-btn" id="rc-download-btn">Download CSV</button></p>`;
  document.getElementById('rc-download-btn').addEventListener('click', () => downloadReportExportCSV(jobId));
}

// This endpoint requires the same Bearer-token auth as every other API call
// (apiMiddleware has no query-string-token fallback), so a plain <a href>
// opened in a new tab can't authenticate itself - fetch the CSV through the
// normal authenticated apiFetch() and hand the browser a Blob URL instead.
async function downloadReportExportCSV(jobId) {
  const res = await apiFetch(`/api/v1/reports/export/${jobId}?download=1`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to download export.');
    return;
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${jobId}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// RFQ / Vendor Quote / Quote Comparison (Stage 13.12) - RFQ/VendorQuote
// creation and listing use the same generic doc API as Vendor/Customer
// (Stage 13.9); this screen adds the comparison view and winner-selection
// action on top, which the generic endpoint doesn't provide.
let selectedRFQId = '';

async function renderRFQView(container) {
  const res = await apiFetch('/api/v1/doc/RFQ');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">RFQ / Quotes</h1>
      <p class="page-subtitle">Request quotes from vendors and compare them before creating a Purchase Order.</p>
    </div>
  `;
  container.appendChild(header);

  const rfqs = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New RFQ</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="rfq-code">RFQ Number</label>
        <input type="text" id="rfq-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 200px;">
        <label class="form-label" for="rfq-description">Item / Requirement Description</label>
        <input type="text" id="rfq-description" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="rfq-quantity">Quantity</label>
        <input type="number" id="rfq-quantity" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="rfq-target-date">Target Date</label>
        <input type="date" id="rfq-target-date" class="form-input">
      </div>
      <button class="btn btn-primary" id="rfq-create-btn">Create RFQ</button>
    </div>
    <div id="rfq-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  listPanel.style.marginBottom = '24px';
  let listHtml = `
    <table>
      <thead><tr><th>RFQ Number</th><th>Description</th><th>Quantity</th><th>Target Date</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  listHtml += rfqs.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No RFQs yet.</td></tr>`
    : rfqs.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.description || ''}</td>
          <td>${r.quantity ?? ''}</td>
          <td>${r.target_date || ''}</td>
          <td><span class="badge ${r.status === 'Closed' ? 'badge-success' : 'badge-secondary'}">${r.status}</span></td>
          <td><button class="action-btn" onclick="viewRFQQuotes('${r.id}')">View Quotes</button></td>
        </tr>
      `).join('');
  listHtml += `</tbody></table>`;
  listPanel.innerHTML = listHtml;
  container.appendChild(listPanel);

  document.getElementById('rfq-create-btn').addEventListener('click', createRFQ);

  if (selectedRFQId) {
    const quotesContainer = document.createElement('div');
    quotesContainer.id = 'rfq-quotes-container';
    container.appendChild(quotesContainer);
    await renderRFQQuotesPanel(quotesContainer, selectedRFQId, rfqs.find(r => r.id === selectedRFQId));
  }
}

async function createRFQ() {
  const errorEl = document.getElementById('rfq-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('rfq-code').value.trim();
  const description = document.getElementById('rfq-description').value.trim();
  const quantity = parseFloat(document.getElementById('rfq-quantity').value) || 0;
  const targetDate = document.getElementById('rfq-target-date').value;

  if (!code || !description || !quantity) {
    errorEl.textContent = 'RFQ Number, Description, and Quantity are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/RFQ', {
    method: 'POST',
    body: JSON.stringify({ id: code, code, description, quantity, target_date: targetDate, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create RFQ.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('rfq');
}

function viewRFQQuotes(rfqId) {
  selectedRFQId = rfqId;
  renderView('rfq');
}

async function renderRFQQuotesPanel(container, rfqId, rfq) {
  const res = await apiFetch(`/api/v1/rfq/quotes?rfq_id=${encodeURIComponent(rfqId)}`);
  if (!res) return;
  const quotes = res.ok ? await res.json() : [];
  const isClosed = rfq && rfq.status === 'Closed';

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Quotes for ${rfqId}</h2>
    ${isClosed ? '' : `
      <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 20px;">
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-code">Quote Number</label>
          <input type="text" id="quote-code" class="form-input" style="width: 150px;">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-vendor">Vendor</label>
          <input type="text" id="quote-vendor" class="form-input" style="width: 160px;">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-price">Quoted Price</label>
          <input type="number" id="quote-price" class="form-input" style="width: 130px;">
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="quote-lead-time">Lead Time (days)</label>
          <input type="number" id="quote-lead-time" class="form-input" style="width: 130px;">
        </div>
        <button class="btn btn-primary" id="quote-submit-btn">Submit Quote</button>
      </div>
      <div id="quote-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    `}
    <table>
      <thead><tr><th>Quote Number</th><th>Vendor</th><th>Quoted Price</th><th>Lead Time (days)</th><th>Status</th><th></th></tr></thead>
      <tbody>
        ${quotes.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No quotes submitted yet.</td></tr>`
          : quotes.map(q => `
            <tr>
              <td style="font-family: monospace;">${q.code || q.id}</td>
              <td>${q.vendor || ''}</td>
              <td>${(q.quoted_price ?? 0).toLocaleString()}</td>
              <td>${q.lead_time_days ?? ''}</td>
              <td><span class="badge ${q.status === 'Selected' ? 'badge-success' : q.status === 'Rejected' ? 'badge-danger' : 'badge-secondary'}">${q.status}</span></td>
              <td>${!isClosed && q.status === 'Submitted' ? `<button class="action-btn" onclick="selectWinningQuote('${rfqId}', '${q.id}')">Select as Winner</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);

  const submitBtn = document.getElementById('quote-submit-btn');
  if (submitBtn) submitBtn.addEventListener('click', () => submitVendorQuote(rfqId));
  const quoteVendorInput = document.getElementById('quote-vendor');
  if (quoteVendorInput) attachTypeahead(quoteVendorInput, 'Vendor');
}

async function submitVendorQuote(rfqId) {
  const errorEl = document.getElementById('quote-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('quote-code').value.trim();
  const vendor = document.getElementById('quote-vendor').value.trim();
  const quotedPrice = parseFloat(document.getElementById('quote-price').value);
  const leadTime = parseFloat(document.getElementById('quote-lead-time').value) || 0;

  if (!code || !vendor || !quotedPrice) {
    errorEl.textContent = 'Quote Number, Vendor, and Quoted Price are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/VendorQuote', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, rfq_id: rfqId, vendor,
      quoted_price: quotedPrice, lead_time_days: leadTime, status: 'Submitted'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit quote.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('rfq');
}

async function selectWinningQuote(rfqId, quoteId) {
  const confirmed = await showCustomConfirm('This will mark this quote as the winner, reject all other quotes, and close the RFQ. Continue?', 'Select Winning Quote');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/rfq/select-quote', {
    method: 'POST',
    body: JSON.stringify({ rfq_id: rfqId, quote_id: quoteId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to select winning quote.', 'Selection Failed');
    return;
  }
  renderView('rfq');
}

// Sticker / Barcode Printing (Stage 13.15) - Printer master creation/listing
// use the same generic doc API as Vendor/Customer/RFQ; this screen adds the
// print action (logs history, then renders a printable label sheet) and
// print-history view on top. Labels show the barcode value as clear text
// rather than a generated scannable barcode symbol/image - correctly
// implementing and verifying a real barcode symbology renderer isn't
// something that can be validated without a physical scanner in this
// environment, and shipping an unverified fake one would be worse than a
// clear text label (which is also how the rest of this app already treats
// barcodes - typed/scanned as text, e.g. the POS screen's SKU input).
let stickerSKUs = [];

async function renderStickersView(container) {
  const [printersRes, historyRes] = await Promise.all([
    apiFetch('/api/v1/doc/Printer'),
    apiFetch('/api/v1/stickers/history')
  ]);
  if (!printersRes || !historyRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Sticker Printing</h1>
      <p class="page-subtitle">Print item labels (barcode, name, HSN) and track print history.</p>
    </div>
  `;
  container.appendChild(header);

  const printers = printersRes.ok ? await printersRes.json() : [];
  const history = historyRes.ok ? await historyRes.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-printer">Printer</label>
        <select id="sticker-printer" class="form-input" style="width: 200px;">
          <option value="">Select a printer</option>
          ${printers.map(p => `<option value="${p.code || p.id}">${p.name || p.code || p.id}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="sticker-copies">Copies per SKU</label>
        <input type="number" id="sticker-copies" class="form-input" style="width: 100px;" value="1" min="1">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 180px;">
        <label class="form-label" for="sticker-reprint-reason">Reprint Reason (optional)</label>
        <input type="text" id="sticker-reprint-reason" class="form-input">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 16px;">
      <div class="form-group" style="flex: 1; margin-bottom: 0;">
        <label class="form-label" for="sticker-sku-input">Scan or Enter SKU</label>
        <input type="text" id="sticker-sku-input" class="form-input" placeholder="Barcode / SKU, then Enter" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="sticker-add-btn">Add</button>
    </div>
    <div id="sticker-sku-list" style="margin-bottom: 16px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="sticker-form-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <button class="btn btn-primary" id="sticker-print-btn">Print Stickers</button>
  `;
  container.appendChild(formPanel);

  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  let historyHtml = `
    <table>
      <thead><tr><th>SKU</th><th>Barcode</th><th>Printer</th><th>Printed By</th><th>Copies</th><th>Reprint Reason</th><th>Date</th></tr></thead>
      <tbody>
  `;
  historyHtml += history.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No print history yet.</td></tr>`
    : history.map(h => `
        <tr>
          <td style="font-family: monospace;">${h.sku}</td>
          <td style="font-family: monospace;">${h.barcode}</td>
          <td>${h.printer_code}</td>
          <td>${h.printed_by}</td>
          <td>${h.copies}</td>
          <td>${h.reprint_reason || ''}</td>
          <td>${new Date(h.printed_at).toLocaleString()}</td>
        </tr>
      `).join('');
  historyHtml += `</tbody></table>`;
  historyPanel.innerHTML = historyHtml;
  container.appendChild(historyPanel);

  document.getElementById('sticker-add-btn').addEventListener('click', addStickerSKU);
  document.getElementById('sticker-sku-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addStickerSKU();
    }
  });
  document.getElementById('sticker-print-btn').addEventListener('click', printStickers);

  renderStickerSKUList();
}

function addStickerSKU() {
  const input = document.getElementById('sticker-sku-input');
  const sku = input.value.trim();
  if (!sku) return;
  if (!stickerSKUs.includes(sku)) stickerSKUs.push(sku);
  input.value = '';
  input.focus();
  renderStickerSKUList();
}

function removeStickerSKU(sku) {
  stickerSKUs = stickerSKUs.filter(s => s !== sku);
  renderStickerSKUList();
}

function renderStickerSKUList() {
  const listEl = document.getElementById('sticker-sku-list');
  if (!listEl) return;
  listEl.innerHTML = stickerSKUs.length === 0
    ? 'No SKUs added yet.'
    : stickerSKUs.map(sku => `${sku} <button class="action-btn action-btn-danger" style="padding: 2px 8px;" onclick="removeStickerSKU('${sku}')">x</button>`).join(' &nbsp; ');
}

async function printStickers() {
  const errorEl = document.getElementById('sticker-form-error');
  errorEl.classList.add('hidden');

  const printerCode = document.getElementById('sticker-printer').value;
  const copies = parseInt(document.getElementById('sticker-copies').value, 10) || 1;
  const reprintReason = document.getElementById('sticker-reprint-reason').value.trim();

  if (!printerCode) {
    errorEl.textContent = 'Select a printer first.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (stickerSKUs.length === 0) {
    errorEl.textContent = 'Add at least one SKU first.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/stickers/print', {
    method: 'POST',
    body: JSON.stringify({ skus: stickerSKUs, printer_code: printerCode, reprint_reason: reprintReason, copies })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to print stickers.';
    errorEl.classList.remove('hidden');
    return;
  }

  renderPrintSheet(data, copies);
  stickerSKUs = [];
  renderView('stickers');
}

function renderPrintSheet(labels, copies) {
  const area = document.getElementById('sticker-print-area');
  let html = '';
  labels.forEach(label => {
    for (let i = 0; i < copies; i++) {
      html += `
        <div class="sticker-label">
          <div class="sticker-name">${label.name || label.sku}</div>
          <div class="sticker-barcode">${label.barcode}</div>
          <div class="sticker-meta">SKU: ${label.sku}${label.hsn_code ? ' &nbsp;|&nbsp; HSN: ' + label.hsn_code : ''}</div>
        </div>
      `;
    }
  });
  area.innerHTML = html;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// HR Foundation (Stage 13.13a, MB 16.3) - Employee is a Master-type doctype
// so it already gets a full CRUD screen for free under Master Definition;
// this screen covers Attendance, Leave, and the Payroll Export, which
// aren't master data and need their own UI.
let currentHRTab = 'attendance';
const HR_TABS = [
  { id: 'attendance', label: 'Attendance' },
  { id: 'leave', label: 'Leave' },
  { id: 'payroll-export', label: 'Payroll Export' }
];

async function renderHRView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">HR</h1>
      <p class="page-subtitle">Attendance, leave, and payroll export. Manage employees under Setup.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = HR_TABS.map(t =>
    `<button class="btn ${t.id === currentHRTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-hr-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-hr-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentHRTab = btn.getAttribute('data-hr-tab');
      renderView('hr');
    });
  });

  const employeesRes = await apiFetch('/api/v1/doc/Employee');
  const employees = employeesRes && employeesRes.ok ? await employeesRes.json() : [];

  if (currentHRTab === 'attendance') {
    await renderAttendanceTab(container, employees);
  } else if (currentHRTab === 'leave') {
    await renderLeaveTab(container, employees);
  } else if (currentHRTab === 'payroll-export') {
    renderPayrollExportTab(container);
  }
}

function employeeOptions(employees) {
  return employees.map(e => `<option value="${e.code || e.id}">${e.code || e.id} - ${e.name || ''}</option>`).join('');
}

async function renderAttendanceTab(container, employees) {
  const res = await apiFetch('/api/v1/doc/Attendance');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Mark Attendance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-code">Attendance Code</label>
        <input type="text" id="att-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-employee">Employee</label>
        <select id="att-employee" class="form-input" style="width: 200px;">
          <option value="">Select employee</option>
          ${employeeOptions(employees)}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-date">Date</label>
        <input type="date" id="att-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-location">Location</label>
        <input type="text" id="att-location" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="att-status">Status</label>
        <select id="att-status" class="form-input" style="width: 130px;">
          <option value="Present">Present</option>
          <option value="Absent">Absent</option>
          <option value="Late">Late</option>
          <option value="Leave">Leave</option>
          <option value="Holiday">Holiday</option>
          <option value="WeeklyOff">WeeklyOff</option>
        </select>
      </div>
      <button class="btn btn-primary" id="att-save-btn">Save</button>
    </div>
    <div id="att-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Date</th><th>Location</th><th>Status</th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No attendance records yet.</td></tr>`
    : records.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.employee_id || ''}</td>
          <td>${r.date || ''}</td>
          <td>${r.location || ''}</td>
          <td><span class="badge ${r.status === 'Present' ? 'badge-success' : r.status === 'Absent' ? 'badge-danger' : 'badge-secondary'}">${r.status}</span></td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('att-save-btn').addEventListener('click', saveAttendance);
  attachTypeahead(document.getElementById('att-location'), 'Location');
}

async function saveAttendance() {
  const errorEl = document.getElementById('att-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('att-code').value.trim();
  const employeeId = document.getElementById('att-employee').value;
  const date = document.getElementById('att-date').value;
  const location = document.getElementById('att-location').value.trim();
  const status = document.getElementById('att-status').value;

  if (!code || !employeeId || !date) {
    errorEl.textContent = 'Attendance Code, Employee, and Date are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Attendance', {
    method: 'POST',
    body: JSON.stringify({ id: code, code, employee_id: employeeId, date, location, status })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to save attendance.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function renderLeaveTab(container, employees) {
  const res = await apiFetch('/api/v1/doc/Leave');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Apply Leave</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-code">Leave Code</label>
        <input type="text" id="leave-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-employee">Employee</label>
        <select id="leave-employee" class="form-input" style="width: 200px;">
          <option value="">Select employee</option>
          ${employeeOptions(employees)}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-type">Leave Type</label>
        <select id="leave-type" class="form-input" style="width: 130px;">
          <option value="Casual">Casual</option>
          <option value="Sick">Sick</option>
          <option value="Earned">Earned</option>
          <option value="Unpaid">Unpaid</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-from">From Date</label>
        <input type="date" id="leave-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-to">To Date</label>
        <input type="date" id="leave-to" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="leave-days">Days</label>
        <input type="number" id="leave-days" class="form-input" style="width: 90px;" min="1">
      </div>
      <button class="btn btn-primary" id="leave-save-btn">Apply</button>
    </div>
    <div id="leave-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Code</th><th>Employee</th><th>Type</th><th>From</th><th>To</th><th>Days</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += records.length === 0
    ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No leave applications yet.</td></tr>`
    : records.map(r => `
        <tr>
          <td style="font-family: monospace;">${r.code || r.id}</td>
          <td>${r.employee_id || ''}</td>
          <td>${r.leave_type || ''}</td>
          <td>${r.from_date || ''}</td>
          <td>${r.to_date || ''}</td>
          <td>${r.days ?? ''}</td>
          <td><span class="badge ${r.status === 'Approved' ? 'badge-success' : r.status === 'Rejected' ? 'badge-danger' : 'badge-warning'}">${r.status}</span></td>
          <td>${r.status === 'Applied' ? `
            <button class="action-btn" onclick="decideLeave('${r.id}', 'Approved')">Approve</button>
            <button class="action-btn action-btn-danger" onclick="decideLeave('${r.id}', 'Rejected')">Reject</button>
          ` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('leave-save-btn').addEventListener('click', saveLeave);
}

async function saveLeave() {
  const errorEl = document.getElementById('leave-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('leave-code').value.trim();
  const employeeId = document.getElementById('leave-employee').value;
  const leaveType = document.getElementById('leave-type').value;
  const fromDate = document.getElementById('leave-from').value;
  const toDate = document.getElementById('leave-to').value;
  const days = parseFloat(document.getElementById('leave-days').value);

  if (!code || !employeeId || !fromDate || !toDate || !days) {
    errorEl.textContent = 'Leave Code, Employee, From/To Date, and Days are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Leave', {
    method: 'POST',
    body: JSON.stringify({ id: code, code, employee_id: employeeId, leave_type: leaveType, from_date: fromDate, to_date: toDate, days, status: 'Applied' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to apply leave.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function decideLeave(leaveId, decision) {
  // The generic doc endpoint replaces the whole document on update, not a
  // partial patch - fetch the current record first and resubmit it with
  // just status changed (same pattern required when editing an Approved
  // PurchaseOrder, Stage 13.8).
  const getRes = await apiFetch(`/api/v1/doc/Leave/${encodeURIComponent(leaveId)}`);
  if (!getRes) return;
  if (!getRes.ok) {
    await showApiError(getRes, 'Failed to load leave record.', 'Update Failed');
    return;
  }
  const leave = await getRes.json();
  leave.status = decision;

  const res = await apiFetch(`/api/v1/doc/Leave/${encodeURIComponent(leaveId)}`, {
    method: 'POST',
    body: JSON.stringify(leave)
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update leave status.', 'Update Failed');
    return;
  }
  renderView('hr');
}

function renderPayrollExportTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; margin-bottom: 20px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-from">From</label>
        <input type="date" id="payroll-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-to">To</label>
        <input type="date" id="payroll-to" class="form-input">
      </div>
      <button class="btn btn-primary" id="payroll-export-btn">Export</button>
    </div>
    <div id="payroll-export-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div id="payroll-export-results"></div>
  `;
  container.appendChild(panel);

  document.getElementById('payroll-export-btn').addEventListener('click', runPayrollExport);
}

async function runPayrollExport() {
  const errorEl = document.getElementById('payroll-export-error');
  const resultsEl = document.getElementById('payroll-export-results');
  errorEl.classList.add('hidden');

  const from = document.getElementById('payroll-from').value;
  const to = document.getElementById('payroll-to').value;
  if (!from || !to) {
    errorEl.textContent = 'Select both From and To dates.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch(`/api/v1/hr/payroll-export?from=${from}&to=${to}`);
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Export failed.';
    errorEl.classList.remove('hidden');
    return;
  }

  let html = `
    <table>
      <thead><tr><th>Employee</th><th>Present Days</th><th>Absent Days</th><th>Late Days</th><th>Approved Leave Days</th></tr></thead>
      <tbody>
  `;
  html += data.length === 0
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No records in this period.</td></tr>`
    : data.map(e => `
        <tr>
          <td>${e.employee_id}</td>
          <td>${e.present_days}</td>
          <td>${e.absent_days}</td>
          <td>${e.late_days}</td>
          <td>${e.approved_leave_days}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  resultsEl.innerHTML = html;
}

// Fixed Asset Management (Stage 13.13b, MB 16.1) - lifecycle:
// Draft -> Capitalised -> (Transfer any number of times) -> Disposed.
// Depreciation/net block are calculated by the backend on every fetch, not
// stored, so they're always current as of "now."
async function renderAssetsView(container) {
  const res = await apiFetch('/api/v1/assets/register');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Fixed Assets</h1>
      <p class="page-subtitle">Asset register with calculated straight-line depreciation and net block.</p>
    </div>
  `;
  container.appendChild(header);

  const assets = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Asset (Draft)</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-code">Asset Number</label>
        <input type="text" id="asset-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-category">Category</label>
        <input type="text" id="asset-category" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-cost">Cost</label>
        <input type="number" id="asset-cost" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-useful-life">Useful Life (yrs)</label>
        <input type="number" id="asset-useful-life" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-location">Location</label>
        <input type="text" id="asset-location" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-custodian">Custodian</label>
        <input type="text" id="asset-custodian" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asset-acquisition-date">Acquisition Date</label>
        <input type="date" id="asset-acquisition-date" class="form-input">
      </div>
      <button class="btn btn-primary" id="asset-create-btn">Create</button>
    </div>
    <div id="asset-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Asset #</th><th>Category</th><th>Location</th><th>Custodian</th>
          <th>Cost</th><th>Accum. Depreciation</th><th>Net Block</th><th>Status</th><th></th>
        </tr>
      </thead>
      <tbody>
  `;
  html += assets.length === 0
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No assets yet.</td></tr>`
    : assets.map(a => `
        <tr>
          <td style="font-family: monospace;">${a.code || a.id}</td>
          <td>${a.category || ''}</td>
          <td>${a.location || ''}</td>
          <td>${a.custodian || ''}</td>
          <td>${a.cost.toLocaleString()}</td>
          <td>${a.accumulated_depreciation.toLocaleString()}</td>
          <td>${a.net_block.toLocaleString()}</td>
          <td><span class="badge ${a.status === 'Capitalised' ? 'badge-success' : a.status === 'Disposed' ? 'badge-danger' : 'badge-secondary'}">${a.status}</span></td>
          <td>${renderAssetActions(a)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('asset-create-btn').addEventListener('click', createAsset);
  attachTypeahead(document.getElementById('asset-location'), 'Location');
  attachTypeahead(document.getElementById('asset-custodian'), 'Employee');
}

function renderAssetActions(asset) {
  if (asset.status === 'Draft') {
    return `<button class="action-btn" onclick="capitalizeAsset('${asset.id}')">Capitalise</button>`;
  }
  if (asset.status === 'Capitalised') {
    return `
      <button class="action-btn" onclick="promptTransferAsset('${asset.id}')">Transfer</button>
      <button class="action-btn action-btn-danger" onclick="promptDisposeAsset('${asset.id}')">Dispose</button>
    `;
  }
  return '';
}

async function createAsset() {
  const errorEl = document.getElementById('asset-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('asset-code').value.trim();
  const category = document.getElementById('asset-category').value.trim();
  const cost = parseFloat(document.getElementById('asset-cost').value);
  const usefulLife = parseFloat(document.getElementById('asset-useful-life').value);
  const location = document.getElementById('asset-location').value.trim();
  const custodian = document.getElementById('asset-custodian').value.trim();
  const acquisitionDate = document.getElementById('asset-acquisition-date').value;

  if (!code || !cost || !usefulLife || !location || !acquisitionDate) {
    errorEl.textContent = 'Asset Number, Cost, Useful Life, Location, and Acquisition Date are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Asset', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, category, cost, useful_life_years: usefulLife,
      location, custodian, acquisition_date: acquisitionDate, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create asset.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('assets');
}

async function capitalizeAsset(assetId) {
  const res = await apiFetch('/api/v1/assets/capitalize', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to capitalise asset.', 'Capitalisation Failed');
    return;
  }
  renderView('assets');
}

async function promptTransferAsset(assetId) {
  const newLocation = await showCustomPrompt('New location:');
  if (!newLocation) return;
  const newCustodian = (await showCustomPrompt('New custodian (optional):')) || '';

  const res = await apiFetch('/api/v1/assets/transfer', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId, new_location: newLocation, new_custodian: newCustodian })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to transfer asset.', 'Transfer Failed');
    return;
  }
  renderView('assets');
}

async function promptDisposeAsset(assetId) {
  const confirmed = await showCustomConfirm('This will write off the asset\'s remaining net book value and close it out. Continue?', 'Dispose Asset');
  if (!confirmed) return;
  const disposalType = await showCustomPrompt('Disposal type (Sale, Scrap, or WriteOff):', 'Scrap');
  if (!disposalType) return;

  const res = await apiFetch('/api/v1/assets/dispose', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId, disposal_type: disposalType })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to dispose asset.', 'Disposal Failed');
    return;
  }
  renderView('assets');
}

// Transfers (Stage 21 QA fix): the sidebar's "Transfers" item routed to a
// view name ('transfers') the router had no case for, so it always fell
// through to the generic "Module Setup Pending" mock screen - a dead entry
// point despite engines/transfer_orders.go already having a full dispatch/
// receive lifecycle. TransferOrder is already a registered generic doctype
// (Draft/Approved/Dispatched/Received), but its dispatch/receive engine
// functions need a JSON-encoded `items` line array that has no field/UI
// anywhere yet, so - unlike Vendors/Stores/POSProfile - this needed a small
// bespoke view (mirroring renderAssetsView's form+list+action-button shape)
// rather than just pointing at the generic doctype-table.
//
// Draft -> Approved has no approval_rules row configured for TransferOrder
// (SubmitForApproval would just error "no approval rule configured"), so
// "Mark Approved" here is a direct status edit an authorized role can
// already make via the generic edit modal - this button just makes that
// one click instead of open-modal-find-status-save. Wiring TransferOrder
// into the maker-checker engine for real is a policy decision (which
// amount slab, which approver role) outside a QA-fix's scope.
let transferLineItems = [];

async function renderTransfersView(container) {
  const res = await apiFetch('/api/v1/doc/TransferOrder');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load transfer orders.', () => renderView('transfers')); return; }
  const transfers = await res.json();
  state.docData = transfers;
  transferLineItems = [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Transfers</h1>
      <p class="page-subtitle">Move stock between stores/warehouses: create a draft, get it approved, then dispatch and receive it.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Transfer (Draft)</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-number">Transfer Number</label>
        <input type="text" id="transfer-number" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-from">From Warehouse</label>
        <input type="text" id="transfer-from" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-to">To Warehouse</label>
        <input type="text" id="transfer-to" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-line-sku">SKU</label>
        <input type="text" id="transfer-line-sku" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="transfer-line-qty">Qty</label>
        <input type="number" id="transfer-line-qty" class="form-input" style="width: 90px;" min="1">
      </div>
      <button class="btn btn-outline" id="transfer-add-line-btn" type="button">Add Line</button>
    </div>
    <div id="transfer-lines-list" style="margin: 12px 0;"></div>
    <div id="transfer-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="transfer-create-btn">Create Transfer</button>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Transfer #</th><th>From</th><th>To</th><th>Items</th><th>Status</th><th></th>
        </tr>
      </thead>
      <tbody>
  `;
  html += transfers.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No transfers yet.</td></tr>`
    : transfers.map(t => `
        <tr>
          <td style="font-family: monospace;">${t.transfer_number || t.id}</td>
          <td>${t.from_warehouse || ''}</td>
          <td>${t.to_warehouse || ''}</td>
          <td>${(() => { try { return JSON.parse(t.items || '[]').length; } catch (e) { return 0; } })()}</td>
          <td><span class="badge ${t.status === 'Received' ? 'badge-success' : t.status === 'Draft' ? 'badge-secondary' : 'badge-warning'}">${t.status}</span></td>
          <td>${renderTransferActions(t)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  renderTransferLinesList();
  document.getElementById('transfer-add-line-btn').addEventListener('click', addTransferLine);
  document.getElementById('transfer-create-btn').addEventListener('click', createTransferOrder);
  attachTypeahead(document.getElementById('transfer-from'), 'Location');
  attachTypeahead(document.getElementById('transfer-to'), 'Location');
}

function renderTransferLinesList() {
  const el = document.getElementById('transfer-lines-list');
  if (!el) return;
  if (transferLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = transferLineItems.map((line, idx) => `
    <div style="display: flex; align-items: center; gap: 12px; padding: 6px 0; font-size: 13.5px;">
      <span style="font-family: monospace;">${line.sku}</span>
      <span>qty ${line.qty}</span>
      <button class="action-btn action-btn-danger" type="button" onclick="removeTransferLine(${idx})">Remove</button>
    </div>
  `).join('');
}

function addTransferLine() {
  const skuEl = document.getElementById('transfer-line-sku');
  const qtyEl = document.getElementById('transfer-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku || !qty || qty <= 0) return;
  transferLineItems.push({ sku, qty });
  skuEl.value = '';
  qtyEl.value = '';
  renderTransferLinesList();
}

window.removeTransferLine = function(idx) {
  transferLineItems.splice(idx, 1);
  renderTransferLinesList();
};

async function createTransferOrder() {
  const errorEl = document.getElementById('transfer-form-error');
  errorEl.classList.add('hidden');

  const transferNumber = document.getElementById('transfer-number').value.trim();
  const fromWarehouse = document.getElementById('transfer-from').value.trim();
  const toWarehouse = document.getElementById('transfer-to').value.trim();

  if (!transferNumber || !fromWarehouse || !toWarehouse || transferLineItems.length === 0) {
    errorEl.textContent = 'Transfer Number, From/To Warehouse, and at least one line item are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/TransferOrder', {
    method: 'POST',
    body: JSON.stringify({
      id: transferNumber, transfer_number: transferNumber,
      from_warehouse: fromWarehouse, to_warehouse: toWarehouse,
      items: JSON.stringify(transferLineItems), status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create transfer.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('transfers');
}

function renderTransferActions(t) {
  if (t.status === 'Draft') {
    return `<button class="action-btn" onclick="approveTransferOrder('${t.id}')">Mark Approved</button>`;
  }
  if (t.status === 'Approved') {
    // Pack (Stage 20.19) is an optional confirmation step, not a required
    // gate - Dispatch stays available directly from Approved too.
    return `<button class="action-btn" onclick="packTransferOrder('${t.id}')">Pack</button> <button class="action-btn" onclick="dispatchTransferOrder('${t.id}')">Dispatch</button>`;
  }
  if (t.status === 'Packed') {
    return `<button class="action-btn" onclick="dispatchTransferOrder('${t.id}')">Dispatch</button>`;
  }
  if (t.status === 'Dispatched') {
    return `<button class="action-btn" onclick="receiveTransferOrder('${t.id}')">Receive</button>`;
  }
  return '';
}

// Prompts for a Box ID per line item (same sequential-prompt pattern
// receiveTransferOrder below uses for received qty), grouping items that
// share a Box ID into one box before submitting - covers both "one box per
// SKU" and "everything in one box" without a bespoke multi-box form.
async function packTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  let lines = [];
  try { lines = JSON.parse(row.items || '[]'); } catch (e) { lines = []; }
  if (lines.length === 0) {
    await showCustomAlert('No line items found on this transfer.', 'Error');
    return;
  }

  const boxByItem = {};
  for (const line of lines) {
    const boxId = await showCustomPrompt(`Box ID for ${line.sku} (qty ${line.qty}):`, 'BOX1');
    if (boxId === null) return;
    if (!boxByItem[boxId]) boxByItem[boxId] = [];
    boxByItem[boxId].push({ sku: line.sku, qty: line.qty });
  }
  const boxes = Object.keys(boxByItem).map(boxId => ({ box_id: boxId, items: boxByItem[boxId] }));

  const res = await apiFetch('/api/v1/wms/transfer/pack', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, boxes })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to pack transfer.', 'Pack Failed');
    return;
  }
  renderView('transfers');
}

// The generic doc engine's POST-with-id update replaces the whole `data`
// blob (no JSONB merge - see internal/server/handlers_core_doc_engine.go's
// `data = EXCLUDED.data`), so this resends every field from the
// already-loaded row rather than just {status: ...}, or a status-only
// payload would silently wipe transfer_number/from_warehouse/to_warehouse/items.
async function approveTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  const res = await apiFetch(`/api/v1/doc/TransferOrder/${id}`, {
    method: 'POST',
    body: JSON.stringify({ ...row, status: 'Approved' })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to approve transfer.');
    return;
  }
  renderView('transfers');
}

async function dispatchTransferOrder(id) {
  if (!(await showCustomConfirm('Dispatch this transfer? Stock will move from the source location into transit.', 'Dispatch Transfer'))) return;
  const res = await apiFetch('/api/v1/transfer/dispatch', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to dispatch transfer.', 'Dispatch Failed');
    return;
  }
  renderView('transfers');
}

// Prompts for the received quantity of each dispatched line (sequentially,
// via the same showCustomPrompt dialog the rest of the app uses), defaulting
// to the full dispatched qty - covers both the common full-receipt case and
// a genuine partial/shortage receipt in one flow without a bespoke modal.
async function receiveTransferOrder(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  let dispatchedLines = [];
  try { dispatchedLines = JSON.parse(row.dispatched_items || row.items || '[]'); } catch (e) { dispatchedLines = []; }
  if (dispatchedLines.length === 0) {
    await showCustomAlert('No dispatched line items found on this transfer.', 'Error');
    return;
  }

  const receivedItems = [];
  for (const line of dispatchedLines) {
    const qtyStr = await showCustomPrompt(`Quantity received for ${line.sku} (dispatched ${line.qty}):`, String(line.qty));
    if (qtyStr === null) return;
    const qty = parseInt(qtyStr, 10);
    receivedItems.push({ sku: line.sku, qty: isNaN(qty) ? 0 : qty });
  }

  const res = await apiFetch('/api/v1/transfer/receive', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, received_items: receivedItems })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to receive transfer.', 'Receive Failed');
    return;
  }
  renderView('transfers');
}

// Expense Management (Stage 13.13c, MB 16.2). Claim -> Manager Approval ->
// Finance Verification -> Payment -> Accounting. Manager Approval reuses
// the existing Approval/Workflow Engine (Stage 13.8) - once submitted, a
// claim shows up in the existing "Approvals" screen automatically (it
// queries every approval-gated doctype), so this screen only needs to
// handle claim creation/submission plus the two stages after approval.
async function renderExpensesView(container) {
  const [claimsRes, employeesRes] = await Promise.all([
    apiFetch('/api/v1/doc/ExpenseClaim'),
    apiFetch('/api/v1/doc/Employee')
  ]);
  if (!claimsRes || !employeesRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Expenses</h1>
      <p class="page-subtitle">Claim &rarr; Manager Approval &rarr; Finance Verification &rarr; Payment.</p>
    </div>
  `;
  container.appendChild(header);

  const claims = claimsRes.ok ? await claimsRes.json() : [];
  const employees = employeesRes.ok ? await employeesRes.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Expense Claim</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-code">Claim Number</label>
        <input type="text" id="exp-code" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-employee">Employee</label>
        <select id="exp-employee" class="form-input" style="width: 180px;">
          <option value="">Select employee</option>
          ${employeeOptions(employees)}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-location">Location</label>
        <input type="text" id="exp-location" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-date">Expense Date</label>
        <input type="date" id="exp-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-category">Category</label>
        <select id="exp-category" class="form-input" style="width: 130px;">
          <option value="Conveyance">Conveyance</option>
          <option value="Travel">Travel</option>
          <option value="Food">Food</option>
          <option value="Hotel">Hotel</option>
          <option value="Fuel">Fuel</option>
          <option value="Repair">Repair</option>
          <option value="Medical">Medical</option>
          <option value="Marketing">Marketing</option>
          <option value="StoreExpense">StoreExpense</option>
          <option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-amount">Amount</label>
        <input type="number" id="exp-amount" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-gst">GST Amount</label>
        <input type="number" id="exp-gst" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="exp-advance">Advance Adjusted</label>
        <input type="number" id="exp-advance" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 160px;">
        <label class="form-label" for="exp-purpose">Purpose</label>
        <input type="text" id="exp-purpose" class="form-input">
      </div>
      <button class="btn btn-primary" id="exp-create-btn">Create Draft</button>
    </div>
    <div id="exp-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr><th>Claim #</th><th>Employee</th><th>Category</th><th>Amount</th><th>GST</th><th>Status</th><th></th></tr>
      </thead>
      <tbody>
  `;
  html += claims.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No expense claims yet.</td></tr>`
    : claims.map(c => `
        <tr>
          <td style="font-family: monospace;">${c.code || c.id}</td>
          <td>${c.employee_id || ''}</td>
          <td>${c.category || ''}</td>
          <td>${(c.amount ?? 0).toLocaleString()}</td>
          <td>${(c.gst_amount ?? 0).toLocaleString()}</td>
          <td><span class="badge ${expenseStatusBadge(c.status)}">${c.status}</span></td>
          <td>${renderExpenseActions(c)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('exp-create-btn').addEventListener('click', createExpenseClaim);
  attachTypeahead(document.getElementById('exp-location'), 'Location');
}

function expenseStatusBadge(status) {
  if (status === 'Paid') return 'badge-success';
  if (status === 'Rejected') return 'badge-danger';
  if (status === 'Pending Approval') return 'badge-warning';
  return 'badge-secondary';
}

function renderExpenseActions(claim) {
  if (claim.status === 'Draft') {
    return `<button class="action-btn" onclick="submitExpenseForApproval('${claim.id}')">Submit for Approval</button>`;
  }
  if (claim.status === 'Approved') {
    return `<button class="action-btn" onclick="verifyExpenseClaim('${claim.id}')">Finance Verify</button>`;
  }
  if (claim.status === 'Verified') {
    return `<button class="action-btn" onclick="payExpenseClaim('${claim.id}')">Mark Paid</button>`;
  }
  return '';
}

async function createExpenseClaim() {
  const errorEl = document.getElementById('exp-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('exp-code').value.trim();
  const employeeId = document.getElementById('exp-employee').value;
  const location = document.getElementById('exp-location').value.trim();
  const expenseDate = document.getElementById('exp-date').value;
  const category = document.getElementById('exp-category').value;
  const amount = parseFloat(document.getElementById('exp-amount').value);
  const gstAmount = parseFloat(document.getElementById('exp-gst').value) || 0;
  const advanceAdjusted = parseFloat(document.getElementById('exp-advance').value) || 0;
  const purpose = document.getElementById('exp-purpose').value.trim();

  if (!code || !employeeId || !location || !expenseDate || !amount) {
    errorEl.textContent = 'Claim Number, Employee, Location, Expense Date, and Amount are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ExpenseClaim', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, employee_id: employeeId, location, expense_date: expenseDate,
      category, amount, gst_amount: gstAmount, advance_adjusted: advanceAdjusted,
      purpose, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create expense claim.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('expenses');
}

async function submitExpenseForApproval(claimId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'ExpenseClaim', document_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.', 'Submit Failed');
    return;
  }
  renderView('expenses');
}

async function verifyExpenseClaim(claimId) {
  const res = await apiFetch('/api/v1/expenses/verify', {
    method: 'POST',
    body: JSON.stringify({ claim_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to verify claim.', 'Verification Failed');
    return;
  }
  renderView('expenses');
}

async function payExpenseClaim(claimId) {
  const confirmed = await showCustomConfirm('This will post the payment GL entry and mark the claim Paid. Continue?', 'Pay Expense Claim');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/expenses/pay', {
    method: 'POST',
    body: JSON.stringify({ claim_id: claimId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to pay claim.', 'Payment Failed');
    return;
  }
  renderView('expenses');
}

// Manufacturing (Stage 13.13e, scoped MVP) - single-level BOM + a linear
// Production Order (Draft -> Material Issued -> Completed). BOM's
// "components" field is JSON under the hood; this screen offers a simple
// "sku:qty, sku:qty" shorthand input instead of asking a user to hand-type
// JSON (BOM can still be edited directly via Master Definition if needed -
// it's a Master-type doctype, so it already has a generic CRUD screen there).
async function renderManufacturingView(container) {
  const [bomsRes, ordersRes] = await Promise.all([
    apiFetch('/api/v1/doc/BOM'),
    apiFetch('/api/v1/doc/ProductionOrder')
  ]);
  if (!bomsRes || !ordersRes) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Manufacturing</h1>
      <p class="page-subtitle">Single-level BOM and production orders (Draft &rarr; Material Issued &rarr; Completed).</p>
    </div>
  `;
  container.appendChild(header);

  const boms = bomsRes.ok ? await bomsRes.json() : [];
  const orders = ordersRes.ok ? await ordersRes.json() : [];

  const bomFormPanel = document.createElement('div');
  bomFormPanel.className = 'table-panel';
  bomFormPanel.style.padding = '24px';
  bomFormPanel.style.marginBottom = '24px';
  bomFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New BOM</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bom-code">BOM Code</label>
        <input type="text" id="bom-code" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bom-parent-item">Parent Item (Finished Good SKU)</label>
        <input type="text" id="bom-parent-item" class="form-input" style="width: 180px;">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 220px;">
        <label class="form-label" for="bom-components">Components (sku:qty, sku:qty, ...)</label>
        <input type="text" id="bom-components" class="form-input" placeholder="e.g. RAW-A:2, RAW-B:1">
      </div>
      <button class="btn btn-primary" id="bom-create-btn">Create BOM</button>
    </div>
    <div id="bom-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(bomFormPanel);

  const orderFormPanel = document.createElement('div');
  orderFormPanel.className = 'table-panel';
  orderFormPanel.style.padding = '24px';
  orderFormPanel.style.marginBottom = '24px';
  orderFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Production Order</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-code">Order Number</label>
        <input type="text" id="po-mfg-code" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-bom">BOM</label>
        <select id="po-mfg-bom" class="form-input" style="width: 200px;">
          <option value="">Select a BOM</option>
          ${boms.map(b => `<option value="${b.code || b.id}">${b.code || b.id} (${b.parent_item || ''})</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-qty">Quantity</label>
        <input type="number" id="po-mfg-qty" class="form-input" style="width: 100px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="po-mfg-location">Location</label>
        <input type="text" id="po-mfg-location" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-primary" id="po-mfg-create-btn">Create Order</button>
    </div>
    <div id="po-mfg-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(orderFormPanel);

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Order #</th><th>BOM</th><th>Quantity</th><th>Location</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += orders.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No production orders yet.</td></tr>`
    : orders.map(o => `
        <tr>
          <td style="font-family: monospace;">${o.code || o.id}</td>
          <td>${o.bom_id || ''}</td>
          <td>${o.quantity ?? ''}</td>
          <td>${o.location || ''}</td>
          <td><span class="badge ${o.status === 'Completed' ? 'badge-success' : 'badge-secondary'}">${o.status}</span></td>
          <td>${renderProductionOrderActions(o)}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('bom-create-btn').addEventListener('click', createBOM);
  document.getElementById('po-mfg-create-btn').addEventListener('click', createProductionOrder);
  attachTypeahead(document.getElementById('bom-parent-item'), 'Item');
  attachTypeahead(document.getElementById('po-mfg-location'), 'Location');
}

function renderProductionOrderActions(order) {
  if (order.status === 'Draft') {
    return `<button class="action-btn" onclick="issueProductionMaterial('${order.id}')">Issue Material</button>`;
  }
  if (order.status === 'Material Issued') {
    return `<button class="action-btn" onclick="completeProductionOrder('${order.id}')">Complete (Receive FG)</button>`;
  }
  return '';
}

async function createBOM() {
  const errorEl = document.getElementById('bom-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('bom-code').value.trim();
  const parentItem = document.getElementById('bom-parent-item').value.trim();
  const componentsRaw = document.getElementById('bom-components').value.trim();

  if (!code || !parentItem || !componentsRaw) {
    errorEl.textContent = 'BOM Code, Parent Item, and Components are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  let components;
  try {
    components = componentsRaw.split(',').map(part => {
      const [sku, qty] = part.split(':').map(s => s.trim());
      if (!sku || !qty || isNaN(parseFloat(qty))) throw new Error('bad format');
      return { sku, qty: parseFloat(qty) };
    });
  } catch (e) {
    errorEl.textContent = 'Components must look like "SKU:QTY, SKU:QTY" (e.g. RAW-A:2, RAW-B:1).';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/BOM', {
    method: 'POST',
    body: JSON.stringify({
      id: code, code, parent_item: parentItem,
      components: JSON.stringify(components), status: 'Active'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create BOM.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('manufacturing');
}

async function createProductionOrder() {
  const errorEl = document.getElementById('po-mfg-form-error');
  errorEl.classList.add('hidden');

  const code = document.getElementById('po-mfg-code').value.trim();
  const bomId = document.getElementById('po-mfg-bom').value;
  const quantity = parseFloat(document.getElementById('po-mfg-qty').value);
  const location = document.getElementById('po-mfg-location').value.trim();

  if (!code || !bomId || !quantity || !location) {
    errorEl.textContent = 'Order Number, BOM, Quantity, and Location are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ProductionOrder', {
    method: 'POST',
    body: JSON.stringify({ id: code, code, bom_id: bomId, quantity, location, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create production order.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('manufacturing');
}

async function issueProductionMaterial(orderId) {
  const res = await apiFetch('/api/v1/manufacturing/issue-material', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to issue material.', 'Material Issue Failed');
    return;
  }
  renderView('manufacturing');
}

async function completeProductionOrder(orderId) {
  const confirmed = await showCustomConfirm('This will receive the finished goods into inventory and close the order. Continue?', 'Complete Production Order');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/manufacturing/complete', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to complete production order.', 'Completion Failed');
    return;
  }
  renderView('manufacturing');
}

// PIM (Product Information Management) Foundation MVP (Stage 15). Product
// Family / Attribute Definition / Family Attribute are plain generic
// doctypes - their tabs below just navigate to the same generic
// doctype-table view "Vendors" already uses (menu-vendors, above), rather
// than duplicating list/table rendering. Workbench is the one bespoke
// screen, since it needs the completeness score/missing-field data the
// generic doc endpoint doesn't have.
let currentPIMTab = 'dashboard';
let currentPIMFamilyFilter = '';
let currentPIMSelectedItem = '';
const PIM_TABS = [
  { id: 'dashboard', label: 'Dashboard' },
  { id: 'workbench', label: 'Workbench' },
	{ id: 'reports', label: 'Reports' },
  { id: 'families', label: 'Product Families', doctype: 'ProductFamily' },
  { id: 'attributes', label: 'Attribute Definitions', doctype: 'ProductAttributeDef' },
  { id: 'attribute-groups', label: 'Attribute Groups', doctype: 'ProductAttributeGroup' },
  { id: 'family-attributes', label: 'Family Attributes', doctype: 'ProductFamilyAttribute' },
  { id: 'channels', label: 'Channels', doctype: 'Channel' },
  { id: 'channel-category-map', label: 'Category Mapping', doctype: 'ChannelCategoryMap' },
  { id: 'channel-field-map', label: 'Field Mapping', doctype: 'ChannelFieldMap' },
  { id: 'channel-validation-rules', label: 'Validation Rules', doctype: 'ChannelValidationRule' }
];

// Stage 26.4.3: taxonomy doctypes whose audit_logs trail (already captured
// by the existing db trigger, no new storage) can be viewed via a "History"
// row action in the generic doctype table - see viewTaxonomyHistory below.
const TAXONOMY_HISTORY_DOCTYPES = new Set(['ProductFamily', 'ProductAttributeDef', 'ProductFamilyAttribute', 'ProductAttributeGroup']);

// Doctypes reachable from a PIM tab (plus ProductContent, reachable from the
// PIM dashboard's "pending approval" shortcut) - renderDocTableView() checks
// this to decide whether to stay inside the PIM shell (header + tab bar)
// instead of replacing it, so clicking e.g. "Product Families" doesn't feel
// like it left PIM for an unrelated full-page master list.
const PIM_DOCTYPES = new Set([...PIM_TABS.filter(t => t.doctype).map(t => t.doctype), 'ProductContent']);

// Renders the "PIM" title + sub-tab bar shared by every PIM screen, whether
// that's renderPIMView's own Dashboard/Workbench/Reports tabs or a
// doctype-table view reached via one of the doctype-backed tabs (see
// PIM_DOCTYPES above). Always reflects currentPIMTab - callers set it first.
function renderPIMShellHeader(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">PIM</h1>
      <p class="page-subtitle">Product family/attribute framework, completeness scoring, content enrichment, media library, and channel publishing.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = PIM_TABS.map(t =>
    `<button class="btn ${t.id === currentPIMTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-pim-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-pim-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = PIM_TABS.find(t => t.id === btn.getAttribute('data-pim-tab'));
      setActiveMenu('menu-pim');
      closeSubmenus();
      currentPIMTab = tab.id;
      if (tab.doctype) {
        currentDoctype = tab.doctype;
        currentSearchQuery = '';
        currentTablePage = 1;
        renderView('doctype-table');
        return;
      }
      currentPIMSelectedItem = '';
      renderView('pim');
    });
  });
}

async function renderPIMView(container) {
  renderPIMShellHeader(container);

  if (currentPIMTab === 'dashboard') {
    await renderPIMDashboardTab(container);
  } else if (currentPIMTab === 'workbench') {
    await renderPIMWorkbenchTab(container);
  } else if (currentPIMTab === 'reports') {
    await renderPIMReportsTab(container);
  }
}

async function renderPIMDashboardTab(container) {
  const res = await apiFetch('/api/v1/pim/dashboard');
  if (!res || !res.ok) { renderErrorPanel(container, 'Unable to load the PIM dashboard.', () => renderView('pim')); return; }
  const stats = await res.json();
  const cards = [
    ['total_products', 'Products', 'workbench'], ['incomplete_products', 'Incomplete', 'workbench'],
    ['pending_content_approvals', 'Pending approval', 'content'], ['ready_to_publish', 'Ready to publish', 'workbench'],
    ['published_products', 'Published', 'workbench'], ['missing_main_images', 'Missing main image', 'workbench'],
    ['queued_publish_jobs', 'Publish queued', 'workbench'], ['failed_publish_jobs', 'Publish failed', 'workbench']
  ];
  const panel = document.createElement('div'); panel.className = 'table-panel';
  panel.innerHTML = `<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:14px;">${cards.map(([key, label, target]) => `<button class="table-panel pim-dashboard-card" data-pim-dashboard-target="${target}" style="padding:18px;text-align:left;border:1px solid var(--border-color);cursor:pointer;"><div class="text-muted" style="font-size:12px;">${label}</div><div style="font-size:28px;font-weight:700;margin-top:6px;">${stats[key] ?? 0}</div></button>`).join('')}</div>`;
  container.appendChild(panel);
  panel.querySelectorAll('[data-pim-dashboard-target]').forEach(card => card.addEventListener('click', () => {
    const target = card.getAttribute('data-pim-dashboard-target');
    if (target === 'content') { currentDoctype = 'ProductContent'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); return; }
    currentPIMTab = 'workbench'; renderView('pim');
  }));
}

async function renderPIMReportsTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `<div class="table-controls"><div class="form-group" style="margin:0;"><label class="form-label" for="pim-report-name">Report</label><select class="form-select" id="pim-report-name"><option value="content-aging">Content aging</option><option value="duplicate-media">Duplicate media</option><option value="channel-mapping-gap">Channel mapping gaps</option><option value="attribute-quality">Attribute quality</option><option value="media-expiry">Media expiry</option><option value="content-sla-breach">Content SLA breaches</option></select></div><button class="btn btn-primary" id="pim-report-run">Run report</button><button class="btn btn-outline" id="pim-search-feed-export">Download Search Feed (CSV)</button></div><div class="table-wrapper" id="pim-report-results" style="margin-top:16px;"></div>`;
  container.appendChild(panel);
  const results = panel.querySelector('#pim-report-results');
  const run = async () => {
    results.innerHTML = '<div class="text-muted">Loading report…</div>';
    const name = panel.querySelector('#pim-report-name').value;
    const res = await apiFetch(`/api/v1/pim/reports/${encodeURIComponent(name)}`);
    if (!res || !res.ok) { results.innerHTML = '<div class="text-muted">Unable to load this report.</div>'; return; }
    const rows = await res.json();
    if (!rows.length) { results.innerHTML = '<div class="text-muted">No issues found.</div>'; return; }
    const columns = Object.keys(rows[0]);
    results.innerHTML = `<table><thead><tr>${columns.map(column => `<th>${column.replaceAll('_', ' ')}</th>`).join('')}</tr></thead><tbody>${rows.map(row => `<tr>${columns.map(column => `<td>${row[column] ?? ''}</td>`).join('')}</tr>`).join('')}</tbody></table>`;
  };
  panel.querySelector('#pim-report-run').addEventListener('click', run);
  // Stage 26.4.9: search/discovery feed export - same authenticated-blob
  // download pattern as downloadReportExportCSV, since a plain <a href>
  // can't carry the Bearer token this endpoint requires.
  panel.querySelector('#pim-search-feed-export').addEventListener('click', async () => {
    const res = await apiFetch('/api/v1/pim/search-feed.csv');
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to download the search feed.'); return; }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'pim_search_feed.csv';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });
  await run();
}

async function renderPIMWorkbenchTab(container) {
  const familiesRes = await apiFetch('/api/v1/doc/ProductFamily');
  const families = familiesRes && familiesRes.ok ? await familiesRes.json() : [];

  const filterPanel = document.createElement('div');
  filterPanel.className = 'table-panel';
  filterPanel.style.padding = '16px 24px';
  filterPanel.style.marginBottom = '16px';
  filterPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-family-filter">Family</label>
        <select id="pim-family-filter" class="form-input" style="width: 220px;">
          <option value="">All families</option>
          ${families.map(f => `<option value="${f.code || f.id}" ${(f.code || f.id) === currentPIMFamilyFilter ? 'selected' : ''}>${f.name || f.code || f.id}</option>`).join('')}
        </select>
      </div>
    </div>
  `;
  container.appendChild(filterPanel);
  filterPanel.querySelector('#pim-family-filter').addEventListener('change', (e) => {
    currentPIMFamilyFilter = e.target.value;
    currentPIMSelectedItem = '';
    renderView('pim');
  });

  const query = currentPIMFamilyFilter ? `?family=${encodeURIComponent(currentPIMFamilyFilter)}` : '';
  const wbRes = await apiFetch(`/api/v1/pim/workbench${query}`);
  const entries = wbRes && wbRes.ok ? await wbRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Item</th><th>Name</th><th>Family</th><th>Status</th><th>Completeness</th><th>Missing</th></tr></thead>
      <tbody>
  `;
  html += entries.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No items found. Create one under Setup &raquo; Item.</td></tr>`
    : entries.map(e => {
        const badgeClass = e.score >= 80 ? 'badge-success' : e.score >= 40 ? 'badge-warning' : 'badge-danger';
        return `
          <tr class="pim-workbench-row" data-item="${e.item_code}" style="cursor: pointer;">
            <td style="font-family: monospace;">${e.item_code}</td>
            <td>${e.name || ''}</td>
            <td>${e.family || ''}</td>
            <td><span class="badge badge-secondary">${e.status || ''}</span></td>
            <td><span class="badge ${badgeClass}">${e.score}%</span></td>
            <td>${e.missing_count}</td>
          </tr>
        `;
      }).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  listPanel.querySelectorAll('.pim-workbench-row').forEach(row => {
    row.addEventListener('click', () => {
      currentPIMSelectedItem = row.getAttribute('data-item');
      renderView('pim');
    });
  });

  if (currentPIMSelectedItem) {
    await renderPIMDetailPanel(container, currentPIMSelectedItem);
  }
}

async function renderPIMDetailPanel(container, itemCode) {
  const compRes = await apiFetch(`/api/v1/pim/completeness/${encodeURIComponent(itemCode)}`);
  if (!compRes || !compRes.ok) return;
  const comp = await compRes.json();

  const attrDefsRes = await apiFetch('/api/v1/doc/ProductAttributeDef');
  const attrDefs = attrDefsRes && attrDefsRes.ok ? await attrDefsRes.json() : [];
  const channelsForOverrideRes = await apiFetch('/api/v1/doc/Channel');
  const channelsForOverride = channelsForOverrideRes && channelsForOverrideRes.ok ? await channelsForOverrideRes.json() : [];

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.style.marginTop = '16px';
  panel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">${itemCode} - Completeness: ${comp.score}% <span class="badge badge-secondary" style="margin-left: 8px;">${comp.enrichment_status || ''}</span></h2>
    <p style="color: var(--text-muted); margin-bottom: 16px;">
      Missing: ${comp.missing_fields && comp.missing_fields.length > 0 ? comp.missing_fields.join(', ') : 'Nothing - fully complete.'}
    </p>

    <h3 style="font-size: 14px; font-weight: 700; margin-bottom: 12px;">Add / Update Attribute Value</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 24px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-attr-select">Attribute</label>
        <select id="pim-attr-select" class="form-input" style="width: 200px;">
          <option value="">Select attribute</option>
          ${attrDefs.map(a => `<option value="${a.code || a.id}">${a.label || a.code || a.id}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-attr-value">Value</label>
        <input type="text" id="pim-attr-value" class="form-input" style="width: 200px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-attr-locale" title="Leave blank to set the global default value">Locale Override</label>
        <input type="text" id="pim-attr-locale" class="form-input" style="width: 100px;" placeholder="e.g. fr">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-attr-channel" title="Leave blank to set the global default value">Channel Override</label>
        <select id="pim-attr-channel" class="form-input" style="width: 160px;">
          <option value="">All channels</option>
          ${channelsForOverride.map(c => `<option value="${c.code || c.id}">${c.name || c.code || c.id}</option>`).join('')}
        </select>
      </div>
      <button class="btn btn-primary" id="pim-attr-save-btn">Save</button>
    </div>
    <div id="pim-attr-error" class="login-error hidden" style="margin-bottom: 16px;"></div>

    <h3 style="font-size: 14px; font-weight: 700; margin-bottom: 12px;">Content</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-lang">Language</label>
        <input type="text" id="pim-content-lang" class="form-input" style="width: 90px;" value="en">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-title">Title</label>
        <input type="text" id="pim-content-title" class="form-input" style="width: 220px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-short">Short Description</label>
        <input type="text" id="pim-content-short" class="form-input" style="width: 260px;">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0; flex: 1;">
        <label class="form-label" for="pim-content-long">Long Description</label>
        <textarea id="pim-content-long" class="form-input" rows="3" style="width: 100%;"></textarea>
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-seo">SEO Title</label>
        <input type="text" id="pim-content-seo" class="form-input" style="width: 220px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-tags">Tags</label>
        <input type="text" id="pim-content-tags" class="form-input" style="width: 220px;">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-owner">Owner (username)</label>
        <input type="text" id="pim-content-owner" class="form-input" style="width: 160px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-content-sla">SLA Due Date</label>
        <input type="date" id="pim-content-sla" class="form-input" style="width: 160px;">
      </div>
      <button class="btn btn-outline" id="pim-content-save-btn">Save Draft</button>
      <button class="btn btn-primary" id="pim-content-submit-btn">Submit for Approval</button>
    </div>
    <div id="pim-content-error" class="login-error hidden" style="margin-top: 16px;"></div>
    <div id="pim-content-history" style="margin-top: 16px;"></div>

    <h3 style="font-size: 14px; font-weight: 700; margin: 24px 0 12px;">Media</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-media-file">File (jpg/png/webp/gif/pdf)</label>
        <input type="file" id="pim-media-file" class="form-input" accept=".jpg,.jpeg,.png,.webp,.gif,.pdf">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-media-role">Role</label>
        <select id="pim-media-role" class="form-input" style="width: 160px;">
          <option>Main Image</option>
          <option>Gallery</option>
          <option>Variant Image</option>
          <option>Lifestyle</option>
          <option>Certificate</option>
          <option>Internal QC</option>
          <option>Video/Other</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-media-alt">Alt Text</label>
        <input type="text" id="pim-media-alt" class="form-input" style="width: 180px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-media-expiry">Expiry Date</label>
        <input type="date" id="pim-media-expiry" class="form-input" style="width: 160px;">
      </div>
      <button class="btn btn-primary" id="pim-media-upload-btn">Upload</button>
    </div>
    <div id="pim-media-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <div id="pim-media-gallery" style="display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 24px;"></div>

    <h3 style="font-size: 14px; font-weight: 700; margin-bottom: 12px;">Channel Publishing</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-publish-channel">Channel</label>
        <select id="pim-publish-channel" class="form-input" style="width: 200px;"><option value="">Loading...</option></select>
      </div>
      <button class="btn btn-outline" id="pim-publish-preview-btn">Preview</button>
      <button class="btn btn-primary" id="pim-publish-btn">Publish</button>
    </div>
    <div id="pim-publish-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <div id="pim-publish-preview" style="margin-bottom: 12px;"></div>
    <div id="pim-publish-log"></div>
  `;
  container.appendChild(panel);

  document.getElementById('pim-attr-save-btn').addEventListener('click', () => savePIMAttributeValue(itemCode));
  document.getElementById('pim-content-save-btn').addEventListener('click', () => savePIMContent(itemCode, 'Draft'));
  document.getElementById('pim-content-submit-btn').addEventListener('click', () => submitPIMContent(itemCode));
  document.getElementById('pim-media-upload-btn').addEventListener('click', () => uploadPIMMedia(itemCode));
  document.getElementById('pim-publish-btn').addEventListener('click', () => publishPIMItem(itemCode));
  document.getElementById('pim-publish-preview-btn').addEventListener('click', () => previewPIMPublish(itemCode));

  await renderPIMMediaGallery(itemCode);
  await renderPIMPublishSection(itemCode);
  await renderPIMContentHistory(itemCode);
}

async function renderPIMMediaGallery(itemCode) {
  const gallery = document.getElementById('pim-media-gallery');
  if (!gallery) return;
  const res = await apiFetch(`/api/v1/pim/media?item=${encodeURIComponent(itemCode)}`);
  const media = res && res.ok ? await res.json() : [];

  if (media.length === 0) {
    gallery.innerHTML = `<div style="color: var(--text-muted); font-size: 13px;">No media uploaded yet.</div>`;
    return;
  }

  gallery.innerHTML = media.map(m => `
    <div class="table-panel" style="padding: 8px; width: 150px;" data-media-card="${m.id}">
      <div style="font-size: 11px; font-weight: 600; margin-bottom: 4px;">${m.media_role} <span class="text-muted">v${m.version_no || 1}</span></div>
      <img data-media-thumb="${m.id}" style="width: 100%; height: 90px; object-fit: cover; background: var(--bg-secondary); border-radius: 4px;" alt="${m.alt_text || m.media_role}">
      <div class="text-muted" style="font-size:10px; margin-top:4px; word-break:break-word;">${m.alt_text || 'No alt text'}${m.expiry_date ? ` · expires ${m.expiry_date}` : ''}</div>
      <button class="btn btn-outline btn-sm" style="width: 100%; margin-top: 6px;" data-edit-media="${m.id}" data-alt="${m.alt_text || ''}" data-expiry="${m.expiry_date || ''}">Edit Alt/Expiry</button>
      <button class="btn btn-outline btn-sm" style="width: 100%; margin-top: 4px;" data-deactivate-media="${m.id}">Deactivate</button>
    </div>
  `).join('');

  // <img> tags can't send an Authorization header, so each thumbnail is
  // fetched as an authenticated blob and swapped in via an object URL
  // rather than pointing src directly at the (auth-gated) file endpoint.
  // Prefer the generated thumbnail (26.4.4, smaller/faster) and fall back
  // to the full file for media with no thumbnail (webp/gif/pdf/decode
  // failure - see engines.generateThumbnail's scope note).
  media.forEach(async (m) => {
    let imgRes = m.has_thumbnail ? await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/thumbnail`) : null;
    if (!imgRes || !imgRes.ok) {
      imgRes = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/file`);
    }
    if (imgRes && imgRes.ok) {
      const blob = await imgRes.blob();
      const imgEl = gallery.querySelector(`[data-media-thumb="${m.id}"]`);
      if (imgEl) imgEl.src = URL.createObjectURL(blob);
    }
  });

  gallery.querySelectorAll('[data-deactivate-media]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const mediaId = btn.getAttribute('data-deactivate-media');
      const res = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(mediaId)}/deactivate`, { method: 'POST' });
      if (res && res.ok) renderView('pim');
    });
  });

  gallery.querySelectorAll('[data-edit-media]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const mediaId = btn.getAttribute('data-edit-media');
      const altText = await showCustomPrompt('Alt text:', btn.getAttribute('data-alt') || '', 'Edit Media Metadata');
      if (altText === null) return;
      const expiryDate = await showCustomPrompt('Expiry date (YYYY-MM-DD, blank for none):', btn.getAttribute('data-expiry') || '', 'Edit Media Metadata');
      if (expiryDate === null) return;
      const res = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(mediaId)}/metadata`, {
        method: 'POST', body: JSON.stringify({ alt_text: altText, expiry_date: expiryDate })
      });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Failed to update media metadata.'); return; }
      renderView('pim');
    });
  });
}

async function uploadPIMMedia(itemCode) {
  const errorEl = document.getElementById('pim-media-error');
  errorEl.classList.add('hidden');

  const fileInput = document.getElementById('pim-media-file');
  const role = document.getElementById('pim-media-role').value;
  if (!fileInput.files.length) {
    errorEl.textContent = 'Select a file first.';
    errorEl.classList.remove('hidden');
    return;
  }

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);
  formData.append('item', itemCode);
  formData.append('media_role', role);
  formData.append('alt_text', document.getElementById('pim-media-alt').value.trim());
  formData.append('expiry_date', document.getElementById('pim-media-expiry').value.trim());

  const res = await apiUpload('/api/v1/pim/media/upload', formData);
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to upload media.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('pim');
}

async function renderPIMPublishSection(itemCode) {
  const select = document.getElementById('pim-publish-channel');
  const logEl = document.getElementById('pim-publish-log');
  if (!select || !logEl) return;

  const channelsRes = await apiFetch('/api/v1/doc/Channel');
  const channels = channelsRes && channelsRes.ok ? await channelsRes.json() : [];
  select.innerHTML = channels.length === 0
    ? `<option value="">No channels configured</option>`
    : channels.map(c => `<option value="${c.code || c.id}">${c.name || c.code || c.id}</option>`).join('');

  const logRes = await apiFetch(`/api/v1/pim/publish-log?item=${encodeURIComponent(itemCode)}`);
  const log = logRes && logRes.ok ? await logRes.json() : [];
  logEl.innerHTML = log.length === 0
    ? `<div style="color: var(--text-muted); font-size: 13px;">No publish attempts yet.</div>`
    // error_code (Stage 26.4.8: marketplace error dictionary) lets a failed
    // attempt be triaged at a glance (missing credential vs. duplicate SKU
    // vs. a blank required field) instead of only reading the raw message.
    : `<table><thead><tr><th>Channel</th><th>Status</th><th>External ID</th><th>Error Code</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${l.channel_code}</td><td><span class="badge ${l.status === 'Published' ? 'badge-success' : 'badge-danger'}">${l.status}</span></td><td style="font-family: monospace;">${l.external_id || ''}</td><td style="font-family: monospace;">${l.error_code || ''}</td><td>${l.created_at || ''}</td></tr>`).join('')
      }</tbody></table>`;
}

// previewPIMPublish (Stage 26.4.7) shows the outbound payload that would be
// sent right now, diffed against the last publish attempt's snapshot if one
// exists - see engines.PreviewChannelDiff for why this isn't a live
// read-back from the platform itself.
async function previewPIMPublish(itemCode) {
  const errorEl = document.getElementById('pim-publish-error');
  const previewEl = document.getElementById('pim-publish-preview');
  errorEl.classList.add('hidden');

  const channel = document.getElementById('pim-publish-channel').value;
  if (!channel) {
    errorEl.textContent = 'Select a channel first.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch(`/api/v1/pim/publish-preview?item=${encodeURIComponent(itemCode)}&channel=${encodeURIComponent(channel)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to build a preview.');
    return;
  }
  const preview = await res.json();
  previewEl.innerHTML = `
    <h4 style="font-size:13px;font-weight:700;margin-bottom:8px;">${preview.has_prior_snapshot ? 'Diff vs. last publish attempt' : 'Outbound payload (no prior publish attempt for this channel to diff against)'}</h4>
    <table><thead><tr><th>Field</th><th>Previously Published</th><th>About to Publish</th></tr></thead><tbody>${
      (preview.fields || []).map(f => `<tr style="${f.changed ? 'font-weight:600;' : ''}"><td>${f.field}</td><td>${f.old || ''}</td><td>${f.new || ''}</td></tr>`).join('')
    }</tbody></table>
  `;
}

// renderPIMContentHistory (Stage 26.4.5/26.4.6) surfaces one ProductContent
// document's approval_log history (in particular, a rejection's mandatory
// comment) and its approved-version snapshots with a one-click restore.
// Scoped to the language currently entered in the content form, since that
// determines the "<item>::<language>" composite id being edited.
async function renderPIMContentHistory(itemCode) {
  const container = document.getElementById('pim-content-history');
  if (!container) return;
  const langInput = document.getElementById('pim-content-lang');
  const language = (langInput && langInput.value.trim()) || 'en';
  const contentID = `${itemCode}::${language}`;

  const [logRes, versionsRes] = await Promise.all([
    apiFetch(`/api/v1/approval/log?doctype=ProductContent&document_id=${encodeURIComponent(contentID)}`),
    apiFetch(`/api/v1/pim/content/${encodeURIComponent(contentID)}/versions`)
  ]);
  const log = logRes && logRes.ok ? await logRes.json() : [];
  const versions = versionsRes && versionsRes.ok ? await versionsRes.json() : [];

  const logHTML = log.length === 0
    ? `<div class="text-muted" style="font-size:13px;">No approval history yet.</div>`
    : `<table><thead><tr><th>Action</th><th>By</th><th>Comment</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${l.action}</td><td>${l.actor_user_id}</td><td>${l.comment || ''}</td><td>${l.created_at || ''}</td></tr>`).join('')
      }</tbody></table>`;

  const versionsHTML = versions.length === 0
    ? `<div class="text-muted" style="font-size:13px;">No approved versions yet.</div>`
    : `<table><thead><tr><th>Version</th><th>Title</th><th>Approved At</th><th></th></tr></thead><tbody>${
        versions.map(v => `<tr><td>${v.version_no}</td><td>${(v.data && v.data.title) || ''}</td><td>${v.created_at || ''}</td><td><button class="btn btn-outline btn-sm" data-rollback-version="${v.id}">Restore</button></td></tr>`).join('')
      }</tbody></table>`;

  container.innerHTML = `
    <h3 style="font-size: 14px; font-weight: 700; margin-bottom: 8px;">Approval History</h3>
    ${logHTML}
    <h3 style="font-size: 14px; font-weight: 700; margin: 16px 0 8px;">Approved Versions</h3>
    ${versionsHTML}
  `;

  container.querySelectorAll('[data-rollback-version]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const versionID = btn.getAttribute('data-rollback-version');
      if (!await showCustomConfirm('Restore this version as the current Draft content? It will need re-approval before publishing again.', 'Confirm Restore')) return;
      const res = await apiFetch(`/api/v1/pim/content/${encodeURIComponent(contentID)}/rollback`, {
        method: 'POST', body: JSON.stringify({ version_id: Number(versionID) })
      });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'Failed to restore this version.'); return; }
      renderView('pim');
    });
  });
}

async function publishPIMItem(itemCode) {
  const errorEl = document.getElementById('pim-publish-error');
  errorEl.classList.add('hidden');

  const channel = document.getElementById('pim-publish-channel').value;
  if (!channel) {
    errorEl.textContent = 'Select a channel first.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/pim/publish', {
    method: 'POST',
    body: JSON.stringify({ item_code: itemCode, channel })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to queue publish.';
    errorEl.classList.remove('hidden');
    return;
  }
  await renderPIMPublishSection(itemCode);
}

async function savePIMAttributeValue(itemCode) {
  const errorEl = document.getElementById('pim-attr-error');
  errorEl.classList.add('hidden');

  const attributeId = document.getElementById('pim-attr-select').value;
  const value = document.getElementById('pim-attr-value').value.trim();
  const locale = document.getElementById('pim-attr-locale').value.trim();
  const channel = document.getElementById('pim-attr-channel').value;
  if (!attributeId || !value) {
    errorEl.textContent = 'Attribute and Value are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // Stage 26.4.1: mirrors engines.attributeValueID - a blank locale+channel
  // is the global default row (unchanged base id), either one set is a
  // scoped override row with its own distinct id.
  const id = (locale === '' && channel === '')
    ? `${itemCode}::${attributeId}`
    : `${itemCode}::${attributeId}::${locale}::${channel}`;
  const res = await apiFetch('/api/v1/doc/ProductAttributeValue', {
    method: 'POST',
    body: JSON.stringify({ id, code: id, item: itemCode, attribute: attributeId, value, locale, channel, status: 'Active' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to save attribute value.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('pim');
}

function pimContentPayload(itemCode, status) {
  const language = document.getElementById('pim-content-lang').value.trim() || 'en';
  const id = `${itemCode}::${language}`;
  return {
    id,
    payload: {
      id,
      code: id,
      product_id: itemCode,
      language,
      title: document.getElementById('pim-content-title').value.trim(),
      short_desc: document.getElementById('pim-content-short').value.trim(),
      long_desc: document.getElementById('pim-content-long').value.trim(),
      seo_title: document.getElementById('pim-content-seo').value.trim(),
      tags: document.getElementById('pim-content-tags').value.trim(),
      owner: document.getElementById('pim-content-owner').value.trim(),
      sla_due_date: document.getElementById('pim-content-sla').value.trim(),
      status
    }
  };
}

async function savePIMContent(itemCode, status) {
  const errorEl = document.getElementById('pim-content-error');
  errorEl.classList.add('hidden');

  const { payload } = pimContentPayload(itemCode, status);
  if (!payload.title) {
    errorEl.textContent = 'Title is required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ProductContent', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to save content.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('pim');
}

async function submitPIMContent(itemCode) {
  const errorEl = document.getElementById('pim-content-error');
  errorEl.classList.add('hidden');

  // Save the current draft first so "Submit" always submits what's on
  // screen, then submit that same id into the existing generic
  // Approval/Workflow Engine (Stage 13.8) - no PIM-specific approval code.
  const { id, payload } = pimContentPayload(itemCode, 'Draft');
  if (!payload.title) {
    errorEl.textContent = 'Title is required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const saveRes = await apiFetch('/api/v1/doc/ProductContent', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
  if (!saveRes) return;
  if (!saveRes.ok) {
    const data = await saveRes.json();
    errorEl.textContent = data.error || 'Failed to save content before submitting.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'ProductContent', document_id: id })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit for approval.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('pim');
}

// Render dynamic DocType CRUD Table view
async function renderDocTableView(container) {
  const metaRes = await apiFetch(`/api/v1/doc/${currentDoctype}/meta`);
  if (!metaRes) return;
  if (!metaRes.ok) {
    const msg = await getErrorMessage(metaRes, `Failed to load schema for ${getTranslatedLabel(currentDoctype)}.`);
    renderErrorPanel(container, msg, () => renderView('doctype-table'));
    return;
  }
  state.activeDocFields = await metaRes.json();

  const dataRes = await apiFetch(`/api/v1/doc/${currentDoctype}`);
  if (!dataRes) return;
  if (!dataRes.ok) {
    const msg = await getErrorMessage(dataRes, `Failed to load records for ${getTranslatedLabel(currentDoctype)}.`);
    renderErrorPanel(container, msg, () => renderView('doctype-table'));
    return;
  }
  state.docData = await dataRes.json();
  bulkSelectedDocIDs = new Set();

  // Stay inside the PIM shell (title + sub-tab bar) for doctypes reached via
  // a PIM tab, instead of this view's own header replacing it outright -
  // otherwise clicking e.g. "Product Families" feels like it left PIM
  // entirely for an unrelated full-page master list.
  if (PIM_DOCTYPES.has(currentDoctype)) {
    renderPIMShellHeader(container);
  }

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${getTranslatedLabel(currentDoctype)}</h1>
      <p class="page-subtitle">Pluggable module metadata records database</p>
    </div>
    <div style="display:flex; gap: 8px;">
      <button class="btn btn-outline" onclick="openImportModal()">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        <span>Bulk Import</span>
      </button>
      <button class="btn btn-primary" onclick="openDynamicModal()">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
        <span>New ${getTranslatedLabel(currentDoctype)}</span>
      </button>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input type="text" placeholder="Search table..." value="${currentSearchQuery}" oninput="handleTableSearch(event)">
      </div>
      ${isPIMBulkEditDoctype() ? `<div class="bulk-edit-bar hidden" id="pim-bulk-edit-bar"><span id="pim-bulk-selection-count">0 selected</span><button class="btn btn-outline" id="pim-bulk-edit-button" onclick="openPIMBulkEditModal()" disabled>Edit Selected</button>${currentDoctype === 'ProductContent' ? `<button class="btn btn-primary" id="pim-bulk-approve-button" onclick="bulkDecideProductContent('Approved')" disabled>Approve Selected</button><button class="btn btn-outline" id="pim-bulk-reject-button" onclick="bulkDecideProductContent('Rejected')" disabled>Reject Selected</button>` : ''}</div>` : ''}
    </div>
    <div class="table-wrapper" id="doc-table-wrapper"></div>
    <div class="pagination" id="doc-table-pagination"></div>
  `;
  container.appendChild(panel);

  renderDocTable();
}

window.handleTableSearch = function(e) {
  currentSearchQuery = e.target.value.toLowerCase();
  currentTablePage = 1;
  renderDocTable();
  saveNavState();
};

function renderDocTable() {
  const wrapper = document.getElementById('doc-table-wrapper');
  const paginator = document.getElementById('doc-table-pagination');
  if (!wrapper) return;

  const filtered = state.docData.filter(d => {
    for (const val of Object.values(d)) {
      if (String(val).toLowerCase().includes(currentSearchQuery)) return true;
    }
    return false;
  });

  const total = filtered.length;
  const pages = Math.ceil(total / itemsPerPage) || 1;
  const start = (currentTablePage - 1) * itemsPerPage;
  const end = Math.min(start + itemsPerPage, total);
  const items = filtered.slice(start, end);
  const bulkEditingEnabled = isPIMBulkEditDoctype();

  let tableHTML = `
    <table>
      <thead>
        <tr>
          ${bulkEditingEnabled ? `<th style="width: 42px;"><input type="checkbox" aria-label="Select all visible records" onchange="togglePIMBulkPageSelection(this.checked)" ${items.length > 0 && items.every(item => bulkSelectedDocIDs.has(item.id)) ? 'checked' : ''}></th>` : ''}
          ${state.activeDocFields.map(f => `<th>${getTranslatedLabel(f.label)}</th>`).join('')}
          <th style="text-align: right;">Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  if (items.length === 0) {
    tableHTML += `<tr><td colspan="${state.activeDocFields.length + 1 + (bulkEditingEnabled ? 1 : 0)}" class="text-center py-8 text-muted">No records found.</td></tr>`;
  } else {
    items.forEach(row => {
      tableHTML += `<tr>`;
      if (bulkEditingEnabled) {
        tableHTML += `<td><input type="checkbox" aria-label="Select ${row.id}" data-doc-id="${encodeURIComponent(row.id)}" onchange="togglePIMBulkDocSelection(decodeURIComponent(this.dataset.docId), this.checked)" ${bulkSelectedDocIDs.has(row.id) ? 'checked' : ''}></td>`;
      }
      state.activeDocFields.forEach(f => {
        const val = row[f.fieldname] || '';
        if (f.fieldname === 'status') {
          const cls = val === 'Active' ? 'badge-success' : 'badge-secondary';
          tableHTML += `<td><span class="badge ${cls}">${val}</span></td>`;
        } else {
          tableHTML += `<td>${val}</td>`;
        }
      });
      const showHistory = TAXONOMY_HISTORY_DOCTYPES.has(currentDoctype);
      tableHTML += `
        <td style="text-align: right;">
          ${showHistory ? `<button class="action-btn" title="History" style="margin-right:4px;" onclick="viewTaxonomyHistory('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          </button>` : ''}
          <button class="action-btn" title="Edit" style="margin-right:4px;" onclick="editDocRecord('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
          </button>
          <button class="action-btn action-btn-danger" title="Delete" onclick="deleteDocRecord('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>
          </button>
        </td>
      </tr>`;
    });
  }

  tableHTML += `</tbody></table>`;
  wrapper.innerHTML = tableHTML;
  updatePIMBulkEditBar();

  paginator.innerHTML = `
    <span>Showing ${total === 0 ? 0 : start + 1}-${end} of ${total}</span>
    <div class="pagination-buttons">
      <button class="pagination-btn" onclick="changeDocPage(${currentTablePage - 1})" ${currentTablePage === 1 ? 'disabled' : ''}>Previous</button>
      <button class="pagination-btn" onclick="changeDocPage(${currentTablePage + 1})" ${currentTablePage === pages ? 'disabled' : ''}>Next</button>
    </div>
  `;
}

function isPIMBulkEditDoctype() {
  if (currentDoctype === 'Item') return true;
  const active = state.activeDoctypes.find(doc => doc.name === currentDoctype);
  return active && String(active.module || '').toLowerCase() === 'pim';
}

function visibleDocTableItems() {
  const filtered = state.docData.filter(d => Object.values(d).some(val => String(val).toLowerCase().includes(currentSearchQuery)));
  const start = (currentTablePage - 1) * itemsPerPage;
  return filtered.slice(start, Math.min(start + itemsPerPage, filtered.length));
}

function updatePIMBulkEditBar() {
  const bar = document.getElementById('pim-bulk-edit-bar');
  const count = document.getElementById('pim-bulk-selection-count');
  const button = document.getElementById('pim-bulk-edit-button');
  if (!bar || !count || !button) return;
  const selected = bulkSelectedDocIDs.size;
  count.textContent = `${selected} selected`;
  button.disabled = selected === 0;
  bar.classList.toggle('hidden', selected === 0);
  const approveBtn = document.getElementById('pim-bulk-approve-button');
  const rejectBtn = document.getElementById('pim-bulk-reject-button');
  if (approveBtn) approveBtn.disabled = selected === 0;
  if (rejectBtn) rejectBtn.disabled = selected === 0;
}

// bulkDecideProductContent (Stage 26.4.6) approves/rejects every currently
// selected ProductContent row in one call - see engines.BulkDecideApproval.
// A rejection needs a comment (APPROV-0159 already enforces this per-
// document server-side); collected once up front rather than per row.
window.bulkDecideProductContent = async function(decision) {
  const ids = [...bulkSelectedDocIDs];
  if (ids.length === 0) return;
  let comment = '';
  if (decision === 'Rejected') {
    comment = (await showCustomPrompt('Reason for rejecting these documents:', '', 'Bulk Reject')) || '';
    if (!comment.trim()) {
      await showCustomAlert('A comment is required to reject.', 'Bulk Reject');
      return;
    }
  }
  if (!await showCustomConfirm(`${decision} ${ids.length} selected content record${ids.length === 1 ? '' : 's'}?`, `Confirm Bulk ${decision}`)) return;
  const res = await apiFetch('/api/v1/approval/bulk-decide', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'ProductContent', document_ids: ids, decision, comment })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, `Bulk ${decision.toLowerCase()} failed.`);
    return;
  }
  const data = await res.json();
  const failedCount = Object.keys(data.failed || {}).length;
  bulkSelectedDocIDs = new Set();
  await showCustomAlert(`${(data.succeeded || []).length} succeeded, ${failedCount} failed.`, `Bulk ${decision} Complete`);
  renderView('doctype-table');
};

window.togglePIMBulkDocSelection = function(id, selected) {
  if (selected) bulkSelectedDocIDs.add(id);
  else bulkSelectedDocIDs.delete(id);
  updatePIMBulkEditBar();
};

window.togglePIMBulkPageSelection = function(selected) {
  visibleDocTableItems().forEach(row => {
    if (selected) bulkSelectedDocIDs.add(row.id);
    else bulkSelectedDocIDs.delete(row.id);
  });
  renderDocTable();
};

// viewTaxonomyHistory (Stage 26.4.3) shows one taxonomy document's existing
// audit_logs trail in a lightweight read-only modal, reusing the same
// .modal-overlay/.modal-container primitives the bulk-edit modal below
// uses instead of introducing a second modal component.
window.viewTaxonomyHistory = async function(id) {
  const res = await apiFetch(`/api/v1/pim/taxonomy-history/${encodeURIComponent(currentDoctype)}/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load history for this record.');
    return;
  }
  const entries = await res.json();

  document.getElementById('pim-history-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-history-modal';
  const rows = entries.length === 0
    ? `<tr><td colspan="3" class="text-center text-muted">No history recorded yet.</td></tr>`
    : entries.map(e => `<tr><td>${e.created_at || ''}</td><td>${e.user_id || ''}</td><td>${e.details || ''}</td></tr>`).join('');
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">History: ${getTranslatedLabel(currentDoctype)} ${id}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><div class="table-wrapper"><table><thead><tr><th>When</th><th>User</th><th>Change</th></tr></thead><tbody>${rows}</tbody></table></div></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
};

window.openPIMBulkEditModal = function() {
  if (bulkSelectedDocIDs.size === 0) return;
  const fields = state.activeDocFields.filter(field => field.fieldname !== 'id' && field.fieldname !== 'code');
  if (fields.length === 0) {
    showCustomAlert('This document type has no editable fields.', 'Bulk Edit');
    return;
  }

  document.getElementById('pim-bulk-edit-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-bulk-edit-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Edit ${bulkSelectedDocIDs.size} selected ${getTranslatedLabel(currentDoctype)} record${bulkSelectedDocIDs.size === 1 ? '' : 's'}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <form><div class="modal-body"><div class="form-group"><label class="form-label">Field</label><select class="form-select" id="pim-bulk-field"></select></div><div class="form-group"><label class="form-label" id="pim-bulk-value-label">New value</label><div id="pim-bulk-value"></div></div><p class="text-muted" style="font-size:13px; margin:0;">Preview: this change will update all ${bulkSelectedDocIDs.size} selected records. Approved records are returned to Pending Approval when their doctype is approval-gated.</p></div><div class="modal-footer"><button type="button" class="btn btn-secondary">Cancel</button><button type="submit" class="btn btn-primary">Confirm bulk edit</button></div></form>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
  const fieldSelect = overlay.querySelector('#pim-bulk-field');
  fields.forEach(field => {
    const option = document.createElement('option');
    option.value = field.fieldname;
    option.textContent = getTranslatedLabel(field.label);
    fieldSelect.appendChild(option);
  });
  const renderValueInput = () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const holder = overlay.querySelector('#pim-bulk-value');
    holder.replaceChildren();
    let input;
    if (field.fieldtype === 'Select') {
      input = document.createElement('select');
      input.className = 'form-select';
      field.options.split(',').filter(Boolean).forEach(value => {
        const option = document.createElement('option');
        option.value = value.trim();
        option.textContent = value.trim();
        input.appendChild(option);
      });
    } else {
      input = document.createElement('input');
      input.className = 'form-input';
      input.type = field.fieldtype === 'Number' ? 'number' : 'text';
    }
    input.id = 'pim-bulk-value-input';
    input.required = field.mandatory;
    holder.appendChild(input);
    overlay.querySelector('#pim-bulk-value-label').textContent = `New ${getTranslatedLabel(field.label)}`;
  };
  fieldSelect.addEventListener('change', renderValueInput);
  renderValueInput();
  overlay.querySelector('form').addEventListener('submit', async event => {
    event.preventDefault();
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const input = overlay.querySelector('#pim-bulk-value-input');
    const value = field.fieldtype === 'Number' ? Number(input.value) : input.value;
    if (field.mandatory && String(input.value).trim() === '') return;
    const count = bulkSelectedDocIDs.size;
    if (!await showCustomConfirm(`Update ${count} selected ${getTranslatedLabel(currentDoctype)} record${count === 1 ? '' : 's'}?`, 'Confirm Bulk Edit')) return;
    const res = await apiFetch('/api/v1/pim/bulk-edit', { method: 'POST', body: JSON.stringify({ doctype: currentDoctype, ids: [...bulkSelectedDocIDs], field: field.fieldname, value }) });
    if (!res) return;
    if (!res.ok) {
      await showApiError(res, 'Bulk edit failed. No records were changed.');
      return;
    }
    close();
    bulkSelectedDocIDs = new Set();
    await showCustomAlert(`${count} record${count === 1 ? '' : 's'} updated.`, 'Bulk Edit Complete');
    renderView('doctype-table');
  });
};

window.changeDocPage = function(page) {
  currentTablePage = page;
  renderDocTable();
  saveNavState();
};

window.deleteDocRecord = async function(id) {
  if (await showCustomConfirm('Delete this record?')) {
    const res = await apiFetch(`/api/v1/doc/${currentDoctype}/${id}`, { method: 'DELETE' });
    if (!res) return;
    if (res.ok) {
      renderView('doctype-table');
    } else {
      await showApiError(res, 'Failed to delete record.');
    }
  }
};

// Open Dynamic Creation Modal. Pass an existing record (as returned by
// GET /api/v1/doc/{doctype}/{id}) to switch into edit mode instead of
// create - see editDocRecord below, the only caller that does this.
window.openDynamicModal = async function(existingRecord) {
  const modal = document.getElementById('dynamic-modal');
  const title = document.getElementById('dynamic-modal-title');
  const body = document.getElementById('dynamic-modal-body');
  if (!modal) return;

  const isEdit = !!existingRecord;
  title.textContent = `${isEdit ? 'Edit' : 'New'} ${getTranslatedLabel(currentDoctype)}`;
  body.innerHTML = '';

  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);
  const isMaster = activeDoc && activeDoc.document_type === 'Master';

  for (const f of state.activeDocFields) {
    if (f.fieldname === 'id') continue;

    const isCodeField = isMaster && f.fieldname.toLowerCase() === 'code';
    const existingVal = isEdit ? existingRecord[f.fieldname] : undefined;

    const fg = document.createElement('div');
    fg.className = 'form-group';
    fg.innerHTML = `<label class="form-label">${getTranslatedLabel(f.label)}${f.mandatory && !isCodeField ? '<span class="required">*</span>' : ''}</label>`;

    if (f.fieldtype === 'Select') {
      const select = document.createElement('select');
      select.className = 'form-select';
      select.name = f.fieldname;
      select.required = f.mandatory;
      select.innerHTML = '<option value="" disabled selected>— Select Option —</option>';
      const opts = f.options.split(',');
      opts.forEach(o => {
        select.innerHTML += `<option value="${o.trim()}">${o.trim()}</option>`;
      });
      if (existingVal !== undefined && existingVal !== null) select.value = existingVal;
      fg.appendChild(select);
    } else if (f.fieldtype === 'Link') {
      const select = document.createElement('select');
      select.className = 'form-select';
      select.name = f.fieldname;
      select.required = f.mandatory;
      select.innerHTML = '<option value="" disabled selected>— Loading Lookups —</option>';
      fg.appendChild(select);

      // Fetch target link options asynchronously
      apiFetch(`/api/v1/doc/${f.options}`).then(res => {
        if (!res || !res.ok) {
          select.innerHTML = '<option value="" disabled selected>— Failed to load options —</option>';
          return;
        }
        return res.json().then(data => {
          select.innerHTML = '<option value="" disabled selected>— Select Reference —</option>';
          data.forEach(item => {
            select.innerHTML += `<option value="${item.name || item.id}">${item.name || item.code || item.id}</option>`;
          });
          if (existingVal !== undefined && existingVal !== null) select.value = existingVal;
        });
      });
    } else if (f.fieldtype === 'Number') {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.type = 'number';
      input.name = f.fieldname;
      input.required = f.mandatory;
      if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      fg.appendChild(input);
    } else {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.type = 'text';
      input.name = f.fieldname;
      if (isCodeField) {
        // On edit the code already exists and must not be regenerated -
        // show it read-only same as the create-mode placeholder behavior,
        // just with the real value instead of "auto-generated" text.
        input.readOnly = true;
        input.required = false;
        if (isEdit) {
          input.value = existingVal ?? '';
        } else {
          input.placeholder = 'Auto-generated upon save';
        }
      } else {
        input.required = f.mandatory;
        if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      }
      fg.appendChild(input);
    }
    body.appendChild(fg);
  }

  modal.classList.add('open');
};

// 21.9 QA-follow-up: the generic record-list screens (Vendors, Stores,
// Bin Master, everything under Master Definition, etc.) had a Delete
// action but no way to correct a mistake short of delete-and-recreate -
// a real gap USER_GUIDE.md's own §8 claimed didn't exist. Reuses the
// exact same modal/fields/submit path as create, just pre-filled and
// posted to the /{id} update route the generic doc engine already serves.
window.editDocRecord = async function(id) {
  const res = await apiFetch(`/api/v1/doc/${currentDoctype}/${id}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load record for editing.');
    return;
  }
  const record = await res.json();
  editingDocID = id;
  editingDocVersion = typeof record.version === 'number' ? record.version : null;
  await openDynamicModal(record);
};

window.closeDynamicModal = function() {
  const modal = document.getElementById('dynamic-modal');
  if (modal) {
    modal.classList.remove('open');
    document.getElementById('dynamic-modal-form').reset();
  }
  editingDocID = null;
  editingDocVersion = null;
};

window.handleDynamicFormSubmit = async function(e) {
  e.preventDefault();
  const form = document.getElementById('dynamic-modal-form');
  const payload = {};
  
  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);
  const isMaster = activeDoc && activeDoc.document_type === 'Master';
  let codeFieldname = null;

  state.activeDocFields.forEach(f => {
    if (f.fieldname === 'id') return;
    const isCodeField = isMaster && f.fieldname.toLowerCase() === 'code';
    const input = form.querySelector(`[name="${f.fieldname}"]`);
    if (input) {
      if (isCodeField && !input.value) {
        codeFieldname = f.fieldname;
      } else {
        if (f.fieldtype === 'Number') {
          payload[f.fieldname] = parseFloat(input.value);
        } else {
          payload[f.fieldname] = input.value;
        }
      }
    }
  });

  if (codeFieldname) {
    const seqRes = await apiFetch('/api/v1/sequence', {
      method: 'POST',
      body: JSON.stringify({
        doc_type: currentDoctype,
        store_code: 'HQ',
        financial_year: new Date().getFullYear().toString()
      })
    });
    if (seqRes && seqRes.ok) {
      const seqData = await seqRes.json();
      payload[codeFieldname] = seqData.code;
    } else {
      await showApiError(seqRes, 'Failed to generate Code sequence.');
      return;
    }
  }

  const isEdit = !!editingDocID;
  if (isEdit && editingDocVersion !== null) {
    payload.expected_version = editingDocVersion;
  }
  const endpoint = isEdit ? `/api/v1/doc/${currentDoctype}/${editingDocID}` : `/api/v1/doc/${currentDoctype}`;
  const res = await apiFetch(endpoint, {
    method: 'POST',
    body: JSON.stringify(payload)
  });

  if (res && res.ok) {
    closeDynamicModal();
    renderView('doctype-table');
  } else if (res) {
    await showApiError(res, isEdit ? 'Failed to save changes - someone else may have edited this record, refresh and try again.' : 'Failed to save record.');
  }
};

// Render Database Schema Design UI (internal name still DocType Builder -
// see docs/micro_checklist.md Stage 2.1 for the historical build record)
async function renderDocTypeBuilderView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Database Schema Design</h1>
      <p class="page-subtitle">Configure schema structures, define dynamic fields, and setup RBAC rules.</p>
    </div>
    <button class="btn btn-primary" onclick="openNewDoctypeModal()">
      <span>Register New Record Type</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.display = 'grid';
  panel.style.gridTemplateColumns = '250px 1fr';
  panel.style.gap = '24px';
  panel.style.padding = '24px';

  // Module-only list, each module's own record types revealed in a hover
  // flyout - reuses the sidebar's own .has-flyout/.menu-flyout mechanism
  // (setupModuleFlyouts()/openFlyout()/closeSubmenus()) rather than a
  // second one, per explicit user request to feel identical to the sidebar
  // ("I will hover and select"). Every doctype already carries a `module`
  // (set via openNewDoctypeModal's "Module Group" prompt below).
  const doctypesByModule = {};
  state.activeDoctypes.forEach(d => {
    const mod = d.module || 'Other';
    (doctypesByModule[mod] = doctypesByModule[mod] || []).push(d);
  });

  let listHTML = `<ul class="doctype-module-list" style="border-right: 1px solid var(--border-color); padding-right: 16px; list-style: none;">`;
  Object.keys(doctypesByModule).sort().forEach(mod => {
    listHTML += `
      <li class="menu-item-container has-flyout">
        <a class="menu-item menu-item-group" href="#">
          <span>${mod}</span>
          <svg class="menu-item-arrow flyout-arrow" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="9 6 15 12 9 18"></polyline>
          </svg>
        </a>
        <ul class="menu-flyout">
          ${doctypesByModule[mod].map(d => `<li><a class="menu-item" onclick="loadDoctypeConfig('${d.name}')"><span>${d.name} (${d.document_type})</span></a></li>`).join('')}
        </ul>
      </li>
    `;
  });
  listHTML += `</ul><div id="doctype-fields-config">Hover a module on the left, then select a record type to configure its metadata schema properties.</div>`;
  panel.innerHTML = listHTML;
  container.appendChild(panel);
  setupModuleFlyouts();
}

window.openNewDoctypeModal = async function() {
  const name = await showCustomPrompt('Enter Record Type Name:');
  if (!name) return;
  const module = await showCustomPrompt('Enter Module Group (e.g. Master Data, Procurement):');
  if (!module) return;
  const docType = await showCustomPrompt('Document Type (Master/Transaction):');
  if (!docType) return;

  const res = await apiFetch('/api/v1/meta/doctypes', {
    method: 'POST',
    body: JSON.stringify({ name, module, document_type: docType })
  });
  if (!res) return;
  if (res.ok) {
    await fetchRegisteredDoctypes();
    renderView('doctype-builder');
  } else {
    await showApiError(res, 'Failed to register record type.');
  }
};

window.loadDoctypeConfig = async function(doctypeName) {
  const container = document.getElementById('doctype-fields-config');
  if (!container) return;

  const res = await apiFetch(`/api/v1/doc/${doctypeName}/meta`);
  if (!res) return;
  if (!res.ok) {
    const msg = await getErrorMessage(res, `Failed to load fields for ${doctypeName}.`);
    renderErrorPanel(container, msg, () => loadDoctypeConfig(doctypeName));
    return;
  }
  const fields = await res.json();

  let html = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h3 style="font-size: 18px; font-weight:600;">Fields for ${doctypeName}</h3>
      <button class="btn btn-outline btn-sm" onclick="addNewFieldConfig('${doctypeName}')">Add Field</button>
    </div>
    <table>
      <thead>
        <tr>
          <th>Fieldname</th>
          <th>Label</th>
          <th>Fieldtype</th>
          <th>Mandatory</th>
          <th>Options</th>
          <th>Order</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  fields.forEach(f => {
    html += `
      <tr>
        <td style="font-family: monospace;">${f.fieldname}</td>
        <td>${f.label}</td>
        <td>${f.fieldtype}</td>
        <td>${f.mandatory ? 'Yes' : 'No'}</td>
        <td>${f.options || '—'}</td>
        <td>${f.display_order}</td>
        <td>
          <button class="action-btn action-btn-danger" onclick="deleteFieldConfig('${doctypeName}', '${f.id}')">Delete</button>
        </td>
      </tr>
    `;
  });

  html += `</tbody></table>`;
  container.innerHTML = html;
};

window.addNewFieldConfig = async function(doctypeName) {
  const fieldname = await showCustomPrompt('Enter Field name (technical identifier, e.g. material_weight):');
  if (!fieldname) return;
  const label = await showCustomPrompt('Enter Label (Display text, e.g. Material Weight):');
  if (!label) return;
  const fieldtype = await showCustomPrompt('Enter Fieldtype (Data/Number/Select/Check/Date/Link):');
  if (!fieldtype) return;
  const mandatory = await showCustomConfirm('Is this field mandatory?');
  const options = await showCustomPrompt('Enter Options (Choice list for Select, Target Record Type for Link, else leave blank):');

  const res = await apiFetch(`/api/v1/meta/${doctypeName}/fields`, {
    method: 'POST',
    body: JSON.stringify({
      fieldname,
      label,
      fieldtype,
      mandatory,
      options: options || '',
      display_order: 10
    })
  });
  if (!res) return;
  if (res.ok) {
    loadDoctypeConfig(doctypeName);
  } else {
    await showApiError(res, 'Failed to add field.');
  }
};

window.deleteFieldConfig = async function(doctypeName, fieldID) {
  if (await showCustomConfirm('Delete this field from record type metadata?')) {
    const res = await apiFetch(`/api/v1/meta/${doctypeName}/fields/${fieldID}`, {
      method: 'DELETE'
    });
    if (!res) return;
    if (res.ok) {
      loadDoctypeConfig(doctypeName);
    } else {
      await showApiError(res, 'Failed to delete field.');
    }
  }
};

// Render Prefix configurations view
async function renderPrefixConfigsView(container) {
  const res = await apiFetch('/api/v1/prefix');
  if (!res) return;
  if (!res.ok) {
    const msg = await getErrorMessage(res, 'Failed to load prefix configurations.');
    renderErrorPanel(container, msg, () => renderView('prefix-configs'));
    return;
  }
  state.prefixConfigs = await res.json();

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Prefix Configurations</h1>
      <p class="page-subtitle">Configure Numbering Sequences for dynamic documents.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Record Type</th>
          <th>Prefix</th>
          <th>Separator</th>
          <th>Padding</th>
          <th>Reset Interval</th>
          <th>Status</th>
          <th>Action</th>
        </tr>
      </thead>
      <tbody>
  `;
  state.prefixConfigs.forEach(c => {
    html += `
      <tr>
        <td style="font-weight:600;">${c.doc_type}</td>
        <td style="font-family: monospace;">${c.prefix}</td>
        <td>${c.separator}</td>
        <td>${c.padding_width}</td>
        <td>${c.reset_frequency}</td>
        <td>${c.active_status ? 'Active' : 'Inactive'}</td>
        <td><button class="btn btn-outline btn-sm" onclick="editPrefixConfig('${c.doc_type}')">Edit</button></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

window.editPrefixConfig = async function(docType) {
  const c = state.prefixConfigs.find(x => x.doc_type === docType);
  if (!c) return;

  const prefix = await showCustomPrompt('Enter Prefix:', c.prefix);
  if (!prefix) return;
  const separator = await showCustomPrompt('Enter Separator:', c.separator);
  if (!separator) return;
  const paddingRaw = await showCustomPrompt('Enter Padding Width:', c.padding_width);
  const padding = parseInt(paddingRaw);
  if (!padding) return;
  const reset = await showCustomPrompt('Enter Reset Frequency (ANNUAL/MONTHLY/NEVER):', c.reset_frequency);

  const res = await apiFetch('/api/v1/prefix', {
    method: 'POST',
    body: JSON.stringify({
      doc_type: docType,
      prefix,
      separator,
      padding_width: padding,
      reset_frequency: reset,
      active_status: true
    })
  });
  if (!res) return;
  if (res.ok) {
    renderView('prefix-configs');
  } else {
    await showApiError(res, 'Failed to save prefix configuration.');
  }
};

// Render Dynamic Labels view
function renderDynamicLabelsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Dynamic Labels</h1>
      <p class="page-subtitle">Configure vocabulary overlays and translation dictionary mappings.</p>
    </div>
    <button class="btn btn-primary" onclick="addNewLabelReplacement()">
      <span>Add Translation Rule</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Original Label</th>
          <th>Custom Overlay Translation</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  for (const [orig, custom] of Object.entries(state.labels)) {
    html += `
      <tr>
        <td>${orig}</td>
        <td style="font-weight:600; color:var(--primary-color);">${custom}</td>
        <td>
          <button class="action-btn action-btn-danger" onclick="deleteLabelReplacement('${orig}')">Remove</button>
        </td>
      </tr>
    `;
  }
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

window.addNewLabelReplacement = async function() {
  const orig = await showCustomPrompt('Enter original word/label (exact case-insensitive match, e.g. Brand):');
  if (!orig) return;
  const custom = await showCustomPrompt('Enter replacement overlay label (e.g. Material Grade):');
  if (!custom) return;
  
  const res = await apiFetch('/api/v1/labels', {
    method: 'POST',
    body: JSON.stringify({ original_text: orig, custom_text: custom })
  });
  if (!res) return;
  if (res.ok) {
    await fetchLabels();
    renderView('dynamic-labels');
  } else {
    await showApiError(res, 'Failed to add label translation.');
  }
};

window.deleteLabelReplacement = async function(orig) {
  if (await showCustomConfirm(`Remove label mapping for "${orig}"?`)) {
    const res = await apiFetch(`/api/v1/labels?original_text=${encodeURIComponent(orig)}`, {
      method: 'DELETE'
    });
    if (!res) return;
    if (res.ok) {
      await fetchLabels();
      renderView('dynamic-labels');
    } else {
      await showApiError(res, 'Failed to remove label translation.');
    }
  }
};

// Extension Hooks (client extension-layer admin, docs/extension_hooks_checklist.md
// gap found 2026-07-22): Stage 14.17-14.20 built the whole hook/token
// mechanism (engines/extensions.go) API-only - no screen existed anywhere in
// public/, so an admin needed curl/Postman to register a hook or issue a
// scoped token for a client's own hired developer. Reuses the same
// .table-panel/inline-form conventions Accounting Periods (Stage 20.34)
// already established for this shape of screen (a small create-form panel
// above a list table).
async function renderExtensionHooksView(container) {
  const res = await apiFetch('/api/v1/admin/extension/hooks');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Extension Hooks</h1>
      <p class="page-subtitle">Webhook hooks and scoped tokens for a client's own hired developer - see extension-sdk/README.md.</p>
    </div>
  `;
  container.appendChild(header);

  if (!res.ok) {
    renderErrorPanel(container, 'Failed to load extension hooks.', () => renderView('extension-hooks'));
    return;
  }
  const hooks = await res.json();

  const hookFormPanel = document.createElement('div');
  hookFormPanel.className = 'table-panel';
  hookFormPanel.style.padding = '24px';
  hookFormPanel.style.marginBottom = '24px';
  hookFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Register a Hook</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hook-point">Hook Point</label>
        <select id="hook-point" class="form-select" style="width: 190px;">
          <option value="document.before_save">document.before_save</option>
          <option value="document.after_save">document.after_save</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hook-doctype">Doctype</label>
        <input type="text" id="hook-doctype" class="form-input" placeholder="e.g. Item, or * for every doctype" style="width: 200px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hook-target-url">Target URL (https://)</label>
        <input type="text" id="hook-target-url" class="form-input" placeholder="https://client-endpoint.example.com/hook" style="width: 320px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="hook-timeout">Timeout (ms)</label>
        <input type="number" id="hook-timeout" class="form-input" value="3000" min="1" max="10000" style="width: 100px;">
      </div>
      <button class="btn btn-primary" id="hook-register-btn">Register Hook</button>
    </div>
    <div id="hook-form-error" class="login-error hidden" style="margin-bottom: 0;"></div>
  `;
  container.appendChild(hookFormPanel);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Hook Point</th><th>Doctype</th><th>Target URL</th><th>Enabled</th><th>Timeout</th><th>Created By</th><th>Created</th><th>Actions</th></tr></thead>
      <tbody>
        ${hooks.length === 0
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No extension hooks registered yet.</td></tr>`
          : hooks.map(h => `
            <tr>
              <td>${h.hook_point}</td>
              <td style="font-weight:600;">${h.doctype}</td>
              <td style="font-family: monospace; max-width: 280px; overflow-wrap: anywhere;">${h.target_url}</td>
              <td><span class="badge ${h.enabled ? 'badge-success' : 'badge-secondary'}">${h.enabled ? 'Enabled' : 'Disabled'}</span></td>
              <td>${h.timeout_ms}ms</td>
              <td>${h.created_by || ''}</td>
              <td>${h.created_at ? new Date(h.created_at).toLocaleString() : ''}</td>
              <td>
                <button class="action-btn" onclick="viewExtensionHookLog('${h.id}')">View Log</button>
                <button class="action-btn action-btn-danger" onclick="deleteExtensionHookRow('${h.id}')">Delete</button>
              </td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);

  const tokenPanel = document.createElement('div');
  tokenPanel.className = 'table-panel';
  tokenPanel.style.padding = '24px';
  tokenPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Issue an Extension Token</h2>
    <p style="font-size: 13px; color: var(--text-muted); margin-bottom: 16px;">Scoped read-only credential for a client's own hired developer - locked to one tenant + one doctype, no role, cannot log into the UI.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="token-scope-doctype">Scope Doctype</label>
        <input type="text" id="token-scope-doctype" class="form-input" placeholder="e.g. Item" style="width: 200px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="token-ttl">TTL (minutes, max 1440)</label>
        <input type="number" id="token-ttl" class="form-input" value="60" min="1" max="1440" style="width: 100px;">
      </div>
      <button class="btn btn-primary" id="token-issue-btn">Issue Token</button>
    </div>
    <div id="token-form-error" class="login-error hidden" style="margin-bottom: 0;"></div>
  `;
  container.appendChild(tokenPanel);

  document.getElementById('hook-register-btn').addEventListener('click', async () => {
    const errorEl = document.getElementById('hook-form-error');
    errorEl.classList.add('hidden');
    const hookPoint = document.getElementById('hook-point').value;
    const doctype = document.getElementById('hook-doctype').value.trim();
    const targetUrl = document.getElementById('hook-target-url').value.trim();
    const timeoutMs = parseInt(document.getElementById('hook-timeout').value, 10) || 3000;
    if (!doctype || !targetUrl) {
      errorEl.textContent = 'Doctype and target URL are both required.';
      errorEl.classList.remove('hidden');
      return;
    }
    const createRes = await apiFetch('/api/v1/admin/extension/hooks', {
      method: 'POST',
      body: JSON.stringify({ hook_point: hookPoint, doctype, target_url: targetUrl, timeout_ms: timeoutMs })
    });
    if (!createRes) return;
    if (!createRes.ok) {
      errorEl.textContent = await getErrorMessage(createRes, 'Failed to register hook.');
      errorEl.classList.remove('hidden');
      return;
    }
    const created = await createRes.json();
    await showOneTimeSecretDialog(
      'Hook Registered',
      'HMAC signing secret - shown once, store it now. It is not persisted in plaintext anywhere and cannot be retrieved again:',
      created.secret
    );
    renderView('extension-hooks');
  });

  document.getElementById('token-issue-btn').addEventListener('click', async () => {
    const errorEl = document.getElementById('token-form-error');
    errorEl.classList.add('hidden');
    const scopeDoctype = document.getElementById('token-scope-doctype').value.trim();
    const ttlMinutes = parseInt(document.getElementById('token-ttl').value, 10) || 60;
    if (!scopeDoctype) {
      errorEl.textContent = 'Scope doctype is required.';
      errorEl.classList.remove('hidden');
      return;
    }
    const tokenRes = await apiFetch('/api/v1/admin/extension/token', {
      method: 'POST',
      body: JSON.stringify({ scope_doctype: scopeDoctype, ttl_minutes: ttlMinutes })
    });
    if (!tokenRes) return;
    if (!tokenRes.ok) {
      errorEl.textContent = await getErrorMessage(tokenRes, 'Failed to issue token.');
      errorEl.classList.remove('hidden');
      return;
    }
    const data = await tokenRes.json();
    await showOneTimeSecretDialog(
      'Extension Token Issued',
      `Scoped to doctype "${data.scope_doctype}", expires in ${data.expires_in_minutes} minutes. Shown once - store it now:`,
      data.token
    );
  });
}

window.deleteExtensionHookRow = async function(hookId) {
  if (await showCustomConfirm('Delete this extension hook? Any 3rd-party integration depending on it will stop being called immediately.')) {
    const res = await apiFetch(`/api/v1/admin/extension/hooks/${hookId}`, { method: 'DELETE' });
    if (!res) return;
    if (res.ok) {
      renderView('extension-hooks');
    } else {
      await showApiError(res, 'Failed to delete extension hook.');
    }
  }
};

window.viewExtensionHookLog = function(hookId) {
  currentExtensionHookLogId = hookId;
  renderView('extension-hook-log');
};

async function renderExtensionHookLogView(container) {
  const hookId = currentExtensionHookLogId;
  const res = await apiFetch(`/api/v1/admin/extension/hooks/${hookId}/log`);
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Hook Call Log</h1>
      <p class="page-subtitle">Most recent 100 calls for this hook.</p>
    </div>
    <button class="btn btn-outline" onclick="renderView('extension-hooks')">Back to Extension Hooks</button>
  `;
  container.appendChild(header);

  if (!res.ok) {
    renderErrorPanel(container, 'Failed to load hook log.', () => renderView('extension-hook-log'));
    return;
  }
  const entries = await res.json();

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <table>
      <thead><tr><th>Called At</th><th>Response Status</th><th>Latency</th><th>Error</th></tr></thead>
      <tbody>
        ${entries.length === 0
          ? `<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No calls logged yet for this hook.</td></tr>`
          : entries.map(e => `
            <tr>
              <td>${e.called_at ? new Date(e.called_at).toLocaleString() : ''}</td>
              <td>${e.response_status != null ? `<span class="badge ${e.response_status >= 200 && e.response_status < 300 ? 'badge-success' : 'badge-danger'}">${e.response_status}</span>` : '<span class="badge badge-secondary">-</span>'}</td>
              <td>${e.latency_ms}ms</td>
              <td style="color:#ef4444;">${e.error || ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(panel);
}

// showOneTimeSecretDialog: a 4th use of the existing custom-dialog chrome
// (alongside showCustomAlert/Confirm/Prompt) for the "generated, shown once,
// never retrievable again" pattern extension hook secrets and tokens both
// need - a readonly, pre-selected input so the value can't be accidentally
// edited but is one click away from being copied. Built with DOM property
// assignment (not innerHTML interpolation) specifically because this value
// is a live credential, not just display data.
function showOneTimeSecretDialog(title, message, secretValue) {
  return new Promise((resolve) => {
    const backdrop = document.getElementById('custom-dialog-container');
    const titleEl = document.getElementById('custom-dialog-title');
    const msgEl = document.getElementById('custom-dialog-message');
    const extraEl = document.getElementById('custom-dialog-extra');
    const okBtn = document.getElementById('custom-dialog-ok-btn');
    const cancelBtn = document.getElementById('custom-dialog-cancel-btn');
    const closeBtn = document.getElementById('custom-dialog-close-btn');

    titleEl.textContent = title;
    msgEl.textContent = message;

    extraEl.innerHTML = '';
    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'form-input';
    input.style.cssText = 'width: 100%; margin-top: 12px; font-family: Consolas, Monaco, monospace;';
    input.readOnly = true;
    input.value = secretValue;
    input.addEventListener('click', () => input.select());
    extraEl.appendChild(input);
    extraEl.classList.remove('hidden');
    cancelBtn.style.display = 'none';
    backdrop.classList.remove('hidden');

    input.focus();
    input.select();

    const cleanUp = () => {
      backdrop.classList.add('hidden');
      extraEl.innerHTML = '';
      extraEl.classList.add('hidden');
      cancelBtn.style.display = '';
      okBtn.replaceWith(okBtn.cloneNode(true));
      closeBtn.replaceWith(closeBtn.cloneNode(true));
    };

    document.getElementById('custom-dialog-ok-btn').addEventListener('click', () => { cleanUp(); resolve(true); });
    document.getElementById('custom-dialog-close-btn').addEventListener('click', () => { cleanUp(); resolve(true); });
  });
}

// Render Activity Log (internal name still Log Hub) & panic dashboard logs
async function renderLogHubView(container) {
  const auditRes = await apiFetch('/api/v1/logs/audit');
  const auditLoadFailed = !!auditRes && !auditRes.ok;
  const auditLogs = auditRes && auditRes.ok ? await auditRes.json() : [];

  const sysRes = await apiFetch('/api/v1/logs/system');
  const sysLoadFailed = !!sysRes && !sysRes.ok;
  const systemLogs = sysRes && sysRes.ok ? await sysRes.json() : [];

  // Stage 9.2: Integration payload logs - wire the existing backend endpoint
  // that was previously unreachable from the UI.
  const intRes = await apiFetch('/api/v1/integration/logs');
  const intLoadFailed = !!intRes && !intRes.ok;
  const intLogs = intRes && intRes.ok ? await intRes.json() : [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Activity Log</h1>
      <p class="page-subtitle">Centralized System Audit trail, Middleware Panic recovery trace log console, and Integration payload viewer.</p>
    </div>
    <button class="btn btn-outline" onclick="triggerPanicRecovery()">
      <span>Test Panic Recovery</span>
    </button>
  `;
  container.appendChild(header);

  // Tab switcher for the three log panes
  const tabBar = document.createElement('div');
  tabBar.className = 'tab-bar';
  tabBar.style.cssText = 'display:flex; gap:0; margin-bottom:16px; border-bottom:2px solid var(--border-color);';
  tabBar.innerHTML = `
    <button class="log-hub-tab active" data-tab="audit" style="padding:10px 20px; border:none; background:var(--card-bg); cursor:pointer; font-weight:600; border-bottom:2px solid var(--primary-color); margin-bottom:-2px; color:var(--primary-color);">Audit Logs</button>
    <button class="log-hub-tab" data-tab="system" style="padding:10px 20px; border:none; background:transparent; cursor:pointer; font-weight:500; color:var(--text-muted);">System Errors</button>
    <button class="log-hub-tab" data-tab="integration" style="padding:10px 20px; border:none; background:transparent; cursor:pointer; font-weight:500; color:var(--text-muted);">Integration Payloads</button>
  `;
  container.appendChild(tabBar);

  // Tab content container
  const tabContent = document.createElement('div');
  tabContent.id = 'log-hub-tab-content';
  container.appendChild(tabContent);

  function renderAuditPane() {
    tabContent.innerHTML = `
      <div class="table-panel">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding: 16px 16px 0;">Audit Logs</h3>
        ${auditLoadFailed ? `<p style="padding: 0 16px 12px; color: #ef4444; font-size: 13px;">Failed to load audit logs.</p>` : ''}
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>User</th>
                <th>Action</th>
                <th>Details</th>
                <th>Timestamp</th>
              </tr>
            </thead>
            <tbody>
              ${auditLogs.length === 0 ? '<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No audit logs found.</td></tr>' : auditLogs.map(l => `
                <tr>
                  <td>${l.user_id}</td>
                  <td>${l.action}</td>
                  <td style="font-size:12px;">${l.details}</td>
                  <td style="font-size:11px; white-space:nowrap;">${l.created_at}</td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  function renderSystemPane() {
    tabContent.innerHTML = `
      <div class="table-panel">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding: 16px 16px 0;">System Panic & Error Logs</h3>
        ${sysLoadFailed ? `<p style="padding: 0 16px 12px; color: #ef4444; font-size: 13px;">Failed to load system logs.</p>` : ''}
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Severity</th>
                <th>Module</th>
                <th>Error Message</th>
                <th>Timestamp</th>
              </tr>
            </thead>
            <tbody>
              ${systemLogs.length === 0 ? '<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No system logs found.</td></tr>' : systemLogs.map(l => `
                <tr style="cursor:pointer;" onclick="viewStackTrace('${l.log_id}')">
                  <td><span class="badge badge-secondary">${l.severity}</span></td>
                  <td>${l.module_source}</td>
                  <td style="font-size:12px; color:var(--text-muted);">${l.error_message}</td>
                  <td style="font-size:11px; white-space:nowrap;">${l.created_at}</td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  function renderIntegrationPane() {
    tabContent.innerHTML = `
      <div class="table-panel">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding: 16px 16px 0;">Integration Payloads</h3>
        ${intLoadFailed ? `<p style="padding: 0 16px 12px; color: #ef4444; font-size: 13px;">Failed to load integration logs.</p>` : ''}
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Event</th>
                <th>Status</th>
                <th>Attempts</th>
                <th>Payload</th>
                <th>Timestamp</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              ${intLogs.length === 0 ? '<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No integration payloads found.</td></tr>' : intLogs.map(l => `
                <tr>
                  <td style="font-weight:600;">${l.event_name}</td>
                  <td><span class="badge ${l.status === 'Dispatched' || l.status === 'Success' ? 'badge-success' : l.status === 'Failed' ? 'badge-danger' : 'badge-secondary'}">${l.status}</span></td>
                  <td>${l.attempts}</td>
                  <td style="font-size:11px; max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" title='${JSON.stringify(l.payload || {})}'>${JSON.stringify(l.payload || {})}</td>
                  <td style="font-size:11px; white-space:nowrap;">${l.created_at}</td>
                  <td>
                    ${l.status === 'Failed' ? `<button class="btn btn-sm btn-outline" onclick="retryIntegrationEvent('${l.id}')">Retry</button>` : '<span style="color:var(--text-muted); font-size:12px;">-</span>'}
                  </td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  // Tab switching logic
  tabBar.querySelectorAll('.log-hub-tab').forEach(btn => {
    btn.addEventListener('click', () => {
      tabBar.querySelectorAll('.log-hub-tab').forEach(b => {
        b.style.borderBottom = '2px solid transparent';
        b.style.background = 'transparent';
        b.style.color = 'var(--text-muted)';
        b.classList.remove('active');
      });
      btn.style.borderBottom = '2px solid var(--primary-color)';
      btn.style.background = 'var(--card-bg)';
      btn.style.color = 'var(--primary-color)';
      btn.classList.add('active');

      const tab = btn.getAttribute('data-tab');
      if (tab === 'audit') renderAuditPane();
      else if (tab === 'system') renderSystemPane();
      else if (tab === 'integration') renderIntegrationPane();
    });
  });

  // Default: show audit logs
  renderAuditPane();

  window.viewStackTrace = async function(logId) {
    const log = systemLogs.find(x => x.log_id === logId);
    if (!log) return;
    await showCustomAlert(`Stack Trace for ${logId}:\n\n${log.stack_trace || 'No trace available.'}`, 'Stack Trace');
  };
}

// Stage 9.2: Retry button handler for failed integration events
window.retryIntegrationEvent = async function(eventId) {
  if (!await showCustomConfirm('Retry this failed integration event?')) return;
  const res = await apiFetch('/api/v1/integration/retry', {
    method: 'POST',
    body: JSON.stringify({ event_id: eventId })
  });
  if (!res) return;
  if (res.ok) {
    await showCustomAlert('Integration event queued for retry.', 'Retry Initiated');
    renderView('audit-logs');
  } else {
    await showApiError(res, 'Failed to retry integration event.');
  }
};

window.triggerPanicRecovery = async function() {
  if (await showCustomConfirm('Trigger deliberate panic in backend router to verify system recovery middleware?')) {
    // A non-network response here - even a 500 - IS the success case: it proves
    // the recovery middleware caught the panic and the server is still up.
    // Only a dropped connection (res === null, already surfaced by apiFetch) means recovery failed.
    const res = await apiFetch('/api/v1/debug/panic');
    if (!res) return;
    await showCustomAlert('Panic endpoint hit. Re-checking Activity Log for stack trace registration.', 'System Recovery');
    renderView('audit-logs');
  }
};

function renderMockModuleView(container, view) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${view.charAt(0).toUpperCase() + view.slice(1).replace('-', ' ')}</h1>
      <p class="page-subtitle">Module setup in progress</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '48px';
  panel.style.textAlign = 'center';
  panel.innerHTML = `
    <div style="max-width: 480px; margin: 0 auto; display: flex; flex-direction: column; gap: 16px; align-items: center;">
      <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5">
        <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
      </svg>
      <h2 style="font-size: 20px; font-weight: 600;">Module Setup Pending</h2>
      <p class="text-muted" style="font-size: 14px; line-height: 1.6;">
        This transaction screen (Stage 4+) is configured. Switch to dynamic **Setup** or customize attributes using **Database Schema Design**.
      </p>
      <button class="btn btn-secondary" onclick="setActiveMenu('menu-dashboard'); renderView('dashboard');">Back to Dashboard</button>
    </div>
  `;
  container.appendChild(panel);
}

window.openImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.classList.add('open');
    document.getElementById('import-result-summary').style.display = 'none';
  }
};

window.closeImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.classList.remove('open');
    document.getElementById('import-modal-form').reset();
  }
};

window.downloadImportTemplate = function() {
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const url = `/api/v1/import/${currentDoctype}/template?tenant_id=${tenantID}`;
  
  const link = document.createElement('a');
  link.href = url;
  link.setAttribute('download', `${currentDoctype}_template.csv`);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
};

window.handleBulkImportSubmit = async function(e) {
  e.preventDefault();
  const fileInput = document.getElementById('import-file-input');
  if (!fileInput.files.length) return;

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);

  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';

  const headers = {
    'X-Tenant-ID': tenantID
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const summary = document.getElementById('import-result-summary');
  let res;
  try {
    res = await fetch(`/api/v1/import/${currentDoctype}`, {
      method: 'POST',
      headers,
      body: formData
    });
  } catch (err) {
    summary.style.display = 'block';
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Import Failed:</strong> Unable to reach the server. Please check your connection and try again.`;
    return;
  }

  if (res.ok) {
    const result = await res.json();
    summary.style.display = 'block';
    summary.style.backgroundColor = 'rgba(46, 213, 115, 0.1)';
    summary.style.border = '1px solid rgba(46, 213, 115, 0.3)';
    summary.style.color = '#2ed573';

    let html = `
      <div style="font-weight:600; margin-bottom:8px;">Import Processed Successfully:</div>
      <div>Total Rows Parsed: ${result.total_rows}</div>
      <div>Created: ${(result.created_ids || []).length}</div>
      <div>Updated: ${(result.updated_ids || []).length}</div>
      <div>Failed Rows: ${result.failed_rows}</div>
    `;

    if (result.errors && result.errors.length > 0) {
      html += `<div style="font-weight:600; margin-top:12px; color:#ff4757;">Validation Errors:</div><ul style="padding-left:16px; margin-top:4px;">`;
      result.errors.forEach(err => {
        html += `<li>Row ${err.row_number}: ${err.message}</li>`;
      });
      html += `</ul>`;
      if (result.import_job_id) {
        const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
        html += `<div style="margin-top:8px;"><a href="/api/v1/pim/import-jobs/${result.import_job_id}/errors.csv?tenant_id=${tenantID}" target="_blank">Download error rows (CSV)</a></div>`;
      }
    }

    summary.innerHTML = html;

    setTimeout(() => {
      closeImportModal();
      renderView('doctype-table');
    }, 3000);
  } else {
    summary.style.display = 'block';
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Import Failed:</strong> Server returned an error processing the CSV request.`;
  }
};

// Preview (Stage 15.2): dry-run of the same file - nothing is written,
// shows the create/update/reject breakdown before the user commits.
window.handleBulkImportPreview = async function() {
  const fileInput = document.getElementById('import-file-input');
  if (!fileInput.files.length) {
    await showCustomAlert('Select a CSV file first.', 'No File Selected');
    return;
  }

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);

  const summary = document.getElementById('import-result-summary');
  const res = await apiUpload(`/api/v1/pim/import/${currentDoctype}/preview`, formData);
  if (!res) return;

  summary.style.display = 'block';
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    summary.style.backgroundColor = 'rgba(255, 71, 87, 0.1)';
    summary.style.border = '1px solid rgba(255, 71, 87, 0.3)';
    summary.style.color = '#ff4757';
    summary.innerHTML = `<strong>Preview Failed:</strong> ${data.error || 'Server returned an error processing the CSV request.'}`;
    return;
  }

  const result = await res.json();
  summary.style.backgroundColor = 'rgba(255, 165, 2, 0.1)';
  summary.style.border = '1px solid rgba(255, 165, 2, 0.3)';
  summary.style.color = '#ffa502';
  let html = `
    <div style="font-weight:600; margin-bottom:8px;">Preview (nothing written yet):</div>
    <div>Total Rows: ${result.total_rows}</div>
    <div>Would Create: ${(result.created_ids || []).length}</div>
    <div>Would Update: ${(result.updated_ids || []).length}</div>
    <div>Would Reject: ${result.failed_rows}</div>
  `;
  if (result.errors && result.errors.length > 0) {
    html += `<div style="font-weight:600; margin-top:12px;">Row Errors:</div><ul style="padding-left:16px; margin-top:4px;">`;
    result.errors.forEach(err => { html += `<li>Row ${err.row_number}: ${err.message}</li>`; });
    html += `</ul>`;
  }
  summary.innerHTML = html;
};

// Window load init
window.addEventListener('DOMContentLoaded', bootstrap);
