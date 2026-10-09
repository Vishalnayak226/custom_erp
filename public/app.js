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
    extraEl.innerHTML = `<input type="${escapeHTMLText(inputType)}" id="custom-dialog-prompt-input" class="form-input" style="width: 100%; margin-top: 12px;" value="${escapeHTMLText(defaultValue)}">`;
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
        // BLD-037: apierror.go's own comment already promises this is "the
        // correlation_id shown to the user" (server-side logs are keyed by
        // it too), but nothing here ever read it off the envelope - so no
        // error surface could ever actually show it. Optional: most
        // responses carry one, a pre-envelope call site never will.
        correlationId: data.correlation_id || '',
      };
    }
  } catch (e) {
    // Body wasn't JSON (a call site not yet migrated to the standardized
    // envelope) - fall through to the fallback message.
  }
  return { message: fallback, detail: '', userAction: '', code: '', displayStyle: '', correlationId: '' };
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
  const { message, detail, userAction, code, displayStyle, correlationId } = details;
  if (code) console.debug(`[API error] ${code}`);
  if (displayStyle === 'Toast') {
    // Toast is transient (a few seconds) - too short-lived for a reference
    // worth writing down, unlike the two persistent surfaces below.
    showToast(composeErrorText(details), { variant: 'warning' });
    return;
  }
  if (displayStyle === 'Page banner') {
    const container = document.getElementById('view-root');
    if (container) {
      renderPageBanner(container, composeErrorText(details), { correlationId });
      return;
    }
  }
  // Stage 30.2.4: the modal shows all three parts as their own lines - the
  // catalog headline, then the specific reason, then what to do about it.
  // Before this, a missing HSN code read only "Tax configuration is missing
  // for this transaction. Please contact administrator." with no indication
  // of which item or which field, to an administrator.
  await showCustomAlert(composeErrorLines(message, detail, userAction, correlationId), title);
}

// Builds the modal body for showApiError. Returns a DOM node when there is
// more than the headline to show, and a plain string otherwise, so the
// single-line case renders byte-for-byte as it always has.
function composeErrorLines(message, detail, userAction, correlationId) {
  if (!detail && !userAction && !correlationId) return message;
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
  if (correlationId) {
    // BLD-037: the one thing worth quoting to support on an error a user
    // can't self-resolve - server logs and the audit trail are both keyed
    // by this same id (apierror.go).
    const ref = document.createElement('div');
    ref.textContent = `Reference: ${correlationId}`;
    ref.style.fontSize = '11px';
    ref.style.color = 'var(--text-muted)';
    wrap.appendChild(ref);
  }
  return wrap;
}

