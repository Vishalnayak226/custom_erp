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
async function getErrorMessage(res, fallback) {
  try {
    const data = await res.clone().json();
    if (data && data.error) return data.error;
  } catch (e) {
    // Body wasn't JSON (some backend handlers use http.Error with a plain
    // text body) - fall through to the fallback message.
  }
  return fallback;
}

async function showApiError(res, fallback) {
  const msg = await getErrorMessage(res, fallback);
  await showCustomAlert(msg, 'Error');
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

let state = {
  activeDoctypes: [],
  activeDocFields: [],
  docData: [],
  prefixConfigs: [],
  labels: {},
  auditLogs: [],
  systemLogs: []
};

let currentView = 'dashboard';
let currentDoctype = '';
let posCart = []; // { sku, available, qty, salePrice, costPrice }
let posLocation = '';
let currentSearchQuery = '';
let currentTablePage = 1;
const itemsPerPage = 10;

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
    logout('Session expired. Please log in again.');
    return null;
  }
  if (response.status === 429) {
    await showCustomAlert('Rate limit exceeded. Please throttle your requests.', 'Rate Limit');
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
    logout('Session expired. Please log in again.');
    return null;
  }
  if (response.status === 429) {
    await showCustomAlert('Rate limit exceeded. Please throttle your requests.', 'Rate Limit');
    return null;
  }
  return response;
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
  if (nameEl) nameEl.textContent = username;
  if (roleEl) roleEl.textContent = role;
  if (avatarEl) avatarEl.textContent = (username.slice(0, 2) || '??').toUpperCase();
}

