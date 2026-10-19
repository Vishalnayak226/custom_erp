// Stage 102: the store app - an installable web app (PWA) for store staff.
// Plain JavaScript with no build step, like the rest of public/. It signs in,
// switches the working store and decides approvals through the ERP's own
// endpoints, and adds one read-only call, GET /api/v1/store-app/item (scan an
// item). The session is the ERP's own: same origin, same localStorage keys, so
// signing in here signs in the ERP in this browser too, and the reverse.
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

  const state = { profile: null, stores: null, mfaToken: '', idleMinutes: 0, idleTimer: 0 };

  const ICON = {
    scan: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3M7 9v6M10 9v6M13 9v6M17 9v6"/></svg>',
    check: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 11l3 3 8-8"/><path d="M20 12v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h9"/></svg>',
    camera: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h3l2-3h6l2 3h3a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z"/><circle cx="12" cy="13" r="3.5"/></svg>',
  };

  // ---------- storage (shared with the ERP) ----------
  const ls = {
    get(k) { try { return localStorage.getItem(k) || ''; } catch (_) { return ''; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (_) { /* private mode */ } },
    del(k) { try { localStorage.removeItem(k); } catch (_) { /* private mode */ } },
  };
  const tenantID = () => ls.get('erp_tenant_id') || 'default';
  const LAST_ACTIVE = 'store_app_last_active';

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

  let toastTimer = 0;
  function toast(text, isError) {
    toastEl.textContent = text;
    toastEl.className = 'toast' + (isError ? ' error' : '');
    toastEl.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toastEl.hidden = true; }, isError ? 6000 : 3500);
  }

  class AppError extends Error {
    constructor(message, quiet) { super(message); this.quiet = !!quiet; }
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
      throw new AppError('', true);
    }
    if (res.status === 403 && res.headers.get('X-Password-Change-Required') === '1') {
      signOut(PASSWORD_MSG);
      throw new AppError('', true);
    }
    let data = null;
    try { data = await res.json(); } catch (_) { /* no body */ }
    if (res.status === 429) throw new AppError('Too many requests just now. Wait a few seconds and try again.');
    if (!res.ok) {
      // A 4xx message is already specific; the catalog's generic next step
      // is added only when the server could not say what went wrong.
      const parts = [data && data.error, res.status >= 500 && data && data.user_action].filter(Boolean);
      throw new AppError(parts.join(' ') || `Something went wrong (${res.status}).`);
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
    ['erp_token', 'erp_username', 'erp_role', LAST_ACTIVE].forEach(k => ls.del(k));
    state.profile = null;
    state.stores = null;
    stopIdle();
    showSignIn(message || '');
  }

  // ---------- top bar ----------
  function setBar(title, back) {
    bar.hidden = false;
    barTitle.textContent = title;
    barBack.hidden = !back;
    renderStoreChip();
  }
  function storeName(code) {
    const found = ((state.stores && state.stores.stores) || []).find(s => s.code === code);
    return found ? found.name : code;
  }
  function renderStoreChip() {
    const s = state.stores;
    if (!s) { storeChip.hidden = true; return; }
    const working = s.working || (state.profile && state.profile.location_code) || '';
    const canSwitch = !!s.all || (s.stores || []).length > 1;
    storeChip.textContent = working ? storeName(working) : 'Choose store';
    storeChip.disabled = !canSwitch;
    storeChip.setAttribute('aria-label', `Working store: ${storeChip.textContent}${canSwitch ? '. Change store' : ''}`);
    storeChip.hidden = false;
  }
  barBack.addEventListener('click', () => { location.hash = '#/home'; });
  storeChip.addEventListener('click', () => { if (!storeChip.disabled) location.hash = '#/stores'; });

  function panelError(message, retry) {
    // Offline, the banner above already says so; just offer the retry.
    const msg = navigator.onLine ? `<div class="msg msg-error" role="alert">${esc(message)}</div>` : '';
    main.innerHTML = `${msg}<button type="button" class="btn btn-block" id="retry">Try again</button>`;
    $('#retry').addEventListener('click', retry);
  }

  // ---------- sign in ----------
  function showSignIn(message) {
    closeCamera();
    bar.hidden = true;
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
        <div class="links"><a class="link" href="/">Open the full ERP</a></div>
      </form>`;
    $('#signin-form').addEventListener('submit', onSignIn);
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
    $('#mfa-back').addEventListener('click', () => { state.mfaToken = ''; showSignIn(''); });
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
      showSignIn(PASSWORD_MSG);
      return;
    }
    ls.set('erp_token', data.token);
    ls.set('erp_username', data.user || '');
    ls.set('erp_role', data.role || '');
    ls.set(LAST_ACTIVE, String(Date.now()));
    if (data.used_recovery_code) {
      toast(`Signed in with a recovery code - ${Number(data.recovery_codes_remaining || 0)} left. Set up a new authenticator in the full ERP (Profile).`);
    }
    if (!/^#\/(scan|approvals|stores)/.test(location.hash)) history.replaceState(null, '', '#/home');
    await boot();
  }

  // ---------- home ----------
  async function renderHome() {
    setBar('Store', false);
    main.innerHTML = `
      <div class="tiles">
        <a class="tile" href="#/scan">${ICON.scan}<span>Scan an item</span></a>
        <a class="tile" href="#/approvals">${ICON.check}<span>Approvals</span><span class="badge" id="appr-badge" hidden></span></a>
      </div>
      <div class="links">
        <a class="link" href="/">Open the full ERP</a>
        <button type="button" class="link" id="signout">Sign out</button>
      </div>`;
    $('#signout').addEventListener('click', () => signOut('You are signed out.'));
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
    if (location.hash !== '#/approvals') return;
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
    main.innerHTML = '<p class="loading">Loading…</p>';
    let s;
    try {
      s = await api('/api/v1/me/stores');
    } catch (e) {
      if (!e.quiet) panelError(e.message, renderStores);
      return;
    }
    if (location.hash !== '#/stores') return;
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
      location.hash = '#/home';
    } catch (e) {
      button.disabled = false;
      if (!e.quiet) toast(e.message, true);
    }
  }

  // ---------- scan an item ----------
  const cameraSupported = () => 'BarcodeDetector' in window && !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia);
  let lookingUp = '';

  function renderScan() {
    setBar('Scan an item', true);
    main.innerHTML = `
      <form class="scan-form" id="scan-form" autocomplete="off">
        <input class="input" id="scan-code" aria-label="Barcode, item code or batch number" placeholder="Scan or type a code"
          autocapitalize="characters" autocorrect="off" spellcheck="false" enterkeyhint="search">
        <button class="btn btn-primary" type="submit">Look up</button>
      </form>
      ${cameraSupported()
        ? `<button type="button" class="btn btn-block scan-cam" id="scan-camera">${ICON.camera}Scan with camera</button>`
        : '<p class="hint">This browser cannot read barcodes with the camera. Use a Bluetooth scanner, or type the code.</p>'}
      <div id="scan-result"></div>`;
    const input = $('#scan-code');
    $('#scan-form').addEventListener('submit', (e) => { e.preventDefault(); lookup(input.value); });
    const cam = $('#scan-camera');
    if (cam) cam.addEventListener('click', () => openCamera(code => { input.value = code; lookup(code); }));
    // A Bluetooth scanner is a keyboard; some send no Enter. A burst of fast
    // keystrokes is a scan, so look it up once the burst stops (Stage 77).
    let lastKey = 0;
    let burst = 0;
    let burstTimer = 0;
    input.addEventListener('input', () => {
      const now = performance.now();
      burst = now - lastKey < 35 ? burst + 1 : 1;
      lastKey = now;
      clearTimeout(burstTimer);
      if (burst >= 6) burstTimer = setTimeout(() => lookup(input.value), 150);
    });
    if (!matchMedia('(pointer: coarse)').matches) input.focus();
  }

  async function lookup(raw) {
    const code = String(raw || '').trim();
    const out = $('#scan-result');
    if (!code || !out || lookingUp === code) return;
    lookingUp = code;
    out.innerHTML = '<p class="loading">Looking up…</p>';
    try {
      const item = await api(`/api/v1/store-app/item?code=${encodeURIComponent(code)}`);
      if ($('#scan-result') === out) out.innerHTML = itemHTML(item);
    } catch (e) {
      if (!e.quiet && $('#scan-result') === out) out.innerHTML = `<div class="msg msg-error" role="alert">${esc(e.message)}</div>`;
    } finally {
      lookingUp = '';
      const input = $('#scan-code');
      if (input) { input.value = code; input.select(); }
    }
  }

  function itemHTML(it) {
    const cur = it.currency;
    const tags = [it.brand, it.category, it.status && it.status !== 'Active' ? it.status : '']
      .filter(Boolean).map(t => `<span class="tag">${esc(t)}</span>`).join(' ');
    const showMRP = it.mrp > 0 && Math.abs(it.mrp - it.price) > 0.004;
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
      </article>
      <h3 class="section-title">Stock</h3>
      ${stockHTML(it)}
      ${(it.batches || []).length ? `
        <h3 class="section-title">Oldest batches with stock left (all stores, estimated)</h3>
        <ul class="rows">${it.batches.map(b => `
          <li><span>${esc(b.batch_no)}<small>Received ${esc(fmtDate(b.received_on))} · ${b.age_days} days</small></span>
          <span class="qty">${qty(b.est_qty_left)} left</span></li>`).join('')}</ul>` : ''}`;
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

  // ---------- routing and start ----------
  const routes = { home: renderHome, scan: renderScan, approvals: renderApprovals, stores: renderStores };

  function route() {
    if (!ls.get('erp_token')) { showSignIn(''); return; }
    if (!state.profile) { boot(); return; }
    closeCamera();
    const name = location.hash.replace(/^#\/?/, '') || 'home';
    (routes[name] || renderHome)();
  }
  window.addEventListener('hashchange', route);

  function updateOnline() { offlineEl.hidden = navigator.onLine; }
  window.addEventListener('online', () => {
    updateOnline();
    // Opened offline: carry on by itself once the connection is back.
    if (!state.profile && ls.get('erp_token')) boot();
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
        panelError(e.message, boot);
      }
      return;
    }
    state.idleMinutes = Number(state.profile.idle_timeout_minutes) || 0;
    if (idleExpired()) { idleSignOut(); return; }
    armIdle(state.idleMinutes);
    route();
  }

  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => { navigator.serviceWorker.register('sw.js').catch(() => { /* the app still works online */ }); });
  }
  boot();
})();
