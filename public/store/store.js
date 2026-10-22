// Stage 102: the store app - an installable web app (PWA) for store staff.
// Plain JavaScript with no build step, like the rest of public/. The session is
// the ERP's own: same origin, same localStorage keys, so signing in here signs
// in the ERP in this browser too, and the reverse.
//
// This file is the shell: sign-in (password + two-factor code, or a PIN on a
// phone registered to the store, 102.1.4), the top bar and working store, the
// home tiles, approvals, scan an item, the camera, settings (alerts, PIN, this
// phone) and routing. Each further task is an ES module in this folder, loaded
// the first time it is opened (MODULES below): it exports `routes`, a map of
// route name -> { title, render(ctx, params) }. To add a task: add its tile to
// TILES, its routes to MODULES, and the module file to sw.js's SHELL if it must
// open offline.
(() => {
  'use strict';

  const $ = (sel, root = document) => root.querySelector(sel);
  const main = $('#main');
  const bar = $('#bar');
  const barTitle = $('#bar-title');
  const barBack = $('#bar-back');
  const storeChip = $('#store-chip');
  const offlineEl = $('#offline');
  const toastEl = $('#toast');

  const state = { profile: null, stores: null, mfaToken: '', idleMinutes: 0, idleTimer: 0, approvals: 0 };

  const ICON = {
    scan: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3M7 9v6M10 9v6M13 9v6M17 9v6"/></svg>',
    check: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 11l3 3 8-8"/><path d="M20 12v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h9"/></svg>',
    camera: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h3l2-3h6l2 3h3a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z"/><circle cx="12" cy="13" r="3.5"/></svg>',
    today: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/></svg>',
    box: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21 8l-9-5-9 5 9 5 9-5zM3 8v8l9 5 9-5V8M12 13v8"/></svg>',
    count: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 5h11M9 12h11M9 19h11M4 5h1M4 12h1M4 19h1"/></svg>',
    basket: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 9h14l-1.5 10a2 2 0 0 1-2 1.7h-7a2 2 0 0 1-2-1.7L5 9zM9 9l3-5 3 5"/></svg>',
    label: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 12l9-9h8v8l-9 9-8-8z"/><circle cx="16" cy="8" r="1.5"/></svg>',
    low: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l9 16H3L12 3zM12 10v4M12 17v.5"/></svg>',
    gear: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>',
    person: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="8" r="3.5"/><path d="M5 20c.8-3.6 3.6-5.5 7-5.5s6.2 1.9 7 5.5"/></svg>',
    tasks: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h11M9 12h11M9 18h11"/><path d="M4 6l1 1 2-2M4 12l1 1 2-2M4 18l1 1 2-2"/></svg>',
  };

  // ---------- storage (shared with the ERP) ----------
  const ls = {
    get(k) { try { return localStorage.getItem(k) || ''; } catch (_) { return ''; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (_) { /* private mode */ } },
    del(k) { try { localStorage.removeItem(k); } catch (_) { /* private mode */ } },
    json(k, fallback) { try { return JSON.parse(localStorage.getItem(k) || 'null') ?? fallback; } catch (_) { return fallback; } },
    setJSON(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch (_) { /* private mode */ } },
  };
  const tenantID = () => ls.get('erp_tenant_id') || 'default';
  const LAST_ACTIVE = 'store_app_last_active';
  // 102.1.4: this phone's registration (kept across sign-outs) and the PIN
  // session's lock-after minutes (cleared with the session).
  const DEVICE_KEY = 'store_app_device';
  const PIN_IDLE_KEY = 'store_app_pin_idle';
  const RECENT_NAMES = 'store_app_recent_names';
  const device = () => ls.json(DEVICE_KEY, null);

  // ---------- small helpers ----------
  const esc = (v) => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const words = (s) => String(s || '').replace(/_/g, ' ').replace(/([a-z0-9])([A-Z])/g, '$1 $2').replace(/^./, c => c.toUpperCase());

  function money(n, currency) {
    const v = Number(n || 0);
    try {
      return new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'INR', maximumFractionDigits: 2 }).format(v);
    } catch (_) {
      return v.toFixed(2);
    }
  }
  const qty = (n) => Number(n || 0).toLocaleString(undefined, { maximumFractionDigits: 2 });

  function fmtDate(ymd) {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(ymd || ''));
    if (!m) return String(ymd || '');
    return new Date(+m[1], +m[2] - 1, +m[3]).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }

  function ago(iso) {
    const t = Date.parse(iso);
    if (!t) return '';
    const mins = Math.max(0, Math.round((Date.now() - t) / 60000));
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins} min ago`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `${hours} h ago`;
    return `${Math.round(hours / 24)} days ago`;
  }

  const newKey = () => (crypto.randomUUID ? crypto.randomUUID() : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`);

  let toastTimer = 0;
  function toast(text, isError) {
    toastEl.textContent = text;
    toastEl.className = 'toast' + (isError ? ' error' : '');
    toastEl.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toastEl.hidden = true; }, isError ? 6000 : 3500);
  }

  class AppError extends Error {
    constructor(message, quiet, status) { super(message); this.quiet = !!quiet; this.status = status || 0; }
  }

  const PASSWORD_MSG = 'An administrator set your password. Open the full ERP, choose your own password, then sign in here.';

  // api calls the ERP. auth:false leaves the saved session out (sign-in);
  // bearer sends a one-off token instead (the two-factor step).
  async function api(path, { method = 'GET', body, auth = true, bearer = '' } = {}) {
    const headers = { 'X-Tenant-ID': tenantID() };
    const token = bearer || (auth ? ls.get('erp_token') : '');
    if (token) headers.Authorization = 'Bearer ' + token;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 20000);
    let res;
    try {
      res = await fetch(path, {
        method, headers, cache: 'no-store', signal: ctrl.signal,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (_) {
      if (!navigator.onLine) throw new AppError('You are offline. Connect to the internet and try again.');
      throw new AppError(method === 'GET'
        ? 'Could not reach the server. Try again.'
        : 'Could not reach the server. Check whether it went through before trying again.');
    } finally {
      clearTimeout(timer);
    }
    if (res.status === 401 && auth && !bearer && ls.get('erp_token')) {
      signOut('Your session ended. Please sign in again.');
      throw new AppError('', true, 401);
    }
    if (res.status === 403 && res.headers.get('X-Password-Change-Required') === '1') {
      signOut(PASSWORD_MSG);
      throw new AppError('', true, 403);
    }
    let data = null;
    try { data = await res.json(); } catch (_) { /* no body */ }
    if (res.status === 429) throw new AppError('Too many requests just now. Wait a few seconds and try again.', false, 429);
    if (!res.ok) {
      // A 4xx message is already specific; the catalog's generic next step
      // is added only when the server could not say what went wrong. A
      // validation answer carries the catalog's generic line in error and the
      // real reason in detail - the reason is what the person needs.
      const detail = data && typeof data.detail === 'string' && data.detail.trim();
      const parts = detail && res.status < 500
        ? [detail.charAt(0).toUpperCase() + detail.slice(1) + (/[.!?]$/.test(detail) ? '' : '.')]
        : [data && data.error, res.status >= 500 && data && data.user_action].filter(Boolean);
      throw new AppError(parts.join(' ') || `Something went wrong (${res.status}).`, false, res.status);
    }
    return data;
  }

  // ---------- idle sign-out: the user's own setting, as in the ERP ----------
  function stopIdle() {
    clearTimeout(state.idleTimer);
    state.idleTimer = 0;
    state.idleMinutes = 0;
  }
  function armIdle(minutes) {
    stopIdle();
    state.idleMinutes = Number(minutes) || 0;
    noteActivity();
  }
  function idleExpired() {
    const minutes = state.idleMinutes;
    const last = Number(ls.get(LAST_ACTIVE)) || 0;
    return minutes > 0 && last > 0 && Date.now() - last > minutes * 60000;
  }
  function idleSignOut() {
    signOut(`Signed out after ${state.idleMinutes} minutes without use.`);
  }
  function noteActivity() {
    if (!state.idleMinutes) return;
    ls.set(LAST_ACTIVE, String(Date.now()));
    clearTimeout(state.idleTimer);
    state.idleTimer = setTimeout(idleSignOut, state.idleMinutes * 60000);
  }
  ['pointerdown', 'keydown'].forEach(evt => document.addEventListener(evt, noteActivity, { capture: true, passive: true }));
  // A phone sleeps and timers stop; check the clock when the app comes back.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      closeCamera();
    } else if (ls.get('erp_token') && idleExpired()) {
      idleSignOut();
    }
  });

  function signOut(message) {
    ['erp_token', 'erp_username', 'erp_role', LAST_ACTIVE, PIN_IDLE_KEY].forEach(k => ls.del(k));
    state.profile = null;
    state.stores = null;
    stopIdle();
    showSignIn(message || '');
  }

  const pinSession = () => !!ls.get(PIN_IDLE_KEY);
  const isManager = () => {
    const role = String((state.profile && state.profile.role) || ls.get('erp_role') || '').toLowerCase();
    return role === 'store manager' || role === 'admin' || role === 'super admin' || role === 'hr/admin';
  };

  // ---------- top bar ----------
  function setBar(title, back) {
    bar.hidden = false;
    barTitle.textContent = title;
    barBack.hidden = !back;
    barBack.dataset.to = typeof back === 'string' ? back : '';
    renderStoreChip();
  }
  function storeName(code) {
    const found = ((state.stores && state.stores.stores) || []).find(s => s.code === code);
    return found ? found.name : code;
  }
  const workingStore = () => (state.stores && state.stores.working) || (state.profile && state.profile.location_code) || '';
  function renderStoreChip() {
    const s = state.stores;
    if (!s) { storeChip.hidden = true; return; }
    const working = workingStore();
    // A PIN session runs at its phone's store only (102.1.4).
    const canSwitch = !pinSession() && (!!s.all || (s.stores || []).length > 1);
    storeChip.textContent = working ? storeName(working) : 'Choose store';
    storeChip.disabled = !canSwitch;
    storeChip.setAttribute('aria-label', `Working store: ${storeChip.textContent}${canSwitch ? '. Change store' : ''}`);
    storeChip.hidden = false;
  }
  // A module's own page may name where Back goes (e.g. a delivery back to its
  // list); otherwise Back is home.
  barBack.addEventListener('click', () => { location.hash = barBack.dataset.to || '#/home'; });
  storeChip.addEventListener('click', () => { if (!storeChip.disabled) location.hash = '#/stores'; });

  function panelError(message, retry) {
    // Offline, the banner above already says so; just offer the retry.
    const msg = navigator.onLine ? `<div class="msg msg-error" role="alert">${esc(message)}</div>` : '';
    main.innerHTML = `${msg}<button type="button" class="btn btn-block" id="retry">Try again</button>`;
    $('#retry').addEventListener('click', retry);
  }

  // ---------- sign in ----------
  function showSignIn(message, forcePassword) {
    closeCamera();
    bar.hidden = true;
    const dev = device();
    if (dev && !forcePassword) { showPINSignIn(message, dev); return; }
    main.innerHTML = `
      <form class="signin" id="signin-form">
        <img class="signin-mark" src="icon.svg" alt="">
        <h1>Store</h1>
        ${message ? `<div class="msg msg-info" role="alert">${esc(message)}</div>` : ''}
        <div class="msg msg-error" id="signin-error" role="alert" hidden></div>
        <label class="field"><span>Username</span>
          <input class="input" id="si-user" name="username" autocomplete="username" autocapitalize="none" spellcheck="false" required></label>
        <label class="field"><span>Password</span>
          <input class="input" id="si-pass" name="password" type="password" autocomplete="current-password" required></label>
        <button class="btn btn-primary btn-block" id="si-submit" type="submit">Sign in</button>
        <div class="links">
          ${dev ? '<button type="button" class="link" id="si-pin">Sign in with a PIN</button>' : ''}
          <a class="link" href="/">Open the full ERP</a>
        </div>
      </form>`;
    $('#signin-form').addEventListener('submit', onSignIn);
    const pinLink = $('#si-pin');
    if (pinLink) pinLink.addEventListener('click', () => showSignIn(''));
  }

  // 102.1.4: on a phone registered to the store, each person signs in with
  // their own name and PIN.
  function showPINSignIn(message, dev) {
    const names = ls.json(RECENT_NAMES, []);
    main.innerHTML = `
      <form class="signin" id="pin-form" autocomplete="off">
        <img class="signin-mark" src="icon.svg" alt="">
        <h1>${esc(dev.store_name || dev.store)}</h1>
        <p class="hint">This phone belongs to the store. Sign in with your own name and PIN.</p>
        ${message ? `<div class="msg msg-info" role="alert">${esc(message)}</div>` : ''}
        <div class="msg msg-error" id="pin-error" role="alert" hidden></div>
        <label class="field"><span>Your username</span>
          <input class="input" id="pin-user" list="pin-names" autocapitalize="none" spellcheck="false" required></label>
        <datalist id="pin-names">${names.map(n => `<option value="${esc(n)}">`).join('')}</datalist>
        <label class="field"><span>PIN</span>
          <input class="input pin-input" id="pin-code" type="password" inputmode="numeric" pattern="[0-9]{4,8}" maxlength="8" autocomplete="off" required></label>
        <button class="btn btn-primary btn-block" id="pin-submit" type="submit">Sign in</button>
        <p class="hint">First time on this phone? Sign in with your password once, then set your PIN in Settings.</p>
        <div class="links"><button type="button" class="link" id="pin-password">Sign in with password instead</button></div>
      </form>`;
    $('#pin-password').addEventListener('click', () => showSignIn('', true));
    const user = $('#pin-user');
    if (names.length === 1) user.value = names[0];
    (user.value ? $('#pin-code') : user).focus();
    $('#pin-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const errEl = $('#pin-error');
      const btn = $('#pin-submit');
      errEl.hidden = true;
      btn.disabled = true;
      try {
        const s = await api('/api/v1/store-app/pin/login', {
          method: 'POST', auth: false,
          body: { device_token: dev.token, username: user.value.trim(), pin: $('#pin-code').value.trim() },
        });
        ls.set(PIN_IDLE_KEY, String(s.idle_minutes || 5));
        ls.setJSON(RECENT_NAMES, [s.user, ...names.filter(n => n !== s.user)].slice(0, 10));
        await finishSignIn({ token: s.token, user: s.user, role: s.role });
      } catch (ex) {
        if (ex.status === 401 && /no longer registered/i.test(ex.message)) {
          ls.del(DEVICE_KEY);
          showSignIn(ex.message, true);
          return;
        }
        if (!ex.quiet) formError(errEl, ex.message);
        $('#pin-code').value = '';
      } finally {
        btn.disabled = false;
      }
    });
    // A removed phone says so before anyone types.
    api('/api/v1/store-app/device/check', { method: 'POST', auth: false, body: { device_token: dev.token } })
      .then(d => { if (d && d.store_name && d.store_name !== dev.store_name) ls.setJSON(DEVICE_KEY, { ...dev, store_name: d.store_name }); })
      .catch(ex => {
        if (ex.status === 404) {
          ls.del(DEVICE_KEY);
          showSignIn(ex.message, true);
        }
      });
  }

  function formError(el, message) {
    el.textContent = message;
    el.hidden = false;
  }

  async function onSignIn(e) {
    e.preventDefault();
    const errEl = $('#signin-error');
    const btn = $('#si-submit');
    errEl.hidden = true;
    btn.disabled = true;
    try {
      const data = await api('/api/v1/login', {
        method: 'POST', auth: false,
        body: { username: $('#si-user').value.trim(), password: $('#si-pass').value },
      });
      if (data.mfa_enrollment_required) {
        formError(errEl, 'Two-factor sign-in is not set up for this account yet. Open the full ERP once to set it up, then sign in here.');
        return;
      }
      if (data.mfa_required) {
        state.mfaToken = data.challenge_token;
        showMFA();
        return;
      }
      await finishSignIn(data);
    } catch (ex) {
      if (!ex.quiet) formError(errEl, ex.message);
    } finally {
      btn.disabled = false;
    }
  }

  function showMFA() {
    main.innerHTML = `
      <form class="signin" id="mfa-form">
        <img class="signin-mark" src="icon.svg" alt="">
        <h1>Two-factor code</h1>
        <div class="msg msg-error" id="mfa-error" role="alert" hidden></div>
        <label class="field"><span id="mfa-label">Code from your authenticator app</span>
          <input class="input" id="mfa-code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" required></label>
        <button class="btn btn-primary btn-block" id="mfa-submit" type="submit">Verify</button>
        <div class="links">
          <button type="button" class="link" id="mfa-recovery">Use a recovery code</button>
          <button type="button" class="link" id="mfa-back">Back to sign in</button>
        </div>
      </form>`;
    const input = $('#mfa-code');
    input.focus();
    $('#mfa-recovery').addEventListener('click', (ev) => {
      input.removeAttribute('pattern');
      input.removeAttribute('maxlength');
      input.setAttribute('inputmode', 'text');
      input.value = '';
      $('#mfa-label').textContent = 'Recovery code';
      ev.currentTarget.hidden = true;
      input.focus();
    });
    $('#mfa-back').addEventListener('click', () => { state.mfaToken = ''; showSignIn('', true); });
    $('#mfa-form').addEventListener('submit', async (ev) => {
      ev.preventDefault();
      const errEl = $('#mfa-error');
      const btn = $('#mfa-submit');
      errEl.hidden = true;
      btn.disabled = true;
      try {
        const data = await api('/api/v1/auth/mfa/verify', {
          method: 'POST', bearer: state.mfaToken, body: { code: input.value.trim() },
        });
        await finishSignIn(data);
      } catch (ex) {
        if (!ex.quiet) formError(errEl, ex.message);
      } finally {
        btn.disabled = false;
      }
    });
  }

  async function finishSignIn(data) {
    state.mfaToken = '';
    if (data.password_change_required === 'true') {
      showSignIn(PASSWORD_MSG, true);
      return;
    }
    ls.set('erp_token', data.token);
    ls.set('erp_username', data.user || '');
    ls.set('erp_role', data.role || '');
    ls.set(LAST_ACTIVE, String(Date.now()));
    if (data.used_recovery_code) {
      toast(`Signed in with a recovery code - ${Number(data.recovery_codes_remaining || 0)} left. Set up a new authenticator in the full ERP (Profile).`);
    }
    if (!/^#\/[a-z]/.test(location.hash) || /^#\/(home)?$/.test(location.hash)) history.replaceState(null, '', '#/home');
    await boot();
  }

  // ---------- home ----------
  // Each tile is a task; show() decides who sees it. Feature tiles open a
  // module from MODULES.
  const TILES = [
    { route: 'scan', label: 'Scan an item', icon: 'scan' },
    { route: 'approvals', label: 'Approvals', icon: 'check', badge: true },
    { route: 'today', label: 'Today', icon: 'today', show: isManager },
    { route: 'receive', label: 'Receive', icon: 'box' },
    { route: 'count', label: 'Count', icon: 'count' },
    { route: 'customer', label: 'Serve a customer', icon: 'person' },
    { route: 'tasks', label: 'My tasks', icon: 'tasks' },
    { route: 'basket', label: 'Basket for the till', icon: 'basket' },
    { route: 'labels', label: 'Labels', icon: 'label' },
    { route: 'low-stock', label: 'Low stock', icon: 'low' },
  ];

  async function renderHome() {
    setBar('Store', false);
    main.innerHTML = `
      <div class="tiles">${TILES.filter(t => !t.show || t.show()).map(t => `
        <a class="tile" href="#/${t.route}">${ICON[t.icon] || ''}<span>${esc(t.label)}</span>${t.badge ? '<span class="badge" id="appr-badge" hidden></span>' : ''}</a>`).join('')}
      </div>
      <div class="links">
        <a class="link" href="#/settings">Settings</a>
        <a class="link" href="/">Open the full ERP</a>
        <button type="button" class="link" id="signout">Sign out</button>
      </div>`;
    $('#signout').addEventListener('click', () => signOut('You are signed out.'));
    syncQueuedCounts();
    try {
      const inbox = await api('/api/v1/inbox');
      const n = (inbox.items || []).filter(i => i.source === 'Approval').length;
      const badge = $('#appr-badge');
      if (badge && n) {
        badge.textContent = n > 99 ? '99+' : String(n);
        badge.setAttribute('aria-label', `${n} waiting`);
        badge.hidden = false;
      }
    } catch (_) { /* the tile still opens the list, which shows the error */ }
  }

  // ---------- approvals ----------
  async function renderApprovals() {
    setBar('Approvals', true);
    main.innerHTML = '<p class="loading">Loading…</p>';
    let inbox;
    try {
      inbox = await api('/api/v1/inbox');
    } catch (e) {
      if (!e.quiet) panelError(e.message, renderApprovals);
      return;
    }
    if (routeName() !== 'approvals') return;
    const items = (inbox.items || []).filter(i => i.source === 'Approval');
    const failed = inbox.errors && inbox.errors.approvals;
    main.innerHTML = `
      <div class="toolbar"><span id="appr-count"></span><button type="button" class="link" id="appr-refresh">Refresh</button></div>
      ${failed ? `<div class="msg msg-error" role="alert">Some approvals could not be read: ${esc(failed)}</div>` : ''}
      <div id="appr-list">${items.map(approvalCard).join('')}</div>`;
    $('#appr-refresh').addEventListener('click', renderApprovals);
    main.querySelectorAll('[data-card]').forEach(wireApprovalCard);
    updateApprovalCount();
  }

  function updateApprovalCount() {
    const list = $('#appr-list');
    if (!list) return;
    const n = list.querySelectorAll('[data-card]').length;
    $('#appr-count').textContent = n ? `${n} waiting for you` : '';
    if (!n && !list.querySelector('.empty')) list.insertAdjacentHTML('beforeend', '<p class="empty">Nothing is waiting for your approval.</p>');
  }

  function approvalCard(item) {
    const label = words(item.doctype);
    const prefix = `${item.doctype} `;
    const ref = String(item.title || '').startsWith(prefix) ? item.title.slice(prefix.length) : (item.title || item.doc_id);
    const waiting = ago(item.waiting_since);
    return `
      <article class="card" data-card data-doctype="${esc(item.doctype)}" data-id="${esc(item.doc_id)}">
        <h2>${esc(label)} · ${esc(ref)}</h2>
        <div class="sub">${esc(item.reason)}${waiting ? ` · waiting ${esc(waiting)}` : ''}</div>
        <button type="button" class="details-toggle" data-act="details" aria-expanded="false">Show details</button>
        <div class="details" hidden></div>
        <label class="field reject-box" hidden><span>Why are you rejecting it?</span>
          <textarea class="textarea" maxlength="500"></textarea></label>
        <div class="actions">
          <button type="button" class="btn btn-danger" data-act="reject">Reject</button>
          <button type="button" class="btn btn-primary" data-act="approve">Approve</button>
        </div>
      </article>`;
  }

  // Fields never worth showing on a phone card.
  const SKIP_FIELD = /^(id|doctype|status|version|exchange_rate|functional_currency|base_.*|created_.*|updated_.*|_.*|.*(password|token|secret|hash).*)$/i;

  function detailsHTML(doc) {
    const rows = [['Status', doc.status]];
    for (const [key, raw] of Object.entries(doc || {})) {
      if (rows.length >= 16) break;
      if (SKIP_FIELD.test(key) || raw === null || raw === '' || raw === undefined) continue;
      let value = raw;
      if (typeof value === 'string' && /^\s*\[/.test(value)) {
        try { value = JSON.parse(value); } catch (_) { /* plain text */ }
      }
      if (Array.isArray(value)) {
        rows.push([words(key), `${value.length} ${value.length === 1 ? 'line' : 'lines'}`]);
        continue;
      }
      if (typeof value === 'object') continue;
      const text = String(value);
      if (text.length > 160) continue;
      rows.push([words(key), text]);
    }
    return `<dl class="kv">${rows.filter(r => r[1] !== undefined && r[1] !== '').map(([k, v]) => `<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join('')}</dl>`;
  }

  function wireApprovalCard(card) {
    const doctype = card.dataset.doctype;
    const id = card.dataset.id;
    const details = $('.details', card);
    const toggle = $('[data-act="details"]', card);
    const rejectBox = $('.reject-box', card);
    const reason = $('textarea', card);
    const approveBtn = $('[data-act="approve"]', card);
    const rejectBtn = $('[data-act="reject"]', card);
    let armTimer = 0;

    toggle.addEventListener('click', async () => {
      const open = details.hidden;
      details.hidden = !open;
      toggle.setAttribute('aria-expanded', String(open));
      toggle.textContent = open ? 'Hide details' : 'Show details';
      if (!open || details.dataset.loaded) return;
      details.innerHTML = '<p class="sub">Loading…</p>';
      try {
        const doc = await api(`/api/v1/doc/${encodeURIComponent(doctype)}/${encodeURIComponent(id)}`);
        details.innerHTML = detailsHTML(doc);
        details.dataset.loaded = '1';
      } catch (e) {
        details.innerHTML = e.quiet ? '' : `<p class="sub">${esc(e.message)}</p>`;
      }
    });

    // Approve takes two taps: a phone in a pocket or a slipped thumb must
    // never approve a document by itself.
    approveBtn.addEventListener('click', () => {
      if (!approveBtn.dataset.armed) {
        approveBtn.dataset.armed = '1';
        approveBtn.textContent = 'Tap again to approve';
        approveBtn.classList.add('btn-confirm');
        clearTimeout(armTimer);
        armTimer = setTimeout(() => {
          delete approveBtn.dataset.armed;
          approveBtn.textContent = 'Approve';
          approveBtn.classList.remove('btn-confirm');
        }, 4000);
        return;
      }
      clearTimeout(armTimer);
      decide(card, doctype, id, 'Approved', '');
    });

    rejectBtn.addEventListener('click', () => {
      if (rejectBox.hidden) {
        rejectBox.hidden = false;
        rejectBtn.textContent = 'Confirm reject';
        reason.focus();
        return;
      }
      const why = reason.value.trim();
      if (!why) {
        reason.focus();
        toast('Say why it is rejected - the person who sent it needs to know.', true);
        return;
      }
      decide(card, doctype, id, 'Rejected', why);
    });
  }

  async function decide(card, doctype, id, decision, comment) {
    const buttons = card.querySelectorAll('.actions .btn');
    buttons.forEach(b => { b.disabled = true; });
    let data;
    try {
      data = await api('/api/v1/approval/decide', { method: 'POST', body: { doctype, document_id: id, decision, comment } });
    } catch (e) {
      buttons.forEach(b => { b.disabled = false; });
      if (!e.quiet) toast(e.message, true);
      return;
    }
    const label = words(doctype);
    // A decision can carry something the approver must see now and never
    // again (a reset password, 49.2.4): keep it on screen until dismissed.
    if (data && data.detail) {
      card.innerHTML = `<div class="msg msg-info" role="alert">${esc(data.detail)}</div>
        <button type="button" class="btn btn-block">Done</button>`;
      $('.btn', card).addEventListener('click', () => { card.remove(); updateApprovalCount(); });
      return;
    }
    const status = data && data.document_status;
    toast(status ? `${label} is now ${status}.` : `${label} ${decision === 'Approved' ? 'approved' : 'rejected'}.`);
    card.remove();
    updateApprovalCount();
  }

  // ---------- working store ----------
  async function renderStores() {
    setBar('Working store', true);
    if (pinSession()) {
      main.innerHTML = '<p class="empty">This phone is registered to one store, so it works there only. Sign in with your password to work at another store.</p>';
      return;
    }
    main.innerHTML = '<p class="loading">Loading…</p>';
    let s;
    try {
      s = await api('/api/v1/me/stores');
    } catch (e) {
      if (!e.quiet) panelError(e.message, renderStores);
      return;
    }
    if (routeName() !== 'stores') return;
    state.stores = s;
    renderStoreChip();
    const stores = s.stores || [];
    if (!stores.length) {
      main.innerHTML = '<p class="empty">No stores are set up for you. Ask your administrator.</p>';
      return;
    }
    main.innerHTML = `
      ${stores.length > 8 ? '<label class="field"><span>Find a store</span><input class="input" id="store-filter" autocomplete="off"></label>' : ''}
      <div id="store-list">${stores.map(o => `
        <button type="button" class="pick" data-store="${esc(o.code)}" data-search="${esc((o.name + ' ' + o.code).toLowerCase())}" aria-current="${o.code === s.working}">
          <span>${esc(o.name)} <small>${esc(o.code)}</small></span>${o.code === s.home ? '<small>Home</small>' : ''}
        </button>`).join('')}</div>`;
    const filter = $('#store-filter');
    if (filter) {
      filter.addEventListener('input', () => {
        const q = filter.value.trim().toLowerCase();
        main.querySelectorAll('.pick').forEach(b => { b.hidden = !!q && !b.dataset.search.includes(q); });
      });
    }
    main.querySelectorAll('.pick').forEach(b => b.addEventListener('click', () => switchStore(b.dataset.store, b)));
  }

  async function switchStore(code, button) {
    if (state.stores && code === state.stores.working) { location.hash = '#/home'; return; }
    button.disabled = true;
    try {
      const data = await api('/api/v1/me/working-store', { method: 'POST', body: { store: code } });
      ls.set('erp_token', data.token);
      state.stores.working = data.store;
      toast(`Working at ${storeName(data.store)}.`);
      refreshPushStore();
      location.hash = '#/home';
    } catch (e) {
      button.disabled = false;
      if (!e.quiet) toast(e.message, true);
    }
  }

  // ---------- scanning, shared by every screen that scans ----------
  const cameraSupported = () => 'BarcodeDetector' in window && !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia);

  // scanBox draws the code box (and the camera button where the browser can
  // read barcodes) and calls onCode for each code: Enter, a scanner burst
  // without Enter (Stage 77's rule), or the camera.
  function scanBox(container, onCode, { placeholder = 'Scan or type a code', button = 'Add' } = {}) {
    container.innerHTML = `
      <form class="scan-form" autocomplete="off">
        <input class="input" aria-label="Barcode, item code or batch number" placeholder="${esc(placeholder)}"
          autocapitalize="characters" autocorrect="off" spellcheck="false" enterkeyhint="go">
        <button class="btn btn-primary" type="submit">${esc(button)}</button>
      </form>
      ${cameraSupported()
        ? `<button type="button" class="btn btn-block scan-cam">${ICON.camera}Scan with camera</button>`
        : '<p class="hint">This browser cannot read barcodes with the camera. Use a Bluetooth scanner, or type the code.</p>'}`;
    const input = $('input', container);
    const fire = () => {
      const code = input.value.trim();
      if (!code) return;
      input.value = '';
      onCode(code);
    };
    $('form', container).addEventListener('submit', (e) => { e.preventDefault(); fire(); });
    const cam = $('.scan-cam', container);
    if (cam) cam.addEventListener('click', () => openCamera(code => onCode(code)));
    let lastKey = 0;
    let burst = 0;
    let burstTimer = 0;
    input.addEventListener('input', () => {
      const now = performance.now();
      burst = now - lastKey < 35 ? burst + 1 : 1;
      lastKey = now;
      clearTimeout(burstTimer);
      if (burst >= 6) burstTimer = setTimeout(fire, 150);
    });
    if (!matchMedia('(pointer: coarse)').matches) input.focus();
    return input;
  }

  // ---------- scan an item ----------
  let lookingUp = '';

  function renderScan(params) {
    setBar('Scan an item', true);
    main.innerHTML = '<div id="scan-box"></div><div id="scan-result"></div>';
    const input = scanBox($('#scan-box'), lookup, { button: 'Look up' });
    input.setAttribute('id', 'scan-code');
    const code = params.get('code');
    if (code) lookup(code);
  }

  async function lookup(raw) {
    const code = String(raw || '').trim();
    const out = $('#scan-result');
    if (!code || !out || lookingUp === code) return;
    lookingUp = code;
    out.innerHTML = '<p class="loading">Looking up…</p>';
    try {
      const item = await api(`/api/v1/store-app/item?code=${encodeURIComponent(code)}`);
      if ($('#scan-result') === out) {
        out.innerHTML = itemHTML(item);
        wireItemActions(out, item);
      }
    } catch (e) {
      if (!e.quiet && $('#scan-result') === out) out.innerHTML = `<div class="msg msg-error" role="alert">${esc(e.message)}</div>`;
    } finally {
      lookingUp = '';
    }
  }

  function itemHTML(it) {
    const cur = it.currency;
    const tags = [it.brand, it.category, it.status && it.status !== 'Active' ? it.status : '']
      .filter(Boolean).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
    const showMRP = it.mrp > 0 && Math.abs(it.mrp - it.price) > 0.004;
    const here = (it.stock || []).find(s => s.working);
    return `
      <article class="card">
        <div class="item-head">
          ${it.image ? `<img class="item-img" src="${esc(it.image)}" alt="">` : ''}
          <div>
            <h2 class="item-name">${esc(it.name)}</h2>
            <div class="item-code">${esc(it.code)}</div>
            ${it.barcode && it.barcode !== it.code ? `<div class="item-code">${esc(it.barcode)}</div>` : ''}
            ${tags}
          </div>
        </div>
        <div class="price-row">
          ${it.price > 0 ? `<span class="price">${esc(money(it.price, cur))}</span>` : '<span class="mrp">No price set</span>'}
          ${showMRP ? `<span class="mrp">MRP <s>${esc(money(it.mrp, cur))}</s></span>` : (it.mrp > 0 && it.price > 0 ? '<span class="mrp">MRP</span>' : '')}
        </div>
        ${it.batch ? batchHTML(it.batch) : ''}
        <div class="item-actions">
          <button type="button" class="btn" data-act="basket">Add to basket</button>
          <button type="button" class="btn" data-act="labels">Print labels</button>
          ${!here || here.available <= 0 ? '<button type="button" class="btn" data-act="order">Order for home delivery</button>' : ''}
          <button type="button" class="btn" data-act="ask">Ask for stock</button>
        </div>
      </article>
      <h3 class="section-title">Stock</h3>
      ${stockHTML(it)}
      ${(it.batches || []).length ? `
        <h3 class="section-title">Oldest batches with stock left (all stores, estimated)</h3>
        <ul class="rows">${it.batches.map(b => `
          <li><span>${esc(b.batch_no)}<small>Received ${esc(fmtDate(b.received_on))} · ${b.age_days} days</small></span>
          <span class="qty">${qty(b.est_qty_left)} left</span></li>`).join('')}</ul>` : ''}`;
  }

  // The item card's actions hand the item to the task that does it.
  function wireItemActions(out, it) {
    const code = it.batch ? it.batch.batch_no : it.code;
    out.querySelectorAll('.item-actions [data-act]').forEach(b => b.addEventListener('click', () => {
      state.lastItem = it;
      location.hash = `#/${b.dataset.act}?code=${encodeURIComponent(code)}`;
    }));
  }

  function batchHTML(b) {
    const where = [b.grn ? `GRN ${b.grn}` : '', b.vendor].filter(Boolean).join(' · ');
    return `<div class="batch-box"><strong>Batch ${esc(b.batch_no)}</strong><br>
      Received ${esc(fmtDate(b.received_on))} - ${b.age_days} days ago (${esc(b.age_band)} days)${where ? `<br>${esc(where)}` : ''}</div>`;
  }

  function stockHTML(it) {
    const rows = it.stock || [];
    if (!rows.length) return '<p class="empty">No stock at the stores you can see.</p>';
    const list = rows.map(s => `
      <li class="${s.working ? 'here' : ''}">
        <span>${esc(s.name)}${s.working ? '<small>This store</small>' : ''}</span>
        <span class="qty${s.available > 0 ? '' : ' zero'}">${qty(s.available)} available${s.on_hand !== s.available ? `<small>${qty(s.on_hand)} on hand</small>` : ''}</span>
      </li>`).join('');
    let note = '';
    if (it.stock_more) note = `<p class="hint">…and ${it.stock_more} more stores with stock.</p>`;
    else if (rows.length === 1 && rows[0].working) note = '<p class="hint">No other store you can see has it.</p>';
    return `<ul class="rows">${list}</ul>${note}`;
  }

  // ---------- camera scanning (102.1.2) ----------
  let cam = null;

  async function openCamera(onCode) {
    let formats = ['code_128', 'ean_13', 'ean_8', 'upc_a', 'upc_e', 'code_39', 'itf', 'qr_code', 'data_matrix'];
    try {
      const supported = await window.BarcodeDetector.getSupportedFormats();
      formats = formats.filter(f => supported.includes(f));
    } catch (_) { /* keep the list */ }
    if (!formats.length) {
      toast('This browser cannot read barcodes with the camera. Type the code instead.', true);
      return;
    }
    const detector = new window.BarcodeDetector({ formats });
    let stream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false });
    } catch (e) {
      toast(e && e.name === 'NotAllowedError'
        ? 'The camera is not allowed. Allow it for this site in the browser settings, or type the code.'
        : 'Could not start the camera. Type the code instead.', true);
      return;
    }
    const overlay = document.createElement('div');
    overlay.className = 'camera';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-label', 'Scan with camera');
    overlay.innerHTML = `<video playsinline muted autoplay></video><div class="camera-frame"></div>
      <div class="camera-bar"><span>Point the camera at the barcode</span><button type="button" class="btn">Close</button></div>`;
    document.body.appendChild(overlay);
    const video = $('video', overlay);
    video.srcObject = stream;
    cam = { stream, overlay, timer: 0 };
    $('button', overlay).addEventListener('click', closeCamera);
    try { await video.play(); } catch (_) { /* autoplay attribute covers it */ }
    const session = cam;
    const tick = async () => {
      if (cam !== session) return;
      try {
        if (video.readyState >= 2) {
          const hit = (await detector.detect(video)).find(c => c.rawValue);
          if (hit && cam === session) {
            closeCamera();
            if (navigator.vibrate) navigator.vibrate(60);
            onCode(hit.rawValue);
            return;
          }
        }
      } catch (_) { /* a frame that could not be read */ }
      if (cam === session) session.timer = setTimeout(tick, 180);
    };
    tick();
  }

  function closeCamera() {
    if (!cam) return;
    clearTimeout(cam.timer);
    cam.stream.getTracks().forEach(t => t.stop());
    cam.overlay.remove();
    cam = null;
  }
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeCamera(); });

  // ---------- push alerts (102.1.3) ----------
  const pushSupported = () => 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;

  function keyBytes(b64) {
    const pad = '='.repeat((4 - (b64.length % 4)) % 4);
    const raw = atob((b64 + pad).replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(raw, c => c.charCodeAt(0));
  }

  async function currentPush() {
    if (!pushSupported()) return null;
    const reg = await navigator.serviceWorker.ready;
    return reg.pushManager.getSubscription();
  }

  async function pushOn(prefs) {
    if (Notification.permission === 'denied') throw new AppError('Alerts are blocked for this site. Allow notifications in the browser settings, then try again.');
    if (Notification.permission !== 'granted' && (await Notification.requestPermission()) !== 'granted') {
      throw new AppError('Alerts need permission to show notifications.');
    }
    const reg = await navigator.serviceWorker.ready;
    let sub = await reg.pushManager.getSubscription();
    if (!sub) {
      const { public_key: key } = await api('/api/v1/store-app/push/key');
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
    }
    return api('/api/v1/store-app/push', { method: 'POST', body: { ...sub.toJSON(), ...prefs } });
  }

  async function pushOff() {
    const sub = await currentPush();
    if (!sub) return;
    try { await api('/api/v1/store-app/push/off', { method: 'POST', body: { endpoint: sub.endpoint } }); } finally { await sub.unsubscribe(); }
  }

  // After a store switch, alerts follow the new working store.
  async function refreshPushStore() {
    try {
      const sub = await currentPush();
      if (sub) await api('/api/v1/store-app/push', { method: 'POST', body: sub.toJSON() });
    } catch (_) { /* alerts stay on the old store until Settings is opened */ }
  }

  // ---------- settings ----------
  async function renderSettings() {
    setBar('Settings', true);
    main.innerHTML = `
      <section class="card" id="set-alerts"><h2>Alerts on this phone</h2><p class="sub">Loading…</p></section>
      <section class="card" id="set-pin"><h2>Your PIN</h2><p class="sub">Loading…</p></section>
      <section class="card" id="set-phone"><h2>This phone</h2></section>
      ${isManager() && !pinSession() ? '<section class="card" id="set-phones"><h2>Store phones</h2><p class="sub">Loading…</p></section>' : ''}`;
    settingsAlerts($('#set-alerts'));
    settingsPIN($('#set-pin'));
    settingsPhone($('#set-phone'));
    if ($('#set-phones')) settingsPhones($('#set-phones'));
  }

  async function settingsAlerts(box) {
    if (!pushSupported()) {
      box.innerHTML = `<h2>Alerts on this phone</h2><p class="sub">This browser cannot show alerts from the app. On an iPhone, add the app to the Home Screen first (Share > Add to Home Screen), then open it from there.</p>`;
      return;
    }
    let status = { subscribed: false, approvals: true, low_stock: true };
    try {
      const sub = await currentPush();
      if (sub) status = await api(`/api/v1/store-app/push?endpoint=${encodeURIComponent(sub.endpoint)}`);
    } catch (e) {
      if (e.quiet) return;
    }
    box.innerHTML = `
      <h2>Alerts on this phone</h2>
      <p class="sub">${status.subscribed ? `On for ${esc(storeName(status.store || workingStore()))}.` : 'Off.'} An alert comes when something new needs you, even when the app is closed.</p>
      <label class="check"><input type="checkbox" id="al-appr" ${status.approvals !== false ? 'checked' : ''}> Approvals waiting for me</label>
      <label class="check"><input type="checkbox" id="al-low" ${status.low_stock !== false ? 'checked' : ''}> Items running low at my store</label>
      ${status.last_error ? `<p class="hint">Last alert: ${esc(status.last_error)}</p>` : ''}
      <div class="actions">
        ${status.subscribed ? '<button type="button" class="btn" id="al-off">Turn off</button>' : ''}
        <button type="button" class="btn btn-primary" id="al-on">${status.subscribed ? 'Save' : 'Turn on alerts'}</button>
      </div>`;
    $('#al-on', box).addEventListener('click', async (e) => {
      e.currentTarget.disabled = true;
      try {
        await pushOn({ approvals: $('#al-appr', box).checked, low_stock: $('#al-low', box).checked });
        toast('Alerts are on for this phone.');
        settingsAlerts(box);
      } catch (ex) {
        if (!ex.quiet) toast(ex.message || 'Alerts could not be turned on.', true);
        e.currentTarget.disabled = false;
      }
    });
    const off = $('#al-off', box);
    if (off) off.addEventListener('click', async () => {
      off.disabled = true;
      try { await pushOff(); toast('Alerts are off for this phone.'); } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
      settingsAlerts(box);
    });
  }

  async function settingsPIN(box) {
    let st;
    try { st = await api('/api/v1/store-app/pin'); } catch (e) {
      box.innerHTML = e.quiet ? '' : `<h2>Your PIN</h2><p class="sub">${esc(e.message)}</p>`;
      return;
    }
    if (!st.allowed) {
      box.innerHTML = '<h2>Your PIN</h2><p class="sub">PIN sign-in is for store roles. An administrator signs in with the password and the two-factor code.</p>';
      return;
    }
    box.innerHTML = `
      <h2>Your PIN</h2>
      <p class="sub">${st.set ? 'You have a PIN.' : 'No PIN yet.'} On a phone registered to your store, you sign in with your username and PIN. After 5 wrong PINs, PIN sign-in stops for 15 minutes.</p>
      <form id="pin-set" autocomplete="off">
        <label class="field"><span>Your password (to confirm it is you)</span><input class="input" id="ps-pass" type="password" autocomplete="current-password" required></label>
        <label class="field"><span>New PIN (4 to 8 digits)</span><input class="input pin-input" id="ps-pin" type="password" inputmode="numeric" pattern="[0-9]{4,8}" maxlength="8" autocomplete="off" required></label>
        <div class="actions">
          ${st.set ? '<button type="button" class="btn" id="ps-remove">Remove PIN</button>' : ''}
          <button type="submit" class="btn btn-primary">${st.set ? 'Change PIN' : 'Set PIN'}</button>
        </div>
      </form>`;
    $('#pin-set', box).addEventListener('submit', async (e) => {
      e.preventDefault();
      try {
        await api('/api/v1/store-app/pin', { method: 'POST', body: { password: $('#ps-pass', box).value, pin: $('#ps-pin', box).value.trim() } });
        toast('Your PIN is set.');
        settingsPIN(box);
      } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
    });
    const rm = $('#ps-remove', box);
    if (rm) rm.addEventListener('click', async () => {
      try { await api('/api/v1/store-app/pin/remove', { method: 'POST', body: {} }); toast('Your PIN is removed.'); settingsPIN(box); } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
    });
  }

  function settingsPhone(box) {
    const dev = device();
    if (dev) {
      box.innerHTML = `<h2>This phone</h2><p class="sub">Registered to ${esc(dev.store_name || dev.store)} as "${esc(dev.label)}". Everyone signs in on it with their own PIN.</p>
        ${pinSession() ? '' : '<div class="actions"><button type="button" class="btn" id="ph-forget">Forget the registration on this phone</button></div>'}`;
      const forget = $('#ph-forget', box);
      if (forget) forget.addEventListener('click', () => {
        ls.del(DEVICE_KEY);
        toast('This phone no longer offers PIN sign-in. A manager can remove it from Store phones too.');
        settingsPhone(box);
      });
      return;
    }
    if (!isManager() || pinSession()) {
      box.innerHTML = '<h2>This phone</h2><p class="sub">This is a personal phone: sign in with your password. A store manager can register a shared store phone so staff sign in with a PIN.</p>';
      return;
    }
    box.innerHTML = `<h2>This phone</h2>
      <p class="sub">Register it as a shared phone for ${esc(storeName(workingStore()))}. After that, staff sign in on it with their own PIN, it locks after a few minutes without use, and it works at this store only.</p>
      <form id="ph-reg"><label class="field"><span>Name for this phone</span><input class="input" id="ph-label" maxlength="160" placeholder="e.g. Counter phone 1" required></label>
      <div class="actions"><button type="submit" class="btn btn-primary">Register to ${esc(storeName(workingStore()))}</button></div></form>`;
    $('#ph-reg', box).addEventListener('submit', async (e) => {
      e.preventDefault();
      try {
        const res = await api('/api/v1/store-app/devices', { method: 'POST', body: { label: $('#ph-label', box).value.trim() } });
        ls.setJSON(DEVICE_KEY, { id: res.device.id, token: res.device_token, store: res.device.store, store_name: res.device.store_name, label: res.device.label });
        toast('This phone is registered. Staff now sign in on it with their PIN.');
        settingsPhone(box);
        const list = $('#set-phones');
        if (list) settingsPhones(list);
      } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
    });
  }

  async function settingsPhones(box) {
    let res;
    try { res = await api('/api/v1/store-app/devices'); } catch (e) {
      box.innerHTML = e.quiet ? '' : `<h2>Store phones</h2><p class="sub">${esc(e.message)}</p>`;
      return;
    }
    const devices = res.devices || [];
    box.innerHTML = `<h2>Store phones</h2>
      ${devices.length ? `<ul class="rows">${devices.map(d => `<li><span>${esc(d.label)}<small>${esc(d.store_name || d.store)} · registered by ${esc(d.registered_by)}${d.last_user ? ` · last used by ${esc(d.last_user)}` : ''}</small></span>
        <button type="button" class="btn btn-danger btn-small" data-remove="${esc(d.id)}">Remove</button></li>`).join('')}</ul>
        <p class="hint">Remove a phone that is lost, stolen or no longer used: everyone signed in on it is signed out at once.</p>` : '<p class="sub">No shared phones are registered at your stores.</p>'}`;
    box.querySelectorAll('[data-remove]').forEach(b => b.addEventListener('click', async () => {
      if (!b.dataset.armed) { b.dataset.armed = '1'; b.textContent = 'Tap again to remove'; return; }
      try {
        await api(`/api/v1/store-app/devices/${encodeURIComponent(b.dataset.remove)}/remove`, { method: 'POST', body: {} });
        const dev = device();
        if (dev && dev.id === b.dataset.remove) ls.del(DEVICE_KEY);
        toast('Phone removed.');
        settingsPhones(box);
      } catch (ex) { if (!ex.quiet) toast(ex.message, true); }
    }));
  }

  // ---------- feature modules ----------
  const MODULES = {
    today: 'store-today.js', 'low-stock': 'store-today.js',
    receive: 'store-stock.js', count: 'store-stock.js', labels: 'store-stock.js', ask: 'store-stock.js',
    basket: 'store-floor.js', order: 'store-floor.js',
    customer: 'store-crm.js', tasks: 'store-crm.js',
  };
  const loadedModules = {};
  const loadModule = (file) => loadedModules[file] || (loadedModules[file] = import(`./${file}?v=2`).catch((e) => { delete loadedModules[file]; throw e; }));

  // ctx is what a module may use of the shell.
  const ctx = {
    api, esc, words, money, qty, fmtDate, ago, toast, setBar, panelError, main, ls, newKey, ICON,
    scanBox, openCamera, closeCamera, AppError, routeName,
    state, workingStore, storeName, isManager, pinSession,
    lookupItem: (code) => api(`/api/v1/store-app/item?code=${encodeURIComponent(code)}`),
  };

  async function renderModule(name, params) {
    let mod;
    try {
      mod = await loadModule(MODULES[name]);
    } catch (_) {
      setBar(words(name), true);
      panelError(navigator.onLine ? 'This part of the app could not be loaded.' : 'This part of the app has not been opened on this phone yet, so it needs a connection the first time.', () => route());
      return;
    }
    const r = mod.routes && mod.routes[name];
    if (!r) { renderHome(); return; }
    setBar(r.title, true);
    await r.render(ctx, params);
  }

  // Counts made offline are sent when the connection is back (102.3.3).
  async function syncQueuedCounts() {
    if (!navigator.onLine || !ls.get('erp_token') || !(ls.json('store_app_count_queue', []) || []).length) return;
    try {
      const mod = await loadModule('store-stock.js');
      if (mod.syncQueuedCounts) await mod.syncQueuedCounts(ctx);
    } catch (_) { /* tried again on the next open */ }
  }

  // ---------- routing and start ----------
  const routes = { home: renderHome, scan: renderScan, approvals: renderApprovals, stores: renderStores, settings: renderSettings };

  function routeName() { return (location.hash.replace(/^#\/?/, '').split('?')[0]) || 'home'; }

  function route() {
    if (!ls.get('erp_token')) { showSignIn(''); return; }
    if (!state.profile) { boot(); return; }
    closeCamera();
    const name = routeName();
    const params = new URLSearchParams(location.hash.split('?')[1] || '');
    if (routes[name]) { routes[name](params); return; }
    if (MODULES[name]) { renderModule(name, params); return; }
    renderHome();
  }
  window.addEventListener('hashchange', route);

  function updateOnline() { offlineEl.hidden = navigator.onLine; }
  window.addEventListener('online', () => {
    updateOnline();
    // Opened offline: carry on by itself once the connection is back.
    if (!state.profile && ls.get('erp_token')) boot();
    else syncQueuedCounts();
  });
  window.addEventListener('offline', updateOnline);

  async function boot() {
    updateOnline();
    if (!ls.get('erp_token')) { showSignIn(''); return; }
    try {
      const [profile, stores] = await Promise.all([api('/api/v1/me'), api('/api/v1/me/stores')]);
      state.profile = profile;
      state.stores = stores;
    } catch (e) {
      if (!e.quiet) {
        bar.hidden = true;
        // Offline with work saved on the phone: the count can still be done.
        if (!navigator.onLine && /^count/.test(routeName())) {
          state.profile = { role: ls.get('erp_role'), offline: true };
          route();
          return;
        }
        panelError(e.message, boot);
      }
      return;
    }
    // A PIN session locks after the client's few minutes, or the person's own
    // shorter Auto Logout (102.1.4).
    const own = Number(state.profile.idle_timeout_minutes) || 0;
    const pin = Number(ls.get(PIN_IDLE_KEY)) || 0;
    state.idleMinutes = pin ? (own ? Math.min(own, pin) : pin) : own;
    if (idleExpired()) { idleSignOut(); return; }
    armIdle(state.idleMinutes);
    route();
  }

  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => { navigator.serviceWorker.register('sw.js').catch(() => { /* the app still works online */ }); });
  }
  boot();
})();