function logout(message) {
  localStorage.removeItem('erp_token');
  localStorage.removeItem('erp_username');
  localStorage.removeItem('erp_role');
  showLoginScreen();
  if (message) {
    showCustomAlert(message, 'Signed Out');
  }
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
  await fetchLabels();
  await fetchRegisteredDoctypes();
  await restoreLastView();
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

async function fetchRegisteredDoctypes() {
  try {
    const res = await apiFetch('/api/v1/meta/doctypes');
    if (!res) return;
    if (res.ok) {
      state.activeDoctypes = await res.json();
      renderSidebarSubmenu();
    } else {
      await showApiError(res, 'Failed to load registered DocTypes.');
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
    if (d.document_type === 'Master') {
      const li = document.createElement('li');
      li.innerHTML = `<a class="submenu-item" data-view="${d.name}">${getTranslatedLabel(d.name)}</a>`;
      sub.appendChild(li);
    }
  });

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

  document.getElementById('menu-master-definition').addEventListener('click', (e) => {
    e.preventDefault();
    const submenu = document.getElementById('submenu-master');
    const arrow = document.querySelector('#menu-master-definition .menu-item-arrow');
    const isOpen = submenu.classList.toggle('open');
    if (arrow) arrow.classList.toggle('rotated', isOpen);
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

  ['menu-stores', 'menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-dynamic-labels', 'menu-audit-logs'].forEach(id => {
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

  const logoutBtn = document.getElementById('logout-btn');
  if (logoutBtn) {
    logoutBtn.addEventListener('click', async () => {
      if (await showCustomConfirm('Are you sure you want to log out?')) {
        logout();
      }
    });
  }
}

function setActiveMenu(menuId) {
  document.querySelectorAll('.menu-item').forEach(item => item.classList.remove('active'));
  document.querySelectorAll('.submenu-item').forEach(item => item.classList.remove('active'));
  const activeMenu = document.getElementById(menuId);
  if (activeMenu) activeMenu.classList.add('active');
}

function closeSubmenus() {
  document.getElementById('submenu-master').classList.remove('open');
  const arrow = document.querySelector('#menu-master-definition .menu-item-arrow');
  if (arrow) arrow.classList.remove('rotated');
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
  'audit-logs': 'menu-audit-logs'
};

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
      submenu.classList.add('open');
      const arrow = document.querySelector('#menu-master-definition .menu-item-arrow');
      if (arrow) arrow.classList.add('rotated');
      return;
    }
  }
  const menuId = STATIC_VIEW_MENU_IDS[view];
  if (menuId) setActiveMenu(menuId);
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
  } else if (view === 'audit-logs') {
    await renderLogHubView(root);
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
      <span class="stat-label">DocTypes Registered</span>
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
    { title: 'DocType Builder', desc: 'Build schemas and customize properties', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/></svg>`, action: () => { setActiveMenu('menu-doctype-builder'); renderView('doctype-builder'); } },
    { title: 'Dynamic Labels', desc: 'Configure customized nomenclature', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>`, action: () => { setActiveMenu('menu-dynamic-labels'); renderView('dynamic-labels'); } },
    { title: 'Prefix Configs', desc: 'Configure sequential transaction prefixes', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect><line x1="16" y1="2" x2="16" y2="6"></line></svg>`, action: () => { setActiveMenu('menu-prefix-configs'); renderView('prefix-configs'); } },
    { title: 'Log Hub', desc: 'Track audits, panics, and payloads', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path></svg>`, action: () => { setActiveMenu('menu-audit-logs'); renderView('audit-logs'); } }
  ];

  modules.forEach(m => {
    const card = document.createElement('div');
    card.className = 'dashboard-card';
    card.innerHTML = `
      <div class="card-icon">${m.icon}</div>
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

  document.getElementById('pos-location').addEventListener('change', (e) => {
    posLocation = e.target.value.trim();
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

  renderPOSCartTable();
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
    const res = await apiFetch('/api/v1/checkout', {
      method: 'POST',
      body: JSON.stringify({
        cart_number: cartNumber,
        location: posLocation,
        payment_mode: document.getElementById('pos-payment-mode').value,
        customer_id: document.getElementById('pos-customer').value.trim(),
        items: posCart.map(line => ({
          sku: line.sku,
          qty: line.qty,
          sale_price: line.salePrice,
          cost_price: line.costPrice
        }))
      })
    });
    if (!res) return;
    if (!res.ok) {
      errorEl.textContent = await getErrorMessage(res, 'Checkout failed.');
      errorEl.classList.remove('hidden');
      return;
    }
    const data = await res.json();
    posCart = [];
    renderPOSCartTable();
    await showCustomAlert(`Sale ${data.cart_number} completed. Total: ${data.sale_total}`, 'Sale Complete');
  } finally {
    checkoutBtn.disabled = false;
  }
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
  const pointsStr = window.prompt('How many points to redeem?');
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
async function renderFinanceView(container) {
  const res = await apiFetch('/api/v1/finance/trial-balance');
  if (!res) return;

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Finance / GL</h1>
      <p class="page-subtitle">Trial balance across all posted GL accounts.</p>
    </div>
  `;
  container.appendChild(header);

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
          <th>Doctype</th>
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
  { id: 'payables-ageing', label: 'Payables Ageing' }
];

async function renderReportsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Reports</h1>
      <p class="page-subtitle">Current Stock, Sales Register, Vendor Ledger, and Payables Ageing.</p>
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
  }
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to select winning quote.', 'Selection Failed');
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
      <p class="page-subtitle">Attendance, leave, and payroll export. Manage employees under Master Definition.</p>
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
    await showCustomAlert('Failed to load leave record.', 'Update Failed');
    return;
  }
  const leave = await getRes.json();
  leave.status = decision;

  const res = await apiFetch(`/api/v1/doc/Leave/${encodeURIComponent(leaveId)}`, {
    method: 'POST',
    body: JSON.stringify(leave)
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to update leave status.', 'Update Failed');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to capitalise asset.', 'Capitalisation Failed');
    return;
  }
  renderView('assets');
}

async function promptTransferAsset(assetId) {
  const newLocation = window.prompt('New location:');
  if (!newLocation) return;
  const newCustodian = window.prompt('New custodian (optional):') || '';

  const res = await apiFetch('/api/v1/assets/transfer', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId, new_location: newLocation, new_custodian: newCustodian })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to transfer asset.', 'Transfer Failed');
    return;
  }
  renderView('assets');
}

