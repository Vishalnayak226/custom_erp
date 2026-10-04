// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
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
      <p class="page-subtitle">Number series for every transaction document, and for master records like Vendor and Item. Purchase orders, goods receipts, transfers, claims, vendor codes, item codes and the rest draw their number from here when they are saved - nobody types one in.</p>
    </div>
    <button class="btn btn-primary" ${actionAttrs('addPrefixConfig')}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="margin-right: 6px;"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
      <span>New Series</span>
    </button>
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
        <td><button class="btn btn-outline btn-sm" ${actionAttrs('editPrefixConfig', [c.doc_type])}>Edit</button></td>
      </tr>
    `;
  });
  html += `</tbody></table>`;
  panel.innerHTML = html;
  container.appendChild(panel);
}

// addPrefixConfig (Stage 51.4): the record types with no row here yet
// (Vendor, Item, or any future one) were still being auto-numbered - the
// engine falls back to sane defaults when no row exists, see
// engines/numbering.go's GenerateSequence - just not editably, since this
// screen could only edit a row that already existed. Reuses the exact same
// upsert endpoint and sequential-prompt flow as editPrefixConfig, with one
// extra prompt up front for the record type itself.
window.addPrefixConfig = async function() {
  const docType = await showCustomPrompt('Record Type (must match the doctype name exactly, e.g. Vendor, Item):', '');
  if (!docType) return;
  if (state.prefixConfigs.some(x => x.doc_type === docType.trim())) {
    await showCustomAlert(`"${docType}" already has a series - use its Edit button instead.`);
    return;
  }
  await editPrefixConfig(docType.trim(), { prefix: docType.trim(), separator: '/', padding_width: 6, reset_frequency: 'ANNUAL', include_store: true });
};

window.editPrefixConfig = async function(docType, newDefaults) {
  const c = state.prefixConfigs.find(x => x.doc_type === docType) || newDefaults;
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
    <button class="btn btn-primary" ${actionAttrs('openApprovalRuleModal')}>
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
                <button class="action-btn" title="Edit" aria-label="Edit the ${escapeHTMLText(r.doctype)} approval rule" style="margin-right:4px;" ${actionAttrs('openApprovalRuleModal', [r.id])}>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                </button>
                <button class="action-btn action-btn-danger" title="Delete" aria-label="Delete the ${escapeHTMLText(r.doctype)} approval rule" ${actionAttrs('deleteApprovalRuleRow', [r.id])}>
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
    <button class="btn btn-primary" ${actionAttrs('addNewLabelReplacement')}>
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
          <button class="action-btn action-btn-danger" ${actionAttrs('deleteLabelReplacement', [orig])}>Remove</button>
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
                <button class="action-btn" ${actionAttrs('viewExtensionHookLog', [h.id])}>View Log</button>
                <button class="action-btn action-btn-danger" ${actionAttrs('deleteExtensionHookRow', [h.id])}>Delete</button>
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
    <button class="btn btn-outline" ${actionAttrs('renderView', ['extension-hooks'])}>Back to Extension Hooks</button>
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

  // Stage 38.6: the async job runner's visibility screen, same pane pattern.
  const jobsRes = await apiFetch('/api/v1/jobs');
  let jobsLoadFailed = !!jobsRes && !jobsRes.ok;
  let jobs = jobsRes && jobsRes.ok ? ((await jobsRes.json()).jobs || []) : [];

  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Activity Log</h1>
      <p class="page-subtitle">Centralized System Audit trail, Middleware Panic recovery trace log console, and Integration payload viewer.</p>
    </div>
    <button class="btn btn-outline" id="log-hub-panic-test">
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
    <button class="log-hub-tab" data-tab="jobs" style="padding:10px 20px; border:none; background:transparent; cursor:pointer; font-weight:500; color:var(--text-muted);">Async Jobs</button>
  `;
  container.appendChild(tabBar);

  // Tab content container
  const tabContent = document.createElement('div');
  tabContent.id = 'log-hub-tab-content';
  container.appendChild(tabContent);
  header.querySelector('#log-hub-panic-test').addEventListener('click', triggerPanicRecovery);
  tabContent.addEventListener('click', (event) => {
    const retry = event.target.closest('[data-integration-retry-id]');
    if (retry) {
      retryIntegrationEvent(retry.dataset.integrationRetryId);
      return;
    }
    const logRow = event.target.closest('[data-log-id]');
    if (logRow && window.viewStackTrace) window.viewStackTrace(logRow.dataset.logId);
  });
  tabContent.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    const logRow = event.target.closest('[data-log-id]');
    if (logRow && window.viewStackTrace) {
      event.preventDefault();
      window.viewStackTrace(logRow.dataset.logId);
    }
  });

  function renderAuditPane() {
    tabContent.innerHTML = `
      <div class="table-panel">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding: 16px 16px 0;">Audit Logs</h3>
        ${auditLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load audit logs. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['audit-logs'])}>Try Again</button></p>` : ''}
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
                  <td>${escapeHTMLText(l.user_id)}</td>
                  <td>${escapeHTMLText(l.action)}</td>
                  <td style="font-size:12px;">${escapeHTMLText(l.details)}</td>
                  <td style="font-size:11px; white-space:nowrap;">${escapeHTMLText(l.created_at)}</td>
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
        ${sysLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load system logs. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['audit-logs'])}>Try Again</button></p>` : ''}
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
                <tr style="cursor:pointer;" data-log-id="${escapeHTMLText(l.log_id)}" tabindex="0" role="button">
                  <td><span class="badge badge-secondary">${escapeHTMLText(l.severity)}</span></td>
                  <td>${escapeHTMLText(l.module_source)}</td>
                  <td style="font-size:12px; color:var(--text-muted);">${escapeHTMLText(l.error_message)}</td>
                  <td style="font-size:11px; white-space:nowrap;">${escapeHTMLText(l.created_at)}</td>
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
        ${intLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load integration logs. <button class="btn btn-outline btn-sm" ${actionAttrs('renderView', ['audit-logs'])}>Try Again</button></p>` : ''}
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
                  <td style="font-weight:600;">${escapeHTMLText(l.event_name)}</td>
                  <td><span class="badge ${l.status === 'Dispatched' || l.status === 'Success' ? 'badge-success' : l.status === 'Failed' ? 'badge-danger' : 'badge-secondary'}">${escapeHTMLText(l.status)}</span></td>
                  <td>${escapeHTMLText(l.attempts)}</td>
                  <td style="font-size:11px; max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" title="${escapeHTMLText(JSON.stringify(l.payload || {}))}">${escapeHTMLText(JSON.stringify(l.payload || {}))}</td>
                  <td style="font-size:11px; white-space:nowrap;">${escapeHTMLText(l.created_at)}</td>
                  <td>
                    ${l.status === 'Failed' ? `<button class="btn btn-sm btn-outline" data-integration-retry-id="${escapeHTMLText(l.id)}">Retry</button>` : '<span style="color:var(--text-muted); font-size:12px;">-</span>'}
                  </td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  function jobStatusBadgeClass(status) {
    if (status === 'Succeeded') return 'badge-success';
    if (status === 'DeadLettered' || status === 'Failed') return 'badge-danger';
    if (status === 'Cancelled') return 'badge-secondary';
    if (status === 'Quarantined') return 'badge-warning';
    return 'badge-secondary'; // Pending, Leased
  }

  async function reloadJobs() {
    const res = await apiFetch('/api/v1/jobs');
    // BLD-036: jobsLoadFailed must be reassigned here (see its `let` above),
    // not just `jobs` - otherwise a successful retry still rendered the
    // stale "Failed to load" message because the pane re-render only ever
    // reads the closure variable, never recomputes it.
    jobsLoadFailed = !!res && !res.ok;
    jobs = res && res.ok ? ((await res.json()).jobs || []) : [];
    renderJobsPane();
  }

  function renderJobsPane() {
    tabContent.innerHTML = `
      <div class="table-panel">
        <h3 style="font-size:16px; font-weight:600; margin-bottom:12px; padding: 16px 16px 0;">Async Jobs</h3>
        <p style="padding: 0 16px 12px; color: var(--text-muted); font-size: 13px;">Every background job queued through the Stage 38.6 job runner (webhook deliveries and future job types) - retry a Failed/DeadLettered job or cancel a Pending one.</p>
        ${jobsLoadFailed ? `<p style="padding: 0 16px 12px; color: var(--danger-color); font-size: 13px;">Failed to load async jobs. <button class="btn btn-outline btn-sm" id="log-hub-jobs-retry-btn">Try Again</button></p>` : ''}
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Type</th>
                <th>Status</th>
                <th>Attempts</th>
                <th>Progress</th>
                <th>Last Error</th>
                <th>Updated</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              ${jobs.length === 0 ? '<tr><td colspan="7" style="text-align:center; color:var(--text-muted);">No async jobs found for the current filter.</td></tr>' : jobs.map(j => `
                <tr>
                  <td style="font-weight:600;">${escapeHTMLText(j.job_type)}</td>
                  <td><span class="badge ${jobStatusBadgeClass(j.status)}">${escapeHTMLText(j.status)}</span></td>
                  <td>${j.attempts}/${j.max_attempts}</td>
                  <td>${j.progress_pct}%</td>
                  <td style="font-size:11px; max-width:220px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" title="${escapeHTMLText(j.last_error || '')}">${escapeHTMLText(j.last_error || '-')}</td>
                  <td style="font-size:11px; white-space:nowrap;">${j.updated_at}</td>
                  <td>
                    ${(j.status === 'Failed' || j.status === 'DeadLettered' || j.status === 'Cancelled') ? `<button class="btn btn-sm btn-outline" data-job-action="retry" data-job-id="${escapeHTMLText(j.id)}">Retry</button>` : ''}
                    ${(j.status === 'Quarantined') ? `<button class="btn btn-sm btn-outline" data-job-action="retry" data-job-id="${escapeHTMLText(j.id)}">Replay</button>` : ''}
                    ${(j.status === 'Pending' || j.status === 'Leased') ? `<button class="btn btn-sm btn-outline" data-job-action="cancel" data-job-id="${escapeHTMLText(j.id)}">Cancel</button>` : ''}
                    ${(j.status === 'Succeeded') ? '<span style="color:var(--text-muted); font-size:12px;">-</span>' : ''}
                  </td>
                </tr>
              `).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
    const retryBtn = document.getElementById('log-hub-jobs-retry-btn');
    if (retryBtn) retryBtn.addEventListener('click', reloadJobs);
    tabContent.querySelectorAll('[data-job-action]').forEach(button => {
      button.addEventListener('click', () => {
        if (button.dataset.jobAction === 'retry') retryAsyncJob(button.dataset.jobId);
        else if (button.dataset.jobAction === 'cancel') cancelAsyncJob(button.dataset.jobId);
      });
    });
  }

  window.retryAsyncJob = async function(jobId) {
    if (!await showCustomConfirm('Retry this job?')) return;
    const res = await apiFetch(`/api/v1/jobs/${encodeURIComponent(jobId)}/retry`, { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to retry this job.'); return; }
    showToast('Job queued for retry.');
    reloadJobs();
  };

  window.cancelAsyncJob = async function(jobId) {
    if (!await showCustomConfirm('Cancel this job?')) return;
    const res = await apiFetch(`/api/v1/jobs/${encodeURIComponent(jobId)}/cancel`, { method: 'POST' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to cancel this job.'); return; }
    showToast('Job cancelled.');
    reloadJobs();
  };

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
      else if (tab === 'jobs') renderJobsPane();
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
  if (!res.ok) {
    // BLD-036: the header above already rendered, so this stays an appended
    // partial-section message (same shape as renderSystemStatusView's own
    // fix for this) rather than a full renderErrorPanel takeover that would
    // wipe it - and unlike the previous showApiError-only modal, it leaves a
    // permanent way back instead of navigate-away-and-back being the only
    // recovery once the modal is dismissed.
    const msg = await getErrorMessage(res, 'Failed to load configuration.');
    const err = document.createElement('p');
    err.style.cssText = 'color:var(--danger-color); font-size:13px; margin-bottom:16px;';
    err.textContent = msg + ' ';
    const retryBtn = document.createElement('button');
    retryBtn.className = 'btn btn-outline btn-sm';
    retryBtn.textContent = 'Try Again';
    retryBtn.addEventListener('click', () => renderView('configuration'));
    err.appendChild(retryBtn);
    container.appendChild(err);
    return;
  }
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

  // BLD-036: used to leave just the bare title with no explanation or retry
  // if the connection itself failed (apiFetch already showed its own dialog,
  // but dismissing it landed on a permanently empty page).
  if (!deployRes || !backupRes) {
    renderErrorPanel(container, 'Could not reach the server to load system status.', () => renderView('system-status'));
    return;
  }

  const deployFailed = !deployRes.ok;
  const backupFailed = !backupRes.ok;
  const deployData = deployFailed ? { latest_by_environment: {}, history: [] } : await deployRes.json();
  const backupData = backupFailed ? { warnings: [], history: [] } : await backupRes.json();

  if (deployFailed || backupFailed) {
    // BLD-036: the rest of the dashboard still renders (with empty history
    // for whichever half failed), so this stays a partial-section message
    // rather than a full renderErrorPanel takeover - but it used to have no
    // way back short of navigating away and back.
    const err = document.createElement('p');
    err.style.cssText = 'color:var(--danger-color); font-size:13px; margin-bottom:16px;';
    err.textContent = deployFailed && backupFailed
      ? 'Failed to load deployment and backup status.'
      : deployFailed ? 'Failed to load deployment status.' : 'Failed to load backup status.';
    const retryBtn = document.createElement('button');
    retryBtn.className = 'btn btn-outline btn-sm';
    retryBtn.style.marginLeft = '8px';
    retryBtn.textContent = 'Try Again';
    retryBtn.addEventListener('click', () => renderView('system-status'));
    err.appendChild(retryBtn);
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
          ${packages.map(p => `<button class="btn btn-outline btn-sm" title="${(p.modules || []).join(', ')}" ${actionAttrs('applyTenantPackage', [tenantId, p.package_key])}>${p.display_name}</button>`).join('')}
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
                      : `<label class="switch"><input type="checkbox" ${m.enabled ? 'checked' : ''} ${actionAttrs('toggleTenantModule', [tenantId, m.module_key, ACTION_ARG_CHECKED], { on: 'change' })}><span class="slider"></span></label>`
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

  async function loadTenant(tenantId, liveBody = null) {
    const targetBody = liveBody?.isConnected ? liveBody : bodyContainer;
    targetBody.innerHTML = '';
    if (!tenantId) return;
    const res = await apiFetch(`/api/v1/admin/tenant/module-entitlements?tenant_id=${encodeURIComponent(tenantId)}`);
    if (!res) return;
    if (!res.ok) {
      renderErrorPanel(targetBody, 'Failed to load this tenant\'s module entitlements.', retryBody => loadTenant(tenantId, retryBody));
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


export { renderPrefixConfigsView, renderApprovalRulesView, renderDynamicLabelsView, renderExtensionHooksView, renderExtensionHookLogView, showOneTimeSecretDialog, renderLogHubView, renderConfigurationView, renderConfigModuleRail, configInputId, renderConfigIntegrations, configIntegrationSectionHtml, wireConfigIntegrationSection, renderConfigFields, configFieldHtml, updateConfigDirtyState, saveConfiguration, renderSystemStatusView, renderTenantEntitlementsView, renderTenantUsageView };
