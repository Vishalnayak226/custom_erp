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
    // Stage 30.2.4: accepts a DOM node as well as a string, so an error can be
    // shown as headline + specific reason + what-to-do-next on separate lines
    // (see composeErrorLines). Deliberately appendChild and textContent, never
    // innerHTML - nothing here is ever parsed as markup.
    msgEl.textContent = '';
    if (message instanceof Node) {
      msgEl.appendChild(message);
    } else {
      msgEl.textContent = message;
    }

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

// inputType (32.5) lets a caller ask for a password without a second dialog
// system - the MFA recovery screens need to re-authenticate, and typing a
// password into a plain text input in front of a colleague is not acceptable.
// Defaults to 'text', so every existing caller is unaffected.
function showCustomPrompt(message, defaultValue = '', title = 'Input Required', inputType = 'text') {
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
    extraEl.innerHTML = `<input type="${inputType}" id="custom-dialog-prompt-input" class="form-input" style="width: 100%; margin-top: 12px;" value="${defaultValue}">`;
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
// Stage 30.2.4 adds `detail` (the engine's own specific reason - which item,
// which field) and `user_action` (the catalog's "what do I do now", populated
// on all 302 rows and never previously sent). Both are optional; a response
// without them behaves exactly as before.
async function getErrorDetails(res, fallback) {
  try {
    const data = await res.clone().json();
    if (data && data.error) {
      return {
        message: data.error,
        detail: data.detail || '',
        userAction: data.user_action || '',
        code: data.code || '',
        displayStyle: data.display_style || '',
      };
    }
  } catch (e) {
    // Body wasn't JSON (a call site not yet migrated to the standardized
    // envelope) - fall through to the fallback message.
  }
  return { message: fallback, detail: '', userAction: '', code: '', displayStyle: '' };
}

// Joins the headline with whichever of detail/user_action came back, for the
// inline error strips and single-line toasts that can only show one string.
// The full three-part layout is showApiError's modal.
function composeErrorText({ message, detail, userAction }) {
  return [message, detail, userAction].filter(Boolean).join(' ');
}

async function getErrorMessage(res, fallback) {
  return composeErrorText(await getErrorDetails(res, fallback));
}

// Stage 23.8: dispatches by the catalog's own display_style instead of
// always showing the blocking modal. Only "Toast" and "Page banner" are
// generic enough to render without knowing which field/form the error
// belongs to (Inline field message, Modal popup, etc. all keep the modal
// fallback here - see apierror.go's apiErrorBody comment). title is only
// used by the modal fallback, so existing callers passing just (res,
// fallback) are unaffected.
async function showApiError(res, fallback, title = 'Error') {
  const details = await getErrorDetails(res, fallback);
  const { message, detail, userAction, code, displayStyle } = details;
  if (code) console.debug(`[API error] ${code}`);
  if (displayStyle === 'Toast') {
    showToast(composeErrorText(details), { variant: 'warning' });
    return;
  }
  if (displayStyle === 'Page banner') {
    const container = document.getElementById('view-root');
    if (container) {
      renderPageBanner(container, composeErrorText(details));
      return;
    }
  }
  // Stage 30.2.4: the modal shows all three parts as their own lines - the
  // catalog headline, then the specific reason, then what to do about it.
  // Before this, a missing HSN code read only "Tax configuration is missing
  // for this transaction. Please contact administrator." with no indication
  // of which item or which field, to an administrator.
  await showCustomAlert(composeErrorLines(message, detail, userAction), title);
}

// Builds the modal body for showApiError. Returns a DOM node when there is
// more than the headline to show, and a plain string otherwise, so the
// single-line case renders byte-for-byte as it always has.
function composeErrorLines(message, detail, userAction) {
  if (!detail && !userAction) return message;
  const wrap = document.createElement('div');
  wrap.style.display = 'flex';
  wrap.style.flexDirection = 'column';
  wrap.style.gap = '10px';
  wrap.style.textAlign = 'left';

  const head = document.createElement('div');
  head.textContent = message;
  wrap.appendChild(head);

  if (detail) {
    const d = document.createElement('div');
    d.textContent = detail;
    d.style.fontSize = '13px';
    d.style.color = 'var(--text-muted)';
    wrap.appendChild(d);
  }
  if (userAction) {
    const a = document.createElement('div');
    a.textContent = userAction;
    a.style.fontSize = '13px';
    a.style.fontWeight = '600';
    wrap.appendChild(a);
  }
  return wrap;
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

// Copy-to-clipboard affordance (Stage 26 P2 UI pass, 2026-07-26) - wraps a
// rendered cell value with a small icon button so a user can copy an
// identifier (SKU, PO number, etc.) here and paste it into a search box
// elsewhere, without adding a new UI framework/library. Kept as one shared
// helper so every table that renders through it behaves identically.
const COPY_ICON_SVG = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
const COPY_ICON_DONE_SVG = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"></polyline></svg>';

function copyableCell(displayValue, rawValue) {
  const raw = rawValue === undefined || rawValue === null ? '' : String(rawValue);
  if (raw === '') return displayValue === undefined || displayValue === null ? '' : String(displayValue);
  // 30.5.8: this one helper renders every copy chip in the app - roughly 30
  // per record list - so the accessible name is attached here rather than at
  // any call site. It names the value, not the action: a screen reader
  // announcing "Copy, button" thirty times down a column says nothing about
  // which row it is on. `title` alone is only a last-resort accessible name
  // and several screen readers skip it, so aria-label is what makes these
  // reachable rather than merely hoverable.
  return `<span class="copyable-cell"><span>${displayValue}</span><button type="button" class="copy-chip" title="Copy" aria-label="Copy ${escapeHTMLText(raw)}" data-copy-value="${encodeURIComponent(raw)}" onclick="event.stopPropagation(); copyValueToClipboard(this)">${COPY_ICON_SVG}</button></span>`;
}

window.copyValueToClipboard = async function(btn) {
  const value = decodeURIComponent(btn.dataset.copyValue || '');
  if (!value) return;
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value);
    } else {
      throw new Error('clipboard API unavailable');
    }
  } catch (e) {
    const ta = document.createElement('textarea');
    ta.value = value;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e2) { /* best-effort fallback only */ }
    document.body.removeChild(ta);
  }
  const original = btn.innerHTML;
  btn.classList.add('copy-chip-done');
  btn.innerHTML = COPY_ICON_DONE_SVG;
  clearTimeout(btn._copyResetTimer);
  btn._copyResetTimer = setTimeout(() => {
    btn.innerHTML = original;
    btn.classList.remove('copy-chip-done');
  }, 900);
};

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
  approvalRules: [],
  labels: {},
  auditLogs: [],
  systemLogs: [],
  profile: null,
  // 22.6: defaults to "show everything" until the real grant set loads
  // (fetchAndApplyPermissions) - a brief full-menu flash is a better
  // failure mode than a brief empty-sidebar flash, and the server's own
  // checkPermission() is the actual enforcement point regardless of what
  // the sidebar shows.
  // create/update/delete (30.5.7) mirror `doctypes`, which holds read grants.
  // Same "show everything until loaded" default as the rest of this block.
  permissions: { isAdmin: true, doctypes: new Set(), create: new Set(), update: new Set(), delete: new Set(), loaded: false },
  // Stage 27: same "show everything until loaded" default as permissions
  // above - enabled: null means "unknown yet," which isMenuModuleVisible
  // treats as visible; moduleGate on the server is the real enforcement
  // point regardless of what the sidebar shows.
  modules: { enabled: null, solePackage: null, ownedPackages: [], loaded: false },
  // Stage 41. setupStatus is "how many records exist per Master record
  // type", loaded once per session from GET /api/v1/setup/status and refreshed
  // whenever a master is created, so any screen can answer "is X set up?"
  // without its own query. byDoctype is empty until loaded; every reader
  // treats "unknown" as "assume it is set up", so a failed/slow load produces
  // no hint rather than a wrong one telling the user to go create records
  // that already exist.
  setupStatus: { byDoctype: {}, loaded: false },
  // The tenant's home country and its phone rule (GET /api/v1/localization).
  // Null until loaded - phone inputs simply stay unrestricted until it lands,
  // which is the safe direction since the server validates regardless.
  localization: null
};

// The screen the app opens on when there is nothing saved to restore. It was
// the Dashboard until the user retired that screen (2026-08-01) - everything
// it showed was derived counts and shortcut tiles into Settings screens.
// Reports is the replacement because it is the one destination every role and
// every tenant can reach (MENU_PERMISSION_MAP marks it `open`, and it carries
// no MENU_MODULE_MAP entitlement gate), and its first tab is the executive
// dashboard, which shows real figures rather than configuration links.
const DEFAULT_VIEW = 'reports';

let currentView = DEFAULT_VIEW;
let currentDoctype = '';
let posCart = []; // { sku, available, qty, salePrice, costPrice }
let posLocation = '';
let posOpenSessionId = ''; // Stage 20.7: '' means no open cashier session at posLocation
const OFFLINE_QUEUE_KEY = 'erp_pos_offline_queue'; // 20.13, see checkoutOnlineOrQueue below
let offlineSyncInFlight = false;

// 21.14: stale-while-revalidate cache, sessionStorage-backed (per-tab,
// cleared on tab close - deliberately not localStorage, which would leak a
// stale list across a login/tenant switch in the same browser profile).
// Read-only GET data only (a doctype's record list, its field metadata) -
// never used for anything that mutates, and every write path already goes
// through apiFetch untouched, so a stale cache is a display lag at worst,
// never a stale write.
const SWR_PREFIX = 'erp_swr_';

function swrCacheGet(key) {
  try {
    const raw = sessionStorage.getItem(SWR_PREFIX + key);
    return raw ? JSON.parse(raw) : null;
  } catch (e) {
    return null;
  }
}

function swrCacheSet(key, data) {
  try {
    sessionStorage.setItem(SWR_PREFIX + key, JSON.stringify(data));
  } catch (e) {
    // sessionStorage full/unavailable (private browsing, quota) - SWR just
    // degrades to "always fetch fresh," no functional loss.
  }
}

// swrFetch(key, fetchFn, onFresh): returns cached data synchronously (or
// null if none cached yet) so the caller can render *something* instantly
// with zero network wait - the "eliminate loading spinners" half of 21.14.
// Always fires fetchFn() in the background regardless of whether cache
// existed; when it resolves, onFresh(data) is called only if the result is
// new/different, so a caller doesn't re-render pointlessly when nothing
// changed. fetchFn returning undefined (a failed/non-ok response) is
// treated as "couldn't revalidate this time" - the stale cache is left in
// place rather than wiped, since showing slightly-stale data beats showing
// nothing on a transient network blip.
function swrFetch(key, fetchFn, onFresh) {
  const cached = swrCacheGet(key);
  (async () => {
    const fresh = await fetchFn();
    if (fresh === undefined) return;
    const freshStr = JSON.stringify(fresh);
    if (!cached || JSON.stringify(cached) !== freshStr) {
      swrCacheSet(key, fresh);
      onFresh(fresh);
    }
  })();
  return cached;
}
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
// the same view/doctype/search/page instead of always bouncing to DEFAULT_VIEW.
const NAV_STATE_KEY = 'erp_nav_state';

function saveNavState() {
  // Stage 41: mirror the current screen into the address bar, so the URL
  // always describes what is on it - copy it, duplicate the tab, or reload,
  // and you land back on the same screen. This is the other half of the deep
  // links the setup hints hand out: without it the hash would be left stale
  // pointing at whichever hinted screen was opened last.
  //
  // replaceState rather than assigning location.hash, for two reasons: it does
  // not fire the hashchange listener (which would bounce straight back into a
  // second render of the view being rendered right now), and it does not push
  // a history entry per navigation, which would make Back require as many
  // presses as the user made clicks.
  try {
    const target = currentView === 'doctype-table' && currentDoctype
      ? deepLinkForDoctype(currentDoctype)
      : deepLinkForView(currentView);
    if (window.location.hash !== target) history.replaceState(null, '', target);
  } catch (e) {
    // file:// or a sandboxed frame - navigation still works, it just isn't
    // addressable. Not worth failing the render over.
  }
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

// Server-issued document number placeholder (Stage 30.6). Every transaction
// create screen used to ask the maker to type the document number, which was
// then sent as the document id - so two makers picking the same number meant
// the second save silently overwrote the first, and nothing stopped a typo or
// an out-of-order number entering the books. The number now comes from the
// tenant's Prefix Configs series, under a row lock, on save.
//
// The field is kept (rather than removed) so the form still reads the same and
// the maker can see which series the number will come from before committing.
// It renders read-only with a placeholder, matching the convention the generic
// record form already used for PurchaseRequisition's code field.
function autoNumberField(label, seriesKey, width = '180px') {
  return `
    <div class="form-group" style="margin-bottom: 0;">
      <label class="form-label">${label}</label>
      <input type="text" class="form-input" style="width: ${width};" readonly tabindex="-1"
             placeholder="Auto (${seriesKey} series)"
             title="Issued automatically from the ${seriesKey} series when you save. Administrators set the format under Prefix Configurations.">
    </div>
  `;
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
  // Stage 30.5.8. Two opt-in behaviours, both added for the consistency
  // sweep and both deliberately off by default so the other 40-odd call
  // sites are untouched:
  //
  //   groupBy         - render the results under a heading per distinct
  //                     value of this field. Location's 103 records are a
  //                     mix of shops, warehouses and the head office, and a
  //                     flat list of eight codes gave no way to tell which
  //                     was which. Group order is first-appearance, not a
  //                     hardcoded list, so a tenant that adds a fourth
  //                     Location type gets it grouped without a code change.
  //   showAllOnFocus  - open the menu on focus with an empty query, so the
  //                     control can be browsed and not only searched. This
  //                     is what lets an employee <select> become a typeahead
  //                     without losing the "just show me the list" affordance
  //                     a dropdown had.
  const groupBy = opts.groupBy || null;
  const showAllOnFocus = !!opts.showAllOnFocus;
  const browseLimit = opts.browseLimit || 50;

  let menu = null;
  let items = [];
  let activeIndex = -1;
  let debounceTimer = null;
  let requestSeq = 0;
  let resultsCapped = false;

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
    resultsCapped = false;
  }

  function pick(doc) {
    let val = '';
    for (const f of valueFields) {
      if (doc[f] !== undefined && doc[f] !== null && doc[f] !== '') { val = doc[f]; break; }
    }
    // Stage 41: opts.onPick lets a caller take over what a selection means,
    // for the one case the valueFields list cannot express - a control whose
    // VISIBLE value and whose STORED value are different fields of the same
    // record (the POS location box shows "Bandra Flagship", the request sends
    // "BKC01"). Everything else is unaffected: with no onPick this behaves
    // exactly as before.
    if (typeof opts.onPick === 'function') {
      closeMenu();
      opts.onPick(doc, val);
      inputEl.focus();
      return;
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
    // Stage 30.5.1: zero matches used to close the menu silently, which is
    // indistinguishable from "the search hasn't run yet". Show a dead-end
    // row that names the record type and links to where one is created -
    // the same affordance an empty <select> now gets, for the other picker
    // control. Deliberately not selectable: it isn't a value.
    const isEmpty = items.length === 0;
    menu = document.createElement('div');
    menu.className = 'typeahead-menu';
    const rect = inputEl.getBoundingClientRect();
    menu.style.left = `${rect.left}px`;
    menu.style.top = `${rect.bottom + 4}px`;
    menu.style.width = `${Math.max(rect.width, 180)}px`;
    if (isEmpty) {
      const row = document.createElement('div');
      row.className = 'typeahead-item typeahead-item-empty';
      row.innerHTML = `No matching ${getTranslatedLabel(doctype)} &mdash; <a href="#" class="empty-state-link">create one</a>`;
      // The menu is a child of <body>, not of the input's container, so it
      // has to be torn down explicitly before navigating away or it is left
      // floating over the next screen.
      row.querySelector('a').addEventListener('mousedown', (e) => {
        e.preventDefault();
        closeMenu();
        openSetupDoctype(doctype);
      });
      menu.appendChild(row);
      document.body.appendChild(menu);
      document.addEventListener('mousedown', onDocMouseDown, true);
      return;
    }
    // Grouping reorders `items` itself rather than only reordering the DOM,
    // because highlight()/pick() address rows by index into `items` - the two
    // orders have to stay identical or the arrow keys select the wrong record.
    if (groupBy) items = groupItemsBy(items, groupBy);
    let lastGroup = null;
    items.forEach((doc) => {
      if (groupBy) {
        const g = groupValue(doc, groupBy);
        if (g !== lastGroup) {
          lastGroup = g;
          if (g) {
            const heading = document.createElement('div');
            heading.className = 'typeahead-group';
            heading.textContent = g;
            menu.appendChild(heading);
          }
        }
      }
      const row = document.createElement('div');
      row.className = 'typeahead-item';
      row.textContent = labelFn(doc);
      row.addEventListener('mousedown', (e) => { e.preventDefault(); pick(doc); });
      menu.appendChild(row);
    });
    // A browse that filled its page looks identical to a browse that returned
    // everything, so a user replacing a <select> with this could reasonably
    // conclude the tenant has 50 employees. Say so instead. Not a
    // .typeahead-item, so it stays outside the keyboard-navigable index.
    if (resultsCapped) {
      const note = document.createElement('div');
      note.className = 'typeahead-note';
      note.textContent = `Showing the first ${items.length} — type to search the rest.`;
      menu.appendChild(note);
    }
    document.body.appendChild(menu);
    document.addEventListener('mousedown', onDocMouseDown, true);
  }

  async function search(q, { browse = false } = {}) {
    const seq = ++requestSeq;
    if (!q && !browse) { closeMenu(); return; }
    const pageSize = browse ? browseLimit : limit;
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(q)}&limit=${pageSize}`);
    if (seq !== requestSeq) return; // a newer keystroke's request already superseded this one
    if (!res || !res.ok) { closeMenu(); return; }
    items = await res.json();
    if (seq !== requestSeq) return;
    resultsCapped = browse && items.length >= pageSize;
    openMenu();
  }

  inputEl.setAttribute('autocomplete', 'off');
  inputEl.addEventListener('input', () => {
    clearTimeout(debounceTimer);
    const q = inputEl.value.trim();
    debounceTimer = setTimeout(() => search(q), 250);
  });
  if (showAllOnFocus) {
    inputEl.addEventListener('focus', () => {
      // Only the empty case browses. Focusing a field that already holds a
      // value (tabbing back through a half-filled form, or the focus()
      // pick() itself performs) must not blow the picked value's menu open
      // again over the top of the next field.
      if (inputEl.value.trim()) return;
      clearTimeout(debounceTimer);
      search('', { browse: true });
    });
  }
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
        // was attached before that one, so attachLinkTypeahead() call sites
        // that share Enter with another handler must run first.
        e.stopImmediatePropagation();
        pick(items[activeIndex]);
      }
    }
    else if (e.key === 'Escape') { closeMenu(); }
  });
}

// Grouping support for attachTypeahead's `groupBy` (Stage 30.5.8).
// Kept out of the closure so both halves - the bucketing and the
// "has the heading changed?" test - read the field the same way; a doc
// whose group field is missing, null or "" is one ungrouped bucket that
// sorts last and renders with no heading, rather than a heading reading
// "undefined".
function groupValue(doc, field) {
  const v = doc ? doc[field] : undefined;
  return (v === undefined || v === null || v === '') ? '' : String(v);
}

function groupItemsBy(docs, field) {
  const order = [];
  const buckets = new Map();
  docs.forEach(doc => {
    const key = groupValue(doc, field);
    if (!buckets.has(key)) { buckets.set(key, []); order.push(key); }
    buckets.get(key).push(doc);
  });
  const named = order.filter(k => k !== '');
  const unnamed = order.filter(k => k === '');
  return named.concat(unnamed).reduce((acc, k) => acc.concat(buckets.get(k)), []);
}

// Per-doctype picker defaults (Stage 30.5.8).
//
// Location is this app's most-reused master - 15 screens pick one - and
// Employee is picked by six. Before this, "how a location is chosen" was
// decided 15 times over, once per call site, which is exactly how the
// inconsistencies this sweep is closing got in. The defaults live here once;
// attachLinkTypeahead() is the single door every screen goes through, so the
// generic record form's Link field and a JSONTable's link column get the same
// picker as a bespoke screen's hand-built input, for free and forever.
//
// Deliberately only presentation. Nothing here declares schema - the field
// list, the link targets and the validation all stay server-side, which is
// why a doctype missing from this table is not a bug: it just takes the
// unadorned default.
const TYPEAHEAD_DOCTYPE_OPTS = {
  // 103 records across Store / Warehouse / HO. A flat list of eight codes
  // gave no way to tell a shop from a warehouse.
  //
  // Stage 41 flips the label to lead with the NAME. `code` is the system
  // identifier - "HO", "BKC01" - and leading with it meant the picker read as
  // a list of codes with a name appended, which is backwards for a human
  // choosing a place. The code stays visible (staff do use it, and it is what
  // gets stored), just second. short_code, the new optional shorthand, is
  // shown alongside it when a location has one.
  Location: {
    groupBy: 'type',
    showAllOnFocus: true,
    labelFn: doc => {
      const name = doc.name || '';
      const code = doc.code || doc.id || '';
      // A location whose name was never filled in stores its code in both
      // fields, and "HO — HO" is not a label. Show the identifiers alone in
      // that case - the same de-duplication the default labelFn does, just
      // with the two sides swapped.
      const ids = [code, doc.short_code].filter(Boolean).filter(v => v !== name).join(' / ');
      if (!name) return ids;
      return ids ? `${name} — ${ids}` : name;
    }
  },
  // Browsable because this control replaced a <select> that listed everyone
  // (30.5.8) - without it, the conversion would have been a net loss for a
  // user who does not know the employee codes.
  Employee: { showAllOnFocus: true },
};

function attachLinkTypeahead(inputEl, doctype, opts = {}) {
  if (!inputEl) return;
  attachTypeahead(inputEl, doctype, { ...(TYPEAHEAD_DOCTYPE_OPTS[doctype] || {}), ...opts });
  // Stage 41: the setup hint rides along here rather than at each of the
  // ~45 call sites. This function was already the single door every picker
  // goes through (30.5.8), which is exactly what makes attaching the guidance
  // once cover every screen - including ones written after this.
  // opts.noSetupHint opts a picker out; used where the target is not a master
  // a user "sets up" (a free-text suggestion catalogue, a transaction lookup).
  if (!opts.noSetupHint) attachSetupHint(inputEl, doctype);
}

// ---------------------------------------------------------------------------
// attachCodeNamePicker (Stage 41)
//
// A picker whose visible value and stored value are different fields of the
// same record: the user sees and searches the NAME, the form submits the
// CODE.
//
// Built for the POS location box, which showed a raw location code ("HO",
// "BKC01") in a field labelled "Location Code" - correct, and useless to a
// cashier who knows their shop by its name. It could not simply be changed to
// display the name, because the code is what every downstream call needs:
// session open/close, availability lookup, the cart number, the receipt. So
// the two are split - a visible search box and a hidden input carrying the
// code - and every existing reader of the hidden input is untouched.
//
// Deliberately generic rather than POS-specific, because the same mismatch
// exists on 15 other screens that pick a location; this is the mechanism they
// can adopt one at a time without a new pattern being invented each time.
//
// Free typing still works. A user who types a code and tabs away gets it
// resolved on blur (exact code, short code, or name), so muscle memory built
// on the old field is not punished. Text that resolves to nothing clears the
// hidden value rather than submitting something that does not exist - the
// failure the old free-text field had, where a typo'd code reached the server.
function attachCodeNamePicker(displayEl, hiddenEl, doctype, opts = {}) {
  if (!displayEl || !hiddenEl) return;
  const codeOf = doc => doc.code || doc.id || '';
  const nameOf = doc => doc.name || codeOf(doc);

  const commit = (doc) => {
    hiddenEl.value = doc ? codeOf(doc) : '';
    displayEl.value = doc ? nameOf(doc) : displayEl.value;
    displayEl.dataset.resolved = doc ? '1' : '';
    // The change event fires on the HIDDEN input, because that is the element
    // holding the value callers care about and the one they already listen to.
    hiddenEl.dispatchEvent(new Event('change', { bubbles: true }));
  };

  attachLinkTypeahead(displayEl, doctype, { ...opts, onPick: commit });

  // Resolve free-typed text on blur. Deferred so a click on a typeahead row
  // (which blurs the input) is handled by onPick first and this sees the
  // already-resolved state instead of racing it.
  displayEl.addEventListener('blur', () => setTimeout(async () => {
    const text = displayEl.value.trim();
    if (!text) { hiddenEl.value = ''; displayEl.dataset.resolved = ''; hiddenEl.dispatchEvent(new Event('change', { bubbles: true })); return; }
    if (displayEl.dataset.resolved === '1' && nameOf({ name: displayEl.value }) === displayEl.value && hiddenEl.value) return;
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(text)}&limit=10`);
    if (!res || !res.ok) return;
    const rows = await res.json();
    const lower = text.toLowerCase();
    const exact = (rows || []).find(d =>
      String(codeOf(d)).toLowerCase() === lower ||
      String(d.short_code || '').toLowerCase() === lower ||
      String(d.name || '').toLowerCase() === lower);
    if (exact) commit(exact);
    else { hiddenEl.value = ''; displayEl.dataset.resolved = ''; hiddenEl.dispatchEvent(new Event('change', { bubbles: true })); }
  }, 200));

  // Show the name for a code the screen already had (a re-render restoring a
  // previously chosen location), so the box doesn't come back showing the raw
  // code this control exists to hide.
  const seed = hiddenEl.value.trim();
  if (seed && !displayEl.value.trim()) {
    apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(seed)}&limit=10`).then(res => {
      if (!res || !res.ok) return;
      return res.json().then(rows => {
        const match = (rows || []).find(d => String(codeOf(d)).toLowerCase() === seed.toLowerCase());
        if (match) { displayEl.value = nameOf(match); displayEl.dataset.resolved = '1'; }
        else displayEl.value = seed;
      });
    });
  }
}

// ---------------------------------------------------------------------------
// Empty-state guidance (Stage 30.5.1 / 30.5.2)
//
// The 2026-07-30 layman audit found 61 empty-state messages of which only two
// told the user what to do next, and 10 of 18 core master pickers rendering as
// a dropdown containing nothing but "Select employee". A screen that says
// "No employees yet" and stops is a dead end: the user has no way to know that
// the fix is a Setup list two flyouts away.
//
// These three helpers are the whole vocabulary. They are string-returning
// rather than node-returning on purpose - almost every existing empty state is
// built inside a template literal for a <td>, so a string drops straight in
// with no restructuring of the call site.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// JSON line editor (Stage 30.5.3)
//
// Renders a JSONTable (array of row objects) or JSONMap (key/value object)
// field as an add-line table instead of a text box demanding hand-typed JSON.
// The whole thing is driven by the column spec the server puts in the field's
// `options`, so adding a line editor to a new field is one migration row and
// no JavaScript - the same reasoning as 30.6.4's auto_generated flag and
// 30.5.4's setup_advanced.
//
// Deliberately kept as a UI layer over a hidden input rather than a new save
// path: the form's submit handler reads `[name=<fieldname>]`.value, so
// keeping the serialised JSON there means nothing downstream changes.
// ---------------------------------------------------------------------------
function renderJSONLineEditor(fg, f, existingVal) {
  const isMap = f.fieldtype === 'JSONMap';
  let cols = [];
  if (isMap) {
    cols = [
      { key: '__key', label: 'Parameter', type: 'text', required: true },
      { key: '__value', label: 'Value', type: 'text' }
    ];
  } else {
    try { cols = JSON.parse(f.options || '[]'); } catch (e) { cols = []; }
  }

  const hidden = document.createElement('input');
  hidden.type = 'hidden';
  hidden.name = f.fieldname;
  fg.appendChild(hidden);

  // Parse whatever is already stored. A value that isn't valid JSON is not
  // discarded - it is handed to the raw-JSON escape hatch below, so a record
  // saved before this Stage (or edited through the API) can still be opened
  // and repaired rather than silently emptied.
  let rows = [];
  let unparseable = '';
  const raw = (existingVal === undefined || existingVal === null) ? '' : String(existingVal);
  if (raw.trim()) {
    try {
      const parsed = JSON.parse(raw);
      if (isMap && parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        rows = Object.entries(parsed).map(([k, v]) => ({ __key: k, __value: v }));
      } else if (!isMap && Array.isArray(parsed)) {
        rows = parsed;
      } else {
        unparseable = raw;
      }
    } catch (e) {
      unparseable = raw;
    }
  }

  const wrap = document.createElement('div');
  wrap.className = 'json-line-editor';
  fg.appendChild(wrap);

  // Empty string rather than "[]"/"{}" for an untouched optional field, so an
  // optional list that was never filled in stays absent instead of being
  // stored as an empty array - which is what it was before this Stage.
  const serialise = () => {
    const live = collect();
    if (live.length === 0) {
      hidden.value = raw.trim() && !unparseable ? (isMap ? '{}' : '[]') : '';
      return;
    }
    if (isMap) {
      const obj = {};
      live.forEach(r => { if (String(r.__key || '').trim()) obj[r.__key] = r.__value; });
      hidden.value = JSON.stringify(obj);
    } else {
      hidden.value = JSON.stringify(live);
    }
  };

  const collect = () => Array.from(wrap.querySelectorAll('[data-line-row]')).map(tr => {
    const row = {};
    cols.forEach(c => {
      const input = tr.querySelector(`[data-line-key="${c.key}"]`);
      if (!input) return;
      const v = input.value;
      if (v === '') return;
      row[c.key] = (c.type === 'number') ? Number(v) : v;
    });
    return row;
  }).filter(r => Object.keys(r).length > 0);

  const draw = () => {
    wrap.innerHTML = `
      <table class="json-line-table">
        <thead>
          <tr>${cols.map(c => `<th>${escapeHTMLText(c.label || c.key)}${c.required ? '<span class="required">*</span>' : ''}</th>`).join('')}<th></th></tr>
        </thead>
        <tbody></tbody>
      </table>
      <button type="button" class="btn btn-outline btn-sm json-line-add">+ Add Line</button>
      ${unparseable ? `<div class="empty-state-hint">This field holds a value the editor could not read. It is shown below as raw JSON so it can be repaired; the lines above are ignored while it is set.</div>
        <textarea class="form-textarea json-line-raw" rows="3">${escapeHTMLText(unparseable)}</textarea>` : ''}
    `;
    const tbody = wrap.querySelector('tbody');
    if (rows.length === 0) {
      tbody.innerHTML = `<tr><td colspan="${cols.length + 1}" class="json-line-empty">No lines yet. Use <b>+ Add Line</b> below.</td></tr>`;
    }
    rows.forEach((r, idx) => tbody.appendChild(buildRow(r, idx)));

    wrap.querySelector('.json-line-add').addEventListener('click', () => {
      rows = collect();
      rows.push({});
      draw();
      serialise();
    });

    const rawEl = wrap.querySelector('.json-line-raw');
    if (rawEl) {
      rawEl.addEventListener('input', () => { hidden.value = rawEl.value; });
      hidden.value = unparseable;
    }
  };

  const buildRow = (r, idx) => {
    const tr = document.createElement('tr');
    tr.setAttribute('data-line-row', String(idx));
    cols.forEach(c => {
      const td = document.createElement('td');
      const input = document.createElement('input');
      input.className = 'form-input';
      input.setAttribute('data-line-key', c.key);
      input.type = c.type === 'number' ? 'number' : 'text';
      input.value = (r[c.key] === undefined || r[c.key] === null) ? '' : r[c.key];
      input.addEventListener('input', serialise);
      input.addEventListener('change', serialise);
      td.appendChild(input);
      // A link column is a live typeahead against the target doctype, which
      // is also what gives it 30.5.1's "none exist yet" affordance for free.
      if (c.type === 'link' && c.link) attachLinkTypeahead(input, c.link);
      tr.appendChild(td);
    });
    const actions = document.createElement('td');
    const del = document.createElement('button');
    del.type = 'button';
    del.className = 'btn btn-outline btn-sm';
    del.textContent = 'Remove';
    del.addEventListener('click', () => {
      rows = collect();
      rows.splice(idx, 1);
      draw();
      serialise();
    });
    actions.appendChild(del);
    tr.appendChild(actions);
    return tr;
  };

  draw();
  serialise();
}

// escapeHTMLText makes an arbitrary user-supplied string safe to interpolate
// into a template literal that becomes innerHTML. Used where an empty state
// echoes back what the user typed.
function escapeHTMLText(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// ---------------------------------------------------------------------------
// Field formats (Stage 40.2)
//
// Every input that holds a GSTIN, an email, a phone number, a PAN, an IFSC
// code, a PIN code or a URL gets, for free:
//
//   - a placeholder showing what a real one looks like ("it should suggest it
//     should be like this");
//   - keystroke filtering, so a phone field takes digits, + - ( ) and spaces
//     but refuses letters outright;
//   - upper-casing as you type where the format is stored upper-case, so a
//     GSTIN can never fail purely on case;
//   - an inline message on blur that states the rule and shows an example.
//
// None of it makes a field mandatory. Leave it blank and nothing complains -
// the checks only fire once there is something to check.
//
// The specs come from the server (GET /api/v1/meta/field-formats), which is
// the same declaration list ValidateDocument enforces against. That is the
// whole point: a second copy of these regexes in JavaScript would drift, and
// the form would start promising a format the server does not accept.
//
// Applied by delegation on document, plus one sweep per view render, so it
// covers bespoke screens and modals as well as the generic record form -
// without a call site per screen.
// ---------------------------------------------------------------------------

let FIELD_FORMATS = [];
// Stage 41: the suffixes that mark a field as a DERIVED companion of a
// formatted one rather than one itself - "phone_country" holds "US", not a
// phone number. Served by the same endpoint as the tokens, for the same
// reason: a second copy here would drift, and the drift shows up as a
// keystroke filter on a field the user cannot type a valid value into.
let FIELD_FORMAT_EXCLUDED_SUFFIXES = [];

async function loadFieldFormats() {
  try {
    const res = await apiFetch('/api/v1/meta/field-formats');
    if (!res || !res.ok) return;
    const data = await res.json();
    FIELD_FORMATS = (data && data.formats) || [];
    FIELD_FORMAT_EXCLUDED_SUFFIXES = (data && data.excluded_suffixes) || [];
  } catch (e) {
    // A missing spec list degrades to "no hints, no filtering" - the server
    // still validates on save, so nothing becomes unsafe, just less helpful.
    console.debug('[field-formats] not available', e);
  }
}

// isDerivedCompanionField mirrors the server's function of the same name.
function isDerivedCompanionField(name) {
  const n = String(name || '').toLowerCase().trim();
  return FIELD_FORMAT_EXCLUDED_SUFFIXES.some(suf => n.endsWith(suf));
}

// detectFieldFormat mirrors the server's DetectFieldFormat: first token match
// wins, and the list arrives already in the server's own priority order.
function detectFieldFormat(name) {
  const n = String(name || '').toLowerCase().trim();
  if (!n || isDerivedCompanionField(n)) return null;
  for (const spec of FIELD_FORMATS) {
    for (const tok of (spec.tokens || [])) {
      if (n.includes(tok)) return spec;
    }
  }
  return null;
}

// fieldFormatFor resolves an element's format from whichever identifier the
// screen happened to use. Bespoke screens name inputs by id ("po-vendor"),
// the generic form uses name (the fieldname) - both are checked, plus an
// explicit data-field-format escape hatch for anything neither covers.
function fieldFormatFor(el) {
  if (!el || el.tagName !== 'INPUT') return null;
  const explicit = el.getAttribute('data-field-format');
  if (explicit) return FIELD_FORMATS.find(f => f.key === explicit) || null;
  const type = (el.getAttribute('type') || 'text').toLowerCase();
  if (type === 'number' || type === 'checkbox' || type === 'radio' || type === 'hidden' || type === 'date') return null;
  return detectFieldFormat(el.getAttribute('name') || el.id || '');
}

// Escapes a server-supplied character class for safe use inside a RegExp
// character set. The server sends e.g. "0-9+\\-() " - ranges are intentional,
// so this only guards the bracket characters that would break the class.
function fieldFormatCharRegex(allowed) {
  if (!allowed) return null;
  try {
    return new RegExp('[^' + allowed.replace(/\]/g, '\\]').replace(/\^/g, '\\^') + ']', 'g');
  } catch (e) {
    return null;
  }
}

function applyFieldFormatInput(el, spec) {
  const before = el.value;
  let v = before;
  if (spec.uppercase) v = v.toUpperCase();
  const strip = fieldFormatCharRegex(spec.allowed_chars);
  if (strip) v = v.replace(strip, '');
  if (spec.max_len && v.length > spec.max_len) v = v.slice(0, spec.max_len);
  if (v !== before) {
    // Preserve the caret: rewriting .value otherwise jumps it to the end on
    // every keystroke, which makes editing the middle of a GSTIN impossible.
    const pos = el.selectionStart;
    const removed = before.length - v.length;
    el.value = v;
    if (pos !== null && el.setSelectionRange) {
      const next = Math.max(0, pos - removed);
      try { el.setSelectionRange(next, next); } catch (e) { /* not a text input */ }
    }
  }
}

// showFieldFormatMessage puts the rule and an example directly under the
// input. Deliberately not a modal or a toast: this is guidance while typing,
// and DisplayStyle for these codes in the message catalog is already
// "Inline field message".
function showFieldFormatMessage(el, message) {
  let note = el.nextElementSibling;
  if (!note || !note.classList || !note.classList.contains('field-format-note')) {
    note = document.createElement('div');
    note.className = 'field-format-note';
    el.insertAdjacentElement('afterend', note);
  }
  note.textContent = message || '';
  note.classList.toggle('field-format-bad', !!message);
  el.classList.toggle('field-format-invalid', !!message);
}

function validateFieldFormatInput(el, spec) {
  const v = (el.value || '').trim();
  // Empty is always fine. This is the "not mandatory" guarantee, and it has
  // to hold here too or the form would contradict the server.
  if (!v) { showFieldFormatMessage(el, ''); return; }
  showFieldFormatMessage(el, fieldFormatValueIsValid(spec, v) ? '' : spec.hint);
}

// Client-side shape checks, kept deliberately loose - the server's regexes are
// authoritative and run on save. These exist to catch the obvious mistake
// while the cursor is still in the box.
function fieldFormatValueIsValid(spec, v) {
  switch (spec.key) {
    case 'email':   return /^[^\s@,;]+@[^\s@,;]+\.[A-Za-z]{2,}$/.test(v);
    case 'gstin':   return v.length === 15;
    case 'pan':     return v.length === 10;
    case 'ifsc':    return v.length === 11 && v[4] === '0';
    case 'pincode': return /^[1-9][0-9]{5}$/.test(v);
    case 'url':     return /^https?:\/\/[^\s]+\.[^\s]+$/.test(v);
    case 'phone':   return !/[A-Za-z]/.test(v);
    default:        return true;
  }
}

// decorateFieldFormats stamps placeholders and hints onto every recognised
// input inside a container. Idempotent - a re-render decorates the new nodes
// and leaves an already-decorated one alone.
function decorateFieldFormats(root) {
  if (!FIELD_FORMATS.length || !root) return;
  root.querySelectorAll('input').forEach(el => {
    if (el.dataset.fieldFormatDone === '1') return;
    const spec = fieldFormatFor(el);
    if (!spec) return;
    el.dataset.fieldFormatDone = '1';
    el.dataset.fieldFormatKey = spec.key;
    if (!el.getAttribute('placeholder') && spec.placeholder) el.setAttribute('placeholder', spec.placeholder);
    if (spec.max_len && !el.getAttribute('maxlength')) el.setAttribute('maxlength', String(spec.max_len));
    if (spec.key === 'email' && !el.getAttribute('inputmode')) el.setAttribute('inputmode', 'email');
    if (spec.key === 'phone' && !el.getAttribute('inputmode')) el.setAttribute('inputmode', 'tel');
    if (spec.key === 'pincode' && !el.getAttribute('inputmode')) el.setAttribute('inputmode', 'numeric');
    if (!el.getAttribute('title')) el.setAttribute('title', spec.hint);
  });
}

// One delegated pair of listeners for the whole app, so an input added by a
// modal or a line editor after this ran is covered without re-binding.
function initFieldFormatListeners() {
  document.addEventListener('input', (e) => {
    const spec = fieldFormatFor(e.target);
    if (spec) applyFieldFormatInput(e.target, spec);
  }, true);

  document.addEventListener('blur', (e) => {
    const spec = fieldFormatFor(e.target);
    if (spec) validateFieldFormatInput(e.target, spec);
  }, true);
}

// openSetupDoctype navigates to a Master doctype's list, exactly as clicking
// it in the Setup flyout does. Kept in sync with renderSidebarSubmenu()'s own
// click handler (it sets the same active classes) so arriving here from a hint
// leaves the sidebar looking the way arriving here from the menu does.
window.openSetupDoctype = function (doctype) {
  document.querySelectorAll('.submenu-item').forEach(i => i.classList.remove('active'));
  document.querySelectorAll('.menu-item').forEach(i => i.classList.remove('active'));
  const setupMenu = document.getElementById('menu-master-definition');
  if (setupMenu) setupMenu.classList.add('active');
  closeSubmenus();
  currentDoctype = doctype;
  currentSearchQuery = '';
  currentTablePage = 1;
  renderView('doctype-table');
};

// setupLink renders "Setup » Brand" as a real link into that list. Uses an
// inline onclick like the DocType Builder's own module flyout does, rather
// than introducing a second delegation scheme for one link.
function setupLink(doctype, label) {
  const text = label || `Setup &raquo; ${getTranslatedLabel(doctype)}`;
  return `<a href="#" class="empty-state-link" onclick="event.preventDefault(); openSetupDoctype('${doctype}')">${text}</a>`;
}

// emptyHint is the next-step line under an empty state or an empty picker.
// `next` is either a plain string (guidance that points at a control already
// on this screen) or a doctype name to link to.
function emptyHint(next, { asLink = false } = {}) {
  const body = asLink ? setupLink(next) : next;
  return `<div class="empty-state-hint">${body}</div>`;
}

// emptyPickerHint is 30.5.1's affordance: the line that appears under a
// <select> or typeahead whose target list is empty. Rendered by the caller
// right after the control, so it inherits the form-group's own spacing.
function emptyPickerHint(doctype, label) {
  return `<div class="empty-state-hint">No ${getTranslatedLabel(label || doctype)} records exist yet &mdash; ${setupLink(doctype, 'create one first')}.</div>`;
}

// ===========================================================================
// Setup guidance (Stage 41)
//
// The brief: when something the user needs has not been set up, the ERP
// should say so where they are, link straight to it, let that link open in a
// new tab, respect what the user is actually allowed to do, and say all of it
// the same way every time - without turning into a nag.
//
// Four decisions shape everything below.
//
// 1. ONE VOCABULARY. Every hint in the product is one of three sentences
//    (SETUP_MSG). A user learns the phrasing once and then recognises it
//    instantly anywhere, and there is exactly one place to change the wording.
//
// 2. ONE ATTACHMENT POINT. attachLinkTypeahead() is the single door all ~45
//    pickers in this app already go through, and renderView() is the single
//    door every screen goes through. Hooking those two means a screen written
//    next year gets this for free, and no screen can be forgotten.
//
// 3. LOUD WHEN BLOCKING, QUIET OTHERWISE. If the target list is EMPTY the user
//    genuinely cannot proceed, so the hint is always visible. If it has
//    records, the "can't find it? add one" line appears only while the field
//    is focused. That is the difference between guidance and noise - and it is
//    why this is a hint rather than a dialog: nothing here ever interrupts,
//    steals focus, or has to be dismissed before work continues.
//
// 4. PERMISSION-AWARE, ALWAYS. A user who cannot create the record is never
//    shown a link that would refuse them. They get the standard "ask your
//    administrator" sentence instead. state.permissions is already populated
//    for exactly this kind of pre-emptive check (30.5.7).
// ===========================================================================

// The whole vocabulary. Three sentences, one place.
const SETUP_MSG = {
  // Nothing exists yet and the user can fix it themselves.
  missing: (label) => `No ${label} has been set up yet.`,
  // Nothing exists yet and the user is not allowed to fix it. Deliberately
  // says who to ask and what to ask for - "contact your administrator" with
  // no object is the message people ignore.
  missingNoAccess: (label) => `No ${label} has been set up yet. You do not have access to add one &mdash; ask your administrator to set up ${label}.`,
  // Records exist; this is the quiet nudge for when none of them is the one
  // the user wants.
  addMore: (label) => `Can't find the ${label} you need?`
};

// --- deep links -----------------------------------------------------------
//
// Until now this app had no addressable views at all: every screen was
// reached by mutating module state and calling renderView(), so "open this in
// a new tab" was not expressible - a new tab would just reopen whatever was
// in localStorage. A hash route fixes that without a router, a build step or
// a server-side change, because the fragment never reaches the server.
//
//   #/setup/<Doctype>  - a Master record type's list
//   #/view/<view>      - a named view, the same strings renderViewContent takes
function deepLinkForDoctype(doctype) { return `#/setup/${encodeURIComponent(doctype)}`; }
function deepLinkForView(view) { return `#/view/${encodeURIComponent(view)}`; }

// parseDeepLink reads the current fragment, or returns null when there isn't
// one. Tolerant of a stale/hand-edited hash: an unrecognised shape is null,
// which falls through to the normal restore-last-view path.
function parseDeepLink() {
  const raw = (window.location.hash || '').replace(/^#/, '');
  if (!raw.startsWith('/')) return null;
  const parts = raw.slice(1).split('/');
  if (parts.length < 2 || !parts[1]) return null;
  const value = decodeURIComponent(parts[1]);
  if (parts[0] === 'setup') return { kind: 'setup', doctype: value };
  if (parts[0] === 'view') return { kind: 'view', view: value };
  return null;
}

// navigateToDeepLink applies a parsed link. Returns false when it could not
// (an unknown doctype, or one this role cannot read) so the caller can fall
// back rather than render an empty screen with no explanation.
async function navigateToDeepLink(link) {
  if (!link) return false;
  if (link.kind === 'setup') {
    const known = state.activeDoctypes.some(d => d.name === link.doctype);
    if (!known || !canReadDoctype(link.doctype)) return false;
    openSetupDoctype(link.doctype);
    return true;
  }
  if (link.kind === 'view') {
    await renderView(link.view);
    return true;
  }
  return false;
}

// One listener, so a hash typed/pasted into the address bar of an already-open
// tab navigates too - not only a fresh tab. Guarded on being signed in.
window.addEventListener('hashchange', () => {
  if (!localStorage.getItem('erp_token')) return;
  const link = parseDeepLink();
  if (link) navigateToDeepLink(link);
});

// --- setup status ---------------------------------------------------------

async function fetchSetupStatus() {
  try {
    const res = await apiFetch('/api/v1/setup/status');
    if (!res || !res.ok) return;
    const data = await res.json();
    const byDoctype = {};
    (data.masters || []).forEach(m => { byDoctype[m.doctype] = m; });
    state.setupStatus = { byDoctype, loaded: true };
  } catch (e) {
    // Guidance is an enhancement; failing to load it must never break a
    // screen. Everything downstream treats "not loaded" as "no hint".
    console.error('Error fetching setup status:', e);
  }
}

// refreshSetupStatus is called after a master record is created so a hint that
// said "no Vendors" stops saying it the moment one exists.
async function refreshSetupStatus() {
  if (!state.setupStatus.loaded) return;
  await fetchSetupStatus();
}

// isDoctypeSetUp answers the question every hint asks. `undefined` (not
// loaded, or a doctype the status query didn't return) counts as set up, so an
// unknown never produces a false alarm.
function isDoctypeSetUp(doctype) {
  const entry = state.setupStatus.byDoctype[doctype];
  if (!entry) return true;
  return (entry.active || 0) > 0;
}

// --- the standard hint ----------------------------------------------------

// setupOpenLinks renders the pair of affordances every hint ends with: an
// inline link that navigates in place, and an explicit open-in-new-tab button.
//
// Both are real <a href> elements pointing at the deep link, which is what
// makes ctrl-click, middle-click and the browser's own "Open link in new tab"
// work on the inline one too. The in-place link cancels the default so a
// normal click doesn't also push a fragment onto the history stack.
function setupOpenLinks(doctype, inlineLabel) {
  const href = deepLinkForDoctype(doctype);
  const label = getTranslatedLabel(doctype);
  return `<a href="${href}" class="empty-state-link" onclick="event.preventDefault(); openSetupDoctype('${doctype}')">${inlineLabel}</a>` +
    `<a href="${href}" target="_blank" rel="noopener" class="setup-hint-newtab" title="Open ${label} setup in a new tab" aria-label="Open ${label} setup in a new tab">` +
    `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" aria-hidden="true">` +
    `<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line>` +
    `</svg></a>`;
}

// setupHintHTML is the standard hint, in whichever of the three forms applies.
// `mode` is 'missing' (nothing exists) or 'addMore' (records exist, this is
// the quiet nudge).
function setupHintHTML(doctype, mode) {
  const label = getTranslatedLabel(doctype);
  if (!canCreateDoctype(doctype)) {
    // The no-access sentence is shown for a missing master (the user needs to
    // know why they are stuck) but not for addMore - telling someone who
    // cannot create records that they could create one is pure noise.
    return mode === 'missing'
      ? `<span class="setup-hint-text setup-hint-blocked">${SETUP_MSG.missingNoAccess(label)}</span>`
      : '';
  }
  if (mode === 'missing') {
    return `<span class="setup-hint-text">${SETUP_MSG.missing(label)}</span> ${setupOpenLinks(doctype, `Set up ${label}`)}`;
  }
  return `<span class="setup-hint-text">${SETUP_MSG.addMore(label)}</span> ${setupOpenLinks(doctype, `Add a ${label}`)}`;
}

// attachSetupHint puts the standard hint under one picker and keeps it right.
//
// Called from attachLinkTypeahead, which is why every picker in the app gets
// it without a single call site changing. Idempotent - re-attaching to the
// same input (a screen that re-renders) replaces the hint rather than
// stacking a second one.
function attachSetupHint(inputEl, doctype) {
  if (!inputEl || !doctype || !inputEl.parentElement) return;

  let hint = inputEl.parentElement.querySelector(`[data-setup-hint="${doctype}"]`);
  if (!hint) {
    hint = document.createElement('div');
    hint.className = 'setup-hint';
    hint.setAttribute('data-setup-hint', doctype);
    inputEl.insertAdjacentElement('afterend', hint);
  }

  const paint = (focused) => {
    // Only Master record types are in setupStatus. A picker whose target is a
    // transaction (a GRN picking its Purchase Order) gets no hint at all -
    // "set up Purchase Orders" is not advice, and inventing a hint for a
    // doctype we know nothing about is exactly the noise this avoids.
    if (!state.setupStatus.byDoctype[doctype]) { hint.innerHTML = ''; hint.classList.remove('visible'); return; }
    const missing = !isDoctypeSetUp(doctype);
    // Missing is always shown - the user cannot proceed and needs to know.
    // Present is shown only on focus, so a form with eight pickers isn't
    // eight permanent lines of advice nobody asked for.
    if (!missing && !focused) { hint.innerHTML = ''; hint.classList.remove('visible'); return; }
    const html = setupHintHTML(doctype, missing ? 'missing' : 'addMore');
    hint.innerHTML = html;
    hint.classList.toggle('visible', !!html);
    hint.classList.toggle('setup-hint-missing', missing);
  };

  paint(false);
  inputEl.addEventListener('focus', () => paint(true));
  // A click inside the hint (the link) must not tear it down before the click
  // lands, so the blur repaint is deferred a tick.
  inputEl.addEventListener('blur', () => setTimeout(() => paint(false), 150));
}

// --- module-level banner --------------------------------------------------
//
// The per-field hint answers "this one picker is empty". It cannot answer
// "this whole screen will not work until you set two other things up first",
// because the user meets that wall before touching any field. VIEW_SETUP_PREREQS
// is that second answer: the masters each screen genuinely needs.
//
// Kept deliberately short per view - only what the screen truly cannot work
// without. A banner listing eight things is the nag this is trying not to be.
const VIEW_SETUP_PREREQS = {
  'pos': ['Location', 'Item'],
  'purchase-orders': ['Vendor', 'Item'],
  'purchase-requisitions': ['Item'],
  'grn': ['Vendor', 'Location'],
  'asn': ['Vendor', 'Location'],
  'rfq': ['Vendor', 'Item'],
  'fulfillment': ['Location'],
  'putaway': ['Location', 'Bin'],
  'bin-conditions': ['Bin'],
  'cycle-count': ['Location', 'Bin'],
  'lpn': ['Bin'],
  'bin-replenishment': ['Location', 'Bin'],
  'wave-picking': ['Location'],
  'marketplace': ['Item'],
  'oms': ['Item', 'Location'],
  'manufacturing': ['Item', 'Location'],
  'hr': ['Employee'],
  'assets': ['Location'],
  'expenses': ['Employee'],
  'pim': ['Item'],
  'stickers': ['Item']
};

// A dismissal lasts for the browser session only. Deliberate: sessionStorage,
// not localStorage. "Not always" was the ask - but a permanent dismissal would
// mean a user who clicks x once never learns the module is unconfigured again,
// which is how a half-set-up ERP stays half set up.
const SETUP_BANNER_DISMISS_KEY = 'erp_setup_banner_dismissed';

function setupBannerDismissed(view) {
  try {
    return (JSON.parse(sessionStorage.getItem(SETUP_BANNER_DISMISS_KEY) || '[]')).includes(view);
  } catch (e) { return false; }
}

window.dismissSetupBanner = function (view) {
  try {
    const list = JSON.parse(sessionStorage.getItem(SETUP_BANNER_DISMISS_KEY) || '[]');
    if (!list.includes(view)) list.push(view);
    sessionStorage.setItem(SETUP_BANNER_DISMISS_KEY, JSON.stringify(list));
  } catch (e) { /* storage unavailable - the banner just won't stay dismissed */ }
  const el = document.getElementById('setup-banner');
  if (el) el.remove();
};

// renderSetupBanner prepends the banner to a rendered view when that view's
// prerequisites are not met. Called from renderView, so no screen has to
// remember to do it.
function renderSetupBanner(view) {
  const root = document.getElementById('view-root');
  if (!root || !state.setupStatus.loaded) return;
  const existing = document.getElementById('setup-banner');
  if (existing) existing.remove();
  if (setupBannerDismissed(view)) return;

  const prereqs = (VIEW_SETUP_PREREQS[view] || [])
    .filter(dt => state.setupStatus.byDoctype[dt])
    .filter(dt => !isDoctypeSetUp(dt));
  if (prereqs.length === 0) return;

  const canFixAny = prereqs.some(canCreateDoctype);
  const banner = document.createElement('div');
  banner.id = 'setup-banner';
  banner.className = 'setup-banner' + (canFixAny ? '' : ' setup-banner-blocked');
  banner.innerHTML = `
    <div class="setup-banner-body">
      <strong>This screen needs some setup first.</strong>
      <ul class="setup-banner-list">
        ${prereqs.map(dt => `<li>${setupHintHTML(dt, 'missing')}</li>`).join('')}
      </ul>
    </div>
    <button type="button" class="setup-banner-close" aria-label="Dismiss" onclick="dismissSetupBanner('${view}')">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
    </button>
  `;
  root.insertBefore(banner, root.firstChild);
}

// ===========================================================================
// Country-driven phone input rules (Stage 41)
//
// The server cleans and validates every phone number regardless (see
// engines/phone.go). This makes the browser agree with it *while typing*, so
// "phone numbers are 10 digits here" is something the field enforces rather
// than something a rejection message explains afterwards.
// ===========================================================================

// phoneRule returns the tenant's rule, or null before /api/v1/localization has
// landed - in which case nothing is restricted, which is the safe direction.
function phoneRule() {
  return (state.localization && state.localization.rule) || null;
}

// isPhoneFieldName mirrors engines.IsPhoneField. The token list comes from the
// server rather than being retyped here, so the two cannot drift.
function isPhoneFieldName(name) {
  const n = String(name || '').toLowerCase();
  if (!n || isDerivedCompanionField(n)) return false;
  const tokens = (state.localization && state.localization.phone_field_tokens) || [];
  return tokens.some(t => n.includes(t));
}

// applyPhoneInputRule turns one text input into a phone input: numeric
// keyboard on mobile, a live filter that drops anything that isn't a digit
// (or a single leading +), a hard digit cap, and an inline hint naming the
// expected length.
//
// The cap is the country's own maximum - 10 for India. A number typed with a
// leading '+' is an explicit international number, so it is capped at E.164's
// 15 digits instead and left for the server to resolve: refusing to let
// someone type a foreign number would defeat the point of accepting foreign
// orders at all.
function applyPhoneInputRule(input) {
  const rule = phoneRule();
  if (!input || !rule || input.dataset.phoneRuleApplied === '1') return;
  input.dataset.phoneRuleApplied = '1';
  input.setAttribute('inputmode', 'tel');
  input.setAttribute('autocomplete', 'tel');
  if (!input.placeholder) input.placeholder = `e.g. ${rule.example}`;

  const clean = () => {
    const raw = input.value;
    const plus = raw.trim().startsWith('+') ? '+' : '';
    let digits = raw.replace(/[^0-9]/g, '');
    const limit = plus ? 15 : rule.max_length;
    if (digits.length > limit) digits = digits.slice(0, limit);
    const next = plus + digits;
    if (next !== raw) {
      // Preserve the caret when the edit was a pure strip at the end, which
      // is the overwhelmingly common case (typing, or pasting a formatted
      // number). Anything else just goes to the end - acceptable, and far
      // less annoying than the caret jumping on every keystroke.
      const atEnd = input.selectionStart === raw.length;
      input.value = next;
      if (!atEnd) {
        const pos = Math.min(input.selectionStart || next.length, next.length);
        try { input.setSelectionRange(pos, pos); } catch (e) { /* not a text input */ }
      }
    }
  };

  input.addEventListener('input', clean);
  input.addEventListener('blur', clean);
  clean();

  // The hint sits under the field so the rule is visible before the first
  // keystroke, not only after a rejection.
  if (!input.parentElement || input.parentElement.querySelector('.phone-rule-hint')) return;
  const hint = document.createElement('div');
  hint.className = 'empty-state-hint phone-rule-hint';
  hint.innerHTML = `${rule.name} numbers are ${rule.length_label}. ` +
    `For another country, start with <code>+</code> and its dialling code.`;
  input.insertAdjacentElement('afterend', hint);
}

// applyPhoneRulesIn sweeps a container and wires every phone-shaped field in
// it. One call per rendered form beats remembering to wire each field.
function applyPhoneRulesIn(container) {
  if (!container || !phoneRule()) return;
  container.querySelectorAll('input[type="text"], input[type="tel"], input:not([type])').forEach(input => {
    if (isPhoneFieldName(input.name) || isPhoneFieldName(input.id)) applyPhoneInputRule(input);
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
  pendingSessionData = null;
  document.getElementById('login-form').classList.remove('hidden');
  document.getElementById('mfa-enroll-screen').classList.add('hidden');
  document.getElementById('mfa-challenge-screen').classList.add('hidden');
  document.getElementById('mfa-recovery-screen').classList.add('hidden');
  setRecoveryCodeMode(false);
}

// 32.5: the session earned by an MFA step, parked while the display-once
// recovery codes are on screen. Held in memory only - the token must not
// reach localStorage until the user has actually acknowledged the codes,
// otherwise a refresh mid-screen would enter the app and the codes would be
// lost for good (the server keeps only their hashes).
let pendingSessionData = null;

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
  // Reconcile the theme with the server-stored per-user preference (the source
  // of truth across devices) - applies + re-caches locally, no write-back.
  if (data.theme_preference) setTheme(data.theme_preference, false);
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
  setRecoveryCodeMode(false);
  showApp();
  init();

  // 32.5: signing in with a recovery code means the authenticator is
  // presumably gone. Say so, and say what to do about it - otherwise the user
  // burns codes one login at a time and is back to a hard lockout once the
  // last one is spent.
  if (data.used_recovery_code) {
    const left = Number(data.recovery_codes_remaining || 0);
    showToast(
      `Signed in with a recovery code - ${left} left. Open Profile to set up a new authenticator device.`,
      { variant: left <= 2 ? 'danger' : 'warning', title: 'Two-factor recovery', ms: 12000 });
  }
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
    // 32.5: enrollment hands back a set of recovery codes that exist in
    // plaintext exactly once. Park the session and show them first; the app
    // is only entered after the user ticks the acknowledgement.
    if (Array.isArray(data.recovery_codes) && data.recovery_codes.length) {
      pendingSessionData = data;
      showRecoveryCodesScreen(data.recovery_codes);
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

// --- 32.5: MFA recovery codes -------------------------------------------
//
// Before this, a lost phone meant SSH-ing to the server and clearing
// mfa_enabled by hand. These three pieces are the in-app path: entering a
// recovery code instead of a TOTP code, saving the codes at enrollment, and
// (on the profile screen) moving the authenticator to a new device.

// setRecoveryCodeMode retargets the single challenge input between a 6-digit
// TOTP code and a recovery code. The numeric pattern/maxlength have to be
// lifted or the browser's own validation rejects a recovery code before it is
// ever submitted.
function setRecoveryCodeMode(on) {
  const input = document.getElementById('mfa-challenge-code');
  const label = document.querySelector('label[for="mfa-challenge-code"]');
  const hint = document.getElementById('mfa-challenge-hint');
  if (!input || !label || !hint) return;
  if (on) {
    input.setAttribute('pattern', '[A-Za-z0-9 -]{10,14}');
    input.setAttribute('maxlength', '14');
    input.setAttribute('inputmode', 'text');
    input.setAttribute('autocomplete', 'off');
    input.setAttribute('placeholder', 'XXXXX-XXXXX');
    label.textContent = 'Recovery code';
    hint.innerHTML = 'Have your phone? <a href="#" id="mfa-use-totp-link">Use an authenticator code</a>';
    document.getElementById('mfa-use-totp-link').addEventListener('click', (e) => { e.preventDefault(); setRecoveryCodeMode(false); });
  } else {
    input.setAttribute('pattern', '[0-9]{6}');
    input.setAttribute('maxlength', '6');
    input.setAttribute('inputmode', 'numeric');
    input.setAttribute('autocomplete', 'one-time-code');
    input.removeAttribute('placeholder');
    label.textContent = '6-digit code';
    hint.innerHTML = 'Lost your phone? <a href="#" id="mfa-use-recovery-link">Use a recovery code</a>';
    document.getElementById('mfa-use-recovery-link').addEventListener('click', (e) => { e.preventDefault(); setRecoveryCodeMode(true); });
  }
  input.value = '';
}

// showRecoveryCodesScreen renders the display-once list and gates the
// Continue button on the acknowledgement checkbox - the one moment these
// codes are recoverable, since the server stores only their hashes.
function showRecoveryCodesScreen(codes) {
  document.getElementById('mfa-recovery-codes').textContent = codes.join('\n');
  document.getElementById('login-form').classList.add('hidden');
  document.getElementById('mfa-enroll-screen').classList.add('hidden');
  document.getElementById('mfa-challenge-screen').classList.add('hidden');
  const ack = document.getElementById('mfa-recovery-ack');
  const cont = document.getElementById('mfa-recovery-continue-btn');
  ack.checked = false;
  cont.disabled = true;
  document.getElementById('mfa-recovery-screen').classList.remove('hidden');
}

function recoveryCodesText() {
  return document.getElementById('mfa-recovery-codes').textContent;
}

// downloadRecoveryCodes writes the codes to a local .txt via an object URL -
// no server round-trip and no new dependency, the same approach the report
// exports already take.
function downloadRecoveryCodes() {
  const blob = new Blob(
    ['CustomERP two-factor recovery codes\n' +
     'Each code can be used once, in place of your authenticator code.\n\n' +
     recoveryCodesText() + '\n'],
    { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'custom-erp-recovery-codes.txt';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function bindRecoveryCodeScreen() {
  const ack = document.getElementById('mfa-recovery-ack');
  const cont = document.getElementById('mfa-recovery-continue-btn');
  ack.addEventListener('change', () => { cont.disabled = !ack.checked; });
  cont.addEventListener('click', () => {
    const data = pendingSessionData;
    pendingSessionData = null;
    document.getElementById('mfa-recovery-screen').classList.add('hidden');
    if (data) completeLogin(data);
  });
  document.getElementById('mfa-recovery-copy-btn').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(recoveryCodesText());
      showToast('Recovery codes copied to the clipboard', { variant: 'success' });
    } catch (err) {
      showToast('Could not copy - please select the codes and copy them manually', { variant: 'warning' });
    }
  });
  document.getElementById('mfa-recovery-download-btn').addEventListener('click', downloadRecoveryCodes);
  document.getElementById('mfa-use-recovery-link').addEventListener('click', (e) => { e.preventDefault(); setRecoveryCodeMode(true); });
}

function bootstrap() {
  document.getElementById('login-form').addEventListener('submit', handleLoginSubmit);
  document.getElementById('mfa-enroll-form').addEventListener('submit', handleMFAEnrollSubmit);
  document.getElementById('mfa-challenge-form').addEventListener('submit', handleMFAChallengeSubmit);
  bindRecoveryCodeScreen();

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
  // Stage 40.2: the delegated listeners are bound before anything renders, so
  // an input created by the very first view is already covered.
  initFieldFormatListeners();

  // Stage 40.3: these four were four sequential round trips, each waiting on
  // the one before it for no reason - none of them reads the others' results.
  // On a 120ms link that was ~half a second of blank screen before the first
  // view could even start. Issued together, the boot pays for the slowest one
  // instead of the sum. loadFieldFormats joins them because it is needed by
  // the first decorate sweep, which runs after restoreLastView.
  // Stage 41 adds two more to the same batch for the same reason: the setup
  // status and the country/phone rule are needed by the first rendered view
  // (its banner, its pickers, its phone fields) and neither depends on the
  // others, so they cost nothing extra here and would cost a visible pop-in
  // if fetched later.
  await Promise.all([
    fetchLabels(),
    fetchRegisteredDoctypes(),
    fetchAndApplyPermissions(),
    fetchAndApplyModules(),
    loadFieldFormats(),
    fetchSetupStatus(),
    fetchLocalization()
  ]);
  applyProductPathRouting();
  await restoreLastView();
  fetchAndApplyProfile();
}

// fetchLocalization loads the tenant's home country and its phone rule, so the
// browser can enforce the same digit rule the server will.
async function fetchLocalization() {
  try {
    const res = await apiFetch('/api/v1/localization');
    if (!res || !res.ok) return;
    state.localization = await res.json();
  } catch (err) {
    // Same posture as fetchSetupStatus: this is an enhancement over
    // server-side validation, never a substitute for it, so a failure here
    // just means fields stay unrestricted rather than wrongly restricted.
    console.error('Error fetching localization:', err);
  }
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
// Any menu id not listed here defaults open the same way.
const MENU_PERMISSION_MAP = {
  'menu-pos': { open: true },
  'menu-pos-profiles': { doctypes: ['POSProfile'] },
  'menu-pos-offline-sync': { doctypes: ['POSOfflineSyncVariance'] },
  'menu-pos-offline-gaps': { doctypes: ['POSOfflineQueueGap'] },

  'menu-finance': { open: true },
  'menu-approvals': { open: true },
  'menu-vendor-invoices': { doctypes: ['VendorInvoice'] },
  'menu-payment-proposals': { doctypes: ['PaymentProposal', 'VendorInvoice'] },
  'menu-bank-reconciliation': { doctypes: ['BankAccount', 'BankStatementLine'] },
  'menu-finance-notes': { doctypes: ['DebitNote', 'CreditNote'] },
  'menu-sales-invoices': { doctypes: ['SalesInvoice'] },

  'menu-fulfillment': { open: true },
  'menu-marketplace': { open: true },
	'menu-oms': { open: true },
  'menu-customers': { doctypes: ['Customer'] },

  'menu-reports': { open: true },

  'menu-purchase-requisitions': { doctypes: ['PurchaseRequisition'] },
  'menu-purchase-orders': { doctypes: ['PurchaseOrder'] },
  'menu-grn': { doctypes: ['GRN'] },
  'menu-vendors': { doctypes: ['Vendor'] },
  'menu-rfq': { doctypes: ['RFQ'] },

  'menu-inventory': { open: true },
  'menu-transfers': { doctypes: ['TransferOrder'] },
  'menu-bins': { doctypes: ['Bin'] },
  // handlers_wms.go has no role_permissions check today (its own header
  // comment: "All role-open... a warehouse operator role doesn't exist
  // separately from Store Manager/Cashier/HR-Admin") - { open: true } here
  // matches that actual server behavior rather than inventing a UI-only gate.
  'menu-putaway': { open: true },
  'menu-bin-conditions': { open: true },
  'menu-cycle-count': { open: true },
  // Stage 26.5: same role-open convention as the rest of the WMS floor-ops
  // screens above (handlers_wms_enterprise.go has no role_permissions check
  // either) - menu-asn is the exception, gated by the ASN doctype itself
  // since it goes through the generic /api/v1/doc/ASN endpoint.
  'menu-asn': { doctypes: ['ASN'] },
  'menu-lpn': { open: true },
  'menu-bin-replenishment': { open: true },
  'menu-wave-picking': { open: true },
  'menu-mobile-picking': { open: true },
  'menu-stickers': { open: true },

  'menu-hr': { doctypes: ['Employee'] },
  'menu-assets': { open: true },
  'menu-expenses': { doctypes: ['ExpenseClaim'] },

  'menu-manufacturing': { doctypes: ['BOM', 'ProductionOrder'] },
  'menu-pim': { open: true },

  'menu-users': { adminOnly: true },
  'menu-roles': { adminOnly: true },
  'menu-prefix-configs': { adminOnly: true },
  'menu-approval-rules': { adminOnly: true },
  'menu-dynamic-labels': { adminOnly: true },
  'menu-doctype-builder': { adminOnly: true },
  'menu-extension-hooks': { adminOnly: true },
  'menu-audit-logs': { adminOnly: true },
  // System Status dashboard (Stage 26.1.2) - reuses the same HR/Admin-only
  // gate as the ops-visibility endpoints it reads (requireHRAdmin on
  // handleDeploymentStatus/handleBackupStatus, Stage 25.8).
  'menu-system-status': { adminOnly: true },
  // Configuration / system settings (Stage 28.1) - HR/Admin-only, same gate
  // as GET/PUT /api/v1/admin/settings.
  'menu-configuration': { adminOnly: true },
  // Tenant Entitlements admin screen (Stage 26.1.4) - reads/writes
  // cross-tenant module entitlements, HR/Admin-only same as every other
  // admin/tenant-control endpoint it calls.
  'menu-tenant-entitlements': { adminOnly: true },
  // Tenant Usage/health dashboard (Stage 26.1.5) - same HR/Admin-only gate
  // as handleTenantUsage.
  'menu-tenant-usage': { adminOnly: true }
};

// Stage 27 (Modular Product Packaging): which module_key gates each sidebar
// item, mirroring MENU_PERMISSION_MAP's own shape and reasoning exactly -
// only items with a genuine moduleGate(...) on their backing route are
// listed (see internal/server/routes.go). Anything not listed here belongs
// to an is_core module (master_data/inventory/sales/finance/core) that's
// permanently enabled for every tenant, so it never needs hiding by this
// mechanism - same "absence means always-visible" convention
// MENU_PERMISSION_MAP already uses. menu-fulfillment/menu-marketplace map
// to 'wms'/'oms' respectively because their backing routes
// (handleFulfillmentTaskTransition / handleMarketplaceReconcile+
// handleLogisticsBook) were re-gated with moduleGate("wms"/"oms", ...) in
// Stage 27 alongside the older featureGate integration flags.
const MENU_MODULE_MAP = {
  'menu-putaway': 'wms',
  'menu-bin-conditions': 'wms',
  'menu-cycle-count': 'wms',
  'menu-lpn': 'wms',
  'menu-bin-replenishment': 'wms',
  'menu-wave-picking': 'wms',
  'menu-fulfillment': 'wms',
  'menu-marketplace': 'oms',
	'menu-oms': 'oms',

  'menu-purchase-requisitions': 'procurement',
  'menu-purchase-orders': 'procurement',
  'menu-grn': 'procurement',
  'menu-asn': 'procurement',
  'menu-vendors': 'procurement',
  'menu-rfq': 'rfq',

  'menu-stickers': 'stickers',

  'menu-hr': 'hr',
  'menu-assets': 'assets',
  'menu-expenses': 'expenses',

  'menu-manufacturing': 'manufacturing',
  'menu-pim': 'pim'
};

function canReadDoctype(doctype) {
  return state.permissions.isAdmin || state.permissions.doctypes.has(doctype);
}

// Stage 30.5.7: the same shape as canReadDoctype, for the other three verbs.
// These hide an affordance the role cannot use; the server's own check is
// still the enforcement point, exactly as with the sidebar trimming - this
// only stops a user filling in a whole form to be refused at Save.
function canCreateDoctype(doctype) {
  return state.permissions.isAdmin || state.permissions.create.has(doctype);
}

function canUpdateDoctype(doctype) {
  return state.permissions.isAdmin || state.permissions.update.has(doctype);
}

function canDeleteDoctype(doctype) {
  return state.permissions.isAdmin || state.permissions.delete.has(doctype);
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
    // Only <li>s that actually carry a navigable entry count. Since 30.5.4
    // the Setup flyout also contains a filter row, module group headings and
    // an Advanced divider; none of those are ever perm-hidden, so counting
    // them would make the "every child is hidden" test permanently false and
    // the flyout would stay visible for a role with no Master read access at
    // all. The vacuous-true case is preserved deliberately: zero real entries
    // still means hide (see the comment above).
    const items = Array.from(container.querySelectorAll('.menu-flyout > li'))
      .filter(li => li.querySelector('.submenu-item[data-view], .menu-item'));
    const allHidden = items.every(li => li.classList.contains('perm-hidden'));
    container.classList.toggle('perm-hidden', allHidden);
  });
}

// isMenuModuleVisible mirrors isMenuRuleVisible: an item with no
// MENU_MODULE_MAP entry (an is_core module, or no module gate at all) is
// always visible; state.modules.enabled === null means the real entitlement
// set hasn't loaded yet, so default to visible (see state's own comment on
// why a brief full-menu flash beats a brief empty one).
function isMenuModuleVisible(moduleKey) {
  if (!moduleKey) return true;
  if (state.modules.enabled === null) return true;
  return state.modules.enabled.has(moduleKey);
}

// applyModuleEntitlements (Stage 27) is applySidebarPermissions()'s sibling:
// same hide-rather-than-remove approach, same "collapse an empty flyout"
// follow-up pass, but driven by which PRODUCTS this tenant licensed rather
// than which doctypes this role can read - a WMS-only tenant and an
// HR/Admin-role WMS-only tenant should see the same trimmed-down sidebar.
// Uses its own 'module-hidden' class (not 'perm-hidden') so this pass can
// never accidentally un-hide something applySidebarPermissions already
// hid, or vice versa - an item needs both checks to pass to show at all.
function applyModuleEntitlements() {
  Object.keys(MENU_MODULE_MAP).forEach(id => {
    const el = document.getElementById(id);
    if (!el) return;
    const item = el.closest('li');
    if (!item) return;
    item.classList.toggle('module-hidden', !isMenuModuleVisible(MENU_MODULE_MAP[id]));
  });

  document.querySelectorAll('.has-flyout').forEach(container => {
    const items = container.querySelectorAll('.menu-flyout > li');
    const allHidden = items.length > 0 && Array.from(items).every(li => li.classList.contains('module-hidden'));
    container.classList.toggle('module-hidden', allHidden);
  });
}

async function fetchAndApplyModules() {
  try {
    const res = await apiFetch('/api/v1/me/modules');
    if (res && res.ok) {
      const data = await res.json();
      state.modules = {
        enabled: new Set(data.enabled_modules || []),
        solePackage: data.sole_package || null,
        ownedPackages: data.owned_packages || [],
        loaded: true
      };
    }
  } catch (err) {
    console.error('Error fetching module entitlements:', err);
  }
  applyModuleEntitlements();
  renderProductSwitcher();
}

// applyProductPathRouting (Stage 27) is a pure navigation convenience, run
// once at boot right after module entitlements load and before
// restoreLastView() picks a screen - it never affects access control (every
// API route still enforces its own moduleGate regardless of the URL) and
// it deliberately changes nothing about which screen renders, only the
// address bar. A tenant whose enabled (non-core) modules resolve to
// exactly one sellable product (state.modules.solePackage, set server-side
// by engines.ResolveSoleProductPackage) gets bare "/" silently rewritten to
// that product's own URL (e.g. "/" -> "/wms") via replaceState (no reload,
// no history entry) - this is the concrete guarantee that a single-module
// client always lands somewhere scoped to them, never a generic screen
// that half-belongs to products they don't have. A multi-product or
// full-suite tenant (solePackage === null) is untouched - exactly today's
// behavior at "/".
function applyProductPathRouting() {
  if (location.pathname === '/' && state.modules.solePackage) {
    history.replaceState(null, '', state.modules.solePackage.url_prefix);
  }
}

// renderProductSwitcher (Stage 27) shows a small "Switch product" list in
// the sidebar footer, but only when it's actually a meaningful choice: a
// tenant with 2+ licensed products but not the full suite (a single-product
// tenant has nowhere else to switch to; a full-suite tenant already gets
// every module in one sidebar exactly as before, so a switcher would just
// be noise). Plain links using pushState, not a page reload - reuses the
// existing account-menu's dropdown styling rather than introducing a new
// component.
function renderProductSwitcher() {
  const existing = document.getElementById('product-switcher');
  if (existing) existing.remove();

  const owned = state.modules.ownedPackages || [];
  if (owned.length < 2) return;

  const footer = document.querySelector('.sidebar-footer');
  if (!footer) return;

  const el = document.createElement('div');
  el.id = 'product-switcher';
  el.className = 'account-menu';
  el.innerHTML = `
    <select class="form-input" id="product-switcher-select" style="width:100%;">
      <option value="">Switch product...</option>
      ${owned.map(p => `<option value="${p.url_prefix}"${location.pathname === p.url_prefix ? ' selected' : ''}>${p.display_name}</option>`).join('')}
    </select>
  `;
  footer.insertAdjacentElement('beforebegin', el);
  document.getElementById('product-switcher-select').addEventListener('change', (e) => {
    if (e.target.value) history.pushState(null, '', e.target.value);
  });
}

async function fetchAndApplyPermissions() {
  try {
    const res = await apiFetch('/api/v1/me/permissions');
    if (res && res.ok) {
      const data = await res.json();
      state.permissions = {
        isAdmin: !!data.is_admin,
        doctypes: new Set(data.doctypes || []),
        create: new Set(data.create || []),
        update: new Set(data.update || []),
        delete: new Set(data.delete || []),
        loaded: true
      };
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

// Stage 30.5.4: the Setup flyout used to be a flat alphabetical dump of every
// Master doctype - 50+ entries with RoboticsIntegrationCredential and
// ChannelValidationRule sitting between Brand and Color. It is now grouped by
// each doctype's own `module`, filterable, and the system-internal ones are
// filed behind an "Advanced" divider (the setup_advanced flag comes from
// doctype_meta, so there is no doctype list duplicated here in JavaScript).
//
// Persisted per tab, not per session: someone who opened Advanced to reach
// StatusTransitionRule almost always has more than one to change.
let setupMenuFilter = '';
let setupAdvancedOpen = sessionStorage.getItem('erp_setup_advanced_open') === '1';

function renderSidebarSubmenu() {
  const sub = document.getElementById('submenu-master');
  if (!sub) return;
  sub.innerHTML = '';

  const visible = state.activeDoctypes.filter(d => d.document_type === 'Master' && canReadDoctype(d.name));
  const needle = setupMenuFilter.trim().toLowerCase();
  const matches = d =>
    !needle ||
    d.name.toLowerCase().includes(needle) ||
    String(getTranslatedLabel(d.name)).toLowerCase().includes(needle) ||
    String(d.module || '').toLowerCase().includes(needle);

  // The filter row is a <li> so it is a legal child of the <ul>, but it holds
  // no .submenu-item - which is exactly how applySidebarPermissions() tells
  // it apart from a real entry when deciding whether the whole flyout is empty.
  const tools = document.createElement('li');
  tools.className = 'submenu-tools';
  tools.innerHTML = `<input type="text" class="submenu-filter" id="setup-menu-filter" placeholder="Filter setup lists..." value="${escapeHTMLText(setupMenuFilter)}" autocomplete="off">`;
  sub.appendChild(tools);
  const filterInput = tools.querySelector('#setup-menu-filter');
  filterInput.addEventListener('input', (e) => {
    setupMenuFilter = e.target.value;
    renderSidebarSubmenu();
    // Re-rendering replaced the node the user is typing into, so focus and
    // caret have to be restored or every keystroke after the first is lost.
    const fresh = document.getElementById('setup-menu-filter');
    if (fresh) { fresh.focus(); fresh.setSelectionRange(fresh.value.length, fresh.value.length); }
  });
  // Escape inside the filter clears it rather than closing the whole menu,
  // which is what the document-level Escape handler would otherwise do.
  filterInput.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && setupMenuFilter) {
      e.stopPropagation();
      setupMenuFilter = '';
      renderSidebarSubmenu();
      document.getElementById('setup-menu-filter')?.focus();
    }
  });

  const appendEntries = (list) => {
    const byModule = {};
    list.forEach(d => { (byModule[d.module || 'Other'] = byModule[d.module || 'Other'] || []).push(d); });
    Object.keys(byModule).sort().forEach(mod => {
      const heading = document.createElement('li');
      heading.className = 'submenu-group-label';
      heading.textContent = mod;
      sub.appendChild(heading);
      byModule[mod]
        .sort((a, b) => String(getTranslatedLabel(a.name)).localeCompare(String(getTranslatedLabel(b.name))))
        .forEach(d => {
          const li = document.createElement('li');
          li.innerHTML = `<a class="submenu-item" data-view="${d.name}">${getTranslatedLabel(d.name)}</a>`;
          sub.appendChild(li);
        });
    });
  };

  const everyday = visible.filter(d => !d.setup_advanced && matches(d));
  const advanced = visible.filter(d => d.setup_advanced && matches(d));

  appendEntries(everyday);

  if (needle && everyday.length === 0 && advanced.length === 0) {
    const none = document.createElement('li');
    none.className = 'submenu-group-label';
    none.textContent = 'No setup list matches that.';
    sub.appendChild(none);
  }

  if (advanced.length > 0) {
    // A filter hit inside Advanced expands it on its own - otherwise
    // searching for "channel" would report nothing while the match sat
    // hidden behind a collapsed divider.
    const expanded = setupAdvancedOpen || !!needle;
    const divider = document.createElement('li');
    divider.className = 'submenu-advanced-toggle';
    divider.innerHTML = `<a class="submenu-item" href="#" role="button" aria-expanded="${expanded}">
      <span>Advanced (${advanced.length})</span><span class="submenu-advanced-caret">${expanded ? '▾' : '▸'}</span>
    </a>`;
    divider.querySelector('a').addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      setupAdvancedOpen = !expanded;
      sessionStorage.setItem('erp_setup_advanced_open', setupAdvancedOpen ? '1' : '0');
      renderSidebarSubmenu();
    });
    sub.appendChild(divider);
    if (expanded) appendEntries(advanced);
  }
  // Setup's own flyout trigger has no read access left once every Master
  // doctype it lists is filtered out - re-evaluate the flyout-hiding pass
  // now that the list this depends on just changed.
  applySidebarPermissions();

  // Rebind event listeners to submenu items. [data-view] matters: the
  // Advanced divider (30.5.4) is also a .submenu-item so it inherits the
  // menu's styling, but it carries no doctype and has its own handler -
  // without this qualifier the generic handler below would also fire on it
  // and navigate to a doctype named "null".
  sub.querySelectorAll('.submenu-item[data-view]').forEach(item => {
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

	document.getElementById('menu-oms').addEventListener('click', (e) => {
		e.preventDefault();
		setActiveMenu('menu-oms');
		closeSubmenus();
		renderView('oms');
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

  document.getElementById('menu-customers').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-customers');
    closeSubmenus();
    currentDoctype = 'Customer';
    currentSearchQuery = '';
    currentTablePage = 1;
    renderView('doctype-table');
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

  // Purchase Requisition (Stage 26.3.2) - same generic doctype-table pattern
  // as Vendors/Bins below: its schema is flat (no line items), so
  // unlike GRN/Purchase Orders it doesn't need a bespoke screen, just this
  // sidebar entry plus the Submit-for-Approval/Convert row actions added to
  // the generic table itself (renderDocTable).
  document.getElementById('menu-purchase-requisitions').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-purchase-requisitions'); closeSubmenus(); currentDoctype = 'PurchaseRequisition'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  document.getElementById('menu-purchase-orders').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-purchase-orders');
    closeSubmenus();
    renderView('purchase-orders');
  });

  // GRN Workbench (Stage 26.3.1) - dedicated screen, same pattern as
  // Purchase Orders above rather than the generic doctype-table view: GRN's
  // one mandatory field (received_items) is a JSON blob no one could
  // realistically hand-type, so this needs its own line-item form.
  document.getElementById('menu-grn').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-grn');
    closeSubmenus();
    renderView('grn');
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

  // Stage 30.5.5: the `menu-stores` handler was removed here along with its
  // sidebar entry. `Stores` had zero Link references and zero Go references -
  // nothing could ever select one - while `Location` (Type = Store) is what
  // every transaction uses. Its four unique fields (address, city,
  // contact_phone, manager) are Location fields now; see
  // db/migrations_stage30_5_5_retire_stores.sql.

  // POS Profile (Stage 20.6) - same generic doctype-table pattern as Vendors above.
  document.getElementById('menu-pos-profiles').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-profiles'); closeSubmenus(); currentDoctype = 'POSProfile'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Bin (Stage 20.16) - same generic doctype-table pattern as POS Profile/Vendors above.
  document.getElementById('menu-bins').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-bins'); closeSubmenus(); currentDoctype = 'Bin'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Offline Sync Review (Stage 20.13) - same generic doctype-table pattern as POS Profile/Bin above.
  document.getElementById('menu-pos-offline-sync').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-offline-sync'); closeSubmenus(); currentDoctype = 'POSOfflineSyncVariance'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Offline Queue Gaps (24.36) - same generic doctype-table pattern as Offline Sync Review above.
  document.getElementById('menu-pos-offline-gaps').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-offline-gaps'); closeSubmenus(); currentDoctype = 'POSOfflineQueueGap'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  ['menu-inventory', 'menu-transfers', 'menu-putaway', 'menu-bin-conditions', 'menu-cycle-count', 'menu-asn', 'menu-lpn', 'menu-bin-replenishment', 'menu-wave-picking', 'menu-mobile-picking', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-approval-rules', 'menu-dynamic-labels', 'menu-extension-hooks', 'menu-audit-logs', 'menu-system-status', 'menu-configuration', 'menu-tenant-entitlements', 'menu-tenant-usage'].forEach(id => {
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
  setupGlobalSearchSuggest(globalSearch);

  // Sync / Reset Database
  document.getElementById('sync-btn').addEventListener('click', async () => {
    if (await showCustomConfirm('Re-fetch translation cache and active schema fields?')) {
      await fetchLabels();
      await fetchRegisteredDoctypes();
      renderView(currentView);
    }
  });

  // Stage 39.5: contextual help for the screen you are on.
  document.getElementById('help-btn')?.addEventListener('click', () => openHelpDrawer(currentView));

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
          renderView(currentView);
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
// Theme (Stage 28.2): light / dark / system. 'system' (and any unknown value)
// lets the CSS media query follow the OS; 'light'/'dark' force a choice. The
// choice is cached in localStorage (applied pre-paint by the inline script in
// index.html <head>, so there's no light-mode flash) and persisted per-user
// server-side via PUT /api/v1/me, so it follows the user across devices -
// reconciled against the server value on load in fetchAndApplyProfile.
const THEME_STORAGE_KEY = 'erp-theme';
const VALID_THEMES = ['light', 'dark', 'system'];

function getStoredTheme() {
  const t = localStorage.getItem(THEME_STORAGE_KEY);
  return VALID_THEMES.includes(t) ? t : 'system';
}

function applyTheme(pref) {
  const t = VALID_THEMES.includes(pref) ? pref : 'system';
  document.documentElement.setAttribute('data-theme', t);
  document.querySelectorAll('.theme-seg-btn').forEach(btn => {
    btn.classList.toggle('active', btn.getAttribute('data-theme-choice') === t);
  });
}

function setTheme(pref, persistToServer) {
  applyTheme(pref);
  try { localStorage.setItem(THEME_STORAGE_KEY, pref); } catch (e) {}
  if (persistToServer && localStorage.getItem('erp_token')) {
    // Fire-and-forget: a failed save just means the choice stays local this
    // session; it's already applied and cached either way.
    apiFetch('/api/v1/me', { method: 'PUT', body: JSON.stringify({ theme_preference: pref }) });
  }
}

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

  // Theme selector: reflect the current choice and wire the three segments.
  applyTheme(getStoredTheme());
  document.querySelectorAll('.theme-seg-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      setTheme(btn.getAttribute('data-theme-choice'), true);
    });
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

// Visual gap between a module row and its flyout panel. The same value is
// spanned by the invisible hover bridge below, so the gap is only a gap to
// the eye - never to the pointer.
const FLYOUT_GAP_PX = 8;
// Grace period before a flyout closes once the pointer has genuinely left
// both the module row and the flyout. Long enough to survive an overshoot,
// short enough not to feel sticky.
const FLYOUT_HIDE_DELAY_MS = 200;

// One shared hide timer for the whole sidebar, deliberately not one per
// container (Stage 28.5 fix): with per-container timers, sliding from module
// A down to module B left A's already-scheduled timer running, and 200ms
// later it fired the *global* closeSubmenus() and shut B's freshly-opened
// flyout. The menu vanished from under the pointer and the click that
// followed landed on the page behind it - which is what "I click the menu
// and nothing opens" actually was. A single timer means opening anything
// cancels the pending close, whichever module scheduled it.
let flyoutHideTimer = null;
// Dwell before hovering a *different* module row takes the open menu away
// from the current one. Its only job is to stop a fast sweep down the sidebar
// strobing every module's menu on the way past; it sits well under the ~100ms
// a human reads as instant, so a row you actually stop on opens with no
// perceptible wait.
const FLYOUT_SWITCH_DELAY_MS = 60;
// Longer dwell, applied ONLY while the pointer is genuinely still travelling
// into the panel that is already open - i.e. cutting the corner diagonally
// across the rows between that module and the item it is aiming at. It is
// re-evaluated on every move, so the moment the pointer stops aiming the wait
// collapses back to FLYOUT_SWITCH_DELAY_MS instead of running to term.
const FLYOUT_AIM_GRACE_MS = 220;
let flyoutOpenTimer = null;
// Which container a pending open belongs to, so leaving row B can never cancel
// an open that row C has already scheduled.
let flyoutOpenTarget = null;

// Pointer trail for the aim test, sampled over a short window rather than
// frame to frame: at 120Hz a single frame's delta is sub-pixel and mostly
// noise, which is why the previous test came out true on idle jitter.
const AIM_WINDOW_MS = 90;
// Minimum horizontal travel across that window to count as a deliberate reach
// for the panel rather than drift.
const AIM_MIN_DX_PX = 12;
// Vertical tolerance on where the trajectory is projected to cross the panel.
const AIM_SLOP_PX = 24;

let flyoutPointerSamples = [];
document.addEventListener('pointermove', (e) => {
  const now = performance.now();
  flyoutPointerSamples.push({ x: e.clientX, y: e.clientY, t: now });
  // Keep just enough history to span AIM_WINDOW_MS. The length cap is a
  // belt-and-braces bound: at a real pointer's 8-16ms sample rate the time
  // test alone holds this at ~12 entries, but it never prunes at all for
  // samples that share a timestamp, so the cap keeps a synthetic or coalesced
  // burst from growing the array without limit.
  while (flyoutPointerSamples.length > 2 &&
         (now - flyoutPointerSamples[1].t > AIM_WINDOW_MS || flyoutPointerSamples.length > 32)) {
    flyoutPointerSamples.shift();
  }
}, { passive: true, capture: true });

// True only when the pointer is on a trajectory that actually lands inside the
// open flyout panel: moving right, fast enough to be deliberate, and aimed at
// the panel's vertical span.
//
// The previous test asked only "is the pointer moving right, and is the panel
// to its right". The panel always sits to the right of the sidebar, so the
// second half was true for essentially every pointer position in the nav, and
// the first half was a single noisy sample. Every module switch therefore paid
// the full 500ms aim grace roughly half the time, at random - and if the user
// moved rightwards to where they expected the menu during that wait, they left
// the row, the pending open was cancelled and the hide fired instead, so no
// menu ever appeared. Clicking the arrow bypassed the whole ladder, which is
// why clicking was the only thing that reliably worked.
function pointerAimingAtOpenFlyout(exceptContainer) {
  const openContainer = document.querySelector('.has-flyout.flyout-open');
  if (!openContainer || openContainer === exceptContainer) return false;
  const panel = openContainer.querySelector('.menu-flyout');
  if (!panel) return false;

  const first = flyoutPointerSamples[0];
  const last = flyoutPointerSamples[flyoutPointerSamples.length - 1];
  if (!first || !last || first === last) return false;
  // Pointer has come to rest - resting is intent to switch, not to travel.
  if (performance.now() - last.t > AIM_WINDOW_MS * 2) return false;

  const dx = last.x - first.x;
  const dy = last.y - first.y;
  if (dx < AIM_MIN_DX_PX) return false;

  const r = panel.getBoundingClientRect();
  const runway = r.left - last.x;
  if (runway <= 0) return false;   // already level with or past the panel
  const projectedY = last.y + (dy / dx) * runway;
  return projectedY >= r.top - AIM_SLOP_PX && projectedY <= r.bottom + AIM_SLOP_PX;
}

function cancelFlyoutHide() {
  if (flyoutHideTimer) { clearTimeout(flyoutHideTimer); flyoutHideTimer = null; }
}

// Pass a container to cancel only an open that container itself scheduled.
function cancelFlyoutOpen(onlyFor) {
  if (onlyFor && flyoutOpenTarget && flyoutOpenTarget !== onlyFor) return;
  if (flyoutOpenTimer) { clearTimeout(flyoutOpenTimer); flyoutOpenTimer = null; }
  flyoutOpenTarget = null;
}

function scheduleFlyoutHide() {
  cancelFlyoutHide();
  flyoutHideTimer = setTimeout(() => { flyoutHideTimer = null; closeSubmenus(); }, FLYOUT_HIDE_DELAY_MS);
}

// Positions a module's flyout beside its trigger (JS-computed, not CSS
// position:absolute, so it's never clipped by .sidebar-menu's own
// overflow-y:auto) and shows it.
function openFlyout(container) {
  const trigger = container.querySelector('.menu-item-group');
  const flyout = container.querySelector('.menu-flyout');
  if (!trigger || !flyout) return;

  // Any pending close is for a menu the pointer has since come back to (or
  // moved on from) - either way it must not fire against this one. Exactly
  // one module flyout is open at a time.
  cancelFlyoutHide();
  closeSubmenus(container);

  const rect = trigger.getBoundingClientRect();
  const margin = 12;
  flyout.style.left = `${Math.round(rect.right + FLYOUT_GAP_PX)}px`;
  flyout.classList.add('open');   // must be displayed before scrollHeight is meaningful

  // A long flyout (Stock's 11 screens, Master Definition's ~25 master
  // doctypes) anchored to a trigger low in the sidebar used to be capped at
  // the space *below* that trigger, which pushed its last items off the
  // bottom of the screen - reachable only by scrolling inside the menu, and
  // in practice not reachable at all, because the pointer had to leave the
  // menu to get there. Instead: give it its natural height where that fits,
  // and slide the whole panel up so its bottom stays on screen. Only a menu
  // taller than the entire viewport still scrolls internally.
  const viewportMax = Math.max(120, window.innerHeight - margin * 2);
  const naturalHeight = flyout.scrollHeight;
  const height = Math.min(naturalHeight, viewportMax);
  let top = Math.round(rect.top);
  if (top + height > window.innerHeight - margin) {
    top = Math.round(Math.max(margin, window.innerHeight - margin - height));
  }
  flyout.style.top = `${top}px`;
  flyout.style.maxHeight = `${viewportMax}px`;
  container.classList.add('flyout-open');

  // Invisible hover bridge over that gap. The gap is outside the container's
  // own box, so a pointer crossing it - or pausing in it, which anyone
  // reaching for a submenu item does - fired container's mouseleave and
  // started the close. The bridge is a child of the container, so the
  // mouseenter/mouseleave pair counts it as "still on the menu", and it is
  // pure hit area: transparent, painted nothing, removed from the flow.
  let bridge = container.querySelector('.menu-flyout-bridge');
  if (!bridge) {
    bridge = document.createElement('div');
    bridge.className = 'menu-flyout-bridge';
    bridge.setAttribute('aria-hidden', 'true');
    container.appendChild(bridge);
  }
  // Spans the full vertical range of both the row and the panel, since the
  // panel may now sit above the row as well as below it.
  const bridgeTop = Math.min(top, Math.round(rect.top));
  const bridgeBottom = Math.max(top + height, Math.round(rect.bottom));
  bridge.style.top = `${bridgeTop}px`;
  bridge.style.left = `${Math.round(rect.right)}px`;
  bridge.style.width = `${FLYOUT_GAP_PX + 2}px`;
  bridge.style.height = `${bridgeBottom - bridgeTop}px`;
  bridge.style.display = 'block';
}

// Closes every open module flyout. Pass a container to leave that one open
// (used by openFlyout to swap which module is showing without a flicker).
function closeSubmenus(except) {
  if (!except) cancelFlyoutOpen();
  document.querySelectorAll('.has-flyout.flyout-open').forEach(c => {
    if (c === except) return;
    c.classList.remove('flyout-open');
    const f = c.querySelector('.menu-flyout');
    if (f) f.classList.remove('open');
    const b = c.querySelector('.menu-flyout-bridge');
    if (b) b.style.display = 'none';
  });
  // Defensive: catch any flyout left marked open without its container class.
  document.querySelectorAll('.menu-flyout.open').forEach(f => {
    if (except && except.contains(f)) return;
    f.classList.remove('open');
  });
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
  // Sibling entries with no flyout of their own (Reports, Manufacturing, PIM). Hovering one should put the open menu away - but on
  // the same dwell rule as switching modules, because the diagonal from a
  // module row down to an item near the bottom of its flyout sweeps straight
  // across these too. Without this they were dead zones: the pointer sat on
  // one with nothing listening, and the close scheduled on the way out of
  // the module row went through unopposed.
  document.querySelectorAll('.menu-item-container:not(.has-flyout)').forEach(item => {
    if (item.dataset.flyoutBound) return;
    item.dataset.flyoutBound = '1';
    item.addEventListener('pointerenter', () => {
      cancelFlyoutOpen();
      cancelFlyoutHide();
      const delay = pointerAimingAtOpenFlyout(null) ? FLYOUT_AIM_GRACE_MS : FLYOUT_SWITCH_DELAY_MS;
      flyoutHideTimer = setTimeout(() => { flyoutHideTimer = null; closeSubmenus(); }, delay);
    });
  });

  document.querySelectorAll('.has-flyout').forEach(container => {
    if (container.dataset.flyoutBound) return;
    const trigger = container.querySelector('.menu-item-group');
    const flyout = container.querySelector('.menu-flyout');
    if (!trigger || !flyout) return;
    container.dataset.flyoutBound = '1';
    const show = () => { cancelFlyoutOpen(); openFlyout(container); };

    // When the pointer arrived on this row. The dwell is measured from here
    // rather than restarted per move, so the required wait only ever counts
    // down - a re-evaluated aim test can shorten it but never extend it.
    let hoverStart = 0;

    // Hover-open. Instant when nothing is open yet; when another module's
    // menu is already showing, this row has to be dwelt on briefly before it
    // takes over, so a pointer merely travelling across it on the way to that
    // other menu doesn't yank it away mid-reach.
    const showOnHover = () => {
      cancelFlyoutHide();
      if (container.classList.contains('flyout-open')) return;
      if (!document.querySelector('.has-flyout.flyout-open')) { cancelFlyoutOpen(); show(); return; }
      if (!hoverStart) hoverStart = performance.now();
      const needed = pointerAimingAtOpenFlyout(container) ? FLYOUT_AIM_GRACE_MS : FLYOUT_SWITCH_DELAY_MS;
      const remaining = Math.max(0, needed - (performance.now() - hoverStart));
      if (remaining === 0) { cancelFlyoutOpen(); show(); return; }
      cancelFlyoutOpen();
      flyoutOpenTarget = container;
      flyoutOpenTimer = setTimeout(() => {
        flyoutOpenTimer = null;
        flyoutOpenTarget = null;
        show();
      }, remaining);
    };

    const onPointerEnter = () => { hoverStart = performance.now(); showOnHover(); };

    // Open as soon as the pointer reaches the module row or its arrow, and
    // keep it open for as long as the pointer is anywhere in the container's
    // subtree - the row, the bridge, or the flyout itself. Because the flyout
    // and bridge are DOM children of the container (even though both are
    // position:fixed), moving between them never leaves the container, so
    // mouseleave only fires when the pointer has genuinely gone elsewhere.
    // Pointer events cover both the persistent sidebar and the dynamically
    // rendered Schema Designer module list; focus keeps it keyboard-usable.
    container.addEventListener('pointerenter', onPointerEnter);
    trigger.addEventListener('pointerenter', onPointerEnter);
    // pointermove, not just pointerenter: while the pointer is resting on a
    // module row, no enter event will ever fire again, so anything that shut
    // the menu meanwhile (navigating from it, a click elsewhere, Escape) left
    // it shut even though the cursor was still sitting right there. Any
    // movement at all over the row brings it straight back.
    //
    // It also re-runs the aim test on a pending open, which is what stops the
    // aim grace from outliving the reach it was granted for: sweep towards the
    // open panel and this row waits, stop sweeping and it opens on the next
    // move. Previously a pending timer made this handler a no-op, so whichever
    // delay was picked on entry ran to term no matter what the pointer did.
    container.addEventListener('pointermove', showOnHover);
    // pointerleave, NOT mouseleave: pointer events fire ahead of their
    // compatibility mouse events, so a mouseleave here landed *after* the
    // next row's pointerenter had already cancelled the pending close - it
    // then scheduled a fresh close that nothing cancelled, and the menu shut
    // 260ms later while the pointer was still on its way to it. Same family
    // on both sides keeps the order leave-then-enter.
    container.addEventListener('pointerleave', () => {
      hoverStart = 0;
      cancelFlyoutOpen(container);
      scheduleFlyoutHide();
    });
    container.addEventListener('focusin', show);
    container.addEventListener('focusout', (e) => {
      if (!container.contains(e.relatedTarget)) scheduleFlyoutHide();
    });

    // Clicking the module row only ever OPENS - it is a fallback for touch
    // and for anyone who clicks out of habit, never a toggle. A toggle here
    // meant a row you had already hovered open closed on the click, so the
    // menu appeared to need clicking two or three times to stick. Closing is
    // moving away, clicking outside the nav, or Escape.
    trigger.addEventListener('click', (e) => {
      e.preventDefault();
      show();
    });
  });

  if (moduleFlyoutDocListenersBound) return;
  moduleFlyoutDocListenersBound = true;
  document.addEventListener('click', (e) => {
    if (!e.target.closest('.has-flyout')) closeSubmenus();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeSubmenus();
  });
  window.addEventListener('resize', () => closeSubmenus());
  const sidebarMenu = document.querySelector('.sidebar-menu');
  // Scrolling the sidebar moves the trigger the flyout is anchored to, so
  // reposition the open one instead of closing it - closing mid-scroll was
  // another way the menu disappeared while the user was still using it.
  if (sidebarMenu) {
    sidebarMenu.addEventListener('scroll', () => {
      const open = document.querySelector('.has-flyout.flyout-open');
      if (open) openFlyout(open);
    });
  }
}

// Global search (top bar). It used to filter only the table you already had
// open, so on any screen that isn't a record table
// typing did nothing whatsoever, despite the placeholder offering to search
// menus and record types. It now also suggests every destination the query
// matches: each sidebar entry (including the ones tucked inside a module
// flyout, which are otherwise only reachable by hovering the right module)
// and each registered record type. The table filtering it already did is
// untouched and still happens alongside.
//
// Picking a suggestion dispatches a click on the real sidebar element
// wherever one exists, so routing, permission filtering and active-item
// highlighting all keep living in exactly one place rather than being
// duplicated here. The dropdown reuses .typeahead-menu/.typeahead-item, the
// same vocabulary attachTypeahead() already renders.
const GLOBAL_SEARCH_LIMIT = 12;

function buildGlobalSearchIndex() {
  const entries = [];
  const seen = new Set();
  const isHidden = el => el.classList.contains('perm-hidden') || el.classList.contains('module-hidden');

  document.querySelectorAll('.sidebar-menu .menu-item-container').forEach(li => {
    if (isHidden(li)) return;
    const group = li.querySelector(':scope > .menu-item-group');
    const moduleLabel = group ? group.textContent.trim() : '';
    const anchors = group
      ? [...li.querySelectorAll(':scope > .menu-flyout > li')]
          .filter(row => !isHidden(row))
          .map(row => row.querySelector('.menu-item, .submenu-item'))
          .filter(Boolean)
      : [li.querySelector(':scope > .menu-item')].filter(Boolean);
    anchors.forEach(a => {
      const label = a.textContent.trim();
      if (!label) return;
      const key = 'nav:' + (a.id || `${moduleLabel}/${label}`);
      if (seen.has(key)) return;
      seen.add(key);
      seen.add('label:' + label.toLowerCase());
      entries.push({ kind: 'Screen', label, context: moduleLabel, el: a });
    });
  });

  (state.activeDoctypes || []).forEach(d => {
    const key = 'doc:' + d.name;
    // A record type the sidebar already lists as a screen (the Setup module's
    // Master Definition submenu lists many) would otherwise appear twice
    // under the same name - keep the screen, which navigates the same place.
    if (seen.has('label:' + getTranslatedLabel(d.name).toLowerCase())) return;
    if (seen.has(key)) return;
    seen.add(key);
    entries.push({
      kind: 'Record type',
      label: getTranslatedLabel(d.name),
      context: d.module || '',
      doctype: d.name,
      raw: d.name
    });
  });

  return entries;
}

function matchGlobalSearch(entries, query) {
  const needle = query.trim().toLowerCase();
  if (!needle) return [];
  const scored = [];
  entries.forEach(entry => {
    const label = entry.label.toLowerCase();
    const hay = `${label} ${entry.context} ${entry.raw || ''}`.toLowerCase();
    const hayIdx = hay.indexOf(needle);
    if (hayIdx < 0) return;
    const labelIdx = label.indexOf(needle);
    // A hit on the entry's own name beats one on its module; an earlier hit
    // beats a later one. Keeps "buy" surfacing Buying's screens above a
    // record type that merely mentions it.
    const rank = labelIdx === 0 ? 0 : labelIdx > 0 ? 20 : 100;
    scored.push({ entry, score: rank + Math.min(hayIdx, 19) });
  });
  scored.sort((a, b) => a.score - b.score || a.entry.label.localeCompare(b.entry.label));
  return scored.slice(0, GLOBAL_SEARCH_LIMIT).map(s => s.entry);
}

function setupGlobalSearchSuggest(inputEl) {
  if (!inputEl || inputEl.dataset.suggestBound) return;
  inputEl.dataset.suggestBound = '1';
  inputEl.setAttribute('autocomplete', 'off');

  let menu = null;
  let results = [];
  let activeIndex = -1;

  const onDocMouseDown = (e) => {
    if (menu && !menu.contains(e.target) && e.target !== inputEl) close();
  };

  function close() {
    if (menu) { menu.remove(); menu = null; }
    document.removeEventListener('mousedown', onDocMouseDown, true);
    results = [];
    activeIndex = -1;
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

  function pick(entry) {
    close();
    inputEl.value = '';
    currentSearchQuery = '';
    currentTablePage = 1;
    inputEl.blur();
    if (entry.el) {
      // The sidebar's own handler owns this destination - let it run.
      entry.el.click();
      return;
    }
    closeSubmenus();
    currentDoctype = entry.doctype;
    renderView('doctype-table');
  }

  function render(query) {
    close();
    results = matchGlobalSearch(buildGlobalSearchIndex(), query);
    if (!query.trim()) return;

    menu = document.createElement('div');
    menu.className = 'typeahead-menu global-search-menu';
    const rect = inputEl.getBoundingClientRect();
    menu.style.left = `${Math.round(rect.left)}px`;
    menu.style.top = `${Math.round(rect.bottom + 6)}px`;
    menu.style.width = `${Math.round(Math.max(rect.width, 280))}px`;

    if (results.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'global-search-empty';
      empty.textContent = `No menu or record type matches "${query.trim()}"`;
      menu.appendChild(empty);
    } else {
      results.forEach((entry) => {
        const row = document.createElement('div');
        row.className = 'typeahead-item global-search-item';
        const label = document.createElement('span');
        label.className = 'global-search-label';
        label.textContent = entry.label;
        const meta = document.createElement('span');
        meta.className = 'global-search-meta';
        meta.textContent = entry.context ? `${entry.kind} · ${entry.context}` : entry.kind;
        row.appendChild(label);
        row.appendChild(meta);
        row.addEventListener('mousedown', (e) => { e.preventDefault(); pick(entry); });
        menu.appendChild(row);
      });
    }
    document.body.appendChild(menu);
    document.addEventListener('mousedown', onDocMouseDown, true);
  }

  inputEl.addEventListener('input', () => render(inputEl.value));
  inputEl.addEventListener('focus', () => { if (inputEl.value.trim()) render(inputEl.value); });
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { close(); return; }
    if (!menu || results.length === 0) return;
    if (e.key === 'ArrowDown') { e.preventDefault(); highlight(Math.min(activeIndex + 1, results.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); highlight(Math.max(activeIndex - 1, 0)); }
    else if (e.key === 'Enter') {
      e.preventDefault();
      pick(results[activeIndex >= 0 ? activeIndex : 0]);
    }
  });
  window.addEventListener('resize', close);
}

// Maps a static view name to the sidebar menu item that represents it, for
// restoring the correct highlighted item after a refresh. doctype-table is
// handled separately below since it points at a submenu item, not a top-level one.
const STATIC_VIEW_MENU_IDS = {
  pos: 'menu-pos',
  finance: 'menu-finance',
  fulfillment: 'menu-fulfillment',
  marketplace: 'menu-marketplace',
	oms: 'menu-oms',
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
  grn: 'menu-grn',
  inventory: 'menu-inventory',
  transfers: 'menu-transfers',
  putaway: 'menu-putaway',
  'bin-conditions': 'menu-bin-conditions',
  'cycle-count': 'menu-cycle-count',
  asn: 'menu-asn',
  lpn: 'menu-lpn',
  'bin-replenishment': 'menu-bin-replenishment',
  'wave-picking': 'menu-wave-picking',
  'mobile-picking': 'menu-mobile-picking',
  users: 'menu-users',
  roles: 'menu-roles',
  'prefix-configs': 'menu-prefix-configs',
  'approval-rules': 'menu-approval-rules',
  'dynamic-labels': 'menu-dynamic-labels',
  'extension-hooks': 'menu-extension-hooks',
  'extension-hook-log': 'menu-extension-hooks',
  'audit-logs': 'menu-audit-logs',
  'system-status': 'menu-system-status',
  'configuration': 'menu-configuration',
  'tenant-entitlements': 'menu-tenant-entitlements',
  'tenant-usage': 'menu-tenant-usage',
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
// always bouncing back to DEFAULT_VIEW after a refresh. Falls back to
// DEFAULT_VIEW if the saved doctype no longer exists (e.g. it was deleted
// elsewhere), or if the saved view itself no longer exists - which every
// browser that was last on the retired Dashboard has in localStorage, and
// which would otherwise restore to a permanently blank screen.
async function restoreLastView() {
  // Stage 39.4: /help and /help/<slug> are real URLs. A tab opened on one must
  // land on that article, ahead of any deep link or saved view - the whole
  // point of giving articles their own URL is that the link works cold.
  if (location.pathname === '/help' || location.pathname.startsWith('/help/')) {
    currentHelpSlug = decodeURIComponent(location.pathname.slice('/help/'.length) || '');
    await renderView('help');
    return;
  }

  // Stage 41: a deep link beats the saved view. This is what makes the
  // hints' "open in a new tab" affordance real - the new tab arrives carrying
  // #/setup/Vendor and must land on Vendors, not on whatever screen the
  // original tab happened to leave in localStorage. A link that can't be
  // resolved (unknown record type, or one this role can't read) falls through
  // to the normal restore rather than showing an empty screen.
  const link = parseDeepLink();
  if (link && await navigateToDeepLink(link)) return;

  const saved = loadNavState();
  let view = DEFAULT_VIEW;
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
    } else if (saved.view !== 'dashboard') {
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
// renderView (32.2) is a thin wrapper that owns the *feedback*: it clears the
// old screen, shows a loading placeholder, and only then hands off to
// renderViewContent's dispatch.
//
// The reason it exists: renderViewContent blanks #view-root and then awaits a
// fetch, so on anything slower than localhost the user got an empty white
// panel with no indication that a click had registered - and clicked again.
// That is the most likely remaining cause of the "I have to click many times"
// report behind Stage 32, and no amount of transition tuning would have fixed
// it, because nothing was being rendered to transition.
async function renderView(view) {
  const root = document.getElementById('view-root');
  root.innerHTML = '';
  root.scrollTop = 0;

  const placeholder = document.createElement('div');
  placeholder.className = 'view-loading';
  placeholder.innerHTML = '<div class="view-loading-bar"></div><span>Loading&hellip;</span>';
  root.appendChild(placeholder);

  try {
    await renderViewContent(view);
  } finally {
    // finally, not after the await: a renderer that throws must not leave a
    // permanent "Loading..." on screen, which would be a worse lie than the
    // blank panel this replaces.
    placeholder.remove();
    // Stage 40.2: one sweep per render decorates every recognised input the
    // view just built with its placeholder and hint. Done here rather than in
    // each renderer so bespoke screens get it without a call site each.
    decorateFieldFormats(root);
    // Stage 41, same reasoning, same door. The banner is attached in
    // `finally` deliberately: a screen that failed to render still tells the
    // user WHY when the reason is missing setup, which is the case where the
    // explanation matters most.
    renderSetupBanner(view);
    applyPhoneRulesIn(root);
  }
}

async function renderViewContent(view) {
  currentView = view;
  saveNavState();
  const root = document.getElementById('view-root');

  if (view === 'pos') {
    renderPOSView(root);
  } else if (view === 'finance') {
    await renderFinanceView(root);
  } else if (view === 'fulfillment') {
    await renderFulfillmentView(root);
  } else if (view === 'putaway') {
    await renderPutawayView(root);
  } else if (view === 'bin-conditions') {
    await renderBinConditionsView(root);
  } else if (view === 'cycle-count') {
    await renderCycleCountView(root);
  } else if (view === 'asn') {
    await renderASNView(root);
  } else if (view === 'lpn') {
    await renderLPNView(root);
  } else if (view === 'bin-replenishment') {
    await renderBinReplenishmentView(root);
  } else if (view === 'wave-picking') {
    await renderWavePickingView(root);
  } else if (view === 'mobile-picking') {
    await renderMobilePickingView(root);
  } else if (view === 'marketplace') {
    await renderMarketplaceView(root);
	} else if (view === 'oms') {
		await renderOMSWorkbenchView(root);
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
  } else if (view === 'grn') {
    await renderGRNWorkbenchView(root);
  } else if (view === 'doctype-table') {
    await renderDocTableView(root);
  } else if (view === 'doctype-builder') {
    await renderDocTypeBuilderView(root);
  } else if (view === 'prefix-configs') {
    await renderPrefixConfigsView(root);
  } else if (view === 'approval-rules') {
    await renderApprovalRulesView(root);
  } else if (view === 'dynamic-labels') {
    renderDynamicLabelsView(root);
  } else if (view === 'extension-hooks') {
    await renderExtensionHooksView(root);
  } else if (view === 'extension-hook-log') {
    await renderExtensionHookLogView(root);
  } else if (view === 'audit-logs') {
    await renderLogHubView(root);
  } else if (view === 'system-status') {
    await renderSystemStatusView(root);
  } else if (view === 'configuration') {
    await renderConfigurationView(root);
  } else if (view === 'tenant-entitlements') {
    await renderTenantEntitlementsView(root);
  } else if (view === 'tenant-usage') {
    await renderTenantUsageView(root);
  } else if (view === 'profile') {
    await renderProfileView(root);
  } else if (view === 'help') {
    await renderHelpView(root);
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
  // Chromium can cache a stale containing-block rect for `position: sticky`
  // content (e.g. a table's frozen header row) when it's inserted into a
  // scroll container in the very same reflow that establishes that
  // container's own scrollable overflow - the sticky element then just
  // scrolls away with the page instead of freezing. One forced layout read
  // after paint fixes it; done once here so every view's tables get it for
  // free instead of patching each table-rendering function individually.
  requestAnimationFrame(() => { void root.offsetHeight; });
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

  // 32.5: two-factor recovery. Only rendered for accounts that actually have
  // MFA enrolled - for everyone else there is nothing here to manage, and an
  // empty panel would just be noise on a screen most users open to change a
  // password.
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
  warning.textContent = 'These are shown only once. Store them somewhere safe and away from your phone. Any codes you had before have stopped working.';
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
  intro.textContent = 'Add a new account in your authenticator app (Google Authenticator, Authy, etc) using this manual-entry code. Your current device keeps working until you confirm the new one.';
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
      <h3 class="card-title" style="margin-bottom: 8px;">Two-Factor Recovery</h3>
      <p style="font-size: 13px; color: var(--text-muted); margin: 0;">Could not load your recovery-code status.</p>`;
    return;
  }
  const info = await res.json();
  const remaining = Number(info.remaining || 0);
  const perSet = Number(info.issued_per_set || 10);

  // The status line is the whole point of the panel: "you have N ways back in
  // if your phone dies". Zero is called out in danger colours because that is
  // the state the Stage 32.5 lockout report was actually written about.
  let statusBadge, statusNote;
  if (remaining === 0) {
    statusBadge = '<span class="badge badge-danger">No recovery codes</span>';
    statusNote = 'If you lose your phone you will be locked out and an administrator will have to reset your two-factor setup. Generate a set now.';
  } else if (remaining <= 2) {
    statusBadge = `<span class="badge badge-warning">${remaining} of ${perSet} left</span>`;
    statusNote = 'You are nearly out. Generate a fresh set before the last one is used.';
  } else {
    statusBadge = `<span class="badge badge-success">${remaining} of ${perSet} left</span>`;
    statusNote = 'Each code signs you in once if your authenticator is unavailable.';
  }

  const pendingNote = info.reenroll_in_progress
    ? `<p style="font-size: 13px; color: var(--warning-strong); margin: 0 0 12px;">
         A device change is part-finished. Your current authenticator still works &mdash; start it again to pick up where you left off, or cancel it.</p>`
    : '';

  panel.innerHTML = `
    <h3 class="card-title" style="margin-bottom: 12px;">Two-Factor Recovery</h3>
    <div style="margin-bottom: 8px;">${statusBadge}</div>
    <p style="font-size: 13px; color: var(--text-muted); margin: 0 0 12px;">${statusNote}</p>
    ${pendingNote}
    <div style="display: flex; flex-wrap: wrap; gap: 8px;">
      <button type="button" class="btn btn-secondary" id="mfa-regen-btn">Generate new recovery codes</button>
      <button type="button" class="btn btn-secondary" id="mfa-newdevice-btn">Set up a new authenticator device</button>
      ${info.reenroll_in_progress ? '<button type="button" class="btn btn-secondary" id="mfa-cancel-reenroll-btn">Cancel device change</button>' : ''}
    </div>
  `;

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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No users yet. Use <b>Create User</b> above to add the first one.</td></tr>`
    : users.map(u => `
        <tr>
          <td style="font-weight:600;">${u.username}</td>
          <td>${u.email || ''}</td>
          <td>${u.role}</td>
          <td>${u.location_code || 'HO'}</td>
          <td><span class="badge ${u.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${u.status}</span></td>
          <td>
            <button class="action-btn" onclick="setUserLocation('${u.id}', '${u.location_code || 'HO'}')">Set Location</button>
            <button class="action-btn" onclick="resetUserMFA('${u.id}', '${u.username}')">Reset 2FA</button>
            ${u.role === 'Supplier' ? `<button class="action-btn" onclick="setUserSupplier('${u.id}', '${u.supplier_code || ''}')">Link Vendor</button>` : ''}
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
  attachLinkTypeahead(document.getElementById('user-location'), 'Location');
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

// 32.5: the admin-side escape hatch for a colleague who lost both their phone
// and their recovery codes. Clears the enrollment rather than disabling MFA,
// so their next login is forced through setup on a new device - previously
// the only route was SSH to the server plus a hand-written UPDATE.
window.resetUserMFA = async function(id, username) {
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
};

// 26.4.10: links a Supplier login to the Vendor it speaks for. Until this is
// set the account can sign in but every screen refuses it - deliberately, an
// unscoped supplier session is the one thing the row-level scoping exists to
// prevent - so this is the step that finishes creating a supplier account.
window.setUserSupplier = async function(id, currentCode) {
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
      <p class="page-subtitle">What each role can see and do, per record type. Super Admin can always do everything; this only governs the other roles.</p>
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No grants configured yet. Pick a role and a record type above, then <b>Save Grant</b> &mdash; roles other than Super Admin see only what a grant allows.</td></tr>`
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
        <!-- Stage 41: the cashier searches and sees the location's NAME;
             #pos-location stays the code, because every downstream call
             (session, availability, cart number, receipt) keys off it. -->
        <label class="form-label" for="pos-location-display">Location</label>
        <input type="text" id="pos-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
        <input type="hidden" id="pos-location" value="${posLocation}">
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
    <!-- Stage 30.7: offers configured in the ERP (Offer master) are evaluated
         server-side and shown here. This block is display-only - the discount
         that reaches the sale is always recomputed at checkout. -->
    <div id="pos-offers-row" class="hidden" style="margin-top: 16px; padding: 12px 14px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-color);"></div>
    <div style="display: flex; justify-content: flex-end; align-items: center; gap: 24px; margin-top: 20px; padding-top: 20px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="max-width: 160px; margin-bottom: 0;">
        <label class="form-label" for="pos-coupon-code">Coupon code</label>
        <input type="text" id="pos-coupon-code" class="form-input" placeholder="Optional" autocomplete="off">
      </div>
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
      <div id="pos-loyalty-discount-row" class="hidden" style="font-size: 13px; font-weight: 600; color: var(--text-muted);"></div>
      <div style="font-size: 20px; font-weight: 700;">Total: <span id="pos-cart-total">0.00</span></div>
      <button class="btn btn-primary" id="pos-checkout-btn">Complete Sale</button>
    </div>
  `;
  container.appendChild(panel);

  attachCodeNamePicker(
    document.getElementById('pos-location-display'),
    document.getElementById('pos-location'),
    'Location');
  attachLinkTypeahead(document.getElementById('pos-customer'), 'Customer');
  attachLinkTypeahead(document.getElementById('pos-sku-input'), 'Item');

  // Still the hidden input's change event: attachCodeNamePicker dispatches it
  // there precisely so this listener (and every other reader of
  // #pos-location) needed no change.
  document.getElementById('pos-location').addEventListener('change', (e) => {
    posLocation = e.target.value.trim();
    refreshPOSSessionStatus();
  });
  // Stage 30.7: re-evaluate offers when the coupon code or the customer
  // changes - a tier-restricted or coupon-gated offer depends on both.
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.addEventListener('change', () => refreshPOSOffers());
  document.getElementById('pos-customer').addEventListener('change', () => refreshPOSOffers());
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
        <label class="form-label" for="pos-return-location-display">Return Location</label>
        <input type="text" id="pos-return-location-display" class="form-input" placeholder="Search by store name or code" autocomplete="off">
        <input type="hidden" id="pos-return-location" value="${posLocation}">
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

  attachLinkTypeahead(document.getElementById('pos-return-sku-input'), 'Item');
  attachCodeNamePicker(
    document.getElementById('pos-return-location-display'),
    document.getElementById('pos-return-location'),
    'Location');
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
    statusEl.textContent = 'Choose a location to check for an open cashier session.';
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
  // Stage 41: name the location the way the cashier does. The display box
  // already holds the resolved name; the code is the fallback for the moment
  // before it resolves, and for a code typed but not yet matched.
  const shown = (document.getElementById('pos-location-display') || {}).value || posLocation;
  if (posOpenSessionId) {
    statusEl.textContent = `Session open at ${shown}.`;
    openBtn.classList.add('hidden');
    closeBtn.classList.remove('hidden');
  } else {
    statusEl.textContent = `No open session at ${shown} - open one before selling.`;
    openBtn.classList.remove('hidden');
    closeBtn.classList.add('hidden');
  }
}

async function openPOSSessionFlow() {
  if (!posLocation) {
    await showCustomAlert('Choose a location first.', 'Location Required');
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
  let msg = `Session closed. Expected: ${data.expected_cash.toFixed(2)}, Counted: ${data.counted_cash.toFixed(2)}, Variance: ${data.variance.toFixed(2)}`;
  // 24.36: the server diffed this session's last offline-queue heartbeat
  // against what actually synced - a non-empty list here means at least
  // one sale was queued (and beaconed) but never arrived, logged to
  // POSOfflineQueueGap for HR/Admin and Store Manager to review. Shown to
  // the closing cashier too, not just the reviewers - deliberate, so
  // there's no incentive to stay quiet about it.
  if (data.offline_queue_gap && data.offline_queue_gap.length > 0) {
    msg += `\n\nWarning: ${data.offline_queue_gap.length} offline sale(s) were queued but never synced (${data.offline_queue_gap.join(', ')}). This has been flagged for manager review.`;
  }
  await showCustomAlert(msg, 'Session Closed');
  await refreshPOSSessionStatus();
}

async function addSKUToPOSCart() {
  const skuInput = document.getElementById('pos-sku-input');
  const errorEl = document.getElementById('pos-scan-error');
  const sku = skuInput.value.trim();
  errorEl.classList.add('hidden');

  if (!posLocation) {
    errorEl.textContent = 'Choose a location before adding items.';
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

  // Stage 30.2.5: a redemption on this cart is shown as its own line and
  // subtracted from the total the cashier reads out, instead of being a number
  // the cashier was told to type into a line's Sale Price by hand.
  const redeemRow = document.getElementById('pos-loyalty-discount-row');
  if (redeemRow) {
    if (posRedeemPoints > 0) {
      redeemRow.textContent = `Loyalty: -${posRedeemPoints.toFixed(2)} (${posRedeemPoints} pt)`;
      redeemRow.classList.remove('hidden');
    } else {
      redeemRow.textContent = '';
      redeemRow.classList.add('hidden');
    }
  }
  if (posRedeemPoints > total) {
    // The cart shrank below what was pledged - keep the two consistent
    // rather than showing a negative total.
    posRedeemPoints = Math.floor(total);
  }

  document.getElementById('pos-cart-total').textContent = Math.max(0, total - posRedeemPoints - posOfferDiscount).toFixed(2);
  refreshPOSOffers();
}

// --- POS offers (Stage 30.7) ---------------------------------------------
// Offers are configured in the ERP as Offer documents and evaluated by the
// server (POST /api/v1/pos/offers/preview). Nothing about which offers exist
// or how they price lives in this file - the cashier sees whatever the ERP
// currently says, so an offer switched on in the back office applies at the
// till immediately, with no POS reload.
//
// This preview is display-only. Checkout re-evaluates the same rules
// server-side, so what the customer is charged never depends on this call
// having run, or on anything the browser could tamper with.
let posOfferDiscount = 0;
let posAppliedOffers = [];
let posOfferPreviewSeq = 0;

function currentPOSCouponCodes() {
  const el = document.getElementById('pos-coupon-code');
  if (!el) return [];
  // Accept several codes separated by comma/space, so a cashier can key in
  // more than one without a second field.
  return el.value.split(/[,\s]+/).map(c => c.trim()).filter(Boolean);
}

async function refreshPOSOffers() {
  const row = document.getElementById('pos-offers-row');
  if (!row) return;
  if (!posCart.length) {
    posOfferDiscount = 0;
    posAppliedOffers = [];
    row.classList.add('hidden');
    return;
  }

  // Guard against out-of-order responses: only the newest request may write
  // back, so a slow earlier preview can't overwrite a newer cart's result.
  const seq = ++posOfferPreviewSeq;
  const customerId = (document.getElementById('pos-customer') || {}).value || '';
  let res;
  try {
    res = await apiFetch('/api/v1/pos/offers/preview', {
      method: 'POST',
      body: JSON.stringify({
        customer_id: customerId.trim(),
        coupon_codes: currentPOSCouponCodes(),
        items: posCart.map(l => ({ sku: l.sku, qty: l.qty, sale_price: l.salePrice }))
      })
    });
  } catch (e) {
    return; // offline or unreachable - the cart simply shows no offers
  }
  if (seq !== posOfferPreviewSeq) return;
  if (!res || !res.ok) return;

  const data = await res.json();
  if (seq !== posOfferPreviewSeq) return;

  posAppliedOffers = Array.isArray(data.applied) ? data.applied : [];
  const newDiscount = Number(data.total_discount) || 0;
  const unmatched = Array.isArray(data.unmatched_codes) ? data.unmatched_codes : [];

  if (!posAppliedOffers.length && !unmatched.length) {
    posOfferDiscount = 0;
    row.classList.add('hidden');
    updatePOSTotalForOffers();
    return;
  }

  row.classList.remove('hidden');
  row.innerHTML = `
    ${posAppliedOffers.length ? `
      <div style="font-size:13px; font-weight:600; margin-bottom:6px;">Offers applied</div>
      <ul style="margin:0 0 4px; padding-left:18px; font-size:13px;">
        ${posAppliedOffers.map(o => `<li>${cfgEsc(o.name)} <span style="color:var(--text-muted);">- ${cfgEsc(o.description || '')}</span> <strong>-${Number(o.discount).toFixed(2)}</strong></li>`).join('')}
      </ul>
      <div style="font-size:13px; font-weight:600;">Total offer discount: -${newDiscount.toFixed(2)}</div>` : ''}
    ${unmatched.length ? `<div style="font-size:12.5px; color:var(--warning-color, #b26a00); margin-top:${posAppliedOffers.length ? '6px' : '0'};">
        Coupon code${unmatched.length > 1 ? 's' : ''} ${unmatched.map(cfgEsc).join(', ')} did not match any live offer for this cart.
      </div>` : ''}
  `;

  posOfferDiscount = newDiscount;
  updatePOSTotalForOffers();
}

// Rewrites just the total after an async offer preview lands, without
// re-entering renderPOSCartTable (which would trigger another preview).
function updatePOSTotalForOffers() {
  const totalEl = document.getElementById('pos-cart-total');
  if (!totalEl) return;
  const gross = posCart.reduce((sum, l) => sum + l.salePrice * l.qty, 0);
  totalEl.textContent = Math.max(0, gross - posRedeemPoints - posOfferDiscount).toFixed(2);
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
  // 24.36: beacon the queue's new state to the server on every mutation
  // (push, or drained by a sync), not just on a timer - the single choke
  // point every queue change already runs through, so this needs no
  // separate call site of its own. Best-effort/fire-and-forget: see
  // sendOfflineQueueHeartbeat's own header comment for why this can't be
  // made fully reliable.
  sendOfflineQueueHeartbeat();
}

function queueOfflinePOSCart(payload) {
  const queue = getOfflineQueue();
  queue.push({ cartNumber: payload.cart_number, location: payload.location, payload, queuedAt: new Date().toISOString() });
  saveOfflineQueue(queue);
}

// 24.36: best-effort beacon of the currently-queued offline cart_numbers
// against the cashier's open session, so a gap between what was queued and
// what actually synced (e.g. a cashier clears browser storage before
// reconnecting) leaves a server-side trace instead of vanishing without
// one - see engines.RecordOfflineHeartbeat/detectOfflineQueueGap. Can't use
// navigator.sendBeacon (no way to attach the Authorization/X-Tenant-ID
// headers this endpoint requires); fetch's keepalive flag is the
// equivalent that still works during page unload for a payload this small.
// A network failure here is expected and silent - it just means this
// particular checkpoint never reaches the server, same as if a heartbeat
// had never fired at all; it does not affect the cashier's ability to keep
// selling or syncing.
function sendOfflineQueueHeartbeat() {
  if (!posOpenSessionId || !posLocation) return;
  const token = localStorage.getItem('erp_token');
  if (!token) return;
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const cartNumbers = getOfflineQueue().map(e => e.cartNumber);
  fetch('/api/v1/pos/offline-heartbeat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}`, 'X-Tenant-ID': tenantID },
    body: JSON.stringify({ session_id: posOpenSessionId, location: posLocation, cart_numbers: cartNumbers }),
    keepalive: true
  }).catch(() => {});
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
//
// 24.36: 'visibilitychange'/'pagehide' additionally re-beacon the queue's
// current state whenever the tab is about to be hidden or closed - the
// single highest-value moment to catch, since it's the last chance to get
// a checkpoint out before a cashier could clear storage or walk away from
// an unattended tab. The 30s poll already re-heartbeats implicitly via
// saveOfflineQueue if it triggers a sync, but a page that's simply idle
// (queue non-empty, nothing changing) wouldn't otherwise re-beacon between
// the initial queue and whenever it's next touched.
function setupOfflineSync() {
  window.addEventListener('online', trySyncOfflineQueue);
  setInterval(() => {
    if (getOfflineQueue().length > 0) trySyncOfflineQueue();
  }, 30000);
  const reheartbeat = () => { if (getOfflineQueue().length > 0) sendOfflineQueueHeartbeat(); };
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') reheartbeat(); });
  window.addEventListener('pagehide', reheartbeat);
}

async function submitPOSCheckout() {
  const errorEl = document.getElementById('pos-scan-error');
  errorEl.classList.add('hidden');

  if (!posLocation) {
    errorEl.textContent = 'Choose a location before completing the sale.';
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
    const customerId = document.getElementById('pos-customer').value.trim();
    // Stage 30.2.5: the redemption travels with the sale and is burned
    // server-side only if the sale completes.
    const redeemPoints = customerId ? posRedeemPoints : 0;
    const res = await checkoutOnlineOrQueue({
      cart_number: cartNumber,
      location: posLocation,
      payment_mode: paymentMode,
      customer_id: customerId,
      discount_pct: discountPct,
      redeem_points: redeemPoints,
      coupon_codes: currentPOSCouponCodes(),
      items: cartItems
    });
    if (res === 'queued') {
      posCart = [];
      clearPOSRedemption();
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
      clearPOSRedemption();
      renderPOSCartTable();
      await showCustomAlert(data.message || 'This sale requires manager approval before it completes.', 'Approval Required');
      return;
    }

    posCart = [];
    clearPOSRedemption();
    renderPOSCartTable();
    // amount_due is the sale total less any loyalty points spent on it
    // (Stage 30.2.5); it equals sale_total when no points were redeemed.
    const amountDue = data.amount_due !== undefined ? data.amount_due : data.sale_total;
    const loyaltyNote = data.loyalty_discount ? ` (${data.loyalty_points_redeemed} loyalty point(s) applied, -${data.loyalty_discount})` : '';
    const printReceipt = await showCustomConfirm(
      `Sale ${data.cart_number} completed. Collect: ${amountDue}${loyaltyNote}. Print receipt?`, 'Sale Complete');
    if (printReceipt) {
      // 31.1.9: silent path first - the server rebuilds the receipt from the
      // Paid POSCart, so a thermal till printer gets ESC-POS with no browser
      // dialog in the cashier's way. quiet: true because a shop with no
      // Receipt printer configured is the normal case, not an error, and
      // must simply fall through to the print sheet below.
      if (!await qzTryPrint('Receipt', { documentRef: data.cart_number, quiet: true })) {
        printPOSReceipt(data.cart_number, posLocation, paymentMode, cartItems,
          data.sale_total, data.loyalty_discount || 0, data.offer_discount || 0);
      }
    }
  } finally {
    checkoutBtn.disabled = false;
  }
}

// Stage 20.14: reuses the sticker-print-area's hidden-until-printing @media
// print pattern (styles.css) rather than a new PDF/print dependency. This is
// the fallback behind the 31.1.9 QZ path - what prints when QZ Tray is not
// running, or when no printer is set as Default For Receipt.
//
// 31.1.9 fix: offerDiscount was missing here. Checkout returns amount_due as
// sale_total - loyalty_discount - offer_discount, so a sale with a Stage 30.7
// offer applied printed a receipt whose total was higher than the cash
// actually collected. Kept as a defaulted parameter so the shape of the call
// is unchanged for anything that does not pass it.
function printPOSReceipt(cartNumber, location, paymentMode, items, saleTotal, loyaltyDiscount = 0, offerDiscount = 0) {
  const area = document.getElementById('receipt-print-area');
  if (!area) return;
  const lines = items.map(it => `
    <div class="receipt-line"><span>${it.sku} x${it.qty}</span><span>${(it.qty * it.sale_price).toFixed(2)}</span></div>
  `).join('');
  // Stage 30.2.5: points spent on the sale are shown on the receipt as their
  // own line, so the customer can see what their points paid for and the
  // printed total matches what was actually collected.
  const subtotalLine = (loyaltyDiscount > 0 || offerDiscount > 0) ? `
      <div class="receipt-line"><span>Subtotal</span><span>${Number(saleTotal).toFixed(2)}</span></div>
  ` : '';
  const offerLine = offerDiscount > 0 ? `
      <div class="receipt-line"><span>Offer discount</span><span>-${Number(offerDiscount).toFixed(2)}</span></div>
  ` : '';
  const loyaltyLine = loyaltyDiscount > 0 ? `
      <div class="receipt-line"><span>Loyalty points applied</span><span>-${Number(loyaltyDiscount).toFixed(2)}</span></div>
  ` : '';
  const amountDue = Number(saleTotal) - Number(loyaltyDiscount) - Number(offerDiscount);
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
      ${subtotalLine}
      ${offerLine}
      ${loyaltyLine}
      <div class="receipt-line receipt-total"><span>Total (${paymentMode})</span><span>${amountDue.toFixed(2)}</span></div>
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

// Stage 30.2.5: the points a cashier has added to THIS cart, not yet burned.
// The burn happens server-side when the sale completes. Cleared whenever the
// cart is (completed, queued offline, or abandoned by leaving the screen), so
// points can never be spent on a sale that didn't happen.
let posRedeemPoints = 0;

function clearPOSRedemption() {
  posRedeemPoints = 0;
  // Stage 30.7: a completed/queued sale must not leave its offers or coupon
  // sitting on the next customer's cart.
  posOfferDiscount = 0;
  posAppliedOffers = [];
  const couponEl = document.getElementById('pos-coupon-code');
  if (couponEl) couponEl.value = '';
  const offersRow = document.getElementById('pos-offers-row');
  if (offersRow) { offersRow.classList.add('hidden'); offersRow.innerHTML = ''; }
}

async function redeemPOSLoyaltyPoints() {
  const infoEl = document.getElementById('pos-loyalty-info');
  const customerId = document.getElementById('pos-customer').value.trim();
  if (!customerId) {
    infoEl.textContent = 'Enter a customer code first.';
    return;
  }
  if (posCart.length === 0) {
    infoEl.textContent = 'Add items to the cart before redeeming points - the discount is applied to this sale.';
    return;
  }

  // Check the balance first so the cashier is told "you have N" rather than
  // finding out at the till.
  const balRes = await apiFetch(`/api/v1/loyalty/ledger?customer_id=${encodeURIComponent(customerId)}`);
  if (!balRes) return;
  if (!balRes.ok) {
    infoEl.textContent = 'Failed to look up loyalty balance.';
    return;
  }
  const balance = (await balRes.json()).balance || 0;
  if (balance <= 0) {
    infoEl.textContent = `${customerId} has no loyalty points to redeem.`;
    return;
  }

  const cartTotal = posCart.reduce((sum, line) => sum + line.qty * line.salePrice, 0);
  const pointsStr = await showCustomPrompt(`How many points to redeem? ${customerId} has ${balance}. 1 point = 1 off this sale.`);
  const points = parseInt(pointsStr, 10);
  if (!points || points <= 0) return;
  if (points > balance) {
    infoEl.textContent = `${customerId} only has ${balance} point(s).`;
    return;
  }
  if (points > cartTotal) {
    infoEl.textContent = `This sale is only ${cartTotal.toFixed(2)} - redeem at most ${Math.floor(cartTotal)} point(s).`;
    return;
  }

  // Nothing is burned here. The points ride on the cart and are spent by the
  // server only if the sale completes.
  posRedeemPoints = points;
  renderPOSCartTable();
  infoEl.textContent = `${points} point(s) will be applied to this sale. They are only deducted once the sale completes.`;
}

// Finance / GL screen - read-only trial balance view against the already-
// working GET /api/v1/finance/trial-balance API (Stage 13.5). Same story as
// the POS screen: the double-entry posting engine and API already work and
// are tested, there was just no screen to see them.
let currentFinanceTab = 'trial-balance';
const FINANCE_TABS = [
  { id: 'trial-balance', label: 'Trial Balance' },
  { id: 'chart-of-accounts', label: 'Chart of Accounts' },
  { id: 'periods', label: 'Accounting Periods' }
];

// Local (not UTC) yyyy-mm-dd. Deliberately not toISOString().slice(0,10):
// that converts to UTC first, so for an IST user everything before 05:30
// local would default a date picker to *yesterday*.
function todayISO() {
  const d = new Date();
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// Stage 29.7.4: the trial balance is now an as-at-a-date statement (the API
// requires as_of), so the screen owns a date and defaults it to today. Kept
// at module scope so switching tabs and back doesn't silently reset a date
// the user deliberately chose.
let financeTrialBalanceAsOf = todayISO();

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

  if (currentFinanceTab === 'chart-of-accounts') {
    await renderChartOfAccountsPanel(container);
    return;
  }

  // As-of picker (29.7.4). Re-renders the whole view on change rather than
  // patching the table in place - same approach the tab bar above uses.
  const controls = document.createElement('div');
  controls.className = 'form-group';
  controls.style.cssText = 'display:flex; align-items:flex-end; gap:12px; margin-bottom:16px;';
  controls.innerHTML = `
    <div>
      <label class="form-label" for="tb-as-of">As Of Date<span class="required">*</span></label>
      <input type="date" id="tb-as-of" class="form-input" style="width:180px;" value="${financeTrialBalanceAsOf}">
    </div>
    <span style="color:var(--text-muted); font-size:12px; padding-bottom:10px;">
      Includes every GL posting up to and including this date.
    </span>
  `;
  container.appendChild(controls);
  document.getElementById('tb-as-of').addEventListener('change', (e) => {
    if (!e.target.value) return; // an emptied picker would 400; keep the last good date
    financeTrialBalanceAsOf = e.target.value;
    renderView('finance');
  });

  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(financeTrialBalanceAsOf)}`);
  if (!res) return;

  if (!res.ok) {
    await showApiError(res, 'Failed to load trial balance.');
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
    <div class="stat-card">
      <span class="stat-label">As Of</span>
      <span class="stat-val">${data.as_of || financeTrialBalanceAsOf}</span>
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
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No GL postings on or before this date. Postings are created automatically when a sale, receipt or invoice is posted &mdash; try a later As Of date.</td></tr>`;
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

// Chart of Accounts (Stage 29.9): reference list of the master gl_accounts
// records themselves (code/name/type) rather than a balance report - reuses
// the trial-balance endpoint since that query already LEFT JOINs every
// account row regardless of postings, so no new backend endpoint is needed.
async function renderChartOfAccountsPanel(container) {
  // Only the account rows (code/name/type) are used here, never the balances,
  // so the as_of the endpoint now requires (29.7.4) is just "today" - the
  // gl_accounts side of that LEFT JOIN is unaffected by it either way.
  const res = await apiFetch(`/api/v1/finance/trial-balance?as_of=${encodeURIComponent(todayISO())}`);
  if (!res) return;
  if (!res.ok) {
    const errPanel = document.createElement('div');
    errPanel.className = 'table-panel';
    errPanel.style.padding = '24px';
    errPanel.textContent = 'Failed to load chart of accounts.';
    container.appendChild(errPanel);
    return;
  }
  const data = await res.json();
  const accounts = data.balances || [];

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr>
          <th>Account Code</th>
          <th>Account Name</th>
          <th>Type</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (accounts.length === 0) {
    html += `<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No GL accounts configured yet. The chart of accounts is seeded during installation &mdash; ask your administrator to apply any pending database migrations.</td></tr>`;
  }
  accounts.forEach(a => {
    html += `
      <tr>
        <td style="font-family: monospace;">${a.account_code}</td>
        <td style="font-weight:600;">${a.account_name}</td>
        <td>${a.account_type}</td>
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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No accounting periods yet. Use <b>Create Period</b> above to open your first one; the close checklist needs a period to run against.</td></tr>`
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
      <h1 class="page-title">Vendor Invoice</h1>
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
          ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No vendor invoices yet. Use <b>+ New Vendor Invoice</b> above &mdash; have the Purchase Order and Goods Receipt it should 3-way match against to hand.</td></tr>`
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
    // Stage 26.3.5: engines.PayVendorInvoice's override path (Stage 24.11 -
    // pay a MismatchHold invoice anyway with a business-justified reason,
    // routed to approval) already existed server-side but had no UI action
    // calling it - only Match did. Draft has nothing to override yet
    // (3-way match hasn't run), so this only shows for MismatchHold.
    return `<button class="action-btn" onclick="matchVendorInvoice('${id}')">Match</button>${v.status === 'MismatchHold' ? `<button class="action-btn" style="margin-left:4px;" onclick="overrideAndPayVendorInvoice('${id}')">Override &amp; Pay</button>` : ''}`;
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

// overrideAndPayVendorInvoice (Stage 26.3.5): the UI action for
// engines.PayVendorInvoice's pre-existing override path - a MismatchHold
// invoice can be paid anyway with a mandatory business reason, routed
// through the approval engine (VendorInvoice's own approval_rules) rather
// than paying immediately, same as VENDOR-0092's own message states.
async function overrideAndPayVendorInvoice(invoiceId) {
  const reason = await showCustomPrompt('This invoice failed 3-way match. Reason to pay anyway (routes to approval):', '', 'Override & Pay');
  if (!reason || !reason.trim()) return;
  const res = await apiFetch('/api/v1/procurement/vendor-invoice/pay', {
    method: 'POST',
    body: JSON.stringify({ invoice_id: invoiceId, override_reason: reason.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit payment override.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(data.status === 'pending_approval' ? 'Override submitted - routed for approval.' : 'Invoice paid.', 'Override & Pay');
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
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No payment proposals yet. Tick one or more Matched vendor invoices above, then <b>Create Proposal from Selected</b>.</td></tr>`
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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No debit notes yet. Fill in the form above and <b>Post</b> to raise one against a vendor.</td></tr>`
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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No credit notes yet. Fill in the form above and <b>Post</b> to raise one against a customer.</td></tr>`
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
      <h1 class="page-title">Sales Invoice</h1>
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
          ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No sales invoices yet. Use <b>+ New Sales Invoice</b> above to bill a customer on credit; POS sales are settled immediately and do not appear here.</td></tr>`
          : invoices.map(v => `
            <tr>
              <td style="font-family: monospace;">${v.invoice_number || v.id}</td>
              <td>${v.customer || ''}</td>
              <td>${(v.total_amount ?? 0).toLocaleString()}</td>
              <td><span class="badge ${STATUS_BADGE[v.status] || 'badge-secondary'}">${v.status}</span></td>
              <td>
                ${v.status === 'Draft' ? `<button class="action-btn" onclick="postSalesInvoiceAction('${v.id}')">Post</button>` : ''}
                ${v.status === 'Approved' ? `<button class="action-btn" onclick="settleSalesInvoiceAction('${v.id}')">Settle</button>` : ''}
                <button class="action-btn" onclick="printSalesInvoice('${v.id}')">Print</button>
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

// printSalesInvoice (Stage 31.1.9). Every status prints, deliberately - a
// Draft invoice is a legitimate proforma to hand a customer. What it must not
// do is look like a posted one, so both this sheet and the server-built
// payload stamp the status on the page; a Draft comes out marked DRAFT.
window.printSalesInvoice = async function(id) {
  if (await qzTryPrint('Invoice', { documentRef: id, quiet: true })) return;

  const res = await apiFetch(`/api/v1/doc/SalesInvoice/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the invoice.');
    return;
  }
  renderInvoicePrintSheet(await res.json());
};

function renderInvoicePrintSheet(invoice) {
  const area = document.getElementById('invoice-print-area');
  if (!area) return;
  const status = invoice.status || '';
  const row = (label, value) => value
    ? `<tr><td class="invoice-key">${label}</td><td>${value}</td></tr>`
    : '';
  const draft = (status !== 'Approved' && status !== 'Paid')
    ? `<div class="invoice-draft">${status.toUpperCase()}</div>`
    : '';
  area.innerHTML = `
    <div class="invoice-sheet">
      <div class="invoice-title">Tax Invoice</div>
      ${draft}
      <hr>
      <table>
        ${row('Invoice', invoice.invoice_number || invoice.id)}
        ${row('Customer', invoice.customer)}
        ${row('Location', invoice.location)}
        ${row('Status', status)}
      </table>
      <div class="invoice-total">Total: ${Number(invoice.total_amount || 0).toFixed(2)}</div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
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
    html += `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No fulfillment tasks routed to your location. Tasks appear automatically when a Sales Order is released under <b>Order Management</b>.</td></tr>`;
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
        <button class="action-btn" onclick="viewPickList('${id}')">View Pick List</button>
      `;
    case 'Picking':
      return `
        <button class="action-btn" onclick="transitionFulfillmentTask('${id}', 'Packed')">Mark Packed</button>
        <button class="action-btn action-btn-danger" onclick="transitionFulfillmentTask('${id}', 'Rejected')">Reject</button>
        <button class="action-btn" onclick="viewPickList('${id}')">View Pick List</button>
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

// viewPickList (Stage 26.3.4) shows GenerateBinPickList's bin-grouped,
// walk-route-sorted result for one FulfillmentTask in a lightweight
// read-only modal, reusing the same .modal-overlay/.modal-container
// primitives as viewTaxonomyHistory instead of introducing a new one.
window.viewPickList = async function(taskId) {
  const res = await apiFetch(`/api/v1/wms/pick-list?task_id=${encodeURIComponent(taskId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the pick list for this task.');
    return;
  }
  const lines = await res.json();

  document.getElementById('pick-list-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pick-list-modal';
  const rows = (!lines || lines.length === 0)
    ? `<tr><td colspan="6" class="text-center text-muted">No bin-level pick lines for this task &mdash; it will be picked from general stock instead. Continue as normal.</td></tr>`
    : lines.map(l => {
        const short = l.shortfall > 0
          ? `<span class="badge badge-danger">Short ${l.shortfall}</span>`
          : '';
        return `<tr><td>${l.sku || ''}</td><td>${l.bin_code || ''}</td><td>${l.zone || ''}</td><td>${l.aisle || ''}</td><td>${l.rack || ''}</td><td>${l.pick_qty || 0} ${short}</td></tr>`;
      }).join('');
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Pick List: ${taskId}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><div class="table-wrapper"><table><thead><tr><th>SKU</th><th>Bin</th><th>Zone</th><th>Aisle</th><th>Rack</th><th>Pick Qty</th></tr></thead><tbody>${rows}</tbody></table></div></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
};

// WMS operations screens (Stage 26.3.4) - engines/wms.go's putaway, bin
// condition transitions, and cycle-count reconciliation (Stage 20 Track B.2)
// have been real, routed, working backend endpoints since Stage 20 with zero
// frontend anywhere. These three screens are pure UI on top of that existing
// backend - no new engine code, doctype, or migration needed.

async function renderPutawayView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Putaway</h1>
      <p class="page-subtitle">Place accepted stock into a bin. Refuses more than the location's unassigned on-hand quantity.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-bin">Bin Code</label>
        <input type="text" id="putaway-bin" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-sku">SKU</label>
        <input type="text" id="putaway-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="putaway-qty">Qty</label>
        <input type="number" id="putaway-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-primary" id="putaway-submit-btn" type="button">Put Away</button>
    </div>
    <div id="putaway-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);

  document.getElementById('putaway-submit-btn').addEventListener('click', submitPutaway);
  attachLinkTypeahead(document.getElementById('putaway-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('putaway-sku'), 'Item');

  // Stage 26.5.3: cross-dock/flow-through putaway - an alternative to
  // shelving when a transfer/sale is already waiting on this exact SKU at
  // this location, skipping bin placement in favor of a staging bin.
  const xdockPanel = document.createElement('div');
  xdockPanel.className = 'table-panel';
  xdockPanel.style.padding = '24px';
  xdockPanel.style.marginTop = '24px';
  xdockPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Cross-Dock Staging</h2>
    <p style="color: var(--text-muted); margin-bottom: 12px;">Check whether an open transfer/sale is already waiting on a SKU before shelving it - if so, stage it for immediate outbound instead.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-sku">SKU</label>
        <input type="text" id="xdock-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-location">Location</label>
        <input type="text" id="xdock-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="xdock-qty">Qty on hand to place</label>
        <input type="number" id="xdock-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-outline" id="xdock-check-btn" type="button">Check Opportunity</button>
      <button class="btn btn-primary" id="xdock-stage-btn" type="button">Stage for Cross-Dock</button>
    </div>
    <div id="xdock-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
    <div id="xdock-form-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(xdockPanel);
  document.getElementById('xdock-check-btn').addEventListener('click', checkCrossDockOpportunity);
  document.getElementById('xdock-stage-btn').addEventListener('click', submitCrossDockPutaway);
  attachLinkTypeahead(document.getElementById('xdock-sku'), 'Item');
  attachLinkTypeahead(document.getElementById('xdock-location'), 'Location');
}

async function checkCrossDockOpportunity() {
  const resultEl = document.getElementById('xdock-result');
  const sku = document.getElementById('xdock-sku').value.trim();
  const location = document.getElementById('xdock-location').value.trim();
  if (!sku || !location) { resultEl.textContent = 'Enter a SKU and Location first.'; return; }
  const res = await apiFetch('/api/v1/wms/cross-dock/check', {
    method: 'POST',
    body: JSON.stringify({ sku, location_code: location })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to check cross-dock opportunity.', 'Check Failed'); return; }
  const data = await res.json();
  if (data.matched_qty > 0) {
    resultEl.innerHTML = `<span class="badge badge-success">Matched ${data.matched_qty} unit(s)</span> across ${data.opportunities.length} open order(s) - eligible for cross-dock.`;
  } else {
    resultEl.textContent = 'No open transfer/sale is waiting on this SKU here - use ordinary Putaway above instead.';
  }
}

async function submitCrossDockPutaway() {
  const errorEl = document.getElementById('xdock-form-error');
  errorEl.classList.add('hidden');
  const resultEl = document.getElementById('xdock-result');
  const sku = document.getElementById('xdock-sku').value.trim();
  const location = document.getElementById('xdock-location').value.trim();
  const qty = parseInt(document.getElementById('xdock-qty').value, 10);
  if (!sku || !location || !qty || qty <= 0) {
    errorEl.textContent = 'SKU, Location, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/cross-dock/putaway', {
    method: 'POST',
    body: JSON.stringify({ sku, location_code: location, qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to stage for cross-dock.', 'Cross-Dock Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Staged ${data.staged} unit(s)</span> for cross-dock.`;
}

async function submitPutaway() {
  const errorEl = document.getElementById('putaway-form-error');
  errorEl.classList.add('hidden');

  const binCode = document.getElementById('putaway-bin').value.trim();
  const sku = document.getElementById('putaway-sku').value.trim();
  const qty = parseInt(document.getElementById('putaway-qty').value, 10);

  if (!binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/putaway', {
    method: 'POST',
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to put away stock.', 'Putaway Failed');
    return;
  }
  await showCustomAlert(`Put away ${qty} x ${sku} into bin ${binCode}.`, 'Putaway Complete');
  document.getElementById('putaway-qty').value = 1;
}

const BIN_STOCK_CONDITIONS = ['Good', 'Damaged', 'QC-Hold', 'RTV'];

async function renderBinConditionsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bin Conditions</h1>
      <p class="page-subtitle">Move bin stock between Good, Damaged, QC-Hold, and RTV. Moving out of Good makes it unsellable; moving into Good makes it sellable again.</p>
    </div>
  `;
  container.appendChild(header);

  const options = BIN_STOCK_CONDITIONS.map(c => `<option value="${c}">${c}</option>`).join('');
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-bin">Bin Code</label>
        <input type="text" id="bincond-bin" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-sku">SKU</label>
        <input type="text" id="bincond-sku" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-qty">Qty</label>
        <input type="number" id="bincond-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-from">From Condition</label>
        <select id="bincond-from" class="form-input" style="width: 130px;">${options}</select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="bincond-to">To Condition</label>
        <select id="bincond-to" class="form-input" style="width: 130px;">${options}</select>
      </div>
      <button class="btn btn-primary" id="bincond-submit-btn" type="button">Move</button>
    </div>
    <div id="bincond-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(panel);
  document.getElementById('bincond-to').value = 'Damaged';

  document.getElementById('bincond-submit-btn').addEventListener('click', submitBinConditionTransition);
  attachLinkTypeahead(document.getElementById('bincond-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('bincond-sku'), 'Item');
}

async function submitBinConditionTransition() {
  const errorEl = document.getElementById('bincond-form-error');
  errorEl.classList.add('hidden');

  const binCode = document.getElementById('bincond-bin').value.trim();
  const sku = document.getElementById('bincond-sku').value.trim();
  const qty = parseInt(document.getElementById('bincond-qty').value, 10);
  const fromCondition = document.getElementById('bincond-from').value;
  const toCondition = document.getElementById('bincond-to').value;

  if (!binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (fromCondition === toCondition) {
    errorEl.textContent = 'From Condition and To Condition must differ.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/condition-transition', {
    method: 'POST',
    body: JSON.stringify({ bin_code: binCode, sku: sku, qty: qty, from_condition: fromCondition, to_condition: toCondition })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to move bin stock condition.', 'Condition Move Failed');
    return;
  }
  await showCustomAlert(`Moved ${qty} x ${sku} in bin ${binCode} from ${fromCondition} to ${toCondition}.`, 'Condition Move Complete');
  document.getElementById('bincond-qty').value = 1;
}

// Stage 26.5.4: LPN/carton/pallet grouping on top of bin_stock - assign a
// bin's sku/condition qty into a container, and look up what's inside one.
async function renderLPNView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">LPN / Carton / Pallet Grouping</h1>
      <p class="page-subtitle">Group bin stock into a carton or pallet container for tracking - a further breakdown of bin stock, never a second source of truth for a bin's total.</p>
    </div>
  `;
  container.appendChild(header);

  const assignPanel = document.createElement('div');
  assignPanel.className = 'table-panel';
  assignPanel.style.padding = '24px';
  assignPanel.style.marginBottom = '24px';
  assignPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">Assign Bin Stock to an LPN</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-code">LPN Code</label>
        <input type="text" id="lpn-code" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-bin">Bin Code</label>
        <input type="text" id="lpn-bin" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-sku">SKU</label>
        <input type="text" id="lpn-sku" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-condition">Condition</label>
        <select id="lpn-condition" class="form-input" style="width: 120px;">${BIN_STOCK_CONDITIONS.map(c => `<option value="${c}">${c}</option>`).join('')}</select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-qty">Qty</label>
        <input type="number" id="lpn-qty" class="form-input" style="width: 90px;" min="1" value="1">
      </div>
      <button class="btn btn-primary" id="lpn-assign-btn" type="button">Assign</button>
    </div>
    <div id="lpn-assign-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(assignPanel);

  const lookupPanel = document.createElement('div');
  lookupPanel.className = 'table-panel';
  lookupPanel.style.padding = '24px';
  lookupPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">Look Up an LPN's Contents</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="lpn-lookup-code">LPN Code</label>
        <input type="text" id="lpn-lookup-code" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="lpn-lookup-btn" type="button">Look Up</button>
    </div>
    <div id="lpn-contents-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(lookupPanel);

  document.getElementById('lpn-assign-btn').addEventListener('click', submitLPNAssign);
  document.getElementById('lpn-lookup-btn').addEventListener('click', lookupLPNContents);
  attachLinkTypeahead(document.getElementById('lpn-bin'), 'Bin');
  attachLinkTypeahead(document.getElementById('lpn-sku'), 'Item');
}

async function submitLPNAssign() {
  const errorEl = document.getElementById('lpn-assign-error');
  errorEl.classList.add('hidden');
  const lpnCode = document.getElementById('lpn-code').value.trim();
  const binCode = document.getElementById('lpn-bin').value.trim();
  const sku = document.getElementById('lpn-sku').value.trim();
  const condition = document.getElementById('lpn-condition').value;
  const qty = parseInt(document.getElementById('lpn-qty').value, 10);
  if (!lpnCode || !binCode || !sku || !qty || qty <= 0) {
    errorEl.textContent = 'LPN Code, Bin Code, SKU, and a Qty greater than zero are required.';
    errorEl.classList.remove('hidden');
    return;
  }
  const res = await apiFetch('/api/v1/wms/lpn/assign', {
    method: 'POST',
    body: JSON.stringify({ lpn_code: lpnCode, bin_code: binCode, sku, condition, qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to assign to LPN.', 'LPN Assign Failed'); return; }
  await showCustomAlert(`Assigned ${qty} x ${sku} (${condition}) from bin ${binCode} to LPN ${lpnCode}.`, 'LPN Assign Complete');
}

async function lookupLPNContents() {
  const resultEl = document.getElementById('lpn-contents-result');
  const lpnCode = document.getElementById('lpn-lookup-code').value.trim();
  if (!lpnCode) return;
  const res = await apiFetch(`/api/v1/wms/lpn/contents?lpn_code=${encodeURIComponent(lpnCode)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to look up LPN contents.', 'Lookup Failed'); return; }
  const lines = await res.json();
  if (lines.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No contents found for LPN ${lpnCode}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>Bin</th><th>SKU</th><th>Condition</th><th>Qty</th></tr></thead>
      <tbody>
        ${lines.map(l => `<tr><td>${l.bin_code}</td><td>${l.sku}</td><td>${l.condition}</td><td>${l.qty}</td></tr>`).join('')}
      </tbody>
    </table>
  `;
}

// Stage 26.5.5: bin-to-bin replenishment min/max triggers - suggestions
// (shortage = max_qty - current_qty, filled from reserve bins highest-qty-
// first) plus the action that actually executes one.
async function renderBinReplenishmentView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Bin Replenishment</h1>
      <p class="page-subtitle">Pick-face bins below their min qty, with a suggested reserve bin to draw from - configure rules via the BinReplenishmentRule master.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="replen-location">Location</label>
        <input type="text" id="replen-location" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="replen-fetch-btn" type="button">Get Suggestions</button>
    </div>
    <div id="replen-result"></div>
  `;
  container.appendChild(panel);
  document.getElementById('replen-fetch-btn').addEventListener('click', fetchBinReplenishmentSuggestions);
  attachLinkTypeahead(document.getElementById('replen-location'), 'Location');
}

async function fetchBinReplenishmentSuggestions() {
  const resultEl = document.getElementById('replen-result');
  const location = document.getElementById('replen-location').value.trim();
  if (!location) return;
  const res = await apiFetch(`/api/v1/wms/bin-replenishment/suggestions?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to fetch replenishment suggestions.', 'Fetch Failed'); return; }
  const suggestions = await res.json();
  if (suggestions.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No bins are below their min qty at ${location}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>Bin</th><th>SKU</th><th>Current</th><th>Min</th><th>Max</th><th>Shortage</th><th>From Bin</th><th>Move Qty</th><th></th></tr></thead>
      <tbody>
        ${suggestions.map((s, idx) => `
          <tr>
            <td>${s.bin_code}</td><td>${s.sku}</td><td>${s.current_qty}</td><td>${s.min_qty}</td><td>${s.max_qty}</td>
            <td><span class="badge badge-warning">${s.shortage}</span></td>
            <td>${s.from_bin_code || '&mdash;'}</td>
            <td>${s.move_qty || 0}</td>
            <td>${s.from_bin_code ? `<button class="action-btn" onclick="executeBinReplenishmentRow(${idx})">Replenish</button>` : ''}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
  window._replenSuggestions = suggestions;
}

window.executeBinReplenishmentRow = async function(idx) {
  const s = (window._replenSuggestions || [])[idx];
  if (!s || !s.from_bin_code) return;
  const res = await apiFetch('/api/v1/wms/bin-replenishment/execute', {
    method: 'POST',
    body: JSON.stringify({ from_bin_code: s.from_bin_code, to_bin_code: s.bin_code, sku: s.sku, qty: s.move_qty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to execute replenishment.', 'Replenishment Failed'); return; }
  await showCustomAlert(`Moved ${s.move_qty} x ${s.sku} from ${s.from_bin_code} to ${s.bin_code}.`, 'Replenishment Complete');
  fetchBinReplenishmentSuggestions();
};

// Stage 26.5.6: wave/batch pick-list grouping - tag a batch of open
// FulfillmentTasks into a wave, then generate one consolidated,
// zone-then-bin-sorted pick list covering every order in it.
async function renderWavePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Wave / Batch Picking</h1>
      <p class="page-subtitle">Tag several open fulfillment tasks into a wave, then generate one consolidated pick list instead of walking the warehouse once per order.</p>
    </div>
  `;
  container.appendChild(header);

  const assignPanel = document.createElement('div');
  assignPanel.className = 'table-panel';
  assignPanel.style.padding = '24px';
  assignPanel.style.marginBottom = '24px';
  assignPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">1. Tag tasks into a wave</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="wave-id">Wave ID</label>
        <input type="text" id="wave-id" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 260px;">
        <label class="form-label" for="wave-task-ids">Task IDs (comma-separated)</label>
        <input type="text" id="wave-task-ids" class="form-input" style="width: 100%;" placeholder="FT-1001, FT-1002, FT-1003">
      </div>
      <button class="btn btn-outline" id="wave-assign-btn" type="button">Tag Tasks</button>
    </div>
    <div id="wave-assign-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(assignPanel);

  const genPanel = document.createElement('div');
  genPanel.className = 'table-panel';
  genPanel.style.padding = '24px';
  genPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">2. Generate the wave pick list</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="wave-gen-id">Wave ID</label>
        <input type="text" id="wave-gen-id" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="wave-gen-btn" type="button">Generate Pick List</button>
    </div>
    <div id="wave-pick-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(genPanel);

  document.getElementById('wave-assign-btn').addEventListener('click', submitWaveAssign);
  document.getElementById('wave-gen-btn').addEventListener('click', submitWavePickList);
}

// Stage 26.5.14 (P2, go-ahead 2026-07-27): mobile/voice picking. Reuses
// the exact same wave pick-list endpoint Wave/Batch Picking already calls
// (GET /api/v1/wms/wave/pick-list) - no new picking logic, just a
// narrow single-item-at-a-time layout suited to a phone screen instead of
// a wide table, plus optional voice readout/confirm via the browser-native
// Web Speech API (SpeechSynthesis/SpeechRecognition) - no new dependency,
// and both are feature-detected so this degrades to silent button-tap
// navigation wherever unsupported (notably: no SpeechRecognition in
// Firefox as of this writing).
let mobilePickLines = [];
let mobilePickIndex = 0;
let mobilePickRecognition = null;

async function renderMobilePickingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Mobile Picking</h1>
      <p class="page-subtitle">A phone-friendly, one-item-at-a-time view of a wave's pick list, with optional voice readout and hands-free "next"/"confirm" control.</p>
    </div>
  `;
  container.appendChild(header);

  const loadPanel = document.createElement('div');
  loadPanel.className = 'table-panel';
  loadPanel.style.padding = '20px';
  loadPanel.style.marginBottom = '16px';
  loadPanel.style.maxWidth = '420px';
  loadPanel.innerHTML = `
    <div style="display:flex; gap:10px; align-items:flex-end; flex-wrap:wrap;">
      <div class="form-group" style="margin-bottom:0; flex:1; min-width:140px;">
        <label class="form-label" for="mobile-pick-wave-id">Wave ID</label>
        <input type="text" id="mobile-pick-wave-id" class="form-input" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="mobile-pick-load-btn" type="button">Load</button>
    </div>
    <div id="mobile-pick-error" class="login-error hidden" style="margin-top:10px;"></div>
  `;
  container.appendChild(loadPanel);

  const cardWrap = document.createElement('div');
  cardWrap.id = 'mobile-pick-card-wrap';
  cardWrap.style.maxWidth = '420px';
  container.appendChild(cardWrap);

  document.getElementById('mobile-pick-load-btn').addEventListener('click', loadMobilePickList);
}

async function loadMobilePickList() {
  const errorEl = document.getElementById('mobile-pick-error');
  errorEl.classList.add('hidden');
  const waveId = document.getElementById('mobile-pick-wave-id').value.trim();
  if (!waveId) { errorEl.textContent = 'Wave ID is required.'; errorEl.classList.remove('hidden'); return; }

  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { errorEl.textContent = await getErrorMessage(res, 'Failed to load the wave pick list.'); errorEl.classList.remove('hidden'); return; }
  const data = await res.json();
  mobilePickLines = data.pick_lines || [];
  mobilePickIndex = 0;
  renderMobilePickCard();
}

function renderMobilePickCard() {
  const wrap = document.getElementById('mobile-pick-card-wrap');
  if (!wrap) return;
  if (mobilePickLines.length === 0) {
    wrap.innerHTML = `<div class="table-panel" style="padding:24px; text-align:center; color:var(--text-muted);">No pick lines for this wave. Generate a wave under <b>Wave Picking</b> first.</div>`;
    return;
  }
  const line = mobilePickLines[mobilePickIndex];
  const speechSupported = 'speechSynthesis' in window;
  const listenSupported = !!(window.SpeechRecognition || window.webkitSpeechRecognition);
  wrap.innerHTML = `
    <div class="table-panel" style="padding:24px; text-align:center;">
      <div class="text-muted" style="font-size:13px; margin-bottom:12px;">Item ${mobilePickIndex + 1} of ${mobilePickLines.length}</div>
      <div style="font-size:32px; font-weight:700; letter-spacing:-0.5px; margin-bottom:6px;">${line.sku}</div>
      <div style="font-size:15px; color:var(--text-muted); margin-bottom:16px;">Zone ${line.zone || '-'} / Aisle ${line.aisle || '-'} / Rack ${line.rack || '-'} / Bin ${line.bin_code}</div>
      <div style="font-size:48px; font-weight:800; color:var(--primary-color); margin-bottom:20px;">${line.pick_qty}${line.shortfall ? ` <span class="badge badge-warning" style="font-size:14px; vertical-align:middle;">short ${line.shortfall}</span>` : ''}</div>
      <div style="display:flex; gap:10px; justify-content:center; margin-bottom:14px;">
        <button class="btn btn-outline" id="mobile-pick-prev" type="button" ${mobilePickIndex === 0 ? 'disabled' : ''}>Previous</button>
        <button class="btn btn-primary" id="mobile-pick-next" type="button" ${mobilePickIndex === mobilePickLines.length - 1 ? 'disabled' : ''}>Confirm &amp; Next</button>
      </div>
      ${speechSupported || listenSupported ? `
      <div style="display:flex; gap:10px; justify-content:center; padding-top:14px; border-top:1px solid var(--border-color);">
        ${speechSupported ? `<button class="btn btn-outline btn-sm" id="mobile-pick-speak" type="button">Speak Item</button>` : ''}
        ${listenSupported ? `<button class="btn btn-outline btn-sm" id="mobile-pick-listen" type="button">${mobilePickRecognition ? 'Stop Listening' : 'Listen ("next"/"confirm")'}</button>` : ''}
      </div>` : ''}
    </div>
  `;
  const prevBtn = document.getElementById('mobile-pick-prev');
  const nextBtn = document.getElementById('mobile-pick-next');
  if (prevBtn) prevBtn.addEventListener('click', () => { mobilePickIndex = Math.max(0, mobilePickIndex - 1); renderMobilePickCard(); });
  if (nextBtn) nextBtn.addEventListener('click', () => { mobilePickIndex = Math.min(mobilePickLines.length - 1, mobilePickIndex + 1); renderMobilePickCard(); });
  const speakBtn = document.getElementById('mobile-pick-speak');
  if (speakBtn) speakBtn.addEventListener('click', () => speakMobilePickLine(line));
  const listenBtn = document.getElementById('mobile-pick-listen');
  if (listenBtn) listenBtn.addEventListener('click', toggleMobilePickListening);
}

function speakMobilePickLine(line) {
  if (!('speechSynthesis' in window)) return;
  window.speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(`Pick ${line.pick_qty} of ${line.sku}, bin ${line.bin_code}, zone ${line.zone || 'unspecified'}.`);
  window.speechSynthesis.speak(utterance);
}

function toggleMobilePickListening() {
  const SpeechRecognitionCtor = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SpeechRecognitionCtor) return;
  const btn = document.getElementById('mobile-pick-listen');
  if (mobilePickRecognition) {
    mobilePickRecognition.stop();
    mobilePickRecognition = null;
    if (btn) btn.textContent = 'Listen ("next"/"confirm")';
    return;
  }
  mobilePickRecognition = new SpeechRecognitionCtor();
  mobilePickRecognition.continuous = true;
  mobilePickRecognition.interimResults = false;
  mobilePickRecognition.onresult = (event) => {
    // continuous:true keeps this instance running independent of DOM
    // rebuilds - renderMobilePickCard() replaces the card markup (including
    // the Listen button) but re-reads mobilePickRecognition's current state
    // to relabel the new button, no stop/restart needed here.
    const said = event.results[event.results.length - 1][0].transcript.trim().toLowerCase();
    if (said.includes('next') || said.includes('confirm')) {
      mobilePickIndex = Math.min(mobilePickLines.length - 1, mobilePickIndex + 1);
      renderMobilePickCard();
    } else if (said.includes('previous') || said.includes('back')) {
      mobilePickIndex = Math.max(0, mobilePickIndex - 1);
      renderMobilePickCard();
    }
  };
  mobilePickRecognition.onerror = () => { mobilePickRecognition = null; };
  mobilePickRecognition.onend = () => { mobilePickRecognition = null; };
  mobilePickRecognition.start();
  if (btn) btn.textContent = 'Stop Listening';
}

async function submitWaveAssign() {
  const resultEl = document.getElementById('wave-assign-result');
  const waveId = document.getElementById('wave-id').value.trim();
  const taskIds = document.getElementById('wave-task-ids').value.split(',').map(s => s.trim()).filter(Boolean);
  if (!waveId || taskIds.length === 0) { resultEl.textContent = 'Wave ID and at least one Task ID are required.'; return; }
  const res = await apiFetch('/api/v1/wms/wave/assign', {
    method: 'POST',
    body: JSON.stringify({ wave_id: waveId, task_ids: taskIds })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to tag tasks into wave.', 'Wave Assign Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Tagged ${data.tagged} of ${taskIds.length} task(s)</span> into wave ${waveId}.`;
  // Same class as the renderExecDashboard null-deref: this runs after an
  // await, so the user may have navigated away and taken the form with them.
  // Writing to a detached `resultEl` above is harmless; a null getElementById
  // here is not. The request itself already succeeded either way - only the
  // convenience refill of the form is skipped.
  const waveIdInput = document.getElementById('wave-gen-id');
  if (waveIdInput) waveIdInput.value = waveId;
}

async function submitWavePickList() {
  const resultEl = document.getElementById('wave-pick-result');
  const waveId = document.getElementById('wave-gen-id').value.trim();
  if (!waveId) return;
  const res = await apiFetch(`/api/v1/wms/wave/pick-list?wave_id=${encodeURIComponent(waveId)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to generate wave pick list.', 'Wave Pick List Failed'); return; }
  const data = await res.json();
  let html = `<h3 style="font-size: 14px; font-weight: 700; margin: 16px 0 8px;">Consolidated Pick List (zone/aisle/rack walking order)</h3>`;
  html += `<table><thead><tr><th>Zone</th><th>Aisle</th><th>Rack</th><th>Bin</th><th>SKU</th><th>Pick Qty</th></tr></thead><tbody>`;
  html += data.pick_lines.length === 0
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No pick lines &mdash; nothing in this wave is currently stored in a bin.</td></tr>`
    : data.pick_lines.map(l => `<tr><td>${l.zone || ''}</td><td>${l.aisle || ''}</td><td>${l.rack || ''}</td><td>${l.bin_code}</td><td>${l.sku}</td><td>${l.pick_qty}</td></tr>`).join('');
  html += `</tbody></table>`;
  html += `<h3 style="font-size: 14px; font-weight: 700; margin: 20px 0 8px;">Per-Order Allocation</h3>`;
  html += `<table><thead><tr><th>Task ID</th><th>SKU</th><th>Allocated Qty</th><th>Shortfall</th></tr></thead><tbody>`;
  html += data.allocations.length === 0
    ? `<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No allocations &mdash; no stock could be reserved for these orders. Check on-hand quantities under <b>Inventory</b>.</td></tr>`
    : data.allocations.map(a => `<tr><td>${a.task_id}</td><td>${a.sku}</td><td>${a.allocated_qty}</td><td>${a.shortfall ? `<span class="badge badge-warning">${a.shortfall}</span>` : '0'}</td></tr>`).join('');
  html += `</tbody></table>`;
  resultEl.innerHTML = html;
}

// Cycle Count session lines are created the same way every other line-item
// bulk load happens in this repo (Stage 20.21: reuses BulkImportCSV rather
// than a bespoke entry form) - but CycleCountLine is a Transaction doctype,
// and the Setup submenu's generic doctype browser only lists Master
// doctypes (renderSidebarSubmenu's `document_type === 'Master'` filter), so
// there was previously no way to reach CycleCountLine's generic table (and
// therefore its Bulk Import button) from the UI at all. Fixed here by
// linking directly into the existing generic doctype-table view instead of
// building a second import mechanism.
async function renderCycleCountView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Cycle Count</h1>
      <p class="page-subtitle">Reconcile a count session: zero-variance lines post immediately, non-zero variance routes to approval.</p>
    </div>
  `;
  container.appendChild(header);

  const importPanel = document.createElement('div');
  importPanel.className = 'table-panel';
  importPanel.style.padding = '24px';
  importPanel.style.marginBottom = '24px';
  importPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">1. Enter counted quantities</h2>
    <p style="color: var(--text-muted); margin-bottom: 12px;">Count lines are entered the same way as any other bulk line-item load, via Bulk Import.</p>
    <button class="btn btn-outline" id="cyclecount-open-lines-btn" type="button">Manage Count Lines</button>
  `;
  container.appendChild(importPanel);

  const reconcilePanel = document.createElement('div');
  reconcilePanel.className = 'table-panel';
  reconcilePanel.style.padding = '24px';
  reconcilePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">2. Reconcile a session</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="cyclecount-session">Count Session</label>
        <input type="text" id="cyclecount-session" class="form-input" style="width: 220px;">
      </div>
      <button class="btn btn-primary" id="cyclecount-reconcile-btn" type="button">Reconcile Session</button>
    </div>
    <div id="cyclecount-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
    <div id="cyclecount-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(reconcilePanel);

  // Stage 26.5.10: a non-zero-variance line cannot post until it has a
  // variance root-cause reason code - set one here, then (if the line was
  // already Approved before the reason existed) retry the post directly.
  const variancePanel = document.createElement('div');
  variancePanel.className = 'table-panel';
  variancePanel.style.padding = '24px';
  variancePanel.style.marginTop = '24px';
  variancePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">3. Variance root-cause + posting</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="ccvariance-line-id">Line ID</label>
        <input type="text" id="ccvariance-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="ccvariance-reason">Reason Code (ReasonCode ID)</label>
        <input type="text" id="ccvariance-reason" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="ccvariance-set-btn" type="button">Set Variance Reason</button>
      <button class="btn btn-primary" id="ccvariance-post-btn" type="button">Retry Post</button>
    </div>
    <div id="ccvariance-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(variancePanel);

  // Stage 26.5.10: blind recount - a second, blind count on a line whose
  // first result looks wrong, before it's trusted enough to post.
  const recountPanel = document.createElement('div');
  recountPanel.className = 'table-panel';
  recountPanel.style.padding = '24px';
  recountPanel.style.marginTop = '24px';
  recountPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">4. Blind recount</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-orig-line-id">Original Line ID</label>
        <input type="text" id="recount-orig-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="recount-request-btn" type="button">Request Recount</button>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-new-line-id">Recount Line ID</label>
        <input type="text" id="recount-new-line-id" class="form-input" style="width: 180px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="recount-value">Counted Qty (blind)</label>
        <input type="number" id="recount-value" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-outline" id="recount-submit-btn" type="button">Submit Recount Value</button>
    </div>
    <div id="recount-result" style="margin-top: 12px; font-size: 13px; color: var(--text-muted);"></div>
  `;
  container.appendChild(recountPanel);

  // Stage 26.5.9: ABC cycle-count planner - which SKUs are due for their
  // next count, ranked by velocity tier.
  const abcPanel = document.createElement('div');
  abcPanel.className = 'table-panel';
  abcPanel.style.padding = '24px';
  abcPanel.style.marginTop = '24px';
  abcPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 12px;">5. ABC cycle-count planner</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="abc-location">Location</label>
        <input type="text" id="abc-location" class="form-input" style="width: 160px;" autocomplete="off">
      </div>
      <button class="btn btn-primary" id="abc-plan-btn" type="button">Get Plan</button>
    </div>
    <div id="abc-plan-result" style="margin-top: 16px;"></div>
  `;
  container.appendChild(abcPanel);

  document.getElementById('cyclecount-open-lines-btn').addEventListener('click', () => {
    currentDoctype = 'CycleCountLine';
    currentSearchQuery = '';
    currentTablePage = 1;
    renderView('doctype-table');
  });
  document.getElementById('cyclecount-reconcile-btn').addEventListener('click', submitCycleCountReconcile);
  document.getElementById('ccvariance-set-btn').addEventListener('click', submitCycleCountVarianceReason);
  document.getElementById('ccvariance-post-btn').addEventListener('click', submitRetryCycleCountPost);
  document.getElementById('recount-request-btn').addEventListener('click', submitRequestRecount);
  document.getElementById('recount-submit-btn').addEventListener('click', submitRecountValue);
  document.getElementById('abc-plan-btn').addEventListener('click', fetchABCCycleCountPlan);
  attachLinkTypeahead(document.getElementById('abc-location'), 'Location');
}

async function submitCycleCountReconcile() {
  const errorEl = document.getElementById('cyclecount-form-error');
  errorEl.classList.add('hidden');
  const resultEl = document.getElementById('cyclecount-result');
  resultEl.innerHTML = '';

  const countSession = document.getElementById('cyclecount-session').value.trim();
  if (!countSession) {
    errorEl.textContent = 'Count Session is required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/wms/cycle-count/reconcile', {
    method: 'POST',
    body: JSON.stringify({ count_session: countSession })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to reconcile the count session.', 'Reconcile Failed');
    return;
  }
  const data = await res.json();
  resultEl.innerHTML = `
    <span class="badge badge-success">${data.posted_no_variance || 0} posted (no variance)</span>
    &nbsp;
    <span class="badge badge-warning">${data.pending_approval || 0} pending approval</span>
  `;
}

async function submitCycleCountVarianceReason() {
  const resultEl = document.getElementById('ccvariance-result');
  const lineId = document.getElementById('ccvariance-line-id').value.trim();
  const reasonCode = document.getElementById('ccvariance-reason').value.trim();
  if (!lineId || !reasonCode) { resultEl.textContent = 'Line ID and Reason Code are both required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/variance-reason', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId, reason_code: reasonCode })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to set variance reason.', 'Set Reason Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Variance reason set</span> on line ${lineId}.`;
}

async function submitRetryCycleCountPost() {
  const resultEl = document.getElementById('ccvariance-result');
  const lineId = document.getElementById('ccvariance-line-id').value.trim();
  if (!lineId) { resultEl.textContent = 'Line ID is required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/post-adjustment', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to post the adjustment.', 'Post Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Posted</span> line ${lineId}.`;
}

async function submitRequestRecount() {
  const resultEl = document.getElementById('recount-result');
  const origLineId = document.getElementById('recount-orig-line-id').value.trim();
  if (!origLineId) { resultEl.textContent = 'Original Line ID is required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/recount/request', {
    method: 'POST',
    body: JSON.stringify({ line_id: origLineId })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to request recount.', 'Recount Request Failed'); return; }
  const data = await res.json();
  resultEl.innerHTML = `<span class="badge badge-success">Recount line ${data.new_line_id} created</span> (blind - no counted/system qty carried over). Enter its value below.`;
  const recountInput = document.getElementById('recount-new-line-id');
  if (recountInput) recountInput.value = data.new_line_id;
}

async function submitRecountValue() {
  const resultEl = document.getElementById('recount-result');
  const lineId = document.getElementById('recount-new-line-id').value.trim();
  const countedQty = parseFloat(document.getElementById('recount-value').value);
  if (!lineId || isNaN(countedQty)) { resultEl.textContent = 'Recount Line ID and a Counted Qty are both required.'; return; }
  const res = await apiFetch('/api/v1/wms/cycle-count/recount/submit', {
    method: 'POST',
    body: JSON.stringify({ line_id: lineId, counted_qty: countedQty })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to submit recount value.', 'Recount Submit Failed'); return; }
  resultEl.innerHTML = `<span class="badge badge-success">Recount value ${countedQty} recorded</span> on ${lineId}. Reconcile its count_session above to post it.`;
}

async function fetchABCCycleCountPlan() {
  const resultEl = document.getElementById('abc-plan-result');
  const location = document.getElementById('abc-location').value.trim();
  if (!location) return;
  const res = await apiFetch(`/api/v1/wms/cycle-count/abc-plan?location_code=${encodeURIComponent(location)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to fetch the ABC cycle-count plan.', 'Fetch Failed'); return; }
  const plan = await res.json();
  if (plan.length === 0) {
    resultEl.innerHTML = `<p style="color: var(--text-muted);">No SKUs on hand at ${location}.</p>`;
    return;
  }
  resultEl.innerHTML = `
    <table>
      <thead><tr><th>SKU</th><th>Tier</th><th>Daily Velocity</th><th>Days Since Last Count</th><th>Interval</th><th>Due</th></tr></thead>
      <tbody>
        ${plan.map(s => `
          <tr>
            <td style="font-family: monospace;">${s.sku}</td>
            <td><span class="badge badge-secondary">${s.tier}</span></td>
            <td>${s.daily_velocity.toFixed(2)}</td>
            <td>${s.days_since_last_count < 0 ? 'never' : s.days_since_last_count}</td>
            <td>${s.interval_days}d</td>
            <td>${s.due ? '<span class="badge badge-warning">Due</span>' : ''}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

// Unified OMS workbench: the order-to-cash operational view over the
// existing SalesOrder, FulfillmentTask, LogisticsBooking, and SalesInvoice
// doctypes. It deliberately reads through the generic document API rather
// than creating a second read model/API for data already available there.
// ---------------------------------------------------------------------------
// The OMS Console (Stage 35.2)
//
// What this replaces, and why: the previous version of this screen fetched
// GET /api/v1/doc/SalesOrder, /FulfillmentTask, /LogisticsBooking and
// /SalesInvoice in full and joined them in the browser. That has no filter, no
// pagination and no ordering - it transfers every order the tenant has ever
// taken on every page view, and faceting is impossible on a page that only
// holds one page of rows. All of that moved to SQL behind /api/v1/oms/*
// (engines/oms_console.go); this file now renders one page plus its facets.
//
// Everything here is built from the existing vocabulary: .table-panel,
// .stat-card, .btn/.action-btn, .badge, .bulk-edit-bar and the
// .modal-overlay/.modal-container primitives. No new table implementation, no
// new dialog mechanism, no framework.
// ---------------------------------------------------------------------------

// omsConsoleState is the whole screen's state: the active filter, the current
// selection, and the last result. Module-level rather than re-derived from the
// DOM so a re-render after an action keeps the operator where they were -
// losing your filter every time you release a hold is what makes a queue
// screen unusable at 200 orders.
let omsConsoleState = {
  filter: { channel: '', status: '', hold_reason: '', location: '', from_date: '', to_date: '', sla_minutes: 0 },
  limit: 50,
  offset: 0,
  selected: new Set(),
  lastResult: null
};

function omsFilterQuery(extra = {}) {
  const params = new URLSearchParams();
  const merged = { ...omsConsoleState.filter, limit: omsConsoleState.limit, offset: omsConsoleState.offset, ...extra };
  Object.entries(merged).forEach(([key, value]) => {
    if (value !== '' && value !== 0 && value !== null && value !== undefined) params.set(key, value);
  });
  return params.toString();
}

async function renderOMSWorkbenchView(container) {
  container.innerHTML = `
    <div class="page-header">
      <div class="page-title-section">
        <h1 class="page-title">Order Management</h1>
        <p class="page-subtitle">Every channel's orders in one queue &mdash; filter, act in bulk, and open any order end to end.</p>
      </div>
      <div class="page-actions">
        <button class="btn btn-outline" id="oms-refresh">Refresh</button>
      </div>
    </div>
    <!-- dashboard-stats-row / stat-val, not stats-grid / stat-value: the two
         class names the previous version of this screen used do not exist in
         styles.css at all, which is why its four tiles rendered as full-width
         stacked bars with an unstyled number. -->
    <div class="dashboard-stats-row" id="oms-tiles"></div>
    <div class="table-panel oms-compact-panel" style="padding:16px 24px;margin-bottom:24px;">
      <div class="oms-search-row">
        <input type="search" id="oms-global-search" class="form-input" placeholder="Search any order: order id, channel order id, AWB, phone, customer or SKU">
        <button class="btn btn-outline" id="oms-search-btn">Search</button>
        <button class="btn btn-outline" id="oms-search-clear">Clear</button>
      </div>
      <div id="oms-search-results"></div>
    </div>
    <div class="table-panel" id="oms-manual-panel" style="padding:24px;margin-bottom:24px;"></div>
    <div class="table-panel" style="padding:24px;">
      <div class="oms-console-head">
        <h2 style="font-size:16px;margin:0;">Orders</h2>
        <div class="oms-view-actions">
          <select id="oms-saved-views" class="form-input" style="max-width:220px;"><option value="">Saved views…</option></select>
          <button class="btn btn-outline btn-sm" id="oms-save-view">Save this view</button>
          <button class="btn btn-outline btn-sm" id="oms-delete-view">Delete view</button>
        </div>
      </div>
      <div id="oms-facets" class="oms-facets"></div>
      <div class="bulk-edit-bar hidden" id="oms-bulk-bar">
        <span id="oms-selection-count">0 selected</span>
        <button class="btn btn-outline" id="oms-bulk-release">Release Hold</button>
        <button class="btn btn-outline" id="oms-bulk-hold">Hold</button>
        <button class="btn btn-outline" id="oms-bulk-cancel">Cancel</button>
      </div>
      <div id="oms-order-table"></div>
    </div>`;

  document.getElementById('oms-refresh').addEventListener('click', () => renderView('oms'));
  document.getElementById('oms-search-btn').addEventListener('click', runOMSGlobalSearch);
  document.getElementById('oms-global-search').addEventListener('keydown', e => { if (e.key === 'Enter') runOMSGlobalSearch(); });
  document.getElementById('oms-search-clear').addEventListener('click', () => {
    document.getElementById('oms-global-search').value = '';
    document.getElementById('oms-search-results').innerHTML = '';
  });
  document.getElementById('oms-bulk-release').addEventListener('click', () => runOMSBulkAction('release'));
  document.getElementById('oms-bulk-hold').addEventListener('click', () => runOMSBulkAction('hold'));
  document.getElementById('oms-bulk-cancel').addEventListener('click', () => runOMSBulkAction('cancel'));
  document.getElementById('oms-save-view').addEventListener('click', saveCurrentOMSView);
  document.getElementById('oms-delete-view').addEventListener('click', deleteSelectedOMSView);
  document.getElementById('oms-saved-views').addEventListener('change', applySelectedOMSView);

  renderManualOrderPanel(document.getElementById('oms-manual-panel'));
  // The tiles, the saved views and the order list are independent reads, so
  // they go out together rather than in sequence.
  await Promise.all([loadOMSTiles(), loadOMSSavedViews(), loadOMSOrders()]);
}

// 35.2.4 - four tiles, each the row count of an already-registered report.
async function loadOMSTiles() {
  const host = document.getElementById('oms-tiles');
  if (!host) return;
  const res = await apiFetch('/api/v1/oms/tiles');
  if (!res || !res.ok) { host.innerHTML = ''; return; }
  const { tiles = [] } = await res.json();
  host.innerHTML = tiles.map(t => `
    <div class="stat-card oms-tile" data-report="${escapeHTMLText(t.report_id)}" title="Open the ${escapeHTMLText(t.label)} report">
      <span class="stat-label">${escapeHTMLText(t.label)}</span>
      <span class="stat-val">${t.error ? '—' : escapeHTMLText(String(t.count))}</span>
      ${t.error ? `<div class="oms-tile-error" title="${escapeHTMLText(t.error)}">unavailable</div>` : ''}
    </div>`).join('');
  // A tile is a shortcut into its own report, not a dead number.
  host.querySelectorAll('.oms-tile').forEach(tile => {
    tile.addEventListener('click', () => execDashboardOpenReport(tile.getAttribute('data-report')));
  });
}

// 35.2.1 - the faceted, paginated list.
async function loadOMSOrders() {
  const host = document.getElementById('oms-order-table');
  if (!host) return;
  host.innerHTML = '<div class="text-muted" style="padding:16px;">Loading orders…</div>';
  const res = await apiFetch(`/api/v1/oms/orders?${omsFilterQuery()}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load orders.'); host.innerHTML = ''; return; }
  const result = await res.json();
  omsConsoleState.lastResult = result;
  renderOMSFacets(result.facets || {});
  renderOMSOrderTable(result);
}

function renderOMSFacets(facets) {
  const host = document.getElementById('oms-facets');
  if (!host) return;
  const group = (key, label, values) => {
    const options = (values || []).map(v =>
      `<option value="${escapeHTMLText(v.value)}"${omsConsoleState.filter[key] === v.value ? ' selected' : ''}>${escapeHTMLText(v.value)} (${v.count})</option>`).join('');
    return `<label class="oms-facet"><span>${escapeHTMLText(label)}</span>
      <select class="form-input" data-facet="${key}"><option value="">All</option>${options}</select></label>`;
  };
  host.innerHTML = `
    ${group('channel', 'Channel', facets.channel)}
    ${group('status', 'Status', facets.status)}
    ${group('hold_reason', 'Hold reason', facets.hold_reason)}
    ${group('location', 'Location', facets.location)}
    <label class="oms-facet"><span>From</span><input type="date" class="form-input" data-facet="from_date" value="${escapeHTMLText(omsConsoleState.filter.from_date || '')}"></label>
    <label class="oms-facet"><span>To</span><input type="date" class="form-input" data-facet="to_date" value="${escapeHTMLText(omsConsoleState.filter.to_date || '')}"></label>
    <label class="oms-facet"><span>SLA breach over</span>
      <select class="form-input" data-facet="sla_minutes">
        <option value="0">Any age</option>
        <option value="60"${omsConsoleState.filter.sla_minutes == 60 ? ' selected' : ''}>1 hour</option>
        <option value="240"${omsConsoleState.filter.sla_minutes == 240 ? ' selected' : ''}>4 hours</option>
        <option value="1440"${omsConsoleState.filter.sla_minutes == 1440 ? ' selected' : ''}>24 hours</option>
      </select></label>
    <button class="btn btn-outline btn-sm" id="oms-clear-filters">Clear filters</button>`;

  host.querySelectorAll('[data-facet]').forEach(control => {
    control.addEventListener('change', () => {
      const key = control.getAttribute('data-facet');
      omsConsoleState.filter[key] = key === 'sla_minutes' ? Number(control.value) : control.value;
      // Changing a filter invalidates both the page and the selection - acting
      // in bulk on rows that scrolled out of the filter is exactly the kind of
      // surprise a queue screen must not spring on anyone.
      omsConsoleState.offset = 0;
      omsConsoleState.selected.clear();
      loadOMSOrders();
    });
  });
  document.getElementById('oms-clear-filters').addEventListener('click', () => {
    omsConsoleState.filter = { channel: '', status: '', hold_reason: '', location: '', from_date: '', to_date: '', sla_minutes: 0 };
    omsConsoleState.offset = 0;
    omsConsoleState.selected.clear();
    loadOMSOrders();
  });
}

function omsStatusBadge(status, holdReason) {
  const cls = status === 'On Hold' ? 'badge-warning' : (status === 'Delivered' || status === 'Shipped') ? 'badge-success' : status === 'Cancelled' ? 'badge-danger' : 'badge-secondary';
  return `<span class="badge ${cls}">${escapeHTMLText(status || '—')}</span>` +
    (holdReason ? `<div class="oms-hold-reason">${escapeHTMLText(holdReason)}</div>` : '');
}

function renderOMSOrderTable(result) {
  const host = document.getElementById('oms-order-table');
  const rows = result.rows || [];
  const from = result.total === 0 ? 0 : result.offset + 1;
  const to = result.offset + rows.length;
  host.innerHTML = `
    <div class="table-wrapper">
      <table>
        <thead><tr>
          <th style="width:32px;"><input type="checkbox" id="oms-select-all" aria-label="Select all orders on this page"></th>
          <th>Order</th><th>Source</th><th>Customer</th><th>Status</th><th>Lines</th><th>Location</th><th>Age</th><th class="num">Value</th><th>Actions</th>
        </tr></thead>
        <tbody>
        ${rows.length === 0
          ? `<tr><td colspan="10" style="text-align:center;color:var(--text-muted);padding:24px;">No orders match this filter. Clear the filters above, use <b>New manual order</b>, or let a channel import create one &mdash; all of them land here.</td></tr>`
          : rows.map(o => {
            const channel = o.channel || 'Manual';
            const source = `${escapeHTMLText(channel)}${o.channel_order_id ? `<div class="oms-channel-ref" title="The order id in ${escapeHTMLText(channel)}">${escapeHTMLText(o.channel_order_id)}</div>` : ''}`;
            const age = o.age_minutes < 60 ? `${o.age_minutes}m` : o.age_minutes < 1440 ? `${Math.floor(o.age_minutes / 60)}h` : `${Math.floor(o.age_minutes / 1440)}d`;
            return `<tr>
              <td><input type="checkbox" class="oms-row-select" data-order="${escapeHTMLText(o.order_id)}"${omsConsoleState.selected.has(o.order_id) ? ' checked' : ''}></td>
              <td style="font-family:monospace;">${copyableCell(escapeHTMLText(o.order_id), o.order_id)}${o.priority === 'Expedite' ? '<div class="badge badge-warning oms-expedite">Expedite</div>' : ''}</td>
              <td>${source}</td>
              <td>${escapeHTMLText(o.customer_name || '—')}${o.customer_phone ? `<div class="oms-channel-ref">${escapeHTMLText(o.customer_phone)}</div>` : ''}</td>
              <td>${omsStatusBadge(o.status, o.hold_reason)}</td>
              <td>${escapeHTMLText(String(o.line_count))}</td>
              <td>${escapeHTMLText(o.locations || '—')}</td>
              <td title="${escapeHTMLText(String(o.created_at))}">${escapeHTMLText(age)}</td>
              <td class="num">${formatMoney(o.total_amount)}</td>
              <td><button class="action-btn" data-open-order="${escapeHTMLText(o.order_id)}">Open</button></td>
            </tr>`;
          }).join('')}
        </tbody>
      </table>
    </div>
    <div class="oms-pager">
      <span class="text-muted">${result.total === 0 ? 'No orders' : `Showing ${from}–${to} of ${result.total}`}</span>
      <span>
        <button class="btn btn-outline btn-sm" id="oms-prev"${result.offset === 0 ? ' disabled' : ''}>Previous</button>
        <button class="btn btn-outline btn-sm" id="oms-next"${to >= result.total ? ' disabled' : ''}>Next</button>
      </span>
    </div>`;

  host.querySelectorAll('[data-open-order]').forEach(btn => {
    btn.addEventListener('click', () => openOMSOrderDetail(btn.getAttribute('data-open-order')));
  });
  host.querySelectorAll('.oms-row-select').forEach(box => {
    box.addEventListener('change', () => {
      const id = box.getAttribute('data-order');
      if (box.checked) omsConsoleState.selected.add(id); else omsConsoleState.selected.delete(id);
      updateOMSBulkBar();
    });
  });
  document.getElementById('oms-select-all').addEventListener('change', e => {
    host.querySelectorAll('.oms-row-select').forEach(box => {
      box.checked = e.target.checked;
      const id = box.getAttribute('data-order');
      if (e.target.checked) omsConsoleState.selected.add(id); else omsConsoleState.selected.delete(id);
    });
    updateOMSBulkBar();
  });
  document.getElementById('oms-prev').addEventListener('click', () => {
    omsConsoleState.offset = Math.max(0, omsConsoleState.offset - omsConsoleState.limit);
    loadOMSOrders();
  });
  document.getElementById('oms-next').addEventListener('click', () => {
    omsConsoleState.offset += omsConsoleState.limit;
    loadOMSOrders();
  });
  updateOMSBulkBar();
}

function updateOMSBulkBar() {
  const bar = document.getElementById('oms-bulk-bar');
  const count = omsConsoleState.selected.size;
  if (!bar) return;
  bar.classList.toggle('hidden', count === 0);
  document.getElementById('oms-selection-count').textContent = `${count} selected`;
}

// 35.2.5 - bulk hold/release/cancel. The endpoint reports per-order outcomes,
// so a partially-applicable selection tells the operator exactly which orders
// refused and why instead of a single "some failed".
async function runOMSBulkAction(action) {
  const orderIDs = Array.from(omsConsoleState.selected);
  if (orderIDs.length === 0) return;
  let reasonCode = '';
  if (action === 'hold' || action === 'cancel') {
    const label = action === 'hold' ? 'Active Hold reason-code:' : 'Active Cancellation reason-code:';
    reasonCode = await showCustomPrompt(label, '', `Bulk ${action} ${orderIDs.length} order(s)`);
    if (reasonCode === null || !reasonCode.trim()) return;
    reasonCode = reasonCode.trim();
  }
  const res = await apiFetch('/api/v1/oms/orders/bulk', {
    method: 'POST',
    body: JSON.stringify({ action, order_ids: orderIDs, reason_code: reasonCode })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, `Failed to ${action} the selected orders.`); return; }
  const result = await res.json();
  const failed = Object.entries(result.failed || {});
  if (failed.length === 0) {
    showToast(`${result.succeeded.length} order(s) ${action === 'release' ? 'released' : action + 'ed'}.`);
  } else {
    showToast(`${result.succeeded.length} succeeded, ${failed.length} refused. First: ${failed[0][0]} — ${failed[0][1]}`, { duration: 9000 });
  }
  omsConsoleState.selected.clear();
  await Promise.all([loadOMSOrders(), loadOMSTiles()]);
}

// 35.2.6 - global search.
async function runOMSGlobalSearch() {
  const query = document.getElementById('oms-global-search').value.trim();
  const host = document.getElementById('oms-search-results');
  if (!query) { host.innerHTML = ''; return; }
  const res = await apiFetch(`/api/v1/oms/orders/search?q=${encodeURIComponent(query)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Search failed.'); return; }
  const { results = [] } = await res.json();
  if (results.length === 0) {
    host.innerHTML = `<div class="text-muted" style="padding:12px 0;">Nothing matched “${escapeHTMLText(query)}”. Order id, channel order id, AWB, phone, customer name and SKU are all searchable.</div>`;
    return;
  }
  host.innerHTML = `
    <div class="table-wrapper" style="margin-top:12px;">
      <table><thead><tr><th>Order</th><th>Matched on</th><th>Source</th><th>Customer</th><th>Status</th><th></th></tr></thead>
      <tbody>${results.map(r => `
        <tr>
          <td style="font-family:monospace;">${escapeHTMLText(r.order_id)}</td>
          <td>${escapeHTMLText(r.matched_on)}</td>
          <td>${escapeHTMLText(r.channel)}${r.channel_order_id ? `<div class="oms-channel-ref">${escapeHTMLText(r.channel_order_id)}</div>` : ''}</td>
          <td>${escapeHTMLText(r.customer_name || '—')}</td>
          <td>${omsStatusBadge(r.status, '')}</td>
          <td><button class="action-btn" data-open-search="${escapeHTMLText(r.order_id)}">Open</button></td>
        </tr>`).join('')}</tbody></table>
    </div>`;
  host.querySelectorAll('[data-open-search]').forEach(btn => {
    btn.addEventListener('click', () => openOMSOrderDetail(btn.getAttribute('data-open-search')));
  });
}

// 35.2.1's saved views.
async function loadOMSSavedViews() {
  const select = document.getElementById('oms-saved-views');
  if (!select) return;
  const res = await apiFetch('/api/v1/oms/views');
  if (!res || !res.ok) return;
  const { views = [] } = await res.json();
  select.innerHTML = '<option value="">Saved views…</option>' +
    views.map(v => `<option value="${escapeHTMLText(v.id)}">${escapeHTMLText(v.name)}</option>`).join('');
  select._views = views;
}

async function saveCurrentOMSView() {
  const name = await showCustomPrompt('Name this view:', '', 'Save View');
  if (name === null || !name.trim()) return;
  // The saved filter uses the Go struct's field names, which is what
  // OrderConsoleFilter unmarshals - the query-string names are a separate,
  // lowercase vocabulary and would silently save an empty filter.
  const f = omsConsoleState.filter;
  const res = await apiFetch('/api/v1/oms/views', {
    method: 'POST',
    body: JSON.stringify({
      name: name.trim(),
      filter: {
        Channel: f.channel, Status: f.status, HoldReason: f.hold_reason, Location: f.location,
        FromDate: f.from_date, ToDate: f.to_date, SLAMinutes: Number(f.sla_minutes) || 0
      }
    })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to save this view.'); return; }
  showToast('View saved.');
  loadOMSSavedViews();
}

function applySelectedOMSView() {
  const select = document.getElementById('oms-saved-views');
  const view = (select._views || []).find(v => v.id === select.value);
  if (!view) return;
  const f = view.filter || {};
  omsConsoleState.filter = {
    channel: f.Channel || '', status: f.Status || '', hold_reason: f.HoldReason || '',
    location: f.Location || '', from_date: f.FromDate || '', to_date: f.ToDate || '',
    sla_minutes: f.SLAMinutes || 0
  };
  omsConsoleState.offset = 0;
  omsConsoleState.selected.clear();
  loadOMSOrders();
}

async function deleteSelectedOMSView() {
  const select = document.getElementById('oms-saved-views');
  if (!select.value) { showToast('Pick a saved view to delete first.'); return; }
  const res = await apiFetch(`/api/v1/oms/views/${encodeURIComponent(select.value)}`, { method: 'DELETE' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to delete this view.'); return; }
  showToast('View deleted.');
  loadOMSSavedViews();
}

// 35.2.2 / 35.2.3 - the order detail, with the action bar on it.
//
// One modal built on the existing .modal-overlay/.modal-container primitives
// rather than a third dialog mechanism, and one fetch rather than nine: the
// detail endpoint assembles lines, reservations, tasks, shipments, invoices,
// returns, refunds, notifications and the audit trail server-side.
window.openOMSOrderDetail = async function(orderID) {
  const res = await apiFetch(`/api/v1/oms/orders/${encodeURIComponent(orderID)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load this order.'); return; }
  const d = await res.json();
  const order = d.order || {};
  const status = order.order_status || '';
  const terminal = ['Shipped', 'Delivered', 'Closed', 'Cancelled'].includes(status);

  const section = (title, rows, columns) => `
    <h4 class="oms-detail-heading">${escapeHTMLText(title)} <span class="text-muted">(${rows.length})</span></h4>
    ${rows.length === 0
      ? `<p class="text-muted oms-detail-empty">None.</p>`
      : `<div class="table-wrapper"><table><thead><tr>${columns.map(c => `<th>${escapeHTMLText(c.label)}</th>`).join('')}</tr></thead>
         <tbody>${rows.map(r => `<tr>${columns.map(c => `<td>${escapeHTMLText(String(r[c.key] ?? '—'))}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`}`;

  const lineRows = (d.lines || []).map(l => `
    <tr>
      <td style="font-family:monospace;">${escapeHTMLText(l.line_id)}</td>
      <td>${escapeHTMLText(l.sku)}</td>
      <td class="num">${escapeHTMLText(String(l.qty))}</td>
      <td class="num">${formatMoney(l.unit_price)}</td>
      <td>${escapeHTMLText(l.location_code || '—')}</td>
      <td>${omsStatusBadge(l.line_status, l.hold_reason)}</td>
      <td>${l.line_status === 'On Hold'
            ? `<button class="action-btn" data-line-release="${escapeHTMLText(l.line_id)}">Release line</button>`
            : (['Dispatched', 'Cancelled', 'Returned'].includes(l.line_status) ? '' : `<button class="action-btn" data-line-hold="${escapeHTMLText(l.line_id)}">Hold line</button>`)}
          <label class="oms-split-pick"><input type="checkbox" class="oms-split-line" data-line="${escapeHTMLText(l.line_id)}"> split</label></td>
    </tr>`).join('');

  document.getElementById('oms-order-detail-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-order-detail-modal';
  overlay.innerHTML = `
    <div class="modal-container oms-detail-container">
      <div class="modal-header">
        <h3 class="modal-title">Order ${escapeHTMLText(orderID)}</h3>
        <button type="button" class="modal-close" aria-label="Close">×</button>
      </div>
      <div class="modal-body">
        <div class="oms-detail-summary">
          <div><span class="stat-label">Status</span><div>${omsStatusBadge(status, order.hold_reason)}</div></div>
          <div><span class="stat-label">Source</span><div>${escapeHTMLText(order.channel || 'Manual')}${order.channel_order_id ? `<div class="oms-channel-ref">${escapeHTMLText(order.channel_order_id)}</div>` : ''}</div></div>
          <div><span class="stat-label">Customer</span><div>${escapeHTMLText(order.customer_name || '—')}${order.customer_phone ? `<div class="oms-channel-ref">${escapeHTMLText(order.customer_phone)}</div>` : ''}</div></div>
          <div><span class="stat-label">Payment</span><div>${escapeHTMLText(order.payment_status || '—')}</div></div>
          <div><span class="stat-label">Priority</span><div>${escapeHTMLText(order.priority || 'Normal')}</div></div>
          <div><span class="stat-label">Value</span><div>${formatMoney(order.total_amount)}</div></div>
        </div>
        <div class="oms-detail-address"><span class="stat-label">Ship to</span><div>${escapeHTMLText(order.shipping_address || '—')}</div></div>

        <div class="oms-action-bar">
          ${status === 'On Hold' ? `<button class="btn btn-primary btn-sm" data-action="release">Release hold</button>` : `<button class="btn btn-outline btn-sm" data-action="hold"${terminal ? ' disabled' : ''}>Hold</button>`}
          <button class="btn btn-outline btn-sm" data-action="edit"${terminal ? ' disabled' : ''}>Edit</button>
          <button class="btn btn-outline btn-sm" data-action="reallocate"${terminal ? ' disabled' : ''}>Reallocate</button>
          <button class="btn btn-outline btn-sm" data-action="switch"${terminal ? ' disabled' : ''}>Switch facility</button>
          <button class="btn btn-outline btn-sm" data-action="priority"${terminal ? ' disabled' : ''}>${order.priority === 'Expedite' ? 'Set Normal' : 'Expedite'}</button>
          <button class="btn btn-outline btn-sm" data-action="split"${terminal ? ' disabled' : ''}>Split selected lines</button>
          <button class="btn btn-outline btn-sm" data-action="cancel"${terminal ? ' disabled' : ''}>Cancel order</button>
        </div>
        ${terminal ? `<p class="text-muted oms-detail-empty">This order is ${escapeHTMLText(status)}, so its actions are closed. A tenant can reopen any of them by configuring a StatusTransitionRule.</p>` : ''}

        <h4 class="oms-detail-heading">Lines <span class="text-muted">(${(d.lines || []).length})</span></h4>
        <div class="table-wrapper"><table>
          <thead><tr><th>Line</th><th>SKU</th><th class="num">Qty</th><th class="num">Unit price</th><th>Allocated to</th><th>Status</th><th>Actions</th></tr></thead>
          <tbody>${lineRows || `<tr><td colspan="7" class="text-center text-muted">No lines.</td></tr>`}</tbody>
        </table></div>

        ${section('Reservations', d.reservations || [], [{ key: 'sku', label: 'SKU' }, { key: 'location_code', label: 'Location' }, { key: 'quantity', label: 'Qty' }, { key: 'reservation_type', label: 'Type' }, { key: 'expires_at', label: 'Expires' }])}
        ${section('Fulfillment tasks', d.fulfillment_tasks || [], [{ key: 'id', label: 'Task' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Location' }, { key: 'created_at', label: 'Created' }])}
        ${section('Shipments', d.shipments || [], [{ key: 'id', label: 'Booking' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'AWB' }, { key: 'created_at', label: 'Created' }])}
        ${section('Invoices', d.invoices || [], [{ key: 'id', label: 'Invoice' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Amount' }, { key: 'created_at', label: 'Created' }])}
        ${section('Returns', d.returns || [], [{ key: 'id', label: 'Return' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Type' }, { key: 'created_at', label: 'Created' }])}
        ${section('Refunds', d.refunds || [], [{ key: 'id', label: 'Refund' }, { key: 'status', label: 'Status' }, { key: 'detail', label: 'Mode' }, { key: 'created_at', label: 'Created' }])}
        ${section('Notifications', d.notifications || [], [{ key: 'id', label: 'Log' }, { key: 'status', label: 'Dispatch' }, { key: 'detail', label: 'Event' }, { key: 'created_at', label: 'At' }])}
        ${section('Audit trail', d.audit_trail || [], [{ key: 'created_at', label: 'At' }, { key: 'user_id', label: 'User' }, { key: 'action', label: 'Action' }, { key: 'status', label: 'Result' }, { key: 'details', label: 'Detail' }])}
      </div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.modal-footer .btn-secondary').addEventListener('click', close);

  const after = async () => {
    close();
    await Promise.all([loadOMSOrders(), loadOMSTiles()]);
  };

  overlay.querySelectorAll('[data-line-hold]').forEach(btn => btn.addEventListener('click', async () => {
    const reasonCode = await showCustomPrompt('Active Hold reason-code:', '', 'Hold Line');
    if (reasonCode === null || !reasonCode.trim()) return;
    await omsPost(`/api/v1/order-lines/${encodeURIComponent(btn.getAttribute('data-line-hold'))}/hold`, { reason_code: reasonCode.trim() }, 'Failed to hold this line.', () => openOMSOrderDetail(orderID));
  }));
  overlay.querySelectorAll('[data-line-release]').forEach(btn => btn.addEventListener('click', async () => {
    await omsPost(`/api/v1/order-lines/${encodeURIComponent(btn.getAttribute('data-line-release'))}/release-hold`, {}, 'Failed to release this line.', () => openOMSOrderDetail(orderID));
  }));

  overlay.querySelectorAll('[data-action]').forEach(btn => btn.addEventListener('click', async () => {
    const action = btn.getAttribute('data-action');
    const path = `/api/v1/orders/${encodeURIComponent(orderID)}`;
    if (action === 'release') return omsPost(`${path}/release-hold`, {}, 'Failed to release the hold.', after);
    if (action === 'hold') {
      const reasonCode = await showCustomPrompt('Active Hold reason-code:', '', 'Hold Order');
      if (reasonCode === null || !reasonCode.trim()) return;
      return omsPost(`${path}/hold`, { reason_code: reasonCode.trim() }, 'Failed to hold this order.', after);
    }
    if (action === 'cancel') {
      const reasonCode = await showCustomPrompt('Active Cancellation reason-code:', '', 'Cancel Order');
      if (reasonCode === null || !reasonCode.trim()) return;
      return omsPost(`${path}/cancel`, { reason_code: reasonCode.trim() }, 'Failed to cancel this order.', after);
    }
    if (action === 'reallocate') {
      // An empty location asks the allocation engine to re-plan rather than
      // forcing a node - that is the difference between Reallocate and Switch.
      return omsPost(`${path}/switch-facility`, { location_code: '' }, 'Failed to reallocate this order.', after);
    }
    if (action === 'switch') {
      const location = await showCustomPrompt('Move unpicked lines to which location code?', '', 'Switch Facility');
      if (location === null || !location.trim()) return;
      return omsPost(`${path}/switch-facility`, { location_code: location.trim() }, 'Failed to switch facility.', after);
    }
    if (action === 'priority') {
      const next = order.priority === 'Expedite' ? 'Normal' : 'Expedite';
      return omsPost(`${path}/priority`, { priority: next }, 'Failed to change priority.', after);
    }
    if (action === 'split') {
      const lineIDs = Array.from(overlay.querySelectorAll('.oms-split-line:checked')).map(b => b.getAttribute('data-line'));
      if (lineIDs.length === 0) { showToast('Tick the lines to split out first.'); return; }
      return omsPost(`${path}/split`, { line_ids: lineIDs }, 'Failed to split this order.', after);
    }
    if (action === 'edit') return openOMSOrderEdit(orderID, order, after);
  }));
};

// omsPost is the one place the console's actions POST from, so the
// error-surfacing and refresh behaviour cannot drift between eight buttons.
async function omsPost(url, body, failureMessage, onSuccess) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) { await showApiError(res, failureMessage); return; }
  showToast('Done.');
  if (onSuccess) await onSuccess();
}

// 35.3.2 - the order edit form.
function openOMSOrderEdit(orderID, order, onSaved) {
  document.getElementById('oms-order-edit-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'oms-order-edit-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Edit order ${escapeHTMLText(orderID)}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <form class="modal-body" id="oms-edit-form">
        <div class="form-group"><label class="form-label" for="oms-edit-customer">Customer name</label>
          <input type="text" id="oms-edit-customer" class="form-input" value="${escapeHTMLText(order.customer_name || '')}"></div>
        <div class="form-group"><label class="form-label" for="oms-edit-phone">Customer phone</label>
          <input type="text" id="oms-edit-phone" name="customer_phone" class="form-input" value="${escapeHTMLText(order.customer_phone || '')}"></div>
        <div class="form-group"><label class="form-label" for="oms-edit-ship">Shipping address</label>
          <textarea id="oms-edit-ship" class="form-textarea" rows="2">${escapeHTMLText(order.shipping_address || '')}</textarea></div>
        <div class="form-group"><label class="form-label" for="oms-edit-bill">Billing address</label>
          <textarea id="oms-edit-bill" class="form-textarea" rows="2">${escapeHTMLText(order.billing_address || '')}</textarea></div>
        <div class="form-group"><label class="form-label" for="oms-edit-payment">Payment status</label>
          <select id="oms-edit-payment" class="form-input">
            ${['Pending', 'Confirmed', 'COD'].map(v => `<option value="${v}"${order.payment_status === v ? ' selected' : ''}>${v}</option>`).join('')}
          </select></div>
        <p class="text-muted">Saving re-runs the same address and payment checks a new order goes through. If the edit leaves the order unfulfillable it is placed On Hold with a reason rather than saved silently broken.</p>
      </form>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-edit-cancel>Cancel</button>
        <button type="button" class="btn btn-primary" data-edit-save>Save changes</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('[data-edit-cancel]').addEventListener('click', close);
  decorateFieldFormats(overlay);
  overlay.querySelector('[data-edit-save]').addEventListener('click', async () => {
    const payload = {
      customer_name: document.getElementById('oms-edit-customer').value,
      customer_phone: document.getElementById('oms-edit-phone').value,
      shipping_address: document.getElementById('oms-edit-ship').value,
      billing_address: document.getElementById('oms-edit-bill').value,
      payment_status: document.getElementById('oms-edit-payment').value
    };
    const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(orderID)}/edit`, { method: 'POST', body: JSON.stringify(payload) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to save this edit.'); return; }
    close();
    showToast('Order updated.');
    if (onSaved) await onSaved();
  });
}

// ---------------------------------------------------------------------------
// Manual order entry (Stage 40.6)
//
// POST /api/v1/orders has existed since the Order Engine was built and is the
// same entry point every channel import ends up at (engines/channel_orders.go's
// ImportChannelSalesOrder maps a payload and then calls CreateSalesOrder, and
// ImportUnicommerceSalesOrder goes through that). What was missing was any way
// to reach it by hand - so a phone order, a walk-in wholesale order or a
// replacement order had no path in the UI at all.
//
// Deliberately posts to that same endpoint rather than creating a SalesOrder
// through the generic doc API: allocation, reservation, hold evaluation and
// idempotency all live behind CreateSalesOrder, and a hand-made document
// would skip every one of them. A manual order is a real order and has to go
// down the same road as a Myntra one.
// ---------------------------------------------------------------------------
let manualOrderLines = [{ sku: '', qty: '', unit_price: '' }];

function renderManualOrderPanel(panel) {
  if (!panel) return;
  panel.innerHTML = `
    <div class="po-composer-head">
      <h2>New manual order</h2>
      <span style="font-size:12px;color:var(--text-muted);">Goes through the same Order Engine as a channel import &mdash; allocation, reservations and holds all apply.</span>
    </div>
    <div class="po-header-grid">
      <div class="form-group">
        <label class="form-label" for="mo-customer">Customer name</label>
        <input type="text" id="mo-customer" class="form-input" placeholder="Who the order is for">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-phone">Customer phone</label>
        <input type="text" id="mo-phone" name="customer_phone" class="form-input">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-channel">Source</label>
        <input type="text" id="mo-channel" class="form-input" value="Manual" title="Recorded on the order so this screen can show where it came from.">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-ref">Reference <span class="po-optional">optional</span></label>
        <input type="text" id="mo-ref" class="form-input" placeholder="Your own order/PO reference" title="Stored as the channel order id. Re-sending the same reference returns the existing order instead of creating a duplicate.">
      </div>
      <div class="form-group">
        <label class="form-label" for="mo-payment">Payment</label>
        <select id="mo-payment" class="form-input">
          <option value="Confirmed">Confirmed (paid)</option>
          <option value="Pending">Pending</option>
          <option value="COD">Cash on delivery</option>
        </select>
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="mo-address">Shipping address</label>
      <textarea id="mo-address" class="form-textarea" rows="2" placeholder="Full delivery address including PIN code"></textarea>
    </div>

    <div class="po-lines-head">
      <h3>Items</h3>
      <button class="btn btn-outline btn-sm" id="mo-add-line" type="button">+ Add item</button>
    </div>
    <div class="table-wrapper">
      <table class="po-lines">
        <thead><tr><th style="min-width:200px;">Item</th><th class="num" style="width:90px;">Qty</th><th class="num" style="width:130px;">Unit price</th><th style="width:36px;"></th></tr></thead>
        <tbody id="mo-lines-body"></tbody>
      </table>
    </div>
    <div class="po-footer">
      <div class="po-total-hint" id="mo-hint">The order appears in the table below the moment it is created, with its source and reference shown.</div>
      <div class="po-actions">
        <div id="mo-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="mo-create">Create Order</button>
      </div>
    </div>
  `;
  renderManualOrderLines();
  document.getElementById('mo-add-line').addEventListener('click', () => {
    manualOrderLines.push({ sku: '', qty: '', unit_price: '' });
    renderManualOrderLines();
  });
  document.getElementById('mo-create').addEventListener('click', createManualOrder);
  decorateFieldFormats(panel);
}

function renderManualOrderLines() {
  const body = document.getElementById('mo-lines-body');
  if (!body) return;
  body.innerHTML = manualOrderLines.map((l, i) => `
    <tr data-mo-line="${i}">
      <td><input type="text" class="form-input" data-mo-field="sku" value="${escapeHTMLText(l.sku)}" placeholder="Search item..."></td>
      <td><input type="number" class="form-input num" data-mo-field="qty" min="1" step="1" value="${escapeHTMLText(l.qty)}"></td>
      <td><input type="number" class="form-input num" data-mo-field="unit_price" min="0" step="0.01" value="${escapeHTMLText(l.unit_price)}"></td>
      <td><button type="button" class="po-line-remove" data-mo-remove="${i}" aria-label="Remove line ${i + 1}">&times;</button></td>
    </tr>`).join('');

  body.querySelectorAll('[data-mo-line]').forEach(tr => {
    const i = Number(tr.getAttribute('data-mo-line'));
    attachLinkTypeahead(tr.querySelector('[data-mo-field="sku"]'), 'Item');
    tr.querySelectorAll('[data-mo-field]').forEach(input => {
      const field = input.getAttribute('data-mo-field');
      const commit = () => { manualOrderLines[i][field] = input.value.trim(); };
      input.addEventListener('change', commit);
      input.addEventListener('blur', commit);
    });
  });
  body.querySelectorAll('[data-mo-remove]').forEach(btn => {
    btn.addEventListener('click', () => {
      manualOrderLines.splice(Number(btn.getAttribute('data-mo-remove')), 1);
      if (manualOrderLines.length === 0) manualOrderLines.push({ sku: '', qty: '', unit_price: '' });
      renderManualOrderLines();
    });
  });
}

async function createManualOrder() {
  const errorEl = document.getElementById('mo-error');
  errorEl.classList.add('hidden');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };

  const address = document.getElementById('mo-address').value.trim();
  if (!address) { fail('A shipping address is required - the Order Engine needs somewhere to ship to.'); return; }

  const lines = manualOrderLines
    .filter(l => l.sku || l.qty)
    .map(l => ({ sku: l.sku, qty: Number(l.qty) || 0, unit_price: Number(l.unit_price) || 0 }));
  if (lines.length === 0) { fail('Add at least one item.'); return; }
  const bad = lines.findIndex(l => !l.sku || l.qty <= 0);
  if (bad !== -1) { fail(`Line ${bad + 1} needs an item and a quantity of at least 1.`); return; }

  const res = await apiFetch('/api/v1/orders', {
    method: 'POST',
    body: JSON.stringify({
      channel: document.getElementById('mo-channel').value.trim() || 'Manual',
      channel_order_id: document.getElementById('mo-ref').value.trim(),
      customer_name: document.getElementById('mo-customer').value.trim(),
      customer_phone: document.getElementById('mo-phone').value.trim(),
      shipping_address: address,
      payment_status: document.getElementById('mo-payment').value,
      lines
    })
  });
  if (!res) return;
  if (!res.ok) { fail(await getErrorMessage(res, 'Failed to create the order.')); return; }

  const data = await res.json();
  manualOrderLines = [{ sku: '', qty: '', unit_price: '' }];
  await showCustomAlert(`Order ${data.order_id} created. It is now in the Orders table below with its fulfillment, shipment and invoice state, exactly like a channel order.`, 'Order Created');
  renderView('oms');
}

function openOMSDoctype(doctype, id) { currentDoctype = doctype; currentSearchQuery = id; currentTablePage = 1; renderView('doctype-table'); }
async function releaseOMSOrder(id) { const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/release-hold`, { method: 'POST' }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to release order hold.'); return; } renderView('oms'); }
async function cancelOMSOrder(id) { const reasonCode = await showCustomPrompt('Active Cancellation reason-code:', '', 'Cancel Order'); if (reasonCode === null || !reasonCode.trim()) return; const res = await apiFetch(`/api/v1/orders/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: JSON.stringify({ reason_code: reasonCode.trim() }) }); if (!res) return; if (!res.ok) { await showApiError(res, 'Failed to cancel order.'); return; } renderView('oms'); }

// Marketplace settlement + logistics booking screen (Stage 13.7) - both
// MarketplaceSettlement and LogisticsBooking are already real doctypes
// (listed via the generic GET /api/v1/doc/... endpoint, no new backend code
// needed for reading), and reconciliation/booking already work via
// POST /api/v1/marketplace/settlement/reconcile and .../logistics/book.
async function renderMarketplaceView(container) {
  const [settlementsRes, bookingsRes, manifestsRes] = await Promise.all([
    apiFetch('/api/v1/doc/MarketplaceSettlement'),
    apiFetch('/api/v1/doc/LogisticsBooking'),
    apiFetch('/api/v1/doc/Manifest')
  ]);
  if (!settlementsRes || !bookingsRes || !manifestsRes) return;

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
  const manifests = manifestsRes.ok ? await manifestsRes.json() : [];

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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No settlements yet. Use <b>Reconcile</b> above once a marketplace payout file is available.</td></tr>`
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

  // --- Logistics bookings panel (Stage 26.12.4: serviceability-driven AWB
  // assignment - Carrier/Tracking Number are now optional, auto-resolved by
  // engines.CreateLogisticsBooking off the CourierServiceArea master when a
  // Destination Pincode is given) ---
  const bookingPanel = document.createElement('div');
  bookingPanel.className = 'table-panel';
  bookingPanel.style.padding = '24px';
  bookingPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Logistics Bookings</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-order-id">Order ID</label>
        <input type="text" id="mkt-order-id" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-fulfillment-task-id">Fulfillment Task (optional)</label>
        <input type="text" id="mkt-fulfillment-task-id" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-pincode">Destination Pincode</label>
        <input type="text" id="mkt-pincode" class="form-input" style="width: 120px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-carrier">Carrier (blank = auto)</label>
        <input type="text" id="mkt-carrier" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-tracking">Tracking Number (optional)</label>
        <input type="text" id="mkt-tracking" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-shipping-charge">Shipping Charge</label>
        <input type="number" id="mkt-shipping-charge" class="form-input" style="width: 110px;">
      </div>
      <button class="btn btn-primary" id="mkt-book-btn">Book</button>
    </div>
    <div id="mkt-booking-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>Booking ID</th>
          <th>Order ID</th>
          <th>Carrier</th>
          <th>AWB Number</th>
          <th>Pincode</th>
          <th>Manifest</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        ${bookings.length === 0
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No logistics bookings yet. Use <b>Book</b> above to book a shipment with a courier.</td></tr>`
          : bookings.map(b => `
            <tr>
              <td style="font-family: monospace;">${b.code || b.id}</td>
              <td>${b.order_id || ''}</td>
              <td>${b.carrier || ''}</td>
              <td style="font-family: monospace;">${b.awb_number || ''}</td>
              <td>${b.destination_pincode || ''}</td>
              <td>${b.manifest_id || ''}</td>
              <td><span class="badge ${b.status === 'RTO' ? 'badge-danger' : b.status === 'Delivered' ? 'badge-success' : 'badge-secondary'}">${b.status}</span></td>
              <td>${renderLogisticsBookingActions(b)}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
    </div>
  `;
  container.appendChild(bookingPanel);

  // --- Manifests panel (Stage 26.12.4) ---
  const manifestPanel = document.createElement('div');
  manifestPanel.className = 'table-panel';
  manifestPanel.style.padding = '24px';
  manifestPanel.style.marginTop = '24px';
  manifestPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Manifests</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-top: -12px; margin-bottom: 16px;">Groups every AWB-assigned shipment for one courier at one location. Handing over a manifest dispatches its fulfillment tasks and, once every task on an order has shipped, flips the order to Shipped.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 16px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-manifest-courier">Courier</label>
        <input type="text" id="mkt-manifest-courier" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mkt-manifest-location">Location Code</label>
        <input type="text" id="mkt-manifest-location" class="form-input" style="width: 150px;">
      </div>
      <button class="btn btn-primary" id="mkt-manifest-btn">Generate Manifest</button>
    </div>
    <div id="mkt-manifest-error" class="login-error hidden" style="margin-bottom: 16px;"></div>
    <table>
      <thead>
        <tr>
          <th>Manifest ID</th>
          <th>Courier</th>
          <th>Location</th>
          <th>Shipments</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        ${manifests.length === 0
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No manifests yet. Use <b>Generate Manifest</b> above once shipments have been booked.</td></tr>`
          : manifests.map(m => `
            <tr>
              <td style="font-family: monospace;">${m.code || m.id}</td>
              <td>${m.courier || ''}</td>
              <td>${m.location_code || ''}</td>
              <td>${m.shipment_count ?? 0}</td>
              <td><span class="badge ${m.status === 'Handed Over' ? 'badge-success' : 'badge-warning'}">${m.status}</span></td>
              <td>${m.status === 'Open' ? `<button class="action-btn" onclick="handoverManifest('${m.code || m.id}')">Hand Over</button>` : ''}</td>
            </tr>
          `).join('')}
      </tbody>
    </table>
  `;
  container.appendChild(manifestPanel);

  document.getElementById('mkt-reconcile-btn').addEventListener('click', submitMarketplaceReconcile);
  document.getElementById('mkt-book-btn').addEventListener('click', submitLogisticsBooking);
  document.getElementById('mkt-manifest-btn').addEventListener('click', submitGenerateManifest);
  attachLinkTypeahead(document.getElementById('mkt-manifest-location'), 'Location');
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

// Stage 26.12.4: Carrier and Tracking Number are now optional - a blank
// Carrier auto-selects the top-priority courier serviceable for Destination
// Pincode (engines.CheckCourierServiceability), and a blank Tracking Number
// defaults to the generated AWB number. Only Order ID and Destination
// Pincode are required (a pincode is needed either way, to validate an
// explicit carrier or to auto-select one).
async function submitLogisticsBooking() {
  const errorEl = document.getElementById('mkt-booking-error');
  errorEl.classList.add('hidden');

  const orderId = document.getElementById('mkt-order-id').value.trim();
  const fulfillmentTaskId = document.getElementById('mkt-fulfillment-task-id').value.trim();
  const pincode = document.getElementById('mkt-pincode').value.trim();
  const carrier = document.getElementById('mkt-carrier').value.trim();
  const trackingNumber = document.getElementById('mkt-tracking').value.trim();
  const shippingCharge = parseFloat(document.getElementById('mkt-shipping-charge').value) || 0;

  if (!orderId || !pincode) {
    errorEl.textContent = 'Order ID and Destination Pincode are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/logistics/book', {
    method: 'POST',
    body: JSON.stringify({
      order_id: orderId,
      fulfillment_task_id: fulfillmentTaskId,
      destination_pincode: pincode,
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

// renderLogisticsBookingActions (Stage 26.12.4) shows the tracking-sync/RTO
// actions valid from a booking's current Shipment-engine status - a label
// is always viewable once AWB-assigned; In-Transit/Delivered/RTO only apply
// once the shipment has actually been handed over to the courier (a
// Manifested-but-not-yet-Handed-Over booking has nothing to track yet).
function renderLogisticsBookingActions(b) {
  const id = b.code || b.id;
  const status = b.status;
  // 31.1.9: "Print Label" is the one-click path (server picks the printer
  // whose Default For is Shipping Label, and a thermal unit gets a real
  // Code 128 AWB rather than digits); "Label" stays as the on-screen read,
  // and is also what Print falls back to when QZ Tray is not running.
  const buttons = [
    `<button class="action-btn" onclick="printShippingLabel('${id}')">Print Label</button>`,
    `<button class="action-btn" onclick="viewShippingLabel('${id}')">Label</button>`
  ];
  if (status === 'Handed Over') {
    buttons.push(`<button class="action-btn" onclick="recordShipmentTracking('${id}', 'In-Transit')">Mark In-Transit</button>`);
  }
  if (status === 'Handed Over' || status === 'In-Transit') {
    buttons.push(`<button class="action-btn" onclick="recordShipmentTracking('${id}', 'Delivered')">Mark Delivered</button>`);
    buttons.push(`<button class="action-btn action-btn-danger" onclick="reportShipmentRTO('${id}')">Report RTO</button>`);
  }
  return buttons.join(' ');
}

async function submitGenerateManifest() {
  const errorEl = document.getElementById('mkt-manifest-error');
  errorEl.classList.add('hidden');

  const courier = document.getElementById('mkt-manifest-courier').value.trim();
  const locationCode = document.getElementById('mkt-manifest-location').value.trim();
  if (!courier || !locationCode) {
    errorEl.textContent = 'Courier and Location Code are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/marketplace/logistics/manifest', {
    method: 'POST',
    body: JSON.stringify({ courier, location_code: locationCode })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'No AWB-assigned shipments found for that courier/location.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('marketplace');
}

window.handoverManifest = async function(manifestId) {
  const confirmed = await showCustomConfirm(`Hand over manifest ${manifestId} to the courier? This dispatches every fulfillment task in it and may flip the parent order(s) to Shipped.`, 'Hand Over Manifest');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/marketplace/logistics/manifest/handover', {
    method: 'POST',
    body: JSON.stringify({ manifest_id: manifestId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to hand over manifest.');
    return;
  }
  renderView('marketplace');
};

window.recordShipmentTracking = async function(bookingId, status) {
  const res = await apiFetch('/api/v1/marketplace/logistics/tracking', {
    method: 'POST',
    body: JSON.stringify({ booking_id: bookingId, status })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to update tracking status.');
    return;
  }
  renderView('marketplace');
};

window.reportShipmentRTO = async function(bookingId) {
  const reason = await showCustomPrompt(`Reason the courier is returning booking ${bookingId} undelivered:`, '', 'Report RTO');
  if (!reason) return;
  const res = await apiFetch('/api/v1/marketplace/logistics/rto', {
    method: 'POST',
    body: JSON.stringify({ booking_id: bookingId, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record RTO.');
    return;
  }
  renderView('marketplace');
};

// printShippingLabel (Stage 31.1.9) sends a booking's label straight to the
// bench's label printer. The payload is built server-side from the booking
// itself (engines.BuildShippingLabelPayload), so no label data passes
// through the browser and a thermal printer gets ZPL with a scannable AWB.
//
// Falls back to the on-screen label rather than to window.print(): the
// plain-text label is not a printable sheet, and someone whose QZ Tray is
// down still needs to read the AWB off the screen to write the docket.
window.printShippingLabel = async function(bookingId) {
  if (await qzTryPrint('Shipping Label', { documentRef: bookingId, quiet: true })) return;
  await viewShippingLabel(bookingId);
};

// viewShippingLabel (Stage 26.12.4) shows GenerateShippingLabel's plain-text
// label in a lightweight read-only modal, the same .modal-overlay/
// .modal-container primitives viewPickList already uses.
window.viewShippingLabel = async function(bookingId) {
  const res = await apiFetch(`/api/v1/marketplace/logistics/label?booking_id=${encodeURIComponent(bookingId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load the shipping label.');
    return;
  }
  const label = await res.text();

  document.getElementById('shipping-label-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'shipping-label-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Shipping Label: ${bookingId}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><pre style="white-space: pre-wrap; font-family: monospace; font-size: 13px;">${label}</pre></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.modal-footer .btn-secondary').addEventListener('click', close);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
};

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

// ---------------------------------------------------------------------------
// Purchase Orders screen (Stage 13.8's maker side, rebuilt in Stage 40.1).
//
// This screen used to ask for a vendor, a warehouse and one hand-typed
// "Total Amount", and posted items: '[]'. A PO therefore recorded what it
// cost but never what was being bought - so GRN receipt had nothing to match,
// the GST engine had nothing to classify, and there was nothing to send a
// vendor. It now edits real lines.
//
// Everything derived is derived server-side, by design: HSN, GST rate,
// per-line tax and the inter-state decision all come back from
// /api/v1/procurement/purchase-order/preview, which runs the same engine
// functions the save path runs. Nothing here recomputes tax in JavaScript,
// because a second implementation is exactly how a screen ends up showing a
// total the saved document disagrees with.
// ---------------------------------------------------------------------------

// The PO being composed or amended. Module-level rather than passed around so
// the line editor, the preview debounce and the save handler all read one
// object - the same shape the API takes, so saving is a POST of this plus the
// serialised lines and no field-by-field assembly.
let poDraft = null;
let poPreview = null;
let poPreviewTimer = null;

function newPODraft() {
  return {
    id: '', vendor: '', target_warehouse: '', location: '',
    // '' means "inherit the tenant default", which the first preview response
    // resolves and displays - the screen never hardcodes Exclusive itself.
    gst_mode: '',
    interstate: false, interstate_override: false,
    lines: [{ sku: '', qty: '', rate: '', mrp: '' }],
    wasApproved: false, version: null
  };
}

async function renderPurchaseOrdersView(container) {
  const res = await apiFetch('/api/v1/doc/PurchaseOrder');
  if (!res) return;

  if (!poDraft) poDraft = newPODraft();

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Purchase Order</h1>
      <p class="page-subtitle">Pick items, set purchase prices, then submit for approval. GST and the supply type are worked out for you.</p>
    </div>
  `;
  container.appendChild(header);

  const ordersLoadFailed = !res.ok;
  const orders = res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel po-composer';
  formPanel.id = 'po-composer';
  container.appendChild(formPanel);
  renderPOComposer(formPanel);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  let html = ordersLoadFailed
    ? `<p style="padding: 16px; color: var(--danger-color); font-size: 13px;">Failed to load existing purchase orders.</p>`
    : '';
  html += `
    <div class="table-wrapper">
    <table>
      <thead>
        <tr>
          <th>PO Number</th>
          <th>Vendor</th>
          <th>Location</th>
          <th>Items</th>
          <th class="num">Taxable</th>
          <th class="num">Grand Total</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;
  if (orders.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No purchase orders yet. Add a vendor and at least one item above, then <b>Create Draft</b>.</td></tr>`;
  }
  orders.forEach(po => {
    const statusBadge = po.status === 'Approved' ? 'badge-success'
      : po.status === 'Rejected' ? 'badge-danger'
      : po.status === 'Pending Approval' ? 'badge-warning'
      : 'badge-secondary';
    let lineCount = 0;
    try { lineCount = (JSON.parse(po.items || '[]') || []).length; } catch (e) { lineCount = 0; }
    const poNumber = po.po_number || po.code || po.id;
    // Sent-to-vendor is shown next to the status rather than as its own
    // column: it is the answer to "did this actually go out?", which is only
    // ever asked about a PO that is already approved.
    const sent = po.sent_to_vendor_at
      ? `<div class="po-sent-stamp" title="Sent ${escapeHTMLText(po.sent_to_vendor_at)}">Sent to vendor</div>` : '';
    html += `
      <tr>
        <td style="font-family: monospace;">${copyableCell(escapeHTMLText(poNumber), poNumber)}</td>
        <td>${escapeHTMLText(po.vendor || '')}</td>
        <td>${escapeHTMLText(po.location || '')}</td>
        <td>${lineCount === 0 ? '<span class="po-no-lines" title="This PO was raised before line items existed, or was created through the API without them.">No lines</span>' : `${lineCount} item${lineCount === 1 ? '' : 's'}`}</td>
        <td class="num">${formatMoney(po.total_amount)}</td>
        <td class="num">${po.grand_total != null ? formatMoney(po.grand_total) : '<span class="text-muted">&mdash;</span>'}</td>
        <td><span class="badge ${statusBadge}">${escapeHTMLText(po.status || '')}</span>${sent}</td>
        <td class="po-row-actions">
          ${po.status === 'Draft' ? `<button class="action-btn" onclick="submitPOForApproval('${escapeHTMLText(po.id)}')">Submit for Approval</button>` : ''}
          ${po.status !== 'Closed' ? `<button class="action-btn" onclick="amendPurchaseOrder('${escapeHTMLText(po.id)}')">Amend</button>` : ''}
          <button class="action-btn" onclick="printPurchaseOrder('${escapeHTMLText(po.id)}')">Print</button>
          ${po.status !== 'Draft' ? `<button class="action-btn" onclick="sendPurchaseOrderToVendor('${escapeHTMLText(po.id)}')">Send to Vendor</button>` : ''}
        </td>
      </tr>
    `;
  });
  html += `</tbody></table></div>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// formatMoney groups the Indian way (12,34,567.89), matching the printed PO
// and engines/amount_words.go's FormatIndianCurrency. en-IN is a built-in
// locale, so this needs no table of its own.
function formatMoney(v) {
  const n = Number(v);
  if (!isFinite(n)) return '0.00';
  return n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function renderPOComposer(panel) {
  const d = poDraft;
  const editing = !!d.id;
  panel.innerHTML = `
    <div class="po-composer-head">
      <h2>${editing ? `Amend ${escapeHTMLText(d.po_number || d.id)}` : 'New Purchase Order'}</h2>
      ${editing ? `<button class="btn btn-ghost btn-sm" id="po-cancel-edit">Cancel</button>` : ''}
    </div>

    <div class="po-header-grid">
      ${editing ? '' : autoNumberField('PO Number', 'PO', '100%')}
      <div class="form-group">
        <label class="form-label" for="po-vendor">Vendor</label>
        <erp-typeahead id="po-vendor" doctype="Vendor"></erp-typeahead>
      </div>
      <div class="form-group">
        <label class="form-label" for="po-location">Location (billing entity)</label>
        <input type="text" id="po-location" class="form-input" value="${escapeHTMLText(d.location)}">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-warehouse">Target Warehouse (ship to)</label>
        <input type="text" id="po-warehouse" class="form-input" value="${escapeHTMLText(d.target_warehouse)}">
      </div>
      <div class="form-group">
        <label class="form-label" for="po-gst-mode">GST treatment of purchase price</label>
        <select id="po-gst-mode" class="form-input">
          <option value="">Tenant default</option>
          <option value="Exclusive"${d.gst_mode === 'Exclusive' ? ' selected' : ''}>Exclusive &mdash; GST added on top</option>
          <option value="Inclusive"${d.gst_mode === 'Inclusive' ? ' selected' : ''}>Inclusive &mdash; price already has GST</option>
        </select>
      </div>
    </div>

    <div id="po-supply-banner" class="po-supply-banner"></div>

    <div class="po-lines-head">
      <h3>Items</h3>
      <button class="btn btn-outline btn-sm" id="po-add-line" type="button">+ Add item</button>
    </div>
    <div class="table-wrapper">
      <table class="po-lines">
        <thead>
          <tr>
            <th style="min-width:190px;">Item</th>
            <th class="num" style="width:80px;">Qty</th>
            <th class="num" style="width:120px;">Purchase Price</th>
            <th class="num" style="width:110px;">MRP <span class="po-optional">optional</span></th>
            <th style="width:90px;">HSN</th>
            <th class="num" style="width:70px;">GST %</th>
            <th class="num" style="width:110px;">Taxable</th>
            <th class="num" style="width:100px;">Tax</th>
            <th class="num" style="width:120px;">Line Total</th>
            <th style="width:36px;"></th>
          </tr>
        </thead>
        <tbody id="po-lines-body"></tbody>
      </table>
    </div>

    <div class="po-footer">
      <div id="po-totals" class="po-totals"></div>
      <div class="po-actions">
        <div id="po-form-error" class="login-error hidden"></div>
        <button class="btn btn-primary" id="po-create-btn">${editing ? 'Save Amendment' : 'Create Draft'}</button>
      </div>
    </div>
  `;

  const vendorEl = document.getElementById('po-vendor');
  vendorEl.value = d.vendor || '';
  vendorEl.addEventListener('change', () => { d.vendor = vendorEl.value.trim(); schedulePOPreview(); });
  // <erp-typeahead> fires `change` on pick, but a typed-and-blurred value has
  // to be caught too, or a vendor typed by hand never reaches the preview and
  // the supply type silently stays underived.
  vendorEl.addEventListener('blur', () => { if (vendorEl.value.trim() !== d.vendor) { d.vendor = vendorEl.value.trim(); schedulePOPreview(); } });

  const locEl = document.getElementById('po-location');
  const whEl = document.getElementById('po-warehouse');
  attachLinkTypeahead(locEl, 'Location');
  attachLinkTypeahead(whEl, 'Location');
  locEl.addEventListener('change', () => { d.location = locEl.value.trim(); schedulePOPreview(); });
  locEl.addEventListener('blur', () => { d.location = locEl.value.trim(); schedulePOPreview(); });
  whEl.addEventListener('change', () => { d.target_warehouse = whEl.value.trim(); });
  whEl.addEventListener('blur', () => { d.target_warehouse = whEl.value.trim(); });

  document.getElementById('po-gst-mode').addEventListener('change', (e) => {
    d.gst_mode = e.target.value;
    schedulePOPreview();
  });

  document.getElementById('po-add-line').addEventListener('click', () => {
    d.lines.push({ sku: '', qty: '', rate: '', mrp: '' });
    renderPOLines();
  });
  document.getElementById('po-create-btn').addEventListener('click', savePurchaseOrder);
  const cancelBtn = document.getElementById('po-cancel-edit');
  if (cancelBtn) cancelBtn.addEventListener('click', () => { poDraft = newPODraft(); poPreview = null; renderView('purchase-orders'); });

  renderPOLines();
}

// renderPOLines redraws the line rows and reattaches their handlers.
//
// Full redraw rather than surgical row patching because a line's derived
// columns (HSN, GST %, tax) all change together when the preview comes back,
// and the row count is small enough - a PO with hundreds of lines is a CSV
// import, not something typed here.
function renderPOLines() {
  const body = document.getElementById('po-lines-body');
  if (!body) return;
  const d = poDraft;
  const previewLines = (poPreview && poPreview.lines) || [];

  body.innerHTML = d.lines.map((line, i) => {
    const p = previewLines[i] || {};
    const err = p.error ? `<div class="po-line-error">${escapeHTMLText(p.error)}</div>` : '';
    const derived = (v, suffix = '') => (v === undefined || v === null || v === '' ? '<span class="text-muted">&mdash;</span>' : escapeHTMLText(String(v)) + suffix);
    return `
      <tr data-po-line="${i}"${p.error ? ' class="po-line-flagged"' : ''}>
        <td>
          <input type="text" class="form-input" data-po-field="sku" value="${escapeHTMLText(line.sku)}" placeholder="Search item...">
          ${p.item_name ? `<div class="po-line-name">${escapeHTMLText(p.item_name)}</div>` : ''}
          ${err}
        </td>
        <td><input type="number" class="form-input num" data-po-field="qty" min="1" step="1" value="${escapeHTMLText(line.qty)}"></td>
        <td><input type="number" class="form-input num" data-po-field="rate" min="0" step="0.01" value="${escapeHTMLText(line.rate)}"></td>
        <td><input type="number" class="form-input num" data-po-field="mrp" min="0" step="0.01" value="${escapeHTMLText(line.mrp)}" placeholder="&mdash;"></td>
        <td class="po-derived">${derived(p.hsn_code)}</td>
        <td class="po-derived num">${p.gst_rate ? escapeHTMLText(String(p.gst_rate)) + '%' : (p.tax_treatment && p.tax_treatment !== 'Taxable' ? escapeHTMLText(p.tax_treatment) : '<span class="text-muted">&mdash;</span>')}</td>
        <td class="po-derived num">${p.taxable ? formatMoney(p.taxable) : '<span class="text-muted">&mdash;</span>'}</td>
        <td class="po-derived num">${p.tax_amount ? formatMoney(p.tax_amount) : '<span class="text-muted">&mdash;</span>'}</td>
        <td class="po-derived num po-line-total">${p.line_total ? formatMoney(p.line_total) : '<span class="text-muted">&mdash;</span>'}</td>
        <td><button type="button" class="po-line-remove" data-po-remove="${i}" title="Remove this line" aria-label="Remove line ${i + 1}">&times;</button></td>
      </tr>
    `;
  }).join('');

  body.querySelectorAll('[data-po-line]').forEach(tr => {
    const i = Number(tr.getAttribute('data-po-line'));
    const skuInput = tr.querySelector('[data-po-field="sku"]');
    attachLinkTypeahead(skuInput, 'Item');
    tr.querySelectorAll('[data-po-field]').forEach(input => {
      const field = input.getAttribute('data-po-field');
      const commit = () => {
        const v = input.value.trim();
        if (poDraft.lines[i][field] === v) return;
        poDraft.lines[i][field] = v;
        schedulePOPreview();
      };
      input.addEventListener('change', commit);
      input.addEventListener('blur', commit);
    });
  });

  body.querySelectorAll('[data-po-remove]').forEach(btn => {
    btn.addEventListener('click', () => {
      const i = Number(btn.getAttribute('data-po-remove'));
      poDraft.lines.splice(i, 1);
      if (poDraft.lines.length === 0) poDraft.lines.push({ sku: '', qty: '', rate: '', mrp: '' });
      renderPOLines();
      schedulePOPreview();
    });
  });

  renderPOTotals();
}

// schedulePOPreview debounces the pricing call. 250ms is long enough that
// typing a 6-digit price is one request rather than six, and short enough
// that the totals land before the eye moves to them.
function schedulePOPreview() {
  clearTimeout(poPreviewTimer);
  poPreviewTimer = setTimeout(runPOPreview, 250);
}

async function runPOPreview() {
  const d = poDraft;
  if (!d) return;
  const res = await apiFetch('/api/v1/procurement/purchase-order/preview', {
    method: 'POST',
    body: JSON.stringify(poDraftPayload(d))
  });
  if (!res) return;
  if (!res.ok) {
    // A preview failure is never fatal - the maker can still save and get the
    // authoritative error from the save path. Showing the derived columns as
    // blank is the honest outcome.
    poPreview = null;
    renderPOLines();
    return;
  }
  poPreview = await res.json();
  // The server resolves '' to the tenant default; reflect what it actually
  // chose so the select stops saying "Tenant default" without saying which.
  const modeSel = document.getElementById('po-gst-mode');
  if (modeSel && !d.gst_mode && poPreview.gst_mode) {
    const opt = modeSel.querySelector('option[value=""]');
    if (opt) opt.textContent = `Tenant default (${poPreview.gst_mode})`;
  }
  renderPOLines();
  renderPOSupplyBanner();
}

// poDraftPayload is the one place the draft becomes an API body, so the
// preview call and the save call cannot drift into sending different shapes.
function poDraftPayload(d) {
  const lines = d.lines
    .filter(l => l.sku || l.qty || l.rate)
    .map(l => ({
      sku: l.sku || '',
      qty: Number(l.qty) || 0,
      rate: Number(l.rate) || 0,
      ...(l.mrp !== '' && l.mrp != null ? { mrp: Number(l.mrp) || 0 } : {})
    }));
  return {
    vendor: d.vendor,
    vendor_id: d.vendor,
    target_warehouse: d.target_warehouse,
    location: d.location,
    items: JSON.stringify(lines),
    gst_mode: d.gst_mode,
    interstate: d.interstate,
    interstate_override: d.interstate_override
  };
}

// renderPOSupplyBanner shows what the two addresses decided, and offers the
// override. This is the visible half of "interstate is worked out from the
// addresses": if it cannot be worked out, the banner says exactly which
// master is missing a GSTIN rather than silently defaulting to intra-state.
function renderPOSupplyBanner() {
  const el = document.getElementById('po-supply-banner');
  if (!el) return;
  const pos = poPreview && poPreview.place_of_supply;
  const d = poDraft;

  if (!pos || (!pos.derived && !d.vendor && !d.location)) {
    el.className = 'po-supply-banner po-supply-idle';
    el.innerHTML = `<span>Pick a vendor and a location &mdash; the supply type is worked out from their states.</span>`;
    return;
  }

  if (!pos.derived) {
    el.className = 'po-supply-banner po-supply-warn';
    el.innerHTML = `
      <span><strong>Supply type could not be derived</strong> &mdash; ${escapeHTMLText(pos.reason || 'a state is missing')}.
      Set it manually below, or add the missing GSTIN/state on the master.</span>
      <label class="po-override"><input type="checkbox" id="po-interstate" ${d.interstate ? 'checked' : ''}> Inter-state (IGST)</label>`;
  } else {
    const kind = pos.interstate ? 'Inter-state' : 'Intra-state';
    const tax = pos.interstate ? 'IGST' : 'CGST + SGST';
    el.className = `po-supply-banner ${d.interstate_override ? 'po-supply-warn' : 'po-supply-ok'}`;
    el.innerHTML = `
      <span><strong>${kind} (${tax})</strong> &mdash; vendor in ${escapeHTMLText(pos.vendor_state_label || '?')}, billing entity in ${escapeHTMLText(pos.buyer_state_label || '?')}.</span>
      <label class="po-override"><input type="checkbox" id="po-interstate-override" ${d.interstate_override ? 'checked' : ''}> Override</label>
      ${d.interstate_override ? `<label class="po-override"><input type="checkbox" id="po-interstate" ${d.interstate ? 'checked' : ''}> Inter-state (IGST)</label>` : ''}`;
  }

  const overrideBox = document.getElementById('po-interstate-override');
  if (overrideBox) overrideBox.addEventListener('change', (e) => {
    d.interstate_override = e.target.checked;
    // Seed the manual flag from what was derived, so ticking Override does
    // not flip the tax treatment as a side effect of opening the control.
    if (d.interstate_override && pos.derived) d.interstate = pos.interstate;
    runPOPreview();
  });
  const interBox = document.getElementById('po-interstate');
  if (interBox) interBox.addEventListener('change', (e) => {
    d.interstate = e.target.checked;
    if (pos && pos.derived) d.interstate_override = true;
    runPOPreview();
  });
}

function renderPOTotals() {
  const el = document.getElementById('po-totals');
  if (!el) return;
  const p = poPreview;
  if (!p || !p.breakdown || (!p.breakdown.taxable_amount && !p.grand_total)) {
    el.innerHTML = `<div class="po-total-hint">Add an item to see the tax breakdown.</div>`;
    return;
  }
  const b = p.breakdown;
  const row = (label, value, cls = '') => `<div class="po-total-row ${cls}"><span>${label}</span><span>${formatMoney(value)}</span></div>`;
  const nonTaxable = (b.exempt_amount || 0) + (b.nil_rated_amount || 0) + (b.zero_rated_amount || 0);
  el.innerHTML = `
    ${row('Taxable value', b.taxable_amount)}
    ${nonTaxable ? row('Exempt / nil / zero-rated', nonTaxable) : ''}
    ${b.interstate ? row(`IGST`, b.igst) : row(`CGST`, b.cgst) + row(`SGST`, b.sgst)}
    ${row('Grand total', p.grand_total, 'po-total-grand')}
    <div class="po-total-mode">Prices entered are <strong>${escapeHTMLText(p.gst_mode || '')}</strong> of GST.</div>
  `;
}

async function savePurchaseOrder() {
  const errorEl = document.getElementById('po-form-error');
  errorEl.classList.add('hidden');
  const d = poDraft;
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };

  if (!d.vendor || !d.target_warehouse || !d.location) {
    fail('Vendor, Location and Target Warehouse are all required.');
    return;
  }
  const payload = poDraftPayload(d);
  const lines = JSON.parse(payload.items);
  if (lines.length === 0) {
    fail('Add at least one item - a purchase order with no lines cannot be received against.');
    return;
  }
  const bad = lines.findIndex(l => !l.sku || l.qty <= 0);
  if (bad !== -1) {
    fail(`Line ${bad + 1} needs an item and a quantity of at least 1.`);
    return;
  }
  if (poPreview && poPreview.blocking) {
    fail('One or more lines could not be priced - see the red rows above. Usually the Item is missing its HSN code or GST rate.');
    return;
  }

  if (d.id) {
    if (d.wasApproved && !await showCustomConfirm('This PO is Approved. Amending it will reset it to Pending Approval for re-approval. Continue?', 'Amend Purchase Order')) return;
    if (typeof d.version === 'number') payload.expected_version = d.version;
    payload.status = d.status || 'Draft';
  } else {
    // The PO number is issued server-side from the PO series (Stage 30.6) and
    // is deliberately not sent.
    payload.status = 'Draft';
  }

  const url = d.id ? `/api/v1/doc/PurchaseOrder/${encodeURIComponent(d.id)}` : '/api/v1/doc/PurchaseOrder';
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify(payload) });
  if (!res) return;
  if (!res.ok) {
    fail(await getErrorMessage(res, d.id ? 'Failed to save amendment - someone else may have edited this record, refresh and try again.' : 'Failed to create purchase order.'));
    return;
  }
  if (d.id && d.wasApproved) await showCustomAlert('Purchase order amended. It now requires re-approval.', 'Amend Purchase Order');
  poDraft = newPODraft();
  poPreview = null;
  renderView('purchase-orders');
}

// amendPurchaseOrder loads an existing PO back into the same composer.
//
// Stage 26.3.6 built this as a chain of four showCustomPrompt() dialogs,
// because the screen had no form worth reusing. It does now - and a prompt
// chain could never have edited the lines, which is the thing an amendment is
// usually about.
window.amendPurchaseOrder = async function(poId) {
  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load purchase order for amendment.');
    return;
  }
  const record = await res.json();
  let lines = [];
  try { lines = JSON.parse(record.items || '[]') || []; } catch (e) { lines = []; }

  poDraft = {
    id: poId,
    po_number: record.po_number || record.code || poId,
    vendor: record.vendor || record.vendor_id || '',
    target_warehouse: record.target_warehouse || '',
    location: record.location || '',
    gst_mode: record.gst_mode || '',
    interstate: !!record.interstate,
    interstate_override: !!record.interstate_override,
    lines: lines.length ? lines.map(l => ({
      sku: l.sku || '', qty: l.qty ?? '', rate: l.rate ?? '', mrp: l.mrp ?? ''
    })) : [{ sku: '', qty: '', rate: '', mrp: '' }],
    wasApproved: record.status === 'Approved',
    status: record.status,
    version: typeof record.version === 'number' ? record.version : null
  };
  poPreview = null;
  renderView('purchase-orders');
  runPOPreview();
  const composer = document.getElementById('po-composer');
  if (composer) composer.scrollIntoView({ behavior: 'smooth', block: 'start' });
};

// ---------------------------------------------------------------------------
// Printed PO and vendor dispatch (Stage 40.1)
//
// The payload is assembled server-side (GET .../print) so the sheet does not
// have to fetch the vendor, the legal entity and every item and stitch them
// together itself - and so MRP is stripped before it ever reaches the page.
// ---------------------------------------------------------------------------
// Deliberately no qzTryPrint() attempt first, unlike printSalesInvoice:
// handlers_qz_print.go has no builder for a PO (its job_type switch covers
// Shipping Label / Sticker / Receipt / Invoice), so asking would be a request
// we already know 422s. The browser sheet is the path; a QZ builder is a
// separate piece of work if silent A4 PO printing is ever wanted.
window.printPurchaseOrder = async function(poId) {
  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/print`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to prepare the purchase order for printing.');
    return;
  }
  renderPOPrintSheet(await res.json());
};

function renderPOPrintSheet(po) {
  const area = document.getElementById('invoice-print-area');
  if (!area) return;
  const party = (title, p) => `
    <div class="po-print-party">
      <div class="po-print-party-title">${title}</div>
      <div class="po-print-party-name">${escapeHTMLText(p.name || '—')}</div>
      ${p.address ? `<div>${escapeHTMLText(p.address)}</div>` : ''}
      ${p.gstin ? `<div>GSTIN: ${escapeHTMLText(p.gstin)}</div>` : ''}
      ${p.state && p.state !== 'Not set' ? `<div>State: ${escapeHTMLText(p.state)}</div>` : ''}
      ${p.email ? `<div>${escapeHTMLText(p.email)}</div>` : ''}
      ${p.phone ? `<div>${escapeHTMLText(p.phone)}</div>` : ''}
    </div>`;

  const b = po.breakdown || {};
  // No MRP column: the server already zeroes it, and the buying side's
  // expected retail price is not the vendor's business.
  area.innerHTML = `
    <div class="invoice-sheet po-print">
      <div class="invoice-title">Purchase Order</div>
      ${po.status !== 'Approved' ? `<div class="invoice-draft">${escapeHTMLText((po.status || '').toUpperCase())}</div>` : ''}
      <div class="po-print-meta">
        <div><strong>PO No:</strong> ${escapeHTMLText(po.po_number || '')}</div>
        ${po.order_date ? `<div><strong>Date:</strong> ${escapeHTMLText(po.order_date)}</div>` : ''}
        ${po.ship_to ? `<div><strong>Ship to:</strong> ${escapeHTMLText(po.ship_to)}</div>` : ''}
      </div>
      <div class="po-print-parties">
        ${party('Buyer', po.buyer || {})}
        ${party('Vendor', po.vendor || {})}
      </div>
      <table class="po-print-lines">
        <thead><tr><th>#</th><th>Item</th><th>HSN</th><th class="num">Qty</th><th class="num">Rate</th><th class="num">GST %</th><th class="num">Taxable</th><th class="num">Amount</th></tr></thead>
        <tbody>
          ${(po.lines || []).map((l, i) => `
            <tr>
              <td>${i + 1}</td>
              <td>${escapeHTMLText(l.sku || '')}${l.item_name ? `<div class="po-print-itemname">${escapeHTMLText(l.item_name)}</div>` : ''}</td>
              <td>${escapeHTMLText(l.hsn_code || '')}</td>
              <td class="num">${escapeHTMLText(String(l.qty ?? ''))}</td>
              <td class="num">${formatMoney(l.rate)}</td>
              <td class="num">${l.gst_rate ? l.gst_rate + '%' : (l.tax_treatment && l.tax_treatment !== 'Taxable' ? escapeHTMLText(l.tax_treatment) : '—')}</td>
              <td class="num">${formatMoney(l.taxable)}</td>
              <td class="num">${formatMoney(l.line_total)}</td>
            </tr>`).join('')}
        </tbody>
      </table>
      <table class="po-print-totals">
        <tr><td>Taxable value</td><td class="num">${formatMoney(b.taxable_amount)}</td></tr>
        ${b.interstate
          ? `<tr><td>IGST</td><td class="num">${formatMoney(b.igst)}</td></tr>`
          : `<tr><td>CGST</td><td class="num">${formatMoney(b.cgst)}</td></tr><tr><td>SGST</td><td class="num">${formatMoney(b.sgst)}</td></tr>`}
        <tr class="po-print-grand"><td>Grand Total</td><td class="num">${formatMoney(po.grand_total)}</td></tr>
      </table>
      ${po.amount_in_words ? `<div class="po-print-words">${escapeHTMLText(po.amount_in_words)}</div>` : ''}
      ${po.place_of_supply && po.place_of_supply.derived ? `<div class="po-print-pos">Place of supply: ${escapeHTMLText(po.place_of_supply.buyer_state_label || '')}</div>` : ''}
      <div class="po-print-foot">
        <div>Prices are <strong>${escapeHTMLText(po.gst_mode || '')}</strong> of GST.</div>
        <div class="po-print-sign">Authorised Signatory</div>
      </div>
    </div>
  `;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// sendPurchaseOrderToVendor records the dispatch and fires the notification
// engine's PurchaseOrderIssued event. When no notification channel is
// configured - the normal state of a tenant that has not set one up - it falls
// back to the user's own mail client rather than dead-ending.
window.sendPurchaseOrderToVendor = async function(poId) {
  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/send`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to send this purchase order.');
    return;
  }
  const data = await res.json();
  const po = data.purchase_order || {};
  if (data.vendor_email) {
    const subject = `Purchase Order ${po.po_number || poId}`;
    const body = [
      `Please find our purchase order ${po.po_number || poId}.`,
      '',
      ...(po.lines || []).map((l, i) => `${i + 1}. ${l.sku}  x${l.qty}  @ ${formatMoney(l.rate)}`),
      '',
      `Grand total: ${formatMoney(po.grand_total)}`,
      po.amount_in_words || ''
    ].join('\n');
    // Opened only after the server has already recorded the dispatch, so the
    // audit trail is written whether or not a mail client actually opens.
    window.location.href = `mailto:${encodeURIComponent(data.vendor_email)}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;
  } else {
    await showCustomAlert('Recorded as sent. This vendor has no contact email on their master record, so nothing could be mailed automatically - add one under Setup > Vendors, or configure a notification channel.', 'Sent to Vendor');
  }
  renderView('purchase-orders');
};

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

// Generic submit-for-approval (Stage 26.8.8/26.8.10) - Appraisal/Grievance
// have no other doctype-specific behavior around this action, so one
// shared function covers both instead of two near-identical copies.
async function submitDocForApproval(doctype, documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype, document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  renderView('doctype-table');
}

async function submitQualityInspectionForApproval(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'QualityInspection', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  renderView('doctype-table');
}

// Stage 26.9.11: SubcontractOrder row actions (Send/Receive).
async function sendSubcontractOrder(id) {
  const res = await apiFetch('/api/v1/manufacturing/subcontract-order/send', {
    method: 'POST',
    body: JSON.stringify({ id })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to send the subcontract order.');
    return;
  }
  showToast('Raw material sent to subcontractor.', { variant: 'success' });
  renderView('doctype-table');
}

async function receiveSubcontractOrder(id, expectedQty) {
  const qtyStr = await showCustomPrompt('Actual quantity received back from the subcontractor:', expectedQty || '', 'Receive Subcontract Order');
  if (qtyStr === null) return;
  const qty = Number(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/subcontract-order/receive', {
    method: 'POST',
    body: JSON.stringify({ id, qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to receive the subcontract order.');
    return;
  }
  showToast('Processed/finished goods received from subcontractor.', { variant: 'success' });
  renderView('doctype-table');
}

// Stage 26.7.9: Customer merge row action - the clicked row is the
// duplicate; the user supplies which customer id it should merge into.
async function mergeCustomerRow(duplicateId) {
  const primaryId = await showCustomPrompt(`Merge customer "${duplicateId}" into which surviving customer id? All their orders, invoices, vouchers, and loyalty points move to that customer.`, '', 'Merge Customer');
  if (primaryId === null) return;
  if (!primaryId.trim() || primaryId.trim() === duplicateId) {
    await showCustomAlert('Enter a different, valid customer id to merge into.', 'Invalid Customer ID');
    return;
  }
  const confirmed = await showCustomConfirm(`This cannot be undone. Merge "${duplicateId}" into "${primaryId.trim()}"?`, 'Confirm Merge');
  if (!confirmed) return;
  const res = await apiFetch('/api/v1/crm/customer/merge', {
    method: 'POST',
    body: JSON.stringify({ primary_customer_id: primaryId.trim(), duplicate_customer_id: duplicateId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to merge customers.');
    return;
  }
  showToast('Customers merged.', { variant: 'success' });
  renderView('doctype-table');
}

// GRN Workbench (Stage 26.3.1). GRN's own registered schema
// (db/migrations_phase3.sql) has always had a mandatory received_items JSON
// field but no screen to fill it in - the only prior path was the generic
// doctype form, and GRN is a Transaction (not Master) doctype so it was
// never even reachable from the Setup submenu that path relies on. This
// posts through the exact same /api/v1/doc/GRN endpoint the (nonexistent)
// generic form would have, so it inherits every existing server-side rule
// for free: the GOODSR-0089/0090 accepted/rejected-qty checks and the
// PURCHA-0082/0084/0086/0087/0088 PO cross-checks (engines/
// transactional_validation.go's validateGRNRules), and the inventory-ledger
// posting hook (internal/server/handlers_core_doc_engine.go). Line shape
// matches validateGRNRules' grnReceivedLine exactly: {sku, qty,
// accepted_qty, rejected_qty, rejection_reason} - qty is the physical
// quantity received (what's checked against the PO's open quantity and
// what actually posts to stock), accepted_qty is auto-derived as qty minus
// rejected_qty rather than asked as a separate input, so it can never
// violate GOODSR-0089's accepted-qty-cannot-exceed-received-qty rule by
// construction. ordered_qty/barcode are carried along for this screen's own
// variance display and label preview - extra keys neither validator nor the
// posting hook look at, so they're harmless to store alongside.
let grnLineItems = [];
// grnLoadedASNId (26.5.1) tracks which ASN, if any, a receipt's lines were
// prefilled from, so createGRN can carry that reference through as GRN's
// new optional asn_id field.
let grnLoadedASNId = '';

async function renderGRNWorkbenchView(container) {
  const res = await apiFetch('/api/v1/doc/GRN');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load goods receipts.', () => renderView('grn')); return; }
  const grns = await res.json();
  state.docData = grns;
  grnLineItems = [];
  grnLoadedASNId = '';

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Goods Receipt</h1>
      <p class="page-subtitle">Receive against a PO: capture accepted, short, and damaged quantities per line, then post to stock.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Goods Receipt</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('GRN Number', 'GRN', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-po">PO Reference</label>
        <input type="text" id="grn-po" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-location">Receiving Location</label>
        <input type="text" id="grn-location" class="form-input" style="width: 130px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="grn-load-po-btn" type="button">Load Items from PO</button>
    </div>
    <div id="grn-po-note" style="margin-top: 8px; font-size: 12.5px; color: var(--text-muted);"></div>

    <!-- Stage 26.5.1: ASN capture ahead of a GRN - an optional prefill
         source alongside the PO one above, same "Load Items from X" pattern. -->
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 12px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-asn">ASN Reference (optional)</label>
        <input type="text" id="grn-asn" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <button class="btn btn-outline" id="grn-load-asn-btn" type="button">Load Items from ASN</button>
    </div>
    <div id="grn-asn-note" style="margin-top: 8px; font-size: 12.5px; color: var(--text-muted);"></div>

    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-sku">SKU</label>
        <input type="text" id="grn-line-sku" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-ordered">Ordered Qty</label>
        <input type="number" id="grn-line-ordered" class="form-input" style="width: 85px;" min="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-received">Received Qty</label>
        <input type="number" id="grn-line-received" class="form-input" style="width: 90px;" min="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-rejected">Rejected Qty</label>
        <input type="number" id="grn-line-rejected" class="form-input" style="width: 85px;" min="0" value="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-reject-reason">Rejection Reason</label>
        <input type="text" id="grn-line-reject-reason" class="form-input" style="width: 150px;" placeholder="required if rejected > 0">
      </div>
      <!-- Stage 26.5.2: QC sampling's third bucket - damaged is now tracked
           separately from rejected instead of one combined field, each with
           its own required reason, and each actually posts to a different
           inventory_availability bucket server-side (PostGRNReceiptWithQC). -->
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-damaged">Damaged Qty</label>
        <input type="number" id="grn-line-damaged" class="form-input" style="width: 85px;" min="0" value="0">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grn-line-damage-reason">Damage Reason</label>
        <input type="text" id="grn-line-damage-reason" class="form-input" style="width: 150px;" placeholder="required if damaged > 0">
      </div>
      <button class="btn btn-outline" id="grn-add-line-btn" type="button">Add Line</button>
    </div>
    <div id="grn-lines-list" style="margin: 12px 0;"></div>
    <div id="grn-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="grn-create-btn">Post Receipt</button>
  `;
  container.appendChild(formPanel);

  attachLinkTypeahead(document.getElementById('grn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('grn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('grn-asn'), 'ASN');
  attachLinkTypeahead(document.getElementById('grn-line-sku'), 'Item');

  document.getElementById('grn-po').addEventListener('change', loadGRNItemsFromPO);
  document.getElementById('grn-load-po-btn').addEventListener('click', loadGRNItemsFromPO);
  document.getElementById('grn-load-asn-btn').addEventListener('click', loadGRNItemsFromASN);
  document.getElementById('grn-add-line-btn').addEventListener('click', addGRNLine);
  document.getElementById('grn-create-btn').addEventListener('click', createGRN);

  renderGRNLinesList();

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead>
        <tr><th>GRN #</th><th>PO</th><th>Location</th><th>Lines</th><th>Received / Rejected / Damaged</th><th>Status</th></tr>
      </thead>
      <tbody>
  `;
  if (grns.length === 0) {
    html += `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No goods receipts yet. Use <b>Load Items from PO</b> above to receive against an approved Purchase Order, then <b>Post Receipt</b>.</td></tr>`;
  }
  grns.forEach(g => {
    let lines = [];
    try { lines = JSON.parse(g.received_items || '[]'); } catch (e) { lines = []; }
    const receivedTotal = lines.reduce((s, l) => s + (Number(l.qty) || 0), 0);
    const rejectedTotal = lines.reduce((s, l) => s + (Number(l.rejected_qty) || 0), 0);
    const damagedTotal = lines.reduce((s, l) => s + (Number(l.damaged_qty) || 0), 0);
    const statusBadge = g.status === 'Approved' ? 'badge-success' : g.status === 'Cancelled' ? 'badge-danger' : 'badge-warning';
    html += `
      <tr>
        <td style="font-family: monospace;">${g.code || g.id}</td>
        <td>${g.po_id || ''}</td>
        <td>${g.location || ''}</td>
        <td>${lines.length}</td>
        <td>${receivedTotal} / ${rejectedTotal} / ${damagedTotal}</td>
        <td><span class="badge ${statusBadge}">${g.status}</span></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);
}

function renderGRNLinesList() {
  const el = document.getElementById('grn-lines-list');
  if (!el) return;
  if (grnLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = `
    <table style="margin-top: 4px;">
      <thead><tr><th>SKU</th><th>Barcode</th><th>Ordered</th><th>Received</th><th>Accepted</th><th>Rejected</th><th>Damaged</th><th>Short</th><th></th></tr></thead>
      <tbody>
        ${grnLineItems.map((line, idx) => {
          const ordered = line.ordered_qty;
          const short = (ordered !== null && ordered !== undefined && ordered !== '') ? Math.max(0, ordered - line.qty) : null;
          return `
            <tr>
              <td style="font-family: monospace;">${line.sku}</td>
              <td><span class="badge badge-secondary" style="font-family: Consolas, Monaco, monospace; letter-spacing: 1px;">${line.barcode || line.sku}</span></td>
              <td>${(ordered === null || ordered === undefined || ordered === '') ? '&mdash;' : ordered}</td>
              <td>${line.qty}</td>
              <td>${line.accepted_qty}</td>
              <td>${line.rejected_qty > 0 ? `<span class="badge badge-danger">${line.rejected_qty}</span>` : '0'}</td>
              <td>${line.damaged_qty > 0 ? `<span class="badge badge-danger">${line.damaged_qty}</span>` : '0'}</td>
              <td>${short === null ? '&mdash;' : short > 0 ? `<span class="badge badge-warning">${short}</span>` : '0'}</td>
              <td><button class="action-btn action-btn-danger" type="button" onclick="removeGRNLine(${idx})">Remove</button></td>
            </tr>
          `;
        }).join('')}
      </tbody>
    </table>
  `;
}

// Best-effort barcode lookup for the line-list preview badge - falls back
// to the SKU itself on any miss, same degrade-gracefully behavior
// engines.PrintStickers already uses server-side for an unregistered SKU.
async function lookupGRNBarcode(sku) {
  const itemRes = await apiFetch(`/api/v1/doc/Item/${encodeURIComponent(sku)}`);
  if (itemRes && itemRes.ok) {
    const item = await itemRes.json();
    return item.barcode || sku;
  }
  return sku;
}

async function addGRNLine() {
  const skuEl = document.getElementById('grn-line-sku');
  const orderedEl = document.getElementById('grn-line-ordered');
  const receivedEl = document.getElementById('grn-line-received');
  const rejectedEl = document.getElementById('grn-line-rejected');
  const reasonEl = document.getElementById('grn-line-reject-reason');
  const damagedEl = document.getElementById('grn-line-damaged');
  const damageReasonEl = document.getElementById('grn-line-damage-reason');
  const errorEl = document.getElementById('grn-form-error');
  errorEl.classList.add('hidden');

  const sku = skuEl.value.trim();
  const ordered = orderedEl.value === '' ? null : parseInt(orderedEl.value, 10);
  const qty = parseInt(receivedEl.value, 10);
  const rejectedQty = parseInt(rejectedEl.value, 10) || 0;
  const rejectionReason = reasonEl.value.trim();
  const damagedQty = parseInt(damagedEl.value, 10) || 0;
  const damageReason = damageReasonEl.value.trim();

  if (!sku || isNaN(qty) || qty < 0) return;
  if (rejectedQty + damagedQty > qty) {
    errorEl.textContent = 'Rejected plus Damaged qty cannot exceed Received qty.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (rejectedQty > 0 && !rejectionReason) {
    errorEl.textContent = 'A rejection reason is required when Rejected qty is greater than 0.';
    errorEl.classList.remove('hidden');
    return;
  }
  if (damagedQty > 0 && !damageReason) {
    errorEl.textContent = 'A damage reason is required when Damaged qty is greater than 0.';
    errorEl.classList.remove('hidden');
    return;
  }

  const barcode = await lookupGRNBarcode(sku);
  grnLineItems.push({
    sku, ordered_qty: ordered, qty,
    accepted_qty: qty - rejectedQty - damagedQty,
    rejected_qty: rejectedQty,
    rejection_reason: rejectionReason,
    damaged_qty: damagedQty,
    damage_reason: damageReason,
    barcode
  });
  skuEl.value = '';
  orderedEl.value = '';
  receivedEl.value = '';
  rejectedEl.value = '0';
  reasonEl.value = '';
  damagedEl.value = '0';
  damageReasonEl.value = '';
  renderGRNLinesList();
}

window.removeGRNLine = function(idx) {
  grnLineItems.splice(idx, 1);
  renderGRNLinesList();
};

// loadGRNItemsFromPO pre-fills lines from the PO's own "items" JSON (Received
// Qty defaulting to the ordered qty - the common case where everything
// ordered showed up intact; the maker adjusts Received/Rejected per line for
// any actual variance before posting). Most POs today still have no item
// lines at all (Stage 26.3.1 audit: the PO create screen only ever saves
// items: '[]', 26.3.6 is the open item to fix that) - that's not an error
// here, just falls through to manual line entry below.
// Reentrancy-guarded: this fires from both the PO field's own 'change'
// event (typeahead pick, or tabbing off a typed value) and the explicit
// "Load Items from PO" button, so a user picking a PO and immediately
// clicking the button (or a script driving both in the same tick) can
// launch two overlapping calls - each does `grnLineItems = []` then awaits
// a barcode lookup per line, so an interleaved second call's reset/pushes
// land on the same shared array the first call resumes into, duplicating
// every line. Caught live while verifying this screen (Playwright triggered
// exactly this sequence), not a theoretical case.
let grnPOLoadInFlight = false;
async function loadGRNItemsFromPO() {
  if (grnPOLoadInFlight) return;
  grnPOLoadInFlight = true;
  try {
    await loadGRNItemsFromPOInner();
  } finally {
    grnPOLoadInFlight = false;
  }
}

async function loadGRNItemsFromPOInner() {
  const poId = document.getElementById('grn-po').value.trim();
  const noteEl = document.getElementById('grn-po-note');
  if (!poId) { noteEl.textContent = ''; return; }

  const res = await apiFetch(`/api/v1/doc/PurchaseOrder/${encodeURIComponent(poId)}`);
  if (!res) return;
  if (!res.ok) {
    noteEl.textContent = 'Could not find that PO - enter lines manually below.';
    return;
  }
  const po = await res.json();

  const locationEl = document.getElementById('grn-location');
  if (!locationEl.value && (po.location || po.target_warehouse)) {
    locationEl.value = po.location || po.target_warehouse;
  }

  let items = [];
  try { items = JSON.parse(po.items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `PO ${poId} has no recorded item lines - add lines manually below.`;
    return;
  }

  grnLineItems = [];
  for (const it of items) {
    const sku = it.sku || it.item_id || '';
    const orderedQty = Number(it.qty) || 0;
    if (!sku) continue;
    const barcode = await lookupGRNBarcode(sku);
    grnLineItems.push({ sku, ordered_qty: orderedQty, qty: orderedQty, accepted_qty: orderedQty, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode });
  }
  grnLoadedASNId = '';
  noteEl.textContent = `Loaded ${grnLineItems.length} line(s) from PO ${poId}. Adjust Received/Rejected/Damaged qty for any variance, then Post Receipt.`;
  renderGRNLinesList();
}

// loadGRNItemsFromASN (26.5.1) mirrors loadGRNItemsFromPOInner exactly, off
// ASN's expected_items instead of a PO's items - the ASN's own po_id is
// used to fill the PO Reference field too if it isn't already set, so the
// GRN still cross-checks against the right PO (validateASNRules already
// confirmed at ASN-creation time that these SKUs belong to that PO).
async function loadGRNItemsFromASN() {
  const asnId = document.getElementById('grn-asn').value.trim();
  const noteEl = document.getElementById('grn-asn-note');
  if (!asnId) { noteEl.textContent = ''; return; }

  const res = await apiFetch(`/api/v1/doc/ASN/${encodeURIComponent(asnId)}`);
  if (!res) return;
  if (!res.ok) {
    noteEl.textContent = 'Could not find that ASN - enter lines manually below.';
    return;
  }
  const asn = await res.json();

  const poEl = document.getElementById('grn-po');
  if (!poEl.value && asn.po_id) poEl.value = asn.po_id;

  let items = [];
  try { items = JSON.parse(asn.expected_items || '[]'); } catch (e) { items = []; }
  if (items.length === 0) {
    noteEl.textContent = `ASN ${asnId} has no recorded expected items - add lines manually below.`;
    return;
  }

  grnLineItems = [];
  for (const it of items) {
    const sku = it.sku || '';
    const expectedQty = Number(it.qty) || 0;
    if (!sku) continue;
    const barcode = await lookupGRNBarcode(sku);
    grnLineItems.push({ sku, ordered_qty: expectedQty, qty: expectedQty, accepted_qty: expectedQty, rejected_qty: 0, rejection_reason: '', damaged_qty: 0, damage_reason: '', barcode });
  }
  grnLoadedASNId = asnId;
  noteEl.textContent = `Loaded ${grnLineItems.length} line(s) from ASN ${asnId}. Adjust Received/Rejected/Damaged qty for any variance, then Post Receipt.`;
  renderGRNLinesList();
}

async function createGRN() {
  const errorEl = document.getElementById('grn-form-error');
  errorEl.classList.add('hidden');

  const poId = document.getElementById('grn-po').value.trim();
  const location = document.getElementById('grn-location').value.trim();

  if (!poId || !location || grnLineItems.length === 0) {
    errorEl.textContent = 'PO Reference, Receiving Location, and at least one line item are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // The GRN number does not exist yet at confirm time - it is issued by the
  // server on save (Stage 30.6) - so this asks about the receipt's effect,
  // which is what actually needs confirming, rather than naming a number the
  // maker typed.
  const totalQty = grnLineItems.reduce((s, l) => s + l.qty, 0);
  if (!(await showCustomConfirm(`Post this goods receipt against ${poId}? ${totalQty} unit(s) will be added to stock at ${location}.`, 'Post Goods Receipt'))) return;

  const res = await apiFetch('/api/v1/doc/GRN', {
    method: 'POST',
    body: JSON.stringify({
      po_id: poId,
      asn_id: grnLoadedASNId || undefined,
      location,
      received_items: JSON.stringify(grnLineItems),
      status: 'Approved'
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to post goods receipt.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('grn');
}

// Stage 26.5.1: ASN (Advance Shipment Notice) capture - a lightweight
// counterpart to the GRN Workbench above (same line-list pattern, just
// sku/expected_qty instead of a full accept/reject/damage split, since an
// ASN is what the vendor SAID is coming, not what actually arrived).
let asnLineItems = [];

async function renderASNView(container) {
  const res = await apiFetch('/api/v1/doc/ASN');
  if (!res) return;
  if (!res.ok) { renderErrorPanel(container, 'Failed to load ASNs.', () => renderView('asn')); return; }
  const asns = await res.json();
  asnLineItems = [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Advance Shipment Notices (ASN)</h1>
      <p class="page-subtitle">Capture what a vendor says is coming, ahead of the actual GRN - the GRN Workbench can prefill its lines from an ASN.</p>
    </div>
  `;
  container.appendChild(header);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  // ASN's asn_number/status/location fields (and the Expected/Received/
  // Cancelled status vocabulary) are the doctype's original, pre-Stage-26.5
  // fields (db/migration.sql) - reused here rather than duplicated, per
  // this repo's "extend the existing doctype, don't build a parallel one"
  // rule. po_id/vendor/carrier/tracking_number/expected_date/expected_items
  // are this Stage's additive fields.
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New ASN</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('ASN Number', 'ASN', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-po">PO Reference</label>
        <input type="text" id="asn-po" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-location">Location</label>
        <input type="text" id="asn-location" class="form-input" style="width: 140px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-vendor">Vendor</label>
        <input type="text" id="asn-vendor" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-carrier">Carrier</label>
        <input type="text" id="asn-carrier" class="form-input" style="width: 130px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-tracking">Tracking Number</label>
        <input type="text" id="asn-tracking" class="form-input" style="width: 150px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-expected-date">Expected Date</label>
        <input type="date" id="asn-expected-date" class="form-input" style="width: 150px;">
      </div>
    </div>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-line-sku">SKU</label>
        <input type="text" id="asn-line-sku" class="form-input" style="width: 150px;" autocomplete="off">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="asn-line-qty">Expected Qty</label>
        <input type="number" id="asn-line-qty" class="form-input" style="width: 100px;" min="1">
      </div>
      <button class="btn btn-outline" id="asn-add-line-btn" type="button">Add Line</button>
    </div>
    <div id="asn-lines-list" style="margin: 12px 0;"></div>
    <div id="asn-form-error" class="login-error hidden" style="margin-bottom: 12px;"></div>
    <button class="btn btn-primary" id="asn-create-btn">Save ASN</button>
  `;
  container.appendChild(formPanel);

  attachLinkTypeahead(document.getElementById('asn-po'), 'PurchaseOrder', { valueFields: ['po_number', 'code', 'id'] });
  attachLinkTypeahead(document.getElementById('asn-location'), 'Location');
  attachLinkTypeahead(document.getElementById('asn-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('asn-line-sku'), 'Item');
  document.getElementById('asn-add-line-btn').addEventListener('click', addASNLine);
  document.getElementById('asn-create-btn').addEventListener('click', createASN);
  renderASNLinesList();

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>ASN #</th><th>PO</th><th>Location</th><th>Vendor</th><th>Carrier</th><th>Expected Date</th><th>Lines</th><th>Status</th></tr></thead>
      <tbody>
  `;
  if (asns.length === 0) {
    html += `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No ASNs yet. Use <b>Add Line</b> then <b>Save ASN</b> above to record a shipment your vendor has despatched.</td></tr>`;
  }
  asns.forEach(a => {
    let lines = [];
    try { lines = JSON.parse(a.expected_items || '[]'); } catch (e) { lines = []; }
    html += `
      <tr>
        <td style="font-family: monospace;">${a.asn_number || a.id}</td>
        <td>${a.po_id || a.po_number || ''}</td>
        <td>${a.location || ''}</td>
        <td>${a.vendor || ''}</td>
        <td>${a.carrier || ''}</td>
        <td>${a.expected_date || ''}</td>
        <td>${lines.length}</td>
        <td><span class="badge badge-secondary">${a.status}</span></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);
}

function renderASNLinesList() {
  const el = document.getElementById('asn-lines-list');
  if (!el) return;
  if (asnLineItems.length === 0) {
    el.innerHTML = `<p style="font-size: 13px; color: var(--text-muted);">No lines added yet.</p>`;
    return;
  }
  el.innerHTML = `
    <table style="margin-top: 4px;">
      <thead><tr><th>SKU</th><th>Expected Qty</th><th></th></tr></thead>
      <tbody>
        ${asnLineItems.map((line, idx) => `
          <tr>
            <td style="font-family: monospace;">${line.sku}</td>
            <td>${line.qty}</td>
            <td><button class="action-btn action-btn-danger" type="button" onclick="removeASNLine(${idx})">Remove</button></td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
}

function addASNLine() {
  const skuEl = document.getElementById('asn-line-sku');
  const qtyEl = document.getElementById('asn-line-qty');
  const sku = skuEl.value.trim();
  const qty = parseInt(qtyEl.value, 10);
  if (!sku || isNaN(qty) || qty <= 0) return;
  asnLineItems.push({ sku, qty });
  skuEl.value = '';
  qtyEl.value = '';
  renderASNLinesList();
}

window.removeASNLine = function(idx) {
  asnLineItems.splice(idx, 1);
  renderASNLinesList();
};

async function createASN() {
  const errorEl = document.getElementById('asn-form-error');
  errorEl.classList.add('hidden');

  const poId = document.getElementById('asn-po').value.trim();
  const location = document.getElementById('asn-location').value.trim();
  if (!poId || !location || asnLineItems.length === 0) {
    errorEl.textContent = 'PO Reference, Location, and at least one line item are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  // asn_number is issued server-side from the ASN series (Stage 30.6).
  // po_number stays the referenced PO's number - despite the name it is this
  // ASN's link to its purchase order, not its own identifier.
  const res = await apiFetch('/api/v1/doc/ASN', {
    method: 'POST',
    body: JSON.stringify({
      po_number: poId,
      po_id: poId,
      location,
      vendor: document.getElementById('asn-vendor').value.trim(),
      carrier: document.getElementById('asn-carrier').value.trim(),
      tracking_number: document.getElementById('asn-tracking').value.trim(),
      expected_date: document.getElementById('asn-expected-date').value,
      expected_items: JSON.stringify(asnLineItems),
      status: 'Expected'
    })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to save ASN.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('asn');
}

// Report catalog (Stage 13.11) - Current Stock, Sales Register, Vendor
// Ledger, Payables Ageing, the four reports the gap analysis prioritized.
let currentReportTab = 'exec-dashboard';

const REPORT_TABS = [
  { id: 'exec-dashboard', label: 'Dashboard' },
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

  if (currentReportTab === 'exec-dashboard') {
    await renderExecDashboard(panel);
  } else if (currentReportTab === 'current-stock') {
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

// Stage 26.10.3: role-based executive dashboard - a frontend-only layer
// over the existing ReportDefinition catalog, no new backend endpoint.
// Every card/chart below just calls RunReport (via the same /reports/run/
// path the Report Catalog tab already uses) against a report registered for
// 26.10.1/26.10.5/17.10/26.12.7 - role-based column masking (Stage 20.39)
// and the catalog's own REPORT-0287 "masked" annotation apply for free, so
// a role without full visibility sees a "restricted" fallback on a
// currency-bearing card/chart instead of silently summing a redacted value.
async function fetchReportRows(reportId, params) {
  const qs = params && Object.keys(params).length ? '?' + reportCatalogQueryString(params) : '';
  const res = await apiFetch(`/api/v1/reports/run/${reportId}${qs}`);
  if (!res || !res.ok) return { rows: [], masked: false };
  const body = await res.json();
  return { rows: body.rows || [], masked: body.code === 'REPORT-0287' };
}

function execDashboardOpenReport(reportId) {
  reportCatalogSelectedId = reportId;
  currentReportTab = 'report-catalog';
  renderView('reports');
}

async function renderExecDashboard(panel) {
  panel.innerHTML = `<p style="padding:16px; color:var(--text-muted);">Loading dashboard&hellip;</p>`;

  const [stale, failedSyncs, negStock, sla, salesRows] = await Promise.all([
    fetchReportRows('exception-stale-approvals', {}),
    fetchReportRows('exception-failed-syncs', {}),
    fetchReportRows('exception-negative-stock', {}),
    fetchReportRows('sla-breach', {}),
    fetchReportRows('sales-register', {})
  ]);

  // Sales trend: last 7 calendar days. sale_total is a Sensitive column
  // (Stage 20.39) - if this role sees it redacted, fall back to an order
  // COUNT trend instead of silently summing a masked "•••" string.
  const days = [];
  for (let i = 6; i >= 0; i--) {
    const d = new Date();
    d.setDate(d.getDate() - i);
    days.push(d.toISOString().slice(0, 10));
  }
  const totalsByDay = Object.fromEntries(days.map(d => [d, 0]));
  const countsByDay = Object.fromEntries(days.map(d => [d, 0]));
  let amountsUsable = !salesRows.masked;
  salesRows.rows.forEach(r => {
    const day = String(r.created_at || '').slice(0, 10);
    if (!(day in countsByDay)) return;
    countsByDay[day]++;
    const amt = Number(r.sale_total);
    if (Number.isFinite(amt)) {
      totalsByDay[day] += amt;
    } else {
      amountsUsable = false;
    }
  });
  const trendValues = days.map(d => (amountsUsable ? totalsByDay[d] : countsByDay[d]));
  const trendLabel = amountsUsable ? 'Sales Total (last 7 days)' : 'Orders (last 7 days) — amounts restricted for your role';

  const slaBreached = sla.rows.filter(r => r.breached).length;
  const cards = [
    { label: 'Stale Approvals', value: stale.rows.length, reportId: 'exception-stale-approvals' },
    { label: 'Failed Syncs', value: failedSyncs.rows.length, reportId: 'exception-failed-syncs' },
    { label: 'Negative Stock Flags', value: negStock.rows.length, reportId: 'exception-negative-stock' },
    { label: 'SLA Breaches', value: slaBreached, reportId: 'sla-breach' }
  ];

  panel.innerHTML = `
    <div style="padding: 16px 16px 0;">
      <p style="color: var(--text-muted); font-size: 13px; margin: 0 0 12px;">Exception queues below (Stage 26.10.5) - click a card to drill into that report in the Report Catalog tab.</p>
    </div>
    <div class="dashboard-stats-row" id="exec-dashboard-cards"></div>
    <div style="padding: 20px; border-top: 1px solid var(--border-color);">
      <h3 style="margin: 0 0 12px; font-size: 14px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em;">${trendLabel}</h3>
      <div id="exec-dashboard-trend"></div>
    </div>
  `;

  // Found while live-verifying 30.5.8: Reports is DEFAULT_VIEW, so this runs
  // on every login, and navigating away before its fetches land leaves this
  // container detached - the forEach below then threw an uncaught TypeError
  // on a null appendChild. Guarded the same way renderExecDashboardTrendChart
  // below already guards its own container.
  const cardsRow = document.getElementById('exec-dashboard-cards');
  if (!cardsRow) return;
  cards.forEach(c => {
    const card = document.createElement('div');
    card.className = 'stat-card';
    card.style.cursor = 'pointer';
    card.title = 'Click to open in Report Catalog';
    card.innerHTML = `
      <span class="stat-label">${c.label}</span>
      <span class="stat-val" style="color:${c.value > 0 ? '#dc2626' : '#10b981'};">${c.value}</span>
    `;
    card.addEventListener('click', () => execDashboardOpenReport(c.reportId));
    cardsRow.appendChild(card);
  });

  renderExecDashboardTrendChart(document.getElementById('exec-dashboard-trend'), days, trendValues);
}

// A plain inline-SVG bar chart - no charting library (this codebase stays
// vanilla JS/CSS, no new frontend dependency), just enough for a 7-day
// at-a-glance trend.
function renderExecDashboardTrendChart(container, labels, values) {
  if (!container) return;
  const width = 640, height = 180, padding = 28;
  const maxVal = Math.max(1, ...values);
  const slotWidth = (width - padding * 2) / values.length;
  const barWidth = Math.max(4, slotWidth - 10);
  const bars = values.map((v, i) => {
    const barHeight = (v / maxVal) * (height - padding * 2);
    const x = padding + i * slotWidth + (slotWidth - barWidth) / 2;
    const y = height - padding - barHeight;
    return `
      <rect x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${barWidth.toFixed(1)}" height="${barHeight.toFixed(1)}" fill="var(--primary-color)" rx="3"></rect>
      <text x="${(x + barWidth / 2).toFixed(1)}" y="${height - padding + 16}" font-size="10" fill="var(--text-muted)" text-anchor="middle">${labels[i].slice(5)}</text>
      <text x="${(x + barWidth / 2).toFixed(1)}" y="${(y - 4).toFixed(1)}" font-size="10" fill="var(--text-main)" text-anchor="middle">${Math.round(v)}</text>
    `;
  }).join('');
  container.innerHTML = `<svg viewBox="0 0 ${width} ${height}" style="width:100%; max-width:${width}px; height:${height}px;">${bars}</svg>`;
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
      ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No stock on hand anywhere yet. Stock appears once a Goods Receipt is posted against a Purchase Order &mdash; see <b>Procurement &raquo; Goods Receipt</b>.</td></tr>`
      : filtered.map(r => `
          <tr>
            <td style="font-family: monospace;">${copyableCell(r.sku, r.sku)}</td>
            <td>${copyableCell(r.location_code, r.location_code)}</td>
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
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No stock on hand yet. Stock appears once a Goods Receipt is posted against a Purchase Order.</td></tr>`
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No completed sales yet. Sales appear here as soon as a cart is checked out at <b>Point of Sale</b>.</td></tr>`
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
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No purchase orders yet. Raise one under <b>Procurement &raquo; Purchase Order</b>; this ledger follows each one through receipt and payment.</td></tr>`
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
      Buckets Approved-but-not-yet-Paid sales invoices (Finance &gt; Sales Invoice) by age since creation.
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
    // Stage 26.6.11: the non-taxable row only renders when there is something
    // in it. Most tenants sell nothing exempt, and three permanent zeroes
    // would be noise on the one report where every figure is meant to be a
    // number someone files.
    const nonTaxable = s.non_taxable_value || 0;
    const nonTaxableRow = nonTaxable === 0 ? '' : `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Exempt Value</span><span class="stat-val">${(s.exempt_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Nil-Rated Value</span><span class="stat-val">${(s.nil_rated_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Zero-Rated Value</span><span class="stat-val">${(s.zero_rated_value || 0).toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Total Non-Taxable</span><span class="stat-val">${nonTaxable.toLocaleString()}</span></div>
      </div>
    `;
    resultEl.innerHTML = `
      <div class="dashboard-stats-row">
        <div class="stat-card"><span class="stat-label">Taxable Value</span><span class="stat-val">${s.taxable_value.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output CGST</span><span class="stat-val">${s.output_cgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output SGST</span><span class="stat-val">${s.output_sgst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Output IGST</span><span class="stat-val">${s.output_igst.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Total Tax Liability</span><span class="stat-val">${s.total_tax_liability.toLocaleString()}</span></div>
        <div class="stat-card"><span class="stat-label">Transactions</span><span class="stat-val">${s.transaction_count}</span></div>
      </div>
      ${nonTaxableRow}
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
      <div class="form-group" style="margin-bottom: 0; min-width: 180px;">
        <label class="form-label" for="rc-column-profile">Column Profile</label>
        <select id="rc-column-profile" class="form-select"><option value="">— Default columns —</option></select>
      </div>
      <button class="btn btn-primary" id="rc-run-btn">Run</button>
      <button class="btn btn-outline" id="rc-columns-btn">Columns</button>
      <button class="btn btn-outline" id="rc-save-filter-btn">Save Filter</button>
      <button class="btn btn-outline" id="rc-export-btn">Export in Background</button>
    </div>
    <div id="rc-columns-panel" class="hidden" style="padding: 12px 16px 0;"></div>
    <div id="rc-export-status" style="padding: 0 16px;"></div>
    <div id="rc-results" style="padding: 16px;"></div>
  `;

  document.getElementById('rc-report-select').addEventListener('change', (e) => {
    reportCatalogSelectedId = e.target.value;
    renderReportCatalogParams();
    loadReportCatalogSavedFilters();
    initReportCatalogColumns();
    loadReportColumnProfiles();
    document.getElementById('rc-columns-panel').classList.add('hidden');
  });
  document.getElementById('rc-run-btn').addEventListener('click', runReportCatalogReport);
  document.getElementById('rc-columns-btn').addEventListener('click', toggleReportColumnsPanel);
  document.getElementById('rc-save-filter-btn').addEventListener('click', saveReportCatalogFilter);
  document.getElementById('rc-export-btn').addEventListener('click', exportReportCatalogReport);
  document.getElementById('rc-saved-filter').addEventListener('change', (e) => {
    applyReportCatalogSavedFilter(e.target.value);
  });
  document.getElementById('rc-column-profile').addEventListener('change', (e) => {
    applyReportColumnProfile(e.target.value);
  });

  renderReportCatalogParams();
  initReportCatalogColumns();
  await loadReportCatalogSavedFilters();
  await loadReportColumnProfiles();
}

function currentReportCatalogDef() {
  return reportCatalogDefs.find(d => d.id === reportCatalogSelectedId);
}

// ---- Report column control + profiles (Stage 28.3) ----
// A column chooser (show/hide + reorder) on the report catalog, plus saveable
// column profiles in two scopes: Personal (only the owner sees it) and Universal
// (shared with everyone; creatable only by privileged roles - also enforced
// server-side in handlers_core_doc_engine.go). Reuses the generic
// ReportColumnProfile doctype the same way saved filters reuse ReportFilterPreset.
let reportCatalogColumnState = []; // [{key, label, visible}] in display order, current report
let reportColumnProfiles = [];     // ReportColumnProfile docs for the current report
let reportCatalogLastResult = null;
let reportCatalogLastParams = {};

function initReportCatalogColumns() {
  const def = currentReportCatalogDef();
  reportCatalogColumnState = (def && def.columns ? def.columns : []).map(c => ({ key: c.key, label: c.label, visible: true }));
}

// effectiveReportColumns maps the current show/hide/order state onto the
// server-returned column set (which carries the authoritative label + any
// sensitive masking). Falls back to the server set if state is empty or would
// hide everything.
function effectiveReportColumns(resultColumns) {
  if (!reportCatalogColumnState.length) return resultColumns;
  const byKey = {};
  resultColumns.forEach(c => { byKey[c.key] = c; });
  const ordered = reportCatalogColumnState.filter(s => s.visible && byKey[s.key]).map(s => byKey[s.key]);
  return ordered.length ? ordered : resultColumns;
}

function toggleReportColumnsPanel() {
  const panel = document.getElementById('rc-columns-panel');
  if (!panel) return;
  if (panel.classList.contains('hidden')) {
    renderReportColumnsPanel();
    panel.classList.remove('hidden');
  } else {
    panel.classList.add('hidden');
  }
}

function renderReportColumnsPanel() {
  const panel = document.getElementById('rc-columns-panel');
  if (!panel) return;
  const role = localStorage.getItem('erp_role') || '';
  const canUniversal = role === 'Super Admin' || role === 'HR/Admin' || role === 'Store Manager';
  const smallBtn = 'padding:2px 8px; font-size:12px;';
  const rows = reportCatalogColumnState.map((c, i) => `
    <div style="display:flex; align-items:center; gap:8px; padding:4px 0;">
      <input type="checkbox" data-col-key="${cfgEsc(c.key)}" ${c.visible ? 'checked' : ''}>
      <span style="flex:1;">${cfgEsc(c.label)}</span>
      <button class="btn btn-outline rc-col-up" data-idx="${i}" style="${smallBtn}" ${i === 0 ? 'disabled' : ''}>&uarr;</button>
      <button class="btn btn-outline rc-col-down" data-idx="${i}" style="${smallBtn}" ${i === reportCatalogColumnState.length - 1 ? 'disabled' : ''}>&darr;</button>
    </div>`).join('');
  panel.innerHTML = `
    <div style="padding:14px; border:1px solid var(--border-color); border-radius:8px; background:var(--panel-bg); max-width:460px;">
      <div style="font-weight:600; margin-bottom:6px;">Show, hide, and reorder columns</div>
      ${rows || '<p class="page-subtitle" style="margin:0;">This report has no columns.</p>'}
      <div style="display:flex; gap:8px; margin-top:14px; flex-wrap:wrap;">
        <button class="btn btn-primary" id="rc-col-apply" style="${smallBtn}">Apply</button>
        <button class="btn btn-outline" id="rc-col-save" style="${smallBtn}">Save as Profile&hellip;</button>
        <button class="btn btn-outline" id="rc-col-reset" style="${smallBtn}">Reset to default</button>
      </div>
    </div>`;
  panel.querySelectorAll('[data-col-key]').forEach(cb => {
    cb.addEventListener('change', () => {
      const st = reportCatalogColumnState.find(s => s.key === cb.getAttribute('data-col-key'));
      if (st) st.visible = cb.checked;
    });
  });
  panel.querySelectorAll('.rc-col-up').forEach(b => b.addEventListener('click', () => moveReportColumn(parseInt(b.dataset.idx, 10), -1)));
  panel.querySelectorAll('.rc-col-down').forEach(b => b.addEventListener('click', () => moveReportColumn(parseInt(b.dataset.idx, 10), 1)));
  document.getElementById('rc-col-apply').addEventListener('click', applyReportColumnState);
  document.getElementById('rc-col-reset').addEventListener('click', () => {
    initReportCatalogColumns();
    renderReportColumnsPanel();
    const sel = document.getElementById('rc-column-profile'); if (sel) sel.value = '';
    applyReportColumnState();
  });
  document.getElementById('rc-col-save').addEventListener('click', () => saveReportColumnProfile(canUniversal));
}

function moveReportColumn(idx, dir) {
  const j = idx + dir;
  if (j < 0 || j >= reportCatalogColumnState.length) return;
  const arr = reportCatalogColumnState;
  const tmp = arr[idx]; arr[idx] = arr[j]; arr[j] = tmp;
  renderReportColumnsPanel();
}

// Re-render the already-fetched result with the current column state, without
// re-running the report.
function applyReportColumnState() {
  const resultsEl = document.getElementById('rc-results');
  if (resultsEl && reportCatalogLastResult) {
    renderReportCatalogResultTable(resultsEl, reportCatalogLastResult, reportCatalogLastParams);
  }
}

async function loadReportColumnProfiles() {
  const select = document.getElementById('rc-column-profile');
  if (!select) return;
  select.innerHTML = `<option value="">— Default columns —</option>`;
  const res = await apiFetch('/api/v1/doc/ReportColumnProfile');
  reportColumnProfiles = (res && res.ok) ? await res.json() : [];
  const username = localStorage.getItem('erp_username') || '';
  reportColumnProfiles
    .filter(p => p.report_id === reportCatalogSelectedId && (p.scope === 'Universal' || p.owner === username))
    .forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = (p.scope === 'Universal' ? '🌐 ' : '') + p.name;
      select.appendChild(opt);
    });
}

function applyReportColumnProfile(profileId) {
  const def = currentReportCatalogDef();
  if (!def) return;
  if (!profileId) { initReportCatalogColumns(); applyReportColumnState(); return; }
  const p = reportColumnProfiles.find(x => x.id === profileId);
  if (!p) return;
  let saved = [];
  try { saved = JSON.parse(p.columns || '[]'); } catch (e) { /* ignore */ }
  const labelByKey = {};
  (def.columns || []).forEach(c => { labelByKey[c.key] = c.label; });
  // Honor the profile's saved order/visibility for columns that still exist,
  // then append any columns added to the report since the profile was saved.
  const seen = new Set();
  const state = [];
  saved.forEach(c => {
    if (labelByKey[c.key] !== undefined && !seen.has(c.key)) {
      state.push({ key: c.key, label: labelByKey[c.key], visible: c.visible !== false });
      seen.add(c.key);
    }
  });
  (def.columns || []).forEach(c => {
    if (!seen.has(c.key)) state.push({ key: c.key, label: c.label, visible: true });
  });
  reportCatalogColumnState = state;
  const panel = document.getElementById('rc-columns-panel');
  if (panel && !panel.classList.contains('hidden')) renderReportColumnsPanel();
  applyReportColumnState();
}

async function saveReportColumnProfile(canUniversal) {
  const def = currentReportCatalogDef();
  if (!def) return;
  const name = await showCustomPrompt('Name this column profile:', '', 'Save Column Profile');
  if (!name) return;
  let scope = 'Personal';
  if (canUniversal) {
    scope = (await showCustomConfirm('Save as a Universal profile, shared with everyone? Choose Cancel to keep it Personal (visible only to you).'))
      ? 'Universal' : 'Personal';
  }
  const username = localStorage.getItem('erp_username') || '';
  const columns = reportCatalogColumnState.map(c => ({ key: c.key, visible: c.visible }));
  const id = `RCP-${Date.now()}`;
  const res = await apiFetch('/api/v1/doc/ReportColumnProfile', {
    method: 'POST',
    body: JSON.stringify({ id, report_id: def.id, name, owner: username, scope, columns: JSON.stringify(columns), status: 'Active' })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to save column profile.'); return; }
  showToast(`Column profile saved (${scope}).`, { variant: 'success' });
  await loadReportColumnProfiles();
  const sel = document.getElementById('rc-column-profile'); if (sel) sel.value = id;
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
  reportCatalogLastResult = result;
  reportCatalogLastParams = params;
  const columns = effectiveReportColumns(result.columns || []);
  const rows = result.rows || [];
  const drillKey = columns.length > 0 ? columns[0].key : null;
  // Stage 36.2.6: any report row that names a product can put that product in
  // someone's inbox. Keyed off the presence of an item_code column rather than
  // a hardcoded list of report ids, so every present and future PIM readiness
  // report gets the affordance without being enumerated here - which is the
  // whole reason the report catalog describes its columns as data.
  const assignKey = columns.some(c => c.key === 'item_code') ? 'item_code' : null;
  const canAssign = assignKey !== null && typeof canCreateDoctype === 'function' && canCreateDoctype('PIMTask');
  const extraCols = (result.has_drill_down ? 1 : 0) + (canAssign ? 1 : 0);
  let html = `<table><thead><tr>`;
  columns.forEach(c => { html += `<th>${c.label}</th>`; });
  if (result.has_drill_down) html += `<th>Details</th>`;
  if (canAssign) html += `<th>Task</th>`;
  html += `</tr></thead><tbody>`;
  if (rows.length === 0) {
    html += `<tr><td colspan="${columns.length + extraCols}" style="text-align:center; color:var(--text-muted);">No rows matched. Widen the date range or clear a filter above, then run the report again.</td></tr>`;
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
    if (canAssign) {
      const itemCode = row[assignKey] === null || row[assignKey] === undefined ? '' : String(row[assignKey]);
      html += itemCode
        ? `<td><button class="action-btn" onclick='openPIMAssignTaskModal(${JSON.stringify(itemCode)}, ${JSON.stringify(result.label || result.id)})'>Assign task</button></td>`
        : `<td></td>`;
    }
    html += `</tr><tr id="rc-drilldown-${idx}" class="hidden"><td colspan="${columns.length + extraCols}"></td></tr>`;
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
    ? `<tr><td colspan="${keys.length || 1}" style="text-align:center; color:var(--text-muted);">No underlying rows for this figure &mdash; it is a computed total with no individual transactions behind it in the selected period.</td></tr>`
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
  const savedFilterSelect = document.getElementById('rc-saved-filter');
  if (savedFilterSelect) savedFilterSelect.value = presetId;
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
      ${autoNumberField('RFQ Number', 'RFQ', '160px')}
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No RFQs yet. Use <b>Create RFQ</b> above to invite quotes from your vendors.</td></tr>`
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

  const description = document.getElementById('rfq-description').value.trim();
  const quantity = parseFloat(document.getElementById('rfq-quantity').value) || 0;
  const targetDate = document.getElementById('rfq-target-date').value;

  if (!description || !quantity) {
    errorEl.textContent = 'Description and Quantity are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/RFQ', {
    method: 'POST',
    body: JSON.stringify({ description, quantity, target_date: targetDate, status: 'Draft' })
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
        ${autoNumberField('Quote Number', 'QTN', '160px')}
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
          ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No quotes submitted yet. Use <b>Submit Quote</b> above to record a vendor's response, then <b>Select as Winner</b>.</td></tr>`
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
  if (quoteVendorInput) attachLinkTypeahead(quoteVendorInput, 'Vendor');
}

async function submitVendorQuote(rfqId) {
  const errorEl = document.getElementById('quote-form-error');
  errorEl.classList.add('hidden');

  const vendor = document.getElementById('quote-vendor').value.trim();
  const quotedPrice = parseFloat(document.getElementById('quote-price').value);
  const leadTime = parseFloat(document.getElementById('quote-lead-time').value) || 0;

  if (!vendor || !quotedPrice) {
    errorEl.textContent = 'Vendor and Quoted Price are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/VendorQuote', {
    method: 'POST',
    body: JSON.stringify({
      rfq_id: rfqId, vendor,
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

// QZ Tray silent printing (Stage 31.1).
//
// Every print path in this app used to be window.print() into a hidden
// @media print area, which pops the browser dialog and cannot choose a
// printer. These helpers route a job to a *named* OS printer instead, so a
// packing bench with a thermal label printer and an A4 invoice printer sends
// each document to the right one with a single click.
//
// Degrades rather than breaks: if QZ Tray is not installed or not running,
// qzTryPrint returns false and the caller falls back to the existing print
// sheet. Nobody is blocked from printing because the bridge is down.

let qzLastError = '';

async function qzTryConnect(silent = true) {
  if (!window.QZPrint) return false;
  try {
    await window.QZPrint.connect();
    qzLastError = '';
    return true;
  } catch (err) {
    qzLastError = err.message || String(err);
    if (!silent) await showCustomAlert(qzLastError, 'QZ Tray Not Available');
    return false;
  }
}

// Records what actually reached the printer. Best-effort: a failed log write
// must never surface as a failed print, because the label is already out.
async function qzLogJob(entry) {
  try {
    await apiFetch('/api/v1/print/qz/log', { method: 'POST', body: JSON.stringify(entry) });
  } catch (err) {
    console.debug('[QZ] print log write failed', err);
  }
}

/**
 * One-click print. Asks the server what to print (and on which printer),
 * hands it to QZ Tray, and records the outcome.
 *
 * @param jobType 'Shipping Label' | 'Sticker' | 'Receipt' | 'Invoice' | 'Document'
 * @param opts    { documentRef, printerCode, copies, skus, reprintReason,
 *                  dataBase64, docFormat, quiet }
 *
 * `quiet` (31.1.9) suppresses the dialog when the server cannot prepare the
 * job, for the call sites that resolve a printer by *role* rather than by an
 * explicit pick. "No Printer record is Default For Receipt" is the normal
 * state of a tenant that has not set QZ up at all - it must fall through to
 * the browser print sheet silently, not put an error in front of the cashier
 * on every sale. A failure at the tray itself is still shown either way:
 * that one means printing was really attempted and really failed.
 *
 * @returns true if it printed, false if the caller should fall back.
 */
async function qzTryPrint(jobType, opts = {}) {
  if (!await qzTryConnect()) return false;

  const res = await apiFetch('/api/v1/print/qz/payload', {
    method: 'POST',
    body: JSON.stringify({
      job_type: jobType,
      document_ref: opts.documentRef || '',
      printer_code: opts.printerCode || '',
      copies: opts.copies || 1,
      skus: opts.skus || [],
      reprint_reason: opts.reprintReason || '',
      data_base64: opts.dataBase64 || '',
      doc_format: opts.docFormat || ''
    })
  });
  if (!res) return false;
  if (!res.ok) {
    if (opts.quiet) {
      console.debug('[QZ] falling back to the browser sheet:', await getErrorMessage(res, 'no print payload'));
      return false;
    }
    await showApiError(res, 'Could not prepare the print job.', 'Print Failed');
    return false;
  }

  const payload = await res.json();

  // A non-thermal sticker printer has no raw command form; the server says
  // so and the existing browser sheet renders it instead.
  if (payload.fallback === 'browser') {
    renderPrintSheet(payload.labels, opts.copies || 1);
    return true;
  }

  const printerName = (payload.printer && payload.printer.qz_printer_name) || '';
  if (!printerName) {
    if (opts.quiet) {
      console.debug('[QZ] printer has no OS printer name set; falling back to the browser sheet');
      return false;
    }
    await showCustomAlert(
      `Printer "${(payload.printer && payload.printer.name) || opts.printerCode}" has no OS printer name set. ` +
      'Open Sticker Printing → Print Setup, copy the exact name from the detected list, ' +
      'and paste it into that Printer record\'s "OS Printer Name" field.',
      'Printer Not Mapped');
    return false;
  }

  try {
    await window.QZPrint.printItems(printerName, payload.items, payload.copies);
  } catch (err) {
    await qzLogJob({
      job_type: jobType, document_ref: opts.documentRef || '', printer_code: payload.printer.code,
      qz_printer_name: printerName, print_format: payload.format, copies: payload.copies,
      status: 'Failed', error_detail: err.message || String(err)
    });
    await showCustomAlert(err.message || String(err), 'Print Failed');
    return false;
  }

  await qzLogJob({
    job_type: jobType, document_ref: opts.documentRef || '', printer_code: payload.printer.code,
    qz_printer_name: printerName, print_format: payload.format, copies: payload.copies,
    status: 'Submitted', error_detail: ''
  });
  showToast(`Sent to ${printerName}.`, { variant: 'success' });
  return true;
}

/**
 * Prints a marketplace-issued label or invoice exactly as the channel
 * produced it. Myntra, and every other channel that hands back a finished
 * PDF, goes through here - the file is passed to the printer untouched,
 * because re-rendering a courier's label risks altering a barcode they scan.
 */
async function qzPrintMarketplaceDocument(file, opts = {}) {
  if (!file) return false;
  const base64 = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '');
    reader.onerror = () => reject(new Error('Could not read the file.'));
    reader.readAsDataURL(file);
  });
  const isPDF = /\.pdf$/i.test(file.name) || file.type === 'application/pdf';
  return qzTryPrint('Document', {
    documentRef: opts.documentRef || file.name,
    printerCode: opts.printerCode || '',
    copies: opts.copies || 1,
    dataBase64: base64,
    docFormat: opts.docFormat || (isPDF ? 'pdf' : 'command')
  });
}

// Print Setup panel. Lives on the Sticker Printing screen because that is
// already where Printer records are managed. Its job is the one thing the
// generic Master form cannot do: ask the local machine what its printers are
// actually called, since "OS Printer Name" has to match verbatim (a Zebra
// commonly reports as e.g. "ZDesigner ZD220-203dpi ZPL").
function renderQZSetupPanel(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '20px';
  panel.style.marginBottom = '24px';
  panel.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap;">
      <div>
        <h2 style="margin: 0 0 4px; font-size: 16px;">Print Setup (QZ Tray)</h2>
        <p id="qz-status" class="page-subtitle" style="margin: 0;">Checking for QZ Tray on this PC&hellip;</p>
      </div>
      <div style="display: flex; gap: 8px; flex-wrap: wrap;">
        <button class="btn btn-outline" onclick="qzDetectPrinters()">Detect Printers</button>
        <button class="btn btn-outline" onclick="qzTestPrint()">Test Print</button>
      </div>
    </div>
    <div id="qz-detected" style="margin-top: 12px;"></div>
    <div style="margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border-color, #e5e7eb);">
      <label class="form-label" for="qz-doc-file">Print a marketplace label or invoice (PDF from Myntra, or any channel)</label>
      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
        <input type="file" id="qz-doc-file" class="form-input" accept=".pdf,.zpl,.txt,.prn" style="max-width: 340px;">
        <button class="btn btn-primary" onclick="qzPrintPickedDocument()">Print</button>
      </div>
      <p class="page-subtitle" style="margin: 6px 0 0;">
        Goes to whichever Printer has <strong>Default For = Shipping Label</strong>, exactly as the channel issued it.
      </p>
    </div>
  `;
  container.appendChild(panel);
  qzRefreshStatus();
}

async function qzRefreshStatus() {
  const el = document.getElementById('qz-status');
  if (!el) return;
  const ok = await qzTryConnect();
  if (ok) {
    const v = window.QZPrint.version();
    el.textContent = `Connected to QZ Tray${v ? ' ' + v : ''} - printing is silent on this PC.`;
    el.style.color = 'var(--color-success, #15803d)';
  } else {
    el.textContent = qzLastError + ' Printing falls back to the browser dialog until it is running.';
    el.style.color = 'var(--color-warning, #b45309)';
  }
}

// Lists the OS printer names QZ can see. This is the value the operator
// copies into a Printer record's "OS Printer Name" field.
async function qzDetectPrinters() {
  const out = document.getElementById('qz-detected');
  if (!out) return;
  if (!await qzTryConnect(false)) return;
  out.textContent = 'Detecting...';
  try {
    const names = await window.QZPrint.listOSPrinters();
    const list = Array.isArray(names) ? names : [names];
    if (list.length === 0) {
      out.textContent = 'QZ Tray reports no printers installed on this PC.';
      return;
    }
    let def = '';
    try { def = await window.QZPrint.getDefaultPrinter(); } catch (e) { /* optional */ }
    out.innerHTML = `
      <p class="page-subtitle" style="margin: 0 0 6px;">Copy the exact name into the matching Printer record:</p>
      <ul style="margin: 0; padding-left: 18px; line-height: 1.7;">
        ${list.map(n => `<li><code>${escapeHTMLText(n)}</code>${n === def ? ' <em>(system default)</em>' : ''}</li>`).join('')}
      </ul>`;
  } catch (err) {
    out.textContent = 'Could not list printers: ' + (err.message || err);
  }
}

// Proves the whole chain - certificate, signature, socket, driver - before an
// operator depends on it during a packing run.
async function qzTestPrint() {
  if (!await qzTryConnect(false)) return;
  const printers = await apiFetch('/api/v1/print/qz/printers');
  if (!printers || !printers.ok) {
    await showApiError(printers, 'Could not load configured printers.', 'Test Print');
    return;
  }
  const list = await printers.json();
  const target = list.find(p => p.qz_printer_name);
  if (!target) {
    await showCustomAlert(
      'No Printer record has an OS Printer Name yet. Click "Detect Printers", then edit a Printer ' +
      'under Masters and paste the exact name into "OS Printer Name".', 'Test Print');
    return;
  }
  const raw = ['ZPL', 'TSPL', 'ESC-POS'].includes((target.printer_language || '').toUpperCase());
  const items = raw
    ? [{ type: 'raw', format: 'command', flavor: 'plain',
         data: '^XA^CI28^FO30,30^A0N,40,40^FDERP test label^FS^FO30,90^A0N,28,28^FD' + new Date().toLocaleString() + '^FS^XZ' }]
    : [{ type: 'pixel', format: 'html', flavor: 'plain',
         data: '<h2>ERP test print</h2><p>' + escapeHTMLText(new Date().toLocaleString()) + '</p>' }];
  try {
    await window.QZPrint.printItems(target.qz_printer_name, items, 1);
    showToast(`Test page sent to ${target.qz_printer_name}.`, { variant: 'success' });
  } catch (err) {
    await showCustomAlert(err.message || String(err), 'Test Print Failed');
  }
}

async function qzPrintPickedDocument() {
  const input = document.getElementById('qz-doc-file');
  if (!input || !input.files || input.files.length === 0) {
    await showCustomAlert('Choose a label or invoice file first.', 'Nothing Selected');
    return;
  }
  const printed = await qzPrintMarketplaceDocument(input.files[0]);
  if (printed) input.value = '';
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
  renderQZSetupPanel(container);

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
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No print history yet. Add SKUs above and use <b>Print Stickers</b>.</td></tr>`
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

  // 31.1: prefer the silent path. The server-side payload builder calls the
  // same engines.PrintStickers this endpoint does, so SKU validation, the
  // DEVICE-0298 printer check and the sticker_print_log audit trail run
  // identically either way - only the delivery to the printer differs.
  if (await qzTryPrint('Sticker', { printerCode, copies, skus: stickerSKUs, reprintReason })) {
    stickerSKUs = [];
    renderView('stickers');
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
  { id: 'payroll-export', label: 'Payroll Export' },
  { id: 'roster', label: 'Shift Roster' },
  { id: 'payroll', label: 'Payroll' },
  { id: 'loans', label: 'Loans/Advances' },
  { id: 'onboarding', label: 'Onboarding/Offboarding' },
  { id: 'appraisals', label: 'Appraisals' },
  { id: 'training', label: 'Training' },
  { id: 'grievances', label: 'Grievances' },
  { id: 'my-requests', label: 'My Requests' }
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

  // Stage 30.5.8: the whole Employee list used to be fetched here, on every
  // HR tab render, purely to build <option>s - including for the eight tabs
  // that never showed an employee picker at all. The pickers are typeaheads
  // now and fetch what they need on demand, so the eager list is gone.
  if (currentHRTab === 'attendance') {
    await renderAttendanceTab(container);
  } else if (currentHRTab === 'leave') {
    await renderLeaveTab(container);
  } else if (currentHRTab === 'payroll-export') {
    renderPayrollExportTab(container);
  } else if (currentHRTab === 'roster') {
    currentDoctype = 'ShiftAssignment';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentHRTab === 'payroll') {
    await renderPayrollTab(container);
  } else if (currentHRTab === 'loans') {
    await renderEmployeeLoansTab(container);
  } else if (currentHRTab === 'onboarding') {
    currentDoctype = 'OnboardingChecklist';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentHRTab === 'appraisals') {
    // 26.8.8 (P2, go-ahead 2026-07-27): AppraisalCycle (the KRA/KPI
    // template) is a Master, managed under Setup like every other master -
    // this tab is just the per-employee Appraisal transactions against it.
    currentDoctype = 'Appraisal';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentHRTab === 'training') {
    // TrainingProgram is a Master (managed under Setup); this tab is the
    // per-employee completion records against it.
    currentDoctype = 'TrainingRecord';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentHRTab === 'grievances') {
    currentDoctype = 'Grievance';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentHRTab === 'my-requests') {
    await renderMyRequestsTab(container);
  }
}

// The employee picker, in one place (Stage 30.5.8).
//
// Six screens ask for an employee. Five hand-built a <select> listing every
// employee up front; Asset's custodian used the typeahead that every other
// master picker in this app uses. That split cost more than tidiness - the
// <select> made each of those five screens fetch the entire Employee list on
// load just to build <option>s, and gave no way to search it once a tenant
// has more staff than fit on a screen.
//
// Settled on the typeahead, being the control ~35 other pickers already use,
// with showAllOnFocus (see TYPEAHEAD_DOCTYPE_OPTS) so the one thing the
// <select> genuinely did better - "show me everyone without typing" - is
// still there. Two things this does not lose:
//   - 30.5.1's empty-state guidance. It gets better, in fact: an empty list
//     used to render a static hint underneath, and now focusing the field
//     opens the typeahead's dead-end row, which names the doctype and links
//     straight to where an Employee is created.
//   - validation. employee_id is a Link field, so an id that names no real
//     record is refused server-side with META-0198 whichever control typed
//     it (engines/doctype.go) - the free-text input is not a new hole.
function employeePickerField(id, width = '200px') {
  return `
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="${id}">Employee</label>
        <input type="text" id="${id}" class="form-input" style="width: ${width};"
               placeholder="Search, or click to browse" autocomplete="off">
      </div>`;
}

function attachEmployeePicker(id) {
  attachLinkTypeahead(document.getElementById(id), 'Employee');
}

async function renderAttendanceTab(container) {
  const res = await apiFetch('/api/v1/doc/Attendance');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Mark Attendance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Attendance Code', 'ATT', '160px')}
${employeePickerField('att-employee', '200px')}
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
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No attendance records yet. Pick an employee and a date above, then <b>Save</b>.</td></tr>`
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
  attachEmployeePicker('att-employee');
  attachLinkTypeahead(document.getElementById('att-location'), 'Location');
}

async function saveAttendance() {
  const errorEl = document.getElementById('att-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('att-employee').value;
  const date = document.getElementById('att-date').value;
  const location = document.getElementById('att-location').value.trim();
  const status = document.getElementById('att-status').value;

  if (!employeeId || !date) {
    errorEl.textContent = 'Employee and Date are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Attendance', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, date, location, status })
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

async function renderLeaveTab(container) {
  const res = await apiFetch('/api/v1/doc/Leave');
  const records = res && res.ok ? await res.json() : [];

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Apply Leave</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Leave Code', 'LV', '160px')}
${employeePickerField('leave-employee', '200px')}
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
    ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No leave applications yet. Use <b>Apply</b> above to record one on an employee's behalf.</td></tr>`
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
  attachEmployeePicker('leave-employee');
}

async function saveLeave() {
  const errorEl = document.getElementById('leave-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('leave-employee').value;
  const leaveType = document.getElementById('leave-type').value;
  const fromDate = document.getElementById('leave-from').value;
  const toDate = document.getElementById('leave-to').value;
  const days = parseFloat(document.getElementById('leave-days').value);

  if (!employeeId || !fromDate || !toDate || !days) {
    errorEl.textContent = 'Employee, From/To Date, and Days are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Leave', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, leave_type: leaveType, from_date: fromDate, to_date: toDate, days, status: 'Applied' })
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
    ? `<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No records in this period. Mark attendance for the month under the <b>Attendance</b> tab, then run the export again.</td></tr>`
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

// Stage 26.8.2/26.8.3: Payroll - Run Payroll (attendance + SalaryStructure +
// TDS + active loan deductions -> a Draft Payslip) and Post to GL (requires
// employee bank details on file). SalaryStructure itself is a Master
// doctype, managed under Setup like any other master - this tab is just the
// payroll *run*, not salary-structure maintenance.
async function renderPayrollTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Run Payroll</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">Salary structures are configured under Setup &rarr; Salary Structure.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
${employeePickerField('payroll-run-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-run-from">Period From</label>
        <input type="date" id="payroll-run-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="payroll-run-to">Period To</label>
        <input type="date" id="payroll-run-to" class="form-input">
      </div>
      <button class="btn btn-primary" id="payroll-run-btn">Run Payroll</button>
    </div>
    <div id="payroll-run-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const payslipsRes = await apiFetch('/api/v1/doc/Payslip');
  const payslips = payslipsRes && payslipsRes.ok ? await payslipsRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Payslip</th><th>Employee</th><th>Period</th><th>Gross</th><th>Net Pay</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += payslips.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No payslips yet. Use <b>Run Payroll</b> above &mdash; each employee needs a Salary Structure first.</td></tr>`
    : payslips.map(p => `
        <tr>
          <td style="font-family: monospace;">${p.code || p.id}</td>
          <td>${p.employee_id || ''}</td>
          <td>${p.period_from || ''} to ${p.period_to || ''}</td>
          <td>${p.gross_pay ?? ''}</td>
          <td>${p.net_pay ?? ''}</td>
          <td><span class="badge ${p.status === 'Posted' ? 'badge-success' : 'badge-secondary'}">${p.status}</span></td>
          <td>${p.status === 'Draft' ? `<button class="action-btn" onclick="postPayslipToGL('${p.id}')">Post to GL</button>` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('payroll-run-btn').addEventListener('click', runPayrollForEmployee);
  attachEmployeePicker('payroll-run-employee');
}

async function runPayrollForEmployee() {
  const errorEl = document.getElementById('payroll-run-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('payroll-run-employee').value;
  const periodFrom = document.getElementById('payroll-run-from').value;
  const periodTo = document.getElementById('payroll-run-to').value;
  if (!employeeId || !periodFrom || !periodTo) {
    errorEl.textContent = 'Employee, Period From, and Period To are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/hr/run-payroll', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, period_from: periodFrom, period_to: periodTo })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to run payroll.', 'Payroll Run Failed');
    return;
  }
  renderView('hr');
}

async function postPayslipToGL(payslipId) {
  const confirmed = await showCustomConfirm('This will post the payslip amounts to the GL and mark it Posted. Continue?', 'Post Payslip');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/hr/post-payslip', {
    method: 'POST',
    body: JSON.stringify({ payslip_id: payslipId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post payslip.', 'Posting Failed');
    return;
  }
  renderView('hr');
}

// Stage 26.8.4: Loans/advances against salary. Custom create form + action
// list (not the generic doctype table) since Disburse needs real logic
// (GL posting + initializing outstanding_balance) beyond generic CRUD -
// same reasoning the Manufacturing screen's BOM/Production Order panels
// already established.
async function renderEmployeeLoansTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Loan/Advance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Loan Code', 'LOAN', '160px')}
${employeePickerField('loan-employee', '200px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="loan-principal">Principal Amount</label>
        <input type="number" id="loan-principal" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="loan-monthly">Monthly Deduction</label>
        <input type="number" id="loan-monthly" class="form-input" style="width: 140px;">
      </div>
      <button class="btn btn-primary" id="loan-create-btn">Create Loan</button>
    </div>
    <div id="loan-form-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const loansRes = await apiFetch('/api/v1/doc/EmployeeLoan');
  const loans = loansRes && loansRes.ok ? await loansRes.json() : [];

  const listPanel = document.createElement('div');
  listPanel.className = 'table-panel';
  let html = `
    <table>
      <thead><tr><th>Loan</th><th>Employee</th><th>Principal</th><th>Monthly Deduction</th><th>Outstanding</th><th>Status</th><th></th></tr></thead>
      <tbody>
  `;
  html += loans.length === 0
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No loans yet. Use <b>Create Loan</b> above, then <b>Disburse</b> to post it to the ledger.</td></tr>`
    : loans.map(l => `
        <tr>
          <td style="font-family: monospace;">${l.code || l.id}</td>
          <td>${l.employee_id || ''}</td>
          <td>${l.principal_amount ?? ''}</td>
          <td>${l.monthly_deduction ?? ''}</td>
          <td>${l.outstanding_balance ?? ''}</td>
          <td><span class="badge ${l.status === 'Active' ? 'badge-success' : 'badge-secondary'}">${l.status}</span></td>
          <td>${l.status === 'Draft' ? `<button class="action-btn" onclick="disburseEmployeeLoan('${l.id}')">Disburse</button>` : ''}</td>
        </tr>
      `).join('');
  html += `</tbody></table>`;
  listPanel.innerHTML = html;
  container.appendChild(listPanel);

  document.getElementById('loan-create-btn').addEventListener('click', createEmployeeLoan);
  attachEmployeePicker('loan-employee');
}

async function createEmployeeLoan() {
  const errorEl = document.getElementById('loan-form-error');
  errorEl.classList.add('hidden');

  const employeeId = document.getElementById('loan-employee').value;
  const principal = parseFloat(document.getElementById('loan-principal').value);
  const monthly = parseFloat(document.getElementById('loan-monthly').value);

  if (!employeeId || !principal || !monthly) {
    errorEl.textContent = 'Employee, Principal Amount, and Monthly Deduction are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/EmployeeLoan', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employeeId, principal_amount: principal, monthly_deduction: monthly, status: 'Draft' })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to create loan.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function disburseEmployeeLoan(loanId) {
  const confirmed = await showCustomConfirm('This will post the disbursement to the GL and activate the loan. Continue?', 'Disburse Loan');
  if (!confirmed) return;

  const res = await apiFetch('/api/v1/hr/disburse-loan', {
    method: 'POST',
    body: JSON.stringify({ loan_id: loanId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to disburse loan.', 'Disbursement Failed');
    return;
  }
  renderView('hr');
}

// Stage 26.8.5: Employee self-service - leave request + expense-claim
// submission from the employee's own login. Resolves the current user's
// own Employee record (GET /api/v1/hr/my-employee) so they don't need to
// know their own employee code, then reuses the existing generic doc-create
// endpoints for Leave/ExpenseClaim (the approval flow itself, Stage
// 13.13c, is untouched - this is only about self-initiated submission).
async function renderMyRequestsTab(container) {
  const empRes = await apiFetch('/api/v1/hr/my-employee');
  const empData = empRes && empRes.ok ? await empRes.json() : { employee: null };
  const employee = empData.employee;

  if (!employee) {
    const panel = document.createElement('div');
    panel.className = 'table-panel';
    panel.style.padding = '24px';
    panel.innerHTML = `<p style="color: var(--text-muted);">Your login is not linked to an Employee record, so there is nothing to self-service here. Ask a Super Admin to set the Employee master's "Linked ERP User ID" field.</p>`;
    container.appendChild(panel);
    return;
  }

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">Request Leave</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">Employee: ${employee.code || employee.id} - ${employee.name || ''}</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Leave Code', 'LV', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-type">Leave Type</label>
        <select id="myreq-leave-type" class="form-input" style="width: 130px;">
          <option value="Casual">Casual</option>
          <option value="Sick">Sick</option>
          <option value="Earned">Earned</option>
          <option value="Unpaid">Unpaid</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-from">From Date</label>
        <input type="date" id="myreq-leave-from" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-to">To Date</label>
        <input type="date" id="myreq-leave-to" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-leave-days">Days</label>
        <input type="number" id="myreq-leave-days" class="form-input" style="width: 80px;">
      </div>
      <button class="btn btn-primary" id="myreq-leave-btn">Submit Leave Request</button>
    </div>
    <div id="myreq-leave-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(formPanel);

  const expensePanel = document.createElement('div');
  expensePanel.className = 'table-panel';
  expensePanel.style.padding = '24px';
  expensePanel.style.marginBottom = '24px';
  expensePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Submit Expense Claim</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Claim Number', 'EXP', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-date">Expense Date</label>
        <input type="date" id="myreq-exp-date" class="form-input">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-category">Category</label>
        <select id="myreq-exp-category" class="form-input" style="width: 140px;">
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
        <label class="form-label" for="myreq-exp-amount">Amount</label>
        <input type="number" id="myreq-exp-amount" class="form-input" style="width: 110px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-exp-purpose">Purpose</label>
        <input type="text" id="myreq-exp-purpose" class="form-input" style="width: 180px;">
      </div>
      <button class="btn btn-primary" id="myreq-exp-btn">Submit Expense Claim</button>
    </div>
    <div id="myreq-exp-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(expensePanel);

  // Stage 26.8.10 (P2, go-ahead 2026-07-27): Grievance submission, same
  // self-service shape as Leave/Expense above - create Draft then submit
  // into the existing approval engine (HR/Admin per this doctype's
  // approval_rules row) rather than a new case-management workflow.
  const grievancePanel = document.createElement('div');
  grievancePanel.className = 'table-panel';
  grievancePanel.style.padding = '24px';
  grievancePanel.style.marginBottom = '24px';
  grievancePanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Submit Grievance</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Grievance Number', 'GRV', '160px')}
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="myreq-griev-category">Category</label>
        <select id="myreq-griev-category" class="form-input" style="width: 160px;">
          <option value="Harassment">Harassment</option>
          <option value="Compensation">Compensation</option>
          <option value="Workplace Safety">Workplace Safety</option>
          <option value="Discrimination">Discrimination</option>
          <option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0; flex:1; min-width:220px;">
        <label class="form-label" for="myreq-griev-desc">Description</label>
        <input type="text" id="myreq-griev-desc" class="form-input">
      </div>
      <button class="btn btn-primary" id="myreq-griev-btn">Submit Grievance</button>
    </div>
    <div id="myreq-griev-error" class="login-error hidden" style="margin-top: 16px;"></div>
  `;
  container.appendChild(grievancePanel);

  const [leavesRes, expensesRes, grievancesRes] = await Promise.all([
    apiFetch('/api/v1/doc/Leave'),
    apiFetch('/api/v1/doc/ExpenseClaim'),
    apiFetch('/api/v1/doc/Grievance')
  ]);
  const myLeaves = (leavesRes && leavesRes.ok ? await leavesRes.json() : []).filter(l => l.employee_id === (employee.code || employee.id));
  const myExpenses = (expensesRes && expensesRes.ok ? await expensesRes.json() : []).filter(e => e.employee_id === (employee.code || employee.id));
  const myGrievances = (grievancesRes && grievancesRes.ok ? await grievancesRes.json() : []).filter(g => g.employee_id === (employee.code || employee.id));

  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  historyPanel.innerHTML = `
    <table>
      <thead><tr><th>Type</th><th>Detail</th><th>Status</th></tr></thead>
      <tbody>
        ${myLeaves.map(l => `<tr><td>Leave</td><td>${l.leave_type || ''} ${l.from_date || ''} to ${l.to_date || ''}</td><td><span class="badge badge-secondary">${l.status}</span></td></tr>`).join('')}
        ${myExpenses.map(e => `<tr><td>Expense</td><td>${e.category || ''} ${e.amount ?? ''}</td><td><span class="badge badge-secondary">${e.status}</span></td></tr>`).join('')}
        ${myGrievances.map(g => `<tr><td>Grievance</td><td>${g.category || ''} ${g.description || ''}</td><td><span class="badge badge-secondary">${g.status}</span></td></tr>`).join('')}
        ${myLeaves.length === 0 && myExpenses.length === 0 && myGrievances.length === 0 ? `<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No requests submitted yet. Use <b>Submit Leave Request</b>, <b>Submit Expense Claim</b> or <b>Submit Grievance</b> above.</td></tr>` : ''}
      </tbody>
    </table>
  `;
  container.appendChild(historyPanel);

  document.getElementById('myreq-leave-btn').addEventListener('click', () => submitMyLeaveRequest(employee));
  document.getElementById('myreq-exp-btn').addEventListener('click', () => submitMyExpenseClaim(employee));
  document.getElementById('myreq-griev-btn').addEventListener('click', () => submitMyGrievance(employee));
}

async function submitMyGrievance(employee) {
  const errorEl = document.getElementById('myreq-griev-error');
  errorEl.classList.add('hidden');

  const category = document.getElementById('myreq-griev-category').value;
  const description = document.getElementById('myreq-griev-desc').value.trim();

  if (!description) {
    errorEl.textContent = 'Description is required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const createRes = await apiFetch('/api/v1/doc/Grievance', {
    method: 'POST',
    body: JSON.stringify({ employee_id: employee.code || employee.id, category, description, status: 'Draft' })
  });
  if (!createRes) return;
  const createData = await createRes.json();
  if (!createRes.ok) {
    errorEl.textContent = createData.error || 'Failed to submit grievance.';
    errorEl.classList.remove('hidden');
    return;
  }
  // The grievance number is issued by the server (Stage 30.6), so the routing
  // step below has to use the id it came back with - there is no longer a
  // client-side value that is guaranteed to match the saved document.
  const submitRes = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'Grievance', document_id: createData.id })
  });
  if (!submitRes) return;
  if (!submitRes.ok) {
    errorEl.textContent = await getErrorMessage(submitRes, 'Grievance was saved but could not be routed for HR review.');
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function submitMyLeaveRequest(employee) {
  const errorEl = document.getElementById('myreq-leave-error');
  errorEl.classList.add('hidden');

  const leaveType = document.getElementById('myreq-leave-type').value;
  const fromDate = document.getElementById('myreq-leave-from').value;
  const toDate = document.getElementById('myreq-leave-to').value;
  const days = parseFloat(document.getElementById('myreq-leave-days').value);

  if (!fromDate || !toDate || !days) {
    errorEl.textContent = 'From Date, To Date, and Days are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/Leave', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employee.code || employee.id, leave_type: leaveType,
      from_date: fromDate, to_date: toDate, days, status: 'Applied'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit leave request.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
}

async function submitMyExpenseClaim(employee) {
  const errorEl = document.getElementById('myreq-exp-error');
  errorEl.classList.add('hidden');

  const expenseDate = document.getElementById('myreq-exp-date').value;
  const category = document.getElementById('myreq-exp-category').value;
  const amount = parseFloat(document.getElementById('myreq-exp-amount').value);
  const purpose = document.getElementById('myreq-exp-purpose').value.trim();

  if (!expenseDate || !amount) {
    errorEl.textContent = 'Expense Date and Amount are both required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ExpenseClaim', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employee.code || employee.id, location: employee.location || '',
      expense_date: expenseDate, category, amount, purpose, status: 'Draft'
    })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    errorEl.textContent = data.error || 'Failed to submit expense claim.';
    errorEl.classList.remove('hidden');
    return;
  }
  renderView('hr');
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
    ? `<tr><td colspan="9" style="text-align:center; color:var(--text-muted);">No assets yet. Use <b>Create</b> above to capitalise your first fixed asset.</td></tr>`
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
  attachLinkTypeahead(document.getElementById('asset-location'), 'Location');
  attachLinkTypeahead(document.getElementById('asset-custodian'), 'Employee');
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
// anywhere yet, so - unlike Vendors/POSProfile - this needed a small
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
      <h1 class="page-title">Stock Transfer</h1>
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
      ${autoNumberField('Transfer Number', 'TO', '160px')}
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No transfers yet. Use <b>Add Line</b> then <b>Create Transfer</b> above to move stock between locations.</td></tr>`
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
  attachLinkTypeahead(document.getElementById('transfer-from'), 'Location');
  attachLinkTypeahead(document.getElementById('transfer-to'), 'Location');
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

  const fromWarehouse = document.getElementById('transfer-from').value.trim();
  const toWarehouse = document.getElementById('transfer-to').value.trim();

  if (!fromWarehouse || !toWarehouse || transferLineItems.length === 0) {
    errorEl.textContent = 'From/To Warehouse and at least one line item are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/TransferOrder', {
    method: 'POST',
    body: JSON.stringify({
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
    // gate - Dispatch stays available directly from Approved too. Stage
    // 26.5.8 adds a second pack path that suggests a carton split instead
    // of prompting box-by-box.
    return `<button class="action-btn" onclick="packTransferOrder('${t.id}')">Pack</button> <button class="action-btn" onclick="packTransferOrderWithCartonization('${t.id}')">Pack (Suggested Cartons)</button> <button class="action-btn" onclick="dispatchTransferOrder('${t.id}')">Dispatch</button>`;
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

// Stage 26.5.8: cartonization - suggests a box split via SuggestCartonization
// (first-fit-decreasing by qty capacity) instead of prompting a Box ID per
// line, then confirms the suggestion before packing exactly like the manual
// path above does with its own boxes array.
async function packTransferOrderWithCartonization(id) {
  const row = state.docData.find(t => t.id === id);
  if (!row) return;
  let lines = [];
  try { lines = JSON.parse(row.items || '[]'); } catch (e) { lines = []; }
  if (lines.length === 0) {
    await showCustomAlert('No line items found on this transfer.', 'Error');
    return;
  }
  const cartonType = await showCustomPrompt('Carton Type code to pack into:', 'BOX-S');
  if (!cartonType) return;

  const suggestRes = await apiFetch('/api/v1/wms/cartonization/suggest', {
    method: 'POST',
    body: JSON.stringify({ carton_type: cartonType, items: lines.map(l => ({ sku: l.sku, qty: l.qty })) })
  });
  if (!suggestRes) return;
  if (!suggestRes.ok) {
    await showApiError(suggestRes, 'Failed to suggest cartonization.', 'Cartonization Failed');
    return;
  }
  const boxes = await suggestRes.json();
  const summary = boxes.map(b => `${b.box_id}: ${b.items.map(it => `${it.sku} x${it.qty}`).join(', ')} (${b.used_capacity}/${b.max_capacity})`).join('\n');
  if (!(await showCustomConfirm(`Pack into ${boxes.length} suggested box(es)?\n\n${summary}`, 'Confirm Suggested Cartonization'))) return;

  const packRes = await apiFetch('/api/v1/wms/transfer/pack', {
    method: 'POST',
    body: JSON.stringify({ transfer_order_id: id, boxes: boxes.map(b => ({ box_id: b.box_id, items: b.items })) })
  });
  if (!packRes) return;
  if (!packRes.ok) {
    await showApiError(packRes, 'Failed to pack transfer.', 'Pack Failed');
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
  // 30.5.8: the parallel Employee fetch that used to sit alongside this one
  // only existed to build the picker's <option>s; the picker is a typeahead
  // now and fetches on demand.
  const claimsRes = await apiFetch('/api/v1/doc/ExpenseClaim');
  if (!claimsRes) return;

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

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">New Expense Claim</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      ${autoNumberField('Claim Number', 'EXP', '160px')}
${employeePickerField('exp-employee', '180px')}
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
    ? `<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No expense claims yet. Use <b>Create Draft</b> above to raise one.</td></tr>`
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
  attachEmployeePicker('exp-employee');
  attachLinkTypeahead(document.getElementById('exp-location'), 'Location');
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

  const employeeId = document.getElementById('exp-employee').value;
  const location = document.getElementById('exp-location').value.trim();
  const expenseDate = document.getElementById('exp-date').value;
  const category = document.getElementById('exp-category').value;
  const amount = parseFloat(document.getElementById('exp-amount').value);
  const gstAmount = parseFloat(document.getElementById('exp-gst').value) || 0;
  const advanceAdjusted = parseFloat(document.getElementById('exp-advance').value) || 0;
  const purpose = document.getElementById('exp-purpose').value.trim();

  if (!employeeId || !location || !expenseDate || !amount) {
    errorEl.textContent = 'Employee, Location, Expense Date, and Amount are required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ExpenseClaim', {
    method: 'POST',
    body: JSON.stringify({
      employee_id: employeeId, location, expense_date: expenseDate,
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
let currentMfgTab = 'orders';
const MFG_TABS = [
  { id: 'orders', label: 'Orders' },
  { id: 'quality', label: 'Quality Inspections' },
  { id: 'mrp', label: 'MRP Suggestions' },
  { id: 'schedule', label: 'Production Schedule' },
  { id: 'subcontracting', label: 'Subcontracting' }
];

// Stage 26.9: Manufacturing/MRP Maturity Sprint - Work Centers and Routing
// are Master doctypes (managed under Setup, free generic form/table, same
// as every other master this Stage adds); this view adds a tab bar around
// the existing BOM/Production Order panels for Quality Inspections
// (26.9.7's QC gate, submitted here then approved on the existing
// Approvals inbox) and MRP Suggestions (26.9.5).
async function renderManufacturingView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Manufacturing</h1>
      <p class="page-subtitle">Multi-level BOM, WIP-tracked production orders, QC, and MRP. Work Centers/Routing are managed under Setup.</p>
    </div>
  `;
  container.appendChild(header);

  const tabBar = document.createElement('div');
  tabBar.style.display = 'flex';
  tabBar.style.gap = '8px';
  tabBar.style.marginBottom = '16px';
  tabBar.innerHTML = MFG_TABS.map(t =>
    `<button class="btn ${t.id === currentMfgTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-mfg-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  tabBar.querySelectorAll('[data-mfg-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      currentMfgTab = btn.getAttribute('data-mfg-tab');
      renderView('manufacturing');
    });
  });

  if (currentMfgTab === 'orders') {
    await renderManufacturingOrdersTab(container);
  } else if (currentMfgTab === 'quality') {
    currentDoctype = 'QualityInspection';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  } else if (currentMfgTab === 'mrp') {
    await renderMRPSuggestionsTab(container);
  } else if (currentMfgTab === 'schedule') {
    await renderProductionScheduleTab(container);
  } else if (currentMfgTab === 'subcontracting') {
    currentDoctype = 'SubcontractOrder';
    currentSearchQuery = '';
    currentTablePage = 1;
    await renderDocTableView(container);
  }
}

async function renderManufacturingOrdersTab(container) {
  const [bomsRes, ordersRes] = await Promise.all([
    apiFetch('/api/v1/doc/BOM'),
    apiFetch('/api/v1/doc/ProductionOrder')
  ]);
  if (!bomsRes || !ordersRes) return;

  const boms = bomsRes.ok ? await bomsRes.json() : [];
  const orders = ordersRes.ok ? await ordersRes.json() : [];

  const bomFormPanel = document.createElement('div');
  bomFormPanel.className = 'table-panel';
  bomFormPanel.style.padding = '24px';
  bomFormPanel.style.marginBottom = '24px';
  bomFormPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;">New BOM</h2>
    <p style="color: var(--text-muted); font-size: 13px; margin-bottom: 12px;">For multi-level sub-assemblies, per-line scrap %, by-products, default/effective-dated alternates, QC requirement, or standard cost, edit the BOM under Setup &rarr; BOM after creating it here.</p>
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
      ${autoNumberField('Order Number', 'PRO', '160px')}
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
    ? `<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No production orders yet. Use <b>Create BOM</b> first if you have none, then <b>Create Order</b>.</td></tr>`
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
  attachLinkTypeahead(document.getElementById('bom-parent-item'), 'Item');
  attachLinkTypeahead(document.getElementById('po-mfg-location'), 'Location');
}

function renderProductionOrderActions(order) {
  if (order.status === 'Draft') {
    return `<button class="action-btn" onclick="issueProductionMaterial('${order.id}')">Issue Material</button>`;
  }
  if (order.status === 'Material Issued' || order.status === 'In Process') {
    // Stage 26.9.3/26.9.4/26.9.6: WIP actions alongside the original
    // one-shot Complete - Confirm Operation/Report Partial/Scrap/Rework are
    // all additive, an order that never uses any of them behaves exactly
    // as it did before this Stage.
    return `
      <button class="action-btn" onclick="completeProductionOrder('${order.id}')">Complete (Receive FG)</button>
      <button class="action-btn" onclick="reportPartialProductionCompletion('${order.id}')">Report Partial</button>
      <button class="action-btn" onclick="confirmProductionOperation('${order.id}')">Confirm Operation</button>
      <button class="action-btn" onclick="postProductionScrap('${order.id}')">Scrap</button>
      <button class="action-btn" onclick="sendProductionToRework('${order.id}')">Rework</button>
    `;
  }
  if (order.status === 'Completed' && (order.actual_cost === undefined || order.actual_cost === null)) {
    return `<button class="action-btn" onclick="recordProductionActualCost('${order.id}')">Record Actual Cost</button>`;
  }
  return '';
}

async function reportPartialProductionCompletion(orderId) {
  const qtyStr = await showCustomPrompt('Quantity to report as completed in this batch:', '', 'Report Partial Completion');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/partial-complete', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, qty })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to report partial completion.', 'Partial Completion Failed');
    return;
  }
  renderView('manufacturing');
}

async function confirmProductionOperation(orderId) {
  const seqStr = await showCustomPrompt('Operation sequence number to confirm (from the order\'s Routing):', '1', 'Confirm Operation');
  if (seqStr === null || seqStr === '') return;
  const seq = parseInt(seqStr, 10);
  const res = await apiFetch('/api/v1/manufacturing/confirm-operation', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, seq })
  });
  if (!res) return;
  const data = await res.json();
  if (!res.ok) {
    await showApiError(res, 'Failed to confirm operation.', 'Confirm Operation Failed');
    return;
  }
  if (data.capacity_warning) {
    await showCustomAlert(data.capacity_warning, 'Capacity Warning');
  }
  renderView('manufacturing');
}

async function postProductionScrap(orderId) {
  const sku = await showCustomPrompt('SKU being scrapped:', '', 'Post Scrap');
  if (sku === null || sku === '') return;
  const qtyStr = await showCustomPrompt('Scrap quantity:', '', 'Post Scrap');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const reason = await showCustomPrompt('Reason for scrap (required):', '', 'Post Scrap');
  if (!reason) return;

  const res = await apiFetch('/api/v1/manufacturing/scrap', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, sku, qty, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to post scrap.', 'Scrap Failed');
    return;
  }
  renderView('manufacturing');
}

async function sendProductionToRework(orderId) {
  const qtyStr = await showCustomPrompt('Quantity to send to rework:', '', 'Send to Rework');
  if (qtyStr === null || qtyStr === '') return;
  const qty = parseFloat(qtyStr);
  if (!qty || qty <= 0) {
    await showCustomAlert('Enter a positive quantity.', 'Invalid Quantity');
    return;
  }
  const reason = await showCustomPrompt('Reason (required):', '', 'Send to Rework');
  if (!reason) return;

  const res = await apiFetch('/api/v1/manufacturing/rework', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, qty, reason })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to log rework.', 'Rework Failed');
    return;
  }
  renderView('manufacturing');
}

async function recordProductionActualCost(orderId) {
  const costStr = await showCustomPrompt('Actual total cost incurred for this production order:', '', 'Record Actual Cost');
  if (costStr === null || costStr === '') return;
  const cost = parseFloat(costStr);
  if (cost < 0 || isNaN(cost)) {
    await showCustomAlert('Enter a valid, non-negative cost.', 'Invalid Cost');
    return;
  }
  const res = await apiFetch('/api/v1/manufacturing/record-actual-cost', {
    method: 'POST',
    body: JSON.stringify({ order_id: orderId, actual_cost: cost })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to record actual cost.', 'Record Cost Failed');
    return;
  }
  renderView('manufacturing');
}

// Stage 26.9.5: MRP reorder suggestions for manufactured items - reuses the
// existing replenishment-suggestion formula/query-param shape rather than a
// new planning engine (same as the backend).
async function renderMRPSuggestionsTab(container) {
  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-location">Location</label>
        <input type="text" id="mrp-location" class="form-input" style="width: 140px;">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-lead-time">Lead Time (Days)</label>
        <input type="number" id="mrp-lead-time" class="form-input" style="width: 100px;" value="7">
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="mrp-safety-stock">Safety Stock</label>
        <input type="number" id="mrp-safety-stock" class="form-input" style="width: 100px;" value="0">
      </div>
      <button class="btn btn-primary" id="mrp-run-btn">Get Suggestions</button>
    </div>
  `;
  container.appendChild(formPanel);
  attachLinkTypeahead(document.getElementById('mrp-location'), 'Location');

  const resultsPanel = document.createElement('div');
  resultsPanel.id = 'mrp-results';
  container.appendChild(resultsPanel);

  document.getElementById('mrp-run-btn').addEventListener('click', runMRPSuggestions);
}

async function runMRPSuggestions() {
  const location = document.getElementById('mrp-location').value.trim();
  const resultsEl = document.getElementById('mrp-results');
  if (!location) {
    resultsEl.innerHTML = `<div class="login-error" style="margin-top: 8px;">Location is required.</div>`;
    return;
  }
  const leadTime = document.getElementById('mrp-lead-time').value || '7';
  const safetyStock = document.getElementById('mrp-safety-stock').value || '0';

  const res = await apiFetch(`/api/v1/manufacturing/mrp-suggestions?location=${encodeURIComponent(location)}&lead_time_days=${leadTime}&safety_stock=${safetyStock}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to fetch MRP suggestions.');
    return;
  }
  const suggestions = await res.json();

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  if (suggestions.length === 0) {
    panel.innerHTML = `<div style="padding: 24px; text-align:center; color:var(--text-muted);">No manufactured items are below their reorder point at this location. Set a reorder point on a manufactured Item, or pick a different location above.</div>`;
  } else {
    let html = `
      <table>
        <thead><tr><th>Item</th><th>Available</th><th>Reorder Point</th><th>Suggested Production Qty</th><th>BOM</th><th>Raw Material Shortfalls</th></tr></thead>
        <tbody>
    `;
    html += suggestions.map(s => `
      <tr>
        <td>${s.parent_item}</td>
        <td>${s.available}</td>
        <td>${s.reorder_point}</td>
        <td>${s.suggested_production_qty}</td>
        <td style="font-family: monospace;">${s.bom_id || '-'}</td>
        <td>${(s.raw_material_shortfalls || []).length === 0 ? 'None' :
          s.raw_material_shortfalls.map(r => `${r.sku}: need ${r.shortfall_qty.toFixed ? r.shortfall_qty.toFixed(2) : r.shortfall_qty} more`).join('<br>')}</td>
      </tr>
    `).join('');
    html += `</tbody></table>`;
    panel.innerHTML = html;
  }
  resultsEl.innerHTML = '';
  resultsEl.appendChild(panel);
}

// 26.9.10 (P2, go-ahead 2026-07-27): finite/infinite capacity scheduling -
// a read-only suggestion, same "GET on render + Refresh button" shape as
// the MRP Suggestions tab above, one table per page.
async function renderProductionScheduleTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '16px';
  panel.innerHTML = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
      <p class="text-muted" style="margin:0; font-size:13px;">Every open, routed production order's operations, sequenced earliest-due-first against each work center's daily capacity. Finite respects that capacity (pushing overflow to a later day); Infinite ignores it - the gap between the two columns is how much capacity is actually stretching the schedule out.</p>
      <button class="btn btn-outline" id="mfg-schedule-refresh">Refresh</button>
    </div>
    <div id="mfg-schedule-results"></div>
  `;
  container.appendChild(panel);
  document.getElementById('mfg-schedule-refresh').addEventListener('click', loadProductionSchedule);
  await loadProductionSchedule();
}

async function loadProductionSchedule() {
  const resultsEl = document.getElementById('mfg-schedule-results');
  if (!resultsEl) return;
  resultsEl.innerHTML = `<div class="text-muted" style="padding:16px;">Loading...</div>`;
  const res = await apiFetch('/api/v1/manufacturing/production-schedule');
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to fetch the production schedule.');
    return;
  }
  const entries = await res.json();
  if (!entries || entries.length === 0) {
    resultsEl.innerHTML = `<div style="padding:24px; text-align:center; color:var(--text-muted);">No open, routed production orders to schedule. Create one under the <b>Orders</b> tab; its BOM needs a Routing before it can be scheduled.</div>`;
    return;
  }
  let html = `
    <table>
      <thead><tr><th>Order</th><th>Op #</th><th>Work Center</th><th>Needed (min)</th><th>Finite Date</th><th>Infinite Date</th><th>Past Due?</th></tr></thead>
      <tbody>
  `;
  html += entries.map(e => `
    <tr>
      <td>${copyableCell(e.order_id, e.order_id)}</td>
      <td>${e.seq}</td>
      <td>${e.work_center_id || '-'}</td>
      <td>${Math.round(e.needed_minutes)}</td>
      <td>${e.finite_date}</td>
      <td>${e.infinite_date}</td>
      <td>${e.overflow ? '<span class="badge badge-warning">Yes</span>' : ''}</td>
    </tr>
  `).join('');
  html += `</tbody></table>`;
  resultsEl.innerHTML = html;
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

  const bomId = document.getElementById('po-mfg-bom').value;
  const quantity = parseFloat(document.getElementById('po-mfg-qty').value);
  const location = document.getElementById('po-mfg-location').value.trim();

  if (!bomId || !quantity || !location) {
    errorEl.textContent = 'BOM, Quantity, and Location are all required.';
    errorEl.classList.remove('hidden');
    return;
  }

  const res = await apiFetch('/api/v1/doc/ProductionOrder', {
    method: 'POST',
    body: JSON.stringify({ bom_id: bomId, quantity, location, status: 'Draft' })
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
    const data = await res.json().catch(() => ({}));
    // Stage 26.9.2 (MFG-0276): the BOM changed after material was issued -
    // offer to acknowledge the variance and retry, rather than a dead end.
    if (data.code === 'MFG-0276') {
      const ack = await showCustomConfirm(data.error + ' Acknowledge the variance and complete anyway?', 'BOM Changed Since Issue');
      if (ack) {
        const ackRes = await apiFetch('/api/v1/manufacturing/acknowledge-bom-variance', {
          method: 'POST', body: JSON.stringify({ order_id: orderId })
        });
        if (ackRes && ackRes.ok) {
          return completeProductionOrder(orderId);
        }
      }
      return;
    }
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
  // Stage 36.2. My Work is the task inbox (36.2.7); the two doctype-backed
  // tabs beside it are the authoring surfaces for templates and workflow
  // definitions, which need no bespoke screen - the generic doctype table
  // already gives them a list, a form, RBAC, audit and CSV import.
  { id: 'my-work', label: 'My Work' },
  { id: 'task-templates', label: 'Task Templates', doctype: 'PIMTaskTemplate' },
  { id: 'workflows', label: 'Workflows', doctype: 'PIMWorkflowDefinition' },
  { id: 'families', label: 'Product Families', doctype: 'ProductFamily' },
  { id: 'attributes', label: 'Attribute Definitions', doctype: 'ProductAttributeDef' },
  { id: 'attribute-groups', label: 'Attribute Groups', doctype: 'ProductAttributeGroup' },
  { id: 'family-attributes', label: 'Family Attributes', doctype: 'ProductFamilyAttribute' },
  { id: 'channels', label: 'Channels', doctype: 'Channel' },
  { id: 'channel-category-map', label: 'Category Mapping', doctype: 'ChannelCategoryMap' },
  { id: 'channel-field-map', label: 'Field Mapping', doctype: 'ChannelFieldMap' },
  { id: 'channel-validation-rules', label: 'Validation Rules', doctype: 'ChannelValidationRule' },
  // 26.4.10: the internal reviewer's side of the supplier portal. Suppliers
  // themselves sign in with the limited 'Supplier' role and reach the same
  // doctype through the generic table screen - there is no second app, and no
  // second list/table implementation here either.
  { id: 'supplier-submissions', label: 'Supplier Submissions', doctype: 'SupplierSubmission' }
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
  } else if (currentPIMTab === 'my-work') {
    await renderPIMMyWorkTab(container);
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

// ---------------------------------------------------------------------------
// Stage 36.2.7 - the My Work inbox, plus the template runner and the workflow
// run panel that make the task engine operable from the browser.
//
// Built entirely from the existing vocabulary: .table-panel, .stat-card /
// .stat-val, .btn / .action-btn, .badge and the .modal-overlay primitives. No
// new table implementation and no second dialog mechanism.
//
// Filtering happens on the server (GET /api/v1/pim/tasks). The one thing this
// screen must never become is the old OMS console - a page that fetches every
// task the tenant has and filters them in the browser.
// ---------------------------------------------------------------------------

let pimMyWorkState = {
  filter: { assignee: 'me', status: '', task_type: '', priority: '', only_overdue: false },
  selected: new Set(),
  users: [],
  templates: [],
  workflows: [],
  lastResult: null
};

function pimTaskQuery() {
  const params = new URLSearchParams();
  const f = pimMyWorkState.filter;
  // 'all' is the screen's word for "clear this filter", not a username - the
  // server would otherwise look for someone actually called "all".
  if (f.assignee && f.assignee !== 'all') params.set('assignee', f.assignee);
  if (f.status) params.set('status', f.status);
  if (f.task_type) params.set('task_type', f.task_type);
  if (f.priority) params.set('priority', f.priority);
  if (f.only_overdue) params.set('only_overdue', '1');
  params.set('limit', '200');
  return params.toString();
}

async function renderPIMMyWorkTab(container) {
  pimMyWorkState.selected.clear();

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <div class="dashboard-stats-row" id="pim-task-tiles"></div>
    <div class="table-controls" style="flex-wrap:wrap;gap:12px;">
      <div class="form-group" style="margin:0;">
        <label class="form-label" for="pim-task-assignee">Assignee</label>
        <select class="form-select" id="pim-task-assignee"><option value="me">My tasks</option><option value="all">Everyone</option></select>
      </div>
      <div class="form-group" style="margin:0;">
        <label class="form-label" for="pim-task-status">Status</label>
        <select class="form-select" id="pim-task-status">
          <option value="">Any status</option><option value="Open">Open</option><option value="In Progress">In Progress</option>
          <option value="Blocked">Blocked</option><option value="Done">Done</option><option value="Cancelled">Cancelled</option>
        </select>
      </div>
      <div class="form-group" style="margin:0;">
        <label class="form-label" for="pim-task-type">Type</label>
        <select class="form-select" id="pim-task-type">
          <option value="">Any type</option><option value="Enrichment">Enrichment</option><option value="Imagery">Imagery</option>
          <option value="Attributes">Attributes</option><option value="Translation">Translation</option>
          <option value="Review">Review</option><option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group" style="margin:0;">
        <label class="form-label" for="pim-task-priority">Priority</label>
        <select class="form-select" id="pim-task-priority">
          <option value="">Any priority</option><option value="High">High</option><option value="Normal">Normal</option><option value="Low">Low</option>
        </select>
      </div>
      <label style="display:flex;align-items:center;gap:6px;font-size:13px;margin-top:18px;">
        <input type="checkbox" id="pim-task-overdue"> Overdue only
      </label>
      <button class="btn btn-outline" id="pim-task-refresh" style="margin-top:14px;">Refresh</button>
    </div>
    <div class="bulk-edit-bar hidden" id="pim-task-bulk-bar">
      <span id="pim-task-selection-count">0 selected</span>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-assign">Reassign</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-status">Set status</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-due">Set due date</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-comment">Add comment</button>
    </div>
    <div class="table-wrapper" id="pim-task-table" style="margin-top:12px;"></div>`;
  container.appendChild(panel);

  const templatePanel = document.createElement('div');
  templatePanel.className = 'table-panel';
  templatePanel.style.cssText = 'padding:24px;margin-top:24px;';
  templatePanel.innerHTML = `
    <h2 style="font-size:16px;margin:0 0 4px;">Run a task template</h2>
    <p class="text-muted" style="font-size:13px;margin:0 0 14px;">Creates one task per product in the chosen group. A product that already has an open task from this template is skipped, so re-running the template picks up new products without duplicating work.</p>
    <div class="table-controls" style="flex-wrap:wrap;gap:12px;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-template-select">Template</label><select class="form-select" id="pim-template-select"></select></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-template-group">Product group</label><select class="form-select" id="pim-template-group"></select></div>
      <button class="btn btn-primary" id="pim-template-run" style="margin-top:14px;">Create tasks</button>
    </div>
    <div id="pim-template-result" style="margin-top:12px;"></div>`;
  container.appendChild(templatePanel);

  const workflowPanel = document.createElement('div');
  workflowPanel.className = 'table-panel';
  workflowPanel.style.cssText = 'padding:24px;margin-top:24px;';
  workflowPanel.innerHTML = `
    <h2 style="font-size:16px;margin:0 0 4px;">Workflow runs</h2>
    <p class="text-muted" style="font-size:13px;margin:0 0 14px;">A run walks one product through a workflow's stages, creating each stage's tasks as it enters. Advance is automatic when a stage's last task closes; press Advance to re-check a run that is waiting on a condition.</p>
    <div class="table-controls" style="flex-wrap:wrap;gap:12px;">
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-workflow-select">Workflow</label><select class="form-select" id="pim-workflow-select"></select></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-workflow-target">Start for</label><select class="form-select" id="pim-workflow-target"><option value="item">One product</option><option value="group">A product group</option></select></div>
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-workflow-ref">Product / group</label><input class="form-input" id="pim-workflow-ref" placeholder="Item code"></div>
      <button class="btn btn-primary" id="pim-workflow-start" style="margin-top:14px;">Start run</button>
      <div class="form-group" style="margin:0;"><label class="form-label" for="pim-run-status">Show</label><select class="form-select" id="pim-run-status"><option value="Running">Running</option><option value="Paused">Paused</option><option value="">All</option><option value="Completed">Completed</option><option value="Cancelled">Cancelled</option></select></div>
    </div>
    <div class="table-wrapper" id="pim-run-table" style="margin-top:12px;"></div>`;
  container.appendChild(workflowPanel);

  const f = pimMyWorkState.filter;
  const bind = (id, key, isCheckbox) => {
    const el = panel.querySelector(id);
    if (!el) return;
    if (isCheckbox) el.checked = !!f[key]; else el.value = f[key] || '';
    el.addEventListener('change', () => {
      f[key] = isCheckbox ? el.checked : el.value;
      pimMyWorkState.selected.clear();
      loadPIMTasks();
    });
  };
  bind('#pim-task-assignee', 'assignee');
  bind('#pim-task-status', 'status');
  bind('#pim-task-type', 'task_type');
  bind('#pim-task-priority', 'priority');
  bind('#pim-task-overdue', 'only_overdue', true);
  panel.querySelector('#pim-task-refresh').addEventListener('click', loadPIMTasks);
  panel.querySelector('#pim-task-bulk-assign').addEventListener('click', () => runPIMTaskBulk('assign'));
  panel.querySelector('#pim-task-bulk-status').addEventListener('click', () => runPIMTaskBulk('status'));
  panel.querySelector('#pim-task-bulk-due').addEventListener('click', () => runPIMTaskBulk('due_date'));
  panel.querySelector('#pim-task-bulk-comment').addEventListener('click', () => runPIMTaskBulk('comment'));

  templatePanel.querySelector('#pim-template-run').addEventListener('click', runPIMTaskTemplate);
  workflowPanel.querySelector('#pim-workflow-start').addEventListener('click', startPIMWorkflowRun);
  workflowPanel.querySelector('#pim-run-status').addEventListener('change', loadPIMWorkflowRuns);
  workflowPanel.querySelector('#pim-workflow-target').addEventListener('change', e => {
    workflowPanel.querySelector('#pim-workflow-ref').placeholder =
      e.target.value === 'group' ? 'Product group id or code' : 'Item code';
  });

  // The four reads are independent, so they go out together rather than in
  // sequence - the inbox is opened dozens of times a day.
  await Promise.all([
    loadPIMAssignableUsers(), loadPIMTaskTemplates(), loadPIMWorkflowDefinitions(),
    loadPIMTasks(), loadPIMWorkflowRuns()
  ]);
}

async function loadPIMAssignableUsers() {
  const res = await apiFetch('/api/v1/pim/assignable-users');
  // A role that may read tasks but not reassign them gets a 403 here. That is
  // not an error worth showing: the picker simply stays as "My tasks /
  // Everyone", which is exactly what that role can act on.
  if (!res || !res.ok) return;
  pimMyWorkState.users = (await res.json()).users || [];
  const select = document.getElementById('pim-task-assignee');
  if (!select) return;
  const current = pimMyWorkState.filter.assignee;
  select.innerHTML = `<option value="me">My tasks</option><option value="all">Everyone</option>` +
    pimMyWorkState.users.map(u => `<option value="${escapeHTMLText(u.username)}">${escapeHTMLText(u.username)} (${escapeHTMLText(u.role)})</option>`).join('');
  select.value = current;
}

async function loadPIMTaskTemplates() {
  const [templateRes, groupRes] = await Promise.all([
    apiFetch('/api/v1/pim/task-templates'),
    apiFetch('/api/v1/doc/PIMProductGroup')
  ]);
  const templateSelect = document.getElementById('pim-template-select');
  const groupSelect = document.getElementById('pim-template-group');
  if (!templateSelect || !groupSelect) return;

  if (templateRes && templateRes.ok) {
    pimMyWorkState.templates = (await templateRes.json()).templates || [];
  }
  templateSelect.innerHTML = pimMyWorkState.templates.length === 0
    ? '<option value="">No active templates</option>'
    : pimMyWorkState.templates.map(t => `<option value="${escapeHTMLText(t.code)}">${escapeHTMLText(t.name || t.code)}</option>`).join('');

  let groups = [];
  if (groupRes && groupRes.ok) {
    groups = (await groupRes.json()).filter(g => (g.status || 'Active') === 'Active');
  }
  groupSelect.innerHTML = groups.length === 0
    ? '<option value="">No active product groups</option>'
    : groups.map(g => `<option value="${escapeHTMLText(g.id)}">${escapeHTMLText(g.name || g.id)}</option>`).join('');
}

async function loadPIMWorkflowDefinitions() {
  const res = await apiFetch('/api/v1/pim/workflows');
  const select = document.getElementById('pim-workflow-select');
  if (!select) return;
  if (res && res.ok) {
    pimMyWorkState.workflows = (await res.json()).workflows || [];
  }
  select.innerHTML = pimMyWorkState.workflows.length === 0
    ? '<option value="">No active workflows</option>'
    : pimMyWorkState.workflows.map(w => `<option value="${escapeHTMLText(w.code)}">${escapeHTMLText(w.name || w.code)} (${(w.stages || []).length} stages)</option>`).join('');
}

async function loadPIMTasks() {
  const host = document.getElementById('pim-task-table');
  if (!host) return;
  host.innerHTML = '<div class="text-muted" style="padding:16px;">Loading tasks…</div>';
  const res = await apiFetch(`/api/v1/pim/tasks?${pimTaskQuery()}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load tasks.'); host.innerHTML = ''; return; }
  const result = await res.json();
  pimMyWorkState.lastResult = result;
  renderPIMTaskTiles(result);
  renderPIMTaskTable(result);
  updatePIMTaskBulkBar();
}

function renderPIMTaskTiles(result) {
  const host = document.getElementById('pim-task-tiles');
  if (!host) return;
  const tally = result.status_tally || {};
  const overdue = (result.tasks || []).filter(t => t.overdue).length;
  const tiles = [
    ['Open', tally['Open'] || 0],
    ['In Progress', tally['In Progress'] || 0],
    ['Blocked', tally['Blocked'] || 0],
    // Counted from the page rather than the whole filtered set, and labelled
    // so - the tally the server returns is per status, and inventing a
    // whole-set overdue number the server did not send would be a guess.
    ['Overdue on this page', overdue]
  ];
  host.innerHTML = tiles.map(([label, count]) => `
    <div class="stat-card">
      <span class="stat-label">${escapeHTMLText(label)}</span>
      <span class="stat-val">${count}</span>
    </div>`).join('');
}

function pimTaskStatusBadge(task) {
  const map = { 'Done': 'badge-success', 'Cancelled': 'badge-secondary', 'Blocked': 'badge-danger', 'In Progress': 'badge-warning' };
  return `<span class="badge ${map[task.status] || 'badge-secondary'}">${escapeHTMLText(task.status)}</span>`;
}

function renderPIMTaskTable(result) {
  const host = document.getElementById('pim-task-table');
  if (!host) return;
  const tasks = result.tasks || [];
  if (tasks.length === 0) {
    host.innerHTML = `<p class="text-muted" style="padding:16px;text-align:center;">No tasks match this filter. ${pimMyWorkState.filter.assignee === 'me' ? 'Switch Assignee to "Everyone" to see the whole queue.' : 'Run a task template below, or assign one from a PIM report row.'}</p>`;
    return;
  }
  const rows = tasks.map(task => {
    const due = task.due_date
      ? `${escapeHTMLText(task.due_date)}${task.overdue ? ' <span class="badge badge-danger">overdue</span>' : ''}`
      : '<span class="text-muted">—</span>';
    const canProgress = task.status !== 'Done' && task.status !== 'Cancelled';
    return `<tr>
      <td><input type="checkbox" class="pim-task-select" data-task="${escapeHTMLText(task.id)}"${pimMyWorkState.selected.has(task.id) ? ' checked' : ''}></td>
      <td>
        <div style="font-weight:600;">${escapeHTMLText(task.title)}</div>
        <div class="text-muted" style="font-size:12px;">${escapeHTMLText(task.task_type || '')}${task.stage ? ' · stage ' + escapeHTMLText(task.stage) : ''}${task.comments && task.comments.length ? ' · ' + task.comments.length + ' comment(s)' : ''}</div>
      </td>
      <td>${task.item_code ? escapeHTMLText(task.item_code) + (task.item_name ? `<div class="text-muted" style="font-size:12px;">${escapeHTMLText(task.item_name)}</div>` : '') : '<span class="text-muted">—</span>'}</td>
      <td>${escapeHTMLText(task.assignee || '(unassigned)')}</td>
      <td>${due}</td>
      <td>${escapeHTMLText(task.priority || 'Normal')}</td>
      <td>${pimTaskStatusBadge(task)}</td>
      <td style="white-space:nowrap;">
        ${canProgress && task.status !== 'In Progress' ? `<button class="action-btn" data-task-act="start" data-task="${escapeHTMLText(task.id)}">Start</button>` : ''}
        ${canProgress ? `<button class="action-btn" data-task-act="done" data-task="${escapeHTMLText(task.id)}">Done</button>` : ''}
        <button class="action-btn" data-task-act="open" data-task="${escapeHTMLText(task.id)}">Details</button>
      </td>
    </tr>`;
  }).join('');

  host.innerHTML = `<table>
    <thead><tr>
      <th style="width:32px;"><input type="checkbox" id="pim-task-select-all"></th>
      <th>Task</th><th>Product</th><th>Assignee</th><th>Due</th><th>Priority</th><th>Status</th><th>Actions</th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>
  <p class="text-muted" style="font-size:12px;margin:10px 0 0;">Showing ${tasks.length} of ${result.total} task(s).</p>`;

  host.querySelectorAll('.pim-task-select').forEach(box => {
    box.addEventListener('change', () => {
      const id = box.getAttribute('data-task');
      if (box.checked) pimMyWorkState.selected.add(id); else pimMyWorkState.selected.delete(id);
      updatePIMTaskBulkBar();
    });
  });
  const selectAll = host.querySelector('#pim-task-select-all');
  if (selectAll) {
    selectAll.addEventListener('change', e => {
      host.querySelectorAll('.pim-task-select').forEach(box => {
        box.checked = e.target.checked;
        const id = box.getAttribute('data-task');
        if (e.target.checked) pimMyWorkState.selected.add(id); else pimMyWorkState.selected.delete(id);
      });
      updatePIMTaskBulkBar();
    });
  }
  host.querySelectorAll('[data-task-act]').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = btn.getAttribute('data-task');
      const act = btn.getAttribute('data-task-act');
      if (act === 'open') { openPIMTaskDetail(id); return; }
      runPIMTaskAction(id, 'status', { status: act === 'start' ? 'In Progress' : 'Done' });
    });
  });
}

function updatePIMTaskBulkBar() {
  const bar = document.getElementById('pim-task-bulk-bar');
  const label = document.getElementById('pim-task-selection-count');
  if (!bar || !label) return;
  const count = pimMyWorkState.selected.size;
  bar.classList.toggle('hidden', count === 0);
  label.textContent = `${count} selected`;
}

async function runPIMTaskAction(taskID, action, body) {
  const res = await apiFetch(`/api/v1/pim/tasks/${encodeURIComponent(taskID)}/${action}`, {
    method: 'POST', body: JSON.stringify(body || {})
  });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, 'That task action was refused.'); return false; }
  await loadPIMTasks();
  // A completed task can advance its workflow run, so the run panel is stale
  // the moment a status changes.
  await loadPIMWorkflowRuns();
  return true;
}

// 36.2.5 - the bulk bar. Reports per-task outcomes rather than a single
// success/failure, because a mixed selection is expected to be partially
// applicable (a Done task cannot be reassigned) and a blanket message would
// hide the ones that did work.
async function runPIMTaskBulk(action) {
  const taskIDs = Array.from(pimMyWorkState.selected);
  if (taskIDs.length === 0) return;
  let value = '';
  if (action === 'assign') {
    value = await showCustomPrompt(`Reassign ${taskIDs.length} task(s) to which user? Leave blank to unassign.`, '', 'Reassign tasks');
    if (value === null) return;
  } else if (action === 'status') {
    value = await showCustomPrompt(`New status for ${taskIDs.length} task(s): Open, In Progress, Blocked, Done or Cancelled.`, 'In Progress', 'Set task status');
    if (!value) return;
  } else if (action === 'due_date') {
    value = await showCustomPrompt(`New due date for ${taskIDs.length} task(s), as YYYY-MM-DD. Leave blank to clear it.`, '', 'Set due date');
    if (value === null) return;
  } else if (action === 'comment') {
    value = await showCustomPrompt(`Comment to add to ${taskIDs.length} task(s).`, '', 'Add comment');
    if (!value) return;
  }
  const res = await apiFetch('/api/v1/pim/tasks/bulk', {
    method: 'POST', body: JSON.stringify({ action, task_ids: taskIDs, value })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'The bulk action was refused.'); return; }
  const result = await res.json();
  const refused = (result.outcomes || []).filter(o => !o.ok);
  let message = `${result.succeeded} of ${result.requested} task(s) updated.`;
  if (refused.length > 0) {
    message += `\n\nRefused:\n` + refused.slice(0, 10).map(o => `• ${o.task_id}: ${o.error}`).join('\n');
    if (refused.length > 10) message += `\n…and ${refused.length - 10} more.`;
  }
  showCustomAlert(message, 'Bulk task action');
  pimMyWorkState.selected.clear();
  await loadPIMTasks();
  await loadPIMWorkflowRuns();
}

// The task detail modal: the full comment thread, plus the actions that do not
// fit on a table row. Built on the same .modal-overlay primitives as every
// other dialog in this file.
async function openPIMTaskDetail(taskID) {
  const res = await apiFetch(`/api/v1/pim/tasks?task_id=${encodeURIComponent(taskID)}`);
  if (!res || !res.ok) { await showApiError(res, 'Could not load that task.'); return; }
  const task = ((await res.json()).tasks || [])[0];
  if (!task) { showCustomAlert('That task no longer exists.', 'Task'); return; }

  document.getElementById('pim-task-detail-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-task-detail-modal';
  const terminal = task.status === 'Done' || task.status === 'Cancelled';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">${escapeHTMLText(task.title)}</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <p style="margin:0 0 10px;">${pimTaskStatusBadge(task)} <span class="text-muted" style="font-size:13px;">${escapeHTMLText(task.task_type || '')} · ${escapeHTMLText(task.scope_type || '')} ${escapeHTMLText(task.scope_ref || '')}</span></p>
        ${task.instructions ? `<p style="font-size:13px;">${escapeHTMLText(task.instructions)}</p>` : ''}
        <dl style="display:grid;grid-template-columns:auto 1fr;gap:4px 14px;font-size:13px;margin:0 0 14px;">
          <dt class="text-muted">Product</dt><dd style="margin:0;">${escapeHTMLText(task.item_code || '—')} ${escapeHTMLText(task.item_name || '')}</dd>
          <dt class="text-muted">Assignee</dt><dd style="margin:0;">${escapeHTMLText(task.assignee || '(unassigned)')}</dd>
          <dt class="text-muted">Due</dt><dd style="margin:0;">${escapeHTMLText(task.due_date || '—')}${task.overdue ? ' <span class="badge badge-danger">overdue</span>' : ''}</dd>
          ${task.workflow_run ? `<dt class="text-muted">Workflow run</dt><dd style="margin:0;">${escapeHTMLText(task.workflow_run)} · stage ${escapeHTMLText(task.stage || '')}</dd>` : ''}
          ${task.completed_at ? `<dt class="text-muted">Completed</dt><dd style="margin:0;">${escapeHTMLText(task.completed_at)} by ${escapeHTMLText(task.completed_by || '')}</dd>` : ''}
        </dl>
        <h4 style="font-size:13px;margin:0 0 6px;">Comments</h4>
        <div id="pim-task-comments" style="max-height:200px;overflow-y:auto;font-size:13px;">
          ${(task.comments || []).length === 0 ? '<p class="text-muted" style="margin:0;">No comments yet.</p>' :
            task.comments.map(c => `<p style="margin:0 0 8px;"><strong>${escapeHTMLText(c.author)}</strong> <span class="text-muted">${escapeHTMLText((c.at || '').slice(0, 16).replace('T', ' '))}</span><br>${escapeHTMLText(c.comment)}</p>`).join('')}
        </div>
        <div class="form-group" style="margin-top:12px;">
          <label class="form-label" for="pim-task-comment-input">Add a comment</label>
          <input class="form-input" id="pim-task-comment-input" placeholder="What changed, or what is blocking this?">
        </div>
      </div>
      <div class="modal-footer" style="flex-wrap:wrap;gap:8px;">
        <button type="button" class="btn btn-secondary" id="pim-task-close">Close</button>
        <button type="button" class="btn btn-outline" id="pim-task-comment-btn">Comment</button>
        ${!terminal ? `<button type="button" class="btn btn-outline" id="pim-task-reassign">Reassign</button>` : ''}
        ${!terminal ? `<button type="button" class="btn btn-outline" id="pim-task-block">Block</button>` : ''}
        ${!terminal ? `<button type="button" class="btn btn-outline" id="pim-task-cancel-task">Cancel task</button>` : ''}
        ${!terminal ? `<button type="button" class="btn btn-primary" id="pim-task-done">Mark done</button>` : ''}
        ${terminal ? `<button type="button" class="btn btn-primary" id="pim-task-followup">Create follow-up</button>` : ''}
      </div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-task-close').addEventListener('click', close);

  const act = async (action, body) => {
    if (await runPIMTaskAction(taskID, action, body)) close();
  };
  overlay.querySelector('#pim-task-comment-btn').addEventListener('click', async () => {
    const input = overlay.querySelector('#pim-task-comment-input');
    if (!input.value.trim()) return;
    await act('comment', { comment: input.value.trim() });
  });
  overlay.querySelector('#pim-task-done')?.addEventListener('click', () => act('status', { status: 'Done' }));
  overlay.querySelector('#pim-task-block')?.addEventListener('click', () => act('status', { status: 'Blocked' }));
  overlay.querySelector('#pim-task-cancel-task')?.addEventListener('click', async () => {
    if (!await showCustomConfirm('Cancel this task? A cancelled task cannot be re-opened.', 'Cancel task')) return;
    await act('status', { status: 'Cancelled' });
  });
  overlay.querySelector('#pim-task-reassign')?.addEventListener('click', async () => {
    const assignee = await showCustomPrompt('Reassign to which user? Leave blank to unassign.', task.assignee || '', 'Reassign task');
    if (assignee === null) return;
    await act('assign', { assignee });
  });
  // Re-opening a Done task is deliberately not offered - a completed task may
  // already have advanced its workflow past that stage, and there is no honest
  // way to un-advance it. A follow-up says the same thing truthfully.
  overlay.querySelector('#pim-task-followup')?.addEventListener('click', async () => {
    const note = await showCustomPrompt('What still needs doing?', '', 'Create follow-up task');
    if (note === null) return;
    await act('follow-up', { note });
  });
}

// 36.2.2 - instantiate a template across a product group.
async function runPIMTaskTemplate() {
  const templateCode = document.getElementById('pim-template-select')?.value;
  const groupID = document.getElementById('pim-template-group')?.value;
  const host = document.getElementById('pim-template-result');
  if (!templateCode || !groupID) {
    showCustomAlert('Pick both a template and a product group first. Templates are authored on the Task Templates tab and groups under PIM » Product Group.', 'Run a task template');
    return;
  }
  const res = await apiFetch(`/api/v1/pim/task-templates/${encodeURIComponent(templateCode)}/instantiate`, {
    method: 'POST', body: JSON.stringify({ group_id: groupID })
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not run that template.'); return; }
  const result = await res.json();
  host.innerHTML = `<p style="font-size:13px;margin:0;"><strong>${result.created_count}</strong> task(s) created${result.skipped_count > 0 ? `, <strong>${result.skipped_count}</strong> skipped (they already have an open task from this template)` : ''}.</p>`;
  await loadPIMTasks();
}

// 36.2.4 / 36.2.5 - the workflow run panel.
async function startPIMWorkflowRun() {
  const code = document.getElementById('pim-workflow-select')?.value;
  const target = document.getElementById('pim-workflow-target')?.value;
  const ref = (document.getElementById('pim-workflow-ref')?.value || '').trim();
  if (!code || !ref) {
    showCustomAlert('Pick a workflow and name the product or group to start it for.', 'Start a workflow');
    return;
  }
  const body = target === 'group' ? { group_id: ref } : { item_code: ref };
  const res = await apiFetch(`/api/v1/pim/workflows/${encodeURIComponent(code)}/start`, {
    method: 'POST', body: JSON.stringify(body)
  });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not start that workflow.'); return; }
  const result = await res.json();
  showCustomAlert(result.run_id
    ? `Run ${result.run_id} started.`
    : `${result.succeeded} of ${result.requested} run(s) started${result.failed ? `, ${result.failed} refused` : ''}.`, 'Workflow started');
  await Promise.all([loadPIMWorkflowRuns(), loadPIMTasks()]);
}

async function loadPIMWorkflowRuns() {
  const host = document.getElementById('pim-run-table');
  if (!host) return;
  const status = document.getElementById('pim-run-status')?.value ?? 'Running';
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  const res = await apiFetch(`/api/v1/pim/workflow-runs?${params.toString()}`);
  if (!res || !res.ok) { host.innerHTML = '<p class="text-muted" style="padding:12px;">Could not load workflow runs.</p>'; return; }
  const runs = (await res.json()).runs || [];
  if (runs.length === 0) {
    host.innerHTML = '<p class="text-muted" style="padding:12px;">No workflow runs in this state.</p>';
    return;
  }
  host.innerHTML = `<table>
    <thead><tr><th>Product</th><th>Workflow</th><th>Stage</th><th>Status</th><th>Waiting on</th><th>Tasks</th><th>Actions</th></tr></thead>
    <tbody>${runs.map(run => `<tr>
      <td>${escapeHTMLText(run.item_code)}${run.item_name ? `<div class="text-muted" style="font-size:12px;">${escapeHTMLText(run.item_name)}</div>` : ''}</td>
      <td>${escapeHTMLText(run.workflow_name || run.workflow)}</td>
      <td>${escapeHTMLText(run.current_stage || '—')}<div class="text-muted" style="font-size:12px;">${escapeHTMLText(run.stage_progress || '')}</div></td>
      <td><span class="badge ${run.status === 'Completed' ? 'badge-success' : run.status === 'Cancelled' ? 'badge-secondary' : run.status === 'Paused' ? 'badge-warning' : 'badge-secondary'}">${escapeHTMLText(run.status)}</span></td>
      <td style="font-size:12px;">${escapeHTMLText(run.blocked_reason || '—')}</td>
      <td>${run.open_tasks} open / ${run.total_tasks}</td>
      <td style="white-space:nowrap;">
        ${run.status === 'Running' ? `<button class="action-btn" data-run-act="advance" data-run="${escapeHTMLText(run.id)}">Advance</button>
        <button class="action-btn" data-run-act="pause" data-run="${escapeHTMLText(run.id)}">Pause</button>` : ''}
        ${run.status === 'Paused' ? `<button class="action-btn" data-run-act="resume" data-run="${escapeHTMLText(run.id)}">Resume</button>` : ''}
        ${run.status === 'Running' || run.status === 'Paused' ? `<button class="action-btn" data-run-act="cancel" data-run="${escapeHTMLText(run.id)}">Cancel</button>` : ''}
        <button class="action-btn" data-run-act="log" data-run="${escapeHTMLText(run.id)}">Activity</button>
      </td></tr>`).join('')}</tbody></table>`;

  host.querySelectorAll('[data-run-act]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const runID = btn.getAttribute('data-run');
      const action = btn.getAttribute('data-run-act');
      if (action === 'log') {
        const run = runs.find(r => r.id === runID);
        const lines = (run?.activity || []).map(a =>
          `${(a.at || '').slice(0, 16).replace('T', ' ')} — ${a.actor}: ${a.event}${a.detail ? ' (' + a.detail + ')' : ''}`);
        showCustomAlert(lines.length ? lines.join('\n') : 'No activity recorded yet.', `Run ${runID}`);
        return;
      }
      if (action === 'cancel' && !await showCustomConfirm('Cancel this run? Its open tasks are cancelled with it.', 'Cancel workflow run')) return;
      const res = await apiFetch(`/api/v1/pim/workflow-runs/${encodeURIComponent(runID)}/action`, {
        method: 'POST', body: JSON.stringify({ action })
      });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'That workflow action was refused.'); return; }
      const result = await res.json();
      if (result.message) showCustomAlert(result.message, 'Workflow run');
      await Promise.all([loadPIMWorkflowRuns(), loadPIMTasks()]);
    });
  });
}

// ---------------------------------------------------------------------------
// 36.2.6 - assign a task straight from a report row.
//
// This is the affordance that turns a readiness report from something you read
// into something you act on: the report tells you which products are short of
// what, and this puts that product in someone's inbox without retyping the code
// into a separate form.
// ---------------------------------------------------------------------------
window.openPIMAssignTaskModal = async function(itemCode, contextLabel) {
  let users = pimMyWorkState.users;
  if (users.length === 0) {
    const res = await apiFetch('/api/v1/pim/assignable-users');
    if (res && res.ok) { users = (await res.json()).users || []; pimMyWorkState.users = users; }
  }

  document.getElementById('pim-assign-task-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-assign-task-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Assign a task</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <p class="text-muted" style="font-size:13px;margin:0 0 12px;">On <strong>${escapeHTMLText(itemCode)}</strong>${contextLabel ? `, from ${escapeHTMLText(contextLabel)}` : ''}.</p>
        <div class="form-group"><label class="form-label" for="pim-assign-title">Title</label><input class="form-input" id="pim-assign-title" value="Fix ${escapeHTMLText(itemCode)}"></div>
        <div class="form-group"><label class="form-label" for="pim-assign-type">Type</label><select class="form-select" id="pim-assign-type">
          <option>Enrichment</option><option>Imagery</option><option>Attributes</option><option>Translation</option><option>Review</option><option>Other</option>
        </select></div>
        <div class="form-group"><label class="form-label" for="pim-assign-who">Assignee</label><select class="form-select" id="pim-assign-who">
          <option value="">(unassigned)</option>${users.map(u => `<option value="${escapeHTMLText(u.username)}">${escapeHTMLText(u.username)} (${escapeHTMLText(u.role)})</option>`).join('')}
        </select></div>
        <div class="form-group"><label class="form-label" for="pim-assign-due">Due date</label><input type="date" class="form-input" id="pim-assign-due"></div>
        <div class="form-group"><label class="form-label" for="pim-assign-priority">Priority</label><select class="form-select" id="pim-assign-priority">
          <option>Normal</option><option>High</option><option>Low</option>
        </select></div>
        <div class="form-group"><label class="form-label" for="pim-assign-notes">Instructions</label><input class="form-input" id="pim-assign-notes" placeholder="What needs doing, and why"></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" id="pim-assign-cancel">Cancel</button>
        <button type="button" class="btn btn-primary" id="pim-assign-create">Create task</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-assign-cancel').addEventListener('click', close);
  overlay.querySelector('#pim-assign-create').addEventListener('click', async () => {
    const body = {
      title: overlay.querySelector('#pim-assign-title').value.trim(),
      task_type: overlay.querySelector('#pim-assign-type').value,
      scope_type: 'Product',
      scope_ref: itemCode,
      item_code: itemCode,
      assignee: overlay.querySelector('#pim-assign-who').value,
      due_date: overlay.querySelector('#pim-assign-due').value,
      priority: overlay.querySelector('#pim-assign-priority').value,
      instructions: overlay.querySelector('#pim-assign-notes').value.trim()
    };
    if (!body.title) { showCustomAlert('A task needs a title.', 'Assign a task'); return; }
    const res = await apiFetch('/api/v1/pim/tasks', { method: 'POST', body: JSON.stringify(body) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Could not create that task.'); return; }
    const result = await res.json();
    close();
    showCustomAlert(`Task ${result.task_id} created${body.assignee ? ` for ${body.assignee}` : ''}. It is on the PIM » My Work tab.`, 'Task created');
  });
};

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
    if (!rows.length) { results.innerHTML = '<div class="text-muted">No issues found &mdash; every item passed this check.</div>'; return; }
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

    <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px;">
      <h3 style="font-size: 14px; font-weight: 700; margin: 0;">Content</h3>
      <button class="btn btn-outline btn-sm" id="pim-content-assist-btn" title="Draft title, descriptions and tags from this product's own stored data. Nothing is saved until you click Save Draft.">Assist</button>
    </div>
    <div id="pim-content-assist-note" class="hidden" style="margin-bottom: 12px; padding: 10px 12px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-color); font-size: 13px; color: var(--text-muted);"></div>
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
  document.getElementById('pim-content-assist-btn').addEventListener('click', () => runPIMContentAssist(itemCode));
  document.getElementById('pim-content-save-btn').addEventListener('click', () => savePIMContent(itemCode, 'Draft'));
  document.getElementById('pim-content-submit-btn').addEventListener('click', () => submitPIMContent(itemCode));
  document.getElementById('pim-media-upload-btn').addEventListener('click', () => uploadPIMMedia(itemCode));
  document.getElementById('pim-publish-btn').addEventListener('click', () => publishPIMItem(itemCode));
  document.getElementById('pim-publish-preview-btn').addEventListener('click', () => previewPIMPublish(itemCode));

  await renderPIMMediaGallery(itemCode);
  await renderPIMPublishSection(itemCode);
  await renderPIMContentHistory(itemCode);
}

// runPIMContentAssist (Stage 36.7.2) wires the Stage 26.4.11 content-assist
// endpoint to a button. The draft is written into the form fields, never sent
// straight to the server: the engine's whole safety argument is that a human
// reviews the text and saves it as an ordinary Draft, which still passes the
// existing approval gate. Fields already carrying text are left alone unless
// the reviewer confirms an overwrite, so Assist can never silently discard
// copy someone was in the middle of writing.
async function runPIMContentAssist(itemCode) {
  const errorEl = document.getElementById('pim-content-error');
  const noteEl = document.getElementById('pim-content-assist-note');
  errorEl.classList.add('hidden');
  noteEl.classList.add('hidden');

  const language = (document.getElementById('pim-content-lang').value || 'en').trim();
  const res = await apiFetch(`/api/v1/pim/content-assist/${encodeURIComponent(itemCode)}?language=${encodeURIComponent(language)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Could not build a suggested draft for this product.');
    return;
  }
  const draft = await res.json();

  const targets = [
    ['pim-content-title', draft.title],
    ['pim-content-seo', draft.seo_title],
    ['pim-content-short', draft.short_desc],
    ['pim-content-long', draft.long_desc],
    ['pim-content-tags', draft.tags]
  ];
  const occupied = targets.filter(([id, value]) => value && document.getElementById(id).value.trim() !== '');
  let overwrite = false;
  if (occupied.length > 0) {
    overwrite = await showCustomConfirm(
      `${occupied.length} content field${occupied.length === 1 ? '' : 's'} already contain text. Replace them with the suggested draft? Choose Cancel to fill only the empty fields.`,
      'Assisted Draft');
  }
  let filled = 0;
  targets.forEach(([id, value]) => {
    if (!value) return;
    const input = document.getElementById(id);
    if (input.value.trim() !== '' && !overwrite) return;
    input.value = value;
    filled++;
  });

  const sources = (draft.source_fields || []).join(', ') || 'none';
  const warnings = (draft.warnings || []).map(w => `<li>${escapeHTMLText(w)}</li>`).join('');
  noteEl.innerHTML = `
    <strong>${filled} field${filled === 1 ? '' : 's'} filled from this product's own data.</strong>
    Nothing has been saved yet - review the text, then use Save Draft or Submit for Approval.
    <div style="margin-top: 6px;">Built from: ${escapeHTMLText(sources)}</div>
    ${warnings ? `<ul style="margin: 6px 0 0 18px;">${warnings}</ul>` : ''}`;
  noteEl.classList.remove('hidden');
}

async function renderPIMMediaGallery(itemCode) {
  const gallery = document.getElementById('pim-media-gallery');
  if (!gallery) return;
  const res = await apiFetch(`/api/v1/pim/media?item=${encodeURIComponent(itemCode)}`);
  const media = res && res.ok ? await res.json() : [];

  if (media.length === 0) {
    gallery.innerHTML = `<div style="color: var(--text-muted); font-size: 13px;">No media uploaded yet. Use the upload control above to attach product images to this item.</div>`;
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
    ? `<div style="color: var(--text-muted); font-size: 13px;">No publish attempts yet. Publish this item to a sales channel from the Channels section above.</div>`
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
    ? `<div class="text-muted" style="font-size:13px;">No approval history yet. Entries appear here once a content change is submitted for approval.</div>`
    : `<table><thead><tr><th>Action</th><th>By</th><th>Comment</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${l.action}</td><td>${l.actor_user_id}</td><td>${l.comment || ''}</td><td>${l.created_at || ''}</td></tr>`).join('')
      }</tbody></table>`;

  const versionsHTML = versions.length === 0
    ? `<div class="text-muted" style="font-size:13px;">No approved versions yet. Once a content version is approved you can <b>Restore</b> it from here.</div>`
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
// 21.14: SWR-backed - fetches this doctype's field metadata + record list
// exactly like before, but wrapped so a cached copy (if any) renders
// immediately with no network wait, then gets silently swapped for fresh
// data in the background. Cold start (nothing cached yet - the common case
// for a doctype no one's opened this tab session) falls back to the exact
// original await-then-render behavior, so this changes nothing about
// correctness, only repeat-visit perceived speed.
async function fetchDocTableData(doctype) {
  const metaRes = await apiFetch(`/api/v1/doc/${doctype}/meta`);
  if (!metaRes || !metaRes.ok) return undefined;
  const dataRes = await apiFetch(`/api/v1/doc/${doctype}`);
  if (!dataRes || !dataRes.ok) return undefined;
  return { fields: await metaRes.json(), records: await dataRes.json() };
}

async function renderDocTableView(container) {
  const doctypeAtRequestTime = currentDoctype;
  const cached = swrFetch(`doctable:${doctypeAtRequestTime}`, () => fetchDocTableData(doctypeAtRequestTime), (fresh) => {
    // Only apply if the user hasn't navigated to a different doctype/view
    // by the time this background revalidation resolves.
    if (currentDoctype !== doctypeAtRequestTime || currentView !== 'doctype-table') return;
    state.activeDocFields = fresh.fields;
    state.docData = fresh.records;
    renderDocTable();
  });

  if (cached) {
    state.activeDocFields = cached.fields;
    state.docData = cached.records;
  } else {
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
  }
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
      ${canCreateDoctype(currentDoctype) ? `
      <button class="btn btn-outline" onclick="openImportModal()">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        <span>Bulk Import</span>
      </button>
      <button class="btn btn-primary" onclick="openDynamicModal()">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
        <span>New ${getTranslatedLabel(currentDoctype)}</span>
      </button>` : `
      <span class="page-header-note" title="Your role has read access to this record type but not create access. Ask an administrator to grant it under Admin &raquo; Roles.">Read-only for your role</span>`}
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
      ${currentDoctype === 'Item' ? `<button class="btn btn-outline" id="pim-group-actions-btn" onclick="openPIMProductGroupActionsModal()" title="Act on a saved Product Group instead of hand-picking rows">Group Actions</button>` : ''}
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
    // Stage 30.5.2: this one placeholder serves every one of the 50+ generic
    // record lists, so it is the highest-leverage empty state in the app -
    // and "No records found." was equally uninformative whether the list was
    // genuinely empty or the user had simply typed a search that matched
    // nothing. Those are different problems with different next steps.
    const label = getTranslatedLabel(currentDoctype);
    const emptyMsg = currentSearchQuery
      ? `No ${label} records match &ldquo;${escapeHTMLText(currentSearchQuery)}&rdquo;. Clear the search box above to see all of them.`
      : `No ${label} records yet. Use <b>New ${label}</b> above to create the first one, or <b>Bulk Import</b> to load a CSV.`;
    tableHTML += `<tr><td colspan="${state.activeDocFields.length + 1 + (bulkEditingEnabled ? 1 : 0)}" class="text-center py-8 text-muted">${emptyMsg}</td></tr>`;
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
          tableHTML += `<td>${copyableCell(val, val)}</td>`;
        }
      });
      const showHistory = TAXONOMY_HISTORY_DOCTYPES.has(currentDoctype);
      // Stage 26.3.2: Purchase Requisition has no bespoke workbench (unlike
      // GRN) - its schema is flat enough that the generic doctype-table
      // form already covers create/edit. The two gaps a plain form/table
      // can't cover on its own are submitting into the (already-existing,
      // Stage 17.7) approval flow, and the post-approval conversion action -
      // both added here as row actions, same extension point as
      // showHistory above.
      const prActions = currentDoctype === 'PurchaseRequisition'
        ? (row.status === 'Draft'
            ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" onclick="submitRequisitionForApproval('${row.id}')">
                 <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
               </button>`
            : row.status === 'Approved'
              ? `<button class="action-btn" title="Convert to RFQ" style="margin-right:4px;" onclick="convertRequisition('${row.id}','RFQ')">RFQ</button>
                 <button class="action-btn" title="Convert to Purchase Order" style="margin-right:4px;" onclick="convertRequisition('${row.id}','PurchaseOrder')">PO</button>`
              : '')
        // Stage 26.9.7: QC gate - a QualityInspection is a plain flat-schema
        // Transaction doctype (same "no bespoke workbench needed" shape as
        // PurchaseRequisition above); the only gap the generic form/table
        // can't cover is submitting into the already-existing approval flow.
        // Approve/Reject itself happens on the existing Approvals inbox
        // screen, not here.
        : currentDoctype === 'QualityInspection' && row.status === 'Draft'
        ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" onclick="submitQualityInspectionForApproval('${row.id}')">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        // Stage 26.9.11: SubcontractOrder is the same "flat doctype, only
        // its state-changing actions need a row button" shape - Send moves
        // raw material out (Draft->Sent), Receive moves the processed/
        // finished good back in (Sent->Received).
        : currentDoctype === 'SubcontractOrder' && row.status === 'Draft'
        ? `<button class="action-btn" title="Send to Subcontractor" aria-label="Send ${escapeHTMLText(row.id)} to subcontractor" style="margin-right:4px;" onclick="sendSubcontractOrder('${row.id}')">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        : currentDoctype === 'SubcontractOrder' && row.status === 'Sent'
        ? `<button class="action-btn" title="Receive from Subcontractor" aria-label="Receive ${escapeHTMLText(row.id)} from subcontractor" style="margin-right:4px;" onclick="receiveSubcontractOrder('${row.id}','${row.expected_received_qty || ''}')">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
           </button>`
        // Stage 26.7.9: customer householding/merge - this row (the
        // duplicate) merges INTO another customer id the user provides.
        : currentDoctype === 'Customer' && row.status !== 'Merged'
        ? `<button class="action-btn" title="Merge Into Another Customer" aria-label="Merge ${escapeHTMLText(row.id)} into another customer" style="margin-right:4px;" onclick="mergeCustomerRow('${row.id}')">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 3h5v5"/><path d="M8 3H3v5"/><path d="M3 16v5h5"/><path d="M16 21h5v-5"/><line x1="3" y1="3" x2="21" y2="21"/></svg>
           </button>`
        // Stage 26.8.8/26.8.10: Appraisal/Grievance are the same "flat
        // doctype, only the submit-for-approval action is bespoke" shape
        // QualityInspection already uses above.
        : (currentDoctype === 'Appraisal' || currentDoctype === 'Grievance') && row.status === 'Draft'
        ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" onclick="submitDocForApproval('${currentDoctype}','${row.id}')">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        : '';
      tableHTML += `
        <td style="text-align: right;">
          ${showHistory ? `<button class="action-btn" title="History" aria-label="History for ${escapeHTMLText(row.id)}" style="margin-right:4px;" onclick="viewTaxonomyHistory('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          </button>` : ''}
          ${prActions}
          ${canUpdateDoctype(currentDoctype) ? `
          <button class="action-btn" title="Edit" aria-label="Edit ${escapeHTMLText(row.id)}" style="margin-right:4px;" onclick="editDocRecord('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
          </button>` : ''}
          ${canDeleteDoctype(currentDoctype) ? `
          <button class="action-btn action-btn-danger" title="Delete" aria-label="Delete ${escapeHTMLText(row.id)}" onclick="deleteDocRecord('${row.id}')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>
          </button>` : ''}
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

// submitRequisitionForApproval (Stage 26.3.2) posts through the same
// generic /api/v1/approval/submit endpoint submitPOForApproval/
// submitExpenseForApproval already use for their own doctypes - Stage 17.7's
// approval_rules for PurchaseRequisition were already configured, this was
// only ever missing a caller.
window.submitRequisitionForApproval = async function(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'PurchaseRequisition', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  renderView('doctype-table');
};

// convertRequisition (Stage 26.3.2) is the frontend for
// engines.ConvertRequisitionToOrder (Stage 17.7), which already existed and
// was already routed (POST /api/v1/procurement/convert-requisition) but had
// no UI action calling it. store_code/financial_year aren't stored on the
// requisition itself, so they're asked for here, same as GRN's own workbench
// asks for what its source document doesn't carry.
window.convertRequisition = async function(requisitionId, target) {
  const storeCode = await showCustomPrompt('Store code for the new document:', 'HO', `Convert to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'}`);
  if (!storeCode) return;
  const financialYear = await showCustomPrompt('Financial year (e.g. 26-27):', '', `Convert to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'}`);
  if (!financialYear) return;
  const res = await apiFetch('/api/v1/procurement/convert-requisition', {
    method: 'POST',
    body: JSON.stringify({ requisition_id: requisitionId, target, store_code: storeCode, financial_year: financialYear })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to convert requisition.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(`Converted to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'} ${data.new_document_id}.`, 'Requisition Converted');
  renderView('doctype-table');
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
    ? `<tr><td colspan="3" class="text-center text-muted">No history recorded yet. Entries appear here the first time this record is edited.</td></tr>`
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

// openPIMProductGroupActionsModal (Stage 36.1.3) is the Product Group's
// production consumer surface: pick a saved static or dynamic group, see how
// many products it resolves to right now, then either export it or bulk edit
// it. It reuses the same .modal-overlay primitives and the same
// /api/v1/pim/bulk-edit endpoint as the selection-based path above - the only
// difference is that the server resolves the target list from the group.
window.openPIMProductGroupActionsModal = async function() {
  const groupsRes = await apiFetch('/api/v1/doc/PIMProductGroup');
  if (!groupsRes) return;
  if (!groupsRes.ok) {
    await showApiError(groupsRes, 'Could not load product groups.');
    return;
  }
  const groups = (await groupsRes.json()).filter(g => (g.status || 'Active') === 'Active');
  if (groups.length === 0) {
    showCustomAlert('No active product groups exist yet. Create one under PIM » Product Group first.', 'Group Actions');
    return;
  }
  const fields = (state.activeDocFields || []).filter(field => field.fieldname !== 'id' && field.fieldname !== 'code');

  document.getElementById('pim-group-actions-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-group-actions-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Product Group Actions</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label" for="pim-group-select">Product group</label>
          <select class="form-select" id="pim-group-select">${groups.map(g => `<option value="${escapeHTMLText(g.id)}">${escapeHTMLText(g.name || g.id)} (${escapeHTMLText(g.group_type || '')})</option>`).join('')}</select>
        </div>
        <p class="text-muted" style="font-size:13px;" id="pim-group-count">Resolving membership&hellip;</p>
        <div class="form-group"><label class="form-label" for="pim-group-field">Field to edit</label><select class="form-select" id="pim-group-field">${fields.map(f => `<option value="${escapeHTMLText(f.fieldname)}">${escapeHTMLText(getTranslatedLabel(f.label))}</option>`).join('')}</select></div>
        <div class="form-group"><label class="form-label" id="pim-group-value-label">New value</label><div id="pim-group-value"></div></div>
        <p class="text-muted" style="font-size:13px; margin:0;">A dynamic group is re-resolved by the server at the moment you confirm, so the edit applies to whatever matches then - not to the count shown above if the catalog changed in between.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" id="pim-group-cancel">Cancel</button>
        <button type="button" class="btn btn-outline" id="pim-group-export">Export CSV</button>
        <button type="button" class="btn btn-primary" id="pim-group-edit">Bulk edit group</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-group-cancel').addEventListener('click', close);

  const groupSelect = overlay.querySelector('#pim-group-select');
  const countEl = overlay.querySelector('#pim-group-count');
  const refreshCount = async () => {
    countEl.textContent = 'Resolving membership…';
    const res = await apiFetch(`/api/v1/pim/product-groups/${encodeURIComponent(groupSelect.value)}/members`);
    if (!res || !res.ok) { countEl.textContent = 'Could not resolve this group right now.'; return; }
    const resolved = await res.json();
    countEl.textContent = `${resolved.member_count} product${resolved.member_count === 1 ? '' : 's'} currently in this group.`;
  };
  groupSelect.addEventListener('change', refreshCount);

  const fieldSelect = overlay.querySelector('#pim-group-field');
  const renderValueInput = () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const holder = overlay.querySelector('#pim-group-value');
    holder.replaceChildren();
    if (!field) return;
    let input;
    if (field.fieldtype === 'Select') {
      input = document.createElement('select');
      input.className = 'form-select';
      (field.options || '').split(',').filter(Boolean).forEach(value => {
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
    input.id = 'pim-group-value-input';
    holder.appendChild(input);
    overlay.querySelector('#pim-group-value-label').textContent = `New ${getTranslatedLabel(field.label)}`;
  };
  fieldSelect.addEventListener('change', renderValueInput);
  renderValueInput();

  overlay.querySelector('#pim-group-export').addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/pim/product-groups/${encodeURIComponent(groupSelect.value)}/export.csv`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to export this product group.'); return; }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `product_group_${groupSelect.value.replace(/[^A-Za-z0-9_-]/g, '_')}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });

  overlay.querySelector('#pim-group-edit').addEventListener('click', async () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const input = overlay.querySelector('#pim-group-value-input');
    if (!field || !input) return;
    if (field.mandatory && String(input.value).trim() === '') return;
    const value = field.fieldtype === 'Number' ? Number(input.value) : input.value;
    const groupName = groupSelect.options[groupSelect.selectedIndex].textContent;
    if (!await showCustomConfirm(`Update every product currently in ${groupName}?`, 'Confirm Group Bulk Edit')) return;
    const res = await apiFetch('/api/v1/pim/bulk-edit', {
      method: 'POST',
      body: JSON.stringify({ doctype: currentDoctype, group_id: groupSelect.value, field: field.fieldname, value })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Group bulk edit failed. No records were changed.'); return; }
    const result = await res.json();
    close();
    await showCustomAlert(`${result.updated_count} record${result.updated_count === 1 ? '' : 's'} updated.`, 'Bulk Edit Complete');
    renderView('doctype-table');
  });

  await refreshCount();
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
  const isPurchaseRequisition = currentDoctype === 'PurchaseRequisition';

  for (const f of state.activeDocFields) {
    if (f.fieldname === 'id') continue;
    // Stage 30.5.6: f.mirrored marks the derived half of a duplicate
    // mandatory pair (PurchaseOrder's vendor_id against vendor). The server
    // copies it across at the same choke point that numbers the document, so
    // asking for it here would be asking the user to type the same value into
    // a second identically-named required box. The flag is computed from the
    // registry that does the copying, not from a doctype list kept here.
    if (f.mirrored) continue;

    // f.auto_generated (Stage 30.6) is stamped on by the server for the
    // document-number fields it issues itself, so this form stops asking for
    // a value it would discard. It comes from the same registry that assigns
    // the number (engines/document_numbering.go), rather than a copy of the
    // doctype list kept here.
    const isCodeField = f.auto_generated || ((isMaster || isPurchaseRequisition) && f.fieldname.toLowerCase() === 'code');
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
          // Stage 30.5.1: an empty target list used to render as a dropdown
          // containing only "— Select Reference —" with no indication that
          // anything was wrong, let alone what to do about it. This is the
          // single choke point for every Link field on every generic form,
          // so attaching the affordance here covers all of them at once
          // (10 of the 18 core master lists were empty at audit time).
          if (!data || data.length === 0) {
            select.innerHTML = `<option value="" disabled selected>— No ${getTranslatedLabel(f.options)} records yet —</option>`;
            fg.insertAdjacentHTML('beforeend', emptyPickerHint(f.options));
            return;
          }
          select.innerHTML = '<option value="" disabled selected>— Select Reference —</option>';
          data.forEach(item => {
            // Value must be item.id - that's what the backend's Link
            // existence check (engines.ValidateDocument) actually verifies
            // against. Using item.name here (pre-18.2 fix) stored the wrong
            // value for any target doctype whose `name` differs from its
            // `id`/`code` (Vendor, Customer, Location, Item all qualify),
            // silently breaking the Link constraint it was meant to enforce.
            select.innerHTML += `<option value="${item.id}">${item.name || item.code || item.id}</option>`;
          });
          if (existingVal !== undefined && existingVal !== null) select.value = existingVal;
        });
      });
    } else if (f.fieldtype === 'JSONTable' || f.fieldtype === 'JSONMap') {
      // Stage 30.5.3. The editor writes its serialised value into a hidden
      // input carrying the field's own name, so handleDynamicFormSubmit -
      // which reads `[name="<fieldname>"]`.value - needed no change at all,
      // and the stored representation is byte-identical to what a user used
      // to hand-type. Every Go consumer keeps reading exactly what it read.
      // Stage 33: the form is a multi-column grid now, and this editor is a
      // table - it takes the whole row rather than a 240px column.
      fg.classList.add('form-group-wide');
      renderJSONLineEditor(fg, f, existingVal);
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
        } else if (f.auto_generated || isPurchaseRequisition) {
          input.placeholder = 'Auto-generated from Prefix Configs on save';
        } else {
          input.placeholder = 'Auto-generated upon save';
        }
      } else if (isDerivedCompanionField(f.fieldname)) {
        // Stage 41: a derived companion ("phone_country") is written by the
        // server from another field's value - the phone engine resolves it
        // from the number itself. Showing it as an empty box invites a user to
        // type something that will be overwritten on the very next save, so it
        // is presented the same way an auto-generated code is: visible,
        // read-only, and labelled with where its value comes from.
        input.readOnly = true;
        input.required = false;
        input.value = existingVal ?? '';
        if (!input.value) input.placeholder = 'Set automatically on save';
      } else {
        input.required = f.mandatory;
        if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      }
      fg.appendChild(input);
    }
    body.appendChild(fg);
  }

  // The requirement catalogue is deliberately a soft suggestion rather than
  // a restrictive Link field: new wording is valid, and the server learns it
  // into PurchaseRequisitionDescription on save. Department is an existing
  // Core master and uses the same picker, returning its code for the stored
  // requisition value.
  if (isPurchaseRequisition) {
    const descriptionInput = body.querySelector('[name="description"]');
    const departmentInput = body.querySelector('[name="department"]');
    if (descriptionInput) {
      attachLinkTypeahead(descriptionInput, 'PurchaseRequisitionDescription', {
        valueFields: ['description'],
        labelFn: doc => doc.description || doc.code || doc.id,
        // Not something a user "sets up": the server learns each new wording
        // into this catalogue on save, so a hint telling them to go create one
        // would be advising them to do by hand what already happens by itself.
        noSetupHint: true
      });
    }
    if (departmentInput) attachLinkTypeahead(departmentInput, 'Department');
  }

  // Stage 33: only the long forms get the wide, multi-column dialog. Item
  // renders ~20 fields and was taller than any screen in one column; a
  // 3-field master in the same 920px box would just be empty space. The
  // column count itself is the stylesheet's job (an intrinsic grid), so
  // this is the one thing that genuinely needs the field count.
  const container = modal.querySelector('.modal-container');
  if (container) {
    container.classList.toggle('modal-container-wide', body.querySelectorAll('.form-group').length > 4);
  }

  // Stage 41: this form is built into the modal, not into #view-root, so
  // renderView's sweep never sees it. One call here covers every generic
  // record form in the product - which is where most phone fields actually
  // live (Customer, Vendor, Employee, Location).
  applyPhoneRulesIn(body);

  modal.classList.add('open');
};

// 21.9 QA-follow-up: the generic record-list screens (Vendors,
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
  const isPurchaseRequisition = currentDoctype === 'PurchaseRequisition';
  let codeFieldname = null;

  state.activeDocFields.forEach(f => {
    if (f.fieldname === 'id') return;
    // Mirrored fields (30.5.6) have no input on the form - the server fills
    // them from their primary. Sending an empty string would overwrite the
    // copy it just made.
    if (f.mirrored) return;
    // Stage 30.6: f.auto_generated joins PurchaseRequisition as a field the
    // server numbers during the save itself, so it must not be routed to the
    // admin-only /api/v1/sequence endpoint below (a Store Manager creating a
    // PO would get a 403 from it) and must not be sent at all - a supplied id
    // is treated as an upsert, which would turn a create into an overwrite.
    const isServerNumbered = f.auto_generated || (isPurchaseRequisition && f.fieldname.toLowerCase() === 'code');
    const isCodeField = isServerNumbered || (isMaster && f.fieldname.toLowerCase() === 'code');
    const input = form.querySelector(`[name="${f.fieldname}"]`);
    if (input) {
      if (isCodeField && !input.value) {
        // Master records retain the existing admin sequence endpoint behavior.
        if (!isServerNumbered) codeFieldname = f.fieldname;
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
    // Stage 41: the record that was just created may be the first of its
    // record type, which means every "No Vendors have been set up yet" hint
    // in the app is now wrong. Refreshed before the re-render so the screen
    // the user lands on is already correct. Awaited rather than fired off,
    // because the render below reads the result.
    await refreshSetupStatus();
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

  // Stage 41: the row's fields are stashed on the element rather than
  // re-fetched or serialised into the onclick attribute - `options` is free
  // text that can contain quotes, which an inline attribute would break on.
  const rowsHTML = fields.map((f, i) => `
      <tr>
        <td style="font-family: monospace;">${escapeHTMLText(f.fieldname)}</td>
        <td>${escapeHTMLText(f.label)}</td>
        <td>${escapeHTMLText(f.fieldtype)}</td>
        <td>${f.mandatory ? 'Yes' : 'No'}</td>
        <td>${escapeHTMLText(f.options || '—')}</td>
        <td>${f.display_order}</td>
        <td>
          <button class="action-btn" onclick="editFieldConfig('${doctypeName}', ${i})">Edit</button>
          <button class="action-btn action-btn-danger" onclick="deleteFieldConfig('${doctypeName}', '${f.id}')">Delete</button>
        </td>
      </tr>
    `).join('');

  html += rowsHTML || `<tr><td colspan="7">No fields defined on this record type yet &mdash; use <b>Add Field</b> above.</td></tr>`;
  html += `</tbody></table>`;
  container.innerHTML = html;
  container.__fields = fields;
};

// --- Schema field add/edit (Stage 41) ---------------------------------
//
// Both verbs go through one modal (#add-field-modal in index.html, which had
// been sitting there unreferenced since it was written). Before this the
// screen had no edit at all and created fields through a chain of six
// prompt() dialogs - each one losing everything typed so far if cancelled,
// with fieldtype typed free-hand against a list the server would then reject.
//
// openFieldModal(doctype, existing) is the single entry point: `existing`
// null = create (POST), a field row = edit (PUT to that field's id).
function openFieldModal(doctypeName, existing) {
  const modal = document.getElementById('add-field-modal');
  if (!modal) return;
  document.getElementById('add-field-doctype').value = doctypeName;
  document.getElementById('add-field-id').value = existing ? existing.id : '';
  document.getElementById('add-field-name').value = existing ? existing.fieldname : '';
  document.getElementById('add-field-label').value = existing ? existing.label : '';
  document.getElementById('add-field-type').value = existing ? existing.fieldtype : 'Data';
  document.getElementById('add-field-mandatory').checked = existing ? !!existing.mandatory : false;
  document.getElementById('add-field-options').value = existing ? (existing.options || '') : '';
  // A new field lands after everything already defined rather than at a fixed
  // 10, which is what the old prompt flow hardcoded - so several added fields
  // no longer all collide on the same order.
  const fields = (document.getElementById('doctype-fields-config') || {}).__fields || [];
  const nextOrder = fields.reduce((m, f) => Math.max(m, Number(f.display_order) || 0), 0) + 1;
  document.getElementById('add-field-order').value = existing ? existing.display_order : nextOrder;

  document.getElementById('add-field-modal-title').textContent =
    existing ? `Edit Field on ${doctypeName}` : `Add Field to ${doctypeName}`;
  document.getElementById('add-field-submit').textContent = existing ? 'Save Changes' : 'Add Field';
  document.getElementById('add-field-rename-warning').classList.toggle('hidden', !existing);
  const err = document.getElementById('add-field-error');
  err.classList.add('hidden');
  err.textContent = '';
  modal.classList.add('open');
  document.getElementById('add-field-name').focus();
}

window.addNewFieldConfig = function(doctypeName) {
  openFieldModal(doctypeName, null);
};

window.editFieldConfig = function(doctypeName, index) {
  const fields = (document.getElementById('doctype-fields-config') || {}).__fields || [];
  const f = fields[index];
  if (!f) return;
  openFieldModal(doctypeName, f);
};

window.closeAddFieldModal = function() {
  const modal = document.getElementById('add-field-modal');
  if (!modal) return;
  modal.classList.remove('open');
  document.getElementById('add-field-form').reset();
  document.getElementById('add-field-id').value = '';
};

window.submitAddField = async function(event) {
  event.preventDefault();
  const doctypeName = document.getElementById('add-field-doctype').value;
  const fieldID = document.getElementById('add-field-id').value;
  const errEl = document.getElementById('add-field-error');

  const body = JSON.stringify({
    fieldname: document.getElementById('add-field-name').value.trim(),
    label: document.getElementById('add-field-label').value.trim(),
    fieldtype: document.getElementById('add-field-type').value,
    mandatory: document.getElementById('add-field-mandatory').checked,
    options: document.getElementById('add-field-options').value.trim(),
    display_order: Number(document.getElementById('add-field-order').value) || 0
  });

  const res = fieldID
    ? await apiFetch(`/api/v1/meta/${doctypeName}/fields/${fieldID}`, { method: 'PUT', body })
    : await apiFetch(`/api/v1/meta/${doctypeName}/fields`, { method: 'POST', body });
  if (!res) return;
  if (!res.ok) {
    // Shown inline in the modal, not as a separate dialog over it - the user
    // keeps everything they typed and can correct the one bad value.
    errEl.textContent = await getErrorMessage(res, fieldID ? 'Failed to update field.' : 'Failed to add field.');
    errEl.classList.remove('hidden');
    return;
  }
  closeAddFieldModal();
  // The schema just changed, so the cached doctype/field metadata the record
  // forms render from is now stale.
  await fetchRegisteredDoctypes();
  loadDoctypeConfig(doctypeName);
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

// prefixConfigSample renders what the next number of a series will look like,
// mirroring engines/numbering.go's own assembly order:
//   <Prefix><Sep>[<Store><Sep>][<Period><Sep>]<Padded>
// Shown on the admin screen so the effect of a prefix/separator/padding/reset
// change is visible before it is applied to real documents.
function prefixConfigSample(c) {
  const parts = [c.prefix];
  if (c.include_store !== false) parts.push('HQ');
  const reset = (c.reset_frequency || 'ANNUAL').toUpperCase();
  // NEVER is the only setting with no period segment - it never resets, so
  // there is no period to name.
  if (reset === 'MONTHLY') parts.push('26-27-04');
  else if (reset !== 'NEVER') parts.push('26-27');
  parts.push(String(1).padStart(c.padding_width || 1, '0'));
  return parts.join(c.separator);
}

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
      <p class="page-subtitle">Number series for every transaction document. Purchase orders, goods receipts, transfers, claims and the rest draw their number from here when they are saved - nobody types one in.</p>
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
          <th>Store Segment</th>
          <th>Next Number Looks Like</th>
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
        <td>${c.include_store === false ? 'No' : 'Yes'}</td>
        <td style="font-family: monospace;">${prefixConfigSample(c)}</td>
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
  // Reset interval decides both how often the counter restarts and whether the
  // number carries a period segment at all: a series that restarts every year
  // without showing the year would re-issue last year's numbers, and since the
  // number is also the document id that is a rejected save, not a cosmetic
  // problem. NEVER is therefore the only way to get a number with no period.
  const reset = await showCustomPrompt('Reset Interval - ANNUAL (PO/HQ/26-27/000001), MONTHLY (PO/HQ/26-27-04/000001), or NEVER (PO/HQ/000001, one continuous series):', c.reset_frequency);
  if (!reset) return;
  const storeRaw = await showCustomPrompt('Include the store code in the number? Yes keeps each location numbering separately (PO/HQ/...); No gives one shared series across all locations (PO/...):', c.include_store === false ? 'No' : 'Yes');
  if (storeRaw === null) return;
  const includeStore = !/^n/i.test(String(storeRaw).trim());

  const res = await apiFetch('/api/v1/prefix', {
    method: 'POST',
    body: JSON.stringify({
      doc_type: docType,
      prefix,
      separator,
      padding_width: padding,
      reset_frequency: String(reset).trim().toUpperCase(),
      active_status: true,
      include_store: includeStore
    })
  });
  if (!res) return;
  if (res.ok) {
    renderView('prefix-configs');
  } else {
    await showApiError(res, 'Failed to save prefix configuration.');
  }
};

// Approval Rules admin screen (Stage 26.3.3). Exposes the amount-slab ->
// required-role routing config (engines/approval.go) for editing - the
// backend (GetApprovalRules/UpsertApprovalRule, Stage 24.8; DeleteApprovalRule,
// added alongside this screen) already existed, this was only ever missing a UI.
async function renderApprovalRulesView(container) {
  const [rulesRes, rolesRes] = await Promise.all([
    apiFetch('/api/v1/approval/rules'),
    apiFetch('/api/v1/admin/roles')
  ]);
  if (!rulesRes) return;
  if (!rulesRes.ok) {
    const msg = await getErrorMessage(rulesRes, 'Failed to load approval rules.');
    renderErrorPanel(container, msg, () => renderView('approval-rules'));
    return;
  }
  state.approvalRules = await rulesRes.json();
  state.approvalRuleRoles = (rolesRes && rolesRes.ok) ? await rolesRes.json() : [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Approval Rules</h1>
      <p class="page-subtitle">Amount-slab to role routing for every approval-gated document type.</p>
    </div>
    <button class="btn btn-primary" onclick="openApprovalRuleModal()">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="margin-right: 6px;"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
      <span>New Rule</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Doctype</th><th>Min Amount</th><th>Max Amount</th><th>Required Role</th><th style="text-align:right;">Actions</th></tr></thead>
        <tbody>
          ${state.approvalRules.length === 0 ? '<tr><td colspan="5" style="text-align:center; color:var(--text-muted);">No approval rules configured. Use <b>Save Rule</b> above to require approval for a record type &mdash; without a rule, documents skip maker-checker entirely.</td></tr>' : state.approvalRules.map(r => `
            <tr>
              <td style="font-weight:600;">${r.doctype}</td>
              <td>${r.min_amount}</td>
              <td>${r.max_amount == null ? 'No limit' : r.max_amount}</td>
              <td><span class="badge badge-secondary">${r.required_role}</span></td>
              <td style="text-align:right;">
                <button class="action-btn" title="Edit" aria-label="Edit the ${escapeHTMLText(r.doctype)} approval rule" style="margin-right:4px;" onclick="openApprovalRuleModal(${r.id})">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                </button>
                <button class="action-btn action-btn-danger" title="Delete" aria-label="Delete the ${escapeHTMLText(r.doctype)} approval rule" onclick="deleteApprovalRuleRow(${r.id})">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>
                </button>
              </td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(panel);
}

// openApprovalRuleModal: same real-form-modal pattern as the PIM bulk-edit
// modal (.modal-overlay/.modal-container, a <form> with a submit handler) -
// ruleId omitted opens it in create mode, otherwise pre-fills from
// state.approvalRules (looked up by id rather than embedding the row's JSON
// into an inline onclick attribute, which would need escaping doctype/role
// values that could contain quotes).
window.openApprovalRuleModal = function(ruleId) {
  const rule = ruleId != null ? state.approvalRules.find(r => r.id === ruleId) : null;
  const roleOptions = (state.approvalRuleRoles || []).length > 0
    ? state.approvalRuleRoles.map(role => `<option value="${role}" ${rule && rule.required_role === role ? 'selected' : ''}>${role}</option>`).join('')
    : `<option value="Store Manager">Store Manager</option><option value="Super Admin">Super Admin</option>`;

  document.getElementById('approval-rule-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'approval-rule-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">${rule ? 'Edit' : 'New'} Approval Rule</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <form>
        <div class="modal-body">
          <div class="form-group"><label class="form-label">Doctype</label><input type="text" class="form-input" id="ar-doctype" value="${rule ? rule.doctype : ''}" placeholder="e.g. PurchaseOrder" required></div>
          <div class="form-group"><label class="form-label">Min Amount</label><input type="number" step="0.01" class="form-input" id="ar-min" value="${rule ? rule.min_amount : '0'}" required></div>
          <div class="form-group"><label class="form-label">Max Amount (blank = no upper bound)</label><input type="number" step="0.01" class="form-input" id="ar-max" value="${rule && rule.max_amount != null ? rule.max_amount : ''}"></div>
          <div class="form-group"><label class="form-label">Required Role</label><select class="form-select" id="ar-role">${roleOptions}</select></div>
        </div>
        <div class="modal-footer"><button type="button" class="btn btn-secondary">Cancel</button><button type="submit" class="btn btn-primary">Save Rule</button></div>
      </form>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
  overlay.querySelector('form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const doctype = document.getElementById('ar-doctype').value.trim();
    const minAmount = Number(document.getElementById('ar-min').value);
    const maxRaw = document.getElementById('ar-max').value.trim();
    const requiredRole = document.getElementById('ar-role').value;
    if (!doctype) return;
    const res = await apiFetch('/api/v1/approval/rules', {
      method: 'POST',
      body: JSON.stringify({
        id: rule ? rule.id : undefined,
        doctype, min_amount: minAmount,
        max_amount: maxRaw === '' ? null : Number(maxRaw),
        required_role: requiredRole
      })
    });
    if (!res) return;
    if (!res.ok) {
      await showApiError(res, 'Failed to save approval rule.');
      return;
    }
    close();
    renderView('approval-rules');
  });
};

window.deleteApprovalRuleRow = async function(ruleId) {
  if (!await showCustomConfirm('Delete this approval rule? Documents whose amount only matched this slab will no longer be approval-gated once it is removed.')) return;
  const res = await apiFetch(`/api/v1/approval/rules?id=${encodeURIComponent(ruleId)}`, { method: 'DELETE' });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to delete approval rule.');
    return;
  }
  renderView('approval-rules');
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
          ? `<tr><td colspan="8" style="text-align:center; color:var(--text-muted);">No extension hooks registered yet. Use <b>Register Hook</b> above to let an external system subscribe to an event.</td></tr>`
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
          ? `<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No calls logged yet for this hook. Entries appear here the first time its event fires.</td></tr>`
          : entries.map(e => `
            <tr>
              <td>${e.called_at ? new Date(e.called_at).toLocaleString() : ''}</td>
              <td>${e.response_status != null ? `<span class="badge ${e.response_status >= 200 && e.response_status < 300 ? 'badge-success' : 'badge-danger'}">${e.response_status}</span>` : '<span class="badge badge-secondary">-</span>'}</td>
              <td>${e.latency_ms}ms</td>
              <td style="color:var(--danger-color);">${e.error || ''}</td>
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
        ${auditLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load audit logs.</p>` : ''}
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
              ${auditLogs.length === 0 ? '<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No audit logs found for the current filter. Widen the date range or clear the filter above.</td></tr>' : auditLogs.map(l => `
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
        ${sysLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load system logs.</p>` : ''}
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
              ${systemLogs.length === 0 ? '<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No system logs found for the current filter. Widen the date range or clear the filter above.</td></tr>' : systemLogs.map(l => `
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
        ${intLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load integration logs.</p>` : ''}
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
              ${intLogs.length === 0 ? '<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No integration payloads found for the current filter. Widen the date range or clear the filter above.</td></tr>' : intLogs.map(l => `
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

// Configuration (Stage 28.1): the module-by-module admin Settings screen.
// Fully generic - it renders whatever engines/settings_registry.go declares,
// one section per module, each setting drawn by its declared type (number/
// toggle/text/select). Registering a setting on the backend makes it appear
// here with no frontend change. HR/Admin only (GET/PUT /api/v1/admin/settings
// enforce requireHRAdmin server-side).
let configSettings = [];       // full definitions+values from the server
let configDirty = {};          // key -> new value, only for changed keys
let configSelectedModule = '';

function cfgEsc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

async function renderConfigurationView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Configuration</h1>
      <p class="page-subtitle">System settings, organized by module. Nothing here is hardcoded - the system reacts to whatever you set.</p>
    </div>
  `;
  container.appendChild(header);

  const res = await apiFetch('/api/v1/admin/settings');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load configuration.'); return; }
  configSettings = await res.json();
  configDirty = {};

  if (!Array.isArray(configSettings) || configSettings.length === 0) {
    const empty = document.createElement('p');
    empty.className = 'page-subtitle';
    empty.textContent = 'No configurable settings are registered.';
    container.appendChild(empty);
    return;
  }

  const modules = [];
  configSettings.forEach(s => { if (!modules.includes(s.module)) modules.push(s.module); });
  // Stage 30.7: Integrations is a synthetic module appended to the registry-
  // driven rail. Its values (Pine Labs terminals, Unicommerce middleware
  // stores) are multi-row credential records rather than single scalars, so
  // they can't live in the key/value settings registry - but the endpoint
  // URLs belong on this screen with everything else, not on a separate page
  // the admin has to know exists. Rendered by renderConfigIntegrations().
  modules.push(CONFIG_INTEGRATIONS_MODULE);
  if (!configSelectedModule || !modules.includes(configSelectedModule)) configSelectedModule = modules[0];

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.cssText = 'display:grid; grid-template-columns:220px 1fr; min-height:440px; overflow:hidden;';
  panel.innerHTML = `
    <div id="config-modules" style="border-right:1px solid var(--border-color); padding:12px 0; background:var(--bg-color);"></div>
    <div style="display:flex; flex-direction:column; min-width:0;">
      <div id="config-fields" style="padding:20px 24px; flex:1; overflow:auto;"></div>
      <div style="padding:14px 24px; border-top:1px solid var(--border-color); display:flex; align-items:center; gap:14px;">
        <button class="btn btn-primary" id="config-save-btn" disabled>Save Changes</button>
        <span id="config-dirty-note" class="page-subtitle" style="margin:0; font-size:13px;"></span>
      </div>
    </div>
  `;
  container.appendChild(panel);

  renderConfigModuleRail(modules);
  renderConfigFields();
  document.getElementById('config-save-btn').addEventListener('click', saveConfiguration);
}

function renderConfigModuleRail(modules) {
  const rail = document.getElementById('config-modules');
  if (!rail) return;
  rail.innerHTML = modules.map(m => {
    const active = m === configSelectedModule;
    return `<a class="config-module-item" data-module="${cfgEsc(m)}"
       style="display:block; padding:10px 20px; cursor:pointer; font-size:14px; font-weight:500;
              border-left:3px solid ${active ? 'var(--primary-color)' : 'transparent'};
              color:${active ? 'var(--text-main)' : 'var(--text-muted)'};
              background:${active ? 'var(--panel-bg)' : 'transparent'};">${cfgEsc(m)}</a>`;
  }).join('');
  rail.querySelectorAll('.config-module-item').forEach(el => {
    el.addEventListener('click', () => {
      configSelectedModule = el.getAttribute('data-module');
      renderConfigModuleRail(modules);
      renderConfigFields();
    });
  });
}

function configInputId(key) { return 'config-input-' + key; }

// --- Integrations (Stage 30.7) -------------------------------------------
// Endpoint/credential config for the two external systems this ERP talks to:
// Pine Labs (card terminals, keyed by terminal_id) and Unicommerce (the OMS
// middleware, keyed by store_code). Both already had save/list endpoints and a
// DB table but no screen at all, so a base URL could only be changed with a
// hand-rolled API call. Each save POSTs to the existing endpoint, which
// upserts - so re-saving the same terminal/store updates it in place, and the
// running workers pick the new URL up on their next call (they read the
// credential row per call, never cache it at startup).
const CONFIG_INTEGRATIONS_MODULE = 'Integrations';

const CONFIG_INTEGRATION_DEFS = [
  {
    id: 'pinelabs',
    title: 'Pine Labs payment terminals',
    blurb: 'Plutus terminal credentials used by POS card payments and the reconciliation worker. One entry per terminal.',
    listUrl: '/api/v1/pinelabs/credentials',
    saveUrl: '/api/v1/pinelabs/credentials',
    keyField: 'terminal_id',
    fields: [
      { name: 'terminal_id', label: 'Terminal ID' },
      { name: 'merchant_id', label: 'Merchant ID' },
      { name: 'api_key', label: 'API key', secret: true },
      { name: 'base_url', label: 'Base URL', wide: true }
    ]
  },
  {
    id: 'unicommerce',
    title: 'Unicommerce (OMS middleware)',
    blurb: 'Middleware endpoint and API credentials used for order push and inventory sync. One entry per store code.',
    listUrl: '/api/v1/unicommerce/credentials',
    saveUrl: '/api/v1/unicommerce/credentials',
    keyField: 'store_code',
    fields: [
      { name: 'store_code', label: 'Store code' },
      { name: 'api_key', label: 'API key', secret: true },
      { name: 'api_secret', label: 'API secret', secret: true },
      { name: 'base_url', label: 'Base URL', wide: true }
    ]
  }
];

async function renderConfigIntegrations(host) {
  host.innerHTML = '<p class="page-subtitle">Loading integrations…</p>';
  const results = await Promise.all(CONFIG_INTEGRATION_DEFS.map(async def => {
    try {
      const res = await apiFetch(def.listUrl);
      if (!res || !res.ok) return { def, rows: [], unavailable: true };
      const body = await res.json();
      return { def, rows: Array.isArray(body) ? body : (body && Array.isArray(body.data) ? body.data : []) };
    } catch (e) {
      return { def, rows: [], unavailable: true };
    }
  }));

  host.innerHTML = results.map(r => configIntegrationSectionHtml(r.def, r.rows, r.unavailable)).join('');
  results.forEach(r => wireConfigIntegrationSection(r.def));
}

function configIntegrationSectionHtml(def, rows, unavailable) {
  const existing = rows.length
    ? `<table class="data-table" style="margin:10px 0 14px; width:100%;">
         <thead><tr>${def.fields.map(f => `<th>${cfgEsc(f.label)}</th>`).join('')}<th>Active</th></tr></thead>
         <tbody>${rows.map(row => `<tr>${def.fields.map(f => {
           const v = row[f.name] == null ? '' : String(row[f.name]);
           return `<td>${f.secret && v ? '••••••' : cfgEsc(v)}</td>`;
         }).join('')}<td>${row.active === false ? 'No' : 'Yes'}</td></tr>`).join('')}</tbody>
       </table>`
    : `<p class="page-subtitle" style="margin:8px 0 14px; font-size:13px;">${unavailable
        ? 'This integration is not enabled for your account, or its credentials could not be read.'
        : 'No entries configured yet.'}</p>`;

  const inputs = def.fields.map(f => `
    <div class="form-group" style="margin:0 0 12px; ${f.wide ? 'grid-column:1/-1;' : ''}">
      <label class="form-label" for="cfgint-${def.id}-${f.name}">${cfgEsc(f.label)}</label>
      <input type="${f.secret ? 'password' : 'text'}" class="form-input"
             id="cfgint-${def.id}-${f.name}" autocomplete="off"
             ${f.name === 'base_url' ? 'placeholder="https://…"' : ''}>
    </div>`).join('');

  return `
    <section style="margin-bottom:30px; max-width:760px;">
      <h3 style="margin:0 0 4px; font-size:15px; font-weight:600;">${cfgEsc(def.title)}</h3>
      <p class="page-subtitle" style="margin:0 0 4px; font-size:12.5px;">${cfgEsc(def.blurb)}</p>
      ${existing}
      <div style="display:grid; grid-template-columns:1fr 1fr; gap:0 16px;">${inputs}</div>
      <button class="btn btn-secondary" id="cfgint-save-${def.id}">Save ${cfgEsc(def.title.split(' ')[0])} entry</button>
      <span class="page-subtitle" style="margin-left:10px; font-size:12.5px;">Saving an existing ${cfgEsc(def.keyField.replace('_', ' '))} updates it in place.</span>
    </section>`;
}

function wireConfigIntegrationSection(def) {
  const btn = document.getElementById(`cfgint-save-${def.id}`);
  if (!btn) return;
  btn.addEventListener('click', async () => {
    const payload = {};
    for (const f of def.fields) {
      const el = document.getElementById(`cfgint-${def.id}-${f.name}`);
      payload[f.name] = el ? el.value.trim() : '';
      if (!payload[f.name]) {
        showToast(`${f.label} is required.`, { variant: 'error' });
        if (el) el.focus();
        return;
      }
    }
    btn.disabled = true;
    const res = await apiFetch(def.saveUrl, { method: 'POST', body: JSON.stringify(payload) });
    btn.disabled = false;
    if (!res) return;
    if (!res.ok) { await showApiError(res, `Failed to save ${def.title}.`); return; }
    showToast(`${def.title} saved.`, { variant: 'success' });
    renderConfigFields();
  });
}

function renderConfigFields() {
  const host = document.getElementById('config-fields');
  if (!host) return;
  if (configSelectedModule === CONFIG_INTEGRATIONS_MODULE) {
    renderConfigIntegrations(host);
    updateConfigDirtyState();
    return;
  }
  const items = configSettings.filter(s => s.module === configSelectedModule);
  host.innerHTML = items.map(configFieldHtml).join('');
  items.forEach(s => {
    const input = document.getElementById(configInputId(s.key));
    if (!input) return;
    const evt = (input.tagName === 'SELECT' || input.type === 'checkbox') ? 'change' : 'input';
    input.addEventListener(evt, () => {
      const val = input.type === 'checkbox' ? String(input.checked) : String(input.value);
      if (val === String(s.value)) delete configDirty[s.key];
      else configDirty[s.key] = val;
      updateConfigDirtyState();
    });
  });
  updateConfigDirtyState();
}

function configFieldHtml(s) {
  const id = configInputId(s.key);
  const unit = s.unit ? `<span style="color:var(--text-muted); font-size:13px; margin-left:8px;">${cfgEsc(s.unit)}</span>` : '';
  let control = '';
  if (s.type === 'bool') {
    const checked = String(s.value) === 'true' ? 'checked' : '';
    control = `<label style="display:inline-flex; align-items:center; gap:8px; cursor:pointer;">
        <input type="checkbox" id="${id}" ${checked} style="width:16px; height:16px;">
        <span style="font-size:13px; color:var(--text-muted);">Enabled when checked</span>
      </label>`;
  } else if (s.type === 'select') {
    const opts = (s.options || []).map(o => `<option value="${cfgEsc(o.value)}" ${String(o.value) === String(s.value) ? 'selected' : ''}>${cfgEsc(o.label)}</option>`).join('');
    control = `<select id="${id}" class="form-select" style="max-width:280px;">${opts}</select>${unit}`;
  } else if (s.type === 'int' || s.type === 'float') {
    const min = (s.min !== null && s.min !== undefined) ? `min="${s.min}"` : '';
    const max = (s.max !== null && s.max !== undefined) ? `max="${s.max}"` : '';
    // float settings (tolerances, rupee thresholds) accept decimals; int stays whole-number.
    const step = s.type === 'float' ? 'step="any"' : 'step="1"';
    control = `<input type="number" id="${id}" class="form-input" value="${cfgEsc(s.value)}" ${min} ${max} ${step} style="max-width:200px; display:inline-block;">${unit}`;
  } else {
    control = `<input type="text" id="${id}" class="form-input" value="${cfgEsc(s.value)}" style="max-width:360px; display:inline-block;">${unit}`;
  }
  return `
    <div class="form-group" style="margin-bottom:22px; max-width:640px;">
      <label class="form-label" for="${id}" style="font-weight:600;">${cfgEsc(s.label)}</label>
      ${s.description ? `<p class="page-subtitle" style="margin:2px 0 8px; font-size:12.5px;">${cfgEsc(s.description)}</p>` : ''}
      <div>${control}</div>
    </div>
  `;
}

function updateConfigDirtyState() {
  const btn = document.getElementById('config-save-btn');
  const note = document.getElementById('config-dirty-note');
  const n = Object.keys(configDirty).length;
  if (btn) btn.disabled = n === 0;
  if (note) note.textContent = n === 0 ? 'No unsaved changes.' : `${n} unsaved change${n === 1 ? '' : 's'}.`;
}

async function saveConfiguration() {
  if (Object.keys(configDirty).length === 0) return;
  const btn = document.getElementById('config-save-btn');
  if (btn) btn.disabled = true;
  const res = await apiFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(configDirty) });
  if (!res) { if (btn) btn.disabled = false; return; }
  if (!res.ok) { await showApiError(res, 'Failed to save configuration.'); if (btn) btn.disabled = false; return; }
  showToast('Configuration saved.', { variant: 'success' });
  // Re-render fresh so persisted values show and dirty tracking resets.
  setActiveMenu('menu-configuration');
  renderView('configuration');
}

// System Status dashboard (Stage 26.1.2, PDF "SLO/status-page dashboard").
// Pure frontend: wires the existing Stage 25.8 deployment-status/
// backup-status endpoints (which already compute the DR-0213/DR-0214
// overdue warnings off the Stage 17.10 error catalog) into one HR/Admin
// screen. No new backend route or table.
async function renderSystemStatusView(container) {
  const [deployRes, backupRes] = await Promise.all([
    apiFetch('/api/v1/ops/deployment-status'),
    apiFetch('/api/v1/ops/backup-status')
  ]);

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">System Status</h1>
      <p class="page-subtitle">Deployment health and backup/restore-drill cadence across every environment.</p>
    </div>
  `;
  container.appendChild(header);

  if (!deployRes || !backupRes) return;

  const deployFailed = !deployRes.ok;
  const backupFailed = !backupRes.ok;
  const deployData = deployFailed ? { latest_by_environment: {}, history: [] } : await deployRes.json();
  const backupData = backupFailed ? { warnings: [], history: [] } : await backupRes.json();

  if (deployFailed || backupFailed) {
    const err = document.createElement('p');
    err.style.cssText = 'color:var(--danger-color); font-size:13px; margin-bottom:16px;';
    err.textContent = deployFailed && backupFailed
      ? 'Failed to load deployment and backup status.'
      : deployFailed ? 'Failed to load deployment status.' : 'Failed to load backup status.';
    container.appendChild(err);
  }

  const warnings = backupData.warnings || [];
  if (warnings.length > 0) {
    const banner = document.createElement('div');
    banner.style.cssText = 'display:flex; flex-direction:column; gap:8px; margin-bottom:20px;';
    banner.innerHTML = warnings.map(w => `
      <div class="badge ${w.code === 'DR-0214' ? 'badge-danger' : 'badge-warning'}" style="display:flex; padding:10px 14px; font-size:13px; font-weight:500; white-space:normal;">
        <span style="font-weight:700; margin-right:8px;">${w.code}</span> ${w.message}
      </div>
    `).join('');
    container.appendChild(banner);
  }

  const envCount = Object.keys(deployData.latest_by_environment || {}).length;
  const backupOverdue = warnings.some(w => w.code === 'DR-0214');
  const drillOverdue = warnings.some(w => w.code === 'DR-0213');
  const statsRow = document.createElement('div');
  statsRow.className = 'dashboard-stats-row';
  statsRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Environments Tracked</span>
      <span class="stat-val">${envCount}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Last Backup</span>
      <div style="margin-top:4px;"><span class="badge ${backupOverdue ? 'badge-danger' : 'badge-success'}">${backupData.last_backup_at || 'Never'}</span></div>
    </div>
    <div class="stat-card">
      <span class="stat-label">Last Restore Drill</span>
      <div style="margin-top:4px;"><span class="badge ${drillOverdue ? 'badge-warning' : 'badge-success'}">${backupData.last_restore_drill_at || 'Never'}</span></div>
    </div>
  `;
  container.appendChild(statsRow);

  const envRows = Object.values(deployData.latest_by_environment || {});
  const envPanel = document.createElement('div');
  envPanel.className = 'table-panel';
  envPanel.style.marginTop = '20px';
  envPanel.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding:16px 16px 0;">Latest Deployment by Environment</h3>
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Environment</th><th>Build Status</th><th>Git Commit</th><th>App Version</th><th>Promoted By</th><th>Promoted At</th></tr></thead>
        <tbody>
          ${envRows.length === 0 ? '<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No deployments recorded yet. Rows appear here after a promote or deploy run records its result.</td></tr>' : envRows.map(d => `
            <tr>
              <td style="font-weight:600; text-transform:capitalize;">${d.environment}</td>
              <td>
                <span class="badge ${d.build_status === 'passed' ? 'badge-success' : d.build_status === 'failed' ? 'badge-danger' : 'badge-secondary'}">${d.build_status}</span>
                ${d.code ? `<div style="font-size:11px; color:var(--danger-strong); margin-top:2px;">${d.code}: ${d.message}</div>` : ''}
              </td>
              <td style="font-family:Consolas,Monaco,monospace; font-size:12px;">${(d.git_commit || '').slice(0, 10)}</td>
              <td>${d.app_version || ''}</td>
              <td>${d.promoted_by || ''}</td>
              <td style="font-size:11px; white-space:nowrap;">${d.promoted_at || ''}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(envPanel);

  const historyPanel = document.createElement('div');
  historyPanel.className = 'table-panel';
  historyPanel.style.marginTop = '20px';
  historyPanel.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding:16px 16px 0;">Deployment History</h3>
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Environment</th><th>Build Status</th><th>Git Commit</th><th>Promoted By</th><th>Promoted At</th><th>Notes</th></tr></thead>
        <tbody>
          ${(deployData.history || []).length === 0 ? '<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No deployment history yet. Rows appear here after a promote or deploy run records its result.</td></tr>' : deployData.history.map(d => `
            <tr>
              <td style="text-transform:capitalize;">${d.environment}</td>
              <td><span class="badge ${d.build_status === 'passed' ? 'badge-success' : d.build_status === 'failed' ? 'badge-danger' : 'badge-secondary'}">${d.build_status}</span></td>
              <td style="font-family:Consolas,Monaco,monospace; font-size:12px;">${(d.git_commit || '').slice(0, 10)}</td>
              <td>${d.promoted_by || ''}</td>
              <td style="font-size:11px; white-space:nowrap;">${d.promoted_at || ''}</td>
              <td style="font-size:12px; color:var(--text-muted);">${d.notes || ''}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(historyPanel);

  const backupPanel = document.createElement('div');
  backupPanel.className = 'table-panel';
  backupPanel.style.marginTop = '20px';
  backupPanel.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding:16px 16px 0;">Backup &amp; Restore Drill History</h3>
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Type</th><th>Environment</th><th>Status</th><th>Detail</th><th>Started</th><th>Finished</th></tr></thead>
        <tbody>
          ${(backupData.history || []).length === 0 ? '<tr><td colspan="6" style="text-align:center; color:var(--text-muted);">No backup/restore runs recorded yet. Backups are driven by the deployment scripts &mdash; see the Admin Guide.</td></tr>' : backupData.history.map(o => `
            <tr>
              <td style="text-transform:capitalize;">${(o.run_type || '').replace('_', ' ')}</td>
              <td style="text-transform:capitalize;">${o.environment}</td>
              <td>
                <span class="badge ${o.status === 'success' ? 'badge-success' : o.status === 'failed' ? 'badge-danger' : 'badge-secondary'}">${o.status}</span>
                ${o.code ? `<div style="font-size:11px; color:var(--danger-strong); margin-top:2px;">${o.code}: ${o.message}</div>` : ''}
              </td>
              <td style="font-size:12px; color:var(--text-muted);">${o.detail || ''}</td>
              <td style="font-size:11px; white-space:nowrap;">${o.started_at || ''}</td>
              <td style="font-size:11px; white-space:nowrap;">${o.finished_at || ''}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(backupPanel);
}

// Tenant Entitlements admin screen (Stage 26.1.4). HR/Admin-only. Lets an
// admin pick a tenant, apply a whole product plan in one action (reuses
// Stage 27's engines.ProductPackages/ApplyPackageSelection via the new
// GET /api/v1/admin/packages + POST /api/v1/admin/tenant/package endpoints),
// or fine-tune individual module toggles (the pre-existing Stage 14
// GET/POST .../tenant/module-entitlement(s) endpoints) - no new engine
// mechanism, just a screen over what already existed server-side.
async function renderTenantEntitlementsView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Tenant Entitlements</h1>
      <p class="page-subtitle">Set which plan/modules each tenant has access to.</p>
    </div>
  `;
  container.appendChild(header);

  const [tenantsRes, packagesRes] = await Promise.all([
    apiFetch('/api/v1/admin/tenants'),
    apiFetch('/api/v1/admin/packages')
  ]);
  if (!tenantsRes || !packagesRes) return;
  if (!tenantsRes.ok || !packagesRes.ok) {
    renderErrorPanel(container, 'Failed to load tenants/plans.', () => renderView('tenant-entitlements'));
    return;
  }
  const tenants = await tenantsRes.json();
  const packages = await packagesRes.json();

  // 27.8 fast-follow: a picker over engines.ProductPackages for
  // handleProvisionTenant's `packages` field - the endpoint itself
  // (POST /api/v1/admin/tenant/provision) has existed since Stage 27 but
  // had no browser UI at all, only ever reachable via curl/scripts.
  // Collapsed by default since provisioning a brand-new tenant is a rare
  // action compared to adjusting an existing one below.
  const provisionPanel = document.createElement('div');
  provisionPanel.className = 'table-panel';
  provisionPanel.style.padding = '16px';
  provisionPanel.style.marginBottom = '20px';
  provisionPanel.innerHTML = `
    <div id="provision-tenant-toggle" style="display:flex; justify-content:space-between; align-items:center; cursor:pointer;">
      <h3 style="font-size:16px; font-weight:600; margin:0;">+ Provision New Tenant</h3>
      <span id="provision-tenant-chevron" style="color:var(--text-muted);">&#9656;</span>
    </div>
    <div id="provision-tenant-form" class="hidden" style="margin-top:16px; display:flex; flex-direction:column; gap:12px;">
      <div style="display:flex; gap:12px; flex-wrap:wrap;">
        <div class="form-group" style="margin-bottom:0;">
          <label class="form-label" for="provision-tenant-id">Tenant ID</label>
          <input type="text" id="provision-tenant-id" class="form-input" placeholder="e.g. acme_co" style="width:220px;">
        </div>
        <div class="form-group" style="margin-bottom:0;">
          <label class="form-label" for="provision-schema-name">Schema Name</label>
          <input type="text" id="provision-schema-name" class="form-input" placeholder="e.g. tenant_acme" style="width:220px;">
        </div>
      </div>
      <div>
        <label class="stat-label" style="display:block; margin-bottom:6px;">Packages (leave all unchecked to provision the full suite)</label>
        <div style="display:flex; flex-wrap:wrap; gap:12px;">
          ${packages.filter(p => p.package_key !== 'erp_full').map(p => `
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; font-weight:400;">
              <input type="checkbox" class="provision-package-checkbox" value="${p.package_key}"> ${p.display_name}
            </label>
          `).join('')}
        </div>
      </div>
      <div id="provision-tenant-error" class="login-error hidden" style="margin-bottom:0;"></div>
      <div>
        <button class="btn btn-primary" id="provision-tenant-btn">Provision Tenant</button>
      </div>
    </div>
  `;
  container.appendChild(provisionPanel);

  document.getElementById('provision-tenant-toggle').addEventListener('click', () => {
    document.getElementById('provision-tenant-form').classList.toggle('hidden');
    const chevron = document.getElementById('provision-tenant-chevron');
    chevron.innerHTML = document.getElementById('provision-tenant-form').classList.contains('hidden') ? '&#9656;' : '&#9662;';
  });

  document.getElementById('provision-tenant-btn').addEventListener('click', async () => {
    const errorEl = document.getElementById('provision-tenant-error');
    errorEl.classList.add('hidden');
    const tenantId = document.getElementById('provision-tenant-id').value.trim();
    const schemaName = document.getElementById('provision-schema-name').value.trim();
    if (!tenantId || !schemaName) {
      errorEl.textContent = 'Tenant ID and schema name are both required.';
      errorEl.classList.remove('hidden');
      return;
    }
    const selectedPackages = Array.from(document.querySelectorAll('.provision-package-checkbox:checked')).map(cb => cb.value);
    const res = await apiFetch('/api/v1/admin/tenant/provision', {
      method: 'POST',
      body: JSON.stringify({ tenant_id: tenantId, schema_name: schemaName, packages: selectedPackages })
    });
    if (!res) return;
    if (!res.ok) {
      errorEl.textContent = await getErrorMessage(res, 'Failed to provision tenant.');
      errorEl.classList.remove('hidden');
      return;
    }
    const data = await res.json();
    await showOneTimeSecretDialog(
      'Tenant Provisioned',
      `Tenant "${data.tenant_id}" is ready. Admin login is username "${data.admin_username}", password shown once below - store it now, it cannot be retrieved again:`,
      data.admin_password
    );
    renderView('tenant-entitlements');
  });

  const pickerPanel = document.createElement('div');
  pickerPanel.className = 'table-panel';
  pickerPanel.style.padding = '16px';
  pickerPanel.innerHTML = `
    <label class="stat-label" for="tenant-entitlements-select" style="display:block; margin-bottom:6px;">Tenant</label>
    <select id="tenant-entitlements-select" class="form-select" style="width:320px; max-width:100%;">
      <option value="">Select a tenant...</option>
      ${tenants.map(t => `<option value="${t.tenant_id}">${t.name} (${t.tenant_id})</option>`).join('')}
    </select>
  `;
  container.appendChild(pickerPanel);

  const bodyContainer = document.createElement('div');
  bodyContainer.id = 'tenant-entitlements-body';
  container.appendChild(bodyContainer);

  function renderBody(tenantId, modules) {
    bodyContainer.innerHTML = `
      <div class="table-panel" style="margin-top:20px;">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:4px; padding:16px 16px 0;">Apply a Plan</h3>
        <p style="font-size:12px; color:var(--text-muted); margin:0; padding: 0 16px 12px;">Enables that plan's modules and disables every other optional module for this tenant.</p>
        <div style="padding:0 16px 16px; display:flex; flex-wrap:wrap; gap:8px;">
          ${packages.map(p => `<button class="btn btn-outline btn-sm" title="${(p.modules || []).join(', ')}" onclick="applyTenantPackage('${tenantId}','${p.package_key}')">${p.display_name}</button>`).join('')}
        </div>
      </div>
      <div class="table-panel" style="margin-top:20px;">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding:16px 16px 0;">Module Entitlements</h3>
        <div class="table-wrapper">
          <table>
            <thead><tr><th>Module</th><th style="width:100px;">Status</th></tr></thead>
            <tbody>
              ${modules.length === 0 ? '<tr><td colspan="2" style="text-align:center; color:var(--text-muted);">No modules registered. Use <b>Provision Tenant</b> above to grant this tenant its product modules.</td></tr>' : modules.map(m => `
                <tr>
                  <td>${m.display_name}${m.is_core ? '<span class="badge badge-secondary" style="margin-left:8px;">Always On</span>' : ''}</td>
                  <td>
                    ${m.is_core
                      ? `<span class="badge badge-success">Enabled</span>`
                      : `<label class="switch"><input type="checkbox" ${m.enabled ? 'checked' : ''} onchange="toggleTenantModule('${tenantId}','${m.module_key}', this.checked)"><span class="slider"></span></label>`
                    }
                  </td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  async function loadTenant(tenantId) {
    bodyContainer.innerHTML = '';
    if (!tenantId) return;
    const res = await apiFetch(`/api/v1/admin/tenant/module-entitlements?tenant_id=${encodeURIComponent(tenantId)}`);
    if (!res) return;
    if (!res.ok) {
      renderErrorPanel(bodyContainer, 'Failed to load this tenant\'s module entitlements.', () => loadTenant(tenantId));
      return;
    }
    renderBody(tenantId, await res.json());
  }

  document.getElementById('tenant-entitlements-select').addEventListener('change', (e) => loadTenant(e.target.value));

  window.applyTenantPackage = async function(tenantId, packageKey) {
    const pkg = packages.find(p => p.package_key === packageKey);
    if (!await showCustomConfirm(`Apply the "${pkg ? pkg.display_name : packageKey}" plan to this tenant? Every other optional module will be disabled to match.`)) return;
    const res = await apiFetch('/api/v1/admin/tenant/package', {
      method: 'POST',
      body: JSON.stringify({ tenant_id: tenantId, packages: [packageKey] })
    });
    if (!res) return;
    if (res.ok) {
      const data = await res.json();
      renderBody(tenantId, data.modules);
    } else {
      await showApiError(res, 'Failed to apply plan.');
    }
  };

  window.toggleTenantModule = async function(tenantId, moduleKey, enabled) {
    const res = await apiFetch('/api/v1/admin/tenant/module-entitlement', {
      method: 'POST',
      body: JSON.stringify({ tenant_id: tenantId, module_key: moduleKey, enabled })
    });
    if (!res) return;
    if (!res.ok) {
      await showApiError(res, 'Failed to update module entitlement.');
      await loadTenant(tenantId); // revert the checkbox to actual server state
    }
  };
}

// Tenant Usage/health dashboard (Stage 26.1.5). HR/Admin-only. Reads the
// single GET /api/v1/admin/tenant-usage endpoint, which reuses Stage 24.30's
// live per-tenant concurrency limiter and Stage 25.8's tenant_limits table -
// no new metering mechanism, just a screen over what already existed.
async function renderTenantUsageView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Tenant Usage</h1>
      <p class="page-subtitle">Live request concurrency and configured usage limits, per tenant.</p>
    </div>
  `;
  container.appendChild(header);

  const res = await apiFetch('/api/v1/admin/tenant-usage');
  if (!res) return;
  if (!res.ok) {
    renderErrorPanel(container, 'Failed to load tenant usage.', () => renderView('tenant-usage'));
    return;
  }
  const rows = await res.json();

  const totalInFlight = rows.reduce((sum, t) => sum + t.in_flight_requests, 0);
  const atCapCount = rows.filter(t => t.concurrency_cap > 0 && t.in_flight_requests >= t.concurrency_cap).length;
  const statsRow = document.createElement('div');
  statsRow.className = 'dashboard-stats-row';
  statsRow.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Tenants</span>
      <span class="stat-val">${rows.length}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">In-Flight Requests (all tenants)</span>
      <span class="stat-val">${totalInFlight}</span>
    </div>
    <div class="stat-card">
      <span class="stat-label">Tenants At Concurrency Cap</span>
      <div style="margin-top:4px;"><span class="badge ${atCapCount > 0 ? 'badge-danger' : 'badge-success'}">${atCapCount}</span></div>
    </div>
  `;
  container.appendChild(statsRow);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.marginTop = '20px';
  panel.innerHTML = `
    <div class="table-wrapper">
      <table>
        <thead><tr><th>Tenant</th><th>Active Users</th><th>In-Flight Requests</th><th>Configured Limits</th></tr></thead>
        <tbody>
          ${rows.length === 0 ? '<tr><td colspan="4" style="text-align:center; color:var(--text-muted);">No tenants found. Tenants are created by the control-plane provisioning flow &mdash; see the Admin Guide.</td></tr>' : rows.map(t => {
            const pctOfCap = t.concurrency_cap > 0 ? t.in_flight_requests / t.concurrency_cap : 0;
            const concurrencyBadge = pctOfCap >= 1 ? 'badge-danger' : pctOfCap >= 0.5 ? 'badge-warning' : 'badge-success';
            const maxUsers = t.configured_limits && t.configured_limits.max_users;
            const usersBadge = maxUsers != null && t.active_users >= maxUsers ? 'badge-danger' : 'badge-secondary';
            const otherLimits = Object.entries(t.configured_limits || {}).filter(([k]) => k !== 'max_users');
            return `
              <tr>
                <td style="font-weight:600;">${t.name} <span style="color:var(--text-muted); font-weight:400;">(${t.tenant_id})</span></td>
                <td><span class="badge ${usersBadge}">${t.active_users}${maxUsers != null ? ' / ' + maxUsers : ''}</span></td>
                <td><span class="badge ${concurrencyBadge}">${t.in_flight_requests} / ${t.concurrency_cap}</span></td>
                <td>${otherLimits.length === 0 ? '<span style="color:var(--text-muted); font-size:12px;">-</span>' : otherLimits.map(([k, v]) => `<span class="badge badge-secondary" style="margin-right:4px;">${k}: ${v}</span>`).join('')}</td>
              </tr>
            `;
          }).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(panel);
}

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
      <button class="btn btn-secondary" onclick="setActiveMenu(STATIC_VIEW_MENU_IDS[DEFAULT_VIEW]); renderView(DEFAULT_VIEW);">Back to Reports</button>
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

// ---------------------------------------------------------------------------
// Knowledge Center (Stage 39.3-39.5)
//
// Articles are rendered to HTML at build time by cmd/genkb and embedded in the
// server binary; this file only fetches, lists and displays them. Nothing here
// parses Markdown, and nothing here needs to: the browser receives inert,
// already-escaped HTML, which is why article text can never execute.
//
// Search is a lookup against a prebuilt inverted index (index fetched once,
// cached for the session) rather than a scan over article bodies or a search
// service. For a corpus of this size that is a few lines of set intersection.
// ---------------------------------------------------------------------------

let helpIndexCache = null;
let helpSearchCache = null;
let currentHelpSlug = '';

async function loadHelpIndex() {
  if (helpIndexCache) return helpIndexCache;
  const res = await apiFetch('/api/v1/help/index');
  if (!res || !res.ok) return null;
  helpIndexCache = await res.json();
  return helpIndexCache;
}

async function loadHelpSearchIndex() {
  if (helpSearchCache) return helpSearchCache;
  const res = await apiFetch('/api/v1/help/search-index');
  if (!res || !res.ok) return null;
  helpSearchCache = await res.json();
  return helpSearchCache;
}

// searchHelp scores a document by how many of the query's terms it contains,
// which is the whole ranking model. A term is matched as a prefix so typing
// "reserv" finds "reservation" - the alternative, exact terms only, makes a
// search box feel broken while you are still typing.
function searchHelp(index, query) {
  const terms = (query || '').toLowerCase().match(/[a-z0-9][a-z0-9_-]*/g) || [];
  if (terms.length === 0) return [];
  const scores = new Map();
  terms.forEach(term => {
    const matchedDocs = new Set();
    Object.keys(index.terms).forEach(indexed => {
      if (indexed.startsWith(term)) index.terms[indexed].forEach(doc => matchedDocs.add(doc));
    });
    matchedDocs.forEach(doc => scores.set(doc, (scores.get(doc) || 0) + 1));
  });
  return [...scores.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)
    .map(([doc, score]) => ({ ...index.docs[doc], score }));
}

// flattenHelpArticles gives the reading order the prev/next links follow - the
// sidebar's own order, so "next" always means the next thing in the sidebar.
function flattenHelpArticles(index) {
  const flat = [];
  (index.sections || []).forEach(section => (section.articles || []).forEach(article => flat.push(article)));
  return flat;
}

async function renderHelpView(container) {
  const index = await loadHelpIndex();
  if (!index) {
    container.innerHTML = `<div class="table-panel" style="padding:24px;"><p>The Knowledge Center could not be loaded.</p></div>`;
    return;
  }
  const flat = flattenHelpArticles(index);
  if (!currentHelpSlug && flat.length > 0) currentHelpSlug = flat[0].slug;

  const layout = document.createElement('div');
  layout.className = 'kb-layout';
  layout.innerHTML = `
    <aside class="kb-sidebar">
      <div class="kb-search">
        <input type="search" id="kb-search-input" class="form-input" placeholder="Search help..." aria-label="Search help">
        <div id="kb-search-results" class="kb-search-results hidden"></div>
      </div>
      <nav id="kb-nav" aria-label="Knowledge Center sections"></nav>
    </aside>
    <article class="kb-article table-panel" id="kb-article" style="padding:24px;"></article>`;
  container.appendChild(layout);

  const nav = layout.querySelector('#kb-nav');
  nav.innerHTML = (index.sections || []).map(section => `
    <div class="kb-nav-section">
      <h4>${escapeHTMLText(section.name)}</h4>
      <ul>${(section.articles || []).map(article =>
        `<li><a href="/help/${encodeURIComponent(article.slug)}" data-slug="${escapeHTMLText(article.slug)}" class="kb-nav-link${article.slug === currentHelpSlug ? ' active' : ''}">${escapeHTMLText(article.title)}</a></li>`).join('')}
      </ul>
    </div>`).join('');
  nav.addEventListener('click', event => {
    const link = event.target.closest('.kb-nav-link');
    if (!link) return;
    event.preventDefault();
    openHelpArticle(link.getAttribute('data-slug'));
  });

  const searchInput = layout.querySelector('#kb-search-input');
  const searchResults = layout.querySelector('#kb-search-results');
  searchInput.addEventListener('input', async () => {
    const query = searchInput.value.trim();
    if (query.length < 2) { searchResults.classList.add('hidden'); return; }
    const searchIndex = await loadHelpSearchIndex();
    if (!searchIndex) return;
    const hits = searchHelp(searchIndex, query);
    searchResults.innerHTML = hits.length === 0
      ? `<div class="kb-search-empty">Nothing matched &ldquo;${escapeHTMLText(query)}&rdquo;.</div>`
      : hits.map(hit => `<a href="#" data-slug="${escapeHTMLText(hit.slug)}"><strong>${escapeHTMLText(hit.title)}</strong><span>${escapeHTMLText(hit.section)}</span></a>`).join('');
    searchResults.classList.remove('hidden');
  });
  searchResults.addEventListener('click', event => {
    const link = event.target.closest('a[data-slug]');
    if (!link) return;
    event.preventDefault();
    searchResults.classList.add('hidden');
    searchInput.value = '';
    openHelpArticle(link.getAttribute('data-slug'));
  });

  await renderHelpArticle(currentHelpSlug);
}

async function openHelpArticle(slug) {
  currentHelpSlug = slug;
  history.pushState({}, '', '/help/' + encodeURIComponent(slug));
  document.querySelectorAll('.kb-nav-link').forEach(link => {
    link.classList.toggle('active', link.getAttribute('data-slug') === slug);
  });
  await renderHelpArticle(slug);
}
window.openHelpArticle = openHelpArticle;

async function renderHelpArticle(slug) {
  const holder = document.getElementById('kb-article');
  if (!holder) return;
  if (!slug) { holder.innerHTML = '<p class="text-muted">Select an article from the list.</p>'; return; }
  const res = await apiFetch(`/api/v1/help/article/${encodeURIComponent(slug)}`);
  if (!res || !res.ok) {
    holder.innerHTML = `<p class="text-muted">That article could not be loaded.</p>`;
    return;
  }
  const article = await res.json();
  const index = await loadHelpIndex();
  const flat = flattenHelpArticles(index || { sections: [] });
  const position = flat.findIndex(entry => entry.slug === slug);
  const previous = position > 0 ? flat[position - 1] : null;
  const next = position >= 0 && position < flat.length - 1 ? flat[position + 1] : null;

  const toc = (article.headings || []).filter(heading => heading.level === 2);
  holder.innerHTML = `
    <nav class="kb-breadcrumb" aria-label="Breadcrumb">
      <a href="/help" onclick="event.preventDefault(); openHelpArticle('${escapeHTMLText(flat[0] ? flat[0].slug : '')}')">Help</a>
      <span>/</span><span>${escapeHTMLText(article.section || '')}</span>
      <span>/</span><span>${escapeHTMLText(article.title || '')}</span>
    </nav>
    ${toc.length > 1 ? `<details class="kb-toc"><summary>On this page</summary><ul>${toc.map(h => `<li><a href="#${escapeHTMLText(h.slug)}">${escapeHTMLText(h.text)}</a></li>`).join('')}</ul></details>` : ''}
    <div class="kb-body">${article.html}</div>
    <footer class="kb-footer">
      ${article.last_verified ? `<span class="text-muted">Last verified ${escapeHTMLText(article.last_verified)}.</span>` : ''}
      <div class="kb-pager">
        ${previous ? `<a href="#" data-slug="${escapeHTMLText(previous.slug)}">&larr; ${escapeHTMLText(previous.title)}</a>` : '<span></span>'}
        ${next ? `<a href="#" data-slug="${escapeHTMLText(next.slug)}">${escapeHTMLText(next.title)} &rarr;</a>` : '<span></span>'}
      </div>
    </footer>`;
  holder.querySelectorAll('.kb-pager a[data-slug]').forEach(link => {
    link.addEventListener('click', event => { event.preventDefault(); openHelpArticle(link.getAttribute('data-slug')); });
  });
  holder.scrollTop = 0;
}

// openHelpDrawer (Stage 39.5) shows the article(s) mapped to a screen, over the
// screen itself. The mapping comes from each article's own `screens:`
// frontmatter via the index's screen_map, so there is no second mapping file to
// keep in sync with the articles.
async function openHelpDrawer(screenID) {
  const index = await loadHelpIndex();
  if (!index) {
    showCustomAlert('The Knowledge Center could not be loaded.', 'Help');
    return;
  }
  const slugs = (index.screen_map || {})[String(screenID || '').toLowerCase()] || [];

  document.getElementById('kb-drawer')?.remove();
  const drawer = document.createElement('div');
  drawer.id = 'kb-drawer';
  drawer.className = 'modal-overlay open';
  drawer.innerHTML = `
    <div class="modal-container kb-drawer-container">
      <div class="modal-header">
        <h3 class="modal-title">Help</h3>
        <button type="button" class="modal-close" aria-label="Close">&times;</button>
      </div>
      <div class="modal-body" id="kb-drawer-body"><p class="text-muted">Loading&hellip;</p></div>
      <div class="modal-footer">
        <a class="btn btn-outline" href="/help">Open the full Knowledge Center</a>
        <button type="button" class="btn btn-secondary">Close</button>
      </div>
    </div>`;
  document.body.appendChild(drawer);
  const close = () => drawer.remove();
  drawer.querySelector('.modal-close').addEventListener('click', close);
  drawer.querySelector('.btn-secondary').addEventListener('click', close);

  const body = drawer.querySelector('#kb-drawer-body');
  if (slugs.length === 0) {
    // Say plainly that this screen has no article yet rather than showing an
    // arbitrary one - a wrong article is worse than an honest gap.
    body.innerHTML = `<p>No help article is mapped to this screen yet.</p>
      <p class="text-muted">Open the full Knowledge Center to search everything, or ask an administrator to have this screen documented.</p>`;
    return;
  }
  const res = await apiFetch(`/api/v1/help/article/${encodeURIComponent(slugs[0])}`);
  if (!res || !res.ok) { body.innerHTML = '<p class="text-muted">That article could not be loaded.</p>'; return; }
  const article = await res.json();
  const others = slugs.slice(1);
  body.innerHTML = `
    <h4 style="margin:0 0 12px;">${escapeHTMLText(article.title)}</h4>
    <div class="kb-body">${article.html}</div>
    ${others.length > 0 ? `<p class="text-muted" style="margin-top:16px;">Also for this screen: ${others.map(slug => `<a href="/help/${encodeURIComponent(slug)}">${escapeHTMLText(slug)}</a>`).join(', ')}</p>` : ''}`;
}
window.openHelpDrawer = openHelpDrawer;

// Window load init
window.addEventListener('DOMContentLoaded', bootstrap);