// Inline centered retry panel for full-page load failures, so a failed GET
// doesn't just leave the user staring at a blank view after they dismiss a
// dialog. Mirrors the centered-card layout already used by renderMockModuleView.
function renderErrorPanel(container, message, retryFn) {
  container.innerHTML = '';
  const errPanel = document.createElement('div');
  errPanel.className = 'table-panel';
  errPanel.style.padding = '48px';
  errPanel.style.textAlign = 'center';
  errPanel.innerHTML = `
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
  container.appendChild(errPanel);
  const btn = errPanel.querySelector('#error-panel-retry-btn');
  if (btn && retryFn) btn.addEventListener('click', () => {
    // Views are first rendered into an off-screen scratch node, then their
    // children are moved into #view-root. Resolve the panel's parent now,
    // not from the original container captured before that move.
    const liveParent = errPanel.parentElement;
    if (!liveParent?.isConnected) return;
    retryFn(liveParent);
  });
}

// BLD-041: screen code is native ES modules fetched only when a permitted
// route is opened. Keep the shell classic so legacy screens and extensions
// retain their existing global bindings; each module explicitly exports its
// old top-level functions, which are published here after a successful load.
// Cache the in-flight Promise as well as the module so rapid/double navigation
// cannot issue duplicate requests. A rejected import is evicted and gets a
// unique retry URL on the next attempt, preserving the existing retry panel.
const loadedViewModules = new Map();
const viewModuleLoadAttempts = new Map();
// Stage 53.17. Two additions to the URL, both for the same reason: a view
// module was the one piece of this app's code with NO cache story at all.
//
// VIEW_MODULE_VERSION - `import('view-pos.js')` is an ordinary HTTP GET, so a
// browser holding yesterday's copy keeps running yesterday's screen after a
// deploy. index.html busts app.js with ?v=N; the lazy modules had no such
// thing, which meant the shell could update while the screens did not. Keep
// this in step with index.html's app.js?v= on every release.
//
// viewModuleCacheBust - set by Reset, so "clear everything and reload" also
// means the screen code itself. Without it Reset cleared every app-level cache
// and then re-imported the same stale module from the HTTP cache, which is
// precisely the "refresh keeps the cache" complaint this work exists to fix.
const VIEW_MODULE_VERSION = '35';
let viewModuleCacheBust = '';
function loadViewModule(src) {
  if (loadedViewModules.has(src)) return loadedViewModules.get(src);
  const attempt = (viewModuleLoadAttempts.get(src) || 0) + 1;
  viewModuleLoadAttempts.set(src, attempt);
  const params = [`v=${VIEW_MODULE_VERSION}`];
  if (viewModuleCacheBust) params.push(`reset=${viewModuleCacheBust}`);
  if (attempt > 1) params.push(`retry=${attempt}`);
  const requestURL = `${src}${src.includes('?') ? '&' : '?'}${params.join('&')}`;
  const pending = import(requestURL).then(module => {
    Object.assign(window, module);
    return module;
  }).catch(error => {
    loadedViewModules.delete(src);
    throw error;
  });
  loadedViewModules.set(src, pending);
  return pending;
}

const LAZY_VIEW_MODULES = {
  pos: ['view-pos.js', 'renderPOSView', 'POS'],
  'rf-traceability': ['view-rf-traceability.js', 'renderRFTraceabilityView', 'RF Traceability'],
  returns: ['view-returns.js', 'renderReturnsView', 'Returns'],
  finance: ['view-finance.js', 'renderFinanceView', 'Finance'],
  'vendor-invoices': ['view-finance.js', 'renderVendorInvoicesView', 'Vendor Invoices'],
  'payment-proposals': ['view-finance.js', 'renderPaymentProposalsView', 'Payment Proposals'],
  'bank-reconciliation': ['view-finance.js', 'renderBankReconciliationView', 'Bank Reconciliation'],
  'finance-notes': ['view-finance.js', 'renderFinanceNotesView', 'Finance Notes'],
  'sales-invoices': ['view-finance.js', 'renderSalesInvoicesView', 'Sales Invoices'],
  'journal-vouchers': ['view-journal.js', 'renderJournalVouchersView', 'Journal Vouchers'],
  fulfillment: ['view-warehouse.js', 'renderFulfillmentView', 'Fulfillment'],
  putaway: ['view-warehouse.js', 'renderPutawayView', 'Putaway'],
  'warehouse-cockpit': ['view-warehouse.js', 'renderWarehouseCockpitView', 'Warehouse Cockpit'],
  'appointment-calendar': ['view-warehouse.js', 'renderAppointmentCalendarView', 'Appointment Calendar'],
  'yard-board': ['view-warehouse.js', 'renderYardBoardView', 'Yard Board'],
  'place-hold': ['view-warehouse.js', 'renderPlaceHoldView', 'Place Hold'],
  'rf-receiving': ['view-warehouse.js', 'renderRFReceivingView', 'RF Receiving'],
  sortation: ['view-warehouse.js', 'renderSortationView', 'Sortation'],
  'loading-dock': ['view-warehouse.js', 'renderLoadingDockView', 'Loading Dock'],
  'bin-conditions': ['view-warehouse.js', 'renderBinConditionsView', 'Bin Conditions'],
  'cycle-count': ['view-warehouse.js', 'renderCycleCountView', 'Cycle Count'],
  asn: ['view-procurement.js', 'renderASNView', 'Advance Shipment Notices'],
  lpn: ['view-warehouse.js', 'renderLPNView', 'License Plates'],
  'bin-replenishment': ['view-warehouse.js', 'renderBinReplenishmentView', 'Bin Replenishment'],
  'location-movement': ['view-warehouse.js', 'renderLocationMovementView', 'Location Movement'],
  'wave-picking': ['view-warehouse.js', 'renderWavePickingView', 'Wave Picking'],
  'mobile-picking': ['view-warehouse.js', 'renderMobilePickingView', 'Mobile Picking'],
  marketplace: ['view-oms.js', 'renderMarketplaceView', 'Marketplace'],
  oms: ['view-oms.js', 'renderOMSWorkbenchView', 'OMS'],
  approvals: ['view-procurement.js', 'renderApprovalsView', 'Approvals'],
  'purchase-orders': ['view-procurement.js', 'renderPurchaseOrdersView', 'Purchase Orders'],
  grn: ['view-procurement.js', 'renderGRNWorkbenchView', 'Goods Receipt'],
  'purchase-returns': ['view-procurement.js', 'renderPurchaseReturnsView', 'Purchase Return'],
  reports: ['view-reports.js', 'renderReportsView', 'Reports'],
  inventory: ['view-reports.js', 'renderInventoryView', 'Inventory'],
  rfq: ['view-reports.js', 'renderRFQView', 'Request for Quotation'],
  stickers: ['view-printing.js', 'renderStickersView', 'Sticker Printing'],
  hr: ['view-hr.js', 'renderHRView', 'HR'],
  assets: ['view-assets.js', 'renderAssetsView', 'Fixed Assets'],
  transfers: ['view-transfers.js', 'renderTransfersView', 'Stock Transfer'],
  expenses: ['view-expenses.js', 'renderExpensesView', 'Expenses'],
  manufacturing: ['view-manufacturing.js', 'renderManufacturingView', 'Manufacturing'],
  pim: ['view-pim.js', 'renderPIMView', 'PIM'],
  'doctype-table': ['view-documents.js', 'renderDocTableView', 'Records'],
  'doctype-builder': ['view-documents.js', 'renderDocTypeBuilderView', 'Doctype Builder'],
  'prefix-configs': ['view-admin.js', 'renderPrefixConfigsView', 'Prefix Configurations'],
  'approval-rules': ['view-admin.js', 'renderApprovalRulesView', 'Approval Rules'],
  'dynamic-labels': ['view-admin.js', 'renderDynamicLabelsView', 'Dynamic Labels'],
  'extension-hooks': ['view-admin.js', 'renderExtensionHooksView', 'Extension Hooks'],
  'extension-hook-log': ['view-admin.js', 'renderExtensionHookLogView', 'Extension Hook Log'],
  'audit-logs': ['view-admin.js', 'renderLogHubView', 'Integration Logs'],
  configuration: ['view-admin.js', 'renderConfigurationView', 'Configuration'],
  'system-status': ['view-admin.js', 'renderSystemStatusView', 'System Status'],
  'tenant-entitlements': ['view-admin.js', 'renderTenantEntitlementsView', 'Tenant Entitlements'],
  'tenant-usage': ['view-admin.js', 'renderTenantUsageView', 'Tenant Usage'],
  help: ['view-help.js', 'renderHelpView', 'Help'],
};

async function renderLazyView(view, root, [moduleFile, entrypoint, label]) {
  let module;
  try {
    module = await loadViewModule(`/${moduleFile}?v=1`);
  } catch (error) {
    console.error(`[BLD-041] ${label} module failed to load`, error);
    renderErrorPanel(root, `Failed to load the ${label} screen. Check your connection and try again.`, () => renderView(view));
    return;
  }
  const renderer = module[entrypoint];
  if (typeof renderer !== 'function') {
    console.error(`[BLD-041] ${moduleFile} does not export ${entrypoint}`);
    renderErrorPanel(root, `The ${label} screen could not be started. Please try again.`, () => renderView(view));
    return;
  }
  await renderer(root);
}

// OMS dashboard tiles drill into the report catalog. Keep that cross-domain
// path lazy too: load the report domain only after a user opens a tile.
async function execDashboardOpenReport(reportId) {
  let reports;
  try {
    reports = await loadViewModule('/view-reports.js?v=1');
  } catch (error) {
    console.error('[BLD-041] Reports module failed to load from an OMS tile', error);
    await showCustomAlert('That report could not be opened. Check your connection and try again.', 'Reports');
    return;
  }
  return reports.execDashboardOpenReport(reportId);
}

async function openPIMAssignTaskModalLazy(...args) {
  try {
    await loadViewModule('/view-pim.js?v=1');
  } catch (error) {
    console.error('[BLD-041] PIM module failed to load from a report action', error);
    await showCustomAlert('The Product task action could not be opened. Check your connection and try again.', 'PIM');
    return;
  }
  const action = window.openPIMAssignTaskModal;
  if (typeof action === 'function' && action !== openPIMAssignTaskModalLazy) return action(...args);
}
window.openPIMAssignTaskModal = openPIMAssignTaskModalLazy;

// BLD-036: disables `button` and swaps its label for the duration of `fn`,
// restoring both afterward regardless of success/failure. A slow save (or an
// impatient repeat click/Enter before the first response returns) used to be
// able to send the same create/save request twice - e.g. the generic record
// form's create path, which has no server-side dedupe for two different
// documents created a fraction of a second apart. A second call arriving
// while `button` is still disabled is dropped rather than queued - the
// original call already covers the intent.
async function guardAgainstDoubleSubmit(button, busyLabel, fn) {
  if (!button) return fn();
  if (button.disabled) return;
  const originalLabel = button.textContent;
  button.disabled = true;
  button.textContent = busyLabel;
  try {
    return await fn();
  } finally {
    button.disabled = false;
    button.textContent = originalLabel;
  }
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
  // BLD-038: this is the one place every toast in the app renders through, so
  // an assistive-tech announcement belongs here rather than at any call site.
  // "assertive" only for danger/warning - an "info"/"success" toast is not
  // worth interrupting whatever the user is doing to announce.
  toast.setAttribute('role', variant === 'danger' || variant === 'warning' ? 'alert' : 'status');
  toast.setAttribute('aria-live', variant === 'danger' || variant === 'warning' ? 'assertive' : 'polite');
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
  // Stage 50/AUD-01: this is the one place every record-list cell in the app
  // renders through (roughly 30 call sites), so it is also the one place a
  // stored value can turn into executable markup if it is trusted instead of
  // escaped - a Vendor-only clerk's saved name reached a Super Admin's
  // browser this way. displayValue is always plain cell text here, never
  // caller-built HTML (checked every call site before making this the
  // choke point), so escaping it unconditionally is safe. Callers that used
  // to pre-escape their own displayValue before calling this now pass the
  // raw value instead - escaping twice would double-encode "&" into
  // "&amp;amp;" and show up wrong on screen.
  if (raw === '') return displayValue === undefined || displayValue === null ? '' : escapeHTMLText(displayValue);
  // 30.5.8: this one helper renders every copy chip in the app - roughly 30
  // per record list - so the accessible name is attached here rather than at
  // any call site. It names the value, not the action: a screen reader
  // announcing "Copy, button" thirty times down a column says nothing about
  // which row it is on. `title` alone is only a last-resort accessible name
  // and several screen readers skip it, so aria-label is what makes these
  // reachable rather than merely hoverable.
  return `<span class="copyable-cell"><span>${escapeHTMLText(displayValue)}</span><button type="button" class="copy-chip" title="Copy" aria-label="Copy ${escapeHTMLText(raw)}" data-copy-value="${encodeURIComponent(raw)}" ${actionAttrs('copyValueToClipboard', [ACTION_ARG_ELEMENT], { stop: true })}>${COPY_ICON_SVG}</button></span>`;
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
  // BLD-038: same reasoning as showToast's role/aria-live above - the one
  // choke point every page-banner-styled API error renders through.
  banner.setAttribute('role', 'alert');
  banner.setAttribute('aria-live', 'assertive');

  const body = document.createElement('span');
  body.className = 'page-banner-body';
  const msgEl = document.createElement('span');
  msgEl.textContent = message;
  body.appendChild(msgEl);
  if (opts.correlationId) {
    // BLD-037: Page banner is this app's single most common error surface
    // (152 of 302 catalog rows) and, unlike a toast, stays on screen until
    // dismissed - the right place for the same support-reference line the
    // modal path (composeErrorLines) shows.
    const ref = document.createElement('span');
    ref.className = 'page-banner-ref';
    ref.textContent = `Ref: ${opts.correlationId}`;
    body.appendChild(ref);
  }
  banner.appendChild(body);

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
  // BLD-034: the generic doctype-table's real total row count for the
  // current doctype/search (from the list endpoint's X-Total-Count header),
  // separate from docData.length now that docData only ever holds the one
  // page of rows actually fetched. Bespoke workbenches that also stash their
  // own list into docData (GRN, Transfer Order) never set this - harmless,
  // since only renderDocTable's own pagination footer reads it.
  docTotal: 0,
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
  permissions: { isAdmin: true, doctypes: new Set(), create: new Set(), update: new Set(), delete: new Set(), capabilities: new Set(), loaded: false },
  // Unknown entitlements hide module navigation until the server confirms
  // them. Direct views and the server's moduleGate also fail closed.
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
// it showed was derived counts and shortcut tiles into Settings screens -
// then Reports (same reasoning: reachable by every role/tenant, no
// MENU_MODULE_MAP gate) until BLD-033 (2026-09-24). Home is now the
// landing view: like Reports it is reachable by every role and tenant
// (MENU_PERMISSION_MAP marks it `open`, MENU_MODULE_MAP gates it on the
// always-on 'core' module), but unlike the retired Dashboard it links only
// to actual task screens this session can reach, never a Settings/admin
// screen - see renderHomeView's own comment for the full reasoning.
const DEFAULT_VIEW = 'home';

let currentView = DEFAULT_VIEW;
let currentDoctype = '';
// quickCreateReturn (2026-09-22 fix): { forDoctype, view, label } set by
// openSetupDoctype() whenever a "create the missing master" shortcut is
// launched from inside another screen (a typeahead's empty-state "create
// one" link, a setup hint) - renderView never pushes browser history (see
// saveNavState's own comment on that decision), so without this the
// shortcut was a one-way trip: e.g. GRN's Item picker -> create Item -> no
// way back to the GRN workbench. Consumed once, by renderDocTableView's
// "Back to X" button or by a successful save in handleDynamicFormSubmit -
// and invalidated by renderViewContent the moment navigation strays
// anywhere else, so a later, unrelated save can never see a stale target.
let quickCreateReturn = null;
let posCart = []; // { sku, available, qty, salePrice, referencePrice, priceSource, overrideId, unpriced } - Stage 47.2: prices are the server's, not the till's
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
// BLD-034: raised from 10 now that each page is its own server round-trip
// (previously this just sliced an already-fully-fetched <=500-row array in
// the browser, so a small page size cost nothing extra). 50 keeps a dense
// table snappy to render while cutting round-trips ~5x over a 100k+ row
// doctype versus keeping the old page size.
const itemsPerPage = 50;
let bulkSelectedDocIDs = new Set();
// BLD-034: bumped on every doctype-table page/search change and captured by
// each in-flight fetch at request time; a response is only applied if it's
// still the most recent request when it resolves. Guards against a slower
// earlier page-click or keystroke's response landing after a faster later
// one and clobbering it with stale rows - fast Tab+Enter paging or fast
// typing in the search box can otherwise resolve out of order.
let docTableRequestSeq = 0;
let docTableSearchDebounce = null;
let docTableLoading = false;
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
    // BLD-034: carries page/search along on a doctype-table screen, so a
    // refresh restores where the user actually was, not just which doctype -
    // found live while verifying BLD-034's "persistent context" against a
    // 250k-row seeded table (a same-tab F5 always looked exactly like a
    // fresh deep link before this, since deepLinkForDoctype's own hint/
    // "open in new tab" callers never pass page/search and so are
    // unaffected - see deepLinkForDoctype's own comment).
    const target = currentView === 'doctype-table' && currentDoctype
      ? deepLinkForDoctype(currentDoctype, { page: currentTablePage, search: currentSearchQuery })
      : deepLinkForView(currentView);
    if (window.location.hash !== target) history.replaceState(history.state, '', target); // keeps a setup detour's marker (Stage 57.2)
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
// BLD-036: apiFetch had no timeout at all - a hung connection (dead proxy,
// stalled DB, a Wi-Fi drop mid-request) waited forever with no
// distinguishable state from a normal slow response, and once
// guardAgainstDoubleSubmit (below) starts disabling the trigger button for
// the duration of a call, an unbounded wait would leave that button disabled
// forever with no way out. Overridable per call via options.timeoutMs (0
// disables it) for the rare caller that legitimately expects longer.
const DEFAULT_API_TIMEOUT_MS = 30000;

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

  const timeoutMs = options.timeoutMs === 0 ? 0 : (options.timeoutMs || DEFAULT_API_TIMEOUT_MS);
  const controller = (timeoutMs > 0 && !options.signal) ? new AbortController() : null;
  const timeoutId = controller ? setTimeout(() => controller.abort(), timeoutMs) : null;

  let response;
  try {
    response = await fetch(url, {
      ...options,
      headers,
      signal: options.signal || (controller ? controller.signal : undefined)
    });
  } catch (err) {
    if (controller && err.name === 'AbortError') {
      // A mutating call's server-side effect is genuinely unknown once the
      // client gives up waiting - never claim "nothing was changed" here.
      const isMutating = !!options.method && options.method !== 'GET';
      await showCustomAlert(isMutating
        ? 'The request timed out waiting for a response. If this was a save, refresh and check whether it went through before trying again.'
        : 'The request timed out. Please check your connection and try again.', 'Request Timed Out');
    } else {
      await showCustomAlert('Unable to reach the server. Please check your connection and try again.', 'Connection Error');
    }
    return null;
  } finally {
    if (timeoutId) clearTimeout(timeoutId);
  }

  if (response.status === 401) {
    logout(await getErrorMessage(response, 'Session expired. Please log in again.'));
    return null;
  }
  // Stage 57.17: the server refuses everything but setting a new password
  // while the account is on one an administrator issued.
  if (response.status === 403 && response.headers.get('X-Password-Change-Required') === '1') {
    showPasswordChangeScreen();
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
// BLD-036: same timeout reasoning as apiFetch above, but a longer default -
// this carries file/CSV uploads, which legitimately take longer than a plain
// JSON call.
const DEFAULT_UPLOAD_TIMEOUT_MS = 90000;

async function apiUpload(url, formData, timeoutMs = DEFAULT_UPLOAD_TIMEOUT_MS) {
  const token = localStorage.getItem('erp_token');
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const headers = { 'X-Tenant-ID': tenantID };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const controller = timeoutMs > 0 ? new AbortController() : null;
  const timeoutId = controller ? setTimeout(() => controller.abort(), timeoutMs) : null;

  let response;
  try {
    response = await fetch(url, { method: 'POST', headers, body: formData, signal: controller ? controller.signal : undefined });
  } catch (err) {
    if (controller && err.name === 'AbortError') {
      await showCustomAlert('The upload timed out waiting for a response. Check whether the import/upload went through before trying again.', 'Request Timed Out');
    } else {
      await showCustomAlert('Unable to reach the server. Please check your connection and try again.', 'Connection Error');
    }
    return null;
  } finally {
    if (timeoutId) clearTimeout(timeoutId);
  }
  if (response.status === 401) {
    logout(await getErrorMessage(response, 'Session expired. Please log in again.'));
    return null;
  }
  // Stage 57.17: the server refuses everything but setting a new password
  // while the account is on one an administrator issued.
  if (response.status === 403 && response.headers.get('X-Password-Change-Required') === '1') {
    showPasswordChangeScreen();
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

// employeePickerField / attachEmployeePicker live here, beside
// autoNumberField and attachLinkTypeahead, because two lazy view modules use
// them: view-hr.js (attendance, leave, payroll) and view-expenses.js (claim
// entry).
//
// They used to be declared inside view-hr.js. As ES modules the view chunks
// have their own top-level scope, so Expenses threw
// `ReferenceError: employeePickerField is not defined` on render unless HR
// happened to have been opened first - the same cross-module breakage the
// BLD-041 split caused for Finance/Fulfillment/OMS/Reports, found on a fifth
// screen by the 47.8.2 browser verification sweep. Promoted to the shared
// core rather than duplicated or imported across chunks, which is the rule
// the other four fixes followed.
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
    window.removeEventListener('scroll', placeMenu, true);
    window.removeEventListener('resize', placeMenu);
    cancelAnimationFrame(trackFrame);
    trackFrame = 0;
  }

  // While a list is open, follow its box through layout shifts too (a banner
  // or preview above it changing height), not only scroll/resize - checked
  // once per frame, and only while a list is open.
  let trackFrame = 0;
  let trackedAt = '';
  function trackBox() {
    if (!menu) { trackFrame = 0; return; }
    const r = inputEl.getBoundingClientRect();
    const at = `${r.top},${r.left}`;
    if (at !== trackedAt) { trackedAt = at; placeMenu(); }
    trackFrame = requestAnimationFrame(trackBox);
  }

  // placeMenu (Stage 57): opens upward when there is no room below, keeps clear
  // of the fixed environment banner, follows its box on scroll and closes when
  // the box leaves the screen (a stale fixed position put rows under the banner).
  function placeMenu(e) {
    if (!menu) return;
    if (e && e.type === 'scroll' && e.target && menu.contains(e.target)) return;
    const rect = inputEl.getBoundingClientRect();
    const banner = document.getElementById('environment-banner');
    const topLimit = (banner ? banner.getBoundingClientRect().bottom : 0) + 4;
    if (rect.bottom < topLimit || rect.top > window.innerHeight) { closeMenu(); return; }
    menu.style.left = `${Math.max(4, Math.min(rect.left, window.innerWidth - menu.offsetWidth - 4))}px`;
    const below = window.innerHeight - rect.bottom - 8;
    const above = rect.top - topLimit - 4;
    const natural = Math.min(menu.scrollHeight, 220);
    const openUp = below < natural && above > below;
    const room = Math.max(80, openUp ? above : below);
    menu.style.maxHeight = `${Math.min(220, room)}px`;
    const h = Math.min(natural, room);
    menu.style.top = `${openUp ? Math.max(topLimit, rect.top - h - 4) : rect.bottom + 4}px`;
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
    // Stage 57.1: a name-display field shows the record's name and keeps the
    // code as its value - see installNameDisplay.
    if (inputEl._nameDisplay) inputEl._nameDisplay.commit(doc, val);
    else inputEl.value = val;
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
      // Stage 57.2: create it here, seeded with the typed text (fallback: full form).
      const typed = typedTextOf(inputEl).trim();
      const label = getDoctypeLabel(doctype);
      if (opts.noQuickCreate) {
        row.innerHTML = `No matching ${label}`;
      } else if (!canCreateDoctype(doctype)) {
        row.innerHTML = `No matching ${label}. ${SETUP_MSG.missingNoAccess(label).replace(/^No [^.]*\. /, '')}`;
      } else {
        row.innerHTML = `No matching ${label} &mdash; <a href="#" class="empty-state-link"></a>`;
        row.querySelector('a').textContent = typed ? `Create “${typed}”` : `Create a new ${label}`;
        // The menu is a child of <body>, not of the input's container, so it
        // has to be torn down explicitly before the dialog opens or it is left
        // floating over it.
        row.querySelector('a').addEventListener('mousedown', (e) => {
          e.preventDefault();
          closeMenu();
          // Committed through pick(), so a field storing something other
          // than the code (valueFields - the HSN number) gets the right value.
          openQuickCreate(doctype, typed).then(doc => { if (doc && inputEl.isConnected) pick(doc); });
        });
      }
      menu.appendChild(row);
      document.body.appendChild(menu);
      document.addEventListener('mousedown', onDocMouseDown, true);
      placeMenu();
      window.addEventListener('scroll', placeMenu, true);
      window.addEventListener('resize', placeMenu); if (!trackFrame) { trackedAt = ''; trackFrame = requestAnimationFrame(trackBox); }
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
    placeMenu();
    window.addEventListener('scroll', placeMenu, true);
    window.addEventListener('resize', placeMenu); if (!trackFrame) { trackedAt = ''; trackFrame = requestAnimationFrame(trackBox); }
  }

  // Stage 53.3: opts.filters narrows the picker to a subset of the doctype,
  // as plain field=value pairs the generic doc endpoint already understands as
  // custom query filters (handlers_core_doc_engine.go skips q/limit/offset/
  // sort/count and treats the rest as data filters). Added here, once, rather
  // than as a POS-specific control: the POS screen needs "sellable locations
  // only", and the same need - "Active vendors only", "this family's items" -
  // is one line away for any screen that wants it next.
  //
  // Off by default, so all ~45 existing pickers send exactly the request they
  // sent before.
  const filterParams = Object.entries(opts.filters || {})
    .map(([k, v]) => `&${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('');

  async function search(q, { browse = false } = {}) {
    const seq = ++requestSeq;
    if (!q && !browse) { closeMenu(); return; }
    const pageSize = browse ? browseLimit : limit;
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(q)}&limit=${pageSize}${filterParams}`);
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
    // What the user typed - on a name-display field .value is the committed
    // code, which is not what they are searching for.
    const q = typedTextOf(inputEl).trim();
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
  // 2026-10-09: a Bin record has no code or name - its human identifier is
  // bin_code and its id is a generated UUID. With the defaults the picker
  // listed and stored that UUID, so Putaway, Bin Conditions and LPN all sent
  // a "bin" no engine could find ("bin ... not found"). bin_code is what the
  // engines look a bin up by.
  Bin: {
    valueFields: ['bin_code'],
    labelFn: doc => doc.bin_code || doc.id || '',
  },
};

function attachLinkTypeahead(inputEl, doctype, opts = {}) {
  if (!inputEl) return;
  inputEl.dataset.linkDoctype = doctype;
  // Stage 57.1: name shown, code stored. Installed before the typeahead's own
  // listeners; skipped where the caller owns the split (onPick) or the value
  // (valueFields).
  if (typeof opts.onPick !== 'function' && !opts.valueFields && !opts.keepCode && !(TYPEAHEAD_DOCTYPE_OPTS[doctype] || {}).valueFields) {
    installNameDisplay(inputEl, doctype);
  }
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
  // Stage 53.3: this control makes its OWN two lookups below (resolve-on-blur
  // and seed-the-name) besides the menu's. They have to carry opts.filters as
  // well, or a narrowed picker leaks the records it is meant to exclude - type
  // "HO" into a sellable-locations-only box, tab away, and the blur resolver
  // would happily commit the one location the filter exists to keep out.
  const filterQS = Object.entries(opts.filters || {})
    .map(([k, v]) => `&${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('');

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
    const res = await apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(text)}&limit=10${filterQS}`);
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
    apiFetch(`/api/v1/doc/${doctype}?q=${encodeURIComponent(seed)}&limit=10${filterQS}`).then(res => {
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
// Names, not codes (Stage 57.1): the box shows the name while `.value`, which
// every caller reads, still returns the code. Code on hover (title).
// ---------------------------------------------------------------------------
const NATIVE_INPUT_VALUE = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');

// linkDocCache memoises reference -> record lookups for the session, so
// showing names does not turn every re-render into another burst of
// requests against the per-session rate limit.
const linkDocCache = new Map();

function linkDisplayName(doc) {
  if (!doc) return '';
  return String(doc.name || doc.code || doc.id || '');
}

function linkCodeOf(doc) {
  if (!doc) return '';
  return String(doc.code || doc.name || doc.id || '');
}

function rememberLinkDoc(doctype, doc) {
  if (!doctype || !doc) return;
  const p = Promise.resolve(doc);
  [doc.id, doc.code].filter(Boolean).forEach(ref => linkDocCache.set(`${doctype}|${ref}`, p));
}

// lookupLinkDoc: by id, then exact code (pre-51.1 records).
function lookupLinkDoc(doctype, ref) {
  ref = String(ref == null ? '' : ref).trim();
  if (!doctype || !ref) return Promise.resolve(null);
  const key = `${doctype}|${ref}`;
  if (linkDocCache.has(key)) return linkDocCache.get(key);
  const pending = (async () => {
    const byId = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}/${encodeURIComponent(ref)}`);
    if (byId && byId.ok) return byId.json();
    const search = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}?q=${encodeURIComponent(ref)}&limit=10`);
    if (!search || !search.ok) return null;
    const lower = ref.toLowerCase();
    return ((await search.json()) || []).find(d =>
      String(d.code || '').toLowerCase() === lower || String(d.id || '').toLowerCase() === lower) || null;
  })().catch(() => null);
  linkDocCache.set(key, pending);
  return pending;
}

// typedTextOf is what is visibly in the box - what the user typed, or the
// name being shown - as opposed to .value, which on a name-display field is
// the committed code.
function typedTextOf(inputEl) {
  return inputEl && inputEl._nameDisplay ? inputEl._nameDisplay.typedText() : (inputEl ? inputEl.value : '');
}

function installNameDisplay(inputEl, doctype) {
  if (!inputEl || inputEl._nameDisplay || !(inputEl instanceof HTMLInputElement) || !NATIVE_INPUT_VALUE) return;
  const nativeGet = () => NATIVE_INPUT_VALUE.get.call(inputEl);
  const nativeSet = v => NATIVE_INPUT_VALUE.set.call(inputEl, v);
  let committed = null; // the code callers read; null = the box holds free text
  let seq = 0;          // invalidates a lookup the user has since typed over

  const show = (code, doc) => {
    committed = code;
    const name = linkDisplayName(doc);
    nativeSet(name || code);
    inputEl.title = name && name !== code ? code : '';
  };

  Object.defineProperty(inputEl, 'value', {
    configurable: true,
    get() {
      // An emptied box is empty whatever was committed before - covers
      // form.reset() and anything else that clears the field natively.
      if (nativeGet() === '') return '';
      return committed !== null ? committed : nativeGet();
    },
    set(v) {
      const code = v == null ? '' : String(v);
      const mySeq = ++seq;
      nativeSet(code);
      inputEl.title = '';
      committed = code === '' ? null : code;
      if (!code) return;
      // Programmatic fills show the code briefly, then the name.
      lookupLinkDoc(doctype, code).then(doc => {
        if (mySeq !== seq || !doc || nativeGet() !== code) return;
        show(code, doc);
      });
    }
  });

  // Typing starts a new free-text value; what was committed no longer is.
  inputEl.addEventListener('input', () => { seq++; committed = null; inputEl.title = ''; });

  // Exact name/code typed and tabbed away: resolve it (unambiguous matches only).
  inputEl.addEventListener('blur', () => setTimeout(async () => {
    if (committed !== null || !inputEl.isConnected) return;
    const text = nativeGet().trim();
    if (!text) return;
    const mySeq = ++seq;
    const res = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}?q=${encodeURIComponent(text)}&limit=10`);
    if (!res || !res.ok || mySeq !== seq) return;
    const lower = text.toLowerCase();
    const exact = ((await res.json()) || []).filter(d =>
      [d.code, d.id, d.name, d.short_code].some(v => String(v || '').toLowerCase() === lower));
    if (exact.length !== 1 || nativeGet().trim() !== text) return;
    const code = linkCodeOf(exact[0]);
    rememberLinkDoc(doctype, exact[0]);
    show(code, exact[0]);
    if (code !== text) inputEl.dispatchEvent(new Event('change', { bubbles: true }));
  }, 200));

  inputEl._nameDisplay = {
    commit(doc, code) { ++seq; rememberLinkDoc(doctype, doc); show(code || linkCodeOf(doc), doc); },
    typedText: nativeGet
  };
  // A field filled before this was attached (an edit form, a restored
  // screen) resolves its name too.
  const initial = nativeGet();
  if (initial) inputEl.value = initial;
}

// List half: names swapped in for data-link-ref cells and whole-cell master
// series codes "<RecordType>/..." (document numbers are not record types).
const linkNameMapCache = new Map();
function linkNameMap(doctype) {
  if (!linkNameMapCache.has(doctype)) {
    linkNameMapCache.set(doctype, (async () => {
      const res = await apiFetch(`/api/v1/doc/${encodeURIComponent(doctype)}?limit=1000`);
      const map = new Map();
      if (res && res.ok) {
        ((await res.json()) || []).forEach(d => {
          if (d.name) [d.id, d.code].filter(Boolean).forEach(k => map.set(String(k), String(d.name)));
        });
      }
      return map;
    })().catch(() => new Map()));
    setTimeout(() => linkNameMapCache.delete(doctype), 60000);
  }
  return linkNameMapCache.get(doctype);
}

const SERIES_CODE_RE = /^([A-Z][A-Za-z]+)\/[A-Za-z0-9-]+(?:\/[A-Za-z0-9-]+)*$/;
function seriesDoctypeOf(text) {
  const m = SERIES_CODE_RE.exec(text);
  return m && state.activeDoctypes.some(d => d.name === m[1]) ? m[1] : null;
}

async function sweepLinkNames(root) {
  if (!root) return;
  const targets = [];
  root.querySelectorAll('[data-link-ref]:not([data-link-swept])').forEach(el => {
    el.dataset.linkSwept = '1';
    const host = el.querySelector(':scope > .copyable-cell > span:first-child') || el;
    targets.push({ el: host, doctype: el.dataset.linkDoctype, ref: el.dataset.linkRef });
  });
  root.querySelectorAll('td:not([data-link-swept])').forEach(td => {
    td.dataset.linkSwept = '1';
    const host = td.children.length === 0 ? td : td.querySelector(':scope > .copyable-cell > span:first-child');
    if (!host || host.children.length) return;
    const text = host.textContent.trim();
    const doctype = text && seriesDoctypeOf(text);
    // A master's own list keeps its Code column: there the code is the
    // record's identity, shown beside its name, not a reference to decode.
    if (doctype && !(currentView === 'doctype-table' && currentDoctype === doctype)) targets.push({ el: host, doctype, ref: text });
  });
  if (!targets.length) return;
  const byDoctype = {};
  targets.forEach(t => { if (t.doctype && t.ref) (byDoctype[t.doctype] = byDoctype[t.doctype] || []).push(t); });
  for (const [doctype, list] of Object.entries(byDoctype)) {
    const map = await linkNameMap(doctype);
    for (const t of list) {
      let name = map.get(t.ref);
      // Past the first 1000 records of a type, fall back to a single lookup -
      // bounded by what is on screen, at most a page of rows.
      if (!name) name = linkDisplayName(await lookupLinkDoc(doctype, t.ref));
      if (name && name !== t.ref && t.el.isConnected) {
        t.el.textContent = name;
        t.el.title = t.ref;
      }
    }
  }
}

