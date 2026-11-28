// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
const PIM_GROUPS = [
  { id: 'products', label: 'Products', tabs: ['dashboard', 'workbench', 'reports', 'families', 'attributes', 'attribute-groups', 'family-attributes'] },
  { id: 'work', label: 'My work', tabs: ['my-work', 'supplier-submissions', 'task-templates', 'workflows'] },
  { id: 'media', label: 'Media', tabs: ['media-library'] },
  { id: 'channels', label: 'Channels & publishing', tabs: ['channels', 'channel-category-map', 'channel-field-map', 'channel-validation-rules', 'media-exceptions', 'catalogs', 'export-templates', 'export-schedules'] },
  { id: 'advanced', label: 'Advanced setup', tabs: ['import-templates', 'import-schedules'] }
];

function openPIMTab(tab) {
  if (!tab) return;
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
  renderView(`pim:${tab.id}`);
}

function renderPIMShellHeader(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${currentPIMTab === 'dashboard' ? 'PIM home' : 'PIM'}</h1>
    </div>
  `;
  container.appendChild(header);
}

async function renderPIMView(container) {
  // Stage 88.1: each working page is its own menu entry ("pim:<tab>"); the group and tab bars are gone,
  // and the setup tabs (families, attributes, channel maps, templates) are Setup > PIM lists.
  if (currentViewTab) currentPIMTab = currentViewTab;
  renderPIMShellHeader(container);

  if (currentPIMTab === 'dashboard') renderPIMSetupPath(container);

  if (currentPIMTab === 'dashboard') {
    await renderPIMDashboardTab(container);
  } else if (currentPIMTab === 'workbench') {
    await renderPIMWorkbenchTab(container);
  } else if (currentPIMTab === 'reports') {
    await renderPIMReportsTab(container);
  } else if (currentPIMTab === 'my-work') {
    await renderPIMMyWorkTab(container);
  } else if (currentPIMTab === 'workflow-runs') {
    await renderPIMWorkflowRunsTab(container);
  } else if (currentPIMTab === 'media-library') {
    await renderPIMMediaLibraryTab(container);
  } else if (currentPIMTab === 'category-rules') {
    await (await loadViewModule('/view-pim-category-rules.js' + new URL(import.meta.url).search)).renderCategoryRules(container);
  } else if (currentPIMTab === 'stewardship') {
    // 47.13.4: who owns each attribute, where its value comes from
    const stw = await loadViewModule('/view-pim-stewardship.js' + new URL(import.meta.url).search);
    await stw.renderStewardship(container);
  } else if (["range", "markdown", "drift"].includes(currentPIMTab)) {
    await (await loadViewModule("/view-pim-range.js" + new URL(import.meta.url).search)).renderPIMRangePage(container, currentPIMTab);
  } else if (['channel-templates', 'feeds', 'design-values', 'checklists'].includes(currentPIMTab)) {
    // 146.5: marketplace templates, feeds with run history, design-level values
    const data = await loadViewModule('/view-pim-data.js' + new URL(import.meta.url).search);
    await data.renderPIMDataPage(container, currentPIMTab);
  }
}

// 302.1: the setup path as numbered step tiles (.pim-steps in view-inline.css).
// The tile draws its own number, so the labels carry none.
function renderPIMSetupPath(container) {
  const panel = document.createElement('section');
  panel.className = 'table-panel pim-setup';
  const title = document.createElement('h2');
  title.className = 'pim-setup-title';
  title.textContent = 'Set up a product for publishing';
  panel.appendChild(title);
  const steps = [
    ['Item (SKU, HSN, GST)', 'Item'],
    ['Product family', 'families'],
    ['Attribute definitions', 'attributes'],
    ['Family attributes', 'family-attributes'],
    ['Assign item to family', 'Item'],
    ['Enrich in Workbench', 'workbench'],
    ['Check completeness', 'reports'],
    ['Channel mappings', 'channels'],
    ['Publish from Workbench', 'workbench']
  ];
  const list = document.createElement('ol');
  list.className = 'pim-steps';
  steps.forEach(([label, target], i) => {
    const item = document.createElement('li');
    const link = document.createElement('a');
    link.className = 'pim-step';
    link.href = target === 'Item' ? '#/setup/Item' : '#/view/pim';
    const num = document.createElement('span');
    num.className = 'pim-step-num';
    num.textContent = String(i + 1);
    const text = document.createElement('span');
    text.textContent = label;
    link.append(num, text);
    if (target !== 'Item') link.addEventListener('click', event => {
      event.preventDefault();
      openPIMTab(PIM_TABS.find(tab => tab.id === target));
    });
    item.appendChild(link);
    list.appendChild(item);
  });
  panel.appendChild(list);
  container.appendChild(panel);
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
    <div class="s-b0e775">
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-item">Item code</label>
        <input type="text" id="pim-medlib-item" class="form-input s-28c0f6" placeholder="e.g. SKU-001">
      </div>
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-role">Role</label>
        <select id="pim-medlib-role" class="form-input s-7c07cd">
          <option value="">Any role</option>
          ${PIM_MEDIA_ROLES.map(role => `<option value="${role}">${role}</option>`).join('')}
        </select>
      </div>
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-tag">Tag</label>
        <input type="text" id="pim-medlib-tag" class="form-input s-830eb9" placeholder="e.g. winter-2026">
      </div>
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-filetype">File type</label>
        <select id="pim-medlib-filetype" class="form-input s-830eb9">
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
    <div class="s-763e02">
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-bulk-zip">Bulk upload (.zip, files named ITEMCODE__role.ext or ITEMCODE.ext)</label>
        <input type="file" id="pim-medlib-bulk-zip" class="form-input" accept=".zip">
      </div>
      <button class="btn btn-outline" id="pim-medlib-bulk-upload-btn">Upload ZIP</button>
      <div class="form-group s-ef0b7a">
        <label class="form-label" for="pim-medlib-bulk-items">Bulk download - item codes (comma-separated)</label>
        <input type="text" id="pim-medlib-bulk-items" class="form-input s-0fac91" placeholder="SKU-001, SKU-002">
      </div>
      <button class="btn btn-outline" id="pim-medlib-bulk-download-btn">Download ZIP</button>
    </div>
    <div id="pim-medlib-bulk-result" class="s-87c136"></div>
    <div id="pim-medlib-grid" class="s-c79bb0"></div>
  `;
  container.appendChild(panel);
  // 146.5.9: image order per channel
  try { await (await loadViewModule('/view-pim-stewardship.js' + new URL(import.meta.url).search)).mountChannelOrder(container); } catch (e) { console.error('channel order', e); }

  let medlibObserver = null;
  const medlibUrls = new Set();
  const runSearch = async () => {
    const grid = panel.querySelector('#pim-medlib-grid');
    if (medlibObserver) medlibObserver.disconnect();
    medlibObserver = null;
    medlibUrls.forEach(u => URL.revokeObjectURL(u));
    medlibUrls.clear();
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
      <div class="table-panel s-7496d0" data-medlib-card="${escapeHTMLText(m.id)}">
        <div class="s-d29ea2">${escapeHTMLText(m.item)}</div>
        <img data-medlib-thumb="${escapeHTMLText(m.id)}" class="s-2e6334" alt="${escapeHTMLText(m.alt_text || m.media_role)}">
        <div class="text-muted s-5c66c7">${escapeHTMLText(m.media_role)} &middot; ${escapeHTMLText(m.file_type)}</div>
        <div class="s-ae6463">${m.tags ? escapeHTMLText(m.tags) : '<span class="text-muted">No tags</span>'}</div>
        <button class="btn btn-outline btn-sm s-313cf2" data-medlib-edit-tags="${escapeHTMLText(m.id)}" data-tags="${escapeHTMLText(m.tags || '')}">Edit Tags</button>
        <button class="btn btn-outline btn-sm s-bb1ee8" data-medlib-deactivate="${escapeHTMLText(m.id)}">Deactivate</button>
      </div>
    `).join('');

    // <img> can't carry a Bearer token - same authenticated-blob swap
    // renderPIMMediaGallery already uses, preferring the small "small"
    // transform (36.6.1) over the full original for a lighter grid.
    // 156.1: a thumbnail is fetched only as its card nears the viewport.
    const byId = new Map(assets.map(m => [String(m.id), m]));
    const obs = medlibObserver = new IntersectionObserver((entries) => {
      entries.forEach(async (en) => {
        if (!en.isIntersecting) return;
        const imgEl = en.target;
        obs.unobserve(imgEl);
        const m = byId.get(imgEl.getAttribute('data-medlib-thumb'));
        // 193: only a JPEG/PNG original has resized copies (ReadMediaTransform), and a
        // file of another known type (PDF, video) cannot show in an <img>: ask for neither.
        const ft = String(m.file_type || '');
        if (ft && !ft.startsWith('image/')) return;
        let imgRes = (ft === 'image/jpeg' || ft === 'image/png') ? await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/transform/small`) : null;
        if (!imgRes || !imgRes.ok) imgRes = m.has_thumbnail ? await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/thumbnail`) : null;
        if (!imgRes || !imgRes.ok) imgRes = await apiFetch(`/api/v1/pim/media/${encodeURIComponent(m.id)}/file`);
        if (!imgRes || !imgRes.ok || medlibObserver !== obs) return;
        const url = URL.createObjectURL(await imgRes.blob());
        if (medlibObserver !== obs) { URL.revokeObjectURL(url); return; }
        medlibUrls.add(url);
        imgEl.src = url;
      });
    }, { rootMargin: '300px' });
    grid.querySelectorAll('[data-medlib-thumb]').forEach(el => obs.observe(el));

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
  if (!res || !res.ok) { renderErrorPanel(container, 'Unable to load PIM home.', () => renderView('pim')); return; }
  const stats = await res.json();
  const cards = [
    ['total_products', 'Products', 'workbench'], ['incomplete_products', 'Incomplete', 'workbench'],
    ['pending_content_approvals', 'Pending approval', 'content'], ['ready_to_publish', 'Ready to publish', 'workbench'],
    ['published_products', 'Published', 'workbench'], ['missing_main_images', 'Missing main image', 'workbench'],
    ['queued_publish_jobs', 'Publish queued', 'workbench'], ['failed_publish_jobs', 'Publish failed', 'workbench']
  ];
  const panel = document.createElement('div'); panel.className = 'table-panel';
  panel.innerHTML = `<div class="s-34d8d3">${cards.map(([key, label, target]) => `<button class="table-panel pim-dashboard-card s-d5a21b" data-pim-dashboard-target="${target}"><div class="text-muted s-6cb285">${label}</div><div class="s-de7088">${stats[key] ?? 0}</div></button>`).join('')}</div>`;
  container.appendChild(panel);
  await renderPIMDashboardTaskCards(panel.firstElementChild);
  panel.querySelectorAll('[data-pim-dashboard-target]').forEach(card => card.addEventListener('click', () => {
    const target = card.getAttribute('data-pim-dashboard-target');
    if (target === 'content') { currentDoctype = 'ProductContent'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); return; }
    currentPIMTab = 'workbench'; renderView('pim');
  }));
  await renderPIMFeedsPanel(container);
}

const PIM_PACK_PLATFORMS = ['shopify', 'amazon', 'myntra'];

async function renderPIMFeedsPanel(container) {
  const get = async (url) => { const r = await apiFetch(url); return r && r.ok ? r.json() : []; };
  const [channels, templates] = await Promise.all([get('/api/v1/doc/Channel?limit=200'), get('/api/v1/doc/PIMExportTemplate?limit=200')]);
  const withFeed = new Set(templates.map(t => t.channel).filter(Boolean));
  const packable = channels.filter(c => PIM_PACK_PLATFORMS.includes(String(c.platform || '').toLowerCase()) && !withFeed.has(c.id));
  const active = templates.filter(t => (t.status || 'Active') === 'Active');
  if (!packable.length && !active.length) return;
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.marginTop = '16px';
  panel.innerHTML = `<h3 class="s-01f1a9">Marketplace feeds</h3>
    ${packable.map(c => `<div class="s-ca3dca"><span>${escapeHTMLText(c.name || c.id)} (${escapeHTMLText(c.platform)})</span>
      <button type="button" class="btn btn-outline btn-sm" data-pack="${escapeHTMLText(c.id)}">Apply ${escapeHTMLText(c.platform)} layout</button></div>`).join('')}
    ${active.length ? `<div class="table-wrapper"><table><thead><tr><th>Feed</th><th>Family</th><th>Category</th><th>File</th><th></th></tr></thead><tbody>
      ${active.map(t => `<tr data-feed="${escapeHTMLText(t.id)}"><td>${escapeHTMLText(t.name || t.id)}</td>
        <td><input class="form-input s-465bfe" data-feed-family placeholder="all"></td><td><input class="form-input s-465bfe" data-feed-category placeholder="all"></td>
        <td><select class="form-select s-30e741" data-feed-format><option value="csv">CSV</option><option value="xlsx">XLSX</option></select></td>
        <td><button type="button" class="btn btn-primary btn-sm" data-feed-run>Download</button></td></tr>`).join('')}</tbody></table></div>` : ''}`;
  container.appendChild(panel);
  panel.querySelectorAll('[data-pack]').forEach(btn => btn.addEventListener('click', async () => {
    btn.disabled = true;
    const r = await apiFetch(`/api/v1/pim/channels/${encodeURIComponent(btn.dataset.pack)}/apply-pack`, { method: 'POST' });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The layout was not applied.'); return; }
    const out = await r.json();
    await showCustomAlert(`${out.platform} layout ready: ${out.columns} columns, ${out.field_maps_added} field map(s).${out.attributes_created.length ? `\nNew attributes to fill on your items: ${out.attributes_created.join(', ')}.` : ''}`, 'Marketplace feeds');
    renderView('pim');
  }));
  panel.querySelectorAll('[data-feed-run]').forEach(btn => btn.addEventListener('click', async () => {
    const row = btn.closest('[data-feed]');
    const q = new URLSearchParams({ family: row.querySelector('[data-feed-family]').value.trim(), category: row.querySelector('[data-feed-category]').value.trim(), format: row.querySelector('[data-feed-format]').value });
    btn.disabled = true;
    const r = await apiFetch(`/api/v1/pim/export-templates/${encodeURIComponent(row.dataset.feed)}/run?${q}`, { method: 'POST' });
    btn.disabled = false;
    if (!r) return;
    if (!r.ok) { showApiError(r, 'The feed was not exported.'); return; }
    const blob = await r.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = ((r.headers.get('Content-Disposition') || '').split('filename=')[1] || `feed.${q.get('format')}`).replace(/"/g, '');
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 5000);
    const skipped = Number(r.headers.get('X-Export-Skipped-Count') || 0);
    if (skipped) {
      let list = [];
      try { list = JSON.parse(decodeURIComponent((r.headers.get('X-Export-Skipped') || '').replace(/\+/g, ' '))); } catch (_) {}
      await showCustomAlert(`${r.headers.get('X-Export-Rows')} item(s) exported, ${skipped} left out:\n${list.map(s => `${s.item}: ${s.reason}`).join('\n')}`, 'Feed');
    }
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
  const { txnPagedSection, txnRestorePane } = await import('./txn-screen.js' + new URL(import.meta.url).search);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  // 284.2: the task count tiles sit on PIM home (one page, one job).
  panel.innerHTML = `
    <div class="table-controls s-f10932">
      <div class="form-group s-116966">
        <label class="form-label" for="pim-task-assignee">Assignee</label>
        <select class="form-select" id="pim-task-assignee"><option value="me">My tasks</option><option value="all">Everyone</option></select>
      </div>
      <div class="form-group s-116966">
        <label class="form-label" for="pim-task-status">Status</label>
        <select class="form-select" id="pim-task-status">
          <option value="">Any status</option><option value="Open">Open</option><option value="In Progress">In Progress</option>
          <option value="Blocked">Blocked</option><option value="Done">Done</option><option value="Cancelled">Cancelled</option>
        </select>
      </div>
      <div class="form-group s-116966">
        <label class="form-label" for="pim-task-type">Type</label>
        <select class="form-select" id="pim-task-type">
          <option value="">Any type</option><option value="Enrichment">Enrichment</option><option value="Imagery">Imagery</option>
          <option value="Attributes">Attributes</option><option value="Translation">Translation</option>
          <option value="Review">Review</option><option value="Other">Other</option>
        </select>
      </div>
      <div class="form-group s-116966">
        <label class="form-label" for="pim-task-priority">Priority</label>
        <select class="form-select" id="pim-task-priority">
          <option value="">Any priority</option><option value="High">High</option><option value="Normal">Normal</option><option value="Low">Low</option>
        </select>
      </div>
      <label class="s-71ebaf">
        <input type="checkbox" id="pim-task-overdue"> Overdue only
      </label>
      <button class="btn btn-outline s-d6f2af" id="pim-task-refresh">Refresh</button>
    </div>
    <div class="bulk-edit-bar hidden" id="pim-task-bulk-bar">
      <span id="pim-task-selection-count">0 selected</span>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-assign">Reassign</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-status">Set status</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-due">Set due date</button>
      <button class="btn btn-outline btn-sm" id="pim-task-bulk-comment">Add comment</button>
    </div>
    <div class="table-wrapper s-56f435" id="pim-task-table"></div>`;

  const templatePanel = document.createElement('div');
  templatePanel.className = 'table-panel';
  templatePanel.style.padding = '24px';
  templatePanel.innerHTML = `
    <div class="table-controls s-f10932">
      <div class="form-group s-116966"><label class="form-label" for="pim-template-select">Template</label><select class="form-select" id="pim-template-select"></select></div>
      <div class="form-group s-116966"><label class="form-label" for="pim-template-group">Product group</label><select class="form-select" id="pim-template-group"></select></div>
      <button class="btn btn-primary s-d6f2af" id="pim-template-run">Create tasks</button>
    </div>
    <div id="pim-template-result" class="s-56f435"></div>`;
  // 284.2 (ask 300, one page, one job): the list is the page; Run a task
  // template is a page of its own behind its button, and Workflow runs is its
  // own PIM page (pim:workflow-runs).
  container.appendChild(txnPagedSection({ id: 'pim-my-work', title: 'My Work', page: 'Run a task template', heading: 'h2',
    newLabel: 'Run a task template', form: templatePanel, list: panel }));
  txnRestorePane('pim-my-work');


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

  // The reads are independent, so they go out together rather than in
  // sequence - the inbox is opened dozens of times a day.
  await Promise.all([loadPIMAssignableUsers(), loadPIMTaskTemplates(), loadPIMTasks()]);
}

// 284.2 (ask 300): Workflow runs, its own PIM page - start a run, see the runs.
// 319.1: starting a run needs the workflow list and the server's two create
// rights (handlePIMWorkflowStart); a role without them (Cashier) sees only the
// runs, instead of a 403 and a "No active workflows" box it cannot use.
async function renderPIMWorkflowRunsTab(container) {
  const mayStart = canReadDoctype('PIMWorkflowDefinition') && canDo('create', 'PIMWorkflowRun') && canDo('create', 'PIMTask');
  const workflowPanel = document.createElement('div');
  workflowPanel.className = 'table-panel';
  workflowPanel.style.padding = '24px';
  workflowPanel.innerHTML = `
    <h2 class="s-8406f1">Workflow runs</h2>
    <div class="table-controls s-f10932">
      ${mayStart ? `<div class="form-group s-116966"><label class="form-label" for="pim-workflow-select">Workflow</label><select class="form-select" id="pim-workflow-select"></select></div>
      <div class="form-group s-116966"><label class="form-label" for="pim-workflow-target">Start for</label><select class="form-select" id="pim-workflow-target"><option value="item">One product</option><option value="group">A product group</option></select></div>
      <div class="form-group s-116966"><label class="form-label" for="pim-workflow-ref">Product / group</label><input class="form-input" id="pim-workflow-ref" placeholder="Item code"></div>
      <button class="btn btn-primary s-d6f2af" id="pim-workflow-start">Start run</button>` : ''}
      <div class="form-group s-116966"><label class="form-label" for="pim-run-status">Show</label><select class="form-select" id="pim-run-status"><option value="Running">Running</option><option value="Paused">Paused</option><option value="">All</option><option value="Completed">Completed</option><option value="Cancelled">Cancelled</option></select></div>
    </div>
    <div class="table-wrapper s-56f435" id="pim-run-table"></div>`;
  container.appendChild(workflowPanel);
  workflowPanel.querySelector('#pim-run-status').addEventListener('change', loadPIMWorkflowRuns);
  if (!mayStart) { await loadPIMWorkflowRuns(); return; }
  workflowPanel.querySelector('#pim-workflow-start').addEventListener('click', startPIMWorkflowRun);
  workflowPanel.querySelector('#pim-workflow-target').addEventListener('change', e => {
    workflowPanel.querySelector('#pim-workflow-ref').placeholder =
      e.target.value === 'group' ? 'Product group id or code' : 'Item code';
  });
  await Promise.all([loadPIMWorkflowDefinitions(), loadPIMWorkflowRuns()]);
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
  host.innerHTML = '<div class="text-muted s-287f77">Loading tasks…</div>';
  const res = await apiFetch(`/api/v1/pim/tasks?${pimTaskQuery()}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to load tasks.'); host.innerHTML = ''; return; }
  const result = await res.json();
  pimMyWorkState.lastResult = result;
  renderPIMTaskTable(result);
  updatePIMTaskBulkBar();
}