async function promptDisposeAsset(assetId) {
  const confirmed = await showCustomConfirm('This will write off the asset\'s remaining net book value and close it out. Continue?', 'Dispose Asset');
  if (!confirmed) return;
  const disposalType = window.prompt('Disposal type (Sale, Scrap, or WriteOff):', 'Scrap');
  if (!disposalType) return;

  const res = await apiFetch('/api/v1/assets/dispose', {
    method: 'POST',
    body: JSON.stringify({ asset_id: assetId, disposal_type: disposalType })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to dispose asset.', 'Disposal Failed');
    return;
  }
  renderView('assets');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to submit for approval.', 'Submit Failed');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to verify claim.', 'Verification Failed');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to pay claim.', 'Payment Failed');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to issue material.', 'Material Issue Failed');
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
  const data = await res.json();
  if (!res.ok) {
    await showCustomAlert(data.error || 'Failed to complete production order.', 'Completion Failed');
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
let currentPIMTab = 'workbench';
let currentPIMFamilyFilter = '';
let currentPIMSelectedItem = '';
const PIM_TABS = [
  { id: 'workbench', label: 'Workbench' },
  { id: 'families', label: 'Product Families', doctype: 'ProductFamily' },
  { id: 'attributes', label: 'Attribute Definitions', doctype: 'ProductAttributeDef' },
  { id: 'family-attributes', label: 'Family Attributes', doctype: 'ProductFamilyAttribute' },
  { id: 'channels', label: 'Channels', doctype: 'Channel' },
  { id: 'channel-category-map', label: 'Category Mapping', doctype: 'ChannelCategoryMap' },
  { id: 'channel-field-map', label: 'Field Mapping', doctype: 'ChannelFieldMap' }
];

async function renderPIMView(container) {
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
      if (tab.doctype) {
        setActiveMenu('menu-pim');
        closeSubmenus();
        currentDoctype = tab.doctype;
        currentSearchQuery = '';
        currentTablePage = 1;
        renderView('doctype-table');
        return;
      }
      currentPIMTab = tab.id;
      currentPIMSelectedItem = '';
      renderView('pim');
    });
  });

  if (currentPIMTab === 'workbench') {
    await renderPIMWorkbenchTab(container);
  }
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No items found. Create one under Master Definition &raquo; Item.</td></tr>`
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
      <button class="btn btn-outline" id="pim-content-save-btn">Save Draft</button>
      <button class="btn btn-primary" id="pim-content-submit-btn">Submit for Approval</button>
    </div>
    <div id="pim-content-error" class="login-error hidden" style="margin-top: 16px;"></div>

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
      <button class="btn btn-primary" id="pim-publish-btn">Publish</button>
    </div>
    <div id="pim-publish-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <div id="pim-publish-log"></div>
  `;
  container.appendChild(panel);

  document.getElementById('pim-attr-save-btn').addEventListener('click', () => savePIMAttributeValue(itemCode));
  document.getElementById('pim-content-save-btn').addEventListener('click', () => savePIMContent(itemCode, 'Draft'));
  document.getElementById('pim-content-submit-btn').addEventListener('click', () => submitPIMContent(itemCode));
  document.getElementById('pim-media-upload-btn').addEventListener('click', () => uploadPIMMedia(itemCode));
  document.getElementById('pim-publish-btn').addEventListener('click', () => publishPIMItem(itemCode));

  await renderPIMMediaGallery(itemCode);
  await renderPIMPublishSection(itemCode);
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
    <div class="table-panel" style="padding: 8px; width: 140px;" data-media-card="${m.id}">
      <div style="font-size: 11px; font-weight: 600; margin-bottom: 4px;">${m.media_role}</div>
      <img data-media-thumb="${m.id}" style="width: 100%; height: 90px; object-fit: cover; background: var(--bg-secondary); border-radius: 4px;" alt="${m.media_role}">
      <button class="btn btn-outline btn-sm" style="width: 100%; margin-top: 6px;" data-deactivate-media="${m.id}">Deactivate</button>
    </div>
  `).join('');

  // <img> tags can't send an Authorization header, so each thumbnail is
  // fetched as an authenticated blob and swapped in via an object URL
  // rather than pointing src directly at the (auth-gated) file endpoint.
  media.forEach(async (m) => {
    const imgRes = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/file`);
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
    : `<table><thead><tr><th>Channel</th><th>Status</th><th>External ID</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${l.channel_code}</td><td><span class="badge ${l.status === 'Published' ? 'badge-success' : 'badge-danger'}">${l.status}</span></td><td style="font-family: monospace;">${l.external_id || ''}</td><td>${l.created_at || ''}</td></tr>`).join('')
      }</tbody></table>`;
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
  if (!attributeId || !value) {
    errorEl.textContent = 'Attribute and Value are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const id = `${itemCode}::${attributeId}`;
  const res = await apiFetch('/api/v1/doc/ProductAttributeValue', {
    method: 'POST',
    body: JSON.stringify({ id, code: id, item: itemCode, attribute: attributeId, value, status: 'Active' })
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

  let tableHTML = `
    <table>
      <thead>
        <tr>
          ${state.activeDocFields.map(f => `<th>${getTranslatedLabel(f.label)}</th>`).join('')}
          <th style="text-align: right;">Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  if (items.length === 0) {
    tableHTML += `<tr><td colspan="${state.activeDocFields.length + 1}" class="text-center py-8 text-muted">No records found.</td></tr>`;
  } else {
    items.forEach(row => {
      tableHTML += `<tr>`;
      state.activeDocFields.forEach(f => {
        const val = row[f.fieldname] || '';
        if (f.fieldname === 'status') {
          const cls = val === 'Active' ? 'badge-success' : 'badge-secondary';
          tableHTML += `<td><span class="badge ${cls}">${val}</span></td>`;
        } else {
          tableHTML += `<td>${val}</td>`;
        }
      });
      tableHTML += `
        <td style="text-align: right;">
          <button class="action-btn action-btn-danger" onclick="deleteDocRecord('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>
          </button>
        </td>
      </tr>`;
    });
  }

  tableHTML += `</tbody></table>`;
  wrapper.innerHTML = tableHTML;

  paginator.innerHTML = `
    <span>Showing ${total === 0 ? 0 : start + 1}-${end} of ${total}</span>
    <div class="pagination-buttons">
      <button class="pagination-btn" onclick="changeDocPage(${currentTablePage - 1})" ${currentTablePage === 1 ? 'disabled' : ''}>Previous</button>
      <button class="pagination-btn" onclick="changeDocPage(${currentTablePage + 1})" ${currentTablePage === pages ? 'disabled' : ''}>Next</button>
    </div>
  `;
}

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