// Installed once; debounced so a table rendering row by row is swept once.
let linkNameObserver = null;
function startLinkNameSweep() {
  const root = document.getElementById('view-root');
  if (!root || linkNameObserver) return;
  let timer = null;
  linkNameObserver = new MutationObserver(() => {
    clearTimeout(timer);
    timer = setTimeout(() => sweepLinkNames(root), 120);
  });
  linkNameObserver.observe(root, { childList: true, subtree: true });
  sweepLinkNames(root);
}

// localISODate is a Date's LOCAL day as YYYY-MM-DD. toISOString() converts
// to UTC first, so in India local midnight became the previous day - the
// Appointment Calendar's next-day arrow did nothing (Stage 57).
function localISODate(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

// ---------------------------------------------------------------------------
// One date picker for every date field (Stage 57.6). The native input and its
// YYYY-MM-DD value stay; only the picker is replaced, by view-pickers.js,
// loaded on first open (NFR-COST-001). One observer marks every date input.
// ---------------------------------------------------------------------------
function loadPickers() {
  return loadViewModule('/view-pickers.js?v=1');
}

function enhanceDateInput(input) {
  if (!input || input.dataset.datePicker === '1' || input.readOnly || input.disabled) return;
  input.dataset.datePicker = '1';
  // A click opens it beside the box and leaves focus in the box, so typing a
  // date still works; Alt+Down or F4 opens it and moves focus into it.
  input.addEventListener('click', (e) => { e.preventDefault(); loadPickers().then(m => m.openDatePicker(input, false)); });
  input.addEventListener('keydown', (e) => {
    if ((e.altKey && e.key === 'ArrowDown') || e.key === 'F4') { e.preventDefault(); loadPickers().then(m => m.openDatePicker(input, true)); }
  });
}

let datePickerObserver = null;
function startDatePickerEnhancer() {
  if (datePickerObserver) return;
  const sweep = () => document.querySelectorAll('input[type="date"]:not([data-date-picker])').forEach(enhanceDateInput);
  let timer = null;
  datePickerObserver = new MutationObserver(() => { clearTimeout(timer); timer = setTimeout(sweep, 60); });
  datePickerObserver.observe(document.body, { childList: true, subtree: true });
  sweep();
}

// ---------------------------------------------------------------------------
// Inline quick-create (Stage 57.2): a missing master is created in a small
// dialog over the current form (required fields only, same /meta rules as the
// full form) and dropped into the field that needed it. Dialog: view-pickers.js.
async function openQuickCreate(doctype, seedText) {
  return (await loadPickers()).openQuickCreateDialog(doctype, seedText);
}

// quickCreateIntoInput runs a quick-create for one field and, on success,
// fills that field with the new record exactly as a pick would.
async function quickCreateIntoInput(inputEl, doctype, seedText) {
  const doc = await openQuickCreate(doctype, seedText);
  if (!doc || !inputEl || !inputEl.isConnected) return doc;
  const code = linkCodeOf(doc);
  if (inputEl._nameDisplay) inputEl._nameDisplay.commit(doc, code);
  else inputEl.value = code;
  inputEl.dispatchEvent(new Event('change', { bubbles: true }));
  inputEl.focus();
  return doc;
}

// defaultSelectValue (Stage 57.4): a new record's Status starts on Active.
function defaultSelectValue(field, options) {
  if (field && String(field.fieldname).toLowerCase() === 'status' && options.includes('Active')) return 'Active';
  return '';
}

// attachHSNPicker (Stage 57.3): HSN catalogue picker with inline create; fills
// an empty GST rate. The box keeps the plain number (engines/hsn_catalog.go).
function attachHSNPicker(hsnInput, gstInput) {
  if (!hsnInput || hsnInput.dataset.hsnPicker === '1') return;
  hsnInput.dataset.hsnPicker = '1';
  attachLinkTypeahead(hsnInput, 'HSNCode', {
    valueFields: ['hsn'],
    labelFn: doc => {
      const parts = [doc.hsn || doc.code];
      if (doc.description) parts.push(doc.description);
      if (doc.gst_rate !== undefined && doc.gst_rate !== null && doc.gst_rate !== '') parts.push(`GST ${doc.gst_rate}%`);
      return parts.join(' — ');
    }
  });
  hsnInput.addEventListener('change', async () => {
    if (!gstInput || gstInput.value !== '') return;
    const hsn = hsnInput.value.trim();
    if (!hsn) return;
    const doc = await lookupLinkDoc('HSNCode', `HSN-${hsn.replace(/[\s.]/g, '')}`);
    if (doc && doc.gst_rate !== undefined && doc.gst_rate !== null && doc.gst_rate !== '' && gstInput.value === '') {
      gstInput.value = doc.gst_rate;
      gstInput.dispatchEvent(new Event('change', { bubbles: true }));
    }
  });
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

// --- 47.8.2: delegated action dispatch ------------------------------------
//
// Inline `onclick="doThing('${row.id}')"` attributes are two problems in one
// string:
//
//  1. They require `script-src 'unsafe-inline'` in the CSP, which defeats the
//     main protection a CSP offers. The policy cannot be enforced while any
//     of them remain.
//  2. Every interpolated argument is unescaped JavaScript source inside an
//     HTML attribute. A record id, SKU or doctype containing a quote does not
//     merely break the handler - it closes the attribute and the string, and
//     whatever follows runs as script. That is a live injection sink wherever
//     the interpolated value came from imported data, a connector payload or
//     a report row.
//
// actionAttrs + this dispatcher close both at once, and close them at the one
// shared choke point rather than per call site. The function name and its
// arguments travel as data attributes (escaped, so a hostile value can only
// ever be *data*), and one delegated listener per event type looks the
// function up and calls it. No `eval`, no string-to-code path, no per-element
// listener to leak.
//
// Deliberately keeps the "functions live on window" convention the lazy view
// modules already rely on (loadViewModule publishes each module's exports),
// so a converted handler reaches exactly the same function the inline
// attribute reached - no import wiring, no behaviour change.

// ACTION_ARG_ELEMENT and friends are the sentinels that replace a handler's
// old `this`/`this.value`/`this.checked` references. JSON cannot carry a DOM
// node, so the dispatcher substitutes them at call time.
const ACTION_ARG_ELEMENT = '$el';
const ACTION_ARG_VALUE = '$value';
const ACTION_ARG_CHECKED = '$checked';
// $event is for the form handlers that took `event` so they could call
// preventDefault() themselves - index.html's modal submits do this.
const ACTION_ARG_EVENT = '$event';

// actionAttrs renders the data attributes for one delegated action.
//
//   actionAttrs('resetUserMFA', [u.id, u.username])
//     -> data-act="resetUserMFA" data-act-args="[&quot;U1&quot;,&quot;ann&quot;]"
//
// Options: {prevent: true} calls event.preventDefault(), {stop: true} calls
// event.stopPropagation() - the two things the inline handlers used to do
// inline before calling their function.
function actionAttrs(fn, args, opts) {
  const options = opts || {};
  let out = 'data-act="' + escapeHTMLText(fn) + '"';
  if (args && args.length) {
    // JSON.stringify first, then HTML-escape: the result is inert data in a
    // quoted attribute no matter what the arguments contain.
    out += ' data-act-args="' + escapeHTMLText(JSON.stringify(args)) + '"';
  }
  // The event is always written out explicitly rather than inferred from the
  // element's tag: a <button> inside a <label>, or a <select> someone clicks
  // rather than changes, both make tag-sniffing guess wrong, and a handler
  // that fires on the wrong event is worse than one that does not fire.
  if (options.on && options.on !== 'click') out += ' data-act-on="' + escapeHTMLText(options.on) + '"';
  if (options.prevent) out += ' data-act-prevent="1"';
  if (options.stop) out += ' data-act-stop="1"';
  return out;
}

// resolveActionArgs substitutes the element sentinels and returns the real
// argument list.
function resolveActionArgs(raw, el, event) {
  if (!raw) return [];
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    console.error('[action] unparseable data-act-args', raw, err);
    return null;
  }
  if (!Array.isArray(parsed)) {
    console.error('[action] data-act-args is not an array', raw);
    return null;
  }
  return parsed.map((a) => {
    if (a === ACTION_ARG_ELEMENT) return el;
    if (a === ACTION_ARG_VALUE) return el.value;
    if (a === ACTION_ARG_CHECKED) return el.checked;
    if (a === ACTION_ARG_EVENT) return event;
    return a;
  });
}

// resolveActionFunction looks up an action by name, and refuses to be a
// general-purpose call primitive while doing it.
//
// A plain `window[name]` lookup is not safe enough. `window['eval']` is a
// function, so `fn.apply(null, args)` with a string argument would execute
// that string - the dispatcher would hand any code path that could influence
// a data-act name full script execution. Today no renderer can: every name
// comes from a string literal in this repository's own source. The point is
// that it stays harmless if that ever stops being true, because one renderer
// passing a record field where it meant to pass a function name should be a
// broken button, not arbitrary code execution.
//
// Three checks, in order of what they rule out:
//
//  1. the name must be a plain identifier - no property paths, no indexing;
//  2. it must be an OWN property of window, which excludes everything
//     inherited from Object.prototype (`constructor`, `__proto__`,
//     `toString`, `valueOf`);
//  3. it must not be a native built-in. Every action in this app is a
//     function declared in app.js or published by a view module, so none of
//     them is native, while `eval`, `Function`, `alert`, `fetch`, `open` and
//     the rest of the platform all are. This is an allowlist by construction
//     rather than a denylist that has to keep up with the platform.
const ACTION_NAME_RE = /^[A-Za-z_$][\w$]*$/;

function resolveActionFunction(name) {
  if (!name || !ACTION_NAME_RE.test(name)) return null;
  if (!Object.prototype.hasOwnProperty.call(window, name)) return null;
  const fn = window[name];
  if (typeof fn !== 'function') return null;
  // Function.prototype.toString on a native function yields "[native code]".
  // Read through the prototype so a function that defines its own toString
  // cannot disguise itself.
  if (Function.prototype.toString.call(fn).includes('[native code]')) return null;
  return fn;
}

function handleDelegatedAction(event) {
  const el = event.target.closest('[data-act]');
  if (!el) return;
  // Only handle the event type this element was authored for, so a data-act
  // on a <select> does not also fire when someone clicks it.
  const wanted = el.getAttribute('data-act-on') || 'click';
  if (wanted !== event.type) return;
  if (el.hasAttribute('data-act-prevent')) event.preventDefault();
  if (el.hasAttribute('data-act-stop')) event.stopPropagation();
  if (el.disabled) return;

  const name = el.getAttribute('data-act');
  const fn = resolveActionFunction(name);
  if (!fn) {
    // A missing function is a programming error, not something to retry or
    // surface to the user as a failure of their action. Logged loudly so it
    // shows up in the browser-boundary smoke run rather than failing silently.
    console.error('[action] no such action function: ' + name);
    return;
  }
  const args = resolveActionArgs(el.getAttribute('data-act-args'), el, event);
  if (args === null) return;
  try {
    fn.apply(null, args);
  } catch (err) {
    console.error('[action] ' + name + ' threw', err);
  }
}

// goToDefaultView replaces an inline handler that chained two calls in one
// attribute (`setActiveMenu(...); renderView(...)`). The dispatcher calls one
// function per action deliberately - it has no statement sequencing and no
// eval - so a chained handler becomes a named function instead.
function goToDefaultView() {
  setActiveMenu(STATIC_VIEW_MENU_IDS[DEFAULT_VIEW]);
  renderView(DEFAULT_VIEW);
}
window.goToDefaultView = goToDefaultView;

// selectFieldText replaces an inline `this.select()` handler.
function selectFieldText(el) {
  if (el && typeof el.select === 'function') el.select();
}
window.selectFieldText = selectFieldText;

// Registered once, on document, in the bubble phase - so markup rendered at
// any time (including into a scratch buffer and swapped in) is covered with
// no per-render wiring. 'change' is listed separately because it does not
// bubble identically for every control type.
document.addEventListener('click', handleDelegatedAction);
document.addEventListener('change', handleDelegatedAction);
// 'submit' is listed because index.html's modal forms used onsubmit. The
// handlers call event.preventDefault() themselves, which is why they receive
// the event through the $event sentinel rather than relying on data-act-prevent.
document.addEventListener('submit', handleDelegatedAction);

// makeClickable turns a plain <div>-with-a-click-listener tile into something
// a keyboard/screen-reader user can actually reach and activate. Found during
// the BLD-038 accessibility sweep: several existing "clickable stat-card"
// tiles (exec dashboard, OMS tiles, Home's approvals count) only ever bound a
// 'click' listener to a bare <div>, so they had no tab stop and no way to
// activate them without a mouse. Applies role/tabindex/keydown once so future
// tiles of the same shape get it for free instead of repeating this by hand.
function makeClickable(el, handler) {
  el.setAttribute('role', 'button');
  el.setAttribute('tabindex', '0');
  el.addEventListener('click', handler);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handler(e);
    }
  });
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
// Stage 47.6.1: explicit semantic metadata, served by the same endpoint so the
// browser and the server cannot disagree about what an input holds. The case
// that forced it: mobile-pick-wave-id contains "mobile", so token inference
// gave the RF Wave ID box the phone keystroke filter and a wave id lost its
// letters as the operator typed - silently, on a device where nobody could see
// it happen.
let FIELD_SEMANTICS = {};
let FIELD_SCAN_SEMANTICS = [];

async function loadFieldFormats() {
  try {
    const res = await apiFetch('/api/v1/meta/field-formats');
    if (!res || !res.ok) return;
    const data = await res.json();
    FIELD_FORMATS = (data && data.formats) || [];
    FIELD_FORMAT_EXCLUDED_SUFFIXES = (data && data.excluded_suffixes) || [];
    FIELD_SEMANTICS = (data && data.semantics) || {};
    FIELD_SCAN_SEMANTICS = (data && data.scan) || [];
  } catch (e) {
    // A missing spec list degrades to "no hints, no filtering" - the server
    // still validates on save, so nothing becomes unsafe, just less helpful.
    console.debug('[field-formats] not available', e);
  }
}

// isDerivedCompanionField mirrors the server's function of the same name.
function isDerivedCompanionField(name) {
  const n = String(name || '').toLowerCase().trim();
  return FIELD_FORMAT_EXCLUDED_SUFFIXES.some(suf => {
    if (!n.endsWith(suf)) return false;
    // `_type` is a derived companion only when it still contains a format
    // token (e.g. phone_type). A domain attribute such as stone_type is a
    // real Item input and must remain editable.
    if (suf === '_type' && !FIELD_FORMATS.some(spec => (spec.tokens || []).some(tok => tok && n.includes(tok)))) return false;
    return true;
  });
}

// fieldSemantic mirrors the server's FieldSemantic: whole identifier first,
// then its "-"/"_" separated words.
function fieldSemantic(name) {
  const n = String(name || '').toLowerCase().trim();
  if (!n) return '';
  if (FIELD_SEMANTICS[n]) return FIELD_SEMANTICS[n];
  for (const w of n.split(/[-_.]/)) {
    if (FIELD_SEMANTICS[w]) return FIELD_SEMANTICS[w];
  }
  return '';
}

// isScanField reports whether an input holds a machine-read code - a barcode,
// wave, LPN, lot or serial. The RF shell uses it to keep scanner focus and to
// keep character filters away from codes that legitimately contain letters.
function isScanField(name) {
  return FIELD_SCAN_SEMANTICS.indexOf(fieldSemantic(name)) >= 0;
}

// detectFieldFormat mirrors the server's DetectFieldFormat: an explicit
// semantic wins, then first token match, from the server's own priority order.
function detectFieldFormat(name) {
  const n = String(name || '').toLowerCase().trim();
  if (!n || isDerivedCompanionField(n)) return null;
  const semantic = fieldSemantic(n);
  if (semantic && semantic !== 'phone') return null;
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
  // Every call site of this function is a "create the missing master"
  // shortcut surfaced from inside another screen - none of them are the
  // sidebar's own Setup-menu navigation (that has its own separate click
  // handler in renderSidebarSubmenu()). Capture where we're leaving from so
  // the list this opens can offer a way back - see quickCreateReturn.
  const originTitle = document.querySelector('.page-title')?.textContent || null;
  // Stage 57.2: close an open new-record form first (keeping its values) and
  // reopen it on return; one history entry so browser Back returns.
  const modal = document.getElementById('dynamic-modal');
  let reopenForm = null;
  if (modal && modal.classList.contains('open') && !editingDocID) {
    reopenForm = { doctype: currentDoctype, values: snapshotFormValues(document.getElementById('dynamic-modal-form')) };
    if (typeof window.closeDynamicModal === 'function') window.closeDynamicModal();
  }
  quickCreateReturn = {
    forDoctype: doctype, view: currentView, label: originTitle,
    originDoctype: currentView === 'doctype-table' ? currentDoctype : null,
    reopenForm,
    hash: window.location.hash,
    pushed: false
  };
  // One history entry for the detour, so the browser's Back button returns
  // to where the user came from. Normal navigation stays on replaceState
  // (saveNavState) - this is the one place a Back press has a clear meaning.
  try {
    history.pushState({ erpSetupDetour: true }, '', window.location.href);
    quickCreateReturn.pushed = true;
  } catch (e) { /* not addressable here - the Back to X link still works */ }
  currentDoctype = doctype;
  currentSearchQuery = '';
  currentTablePage = 1;
  renderView('doctype-table');
};

// snapshot/restoreFormValues: carry a half-filled form across a detour; a select
// whose options are not loaded yet keeps the value in data-pending-value.
function snapshotFormValues(form) {
  const values = {};
  if (!form) return values;
  form.querySelectorAll('[name]').forEach(el => {
    if (el.type === 'checkbox') values[el.name] = el.checked;
    else if (el.type !== 'file' && el.value !== '') values[el.name] = el.value;
  });
  return values;
}

function restoreFormValues(form, values) {
  if (!form || !values) return;
  Object.entries(values).forEach(([name, value]) => {
    const el = form.querySelector(`[name="${CSS.escape(name)}"]`);
    if (!el || el.readOnly) return;
    if (el.type === 'checkbox') { el.checked = !!value; return; }
    if (el.tagName === 'SELECT' && ![...el.options].some(o => o.value === value)) { el.dataset.pendingValue = value; return; }
    el.value = value;
  });
}