// 284.2 (user 2026-11-26, "one page, one job"): my task counts are cards on the
// PIM home, not tiles above the My work list. The status counts are the
// server's whole-set tally; overdue is the total of an only-overdue query, so
// both count every task, not one page of them. A card opens My work filtered.
async function renderPIMDashboardTaskCards(grid) {
  const [allRes, overdueRes] = await Promise.all([
    apiFetch('/api/v1/pim/tasks?assignee=me&limit=1'),
    apiFetch('/api/v1/pim/tasks?assignee=me&only_overdue=1&limit=1')
  ]);
  if (!grid || !allRes || !allRes.ok) return; // no task access: no task cards
  const tally = (await allRes.json()).status_tally || {};
  const overdue = overdueRes && overdueRes.ok ? ((await overdueRes.json()).total || 0) : 0;
  const cards = [
    ['Open', 'My open tasks', tally['Open'] || 0],
    ['In Progress', 'My tasks in progress', tally['In Progress'] || 0],
    ['Blocked', 'My blocked tasks', tally['Blocked'] || 0],
    ['overdue', 'My overdue tasks', overdue]
  ];
  grid.insertAdjacentHTML('beforeend', cards.map(([key, label, count]) => `<button class="table-panel pim-dashboard-card s-d5a21b" data-pim-task-card="${key}"><div class="text-muted s-6cb285">${label}</div><div class="s-de7088">${count}</div></button>`).join(''));
  grid.querySelectorAll('[data-pim-task-card]').forEach(card => card.addEventListener('click', () => {
    const key = card.dataset.pimTaskCard;
    Object.assign(pimMyWorkState.filter, { assignee: 'me', status: key === 'overdue' ? '' : key, task_type: '', priority: '', only_overdue: key === 'overdue' });
    setActiveMenu('menu-pim-my-work');
    renderView('pim:my-work');
  }));
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
    host.innerHTML = `<p class="text-muted s-b8127d">No tasks match this filter.</p>`;
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
        <div class="s-eed0f8">${escapeHTMLText(task.title)}</div>
        <div class="text-muted s-6cb285">${escapeHTMLText(task.task_type || '')}${task.stage ? ' · stage ' + escapeHTMLText(task.stage) : ''}${task.comments && task.comments.length ? ' · ' + task.comments.length + ' comment(s)' : ''}</div>
      </td>
      <td>${task.item_code ? escapeHTMLText(task.item_code) + (task.item_name ? `<div class="text-muted s-6cb285">${escapeHTMLText(task.item_name)}</div>` : '') : '<span class="text-muted">—</span>'}</td>
      <td>${escapeHTMLText(task.assignee || '(unassigned)')}</td>
      <td>${due}</td>
      <td>${escapeHTMLText(task.priority || 'Normal')}</td>
      <td>${pimTaskStatusBadge(task)}</td>
      <td class="s-86abda">
        ${canProgress && task.status !== 'In Progress' ? `<button class="action-btn" data-task-act="start" data-task="${escapeHTMLText(task.id)}">Start</button>` : ''}
        ${canProgress ? `<button class="action-btn" data-task-act="done" data-task="${escapeHTMLText(task.id)}">Done</button>` : ''}
        <button class="action-btn" data-task-act="open" data-task="${escapeHTMLText(task.id)}">Details</button>
      </td>
    </tr>`;
  }).join('');

  host.innerHTML = `<table>
    <thead><tr>
      <th class="s-82b9d8"><input type="checkbox" id="pim-task-select-all"></th>
      <th>Task</th><th>Product</th><th>Assignee</th><th>Due</th><th>Priority</th><th>Status</th><th>Actions</th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>
  <p class="text-muted s-60db2d">Showing ${tasks.length} of ${result.total} task(s).</p>`;

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
        <p class="s-759e41">${pimTaskStatusBadge(task)} <span class="text-muted s-5e0faa">${escapeHTMLText(task.task_type || '')} · ${escapeHTMLText(task.scope_type || '')} ${escapeHTMLText(task.scope_ref || '')}</span></p>
        ${task.instructions ? `<p class="s-5e0faa">${escapeHTMLText(task.instructions)}</p>` : ''}
        <dl class="s-49e729">
          <dt class="text-muted">Product</dt><dd class="s-116966">${escapeHTMLText(task.item_code || '—')} ${escapeHTMLText(task.item_name || '')}</dd>
          <dt class="text-muted">Assignee</dt><dd class="s-116966">${escapeHTMLText(task.assignee || '(unassigned)')}</dd>
          <dt class="text-muted">Due</dt><dd class="s-116966">${escapeHTMLText(task.due_date || '—')}${task.overdue ? ' <span class="badge badge-danger">overdue</span>' : ''}</dd>
          ${task.workflow_run ? `<dt class="text-muted">Workflow run</dt><dd class="s-116966">${escapeHTMLText(task.workflow_run)} · stage ${escapeHTMLText(task.stage || '')}</dd>` : ''}
          ${task.completed_at ? `<dt class="text-muted">Completed</dt><dd class="s-116966">${escapeHTMLText(task.completed_at)} by ${escapeHTMLText(task.completed_by || '')}</dd>` : ''}
        </dl>
        <h4 class="s-84a09e">Comments</h4>
        <div id="pim-task-comments" class="s-354c12">
          ${(task.comments || []).length === 0 ? '<p class="text-muted s-116966">No comments yet.</p>' :
            task.comments.map(c => `<p class="s-df6718"><strong>${escapeHTMLText(c.author)}</strong> <span class="text-muted">${escapeHTMLText((c.at || '').slice(0, 16).replace('T', ' '))}</span><br>${escapeHTMLText(c.comment)}</p>`).join('')}
        </div>
        <div class="form-group s-56f435">
          <label class="form-label" for="pim-task-comment-input">Add a comment</label>
          <input class="form-input" id="pim-task-comment-input" placeholder="What changed, or what is blocking this?">
        </div>
      </div>
      <div class="modal-footer s-f854f4">
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
  host.innerHTML = `<p class="s-06ed28"><strong>${result.created_count}</strong> task(s) created${result.skipped_count > 0 ? `, <strong>${result.skipped_count}</strong> skipped (they already have an open task from this template)` : ''}.</p>`;
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
  if (!res || !res.ok) { host.innerHTML = '<p class="text-muted s-cc781b">Could not load workflow runs.</p>'; return; }
  const runs = (await res.json()).runs || [];
  if (runs.length === 0) {
    host.innerHTML = '<p class="text-muted s-cc781b">No workflow runs in this state.</p>';
    return;
  }
  const mayChange = canDo('update', 'PIMWorkflowRun'); // 319.1: the server's right for Advance/Pause/Resume/Cancel
  host.innerHTML = `<table>
    <thead><tr><th>Product</th><th>Workflow</th><th>Stage</th><th>Status</th><th>Waiting on</th><th>Tasks</th><th>Actions</th></tr></thead>
    <tbody>${runs.map(run => `<tr>
      <td>${escapeHTMLText(run.item_code)}${run.item_name ? `<div class="text-muted s-6cb285">${escapeHTMLText(run.item_name)}</div>` : ''}</td>
      <td>${escapeHTMLText(run.workflow_name || run.workflow)}</td>
      <td>${escapeHTMLText(run.current_stage || '—')}<div class="text-muted s-6cb285">${escapeHTMLText(run.stage_progress || '')}</div></td>
      <td><span class="badge ${run.status === 'Completed' ? 'badge-success' : run.status === 'Cancelled' ? 'badge-secondary' : run.status === 'Paused' ? 'badge-warning' : 'badge-secondary'}">${escapeHTMLText(run.status)}</span></td>
      <td class="s-6cb285">${escapeHTMLText(run.blocked_reason || '—')}</td>
      <td>${run.open_tasks} open / ${run.total_tasks}</td>
      <td class="s-86abda">
        ${mayChange && run.status === 'Running' ? `<button class="action-btn" data-run-act="advance" data-run="${escapeHTMLText(run.id)}">Advance</button>
        <button class="action-btn" data-run-act="pause" data-run="${escapeHTMLText(run.id)}">Pause</button>` : ''}
        ${mayChange && run.status === 'Paused' ? `<button class="action-btn" data-run-act="resume" data-run="${escapeHTMLText(run.id)}">Resume</button>` : ''}
        ${mayChange && (run.status === 'Running' || run.status === 'Paused') ? `<button class="action-btn" data-run-act="cancel" data-run="${escapeHTMLText(run.id)}">Cancel</button>` : ''}
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
        <p class="text-muted s-642b44">On <strong>${escapeHTMLText(itemCode)}</strong>${contextLabel ? `, from ${escapeHTMLText(contextLabel)}` : ''}.</p>
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
  panel.innerHTML = `<div class="table-controls"><div class="form-group s-116966"><label class="form-label" for="pim-report-name">Report</label><select class="form-select" id="pim-report-name"><option value="content-aging">Content aging</option><option value="duplicate-media">Duplicate media</option><option value="channel-mapping-gap">Channel mapping gaps</option><option value="attribute-quality">Attribute quality</option><option value="media-expiry">Media expiry</option><option value="content-sla-breach">Content SLA breaches</option></select></div><button class="btn btn-primary" id="pim-report-run">Run report</button><button class="btn btn-outline" id="pim-search-feed-export">Download Search Feed (CSV)</button></div><div class="table-wrapper s-1b0f49" id="pim-report-results"></div>`;
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
  (await loadViewModule('/view-market-harvest.js' + new URL(import.meta.url).search)).renderMarketHarvest(container);
  await run();
}

let pimSkuPaste = ''; // 146.5.13: pasted item codes narrowing the workbench

async function renderPIMWorkbenchTab(container) {
  const familiesRes = await apiFetch('/api/v1/doc/ProductFamily');
  const families = familiesRes && familiesRes.ok ? await familiesRes.json() : [];

  const filterPanel = document.createElement('div');
  filterPanel.className = 'table-panel';
  filterPanel.style.padding = '16px 24px';
  filterPanel.style.marginBottom = '16px';
  filterPanel.style.minHeight = '0';
  filterPanel.innerHTML = `
    <div class="s-ab84b5">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-family-filter">Family</label>
        <select id="pim-family-filter" class="form-input s-30080b">
          <option value="">All families</option>
          ${families.map(f => `<option value="${escapeHTMLText(f.code || f.id)}" ${(f.code || f.id) === currentPIMFamilyFilter ? 'selected' : ''}>${escapeHTMLText(f.name || f.code || f.id)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-sku-paste">Item codes</label>
        <input id="pim-sku-paste" class="form-input s-5341bb" value="${escapeHTMLText(pimSkuPaste)}" placeholder="Paste codes">
      </div>
    </div>
  `;
  container.appendChild(filterPanel);
  filterPanel.querySelector('#pim-sku-paste').addEventListener('change', (e) => {
    pimSkuPaste = e.target.value.trim();
    currentPIMSelectedItem = '';
    renderView('pim');
  });
  filterPanel.querySelector('#pim-family-filter').addEventListener('change', (e) => {
    currentPIMFamilyFilter = e.target.value;
    currentPIMSelectedItem = '';
    renderView('pim');
  });

  const wbParams = new URLSearchParams();
  if (currentPIMFamilyFilter) wbParams.set('family', currentPIMFamilyFilter);
  if (pimSkuPaste) wbParams.set('skus', pimSkuPaste);
  const query = wbParams.toString() ? `?${wbParams}` : '';
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
    ? `<tr><td colspan="6" class="s-306094">No items found. Create one under Setup &raquo; Item.</td></tr>`
    : entries.map(e => {
        const badgeClass = e.score >= 80 ? 'badge-success' : e.score >= 40 ? 'badge-warning' : 'badge-danger';
        // 235.1: the Item cell keeps its code (data-link-swept); Name beside it shows the name.
        return `
          <tr class="pim-workbench-row s-74fa97" data-item="${escapeHTMLText(e.item_code)}">
            <td class="s-355794" data-link-swept="1">${escapeHTMLText(e.item_code)}</td>
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
    <h2 class="s-79bd22"><span data-link-ref="${escapeHTMLText(itemCode)}" data-link-doctype="Item">${escapeHTMLText(itemCode)}</span> - Completeness: ${escapeHTMLText(comp.score)}% <span class="badge badge-secondary s-8ce609">${escapeHTMLText(comp.enrichment_status || '')}</span></h2>
    <div class="pim-missing-fields s-87c136">
      <strong>What to fix next</strong>
      ${comp.missing_fields && comp.missing_fields.length > 0
        ? `<ul class="s-6c5e8c">${comp.missing_fields.map(field => `<li>${escapeHTMLText(field)} — ${pimCorrectionMarkup(field, attrDefs)}</li>`).join('')}</ul>`
        : '<p class="s-d3925e">Core product is complete. Check a channel below before publishing.</p>'}
    </div>

    <h3 class="s-3ad76c">Add / Update Attribute Value</h3>
    <div class="s-00f3c5">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-attr-select">Attribute</label>
        <select id="pim-attr-select" class="form-input s-0fff9b">
          <option value="">Select attribute</option>
          ${attrDefs.map(a => `<option value="${escapeHTMLText(a.code || a.id)}" data-value-type="${escapeHTMLText(a.value_type || 'Data')}" data-value-options="${escapeHTMLText(a.value_options || '')}" data-uom="${escapeHTMLText(a.uom || '')}">${escapeHTMLText(a.label || a.code || a.id)}</option>`).join('')}
        </select>
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-attr-value">Value</label>
        <input type="text" id="pim-attr-value" class="form-input s-0fff9b">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-attr-locale" title="Leave blank to set the global default value">Locale Override</label>
        <input type="text" id="pim-attr-locale" class="form-input s-da190a" placeholder="e.g. fr">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-attr-channel" title="Leave blank to set the global default value">Channel Override</label>
        <select id="pim-attr-channel" class="form-input s-56cae7">
          <option value="">All channels</option>
          ${channelsForOverride.map(c => `<option value="${escapeHTMLText(c.code || c.id)}">${escapeHTMLText(c.name || c.code || c.id)}</option>`).join('')}
        </select>
      </div>
      <button class="btn btn-primary" id="pim-attr-save-btn">Save</button>
    </div>
    <div id="pim-attr-error" class="login-error hidden s-29b798"></div>

    <div class="s-138ab9">
      <h3 class="s-95d910">Content</h3>
      <button class="btn btn-outline btn-sm" id="pim-content-assist-btn" title="Draft title, descriptions and tags from this product's own stored data. Nothing is saved until you click Save Draft.">Assist</button>
      <span class="text-muted" id="pim-content-current" role="status">Loading saved content…</span>
    </div>
    <div id="pim-content-assist-note" class="hidden s-a0ff6d"></div>
    <div class="s-608a43">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-lang">Language</label>
        <input type="text" id="pim-content-lang" class="form-input s-34deaa" value="en">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-shape" title="Marketplace shapes the title and adds bullet points plus a meta description - the format most marketplace listing pages use, versus a storefront page's single flowing description.">Assist Shape</label>
        <select id="pim-content-shape" class="form-input s-21b277">
          <option value="standard">Standard</option>
          <option value="marketplace">Marketplace</option>
        </select>
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-title">Title</label>
        <input type="text" id="pim-content-title" class="form-input s-30080b">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-short">Short Description</label>
        <input type="text" id="pim-content-short" class="form-input s-5341bb">
      </div>
    </div>
    <div class="s-cdbe80">
      <div class="form-group s-121ddc">
        <label class="form-label" for="pim-content-long">Long Description</label>
        <textarea id="pim-content-long" class="form-input s-16000c" rows="3"></textarea>
      </div>
    </div>
    <div class="s-cdbe80">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-seo">SEO Title</label>
        <input type="text" id="pim-content-seo" class="form-input s-30080b">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-tags">Tags</label>
        <input type="text" id="pim-content-tags" class="form-input s-30080b">
      </div>
    </div>
    <div class="s-cdbe80">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-owner">Owner (username)</label>
        <input type="text" id="pim-content-owner" class="form-input s-56cae7">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-content-sla">SLA Due Date</label>
        <input type="date" id="pim-content-sla" class="form-input s-56cae7">
      </div>
      <button class="btn btn-outline" id="pim-content-save-btn">Save Draft</button>
      <button class="btn btn-primary" id="pim-content-submit-btn">Submit for Approval</button>
    </div>
    <div id="pim-content-error" class="login-error hidden s-4c0544"></div>
    <div id="pim-content-history" class="s-4c0544"></div>

    <h3 class="s-1a80e4">Media</h3>
    <div class="s-cade60">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-media-file">File (jpg/png/webp/gif/pdf)</label>
        <input type="file" id="pim-media-file" class="form-input" accept=".jpg,.jpeg,.png,.webp,.gif,.pdf">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-media-role">Role</label>
        <select id="pim-media-role" class="form-input s-56cae7">
          <option>Main Image</option>
          <option>Gallery</option>
          <option>Variant Image</option>
          <option>Lifestyle</option>
          <option>Certificate</option>
          <option>Internal QC</option>
          <option>Video/Other</option>
        </select>
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-media-alt">Alt Text</label>
        <input type="text" id="pim-media-alt" class="form-input s-f9de92">
      </div>
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-media-expiry">Expiry Date</label>
        <input type="date" id="pim-media-expiry" class="form-input s-56cae7">
      </div>
      <button class="btn btn-primary" id="pim-media-upload-btn">Upload</button>
    </div>
    <div id="pim-media-error" class="login-error hidden s-5beb8f"></div>
    <div id="pim-media-gallery" class="s-beb40d"></div>

    <h3 class="s-3ad76c">Channel Publishing</h3>
    <div class="s-cade60">
      <div class="form-group s-50435f">
        <label class="form-label" for="pim-publish-channel">Channel</label>
        <select id="pim-publish-channel" class="form-input s-0fff9b"><option value="">Loading...</option></select>
      </div>
      <button class="btn btn-outline" id="pim-publish-preview-btn">Preview</button>
      <button class="btn btn-primary" id="pim-publish-btn" disabled>Publish</button>
    </div>
    <div id="pim-publish-readiness" role="status" class="s-da12f2">Checking channel requirements…</div>
    <div id="pim-publish-error" class="login-error hidden s-5beb8f"></div>
    <div id="pim-publish-preview" class="s-5beb8f"></div>
    <div id="pim-publish-log"></div>

    <h3 class="s-1a80e4">Related Products</h3>
    <div id="pim-related-products" class="text-muted">Loading&hellip;</div>
  `;
  container.appendChild(panel);

  panel.querySelectorAll('[data-pim-correction]').forEach(button => button.addEventListener('click', () => {
    const target = button.getAttribute('data-pim-correction');
    if (target === 'item') {
      window.location.hash = '#/setup/Item';
    } else if (target === 'attribute') {
      const select = panel.querySelector('#pim-attr-select');
      const option = [...select.options].find(candidate => candidate.textContent === button.getAttribute('data-pim-field'));
      if (option) select.value = option.value;
      panel.querySelector('#pim-attr-value').focus();
    } else if (target === 'content') {
      panel.querySelector('#pim-content-title').focus();
    }
  }));

  // Stage 127.1: a Number attribute takes a number, a Date one a date.
  document.getElementById('pim-attr-select').addEventListener('change', (e) => {
    const o = e.target.selectedOptions[0];
    pimSwapValueControl((o && o.dataset.valueType) || 'Data', (o && o.dataset.valueOptions) || '');
  });
  try { await (await loadViewModule('/view-pim-category-rules.js' + new URL(import.meta.url).search)).applyItemRules(panel, itemCode, attrDefs); } catch (e) { console.error('category rules', e); }
  document.getElementById('pim-attr-save-btn').addEventListener('click', () => savePIMAttributeValue(itemCode));
  document.getElementById('pim-content-assist-btn').addEventListener('click', () => runPIMContentAssist(itemCode));
  document.getElementById('pim-content-save-btn').addEventListener('click', () => savePIMContent(itemCode, 'Draft'));
  document.getElementById('pim-content-submit-btn').addEventListener('click', () => submitPIMContent(itemCode));
  document.getElementById('pim-content-lang').addEventListener('change', async () => {
    await loadPIMContentDraft(itemCode);
    await renderPIMContentHistory(itemCode);
  });
  document.getElementById('pim-media-upload-btn').addEventListener('click', () => uploadPIMMedia(itemCode));
  document.getElementById('pim-publish-btn').addEventListener('click', () => publishPIMItem(itemCode));
  document.getElementById('pim-publish-preview-btn').addEventListener('click', () => previewPIMPublish(itemCode));

  await loadPIMContentDraft(itemCode);
  await renderPIMMediaGallery(itemCode);
  await renderPIMPublishSection(itemCode);
  await renderPIMContentHistory(itemCode);
  await renderPIMRelatedProducts(itemCode);
  // 146.5.12 / 146.5.13: typed relations and "Live on" links (lazy module)
  const extras = document.createElement('div');
  container.appendChild(extras);
  const x = await loadViewModule('/view-pim-extras.js' + new URL(import.meta.url).search);
  await x.renderPIMItemExtras(extras, itemCode);
}

function pimCorrectionMarkup(field, attrDefs) {
  const escaped = escapeHTMLText(field);
  if (field.startsWith('Approved Content')) {
    return '<button type="button" class="btn btn-link btn-sm" data-pim-correction="content">Complete content and submit it for approval below</button>';
  }
  if (['name', 'barcode', 'category', 'hsn_code', 'gst_rate', 'Product Family'].includes(field)) {
    return '<button type="button" class="btn btn-link btn-sm" data-pim-correction="item">Edit this in Setup → Item</button>';
  }
  if (attrDefs.some(def => (def.label || def.code || def.id) === field)) {
    return `<button type="button" class="btn btn-link btn-sm" data-pim-correction="attribute" data-pim-field="${escaped}">Enter its value below</button>`;
  }
  return 'Review the product family or channel setup.';
}

// Keep template authoring in the generic table, but make its existing
// mapping/preview/import endpoints usable from the template row itself.
async function openPIMImportTemplateRun(templateID) {
  document.getElementById('pim-import-run-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.id = 'pim-import-run-modal';
  overlay.className = 'modal-overlay open';
  overlay.setAttribute('role', 'dialog');
  overlay.setAttribute('aria-modal', 'true');
  overlay.setAttribute('aria-label', 'Preview and import catalog CSV');
  overlay.innerHTML = `<div class="modal-container s-09a5de">
    <div class="modal-header"><h3 class="modal-title">Preview and import catalog CSV</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
    <div class="modal-body">
      <p>Template: <strong>${escapeHTMLText(templateID)}</strong></p>
      <div id="pim-import-mapping" role="status">Loading field mapping…</div>
      <div class="form-group s-1b0f49"><label class="form-label" for="pim-import-file">CSV file</label><input class="form-input" type="file" id="pim-import-file" accept=".csv,text/csv"></div>
      <div id="pim-import-result" role="status"></div>
    </div>
    <div class="modal-footer"><button type="button" class="btn btn-secondary" id="pim-import-close">Close</button><button type="button" class="btn btn-outline" id="pim-import-preview">Preview (no changes)</button><button type="button" class="btn btn-primary" id="pim-import-commit" disabled>Import validated file</button></div>
  </div>`;
  document.body.appendChild(overlay);
  const fileInput = overlay.querySelector('#pim-import-file');
  const previewButton = overlay.querySelector('#pim-import-preview');
  const commitButton = overlay.querySelector('#pim-import-commit');
  const resultEl = overlay.querySelector('#pim-import-result');
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-import-close').addEventListener('click', close);
  overlay.addEventListener('click', event => { if (event.target === overlay) close(); });
  fileInput.addEventListener('change', () => { commitButton.disabled = true; resultEl.textContent = 'Preview this file before importing.'; });
  const mappingRes = await apiFetch(`/api/v1/pim/import-templates/${encodeURIComponent(templateID)}/preview-mapping`);
  if (!overlay.isConnected) return;
  const mappingEl = overlay.querySelector('#pim-import-mapping');
  if (!mappingRes || !mappingRes.ok) {
    mappingEl.textContent = 'Field mapping could not be loaded. Refresh the template and try again.';
    previewButton.disabled = true;
    return;
  }
  const mapping = await mappingRes.json();
  const rows = mapping.mappings || [];
  mappingEl.innerHTML = `<p class="s-f5554e">Target: <strong>${escapeHTMLText(mapping.target_doctype)}</strong></p><div class="table-wrapper"><table><thead><tr><th>CSV column</th><th>ERP field</th><th>Transform</th><th>Default</th></tr></thead><tbody>${rows.map(row => `<tr><td>${escapeHTMLText(row.source_column)}</td><td>${row.target_label ? escapeHTMLText(row.target_label) : `<span class="badge badge-danger">Unknown: ${escapeHTMLText(row.target_field)}</span>`}</td><td>${escapeHTMLText(row.transform_rule || '')}</td><td>${escapeHTMLText(row.default_value || '')}</td></tr>`).join('')}</tbody></table></div>`;
  if (rows.length === 0 || rows.some(row => !row.target_label)) {
    previewButton.disabled = true;
    resultEl.textContent = 'Repair the template mapping before uploading a file.';
    return;
  }
  const run = async dryRun => {
    const file = fileInput.files[0];
    if (!file) { resultEl.textContent = 'Choose a CSV file first.'; return; }
    previewButton.disabled = true;
    commitButton.disabled = true;
    resultEl.textContent = dryRun ? 'Validating file without writing…' : 'Importing validated file…';
    const body = new FormData(); body.append('file', file);
    const action = dryRun ? 'preview' : 'import';
    const res = await apiUpload(`/api/v1/pim/import-templates/${encodeURIComponent(templateID)}/${action}`, body);
    if (!overlay.isConnected) return;
    previewButton.disabled = false;
    if (!res || !res.ok) {
      const error = res ? await res.json().catch(() => ({})) : {};
      resultEl.textContent = error.error || 'File validation failed. Correct the CSV and preview again.';
      return;
    }
    const outcome = await res.json();
    const errors = outcome.errors || [];
    resultEl.innerHTML = `<p><strong>${dryRun ? 'Preview only — nothing written' : 'Import completed'}</strong>: ${escapeHTMLText(outcome.total_rows)} row(s), ${escapeHTMLText(outcome.created_ids?.length || 0)} created, ${escapeHTMLText(outcome.updated_ids?.length || 0)} updated, ${escapeHTMLText(outcome.failed_rows || 0)} failed.</p>${errors.length ? `<ul>${errors.slice(0,20).map(error => `<li>Row ${escapeHTMLText(error.row_number)}: ${escapeHTMLText(error.message)}</li>`).join('')}</ul>${errors.length > 20 ? '<p>More error rows are in the job download.</p>' : ''}` : ''}`;
    if (dryRun) {
      commitButton.disabled = outcome.total_rows < 1 || outcome.failed_rows > 0;
      if (outcome.failed_rows > 0) resultEl.insertAdjacentHTML('beforeend', '<p>Correct the file and preview it again. No rows were imported.</p>');
    } else {
      fileInput.value = '';
      if (outcome.import_job_id) {
        const download = document.createElement('button');
        download.type = 'button'; download.className = 'btn btn-outline btn-sm';
        download.textContent = `Download job ${outcome.import_job_id} error rows`;
        download.addEventListener('click', async () => {
          const response = await apiFetch(`/api/v1/pim/import-jobs/${encodeURIComponent(outcome.import_job_id)}/errors.csv`);
          if (!response || !response.ok) { await showApiError(response, 'Could not download error rows.'); return; }
          const url = URL.createObjectURL(await response.blob());
          const link = document.createElement('a'); link.href = url; link.download = `pim_import_${outcome.import_job_id}_errors.csv`; link.click(); URL.revokeObjectURL(url);
        });
        resultEl.appendChild(download);
      }
      // Stage 55.11: a partly failed job resumes from its failed rows only.
      if (outcome.import_job_id && outcome.failed_rows > 0) {
        const resume = document.createElement('button');
        resume.type = 'button'; resume.className = 'btn btn-primary btn-sm'; resume.style.marginLeft = '8px';
        resume.textContent = `Fix and resume the ${outcome.failed_rows} failed row(s)`;
        resume.addEventListener('click', async () => {
          const jobs = await loadViewModule('/view-import-jobs.js' + new URL(import.meta.url).search);
          jobs.openImportResume(outcome.import_job_id);
        });
        resultEl.appendChild(resume);
      }
    }
  };
  previewButton.addEventListener('click', () => run(true));
  commitButton.addEventListener('click', () => run(false));
  fileInput.focus();
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
    el.innerHTML = '<span class="text-muted">No related products found.</span>';
    return;
  }
  el.innerHTML = `
    <table>
      <thead><tr><th>Item</th><th>Name</th><th>Shared Attributes</th></tr></thead>
      <tbody>
        ${related.map(r => `
          <tr class="pim-related-row s-3b6a3a" data-item="${escapeHTMLText(r.item_code)}">
            <td class="s-355794">${escapeHTMLText(r.item_code)}</td>
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
    ? `<div class="s-1cf295">
         ${bullets ? `<div><strong>Bullet points:</strong></div><ul class="s-28850b">${bullets}</ul>` : ''}
         ${draft.meta_description ? `<div class="s-a5595d"><strong>Meta description:</strong> ${escapeHTMLText(draft.meta_description)}</div>` : ''}
       </div>`
    : '';
  noteEl.innerHTML = `
    <strong>${filled} field${filled === 1 ? '' : 's'} filled from this product's own data.</strong>
    Nothing has been saved yet - review the text, then use Save Draft or Submit for Approval.
    <div class="s-a5595d">Built from: ${escapeHTMLText(sources)}</div>
    ${warnings ? `<ul class="s-f45d9b">${warnings}</ul>` : ''}
    ${marketplaceExtra}`;
  noteEl.classList.remove('hidden');
}

async function renderPIMMediaGallery(itemCode) {
  const gallery = document.getElementById('pim-media-gallery');
  if (!gallery) return;
  const res = await apiFetch(`/api/v1/pim/media?item=${encodeURIComponent(itemCode)}`);
  const media = res && res.ok ? await res.json() : [];

  if (media.length === 0) {
    gallery.innerHTML = `<div class="s-4b3c6e">No media uploaded yet.</div>`;
    return;
  }

  gallery.innerHTML = media.map(m => `
    <div class="table-panel s-53e79d" data-media-card="${escapeHTMLText(m.id)}">
      <div class="s-d8d01b">${escapeHTMLText(m.media_role)} <span class="text-muted">v${escapeHTMLText(m.version_no || 1)}</span></div>
      <img data-media-thumb="${escapeHTMLText(m.id)}" class="s-9e6a15" alt="${escapeHTMLText(m.alt_text || m.media_role)}">
      <div class="text-muted s-57f567">${escapeHTMLText(m.alt_text || 'No alt text')}${m.expiry_date ? ` · expires ${escapeHTMLText(m.expiry_date)}` : ''}</div>
      <button class="btn btn-outline btn-sm s-e63a63" data-edit-media="${escapeHTMLText(m.id)}" data-alt="${escapeHTMLText(m.alt_text || '')}" data-expiry="${escapeHTMLText(m.expiry_date || '')}">Edit Alt/Expiry</button>
      <button class="btn btn-outline btn-sm s-13648a" data-deactivate-media="${escapeHTMLText(m.id)}">Deactivate</button>
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
    : channels.map(c => `<option value="${escapeHTMLText(c.code || c.id)}">${escapeHTMLText(c.name || c.code || c.id)}</option>`).join('');

  select.addEventListener('change', () => checkPIMPublishReadiness(itemCode));
  await checkPIMPublishReadiness(itemCode);

  const logRes = await apiFetch(`/api/v1/pim/publish-log?item=${encodeURIComponent(itemCode)}`);
  const log = logRes && logRes.ok ? await logRes.json() : [];
  logEl.innerHTML = log.length === 0
    ? `<div class="s-4b3c6e">No publish attempts yet.</div>`
    // error_code (Stage 26.4.8: marketplace error dictionary) lets a failed
    // attempt be triaged at a glance (missing credential vs. duplicate SKU
    // vs. a blank required field) instead of only reading the raw message.
    : `<table><thead><tr><th>Channel</th><th>Status</th><th>External ID</th><th>Error Code</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${escapeHTMLText(l.channel_code)}</td><td><span class="badge ${l.status === 'Published' ? 'badge-success' : 'badge-danger'}">${escapeHTMLText(l.status)}</span></td><td class="s-355794">${escapeHTMLText(l.external_id || '')}</td><td class="s-355794">${escapeHTMLText(l.error_code || '')}</td><td>${escapeHTMLText(l.created_at || '')}</td></tr>`).join('')
      }</tbody></table>`;
}

async function checkPIMPublishReadiness(itemCode) {
  const select = document.getElementById('pim-publish-channel');
  const status = document.getElementById('pim-publish-readiness');
  const publish = document.getElementById('pim-publish-btn');
  if (!select || !status || !publish) return;
  const channel = select.value;
  publish.disabled = true;
  if (!channel) {
    status.textContent = 'Set up a channel under Channels & publishing to check readiness.';
    return;
  }
  status.textContent = 'Checking channel requirements…';
  const res = await apiFetch(`/api/v1/pim/completeness/${encodeURIComponent(itemCode)}?channel=${encodeURIComponent(channel)}`);
  if (select.value !== channel || currentPIMSelectedItem !== itemCode) return;
  if (!res || !res.ok) {
    status.textContent = 'Readiness could not be checked. Check your connection or channel setup and retry by selecting the channel again.';
    return;
  }
  const result = await res.json();
  const readiness = result.publish_readiness;
  if (!readiness) {
    status.textContent = 'Readiness information is unavailable; publishing is paused.';
    return;
  }
  publish.disabled = !readiness.ready;
  if (readiness.ready) {
    status.textContent = `Ready to publish to ${select.selectedOptions[0]?.textContent || channel}. This queues a local publish job; an external marketplace receipt must be checked separately.`;
  } else {
    const issues = readiness.missing_fields || [];
    status.innerHTML = `<strong>Not ready for ${escapeHTMLText(select.selectedOptions[0]?.textContent || channel)}:</strong><ul class="s-6c5e8c">${issues.map(issue => `<li>${escapeHTMLText(issue)} ${pimReadinessCorrectionMarkup(issue)}</li>`).join('')}</ul>`;
    status.querySelectorAll('[data-pim-ready-action]').forEach(button => button.addEventListener('click', () => {
      const target = button.getAttribute('data-pim-ready-action');
      if (target === 'item') { window.location.hash = '#/setup/Item'; return; }
      if (target === 'category') { openPIMTab(PIM_TABS.find(tab => tab.id === 'channel-category-map')); return; }
      const field = document.getElementById(`pim-${target}`);
      if (field) { field.focus(); field.scrollIntoView({ block: 'center' }); }
    }));
  }
}

function pimReadinessCorrectionMarkup(issue) {
  let action = '', label = '';
  if (issue.startsWith('category mapping')) { action = 'category'; label = 'Open Category Mapping'; }
  else if (issue.startsWith('Required Tag')) { action = 'content-tags'; label = 'Edit tags'; }
  else if (issue.startsWith('Max Title Length')) { action = 'content-title'; label = 'Edit title'; }
  else if (issue.startsWith('Min Images') || issue.toLowerCase().includes('primary image')) { action = 'media-file'; label = 'Add an image'; }
  else if (issue.startsWith('Approved Content')) { action = 'content-title'; label = 'Submit content for approval'; }
  else if (['name', 'barcode', 'category', 'hsn_code', 'gst_rate', 'Product Family'].includes(issue)) { action = 'item'; label = 'Edit Item'; }
  if (!action) return '';
  return `<button type="button" class="btn btn-link btn-sm" data-pim-ready-action="${action}">${label}</button>`;
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
    <h4 class="s-db5726">${preview.has_prior_snapshot ? 'Diff vs. last publish attempt' : 'Outbound payload'}</h4>
    <table><thead><tr><th>Field</th><th>Previously Published</th><th>About to Publish</th></tr></thead><tbody>${
      (preview.fields || []).map(f => `<tr class="${f.changed ? 'u-fw6' : ''}"><td>${escapeHTMLText(f.field)}</td><td>${escapeHTMLText(f.old || '')}</td><td>${escapeHTMLText(f.new || '')}</td></tr>`).join('')
    }</tbody></table>
  `;
}

async function loadPIMContentDraft(itemCode) {
  const language = document.getElementById('pim-content-lang')?.value.trim() || 'en';
  const status = document.getElementById('pim-content-current');
  if (!status) return;
  const saveButton = document.getElementById('pim-content-save-btn');
  const submitButton = document.getElementById('pim-content-submit-btn');
  saveButton.disabled = true;
  submitButton.disabled = true;
  status.textContent = 'Loading saved content…';
  const contentID = `${itemCode}::${language}`;
  const res = await apiFetch(`/api/v1/doc/ProductContent/${encodeURIComponent(contentID)}`);
  if (currentPIMSelectedItem !== itemCode || document.getElementById('pim-content-lang')?.value.trim() !== language) return;
  if (!res) { status.textContent = 'Could not load saved content. Retry by reselecting the language.'; return; }
  if (!res.ok && res.status !== 404) { status.textContent = 'Could not load saved content. Do not overwrite it until this is resolved.'; return; }
  const saved = res.ok ? await res.json() : {};
  for (const [field, key] of [['title', 'title'], ['short', 'short_desc'], ['long', 'long_desc'], ['seo', 'seo_title'], ['tags', 'tags'], ['owner', 'owner'], ['sla', 'sla_due_date']]) {
    const input = document.getElementById(`pim-content-${field}`);
    if (input) input.value = saved[key] || '';
  }
  saveButton.disabled = false;
  submitButton.disabled = false;
  status.textContent = res.ok ? `Saved content: ${saved.status || 'Draft'}` : 'No saved content for this language yet.';
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
    ? `<div class="text-muted s-5e0faa">No approval history yet.</div>`
    : `<table><thead><tr><th>Action</th><th>By</th><th>Comment</th><th>When</th></tr></thead><tbody>${
        log.map(l => `<tr><td>${escapeHTMLText(l.action)}</td><td>${escapeHTMLText(l.actor_user_id)}</td><td>${escapeHTMLText(l.comment || '')}</td><td>${escapeHTMLText(l.created_at || '')}</td></tr>`).join('')
      }</tbody></table>`;

  const versionsHTML = versions.length === 0
    ? `<div class="text-muted s-5e0faa">No approved versions yet.</div>`
    : `<table><thead><tr><th>Version</th><th>Title</th><th>Approved At</th><th></th></tr></thead><tbody>${
        versions.map(v => `<tr><td>${escapeHTMLText(v.version_no)}</td><td>${escapeHTMLText((v.data && v.data.title) || '')}</td><td>${escapeHTMLText(v.created_at || '')}</td><td><button class="btn btn-outline btn-sm" data-rollback-version="${escapeHTMLText(v.id)}">Restore</button></td></tr>`).join('')
      }</tbody></table>`;

  container.innerHTML = `
    <h3 class="s-0c8c0c">Approval History</h3>
    ${logHTML}
    <h3 class="s-55dbab">Approved Versions</h3>
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

// 146.5.11: the value box follows the attribute type.
function pimSwapValueControl(type, options) {
  const old = document.getElementById('pim-attr-value');
  if (!old) return;
  const list = String(options).split(',').map(x => x.trim()).filter(Boolean);
  let el;
  if (type === 'Yes / No' || (type === 'Multi-select' && list.length)) {
    el = document.createElement('select');
    const opts = type === 'Yes / No' ? ['Yes', 'No'] : list;
    el.innerHTML = (type === 'Yes / No' ? '<option value="">-</option>' : '') + opts.map(v => `<option value="${escapeHTMLText(v)}">${escapeHTMLText(v)}</option>`).join('');
    if (type === 'Multi-select') { el.multiple = true; el.size = Math.min(4, opts.length); }
  } else if (type === 'Long text') {
    el = document.createElement('textarea');
    el.rows = 3;
  } else {
    el = document.createElement('input');
    el.type = type === 'Number' ? 'number' : type === 'Date' ? 'date' : type === 'URL' ? 'url' : 'text';
  }
  el.id = 'pim-attr-value';
  el.className = old.className;
  el.style.cssText = old.style.cssText;
  old.replaceWith(el);
}

async function savePIMAttributeValue(itemCode) {
  const errorEl = document.getElementById('pim-attr-error');
  errorEl.classList.add('hidden');

  const attributeId = document.getElementById('pim-attr-select').value;
  const valueEl = document.getElementById('pim-attr-value');
  const value = (valueEl.multiple ? [...valueEl.selectedOptions].map(o => o.value).join(',') : valueEl.value).trim();
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
  if (data.held_fields) await showCustomAlert('Sent for approval. The old value stays until it is approved.');
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

export { renderPIMShellHeader, renderPIMView, renderPIMMediaLibraryTab, renderPIMDashboardTab, pimTaskQuery, renderPIMMyWorkTab, loadPIMAssignableUsers, loadPIMTaskTemplates, loadPIMWorkflowDefinitions, loadPIMTasks, pimTaskStatusBadge, renderPIMTaskTable, renderPIMDetailPanel, openPIMImportTemplateRun, updatePIMTaskBulkBar, runPIMTaskAction, runPIMTaskBulk, openPIMTaskDetail, runPIMTaskTemplate, startPIMWorkflowRun, loadPIMWorkflowRuns, renderPIMReportsTab, renderPIMWorkbenchTab, renderPIMRelatedProducts, runPIMContentAssist, renderPIMMediaGallery, uploadPIMMedia, renderPIMPublishSection, previewPIMPublish, renderPIMContentHistory, publishPIMItem, savePIMAttributeValue, pimContentPayload, savePIMContent, submitPIMContent };
