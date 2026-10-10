// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
// Where the PIM tab bar was scrolled to, kept across re-renders (Stage 57).
let pimTabBarScrollLeft = 0;

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
  // Stage 45: at a narrow enough width/zoom this row is wider than the page
  // - .page-container clips overflow-x rather than scrolling it, so without
  // this the trailing tabs were just gone with no way back to them. Scoping
  // the scroll to the bar itself (not the whole page) keeps the rest of the
  // screen from picking up a horizontal scrollbar too.
  //
  // flex-shrink: 0 is load-bearing, not decorative. Verified live: setting
  // only overflow-x on a flex item inside .page-container's column flexbox
  // let the whole bar collapse to 0px height whenever the page's other
  // content was tall enough to overflow vertically - overflow-x non-visible
  // suppresses this item's automatic min-height (content-based sizing only
  // applies when overflow is 'visible'), and per the CSS Overflow spec the
  // *used* value of overflow-y becomes 'auto' here regardless of what's
  // declared (confirmed: getComputedStyle still reports 'auto' even with
  // overflow-y explicitly set to 'visible' below - that line documents
  // intent but does not itself prevent the collapse). flex-shrink: 0 is what
  // actually fixes it: it exempts the item from shrinking at all, which
  // sidesteps the automatic-minimum-size calculation entirely rather than
  // trying to win it.
  tabBar.style.overflowX = 'auto';
  tabBar.style.overflowY = 'visible';
  tabBar.style.flexShrink = '0';
  tabBar.innerHTML = PIM_TABS.map(t =>
    `<button class="btn ${t.id === currentPIMTab ? 'btn-primary' : 'btn-outline'} btn-sm" data-pim-tab="${t.id}">${t.label}</button>`
  ).join('');
  container.appendChild(tabBar);
  // Stage 57 (user QA): every tab click re-renders the whole screen, which
  // rebuilt this bar scrolled back to its first tab - so picking Catalog
  // (far right) snapped the strip to the start and hid the tab just chosen.
  // The bar's scroll position is carried across the re-render, then nudged
  // only if the active tab would otherwise be out of view.
  // The screen is built off-page and settles over several layouts (attached
  // at a 1px width first), so the position is re-applied each time the bar
  // reaches a real width, until the user scrolls it themselves - and a
  // position read from a collapsed bar is never remembered.
  const REAL_WIDTH = 40;
  let userMovedBar = false;
  const restoreTabScroll = () => {
    if (userMovedBar || !tabBar.isConnected || tabBar.clientWidth < REAL_WIDTH) return;
    tabBar.scrollLeft = pimTabBarScrollLeft;
    const active = tabBar.querySelector('.btn-primary');
    if (active) {
      const left = active.offsetLeft - tabBar.offsetLeft;
      const right = left + active.offsetWidth;
      if (left < tabBar.scrollLeft) tabBar.scrollLeft = Math.max(0, left - 16);
      else if (right > tabBar.scrollLeft + tabBar.clientWidth) tabBar.scrollLeft = right - tabBar.clientWidth + 16;
    }
  };
  restoreTabScroll();
  new ResizeObserver(restoreTabScroll).observe(tabBar);
  ['wheel', 'pointerdown', 'touchstart', 'keydown'].forEach(type =>
    tabBar.addEventListener(type, () => { userMovedBar = true; }, { passive: true }));
  tabBar.addEventListener('scroll', () => {
    if (tabBar.clientWidth >= REAL_WIDTH) pimTabBarScrollLeft = tabBar.scrollLeft;
  }, { passive: true });
  tabBar.querySelectorAll('[data-pim-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      pimTabBarScrollLeft = tabBar.scrollLeft;
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
  } else if (currentPIMTab === 'media-library') {
    await renderPIMMediaLibraryTab(container);
  }
}

// Stage 36.6.3: catalog-wide media browse/search/tag - the per-item gallery
// inside the Workbench (renderPIMWorkbenchTab's Media section) stays as-is
// for "upload onto the product I'm looking at"; this tab is for "find/tag/
// bulk-manage assets across the whole catalog," which had no screen at all
// before this (metadata like tags already existed server-side; only the
// browse UI was missing).
const PIM_MEDIA_ROLES = ['Main Image', 'Gallery', 'Variant Image', 'Lifestyle', 'Certificate', 'Internal QC', 'Video/Other'];

async function renderPIMMediaLibraryTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap; margin-bottom:16px;">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-item">Item code</label>
        <input type="text" id="pim-medlib-item" class="form-input" style="width:160px;" placeholder="e.g. SKU-001">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-role">Role</label>
        <select id="pim-medlib-role" class="form-input" style="width:150px;">
          <option value="">Any role</option>
          ${PIM_MEDIA_ROLES.map(role => `<option value="${role}">${role}</option>`).join('')}
        </select>
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-tag">Tag</label>
        <input type="text" id="pim-medlib-tag" class="form-input" style="width:140px;" placeholder="e.g. winter-2026">
      </div>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-filetype">File type</label>
        <select id="pim-medlib-filetype" class="form-input" style="width:140px;">
          <option value="">Any type</option>
          <option value="image/jpeg">JPEG</option>
          <option value="image/png">PNG</option>
          <option value="image/webp">WebP</option>
          <option value="image/gif">GIF</option>
          <option value="application/pdf">PDF</option>
        </select>
      </div>
      <button class="btn btn-outline" id="pim-medlib-search">Search</button>
    </div>
    <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap; margin-bottom:16px; padding-top:12px; border-top:1px solid var(--border-color);">
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-bulk-zip">Bulk upload (.zip, files named ITEMCODE__role.ext or ITEMCODE.ext)</label>
        <input type="file" id="pim-medlib-bulk-zip" class="form-input" accept=".zip">
      </div>
      <button class="btn btn-outline" id="pim-medlib-bulk-upload-btn">Upload ZIP</button>
      <div class="form-group" style="margin-bottom:0;">
        <label class="form-label" for="pim-medlib-bulk-items">Bulk download - item codes (comma-separated)</label>
        <input type="text" id="pim-medlib-bulk-items" class="form-input" style="width:220px;" placeholder="SKU-001, SKU-002">
      </div>
      <button class="btn btn-outline" id="pim-medlib-bulk-download-btn">Download ZIP</button>
    </div>
    <div id="pim-medlib-bulk-result" style="margin-bottom:16px;"></div>
    <div id="pim-medlib-grid" style="display:flex; gap:12px; flex-wrap:wrap;"></div>
  `;
  container.appendChild(panel);

  const runSearch = async () => {
    const grid = panel.querySelector('#pim-medlib-grid');
    grid.innerHTML = `<div class="text-muted">Searching&hellip;</div>`;
    const params = new URLSearchParams();
    const item = panel.querySelector('#pim-medlib-item').value.trim();
    const role = panel.querySelector('#pim-medlib-role').value;
    const tag = panel.querySelector('#pim-medlib-tag').value.trim();
    const fileType = panel.querySelector('#pim-medlib-filetype').value;
    if (item) params.set('item', item);
    if (role) params.set('role', role);
    if (tag) params.set('tag', tag);
    if (fileType) params.set('file_type', fileType);
    const res = await apiFetch(`/api/v1/pim/media/search?${params.toString()}`);
    if (!res || !res.ok) { grid.innerHTML = `<div class="text-muted">Unable to search media right now.</div>`; return; }
    const assets = await res.json();
    if (assets.length === 0) {
      grid.innerHTML = `<div class="text-muted">No media matches these filters.</div>`;
      return;
    }
    grid.innerHTML = assets.map(m => `
      <div class="table-panel" style="padding:8px; width:160px;" data-medlib-card="${escapeHTMLText(m.id)}">
        <div style="font-size:11px; font-weight:600; margin-bottom:4px;">${escapeHTMLText(m.item)}</div>
        <img data-medlib-thumb="${escapeHTMLText(m.id)}" style="width:100%; height:90px; object-fit:cover; background:var(--surface-subtle); border-radius:4px;" alt="${escapeHTMLText(m.alt_text || m.media_role)}">
        <div class="text-muted" style="font-size:10px; margin-top:4px;">${escapeHTMLText(m.media_role)} &middot; ${escapeHTMLText(m.file_type)}</div>
        <div style="font-size:10px; margin-top:2px; word-break:break-word;">${m.tags ? escapeHTMLText(m.tags) : '<span class="text-muted">No tags</span>'}</div>
        <button class="btn btn-outline btn-sm" style="width:100%; margin-top:6px;" data-medlib-edit-tags="${escapeHTMLText(m.id)}" data-tags="${escapeHTMLText(m.tags || '')}">Edit Tags</button>
        <button class="btn btn-outline btn-sm" style="width:100%; margin-top:4px;" data-medlib-deactivate="${escapeHTMLText(m.id)}">Deactivate</button>
      </div>
    `).join('');

    // <img> can't carry a Bearer token - same authenticated-blob swap
    // renderPIMMediaGallery already uses, preferring the small "small"
    // transform (36.6.1) over the full original for a lighter grid.
    assets.forEach(async (m) => {
      let imgRes = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/transform/small`);
      if (!imgRes || !imgRes.ok) imgRes = m.has_thumbnail ? await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/thumbnail`) : null;
      if (!imgRes || !imgRes.ok) imgRes = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/file`);
      if (imgRes && imgRes.ok) {
        const blob = await imgRes.blob();
        const imgEl = grid.querySelector(`[data-medlib-thumb="${CSS.escape(m.id)}"]`);
        if (imgEl) imgEl.src = URL.createObjectURL(blob);
      }
    });

    grid.querySelectorAll('[data-medlib-deactivate]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const mediaId = btn.getAttribute('data-medlib-deactivate');
        if (!await showCustomConfirm('Deactivate this media asset? It stops appearing anywhere it is used from.', 'Deactivate Media')) return;
        const res2 = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(mediaId)}/deactivate`, { method: 'POST' });
        if (res2 && res2.ok) runSearch();
      });
    });
    grid.querySelectorAll('[data-medlib-edit-tags]').forEach(btn => {
      btn.addEventListener('click', async () => {
        const mediaId = btn.getAttribute('data-medlib-edit-tags');
        const tags = await showCustomPrompt('Tags (comma-separated):', btn.getAttribute('data-tags') || '', 'Edit Media Tags');
        if (tags === null) return;
        const res2 = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(mediaId)}/metadata`, {
          method: 'POST', body: JSON.stringify({ tags })
        });
        if (!res2) return;
        if (!res2.ok) { await showApiError(res2, 'Failed to update tags.'); return; }
        runSearch();
      });
    });
  };

  panel.querySelector('#pim-medlib-search').addEventListener('click', runSearch);

  panel.querySelector('#pim-medlib-bulk-upload-btn').addEventListener('click', async () => {
    const fileInput = panel.querySelector('#pim-medlib-bulk-zip');
    const resultEl = panel.querySelector('#pim-medlib-bulk-result');
    if (!fileInput.files[0]) { resultEl.innerHTML = `<div class="login-error">Choose a .zip file first.</div>`; return; }
    const formData = new FormData();
    formData.append('file', fileInput.files[0]);
    resultEl.innerHTML = `<div class="text-muted">Uploading&hellip;</div>`;
    // apiUpload (not apiFetch): a multipart body needs the browser to set
    // its own Content-Type with the boundary - apiFetch's default
    // 'Content-Type: application/json' would break parsing server-side.
    const res = await apiUpload('/api/v1/pim/media/bulk-upload', formData);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Bulk upload failed.'); resultEl.innerHTML = ''; return; }
    const outcomes = await res.json();
    const ok = outcomes.filter(o => !o.error).length;
    resultEl.innerHTML = `<div class="table-wrapper"><p>${ok} of ${outcomes.length} file(s) uploaded.</p><table><thead><tr><th>File</th><th>Item</th><th>Role</th><th>Result</th></tr></thead><tbody>${
      outcomes.map(o => `<tr><td>${escapeHTMLText(o.filename)}</td><td>${escapeHTMLText(o.item_code)}</td><td>${escapeHTMLText(o.media_role)}</td><td>${o.error ? `<span class="badge badge-danger">${escapeHTMLText(o.error)}</span>` : `<span class="badge badge-success">Saved</span>`}</td></tr>`).join('')
    }</tbody></table></div>`;
    runSearch();
  });

  panel.querySelector('#pim-medlib-bulk-download-btn').addEventListener('click', async () => {
    const raw = panel.querySelector('#pim-medlib-bulk-items').value.trim();
    if (!raw) { showCustomAlert('Enter one or more comma-separated item codes first.', 'Bulk Download'); return; }
    const res = await apiFetch(`/api/v1/pim/media/bulk-download?item=${encodeURIComponent(raw)}`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Bulk download failed.'); return; }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'pim_media_bulk.zip';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });

  await runSearch();
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
    results.innerHTML = `<table><thead><tr>${columns.map(column => `<th>${escapeHTMLText(column.replaceAll('_', ' '))}</th>`).join('')}</tr></thead><tbody>${rows.map(row => `<tr>${columns.map(column => `<td>${escapeHTMLText(row[column] ?? '')}</td>`).join('')}</tr>`).join('')}</tbody></table>`;
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
          ${families.map(f => `<option value="${escapeHTMLText(f.code || f.id)}" ${(f.code || f.id) === currentPIMFamilyFilter ? 'selected' : ''}>${escapeHTMLText(f.name || f.code || f.id)}</option>`).join('')}
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
          <tr class="pim-workbench-row" data-item="${escapeHTMLText(e.item_code)}" style="cursor: pointer;">
            <td style="font-family: monospace;">${escapeHTMLText(e.item_code)}</td>
            <td>${escapeHTMLText(e.name || '')}</td>
            <td ${e.family ? `data-link-ref="${escapeHTMLText(e.family)}" data-link-doctype="ProductFamily"` : ''}>${escapeHTMLText(e.family || '')}</td>
            <td><span class="badge badge-secondary">${escapeHTMLText(e.status || '')}</span></td>
            <td><span class="badge ${badgeClass}">${escapeHTMLText(e.score)}%</span></td>
            <td>${escapeHTMLText(e.missing_count)}</td>
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
    <h2 style="font-size: 16px; font-weight: 700; margin-bottom: 8px;"><span data-link-ref="${escapeHTMLText(itemCode)}" data-link-doctype="Item">${escapeHTMLText(itemCode)}</span> - Completeness: ${escapeHTMLText(comp.score)}% <span class="badge badge-secondary" style="margin-left: 8px;">${escapeHTMLText(comp.enrichment_status || '')}</span></h2>
    <p style="color: var(--text-muted); margin-bottom: 16px;">
      Missing: ${comp.missing_fields && comp.missing_fields.length > 0 ? escapeHTMLText(comp.missing_fields.join(', ')) : 'Nothing - fully complete.'}
    </p>

    <h3 style="font-size: 14px; font-weight: 700; margin-bottom: 12px;">Add / Update Attribute Value</h3>
    <div style="display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 24px;">
      <div class="form-group" style="margin-bottom: 0;">
        <label class="form-label" for="pim-attr-select">Attribute</label>
        <select id="pim-attr-select" class="form-input" style="width: 200px;">
          <option value="">Select attribute</option>
          ${attrDefs.map(a => `<option value="${escapeHTMLText(a.code || a.id)}">${escapeHTMLText(a.label || a.code || a.id)}</option>`).join('')}
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
          ${channelsForOverride.map(c => `<option value="${escapeHTMLText(c.code || c.id)}">${escapeHTMLText(c.name || c.code || c.id)}</option>`).join('')}
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
        <label class="form-label" for="pim-content-shape" title="Marketplace shapes the title and adds bullet points plus a meta description - the format most marketplace listing pages use, versus a storefront page's single flowing description.">Assist Shape</label>
        <select id="pim-content-shape" class="form-input" style="width: 140px;">
          <option value="standard">Standard</option>
          <option value="marketplace">Marketplace</option>
        </select>
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

    <h3 style="font-size: 14px; font-weight: 700; margin: 24px 0 12px;">Related Products</h3>
    <div id="pim-related-products" class="text-muted">Loading&hellip;</div>
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
  await renderPIMRelatedProducts(itemCode);
}

// renderPIMRelatedProducts (Stage 36.7.3): other Active items in the same
// family sharing attribute values with this one, most-shared first -
// clicking a row jumps the Workbench straight to that product, the same
// "row is a shortcut into the thing it names" pattern the workbench's own
// item list already uses.
async function renderPIMRelatedProducts(itemCode) {
  const el = document.getElementById('pim-related-products');
  if (!el) return;
  const res = await apiFetch(`/api/v1/pim/related-products/${encodeURIComponent(itemCode)}`);
  if (!res || !res.ok) { el.textContent = 'Unable to load related products.'; return; }
  const related = await res.json();
  if (!related || related.length === 0) {
    el.innerHTML = '<span class="text-muted">No related products found (needs a shared family and at least one shared attribute value).</span>';
    return;
  }
  el.innerHTML = `
    <table>
      <thead><tr><th>Item</th><th>Name</th><th>Shared Attributes</th></tr></thead>
      <tbody>
        ${related.map(r => `
          <tr class="pim-related-row" data-item="${escapeHTMLText(r.item_code)}" style="cursor:pointer;">
            <td style="font-family: monospace;">${escapeHTMLText(r.item_code)}</td>
            <td>${escapeHTMLText(r.name)}</td>
            <td>${r.shared_attributes}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  `;
  el.querySelectorAll('.pim-related-row').forEach(row => {
    row.addEventListener('click', () => {
      currentPIMSelectedItem = row.getAttribute('data-item');
      renderView('pim');
    });
  });
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
  const shape = document.getElementById('pim-content-shape').value || 'standard';
  const res = await apiFetch(`/api/v1/pim/content-assist/${encodeURIComponent(itemCode)}?language=${encodeURIComponent(language)}&shape=${encodeURIComponent(shape)}`);
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
  // Stage 36.7.1: bullets/meta_description only come back for the
  // marketplace shape. Neither has a ProductContent field of its own, so
  // they are shown here for the reviewer to copy manually rather than
  // silently dropped.
  const bullets = (draft.bullets || []).map(b => `<li>${escapeHTMLText(b)}</li>`).join('');
  const marketplaceExtra = (draft.bullets && draft.bullets.length > 0) || draft.meta_description
    ? `<div style="margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--border-color);">
         ${bullets ? `<div><strong>Bullet points:</strong></div><ul style="margin: 4px 0 0 18px;">${bullets}</ul>` : ''}
         ${draft.meta_description ? `<div style="margin-top: 6px;"><strong>Meta description:</strong> ${escapeHTMLText(draft.meta_description)}</div>` : ''}
       </div>`
    : '';
  noteEl.innerHTML = `
    <strong>${filled} field${filled === 1 ? '' : 's'} filled from this product's own data.</strong>
    Nothing has been saved yet - review the text, then use Save Draft or Submit for Approval.
    <div style="margin-top: 6px;">Built from: ${escapeHTMLText(sources)}</div>
    ${warnings ? `<ul style="margin: 6px 0 0 18px;">${warnings}</ul>` : ''}
    ${marketplaceExtra}`;
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
// 21.14/BLD-034: SWR-backed - fetches this doctype's field metadata + one
// page of records exactly like before, but wrapped so a cached copy (if
// any) renders immediately with no network wait, then gets silently
// swapped for fresh data in the background. Cold start (nothing cached
// yet - the common case for a doctype no one's opened this tab session)
// falls back to the exact original await-then-render behavior, so this
// changes nothing about correctness, only repeat-visit perceived speed.
//
// BLD-034: this used to fetch the whole (server-capped, up to 500-1000 row)
// record set once and let renderDocTable's own .filter()/.slice() page and
// search it entirely in the browser - meaning a doctype with more rows than
// that cap was silently unreachable past row ~500 no matter how many
// "pages" the pagination footer showed, and the search box could never see
// past that same window either. Paging/searching are now real server round
// trips (limit/offset/q on the existing list endpoint), keyed by page+
// search so the SWR cache below can't hand back the wrong page.

export { renderPIMShellHeader, renderPIMView, renderPIMMediaLibraryTab, renderPIMDashboardTab, pimTaskQuery, renderPIMMyWorkTab, loadPIMAssignableUsers, loadPIMTaskTemplates, loadPIMWorkflowDefinitions, loadPIMTasks, renderPIMTaskTiles, pimTaskStatusBadge, renderPIMTaskTable, updatePIMTaskBulkBar, runPIMTaskAction, runPIMTaskBulk, openPIMTaskDetail, runPIMTaskTemplate, startPIMWorkflowRun, loadPIMWorkflowRuns, renderPIMReportsTab, renderPIMWorkbenchTab, renderPIMDetailPanel, renderPIMRelatedProducts, runPIMContentAssist, renderPIMMediaGallery, uploadPIMMedia, renderPIMPublishSection, previewPIMPublish, renderPIMContentHistory, publishPIMItem, savePIMAttributeValue, pimContentPayload, savePIMContent, submitPIMContent };