// performQuickCreateReturn: back to the origin view and list, reopening a
// new-record form with its values and the just-created record filled in.
async function performQuickCreateReturn(target, createdId) {
  if (target.originDoctype) {
    currentDoctype = target.originDoctype;
    currentSearchQuery = '';
    currentTablePage = 1;
  }
  await renderView(target.view);
  if (!target.reopenForm || typeof window.openDynamicModal !== 'function') return;
  currentDoctype = target.reopenForm.doctype;
  await window.openDynamicModal();
  const form = document.getElementById('dynamic-modal-form');
  restoreFormValues(form, target.reopenForm.values);
  if (createdId && form) {
    const waiting = [...form.querySelectorAll(`[data-link-doctype="${CSS.escape(target.forDoctype)}"]`)]
      .find(el => !el.value);
    if (waiting) {
      if (waiting.tagName === 'SELECT' && ![...waiting.options].some(o => o.value === createdId)) waiting.dataset.pendingValue = createdId;
      else waiting.value = createdId;
    }
  }
}

// returnFromQuickCreate: "Back to X" and post-save return, one-shot. A pushed
// detour entry is unwound with history.back(); hashchange finishes the return.
window.returnFromQuickCreate = function (createdId) {
  const target = quickCreateReturn;
  if (!target) return;
  if (createdId) target.createdId = createdId;
  if (target.pushed && history.state && history.state.erpSetupDetour) {
    history.back();
    return;
  }
  quickCreateReturn = null;
  performQuickCreateReturn(target, target.createdId);
};