// Open Dynamic Creation Modal
window.openDynamicModal = async function() {
  const modal = document.getElementById('dynamic-modal');
  const title = document.getElementById('dynamic-modal-title');
  const body = document.getElementById('dynamic-modal-body');
  if (!modal) return;

  title.textContent = `New ${getTranslatedLabel(currentDoctype)}`;
  body.innerHTML = '';

  for (const f of state.activeDocFields) {
    if (f.fieldname === 'id' || f.fieldname === 'status') continue;
    
    const fg = document.createElement('div');
    fg.className = 'form-group';
    fg.innerHTML = `<label class="form-label">${getTranslatedLabel(f.label)}${f.mandatory ? '<span class="required">*</span>' : ''}</label>`;
    
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
        });
      });
    } else if (f.fieldtype === 'Number') {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.type = 'number';
      input.name = f.fieldname;
      input.required = f.mandatory;
      fg.appendChild(input);
    } else {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.type = 'text';
      input.name = f.fieldname;
      input.required = f.mandatory;
      fg.appendChild(input);
    }
    body.appendChild(fg);
  }

  modal.classList.add('open');
};

window.closeDynamicModal = function() {
  const modal = document.getElementById('dynamic-modal');
  if (modal) {
    modal.classList.remove('open');
    document.getElementById('dynamic-modal-form').reset();
  }
};

window.handleDynamicFormSubmit = async function(e) {
  e.preventDefault();
  const form = document.getElementById('dynamic-modal-form');
  const payload = {};
  
  state.activeDocFields.forEach(f => {
    if (f.fieldname === 'id' || f.fieldname === 'status') return;
    const input = form.querySelector(`[name="${f.fieldname}"]`);
    if (input) {
      if (f.fieldtype === 'Number') {
        payload[f.fieldname] = parseFloat(input.value);
      } else {
        payload[f.fieldname] = input.value;
      }
    }
  });

  const res = await apiFetch(`/api/v1/doc/${currentDoctype}`, {
    method: 'POST',
    body: JSON.stringify(payload)
  });

  if (res && res.ok) {
    closeDynamicModal();
    renderView('doctype-table');
  } else if (res) {
    await showApiError(res, 'Failed to save record.');
  }
};

