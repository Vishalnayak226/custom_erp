// Stage 65.5 (the user, 2026-10-16: "a page where I can see all clients and
// what access I gave, release and all other control, where hosted and
// everything"): the platform operator's Control Panel. Read-only - it gathers
// GET /api/v1/platform/control-panel; changes stay on the screens that own
// them (Tenant Entitlements for modules, the Data Console for records), which
// each row opens with that client already chosen.
//
// BLD-041: native ES module, loaded on first authorized view visit; shared app
// services (apiFetch, escapeHTMLText, renderView...) resolve from the shell.

const when = (v) => {
  if (!v) return '<span class="text-muted">&mdash;</span>';
  const d = new Date(v);
  return isNaN(d) ? escapeHTMLText(String(v)) : escapeHTMLText(d.toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }));
};
const day = (v) => {
  if (!v) return '<span class="text-muted">&mdash;</span>';
  const d = new Date(v);
  return isNaN(d) ? escapeHTMLText(String(v)) : escapeHTMLText(d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }));
};
const size = (b) => (b > 1048576 ? `${(b / 1048576).toFixed(1)} MB` : b > 1024 ? `${Math.round(b / 1024)} KB` : `${b || 0} B`);

function statusBadge(c) {
  const s = (c.status || 'active').toLowerCase();
  const cls = s === 'active' ? 'badge-success' : s === 'suspended' ? 'badge-warning' : 'badge-danger';
  return `<span class="badge ${cls}">${escapeHTMLText(c.status || 'active')}</span>`
    + (c.is_sandbox ? ` <span class="badge badge-secondary" title="${c.sandbox_expires_at ? 'Expires ' + escapeHTMLText(c.sandbox_expires_at) : ''}">sandbox</span>` : '')
    + (c.legal_hold ? ' <span class="badge badge-danger">legal hold</span>' : '');
}