// setupLink renders "Setup » Brand" as a real link into that list. Uses an
// inline onclick like the DocType Builder's own module flyout does, rather
// than introducing a second delegation scheme for one link.
function setupLink(doctype, label) {
  const text = label || `Setup &raquo; ${getDoctypeLabel(doctype)}`;
  return `<a href="#" class="empty-state-link" ${actionAttrs('openSetupDoctype', [doctype], { prevent: true })}>${text}</a>`;
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
  return `<div class="empty-state-hint">No ${getDoctypeLabel(label || doctype)} records exist yet &mdash; ${setupLink(doctype, 'create one first')}.</div>`;
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
// extra ({page, search}) is optional and only ever passed by saveNavState's
// own self-referential write (BLD-034) - every other caller (setupOpenLinks'
// hint/"open in new tab" affordances, "back to X" shortcuts) calls this with
// just a doctype and must keep landing clean on page 1/no search, which
// omitting the argument still does.
function deepLinkForDoctype(doctype, extra) {
  let link = `#/setup/${encodeURIComponent(doctype)}`;
  if (extra && (extra.page > 1 || extra.search)) {
    const qs = new URLSearchParams();
    if (extra.page > 1) qs.set('p', String(extra.page));
    if (extra.search) qs.set('q', extra.search);
    link += `?${qs.toString()}`;
  }
  return link;
}
function deepLinkForView(view) { return `#/view/${encodeURIComponent(view)}`; }

// parseDeepLink reads the current fragment, or returns null when there isn't
// one. Tolerant of a stale/hand-edited hash: an unrecognised shape is null,
// which falls through to the normal restore-last-view path. BLD-034: a
// 'setup' link's optional ?p=/q= (see deepLinkForDoctype) is parsed off
// before splitting the path on '/', so a page/search-carrying self-link
// doesn't corrupt the doctype segment.
function parseDeepLink() {
  const raw = (window.location.hash || '').replace(/^#/, '');
  if (!raw.startsWith('/')) return null;
  const [pathPart, queryPart] = raw.slice(1).split('?');
  const parts = pathPart.split('/');
  if (parts.length < 2 || !parts[1]) return null;
  const value = decodeURIComponent(parts[1]);
  if (parts[0] === 'setup') {
    const link = { kind: 'setup', doctype: value };
    if (queryPart) {
      const qs = new URLSearchParams(queryPart);
      const p = parseInt(qs.get('p'), 10);
      if (p > 1) link.page = p;
      if (qs.get('q')) link.search = qs.get('q');
    }
    return link;
  }
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
    // BLD-037: plain navigation, not openSetupDoctype() - that function is
    // the "create the missing master" shortcut, and unconditionally sets
    // quickCreateReturn for its own "Back to X" button. A deep link arriving
    // cold (a refresh, a bookmark, a shared/new-tab URL - restoreLastView's
    // own comment above) has no real prior screen to go back to in this tab;
    // routing it through openSetupDoctype anyway picked up currentView's
    // still-default 'home' as a bogus origin, showing a nonsensical,
    // untranslated "Back to home" link on every refreshed doctype-table
    // screen. restoreActiveMenuState is the same sidebar-highlight logic
    // openSetupDoctype's active-class toggling duplicated, already built for
    // exactly this "arrived cold" case.
    currentDoctype = link.doctype;
    // BLD-034: an actual same-tab refresh's self-written link (see
    // saveNavState) carries the page/search it left off at; a hint's or a
    // shared/bookmarked link never does (deepLinkForDoctype's other
    // callers don't pass them), so both still land clean on page 1/no
    // search exactly as before.
    currentSearchQuery = link.search || '';
    currentTablePage = link.page || 1;
    await renderView('doctype-table');
    restoreActiveMenuState('doctype-table', link.doctype);
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
  // Stage 57.2: Back from a setup detour lands on the entry it pushed from -
  // finish the return there (reopening the form the user was in) rather
  // than as a plain deep link, which would only show the list.
  if (quickCreateReturn && quickCreateReturn.pushed && window.location.hash === quickCreateReturn.hash) {
    const target = quickCreateReturn;
    quickCreateReturn = null;
    performQuickCreateReturn(target, target.createdId);
    return;
  }
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
  const label = getDoctypeLabel(doctype);
  return `<a href="${href}" class="empty-state-link" ${actionAttrs('openSetupDoctype', [doctype], { prevent: true })}>${inlineLabel}</a>` +
    `<a href="${href}" target="_blank" rel="noopener" class="setup-hint-newtab" title="Open ${label} setup in a new tab" aria-label="Open ${label} setup in a new tab">` +
    `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" aria-hidden="true">` +
    `<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line>` +
    `</svg></a>`;
}

// setupHintHTML is the standard hint, in whichever of the three forms applies.
// `mode` is 'missing' (nothing exists) or 'addMore' (records exist, this is
// the quiet nudge).
function setupHintHTML(doctype, mode) {
  const label = getDoctypeLabel(doctype);
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
    // Stage 57.2: a field hint creates the record inline and fills the field.
    const primary = hint.querySelector('a.empty-state-link');
    if (primary) {
      ['data-act', 'data-act-args', 'data-act-prevent'].forEach(a => primary.removeAttribute(a));
      primary.addEventListener('mousedown', (e) => e.preventDefault()); // keep focus, so blur does not repaint it away
      primary.addEventListener('click', (e) => {
        e.preventDefault();
        quickCreateIntoInput(inputEl, doctype, typedTextOf(inputEl).trim());
      });
    }
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
  'purchase-returns': ['Vendor'],
  'asn': ['Vendor', 'Location'],
  'rfq': ['Vendor', 'Item'],
  'fulfillment': ['Location'],
  'putaway': ['Location', 'Bin'],
  'bin-conditions': ['Bin'],
  'cycle-count': ['Location', 'Bin'],
  'lpn': ['Bin'],
  'bin-replenishment': ['Location', 'Bin'],
  'location-movement': ['Location', 'Bin'],
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

// Stage 45: no dismiss. Previously a click on the banner's x hid it for the
// rest of the browser session (sessionStorage) - the user asked for that to
// go away entirely: as long as a screen's prerequisites are genuinely unmet,
// the banner is not something the user should be able to make disappear
// while the underlying gap is still there, every visit.
//
// renderSetupBanner prepends the banner to a rendered view when that view's
// prerequisites are not met. Called from renderView, so no screen has to
// remember to do it.
function renderSetupBanner(view) {
  const root = document.getElementById('view-root');
  if (!root || !state.setupStatus.loaded) return;
  const existing = document.getElementById('setup-banner');
  if (existing) existing.remove();

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
  // The country-specific decorator must honor the same semantics as the
  // format matcher; "mobile-pick-wave-id" is a wave, not a phone number.
  const semantic = fieldSemantic(n);
  if (semantic) return semantic === 'phone';
  // Missing semantic metadata must not install a destructive input filter.
  if (!Object.keys(FIELD_SEMANTICS).length) return false;
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
  hint.innerHTML = `${escapeHTMLText(rule.name)} numbers are ${escapeHTMLText(rule.length_label)}. ` +
    `For another country, start with <code>+</code> and its dialling code.`;
  input.insertAdjacentElement('afterend', hint);
}

// applyPhoneRulesIn sweeps a container and wires every phone-shaped field in
// it. One call per rendered form beats remembering to wire each field.
function applyPhoneRulesIn(container) {
  if (!container || !phoneRule()) return;
  container.querySelectorAll('input[type="text"], input[type="tel"], input:not([type])').forEach(input => {
    const semantic = fieldSemantic(input.name) || fieldSemantic(input.id);
    if (semantic && semantic !== 'phone') return;
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
  passwordChangeShowing = false;
  document.getElementById('password-change-form')?.classList.add('hidden');
  document.querySelectorAll('.login-card > .login-subtitle').forEach(el => el.classList.remove('hidden'));
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

// showPasswordChangeScreen (Stage 57.17): forced "set your own password" after
// an administrator chose it. apiMiddleware enforces the same on every call.
let passwordChangeShowing = false;
function showPasswordChangeScreen() {
  if (passwordChangeShowing) return;
  passwordChangeShowing = true;
  document.getElementById('app-root').classList.add('hidden');
  document.getElementById('login-screen').classList.remove('hidden');
  ['login-form', 'mfa-enroll-screen', 'mfa-challenge-screen'].forEach(id => document.getElementById(id)?.classList.add('hidden'));
  document.querySelectorAll('.login-card > .login-subtitle').forEach(el => el.classList.add('hidden'));
  const form = document.getElementById('password-change-form');
  form.reset();
  document.getElementById('pwchange-error').classList.add('hidden');
  form.classList.remove('hidden');
  document.getElementById('pwchange-current').focus();
}

async function handlePasswordChangeSubmit(event) {
  event.preventDefault();
  const errorEl = document.getElementById('pwchange-error');
  const fail = (msg) => { errorEl.textContent = msg; errorEl.classList.remove('hidden'); };
  errorEl.classList.add('hidden');
  const current = document.getElementById('pwchange-current').value;
  const next = document.getElementById('pwchange-new').value;
  const confirm = document.getElementById('pwchange-confirm').value;
  if (!current || !next) return fail('Enter the password you signed in with and a new one.');
  if (next !== confirm) return fail('The two new passwords do not match.');
  if (next === current) return fail('Choose a password different from the one you were given.');
  const btn = document.getElementById('pwchange-submit');
  btn.disabled = true;
  try {
    const res = await apiFetch('/api/v1/me/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: current, new_password: next })
    });
    if (!res) return;
    if (!res.ok) return fail(await getErrorMessage(res, 'Could not set the new password.'));
    const data = await res.json().catch(() => ({}));
    if (data.token) localStorage.setItem('erp_token', data.token);
    passwordChangeShowing = false;
    document.getElementById('password-change-form').classList.add('hidden');
    document.querySelectorAll('.login-card > .login-subtitle').forEach(el => el.classList.remove('hidden'));
    document.getElementById('login-form').classList.remove('hidden');
    showApp();
    init();
    showToast('Your new password is set.', { variant: 'success' });
  } finally {
    btn.disabled = false;
  }
}

function showApp() {
  document.getElementById('login-screen').classList.add('hidden');
  document.getElementById('app-root').classList.remove('hidden');
  updateSidebarUserInfo();
  applyIndustryLockUI();
}

// Stage 51.3: the industry-profile switch is now locked server-side after
// the first successful switch (engines.SwitchIndustryProfile's industry_lock
// row) - this reads that real state via GET /api/v1/admin/industry/lock and
// disables the selector with an Override affordance, replacing the old
// client-only localStorage memory ("There's no backend 'current industry'
// endpoint to read back" was true before this Stage; it no longer is).
async function applyIndustryLockUI() {
  const sel = document.getElementById('industry-selector');
  const overrideBtn = document.getElementById('industry-override-btn');
  if (!sel) return;
  // Admin-only endpoint: other roles take the fallback below without asking
  // (it used to log a 403 for every non-admin session at startup).
  const res = isAdminRoleName(localStorage.getItem('erp_role')) ? await apiFetch('/api/v1/admin/industry/lock') : null;
  if (!res || !res.ok) {
    // Not a Super Admin (403), or the check itself failed - fall back to the
    // old per-browser memory rather than showing nothing, and leave the
    // selector enabled since we can't confirm a lock either way.
    const saved = localStorage.getItem('erp_industry_code');
    if (saved && Array.from(sel.options).some(o => o.value === saved)) sel.value = saved;
    return;
  }
  const lock = await res.json();
  if (lock.locked) {
    const optionValue = (lock.industry_code || '').toLowerCase();
    if (Array.from(sel.options).some(o => o.value === optionValue)) sel.value = optionValue;
    sel.disabled = true;
    sel.title = `Locked to ${lock.industry_code} by ${lock.set_by} on ${new Date(lock.set_at).toLocaleDateString()}`;
    if (overrideBtn) overrideBtn.classList.remove('hidden');
  } else {
    sel.disabled = false;
    sel.title = '';
    if (overrideBtn) overrideBtn.classList.add('hidden');
  }
}

// switchIndustryProfile posts the switch, transparently retrying with an
// explicit override + reason if the server reports the tenant is already
// locked (409) - so the override path works whether the user got here via
// the (disabled, once locked) selector directly or the Override button.
async function switchIndustryProfile(code) {
  if (!(await showCustomConfirm(`Switch to active industry profile: ${code}? This will re-load preset table field configurations.`))) return;
  let res = await apiFetch('/api/v1/admin/industry', {
    method: 'POST',
    body: JSON.stringify({ industry_code: code })
  });
  if (res && res.status === 409) {
    const reason = await showCustomPrompt("This tenant's industry profile is already locked. Enter a reason to override and change it anyway:", '');
    if (!reason || !reason.trim()) return;
    res = await apiFetch('/api/v1/admin/industry', {
      method: 'POST',
      body: JSON.stringify({ industry_code: code, override: true, override_reason: reason.trim() })
    });
  }
  if (res && res.ok) {
    localStorage.setItem('erp_industry_code', code);
    await showCustomAlert('Industry configuration updated successfully!', 'Success');
    await fetchLabels();
    await fetchRegisteredDoctypes();
    renderView(currentView);
    await applyIndustryLockUI();
  } else if (res) {
    await showApiError(res, 'Failed to switch industry profile.');
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
  // Stage 57.17: an administrator chose this password - set your own first.
  if (data.password_change_required === 'true') {
    showPasswordChangeScreen();
    return;
  }
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
  document.getElementById('password-change-form').addEventListener('submit', handlePasswordChangeSubmit);
  document.getElementById('pwchange-signout').addEventListener('click', () => {
    passwordChangeShowing = false;
    document.getElementById('password-change-form').classList.add('hidden');
    document.querySelectorAll('.login-card > .login-subtitle').forEach(el => el.classList.remove('hidden'));
    logout();
  });
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
  fetchAndRenderEnvironmentBanner();
  loadAppVersion();
}

// Stage 47.0.4/47.0.5: fetch once at boot and show a persistent strip when
// this instance is not delivering real external side effects (email/
// webhook/ops-alert) - the common case outside a production deployment.
// Deliberately not on the critical init() Promise.all above: nothing else
// waits on it, and a slow/failed fetch should never delay the first view.
async function fetchAndRenderEnvironmentBanner() {
  try {
    const res = await apiFetch('/api/v1/system/environment');
    if (!res || !res.ok) return;
    const data = await res.json();
    const notices = data.supported_configuration_notice || [];
    // Stage 47.0.4: this must never disappear behind a menu - the audit's
    // own words. Shown whenever there is anything to say, regardless of
    // role, alongside (not instead of) the simulated-environment strip.
    if (data.external_side_effects && notices.length === 0) return;
    const parts = [];
    if (!data.external_side_effects) {
      parts.push(`SIMULATED ENVIRONMENT (${data.env}) - email, webhook and ops-alert delivery are OFF`);
    }
    if (notices.length > 0) {
      parts.push(`Conditionally supported: ${notices.join(' · ')}`);
    }
    const fullText = parts.join('  |  ');

    const banner = document.createElement('div');
    banner.id = 'environment-banner';

    const textEl = document.createElement('span');
    textEl.className = 'env-banner-text';
    textEl.textContent = fullText;
    banner.appendChild(textEl);

    // BLD-037: a real <button>, not the whole strip made clickable - this
    // message is read, not acted on, so only the explicit toggle needs a
    // keyboard/AX target (a native button gets that for free, no
    // makeClickable() role/tabindex retrofit needed).
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'env-banner-toggle';
    toggle.textContent = 'Show more';
    toggle.hidden = true;
    toggle.setAttribute('aria-expanded', 'false');
    banner.appendChild(toggle);
    document.body.appendChild(banner);
    document.body.classList.add('env-banner-active');

    const syncBannerHeight = () => {
      document.body.style.paddingTop = `${banner.offsetHeight}px`;
    };
    // Only the collapsed single line can actually overflow - once expanded
    // the toggle always stays visible (as "Show less"), so this only needs
    // to re-measure while collapsed.
    const checkTruncation = () => {
      if (banner.classList.contains('env-banner-expanded')) return;
      toggle.hidden = textEl.scrollWidth <= textEl.clientWidth + 1;
      syncBannerHeight();
    };
    toggle.addEventListener('click', () => {
      const expanded = banner.classList.toggle('env-banner-expanded');
      toggle.textContent = expanded ? 'Show less' : 'Show more';
      toggle.setAttribute('aria-expanded', String(expanded));
      toggle.hidden = false;
      syncBannerHeight();
    });
    checkTruncation();
    window.addEventListener('resize', checkTruncation);
  } catch (e) {
    // Non-critical - never block the app on this.
  }
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
  'menu-home': { open: true },
  'menu-pos': { doctypes: ['POSCart', 'POSSession'] },
  'menu-pos-profiles': { doctypes: ['POSProfile'] },
  'menu-pos-offline-sync': { doctypes: ['POSOfflineSyncVariance'] },
  'menu-pos-offline-gaps': { doctypes: ['POSOfflineQueueGap'] },

  'menu-finance': { modules: ['Finance'] },
  'menu-approvals': { open: true },
  'menu-vendor-invoices': { doctypes: ['VendorInvoice'] },
  'menu-payment-proposals': { doctypes: ['PaymentProposal', 'VendorInvoice'] },
  'menu-bank-reconciliation': { doctypes: ['BankAccount', 'BankStatementLine'] },
  'menu-finance-notes': { doctypes: ['DebitNote', 'CreditNote'] },
  'menu-sales-invoices': { doctypes: ['SalesInvoice'] },
  'menu-journal-vouchers': { doctypes: ['JournalVoucher'] },

  'menu-fulfillment': { modules: ['OMS'] },
  'menu-marketplace': { modules: ['OMS', 'PIM'] },
	'menu-oms': { modules: ['OMS'] },
  'menu-customers': { doctypes: ['Customer'] },
  // Stage 57.17: these two had no rule at all, so every role saw them.
  'menu-returns': { doctypes: ['SalesReturn'] },
  'menu-rf-traceability': { modules: ['Inventory'] },

  'menu-reports': { open: true },

  'menu-purchase-requisitions': { doctypes: ['PurchaseRequisition'] },
  'menu-purchase-orders': { doctypes: ['PurchaseOrder'] },
  'menu-grn': { doctypes: ['GRN'] },
  'menu-purchase-returns': { doctypes: ['PurchaseReturn'] },
  'menu-vendors': { doctypes: ['Vendor'] },
  'menu-rfq': { doctypes: ['RFQ'] },

  'menu-inventory': { modules: ['Inventory'] },
  'menu-transfers': { doctypes: ['TransferOrder'] },
  'menu-location-movement': { modules: ['Inventory'] },
  'menu-bins': { doctypes: ['Bin'] },
  // handlers_wms.go has no role_permissions check today (its own header
  // comment: "All role-open... a warehouse operator role doesn't exist
  // separately from Store Manager/Cashier/HR-Admin") - { open: true } here
  // matches that actual server behavior rather than inventing a UI-only gate.
  'menu-putaway': { modules: ['Inventory'] },
  // Stage 42.2.10: same role-open convention as the rest of the WMS
  // floor-ops screens (handlers_warehouse_task.go's cockpit route has no
  // role_permissions check either).
  'menu-warehouse-cockpit': { modules: ['Inventory'] },
  'menu-bin-conditions': { modules: ['Inventory'] },
  'menu-cycle-count': { modules: ['Inventory'] },
  // Stage 26.5: same role-open convention as the rest of the WMS floor-ops
  // screens above (handlers_wms_enterprise.go has no role_permissions check
  // either) - menu-asn is the exception, gated by the ASN doctype itself
  // since it goes through the generic /api/v1/doc/ASN endpoint.
  'menu-asn': { doctypes: ['ASN'] },
  'menu-lpn': { modules: ['Inventory'] },
  'menu-bin-replenishment': { modules: ['Inventory'] },
  'menu-wave-picking': { modules: ['Inventory'] },
  'menu-mobile-picking': { modules: ['Inventory'] },
  'menu-stickers': { modules: ['Inventory', 'PIM'] },
  // Stage 42.3: DockDoor/Trailer/HoldCode/Hold/HoldReleaseRequest all ride
  // the generic doctype-table screen, gated by the doctype's own
  // role_permissions exactly like menu-bins/menu-asn above. Appointment and
  // YardCheckIn have dedicated screens (calendar / yard board) but are
  // gated the same way since both still read/write through the generic
  // /api/v1/doc/{doctype} endpoint underneath.
  'menu-dock-doors': { doctypes: ['DockDoor'] },
  'menu-appointments': { doctypes: ['Appointment'] },
  'menu-yard-board': { doctypes: ['YardCheckIn'] },
  'menu-trailers': { doctypes: ['Trailer'] },
  'menu-hold-codes': { doctypes: ['HoldCode'] },
  'menu-holds': { doctypes: ['Hold'] },
  'menu-place-hold': { modules: ['Inventory'] },
  'menu-hold-release-requests': { doctypes: ['HoldReleaseRequest'] },
  'menu-crossdock-plans': { doctypes: ['CrossDockPlan'] },
  'menu-rf-receiving': { doctypes: ['GRN'] },
  // Stage 42.4: same "gated by the doctype's own role_permissions" pattern
  // as the Stage 42.3 rows above - Sortation/Loading read/write through the
  // generic /api/v1/doc/{doctype} endpoint underneath their dedicated screens.
  'menu-waves': { doctypes: ['Wave'] },
  'menu-sortation': { doctypes: ['SortSlot'] },
  'menu-loading': { doctypes: ['LoadingTask'] },

  'menu-hr': { doctypes: ['Employee'] },
  'menu-assets': { doctypes: ['Asset'] },
  'menu-expenses': { doctypes: ['ExpenseClaim'] },

  'menu-manufacturing': { doctypes: ['BOM', 'ProductionOrder'] },
  'menu-pim': { modules: ['PIM'] },

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

// Module navigation registry: sidebar entries and direct screen entry share
// one module declaration. @ entries are programmatic views without a menu.
// Generic record views resolve their module from server DocType metadata.
// BLD-021 source checks compare every dispatched screen with this registry.
// BLD-021: this object literal is machine-parsed as strict JSON by
// internal/server/module_manifest_test.go (TestModuleManifestCatalog) to
// cross-check the navigation registry against the real module manifest -
// no comments, trailing commas or other JS-only syntax inside it.
//
// menu-home's "core" module_key (is_core = TRUE, see
// migrations_stage39_9_help_feedback.sql's comment) is never disableable,
// same as "reports" below - Home must never go dark just because some
// unrelated module toggle is off.
const MENU_MODULE_MAP = {
  "menu-home": {
    "module": "core",
    "views": [
      "home"
    ]
  },
  "menu-putaway": {
    "module": "wms",
    "views": [
      "putaway"
    ]
  },
  "menu-warehouse-cockpit": {
    "module": "wms",
    "views": [
      "warehouse-cockpit"
    ]
  },
  "menu-place-hold": {
    "module": "wms",
    "views": [
      "place-hold"
    ]
  },
  "menu-bin-conditions": {
    "module": "wms",
    "views": [
      "bin-conditions"
    ]
  },
  "menu-cycle-count": {
    "module": "wms",
    "views": [
      "cycle-count"
    ]
  },
  "menu-lpn": {
    "module": "wms",
    "views": [
      "lpn"
    ]
  },
  "menu-bin-replenishment": {
    "module": "wms",
    "views": [
      "bin-replenishment"
    ]
  },
  "menu-location-movement": {
    "module": "wms",
    "views": [
      "location-movement"
    ]
  },
  "menu-wave-picking": {
    "module": "wms",
    "views": [
      "wave-picking"
    ]
  },
  "menu-fulfillment": {
    "module": "wms",
    "views": [
      "fulfillment"
    ]
  },
  "menu-marketplace": {
    "module": "oms",
    "views": [
      "marketplace"
    ]
  },
  "menu-oms": {
    "module": "oms",
    "views": [
      "oms"
    ]
  },
  "menu-returns": {
    "module": "oms",
    "views": [
      "returns"
    ]
  },
  "menu-rf-traceability": {
    "module": "wms",
    "views": [
      "rf-traceability"
    ]
  },
  "menu-purchase-requisitions": {
    "module": "procurement",
    "views": []
  },
  "menu-purchase-orders": {
    "module": "procurement",
    "views": [
      "purchase-orders"
    ]
  },
  "menu-grn": {
    "module": "procurement",
    "views": [
      "grn"
    ]
  },
  "menu-purchase-returns": {
    "module": "procurement",
    "views": [
      "purchase-returns"
    ]
  },
  "menu-asn": {
    "module": "procurement",
    "views": [
      "asn"
    ]
  },
  "menu-vendors": {
    "module": "procurement",
    "views": []
  },
  "menu-rfq": {
    "module": "rfq",
    "views": [
      "rfq"
    ]
  },
  "menu-stickers": {
    "module": "stickers",
    "views": [
      "stickers"
    ]
  },
  "menu-hr": {
    "module": "hr",
    "views": [
      "hr"
    ]
  },
  "menu-assets": {
    "module": "assets",
    "views": [
      "assets"
    ]
  },
  "menu-expenses": {
    "module": "expenses",
    "views": [
      "expenses"
    ]
  },
  "menu-manufacturing": {
    "module": "manufacturing",
    "views": [
      "manufacturing"
    ]
  },
  "menu-pim": {
    "module": "pim",
    "views": [
      "pim"
    ]
  },
  "menu-pos": {
    "module": "sales",
    "views": [
      "pos"
    ]
  },
  "menu-finance": {
    "module": "finance",
    "views": [
      "finance"
    ]
  },
  "menu-appointments": {
    "module": "wms",
    "views": [
      "appointment-calendar"
    ]
  },
  "menu-yard-board": {
    "module": "wms",
    "views": [
      "yard-board"
    ]
  },
  "menu-rf-receiving": {
    "module": "wms",
    "views": [
      "rf-receiving"
    ]
  },
  "menu-sortation": {
    "module": "wms",
    "views": [
      "sortation"
    ]
  },
  "menu-loading": {
    "module": "wms",
    "views": [
      "loading-dock"
    ]
  },
  "asn-create-btn": {
    "module": "core",
    "views": []
  },
  "@mobile-picking": {
    "module": "wms",
    "views": [
      "mobile-picking"
    ]
  },
  "menu-approvals": {
    "module": "core",
    "views": [
      "approvals"
    ]
  },
  "menu-reports": {
    "module": "reports",
    "views": [
      "reports"
    ]
  },
  "@doctype-table": {
    "module": "core",
    "views": [
      "doctype-table"
    ]
  },
  "menu-doctype-builder": {
    "module": "core",
    "views": [
      "doctype-builder"
    ]
  },
  "@prefix-configs": {
    "module": "core",
    "views": [
      "prefix-configs"
    ]
  },
  "@approval-rules": {
    "module": "core",
    "views": [
      "approval-rules"
    ]
  },
  "@dynamic-labels": {
    "module": "core",
    "views": [
      "dynamic-labels"
    ]
  },
  "hook-register-btn": {
    "module": "core",
    "views": [
      "extension-hooks"
    ]
  },
  "@extension-hook-log": {
    "module": "core",
    "views": [
      "extension-hook-log"
    ]
  },
  "custom-dialog-close-btn": {
    "module": "core",
    "views": [
      "audit-logs"
    ]
  },
  "@system-status": {
    "module": "core",
    "views": [
      "system-status"
    ]
  },
  "config-save-btn": {
    "module": "core",
    "views": [
      "configuration"
    ]
  },
  "@tenant-entitlements": {
    "module": "core",
    "views": [
      "tenant-entitlements"
    ]
  },
  "@tenant-usage": {
    "module": "core",
    "views": [
      "tenant-usage"
    ]
  },
  "account-menu-profile-btn": {
    "module": "core",
    "views": [
      "profile"
    ]
  },
  "menu-knowledge-center": {
    "module": "core",
    "views": [
      "help"
    ]
  },
  "transfer-create-btn": {
    "module": "inventory",
    "views": [
      "transfers"
    ]
  },
  "dashboard-save-btn": {
    "module": "inventory",
    "views": [
      "inventory"
    ]
  },
  "mfa-newdevice-btn": {
    "module": "core",
    "views": [
      "users"
    ]
  },
  "grant-save-btn": {
    "module": "core",
    "views": [
      "roles"
    ]
  },
  "menu-vendor-invoices": {
    "module": "procurement",
    "views": [
      "vendor-invoices"
    ]
  },
  "menu-payment-proposals": {
    "module": "finance",
    "views": [
      "payment-proposals"
    ]
  },
  "menu-bank-reconciliation": {
    "module": "finance",
    "views": [
      "bank-reconciliation"
    ]
  },
  "menu-finance-notes": {
    "module": "finance",
    "views": [
      "finance-notes"
    ]
  },
  "menu-journal-vouchers": {
    "module": "finance",
    "views": [
      "journal-vouchers"
    ]
  },
  "menu-sales-invoices": {
    "module": "sales",
    "views": [
      "sales-invoices"
    ]
  }
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
  // Stage 57.17: a module screen shows once the role can read anything in it
  // (these were `open` to every role).
  if (rule.modules) {
    return state.permissions.isAdmin ||
      state.activeDoctypes.some(d => rule.modules.includes(d.module) && canReadDoctype(d.name));
  }
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
      .filter(li => li.querySelector('.submenu-item[data-view], .menu-item') && !li.hasAttribute('data-cross-module'));
    const allHidden = items.every(li => li.classList.contains('perm-hidden'));
    container.classList.toggle('perm-hidden', allHidden);
  });
  syncFlyoutHeadings();
}

// isMenuModuleVisible mirrors isMenuRuleVisible: an item with no
// MENU_MODULE_MAP entry (an is_core module, or no module gate at all) is
// always visible; an unverified entitlement set hides gated navigation until
// the server confirms it. Direct views use the same fail-closed rule.
function isMenuModuleVisible(moduleKey) {
  if (!moduleKey) return true;
  if (state.modules.enabled === null) return false;
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
    item.classList.toggle('module-hidden', !isMenuModuleVisible(MENU_MODULE_MAP[id].module));
  });

  document.querySelectorAll('.has-flyout').forEach(container => {
    // Section headings (Stage 57) are never module-hidden themselves, so
    // they must not count or an empty flyout could never collapse.
    const items = Array.from(container.querySelectorAll('.menu-flyout > li')).filter(li => !li.classList.contains('submenu-group-label') && !li.hasAttribute('data-cross-module'));
    const allHidden = items.length > 0 && items.every(li => li.classList.contains('module-hidden'));
    container.classList.toggle('module-hidden', allHidden);
  });
  syncFlyoutHeadings();
}

// syncFlyoutHeadings hides a static flyout's section heading (the Stock
// menu's "WMS", Stage 57) when every entry under it is hidden for this role
// or tenant, so a heading never sits over nothing. Setup's own dynamically
// built headings are left to renderSidebarSubmenu.
function syncFlyoutHeadings() {
  document.querySelectorAll('.has-flyout > .menu-flyout > li.submenu-group-label').forEach(heading => {
    if (heading.closest('#menu-master-definition-submenu, .submenu')) return;
    let sibling = heading.nextElementSibling;
    let anyVisible = false;
    while (sibling && !sibling.classList.contains('submenu-group-label')) {
      if (sibling.querySelector('.menu-item') && !sibling.classList.contains('perm-hidden') && !sibling.classList.contains('module-hidden')) { anyVisible = true; break; }
      sibling = sibling.nextElementSibling;
    }
    heading.classList.toggle('hidden', !anyVisible);
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
        // Stage 47.2.3: capability-gated actions (POS price override) show
        // or hide from this, not from a hardcoded role name. Server-side
        // enforcement is unchanged and unconditional; this only avoids
        // offering a button that would be refused.
        capabilities: new Set(data.capabilities || []),
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
    String(getDoctypeLabel(d.name)).toLowerCase().includes(needle) ||
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
        .sort((a, b) => String(getDoctypeLabel(a.name)).localeCompare(String(getDoctypeLabel(b.name))))
        .forEach(d => {
          const li = document.createElement('li');
          li.innerHTML = `<a class="submenu-item" data-view="${escapeHTMLText(d.name)}">${escapeHTMLText(getDoctypeLabel(d.name))}</a>`;
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

// --- Navigation drawer (Stage 47.6.3) --------------------------------------
//
// Below the operator breakpoint the sidebar leaves the layout flow and becomes
// an off-canvas drawer (see the media query in styles.css). Before this, the
// sidebar was a hard 270px with no collapsed state anywhere in the stylesheet,
// so on a 390px device it took 69% of the viewport and left 120px for the
// application - the concrete form of audit A-06/A-39's "a desktop page
// squeezed into 390px".
//
// The keyboard and focus behaviour here is not decoration: an open drawer that
// cannot be closed with Escape, or that leaves focus behind it, is a WCAG
// failure (SC 2.1.2 No Keyboard Trap, SC 2.4.3 Focus Order) and, more
// practically, is unusable with a ring scanner that only sends Tab/Enter.

// The breakpoint is duplicated from styles.css deliberately and checked with
// matchMedia rather than a hardcoded width comparison, so the JS follows
// whatever the CSS actually applied - including at a zoom level where the CSS
// pixel width and the device width disagree.
const NAV_DRAWER_QUERY = '(max-width: 820px)';

function navDrawerIsActive() {
  return window.matchMedia && window.matchMedia(NAV_DRAWER_QUERY).matches;
}

function setNavDrawer(open) {
  const container = document.getElementById('app-root');
  const toggle = document.getElementById('nav-toggle');
  const scrim = document.getElementById('nav-scrim');
  if (!container) return;
  container.classList.toggle('nav-open', !!open);
  if (toggle) {
    toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    toggle.setAttribute('aria-label', open ? 'Close navigation' : 'Open navigation');
  }
  // The scrim is `hidden` rather than only CSS-hidden so it is removed from
  // the accessibility tree as well as the picture - a screen reader should not
  // find a clickable nothing sitting over the page.
  if (scrim) scrim.hidden = !open;
  if (open) {
    // Move focus into the drawer so the next Tab lands inside it rather than
    // continuing through the page behind the scrim.
    const first = document.querySelector('#app-sidebar .menu-item');
    if (first && first.focus) first.focus();
  } else if (toggle && navDrawerIsActive() && toggle.focus) {
    // Returning focus to the control that opened it is what stops a keyboard
    // user being dropped at the top of the document on every close.
    toggle.focus();
  }
}

function closeNavDrawer() {
  setNavDrawer(false);
}

// Found during the BLD-038 accessibility sweep: every `.modal-overlay`
// dialog hides itself with `opacity: 0; pointer-events: none;`, not
// `display: none`. That leaves a closed dialog fully in the tab order and
// the accessibility tree - a keyboard user tabbing through ANY screen lands
// on 50+ invisible controls belonging to whichever dialogs happen to be
// closed, confirmed live via a 120-press Tab walk from Home. The real
// open/close functions (openDynamicModal/closeDynamicModal,
// openFieldModal/closeAddFieldModal, openImportModal/closeImportModal) each
// now set `.inert` explicitly alongside `.open` - a MutationObserver-based
// version of this was tried first, but at least one of them
// (openFieldModal) calls .focus() on a field INSIDE the modal in the same
// synchronous call that opens it, and a MutationObserver callback is a
// microtask that had not yet run by then, so the un-inerting lost the race
// and the focus() call silently failed. This one-time pass only has to
// cover what those functions do not: #edit-prefix-modal/#add-label-modal,
// two more `.modal-overlay`s confirmed dead - no classList reference to
// their ids anywhere in this file - left over from before those flows moved
// to prompt()/inline rows, so nothing ever opens or closes them.
function initModalInertSync() {
  document.querySelectorAll('.modal-overlay').forEach(overlay => {
    overlay.inert = !overlay.classList.contains('open');
  });
}

function initNavDrawer() {
  const toggle = document.getElementById('nav-toggle');
  const scrim = document.getElementById('nav-scrim');
  const container = document.getElementById('app-root');
  if (!toggle || !container) return;

  toggle.addEventListener('click', (e) => {
    e.preventDefault();
    setNavDrawer(!container.classList.contains('nav-open'));
  });
  if (scrim) scrim.addEventListener('click', closeNavDrawer);

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && container.classList.contains('nav-open')) {
      closeNavDrawer();
    }
  });

  // Choosing a destination closes the drawer - on a phone the operator wants
  // the screen they picked, not the menu they picked it from. Delegated from
  // the sidebar so it covers the dynamically-rendered Setup submenu too.
  const sidebar = document.getElementById('app-sidebar');
  if (sidebar) {
    sidebar.addEventListener('click', (e) => {
      if (!navDrawerIsActive()) return;
      const item = e.target && e.target.closest && e.target.closest('.menu-item');
      // A module row that owns a flyout is a disclosure, not a destination -
      // closing on it would shut the menu before its children could be read.
      if (item && !item.closest('.has-submenu')) closeNavDrawer();
    });
  }

  // Resizing back above the breakpoint (rotating a tablet, undoing zoom) must
  // clear the open state, or the desktop layout renders with a drawer class
  // still applied and the scrim still in the accessibility tree.
  if (window.matchMedia) {
    const mq = window.matchMedia(NAV_DRAWER_QUERY);
    const onChange = () => { if (!mq.matches) closeNavDrawer(); };
    if (mq.addEventListener) mq.addEventListener('change', onChange);
    else if (mq.addListener) mq.addListener(onChange);
  }
}

function setupEventListeners() {
  // Stage 47.6.3: the operator-width navigation drawer.
  initNavDrawer();
  initModalInertSync();

  // Main Navigation links
  document.getElementById('menu-home').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-home');
    closeSubmenus();
    renderView('home');
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

	document.getElementById('menu-oms').addEventListener('click', (e) => {
		e.preventDefault();
		setActiveMenu('menu-oms');
		closeSubmenus();
		renderView('oms');
	});

  const rfMenu = document.getElementById('menu-rf-traceability');
  if (rfMenu) {
    rfMenu.addEventListener('click', (e) => {
      e.preventDefault();
      setActiveMenu('menu-rf-traceability');
      closeSubmenus();
      renderView('rf-traceability');
    });
  }

  const returnsMenu = document.getElementById('menu-returns');
  if (returnsMenu) {
    returnsMenu.addEventListener('click', (e) => {
      e.preventDefault();
      setActiveMenu('menu-returns');
      closeSubmenus();
      renderView('returns');
    });
  }

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

  document.getElementById('menu-knowledge-center').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-knowledge-center');
    closeSubmenus();
    currentHelpSlug = '';
    renderView('help');
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

  // Purchase Return (Stage 57.15) - dedicated screen for the same reason as
  // the GRN Workbench: its lines come from the GRN, not from hand-typed JSON.
  document.getElementById('menu-purchase-returns').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-purchase-returns');
    closeSubmenus();
    renderView('purchase-returns');
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

  // Stage 42.3.1/42.3.4/42.3.5: masters/logs on the generic doctype-table
  // pattern, same as Bin above.
  document.getElementById('menu-dock-doors').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-dock-doors'); closeSubmenus(); currentDoctype = 'DockDoor'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-trailers').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-trailers'); closeSubmenus(); currentDoctype = 'Trailer'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-hold-codes').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-hold-codes'); closeSubmenus(); currentDoctype = 'HoldCode'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-holds').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-holds'); closeSubmenus(); currentDoctype = 'Hold'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-hold-release-requests').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-hold-release-requests'); closeSubmenus(); currentDoctype = 'HoldReleaseRequest'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-crossdock-plans').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-crossdock-plans'); closeSubmenus(); currentDoctype = 'CrossDockPlan'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-rf-receiving').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-rf-receiving'); closeSubmenus(); renderView('rf-receiving'); });
  document.getElementById('menu-waves').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-waves'); closeSubmenus(); currentDoctype = 'Wave'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });
  document.getElementById('menu-sortation').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-sortation'); closeSubmenus(); renderView('sortation'); });
  document.getElementById('menu-loading').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-loading'); closeSubmenus(); renderView('loading-dock'); });
  document.getElementById('menu-appointments').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-appointments'); closeSubmenus(); renderView('appointment-calendar'); });
  document.getElementById('menu-yard-board').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-yard-board'); closeSubmenus(); renderView('yard-board'); });
  document.getElementById('menu-place-hold').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-place-hold'); closeSubmenus(); renderView('place-hold'); });

  // Offline Sync Review (Stage 20.13) - same generic doctype-table pattern as POS Profile/Bin above.
  document.getElementById('menu-pos-offline-sync').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-offline-sync'); closeSubmenus(); currentDoctype = 'POSOfflineSyncVariance'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  // Offline Queue Gaps (24.36) - same generic doctype-table pattern as Offline Sync Review above.
  document.getElementById('menu-pos-offline-gaps').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-pos-offline-gaps'); closeSubmenus(); currentDoctype = 'POSOfflineQueueGap'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });

  ['menu-inventory', 'menu-journal-vouchers', 'menu-transfers', 'menu-location-movement', 'menu-putaway', 'menu-warehouse-cockpit', 'menu-bin-conditions', 'menu-cycle-count', 'menu-asn', 'menu-lpn', 'menu-bin-replenishment', 'menu-wave-picking', 'menu-mobile-picking', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-approval-rules', 'menu-dynamic-labels', 'menu-extension-hooks', 'menu-audit-logs', 'menu-system-status', 'menu-configuration', 'menu-tenant-entitlements', 'menu-tenant-usage'].forEach(id => {
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

  // Refresh (Stage 53.17: was labelled "Sync"). Re-fetches the two things that
  // go stale when an administrator changes a label or a doctype field, and
  // touches nothing else - every cache, and every half-finished screen, is
  // deliberately left exactly as it was. That is the whole distinction from
  // Reset below.
  document.getElementById('sync-btn').addEventListener('click', async () => {
    if (await showCustomConfirm('Re-fetch labels and screen definitions from the server?\n\nEverything you have open stays as it is, and cached data is kept. If you need a genuinely clean slate, use Reset instead.', 'Refresh')) {
      await fetchLabels();
      await fetchRegisteredDoctypes();
      renderView(currentView);
      showToast('Labels and screen definitions re-fetched.', { variant: 'success', title: 'Refreshed' });
    }
  });

  document.getElementById('reset-btn')?.addEventListener('click', resetClientState);

  // Stage 39.5: contextual help for the screen you are on.
  document.getElementById('help-btn')?.addEventListener('click', () => openHelpDrawer(currentView));

  const indSelector = document.getElementById('industry-selector');
  if (indSelector) {
    indSelector.addEventListener('change', async (e) => {
      const code = e.target.value;
      if (!code) return;
      await switchIndustryProfile(code);
    });
  }
  const industryOverrideBtn = document.getElementById('industry-override-btn');
  if (industryOverrideBtn) {
    industryOverrideBtn.addEventListener('click', async () => {
      const code = await showCustomPrompt('Industry code to switch to (jewelry, food_bev, auto, clothing, pharma, metal, construction, medical, semiconductor, agriculture):', indSelector ? indSelector.value : '');
      if (!code || !code.trim()) return;
      await switchIndustryProfile(code.trim());
    });
  }

  setupAccountMenu();
}