// Render DocType Builder UI
async function renderDocTypeBuilderView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">DocType Builder</h1>
      <p class="page-subtitle">Configure schema structures, define dynamic fields, and setup RBAC rules.</p>
    </div>
    <button class="btn btn-primary" onclick="openNewDoctypeModal()">
      <span>Register New DocType</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.display = 'grid';
  panel.style.gridTemplateColumns = '250px 1fr';
  panel.style.gap = '24px';
  panel.style.padding = '24px';
  
  let listHTML = `<div class="doctype-list" style="border-right: 1px solid var(--border-color); padding-right: 16px; display:flex; flex-direction:column; gap: 8px;">`;
  state.activeDoctypes.forEach(d => {
    listHTML += `<button class="btn btn-secondary text-left" onclick="loadDoctypeConfig('${d.name}')">${d.name} (${d.document_type})</button>`;
  });
  listHTML += `</div><div id="doctype-fields-config">Select a DocType from the left panel to configure its metadata schema properties.</div>`;
  panel.innerHTML = listHTML;
  container.appendChild(panel);
}

window.openNewDoctypeModal = async function() {
  const name = await showCustomPrompt('Enter DocType Name:');
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
    await showApiError(res, 'Failed to register DocType.');
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
  const options = await showCustomPrompt('Enter Options (Choice list for Select, Target DocType for Link, else leave blank):');

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
  if (await showCustomConfirm('Delete this field from doctype metadata?')) {
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
          <th>DocType</th>
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

// Render Log Hub & panic dashboard logs
async function renderLogHubView(container) {
  const auditRes = await apiFetch('/api/v1/logs/audit');
  const auditLoadFailed = !!auditRes && !auditRes.ok;
  const auditLogs = auditRes && auditRes.ok ? await auditRes.json() : [];

  const sysRes = await apiFetch('/api/v1/logs/system');
  const sysLoadFailed = !!sysRes && !sysRes.ok;
  const systemLogs = sysRes && sysRes.ok ? await sysRes.json() : [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Log Hub</h1>
      <p class="page-subtitle">Centralized System Audit trail and Middleware Panic recovery trace log console.</p>
    </div>
    <button class="btn btn-outline" onclick="triggerPanicRecovery()">
      <span>Test Panic Recovery</span>
    </button>
  `;
  container.appendChild(header);

  const grid = document.createElement('div');
  grid.style.display = 'grid';
  grid.style.gridTemplateColumns = '1fr 1fr';
  grid.style.gap = '24px';

  // Audit Logs Pane
  const auditPanel = document.createElement('div');
  auditPanel.className = 'table-panel';
  auditPanel.innerHTML = `
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
          ${auditLogs.map(l => `
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
  `;
  grid.appendChild(auditPanel);

  // System Panic Logs Pane
  const sysPanel = document.createElement('div');
  sysPanel.className = 'table-panel';
  sysPanel.innerHTML = `
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
          ${systemLogs.map(l => `
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
  `;
  grid.appendChild(sysPanel);
  container.appendChild(grid);

  window.viewStackTrace = async function(logId) {
    const log = systemLogs.find(x => x.log_id === logId);
    if (!log) return;
    await showCustomAlert(`Stack Trace for ${logId}:\n\n${log.stack_trace || 'No trace available.'}`, 'Stack Trace');
  };
}

window.triggerPanicRecovery = async function() {
  if (await showCustomConfirm('Trigger deliberate panic in backend router to verify system recovery middleware?')) {
    // A non-network response here - even a 500 - IS the success case: it proves
    // the recovery middleware caught the panic and the server is still up.
    // Only a dropped connection (res === null, already surfaced by apiFetch) means recovery failed.
    const res = await apiFetch('/api/v1/debug/panic');
    if (!res) return;
    await showCustomAlert('Panic endpoint hit. Re-checking Log Hub for stack trace registration.', 'System Recovery');
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
        This transaction screen (Stage 4+) is configured. Switch to dynamic **Master Definitions** or customize attributes using the **DocType Builder**.
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