async function renderControlPanelView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Control Panel</h1>
      <p class="page-subtitle">Every client in one place: status, address, modules, release, users and activity. Platform operators only.</p>
    </div>
    <button class="btn btn-outline" id="cp-refresh" type="button">Refresh</button>
  `;
  container.appendChild(header);
  document.getElementById('cp-refresh').addEventListener('click', () => renderView('control-panel'));

  const res = await apiFetch('/api/v1/platform/control-panel');
  if (!res) return;
  if (res.status === 403) {
    const p = document.createElement('div');
    p.className = 'table-panel';
    p.innerHTML = `<p style="padding:16px;">Only a <b>platform operator</b> can open the Control Panel. The server setting <code>ERP_PLATFORM_OPERATORS</code> lists who that is (Admin Guide &sect;C.7).</p>`;
    container.appendChild(p);
    return;
  }
  if (!res.ok) {
    renderErrorPanel(container, 'Failed to load the control panel.', () => renderView('control-panel'));
    return;
  }
  const cp = await res.json();
  const clients = cp.clients || [];
  const live = clients.filter(c => !c.is_sandbox);
  const b = cp.build || {};
  const bk = cp.last_backup;
  const backupOK = bk && /success/i.test(bk.outcome || '');

  const stats = document.createElement('div');
  stats.className = 'dashboard-stats-row';
  stats.innerHTML = `
    <div class="stat-card">
      <span class="stat-label">Running release</span>
      <span class="stat-val">${escapeHTMLText(b.version || '—')}</span>
      <div class="text-muted" style="font-size:12px; margin-top:4px;">${escapeHTMLText(b.git_commit || '')} &middot; ${b.release_date ? day(b.release_date) : 'dev build'} &middot; ${escapeHTMLText(cp.environment || '')}</div>
    </div>
    <div class="stat-card">
      <span class="stat-label">Clients</span>
      <span class="stat-val">${live.length}</span>
      <div class="text-muted" style="font-size:12px; margin-top:4px;">${clients.length - live.length} sandbox &middot; ${clients.filter(c => c.problem).length} with a problem</div>
    </div>
    <div class="stat-card">
      <span class="stat-label">Hosted at</span>
      <span class="stat-val" style="font-size:18px;">${escapeHTMLText(cp.base_domain || 'one address')}</span>
      <div class="text-muted" style="font-size:12px; margin-top:4px;">${cp.base_domain ? 'one address per client' : 'TENANT_BASE_DOMAIN unset'}</div>
    </div>
    <div class="stat-card">
      <span class="stat-label">Last backup</span>
      <span class="stat-val" style="font-size:18px; color:${bk ? (backupOK ? 'var(--success-color)' : 'var(--danger-color)') : 'var(--text-muted)'};">${bk ? escapeHTMLText(bk.outcome || '—') : 'none recorded'}</span>
      <div class="text-muted" style="font-size:12px; margin-top:4px;">${bk ? `${when(bk.finished_at)} &middot; ${size(bk.size_bytes)}` : ''}${!backupOK && cp.last_good_backup_at ? ` &middot; last good ${when(cp.last_good_backup_at)}` : ''}</div>
    </div>
  `;
  container.appendChild(stats);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; padding:16px 16px 0;">Clients</h3>
    <div class="table-wrapper">
      <table>
        <thead><tr>
          <th>Client</th><th>Status</th><th>Address</th><th>Created</th><th>Version</th>
          <th>Modules</th><th class="num">Users</th><th class="num">Records</th><th>Last activity</th><th class="num">Errors (24h)</th><th>Actions</th>
        </tr></thead>
        <tbody>
          ${clients.length === 0 ? '<tr><td colspan="11" style="text-align:center; color:var(--text-muted);">No clients yet. Create one under Tenant Entitlements &rarr; Provision New Tenant.</td></tr>' : ''}
          ${clients.map((c, i) => `
            <tr>
              <td><strong>${escapeHTMLText(c.name || c.tenant_id)}</strong><div class="text-muted" style="font-size:12px;">${escapeHTMLText(c.tenant_id)} &middot; ${escapeHTMLText(c.schema)}</div>${c.problem ? `<div class="po-line-error">${escapeHTMLText(c.problem)}</div>` : ''}</td>
              <td>${statusBadge(c)}${c.status_reason ? `<div class="text-muted" style="font-size:12px;">${escapeHTMLText(c.status_reason)}</div>` : ''}${c.admin_signed_in ? '' : '<div class="text-muted" style="font-size:12px;">admin not signed in yet</div>'}</td>
              <td>${c.url ? `<a href="${escapeHTMLText(c.url)}" target="_blank" rel="noopener" style="color:inherit; text-decoration:underline;">${escapeHTMLText(c.url.replace(/^https:\/\//, '').replace(/\/$/, ''))}</a>` : '<span class="text-muted">main address</span>'}</td>
              <td>${day(c.provisioned_at || c.created_at)}</td>
              <td>${c.pinned_version ? `pinned ${escapeHTMLText(c.pinned_version)}` : c.app_version ? escapeHTMLText(c.app_version) : '<span class="text-muted">follows release</span>'}</td>
              <td><button type="button" class="btn btn-ghost btn-sm" data-cp-modules="${i}" title="${escapeHTMLText((c.modules_enabled || []).join(', '))}">${(c.modules_enabled || []).length} on</button>${(c.modules_disabled || []).length ? ` <span class="text-muted">/ ${(c.modules_disabled || []).length} off</span>` : ''}</td>
              <td class="num">${c.active_users}</td>
              <td class="num">${Number(c.records || 0).toLocaleString('en-IN')}</td>
              <td>${when(c.last_activity_at)}</td>
              <td class="num">${c.errors_24h ? `<span style="color:var(--danger-color); font-weight:600;">${c.errors_24h}</span>` : '0'}${c.last_error_at ? `<div class="text-muted" style="font-size:11px;">last ${when(c.last_error_at)}</div>` : ''}</td>
              <td class="po-row-actions">
                <button class="action-btn" type="button" data-cp-open="tenant-entitlements" data-cp-tenant="${escapeHTMLText(c.tenant_id)}">Modules</button>
                <button class="action-btn" type="button" data-cp-open="platform-console" data-cp-tenant="${escapeHTMLText(c.tenant_id)}">Data</button>
              </td>
            </tr>
            <tr class="cp-modules-row hidden" data-cp-modules-row="${i}"><td colspan="11" style="font-size:12px;">
              <b>On:</b> ${escapeHTMLText((c.modules_enabled || []).join(', ') || 'none')}<br>
              <b>Off:</b> ${escapeHTMLText((c.modules_disabled || []).join(', ') || 'none')}
            </td></tr>`).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(panel);

  const rel = document.createElement('div');
  rel.className = 'table-panel';
  const deps = cp.deployments || [];
  rel.innerHTML = `
    <h3 style="font-size:16px; font-weight:600; padding:16px 16px 0;">Releases</h3>
    <div class="table-wrapper">
      <table>
        <thead><tr><th>When</th><th>Environment</th><th>Version</th><th>Commit</th><th>By</th><th>Status</th><th>Notes</th></tr></thead>
        <tbody>
          ${deps.length === 0 ? `<tr><td colspan="7" style="color:var(--text-muted);">No release recorded in the ledger yet. Running now: <b>${escapeHTMLText(b.version || '')}</b> (${escapeHTMLText(b.git_commit || '')}).</td></tr>` : ''}
          ${deps.map(d => `<tr><td>${when(d.promoted_at)}</td><td>${escapeHTMLText(d.environment)}</td><td>${escapeHTMLText(d.app_version)}</td><td style="font-family:monospace;">${escapeHTMLText(d.git_commit)}</td><td>${escapeHTMLText(d.promoted_by)}</td><td>${escapeHTMLText(d.status)}</td><td>${escapeHTMLText(d.notes || '')}</td></tr>`).join('')}
        </tbody>
      </table>
    </div>
  `;
  container.appendChild(rel);

  panel.querySelectorAll('[data-cp-modules]').forEach(btn => btn.addEventListener('click', () => {
    const row = panel.querySelector(`[data-cp-modules-row="${btn.getAttribute('data-cp-modules')}"]`);
    if (row) row.classList.toggle('hidden');
  }));
  // Open the owning screen with this client already chosen.
  panel.querySelectorAll('[data-cp-open]').forEach(btn => btn.addEventListener('click', async () => {
    const view = btn.getAttribute('data-cp-open');
    const tenant = btn.getAttribute('data-cp-tenant');
    await renderView(view);
    const selId = view === 'tenant-entitlements' ? 'tenant-entitlements-select' : 'pc-tenant';
    for (let n = 0; n < 40; n++) {
      const sel = document.getElementById(selId);
      if (sel && [...sel.options].some(o => o.value === tenant)) {
        if (sel.value !== tenant) { sel.value = tenant; sel.dispatchEvent(new Event('change', { bubbles: true })); }
        return;
      }
      await new Promise(r => setTimeout(r, 100));
    }
  }));
}

export { renderControlPanelView };