// ---------------------------------------------------------------------------
// Reset (Stage 53.17)
//
// Why this is a second button and not a bigger Sync.
//
// Refresh re-asks the server for labels and screen definitions. Everything
// else survives it: the per-tab stale-while-revalidate cache (SWR_PREFIX in
// sessionStorage, which backs every record list and field-metadata read), the
// in-memory `state` object, the sidebar's remembered open/closed sections, and
// whatever half-finished work is sitting on the current screen. That is the
// right behaviour for "a label changed" and the wrong behaviour for "this
// screen is showing me something that is not true any more".
//
// A browser reload is not the cure for the second case either, which is the
// trap worth naming: F5 re-runs the app against the SAME sessionStorage and
// localStorage, so the stale copy is read straight back in. Reset is the only
// thing that drops the cached copy before rebuilding from the server.
//
// What it clears:
//   - every SWR entry in sessionStorage (the real "cache" in this app)
//   - the in-memory state caches - doctype list, field metadata, fetched rows,
//     label map, permissions - all re-fetched immediately afterwards
//   - the sidebar nav state
//   - all unsaved on-screen work, including the POS till (cart, customer,
//     coupon, discount, tender, frozen bill, store binding)
//
// What it deliberately does NOT clear, and why each one:
//   - Unsynced offline sales. These are sales that happened; the customer has
//     walked out with the goods. They are not cache. Reset REFUSES outright
//     while any are queued rather than quietly destroying them - the same
//     posture closePOSSessionFlow already takes for the same reason.
//   - Your session. Reset is not Sign Out; a cashier mid-shift must not be
//     thrown back to a login screen for asking a screen to redraw cleanly.
//   - Your theme. A personal preference is not stale data.
async function resetClientState() {
  // Guard first, before anything is cleared and before the user is asked to
  // confirm - there is no point confirming an action that must not proceed.
  const queued = getOfflineQueue().length;
  if (queued > 0) {
    await showCustomAlert(
      `${queued} offline sale${queued === 1 ? '' : 's'} ${queued === 1 ? 'is' : 'are'} still waiting to reach the server. Reset will not run while that is true - these are completed sales, not cached data, and clearing them would lose them.\n\nReconnect and let them sync (or click the offline badge on the POS screen to sync now), then try Reset again.`,
      'Unsynced Sales — Reset Blocked');
    return;
  }

  const confirmed = await showCustomConfirm(
    'Reset clears every cached copy this browser is holding and discards all unsaved work on screen — including anything in the POS till that has not been completed.\n\n' +
    'Your login, your theme and everything already saved on the server are untouched, and nothing is deleted from the server.\n\n' +
    'Reset now?', 'Reset');
  if (!confirmed) return;

  // 1. The cache the user actually means. Collect the keys first and delete
  //    afterwards: removing from sessionStorage while iterating it by index
  //    reindexes the remaining entries and silently skips every other one.
  try {
    const swrKeys = [];
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i);
      if (key && key.startsWith(SWR_PREFIX)) swrKeys.push(key);
    }
    swrKeys.forEach(k => sessionStorage.removeItem(k));
    sessionStorage.removeItem('erp_setup_advanced_open');
  } catch (e) {
    // Private browsing / quota / storage disabled. A cache that cannot be read
    // cannot be stale either, so this is not a failure worth stopping for.
  }

  // 2. The sidebar's remembered shape. Left to rebuild from the fresh doctype
  //    list rather than from a remembered one that may name retired screens.
  try { localStorage.removeItem(NAV_STATE_KEY); } catch (e) { /* see above */ }

  // 3. In-memory caches. Re-fetched immediately below; emptied first so a
  //    failed re-fetch shows an honest empty screen rather than stale rows
  //    the user would have no way to recognise as stale.
  state.activeDoctypes = [];
  state.activeDocFields = [];
  state.docData = [];
  state.docTotal = 0;
  state.prefixConfigs = [];
  state.approvalRules = [];
  state.labels = {};
  state.auditLogs = [];
  state.systemLogs = [];
  quickCreateReturn = null;

  // 4. The till. Called through window because view-pos.js is lazy-loaded
  //    (BLD-041) - on a browser that has never opened POS the module is not
  //    there, and that is not an error, it is simply nothing to clear.
  posCart = [];
  posCartNumber = '';
  posOpenSessionId = '';
  if (typeof window.clearPOSTillState === 'function') {
    try { window.clearPOSTillState(); } catch (e) { /* the screen is about to be rebuilt anyway */ }
  }

  // 5. The screen code itself. Dropping the module registry alone would not be
  //    enough - the next import() would be served the identical URL out of the
  //    browser's HTTP cache. The bust param is what makes the re-fetch real,
  //    and re-importing under a new URL also re-runs each module's top-level
  //    state (a frozen till, a locked store) back to its initial values.
  viewModuleCacheBust = String(Date.now());
  loadedViewModules.clear();
  viewModuleLoadAttempts.clear();

  // 6. Rebuild from the server.
  await fetchLabels();
  await fetchRegisteredDoctypes();
  await fetchAndApplyPermissions();
  renderView(currentView);
  showToast('Cached data cleared and reloaded from the server.', { variant: 'success', title: 'Reset Complete' });
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

// ---------------------------------------------------------------------------
// Release identity: the quiet "v0.1.0 · 4 Oct 2026" line under the profile
// chip in the sidebar footer.
//
// Nothing here is hand-maintained. The semver comes from the embedded
// internal/server/VERSION file and the release date is derived server-side
// from the -ldflags build timestamp that manage.ps1, deploy/deploy.ps1,
// promote.ps1 and CI all stamp into every release build - so a deploy
// updates what this shows by itself, with no file to remember to edit.
// ---------------------------------------------------------------------------

async function loadAppVersion() {
  const line = document.getElementById('app-version-line');
  if (!line) return;
  let info;
  try {
    // Public route (see publicRoutes server-side), so a plain fetch rather
    // than apiFetch: this should still render if a tenant/permission call
    // fails. no-store because the whole point is to reflect the binary
    // running right now, not what a cached response said before a deploy.
    const res = await fetch('/api/v1/version', { cache: 'no-store' });
    if (!res.ok) return;
    info = await res.json();
  } catch (_) {
    return; // Offline or blocked: the line simply stays hidden.
  }
  if (!info || !info.version) return;

  // The sidebar carries the bare number. Everything else - the word
  // "Version", the word "Released", the spelled-out month - was stripped on
  // the user's call (2026-10-04): this is a reference string, not a sentence.
  line.textContent = info.version;

  const num = document.getElementById('app-version-pop-num');
  const date = document.getElementById('app-version-pop-date');
  if (num) num.textContent = info.version;
  if (date) date.textContent = formatReleaseDate(info.release_date) || 'dev build';

  line.classList.remove('hidden');
  setupAppVersionPopup();
}

// "4/10/26" from "2026-10-04" - day/month/two-digit year, built from the ISO
// parts rather than toLocaleDateString. Locale formatting was the first
// implementation and was wrong here twice over: it renders US month-first
// ("10/4/26") on a US-locale browser, inverting the day and month for an
// Indian reader, and it cannot be pinned without hardcoding a locale anyway.
// Returns '' for the empty release_date an unstamped `go build` produces,
// which the caller renders as a dev build.
function formatReleaseDate(isoDate) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(isoDate || '');
  if (!m) return '';
  const [, year, month, day] = m;
  return `${Number(day)}/${Number(month)}/${year.slice(2)}`;
}

