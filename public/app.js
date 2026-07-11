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
let currentSearchQuery = '';
let currentTablePage = 1;
const itemsPerPage = 10;

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
  
  const response = await fetch(url, {
    ...options,
    headers
  });
  
  if (response.status === 401) {
    await showCustomAlert('Session expired. Please log in again.', 'Unauthorized');
    return null;
  }
  if (response.status === 429) {
    await showCustomAlert('Rate limit exceeded. Please throttle your requests.', 'Rate Limit');
    return null;
  }
  
  return response;
}

// Initializer
async function init() {
  setupEventListeners();
  await fetchLabels();
  await fetchRegisteredDoctypes();
  renderView(currentView);
}

async function fetchLabels() {
  try {
    const res = await apiFetch('/api/v1/labels');
    if (res && res.ok) {
      state.labels = await res.json();
    }
  } catch (err) {
    console.error('Error fetching labels:', err);
  }
}

async function fetchRegisteredDoctypes() {
  try {
    const res = await apiFetch('/api/v1/meta/doctypes');
    if (res && res.ok) {
      state.activeDoctypes = await res.json();
      renderSidebarSubmenu();
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

  ['menu-vendors', 'menu-stores', 'menu-purchase-orders', 'menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-dynamic-labels', 'menu-audit-logs'].forEach(id => {
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
          await showCustomAlert('Industry configuration updated successfully!', 'Success');
          await fetchLabels();
          await fetchRegisteredDoctypes();
          renderView('dashboard');
        } else {
          await showCustomAlert('Failed to switch industry profile.', 'Error');
        }
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

// Router
async function renderView(view) {
  currentView = view;
  const root = document.getElementById('view-root');
  root.innerHTML = ''; 

  if (view === 'dashboard') {
    renderDashboard(root);
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

// Render dynamic DocType CRUD Table view
async function renderDocTableView(container) {
  const metaRes = await apiFetch(`/api/v1/doc/${currentDoctype}/meta`);
  if (!metaRes || !metaRes.ok) return;
  state.activeDocFields = await metaRes.json();

  const dataRes = await apiFetch(`/api/v1/doc/${currentDoctype}`);
  if (!dataRes || !dataRes.ok) return;
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
        <input type="text" placeholder="Search table..." oninput="handleTableSearch(event)">
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
};

window.deleteDocRecord = async function(id) {
  if (await showCustomConfirm('Delete this record?')) {
    const res = await apiFetch(`/api/v1/doc/${currentDoctype}/${id}`, { method: 'DELETE' });
    if (res && res.ok) {
      renderView('doctype-table');
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
      apiFetch(`/api/v1/doc/${f.options}`).then(res => res.json()).then(data => {
        select.innerHTML = '<option value="" disabled selected>— Select Reference —</option>';
        data.forEach(item => {
          select.innerHTML += `<option value="${item.name || item.id}">${item.name || item.code || item.id}</option>`;
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
    const errorData = await res.json();
    await showCustomAlert(`Validation Failed: ${errorData.error}`, 'Validation Failure');
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

window.openNewDoctypeModal = function() {
  const name = prompt('Enter DocType Name:');
  const module = prompt('Enter Module Group (e.g. Master Data, Procurement):');
  const docType = prompt('Document Type (Master/Transaction):');
  
  if (name && module && docType) {
    apiFetch('/api/v1/meta/doctypes', {
      method: 'POST',
      body: JSON.stringify({ name, module, document_type: docType })
    }).then(async res => {
      if (res && res.ok) {
        await fetchRegisteredDoctypes();
        renderView('doctype-builder');
      }
    });
  }
};

window.loadDoctypeConfig = async function(doctypeName) {
  const container = document.getElementById('doctype-fields-config');
  if (!container) return;

  const res = await apiFetch(`/api/v1/doc/${doctypeName}/meta`);
  if (!res || !res.ok) return;
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
  
  apiFetch(`/api/v1/meta/${doctypeName}/fields`, {
    method: 'POST',
    body: JSON.stringify({
      fieldname,
      label,
      fieldtype,
      mandatory,
      options: options || '',
      display_order: 10
    })
  }).then(res => {
    if (res && res.ok) {
      loadDoctypeConfig(doctypeName);
    }
  });
};

window.deleteFieldConfig = async function(doctypeName, fieldID) {
  if (await showCustomConfirm('Delete this field from doctype metadata?')) {
    apiFetch(`/api/v1/meta/${doctypeName}/fields/${fieldID}`, {
      method: 'DELETE'
    }).then(res => {
      if (res && res.ok) {
        loadDoctypeConfig(doctypeName);
      }
    });
  }
};

// Render Prefix configurations view
async function renderPrefixConfigsView(container) {
  const res = await apiFetch('/api/v1/prefix');
  if (!res || !res.ok) return;
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

window.editPrefixConfig = function(docType) {
  const c = state.prefixConfigs.find(x => x.doc_type === docType);
  if (!c) return;

  const prefix = prompt('Enter Prefix:', c.prefix);
  const separator = prompt('Enter Separator:', c.separator);
  const padding = parseInt(prompt('Enter Padding Width:', c.padding_width));
  const reset = prompt('Enter Reset Frequency (ANNUAL/MONTHLY/NEVER):', c.reset_frequency);

  if (prefix && separator && padding) {
    apiFetch('/api/v1/prefix', {
      method: 'POST',
      body: JSON.stringify({
        doc_type: docType,
        prefix,
        separator,
        padding_width: padding,
        reset_frequency: reset,
        active_status: true
      })
    }).then(res => {
      if (res && res.ok) {
        renderView('prefix-configs');
      }
    });
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
  
  apiFetch('/api/v1/labels', {
    method: 'POST',
    body: JSON.stringify({ original_text: orig, custom_text: custom })
  }).then(async res => {
    if (res && res.ok) {
      await fetchLabels();
      renderView('dynamic-labels');
    }
  });
};

window.deleteLabelReplacement = async function(orig) {
  if (await showCustomConfirm(`Remove label mapping for "${orig}"?`)) {
    apiFetch(`/api/v1/labels?original_text=${encodeURIComponent(orig)}`, {
      method: 'DELETE'
    }).then(async res => {
      if (res && res.ok) {
        await fetchLabels();
        renderView('dynamic-labels');
      }
    });
  }
};

// Render Log Hub & panic dashboard logs
async function renderLogHubView(container) {
  const auditRes = await apiFetch('/api/v1/logs/audit');
  const auditLogs = auditRes && auditRes.ok ? await auditRes.json() : [];

  const sysRes = await apiFetch('/api/v1/logs/system');
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
    apiFetch('/api/v1/debug/panic').then(async res => {
      await showCustomAlert('Panic endpoint hit. Re-checking Log Hub for stack trace registration.', 'System Recovery');
      renderView('audit-logs');
    });
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

  const res = await fetch(`/api/v1/import/${currentDoctype}`, {
    method: 'POST',
    headers,
    body: formData
  });

  const summary = document.getElementById('import-result-summary');
  if (res.ok) {
    const result = await res.json();
    summary.style.display = 'block';
    summary.style.backgroundColor = 'rgba(46, 213, 115, 0.1)';
    summary.style.border = '1px solid rgba(46, 213, 115, 0.3)';
    summary.style.color = '#2ed573';
    
    let html = `
      <div style="font-weight:600; margin-bottom:8px;">Import Processed Successfully:</div>
      <div>Total Rows Parsed: ${result.total_rows}</div>
      <div>Successfully Inserted: ${result.success_rows}</div>
      <div>Failed Rows: ${result.failed_rows}</div>
    `;

    if (result.errors && result.errors.length > 0) {
      html += `<div style="font-weight:600; margin-top:12px; color:#ff4757;">Validation Errors:</div><ul style="padding-left:16px; margin-top:4px;">`;
      result.errors.forEach(err => {
        html += `<li>Row ${err.row_number}: ${err.message}</li>`;
      });
      html += `</ul>`;
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

// Window load init
window.addEventListener('DOMContentLoaded', init);