// Hover, keyboard focus and tap all open the popup; it closes on leave, blur,
// Escape, or an outside click. Touch needs the click path because a device
// with no pointer never fires mouseenter - which is exactly why the native
// `title` tooltip this replaced was unreachable on a phone.
function setupAppVersionPopup() {
  const line = document.getElementById('app-version-line');
  const pop = document.getElementById('app-version-pop');
  if (!line || !pop || line.dataset.popupBound) return;
  line.dataset.popupBound = '1';

  const show = () => { pop.classList.remove('hidden'); line.setAttribute('aria-expanded', 'true'); };
  const hide = () => { pop.classList.add('hidden'); line.setAttribute('aria-expanded', 'false'); };

  line.addEventListener('mouseenter', show);
  line.addEventListener('mouseleave', hide);
  line.addEventListener('focus', show);
  line.addEventListener('blur', hide);
  line.addEventListener('click', (e) => {
    e.stopPropagation();
    pop.classList.contains('hidden') ? show() : hide();
  });
  document.addEventListener('click', (e) => {
    if (!pop.classList.contains('hidden') && !line.contains(e.target)) hide();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') hide();
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
    if (seen.has('label:' + getDoctypeLabel(d.name).toLowerCase())) return;
    if (seen.has(key)) return;
    seen.add(key);
    entries.push({
      kind: 'Record type',
      label: getDoctypeLabel(d.name),
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
  home: 'menu-home',
  pos: 'menu-pos',
  finance: 'menu-finance',
  fulfillment: 'menu-fulfillment',
  marketplace: 'menu-marketplace',
	oms: 'menu-oms',
  'rf-traceability': 'menu-rf-traceability',
  returns: 'menu-returns',
  approvals: 'menu-approvals',
  reports: 'menu-reports',
  help: 'menu-knowledge-center',
  rfq: 'menu-rfq',
  stickers: 'menu-stickers',
  'appointment-calendar': 'menu-appointments',
  'yard-board': 'menu-yard-board',
  'place-hold': 'menu-place-hold',
  'rf-receiving': 'menu-rf-receiving',
  sortation: 'menu-sortation',
  'loading-dock': 'menu-loading',
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
  'purchase-returns': 'menu-purchase-returns',
  inventory: 'menu-inventory',
  transfers: 'menu-transfers',
  'location-movement': 'menu-location-movement',
  putaway: 'menu-putaway',
  'warehouse-cockpit': 'menu-warehouse-cockpit',
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
  'sales-invoices': 'menu-sales-invoices',
  'journal-vouchers': 'menu-journal-vouchers'
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
// Stage 45: navigation is not serialised anywhere upstream (menu clicks,
// deep links, restoreLastView all call this directly), so two calls can be
// in flight together - a double click, or any other duplicate trigger fired
// before the first one's fetch has resolved. Previously both calls appended
// straight into #view-root, so a losing call's content landed permanently
// alongside the winner's instead of being discarded. viewRenderToken makes
// each call check, after its own fetch settles, whether a newer call has
// since started; a stale call still cleans up its own loading placeholder
// but never touches #view-root otherwise.
let viewRenderToken = 0;

async function renderView(view) {
  const myToken = ++viewRenderToken;
  const root = document.getElementById('view-root');
  root.innerHTML = '';
  root.scrollTop = 0;

  const placeholder = document.createElement('div');
  placeholder.className = 'view-loading';
  placeholder.innerHTML = '<div class="view-loading-bar"></div><span>Loading&hellip;</span>';
  root.appendChild(placeholder);

  // Built in a container that is NOT #view-root, so a stale call has
  // something harmless to throw away instead of DOM nodes that were ever
  // actually visible - but it must still be attached to the live document
  // (off-screen, not display:none - Chromium skips layout for display:none
  // subtrees, which broke position:sticky table headers the first time this
  // was tried), not a fully detached node: a render*View function's own
  // document.getElementById(...) calls for elements it just created (the
  // overwhelming majority of them, e.g. renderPutawayView's
  // "putaway-submit-btn" wiring) only find those elements while they live
  // in the actual document - detached, getElementById simply can never see
  // them, however deep an await this function is awaiting. Found live
  // (Stage 42.2.7's own Playwright pass): every renderer that wires an event
  // listener or reads/writes an input's value right after building it was
  // silently broken by this the moment Stage 45 introduced the detached
  // scratch buffer - confirmed on Putaway, Bin Conditions and Wave Picking,
  // so this is not a one-screen bug, it is the buffering mechanism itself.
  const scratch = document.createElement('div');
  scratch.style.cssText = 'position:absolute; left:-99999px; top:0; width:1px; overflow:hidden;';
  document.body.appendChild(scratch);
  try {
    await renderViewContent(view, scratch);
  } finally {
    // finally, not after the await: a renderer that throws must not leave a
    // permanent "Loading..." on screen, which would be a worse lie than the
    // blank panel this replaces.
    placeholder.remove();
    if (myToken !== viewRenderToken) { scratch.remove(); return; }
    while (scratch.firstChild) root.appendChild(scratch.firstChild);
    scratch.remove();
    // Chromium can cache a stale containing-block rect for `position: sticky`
    // content (e.g. a table's frozen header row) when it's inserted into a
    // scroll container in the very same reflow that establishes that
    // container's own scrollable overflow - the sticky element then just
    // scrolls away with the page instead of freezing. One forced layout read
    // after paint fixes it; done once here so every view's tables get it for
    // free instead of patching each table-rendering function individually.
    requestAnimationFrame(() => { void root.offsetHeight; });
    setTimeout(translateDOM, 50);
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
    // Stage 57.1: names instead of stored codes in every table, this view and
    // whatever it renders later (installed once, then self-sustaining).
    startLinkNameSweep();
    // Stage 57.6: the shared date picker, same once-installed pattern.
    startDatePickerEnhancer();
  }
}

async function renderViewContent(view, root) {
  // Invalidate a pending quick-create return target the moment navigation
  // strays anywhere other than the new master's own list or back to the
  // screen the shortcut was launched from - otherwise a later, unrelated
  // save could still see it and redirect somewhere stale (e.g. abandon a
  // GRN item quick-create, browse to Vendors instead, save a new Vendor
  // there, land back on GRN uninvited). See quickCreateReturn's declaration.
  if (quickCreateReturn) {
    const stillRelevant = view === quickCreateReturn.view ||
      (view === 'doctype-table' && currentDoctype === quickCreateReturn.forDoctype);
    if (!stillRelevant) quickCreateReturn = null;
  }

  // BLD-021: direct links and restored navigation obey the same registry as
  // sidebar visibility. The server independently enforces the entitlement.
  const entry = Object.values(MENU_MODULE_MAP).find(item => item.views.includes(view));
  if (!state.modules.loaded) await fetchAndApplyModules();
  const moduleKey = view === 'doctype-table'
    ? (state.activeDoctypes.find(item => item.name === currentDoctype) || {}).module_key
    : entry && entry.module;
  if (!moduleKey || !isMenuModuleVisible(moduleKey)) {
    currentView = view;
    document.body.classList.remove('rf-shell');
    root.innerHTML = '<div class="table-panel" role="status"><h2>Unavailable</h2><p>This page is not available for your current modules. Contact your administrator if you need access.</p></div>';
    return;
  }
  currentView = view;
  saveNavState();

  // Stage 47.6.2: the RF shell hides the sidebar and the top bar while a task
  // is open, so leaving it has to put them back. Cleared here - the one place
  // every view change passes through - rather than in each view's own render,
  // which is how a screen ends up permanently chromeless after one bad exit.
  document.body.classList.toggle('rf-shell', view === 'rf-traceability');

  if (view === 'home') {
    await renderHomeView(root);
  } else if (LAZY_VIEW_MODULES[view]) {
    await renderLazyView(view, root, LAZY_VIEW_MODULES[view]);
  } else if (view === 'profile') {
    await renderProfileView(root);
  } else if (view === 'users') {
    await renderUsersView(root);
  } else if (view === 'roles') {
    await renderRolesView(root);
  } else {
    renderMockModuleView(root, view);
  }
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

// BLD-037: humanizes a raw PascalCase/camelCase identifier ("PurchaseOrder",
// "POSCart") into spaced words ("Purchase Order", "POS Cart"). No migration
// ever seeds a tenant's custom_labels, so any doctype nobody has explicitly
// relabeled - the common case - used to reach the caller as the bare
// internal name, wherever getTranslatedLabel is called (page titles, the
// Setup sidebar, dialog headings, ~30 call sites). Idempotent: an
// already-spaced string or a standalone all-caps acronym ("Sales Invoice",
// "GRN") has no lower-to-upper or upper-run-to-upper+lower transition to
// anchor on, so it passes through unchanged.
function humanizeIdentifier(text) {
  return text
    // an acronym run right before a capitalized word: "GSTReturn" -> "GST Return".
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    // a lowercase letter or digit directly followed by an uppercase: "PurchaseOrder" -> "Purchase Order"
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    // a letter directly followed by a digit: "Item2" -> "Item 2"
    .replace(/([A-Za-z])([0-9])/g, '$1 $2');
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

// getDoctypeLabel is getTranslatedLabel plus the identifier humanizer above,
// for the specific call sites that render a DOCTYPE NAME as a heading -
// never for translateDOM()'s blind whole-page sweep (above), which calls
// getTranslatedLabel directly on arbitrary leaf text (banners, table cells,
// numbers...) and depends on its untouched-when-no-match behavior to stay a
// safe no-op - humanizing there mangled real strings like "3PL" -> "3 PL"
// and "/api/v1/" -> "/api/v 1/" (found live, reverted before shipping).
// Already-humanized text never matches translateDOM's later re-scan (its
// raw doctype-name keys have no space), so the two passes cannot conflict.
function getDoctypeLabel(name) {
  return humanizeIdentifier(getTranslatedLabel(name));
}

// BLD-035: a shared translation from a document's real stored `status` value
// (the vocabulary engines/approval.go and the vendor-invoice/POS finalize
// paths actually use - Draft/Pending Approval/Approved/Rejected/Paid/
// Cancelled) into the sentence a save/submit/decide action should show. This
// is the fix for MC-042/MC-089 ("a 200 response alone never renders 'Paid'"):
// every generic save used to close its dialog and re-render with no message
// at all, regardless of whether the document was a plain Draft or had just
// been quietly reset to Pending Approval. Every caller passes the doctype's
// own label so the same status reads naturally for different record kinds.
function describeDocumentStatusOutcome(doctypeLabel, status) {
  switch (status) {
    case 'Draft':
      return `${doctypeLabel} saved as Draft.`;
    case 'Pending Approval':
      return `${doctypeLabel} saved - now pending approval.`;
    case 'Approved':
      return `${doctypeLabel} approved.`;
    case 'Rejected':
      return `${doctypeLabel} rejected.`;
    case 'Paid':
      return `${doctypeLabel} paid.`;
    case 'Cancelled':
      return `${doctypeLabel} cancelled.`;
    default:
      return `${doctypeLabel} saved.`;
  }
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
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-settings-current-password">Current Password (only needed if changing Email)</label>
          <input type="password" id="profile-settings-current-password" class="form-input" autocomplete="current-password">
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
          <input type="password" id="profile-new-password" class="form-input" autocomplete="new-password" minlength="12" required>
        </div>
        <div class="form-group" style="margin-bottom: 0;">
          <label class="form-label" for="profile-confirm-password">Confirm New Password</label>
          <input type="password" id="profile-confirm-password" class="form-input" autocomplete="new-password" minlength="12" required>
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
        <input type="password" id="user-password" class="form-input" style="width: 150px;" autocomplete="new-password" minlength="12">
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
            <button class="action-btn" ${actionAttrs('setUserLocation', [u.id, u.location_code || 'HO'])}>Set Location</button>
            <button class="action-btn" ${actionAttrs('resetUserMFA', [u.id, u.username])}>Reset 2FA</button>
            <button class="action-btn" ${actionAttrs('resetUserPassword', [u.id, u.username])}>Reset Password</button>
            ${u.role === 'Supplier' ? `<button class="action-btn" ${actionAttrs('setUserSupplier', [u.id, u.supplier_code || ''])}>Link Vendor</button>` : ''}
            ${u.status === 'Active'
              ? `<button class="action-btn action-btn-danger" ${actionAttrs('setUserStatus', [u.id, 'Inactive'])}>Deactivate</button>`
              : `<button class="action-btn" ${actionAttrs('setUserStatus', [u.id, 'Active'])}>Reactivate</button>`}
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

// 49.2.4: the admin-assisted "helpdesk" password reset. An ordinary account
// is reset immediately and the one-time password is shown once, right here -
// it is never shown again and never emailed, so it must be relayed to the
// user through a secure out-of-band channel (phone call, in person). A Super
// Admin target instead comes back "pending_approval": the request now shows
// up on a second Super Admin's Approvals screen, and only THAT approver ever
// sees the generated password (via decideApproval below) - the point of
// requiring a second admin is that the requester alone must not learn it.
window.resetUserPassword = async function(id, username) {
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

  // Stage 57.17: roles are created here before anyone holds one, and a role
  // can be given a whole module at once - "the user sees that module only".
  const moduleOptions = [...new Set(state.activeDoctypes.map(d => d.module).filter(Boolean))].sort();
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Roles</h1>
      <p class="page-subtitle">What each role can see and do. A role sees a module in the menu once it can read that module's records. Super Admin can always do everything; this only governs the other roles.</p>
    </div>
  `;
  container.appendChild(header);

  const createPanel = document.createElement('div');
  createPanel.className = 'table-panel';
  createPanel.style.padding = '24px';
  createPanel.style.marginBottom = '24px';
  createPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 4px;">Create a Role</h2>
    <p style="font-size: 13px; color: var(--text-muted); margin-bottom: 16px;">A new role can see nothing until you grant it access below. Then assign it to users on the Users screen.</p>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="new-role-name">Role name</label>
        <input type="text" id="new-role-name" class="form-input" style="width: 200px;" placeholder="e.g. Purchase Clerk" maxlength="60">
      </div>
      <div class="form-group" style="margin-bottom: 0; flex: 1; min-width: 200px;">
        <label class="form-label" for="new-role-description">Description (optional)</label>
        <input type="text" id="new-role-description" class="form-input" placeholder="What this role is for">
      </div>
      <button class="btn btn-primary" id="new-role-btn" type="button">Create Role</button>
    </div>
    <div id="new-role-error" class="login-error hidden" style="margin-top: 12px;"></div>
  `;
  container.appendChild(createPanel);

  const formPanel = document.createElement('div');
  formPanel.className = 'table-panel';
  formPanel.style.padding = '24px';
  formPanel.style.marginBottom = '24px';
  formPanel.innerHTML = `
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 16px;">Add or Update Access</h2>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grant-role">Role</label>
        <select id="grant-role" class="form-select" style="width: 170px;">
          ${roles.filter(r => !isAdminRoleName(r)).map(r => `<option value="${escapeHTMLText(r)}" ${r === rolesScreenSelectedRole ? 'selected' : ''}>${escapeHTMLText(r)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="grant-scope">Grant on</label>
        <select id="grant-scope" class="form-select" style="width: 170px;">
          <option value="module">A whole module</option>
          <option value="doctype">One record type</option>
        </select>
      </div>
      <div class="form-group" style="margin-bottom: 0;" id="grant-module-group">
        <label class="form-label" for="grant-module">Module</label>
        <select id="grant-module" class="form-select" style="width: 190px;">
          ${moduleOptions.map(m => `<option value="${escapeHTMLText(m)}">${escapeHTMLText(m)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group hidden" style="margin-bottom: 0;" id="grant-doctype-group">
        <label class="form-label" for="grant-doctype">Record Type</label>
        <select id="grant-doctype" class="form-select" style="width: 190px;">
          ${doctypeOptions.map(d => `<option value="${escapeHTMLText(d)}">${escapeHTMLText(getDoctypeLabel(d))}</option>`).join('')}
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
  document.getElementById('new-role-btn').addEventListener('click', createRole);
  const scopeSel = document.getElementById('grant-scope');
  scopeSel.addEventListener('change', () => {
    const whole = scopeSel.value === 'module';
    document.getElementById('grant-module-group').classList.toggle('hidden', !whole);
    document.getElementById('grant-doctype-group').classList.toggle('hidden', whole);
  });
}

// The role the Roles screen should have selected after a re-render - the
// one just created, so its first grant is one click away.
let rolesScreenSelectedRole = '';

function isAdminRoleName(name) {
  return ['superadmin', 'hr/admin', 'hradmin'].includes(String(name || '').toLowerCase().replace(/\s+/g, ''));
}

async function createRole() {
  const nameEl = document.getElementById('new-role-name');
  const errorEl = document.getElementById('new-role-error');
  errorEl.classList.add('hidden');
  const name = nameEl.value.trim();
  if (!name) { errorEl.textContent = 'Enter a role name.'; errorEl.classList.remove('hidden'); nameEl.focus(); return; }
  const res = await apiFetch('/api/v1/admin/roles', {
    method: 'POST',
    body: JSON.stringify({ name, description: document.getElementById('new-role-description').value.trim() })
  });
  if (!res) return;
  if (!res.ok) {
    errorEl.textContent = await getErrorMessage(res, 'Failed to create the role.');
    errorEl.classList.remove('hidden');
    return;
  }
  const data = await res.json().catch(() => ({}));
  rolesScreenSelectedRole = data.name || name;
  showToast(`Role "${rolesScreenSelectedRole}" created. Grant it a module below, then assign it to users.`, { variant: 'success' });
  renderView('roles');
}

async function saveRoleGrant() {
  const role = document.getElementById('grant-role').value;
  if (!role) { await showCustomAlert('Create a role first.', 'No role selected'); return; }
  const whole = document.getElementById('grant-scope').value === 'module';
  const body = {
    role,
    allow_read: document.getElementById('grant-read').checked,
    allow_create: document.getElementById('grant-create').checked,
    allow_update: document.getElementById('grant-update').checked,
    allow_delete: document.getElementById('grant-delete').checked
  };
  if (whole) {
    const mod = document.getElementById('grant-module').value;
    body.doctype_names = state.activeDoctypes.filter(d => d.module === mod).map(d => d.name);
    if (body.doctype_names.length === 0) { await showCustomAlert('That module has no record types.', 'Nothing to grant'); return; }
  } else {
    body.doctype_name = document.getElementById('grant-doctype').value;
  }
  const res = await apiFetch('/api/v1/admin/role-permissions', { method: 'POST', body: JSON.stringify(body) });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to save grant.');
    return;
  }
  rolesScreenSelectedRole = role;
  showToast(whole ? `${role} now has access to ${body.doctype_names.length} record types.` : `${role}'s access saved.`, { variant: 'success' });
  renderView('roles');
}

// ===========================================================================
// Home (BLD-033: task-oriented role navigation and onboarding)
//
// DEFAULT_VIEW's own comment has the short version. The longer one: the
// retired Dashboard (2026-08-01) failed because it was a second, generic
// front door into screens the sidebar already listed - "derived counts and
// shortcut tiles into Settings screens" was the user's own verdict. Home is
// deliberately NOT that:
//   - It links only to day-to-day TASK screens (POS, Purchase Orders,
//     Warehouse Cockpit, Finance...), never a Settings/admin/config screen -
//     an Administrator's sidebar already has every one of those, unchanged,
//     one click away; duplicating that list here is exactly the mistake
//     that got the old screen removed.
//   - Every tile is real, permission-derived navigation the visitor can
//     already reach some other way (the same MENU_PERMISSION_MAP/
//     MENU_MODULE_MAP rules the sidebar itself is gated by), never a static
//     list shown regardless of role - a permission-limited tenant simply
//     sees fewer tiles, never a disabled one.
//   - It surfaces STATE (approvals waiting, what you last touched, whether
//     the tenant has anything set up yet) rather than only being a link
//     farm - the thing a generic sidebar structurally cannot do.
//
// HOME_QUICK_ACTIONS is deliberately NOT keyed by role name: state.permissions
// (fetchAndApplyPermissions' own comment) already made that call for the
// exact same reason - a role-name branch can't see a tenant's custom role or
// a template edit, a doctype/module/capability check always can. Each entry
// reuses the SAME menuId its sidebar item is gated by, so Home can never
// show a tile the sidebar itself would hide, and a future MENU_PERMISSION_MAP/
// MENU_MODULE_MAP edit is picked up here for free.
const HOME_QUICK_ACTIONS = [
  { id: 'pos', label: 'Point of Sale', desc: 'Ring up sales, take payment, handle receipted returns.', view: 'pos', menuId: 'menu-pos' },
  { id: 'warehouse-cockpit', label: 'Warehouse Cockpit', desc: 'Open tasks, exceptions, waves and inbound for one location.', view: 'warehouse-cockpit', menuId: 'menu-warehouse-cockpit' },
  { id: 'purchase-orders', label: 'Purchase Orders', desc: 'Raise and track orders to vendors.', view: 'doctype-table', doctype: 'PurchaseOrder', menuId: 'menu-purchase-orders' },
  { id: 'purchase-requisitions', label: 'Purchase Requisitions', desc: 'Requests waiting to become a purchase order.', view: 'doctype-table', doctype: 'PurchaseRequisition', menuId: 'menu-purchase-requisitions' },
  { id: 'grn', label: 'Goods Receipt (GRN)', desc: 'Receive against open purchase orders.', view: 'doctype-table', doctype: 'GRN', menuId: 'menu-grn' },
  { id: 'vendor-invoices', label: 'Vendor Invoices', desc: 'Match and process what the company owes.', view: 'doctype-table', doctype: 'VendorInvoice', menuId: 'menu-vendor-invoices' },
  { id: 'payment-proposals', label: 'Payment Proposals', desc: 'Review and release vendor payments.', view: 'doctype-table', doctype: 'PaymentProposal', menuId: 'menu-payment-proposals' },
  { id: 'vendors', label: 'Vendors', desc: 'Vendor master records.', view: 'doctype-table', doctype: 'Vendor', menuId: 'menu-vendors' },
  { id: 'finance', label: 'Finance / GL', desc: 'Journals, period close, tax and the statements that come out of them.', view: 'finance', menuId: 'menu-finance' },
  { id: 'approvals', label: 'Approvals', desc: 'Documents waiting on your sign-off.', view: 'approvals', menuId: 'menu-approvals' },
  { id: 'sales-invoices', label: 'Sales Invoices', desc: 'What customers owe the company.', view: 'sales-invoices', menuId: 'menu-sales-invoices' },
  { id: 'journal-vouchers', label: 'Journal Vouchers', desc: 'Manual entries: accruals, corrections, opening balances.', view: 'journal-vouchers', menuId: 'menu-journal-vouchers' },
  { id: 'customers', label: 'Customers', desc: 'Customer master records.', view: 'doctype-table', doctype: 'Customer', menuId: 'menu-customers' },
  { id: 'oms', label: 'Order Management', desc: 'Track orders across every channel.', view: 'oms', menuId: 'menu-oms' },
  { id: 'fulfillment', label: 'Fulfillment', desc: 'Pick, pack and ship open orders.', view: 'fulfillment', menuId: 'menu-fulfillment' },
  { id: 'returns', label: 'Returns', desc: 'Process customer returns and exceptions.', view: 'returns', menuId: 'menu-returns' },
  { id: 'hr', label: 'HR & People', desc: 'Employee records, leave, attendance.', view: 'hr', menuId: 'menu-hr' }
];

// isHomeActionVisible reuses the exact predicates the sidebar itself is
// gated by (isMenuRuleVisible/canReadDoctype/isMenuModuleVisible) - no
// second definition of "can this session actually get there" to drift from
// the real one.
function isHomeActionVisible(action) {
  const permRule = MENU_PERMISSION_MAP[action.menuId];
  if (permRule && !isMenuRuleVisible(permRule)) return false;
  if (action.doctype && !canReadDoctype(action.doctype)) return false;
  const moduleEntry = MENU_MODULE_MAP[action.menuId];
  if (moduleEntry && !isMenuModuleVisible(moduleEntry.module)) return false;
  return true;
}

function navigateHomeAction(action) {
  setActiveMenu(action.menuId);
  closeSubmenus();
  if (action.view === 'doctype-table') {
    currentDoctype = action.doctype;
    currentSearchQuery = '';
    currentTablePage = 1;
  }
  renderView(action.view);
}

// navigateHomeRecent opens the doctype-table list for one "recent" record,
// pre-filtered to it by id/code - reuses the well-tested doctype-table view
// rather than driving the edit modal from a screen it wasn't written for.
function navigateHomeRecent(doctype, recordID) {
  setActiveMenu((HOME_QUICK_ACTIONS.find(a => a.doctype === doctype) || {}).menuId);
  closeSubmenus();
  currentDoctype = doctype;
  currentSearchQuery = recordID;
  currentTablePage = 1;
  renderView('doctype-table');
}

async function renderHomeView(container) {
  const visible = HOME_QUICK_ACTIONS.filter(isHomeActionVisible);

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Home</h1>
      <p class="page-subtitle">Your quick actions, what needs attention and what you last touched.</p>
    </div>
  `;
  container.appendChild(header);

  if (visible.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'table-panel';
    empty.style.padding = '24px';
    empty.innerHTML = `<p>No screens are set up for your role yet. Contact your administrator for access.</p>`;
    container.appendChild(empty);
    return;
  }

  // Today's work: approvals waiting on this session. ListPendingApprovals is
  // already scoped server-side to the caller's own role/location (see
  // renderApprovalsView's comment) - a safe, cheap call for anyone, since
  // Approvals (menu-approvals) is open to every role already.
  const statsRow = document.createElement('div');
  // BLD-037: plain .dashboard-stats-row (shared with Finance/exec-dashboard/
  // System Status/Tenant Usage, which all reliably render 3-4+ cards that
  // fill the row) stretches a LONE card to the full row width via its
  // auto-fit/1fr grid - Home only ever renders this one approvals-count
  // card, so it read as a half-empty panel. home-stats-row caps card width
  // here only, same scoped-override precedent as .oms-tile above.
  statsRow.className = 'dashboard-stats-row home-stats-row';
  container.appendChild(statsRow);
  if (isHomeActionVisible({ menuId: 'menu-approvals' })) {
    try {
      const res = await apiFetch('/api/v1/approval/pending');
      if (res && res.ok) {
        const pending = await res.json();
        const card = document.createElement('div');
        card.className = 'stat-card';
        card.style.cursor = 'pointer';
        card.innerHTML = `
          <div class="stat-info">
            <span class="stat-val">${pending.length}</span>
            <span class="stat-label">Approval${pending.length === 1 ? '' : 's'} waiting on you</span>
          </div>
        `;
        makeClickable(card, () => navigateHomeAction({ view: 'approvals', menuId: 'menu-approvals' }));
        statsRow.appendChild(card);
      }
    } catch (e) {
      // Best-effort, same as every other dashboard tile - a failed count
      // must never block the rest of Home from rendering.
    }
  }

  // Quick actions grid.
  const actionsPanel = document.createElement('div');
  actionsPanel.className = 'table-panel';
  actionsPanel.style.padding = '20px';
  actionsPanel.style.marginBottom = '24px';
  actionsPanel.innerHTML = `<h2 class="card-title" style="margin-bottom: 12px;">Your quick actions</h2>`;
  const grid = document.createElement('div');
  grid.className = 'home-action-grid';
  visible.forEach(action => {
    const tile = document.createElement('button');
    tile.type = 'button';
    tile.className = 'home-action-card';
    tile.innerHTML = `
      <span class="card-title">${escapeHTMLText(action.label)}</span>
      <span class="card-desc">${escapeHTMLText(action.desc)}</span>
    `;
    tile.addEventListener('click', () => navigateHomeAction(action));
    grid.appendChild(tile);
  });
  actionsPanel.appendChild(grid);
  container.appendChild(actionsPanel);

  // Scope-led setup: only the masters behind what THIS session can actually
  // reach - not a blanket "set up everything" nag. Reuses the exact same
  // setupHintHTML/isDoctypeSetUp Stage 41 already built for per-field hints.
  const missingMasters = [];
  visible.forEach(action => {
    if (!action.doctype) return;
    if (!state.setupStatus.byDoctype[action.doctype]) return; // not a Master, no hint to give
    if (!isDoctypeSetUp(action.doctype) && !missingMasters.includes(action.doctype)) {
      missingMasters.push(action.doctype);
    }
  });
  if (missingMasters.length > 0) {
    const setupPanel = document.createElement('div');
    setupPanel.className = 'table-panel setup-banner';
    setupPanel.style.marginBottom = '24px';
    setupPanel.innerHTML = `
      <div class="setup-banner-body">
        <strong>Get started</strong>
        <ul class="setup-banner-list">
          ${missingMasters.map(dt => `<li>${setupHintHTML(dt, 'missing')}</li>`).join('')}
        </ul>
      </div>
    `;
    container.appendChild(setupPanel);
  }

  // Recent: the first visible task with a doctype of its own - "what did I
  // touch last" for the thing this session actually does day to day.
  const recentSource = visible.find(a => a.doctype);
  if (recentSource) {
    const res = await apiFetch(`/api/v1/doc/${recentSource.doctype}?sort=recent&limit=5`).catch(() => null);
    if (res && res.ok) {
      const rows = await res.json();
      if (rows.length > 0) {
        const recentPanel = document.createElement('div');
        recentPanel.className = 'table-panel';
        recentPanel.style.padding = '20px';
        recentPanel.innerHTML = `<h2 class="card-title" style="margin-bottom: 12px;">Recent ${escapeHTMLText(getDoctypeLabel(recentSource.doctype))}</h2>`;
        const list = document.createElement('div');
        list.className = 'home-recent-list';
        rows.forEach(row => {
          const item = document.createElement('button');
          item.type = 'button';
          item.className = 'home-recent-item';
          const label = row.code || row.name || row.id;
          item.innerHTML = `<span>${escapeHTMLText(String(label))}</span><span class="card-desc">${escapeHTMLText(String(row.status || ''))}</span>`;
          item.addEventListener('click', () => navigateHomeRecent(recentSource.doctype, row.id));
          list.appendChild(item);
        });
        recentPanel.appendChild(list);
        container.appendChild(recentPanel);
      }
    }
  }
}

// POS / Billing screen - cashier/barcode-scan-to-sell UI against the
// already-working checkout/availability APIs (Stage 13.4). Kept independent
// of the generic DocType table view since a checkout cart isn't a plain
// CRUD record: it's built up client-side line by line before a single
// POST /api/v1/checkout submits the whole thing atomically.
/* BLD-041: view-pos.js is loaded with native import() on first view visit. */
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
  // Stage 57.17: the server refuses everything but setting a new password
  // while the account is on one an administrator issued.
  if (response.status === 403 && response.headers.get('X-Password-Change-Required') === '1') {
    showPasswordChangeScreen();
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

function batchCellHTML(line) {
  if (!line || !line.batch_no) return '<span class="text-muted">&mdash;</span>';
  const safeBatch = escapeHTMLText(line.batch_no);
  if (!line.expiry_date) return `<strong>${safeBatch}</strong>`;
  const expiry = String(line.expiry_date).slice(0, 10);
  const days = Math.floor((new Date(expiry + 'T00:00:00Z') - new Date(new Date().toISOString().slice(0, 10) + 'T00:00:00Z')) / 86400000);
  let badge = '';
  if (!isNaN(days)) {
    if (days < 0) badge = `<span class="badge badge-danger">expired</span>`;
    else if (days <= 7) badge = `<span class="badge badge-danger">${days}d left</span>`;
    else if (days <= 30) badge = `<span class="badge badge-warning">${days}d left</span>`;
  }
  return `<strong>${safeBatch}</strong><br><small class="text-muted">${escapeHTMLText(expiry)}</small> ${badge}`;
}


function formatMoney(v) {
  const n = Number(v);
  if (!isFinite(n)) return '0.00';
  return n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}


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
 * @param jobType 'Shipping Label' | 'Sticker' | 'Receipt' | 'Invoice' | 'Purchase Order' | 'Document'
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
      doc_format: opts.docFormat || '',
      // Stage 52: bulk print from a GRN/Transfer Order - source_doctype +
      // source_doc_id tell the server to resolve the document's own lines
      // (engines.PrintStickersForDocument) instead of requiring skus.
      source_doctype: opts.sourceDoctype || '',
      source_doc_id: opts.sourceDocId || '',
      copies_override: opts.copiesOverride || undefined,
      // Stage 52.8: per-line selection (sku + batch_no + copies). A SKU
      // received on two lots is two lines, which skus/copies_override
      // cannot tell apart; the server accepts either shape.
      lines: opts.lines || undefined
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
function renderPrintSheet(labels, copies) {
  const area = document.getElementById('sticker-print-area');
  let html = '';
  labels.forEach((label, idx) => {
    const labelCopies = label.qty > 0 ? label.qty : copies;
    const nextCategory = idx < labels.length - 1 ? (labels[idx + 1].category || '') : null;
    const isLastOfCategoryGroup = nextCategory === null || nextCategory !== (label.category || '');
    let elements = [];
    if (label.template_elements) {
      try { elements = JSON.parse(label.template_elements); } catch (e) { elements = []; }
    }
    for (let i = 0; i < labelCopies; i++) {
      const breakClass = (i === labelCopies - 1 && isLastOfCategoryGroup) ? ' sticker-label-group-end' : '';
      if (elements.length > 0 && label.label_width_mm && label.label_height_mm) {
        html += `
          <div class="sticker-label sticker-label-templated${breakClass}" style="width:${label.label_width_mm}mm; height:${label.label_height_mm}mm;">
            ${elements.map(el => renderStickerElementHTML(el, label)).join('')}
          </div>
        `;
      } else {
        html += `
          <div class="sticker-label${breakClass}">
            <div class="sticker-name">${escapeHTMLText(label.name || label.sku)}</div>
            <div class="sticker-barcode">${label.barcode_svg || escapeHTMLText(label.barcode || '')}</div>
            <div class="sticker-meta">SKU: ${escapeHTMLText(label.sku)}${label.hsn_code ? ' &nbsp;|&nbsp; HSN: ' + escapeHTMLText(label.hsn_code) : ''}</div>
          </div>
        `;
      }
    }
  });
  area.innerHTML = html;
  area.classList.add('printing');
  window.print();
  setTimeout(() => area.classList.remove('printing'), 500);
}

// Stage 58: the sticker template designer is public/sticker-studio.js (its
// state lives there); stickerFieldText/renderStickerElementHTML used by
// renderPrintSheet above come from view-printing.js.


// BLD-041: asset, transfer, expense, manufacturing, HR and printing screens
// are native modules registered in LAZY_VIEW_MODULES above.

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
  // Stage 36.6.3: catalog-wide media browse/search/tag/bulk. No doctype -
  // same bespoke-screen treatment as Dashboard/Workbench/Reports/My Work,
  // since it needs thumbnails, filters and a bulk zip action the generic
  // doctype table doesn't have.
  { id: 'media-library', label: 'Media Library' },
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
  { id: 'supplier-submissions', label: 'Supplier Submissions', doctype: 'SupplierSubmission' },
  // Stage 36.3 import depth. Also closes that stage's own stated gap (no
  // frontend surface yet) - the generic doctype table already gives
  // create/list/edit/RBAC/audit for free, so a tab is the whole fix.
  { id: 'import-templates', label: 'Import Templates', doctype: 'PIMImportTemplate' },
  { id: 'import-schedules', label: 'Import Schedules', doctype: 'PIMImportSchedule' },
  // Stage 36.4 export & syndication depth. "Run" (export templates) and
  // "Share Link" (catalogs) are row actions added in the generic table's
  // action-button wiring below, PIM_DOCTYPE_ROW_ACTIONS - the templates and
  // schedules besides them need no bespoke screen either.
  { id: 'export-templates', label: 'Export Templates', doctype: 'PIMExportTemplate' },
  { id: 'export-schedules', label: 'Export Schedules', doctype: 'PIMExportSchedule' },
  { id: 'catalogs', label: 'Catalogs', doctype: 'PIMCatalog' }
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
function docTableListParams(page, search) {
  const offset = (page - 1) * itemsPerPage;
  // count=true (BLD-034) is opt-in on the server - it runs a second real
  // COUNT(*) query, so only this screen's own pagination footer asks for
  // it; the endpoint's dozen-plus other callers (Link-field typeaheads
  // chief among them, firing on every keystroke) don't pay for a number
  // they never read.
  const params = new URLSearchParams({ limit: String(itemsPerPage), offset: String(offset), count: 'true' });
  if (search) params.set('q', search);
  return params;
}

async function fetchDocTablePage(doctype, page, search) {
  const params = docTableListParams(page, search);
  const dataRes = await apiFetch(`/api/v1/doc/${doctype}?${params.toString()}`);
  if (!dataRes || !dataRes.ok) return undefined;
  const records = await dataRes.json();
  // X-Total-Count (BLD-034) is additive - an intermediary or test double
  // that doesn't forward it still renders correctly, just unable to say
  // whether more rows exist past this page.
  const totalHeader = dataRes.headers.get('X-Total-Count');
  const offset = (page - 1) * itemsPerPage;
  const total = totalHeader !== null ? (parseInt(totalHeader, 10) || 0) : offset + records.length;
  return { records, total };
}

async function fetchDocTableData(doctype, page, search) {
  const metaRes = await apiFetch(`/api/v1/doc/${doctype}/meta`);
  if (!metaRes || !metaRes.ok) return undefined;
  const pageData = await fetchDocTablePage(doctype, page, search);
  if (!pageData) return undefined;
  return { fields: await metaRes.json(), records: pageData.records, total: pageData.total };
}

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

// cfgEsc is a historical alias for escapeHTMLText. It used to be a second,
// byte-for-byte identical copy of the same five replacements - two escaping
// helpers for one job, which is exactly the parallel-implementation problem
// the 47.8.1 sink review exists to find: a change to the escape rules (adding
// backticks, say) would have had to be made twice, and missing one would
// leave 55 call sites across five view modules quietly on the old behaviour.
//
// Delegating rather than renaming all 55 call sites: one implementation is
// the whole point, and this achieves it with one edit instead of 55 in a tree
// other sessions are editing. Renaming the remaining callers is a separate,
// purely mechanical follow-up.
function cfgEsc(s) {
  return escapeHTMLText(s);
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
      <button class="btn btn-secondary" ${actionAttrs('goToDefaultView')}>Back to Reports</button>
    </div>
  `;
  container.appendChild(panel);
}

window.openImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.inert = false;
    modal.classList.add('open');
    document.getElementById('import-result-summary').style.display = 'none';
    // Stage 51.10: the SKU-from-Design template only exists for Item.
    const familyBtn = document.getElementById('import-family-template-btn');
    if (familyBtn) familyBtn.style.display = currentDoctype === 'Item' ? '' : 'none';
  }
};

window.closeImportModal = function() {
  const modal = document.getElementById('import-modal');
  if (modal) {
    modal.classList.remove('open');
    modal.inert = true;
    document.getElementById('import-modal-form').reset();
  }
};

// The template endpoint is behind apiMiddleware, so it needs the bearer
// token. A plain anchor href sends no Authorization header, and the browser
// answers the resulting 401 with its own credential prompt - hence fetch it
// through apiFetch and hand the browser a blob it can save locally, the same
// object-URL approach the recovery-code and report exports already take.
window.downloadImportTemplate = async function(variant) {
  const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
  const variantParam = typeof variant === 'string' && variant ? `&variant=${encodeURIComponent(variant)}` : '';
  const url = `/api/v1/import/${currentDoctype}/template?tenant_id=${tenantID}${variantParam}`;

  let blob;
  try {
    const response = await apiFetch(url);
    if (!response.ok) {
      await showApiError(response, 'Could not download the import template.');
      return;
    }
    blob = await response.blob();
  } catch (err) {
    showToast('Could not download the import template. Check your connection and try again.',
      { variant: 'error', title: 'Download failed' });
    return;
  }

  const objectURL = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = objectURL;
  link.download = variantParam ? `${currentDoctype}_${variant}_template.csv` : `${currentDoctype}_template.csv`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(objectURL);
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
        html += `<li>Row ${escapeHTMLText(err.row_number)}: ${escapeHTMLText(err.message)}</li>`;
      });
      html += `</ul>`;
      if (result.import_job_id) {
        const tenantID = localStorage.getItem('erp_tenant_id') || 'default';
        html += `<div style="margin-top:8px;"><a href="/api/v1/pim/import-jobs/${encodeURIComponent(result.import_job_id)}/errors.csv?tenant_id=${encodeURIComponent(tenantID)}" target="_blank" rel="noopener">Download error rows (CSV)</a></div>`;
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
    summary.innerHTML = `<strong>Preview Failed:</strong> ${escapeHTMLText(data.error || 'Server returned an error processing the CSV request.')}`;
    return;
  }

  const result = await res.json();
  summary.style.backgroundColor = 'rgba(255, 165, 2, 0.1)';
  summary.style.border = '1px solid rgba(255, 165, 2, 0.3)';
  summary.style.color = '#ffa502';
  let html = `
    <div style="font-weight:600; margin-bottom:8px;">Preview (nothing written yet):</div>
    <div>Total Rows: ${escapeHTMLText(result.total_rows)}</div>
    <div>Would Create: ${(result.created_ids || []).length}</div>
    <div>Would Update: ${(result.updated_ids || []).length}</div>
    <div>Would Reject: ${escapeHTMLText(result.failed_rows)}</div>
  `;
  if (result.errors && result.errors.length > 0) {
    html += `<div style="font-weight:600; margin-top:12px;">Row Errors:</div><ul style="padding-left:16px; margin-top:4px;">`;
    result.errors.forEach(err => { html += `<li>Row ${escapeHTMLText(err.row_number)}: ${escapeHTMLText(err.message)}</li>`; });
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

async function openHelpDrawer(screenID) {
  let helpModule;
  try {
    helpModule = await loadViewModule('/view-help.js?v=1');
  } catch (error) {
    console.error('[BLD-041] Help module failed to load', error);
    await showCustomAlert('The Knowledge Center could not be loaded. Check your connection and try again.', 'Help');
    return;
  }
  return helpModule.openHelpDrawer(screenID);
}

window.openHelpDrawer = openHelpDrawer;
window.addEventListener('DOMContentLoaded', bootstrap);
